// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal_test

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

func i128(s string) temporal.Int128 {
	b, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad integer " + s)
	}
	x, ok := temporal.Int128FromBig(b)
	if !ok {
		panic("integer out of range " + s)
	}
	return x
}

// TestRangeLimits checks the limits of instants, date-times and year-months, and the values
// just beyond them.
func TestRangeLimits(t *testing.T) {
	for _, tc := range []struct {
		ns   string
		want bool
	}{
		{
			ns:   "8640000000000000000000",
			want: true,
		},
		{
			ns:   "8640000000000000000001",
			want: false,
		},
		{
			ns:   "-8640000000000000000000",
			want: true,
		},
		{
			ns:   "-8640000000000000000001",
			want: false,
		},
	} {
		if got := temporal.IsValidEpochNanoseconds(i128(tc.ns)); got != tc.want {
			t.Errorf("IsValidEpochNanoseconds(%s): got %v, want %v", tc.ns, got, tc.want)
		}
	}

	for _, tc := range []struct {
		dt   temporal.ISODateTime
		want bool
	}{
		{
			dt: temporal.ISODateTime{
				Date: temporal.ISODate{Year: -271821, Month: 4, Day: 19},
				Time: temporal.Time{Nanosecond: 1},
			},
			want: true,
		},
		{
			dt: temporal.ISODateTime{
				Date: temporal.ISODate{Year: -271821, Month: 4, Day: 19},
			},
			want: false,
		},
		{
			dt: temporal.ISODateTime{
				Date: temporal.ISODate{Year: 275760, Month: 9, Day: 13},
				Time: temporal.Time{Hour: 23, Minute: 59, Second: 59, Millisecond: 999, Microsecond: 999, Nanosecond: 999},
			},
			want: true,
		},
		{
			dt: temporal.ISODateTime{
				Date: temporal.ISODate{Year: 275760, Month: 9, Day: 14},
			},
			want: false,
		},
	} {
		if got := temporal.ISODateTimeWithinLimits(tc.dt); got != tc.want {
			t.Errorf("ISODateTimeWithinLimits(%v): got %v, want %v", tc.dt, got, tc.want)
		}
	}

	for _, tc := range []struct {
		d    temporal.ISODate
		want bool
	}{
		{
			d:    temporal.ISODate{Year: -271821, Month: 4, Day: 1},
			want: true,
		},
		{
			d:    temporal.ISODate{Year: -271821, Month: 3, Day: 1},
			want: false,
		},
		{
			d:    temporal.ISODate{Year: 275760, Month: 9, Day: 1},
			want: true,
		},
		{
			d:    temporal.ISODate{Year: 275760, Month: 10, Day: 1},
			want: false,
		},
	} {
		if got := temporal.ISOYearMonthWithinLimits(tc.d); got != tc.want {
			t.Errorf("ISOYearMonthWithinLimits(%v): got %v, want %v", tc.d, got, tc.want)
		}
	}

	// The epoch days of the limits round-trip through the date conversion.
	for _, days := range []int64{-100_000_001, -1, 0, 100_000_001} {
		d := temporal.EpochDaysToISODate(days)
		if got := d.EpochDays(); got != days {
			t.Errorf("EpochDays(EpochDaysToISODate(%d)): got %d (%v)", days, got, d)
		}
	}
}

