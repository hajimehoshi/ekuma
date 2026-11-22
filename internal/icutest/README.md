# ICU formatting and calendar conformance

From the repository root, set `EKUMA_ICU` to the pinned ICU 78.3 checkout and
`EKUMA_ICU_CLDR` to the pinned ICU 79.1 checkout. The tests also discover the
`ekuma/icu` and `ekuma/icu-cldr` user cache directories. Each checkout runs
independently, offline. Short mode skips both.

```sh
go test -count=1 -v -run '^TestICU$/^([Nn]umber|dcfmtest|icu4j-Number)' .
go test -count=1 -v -run '^TestICU$/^localeDisplayName' .
go test -count=1 -v -run '^TestICU$/^(plural|icu4j-plurals)' .
go test -count=1 -v -run '^TestICU$/^(calendar|caltestdata|format\.txt)' .
go test -count=1 -v -run '^TestICU$' .
```

The four areas use ICU 79.1, which supplies the CLDR 49 expectations. Ekuma
also pins ICU 79.1 for calendar data and ICU4X `icu_calendar` 2.4.0 for its
East Asian year tables. ICU 79.1 packages tzdata 2026e. Ekuma's zone links
come from 2026f and its named-zone transitions come from Go's available
zoneinfo. All upstream data stays in the external checkouts.
[CI sparse checkout](../../.github/workflows/test.yml) ·
[ICU pins](testdata) · [Calendar generation](../intl/calendar/gen_years.go)

## Fixture inventory

Counts include passing cases and cases reproducing the exact known result.
An exclusion counts a result or operation, except that the lunar table reports
disabled source rows. Verbose output gives counts for each exclusion reason.
A changed known result, an unexpected pass, or an unused failure entry fails
the test. [Known results](testdata/icu-failures.txt)

| ICU 79.1 fixture | Executed | Excluded | Known | Reader and coverage |
|---|---:|---:|---:|---|
| `numberpermutationtest.txt` | 2,484 | 3,366 | 0 | `NumberPermutationTest` in C++ and Java. Three numbers in each of three locales per skeleton. Exclusions cover exponent width/sign, integer truncation, a forced decimal separator, and furlongs. |
| `numberformattestspecification.txt` | 216 | 743 | 1 | `NumberFormatDataDrivenTest`. Per-suite defaults and per-row overrides drive decimal formatting and Serbian plural selection. Exclusions include parsing, pattern serialization or rejection, custom affixes, padding, grouping widths, and unsupported digit or rounding settings. |
| ICU4C `NumberFormatTestCases.txt` | 38 | 65 | 0 | `numfmtst.cpp`. Decimal and currency format results, including the format side of round trips. Parsing, pattern syntax and serialization, and custom grouping widths are excluded. |
| ICU4J `NumberFormatTestCases.txt` | 38 | 84 | 0 | `NumberFormatTest.java`. The Java file has a different set of pattern and parsing cases. |
| `dcfmtest.txt` | 1,475 | 16 | 0 | `dcfmtest.cpp`. ICU's two decimal input overloads have one exact decimal-string counterpart in JS. Exclusions cover parsing, custom scientific mantissas, half-odd rounding, and inexact-rounding traps. |
| `localeDisplayName.txt` | 1,769 | 1,664 | 0 | CLDR's `GenerateLocaleIDTestData` supplies display locale and standard/dialect directives. ICU root and subject codes with Unicode or transformed extensions have no corresponding DisplayNames operation. |
| ICU4C `misc/plurals.txt` | 13,720 | 1,701 | 0 | `PluralRulesTest.TestGetDecimalQuantitySamples` checks samples against their rule keyword. Integer and decimal intervals are expanded at their specified precision. Compact samples use compact notation. Exclusions identify the root and requests that resolve to another locale. |
| ICU4J `format/plurals.txt` | 2,368 | 0 | 0 | `WritePluralRulesData.getOriginalSamples` extracts the first decimal in each localized phrase. The adapter evaluates the pinned cardinal rules independently for its expected category. |
| `misc/pluralRanges.txt` | 437 | 10 | 0 | `PluralRangesTest.TestLocaleData` checks category pairs. Distinct numeric endpoints come from the cardinal samples and share one set of digit options. Six rows resolve to another locale. Four `one..one` rows require distinct formatted operands with absolute value 1. |
| `calendar.txt` | 72 | 18 | 2 | `dadrcal.cpp` and `DataDrivenCalendarTest.java`. Both conversion directions, month codes, leap months, eras, date addition, and epoch-millisecond addition. Temporal has no field-roll operation. |
| `format.txt` | 19 | 43 | 4 | `dadrfmt.cpp` and `DataDrivenFormatTest.java`. Style formatting and the era/year fields of `G y` and `G yy` through `formatToParts`. Parsing, longer padded year widths, and ICU relative-date words and join patterns are excluded. |
| `caltestdata.h` | 14,610 | 60 | 0 | `CalendarTest::TestChineseCalendarMapping`. Both directions of every active Gregorian/lunar row, including cyclic year, one-based month, leap flag, and day. ICU marks the disabled rows with `//#`. |

The permutation, specification, and display-name files have identical Git
blobs in ICU4C and ICU4J. Each runs once. The Java calendar and format
resource bundles are compiled copies of the ICU4C source fixtures. ICU's
unit-conversion and preference files test unit conversion and selection.
Its MessageFormat fixtures exercise message evaluation.

## Mapping details

- NumberFormat receives decimal strings for exact quantities, including large integers and rounding ties. ICU defaults to half-even. The permutation adapter accounts for the percent skeleton's unscaled input and applies ICU scales in decimal arithmetic. The pattern adapter translates precision, grouping, and exponent intervals independently of the expected string.
- The display-name adapter preserves the entire expected string. When a regular variant has no localized name, its canonical subtag appears alongside the localized language, script, and region.
- Cardinal, ordinal, and range checks preserve visible fraction digits with digit options. ICU4J phrase wording can be shared by several categories. The original reader extracts a numeric input, and ICU's CLDR resource supplies the category rules for that input. The number specification separately expects `other` for Serbian scientific `2E0`, where Ekuma selects `few`. [Original ICU4J reader](https://github.com/unicode-org/icu/blob/4524c8c33d1f59eabc0ed0e5e87bd59eef0a871d/icu4j/main/tests/core/src/com/ibm/icu/dev/test/format/WritePluralRulesData.java#L325) · [ECMA-402 range resolution](https://tc39.es/ecma402/#sec-resolvepluralrange)
- In `calendar.txt`, an empty target field inherits the source field. ICU month numbers are zero-based, and a leap month repeats a month number. Temporal's month code carries the leap flag. The Dangi tenth month of 1880 starts a day earlier in Ekuma, which uses the Beijing-meridian calculation before its Qing and Korean tables.
- ICU's formatting harness defaults to `America/Los_Angeles`. The date tests pass the zone explicitly. Ekuma uses an ASCII space before AM/PM in four strings. The distant Hebrew date also uses a Pacific time-zone name where ICU expects an offset. The separate lunar table's reader requests the unrecognized ICU zone ID `China`, which resolves to a fixed zero offset.
