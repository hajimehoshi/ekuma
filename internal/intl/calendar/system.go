// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package calendar

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/unicode/icu4c"
	"github.com/hajimehoshi/ekuma/internal/unicode/icu4x"
)

// A system is the arithmetic of a calendar. Years are arithmetic years, months are ordinal
// months starting at 1, and days are counted from 1970-01-01.
type system interface {
	// newYear returns the first day of year y.
	newYear(y int) int64

	// yearOf returns the year that contains day d.
	yearOf(d int64) int

	// monthsInYear returns the number of months of year y.
	monthsInYear(y int) int

	// daysInMonth returns the number of days of month m of year y.
	daysInMonth(y, m int) int

	// monthCode returns the number of the month code of month m of year y, and whether it is
	// a leap month.
	monthCode(y, m int) (int, bool)

	// ordinal returns the month of year y with the month code number n and leap flag, and
	// whether year y has that month.
	ordinal(y, n int, leap bool) (int, bool)

	// monthsBefore returns the number of months from the start of year 0 to the start of
	// year y, negative for a year before 0.
	monthsBefore(y int) int64
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	return a - floorDiv(a, b)*b
}

// isoEpochDays returns the days from 1970-01-01 to the proleptic Gregorian date y-m-d.
func isoEpochDays(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// isoDate returns the proleptic Gregorian date of day d.
func isoDate(d int64) (y int64, m, day int) {
	z := d + 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y = yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	day = int(doy - (153*mp+2)/5 + 1)
	m = int(mp + 3)
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return y, m, day
}

func isISOLeapYear(y int64) bool {
	return y%4 == 0 && (y%100 != 0 || y%400 == 0)
}

func isoDaysInMonth(y int64, m int) int {
	switch m {
	case 2:
		if isISOLeapYear(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// commonMonths is the ordinal of a system whose months are the 12 common months.
func commonMonths(n int, leap bool) (int, bool) {
	if leap || n < 1 || n > 12 {
		return 0, false
	}
	return n, true
}

// gregorian is a calendar whose months and days are those of the ISO 8601 calendar. Its year
// y is the ISO year y - offset.
type gregorian struct {
	offset int
}

func (g gregorian) newYear(y int) int64 {
	return isoEpochDays(int64(y-g.offset), 1, 1)
}

func (g gregorian) yearOf(d int64) int {
	y, _, _ := isoDate(d)
	return int(y) + g.offset
}

func (g gregorian) monthsInYear(y int) int {
	return 12
}

func (g gregorian) daysInMonth(y, m int) int {
	return isoDaysInMonth(int64(y-g.offset), m)
}

func (g gregorian) monthCode(y, m int) (int, bool) {
	return m, false
}

func (g gregorian) ordinal(y, n int, leap bool) (int, bool) {
	return commonMonths(n, leap)
}

// coptic is the Coptic and Ethiopian calendar: twelve months of 30 days and a thirteenth of
// 5 days, or 6 days in leap years. epoch is the first day of year 1.
type coptic struct {
	epoch int64
}

func (c coptic) newYear(y int) int64 {
	return c.epoch + 365*int64(y-1) + floorDiv(int64(y), 4)
}

func (c coptic) yearOf(d int64) int {
	return int(floorDiv(4*(d-c.epoch)+1463, 1461))
}

func (c coptic) monthsInYear(y int) int {
	return 13
}

func (c coptic) daysInMonth(y, m int) int {
	if m < 13 {
		return 30
	}
	if floorMod(int64(y), 4) == 3 {
		return 6
	}
	return 5
}

func (c coptic) monthCode(y, m int) (int, bool) {
	return m, false
}

func (c coptic) ordinal(y, n int, leap bool) (int, bool) {
	if leap || n < 1 || n > 13 {
		return 0, false
	}
	return n, true
}

// indian is the Indian national calendar. Year y starts in ISO year y + 78.
type indian struct{}

func (indian) newYear(y int) int64 {
	iy := int64(y) + 78
	if isISOLeapYear(iy) {
		return isoEpochDays(iy, 3, 21)
	}
	return isoEpochDays(iy, 3, 22)
}

func (c indian) yearOf(d int64) int {
	iy, _, _ := isoDate(d)
	y := int(iy) - 78
	if d < c.newYear(y) {
		y--
	}
	return y
}

func (indian) monthsInYear(y int) int {
	return 12
}

func (indian) daysInMonth(y, m int) int {
	switch {
	case m == 1:
		if isISOLeapYear(int64(y) + 78) {
			return 31
		}
		return 30
	case m <= 6:
		return 31
	}
	return 30
}

func (indian) monthCode(y, m int) (int, bool) {
	return m, false
}

func (indian) ordinal(y, n int, leap bool) (int, bool) {
	return commonMonths(n, leap)
}

// persianEpoch is the day before the first day of year 1 of the Persian calendar, by the
// 33-year rule.
const persianEpoch = -492268

// persian is the Persian calendar with the 33-year rule, and the corrections of ICU4C where the
// equinox disagrees with the rule.
type persian struct{}

func (persian) newYear(y int) int64 {
	d := persianEpoch + 365*int64(y-1) + floorDiv(8*int64(y)+21, 33)
	// A year that the rule makes a leap year but the equinox does not starts the next year a
	// day earlier.
	if _, ok := slices.BinarySearch(icu4c.PersianNonLeapYears[:], int32(y-1)); ok {
		d--
	}
	return d
}

func (c persian) yearOf(d int64) int {
	y := int(1 + floorDiv(33*(d-persianEpoch)+3, 12053))
	for d < c.newYear(y) {
		y--
	}
	for d >= c.newYear(y+1) {
		y++
	}
	return y
}

func (persian) monthsInYear(y int) int {
	return 12
}

func (c persian) daysInMonth(y, m int) int {
	switch {
	case m <= 6:
		return 31
	case m <= 11:
		return 30
	}
	if c.newYear(y+1)-c.newYear(y) == 366 {
		return 30
	}
	return 29
}

func (persian) monthCode(y, m int) (int, bool) {
	return m, false
}

func (persian) ordinal(y, n int, leap bool) (int, bool) {
	return commonMonths(n, leap)
}

// The first days of year 1 of the tabular Hijri calendars.
const (
	hijriCivilEpoch        = -492148 // 0622-07-19
	hijriAstronomicalEpoch = -492149 // 0622-07-18
)

// tabularHijri is a tabular Hijri calendar with the leap years 2, 5, 7, 10, 13, 16, 18, 21,
// 24, 26 and 29 of the 30-year cycle. epoch is the first day of year 1.
type tabularHijri struct {
	epoch int64
}

func (c tabularHijri) newYear(y int) int64 {
	return c.epoch + 354*int64(y-1) + floorDiv(3+11*int64(y), 30)
}

func (c tabularHijri) yearOf(d int64) int {
	y := int(floorDiv(30*(d-c.epoch)+10646, 10631))
	for d < c.newYear(y) {
		y--
	}
	for d >= c.newYear(y+1) {
		y++
	}
	return y
}

func (tabularHijri) monthsInYear(y int) int {
	return 12
}

func (tabularHijri) daysInMonth(y, m int) int {
	if m%2 == 1 {
		return 30
	}
	if m == 12 && floorMod(14+11*int64(y), 30) < 11 {
		return 30
	}
	return 29
}

func (tabularHijri) monthCode(y, m int) (int, bool) {
	return m, false
}

func (tabularHijri) ordinal(y, n int, leap bool) (int, bool) {
	return commonMonths(n, leap)
}

// umalqura is the Umm al-Qura calendar: the published years, and the civil tabular calendar
// outside them.
type umalqura struct{}

var umalquraTabular = tabularHijri{epoch: hijriCivilEpoch}

// umalquraYear returns the data of year y, and whether it is published.
func umalquraYear(y int) (newYear int64, longMonths int32, ok bool) {
	i := y - icu4x.UmalquraStart
	if i < 0 || i >= len(icu4x.UmalquraYears) {
		return 0, 0, false
	}
	v := icu4x.UmalquraYears[i]
	return int64(v >> 12), v & 0xfff, true
}

func (umalqura) newYear(y int) int64 {
	if d, _, ok := umalquraYear(y); ok {
		return d
	}
	return umalquraTabular.newYear(y)
}

func (c umalqura) yearOf(d int64) int {
	y := umalquraTabular.yearOf(d)
	for d < c.newYear(y) {
		y--
	}
	for d >= c.newYear(y+1) {
		y++
	}
	return y
}

func (umalqura) monthsInYear(y int) int {
	return 12
}

func (c umalqura) daysInMonth(y, m int) int {
	if _, bits, ok := umalquraYear(y); ok {
		return 29 + int(bits>>(m-1)&1)
	}
	return umalquraTabular.daysInMonth(y, m)
}

func (umalqura) monthCode(y, m int) (int, bool) {
	return m, false
}

func (umalqura) ordinal(y, n int, leap bool) (int, bool) {
	return commonMonths(n, leap)
}

func (g gregorian) monthsBefore(y int) int64 {
	return 12 * int64(y)
}

func (c coptic) monthsBefore(y int) int64 {
	return 13 * int64(y)
}

func (indian) monthsBefore(y int) int64 {
	return 12 * int64(y)
}

func (persian) monthsBefore(y int) int64 {
	return 12 * int64(y)
}

func (tabularHijri) monthsBefore(y int) int64 {
	return 12 * int64(y)
}

func (umalqura) monthsBefore(y int) int64 {
	return 12 * int64(y)
}