// TestRoundingAtBoundaries checks the rounding modes at and around the halfway points, for
// negative values and for values beyond the range of int64.
func TestRoundingAtBoundaries(t *testing.T) {
	for _, tc := range []struct {
		x    int64
		mode temporal.RoundingMode
		want int64
	}{
		{x: 15, mode: temporal.RoundHalfEven, want: 20},
		{x: 25, mode: temporal.RoundHalfEven, want: 20},
		{x: -25, mode: temporal.RoundHalfEven, want: -20},
		{x: -15, mode: temporal.RoundHalfCeil, want: -10},
		{x: -15, mode: temporal.RoundHalfFloor, want: -20},
		{x: -15, mode: temporal.RoundHalfTrunc, want: -10},
		{x: -15, mode: temporal.RoundHalfExpand, want: -20},
		{x: -11, mode: temporal.RoundCeil, want: -10},
		{x: -11, mode: temporal.RoundFloor, want: -20},
		{x: -11, mode: temporal.RoundExpand, want: -20},
		{x: -19, mode: temporal.RoundTrunc, want: -10},
		{x: 14, mode: temporal.RoundHalfExpand, want: 10},
	} {
		if got := temporal.RoundNumberToIncrement(tc.x, 10, tc.mode); got != tc.want {
			t.Errorf("RoundNumberToIncrement(%d, 10, %s): got %d, want %d", tc.x, temporal.RoundingModeNames[tc.mode], got, tc.want)
		}
	}

	// Epoch nanoseconds before the epoch round as if they were positive: floor towards the
	// past, ceil towards the future.
	ns := i128("-8639999999999999999999")
	if got, want := temporal.RoundNumberToIncrementAsIfPositive(ns, temporal.NsPerSecond, temporal.RoundFloor), i128("-8640000000000000000000"); got.Cmp(want) != 0 {
		t.Errorf("RoundNumberToIncrementAsIfPositive(%v, floor): got %v, want %v", ns, got, want)
	}
	if got, want := temporal.RoundNumberToIncrementAsIfPositive(ns, temporal.NsPerSecond, temporal.RoundHalfTrunc), i128("-8640000000000000000000"); got.Cmp(want) != 0 {
		t.Errorf("RoundNumberToIncrementAsIfPositive(%v, halfTrunc): got %v, want %v", ns, got, want)
	}

	// An increment beyond int64, as rounding to a large number of days needs.
	increment := i128("86400000000000000000000")
	x := i128("43200000000000000000000")
	if got := temporal.RoundInt128ToIncrement(x, increment, temporal.RoundHalfEven); !got.IsZero() {
		t.Errorf("RoundInt128ToIncrement(%v, %v, halfEven): got %v, want 0", x, increment, got)
	}
	if got := temporal.RoundInt128ToIncrement(x.Neg(), increment, temporal.RoundHalfExpand); got.Cmp(increment.Neg()) != 0 {
		t.Errorf("RoundInt128ToIncrement(%v, %v, halfExpand): got %v, want %v", x.Neg(), increment, got, increment.Neg())
	}

	// The total is the Number nearest to the exact quotient, which dividing two Numbers would
	// miss: (2⁵³ + 1) × 10⁹ + 5 × 10⁸ ns is 2⁵³ + 1.5 s, which rounds to 2⁵³ + 2.
	d := temporal.Mul64(1<<53+1, temporal.NsPerSecond).Add(temporal.I128(500_000_000))
	if got, want := temporal.TotalTimeDuration(d, temporal.UnitSecond), float64(1<<53+2); got != want {
		t.Errorf("TotalTimeDuration(%v, second): got %v, want %v", d, got, want)
	}
}

// zoneType is a local time type of a time zone.
type zoneType struct {
	offset int32
	abbr   string
}

// daylightSavingZone returns a time zone that is std except from start until end, two instants
// in Unix seconds, when it is dst. The data is built here so that the test does not depend on
// the time zone database of the machine.
func daylightSavingZone(t *testing.T, std, dst zoneType, start, end uint32) temporal.TimeZone {
	t.Helper()
	be := binary.BigEndian
	var b []byte
	b = append(b, "TZif"...)
	b = append(b, make([]byte, 16)...)
	// The counts of UT indicators, standard indicators, leap seconds, transitions, types and abbreviation bytes.
	for _, n := range []uint32{0, 0, 0, 2, 2, uint32(len(std.abbr) + len(dst.abbr) + 2)} {
		b = be.AppendUint32(b, n)
	}
	b = be.AppendUint32(b, start)
	b = be.AppendUint32(b, end)
	b = append(b, 1, 0)
	b = be.AppendUint32(b, uint32(std.offset))
	b = append(b, 0, 0)
	b = be.AppendUint32(b, uint32(dst.offset))
	b = append(b, 1, byte(len(std.abbr)+1))
	b = append(b, std.abbr+"\x00"+dst.abbr+"\x00"...)
	loc, err := time.LoadLocationFromTZData("Test/"+std.abbr, b)
	if err != nil {
		t.Fatalf("LoadLocationFromTZData: %v", err)
	}
	return temporal.SystemTimeZone(datetime.LocationZone(loc))
}

func dateTime(y, mo, d, h, mi int) temporal.ISODateTime {
	return temporal.ISODateTime{
		Date: temporal.ISODate{Year: y, Month: mo, Day: d},
		Time: temporal.Time{Hour: h, Minute: mi},
	}
}

func epochNs(sec int64) temporal.Int128 {
	return temporal.Mul64(sec, temporal.NsPerSecond)
}

