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
	o.ensureUnpacked()
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

// packedNumElems holds a hole-free numeric prefix and the array's other state.
type packedNumElems struct {
	values []float64
	ext    any
}

const (
	packElemsThreshold = MaxInlineObjectValues

	// Limit the number of buffer allocations when growing to MaxPresizedElems.
	packedGrowthThreshold = 256
)

type packedNumBlock struct {
	packedNumElems
	storage [2 * packElemsThreshold]float64
}

func newPackedNumBlock(n, room int) *packedNumElems {
	p := &packedNumBlock{}
	p.values = p.storage[:n:room]
	return &p.packedNumElems
}

func newPackedNums(n int) *packedNumElems {
	if n <= 2*packElemsThreshold {
		return newPackedNumBlock(n, 2*packElemsThreshold)
	}
	return &packedNumElems{
		values: make([]float64, n),
	}
}

func (o *Object) packedNums() *packedNumElems {
	p, _ := o.Ext.(*packedNumElems)
	return p
}

func (o *Object) numericElements() []float64 {
	if p := o.packedNums(); p != nil {
		return p.values
	}
	return nil
}

func (o *Object) packElems(p *packedNumElems) {
	o.replaceElems(nil)
	p.ext = o.Ext
	o.ownTail().Ext = p
}

// setElems makes elems the elements of o, which may not have a tail of its own yet.
func (o *Object) setElems(elems []Value) {
	o.ownTail().elems = elems
}

// InitElems makes storage the elements of an object that has none.
func (o *Object) InitElems(storage []Value) {
	if ValidateExec && (len(o.elems) != 0 || o.packedNums() != nil) {
		panic("storage: InitElems on an object with elements")
	}
	o.setElems(storage)
}

