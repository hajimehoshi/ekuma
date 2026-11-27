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

// The builder of a string that a concatenation made (see [storage.TailBuilder] and
// [storage.HeadBuilder]), or its concatenation count, tells how a concatenation onto it proceeds.
//
// A *strings.Builder whose contents are the string makes the string the tail of its builder: a
// concatenation onto it appends to the builder, and the result becomes the tail in its place.
// The strings made from one builder share its memory, and each keeps its contents, because a
// builder never changes the bytes it holds. Only the tail refers to the builder, so that the
// earlier strings keep no more memory alive than they had when they were made.
//
// A *storage.ReversedBuilder whose contents are the str of a reversed string (see
// [storage.ObjReversed]) makes the string the head of its builder in the same way: a concatenation
// that prepends to it appends the prepended bytes in reverse order to the builder, and the result
// becomes the head in its place. Only a head refers to its builder.
//
// A storage.ConcatCount counts the concatenations in a row that made the string without a builder.

// Both counts of a storage.ConcatCount must fit in their bits.
const _ uint = 1<<storage.PrependShift - 1 - uint(storage.BufferAfter)

// Concat returns the concatenation of the strings a and b.
func (cx *ExecutionContext) Concat(a, b storage.Value) storage.Value {
	if sb := storage.TailBuilder(a.O); sb != nil {
		// An ASCII suffix cannot join a surrogate pair.
		if b.O.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII|storage.ObjReversed) == storage.ObjASCIIKnown|storage.ObjASCII {
			y := b.O.Text()
			if y == "" {
				return a
			}
			if len(y) > storage.MaxStringLength-len(a.O.Text()) {
				cx.ThrowRangeError("invalid string length")
			}
			sb.WriteString(string(y))
			return cx.extendTail(a.O, sb, true)
		}
		// A string of at most shortstrings.MaxBytes bytes holds them in b.N, and its object need not
		// know that they are ASCII.
		if data := math.Float64bits(b.N); shortstrings.IsASCII(data) {
			if n := b.StringByteLength(); n <= shortstrings.MaxBytes {
				if n == 0 {
					return a
				}
				if n > storage.MaxStringLength-len(a.O.Text()) {
					cx.ThrowRangeError("invalid string length")
					// ThrowRangeError does not return. Returning keeps b.N out of what
					// Concat saves on entry.
					return storage.Undefined
				}
				bytes := shortstrings.Bytes(data)
				sb.Write(bytes[:n])
				return cx.extendTail(a.O, sb, true)
			}
		}
	}
	if b.O.Flags()&storage.ObjReversed != 0 && a.O.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII|storage.ObjReversed) == storage.ObjASCIIKnown|storage.ObjASCII {
		if rb := storage.HeadBuilder(b.O); rb != nil {
			x := a.O.Text()
			if x == "" {
				return b
			}
			if len(x) > storage.MaxStringLength-len(b.O.Text()) {
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

func (cx *ExecutionContext) concatShort(a storage.Value, aN int, b storage.Value, bN int) (storage.Value, bool) {
	x, y := math.Float64bits(a.N), math.Float64bits(b.N)
	data, n, ok := shortstrings.Concat(x, aN, y, bN)
	if !ok {
		return storage.Value{}, false
	}
	return storage.Value{
		N: math.Float64frombits(data),
		O: &cx.realm.Shapes.ShortStrings[n],
	}, true
}

// concat returns the concatenation of a and b. x and y are the str of a and b when those are
// strings, and their string forms otherwise.
func (cx *ExecutionContext) concat(a storage.Value, x wtf8.String, b storage.Value, y wtf8.String) storage.Value {
	if x == "" && b.IsString() {
		return b
	}
	if y == "" && a.IsString() {
		return a
	}
	n := len(x) + len(y)
	if n > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	if b.IsString() && b.O.Flags()&storage.ObjReversed != 0 {
		return cx.concatReversed(a, x, b, y)
	}
	var ca, cb storage.ConcatCount
	if a.IsString() {
		// A tail is never reversed.
		if sb := storage.TailBuilder(a.O); sb != nil {
			if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
				return cx.NewString(wtf8.Concat(x, y))
			}
			sb.WriteString(string(y))
			return cx.extendTail(a.O, sb, storage.IsASCII(b, y))
		}
		if a.O.Flags()&storage.ObjReversed != 0 {
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
	if n >= storage.MinBufferedLength {
		if ca.Appends() >= storage.BufferAfter {
			sb := new(strings.Builder)
			sb.Grow(n)
			sb.WriteString(string(x))
			sb.WriteString(string(y))
			return cx.newTail(sb, storage.IsASCII(a, x) && storage.IsASCII(b, y))
		}
		if cb.Prepends() >= storage.BufferAfter {
			rb := new(storage.ReversedBuilder)
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
	if n <= storage.MaxStackConcatBytes {
		var buf [storage.MaxStackConcatBytes]byte
		copy(buf[copy(buf[:], x):], y)
		return cx.memoizedConcat(&buf, n, storage.NextCount(ca, cb))
	}
	return cx.concatenated(x+y, storage.NextCount(ca, cb))
}

// concatReversed is [ExecutionContext.concat] for operands one or both of which are reversed strings.
func (cx *ExecutionContext) concatReversed(a storage.Value, x wtf8.String, b storage.Value, y wtf8.String) storage.Value {
	ar := a.IsString() && a.O.Flags()&storage.ObjReversed != 0
	br := b.IsString() && b.O.Flags()&storage.ObjReversed != 0
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
		rb = new(storage.ReversedBuilder)
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
func (cx *ExecutionContext) ConcatStrings(parts []storage.Value) storage.Value {
	var n, nonEmpty, previousBytes int
	var needsUnpacking bool
	var flags uint8
	last := -1
	for i, p := range parts {
		nbytes := len(p.O.Text())
		needsUnpacking = needsUnpacking || nbytes <= shortstrings.MaxBytes && p.O.Flags()&storage.ObjInterned == 0
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
		if n > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		flags |= p.O.Flags()
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
	if flags&storage.ObjReversed != 0 {
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
	if n < storage.MinBufferedLength || ca.Appends() < storage.BufferAfter && cb.Prepends() < storage.BufferAfter {
		if n <= storage.MaxStackConcatBytes {
			return cx.concatSmall(parts, n, storage.NextCount(ca, cb), needsUnpacking)
		}
		var sb strings.Builder
		sb.Grow(n)
		if needsUnpacking {
			for _, p := range parts {
				if p.IsShortString() {
					p.WriteShortString(&sb)
				} else {
					sb.WriteString(string(p.O.Text()))
				}
			}
		} else {
			for _, p := range parts {
				sb.WriteString(string(p.O.Text()))
			}
		}
		return cx.concatenated(wtf8.String(sb.String()), storage.NextCount(ca, cb))
	}
	return cx.newBuffered(parts, n, ca.Appends() >= storage.BufferAfter)
}

// concatSmall joins parts with total byte length n, at most storage.MaxStackConcatBytes.
// No part is reversed and no boundary forms a surrogate pair.
func (cx *ExecutionContext) concatSmall(parts []storage.Value, n int, count storage.ConcatCount, needsUnpacking bool) storage.Value {
	if n <= shortstrings.MaxBytes {
		// Every part is a short string, whose value holds its bytes packed.
		var data uint64
		var shift int
		for _, p := range parts {
			data |= math.Float64bits(p.N) << shift
			shift += 8 * len(p.O.Text())
		}
		return storage.Value{
			N: math.Float64frombits(data),
			O: &cx.realm.Shapes.ShortStrings[n],
		}
	}
	var buf [storage.MaxStackConcatBytes]byte
	var written int
	if needsUnpacking {
		for _, p := range parts {
			written += copy(buf[written:], p.RawString())
		}
	} else {
		for _, p := range parts {
			written += copy(buf[written:], p.O.Text())
		}
	}
	return cx.memoizedConcat(&buf, n, count)
}

// concatReversedStrings is [ExecutionContext.ConcatStrings] for parts one or more of which are reversed
// strings.
func (cx *ExecutionContext) concatReversedStrings(parts []storage.Value) storage.Value {
	for _, p := range parts[:len(parts)-1] {
		if p.O.Flags()&storage.ObjReversed != 0 {
			p.O.Unreverse()
		}
	}
	// Every part before the last one is in order now, so a reversed last part is not also one of
	// them, and they can be prepended to it.
	if end := parts[len(parts)-1].O; end.Flags()&storage.ObjReversed != 0 {
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
func (cx *ExecutionContext) newBuffered(parts []storage.Value, n int, tail bool) storage.Value {
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
	rb := new(storage.ReversedBuilder)
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
func (cx *ExecutionContext) prependStrings(parts []storage.Value, b *storage.Object, rb *storage.ReversedBuilder) (storage.Value, bool) {
	n := len(b.Text())
	ascii := true
	next, nextReversed, nextASCII := b.Text(), true, b.IsASCII()
	for _, part := range slices.Backward(parts) {
		p := part.RawString()
		if p == "" {
			continue
		}
		pASCII := storage.IsASCII(part, p)
		if !pASCII && !nextASCII && storage.JoinsPair(p, false, next, nextReversed) {
			return storage.Value{}, false
		}
		n += len(p)
		if n > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
		ascii = ascii && pASCII
		next, nextReversed, nextASCII = p, false, pASCII
	}
	if n == len(b.Text()) {
		return storage.Value{O: b}, true
	}
	for _, part := range slices.Backward(parts) {
		storage.WriteReversed(&rb.Sb, part.RawString())
	}
	return cx.extendHead(b, rb, ascii), true
}

// concatenated returns s, the result of a concatenation without a builder, whose
// [storage.ConcatCount] is count.
func (cx *ExecutionContext) concatenated(s wtf8.String, count storage.ConcatCount) storage.Value {
	if len(s) <= shortstrings.MaxBytes {
		return cx.NewString(s)
	}
	return cx.NewConcatenatedString(s, count)
}

// newTail returns the contents of sb as a string that is the tail of sb.
// ascii reports whether the contents are ASCII.
func (cx *ExecutionContext) newTail(sb *strings.Builder, ascii bool) storage.Value {
	flags := storage.ObjASCIIKnown
	if ascii {
		flags |= storage.ObjASCII
	}
	return cx.NewBufferedString(wtf8.String(sb.String()), flags, sb, nil)
}

// extendTail returns the contents of sb as the string that becomes its tail in place of the
// string a, after strings were appended to the contents of a. ascii reports whether the
// appended strings are ASCII.
func (cx *ExecutionContext) extendTail(a *storage.Object, sb *strings.Builder, ascii bool) storage.Value {
	a.ClearBuilder()
	s := wtf8.String(sb.String())
	flags := storage.ObjASCIIKnown
	var meta *storage.ObjectMeta
	if ascii && a.IsASCII() {
		flags |= storage.ObjASCII
	} else if m := a.Meta(); m != nil {
		// The UTF-16 view of a stays valid for its prefix of s. Carrying it over keeps
		// a loop that appends and reads the length or the last code units linear.
		meta = &storage.ObjectMeta{
			Cursor: m.Cursor,
		}
		if m.Length != 0 {
			meta.Length = m.Length + int32(wtf8.Len(s[len(a.Text()):]))
		}
	}
	return cx.NewBufferedString(s, flags, sb, meta)
}

// newHead returns the reversed string whose str is the contents of rb, as the head of rb.
// ascii reports whether the contents are ASCII.
func (cx *ExecutionContext) newHead(rb *storage.ReversedBuilder, ascii bool) storage.Value {
	flags := storage.ObjASCIIKnown | storage.ObjReversed
	if ascii {
		flags |= storage.ObjASCII
	}
	return cx.NewBufferedString(wtf8.String(rb.Sb.String()), flags, rb, nil)
}

// extendHead returns the reversed string whose str is the contents of rb, as the head of rb, after
// strings were prepended to the reversed string b by writing them to rb. ascii reports whether
// the prepended strings are ASCII.
func (cx *ExecutionContext) extendHead(b *storage.Object, rb *storage.ReversedBuilder, ascii bool) storage.Value {
	b.ClearBuilder()
	s := wtf8.String(rb.Sb.String())
	flags := storage.ObjASCIIKnown | storage.ObjReversed
	var meta *storage.ObjectMeta
	if ascii && b.IsASCII() {
		flags |= storage.ObjASCII
	} else if m := b.Meta(); m != nil && m.Length != 0 {
		// Carrying the length over keeps a loop that prepends and reads the length linear.
		meta = &storage.ObjectMeta{
			Length: m.Length + int32(wtf8.Len(s[len(b.Text()):])),
		}
	}
	return cx.NewBufferedString(s, flags, rb, meta)
}

// Append returns the concatenation of the strings a and b for an append to a variable that
// holds a and receives the result. A string marked [storage.ObjExclusive] grows in place, and a
// result that is a new tail is marked so.
func (cx *ExecutionContext) Append(a, b storage.Value) storage.Value {
	if a.O.Flags()&storage.ObjExclusive != 0 {
		if sb := storage.TailBuilder(a.O); sb != nil && cx.appendInPlace(a, b, sb) {
			return a
		}
	}
	v := cx.Concat(a, b)
	if v.O != a.O && v.O != b.O && storage.TailBuilder(v.O) != nil {
		v.O.MarkExclusive()
	}
	return v
}

// appendInPlace appends the string b to the string a, which is marked [storage.ObjExclusive]
// and is the tail of sb, and reports whether it did. It does not when b is reversed or when the
// strings form a surrogate pair where they meet.
func (cx *ExecutionContext) appendInPlace(a, b storage.Value, sb *strings.Builder) bool {
	x := a.O.Text()
	// An ASCII suffix cannot join a surrogate pair.
	if b.O.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII|storage.ObjReversed) == storage.ObjASCIIKnown|storage.ObjASCII {
		y := b.O.Text()
		if len(y) > storage.MaxStringLength-len(x) {
			cx.ThrowRangeError("invalid string length")
			return false
		}
		sb.WriteString(string(y))
		a.O.ExtendExclusive(sb, true)
		return true
	}
	// A string of at most shortstrings.MaxBytes bytes holds them in b.N.
	if data := math.Float64bits(b.N); shortstrings.IsASCII(data) {
		if n := b.StringByteLength(); n <= shortstrings.MaxBytes {
			if n > storage.MaxStringLength-len(x) {
				cx.ThrowRangeError("invalid string length")
				return false
			}
			bytes := shortstrings.Bytes(data)
			sb.Write(bytes[:n])
			a.O.ExtendExclusive(sb, true)
			return true
		}
	}
	if b.O.Flags()&storage.ObjReversed != 0 {
		return false
	}
	y := b.RawString()
	if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
		return false
	}
	if len(y) > storage.MaxStringLength-len(x) {
		cx.ThrowRangeError("invalid string length")
		return false
	}
	sb.WriteString(string(y))
	a.O.ExtendExclusive(sb, storage.IsASCII(b, y))
	return true
}
