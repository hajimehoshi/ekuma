// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ToLowerCase converts s[start:end] to lowercase with the root locale's full case mapping, in the
// context of the rest of s. s must be valid UTF-8, and start and end must be rune boundaries.
func ToLowerCase(s string, start, end int) string {
	var b strings.Builder
	b.Grow(end - start)
	for k, r := range s[start:end] {
		i := start + k
		switch r {
		case '\u0130':
			// LATIN CAPITAL LETTER I WITH DOT ABOVE keeps the dot as a combining character.
			b.WriteString("i\u0307")
		case '\u03A3':
			if isFinalSigma(s, i) {
				b.WriteRune('\u03C2')
			} else {
				b.WriteRune('\u03C3')
			}
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// isFinalSigma reports whether the character at byte offset i of s satisfies the Final_Sigma casing context.
func isFinalSigma(s string, i int) bool {
	// Before: a cased letter followed by zero or more case-ignorable characters.
	// A character that is both cased and case-ignorable counts as case-ignorable.
	var before bool
	for j := i; j > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
		if isCaseIgnorable(r) {
			continue
		}
		before = isCased(r)
		break
	}
	if !before {
		return false
	}
	// After: not zero or more case-ignorable characters followed by a cased letter.
	_, size := utf8.DecodeRuneInString(s[i:])
	for _, r := range s[i+size:] {
		if isCaseIgnorable(r) {
			continue
		}
		return !isCased(r)
	}
	return true
}

// isCased reports whether r has the Unicode Cased property.
func isCased(r rune) bool {
	return unicode.In(r, unicode.Lu, unicode.Ll, unicode.Lt, unicode.Other_Lowercase, unicode.Other_Uppercase)
}

// isCaseIgnorable reports whether r has the Unicode Case_Ignorable property.
func isCaseIgnorable(r rune) bool {
	switch r {
	// Word_Break values MidLetter, MidNumLet and Single_Quote.
	case '\'', '.', ':', '\u00B7', '\u0387', '\u055F', '\u05F4', '\u2018', '\u2019', '\u2024', '\u2027',
		'\uFE13', '\uFE52', '\uFE55', '\uFF07', '\uFF0E', '\uFF1A':
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf, unicode.Lm, unicode.Sk)
}

// ToLowerASCII returns s with the ASCII upper-case letters in lower case.
func ToLowerASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}
