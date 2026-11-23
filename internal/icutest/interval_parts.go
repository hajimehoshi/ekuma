// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

var formatFields = map[string]string{
	"UDAT_DATE_FIELD":                  "day",
	"UDAT_MONTH_FIELD":                 "month",
	"UDAT_YEAR_FIELD":                  "year",
	"UDAT_HOUR_OF_DAY0_FIELD":          "hour",
	"UDAT_MINUTE_FIELD":                "minute",
	"UDAT_AM_PM_FIELD":                 "dayPeriod",
	"UNUM_FRACTION_FIELD":              "fraction",
	"UNUM_CURRENCY_FIELD":              "currency",
	"UNUM_DECIMAL_SEPARATOR_FIELD":     "decimal",
	"UNUM_EXPONENT_SYMBOL_FIELD":       "exponentSeparator",
	"UNUM_EXPONENT_SIGN_FIELD":         "exponentMinusSign",
	"UNUM_EXPONENT_FIELD":              "exponentInteger",
	"DecimalFormat::kMeasureUnitField": "unit",
	"UNUM_INTEGER_FIELD":               "integer",
	"UNUM_COMPACT_FIELD":               "compact",
	"UNUM_MEASURE_UNIT_FIELD":          "unit",
	"UNUM_GROUPING_SEPARATOR_FIELD":    "group",
	"UNUM_APPROXIMATELY_SIGN_FIELD":    "approximatelySign",
}

type intervalPosition struct {
	label      string
	begin, end int
}

func rangeParts(t *testing.T, want string, rows []intervalRow) string {
	t.Helper()
	units := utf16.Encode([]rune(want))
	var fields, spans []intervalPosition
	for _, row := range rows {
		col := row.columns(t, 4)
		p := intervalPosition{
			begin: integer(col[2]),
			end:   integer(col[3]),
		}
		if p.begin < 0 || p.end <= p.begin || p.end > len(units) {
			t.Fatalf("line %d: invalid UTF-16 range %d..%d for %q", row.line, p.begin, p.end, want)
		}
		switch col[0] {
		case "UFIELD_CATEGORY_DATE_INTERVAL_SPAN", "UFIELD_CATEGORY_NUMBER_RANGE_SPAN":
			switch col[1] {
			case "0":
				p.label = "startRange"
			case "1":
				p.label = "endRange"
			default:
				t.Fatalf("line %d: unknown interval span %s", row.line, col[1])
			}
			spans = append(spans, p)
		case "UFIELD_CATEGORY_DATE", "UFIELD_CATEGORY_NUMBER":
			p.label = formatFieldType(t, col[1], units[p.begin:p.end], row.line)
			fields = append(fields, p)
		default:
			t.Fatalf("line %d: unknown field category %s", row.line, col[0])
		}
	}
	return positionedParts(units, fields, spans, "shared")
}

func formatFieldType(t *testing.T, field string, value []uint16, line int) string {
	t.Helper()
	if field == "UNUM_SIGN_FIELD" {
		switch string(utf16.Decode(value)) {
		case "-":
			return "minusSign"
		case "+":
			return "plusSign"
		}
	}
	if label := formatFields[field]; label != "" {
		return label
	}
	t.Fatalf("line %d: unknown field %s for %q", line, field, string(utf16.Decode(value)))
	return ""
}

// Field boundaries determine part types. Range spans also assign a source to
// each part, including literals inside the span. Both use UTF-16 offsets.
func positionedParts(units []uint16, fields, spans []intervalPosition, defaultSource string) string {
	bounds := []int{0, len(units)}
	for _, p := range fields {
		bounds = append(bounds, p.begin, p.end)
	}
	for _, p := range spans {
		bounds = append(bounds, p.begin, p.end)
	}
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	var parts []string
	for i, begin := range bounds[:len(bounds)-1] {
		end := bounds[i+1]
		typ, source := "literal", defaultSource
		// Grouping separators are nested inside the ICU integer field.
		width := len(units) + 1
		for _, field := range fields {
			if field.begin <= begin && end <= field.end && field.end-field.begin < width {
				typ = field.label
				width = field.end - field.begin
			}
		}
		for _, span := range spans {
			if span.begin <= begin && end <= span.end {
				source = span.label
			}
		}
		if source != "" {
			typ = source + " " + typ
		}
		parts = append(parts, typ+" "+string(utf16.Decode(units[begin:end])))
	}
	return strings.Join(parts, "\n")
}

func (s *intervalSuite) checkResult(block intervalSource, group string, in intervalInput) {
	row := block.from("expectedString = ")
	want := intervalString(s.t, row.lines[0])
	s.checkInterval(row.line, group, "formatRange", in, want)
	parts := rangeParts(s.t, want, row.table("expectedFieldPositions[] ="))
	s.checkInterval(row.line, group, "formatRangeToParts", in, parts)
}

