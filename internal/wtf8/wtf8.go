// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package wtf8 implements JavaScript string semantics on Go strings.
//
// A JavaScript string is a sequence of UTF-16 code units. This package keeps such a string
// in a Go string as WTF-8: UTF-8 in which a surrogate code point that is not part of a pair
// is encoded as a three-byte sequence. A high surrogate followed by a low surrogate is never
// stored as two three-byte sequences, so every JavaScript string has exactly one encoding.
//
// Every function expects its string arguments in that canonical form.
package wtf8

import (
	"strings"
	"unicode/utf8"
)

const (
	surrogateMin     = 0xD800
	lowSurrogateMin  = 0xDC00
	surrogateMax     = 0xDFFF
	supplementaryMin = 0x10000

	// ReplacementChar stands in for a lone surrogate in well-formed text.
	ReplacementChar = 0xFFFD
)

// IsASCII reports whether every byte of s is below 0x80.
func IsASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// IsHighSurrogate reports whether r is a high (leading) surrogate.
func IsHighSurrogate(r rune) bool {
	return r >= surrogateMin && r < lowSurrogateMin
}

// IsLowSurrogate reports whether r is a low (trailing) surrogate.
func IsLowSurrogate(r rune) bool {
	return r >= lowSurrogateMin && r <= surrogateMax
}

// DecodeRune returns the first code point of s and its size in bytes.
// The code point is a surrogate when s starts with a lone surrogate.
func DecodeRune(s string) (rune, int) {
	if len(s) == 0 {
		return utf8.RuneError, 0
	}
	b := s[0]
	if b < 0x80 {
		return rune(b), 1
	}
	if b == 0xED && len(s) >= 3 && s[1] >= 0xA0 {
		return 0xD000 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	}
	return utf8.DecodeRuneInString(s)
}

// DecodeLastRune returns the last code point of s and its size in bytes.
func DecodeLastRune(s string) (rune, int) {
	n := len(s)
	if n == 0 {
		return utf8.RuneError, 0
	}
	if s[n-1] < 0x80 {
		return rune(s[n-1]), 1
	}
	if n >= 3 && s[n-3] == 0xED && s[n-2] >= 0xA0 {
		return 0xD000 | rune(s[n-2]&0x3F)<<6 | rune(s[n-1]&0x3F), 3
	}
	return utf8.DecodeLastRuneInString(s)
}

