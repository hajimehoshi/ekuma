// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func intervalMillis(t *testing.T, value string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(value), ".0"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func intervalDelta(t *testing.T, expr string) int64 {
	t.Helper()
	value, unit, _ := strings.Cut(expr, "*")
	var scale int64
	switch unit {
	case "_HOUR":
		scale = 3600000
	case "_DAY":
		scale = 86400000
	default:
		t.Fatalf("unknown interval delta %q", expr)
	}
	return intervalMillis(t, value) * scale
}

func intervalDates(src intervalSource) map[string]int64 {
	dates := map[string]int64{}
	for _, row := range src.lines {
		if strings.HasPrefix(row, "#define Date") {
			col := strings.Fields(row)
			dates[col[1]] = intervalMillis(src.t, col[2])
		}
	}
	return dates
}

func (s *intervalSuite) checkZonedIntervals(src intervalSource, dates map[string]int64) {
	t := src.t
	zones := map[string]string{}
	for _, row := range src.lines {
		if strings.HasPrefix(row, "static const char tz") {
			name := strings.Fields(row)[3]
			zones[strings.TrimSuffix(name, "[]")] = intervalString(t, row)
		}
	}
	for _, row := range src.table("DateIntervalFormatTestItem testItems[]") {
		col := row.columns(t, 7)
		if col[0] == "NULL" {
			continue
		}
		if col[2] != "CAP_NONE" {
			s.excluded["capitalization context "+col[2]]++
			continue
		}
		from, delta, ok := strings.Cut(col[5], " + ")
		start, haveStart := dates[col[4]]
		end, haveEnd := dates[from]
		if !ok || !haveStart || !haveEnd || zones[col[3]] == "" {
			t.Fatalf("%s:%d: unexpected interval or zone: %s", src.path, row.line, row.text)
		}
		in := intervalInput{
			locale:   intervalString(t, col[0]),
			skeleton: intervalString(t, col[1]),
			zone:     zones[col[3]],
			start:    start,
			end:      end + intervalDelta(t, delta),
		}
		s.checkInterval(row.line, "TestDateIntervalFormat", "formatRange", in, intervalString(t, col[6]))
	}
}

// icuWallTime applies ICU Calendar's forward adjustment in a DST gap.
func icuWallTime(zone *time.Location, y, m, d, h, min, sec, ms int) int64 {
	wall := time.Date(y, time.Month(m), d, h, min, sec, ms*1000000, time.UTC)
	local := time.Date(y, time.Month(m), d, h, min, sec, ms*1000000, zone)

	_, offset := local.Zone()
	if diff := wall.Sub(local.Add(time.Duration(offset) * time.Second)); diff > 0 {
		local = local.Add(diff)
	}
	return local.UnixMilli()
}

func (s *intervalSuite) eraDate(input string, buddhist bool) int64 {
	col := strings.Fields(input)
	var era string
	if len(col) == 5 {
		era, col = col[0], col[1:]
	}
	if len(col) != 4 {
		s.t.Fatalf("invalid interval endpoint %q", input)
	}
	y := integer(col[0])
	switch {
	case buddhist:
		y -= 543
	case era == "CE":
	case era == "S":
		y += 1925
	case era == "H":
		y += 1988
	case era == "R":
		y += 2018
	default:
		s.t.Fatalf("unknown interval era in %q", input)
	}
	hms := strings.Split(col[3], ":")
	return icuWallTime(s.pacific, y, integer(col[1]), integer(col[2]), integer(hms[0]), integer(hms[1]), integer(hms[2]), 0)
}

func (s *intervalSuite) checkSkeletonIntervals(src, defs intervalSource) {
	t := s.t
	eras := strings.NewReplacer(
		"JP_ERA_2019_NARROW", strings.Fields(defs.from("#define JP_ERA_2019_NARROW").lines[0])[2],
		"JP_ERA_2019_ROOT", strings.Fields(defs.from("#define JP_ERA_2019_ROOT").lines[0])[2],
	)
	for _, group := range []string{"testFormat", "testHourMetacharacters"} {
		rows := src.from("::" + group + "() {").table("char* DATA[]")
		if intervalString(t, rows[0].text) != "GGGGG y MM dd HH:mm:ss" {
			t.Fatalf("%s:%d: unknown date pattern", src.path, rows[0].line)
		}
		for _, row := range rows[1:] {
			row.text = eras.Replace(row.text)
			col := row.columns(t, 5)
			locale := strings.ReplaceAll(intervalString(t, col[0]), "_", "-")
			in := intervalInput{
				locale:   locale,
				skeleton: intervalString(t, col[3]),
				zone:     intervalPacific,
				start:    s.eraDate(intervalString(t, col[1]), locale == "th"),
				end:      s.eraDate(intervalString(t, col[2]), locale == "th"),
			}
			s.checkInterval(row.line, group, "formatRange", in, intervalString(t, col[4]))
		}
	}
}

