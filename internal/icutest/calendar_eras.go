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

func eraStart(f *formatFixture, eras resource, id string) (time.Time, int) {
	r := eras.child(id).child("start:intvector")
	if len(r.values) != 3 {
		f.t.Fatalf("%s:%d: missing era %s start in %s", f.path, eras.line, id, eras.name)
	}
	return time.Date(integer(r.values[0]), time.Month(integer(r.values[1])), integer(r.values[2]), 0, 0, 0, 0, time.UTC), r.line
}

func checkCalendarEras(f *formatFixture, calendars resource) {
	step := f.function(`(calendar, iso, days) => {
		const d = Temporal.PlainDate.from(iso).withCalendar(calendar).add({days});
		return [d.withCalendar("iso8601").toString(), d.era, d.eraYear].join(" ");
	}`)
	check := func(calendar string, line int, start time.Time, beforeEra string, beforeYear int, afterEra string) {
		before := start.AddDate(0, 0, -1).Format(time.DateOnly)
		after := start.Format(time.DateOnly)
		f.check(line, "add/next-era", step, after+" "+afterEra+" 1", calendar, before, 1)
		f.check(line, "add/previous-era", step, fmt.Sprintf("%s %s %d", before, beforeEra, beforeYear), calendar, after, -1)
	}

	paired := func(name, eraID, before, after string) {
		start, line := eraStart(f, calendars.child(name).child("eras"), eraID)
		check(temporalCalendarID(name), line, start, before, 1, after)
	}
	paired("gregorian", "1", "bce", "ce")
	paired("roc", "1", "broc", "roc")
	for _, name := range []string{"islamic-civil", "islamic-tbla", "islamic-umalqura"} {
		paired(name, "0", "bh", "ah")
	}
	// Japanese inherits BCE and CE. Later era years change on January 1.
	japanese := calendars.child("japanese")
	if resourceValue(f, japanese.child("inheritEras")) != "gregorian" {
		f.t.Fatalf("%s:%d: unexpected Japanese era inheritance", f.path, japanese.line)
	}
	start, line := eraStart(f, calendars.child("gregorian").child("eras"), "1")
	check("japanese", line, start, "bce", 1, "ce")
	previousEra, firstYear := "ce", 1
	for i, name := range []string{"meiji", "taisho", "showa", "heisei", "reiwa"} {
		start, line := eraStart(f, japanese.child("eras"), strconv.Itoa(232+i))
		check("japanese", line, start, previousEra, start.AddDate(0, 0, -1).Year()-firstYear+1, name)
		previousEra, firstYear = name, start.Year()
	}
}

// The two rows at the era boundary supply Julian dates at midnight and
// one-based calendar fields. ICU counts Coptic years before AM backwards.
func checkAlexandrianEra(f *formatFixture, src intervalSource, calendar string) {
	fn := f.function(`(calendar, start, days) => {
		const d = Temporal.PlainDate.from(start).withCalendar(calendar).add({days});
		return [d.withCalendar("iso8601").toString(), d.era, d.eraYear, d.monthCode, d.day].join(" ");
	}`)
	lastYear := 1
	if calendar == "ethiopic" {
		lastYear = 5500
	}
	type endpoint struct {
		date time.Time
		want string
		line int
	}
	var before, after endpoint
	for _, row := range src.from("void TestCases()").table("final TestCase[] tests =") {
		m := eraTestCase.FindStringSubmatch(row.text)
		if m == nil {
			continue
		}
		era, year, month, day := integer(m[2]), integer(m[3]), integer(m[4]), integer(m[5])
		eraName := "am"
		var dest *endpoint
		switch {
		case era == 0 && year == lastYear && month == 13 && day == 5:
			dest = &before
			if calendar == "coptic" {
				year = 1 - year
			} else {
				eraName = "aa"
			}
		case era == 1 && year == 1 && month == 1 && day == 1:
			dest = &after
		default:
			continue
		}
		date := time.Unix(int64(integer(m[1])-2440587)*86400, 0).UTC()
		*dest = endpoint{
			date: date,
			want: fmt.Sprintf("%s %s %d %s %d", date.Format(time.DateOnly), eraName, year, monthCode(month, 0), day),
			line: row.line,
		}
	}
	if after.date.Sub(before.date) != 24*time.Hour || before.line == 0 || after.line == 0 {
		f.t.Fatalf("%s: missing adjacent era boundary dates", f.path)
	}
	f.check(after.line, "TestCases add one day", fn, after.want, calendar, before.date.Format(time.DateOnly), 1)
	f.check(before.line, "TestCases subtract one day", fn, before.want, calendar, after.date.Format(time.DateOnly), -1)
}

