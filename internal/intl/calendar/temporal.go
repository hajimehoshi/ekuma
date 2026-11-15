// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package calendar

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// Temporal is the set of the calendars for package temporal.
var Temporal temporal.Calendars = temporalCalendars{}

type temporalCalendars struct{}

func (temporalCalendars) Canonicalize(id string) (string, bool) {
	return Canonicalize(id)
}

func (temporalCalendars) Lookup(id string) temporal.Calendar {
	return temporalCalendar{
		c: Lookup(id),
	}
}

// temporalCalendar implements the abstract operations of ECMA-402 for a calendar other than
// ISO 8601.
type temporalCalendar struct {
	c *Calendar
}

func rangeError(msg string) error {
	return &temporal.Error{
		Kind:    temporal.RangeError,
		Message: msg,
	}
}

func typeError(msg string) error {
	return &temporal.Error{
		Kind:    temporal.TypeError,
		Message: msg,
	}
}

// maxYear bounds the arithmetic years that the calculations accept. It is beyond the years of
// the dates that Temporal can represent in any calendar.
const maxYear = 300000

// The first and the last day within the limits of Temporal, -271821-04-19 and +275760-09-13.
const (
	minEpochDays = -100000001
	maxEpochDays = 100000000
)

// errOutOfRange is the error of a date that cannot be within the limits of Temporal.
var errOutOfRange = rangeError("date is out of range")

func yearInRange(y float64) bool {
	return y >= -maxYear && y <= maxYear
}

func parseMonthCode(s string) MonthCode {
	n, leap, _ := temporal.ParseMonthCode(s)
	return MonthCode{
		Number: n,
		Leap:   leap,
	}
}

func (t temporalCalendar) ISOToDate(d temporal.ISODate) temporal.CalendarDate {
	x := t.c.Date(d.EpochDays())
	return temporal.CalendarDate{
		Era:          x.Era,
		EraYear:      x.EraYear,
		Year:         x.Year,
		Month:        x.Month,
		MonthCode:    x.MonthCode.String(),
		Day:          x.Day,
		DayOfWeek:    d.DayOfWeek(),
		DayOfYear:    x.DayOfYear,
		DaysInWeek:   7,
		DaysInMonth:  x.DaysInMonth,
		DaysInYear:   x.DaysInYear,
		MonthsInYear: x.MonthsInYear,
		InLeapYear:   x.InLeapYear,
	}
}

func (t temporalCalendar) HasEras() bool {
	return t.c.HasEras()
}

func (t temporalCalendar) HasMidYearEras() bool {
	return t.c.HasMidYearEras()
}

// ResolveFields implements NonISOResolveFields.
func (t temporalCalendar) ResolveFields(f *temporal.CalendarFields, typ temporal.FieldsType) error {
	c := t.c
	needsYear := typ == temporal.FieldsDate || typ == temporal.FieldsYearMonth || !f.HasMonthCode || f.HasMonth
	needsDay := typ == temporal.FieldsDate || typ == temporal.FieldsMonthDay
	if needsYear && !f.HasYear {
		if !c.HasEras() || !f.HasEra || !f.HasEraYear {
			return typeError("year or era and eraYear are required")
		}
	}
	if c.HasEras() && f.HasEra != f.HasEraYear {
		return typeError("era and eraYear must be given together")
	}
	if needsDay && !f.HasDay {
		return typeError("day is required")
	}
	if !f.HasMonth && !f.HasMonthCode {
		return typeError("month or monthCode is required")
	}
	if c.HasEras() && f.HasEraYear {
		if !yearInRange(f.EraYear) {
			return errOutOfRange
		}
		y, ok := c.YearOfEra(f.Era, int(f.EraYear))
		if !ok {
			return rangeError("unknown era " + f.Era)
		}
		if f.HasYear && f.Year != float64(y) {
			return rangeError("year and era year differ")
		}
		f.Year, f.HasYear = float64(y), true
	}
	f.HasEra, f.HasEraYear = false, false
	if f.HasMonthCode {
		mc := parseMonthCode(f.MonthCode)
		if !c.IsValidMonthCode(mc) {
			return rangeError("month code " + f.MonthCode + " is not in the calendar " + c.id)
		}
		if f.HasYear {
			if !yearInRange(f.Year) {
				return errOutOfRange
			}
			y := int(f.Year)
			m, ok := c.Ordinal(y, mc)
			if !ok {
				m, _ = c.Ordinal(y, c.ConstrainMonthCode(mc))
			}
			if f.HasMonth && f.Month != float64(m) {
				return rangeError("month and monthCode differ")
			}
			f.Month, f.HasMonth = float64(m), true
		}
	}
	return nil
}

