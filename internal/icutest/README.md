# ICU conformance

Run `TestICU` from the repository root. Set `EKUMA_ICU` to the ICU 78.3
checkout for Unicode and collation tests, and `EKUMA_ICU_CLDR` to ICU 79.1
for CLDR 49 tests. Each checkout runs independently and offline. The 0BSD Go
adapters read fixtures from the external checkouts. [Pins and default cache paths](../../README.md#conformance-tests) · [CI sparse checkout](../../.github/workflows/test.yml)

```sh
# Full suite
go test -count=1 -v -run '^TestICU$' .

# Unicode casing and ICU casing regressions
go test -count=1 -v -run '^TestICU$/^(UnicodeData\.txt|SpecialCasing\.txt|casing\.txt)$' .

# List formatting
go test -count=1 -v -run '^TestICU$/^(listformattertest\.cpp|ulistfmttest\.c|ListFormatterTest\.java)$' .

# Relative time formatting
go test -count=1 -v -run '^TestICU$/^(reldatefmttest\.cpp|crelativedateformattest\.c|RelativeDateTimeFormatterTest\.java)$' .
```

Use a fixture name after `^TestICU$/` to run one subtest. Verbose output
reports executed cases, exclusions by reason, and known differences.
Listed differences still run. A changed result or an unexpected pass fails
the test. [Known results](testdata/icu-failures.txt)

## Fixture inventory

| ICU 78.3 casing fixtures | Coverage |
|---|---|
| `UnicodeData.txt` | Both default String casing methods on all 2,989 code points with a simple uppercase or lowercase mapping. Unconditional full mappings from `SpecialCasing.txt` take precedence. |
| `SpecialCasing.txt` | Both directions on all 103 unconditional and 16 conditional rows. Whole-string checks cover final sigma, Lithuanian, Turkish and Azerbaijani casing, including combining marks and deletions. Contextual rules run with matching and nonmatching inputs. Locale rules also run under English. Titlecase is outside the String API. |
| `casing.txt` | Separate ICU regression examples for default and locale-sensitive String casing. Titlecasing and case folding have no corresponding String methods. |

| ICU 79.1 fixtures | Coverage |
|---|---|
| `numberpermutationtest.txt`, `numberformattestspecification.txt`, `dcfmtest.txt`, ICU4C and ICU4J `NumberFormatTestCases.txt` | Decimal, currency and scientific formatting. Decimal-string inputs preserve precision. The adapter maps representable ICU patterns to NumberFormat options and excludes parsing. |
| `localeDisplayName.txt` | Standard and dialect display names for locale identifiers. |
| `misc/plurals.txt`, ICU4J `format/plurals.txt`, `misc/pluralRanges.txt` | Cardinal, ordinal and range categories. The adapter preserves visible fraction digits and uses ICU's cardinal rules to interpret localized phrase samples. |
| `calendar.txt`, `caltestdata.h`, `format.txt` | Calendar conversion and arithmetic, leap months, Gregorian/lunar mappings, and localized date strings and parts. |

## Date interval fixtures

`cdateintervalformattest.c` and `dtifmtts.cpp` check localized range text and
parts, including collapsed ranges. ICU's UTF-16 field positions give each
part its type. Interval spans identify the start, end, and shared text.
Known differences include English spacing and the field type of German
`Uhr`. [Known results](testdata/icu-failures.txt)

## Number range fixtures

`NumberRangeFormatterTest.java`, `PluralRangesTest.java`, `numbertest_range.cpp`,
and `unumberrangeformattertst.c` cover rounding, approximate results, range
plurals, and shared signs, currencies, and units. The adapter uses ICU's
automatic collapse and approximate identity fallback. It excludes cases that
require separate endpoint formatters or force a different result on equality.

## List formatting fixtures

`listformattertest.cpp`, `ulistfmttest.c`, and `ListFormatterTest.java` cover
list lengths, types, widths, and contextual Spanish and Hebrew conjunctions.
The adapter checks exact strings and uses ICU's UTF-16 field positions to
check element and literal parts. Urdu narrow-unit patterns are locale data
that JavaScript can request. Custom ICU patterns are excluded. ICU omits
zero-width fields, while ECMA-402 retains an element part for an empty input
string. [ECMA-402 CreatePartsFromList](https://tc39.es/ecma402/#sec-createpartsfromlist)

## Relative time fixtures

`reldatefmttest.cpp`, `crelativedateformattest.c`, and
`RelativeDateTimeFormatterTest.java` cover named and numeric offsets,
fractional quantities, Serbian plurals, and style fallback. The adapter
checks exact strings and uses ICU's UTF-16 field positions to check part
types, values, and units. It excludes ICU-specific features such as weekday
units, capitalization contexts, and custom number formats.
