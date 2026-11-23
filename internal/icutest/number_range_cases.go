// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strconv"
	"strings"
)

type numberRangePair struct {
	start, end float64
}

func javaRangeNumber(src intervalSource, literal string) float64 {
	src.t.Helper()
	n, err := strconv.ParseFloat(literal, 64)
	if err != nil {
		src.t.Fatalf("%s:%d: %v", src.path, src.line, err)
	}
	return n
}

func javaRangePairs(src intervalSource) []numberRangePair {
	var pairs []numberRangePair
	helper := src.from("static void assertFormatRange(")
	for _, line := range helper.lines {
		args, ok := strings.CutPrefix(line, "assertFormattedRangeEquals(message, l, ")
		if !ok {
			if len(pairs) > 0 {
				break
			}
			continue
		}
		cols := strings.Split(args, ", ")
		if len(cols) != 3 {
			src.t.Fatalf("%s: unexpected helper call %q", src.path, line)
		}
		pairs = append(pairs, numberRangePair{
			start: javaRangeNumber(helper, cols[0]),
			end:   javaRangeNumber(helper, cols[1]),
		})
	}
	if len(pairs) == 0 {
		src.t.Fatalf("%s: no number range helper inputs", src.path)
	}
	return pairs
}

var javaRangeSetting = regexp.MustCompile(`\.(?:unit|unitWidth|notation|precision|forSkeleton)\((?:MeasureUnit\.|UnitWidth\.|Notation\.|Precision\.)?(` + intervalLiteralPattern + `|\w+)`)

// javaRangeSkeleton maps NumberFormatter settings in assertFormatRange calls to ICU skeleton tokens.
func javaRangeSkeleton(src intervalSource, settings string) string {
	var tokens []string
	for _, match := range javaRangeSetting.FindAllStringSubmatch(settings, -1) {
		var token string
		switch value := match[1]; value {
		case "CHF", "GBP", "PTE", "USD":
			token = "currency/" + value
		case "METER", "FAHRENHEIT":
			token = strings.ToLower(value)
		case "FULL_NAME":
			token = "unit-width-full-name"
		case "ISO_CODE":
			token = "unit-width-iso-code"
		case "compactLong":
			token = "compact-long"
		case "compactShort":
			token = "compact-short"
		case "scientific":
			token = value
		case "integer":
			token = "precision-integer"
		default:
			if !strings.HasPrefix(value, `"`) {
				src.t.Fatalf("%s:%d: unknown number formatter setting %s", src.path, src.line, value)
			}
			token = intervalString(src.t, value)
		}
		tokens = append(tokens, token)
	}
	return strings.Join(tokens, " ")
}

func readRangeMatrixCall(block intervalSource) (group, locale, settings string, rows []intervalRow) {
	group = intervalString(block.t, block.lines[1])
	for j, arg := range block.lines[2:] {
		switch {
		case strings.HasPrefix(arg, "new ULocale("), strings.HasPrefix(arg, "ULocale.forLanguageTag("):
			locale = intervalString(block.t, arg)
		case strings.HasPrefix(arg, `"`):
			rows = append(rows, intervalRow{
				line: block.line + j + 2,
				text: intervalString(block.t, arg),
			})
		default:
			settings += arg
		}
		if strings.HasSuffix(arg, ");") {
			break
		}
	}
	return group, locale, settings, rows
}

type rangeMatrixKey struct {
	locale, skeleton string
	numberRangePair
}

func (s *numberRangeSuite) javaRangeMatrix(src intervalSource) {
	pairs := javaRangePairs(src)
	defaults := map[rangeMatrixKey]string{}
	for i, line := range src.lines {
		if line != "assertFormatRange(" {
			continue
		}
		block := src
		block.line += i
		block.lines = src.lines[i:]
		group, locale, settings, rows := readRangeMatrixCall(block)
		if locale == "" || len(rows) != len(pairs) {
			s.t.Fatalf("%s:%d: %s: locale %q, %d results for %d inputs", src.path, block.line, group, locale, len(rows), len(pairs))
		}
		if strings.Contains(settings, ".numberFormatterFirst(") {
			s.excluded["different endpoint formatters"] += len(rows)
			continue
		}
		if strings.Contains(settings, "MeasureUnit.KELVIN") {
			s.excluded["kelvin is not a sanctioned unit"] += len(rows)
			continue
		}
		skeleton := javaRangeSkeleton(block, settings)
		if base, ns, ok := strings.Cut(locale, "@numbers="); ok {
			locale = base + "-u-nu-" + ns
		}
		collapse := strings.Contains(settings, ".collapse(")
		// With only a scientific exponent, AUTO and NONE both repeat the exponent.
		if skeleton == "scientific" && strings.Contains(settings, "RangeCollapse.NONE") {
			collapse = false
		}
		fallback := strings.Contains(settings, ".identityFallback(") && !strings.Contains(settings, ".identityFallback(null)")
		for j, row := range rows {
			pair := pairs[j]
			key := rangeMatrixKey{
				locale:          locale,
				skeleton:        skeleton,
				numberRangePair: pair,
			}
			if want, ok := defaults[key]; ok {
				switch {
				case want == row.text:
					s.excluded["repeats mapped formatter expectation"]++
				case collapse:
					s.excluded["explicit ICU collapse changes the range"]++
				case fallback:
					s.excluded["explicit ICU identity fallback changes the range"]++
				default:
					s.t.Fatalf("%s:%d: conflicting expectations for %v", src.path, row.line, key)
				}
				continue
			}
			// This compact formatter occurs only with APPROXIMATELY_OR_SINGLE_VALUE.
			// Its output differs from the default only for identical inputs.
			if fallback && strings.Contains(settings, "APPROXIMATELY_OR_SINGLE_VALUE") && pair.start == pair.end {
				s.excluded["explicit ICU identity fallback changes the range"]++
				continue
			}
			if collapse || (fallback && !strings.Contains(settings, "APPROXIMATELY_OR_SINGLE_VALUE")) {
				s.t.Fatalf("%s:%d: missing default reference for %s", src.path, row.line, group)
			}
			defaults[key] = row.text
			s.checkRange(row.line, group, "formatRange", locale, skeleton, pair.start, pair.end, row.text)
			// The helper's second pair rounds to one displayed value with basic precision.
			if group == "Basic" && j == 1 {
				s.checkRange(row.line, group, "sources", locale, skeleton, pair.start, pair.end, "shared")
			}
			if j == 0 {
				switch group {
				case "Default collapse on currency":
					s.checkRange(row.line, group, "currency", locale, skeleton, pair.start, pair.end, "startRange endRange")
				case "Default collapse on currency ISO code":
					s.checkRange(row.line, group, "currency", locale, skeleton, pair.start, pair.end, "shared")
				}
			}
		}
	}
}

