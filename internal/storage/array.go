// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// MinElems is the number of elements that the storage of an array has room for when it is
// first allocated to add an element.
const MinElems = 4

// MaxPresizedElems is the largest length for which ArrayCreate allocates the storage of the
// elements upfront.
const MaxPresizedElems = 1024

// minShrinkElems is the capacity up to which the storage of the elements of an array is kept
// however few elements remain.
const minShrinkElems = 64

// shiftCopyElems is the length below which removing the first element of an array moves
// the other elements, so that the storage keeps its room for elements added later.
const shiftCopyElems = 32

// NewArray returns a new array with the given elements, which it keeps.
func (r *RealmPart) NewArray(elems []Value) *Object {
	return NewArray(r.Shapes.Array, elems)
}

// NewArray returns a new array with the shape s and the given elements, which it keeps.
func NewArray(s *Shape, elems []Value) *Object {
	return (&PropertyObject{
		Kind:   KindArray,
		Class:  ClassArray,
		Shape:  s,
		Elems:  elems,
		Length: uint32(len(elems)),
	}).Init()
}

// ArrayCreate implements ArrayCreate.
func (r *RealmPart) ArrayCreate(length uint32, proto *Object) *Object {
	var a *Object
	if length > 0 && length <= MaxPresizedElems {
		// The array is usually filled up to its length afterwards.
		var elems []Value
		a, elems = NewObjectWithStorage(int(length))
		a.Kind = KindArray
		a.Class = ClassArray
		a.Shape = r.Shapes.Array
		a.InitElems(elems[:0])
	} else {
		a = r.NewArray(nil)
	}
	r.SetProto(a, proto)
	a.Length = length
	return a
}

// pairArray is an array of two elements together with the storage of its elements,
// so that creating one is a single allocation.
type pairArray struct {
	PropertyObject
	pair [2]Value
}

// NewArrayPair returns a new array with the elements a and b.
func (r *RealmPart) NewArrayPair(a, b Value) *Object {
	p := &pairArray{
		Kind:   KindArray,
		Class:  ClassArray,
		Pinned: PinnedElems,
		Shape:  r.Shapes.Array,
		Length: 2,
		pair:   [2]Value{a, b},
	}
	p.Elems = p.pair[:]
	return p.Init()
}

// NewTemplateArray returns a new frozen array with the elements vals, which still can be given
// named properties by [Space.Add].
func (r *RealmPart) NewTemplateArray(vals []Value) *Object {
	a := r.NewArray(nil)
	for i, v := range vals {
		r.Add(a, r.IndexName(uint32(i)), v, FlagEnumerable)
	}
	a.Length = uint32(len(vals))
	a.Flags |= ObjSparse | ObjFrozenLength | ObjNotExtensible
	return a
}

// FreezeLength makes the length of the array o read-only.
func (o *Object) FreezeLength() {
	o.Flags |= ObjFrozenLength
}

// InheritsNoIndex reports whether no prototype of o has a property with an array index, and
// looking for one runs no JavaScript. It says nothing about o itself.
func (o *Object) InheritsNoIndex() bool {
	for h := o.Shape.Proto; h != nil; h = h.Shape.Proto {
		if len(h.Elems) != 0 || h.Flags&(ObjSparse|ObjExotic) != 0 || h.Class != ClassObject && h.Class != ClassArray && h.Class != ClassFunction {
			return false
		}
	}
	return true
}

// HolesAreAbsent reports whether o is an array, and an array index that it does not store is
// absent from it and its prototypes, and finding that out runs no JavaScript. An array marked
// ObjSparse stores some indices in its shape.
func (o *Object) HolesAreAbsent() bool {
	return o.Kind == KindArray && o.InheritsNoIndex()
}

