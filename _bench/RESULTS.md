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
Richards      1.00    6.88   6.90  1.03   36.8    4.80          4.73  1.44     1.04       0.0305
DeltaBlue     1.00    5.70   5.67  1.42   48.3    5.74          5.23  1.64     1.19       0.0141
Encrypt       1.00   10.4   10.4   1.24   46.2    5.97           n/a  2.01     1.51       0.0313
Decrypt       1.00   11.7    9.65  1.14   43.2    5.44           n/a  1.44     1.25       0.0316
RayTrace      1.00    9.29   9.38  1.04   31.2    5.59          7.12  1.50     0.792      0.0261
Earley        1.00    5.23   5.34  0.919  28.9    4.32           n/a  1.26     0.497      0.0311
Boyer         1.00    6.24   6.41  0.775  35.9    4.99           n/a  1.17     0.630      0.0411
RegExp        1.00    9.94   9.99  0.986    n/a  10.4          26.1   6.53     0.598      0.196
Splay         1.00    3.62   3.58  0.610  16.7    2.29          2.74  0.737    0.428      0.0775
NavierStokes  1.00   11.6   11.5   0.848  48.8    4.38          7.58  1.38     1.53       0.0910
Geomean       1.00    7.55   7.43  0.975  35.7    5.07          6.76  1.58     0.862      0.0426
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                            modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   moejs   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.60   11.0    11.1    1.65     59.0     7.70          7.59    2.31    1.67      0.0489
DeltaBlue     ms     2.50   14.2    14.1    3.53    120      14.3          13.1     4.08    2.97      0.0351
Encrypt       ms     2.33   24.1    24.2    2.88    108      13.9            n/a    4.69    3.53      0.0730
Decrypt       ms    47.1   552     454     53.6    2033     256              n/a   67.8    58.9       1.49
RayTrace      ms    13.7   127     129     14.2     427      76.7          97.7    20.5    10.9       0.358
Earley        ms     3.08   16.1    16.4    2.83     89.0    13.3            n/a    3.89    1.53      0.0958
Boyer         ms    48.0   300     308     37.2    1726     240              n/a   56.0    30.3       1.97
RegExp        ms    21.5   214     215     21.2        n/a  223           561     140      12.9       4.22
Splay         ms     1.39    5.03    4.98   0.849    23.3     3.18          3.81    1.02    0.596     0.108
NavierStokes  ms    31.1   362     357     26.4    1520     136           236      42.9    47.6       2.83
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                             modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja    Sobek   moejs   otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00    12.3    12.1    1.38   n/a    4.95         11.2   3.53     2.25       0.174
empty_down_loop         1.00    11.8    11.8    1.33   n/a    4.18          6.07  1.28     1.75       0.163
empty_down_loop2        1.00    15.6    16.1    1.46   n/a    6.42         11.3   1.81     2.11       0.182
empty_do_loop           1.00    13.3    13.1    1.33   n/a    4.84          5.97  1.61     1.54       0.163
date_now                1.00     2.82    2.84   0.831  n/a    2.17          3.54  0.529    0.691      0.575
date_parse              1.00     1.76    1.78   1.00   n/a    4.73          3.80  1.80     2.19       2.05
prop_read               1.00    10.2    10.4    0.967  n/a    4.68          6.67  1.47     1.14       0.0616
prop_write              1.00    15.5    14.6    0.978  n/a    6.17          9.90  1.71     1.68       0.0961
prop_update             1.00     9.23    9.20   0.975  n/a    3.32          6.93  0.909    1.35       0.0538
prop_create             1.00     4.25    4.37   0.990  n/a    2.86          4.61  0.962    0.389      0.156
prop_clone              1.00    20.0    20.2    1.08   n/a   15.4          12.7   4.11     0.146      0.118
prop_delete             1.00     9.71    9.74   2.06   n/a    8.05          6.71  2.16     0.958      0.772
array_read              1.00    11.6    11.5    1.65   n/a    4.93          8.41  1.79     2.13       0.136
array_write             1.00    13.0    13.2    1.39   n/a    7.81         35.1   1.83     3.56       0.0927
array_update            1.00     7.51    7.57   1.06   n/a    2.75          8.05  0.846    1.69       0.0433
array_prop_create       1.00     8.20    8.43   1.73   n/a    2.01          6.79  0.884    1.03       0.184
array_slice             1.00    10.0     9.87   0.685  n/a   15.2           6.97  2.79     0.0298     0.0239
array_length_read       1.00    11.0    11.2    1.10   n/a    4.94          7.10  1.63     1.29       0.176
array_length_decr       1.00     2.28    2.29   1.16   n/a    2.29          1.68  0.420    0.519      0.348
array_hole_length_decr  1.00     4.02    4.07   2.01   n/a    4.97          4.23  1.42     0.793      0.541
array_push              1.00     6.63    6.68   0.904  n/a    3.48          4.70  0.841    0.541      0.0668
array_pop               1.00     5.38    5.38   2.09   n/a    9.55          4.85  1.46     0.658      0.0918
typed_array_read        1.00     7.21    7.26   1.36   n/a    5.99          4.97  1.12     1.50       0.0810
typed_array_write       1.00    12.4    12.6    2.89   n/a    6.68         19.8   1.75     2.15       0.114
arguments_read          1.00    19.3    19.3    1.17   n/a    7.77           n/a  1.47     1.05       0.171
arguments_strict_read   1.00    18.7    18.8    1.17   n/a    5.83           n/a  1.24     1.03       0.136
global_read             1.00    10.7    10.3    1.11   n/a    3.53          6.41  1.64     1.02       0.0428
global_write            1.00     9.91    7.71   0.498  n/a    2.24          4.43  0.454    0.481      0.0267
global_write_strict     1.00    10.1     9.03   0.495  n/a    2.23          4.40  0.452    0.484      0.0258
local_destruct          1.00     6.26    6.27   0.910  n/a    9.25         29.6   1.45     1.40       0.0542
global_destruct         1.00     8.01    8.21   0.773  n/a    7.71          9.37  1.18     1.35       0.0915
global_destruct_strict  1.00     8.84    9.28   0.753  n/a    7.58          9.12  1.20     1.33       0.0911
global_func_call        1.00     4.27    4.19   0.963  n/a    4.09          3.23  0.838    1.03       0.0175
func_call               1.00     3.65    3.65   0.948  n/a    4.11          2.72  0.858    0.981      0.00731
func_closure_call       1.00     3.20    3.19   0.966  n/a    3.71          2.52  0.828    0.849      0.0464
int_arith               1.00    10.7    10.8    1.07   n/a    4.06          5.69  1.27     1.28       0.0461
float_arith             1.00    14.8    14.9    1.07   n/a    3.90          6.60  1.40     2.00       0.0776
map_set_string          1.00     2.89    2.93   0.680  n/a    3.57          3.32  0.930    0.396      0.254
map_set_int             1.00     3.58    3.56   0.976  n/a    3.89         60.7   1.04     0.735      0.185
map_set_bigint          1.00     3.70    3.71   1.33   n/a    4.28         79.5   0.767    0.991      0.664
map_delete              1.00     2.73    2.66   0.629  n/a    3.08           n/a  0.784    0.357      0.153
weak_map_set            1.00   789     708     13.9    n/a    1.39           n/a  0.290    0.254      0.118
weak_map_delete         1.00     5.45    6.52   0.281  n/a    0.970          n/a  0.229    0.171      0.0727
array_for               1.00     8.92    8.85   1.03   n/a    3.88          5.89  1.36     1.53       0.126
array_for_in            1.00    11.0    11.4    1.70   n/a    8.29          5.62  1.60     0.686      0.546
array_for_of            1.00     7.72    7.53   1.04   n/a   12.5           6.23  1.74     2.90       0.356
math_min                1.00     7.66    7.42   1.60   n/a    5.40          5.66  1.08     1.15       0.0193
regexp_ascii            1.00     5.85    6.00   1.14   n/a    2.42          6.58  0.719    0.292      0.191
regexp_utf16            1.00     5.05    7.07   1.28   n/a    2.09          6.15  0.718    0.271      0.175
regexp_replace          1.00     4.77    4.68   1.46   n/a    3.42         25.7   3.13     0.404      0.328
string_length           1.00    19.4    19.2    1.02   n/a    7.58          6.27  1.52     1.53       0.156
string_build1           1.00     1.86    1.92   0.728  n/a    0.900         2.72  0.308    0.273      0.0559
string_build1x          1.00     1.92    1.88   0.739  n/a    0.908         2.82  0.315    0.271      0.0572
string_build2c          1.00     1.82    1.80   0.713  n/a    0.883         2.99  0.340    0.253      0.0550
string_build2           1.00     1.88    1.88   0.733  n/a    2.30          3.03  0.960    0.277      0.0571
string_build3           1.00     1.84    1.87   0.525  n/a    2.15          2.57  0.582    0.257      0.0497
string_build4           1.00     1.88    1.88   0.712  n/a    2.43          2.84  0.917    0.259      0.0559
string_build_large1     1.00     1.80    1.84   0.629  n/a    2.57         30.2   0.798    0.242      0.172
string_build_large2     1.00     1.76    1.74   0.431  n/a    2.26         26.1   0.671    0.233      0.0435
string_concat           1.00     2.16    2.16   2.17   n/a    2.87          4.66  0.817    0.253      0.225
string_array_join       1.00     2.61    2.80   0.974  n/a    3.66          3.19  1.08     0.432      0.402
string_template         1.00     1.98    2.09   3.91   n/a    4.45          8.35  1.06     0.738      0.579
int_to_string           1.00     3.46    3.51   1.25   n/a    4.31          4.48  1.03     0.598      0.168
int_toString            1.00     4.00    4.03   1.14   n/a    5.48          4.29  0.991    0.539      0.190
float_to_string         1.00     2.67    2.66   1.44   n/a    5.03          5.25  2.39     0.747      0.491
float_toString          1.00     2.29    2.31   1.27   n/a    5.44          5.18  2.12     0.664      0.456
float_toFixed           1.00     6.87    6.99   1.91   n/a    4.46          3.17  0.856    1.06       0.807
float_toPrecision       1.00     2.52    2.50   6.54   n/a    3.29          3.24  1.01     0.778      0.547
float_toExponential     1.00     2.30    2.28   6.46   n/a    3.24          3.11  0.928    0.723      0.510
string_to_int           1.00     6.76    6.72   2.98   n/a    8.92          9.42  3.35     0.832      0.207
string_to_float         1.00     2.91    2.89   0.975  n/a    3.54          3.42  1.34     0.591      0.434
bigint32_arith          1.00     9.59    9.77   6.10   n/a    4.64          6.53  0.953    1.08       1.12
bigint64_arith          1.00     3.12    3.14   1.95   n/a    1.97          2.95  0.444    0.396      0.161
bigint256_arith         1.00     1.00    1.02   0.635  n/a    1.10          1.40  0.302    0.191      0.114
Geomean                 1.00     5.75    5.77   1.21   n/a    3.88          6.17  1.05     0.702      0.137
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                            modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      moejs     otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      3.19      39.3       38.6      4.40   n/a     15.8           35.7     11.3       7.18      0.554
empty_down_loop         ns      3.33      39.4       39.2      4.44   n/a     13.9           20.2      4.26      5.82      0.543
empty_down_loop2        ns      2.96      46.0       47.6      4.33   n/a     19.0           33.3      5.36      6.23      0.539
empty_do_loop           ns      3.31      44.1       43.5      4.40   n/a     16.0           19.8      5.32      5.11      0.539
date_now                ns     42.1      119        119       34.9    n/a     91.4          149       22.2      29.1      24.2
date_parse              µs      2.20       3.87       3.92     2.21   n/a     10.4            8.38     3.96      4.83      4.51
prop_read               ns     18.2      185        189       17.6    n/a     85.1          121       26.7      20.7       1.12
prop_write              ns      9.62     150        140        9.41   n/a     59.3           95.2     16.4      16.2       0.924
prop_update             ns     29.4      272        271       28.7    n/a     97.7          204       26.7      39.6       1.58
prop_create             ns    586       2490       2561      581      n/a   1674           2705      564       228        91.6
prop_clone              ns    181       3626       3666      196      n/a   2791           2309      745        26.5      21.4
prop_delete             ns    535       5195       5211     1104      n/a   4305           3589     1155       513       413
array_read              ns     34.0      394        393       56.0    n/a    168            286       60.9      72.6       4.63
array_write             ns     17.3      225        227       24.0    n/a    135            606       31.6      61.4       1.60
array_update            ns     74.4      559        563       78.8    n/a    204            598       62.9     126         3.22
array_prop_create       µs     13.2      108        111       22.8    n/a     26.4           89.4     11.6      13.5       2.42
array_slice             µs      3.76      37.8       37.2      2.58   n/a     57.1           26.2     10.5       0.112     0.0901
array_length_read       ns     16.1      177        181       17.6    n/a     79.4          114       26.2      20.8       2.83
array_length_decr       µs     49.9      113        114       57.6    n/a    114             83.8     21.0      25.9      17.4
array_hole_length_decr  µs     32.3      130        131       65.0    n/a    160            136       46.0      25.6      17.4
array_push              µs     13.8       91.3       92.0     12.4    n/a     48.0           64.8     11.6       7.44      0.920
array_pop               µs     13.4       72.1       72.2     28.0    n/a    128             65.1     19.5       8.82      1.23
typed_array_read        ns     56.3      406        409       76.7    n/a    337            280       63.3      84.1       4.56
typed_array_write       ns     31.1      384        392       89.9    n/a    208            615       54.3      66.9       3.55
arguments_read          ns    189       3647       3643      221      n/a   1468               n/a   278       198        32.4
arguments_strict_read   ns    188       3512       3532      221      n/a   1096               n/a   233       193        25.6
global_read             ns     16.0      171        165       17.8    n/a     56.6          103       26.2      16.4       0.685
global_write            ns     31.2      309        240       15.5    n/a     69.8          138       14.2      15.0       0.831
global_write_strict     ns     31.3      317        282       15.5    n/a     69.8          138       14.1      15.1       0.805
local_destruct          ns    130        814        814      118      n/a   1201           3841      188       182         7.04
global_destruct         ns    140       1124       1152      108      n/a   1083           1315      166       190        12.8
global_destruct_strict  ns    141       1244       1306      106      n/a   1067           1283      168       188        12.8
global_func_call        ns     69.0      295        290       66.5    n/a    282            223       57.9      71.0       1.21
func_call               ns     66.3      242        242       62.9    n/a    272            181       56.9      65.0       0.484
func_closure_call       ns     74.7      239        239       72.2    n/a    277            188       61.9      63.5       3.46
int_arith               µs      7.28      78.0       78.9      7.77   n/a     29.6           41.4      9.24      9.33      0.336
float_arith             µs      8.75     130        130        9.38   n/a     34.1           57.8     12.2      17.5       0.679
map_set_string          µs    141        408        415       96.1    n/a    504            469      131        56.0      36.0
map_set_int             µs     74.4      267        265       72.6    n/a    290           4518       77.7      54.7      13.8
map_set_bigint          µs    109        404        405      145      n/a    467           8671       83.7     108        72.4
map_delete              µs    163        445        433      103      n/a    501               n/a   128        58.1      25.0
weak_map_set            µs    155     122487     110012     2160      n/a    216               n/a    45.0      39.5      18.3
weak_map_delete         µs    477       2599       3110      134      n/a    463               n/a   109        81.5      34.7
array_for               ns    798       7117       7062      825      n/a   3097           4700     1089      1218       100
array_for_in            µs      2.12      23.4       24.3      3.60   n/a     17.6           11.9      3.39      1.46      1.16
array_for_of            ns    782       6037       5887      816      n/a   9768           4875     1362      2267       279
math_min                µs     14.7      113        109       23.6    n/a     79.5           83.2     15.8      16.9       0.284
regexp_ascii            µs    153        894        916      174      n/a    370           1006      110        44.6      29.2
regexp_utf16            µs    182        921       1289      234      n/a    381           1121      131        49.3      32.0
regexp_replace          µs    193        922        905      282      n/a    660           4970      605        78.0      63.3
string_length           ns     17.3      335        332       17.6    n/a    131            108       26.3      26.5       2.70
string_build1           µs     39.3       72.9       75.6     28.6    n/a     35.4          107       12.1      10.7       2.20
string_build1x          µs     38.7       74.3       72.6     28.6    n/a     35.1          109       12.2      10.5       2.21
string_build2c          µs     40.7       74.2       73.3     29.0    n/a     35.9          121       13.8      10.3       2.23
string_build2           µs     38.8       72.8       72.8     28.4    n/a     89.3          117       37.2      10.7       2.21
string_build3           µs     44.6       82.0       83.4     23.4    n/a     96.0          115       26.0      11.4       2.21
string_build4           µs     42.1       79.1       79.3     30.0    n/a    102            120       38.6      10.9       2.35
string_build_large1     µs    871       1567       1598      548      n/a   2234          26257      694       211       149
string_build_large2     ms      1.05       1.85       1.83     0.453  n/a      2.37          27.4      0.705     0.245     0.0458
string_concat           ns    743       1603       1607     1610      n/a   2129           3460      607       188       167
string_array_join       ns    548       1433       1536      534      n/a   2004           1747      592       237       221
string_template         ns    332        656        693     1296      n/a   1477           2771      352       245       192
int_to_string           ns     62.0      215        218       77.7    n/a    268            278       63.9      37.1      10.4
int_toString            ns     86.8      347        350       98.8    n/a    476            373       86.0      46.8      16.5
float_to_string         ns    169        452        450      244      n/a    852            889      404       127        83.2
float_toString          ns    197        452        456      252      n/a   1074           1022      419       131        90.0
float_toFixed           ns    222       1522       1550      423      n/a    989            703      190       235       179
float_toPrecision       ns    232        586        580     1520      n/a    765            754      236       181       127
float_toExponential     ns    240        554        548     1552      n/a    778            748      223       174       123
string_to_int           ns     16.8      114        113       50.1    n/a    150            158       56.2      14.0       3.47
string_to_float         ns     52.0      151        150       50.6    n/a    184            178       69.9      30.7      22.6
bigint32_arith          µs     18.5      177        181      113      n/a     85.8          121       17.6      20.0      20.7
bigint64_arith          µs     56.9      178        179      111      n/a    112            168       25.2      22.5       9.13
bigint256_arith         µs    194        194        198      123      n/a    213            272       58.5      37.0      22.1
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
| date | 2026-11-26T19:41:14+09:00 | 2026-11-26T19:43:59+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261126103810-5acd587489b5 | v0.0.0-20261126103810-5acd587489b5 |
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
| load-start | 5.01 4.60 4.27 | 2.83 3.91 4.05 |
| load-end | 2.83 3.91 4.05 | 3.61 3.67 3.89 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
