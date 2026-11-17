// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// This file holds the operations on the elements of objects for the built-in objects.
// An operation that reports false did nothing, and the caller has to run the generic algorithm.

// minElems is the number of elements that the storage of an array has room for when it is
// first allocated to add an element.
const minElems = 4

// maxPresizedElems is the largest length for which ArrayCreate allocates the storage of the
// elements upfront.
const maxPresizedElems = 1024

// minShrinkElems is the capacity up to which the storage of the elements of an array is kept
// however few elements remain.
const minShrinkElems = 64

// shiftCopyElems is the length below which removing the first element of an array moves
// the other elements, so that the storage keeps its room for elements added later.
const shiftCopyElems = 32

// ArrayCreate implements ArrayCreate.
func (vm *VM) ArrayCreate(length uint32, proto *Object) *Object {
	var a *Object
	if length > 0 && length <= maxPresizedElems {
		// The array is usually filled up to its length afterwards.
		var elems []Value
		a, elems = newObjectWithStorage(int(length))
		a.kind = kindArray
		a.class = classArray
		a.shape = vm.shapes.array
		a.elems = elems[:0]
	} else {
		a = vm.NewArray(nil)
	}
	vm.setProto(a, proto)
	a.length = length
	return a
}

// reserveElems returns elems with room for n more elements.
func reserveElems(elems []Value, n int) []Value {
	if cap(elems)-len(elems) >= n {
		return elems
	}
	if cap(elems) == 0 {
		return make([]Value, 0, max(n, minElems))
	}
	return slices.Grow(elems, n)
}

// appendElem appends v to elems.
func appendElem(elems []Value, v Value) []Value {
	if cap(elems) == 0 {
		elems = make([]Value, 0, minElems)
	}
	return append(elems, v)
}

// elemsStorage returns the number of values that the storage of the elements of o has room
// for, counting those before the elements that removing the first elements left unused.
func (o *Object) elemsStorage() int {
	n := cap(o.elems)
	if m := o.meta; m != nil && m.shiftedCap == n {
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
func (vm *VM) reallocElems(o *Object) {
	elems := make([]Value, len(o.elems), max(2*len(o.elems), minElems))
	CopyPolled(vm, elems, o.elems)
	o.elems = elems
}

// LengthOfArrayLike implements LengthOfArrayLike.
func (vm *VM) LengthOfArrayLike(o *Object) int64 {
	if o.kind == kindArray {
		return int64(o.length)
	}
	return numconv.ToLength(vm.ToNumber(vm.GetV(Value{o: o}, vm.Key(KeyLength))))
}

// Element returns the own property of o with the integer index i when reading it
// cannot run any JavaScript. It reports false when that is not known.
func (o *Object) Element(i int64) (Value, bool) {
	if i >= 0 && i < int64(len(o.elems)) {
		if e := o.elems[i]; !e.isHole() {
			return e, true
		}
	}
	if o.class == classTypedArray && i >= 0 && i <= MaxArrayIndex {
		t := o.ext.(*TypedArray)
		if b, ok := t.element(uint32(i)); ok {
			return o.shape.vm.loadElement(t.typ, b), true
		}
	}
	return Value{}, false
}

// SetElement stores v as the own property of o with the integer index i when
// [Object.Element] returns that property, and reports whether it did.
func (o *Object) SetElement(i int64, v Value) bool {
	if i >= 0 && i < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = v
		return true
	}
	return false
}

// ElementCount returns how many elements the array v holds in memory, holes included, or 0
// when v is not an array.
func ElementCount(v Value) int {
	if v.o == nil || v.o.kind != kindArray {
		return 0
	}
	return len(v.o.elems)
}

// ElementList returns a copy of the elements of the array o
// when reading them cannot run any JavaScript.
func (o *Object) ElementList() ([]Value, bool) {
	elems, ok := o.plainElems()
	if !ok {
		return nil, false
	}
	return append([]Value(nil), elems...), true
}

// plainElems returns the elements of o, which the caller must not modify, when o is an array
// whose elements can be read without running any JavaScript. o may be nil.
func (o *Object) plainElems() ([]Value, bool) {
	if o == nil || o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objExotic) != 0 {
		return nil, false
	}
	for _, e := range o.elems {
		if e.isHole() {
			return nil, false
		}
	}
	return o.elems, true
}

