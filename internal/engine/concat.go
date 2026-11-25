// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// The internal state (see [storage.Object.ExtState]) of a string that a concatenation made tells how a concatenation onto it proceeds.
//
// A *strings.Builder whose contents are the string makes the string the tail of its builder: a
// concatenation onto it appends to the builder, and the result becomes the tail in its place.
// The strings made from one builder share its memory, and each keeps its contents, because a
// builder never changes the bytes it holds. Only the tail refers to the builder, so that the
// earlier strings keep no more memory alive than they had when they were made.
//
// A *reversedBuilder whose contents are the str of a reversed string (see [storage.ObjReversed]) makes
// the string the head of its builder in the same way: a concatenation that prepends to it
// appends the prepended bytes in reverse order to the builder, and the result becomes the head
// in its place. Only a head refers to its builder, so the internal state of a reversed
// string is its builder or nil.
//
// A concatCount counts the concatenations in a row that made the string without a builder.

// Both counts of a concatCount must fit in their bits.
const _ uint = 1<<prependShift - 1 - uint(bufferAfter)

// Concat returns the concatenation of the strings a and b.
func (cx *ExecutionContext) Concat(a, b Value) Value {
	if sb := storage.TailBuilder(a.O); sb != nil && b.O.Flags&(objASCIIKnown|objASCII|objReversed) == objASCIIKnown|objASCII {
		y := b.O.Text
		if y == "" {
			return a
		}
		if len(y) > MaxStringLength-len(a.O.Text) {
			cx.ThrowRangeError("invalid string length")
		}
		// An ASCII suffix cannot join a surrogate pair.
		sb.WriteString(string(y))
		return cx.extendTail(a.O, sb, true)
	}
	if b.O.Flags&objReversed != 0 && a.O.Flags&(objASCIIKnown|objASCII|objReversed) == objASCIIKnown|objASCII {
		if rb := storage.HeadBuilder(b.O); rb != nil {
			x := a.O.Text
			if x == "" {
				return b
			}
			if len(x) > MaxStringLength-len(b.O.Text) {
				cx.ThrowRangeError("invalid string length")
			}
			storage.WriteReversed(&rb.Sb, x)
			return cx.extendHead(b.O, rb, true)
		}
	}
	aN := a.StringByteLength()
	if aN <= shortstrings.MaxBytes {
		if aN == 0 {
			return b
		}
		bN := b.StringByteLength()
		if bN == 0 {
			return a
		}
		if bN <= shortstrings.MaxBytes && aN+bN <= shortstrings.MaxBytes+2 &&
			(aN+bN <= shortstrings.MaxBytes || storage.ShortStringsJoinPair(a, aN, b, bN)) {
			if v, ok := cx.concatShort(a, aN, b, bN); ok {
				return v
			}
		}
	}
	return cx.concat(a, a.RawString(), b, b.RawString())
}

func (cx *ExecutionContext) concatShort(a Value, aN int, b Value, bN int) (Value, bool) {
	x, y := math.Float64bits(a.N), math.Float64bits(b.N)
	data, n, ok := shortstrings.Concat(x, aN, y, bN)
	if !ok {
		return Value{}, false
	}
	return Value{
		N: math.Float64frombits(data),
		O: &cx.realm.Shapes.ShortStrings[n],
	}, true
}

