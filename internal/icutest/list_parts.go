// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

func (s *listSuite) cppListPositions(src intervalSource) {
	for _, suffix := range []string{"1Item", "2Items", "2ItemsPatternShift", "3Items", "3ItemsPatternShift"} {
		group := "TestFieldPositionIteratorWith" + suffix
		block := src.from("::" + group + "() {")
		locale, typ, style := "en", "conjunction", "long"
		if strings.HasSuffix(suffix, "PatternShift") {
			locale, typ, style = "ur-IN", "unit", "narrow"
		}
		input := listArray(block, "UnicodeString data[")
		result := block.from(`u"`)
		s.checkFields(result, group, locale, typ, style, input, block.table("expected[] ="), false)
	}

	block := src.from("::TestFormattedValue() {")
	for _, tc := range []struct {
		marker, locale, typ, style string
	}{
		{
			marker: "Field position test 1",
			locale: "en",
			typ:    "conjunction",
			style:  "long",
		},
		{
			marker: "Field position test 2",
			locale: "zh",
			typ:    "unit",
			style:  "short",
		},
		{
			marker: "ICU-21383 Long list",
			locale: "en",
			typ:    "unit",
			style:  "short",
		},
	} {
		s.mixedListFields(block.from(tc.marker), tc.marker, tc.locale, tc.typ, tc.style)
	}

	block = src.from("::TestNextPosition() {")
	input := listArray(block, "inputs[] =")
	for _, locale := range listArray(block, "locales =") {
		for _, typ := range []string{"conjunction", "disjunction", "unit"} {
			for _, style := range []string{"long", "short", "narrow"} {
				for n := 1; n <= len(input); n++ {
					s.checkList(block.line, "TestNextPosition", "elementCount", locale, typ, style, input[:n], strconv.Itoa(n))
				}
			}
		}
	}
}

func (s *listSuite) mixedListFields(block intervalSource, group, locale, typ, style string) {
	input := listArray(block, "inputs[] =")
	result := block.from("expectedString =")
	s.checkFields(result, group, locale, typ, style, input, block.table("expectedFieldPositions[] ="), true)
}

func (s *listSuite) checkFields(result intervalSource, group, locale, typ, style string, input []string, rows []intervalRow, mixed bool) {
	want := intervalString(s.t, result.lines[0])
	s.checkList(result.line, group, "format", locale, typ, style, input, want)
	units := utf16.Encode([]rune(want))
	var parts []string
	var offset int
	for _, row := range rows {
		var cols []string
		if mixed {
			cols = row.columns(s.t, 4)
		} else {
			cols = row.columns(s.t, 3)
		}
		begin, end := integer(cols[len(cols)-2]), integer(cols[len(cols)-1])
		if begin != offset || end <= begin || end > len(units) {
			s.t.Fatalf("%s:%d: invalid UTF-16 range %d..%d at %d for %q", s.path, row.line, begin, end, offset, want)
		}
		value := string(utf16.Decode(units[begin:end]))
		if mixed {
			switch cols[0] {
			case "UFIELD_CATEGORY_LIST_SPAN":
				index := integer(cols[1])
				if index < 0 || index >= len(input) || input[index] != value {
					s.t.Fatalf("%s:%d: invalid input span %d for %q", s.path, row.line, index, value)
				}
				continue
			case "UFIELD_CATEGORY_LIST":
				cols = cols[1:]
			default:
				s.t.Fatalf("%s:%d: unknown list field category %s", s.path, row.line, cols[0])
			}
		}
		switch cols[0] {
		case "ULISTFMT_ELEMENT_FIELD":
			parts = append(parts, "element "+value)
		case "ULISTFMT_LITERAL_FIELD":
			parts = append(parts, "literal "+value)
		default:
			s.t.Fatalf("%s:%d: unknown list field %s", s.path, row.line, cols[0])
		}
		offset = end
	}
	if offset != len(units) {
		s.t.Fatalf("%s:%d: fields cover %d of %d UTF-16 units", s.path, result.line, offset, len(units))
	}
	s.checkList(result.line, group, "formatToParts", locale, typ, style, input, strings.Join(parts, "\n"))
}

func (s *listSuite) cListPositions(src intervalSource) {
	block := src.from("static void TestUListFmtToValue(void) {")
	for i, line := range block.lines {
		if strings.HasPrefix(line, "static void TestUListOpenStyled(") {
			break
		}
		if strings.HasPrefix(line, "const UChar* expectedString =") {
			row := block
			row.line += i
			row.lines = block.lines[i:]
			s.mixedListFields(row, "TestUListFmtToValue", "en", "conjunction", "long")
		}
	}
	s.mixedListFields(src.from("static void TestUListOpenStyled(void) {"), "TestUListOpenStyled", "en", "disjunction", "short")

	for _, group := range []string{"TestUList21871_A", "TestUList21871_B"} {
		block := src.from("static void " + group + "(void) {")
		input := listArray(block, "strs[] =")
		row := block.from("assertUEquals(")
		want := listStrings(s.t, row.lines[0])[1]
		s.checkList(row.line, group, "format", "en", "conjunction", "long", input, want)

		// ICU reports only the nonempty element's range. ECMA-402 also
		// returns an element part for the empty input string.
		index := func(label string) int {
			line := block.from(`assertIntEquals("TEST ` + label + `",`).lines[0]
			return integer(strings.TrimSpace(strings.Split(line, ",")[1]))
		}
		begin, end := index("beginIndex"), index("endIndex")
		units := utf16.Encode([]rune(want))
		if begin < 0 || end <= begin || end > len(units) {
			s.t.Fatalf("%s:%d: invalid element range %d..%d for %q", s.path, row.line, begin, end, want)
		}
		element := string(utf16.Decode(units[begin:end]))
		var parts string
		if input[0] == "" && end == len(units) {
			parts = fmt.Sprintf("element \nliteral %s\nelement %s", string(utf16.Decode(units[:begin])), element)
		} else if input[1] == "" && begin == 0 {
			parts = fmt.Sprintf("element %s\nliteral %s\nelement ", element, string(utf16.Decode(units[end:])))
		} else {
			s.t.Fatalf("%s:%d: unexpected empty-element positions for %q", s.path, row.line, input)
		}
		s.checkList(row.line, group, "formatToParts", "en", "conjunction", "long", input, parts)
	}
}
