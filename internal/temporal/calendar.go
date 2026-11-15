// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"strings"
)

// ISO8601 is the identifier of the ISO 8601 calendar.
const ISO8601 = "iso8601"

// Calendar is the arithmetic of a calendar other than ISO 8601. Its methods implement the
// abstract operations of the same names for that calendar.
type Calendar interface {
	ISOToDate(d ISODate) CalendarDate
	ResolveFields(f *CalendarFields, t FieldsType) error
	DateToISO(f *CalendarFields, overflow Overflow) (ISODate, error)
	MonthDayToISOReferenceDate(f *CalendarFields, overflow Overflow) (ISODate, error)
	DateAdd(d ISODate, dd DateDuration, overflow Overflow) (ISODate, error)
	DateUntil(one, two ISODate, largestUnit Unit) DateDuration

	// HasEras implements CalendarSupportsEra.
	HasEras() bool

	// HasMidYearEras implements CalendarHasMidYearEras.
	HasMidYearEras() bool
}

// Calendars is a set of calendars other than ISO 8601.
type Calendars interface {
	// Canonicalize returns the canonical identifier of the lower-case calendar identifier id,
	// and whether the set has that calendar.
	Canonicalize(id string) (string, bool)

	// Lookup returns the calendar with the canonical identifier id.
	Lookup(id string) Calendar
}

// calendars is the set of calendars other than ISO 8601, or nil.
var calendars Calendars

// SetCalendars makes the calendars of c available besides ISO 8601. It must be called before
// any other function of this package, from an init function.
func SetCalendars(c Calendars) {
	calendars = c
}

// CanonicalizeCalendar implements CanonicalizeCalendar.
func CanonicalizeCalendar(id string) (string, error) {
	l := asciiLower(id)
	if l == ISO8601 {
		return ISO8601, nil
	}
	if calendars != nil {
		if c, ok := calendars.Canonicalize(l); ok {
			return c, nil
		}
	}
	return "", rangeErrorf("unknown calendar %s", id)
}

// nonISO returns the calendar cal, or nil when cal is the ISO 8601 calendar.
func nonISO(cal string) Calendar {
	if cal == ISO8601 {
		return nil
	}
	return calendars.Lookup(cal)
}

// CalendarDate is a Calendar Date Record. Era is empty for a calendar without eras, and
// WeekOfYear and YearOfWeek are zero for a calendar without week numbering.
type CalendarDate struct {
	Era          string
	EraYear      int
	Year         int
	Month        int
	MonthCode    string
	Day          int
	DayOfWeek    int
	DayOfYear    int
	WeekOfYear   int
	YearOfWeek   int
	DaysInWeek   int
	DaysInMonth  int
	DaysInYear   int
	MonthsInYear int
	InLeapYear   bool
}

// CalendarISOToDate implements CalendarISOToDate.
func CalendarISOToDate(cal string, d ISODate) CalendarDate {
	if c := nonISO(cal); c != nil {
		return c.ISOToDate(d)
	}
	w, wy := d.WeekOfYear()
	return CalendarDate{
		Year:         d.Year,
		Month:        d.Month,
		MonthCode:    CreateMonthCode(d.Month, false),
		Day:          d.Day,
		DayOfWeek:    d.DayOfWeek(),
		DayOfYear:    d.DayOfYear(),
		WeekOfYear:   w,
		YearOfWeek:   wy,
		DaysInWeek:   7,
		DaysInMonth:  ISODaysInMonth(d.Year, d.Month),
		DaysInYear:   DaysInYear(d.Year),
		MonthsInYear: 12,
		InLeapYear:   IsLeapYear(d.Year),
	}
}

