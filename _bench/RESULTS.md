# Engine comparison results

The results of the [engine comparison](README.md) for each benchmark. The summary in
[Results](README.md#results) gives the date and the machine of the measurement, and
[Method](README.md#method) how the times are measured and compared.

## V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                          modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja   Sobek  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00    5.84   5.86  36.7   4.95           4.84  1.44     1.10       0.0408
DeltaBlue     1.00    5.28   5.29  44.8   5.34           4.94  1.54     1.12       0.0137
Encrypt       1.00   10.4   10.0   41.6   5.43            n/a  1.46     1.35       0.0294
Decrypt       1.00    9.95  10.3   45.6   5.62            n/a  1.57     1.36       0.0291
RayTrace      1.00    9.04   9.16  30.9   5.59           7.13  1.50     0.803      0.0264
Earley        1.00    4.90   5.02  27.1   4.35            n/a  1.21     0.484      0.0298
Boyer         1.00    6.36   6.32  35.0   4.86            n/a  1.14     0.615      0.0398
RegExp        1.00    6.56   6.79    n/a  7.15          17.3   4.32     0.366      0.134
Splay         1.00    3.65   3.73  17.9   2.39           2.77  0.754    0.436      0.0795
NavierStokes  1.00   11.3   11.1   47.4   3.70           7.22  1.32     1.46       0.0861
Geomean       1.00    6.89   6.94  34.9   4.77           6.24  1.46     0.814      0.0411
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.55    9.05    9.09    57.4     7.74          7.53    2.25    1.72      0.0632
DeltaBlue     ms     2.67   14.1    14.2    120      14.2          13.2     4.10    3.00      0.0372
Encrypt       ms     2.49   25.7    24.7    104      13.5            n/a    3.64    3.19      0.0732
Decrypt       ms    45.2   455     455     2007     258              n/a   71.3    60.1       1.29
RayTrace      ms    13.7   124     126      424      76.3          97.4    20.9    10.9       0.360
Earley        ms     3.19   15.9    15.9     87.7    14.1            n/a    3.88    1.57      0.0962
Boyer         ms    49.6   317     313     1756     250              n/a   59.6    30.4       1.98
RegExp        ms    32.4   216     221         n/a  226           561     140      11.7       4.25
Splay         ms     1.38    5.07    5.16    24.1     3.28          3.82    1.05    0.602     0.110
NavierStokes  ms    32.5   368     363     1540     121           235      43.0    47.7       2.93
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                        modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja      Sobek    otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00      7.81     7.68   n/a    2.78          4.02  0.804    1.12       0.107
empty_down_loop         1.00      7.53     7.61   n/a    2.73          3.94  0.797    1.00       0.105
empty_down_loop2        1.00      8.63     8.64   n/a    3.47          6.08  1.02     1.17       0.101
empty_do_loop           1.00      8.57     8.62   n/a    3.17          3.91  1.05     1.00       0.105
date_now                1.00      2.66     2.64   n/a    2.04          3.29  0.501    0.673      0.562
date_parse              1.00      1.75     1.77   n/a    5.55          4.41  1.60     2.15       2.00
prop_read               1.00      9.32     9.31   n/a    4.32          5.98  1.34     1.03       0.0555
prop_write              1.00     13.4     13.3    n/a    5.57          9.18  1.57     1.57       0.0511
prop_update             1.00      8.64     8.77   n/a    3.21          6.62  0.864    1.28       0.0518
prop_create             1.00      3.90     4.01   n/a    2.83          4.60  0.964    0.388      0.158
prop_clone              1.00      3.98     3.99   n/a    3.16          2.56  0.873    0.0306     0.0236
prop_delete             1.00      2.08     2.11   n/a    1.82          1.51  0.468    0.212      0.176
array_read              1.00     11.1     11.4    n/a    4.76          8.18  1.73     2.10       0.130
array_write             1.00      6.38     6.46   n/a    3.82         17.6   0.898    1.76       0.0384
array_update            1.00      5.07     5.00   n/a    1.87          5.28  0.572    1.14       0.0352
array_prop_create       1.00      5.87     5.94   n/a    1.42          4.79  0.624    0.725      0.100
array_slice             1.00      9.00     9.07   n/a   13.8           6.46  2.51     0.0278     0.0215
array_length_read       1.00     10.1     10.0    n/a    4.71          6.44  1.49     1.21       0.153
array_length_decr       1.00      4.05     4.11   n/a    4.27          3.16  0.794    0.972      0.657
array_hole_length_decr  1.00      4.30     4.30   n/a    5.25          4.78  1.55     0.886      0.610
array_push              1.00      5.65     5.65   n/a    3.12          4.07  0.719    0.477      0.0590
array_pop               1.00      4.79     4.72   n/a    8.37          4.34  1.33     0.564      0.0778
typed_array_read        1.00      4.06     4.12   n/a    3.32          2.82  0.630    0.862      0.0475
typed_array_write       1.00      3.95     4.03   n/a    2.19          6.44  0.601    0.692      0.0147
arguments_read          1.00      7.00     7.06   n/a    2.99           n/a  0.567    0.411      0.0653
arguments_strict_read   1.00      6.71     6.88   n/a    2.26           n/a  0.479    0.399      0.0672
global_read             1.00      9.90     9.44   n/a    3.20          5.76  1.48     0.933      0.0391
global_write            1.00     14.1     13.2    n/a    4.14          8.31  0.855    0.911      0.0483
global_write_strict     1.00     17.0     16.8    n/a    4.15          8.30  0.856    0.910      0.0485
local_destruct          1.00      5.46     5.48   n/a    7.81         25.3   1.23     1.22       0.0470
global_destruct         1.00      7.13     7.49   n/a    7.45          8.91  1.15     1.33       0.0877
global_destruct_strict  1.00      8.65     8.83   n/a    7.41          8.87  1.17     1.34       0.0871
global_func_call        1.00      3.83     3.87   n/a    3.84          2.96  0.748    0.889      0.00684
func_call               1.00      3.25     3.25   n/a    3.76          2.45  0.787    0.898      0.00621
func_closure_call       1.00      2.92     2.84   n/a    3.37          2.30  0.761    0.737      0.114
int_arith               1.00      8.77     8.74   n/a    3.30          4.66  1.04     1.05       0.0379
float_arith             1.00     12.0     12.0    n/a    3.21          5.35  1.10     1.58       0.0668
map_set_string          1.00      1.75     1.74   n/a    2.20          2.05  0.575    0.250      0.0940
map_set_int             1.00      2.08     2.08   n/a    2.35         36.8   0.615    0.443      0.112
map_set_bigint          1.00      2.49     2.55   n/a    2.96         55.3   0.531    0.697      0.465
map_delete              1.00      1.46     1.47   n/a    1.71           n/a  0.430    0.197      0.0847
weak_map_set            1.00   1137      836      n/a    1.52           n/a  0.323    0.282      0.128
weak_map_delete         1.00     14.7     15.7    n/a    2.84           n/a  0.648    0.502      0.209
array_for               1.00      7.33     7.36   n/a    3.23          4.81  1.13     1.28       0.104
array_for_in            1.00     11.8     12.0    n/a    9.21          6.28  1.79     0.773      0.615
array_for_of            1.00      7.32     7.18   n/a   12.2           5.94  1.67     2.83       0.342
math_min                1.00      3.72     3.74   n/a    2.76          2.88  0.555    0.614      0.00936
regexp_ascii            1.00      5.01     5.01   n/a    2.09          5.79  0.631    0.257      0.169
regexp_utf16            1.00      4.15     4.19   n/a    1.78          5.09  0.605    0.217      0.147
regexp_replace          1.00      2.41     2.41   n/a    1.89         14.1   1.68     0.223      0.175
string_length           1.00     11.5     11.4    n/a    4.70          3.79  0.931    0.909      0.0993
string_build1           1.00      1.74     1.74   n/a    0.857         2.62  0.292    0.261      0.0543
string_build1x          1.00      1.73     1.75   n/a    0.869         2.68  0.292    0.257      0.0539
string_build2c          1.00      1.69     1.72   n/a    0.850         2.87  0.328    0.238      0.0519
string_build2           1.00      1.74     1.75   n/a    2.17          2.82  0.894    0.260      0.0537
string_build3           1.00      1.73     1.75   n/a    2.06          2.47  0.563    0.250      0.0481
string_build4           1.00      1.80     1.82   n/a    2.41          2.83  0.898    0.250      0.0520
string_build_large1     1.00      1.68     1.67   n/a    2.38         27.9   0.738    0.223      0.155
string_build_large2     1.00      1.72     1.73   n/a    2.19         25.1   0.656    0.226      0.134
string_concat           1.00      2.04     2.07   n/a    2.88          4.67  0.809    0.255      0.225
string_array_join       1.00      1.80     1.88   n/a    2.57          2.33  0.783    0.311      0.290
string_template         1.00      2.11     2.12   n/a    4.59          8.59  1.10     0.766      0.597
int_to_string           1.00      2.12     2.14   n/a    2.66          2.77  0.615    0.372      0.103
int_toString            1.00      4.10     4.10   n/a    5.51          4.32  0.994    0.548      0.196
float_to_string         1.00      2.18     2.17   n/a    4.10          4.28  1.97     0.607      0.404
float_toString          1.00      2.35     2.59   n/a    5.29          5.01  2.08     0.672      0.459
float_toFixed           1.00      4.82     4.83   n/a    2.21          2.57  0.611    0.744      0.559
float_toPrecision       1.00      1.98     1.99   n/a    2.55          2.48  0.754    0.583      0.420
float_toExponential     1.00      1.86     1.86   n/a    2.56          2.49  0.739    0.577      0.415
string_to_int           1.00      6.10     6.24   n/a    8.17          8.57  3.05     0.763      0.189
string_to_float         1.00      2.81     2.83   n/a    3.46          3.37  1.32     0.575      0.381
bigint32_arith          1.00      8.20     8.31   n/a    4.16          4.88  0.872    0.972      0.137
bigint64_arith          1.00      2.87     2.86   n/a    1.91          2.78  0.433    0.383      0.297
bigint256_arith         1.00      0.920    0.921  n/a    1.05          1.41  0.292    0.180      0.111
Geomean                 1.00      4.58     4.59   n/a    3.14          5.00  0.841    0.569      0.108
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      5.04      39.4       38.7   n/a     14.0           20.3      4.06      5.64      0.538
empty_down_loop         ns      5.05      38.3       38.5   n/a     13.8           19.9      4.03      5.06      0.536
empty_down_loop2        ns      5.57      48.0       46.1   n/a     18.6           34.5      5.68      6.51      0.562
empty_do_loop           ns      5.15      45.5       45.7   n/a     16.4           19.8      5.32      5.06      0.540
date_now                ns     44.5      123        118     n/a     90.5          146       23.1      30.2      25.1
date_parse              µs      1.87       3.26       3.31  n/a     10.4            8.23     3.00      4.02      3.73
prop_read               ns     19.8      196        195     n/a     84.8          119       26.3      20.6       1.13
prop_write              ns     10.4      139        138     n/a     57.8           95.3     16.2      16.2       0.532
prop_update             ns     30.7      265        268     n/a     98.6          204       26.5      39.5       1.59
prop_create             ns    592       2309       2350     n/a   1658           2697      565       227        93.3
prop_clone              ns    866       3450       3456     n/a   2743           2223      754        26.5      20.4
prop_delete             µs      2.36       4.89       4.97  n/a      4.28           3.57     1.11      0.500     0.416
array_read              ns     35.3      392        399     n/a    168            287       60.6      73.4       4.65
array_write             ns     35.1      224        226     n/a    135            615       31.5      61.7       1.35
array_update            ns    111        563        555     n/a    208            583       63.7     125         3.90
array_prop_create       µs     18.5      115        116     n/a     26.3           88.6     11.8      13.6       1.87
array_slice             µs      4.02      36.2       36.5   n/a     55.9           26.0     10.1       0.112     0.104
array_length_read       ns     17.6      177        176     n/a     82.6          113       26.1      21.1       2.69
array_length_decr       µs     26.2      106        107     n/a    112             82.8     20.7      25.5      17.2
array_hole_length_decr  µs     29.4      124        125     n/a    154            141       45.7      26.1      17.6
array_push              µs     15.5       86.6       87.5   n/a     49.1           62.7     11.1       7.36      0.913
array_pop               µs     15.4       74.2       72.7   n/a    128             66.5     19.8       8.17      1.20
typed_array_read        ns     98.9      401        407     n/a    332            279       62.1      91.6       5.12
typed_array_write       ns     95.3      376        384     n/a    209            613       57.5      66.0       1.40
arguments_read          ns    486       3376       3430     n/a   1454               n/a   274       200        31.7
arguments_strict_read   ns    483       3325       3332     n/a   1109               n/a   230       198        32.3
global_read             ns     17.6      174        166     n/a     56.3          101       26.1      16.4       0.687
global_write            ns     16.5      232        218     n/a     68.2          137       14.1      15.1       0.797
global_write_strict     ns     16.5      282        279     n/a     68.7          137       14.2      15.1       0.801
local_destruct          ns    150        787        778     n/a   1171           3788      185       181         7.00
global_destruct         ns    144       1042       1094     n/a   1075           1284      168       191        12.6
global_destruct_strict  ns    144       1216       1241     n/a   1063           1273      167       189        12.7
global_func_call        ns     75.7      290        293     n/a    293            223       57.1      67.0       0.518
func_call               ns     73.7      240        239     n/a    274            181       57.3      65.2       0.462
func_closure_call       ns     82.9      240        236     n/a    280            191       63.1      61.1       9.44
int_arith               µs      8.81      78.2       77.0   n/a     29.1           41.0      9.16      9.27      0.338
float_arith             µs     10.7      128        128     n/a     34.2           57.1     11.8      17.1       0.712
map_set_string          µs    229        405        395     n/a    502            462      132        57.3      21.0
map_set_int             µs    124        255        256     n/a    291           4571       75.9      54.2      13.7
map_set_bigint          µs    156        390        393     n/a    458           8595       82.4     108        71.9
map_delete              µs    296        426        429     n/a    503               n/a   128        58.1      24.7
weak_map_set            µs    139     162473     117699     n/a    218               n/a    44.9      39.1      18.1
weak_map_delete         µs    167       2546       2572     n/a    491               n/a   109        82.8      34.8
array_for               ns    976       6986       7008     n/a   3072           4637     1073      1248       102
array_for_in            µs      1.89      22.3       23.1   n/a     17.8           11.9      3.39      1.46      1.16
array_for_of            ns    804       5867       5758     n/a   9746           4791     1365      2318       279
math_min                µs     28.7      109        108     n/a     79.3           84.6     15.9      17.5       0.270
regexp_ascii            µs    172        859        867     n/a    359            997      111        46.1      29.0
regexp_utf16            µs    214        878        896     n/a    368           1072      129        45.2      30.9
regexp_replace          µs    352        847        850     n/a    665           4952      591        78.7      61.5
string_length           ns     28.3      325        323     n/a    133            108       26.4      26.0       2.83
string_build1           µs     40.7       70.7       71.2   n/a     35.2          107       11.8      10.6       2.22
string_build1x          µs     40.7       70.6       71.1   n/a     35.1          109       11.9      10.4       2.20
string_build2c          µs     42.0       71.0       71.9   n/a     35.7          122       13.8      10.3       2.20
string_build2           µs     41.1       71.5       71.8   n/a     89.0          116       36.6      10.7       2.20
string_build3           µs     46.0       79.8       80.7   n/a     94.8          113       25.7      11.4       2.21
string_build4           µs     42.6       76.5       77.0   n/a    102            120       38.2      10.7       2.20
string_build_large1     µs    941       1557       1557     n/a   2216          26220      690       211       144
string_build_large2     ms      1.07       1.84       1.86  n/a      2.35          26.9      0.703     0.243     0.143
string_concat           ns    734       1499       1521     n/a   2110           3438      596       188       165
string_array_join       ns    775       1362       1454     n/a   1979           1760      603       235       222
string_template         ns    316        653        668     n/a   1445           2740      350       242       194
int_to_string           ns     99.6      211        212     n/a    264            273       60.9      36.6      10.2
int_toString            ns     85.9      352        349     n/a    471            371       84.5      47.1      16.6
float_to_string         ns    206        449        446     n/a    842            882      404       125        83.3
float_toString          ns    200        493        506     n/a   1056           1011      415       134        90.7
float_toFixed           ns    314       1520       1528     n/a    695            805      205       240       177
float_toPrecision       ns    297        600        588     n/a    756            735      224       177       125
float_toExponential     ns    298        559        554     n/a    773            738      224       174       123
string_to_int           ns     18.4      112        115     n/a    150            158       56.1      14.0       3.45
string_to_float         ns     52.7      151        149     n/a    182            177       70.2      30.7      20.4
bigint32_arith          µs     20.4      167        169     n/a     84.9           98.9     17.8      19.7       2.79
bigint64_arith          µs     58.9      167        167     n/a    112            170       25.5      22.6      20.5
bigint256_arith         µs    200        181        181     n/a    210            286       58.6      35.4      22.2
```

<!-- END jsbench Micro.time -->

## Missing results

<!-- BEGIN jsbench missing -->

- otto in V8 benchmark suite v7 (RegExp): `regexp.js: Line 1096:7 Invalid regular expression: re2: Invalid (?=) <lookahead> (and 1 more errors)`
- QJS in V8 benchmark suite v7 (Encrypt, Decrypt): `ReferenceError: setupEngine is not defined`
- QJS in V8 benchmark suite v7 (Earley, Boyer): `SyntaxError: Octal escape sequences are not allowed in strict mode`
- otto in QuickJS microbench (all 74): `microbench.js: Line 91:18 Unexpected token ) (and 917 more errors)`
- QJS in QuickJS microbench (arguments_read, arguments_strict_read): `ReferenceError: sum is not defined`
- QJS in QuickJS microbench (map_delete, weak_map_set, weak_map_delete): `ReferenceError: len is not defined`

<!-- END jsbench missing -->

## Environment

<!-- BEGIN jsbench environment -->

|  | V8 benchmark suite v7 | QuickJS microbench |
| --- | --- | --- |
| date | 2026-11-21T00:54:06+09:00 | 2026-11-21T01:06:46+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261120154757-f130b4d6c37d | v0.0.0-20261120154757-f130b4d6c37d |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 2.16 2.58 2.97 | 4.13 3.41 3.20 |
| load-end | 4.13 3.41 3.20 | 2.62 3.14 3.29 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
