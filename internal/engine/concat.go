// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"encoding/binary"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// The ext of a string that a concatenation made tells how a concatenation onto it proceeds.
//
// A *strings.Builder whose contents are the string makes the string the tail of its builder: a
// concatenation onto it appends to the builder, and the result becomes the tail in its place.
// The strings made from one builder share its memory, and each keeps its contents, because a
// builder never changes the bytes it holds. Only the tail refers to the builder, so that the
// earlier strings keep no more memory alive than they had when they were made.
//
// A *reversedBuilder whose contents are the str of a reversed string (see [objReversed]) makes
// the string the head of its builder in the same way: a concatenation that prepends to it
// appends the prepended bytes in reverse order to the builder, and the result becomes the head
// in its place. Only a head refers to its builder, so the ext of a reversed string is its
// builder or nil.
//
// A concatCount counts the concatenations in a row that made the string without a builder.

// concatCount counts the concatenations in a row that made a string, each up to [bufferAfter]:
// in its low bits those whose left operand was the result of the one before, and from
// [prependShift] on those whose right operand was.
type concatCount uint8

const (
	// bufferAfter is the number of concatenations in a row after which the next one makes its
	// result the tail or the head of a builder.
	bufferAfter concatCount = 3

	// minBufferedLength is the smallest length in bytes of a string that is the tail or the head
	// of a builder.
	minBufferedLength = 32

	prependShift = 2
)

// Both counts of a concatCount must fit in their bits.
const _ uint = 1<<prependShift - 1 - uint(bufferAfter)

// appends returns the number of concatenations in a row each onto the result of the one before
// as its left operand.
func (c concatCount) appends() concatCount {
	return c & (1<<prependShift - 1)
}

// prepends returns the number of concatenations in a row each onto the result of the one before
// as its right operand.
func (c concatCount) prepends() concatCount {
	return c >> prependShift
}

// reversedBuilder holds the reversed contents of the reversed strings made from it.
type reversedBuilder struct {
	sb strings.Builder
}

// Concat returns the concatenation of the strings a and b.
func (vm *VM) Concat(a, b Value) Value {
	return vm.concat(a, a.o.str, b, b.o.str)
}

// concat returns the concatenation of a and b. x and y are the str of a and b when those are
// strings, and their string forms otherwise.
func (vm *VM) concat(a Value, x string, b Value, y string) Value {
	if x == "" && b.IsString() {
		return b
	}
	if y == "" && a.IsString() {
		return a
	}
	n := len(x) + len(y)
	if n > MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
	if b.IsString() && b.o.flags&objReversed != 0 {
		return vm.concatReversed(a, x, b, y)
	}
	var ca, cb concatCount
	if a.IsString() {
		// A tail is never reversed.
		if sb := tailBuilder(a.o); sb != nil {
			if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
				return vm.NewString(wtf8.Concat(x, y))
			}
			sb.WriteString(y)
			return vm.extendTail(a.o, sb, isASCII(b, y))
		}
		if a.o.flags&objReversed != 0 {
			return vm.concatReversed(a, x, b, y)
		}
		ca, _ = a.o.ext.(concatCount)
	}
	if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
		return vm.NewString(wtf8.Concat(x, y))
	}
	if b.IsString() {
		cb, _ = b.o.ext.(concatCount)
	}
	if n >= minBufferedLength {
		if ca.appends() >= bufferAfter {
			sb := new(strings.Builder)
			sb.Grow(n)
			sb.WriteString(x)
			sb.WriteString(y)
			return vm.newTail(sb, isASCII(a, x) && isASCII(b, y))
		}
		if cb.prepends() >= bufferAfter {
			rb := new(reversedBuilder)
			rb.sb.Grow(n)
			writeReversed(&rb.sb, y)
			writeReversed(&rb.sb, x)
			return vm.newHead(rb, isASCII(a, x) && isASCII(b, y))
		}
	}
	return vm.concatenated(x+y, nextCount(ca, cb))
}

// concatReversed is [VM.concat] for operands one or both of which are reversed strings.
func (vm *VM) concatReversed(a Value, x string, b Value, y string) Value {
	ar := a.IsString() && a.o.flags&objReversed != 0
	br := b.IsString() && b.o.flags&objReversed != 0
	asciiX := isASCII(a, x)
	if !br || !asciiX && !b.o.IsASCII() && joinsPair(x, ar, y, true) {
		// Only a prepend keeps the result reversed.
		if ar {
			x = a.o.Str()
		}
		if br {
			y = b.o.Str()
		}
		return vm.concat(a, x, b, y)
	}
	rb := headBuilder(b.o)
	if rb == nil {
		rb = new(reversedBuilder)
		rb.sb.Grow(len(x) + len(y))
		rb.sb.WriteString(y)
	}
	if ar {
		rb.sb.WriteString(x)
	} else {
		writeReversed(&rb.sb, x)
	}
	return vm.extendHead(b.o, rb, asciiX)
}

