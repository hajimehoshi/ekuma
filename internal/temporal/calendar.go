// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"strings"
)

// ISO8601 is the identifier of the ISO 8601 calendar, the only available calendar.
const ISO8601 = "iso8601"

// CanonicalizeCalendar implements CanonicalizeCalendar.
func CanonicalizeCalendar(id string) (string, error) {
	if asciiLower(id) != ISO8601 {
		return "", rangeErrorf("unknown calendar %s", id)
	}
	return ISO8601, nil
}

// asciiLower returns s with the ASCII upper-case letters in lower case.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// CalendarFields is a Calendar Fields Record. A field is unset when its Has flag is false.
// The numeric fields are mathematical values.
type CalendarFields struct {
	Year, Month, Day                                              float64
	HasYear, HasMonth, HasDay                                     bool
	MonthCode                                                     string
	HasMonthCode                                                  bool
	Hour, Minute, Second, Millisecond, Microsecond, Nanosecond    float64
	HasHour, HasMinute, HasSecond, HasMillisecond, HasMicrosecond bool
	HasNanosecond                                                 bool
	Offset                                                        string
	HasOffset                                                     bool
	TimeZone                                                      TimeZone
	HasTimeZone                                                   bool
}

// SetTime sets the time fields of f to t.
func (f *CalendarFields) SetTime(t Time) {
	f.Hour, f.Minute, f.Second = float64(t.Hour), float64(t.Minute), float64(t.Second)
	f.Millisecond, f.Microsecond, f.Nanosecond = float64(t.Millisecond), float64(t.Microsecond), float64(t.Nanosecond)
	f.HasHour, f.HasMinute, f.HasSecond = true, true, true
	f.HasMillisecond, f.HasMicrosecond, f.HasNanosecond = true, true, true
}

// FieldsType is the type of ISODateToFields.
type FieldsType int

const (
	FieldsDate FieldsType = iota
	FieldsYearMonth
	FieldsMonthDay
)

// ISODateToFields implements ISODateToFields.
func ISODateToFields(d ISODate, t FieldsType) CalendarFields {
	f := CalendarFields{
		MonthCode:    CreateMonthCode(d.Month, false),
		HasMonthCode: true,
	}
	if t == FieldsMonthDay || t == FieldsDate {
		f.Day, f.HasDay = float64(d.Day), true
	}
	if t == FieldsYearMonth || t == FieldsDate {
		f.Year, f.HasYear = float64(d.Year), true
	}
	return f
}

// CalendarMergeFields implements CalendarMergeFields for the ISO 8601 calendar.
func CalendarMergeFields(fields, additional CalendarFields) CalendarFields {
	m := fields
	if additional.HasYear {
		m.Year, m.HasYear = additional.Year, true
	}
	// A month or a month code replaces both of them.
	if additional.HasMonth || additional.HasMonthCode {
		m.Month, m.HasMonth = additional.Month, additional.HasMonth
		m.MonthCode, m.HasMonthCode = additional.MonthCode, additional.HasMonthCode
	}
	if additional.HasDay {
		m.Day, m.HasDay = additional.Day, true
	}
	if additional.HasHour {
		m.Hour, m.HasHour = additional.Hour, true
	}
	if additional.HasMinute {
		m.Minute, m.HasMinute = additional.Minute, true
	}
	if additional.HasSecond {
		m.Second, m.HasSecond = additional.Second, true
	}
	if additional.HasMillisecond {
		m.Millisecond, m.HasMillisecond = additional.Millisecond, true
	}
	if additional.HasMicrosecond {
		m.Microsecond, m.HasMicrosecond = additional.Microsecond, true
	}
	if additional.HasNanosecond {
		m.Nanosecond, m.HasNanosecond = additional.Nanosecond, true
	}
	if additional.HasOffset {
		m.Offset, m.HasOffset = additional.Offset, true
	}
	if additional.HasTimeZone {
		m.TimeZone, m.HasTimeZone = additional.TimeZone, true
	}
	return m
}

// ParseMonthCode implements the validation of ParseMonthCode on a string. It returns the
// month number and whether the month is a leap month.
func ParseMonthCode(s string) (int, bool, error) {
	if len(s) < 3 || len(s) > 4 || s[0] != 'M' || !isDigit(s[1]) || !isDigit(s[2]) {
		return 0, false, rangeErrorf("invalid month code %s", s)
	}
	n := int(s[1]-'0')*10 + int(s[2]-'0')
	leap := len(s) == 4
	if leap && s[3] != 'L' {
		return 0, false, rangeErrorf("invalid month code %s", s)
	}
	if !leap && n == 0 {
		return 0, false, rangeErrorf("invalid month code %s", s)
	}
	return n, leap, nil
}

// CreateMonthCode implements CreateMonthCode.
func CreateMonthCode(n int, leap bool) string {
	s := "M" + pad(int64(n), 2)
	if leap {
		s += "L"
	}
	return s
}

// CalendarResolveFields implements CalendarResolveFields for the ISO 8601 calendar.
func CalendarResolveFields(f *CalendarFields, t FieldsType) error {
	if (t == FieldsDate || t == FieldsYearMonth) && !f.HasYear {
		return typeErrorf("year is required")
	}
	if (t == FieldsDate || t == FieldsMonthDay) && !f.HasDay {
		return typeErrorf("day is required")
	}
	if !f.HasMonthCode {
		if !f.HasMonth {
			return typeErrorf("month or monthCode is required")
		}
		return nil
	}
	n, leap, err := ParseMonthCode(f.MonthCode)
	if err != nil {
		return err
	}
	if leap || n < 1 || n > 12 {
		return rangeErrorf("month code %s is not in the ISO 8601 calendar", f.MonthCode)
	}
	if f.HasMonth && f.Month != float64(n) {
		return rangeErrorf("month %v and monthCode %s differ", f.Month, f.MonthCode)
	}
	f.Month, f.HasMonth = float64(n), true
	return nil
}