// ArrayProtoPlain is [Object.InheritsNoIndex] for the objects whose prototype is %Array.prototype%,
// except that it also reports false when %Object.prototype% does not follow in the prototype chain.
//
// %Array.prototype% is an array and %Object.prototype% an ordinary object from the start, neither of
// which can become exotic later.
func (r *RealmPart) ArrayProtoPlain() bool {
	a := r.Intrinsic(IntrinsicArrayPrototype)
	o := r.Intrinsic(IntrinsicObjectPrototype)
	plain := len(a.Elems) == 0 && len(o.Elems) == 0 && (a.Flags|o.Flags)&ObjSparse == 0 && a.Shape.Proto == o
	if ValidateExec && plain {
		r.checkArrayProtoPlain()
	}
	return plain
}

// NewElementsPlain reports whether storing a new element in the array o defines it as an own
// data property without consulting a prototype, when o accepts it.
func (r *RealmPart) NewElementsPlain(o *Object) bool {
	return o.Shape.Proto == r.Intrinsic(IntrinsicArrayPrototype) && r.ArrayProtoPlain()
}

// elemsComplete reports whether o is an array whose index properties are exactly those in elems
// and whose length is that of elems. There can be holes. It permits reading only.
func (o *Object) elemsComplete() bool {
	return o.Kind == KindArray && int(o.Length) == len(o.Elems) && o.Flags&ObjSparse == 0
}

// elemsChangeable reports whether o is an array whose index properties are exactly those in elems
// and that accepts changes to its elements and its length, which can be greater than that of elems.
func (o *Object) elemsChangeable() bool {
	return o.Kind == KindArray && o.Flags&(ObjSparse|ObjNotExtensible|ObjFrozenLength) == 0
}

// ElemsResizable is elemsComplete and elemsChangeable.
func (o *Object) ElemsResizable() bool {
	return o.Kind == KindArray && int(o.Length) == len(o.Elems) && o.Flags&(ObjSparse|ObjNotExtensible|ObjFrozenLength) == 0
}

// PlainElems returns the elements of o, which the caller must not modify, when o is an array
// whose elements can be read without running any JavaScript. o may be nil.
func (o *Object) PlainElems() ([]Value, bool) {
	if o == nil || !o.elemsComplete() || ContainsHole(o.Elems) {
		return nil, false
	}
	return o.Elems, true
}

// isDense reports whether o is an array whose elements are all present in elems
// and that accepts changes to its elements and its length.
func (o *Object) isDense() bool {
	return o.ElemsResizable() && !ContainsHole(o.Elems)
}

// elemsMovable reports whether moving the elements of the array o by index, holes included,
// does what moving them one by one with HasProperty, Set and DeletePropertyOrThrow does, and
// whether o accepts changes to its elements and its length.
func (o *Object) elemsMovable() bool {
	return o.ElemsResizable() && (o.InheritsNoIndex() || !ContainsHole(o.Elems))
}

// ContainsHole reports whether elems has a hole.
func ContainsHole(elems []Value) bool {
	for i := range elems {
		if elems[i].IsHole() {
			return true
		}
	}
	return false
}

// Element returns the own property of o with the integer index i when reading it
// cannot run any JavaScript. It reports false when that is not known.
func Element(o *Object, i int64) (Value, bool) {
	if o.ObjectProperties != nil && i >= 0 && i < int64(len(o.Elems)) {
		if e := o.Elems[i]; !e.IsHole() {
			return e, true
		}
	}
	if o.Class == ClassTypedArray && i >= 0 && i <= MaxArrayIndex {
		t := o.ExtState().(*TypedArray)
		if b, ok := t.ElementBytes(uint32(i)); ok {
			return ElementFromBytes(t.Type, b), true
		}
	}
	return Value{}, false
}

// SetElement stores v as the own property of o with the integer index i when
// Element returns that property and o is not a typed array, and reports whether it did.
func (o *Object) SetElement(i int64, v Value) bool {
	if o.ObjectProperties != nil && i >= 0 && i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		o.Elems[i] = v
		return true
	}
	return false
}

// ElementCount returns how many elements the array v holds in memory, holes included, or 0
// when v is not an array.
func ElementCount(v Value) int {
	if v.O == nil || v.O.Kind != KindArray {
		return 0
	}
	return len(v.O.Elems)
}

// ElementList returns a copy of the elements of the array o
// when reading them cannot run any JavaScript.
func (o *Object) ElementList() ([]Value, bool) {
	elems, ok := o.PlainElems()
	if !ok {
		return nil, false
	}
	return append([]Value(nil), elems...), true
}

// ArrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func (r *RealmPart) ArrayCtorPlain(o *Object) bool {
	if o.Shape != r.Shapes.Array {
		return false
	}
	ctor := r.Intrinsic(IntrinsicArray)
	c := r.LookupDataSlot(r.Intrinsic(IntrinsicArrayPrototype), r.Key(KeyConstructor))
	if c.Holder == nil || c.Value().O != ctor {
		return false
	}
	p, found := ctor.getOwn(r.WellKnownSymbol(SymbolSpecies))
	return found && p.Flags&FlagAccessor != 0 && ctor.Slots[p.Slot].Accessor().Getter == r.Intrinsic(IntrinsicArraySpeciesGetter)
}

// AppendElements is quiet.Context.AppendElements.
func (r *RealmPart) AppendElements(o *Object, vals []Value) (uint32, bool) {
	if !o.ElemsResizable() || !r.NewElementsPlain(o) {
		return 0, false
	}
	o.ReserveElems(len(vals))
	o.Elems = append(o.Elems, vals...)
	o.Length = uint32(len(o.Elems))
	return o.Length, true
}

// PrependElements is quiet.Context.PrependElements.
func (r *RealmPart) PrependElements(o *Object, vals []Value) (uint32, bool) {
	if !o.elemsMovable() || !r.NewElementsPlain(o) {
		return 0, false
	}
	n, k := len(o.Elems), len(vals)
	if n > PollChunk {
		r.Poll()
	}
	if cap(o.Elems)-n >= k {
		o.Elems = o.Elems[:n+k]
		copy(o.Elems[k:], o.Elems[:n])
	} else {
		elems := make([]Value, n+k, n+k+max((n+k)/2, MinElems))
		copy(elems[k:], o.Elems)
		o.leaveElems(o.Elems)
		o.Elems = elems
	}
	copy(o.Elems, vals)
	o.Length = uint32(len(o.Elems))
	return o.Length, true
}

// RemoveLastElement removes the last element of the array o and returns it.
// It reports false and does nothing when o is empty or needs the generic algorithm.
func (o *Object) RemoveLastElement() (Value, bool) {
	if !o.ElemsResizable() || len(o.Elems) == 0 {
		return Value{}, false
	}
	// Holes before the last element stay holes after it is removed.
	n := len(o.Elems) - 1
	v := o.Elems[n]
	if v.IsHole() {
		return Value{}, false
	}
	o.removeLastElem()
	o.Length = uint32(n)
	if o.ElemsOversized() {
		o.Shape.Space.ReallocElems(o)
	}
	return v, true
}

// RemoveFirstElement removes the first element of the array o and returns it.
// It reports false and does nothing when o is empty or needs the generic algorithm.
func (o *Object) RemoveFirstElement() (Value, bool) {
	if !o.ElemsResizable() || len(o.Elems) == 0 {
		return Value{}, false
	}
	v := o.Elems[0]
	if v.IsHole() {
		return Value{}, false
	}
	// A hole after the first element moves with the elements when no prototype can have
	// an element.
	if !o.HolesAreAbsent() && ContainsHole(o.Elems) {
		return Value{}, false
	}
	n := len(o.Elems) - 1
	if n < shiftCopyElems {
		copy(o.Elems, o.Elems[1:])
		o.Elems[n] = Value{}
		o.Elems = o.Elems[:n]
	} else {
		o.shiftElem()
	}
	o.Length = uint32(n)
	if o.ElemsOversized() {
		o.Shape.Space.ReallocElems(o)
	}
	return v, true
}

// FillElements is quiet.Context.FillElements.
func (r *RealmPart) FillElements(o *Object, n, start, end int64, v Value) bool {
	if !o.elemsChangeable() || int64(o.Length) != n {
		return false
	}
	// Storing past the stored elements must not skip an index.
	if start < 0 || start > int64(len(o.Elems)) || end > n || !o.HolesAreAbsent() {
		return false
	}
	for i := start; i < end; i++ {
		if r.InterruptFlag.Load() {
			r.Poll()
		}
		if i < int64(len(o.Elems)) {
			o.Elems[i] = v
		} else {
			o.AppendElem(v)
		}
	}
	return true
}