// constrainMonthCode implements ConstrainMonthCode and returns the ordinal month.
func (t temporalCalendar) constrainMonthCode(y int, mc MonthCode, overflow temporal.Overflow) (int, error) {
	if m, ok := t.c.Ordinal(y, mc); ok {
		return m, nil
	}
	if overflow == temporal.OverflowReject {
		return 0, rangeError("the year has no month " + mc.String())
	}
	m, _ := t.c.Ordinal(y, t.c.ConstrainMonthCode(mc))
	return m, nil
}

func toISODate(days int64) (temporal.ISODate, error) {
	// Days beyond the limits would overflow the conversion.
	if days < -1e9 || days > 1e9 {
		return temporal.ISODate{}, errOutOfRange
	}
	return temporal.EpochDaysToISODate(days), nil
}

// DateToISO implements NonISOCalendarDateToISO.
func (t temporalCalendar) DateToISO(f *temporal.CalendarFields, overflow temporal.Overflow) (temporal.ISODate, error) {
	c := t.c
	if !yearInRange(f.Year) {
		return temporal.ISODate{}, errOutOfRange
	}
	y := int(f.Year)
	if f.HasMonthCode {
		if _, err := t.constrainMonthCode(y, parseMonthCode(f.MonthCode), overflow); err != nil {
			return temporal.ISODate{}, err
		}
	}
	months := c.MonthsInYear(y)
	m := months
	if f.Month > float64(months) {
		if overflow == temporal.OverflowReject {
			return temporal.ISODate{}, rangeError("month is out of range")
		}
	} else {
		m = int(f.Month)
	}
	days := c.DaysInMonth(y, m)
	d := days
	if f.Day > float64(days) {
		if overflow == temporal.OverflowReject {
			return temporal.ISODate{}, rangeError("day is out of range")
		}
	} else {
		d = int(f.Day)
	}
	return toISODate(c.EpochDays(y, m, d))
}

// eastAsianReferenceYears are the ISO reference years of the month codes of the Chinese and
// Korean calendars for days 1 to 29 and for day 30, by month code number and leap flag. 0 is a
// month day that is not known to occur, and -1 for a leap M11 is 2033 for days 1 to 10 and
// 2034 for days 11 to 29.
var eastAsianReferenceYears = [13][2][2]int{
	1:  {{1972, 1970}, {0, 0}},
	2:  {{1972, 1972}, {1947, 0}},
	3:  {{1972, 1966}, {1966, 1955}},
	4:  {{1972, 1970}, {1963, 1944}},
	5:  {{1972, 1972}, {1971, 1952}},
	6:  {{1972, 1971}, {1960, 1941}},
	7:  {{1972, 1972}, {1968, 1938}},
	8:  {{1972, 1971}, {1957, 0}},
	9:  {{1972, 1972}, {2014, 0}},
	10: {{1972, 1972}, {1984, 0}},
	11: {{1972, 1970}, {-1, 0}},
	12: {{1972, 1972}, {0, 0}},
}

// eastAsianReferenceYear returns the ISO reference year of a month day of the Chinese or the
// Korean calendar, or 0 if it is not known to occur.
func (c *Calendar) eastAsianReferenceYear(mc MonthCode, day int) int {
	i := 0
	if mc.Leap {
		i = 1
	}
	j := 0
	if day == 30 {
		j = 1
	}
	y := eastAsianReferenceYears[mc.Number][i][j]
	switch {
	case y == -1 && day <= 10:
		return 2033
	case y == -1:
		return 2034
	case mc.Number == 3 && !mc.Leap && day == 30 && c.id == "dangi":
		return 1968
	}
	return y
}

// latestInISOYear returns the latest day in ISO year iy with the month code mc and day, and
// whether there is one.
func (c *Calendar) latestInISOYear(iy int64, mc MonthCode, day int) (int64, bool) {
	end := isoEpochDays(iy+1, 1, 1)
	for y := c.sys.yearOf(end - 1); ; y-- {
		if c.sys.newYear(y+1) <= isoEpochDays(iy, 1, 1) {
			return 0, false
		}
		m, ok := c.sys.ordinal(y, mc.Number, mc.Leap)
		if !ok || day > c.sys.daysInMonth(y, m) {
			continue
		}
		d := c.EpochDays(y, m, day)
		if d >= isoEpochDays(iy, 1, 1) && d < end {
			return d, true
		}
	}
}

