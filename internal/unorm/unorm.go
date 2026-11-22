// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package unorm implements the Unicode normalization forms of UAX #15 for any length of the runs
// of non-starters.
package unorm

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// chunk is the number of bytes of a string that String normalizes in one step when the string
// has a boundary near there.
const chunk = 1 << 16

// pollRunes is the number of characters between two calls of the poll function.
const pollRunes = 1 << 14

// String returns the well-formed string s in the normalization form. poll, which may be nil, is
// called now and then over a long s and may stop the normalization by panicking.
func String(form Form, s string, poll func()) string {
	var b strings.Builder
	for {
		n := len(s)
		if n > chunk {
			n = cut(form, s, poll)
		}
		if n == len(s) && b.Cap() == 0 {
			return normalizePart(form, s, poll)
		}
		b.Grow(len(s))
		b.WriteString(normalizePart(form, s[:n], poll))
		s = s[n:]
		if s == "" {
			return b.String()
		}
		if poll != nil {
			poll()
		}
	}
}

// cut returns the offset of a boundary of the normalization form in s, which is longer than
// chunk, other than its start: the last one in the boundaryWindow bytes before chunk, or else
// the first one from chunk on, or len(s) when there is none.
func cut(form Form, s string, poll func()) int {
	if i := LastBoundary(form, s[:chunk]); i > 0 {
		return i
	}
	for i := chunk; i < len(s); i++ {
		if poll != nil && i%chunk == 0 {
			poll()
		}
		if utf8.RuneStart(s[i]) {
			r, _ := utf8.DecodeRuneInString(s[i:])
			if boundaryBefore(form, r) {
				return i
			}
		}
	}
	return len(s)
}

// normalizePart returns the well-formed string s, which begins and ends at boundaries, in the
// normalization form.
func normalizePart(form Form, s string, poll func()) string {
	if QuickCheck(form, s, poll) {
		return s
	}
	return normalizeRuns(form, s, poll)
}

// QuickCheck reports whether the Unicode quick check confirms that s is in form.
func QuickCheck(form Form, s string, poll func()) bool {
	var last uint8
	var n int
	for _, r := range s {
		if n++; poll != nil && n%pollRunes == 0 {
			poll()
		}
		p := lookup(r)
		ccc := uint8(p)
		if !p.quickYes(form) || ccc != 0 && ccc < last {
			return false
		}
		last = ccc
	}
	return true
}

// normChar is a character of a decomposed string in its low 21 bits and its canonical combining
// class in the bits above.
type normChar uint32

const cccShift = 21

func newNormChar(r rune, ccc uint8) normChar {
	return normChar(ccc)<<cccShift | normChar(r)
}

func (c normChar) r() rune {
	return rune(c & (1<<cccShift - 1))
}

func (c normChar) ccc() uint8 {
	return uint8(c >> cccShift)
}

// normalizeRuns returns the well-formed string s in the normalization form by the algorithm of
// UAX #15, for any length of the runs of non-starters.
func normalizeRuns(form Form, s string, poll func()) string {
	chars := make([]normChar, 0, utf8.RuneCountInString(s))
	var n int
	for _, r := range s {
		if n++; poll != nil && n%pollRunes == 0 {
			poll()
		}
		chars = appendDecomposed(chars, r, form)
	}
	orderCanonically(chars, poll)
	if form == NFC || form == NFKC {
		chars = composeCanonically(chars, poll)
	}
	var b strings.Builder
	b.Grow(len(s))
	for k, c := range chars {
		if poll != nil && k%pollRunes == 0 {
			poll()
		}
		b.WriteRune(c.r())
	}
	return b.String()
}

// orderCanonically sorts each run of non-starters of chars stably by combining class.
func orderCanonically(chars []normChar, poll func()) {
	var scratch []normChar
	for i := 0; i < len(chars); {
		if chars[i].ccc() == 0 {
			i++
			continue
		}
		j := i
		for j < len(chars) && chars[j].ccc() != 0 {
			j++
		}
		run := chars[i:j]
		i = j
		if len(run) <= 32 {
			slices.SortStableFunc(run, func(a, b normChar) int {
				return int(a.ccc()) - int(b.ccc())
			})
			continue
		}
		// A counting sort keeps a long run linear.
		var counts [256]int
		for _, c := range run {
			counts[c.ccc()]++
		}
		pos := 0
		for k, n := range counts {
			counts[k] = pos
			pos += n
		}
		scratch = slices.Grow(scratch[:0], len(run))[:len(run)]
		for k, c := range run {
			if poll != nil && k%pollRunes == 0 {
				poll()
			}
			scratch[counts[c.ccc()]] = c
			counts[c.ccc()]++
		}
		copy(run, scratch)
	}
}

// composeCanonically applies the canonical composition algorithm of UAX #15 to the
// canonically ordered decomposed characters chars, in place.
func composeCanonically(chars []normChar, poll func()) []normChar {
	if len(chars) == 0 {
		return chars
	}
	starter := 0
	// A non-starter at the start has no starter to compose with.
	lastCCC := 256
	if chars[0].ccc() == 0 {
		lastCCC = 0
	}
	n := 1
	for k, c := range chars[1:] {
		if poll != nil && k%pollRunes == 0 {
			poll()
		}
		if lastCCC == 0 || lastCCC < int(c.ccc()) {
			if r, ok := primaryComposite(chars[starter].r(), c.r()); ok {
				chars[starter] = newNormChar(r, chars[starter].ccc())
				continue
			}
		}
		if c.ccc() == 0 {
			starter = n
		}
		lastCCC = int(c.ccc())
		chars[n] = c
		n++
	}
	return chars[:n]
}

// boundaryWindow is the number of bytes at the end of a string in which LastBoundary looks for a
// boundary.
const boundaryWindow = 256

// LastBoundary returns the offset of the last boundary of the normalization form in the last
// bytes of s other than its start, or -1 when there is none. The parts of a string between its
// boundaries normalize independently.
func LastBoundary(form Form, s string) int {
	end := max(0, len(s)-boundaryWindow)
	for i := len(s) - 1; i > end; i-- {
		// A boundary is before a whole character that cannot combine with what precedes it.
		if utf8.RuneStart(s[i]) && utf8.FullRuneInString(s[i:]) {
			r, _ := utf8.DecodeRuneInString(s[i:])
			if boundaryBefore(form, r) {
				return i
			}
		}
	}
	return -1
}
