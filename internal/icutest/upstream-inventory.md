# Upstream ICU suite inventory

At Ekuma `033441f`, the full two-pin `TestICU` run passed all 117 leaf subtests with
289 expected differences and zero unexpected failures. Issue #9 stays open until
each applicable gap has a maintained test or a linked issue. [Tracker](https://github.com/hajimehoshi/ekuma/issues/9)

## Proposed follow-ups in priority order

1. **Regex captures.** Use the ECMAScript-compatible rows to check capture text and UTF-16 offsets. For `(.*)(?=c)` on `abcdef`, both the match and group 1 span [0, 2). [Tagged captures][regex-data] · [tabular offsets][perl-data]
2. **Persian New Year.** Check both conversion directions across ISO 1997-03-20 and 1997-03-21, corresponding to Persian 1375-12-30 and 1376-01-01. [TestMapping][persian]
3. **Japanese and Korean collation.** Check Japanese base sensitivity against `カ < カキ < キ < キキ`, and Korean variant sensitivity against `가 < 각`. [Japanese table][kana] · [Korean pair][korean]
4. **Number formatting.** Check Serbian compact rounding of −999999 to `-1 милион`, Hungarian forint output at zero, two and three fraction digits, and the two grouping spans in Swiss German `-1'000'007`. [Compact numbers][compact] · [currency digits][currency] · [field positions][simple-number]
5. **French date ranges.** Check `17–23 septembre 2024` within a month and `17 septembre – 3 octobre 2024` across months, including the thin spaces. [Date interval cases][fr-range]
6. **Normalization.** The corrected 3.2 fixture adds 246 distinct five-column rows beyond the Unicode 17 corpus. All 4,920 comparisons matched in a one-off run. [Older fixture][old-norm] · [Unicode 17 fixture][norm]

## Scope and verification

The audit checked both recursive Git trees and their C, C++ and JUnit registrations.
It classified 2,453 distinct paths. ICU 78.3 supplies Unicode 17 and CLDR 48.2
collation. ICU 79.1 supplies CLDR 49 locale and formatting data.

| Counting unit | ICU 78.3 | ICU 79.1 |
|---|---:|---:|
| Indexed paths | 2,273 | 2,344 |
| Files containing suite registrations or JUnit tests | 518 | 533 |
| Fixture files | 966 | 1,014 |
| Distinct fixture blobs | 639 | 670 |
| Unclassified paths in scope | 0 | 0 |

The path index identifies shared fixtures, related tests and ICU-only families.
For example, named LSTM models require selecting a break engine, and locale-distance
scores require access to matcher internals. Intl exposes the resulting segments
and selected locale. The adapter index gives the runnable selections and counts
each exclusion by reason. A single fixture row can produce several assertions.
[Path index](upstream-paths.tsv) · [Adapter index](upstream-adapters.tsv) · [Coverage and test commands](README.md)

To verify the counts against Git objects in both pinned checkouts, run from the
Ekuma repository root:

```sh
go run internal/icutest/verify_inventory.go "$EKUMA_ICU" "$EKUMA_ICU_CLDR"
```

[regex-data]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/test/testdata/regextst.txt
[perl-data]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/test/testdata/re_tests.txt
[persian]: https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4j/main/core/src/test/java/com/ibm/icu/dev/test/calendar/PersianTest.java#L24
[kana]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/test/intltest/jacoll.cpp#L67
[korean]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/test/intltest/lcukocol.cpp#L53
[compact]: https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/format/CompactDecimalFormatTest.java#L109
[currency]: https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4c/source/test/cintltst/currtest.c#L196
[simple-number]: https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4c/source/test/intltest/numbertest_simple.cpp#L29
[fr-range]: https://github.com/unicode-org/icu/blob/ddc6b66926bfac6036144ca9902992a0d6d50af9/icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format/JavaTimeFormatTest.java#L405
[old-norm]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/test/testdata/NormalizationTest-3.2.0.txt
[norm]: https://github.com/unicode-org/icu/blob/21d1eb0f306e1141c10931e914dfc038c06121da/icu4c/source/data/unidata/NormalizationTest.txt
