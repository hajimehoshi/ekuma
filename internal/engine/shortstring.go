// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
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

// CodePointAt returns the code point starting at UTF-16 index i. i must be in range.
func (v Value) CodePointAt(i int) rune {
	if !v.isShortString() {
		return v.o.CodePointAt(i)
	}
	return shortstrings.CodePointAt(math.Float64bits(v.n), i)
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
		part := v.o.Substring(start, end)
		if other.isShortString() {
			return len(part) == int(other.o.shortStringBytes) && shortstrings.Pack(part) == math.Float64bits(other.n)
		}
		return part == other.o.Str()
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
// Both values must be strings, and from must be nonnegative.
func (v Value) UnitIndexOf(sub Value, from int) int {
	if !v.isShortString() && !sub.isShortString() {
		return v.o.UnitIndexOf(sub.o.Str(), from)
	}
	return v.shortStringIndex(sub, from, false)
}

// UnitLastIndexOf returns the last UTF-16 index of sub at or before from, or -1.
// Both values must be strings, and from must be nonnegative.
func (v Value) UnitLastIndexOf(sub Value, from int) int {
	if !v.isShortString() && !sub.isShortString() {
		return v.o.UnitLastIndexOf(sub.o.Str(), from)
	}
	return v.shortStringIndex(sub, from, true)
}

func (v Value) shortStringIndex(sub Value, from int, backward bool) int {
	n, subN := v.StringByteLength(), sub.StringByteLength()
	if n > shortstrings.MaxBytes {
		// The dispatch requires a packed operand, so sub is packed here.
		data := shortstrings.Bytes(math.Float64bits(sub.n))
		text := string(data[:subN])
		if backward {
			return v.o.UnitLastIndexOf(text, from)
		}
		return v.o.UnitIndexOf(text, from)
	}
	if subN > shortstrings.MaxBytes {
		return -1
	}
	var data, pattern uint64
	if v.isShortString() {
		data = math.Float64bits(v.n)
	} else {
		data = shortstrings.Pack(v.o.Str())
	}
	if sub.isShortString() {
		pattern = math.Float64bits(sub.n)
	} else {
		pattern = shortstrings.Pack(sub.o.Str())
	}
	if backward {
		return shortstrings.LastIndex(data, n, pattern, subN, from)
	}
	return shortstrings.Index(data, n, pattern, subN, from)
}

// IsASCII reports whether s contains only ASCII code units. s must be a string.
func (s Value) IsASCII() bool {
	if s.o.flags&objASCIIKnown != 0 {
		return s.o.flags&objASCII != 0
	}
	return s.isASCIISlow()
}

func (s Value) isASCIISlow() bool {
	if s.isShortString() {
		return shortstrings.IsASCII(math.Float64bits(s.n))
	}
	return s.o.IsASCII()
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

// MapASCII converts s to uppercase if upper is true, or lowercase otherwise.
// s must be a string; false means it is non-ASCII or too long to pack.
func (vm *VM) MapASCII(s Value, upper bool) (mapped Value, ok bool) {
	// Packed sentinels have no string data.
	if len(s.o.str) > shortstrings.MaxBytes {
		return
	}
	mapped, ok = vm.mapASCII(s, upper)
	return
}

func (vm *VM) mapASCII(s Value, upper bool) (Value, bool) {
	var data uint64
	var n int
	if s.isShortString() {
		data = math.Float64bits(s.n)
		if !shortstrings.IsASCII(data) {
			return Value{}, false
		}
		n = int(s.o.shortStringBytes)
	} else {
		if !s.o.IsASCII() {
			return Value{}, false
		}
		text := s.o.Str()
		data = shortstrings.Pack(text)
		n = len(text)
	}
	var mapped uint64
	if upper {
		mapped = shortstrings.ToUpperASCII(data)
	} else {
		mapped = shortstrings.ToLowerASCII(data)
	}
	if mapped == data {
		return s, true
	}
	if n == 1 {
		return vm.strings.ascii[byte(mapped)], true
	}
	return Value{
		n: math.Float64frombits(mapped),
		o: &vm.shapes.shortStrings[n],
	}, true
}

// TrimString trims whitespace from the selected ends of the string s.
func (vm *VM) TrimString(s Value, start, end bool) Value {
	if s.isShortString() {
		return vm.trimShortString(s, start, end)
	}
	text := s.o.Str()
	trimmed := numconv.TrimWhiteSpace(text, start, end)
	if len(trimmed) == len(text) {
		return s
	}
	return vm.NewString(trimmed)
}

func (vm *VM) trimShortString(s Value, start, end bool) Value {
	data := math.Float64bits(s.n)
	n := int(s.o.shortStringBytes)
	var trimmed uint64
	var trimmedN int
	if shortstrings.IsASCII(data) {
		trimmed, trimmedN = shortstrings.TrimASCII(data, n, start, end)
	} else {
		text := shortstrings.String(data, n)
		part := numconv.TrimWhiteSpace(text, start, end)
		trimmedN = len(part)
		if trimmedN == n {
			return s
		}
		trimmed = shortstrings.Pack(part)
	}
	if trimmedN == n {
		return s
	}
	if trimmedN == 0 {
		return vm.emptyString
	}
	if trimmedN == 1 && byte(trimmed) < 0x80 {
		return vm.strings.ascii[byte(trimmed)]
	}
	return Value{
		n: math.Float64frombits(trimmed),
		o: &vm.shapes.shortStrings[trimmedN],
	}
}

// RepeatShort returns count copies of s and reports whether the result fits a packed string.
// s must be a string, and count must be nonnegative.
func (vm *VM) RepeatShort(s Value, count int) (repeated Value, ok bool) {
	// Packed sentinels have no string data.
	if len(s.o.str) > shortstrings.MaxBytes && count != 0 {
		return
	}
	repeated, ok = vm.repeatShort(s, count)
	return
}

func (vm *VM) repeatShort(s Value, count int) (Value, bool) {
	if count == 0 {
		return vm.emptyString, true
	}
	n := s.StringByteLength()
	if n == 0 {
		return vm.emptyString, true
	}
	if count > shortstrings.MaxBytes/n {
		return Value{}, false
	}
	if count == 1 {
		return s, true
	}
	var data uint64
	if s.isShortString() {
		data = math.Float64bits(s.n)
	} else {
		data = shortstrings.Pack(s.o.Str())
	}
	repeated, repeatedN, ok := shortstrings.Repeat(data, n, count)
	if !ok {
		return Value{}, false
	}
	return Value{
		n: math.Float64frombits(repeated),
		o: &vm.shapes.shortStrings[repeatedN],
	}, true
}

// CompareStrings orders a and b by UTF-16 code units and returns -1, 0, or 1.
// Both values must be strings.
func CompareStrings(a, b Value) int {
	if a.isShortString() || b.isShortString() {
		aN, bN := a.StringByteLength(), b.StringByteLength()
		if aN <= shortstrings.MaxBytes && bN <= shortstrings.MaxBytes && (a.o.flags|b.o.flags)&objReversed == 0 {
			return compareShortStrings(a, aN, b, bN)
		}
	}
	return wtf8.Compare(a.Str(), b.Str())
}

// NewStringBytes returns a string value containing the canonical WTF-8 bytes s.
func (vm *VM) NewStringBytes(s []byte) Value {
	if len(s) == 0 {
		return vm.emptyString
	}
	if len(s) == 1 && s[0] < 0x80 {
		return vm.strings.ascii[s[0]]
	}
	if len(s) <= shortstrings.MaxBytes {
		return vm.shortString(string(s))
	}
	return vm.NewString(string(s))
}

func equalShortStrings(a, b Value) bool {
	if !a.isShortString() {
		a, b = b, a
	}
	n := int(a.o.shortStringBytes)
	if b.isShortString() {
		return n == int(b.o.shortStringBytes) && math.Float64bits(a.n) == math.Float64bits(b.n)
	}
	if n != len(b.o.str) {
		return false
	}
	return math.Float64bits(a.n) == shortstrings.Pack(b.o.Str())
}

// WriteToBuilder appends the string value v to b.
func (v Value) WriteToBuilder(b *xstrings.Builder) {
	if v.isShortString() {
		b.WritePacked(math.Float64bits(v.n), int(v.o.shortStringBytes))
		return
	}
	b.WriteString(v.o.Str())
}

// StringFromBuilder returns b's string in vm's realm.
func (vm *VM) StringFromBuilder(b *xstrings.Builder) Value {
	data, n, ok := b.Packed()
	if !ok {
		return vm.NewString(b.String())
	}
	if n == 0 {
		return vm.emptyString
	}
	if n == 1 && byte(data) < 0x80 {
		return vm.strings.ascii[byte(data)]
	}
	return Value{
		n: math.Float64frombits(data),
		o: &vm.shapes.shortStrings[n],
	}
}

// AppendStringBytes appends the string value's canonical WTF-8 bytes to dst.
func (v Value) AppendStringBytes(dst []byte) []byte {
	if v.isShortString() {
		data := shortstrings.Bytes(math.Float64bits(v.n))
		return append(dst, data[:v.o.shortStringBytes]...)
	}
	return append(dst, v.o.Str()...)
}
