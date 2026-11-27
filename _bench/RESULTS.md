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
Richards      1.00    5.61   5.81  0.983  37.9    4.73         0.894     4.34  1.28     0.955      0.0283
DeltaBlue     1.00    5.74   5.67  1.24   48.4    5.71         1.32      5.94  1.66     1.27       0.0143
Encrypt       1.00   10.7   10.3   1.23   44.6    5.79         0.925      n/a  1.58     1.37       0.0317
Decrypt       1.00   10.8   10.4   1.28   46.7    5.90         0.947      n/a  2.05     1.52       0.0306
RayTrace      1.00    9.88  10.2   1.08   32.4    5.93         1.12      7.67  1.63     0.934      0.0354
Earley        1.00    7.16   6.58  1.05   39.2    5.66         1.27       n/a  1.70     0.643      0.0467
Boyer         1.00    7.62   7.41  0.914  43.5    5.81         0.758      n/a  1.32     0.735      0.0474
RegExp        1.00   10.0   12.4   1.01     n/a  11.0          1.12     25.9   6.55     0.538      0.196
Splay         1.00    5.60   5.79  1.03   42.7    3.83         0.813     4.85  1.13     0.671      0.119
NavierStokes  1.00   13.7   13.4   0.971  61.1    4.72         0.989     8.39  1.53     1.82       0.108
Geomean       1.00    8.30   8.38  1.07   43.4    5.69         1.00      7.70  1.76     0.970      0.0489
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                               modernc.org/                    QuickJS  v8go
Benchmark     Unit  Ekuma   goja     Sobek   moejs   otto      quickjs       BeastJS  QJS      (C)      --jitless  v8go
Richards      ms      1.75     9.82    10.2    1.72     66.3      8.27         1.56      7.60     2.24    1.67       0.0494
DeltaBlue     ms      2.51    14.4     14.2    3.10    121       14.3          3.31     14.9      4.16    3.18       0.0358
Encrypt       ms      2.41    25.8     24.9    2.97    107       14.0          2.23       n/a     3.82    3.30       0.0764
Decrypt       ms     44.3    478      462     56.7    2069      261           42.0        n/a    90.9    67.4        1.36
RayTrace      ms     12.9    128      132     14.0     419       76.7         14.4      99.0     21.1    12.1        0.458
Earley        ms      2.38    17.1     15.7    2.51     93.3     13.5          3.03       n/a     4.06    1.53       0.111
Boyer         ms     43.0    328      319     39.3    1873      250           32.6        n/a    57.0    31.6        2.04
RegExp        ms     22.3    224      277     22.5        n/a   246           25.1     578      146      12.0        4.38
Splay         µs    943     5280     5464    972     40270     3608          767      4569     1065     633        112
NavierStokes  ms     28.4    389      380     27.6    1738      134           28.1     238       43.4    51.9        3.06
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                                 modernc.org/                  QuickJS  v8go
Benchmark               Ekuma  goja      Sobek     moejs   otto  quickjs       BeastJS  QJS    (C)      --jitless  v8go
empty_loop              1.00     11.7      11.9     1.29   n/a    4.07         0.675     6.32  1.26     1.74       0.158
empty_down_loop         1.00     11.7      11.7     1.43   n/a    4.19         0.684     6.06  1.25     1.56       0.211
empty_down_loop2        1.00     14.6      14.4     1.47   n/a    5.91         1.19     10.8   1.87     1.97       0.169
empty_do_loop           1.00     13.4      13.9     1.33   n/a    4.95         0.678     6.13  2.22     1.53       0.163
date_now                1.00      2.83      2.80    0.840  n/a    2.24         0.964     3.50  0.536    0.780      0.582
date_parse              1.00      1.79      1.81    1.17   n/a    5.71         0.795     4.48  1.62     2.15       2.02
prop_read               1.00     10.3      10.1     0.987  n/a    4.66         0.617     6.64  1.49     1.36       0.0600
prop_write              1.00     15.3      14.7     0.974  n/a    6.17         0.933    10.0   1.74     1.72       0.0637
prop_update             1.00      9.37      9.34    0.984  n/a    3.42         0.685     7.23  0.944    1.37       0.0591
prop_create             1.00      4.37      4.54    1.01   n/a    2.88         0.622     4.70  0.972    0.392      0.155
prop_clone              1.00     20.9      20.7     1.12   n/a   15.2          2.10     12.3   4.33     0.171      0.109
prop_delete             1.00      9.78      9.73    2.05   n/a    8.00         1.67      6.61  2.10     0.930      0.757
array_read              1.00     11.3      11.2     1.59   n/a    4.82         0.701     8.13  1.75     2.07       0.129
array_write             1.00     12.7      12.9     1.33   n/a    7.47         1.22     34.0   1.76     3.43       0.0753
array_update            1.00      7.37      7.36    1.10   n/a    2.77         0.737     7.74  0.868    1.69       0.0425
array_prop_create       1.00      8.55      8.33    1.80   n/a    2.09         0.624     6.90  0.930    1.03       0.141
array_slice             1.00      9.88     10.0     0.690  n/a   15.3          0.322     6.89  2.77     0.0298     0.0222
array_length_read       1.00     11.2      11.3     1.10   n/a    5.07         0.714     7.08  1.63     1.32       0.179
array_length_decr       1.00      7.74     10.3     6.29   n/a   20.0          0.996     5.01  1.27     1.53       1.04
array_hole_length_decr  1.00      6.08      6.01    2.75   n/a    7.17         1.91      6.83  2.31     1.24       0.834
array_push              1.00      6.46      6.39    0.876  n/a    3.37         0.453     4.32  0.761    0.682      0.0775
array_pop               1.00      5.57      5.48    2.16   n/a    9.65         0.992     5.03  1.46     0.613      0.0885
typed_array_read        1.00      6.86      6.91    1.31   n/a    5.65         1.05      4.81  1.06     1.48       0.0751
typed_array_write       1.00     11.6      18.7     2.69   n/a    6.30         1.48     18.5   1.67     2.00       0.0398
arguments_read          1.00     21.7      30.5     2.04   n/a   10.9          0.910      n/a  2.01     1.50       0.202
arguments_strict_read   1.00     20.8      20.9     1.24   n/a    6.69         0.772      n/a  1.42     1.21       0.154
global_read             1.00      9.65      9.35    1.02   n/a    3.23         0.528     5.92  1.43     0.956      0.0396
global_write            1.00     30.3      32.5     2.10   n/a    8.78         0.822    17.4   1.81     1.91       0.101
global_write_strict     1.00     36.3      37.1     2.15   n/a    8.85         0.828    17.4   1.81     1.92       0.101
local_destruct          1.00      6.23      6.22    0.681  n/a    6.92         0.748    22.4   1.23     1.24       0.0437
global_destruct         1.00      6.82      7.07    0.513  n/a    5.00         0.536     5.94  0.774    0.962      0.0662
global_destruct_strict  1.00      8.91      8.76    0.741  n/a    7.34         0.828     8.81  1.50     1.29       0.0847
global_func_call        1.00      5.28      4.50    1.00   n/a    4.10         0.433     3.16  0.834    0.945      0.00645
func_call               1.00      3.56      3.62    0.912  n/a    4.21         0.382     2.66  0.852    0.973      0.00654
func_closure_call       1.00      3.07      4.00    0.929  n/a    3.55         0.375     2.42  0.768    0.806      0.121
int_arith               1.00     10.7      10.7     1.07   n/a    4.01         0.539     5.64  1.29     1.32       0.0477
float_arith             1.00     15.2      15.0     0.935  n/a    3.50         0.602     5.81  1.22     1.99       0.0824
map_set_string          1.00      2.94      2.90    0.700  n/a    4.17         0.974     3.31  0.945    0.399      0.150
map_set_int             1.00      2.54      2.53    0.702  n/a    2.75         0.619    42.8   0.729    0.528      0.131
map_set_bigint          1.00      3.41      3.43    1.20   n/a    3.88         0.869    73.5   0.715    0.923      0.605
map_delete              1.00      2.76      2.72    0.655  n/a    3.13         0.955      n/a  0.795    0.534      0.158
weak_map_set            1.00   1072      1098      15.3    n/a    1.45         0.340      n/a  0.299    0.262      0.117
weak_map_delete         1.00      6.38      7.43    0.337  n/a    1.16         0.217      n/a  0.268    0.206      0.0871
array_for               1.00      9.00      8.98    1.06   n/a    3.93         0.582     6.08  1.39     1.56       0.126
array_for_in            1.00     11.1      11.3     1.64   n/a    8.20         1.35      5.72  1.65     0.701      0.641
array_for_of            1.00     26.7       7.76    1.04   n/a   12.4          1.07      6.07  2.01     2.99       0.340
math_min                1.00      7.51      7.57    1.68   n/a    5.61         1.11      5.95  1.12     1.17       0.0191
regexp_ascii            1.00      6.12      6.10    1.16   n/a    2.49         0.952     6.95  0.761    0.318      0.200
regexp_utf16            1.00      5.33      5.35    1.22   n/a    2.13         0.921     6.14  0.925    0.266      0.172
regexp_replace          1.00      4.71      5.78    2.19   n/a    3.65         1.26     29.7   4.03     0.430      0.319
string_length           1.00     19.1      19.1     1.02   n/a    7.48         0.563     6.28  1.56     1.60       0.116
string_build1           1.00      4.94      4.85    1.86   n/a    2.42         0.627     7.16  0.790    0.714      0.146
string_build1x          1.00      5.60      5.70    2.93   n/a    2.92         0.721     8.39  0.925    0.847      0.180
string_build2c          1.00      5.77      6.04    2.37   n/a    2.76         0.709    12.1   1.30     0.823      0.172
string_build2           1.00      5.65      4.97    1.98   n/a    6.06         0.696     7.74  2.48     0.708      0.188
string_build3           1.00      1.89      1.86    0.526  n/a    2.12         0.592     2.51  0.577    0.256      0.0487
string_build4           1.00      2.50      2.52    0.974  n/a    3.30         1.08      3.81  1.23     0.344      0.0697
string_build_large1     1.00      4.89      5.12    1.76   n/a    7.26         0.627    83.6   2.20     0.690      0.458
string_build_large2     1.00      1.76      1.80    0.437  n/a    2.23         0.682    25.7   0.673    0.234      0.133
string_concat           1.00      2.28      2.19    2.18   n/a    2.85         0.741     4.69  0.825    0.253      0.226
string_array_join       1.00      8.01      9.60    2.09   n/a    6.70         1.01      6.10  2.04     0.810      0.828
string_template         1.00      2.24      2.03    3.53   n/a    4.16         0.953     7.64  0.971    0.671      0.351
int_to_string           1.00      3.44      3.40    1.25   n/a    4.28         1.40      4.44  1.01     0.590      0.164
int_toString            1.00      4.08      4.10    1.15   n/a    5.59         0.815     4.39  0.976    0.562      0.193
float_to_string         1.00      2.63      2.61    1.40   n/a    4.93         0.820     5.24  2.43     0.733      0.477
float_toString          1.00      2.30      2.22    1.36   n/a    4.68         0.599     4.29  1.79     0.605      0.468
float_toFixed           1.00      7.13      7.13    1.88   n/a    3.17         0.885     3.12  0.902    1.05       0.790
float_toPrecision       1.00      2.67      2.65    6.92   n/a    3.49         1.04      3.44  1.02     0.796      0.587
float_toExponential     1.00      2.32      2.32    6.38   n/a    3.25         0.965     3.12  0.932    0.725      0.508
string_to_int           1.00      6.56      6.75    3.02   n/a    9.33         2.72      9.31  3.35     0.832      0.205
string_to_float         1.00      2.97      2.93    0.980  n/a    3.55         1.74      3.40  1.35     0.597      0.395
bigint32_arith          1.00      9.57      9.60    6.10   n/a    4.63         1.74      5.40  0.970    1.11       1.12
bigint64_arith          1.00      2.95      2.97    1.81   n/a    1.89         0.731     2.63  0.424    0.379      0.161
bigint256_arith         1.00      0.920     0.927   0.579  n/a    1.00         0.470     1.38  0.273    0.170      0.106
Geomean                 1.00      6.78      6.82    1.41   n/a    4.46         0.800     6.94  1.20     0.810      0.152
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                            modernc.org/                     QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      moejs     otto  quickjs       BeastJS  QJS       (C)       --jitless  v8go
empty_loop              ns      3.39      39.7       40.4      4.38   n/a      13.8         2.29      21.4      4.28      5.90      0.536
empty_down_loop         ns      3.28      38.3       38.4      4.68   n/a      13.7         2.24      19.9      4.09      5.12      0.692
empty_down_loop2        ns      3.16      46.2       45.5      4.63   n/a      18.6         3.75      34.2      5.91      6.23      0.533
empty_do_loop           ns      3.32      44.4       46.3      4.40   n/a      16.4         2.25      20.4      7.37      5.09      0.542
date_now                ns     41.6      118        117       35.0    n/a      93.1        40.1      146       22.3      32.4      24.2
date_parse              µs      1.85       3.31       3.35     2.17   n/a      10.6         1.47       8.27     2.99      3.97      3.72
prop_read               ns     18.6      190        188       18.3    n/a      86.4        11.4      123       27.7      25.2       1.11
prop_write              ns      9.76     150        144        9.51   n/a      60.2         9.11      98.0     16.9      16.8       0.622
prop_update             ns     29.8      279        278       29.3    n/a     102          20.4      215       28.1      40.8       1.76
prop_create             ns    608       2656       2757      617      n/a    1752         378       2857      591       238        94.0
prop_clone              ns    188       3923       3877      210      n/a    2862         395       2316      812        32.2      20.4
prop_delete             ns    565       5524       5499     1156      n/a    4523         941       3734     1188       525       428
array_read              ns     36.5      414        410       58.1    n/a     176          25.6      297       64.0      75.7       4.70
array_write             ns     18.8      238        242       24.9    n/a     140          22.9      640       33.1      64.5       1.42
array_update            ns     79.1      583        582       87.1    n/a     219          58.3      612       68.6     134         3.36
array_prop_create       µs     13.6      116        113       24.4    n/a      28.3         8.46      93.5     12.6      14.0       1.91
array_slice             µs      3.92      38.8       39.4      2.71   n/a      60.0         1.26      27.0     10.9       0.117     0.0870
array_length_read       ns     16.7      187        188       18.4    n/a      84.5        11.9      118       27.2      22.0       2.98
array_length_decr       µs     17.4      134        178      109      n/a     348          17.3       87.0     22.1      26.6      18.0
array_hole_length_decr  µs     22.1      134        133       60.8    n/a     158          42.3      151       51.0      27.3      18.4
array_push              µs     15.4       99.7       98.6     13.5    n/a      52.0         7.00      66.6     11.7      10.5       1.20
array_pop               µs     13.8       76.7       75.5     29.8    n/a     133          13.7       69.2     20.1       8.44      1.22
typed_array_read        ns     61.4      422        425       80.2    n/a     347          64.3      296       65.3      90.7       4.61
typed_array_write       ns     35.0      404        652       94.0    n/a     220          51.8      646       58.4      70.1       1.39
arguments_read          ns    175       3795       5325      356      n/a    1896         159           n/a   351       262        35.4
arguments_strict_read   ns    172       3578       3583      213      n/a    1149         133           n/a   244       208        26.5
global_read             ns     18.1      174        169       18.4    n/a      58.4         9.54     107       25.9      17.3       0.715
global_write            ns      8.20     249        267       17.2    n/a      72.0         6.74     143       14.9      15.7       0.833
global_write_strict     ns      8.21     298        305       17.6    n/a      72.7         6.80     143       14.8      15.8       0.833
local_destruct          ns    179       1116       1114      122      n/a    1239         134       4006      221       222         7.83
global_destruct         ns    226       1543       1599      116      n/a    1132         121       1345      175       218        15.0
global_destruct_strict  ns    153       1365       1342      114      n/a    1125         127       1350      230       197        13.0
global_func_call        ns     73.9      390        332       74.2    n/a     303          32.0      233       61.6      69.8       0.476
func_call               ns     69.9      248        253       63.7    n/a     294          26.7      186       59.5      68.0       0.457
func_closure_call       ns     81.3      249        325       75.5    n/a     289          30.5      197       62.4      65.5       9.86
int_arith               µs      7.57      81.3       80.9      8.11   n/a      30.3         4.08      42.7      9.78      9.95      0.361
float_arith             µs     10.4      158        155        9.68   n/a      36.2         6.23      60.2     12.6      20.6       0.853
map_set_string          µs    144        422        417      101      n/a     599         140        476      136        57.4      21.6
map_set_int             µs    110        279        278       77.1    n/a     303          68.0     4705       80.1      58.0      14.4
map_set_bigint          µs    124        424        426      150      n/a     482         108       9129       88.8     115        75.2
map_delete              µs    165        457        450      108      n/a     518         158           n/a   131        88.3      26.0
weak_map_set            µs    158     169147     173200     2420      n/a     229          53.6         n/a    47.2      41.4      18.5
weak_map_delete         µs    417       2660       3097      141      n/a     485          90.6         n/a   112        85.8      36.3
array_for               ns    822       7398       7379      868      n/a    3231         478       4999     1144      1285       104
array_for_in            µs      2.21      24.6       24.9      3.62   n/a      18.1         2.98      12.6      3.63      1.55      1.41
array_for_of            ns    825      22028       6406      861      n/a   10232         885       5006     1655      2465       280
math_min                µs     14.7      110        111       24.7    n/a      82.4        16.3       87.3     16.5      17.2       0.280
regexp_ascii            µs    152        931        929      176      n/a     380         145       1058      116        48.4      30.4
regexp_utf16            µs    181        965        968      220      n/a     386         167       1111      167        48.2      31.2
regexp_replace          µs    200        941       1154      437      n/a     730         252       5927      805        85.9      63.7
string_length           ns     18.1      345        345       18.5    n/a     135          10.2      113       28.2      28.8       2.10
string_build1           µs     15.8       78.2       76.8     29.4    n/a      38.2         9.92     113       12.5      11.3       2.30
string_build1x          µs     13.4       75.2       76.5     39.3    n/a      39.1         9.67     113       12.4      11.4       2.41
string_build2c          µs     13.7       78.9       82.7     32.4    n/a      37.8         9.70     166       17.8      11.3       2.35
string_build2           µs     16.0       90.1       79.2     31.6    n/a      96.6        11.1      124       39.6      11.3       3.00
string_build3           µs     47.2       89.1       87.6     24.8    n/a     100          28.0      118       27.2      12.1       2.30
string_build4           µs     32.7       81.5       82.3     31.8    n/a     108          35.2      125       40.2      11.2       2.28
string_build_large1     µs    322       1574       1650      568      n/a    2337         202      26897      708       222       147
string_build_large2     ms      1.07       1.88       1.93     0.469  n/a       2.40        0.732     27.6      0.722     0.252     0.143
string_concat           ns    759       1731       1665     1652      n/a    2165         562       3563      626       192       172
string_array_join       ns    302       2419       2899      632      n/a    2024         306       1842      616       244       250
string_template         ns    372        835        755     1314      n/a    1546         354       2843      361       250       131
int_to_string           ns     63.3      218        216       79.2    n/a     271          88.8      281       64.2      37.3      10.4
int_toString            ns     87.2      356        357      100      n/a     488          71.1      383       85.1      49.0      16.8
float_to_string         ns    175        460        457      245      n/a     864         144        917      425       128        83.5
float_toString          ns    242        556        538      328      n/a    1131         145       1037      432       146       113
float_toFixed           ns    229       1631       1633      430      n/a     726         202        713      206       241       181
float_toPrecision       ns    226        603        598     1563      n/a     789         234        776      230       180       132
float_toExponential     ns    248        575        574     1581      n/a     806         239        772      231       180       126
string_to_int           ns     17.2      113        116       51.8    n/a     160          46.6      160       57.6      14.3       3.52
string_to_float         ns     51.8      154        152       50.8    n/a     184          90.0      176       69.7      30.9      20.5
bigint32_arith          µs     18.4      176        177      112      n/a      85.4        32.1       99.4     17.9      20.5      20.6
bigint64_arith          µs     59.8      177        178      108      n/a     113          43.7      158       25.4      22.7       9.62
bigint256_arith         µs    211        194        196      123      n/a     212          99.3      291       57.7      35.9      22.4
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
| date | 2026-11-28T00:03:16+09:00 | 2026-11-28T00:06:22+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.2.0-alpha.0.20261127145346-5a36a064fec5 | v0.2.0-alpha.0.20261127145346-5a36a064fec5 |
| github.com/dop251/goja | v0.0.0-20261118204651-8dfece3d1e1d | v0.0.0-20261118204651-8dfece3d1e1d |
| github.com/grafana/sobek | v0.0.0-20261119094010-6eceb967bb5b | v0.0.0-20261119094010-6eceb967bb5b |
| github.com/MoeForge/moejs | v0.1.1 | v0.1.1 |
| github.com/robertkrimen/otto | v0.5.1 | v0.5.1 |
| modernc.org/quickjs | v0.27.0 | v0.27.0 |
| github.com/liustack/beastjs | v0.33.3 | v0.33.3 |
| github.com/fastschema/qjs | v0.0.6 | v0.0.6 |
| github.com/bellard/quickjs | v0.0.0-20260604125329-3d5e064e9dd6 | v0.0.0-20260604125329-3d5e064e9dd6 |
| github.com/tommie/v8go | v0.44.0 | v0.44.0 |
| rounds | 1 | 1 |
| benchtime | 1s | 200ms |
| load-start | 5.37 5.61 5.78 | 5.75 5.42 5.64 |
| load-end | 5.75 5.42 5.64 | 4.96 5.64 5.69 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
