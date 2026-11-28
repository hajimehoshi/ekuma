// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"encoding/binary"
	"math"
	"strings"
	"unsafe"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
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
	if p := o.stringBuilder(); p != nil && o.flags&ObjReversed == 0 {
		if sb := (*strings.Builder)(p); sb.Len() == len(o.Text()) {
			return sb
		}
	}
	return nil
}

// HeadBuilder returns the builder whose head the string o is, or nil.
func HeadBuilder(o *Object) *ReversedBuilder {
	if p := o.stringBuilder(); p != nil && o.flags&ObjReversed != 0 {
		if rb := (*ReversedBuilder)(p); rb.Sb.Len() == len(o.Text()) {
			return rb
		}
	}
	return nil
}

// MarkExclusive marks o as a new builder tail or head owned by one variable's register.
func (o *Object) MarkExclusive() {
	o.flags |= ObjExclusive
}

// Share clears [ObjExclusive] of o, which must be a string or BigInt.
func (o *Object) Share() {
	o.flags &^= ObjExclusive
}

// Seal makes an exclusive string or BigInt safe to share.
func (v Value) Seal() {
	if o := v.O; o != nil && (o.Kind == KindString || o.Kind == KindBigInt) && o.flags&ObjExclusive != 0 {
		o.Share()
	}
}

// ExtendExclusive makes the string o, which is marked [ObjExclusive] and was the tail of sb, hold
// the contents of sb after strings were appended to them. ascii reports whether the appended
// strings are ASCII.
func (o *Object) ExtendExclusive(sb *strings.Builder, ascii bool) {
	if ValidateExec && (o.flags&(ObjExclusive|ObjInterned|ObjReversed|ObjASCIIKnown) != ObjExclusive|ObjASCIIKnown || o.length != 0 || o.stringBuilder() != unsafe.Pointer(sb)) {
		panic("storage: ExtendExclusive of a string that is not an exclusive tail")
	}
	o.extendExclusive(wtf8.String(sb.String()), ascii)
}

// ExtendHeadExclusive updates the exclusive string o to the current contents of rb.
func (o *Object) ExtendHeadExclusive(rb *ReversedBuilder, ascii bool) {
	if ValidateExec && (o.flags&(ObjExclusive|ObjInterned|ObjReversed|ObjASCIIKnown) != ObjExclusive|ObjReversed|ObjASCIIKnown || o.length != 0 || o.stringBuilder() != unsafe.Pointer(rb)) {
		panic("storage: ExtendHeadExclusive of a string that is not an exclusive head")
	}
	o.extendExclusive(wtf8.String(rb.Sb.String()), ascii)
}

func (o *Object) extendExclusive(s wtf8.String, ascii bool) {
	n := len(o.Text())
	o.SetText(s)
	if o.flags&ObjASCII != 0 {
		if !ascii {
			o.flags &^= ObjASCII
		}
		return
	}
	// Growth changes the length. A tail's UTF-16 positions stay valid, and a
	// reversed head caches only the length.
	if m := o.Meta(); m != nil && m.Length != 0 {
		m.Length += int32(wtf8.Len(s[n:]))
	}
}

// Unreverse makes the reversed string o hold its contents in order.
func (o *Object) Unreverse() {
	var sb strings.Builder
	sb.Grow(len(o.Text()))
	WriteReversed(&sb, o.Text())
	o.SetText(wtf8.String(sb.String()))
	o.flags &^= ObjReversed
	o.ClearBuilder()
}

// sameStrings reports whether the strings a and b have the same contents.
func sameStrings(a, b *Object) bool {
	// A reversed string has as many bytes as its contents.
	if len(a.Text()) != len(b.Text()) {
		return false
	}
	if a.flags&b.flags&ObjReversed != 0 {
		return a.Text() == b.Text()
	}
	return a.Str() == b.Str()
}

// JoinsPair reports whether the contents of two strings form a surrogate pair where they meet,
// so that their bytes do not simply concatenate. x and y are the [Value.RawString] of the strings,
// and xr and yr report whether they are reversed.
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

// ConcatASCIIStrings joins first and rest if they are unreversed ASCII strings.
// length is their combined byte length and must fit within [MaxStringLength].
func (r *RealmPart) ConcatASCIIStrings(first Value, rest []Value, length int) (Value, bool) {
	if !flatASCIIString(first) {
		return Value{}, false
	}
	for _, s := range rest {
		if !flatASCIIString(s) {
			return Value{}, false
		}
	}
	var w joinWriter
	w.sb.Grow(length)
	writeFlatString(&w, first)
	for _, s := range rest {
		writeFlatString(&w, s)
	}
	w.flush()
	return r.newJoined(w.sb.String()), true
}

func flatASCIIString(v Value) bool {
	if !v.IsString() || v.O.flags&ObjReversed != 0 {
		return false
	}
	if v.StringByteLength() <= shortstrings.MaxBytes {
		return shortstrings.IsASCII(math.Float64bits(v.N))
	}
	return v.O.flags&(ObjASCIIKnown|ObjASCII) == ObjASCIIKnown|ObjASCII
}

func writeFlatString(w *joinWriter, v Value) {
	text := v.O.Text()
	if len(text) <= shortstrings.MaxBytes {
		w.writePacked(math.Float64bits(v.N), len(text))
		return
	}
	if w.k != 0 {
		w.flush()
	}
	w.sb.WriteString(string(text))
}

// NewConcatenatedString returns the string s, which is longer than a short string, with the
// concatenation count count.
func (r *RealmPart) NewConcatenatedString(s wtf8.String, count ConcatCount) Value {
	return Value{
		O: newText(&textObject{
			Kind:       KindString,
			Pinned:     uint8(count),
			shape:      r.Shapes.String,
			text:       s,
			objectTail: &zeroTail,
		}),
	}
}

// NewBufferedString returns a string with its builder, a *strings.Builder or, for a string
// marked ObjReversed, a *ReversedBuilder, and optional UTF-16 metadata.
func (r *RealmPart) NewBufferedString(s wtf8.String, flags uint8, builder any, meta *ObjectMeta) Value {
	var b unsafe.Pointer
	switch x := builder.(type) {
	case *strings.Builder:
		b = unsafe.Pointer(x)
	case *ReversedBuilder:
		b = unsafe.Pointer(x)
	}
	if ValidateExec {
		if _, ok := builder.(*ReversedBuilder); ok != (flags&ObjReversed != 0) {
			panic("storage: NewBufferedString with a builder of the other direction")
		}
	}
	if meta == nil {
		return Value{
			O: newText(&textObject{
				Kind:       KindString,
				flags:      flags,
				shape:      r.Shapes.String,
				text:       s,
				builder:    b,
				objectTail: &zeroTail,
			}),
		}
	}
	// The metadata belongs only to this string, so both have the same lifetime.
	p := &struct {
		object textTailObject
		meta   ObjectMeta
	}{
		object: textTailObject{
			Kind:    KindString,
			flags:   flags,
			shape:   r.Shapes.String,
			text:    s,
			builder: b,
		},
		meta: *meta,
	}
	p.object.tail.Ext = &p.meta
	return Value{O: p.object.Init()}
}
