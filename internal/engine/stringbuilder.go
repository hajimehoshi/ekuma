// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// stringBuilder builds a canonical WTF-8 string, packed like a short string value while it fits.
// Its zero value is ready to use.
type stringBuilder struct {
	data uint64
	n    int
	long bool
	text wtf8.Buffer
}

func (b *stringBuilder) appendPacked(data uint64, n int) bool {
	joined, size, ok := shortstrings.Concat(b.data, b.n, data, n)
	if !ok {
		return false
	}
	b.data, b.n = joined, size
	return true
}

func (b *stringBuilder) materialize() {
	if b.long {
		return
	}
	b.long = true
	data := shortstrings.Bytes(b.data)
	b.text.WriteStringBytes(data[:b.n])
}

// WriteString appends s.
func (b *stringBuilder) WriteString(s wtf8.String) {
	if b.long {
		b.text.WriteString(s)
		return
	}
	if len(s) <= shortstrings.MaxBytes && b.appendPacked(shortstrings.Pack(s), len(s)) {
		return
	}
	b.materialize()
	b.text.WriteString(s)
}

// WriteRune appends r, which must be a Unicode code point or surrogate.
func (b *stringBuilder) WriteRune(r rune) {
	if !b.long && r < utf8.RuneSelf && b.n < shortstrings.MaxBytes {
		b.data |= uint64(r) << (8 * b.n)
		b.n++
		return
	}
	if b.long {
		b.text.WriteRune(r)
		return
	}
	var buf [utf8.UTFMax]byte
	b.WriteString(wtf8.String(wtf8.AppendRune(buf[:0], r)))
}

// WriteUnit appends the UTF-16 code unit u.
func (b *stringBuilder) WriteUnit(u uint16) {
	b.WriteRune(rune(u))
}

// packed returns the string and its byte length in packed form when available.
func (b *stringBuilder) packed() (uint64, int, bool) {
	return b.data, b.n, !b.long
}

// String returns the built string.
func (b *stringBuilder) String() wtf8.String {
	if b.long {
		return b.text.String()
	}
	return shortstrings.String(b.data, b.n)
}

// StringFromCodeUnits returns the string value whose code units are units.
func (cx *ExecutionContext) StringFromCodeUnits(units []uint16) Value {
	// Every code unit takes at least one byte.
	if len(units) > shortstrings.MaxBytes {
		return cx.NewString(wtf8.FromUTF16(units))
	}
	var b stringBuilder
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
	var b stringBuilder
	for _, r := range points {
		b.WriteRune(r)
	}
	return cx.builtString(&b)
}

// builtString returns b's string as a string value.
func (cx *ExecutionContext) builtString(b *stringBuilder) Value {
	data, n, ok := b.packed()
	if !ok {
		return cx.NewString(b.String())
	}
	if n == 0 {
		return cx.emptyString
	}
	if n == 1 && byte(data) < 0x80 {
		return cx.strings.ascii[byte(data)]
	}
	return Value{
		n: math.Float64frombits(data),
		o: &cx.realm.shapes.shortStrings[n],
	}
}
