// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const calendarJava = "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/calendar/"

func runCalendarArithmetic(t *testing.T, root string, failures map[string]map[int]string) {
	run := func(file string, check func(*formatFixture, intervalSource)) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			path := filepath.Join(root, file)
			f := newFormatFixture(t, path)
			f.trackOperations(failures)
			check(f, readIntervalSource(t, path))
		})
	}
	run(calendarJava+"OrdinalMonthTest.java", checkOrdinalMonths)
	run(calendarJava+"InTemporalLeapYearTest.java", checkCalendarLeapYears)
	run(calendarJava+"TemporalMonthCodeTest.java", checkTemporalMonthCodes)
	run(calendarJava+"HebrewTest.java", checkHebrewAdd)
	run(calendarJava+"DangiTest.java", checkDangiAdd)
	for _, name := range []string{"Coptic", "Ethiopic"} {
		run(calendarJava+name+"Test.java", func(f *formatFixture, src intervalSource) {
			checkThirteenthMonthAdd(f, src, strings.ToLower(name))
			checkAlexandrianEra(f, src, strings.ToLower(name))
		})
	}
	run(calendarJava+"IndianTest.java", checkIndianYearEdge)
	run(calendarJava+"CalendarRegressionTest.java", checkGregorianYearAdd)
	run("icu4c/source/test/intltest/incaltst.cpp", checkJapaneseEraFields)
	run("icu4c/source/test/cintltst/ccaltst.c", checkEraYearAdd)
	run("icu4c/source/test/intltest/caltest.cpp", checkChineseSpecialYears)
	t.Run("era-boundaries", func(t *testing.T) {
		path := filepath.Join(root, icuData, "misc/supplementalData.txt")
		f := newFormatFixture(t, path)
		f.trackOperations(failures)
		checkCalendarEras(f, readResource(t, path).child("calendarData"))
	})
}

func calendarDateBag(calendar string, year int, code string, day int) map[string]any {
	return map[string]any{"calendar": calendar, "year": year, "monthCode": code, "day": day}
}

const calendarAdd = `(bag, unit, amount) => {
	const start = Temporal.PlainDate.from(bag, {overflow: "reject"});
	const d = start.add({[unit]: amount}, {overflow: "constrain"});
	return [d.year, d.monthCode, d.day].join(" ");
}`

func calendarAddUnit(field string) string {
	switch strings.TrimPrefix(field, "Calendar.") {
	case "MONTH":
		return "months"
	case "DATE":
		return "days"
	case "YEAR":
		return "years"
	default:
		panic("unexpected ICU add field " + field)
	}
}

// ICU reserves a month number for Adar I even in a common year.
var hebrewMonths = []string{"TISHRI", "HESHVAN", "KISLEV", "TEVET", "SHEVAT", "ADAR_1", "ADAR", "NISAN", "IYAR", "SIVAN", "TAMUZ", "AV", "ELUL"}

func hebrewMonthCode(index int) string {
	if index == 5 {
		return "M05L"
	}
	if index < 5 {
		return monthCode(index+1, 0)
	}
	return monthCode(index, 0)
}

func checkHebrewAdd(f *formatFixture, src intervalSource) {
	add := f.function(calendarAdd)
	bag := func(cols []string) map[string]any {
		index := slices.Index(hebrewMonths, cols[1])
		if index < 0 {
			f.t.Fatalf("unknown Hebrew month %q", cols[1])
		}
		return calendarDateBag("hebrew", integer(cols[0]), hebrewMonthCode(index), integer(cols[2]))
	}
	for _, row := range src.from("void TestAdd()").table("new int[][] {") {
		c := row.columns(f.t, 8)
		f.check(row.line, "TestAdd PlainDate.add", add, dateResult(bag(c[5:])), bag(c), calendarAddUnit(c[3]), integer(c[4]))
	}
	for _, row := range src.from("void TestMonthMovement()").table("int[] DATA =") {
		c := row.columns(f.t, 7)
		// clear() defaults the day to 1. Both month columns are one-based ICU MONTH.
		start := calendarDateBag("hebrew", integer(c[1]), hebrewMonthCode(integer(c[0])-1), 1)
		end := calendarDateBag("hebrew", integer(c[4]), hebrewMonthCode(integer(c[3])-1), 1)
		f.check(row.line, "TestMonthMovement PlainDate.add", add, dateResult(end), start, "months", integer(c[2]))
		f.excluded["lenient set(MONTH) balances the raw field"]++
	}
	f.excluded["roll wraps within the current field range"] += len(src.from("void TestRoll()").table("new int[][] {"))
}