// isDense reports whether o is an array whose elements are all present in elems
// and that accepts changes to its elements and its length.
func (o *Object) isDense() bool {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 {
		return false
	}
	for _, e := range o.elems {
		if e.isHole() {
			return false
		}
	}
	return true
}

// elemsMovable reports whether moving the elements of the array o by index, holes included,
// does what moving them one by one with HasProperty, Set and DeletePropertyOrThrow does, and
// whether o accepts changes to its elements and its length.
func (vm *VM) elemsMovable(o *Object) bool {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 {
		return false
	}
	return vm.holesAreAbsent(o) || o.isDense()
}

// arrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func (vm *VM) arrayCtorPlain(o *Object) bool {
	if o.shape != vm.shapes.array {
		return false
	}
	c, ok := vm.lookupData(vm.Intrinsic(IntrinsicArrayPrototype), vm.Key(KeyConstructor))
	if !ok || c.o != vm.Intrinsic(IntrinsicArray) {
		return false
	}
	_, p, found := find(vm.Intrinsic(IntrinsicArray), vm.WellKnownSymbol(SymbolSpecies))
	return found && p.flags&FlagAccessor != 0 && vm.Intrinsic(IntrinsicArray).slots[p.slot].accessor().getter == vm.Intrinsic(IntrinsicArraySpeciesGetter)
}

// newElementsPlain reports whether storing a new element in the array o defines it as an own
// data property without consulting a prototype.
func (vm *VM) newElementsPlain(o *Object) bool {
	return o.shape.proto == vm.Intrinsic(IntrinsicArrayPrototype) && vm.arrayProtoPlain()
}

// AppendElements adds vals at the end of the array o and returns the new length.
func (vm *VM) AppendElements(o *Object, vals []Value) (uint32, bool) {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || !vm.newElementsPlain(o) {
		return 0, false
	}
	o.elems = append(reserveElems(o.elems, len(vals)), vals...)
	o.length = uint32(len(o.elems))
	vm.touchProto(o)
	return o.length, true
}

// PrependElements adds vals at the start of the array o and returns the new length.
func (vm *VM) PrependElements(o *Object, vals []Value) (uint32, bool) {
	if !vm.elemsMovable(o) || !vm.newElementsPlain(o) {
		return 0, false
	}
	n, k := len(o.elems), len(vals)
	if cap(o.elems)-n >= k {
		o.elems = o.elems[:n+k]
		copy(o.elems[k:], o.elems[:n])
	} else {
		elems := make([]Value, n+k, n+k+max((n+k)/2, minElems))
		copy(elems[k:], o.elems)
		o.elems = elems
	}
	copy(o.elems, vals)
	o.length = uint32(len(o.elems))
	return o.length, true
}

// RemoveLastElement removes the last element of the array o and returns it.
// It reports false for an empty array.
func (o *Object) RemoveLastElement() (Value, bool) {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || len(o.elems) == 0 {
		return Value{}, false
	}
	// Holes before the last element stay holes after it is removed.
	n := len(o.elems) - 1
	v := o.elems[n]
	if v.isHole() {
		return Value{}, false
	}
	o.elems[n] = Value{}
	o.elems = o.elems[:n]
	o.length = uint32(n)
	if o.elemsOversized() {
		o.shape.vm.reallocElems(o)
	}
	return v, true
}

