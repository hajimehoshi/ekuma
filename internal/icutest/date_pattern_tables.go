// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strings"
	"time"
)

func (s *datePatternSuite) strings(row intervalRow, count int) []string {
	values := listStrings(s.t, row.text)
	if len(values) != count {
		s.t.Fatalf("%s:%d: expected %d strings in %q", s.path, row.line, count, row.text)
	}
	return values
}

func (s *datePatternSuite) cppDatePatterns(src intervalSource) {
	s.fixedDateTables(src)
	for _, row := range src.table("DTPtnGenOptionsData testData[]") {
		v := s.strings(row, 3)
		c := row.columns(s.t, 4)
		switch c[3] {
		case "UDATPG_MATCH_NO_OPTIONS":
			s.pattern(row.line, v[0], v[1], v[2], localeHourWidth)
		case "UDATPG_MATCH_HOUR_FIELD_LENGTH":
			s.pattern(row.line, v[0], v[1], v[2], requestedHourWidth)
		default:
			s.t.Fatalf("%s:%d: unknown match option %s", s.path, row.line, c[3])
		}
	}
	for _, row := range src.table("tests[][3]") {
		v := s.strings(row, 3)
		s.pattern(row.line, v[0], v[1], v[2], requestedHourWidth)
	}
	for _, rows := range [][]intervalRow{
		src.from("::testBestPattern() {").table("} testCases[]"),
		src.table("DTPtnGenTwoDigitYearData testData[]"),
	} {
		for _, row := range rows {
			v := s.strings(row, 3)
			s.pattern(row.line, v[0], v[1], v[2], localeHourWidth)
		}
	}
	s.allDateFields(src)
	skel := listArray(src, "UnicodeString skeletons[kNumDateTimePatterns]")
	var loc string
	var i int
	for _, row := range src.table("DTPLocaleAndResults localeAndResults[]") {
		v := listStrings(s.t, row.text)
		if strings.HasPrefix(row.text, "{") && loc != "" && i != len(skel) {
			s.t.Fatalf("%s:%d: %s has %d combining patterns for %d skeletons", s.path, row.line, loc, i, len(skel))
		}
		if strings.Contains(row.text, "nullptr") {
			break
		}
		if len(v) == 2 {
			loc, i = v[0], 0
			v = v[1:]
		}
		if len(v) != 1 || i >= len(skel) {
			s.t.Fatalf("%s:%d: invalid combining-pattern row %s", s.path, row.line, row.text)
		}
		s.pattern(row.line, loc, skel[i], v[0], localeHourWidth)
		i++
	}

	for _, row := range src.from("::test20640_HourCyclArsEnNH() {").table("} cases[]") {
		v := s.strings(row, 3)
		s.pattern(row.line, v[0], "j", v[1], localeHourWidth)
	}
	for _, row := range src.from("::testRegionOverride() {").table("} testCases[]") {
		v := s.strings(row, 2)
		s.pattern(row.line, v[0], "jmm", v[1], localeHourWidth)
	}
	for _, row := range src.from("::testISO8601() {").table("localeIDs[]") {
		id := intervalString(s.t, row.text)
		loc, opts, _ := datePatternOptions(id, "jms", "UTC")
		s.check(row.line, "format/ISO8601", s.observe, "localized", loc, opts, patternInstant.UnixMilli(), "jms", "iso")
	}

	block := src.from(`pattern = UnicodeString("hmv")`)
	want := block.from("UnicodeString expR =")
	s.pattern(want.line, "en", intervalString(s.t, block.lines[0]), intervalString(s.t, want.lines[0]), localeHourWidth)
	block = src.from(`const char* testLocaleID = "de_US"`)
	s.clock(block.line, intervalString(s.t, block.lines[0]), "h12")

	s.excluded["generator populated with custom day-period patterns"] += len(src.table("testItems[][2]"))
	s.excluded["generator skeleton extraction or enumeration"] += len(src.table("testGetSkeletonAndBase[][3]")) +
		len(src.table("testSkeletonsResults[]")) + len(src.table("testBaseSkeletonsResults[]")) + len(src.from("::testStaticGetSkeleton(").table("testData[]"))
	s.excluded["localized field display name"] += len(src.table("FieldDisplayNameData testData[]"))
	s.excluded["custom date/time combining pattern"] += len(skel)
	s.excluded["week-based year in Japanese and Chinese month regression"] += 2
	s.excluded["comparison of three ICU formatting APIs without an independent expected pattern"] += len(listArray(src.from("::test_jConsistencyOddLocales() {"), "localeIDs[]"))
}

