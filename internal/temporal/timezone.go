// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// TimeZone is an available time zone: UTC, an offset time zone, or a named time zone.
// The zero value is UTC.
type TimeZone struct {
	// id is the identifier, or empty for UTC.
	id string

	// primary is the primary identifier of a named time zone.
	primary string

	// offsetMinutes is the offset of an offset time zone.
	offsetMinutes int

	isOffset bool

	// zone has the rules of a named time zone other than UTC.
	zone datetime.Zone
}

// UTC is the time zone UTC.
var UTC = TimeZone{}

// ID returns the identifier of z.
func (z TimeZone) ID() string {
	if z.id == "" {
		return "UTC"
	}
	return z.id
}

// IsOffset reports whether z is an offset time zone.
func (z TimeZone) IsOffset() bool {
	return z.isOffset
}

// hasTransitions reports whether z may have offset transitions.
func (z TimeZone) hasTransitions() bool {
	return z.zone != nil
}

// OffsetTimeZone returns the offset time zone of offsetMinutes, which must be in the
// interval from -1439 to 1439.
func OffsetTimeZone(offsetMinutes int) TimeZone {
	return TimeZone{
		id:            FormatOffsetTimeZoneIdentifier(offsetMinutes, false),
		offsetMinutes: offsetMinutes,
		isOffset:      true,
	}
}

// SystemTimeZone returns the time zone tz of the host as a TimeZone, implementing
// SystemTimeZoneIdentifier. It reports false for a zone without a usable identifier, whose time
// zone [SystemOffsetTimeZone] gives.
func SystemTimeZone(tz datetime.TimeZone) (TimeZone, bool) {
	id := datetime.ZoneName(tz)
	if id == "" {
		return TimeZone{}, false
	}
	if strings.EqualFold(id, "UTC") || strings.EqualFold(id, "Etc/UTC") || strings.EqualFold(id, "Etc/GMT") || strings.EqualFold(id, "GMT") {
		return UTC, true
	}
	name, offset, isOffset, err := ParseTimeZoneIdentifier(id, nil)
	if err != nil {
		return TimeZone{}, false
	}
	if isOffset {
		return OffsetTimeZone(offset), true
	}
	// A zone that the platform knows by the same name is the same zone. Any other name is not
	// an available named time zone.
	lid, primary, lz, ok := datetime.LoadZone(name)
	if !ok {
		return TimeZone{}, false
	}
	return TimeZone{
		id:      lid,
		primary: primary,
		zone:    lz,
	}, true
}

// SystemOffsetTimeZone returns the time zone of the host tz that has no usable identifier: the
// offset time zone of its offset at now, the current time value.
func SystemOffsetTimeZone(tz datetime.TimeZone, now float64) TimeZone {
	var off int
	if _, z, ok := datetime.ZoneOf(tz); ok {
		off = z.OffsetSeconds(int64(math.Floor(now / 1000)))
	} else {
		o, _ := tz.Offset(now)
		off = int(o / 1000)
	}
	if off == 0 {
		return UTC
	}
	return OffsetTimeZone(off / 60)
}

// TimeZoneFromIdentifier implements the part of ToTemporalTimeZoneIdentifier after the parse,
// returning the time zone of a named or offset time zone identifier. system is the time zone
// of the host, which is found by its identifier too.
func TimeZoneFromIdentifier(name string, offsetMinutes int, isOffset bool, system TimeZone) (TimeZone, error) {
	if isOffset {
		return OffsetTimeZone(offsetMinutes), nil
	}
	if z, ok := GetAvailableNamedTimeZone(name, system); ok {
		return z, nil
	}
	return TimeZone{}, rangeErrorf("unknown time zone %s", name)
}