// concatStrings returns the concatenation of the strings parts.
func (vm *VM) concatStrings(parts []Value) Value {
	var n, nonEmpty int
	var flags uint8
	last := -1
	for i, p := range parts {
		s := p.o.str
		if s == "" {
			continue
		}
		if last >= 0 && wtf8.JoinsPair(parts[last].o.str, s) {
			acc := parts[0]
			for _, p := range parts[1:] {
				acc = vm.concat(acc, acc.o.str, p, p.o.str)
			}
			return acc
		}
		n += len(s)
		if n > MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
		flags |= p.o.flags
		last = i
		nonEmpty++
	}
	switch nonEmpty {
	case 0:
		return vm.emptyString
	case 1:
		return parts[last]
	}
	if flags&objReversed != 0 {
		return vm.concatReversedStrings(parts)
	}

	first := parts[0].o
	if sb := tailBuilder(first); sb != nil {
		ascii := true
		for _, p := range parts[1:] {
			sb.WriteString(p.o.str)
			ascii = ascii && p.o.IsASCII()
		}
		return vm.extendTail(first, sb, ascii)
	}
	ca, _ := first.ext.(concatCount)
	cb, _ := parts[len(parts)-1].o.ext.(concatCount)
	if n < minBufferedLength || ca.appends() < bufferAfter && cb.prepends() < bufferAfter {
		var sb strings.Builder
		sb.Grow(n)
		for _, p := range parts {
			sb.WriteString(p.o.str)
		}
		return vm.concatenated(sb.String(), nextCount(ca, cb))
	}
	return vm.newBuffered(parts, n, ca.appends() >= bufferAfter)
}

// concatReversedStrings is [VM.concatStrings] for parts one or more of which are reversed
// strings.
func (vm *VM) concatReversedStrings(parts []Value) Value {
	for _, p := range parts[:len(parts)-1] {
		if p.o.flags&objReversed != 0 {
			p.o.unreverse()
		}
	}
	// Every part before the last one is in order now, so a reversed last part is not also one of
	// them, and they can be prepended to it.
	if end := parts[len(parts)-1].o; end.flags&objReversed != 0 {
		if rb := headBuilder(end); rb != nil {
			if v, ok := vm.prependStrings(parts[:len(parts)-1], end, rb); ok {
				return v
			}
		}
		end.unreverse()
	}
	return vm.concatStrings(parts)
}

// newBuffered returns the concatenation of the strings parts, none of which is reversed, as the
// tail of a new builder if tail is set, and as the head of a new builder otherwise. n is the
// length of the concatenation in bytes.
func (vm *VM) newBuffered(parts []Value, n int, tail bool) Value {
	ascii := true
	if tail {
		sb := new(strings.Builder)
		sb.Grow(n)
		for _, p := range parts {
			sb.WriteString(p.o.str)
			ascii = ascii && p.o.IsASCII()
		}
		return vm.newTail(sb, ascii)
	}
	rb := new(reversedBuilder)
	rb.sb.Grow(n)
	for _, p := range slices.Backward(parts) {
		writeReversed(&rb.sb, p.o.str)
		ascii = ascii && p.o.IsASCII()
	}
	return vm.newHead(rb, ascii)
}

// prependStrings returns the concatenation of the strings parts, none of which is reversed, and
// the string b, which is the head of rb. It reports false when two of the strings form a
// surrogate pair where they meet.
func (vm *VM) prependStrings(parts []Value, b *Object, rb *reversedBuilder) (Value, bool) {
	n := len(b.str)
	ascii := true
	next, nextReversed, nextASCII := b.str, true, b.IsASCII()
	for _, part := range slices.Backward(parts) {
		p := part.o
		if p.str == "" {
			continue
		}
		pASCII := p.IsASCII()
		if !pASCII && !nextASCII && joinsPair(p.str, false, next, nextReversed) {
			return Value{}, false
		}
		n += len(p.str)
		if n > MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
		ascii = ascii && pASCII
		next, nextReversed, nextASCII = p.str, false, pASCII
	}
	if n == len(b.str) {
		return Value{o: b}, true
	}
	for _, part := range slices.Backward(parts) {
		writeReversed(&rb.sb, part.o.str)
	}
	return vm.extendHead(b, rb, ascii), true
}

// concatenated returns s, the result of a concatenation without a builder, whose
// [concatCount] is count.
func (vm *VM) concatenated(s string, count concatCount) Value {
	if len(s) <= 1 {
		return vm.NewString(s)
	}
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   s,
			ext:   count,
		},
	}
}

// nextCount returns the [concatCount] of the result of a concatenation without a builder whose
// left and right operands have the counts ca and cb.
func nextCount(ca, cb concatCount) concatCount {
	return min(ca.appends()+1, bufferAfter) | min(cb.prepends()+1, bufferAfter)<<prependShift
}

// tailBuilder returns the builder whose tail the string o is, or nil.
func tailBuilder(o *Object) *strings.Builder {
	if sb, ok := o.ext.(*strings.Builder); ok && sb.Len() == len(o.str) {
		return sb
	}
	return nil
}