// TestOffsetTransitions checks the local times at the transitions of a time zone: a skipped
// local time has no instant, a repeated one has two, disambiguation chooses among them, and
// the transitions are found strictly after and before an instant.
func TestOffsetTransitions(t *testing.T) {
	// Central Europe in 2021.
	const (
		start = 1616893200 // 2021-03-28T01:00:00Z
		end   = 1635642000 // 2021-10-31T01:00:00Z
	)
	cet := daylightSavingZone(t, zoneType{
		offset: 3600,
		abbr:   "CET",
	}, zoneType{
		offset: 7200,
		abbr:   "CEST",
	}, start, end)
	if got, want := cet.ID(), "Test/CET"; got != want {
		t.Errorf("ID: got %q, want %q", got, want)
	}

	skipped := dateTime(2021, 3, 28, 2, 30)
	if got, err := cet.PossibleEpochNanoseconds(skipped); err != nil || len(got) != 0 {
		t.Errorf("PossibleEpochNanoseconds(%v): got %v, %v, want none", skipped, got, err)
	}
	repeated := dateTime(2021, 10, 31, 2, 30)
	possible, err := cet.PossibleEpochNanoseconds(repeated)
	if err != nil || len(possible) != 2 || possible[0].Cmp(epochNs(end-1800)) != 0 || possible[1].Cmp(epochNs(end+1800)) != 0 {
		t.Errorf("PossibleEpochNanoseconds(%v): got %v, %v, want [%v %v]", repeated, possible, err, epochNs(end-1800), epochNs(end+1800))
	}

	for _, tc := range []struct {
		dt   temporal.ISODateTime
		d    temporal.Disambiguation
		want int64
	}{
		// A skipped time moves forward by the length of the gap, or backward for earlier.
		{dt: skipped, d: temporal.DisambiguationCompatible, want: start + 1800},
		{dt: skipped, d: temporal.DisambiguationLater, want: start + 1800},
		{dt: skipped, d: temporal.DisambiguationEarlier, want: start - 1800},
		{dt: repeated, d: temporal.DisambiguationCompatible, want: end - 1800},
		{dt: repeated, d: temporal.DisambiguationEarlier, want: end - 1800},
		{dt: repeated, d: temporal.DisambiguationLater, want: end + 1800},
	} {
		got, err := cet.EpochNanosecondsFor(tc.dt, tc.d)
		if err != nil || got.Cmp(epochNs(tc.want)) != 0 {
			t.Errorf("EpochNanosecondsFor(%v, %d): got %v, %v, want %v", tc.dt, tc.d, got, err, epochNs(tc.want))
		}
	}
	for _, dt := range []temporal.ISODateTime{skipped, repeated} {
		if _, err := cet.EpochNanosecondsFor(dt, temporal.DisambiguationReject); err == nil {
			t.Errorf("EpochNanosecondsFor(%v, reject): got no error", dt)
		}
	}

	for _, tc := range []struct {
		ns   temporal.Int128
		next bool
		want temporal.Int128
		ok   bool
	}{
		// The transitions are strictly after or before the instant, also by a nanosecond.
		{ns: epochNs(start), next: true, want: epochNs(end), ok: true},
		{ns: epochNs(start).Sub(temporal.I128(1)), next: true, want: epochNs(start), ok: true},
		{ns: epochNs(start), next: false, ok: false},
		{ns: epochNs(start).Add(temporal.I128(1)), next: false, want: epochNs(start), ok: true},
		{ns: epochNs(end).Add(temporal.I128(1)), next: true, ok: false},
	} {
		got, ok := cet.Transition(tc.ns, tc.next)
		if ok != tc.ok || (ok && got.Cmp(tc.want) != 0) {
			t.Errorf("Transition(%v, next: %v): got %v, %v, want %v, %v", tc.ns, tc.next, got, ok, tc.want, tc.ok)
		}
	}

	// Cuba in 2021, whose spring transition skips midnight, so the day starts at 01:00.
	cuba := daylightSavingZone(t, zoneType{
		offset: -5 * 3600,
		abbr:   "CST",
	}, zoneType{
		offset: -4 * 3600,
		abbr:   "CDT",
	},
		1615698000, // 2021-03-14T05:00:00Z
		1636261200, // 2021-11-07T05:00:00Z
	)
	got, err := cuba.StartOfDay(temporal.ISODate{Year: 2021, Month: 3, Day: 14})
	if err != nil || got.Cmp(epochNs(1615698000)) != 0 {
		t.Errorf("StartOfDay(2021-03-14): got %v, %v, want %v", got, err, epochNs(1615698000))
	}
}

// TestDurationLimits checks the limits of the time part of a duration, which are beyond the
// range of int64 nanoseconds.
func TestDurationLimits(t *testing.T) {
	maxSeconds := temporal.Duration{
		Seconds:     1<<53 - 1,
		Nanoseconds: 999_999_999,
	}
	if !temporal.IsValidDuration(&maxSeconds) {
		t.Errorf("IsValidDuration(%v): got false, want true", maxSeconds)
	}
	over := maxSeconds
	over.Nanoseconds++
	if temporal.IsValidDuration(&over) {
		t.Errorf("IsValidDuration(%v): got true, want false", over)
	}
	mixed := temporal.Duration{
		Hours:   1,
		Minutes: -1,
	}
	if temporal.IsValidDuration(&mixed) {
		t.Errorf("IsValidDuration(%v): got true, want false", mixed)
	}
	// The largest time duration balances to seconds exactly, but as nanoseconds it is the
	// Number nearest to it, which is 2⁵³ seconds and so out of range.
	id := temporal.ToInternalDurationRecord(&maxSeconds)
	d, err := temporal.TemporalDurationFromInternal(id, temporal.UnitSecond)
	want := temporal.Duration{
		Seconds:      1<<53 - 1,
		Milliseconds: 999,
		Microseconds: 999,
		Nanoseconds:  999,
	}
	if err != nil || d != want {
		t.Errorf("TemporalDurationFromInternal(second): got %v, %v, want %v", d, err, want)
	}
	if d, err := temporal.TemporalDurationFromInternal(id, temporal.UnitNanosecond); err == nil {
		t.Errorf("TemporalDurationFromInternal(nanosecond): got %v, want an error", d)
	}
}
