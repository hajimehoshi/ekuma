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
Richards      1.00    5.90   5.89  1.06   63.2   5.06           5.19  1.46     1.09       0.0408
DeltaBlue     1.00    5.16   5.17   n/a   43.6   5.13           4.75  1.48     1.09       0.0178
Encrypt       1.00   10.2    8.99  1.11   38.6   5.04            n/a  1.35     1.57       0.0275
Decrypt       1.00    9.18   9.81  1.12   41.8   5.30            n/a  1.42     1.28       0.0275
RayTrace      1.00    9.24   9.36   n/a   36.7   6.06           7.28  1.50     0.804      0.0267
Earley        1.00    5.42   5.53  0.865  29.4   4.14            n/a  1.19     0.475      0.0305
Boyer         1.00    6.63   6.48  0.791  37.4   4.98            n/a  1.41     0.636      0.0412
RegExp        1.00    8.17   7.81  0.811    n/a  7.86          22.9   5.31     0.491      0.152
Splay         1.00    3.75   3.60  0.583  17.5   2.31           3.09  0.732    0.482      0.0778
NavierStokes  1.00   12.6   12.5   0.851  49.3   3.88           8.08  1.39     1.57       0.0928
Geomean       1.00    7.18   7.08  0.881  37.7   4.77           6.84  1.50     0.858      0.0427
```

<!-- END jsbench V8.relative -->

Median time of one run, in the unit of its row:

<!-- BEGIN jsbench V8.time -->

```
                                                            modernc.org/          QuickJS  v8go
Benchmark     Unit  Ekuma  goja    Sobek   moejs   otto     quickjs       QJS     (C)      --jitless  v8go
Richards      ms     1.54    9.08    9.07   1.63     97.2     7.78          7.98    2.25    1.68      0.0628
DeltaBlue     ms     2.76   14.2    14.3     n/a    120      14.2          13.1     4.08    3.00      0.0492
Encrypt       ms     2.69   27.5    24.2    2.98    104      13.5            n/a    3.63    4.23      0.0740
Decrypt       ms    49.8   457     489     55.7    2082     264              n/a   70.7    63.7       1.37
RayTrace      ms    13.4   124     126       n/a    493      81.4          97.8    20.1    10.8       0.358
Earley        ms     3.18   17.2    17.6    2.75     93.5    13.1            n/a    3.77    1.51      0.0970
Boyer         ms    48.4   321     313     38.3    1808     241              n/a   68.3    30.7       1.99
RegExp        ms    28.2   230     220     22.8        n/a  221           645     150      13.8       4.29
Splay         ms     1.39    5.20    4.99   0.809    24.3     3.20          4.28    1.02    0.668     0.108
NavierStokes  ms    31.2   393     390     26.6    1540     121           252      43.3    49.1       2.90
```

<!-- END jsbench V8.time -->

## QuickJS microbench

Time relative to Ekuma's:

<!-- BEGIN jsbench Micro.relative -->

```
                                                               modernc.org/         QuickJS  v8go
