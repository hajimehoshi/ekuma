// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

type matcherGroup struct {
	name, supported, defaultLocale, favor, threshold string
}

func runLocaleMatcher(t *testing.T, path string, failures map[int]string) {
	f := newFormatFixture(t, path)
	const op = "best fit"
	f.track(op, failures)
	var group matcherGroup
	var groups int
	fixtureLines(t, path, func(line int, row string) {
		row, _, _ = strings.Cut(row, "#")
		row = strings.TrimSpace(row)
		if row == "" {
			return
		}
		if name, ok := strings.CutPrefix(row, "** test: "); ok {
			group = matcherGroup{
				name: name,
			}
			groups++
			return
		}
		if group.name == "" {
			t.Fatalf("%s:%d: missing matcher group", path, line)
		}
		if setting, ok := strings.CutPrefix(row, "@"); ok {
			key, value, ok := strings.Cut(setting, "=")
			if !ok {
				t.Fatalf("%s:%d: malformed setting %q", path, line, row)
			}
			switch key {
			case "supported":
				group.supported = value
			case "default":
				group.defaultLocale = value
			case "favor":
				switch value {
				case "", "normal", "script":
				default:
					t.Fatalf("%s:%d: unknown favor setting %q", path, line, value)
				}
				group.favor = value
			case "threshold":
				group.threshold = value
			default:
				t.Fatalf("%s:%d: unknown matcher setting %q", path, line, key)
			}
			return
		}
		request, results, ok := strings.Cut(row, ">>")
		if !ok {
			t.Fatalf("%s:%d: malformed matcher case %q", path, line, row)
		}
		// The optional second and third columns describe ICU's desired and combined locales.
		match, _, _ := strings.Cut(results, "|")
		want := strings.TrimSpace(match)
		if want == "" {
			f.excluded["no selected-locale assertion"]++
			return
		}
		supported, requested, def, reason := group.inputs(strings.TrimSpace(request))
		if reason != "" {
			f.excluded[reason]++
			return
		}
		expected, ok := locale.Canonicalize(want, nil)
		if !ok {
			t.Fatalf("%s:%d: invalid expected locale %q", path, line, want)
		}
		got, _ := locale.NewMatcher(supported).Match(requested, def, true)
		settings := fmt.Sprintf("group=%q supported=[%s] requested=[%s] default=%s favor=%s threshold=%s ICU match=%s",
			group.name, group.supported, strings.TrimSpace(request), def, cmp.Or(group.favor, "normal"), cmp.Or(group.threshold, "implicit"), want)
		f.checkValue(line, op, got, expected, settings)
	})
	t.Logf("%d groups", groups)
}

func (g matcherGroup) inputs(request string) (supported, requested []string, def, reason string) {
	switch {
	case g.threshold != "":
		return nil, nil, "", "custom threshold"
	case g.favor == "script":
		return nil, nil, "", "script preference"
	case g.supported == "" && g.defaultLocale == "":
		return nil, nil, "", "empty candidate set without a default"
	}
	var ok bool
	if supported, ok = matcherLocaleList(g.supported, true); !ok {
		return nil, nil, "", "unsupported candidate set"
	}
	if g.defaultLocale != "" {
		def, ok = locale.Canonicalize(g.defaultLocale, nil)
		if !ok || !locale.IsAvailable(def) {
			return nil, nil, "", "unavailable default"
		}
	} else {
		def = supported[0]
	}
	if requested, ok = matcherLocaleList(request, false); !ok {
		return nil, nil, "", "invalid requested locale"
	}
	return supported, requested, def, ""
}

func matcherLocaleList(list string, availableOnly bool) ([]string, bool) {
	var tags []string
	if list == "" {
		return tags, true
	}
	for item := range strings.SplitSeq(list, ",") {
		tag, ok := locale.Canonicalize(strings.TrimSpace(item), nil)
		if !ok || availableOnly && !locale.IsAvailable(tag) {
			return nil, false
		}
		tags = append(tags, tag)
	}
	return tags, true
}
