// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"encoding/binary"
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// ConcatCount counts the concatenations in a row that made a string, each up to [BufferAfter]:
// in its low bits those whose left operand was the result of the one before, and from
// [PrependShift] on those whose right operand was.
type ConcatCount uint8

const (
	// BufferAfter is the number of concatenations in a row after which the next one makes its
	// result the tail or the head of a builder.
	BufferAfter ConcatCount = 3

	// MinBufferedLength is the smallest length in bytes of a string that is the tail or the head
	// of a builder.
	MinBufferedLength = 32

	// MaxStackConcatBytes bounds scratch space for multi-part concatenation.
	MaxStackConcatBytes = 64

	PrependShift = 2
)

// Appends returns the number of concatenations in a row each onto the result of the one before
// as its left operand.
func (c ConcatCount) Appends() ConcatCount {
	return c & (1<<PrependShift - 1)
}

// Prepends returns the number of concatenations in a row each onto the result of the one before
// as its right operand.
func (c ConcatCount) Prepends() ConcatCount {
	return c >> PrependShift
}

// ReversedBuilder holds the reversed contents of the reversed strings made from it.
type ReversedBuilder struct {
	Sb strings.Builder
}

// ShortStringsJoinPair reports whether the unreversed strings form a surrogate pair at their join.
func ShortStringsJoinPair(a Value, aN int, b Value, bN int) bool {
	if aN < 3 || bN < 3 {
		return false
	}
	tail := math.Float64bits(a.N) >> (8 * (aN - 3))
	return tail&0xf0ff == 0xa0ed && math.Float64bits(b.N)&0xf0ff == 0xb0ed
}

// NextCount returns the [ConcatCount] of the result of a concatenation without a builder whose
// left and right operands have the counts ca and cb.
func NextCount(ca, cb ConcatCount) ConcatCount {
	return min(ca.Appends()+1, BufferAfter) | min(cb.Prepends()+1, BufferAfter)<<PrependShift
}

// TailBuilder returns the builder whose tail the string o is, or nil.
func TailBuilder(o *Object) *strings.Builder {
	if sb, ok := o.ExtState().(*strings.Builder); ok && sb.Len() == len(o.Text) {
		return sb
	}
	return nil
}

// HeadBuilder returns the builder whose head the string o is, or nil.
func HeadBuilder(o *Object) *ReversedBuilder {
	if rb, ok := o.ExtState().(*ReversedBuilder); ok && rb.Sb.Len() == len(o.Text) {
		return rb
	}
	return nil
}

// Unreverse makes the reversed string o hold its contents in order.
func (o *Object) Unreverse() {
	var sb strings.Builder
	sb.Grow(len(o.Text))
	WriteReversed(&sb, o.Text)
	o.Text = wtf8.String(sb.String())
	o.Flags &^= ObjReversed
	o.SetExtState(nil)
}

// sameStrings reports whether the strings a and b have the same contents.
func sameStrings(a, b *Object) bool {
	// A reversed string has as many bytes as its contents.
	if len(a.Text) != len(b.Text) {
		return false
	}
	if a.Flags&b.Flags&ObjReversed != 0 {
		return a.Text == b.Text
	}
	return a.Str() == b.Str()
}

// JoinsPair reports whether the contents of two strings form a surrogate pair where they meet,
// so that their bytes do not simply concatenate. x and y are the str of the strings, and xr
// and yr report whether they are reversed.
func JoinsPair(x wtf8.String, xr bool, y wtf8.String, yr bool) bool {
	if len(x) < 3 || len(y) < 3 {
		return false
	}
	end := [3]byte{x[len(x)-3], x[len(x)-2], x[len(x)-1]}
	if xr {
		end = [3]byte{x[2], x[1], x[0]}
	}
	start := [3]byte{y[0], y[1], y[2]}
	if yr {
		start = [3]byte{y[len(y)-1], y[len(y)-2], y[len(y)-3]}
	}
	return wtf8.JoinsPair(wtf8.String(end[:]), wtf8.String(start[:]))
}

// WriteReversed appends the bytes of s to sb in reverse order.
func WriteReversed(sb *strings.Builder, s wtf8.String) {
	var buf [256]byte
	for len(s) > 0 {
		n := min(len(s), len(buf))
		reverseBytes(buf[:n], s[len(s)-n:])
		sb.Write(buf[:n])
		s = s[:len(s)-n]
	}
}

// reverseBytes sets dst to the bytes of src in reverse order. dst and src have the same length.
func reverseBytes(dst []byte, src wtf8.String) {
	n := len(src)
	i := 0
	for ; i+8 <= n; i += 8 {
		w := src[n-i-8 : n-i]
		binary.BigEndian.PutUint64(dst[i:], uint64(w[0])|uint64(w[1])<<8|uint64(w[2])<<16|uint64(w[3])<<24|
			uint64(w[4])<<32|uint64(w[5])<<40|uint64(w[6])<<48|uint64(w[7])<<56)
	}
	for ; i < n; i++ {
		dst[i] = src[n-1-i]
	}
}
