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

The summary in [Results](#results) gives the version of each engine, and
[Environment](#environment) the rest of the setup.

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

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261116060912-21d9ec0f3fd5
goja                  3.33         2.59          v0.0.0-20261110204830-f493a2b600d6
Sobek                 3.32         2.58          v0.0.0-20261105103427-a5b602015248
otto                 17.8 (9/10)    n/a          v0.5.1
modernc.org/quickjs   2.42         1.81          v0.27.0
QJS                   3.36 (6/10)  2.80 (69/74)  v0.0.6
QuickJS (C)           0.739        0.486         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.407        0.329         v0.44.0
v8go                  0.0208       0.0639        v0.44.0
```

Measured on 2026-11-16: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                         modernc.org/        QuickJS  v8go
Benchmark     Ekuma  goja  Sobek  otto   quickjs       QJS   (C)      --jitless  v8go
Richards      1.00   3.34  3.16   18.8   2.67          2.55  0.789    0.594      0.0211
DeltaBlue     1.00   2.93  2.92   23.7   2.99          2.74  0.850    0.618      0.00741
Encrypt       1.00   2.36  2.37    9.72  1.31           n/a  0.359    0.315      0.00718
Decrypt       1.00   3.82  3.84   15.3   2.18           n/a  0.575    0.501      0.0109
RayTrace      1.00   6.21  6.18   20.7   4.00          5.04  1.06     0.568      0.0187
Earley        1.00   3.53  3.53   20.0   3.32           n/a  0.956    0.377      0.0237
Boyer         1.00   3.41  3.40   18.7   3.18           n/a  0.715    0.392      0.0272
RegExp        1.00   1.52  1.54     n/a  1.58          4.03  0.996    0.0837     0.0300
Splay         1.00   2.71  2.77   13.6   2.20          2.52  0.652    0.377      0.0707
NavierStokes  1.00   6.15  6.08   25.6   2.07          4.02  0.751    0.808      0.0492
Geomean       1.00   3.33  3.32   17.8   2.42          3.36  0.739    0.407      0.0208
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                     modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma   goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms      2.94   10.1     9.11    54.8     7.82          7.67    2.26    1.70      0.0628
DeltaBlue     ms      4.82   14.1    14.1    118      14.5          13.3     4.12    3.00      0.0357
Encrypt       ms     10.2    23.9    24.0     99.0    13.4            n/a    3.70    3.21      0.0737
Decrypt       ms    117     444     447     1867     254              n/a   67.7    60.0       1.27
RayTrace      ms     19.1   121     121      395      76.7          96.8    20.2    10.9       0.364
Earley        ms      4.02   14.1    14.3     80.6    13.3            n/a    3.81    1.52      0.0956
Boyer         ms     77.0   260     262     1445     241              n/a   54.6    30.3       2.08
RegExp        ms    141     214     212         n/a  224           560     141      11.6       4.33
Splay         ms      1.58    3.99    4.38    21.5     3.23          3.82    1.03    0.600     0.114
NavierStokes  ms     59.0   362     357     1509     121           236      43.8    47.9       2.89
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                           modernc.org/          QuickJS  v8go
Benchmark               Ekuma  goja       Sobek      otto  quickjs       QJS     (C)      --jitless  v8go
empty_loop              1.00      6.06       6.05    n/a    2.18          3.20   0.630    0.884      0.0835
empty_down_loop         1.00      6.03       5.99    n/a    2.14          3.13   0.629    0.805      0.0838
empty_down_loop2        1.00      5.22       5.22    n/a    2.14          3.78   0.613    0.710      0.0611
empty_do_loop           1.00      5.08       5.03    n/a    1.87          2.31   0.620    0.593      0.0621
date_now                1.00      2.29       2.29    n/a    1.79          2.83   0.438    0.578      0.467
date_parse              1.00      0.490      0.495   n/a    1.55          1.26   0.419    0.612      0.578
prop_read               1.00      7.22       7.25    n/a    3.39          4.67   1.04     0.812      0.0436
prop_write              1.00      8.33       8.34    n/a    3.45          5.52   0.968    0.981      0.0319
prop_update             1.00      7.45       7.45    n/a    2.79          5.15   0.660    0.970      0.0432
prop_create             1.00      4.26       4.30    n/a    3.04          5.03   1.07     0.427      0.173
prop_clone              1.00      3.13       3.16    n/a    2.67          2.15   0.727    0.0253     0.0196
prop_delete             1.00      1.32       1.27    n/a    1.14          0.936  0.287    0.132      0.110
array_read              1.00      7.56       7.50    n/a    3.17          5.39   1.15     1.38       0.0847
array_write             1.00      3.70       3.70    n/a    2.23         10.3    0.527    1.03       0.0222
array_update            1.00      1.85       1.85    n/a    0.683         1.95   0.210    0.424      0.0105
array_prop_create       1.00      6.14       5.72    n/a    1.42          4.81   0.620    0.721      0.0999
array_slice             1.00     12.9       12.9     n/a   20.4           9.54   3.66     0.0407     0.0299
array_length_read       1.00      7.31       7.08    n/a    3.25          4.56   1.05     0.850      0.114
array_length_decr       1.00      4.75       4.73    n/a    5.29          3.91   0.970    1.19       0.812
array_hole_length_decr  1.00      4.93       5.20    n/a    6.19          5.66   1.85     1.06       0.714
array_push              1.00      4.70       4.70    n/a    2.54          3.35   0.586    0.391      0.0484
array_pop               1.00      1.26       1.23    n/a    2.38          1.23   0.358    0.149      0.0218
typed_array_read        1.00      4.16       3.94    n/a    3.25          2.79   0.623    0.827      0.0449
typed_array_write       1.00      4.02       4.01    n/a    2.18          6.47   0.582    0.697      0.0141
arguments_read          1.00      5.94       5.96    n/a    2.72           n/a   0.515    0.372      0.0593
arguments_strict_read   1.00      4.48       4.54    n/a    1.61           n/a   0.337    0.283      0.0465
global_read             1.00      6.48       6.45    n/a    2.21          4.02   1.03     0.651      0.0270
global_write            1.00      8.19       8.31    n/a    2.72          5.25   0.545    0.584      0.0311
global_write_strict     1.00     10.9       10.6     n/a    2.75          5.40   0.561    0.589      0.0315
local_destruct          1.00      5.33       5.23    n/a    7.55         23.8    1.16     1.31       0.0480
global_destruct         1.00      8.37       8.33    n/a    8.84         10.7    1.38     1.58       0.105
global_destruct_strict  1.00      9.90       9.69    n/a    9.09         10.7    1.38     1.61       0.105
global_func_call        1.00      2.36       2.33    n/a    2.23          1.82   0.470    0.540      0.00390
func_call               1.00      1.95       1.93    n/a    2.23          1.49   0.470    0.569      0.00390
func_closure_call       1.00      1.73       1.75    n/a    2.02          1.39   0.457    0.457      0.0698
int_arith               1.00      7.03       7.08    n/a    2.64          3.72   0.847    0.881      0.0304
float_arith             1.00      9.53       9.63    n/a    2.53          4.23   0.878    1.27       0.0522
map_set_string          1.00      1.41       1.40    n/a    1.83          1.68   0.480    0.208      0.0750
map_set_int             1.00      1.88       1.86    n/a    2.21         34.0    0.575    0.409      0.120
map_set_bigint          1.00      0.826      0.836   n/a    1.01         18.6    0.178    0.239      0.153
map_delete              1.00      1.28       1.27    n/a    1.54           n/a   0.390    0.178      0.0746
weak_map_set            1.00   1115       1067       n/a    1.74           n/a   0.359    0.307      0.143
weak_map_delete         1.00     12.1       12.4     n/a    1.95           n/a   0.464    0.346      0.150
array_for               1.00      6.18       6.20    n/a    2.72          4.08   0.950    1.07       0.0872
array_for_in            1.00      1.37       1.37    n/a    1.09          0.766  0.226    0.0904     0.0735
array_for_of            1.00      6.17       6.04    n/a   10.3           4.98   1.41     2.39       0.292
math_min                1.00      3.24       3.25    n/a    2.43          2.52   0.476    0.509      0.00823
regexp_ascii            1.00      3.24       3.24    n/a    1.48          4.11   0.440    0.180      0.117
regexp_utf16            1.00      2.97       2.98    n/a    1.36          3.86   0.459    0.166      0.111
regexp_replace          1.00      2.00       1.99    n/a    1.65         12.3    1.48     0.194      0.152
string_length           1.00      8.47       8.36    n/a    3.38          2.87   0.697    0.704      0.0707
string_build1           1.00      0.609      0.614   n/a    0.311         0.955  0.107    0.0948     0.0195
string_build1x          1.00      0.617      0.609   n/a    0.310         0.942  0.103    0.0915     0.0193
string_build2c          1.00      0.388      0.385   n/a    0.201         0.686  0.0820   0.0587     0.0124
string_build2           1.00      0.620      0.612   n/a    0.805         1.04   0.331    0.0955     0.0198
string_build3           1.00      0.695      0.699   n/a    0.841         1.02   0.232    0.102      0.0197
string_build4           1.00      0.649      0.648   n/a    0.885         1.04   0.332    0.0928     0.0191
string_build_large1     1.00      0.0131     0.0130  n/a    0.0196        0.238  0.00629  0.00190    0.00131
string_build_large2     1.00      0.0141     0.0142  n/a    0.0193        0.224  0.00581  0.00203    0.00120
string_concat           1.00      2.08       2.06    n/a    3.08          5.00   0.871    0.270      0.239
string_array_join       1.00      1.83       1.85    n/a    2.72          2.42   0.833    0.325      0.305
string_template         1.00      0.291      0.288   n/a    0.643         1.20   0.155    0.111      0.0852
int_to_string           1.00      1.64       1.63    n/a    2.10          2.19   0.611    0.316      0.0894
int_toString            1.00      2.31       2.28    n/a    3.15          2.45   0.564    0.310      0.110
float_to_string         1.00      1.77       1.76    n/a    3.36          3.50   1.59     0.491      0.322
float_toString          1.00      1.60       1.60    n/a    3.86          3.61   1.51     0.466      0.325
float_toFixed           1.00      2.42       2.43    n/a    1.13          1.12   0.315    0.381      0.281
float_toPrecision       1.00      0.679      0.671   n/a    0.900         0.876  0.263    0.206      0.146
float_toExponential     1.00      0.900      0.890   n/a    1.26          1.21   0.369    0.284      0.200
string_to_int           1.00      5.96       6.05    n/a    7.95          8.32   3.00     0.744      0.185
string_to_float         1.00      3.44       3.56    n/a    4.23          4.09   1.62     0.731      0.470
bigint32_arith          1.00      0.879      0.880   n/a    0.459         0.649  0.0963   0.107      0.109
bigint64_arith          1.00      0.879      0.894   n/a    0.608         0.908  0.135    0.121      0.111
bigint256_arith         1.00      0.864      0.862   n/a    1.04          1.44   0.289    0.179      0.110
Geomean                 1.00      2.59       2.58    n/a    1.81          2.80   0.486    0.329      0.0639
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                    modernc.org/           QuickJS   v8go
Benchmark               Unit  Ekuma   goja        Sobek       otto  quickjs       QJS      (C)       --jitless  v8go
empty_loop              ns      6.35      38.6        38.6    n/a     13.8          20.3      4.00      5.61      0.530
empty_down_loop         ns      6.33      38.2        38.0    n/a     13.6          19.8      4.04      5.06      0.530
empty_down_loop2        ns      8.69      45.4        45.5    n/a     18.6          32.9      5.33      6.19      0.532
empty_do_loop           ns      8.53      43.2        42.9    n/a     15.9          19.7      5.29      5.05      0.530
date_now                ns     50.6      115         116      n/a     90.9         143       22.1      29.3      23.6
date_parse              µs      6.59       3.23        3.25   n/a     10.3           8.32     2.77      4.02      3.81
prop_read               ns     25.2      182         183      n/a     85.4         118       26.3      20.5       1.10
prop_write              ns     16.8      140         140      n/a     57.8          92.3     16.2      16.3       0.531
prop_update             ns     40.5      335         282      n/a    104           204       26.8      39.3       1.81
prop_create             ns    543       2312        2284      n/a   1703          2732      567       232        91.6
prop_clone              µs      1.03       3.23        3.25   n/a      2.81          2.21     0.750     0.0261    0.0203
prop_delete             µs      3.79       4.94        4.81   n/a      4.30          3.54     1.10      0.505     0.412
array_read              ns     52.5      399         397      n/a    166           286       60.2      73.1       4.45
array_write             ns     60.2      225         224      n/a    134           615       31.7      61.8       1.34
array_update            ns    297        557         552      n/a    203           580       62.4     126         3.12
array_prop_create       µs     18.6      114         106      n/a     26.3          89.5     11.5      13.4       1.86
array_slice             µs      2.75      35.5        35.5    n/a     56.2          26.4     10.1       0.112     0.0828
array_length_read       ns     24.7      180         177      n/a     81.3         114       26.1      21.4       2.81
array_length_decr       µs     21.3      101         101      n/a    113            83.2     20.8      25.4      17.3
array_hole_length_decr  µs     24.0      118         124      n/a    148           135       44.4      25.5      17.3
array_push              µs     18.9       89.5        88.5    n/a     47.8          62.9     11.0       7.36      0.911
array_pop               µs     54.1       70.0        67.3    n/a    129            65.6     19.7       8.34      1.18
typed_array_read        ns    101        418         398      n/a    327           280       63.6      84.5       4.57
typed_array_write       ns     94.6      380         380      n/a    207           612       55.1      66.3       1.33
arguments_read          ns    535       3180        3210      n/a   1452              n/a   276       199        31.9
arguments_strict_read   ns    683       3060        3185      n/a   1094              n/a   231       193        31.7
global_read             ns     25.3      164         167      n/a     55.9         102       26.1      16.4       0.690
global_write            ns     25.8      218         213      n/a     69.8         136       14.1      15.1       0.806
global_write_strict     ns     25.3      274         268      n/a     69.5         138       14.3      14.9       0.808
local_destruct          ns    162        884         750      n/a   1213          3858      188       186         7.00
global_destruct         ns    121       1011        1010      n/a   1069          1289      166       191        12.7
global_destruct_strict  ns    120       1178        1167      n/a   1093          1283      166       207        12.7
global_func_call        ns    122        289         286      n/a    276           223       57.9      66.1       0.490
func_call               ns    125        237         234      n/a    270           182       57.3      69.0       0.495
func_closure_call       ns    135        235         237      n/a    272           188       62.1      61.6       9.40
int_arith               µs     11.0       76.7        78.0    n/a     29.1          40.9      9.32      9.72      0.334
float_arith             µs     13.5      128         130      n/a     34.0          56.9     11.8      17.0       0.702
map_set_string          µs    276        388         386      n/a    503           464      132        57.4      20.7
map_set_int             µs    135        253         251      n/a    297          4574       76.3      54.3      16.1
map_set_bigint          µs    461        387         394      n/a    466          8601       82.3     113        70.8
map_delete              µs    330        424         421      n/a    505              n/a   128        58.3      24.6
weak_map_set            µs    126     140372      134337      n/a    219              n/a    45.3      39.5      18.0
weak_map_delete         µs    236       3084        3077      n/a    460              n/a   108        82.0      34.6
array_for               µs      1.13       7.00        7.01   n/a      3.08          4.62     1.07      1.21      0.0991
array_for_in            µs     15.8       21.6        21.7    n/a     17.2          12.1      3.56      1.47      1.16
array_for_of            ns    950       5855        5884      n/a   9730          4707     1340      2260       276
math_min                µs     32.9      106         107      n/a     80.4          83.6     15.9      16.7       0.270
regexp_ascii            µs    245        812         795      n/a    368           998      109        44.1      29.2
regexp_utf16            µs    277        832         835      n/a    370          1054      127        45.4      31.1
regexp_replace          µs    401        800         801      n/a    660          4933      594        77.9      61.4
string_length           ns     37.6      319         315      n/a    128           108       26.2      26.5       2.66
string_build1           µs    112         68.8        69.4    n/a     34.9         107       12.0      10.6       2.20
string_build1x          µs    113         69.0        69.0    n/a     35.1         107       12.0      10.4       2.19
string_build2c          µs    177         68.8        68.3    n/a     35.6         122       13.7      10.6       2.26
string_build2           µs    112         69.3        69.2    n/a     89.6         115       36.9      10.7       2.21
string_build3           µs    111         77.6        77.4    n/a     93.8         113       26.0      11.4       2.20
string_build4           µs    115         75.2        74.6    n/a    102           119       38.4      10.7       2.19
string_build_large1     ms    111          1.45        1.43   n/a      2.18         26.0      0.697     0.211     0.144
string_build_large2     ms    120          1.70        1.71   n/a      2.33         27.0      0.700     0.243     0.143
string_concat           ns    685       1397        1398      n/a   2112          3425      596       185       161
string_array_join       ns    723       1322        1336      n/a   1964          1753      599       234       220
string_template         µs      2.29       0.659       0.655  n/a      1.45          2.73     0.351     0.252     0.193
int_to_string           ns    127        212         208      n/a    264           274       78.7      39.5      11.2
int_toString            ns    151        347         344      n/a    486           370       84.4      46.8      16.5
float_to_string         ns    252        447         446      n/a    852           887      401       126        83.2
float_toString          ns    279        447         449      n/a   1083          1015      419       130        91.1
float_toFixed           ns    613       1506        1489      n/a    699           695      193       231       174
float_toPrecision       ns    837        570         562      n/a    754           750      224       173       123
float_toExponential     ns    605        546         538      n/a    762           733      225       172       122
string_to_int           ns     18.7      112         113      n/a    149           156       56.7      13.9       3.45
string_to_float         ns     42.7      147         152      n/a    181           175       69.0      31.2      20.1
bigint32_arith          µs    183        163         162      n/a     85.4         121       17.8      19.7      20.2
bigint64_arith          µs    185        161         167      n/a    112           168       24.7      22.4      20.3
bigint256_arith         µs    199        172         171      n/a    208           286       57.9      35.7      21.9
```

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
