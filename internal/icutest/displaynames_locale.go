// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"strings"

	"github.com/hajimehoshi/ekuma"
)

// countryRow reads one row of cloctst.c's transposed rawData2 matrix.
func countryRow(src intervalSource, marker string) []intervalRow {
	row := src.from(marker)
	var cells []intervalRow
	for i, line := range row.lines[1:] {
		for _, value := range listStrings(src.t, line) {
			cells = append(cells, intervalRow{
				line: row.line + i + 1,
				text: value,
			})
		}
		if strings.Contains(line, "}") {
			if len(cells) != 9 {
				src.t.Fatalf("%s:%d: expected nine columns for %s", src.path, row.line, marker)
			}
			return cells
		}
	}
	src.t.Fatalf("%s:%d: unterminated %s", src.path, row.line, marker)
	return nil
}

func (s *displayNameSuite) cLocaleNames() {
	src := readIntervalSource(s.t, s.path)
	codes := countryRow(src, "/* country code */")
	for language, locale := range map[string]string{
		"English": "en-US",
		"French":  "fr-FR",
		"Catalan": "ca-ES",
		"Greek":   "el-GR",
	} {
		names := countryRow(src, "/* display country ("+language+") */")
		for i, code := range codes {
			if code.text == "" {
				s.excluded["locale has no region subtag"]++
				continue
			}
			s.name(names[i].line, locale, "region", code.text, "long", "code", names[i].text)
		}
		s.excluded["uloc_getDisplayScript uses the stand-alone Hans name"]++
	}
	for _, row := range src.table("displayNameBracketsItems[]") {
		c := row.columns(s.t, 5)
		if c[0] == "NULL" {
			break
		}
		if intervalString(s.t, c[2]) == "my_Mymr_MM" {
			s.excluded["same region in a second compound locale name"]++
			continue
		}
		s.name(row.line, intervalString(s.t, c[0]), "region", intervalString(s.t, c[1]), "long", "code", intervalString(s.t, c[3]))
	}
	noSub := src.from(`uldn_openForContext("en", contexts, 1, &status)`)
	for _, typ := range []string{"script", "region"} {
		call := noSub.from("uldn_" + typ + "DisplayName(ldn,")
		s.name(call.line, "en", typ, intervalString(s.t, call.lines[0]), "long", "none", ekuma.Undefined)
	}
	s.excluded["two-letter calendar keyword is invalid in Intl"]++

	warnings := src.from("TestUsingDefaultWarning(void) {")
	for _, typ := range []string{"script", "region"} {
		input := warnings.from("const char* " + typ + " =")
		code := strings.TrimPrefix(intervalString(s.t, input.lines[0]), "und-")
		call := input.from("length = uloc_getDisplay")
		result := call.from("u_strcmp(buff,")
		s.name(result.line, intervalString(s.t, call.lines[0]), typ, code, "long", "code", intervalString(s.t, result.lines[0]))
	}
	s.keywordValues(src.from("TestDisplayKeywordValues(void){"))
}

func (s *displayNameSuite) keywordValues(src intervalSource) {
	rows := src.table("} testCases[]")
	for i, row := range rows {
		if !strings.Contains(row.text, "@") {
			continue
		}
		v := listStrings(s.t, row.text)
		_, keyword, _ := strings.Cut(v[0], "@")
		typ, code, _ := strings.Cut(keyword, "=")
		if typ == "collation" {
			s.excluded["collation has no Intl.DisplayNames type"]++
			continue
		}
		want := rows[i+1]
		s.name(want.line, strings.ReplaceAll(v[1], "_", "-"), typ, code, "long", "code", fixtureUChars(s.t, want.text))
	}
	// ICU enumerates the three keywords in alphabetical order.
	multi := src.from(`localeID = "es@collation=phonebook;calendar=buddhist;currency=DEM"`)
	rows = multi.table("UChar expected[][50]")
	locale := intervalString(s.t, multi.from("const char* displayLocale =").lines[0])
	s.name(rows[0].line, locale, "calendar", "buddhist", "long", "code", fixtureUChars(s.t, rows[0].text))
	s.name(rows[2].line, locale, "currency", "DEM", "long", "code", fixtureUChars(s.t, rows[2].text))
	s.excluded["collation has no Intl.DisplayNames type"]++
}

