// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type displayNameSuite struct {
	*formatFixture
	vm ekuma.VM
	of ekuma.Value
}

func runDisplayNameComponents(t *testing.T, root string) {
	const java = "icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/"
	run := func(path string, read func(*displayNameSuite)) {
		t.Run(filepath.Base(path), func(t *testing.T) {
			s := &displayNameSuite{
				formatFixture: newFormatFixture(t, filepath.Join(root, path)),
			}
			s.of = s.functionIn(&s.vm, `(locale, type, code, style, fallback) =>
				new Intl.DisplayNames(locale, {type, style, fallback}).of(code)`)
			read(s)
		})
	}
	run("icu4c/source/test/intltest/locnmtst.cpp", (*displayNameSuite).localeComponents)
	run("icu4c/source/test/cintltst/cloctst.c", (*displayNameSuite).cLocaleNames)
	run("icu4c/source/test/intltest/dtptngts.cpp", (*displayNameSuite).fieldNames)
	run(java+"TestLocaleNamePackaging.java", (*displayNameSuite).packagedRegions)
	run(java+"util/CurrencyTest.java", (*displayNameSuite).currencyNames)
	run(java+"util/ULocaleTest.java", (*displayNameSuite).calendarNames)
	run(icuData+"region/es.txt", func(s *displayNameSuite) {
		s.shortNames(readResource(s.t, s.path), "Countries", "region")
	})
	run(icuData+"lang/en.txt", func(s *displayNameSuite) {
		lang := readResource(s.t, s.path)
		s.shortNames(lang, "Scripts", "script")
		// The explicit Kawi entry distinguishes a name equal to its code from a missing name.
		entry := lang.child("Scripts").child("Kawi")
		s.name(entry.line, lang.name, "script", entry.name, "long", "none", resourceValue(s.formatFixture, entry))
		mapping := readResource(s.t, filepath.Join(root, icuData, "misc/keyTypeData.txt")).child("typeMap").child("calendar")
		for _, entry := range lang.child("Types").child("calendar").children {
			code := unicodeType(s.formatFixture, mapping, entry.name)
			s.name(entry.line, lang.name, "calendar", code, "long", "none", resourceValue(s.formatFixture, entry))
		}
	})
	run(icuData+"locales/bn.txt", (*displayNameSuite).fieldResources)
}

// want is a string or ekuma.Undefined for a missing name with fallback "none".
func (s *displayNameSuite) name(line int, locale, typ, code, style, fallback string, want any) {
	s.t.Helper()
	s.executed++
	expected := s.vm.ValueOf(want)
	got, err := s.of.Invoke(s.t.Context(), locale, typ, code, style, fallback)
	if err != nil {
		s.t.Errorf("%s:%d: Intl.DisplayNames(%q, {type:%q, style:%q, fallback:%q}).of(%q): got error %v, want %s %q",
			s.path, line, locale, typ, style, fallback, code, err, expected.Type(), expected.String())
	} else if !got.Equal(expected) {
		s.t.Errorf("%s:%d: Intl.DisplayNames(%q, {type:%q, style:%q, fallback:%q}).of(%q): got %s %q, want %s %q",
			s.path, line, locale, typ, style, fallback, code, got.Type(), got.String(), expected.Type(), expected.String())
	}
}

