// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/rand/v2"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/quiet"
)

// DefaultTimeZone returns the time zone of local time. It is the local time zone of the host
// unless [agent.SetDefaultTimeZone] changed it.
func (ag *agent) DefaultTimeZone() datetime.TimeZone {
	if ag.timeZone == nil {
		return datetime.Local()
	}
	return ag.timeZone
}

// SetDefaultTimeZone sets the time zone of local time to tz. A nil tz restores the local time
// zone of the host.
func (ag *agent) SetDefaultTimeZone(tz datetime.TimeZone) {
	ag.timeZone = tz
}

// SetClock sets now as the clock of [agent.Now]. A nil now restores [time.Now].
func (ag *agent) SetClock(now func() time.Time) {
	ag.clock = now
	ag.HasHostClock = now != nil
}

// HasCustomClock reports whether reading the clock calls a host function.
func (ag *agent) HasCustomClock() bool {
	return ag.HasHostClock
}

// The limits of the current time: the limits of time values, and of epoch nanoseconds of
// Temporal.Instant.
// Now returns the current time of the clock, clamped to the range of time values. It
// implements HostSystemUTCEpochNanoseconds.
func (ag *agent) Now() time.Time {
	var t time.Time
	if ag.clock == nil {
		t = time.Now()
	} else {
		t = ag.clock()
	}
	return quiet.ClampSystemTime(t)
}

// SystemUTCEpochMilliseconds returns the current time as a time value.
func (ag *agent) SystemUTCEpochMilliseconds() float64 {
	return quiet.EpochMilliseconds(ag.Now())
}

// SetRandomSource sets src as the source of [agent.RandomNumber]. A nil src restores the global
// source of math/rand/v2.
func (ag *agent) SetRandomSource(src rand.Source) {
	ag.randomSource = src
}

// RandomNumber returns a number in [0, 1) for Math.random. With a source, it is the number
// that [rand.Rand.Float64] would return.
func (ag *agent) RandomNumber() float64 {
	if ag.randomSource == nil {
		return rand.Float64()
	}
	// The low 53 bits, as rand.Rand.Float64 takes them.
	return float64(ag.randomSource.Uint64()<<11>>11) / (1 << 53)
}
