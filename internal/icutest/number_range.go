// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type numberRangeSuite struct {
	*formatFixture
	format ekuma.Value
}

func runNumberRanges(t *testing.T, root string) {
	const java = "icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/"
	for _, file := range []struct {
		path string
		run  func(*numberRangeSuite, intervalSource)
	}{
		{
			path: java + "number/NumberRangeFormatterTest.java",
			run:  (*numberRangeSuite).javaNumberRanges,
		},
		{
			path: java + "format/PluralRangesTest.java",
			run:  (*numberRangeSuite).pluralFormatting,
		},
		{
			path: "icu4c/source/test/intltest/numbertest_range.cpp",
			run:  (*numberRangeSuite).cppNumberRanges,
		},
		{
			path: "icu4c/source/test/cintltst/unumberrangeformattertst.c",
			run:  (*numberRangeSuite).cNumberRanges,
		},
	} {
		name := filepath.Base(file.path)
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, file.path)
			f := newFormatFixture(t, path)
			s := &numberRangeSuite{
				formatFixture: f,
				format: f.function(`(op, locale, options, start, end) => {
					const fmt = new Intl.NumberFormat(locale, options);
					if (op === "formatRange") return fmt.formatRange(start, end);
					const parts = fmt.formatRangeToParts(start, end);
					if (op === "sources") return [...new Set(parts.map(p => p.source))].join(" ");
					if (op === "minusSign" || op === "currency") {
						return parts.filter(p => p.type === op).map(p => p.source).join(" ");
					}
					return parts.map(p => [p.source, p.type, p.value].join(" ")).join("\n");
				}`),
			}
			file.run(s, readIntervalSource(t, path))
		})
	}
}

func (s *numberRangeSuite) checkRange(line int, group, op, locale, skeleton string, start, end any, want string) {
	s.t.Helper()
	options, divisor, reason := numberSkeletonOptions(strings.Fields(skeleton))
	if reason != "" {
		s.t.Fatalf("%s:%d: %s: %s", s.path, line, group, reason)
	}
	if divisor != 1 {
		start = scaleDecimal(fmt.Sprint(start), 1, int64(divisor))
		end = scaleDecimal(fmt.Sprint(end), 1, int64(divisor))
	}
	s.check(line, op, s.format, want, op, locale, options, start, end, group)
}

// The C and C++ assertions place the expected string immediately after the message.
func (s *numberRangeSuite) assertion(src intervalSource, group, locale, skeleton string, start, end any) {
	row := src.from(`"` + group + `"`)
	literals := intervalLiteral.FindAllString(row.lines[0], -1)
	if len(literals) < 2 {
		row.line++
		literals = intervalLiteral.FindAllString(row.lines[1], -1)
	} else {
		literals = literals[1:]
	}
	if len(literals) == 0 {
		s.t.Fatalf("%s:%d: missing expected string for %s", src.path, row.line, group)
	}
	s.checkRange(row.line, group, "formatRange", locale, skeleton, start, end, intervalString(s.t, literals[0]))
}

func (s *numberRangeSuite) fieldTable(block intervalSource, group, locale, skeleton string, start, end any, row intervalSource) {
	want := intervalString(s.t, row.lines[0])
	s.checkRange(row.line, group, "formatRange", locale, skeleton, start, end, want)
	parts := rangeParts(s.t, want, block.table("expectedFieldPositions[] ="))
	s.checkRange(row.line, group, "formatRangeToParts", locale, skeleton, start, end, parts)
}

func (s *numberRangeSuite) cppNumberRanges(src intervalSource) {
	infinity := src.from("::testNaNInfinity() {")
	// The first three use doubles. The fourth uses decimal strings.
	s.assertion(infinity, "0 - inf", "en", "", math.Inf(-1), 0)
	s.assertion(infinity, "-inf - 0", "en", "", 0, math.Inf(1))
	s.assertion(infinity, "-inf - inf", "en", "", math.Inf(-1), math.Inf(1))
	s.assertion(infinity, "1000 - inf", "en", "", "1000", "Infinity")
	s.excluded["NaN endpoint raises RangeError in JS"] += 4

	fields := src.from("::testFieldPositions() {")
	for _, tc := range []struct {
		group, skeleton string
		start, end      int
	}{
		{
			group:    "Field position test 1",
			skeleton: "meter compact-short",
			start:    3000,
			end:      5000,
		},
		{
			group: "Field position test 2",
			start: 87654321,
			end:   98765432,
		},
		{
			group: "Field position with approximately sign",
			start: -100,
			end:   -100,
		},
	} {
		block := fields.from(`"` + tc.group + `"`)
		s.fieldTable(block, tc.group, "en-US", tc.skeleton, tc.start, tc.end, block.from("expectedString ="))
	}

	copyMove := src.from("::testCopyMove() {")
	s.assertion(copyMove, "Currency behavior", "fr-FR", "currency/USD", 1, 5)
	s.assertion(copyMove, "FormattedNumberRange move assignment", "fr-FR", "currency/USD", 3, 6)
	s.assertion(src, "object API test, const lvalue", "en", "", 5, 7)
	s.assertion(src, "Range: Formatted string should be as expected", "en", "currency/USD", 1, 5)
	s.assertion(src, "Identity: Formatted string should be as expected", "en", "currency/USD", 3, 3)

	// ICU counts number fields and endpoint spans. These two rows have no nested
	// fields and each endpoint occupies a single span.
	state := s.function(`(() => {
		const fmt = new Intl.NumberFormat("en", {roundingMode: "halfEven", maximumFractionDigits: 6});
		return (start, end) => {
			const parts = fmt.formatRangeToParts(start, end);
			const fields = parts.filter(p => p.type !== "literal");
			const spans = new Set(parts.map(p => p.source).filter(source => source !== "shared"));
			return String(fields.length + spans.size);
		};
	})()`)
	for _, row := range src.table("} ranges[] =") {
		col := row.columns(s.t, 4)
		s.checkRange(row.line, "test21683_StateLeak", "formatRange", "en", "", col[0], col[1], intervalString(s.t, col[2]))
		s.check(row.line, "test21683_StateLeak field count (en, decimal)", state, col[3], col[0], col[1])
	}
}

func (s *numberRangeSuite) cNumberRanges(src intervalSource) {
	for _, tc := range []struct {
		group, skeleton string
	}{
		{
			group:    "TestExampleCode",
			skeleton: "currency/USD precision-integer",
		},
		{
			group:    "TestGetDecimalNumbers",
			skeleton: "currency/USD",
		},
	} {
		block := src.from("static void " + tc.group + "(void) {")
		s.assertion(block, "Should produce expected string result", "en-US", tc.skeleton, 3, 5)
	}
	block := src.from("static void TestFormattedValue(void) {")
	inputs := intervalLiteral.FindAllString(block.from("unumrf_formatDecimalRange(").lines[0], -1)
	if len(inputs) != 2 {
		s.t.Fatalf("%s: expected two decimal string endpoints", src.path)
	}
	result := block.from("checkMixedFormattedValue(").from("fv,")
	result = result.from(`u"`)
	s.fieldTable(block, "TestFormattedValue", "en-US", "compact-short", intervalString(s.t, inputs[0]), intervalString(s.t, inputs[1]), result)
}