// referenceDay returns the reference date of a month day: the latest from 1900 to 1972, or
// else the earliest from 1973 to 2035.
func (c *Calendar) referenceDay(mc MonthCode, day int) (int64, bool) {
	lo, hi := isoEpochDays(1900, 1, 1), isoEpochDays(1973, 1, 1)
	for y := c.sys.yearOf(hi - 1); c.sys.newYear(y+1) > lo; y-- {
		m, ok := c.sys.ordinal(y, mc.Number, mc.Leap)
		if !ok || day > c.sys.daysInMonth(y, m) {
			continue
		}
		if d := c.EpochDays(y, m, day); d >= lo && d < hi {
			return d, true
		}
	}
	end := isoEpochDays(2036, 1, 1)
	for y := c.sys.yearOf(hi); c.sys.newYear(y) < end; y++ {
		m, ok := c.sys.ordinal(y, mc.Number, mc.Leap)
		if !ok || day > c.sys.daysInMonth(y, m) {
			continue
		}
		if d := c.EpochDays(y, m, day); d >= hi && d < end {
			return d, true
		}
	}
	return 0, false
}

// MonthDayToISOReferenceDate implements NonISOMonthDayToISOReferenceDate.
func (t temporalCalendar) MonthDayToISOReferenceDate(f *temporal.CalendarFields, overflow temporal.Overflow) (temporal.ISODate, error) {
	c := t.c
	var mc MonthCode
	var days int
	if f.HasYear {
		if !yearInRange(f.Year) {
			return temporal.ISODate{}, errOutOfRange
		}
		y := int(f.Year)
		// The year must have a day within the limits.
		if c.sys.newYear(y+1) <= minEpochDays || c.sys.newYear(y) > maxEpochDays {
			return temporal.ISODate{}, errOutOfRange
		}
		months := c.MonthsInYear(y)
		m := months
		if f.Month > float64(months) {
			if overflow == temporal.OverflowReject {
				return temporal.ISODate{}, rangeError("month is out of range")
			}
		} else {
			m = int(f.Month)
		}
		if f.HasMonthCode {
			om, err := t.constrainMonthCode(y, parseMonthCode(f.MonthCode), overflow)
			if err != nil {
				return temporal.ISODate{}, err
			}
			mc = c.MonthCodeOf(y, om)
		} else {
			mc = c.MonthCodeOf(y, m)
		}
		days = c.DaysInMonth(y, m)
	} else {
		mc = parseMonthCode(f.MonthCode)
		days = c.maxDaysInMonth(mc)
	}
	day := days
	if f.Day > float64(days) {
		if overflow == temporal.OverflowReject {
			return temporal.ISODate{}, rangeError("day is out of range")
		}
	} else {
		day = int(f.Day)
	}
	if c.eastAsian {
		if c.eastAsianReferenceYear(mc, day) == 0 {
			if overflow == temporal.OverflowReject {
				return temporal.ISODate{}, rangeError("the month day is not known to occur")
			}
			mc.Leap = false
		}
		d, ok := c.latestInISOYear(int64(c.eastAsianReferenceYear(mc, day)), mc, day)
		if !ok {
			return temporal.ISODate{}, rangeError("the month day is not known to occur")
		}
		return temporal.EpochDaysToISODate(d), nil
	}
	d, ok := c.referenceDay(mc, day)
	if !ok {
		return temporal.ISODate{}, rangeError("the month day is not known to occur")
	}
	return temporal.EpochDaysToISODate(d), nil
}

// balanceMonths implements BalanceNonISODate for the month: it returns the year and the month
// of month m of year y, where m may be out of range.
func (c *Calendar) balanceMonths(y int, m int64) (int, int, bool) {
	if m >= 1 && m <= int64(c.sys.monthsInYear(y)) {
		return y, int(m), true
	}
	i := c.sys.monthsBefore(y) + m - 1
	// Estimate the year from the mean months per year, and adjust it.
	perYear := float64(c.sys.monthsBefore(1000)-c.sys.monthsBefore(0)) / 1000
	e := math.Floor(float64(i) / perYear)
	if e < -maxYear || e > maxYear {
		return 0, 0, false
	}
	ry := int(e)
	for c.sys.monthsBefore(ry) > i {
		ry--
	}
	for c.sys.monthsBefore(ry+1) <= i {
		ry++
	}
	if ry < -maxYear || ry > maxYear {
		return 0, 0, false
	}
	return ry, int(i-c.sys.monthsBefore(ry)) + 1, true
}

