# Engine comparison

This module compares the speed of Ekuma with other JavaScript engines for Go. It is a module of
its own, so the engines that it compares are not dependencies of Ekuma.

## Engines

| Column | Engine |
|---|---|
| Ekuma | This repository |
| goja | [github.com/dop251/goja](https://github.com/dop251/goja), written in Go |
| Sobek | [github.com/grafana/sobek](https://github.com/grafana/sobek), the fork of goja that k6 uses |
| otto | [github.com/robertkrimen/otto](https://github.com/robertkrimen/otto), written in Go |
| modernc.org/quickjs | [modernc.org/quickjs](https://pkg.go.dev/modernc.org/quickjs), QuickJS translated from C to Go |
| QJS | [github.com/fastschema/qjs](https://github.com/fastschema/qjs), QuickJS-ng compiled to WebAssembly and run by wazero |
| QuickJS (C) | [QuickJS](https://github.com/bellard/quickjs) in C, as a reference |
| v8go `--jitless` | [github.com/tommie/v8go](https://github.com/tommie/v8go) with V8's `--jitless` flag, which leaves only its bytecode interpreter |
| v8go | The same with V8's JIT compilers, as a reference |

- QuickJS (C) is the commit that modernc.org/quickjs translates, the release 2026-06-04, built
  with its Makefile (`-O2`). It runs as a `qjs` process that evaluates the scripts that it reads
  from its standard input, so the benchmarks time it as they time the other engines.
- QJS evaluates every script in strict mode, and its options cannot turn that off, so it fails
  the benchmarks that are sloppy code.
- otto implements ES5 and uses Go's `regexp`, so it cannot parse microbench.js or the regular
  expressions of the RegExp benchmark.

The versions of the engines are in [Environment](#environment).

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

- Each run of a benchmark on an engine is a process of its own, which `go test -bench` times for
  1 second for the V8 suite and 200 milliseconds for microbench.js. Parsing and setting up are
  not timed, and the instance of the engine is kept while `go test` raises the number of
  iterations, so a JIT warms up.
- A round runs each benchmark once on every engine, in an order that rotates with the round and
  the benchmark. The V8 suite runs 5 rounds, and microbench.js 3.
- The tables give the median over the rounds of the time relative to Ekuma's in the same round
  (lower is faster), and a suite's row in the summary is the geometric mean of these over the
  benchmarks that the engine ran.
- Only time is compared. The memory of V8, of QuickJS in C and of QJS's WebAssembly instance is
  not in the Go heap, so allocations are not comparable.
- The machine was not idle: the load averages are in [Environment](#environment). A single round
  sometimes took twice as long as the others, which the medians leave out. A difference of less
  than about 20% between two engines is within the noise.

## Results

<!-- BEGIN jsbench summary -->

|  | Ekuma | goja | Sobek | otto | modernc.org/quickjs | QJS | QuickJS (C) | v8go `--jitless` | v8go |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| V8 benchmark suite v7 | 1.00 | 3.33 | 3.32 | 17.8 (9 of 10) | 2.42 | 3.36 (6 of 10) | 0.739 | 0.407 | 0.0208 |
| QuickJS microbench | 1.00 | 2.59 | 2.58 | n/a | 1.81 | 2.80 (69 of 74) | 0.486 | 0.329 | 0.0639 |

Measured on 2026-11-16: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

| Benchmark | Ekuma | goja | Sobek | otto | modernc.org/quickjs | QJS | QuickJS (C) | v8go `--jitless` | v8go |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| Richards | 1.00 | 3.34 | 3.16 | 18.8 | 2.67 | 2.55 | 0.789 | 0.594 | 0.0211 |
| DeltaBlue | 1.00 | 2.93 | 2.92 | 23.7 | 2.99 | 2.74 | 0.850 | 0.618 | 0.00741 |
| Encrypt | 1.00 | 2.36 | 2.37 | 9.72 | 1.31 | n/a | 0.359 | 0.315 | 0.00718 |
| Decrypt | 1.00 | 3.82 | 3.84 | 15.3 | 2.18 | n/a | 0.575 | 0.501 | 0.0109 |
| RayTrace | 1.00 | 6.21 | 6.18 | 20.7 | 4.00 | 5.04 | 1.06 | 0.568 | 0.0187 |
| Earley | 1.00 | 3.53 | 3.53 | 20.0 | 3.32 | n/a | 0.956 | 0.377 | 0.0237 |
| Boyer | 1.00 | 3.41 | 3.40 | 18.7 | 3.18 | n/a | 0.715 | 0.392 | 0.0272 |
| RegExp | 1.00 | 1.52 | 1.54 | n/a | 1.58 | 4.03 | 0.996 | 0.0837 | 0.0300 |
| Splay | 1.00 | 2.71 | 2.77 | 13.6 | 2.20 | 2.52 | 0.652 | 0.377 | 0.0707 |
| NavierStokes | 1.00 | 6.15 | 6.08 | 25.6 | 2.07 | 4.02 | 0.751 | 0.808 | 0.0492 |
| Geomean | 1.00 | 3.33 | 3.32 | 17.8 (9 of 10) | 2.42 | 3.36 (6 of 10) | 0.739 | 0.407 | 0.0208 |

<!-- END jsbench V8.relative -->

Median time of one run:

<!-- BEGIN jsbench V8.time -->

| Benchmark | Ekuma | goja | Sobek | otto | modernc.org/quickjs | QJS | QuickJS (C) | v8go `--jitless` | v8go |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| Richards | 2.94 ms | 10.1 ms | 9.11 ms | 54.8 ms | 7.82 ms | 7.67 ms | 2.26 ms | 1.70 ms | 62.8 µs |
| DeltaBlue | 4.82 ms | 14.1 ms | 14.1 ms | 118 ms | 14.5 ms | 13.3 ms | 4.12 ms | 3.00 ms | 35.7 µs |
| Encrypt | 10.2 ms | 23.9 ms | 24.0 ms | 99.0 ms | 13.4 ms | n/a | 3.70 ms | 3.21 ms | 73.7 µs |
| Decrypt | 117 ms | 444 ms | 447 ms | 1.87 s | 254 ms | n/a | 67.7 ms | 60.0 ms | 1.27 ms |
| RayTrace | 19.1 ms | 121 ms | 121 ms | 395 ms | 76.7 ms | 96.8 ms | 20.2 ms | 10.9 ms | 364 µs |
| Earley | 4.02 ms | 14.1 ms | 14.3 ms | 80.6 ms | 13.3 ms | n/a | 3.81 ms | 1.52 ms | 95.6 µs |
| Boyer | 77.0 ms | 260 ms | 262 ms | 1.44 s | 241 ms | n/a | 54.6 ms | 30.3 ms | 2.08 ms |
| RegExp | 141 ms | 214 ms | 212 ms | n/a | 224 ms | 560 ms | 141 ms | 11.6 ms | 4.33 ms |
| Splay | 1.58 ms | 3.99 ms | 4.38 ms | 21.5 ms | 3.23 ms | 3.82 ms | 1.03 ms | 600 µs | 114 µs |
| NavierStokes | 59.0 ms | 362 ms | 357 ms | 1.51 s | 121 ms | 236 ms | 43.8 ms | 47.9 ms | 2.89 ms |

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

| Benchmark | Ekuma | goja | Sobek | otto | modernc.org/quickjs | QJS | QuickJS (C) | v8go `--jitless` | v8go |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| empty_loop | 1.00 | 6.06 | 6.05 | n/a | 2.18 | 3.20 | 0.630 | 0.884 | 0.0835 |
| empty_down_loop | 1.00 | 6.03 | 5.99 | n/a | 2.14 | 3.13 | 0.629 | 0.805 | 0.0838 |
| empty_down_loop2 | 1.00 | 5.22 | 5.22 | n/a | 2.14 | 3.78 | 0.613 | 0.710 | 0.0611 |
| empty_do_loop | 1.00 | 5.08 | 5.03 | n/a | 1.87 | 2.31 | 0.620 | 0.593 | 0.0621 |
| date_now | 1.00 | 2.29 | 2.29 | n/a | 1.79 | 2.83 | 0.438 | 0.578 | 0.467 |
| date_parse | 1.00 | 0.490 | 0.495 | n/a | 1.55 | 1.26 | 0.419 | 0.612 | 0.578 |
| prop_read | 1.00 | 7.22 | 7.25 | n/a | 3.39 | 4.67 | 1.04 | 0.812 | 0.0436 |
| prop_write | 1.00 | 8.33 | 8.34 | n/a | 3.45 | 5.52 | 0.968 | 0.981 | 0.0319 |
| prop_update | 1.00 | 7.45 | 7.45 | n/a | 2.79 | 5.15 | 0.660 | 0.970 | 0.0432 |
| prop_create | 1.00 | 4.26 | 4.30 | n/a | 3.04 | 5.03 | 1.07 | 0.427 | 0.173 |
| prop_clone | 1.00 | 3.13 | 3.16 | n/a | 2.67 | 2.15 | 0.727 | 0.0253 | 0.0196 |
| prop_delete | 1.00 | 1.32 | 1.27 | n/a | 1.14 | 0.936 | 0.287 | 0.132 | 0.110 |
| array_read | 1.00 | 7.56 | 7.50 | n/a | 3.17 | 5.39 | 1.15 | 1.38 | 0.0847 |
| array_write | 1.00 | 3.70 | 3.70 | n/a | 2.23 | 10.3 | 0.527 | 1.03 | 0.0222 |
| array_update | 1.00 | 1.85 | 1.85 | n/a | 0.683 | 1.95 | 0.210 | 0.424 | 0.0105 |
| array_prop_create | 1.00 | 6.14 | 5.72 | n/a | 1.42 | 4.81 | 0.620 | 0.721 | 0.0999 |
| array_slice | 1.00 | 12.9 | 12.9 | n/a | 20.4 | 9.54 | 3.66 | 0.0407 | 0.0299 |
| array_length_read | 1.00 | 7.31 | 7.08 | n/a | 3.25 | 4.56 | 1.05 | 0.850 | 0.114 |
| array_length_decr | 1.00 | 4.75 | 4.73 | n/a | 5.29 | 3.91 | 0.970 | 1.19 | 0.812 |
| array_hole_length_decr | 1.00 | 4.93 | 5.20 | n/a | 6.19 | 5.66 | 1.85 | 1.06 | 0.714 |
| array_push | 1.00 | 4.70 | 4.70 | n/a | 2.54 | 3.35 | 0.586 | 0.391 | 0.0484 |
| array_pop | 1.00 | 1.26 | 1.23 | n/a | 2.38 | 1.23 | 0.358 | 0.149 | 0.0218 |
| typed_array_read | 1.00 | 4.16 | 3.94 | n/a | 3.25 | 2.79 | 0.623 | 0.827 | 0.0449 |
| typed_array_write | 1.00 | 4.02 | 4.01 | n/a | 2.18 | 6.47 | 0.582 | 0.697 | 0.0141 |
| arguments_read | 1.00 | 5.94 | 5.96 | n/a | 2.72 | n/a | 0.515 | 0.372 | 0.0593 |
| arguments_strict_read | 1.00 | 4.48 | 4.54 | n/a | 1.61 | n/a | 0.337 | 0.283 | 0.0465 |
| global_read | 1.00 | 6.48 | 6.45 | n/a | 2.21 | 4.02 | 1.03 | 0.651 | 0.0270 |
| global_write | 1.00 | 8.19 | 8.31 | n/a | 2.72 | 5.25 | 0.545 | 0.584 | 0.0311 |
| global_write_strict | 1.00 | 10.9 | 10.6 | n/a | 2.75 | 5.40 | 0.561 | 0.589 | 0.0315 |
| local_destruct | 1.00 | 5.33 | 5.23 | n/a | 7.55 | 23.8 | 1.16 | 1.31 | 0.0480 |
| global_destruct | 1.00 | 8.37 | 8.33 | n/a | 8.84 | 10.7 | 1.38 | 1.58 | 0.105 |
| global_destruct_strict | 1.00 | 9.90 | 9.69 | n/a | 9.09 | 10.7 | 1.38 | 1.61 | 0.105 |
| global_func_call | 1.00 | 2.36 | 2.33 | n/a | 2.23 | 1.82 | 0.470 | 0.540 | 0.00390 |
| func_call | 1.00 | 1.95 | 1.93 | n/a | 2.23 | 1.49 | 0.470 | 0.569 | 0.00390 |
| func_closure_call | 1.00 | 1.73 | 1.75 | n/a | 2.02 | 1.39 | 0.457 | 0.457 | 0.0698 |
| int_arith | 1.00 | 7.03 | 7.08 | n/a | 2.64 | 3.72 | 0.847 | 0.881 | 0.0304 |
| float_arith | 1.00 | 9.53 | 9.63 | n/a | 2.53 | 4.23 | 0.878 | 1.27 | 0.0522 |
| map_set_string | 1.00 | 1.41 | 1.40 | n/a | 1.83 | 1.68 | 0.480 | 0.208 | 0.0750 |
| map_set_int | 1.00 | 1.88 | 1.86 | n/a | 2.21 | 34.0 | 0.575 | 0.409 | 0.120 |
| map_set_bigint | 1.00 | 0.826 | 0.836 | n/a | 1.01 | 18.6 | 0.178 | 0.239 | 0.153 |
| map_delete | 1.00 | 1.28 | 1.27 | n/a | 1.54 | n/a | 0.390 | 0.178 | 0.0746 |
| weak_map_set | 1.00 | 1115 | 1067 | n/a | 1.74 | n/a | 0.359 | 0.307 | 0.143 |
| weak_map_delete | 1.00 | 12.1 | 12.4 | n/a | 1.95 | n/a | 0.464 | 0.346 | 0.150 |
| array_for | 1.00 | 6.18 | 6.20 | n/a | 2.72 | 4.08 | 0.950 | 1.07 | 0.0872 |
| array_for_in | 1.00 | 1.37 | 1.37 | n/a | 1.09 | 0.766 | 0.226 | 0.0904 | 0.0735 |
| array_for_of | 1.00 | 6.17 | 6.04 | n/a | 10.3 | 4.98 | 1.41 | 2.39 | 0.292 |
| math_min | 1.00 | 3.24 | 3.25 | n/a | 2.43 | 2.52 | 0.476 | 0.509 | 0.00823 |
| regexp_ascii | 1.00 | 3.24 | 3.24 | n/a | 1.48 | 4.11 | 0.440 | 0.180 | 0.117 |
| regexp_utf16 | 1.00 | 2.97 | 2.98 | n/a | 1.36 | 3.86 | 0.459 | 0.166 | 0.111 |
| regexp_replace | 1.00 | 2.00 | 1.99 | n/a | 1.65 | 12.3 | 1.48 | 0.194 | 0.152 |
| string_length | 1.00 | 8.47 | 8.36 | n/a | 3.38 | 2.87 | 0.697 | 0.704 | 0.0707 |
| string_build1 | 1.00 | 0.609 | 0.614 | n/a | 0.311 | 0.955 | 0.107 | 0.0948 | 0.0195 |
| string_build1x | 1.00 | 0.617 | 0.609 | n/a | 0.310 | 0.942 | 0.103 | 0.0915 | 0.0193 |
| string_build2c | 1.00 | 0.388 | 0.385 | n/a | 0.201 | 0.686 | 0.0820 | 0.0587 | 0.0124 |
| string_build2 | 1.00 | 0.620 | 0.612 | n/a | 0.805 | 1.04 | 0.331 | 0.0955 | 0.0198 |
| string_build3 | 1.00 | 0.695 | 0.699 | n/a | 0.841 | 1.02 | 0.232 | 0.102 | 0.0197 |
| string_build4 | 1.00 | 0.649 | 0.648 | n/a | 0.885 | 1.04 | 0.332 | 0.0928 | 0.0191 |
| string_build_large1 | 1.00 | 0.0131 | 0.0130 | n/a | 0.0196 | 0.238 | 0.00629 | 0.00190 | 0.00131 |
| string_build_large2 | 1.00 | 0.0141 | 0.0142 | n/a | 0.0193 | 0.224 | 0.00581 | 0.00203 | 0.00120 |
| string_concat | 1.00 | 2.08 | 2.06 | n/a | 3.08 | 5.00 | 0.871 | 0.270 | 0.239 |
| string_array_join | 1.00 | 1.83 | 1.85 | n/a | 2.72 | 2.42 | 0.833 | 0.325 | 0.305 |
| string_template | 1.00 | 0.291 | 0.288 | n/a | 0.643 | 1.20 | 0.155 | 0.111 | 0.0852 |
| int_to_string | 1.00 | 1.64 | 1.63 | n/a | 2.10 | 2.19 | 0.611 | 0.316 | 0.0894 |
| int_toString | 1.00 | 2.31 | 2.28 | n/a | 3.15 | 2.45 | 0.564 | 0.310 | 0.110 |
| float_to_string | 1.00 | 1.77 | 1.76 | n/a | 3.36 | 3.50 | 1.59 | 0.491 | 0.322 |
| float_toString | 1.00 | 1.60 | 1.60 | n/a | 3.86 | 3.61 | 1.51 | 0.466 | 0.325 |
| float_toFixed | 1.00 | 2.42 | 2.43 | n/a | 1.13 | 1.12 | 0.315 | 0.381 | 0.281 |
| float_toPrecision | 1.00 | 0.679 | 0.671 | n/a | 0.900 | 0.876 | 0.263 | 0.206 | 0.146 |
| float_toExponential | 1.00 | 0.900 | 0.890 | n/a | 1.26 | 1.21 | 0.369 | 0.284 | 0.200 |
| string_to_int | 1.00 | 5.96 | 6.05 | n/a | 7.95 | 8.32 | 3.00 | 0.744 | 0.185 |
| string_to_float | 1.00 | 3.44 | 3.56 | n/a | 4.23 | 4.09 | 1.62 | 0.731 | 0.470 |
| bigint32_arith | 1.00 | 0.879 | 0.880 | n/a | 0.459 | 0.649 | 0.0963 | 0.107 | 0.109 |
| bigint64_arith | 1.00 | 0.879 | 0.894 | n/a | 0.608 | 0.908 | 0.135 | 0.121 | 0.111 |
| bigint256_arith | 1.00 | 0.864 | 0.862 | n/a | 1.04 | 1.44 | 0.289 | 0.179 | 0.110 |
| Geomean | 1.00 | 2.59 | 2.58 | n/a | 1.81 | 2.80 (69 of 74) | 0.486 | 0.329 | 0.0639 |

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function:

<!-- BEGIN jsbench Micro.time -->

| Benchmark | Ekuma | goja | Sobek | otto | modernc.org/quickjs | QJS | QuickJS (C) | v8go `--jitless` | v8go |
| --- | --: | --: | --: | --: | --: | --: | --: | --: | --: |
| empty_loop | 6.35 ns | 38.6 ns | 38.6 ns | n/a | 13.8 ns | 20.3 ns | 4.00 ns | 5.61 ns | 0.530 ns |
| empty_down_loop | 6.33 ns | 38.2 ns | 38.0 ns | n/a | 13.6 ns | 19.8 ns | 4.04 ns | 5.06 ns | 0.530 ns |
| empty_down_loop2 | 8.69 ns | 45.4 ns | 45.5 ns | n/a | 18.6 ns | 32.9 ns | 5.33 ns | 6.19 ns | 0.532 ns |
| empty_do_loop | 8.53 ns | 43.2 ns | 42.9 ns | n/a | 15.9 ns | 19.7 ns | 5.29 ns | 5.05 ns | 0.530 ns |
| date_now | 50.6 ns | 115 ns | 116 ns | n/a | 90.9 ns | 143 ns | 22.1 ns | 29.3 ns | 23.6 ns |
| date_parse | 6.59 µs | 3.23 µs | 3.25 µs | n/a | 10.3 µs | 8.32 µs | 2.77 µs | 4.02 µs | 3.81 µs |
| prop_read | 25.2 ns | 182 ns | 183 ns | n/a | 85.4 ns | 118 ns | 26.3 ns | 20.5 ns | 1.10 ns |
| prop_write | 16.8 ns | 140 ns | 140 ns | n/a | 57.8 ns | 92.3 ns | 16.2 ns | 16.3 ns | 0.531 ns |
| prop_update | 40.5 ns | 335 ns | 282 ns | n/a | 104 ns | 204 ns | 26.8 ns | 39.3 ns | 1.81 ns |
| prop_create | 543 ns | 2.31 µs | 2.28 µs | n/a | 1.70 µs | 2.73 µs | 567 ns | 232 ns | 91.6 ns |
| prop_clone | 1.03 µs | 3.23 µs | 3.25 µs | n/a | 2.81 µs | 2.21 µs | 750 ns | 26.1 ns | 20.3 ns |
| prop_delete | 3.79 µs | 4.94 µs | 4.81 µs | n/a | 4.30 µs | 3.54 µs | 1.10 µs | 505 ns | 412 ns |
| array_read | 52.5 ns | 399 ns | 397 ns | n/a | 166 ns | 286 ns | 60.2 ns | 73.1 ns | 4.45 ns |
| array_write | 60.2 ns | 225 ns | 224 ns | n/a | 134 ns | 615 ns | 31.7 ns | 61.8 ns | 1.34 ns |
| array_update | 297 ns | 557 ns | 552 ns | n/a | 203 ns | 580 ns | 62.4 ns | 126 ns | 3.12 ns |
| array_prop_create | 18.6 µs | 114 µs | 106 µs | n/a | 26.3 µs | 89.5 µs | 11.5 µs | 13.4 µs | 1.86 µs |
| array_slice | 2.75 µs | 35.5 µs | 35.5 µs | n/a | 56.2 µs | 26.4 µs | 10.1 µs | 112 ns | 82.8 ns |
| array_length_read | 24.7 ns | 180 ns | 177 ns | n/a | 81.3 ns | 114 ns | 26.1 ns | 21.4 ns | 2.81 ns |
| array_length_decr | 21.3 µs | 101 µs | 101 µs | n/a | 113 µs | 83.2 µs | 20.8 µs | 25.4 µs | 17.3 µs |
| array_hole_length_decr | 24.0 µs | 118 µs | 124 µs | n/a | 148 µs | 135 µs | 44.4 µs | 25.5 µs | 17.3 µs |
| array_push | 18.9 µs | 89.5 µs | 88.5 µs | n/a | 47.8 µs | 62.9 µs | 11.0 µs | 7.36 µs | 911 ns |
| array_pop | 54.1 µs | 70.0 µs | 67.3 µs | n/a | 129 µs | 65.6 µs | 19.7 µs | 8.34 µs | 1.18 µs |
| typed_array_read | 101 ns | 418 ns | 398 ns | n/a | 327 ns | 280 ns | 63.6 ns | 84.5 ns | 4.57 ns |
| typed_array_write | 94.6 ns | 380 ns | 380 ns | n/a | 207 ns | 612 ns | 55.1 ns | 66.3 ns | 1.33 ns |
| arguments_read | 535 ns | 3.18 µs | 3.21 µs | n/a | 1.45 µs | n/a | 276 ns | 199 ns | 31.9 ns |
| arguments_strict_read | 683 ns | 3.06 µs | 3.19 µs | n/a | 1.09 µs | n/a | 231 ns | 193 ns | 31.7 ns |
| global_read | 25.3 ns | 164 ns | 167 ns | n/a | 55.9 ns | 102 ns | 26.1 ns | 16.4 ns | 0.690 ns |
| global_write | 25.8 ns | 218 ns | 213 ns | n/a | 69.8 ns | 136 ns | 14.1 ns | 15.1 ns | 0.806 ns |
| global_write_strict | 25.3 ns | 274 ns | 268 ns | n/a | 69.5 ns | 138 ns | 14.3 ns | 14.9 ns | 0.808 ns |
| local_destruct | 162 ns | 884 ns | 750 ns | n/a | 1.21 µs | 3.86 µs | 188 ns | 186 ns | 7.00 ns |
| global_destruct | 121 ns | 1.01 µs | 1.01 µs | n/a | 1.07 µs | 1.29 µs | 166 ns | 191 ns | 12.7 ns |
| global_destruct_strict | 120 ns | 1.18 µs | 1.17 µs | n/a | 1.09 µs | 1.28 µs | 166 ns | 207 ns | 12.7 ns |
| global_func_call | 122 ns | 289 ns | 286 ns | n/a | 276 ns | 223 ns | 57.9 ns | 66.1 ns | 0.490 ns |
| func_call | 125 ns | 237 ns | 234 ns | n/a | 270 ns | 182 ns | 57.3 ns | 69.0 ns | 0.495 ns |
| func_closure_call | 135 ns | 235 ns | 237 ns | n/a | 272 ns | 188 ns | 62.1 ns | 61.6 ns | 9.40 ns |
| int_arith | 11.0 µs | 76.7 µs | 78.0 µs | n/a | 29.1 µs | 40.9 µs | 9.32 µs | 9.72 µs | 334 ns |
| float_arith | 13.5 µs | 128 µs | 130 µs | n/a | 34.0 µs | 56.9 µs | 11.8 µs | 17.0 µs | 702 ns |
| map_set_string | 276 µs | 388 µs | 386 µs | n/a | 503 µs | 464 µs | 132 µs | 57.4 µs | 20.7 µs |
| map_set_int | 135 µs | 253 µs | 251 µs | n/a | 297 µs | 4.57 ms | 76.3 µs | 54.3 µs | 16.1 µs |
| map_set_bigint | 461 µs | 387 µs | 394 µs | n/a | 466 µs | 8.60 ms | 82.3 µs | 113 µs | 70.8 µs |
| map_delete | 330 µs | 424 µs | 421 µs | n/a | 505 µs | n/a | 128 µs | 58.3 µs | 24.6 µs |
| weak_map_set | 126 µs | 140 ms | 134 ms | n/a | 219 µs | n/a | 45.3 µs | 39.5 µs | 18.0 µs |
| weak_map_delete | 236 µs | 3.08 ms | 3.08 ms | n/a | 460 µs | n/a | 108 µs | 82.0 µs | 34.6 µs |
| array_for | 1.13 µs | 7.00 µs | 7.01 µs | n/a | 3.08 µs | 4.62 µs | 1.07 µs | 1.21 µs | 99.1 ns |
| array_for_in | 15.8 µs | 21.6 µs | 21.7 µs | n/a | 17.2 µs | 12.1 µs | 3.56 µs | 1.47 µs | 1.16 µs |
| array_for_of | 950 ns | 5.86 µs | 5.88 µs | n/a | 9.73 µs | 4.71 µs | 1.34 µs | 2.26 µs | 276 ns |
| math_min | 32.9 µs | 106 µs | 107 µs | n/a | 80.4 µs | 83.6 µs | 15.9 µs | 16.7 µs | 270 ns |
| regexp_ascii | 245 µs | 812 µs | 795 µs | n/a | 368 µs | 998 µs | 109 µs | 44.1 µs | 29.2 µs |
| regexp_utf16 | 277 µs | 832 µs | 835 µs | n/a | 370 µs | 1.05 ms | 127 µs | 45.4 µs | 31.1 µs |
| regexp_replace | 401 µs | 800 µs | 801 µs | n/a | 660 µs | 4.93 ms | 594 µs | 77.9 µs | 61.4 µs |
| string_length | 37.6 ns | 319 ns | 315 ns | n/a | 128 ns | 108 ns | 26.2 ns | 26.5 ns | 2.66 ns |
| string_build1 | 112 µs | 68.8 µs | 69.4 µs | n/a | 34.9 µs | 107 µs | 12.0 µs | 10.6 µs | 2.20 µs |
| string_build1x | 113 µs | 69.0 µs | 69.0 µs | n/a | 35.1 µs | 107 µs | 12.0 µs | 10.4 µs | 2.19 µs |
| string_build2c | 177 µs | 68.8 µs | 68.3 µs | n/a | 35.6 µs | 122 µs | 13.7 µs | 10.6 µs | 2.26 µs |
| string_build2 | 112 µs | 69.3 µs | 69.2 µs | n/a | 89.6 µs | 115 µs | 36.9 µs | 10.7 µs | 2.21 µs |
| string_build3 | 111 µs | 77.6 µs | 77.4 µs | n/a | 93.8 µs | 113 µs | 26.0 µs | 11.4 µs | 2.20 µs |
| string_build4 | 115 µs | 75.2 µs | 74.6 µs | n/a | 102 µs | 119 µs | 38.4 µs | 10.7 µs | 2.19 µs |
| string_build_large1 | 111 ms | 1.45 ms | 1.43 ms | n/a | 2.18 ms | 26.0 ms | 697 µs | 211 µs | 144 µs |
| string_build_large2 | 120 ms | 1.70 ms | 1.71 ms | n/a | 2.33 ms | 27.0 ms | 700 µs | 243 µs | 143 µs |
| string_concat | 685 ns | 1.40 µs | 1.40 µs | n/a | 2.11 µs | 3.42 µs | 596 ns | 185 ns | 161 ns |
| string_array_join | 723 ns | 1.32 µs | 1.34 µs | n/a | 1.96 µs | 1.75 µs | 599 ns | 234 ns | 220 ns |
| string_template | 2.29 µs | 660 ns | 656 ns | n/a | 1.45 µs | 2.73 µs | 351 ns | 252 ns | 193 ns |
| int_to_string | 127 ns | 212 ns | 208 ns | n/a | 264 ns | 274 ns | 78.7 ns | 39.5 ns | 11.2 ns |
| int_toString | 151 ns | 347 ns | 344 ns | n/a | 486 ns | 370 ns | 84.4 ns | 46.8 ns | 16.5 ns |
| float_to_string | 252 ns | 447 ns | 446 ns | n/a | 852 ns | 887 ns | 401 ns | 126 ns | 83.2 ns |
| float_toString | 279 ns | 447 ns | 449 ns | n/a | 1.08 µs | 1.01 µs | 419 ns | 130 ns | 91.1 ns |
| float_toFixed | 613 ns | 1.51 µs | 1.49 µs | n/a | 699 ns | 695 ns | 193 ns | 231 ns | 174 ns |
| float_toPrecision | 837 ns | 570 ns | 562 ns | n/a | 754 ns | 750 ns | 224 ns | 173 ns | 123 ns |
| float_toExponential | 605 ns | 546 ns | 538 ns | n/a | 762 ns | 733 ns | 225 ns | 172 ns | 122 ns |
| string_to_int | 18.7 ns | 112 ns | 113 ns | n/a | 149 ns | 156 ns | 56.7 ns | 13.9 ns | 3.45 ns |
| string_to_float | 42.7 ns | 147 ns | 152 ns | n/a | 181 ns | 175 ns | 69.0 ns | 31.2 ns | 20.1 ns |
| bigint32_arith | 183 µs | 163 µs | 162 µs | n/a | 85.4 µs | 121 µs | 17.8 µs | 19.7 µs | 20.2 µs |
| bigint64_arith | 185 µs | 161 µs | 167 µs | n/a | 112 µs | 168 µs | 24.7 µs | 22.4 µs | 20.3 µs |
| bigint256_arith | 199 µs | 172 µs | 171 µs | n/a | 208 µs | 286 µs | 57.9 µs | 35.7 µs | 21.9 µs |

<!-- END jsbench Micro.time -->

### Missing results

<!-- BEGIN jsbench missing -->

- otto in V8 benchmark suite v7 (RegExp): `regexp.js: Line 1096:7 Invalid regular expression: re2: Invalid (?=) <lookahead> (and 1 more errors)`
- QJS in V8 benchmark suite v7 (Encrypt, Decrypt): `ReferenceError: setupEngine is not defined`
- QJS in V8 benchmark suite v7 (Earley, Boyer): `SyntaxError: Octal escape sequences are not allowed in strict mode`
- otto in QuickJS microbench (all 74): `microbench.js: Line 91:18 Unexpected token ) (and 917 more errors)`
- QJS in QuickJS microbench (arguments_read, arguments_strict_read): `ReferenceError: sum is not defined`
- QJS in QuickJS microbench (map_delete, weak_map_set, weak_map_delete): `ReferenceError: len is not defined`

<!-- END jsbench missing -->

### Environment

<!-- BEGIN jsbench environment -->

|  | V8 benchmark suite v7 | QuickJS microbench |
| --- | --- | --- |
| date | 2026-11-16T15:11:56+09:00 | 2026-11-16T15:24:24+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261116060912-21d9ec0f3fd5 | v0.0.0-20261116060912-21d9ec0f3fd5 |
| github.com/dop251/goja | v0.0.0-20261110204830-f493a2b600d6 | v0.0.0-20261110204830-f493a2b600d6 |
| github.com/grafana/sobek | v0.0.0-20261105103427-a5b602015248 | v0.0.0-20261105103427-a5b602015248 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 2.29 3.07 3.59 | 10.02 5.85 4.66 |
| load-end | 10.02 5.85 4.66 | 3.94 3.84 3.99 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->

## Running

This needs Go, make and Clang 21 or later, which [v8go](https://github.com/tommie/v8go) needs for
the libc++ headers of V8. In this directory:

```sh
go run ./cmd/jsbench
```

It fetches the benchmark programs and builds QuickJS in C into `ekuma/bench` in the user cache
directory, or into the directory that `EKUMA_BENCH` names. It writes the output of `go test` to
`results/GOOS_GOARCH`, which [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) can
read, and prints the tables. The whole run takes about 25 minutes on an Apple M3 Pro.

- `-suite V8` or `-suite Micro` runs one suite, `-bench` runs the benchmarks whose names match a
  regular expression, and `-rounds` sets the number of rounds.
- `-report` makes the tables from the results files without running the benchmarks, and
  `-update README.md,../README.md` writes them between the markers of these files.
- `-setup` only fetches the programs and builds QuickJS in C. Then `go test` runs the benchmarks
  directly, with `-nostdinc++`, which v8go needs and `#cgo` directives cannot give:

  ```sh
  CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' -bench '^BenchmarkV8$/^Richards$/^(ekuma|goja)$'
  ```

  V8's flags apply to a whole process, so `v8go` and `v8go-jitless` need separate runs.
