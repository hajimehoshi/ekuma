// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package unorm implements the Unicode normalization forms of UAX #15 for any length of the runs
// of non-starters.
package unorm

import (
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// cgj is U+034F COMBINING GRAPHEME JOINER.
const cgj = "͏"

// String returns the well-formed string s in the normalization form.
func String(form norm.Form, s string) string {
	t := form.String(s)
	// The package norm puts a string in the Stream-Safe Text Format of UAX #15 by inserting
	// a CGJ into a long run of non-starters, which Unicode normalization does not.
	if !strings.Contains(t, cgj) {
		return t
	}
	return normalizeRuns(form, s)
}

// normChar is a character of a decomposed string and its canonical combining class.
type normChar struct {
	r   rune
	ccc uint8
}

// normalizeRuns returns the well-formed string s in the normalization form by the algorithm of
// UAX #15, for any length of the runs of non-starters.
func normalizeRuns(form norm.Form, s string) string {
	decomposition := norm.NFD
	if form == norm.NFKC || form == norm.NFKD {
		decomposition = norm.NFKD
	}
	var chars []normChar
	for _, r := range s {
		for _, d := range decomposition.String(string(r)) {
			chars = append(chars, normChar{
				r:   d,
				ccc: decomposition.PropertiesString(string(d)).CCC(),
			})
		}
	}
	// The canonical ordering sorts each run of non-starters by combining class.
	for i := 0; i < len(chars); {
		if chars[i].ccc == 0 {
			i++
			continue
		}
		j := i
		for j < len(chars) && chars[j].ccc != 0 {
			j++
		}
		slices.SortStableFunc(chars[i:j], func(a, b normChar) int {
			return int(a.ccc) - int(b.ccc)
		})
		i = j
	}
	if form == norm.NFC || form == norm.NFKC {
		chars = composeCanonically(chars)
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range chars {
		b.WriteRune(c.r)
	}
	return b.String()
}

// composeCanonically applies the canonical composition algorithm of UAX #15 to the
// canonically ordered decomposed characters chars, in place.
func composeCanonically(chars []normChar) []normChar {
	if len(chars) == 0 {
		return chars
	}
	starter := 0
	// A non-starter at the start has no starter to compose with.
	lastCCC := 256
	if chars[0].ccc == 0 {
		lastCCC = 0
	}
	n := 1
	for _, c := range chars[1:] {
		if lastCCC == 0 || lastCCC < int(c.ccc) {
			if r, ok := primaryComposite(chars[starter].r, c.r); ok {
				chars[starter].r = r
				continue
			}
		}
		if c.ccc == 0 {
			starter = n
		}
		lastCCC = int(c.ccc)
		chars[n] = c
		n++
	}
	return chars[:n]
}

// primaryComposite returns the primary composite of the starter a and the character b, and
// reports whether there is one.
func primaryComposite(a, b rune) (rune, bool) {
	// The composition of two characters needs no Stream-Safe Text Format.
	c := norm.NFC.String(string(a) + string(b))
	r, n := utf8.DecodeRuneInString(c)
	return r, n == len(c)
}

// boundaryWindow is the number of bytes at the end of a string in which LastBoundary looks for a
// boundary.
const boundaryWindow = 256

// LastBoundary returns the offset of the last boundary of the normalization form in the last
// bytes of s other than its start, or -1 when there is none. The parts of a string between its
// boundaries normalize independently.
func LastBoundary(form norm.Form, s string) int {
	end := max(0, len(s)-boundaryWindow)
	for i := len(s) - 1; i > end; i-- {
		// A boundary is before a whole character that cannot combine with what precedes it.
		if utf8.RuneStart(s[i]) && utf8.FullRuneInString(s[i:]) && form.PropertiesString(s[i:]).BoundaryBefore() {
			return i
		}
	}
	return -1
}
