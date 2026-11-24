// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// StringFromCodeUnits returns the string value whose code units are units.
func (cx *ExecutionContext) StringFromCodeUnits(units []uint16) Value {
	// Every code unit takes at least one byte.
	if len(units) > shortstrings.MaxBytes {
		return cx.NewString(wtf8.FromUTF16(units))
	}
	var b shortstrings.Builder
	for _, u := range units {
		b.WriteUnit(u)
	}
	return cx.builtString(&b)
}

// CodePointsToString returns the string value of points, code points that may be surrogates.
func (cx *ExecutionContext) CodePointsToString(points []rune) Value {
	// Every code point takes at least one byte.
	if len(points) > shortstrings.MaxBytes {
		var b wtf8.Buffer
		for _, r := range points {
			b.WriteRune(r)
		}
		return cx.NewString(b.String())
	}
	var b shortstrings.Builder
	for _, r := range points {
		b.WriteRune(r)
	}
	return cx.builtString(&b)
}

// builtString returns b's string as a string value.
func (cx *ExecutionContext) builtString(b *shortstrings.Builder) Value {
	data, n, ok := b.Packed()
	if !ok {
		return cx.NewString(b.String())
	}
	if n == 0 {
		return cx.strings.empty
	}
	if n == 1 && byte(data) < 0x80 {
		return cx.strings.character(byte(data))
	}
	return Value{
		n: math.Float64frombits(data),
		o: &cx.realm.shapes.shortStrings[n],
	}
}