// SearchElements is quiet.Context.SearchElements.
func (r *RealmPart) SearchElements(o *Object, i, end int64, target Value, sameValueZero bool) (found, next int64) {
	// Only strings and BigInts can take long to compare.
	if t := target.O; t != nil && (len(t.Text) > maxQuickCompare || t.Kind == KindBigInt) {
		return r.searchElementsSlow(o, i, end, target)
	}
	eq := IsStrictlyEqual
	if sameValueZero {
		eq = SameValueZero
	}
	elems := o.Elems
	// The runs between two checks for an interruption end at multiples of PollInterval, so that
	// the checks keep their pace when the callers resume a search after a hole.
	if end < i {
		for i > end && i < int64(len(elems)) {
			stop := max(end, (i&^(PollInterval-1))-1)
			for ; i > stop; i-- {
				e := elems[i]
				if e.IsHole() {
					return -1, i
				}
				if eq(e, target) {
					return i, i
				}
			}
			if i > end && i&(PollInterval-1) == PollInterval-1 {
				r.Poll()
			}
		}
		return -1, i
	}
	for i < end && i < int64(len(elems)) {
		stop := min(end, int64(len(elems)), (i|(PollInterval-1))+1)
		for ; i < stop; i++ {
			e := elems[i]
			if e.IsHole() {
				return -1, i
			}
			if eq(e, target) {
				return i, i
			}
		}
		if i < end && i&(PollInterval-1) == 0 {
			r.Poll()
		}
	}
	return -1, i
}

// searchElementsSlow is [RealmPart.SearchElements] for a target that can take long to
// compare. The target is not a number, so both comparisons are IsStrictlyEqual.
func (r *RealmPart) searchElementsSlow(o *Object, i, end int64, target Value) (found, next int64) {
	step := int64(1)
	if end < i {
		step = -1
	}
	elems := o.Elems
	// Each comparison also counts as reading the element.
	n := max(comparedBytes(target), maxQuickCompare)
	var read int
	for ; i != end && i < int64(len(elems)); i += step {
		e := elems[i]
		if e.IsHole() {
			// A caller that resumes after the hole does not count what was read before it.
			r.Poll()
			return -1, i
		}
		if IsStrictlyEqual(e, target) {
			return i, i
		}
		if read += n; read >= compareBudget {
			read = 0
			r.Poll()
		}
	}
	return -1, i
}

// JoinStrings is quiet.Context.JoinStrings.
func (r *RealmPart) JoinStrings(o *Object, length int64, sep wtf8.String) (Value, bool) {
	if o.Kind != KindArray || int64(o.Length) != length || int64(len(o.Elems)) != length || !wtf8.IsASCII(sep) {
		return Value{}, false
	}
	elems := o.Elems
	n := int64(len(sep)) * int64(max(len(elems)-1, 0))
	for i, e := range elems {
		if e.O == nil || e.O.Kind != KindString || !e.IsASCII() {
			return Value{}, false
		}
		if n += int64(len(e.O.Text)); n > MaxStringLength {
			return Value{}, false
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.Poll()
		}
	}
	var b strings.Builder
	b.Grow(int(n))
	var buf [shortstrings.MaxBytes]byte
	for i, e := range elems {
		if i > 0 {
			b.WriteString(string(sep))
		}
		if e.IsShortString() {
			b.Write(e.AppendStringBytes(buf[:0]))
		} else {
			b.WriteString(string(e.O.Str()))
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.Poll()
		}
	}
	v := r.NewString(wtf8.String(b.String()))
	if !v.IsShortString() {
		// A string longer than a short one is a new object.
		v.O.Flags |= ObjASCIIKnown | ObjASCII
	}
	return v, true
}

