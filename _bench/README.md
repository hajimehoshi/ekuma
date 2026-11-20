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

The comparison uses the dependency versions checked on 2026-11-20, with goja and Sobek at
their latest upstream commits and the other Go engine modules at their latest releases.
QuickJS in C stays at the revision translated by modernc.org/quickjs. The summary in
[Results](#results) gives the version of each engine, and [Environment](#environment) the
rest of the setup.

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
  (lower is faster). A ratio uses only rounds in which both the engine and Ekuma succeeded.
  A suite's row in the summary is the geometric mean over benchmarks with such paired results;
  `(k/n)` gives their count when fewer than all n benchmarks have results. Failed runs remain
  in the raw results and are listed below, even when other rounds of that benchmark succeeded.
- Only time is compared. The memory of V8, of QuickJS in C and of QJS's WebAssembly instance is
  not in the Go heap, so allocations are not comparable.
- This measurement used an external watchdog that checked resident memory every 500 ms and
  killed the benchmark process group above 2 GiB per process or 4 GiB overall. The watchdog
  was validated with a bounded allocation before the run.
- The machine was not idle: the load averages are in [Environment](#environment). The medians
  reduce the influence of unusually slow rounds. A difference of less than about 20% between
  two engines is within the noise.

## Results

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261119181810-eb2d4fe68006
goja                  6.62         4.60          v0.0.0-20261118204651-8dfece3d1e1d
Sobek                 6.62         4.61          v0.0.0-20261119094010-6eceb967bb5b
otto                 33.0 (9/10)    n/a          v0.5.1
modernc.org/quickjs   4.83         3.19          v0.27.0
QJS                   6.31 (6/10)  5.08 (69/74)  v0.0.6
QuickJS (C)           1.48         0.858         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.821        0.582         v0.44.0
v8go                  0.0418       0.101         v0.44.0
```

Measured on 2026-11-20: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                          modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja   Sobek  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00    5.33   5.34  32.2   4.62           4.46  1.32     0.992      0.0367
DeltaBlue     1.00    4.91   4.92  38.6   4.94           4.49  1.40     1.03       0.0123
Encrypt       1.00    9.75   9.79  41.2   5.52            n/a  1.49     1.30       0.0300
Decrypt       1.00    9.77   9.79  41.7   5.60            n/a  1.48     1.31       0.0281
RayTrace      1.00    9.06   9.09  30.4   5.79           7.35  1.53     0.819      0.0271
Earley        1.00    4.91   4.93  28.2   4.54            n/a  1.29     0.523      0.0316
Boyer         1.00    5.64   5.63  31.7   5.16            n/a  1.20     0.652      0.0431
RegExp        1.00    7.04   7.07    n/a  7.47          18.5   4.64     0.381      0.142
Splay         1.00    3.51   3.49  17.9   2.81           3.42  0.910    0.540      0.0956
NavierStokes  1.00   10.5   10.4   45.2   3.45           6.74  1.25     1.40       0.0845
Geomean       1.00    6.62   6.62  33.0   4.83           6.31  1.48     0.821      0.0418
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.71    9.12    9.13    55.5     7.88          7.70    2.28    1.70      0.0621
DeltaBlue     ms     2.92   14.3    14.3    115      14.5          13.2     4.10    3.02      0.0363
Encrypt       ms     2.46   24.0    24.0    100      13.6            n/a    3.67    3.20      0.0739
Decrypt       ms    46.4   449     451     1899     258              n/a   68.5    60.1       1.29
RayTrace      ms    13.4   122     124      411      77.8          98.1    21.1    11.0       0.363
Earley        ms     3.05   14.7    14.5     84.6    13.3            n/a    3.88    1.54      0.0960
Boyer         ms    47.1   266     263     1485     243              n/a   58.2    30.9       2.10
RegExp        ms    30.2   221     220         n/a  232           571     143      11.8       4.35
Splay         ms     1.18    4.14    4.11    21.5     3.36          3.93    1.07    0.640     0.113
NavierStokes  ms    35.6   375     367     1613     123           236      44.1    50.9       2.93
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                         modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja      Sobek     otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00      7.93      7.59   n/a    2.74          3.94  0.789    1.11       0.105
empty_down_loop         1.00      7.63      7.49   n/a    2.65          3.86  0.791    0.999      0.105
empty_down_loop2        1.00      8.43      8.40   n/a    3.49          6.08  0.996    1.16       0.0996
empty_do_loop           1.00      8.57      8.60   n/a    3.21          3.93  1.05     0.991      0.105
date_now                1.00      2.56      2.54   n/a    1.95          3.15  0.482    0.638      0.512
date_parse              1.00      1.75      1.77   n/a    5.50          4.48  1.56     2.12       2.03
prop_read               1.00      8.96      9.21   n/a    4.14          5.79  1.26     0.997      0.0538
prop_write              1.00     11.7      11.9    n/a    4.85          8.06  1.40     1.39       0.0452
prop_update             1.00      8.19      8.15   n/a    3.05          6.23  0.824    1.21       0.0495
prop_create             1.00      4.14      4.26   n/a    2.95          5.02  1.03     0.418      0.173
prop_clone              1.00      4.00      3.93   n/a    3.37          2.72  0.929    0.0326     0.0253
prop_delete             1.00      2.17      2.15   n/a    1.96          1.62  0.504    0.223      0.183
array_read              1.00     11.0      11.1    n/a    4.77          8.17  1.71     2.07       0.157
array_write             1.00      6.44      6.36   n/a    3.82         17.4   0.896    1.76       0.0386
array_update            1.00      4.76      4.72   n/a    1.72          4.98  0.536    1.10       0.0277
array_prop_create       1.00      6.50      6.64   n/a    1.34          4.98  0.606    0.665      0.0942
array_slice             1.00     11.6      11.6    n/a   18.5           8.52  3.30     0.0368     0.0280
array_length_read       1.00      9.97     10.1    n/a    4.54          6.45  1.48     1.22       0.161
array_length_decr       1.00      4.27      4.29   n/a    4.79          3.50  0.872    1.10       0.715
array_hole_length_decr  1.00      4.51      4.65   n/a    5.62          5.06  1.63     0.929      0.698
array_push              1.00      5.38      5.40   n/a    2.85          3.93  0.709    0.461      0.0572
array_pop               1.00      4.62      4.71   n/a    8.80          4.45  1.26     0.543      0.0404
typed_array_read        1.00      4.20      4.22   n/a    3.47          2.94  0.647    0.889      0.0499
typed_array_write       1.00      3.87      3.90   n/a    2.12          6.30  0.567    0.700      0.0138
arguments_read          1.00      7.82      7.53   n/a    3.41           n/a  0.639    0.455      0.0701
arguments_strict_read   1.00      7.22      7.14   n/a    2.53           n/a  0.547    0.467      0.0708
global_read             1.00      9.49      9.41   n/a    2.99          5.31  1.25     0.873      0.0365
global_write            1.00     12.1      12.1    n/a    4.00          7.90  0.815    0.854      0.0457
global_write_strict     1.00     16.0      17.1    n/a    3.94          7.78  0.802    0.850      0.0462
local_destruct          1.00      5.62      5.67   n/a    9.00         29.3   1.43     1.39       0.0538
global_destruct         1.00      7.44      7.53   n/a    7.77          9.54  1.24     1.42       0.0929
global_destruct_strict  1.00      8.59      8.35   n/a    7.60          9.05  1.22     1.35       0.0899
global_func_call        1.00      3.83      3.89   n/a    3.78          2.91  0.754    0.894      0.00617
func_call               1.00      3.25      3.28   n/a    3.69          2.44  0.774    1.02       0.00601
func_closure_call       1.00      2.63      2.58   n/a    2.99          2.07  0.680    0.671      0.103
int_arith               1.00      8.69      8.72   n/a    3.31          4.69  1.05     1.06       0.0386
float_arith             1.00     12.2      12.4    n/a    3.26          5.57  1.15     1.68       0.0668
map_set_string          1.00      1.79      1.77   n/a    2.29          2.07  0.599    0.259      0.0969
map_set_int             1.00      2.31      2.08   n/a    2.29         36.2   0.616    0.439      0.111
map_set_bigint          1.00      2.55      2.53   n/a    2.96         57.3   0.531    0.697      0.450
map_delete              1.00      1.46      1.45   n/a    1.72           n/a  0.443    0.201      0.0866
weak_map_set            1.00   1081      1018      n/a    1.43           n/a  0.307    0.269      0.124
weak_map_delete         1.00     20.2      22.5    n/a    3.16           n/a  0.733    0.560      0.237
array_for               1.00      7.40      7.37   n/a    3.26          4.89  1.13     1.28       0.105
array_for_in            1.00     11.7      11.6    n/a    9.34          6.37  1.81     0.778      0.624
array_for_of            1.00      7.42      7.39   n/a   12.2           5.75  1.68     2.82       0.343
math_min                1.00      3.61      3.66   n/a    2.71          2.91  0.545    0.561      0.00914
regexp_ascii            1.00      4.74      4.66   n/a    2.12          5.98  0.662    0.268      0.172
regexp_utf16            1.00      4.28      4.27   n/a    1.88          5.42  0.652    0.234      0.153
regexp_replace          1.00      2.31      2.28   n/a    1.87         14.4   1.72     0.224      0.174
string_length           1.00     13.2      13.2    n/a    5.43          4.45  1.09     1.12       0.113
string_build1           1.00      1.86      1.81   n/a    0.943         2.86  0.307    0.290      0.0602
string_build1x          1.00      1.91      1.91   n/a    0.973         2.96  0.328    0.287      0.0609
string_build2c          1.00      1.86      1.86   n/a    0.962         3.29  0.370    0.282      0.0588
string_build2           1.00      1.87      1.86   n/a    2.39          3.22  1.04     0.300      0.0605
string_build3           1.00      1.66      1.77   n/a    2.25          2.53  0.617    0.250      0.0528
string_build4           1.00      2.02      2.01   n/a    2.89          3.30  1.02     0.282      0.0581
string_build_large1     1.00      1.76      1.76   n/a    2.71         32.2   0.846    0.264      0.175
string_build_large2     1.00      1.75      1.74   n/a    2.40         28.5   0.726    0.249      0.142
string_concat           1.00      2.17      2.18   n/a    3.27          5.34  0.915    0.292      0.260
string_array_join       1.00      1.77      1.90   n/a    2.72          2.53  0.894    0.317      0.295
string_template         1.00      1.97      2.11   n/a    4.06          8.33  1.06     0.778      0.576
int_to_string           1.00      2.10      2.10   n/a    2.69          2.74  0.628    0.375      0.106
int_toString            1.00      3.17      3.26   n/a    4.59          3.43  0.775    0.424      0.149
float_to_string         1.00      2.17      2.18   n/a    4.17          4.30  1.94     0.614      0.00154
float_toString          1.00      2.14      2.14   n/a    5.09          4.75  1.96     0.627      0.430
float_toFixed           1.00      4.45      4.42   n/a    2.01          2.02  0.564    0.677      0.498
float_toPrecision       1.00      1.74      1.79   n/a    2.34          2.22  0.666    0.518      0.369
float_toExponential     1.00      1.68      1.66   n/a    2.32          2.29  0.695    0.582      0.395
string_to_int           1.00      6.14      6.13   n/a    8.08          8.64  3.06     0.765      0.191
string_to_float         1.00      2.85      2.75   n/a    3.40          3.34  1.30     0.578      0.412
bigint32_arith          1.00      7.96      8.05   n/a    4.20          5.13  1.03     1.03       0.171
bigint64_arith          1.00      2.96      3.02   n/a    2.06          2.93  0.462    0.415      0.343
bigint256_arith         1.00      0.896     0.887  n/a    1.06          1.42  0.296    0.182      0.111
Geomean                 1.00      4.60      4.61   n/a    3.19          5.08  0.858    0.582      0.101
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS  v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)      --jitless  v8go
empty_loop              ns      5.14      40.8       39.0   n/a     14.1           20.9      4.03     5.72      0.555
empty_down_loop         ns      5.15      39.3       38.5   n/a     13.6           19.9      4.07     5.14      0.535
empty_down_loop2        ns      5.43      45.8       45.6   n/a     19.1           33.9      5.44     6.55      0.539
empty_do_loop           ns      5.12      43.5       44.1   n/a     16.6           20.1      5.38     5.07      0.539
date_now                ns     46.2      117        120     n/a     91.1          147       22.3     30.0      23.6
date_parse              µs      1.88       3.26       3.34  n/a     10.4            8.43     2.96     4.03      3.83
prop_read               ns     21.1      189        195     n/a     87.4          122       26.8     21.1       1.16
prop_write              ns     11.9      140        142     n/a     57.8           95.0     16.5     16.5       0.533
prop_update             ns     32.9      268        268     n/a    101            204       27.1     39.6       1.61
prop_create             ns    565       2255       2406     n/a   1674           2730      576      240        97.5
prop_clone              ns    826       3270       3270     n/a   2785           2249      777       26.7      20.9
prop_delete             µs      2.24       4.85       4.82  n/a      4.33           3.62     1.11     0.500     0.407
array_read              ns     35.5      389        390     n/a    168            290       60.5     73.6       5.57
array_write             ns     35.2      227        227     n/a    136            615       31.9     62.0       1.41
array_update            ns    119        561        556     n/a    205            598       65.2    130         3.13
array_prop_create       µs     20.1      156        134     n/a     27.0           91.7     12.1     14.5       2.05
array_slice             µs      3.08      36.3       36.3   n/a     57.5           26.9     10.3      0.114     0.0857
array_length_read       ns     17.8      178        183     n/a     81.5          115       26.2     21.8       2.93
array_length_decr       µs     25.3      107        105     n/a    121             85.9     21.4     27.0      17.9
array_hole_length_decr  µs     27.8      125        128     n/a    158            143       45.3     25.8      19.4
array_push              µs     16.3       88.2       91.6   n/a     46.6           63.1     11.5      7.46      0.996
array_pop               µs     15.6       73.9       73.5   n/a    130             70.6     19.7      8.47      1.25
typed_array_read        ns     99.4      418        420     n/a    345            292       64.4     88.2       4.98
typed_array_write       ns    102        391        397     n/a    216            638       58.0     74.1       1.39
arguments_read          ns    441       3449       3376     n/a   1503               n/a   284      209        32.6
arguments_strict_read   ns    447       3229       3226     n/a   1132               n/a   244      209        32.6
global_read             ns     19.6      177        176     n/a     58.5          104       25.2     16.6       0.699
global_write            ns     17.9      216        216     n/a     71.5          141       14.6     15.2       0.817
global_write_strict     ns     17.8      284        304     n/a     69.9          139       14.3     15.2       0.822
local_destruct          ns    132        738        751     n/a   1201           3885      191      185         7.08
global_destruct         ns    138       1030       1037     n/a   1076           1329      170      200        12.9
global_destruct_strict  ns    145       1219       1200     n/a   1095           1291      168      191        13.0
global_func_call        ns     78.0      292        301     n/a    295            227       58.8     68.4       0.468
func_call               ns     75.0      244        247     n/a    274            183       60.1     75.2       0.464
func_closure_call       ns     92.8      244        239     n/a    278            190       62.2     62.3       9.61
int_arith               µs      8.87      77.0       77.3   n/a     29.6           41.9      9.26     9.48      0.343
float_arith             µs     10.5      128        131     n/a     34.8           58.6     12.1     17.0       0.724
map_set_string          µs    224        400        393     n/a    513            468      134       57.5      21.1
map_set_int             µs    132        294        275     n/a    303           4766       83.1     57.9      14.3
map_set_bigint          µs    166        405        419     n/a    517           9784       87.9    115        74.5
map_delete              µs    294        431        424     n/a    516               n/a   130       59.4      26.2
weak_map_set            µs    150     162428     152949     n/a    215               n/a    46.2     40.4      18.6
weak_map_delete         µs    150       3076       3336     n/a    474               n/a   110       83.8      36.0
array_for               ns    956       7384       7128     n/a   3140           4785     1089     1214        99.6
array_for_in            µs      1.93      21.9       21.9   n/a     17.4           11.9      3.41     1.45      1.17
array_for_of            ns    806       6238       5931     n/a   9879           4903     1373     2277       276
math_min                µs     29.7      107        108     n/a     81.1           86.3     16.2     16.6       0.272
regexp_ascii            µs    170        807        826     n/a    362           1001      114       47.2      29.3
regexp_utf16            µs    201        869        883     n/a    378           1113      129       47.0      30.8
regexp_replace          µs    353        801        802     n/a    658           4966      592       78.9      61.5
string_length           ns     24.1      319        319     n/a    132            107       26.6     27.0       2.75
string_build1           µs     38.2       69.9       69.3   n/a     35.3          107       11.8     10.7       2.22
string_build1x          µs     36.3       69.4       69.2   n/a     35.2          107       12.0     10.4       2.20
string_build2c          µs     37.2       68.4       68.5   n/a     35.8          123       13.8     10.5       2.19
string_build2           µs     37.4       70.1       69.5   n/a     89.3          120       36.8     10.8       2.26
string_build3           µs     47.2       78.9       82.3   n/a     97.1          118       26.7     11.8       2.23
string_build4           µs     38.2       77.1       76.7   n/a    109            125       38.5     10.8       2.21
string_build_large1     µs    815       1442       1438     n/a   2211          26210      691      213       143
string_build_large2     µs    978       1709       1691     n/a   2344          27678      707      244       139
string_concat           ns    660       1453       1453     n/a   2155           3522      603      196       171
string_array_join       ns    745       1345       1462     n/a   2040           1935      664      253       222
string_template         ns    392        710        755     n/a   1593           2942      364      325       197
int_to_string           ns    103        217        216     n/a    276            282       64.6     38.6      10.9
int_toString            ns    112        357        366     n/a    516            386       87.2     47.5      16.5
float_to_string         ns    205        447        451     n/a    865            896      410      126         0.339
float_toString          ns    210        449        446     n/a   1079           1005      420      132        90.4
float_toFixed           ns    362       1605       1560     n/a    714            692      189      234       180
float_toPrecision       ns    345        590        616     n/a    813            763      228      175       127
float_toExponential     ns    329        552        549     n/a    774            752      231      192       130
string_to_int           ns     18.4      113        114     n/a    150            159       56.7     14.1       3.47
string_to_float         ns     53.3      152        148     n/a    183            179       69.5     31.2      22.0
bigint32_arith          µs     20.2      160        162     n/a     84.8          102       21.0     20.5       3.53
bigint64_arith          µs     55.1      166        167     n/a    114            165       25.7     22.8      20.4
bigint256_arith         µs    197        176        173     n/a    214            295       58.2     36.0      22.1
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
| date | 2026-11-20T03:44:34+09:00 | 2026-11-20T03:56:52+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261119181810-eb2d4fe68006 | v0.0.0-20261119181810-eb2d4fe68006 |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 7.61 3.98 3.36 | 4.22 4.33 3.92 |
| load-end | 4.22 4.33 3.92 | 3.90 3.85 3.80 |
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

## CPU profiling

A separate Ekuma profile of the V8 suite was captured on 2026-11-20 at `eb2d4fe68006`,
on Apple M3 Pro, macOS 27.2, and go1.27.4, with a 1-second benchmark duration.
`(*VM).exec` accounted for 46.0% of flat CPU samples and 63.1% cumulatively. See the
[profile summary](results/darwin_arm64/Ekuma-V8-cpu.txt). This profile covers the entire test
process, including parsing, setup, benchmark calibration, and garbage collection. Its
samples are not the timed operations alone, and do not contribute to the comparison tables.

For setup requirements, see [Running](#running). To capture another profile, run
the following commands from `_bench`:

```sh
CGO_CXXFLAGS="$(go env CGO_CXXFLAGS) -nostdinc++" go test -run '^$' \
  -bench '^BenchmarkV8$/./^ekuma$' -benchtime 1s \
  -cpuprofile /tmp/ekuma-v8-cpu.pprof -o /tmp/ekuma-v8-profile.test
go tool pprof -top /tmp/ekuma-v8-profile.test /tmp/ekuma-v8-cpu.pprof
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
go test -c -o /tmp/ekuma.test . && \
  go tool objdump -s 'engine\.\(\*VM\)\.exec$' /tmp/ekuma.test
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