func (s *datePatternSuite) fixedDateTables(src intervalSource) {
	pacific, err := time.LoadLocation(intervalPacific)
	if err != nil {
		s.t.Fatal(err)
	}
	date := time.Date(1999, time.January, 13, 23, 58, 59, 123e6, pacific)
	skeletons := listArray(src, "UnicodeString patternData[]")
	locales := src.table("testLocale[MAX_LOCALE][4]")
	results := src.table("patternResults[MAX_LOCALE]")
	if len(locales) != len(results) {
		s.t.Fatalf("%s: %d locale rows for %d result tables", s.path, len(locales), len(results))
	}
	for i, row := range locales {
		v := s.strings(row, 4)
		loc := strings.TrimRight(strings.Join(v[:3], "-"), "-")
		if v[3] != "" {
			loc += "@" + v[3]
		}
		table := strings.TrimSuffix(results[i].text, ",")
		s.fixedRows(src.table(table+"[]"), skeletons, loc, date, intervalPacific)
	}
	// The test creates the October instant in Los Angeles, then formats it
	// with ECT/GMT (an unknown ICU zone with zero offset) and ECT (Paris).
	oct := time.Date(1999, time.October, 13, 23, 58, 59, 0, pacific)
	skeletons = listArray(src, "UnicodeString patternTests2[]")
	rows := src.table("UnicodeString patternResults2[]")
	if skeletons[len(skeletons)-1] != "" || intervalString(s.t, rows[len(rows)-1].text) != "" {
		s.t.Fatalf("%s: missing English table terminator", s.path)
	}
	s.fixedRows(rows[:len(rows)-1], skeletons[:len(skeletons)-1], "en", oct.UTC(), "UTC")
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		s.t.Fatal(err)
	}
	best := src.from("UnicodeString findPattern = gen->getBestPattern")
	want := best.from("expectedResult=UnicodeString(")
	s.fixed(want.line, "de-DE", intervalString(s.t, best.lines[0]), oct.In(paris), paris.String(), intervalString(s.t, want.lines[0]))
}

func (s *datePatternSuite) fixedRows(rows []intervalRow, skeletons []string, loc string, date time.Time, zone string) {
	if len(rows) != len(skeletons) {
		s.t.Fatalf("%s: %s has %d results for %d skeletons", s.path, loc, len(rows), len(skeletons))
	}
	for i, row := range rows {
		s.fixed(row.line, loc, skeletons[i], date, zone, intervalString(s.t, row.text))
	}
}

var dateFieldRow = regexp.MustCompile(`^\{\s*'(.)',\s*\{([0-9,]+)\},\s*"([A-Za-z]+)"\s*\},?$`)

func (s *datePatternSuite) allDateFields(src intervalSource) {
	block := src.from("::testAllFieldPatterns(")
	locales := listArray(block, "localeNames[]")
	fieldTypes := map[byte]string{
		'G': "era", 'y': "year", 'U': "yearName", 'M': "month", 'L': "month",
		'd': "day", 'E': "weekday", 'e': "weekday", 'c': "weekday",
		'h': "hour", 'H': "hour", 'K': "hour", 'k': "hour", 'a': "dayPeriod", 'B': "dayPeriod",
		'm': "minute", 's': "second", 'S': "fractionalSecond",
		'z': "timeZoneName", 'Z': "timeZoneName", 'O': "timeZoneName", 'v': "timeZoneName",
	}
	for _, row := range block.table("AllFieldsTestItem testData[]") {
		v := dateFieldRow.FindStringSubmatch(row.text)
		if v == nil {
			s.t.Fatalf("%s:%d: invalid field test %s", s.path, row.line, row.text)
		}
		for w := range strings.SplitSeq(v[2], ",") {
			if w == "0" {
				break
			}
			skel := strings.Repeat(v[1], integer(w))
			for _, id := range locales {
				loc, opts, reason := datePatternOptions(id, skel, "UTC")
				if reason != "" {
					s.excluded[reason]++
					continue
				}
				op := "includes/" + id + "/" + skel
				switch v[3] {
				case "hK", "Hk":
					// At 13:05 the clock families give different hour parts.
					want := "1"
					if v[3] == "Hk" {
						want = "13"
					}
					s.check(row.line, op, s.observe, want, loc, opts, patternInstant.Add(12*time.Hour).UnixMilli(), skel, "hour")
					continue
				case "B":
					s.check(row.line, op, s.observe, "flexible", loc, opts, patternInstant.UnixMilli(), skel, "kind", "dayPeriod")
					continue
				case "z", "Z", "O", "v":
					want := map[string]string{"z": "specific", "v": "Generic", "O": "Offset", "Z": "Offset"}[v[3]]
					s.check(row.line, op, s.observe, want, loc, opts, patternInstant.UnixMilli(), skel, "kind", "timeZoneName")
					continue
				case "S":
					s.check(row.line, op, s.observe, patternFraction(len(skel)), loc, opts, patternInstant.UnixMilli(), skel, "fraction")
					continue
				}
				var names []any
				for i := range len(v[3]) {
					name := fieldTypes[v[3][i]]
					if name == "" {
						s.t.Fatalf("%s:%d: unknown field in %s", s.path, row.line, v[3])
					}
					names = append(names, name)
				}
				s.check(row.line, op, s.observe, "present", loc, opts, patternInstant.UnixMilli(), skel, "includes", names)
			}
		}
	}
}

