// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "slices"

// This file owns the storage of the slots and the elements of objects. Only the operations here
// may make it shorter, move it or replace it, so that the values that the storage gives up do not
// stay reachable. TestValueStorageOwnership lists the exceptions.

// truncated returns the first n values of s, and clears the others.
func truncated[T any](s []T, n int) []T {
	clear(s[n:])
	return s[:n]
}

// withoutLast returns s without its last value, which it clears.
func withoutLast[T any](s []T) []T {
	n := len(s) - 1
	var zero T
	s[n] = zero
	return s[:n]
}

// initSlots makes storage the slots of an object that has none.
func (p *objectProperties) initSlots(storage []Value) {
	if validateExec && len(p.slots) != 0 {
		panic("engine: initSlots on an object with slots")
	}
	p.slots = storage
}

// fillSlots copies vals to the slots of an object that has none.
func (p *objectProperties) fillSlots(vals []Value) {
	if validateExec && len(p.slots) != 0 {
		panic("engine: fillSlots on an object with slots")
	}
	p.slots = append(p.slots[:0], vals...)
}

// appendSlot appends v to the slots of o.
func (o *Object) appendSlot(v Value) {
	old := o.slots
	o.slots = append(old, v)
	if cap(o.slots) != cap(old) {
		o.leaveSlots(old)
	}
}

// reserveSlots gives the slots of o room for n more values.
func (o *Object) reserveSlots(n int) {
	if old := o.slots; cap(old)-len(old) < n {
		o.slots = slices.Grow(old, n)
		o.leaveSlots(old)
	}
}

// leaveSlots clears old, storage that the slots of o have moved out of, when it stays reachable.
func (o *Object) leaveSlots(old []Value) {
	// Inline caches point into the slots of global objects and of prototypes.
	if o.pinned&(pinnedSlots|pinnedGlobalSlots) != 0 || o.flags&objIsProto != 0 {
		clear(old)
	}
	o.pinned &^= pinnedSlots
}

// leaveElems clears old, storage that the elements of o have moved out of, when it stays
// reachable.
func (o *Object) leaveElems(old []Value) {
	if o.pinned&pinnedElems != 0 {
		clear(old)
		o.pinned &^= pinnedElems
	}
}

// replaceSlots makes slots, which holds the values that remain, the slots of o.
func (o *Object) replaceSlots(slots []Value) {
	o.leaveSlots(o.slots)
	o.slots = slots
}

// detachSlots moves the first n slots of o to new storage with room for room values, and returns
// the old storage with all the slots. The caller must call leaveSlots with it after reading the
// others.
func (o *Object) detachSlots(n, room int) []Value {
	old := o.slots
	o.slots = append(make([]Value, 0, room), old[:n]...)
	return old
}

// truncateSlots removes the slots beyond the first n.
func (o *Object) truncateSlots(n int) {
	o.slots = truncated(o.slots, n)
}

// removeSlot removes the slot i, moving the later ones down.
func (o *Object) removeSlot(i int) {
	n := len(o.slots) - 1
	copy(o.slots[i:], o.slots[i+1:])
	o.slots[n] = Value{}
	o.slots = o.slots[:n]
}

// initElems makes storage the elements of an object that has none.
func (p *objectProperties) initElems(storage []Value) {
	if validateExec && len(p.elems) != 0 {
		panic("engine: initElems on an object with elements")
	}
	p.elems = storage
}

// reserveElems gives the elements of o room for n more elements.
func (o *Object) reserveElems(n int) {
	if cap(o.elems)-len(o.elems) < n {
		o.moveElems(n)
	}
}

// moveElems moves the elements of o to new storage with room for n more elements.
// It stays out of line so that reserveElems can be inlined.
//
//go:noinline
func (o *Object) moveElems(n int) {
	elems := o.elems
	if cap(elems) == 0 {
		o.elems = make([]Value, 0, max(n, minElems))
		return
	}
	o.elems = slices.Grow(elems, n)
	o.leaveElems(elems)
}

// appendElem appends v to the elements of o.
func (o *Object) appendElem(v Value) {
	elems := o.elems
	if cap(elems) == 0 {
		elems = make([]Value, 0, minElems)
	}
	o.elems = append(elems, v)
	if cap(o.elems) != cap(elems) {
		o.leaveElems(elems)
	}
}

// elemsStorage returns the number of values that the storage of the elements of o has room
// for, counting those before the elements that removing the first elements left unused.
func (o *Object) elemsStorage() int {
	n := cap(o.elems)
	if m := o.meta(); m != nil && m.shiftedCap == n {
		n += m.shiftedElems
	}
	return n
}

// elemsOversized reports whether the elements of o use less than a quarter of their storage
// and the storage is larger than minShrinkElems.
func (o *Object) elemsOversized() bool {
	n := o.elemsStorage()
	return n > minShrinkElems && len(o.elems) < n/4
}

// reallocElems moves the elements of o to new storage with room for as many elements again.
func (cx *ExecutionContext) reallocElems(o *Object) {
	elems := make([]Value, len(o.elems), max(2*len(o.elems), minElems))
	CopyPolled(cx, elems, o.elems)
	o.replaceElems(elems)
}

// replaceElems makes elems, which holds the values that remain, the elements of o.
func (o *Object) replaceElems(elems []Value) {
	o.leaveElems(o.elems)
	o.elems = elems
}

// pushElem appends v to the elements, which must have room for it.
func (p *objectProperties) pushElem(v Value) {
	n := len(p.elems)
	p.elems = p.elems[:n+1]
	p.elems[n] = v
}

// removeLastElem removes the last element. The storage keeps its size: see elemsOversized.
func (p *objectProperties) removeLastElem() {
	n := len(p.elems) - 1
	p.elems[n] = Value{}
	p.elems = p.elems[:n]
}
