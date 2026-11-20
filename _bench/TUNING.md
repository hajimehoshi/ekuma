# Tuning Ekuma

These notes measure Ekuma alone, to find where it spends its time and to check a change to it.
[README.md](README.md) describes the comparison with the other engines.

## CPU profiling

On 2026-11-21, Ekuma's interpreter loop (`(*VM).exec`) accounted for 46.7% of CPU samples
directly and 64.0% including its callees. The profile covers all ten V8 benchmarks at
`f130b4d6c37d` on an Apple M3 Pro running macOS 27.2 and go1.27.4. It includes parsing,
setup, benchmark calibration, and garbage collection. See the
[profile summary](results/darwin_arm64/ekuma-v8-cpu.txt).

For setup requirements, see [Running](README.md#running). To capture another profile, run
the following commands from `_bench`:

```sh
GOMAXPROCS=2 CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' \
  -bench '^BenchmarkV8$/./^ekuma$' -benchtime 1s \
  -cpuprofile /tmp/ekuma-v8-cpu.pprof -o /tmp/ekuma-v8-profile.test
go tool pprof -top -nodecount=15 /tmp/ekuma-v8-profile.test /tmp/ekuma-v8-cpu.pprof
```

Run profiling separately from engine comparisons to keep its overhead out of the timings.

## Instruction counts and the loop head of exec

A change to `(*VM).exec` can speed up one architecture and slow down another. Go's register
allocator stores a value that the dispatch loop carries in the block that dominates all of its
reloads. When a fast path of the first switch runs out of registers and reloads such a value, the
store moves to the loop head and runs before every instruction. arm64 has registers enough that
its loop head stores nothing; amd64 has fewer, so check a change to exec on both.

The stores show in the disassembly of exec, from the start of `i := &fp.code[pc]`, where the loop
jumps back to, to the jump through the table of the first switch: `JMP 0(R10)(DX*8)` or similar on
amd64, `JMP (R27)` on arm64. From the repository root:

```sh
GOARCH=amd64 go test -c -o /tmp/ekuma-amd64.test . && \
  go tool objdump -s 'engine\.\(\*VM\)\.exec$' /tmp/ekuma-amd64.test
GOARCH=arm64 go test -c -o /tmp/ekuma-arm64.test . && \
  go tool objdump -s 'engine\.\(\*VM\)\.exec$' /tmp/ekuma-arm64.test
```

At `be32cfd`, amd64 stores two values there, pc and the address of the code, and only for the
instructions of the first switch; arm64 stores none. A store that leaves the loop head can come
back as more work in the fast paths, so judge a change by the instructions it executes, not by
the loop head alone.

A virtual machine often has no hardware counters. On Linux, cachegrind of
[Valgrind](https://valgrind.org/) counts the instructions that a test binary executes, and the
counts repeat to within 0.1%. With `-test.benchtime Nx`, `go test` runs the benchmark function
with b.N = 1 and then b.N = N, each after its setup, so one iteration is the difference between
the counts with N and with 2, divided by N − 2. From the repository root:

```sh
go test -c -o /tmp/ekuma.test .
for n in 2 202; do
  GOGC=off GOMAXPROCS=1 valgrind --tool=cachegrind --cache-sim=no --cachegrind-out-file=/dev/null \
    /tmp/ekuma.test -test.run '^$' -test.bench 'Workload/plugins=12$' -test.benchtime ${n}x 2>&1 | grep 'I refs'
done
```

A benchmark of the V8 suite can be counted the same way with a small program that evaluates
`base.js`, the file of the benchmark and the script of `v8SelectFormat` in `v8_test.go`, and then
runs `__run(n)`.

Time on a shared virtual machine varies by 10% or more from run to run. Compare builds in rounds
that run each build once, in an order that rotates from round to round, with a second build of
the baseline as a control, and take the median of the per-round ratios over 50 to 80 rounds.
