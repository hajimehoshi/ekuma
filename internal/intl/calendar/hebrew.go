// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package calendar

// hebrewEpoch is the first day of year 1 of the Hebrew calendar before postponements, 3761 BCE
// October 7 in the Julian calendar.
const hebrewEpoch = -2092590

// hebrew is the Hebrew calendar with Tishrei as its first month. A leap year inserts Adar I
// (M05L) after Shevat (M05).
type hebrew struct{}

func hebrewLeapYear(y int) bool {
	return floorMod(7*int64(y)+1, 19) < 7
}

// hebrewElapsedDays returns the days from the epoch to the molad of Tishrei of year y,
// postponed when the molad falls on a Sunday, Wednesday or Friday.
func hebrewElapsedDays(y int) int64 {
	months := floorDiv(235*int64(y)-234, 19)
	parts := 12084 + 13753*months
	days := 29*months + floorDiv(parts, 25920)
	if floorMod(3*(days+1), 7) < 3 {
		days++
	}
	return days
}

func (hebrew) newYear(y int) int64 {
	ny0 := hebrewElapsedDays(y - 1)
	ny1 := hebrewElapsedDays(y)
	ny2 := hebrewElapsedDays(y + 1)
	var correction int64
	switch {
	case ny2-ny1 == 356:
		correction = 2
	case ny1-ny0 == 382:
		correction = 1
	}
	return hebrewEpoch + ny1 + correction
}

func (c hebrew) yearOf(d int64) int {
	y := int(floorDiv((d-hebrewEpoch)*98496, 35975351)) + 1
	for d < c.newYear(y) {
		y--
	}
	for d >= c.newYear(y+1) {
		y++
	}
	return y
}

func (hebrew) monthsInYear(y int) int {
	if hebrewLeapYear(y) {
		return 13
	}
	return 12
}

func (c hebrew) daysInMonth(y, m int) int {
	n, leap := c.monthCode(y, m)
	switch {
	case leap:
		return 30
	case n == 2:
		// Heshvan is long in a complete year.
		if (c.newYear(y+1)-c.newYear(y))%10 == 5 {
			return 30
		}
		return 29
	case n == 3:
		// Kislev is short in a deficient year.
		if (c.newYear(y+1)-c.newYear(y))%10 == 3 {
			return 29
		}
		return 30
	}
	// The other months alternate between 30 and 29 days from Tishrei on.
	if n%2 == 1 {
		return 30
	}
	return 29
}

func (hebrew) monthCode(y, m int) (int, bool) {
	if !hebrewLeapYear(y) || m <= 5 {
		return m, false
	}
	if m == 6 {
		return 5, true
	}
	return m - 1, false
}

func (hebrew) ordinal(y, n int, leap bool) (int, bool) {
	if n < 1 || n > 12 {
		return 0, false
	}
	if leap {
		if n != 5 || !hebrewLeapYear(y) {
			return 0, false
		}
		return 6, true
	}
	if hebrewLeapYear(y) && n >= 6 {
		return n + 1, true
	}
	return n, true
}

func (hebrew) monthsBefore(y int) int64 {
	// The Metonic cycle has 235 months in 19 years.
	return floorDiv(235*int64(y)-234, 19)
}
