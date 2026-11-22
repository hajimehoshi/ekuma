// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

func (s *relativeSuite) cppRelativeFields(src intervalSource) {
	check := func(call, locale, numeric, unit string, offset float64) {
		row := src.from(call).from("expectedString = ")
		want := intervalString(s.t, row.lines[0])
		s.check(row.line, "format", s.format, want, locale, "long", numeric, offset, unit)
		parts := s.mixedRelativeParts(want, unit, row.table("expectedFieldPositions[] ="))
		s.check(row.line, "formatToParts", s.parts, parts, locale, "long", numeric, offset, unit)
	}
	check("fmt.formatToValue(1, UDAT_REL_UNIT_DAY", "en-US", "auto", "day", 1)
	check("fmt.formatToValue(3, UDAT_REL_UNIT_DAY", "en-US", "auto", "day", 3)
	check("fmt.formatNumericToValue(1.5, UDAT_REL_UNIT_WEEK", "en-US", "always", "week", 1.5)
	check("fmt.formatToValue(12, UDAT_DIRECTION_LAST, UDAT_RELATIVE_HOURS", "en-US", "always", "hour", -12)
	check("fmt.formatToValue(12, UDAT_REL_UNIT_HOUR", "sw", "auto", "hour", 12)
	s.excluded["weekday unit"]++
}

func (s *relativeSuite) mixedRelativeParts(want, unit string, rows []intervalRow) string {
	text := utf16.Encode([]rune(want))
	labels := make([]string, len(text))
	relativeFields := make([]string, len(text))
	for _, row := range rows {
		c := row.columns(s.t, 4)
		begin, end := integer(c[2]), integer(c[3])
		if begin < 0 || begin >= end || end > len(text) {
			s.t.Fatalf("%s:%d: invalid field range %d..%d for %q", s.path, row.line, begin, end, want)
		}
		switch c[0] {
		case "UFIELD_CATEGORY_RELATIVE_DATETIME":
			if c[1] != "UDAT_REL_NUMERIC_FIELD" && c[1] != "UDAT_REL_LITERAL_FIELD" {
				s.t.Fatalf("%s:%d: unknown relative-time field %s", s.path, row.line, c[1])
			}
			for i := begin; i < end; i++ {
				relativeFields[i] = c[1]
			}
		case "UFIELD_CATEGORY_NUMBER":
			label := map[string]string{
				"UNUM_INTEGER_FIELD":           "integer",
				"UNUM_DECIMAL_SEPARATOR_FIELD": "decimal",
				"UNUM_FRACTION_FIELD":          "fraction",
			}[c[1]]
			if label == "" {
				s.t.Fatalf("%s:%d: unknown number field %s", s.path, row.line, c[1])
			}
			for i := begin; i < end; i++ {
				labels[i] = label + "[" + unit + "]"
			}
		default:
			s.t.Fatalf("%s:%d: unknown field category %s", s.path, row.line, c[0])
		}
	}
	// ICU can leave whitespace outside its literal fields. It belongs to the
	// adjacent literal in formatToParts.
	for i, field := range relativeFields {
		if (field == "UDAT_REL_NUMERIC_FIELD") != (labels[i] != "") || (field == "" && !unicode.IsSpace(rune(text[i]))) {
			s.t.Fatalf("%s:%d: inconsistent fields at UTF-16 offset %d in %q", s.path, rows[0].line, i, want)
		}
		if labels[i] == "" {
			labels[i] = "literal"
		}
	}
	var parts []string
	for begin := 0; begin < len(text); {
		end := begin + 1
		for end < len(text) && labels[end] == labels[begin] {
			end++
		}
		parts = append(parts, labels[begin]+" "+string(utf16.Decode(text[begin:end])))
		begin = end
	}
	return strings.Join(parts, "\n")
}

