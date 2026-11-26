// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func (v Value) IsShortString() bool {
	return v.IsString() && len(v.O.Text()) <= shortstrings.MaxBytes
}

func (r *RealmPart) ShortString(s wtf8.String) Value {
	// The immutable sentinel carries the byte length and the originating realm.
	return Value{
		N: math.Float64frombits(shortstrings.Pack(s)),
		O: &r.Shapes.ShortStrings[len(s)],
	}
}

// NewString returns the string s as a value.
func (r *RealmPart) NewString(s wtf8.String) Value {
	switch len(s) {
	case 0:
		return r.Strings.Empty
	case 1:
		if s[0] < 0x80 {
			return r.Strings.Character(s[0])
		}
	}
	if len(s) <= shortstrings.MaxBytes {
		return r.ShortString(s)
	}
	return Value{
		O: newText(&textObject{
			Kind:       KindString,
			shape:      r.Shapes.String,
			text:       s,
			objectTail: &zeroTail,
		}),
	}
}

// SharedString returns the string s as a value that may be the same object as a property key
// with the same contents.
func (r *RealmPart) SharedString(s wtf8.String) Value {
	if o := r.Strings.lookup(s); o != nil {
		return ObjectValue(o)
	}
	return r.NewString(s)
}

// UnitString returns the string that consists of the code unit u.
func (r *RealmPart) UnitString(u uint16) Value {
	if u < 0x80 {
		return r.Strings.Character(byte(u))
	}
	var buf [utf8.UTFMax]byte
	return r.ShortString(wtf8.String(wtf8.AppendRune(buf[:0], rune(u))))
}

// StringFromCodeUnits returns the string value whose code units are units.
func (r *RealmPart) StringFromCodeUnits(units []uint16) Value {
	// Every code unit takes at least one byte.
	if len(units) > shortstrings.MaxBytes {
		return r.NewString(wtf8.FromUTF16(units))
	}
	var b shortstrings.Builder
	for _, u := range units {
		b.WriteUnit(u)
	}
	return r.builtString(&b)
}

// CodePointsToString returns the string value of points, code points that may be surrogates.
func (r *RealmPart) CodePointsToString(points []rune) Value {
	// Every code point takes at least one byte.
	if len(points) > shortstrings.MaxBytes {
		var b wtf8.Buffer
		for _, c := range points {
			b.WriteRune(c)
		}
		return r.NewString(b.String())
	}
	var b shortstrings.Builder
	for _, c := range points {
		b.WriteRune(c)
	}
	return r.builtString(&b)
}

// builtString returns b's string as a string value.
func (r *RealmPart) builtString(b *shortstrings.Builder) Value {
	data, n, ok := b.Packed()
	if !ok {
		return r.NewString(b.String())
	}
	if n == 0 {
		return r.Strings.Empty
	}
	if n == 1 && byte(data) < 0x80 {
		return r.Strings.Character(byte(data))
	}
	return Value{
		N: math.Float64frombits(data),
		O: &r.Shapes.ShortStrings[n],
	}
}

// NumberToString is quiet.Context.NumberToString.
func (r *RealmPart) NumberToString(n float64) Value {
	var buf [32]byte
	text := numconv.AppendNumber(buf[:0], n)
	if len(text) == 1 {
		return r.Strings.Character(text[0])
	}
	if len(text) <= shortstrings.MaxBytes {
		return r.ShortString(wtf8.String(text))
	}
	return r.NewString(wtf8.String(text))
}

func (v Value) ShortText() wtf8.String {
	return shortstrings.String(math.Float64bits(v.N), len(v.O.Text()))
}

// Str returns the content of the string value.
func (v Value) Str() wtf8.String {
	// Keep the cached-text path small enough to inline.
	if v.O.flags&ObjInterned != 0 {
		return v.O.Text()
	}
	return v.stringSlow()
}

func (v Value) stringSlow() wtf8.String {
	if v.IsShortString() {
		return v.ShortText()
	}
	return v.O.Str()
}

func (v Value) RawString() wtf8.String {
	if v.IsShortString() && v.O.flags&ObjInterned == 0 {
		return v.ShortText()
	}
	return v.O.Text()
}

