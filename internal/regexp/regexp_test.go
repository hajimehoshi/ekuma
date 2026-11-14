// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func mustCompile(t *testing.T, pattern, flags string) *regexp.Regexp {
	t.Helper()
	f, err := regexp.ParseFlags(flags)
	if err != nil {
		t.Fatalf("ParseFlags(%q): %v", flags, err)
	}
	re, err := regexp.Compile(pattern, f)
	if err != nil {
		t.Fatalf("Compile(%q, %q): %v", pattern, flags, err)
	}
	return re
}

// units builds a WTF-8 string from UTF-16 code units, so that tests can use lone surrogates.
func units(u ...uint16) string {
	return wtf8.FromUTF16(u)
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern string
		flags   string
		input   string
		start   int
		want    []int
	}{
		// Literals, classes and the prefix and first-character scans.
		{pattern: "abc", input: "xxabcx", want: []int{2, 5}},
		{pattern: "a[bc]+d", input: "abcbd", want: []int{0, 5}},
		{pattern: "[^a-z]", input: "abC", want: []int{2, 3}},
		{pattern: "x", input: "abc", want: nil},
		// Greedy and lazy quantifiers, and giving back characters.
		{pattern: "a.*c", input: "abcbc", want: []int{0, 5}},
		{pattern: "a.*?c", input: "abcbc", want: []int{0, 3}},
		{pattern: "a{2,3}", input: "aaaa", want: []int{0, 3}},
		{pattern: "(a|ab)(c|bcd)(d*)", input: "abcd", want: []int{0, 4, 0, 1, 1, 4, 4, 4}},
		// Captures are reset on each iteration of a quantifier.
		{pattern: "(z)((a+)?(b+)?(c))*", input: "zaacbbbcac", want: []int{0, 10, 0, 1, 8, 10, 8, 9, -1, -1, 9, 10}},
		// An iteration that matches the empty string ends the loop.
		{pattern: "(a*)*b", input: "b", want: []int{0, 1, -1, -1}},
		{pattern: "(?:a|())*?b", input: "ab", want: []int{0, 2, -1, -1}},
		// Backreferences, also before their group and case-insensitive.
		{pattern: `(a+)b\1`, input: "aabaa", want: []int{0, 5, 0, 2}},
		{pattern: `\1(a)`, input: "a", want: []int{0, 1, 0, 1}},
		{pattern: `(a)\1`, flags: "i", input: "aA", want: []int{0, 2, 0, 1}},
		{pattern: `(?<x>.)\k<x>`, input: "abb", want: []int{1, 3, 1, 2}},
		{pattern: `(?:(?<x>a)|(?<x>b))\k<x>`, input: "bb", want: []int{0, 2, -1, -1, 0, 1}},
		// Lookarounds, and captures in them.
		{pattern: `a(?=(b))`, input: "ab", want: []int{0, 1, 1, 2}},
		{pattern: `a(?!b)`, input: "abac", want: []int{2, 3}},
		{pattern: `(?<=\$)\d+`, input: "a1 $23", want: []int{4, 6}},
		{pattern: `(?<!a)b`, input: "abcb", want: []int{3, 4}},
		{pattern: `(?<=(\d)(\d))x`, input: "12x", want: []int{2, 3, 0, 1, 1, 2}},
		{pattern: `(?<=\1(a))b`, input: "aab", want: []int{2, 3, 1, 2}},
		// Assertions.
		{pattern: `^b`, flags: "m", input: "a\nb", want: []int{2, 3}},
		{pattern: `a$`, input: "a\nb", want: nil},
		{pattern: `\bfoo\b`, input: "a foo.", want: []int{2, 5}},
		{pattern: `\Bo`, input: "foo", want: []int{1, 2}},
		// Sticky and the start index.
		{pattern: "a", flags: "y", input: "ba", want: nil},
		{pattern: "a", flags: "y", input: "ba", start: 1, want: []int{1, 2}},
		{pattern: "a", input: "aba", start: 1, want: []int{2, 3}},
		// Case folding: the u flag uses simple case folding, the other modes the upper case mapping.
		{pattern: "ſ", flags: "i", input: "s", want: nil},
		{pattern: "ſ", flags: "iu", input: "S", want: []int{0, 1}},
		{pattern: `\w`, flags: "iu", input: "K", want: []int{0, 1}},
		{pattern: `\w`, flags: "i", input: "K", want: nil},
		{pattern: "[a-z]+", flags: "i", input: "xABCx", want: []int{0, 5}},
		{pattern: "ᾀ", flags: "i", input: "ᾈ", want: nil},
		// Modifiers.
		{pattern: "a(?i:b)c", input: "aBc", want: []int{0, 3}},
		{pattern: "a(?i:b)c", input: "aBC", want: nil},
		{pattern: "(?-i:a)b", flags: "i", input: "aB", want: []int{0, 2}},
		{pattern: "(?s:.)", input: "\n", want: []int{0, 1}},
		// Annex B syntax.
		{pattern: `\c`, input: `\c`, want: []int{0, 2}},
		{pattern: `a{`, input: "a{", want: []int{0, 2}},
		{pattern: `\8`, input: "8", want: []int{0, 1}},
		{pattern: `\101`, input: "A", want: []int{0, 1}},
		{pattern: `[\c_]`, input: "\x1f", want: []int{0, 1}},
		{pattern: `(?=a)*`, input: "b", want: []int{0, 0}},
		// Unicode property escapes and the v flag.
		{pattern: `\p{Lu}+`, flags: "u", input: "aBÇd", want: []int{1, 3}},
		{pattern: `\p{Script=Greek}`, flags: "u", input: "aβ", want: []int{1, 2}},
		{pattern: `[\p{L}--[a-z]]`, flags: "v", input: "abC", want: []int{2, 3}},
		{pattern: `[[a-z]&&[^aeiou]]+`, flags: "v", input: "aebcd", want: []int{2, 5}},
		{pattern: `[\q{abc|d}x]`, flags: "v", input: "zabc", want: []int{1, 4}},
		{pattern: `^\p{RGI_Emoji}$`, flags: "v", input: "\U0001F468‍\U0001F469‍\U0001F467", want: []int{0, 8}},
	}
	var m regexp.Matcher
	for _, tc := range tests {
		re := mustCompile(t, tc.pattern, tc.flags)
		got := re.Match(&m, tc.input, tc.start, nil)
		if !slices.Equal(got, tc.want) {
			t.Errorf("/%s/%s on %q from %d: got %v, want %v", tc.pattern, tc.flags, tc.input, tc.start, got, tc.want)
		}
	}
}

