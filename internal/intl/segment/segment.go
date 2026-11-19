// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package segment implements the grapheme cluster, word and sentence boundaries of UAX #29.
package segment

import (
	"encoding/binary"
	"unicode"
)

// Granularity is the kind of segments.
type Granularity int

const (
	Grapheme Granularity = iota
	Word
	Sentence
)

// Grapheme_Cluster_Break values.
const (
	gOther = iota
	gCR
	gLF
	gControl
	gExtend
	gZWJ
	gRI
	gPrepend
	gSpacingMark
	gL
	gV
	gT
	gLV
	gLVT
)

// Word_Break values.
const (
	wOther = iota
	wCR
	wLF
	wNewline
	wExtend
	wZWJ
	wRI
	wFormat
	wKatakana
	wHebrewLetter
	wALetter
	wSingleQuote
	wDoubleQuote
	wMidNumLet
	wMidLetter
	wMidNum
	wNumeric
	wExtendNumLet
	wWSegSpace
)

// Sentence_Break values.
const (
	sOther = iota
	sCR
	sLF
	sExtend
	sSep
	sFormat
	sSp
	sLower
	sUpper
	sOLetter
	sNumeric
	sATerm
	sSContinue
	sSTerm
	sClose
)

// Indic_Conjunct_Break values.
const (
	incbNone = iota
	incbConsonant
	incbLinker
	incbExtend
)

func u16(off int) uint16 {
	return binary.LittleEndian.Uint16([]byte(properties[off : off+2]))
}

// props returns the packed break properties of c.
func props(c rune) uint16 {
	if c < 0 || c > unicode.MaxRune {
		return 0
	}
	mid := int(u16(int(c>>10) * 2))
	leaf := int(u16(propertiesStage2 + (mid*32+int(c>>5)&31)*2))
	return u16(propertiesLeaves + (leaf*32+int(c)&31)*2)
}

func graphemeOf(p uint16) int    { return int(p & 0xF) }
func wordOf(p uint16) int        { return int(p >> 4 & 0x1F) }
func sentenceOf(p uint16) int    { return int(p >> 9 & 0xF) }
func pictographic(p uint16) bool { return p&(1<<13) != 0 }
func incbOf(p uint16) int        { return int(p >> 14) }

// pollSteps is the number of code points between two calls of the poll function of
// [Boundaries].
const pollSteps = 1 << 14

// Boundaries returns the boundaries of the segments of rs as indices of rs, from 0 to
// len(rs), which must be less than 2^31. An empty rs has no boundaries. Boundaries calls poll, which may be nil, now and
// then during a long input; poll may stop Boundaries by panicking.
func Boundaries(g Granularity, rs []rune, poll func()) []int32 {
	if len(rs) == 0 {
		return nil
	}
	ps := make([]uint16, len(rs))
	for i, r := range rs {
		if poll != nil && i%pollSteps == pollSteps-1 {
			poll()
		}
		ps[i] = props(r)
	}
	var gs graphemeState
	var ws wordState
	var ss sentenceState
	switch g {
	case Grapheme:
		gs.advance(ps[0])
	case Word:
		ws.advance(ps[0])
	case Sentence:
		ss.advance(ps, 0)
	}
	bs := []int32{0}
	for i := 1; i < len(rs); i++ {
		if poll != nil && i%pollSteps == 0 {
			poll()
		}
		var brk bool
		switch g {
		case Grapheme:
			brk = graphemeBreak(ps, i, &gs)
			gs.advance(ps[i])
		case Word:
			brk = wordBreak(ps, i, &ws)
			ws.advance(ps[i])
		case Sentence:
			brk = sentenceBreak(ps, i, &ss)
			ss.advance(ps, i)
		}
		if brk {
			bs = append(bs, int32(i))
		}
	}
	return append(bs, int32(len(rs)))
}

// Indic conjunct states of graphemeState.
const (
	conjNone = iota
	// conjConsonant is after Consonant [Extend Linker]* without a Linker.
	conjConsonant
	// conjLinked is after Consonant [Extend Linker]* with a Linker.
	conjLinked
)

// graphemeState summarizes the code points before a position for the rules of
// graphemeBreak that look back over runs.
type graphemeState struct {
	conj int
	// pict and prevPict report whether the code points up to the last and up to the one
	// before the last end in ExtPict Extend*.
	pict     bool
	prevPict bool
	// ri is the number of the regional indicators that end the code points.
	ri int
}