func (s *displayNameSuite) localeComponents() {
	src := readIntervalSource(s.t, s.path)
	components := src.from("::TestUldnComponents() {")
	for _, typ := range []string{"script", "region"} {
		call := components.from("uldn_" + typ + "DisplayName(ldn,")
		code := intervalString(s.t, call.lines[0])
		result := call.from("test_assert_equal(")
		s.name(result.line, "de-DE", typ, code, "long", "code", intervalString(s.t, result.lines[0]))
	}
	// ICU's legacy gregorian keyword has the BCP 47 type gregory.
	calendar := components.from(`uldn_keyValueDisplayName(ldn, "calendar", "gregorian"`).from("test_assert_equal(")
	s.name(calendar.line, "de-DE", "calendar", "gregory", "long", "code", intervalString(s.t, calendar.lines[0]))
	s.excluded["component has no Intl.DisplayNames type (variant or keyword key)"] += 2
	s.excluded["script enum repeats the string-code case"]++
	s.excluded["currency keyword embedded in a compound locale name"] += 2
	for _, row := range src.table("LocNameDispContextItem ctxtItems[]") {
		if strings.HasPrefix(row.text, `{ "`) {
			s.excluded["capitalization and length contexts on compound or language names"]++
		}
	}
	numeric := src.from("::TestNumericRegionID() {")
	for _, line := range numeric.lines {
		if line == "}" {
			break
		}
		if !strings.Contains(line, "uldn_open(") {
			continue
		}
		loc := strings.ReplaceAll(intervalString(s.t, line), "_", "-")
		call := numeric.from(line).from("uldn_regionDisplayName(")
		want := call.from("test_assert_equal(")
		s.name(want.line, loc, "region", intervalString(s.t, call.lines[0]), "long", "code", intervalString(s.t, want.lines[0]))
	}

	for _, typ := range []string{"region", "script", "calendar"} {
		variable := "unknown_" + typ
		if typ == "calendar" {
			variable = "unknown_ca_value"
		}
		decl := src.from("char " + variable + "[]")
		code := intervalString(s.t, decl.lines[0])
		// ECMA-402 canonicalizes the code before applying fallback.
		canonical := code
		switch typ {
		case "region":
			canonical = strings.ToUpper(code)
		case "script":
			canonical = strings.ToUpper(code[:1]) + code[1:]
		}
		sub := src.from("::VerifySubstitute(").from("test_assert_equal(" + variable + ", temp)")
		s.name(sub.line, "de-DE", typ, code, "long", "code", canonical)
		noSub := src.from("::VerifyNoSubstitute(").from(variable + ", temp)").from("test_assert(true == temp.isBogus())")
		s.name(noSub.line, "de-DE", typ, code, "long", "none", ekuma.Undefined)
	}
}

func (s *displayNameSuite) shortNames(r resource, table, typ string) {
	long := r.child(table)
	shortNames := r.child(table + "%short")
	if len(shortNames.children) == 0 {
		s.t.Fatalf("%s: missing %s short names", s.path, table)
	}
	for _, short := range shortNames.children {
		full := long.child(short.name)
		s.name(full.line, r.name, typ, short.name, "long", "code", resourceValue(s.formatFixture, full))
		// ICU offers one abbreviated form for these two Intl styles.
		for _, style := range []string{"short", "narrow"} {
			s.name(short.line, r.name, typ, short.name, style, "code", resourceValue(s.formatFixture, short))
		}
	}
}

func (s *displayNameSuite) fieldNames() {
	src := readIntervalSource(s.t, s.path)
	// udatpg_test.c and DateTimeGeneratorTest.java repeat the C++ field-name table.
	widths := map[string]string{
		"UDATPG_WIDE":        "long",
		"UDATPG_ABBREVIATED": "short",
		"UDATPG_NARROW":      "narrow",
	}
	for _, row := range src.table("FieldDisplayNameData testData[]") {
		if strings.HasPrefix(row.text, "/*") {
			continue
		}
		c := row.columns(s.t, 4)
		if c[1] == "UDATPG_DAY_OF_WEEK_IN_MONTH_FIELD" {
			s.excluded["weekday of the month has no Intl dateTimeField code"]++
			continue
		}
		code := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(c[1], "UDATPG_"), "_FIELD"))
		style := widths[c[2]]
		s.name(row.line, intervalString(s.t, c[0]), "dateTimeField", code, style, "code", intervalString(s.t, c[3]))
	}
}

func (s *displayNameSuite) fieldResources() {
	r := readResource(s.t, s.path)
	for _, field := range r.child("fields").children {
		dn := field.child("dn")
		if dn.line == 0 {
			continue
		}
		code, style, ok := strings.Cut(field.name, "-")
		if !ok {
			style = "long"
		}
		switch code {
		case "week":
			code = "weekOfYear"
		case "zone":
			code = "timeZoneName"
		case "dayperiod":
			code = "dayPeriod"
		case "dayOfYear", "weekOfMonth", "weekdayOfMonth":
			s.excluded["field has no Intl dateTimeField code"]++
			continue
		}
		s.name(dn.line, r.name, "dateTimeField", code, style, "code", resourceValue(s.formatFixture, dn))
	}
}