// CalendarDateToISO implements CalendarDateToISO for the ISO 8601 calendar.
func CalendarDateToISO(f *CalendarFields, overflow Overflow) (ISODate, error) {
	return RegulateISODate(f.Year, f.Month, f.Day, overflow)
}

// CalendarDateFromFields implements CalendarDateFromFields.
func CalendarDateFromFields(f CalendarFields, overflow Overflow) (ISODate, error) {
	if err := CalendarResolveFields(&f, FieldsDate); err != nil {
		return ISODate{}, err
	}
	d, err := CalendarDateToISO(&f, overflow)
	if err != nil {
		return ISODate{}, err
	}
	if !ISODateWithinLimits(d) {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	return d, nil
}

// CalendarYearMonthFromFields implements CalendarYearMonthFromFields.
func CalendarYearMonthFromFields(f CalendarFields, overflow Overflow) (ISODate, error) {
	if err := CalendarResolveFields(&f, FieldsYearMonth); err != nil {
		return ISODate{}, err
	}
	f.Day, f.HasDay = 1, true
	d, err := CalendarDateToISO(&f, overflow)
	if err != nil {
		return ISODate{}, err
	}
	if !ISOYearMonthWithinLimits(d) {
		return ISODate{}, rangeErrorf("year and month are out of range")
	}
	return d, nil
}

// CalendarMonthDayFromFields implements CalendarMonthDayFromFields.
func CalendarMonthDayFromFields(f CalendarFields, overflow Overflow) (ISODate, error) {
	if err := CalendarResolveFields(&f, FieldsMonthDay); err != nil {
		return ISODate{}, err
	}
	year := 1972.0
	if f.HasYear {
		year = f.Year
	}
	d, err := RegulateISODate(year, f.Month, f.Day, overflow)
	if err != nil {
		return ISODate{}, err
	}
	d.Year = 1972
	return d, nil
}

// CalendarDateAdd implements CalendarDateAdd for the ISO 8601 calendar.
func CalendarDateAdd(d ISODate, dd DateDuration, overflow Overflow) (ISODate, error) {
	y, m := BalanceISOYearMonth(int64(d.Year)+dd.Years, int64(d.Month)+dd.Months)
	if y < -1e6 || y > 1e6 {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	intermediate, err := RegulateISODate(float64(y), float64(m), float64(d.Day), overflow)
	if err != nil {
		return ISODate{}, err
	}
	days := dd.Days + 7*dd.Weeks
	if days < -1e9 || days > 1e9 {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	r := AddDaysToISODate(intermediate, days)
	if !ISODateWithinLimits(r) {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	return r, nil
}

// isoDateSurpasses implements ISODateSurpasses for the years and months of a difference.
func isoDateSurpasses(sign int, base ISODate, years, months int64, target ISODate) bool {
	y0 := int64(base.Year) + years
	if compareSurpasses(sign, y0, int64(base.Month), int64(base.Day), target) {
		return true
	}
	if months == 0 {
		return false
	}
	y, m := BalanceISOYearMonth(y0, int64(base.Month)+months)
	return compareSurpasses(sign, y, int64(m), int64(base.Day), target)
}

func compareSurpasses(sign int, year, month, day int64, target ISODate) bool {
	s := int64(sign)
	switch {
	case year != int64(target.Year):
		return s*(year-int64(target.Year)) > 0
	case month != int64(target.Month):
		return s*(month-int64(target.Month)) > 0
	case day != int64(target.Day):
		return s*(day-int64(target.Day)) > 0
	}
	return false
}

// CalendarDateUntil implements CalendarDateUntil for the ISO 8601 calendar.
func CalendarDateUntil(one, two ISODate, largestUnit Unit) DateDuration {
	sign := -CompareISODate(one, two)
	if sign == 0 {
		return DateDuration{}
	}
	var years, months int64
	if largestUnit == UnitYear || largestUnit == UnitMonth {
		candidate := int64(two.Year - one.Year)
		if candidate != 0 {
			candidate -= int64(sign)
		}
		for !isoDateSurpasses(sign, one, candidate, 0, two) {
			years = candidate
			candidate += int64(sign)
		}
		candidateMonths := int64(sign)
		for !isoDateSurpasses(sign, one, years, candidateMonths, two) {
			months = candidateMonths
			candidateMonths += int64(sign)
		}
		if largestUnit == UnitMonth {
			months += years * 12
			years = 0
		}
	}
	y, m := BalanceISOYearMonth(int64(one.Year)+years, int64(one.Month)+months)
	constrained := ISODate{
		Year:  int(y),
		Month: m,
		Day:   min(one.Day, ISODaysInMonth(int(y), m)),
	}
	days := two.EpochDays() - constrained.EpochDays()
	var weeks int64
	if largestUnit == UnitWeek {
		weeks = days / 7
		days %= 7
	}
	return DateDuration{
		Years:  years,
		Months: months,
		Weeks:  weeks,
		Days:   days,
	}
}
