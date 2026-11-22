// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func mustCompile(t *testing.T, pattern, flags wtf8.String) *regexp.Regexp {
	t.Helper()
	f, err := regexp.ParseFlags(flags)
	if err != nil {
		t.Fatalf("ParseFlags(%q): %v", flags, err)
	}
	re, err := regexp.Compile(pattern, f, nil)
	if err != nil {
		t.Fatalf("Compile(%q, %q): %v", pattern, flags, err)
	}
	return re
}

// units builds a WTF-8 string from UTF-16 code units, so that tests can use lone surrogates.
func units(u ...uint16) wtf8.String {
	return wtf8.FromUTF16(u)
}

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern wtf8.String
		flags   wtf8.String
		input   wtf8.String
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
		{
			pattern: `(.)\1`,
			flags:   "iu",
			input:   "ɷ\ua7dd",
			want:    []int{0, 2, 0, 1},
		},
		{pattern: `(?<x>.)\k<x>`, input: "abb", want: []int{1, 3, 1, 2}},
		{pattern: `(?:(?<x>a)|(?<x>b))\k<x>`, input: "bb", want: []int{0, 2, -1, -1, 0, 1}},
		// Lookarounds, and captures in them.
		{
			pattern: `(?!^a)a`,
			input:   "aa",
			want:    []int{1, 2},
		},
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
	const pair = "\U0001F600" // the code units D83D DE00
	tests := []struct {
		pattern wtf8.String
		flags   wtf8.String
		input   wtf8.String
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
		// A class repetition over a long subject of characters of every length.
		{pattern: `[^x]+`, flags: "u", input: "x" + wtf8.String(strings.Repeat("aé日"+pair, 4)) + "x", want: []int{1, 21}},
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
		pattern wtf8.String
		flags   wtf8.String
	}{
		{pattern: "a**"},
		{pattern: "(?<n>a)(?<n>b)"},
		{pattern: "(?<n>a)|(?<n>b)", flags: "x"},
		{pattern: "[b-a]"},
		{pattern: "a{2,1}"},
		{pattern: "a{2147483648,2147483647}"},
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
		{pattern: wtf8.String(strings.Repeat("(?:", 100000) + "a" + strings.Repeat(")", 100000))},
		{pattern: wtf8.String(strings.Repeat("[", 100000) + "a" + strings.Repeat("]", 100000)), flags: "v"},
	}
	for _, tc := range tests {
		f, err := regexp.ParseFlags(tc.flags)
		if err == nil {
			_, err = regexp.Compile(tc.pattern, f, nil)
		}
		if !errors.Is(err, regexp.ErrSyntax) {
			t.Errorf("/%.40s/%s: got %v, want a syntax error", tc.pattern, tc.flags, err)
		}
	}
}

// TestManyDuplicateNames checks patterns with many groups of one name.
func TestManyDuplicateNames(t *testing.T) {
	var b strings.Builder
	for i := range 1 << 18 {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprintf(&b, "(?<g>a%d)", i)
	}
	alts := wtf8.String(b.String())
	if err := regexp.Validate(alts, "", nil); err != nil {
		t.Errorf("groups in different alternatives: got %v, want nil", err)
	}
	if err := regexp.Validate("(?:"+alts+")(?<g>b)", "", nil); !errors.Is(err, regexp.ErrSyntax) {
		t.Errorf("a group after the alternatives: got %v, want a syntax error", err)
	}
}

// TestManyAlternatives checks a pattern of many alternatives that start with different
// characters.
func TestManyAlternatives(t *testing.T) {
	var b strings.Builder
	for i := range 1 << 17 {
		if i > 0 {
			b.WriteByte('|')
		}
		fmt.Fprintf(&b, "%c%d", 0x4E00+2*(i%10000), i)
	}
	re := mustCompile(t, wtf8.String(b.String()), "")
	var m regexp.Matcher
	if got := re.Find(&m, "x\u4E022y", 0, nil); got != nil {
		t.Errorf("got %v, want no match", got)
	}
	if got := re.Find(&m, "x\u4E021", 0, nil); !slices.Equal(got, []int{1, 3}) {
		t.Errorf("got %v, want [1 3]", got)
	}
}

