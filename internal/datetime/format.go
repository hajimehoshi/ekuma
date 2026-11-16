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
	b = append(b, weekdayNames[int(WeekDay(tv))]...)
	b = append(b, ' ')
	b = append(b, monthNames[int(MonthFromTime(tv))]...)
	b = append(b, ' ')
	b = pad(b, DateFromTime(tv), 2)
	b = append(b, ' ')
	return appendYear(b, YearFromTime(tv))
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
func appendTimeZone(b []byte, tv float64, tz TimeZone) []byte {
	off, name := tz.Offset(tv)
	if off >= 0 {
		b = append(b, '+')
	} else {
		b = append(b, '-')
	}
	off = math.Abs(off)
	b = pad(b, math.Floor(off/msPerHour), 2)
	b = pad(b, MinFromTime(off), 2)
	// Go reports a numeric offset such as "+0530" as the name of a zone without an abbreviation.
	if name != "" && name[0] != '+' && name[0] != '-' {
		b = append(b, " ("...)
		b = append(b, name...)
		b = append(b, ')')
	}
	return b
}

// String implements ToDateString for the time value tv, which must not be NaN.
func String(tv float64, tz TimeZone) string {
	t := LocalTime(tv, tz)
	b := appendDate(make([]byte, 0, 64), t)
	b = append(b, ' ')
	b = appendTime(b, t)
	return string(appendTimeZone(b, tv, tz))
}

// DateString returns the result of Date.prototype.toDateString for the time value tv, which must not be NaN.
func DateString(tv float64, tz TimeZone) string {
	return string(appendDate(nil, LocalTime(tv, tz)))
}

// TimeString returns the result of Date.prototype.toTimeString for the time value tv, which must not be NaN.
func TimeString(tv float64, tz TimeZone) string {
	b := appendTime(nil, LocalTime(tv, tz))
	return string(appendTimeZone(b, tv, tz))
}

// UTCString returns the result of Date.prototype.toUTCString for the time value tv, which must not be NaN.
func UTCString(tv float64) string {
	b := append(make([]byte, 0, 32), weekdayNames[int(WeekDay(tv))]...)
	b = append(b, ", "...)
	b = pad(b, DateFromTime(tv), 2)
	b = append(b, ' ')
	b = append(b, monthNames[int(MonthFromTime(tv))]...)
	b = append(b, ' ')
	b = appendYear(b, YearFromTime(tv))
	b = append(b, ' ')
	return string(appendTime(b, tv))
}

// ISOString returns the result of Date.prototype.toISOString for the time value tv, which must be finite.
func ISOString(tv float64) string {
	b := make([]byte, 0, 32)
	switch y := YearFromTime(tv); {
	case y < 0:
		b = append(b, '-')
		b = pad(b, -y, 6)
	case y > 9999:
		b = append(b, '+')
		b = pad(b, y, 6)
	default:
		b = pad(b, y, 4)
	}
	b = append(b, '-')
	b = pad(b, MonthFromTime(tv)+1, 2)
	b = append(b, '-')
	b = pad(b, DateFromTime(tv), 2)
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
	var b strings.Builder
	b.WriteString(strconv.FormatFloat(MonthFromTime(t)+1, 'f', 0, 64))
	b.WriteByte('/')
	b.WriteString(strconv.FormatFloat(DateFromTime(t), 'f', 0, 64))
	b.WriteByte('/')
	b.WriteString(strconv.FormatFloat(YearFromTime(t), 'f', 0, 64))
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