// RemoveFirstElement removes the first element of the array o and returns it.
// It reports false for an empty array.
func (o *Object) RemoveFirstElement() (Value, bool) {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || len(o.elems) == 0 {
		return Value{}, false
	}
	v := o.elems[0]
	if v.isHole() {
		return Value{}, false
	}
	// A hole after the first element moves with the elements when no prototype can have
	// an element.
	if !o.shape.vm.holesAreAbsent(o) && slices.ContainsFunc(o.elems, Value.isHole) {
		return Value{}, false
	}
	n := len(o.elems) - 1
	if n < shiftCopyElems {
		copy(o.elems, o.elems[1:])
		o.elems[n] = Value{}
		o.elems = o.elems[:n]
	} else {
		// The storage before the elements is released when they move to new storage.
		shifted := o.elemsStorage() - cap(o.elems) + 1
		o.elems[0] = Value{}
		o.elems = o.elems[1:]
		m := o.ensureMeta()
		m.shiftedElems = shifted
		m.shiftedCap = cap(o.elems)
	}
	o.length = uint32(n)
	if o.elemsOversized() {
		o.shape.vm.reallocElems(o)
	}
	return v, true
}

// FillElements stores v as the elements of the array o from start up to end, as
// Array.prototype.fill does when the length of o is n. It reports false when it did nothing.
func (vm *VM) FillElements(o *Object, n, start, end int64, v Value) bool {
	if o.kind != kindArray || int64(o.length) != n || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 {
		return false
	}
	// Storing past the stored elements must not skip an index.
	if start < 0 || start > int64(len(o.elems)) || end > n || !vm.holesAreAbsent(o) {
		return false
	}
	vm.touchProto(o)
	for i := start; i < end; i++ {
		if vm.interrupt.Load() {
			vm.Poll()
		}
		if i < int64(len(o.elems)) {
			o.elems[i] = v
		} else {
			o.elems = appendElem(o.elems, v)
		}
	}
	return true
}

// SearchElements returns the first index of the elements of o from i toward end, which is
// excluded and may be below i, whose element equals target, comparing with SameValueZero when
// sameValueZero is set and with IsStrictlyEqual otherwise. It looks only at a run of elements
// that reading runs no JavaScript for, and returns -1 and the index after the run when the run
// has no such element.
func (vm *VM) SearchElements(o *Object, i, end int64, target Value, sameValueZero bool) (found, next int64) {
	eq := IsStrictlyEqual
	if sameValueZero {
		eq = SameValueZero
	}
	elems := o.elems
	// The runs between two checks for an interruption end at multiples of PollInterval, so that
	// the checks keep their pace when the callers resume a search after a hole.
	if end < i {
		for i > end && i < int64(len(elems)) {
			stop := max(end, (i&^(PollInterval-1))-1)
			for ; i > stop; i-- {
				e := elems[i]
				if e.isHole() {
					return -1, i
				}
				if eq(e, target) {
					return i, i
				}
			}
			if i > end && i&(PollInterval-1) == PollInterval-1 {
				vm.Poll()
			}
		}
		return -1, i
	}
	for i < end && i < int64(len(elems)) {
		stop := min(end, int64(len(elems)), (i|(PollInterval-1))+1)
		for ; i < stop; i++ {
			e := elems[i]
			if e.isHole() {
				return -1, i
			}
			if eq(e, target) {
				return i, i
			}
		}
		if i < end && i&(PollInterval-1) == 0 {
			vm.Poll()
		}
	}
	return -1, i
}

// ReverseElements reverses the order of the elements of the array o.
func (vm *VM) ReverseElements(o *Object) bool {
	if !o.isDense() {
		return false
	}
	for i, j := 0, len(o.elems)-1; i < j; i, j = i+1, j-1 {
		vm.PollAt(int64(i))
		o.elems[i], o.elems[j] = o.elems[j], o.elems[i]
	}
	return true
}

