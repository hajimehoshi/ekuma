# Engine comparison

This module compares the speed of Ekuma with other JavaScript engines for Go. It is a module of
its own, so the engines that it compares are not dependencies of Ekuma.

[RESULTS.md](RESULTS.md) has the results of each benchmark, and [TUNING.md](TUNING.md) notes on
profiling Ekuma and checking a change to its interpreter.

## Engines

| Column | Engine |
|---|---|
| Ekuma | This repository |
| goja | [github.com/dop251/goja](https://github.com/dop251/goja), written in Go |
| Sobek | [github.com/grafana/sobek](https://github.com/grafana/sobek), the fork of goja that k6 uses |
| moejs | [github.com/MoeForge/moejs](https://github.com/MoeForge/moejs), written in Go |
| otto | [github.com/robertkrimen/otto](https://github.com/robertkrimen/otto), written in Go |
| modernc.org/quickjs | [modernc.org/quickjs](https://pkg.go.dev/modernc.org/quickjs), QuickJS translated from C to Go |
| QJS | [github.com/fastschema/qjs](https://github.com/fastschema/qjs), QuickJS-ng compiled to WebAssembly and run by wazero |
| QuickJS (C) | [QuickJS](https://github.com/bellard/quickjs) in C, as a reference |
| v8go `--jitless` | [github.com/tommie/v8go](https://github.com/tommie/v8go) with V8's `--jitless` flag, which leaves only its bytecode interpreter |
| v8go | The same with V8's JIT compilers, as a reference |

- moejs runs with `MutableIntrinsics: true` so DeltaBlue and RayTrace can extend built-ins during
  setup.
- QuickJS (C) is the commit that modernc.org/quickjs translates, the release 2026-06-04, built
  with its Makefile (`-O2`). It runs as a `qjs` process that evaluates the scripts that it reads
  from its standard input, so the benchmarks time it as they time the other engines.
- QJS evaluates every script in strict mode, and its options cannot turn that off, so it fails
  the benchmarks that are sloppy code.
- otto implements ES5 and uses Go's `regexp`, so it cannot parse microbench.js or the regular
  expressions of the RegExp benchmark.

The comparison uses the Go engine versions pinned in [go.mod](go.mod). QuickJS in C uses
the revision translated by modernc.org/quickjs. [Results](#results) lists the measured
versions, and [Environment](RESULTS.md#environment) records the run configuration.

## Suites

- V8 benchmark suite v7: [the benchmarks of V8](https://github.com/v8/v8/tree/4.5.1/benchmarks)
  at the tag 4.5.1. One iteration is one run of a benchmark after its setup. The benchmarks
  check their results.
- QuickJS microbench: the functions of
  [microbench.js](https://github.com/bellard/quickjs/blob/888481da0b69b369333bfcbf52156273954f8c72/tests/microbench.js)
  that its runner runs, other than `sort_bench`, which measures time itself. One iteration is one
  iteration of the loop of a function, which may perform its operation several times.

The repository contains neither: their files are under the licenses that they state, and some
state none. `go run ./cmd/jsbench` downloads them at fixed revisions and checks their SHA-256
sums.

## Method

- Each benchmark runs in its own process for each engine, with a target duration of 1 second
  for the V8 suite and 200 milliseconds for microbench.js. This run used `GOMAXPROCS=2`.
  The harness loads the benchmark programs and creates the VM. Timing starts after the VM
  parses the programs and runs their setup.
  Once per timed batch, the harness formats a short script with the iteration count (`b.N`).
  The engine parses and evaluates that script to run all `b.N` iterations. The same VM is
  reused as `go test` increases `b.N`, so a JIT can warm up.
- A round runs each benchmark once on every engine, in an order that rotates with the round and
  the benchmark. The published results use one round of each suite. By default the harness
  runs 5 rounds of the V8 suite and 3 of microbench.js.
- The tables give the median of the time ratios relative to Ekuma (lower is faster). Each ratio
  pairs results from the same round where both engines succeeded. A suite's row in the summary is
  the geometric mean across benchmarks with paired results. `(k/n)` gives their count when fewer
  than all n benchmarks have results. Failed runs remain in the raw results and appear in
  [Missing results](RESULTS.md#missing-results), even when other rounds succeeded.
- Only time is compared. The memory of V8, of QuickJS in C and of QJS's WebAssembly instance is
  not in the Go heap, so allocations are not comparable.
- This measurement used an external watchdog that checked resident memory every 500 ms and
  killed the benchmark process group above 2 GiB per process or 4 GiB overall. The watchdog
  was validated with a bounded allocation before the run.
- Small timing differences can reflect noise in a single round. The recorded load averages
  are in [Environment](RESULTS.md#environment).

## Results

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark    QuickJS
                     suite v7        microbench     Version
Ekuma                 1.00           1.00           v0.0.0-20261126074323-b9ac5f60d84c
goja                  7.11           5.76           v0.0.0-20261118204651-8dfece3d1e1d
Sobek                 7.12           5.76           v0.0.0-20261119094010-6eceb967bb5b
moejs                 0.919          1.21           v0.1.1
otto                 35.7    (9/10)   n/a           v0.5.1
modernc.org/quickjs   4.92           3.88           v0.27.0
QJS                   6.62   (6/10)  6.12  (69/74)  v0.0.6
QuickJS (C)           1.50           1.04           v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.837          0.699          v0.44.0
v8go                  0.0418         0.134          v0.44.0
```

Measured on 2026-11-26: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

[RESULTS.md](RESULTS.md) has the tables of each benchmark, the missing results and the environment.

## Running

This needs Go, make and Clang 21 or later, which [v8go](https://github.com/tommie/v8go) needs for
the libc++ headers of V8. In this directory:

```sh
go run ./cmd/jsbench
```

It fetches the benchmark programs and builds QuickJS in C into `ekuma/bench` in the user cache
directory, or into the directory that `EKUMA_BENCH` names. It writes the output of `go test` to
`results/GOOS_GOARCH`, which [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) can
read, and prints the tables. The default run takes about 25 minutes on an Apple M3 Pro.
Use `GOMAXPROCS=2 go run ./cmd/jsbench -rounds 1` to repeat the published run.

- `-suite V8` or `-suite Micro` runs one suite, `-bench` runs the benchmarks whose names match a
  regular expression, and `-rounds` sets the number of rounds.
- `-engine` runs the engines whose names match a regular expression. `-engine '^ekuma$' -results
  DIR` measures Ekuma alone in about 3 minutes, and benchstat compares its results files with
  earlier ones. Times measured hours apart can differ by 20% on a busy machine, so the tables
  come from runs of all the engines.
- `-engine '^(ekuma|moejs)$' -results results/ekuma-moejs` compares Ekuma with moejs and
  writes those results to a separate directory.
- `-report` makes the tables from the results files without running the benchmarks, and
  `-update README.md,RESULTS.md,../README.md` writes them between the markers of these files.
- `-setup` only fetches the programs and builds QuickJS in C. Then `go test` runs the benchmarks
  directly, with `-nostdinc++`, which v8go needs and `#cgo` directives cannot give:

  ```sh
  CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' -bench '^BenchmarkV8$/^Richards$/^(ekuma|goja)$'
  ```

  V8's flags apply to a whole process, so `v8go` and `v8go-jitless` need separate runs.
