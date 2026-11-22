// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type pluralSample struct {
	value   string
	digits  int
	compact bool
	line    int
}

// pluralSamples expands the CLDR sample ranges at the precision of their endpoints.
func pluralSamples(category resource) []pluralSample {
	var samples []pluralSample
	_, tail, _ := strings.Cut(strings.Join(category.values, ""), "@")
	for section := range strings.SplitSeq(tail, "@") {
		_, items, ok := strings.Cut(section, " ")
		if !ok {
			continue
		}
		for item := range strings.SplitSeq(strings.TrimSpace(items), ",") {
			item = strings.TrimSpace(item)
			if item == "…" {
				continue
			}
			if mantissa, exp, ok := strings.Cut(item, "c"); ok {
				_, fraction, _ := strings.Cut(mantissa, ".")
				samples = append(samples, pluralSample{
					value:   mantissa + "e" + exp,
					digits:  max(0, len(fraction)-integer(exp)),
					compact: true,
					line:    category.line,
				})
				continue
			}
			lo, hi, ranged := strings.Cut(item, "~")
			_, fraction, _ := strings.Cut(lo, ".")
			digits := len(fraction)
			if !ranged {
				samples = append(samples, pluralSample{
					value:  lo,
					digits: digits,
					line:   category.line,
				})
				continue
			}
			start := integer(strings.ReplaceAll(lo, ".", ""))
			end := integer(strings.ReplaceAll(hi, ".", ""))
			for i := start; i <= end; i++ {
				raw := fmt.Sprintf("%0*d", digits+1, i)
				if digits > 0 {
					k := len(raw) - digits
					raw = raw[:k] + "." + raw[k:]
				}
				samples = append(samples, pluralSample{
					value:  raw,
					digits: digits,
					line:   category.line,
				})
			}
		}
	}
	return samples
}

// A best-fit match can select a different language when CLDR supplies only
// supplemental plural rules for the requested locale.
func pluralLocaleProbe(f *formatFixture) ekuma.Value {
	return f.function(`id => {
		const requested = new Intl.Locale(id);
		const resolved = new Intl.Locale(new Intl.PluralRules(id).resolvedOptions().locale);
		return requested.language === resolved.language && (!requested.region || requested.region === resolved.region)
			? "" : resolved.baseName;
	}`)
}

func pluralLocaleExclusion(f *formatFixture, probe ekuma.Value, id string) string {
	if id == "root" {
		return "ICU root locale cannot be selected by Intl locale negotiation"
	}
	v, err := probe.Invoke(f.t.Context(), id)
	if err != nil {
		f.t.Fatalf("%s: resolving %q: %v", f.path, id, err)
	}
	if v.String() != "" {
		return "requested plural locale " + id + " resolves to " + v.String()
	}
	return ""
}

func runPluralSamples(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	data := readResource(t, path)
	probe := pluralLocaleProbe(f)
	selectFn := f.function(`(locale, type, digits, notation, value) =>
		new Intl.PluralRules(locale, {type, notation, compactDisplay: "long", minimumFractionDigits: digits, maximumFractionDigits: digits}).select(Number(value))`)
	for _, kind := range []string{"cardinal", "ordinal"} {
		key := "locales"
		if kind == "ordinal" {
			key = "locales_ordinals"
		}
		for _, locale := range data.child(key).children {
			id := strings.ReplaceAll(locale.name, "_", "-")
			reason := pluralLocaleExclusion(f, probe, id)
			for _, category := range data.child("rules").child(locale.values[0]).children {
				for _, s := range pluralSamples(category) {
					if reason != "" {
						f.excluded[reason]++
						continue
					}
					notation := "standard"
					if s.compact {
						notation = "compact"
					}
					f.check(s.line, "Intl.PluralRules.select", selectFn, category.name, id, kind, s.digits, notation, s.value)
				}
			}
		}
	}
}

// rangeOperands chooses distinct operands from upstream cardinal examples that
// retain their visible fraction digits under a single set of formatter options.
func rangeOperands(left, right []pluralSample) (string, string, int, int, bool) {
	for _, a := range left {
		for _, b := range right {
			if a.compact || b.compact {
				continue
			}
			var minimum int
			for _, s := range []pluralSample{a, b} {
				if s.digits > 0 && strings.HasSuffix(s.value, "0") {
					minimum = max(minimum, s.digits)
				}
			}
			if minimum > min(a.digits, b.digits) {
				continue
			}
			x, _ := strconv.ParseFloat(a.value, 64)
			y, _ := strconv.ParseFloat(b.value, 64)
			if x != y {
				return a.value, b.value, minimum, max(a.digits, b.digits), true
			}
		}
	}
	return "", "", 0, 0, false
}

func runPluralRanges(t *testing.T, path, samplesPath string) {
	f := newFormatFixture(t, path)
	ranges := readResource(t, path)
	cardinals := readResource(t, samplesPath)
	probe := pluralLocaleProbe(f)
	selectRange := f.function(`(locale, min, max, start, end) =>
		new Intl.PluralRules(locale, {minimumFractionDigits:min, maximumFractionDigits:max}).selectRange(Number(start), Number(end))`)
	for _, locale := range ranges.child("locales").children {
		id := locale.name
		reason := pluralLocaleExclusion(f, probe, id)
		set := cardinals.child("locales").child(id)
		rules := cardinals.child("rules").child(set.values[0])
		categories := map[string][]pluralSample{}
		for _, cat := range rules.children {
			categories[cat.name] = pluralSamples(cat)
		}
		for _, row := range ranges.child("rules").child(locale.values[0]).children {
			if reason != "" {
				f.excluded[reason]++
				continue
			}
			start, end, want := row.values[0], row.values[1], row.values[2]
			a, b, min, max, ok := rangeOperands(categories[start], categories[end])
			if !ok {
				condition, _, _ := strings.Cut(strings.Join(rules.child(start).values, ""), "@")
				if start == end && strings.TrimSpace(condition) == "n = 1" {
					f.excluded["identical formatted operands bypass the CLDR range rule ("+id+" "+start+".."+end+")"]++
					continue
				}
				t.Fatalf("%s:%d: no distinct %s operands for %s..%s", path, row.line, id, start, end)
			}
			f.check(row.line, "Intl.PluralRules.selectRange ("+start+".."+end+")", selectRange, want, id, min, max, a, b)
		}
	}
}
