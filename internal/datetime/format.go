// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"math"
	"strconv"
	"strings"
)

var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

var monthNames = [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// pad appends n, which must be a non-negative integer, with at least width digits.
func pad(b []byte, n float64, width int) []byte {
	var buf [24]byte
	var s []byte
	if n < maxExactInt {
		s = strconv.AppendUint(buf[:0], uint64(n), 10)
	} else {
		s = strconv.AppendFloat(buf[:0], n, 'f', 0, 64)
	}
	for i := len(s); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// appendYear appends the year y with at least four digits and a minus sign when negative.
func appendYear(b []byte, y float64) []byte {
	if y < 0 {
		b = append(b, '-')
	}
	return pad(b, math.Abs(y), 4)
}

// appendDate implements DateString.
func appendDate(b []byte, tv float64) []byte {
	year, month, date := calendarFromTime(tv)
	b = append(b, weekdayNames[int(WeekDay(tv))]...)
	b = append(b, ' ')
	b = append(b, monthNames[month]...)
	b = append(b, ' ')
	b = pad(b, date, 2)
	b = append(b, ' ')
	return appendYear(b, year)
}

// appendTime implements TimeString.
func appendTime(b []byte, tv float64) []byte {
	b = pad(b, HourFromTime(tv), 2)
	b = append(b, ':')
	b = pad(b, MinFromTime(tv), 2)
	b = append(b, ':')
	b = pad(b, SecFromTime(tv), 2)
	return append(b, " GMT"...)
}

// appendTimeZone implements TimeZoneString.
func appendTimeZone(b []byte, off float64, name string) []byte {
	if off >= 0 {
		b = append(b, '+')
	} else {
		b = append(b, '-')
	}
	off = math.Abs(off)
	b = pad(b, math.Floor(off/msPerHour), 2)
	b = pad(b, MinFromTime(off), 2)
	// Go reports a numeric offset such as "+0530" as the name of a zone without an abbreviation.
	if name == "" || name[0] == '+' || name[0] == '-' {
		return b
	}
	// Date.parse treats the name as a parenthesized comment. Some CLDR names
	// contain an unmatched parenthesis, including Wolof's Central Daylight Time.
	var depth int
	for _, c := range name {
		switch c {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return b
			}
			depth--
		}
	}
	if depth != 0 {
		return b
	}
	b = append(b, " ("...)
	b = append(b, name...)
	return append(b, ')')
}

// String implements ToDateString for the finite time value tv and UTC offset in milliseconds.
func String(tv, offset float64, name string) string {
	t := tv + offset
	b := appendDate(make([]byte, 0, 64), t)
	b = append(b, ' ')
	b = appendTime(b, t)
	return string(appendTimeZone(b, offset, name))
}

// DateString returns the result of Date.prototype.toDateString for the time value tv, which must not be NaN.
func DateString(tv float64, tz TimeZone) string {
	return string(appendDate(nil, LocalTime(tv, tz)))
}

// TimeString formats the local time of the finite time value tv with the UTC offset in milliseconds.
func TimeString(tv, offset float64, name string) string {
	b := appendTime(nil, tv+offset)
	return string(appendTimeZone(b, offset, name))
}

// UTCString returns the result of Date.prototype.toUTCString for the time value tv, which must not be NaN.
func UTCString(tv float64) string {
	year, month, date := calendarFromTime(tv)
	b := append(make([]byte, 0, 32), weekdayNames[int(WeekDay(tv))]...)
	b = append(b, ", "...)
	b = pad(b, date, 2)
	b = append(b, ' ')
	b = append(b, monthNames[month]...)
	b = append(b, ' ')
	b = appendYear(b, year)
	b = append(b, ' ')
	return string(appendTime(b, tv))
}

// ISOString returns the result of Date.prototype.toISOString for the time value tv, which must be finite.
func ISOString(tv float64) string {
	year, month, date := calendarFromTime(tv)
	b := make([]byte, 0, 32)
	switch {
	case year < 0:
		b = append(b, '-')
		b = pad(b, -year, 6)
	case year > 9999:
		b = append(b, '+')
		b = pad(b, year, 6)
	default:
		b = pad(b, year, 4)
	}
	b = append(b, '-')
	b = pad(b, float64(month)+1, 2)
	b = append(b, '-')
	b = pad(b, date, 2)
	b = append(b, 'T')
	b = pad(b, HourFromTime(tv), 2)
	b = append(b, ':')
	b = pad(b, MinFromTime(tv), 2)
	b = append(b, ':')
	b = pad(b, SecFromTime(tv), 2)
	b = append(b, '.')
	b = pad(b, MsFromTime(tv), 3)
	return string(append(b, 'Z'))
}

// LocaleString returns the result of Date.prototype.toLocaleString for the time value tv, which must not be NaN.
// The format is fixed: month/day/year, hour:minute:second and AM or PM, in local time.
func LocaleString(tv float64, tz TimeZone) string {
	return LocaleDateString(tv, tz) + ", " + LocaleTimeString(tv, tz)
}

// LocaleDateString returns the result of Date.prototype.toLocaleDateString for the time value tv, which must not be NaN.
func LocaleDateString(tv float64, tz TimeZone) string {
	t := LocalTime(tv, tz)
	year, month, date := calendarFromTime(t)
	var b strings.Builder
	b.WriteString(strconv.FormatFloat(float64(month)+1, 'f', 0, 64))
	b.WriteByte('/')
	b.WriteString(strconv.FormatFloat(date, 'f', 0, 64))
	b.WriteByte('/')
	b.WriteString(strconv.FormatFloat(year, 'f', 0, 64))
	return b.String()
}

// LocaleTimeString returns the result of Date.prototype.toLocaleTimeString for the time value tv, which must not be NaN.
func LocaleTimeString(tv float64, tz TimeZone) string {
	t := LocalTime(tv, tz)
	h := HourFromTime(t)
	suffix := " AM"
	if h >= 12 {
		suffix = " PM"
	}
	h = modulo(h, 12)
	if h == 0 {
		h = 12
	}
	b := strconv.AppendFloat(nil, h, 'f', 0, 64)
	b = append(b, ':')
	b = pad(b, MinFromTime(t), 2)
	b = append(b, ':')
	b = pad(b, SecFromTime(t), 2)
	return string(append(b, suffix...))
}
