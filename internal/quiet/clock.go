// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"runtime"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

var maxSystemTime = time.Unix(8.64e12, 0)

var minSystemTime = time.Unix(-8.64e12, 0)

// ClampSystemTime returns t clamped to the range of time values.
func ClampSystemTime(t time.Time) time.Time {
	if t.After(maxSystemTime) {
		return maxSystemTime
	}
	if t.Before(minSystemTime) {
		return minSystemTime
	}
	return t
}

// SystemUTCEpochMilliseconds is agent.SystemUTCEpochMilliseconds without a host clock. It
// reports false with one.
func (q Context) SystemUTCEpochMilliseconds() (float64, bool) {
	if q.Realm.HasHostClock {
		return 0, false
	}
	return EpochMilliseconds(ClampSystemTime(time.Now())), true
}

// DefaultTimeZone returns the local time zone when it can be used without running host code.
func (q Context) DefaultTimeZone() (datetime.TimeZone, bool) {
	if runtime.GOOS == "js" || q.Realm.HasHostTimeZone {
		return nil, false
	}
	return datetime.Local(), true
}

func EpochMilliseconds(t time.Time) float64 {
	// UnixMilli rounds toward negative infinity, as the floor of the specification does.
	return float64(t.UnixMilli())
}
