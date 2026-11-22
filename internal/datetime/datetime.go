// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package datetime implements the date and time operations of ECMAScript.
// Time values count milliseconds since 1970-01-01T00:00:00Z.
// Day numbers count calendar days from the same epoch, starting at zero.
package datetime

import (
	"math"
)

const (
	msPerSecond = 1000
	msPerMinute = 60 * msPerSecond
	msPerHour   = 60 * msPerMinute

	// MsPerDay is the number of milliseconds in a day.
	MsPerDay = 24 * msPerHour

	// maxTime is the largest absolute value of a time value.
	maxTime = 8.64e15
)

// maxExactInt is the largest magnitude below which every integer is a float64.
const maxExactInt = 1 << 53

// modulo returns x modulo y with the sign of y.
func modulo(x, y float64) float64 {
	if y != 0 && x == math.Trunc(x) && y == math.Trunc(y) && math.Abs(x) <= maxExactInt && math.Abs(y) <= maxExactInt {
		r := int64(x) % int64(y)
		if r != 0 && (r < 0) != (y < 0) {
			r += int64(y)
		}
		return float64(r)
	}
	r := math.Mod(x, y)
	if r != 0 && (r < 0) != (y < 0) {
		r += y
	}
	return r + 0 // Turn -0 into +0.
}

// Day returns the day number containing the time value t, with 1970-01-01 as day zero.
func Day(t float64) float64 {
	return math.Floor(t / MsPerDay)
}

// TimeWithinDay returns the milliseconds since midnight for the time value t.
func TimeWithinDay(t float64) float64 {
	return modulo(t, MsPerDay)
}

// DaysInYear returns the number of days (365 or 366) in the Gregorian calendar year y.
func DaysInYear(y float64) float64 {
	switch {
	case modulo(y, 4) != 0:
		return 365
	case modulo(y, 100) != 0:
		return 366
	case modulo(y, 400) != 0:
		return 365
	}
	return 366
}

// DayFromYear returns the day number of January 1 of year y.
func DayFromYear(y float64) float64 {
	return 365*(y-1970) + math.Floor((y-1969)/4) - math.Floor((y-1901)/100) + math.Floor((y-1601)/400)
}

// TimeFromYear returns the time value at midnight on January 1 of year y.
func TimeFromYear(y float64) float64 {
	return MsPerDay * DayFromYear(y)
}

// YearFromTime returns the calendar year containing the finite time value t.
func YearFromTime(t float64) float64 {
	y := math.Floor(t/(MsPerDay*365.2425)) + 1970
	for TimeFromYear(y) > t {
		y--
	}
	for TimeFromYear(y+1) <= t {
		y++
	}
	return y
}

// InLeapYear reports whether the finite time value t falls in a leap year.
func InLeapYear(t float64) bool {
	return DaysInYear(YearFromTime(t)) == 366
}

// cumulativeDays is the number of days before each month in a year that is not a leap year.
var cumulativeDays = [13]float64{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334, 365}

// daysBeforeMonth returns the number of days in a year before the month m (0 to 11).
func daysBeforeMonth(m int, leap bool) float64 {
	d := cumulativeDays[m]
	if leap && m >= 2 {
		d++
	}
	return d
}

// DayWithinYear returns the zero-based day of the year for the finite time value t.
func DayWithinYear(t float64) float64 {
	return Day(t) - DayFromYear(YearFromTime(t))
}

// monthFromTime returns the year, month (0 to 11), day within the year, and leap status of t.
func monthFromTime(t float64) (year float64, month int, day float64, leap bool) {
	year = YearFromTime(t)
	day = Day(t) - DayFromYear(year)
	leap = DaysInYear(year) == 366
	for month < 11 && day >= daysBeforeMonth(month+1, leap) {
		month++
	}
	return year, month, day, leap
}

// calendarFromTime returns the year, month (0 to 11), and day of the month of t.
func calendarFromTime(t float64) (year float64, month int, date float64) {
	year, month, day, leap := monthFromTime(t)
	return year, month, day - daysBeforeMonth(month, leap) + 1
}

