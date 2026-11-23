// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

func (v Value) isShortString() bool {
	return v.IsString() && len(v.o.str) <= shortstrings.MaxBytes
}

func (vm *VM) shortString(s wtf8.String) Value {
	// The immutable sentinel carries the byte length and the originating realm.
	return Value{
		n: math.Float64frombits(shortstrings.Pack(s)),
		o: &vm.curRealm.shapes.shortStrings[len(s)],
	}
}

func (v Value) shortText() wtf8.String {
	return shortstrings.String(math.Float64bits(v.n), len(v.o.str))
}

// Str returns the content of the string value.
func (v Value) Str() wtf8.String {
	// Keep the cached-text path small enough to inline.
	if v.o.flags&objInterned != 0 {
		return v.o.str
	}
	return v.stringSlow()
}

func (v Value) stringSlow() wtf8.String {
	if v.isShortString() {
		return v.shortText()
	}
	return v.o.Str()
}

func (v Value) rawString() wtf8.String {
	if v.isShortString() && v.o.flags&objInterned == 0 {
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
		// Interned objects own the text used by this cache.
		if v.o.flags&(objInterned|objASCIIKnown) == objInterned {
			v.o.classifyASCII()
		}
		return shortstrings.Len(math.Float64bits(v.n), len(v.o.str))
	}
	return v.o.StringLength()
}

func (v Value) stringObject() *Object {
	if !v.isShortString() || v.o.flags&objInterned != 0 {
		return v.o
	}
	return &Object{
		kind:  kindString,
		shape: v.o.shape,
		str:   v.shortText(),
	}
}

// WriteStringTo appends the string value to b.
func (v Value) WriteStringTo(b *wtf8.Buffer) {
	if v.isShortString() {
		data := shortstrings.Bytes(math.Float64bits(v.n))
		b.WriteStringBytes(data[:len(v.o.str)])
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
func (v Value) Substring(start, end int) wtf8.String {
	if v.o.flags&(objASCIIKnown|objASCII|objReversed) == objASCIIKnown|objASCII {
		return v.o.str[start:end]
	}
	return v.substringSlow(start, end)
}

func (v Value) substringSlow(start, end int) wtf8.String {
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
			return len(part) == len(other.o.str) && shortstrings.Pack(part) == math.Float64bits(other.n)
		}
		return part == other.o.Str()
	}
	data, n := shortstrings.Slice(math.Float64bits(v.n), start, end)
	if other.isShortString() {
		return n == len(other.o.str) && data == math.Float64bits(other.n)
	}
	return false
}

func (v Value) writeShortString(b *strings.Builder) {
	data := shortstrings.Bytes(math.Float64bits(v.n))
	b.Write(data[:len(v.o.str)])
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
		o: &vm.curRealm.shapes.shortStrings[n],
	}
}

// SubstringBetween is like [VM.Substring] for the positions start and end in the text of s, which
// must be in order.
func (vm *VM) SubstringBetween(s Value, start, end wtf8.Pos) Value {
	if s.isShortString() {
		// The packed text gives the code units without a search.
		return vm.Substring(s, start.Unit, end.Unit)
	}
	text := s.o.Str()
	if start.Byte == 0 && !start.Low && end.Byte == len(text) && !end.Low {
		return s
	}
	return vm.NewString(wtf8.Slice(text, start, end))
}

// StringByteLength returns the byte length of the string's canonical WTF-8 encoding.
func (v Value) StringByteLength() int {
	return len(v.o.str)
}

// UnitIndexOf returns the first UTF-16 index of sub at or after from, or -1.
// Both values must be strings, and from must be nonnegative. poll, which may be nil, is called
// now and then during a long search.
func (v Value) UnitIndexOf(sub Value, from int, poll func()) int {
	if len(v.o.str) > shortstrings.MaxBytes && (len(sub.o.str) > shortstrings.MaxBytes || sub.o.flags&objInterned != 0) {
		return v.o.UnitIndexOf(sub.o.Str(), from, poll)
	}
	return v.shortStringIndex(sub, from, false, poll)
}

// UnitLastIndexOf returns the last UTF-16 index of sub at or before from, or -1.
// Both values must be strings, and from must be nonnegative. poll is as for [Value.UnitIndexOf].
func (v Value) UnitLastIndexOf(sub Value, from int, poll func()) int {
	if len(v.o.str) > shortstrings.MaxBytes && (len(sub.o.str) > shortstrings.MaxBytes || sub.o.flags&objInterned != 0) {
		return v.o.UnitLastIndexOf(sub.o.Str(), from, poll)
	}
	return v.shortStringIndex(sub, from, true, poll)
}

func (v Value) shortStringIndex(sub Value, from int, backward bool, poll func()) int {
	n, subN := v.StringByteLength(), sub.StringByteLength()
	if n > shortstrings.MaxBytes {
		// The dispatch requires a packed operand, so sub is packed here.
		data := shortstrings.Bytes(math.Float64bits(sub.n))
		text := wtf8.String(data[:subN])
		if backward {
			return v.o.UnitLastIndexOf(text, from, poll)
		}
		return v.o.UnitIndexOf(text, from, poll)
	}
	if subN > shortstrings.MaxBytes {
		return -1
	}
	data, pattern := math.Float64bits(v.n), math.Float64bits(sub.n)
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
	// Interned objects own their text; length sentinels share flags across payloads.
	if len(s.o.str) <= shortstrings.MaxBytes && s.o.flags&objInterned == 0 {
		return shortstrings.IsASCII(math.Float64bits(s.n))
	}
	return s.o.IsASCII()
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
		return vm.toWellFormedShort(s, math.Float64bits(s.n), len(s.o.str))
	}
	text := s.o.Str()
	repaired := wtf8.String(wtf8.ToValidUTF8(text))
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
		o: &vm.curRealm.shapes.shortStrings[n],
	}
}