var eraTestCase = regexp.MustCompile(`^new TestCase\((\d+)\.5, (\d+), (\d+), (\d+), (\d+),`)

func checkJapaneseEraFields(f *formatFixture, src intervalSource) {
	toISO := f.function(`bag => Temporal.PlainDate.from(bag, {overflow: "reject"}).withCalendar("iso8601").toString()`)
	fromISO := f.function(`(iso, calendar) => {
		const d = Temporal.PlainDate.from(iso).withCalendar(calendar);
		return [d.era, d.eraYear].join(" ");
	}`)
	for _, row := range src.from("void IntlCalendarTest::TestJapanese()").table("int32_t data[] =") {
		if strings.HasPrefix(row.text, "-1,") {
			break
		}
		c := row.columns(f.t, 5)
		era, ok := strings.CutPrefix(c[0], "JapaneseCalendar_")
		if !ok {
			f.t.Fatalf("%s:%d: unexpected Japanese era %s", f.path, row.line, c[0])
		}
		month := fixtureMonth(f.t, c[3])
		bag := map[string]any{"calendar": "japanese", "era": strings.ToLower(era), "eraYear": integer(c[1]), "month": month, "day": integer(c[4])}
		iso := fmt.Sprintf("%04d-%02d-%02d", integer(c[2]), month, integer(c[4]))
		f.check(row.line, "TestJapanese from era/eraYear", toISO, iso, bag)
		f.check(row.line, "withCalendar/era", fromISO, strings.ToLower(era)+" "+c[1], iso, "japanese")
	}
}

func checkEraYearAdd(f *formatFixture, src intervalSource) {
	add := f.function(`bag => {
		const d = Temporal.PlainDate.from(bag, {overflow: "reject"}).add({years: 1});
		return [d.era, d.eraYear].join(" ");
	}`)
	// Names are in ICU era order. Temporal's single Coptic era has signed years.
	eras := map[string][]string{
		"gregory":  {"bce", "ce"},
		"roc":      {"broc", "roc"},
		"coptic":   {"am", "am"},
		"ethiopic": {"aa", "am"},
		"buddhist": {"be"},
		"hebrew":   {"am"},
		"indian":   {"shaka"},
		"persian":  {"ap"},
		"ethioaa":  {"aa"},
	}
	for _, row := range src.table("EraTestItem eraTestItems[]") {
		if strings.HasPrefix(row.text, "/*") || strings.Contains(row.text, "NULL") {
			continue
		}
		c := row.columns(f.t, 2)
		_, name := calendarLocale(intervalString(f.t, c[0]))
		cal := temporalCalendarID(name)
		switch cal {
		case "chinese":
			f.excluded["ICU's Chinese ERA is a sexagenary cycle"]++
			continue
		case "japanese":
			f.excluded["ccaltst.c skips the Japanese row for ICU-23108"]++
			continue
		case "islamic":
			f.excluded["observational Islamic calendar has no matching Temporal calendar"]++
			continue
		}
		if len(eras[cal]) == 0 {
			f.t.Fatalf("%s:%d: unknown era mapping for %s", f.path, row.line, cal)
		}
		for index, era := range eras[cal] {
			start, end := 2, 3
			if index == 0 && c[1] == "true" {
				end = 1
				if cal == "coptic" {
					start, end = 1-start, 1-end
				}
			}
			bag := map[string]any{"calendar": cal, "era": era, "eraYear": start, "month": 1, "day": 1}
			f.check(row.line, "TestAddRollEra0AndEraBounds add({years: 1})", add, fmt.Sprintf("%s %d", era, end), bag)
		}
	}
}
