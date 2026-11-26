// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// SubstringBetween is like [storage.RealmPart.Substring] for the positions start and end in the
// text of s, which must be in order.
func (cx *ExecutionContext) SubstringBetween(s storage.Value, start, end wtf8.Pos) storage.Value {
	if s.IsShortString() {
		// The packed text gives the code units without a search.
		return cx.Substring(s, start.Unit, end.Unit)
	}
	text := s.O.Str()
	if start.Byte == 0 && !start.Low && end.Byte == len(text) && !end.Low {
		return s
	}
	return cx.NewString(wtf8.Slice(text, start, end))
}

// ToWellFormed returns s with lone surrogates replaced by U+FFFD.
func (cx *ExecutionContext) ToWellFormed(s storage.Value) storage.Value {
	if s.IsShortString() {
		return cx.toWellFormedShort(s, math.Float64bits(s.N), len(s.O.Text()))
	}
	text := s.O.Str()
	repaired := wtf8.String(wtf8.ToValidUTF8(text))
	if repaired == text {
		return s
	}
	return cx.NewString(repaired)
}

func (cx *ExecutionContext) toWellFormedShort(s storage.Value, data uint64, n int) storage.Value {
	repaired := shortstrings.ToWellFormed(data)
	if repaired == data {
		return s
	}
	return storage.Value{
		N: math.Float64frombits(repaired),
		O: &cx.realm.Shapes.ShortStrings[n],
	}
}

// MapASCII converts s to uppercase if upper is true, or lowercase otherwise.
// s must be a string; false means it is non-ASCII or too long to pack.
func (cx *ExecutionContext) MapASCII(s storage.Value, upper bool) (mapped storage.Value, ok bool) {
	if len(s.O.Text()) > shortstrings.MaxBytes {
		return
	}
	mapped, ok = cx.mapASCII(s, upper)
	return
}

func (cx *ExecutionContext) mapASCII(s storage.Value, upper bool) (storage.Value, bool) {
	data := math.Float64bits(s.N)
	if !shortstrings.IsASCII(data) {
		return storage.Value{}, false
	}
	n := len(s.O.Text())
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
		return cx.Strings.Character(byte(mapped)), true
	}
	return storage.Value{
		N: math.Float64frombits(mapped),
		O: &cx.realm.Shapes.ShortStrings[n],
	}, true
}

// TrimString trims whitespace from the selected ends of the string s.
func (cx *ExecutionContext) TrimString(s storage.Value, start, end bool) storage.Value {
	if s.IsShortString() {
		return cx.trimShortString(s, start, end)
	}
	text := s.O.Str()
	trimmed := xstrings.TrimWhiteSpace(text, start, end)
	if len(trimmed) == len(text) {
		return s
	}
	return cx.NewString(trimmed)
}

func (cx *ExecutionContext) trimShortString(s storage.Value, start, end bool) storage.Value {
	data := math.Float64bits(s.N)
	n := len(s.O.Text())
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
		return cx.Strings.Empty
	}
	if trimmedN == 1 && byte(trimmed) < 0x80 {
		return cx.Strings.Character(byte(trimmed))
	}
	return storage.Value{
		N: math.Float64frombits(trimmed),
		O: &cx.realm.Shapes.ShortStrings[trimmedN],
	}
}

// RepeatShort returns count copies of s and reports whether the result fits a packed string.
// s must be a string, and count must be nonnegative.
func (cx *ExecutionContext) RepeatShort(s storage.Value, count int) (repeated storage.Value, ok bool) {
	if len(s.O.Text()) > shortstrings.MaxBytes && count != 0 {
		return
	}
	repeated, ok = cx.repeatShort(s, count)
	return
}

func (cx *ExecutionContext) repeatShort(s storage.Value, count int) (storage.Value, bool) {
	if count == 0 {
		return cx.Strings.Empty, true
	}
	n := s.StringByteLength()
	if n == 0 {
		return cx.Strings.Empty, true
	}
	if count > shortstrings.MaxBytes/n {
		return storage.Value{}, false
	}
	if count == 1 {
		return s, true
	}
	data := math.Float64bits(s.N)
	repeated, repeatedN, ok := shortstrings.Repeat(data, n, count)
	if !ok {
		return storage.Value{}, false
	}
	return storage.Value{
		N: math.Float64frombits(repeated),
		O: &cx.realm.Shapes.ShortStrings[repeatedN],
	}, true
}

// PadShort pads s with fill to target UTF-16 units and reports whether the result fits.
// s and fill must be strings, fill must be nonempty, and need must be positive and equal target minus s's length.
func (cx *ExecutionContext) PadShort(s, fill storage.Value, target, need int, atStart bool) (padded storage.Value, ok bool) {
	if target > shortstrings.MaxBytes {
		return
	}
	padded, ok = cx.padShort(s, fill, need, atStart)
	return
}

func (cx *ExecutionContext) padShort(s, fill storage.Value, need int, atStart bool) (storage.Value, bool) {
	n := s.StringByteLength()
	if n > shortstrings.MaxBytes {
		return storage.Value{}, false
	}
	data := math.Float64bits(s.N)
	var fillData uint64
	fillN := fill.StringByteLength()
	var fillLen int
	if fillN > shortstrings.MaxBytes {
		// Only the required prefix contributes to the result.
		text := fill.O.Str()
		end := wtf8.Seek(text, wtf8.Pos{}, need)
		fillN = end.Byte
		if end.Low {
			fillN += 3
		}
		if fillN > shortstrings.MaxBytes {
			return storage.Value{}, false
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
		fillData = math.Float64bits(fill.N)
		fillLen = shortstrings.Len(fillData, fillN)
	}
	padding, paddingN, ok := shortstrings.Repeat(fillData, fillN, need/fillLen)
	if !ok {
		return storage.Value{}, false
	}
	if tailLen := need % fillLen; tailLen != 0 {
		tail, tailN := shortstrings.Slice(fillData, 0, tailLen)
		padding, paddingN, ok = shortstrings.Concat(padding, paddingN, tail, tailN)
		if !ok {
			return storage.Value{}, false
		}
	}
	if atStart {
		data, n, ok = shortstrings.Concat(padding, paddingN, data, n)
	} else {
		data, n, ok = shortstrings.Concat(data, n, padding, paddingN)
	}
	if !ok {
		return storage.Value{}, false
	}
	if n == 1 && byte(data) < 0x80 {
		return cx.Strings.Character(byte(data)), true
	}
	return storage.Value{
		N: math.Float64frombits(data),
		O: &cx.realm.Shapes.ShortStrings[n],
	}, true
}

// NewStringBytes returns a string value containing the canonical WTF-8 bytes s.
func (cx *ExecutionContext) NewStringBytes(s []byte) storage.Value {
	if len(s) == 0 {
		return cx.Strings.Empty
	}
	if len(s) == 1 && s[0] < 0x80 {
		return cx.Strings.Character(s[0])
	}
	if len(s) <= shortstrings.MaxBytes {
		return cx.ShortString(wtf8.String(s))
	}
	return cx.NewString(wtf8.String(s))
}
