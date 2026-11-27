// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma"
)

func runCaseFoldingTest(t *testing.T, path string) {
	var vm ekuma.VM
	ctor, err := vm.Global().Get(t.Context(), "RegExp")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	forEachDataRow(t, path, 4, func(line int, fields []string) {
		source, status := fields[0], fields[1]
		cp, err := parseScalar(source)
		if err != nil {
			t.Fatalf("%s:%d: fold %s: %v", path, line, status, err)
		}
		mapping, err := parseCodePoints(fields[2])
		if err != nil || fields[3] != "" {
			t.Fatalf("%s:%d: invalid fold %s for U+%s: %q", path, line, status, source, fields[2:])
		}
		var want bool
		switch status {
		case "C", "S":
			want = true
		case "F", "T":
			// Full expansions and the two special Turkic mappings are not
			// equivalences under default Unicode simple case folding.
		default:
			t.Fatalf("%s:%d: unknown fold status %q for U+%s", path, line, status, source)
		}
		if (utf8.RuneCountInString(mapping) > 1) != (status == "F") {
			t.Fatalf("%s:%d: unexpected mapping length for %s fold U+%s -> %s", path, line, status, source, fields[2])
		}
		pattern := fmt.Sprintf(`^\u{%X}$`, cp)
		for _, flags := range []string{"iu", "iv"} {
			re, err := ctor.New(t.Context(), pattern, flags)
			if err != nil {
				t.Errorf("%s:%d: %s fold /%s/%s on %+q: %v", path, line, status, pattern, flags, mapping, err)
				continue
			}
			got, err := re.Call(t.Context(), "test", mapping)
			if err != nil || !got.Equal(vm.ValueOf(want)) {
				t.Errorf("%s:%d: %s fold /%s/%s on %+q: got %s (error %v), want %t", path, line, status, pattern, flags, mapping, got, err, want)
			}
		}
		counts[status]++
	})
	t.Logf("%d common, %d simple, %d full, %d Turkic rows checked in iu and iv modes; 0 known failures", counts["C"], counts["S"], counts["F"], counts["T"])
}

func runEmojiTest(t *testing.T, path string) {
	var vm ekuma.VM
	ctor, err := vm.Global().Get(t.Context(), "RegExp")
	if err != nil {
		t.Fatal(err)
	}
	// The six component properties of RGI_Emoji have disjoint sequence shapes.
	properties := []string{
		"Basic_Emoji", "Emoji_Keycap_Sequence", "RGI_Emoji_Modifier_Sequence",
		"RGI_Emoji_Flag_Sequence", "RGI_Emoji_Tag_Sequence", "RGI_Emoji_ZWJ_Sequence", "RGI_Emoji",
	}
	regexps := map[string]ekuma.Value{}
	var cases int
	forEachDataRow(t, path, 3, func(line int, fields []string) {
		codes, property := fields[0], fields[1]
		if len(regexps) == 0 {
			for _, name := range properties {
				re, err := ctor.New(t.Context(), `^\p{`+name+`}$`, "v")
				if err != nil {
					t.Fatalf("%s:%d: compile %s for %s: %v", path, line, name, codes, err)
				}
				regexps[name] = re
			}
		}
		_, ok := regexps[property]
		if !ok || property == "RGI_Emoji" || (property == "RGI_Emoji_ZWJ_Sequence") != (filepath.Base(path) == "emoji-zwj-sequences.txt") {
			t.Fatalf("%s:%d: unexpected property %q for %s", path, line, property, codes)
		}
		check := func(input string) {
			for _, name := range properties {
				want := name == property || name == "RGI_Emoji"
				got, err := regexps[name].Call(t.Context(), "test", input)
				if err != nil || !got.Equal(vm.ValueOf(want)) {
					t.Errorf("%s:%d: /^\\p{%s}$/v on %+q (%s): got %s (error %v), want %t", path, line, name, input, property, got, err, want)
				}
			}
			cases++
		}
		if lo, hi, ok := strings.Cut(codes, ".."); ok {
			first, err1 := parseScalar(lo)
			last, err2 := parseScalar(hi)
			if err1 != nil || err2 != nil || last < first || property != "Basic_Emoji" || first < 0xd800 && last > 0xdfff {
				t.Fatalf("%s:%d: invalid %s range %q", path, line, property, codes)
			}
			for r := first; r <= last; r++ {
				check(string(r))
			}
			return
		}
		input, err := parseCodePoints(codes)
		if err != nil {
			t.Fatalf("%s:%d: %s: %v", path, line, property, err)
		}
		check(input)
	})
	t.Logf("%d sequences checked against all %d properties; 0 known failures", cases, len(properties))
}