func (s *numberRangeSuite) javaNumberRanges(src intervalSource) {
	s.javaRangeMatrix(src)
	plurals := src.from("void testPlurals()")
	for _, row := range plurals.table("new Object[][] {") {
		col := row.columns(s.t, 3)
		if col[0] == col[1] {
			s.excluded["equal Slovenian endpoints with forced range"]++
			continue
		}
		s.checkRange(row.line, "testPlurals", "formatRange", "sl", "currency/GBP unit-width-full-name precision-integer", col[0], col[1], intervalString(s.t, col[2]))
	}
	s.excluded["different endpoint formatters"]++ // test22288_DifferentStartEndSettings
	s.excluded["equal endpoints with forced range in locale()"] += 2

	s.javaDirectRanges(src.from("void test21358_SignPosition()"))
}

func (s *numberRangeSuite) javaDirectRanges(src intervalSource) {
	// The sign and numbering-system regressions each specify their endpoints in a direct call.
	var locale, skeleton string
	for i, line := range src.lines {
		switch {
		case strings.Contains(line, "NumberRangeFormatter.withLocale(ULocale.forLanguageTag("):
			locale = intervalString(s.t, line)
			skeleton = ""
		case strings.Contains(line, `forSkeleton("%")`):
			skeleton = "percent"
		case strings.Contains(line, ".### rounding-mode-half-up"), strings.Contains(line, `forSkeleton("numbering-system/arab")`):
			skeleton = intervalString(s.t, line)
		case strings.HasPrefix(line, "String actual = lnrf.formatRange("):
			args := strings.TrimSuffix(strings.TrimPrefix(line, "String actual = lnrf.formatRange("), ").toString();")
			start, end, ok := strings.Cut(args, ", ")
			if !ok || locale == "" {
				s.t.Fatalf("%s:%d: missing locale or endpoints", src.path, src.line+i)
			}
			assert := intervalLiteral.FindAllString(src.lines[i+1], -1)
			if len(assert) != 2 {
				s.t.Fatalf("%s:%d: missing range assertion", src.path, src.line+i+1)
			}
			group, want := intervalString(s.t, assert[0]), intervalString(s.t, assert[1])
			s.checkRange(src.line+i+1, group, "formatRange", locale, skeleton, start, end, want)
			if strings.HasPrefix(start, "-") || strings.HasPrefix(end, "-") {
				source := "endRange"
				if strings.HasPrefix(start, "-") {
					source = "startRange"
				}
				s.checkRange(src.line+i+1, group, "minusSign", locale, skeleton, start, end, source)
			}
		}
	}
}

func (s *numberRangeSuite) pluralFormatting(src intervalSource) {
	rows := src.from("void TestFormatting()").table("Object[][] tests =")
	var entry string
	for _, row := range rows {
		entry += " " + row.text
		if !strings.HasSuffix(row.text, "}") && !strings.HasSuffix(row.text, "},") {
			continue
		}
		row.text, entry = entry, ""
		col := row.columns(s.t, 6)
		locale := map[string]string{
			"ULocale.ENGLISH": "en",
			"ULocale.FRENCH":  "fr",
			"ULocale.FRANCE":  "fr-FR",
			"ULocale.CHINESE": "zh",
		}[col[2]]
		if locale == "" {
			s.t.Fatalf("%s:%d: unknown locale %s", src.path, row.line, col[2])
		}
		var skeleton string
		if unit, ok := strings.CutPrefix(col[4], "MeasureUnit."); ok {
			skeleton = strings.ToLower(unit)
		} else {
			skeleton = "currency/" + intervalString(s.t, col[4])
		}
		switch col[3] {
		case "UnitWidth.FULL_NAME":
			skeleton += " unit-width-full-name"
		case "UnitWidth.NARROW":
			skeleton += " unit-width-narrow"
		case "UnitWidth.SHORT":
		default:
			s.t.Fatalf("%s:%d: unknown unit width %s", src.path, row.line, col[3])
		}
		s.checkRange(row.line, "TestFormatting", "formatRange", locale, skeleton, javaRangeNumber(src, col[0]), javaRangeNumber(src, col[1]), intervalString(s.t, col[5]))
	}
	if entry != "" {
		s.t.Fatalf("%s: incomplete TestFormatting row %q", src.path, entry)
	}
}
