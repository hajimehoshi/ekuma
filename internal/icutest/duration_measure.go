// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strings"
)

var cppMeasure = regexp.MustCompile(`Measure\(\s*(?:static_cast<double>\()?(-?[0-9.]+)\)?,\s*(?:MeasureUnit::create([A-Za-z]+)\(status\)|TimeUnit::createInstance\(\s*TimeUnit::UTIMEUNIT_([A-Z]+),\s*status\)),\s*status\)`)

func measureArray(src intervalSource, name string) []durationMeasure {
	block := src.from("Measure " + name + "[] =")
	var lines []string
	for _, line := range block.lines {
		lines = append(lines, line)
		if strings.Contains(line, "};") {
			break
		}
	}
	body := strings.Join(lines, " ")
	var measures []durationMeasure
	for _, match := range cppMeasure.FindAllStringSubmatch(body, -1) {
		measures = append(measures, durationMeasure{
			unit:   strings.ToLower(match[2] + match[3]),
			amount: match[1],
		})
	}
	if len(measures) == 0 || len(measures) != strings.Count(body, "Measure(") {
		src.t.Fatalf("%s:%d: cannot read %s", src.path, block.line, body)
	}
	return measures
}

func (s *durationSuite) measureFormats(src intervalSource) {
	block := src.from("::TestFormatPeriodEn() {")
	for _, table := range []struct {
		name, locale, style string
	}{
		{
			name:   "fullData",
			locale: "en",
			style:  "long",
		},
		{
			name:   "abbrevData",
			locale: "en",
			style:  "short",
		},
		{
			name:   "narrowData",
			locale: "en",
			style:  "narrow",
		},
		{
			name:   "numericData",
			locale: "en",
			style:  "digital",
		},
		{
			name:   "fullDataDe",
			locale: "de",
			style:  "long",
		},
		{
			name:   "numericDataDe",
			locale: "de",
			style:  "digital",
		},
		{
			name:   "numericDataBn",
			locale: "bn",
			style:  "digital",
		},
		{
			name:   "numericDataBnLatn",
			locale: "bn-u-nu-latn",
			style:  "digital",
		},
	} {
		for _, row := range block.table("ExpectedResult " + table.name + "[] =") {
			cols := row.columns(s.t, 3)
			s.measures(row.line, table.locale, table.style, measureArray(block, cols[0]), -1, intervalString(s.t, cols[2]))
		}
	}
	for _, name := range []string{"fullDataSpellout", "fullDataSpelloutFr"} {
		s.excluded["spellout number formatter"] += len(block.table("ExpectedResult " + name + "[] ="))
	}

	block = src.from("::TestManyLocaleDurations() {")
	measures := measureArray(block, "measures")
	block = block.from("helperTestManyLocaleDurations(\"")
	for i, line := range block.lines {
		if line == "}" {
			break
		}
		if strings.HasPrefix(line, "helperTestManyLocaleDurations(") {
			args := listStrings(s.t, line)
			style := "narrow"
			if strings.Contains(line, "UMEASFMT_WIDTH_NUMERIC") {
				style = "digital"
			} else if !strings.Contains(line, "UMEASFMT_WIDTH_NARROW") {
				s.t.Fatalf("%s:%d: unknown width: %s", src.path, block.line+i, line)
			}
			s.measures(block.line+i, strings.ReplaceAll(args[0], "_", "-"), style, measures, -1, args[1])
		}
	}
	block = src.from("::Test21223_FrenchDuration() {").from("assertEquals(")
	s.check(block.line, "format", s.format, listStrings(s.t, block.lines[0])[1], "fr-FR",
		map[string]any{"hours": 5, "minutes": 10}, map[string]any{"style": "narrow"})

	rows := src.from("::TestGreek() {").table("const char *expected[] =")
	if len(rows) != 2*2*2*7 {
		s.t.Fatalf("%s: got %d Greek results for two locales, two values, two widths and seven units", src.path, len(rows))
	}
	var n int
	for _, locale := range []string{"el-GR", "el"} {
		for _, value := range []int{1, 7} {
			for _, style := range []string{"long", "short"} {
				for _, unit := range []string{"second", "minute", "hour", "day", "week", "month", "year"} {
					row := rows[n]
					s.check(row.line, "format", s.format, intervalString(s.t, row.text), locale,
						map[string]any{unit + "s": value}, map[string]any{"style": style})
					n++
				}
			}
		}
	}
}

