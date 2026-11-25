// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the quiet operations on the elements of arrays.

// ArraySpeciesCreatePlain returns a new array of the length n with the prototype
// %Array.prototype% when that is what ArraySpeciesCreate for o creates, because o is an array
// with the original constructor of the current realm. It reports false otherwise.
func (q QuietContext) ArraySpeciesCreatePlain(o *Object, n int64) (*Object, bool) {
	r := q.realm
	if o.Kind != kindArray || n > MaxArrayIndex+1 || !r.ArrayCtorPlain(o) {
		return nil, false
	}
	return r.ArrayCreate(uint32(n), r.Intrinsic(IntrinsicArrayPrototype)), true
}

// AppendElements adds vals at the end of the array o and returns the new length.
func (q QuietContext) AppendElements(o *Object, vals []Value) (uint32, bool) {
	return q.realm.AppendElements(o, vals)
}

// PrependElements adds vals at the start of the array o and returns the new length.
func (q QuietContext) PrependElements(o *Object, vals []Value) (uint32, bool) {
	return q.realm.PrependElements(o, vals)
}

// FillElements stores v as the elements of the array o from start up to end, as
// Array.prototype.fill does when the length of o is n. It reports false when it did nothing.
func (q QuietContext) FillElements(o *Object, n, start, end int64, v Value) bool {
	return q.realm.FillElements(o, n, start, end, v)
}

// SearchArray returns the index of the first element of the Array v that equals target, or -1,
// comparing with SameValueZero when sameValueZero is set and with IsStrictlyEqual otherwise. It
// reports false when v is not an Array or reading one of its elements could run JavaScript.
func (q QuietContext) SearchArray(v, target Value, sameValueZero bool) (int64, bool) {
	o := v.O
	if o == nil || o.Kind != kindArray {
		return -1, false
	}
	n := int64(o.Length)
	found, next := q.SearchElements(o, 0, n, target, sameValueZero)
	if found < 0 && next < n {
		return -1, false
	}
	return found, true
}

// SearchElements returns the first index of the elements of o from i toward end, which is
// excluded and may be below i, whose element equals target, comparing with SameValueZero when
// sameValueZero is set and with IsStrictlyEqual otherwise. It looks only at a run of elements
// that reading runs no JavaScript for, and returns -1 and the index after the run when the run
// has no such element.
func (q QuietContext) SearchElements(o *Object, i, end int64, target Value, sameValueZero bool) (found, next int64) {
	return q.realm.SearchElements(o, i, end, target, sameValueZero)
}

// JoinStrings joins an array of the given length with sep when its elements and sep are
// ASCII strings and the result fits in MaxStringLength bytes. It reports false otherwise.
func (q QuietContext) JoinStrings(o *Object, length int64, sep wtf8.String) (Value, bool) {
	return q.realm.JoinStrings(o, length, sep)
}

// ReverseElements reverses the order of the elements of the array o.
func (q QuietContext) ReverseElements(o *Object) bool {
	return q.realm.ReverseElements(o)
}

// CopyElementRange returns a new array with the elements of the array o from start up to end,
// as Array.prototype.slice creates it. A range that ends before it starts is empty.
func (q QuietContext) CopyElementRange(o *Object, start, end int64) (*Object, bool) {
	return q.realm.CopyElementRange(o, start, end)
}

// SpliceElements replaces del elements of the array o from start on with items, and returns
// the removed elements as a new array, as Array.prototype.splice creates it.
// length is the length of o that start and del were checked against.
func (q QuietContext) SpliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	return q.realm.SpliceElements(o, length, start, del, items)
}

// AppendElementsOf adds the elements of the array src at the end of the array dst,
// which must have the length n, exactly n elements and no other property.
func (q QuietContext) AppendElementsOf(dst *Object, n int64, src *Object) bool {
	return q.realm.AppendElementsOf(dst, n, src)
}

// SkipHoles returns the greatest index j with i <= j <= end such that neither o nor its prototypes
// have a property with an integer index from i to j-1. It returns i when that is not known
// without running JavaScript.
func (q QuietContext) SkipHoles(o *Object, i, end int64) int64 {
	return q.realm.SkipHoles(o, i, end)
}

// SkipHolesBackward returns the least index j with end <= j <= i such that neither o nor its
// prototypes have a property with an integer index from j+1 to i. It returns i when that is not
// known without running JavaScript.
func (q QuietContext) SkipHolesBackward(o *Object, i, end int64) int64 {
	return q.realm.SkipHolesBackward(o, i, end)
}