// headBuilder returns the builder whose head the string o is, or nil.
func headBuilder(o *Object) *reversedBuilder {
	if rb, ok := o.ext.(*reversedBuilder); ok && rb.sb.Len() == len(o.str) {
		return rb
	}
	return nil
}

// newTail returns the contents of sb as a string that is the tail of sb.
// ascii reports whether the contents are ASCII.
func (vm *VM) newTail(sb *strings.Builder, ascii bool) Value {
	flags := objASCIIKnown
	if ascii {
		flags |= objASCII
	}
	return Value{
		o: &Object{
			kind:  kindString,
			flags: flags,
			shape: vm.shapes.string,
			str:   sb.String(),
			ext:   sb,
		},
	}
}

// extendTail returns the contents of sb as the string that becomes its tail in place of the
// string a, after strings were appended to the contents of a. ascii reports whether the
// appended strings are ASCII.
func (vm *VM) extendTail(a *Object, sb *strings.Builder, ascii bool) Value {
	a.ext = nil
	s := sb.String()
	flags := objASCIIKnown
	var meta *objectMeta
	if ascii && a.IsASCII() {
		flags |= objASCII
	} else if m := a.meta; m != nil {
		// The UTF-16 view of a stays valid for its prefix of s. Carrying it over keeps
		// a loop that appends and reads the length or the last code units linear.
		meta = &objectMeta{
			cursor: m.cursor,
		}
		if m.length != 0 {
			meta.length = m.length + int32(wtf8.Len(s[len(a.str):]))
		}
	}
	return Value{
		o: &Object{
			kind:  kindString,
			flags: flags,
			shape: vm.shapes.string,
			str:   s,
			meta:  meta,
			ext:   sb,
		},
	}
}

// newHead returns the reversed string whose str is the contents of rb, as the head of rb.
// ascii reports whether the contents are ASCII.
func (vm *VM) newHead(rb *reversedBuilder, ascii bool) Value {
	flags := objASCIIKnown | objReversed
	if ascii {
		flags |= objASCII
	}
	return Value{
		o: &Object{
			kind:  kindString,
			flags: flags,
			shape: vm.shapes.string,
			str:   rb.sb.String(),
			ext:   rb,
		},
	}
}

// extendHead returns the reversed string whose str is the contents of rb, as the head of rb, after
// strings were prepended to the reversed string b by writing them to rb. ascii reports whether
// the prepended strings are ASCII.
func (vm *VM) extendHead(b *Object, rb *reversedBuilder, ascii bool) Value {
	b.ext = nil
	s := rb.sb.String()
	flags := objASCIIKnown | objReversed
	var meta *objectMeta
	if ascii && b.IsASCII() {
		flags |= objASCII
	} else if m := b.meta; m != nil && m.length != 0 {
		// Carrying the length over keeps a loop that prepends and reads the length linear.
		meta = &objectMeta{
			length: m.length + int32(wtf8.Len(s[len(b.str):])),
		}
	}
	return Value{
		o: &Object{
			kind:  kindString,
			flags: flags,
			shape: vm.shapes.string,
			str:   s,
			meta:  meta,
			ext:   rb,
		},
	}
}

// unreverse makes the reversed string o hold its contents in order.
func (o *Object) unreverse() {
	var sb strings.Builder
	sb.Grow(len(o.str))
	writeReversed(&sb, o.str)
	o.str = sb.String()
	o.flags &^= objReversed
	o.ext = nil
}

// sameStrings reports whether the strings a and b have the same contents.
func sameStrings(a, b *Object) bool {
	// A reversed string has as many bytes as its contents.
	if len(a.str) != len(b.str) {
		return false
	}
	if a.flags&b.flags&objReversed != 0 {
		return a.str == b.str
	}
	return a.Str() == b.Str()
}

// joinsPair reports whether the contents of two strings form a surrogate pair where they meet,
// so that their bytes do not simply concatenate. x and y are the str of the strings, and xr
// and yr report whether they are reversed.
func joinsPair(x string, xr bool, y string, yr bool) bool {
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
	return wtf8.JoinsPair(string(end[:]), string(start[:]))
}

// writeReversed appends the bytes of s to sb in reverse order.
func writeReversed(sb *strings.Builder, s string) {
	var buf [256]byte
	for len(s) > 0 {
		n := min(len(s), len(buf))
		reverseBytes(buf[:n], s[len(s)-n:])
		sb.Write(buf[:n])
		s = s[:len(s)-n]
	}
}

// reverseBytes sets dst to the bytes of src in reverse order. dst and src have the same length.
func reverseBytes(dst []byte, src string) {
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

// isASCII reports whether s, which is the str of v when v is a string, consists of ASCII
// characters only.
func isASCII(v Value, s string) bool {
	if v.IsString() {
		return v.o.IsASCII()
	}
	return wtf8.IsASCII(s)
}
