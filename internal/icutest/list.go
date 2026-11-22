// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/hajimehoshi/ekuma"
)

type listSuite struct {
	*formatFixture
	format ekuma.Value
}

func runListFormats(t *testing.T, root string) {
	for _, fixture := range []struct {
		path string
		run  func(*listSuite, intervalSource)
	}{
		{
			path: "icu4c/source/test/intltest/listformattertest.cpp",
			run:  (*listSuite).cppLists,
		},
		{
			path: "icu4c/source/test/cintltst/ulistfmttest.c",
			run:  (*listSuite).cLists,
		},
		{
			path: "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format/ListFormatterTest.java",
			run:  (*listSuite).javaLists,
		},
	} {
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			path := filepath.Join(root, fixture.path)
			f := newFormatFixture(t, path)
			var vm ekuma.VM
			// The fallback cases in both ICU readers use the process default.
			if err := vm.SetLocale("en-US"); err != nil {
				t.Fatal(err)
			}
			s := listSuite{
				formatFixture: f,
				format: f.functionIn(&vm, `(op, locale, type, style, input) => {
					const fmt = new Intl.ListFormat(locale || undefined, {type, style});
					if (op === "format") return fmt.format(input);
					const parts = fmt.formatToParts(input);
					if (op === "elementCount") return String(parts.filter(p => p.type === "element").length);
					return parts.map(p => p.type + " " + p.value).join("\n");
				}`),
			}
			fixture.run(&s, readIntervalSource(t, path))
		})
	}
}

func (s *listSuite) checkList(line int, group, op, locale, typ, style string, input []string, want string) {
	s.t.Helper()
	args := make([]any, len(input))
	for i, str := range input {
		args[i] = str
	}
	locale = strings.ReplaceAll(locale, "_", "-")
	s.executed++
	got, err := s.format.Invoke(s.t.Context(), op, locale, typ, style, args)
	if err != nil {
		s.t.Errorf("%s:%d: %s %s(locale=%q type=%s style=%s input=%q): got error %v, want %q", s.path, line, group, op, locale, typ, style, input, err, want)
	} else if actual := got.String(); actual != want {
		s.t.Errorf("%s:%d: %s %s(locale=%q type=%s style=%s input=%q): got %q, want %q", s.path, line, group, op, locale, typ, style, input, actual, want)
	}
}

func listStrings(t *testing.T, expr string) []string {
	t.Helper()
	var values []string
	for _, token := range intervalLiteral.FindAllString(expr, -1) {
		values = append(values, intervalString(t, token))
	}
	return values
}

// listArray reads a string initializer that can start and end on the same line.
func listArray(src intervalSource, marker string) []string {
	block := src.from(marker)
	var values []string
	for _, line := range block.lines {
		row, _, end := strings.Cut(line, "};")
		values = append(values, listStrings(src.t, row)...)
		if end {
			return values
		}
	}
	src.t.Fatalf("%s:%d: unterminated %s", src.path, block.line, marker)
	return nil
}

var listTypes = map[string]string{
	"ULISTFMT_TYPE_AND":   "conjunction",
	"ULISTFMT_TYPE_OR":    "disjunction",
	"ULISTFMT_TYPE_UNITS": "unit",
}

var listStyles = map[string]string{
	"ULISTFMT_WIDTH_WIDE":   "long",
	"ULISTFMT_WIDTH_SHORT":  "short",
	"ULISTFMT_WIDTH_NARROW": "narrow",
}

func (s *listSuite) cppLists(src intervalSource) {
	s.cppFourCases(src)

	block := src.from("::Test9946() {")
	row := block.from("UnicodeString expected(")
	s.checkList(row.line, "Test9946", "format", "en", "conjunction", "long", listArray(block, "data[3] ="), intervalString(s.t, row.lines[0]))

	block = src.from("::TestDifferentStyles() {")
	input := listArray(block, "input[4] =")
	for i, line := range block.lines {
		if strings.HasPrefix(line, "DoTheRealListStyleTesting(locale,") {
			cols := strings.SplitN(line, ", ", 6)
			s.checkList(block.line+i, "TestDifferentStyles", "format", "fr", listTypes[cols[3]], listStyles[cols[4]], input, intervalString(s.t, cols[5]))
		}
	}

	block = src.from("::TestCreateStyled() {")
	input = listArray(block, "inputs3[] =")
	for _, row := range block.table("} cases[] =") {
		cols := row.columns(s.t, 6)
		for n := 1; n <= len(input); n++ {
			s.checkList(row.line, "TestCreateStyled", "format", intervalString(s.t, cols[0]), listTypes[cols[1]], listStyles[cols[2]], input[:n], intervalString(s.t, cols[6-n]))
		}
	}

	s.cppContextual(src)
	s.cppListPositions(src)
	s.excluded["ICU input-too-long error"]++
}

