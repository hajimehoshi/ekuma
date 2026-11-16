// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
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
// A concatCount is the number of concatenations in a row, each onto the result of the one
// before, that made the string without a builder.

// concatCount counts the concatenations in a row that made a string, up to [bufferAfter].
type concatCount uint8

const (
	// bufferAfter is the number of concatenations in a row after which the next one makes its
	// result the tail of a builder.
	bufferAfter concatCount = 3

	// minBufferedLength is the smallest length in bytes of a string that is the tail of a builder.
	minBufferedLength = 32
)

// concat returns the concatenation of the strings x and y, which are the contents of a and b
// when those are strings.
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
	if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
		return vm.NewString(wtf8.Concat(x, y))
	}
	var count concatCount
	if a.IsString() {
		if sb := tailBuilder(a.o); sb != nil {
			sb.WriteString(y)
			return vm.extendTail(a.o, sb, isASCII(b, y))
		}
		count, _ = a.o.ext.(concatCount)
	}
	if n < minBufferedLength || count < bufferAfter {
		return vm.concatenated(x+y, count)
	}
	sb := new(strings.Builder)
	sb.Grow(n)
	sb.WriteString(x)
	sb.WriteString(y)
	return vm.newTail(sb, isASCII(a, x) && isASCII(b, y))
}

// concatStrings returns the concatenation of the strings parts.
func (vm *VM) concatStrings(parts []Value) Value {
	var n, nonEmpty int
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
		last = i
		nonEmpty++
	}
	switch nonEmpty {
	case 0:
		return vm.emptyString
	case 1:
		return parts[last]
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
	count, _ := first.ext.(concatCount)
	if n < minBufferedLength || count < bufferAfter {
		var sb strings.Builder
		sb.Grow(n)
		for _, p := range parts {
			sb.WriteString(p.o.str)
		}
		return vm.concatenated(sb.String(), count)
	}
	sb := new(strings.Builder)
	sb.Grow(n)
	ascii := true
	for _, p := range parts {
		sb.WriteString(p.o.str)
		ascii = ascii && p.o.IsASCII()
	}
	return vm.newTail(sb, ascii)
}

// concatenated returns s, the result of a concatenation without a builder. count is the
// [concatCount] of the string that the concatenation was onto.
func (vm *VM) concatenated(s string, count concatCount) Value {
	if len(s) <= 1 {
		return vm.NewString(s)
	}
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   s,
			ext:   min(count+1, bufferAfter),
		},
	}
}

// tailBuilder returns the builder whose tail the string o is, or nil.
func tailBuilder(o *Object) *strings.Builder {
	if sb, ok := o.ext.(*strings.Builder); ok && sb.Len() == len(o.str) {
		return sb
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
			meta.length = m.length + wtf8.Len(s[len(a.str):])
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

// isASCII reports whether s, which is the contents of v when v is a string, consists of
// ASCII characters only.
func isASCII(v Value, s string) bool {
	if v.IsString() {
		return v.o.IsASCII()
	}
	return wtf8.IsASCII(s)
}
