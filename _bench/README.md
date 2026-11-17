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

The comparison uses the dependency versions checked on 2026-11-17, when goja and Sobek were
at their latest upstream commits and the other engines at their latest releases. These
versions were kept for the 2026-11-18 measurement of the current Ekuma checkout. The summary in
[Results](#results) gives the version of each engine, and [Environment](#environment) the rest
of the setup.

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
- The machine was not idle: the load averages are in [Environment](#environment). The medians
  reduce the influence of unusually slow rounds. A difference of less than about 20% between
  two engines is within the noise.

## Results

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261117164559-24a1e5134647
goja                  4.82         4.05          v0.0.0-20261117122431-0802ca125248
Sobek                 4.82         4.08          v0.0.0-20261116133901-d070e0f1f200
otto                 23.2 (9/10)    n/a          v0.5.1
modernc.org/quickjs   3.52         2.82          v0.27.0
QJS                   4.69 (6/10)  4.48 (69/74)  v0.0.6
QuickJS (C)           1.08         0.755         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.596        0.512         v0.44.0
v8go                  0.0306       0.0987        v0.44.0
```

Measured on 2026-11-18: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                         modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja  Sobek  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00   3.20  3.21   18.9   2.76           2.67  0.798    0.617      0.0230
DeltaBlue     1.00   3.16  3.15   24.8   3.21           2.92  0.908    0.666      0.00798
Encrypt       1.00   6.91  6.82   28.4   3.88            n/a  1.05     0.915      0.0211
Decrypt       1.00   6.91  6.90   29.2   3.91            n/a  1.05     0.918      0.0197
RayTrace      1.00   6.55  6.57   22.1   4.23           5.33  1.10     0.595      0.0197
Earley        1.00   3.76  3.82   21.8   3.52            n/a  1.01     0.397      0.0251
Boyer         1.00   3.52  3.53   19.9   3.27            n/a  0.741    0.409      0.0279
RegExp        1.00   6.81  6.82     n/a  7.02          17.7   4.42     0.364      0.136
Splay         1.00   2.87  2.92   14.6   2.28           2.74  0.748    0.430      0.0785
NavierStokes  1.00   8.22  8.09   35.5   2.75           5.31  0.970    1.08       0.0652
Geomean       1.00   4.82  4.82   23.2   3.52           4.69  1.08     0.596      0.0306
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     2.83    9.10    9.09    53.7     7.86          7.59    2.27    1.75      0.0650
DeltaBlue     ms     4.53   14.4    14.3    115      14.6          13.2     4.11    3.02      0.0372
Encrypt       ms     3.49   24.1    24.1     99.7    13.6            n/a    3.66    3.17      0.0736
Decrypt       ms    65.2   450     449     1894     255              n/a   68.4    59.8       1.28
RayTrace      ms    18.4   121     121      407      77.9          97.9    20.4    10.9       0.363
Earley        ms     3.83   14.5    14.6     83.6    13.5            n/a    3.85    1.53      0.0963
Boyer         ms    74.4   262     263     1483     244              n/a   55.1    30.5       2.07
RegExp        ms    31.9   218     218         n/a  224           565     141      11.7       4.33
Splay         ms     1.42    4.10    4.17    20.5     3.26          3.91    1.07    0.614     0.112
NavierStokes  ms    44.6   367     361     1589     123           237      43.4    48.2       2.91
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                       modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja     Sobek    otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00      6.20     6.19  n/a    2.23          3.25  0.649    0.901      0.0858
empty_down_loop         1.00      6.27     6.33  n/a    2.22          3.24  0.667    0.830      0.0876
empty_down_loop2        1.00      5.53     5.55  n/a    2.23          3.89  0.634    0.734      0.0635
empty_do_loop           1.00      5.14     5.18  n/a    1.90          2.35  0.636    0.604      0.0633
date_now                1.00      2.15     2.12  n/a    1.69          2.66  0.410    0.546      0.439
date_parse              1.00      1.68     1.68  n/a    5.30          4.30  1.41     2.05       1.93
prop_read               1.00      7.39     7.46  n/a    3.42          4.82  1.07     0.826      0.0455
prop_write              1.00      8.61     8.69  n/a    3.59          5.80  1.01     1.01       0.0331
prop_update             1.00      7.44     7.46  n/a    2.78          5.71  0.747    1.12       0.0436
prop_create             1.00      4.14     4.12  n/a    2.99          4.88  0.996    0.406      0.162
prop_clone              1.00      4.15     4.16  n/a    3.42          2.80  0.953    0.0336     0.0258
prop_delete             1.00      2.19     2.21  n/a    1.94          1.61  0.502    0.224      0.191
array_read              1.00      7.72     7.61  n/a    3.25          5.58  1.20     1.48       0.0872
array_write             1.00      3.84     3.89  n/a    2.29         10.4   0.536    1.05       0.0227
array_update            1.00      4.31     4.27  n/a    1.59          4.56  0.485    0.981      0.0288
array_prop_create       1.00      5.66     5.62  n/a    1.39          4.77  0.610    0.712      0.0986
array_slice             1.00     11.7     11.7   n/a   18.3           8.43  3.28     0.0383     0.0278
array_length_read       1.00      7.33     7.49  n/a    3.31          4.72  1.10     0.897      0.112
array_length_decr       1.00      3.89     3.88  n/a    4.20          3.12  0.783    0.959      0.649
array_hole_length_decr  1.00      4.13     4.25  n/a    5.14          4.66  1.53     0.878      0.599
array_push              1.00      4.33     4.44  n/a    2.40          3.20  0.578    0.376      0.0471
array_pop               1.00      3.92     3.95  n/a    7.24          3.78  1.08     0.469      0.0681
typed_array_read        1.00      3.57     3.55  n/a    2.88          2.47  0.553    0.747      0.0408
typed_array_write       1.00      3.52     3.55  n/a    1.93          5.70  0.508    0.614      0.0123
arguments_read          1.00      6.42     6.47  n/a    2.96           n/a  0.553    0.400      0.0630
arguments_strict_read   1.00      6.12     6.19  n/a    2.20           n/a  0.461    0.380      0.0519
global_read             1.00      6.80     7.03  n/a    2.28          4.12  1.06     0.664      0.0280
global_write            1.00      9.81     8.51  n/a    2.76          5.49  0.570    0.601      0.0320
global_write_strict     1.00     12.3     11.4   n/a    2.71          5.45  0.563    0.597      0.0317
local_destruct          1.00      4.61     4.59  n/a    7.39         23.8   1.17     1.13       0.0435
global_destruct         1.00      7.08     7.31  n/a    7.47          8.77  1.15     1.31       0.0886
global_destruct_strict  1.00      8.49     8.29  n/a    7.48          9.20  1.17     1.36       0.0887
global_func_call        1.00      2.84     2.78  n/a    2.72          2.15  0.545    0.657      0.00443
func_call               1.00      2.28     2.27  n/a    2.75          1.71  0.536    0.620      0.00428
func_closure_call       1.00      2.05     2.04  n/a    2.46          1.63  0.533    0.532      0.0808
int_arith               1.00      7.21     7.27  n/a    2.72          3.86  0.883    0.902      0.0315
float_arith             1.00      9.80     9.95  n/a    2.62          4.44  0.936    1.30       0.0542
map_set_string          1.00      1.55     1.53  n/a    1.99          1.82  0.508    0.221      0.0820
map_set_int             1.00      1.88     1.90  n/a    2.22         34.3   0.574    0.410      0.105
map_set_bigint          1.00      2.25     2.25  n/a    2.72         50.2   0.482    0.629      0.412
map_delete              1.00      1.41     1.40  n/a    1.67           n/a  0.419    0.192      0.0814
weak_map_set            1.00   1018     1112     n/a    1.57           n/a  0.319    0.283      0.131
weak_map_delete         1.00     12.5     14.1   n/a    1.92           n/a  0.440    0.339      0.142
array_for               1.00      6.26     6.30  n/a    2.78          4.15  0.967    1.09       0.0900
array_for_in            1.00     13.7     13.6   n/a   10.9           7.45  2.11     0.920      0.729
array_for_of            1.00      6.29     6.42  n/a   10.6           5.13  1.46     2.48       0.299
math_min                1.00      3.20     3.28  n/a    2.44          2.53  0.477    0.501      0.00810
regexp_ascii            1.00      4.43     4.37  n/a    1.89          5.20  0.570    0.233      0.154
regexp_utf16            1.00      3.98     3.89  n/a    1.71          4.85  0.582    0.210      0.141
regexp_replace          1.00      2.27     2.27  n/a    1.85         14.0   1.67     0.218      0.171
string_length           1.00      8.49     8.40  n/a    3.37          2.84  0.699    0.697      0.0505
string_build1           1.00      1.88     1.90  n/a    0.953         2.89  0.322    0.296      0.0576
string_build1x          1.00      1.91     1.92  n/a    0.965         2.96  0.331    0.286      0.0611
string_build2c          1.00      1.89     1.89  n/a    0.968         3.33  0.376    0.280      0.0601
string_build2           1.00      1.92     1.94  n/a    2.47          3.20  1.01     0.298      0.0607
string_build3           1.00      1.86     2.43  n/a    2.34          2.72  0.615    0.277      0.0532
string_build4           1.00      1.96     1.98  n/a    2.66          3.11  1.01     0.281      0.0578
string_build_large1     1.00      1.91     1.89  n/a    2.82         34.3   0.899    0.278      0.188
string_build_large2     1.00      1.82     1.84  n/a    2.52         28.9   0.752    0.262      0.157
string_concat           1.00      2.04     2.07  n/a    3.05          4.99  0.956    0.270      0.239
string_array_join       1.00      1.75     1.73  n/a    2.55          2.24  0.769    0.306      0.284
string_template         1.00      1.86     1.89  n/a    4.20          7.99  1.01     0.698      0.554
int_to_string           1.00      1.85     1.86  n/a    2.34          2.41  0.549    0.328      0.0916
int_toString            1.00      2.47     2.48  n/a    3.42          2.67  0.607    0.339      0.115
float_to_string         1.00      1.99     1.97  n/a    3.78          3.93  1.80     0.555      0.375
float_toString          1.00      1.76     1.78  n/a    4.24          3.94  1.64     0.527      0.357
float_toFixed           1.00      4.26     4.27  n/a    1.97          1.97  0.535    0.657      0.491
float_toPrecision       1.00      1.56     1.53  n/a    2.05          1.99  0.593    0.468      0.328
float_toExponential     1.00      1.56     1.56  n/a    2.19          2.09  0.642    0.495      0.346
string_to_int           1.00      5.50     5.66  n/a    7.50          7.92  2.83     0.694      0.175
string_to_float         1.00      3.42     3.43  n/a    4.18          4.07  1.59     0.703      0.508
bigint32_arith          1.00      8.08     8.05  n/a    4.21          5.00  0.906    1.01       1.02
bigint64_arith          1.00      3.59     3.58  n/a    2.48          3.77  0.555    0.498      0.457
bigint256_arith         1.00      1.04     1.03  n/a    1.24          1.65  0.355    0.214      0.131
Geomean                 1.00      4.05     4.08  n/a    2.82          4.48  0.755    0.512      0.0987
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS  v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)      --jitless  v8go
empty_loop              ns      6.30      39.1       39.0   n/a     14.0           20.4      4.08     5.65      0.538
empty_down_loop         ns      6.16      38.6       39.0   n/a     13.7           20.1      4.11     5.12      0.538
empty_down_loop2        ns      8.45      46.7       47.0   n/a     18.8           32.9      5.37     6.22      0.537
empty_do_loop           ns      8.47      43.6       43.9   n/a     16.1           19.9      5.39     5.12      0.537
date_now                ns     54.6      117        116     n/a     92.6          146       22.3     29.7      23.8
date_parse              µs      1.97       3.31       3.31  n/a     10.4            8.44     2.77     4.02      3.79
prop_read               ns     25.0      184        186     n/a     85.4          120       26.8     20.6       1.14
prop_write              ns     16.3      140        142     n/a     58.6           94.8     16.4     16.5       0.542
prop_update             ns     36.0      268        269     n/a    100            205       26.9     40.4       1.57
prop_create             ns    564       2353       2314     n/a   1679           2751      569      229        91.5
prop_clone              ns    803       3312       3382     n/a   2742           2251      776       26.6      20.5
prop_delete             µs      2.24       4.89       4.95  n/a      4.33           3.60     1.12     0.501     0.425
array_read              ns     51.8      400        392     n/a    168            287       61.5     76.5       4.49
array_write             ns     59.0      228        230     n/a    136            612       31.7     61.8       1.34
array_update            ns    130        558        554     n/a    206            596       62.8    127         3.72
array_prop_create       µs     19.2      108        107     n/a     26.6           91.6     11.7     13.6       1.90
array_slice             µs      3.12      36.3       36.4   n/a     57.1           26.3     10.6      0.119     0.0865
array_length_read       ns     24.1      176        180     n/a     79.9          114       26.5     21.6       2.70
array_length_decr       µs     26.8      106        104     n/a    114             83.6     21.0     25.7      17.4
array_hole_length_decr  µs     29.5      121        125     n/a    151            137       44.9     25.8      17.6
array_push              µs     20.0       92.7       89.7   n/a     49.0           63.5     11.6      7.45      0.932
array_pop               µs     17.9       70.1       69.9   n/a    131             67.3     19.6      8.46      1.21
typed_array_read        ns    114        408        405     n/a    332            283       63.1     85.4       4.69
typed_array_write       ns    109        382        386     n/a    210            619       55.3     66.7       1.34
arguments_read          ns    504       3258       3257     n/a   1489               n/a   278      200        32.1
arguments_strict_read   ns    507       3100       3155     n/a   1137               n/a   234      197        31.9
global_read             ns     24.8      170        178     n/a     56.5          102       26.4     16.5       0.694
global_write            ns     25.3      247        214     n/a     69.6          140       14.6     15.2       0.806
global_write_strict     ns     25.5      320        297     n/a     70.0          139       14.4     15.4       0.816
local_destruct          ns    162        750        744     n/a   1198           3858      190      183         7.06
global_destruct         ns    147       1026       1077     n/a   1089           1295      170      192        12.9
global_destruct_strict  ns    144       1253       1216     n/a   1084           1319      170      200        12.8
global_func_call        ns    107        305        296     n/a    290            228       58.3     70.5       0.468
func_call               ns    106        240        239     n/a    289            182       57.1     66.8       0.451
func_closure_call       ns    117        241        239     n/a    287            190       62.5     62.3       9.59
int_arith               µs     10.8       78.5       78.1   n/a     29.4           41.8      9.52     9.76      0.338
float_arith             µs     13.1      128        130     n/a     34.6           58.5     12.2     17.1       0.709
map_set_string          µs    256        395        392     n/a    510            466      131       56.7      20.9
map_set_int             µs    134        253        255     n/a    296           4584       77.3     54.9      14.0
map_set_bigint          µs    173        390        388     n/a    471           8705       83.6    109        71.4
map_delete              µs    306        428        423     n/a    509               n/a   128       58.3      24.9
weak_map_set            µs    139     153845     154915     n/a    220               n/a    46.0     39.4      18.5
weak_map_delete         µs    244       3105       3566     n/a    469               n/a   108       82.9      34.8
array_for               µs      1.12       7.03       7.08  n/a      3.12           4.68     1.09     1.23      0.101
array_for_in            µs      1.62      22.1       21.8   n/a     17.5           11.9      3.38     1.48      1.18
array_for_of            ns    931       5879       5967     n/a   9943           4778     1353     2300       278
math_min                µs     33.5      108        110     n/a     81.2           84.6     16.0     16.8       0.272
regexp_ascii            µs    194        859        821     n/a    363           1008      110       45.1      29.3
regexp_utf16            µs    219        866        850     n/a    375           1060      128       45.7      31.0
regexp_replace          µs    358        813        821     n/a    664           5056      601       78.2      61.1
string_length           ns     38.3      324        320     n/a    129            109       26.7     26.6       1.93
string_build1           µs     38.7       70.2       70.3   n/a     35.3          109       12.0     10.8       2.24
string_build1x          µs     36.6       69.8       70.2   n/a     35.6          108       12.0     10.5       2.22
string_build2c          µs     37.0       69.9       70.2   n/a     36.0          123       13.9     10.5       2.22
string_build2           µs     36.6       70.3       70.9   n/a     90.0          117       37.4     10.8       2.25
string_build3           µs     42.5       79.3      103     n/a     99.4          115       26.1     11.7       2.26
string_build4           µs     38.6       75.7       76.3   n/a    103            120       38.8     10.8       2.22
string_build_large1     µs    782       1470       1468     n/a   2205          26501      704      216       147
string_build_large2     µs    949       1727       1735     n/a   2427          27411      711      249       147
string_concat           ns    701       1418       1453     n/a   2130           3483      670      188       169
string_array_join       ns    782       1367       1356     n/a   1994           1773      605      240       223
string_template         ns    348        649        654     n/a   1463           2787      352      243       196
int_to_string           ns    114        211        212     n/a    266            276       62.5     37.5      10.4
int_toString            ns    140        347        347     n/a    481            375       85.6     47.6      16.3
float_to_string         ns    227        456        451     n/a    872            892      410      126        85.3
float_toString          ns    257        456        458     n/a   1082           1012      420      134        91.9
float_toFixed           ns    357       1518       1533     n/a    703            711      191      234       176
float_toPrecision       ns    372        581        571     n/a    772            743      226      177       125
float_toExponential     ns    354        552        555     n/a    770            745      225      178       124
string_to_int           ns     20.3      111        115     n/a    151            158       57.3     14.2       3.51
string_to_float         ns     43.7      150        150     n/a    184            178       70.1     30.9      22.3
bigint32_arith          µs     20.1      163        162     n/a     84.9          101       18.2     20.4      20.4
bigint64_arith          µs     45.3      164        164     n/a    113            170       25.1     22.5      20.7
bigint256_arith         µs    169        175        174     n/a    210            277       59.8     36.0      22.0
```

<!-- END jsbench Micro.time -->

### Missing results

In the 2026-11-18 run, Ekuma failed the V8 Decrypt correctness check in one of five rounds.
The Decrypt ratios use only the four successful paired rounds. The timings do not establish
that this workload runs reliably; the intermittent failure was unresolved at measurement time.

<!-- BEGIN jsbench missing -->

- Ekuma in V8 benchmark suite v7 (Decrypt): `1/5 rounds missing: Error: Crypto operation failed`
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
| date | 2026-11-18T02:16:43+09:00 | 2026-11-18T02:29:08+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261117164559-24a1e5134647 | v0.0.0-20261117164559-24a1e5134647 |
| github.com/dop251/goja | v0.0.0-20261117122431-0802ca125248 | v0.0.0-20261117122431-0802ca125248 |
| github.com/grafana/sobek | v0.0.0-20261116133901-d070e0f1f200 | v0.0.0-20261116133901-d070e0f1f200 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 3.49 3.55 3.64 | 3.78 4.77 4.51 |
| load-end | 3.78 4.77 4.51 | 4.08 4.23 4.36 |
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