// advance appends the code point of the properties p.
func (s *graphemeState) advance(p uint16) {
	switch incbOf(p) {
	case incbConsonant:
		s.conj = conjConsonant
	case incbLinker:
		if s.conj != conjNone {
			s.conj = conjLinked
		}
	case incbExtend:
	default:
		s.conj = conjNone
	}
	s.prevPict = s.pict
	if graphemeOf(p) != gExtend {
		s.pict = pictographic(p)
	}
	if graphemeOf(p) == gRI {
		s.ri++
	} else {
		s.ri = 0
	}
}

// graphemeBreak reports whether there is a boundary before ps[i]. s summarizes ps[:i].
func graphemeBreak(ps []uint16, i int, s *graphemeState) bool {
	a, b := graphemeOf(ps[i-1]), graphemeOf(ps[i])
	switch {
	case a == gCR && b == gLF:
		return false
	case a == gControl || a == gCR || a == gLF || b == gControl || b == gCR || b == gLF:
		return true
	case a == gL && (b == gL || b == gV || b == gLV || b == gLVT):
		return false
	case (a == gLV || a == gV) && (b == gV || b == gT):
		return false
	case (a == gLVT || a == gT) && b == gT:
		return false
	case b == gExtend || b == gZWJ || b == gSpacingMark || a == gPrepend:
		return false
	}
	// GB9c: Consonant [Extend Linker]* Linker [Extend Linker]* × Consonant.
	if incbOf(ps[i]) == incbConsonant && s.conj == conjLinked {
		return false
	}
	// GB11: ExtPict Extend* ZWJ × ExtPict.
	if a == gZWJ && pictographic(ps[i]) && s.prevPict {
		return false
	}
	// GB12, GB13: pairs of regional indicators.
	if a == gRI && b == gRI {
		return s.ri%2 == 0
	}
	return true
}

func wordIgnorable(w int) bool {
	return w == wExtend || w == wFormat || w == wZWJ
}

func isAHLetter(w int) bool {
	return w == wALetter || w == wHebrewLetter
}

func isMidNumLetQ(w int) bool {
	return w == wMidNumLet || w == wSingleQuote
}

// wordState summarizes the code points before a position for the rules of wordBreak that
// look back over runs.
type wordState struct {
	// ri is the number of the regional indicators that end the code points, not counting the
	// ignorable ones.
	ri int
}

// advance appends the code point of the properties p.
func (s *wordState) advance(p uint16) {
	switch w := wordOf(p); {
	case wordIgnorable(w):
	case w == wRI:
		s.ri++
	default:
		s.ri = 0
	}
}

// wordBreak reports whether there is a boundary before ps[i]. s summarizes ps[:i].
func wordBreak(ps []uint16, i int, s *wordState) bool {
	a, b := wordOf(ps[i-1]), wordOf(ps[i])
	switch {
	case a == wCR && b == wLF:
		return false
	case a == wNewline || a == wCR || a == wLF || b == wNewline || b == wCR || b == wLF:
		return true
	case a == wZWJ && pictographic(ps[i]):
		return false
	case a == wWSegSpace && b == wWSegSpace:
		return false
	case wordIgnorable(b):
		return false
	}
	// WB4: the ignorable characters belong to the character before them. Each run of them is
	// skipped from a bounded number of positions, as b is not ignorable.
	prev := func(j int) int {
		for j >= 0 && wordIgnorable(wordOf(ps[j])) {
			j--
		}
		return j
	}
	next := func(j int) int {
		for j < len(ps) && wordIgnorable(wordOf(ps[j])) {
			j++
		}
		return j
	}
	at := func(j int) int {
		if j < 0 || j >= len(ps) {
			return -1
		}
		return wordOf(ps[j])
	}
	pi := prev(i - 1)
	a = at(pi)
	aa := at(prev(pi - 1))
	bb := at(next(i + 1))
	switch {
	case isAHLetter(a) && isAHLetter(b):
		return false
	case isAHLetter(a) && (b == wMidLetter || isMidNumLetQ(b)) && isAHLetter(bb):
		return false
	case isAHLetter(aa) && (a == wMidLetter || isMidNumLetQ(a)) && isAHLetter(b):
		return false
	case a == wHebrewLetter && b == wSingleQuote:
		return false
	case a == wHebrewLetter && b == wDoubleQuote && bb == wHebrewLetter:
		return false
	case aa == wHebrewLetter && a == wDoubleQuote && b == wHebrewLetter:
		return false
	case a == wNumeric && b == wNumeric:
		return false
	case isAHLetter(a) && b == wNumeric:
		return false
	case a == wNumeric && isAHLetter(b):
		return false
	case aa == wNumeric && (a == wMidNum || isMidNumLetQ(a)) && b == wNumeric:
		return false
	case a == wNumeric && (b == wMidNum || isMidNumLetQ(b)) && bb == wNumeric:
		return false
	case a == wKatakana && b == wKatakana:
		return false
	case (isAHLetter(a) || a == wNumeric || a == wKatakana || a == wExtendNumLet) && b == wExtendNumLet:
		return false
	case a == wExtendNumLet && (isAHLetter(b) || b == wNumeric || b == wKatakana):
		return false
	case a == wRI && b == wRI:
		return s.ri%2 == 0
	}
	return true
}

