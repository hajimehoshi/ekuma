// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma"
)

// ICU's Buddhist and ROC calendars use a Gregorian EXTENDED_YEAR.
func temporalExtendedYear(calendar string, year int) int {
	switch calendar {
	case "buddhist":
		return year + 543
	case "roc":
		return year - 1911
	default:
		return year
	}
}

func temporalCalendarID(name string) string {
	switch name {
	case "gregorian":
		return "gregory"
	case "ethiopic-amete-alem":
		return "ethioaa"
	default:
		return name
	}
}

var ordinalYear = regexp.MustCompile(`cc1\.set\(Calendar\.EXTENDED_YEAR, (\w+)\);`)
var ordinalDay = regexp.MustCompile(`cc1\.set\(Calendar\.DATE, (\d+)\);`)
var ordinalVerify = regexp.MustCompile(`, cc1, [\w.]+, (\d+), (?:true|false), "(M\d\dL?)"\);$`)
var ordinalChineseYears = regexp.MustCompile(`RunTestChineseCalendarSet\("(\w+)", (\d+), (\d+)\);`)

func checkOrdinalSet(f *formatFixture, fn ekuma.Value, block intervalSource, calendar string, years map[string]int) {
	var year, day, checks int
	for i, line := range block.lines[1:] {
		if line == "@Test" || strings.HasPrefix(line, "private void ") {
			break
		}
		if m := ordinalYear.FindStringSubmatch(line); m != nil {
			var ok bool
			year, ok = years[m[1]]
			if !ok {
				year = integer(m[1])
			}
			year = temporalExtendedYear(calendar, year)
		}
		if m := ordinalDay.FindStringSubmatch(line); m != nil {
			day = integer(m[1])
		}
		if m := ordinalVerify.FindStringSubmatch(line); m != nil {
			// The final Chinese assertion has a stale year in its message.
			// Use the EXTENDED_YEAR last set on the calendar.
			ordinal := integer(m[1]) + 1
			bag := calendarDateBag(calendar, year, m[2], day)
			want := fmt.Sprintf("%s %d", dateResult(bag), ordinal)
			f.check(block.line+i+1, "VerifyMonth from monthCode", fn, want, bag)
			delete(bag, "monthCode")
			bag["month"] = ordinal
			f.check(block.line+i+1, "VerifyMonth from month", fn, want, bag)
			checks++
		}
	}
	invalid := block.from("String[] invalidMonthCodes =")
	for i, line := range invalid.lines {
		for _, code := range intervalLiteral.FindAllString(line, -1) {
			bag := calendarDateBag(calendar, year, intervalString(f.t, code), day)
			f.check(invalid.line+i, "from invalid monthCode (reject)", fn, "RangeError", bag)
		}
		if strings.Contains(line, "};") {
			break
		}
	}
	if strings.Contains(block.lines[0], "TestMostCalendarsSet") {
		// On day 3 the epagomenal month exists in both common and leap years.
		bag := calendarDateBag(calendar, year, "M13", day)
		want := "RangeError"
		switch calendar {
		case "coptic", "ethiopic", "ethioaa":
			want = dateResult(bag) + " 13"
		}
		marker := `assertSetTemporalMonthCodeThrowIllegalArgumentException(cc1, "M13")`
		if want != "RangeError" {
			marker = `cc1.setTemporalMonthCode("M13")`
		}
		f.check(block.from(marker).line, "from M13 (reject)", fn, want, bag)
	}
	if checks == 0 {
		f.t.Fatalf("%s:%d: no VerifyMonth cases for %s", f.path, block.line, calendar)
	}
}

