// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
)

// Truncated returns the first n values of s, and clears the others.
func Truncated[T any](s []T, n int) []T {
	clear(s[n:])
	return s[:n]
}

// WithoutLast returns s without its last value, which it clears.
func WithoutLast[T any](s []T) []T {
	n := len(s) - 1
	var zero T
	s[n] = zero
	return s[:n]
}

// InitSlots makes storage the slots of an object that has none.
func (p *ObjectProperties) InitSlots(storage []Value) {
	if ValidateExec && len(p.Slots) != 0 {
		panic("engine: initSlots on an object with slots")
	}
	p.Slots = storage
}

// fillSlots copies vals to the slots of an object that has none.
func (p *ObjectProperties) fillSlots(vals []Value) {
	if ValidateExec && len(p.Slots) != 0 {
		panic("engine: fillSlots on an object with slots")
	}
	p.Slots = append(p.Slots[:0], vals...)
}

// AppendSlot appends v to the slots of o.
func (o *Object) AppendSlot(v Value) {
	old := o.Slots
	o.Slots = append(old, v)
	if cap(o.Slots) != cap(old) {
		o.leaveSlots(old)
	}
}

// ReserveSlots gives the slots of o room for n more values.
func (o *Object) ReserveSlots(n int) {
	if old := o.Slots; cap(old)-len(old) < n {
		o.Slots = slices.Grow(old, n)
		o.leaveSlots(old)
	}
}

// leaveSlots clears old, storage that the slots of o have moved out of, when it stays reachable.
func (o *Object) leaveSlots(old []Value) {
	// Inline caches point into the slots of global objects and of prototypes.
	if o.Pinned&(PinnedSlots|PinnedGlobalSlots) != 0 || o.Flags&ObjIsProto != 0 {
		clear(old)
	}
	o.Pinned &^= PinnedSlots
}

// leaveElems clears old, storage that the elements of o have moved out of, when it stays
// reachable.
func (o *Object) leaveElems(old []Value) {
	if o.Pinned&PinnedElems != 0 {
		clear(old)
		o.Pinned &^= PinnedElems
	}
}

// ReplaceSlots makes slots, which holds the values that remain, the slots of o.
func (o *Object) ReplaceSlots(slots []Value) {
	o.leaveSlots(o.Slots)
	o.Slots = slots
}

// detachSlots moves the first n slots of o to new storage with room for room values, and returns
// the old storage with all the slots. The caller must call leaveSlots with it after reading the
// others.
func (o *Object) detachSlots(n, room int) []Value {
	old := o.Slots
	o.Slots = append(make([]Value, 0, room), old[:n]...)
	return old
}

// truncateSlots removes the slots beyond the first n.
func (o *Object) truncateSlots(n int) {
	o.Slots = Truncated(o.Slots, n)
}

// RemoveSlot removes the slot i, moving the later ones down.
func (o *Object) RemoveSlot(i int) {
	n := len(o.Slots) - 1
	copy(o.Slots[i:], o.Slots[i+1:])
	o.Slots[n] = Value{}
	o.Slots = o.Slots[:n]
}

// InitElems makes storage the elements of an object that has none.
func (p *ObjectProperties) InitElems(storage []Value) {
	if ValidateExec && len(p.Elems) != 0 {
		panic("engine: initElems on an object with elements")
	}
	p.Elems = storage
}

// ReserveElems gives the elements of o room for n more elements.
func (o *Object) ReserveElems(n int) {
	if cap(o.Elems)-len(o.Elems) < n {
		o.moveElems(n)
	}
}

// moveElems moves the elements of o to new storage with room for n more elements.
// It stays out of line so that ReserveElems can be inlined.
//
//go:noinline
func (o *Object) moveElems(n int) {
	elems := o.Elems
	if cap(elems) == 0 {
		o.Elems = make([]Value, 0, max(n, MinElems))
		return
	}
	o.Elems = slices.Grow(elems, n)
	o.leaveElems(elems)
}

// AppendElem appends v to the elements of o.
func (o *Object) AppendElem(v Value) {
	elems := o.Elems
	if cap(elems) == 0 {
		elems = make([]Value, 0, MinElems)
	}
	o.Elems = append(elems, v)
	if cap(o.Elems) != cap(elems) {
		o.leaveElems(elems)
	}
}

// ElemsStorage returns the number of values that the storage of the elements of o has room
// for, counting those before the elements that removing the first elements left unused.
func (o *Object) ElemsStorage() int {
	n := cap(o.Elems)
	if m := o.Meta(); m != nil && m.shiftedCap == n {
		n += m.shiftedElems
	}
	return n
}

// ElemsOversized reports whether the elements of o use less than a quarter of their storage
// and the storage is larger than minShrinkElems.
func (o *Object) ElemsOversized() bool {
	n := o.ElemsStorage()
	return n > minShrinkElems && len(o.Elems) < n/4
}

// ReallocElems moves the elements of o to new storage with room for as many elements again.
func (p *Poller) ReallocElems(o *Object) {
	elems := make([]Value, len(o.Elems), max(2*len(o.Elems), MinElems))
	CopyPolled(p, elems, o.Elems)
	o.replaceElems(elems)
}

// MoveElemsTo moves the elements of o to new storage with room for room elements in all.
func (o *Object) MoveElemsTo(room int) {
	elems := make([]Value, len(o.Elems), room)
	copy(elems, o.Elems)
	o.replaceElems(elems)
}

// replaceElems makes elems, which holds the values that remain, the elements of o.
func (o *Object) replaceElems(elems []Value) {
	o.leaveElems(o.Elems)
	o.Elems = elems
	if m := o.Meta(); m != nil {
		m.shiftedElems, m.shiftedCap = 0, 0
	}
}

// shiftElem removes the first element without moving the others. The storage keeps its size: see
// ElemsOversized.
func (o *Object) shiftElem() {
	m, elems := o.ensureMeta(), o.Elems
	if m.shiftedCap != cap(elems) {
		m.shiftedElems = 0
	}
	m.shiftedElems++
	m.shiftedCap = cap(elems) - 1
	elems[0] = Value{}
	o.Elems = elems[1:]
}

// ClipElems keeps the storage after the elements out of reach of later growth.
func (o *Object) ClipElems() {
	old := cap(o.Elems)
	o.Elems = slices.Clip(o.Elems)
	m := o.Meta()
	switch {
	case m == nil:
	case m.shiftedCap == old:
		m.shiftedCap = cap(o.Elems)
	default:
		m.shiftedElems, m.shiftedCap = 0, 0
	}
}

// pushElem appends v to the elements, which must have room for it.
func (p *ObjectProperties) pushElem(v Value) {
	n := len(p.Elems)
	p.Elems = p.Elems[:n+1]
	p.Elems[n] = v
}

// removeLastElem removes the last element. The storage keeps its size: see ElemsOversized.
func (p *ObjectProperties) removeLastElem() {
	n := len(p.Elems) - 1
	p.Elems[n] = Value{}
	p.Elems = p.Elems[:n]
}