// ReverseElements is quiet.Context.ReverseElements.
func (r *RealmPart) ReverseElements(o *Object) bool {
	if !o.isDense() {
		return false
	}
	for i, j := 0, len(o.Elems)-1; i < j; i, j = i+1, j-1 {
		r.PollAtIndex(int64(i))
		o.Elems[i], o.Elems[j] = o.Elems[j], o.Elems[i]
	}
	return true
}

// CopyElementRange is quiet.Context.CopyElementRange.
func (r *RealmPart) CopyElementRange(o *Object, start, end int64) (*Object, bool) {
	if !o.elemsComplete() {
		return nil, false
	}
	if start < 0 || end > int64(len(o.Elems)) {
		return nil, false
	}
	var elems []Value
	if start < end {
		elems = o.Elems[start:end]
	}
	if ContainsHole(elems) || !r.ArrayCtorPlain(o) {
		return nil, false
	}
	return r.NewArray(AppendPolled(&r.Poller, nil, elems)), true
}

// SpliceElements is quiet.Context.SpliceElements.
func (r *RealmPart) SpliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	if length != int64(len(o.Elems)) || start < 0 || start > length || del < 0 || del > length-start {
		return nil, false
	}
	if !o.elemsMovable() || !r.ArrayCtorPlain(o) || !r.ArrayProtoPlain() {
		return nil, false
	}
	removed := AppendPolled(&r.Poller, nil, o.Elems[start:start+del])
	n, k := int(length), len(items)
	newLen := n - int(del) + k
	if n-int(start+del) > PollChunk {
		r.Poll()
	}
	elems := o.Elems
	if newLen > n {
		elems = slices.Grow(elems, newLen-n)
		if cap(elems) != cap(o.Elems) {
			o.leaveElems(o.Elems)
		}
	}
	elems = elems[:max(n, newLen)]
	copy(elems[int(start)+k:], elems[start+del:n])
	copy(elems[start:], items)
	// The removed elements stay out of reach in the storage that the elements keep.
	clear(elems[newLen:])
	o.Elems = elems[:newLen]
	o.Length = uint32(newLen)
	if o.ElemsOversized() {
		r.ReallocElems(o)
	}
	return r.NewArray(removed), true
}

// AppendElementsOf is quiet.Context.AppendElementsOf.
func (r *RealmPart) AppendElementsOf(dst *Object, n int64, src *Object) bool {
	if !dst.ElemsResizable() || int64(dst.Length) != n || dst.Shape != r.Shapes.Array {
		return false
	}
	elems, ok := src.PlainElems()
	if !ok {
		return false
	}
	old := dst.Elems
	dst.Elems = AppendPolled(&r.Poller, old, elems)
	if cap(dst.Elems) != cap(old) {
		dst.leaveElems(old)
	}
	dst.Length = uint32(len(dst.Elems))
	return true
}

// SkipHoles is quiet.Context.SkipHoles.
func (r *RealmPart) SkipHoles(o *Object, i, end int64) int64 {
	if i < 0 || i >= end || i < int64(len(o.Elems)) && !o.Elems[i].IsHole() || !o.HolesAreAbsent() {
		return i
	}
	j := end
	for k := i; k < min(end, int64(len(o.Elems))); k++ {
		if !o.Elems[k].IsHole() {
			j = k
			break
		}
	}
	if o.Flags&ObjSparse != 0 {
		idxs := r.sparseIndices.list(o)
		if p, _ := slices.BinarySearch(idxs, uint32(i)); p < len(idxs) && int64(idxs[p]) < j {
			j = int64(idxs[p])
		}
	}
	return j
}

// SkipHolesBackward is quiet.Context.SkipHolesBackward.
func (r *RealmPart) SkipHolesBackward(o *Object, i, end int64) int64 {
	if i <= end || i < 0 || i > MaxArrayIndex || i < int64(len(o.Elems)) && !o.Elems[i].IsHole() || !o.HolesAreAbsent() {
		return i
	}
	j := max(end, -1)
	for k := min(i, int64(len(o.Elems))-1); k > j; k-- {
		if !o.Elems[k].IsHole() {
			j = k
			break
		}
	}
	if o.Flags&ObjSparse != 0 {
		idxs := r.sparseIndices.list(o)
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
