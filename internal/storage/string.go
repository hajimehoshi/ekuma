// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// MaxStringLength is the largest length a string may have, in bytes.
// It is smaller where an int has 32 bits.
const MaxStringLength = 1<<(27+3*(strconv.IntSize/64)) - 1

// stringPos is a [wtf8.Pos] in a string of at most MaxStringLength bytes.
type stringPos struct {
	unit int32
	byte int32
	low  bool
}

func (p stringPos) pos() wtf8.Pos {
	return wtf8.Pos{
		Unit: int(p.unit),
		Byte: int(p.byte),
		Low:  p.low,
	}
}

func newStringPos(p wtf8.Pos) stringPos {
	return stringPos{
		unit: int32(p.Unit),
		byte: int32(p.Byte),
		low:  p.Low,
	}
}

// Str returns the content of the string o in WTF-8.
func (o *Object) Str() wtf8.String {
	if o.flags&ObjReversed != 0 {
		o.Unreverse()
	}
	return o.Text
}

// IsASCII reports whether the string o consists of ASCII characters only.
func (o *Object) IsASCII() bool {
	if o.flags&ObjASCIIKnown == 0 {
		o.classifyASCII()
	}
	return o.flags&ObjASCII != 0
}

// classifyASCII records in the flags of the string o whether it is ASCII.
// It stays out of line so that IsASCII can be inlined.
//
//go:noinline
func (o *Object) classifyASCII() {
	o.flags |= ObjASCIIKnown
	if wtf8.IsASCII(o.Text) {
		o.flags |= ObjASCII
	}
}

// StringLength returns the length of the string o in UTF-16 code units.
func (o *Object) StringLength() int {
	if o.IsASCII() {
		return len(o.Text)
	}
	if !o.cachesUTF16() {
		return wtf8.Len(o.Text)
	}
	m := o.ensureMeta()
	// A string that is not ASCII is never empty, so 0 means that the length is not known yet.
	if m.Length == 0 {
		m.Length = int32(wtf8.Len(o.Text))
	}
	return int(m.Length)
}

// cachesUTF16 reports whether the non-ASCII string o keeps its UTF-16 view in its side data.
func (o *Object) cachesUTF16() bool {
	if len(o.Text) > MaxStringLength {
		// Only a host can make such a string.
		return false
	}
	return o.Meta() != nil || len(o.Text) > maxUncachedLength
}

// maxUncachedLength is the length in bytes up to which a non-ASCII string without side data
// computes its UTF-16 view at each use.
const maxUncachedLength = 32

// ArrayIndex returns the array index whose canonical string form is the string o.
func (o *Object) ArrayIndex() (uint32, bool) {
	if i, ok := o.KnownArrayIndex(); ok {
		return i, true
	}
	i, ok := StringIndex(o.Str())
	if ok {
		o.length = i + 1
	}
	return i, ok
}

// KnownArrayIndex returns the array index that the string o stands for, if it is known
// already. It reports false otherwise, even when o is an array index.
func (o *Object) KnownArrayIndex() (uint32, bool) {
	return o.length - 1, o.length != 0
}

// seek returns the position of the code unit i of the non-ASCII string o.
func (o *Object) seek(i int) wtf8.Pos {
	s := o.Str()
	if !o.cachesUTF16() {
		return wtf8.Seek(s, wtf8.Pos{}, i)
	}
	m := o.ensureMeta()
	from := m.Cursor.pos()
	if d := i - from.Unit; (d > checkpointInterval || d < -checkpointInterval) && i >= checkpointInterval {
		if m.checkpoints == nil && len(s) >= minCheckpointedLength {
			cs := checkpointsOf(s)
			m.checkpoints = &cs
		}
		if m.checkpoints != nil {
			if k := i / checkpointInterval; k < len(*m.checkpoints) {
				c := (*m.checkpoints)[k]
				from = wtf8.Pos{
					Unit: k * checkpointInterval,
					Byte: int(c >> 1),
					Low:  c&1 != 0,
				}
			}
		}
	}
	p := wtf8.Seek(s, from, i)
	m.Cursor = newStringPos(p)
	return p
}

const (
	// checkpointInterval is the distance in code units between two checkpoints of a string.
	checkpointInterval = 64

	// minCheckpointedLength is the length in bytes from which a string gets checkpoints.
	minCheckpointedLength = 4 * checkpointInterval
)

// checkpointsOf returns the positions of the code units of the non-ASCII string s whose
// indices are multiples of checkpointInterval, in order. A position is the byte of the code
// unit shifted left by one, with the low bit set when the code unit is the low surrogate of
// the four-byte sequence at that byte.
func checkpointsOf(s wtf8.String) []uint32 {
	cs := make([]uint32, 0, len(s)/(3*checkpointInterval)+1)
	var unit int
	for i := 0; i < len(s); {
		if unit%checkpointInterval == 0 {
			cs = append(cs, uint32(i)<<1)
		}
		switch b := s[i]; {
		case b < 0x80:
			i++
			unit++
		case b < 0xE0:
			i += 2
			unit++
		case b < 0xF0:
			i += 3
			unit++
		default:
			if (unit+1)%checkpointInterval == 0 {
				cs = append(cs, uint32(i)<<1|1)
			}
			i += 4
			unit += 2
		}
	}
	return cs
}

// UnitAt returns the code unit i of the string o. i must be in range.
func (o *Object) UnitAt(i int) uint16 {
	if o.IsASCII() {
		return o.asciiUnitAt(i)
	}
	return wtf8.At(o.Str(), o.seek(i))
}

