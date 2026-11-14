// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// This file holds the operations on the elements of objects for the built-in objects.
// An operation that reports false did nothing, and the caller has to run the generic algorithm.

// IsArray implements IsArray.
func IsArray(v Value) bool {
	return v.o != nil && v.o.kind == kindArray
}

// IsArray reports whether o is an array.
func (o *Object) IsArray() bool {
	return o.kind == kindArray
}

// ArrayCreate implements ArrayCreate.
func (vm *VM) ArrayCreate(length uint32, proto *Object) *Object {
	a := vm.NewArray(nil)
	vm.setProto(a, proto)
	a.length = length
	return a
}

// LengthOfArrayLike implements LengthOfArrayLike.
func (vm *VM) LengthOfArrayLike(o *Object) int64 {
	if o.kind == kindArray {
		return int64(o.length)
	}
	return numconv.ToLength(vm.ToNumber(vm.GetV(Value{o: o}, vm.K.Length)))
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
// Element returns that property, and reports whether it did.
func (o *Object) SetElement(i int64, v Value) bool {
	if i >= 0 && i < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = v
		return true
	}
	return false
}

// ElementList returns a copy of the elements of the array o
// when reading them cannot run any JavaScript.
func (o *Object) ElementList() ([]Value, bool) {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objExotic) != 0 {
		return nil, false
	}
	for _, e := range o.elems {
		if e.isHole() {
			return nil, false
		}
	}
	return append([]Value(nil), o.elems...), true
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

// arrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func (vm *VM) arrayCtorPlain(o *Object) bool {
	if o.shape != vm.arrayShape {
		return false
	}
	c, ok := vm.lookupData(vm.ArrayProto, vm.K.Constructor)
	if !ok || c.o != vm.ArrayCtor {
		return false
	}
	_, p, found := find(vm.ArrayCtor, vm.K.SymSpecies)
	return found && p.flags&FlagAccessor != 0 && vm.ArrayCtor.slots[p.slot].accessor().getter == vm.FnArraySpecies
}

// AppendElements adds vals at the end of the array o and returns the new length.
func (vm *VM) AppendElements(o *Object, vals []Value) (uint32, bool) {
	if o.kind != kindArray || int(o.length) != len(o.elems) || o.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || !vm.arrayProtoPlain() {
		return 0, false
	}
	o.elems = append(o.elems, vals...)
	o.length = uint32(len(o.elems))
	vm.touchProto(o)
	return o.length, true
}

// PrependElements adds vals at the start of the array o and returns the new length.
func (vm *VM) PrependElements(o *Object, vals []Value) (uint32, bool) {
	if !o.isDense() || !vm.arrayProtoPlain() {
		return 0, false
	}
	o.elems = append(append(make([]Value, 0, len(o.elems)+len(vals)), vals...), o.elems...)
	o.length = uint32(len(o.elems))
	return o.length, true
}

// RemoveLastElement removes the last element of the array o and returns it.
// It reports false for an empty array.
func (o *Object) RemoveLastElement() (Value, bool) {
	if !o.isDense() || len(o.elems) == 0 {
		return Value{}, false
	}
	n := len(o.elems) - 1
	v := o.elems[n]
	o.elems[n] = Value{}
	o.elems = o.elems[:n]
	o.length = uint32(n)
	return v, true
}

// RemoveFirstElement removes the first element of the array o and returns it.
// It reports false for an empty array.
func (o *Object) RemoveFirstElement() (Value, bool) {
	if !o.isDense() || len(o.elems) == 0 {
		return Value{}, false
	}
	v := o.elems[0]
	copy(o.elems, o.elems[1:])
	n := len(o.elems) - 1
	o.elems[n] = Value{}
	o.elems = o.elems[:n]
	o.length = uint32(n)
	return v, true
}

// ReverseElements reverses the order of the elements of the array o.
func (o *Object) ReverseElements() bool {
	if !o.isDense() {
		return false
	}
	for i, j := 0, len(o.elems)-1; i < j; i, j = i+1, j-1 {
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
	return vm.NewArray(append([]Value(nil), o.elems[start:end]...)), true
}

// SpliceElements replaces del elements of the array o from start on with items, and returns
// the removed elements as a new array, as Array.prototype.splice creates it.
// length is the length of o that start and del were checked against.
func (vm *VM) SpliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	if length != int64(len(o.elems)) || start < 0 || start > length || del < 0 || del > length-start {
		return nil, false
	}
	if !o.isDense() || !vm.arrayCtorPlain(o) || !vm.arrayProtoPlain() {
		return nil, false
	}
	removed := append([]Value(nil), o.elems[start:start+del]...)
	tail := append([]Value(nil), o.elems[start+del:]...)
	o.elems = append(append(o.elems[:start], items...), tail...)
	o.length = uint32(len(o.elems))
	return vm.NewArray(removed), true
}

// AppendElementsOf adds the elements of the array src at the end of the array dst,
// which must have the length n, exactly n elements and no other property.
func (vm *VM) AppendElementsOf(dst *Object, n int64, src *Object) bool {
	if dst.kind != kindArray || int64(dst.length) != n || int64(len(dst.elems)) != n || dst.shape != vm.arrayShape {
		return false
	}
	if dst.flags&(objSparse|objNotExtensible|objFrozenLength) != 0 || !src.isDense() {
		return false
	}
	dst.elems = append(dst.elems, src.elems...)
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
		idxs := vm.sparseIndexList(o)
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
		idxs := vm.sparseIndexList(o)
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

// sparseIndexList returns the array indices that o stores in its shape, in ascending order.
// The result must not be modified.
func (vm *VM) sparseIndexList(o *Object) []uint32 {
	s := o.shape
	if vm.sparseIndexShape == s && vm.sparseIndexCount == s.count {
		return vm.sparseIndices
	}
	if added, ok := vm.addedSparseKeys(s); ok && len(added) <= maxSparseIndexInserts {
		idxs := vm.sparseIndices
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
		vm.sparseIndexShape = s
		vm.sparseIndexCount = s.count
		vm.sparseIndices = idxs
		return idxs
	}
	idxs := vm.sparseIndices[:0]
	for _, k := range s.keys() {
		if k.kind != kindString {
			continue
		}
		if i, ok := stringIndex(k.str); ok {
			idxs = append(idxs, i)
		}
	}
	slices.Sort(idxs)
	vm.sparseIndexShape = s
	vm.sparseIndexCount = s.count
	vm.sparseIndices = idxs
	return idxs
}

// maxSparseIndexInserts is the number of new keys above which sparseIndexList rebuilds the list
// instead of inserting each one.
const maxSparseIndexInserts = 16

// addedSparseKeys returns the keys that s has in addition to those of the shape whose indices
// sparseIndices holds, and reports whether s has all of those keys in the same slots.
func (vm *VM) addedSparseKeys(s *shape) ([]*Object, bool) {
	c, n := vm.sparseIndexShape, vm.sparseIndexCount
	if c == nil || s.count < n {
		return nil, false
	}
	if s.private {
		// A private shape only grows in place.
		if c != s {
			return nil, false
		}
		return s.order[n:], true
	}
	if c.private {
		return nil, false
	}
	if s.count-n > maxSparseIndexInserts {
		return nil, false
	}
	var added []*Object
	t := s
	for ; t.count > n; t = t.parent {
		added = append(added, t.key)
	}
	if t != c {
		return nil, false
	}
	return added, true
}
