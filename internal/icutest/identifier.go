// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"errors"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func runIdentifierBoundaries(t *testing.T, path string) {
	points := []rune{
		'$', '_', 0x200b, 0x200c, 0x200d, 0x200e,
		0x0888, 0x0889, 0x088f, 0x0890, // Arabic letters ending at a Unicode 17 addition.
		0x1e6df, 0x1e6e0, 0x1e6e2, 0x1e6e3, 0x1e6e4, // Tai Yo letters separated by a combining mark.
		0x3134f, 0x31350, 0x33479, 0x3347a, // CJK Extensions H and J form one ID range.
	}
	var vm ekuma.VM
	ctor, err := vm.Global().Get("RegExp")
	if err != nil {
		t.Fatal(err)
	}
	for _, property := range []string{"ID_Start", "ID_Continue"} {
		members := identifierMembers(t, path, property, points)
		pattern := `^\p{` + property + `}$`
		re, err := ctor.New(t.Context(), pattern, "v")
		if err != nil {
			t.Fatal(err)
		}
		for _, cp := range points {
			want := members[cp]
			got, err := re.Call(t.Context(), "test", string(cp))
			if err != nil || !got.Equal(vm.ValueOf(want)) {
				t.Errorf("%U /%s/v: got %v (error %v), want %t", cp, pattern, got, err, want)
			}

			name := string(cp)
			allowed := want || cp == '$' || cp == '_'
			if property == "ID_Continue" {
				name = "a" + name
				allowed = allowed || cp == 0x200c || cp == 0x200d
			}
			// A character skipped as whitespace after "a" would redeclare the binding.
			src := "const a = 0, " + name + " = 1;"
			_, err = ekuma.ParseUnboundScript("identifier.js", src)
			if allowed {
				if err != nil {
					t.Errorf("%U %s in %+q: want valid identifier, got %v", cp, property, src, err)
				}
			} else if _, ok := errors.AsType[*ekuma.SyntaxError](err); !ok {
				t.Errorf("%U %s in %+q: want SyntaxError, got %v", cp, property, src, err)
			}
		}
		t.Logf("%s: %d RegExp v checks and %d binding identifier checks", property, len(points), len(points))
	}
}

func identifierMembers(t *testing.T, path, property string, points []rune) map[rune]bool {
	t.Helper()
	members := make(map[rune]bool)
	var rows int
	fixtureLines(t, path, func(line int, row string) {
		row, _, _ = strings.Cut(row, "#")
		field, prop, _ := strings.Cut(row, ";")
		if strings.TrimSpace(prop) != property {
			return
		}
		first, last, ranged := strings.Cut(strings.TrimSpace(field), "..")
		if !ranged {
			last = first
		}
		lo, errLo := parseScalar(first)
		hi, errHi := parseScalar(last)
		if errLo != nil || errHi != nil || lo > hi {
			t.Fatalf("%s:%d: invalid %s range %q", path, line, property, field)
		}
		for _, cp := range points {
			if lo <= cp && cp <= hi {
				members[cp] = true
			}
		}
		rows++
	})
	if rows == 0 {
		t.Fatalf("%s contains no %s ranges", path, property)
	}
	return members
}