func (o *Object) asciiUnitAt(i int) uint16 {
	if o.flags&ObjReversed != 0 {
		// Reading one unit leaves a string that is being prepended to reversed.
		return uint16(o.Text[len(o.Text)-1-i])
	}
	return uint16(o.Text[i])
}

// CodePointAt returns the code point starting at UTF-16 index i. i must be in range.
func (o *Object) CodePointAt(i int) rune {
	if o.IsASCII() {
		return rune(o.asciiUnitAt(i))
	}
	s := o.Str()
	p := o.seek(i)
	if b := s[p.Byte]; b < 0x80 {
		return rune(b)
	}
	r, _ := wtf8.DecodeRune(s[p.Byte:])
	if p.Low {
		return 0xdc00 + (r-0x10000)&0x3ff
	}
	return r
}

// Substring returns the code units from start up to end of the string o.
// The indices must be in range and in order.
func (o *Object) Substring(start, end int) wtf8.String {
	s := o.Str()
	if o.IsASCII() {
		return s[start:end]
	}
	a := o.seek(start)
	b := wtf8.Seek(s, a, end)
	if o.cachesUTF16() {
		o.Meta().Cursor = newStringPos(b)
	}
	return wtf8.Slice(s, a, b)
}

// UnitIndexOf returns the index in code units of the first occurrence of sub in the string o
// at or after from, or -1. poll, which may be nil, is called now and then during a long search.
func (o *Object) UnitIndexOf(sub wtf8.String, from int, poll func()) int {
	s := o.Str()
	n := o.StringLength()
	if from > n {
		return -1
	}
	if sub == "" {
		return from
	}
	if o.IsASCII() {
		i := xstrings.Index(s[from:], sub, poll)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if mayMatchInsidePair(sub) {
		return o.findUnits(sub, from, false, poll)
	}
	p := o.seek(from)
	if p.Low {
		// This pattern can only start at a code point boundary.
		p = wtf8.Seek(s, p, from+1)
	}
	i := xstrings.Index(s[p.Byte:], sub, poll)
	if i < 0 {
		return -1
	}
	return p.Unit + wtf8.Len(s[p.Byte:p.Byte+i])
}

// UnitLastIndexOf returns the index in code units of the last occurrence of sub in the string o
// that starts at or before from, or -1. poll is as for [Object.UnitIndexOf].
func (o *Object) UnitLastIndexOf(sub wtf8.String, from int, poll func()) int {
	n := o.StringLength()
	subLen := wtf8.Len(sub)
	from = min(from, n-subLen)
	if from < 0 {
		return -1
	}
	if sub == "" {
		return from
	}
	if !o.IsASCII() && mayMatchInsidePair(sub) {
		return o.findUnits(sub, from, true, poll)
	}
	s := o.Str()
	if o.IsASCII() {
		return xstrings.LastIndex(s[:from+subLen], sub, poll)
	}
	end := o.seek(from + subLen)
	i := xstrings.LastIndex(s[:end.Byte], sub, poll)
	if i < 0 {
		return -1
	}
	// The match cannot end inside a pair, so the low surrogate of a pair at end stands for
	// the start of the pair.
	unit := end.Unit
	if end.Low {
		unit--
	}
	return unit - wtf8.Len(s[i:end.Byte])
}

func mayMatchInsidePair(sub wtf8.String) bool {
	// A canonical pattern can split a pair only at its outer boundaries.
	first, _ := wtf8.DecodeRune(sub)
	last, _ := wtf8.DecodeLastRune(sub)
	return wtf8.IsLowSurrogate(first) || wtf8.IsHighSurrogate(last)
}

// findUnitsPollSteps is the number of code units or bytes that findUnits reads between two calls
// of its poll function.
const findUnitsPollSteps = 1 << 16

// findUnits returns the start of the match of sub nearest to from in the given direction, or -1,
// comparing code units by the Knuth-Morris-Pratt algorithm.
func (o *Object) findUnits(sub wtf8.String, from int, backward bool, poll func()) int {
	var units []uint16
	rest := sub
	for len(rest) > findUnitsPollSteps {
		// A part ends before the first byte of a character.
		n := findUnitsPollSteps
		for n < len(rest) && rest[n]&0xC0 == 0x80 {
			n++
		}
		units = wtf8.AppendUTF16(units, rest[:n])
		rest = rest[n:]
		if poll != nil {
			poll()
		}
	}
	units = wtf8.AppendUTF16(units, rest)
	m := len(units)
	if backward {
		slices.Reverse(units)
	}
	// border[j] is the length of the longest proper border of units[:j+1].
	border := make([]int32, m)
	for j, k := 1, 0; j < m; j++ {
		if j%findUnitsPollSteps == 0 && poll != nil {
			poll()
		}
		for k > 0 && units[k] != units[j] {
			k = int(border[k-1])
		}
		if units[k] == units[j] {
			k++
		}
		border[j] = int32(k)
	}
	s := o.Str()
	n := o.StringLength()
	// i runs over the code units of o from from, or backwards from the last unit of a match
	// that starts at from.
	i, step := from, 1
	if backward {
		i, step = min(from+m, n)-1, -1
	}
	p := o.seek(i)
	for k, steps := 0, 0; i >= 0 && i < n; i += step {
		if steps++; steps%findUnitsPollSteps == 0 && poll != nil {
			poll()
		}
		p = wtf8.Seek(s, p, i)
		c := wtf8.At(s, p)
		for k > 0 && units[k] != c {
			k = int(border[k-1])
		}
		if units[k] == c {
			k++
		}
		if k == m {
			if backward {
				return i
			}
			return i - m + 1
		}
	}
	return -1
}