// ReserveElems gives the elements of o room for n more elements.
func (o *Object) ReserveElems(n int) {
	if p := o.packedNums(); p != nil {
		o.unpackNums(p, n)
	}
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
	o.touchProtoElems()
	elems := o.elems
	if cap(elems) == 0 {
		if p := o.packedNums(); p != nil {
			if v.O == NumberObject {
				o.appendNum(p, v.N)
				return
			}
			o.unpackNums(p, 1)
			elems = o.elems
		}
	}
	// Keep small arrays in their inline storage until they need a new allocation.
	if len(elems) == cap(elems) && len(elems) == packElemsThreshold && v.O == NumberObject && o.tryPackNums(v.N) {
		return
	}
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

func (o *Object) appendNum(p *packedNumElems, v float64) {
	n := len(p.values)
	if n == cap(p.values) {
		switch {
		case n >= PollChunk:
			p.values = appendPolled(&o.shape.Space.Poller, p.values, []float64{v})
			return
		case n >= packedGrowthThreshold && n < MaxPresizedElems:
			values := make([]float64, n, min(2*n, MaxPresizedElems))
			copy(values, p.values)
			p.values = values
		}
	}
	p.values = append(p.values, v)
}

// tryPackNums replaces a full buffer with room for one more numeric element.
func (o *Object) tryPackNums(v float64) bool {
	elems := o.elems
	n := len(elems)
	if o.Kind != KindArray || o.flags != 0 || o.Pinned&SharedElems != 0 || n != packElemsThreshold {
		return false
	}
	for _, e := range elems {
		if e.O != NumberObject {
			return false
		}
	}
	p := newPackedNumBlock(n+1, 2*n)
	for i, e := range elems {
		p.values[i] = e.N
	}
	p.values[n] = v
	o.packElems(p)
	return true
}

func allNumeric(p *Poller, vals []Value) bool {
	for i, v := range vals {
		if v.O != NumberObject {
			return false
		}
		if i%PollInterval == PollInterval-1 {
			p.Poll()
		}
	}
	return true
}

func writeNums(p *Poller, dst []float64, vals []Value) {
	for i, v := range vals {
		if i > 0 && i%PollInterval == 0 {
			p.Poll()
		}
		dst[i] = v.N
	}
}

// appendNumericValues appends numeric values to an array, packing its own numeric elements on growth.
func (o *Object) appendNumericValues(p *Poller, vals []Value) bool {
	if packed := o.packedNums(); packed != nil {
		if !allNumeric(p, vals) {
			return false
		}
		nums := packed.values
		n, newLen := len(nums), len(nums)+len(vals)
		if newLen > cap(nums) {
			room := cap(nums)
			grown := make([]float64, newLen, max(newLen, min(2*room, MaxPresizedElems), room+room/4))
			CopyPolled(p, grown, nums)
			nums = grown
		}
		nums = nums[:newLen]
		writeNums(p, nums[n:], vals)
		packed.values = nums
		return true
	}
	elems := o.elems
	n := len(elems) + len(vals)
	if n <= max(cap(elems), packElemsThreshold) || o.flags != 0 || o.Pinned&SharedElems != 0 ||
		!allNumeric(p, vals) || !allNumeric(p, elems) {
		return false
	}
	packed := newPackedNums(n)
	writeNums(p, packed.values, elems)
	writeNums(p, packed.values[len(elems):], vals)
	o.packElems(packed)
	return true
}

func (o *Object) ensureUnpacked() {
	if p := o.packedNums(); p != nil {
		o.unpackNums(p, 0)
	}
}

//go:noinline
func (o *Object) unpackNums(p *packedNumElems, extra int) {
	n := len(p.values)
	room := max(cap(p.values), MinElems)
	if room-n < extra {
		room = max(n+extra, room+room/4)
	}
	elems := make([]Value, n, room)
	boxNums(&o.shape.Space.Poller, elems, p.values)
	o.ownTail().Ext = p.ext
	// p can be part of the allocation of o, which must not keep the numbers reachable.
	*p = packedNumElems{}
	o.replaceElems(elems)
}

func boxNums(p *Poller, dst []Value, nums []float64) {
	for i, n := range nums {
		if i > 0 && i%PollInterval == 0 {
			p.Poll()
		}
		dst[i] = Number(n)
	}
}

// ElemsStorage returns the number of values that the storage of the elements of o has room
// for, counting those before the elements that removing the first elements left unused.
func (o *Object) ElemsStorage() int {
	n := o.ElemRoom()
	if m := o.Meta(); m != nil && m.shiftedCap == n {
		n += m.shiftedElems
	}
	return n
}

// elemsOversized reports whether the elements of o use less than a quarter of their storage
// and the storage is larger than minShrinkElems.
func (o *Object) elemsOversized() bool {
	return ElemsStorageOversized(o.ElemCount(), o.ElemsStorage())
}

// ElemsStorageOversized reports whether count elements leave too much storage unused.
func ElemsStorageOversized(count, room int) bool {
	return room > minShrinkElems && count < room/4
}

// reallocElems moves the elements of o to new storage with room for as many elements again.
func (p *Poller) reallocElems(o *Object) {
	if packed := o.packedNums(); packed != nil {
		n := len(packed.values)
		nums := make([]float64, n, max(2*n, MinElems))
		CopyPolled(p, nums, packed.values)
		packed.values = nums
		o.clearShiftedElems()
		return
	}
	elems := make([]Value, len(o.elems), max(2*len(o.elems), MinElems))
	CopyPolled(p, elems, o.elems)
	o.replaceElems(elems)
}

// MoveElemsTo moves the elements of o to new storage with room for room elements in all.
func (o *Object) MoveElemsTo(room int) {
	if packed := o.packedNums(); packed != nil {
		nums := make([]float64, len(packed.values), room)
		copy(nums, packed.values)
		packed.values = nums
		o.clearShiftedElems()
		return
	}
	elems := make([]Value, len(o.elems), room)
	copy(elems, o.elems)
	o.replaceElems(elems)
}

// replaceElems makes elems, which holds the values that remain, the elements of o.
func (o *Object) replaceElems(elems []Value) {
	o.leaveElems(o.elems)
	o.setElems(elems)
	o.clearShiftedElems()
}

func (o *Object) clearShiftedElems() {
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
	o.recordShift(cap(o.elems))
	o.elems[0] = Value{}
	o.elems = o.elems[1:]
}

func (o *Object) recordShift(room int) {
	m := o.ensureMeta()
	if m.shiftedCap != room {
		m.shiftedElems = 0
	}
	m.shiftedElems++
	m.shiftedCap = room - 1
}

// clipElems keeps the storage after the elements out of reach of later growth.
func (o *Object) clipElems() {
	old := o.ElemRoom()
	if packed := o.packedNums(); packed != nil {
		packed.values = slices.Clip(packed.values)
	} else if old != 0 {
		// An object without elements may share zeroTail.
		o.elems = slices.Clip(o.elems)
	}
	m := o.Meta()
	switch {
	case m == nil:
	case m.shiftedCap == old:
		m.shiftedCap = o.ElemRoom()
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