func (s *relativeSuite) cRelative(src intervalSource) {
	line := src.from("double offsets[] =")
	_, values, _ := strings.Cut(line.lines[0], "{")
	values, _, _ = strings.Cut(values, "}")
	var offsets []float64
	for token := range strings.SplitSeq(values, ",") {
		offsets = append(offsets, s.offset(line.line, strings.TrimSpace(token)))
	}
	items := src.table("fmtTestItems[] =")
	for i := 0; i < len(items); i += 2 {
		row := items[i]
		if strings.HasPrefix(row.text, "{ NULL") {
			break
		}
		row.text += items[i+1].text
		c := row.columns(s.t, 7)
		unit := strings.ToLower(strings.TrimPrefix(c[4], "UDAT_REL_UNIT_"))
		switch {
		case weekdayUnit(unit):
			s.excluded["weekday unit"] += 2 * len(offsets)
			continue
		case c[1] != "-1":
			s.excluded["custom fraction digits"] += 2 * len(offsets)
			continue
		case c[3] == "UDISPCTX_CAPITALIZATION_FOR_STANDALONE":
			s.excluded["standalone capitalization context"] += 2 * len(offsets)
			continue
		case c[3] != "UDISPCTX_CAPITALIZATION_FOR_MIDDLE_OF_SENTENCE":
			s.t.Fatalf("%s:%d: unknown capitalization context %s", s.path, row.line, c[3])
		}
		locale := strings.ReplaceAll(intervalString(s.t, c[0]), "@calendar=iso8601", "-u-ca-iso8601")
		style := strings.ToLower(strings.TrimPrefix(c[2], "UDAT_STYLE_"))
		results := relativeOffsetRows(src, c[5], len(offsets))
		fields := relativeOffsetRows(src, c[6], len(offsets))
		for j, offset := range offsets {
			wants := listStrings(s.t, results[j].text)
			spans := strings.Split(strings.TrimSuffix(fields[j].text, ","), "},")
			if len(wants) != 2 || len(spans) != 2 {
				s.t.Fatalf("%s:%d: expected auto and always entries", s.path, results[j].line)
			}
			for k, numeric := range []string{"auto", "always"} {
				want := wants[k]
				s.check(results[j].line, "format", s.format, want, locale, style, numeric, offset, unit)
				span := intervalRow{
					line: fields[j].line,
					text: spans[k],
				}
				parts := s.relativeSpanParts(span, want, unit)
				s.check(span.line, "formatToParts", s.parts, parts, locale, style, numeric, offset, unit)
			}
		}
	}
	block := src.from("ureldatefmt_formatToResult(fmt, -1, UDAT_REL_UNIT_WEEK")
	rows := block.table("expectedFieldPositions[] =")
	result := block.from("u\"")
	want := intervalString(s.t, result.lines[0])
	s.check(result.line, "format", s.format, want, "en-US", "short", "auto", -1, "week")
	s.check(result.line, "formatToParts", s.parts, s.mixedRelativeParts(want, "week", rows), "en-US", "short", "auto", -1, "week")
	s.excluded["weekday unit"]++
}

func relativeOffsetRows(src intervalSource, table string, n int) []intervalRow {
	var rows []intervalRow
	for _, row := range src.table(table + "[kNumOffsets*2] =") {
		row.text, _, _ = strings.Cut(row.text, "/*")
		row.text = strings.TrimSpace(row.text)
		if row.text != "" {
			rows = append(rows, row)
		}
	}
	if len(rows) != n {
		src.t.Fatalf("%s: %s has %d rows for %d offsets", src.path, table, len(rows), n)
	}
	return rows
}

// The C offset tables supply one span for the whole number. The selected
// English and French quantities contain only ungrouped digits and a decimal separator.
func (s *relativeSuite) relativeSpanParts(row intervalRow, want, unit string) string {
	c := row.columns(s.t, 3)
	if c[0] == "-1" && c[1] == "-1" && c[2] == "-1" {
		return "literal " + want
	}
	begin, end := integer(c[1]), integer(c[2])
	text := utf16.Encode([]rune(want))
	if c[0] != "UDAT_REL_NUMERIC_FIELD" || begin < 0 || end <= begin || end > len(text) {
		s.t.Fatalf("%s:%d: invalid numeric field %s for %q", s.path, row.line, row.text, want)
	}
	var parts []string
	if begin > 0 {
		parts = append(parts, "literal "+string(utf16.Decode(text[:begin])))
	}
	number := string(utf16.Decode(text[begin:end]))
	suffix := "[" + unit + "] "
	if i := strings.IndexAny(number, ".,"); i >= 0 {
		parts = append(parts, "integer"+suffix+number[:i], "decimal"+suffix+number[i:i+1], "fraction"+suffix+number[i+1:])
	} else {
		parts = append(parts, "integer"+suffix+number)
	}
	if end < len(text) {
		parts = append(parts, "literal "+string(utf16.Decode(text[end:])))
	}
	return strings.Join(parts, "\n")
}
