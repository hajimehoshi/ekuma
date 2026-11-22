// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/intl/collate"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type collationSettings struct {
	locale                        string // "root" selects the root directly, independently of JS locale negotiation.
	strength                      string
	alternate                     string
	caseFirst                     string
	numeric, caseLevel, backwards bool
	reorder, maxVariable          string
	unsupported                   string
}

func newCollationSettings(id string) collationSettings {
	return collationSettings{
		locale:   id,
		strength: "tertiary",
	}
}

func (s collationSettings) exclusion() string {
	switch {
	case s.unsupported != "":
		return s.unsupported
	case s.reorder != "":
		return "explicit ICU script reordering is unavailable"
	case s.alternate == "shifted" && s.maxVariable == "space":
		return "ICU shifts only spaces; Ekuma also shifts punctuation"
	case s.alternate == "shifted" && s.maxVariable == "currency":
		return "Ekuma shifts through punctuation; ICU maxVariable=currency also shifts symbols and currency"
	case s.locale != "root" && s.backwards:
		return "Intl.Collator cannot enable backwards secondary independently of locale data"
	case s.locale != "root" && (s.strength == "quaternary" || s.strength == "identical"):
		return "Intl.Collator exposes sensitivity through tertiary strength"
	case s.locale != "root" && s.caseLevel && s.strength != "primary":
		return "Intl.Collator has no sensitivity combining accents with a separate case level"
	}
	return ""
}

func (s *collationSettings) setAttribute(attr string) error {
	switch attr {
	case "strength=primary", "strength=secondary", "strength=tertiary", "strength=quaternary", "strength=identical":
		s.strength = strings.TrimPrefix(attr, "strength=")
	case "alternate=non-ignorable", "alternate=shifted":
		s.alternate = strings.TrimPrefix(attr, "alternate=")
	case "caseFirst=upper", "caseFirst=lower":
		s.caseFirst = strings.TrimPrefix(attr, "caseFirst=")
	case "numeric=on", "numeric=off":
		s.numeric = attr == "numeric=on"
	case "caseLevel=on", "caseLevel=off":
		s.caseLevel = attr == "caseLevel=on"
	case "backwards=on", "backwards=off":
		s.backwards = attr == "backwards=on"
	case "maxVariable=currency", "maxVariable=punct", "maxVariable=space":
		s.maxVariable = strings.TrimPrefix(attr, "maxVariable=")
	case "reorder Hani Zzzz digit", "reorder Zzzz Grek", "reorder Grek punct space", "reorder Goth Latn":
		s.reorder = strings.TrimPrefix(attr, "reorder ")
	case "reorder default":
		s.reorder = ""
	default:
		return fmt.Errorf("unknown attribute %q", attr)
	}
	return nil
}

// ruleCollationSettings recognizes whole rule blocks with known settings or a bundled tailoring.
func ruleCollationSettings(rules string) collationSettings {
	s := newCollationSettings("root")
	switch rules {
	case "[import el]":
		s.locale = "el"
	case "", "[import el][reorder Zzzz]", "[import el][reorder others]":
		// Greek standard changes only normalization and script order. Clearing
		// that reordering leaves the normalized root order.
	case "[import und-u-co-search][import de-u-co-phonebk]", "[import root-u-co-search][import de-u-co-phonebk]":
		// These two imports are the complete German search tailoring.
		s.locale = "de-u-co-search"
	case "[maxVariable space][alternate shifted]":
		s.maxVariable = "space"
		s.alternate = "shifted"
	default:
		s.unsupported = "custom ICU tailoring requires a run-time rule builder"
	}
	return s
}

func localeCollationSettings(id string) (collationSettings, error) {
	s := newCollationSettings(id)
	switch strings.ToLower(id) {
	case "de@collation=phonebook":
		s.locale = "de-u-co-phonebk"
	case "de-u-kv-space-ka-shifted-kn-kk-false-kf-upper-kc-true-ks-level4":
		s.unsupported = "ICU shifts only spaces and disables normalization with this locale's keywords"
	case "fr@colbackwards=yes;colstrength=quaternary;kv=currency;colalternate=shifted":
		s.unsupported = "ICU locale keywords select backwards secondary and shift through currency"
	case "el-u-kr-kana-symbol-grek-hani-cyrl-latn-digit-armn-deva-ethi-thai":
		s.unsupported = "explicit ICU script reordering is unavailable"
	default:
		tag, ok := locale.Parse(id)
		if !ok {
			return s, fmt.Errorf("invalid or unrecognized ICU locale %q", id)
		}
		for _, k := range tag.Keywords {
			if k.Key != "co" {
				return s, fmt.Errorf("unrecognized collation keyword %q in %q", k.Key, id)
			}
		}
	}
	return s, nil
}

