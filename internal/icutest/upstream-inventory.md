# Upstream ICU suite inventory

All follow-ups identified by the pinned inventory have maintained checks.
The full two-pin `TestICU` run passed all 129 leaf subtests with 289 expected
differences and zero unexpected failures. [Tracker](https://github.com/hajimehoshi/ekuma/issues/9)

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
