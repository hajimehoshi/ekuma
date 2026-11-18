// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func (v Value) isShortString() bool {
	return v.o != nil && v.o.class == classShortString
}

func (vm *VM) shortString(s string) Value {
	// The immutable sentinel carries the byte length and the originating realm.
	return Value{
		n: math.Float64frombits(shortstrings.Pack(s)),
		o: &vm.shapes.shortStrings[len(s)],
	}
}

func (v Value) shortText() string {
	return shortstrings.String(math.Float64bits(v.n), int(v.o.shortStringBytes))
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
		return shortstrings.Len(math.Float64bits(v.n), int(v.o.shortStringBytes))
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
		data := shortstrings.Bytes(math.Float64bits(v.n))
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
	return shortstrings.UnitAt(math.Float64bits(v.n), i)
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
	data, n := shortstrings.Slice(math.Float64bits(v.n), start, end)
	return shortstrings.String(data, n)
}

// SubstringEquals reports whether the substring from start up to end equals other.
// Both values must be strings, and the UTF-16 indices must be in range and in order.
func (v Value) SubstringEquals(start, end int, other Value) bool {
	if !v.isShortString() {
		return v.o.Substring(start, end) == other.Str()
	}
	data, n := shortstrings.Slice(math.Float64bits(v.n), start, end)
	if other.isShortString() {
		return n == int(other.o.shortStringBytes) && data == math.Float64bits(other.n)
	}
	s := other.Str()
	return n == len(s) && data == shortstrings.Pack(s)
}

func (v Value) writeShortString(b *strings.Builder) {
	data := shortstrings.Bytes(math.Float64bits(v.n))
	b.Write(data[:v.o.shortStringBytes])
}

// Substring returns the code units of s from start up to end, which must be in range and in order.
func (vm *VM) Substring(s Value, start, end int) Value {
	if !s.isShortString() {
		return vm.NewString(s.o.Substring(start, end))
	}
	if start == end {
		return vm.emptyString
	}
	data, n := shortstrings.Slice(math.Float64bits(s.n), start, end)
	if n == 1 && byte(data) < 0x80 {
		return vm.strings.ascii[byte(data)]
	}
	return Value{
		n: math.Float64frombits(data),
		o: &vm.shapes.shortStrings[n],
	}
}

// StringByteLength returns the byte length of the string's canonical WTF-8 encoding.
func (v Value) StringByteLength() int {
	if v.isShortString() {
		return int(v.o.shortStringBytes)
	}
	return len(v.o.str)
}

// UnitIndexOf returns the first UTF-16 index of sub at or after from, or -1.
// from must be nonnegative.
func (v Value) UnitIndexOf(sub string, from int) int {
	if !v.isShortString() {
		return v.o.UnitIndexOf(sub, from)
	}
	if len(sub) > shortstrings.MaxBytes {
		return -1
	}
	return shortstrings.Index(math.Float64bits(v.n), int(v.o.shortStringBytes), shortstrings.Pack(sub), len(sub), from)
}

// UnitLastIndexOf returns the last UTF-16 index of sub at or before from, or -1.
// from must be nonnegative.
func (v Value) UnitLastIndexOf(sub string, from int) int {
	if !v.isShortString() {
		return v.o.UnitLastIndexOf(sub, from)
	}
	if len(sub) > shortstrings.MaxBytes {
		return -1
	}
	return shortstrings.LastIndex(math.Float64bits(v.n), int(v.o.shortStringBytes), shortstrings.Pack(sub), len(sub), from)
}

// compareShortStrings compares unreversed strings whose byte lengths fit a packed payload.
func compareShortStrings(a Value, aN int, b Value, bN int) int {
	var x, y uint64
	if a.isShortString() {
		x = math.Float64bits(a.n)
	} else {
		x = shortstrings.Pack(a.o.str)
	}
	if b.isShortString() {
		y = math.Float64bits(b.n)
	} else {
		y = shortstrings.Pack(b.o.str)
	}
	return shortstrings.Compare(x, aN, y, bN)
}

// IsWellFormed reports whether the string value contains no lone surrogates.
func (v Value) IsWellFormed() bool {
	if v.isShortString() {
		return shortstrings.IsWellFormed(math.Float64bits(v.n))
	}
	return wtf8.IsWellFormed(v.o.Str())
}

// ToWellFormed returns s with lone surrogates replaced by U+FFFD.
func (vm *VM) ToWellFormed(s Value) Value {
	if s.isShortString() {
		return vm.toWellFormedShort(s, math.Float64bits(s.n), int(s.o.shortStringBytes))
	}
	text := s.o.Str()
	if len(text) <= shortstrings.MaxBytes {
		return vm.toWellFormedShort(s, shortstrings.Pack(text), len(text))
	}
	repaired := wtf8.ToValidUTF8(text)
	if repaired == text {
		return s
	}
	return vm.NewString(repaired)
}

func (vm *VM) toWellFormedShort(s Value, data uint64, n int) Value {
	repaired := shortstrings.ToWellFormed(data)
	if repaired == data {
		return s
	}
	return Value{
		n: math.Float64frombits(repaired),
		o: &vm.shapes.shortStrings[n],
	}
}
