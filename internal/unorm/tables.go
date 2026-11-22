// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package unorm

import (
	"cmp"
	_ "embed"
	"encoding/binary"
	"sort"
	"unicode/utf8"
)

//go:generate go run gen.go

// The Unicode normalization data is under the Unicode License v3 in internal/unicode/LICENSE.
//
//go:embed tables.bin
var tables string

var (
	mappingsStart = binary.LittleEndian.Uint32([]byte(tables[:4]))
	pairsStart    = binary.LittleEndian.Uint32([]byte(tables[4:8]))
	mappings      = tables[mappingsStart:pairsStart]
	pairs         = tables[pairsStart:]
)

// Form is a Unicode normalization form.
type Form int

const (
	NFC Form = iota
	NFD
	NFKC
	NFKD
)

// A property holds the combining class, quick-check and compatibility flags,
// and offset of the length-prefixed UTF-8 decomposition mapping.
type property uint32

func lookup(r rune) property {
	if r < utf8.RuneSelf || r > utf8.MaxRune {
		return 0
	}
	const blocksStart = 8 + (utf8.MaxRune+1)/256*2
	page := 8 + int(r>>8)*2
	block := int(binary.LittleEndian.Uint16([]byte(tables[page : page+2])))
	i := blocksStart + (block*256+int(r&255))*4
	return property(binary.LittleEndian.Uint32([]byte(tables[i : i+4])))
}

// CombiningClass returns the canonical combining class of r.
func CombiningClass(r rune) uint8 {
	return uint8(lookup(r))
}

func (p property) mapping(form Form) string {
	offset := int(p >> 16)
	if offset == 0 || (p&(1<<12) != 0 && (form == NFC || form == NFD)) {
		return ""
	}
	return mappings[offset+1 : offset+1+int(mappings[offset])]
}

func (p property) quickYes(form Form) bool {
	return p&(1<<(8+form)) == 0
}

// BoundaryBefore reports whether normalization can split immediately before r.
func BoundaryBefore(form Form, r rune) bool {
	for {
		p := lookup(r)
		s := p.mapping(form)
		if s == "" {
			return uint8(p) == 0 && (form == NFD || form == NFKD || p.quickYes(NFC))
		}
		r, _ = utf8.DecodeRuneInString(s)
	}
}

const (
	hangulBase  = 0xac00
	lBase       = 0x1100
	vBase       = 0x1161
	tBase       = 0x11a7
	lCount      = 19
	vCount      = 21
	tCount      = 28
	hangulCount = lCount * vCount * tCount
)

func appendDecomposed(chars []normChar, r rune, form Form) []normChar {
	if index := r - hangulBase; 0 <= index && index < hangulCount {
		chars = append(chars, newNormChar(lBase+index/(vCount*tCount), 0), newNormChar(vBase+index/tCount%vCount, 0))
		if t := index % tCount; t != 0 {
			chars = append(chars, newNormChar(tBase+t, 0))
		}
		return chars
	}
	p := lookup(r)
	mapping := p.mapping(form)
	if mapping == "" {
		return append(chars, newNormChar(r, uint8(p)))
	}
	for _, d := range mapping {
		chars = appendDecomposed(chars, d, form)
	}
	return chars
}

func primaryComposite(a, b rune) (rune, bool) {
	if lookup(b).quickYes(NFC) {
		return 0, false
	}
	if l, v := a-lBase, b-vBase; 0 <= l && l < lCount && 0 <= v && v < vCount {
		return hangulBase + (l*vCount+v)*tCount, true
	}
	if lv, t := a-hangulBase, b-tBase; 0 <= lv && lv < hangulCount && lv%tCount == 0 && 0 < t && t < tCount {
		return a + t, true
	}
	key := uint64(a)<<21 | uint64(b)
	// Each composition is a sorted pair key followed by the composite code point.
	const size = 12
	i, ok := sort.Find(len(pairs)/size, func(i int) int {
		return cmp.Compare(key, binary.LittleEndian.Uint64([]byte(pairs[i*size:i*size+8])))
	})
	if !ok {
		return 0, false
	}
	return rune(binary.LittleEndian.Uint32([]byte(pairs[i*size+8 : (i+1)*size]))), true
}