Benchmark               Ekuma  goja     Sobek    moejs   otto  quickjs       QJS    (C)      --jitless  v8go
empty_loop              1.00     12.6     12.2    1.37   n/a    4.31          7.85  1.25     1.80       0.188
empty_down_loop         1.00     13.2     13.4    1.51   n/a    4.53          7.60  1.22     1.52       0.166
empty_down_loop2        1.00     15.6     18.6    1.45   n/a    6.35         11.5   1.83     2.16       0.182
empty_do_loop           1.00     13.4     13.2    1.35   n/a    4.92          6.05  1.61     1.56       0.163
date_now                1.00      2.83     2.78   0.838  n/a    2.21          3.55  0.535    0.717      0.604
date_parse              1.00      1.89     1.92   1.15   n/a    5.45          4.37  1.62     2.33       2.07
prop_read               1.00      7.01     7.05   0.667  n/a    3.19          4.45  0.996    0.774      0.0441
prop_write              1.00     15.1     15.0    1.01   n/a    6.24         10.4   1.77     1.77       0.0573
prop_update             1.00      8.40     8.26   0.886  n/a    3.07          6.43  0.836    1.19       0.0500
prop_create             1.00      4.38     4.29   0.982  n/a    2.87          4.69  0.951    0.394      0.157
prop_clone              1.00     20.2     20.3    1.07   n/a   15.2          12.4   4.26     0.149      0.118
prop_delete             1.00     10.4      9.66   2.10   n/a    8.08          6.87  2.06     0.937      0.761
array_read              1.00     11.6     11.6    1.64   n/a    4.97          8.68  1.88     2.19       0.134
array_write             1.00     13.2     13.2    1.40   n/a    7.90         35.7   1.85     3.62       0.0780
array_update            1.00      7.29     7.28   1.03   n/a    2.71         10.1   0.841    1.69       0.0588
array_prop_create       1.00      8.48     8.74   1.74   n/a    2.00          6.72  0.874    1.01       0.141
array_slice             1.00      9.49     9.73   0.690  n/a   14.6           6.72  2.64     0.0287     0.0252
array_length_read       1.00     11.1     11.6    1.16   n/a    5.07          7.09  1.64     1.36       0.189
array_length_decr       1.00      4.16     4.09   2.11   n/a   19.1           4.67  0.691    0.803      0.550
array_hole_length_decr  1.00      3.89     4.07   1.83   n/a    4.84          6.15  1.44     0.827      0.594
array_push              1.00      6.71     7.88   1.09   n/a    3.74          4.73  0.835    0.551      0.0684
array_pop               1.00      5.55     5.73   2.20   n/a    9.97          5.15  1.51     0.631      0.0931
typed_array_read        1.00      7.10     7.18   1.37   n/a    5.89          4.97  1.12     1.48       0.0833
typed_array_write       1.00     12.5     12.6    2.91   n/a    6.76         21.8   1.88     2.17       0.0434
arguments_read          1.00     19.2     22.8    1.25   n/a    7.74           n/a  1.45     1.09       0.136
arguments_strict_read   1.00     18.5     18.9    1.17   n/a    6.75           n/a  1.42     1.15       0.170
global_read             1.00     12.7     10.2    1.10   n/a    3.50          6.32  1.63     1.03       0.0428
global_write            1.00      7.04     7.26   0.493  n/a    2.21          4.41  0.452    0.482      0.0255
global_write_strict     1.00      9.61     8.91   0.495  n/a    2.23          4.32  0.450    0.482      0.0254
local_destruct          1.00      6.39     6.34   0.936  n/a    9.45         30.5   1.52     1.45       0.0561
global_destruct         1.00      7.91     7.92   0.755  n/a    7.58          9.22  1.18     1.36       0.0902
global_destruct_strict  1.00      6.14     6.00   0.499  n/a    4.99          5.97  0.966    0.898      0.0604
global_func_call        1.00      4.27     4.40   0.975  n/a    4.10          3.28  0.862    0.975      0.00640
func_call               1.00      3.65     3.67   0.946  n/a    4.20          2.72  0.876    1.04       0.00685
func_closure_call       1.00      3.11     3.13   0.930  n/a    3.60          2.54  0.806    0.794      0.124
int_arith               1.00     10.7     10.7    1.07   n/a    4.03          5.75  1.27     1.33       0.0463
float_arith             1.00     14.8     14.8    1.06   n/a    3.92          6.60  1.40     1.95       0.0809
map_set_string          1.00      2.94     2.97   0.708  n/a    3.67          3.37  0.962    0.411      0.155
map_set_int             1.00      3.44     3.47   0.954  n/a    3.89         70.1   1.03     0.727      0.206
map_set_bigint          1.00      3.69     3.67   1.32   n/a    4.23         78.5   0.763    0.983      0.648
map_delete              1.00      2.77     2.71   0.665  n/a    3.11           n/a  0.805    0.366      0.153
weak_map_set            1.00   1041     1058     14.3    n/a    1.40           n/a  0.298    0.268      0.122
weak_map_delete         1.00      5.00     6.87   0.283  n/a    0.958          n/a  0.223    0.171      0.0726
array_for               1.00      8.99     9.00   1.06   n/a    3.95          5.93  1.38     1.56       0.127
array_for_in            1.00     10.6     10.7    1.59   n/a    7.93          5.47  1.56     0.663      0.529
array_for_of            1.00      7.43     7.43   1.04   n/a   12.3           5.82  1.71     2.89       0.352
math_min                1.00      7.78     7.62   1.69   n/a    5.65          6.01  1.15     1.18       0.0188
regexp_ascii            1.00      5.44     5.47   1.03   n/a    2.17          6.01  0.668    0.284      0.173
regexp_utf16            1.00      5.03     5.07   1.14   n/a    2.01          5.68  0.713    0.248      0.165
regexp_replace          1.00      3.44     2.78   0.846  n/a    2.07         15.8   1.88     0.245      0.194
string_length           1.00     19.1     18.8    1.03   n/a    7.62          6.30  1.54     1.57       0.159
string_build1           1.00      1.86     1.91   0.824  n/a    0.901         2.76  0.308    0.275      0.0570
string_build1x          1.00      1.84     1.87   0.720  n/a    0.898         2.75  0.308    0.266      0.0561
string_build2c          1.00      1.82     1.83   0.725  n/a    0.906         3.06  0.349    0.263      0.0554
string_build2           1.00      1.86     1.88   0.719  n/a    2.27          2.96  0.941    0.274      0.0568
string_build3           1.00      1.86     1.89   0.523  n/a    2.20          2.59  0.615    0.270      0.0504
string_build4           1.00      1.89     1.88   0.742  n/a    2.50          2.89  0.933    0.259      0.0537
string_build_large1     1.00      1.75     1.76   0.624  n/a    2.52         30.5   0.791    0.242      0.169
string_build_large2     1.00      1.78     1.78   0.463  n/a    2.28         29.4   0.681    0.237      0.140
string_concat           1.00      2.04     2.05   2.06   n/a    2.69          4.38  0.768    0.237      0.214
string_array_join       1.00      2.65     2.86   0.975  n/a    3.75          3.26  1.13     0.435      0.411
string_template         1.00      2.02     2.08   3.87   n/a    4.41          8.36  1.06     0.747      0.382
int_to_string           1.00      3.50     3.48   1.28   n/a    4.44          4.50  0.989    0.607      0.168
int_toString            1.00      4.02     4.07   1.17   n/a    5.56          4.37  0.980    0.564      0.192
float_to_string         1.00      2.62     2.64   1.42   n/a    4.98          5.16  2.40     0.722      0.490
float_toString          1.00      2.30     2.41   1.29   n/a    5.45          5.15  2.13     0.688      0.457
float_toFixed           1.00      6.83     6.82   1.90   n/a    3.13          3.13  0.845    1.04       0.793
float_toPrecision       1.00      2.59     2.61   6.77   n/a    3.47          3.32  1.02     0.775      0.557
float_toExponential     1.00      2.27     2.27   6.29   n/a    3.16          3.04  0.939    0.733      0.509
string_to_int           1.00      6.81     6.81   3.05   n/a    9.04          9.52  3.38     0.843      0.209
string_to_float         1.00      2.91     2.88   0.979  n/a    3.50          3.39  1.37     0.589      0.391
bigint32_arith          1.00      9.61     9.66   6.14   n/a    4.61          6.67  0.962    1.08       1.11
bigint64_arith          1.00      3.11     3.15   1.93   n/a    2.00          2.79  0.439    0.397      0.362
bigint256_arith         1.00      1.01     1.01   0.639  n/a    1.09          1.40  0.305    0.186      0.115
Geomean                 1.00      5.70     5.75   1.20   n/a    3.91          6.22  1.03     0.694      0.135
```

<!-- END jsbench Micro.relative -->

Median time of one iteration of the loop of a function, in the unit of its row:

<!-- BEGIN jsbench Micro.time -->

```
                                                                            modernc.org/            QuickJS   v8go