// MapASCII converts s to uppercase if upper is true, or lowercase otherwise.
// s must be a string; false means it is non-ASCII or too long to pack.
func (vm *VM) MapASCII(s Value, upper bool) (mapped Value, ok bool) {
	if len(s.o.str) > shortstrings.MaxBytes {
		return
	}
	mapped, ok = vm.mapASCII(s, upper)
	return
}

func (vm *VM) mapASCII(s Value, upper bool) (Value, bool) {
	data := math.Float64bits(s.n)
	if !shortstrings.IsASCII(data) {
		return Value{}, false
	}
	n := len(s.o.str)
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
		o: &vm.curRealm.shapes.shortStrings[n],
	}, true
}

// TrimString trims whitespace from the selected ends of the string s.
func (vm *VM) TrimString(s Value, start, end bool) Value {
	if s.isShortString() {
		return vm.trimShortString(s, start, end)
	}
	text := s.o.Str()
	trimmed := xstrings.TrimWhiteSpace(text, start, end)
	if len(trimmed) == len(text) {
		return s
	}
	return vm.NewString(trimmed)
}

func (vm *VM) trimShortString(s Value, start, end bool) Value {
	data := math.Float64bits(s.n)
	n := len(s.o.str)
	var trimmed uint64
	var trimmedN int
	if shortstrings.IsASCII(data) {
		trimmed, trimmedN = shortstrings.TrimASCII(data, n, start, end)
	} else {
		text := shortstrings.String(data, n)
		part := xstrings.TrimWhiteSpace(text, start, end)
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
		o: &vm.curRealm.shapes.shortStrings[trimmedN],
	}
}

// RepeatShort returns count copies of s and reports whether the result fits a packed string.
// s must be a string, and count must be nonnegative.
func (vm *VM) RepeatShort(s Value, count int) (repeated Value, ok bool) {
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
	data := math.Float64bits(s.n)
	repeated, repeatedN, ok := shortstrings.Repeat(data, n, count)
	if !ok {
		return Value{}, false
	}
	return Value{
		n: math.Float64frombits(repeated),
		o: &vm.curRealm.shapes.shortStrings[repeatedN],
	}, true
}

// PadShort pads s with fill to target UTF-16 units and reports whether the result fits.
// s and fill must be strings, fill must be nonempty, and need must be positive and equal target minus s's length.
func (vm *VM) PadShort(s, fill Value, target, need int, atStart bool) (padded Value, ok bool) {
	if target > shortstrings.MaxBytes {
		return
	}
	padded, ok = vm.padShort(s, fill, need, atStart)
	return
}

func (vm *VM) padShort(s, fill Value, need int, atStart bool) (Value, bool) {
	n := s.StringByteLength()
	if n > shortstrings.MaxBytes {
		return Value{}, false
	}
	data := math.Float64bits(s.n)
	var fillData uint64
	fillN := fill.StringByteLength()
	var fillLen int
	if fillN > shortstrings.MaxBytes {
		// Only the required prefix contributes to the result.
		text := fill.o.Str()
		end := wtf8.Seek(text, wtf8.Pos{}, need)
		fillN = end.Byte
		if end.Low {
			fillN += 3
		}
		if fillN > shortstrings.MaxBytes {
			return Value{}, false
		}
		fillData = shortstrings.Pack(text[:end.Byte])
		if end.Low {
			var buf [3]byte
			high := wtf8.At(text, wtf8.Pos{Byte: end.Byte})
			tail := shortstrings.Pack(wtf8.String(wtf8.AppendRune(buf[:0], rune(high))))
			fillData |= tail << (8 * end.Byte)
		}
		fillLen = end.Unit
	} else {
		fillData = math.Float64bits(fill.n)
		fillLen = shortstrings.Len(fillData, fillN)
	}
	padding, paddingN, ok := shortstrings.Repeat(fillData, fillN, need/fillLen)
	if !ok {
		return Value{}, false
	}
	if tailLen := need % fillLen; tailLen != 0 {
		tail, tailN := shortstrings.Slice(fillData, 0, tailLen)
		padding, paddingN, ok = shortstrings.Concat(padding, paddingN, tail, tailN)
		if !ok {
			return Value{}, false
		}
	}
	if atStart {
		data, n, ok = shortstrings.Concat(padding, paddingN, data, n)
	} else {
		data, n, ok = shortstrings.Concat(data, n, padding, paddingN)
	}
	if !ok {
		return Value{}, false
	}
	if n == 1 && byte(data) < 0x80 {
		return vm.strings.ascii[byte(data)], true
	}
	return Value{
		n: math.Float64frombits(data),
		o: &vm.curRealm.shapes.shortStrings[n],
	}, true
}

// CompareStrings orders a and b by UTF-16 code units and returns -1, 0, or 1.
// Both values must be strings.
func CompareStrings(a, b Value) int {
	aN, bN := a.StringByteLength(), b.StringByteLength()
	if aN <= shortstrings.MaxBytes && bN <= shortstrings.MaxBytes {
		return shortstrings.Compare(math.Float64bits(a.n), aN, math.Float64bits(b.n), bN)
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
		return vm.shortString(wtf8.String(s))
	}
	return vm.NewString(wtf8.String(s))
}

// AppendStringBytes appends the string value's canonical WTF-8 bytes to dst.
func (v Value) AppendStringBytes(dst []byte) []byte {
	if v.isShortString() {
		data := shortstrings.Bytes(math.Float64bits(v.n))
		return append(dst, data[:len(v.o.str)]...)
	}
	return append(dst, v.o.Str()...)
}