func (s *intervalSuite) checkMilliseconds(src intervalSource) {
	t := s.t
	// Calendar::set takes a zero-based month.
	for _, row := range src.from("::testFormatMillisecond() {").table("kTestCases [] =") {
		col := row.columns(t, 13)
		if col[11] == "nullptr" {
			continue
		}
		y, m, d := integer(col[0]), integer(col[1])+1, integer(col[2])
		endpoint := func(fields []string) int64 {
			return icuWallTime(s.pacific, y, m, d, integer(fields[0]), integer(fields[1]), integer(fields[2]), integer(fields[3]))
		}
		in := intervalInput{
			locale:   "en",
			skeleton: intervalString(t, col[11]),
			zone:     intervalPacific,
			start:    endpoint(col[3:7]),
			end:      endpoint(col[7:11]),
		}
		s.checkInterval(row.line, "testFormatMillisecond", "formatRange", in, intervalString(t, col[12]))
	}
}

func (s *intervalSuite) checkHourCycles(src intervalSource) {
	t := s.t
	block := src.from("::testTicket20707() {")
	locales := intervalLiteral.FindAllString(block.from("Locale locales[]").lines[0], -1)
	skeletons := intervalLiteral.FindAllString(block.from("for (const UnicodeString skeleton :").lines[0], -1)
	rows := block.table("UnicodeString expected[][7]")
	if len(locales) != len(rows) {
		t.Fatalf("%s: locale/expectation length mismatch", src.path)
	}
	for i, row := range rows {
		col := row.columns(t, len(skeletons))
		for j, literal := range skeletons {
			skel := intervalString(t, literal)
			in := intervalInput{
				locale:   intervalString(t, locales[i]),
				skeleton: skel,
				zone:     "UTC",
				start:    1563235200000,
				end:      1563235200000,
			}
			s.checkInterval(row.line, "testTicket20707", "formatRange/"+skel, in, intervalString(t, col[j]))
		}
	}
}

func (s *intervalSuite) checkCalendarIntervals(src intervalSource) {
	in := intervalInput{
		locale:   "es-US",
		skeleton: "yMMM",
		zone:     intervalPacific,
		start:    1232364615000,
		end:      1328787015000,
	}
	row := src.from("::testTicket11583_2() {").from("UnicodeString expected(")
	s.checkInterval(row.line, "testTicket11583_2", "formatRange", in, intervalString(s.t, row.lines[0]))

	// The era tickets construct Gregorian dates at 06:00 GMT, then format them
	// in the requested calendar. Each range names its assertion in the fixture.
	read := func(group, locale, label, start, end string) {
		row := src.from("::" + group + "() {").from(label)
		atSix := func(date string) int64 {
			instant, err := time.Parse(time.DateOnly, date)
			if err != nil {
				s.t.Fatal(err)
			}
			return instant.Add(6 * time.Hour).UnixMilli()
		}
		in := intervalInput{
			locale:   locale,
			skeleton: "h",
			zone:     "GMT",
			start:    atSix(start),
			end:      atSix(end),
		}
		s.checkInterval(row.line+1, group, "formatRange", in, intervalString(s.t, row.lines[1]))
	}
	const roc = "testTicket21222ROCEraDiff"
	const tw = "zh-Hant-TW-u-ca-roc"
	read(roc, tw, "both dates in MG Era", "1912-01-02", "1913-01-02")
	read(roc, tw, "prior MG Era and in MG Era", "1911-01-02", "1913-01-02")
	read(roc, tw, "both dates prior MG Era", "1910-01-02", "1911-01-02")
	const japanese = "testTicket21222JapaneseEraDiff"
	const ja = "ja-u-ca-japanese"
	read(japanese, ja, "both dates in Reiwa", "2019-05-04", "2019-06-05")
	read(japanese, ja, "both dates before Reiwa", "2019-03-02", "2019-04-03")
	read(japanese, ja, "date before and in Reiwa", "2019-03-02", "2019-05-04")
}

func (s *intervalSuite) countIntervalExclusions(src intervalSource) {
	t := s.t
	rows := src.from("::testFormatUserDII() {").table("char* DATA[]")
	s.excluded["custom DateIntervalInfo patterns"] += len(rows) - 1
	for _, row := range src.from("::testContext() {").table("DateIntervalContextItem testItems[]") {
		col := row.columns(t, 5)
		switch {
		case col[0] == "nullptr":
		case col[2] == "CAP_NONE":
			s.excluded["duplicates the C capitalization table"]++
		default:
			s.excluded["capitalization context "+col[2]]++
		}
	}
	identity := src.from("::testTicket20710_IntervalIdentity() {")
	quickLocales := len(intervalLiteral.FindAllString(identity.from("Locale quickLocales[]").lines[1], -1))
	s.excluded["quick identity matrix uses runtime-generated reference text"] += len(identity.table("skeletons[]")) * quickLocales

	s.excluded["Julian dates before the Gregorian cutover (year formats and Gregorian era ticket)"] += 6
	s.excluded["stress date pairs without expected output"] += len(src.from("::testStress() {").table("char* DATA[]")) - 1
	s.excluded["time-zone examples duplicated by the C fixture"] += 2
}