func sentenceIgnorable(s int) bool {
	return s == sExtend || s == sFormat
}

func isParaSep(s int) bool {
	return s == sSep || s == sCR || s == sLF
}

func isSATerm(s int) bool {
	return s == sSTerm || s == sATerm
}

// sentenceState summarizes the code points before a position for the rules of sentenceBreak
// that look over runs.
type sentenceState struct {
	// term is the SATerm of SATerm Close* Sp* that ends the code points, not counting the
	// ignorable ones, or -1.
	term int
	// sp reports whether that match has spaces.
	sp bool
	// lowerFrom and lowerAt cache the scan of SB8: the first code point at lowerFrom or
	// later that is OLetter, Upper, Lower, ParaSep or SATerm is at lowerAt.
	lowerFrom int
	lowerAt   int
}

// advance appends the code point ps[i].
func (s *sentenceState) advance(ps []uint16, i int) {
	if i == 0 {
		s.term = -1
		s.lowerFrom = -1
	}
	switch c := sentenceOf(ps[i]); {
	case sentenceIgnorable(c):
	case isSATerm(c):
		s.term = c
		s.sp = false
	case c == sClose && s.term >= 0 && !s.sp:
	case c == sSp && s.term >= 0:
		s.sp = true
	default:
		s.term = -1
	}
}

// lowerAfter returns the index of the first code point at i or later that is OLetter, Upper,
// Lower, ParaSep or SATerm, or len(ps).
func (s *sentenceState) lowerAfter(ps []uint16, i int) int {
	if s.lowerFrom >= 0 && s.lowerFrom <= i && i <= s.lowerAt {
		return s.lowerAt
	}
	k := i
	for k < len(ps) {
		c := sentenceOf(ps[k])
		if c == sOLetter || c == sUpper || c == sLower || isParaSep(c) || isSATerm(c) {
			break
		}
		k++
	}
	s.lowerFrom, s.lowerAt = i, k
	return k
}

// sentenceBreak reports whether there is a boundary before ps[i]. s summarizes ps[:i].
func sentenceBreak(ps []uint16, i int, s *sentenceState) bool {
	a, b := sentenceOf(ps[i-1]), sentenceOf(ps[i])
	switch {
	case a == sCR && b == sLF:
		return false
	case isParaSep(a):
		return true
	case sentenceIgnorable(b):
		return false
	}
	// SB5: the ignorable characters belong to the character before them. Each run of them is
	// skipped from a bounded number of positions, as b is not ignorable.
	prev := func(j int) int {
		for j >= 0 && sentenceIgnorable(sentenceOf(ps[j])) {
			j--
		}
		return j
	}
	at := func(j int) int {
		if j < 0 || j >= len(ps) {
			return -1
		}
		return sentenceOf(ps[j])
	}
	pi := prev(i - 1)
	a = at(pi)
	aa := at(prev(pi - 1))
	if a == sATerm && b == sNumeric {
		return false
	}
	if (aa == sUpper || aa == sLower) && a == sATerm && b == sUpper {
		return false
	}
	if s.term < 0 {
		return false
	}
	if s.term == sATerm {
		// SB8.
		if k := s.lowerAfter(ps, i); k < len(ps) && sentenceOf(ps[k]) == sLower {
			return false
		}
	}
	if b == sSContinue || isSATerm(b) {
		return false
	}
	if !s.sp && (b == sClose || b == sSp || isParaSep(b)) {
		return false
	}
	if b == sSp || isParaSep(b) {
		return false
	}
	return true
}

// IsWordLike reports whether a word segment is word-like: whether it has letters, digits or
// ideographs.
func IsWordLike(rs []rune) bool {
	for _, r := range rs {
		switch wordOf(props(r)) {
		case wALetter, wHebrewLetter, wNumeric, wKatakana:
			return true
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
