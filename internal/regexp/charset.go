// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp

import (
	"slices"
	"sync"
	"unicode"
)

// charSet is a set of characters: sorted, disjoint and non-adjacent inclusive ranges,
// stored as the pairs lo0, hi0, lo1, hi1, ...
type charSet []rune

const maxRune = 0x10FFFF

func (s charSet) has(c rune) bool {
	return s.find(c) >= 0
}

// find returns the index in s of the lower bound of the range that holds c, or -1.
func (s charSet) find(c rune) int {
	// The sets are usually short, so a linear scan of the first pairs beats a binary search.
	if len(s) <= 8 {
		for i := 0; i+1 < len(s); i += 2 {
			if c < s[i] {
				return -1
			}
			if c <= s[i+1] {
				return i
			}
		}
		return -1
	}
	// Find the first range that does not end before c.
	lo, hi := 0, len(s)/2
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if s[2*mid+1] < c {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if i := 2 * lo; i < len(s) && s[i] <= c {
		return i
	}
	return -1
}

// normalize returns s with its pairs sorted and merged. It does not modify s, which may be shared.
func (s charSet) normalize() charSet {
	n := len(s) / 2
	if n <= 1 {
		return s
	}
	ok := true
	for i := 2; i < len(s); i += 2 {
		if s[i] <= s[i-1]+1 {
			ok = false
			break
		}
	}
	if ok {
		return s
	}
	type pair struct{ lo, hi rune }
	ps := make([]pair, n)
	for i := range ps {
		ps[i] = pair{s[2*i], s[2*i+1]}
	}
	slices.SortFunc(ps, func(a, b pair) int { return int(a.lo - b.lo) })
	out := make(charSet, 0, len(s))
	for _, p := range ps {
		if k := len(out); k > 0 && p.lo <= out[k-1]+1 {
			out[k-1] = max(out[k-1], p.hi)
			continue
		}
		out = append(out, p.lo, p.hi)
	}
	return out
}

func (s charSet) union(t charSet) charSet {
	out := make(charSet, 0, len(s)+len(t))
	out = append(out, s...)
	out = append(out, t...)
	return out.normalize()
}

// complement returns the characters from 0 to top that are not in s.
func (s charSet) complement(top rune) charSet {
	var out charSet
	next := rune(0)
	for i := 0; i < len(s); i += 2 {
		if s[i] > top {
			break
		}
		if s[i] > next {
			out = append(out, next, s[i]-1)
		}
		next = s[i+1] + 1
	}
	if next <= top {
		out = append(out, next, top)
	}
	return out
}

func (s charSet) intersect(t charSet) charSet {
	var out charSet
	i, j := 0, 0
	for i < len(s) && j < len(t) {
		lo := max(s[i], t[j])
		hi := min(s[i+1], t[j+1])
		if lo <= hi {
			out = append(out, lo, hi)
		}
		if s[i+1] < t[j+1] {
			i += 2
		} else {
			j += 2
		}
	}
	return out
}

func (s charSet) subtract(t charSet) charSet {
	return s.intersect(t.complement(maxRune))
}

// single reports the only character of s, if it has exactly one.
func (s charSet) single() (rune, bool) {
	if len(s) == 2 && s[0] == s[1] {
		return s[0], true
	}
	return 0, false
}

func setOf(r ...rune) charSet {
	s := make(charSet, 0, 2*len(r))
	for _, c := range r {
		s = append(s, c, c)
	}
	return s.normalize()
}

func fromTable(t *unicode.RangeTable) charSet {
	var s charSet
	for _, r := range t.R16 {
		if r.Stride == 1 {
			s = append(s, rune(r.Lo), rune(r.Hi))
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			s = append(s, c, c)
		}
	}
	for _, r := range t.R32 {
		if r.Stride == 1 {
			s = append(s, rune(r.Lo), rune(r.Hi))
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			s = append(s, c, c)
		}
	}
	return s.normalize()
}

var (
	digitSet = charSet{'0', '9'}
	wordSet  = charSet{'0', '9', 'A', 'Z', '_', '_', 'a', 'z'}
	spaceSet = charSet{
		'\t', '\r',
		' ', ' ',
		0xA0, 0xA0,
		0x1680, 0x1680,
		0x2000, 0x200A,
		0x2028, 0x2029,
		0x202F, 0x202F,
		0x205F, 0x205F,
		0x3000, 0x3000,
		0xFEFF, 0xFEFF,
	}
	lineTerminators = charSet{'\n', '\n', '\r', '\r', 0x2028, 0x2029}
)

// Case folding.
//
// A character class is matched case-insensitively by closing it over case equivalence
// at compile time: the closure holds every character whose canonical form is the canonical
// form of a member. The equivalence classes differ between the unicode modes, which use
// simple case folding, and the other mode, which uses the upper case mapping.

// noUpperSingle holds the characters whose full upper case mapping has more than one
// character, although their simple mapping is another single character.
// Canonicalize leaves them unchanged.
var noUpperSingle = charSet{
	0x1F80, 0x1FAF,
	0x1FB3, 0x1FB3,
	0x1FBC, 0x1FBC,
	0x1FC3, 0x1FC3,
	0x1FCC, 0x1FCC,
	0x1FF3, 0x1FF3,
	0x1FFC, 0x1FFC,
}

// canonUCS2 implements Canonicalize for a code unit when neither the u nor the v flag is set.
func canonUCS2(c rune) rune {
	if c < 0x80 {
		if 'a' <= c && c <= 'z' {
			return c - 'a' + 'A'
		}
		return c
	}
	if noUpperSingle.has(c) {
		return c
	}
	u := unicode.ToUpper(c)
	if u < 0x80 || u > 0xFFFF {
		return c
	}
	return u
}

// canonUnicode returns a canonical representative of the simple case folding class of c:
// its smallest member.
func canonUnicode(c rune) rune {
	if c < 0x80 {
		if 'a' <= c && c <= 'z' {
			return c - 'a' + 'A'
		}
		return c
	}
	m := c
	for f := unicode.SimpleFold(c); f != c; f = unicode.SimpleFold(f) {
		m = min(m, f)
	}
	return m
}

// foldClasses lists, for one canonicalization, the characters that are equivalent to
// other characters, grouped by class.
type foldClasses struct {
	// members holds every character of a class of at least two characters, sorted.
	members charSet
	// classOf maps each such character to its class.
	classOf map[rune][]rune
}

var (
	ucs2Folds    foldClasses
	ucs2Once     sync.Once
	unicodeFolds foldClasses
	unicodeOnce  sync.Once
)

func add(byCanon map[rune][]rune, c, k rune) {
	cl := byCanon[k]
	if slices.Contains(cl, c) {
		return
	}
	if len(cl) == 0 && k != c {
		cl = append(cl, k)
	}
	byCanon[k] = append(cl, c)
}

func buildFolds(top rune, canon func(rune) rune) foldClasses {
	byCanon := map[rune][]rune{}
	// No character after U+1FFFF has a case equivalent.
	for c := rune(0); c <= min(top, 0x1FFFF); c++ {
		if k := canon(c); k != c || unicode.SimpleFold(c) != c {
			add(byCanon, c, k)
		}
	}
	f := foldClasses{classOf: map[rune][]rune{}}
	for _, cl := range byCanon {
		if len(cl) < 2 {
			continue
		}
		slices.Sort(cl)
		for _, c := range cl {
			f.classOf[c] = cl
			f.members = append(f.members, c, c)
		}
	}
	f.members = f.members.normalize()
	return f
}

func folds(unicodeMode bool) *foldClasses {
	if unicodeMode {
		unicodeOnce.Do(func() {
			unicodeFolds = buildFolds(maxRune, canonUnicode)
		})
		return &unicodeFolds
	}
	ucs2Once.Do(func() {
		ucs2Folds = buildFolds(0xFFFF, canonUCS2)
	})
	return &ucs2Folds
}

// foldClosure returns s with every character that is case-equivalent to a member added.
func foldClosure(s charSet, unicodeMode bool) charSet {
	f := folds(unicodeMode)
	common := s.intersect(f.members)
	if len(common) == 0 {
		return s
	}
	out := slices.Clone(s)
	for i := 0; i < len(common); i += 2 {
		for c := common[i]; c <= common[i+1]; c++ {
			for _, e := range f.classOf[c] {
				out = append(out, e, e)
			}
		}
	}
	return out.normalize()
}

// foldEquivalents returns the characters that are case-equivalent to c, including c.
func foldEquivalents(c rune, unicodeMode bool) []rune {
	if cl, ok := folds(unicodeMode).classOf[c]; ok {
		return cl
	}
	return []rune{c}
}
