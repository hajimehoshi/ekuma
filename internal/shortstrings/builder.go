// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package shortstrings

import (
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Builder builds a canonical WTF-8 string, packed while it fits.
// Its zero value is ready to use.
type Builder struct {
	data uint64
	n    int
	long bool
	text wtf8.Buffer
}

func (b *Builder) appendPacked(data uint64, n int) bool {
	joined, size, ok := Concat(b.data, b.n, data, n)
	if !ok {
		return false
	}
	b.data, b.n = joined, size
	return true
}

func (b *Builder) materialize() {
	if b.long {
		return
	}
	b.long = true
	data := Bytes(b.data)
	b.text.WriteStringBytes(data[:b.n])
}

// WriteString appends s.
func (b *Builder) WriteString(s wtf8.String) {
	if b.long {
		b.text.WriteString(s)
		return
	}
	if len(s) <= MaxBytes && b.appendPacked(Pack(s), len(s)) {
		return
	}
	b.materialize()
	b.text.WriteString(s)
}

// WriteRune appends r, which must be a Unicode code point or surrogate.
func (b *Builder) WriteRune(r rune) {
	if !b.long && r < utf8.RuneSelf && b.n < MaxBytes {
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
func (b *Builder) WriteUnit(u uint16) {
	b.WriteRune(rune(u))
}

// Packed returns the built string in packed form and its byte length, and reports whether it fits.
func (b *Builder) Packed() (uint64, int, bool) {
	return b.data, b.n, !b.long
}

// String returns the built string.
func (b *Builder) String() wtf8.String {
	if b.long {
		return b.text.String()
	}
	return String(b.data, b.n)
}