func (s *listSuite) cppFourCases(src intervalSource) {
	var input []string
	names := map[string]string{}
	ctor := src.from("ListFormatterTest::ListFormatterTest()")
	for _, key := range []string{"one", "two", "three", "four"} {
		line := ctor.from(key + "(\"").lines[0]
		_, expr, _ := strings.Cut(line, key+"(")
		names[key] = intervalString(s.t, intervalLiteral.FindString(expr))
		input = append(input, names[key])
	}

	for _, group := range []string{"TestEnglish", "TestEnglishUS", "TestEnglishGB", "TestNynorsk", "TestChineseTradHK", "TestRussian", "TestMalayalam", "TestZulu"} {
		block := src.from("::" + group + "() {")
		locale := listStrings(s.t, block.from("CheckFourCases(").lines[0])[0]
		for _, line := range block.lines[1:] {
			if strings.Contains(line, "results[4] =") {
				break
			}
			if name, expr, ok := strings.Cut(line, " = UnicodeString("); ok {
				names[strings.TrimPrefix(name, "UnicodeString ")] = intervalString(s.t, expr)
			}
		}
		for i, row := range block.table("results[4] =") {
			var want strings.Builder
			for token := range strings.SplitSeq(strings.TrimSuffix(row.text, ","), " + ") {
				if strings.HasPrefix(token, "\"") {
					want.WriteString(intervalString(s.t, token))
				} else if v, ok := names[token]; ok {
					want.WriteString(v)
				} else {
					s.t.Fatalf("%s:%d: unknown operand %s", src.path, row.line, token)
				}
			}
			s.checkList(row.line, group, "format", locale, "conjunction", "long", input[:i+1], want.String())
			if group == "TestEnglishUS" {
				// TestBogus takes its expected strings from the default formatter.
				bogus := src.from("::TestBogus() {").from("CheckFourCases(")
				s.checkList(bogus.line, "TestBogus", "format", listStrings(s.t, bogus.lines[0])[0], "conjunction", "long", input[:i+1], want.String())
			}
		}
	}
	s.excluded["ICU root locale"] += len(src.from("::TestRoot() {").table("results[4] ="))
	s.excluded["custom ICU list patterns"] += len(src.from("::TestOutOfOrderPatterns() {").table("results[4] ="))
}

func (s *listSuite) cppContextual(src intervalSource) {
	block := src.from("::TestContextual() {")
	locales := map[string][]string{
		"es": listArray(block, " es ="),
		"he": listArray(block, " he ="),
	}
	var row intervalRow
	for _, fragment := range block.table("} cases[] =") {
		if row.text == "" {
			row = fragment
		} else {
			row.text += " " + fragment.text
		}
		if !strings.HasSuffix(row.text, "},") {
			continue
		}
		cols := row.columns(s.t, 6)
		input := listStrings(s.t, strings.Join(cols[3:], ","))
		group, ok := locales[cols[0]]
		if !ok {
			s.t.Fatalf("%s:%d: unknown contextual locale group %s", src.path, row.line, cols[0])
		}
		for _, locale := range group {
			for _, style := range []string{"long", "short", "narrow"} {
				s.checkList(row.line, "TestContextual", "format", locale, listTypes[cols[1]], style, input, intervalString(s.t, cols[2]))
			}
		}
		row = intervalRow{}
	}
	if row.text != "" {
		s.t.Fatalf("%s:%d: incomplete contextual case", src.path, row.line)
	}
}

func (s *listSuite) javaLists(src intervalSource) {
	input := listStrings(s.t, src.from(`assertEquals("5", strings[5]`).lines[0])[1:]
	for _, tc := range []struct {
		table   string
		locales []string
	}{
		{
			table:   "EnglishTestData",
			locales: []string{"en", "en-US", "", "xxx"},
		},
		{
			table:   "EnglishGBTestData",
			locales: []string{"en-GB"},
		},
		{
			table:   "ChineseTradHKTestData",
			locales: []string{"zh-Hant-HK"},
		},
		{
			table:   "JapaneseTestData",
			locales: []string{"ja"},
		},
	} {
		block := src.from("String[] " + tc.table + " =")
		results := listArray(block, tc.table)
		if len(results) != len(input)+1 {
			s.t.Fatalf("%s:%d: got %d results for lists of up to %d items", src.path, block.line, len(results), len(input))
		}
		for n, want := range results {
			for _, locale := range tc.locales {
				s.checkList(block.line, tc.table, "format", locale, "conjunction", "long", input[:n], want)
			}
		}
	}
	s.excluded["ICU root locale"] += len(listArray(src, "String[] RootTestData ="))
	for _, table := range []string{"HardcodedTestData", "outOfOrderTestData"} {
		s.excluded["custom ICU list patterns"] += len(listArray(src, "String[] "+table+" ="))
	}
}

func (s *listSuite) cLists(src intervalSource) {
	var input []string
	for i := range 5 {
		line := src.from(fmt.Sprintf("UChar str%d[] =", i)).lines[0]
		_, initializer, _ := strings.Cut(line, "{")
		values, _, _ := strings.Cut(initializer, "}")
		var units []uint16
		for token := range strings.SplitSeq(values, ",") {
			n, err := strconv.ParseUint(strings.TrimSpace(token), 0, 16)
			if err != nil {
				s.t.Fatal(err)
			}
			if n == 0 {
				break
			}
			units = append(units, uint16(n))
		}
		input = append(input, string(utf16.Decode(units)))
	}
	for _, row := range src.table("listFmtTestEntries[] =") {
		if strings.HasPrefix(row.text, "/*") || strings.HasPrefix(row.text, "{ NULL") {
			continue
		}
		cols := row.columns(s.t, 3)
		s.checkList(row.line, "TestUListFmt", "format", intervalString(s.t, cols[0]), "conjunction", "long", input[:integer(cols[1])], intervalString(s.t, cols[2]))
		s.excluded["repeated C string length conventions"] += 2
		s.excluded["null C input or output buffer"] += 2
	}
	s.cListPositions(src)
}
