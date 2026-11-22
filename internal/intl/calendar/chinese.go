// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package calendar

// eastAsianYear is a year of the Chinese or Korean calendar, as ICU4X encodes it: bits 0 to 12
// are set for the months with 30 days, bits 13 to 16 are the ordinal of the leap month or 0,
// and bits 17 to 22 are the new year in days after January 19 of the related ISO year.
type eastAsianYear uint32

func (e eastAsianYear) leapMonth() int {
	return int(e >> 13 & 0xf)
}

func (e eastAsianYear) monthsInYear() int {
	if e.leapMonth() != 0 {
		return 13
	}
	return 12
}

func (e eastAsianYear) daysInMonth(m int) int {
	return 29 + int(e>>(m-1)&1)
}

// newEastAsianYear encodes a year. newYear is in days from 1970-01-01, and leap is the
// ordinal of the leap month or 0.
func newEastAsianYear(relatedISO int, long [13]bool, leap int, newYear int64) eastAsianYear {
	var e eastAsianYear
	for i, l := range long {
		if l {
			e |= 1 << i
		}
	}
	e |= eastAsianYear(leap) << 13
	// Like ICU4X, keep the offset in 6 bits even when an approximation goes out of range.
	offset := (newYear - isoEpochDays(int64(relatedISO), 1, 19)) & 0x3f
	e |= eastAsianYear(offset) << 17
	return e
}

// The parameters of the mean approximation of ICU4X, in milliseconds.
const (
	msPerDay = 24 * 60 * 60 * 1000

	// The mean Gregorian year and solar term.
	meanYear      = msPerDay * 146097 / 400
	meanSolarTerm = msPerDay * 146097 / 400 / 12

	// The mean synodic month on 2000-01-01, by the Astronomical Almanac (1992).
	meanSynodicMonth = msPerDay * 295305888531 / 10000000000

	utcPlus8      = msPerDay * 8 / 24
	utcPlus9      = msPerDay * 9 / 24
	beijingOffset = msPerDay * 1397 / 180 / 24 // the reference meridian of the Qing dynasty
)

// A moment is a day and the milliseconds since its start.
type moment struct {
	day int64
	ms  int64
}

func (m moment) add(ms int64) moment {
	t := m.ms + ms
	return moment{
		day: m.day + floorDiv(t, msPerDay),
		ms:  floorMod(t, msPerDay),
	}
}

var (
	// The December solstice of 1999, 1999-12-22T07:44Z.
	utcSolstice = moment{
		day: isoEpochDays(1999, 12, 22),
		ms:  (7*60 + 44) * 60 * 1000,
	}
	// The new moon of 2000-01-06T18:14Z.
	utcNewMoon = moment{
		day: isoEpochDays(2000, 1, 6),
		ms:  (18*60 + 14) * 60 * 1000,
	}
)

// lastPeriodOnOrBefore returns the latest moment base + n × period that falls on or before day.
func lastPeriodOnOrBefore(day int64, base moment, period int64) moment {
	diff := (day-base.day)*msPerDay - base.ms
	n := floorDiv(diff+msPerDay-1, period)
	t := base.day*msPerDay + base.ms + n*period
	return moment{
		day: floorDiv(t, msPerDay),
		ms:  floorMod(t, msPerDay),
	}
}

// simpleEastAsianYear is ICU4X's approximation of the year that starts in relatedISO: mean
// solar terms and mean new moons at the given UTC offset, as the píngqì rule of the Ming dynasty.
func simpleEastAsianYear(offset int64, relatedISO int) eastAsianYear {
	term := lastPeriodOnOrBefore(isoEpochDays(int64(relatedISO), 1, 1)-1, utcSolstice.add(offset), meanYear)
	newMoon := lastPeriodOnOrBefore(term.day, utcNewMoon.add(offset), meanSynodicMonth)
	next := newMoon.add(meanSynodicMonth)

	// The solstice is in month 11 of the previous year.
	solarTerm := -2
	hadLeap := false
	for solarTerm < 0 || next.day <= term.day && !hadLeap {
		if next.day <= term.day && !hadLeap {
			hadLeap = true
		} else {
			solarTerm++
			term = term.add(meanSolarTerm)
		}
		newMoon, next = next, next.add(meanSynodicMonth)
	}

	newYear := newMoon.day
	var long [13]bool
	leap := 0
	for solarTerm < 12 || next.day <= term.day && !hadLeap {
		i := solarTerm
		if leap != 0 {
			i++
		}
		if i < len(long) {
			long[i] = next.day-newMoon.day == 30
		}
		if next.day <= term.day && !hadLeap {
			hadLeap = true
			leap = solarTerm + 1
		} else {
			solarTerm++
			term = term.add(meanSolarTerm)
		}
		newMoon, next = next, next.add(meanSynodicMonth)
	}
	return newEastAsianYear(relatedISO, long, leap, newYear)
}

// eastAsian is the Chinese or the Korean calendar. Its arithmetic year is the ISO year in
// which the year starts.
type eastAsian struct {
	modern          []uint32
	modernStart     int
	historical      []uint32
	historicalStart int
	offset          int64
}

var (
	chinese = eastAsian{
		modern:          chinaYears[:],
		modernStart:     chinaStart,
		historical:      qingYears[:],
		historicalStart: qingStart,
		offset:          utcPlus8,
	}
	dangi = eastAsian{
		modern:          koreaYears[:],
		modernStart:     koreaStart,
		historical:      historicalKoreaYears[:],
		historicalStart: historicalKoreaStart,
		offset:          utcPlus9,
	}
)

func (c eastAsian) year(y int) eastAsianYear {
	if i := y - c.modernStart; i >= 0 && i < len(c.modern) {
		return eastAsianYear(c.modern[i])
	}
	if y > c.modernStart {
		return simpleEastAsianYear(c.offset, y)
	}
	if i := y - c.historicalStart; i >= 0 && i < len(c.historical) {
		return eastAsianYear(c.historical[i])
	}
	return simpleEastAsianYear(beijingOffset, y)
}

func (c eastAsian) newYear(y int) int64 {
	return isoEpochDays(int64(y), 1, 19) + int64(c.year(y)>>17&0x3f)
}

func (c eastAsian) yearOf(d int64) int {
	iy, _, _ := isoDate(d)
	y := int(iy)
	if d < c.newYear(y) {
		y--
	}
	return y
}

func (c eastAsian) monthsInYear(y int) int {
	return c.year(y).monthsInYear()
}

func (c eastAsian) daysInMonth(y, m int) int {
	return c.year(y).daysInMonth(m)
}

func (c eastAsian) monthCode(y, m int) (int, bool) {
	l := c.year(y).leapMonth()
	switch {
	case l == 0 || m < l:
		return m, false
	case m == l:
		return m - 1, true
	}
	return m - 1, false
}

func (c eastAsian) ordinal(y, n int, leap bool) (int, bool) {
	if n < 1 || n > 12 {
		return 0, false
	}
	l := c.year(y).leapMonth()
	if leap {
		if l == 0 || n != l-1 {
			return 0, false
		}
		return l, true
	}
	if l != 0 && n >= l {
		return n + 1, true
	}
	return n, true
}

// meanLunationNanodays is the mean synodic month in nanodays.
const meanLunationNanodays = 29530588853

func (c eastAsian) monthsBefore(y int) int64 {
	// Every month starts at a new moon, which is within two days of the mean lunation.
	diff := c.newYear(y) - c.newYear(0)
	return floorDiv(diff*1e9+meanLunationNanodays/2, meanLunationNanodays)
}