// AppendRune appends the encoding of r, which may be a surrogate, to b.
// The caller is responsible for not leaving a high surrogate directly before a low one.
func AppendRune(b []byte, r rune) []byte {
	if r >= surrogateMin && r <= surrogateMax {
		return append(b, 0xED, 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(b, r)
}

// Len returns the length of s in UTF-16 code units.
func Len(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		b := s[i]
		// Every byte except a continuation byte starts a code point,
		// and a four-byte sequence is two code units.
		if b&0xC0 != 0x80 {
			n++
		}
		if b >= 0xF0 {
			n++
		}
	}
	return n
}

// Pos is a position in a string: the code unit Unit starts at the byte Byte.
// Low is set when the code unit is the low surrogate of the four-byte sequence at Byte.
type Pos struct {
	Unit int
	Byte int
	Low  bool
}

// Seek returns the position of the code unit unit of s, starting the search at from,
// which must be a position in s. unit is clamped to the range from 0 to the length of s.
func Seek(s string, from Pos, unit int) Pos {
	if unit <= 0 {
		return Pos{}
	}
	p := from
	if unit < p.Unit/2 {
		p = Pos{}
	}
	if p.Low {
		// Normalize to the start of the code point so that stepping is uniform.
		p.Low = false
		p.Unit--
	}
	for p.Unit < unit && p.Byte < len(s) {
		b := s[p.Byte]
		switch {
		case b < 0x80:
			p.Byte++
			p.Unit++
		case b < 0xE0:
			p.Byte += 2
			p.Unit++
		case b < 0xF0:
			p.Byte += 3
			p.Unit++
		default:
			if p.Unit+1 == unit {
				p.Unit++
				p.Low = true
				return p
			}
			p.Byte += 4
			p.Unit += 2
		}
	}
	for p.Unit > unit {
		p.Byte--
		for s[p.Byte]&0xC0 == 0x80 {
			p.Byte--
		}
		if s[p.Byte] >= 0xF0 {
			if p.Unit-1 == unit {
				p.Unit--
				p.Low = true
				return p
			}
			p.Unit -= 2
		} else {
			p.Unit--
		}
	}
	return p
}

// At returns the code unit at p, which must be a position before the end of s.
func At(s string, p Pos) uint16 {
	b := s[p.Byte]
	if b < 0x80 {
		return uint16(b)
	}
	r, _ := DecodeRune(s[p.Byte:])
	if r < supplementaryMin {
		return uint16(r)
	}
	r -= supplementaryMin
	if p.Low {
		return uint16(lowSurrogateMin + r&0x3FF)
	}
	return uint16(surrogateMin + r>>10)
}

// Slice returns the code units of s from a up to b. a must not be after b.
func Slice(s string, a, b Pos) string {
	if !a.Low && !b.Low {
		return s[a.Byte:b.Byte]
	}
	if a.Unit >= b.Unit {
		return ""
	}
	var buf []byte
	start := a.Byte
	if a.Low {
		buf = AppendRune(buf, rune(At(s, a)))
		start += 4
	}
	if !b.Low {
		return string(append(buf, s[start:b.Byte]...))
	}
	if start <= b.Byte {
		buf = append(buf, s[start:b.Byte]...)
	}
	buf = AppendRune(buf, rune(At(s, Pos{Byte: b.Byte})))
	return string(buf)
}

// endsWithHigh reports whether s ends with a lone high surrogate.
func endsWithHigh(s string) bool {
	n := len(s)
	return n >= 3 && s[n-3] == 0xED && s[n-2] >= 0xA0 && s[n-2] < 0xB0
}

// startsWithLow reports whether s starts with a lone low surrogate.
func startsWithLow(s string) bool {
	return len(s) >= 3 && s[0] == 0xED && s[1] >= 0xB0
}

// JoinsPair reports whether concatenating a and b forms a surrogate pair at the boundary,
// in which case the plain concatenation of the bytes is not canonical.
func JoinsPair(a, b string) bool {
	return endsWithHigh(a) && startsWithLow(b)
}

func combine(hi, lo rune) rune {
	return supplementaryMin + (hi-surrogateMin)<<10 + (lo - lowSurrogateMin)
}

// Concat returns the concatenation of a and b.
func Concat(a, b string) string {
	if !JoinsPair(a, b) {
		return a + b
	}
	hi, _ := DecodeLastRune(a)
	lo, _ := DecodeRune(b)
	buf := make([]byte, 0, len(a)+len(b)-2)
	buf = append(buf, a[:len(a)-3]...)
	buf = utf8.AppendRune(buf, combine(hi, lo))
	buf = append(buf, b[3:]...)
	return string(buf)
}

// Compare compares a and b by their UTF-16 code units and returns -1, 0 or 1.
func Compare(a, b string) int {
	n := min(len(a), len(b))
	for i := range n {
		x, y := a[i], b[i]
		if x == y {
			continue
		}
		// A supplementary code point is two surrogate code units, which sort among the lone
		// surrogates and below the code points from U+E000 on, although its bytes sort above theirs.
		if (x >= 0xF0 && y >= 0xED) || (y >= 0xF0 && x >= 0xED) {
			return compareUnits(a[i:], b[i:])
		}
		if x < y {
			return -1
		}
		return 1
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// compareUnits compares a and b code unit by code unit. Both must start at a code point.
func compareUnits(a, b string) int {
	var pa, pb Pos
	for pa.Byte < len(a) && pb.Byte < len(b) {
		x, y := At(a, pa), At(b, pb)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
		pa = Seek(a, pa, pa.Unit+1)
		pb = Seek(b, pb, pb.Unit+1)
	}
	switch {
	case pa.Byte < len(a):
		return 1
	case pb.Byte < len(b):
		return -1
	}
	return 0
}

// IsWellFormed reports whether s contains no lone surrogate.
func IsWellFormed(s string) bool {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == 0xED && s[i+1] >= 0xA0 {
			return false
		}
	}
	return true
}

// ToValidUTF8 returns s with every lone surrogate replaced by U+FFFD.
func ToValidUTF8(s string) string {
	if IsWellFormed(s) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 {
			sb.WriteRune(ReplacementChar)
			i += 3
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// FromValidUTF8 returns s as a JavaScript string. Invalid UTF-8 is replaced by U+FFFD.
func FromValidUTF8(s string) string {
	if IsASCII(s) || utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// Builder builds a string from code units, code points and strings.
// The zero value is an empty builder.
type Builder struct {
	buf []byte
}

// Grow reserves room for n more bytes.
func (b *Builder) Grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		buf := make([]byte, len(b.buf), 2*cap(b.buf)+n)
		copy(buf, b.buf)
		b.buf = buf
	}
}

// Len returns the number of bytes written so far.
func (b *Builder) Len() int {
	return len(b.buf)
}

// Reset empties the builder.
func (b *Builder) Reset() {
	b.buf = b.buf[:0]
}

func (b *Builder) endsWithHigh() (rune, bool) {
	n := len(b.buf)
	if n >= 3 && b.buf[n-3] == 0xED && b.buf[n-2] >= 0xA0 && b.buf[n-2] < 0xB0 {
		return 0xD000 | rune(b.buf[n-2]&0x3F)<<6 | rune(b.buf[n-1]&0x3F), true
	}
	return 0, false
}

// WriteByte appends the ASCII character c.
func (b *Builder) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

// WriteRune appends the code point r, which may be a surrogate.
func (b *Builder) WriteRune(r rune) {
	if IsLowSurrogate(r) {
		if hi, ok := b.endsWithHigh(); ok {
			b.buf = utf8.AppendRune(b.buf[:len(b.buf)-3], combine(hi, r))
			return
		}
	}
	b.buf = AppendRune(b.buf, r)
}

// WriteUnit appends the code unit u.
func (b *Builder) WriteUnit(u uint16) {
	if u < 0x80 {
		b.buf = append(b.buf, byte(u))
		return
	}
	b.WriteRune(rune(u))
}

// WriteString appends the JavaScript string s.
func (b *Builder) WriteString(s string) {
	if startsWithLow(s) {
		if hi, ok := b.endsWithHigh(); ok {
			lo, _ := DecodeRune(s)
			b.buf = utf8.AppendRune(b.buf[:len(b.buf)-3], combine(hi, lo))
			s = s[3:]
		}
	}
	b.buf = append(b.buf, s...)
}

// WriteRepeated appends the JavaScript string s count times.
func (b *Builder) WriteRepeated(s string, count int) {
	if count <= 0 {
		return
	}
	if JoinsPair(s, s) {
		for range count {
			b.WriteString(s)
		}
		return
	}
	b.WriteString(s)
	n := (count - 1) * len(s)
	if n == 0 {
		return
	}
	b.Grow(n)
	start := len(b.buf)
	b.buf = append(b.buf, s...)
	for w := len(s); w < n; w = len(b.buf) - start {
		b.buf = append(b.buf, b.buf[start:start+min(w, n-w)]...)
	}
}

// String returns the built string.
func (b *Builder) String() string {
	return string(b.buf)
}

// FromUTF16 returns the string with the code units u.
func FromUTF16(u []uint16) string {
	var b Builder
	b.Grow(len(u))
	for _, c := range u {
		b.WriteUnit(c)
	}
	return b.String()
}

// AppendUTF16 appends the code units of s to u.
func AppendUTF16(u []uint16, s string) []uint16 {
	for i := 0; i < len(s); {
		if b := s[i]; b < 0x80 {
			u = append(u, uint16(b))
			i++
			continue
		}
		r, size := DecodeRune(s[i:])
		i += size
		if r < supplementaryMin {
			u = append(u, uint16(r))
			continue
		}
		r -= supplementaryMin
		u = append(u, uint16(surrogateMin+r>>10), uint16(lowSurrogateMin+r&0x3FF))
	}
	return u
}
