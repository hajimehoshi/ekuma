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
GOMAXPROCS=2 go test -p=1 -parallel=2 -count=1 -v -run '^TestICU$/^(cdateintervalformattest\.c|dtifmtts\.cpp)$' .
go test -count=1 -v -run '^TestICU$' .
```

These fixtures use ICU 79.1, which supplies the CLDR 49 expectations. Ekuma
also pins ICU 79.1 for calendar data and ICU4X `icu_calendar` 2.4.0 for its
East Asian year tables. For Korean years 1391–1911 it uses the historical
table from .NET 10.0.0, which its authors checked against the Korea Astronomy
and Space Science Institute's calendar. ICU 79.1 packages tzdata 2026e. Ekuma's
zone links come from 2026f and its named-zone transitions come from Go's
available zoneinfo. The ICU test fixtures stay in the external checkouts.
[CI sparse checkout](../../.github/workflows/test.yml) ·
[ICU pins](testdata) · [Calendar generation](../intl/calendar/gen_years.go) ·
[Korean calendar source](https://github.com/dotnet/runtime/blob/v10.0.0/src/libraries/System.Private.CoreLib/src/System/Globalization/KoreanLunisolarCalendar.cs)

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
| `calendar.txt` | 72 | 18 | 0 | `dadrcal.cpp` and `DataDrivenCalendarTest.java`. Both conversion directions, month codes, leap months, eras, date addition, and epoch-millisecond addition. Temporal has no field-roll operation. |
| `format.txt` | 19 | 43 | 4 | `dadrfmt.cpp` and `DataDrivenFormatTest.java`. Style formatting and the era/year fields of `G y` and `G yy` through `formatToParts`. Parsing, longer padded year widths, and ICU relative-date words and join patterns are excluded. |
| `caltestdata.h` | 14,610 | 60 | 0 | `CalendarTest::TestChineseCalendarMapping`. Both directions of every active Gregorian/lunar row, including cyclic year, one-based month, leap flag, and day. ICU marks the disabled rows with `//#`. |
| `cdateintervalformattest.c` | 195 | 12 | 43 | 95 exact strings and 100 parts checks. Covers explicit zones, minute positions, collapsed ranges, and complete German field and span tables. |
| `dtifmtts.cpp` | 464 | 115 | 239 | 459 exact strings and 5 parts checks. Covers localized skeletons, hour cycles, fractional seconds, Japanese and ROC eras, and shared or endpoint-specific fields. |
| **Total** | **37,905** | **7,897** | **287** | 14 fixtures |

The permutation, specification, and display-name files have identical Git
blobs in ICU4C and ICU4J. Each runs once. The Java calendar and format
resource bundles are compiled copies of the ICU4C source fixtures. ICU's
unit-conversion and preference files test unit conversion and selection.
Its MessageFormat fixtures exercise message evaluation.

## Mapping details

- NumberFormat receives decimal strings for exact quantities, including large integers and rounding ties. ICU defaults to half-even. The permutation adapter accounts for the percent skeleton's unscaled input and applies ICU scales in decimal arithmetic. The pattern adapter translates precision, grouping, and exponent intervals independently of the expected string.
- The display-name adapter preserves the entire expected string. When a regular variant has no localized name, its canonical subtag appears alongside the localized language, script, and region.
- Cardinal, ordinal, and range checks preserve visible fraction digits with digit options. ICU4J phrase wording can be shared by several categories. The original reader extracts a numeric input, and ICU's CLDR resource supplies the category rules for that input. The number specification separately expects `other` for Serbian scientific `2E0`, where Ekuma selects `few`. [Original ICU4J reader](https://github.com/unicode-org/icu/blob/4524c8c33d1f59eabc0ed0e5e87bd59eef0a871d/icu4j/main/tests/core/src/com/ibm/icu/dev/test/format/WritePluralRulesData.java#L325) · [ECMA-402 range resolution](https://tc39.es/ecma402/#sec-resolvepluralrange)
- In `calendar.txt`, an empty target field inherits the source field. ICU month numbers are zero-based, and a leap month repeats a month number. Temporal's month code carries the leap flag. The Dangi cases check November 3, 1880, the first day of lunar month 10, in both directions.
- ICU's formatting harness defaults to `America/Los_Angeles`. The date tests pass the zone explicitly. Ekuma uses an ASCII space before AM/PM in four strings. The distant Hebrew date also uses a Pacific time-zone name where ICU expects an offset. The separate lunar table's reader requests the unrecognized ICU zone ID `China`, which resolves to a fixed zero offset.

