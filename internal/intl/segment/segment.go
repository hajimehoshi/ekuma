// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package segment implements UAX #29 segmentation with dictionary word boundaries and locale tailorings.
package segment

import (
	"encoding/binary"
	"strings"
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
	wHangul
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

type propertyIndex byte

func props(c rune) propertyIndex {
	if c < 0 || c > unicode.MaxRune {
		return 0
	}
	mid := int(u16(int(c>>10) * 2))
	leaf := int(u16(propertiesStage2 + (mid*32+int(c>>5)&31)*2))
	return propertyIndex(properties[propertiesLeaves+leaf*32+(int(c)&31)])
}

func graphemeOf(p propertyIndex) int      { return int(propertyValues[p] & 0xF) }
func wordOf(p propertyIndex) int          { return int(propertyValues[p] >> 4 & 0x1F) }
func sentenceOf(p propertyIndex) int      { return int(propertyValues[p] >> 9 & 0xF) }
func pictographic(p propertyIndex) bool   { return propertyValues[p]&(1<<13) != 0 }
func incbOf(p propertyIndex) int          { return int(propertyValues[p] >> 14 & 3) }
func wordBase(p propertyIndex) bool       { return propertyValues[p]&(1<<16) != 0 }
func dictionaryMark(p propertyIndex) bool { return propertyValues[p]&(1<<17) != 0 }
func scriptOf(p propertyIndex) wordScript { return wordScript(propertyValues[p] >> 18 & 7) }

func joinsCJ(p propertyIndex) bool { return propertyValues[p]&(1<<21) != 0 }

// pollSteps is the number of code points between two calls of the poll function of
// [Boundaries].
const pollSteps = 1 << 14

// Boundary marks a position between segments.
type Boundary uint32

const wordLikeFlag Boundary = 1

func boundaryAt(index int) Boundary {
	return Boundary(index) << 1
}

// Index returns the rune index at the boundary.
func (b Boundary) Index() int32 {
	return int32(b >> 1)
}

// IsWordLike reports whether the segment ending at b is word-like.
func (b Boundary) IsWordLike() bool {
	return b&wordLikeFlag != 0
}

// Boundaries returns the segment boundaries for locale from 0 to len(rs),
// which must be less than 2^31. An empty locale uses the default rules. An empty
// input has no boundaries. A non-nil poll may interrupt the operation by panicking.
func Boundaries(g Granularity, rs []rune, locale string, poll func()) []Boundary {
	if len(rs) == 0 {
		return nil
	}
	language, _, _ := strings.Cut(locale, "-")
	greekSentence := g == Sentence && language == "el"
	var dictionaryWords bool
	ps := make([]propertyIndex, len(rs))
	for i, r := range rs {
		if poll != nil && i%pollSteps == pollSteps-1 {
			poll()
		}
		ps[i] = props(r)
		if g == Word && scriptOf(ps[i]) != otherScript {
			dictionaryWords = true
		}
		if greekSentence && (r == ';' || r == '\u037E') {
			ps[i] = props('!')
		}
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
	dp := dictionaryPoller{
		poll: poll,
	}
	bs := []Boundary{0}
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
			if g == Word {
				bs = appendWordBoundaries(bs, rs, ps, i, dictionaryWords, &dp)
			} else {
				bs = append(bs, boundaryAt(i))
			}
		}
	}
	if g == Word {
		return appendWordBoundaries(bs, rs, ps, len(rs), dictionaryWords, &dp)
	}
	return append(bs, boundaryAt(len(rs)))
}

func appendWordBoundaries(bs []Boundary, rs []rune, ps []propertyIndex, end int, dictionary bool, dp *dictionaryPoller) []Boundary {
	first := len(bs)
	start := int(bs[first-1].Index())
	if dictionary {
		bs = appendDictionaryBreaks(bs, rs, ps, start, end, dp)
	}
	b := boundaryAt(end)
	if wordBase(ps[end-1]) || contextualWordStatus(ps[start:end], dp.poll) {
		b |= wordLikeFlag
		// Dictionary subdivisions inherit the status at the rule-based boundary.
		for i := first; i < len(bs); i++ {
			bs[i] |= wordLikeFlag
		}
	}
	return append(bs, b)
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
func (s *graphemeState) advance(p propertyIndex) {
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
func graphemeBreak(ps []propertyIndex, i int, s *graphemeState) bool {
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
func (s *wordState) advance(p propertyIndex) {
	switch w := wordOf(p); {
	case wordIgnorable(w):
	case w == wRI:
		s.ri++
	default:
		s.ri = 0
	}
}

// wordBreak reports whether there is a boundary before ps[i]. s summarizes ps[:i].
func wordBreak(ps []propertyIndex, i int, s *wordState) bool {
	a, b := wordOf(ps[i-1]), wordOf(ps[i])
	switch {
	case a == wCR && b == wLF:
		return false
	case a == wNewline || a == wCR || a == wLF || b == wNewline || b == wCR || b == wLF:
		return true
	case a == wZWJ && pictographic(ps[i]):
		return false
	case a == wHangul && b == wHangul, joinsCJ(ps[i-1]) && joinsCJ(ps[i]):
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
	switch {
	case isAHLetter(a) && isAHLetter(b):
		return false
	case isAHLetter(a) && (b == wMidLetter || isMidNumLetQ(b)) && isAHLetter(at(next(i+1))):
		return false
	case isAHLetter(b) && (a == wMidLetter || isMidNumLetQ(a)) && isAHLetter(at(prev(pi-1))):
		return false
	case a == wHebrewLetter && b == wSingleQuote:
		return false
	case a == wHebrewLetter && b == wDoubleQuote && at(next(i+1)) == wHebrewLetter:
		return false
	case b == wHebrewLetter && a == wDoubleQuote && at(prev(pi-1)) == wHebrewLetter:
		return false
	case a == wNumeric && b == wNumeric:
		return false
	case isAHLetter(a) && b == wNumeric:
		return false
	case a == wNumeric && isAHLetter(b):
		return false
	case b == wNumeric && (a == wMidNum || isMidNumLetQ(a)) && at(prev(pi-1)) == wNumeric:
		return false
	case a == wNumeric && (b == wMidNum || isMidNumLetQ(b)) && at(next(i+1)) == wNumeric:
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
func (s *sentenceState) advance(ps []propertyIndex, i int) {
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
func (s *sentenceState) lowerAfter(ps []propertyIndex, i int) int {
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
func sentenceBreak(ps []propertyIndex, i int, s *sentenceState) bool {
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

// contextualWordStatus reports word status when the final character has none of its own.
func contextualWordStatus(ps []propertyIndex, poll func()) bool {
	n := len(ps)
	last := ps[n-1]
	// ICU assigns word status to a boundary immediately after two kana or Han characters.
	if joinsCJ(last) && n >= 2 && joinsCJ(ps[n-2]) {
		return true
	}
	switch wordOf(last) {
	case wHangul:
		return true
	case wSingleQuote, wExtendNumLet:
		return n > 1
	case wExtend, wFormat, wZWJ:
		for i := n - 2; i >= 0; i-- {
			if poll != nil && (n-i)%pollSteps == 0 {
				poll()
			}
			if wordBase(ps[i]) {
				return true
			}
			if !wordIgnorable(wordOf(ps[i])) {
				break
			}
		}
	}
	return false
}
