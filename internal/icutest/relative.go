// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type relativeSuite struct {
	*formatFixture
	format, parts ekuma.Value
}

func runRelativeTimeFormats(t *testing.T, root string) {
	for _, fixture := range []struct {
		path string
		run  func(*relativeSuite, intervalSource)
	}{
		{
			path: "icu4c/source/test/intltest/reldatefmttest.cpp",
			run:  (*relativeSuite).cppRelative,
		},
		{
			path: "icu4c/source/test/cintltst/crelativedateformattest.c",
			run:  (*relativeSuite).cRelative,
		},
		{
			path: "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format/RelativeDateTimeFormatterTest.java",
			run:  (*relativeSuite).javaRelative,
		},
	} {
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			path := filepath.Join(root, fixture.path)
			f := newFormatFixture(t, path)
			s := relativeSuite{
				formatFixture: f,
				format: f.function(`(locale, style, numeric, offset, unit) =>
					new Intl.RelativeTimeFormat(locale, {style, numeric}).format(offset, unit)`),
				parts: f.function(`(locale, style, numeric, offset, unit) =>
					new Intl.RelativeTimeFormat(locale, {style, numeric}).formatToParts(offset, unit)
						.map(p => (Object.hasOwn(p, "unit") ? p.type + "[" + p.unit + "]" : p.type) + " " + p.value)
						.join("\n")`),
			}
			fixture.run(&s, readIntervalSource(t, path))
		})
	}
}

func (s *relativeSuite) offset(line int, token string) float64 {
	n, err := strconv.ParseFloat(token, 64)
	if err != nil {
		s.t.Fatalf("%s:%d: invalid relative offset %q", s.path, line, token)
	}
	return n
}

func weekdayUnit(unit string) bool {
	switch unit {
	case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return true
	}
	return false
}

func (s *relativeSuite) quantity(line int, locale, style, magnitude, direction, unit, want string) {
	value := s.offset(line, magnitude)
	switch direction {
	case "LAST":
		value = -value // Preserve past zero.
	case "NEXT":
	default:
		s.t.Fatalf("%s:%d: unexpected quantity direction %s", s.path, line, direction)
	}
	s.check(line, "format", s.format, want, locale, style, "always", value, unit)
}