// DateAdd implements NonISODateAdd.
func (t temporalCalendar) DateAdd(d temporal.ISODate, dd temporal.DateDuration, overflow temporal.Overflow) (temporal.ISODate, error) {
	c := t.c
	parts := c.Date(d.EpochDays())
	if dd.Years < -maxYear || dd.Years > maxYear || dd.Months < -13*maxYear || dd.Months > 13*maxYear {
		return temporal.ISODate{}, errOutOfRange
	}
	y0 := parts.Year + int(dd.Years)
	if !yearInRange(float64(y0)) {
		return temporal.ISODate{}, errOutOfRange
	}
	m0, err := t.constrainMonthCode(y0, parts.MonthCode, overflow)
	if err != nil {
		return temporal.ISODate{}, err
	}
	y, m, ok := c.balanceMonths(y0, int64(m0)+dd.Months)
	if !ok {
		return temporal.ISODate{}, errOutOfRange
	}
	day := parts.Day
	if days := c.DaysInMonth(y, m); day > days {
		if overflow == temporal.OverflowReject {
			return temporal.ISODate{}, rangeError("day is out of range")
		}
		day = days
	}
	extra := dd.Days + 7*dd.Weeks
	if extra < -1e9 || extra > 1e9 {
		return temporal.ISODate{}, errOutOfRange
	}
	return toISODate(c.EpochDays(y, m, day) + extra)
}

// compareSurpasses implements CompareSurpasses. It compares the month code mc when byCode is
// true, and the ordinal month otherwise.
func compareSurpasses(sign int, year int, mc MonthCode, month int, byCode bool, day int, target Date) bool {
	switch {
	case year != target.Year:
		return sign*(year-target.Year) > 0
	case byCode && mc != target.MonthCode:
		a, b := mc.String(), target.MonthCode.String()
		if sign > 0 {
			return a > b
		}
		return b > a
	case !byCode && month != target.Month:
		return sign*(month-target.Month) > 0
	case day != target.Day:
		return sign*(day-target.Day) > 0
	}
	return false
}

// surpasses implements NonISODateSurpasses without weeks and days.
func (t temporalCalendar) surpasses(sign int, parts, target Date, years, months int64) bool {
	c := t.c
	y0 := parts.Year + int(years)
	if compareSurpasses(sign, y0, parts.MonthCode, 0, true, parts.Day, target) {
		return true
	}
	m0, _ := t.constrainMonthCode(y0, parts.MonthCode, temporal.OverflowConstrain)
	y, m, ok := c.balanceMonths(y0, int64(m0)+months)
	if !ok {
		return true
	}
	return compareSurpasses(sign, y, MonthCode{}, m, false, parts.Day, target)
}

// DateUntil implements NonISODateUntil.
func (t temporalCalendar) DateUntil(one, two temporal.ISODate, largestUnit temporal.Unit) temporal.DateDuration {
	c := t.c
	sign := -temporal.CompareISODate(one, two)
	if sign == 0 {
		return temporal.DateDuration{}
	}
	d1, d2 := one.EpochDays(), two.EpochDays()
	parts := c.Date(d1)
	target := c.Date(d2)
	var years, months int64
	if largestUnit == temporal.UnitYear {
		// Any count of years two years short of the difference of the years does not surpass.
		years = int64(target.Year - parts.Year - 2*sign)
		if int(years)*sign < 0 {
			years = 0
		}
		for !t.surpasses(sign, parts, target, years+int64(sign), 0) {
			years += int64(sign)
		}
	}
	if largestUnit == temporal.UnitYear || largestUnit == temporal.UnitMonth {
		// Any count of months two months short of the difference of the months does not
		// surpass.
		i1 := c.sys.monthsBefore(parts.Year) + int64(parts.Month)
		i2 := c.sys.monthsBefore(target.Year) + int64(target.Month)
		months = i2 - i1 - 2*int64(sign)
		if largestUnit == temporal.UnitYear || months*int64(sign) < 0 {
			months = 0
		}
		for !t.surpasses(sign, parts, target, years, months+int64(sign)) {
			months += int64(sign)
		}
	}
	// The remaining days count from the regulated day after the years and the months.
	y0 := parts.Year + int(years)
	m0, _ := t.constrainMonthCode(y0, parts.MonthCode, temporal.OverflowConstrain)
	y, m, _ := c.balanceMonths(y0, int64(m0)+months)
	day := min(parts.Day, c.DaysInMonth(y, m))
	days := d2 - c.EpochDays(y, m, day)
	var weeks int64
	if largestUnit == temporal.UnitWeek {
		weeks = days / 7
		days %= 7
	}
	return temporal.DateDuration{
		Years:  years,
		Months: months,
		Weeks:  weeks,
		Days:   days,
	}
}
