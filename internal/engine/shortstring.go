// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

const maxShortStringBytes = 8

func (v Value) isShortString() bool {
	return v.o != nil && v.o.class == classShortString
}

func (vm *VM) shortString(s string) Value {
	// The immutable sentinel carries the byte length and the originating realm.
	// Unused payload bytes are zero, so equality can compare all 64 bits.
	var data [maxShortStringBytes]byte
	copy(data[:], s)
	return Value{
		n: math.Float64frombits(binary.LittleEndian.Uint64(data[:])),
		o: &vm.shapes.shortStrings[len(s)],
	}
}

func (v Value) shortText() string {
	var data [maxShortStringBytes]byte
	binary.LittleEndian.PutUint64(data[:], math.Float64bits(v.n))
	return string(data[:v.o.shortStringBytes])
}

// Str returns the string value in canonical WTF-8.
func (v Value) Str() string {
	if v.o.class != classShortString && v.o.flags&objReversed == 0 {
		return v.o.str
	}
	return v.stringSlow()
}

func (v Value) stringSlow() string {
	if v.isShortString() {
		return v.shortText()
	}
	return v.o.Str()
}

func (v Value) rawString() string {
	if v.isShortString() {
		return v.shortText()
	}
	return v.o.str
}

// StringLength returns the number of UTF-16 code units in the string value.
func (v Value) StringLength() int {
	if v.o.flags&(objASCIIKnown|objASCII) == objASCIIKnown|objASCII {
		return len(v.o.str)
	}
	return v.stringLengthSlow()
}

func (v Value) stringLengthSlow() int {
	if v.isShortString() {
		return wtf8.Len(v.shortText())
	}
	return v.o.StringLength()
}

func (v Value) stringObject() *Object {
	if !v.isShortString() {
		return v.o
	}
	return &Object{
		kind:  kindString,
		shape: v.o.shape,
		str:   v.shortText(),
	}
}

// WriteStringTo appends the string value to b.
func (v Value) WriteStringTo(b *wtf8.Builder) {
	if v.isShortString() {
		var data [maxShortStringBytes]byte
		binary.LittleEndian.PutUint64(data[:], math.Float64bits(v.n))
		b.WriteStringBytes(data[:v.o.shortStringBytes])
		return
	}
	b.WriteString(v.o.Str())
}

// UnitAt returns the code unit at i in the string value. i must be in range.
func (v Value) UnitAt(i int) uint16 {
	if !v.isShortString() {
		return v.o.UnitAt(i)
	}
	s := v.shortText()
	return wtf8.At(s, wtf8.Seek(s, wtf8.Pos{}, i))
}

// Substring returns the string's code units from start up to end, which must be in range and in order.
func (v Value) Substring(start, end int) string {
	if v.o.flags&(objASCIIKnown|objASCII|objReversed) == objASCIIKnown|objASCII {
		return v.o.str[start:end]
	}
	return v.substringSlow(start, end)
}

func (v Value) substringSlow(start, end int) string {
	if !v.isShortString() {
		return v.o.Substring(start, end)
	}
	s := v.shortText()
	a := wtf8.Seek(s, wtf8.Pos{}, start)
	b := wtf8.Seek(s, a, end)
	return wtf8.Slice(s, a, b)
}

func (v Value) writeShortString(b *strings.Builder) {
	var data [maxShortStringBytes]byte
	binary.LittleEndian.PutUint64(data[:], math.Float64bits(v.n))
	b.Write(data[:v.o.shortStringBytes])
}

// Substring returns the code units of s from start up to end, which must be in range and in order.
func (vm *VM) Substring(s Value, start, end int) Value {
	if !s.isShortString() {
		return vm.NewString(s.Substring(start, end))
	}
	if start == end {
		return vm.emptyString
	}
	text := s.shortText()
	a := wtf8.Seek(text, wtf8.Pos{}, start)
	b := wtf8.Seek(text, a, end)
	part := wtf8.Slice(text, a, b)
	if len(part) == 1 && part[0] < 0x80 {
		return vm.strings.ascii[part[0]]
	}
	return vm.shortString(part)
}

// StringByteLength returns the byte length of the string's canonical WTF-8 encoding.
func (v Value) StringByteLength() int {
	if v.isShortString() {
		return int(v.o.shortStringBytes)
	}
	return len(v.o.str)
}