func (s *displayNameSuite) packagedRegions() {
	src := readIntervalSource(s.t, s.path)
	// The ICU4J table crosses display locales and target locales in this order.
	locales := []string{"root", "en-US", "es-ES", "de-DE", "und-TH"}
	regions := []string{"", "US", "ES", "DE", "TH"}
	rows := src.table("String[] expectedWithRegionData")
	if len(rows) != len(locales)*len(regions) {
		s.t.Fatalf("%s: invalid region name matrix", s.path)
	}
	for i, row := range rows {
		loc, code := locales[i/len(regions)], regions[i%len(regions)]
		switch {
		case loc == "root" || loc == "und-TH":
			s.excluded["ICU root or language-less display locale"]++
		case code == "":
			s.excluded["locale has no region subtag"]++
		default:
			s.name(row.line, loc, "region", code, "long", "code", intervalString(s.t, row.text))
		}
	}
}

func (s *displayNameSuite) currencyNames() {
	src := readIntervalSource(s.t, s.path)
	// NumberFormatTest::TestCurrencyNames repeats these two long-name assertions.
	for code, locale := range map[string]string{"USD": "en", "USX": "en-US"} {
		result := src.from(`"` + code + `.getName(LONG_NAME`)
		s.name(result.line+1, locale, "currency", code, "long", "code", intervalString(s.t, result.lines[1]))
	}
	cny := src.from(`assertEquals("currency name mismatch", "`)
	s.name(cny.line, "en-CA", "currency", "CNY", "long", "code", listStrings(s.t, cny.lines[0])[1])

	names := src.from("void TestCurrencyDisplayNames()")
	var loc, fallback string
	for i, line := range names.lines {
		if strings.Contains(line, "getInstance(Locale.GERMANY") {
			break // The java.util.Locale overload repeats the ULocale cases.
		}
		switch {
		case strings.Contains(line, "CurrencyDisplayNames.getInstance("):
			loc = "de-DE"
			if strings.Contains(line, "forLanguageTag") {
				loc = intervalString(s.t, line)
			}
			fallback = "code"
			if strings.Contains(line, ", true)") {
				fallback = "none"
			}
		case strings.Contains(line, "cdn.getName("):
			v := listStrings(s.t, line)
			var want any = ekuma.Undefined
			if strings.HasPrefix(line, "assertEquals(") {
				want = v[1]
			}
			s.name(names.line+i, loc, "currency", v[len(v)-1], "long", fallback, want)
		case strings.Contains(line, "cdn.getSymbol("), strings.Contains(line, "cdn.getPluralName("):
			s.excluded["currency symbol or plural form"]++
		}
	}
	// TestFmwk sets the ICU4J default display locale to Locale.US.
	for _, row := range src.table("String[][] DISPNAME_TESTDATA") {
		v := listStrings(s.t, row.text)
		if v[0] != "USD" { // The en-US name was checked by TestCurrencyDisplayNames.
			s.name(row.line, "en-US", "currency", v[0], "long", "code", v[1])
		}
		s.excluded["root display locale"]++
		s.excluded["Japanese name compared only for inequality"]++
	}
}

func (s *displayNameSuite) calendarNames() {
	src := readIntervalSource(s.t, s.path)
	cases := src.from("void TestDisplayKeyword()")
	names := src.from("void initHashtable()")
	for _, row := range cases.table("String[] data =") {
		_, code, ok := strings.Cut(intervalString(s.t, row.text), ";calendar=")
		if !ok {
			s.t.Fatalf("%s:%d: missing calendar keyword", s.path, row.line)
		}
		for table, loc := range map[string]string{"h[0]": "en-US", "h[1]": "zh-CN"} {
			want := names.from(table + `.put("` + code + `",`)
			s.name(want.line, loc, "calendar", code, "long", "code", listStrings(s.t, want.lines[0])[1])
		}
	}
	s.excluded["getDisplayScript overloads request the stand-alone Hans name"] += 3
}