// CalendarSupportsEra reports whether cal has eras, which makes era and eraYear calendar fields.
func CalendarSupportsEra(cal string) bool {
	c := nonISO(cal)
	return c != nil && c.HasEras()
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
	Era                                                           string
	HasEra                                                        bool
	EraYear                                                       float64
	HasEraYear                                                    bool
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
func ISODateToFields(cal string, d ISODate, t FieldsType) CalendarFields {
	year, monthCode, day := d.Year, CreateMonthCode(d.Month, false), d.Day
	if c := nonISO(cal); c != nil {
		cd := c.ISOToDate(d)
		year, monthCode, day = cd.Year, cd.MonthCode, cd.Day
	}
	f := CalendarFields{
		MonthCode:    monthCode,
		HasMonthCode: true,
	}
	if t == FieldsMonthDay || t == FieldsDate {
		f.Day, f.HasDay = float64(day), true
	}
	if t == FieldsYearMonth || t == FieldsDate {
		f.Year, f.HasYear = float64(year), true
	}
	return f
}

// CalendarMergeFields implements CalendarMergeFields.
func CalendarMergeFields(cal string, fields, additional CalendarFields) CalendarFields {
	m := fields
	c := nonISO(cal)
	// CalendarFieldKeysToIgnore: a month or a month code replaces both of them. In a calendar
	// with eras, any of era, eraYear and year replaces all three, and in a calendar with eras
	// that start in the middle of a year, a day or a month replaces the era and the era year.
	if c != nil && c.HasEras() && (additional.HasEra || additional.HasEraYear || additional.HasYear) {
		m.HasEra, m.HasEraYear, m.HasYear = false, false, false
	}
	if c != nil && c.HasMidYearEras() && (additional.HasDay || additional.HasMonth || additional.HasMonthCode) {
		m.HasEra, m.HasEraYear = false, false
	}
	if additional.HasEra {
		m.Era, m.HasEra = additional.Era, true
	}
	if additional.HasEraYear {
		m.EraYear, m.HasEraYear = additional.EraYear, true
	}
	if additional.HasYear {
		m.Year, m.HasYear = additional.Year, true
	}
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

// CalendarResolveFields implements CalendarResolveFields.
func CalendarResolveFields(cal string, f *CalendarFields, t FieldsType) error {
	if c := nonISO(cal); c != nil {
		return c.ResolveFields(f, t)
	}
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

// CalendarDateToISO implements CalendarDateToISO.
func CalendarDateToISO(cal string, f *CalendarFields, overflow Overflow) (ISODate, error) {
	if c := nonISO(cal); c != nil {
		return c.DateToISO(f, overflow)
	}
	return RegulateISODate(f.Year, f.Month, f.Day, overflow)
}

// CalendarDateFromFields implements CalendarDateFromFields.
func CalendarDateFromFields(cal string, f CalendarFields, overflow Overflow) (ISODate, error) {
	if err := CalendarResolveFields(cal, &f, FieldsDate); err != nil {
		return ISODate{}, err
	}
	d, err := CalendarDateToISO(cal, &f, overflow)
	if err != nil {
		return ISODate{}, err
	}
	if !ISODateWithinLimits(d) {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	return d, nil
}

// CalendarYearMonthFromFields implements CalendarYearMonthFromFields.
func CalendarYearMonthFromFields(cal string, f CalendarFields, overflow Overflow) (ISODate, error) {
	f.Day, f.HasDay = 1, true
	if err := CalendarResolveFields(cal, &f, FieldsYearMonth); err != nil {
		return ISODate{}, err
	}
	d, err := CalendarDateToISO(cal, &f, overflow)
	if err != nil {
		return ISODate{}, err
	}
	if !ISOYearMonthWithinLimits(d) {
		return ISODate{}, rangeErrorf("year and month are out of range")
	}
	return d, nil
}

// CalendarMonthDayFromFields implements CalendarMonthDayFromFields.
func CalendarMonthDayFromFields(cal string, f CalendarFields, overflow Overflow) (ISODate, error) {
	if err := CalendarResolveFields(cal, &f, FieldsMonthDay); err != nil {
		return ISODate{}, err
	}
	if c := nonISO(cal); c != nil {
		return c.MonthDayToISOReferenceDate(&f, overflow)
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

// CalendarDateAdd implements CalendarDateAdd.
func CalendarDateAdd(cal string, d ISODate, dd DateDuration, overflow Overflow) (ISODate, error) {
	if c := nonISO(cal); c != nil {
		r, err := c.DateAdd(d, dd, overflow)
		if err != nil {
			return ISODate{}, err
		}
		if !ISODateWithinLimits(r) {
			return ISODate{}, rangeErrorf("date is out of range")
		}
		return r, nil
	}
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

// CalendarDateUntil implements CalendarDateUntil.
func CalendarDateUntil(cal string, one, two ISODate, largestUnit Unit) DateDuration {
	sign := -CompareISODate(one, two)
	if sign == 0 {
		return DateDuration{}
	}
	if c := nonISO(cal); c != nil {
		return c.DateUntil(one, two, largestUnit)
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
