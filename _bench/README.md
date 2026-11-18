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
versions were kept for the 2026-11-19 measurement of the current Ekuma checkout. The summary in
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
Ekuma                 1.00         1.00          v0.0.0-20261118193413-b9d4e40e8453
goja                  4.80         4.03          v0.0.0-20261117122431-0802ca125248
Sobek                 4.75         4.00          v0.0.0-20261116133901-d070e0f1f200
otto                 23.0 (9/10)    n/a          v0.5.1
modernc.org/quickjs   3.49         2.84          v0.27.0
QJS                   4.64 (6/10)  4.46 (69/74)  v0.0.6
QuickJS (C)           1.07         0.754         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.596        0.511         v0.44.0
v8go                  0.0304       0.0983        v0.44.0
```

Measured on 2026-11-19: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

### V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                         modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja  Sobek  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00   3.21  3.16   19.1   2.76           2.65  0.787    0.593      0.0224
DeltaBlue     1.00   3.24  3.17   24.8   3.21           2.92  0.917    0.698      0.00831
Encrypt       1.00   6.90  6.89   28.6   3.88            n/a  1.05     0.910      0.0211
Decrypt       1.00   6.98  6.82   28.9   3.96            n/a  1.04     0.918      0.0196
RayTrace      1.00   6.47  6.40   21.4   4.11           5.21  1.09     0.589      0.0196
Earley        1.00   3.76  3.82   22.1   3.53            n/a  1.01     0.411      0.0256
Boyer         1.00   3.50  3.36   19.2   3.19            n/a  0.714    0.403      0.0263
RegExp        1.00   6.73  6.65     n/a  6.95          17.4   4.35     0.370      0.132
Splay         1.00   2.79  2.80   14.2   2.23           2.66  0.727    0.419      0.0760
NavierStokes  1.00   8.17  8.04   35.4   2.76           5.36  0.971    1.09       0.0656
Geomean       1.00   4.80  4.75   23.0   3.49           4.64  1.07     0.596      0.0304
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     2.85    9.17    9.05    54.4     7.85          7.60    2.25    1.69      0.0638
DeltaBlue     ms     4.50   14.5    14.3    114      14.6          13.2     4.13    3.21      0.0381
Encrypt       ms     3.49   24.0    24.0     99.8    13.5            n/a    3.66    3.21      0.0738
Decrypt       ms    65.4   457     445     1886     260              n/a   68.3    60.0       1.28
RayTrace      ms    18.6   120     119      405      77.2          97.5    20.4    10.9       0.363
Earley        ms     3.84   14.5    14.7     85.3    13.5            n/a    3.84    1.55      0.0989
Boyer         ms    77.4   266     262     1487     245              n/a   55.3    31.5       2.03
RegExp        ms    32.3   217     215         n/a  225           563     141      11.9       4.28
Splay         ms     1.46    4.14    4.08    20.8     3.26          3.91    1.06    0.613     0.111
NavierStokes  ms    44.8   366     359     1585     123           238      43.5    48.4       2.91
```

<!-- END jsbench V8.time -->

### QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                     modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja    Sobek   otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00     6.22    6.20  n/a    2.24          3.25  0.651    0.899      0.0858
empty_down_loop         1.00     6.23    6.22  n/a    2.20          3.25  0.661    0.823      0.0863
empty_down_loop2        1.00     5.38    5.39  n/a    2.26          3.86  0.629    0.727      0.0628
empty_do_loop           1.00     5.18    5.17  n/a    1.80          2.38  0.612    0.590      0.0610
date_now                1.00     2.13    2.06  n/a    1.60          2.59  0.398    0.535      0.434
date_parse              1.00     1.67    1.66  n/a    5.26          4.25  1.37     2.01       1.94
prop_read               1.00     7.46    7.42  n/a    3.45          4.73  1.07     0.837      0.0468
prop_write              1.00     8.56    8.54  n/a    3.60          5.74  1.02     1.26       0.0329
prop_update             1.00     7.69    7.65  n/a    2.83          5.78  0.745    1.10       0.0448
prop_create             1.00     3.93    3.93  n/a    2.97          4.97  1.00     0.398      0.159
prop_clone              1.00     4.16    4.55  n/a    3.48          2.87  0.965    0.0335     0.0256
prop_delete             1.00     2.18    2.17  n/a    1.95          1.62  0.502    0.226      0.183
array_read              1.00     7.46    7.47  n/a    3.30          5.78  1.17     1.43       0.0888
array_write             1.00     3.89    3.88  n/a    2.34         10.6   0.561    1.07       0.0232
array_update            1.00     4.36    4.36  n/a    1.63          4.61  0.496    1.01       0.0248
array_prop_create       1.00     5.77    5.55  n/a    1.34          4.53  0.587    0.692      0.0951
array_slice             1.00    11.9    11.9   n/a   18.5           8.60  3.35     0.0380     0.0282
array_length_read       1.00     7.27    7.22  n/a    3.36          4.66  1.07     0.877      0.117
array_length_decr       1.00     3.87    3.85  n/a    4.22          3.11  0.781    0.954      0.648
array_hole_length_decr  1.00     4.55    4.09  n/a    4.48          4.06  1.32     0.754      0.593
array_push              1.00     4.33    4.36  n/a    5.27          3.29  0.562    0.371      0.0461
array_pop               1.00     3.86    3.82  n/a    7.23          3.83  1.12     0.479      0.0674
typed_array_read        1.00     3.45    3.46  n/a    2.82          2.44  0.541    0.733      0.0404
typed_array_write       1.00     3.56    3.64  n/a    2.06          5.90  0.516    0.629      0.0126
arguments_read          1.00     6.38    6.41  n/a    2.90           n/a  0.538    0.400      0.0629
arguments_strict_read   1.00     6.18    6.19  n/a    1.99           n/a  0.419    0.350      0.0554
global_read             1.00     7.11    6.69  n/a    2.58          4.79  1.21     0.739      0.0279
global_write            1.00     9.16    8.90  n/a    2.71          5.39  0.555    0.590      0.0313
global_write_strict     1.00    10.5    12.2   n/a    2.68          5.43  0.559    0.595      0.0315
local_destruct          1.00     4.40    4.32  n/a    7.19         22.7   1.16     1.09       0.0413
global_destruct         1.00     6.87    6.75  n/a    7.17          8.49  1.11     1.28       0.0839
global_destruct_strict  1.00     8.29    8.05  n/a    7.22          8.90  1.13     1.30       0.0869
global_func_call        1.00     2.75    2.74  n/a    2.65          2.18  0.561    0.643      0.00497
func_call               1.00     2.24    2.27  n/a    2.60          1.66  0.532    0.613      0.00450
func_closure_call       1.00     2.01    2.05  n/a    2.37          1.63  0.517    0.539      0.0810
int_arith               1.00     7.12    7.04  n/a    2.73          3.85  0.873    0.911      0.0308
float_arith             1.00     9.71    9.62  n/a    2.59          4.36  0.911    1.32       0.0698
map_set_string          1.00     1.70    1.69  n/a    2.23          2.05  0.571    0.247      0.0937
map_set_int             1.00     1.89    1.89  n/a    2.20         34.9   0.567    0.420      0.105
map_set_bigint          1.00     2.21    2.25  n/a    2.63         49.3   0.466    0.631      0.404
map_delete              1.00     1.40    1.42  n/a    1.68           n/a  0.433    0.195      0.0833
weak_map_set            1.00   886     734     n/a    1.57           n/a  0.322    0.279      0.129
weak_map_delete         1.00    14.2    11.9   n/a    1.92           n/a  0.445    0.343      0.145
array_for               1.00     6.22    6.20  n/a    2.77          4.04  0.961    1.11       0.0909
array_for_in            1.00    11.5    11.1   n/a    8.94          6.15  1.91     0.767      0.650
array_for_of            1.00     6.41    6.36  n/a   11.3           5.19  1.47     2.46       0.296
math_min                1.00     3.22    3.14  n/a    2.34          2.51  0.475    0.496      0.00807
regexp_ascii            1.00     4.27    4.24  n/a    1.80          4.94  0.548    0.222      0.146
regexp_utf16            1.00     3.80    3.74  n/a    1.63          4.65  0.555    0.204      0.136
regexp_replace          1.00     2.34    2.30  n/a    1.85         14.5   1.67     0.227      0.171
string_length           1.00    10.5    10.8   n/a    4.27          3.51  0.871    0.858      0.0637
string_build1           1.00     1.69    1.68  n/a    0.863         2.61  0.288    0.264      0.0537
string_build1x          1.00     1.72    1.72  n/a    0.875         2.63  0.289    0.255      0.0533
string_build2c          1.00     1.69    1.67  n/a    0.871         2.98  0.344    0.253      0.0536
string_build2           1.00     1.72    1.72  n/a    2.25          2.91  0.933    0.266      0.0554
string_build3           1.00     1.70    1.68  n/a    2.04          2.44  0.557    0.247      0.0477
string_build4           1.00     1.76    1.74  n/a    2.37          2.77  0.897    0.245      0.0500
string_build_large1     1.00     1.69    1.65  n/a    2.48         29.3   0.793    0.241      0.161
string_build_large2     1.00     1.66    1.76  n/a    2.42         25.7   0.673    0.235      0.139
string_concat           1.00     1.96    2.00  n/a    2.93          4.77  0.961    0.300      0.265
string_array_join       1.00     1.90    1.87  n/a    2.71          2.40  0.819    0.319      0.300
string_template         1.00     1.96    1.92  n/a    4.32          8.10  1.03     0.763      0.385
int_to_string           1.00     2.17    2.16  n/a    2.73          2.80  0.650    0.373      0.106
int_toString            1.00     3.05    3.03  n/a    4.24          3.58  0.890    0.442      0.146
float_to_string         1.00     2.13    2.08  n/a    4.01          4.13  1.89     0.601      0.402
float_toString          1.00     2.09    2.08  n/a    5.07          4.69  1.96     0.609      0.423
float_toFixed           1.00     4.28    4.23  n/a    2.05          2.00  0.544    0.666      0.496
float_toPrecision       1.00     1.65    1.63  n/a    2.16          2.08  0.633    0.496      0.353
float_toExponential     1.00     1.63    1.58  n/a    2.23          2.17  0.662    0.511      0.359
string_to_int           1.00     5.72    6.30  n/a    7.91          8.16  3.09     0.725      0.181
string_to_float         1.00     3.17    3.11  n/a    3.93          3.71  1.46     0.637      0.434
bigint32_arith          1.00     8.27    8.08  n/a    4.34          4.98  0.931    1.01       1.04
bigint64_arith          1.00     3.58    3.51  n/a    2.46          3.76  0.557    0.490      0.454
bigint256_arith         1.00     1.06    1.05  n/a    1.25          1.62  0.352    0.215      0.134
Geomean                 1.00     4.03    4.00  n/a    2.84          4.46  0.754    0.511      0.0983
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      6.27      39.2       38.9   n/a      14.0          20.4      4.03      5.62      0.538
empty_down_loop         ns      6.24      39.0       38.8   n/a      13.7          20.2      4.13      5.11      0.537
empty_down_loop2        ns      8.52      46.7       46.0   n/a      19.2          32.9      5.34      6.27      0.536
empty_do_loop           ns      9.27      53.1       43.3   n/a      16.2          21.5      5.67      5.46      0.565
date_now                ns     56.1      119        116     n/a      91.8         145       22.3      30.0      24.3
date_parse              µs      2.00       3.34       3.32  n/a      10.6           8.49     2.94      4.03      3.87
prop_read               ns     25.0      186        186     n/a      86.2         119       26.9      20.8       1.17
prop_write              ns     16.4      139        140     n/a      58.7          96.4     16.6      20.6       0.547
prop_update             ns     35.9      274        272     n/a     101           209       27.1      40.0       1.64
prop_create             ns    570       2796       2471     n/a    1912          2941      571       226        90.5
prop_clone              ns    791       3330       3599     n/a    2764          2268      773        26.6      20.5
prop_delete             µs      2.23       4.90       4.88  n/a       4.37          3.60     1.12      0.504     0.413
array_read              ns     52.1      390        390     n/a     169           297       61.0      74.5       4.60
array_write             ns     58.3      225        226     n/a     136           612       32.2      61.9       1.35
array_update            ns    126        564        558     n/a     205           588       63.4     127         3.14
array_prop_create       µs     19.9      114        110     n/a      26.5          90.0     11.7      13.7       1.89
array_slice             µs      3.05      36.2       36.2   n/a      57.3          25.8     10.2       0.116     0.0858
array_length_read       ns     24.4      178        175     n/a      81.4         113       26.0      21.4       2.85
array_length_decr       µs     26.6      103        103     n/a     112            82.7     20.7      25.5      17.2
array_hole_length_decr  µs     33.7      140        121     n/a     149           136       44.4      25.4      19.6
array_push              µs     19.8       85.5       89.3   n/a     102            63.8     11.2       7.42      0.915
array_pop               µs     17.6       67.7       67.4   n/a     130            67.3     19.6       8.39      1.18
typed_array_read        ns    116        403        402     n/a     326           286       63.3      85.0       4.68
typed_array_write       ns    106        379        387     n/a     219           628       54.8      67.1       1.34
arguments_read          ns    504       3251       3262     n/a    1476              n/a   271       202        31.7
arguments_strict_read   ns    553       3278       3141     n/a    1116              n/a   232       193        34.7
global_read             ns     24.7      175        164     n/a      63.5         120       30.1      18.2       0.685
global_write            ns     25.8      236        223     n/a      69.7         138       14.2      15.2       0.809
global_write_strict     ns     25.7      285        309     n/a      69.0         139       14.3      15.1       0.811
local_destruct          ns    172        762        740     n/a    1236          3905      193       184         7.03
global_destruct         ns    154       1060       1041     n/a    1083          1342      170       193        12.9
global_destruct_strict  ns    150       1243       1207     n/a    1082          1334      169       195        13.0
global_func_call        ns    105        291        287     n/a     279           234       58.9      68.7       0.522
func_call               ns    108        241        237     n/a     273           178       56.5      66.1       0.479
func_closure_call       ns    117        236        239     n/a     280           189       60.8      64.0       9.44
int_arith               µs     10.9       77.8       76.6   n/a      29.7          42.3      9.53     10.1       0.336
float_arith             µs     13.3      129        127     n/a      34.2          57.7     12.1      17.6       0.924
map_set_string          µs    229        390        391     n/a     515           470      131        56.6      21.8
map_set_int             µs    135        256        255     n/a     297          4695       76.6      55.9      14.3
map_set_bigint          µs    180        396        407     n/a     468          8699       83.5     109        71.3
map_delete              µs    307        429        430     n/a     505              n/a   133        59.8      24.8
weak_map_set            µs    140     120777     100268     n/a     219              n/a    46.9      40.5      18.1
weak_map_delete         µs    243       3500       2906     n/a     466              n/a   108        83.4      35.0
array_for               µs      1.16       7.07       7.15  n/a       3.22          4.69     1.10      1.24      0.102
array_for_in            µs      1.93      22.3       22.4   n/a      17.8          12.0      3.60      1.50      1.21
array_for_of            ns    928       5998       6148     n/a   10332          4819     1415      2288       284
math_min                µs     33.8      109        106     n/a      79.0          84.7     16.0      16.7       0.273
regexp_ascii            µs    204        877        864     n/a     365          1017      110        45.2      29.3
regexp_utf16            µs    231        881        864     n/a     375          1074      130        47.1      31.2
regexp_replace          µs    358        843        823     n/a     669          5156      605        81.1      62.1
string_length           ns     30.9      326        328     n/a     134           109       26.7      26.6       2.00
string_build1           µs     41.3       69.7       71.2   n/a      35.9         108       12.1      10.8       2.27
string_build1x          µs     41.2       69.6       69.8   n/a      35.7         108       12.1      10.5       2.23
string_build2c          µs     41.1       69.2       68.1   n/a      35.8         122       14.1      10.4       2.20
string_build2           µs     40.4       69.3       69.6   n/a      90.7         117       37.1      10.7       2.23
string_build3           µs     46.9       81.9       76.9   n/a      95.5         114       26.1      11.8       2.22
string_build4           µs     43.8       75.2       75.4   n/a     103           121       38.7      10.7       2.22
string_build_large1     µs    884       1489       1461     n/a    2244         26279      705       213       142
string_build_large2     ms      1.04       1.73       1.83  n/a       2.58         26.9      0.700     0.245     0.145
string_concat           ns    722       1410       1428     n/a    2118          3442      615       188       169
string_array_join       ns    743       1406       1391     n/a    2015          1783      605       237       223
string_template         ns    341        661        649     n/a    1459          2752      350       254       133
int_to_string           ns     98.3      216        216     n/a     274           278       65.1      37.1      10.4
int_toString            ns    112        342        344     n/a     477           402      100        49.7      16.5
float_to_string         ns    212        455        447     n/a     854           899      407       127        85.6
float_toString          ns    218        454        452     n/a    1117          1034      427       133        92.1
float_toFixed           ns    354       1514       1524     n/a     718           707      193       236       177
float_toPrecision       ns    353        579        575     n/a     760           742      224       175       126
float_toExponential     ns    343        557        543     n/a     765           743      227       175       123
string_to_int           ns     19.3      112        124     n/a     156           158       59.5      14.2       3.50
string_to_float         ns     47.4      151        148     n/a     190           177       69.4      30.2      22.2
bigint32_arith          µs     20.2      164        163     n/a      85.4         100       18.3      20.3      21.0
bigint64_arith          µs     45.5      169        160     n/a     115           170       26.4      23.0      21.1
bigint256_arith         µs    169        178        177     n/a     213           271       58.4      36.2      22.3
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
| date | 2026-11-19T04:37:09+09:00 | 2026-11-19T04:49:45+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261118193413-b9d4e40e8453 | v0.0.0-20261118193413-b9d4e40e8453 |
| github.com/dop251/goja | v0.0.0-20261117122431-0802ca125248 | v0.0.0-20261117122431-0802ca125248 |
| github.com/grafana/sobek | v0.0.0-20261116133901-d070e0f1f200 | v0.0.0-20261116133901-d070e0f1f200 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 5.05 5.69 5.99 | 6.29 5.34 5.38 |
| load-end | 6.29 5.34 5.38 | 5.51 5.42 5.30 |
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
