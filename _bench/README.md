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

Engine versions were checked on 2026-11-17 before measurement: goja and Sobek use their
latest upstream commits, and the other engines use their latest releases. The summary in
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
  (lower is faster), and a suite's row in the summary is the geometric mean of these over the
  benchmarks that the engine ran.
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
Ekuma                 1.00         1.00          v0.0.0-20261117123730-0439f78112fd
goja                  4.73         3.75          v0.0.0-20261117122431-0802ca125248
Sobek                 4.76         3.76          v0.0.0-20261116133901-d070e0f1f200
otto                 23.0 (9/10)    n/a          v0.5.1
modernc.org/quickjs   3.47         2.63          v0.27.0
QJS                   4.60 (6/10)  4.16 (69/74)  v0.0.6
QuickJS (C)           1.07         0.703         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.586        0.480         v0.44.0
v8go                  0.0301       0.0900        v0.44.0
```

Measured on 2026-11-17: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                         modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja  Sobek  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00   3.15  3.20   18.7   2.75           2.65  0.790    0.584      0.0216
DeltaBlue     1.00   3.10  2.93   25.2   2.99           2.78  0.862    0.620      0.00779
Encrypt       1.00   6.64  6.63   27.9   3.74            n/a  1.02     0.879      0.0205
Decrypt       1.00   6.73  6.75   28.2   3.83            n/a  1.03     0.909      0.0194
RayTrace      1.00   6.77  6.96   22.7   4.44           5.57  1.18     0.638      0.0206
Earley        1.00   3.75  3.84   22.1   3.57            n/a  1.02     0.404      0.0258
Boyer         1.00   3.64  3.69   20.5   3.41            n/a  0.768    0.422      0.0278
RegExp        1.00   6.52  6.57     n/a  6.82          17.0   4.26     0.355      0.131
Splay         1.00   2.73  2.78   13.5   2.20           2.64  0.752    0.412      0.0759
NavierStokes  1.00   7.83  7.92   34.7   2.65           5.11  0.939    1.04       0.0614
Geomean       1.00   4.73  4.76   23.0   3.47           4.60  1.07     0.586      0.0301
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     2.89    9.11    9.16    54.5     8.02          7.57    2.28    1.69      0.0625
DeltaBlue     ms     4.81   14.4    15.3    116      14.6          13.3     4.13    2.98      0.0373
Encrypt       ms     3.59   24.1    24.1    101      13.6            n/a    3.70    3.18      0.0738
Decrypt       ms    66.5   450     449     1886     255              n/a   68.6    60.0       1.28
RayTrace      ms    17.5   119     119      398      77.3          97.4    20.3    11.0       0.362
Earley        ms     3.76   14.4    14.6     84.1    13.5            n/a    3.84    1.53      0.0967
Boyer         ms    72.1   263     267     1479     244              n/a   55.1    30.4       2.02
RegExp        ms    33.0   220     217         n/a  224           576     141      11.8       4.32
Splay         ms     1.50    4.10    4.16    20.4     3.27          3.99    1.14    0.616     0.114
NavierStokes  ms    47.7   371     364     1592     123           250      43.2    48.0       2.92
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                       modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja     Sobek    otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00     6.03     6.18   n/a    2.22          3.12  0.610    0.854      0.0808
empty_down_loop         1.00     5.72     5.80   n/a    2.04          2.97  0.604    0.760      0.0798
empty_down_loop2        1.00     5.10     5.20   n/a    2.10          3.70  0.603    0.730      0.0605
empty_do_loop           1.00     4.80     4.88   n/a    1.83          2.21  0.617    0.582      0.0598
date_now                1.00     2.20     2.18   n/a    1.79          2.69  0.416    0.562      0.460
date_parse              1.00     1.65     1.69   n/a    5.40          4.15  1.38     2.04       1.91
prop_read               1.00     6.88     6.84   n/a    3.14          4.38  0.993    0.774      0.0414
prop_write              1.00     8.18     7.93   n/a    3.21          5.19  0.881    0.888      0.0306
prop_update             1.00     6.56     6.68   n/a    2.42          4.87  0.630    0.939      0.0375
prop_create             1.00     3.95     3.94   n/a    2.88          4.82  0.977    0.394      0.160
prop_clone              1.00     4.20     4.19   n/a    3.48          2.80  0.956    0.0333     0.0260
prop_delete             1.00     2.17     2.32   n/a    1.96          1.62  0.493    0.226      0.189
array_read              1.00     7.04     7.13   n/a    3.02          5.09  1.09     1.32       0.0842
array_write             1.00     3.73     3.74   n/a    2.23         10.0   0.526    1.02       0.0220
array_update            1.00     4.08     3.92   n/a    1.41          4.34  0.453    0.937      0.0234
array_prop_create       1.00     5.83     6.11   n/a    1.37          4.95  0.651    0.712      0.0978
array_slice             1.00    13.1     12.7    n/a   20.8           9.47  3.67     0.0496     0.0296
array_length_read       1.00     6.97     6.91   n/a    3.12          4.47  1.09     0.879      0.105
array_length_decr       1.00     3.99     3.93   n/a    3.86          3.02  0.751    0.873      0.625
array_hole_length_decr  1.00     3.88     3.82   n/a    4.76          4.89  1.63     0.854      0.580
array_push              1.00     4.44     4.50   n/a    2.42          3.20  0.574    0.378      0.0469
array_pop               1.00     3.87     3.80   n/a    7.16          3.72  1.10     0.466      0.0667
typed_array_read        1.00     3.61     3.56   n/a    2.94          2.53  0.556    0.766      0.0430
typed_array_write       1.00     3.61     3.61   n/a    2.08          5.89  0.522    0.630      0.0128
arguments_read          1.00     6.90     6.86   n/a    3.06           n/a  0.571    0.431      0.0669
arguments_strict_read   1.00     6.70     6.64   n/a    2.36           n/a  0.496    0.426      0.0676
global_read             1.00     6.63     6.10   n/a    2.14          3.86  0.975    0.630      0.0254
global_write            1.00     8.35     7.83   n/a    2.57          5.00  0.513    0.564      0.0304
global_write_strict     1.00    11.1     10.5    n/a    2.63          5.24  0.537    0.574      0.0312
local_destruct          1.00     4.50     4.44   n/a    7.19         23.4   1.13     1.11       0.0432
global_destruct         1.00     6.93     6.93   n/a    7.34          8.82  1.17     1.28       0.0870
global_destruct_strict  1.00     8.14     7.97   n/a    7.24          8.67  1.13     1.29       0.0867
global_func_call        1.00     2.49     2.45   n/a    2.38          1.95  0.492    0.577      0.00422
func_call               1.00     2.05     2.09   n/a    2.39          1.55  0.499    0.611      0.00395
func_closure_call       1.00     1.85     1.87   n/a    2.14          1.46  0.488    0.516      0.0732
int_arith               1.00     6.76     6.78   n/a    2.53          3.47  0.792    0.828      0.0291
float_arith             1.00     9.28     9.21   n/a    2.46          4.13  0.859    1.19       0.0500
map_set_string          1.00     1.38     1.48   n/a    1.96          1.66  0.473    0.203      0.0832
map_set_int             1.00     1.87     1.86   n/a    2.17         35.0   0.585    0.423      0.101
map_set_bigint          1.00     2.18     2.36   n/a    2.67         52.1   0.475    0.642      0.406
map_delete              1.00     1.27     1.22   n/a    1.49           n/a  0.383    0.172      0.0749
weak_map_set            1.00   694      886      n/a    1.35           n/a  0.289    0.245      0.114
weak_map_delete         1.00    12.9     13.7    n/a    1.92           n/a  0.440    0.338      0.144
array_for               1.00     5.88     6.05   n/a    2.60          3.92  0.919    1.05       0.0863
array_for_in            1.00    12.7     12.7    n/a   10.1           6.98  2.03     0.852      0.682
array_for_of            1.00     5.99     6.10   n/a    9.60          4.73  1.30     2.17       0.265
math_min                1.00     3.08     3.11   n/a    2.66          2.74  0.466    0.489      0.00790
regexp_ascii            1.00     3.95     4.06   n/a    1.78          4.92  0.539    0.221      0.143
regexp_utf16            1.00     3.88     3.59   n/a    1.64          4.59  0.549    0.203      0.134
regexp_replace          1.00     2.19     2.20   n/a    1.74         13.4   1.60     0.238      0.170
string_length           1.00     8.19     8.41   n/a    3.33          2.71  0.666    0.665      0.0670
string_build1           1.00     1.42     1.39   n/a    0.689         2.06  0.246    0.226      0.0435
string_build1x          1.00     1.42     1.41   n/a    0.717         2.13  0.244    0.211      0.0451
string_build2c          1.00     1.43     1.41   n/a    0.738         2.60  0.303    0.241      0.0450
string_build2           1.00     1.44     1.48   n/a    1.84          2.37  0.741    0.216      0.0456
string_build3           1.00     1.48     1.46   n/a    1.78          2.12  0.483    0.218      0.0415
string_build4           1.00     1.49     1.48   n/a    2.03          2.39  0.770    0.213      0.0444
string_build_large1     1.00     1.47     1.46   n/a    2.20         26.3   0.703    0.213      0.145
string_build_large2     1.00     1.48     1.47   n/a    2.01         23.4   0.618    0.215      0.122
string_concat           1.00     2.04     2.07   n/a    3.08          5.17  0.870    0.273      0.241
string_array_join       1.00     1.76     1.76   n/a    2.60          2.26  0.780    0.303      0.286
string_template         1.00     1.90     2.01   n/a    4.24          8.05  1.03     0.721      0.523
int_to_string           1.00     1.55     1.55   n/a    2.02          2.04  0.468    0.273      0.0758
int_toString            1.00     2.22     2.22   n/a    3.07          2.39  0.545    0.302      0.106
float_to_string         1.00     1.69     1.68   n/a    3.20          3.32  1.51     0.474      0.316
float_toString          1.00     1.58     1.55   n/a    3.74          3.53  1.45     0.457      0.315
float_toFixed           1.00     3.75     3.67   n/a    1.69          1.70  0.463    0.582      0.436
float_toPrecision       1.00     1.42     1.40   n/a    1.90          1.87  0.549    0.427      0.300
float_toExponential     1.00     1.38     1.38   n/a    1.94          1.86  0.570    0.434      0.308
string_to_int           1.00     5.18     5.31   n/a    7.09          7.36  2.65     0.656      0.163
string_to_float         1.00     3.36     3.35   n/a    4.15          3.99  1.57     0.693      0.459
bigint32_arith          1.00     7.66     7.63   n/a    3.99          4.67  0.840    0.970      0.161
bigint64_arith          1.00     3.51     3.49   n/a    2.47          3.70  0.550    0.500      0.444
bigint256_arith         1.00     0.906    0.859  n/a    1.08          1.47  0.296    0.185      0.114
Geomean                 1.00     3.75     3.76   n/a    2.63          4.16  0.703    0.480      0.0900
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                   modernc.org/            QuickJS  v8go
Benchmark               Unit  Ekuma    goja       Sobek      otto  quickjs       QJS       (C)      --jitless  v8go
empty_loop              ns       6.56      39.3       40.3   n/a     14.5           20.3     4.00     5.62       0.531
empty_down_loop         ns       6.68      38.2       38.5   n/a     13.5           19.8     4.06     5.15       0.536
empty_down_loop2        ns       8.86      45.4       45.8   n/a     18.6           32.5     5.34     6.47       0.536
empty_do_loop           ns       8.93      42.9       43.1   n/a     16.2           19.8     5.45     5.14       0.530
date_now                ns      53.5      125        122     n/a     95.8          145      22.2     30.5       24.5
date_parse              µs       2.06       3.28       3.48  n/a     11.1            8.53    2.84     4.05       3.90
prop_read               ns      27.1      188        186     n/a     85.8          133      28.5     21.5        1.14
prop_write              ns      18.4      151        138     n/a     58.8           95.3    16.3     16.2        0.535
prop_update             ns      43.0      282        287     n/a    104            209      27.1     40.4        1.61
prop_create             ns     574       2268       2263     n/a   1702           2729     566      227         91.8
prop_clone              ns     793       3330       3293     n/a   2798           2259     766       27.6       20.6
prop_delete             µs       2.21       4.79       5.12  n/a      4.33           3.57    1.10     0.498      0.417
array_read              ns      55.4      417        459     n/a    167            283      61.3     73.5        5.20
array_write             ns      60.3      224        230     n/a    138            637      32.7     61.5        1.34
array_update            ns     146        595        570     n/a    205            623      68.8    139          3.63
array_prop_create       µs      19.1      111        124     n/a     26.5           94.7    12.4     14.5        1.87
array_slice             µs       2.74      35.9       36.5   n/a     61.9           26.4    10.4      0.134      0.0827
array_length_read       ns      25.7      179        178     n/a     80.2          115      27.8     22.4        2.71
array_length_decr       µs      29.9      113        111     n/a    116             86.1    21.3     26.1       18.4
array_hole_length_decr  µs      31.8      119        120     n/a    151            146      51.9     26.8       18.3
array_push              µs      19.5       86.3       87.8   n/a     47.1           62.7    11.2      7.41       0.914
array_pop               µs      17.8       69.0       68.2   n/a    127             68.3    20.8      8.61       1.21
typed_array_read        ns     111        403        398     n/a    329            282      63.3     85.2        4.82
typed_array_write       ns     104        376        384     n/a    217            612      56.8     66.2        1.33
arguments_read          ns     474       3208       3189     n/a   1454               n/a  272      203         31.9
arguments_strict_read   ns     470       3113       3121     n/a   1105               n/a  233      200         31.6
global_read             ns      26.9      175        166     n/a     56.8          103      26.2     16.9        0.682
global_write            ns      27.4      237        222     n/a     68.6          138      14.1     15.3        0.817
global_write_strict     ns      26.3      293        276     n/a     70.7          137      14.2     15.1        0.841
local_destruct          ns     164        741        730     n/a   1182           3797     186      181          7.21
global_destruct         ns     148       1030       1031     n/a   1067           1283     171      189         12.7
global_destruct_strict  ns     147       1197       1185     n/a   1060           1269     165      189         12.8
global_func_call        ns     117        289        285     n/a    278            226      57.4     66.8        0.493
func_call               ns     116        237        244     n/a    272            179      57.1     70.6        0.453
func_closure_call       ns     129        238        240     n/a    281            189      62.9     66.3        9.41
int_arith               µs      11.8       78.6       77.6   n/a     29.9           41.2     9.34     9.62       0.338
float_arith             µs      14.7      134        137     n/a     35.5           60.4    12.7     17.2        0.737
map_set_string          µs     280        391        413     n/a    549            464     132       56.7       23.3
map_set_int             µs     135        263        250     n/a    293           4711      78.8     56.9       14.3
map_set_bigint          µs     177        381        414     n/a    472           9218      84.1    113         71.3
map_delete              µs     338        439        424     n/a    509               n/a  137       58.2       25.4
weak_map_set            µs     170     110044     126580     n/a    229               n/a   46.1     40.1       18.4
weak_map_delete         µs     242       3291       3482     n/a    487               n/a  108       83.1       34.9
array_for               µs       1.19       7.00       7.21  n/a      3.10           4.67    1.09     1.23       0.103
array_for_in            µs       1.72      22.0       22.1   n/a     18.5           12.1     3.76     1.49       1.22
array_for_of            µs       1.07       6.45       6.43  n/a     10.0            5.07    1.36     2.32       0.287
math_min                µs      34.3      107        108     n/a     92.8           95.3    16.2     17.0        0.272
regexp_ascii            µs     205        802        831     n/a    366           1008     113       45.3       28.9
regexp_utf16            µs     235        902        838     n/a    384           1077     131       48.2       31.8
regexp_replace          µs     376        830        814     n/a    653           5053     599       89.7       65.3
string_length           ns      39.8      326        334     n/a    135            108      26.5     26.4        2.65
string_build1           µs      51.8       74.6       74.1   n/a     36.8          110      12.8     11.7        2.25
string_build1x          µs      50.3       71.3       69.9   n/a     36.1          108      12.1     10.6        2.29
string_build2c          µs      48.6       69.8       69.2   n/a     36.8          126      14.7     11.7        2.22
string_build2           µs      50.2       72.0       74.0   n/a     92.4          119      37.2     10.8        2.28
string_build3           µs      54.1       80.0       79.3   n/a     96.0          114      26.1     11.7        2.23
string_build4           µs      50.7       75.9       75.5   n/a    103            122      39.0     10.8        2.25
string_build_large1     µs    1000       1501       1458     n/a   2220          26865     704      218        149
string_build_large2     ms       1.16       1.77       1.73  n/a      2.36          27.4     0.716    0.249      0.146
string_concat           ns     694       1407       1410     n/a   2134           3530     605      189        168
string_array_join       ns     777       1370       1354     n/a   2009           1784     606      235        222
string_template         ns     339        644        689     n/a   1460           2788     358      245        193
int_to_string           ns     134        211        209     n/a    271            276      62.9     38.7       10.2
int_toString            ns     157        347        348     n/a    488            378      87.9     47.2       16.6
float_to_string         ns     269        447        446     n/a    850            892     404      127         84.8
float_toString          ns     285        450        446     n/a   1082           1007     418      130         90.1
float_toFixed           ns     417       1522       1529     n/a    704            710     193      234        178
float_toPrecision       ns     403        572        570     n/a    764            752     221      172        123
float_toExponential     ns     401        543        542     n/a    777            736     225      182        122
string_to_int           ns      21.2      110        112     n/a    150            156      56.1     13.9        3.45
string_to_float         ns      44.0      148        147     n/a    181            176      69.2     30.4       20.1
bigint32_arith          µs      21.1      163        163     n/a     85.4          100      17.7     20.5        3.38
bigint64_arith          µs      46.1      162        162     n/a    114            171      25.3     22.8       20.5
bigint256_arith         µs     196        176        171     n/a    211            289      58.1     36.0       22.3
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
| date | 2026-11-17T21:40:48+09:00 | 2026-11-17T21:53:21+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261117123730-0439f78112fd | v0.0.0-20261117123730-0439f78112fd |
| github.com/dop251/goja | v0.0.0-20261117122431-0802ca125248 | v0.0.0-20261117122431-0802ca125248 |
| github.com/grafana/sobek | v0.0.0-20261116133901-d070e0f1f200 | v0.0.0-20261116133901-d070e0f1f200 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 8.57 5.22 4.60 | 4.26 4.21 4.34 |
| load-end | 4.26 4.21 4.34 | 3.54 5.07 4.86 |
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
