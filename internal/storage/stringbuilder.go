// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

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

// NumberToString is QuietContext.NumberToString.
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