// concat returns the concatenation of a and b. x and y are the str of a and b when those are
// strings, and their string forms otherwise.
func (cx *ExecutionContext) concat(a Value, x wtf8.String, b Value, y wtf8.String) Value {
	if x == "" && b.IsString() {
		return b
	}
	if y == "" && a.IsString() {
		return a
	}
	n := len(x) + len(y)
	if n > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	if b.IsString() && b.O.Flags&objReversed != 0 {
		return cx.concatReversed(a, x, b, y)
	}
	var ca, cb concatCount
	if a.IsString() {
		// A tail is never reversed.
		if sb := storage.TailBuilder(a.O); sb != nil {
			if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
				return cx.NewString(wtf8.Concat(x, y))
			}
			sb.WriteString(string(y))
			return cx.extendTail(a.O, sb, storage.IsASCII(b, y))
		}
		if a.O.Flags&objReversed != 0 {
			return cx.concatReversed(a, x, b, y)
		}
		ca = a.O.ExtConcatCount()
	}
	if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
		return cx.NewString(wtf8.Concat(x, y))
	}
	if b.IsString() {
		cb = b.O.ExtConcatCount()
	}
	if n >= minBufferedLength {
		if ca.Appends() >= bufferAfter {
			sb := new(strings.Builder)
			sb.Grow(n)
			sb.WriteString(string(x))
			sb.WriteString(string(y))
			return cx.newTail(sb, storage.IsASCII(a, x) && storage.IsASCII(b, y))
		}
		if cb.Prepends() >= bufferAfter {
			rb := new(reversedBuilder)
			rb.Sb.Grow(n)
			storage.WriteReversed(&rb.Sb, y)
			storage.WriteReversed(&rb.Sb, x)
			return cx.newHead(rb, storage.IsASCII(a, x) && storage.IsASCII(b, y))
		}
	}
	if n <= shortstrings.MaxBytes {
		var data [shortstrings.MaxBytes]byte
		copy(data[:], x)
		copy(data[len(x):], y)
		return cx.ShortString(wtf8.String(data[:n]))
	}
	return cx.concatenated(x+y, storage.NextCount(ca, cb))
}

// concatReversed is [ExecutionContext.concat] for operands one or both of which are reversed strings.
func (cx *ExecutionContext) concatReversed(a Value, x wtf8.String, b Value, y wtf8.String) Value {
	ar := a.IsString() && a.O.Flags&objReversed != 0
	br := b.IsString() && b.O.Flags&objReversed != 0
	asciiX := storage.IsASCII(a, x)
	if !br || !asciiX && !b.O.IsASCII() && storage.JoinsPair(x, ar, y, true) {
		// Only a prepend keeps the result reversed.
		if ar {
			x = a.Str()
		}
		if br {
			y = b.Str()
		}
		return cx.concat(a, x, b, y)
	}
	rb := storage.HeadBuilder(b.O)
	if rb == nil {
		rb = new(reversedBuilder)
		rb.Sb.Grow(len(x) + len(y))
		rb.Sb.WriteString(string(y))
	}
	if ar {
		rb.Sb.WriteString(string(x))
	} else {
		storage.WriteReversed(&rb.Sb, x)
	}
	return cx.extendHead(b.O, rb, asciiX)
}

// ConcatStrings returns the concatenation of the strings parts.
func (cx *ExecutionContext) ConcatStrings(parts []Value) Value {
	var n, nonEmpty, previousBytes int
	var needsUnpacking bool
	var flags uint8
	last := -1
	for i, p := range parts {
		nbytes := len(p.O.Text)
		needsUnpacking = needsUnpacking || nbytes <= shortstrings.MaxBytes && p.O.Flags&objInterned == 0
		if nbytes == 0 {
			continue
		}
		// A lone surrogate occupies three bytes in WTF-8.
		if previousBytes >= 3 && nbytes >= 3 && wtf8.JoinsPair(parts[last].RawString(), p.RawString()) {
			acc := parts[0]
			for _, p := range parts[1:] {
				acc = cx.Concat(acc, p)
			}
			return acc
		}
		n += nbytes
		if n > MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		flags |= p.O.Flags
		last = i
		previousBytes = nbytes
		nonEmpty++
	}
	switch nonEmpty {
	case 0:
		return cx.Strings.Empty
	case 1:
		return parts[last]
	}
	if flags&objReversed != 0 {
		return cx.concatReversedStrings(parts)
	}

	first := parts[0].O
	if sb := storage.TailBuilder(first); sb != nil {
		ascii := true
		for _, p := range parts[1:] {
			s := p.RawString()
			sb.WriteString(string(s))
			ascii = ascii && storage.IsASCII(p, s)
		}
		return cx.extendTail(first, sb, ascii)
	}
	ca := first.ExtConcatCount()
	cb := parts[len(parts)-1].O.ExtConcatCount()
	if n < minBufferedLength || ca.Appends() < bufferAfter && cb.Prepends() < bufferAfter {
		if n <= maxStackConcatBytes {
			return cx.concatSmall(parts, n, storage.NextCount(ca, cb), needsUnpacking)
		}
		var sb strings.Builder
		sb.Grow(n)
		if needsUnpacking {
			for _, p := range parts {
				if p.IsShortString() {
					p.WriteShortString(&sb)
				} else {
					sb.WriteString(string(p.O.Text))
				}
			}
		} else {
			for _, p := range parts {
				sb.WriteString(string(p.O.Text))
			}
		}
		return cx.concatenated(wtf8.String(sb.String()), storage.NextCount(ca, cb))
	}
	return cx.newBuffered(parts, n, ca.Appends() >= bufferAfter)
}

