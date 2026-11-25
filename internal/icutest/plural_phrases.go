// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strings"
	"testing"
)

// ICU4J's WritePluralRulesData.getOriginalSamples takes the first decimal
// from each phrase. Several categories can share the same words. The category
// oracle for those inputs is the cardinal rule in ICU's CLDR resource.
func runPluralPhrases(t *testing.T, path, rulesPath string) {
	f := newFormatFixture(t, path)
	data := readResource(t, rulesPath)
	selectFn := f.function(`(locale, digits, value) => new Intl.PluralRules(locale,
		{minimumFractionDigits: digits, maximumFractionDigits: digits}).select(Number(value))`)
	decimal := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)
	fixtureLines(t, path, func(line int, row string) {
		if row == "" || strings.HasPrefix(row, "#") {
			return
		}
		id, phrase, ok := strings.Cut(row, "\t")
		value := decimal.FindString(phrase)
		if !ok || value == "" {
			t.Fatalf("%s:%d: invalid plural sample %q", path, line, row)
		}
		mapping := data.child("locales").child(id)
		if len(mapping.values) == 0 {
			language, _, _ := strings.Cut(id, "_")
			mapping = data.child("locales").child(language)
		}
		if len(mapping.values) != 1 {
			t.Fatalf("%s:%d: missing cardinal rules for %s", path, line, id)
		}
		whole, frac, _ := strings.Cut(value, ".")
		digits := len(frac)
		i := integer(whole)
		fraction := integer("0" + frac)
		// CLDR '=' tests membership in integer ranges. A non-integral n
		// cannot match any such range, including after a modulo operation.
		n := i
		if fraction != 0 {
			n = -1
		}
		operands := map[string]int{"n": n, "i": i, "v": digits, "f": fraction, "t": integer("0" + strings.TrimRight(frac, "0")), "e": 0}
		want := "other"
		for _, cat := range data.child("rules").child(mapping.values[0]).children {
			condition, _, _ := strings.Cut(strings.Join(cat.values, ""), "@")
			if cat.name != "other" && matchesCardinalRule(strings.TrimSpace(condition), operands) {
				want = cat.name
				break
			}
		}
		f.check(line, "Intl.PluralRules.select", selectFn, want, strings.ReplaceAll(id, "_", "-"), digits, value)
	})
}

func matchesCardinalRule(condition string, operands map[string]int) bool {
	for alternative := range strings.SplitSeq(condition, " or ") {
		match := true
		for relation := range strings.SplitSeq(alternative, " and ") {
			if !matchesCardinalRelation(relation, operands) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func matchesCardinalRelation(relation string, operands map[string]int) bool {
	words := strings.Fields(relation)
	value, ok := operands[words[0]]
	if !ok {
		panic("icutest: unknown plural operand in " + relation)
	}
	if len(words) == 5 && words[1] == "%" {
		if value >= 0 {
			value %= integer(words[2])
		}
		words = words[2:]
	}
	if len(words) != 3 || words[1] != "=" && words[1] != "!=" {
		panic("icutest: unknown plural relation " + relation)
	}
	var member bool
	for interval := range strings.SplitSeq(words[2], ",") {
		lo, hi, ranged := strings.Cut(interval, "..")
		if !ranged {
			hi = lo
		}
		if value >= integer(lo) && value <= integer(hi) {
			member = true
			break
		}
	}
	return member == (words[1] == "=")
}