// Native comparators include all weaker levels. Root quaternary and identical
// are additional tie-breakers after those comparators.
type levelComparison struct {
	level   string
	compare compareStrings
}

type compareStrings func(a, b wtf8.String) (int, error)

func goComparison(compare func(a, b string) int) compareStrings {
	return func(a, b wtf8.String) (int, error) {
		return compare(string(a), string(b)), nil
	}
}

func (s collationSettings) comparisons(t *testing.T, factory ekuma.Value) ([]levelComparison, error) {
	var levels []levelComparison
	for i, name := range []string{"primary", "secondary", "tertiary"} {
		strength := i + 1
		c, err := s.compareAt(t, factory, strength, s.caseLevel && strength == 3)
		if err != nil {
			return nil, err
		}
		levels = append(levels, levelComparison{
			level:   []string{"1", "2", "3"}[i],
			compare: c,
		})
		if s.caseLevel && (strength == 2 || strength == 1 && s.strength == "primary") {
			c, err := s.compareAt(t, factory, strength, true)
			if err != nil {
				return nil, err
			}
			levels = append(levels, levelComparison{
				level:   "c",
				compare: c,
			})
		}
		if s.strength == name {
			return levels, nil
		}
	}
	if s.alternate == "shifted" {
		levels = append(levels, levelComparison{
			level:   "4",
			compare: goComparison(collate.CompareRootShiftedQuaternary),
		})
	}
	if s.strength == "identical" {
		levels = append(levels, levelComparison{
			level:   "i",
			compare: goComparison(identicalOrder),
		})
	}
	return levels, nil
}

func (s collationSettings) compareAt(t *testing.T, factory ekuma.Value, strength int, withCase bool) (compareStrings, error) {
	if s.locale == "root" {
		root := collate.New("und", "standard", collate.Options{
			Strength:          strength,
			CaseFirst:         s.caseFirst,
			CaseLevel:         withCase,
			Numeric:           s.numeric,
			Backwards:         s.backwards,
			IgnorePunctuation: s.alternate == "shifted",
		})
		return goComparison(root.Compare), nil
	}
	opts := map[string]any{
		"sensitivity": []string{"base", "accent", "variant"}[strength-1],
		"numeric":     s.numeric,
	}
	if withCase {
		opts["sensitivity"] = "case"
	}
	if s.caseFirst != "" {
		opts["caseFirst"] = s.caseFirst
	}
	if s.alternate != "" {
		opts["ignorePunctuation"] = s.alternate == "shifted"
	}
	id := s.locale
	if before, ok := strings.CutSuffix(id, "-u-co-search"); ok {
		id = before
		opts["usage"] = "search"
	}
	fn, err := factory.Invoke(t.Context(), id, opts)
	if err != nil {
		return nil, err
	}
	return func(a, b wtf8.String) (int, error) {
		v, err := fn.Invoke(t.Context(), jsCollationString(a), jsCollationString(b))
		if err != nil {
			return 0, err
		}
		if v.Type() != ekuma.TypeNumber || math.IsNaN(v.Float()) {
			return 0, fmt.Errorf("compare returned %v", v)
		}
		return cmp.Compare(v.Float(), 0), nil
	}, nil
}

func jsCollationString(s wtf8.String) any {
	if wtf8.IsWellFormed(s) {
		return string(s)
	}
	var units []any
	for _, u := range wtf8.AppendUTF16(nil, s) {
		units = append(units, u)
	}
	return units
}

func compareCollationLevels(levels []levelComparison, a, b wtf8.String) (string, error) {
	relation := "="
	for _, l := range levels {
		if (l.level == "4" || l.level == "i") && relation != "=" {
			break
		}
		got, err := l.compare(a, b)
		if err != nil {
			return "", err
		}
		sign := orderSign(got)
		if relation != "=" && sign != relation[:1] {
			return "", fmt.Errorf("got %s at level %s, want %s after %s", sign, l.level, relation[:1], relation)
		}
		if relation == "=" && got != 0 {
			relation = sign + l.level
		}
	}
	return relation, nil
}