// concatSmall joins parts with total byte length n, at most maxStackConcatBytes.
// No part is reversed and no boundary forms a surrogate pair.
func (cx *ExecutionContext) concatSmall(parts []Value, n int, count concatCount, needsUnpacking bool) Value {
	if n <= shortstrings.MaxBytes {
		// Every part is a short string, whose value holds its bytes packed.
		var data uint64
		var shift int
		for _, p := range parts {
			data |= math.Float64bits(p.N) << shift
			shift += 8 * len(p.O.Text)
		}
		return Value{
			N: math.Float64frombits(data),
			O: &cx.realm.Shapes.ShortStrings[n],
		}
	}
	var buf [maxStackConcatBytes]byte
	var written int
	if needsUnpacking {
		for _, p := range parts {
			written += copy(buf[written:], p.RawString())
		}
	} else {
		for _, p := range parts {
			written += copy(buf[written:], p.O.Text)
		}
	}
	return cx.concatenated(wtf8.String(buf[:n]), count)
}

// concatReversedStrings is [ExecutionContext.ConcatStrings] for parts one or more of which are reversed
// strings.
func (cx *ExecutionContext) concatReversedStrings(parts []Value) Value {
	for _, p := range parts[:len(parts)-1] {
		if p.O.Flags&objReversed != 0 {
			p.O.Unreverse()
		}
	}
	// Every part before the last one is in order now, so a reversed last part is not also one of
	// them, and they can be prepended to it.
	if end := parts[len(parts)-1].O; end.Flags&objReversed != 0 {
		if rb := storage.HeadBuilder(end); rb != nil {
			if v, ok := cx.prependStrings(parts[:len(parts)-1], end, rb); ok {
				return v
			}
		}
		end.Unreverse()
	}
	return cx.ConcatStrings(parts)
}

// newBuffered returns the concatenation of the strings parts, none of which is reversed, as the
// tail of a new builder if tail is set, and as the head of a new builder otherwise. n is the
// length of the concatenation in bytes.
func (cx *ExecutionContext) newBuffered(parts []Value, n int, tail bool) Value {
	ascii := true
	if tail {
		sb := new(strings.Builder)
		sb.Grow(n)
		for _, p := range parts {
			s := p.RawString()
			sb.WriteString(string(s))
			ascii = ascii && storage.IsASCII(p, s)
		}
		return cx.newTail(sb, ascii)
	}
	rb := new(reversedBuilder)
	rb.Sb.Grow(n)
	for _, p := range slices.Backward(parts) {
		s := p.RawString()
		storage.WriteReversed(&rb.Sb, s)
		ascii = ascii && storage.IsASCII(p, s)
	}
	return cx.newHead(rb, ascii)
}

// prependStrings returns the concatenation of the strings parts, none of which is reversed, and
// the string b, which is the head of rb. It reports false when two of the strings form a
// surrogate pair where they meet.
func (cx *ExecutionContext) prependStrings(parts []Value, b *Object, rb *reversedBuilder) (Value, bool) {
	n := len(b.Text)
	ascii := true
	next, nextReversed, nextASCII := b.Text, true, b.IsASCII()
	for _, part := range slices.Backward(parts) {
		p := part.RawString()
		if p == "" {
			continue
		}
		pASCII := storage.IsASCII(part, p)
		if !pASCII && !nextASCII && storage.JoinsPair(p, false, next, nextReversed) {
			return Value{}, false
		}
		n += len(p)
		if n > MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		ascii = ascii && pASCII
		next, nextReversed, nextASCII = p, false, pASCII
	}
	if n == len(b.Text) {
		return Value{O: b}, true
	}
	for _, part := range slices.Backward(parts) {
		storage.WriteReversed(&rb.Sb, part.RawString())
	}
	return cx.extendHead(b, rb, ascii), true
}

