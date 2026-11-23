// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func runLocaleCollation(t *testing.T, dir string, failures map[string]map[int]string) {
	run := func(file string, check func(*tailoredCollation)) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			path := filepath.Join(dir, file)
			f := newFormatFixture(t, path)
			f.trackOperations(failures)
			s := tailoredCollation{
				formatFixture: f,
				factory: f.function(`(locale, options) => {
					const collator = new Intl.Collator(locale, options);
					const resolved = collator.resolvedOptions();
					if (resolved.locale !== locale || resolved.collation !== (options.collation || "default")) {
						throw new Error("resolved " + resolved.locale + " collation=" + resolved.collation);
					}
					return collator.compare;
				}`),
			}
			check(&s)
		})
	}
	runSource := func(file string, check func(*tailoredCollation, intervalSource)) {
		run(file, func(s *tailoredCollation) {
			check(s, readIntervalSource(s.t, s.path))
		})
	}
	const intltest = "icu4c/source/test/intltest/"
	runSource(intltest+"regcoll.cpp", (*tailoredCollation).regressions)
	runSource(intltest+"frcoll.cpp", (*tailoredCollation).french)
	runSource(intltest+"ficoll.cpp", func(s *tailoredCollation, src intervalSource) {
		// ICU requests fi_FI@collation=standard, the default Finnish tailoring.
		s.pairTables(src, "fi-FI", 4, false)
	})
	runSource(intltest+"trcoll.cpp", func(s *tailoredCollation, src intervalSource) {
		s.pairTables(src, "tr", 8, false)
	})
	runSource(intltest+"thcoll.cpp", (*tailoredCollation).thai)
	run("icu4c/source/test/testdata/riwords.txt", (*tailoredCollation).thaiDictionary)
	runSource("icu4c/source/test/cintltst/cmsccoll.c", (*tailoredCollation).longNumbers)
	runSource("icu4j/main/collate/src/test/java/com/ibm/icu/dev/test/collator/CollationMiscTest.java", (*tailoredCollation).javaCases)
}

type tailoredCollation struct {
	*formatFixture
	factory ekuma.Value
}

type checkCollation func(line int, a, b, want string)

func (s *tailoredCollation) comparator(group, locale string, options map[string]any) checkCollation {
	s.t.Helper()
	compare, err := s.factory.Invoke(s.t.Context(), locale, options)
	if err != nil {
		s.t.Fatalf("%s: %s locale=%s options=%v: %v", s.path, group, locale, options, err)
	}
	return func(line int, a, b, want string) {
		got, err := compare.Invoke(s.t.Context(), a, b)
		if err != nil || got.Type() != ekuma.TypeNumber || math.IsNaN(got.Float()) {
			s.executed++
			s.t.Errorf("%s:%d: %s locale=%s options=%v: %+q vs %+q: got %v (error %v), want %s", s.path, line, group, locale, options, a, b, got, err, want)
			return
		}
		relation := orderSign(cmp.Compare(got.Float(), 0))
		if want == "<=" && relation != ">" {
			relation = "<="
		}
		s.checkValue(line, group, relation, want,
			fmt.Sprintf("locale=%s options=%v: %+q vs %+q", locale, options, a, b))
	}
}

var cppInlineComment = regexp.MustCompile(`/\*.*?\*/`)
var ucharInitializer = regexp.MustCompile(`\{[^{}]*\}`)

func (s *tailoredCollation) regressions(src intervalSource) {
	triples := func(test, table, locale, sensitivity string) {
		check := s.comparator(test, locale, map[string]any{"sensitivity": sensitivity})
		rows := src.from("void CollationRegressionTest::" + test + "(").table("const char16_t " + table + "[][")
		for _, row := range rows {
			triple := ucharInitializer.FindAllString(row.text, -1)
			if len(triple) != 3 {
				s.t.Fatalf("%s:%d: malformed comparison %q", src.path, row.line, row.text)
			}
			check(row.line, fixtureUChars(s.t, triple[0]), fixtureUChars(s.t, triple[2]), fixtureUChars(s.t, triple[1]))
		}
	}
	triples("Test4062418", "tests", "fr-CA", "accent")
	triples("Test4066696", "tests", "fr-CA", "accent")
	triples("Test4132736", "test1", "fr-CA", "variant")
	triples("Test4087241", "tests", "da-DK", "accent")
}

func collationWords(src intervalSource, table string) []intervalRow {
	rows := src.table(table)
	for i, row := range rows {
		rows[i].text = fixtureUChars(src.t, cppInlineComment.ReplaceAllString(row.text, ""))
	}
	return rows
}

func (s *tailoredCollation) french(src intervalSource) {
	// fr_CA's standard tailoring contains [backwards 2].
	check := s.comparator("TestSecondary/testAcute", "fr-CA", map[string]any{"sensitivity": "accent"})
	accents := collationWords(src, "::testAcute[][")
	for i, a := range accents {
		for j, b := range accents {
			check(b.line, a.text, b.text, orderSign(cmp.Compare(i, j)))
		}
	}

	s.pairTables(src, "fr-CA", 12, true)
	check = s.comparator("TestExtra/testBugs", "fr-CA", map[string]any{"sensitivity": "variant"})
	words := collationWords(src, "::testBugs[][")
	for i, b := range words {
		for _, a := range words[:i] {
			check(b.line, a.text, b.text, "<")
		}
	}
}

