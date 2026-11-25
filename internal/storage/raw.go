// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// Raw gives access to the property storage of an object without the checks of the property
// operations. Only the interpreter loop uses it.
type Raw struct {
	o *Object
}

// Raw returns the raw access to o.
func (o *Object) Raw() Raw {
	return Raw{o: o}
}

// Elems returns the elements, which the caller must not change.
func (r Raw) Elems() []Value {
	return r.o.elems
}

// ElemCount returns the number of elements.
func (r Raw) ElemCount() int {
	return len(r.o.elems)
}

// ElemRoom returns how many elements the storage of the elements has room for.
func (r Raw) ElemRoom() int {
	return cap(r.o.elems)
}

// Elem returns the element i, which can be a hole.
func (r Raw) Elem(i int) Value {
	return r.o.elems[i]
}

// SetElem makes v the element i.
func (r Raw) SetElem(i int, v Value) {
	r.o.elems[i] = v
}

// ExtendElems makes the first n values of the storage of the elements the elements. The storage
// must have room for them.
func (r Raw) ExtendElems(n int) {
	r.o.elems = r.o.elems[:n]
}

// SlotCount returns the number of slots.
func (r Raw) SlotCount() int {
	return len(r.o.slots)
}

// SlotRoom returns how many slots the storage of the slots has room for.
func (r Raw) SlotRoom() int {
	return cap(r.o.slots)
}

// Slot returns the value of the slot i.
func (r Raw) Slot(i int) Value {
	return r.o.slots[i]
}

// SlotRef returns a pointer to the slot i, which the caller must not store to.
func (r Raw) SlotRef(i int) *Value {
	return &r.o.slots[i]
}

// SetSlot makes v the value of the slot i.
func (r Raw) SetSlot(i int, v Value) {
	r.o.slots[i] = v
}

// ExtendSlots makes the first n values of the storage of the slots the slots. The storage must
// have room for them.
func (r Raw) ExtendSlots(n int) {
	r.o.slots = r.o.slots[:n]
}

// SetShape makes s the shape.
func (r Raw) SetShape(s *Shape) {
	r.o.shape = s
}

// SetFlags makes flags the flags.
func (r Raw) SetFlags(flags uint8) {
	r.o.flags = flags
}

// SetLength makes n the length of an array.
func (r Raw) SetLength(n uint32) {
	r.o.length = n
}
