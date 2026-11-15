// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package calendar implements the calendars of ECMA-402 other than ISO 8601: their arithmetic,
// month codes and eras.
package calendar

import (
	"strconv"
)

// leapShift is the Leap to Common Month Transformation of a calendar.
type leapShift int

const (
	noLeapMonths leapShift = iota
	skipBackward
	skipForward
)

// eraStyle is how a calendar maps its years to eras.
type eraStyle int

const (
	noEras    eraStyle = iota
	singleEra          // one era whose year is the arithmetic year
	twoEras            // an epoch era from year 1, and a negative era before it
	ethiopian          // am from year 1, and aa, offset by 5500 years, before it
	japanese           // the Japanese eras from 1873, and the eras of gregory before
)

// Calendar is a calendar other than ISO 8601.
type Calendar struct {
	id    string
	sys   system
	eras  eraStyle
	names [2]string // the epoch era, and the era before it

	shift leapShift

	// thirteenth reports whether M13 is a month code of the calendar.
	thirteenth bool

	// lunisolar reports whether leap years have more months than other years.
	lunisolar bool

	// eastAsian reports whether c is the Chinese or the Korean calendar.
	eastAsian bool

	// commonDays is the number of days of a year that is not a leap year.
	commonDays int
}

var calendars = []*Calendar{
	{id: "buddhist", sys: gregorian{offset: 543}, eras: singleEra, names: [2]string{"be"}, commonDays: 365},
	{id: "chinese", sys: chinese, shift: skipBackward, lunisolar: true, eastAsian: true},
	{id: "coptic", sys: coptic{epoch: -615558}, eras: singleEra, names: [2]string{"am"}, thirteenth: true, commonDays: 365},
	{id: "dangi", sys: dangi, shift: skipBackward, lunisolar: true, eastAsian: true},
	{id: "ethioaa", sys: coptic{epoch: -716367 - 365*5500 - 5500/4}, eras: singleEra, names: [2]string{"aa"}, thirteenth: true, commonDays: 365},
	{id: "ethiopic", sys: coptic{epoch: -716367}, eras: ethiopian, names: [2]string{"am", "aa"}, thirteenth: true, commonDays: 365},
	{id: "gregory", sys: gregorian{}, eras: twoEras, names: [2]string{"ce", "bce"}, commonDays: 365},
	{id: "hebrew", sys: hebrew{}, eras: singleEra, names: [2]string{"am"}, shift: skipForward, lunisolar: true},
	{id: "indian", sys: indian{}, eras: singleEra, names: [2]string{"shaka"}, commonDays: 365},
	{id: "islamic-civil", sys: tabularHijri{epoch: hijriCivilEpoch}, eras: twoEras, names: [2]string{"ah", "bh"}, commonDays: 354},
	{id: "islamic-tbla", sys: tabularHijri{epoch: hijriAstronomicalEpoch}, eras: twoEras, names: [2]string{"ah", "bh"}, commonDays: 354},
	{id: "islamic-umalqura", sys: umalqura{}, eras: twoEras, names: [2]string{"ah", "bh"}, commonDays: 354},
	{id: "japanese", sys: gregorian{}, eras: japanese, names: [2]string{"ce", "bce"}, commonDays: 365},
	{id: "persian", sys: persian{}, eras: singleEra, names: [2]string{"ap"}, commonDays: 365},
	{id: "roc", sys: gregorian{offset: -1911}, eras: twoEras, names: [2]string{"roc", "broc"}, commonDays: 365},
}

// aliases maps the calendar aliases to their canonical identifiers.
var aliases = map[string]string{
	"ethiopic-amete-alem": "ethioaa",
	"islamicc":            "islamic-civil",
}

// Lookup returns the calendar with the canonical identifier id, or nil if there is none.
func Lookup(id string) *Calendar {
	for _, c := range calendars {
		if c.id == id {
			return c
		}
	}
	return nil
}