// StringLength returns the number of UTF-16 code units in the string value.
func (v Value) StringLength() int {
	if v.O.flags&(ObjASCIIKnown|ObjASCII) == ObjASCIIKnown|ObjASCII {
		return len(v.O.Text())
	}
	return v.stringLengthSlow()
}

func (v Value) stringLengthSlow() int {
	if v.IsShortString() {
		// Interned objects own the text used by this cache.
		if v.O.flags&(ObjInterned|ObjASCIIKnown) == ObjInterned {
			v.O.classifyASCII()
		}
		return shortstrings.Len(math.Float64bits(v.N), len(v.O.Text()))
	}
	return v.O.StringLength()
}

func (v Value) StringObject() *Object {
	if !v.IsShortString() || v.O.flags&ObjInterned != 0 {
		return v.O
	}
	return newText(&textObject{
		Kind:       KindString,
		shape:      v.O.shape,
		text:       v.ShortText(),
		objectTail: &zeroTail,
	})
}

// WriteStringTo appends the string value to b.
func (v Value) WriteStringTo(b *wtf8.Buffer) {
	if v.IsShortString() {
		data := shortstrings.Bytes(math.Float64bits(v.N))
		b.WriteStringBytes(data[:len(v.O.Text())])
		return
	}
	b.WriteString(v.O.Str())
}

// UnitAt returns the code unit at i in the string value. i must be in range.
func (v Value) UnitAt(i int) uint16 {
	if !v.IsShortString() {
		return v.O.UnitAt(i)
	}
	return shortstrings.UnitAt(math.Float64bits(v.N), i)
}

// CodePointAt returns the code point starting at UTF-16 index i. i must be in range.
func (v Value) CodePointAt(i int) rune {
	if !v.IsShortString() {
		return v.O.CodePointAt(i)
	}
	return shortstrings.CodePointAt(math.Float64bits(v.N), i)
}

// Substring returns the string's code units from start up to end, which must be in range and in order.
func (v Value) Substring(start, end int) wtf8.String {
	if v.O.flags&(ObjASCIIKnown|ObjASCII|ObjReversed) == ObjASCIIKnown|ObjASCII {
		return v.O.Text()[start:end]
	}
	return v.substringSlow(start, end)
}

func (v Value) substringSlow(start, end int) wtf8.String {
	if !v.IsShortString() {
		return v.O.Substring(start, end)
	}
	data, n := shortstrings.Slice(math.Float64bits(v.N), start, end)
	return shortstrings.String(data, n)
}

// SubstringEquals reports whether the substring from start up to end equals other.
// Both values must be strings, and the UTF-16 indices must be in range and in order.
func (v Value) SubstringEquals(start, end int, other Value) bool {
	if !v.IsShortString() {
		part := v.O.Substring(start, end)
		if other.IsShortString() {
			return len(part) == len(other.O.Text()) && shortstrings.Pack(part) == math.Float64bits(other.N)
		}
		return part == other.O.Str()
	}
	data, n := shortstrings.Slice(math.Float64bits(v.N), start, end)
	if other.IsShortString() {
		return n == len(other.O.Text()) && data == math.Float64bits(other.N)
	}
	return false
}

func (v Value) WriteShortString(b *strings.Builder) {
	data := shortstrings.Bytes(math.Float64bits(v.N))
	b.Write(data[:len(v.O.Text())])
}

// Substring returns the code units of s from start up to end, which must be in range and in order.
func (r *RealmPart) Substring(s Value, start, end int) Value {
	if !s.IsShortString() {
		return r.NewString(s.O.Substring(start, end))
	}
	if start == end {
		return r.Strings.Empty
	}
	data, n := shortstrings.Slice(math.Float64bits(s.N), start, end)
	if n == 1 && byte(data) < 0x80 {
		return r.Strings.Character(byte(data))
	}
	return Value{
		N: math.Float64frombits(data),
		O: &r.Shapes.ShortStrings[n],
	}
}

// StringByteLength returns the byte length of the string's canonical WTF-8 encoding.
func (v Value) StringByteLength() int {
	return len(v.O.Text())
}