Benchmark               Unit  Ekuma   goja       Sobek      moejs     otto  quickjs       QJS       (C)       --jitless  v8go
empty_loop              ns      3.30      41.7       40.4      4.52   n/a     14.2           25.9      4.13      5.95      0.620
empty_down_loop         ns      3.33      43.8       44.7      5.03   n/a     15.1           25.3      4.08      5.07      0.553
empty_down_loop2        ns      2.93      45.8       54.5      4.26   n/a     18.6           33.6      5.38      6.34      0.534
empty_do_loop           ns      3.27      43.7       43.1      4.42   n/a     16.1           19.8      5.25      5.10      0.532
date_now                ns     41.4      117        115       34.7    n/a     91.5          147       22.1      29.6      25.0
date_parse              µs      1.88       3.55       3.62     2.17   n/a     10.3            8.23     3.04      4.39      3.90
prop_read               ns     26.6      186        187       17.7    n/a     84.7          118       26.4      20.5       1.17
prop_write              ns      9.26     140        139        9.31   n/a     57.8           96.3     16.4      16.4       0.531
prop_update             ns     32.7      275        270       29.0    n/a    100            210       27.4      38.9       1.63
prop_create             ns    578       2531       2479      568      n/a   1662           2712      550       228        90.5
prop_clone              ns    179       3602       3629      191      n/a   2709           2222      760        26.5      21.0
prop_delete             ns    531       5502       5135     1117      n/a   4295           3652     1096       498       404
array_read              ns     33.5      390        390       54.9    n/a    167            291       63.1      73.3       4.50
array_write             ns     17.0      226        225       23.8    n/a    135            608       31.5      61.7       1.33
array_update            ns     76.0      555        554       78.1    n/a    206            768       63.9     128         4.47
array_prop_create       µs     13.2      112        116       23.0    n/a     26.4           88.9     11.6      13.4       1.86
array_slice             µs      3.92      37.2       38.2      2.71   n/a     57.4           26.4     10.4       0.113     0.0987
array_length_read       ns     16.1      179        187       18.7    n/a     81.8          114       26.5      21.9       3.04
array_length_decr       µs     31.5      131        129       66.2    n/a    600            147       21.7      25.3      17.3
array_hole_length_decr  µs     31.1      121        126       56.9    n/a    150            191       44.7      25.7      18.5
array_push              µs     13.6       90.9      107       14.7    n/a     50.7           64.1     11.3       7.46      0.926
array_pop               µs     13.0       72.3       74.6     28.7    n/a    130             67.0     19.6       8.22      1.21
typed_array_read        ns     56.9      404        408       77.9    n/a    335            283       63.6      84.1       4.74
typed_array_write       ns     30.8      384        388       89.5    n/a    208            672       58.0      66.9       1.34
arguments_read          ns    187       3598       4265      234      n/a   1450               n/a   273       204        25.5
arguments_strict_read   ns    186       3439       3510      218      n/a   1257               n/a   265       214        31.7
global_read             ns     15.9      203        163       17.5    n/a     55.8          101       26.1      16.3       0.682
global_write            ns     31.2      220        226       15.4    n/a     68.9          138       14.1      15.0       0.796
global_write_strict     ns     31.3      301        279       15.5    n/a     69.8          135       14.1      15.1       0.796
local_destruct          ns    125        796        790      117      n/a   1178           3805      189       180         6.99
global_destruct         ns    140       1110       1111      106      n/a   1063           1293      166       190        12.7
global_destruct_strict  ns    212       1304       1274      106      n/a   1061           1269      205       191        12.8
global_func_call        ns     67.6      289        297       65.9    n/a    277            222       58.2      65.9       0.433
func_call               ns     64.9      237        238       61.3    n/a    273            177       56.8      67.2       0.444
func_closure_call       ns     75.9      236        237       70.6    n/a    273            192       61.2      60.3       9.43
int_arith               µs      7.22      77.4       77.2      7.73   n/a     29.1           41.5      9.14      9.61      0.334
float_arith             µs      8.67     129        129        9.20   n/a     34.0           57.2     12.1      16.9       0.702
map_set_string          µs    136        399        403       96.2    n/a    498            458      131        55.9      21.1
map_set_int             µs     75.0      259        261       71.6    n/a    292           5264       77.0      54.5      15.5
map_set_bigint          µs    109        402        401      144      n/a    461           8560       83.2     107        70.6
map_delete              µs    159        440        430      106      n/a    494               n/a   128        58.1      24.4
weak_map_set            µs    149     155216     157739     2134      n/a    209               n/a    44.5      40.0      18.2
weak_map_delete         µs    474       2375       3258      134      n/a    455               n/a   106        81.0      34.4
array_for               ns    776       6978       6985      821      n/a   3068           4601     1072      1214        99.0
array_for_in            µs      2.17      23.0       23.2      3.45   n/a     17.2           11.9      3.40      1.44      1.15
array_for_of            ns    785       5830       5827      815      n/a   9658           4569     1338      2271       276
math_min                µs     14.0      109        107       23.7    n/a     79.0           84.1     16.1      16.5       0.263
regexp_ascii            µs    166        902        907      171      n/a    360            996      111        47.1      28.6
regexp_utf16            µs    184        928        934      211      n/a    370           1048      132        45.8      30.4
regexp_replace          µs    316       1084        878      267      n/a    652           4994      592        77.2      61.2
string_length           ns     17.0      324        320       17.5    n/a    130            107       26.2      26.7       2.70
string_build1           µs     38.8       72.3       74.0     31.9    n/a     34.9          107       11.9      10.7       2.21
string_build1x          µs     39.0       71.7       72.7     28.1    n/a     35.0          107       12.0      10.4       2.19
string_build2c          µs     39.4       71.7       71.9     28.6    n/a     35.7          121       13.7      10.4       2.18
string_build2           µs     39.0       72.4       73.1     28.0    n/a     88.3          115       36.7      10.7       2.21
string_build3           µs     44.1       82.0       83.2     23.0    n/a     96.8          114       27.1      11.9       2.22
string_build4           µs     41.1       77.8       77.3     30.5    n/a    103            119       38.4      10.7       2.21
string_build_large1     µs    873       1530       1534      545      n/a   2204          26668      691       212       148
string_build_large2     ms      1.03       1.83       1.83     0.476  n/a      2.34          30.3      0.701     0.244     0.144
string_concat           ns    784       1597       1610     1614      n/a   2107           3435      602       186       168
string_array_join       ns    535       1416       1530      522      n/a   2008           1742      603       233       220
string_template         ns    327        660        680     1268      n/a   1445           2737      348       245       125
int_to_string           ns     61.2      214        213       78.5    n/a    272            276       60.5      37.1      10.2
int_toString            ns     84.9      341        346       99.1    n/a    472            371       83.2      47.9      16.3
float_to_string         ns    170        447        449      241      n/a    848            879      409       123        83.4
float_toString          ns    196        449        471      253      n/a   1066           1008      418       135        89.4
float_toFixed           ns    222       1514       1513      421      n/a    693            694      187       231       176
float_toPrecision       ns    220        570        575     1490      n/a    763            731      226       171       123
float_toExponential     ns    241        547        547     1515      n/a    761            732      226       176       122
string_to_int           ns     16.5      112        112       50.5    n/a    149            157       55.9      13.9       3.45
string_to_float         ns     51.5      150        148       50.4    n/a    180            175       70.5      30.3      20.1
bigint32_arith          µs     18.3      176        177      112      n/a     84.2          122       17.6      19.7      20.3
bigint64_arith          µs     56.3      175        177      109      n/a    113            157       24.7      22.3      20.4
bigint256_arith         µs    191        192        193      122      n/a    209            266       58.2      35.4      21.9
```

<!-- END jsbench Micro.time -->

## Missing results

<!-- BEGIN jsbench missing -->

- moejs in V8 benchmark suite v7 (DeltaBlue): `TypeError: UnaryConstraint.inheritsFrom is not a function`
- moejs in V8 benchmark suite v7 (RayTrace): `TypeError: Object.extend is not a function`
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
| date | 2026-11-26T16:29:31+09:00 | 2026-11-26T16:32:17+09:00 |
| os | macOS 27.2 | macOS 27.2 |
| go | go1.27.4 | go1.27.4 |
| github.com/hajimehoshi/ekuma | v0.0.0-20261126072711-8a041c6407f6 | v0.0.0-20261126072711-8a041c6407f6 |
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
| load-start | 3.30 4.37 5.30 | 5.69 4.87 5.32 |
| load-end | 5.69 4.87 5.32 | 1.82 3.45 4.61 |
| goos | darwin | darwin |
| goarch | arm64 | arm64 |
| pkg | github.com/hajimehoshi/ekuma/_bench | github.com/hajimehoshi/ekuma/_bench |
| cpu | Apple M3 Pro | Apple M3 Pro |

<!-- END jsbench environment -->
