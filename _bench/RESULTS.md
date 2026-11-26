# Engine comparison results

The results of the [engine comparison](README.md) for each benchmark. The summary in
[Results](README.md#results) gives the date and the machine of the measurement, and
[Method](README.md#method) explains how the times are measured and compared. The raw
outputs are [v8.txt](results/darwin_arm64/v8.txt) and
[micro.txt](results/darwin_arm64/micro.txt).

## V8 benchmark suite v7

Time relative to Ekuma's:

<!-- BEGIN jsbench V8.relative -->

```
                                                 modernc.org/         QuickJS  v8go
Benchmark     Ekuma  goja   Sobek  moejs  otto   quickjs       QJS    (C)      --jitless  v8go
Richards      1.00    5.88   5.87  1.06   36.3   5.05           5.04  1.45     1.10       0.0324
DeltaBlue     1.00    5.52   5.53  1.19   48.4   5.56           5.18  1.58     1.16       0.0139
Encrypt       1.00    9.93  10.0   1.16   42.6   5.59            n/a  1.51     1.31       0.0306
Decrypt       1.00    9.99  10.0   1.19   43.1   5.66            n/a  1.52     1.34       0.0285
RayTrace      1.00    9.15   9.26  1.03   30.8   5.68           7.30  1.51     0.806      0.0270
Earley        1.00    5.42   5.47  0.802  29.5   4.48            n/a  1.29     0.542      0.0326
Boyer         1.00    6.20   6.17  0.768  34.7   4.90            n/a  1.12     0.616      0.0437
RegExp        1.00    7.58   7.58  0.747    n/a  7.84          19.8   4.92     0.411      0.151
Splay         1.00    3.70   3.73  0.613  17.8   2.44           2.94  0.762    0.442      0.0801
NavierStokes  1.00   11.8   11.6   0.844  51.7   3.89           7.56  1.37     1.52       0.0896
Geomean       1.00    7.11   7.12  0.919  35.7   4.92           6.62  1.50     0.837      0.0418
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                            modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   moejs   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.50    8.82    8.81   1.59     54.5     7.58          7.56    2.18    1.65      0.0486
DeltaBlue     ms     2.55   14.1    14.1    3.03    123      14.2          13.2     4.04    2.97      0.0355
Encrypt       ms     2.41   23.9    24.1    2.80    103      13.5            n/a    3.65    3.16      0.0737
Decrypt       ms    44.8   448     448     53.5    1931     254              n/a   68.3    60.2       1.28
RayTrace      ms    13.4   123     124     13.8     413      76.2          97.9    20.2    10.8       0.363
Earley        ms     2.97   16.1    16.3    2.38     87.7    13.3            n/a    3.83    1.61      0.0968
Boyer         ms    49.6   308     306     38.1    1722     243              n/a   55.5    30.6       2.17
RegExp        ms    28.5   216     216     21.3        n/a  224           565     140      11.7       4.30
Splay         ms     1.38    5.12    5.15   0.847    24.6     3.37          4.06    1.05    0.610     0.111
NavierStokes  ms    31.5   372     366     26.6    1629     123           238      43.2    47.9       2.82
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                              modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja     Sobek   moejs   otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00    12.2     12.1    1.36   n/a    4.29          6.40  1.25     1.74       0.165
empty_down_loop         1.00    11.7     11.7    1.34   n/a    4.13          6.05  1.23     1.55       0.163
empty_down_loop2        1.00    15.7     15.4    1.44   n/a    6.32         11.1   1.80     2.08       0.180
empty_do_loop           1.00    13.3     13.2    1.34   n/a    4.85          6.01  1.60     1.54       0.162
date_now                1.00     2.77     2.80   0.841  n/a    2.19          3.46  0.867    0.720      0.569
date_parse              1.00     1.80     1.79   1.17   n/a    5.63          4.54  1.58     2.18       2.11
prop_read               1.00    10.1     10.3    0.967  n/a    4.65          6.49  1.46     1.13       0.0591
prop_write              1.00    15.1     15.0    1.00   n/a    6.24         10.2   1.75     1.73       0.0574
prop_update             1.00     9.23     9.32   0.980  n/a    3.40          7.05  0.919    1.34       0.0522
prop_create             1.00     4.17     4.19   0.953  n/a    2.85          4.65  0.938    0.391      0.162
prop_clone              1.00    20.1     20.3    1.08   n/a   15.4          12.4   4.26     0.149      0.115
prop_delete             1.00     9.57     9.51   2.02   n/a    8.91          6.53  2.08     0.920      0.764
array_read              1.00    11.5     11.7    1.67   n/a    4.92          8.45  1.80     2.13       0.135
array_write             1.00    13.2     13.2    1.39   n/a    8.12         35.3   1.84     3.60       0.0776
array_update            1.00     7.23     7.26   1.10   n/a    2.66          7.65  0.841    1.66       0.0408
array_prop_create       1.00     8.27     8.42   1.68   n/a    2.04          6.85  0.887    1.03       0.144
array_slice             1.00     9.76     9.77   0.678  n/a   15.0           6.84  2.67     0.0295     0.0226
array_length_read       1.00    11.1     11.2    1.10   n/a    4.96          7.06  1.63     1.35       0.168
array_length_decr       1.00     3.82     3.84   2.00   n/a    3.94          2.89  0.728    0.904      0.612
array_hole_length_decr  1.00     4.01     3.93   1.86   n/a    4.91          4.47  1.48     0.840      0.565
array_push              1.00     6.96     6.60   0.942  n/a    3.54          4.69  0.851    0.553      0.0684
array_pop               1.00     5.62     5.55   2.18   n/a    9.88          5.13  1.69     0.673      0.0908
typed_array_read        1.00     7.16     7.09   1.33   n/a    5.83          4.95  1.09     1.48       0.0835
typed_array_write       1.00    12.5     12.7    2.90   n/a    6.70         19.9   1.79     2.14       0.0433
arguments_read          1.00    18.9     19.0    1.16   n/a    7.79           n/a  1.46     1.04       0.169
arguments_strict_read   1.00    18.3     18.7    1.15   n/a    5.88           n/a  1.24     1.08       0.170
global_read             1.00    11.3     10.8    1.10   n/a    3.50          6.30  1.63     1.02       0.0426
global_write            1.00     7.76     8.84   0.497  n/a    2.17          4.34  0.451    0.480      0.0257
global_write_strict     1.00    10.9      9.08   0.500  n/a    2.21          4.40  0.453    0.485      0.0258
local_destruct          1.00     6.33     6.31   0.942  n/a   11.6          30.7   1.50     1.46       0.0564
global_destruct         1.00     7.93     7.69   0.752  n/a    7.64          9.15  1.18     1.35       0.0905
global_destruct_strict  1.00     8.77     9.18   0.756  n/a    7.54          9.04  1.18     1.37       0.0903
global_func_call        1.00     4.25     4.18   0.963  n/a    4.02          3.24  0.841    1.01       0.00843
func_call               1.00     3.56     3.55   0.945  n/a    4.10          2.74  0.859    0.999      0.00844
func_closure_call       1.00     3.12     3.13   0.944  n/a    3.59          2.46  0.806    0.809      0.116
int_arith               1.00    10.7     10.8    1.07   n/a    4.20          5.69  1.27     1.30       0.0454
float_arith             1.00    14.8     15.1    1.06   n/a    3.91          6.61  1.39     1.93       0.0799
map_set_string          1.00     2.97     2.96   0.700  n/a    3.75          3.43  0.969    0.417      0.155
map_set_int             1.00     3.50     3.50   0.948  n/a    3.84         60.4   1.02     0.726      0.181
map_set_bigint          1.00     3.78     3.84   1.38   n/a    4.36         81.6   0.779    1.04       0.671
map_delete              1.00     2.79     2.74   0.647  n/a    3.16           n/a  0.815    0.363      0.155
weak_map_set            1.00   973      996     13.9    n/a    1.36           n/a  0.286    0.244      0.117
weak_map_delete         1.00     5.54     5.88   0.302  n/a    0.974          n/a  0.230    0.175      0.0733
array_for               1.00     8.82     8.91   1.04   n/a    3.91          5.85  1.37     1.55       0.126
array_for_in            1.00    10.7     11.7    1.59   n/a    8.11          5.55  1.60     0.685      0.543
array_for_of            1.00     7.92     7.56   1.04   n/a   12.5           6.18  1.72     2.91       0.358
math_min                1.00     7.79     7.67   1.67   n/a    5.86          6.02  1.14     1.19       0.0188
regexp_ascii            1.00     5.43     5.44   1.03   n/a    2.19          6.08  0.665    0.276      0.174
regexp_utf16            1.00     4.90     4.97   1.13   n/a    1.99          5.67  0.689    0.246      0.164
regexp_replace          1.00     2.90     2.90   0.921  n/a    2.14         16.7   1.94     0.253      0.197
string_length           1.00    19.4     18.9    1.03   n/a    7.66          6.28  1.54     1.52       0.115
string_build1           1.00     1.84     1.87   0.727  n/a    0.902         2.76  0.302    0.275      0.0563
string_build1x          1.00     1.88     1.85   0.732  n/a    0.899         2.74  0.309    0.267      0.0564
string_build2c          1.00     1.83     1.84   0.714  n/a    0.903         3.05  0.345    0.262      0.0548
string_build2           1.00     1.88     1.89   0.729  n/a    2.32          2.99  0.951    0.275      0.0573
string_build3           1.00     1.85     1.86   0.529  n/a    2.15          2.54  0.581    0.257      0.0495
string_build4           1.00     2.34     1.87   0.740  n/a    2.41          2.81  0.897    0.252      0.0515
string_build_large1     1.00     1.78     1.76   0.634  n/a    2.50         29.8   0.793    0.245      0.169
string_build_large2     1.00     1.80     1.89   0.449  n/a    2.32         26.7   0.684    0.240      0.136
string_concat           1.00     2.05     2.10   2.27   n/a    2.68          4.09  0.654    0.204      0.183
string_array_join       1.00     2.66     2.84   0.967  n/a    3.74          3.26  1.09     0.439      0.413
string_template         1.00     2.01     2.08   3.86   n/a    4.42          8.37  1.07     0.786      0.584
int_to_string           1.00     3.45     3.50   1.26   n/a    4.31          4.45  1.01     0.609      0.168
int_toString            1.00     3.97     4.04   1.15   n/a    5.49          4.35  1.00     0.543      0.188
float_to_string         1.00     2.62     2.64   1.38   n/a    4.96          5.22  2.37     0.716      0.492
float_toString          1.00     2.39     2.32   1.29   n/a    5.44          5.20  2.14     0.664      0.457
float_toFixed           1.00     6.80     6.80   1.87   n/a    3.12          3.13  0.844    1.04       0.774
float_toPrecision       1.00     2.60     2.60   6.75   n/a    3.42          3.34  0.988    0.775      0.555
float_toExponential     1.00     2.27     2.28   6.22   n/a    3.15          3.05  0.932    0.712      0.503
string_to_int           1.00     6.74     6.80   3.00   n/a    9.19          9.36  3.31     0.838      0.208
string_to_float         1.00     2.92     2.90   0.995  n/a    3.53          3.42  1.35     0.591      0.391
bigint32_arith          1.00     9.48     9.67   6.11   n/a    4.82          6.66  0.954    1.07       1.11
bigint64_arith          1.00     3.15     3.19   2.00   n/a    2.02          3.07  0.456    0.404      0.165
bigint256_arith         1.00     0.987    1.02   0.636  n/a    1.08          1.40  0.301    0.183      0.115
Geomean                 1.00     5.76     5.76   1.21   n/a    3.88          6.12  1.04     0.699      0.134
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                            modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      moejs     otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      3.23      39.3       39.2      4.39   n/a     13.8           20.6      4.02      5.62      0.533
empty_down_loop         ns      3.29      38.6       38.5      4.40   n/a     13.6           19.9      4.05      5.09      0.536
empty_down_loop2        ns      2.96      46.4       45.7      4.28   n/a     18.7           32.8      5.33      6.18      0.535
empty_do_loop           ns      3.31      44.0       43.7      4.44   n/a     16.1           19.9      5.29      5.11      0.537
date_now                ns     41.9      116        117       35.2    n/a     91.6          145       36.3      30.1      23.8
date_parse              µs      1.84       3.32       3.30     2.15   n/a     10.4            8.36     2.90      4.01      3.88
prop_read               ns     18.3      185        188       17.7    n/a     85.1          119       26.7      20.7       1.08
prop_write              ns      9.34     141        140        9.38   n/a     58.2           95.1     16.3      16.2       0.536
prop_update             ns     29.2      269        272       28.6    n/a     99.2          206       26.8      39.2       1.52
prop_create             ns    584       2434       2447      557      n/a   1664           2715      548       228        94.8
prop_clone              ns    180       3621       3648      195      n/a   2763           2235      766        26.7      20.7
prop_delete             ns    546       5226       5198     1102      n/a   4869           3569     1134       502       417
array_read              ns     34.0      393        397       56.7    n/a    167            288       61.2      72.5       4.59
array_write             ns     17.3      228        228       23.9    n/a    140            609       31.8      62.1       1.34
array_update            ns     76.8      555        558       84.8    n/a    205            588       64.7     127         3.13
array_prop_create       µs     13.3      110        112       22.3    n/a     27.1           91.0     11.8      13.7       1.92
array_slice             µs      3.84      37.5       37.6      2.61   n/a     57.5           26.3     10.3       0.113     0.0869
array_length_read       ns     16.1      178        181       17.8    n/a     80.0          114       26.4      21.8       2.71
array_length_decr       µs     28.7      110        110       57.5    n/a    113             83.0     20.9      26.0      17.6
array_hole_length_decr  µs     30.8      123        121       57.2    n/a    151            138       45.7      25.9      17.4
array_push              µs     13.5       93.7       88.9     12.7    n/a     47.7           63.1     11.5       7.44      0.922
array_pop               µs     12.9       72.7       71.9     28.2    n/a    128             66.4     21.8       8.72      1.18
typed_array_read        ns     57.6      412        408       76.7    n/a    336            285       62.5      85.3       4.81
typed_array_write       ns     31.0      386        395       90.0    n/a    208            619       55.5      66.5       1.34
arguments_read          ns    189       3575       3596      219      n/a   1474               n/a   276       196        32.0
arguments_strict_read   ns    188       3432       3512      216      n/a   1105               n/a   234       203        32.0
global_read             ns     16.1      182        175       17.7    n/a     56.4          102       26.2      16.4       0.688
global_write            ns     31.4      244        278       15.6    n/a     68.2          136       14.2      15.1       0.807
global_write_strict     ns     31.3      341        285       15.7    n/a     69.3          138       14.2      15.2       0.808
local_destruct          ns    125        792        790      118      n/a   1454           3844      188       183         7.05
global_destruct         ns    141       1122       1087      106      n/a   1080           1294      167       191        12.8
global_destruct_strict  ns    142       1246       1304      107      n/a   1071           1283      168       195        12.8
global_func_call        ns     69.7      296        291       67.2    n/a    280            226       58.7      70.6       0.588
func_call               ns     67.4      240        239       63.7    n/a    277            185       57.9      67.4       0.569
func_closure_call       ns     77.0      240        241       72.7    n/a    276            190       62.0      62.2       8.94
int_arith               µs      7.30      77.9       78.5      7.83   n/a     30.7           41.6      9.31      9.53      0.332
float_arith             µs      8.78     130        133        9.31   n/a     34.3           58.0     12.2      17.0       0.702
map_set_string          µs    136        405        404       95.5    n/a    511            467      132        56.9      21.2
map_set_int             µs     75.8      265        265       71.9    n/a    291           4577       77.3      55.0      13.7
map_set_bigint          µs    107        403        409      146      n/a    464           8695       83.0     111        71.5
map_delete              µs    160        448        439      104      n/a    506               n/a   131        58.2      24.9
weak_map_set            µs    159     154712     158362     2216      n/a    216               n/a    45.5      38.8      18.6
weak_map_delete         µs    472       2616       2774      143      n/a    460               n/a   109        82.7      34.6
array_for               ns    796       7019       7091      832      n/a   3111           4657     1087      1230        99.9
array_for_in            µs      2.16      23.0       25.2      3.42   n/a     17.5           12.0      3.44      1.48      1.17
array_for_of            ns    787       6233       5951      821      n/a   9811           4861     1352      2293       281
math_min                µs     14.0      109        107       23.4    n/a     82.1           84.2     16.0      16.7       0.264
regexp_ascii            µs    166        899        901      171      n/a    363           1007      110        45.8      28.8
regexp_utf16            µs    187        918        932      211      n/a    374           1064      129        46.1      30.8
regexp_replace          µs    309        894        893      284      n/a    661           5139      598        78.0      60.6
string_length           ns     17.2      334        324       17.7    n/a    132            108       26.5      26.2       1.98
string_build1           µs     39.3       72.3       73.7     28.6    n/a     35.4          108       11.9      10.8       2.21
string_build1x          µs     39.3       74.0       72.8     28.8    n/a     35.3          108       12.1      10.5       2.22
string_build2c          µs     40.2       73.5       74.0     28.7    n/a     36.3          123       13.8      10.5       2.20
string_build2           µs     39.1       73.6       73.8     28.5    n/a     90.7          117       37.2      10.7       2.24
string_build3           µs     44.9       83.1       83.3     23.7    n/a     96.3          114       26.1      11.6       2.22
string_build4           µs     42.9      101         80.2     31.7    n/a    103            121       38.5      10.8       2.21
string_build_large1     µs    880       1568       1546      558      n/a   2197          26227      698       215       149
string_build_large2     ms      1.03       1.85       1.94     0.461  n/a      2.38          27.4      0.703     0.246     0.140
string_concat           ns    913       1869       1914     2075      n/a   2443           3735      597       187       167
string_array_join       ns    541       1437       1535      523      n/a   2021           1764      592       238       223
string_template         ns    330        665        686     1277      n/a   1461           2767      354       260       193
int_to_string           ns     61.9      214        217       77.8    n/a    267            276       62.4      37.7      10.4
int_toString            ns     87.0      346        352      100      n/a    478            378       87.1      47.2      16.3
float_to_string         ns    173        455        458      239      n/a    859            904      411       124        85.2
float_toString          ns    197        470        457      253      n/a   1071           1022      420       131        90.0
float_toFixed           ns    225       1530       1531      421      n/a    702            704      190       235       174
float_toPrecision       ns    223        581        580     1506      n/a    762            745      220       173       124
float_toExponential     ns    244        555        556     1519      n/a    769            745      227       174       123
string_to_int           ns     16.8      113        114       50.5    n/a    154            157       55.5      14.1       3.49
string_to_float         ns     52.0      152        151       51.7    n/a    183            178       70.0      30.7      20.3
bigint32_arith          µs     18.5      175        179      113      n/a     89.1          123       17.6      19.8      20.5
bigint64_arith          µs     56.1      177        179      112      n/a    113            172       25.5      22.7       9.27
bigint256_arith         µs    195        192        199      124      n/a    211            272       58.7      35.6      22.4
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
| date | 2026-11-26T16:48:43+09:00 | 2026-11-26T16:51:28+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261126074323-b9ac5f60d84c | v0.0.0-20261126074323-b9ac5f60d84c |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/MoeForge/moejs | v0.1.1 | v0.1.1 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 1 | 1 |
| benchtime | 1s | 200ms |
| load-start | 2.08 2.43 3.29 | 3.80 2.80 3.27 |
| load-end | 3.80 2.80 3.27 | 3.14 3.02 3.24 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