func (s *intervalSuite) checkMinutePositions(src intervalSource) {
	t := s.t
	var deltas []int64
	for _, row := range src.table("double deltas[]") {
		d := intervalMillis(t, strings.TrimSuffix(row.text, ","))
		if d >= 0 {
			deltas = append(deltas, d)
		}
	}
	start := src.from("UDate startTime =")
	_, value, _ := strings.Cut(start.lines[0], "=")
	ms := intervalMillis(t, strings.TrimSuffix(value, ";"))
	for _, row := range src.table("LocaleAndSkeletonItem locSkelItems[]") {
		col := row.columns(t, 4)
		if col[0] == "NULL" {
			continue
		}
		if col[2] != "UDAT_MINUTE_FIELD" {
			t.Fatalf("%s:%d: unexpected field %s", src.path, row.line, col[2])
		}
		rows := src.table("ExpectPosAndFormat " + col[3] + "[")
		if len(rows) != len(deltas) {
			t.Fatalf("%s:%d: got %d rows for %d deltas", src.path, row.line, len(rows), len(deltas))
		}
		in := intervalInput{
			locale:   intervalString(t, col[0]),
			skeleton: intervalString(t, col[1]),
			zone:     "GMT",
			start:    ms,
		}
		single := intervalString(t, rows[0].columns(t, 3)[2])
		for i, expected := range rows {
			in.end = ms + deltas[i]
			col := expected.columns(t, 3)
			want := intervalString(t, col[2])
			const group = "TestFPos_SkelWithSeconds"
			s.checkInterval(expected.line, group, "formatRange", in, want)
			begin, end := integer(col[0]), integer(col[1])
			units := utf16.Encode([]rune(want))
			if begin < 0 || begin > end || end > len(units) {
				t.Fatalf("%s:%d: invalid minute position", src.path, expected.line)
			}
			pos := fmt.Sprintf("%d:%d:%s", begin, end, string(utf16.Decode(units[begin:end])))
			s.checkInterval(expected.line, group, "minutePosition", in, pos)
			if in.start != in.end && want == single {
				s.checkInterval(expected.line, group, "sources", in, "shared")
			}
		}
	}
}

func (s *intervalSuite) checkGermanParts(src intervalSource, dates map[string]int64) {
	block := src.from("static void TestFormatToResult(void) {")
	in := intervalInput{
		locale:   "de",
		skeleton: "dMMMMyHHmm",
		zone:     "GMT",
		start:    dates["Date201009270800"],
		end:      dates["Date201103021030"],
	}
	s.checkResult(block.from(`message = "Field position test 1"`), "TestFormatToResult 1", in)
	in.end = in.start + 7*3600000
	s.checkResult(block.from(`message = "Field position test 2"`), "TestFormatToResult 2", in)

	// The first two calendar overload cases repeat the same fields and spans.
	block = src.from("static void TestFormatCalendarToResult(void) {")
	s.excluded["calendar overload duplicates TestFormatToResult"] += 2
	s.excluded["Julian/Gregorian cutover in 1582"]++
	in.start, in.end = dates["Date158210140000"], dates["Date158210160000"]
	s.checkResult(block.from(`message = "Field position test 4"`), "TestFormatCalendarToResult 4 (proleptic Gregorian)", in)
}

func (s *intervalSuite) checkEnglishParts(src intervalSource) {
	t := s.t
	block := src.from("::testFormattedDateInterval() {")
	july20 := icuWallTime(s.pacific, 2018, 7, 20, 0, 0, 0, 0)
	in := intervalInput{
		locale:   "en-US",
		skeleton: "dMMMMy",
		zone:     intervalPacific,
		start:    july20,
		end:      icuWallTime(s.pacific, 2018, 7, 25, 0, 0, 0, 0),
	}
	s.checkResult(block.from(`message = u"FormattedDateInterval test 1"`), "testFormattedDateInterval 1", in)
	in.end = july20
	s.checkResult(block.from(`message = u"FormattedDateInterval identical dates`), "testFormattedDateInterval identity", in)
	in.end = icuWallTime(s.pacific, 2018, 8, 3, 0, 0, 0, 0)
	sample := block.from(`assertEquals("Expected output from format"`)
	s.checkInterval(sample.line+1, "testFormattedDateInterval sample", "formatRange", in, intervalString(t, sample.lines[1]))
	spanIndex := func(marker string) string {
		_, tail, _ := strings.Cut(sample.from(marker).lines[0], marker)
		value, _, _ := strings.Cut(tail, ",")
		return strings.TrimSpace(value)
	}
	s.checkInterval(sample.line+1, "testFormattedDateInterval sample", "startSpan", in, spanIndex(`"Expect start index",`)+":"+spanIndex(`"Expect end index",`))
	s.excluded["custom reversed fallback pattern"]++
}

func (s *intervalSuite) checkSpanIdentity(src intervalSource) {
	block := src.from("::testTicket20710_FieldIdentity() {")
	in := intervalInput{
		locale:   "de-CH",
		skeleton: "eeeeMMMddyyhhmma",
		zone:     "CET",
		start:    1563235200000,
		end:      1564235200000,
	}
	row := block.from(`assertTrue("Span field`)
	s.checkInterval(row.line, "testTicket20710_FieldIdentity", "sources", in, "endRange shared startRange")
	in.end = in.start
	row = block.from(`assertFalse("Span field`)
	s.checkInterval(row.line, "testTicket20710_FieldIdentity", "sources", in, "shared")
}