func (s *datePatternSuite) clock(line int, id, cycle string) {
	loc, opts, reason := datePatternOptions(id, "j", "UTC")
	if reason != "" {
		s.excluded[reason]++
		return
	}
	// At midnight the four clock conventions have distinct zero, 12 and
	// 24 values. The resolved cycle also distinguishes h11 from h23.
	hour := map[string]string{"h11": "0", "h12": "12", "h23": "0", "h24": "24"}[cycle]
	if opts["numberingSystem"] == "arab" {
		hour = strings.Map(func(r rune) rune { return '٠' + r - '0' }, hour)
	}
	s.check(line, "hourCycle", s.observe, cycle+" "+hour, loc, opts, patternInstant.Truncate(24*time.Hour).UnixMilli(), "j", "clock")
}

func (s *datePatternSuite) cDatePatterns(src intervalSource) {
	for _, row := range src.table("appendItemNameData[]") {
		if !strings.Contains(row.text, "UDATPG_FIELD_COUNT") {
			s.excluded["generator append-item field name"]++
		}
	}
	uchar := func(name string) string {
		return fixtureUChars(s.t, src.from(name + "[]").lines[0])
	}
	row := src.from("sampleFormatted[]")
	ms := src.from("UDate sampleDate =").lines[0]
	_, ms, _ = strings.Cut(ms, "= ")
	instant := time.UnixMilli(intervalMillis(s.t, strings.TrimSuffix(ms, ";"))).UTC()
	s.fixed(row.line, "fr", uchar("skeleton"), instant, "UTC", uchar("sampleFormatted"))
	row = src.from("expectingBestPattern[]")
	s.pattern(row.line, "fi", uchar("testSkeleton1"), uchar("expectingBestPattern"), localeHourWidth)
	for _, row := range src.from("void TestEras(void) {").table("localeIDs[] =") {
		loc, opts, _ := datePatternOptions(intervalString(s.t, row.text), "y", "UTC")
		s.check(row.line, "formatToParts/era", s.observe, "present", loc, opts, patternInstant.UnixMilli(), "y", "includes", []any{"era"})
	}
	for _, row := range src.table("HourCycleData testData[]") {
		if strings.HasPrefix(row.text, "/*") {
			continue
		}
		v := row.columns(s.t, 2)
		s.clock(row.line, intervalString(s.t, v[0]), "h"+strings.TrimPrefix(v[1], "UDAT_HOUR_CYCLE_"))
	}
}

func (s *datePatternSuite) javaDatePatterns(src intervalSource) {
	s.excluded["ICU root locale in TestRoot"]++

	for _, row := range src.from("void testISO8601More()").table("testCases =") {
		v := s.strings(row, 3)
		s.pattern(row.line, v[0], v[1], v[2], localeHourWidth)
	}

	// The Taiwan options rows follow the shared C++ option cases. Some Java
	// constructor calls span several lines.
	var item intervalRow
	for _, row := range src.table("TestOptionsItem[] testOptionsData") {
		if strings.HasPrefix(row.text, "new TestOptionsItem(") {
			item = row
		} else {
			item.text += row.text
		}
		if !strings.HasSuffix(row.text, "),") {
			continue
		}
		v := s.strings(item, 3)
		if strings.HasPrefix(v[0], "zh_TW") {
			switch {
			case strings.Contains(item.text, ".MATCH_NO_OPTIONS"):
				s.pattern(item.line, v[0], v[1], v[2], localeHourWidth)
			case strings.Contains(item.text, ".MATCH_ALL_FIELDS_LENGTH"):
				s.pattern(item.line, v[0], v[1], v[2], requestedHourWidth)
			default:
				s.t.Fatalf("%s:%d: unknown match option in %s", s.path, item.line, item.text)
			}
		}
	}
	// Java alone checks fallback for the observational Islamic calendar.
	for _, row := range src.from("void testBestPattern()").table("String[] testCases") {
		v := s.strings(row, 3)
		if strings.Contains(v[0], "@calendar=islamic") {
			s.pattern(row.line, v[0], v[1], v[2], localeHourWidth)
		}
	}
}
