// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var leapYearRange = regexp.MustCompile(`for \(int year = (\d+); year < (\d+); \+\+year\)`)
var leapTestCalendar = regexp.MustCompile(`setKeywordValue\("calendar", "([^"]+)"\)`)

func checkCalendarLeapYears(f *formatFixture, src intervalSource) {
	leap := f.function(`(iso, calendar) => Temporal.PlainDate.from(iso).withCalendar(calendar).inLeapYear.toString()`)
	block := src.from("void TestGregorian()").from("for (int year =")
	years := leapYearRange.FindStringSubmatch(block.lines[0])
	if years == nil || block.lines[1] != "gc.set(year, Calendar.MARCH, 7);" {
		f.t.Fatalf("%s:%d: unexpected Gregorian leap-year loop", f.path, block.line)
	}
	for year := integer(years[1]); year < integer(years[2]); year++ {
		isLeap := time.Date(year, time.March, 0, 0, 0, 0, 0, time.UTC).Day() == 29
		f.check(block.line, "TestGregorian inLeapYear", leap, strconv.FormatBool(isLeap), fmt.Sprintf("%04d-03-07", year), "gregory")
	}

	// The Java loops compute expectations with a second ICU Calendar. The
	// saved ICU 79.1 year lengths keep this check independent of Ekuma.
	sourceLines := map[string]int{}
	for i, line := range src.lines {
		if m := leapTestCalendar.FindStringSubmatch(line); m != nil {
			if !calendarAvailable(m[1]) {
				f.excluded["observational Islamic calendar has no matching Temporal calendar"]++
				continue
			}
			sourceLines[m[1]] = src.line + i
		}
	}
	const lengths = "internal/icutest/testdata/calendar-year-lengths.txt"
	var names []string
	date := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, method := range []string{"void RunChinese(", "void TestHebrew()", "void RunXDaysIsLeap("} {
		b := src.from(method).from("int startYear = 1900;").from("if (quick)")
		if b.lines[1] != "incrementDays = 317;" || b.lines[2] != "stopYear = 2100;" {
			f.t.Fatalf("%s:%d: year lengths do not match the ICU quick-mode schedule", f.path, b.line)
		}
	}
	fixtureLines(f.t, lengths, func(line int, row string) {
		cols := strings.Fields(row)
		if line == 1 {
			if len(cols) != len(sourceLines)+1 || cols[0] != "date" {
				f.t.Fatalf("%s: invalid calendar header", lengths)
			}
			names = cols[1:]
			seen := map[string]bool{}
			for _, name := range names {
				if sourceLines[name] == 0 || seen[name] {
					f.t.Fatalf("%s: unexpected calendar %s", lengths, name)
				}
				seen[name] = true
			}
			return
		}
		if len(cols) != len(names)+1 || date.Year() > 2100 || cols[0] != date.Format(time.DateOnly) {
			f.t.Fatalf("%s:%d: expected lengths for %s", lengths, line, date.Format(time.DateOnly))
		}
		for i, name := range names {
			common := 365
			switch name {
			case "chinese", "dangi", "hebrew":
				common = 12
			case "islamic-tbla", "islamic-civil", "islamic-umalqura":
				common = 354
			}
			n := integer(cols[i+1])
			if n != common && n != common+1 {
				f.t.Fatalf("%s:%d: unexpected %s year length %d", lengths, line, name, n)
			}
			f.check(sourceLines[name], "inLeapYear", leap, strconv.FormatBool(n == common+1), cols[0], temporalCalendarID(name))
		}
		date = date.AddDate(0, 0, 317)
	})
	if date.Year() <= 2100 {
		f.t.Fatalf("%s: missing lengths for %s", lengths, date.Format(time.DateOnly))
	}
}