// CopyElementRange returns a new array with the elements of the array o from start up to end,
// as Array.prototype.slice creates it. A range that ends before it starts is empty.
func (vm *VM) CopyElementRange(o *Object, start, end int64) (*Object, bool) {
	if start < 0 || end > int64(len(o.elems)) || !o.isDense() || !vm.arrayCtorPlain(o) {
		return nil, false
	}
	if start >= end {
		return vm.NewArray(nil), true
	}
	return vm.NewArray(appendPolled(vm, nil, o.elems[start:end])), true
}

// SpliceElements replaces del elements of the array o from start on with items, and returns
// the removed elements as a new array, as Array.prototype.splice creates it.
// length is the length of o that start and del were checked against.
func (vm *VM) SpliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	if length != int64(len(o.elems)) || start < 0 || start > length || del < 0 || del > length-start {
		return nil, false
	}
	if !vm.elemsMovable(o) || !vm.arrayCtorPlain(o) || !vm.arrayProtoPlain() {
		return nil, false
	}
	removed := appendPolled(vm, nil, o.elems[start:start+del])
	n, k := int(length), len(items)
	newLen := n - int(del) + k
	if n-int(start+del) > pollChunk {
		vm.Poll()
	}
	elems := o.elems
	if newLen > n {
		elems = slices.Grow(elems, newLen-n)
	}
	elems = elems[:max(n, newLen)]
	copy(elems[int(start)+k:], elems[start+del:n])
	copy(elems[start:], items)
	// The removed elements stay out of reach in the storage that the elements keep.
	clear(elems[newLen:])
	o.elems = elems[:newLen]
	o.length = uint32(newLen)
	if o.elemsOversized() {
		vm.reallocElems(o)
	}
	return vm.NewArray(removed), true
}

// AppendElementsOf adds the elements of the array src at the end of the array dst,
// which must have the length n, exactly n elements and no other property.
func (vm *VM) AppendElementsOf(dst *Object, n int64, src *Object) bool {
	if dst.kind != kindArray || int64(dst.length) != n || int64(len(dst.elems)) != n || dst.shape != vm.shapes.array {
		return false
	}
	if dst.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || !src.isDense() {
		return false
	}
	dst.elems = appendPolled(vm, dst.elems, src.elems)
	dst.length = uint32(len(dst.elems))
	return true
}

// SkipHoles returns the least index j with i <= j <= end such that neither o nor its prototypes
// have a property with an integer index from i to j-1. It returns i when that is not known
// without running JavaScript.
func (vm *VM) SkipHoles(o *Object, i, end int64) int64 {
	if i < 0 || i >= end || i < int64(len(o.elems)) && !o.elems[i].isHole() || !vm.holesAreAbsent(o) {
		return i
	}
	j := end
	for k := i; k < min(end, int64(len(o.elems))); k++ {
		if !o.elems[k].isHole() {
			j = k
			break
		}
	}
	if o.flags&objSparse != 0 {
		idxs := vm.sparseIndices.list(o)
		if p, _ := slices.BinarySearch(idxs, uint32(i)); p < len(idxs) && int64(idxs[p]) < j {
			j = int64(idxs[p])
		}
	}
	return j
}

// SkipHolesBackward returns the greatest index j with end <= j <= i such that neither o nor its
// prototypes have a property with an integer index from j+1 to i. It returns i when that is not
// known without running JavaScript.
func (vm *VM) SkipHolesBackward(o *Object, i, end int64) int64 {
	if i <= end || i < 0 || i > MaxArrayIndex || i < int64(len(o.elems)) && !o.elems[i].isHole() || !vm.holesAreAbsent(o) {
		return i
	}
	j := max(end, -1)
	for k := min(i, int64(len(o.elems))-1); k > j; k-- {
		if !o.elems[k].isHole() {
			j = k
			break
		}
	}
	if o.flags&objSparse != 0 {
		idxs := vm.sparseIndices.list(o)
		p, found := slices.BinarySearch(idxs, uint32(i))
		if found {
			p++
		}
		if p > 0 && int64(idxs[p-1]) > j {
			j = int64(idxs[p-1])
		}
	}
	return max(j, end)
}

