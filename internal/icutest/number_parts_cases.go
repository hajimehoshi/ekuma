// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import "strings"

func (s *numberPartsSuite) cFormatter(src intervalSource) {
	accounting := src.from("void TestSkeletonFormatToString(void) {")
	settings := listStrings(s.t, accounting.from(`u"`).lines[0])
	var input any
	for i, line := range accounting.lines[1:] {
		if strings.HasPrefix(line, "unumf_closeResult(") {
			break
		}
		if strings.HasPrefix(line, "unumf_format") {
			args := strings.Split(line, ",")
			if strings.HasPrefix(line, "unumf_formatDecimal(") {
				input = intervalString(s.t, args[1])
			} else {
				input = fixtureFloat(accounting, strings.TrimSpace(args[1]))
			}
		}
		if strings.HasPrefix(line, "assertUEquals(") {
			s.checkParts(accounting.line+i+1, settings[1], settings[0], input, listStrings(s.t, line)[1], nil)
			input = nil
		}
	}

	fields := src.from("void TestSkeletonFormatToFields(void) {")
	call := fields.from("unumf_formatInt(")
	// This test puts the expected text in a comment on the format call.
	var want string
	fixtureLines(s.t, s.path, func(line int, raw string) {
		if line == call.line {
			_, comment, _ := strings.Cut(raw, "//")
			want = intervalString(s.t, comment)
		}
	})
	checkInt := func(block intervalSource, table, want string) {
		settings := listStrings(s.t, block.from(`u"`).lines[0])
		call := block.from("unumf_formatInt(")
		arg := strings.TrimSpace(strings.Split(call.lines[0], ",")[1])
		s.checkParts(call.line, settings[1], settings[0], fixtureFloat(call, strings.TrimSuffix(arg, "L")), want, block.table(table))
	}
	checkInt(fields, "expectedFields[] =", want)
	value := src.from("void TestFormattedValue(void) {")
	checkInt(value, "expectedFieldPositions[] =", intervalString(s.t, value.from("checkFormattedValue(").from(`u"`).lines[0]))
}

func (s *numberPartsSuite) cppFormatter(src intervalSource) {
	// The C++ and Java fieldPositionLogic and fieldPositionCoverage tables
	// mirror one another. The first skeleton in each call uses the long form.
	block := src.from("::fieldPositionLogic() {")
	for i, line := range block.lines {
		if strings.HasPrefix(line, "void NumberFormatterApiTest::toFormat(") {
			break
		}
		if !strings.HasSuffix(line, "= assertFormatSingle(") {
			continue
		}
		call := intervalSource{
			t:     s.t,
			path:  s.path,
			lines: block.lines[i:],
			line:  block.line + i,
		}
		skeleton := intervalString(s.t, call.lines[2])
		s.excluded["matching ICU4J field-position case"]++
		switch skeleton {
		case "measure-unit/temperature-kelvin":
			s.excluded["kelvin is not an ECMA-402 sanctioned unit"]++
			continue
		case "measure-unit/length-meter per-measure-unit/duration-second unit-width-full-name":
			s.excluded["composed and built-in meter-per-second have identical parts"]++
			continue
		}
		s.cppSingle(call, skeleton, call.table("expectedFieldPositions[] ="))
	}

	for _, name := range []string{
		"Currency Difference between Narrow and Short (Narrow Version)",
		"Currency Difference between Narrow and Short (Short Version)",
		"Currency spacing in suffix (ICU-20954)",
		"Sign position on negative number with pattern spacing",
		"Sign position on negative number with currency spacing",
		"Sign Accounting Negative Iso Code",
		"Sign Accounting Negative Full Name",
	} {
		call := src.from(`u"` + name + `"`)
		s.cppSingle(call, intervalString(s.t, call.lines[1]), nil)
	}

	mathDigits := src.from(`u"Math Numbering System with French Data"`)
	row := mathDigits.from("Locale::getFrench(),")
	// assertFormatDescending starts at 87650.
	s.checkParts(row.line+1, "fr", intervalString(s.t, mathDigits.lines[1]), 87650, intervalString(s.t, row.lines[1]), nil)

	types := src.from("::formatTypes() {")
	decimal := types.from("formatter.formatDecimal(")
	result := decimal.from(`assertEquals("Format decNumber"`)
	s.checkParts(result.line, "en", "", intervalString(s.t, decimal.lines[0]), listStrings(s.t, result.lines[0])[1], nil)
	long := types.from("static const char* str =")
	input := intervalString(s.t, long.lines[0])
	s.checkParts(long.line, "en", "precision-unlimited", input, input, nil)
}

func (s *numberPartsSuite) cppSingle(call intervalSource, skeleton string, fields []intervalRow) {
	// Locale, input, and expected string are the last three arguments.
	result := call.from(`");`)
	j := result.line - call.line
	loc := call.lines[j-2]
	var locale string
	switch loc {
	case "Locale::getEnglish(),":
		locale = "en"
	case "Locale::getCanada(),":
		locale = "en-CA"
	case "Locale::getUS(),":
		locale = "en-US"
	default:
		locale = strings.ReplaceAll(intervalString(s.t, loc), "_", "-")
	}
	input := fixtureFloat(call, strings.TrimSuffix(call.lines[j-1], ","))
	s.checkParts(result.line, locale, skeleton, input, intervalString(s.t, result.lines[0]), fields)
}

func (s *numberPartsSuite) formatAttributes(src intervalSource) {
	block := src.from("::TestFormatAttributes() {")
	// TestFormatAttributes asserts the fields and only logs the formatted
	// text. These two strings were read from the corresponding ICU 79.1 formats.
	for _, tc := range []struct {
		marker, skeleton, text string
	}{
		{
			marker:   "double val = ",
			skeleton: "currency/USD",
			text:     "$12,345.67",
		},
		{
			marker:   "val = -",
			skeleton: "scientific",
			text:     "-1.23E-5",
		},
	} {
		row := block.from(tc.marker)
		_, input, _ := strings.Cut(row.lines[0], "= ")
		s.checkParts(row.line, "en-US", tc.skeleton, fixtureFloat(row, strings.TrimSuffix(input, ";")), tc.text, row.table("int32_t expected[] ="))
	}
}

func (s *numberPartsSuite) accounting(src intervalSource) {
	currencies := map[string]string{"en_US": "USD", "ja_JP": "JPY", "de_DE": "EUR", "en_ID": "IDR", "sh_ME": "EUR"}
	block := src.from("void TestAccountingCurrency()")
	var start int
	var cols []string
	for _, row := range block.table("String[][] tests =") {
		if len(cols) == 0 {
			start = row.line
		}
		cols = append(cols, listStrings(s.t, row.text)...)
		if !strings.Contains(row.text, "}") {
			continue
		}
		if len(cols) != 6 {
			s.t.Fatalf("%s:%d: expected six columns in accounting test", s.path, start)
		}
		locale, input, want := cols[0], cols[1], cols[4]
		cols = nil
		if strings.Contains(locale, "@cf=") {
			s.excluded["locale currency preference repeats explicit accounting style"]++
			continue
		}
		currency := currencies[locale]
		if currency == "" {
			s.t.Fatalf("%s:%d: missing regional currency for %s", s.path, start, locale)
		}
		s.checkParts(start, strings.ReplaceAll(locale, "_", "-"), "currency/"+currency+" sign-accounting", fixtureFloat(block, input), want, nil)
	}
}