// concatenated returns s, the result of a concatenation without a builder, whose
// [storage.ConcatCount] is count.
func (cx *ExecutionContext) concatenated(s wtf8.String, count concatCount) Value {
	if len(s) <= shortstrings.MaxBytes {
		return cx.NewString(s)
	}
	return Value{
		O: &Object{
			Kind:  kindString,
			Shape: cx.realm.Shapes.String,
			Text:  s,
			Ext:   count,
		},
	}
}

// newTail returns the contents of sb as a string that is the tail of sb.
// ascii reports whether the contents are ASCII.
func (cx *ExecutionContext) newTail(sb *strings.Builder, ascii bool) Value {
	flags := objASCIIKnown
	if ascii {
		flags |= objASCII
	}
	return cx.newBufferedString(wtf8.String(sb.String()), flags, sb, nil)
}

// extendTail returns the contents of sb as the string that becomes its tail in place of the
// string a, after strings were appended to the contents of a. ascii reports whether the
// appended strings are ASCII.
func (cx *ExecutionContext) extendTail(a *Object, sb *strings.Builder, ascii bool) Value {
	a.SetExtState(nil)
	s := wtf8.String(sb.String())
	flags := objASCIIKnown
	var meta *objectMeta
	if ascii && a.IsASCII() {
		flags |= objASCII
	} else if m := a.Meta(); m != nil {
		// The UTF-16 view of a stays valid for its prefix of s. Carrying it over keeps
		// a loop that appends and reads the length or the last code units linear.
		meta = &objectMeta{
			Cursor: m.Cursor,
		}
		if m.Length != 0 {
			meta.Length = m.Length + int32(wtf8.Len(s[len(a.Text):]))
		}
	}
	return cx.newBufferedString(s, flags, sb, meta)
}

// newHead returns the reversed string whose str is the contents of rb, as the head of rb.
// ascii reports whether the contents are ASCII.
func (cx *ExecutionContext) newHead(rb *reversedBuilder, ascii bool) Value {
	flags := objASCIIKnown | objReversed
	if ascii {
		flags |= objASCII
	}
	return cx.newBufferedString(wtf8.String(rb.Sb.String()), flags, rb, nil)
}

// extendHead returns the reversed string whose str is the contents of rb, as the head of rb, after
// strings were prepended to the reversed string b by writing them to rb. ascii reports whether
// the prepended strings are ASCII.
func (cx *ExecutionContext) extendHead(b *Object, rb *reversedBuilder, ascii bool) Value {
	b.SetExtState(nil)
	s := wtf8.String(rb.Sb.String())
	flags := objASCIIKnown | objReversed
	var meta *objectMeta
	if ascii && b.IsASCII() {
		flags |= objASCII
	} else if m := b.Meta(); m != nil && m.Length != 0 {
		// Carrying the length over keeps a loop that prepends and reads the length linear.
		meta = &objectMeta{
			Length: m.Length + int32(wtf8.Len(s[len(b.Text):])),
		}
	}
	return cx.newBufferedString(s, flags, rb, meta)
}

// newBufferedString returns a string with its builder and optional UTF-16 metadata.
func (cx *ExecutionContext) newBufferedString(s wtf8.String, flags uint8, builder any, meta *objectMeta) Value {
	if meta == nil {
		return Value{
			O: &Object{
				Kind:  kindString,
				Flags: flags,
				Shape: cx.realm.Shapes.String,
				Text:  s,
				Ext:   builder,
			},
		}
	}
	// The metadata belongs only to this string, so both have the same lifetime.
	p := &struct {
		object Object
		meta   objectMeta
	}{
		object: Object{
			Kind:  kindString,
			Flags: flags,
			Shape: cx.realm.Shapes.String,
			Text:  s,
		},
		meta: *meta,
	}
	p.meta.Ext = builder
	p.object.Ext = &p.meta
	return Value{O: &p.object}
}