// GetAvailableNamedTimeZone implements GetAvailableNamedTimeZoneIdentifier, returning the
// time zone. system is the time zone of the host.
func GetAvailableNamedTimeZone(name string, system TimeZone) (TimeZone, bool) {
	switch {
	case strings.EqualFold(name, "UTC"):
		return UTC, true
	case !system.isOffset && system.id != "" && strings.EqualFold(name, system.id):
		return system, true
	}
	id, primary, z, ok := datetime.LoadZone(name)
	if !ok {
		return TimeZone{}, false
	}
	return TimeZone{
		id:      id,
		primary: primary,
		zone:    z,
	}, true
}

// primaryID returns the primary identifier of a named time zone.
func (z TimeZone) primaryID() string {
	if z.id == "" {
		return "UTC"
	}
	return z.primary
}

// TimeZoneEquals implements TimeZoneEquals.
func TimeZoneEquals(a, b TimeZone) bool {
	if a.ID() == b.ID() {
		return true
	}
	if !a.isOffset && !b.isOffset {
		return a.primaryID() == b.primaryID()
	}
	return false
}

// OffsetNanosecondsFor implements GetOffsetNanosecondsFor.
func (z TimeZone) OffsetNanosecondsFor(epochNs Int128) int64 {
	if z.isOffset {
		return int64(z.offsetMinutes) * NsPerMinute
	}
	if z.zone == nil {
		return 0
	}
	sec, _ := epochNs.FloorDivInt64(NsPerSecond)
	s, _ := sec.Int64()
	return int64(z.zone.OffsetSeconds(s)) * NsPerSecond
}

// ISODateTimeFor implements GetISODateTimeFor.
func (z TimeZone) ISODateTimeFor(epochNs Int128) ISODateTime {
	return ISODateTimeFromEpochNanoseconds(epochNs.Add(I128(z.OffsetNanosecondsFor(epochNs))))
}

// PossibleEpochNanoseconds implements GetPossibleEpochNanoseconds.
func (z TimeZone) PossibleEpochNanoseconds(dt ISODateTime) ([]Int128, error) {
	var result []Int128
	if !z.hasTransitions() {
		balanced := BalanceISODateTime(dt.Date, int64(dt.Time.Hour), int64(dt.Time.Minute)-int64(z.offsetMinutes), int64(dt.Time.Second),
			int64(dt.Time.Millisecond), int64(dt.Time.Microsecond), int64(dt.Time.Nanosecond))
		if err := CheckISODaysRange(balanced.Date); err != nil {
			return nil, err
		}
		result = []Int128{balanced.UTCEpochNanoseconds()}
	} else {
		result = z.namedEpochNanoseconds(dt)
	}
	for _, ns := range result {
		if !IsValidEpochNanoseconds(ns) {
			return nil, rangeErrorf("date-time is out of range")
		}
	}
	return result, nil
}

// namedEpochNanoseconds implements GetNamedTimeZoneEpochNanoseconds.
func (z TimeZone) namedEpochNanoseconds(dt ISODateTime) []Int128 {
	ns := dt.UTCEpochNanoseconds()
	sec, _ := ns.FloorDivInt64(NsPerSecond)
	s, _ := sec.Int64()
	// Every offset is less than a day, so the offsets that can apply are the ones within a day
	// of the local time read as UTC.
	from, to := s-86400, s+86400
	offsets := []int{z.zone.OffsetSeconds(from)}
	for t := from; ; {
		next, ok := z.zone.Transition(t, true)
		if !ok || next > to {
			break
		}
		if off := z.zone.OffsetSeconds(next); !slices.Contains(offsets, off) {
			offsets = append(offsets, off)
		}
		t = next
	}
	var result []Int128
	for _, off := range offsets {
		cand := ns.Sub(Mul64(int64(off), NsPerSecond))
		if z.OffsetNanosecondsFor(cand) == int64(off)*NsPerSecond {
			result = append(result, cand)
		}
	}
	slices.SortFunc(result, Int128.Cmp)
	return slices.CompactFunc(result, func(a, b Int128) bool { return a.Cmp(b) == 0 })
}