// Canonicalize returns the canonical identifier of the calendar identifier id, which is in
// lower case, and whether there is such a calendar.
func Canonicalize(id string) (string, bool) {
	if a, ok := aliases[id]; ok {
		return a, true
	}
	if Lookup(id) != nil {
		return id, true
	}
	return "", false
}

// ID returns the canonical identifier of c.
func (c *Calendar) ID() string {
	return c.id
}

// Date is a date of a calendar.
type Date struct {
	// Era is the era, or "" for a calendar without eras.
	Era     string
	EraYear int

	// Year is the arithmetic year.
	Year int

	// Month is the ordinal month, starting at 1.
	Month     int
	MonthCode MonthCode
	Day       int

	DayOfYear    int
	DaysInMonth  int
	DaysInYear   int
	MonthsInYear int
	InLeapYear   bool
}

// MonthCode is a month code: the number of the month in a common year, or of the month that
// a leap month follows.
type MonthCode struct {
	Number int
	Leap   bool
}

// String returns the month code as Temporal writes it, such as "M05L".
func (m MonthCode) String() string {
	s := "M"
	if m.Number < 10 {
		s += "0"
	}
	s += strconv.Itoa(m.Number)
	if m.Leap {
		s += "L"
	}
	return s
}

// The first days of the Japanese eras from Meiji 6, when Japan took the Gregorian calendar.
var japaneseEras = []struct {
	name  string
	start int64
	year  int // the ISO year of the first year of the era
}{
	{name: "reiwa", start: isoEpochDays(2019, 5, 1), year: 2019},
	{name: "heisei", start: isoEpochDays(1989, 1, 8), year: 1989},
	{name: "showa", start: isoEpochDays(1926, 12, 25), year: 1926},
	{name: "taisho", start: isoEpochDays(1912, 7, 30), year: 1912},
	{name: "meiji", start: isoEpochDays(1873, 1, 1), year: 1868},
}

// Date returns the date of c on day d, counted from 1970-01-01.
func (c *Calendar) Date(d int64) Date {
	y := c.sys.yearOf(d)
	ny := c.sys.newYear(y)
	months := c.sys.monthsInYear(y)
	start := ny
	m := 1
	for ; m < months; m++ {
		n := start + int64(c.sys.daysInMonth(y, m))
		if d < n {
			break
		}
		start = n
	}
	days := int(c.sys.newYear(y+1) - ny)
	r := Date{
		Year:         y,
		Month:        m,
		Day:          int(d-start) + 1,
		DayOfYear:    int(d-ny) + 1,
		DaysInMonth:  c.sys.daysInMonth(y, m),
		DaysInYear:   days,
		MonthsInYear: months,
	}
	if r.Day > r.DaysInMonth {
		// An approximation leaves days after the last month: keep them in it.
		r.Day = r.DaysInMonth
	}
	r.MonthCode.Number, r.MonthCode.Leap = c.sys.monthCode(y, m)
	if c.lunisolar {
		r.InLeapYear = months > 12
	} else {
		r.InLeapYear = days > c.commonDays
	}
	r.Era, r.EraYear = c.era(y, d)
	return r
}

// era returns the era and the era year of day d in year y.
func (c *Calendar) era(y int, d int64) (string, int) {
	switch c.eras {
	case singleEra:
		return c.names[0], y
	case twoEras:
		if y >= 1 {
			return c.names[0], y
		}
		return c.names[1], 1 - y
	case ethiopian:
		if y >= 1 {
			return c.names[0], y
		}
		return c.names[1], y + 5500
	case japanese:
		for _, e := range japaneseEras {
			if d >= e.start {
				return e.name, y - e.year + 1
			}
		}
		if y >= 1 {
			return c.names[0], y
		}
		return c.names[1], 1 - y
	}
	return "", 0
}