func checkDangiAdd(f *formatFixture, src intervalSource) {
	add := f.function(calendarAdd)
	bag := func(c []string) map[string]any {
		return calendarDateBag("dangi", integer(c[0]), monthCode(integer(c[1]), integer(c[2])), integer(c[3]))
	}
	for _, row := range src.from("void TestAdd()").table("new int[][] {") {
		c := row.columns(f.t, 10)
		f.check(row.line, "TestAdd PlainDate.add", add, dateResult(bag(c[6:])), bag(c), calendarAddUnit(c[4]), integer(c[5]))
	}
	f.excluded["roll wraps within the current field range"] += len(src.from("void TestRoll()").table("new int[][] {"))
}

func checkThirteenthMonthAdd(f *formatFixture, src intervalSource, calendar string) {
	add := f.function(calendarAdd)
	bag := func(c []string) map[string]any {
		return calendarDateBag(calendar, integer(c[0]), monthCode(integer(c[1])+1, 0), integer(c[2]))
	}
	for _, row := range src.table("final TestAddSetItem[] tests =") {
		args, ok := strings.CutPrefix(row.text, "new TestAddSetItem(")
		if !ok || !strings.HasSuffix(args, "),") {
			f.t.Fatalf("%s:%d: invalid add case %q", f.path, row.line, row.text)
		}
		row.text = strings.TrimSuffix(args, "),")
		c := row.columns(f.t, 8)
		f.check(row.line, "TestAddSet PlainDate.add", add, dateResult(bag(c[5:])), bag(c), calendarAddUnit(c[3]), integer(c[4]))
	}
}

func checkIndianYearEdge(f *formatFixture, src intervalSource) {
	block := src.from("void TestYearEdge()")
	add := f.function(`(iso, calendar) => Temporal.PlainDate.from(iso).withCalendar(calendar)
		.add({months: 1}).withCalendar("iso8601").toString()`)
	var start string
	for i, line := range block.lines {
		if line == "@Test" {
			break
		}
		if !strings.HasPrefix(line, "if (!fmt.format(cal.getTime()).equals(") {
			continue
		}
		date := intervalString(f.t, line)
		if start != "" {
			if block.lines[i-1] != "cal.add(Calendar.MONTH, 1);" {
				f.t.Fatalf("%s:%d: expected month addition before %s", f.path, block.line+i, date)
			}
			f.check(block.line+i, "TestYearEdge PlainDate.add({months: 1})", add, date, start, "indian")
		}
		start = date
	}
}

func fixtureMonth(t *testing.T, field string) int {
	t.Helper()
	field = strings.TrimPrefix(strings.TrimPrefix(field, "Calendar."), "UCAL_")
	date, err := time.Parse("January", field)
	if err != nil {
		t.Fatal(err)
	}
	return int(date.Month())
}

func checkChineseSpecialYears(f *formatFixture, src intervalSource) {
	fn := f.function(`(iso, calendar) => {
		const d = Temporal.PlainDate.from(iso).withCalendar(calendar);
		return [d.monthCode, d.day].join(" ");
	}`)
	for _, row := range src.from("void CalendarTest::TestChineseCalendarMonthInSpecialYear()").table("} cases[] = {") {
		c := row.columns(f.t, 6)
		iso := fmt.Sprintf("%04d-%02d-%02d", integer(c[0]), fixtureMonth(f.t, c[1]), integer(c[2]))
		month, ok := strings.CutSuffix(c[3], "-1")
		if !ok {
			f.t.Fatalf("%s:%d: expected zero-based month expression, got %s", f.path, row.line, c[3])
		}
		var leap int
		if c[5] == "true" {
			leap = 1
		}
		f.check(row.line, "TestChineseCalendarMonthInSpecialYear", fn, monthCode(integer(month), leap)+" "+c[4], iso, "chinese")
	}
}

func checkGregorianYearAdd(f *formatFixture, src intervalSource) {
	block := src.from("void TestT8057()")
	start := block.from("cal.set(")
	var y, d int
	var m string
	if _, err := fmt.Sscanf(strings.ReplaceAll(start.lines[0], ",", " "), "cal.set(%d %s %d);", &y, &m, &d); err != nil {
		f.t.Fatal(err)
	}
	bag := calendarDateBag("gregory", y, monthCode(fixtureMonth(f.t, m), 0), d)
	expected := block.from("long expected = ")
	millis := strings.TrimSuffix(strings.TrimPrefix(expected.lines[0], "long expected = "), "L;")
	op := block.from("cal.add(Calendar.YEAR, 1);")
	fn := f.function(`(bag, timeZone) => Temporal.ZonedDateTime.from({...bag, timeZone}, {overflow: "reject"})
		.add({years: 1}).epochMilliseconds.toString()`)
	f.check(op.line, "TestT8057 ZonedDateTime.add({years: 1})", fn, millis, bag, "America/Los_Angeles")
}
