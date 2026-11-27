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
                                                 modernc.org/                  QuickJS  v8go
Benchmark     Ekuma  goja   Sobek  moejs  otto   quickjs       BeastJS  QJS    (C)      --jitless  v8go
Richards      1.00    5.83   5.93  1.04   36.5   5.04          0.928     4.89  1.45     1.07       0.0405
DeltaBlue     1.00    5.58   5.70  1.19   46.8   5.79          1.22      5.17  1.62     1.17       0.0150
Encrypt       1.00   10.7   10.8   1.27   56.3   5.97          0.939      n/a  1.63     1.40       0.0327
Decrypt       1.00   10.8   10.8   1.29   47.6   6.08          0.970      n/a  1.62     1.40       0.0303
RayTrace      1.00   10.3   10.5   1.15   34.6   6.31          1.06      7.94  1.65     0.878      0.0294
Earley        1.00    6.88   6.97  1.02   37.9   5.94          1.23       n/a  1.64     0.650      0.0409
Boyer         1.00    7.47   7.41  0.936  41.8   5.86          0.723      n/a  1.32     0.737      0.0492
RegExp        1.00    7.73   7.83  0.765    n/a  7.86          0.970    19.8   4.94     0.407      0.264
Splay         1.00    5.67   5.53  0.919  25.9   3.55          0.722     4.27  1.21     0.704      0.162
NavierStokes  1.00   13.3   13.6   0.951  57.5   4.35          0.880     8.58  1.55     1.73       0.108
Geomean       1.00    8.06   8.12  1.04   41.6   5.56          0.950     7.25  1.70     0.935      0.0527
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                                modernc.org/                    QuickJS  v8go
Benchmark     Unit  Ekuma   goja     Sobek    moejs   otto      quickjs       BeastJS  QJS      (C)      --jitless  v8go
Richards      ms      1.56     9.12     9.26    1.63     57.1      7.88         1.45      7.64     2.26    1.67       0.0633
DeltaBlue     ms      2.56    14.3     14.6     3.05    120       14.8          3.12     13.2      4.14    2.99       0.0383
Encrypt       ms      2.27    24.4     24.4     2.89    128       13.5          2.13       n/a     3.69    3.18       0.0742
Decrypt       ms     42.2    454      456      54.2    2005      256           40.9        n/a    68.2    59.2        1.28
RayTrace      ms     12.3    127      129      14.1     426       77.7         13.1      97.8     20.3    10.8        0.362
Earley        ms      2.35    16.2     16.4     2.40     89.1     14.0          2.89       n/a     3.85    1.53       0.0962
Boyer         ms     41.5    310      308      38.8    1734      243           30.0        n/a    54.8    30.6        2.04
RegExp        ms     28.5    220      223      21.8        n/a   224           27.6     565      141      11.6        7.50
Splay         µs    899     5097     4977     827     23307     3196          650      3837     1087     633        146
NavierStokes  ms     27.8    368      377      26.4    1597      121           24.4     239       43.0    47.9        3.00
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                               modernc.org/                  QuickJS  v8go
Benchmark               Ekuma  goja     Sobek    moejs   otto  quickjs       BeastJS  QJS    (C)      --jitless  v8go
empty_loop              1.00    11.8     11.9     1.33   n/a    4.22         0.698     6.18  1.24     1.76       0.163
empty_down_loop         1.00    11.8     11.8     1.34   n/a    4.17         0.681     6.05  1.23     1.55       0.162
empty_down_loop2        1.00    15.7     15.8     1.47   n/a    6.52         1.24     11.3   1.84     2.13       0.182
empty_do_loop           1.00    13.2     13.2     1.34   n/a    4.93         0.676     6.15  1.62     1.55       0.162
date_now                1.00     2.83     2.80    0.828  n/a    2.19         0.929     3.43  0.531    0.702      0.572
date_parse              1.00     1.80     1.78    1.16   n/a    5.78         0.820     4.49  1.61     2.16       2.03
prop_read               1.00    10.3     10.3     0.976  n/a    4.79         0.630     6.54  1.46     1.14       0.0630
prop_write              1.00    15.5     15.9     1.05   n/a    6.86         0.972    10.7   1.84     1.86       0.0598
prop_update             1.00     9.16     9.12    0.975  n/a    3.53         0.628     7.06  0.910    1.35       0.0562
prop_create             1.00     4.60     4.47    1.01   n/a    4.28         0.617     5.40  1.12     0.397      0.158
prop_clone              1.00    29.1     13.0     0.567  n/a    7.87         1.07      6.36  2.22     0.0761     0.0691
prop_delete             1.00    10.6     12.5     2.16   n/a    8.16         1.60      6.83  4.91     0.934      0.820
array_read              1.00    11.6     11.6     1.65   n/a    7.63         0.766    10.5   1.91     3.59       0.143
array_write             1.00    13.1     13.3     1.42   n/a    7.56         1.21     42.1   2.01     3.50       0.0740
array_update            1.00     7.22     7.32    1.21   n/a    2.87         0.766     7.76  0.808    1.72       0.0433
array_prop_create       1.00     9.16     9.02    1.73   n/a    2.00         0.659     6.69  0.869    1.01       0.140
array_slice             1.00    10.4      8.85    0.635  n/a   13.5          0.289     6.84  2.80     0.0333     0.0267
array_length_read       1.00    11.0     11.3     1.09   n/a    5.31         0.710     7.13  1.67     1.34       0.178
array_length_decr       1.00     4.06     3.98    2.08   n/a    3.99         0.436     2.94  0.737    0.892      0.619
array_hole_length_decr  1.00     3.95     3.87    1.84   n/a    4.88         1.29      4.38  1.44     0.828      0.555
array_push              1.00     6.81     6.36    0.921  n/a    3.54         0.484     4.59  0.811    0.540      0.0670
array_pop               1.00     5.36     5.36    2.09   n/a    9.68         0.988     5.02  1.49     0.614      0.0896
typed_array_read        1.00     7.05     7.10    1.36   n/a    5.82         1.05      4.90  1.09     1.49       0.0820
typed_array_write       1.00    12.5     12.7     3.00   n/a    6.79         1.56     20.0   1.79     2.16       0.0436
arguments_read          1.00    21.8     21.9     1.31   n/a    8.83         0.759      n/a  1.65     1.21       0.153
arguments_strict_read   1.00    21.0     21.2     1.30   n/a    6.59         0.764      n/a  1.41     1.16       0.194
global_read             1.00    10.5     10.2     1.10   n/a    3.56         0.558     6.43  1.63     1.03       0.0428
global_write            1.00     7.21     7.18    0.529  n/a    2.21         0.207     4.45  0.452    0.485      0.0256
global_write_strict     1.00    10.3      9.12    0.530  n/a    2.17         0.206     4.41  0.452    0.479      0.0256
local_destruct          1.00     6.46     6.52    0.952  n/a    9.66         1.04     31.5   1.52     1.49       0.0575
global_destruct         1.00     8.24     8.00    0.782  n/a    7.69         0.847     9.24  1.27     1.38       0.0948
global_destruct_strict  1.00     9.25     9.89    0.782  n/a    7.69         0.844     9.37  1.22     1.35       0.0889
global_func_call        1.00     4.24     4.22    0.966  n/a    4.05         0.386     3.20  0.834    0.978      0.00633
func_call               1.00     3.47     3.42    0.894  n/a    3.87         0.368     2.52  0.811    0.930      0.00662
func_closure_call       1.00     3.02     3.06    0.916  n/a    3.49         0.371     2.40  0.764    0.778      0.120
int_arith               1.00    10.9     10.3     1.04   n/a    4.05         0.530     5.64  1.28     1.29       0.289
float_arith             1.00    14.4     14.4     1.05   n/a    3.82         0.647     6.32  1.34     1.89       0.0817
map_set_string          1.00     2.97     2.98    0.697  n/a    3.69         1.00      3.39  0.953    0.410      0.154
map_set_int             1.00     3.29     3.24    0.888  n/a    3.63         0.834    58.0   0.946    0.685      0.194
map_set_bigint          1.00     3.63     3.68    1.30   n/a    4.12         0.742    77.0   0.729    1.02       0.638
map_delete              1.00     2.77     2.76    0.657  n/a    3.15         0.969      n/a  0.809    0.362      0.153
weak_map_set            1.00   930      823      13.0    n/a    1.37         0.326      n/a  0.285    0.250      0.116
weak_map_delete         1.00     7.31     8.34    0.338  n/a    1.16         0.216      n/a  0.271    0.205      0.0872
array_for               1.00     9.77    14.7     1.98   n/a    4.51         0.573     5.69  1.71     1.56       0.120
array_for_in            1.00    11.5     12.0     1.67   n/a    8.24         1.30      5.60  1.63     0.691      0.550
array_for_of            1.00     7.60     7.57    1.04   n/a   12.5          1.05      5.84  1.70     2.92       0.352
math_min                1.00     7.71     7.64    1.68   n/a    5.89         1.12      6.03  1.14     1.19       0.0194
regexp_ascii            1.00     6.14     6.12    1.15   n/a    2.43         0.923     6.83  0.725    0.425      0.193
regexp_utf16            1.00     5.51     5.61    1.25   n/a    2.20         0.961     6.46  0.764    0.276      0.181
regexp_replace          1.00     4.69     4.71    1.48   n/a    3.42         1.26     25.8   3.18     0.403      0.322
string_length           1.00    19.0     19.1     1.03   n/a    7.54         0.561     6.29  1.55     1.54       0.116
string_build1           1.00     1.92     1.93    0.757  n/a    0.917        0.240     2.82  0.315    0.277      0.0574
string_build1x          1.00     1.98     2.19    0.859  n/a    1.04         0.261     2.81  0.309    0.270      0.0568
string_build2c          1.00     1.98     1.85    0.728  n/a    0.926        0.262     5.89  0.415    0.265      0.0560
string_build2           1.00     1.76     1.74    0.683  n/a    2.16         0.288     2.81  0.884    0.258      0.0631
string_build3           1.00     1.82     1.84    0.523  n/a    2.12         0.611     2.55  0.573    0.256      0.0509
string_build4           1.00     1.91     1.93    0.754  n/a    2.49         0.814     3.03  0.944    0.262      0.0542
string_build_large1     1.00     1.75     1.74    0.616  n/a    2.52         0.215    29.8   0.790    0.241      0.164
string_build_large2     1.00     1.76     1.72    0.438  n/a    2.36         0.784    30.5   0.773    0.271      0.132
string_concat           1.00     2.03     2.08    2.08   n/a    2.74         0.716     4.44  0.779    0.245      0.215
string_array_join       1.00     2.69     2.82    1.01   n/a    3.68         0.558     3.27  1.10     0.440      0.413
string_template         1.00     2.27     2.25    4.24   n/a    4.92         1.03      9.26  1.18     0.860      0.429
int_to_string           1.00     3.41     3.43    1.25   n/a    4.25         0.976     4.38  0.978    0.591      0.165
int_toString            1.00     4.10     3.96    1.14   n/a    5.77         0.808     4.23  0.943    0.539      0.184
float_to_string         1.00     2.66     2.64    1.41   n/a    4.93         0.813     5.15  2.36     0.765      0.497
float_toString          1.00     2.36     2.36    1.31   n/a    5.51         0.755     5.20  2.18     0.712      0.469
float_toFixed           1.00     7.05     7.02    2.01   n/a    3.24         0.923     3.23  0.891    1.09       0.810
float_toPrecision       1.00     2.69     2.65    6.94   n/a    3.52         1.05      4.72  1.03     0.812      0.571
float_toExponential     1.00     2.33     2.37    6.52   n/a    3.24         0.981     3.15  0.965    0.743      0.515
string_to_int           1.00     7.17     7.91    3.01   n/a    8.89         2.86      9.33  3.41     0.917      0.208
string_to_float         1.00     3.60     2.97    0.986  n/a    4.01         2.94      3.34  1.31     0.588      0.389
bigint32_arith          1.00     9.65     9.64    6.38   n/a   12.7          1.78      6.77  0.964    1.08       0.149
bigint64_arith          1.00     2.95     3.02    1.81   n/a    1.85         0.866     2.65  0.719    0.372      0.344
bigint256_arith         1.00     0.921    0.922   0.583  n/a    0.997        0.474     1.39  0.279    0.171      0.104
Geomean                 1.00     5.92     5.87    1.23   n/a    4.01         0.711     6.27  1.07     0.715      0.136
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                            modernc.org/                     QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      moejs     otto  quickjs       BeastJS  QJS       (C)       --jitless  v8go
empty_loop              ns      3.30      39.0       39.4      4.38   n/a     13.9          2.30      20.4      4.09      5.79      0.537
empty_down_loop         ns      3.29      38.8       38.8      4.42   n/a     13.7          2.24      19.9      4.04      5.10      0.534
empty_down_loop2        ns      2.93      45.9       46.4      4.30   n/a     19.1          3.64      33.2      5.40      6.25      0.533
empty_do_loop           ns      3.28      43.3       43.3      4.39   n/a     16.2          2.22      20.2      5.32      5.09      0.533
date_now                ns     42.0      119        118       34.8    n/a     92.0         39.0      144       22.3      29.5      24.0
date_parse              µs      1.84       3.30       3.28     2.13   n/a     10.6          1.51       8.24     2.96      3.96      3.73
prop_read               ns     18.1      185        186       17.6    n/a     86.5         11.4      118       26.3      20.5       1.14
prop_write              ns      8.88     138        141        9.31   n/a     60.9          8.63      94.8     16.3      16.5       0.530
prop_update             ns     29.1      267        266       28.4    n/a    103           18.3      206       26.5      39.5       1.64
prop_create             ns    569       2615       2543      573      n/a   2435          351       3072      635       226        90.0
prop_clone              ns    349      10137       4533      198      n/a   2745          374       2217      774        26.5      24.1
prop_delete             ns    536       5693       6706     1159      n/a   4373          860       3663     2631       501       440
array_read              ns     33.7      391        391       55.6    n/a    257           25.8      353       64.2     121         4.80
array_write             ns     18.1      237        240       25.7    n/a    137           22.0      762       36.3      63.3       1.34
array_update            ns     76.7      554        562       92.5    n/a    220           58.7      595       61.9     132         3.32
array_prop_create       µs     13.3      122        120       23.0    n/a     26.5          8.75      88.9     11.5      13.4       1.86
array_slice             µs      4.16      43.3       36.8      2.64   n/a     56.1          1.20      28.4     11.6       0.138     0.111
array_length_read       ns     16.1      177        182       17.6    n/a     85.4         11.4      115       26.9      21.5       2.85
array_length_decr       µs     28.3      115        113       58.8    n/a    113           12.3       83.0     20.8      25.2      17.5
array_hole_length_decr  µs     30.9      122        119       56.8    n/a    151           39.7      135       44.4      25.5      17.1
array_push              µs     13.6       92.6       86.6     12.5    n/a     48.2          6.58      62.5     11.0       7.35      0.911
array_pop               µs     13.2       70.9       70.9     27.7    n/a    128           13.1       66.5     19.8       8.13      1.19
typed_array_read        ns     56.7      399        402       77.1    n/a    330           59.7      278       61.7      84.3       4.65
typed_array_write       ns     30.5      380        389       91.5    n/a    207           47.8      610       54.8      66.0       1.33
arguments_read          ns    164       3575       3589      214      n/a   1447          124           n/a   271       198        25.0
arguments_strict_read   ns    165       3465       3497      215      n/a   1088          126           n/a   232       191        32.0
global_read             ns     15.9      167        163       17.5    n/a     56.8          8.89     102       26.0      16.3       0.681
global_write            ns     31.1      224        223       16.4    n/a     68.8          6.42     138       14.1      15.1       0.795
global_write_strict     ns     31.1      321        284       16.5    n/a     67.6          6.41     137       14.1      14.9       0.797
local_destruct          ns    122        788        796      116      n/a   1179          127       3838      185       181         7.02
global_destruct         ns    138       1135       1102      108      n/a   1059          117       1273      175       190        13.1
global_destruct_strict  ns    142       1314       1405      111      n/a   1092          120       1331      173       191        12.6
global_func_call        ns     71.5      303        302       69.0    n/a    289           27.6      229       59.6      69.9       0.452
func_call               ns     69.9      243        240       62.5    n/a    271           25.7      176       56.7      65.0       0.463
func_closure_call       ns     78.3      236        239       71.7    n/a    273           29.1      188       59.8      60.9       9.41
int_arith               µs      7.53      81.8       77.5      7.85   n/a     30.5          3.99      42.5      9.60      9.71      2.17
float_arith             µs      9.40     136        135        9.90   n/a     35.9          6.08      59.4     12.6      17.8       0.768
map_set_string          µs    140        417        419       97.9    n/a    518          141        476      134        57.6      21.6
map_set_int             µs     82.5      272        268       73.3    n/a    299           68.9     4786       78.1      56.6      16.0
map_set_bigint          µs    115        419        425      150      n/a    476           85.7     8889       84.2     118        73.7
map_delete              µs    164        455        453      108      n/a    518          159           n/a   133        59.5      25.2
weak_map_set            µs    162     150758     133477     2113      n/a    222           52.9         n/a    46.2      40.5      18.8
weak_map_delete         µs    412       3011       3432      139      n/a    479           89.1         n/a   112        84.4      35.9
array_for               ns    844       8245      12409     1674      n/a   3806          484       4799     1446      1319       101
array_for_in            µs      2.12      24.3       25.4      3.53   n/a     17.4          2.74      11.8      3.45      1.46      1.16
array_for_of            ns    788       5989       5963      820      n/a   9808          823       4600     1340      2298       277
math_min                µs     14.0      108        107       23.5    n/a     82.2         15.7       84.2     15.9      16.6       0.271
regexp_ascii            µs    149        914        911      172      n/a    362          137       1016      108        63.2      28.7
regexp_utf16            µs    169        934        950      212      n/a    372          163       1095      129        46.8      30.7
regexp_replace          µs    193        907        910      287      n/a    660          244       4983      615        78.0      62.2
string_length           ns     17.5      333        335       18.0    n/a    132            9.82     110       27.1      27.0       2.03
string_build1           µs     39.1       74.9       75.3     29.6    n/a     35.9          9.39     110       12.3      10.8       2.24
string_build1x          µs     39.3       78.0       86.2     33.8    n/a     41.0         10.3      111       12.2      10.6       2.23
string_build2c          µs     40.2       79.5       74.3     29.3    n/a     37.2         10.5      236       16.6      10.7       2.25
string_build2           µs     42.1       74.3       73.5     28.8    n/a     91.0         12.1      118       37.2      10.9       2.66
string_build3           µs     45.7       83.3       84.1     23.9    n/a     97.1         27.9      117       26.2      11.7       2.33
string_build4           µs     41.7       79.6       80.4     31.4    n/a    104           33.9      126       39.4      10.9       2.26
string_build_large1     µs    902       1579       1570      556      n/a   2268          194      26842      712       217       148
string_build_large2     ms      1.05       1.85       1.81     0.461  n/a      2.48         0.824     32.1      0.813     0.285     0.139
string_concat           ns    771       1567       1602     1600      n/a   2110          552       3427      600       189       166
string_array_join       ns    535       1440       1507      540      n/a   1969          299       1749      589       236       221
string_template         ns    301        683        678     1276      n/a   1482          310       2788      357       259       129
int_to_string           ns     63.4      216        217       79.5    n/a    269           61.9      278       62.0      37.5      10.4
int_toString            ns     89.0      365        352      102      n/a    514           71.9      377       84.0      48.0      16.4
float_to_string         ns    174        461        458      244      n/a    856          141        894      409       133        86.2
float_toString          ns    197        464        464      258      n/a   1083          148       1022      429       140        92.2
float_toFixed           ns    220       1547       1541      442      n/a    711          202        708      196       239       178
float_toPrecision       ns    220        590        583     1524      n/a    773          231       1036      226       178       125
float_toExponential     ns    240        559        567     1562      n/a    775          235        754      231       178       123
string_to_int           ns     17.0      122        134       51.1    n/a    151           48.7      159       58.1      15.6       3.53
string_to_float         ns     53.9      194        160       53.1    n/a    216          158        180       70.5      31.7      20.9
bigint32_arith          µs     18.6      180        180      119      n/a    237           33.2      126       18.0      20.2       2.77
bigint64_arith          µs     61.5      182        186      111      n/a    114           53.3      163       44.3      22.9      21.2
bigint256_arith         µs    215        198        198      125      n/a    214          102        299       59.8      36.7      22.4
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
| date | 2026-11-27T16:36:55+09:00 | 2026-11-27T16:39:53+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.2.0-alpha.0.20261127071217-256de7e5b42d | v0.2.0-alpha.0.20261127071217-256de7e5b42d |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/MoeForge/moejs | v0.1.1 | v0.1.1 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/liustack/beastjs | v0.33.2 | v0.33.2 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 1 | 1 |
| benchtime | 1s | 200ms |
| load-start | 5.38 5.99 5.53 | 3.18 4.65 5.05 |
| load-end | 3.18 4.65 5.05 | 8.42 5.00 4.93 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