// TestUTF16 checks the code unit semantics over WTF-8 subjects.
func TestUTF16(t *testing.T) {
	pair := "\U0001F600" // the code units D83D DE00
	tests := []struct {
		pattern string
		flags   string
		input   string
		start   int
		want    []int
	}{
		// Without the u flag, a supplementary character is two characters.
		{pattern: "^.$", input: pair, want: nil},
		{pattern: "^..$", input: pair, want: []int{0, 2}},
		{pattern: "^.$", flags: "u", input: pair, want: []int{0, 2}},
		// A match can start and end in the middle of a surrogate pair.
		{pattern: `\uDE00`, input: "a" + pair, want: []int{2, 3}},
		{pattern: `\uD83D`, input: pair + "b", want: []int{0, 1}},
		{pattern: `.`, input: pair, start: 1, want: []int{1, 2}},
		{pattern: `\uDE00`, flags: "u", input: pair, want: nil},
		// With the u flag, a start in the middle of a pair moves to its start.
		{pattern: `.`, flags: "u", input: pair, start: 1, want: []int{0, 2}},
		// Lone surrogates are characters of their own in both modes.
		{pattern: `\uD83D`, flags: "u", input: units(0x61, 0xD83D), want: []int{1, 2}},
		{pattern: `^[😀]$`, input: pair, want: nil},
		{pattern: `^[😀]+$`, input: pair, want: []int{0, 2}},
		{pattern: `^[😀]$`, flags: "u", input: pair, want: []int{0, 2}},
		// Literal runs of the pattern that hold surrogates.
		{pattern: pair, input: "x" + pair, want: []int{1, 3}},
		{pattern: pair + "+", input: units(0xD83D, 0xDE00, 0xDE00), want: []int{0, 3}},
		// Backreferences to a capture that ends in the middle of a pair.
		{pattern: `(.)\1`, input: units(0xD83D, 0xD83D, 0xDE00), want: []int{0, 2, 0, 1}},
		// Lookbehind steps over a pair by code units or by code point.
		{pattern: `(?<=\uDE00)x`, input: pair + "x", want: []int{2, 3}},
		{pattern: `(?<=^.)x`, flags: "u", input: pair + "x", want: []int{2, 3}},
		{pattern: `(?<=^.)x`, input: pair + "x", want: nil},
		// Indices after non-ASCII characters.
		{pattern: `b`, input: "éあ" + pair + "b", want: []int{4, 5}},
		{pattern: `é+`, flags: "i", input: "xÉé", want: []int{1, 3}},
	}
	var m regexp.Matcher
	for _, tc := range tests {
		re := mustCompile(t, tc.pattern, tc.flags)
		got := re.Match(&m, tc.input, tc.start, nil)
		if !slices.Equal(got, tc.want) {
			t.Errorf("/%q/%s on %q from %d: got %v, want %v", tc.pattern, tc.flags, tc.input, tc.start, got, tc.want)
		}
	}
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct {
		pattern string
		flags   string
	}{
		{pattern: "a**"},
		{pattern: "(?<n>a)(?<n>b)"},
		{pattern: "(?<n>a)|(?<n>b)", flags: "x"},
		{pattern: "[b-a]"},
		{pattern: "a{2,1}"},
		{pattern: `\k<n>(?<m>a)`},
		{pattern: `{1}`},
		{pattern: `\c`, flags: "u"},
		{pattern: `a{`, flags: "u"},
		{pattern: `\p{Foo}`, flags: "u"},
		{pattern: `(?<=a)*`},
		{pattern: `[^\p{RGI_Emoji}]`, flags: "v"},
		{pattern: `[a&&&b]`, flags: "v"},
		{pattern: `(?ii:a)`},
		{pattern: `(?-:a)`},
		{pattern: `(`},
		{pattern: `\`},
	}
	for _, tc := range tests {
		f, err := regexp.ParseFlags(tc.flags)
		if err == nil {
			_, err = regexp.Compile(tc.pattern, f)
		}
		if !errors.Is(err, regexp.ErrSyntax) {
			t.Errorf("/%s/%s: got %v, want a syntax error", tc.pattern, tc.flags, err)
		}
	}
}

type countPoller struct {
	n int
}

type stop struct{}

func (p *countPoller) Poll() {
	p.n++
	panic(stop{})
}

// TestPoll checks that a catastrophic pattern can be stopped.
func TestPoll(t *testing.T) {
	re := mustCompile(t, "^(a+)+$", "")
	input := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"
	p := &countPoller{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(stop); !ok {
					panic(r)
				}
			}
		}()
		var m regexp.Matcher
		re.Match(&m, input, 0, p)
	}()
	if p.n != 1 {
		t.Errorf("Poll was called %d times, want 1", p.n)
	}
}