// UnitIndexOf returns the first UTF-16 index of sub at or after from, or -1.
// Both values must be strings, and from must be nonnegative. poll, which may be nil, is called
// now and then during a long search.
func (v Value) UnitIndexOf(sub Value, from int, poll func()) int {
	if len(v.O.Text()) > shortstrings.MaxBytes && (len(sub.O.Text()) > shortstrings.MaxBytes || sub.O.flags&ObjInterned != 0) {
		return v.O.UnitIndexOf(sub.O.Str(), from, poll)
	}
	return v.shortStringIndex(sub, from, false, poll)
}

// UnitLastIndexOf returns the last UTF-16 index of sub at or before from, or -1.
// Both values must be strings, and from must be nonnegative. poll is as for [Value.UnitIndexOf].
func (v Value) UnitLastIndexOf(sub Value, from int, poll func()) int {
	if len(v.O.Text()) > shortstrings.MaxBytes && (len(sub.O.Text()) > shortstrings.MaxBytes || sub.O.flags&ObjInterned != 0) {
		return v.O.UnitLastIndexOf(sub.O.Str(), from, poll)
	}
	return v.shortStringIndex(sub, from, true, poll)
}

func (v Value) shortStringIndex(sub Value, from int, backward bool, poll func()) int {
	n, subN := v.StringByteLength(), sub.StringByteLength()
	if n > shortstrings.MaxBytes {
		// The dispatch requires a packed operand, so sub is packed here.
		data := shortstrings.Bytes(math.Float64bits(sub.N))
		text := wtf8.String(data[:subN])
		if backward {
			return v.O.UnitLastIndexOf(text, from, poll)
		}
		return v.O.UnitIndexOf(text, from, poll)
	}
	if subN > shortstrings.MaxBytes {
		return -1
	}
	data, pattern := math.Float64bits(v.N), math.Float64bits(sub.N)
	if backward {
		return shortstrings.LastIndex(data, n, pattern, subN, from)
	}
	return shortstrings.Index(data, n, pattern, subN, from)
}

// IsASCII reports whether s contains only ASCII code units. s must be a string.
func (s Value) IsASCII() bool {
	if s.O.flags&ObjASCIIKnown != 0 {
		return s.O.flags&ObjASCII != 0
	}
	return s.isASCIISlow()
}

func (s Value) isASCIISlow() bool {
	// Interned objects own their text; length sentinels share flags across payloads.
	if len(s.O.Text()) <= shortstrings.MaxBytes && s.O.flags&ObjInterned == 0 {
		return shortstrings.IsASCII(math.Float64bits(s.N))
	}
	return s.O.IsASCII()
}

// IsWellFormed reports whether the string value contains no lone surrogates.
func (v Value) IsWellFormed() bool {
	if v.IsShortString() {
		return shortstrings.IsWellFormed(math.Float64bits(v.N))
	}
	return wtf8.IsWellFormed(v.O.Str())
}

// AppendStringBytes appends the string value's canonical WTF-8 bytes to dst.
func (v Value) AppendStringBytes(dst []byte) []byte {
	if v.IsShortString() {
		data := shortstrings.Bytes(math.Float64bits(v.N))
		return append(dst, data[:len(v.O.Text())]...)
	}
	return append(dst, v.O.Str()...)
}

// IsASCII reports whether s, which is the [Value.RawString] of v when v is a string, consists of
// ASCII characters only.
func IsASCII(v Value, s wtf8.String) bool {
	if v.IsString() && !v.IsShortString() {
		return v.O.IsASCII()
	}
	return wtf8.IsASCII(s)
}

// InitShortStrings creates the sentinels of short strings with the shape of strings.
func (r *RealmPart) InitShortStrings() {
	r.Shapes.ShortStrings = new([shortstrings.MaxBytes + 1]Object)
	for i := range r.Shapes.ShortStrings {
		r.Shapes.ShortStrings[i] = Object{
			Kind:       KindString,
			shape:      r.Shapes.String,
			objectTail: &zeroTail,
		}
		r.Shapes.ShortStrings[i].SetText(wtf8.String("\x00\x00\x00\x00\x00\x00\x00\x00")[:i])
	}
}