func checkOrdinalMonths(f *formatFixture, src intervalSource) {
	fields := f.function(`bag => {
		try {
			const d = Temporal.PlainDate.from(bag, {overflow: "reject"});
			return [d.year, d.monthCode, d.day, d.month].join(" ");
		} catch (e) {
			if (e instanceof RangeError) return "RangeError";
			throw e;
		}
	}`)
	most := src.from("void TestMostCalendarsSet()")
	for _, row := range src.from("void TestLimits()").table("Object[][] cases =") {
		name := intervalString(f.t, row.columns(f.t, 5)[0])
		f.excluded["calendar-wide field limits have no Temporal property"]++
		switch name {
		case "chinese", "dangi", "hebrew":
			continue
		}
		if !calendarAvailable(name) {
			f.excluded["observational Islamic calendar has no matching Temporal calendar"]++
			continue
		}
		checkOrdinalSet(f, fields, most, temporalCalendarID(name), nil)
	}
	for _, name := range []string{"Chinese", "Dangi"} {
		call := src.from("void Test" + name + "CalendarSet()").from("RunTestChineseCalendarSet(")
		m := ordinalChineseYears.FindStringSubmatch(call.lines[0])
		if m == nil {
			f.t.Fatalf("%s:%d: invalid year arguments %q", f.path, call.line, call.lines[0])
		}
		checkOrdinalSet(f, fields, src.from("void RunTestChineseCalendarSet("), m[1], map[string]int{
			"notLeapYear": integer(m[2]), "leapMarchYear": integer(m[3]),
		})
	}
	hebrew := src.from("void TestHebrewCalendarSet()")
	years := map[string]int{}
	for _, name := range []string{"leapYear", "notLeapYear"} {
		line := hebrew.from("int " + name + " = ").lines[0]
		_, value, _ := strings.Cut(line, " = ")
		years[name] = integer(strings.TrimSuffix(value, ";"))
	}
	checkOrdinalSet(f, fields, hebrew, "hebrew", years)

	limits := f.function(`bag => Temporal.PlainDate.from(bag, {overflow: "reject"}).monthsInYear.toString()`)
	for _, row := range src.from("void TestActaulLimits()").table("Object[][] cases =") {
		c := row.columns(f.t, 4)
		name := intervalString(f.t, c[0])
		if !calendarAvailable(name) {
			f.excluded["observational Islamic calendar has no matching Temporal calendar"]++
			continue
		}
		calendar := temporalCalendarID(name)
		var extendedYear int
		for term := range strings.SplitSeq(c[1], " + ") {
			extendedYear += integer(term)
		}
		bag := calendarDateBag(calendar, temporalExtendedYear(calendar, extendedYear), "M01", 1)
		f.check(row.line, "monthsInYear", limits, strconv.Itoa(integer(c[3])-integer(c[2])+1), bag)
	}
	f.excluded["TestAdd compares two ICU add fields without an expected date"]++
	f.excluded["TestRoll wraps one field without carrying"]++
}

func checkTemporalMonthCodes(f *formatFixture, src intervalSource) {
	fromISO := f.function(`(iso, calendar) => {
		const d = Temporal.PlainDate.from(iso).withCalendar(calendar);
		return [d.year, d.monthCode, d.day, d.month].join(" ");
	}`)
	toISO := f.function(`bag => Temporal.PlainDate.from(bag, {overflow: "reject"}).withCalendar("iso8601").toString()`)
	// ICU's GregorianCalendar uses a Julian cutover. This table compares
	// field setters only, including for years before the cutover.
	gregorian := src.from("void TestGregorianCalendarSetTemporalMonthCode()")
	fields := f.function(`bag => {
		const d = Temporal.PlainDate.from(bag, {overflow: "reject"});
		return [d.year, d.monthCode, d.day, d.month].join(" ");
	}`)
	for _, row := range gregorian.table("Object[][] cases =") {
		c := row.columns(f.t, 5)
		bag := calendarDateBag("gregory", integer(c[0]), intervalString(f.t, c[3]), integer(c[2]))
		want := fmt.Sprintf("%s %d", dateResult(bag), integer(c[4])+1)
		f.check(row.line, "TestGregorian from monthCode", fields, want, bag)
		delete(bag, "monthCode")
		bag["month"] = fixtureMonth(f.t, c[1])
		f.check(row.line, "TestGregorian from month", fields, want, bag)
	}
	for _, name := range []string{"Chinese", "Hebrew", "Coptic", "Ethiopic"} {
		calendar := strings.ToLower(name)
		method := "Test" + name + "CalendarSetTemporalMonthCode"
		count := 8
		if calendar == "chinese" {
			count = 9
		}
		for _, row := range src.from("void " + method + "()").table("Object[][] cases =") {
			c := row.columns(f.t, count)
			iso := fmt.Sprintf("%04d-%02d-%02d", integer(c[0]), fixtureMonth(f.t, c[1]), integer(c[2]))
			bag := calendarDateBag(calendar, integer(c[3]), intervalString(f.t, c[6]), integer(c[5]))
			ordinal := integer(c[count-1]) + 1
			f.check(row.line, method+" month and monthCode", fromISO, fmt.Sprintf("%s %d", dateResult(bag), ordinal), iso, calendar)
			f.check(row.line, method+" from monthCode", toISO, iso, bag)
		}
	}
}
