// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package xstrings provides operations on JavaScript strings.
package xstrings

import (
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Builder builds a canonical WTF-8 string. Its zero value is ready to use.
type Builder struct {
	data uint64
	n    int
	long bool
	text wtf8.Builder
}

func (b *Builder) appendPacked(data uint64, n int) bool {
	joined, size, ok := shortstrings.Concat(b.data, b.n, data, n)
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
	data := shortstrings.Bytes(b.data)
	b.text.WriteStringBytes(data[:b.n])
}

// WriteString appends s, which must be canonical WTF-8.
func (b *Builder) WriteString(s string) {
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

// WritePacked appends the packed canonical WTF-8 string data with byte length n.
func (b *Builder) WritePacked(data uint64, n int) {
	if !b.long && b.appendPacked(data, n) {
		return
	}
	b.materialize()
	bytes := shortstrings.Bytes(data)
	b.text.WriteStringBytes(bytes[:n])
}

// WriteRune appends r, which must be a Unicode code point or surrogate.
func (b *Builder) WriteRune(r rune) {
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
	b.WriteString(string(wtf8.AppendRune(buf[:0], r)))
}

// WriteUnit appends the UTF-16 code unit u.
func (b *Builder) WriteUnit(u uint16) {
	b.WriteRune(rune(u))
}

// Packed returns the string and its byte length in packed form when available.
func (b *Builder) Packed() (uint64, int, bool) {
	return b.data, b.n, !b.long
}

// String returns the built string in canonical WTF-8.
func (b *Builder) String() string {
	if b.long {
		return b.text.String()
	}
	return shortstrings.String(b.data, b.n)
}