// TestManyClassStrings checks a class of the v flag with many strings.
func TestManyClassStrings(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	for i := range 100000 {
		fmt.Fprintf(&b, "\\q{x%d}", i)
	}
	b.WriteString("a]")
	re := mustCompile(t, wtf8.String(b.String()), "v")
	var m regexp.Matcher
	if got := re.Find(&m, "-x99999-", 0, nil); !slices.Equal(got, []int{1, 7}) {
		t.Errorf("got %v, want [1 7]", got)
	}
}

// FuzzValidate checks that Validate reports the error that Compile reports.
func FuzzValidate(f *testing.F) {
	for _, p := range []string{
		// Valid in some modes.
		"", "abc", "a|b|", `^a$\b\B`, ".", "a*?b+c?d{2}e{2,}f{2,3}?", "a{", "{", "}", "]",
		"(a)(?:b)(?=c)(?!d)(?<=e)(?<!f)", "(?=a)*", "(?i:a)(?-i:b)(?im-s:c)",
		`(?<n>a)\k<n>`, `(?<n>a)|(?<n>b)\k<n>`, `(a)\1`, `\2(a)`, `\k`, `\k<a>`, `(?<a>a)`,
		`[a-z\d\W-]`, `[^\s]`, `[\b\-]`, `[\w-a]`, `\c`, `[\c1]`, `[\c]`, `\0\01\8`, `\x41A`,
		`\u{1F600}`, `😀`, "😀[😀-😁]", string(units(0xD83D, '[', 0xDE00, ']')),
		`\p{L}`, `\P{Script=Greek}`, `\p{Script_Extensions=Latin}`, `\p{RGI_Emoji}`, `\w\W\d\D\s\S`,
		`[\p{RGI_Emoji}--\q{x}]`, `[[a-z]&&[aeiou]]`, `[\q{abc|d|}]`, `[^[a-z]--b]`, `[\P{L}a]`,
		// Invalid in every mode.
		"a**", "*", "(", ")", `\`, "[", "a{2,1}", "a{2147483648,2147483647}", "[b-a]",
		"(?<n>a)(?<n>b)", `\k<m>(?<n>a)`, "(?<1>a)", "(?<a", "(?<=a)*", "(?ii:a)", "(?-:a)", "(?x)",
		strings.Repeat("(?:", 100000) + "a" + strings.Repeat(")", 100000),
		strings.Repeat("[", 100000) + "a" + strings.Repeat("]", 100000),
		// Invalid with the u or v flag.
		`\u{110000}`, `\x`, `\p{Foo}`, `\p{Script=Foo}`, `\P{RGI_Emoji}`,
		`[^\q{ab}]`, `[^\p{RGI_Emoji}]`, `[a&&&b]`, `[a-z&&b]`, `[a--b&&c]`, `[\q{a]`, `[(]`,
		`[a!!b]`, `[\q{a|b]`,
	} {
		f.Add(p)
	}
	flagSets := []wtf8.String{"", "i", "m", "s", "u", "iu", "v", "iv", "uv"}
	f.Fuzz(func(t *testing.T, pattern string) {
		p := wtf8.FromValidUTF8(pattern)
		for _, flags := range flagSets {
			got := regexp.Validate(p, flags, nil)
			fl, want := regexp.ParseFlags(flags)
			if want == nil {
				_, want = regexp.Compile(p, fl, nil)
			}
			if !sameError(got, want) {
				t.Errorf("Validate(%.40q, %q): got %v, want %v", pattern, flags, got, want)
			}
		}
	})
}

// sameError reports whether a and b are both nil or both equal values of *regexp.Error.
func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var ea, eb *regexp.Error
	return errors.As(a, &ea) && errors.As(b, &eb) && *ea == *eb
}

type countPoller struct {
	n int
}

type stop struct{}

func (p *countPoller) Poll() {
	p.n++
	panic(stop{})
}

// TestCompilePoll checks that the compilation and the validation of a long pattern can be
// stopped.
func TestCompilePoll(t *testing.T) {
	for _, pattern := range []wtf8.String{
		wtf8.String(strings.Repeat("a", 1<<20)),
		wtf8.String(strings.Repeat("(?:a|b)", 1<<17)),
	} {
		p := &countPoller{}
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(stop); !ok {
						panic(r)
					}
				}
			}()
			regexp.Compile(pattern, 0, p)
		}()
		if p.n != 1 {
			t.Errorf("/%.20s.../: Poll was called %d times, want 1", pattern, p.n)
		}
	}
	p := &countPoller{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(stop); !ok {
					panic(r)
				}
			}
		}()
		regexp.Validate(wtf8.String(strings.Repeat("a", 1<<20)), "", p)
	}()
	if p.n != 1 {
		t.Errorf("Validate: Poll was called %d times, want 1", p.n)
	}
}

// TestPoll checks that a catastrophic pattern can be stopped.
func TestPoll(t *testing.T) {
	re := mustCompile(t, "^(a+)+$", "")
	var input wtf8.String = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"
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

// FuzzRequiredLiteral compares matching with the same pattern in an alternation, where no
// literal is required.
func FuzzRequiredLiteral(f *testing.F) {
	atoms := []string{"a", "b", "ab", "abc", "x", "xyz", "1", ".", `\d`, `\w`, `\s`, "[ab]", "[^a]", "(a)", "(ab|b)", "(?:a|bc)", "a*", "b+", "a?", ".*", ".*?", ".{2}", `\d{1,3}`, "b{2,}", "(a|)", "^", "$", `\b`, "(?=b)", "(?!a)", "(?<=a)", "(?<!b)", `\1`, "é", "😀", `\uD83D`, `\uDE00`, "(?:ab){1,2}", "a{0,2}", "(x)?", "(?:)", "[a-c]{2,}", "日本", "(?<n>b)", `\k<n>`, "(?i:b)", `[\q{ab|c}]`}
	inputs := []string{"", "a", "abc", "aabbcc", "xabcabcx", "ababab", "aaa bbb ccc abc", "x1 xyz 12 xyz9", "é日本ab😀abc", "\x01ab\x02c\x01\x02", "ccab\nabcc", "xxabab日本abc", "ABCabcAbC xYz"}
	seed := uint32(1)
	rnd := func(n int) int {
		seed = seed*1103515245 + 12345
		return int(seed>>8) % n
	}
	for range 300 {
		var p strings.Builder
		for range 1 + rnd(5) {
			p.WriteString(atoms[rnd(len(atoms))])
			if rnd(8) == 0 {
				p.WriteString("|")
			}
		}
		f.Add(p.String(), inputs[rnd(len(inputs))])
	}
	// Literals at the greatest distance from the start of a match.
	f.Add(".{2}xyz", "a\nbxyz")
	f.Add(`\d{1,3}(?:a|bc)x`, "1234bcx")
	f.Add(`(?<=b)(?:a|日本)x`, "b日本x")
	flagSets := []wtf8.String{"", "g", "i", "m", "s", "u", "y", "iu", "uy", "v", "iv"}
	f.Fuzz(func(t *testing.T, pattern, input string) {
		p := wtf8.FromValidUTF8(pattern)
		// \x01 and \x02 stand for lone surrogates.
		var us []uint16
		for _, r := range input {
			switch r {
			case 1:
				us = append(us, 0xD83D)
			case 2:
				us = append(us, 0xDE00)
			default:
				if r >= 0x10000 {
					r -= 0x10000
					us = append(us, uint16(0xD800+r>>10), uint16(0xDC00+r&0x3FF))
				} else {
					us = append(us, uint16(r))
				}
			}
		}
		s := wtf8.FromUTF16(us)
		var m regexp.Matcher
		for _, flags := range flagSets {
			fl, err := regexp.ParseFlags(flags)
			if err != nil {
				t.Fatal(err)
			}
			re, err := regexp.Compile(p, fl, nil)
			if err != nil {
				continue
			}
			ref, err := regexp.Compile("(?:"+p+`)|[^\s\S]`, fl, nil)
			if err != nil {
				continue
			}
			for start := 0; start <= len(us); start++ {
				got := slices.Clone(re.Match(&m, s, start, nil))
				want := ref.Match(&m, s, start, nil)
				if !slices.Equal(got, want) {
					t.Errorf("Match /%s/%s on %q from %d: got %v, want %v", pattern, flags, input, start, got, want)
				}
				got = slices.Clone(re.Find(&m, s, start, nil))
				want = ref.Find(&m, s, start, nil)
				if !slices.Equal(got, want) {
					t.Errorf("Find /%s/%s on %q from %d: got %v, want %v", pattern, flags, input, start, got, want)
				}
			}
		}
	})
}
