// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"math"
	"time"
)

// TimeZone is the time zone of local time.
type TimeZone interface {
	// Offset returns the offset from UTC in milliseconds at the finite time value t,
	// a whole number of minutes, and the abbreviation of the zone, which may be empty.
	Offset(t float64) (offset float64, name string)
}

// maxSeconds bounds the seconds handed to the time package. It is beyond the range of
// time values, so a clamped instant is never the one of a valid time value.
const maxSeconds = 1e13

// locationZone is a [TimeZone] from the time package.
type locationZone struct {
	loc *time.Location
}

// LocationZone returns the [TimeZone] of loc.
func LocationZone(loc *time.Location) TimeZone {
	return locationZone{loc: loc}
}

func (z locationZone) Offset(t float64) (float64, string) {
	sec := math.Floor(t / msPerSecond)
	sec = math.Max(-maxSeconds, math.Min(maxSeconds, sec))
	name, off := time.Unix(int64(sec), 0).In(z.loc).Zone()
	// The offsets of local mean time have seconds, which the offset in String cannot show.
	// Whole minutes keep String and Parse inverse to each other.
	return float64(off/60) * msPerMinute, name
}

// LocalTime implements LocalTime with tz as the system time zone.
func LocalTime(t float64, tz TimeZone) float64 {
	if !isFinite(t) {
		return t
	}
	off, _ := tz.Offset(t)
	return t + off
}

// UTC implements UTC with tz as the system time zone. A local time that is repeated
// is the earlier of its instants, and a local time that is skipped is interpreted
// with the offset before the transition.
func UTC(t float64, tz TimeZone) float64 {
	if !isFinite(t) {
		return math.NaN()
	}
	// Every offset is less than a day, so the offsets that apply to the instants that t can be
	// are the ones before and after a transition near t. At most one transition is assumed there.
	before, _ := tz.Offset(t - MsPerDay)
	after, _ := tz.Offset(t + MsPerDay)
	if before == after {
		return t - before
	}
	u := math.NaN()
	for _, off := range [2]float64{before, after} {
		if o, _ := tz.Offset(t - off); o == off && (math.IsNaN(u) || t-off < u) {
			u = t - off
		}
	}
	if math.IsNaN(u) {
		// t is in a gap.
		return t - before
	}
	return u
}
