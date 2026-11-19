# CPU profiling

A separate Ekuma profile of the V8 suite was captured on 2026-11-20 at `eb2d4fe68006`,
on Apple M3 Pro, macOS 27.2, and go1.27.4, with a 1-second benchmark duration.
`(*VM).exec` accounted for 46.0% of flat CPU samples and 63.1% cumulatively. See the
[profile summary](results/darwin_arm64/Ekuma-V8-cpu.txt). This profile covers the entire test
process, including parsing, setup, benchmark calibration, and garbage collection. Its
samples are not the timed operations alone, and do not contribute to the comparison tables.

For setup requirements, see [Running](README.md#running). To capture another profile, run
the following commands from `_bench`:

```sh
CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' \
  -bench '^BenchmarkV8$/./^ekuma$' -benchtime 1s \
  -cpuprofile /tmp/ekuma-v8-cpu.pprof -o /tmp/ekuma-v8-profile.test
go tool pprof -top /tmp/ekuma-v8-profile.test /tmp/ekuma-v8-cpu.pprof
```

Run profiling separately from engine comparisons to keep its overhead out of the timings.