// EpochDays returns the day of year y, month m and day day of c, counted from 1970-01-01. The
// month and the day must exist in that year.
func (c *Calendar) EpochDays(y, m, day int) int64 {
	d := c.sys.newYear(y)
	for i := 1; i < m; i++ {
		d += int64(c.sys.daysInMonth(y, i))
	}
	return d + int64(day-1)
}

// MonthsInYear returns the number of months of year y.
func (c *Calendar) MonthsInYear(y int) int {
	return c.sys.monthsInYear(y)
}

// DaysInMonth returns the number of days of month m of year y.
func (c *Calendar) DaysInMonth(y, m int) int {
	return c.sys.daysInMonth(y, m)
}

// MonthCodeOf returns the month code of month m of year y.
func (c *Calendar) MonthCodeOf(y, m int) MonthCode {
	n, leap := c.sys.monthCode(y, m)
	return MonthCode{
		Number: n,
		Leap:   leap,
	}
}

// IsValidMonthCode implements IsValidMonthCodeForCalendar.
func (c *Calendar) IsValidMonthCode(mc MonthCode) bool {
	switch {
	case !mc.Leap && mc.Number >= 1 && mc.Number <= 12:
		return true
	case !mc.Leap && mc.Number == 13:
		return c.thirteenth
	case mc.Leap && mc.Number >= 1 && mc.Number <= 12:
		return c.shift == skipBackward || c.shift == skipForward && mc.Number == 5
	}
	return false
}

// Ordinal returns the ordinal month of the month code mc in year y, and whether year y has
// that month. mc must be valid for c.
func (c *Calendar) Ordinal(y int, mc MonthCode) (int, bool) {
	return c.sys.ordinal(y, mc.Number, mc.Leap)
}

// ConstrainMonthCode returns the common month that stands in for the leap month mc in a year
// without it, as ConstrainMonthCode does with constrain.
func (c *Calendar) ConstrainMonthCode(mc MonthCode) MonthCode {
	if c.shift == skipForward {
		return MonthCode{
			Number: mc.Number + 1,
		}
	}
	return MonthCode{
		Number: mc.Number,
	}
}

// HasEras reports whether c has eras, as CalendarSupportsEra does.
func (c *Calendar) HasEras() bool {
	return c.eras != noEras
}

// HasMidYearEras implements CalendarHasMidYearEras.
func (c *Calendar) HasMidYearEras() bool {
	return c.eras == japanese
}

// eraAliases maps the era aliases of the calendars to their canonical names.
var eraAliases = map[string]string{
	"ad": "ce",
	"bc": "bce",
}

// YearOfEra returns the arithmetic year of the year eraYear of era, which may be an alias,
// and whether c has the era, as CanonicalizeEraInCalendar and
// CalendarDateArithmeticYearForEraYear do.
func (c *Calendar) YearOfEra(era string, eraYear int) (int, bool) {
	if c.eras == twoEras || c.eras == japanese {
		if a, ok := eraAliases[era]; ok && c.names[0] == "ce" {
			era = a
		}
	}
	switch {
	case era == "":
	case era == c.names[0]:
		return eraYear, true
	case era == c.names[1] && c.eras == ethiopian:
		return eraYear - 5500, true
	case era == c.names[1]:
		return 1 - eraYear, true
	case c.eras == japanese:
		for _, e := range japaneseEras {
			if e.name == era {
				return e.year + eraYear - 1, true
			}
		}
	}
	return 0, false
}

// maxDaysInMonth returns the largest number of days of the month with the month code mc in any
// year.
func (c *Calendar) maxDaysInMonth(mc MonthCode) int {
	if c.eastAsian {
		return 30
	}
	// Every month length of these calendars occurs within 60 years.
	y0 := c.sys.yearOf(isoEpochDays(1972, 1, 1))
	n := 0
	for y := y0 - 30; y < y0+30; y++ {
		if m, ok := c.sys.ordinal(y, mc.Number, mc.Leap); ok {
			n = max(n, c.sys.daysInMonth(y, m))
		}
	}
	return n
}