func (s *durationSuite) numericTime(src intervalSource) {
	// The C++ and Java period and Greek tables cover the same cases. Java
	// additionally tests the default three fraction digits with Bengali digits.
	block := src.from("void TestNumericTimeNonLatin()")
	row := block.from("assertEquals(")
	s.measures(row.line, "bn", "digital", []durationMeasure{
		{
			unit:   "minute",
			amount: "12",
		},
		{
			unit:   "second",
			amount: "39.12345",
		},
	}, 3, listStrings(s.t, row.lines[0])[1])

	// Both readers check over-range clock fields without carrying. The
	// Java assertions name their input measures directly.
	block = src.from("void TestNumericTime()")
	amounts := map[string]durationMeasure{}
	for i, line := range block.lines {
		if line == "}" {
			break
		}
		if after, ok := strings.CutPrefix(line, "Measure "); ok {
			name, expr, ok := strings.Cut(after, " = new Measure(")
			amount, unit, found := strings.Cut(strings.TrimSuffix(expr, ");"), ", MeasureUnit.")
			if !ok || !found {
				s.t.Fatalf("%s:%d: unexpected measure %s", src.path, block.line+i, line)
			}
			amounts[name] = durationMeasure{
				unit:   strings.ToLower(unit),
				amount: amount,
			}
		}
		if strings.HasPrefix(line, "Assert.assertEquals(") {
			_, args, ok := strings.Cut(line, "fmt.formatMeasures(")
			if !ok {
				s.t.Fatalf("%s:%d: expected formatMeasures", src.path, block.line+i)
			}
			var measures []durationMeasure
			for name := range strings.SplitSeq(strings.TrimSuffix(args, "));"), ", ") {
				measures = append(measures, amounts[name])
			}
			s.measures(block.line+i, "en", "digital", measures, 3, intervalString(s.t, line))
		}
	}
}

func (s *durationSuite) timeUnits(src intervalSource) {
	for _, tc := range []struct{ method, locale string }{
		{
			method: "TestClone",
			locale: "en",
		},
		{
			method: "TestBritishShortHourFallback",
			locale: "en-GB",
		},
	} {
		row := src.from("void " + tc.method + "()").from("assertEquals(")
		s.check(row.line, "format", s.format, listStrings(s.t, row.lines[0])[1], tc.locale,
			map[string]any{"hours": 1}, map[string]any{"style": "short"})
	}
}

func (s *durationSuite) negativeDurations(src intervalSource) {
	// ICU decomposes -1.24 hours into h/m/s. Its Dutch media preference
	// rounds -77.7 seconds to whole seconds and retains tenths for -2.7.
	for _, tc := range []struct {
		label, locale string
		fields        map[string]any
	}{
		{
			label:  "Negative numbers: time",
			locale: "de-DE",
			fields: map[string]any{"hours": -1, "minutes": -14, "seconds": -24},
		},
		{
			label:  "Negative numbers: minute-and-second",
			locale: "nl-NL",
			fields: map[string]any{"minutes": -1, "seconds": -18},
		},
		{
			label:  "Negative numbers: media seconds",
			locale: "nl-NL",
			fields: map[string]any{"seconds": -2, "milliseconds": -700},
		},
	} {
		row := src.from(tc.label).from("u\"-")
		opts := map[string]any{"style": "short"}
		if tc.fields["milliseconds"] != nil {
			opts["milliseconds"] = "numeric"
		}
		want := intervalString(s.t, row.lines[0])
		s.check(row.line, "format", s.format, want, tc.locale, tc.fields, opts)
		if tc.fields["milliseconds"] != nil {
			// The Dutch row is a signed decimal followed by a space and the unit.
			number, unit, ok := strings.Cut(want, " ")
			tokens := clockTokens.FindAllString(number, -1)
			if !ok || len(tokens) != 4 || unit == "" {
				s.t.Fatalf("%s:%d: unexpected signed seconds %q", src.path, row.line, want)
			}
			s.check(row.line, "formatToParts", s.parts, strings.Join([]string{
				"minusSign[second] " + tokens[0], "integer[second] " + tokens[1],
				"decimal[second] " + tokens[2], "fraction[second] " + tokens[3],
				"literal[second]  ", "unit[second] " + unit,
			}, "\n"), tc.locale, tc.fields, opts)
		}
	}
}
