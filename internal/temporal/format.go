// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"math"
	"strconv"
	"strings"
)

// pad returns n in decimal with at least width digits.
func pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	if len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}
	return s
}

// PadISOYear implements PadISOYear.
func PadISOYear(y int) string {
	if y >= 0 && y <= 9999 {
		return pad(int64(y), 4)
	}
	sign := "+"
	if y < 0 {
		sign = "-"
		y = -y
	}
	return sign + pad(int64(y), 6)
}

// FormatFractionalSeconds implements FormatFractionalSeconds.
func FormatFractionalSeconds(subSecondNanoseconds int64, precision Precision) string {
	if precision == PrecisionAuto {
		if subSecondNanoseconds == 0 {
			return ""
		}
		return "." + strings.TrimRight(pad(subSecondNanoseconds, 9), "0")
	}
	if precision == 0 {
		return ""
	}
	return "." + pad(subSecondNanoseconds, 9)[:precision]
}

// FormatTimeString implements FormatTimeString. unseparated selects the style without colons.
func FormatTimeString(hour, minute, second int, subSecondNanoseconds int64, precision Precision, unseparated bool) string {
	sep := ":"
	if unseparated {
		sep = ""
	}
	s := pad(int64(hour), 2) + sep + pad(int64(minute), 2)
	if precision == PrecisionMinute {
		return s
	}
	return s + sep + pad(int64(second), 2) + FormatFractionalSeconds(subSecondNanoseconds, precision)
}

// FormatOffsetTimeZoneIdentifier implements FormatOffsetTimeZoneIdentifier.
func FormatOffsetTimeZoneIdentifier(offsetMinutes int, unseparated bool) string {
	sign := "+"
	if offsetMinutes < 0 {
		sign = "-"
		offsetMinutes = -offsetMinutes
	}
	return sign + FormatTimeString(offsetMinutes/60, offsetMinutes%60, 0, 0, PrecisionMinute, unseparated)
}

// FormatUTCOffsetNanoseconds implements FormatUTCOffsetNanoseconds.
func FormatUTCOffsetNanoseconds(offsetNs int64) string {
	sign := "+"
	if offsetNs < 0 {
		sign = "-"
		offsetNs = -offsetNs
	}
	hour := offsetNs / NsPerHour
	minute := offsetNs / NsPerMinute % 60
	second := offsetNs / NsPerSecond % 60
	sub := offsetNs % NsPerSecond
	precision := PrecisionAuto
	if second == 0 && sub == 0 {
		precision = PrecisionMinute
	}
	return sign + FormatTimeString(int(hour), int(minute), int(second), sub, precision, false)
}

// FormatDateTimeUTCOffsetRounded implements FormatDateTimeUTCOffsetRounded.
func FormatDateTimeUTCOffsetRounded(offsetNs int64) string {
	offsetNs = RoundNumberToIncrement(offsetNs, NsPerMinute, RoundHalfExpand)
	return FormatOffsetTimeZoneIdentifier(int(offsetNs/NsPerMinute), false)
}

// FormatCalendarAnnotation implements FormatCalendarAnnotation.
func FormatCalendarAnnotation(id string, show ShowCalendar) string {
	switch {
	case show == ShowCalendarNever:
		return ""
	case show == ShowCalendarAuto && id == "iso8601":
		return ""
	case show == ShowCalendarCritical:
		return "[!u-ca=" + id + "]"
	}
	return "[u-ca=" + id + "]"
}

// ISODateString returns d as YYYY-MM-DD.
func ISODateString(d ISODate) string {
	return PadISOYear(d.Year) + "-" + pad(int64(d.Month), 2) + "-" + pad(int64(d.Day), 2)
}

// TemporalDateToString implements TemporalDateToString.
func TemporalDateToString(d ISODate, calendar string, show ShowCalendar) string {
	return ISODateString(d) + FormatCalendarAnnotation(calendar, show)
}

// TimeRecordToString implements TimeRecordToString.
func TimeRecordToString(t Time, precision Precision) string {
	return FormatTimeString(t.Hour, t.Minute, t.Second, int64(t.SubSecondNanoseconds()), precision, false)
}

// ISODateTimeToString implements ISODateTimeToString.
func ISODateTimeToString(dt ISODateTime, calendar string, precision Precision, show ShowCalendar) string {
	return ISODateString(dt.Date) + "T" + TimeRecordToString(dt.Time, precision) + FormatCalendarAnnotation(calendar, show)
}

