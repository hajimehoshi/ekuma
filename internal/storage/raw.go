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
	r.o.ensureUnpacked()
	return r.o.elems
}

// ElemsShared reports whether the elements are shared with other arrays, so that storing one
// needs the property operations.
func (r Raw) ElemsShared() bool {
	return r.o.Pinned&SharedElems != 0
}

// ElemCount returns the number of boxed elements.
func (r Raw) ElemCount() int {
	return len(r.o.elems)
}

// ElemRoom returns the capacity of the boxed elements.
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
// must have room for them, and the caller must store each new element before reading it.
func (r Raw) ExtendElems(n int) {
	if ValidateExec && r.o.packedNums() != nil {
		panic("storage: extending boxed storage on a packed array")
	}
	r.o.elems = r.o.elems[:n]
}

// NumCount returns the number of packed numeric elements.
func (r Raw) NumCount() int {
	if p, ok := r.o.Ext.(*packedNumElems); ok {
		return len(p.values)
	}
	return 0
}

// NumRoom returns the capacity of the packed numeric elements.
func (r Raw) NumRoom() int {
	if p, ok := r.o.Ext.(*packedNumElems); ok {
		return cap(p.values)
	}
	return 0
}

// Nums returns the packed numeric elements, which the caller must not change.
func (r Raw) Nums() []float64 {
	if p, ok := r.o.Ext.(*packedNumElems); ok {
		return p.values
	}
	return nil
}

// SetNum sets the packed numeric element at i.
func (r Raw) SetNum(i int, n float64) {
	r.o.Ext.(*packedNumElems).values[i] = n
}

// ExtendNums sets the number of packed elements within their existing capacity.
func (r Raw) ExtendNums(n int) {
	p := r.o.Ext.(*packedNumElems)
	p.values = p.values[:n]
}

// HasNumSideData reports whether the packed array keeps state beside its elements.
func (r Raw) HasNumSideData() bool {
	return r.o.Ext.(*packedNumElems).ext != nil
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
