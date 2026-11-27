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
func (o *Object) InitSlots(storage []Value) {
	if ValidateExec && len(o.slots) != 0 {
		panic("storage: InitSlots on an object with slots")
	}
	o.slots = storage
}

// fillSlots copies vals to the slots of an object that has none.
func (o *Object) fillSlots(vals []Value) {
	if ValidateExec && len(o.slots) != 0 {
		panic("storage: fillSlots on an object with slots")
	}
	o.slots = append(o.slots[:0], vals...)
}

// AppendSlot appends v to the slots of o.
func (o *Object) AppendSlot(v Value) {
	old := o.slots
	o.slots = append(old, v)
	if cap(o.slots) != cap(old) {
		o.leaveSlots(old)
	}
}

// ReserveSlots gives the slots of o room for n more values.
func (o *Object) ReserveSlots(n int) {
	if old := o.slots; cap(old)-len(old) < n {
		o.slots = slices.Grow(old, n)
		o.leaveSlots(old)
	}
}

// leaveSlots clears old, storage that the slots of o have moved out of, when it stays reachable.
func (o *Object) leaveSlots(old []Value) {
	// Inline caches point into the slots of global objects and of prototypes.
	if o.Pinned&(PinnedSlots|PinnedGlobalSlots) != 0 || o.flags&ObjIsProto != 0 {
		clear(old)
	}
	o.Pinned &^= PinnedSlots
}

// leaveElems clears old, storage that the elements of o have moved out of, when it is pinned and
// so stays reachable. Shared storage stays as it is, and the new elements of o are its own.
func (o *Object) leaveElems(old []Value) {
	if o.Pinned&PinnedElems != 0 {
		clear(old)
	}
	// The elements of o are no longer shared, if they were.
	o.Pinned &^= PinnedElems | SharedElems
}

// ownElems gives o elements of its own in place of shared ones, so that they can be changed in
// place.
func (o *Object) ownElems() {
	if o.Pinned&SharedElems != 0 {
		o.copySharedElems()
	}
}

// copySharedElems is ownElems for an object with shared elements. It stays out of line so that
// ownElems can be inlined.
//
//go:noinline
func (o *Object) copySharedElems() {
	o.elems = slices.Clone(o.elems)
	o.Pinned &^= SharedElems
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
	o.slots = Truncated(o.slots, n)
}

// removeSlot removes the slot i, moving the later ones down.
func (o *Object) removeSlot(i int) {
	n := len(o.slots) - 1
	copy(o.slots[i:], o.slots[i+1:])
	o.slots[n] = Value{}
	o.slots = o.slots[:n]
}

// setElems makes elems the elements of o, which may not have a tail of its own yet.
func (o *Object) setElems(elems []Value) {
	o.ownTail().elems = elems
}

// InitElems makes storage the elements of an object that has none.
func (o *Object) InitElems(storage []Value) {
	if ValidateExec && len(o.elems) != 0 {
		panic("storage: InitElems on an object with elements")
	}
	o.setElems(storage)
}

// ReserveElems gives the elements of o room for n more elements.
func (o *Object) ReserveElems(n int) {
	if cap(o.elems)-len(o.elems) < n {
		o.moveElems(n)
	}
}

// moveElems moves the elements of o to new storage with room for n more elements.
// It stays out of line so that ReserveElems can be inlined.
//
//go:noinline
func (o *Object) moveElems(n int) {
	elems := o.elems
	if cap(elems) == 0 {
		o.setElems(make([]Value, 0, max(n, MinElems)))
		return
	}
	o.elems = slices.Grow(elems, n)
	o.leaveElems(elems)
}

// AppendElem appends v to the elements of o.
func (o *Object) AppendElem(v Value) {
	elems := o.elems
	if cap(elems) == 0 {
		// An object without elements may not have a tail of its own.
		o.setElems(make([]Value, 0, MinElems))
		elems = o.elems
	}
	o.elems = append(elems, v)
	if cap(o.elems) != cap(elems) {
		o.leaveElems(elems)
	}
}

// ElemsStorage returns the number of values that the storage of the elements of o has room
// for, counting those before the elements that removing the first elements left unused.
func (o *Object) ElemsStorage() int {
	n := cap(o.elems)
	if m := o.Meta(); m != nil && m.shiftedCap == n {
		n += m.shiftedElems
	}
	return n
}

// elemsOversized reports whether the elements of o use less than a quarter of their storage
// and the storage is larger than minShrinkElems.
func (o *Object) elemsOversized() bool {
	n := o.ElemsStorage()
	return n > minShrinkElems && len(o.elems) < n/4
}

// reallocElems moves the elements of o to new storage with room for as many elements again.
func (p *Poller) reallocElems(o *Object) {
	elems := make([]Value, len(o.elems), max(2*len(o.elems), MinElems))
	CopyPolled(p, elems, o.elems)
	o.replaceElems(elems)
}

// MoveElemsTo moves the elements of o to new storage with room for room elements in all.
func (o *Object) MoveElemsTo(room int) {
	elems := make([]Value, len(o.elems), room)
	copy(elems, o.elems)
	o.replaceElems(elems)
}

// replaceElems makes elems, which holds the values that remain, the elements of o.
func (o *Object) replaceElems(elems []Value) {
	o.leaveElems(o.elems)
	o.setElems(elems)
	if m := o.Meta(); m != nil {
		m.shiftedElems, m.shiftedCap = 0, 0
	}
}

// clearLeft clears *v, a value that the storage of the elements keeps after a removal, unless *v
// is a number.
func clearLeft(v *Value) {
	// A number left in the storage keeps nothing alive.
	if v.O != NumberObject {
		*v = Value{}
	}
}

// shiftElem removes the first element without moving the others. The storage keeps its size: see
// elemsOversized.
func (o *Object) shiftElem() {
	m, elems := o.ensureMeta(), o.elems
	if m.shiftedCap != cap(elems) {
		m.shiftedElems = 0
	}
	m.shiftedElems++
	m.shiftedCap = cap(elems) - 1
	elems[0] = Value{}
	o.elems = elems[1:]
}

// clipElems keeps the storage after the elements out of reach of later growth.
func (o *Object) clipElems() {
	old := cap(o.elems)
	if old != 0 {
		// An object without elements may share zeroTail.
		o.elems = slices.Clip(o.elems)
	}
	m := o.Meta()
	switch {
	case m == nil:
	case m.shiftedCap == old:
		m.shiftedCap = cap(o.elems)
	default:
		m.shiftedElems, m.shiftedCap = 0, 0
	}
}

// pushElem appends v to the elements, which must have room for it.
func (o *Object) pushElem(v Value) {
	n := len(o.elems)
	o.elems = o.elems[:n+1]
	o.elems[n] = v
}

// removeLastElem removes the last of elems, the elements of o, which are its own. The storage
// keeps its size: see elemsOversized.
func (o *Object) removeLastElem(elems []Value) {
	if ValidateExec && (len(elems) != len(o.elems) || &elems[0] != &o.elems[0] || o.Pinned&SharedElems != 0) {
		panic("storage: removeLastElem with elements that are not the object's own")
	}
	n := len(elems) - 1
	clearLeft(&elems[n])
	// Reslicing the field itself stores only the length.
	o.elems = o.elems[:n]
}