func (s *relativeSuite) cppRelative(src intervalSource) {
	quantity := func(table, locale, style string) {
		for _, row := range src.table(table + "[] =") {
			c := row.columns(s.t, 4)
			unit := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(c[2], "UDAT_RELATIVE_"), "S"))
			s.quantity(row.line, locale, style, c[0], strings.TrimPrefix(c[1], "UDAT_DIRECTION_"), unit, intervalString(s.t, c[3]))
		}
	}
	quantity("kEnglish", "en", "long")
	quantity("kEnglishShort", "en", "short")
	quantity("kEnglishNarrow", "en", "narrow")
	quantity("kSerbian", "sr", "long")
	quantity("kSerbianNarrow", "sr", "narrow")
	quantity("kAfrikaans", "af", "long")

	named := func(table, locale, style string) {
		for _, row := range src.table(table + "[] =") {
			c := row.columns(s.t, 3)
			direction := strings.TrimPrefix(c[0], "UDAT_DIRECTION_")
			unit := strings.ToLower(strings.TrimPrefix(c[1], "UDAT_ABSOLUTE_"))
			want := intervalString(s.t, c[2])
			switch {
			case weekdayUnit(unit):
				s.excluded["weekday unit"]++
			case want == "":
				s.excluded["missing standalone ICU name"]++
			case unit == "now" && direction == "PLAIN":
				s.check(row.line, "format", s.format, want, locale, style, "auto", 0, "second")
			case direction == "PLAIN":
				s.excluded["standalone unit name"]++
			default:
				offset, ok := map[string]int{"LAST_2": -2, "LAST": -1, "THIS": 0, "NEXT": 1, "NEXT_2": 2}[direction]
				if !ok {
					s.t.Fatalf("%s:%d: unexpected absolute direction %s", s.path, row.line, direction)
				}
				s.check(row.line, "format", s.format, want, locale, style, "auto", offset, unit)
			}
		}
	}
	named("kEnglishNoQuantity", "en", "long")
	named("kEnglishNoQuantityShort", "en", "short")
	named("kEnglishNoQuantityNarrow", "en", "narrow")
	named("kSpanishNoQuantity", "es", "long")
	named("kAfrikaansNoQuantity", "af", "long")

	signed := func(table, locale, numeric string) {
		for _, row := range src.table(table + "[] =") {
			c := row.columns(s.t, 3)
			unit := strings.ToLower(strings.TrimPrefix(c[1], "UDAT_REL_UNIT_"))
			if weekdayUnit(unit) {
				s.excluded["weekday unit"]++
				continue
			}
			s.check(row.line, "format", s.format, intervalString(s.t, c[2]), locale, "long", numeric, s.offset(row.line, c[0]), unit)
		}
	}
	signed("kEnglishFormat", "en", "auto")
	signed("kEnglishFormatNumeric", "en", "always")
	signed("kAfrikaansFormatNumeric", "af", "always")
	s.excluded["sentence capitalization"] += len(src.table("kEnglishCaps[] =")) + len(src.table("kEnglishNoQuantityCaps[] ="))
	s.excluded["custom fraction digits"] += len(src.table("kEnglishDecimal[] ="))

	block := src.from("::TestSidewaysDataLoading() {")
	check := func(row intervalSource, delta int, numeric, unit string, offset int) {
		line := row.line + delta
		s.check(line, "format", s.format, intervalString(s.t, block.lines[line-block.line]), "en-GB", "narrow", numeric, offset, unit)
	}
	check(block.from("fmt.format(3.0, UDAT_DIRECTION_NEXT, UDAT_RELATIVE_MONTHS"), 1, "always", "month", 3)
	check(block.from("fmt.format(3.0, UDAT_DIRECTION_LAST, UDAT_RELATIVE_DAYS"), 1, "always", "day", -3)
	check(block.from("fmt.format(UDAT_DIRECTION_NEXT, UDAT_ABSOLUTE_YEAR"), -1, "auto", "year", 1)
	s.excluded["independent quantity sign and direction"]++
	s.cppRelativeFields(src.from("::TestFields() {"))
}

func (s *relativeSuite) javaRelative(src intervalSource) {
	// ICU4J adds quarters to the legacy quantity API.
	quarters := func(suffix, style string) {
		block := src.from("void TestRelativeDateWithQuantity" + suffix + "()")
		for _, row := range block.table("Object[][] data =") {
			c := row.columns(s.t, 4)
			if c[2] == "RelativeUnit.QUARTERS" {
				s.quantity(row.line, "en-US", style, c[0], strings.TrimPrefix(c[1], "Direction."), "quarter", intervalString(s.t, c[3]))
			}
		}
	}
	quarters("", "long")
	quarters("Short", "short")
	quarters("Narrow", "narrow")

	block := src.from("void TestSidewaysDataLoading()")
	for i, line := range block.lines {
		if line == "}" {
			break
		}
		_, args, ok := strings.Cut(line, " = fmt.format(")
		if !ok {
			continue
		}
		row := intervalRow{
			line: block.line + i,
			text: strings.TrimSuffix(args, ");"),
		}
		c := row.columns(s.t, 3)
		unit := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(c[2], "RelativeUnit."), "S"))
		want := listStrings(s.t, block.lines[i+1])[1]
		s.quantity(row.line, "en-GB", "narrow", c[0], strings.TrimPrefix(c[1], "Direction."), unit, want)
	}
	block = src.from("void TestJavaLocale()")
	amount := block.from("double amount = ")
	_, token, _ := strings.Cut(amount.lines[0], " = ")
	row := block.from("assertEquals(")
	s.check(row.line, "format", s.format, listStrings(s.t, row.lines[0])[1], "en-US", "long", "always", -s.offset(amount.line, strings.TrimSuffix(token, "d;")), "second")
}
