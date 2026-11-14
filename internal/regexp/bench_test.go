// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/regexp"
)

// BenchmarkMatch measures typical patterns of game scripts on short strings:
// escape sequences in messages, note tags, trimming and a match that fails.
func BenchmarkMatch(b *testing.B) {
	cases := []struct {
		name    string
		pattern string
		flags   string
		input   string
	}{
		{name: "escape", pattern: `\\V\[(\d+)\]`, flags: "gi", input: `Gold: \V[12] / \V[7] pieces`},
		{name: "notetag", pattern: `<(\w+):\s*(\d+)>`, flags: "", input: "<Price: 120>\n<Weight: 3>"},
		{name: "trim", pattern: `^\s+|\s+$`, flags: "g", input: "   some text with spaces   "},
		{name: "nomatch", pattern: `[\x1b\x1c]`, flags: "", input: "a message without any control code"},
		{name: "nonascii", pattern: `「(.+?)」`, flags: "", input: "勇者は言った。「こんにちは」と。"},
	}
	for _, c := range cases {
		f, err := regexp.ParseFlags(c.flags)
		if err != nil {
			b.Fatal(err)
		}
		re, err := regexp.Compile(c.pattern, f)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(c.name, func(b *testing.B) {
			var m regexp.Matcher
			b.ReportAllocs()
			for b.Loop() {
				// Find every match, as a global replace does.
				for start := 0; start <= len(c.input); {
					caps := re.Match(&m, c.input, start, nil)
					if caps == nil {
						break
					}
					start = max(caps[1], caps[0]+1)
				}
			}
		})
	}
}
