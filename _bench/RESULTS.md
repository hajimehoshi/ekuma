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
Richards      1.00    5.93   5.79  37.6   5.01           4.84  1.43     1.11       0.0414
DeltaBlue     1.00    5.60   5.59  48.2   5.52           5.13  1.66     1.14       0.0144
Encrypt       1.00   10.3   10.5   45.0   5.69            n/a  1.54     1.35       0.0316
Decrypt       1.00   10.7   10.6   46.3   5.76            n/a  1.53     1.32       0.0283
RayTrace      1.00    9.34   9.07  31.3   5.68           7.09  1.45     0.803      0.0259
Earley        1.00    5.49   5.54  31.0   4.54            n/a  1.26     0.501      0.0319
Boyer         1.00    6.38   6.42  38.0   4.87            n/a  1.14     0.620      0.0427
RegExp        1.00    9.09   7.60    n/a  7.55          18.6   4.58     0.367      0.141
Splay         1.00    3.67   3.56  17.8   2.33           2.86  0.767    0.446      0.0810
NavierStokes  1.00   11.5   11.8   52.3   3.82           7.38  1.37     1.52       0.0906
Geomean       1.00    7.35   7.19  37.0   4.88           6.41  1.50     0.821      0.0425
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                    modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.62    9.59    9.55    61.8     8.32          7.92    2.32    1.77      0.0667
DeltaBlue     ms     2.64   15.2    15.1    127      15.0          14.1     4.32    3.11      0.0374
Encrypt       ms     2.48   24.8    25.3    109      13.9            n/a    3.78    3.35      0.0774
Decrypt       ms    46.8   502     497     2110     263              n/a   71.8    62.0       1.35
RayTrace      ms    14.9   134     143      476      83.1         105      22.2    11.4       0.387
Earley        ms     3.21   17.4    17.5     98.9    14.5            n/a    4.04    1.60      0.102
Boyer         ms    53.0   348     345     2035     251              n/a   57.0    33.0       2.23
RegExp        ms    32.1   282     252         n/a  238           582     147      12.1       4.44
Splay         ms     1.43    5.24    5.21    25.6     3.39          4.09    1.11    0.637     0.113
NavierStokes  ms    32.7   389     380     1718     126           242      44.7    50.2       2.96
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                       modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja     Sobek    otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00    12.2     12.2    n/a    4.54          6.44  1.28     1.78       0.167
empty_down_loop         1.00    12.0     11.6    n/a    4.24          6.01  1.24     1.54       0.168
empty_down_loop2        1.00    14.8     16.0    n/a    6.46         10.8   1.75     2.08       0.181
empty_do_loop           1.00    14.2     13.9    n/a    5.20          6.35  1.68     1.62       0.179
date_now                1.00     2.96     2.95   n/a    2.27          3.59  0.565    0.733      0.595
date_parse              1.00     1.83     1.84   n/a    5.73          4.53  1.53     2.27       2.13
prop_read               1.00    11.0     10.9    n/a    5.11          7.02  1.55     1.21       0.0660
prop_write              1.00    14.0     14.7    n/a    5.64          9.22  1.59     1.57       0.0528
prop_update             1.00     9.37     9.21   n/a    3.38          7.08  0.906    1.34       0.0522
prop_create             1.00     4.03     3.89   n/a    2.80          4.70  1.35     0.404      0.154
prop_clone              1.00    20.4     20.7    n/a   15.8          12.7   4.36     0.152      0.118
prop_delete             1.00     9.41     9.33   n/a    8.42          6.81  2.10     0.938      0.785
array_read              1.00    11.8     12.0    n/a    5.05          9.00  1.87     2.18       0.133
array_write             1.00    12.7     12.5    n/a    7.79         34.9   1.81     3.57       0.0752
array_update            1.00     8.13     7.26   n/a    2.74          7.92  0.837    1.64       0.0407
array_prop_create       1.00     8.47     8.86   n/a    1.97          6.66  0.868    0.998      0.140
array_slice             1.00     9.24     9.19   n/a   14.1           6.71  2.58     0.0292     0.0221
array_length_read       1.00    11.6     11.3    n/a    5.00          7.02  1.64     1.33       0.169
array_length_decr       1.00     4.10     4.45   n/a    4.48          3.30  0.825    0.985      0.664
array_hole_length_decr  1.00     4.25     4.31   n/a    5.19          4.73  1.54     0.889      0.601
array_push              1.00     7.07     7.32   n/a    3.74          4.96  0.866    0.578      0.0715
array_pop               1.00     5.71     5.66   n/a   10.7           5.64  1.64     0.708      0.0971
typed_array_read        1.00     6.97     6.78   n/a    5.57          4.82  1.03     1.41       0.0799
typed_array_write       1.00    12.6     12.6    n/a    6.92         19.7   1.75     2.14       0.0431
arguments_read          1.00    18.8     19.3    n/a    8.03           n/a  1.48     1.09       0.140
arguments_strict_read   1.00    18.1     18.5    n/a    6.14           n/a  1.30     1.13       0.175
global_read             1.00    10.4     10.4    n/a    3.50          6.50  1.63     1.04       0.0433
global_write            1.00    14.7     14.7    n/a    4.55          9.05  0.935    1.01       0.0528
global_write_strict     1.00    19.8     20.6    n/a    4.62          9.07  0.950    1.00       0.0528
local_destruct          1.00     6.06     6.34   n/a    9.55         31.4   1.51     1.46       0.0538
global_destruct         1.00     8.36     8.67   n/a    8.49         10.0   1.32     1.49       0.0967
global_destruct_strict  1.00     9.59     9.52   n/a    8.05          9.61  1.33     1.56       0.0928
global_func_call        1.00     4.13     4.07   n/a    4.06          3.15  0.824    0.955      0.00613
func_call               1.00     3.60     3.60   n/a    4.18          2.70  0.882    0.998      0.00668
func_closure_call       1.00     3.15     3.20   n/a    3.64          2.39  0.805    0.774      0.122
int_arith               1.00    10.6     10.4    n/a    3.93          5.78  1.27     1.29       0.0447
float_arith             1.00    14.4     14.8    n/a    3.81          6.41  1.41     1.92       0.0799
map_set_string          1.00     2.91     3.06   n/a    3.86          3.53  0.961    0.419      0.154
map_set_int             1.00     3.71     3.83   n/a    4.17         65.0   1.20     0.770      0.202
map_set_bigint          1.00     3.66     3.75   n/a    4.58         84.9   0.804    1.08       0.690
map_delete              1.00     2.76     2.88   n/a    3.08           n/a  0.760    0.346      0.149
weak_map_set            1.00   697      797      n/a    1.17           n/a  0.240    0.211      0.0953
weak_map_delete         1.00     4.45     4.22   n/a    0.921          n/a  0.214    0.170      0.0723
array_for               1.00     8.88     8.82   n/a    3.97          6.08  1.40     1.56       0.127
array_for_in            1.00    11.3     11.5    n/a    8.70          6.11  1.71     0.723      0.590
array_for_of            1.00     7.31     7.49   n/a   12.2           6.16  1.72     2.94       0.345
math_min                1.00     7.87     7.99   n/a    5.68          5.98  1.14     1.23       0.0197
regexp_ascii            1.00     5.19     5.02   n/a    2.14          6.29  0.671    0.281      0.173
regexp_utf16            1.00     4.93     4.90   n/a    2.04          5.78  0.687    0.247      0.168
regexp_replace          1.00     2.82     2.81   n/a    2.13         16.3   1.93     0.249      0.195
string_length           1.00    19.2     18.8    n/a    7.56          6.30  1.54     1.57       0.117
string_build1           1.00     1.79     1.75   n/a    0.855         2.69  0.294    0.275      0.0551
string_build1x          1.00     1.78     1.83   n/a    0.889         2.72  0.301    0.260      0.0560
string_build2c          1.00     1.73     1.73   n/a    0.883         2.97  0.339    0.257      0.0543
string_build2           1.00     1.78     1.81   n/a    2.26          2.98  0.916    0.266      0.0554
string_build3           1.00     1.79     1.74   n/a    2.09          2.56  0.553    0.261      0.0500
string_build4           1.00     1.85     1.88   n/a    2.48          2.92  0.933    0.256      0.0533
string_build_large1     1.00     1.76     1.76   n/a    2.59         30.2   0.788    0.234      0.160
string_build_large2     1.00     1.81     1.76   n/a    2.27         26.3   0.673    0.241      0.137
string_concat           1.00     2.03     2.13   n/a    2.85          4.48  0.785    0.258      0.218
string_array_join       1.00     2.63     2.64   n/a    3.68          3.32  1.11     0.440      0.411
string_template         1.00     2.02     2.08   n/a    4.39          8.17  1.04     0.744      0.384
int_to_string           1.00     3.72     3.52   n/a    4.43          4.62  1.03     0.615      0.173
int_toString            1.00     4.36     4.30   n/a    6.01          4.71  1.06     0.583      0.208
float_to_string         1.00     2.76     2.77   n/a    5.20          5.26  2.41     0.753      0.494
float_toString          1.00     2.40     2.34   n/a    5.37          5.06  2.06     0.657      0.452
float_toFixed           1.00     7.09     7.22   n/a    3.22          3.21  0.874    1.06       0.799
float_toPrecision       1.00     2.64     2.62   n/a    3.59          3.45  1.05     0.796      0.568
float_toExponential     1.00     2.13     2.15   n/a    2.96          2.81  0.900    0.718      0.496
string_to_int           1.00     6.67     6.72   n/a    9.27          9.29  3.40     0.814      0.206
string_to_float         1.00     3.30     2.96   n/a    3.51          3.40  1.30     0.576      0.418
bigint32_arith          1.00     9.05     8.99   n/a    4.59          5.37  0.959    1.06       0.191
bigint64_arith          1.00     2.96     2.98   n/a    1.96          3.13  0.481    0.411      0.176
bigint256_arith         1.00     0.960    0.975  n/a    1.13          1.54  0.315    0.189      0.116
Geomean                 1.00     5.81     5.83   n/a    3.98          6.34  1.07     0.721      0.133
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                  modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      3.34      40.7       40.2   n/a     14.7           20.5      4.12      5.83      0.543
empty_down_loop         ns      3.37      40.9       39.1   n/a     14.3           20.2      4.27      5.18      0.576
empty_down_loop2        ns      3.10      48.4       48.5   n/a     19.3           33.5      5.44      6.44      0.562
empty_do_loop           ns      3.23      45.4       44.5   n/a     16.8           20.3      5.51      5.27      0.579
date_now                ns     41.0      121        121     n/a     93.9          147       23.5      30.5      24.4
date_parse              µs      1.85       3.38       3.37  n/a     10.6            8.30     2.83      4.15      3.94
prop_read               ns     17.2      189        186     n/a     90.4          124       26.6      21.5       1.14
prop_write              ns     10.6      146        155     n/a     60.7           98.3     16.9      16.8       0.556
prop_update             ns     30.3      283        279     n/a    103            211       27.4      40.7       1.60
prop_create             ns    610       2518       2430     n/a   1706           2812      800       244        93.7
prop_clone              ns    180       3783       3642     n/a   2853           2311      778        27.9      21.3
prop_delete             ns    547       5105       5214     n/a   4521           3654     1152       514       421
array_read              ns     33.7      398        408     n/a    173            310       62.7      73.7       4.47
array_write             ns     18.3      232        228     n/a    140            637       33.1      64.2       1.40
array_update            ns     78.0      582        578     n/a    214            613       65.1     128         3.18
array_prop_create       µs     13.8      117        123     n/a     27.1           92.1     12.0      13.8       1.93
array_slice             µs      4.04      37.4       37.9   n/a     57.8           27.2     10.7       0.123     0.0982
array_length_read       ns     16.5      251        212     n/a     88.0          120       27.1      22.1       2.98
array_length_decr       µs     26.8      110        119     n/a    119             86.9     22.0      26.2      17.7
array_hole_length_decr  µs     30.0      139        130     n/a    156            138       46.3      26.7      17.6
array_push              µs     13.1       92.4       95.7   n/a     49.5           64.9     11.5       7.62      0.934
array_pop               µs     12.5       73.7       71.0   n/a    132             70.3     20.9       9.10      1.20
typed_array_read        ns     61.5      435        419     n/a    341            297       63.1      86.8       4.99
typed_array_write       ns     31.7      401        401     n/a    220            648       58.4      69.0       1.36
arguments_read          ns    187       3534       3543     n/a   1482               n/a   278       204        26.2
arguments_strict_read   ns    185       3334       3423     n/a   1135               n/a   241       205        33.1
global_read             ns     16.2      168        168     n/a     57.0          105       26.3      16.8       0.704
global_write            ns     15.5      230        225     n/a     71.2          141       14.5      15.9       0.811
global_write_strict     ns     15.5      309        319     n/a     71.5          140       14.6      15.7       0.826
local_destruct          ns    126        775        801     n/a   1213           4003      194       188         7.01
global_destruct         ns    134       1104       1175     n/a   1141           1349      171       198        12.9
global_destruct_strict  ns    140       1326       1307     n/a   1122           1347      181       223        13.2
global_func_call        ns     73.3      309        306     n/a    296            232       60.6      72.0       0.460
func_call               ns     69.5      246        246     n/a    287            185       59.1      66.3       0.459
func_closure_call       ns     79.9      245        242     n/a    282            191       63.3      61.8       9.66
int_arith               µs      7.58      82.3       82.2   n/a     31.4           43.9      9.55      9.99      0.354
float_arith             µs      9.00     133        133     n/a     35.6           60.7     12.4      17.2       0.719
map_set_string          µs    138        400        415     n/a    526            489      133        57.9      20.8
map_set_int             µs     71.3      268        283     n/a    298           5590       91.4      62.7      15.4
map_set_bigint          µs    111        414        414     n/a    506           9591       88.9     119        76.0
map_delete              µs    177        454        462     n/a    525               n/a   137        62.4      25.8
weak_map_set            µs    188     132729     155010     n/a    217               n/a    47.3      39.7      18.6
weak_map_delete         µs    522       2590       2205     n/a    481               n/a   112        88.4      38.7
array_for               ns    802       7120       7070     n/a   3247           4852     1126      1282       102
array_for_in            µs      2.06      23.8       23.6   n/a     17.6           12.2      3.51      1.49      1.18
array_for_of            ns    812       6034       6081     n/a   9798           5005     1399      2422       278
math_min                µs     14.1      111        111     n/a     81.2           85.4     16.1      17.0       0.279
regexp_ascii            µs    173        896        904     n/a    369           1149      116        48.8      29.3
regexp_utf16            µs    186        902        910     n/a    377           1067      128        46.0      31.0
regexp_replace          µs    312        921        880     n/a    666           5085      610        80.6      61.6
string_length           ns     17.3      332        324     n/a    132            111       27.2      27.2       2.03
string_build1           µs     42.0       75.0       73.8   n/a     36.6          113       11.9      11.1       2.26
string_build1x          µs     42.0       71.6       73.0   n/a     36.0          109       12.4      10.9       2.31
string_build2c          µs     41.5       71.4       71.2   n/a     37.4          123       14.1      10.7       2.25
string_build2           µs     40.2       71.6       72.8   n/a     91.8          119       37.7      10.8       2.25
string_build3           µs     48.0       85.7       83.4   n/a    100            120       26.6      12.1       2.29
string_build4           µs     41.9       77.6       80.0   n/a    104            121       38.8      10.9       2.25
string_build_large1     µs    905       1591       1584     n/a   2330          27298      713       215       145
string_build_large2     ms      1.05       1.94       1.95  n/a      2.40          28.2      0.711     0.259     0.146
string_concat           ns    770       1577       1649     n/a   2182           3500      610       194       170
string_array_join       ns    563       1487       1487     n/a   2073           1854      642       246       227
string_template         ns    342        682        717     n/a   1501           2790      360       257       131
int_to_string           ns     62.0      226        218     n/a    275            286       64.5      38.8      10.8
int_toString            ns     82.9      362        356     n/a    498            390       87.9      48.2      17.3
float_to_string         ns    170        464        466     n/a    878            894      415       127        83.1
float_toString          ns    205        492        478     n/a   1097           1036      423       134        92.6
float_toFixed           ns    218       1563       1569     n/a    705            700      190       234       174
float_toPrecision       ns    218        609        603     n/a    792            777      232       174       125
float_toExponential     ns    267        581        574     n/a    790            751      240       218       132
string_to_int           ns     17.4      116        113     n/a    156            158       59.0      14.2       3.56
string_to_float         ns     53.7      185        159     n/a    193            185       73.0      32.1      22.4
bigint32_arith          µs     18.9      171        167     n/a     89.2          103       19.2      20.1       3.54
bigint64_arith          µs     57.4      170        171     n/a    113            177       28.5      23.6      10.4
bigint256_arith         µs    192        184        187     n/a    217            295       60.3      36.3      22.4
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
| date | 2026-11-23T01:27:25+09:00 | 2026-11-23T01:40:19+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261122162400-5d3411ee3ba0 | v0.0.0-20261122162400-5d3411ee3ba0 |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 5 | 3 |
| benchtime | 1s | 200ms |
| load-start | 3.99 4.98 5.23 | 5.89 5.79 5.46 |
| load-end | 5.89 5.79 5.46 | 6.45 6.83 6.05 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
