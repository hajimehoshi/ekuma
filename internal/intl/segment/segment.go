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

// Boundaries returns the boundaries of the segments of rs as indices of rs, from 0 to
// len(rs). An empty rs has no boundaries.
func Boundaries(g Granularity, rs []rune) []int {
	if len(rs) == 0 {
		return nil
	}
	ps := make([]uint16, len(rs))
	for i, r := range rs {
		ps[i] = props(r)
	}
	bs := []int{0}
	for i := 1; i < len(rs); i++ {
		var brk bool
		switch g {
		case Grapheme:
			brk = graphemeBreak(ps, i)
		case Word:
			brk = wordBreak(ps, i)
		case Sentence:
			brk = sentenceBreak(ps, i)
		}
		if brk {
			bs = append(bs, i)
		}
	}
	return append(bs, len(rs))
}

// graphemeBreak reports whether there is a boundary before ps[i].
func graphemeBreak(ps []uint16, i int) bool {
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
	if incbOf(ps[i]) == incbConsonant {
		linker := false
		for j := i - 1; j >= 0; j-- {
			switch incbOf(ps[j]) {
			case incbLinker:
				linker = true
				continue
			case incbExtend:
				continue
			case incbConsonant:
				if linker {
					return false
				}
			}
			break
		}
	}
	// GB11: ExtPict Extend* ZWJ × ExtPict.
	if a == gZWJ && pictographic(ps[i]) {
		j := i - 2
		for j >= 0 && graphemeOf(ps[j]) == gExtend {
			j--
		}
		if j >= 0 && pictographic(ps[j]) {
			return false
		}
	}
	// GB12, GB13: pairs of regional indicators.
	if a == gRI && b == gRI {
		n := 0
		for j := i - 1; j >= 0 && graphemeOf(ps[j]) == gRI; j-- {
			n++
		}
		return n%2 == 0
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

// wordBreak reports whether there is a boundary before ps[i].
func wordBreak(ps []uint16, i int) bool {
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
	// WB4: the ignorable characters belong to the character before them.
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
		n := 0
		for j := pi; j >= 0; j = prev(j - 1) {
			if wordOf(ps[j]) != wRI {
				break
			}
			n++
		}
		return n%2 == 0
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

// sentenceBreak reports whether there is a boundary before ps[i].
func sentenceBreak(ps []uint16, i int) bool {
	a, b := sentenceOf(ps[i-1]), sentenceOf(ps[i])
	switch {
	case a == sCR && b == sLF:
		return false
	case isParaSep(a):
		return true
	case sentenceIgnorable(b):
		return false
	}
	// SB5: the ignorable characters belong to the character before them.
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
	// Match SATerm Close* Sp* backwards from pi, noting where the spaces and the closes are.
	j := pi
	sp := false
	for j >= 0 && at(j) == sSp {
		sp = true
		j = prev(j - 1)
	}
	closeOnly := !sp
	for j >= 0 && at(j) == sClose {
		j = prev(j - 1)
	}
	term := at(j)
	if isSATerm(term) {
		if term == sATerm {
			// SB8.
			k := i
			for k < len(ps) {
				s := sentenceOf(ps[k])
				if s == sOLetter || s == sUpper || s == sLower || isParaSep(s) || isSATerm(s) {
					break
				}
				k++
			}
			if k < len(ps) && sentenceOf(ps[k]) == sLower {
				return false
			}
		}
		if b == sSContinue || isSATerm(b) {
			return false
		}
		if closeOnly && (b == sClose || b == sSp || isParaSep(b)) {
			return false
		}
		if b == sSp || isParaSep(b) {
			return false
		}
		return true
	}
	return false
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