## Date interval fixtures

The 0BSD Go adapter reads the ICU sources as data at test time. The original
string literals, interval field tables, and Japanese-era macro definitions
live in the pinned ICU checkout. `formatRange` compares the entire Unicode
string. For complete `formatRangeToParts` expectations, each ICU date field
sets a part type and each interval span sets its source. Gaps between fields
become literals. The adapter slices the expected text at both kinds of
boundary in UTF-16 code units. An interval with no spans has only shared parts.
[ICU field fixtures](https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4c/source/test/cintltst/cdateintervalformattest.c) ·
[ICU interval fixtures](https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4c/source/test/intltest/dtifmtts.cpp)

| Upstream cases | Coverage |
|---|---|
| `TestDateIntervalFormat` | All 11 default-context rows, including the same instants in US/Pacific and Asia/Tokyo. Nine rows request capitalization for a sentence, list, or standalone use (three each). |
| `TestFPos_SkelWithSeconds` | Nine skeleton/locale combinations at nine GMT deltas. Every row checks both the text and the first minute's value and offsets, including its absence. In 16 rows distinct instants collapse to the zero-delta text and every part must be shared. |
| `TestFormatToResult`, `TestFormatCalendarToResult` | Complete text and parts across months, within a day, and for the proleptic Gregorian case. Two calendar-overload rows repeat the same expectations. One row requests ICU's historical Julian/Gregorian cutover. |
| `testFormat`, `testHourMetacharacters`, `testTicket20707` | Read both flat tables and the locale-by-skeleton midnight matrix. The skeleton determines the clock family. A compatible locale override selects the cycle within it. The C++ harness uses Los Angeles for wall times and the midnight matrix uses UTC. The adapter converts Buddhist and Japanese era years to instants before formatting. |
| `testFormatMillisecond`, `testTicket11583_2`, `testTicket21222*EraDiff` | Fractional seconds use the requested precision, including omitted fields that distinguish endpoints. The March 2019 Los Angeles DST gap advances the skipped wall time. Also checks Spanish month/year ranges and the Japanese and ROC era boundaries at 06:00 GMT. |
| `testFormattedDateInterval`, `testTicket20710_FieldIdentity` | Exact English text and parts for same-month and identical dates, the specified start span across months, and source attribution with distinct or identical CET endpoints. Date-only calendar inputs use local midnight. |

The C++ exclusions count 1 root locale, 5 padded-year skeletons, 6 explicit
AM/PM widths, 3 noon/midnight day periods, and 7 suppressed day periods on
12-hour clocks. Six early-date assertions in `testYearFormats` and the
Gregorian era ticket use Julian calendar dates. The user-defined
`DateIntervalInfo` table supplies 22 custom patterns and the formatted-result
test has 1 custom reversed fallback. The context table repeats 3 C cases and
requests 9 capitalization overrides. Two API time-zone examples repeat the
C fixture. The stress test supplies 6 date pairs without expected strings.
The quick interval-identity matrix computes its own reference text for
44 locale/skeleton combinations. API lifecycle, threading, construction,
and pattern-inspection tests supply no further fixed range results.

Of the 282 known interval differences, 281 reproduce Ekuma's web-compatible
spaces around English interval separators or before day periods. In one
German parts check, the ICU fixture labels `Uhr` as a shared day period.
Ekuma treats the quoted word in the CLDR pattern as part of a shared literal.
Each entry applies to one operation and explicit upstream lines.
[Known results](testdata/icu-failures.txt)