// holesAreAbsent reports whether an integer index that o does not store is absent from o
// and its prototypes, and whether finding that out runs no JavaScript.
func (vm *VM) holesAreAbsent(o *Object) bool {
	if o.kind != kindArray || o.flags&objExotic != 0 {
		return false
	}
	for h := o.shape.proto; h != nil; h = h.shape.proto {
		if len(h.elems) != 0 || h.flags&(objSparse|objExotic) != 0 || h.class != classObject && h.class != classArray && h.class != classFunction {
			return false
		}
	}
	return true
}

// sparseIndexCache holds the array indices that the keys of one shape contain.
type sparseIndexCache struct {
	// shape and count identify the shape whose array index keys indices holds in ascending
	// order. A private shape only grows in place, so its count tells whether the keys changed.
	shape   *shape
	count   int32
	indices []uint32
}

// list returns the array indices that o stores in its shape, in ascending order.
// The result must not be modified.
func (c *sparseIndexCache) list(o *Object) []uint32 {
	s := o.shape
	if c.shape == s && c.count == s.count {
		return c.indices
	}
	// addedKeys appends at most maxSparseIndexInserts keys, so buf holds them all.
	var buf [maxSparseIndexInserts]*Object
	if added, ok := c.addedKeys(s, buf[:0]); ok {
		idxs := c.indices
		for _, k := range added {
			if k.kind != kindString {
				continue
			}
			if i, ok := stringIndex(k.str); ok {
				if p, found := slices.BinarySearch(idxs, i); !found {
					idxs = slices.Insert(idxs, p, i)
				}
			}
		}
		c.shape = s
		c.count = s.count
		c.indices = idxs
		return idxs
	}
	idxs := c.indices[:0]
	for _, k := range s.keys() {
		if k.kind != kindString {
			continue
		}
		if i, ok := stringIndex(k.str); ok {
			idxs = append(idxs, i)
		}
	}
	slices.Sort(idxs)
	c.shape = s
	c.count = s.count
	c.indices = idxs
	return idxs
}

// remove updates the cache for the deletion of key from the shape old, which made the shape s.
func (c *sparseIndexCache) remove(old, s *shape, key *Object) {
	if c.shape != old || c.count != old.count {
		return
	}
	if key.kind == kindString {
		if i, ok := stringIndex(key.str); ok {
			if p, found := slices.BinarySearch(c.indices, i); found {
				c.indices = slices.Delete(c.indices, p, p+1)
			}
		}
	}
	c.shape = s
	c.count = s.count
}

// maxSparseIndexInserts is the number of new keys above which sparseIndexCache.list rebuilds
// the list instead of inserting each one.
const maxSparseIndexInserts = 16

// addedKeys returns the keys that s has in addition to those of the cached shape, appended to
// buf, and reports whether s has all of those keys in the same slots and they are at most
// maxSparseIndexInserts.
func (c *sparseIndexCache) addedKeys(s *shape, buf []*Object) ([]*Object, bool) {
	cached, n := c.shape, c.count
	if cached == nil || s.count < n {
		return nil, false
	}
	if s.private {
		// A private shape only grows in place.
		if cached != s || s.count-n > maxSparseIndexInserts {
			return nil, false
		}
		// Without deletions since, the added keys are the last ones.
		return s.order[len(s.order)-int(s.count-n):], true
	}
	if cached.private {
		return nil, false
	}
	if s.count-n > maxSparseIndexInserts {
		return nil, false
	}
	added := buf
	t := s
	for ; t.count > n; t = t.parent {
		if t.parent == nil {
			return nil, false
		}
		added = append(added, t.key)
	}
	if t != cached {
		return nil, false
	}
	return added, true
}