// EpochNanosecondsFor implements GetEpochNanosecondsFor.
func (z TimeZone) EpochNanosecondsFor(dt ISODateTime, d Disambiguation) (Int128, error) {
	possible, err := z.PossibleEpochNanoseconds(dt)
	if err != nil {
		return Int128{}, err
	}
	return z.DisambiguatePossibleEpochNanoseconds(possible, dt, d)
}

// DisambiguatePossibleEpochNanoseconds implements DisambiguatePossibleEpochNanoseconds.
func (z TimeZone) DisambiguatePossibleEpochNanoseconds(possible []Int128, dt ISODateTime, d Disambiguation) (Int128, error) {
	n := len(possible)
	if n == 1 {
		return possible[0], nil
	}
	if n != 0 {
		switch d {
		case DisambiguationEarlier, DisambiguationCompatible:
			return possible[0], nil
		case DisambiguationLater:
			return possible[n-1], nil
		}
		return Int128{}, rangeErrorf("the local time is ambiguous")
	}
	if d == DisambiguationReject {
		return Int128{}, rangeErrorf("the local time does not exist")
	}
	utc := dt.UTCEpochNanoseconds()
	before := utc.Sub(I128(NsPerDay))
	after := utc.Add(I128(NsPerDay))
	if !IsValidEpochNanoseconds(before) || !IsValidEpochNanoseconds(after) {
		return Int128{}, rangeErrorf("date-time is out of range")
	}
	delta := z.OffsetNanosecondsFor(after) - z.OffsetNanosecondsFor(before)
	if d == DisambiguationEarlier {
		delta = -delta
	}
	t, days := BalanceTime(int64(dt.Time.Hour), int64(dt.Time.Minute), int64(dt.Time.Second),
		int64(dt.Time.Millisecond), int64(dt.Time.Microsecond), int64(dt.Time.Nanosecond)+delta)
	shifted := ISODateTime{
		Date: AddDaysToISODate(dt.Date, days),
		Time: t,
	}
	possible, err := z.PossibleEpochNanoseconds(shifted)
	if err != nil {
		return Int128{}, err
	}
	if len(possible) == 0 {
		return Int128{}, rangeErrorf("the local time does not exist")
	}
	if d == DisambiguationEarlier {
		return possible[0], nil
	}
	return possible[len(possible)-1], nil
}

// StartOfDay implements GetStartOfDay.
func (z TimeZone) StartOfDay(d ISODate) (Int128, error) {
	dt := ISODateTime{
		Date: d,
	}
	possible, err := z.PossibleEpochNanoseconds(dt)
	if err != nil {
		return Int128{}, err
	}
	if len(possible) > 0 {
		return possible[0], nil
	}
	before := dt.UTCEpochNanoseconds().Sub(I128(NsPerDay))
	if !IsValidEpochNanoseconds(before) {
		return Int128{}, rangeErrorf("date-time is out of range")
	}
	t, ok := z.Transition(before, true)
	if !ok {
		return Int128{}, rangeErrorf("the start of the day does not exist")
	}
	return t, nil
}

// Transition implements GetNamedTimeZoneNextTransition when next is true and
// GetNamedTimeZonePreviousTransition when it is false. It reports false for the null result.
func (z TimeZone) Transition(epochNs Int128, next bool) (Int128, bool) {
	if !z.hasTransitions() {
		return Int128{}, false
	}
	var s int64
	if next {
		sec, _ := epochNs.FloorDivInt64(NsPerSecond)
		s, _ = sec.Int64()
	} else {
		sec, r := epochNs.FloorDivInt64(NsPerSecond)
		if r != 0 {
			sec = sec.Add(I128(1))
		}
		s, _ = sec.Int64()
	}
	t, ok := z.zone.Transition(s, next)
	if !ok {
		return Int128{}, false
	}
	ns := Mul64(t, NsPerSecond)
	if !IsValidEpochNanoseconds(ns) {
		return Int128{}, false
	}
	return ns, true
}