func (c *collationResults) checkLevels(t *testing.T, line, prevLine int, settings string, levels []levelComparison, a, b wtf8.String, want string) {
	got, err := compareCollationLevels(levels, a, b)
	reverse, reverseErr := compareCollationLevels(levels, b, a)
	if err != nil || reverseErr != nil {
		c.compared++
		delete(c.failures, line)
		t.Errorf("%s:%d (previous line %d): %s: %+q vs %+q: got %q (error %v), reverse %q (error %v), want %s", c.path, line, prevLine, settings, a, b, got, err, reverse, reverseErr, want)
		return
	}
	c.check(t, line, prevLine, settings, a, b, got, reverse, want)
}

func runCollationTest(t *testing.T, path string, failures map[int]string) {
	var vm ekuma.VM
	script, err := vm.ParseScript("collation-test.js", `(id, options) => {
		if (!Intl.Collator.supportedLocalesOf(id).length) throw new Error("unsupported locale " + id);
		const compare = new Intl.Collator(id, options).compare;
		const str = s => typeof s === "string" ? s : String.fromCharCode(...s);
		return (a, b) => compare(str(a), str(b));
	}`)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := script.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	results := newCollationResults(t, path, failures)
	var settings collationSettings
	var section, title, settingsText, description string
	var rules strings.Builder
	var levels []levelComparison
	var prev wtf8.String
	var prevLine int
	forEachCollationLine(t, path, func(line int, data string) {
		directive := strings.ContainsRune("@%*", rune(data[0]))
		if section == "rules" {
			if !directive {
				rules.WriteString(data)
				return
			}
			settings = ruleCollationSettings(rules.String())
			if settings.unsupported == "" && rules.Len() > 0 {
				settingsText += " " + rules.String()
			}
			rules.Reset()
		}
		switch {
		case strings.HasPrefix(data, "** test: "):
			title = strings.TrimPrefix(data, "** test: ")
			section = ""
		case data == "@ root" || data == "@ rules":
			settings = newCollationSettings("root")
			settingsText = data
			section = strings.TrimPrefix(data, "@ ")
		case strings.HasPrefix(data, "@ locale "):
			settings, err = localeCollationSettings(strings.TrimPrefix(data, "@ locale "))
			if err != nil {
				t.Fatalf("%s:%d: %v", path, line, err)
			}
			section, settingsText = "", data
		case strings.HasPrefix(data, "% "):
			if err := settings.setAttribute(strings.TrimPrefix(data, "% ")); err != nil {
				t.Fatalf("%s:%d: %v", path, line, err)
			}
			settingsText += " " + data
			section = ""
		case data == "* compare":
			section = "compare"
			prev, prevLine = "", line
			if settings.locale == "" {
				t.Fatalf("%s:%d: missing collator", path, line)
			}
			description = fmt.Sprintf("%s (%s, strength=%s)", title, settingsText, settings.strength)
			if settings.exclusion() == "" {
				levels, err = settings.comparisons(t, factory)
				if err != nil {
					t.Fatalf("%s:%d: %s: %v", path, line, description, err)
				}
			}
		case directive:
			t.Fatalf("%s:%d: unknown directive %q", path, line, data)
		case section == "compare":
			fields := strings.FieldsFunc(data, func(r rune) bool { return r == ' ' || r == '\t' || r == '　' })
			if len(fields) != 2 {
				t.Fatalf("%s:%d: invalid comparison %q", path, line, data)
			}
			want := fields[0]
			switch want {
			case "=", "<", "<1", "<2", "<c", "<3", "<4", "<i":
			default:
				t.Fatalf("%s:%d: unknown relation %q", path, line, want)
			}
			curr, err := parseCollationString(fields[1])
			if err != nil {
				t.Fatalf("%s:%d: %v", path, line, err)
			}
			results.rows++
			if why := settings.exclusion(); why != "" {
				results.excluded[why]++
			} else if settings.locale == "root" && (!wtf8.IsWellFormed(prev) || !wtf8.IsWellFormed(curr)) {
				results.excluded[unpairedReason]++
			} else {
				results.checkLevels(t, line, prevLine, description, levels, prev, curr, want)
			}
			prev, prevLine = curr, line
		default:
			t.Fatalf("%s:%d: unexpected line %q", path, line, data)
		}
	})
	results.report(t)
}