// MonthFromTime returns the month of the finite time value t, from 0 (January) to 11 (December).
func MonthFromTime(t float64) float64 {
	_, m, _, _ := monthFromTime(t)
	return float64(m)
}

// DateFromTime returns the day of the month (1 to 31) for the finite time value t.
func DateFromTime(t float64) float64 {
	_, m, d, leap := monthFromTime(t)
	return d - daysBeforeMonth(m, leap) + 1
}

// WeekDay returns the weekday of the time value t, from 0 (Sunday) to 6 (Saturday).
func WeekDay(t float64) float64 {
	return modulo(Day(t)+4, 7)
}

// HourFromTime returns the hour of the day (0 to 23) for the time value t.
func HourFromTime(t float64) float64 {
	return modulo(math.Floor(t/msPerHour), 24)
}

// MinFromTime returns the minute within the hour (0 to 59) for the time value t.
func MinFromTime(t float64) float64 {
	return modulo(math.Floor(t/msPerMinute), 60)
}

// SecFromTime returns the second within the minute (0 to 59) for the time value t.
func SecFromTime(t float64) float64 {
	return modulo(math.Floor(t/msPerSecond), 60)
}

// MsFromTime returns the millisecond within the second (0 to 999) for the time value t.
func MsFromTime(t float64) float64 {
	return modulo(t, msPerSecond)
}

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// toInteger implements ToIntegerOrInfinity for a finite number.
func toInteger(f float64) float64 {
	return math.Trunc(f) + 0
}

// MakeTime combines hours, minutes, seconds, and milliseconds into a millisecond offset
// from midnight. It truncates each component toward zero and returns NaN for non-finite input.
func MakeTime(hour, min, sec, ms float64) float64 {
	if !isFinite(hour) || !isFinite(min) || !isFinite(sec) || !isFinite(ms) {
		return math.NaN()
	}
	// The conversions keep the compiler from fusing a multiplication and an addition,
	// which would round differently from the operators of ECMAScript.
	t := float64(toInteger(hour) * msPerHour)
	t = float64(t + float64(toInteger(min)*msPerMinute))
	t = float64(t + float64(toInteger(sec)*msPerSecond))
	return t + toInteger(ms)
}

// maxYear bounds the years that [MakeDay] accepts. A day in a year beyond it cannot
// make a valid time value with any reasonable date argument.
const maxYear = 1e6

// MakeDay returns the day number for the given year, zero-based month, and one-based date.
// It truncates the components toward zero and normalizes out-of-range months and dates.
// It returns NaN for non-finite input or if the year after month normalization is outside
// the range -1,000,000 to 1,000,000.
func MakeDay(year, month, date float64) float64 {
	if !isFinite(year) || !isFinite(month) || !isFinite(date) {
		return math.NaN()
	}
	y, m, dt := toInteger(year), toInteger(month), toInteger(date)
	ym := y + math.Floor(m/12)
	if math.Abs(ym) > maxYear {
		return math.NaN()
	}
	mn := int(modulo(m, 12))
	day := DayFromYear(ym) + daysBeforeMonth(mn, DaysInYear(ym) == 366)
	return day + dt - 1
}

// MakeDate returns the time value for the given day number and millisecond offset from
// midnight. It returns NaN if either input or the result is non-finite.
func MakeDate(day, time float64) float64 {
	if !isFinite(day) || !isFinite(time) {
		return math.NaN()
	}
	tv := float64(day*MsPerDay) + time
	if !isFinite(tv) {
		return math.NaN()
	}
	return tv
}

// MakeFullYear truncates year toward zero and maps years 0 through 99 to 1900 through 1999.
// It preserves NaN and infinities.
func MakeFullYear(year float64) float64 {
	if math.IsNaN(year) {
		return math.NaN()
	}
	if math.IsInf(year, 0) {
		return year
	}
	y := toInteger(year)
	if 0 <= y && y <= 99 {
		return 1900 + y
	}
	return y
}

// TimeClip truncates time toward zero to whole milliseconds. It returns NaN if time is
// non-finite or its magnitude exceeds 8.64e15 milliseconds.
func TimeClip(time float64) float64 {
	if !isFinite(time) || math.Abs(time) > maxTime {
		return math.NaN()
	}
	return toInteger(time)
}
