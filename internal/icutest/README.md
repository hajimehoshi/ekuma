# ICU conformance

Run `TestICU` from the repository root. Set `EKUMA_ICU` to the ICU 78.3
checkout for Unicode and collation tests, and `EKUMA_ICU_CLDR` to ICU 79.1
for CLDR 49 tests. Each checkout runs independently and offline. The 0BSD Go
adapters read fixtures from the external checkouts. [Pins and default cache paths](../../README.md#conformance-tests) · [CI sparse checkout](../../.github/workflows/test.yml)

```sh
# Full suite
go test -count=1 -v -run '^TestICU$' .

# Stage 17 calendar arithmetic and era boundaries
GOMAXPROCS=2 go test -p=1 -parallel=2 -count=1 -v -run '^TestICU$/^calendar-arithmetic$' .

# Stage 16 duration and multi-unit formatting
GOMAXPROCS=2 go test -p=1 -parallel=2 -count=1 -v -run '^TestICU$/^duration$' .

# Stage 15 locale information and supported values
go test -count=1 -v -run '^TestICU$/^locale-info$' .

# One fixture
go test -count=1 -v -run '^TestICU$/^TimeZoneAliases\.txt$' .
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

## Calendar arithmetic and era boundaries

ICU4J and ICU4C fixtures exercise 4,490 Temporal checks covering addition
across leap months and year boundaries, ordinal months and `monthCode`,
`inLeapYear`, and dates on either side of era transitions. ICU4C 79.1
supplies the reference year lengths. Exclusions include ICU field rolls,
lenient field setting, and observational Islamic calendars. The six known
differences concern Chinese year lengths in 4645–4646 and the start of
the Meiji era. [Reference year lengths](testdata/calendar-year-lengths.txt) · [Known results](testdata/icu-failures.txt)

## Best-fit locale matching

`localeMatcherTest.txt` checks ordered requests, script and region fallback,
related languages, and default selection. The Intl matcher runs 415 of 929
rows with the fixture's exact candidates and default. Exclusions cover script
preference (427), unavailable candidates such as `en-XA` (79), custom thresholds
(4), and empty candidate sets without a default (4). ECMA-402 leaves best-fit
matching implementation-defined. One difference remains for `sh`, which Intl
canonicalizes to `sr-Latn`. [Known results](testdata/icu-failures.txt)

## Locale information and supported values

The `locale-info` group uses ICU4C and ICU4J fixtures and CLDR supplemental
data to check week starts and weekends, text direction, calendar preferences,
collations, hour cycles, numbering systems, and regional time zones. It also
checks the sorted lists for all six `Intl.supportedValuesOf` keys.

## Single-date pattern fixtures

`dtptngts.cpp`, `udatpg_test.c`, and `DateTimeGeneratorTest.java` cover
field widths, hour cycles, fractional seconds, eras, and day periods.
When ICU supplies formatted text, the adapter checks the exact Unicode
string for the ICU skeleton, then checks JavaScript text and parts with
web-compatible spacing. For pattern-only rows it checks field order, widths,
and literals. When ICU chooses the hour width, the adapter checks the ICU
pattern directly and requests its hour width in JavaScript. Era and clock
cases also check the effects of default date fields and locale negotiation.

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

## Duration and multi-unit formatting

`measfmttest.cpp` and ICU4J duration fixtures check long, short, narrow and
digital output for negative durations, explicit zeros and fractional seconds.
ICU list spans and CLDR unit patterns identify which parts belong to each
unit and which literals separate them. All 260 checks pass. Exclusions cover
mixed signs, repeated or out-of-order units, legacy wording and precision,
and elapsed time relative to a reference instant.

## Relative time fixtures

`reldatefmttest.cpp`, `crelativedateformattest.c`, and
`RelativeDateTimeFormatterTest.java` cover named and numeric offsets,
fractional quantities, Serbian plurals, and style fallback. The adapter
checks exact strings and uses ICU's UTF-16 field positions to check part
types, values, and units. It excludes ICU-specific features such as weekday
units, capitalization contexts, and custom number formats.

## Time-zone identifiers and display names

`TimeZoneAliases.txt`, `ccaltst.c` and `tztest.cpp` check alias acceptance
and the identifier returned by Intl.DateTimeFormat and Temporal.ZonedDateTime.
`tzfmttst.cpp`, `dtfmttst.cpp` and `TimeZoneFormatTest.java` supply exact
specific, generic and localized GMT names at fixed instants. The adapter
checks the formatted text and the `timeZoneName` part, including standard
and daylight names. ICU 79.1 uses tzdata 2026e and Ekuma's identifier
tables use 2026f. [Known results](testdata/icu-failures.txt)

## Time-zone transitions and Temporal disambiguation

`tzbdtest.cpp`, `tztest.cpp`, and `TimeZoneBoundaryTest.java` check transition
counts, UTC offsets, and local times around named-zone boundaries.
`tzoffloc.cpp` and `caltest.cpp` check all four Temporal disambiguation modes
for gaps and overlaps, including Lord Howe's 30-minute overlap and Apia's
skipped day. The VM uses Go 1.27.4's tzdata 2026c, which agrees with ICU's
2026e rules for these cases. If the installed Go archive differs, set
`EKUMA_ICU_ZONEINFO` to a local copy of the [pinned zoneinfo.zip](https://raw.githubusercontent.com/golang/go/go1.27.4/lib/time/zoneinfo.zip).
