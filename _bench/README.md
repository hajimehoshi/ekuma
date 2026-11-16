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
Ekuma                 1.00         1.00          v0.0.0-20261116080942-4f50ec950347
goja                  3.94         3.35          v0.0.0-20261110204830-f493a2b600d6
Sobek                 3.93         3.34          v0.0.0-20261105103427-a5b602015248
otto                 21.8 (9/10)    n/a          v0.5.1
modernc.org/quickjs   2.87         2.33          v0.27.0
QJS                   3.45 (6/10)  3.68 (69/74)  v0.0.6
QuickJS (C)           0.884        0.626         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.486        0.424         v0.44.0
v8go                  0.0252       0.0797        v0.44.0
```

Measured on 2026-11-16: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                         modernc.org/        QuickJS  v8go
Benchmark     Ekuma  goja  Sobek  otto   quickjs       QJS   (C)      --jitless  v8go
Richards      1.00   3.23  3.21   19.6   2.77          2.69  0.806    0.600      0.0228
DeltaBlue     1.00   2.91  2.91   23.4   2.98          2.72  0.851    0.622      0.00776
Encrypt       1.00   6.72  6.80   27.5   3.75           n/a  1.03     0.882      0.0206
Decrypt       1.00   6.76  6.84   28.3   3.87           n/a  1.03     0.901      0.0193
RayTrace      1.00   6.16  6.20   20.4   4.01          5.08  1.05     0.567      0.0188
Earley        1.00   3.55  3.55   20.9   3.26           n/a  0.941    0.372      0.0233
Boyer         1.00   3.42  3.38   18.9   3.19           n/a  0.706    0.385      0.0277
RegExp        1.00   1.59  1.57     n/a  1.64          4.14  1.02     0.0853     0.0314
Splay         1.00   2.79  2.75   13.8   2.18          2.64  0.725    0.422      0.0765
NavierStokes  1.00   6.41  6.39   27.9   2.16          4.18  0.771    0.854      0.0513
Geomean       1.00   3.94  3.93   21.8   2.87          3.45  0.884    0.486      0.0252
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                     modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma   goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms      2.85    9.21    9.27    54.6     7.73          7.65    2.26    1.70      0.0646
DeltaBlue     ms      4.82   14.1    14.0    115      14.3          13.2     4.14    3.01      0.0376
Encrypt       ms      3.64   24.0    24.3     99.4    13.6            n/a    3.68    3.19      0.0740
Decrypt       ms     67.0   458     451     1954     258              n/a   70.0    59.8       1.30
RayTrace      ms     19.9   118     118      392      76.5          96.6    20.4    10.8       0.359
Earley        ms      4.11   14.6    14.5     85.6    13.5            n/a    3.86    1.55      0.0972
Boyer         ms     77.5   264     263     1481     245              n/a   54.4    30.1       2.16
RegExp        ms    138     218     216         n/a  229           569     144      11.8       4.33
Splay         ms      1.48    4.12    4.06    20.4     3.20          3.90    1.07    0.609     0.114
NavierStokes  ms     56.3   367     359     1573     122           236      43.4    48.0       2.90
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                         modernc.org/          QuickJS  v8go
Benchmark               Ekuma  goja      Sobek     otto  quickjs       QJS     (C)      --jitless  v8go
empty_loop              1.00     6.07      6.08    n/a    2.16          3.12   0.623    0.874      0.0828
empty_down_loop         1.00     6.06      6.05    n/a    2.17          3.17   0.644    0.802      0.0842
empty_down_loop2        1.00     5.28      5.34    n/a    2.25          3.81   0.624    0.722      0.0622
empty_do_loop           1.00     4.85      4.80    n/a    1.78          2.27   0.593    0.599      0.0625
date_now                1.00     2.31      2.27    n/a    1.81          2.83   0.434    0.575      0.462
date_parse              1.00     0.500     0.500   n/a    1.54          1.28   0.422    0.606      0.580
prop_read               1.00     7.24      7.13    n/a    3.65          4.69   1.04     0.817      0.0441
prop_write              1.00     8.44      8.39    n/a    3.47          5.61   0.980    0.992      0.0321
prop_update             1.00     7.53      7.52    n/a    2.75          5.71   0.743    1.10       0.0438
prop_create             1.00     4.07      4.09    n/a    3.00          4.90   1.01     0.416      0.165
prop_clone              1.00     3.04      3.04    n/a    2.40          1.98   0.710    0.0247     0.0185
prop_delete             1.00     1.27      1.27    n/a    1.13          0.940  0.295    0.131      0.110
array_read              1.00     7.56      7.50    n/a    3.17          5.49   1.17     1.40       0.0883
array_write             1.00     3.72      3.73    n/a    2.21          9.95   0.536    1.04       0.0223
array_update            1.00     2.94      2.93    n/a    1.08          3.11   0.327    0.669      0.0169
array_prop_create       1.00     5.77      5.95    n/a    1.39          4.80   0.619    0.724      0.100
array_slice             1.00    13.0      12.9     n/a   20.3           9.56   3.66     0.0408     0.0371
array_length_read       1.00     7.25      7.40    n/a    3.28          4.57   1.06     0.868      0.0869
array_length_decr       1.00     4.75      4.73    n/a    5.29          3.88   0.967    1.20       0.805
array_hole_length_decr  1.00     4.99      5.16    n/a    6.27          5.72   1.87     1.08       0.725
array_push              1.00     4.53      4.66    n/a    2.45          3.27   0.581    0.385      0.0477
array_pop               1.00     1.26      1.25    n/a    2.35          1.23   0.365    0.152      0.0219
typed_array_read        1.00     4.14      3.92    n/a    3.24          2.80   0.622    0.839      0.0451
typed_array_write       1.00     4.00      4.02    n/a    2.18          6.50   0.593    0.730      0.0143
arguments_read          1.00     6.01      5.97    n/a    2.73           n/a   0.509    0.376      0.0595
arguments_strict_read   1.00     4.54      4.63    n/a    1.62           n/a   0.346    0.292      0.0371
global_read             1.00     6.87      6.57    n/a    2.19          3.91   0.998    0.634      0.0273
global_write            1.00    10.1       8.29    n/a    2.80          5.46   0.571    0.607      0.0319
global_write_strict     1.00    11.7      11.9     n/a    2.75          5.42   0.557    0.591      0.0315
local_destruct          1.00     5.28      5.27    n/a    8.50         27.4    1.35     1.32       0.0502
global_destruct         1.00     8.48      8.28    n/a    8.75         10.6    1.37     1.57       0.105
global_destruct_strict  1.00    10.1       9.79    n/a    8.76         10.3    1.37     1.53       0.104
global_func_call        1.00     2.35      2.38    n/a    2.24          1.80   0.474    0.565      0.00380
func_call               1.00     1.94      1.94    n/a    2.24          1.46   0.484    0.561      0.00396
func_closure_call       1.00     1.74      1.73    n/a    2.03          1.39   0.456    0.454      0.0688
int_arith               1.00     7.16      7.20    n/a    2.70          3.81   0.863    0.884      0.0310
float_arith             1.00     9.71      9.86    n/a    2.58          4.39   0.913    1.31       0.0544
map_set_string          1.00     1.41      1.40    n/a    1.82          1.69   0.470    0.204      0.0760
map_set_int             1.00     1.88      1.89    n/a    2.16         33.8    0.570    0.405      0.104
map_set_bigint          1.00     2.25      2.26    n/a    2.71         50.7    0.487    0.640      0.416
map_delete              1.00     1.29      1.27    n/a    1.50           n/a   0.385    0.177      0.0750
weak_map_set            1.00   997       951       n/a    1.56           n/a   0.332    0.280      0.131
weak_map_delete         1.00    13.7      13.3     n/a    1.99           n/a   0.464    0.353      0.150
array_for               1.00     6.22      6.23    n/a    2.75          4.13   0.951    1.07       0.0886
array_for_in            1.00    13.2      13.3     n/a   10.5           7.32   2.08     0.890      0.712
array_for_of            1.00     6.30      6.23    n/a   10.4           5.06   1.43     2.42       0.295
math_min                1.00     3.30      3.26    n/a    2.44          2.55   0.485    0.508      0.00821
regexp_ascii            1.00     3.29      3.24    n/a    1.45          4.07   0.449    0.186      0.118
regexp_utf16            1.00     3.06      2.96    n/a    1.38          3.84   0.464    0.170      0.111
regexp_replace          1.00     2.00      2.00    n/a    1.63         12.4    1.48     0.193      0.154
string_length           1.00     8.43      8.39    n/a    3.37          2.85   0.699    0.699      0.0711
string_build1           1.00     1.49      1.48    n/a    0.751         2.29   0.254    0.229      0.0472
string_build1x          1.00     1.51      1.51    n/a    0.763         2.33   0.282    0.224      0.0471
string_build2c          1.00     1.46      1.45    n/a    0.753         2.63   0.288    0.214      0.0458
string_build2           1.00     1.56      1.51    n/a    1.92          2.51   0.789    0.230      0.0474
string_build3           1.00     0.689     0.684   n/a    0.835         1.01   0.230    0.102      0.0196
string_build4           1.00     1.56      1.56    n/a    2.11          2.45   0.773    0.221      0.0446
string_build_large1     1.00     1.47      1.53    n/a    2.22         27.4    0.734    0.224      0.153
string_build_large2     1.00     0.0147    0.0148  n/a    0.0205        0.234  0.00610  0.00213    0.00120
string_concat           1.00     2.04      2.07    n/a    3.12          5.05   0.876    0.275      0.248
string_array_join       1.00     1.85      1.87    n/a    2.71          2.48   0.841    0.328      0.308
string_template         1.00     1.98      1.97    n/a    4.30          8.16   1.05     0.760      0.576
int_to_string           1.00     1.58      1.56    n/a    1.97          2.05   0.456    0.278      0.0776
int_toString            1.00     2.27      2.28    n/a    3.12          2.44   0.545    0.308      0.106
float_to_string         1.00     1.75      1.74    n/a    3.30          3.42   1.56     0.482      0.322
float_toString          1.00     1.59      1.60    n/a    3.85          3.59   1.52     0.464      0.320
float_toFixed           1.00     2.42      2.45    n/a    1.15          1.15   0.306    0.384      0.284
float_toPrecision       1.00     0.662     0.663   n/a    0.886         0.870  0.262    0.204      0.144
float_toExponential     1.00     0.891     0.879   n/a    1.24          1.19   0.365    0.284      0.203
string_to_int           1.00     5.69      5.79    n/a    7.62          8.05   2.95     0.717      0.178
string_to_float         1.00     3.50      3.48    n/a    4.24          4.13   1.63     0.715      0.477
bigint32_arith          1.00     8.31      8.28    n/a    4.29          6.24   0.915    1.02       0.140
bigint64_arith          1.00     3.37      3.28    n/a    2.27          3.22   0.513    0.459      0.421
bigint256_arith         1.00     0.837     0.830   n/a    1.02          1.31   0.283    0.172      0.106
Geomean                 1.00     3.35      3.34    n/a    2.33          3.68   0.626    0.424      0.0797
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      6.47      39.3       39.2   n/a     14.0           20.5      4.05      5.66      0.538
empty_down_loop         ns      6.35      38.6       38.5   n/a     13.8           20.3      4.10      5.14      0.535
empty_down_loop2        ns      8.67      45.9       46.3   n/a     19.6           33.1      5.41      6.26      0.539
empty_do_loop           ns      9.12      44.2       43.2   n/a     16.1           20.6      5.54      5.72      0.664
date_now                ns     51.3      118        116     n/a     92.2          145       22.2      29.4      23.9
date_parse              µs      6.73       3.31       3.28  n/a     10.3            8.40     2.79      4.02      3.80
prop_read               ns     25.8      187        184     n/a     95.0          120       26.7      20.9       1.12
prop_write              ns     16.9      142        140     n/a     58.3           94.5     17.0      16.5       0.541
prop_update             ns     35.8      269        270     n/a     99.0          205       26.8      39.1       1.57
prop_create             ns    554       2275       2274     n/a   1659           2699      558       228        90.5
prop_clone              µs      1.14       3.29       3.31  n/a      2.76           2.25     0.775     0.0266    0.0206
prop_delete             µs      3.83       4.86       4.88  n/a      4.33           3.62     1.13      0.502     0.423
array_read              ns     53.0      401        397     n/a    168            288       61.2      73.4       4.68
array_write             ns     61.0      225        225     n/a    136            607       32.9      62.5       1.35
array_update            ns    190        559        558     n/a    206            590       62.1     127         3.20
array_prop_create       µs     18.8      114        112     n/a     26.3           90.1     11.6      13.6       1.90
array_slice             µs      2.77      36.1       35.9   n/a     56.9           26.4     10.5       0.118     0.108
array_length_read       ns     24.9      182        188     n/a     83.5          116       26.6      21.6       2.12
array_length_decr       µs     21.7      103        103     n/a    115             84.1     20.9      25.6      17.2
array_hole_length_decr  µs     23.9      120        124     n/a    151            137       44.6      25.8      17.4
array_push              µs     19.1       86.3       89.0   n/a     46.6           62.5     11.1       7.37      0.926
array_pop               µs     54.2       68.5       67.7   n/a    128             66.8     19.7       8.22      1.18
typed_array_read        ns    102        421        396     n/a    330            284       62.7      84.9       4.68
typed_array_write       ns     95.8      383        379     n/a    209            616       56.8      69.2       1.36
arguments_read          ns    541       3295       3230     n/a   1474               n/a   275       200        32.1
arguments_strict_read   ns    681       3118       3154     n/a   1113               n/a   238       201        25.5
global_read             ns     26.1      180        171     n/a     56.8          102       26.0      16.6       0.717
global_write            ns     25.4      255        217     n/a     70.8          138       14.5      15.4       0.808
global_write_strict     ns     25.6      295        301     n/a     70.8          139       14.3      15.1       0.808
local_destruct          ns    140        738        739     n/a   1184           3819      189       184         7.01
global_destruct         ns    122       1038       1003     n/a   1071           1294      168       192        12.8
global_destruct_strict  ns    127       1296       1194     n/a   1070           1287      169       193        12.8
global_func_call        ns    124        294        294     n/a    283            224       58.6      71.6       0.468
func_call               ns    122        237        237     n/a    274            179       59.6      69.0       0.480
func_closure_call       ns    137        238        239     n/a    276            189       62.7      63.4       9.50
int_arith               µs     10.9       78.2       78.7   n/a     29.4           41.4      9.35      9.65      0.338
float_arith             µs     13.2      129        130     n/a     34.1           57.9     12.0      17.3       0.717
map_set_string          µs    277        396        391     n/a    506            468      132        56.5      21.2
map_set_int             µs    135        253        253     n/a    292           4582       77.1      54.8      13.8
map_set_bigint          µs    172        387        388     n/a    468           8685       83.4     110        71.5
map_delete              µs    334        435        424     n/a    503               n/a   129        58.6      24.8
weak_map_set            µs    140     133319     133532     n/a    218               n/a    46.1      39.5      18.3
weak_map_delete         µs    234       3197       3112     n/a    465               n/a   109        82.4      35.2
array_for               µs      1.13       7.11       7.02  n/a      3.10           4.66     1.08      1.22      0.100
array_for_in            µs      1.64      21.8       22.2   n/a     17.4           12.0      3.42      1.46      1.17
array_for_of            ns    944       5897       5882     n/a   9823           4774     1360      2283       276
math_min                µs     33.0      108        108     n/a     81.3           84.4     16.1      16.8       0.273
regexp_ascii            µs    249        826        806     n/a    361           1011      112        46.3      29.4
regexp_utf16            µs    276        855        840     n/a    377           1063      128        46.5      30.7
regexp_replace          µs    402        804        803     n/a    657           4963      598        78.4      61.8
string_length           ns     38.1      317        317     n/a    128            108       26.2      26.4       2.67
string_build1           µs     46.8       69.5       69.1   n/a     35.3          107       11.9      10.7       2.20
string_build1x          µs     46.6       70.0       70.0   n/a     35.4          108       13.1      10.4       2.24
string_build2c          µs     47.5       68.8       69.5   n/a     36.1          124       13.8      10.2       2.29
string_build2           µs     46.9       72.7       70.8   n/a     89.9          117       37.1      10.8       2.21
string_build3           µs    114         77.9       77.9   n/a     95.1          114       26.2      11.6       2.22
string_build4           µs     49.2       80.7       78.0   n/a    103            119       38.4      10.8       2.22
string_build_large1     µs    978       1462       1462     n/a   2196          26489      704       223       148
string_build_large2     ms    116          1.72       1.72  n/a      2.37          27.1      0.709     0.247     0.144
string_concat           ns    685       1400       1418     n/a   2136           3456      602       188       170
string_array_join       ns    724       1332       1344     n/a   1971           1781      604       239       221
string_template         ns    335        662        656     n/a   1460           2790      352       255       193
int_to_string           ns    134        210        210     n/a    264            275       61.2      37.2      10.3
int_toString            ns    153        350        350     n/a    478            372       83.4      47.0      16.1
float_to_string         ns    260        454        452     n/a    858            884      403       125        83.8
float_toString          ns    282        449        452     n/a   1085           1011      429       131        90.1
float_toFixed           ns    620       1499       1515     n/a    707            704      190       236       176
float_toPrecision       ns    863        569        567     n/a    765            747      223       174       122
float_toExponential     ns    618        552        551     n/a    770            736      226       174       124
string_to_int           ns     19.6      112        113     n/a    150            158       58.1      14.1       3.50
string_to_float         ns     42.9      150        149     n/a    181            178       69.7      30.7      20.4
bigint32_arith          µs     19.6      163        162     n/a     84.4          122       17.9      19.9       2.75
bigint64_arith          µs     49.2      165        164     n/a    112            158       25.1      22.4      20.6
bigint256_arith         µs    209        174        173     n/a    211            272       58.8      35.8      22.1
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
| date | 2026-11-16T17:09:58+09:00 | 2026-11-16T17:22:26+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261116080942-4f50ec950347 | v0.0.0-20261116080942-4f50ec950347 |
| github.com/dop251/goja | v0.0.0-20261110204830-f493a2b600d6 | v0.0.0-20261110204830-f493a2b600d6 |
| github.com/grafana/sobek | v0.0.0-20261105103427-a5b602015248 | v0.0.0-20261105103427-a5b602015248 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 3.00 3.69 5.42 | 5.81 4.46 4.54 |
| load-end | 5.81 4.46 4.54 | 2.66 2.93 3.53 |
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
- `-engine` runs the engines whose names match a regular expression. `-engine '^ekuma$' -results
  DIR` measures Ekuma alone in about 3 minutes, and benchstat compares its results files with
  earlier ones. Times measured hours apart can differ by 20% on a busy machine, so the tables
  come from runs of all the engines.
- `-report` makes the tables from the results files without running the benchmarks, and
  `-update README.md,../README.md` writes them between the markers of these files.
- `-setup` only fetches the programs and builds QuickJS in C. Then `go test` runs the benchmarks
  directly, with `-nostdinc++`, which v8go needs and `#cgo` directives cannot give:

  ```sh
  CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' -bench '^BenchmarkV8$/^Richards$/^(ekuma|goja)$'
  ```

  V8's flags apply to a whole process, so `v8go` and `v8go-jitless` need separate runs.
