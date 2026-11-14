// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"math"
	"time"
)

// maxSeconds bounds the seconds handed to the time package. It is beyond the range of
// time values, so a clamped instant is never the one of a valid time value.
const maxSeconds = 1e13

// zoneAt returns the zone of loc in effect at the time value t: its offset in milliseconds,
// its abbreviation, and the time values where it starts and ends (infinite when unbounded).
func zoneAt(t float64, loc *time.Location) (offset float64, name string, start, end float64) {
	sec := math.Floor(t / msPerSecond)
	sec = math.Max(-maxSeconds, math.Min(maxSeconds, sec))
	tm := time.Unix(int64(sec), 0).In(loc)
	name, off := tm.Zone()
	s, e := tm.ZoneBounds()
	start, end = math.Inf(-1), math.Inf(1)
	if !s.IsZero() {
		start = float64(s.Unix()) * msPerSecond
	}
	if !e.IsZero() {
		end = float64(e.Unix()) * msPerSecond
	}
	// The offsets of local mean time have seconds, which the offset in String cannot show.
	// Whole minutes keep String and Parse inverse to each other.
	return float64(off/60) * msPerMinute, name, start, end
}

// Offset returns the offset of loc from UTC in milliseconds at the time value t, and the
// abbreviation of the zone, which may be empty. t must be finite.
func Offset(t float64, loc *time.Location) (offset float64, name string) {
	offset, name, _, _ = zoneAt(t, loc)
	return offset, name
}

// LocalTime implements LocalTime with loc as the system time zone.
func LocalTime(t float64, loc *time.Location) float64 {
	if !isFinite(t) {
		return t
	}
	off, _ := Offset(t, loc)
	return t + off
}

// UTC implements UTC with loc as the system time zone. A local time that is repeated
// is the earlier of its instants, and a local time that is skipped is interpreted
// with the offset before the transition.
func UTC(t float64, loc *time.Location) float64 {
	if !isFinite(t) {
		return math.NaN()
	}
	// Every offset is less than a day, so the instants that t can be are within a day of it.
	lo, hi := t-MsPerDay, t+MsPerDay
	if lo < -maxTime-MsPerDay || hi > maxTime+MsPerDay {
		off, _ := Offset(t, loc)
		return t - off
	}
	var prevOff float64
	hasPrev := false
	for u := lo; ; {
		off, _, start, end := zoneAt(u, loc)
		cand := t - off
		if start <= cand && cand < end {
			return cand
		}
		if cand < start && hasPrev {
			// t is in a gap between the previous zone and this one.
			return t - prevOff
		}
		if end > hi {
			break
		}
		prevOff, hasPrev = off, true
		u = end
	}
	off, _ := Offset(t, loc)
	return t - off
}
