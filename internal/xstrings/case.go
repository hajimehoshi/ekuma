// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// pollRunes is the number of characters that a case mapping reads between two calls of its poll
// function.
const pollRunes = 1 << 14

//go:generate go run gen_case.go

// A caseRange maps every stride-th rune from lo through hi.
type caseRange struct {
	lo, hi    rune
	stride    rune
	delta     rune
	expansion string
}

type caseTable []caseRange

func (ranges caseTable) lookup(r rune) *caseRange {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	if i == len(ranges) {
		return nil
	}
	m := &ranges[i]
	if r < m.lo || (r-m.lo)%m.stride != 0 {
		return nil
	}
	return m
}

// ToUpperCase returns the full default uppercase mapping of a valid UTF-8 string.
func ToUpperCase(s string) string {
	return mapCase(s, 0, len(s), true, nil)
}

// ToLowerCase maps s[start:end] to full default lowercase in the context of s.
// s must be valid UTF-8 and both offsets must be rune boundaries.
// A non-nil poll may interrupt a long mapping.
func ToLowerCase(s string, start, end int, poll func()) string {
	return mapCase(s, start, end, false, poll)
}

func mapCase(s string, start, end int, upper bool, poll func()) string {
	ranges := lowerCaseRanges
	if upper {
		ranges = upperCaseRanges
	}
	var b strings.Builder
	copied := start
	var n int
	for k, r := range s[start:end] {
		if n++; poll != nil && n%pollRunes == 0 {
			poll()
		}
		m := ranges.lookup(r)
		if m == nil {
			continue
		}
		i := start + k
		if b.Cap() == 0 {
			b.Grow(end - start)
		}
		b.WriteString(s[copied:i])
		switch {
		case !upper && r == 'Σ' && isFinalSigma(s, i, poll):
			b.WriteRune('ς')
		case m.expansion != "":
			b.WriteString(m.expansion)
		default:
			b.WriteRune(r + m.delta)
		}
		copied = i + utf8.RuneLen(r)
	}
	if copied == start {
		return s[start:end]
	}
	b.WriteString(s[copied:end])
	return b.String()
}

// isFinalSigma reports whether the character at byte offset i of s satisfies the Final_Sigma
// casing context. poll is as for [ToLowerCase].
func isFinalSigma(s string, i int, poll func()) bool {
	_, size := utf8.DecodeRuneInString(s[i:])
	return EndsWithCasedLetter(s[:i], poll) && !StartsWithCasedLetter(s[i+size:], poll)
}

// EndsWithCasedLetter reports whether s ends in a cased letter after ignoring trailing
// case-ignorable characters. poll is as for [ToLowerCase].
func EndsWithCasedLetter(s string, poll func()) bool {
	// A character that is both cased and case-ignorable counts as case-ignorable.
	var n int
	for len(s) > 0 {
		if n++; n%pollRunes == 0 && poll != nil {
			poll()
		}
		r, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
		if inCaseProperty(r, caseIgnorableBounds) {
			continue
		}
		return inCaseProperty(r, casedBounds)
	}
	return false
}

// StartsWithCasedLetter reports whether s starts with a cased letter after ignoring leading
// case-ignorable characters. poll is as for [ToLowerCase].
func StartsWithCasedLetter(s string, poll func()) bool {
	var n int
	for _, r := range s {
		if n++; n%pollRunes == 0 && poll != nil {
			poll()
		}
		if inCaseProperty(r, caseIgnorableBounds) {
			continue
		}
		return inCaseProperty(r, casedBounds)
	}
	return false
}

// inCaseProperty reports whether r is in a half-open range delimited by bounds.
func inCaseProperty(r rune, bounds []rune) bool {
	i := sort.Search(len(bounds), func(i int) bool { return bounds[i] > r })
	return i%2 != 0
}