// TemporalYearMonthToString implements TemporalYearMonthToString.
func TemporalYearMonthToString(d ISODate, calendar string, show ShowCalendar) string {
	s := PadISOYear(d.Year) + "-" + pad(int64(d.Month), 2)
	if show == ShowCalendarAlways || show == ShowCalendarCritical || calendar != "iso8601" {
		s += "-" + pad(int64(d.Day), 2)
	}
	return s + FormatCalendarAnnotation(calendar, show)
}

// TemporalMonthDayToString implements TemporalMonthDayToString.
func TemporalMonthDayToString(d ISODate, calendar string, show ShowCalendar) string {
	s := pad(int64(d.Month), 2) + "-" + pad(int64(d.Day), 2)
	if show == ShowCalendarAlways || show == ShowCalendarCritical || calendar != "iso8601" {
		s = PadISOYear(d.Year) + "-" + s
	}
	return s + FormatCalendarAnnotation(calendar, show)
}

// TemporalInstantToString implements TemporalInstantToString. A nil tz stands for undefined.
func TemporalInstantToString(epochNs Int128, tz *TimeZone, precision Precision) string {
	out := UTC
	if tz != nil {
		out = *tz
	}
	dt := out.ISODateTimeFor(epochNs)
	s := ISODateTimeToString(dt, "iso8601", precision, ShowCalendarNever)
	if tz == nil {
		return s + "Z"
	}
	return s + FormatDateTimeUTCOffsetRounded(out.OffsetNanosecondsFor(epochNs))
}

// TemporalZonedDateTimeToString implements TemporalZonedDateTimeToString for an epochNs that
// is already rounded.
func TemporalZonedDateTimeToString(epochNs Int128, tz TimeZone, calendar string, precision Precision, showCalendar ShowCalendar, showTimeZone ShowTimeZone, showOffset bool) string {
	offsetNs := tz.OffsetNanosecondsFor(epochNs)
	dt := ISODateTimeFromEpochNanoseconds(epochNs.Add(I128(offsetNs)))
	s := ISODateTimeToString(dt, "iso8601", precision, ShowCalendarNever)
	if showOffset {
		s += FormatDateTimeUTCOffsetRounded(offsetNs)
	}
	switch showTimeZone {
	case ShowTimeZoneAuto:
		s += "[" + tz.ID() + "]"
	case ShowTimeZoneCritical:
		s += "[!" + tz.ID() + "]"
	}
	return s + FormatCalendarAnnotation(calendar, showCalendar)
}

// formatDecimal returns the integral Number f, f ≥ 0, in decimal without an exponent.
func formatDecimal(f float64) string {
	if f < 1<<62 {
		return strconv.FormatInt(int64(f), 10)
	}
	if f >= 1<<126 {
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	return int128FromFloat(f).String()
}

// TemporalDurationToString implements TemporalDurationToString.
func TemporalDurationToString(d *Duration, precision Precision) string {
	sign := DurationSign(d)
	var date, tm strings.Builder
	for _, p := range []struct {
		v float64
		c string
	}{{d.Years, "Y"}, {d.Months, "M"}, {d.Weeks, "W"}, {d.Days, "D"}} {
		if p.v != 0 {
			date.WriteString(formatDecimal(math.Abs(p.v)) + p.c)
		}
	}
	if d.Hours != 0 {
		tm.WriteString(formatDecimal(math.Abs(d.Hours)) + "H")
	}
	if d.Minutes != 0 {
		tm.WriteString(formatDecimal(math.Abs(d.Minutes)) + "M")
	}
	secs := TimeDurationFromComponents(0, 0, d.Seconds, d.Milliseconds, d.Microseconds, d.Nanoseconds)
	if !secs.IsZero() || DefaultTemporalLargestUnit(d) >= UnitSecond || precision != PrecisionAuto {
		q, r := secs.Abs().QuoRemInt64(NsPerSecond)
		tm.WriteString(q.String() + FormatFractionalSeconds(r, precision) + "S")
	}
	s := "P" + date.String()
	if sign < 0 {
		s = "-" + s
	}
	if tm.Len() > 0 {
		s += "T" + tm.String()
	}
	return s
}