func (s *tailoredCollation) pairTables(src intervalSource, locale string, tertiaryCount int, shifted bool) {
	left := collationWords(src, "::testSourceCases[][")
	right := collationWords(src, "::testTargetCases[][")
	results := src.table("::results[] =")
	if len(left) != len(right) || len(left) != len(results) || len(left) < tertiaryCount {
		s.t.Fatalf("%s: inconsistent comparison tables (%d sources, %d targets, %d results, %d tertiary)", src.path, len(left), len(right), len(results), tertiaryCount)
	}
	check := s.comparator("TestTertiary", locale, map[string]any{"sensitivity": "variant", "ignorePunctuation": shifted})
	relations := map[string]string{"Collator::LESS": "<", "Collator::EQUAL": "=", "Collator::GREATER": ">"}
	for i, result := range results {
		if i == tertiaryCount {
			check = s.comparator("TestPrimary", locale, map[string]any{"sensitivity": "base"})
		}
		relation := strings.Trim(cppInlineComment.ReplaceAllString(result.text, ""), ", ")
		want, ok := relations[relation]
		if !ok {
			s.t.Fatalf("%s:%d: unknown comparison %q", src.path, result.line, relation)
		}
		check(result.line, left[i].text, right[i].text, want)
	}
}

func (s *tailoredCollation) thai(src intervalSource) {
	triples := func(group, table, sensitivity string) {
		check := s.comparator(group, "th-TH", map[string]any{"sensitivity": sensitivity, "ignorePunctuation": true})
		for _, row := range src.from("void CollationThaiTest::" + group + "(").table(table) {
			cols := row.columns(s.t, 3)
			check(row.line, intervalString(s.t, cols[0]), intervalString(s.t, cols[2]), intervalString(s.t, cols[1]))
		}
	}
	triples("TestCornerCases", "char* TESTS[] =", "variant")
	triples("TestReordering", "char *tests[] =", "accent")
	s.excluded["TestReordering/testcontraction uses the custom rule & c < ab"]++
}

func (s *tailoredCollation) thaiDictionary() {
	check := s.comparator("TestDictionary", "th-TH", map[string]any{"sensitivity": "variant", "ignorePunctuation": true})
	var prev string
	fixtureLines(s.t, s.path, func(line int, word string) {
		if trimmed := strings.TrimSpace(word); trimmed == "" || strings.HasPrefix(trimmed, "#") {
			return
		}
		if prev != "" {
			// TestDictionary accepts ties between adjacent entries.
			check(line, prev, word, "<=")
		}
		prev = word
	})
}

func (s *tailoredCollation) longNumbers(src intervalSource) {
	// ICU's en collation resource is empty. Its default inherits the root
	// ordering used by TestNumericCollation for these digit runs.
	var numbers []intervalRow
	for _, row := range src.table("*longNumericStrings[]=") {
		if strings.HasPrefix(row.text, `"`) {
			row.text = intervalString(s.t, row.text)
			numbers = append(numbers, row)
		}
	}
	for i, a := range numbers {
		group := fmt.Sprintf("longNumericStrings/%d", a.line)
		check := s.comparator(group, "en", map[string]any{"numeric": true, "sensitivity": "variant"})
		for _, b := range numbers[i+1:] {
			check(b.line, a.text, b.text, "<")
		}
	}
}

// javaCollationArray reads the quoted strings, including UTF-16 surrogate pairs.
func javaCollationArray(src intervalSource, marker string) ([]string, int) {
	start := src.from(marker)
	_, body, ok := strings.Cut(strings.Join(start.lines, "\n"), "{")
	body, _, end := strings.Cut(body, "};")
	var values []string
	if err := json.Unmarshal([]byte("["+body+"]"), &values); !ok || !end || err != nil || len(values) < 2 {
		src.t.Fatalf("%s:%d: malformed %s: %v", src.path, start.line, marker, err)
	}
	return values, start.line
}

func (check checkCollation) forwardPairs(line int, values []string, want string) {
	for i, a := range values {
		for _, b := range values[i+1:] {
			check(line, a, b, want)
		}
	}
}

func (s *tailoredCollation) javaCases(src intervalSource) {
	ch := src.from("public void TestChMove(")
	values, line := javaCollationArray(ch, "String[] chTest =")
	check := s.comparator("TestChMove/chTest", "cs", map[string]any{"sensitivity": "variant"})
	check.forwardPairs(line, values, "<")

	// ICU's zh__PINYIN uses the zh resource's default pinyin tailoring.
	pinyin := src.from("public void TestPinyinProblem(")
	values, line = javaCollationArray(pinyin, "String test[]")
	check = s.comparator("TestPinyinProblem", "zh", map[string]any{"sensitivity": "variant", "collation": "pinyin"})
	check.forwardPairs(line, values, "<")

	numeric := src.from("public void TestNumericCollation(")
	options := map[string]any{"numeric": true, "sensitivity": "variant"}
	for _, name := range []string{"basicTestStrings", "thirtyTwoBitNumericStrings", "foreignDigits", "supplementaryDigits", "preZeroTestStrings"} {
		values, line := javaCollationArray(numeric, "String "+name+"[]")
		check := s.comparator("TestNumericCollation/"+name, "en", options)
		want := "<"
		if name == "preZeroTestStrings" {
			want = "="
		}
		check.forwardPairs(line, values, want)
	}

	values, line = javaCollationArray(numeric, "String lastDigitDifferent[]")
	if len(values)%2 != 0 {
		s.t.Fatalf("%s:%d: expected digit pairs", src.path, line)
	}
	check = s.comparator("TestNumericCollation/lastDigitDifferent", "en", options)
	for i := 0; i < len(values); i += 2 {
		check(line, values[i], values[i+1], "<")
	}
}
