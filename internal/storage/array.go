// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"encoding/binary"
	"math"
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
		shape:  s,
		elems:  elems,
		length: uint32(len(elems)),
	}).Init()
}

type packedNumArray struct {
	PropertyObject
	packedNumElems
}

func (r *RealmPart) newPackedNumArray(nums []float64) *Object {
	a := &packedNumArray{}
	a.packedNumElems.values = nums
	a.PropertyObject.objectTail.Ext = &a.packedNumElems
	o := a.PropertyObject.Init()
	o.Kind = KindArray
	o.Class = ClassArray
	o.shape = r.Shapes.Array
	o.length = uint32(len(nums))
	return o
}

// ArrayCreate implements ArrayCreate.
func (r *RealmPart) ArrayCreate(length uint32, proto *Object) *Object {
	var a *Object
	if length > 0 && length <= MaxPresizedElems {
		// The array is usually filled up to its length afterwards.
		var elems []Value
		a, elems = NewTailObjectWithStorage(int(length))
		a.Kind = KindArray
		a.Class = ClassArray
		a.shape = r.Shapes.Array
		a.InitElems(elems[:0])
	} else {
		a = r.NewArray(nil)
	}
	r.SetProto(a, proto)
	a.length = length
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
		shape:  r.Shapes.Array,
		length: 2,
		pair:   [2]Value{a, b},
	}
	p.elems = p.pair[:]
	return p.Init()
}

// NewTemplateArray returns a new frozen array with the elements vals, which still can be given
// named properties by [Space.Add].
func (r *RealmPart) NewTemplateArray(vals []Value) *Object {
	a := r.NewArray(nil)
	for i, v := range vals {
		r.Add(a, r.IndexName(uint32(i)), v, FlagEnumerable)
	}
	a.length = uint32(len(vals))
	a.flags |= ObjSparse | ObjFrozenLength | ObjNotExtensible
	return a
}

// FreezeLength makes the length of the array o read-only.
func (o *Object) FreezeLength() {
	o.ensureUnpacked()
	o.flags |= ObjFrozenLength
}

// InheritsNoIndex reports whether no prototype of o has a property with an array index, and
// looking for one runs no JavaScript. It says nothing about o itself.
func (o *Object) InheritsNoIndex() bool {
	for h := o.shape.Proto; h != nil; h = h.shape.Proto {
		if h.ElemCount() != 0 || h.flags&(ObjSparse|ObjExotic) != 0 || h.Class != ClassObject && h.Class != ClassArray && h.Class != ClassFunction {
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
	// %Array.prototype% is marked as a prototype at initialization, so its elements stay boxed.
	plain := len(a.elems) == 0 && len(o.elems) == 0 && (a.flags|o.flags)&ObjSparse == 0 && a.shape.Proto == o
	if ValidateExec && plain {
		r.checkArrayProtoPlain()
	}
	return plain
}

// NewElementsPlain reports whether storing a new element in the array o defines it as an own
// data property without consulting a prototype, when o accepts it.
func (r *RealmPart) NewElementsPlain(o *Object) bool {
	return o.shape.Proto == r.Intrinsic(IntrinsicArrayPrototype) && r.ArrayProtoPlain()
}

// elemsComplete reports whether o is an array whose index properties are exactly those in elems
// and whose length is that of elems. There can be holes. It permits reading only.
func (o *Object) elemsComplete() bool {
	return o.Kind == KindArray && int(o.length) == o.ElemCount() && o.flags&ObjSparse == 0
}

// elemsChangeable reports whether o is an array whose index properties are exactly those in elems
// and that accepts changes to its elements and its length, which can be greater than that of elems.
func (o *Object) elemsChangeable() bool {
	return o.Kind == KindArray && o.flags&(ObjSparse|ObjNotExtensible|ObjFrozenLength) == 0
}

// ElemsResizable is elemsComplete and elemsChangeable.
func (o *Object) ElemsResizable() bool {
	if o.Kind != KindArray || o.flags&(ObjSparse|ObjNotExtensible|ObjFrozenLength) != 0 {
		return false
	}
	if int(o.length) == len(o.elems) {
		return true
	}
	p, ok := o.Ext.(*packedNumElems)
	return ok && int(o.length) == len(p.values)
}

// PlainElems returns the elements of o, which the caller must not modify, when o is an array
// whose elements can be read without running any JavaScript. o may be nil.
func (o *Object) PlainElems() ([]Value, bool) {
	if o == nil || !o.elemsComplete() || containsHole(o.elems) {
		return nil, false
	}
	o.ensureUnpacked()
	return o.elems, true
}

// isDense reports whether o is an array whose elements are all present in elems
// and that accepts changes to its elements and its length.
func (o *Object) isDense() bool {
	return o.ElemsResizable() && !containsHole(o.elems)
}

// elemsMovable reports whether moving the elements of the array o by index, holes included,
// does what moving them one by one with HasProperty, Set and DeletePropertyOrThrow does, and
// whether o accepts changes to its elements and its length.
func (o *Object) elemsMovable() bool {
	return o.ElemsResizable() && (o.InheritsNoIndex() || !containsHole(o.elems))
}

// containsHole reports whether elems has a hole.
func containsHole(elems []Value) bool {
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
	if i >= 0 && i < int64(len(o.elems)) {
		if e := o.elems[i]; !e.IsHole() {
			return e, true
		}
	}
	if nums := o.numericElements(); uint64(i) < uint64(len(nums)) {
		return Number(nums[i]), true
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
// Element returns that property, o is not a typed array and its elements are not shared
// (see [SharedElems]), and reports whether it did.
func (o *Object) SetElement(i int64, v Value) bool {
	if uint64(i) < uint64(len(o.numericElements())) {
		o.storeElem(uint32(i), v)
		return true
	}
	if i >= 0 && i < int64(len(o.elems)) && !o.elems[i].IsHole() && o.Pinned&SharedElems == 0 {
		o.elems[i] = v
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
	return v.O.ElemCount()
}

// ElementList returns a copy of the elements of the array o
// when reading them cannot run any JavaScript.
func (o *Object) ElementList() ([]Value, bool) {
	if o == nil || !o.elemsComplete() || containsHole(o.elems) {
		return nil, false
	}
	if o.packedNums() != nil {
		return o.shape.Space.CopyDenseElements(o)
	}
	return append([]Value(nil), o.elems...), true
}

type arrayCtorCache struct {
	protoShape  *Shape
	ctorShape   *Shape
	ctorSlot    int32
	speciesSlot int32
}

// ArrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func (r *RealmPart) ArrayCtorPlain(o *Object) bool {
	if o.shape != r.Shapes.Array {
		return false
	}
	proto := r.Intrinsic(IntrinsicArrayPrototype)
	ctor := r.Intrinsic(IntrinsicArray)
	if c := &r.arrayCtor; c.protoShape == proto.shape && c.ctorShape == ctor.shape {
		// Values can change without changing either shape.
		return proto.slots[c.ctorSlot].O == ctor &&
			ctor.slots[c.speciesSlot].Accessor().Getter == r.Intrinsic(IntrinsicArraySpeciesGetter)
	}
	c := r.LookupDataSlot(proto, r.Key(KeyConstructor))
	if c.Holder == nil || c.Value().O != ctor {
		return false
	}
	p, found := ctor.getOwn(r.WellKnownSymbol(SymbolSpecies))
	if !found || p.Flags&FlagAccessor == 0 || ctor.slots[p.Slot].Accessor().Getter != r.Intrinsic(IntrinsicArraySpeciesGetter) {
		return false
	}
	if c.Holder == proto {
		r.arrayCtor = arrayCtorCache{
			protoShape:  proto.shape,
			ctorShape:   ctor.shape,
			ctorSlot:    c.slot,
			speciesSlot: p.Slot,
		}
	}
	return true
}

// AppendElements is quiet.Context.AppendElements.
func (r *RealmPart) AppendElements(o *Object, vals []Value) (uint32, bool) {
	if !o.ElemsResizable() || !r.NewElementsPlain(o) {
		return 0, false
	}
	n := len(vals)
	if n == 0 {
		return o.length, true
	}
	if n == 1 && len(o.elems) == cap(o.elems) {
		o.AppendElem(vals[0])
	} else {
		if cap(o.elems)-len(o.elems) < n {
			o.ReserveElems(n)
		}
		o.elems = append(o.elems, vals...)
	}
	o.length += uint32(n)
	return o.length, true
}

// PrependElements is quiet.Context.PrependElements.
func (r *RealmPart) PrependElements(o *Object, vals []Value) (uint32, bool) {
	if !o.elemsMovable() || !r.NewElementsPlain(o) {
		return 0, false
	}
	o.ensureUnpacked()
	n, k := len(o.elems), len(vals)
	if n > PollChunk {
		r.Poll()
	}
	if cap(o.elems)-n >= k {
		o.elems = o.elems[:n+k]
		copy(o.elems[k:], o.elems[:n])
	} else {
		elems := make([]Value, n+k, n+k+max((n+k)/2, MinElems))
		copy(elems[k:], o.elems)
		o.leaveElems(o.elems)
		o.elems = elems
	}
	copy(o.elems, vals)
	o.length = uint32(len(o.elems))
	return o.length, true
}

// RemoveLastElement removes the last element of the array o and returns it.
// It reports false and does nothing when o is empty or needs the generic algorithm.
func (o *Object) RemoveLastElement() (Value, bool) {
	if !o.ElemsResizable() || o.ElemCount() == 0 {
		return Value{}, false
	}
	if p := o.packedNums(); p != nil {
		n := len(p.values) - 1
		v := Number(p.values[n])
		p.values = p.values[:n]
		o.length = uint32(n)
		if o.elemsOversized() {
			o.shape.Space.reallocElems(o)
		}
		return v, true
	}
	elems := o.elems
	// Holes before the last element stay holes after it is removed.
	n := len(elems) - 1
	v := elems[n]
	if v.IsHole() {
		return Value{}, false
	}
	if o.Pinned&SharedElems != 0 {
		// The call removes the last of the copied elements, which are the array's own.
		o.copySharedElems()
		return o.RemoveLastElement()
	}
	o.removeLastElem(elems)
	o.length = uint32(n)
	if o.elemsOversized() {
		o.shape.Space.reallocElems(o)
	}
	return v, true
}

// RemoveFirstElement removes the first element of the array o and returns it.
// It reports false and does nothing when o is empty or needs the generic algorithm.
func (o *Object) RemoveFirstElement() (Value, bool) {
	if !o.ElemsResizable() || o.ElemCount() == 0 {
		return Value{}, false
	}
	if p := o.packedNums(); p != nil {
		n := len(p.values) - 1
		v := Number(p.values[0])
		if n < shiftCopyElems {
			copy(p.values, p.values[1:])
			p.values = p.values[:n]
		} else {
			o.recordShift(cap(p.values))
			p.values = p.values[1:]
		}
		o.length = uint32(n)
		if o.elemsOversized() {
			o.shape.Space.reallocElems(o)
		}
		return v, true
	}
	v := o.elems[0]
	if v.IsHole() {
		return Value{}, false
	}
	// A hole after the first element moves with the elements when no prototype can have
	// an element.
	if !o.HolesAreAbsent() && containsHole(o.elems) {
		return Value{}, false
	}
	o.ownElems()
	n := len(o.elems) - 1
	if n < shiftCopyElems {
		copy(o.elems, o.elems[1:])
		o.elems[n] = Value{}
		o.elems = o.elems[:n]
	} else {
		o.shiftElem()
	}
	o.length = uint32(n)
	if o.elemsOversized() {
		o.shape.Space.reallocElems(o)
	}
	return v, true
}

// FillElements is quiet.Context.FillElements.
func (r *RealmPart) FillElements(o *Object, n, start, end int64, v Value) bool {
	if !o.elemsChangeable() || int64(o.length) != n {
		return false
	}
	// Storing past the stored elements must not skip an index.
	if start < 0 || start > int64(o.ElemCount()) || end > n || !o.HolesAreAbsent() {
		return false
	}
	if start >= end {
		return true
	}
	if o.packedNums() == nil || v.O != NumberObject {
		o.ownElems()
	}
	for i := start; i < end; i++ {
		if r.InterruptFlag.Load() {
			r.Poll()
		}
		if i < int64(len(o.elems)) {
			o.elems[i] = v
		} else if nums := o.numericElements(); i < int64(len(nums)) {
			nums[i] = v.N
		} else {
			o.AppendElem(v)
		}
	}
	return true
}

// SearchElements is quiet.Context.SearchElements.
func (r *RealmPart) SearchElements(o *Object, i, end int64, target Value, sameValueZero bool) (found, next int64) {
	if nums := o.numericElements(); nums != nil {
		return r.searchNums(nums, i, end, target, sameValueZero)
	}
	// Only strings and BigInts can take long to compare.
	if t := target.O; t != nil && t.Kind < KindFirstObject && (len(t.Text()) > maxQuickCompare || t.Kind == KindBigInt) {
		return r.searchElementsSlow(o, i, end, target)
	}
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

func (r *RealmPart) searchNums(nums []float64, i, end int64, target Value, sameValueZero bool) (int64, int64) {
	step := int64(1)
	if end < i {
		step = -1
	}
	isNumber := target.O == NumberObject
	isNaN := sameValueZero && math.IsNaN(target.N)
	for ; i != end && uint64(i) < uint64(len(nums)); i += step {
		n := nums[i]
		if isNumber && (n == target.N || isNaN && math.IsNaN(n)) {
			return i, i
		}
		if (i+step)&(PollInterval-1) == 0 {
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
	elems := o.elems
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
	if o.Kind != KindArray || int64(o.length) != length || int64(len(o.elems)) != length || !wtf8.IsASCII(sep) {
		return Value{}, false
	}
	elems := o.elems
	n := int64(len(sep)) * int64(max(len(elems)-1, 0))
	var short bool
	for i, e := range elems {
		if e.O == nil || e.O.Kind != KindString {
			return Value{}, false
		}
		// The bits of a short string that is not interned tell whether it is ASCII.
		m := len(e.O.Text())
		short = short || m <= shortstrings.MaxBytes
		if m <= shortstrings.MaxBytes && e.O.flags&ObjInterned == 0 {
			if !shortstrings.IsASCII(math.Float64bits(e.N)) {
				return Value{}, false
			}
		} else if !e.IsASCII() {
			return Value{}, false
		}
		if n += int64(m); n > MaxStringLength {
			return Value{}, false
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.Poll()
		}
	}
	if short {
		return r.joinShort(elems, sep, n), true
	}
	var b strings.Builder
	b.Grow(int(n))
	for i, e := range elems {
		if i > 0 {
			b.WriteString(string(sep))
		}
		b.WriteString(string(e.O.Str()))
		if i&(PollInterval-1) == PollInterval-1 {
			r.Poll()
		}
	}
	return r.newJoined(b.String()), true
}

// joinShort is JoinStrings for elements some of which are short strings. n is the byte length
// of the result.
func (r *RealmPart) joinShort(elems []Value, sep wtf8.String, n int64) Value {
	var w joinWriter
	w.sb.Grow(int(n))
	var sepData uint64
	if len(sep) <= shortstrings.MaxBytes {
		sepData = shortstrings.Pack(sep)
	}
	for i, e := range elems {
		if i > 0 && len(sep) > 0 {
			if len(sep) <= shortstrings.MaxBytes {
				w.writePacked(sepData, len(sep))
			} else {
				if w.k > 0 {
					w.flush()
				}
				w.sb.WriteString(string(sep))
			}
		}
		if m := len(e.O.Text()); m <= shortstrings.MaxBytes {
			w.writePacked(math.Float64bits(e.N), m)
		} else {
			if w.k > 0 {
				w.flush()
			}
			w.sb.WriteString(string(e.O.Str()))
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.Poll()
		}
	}
	w.flush()
	return r.newJoined(w.sb.String())
}

// newJoined returns the ASCII string s that JoinStrings joined.
func (r *RealmPart) newJoined(s string) Value {
	v := r.NewString(wtf8.String(s))
	if !v.IsShortString() {
		// A string longer than a short one is a new object.
		v.O.flags |= ObjASCIIKnown | ObjASCII
	}
	return v
}

// joinChunk is the number of bytes of packed strings that a joinWriter collects before it
// writes them to its builder.
const joinChunk = 128

// joinWriter writes the parts of a join to sb, collecting packed strings in chunk. A longer part
// goes to sb after a flush of chunk.
type joinWriter struct {
	sb strings.Builder

	// k is the number of bytes in chunk. It stays below joinChunk between writes, so that a
	// packed string's eight bytes always fit.
	k     int
	chunk [joinChunk + shortstrings.MaxBytes]byte
}

// writePacked writes the packed string data whose byte length is n.
func (w *joinWriter) writePacked(data uint64, n int) {
	binary.LittleEndian.PutUint64(w.chunk[w.k:], data)
	w.k += n
	if w.k >= joinChunk {
		w.flush()
	}
}

// flush writes the collected bytes to sb.
func (w *joinWriter) flush() {
	w.sb.Write(w.chunk[:w.k])
	w.k = 0
}

// ReverseElements is quiet.Context.ReverseElements.
func (r *RealmPart) ReverseElements(o *Object) bool {
	if !o.isDense() {
		return false
	}
	if nums := o.numericElements(); nums != nil {
		for i, j := 0, len(nums)-1; i < j; i, j = i+1, j-1 {
			r.PollAtIndex(int64(i))
			nums[i], nums[j] = nums[j], nums[i]
		}
		return true
	}
	o.ownElems()
	for i, j := 0, len(o.elems)-1; i < j; i, j = i+1, j-1 {
		r.PollAtIndex(int64(i))
		o.elems[i], o.elems[j] = o.elems[j], o.elems[i]
	}
	return true
}

// CopyElementRange is quiet.Context.CopyElementRange.
func (r *RealmPart) CopyElementRange(o *Object, start, end int64) (*Object, bool) {
	if !o.elemsComplete() {
		return nil, false
	}
	if start < 0 || end > int64(o.ElemCount()) {
		return nil, false
	}
	if nums := o.numericElements(); nums != nil {
		if !r.ArrayCtorPlain(o) {
			return nil, false
		}
		if start >= end {
			return r.NewArray(nil), true
		}
		return r.newPackedNumArray(appendPolled(&r.Poller, nil, nums[start:end])), true
	}
	var elems []Value
	if start < end {
		elems = o.elems[start:end]
	}
	if containsHole(elems) || !r.ArrayCtorPlain(o) {
		return nil, false
	}
	return r.NewArray(appendPolled(&r.Poller, nil, elems)), true
}

// SpliceElements is quiet.Context.SpliceElements.
func (r *RealmPart) SpliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	if length != int64(o.ElemCount()) || start < 0 || start > length || del < 0 || del > length-start {
		return nil, false
	}
	if !o.elemsMovable() || !r.ArrayCtorPlain(o) || !r.ArrayProtoPlain() {
		return nil, false
	}
	o.ensureUnpacked()
	removed := appendPolled(&r.Poller, nil, o.elems[start:start+del])
	n, k := int(length), len(items)
	newLen := n - int(del) + k
	if n-int(start+del) > PollChunk {
		r.Poll()
	}
	o.ownElems()
	elems := o.elems
	if newLen > n {
		elems = slices.Grow(elems, newLen-n)
		if cap(elems) != cap(o.elems) {
			o.leaveElems(o.elems)
		}
	}
	elems = elems[:max(n, newLen)]
	copy(elems[int(start)+k:], elems[start+del:n])
	copy(elems[start:], items)
	// The storage that the elements keep still holds the values past the new length.
	if tail := elems[newLen:]; len(tail) == 1 {
		clearLeft(&tail[0])
	} else {
		clear(tail)
	}
	o.elems = elems[:newLen]
	o.length = uint32(newLen)
	if o.elemsOversized() {
		r.reallocElems(o)
	}
	return r.NewArray(removed), true
}

// ReserveElementsForIteration gives dst room to collect the elements of src.
func (r *RealmPart) ReserveElementsForIteration(dst, src *Object) {
	n := src.ElemCount()
	if dst.packedNums() != nil {
		// Appending grows the numeric storage or unpacks it according to the values read.
		return
	}
	if n == 2*packElemsThreshold && n > dst.ElemRoom() && dst.ElemCount() == 0 && dst.flags == 0 &&
		dst.Kind == KindArray && src.packedNums() != nil && src.elemsComplete() && r.ArrayIterationPlain(src) {
		dst.packElems(newPackedNumBlock(0, n))
		return
	}
	dst.ReserveElems(n)
}

// AppendElementsOf is quiet.Context.AppendElementsOf.
func (r *RealmPart) AppendElementsOf(dst *Object, n int64, src *Object) bool {
	if !dst.ElemsResizable() || int64(dst.length) != n || dst.shape != r.Shapes.Array {
		return false
	}
	if nums := src.numericElements(); nums != nil && src.elemsComplete() {
		if dstPacked := dst.packedNums(); dstPacked != nil {
			dstPacked.values = appendPolled(&r.Poller, dstPacked.values, nums)
			dst.length = uint32(len(dstPacked.values))
			return true
		}
		if n == 0 && dst.flags == 0 {
			if len(nums) != 0 {
				dst.packElems(&packedNumElems{
					values: appendPolled(&r.Poller, nil, nums),
				})
				dst.length = uint32(len(nums))
			}
			return true
		}
	}
	elems, ok := src.PlainElems()
	if !ok {
		return false
	}
	dst.ensureUnpacked()
	old := dst.elems
	dst.elems = appendPolled(&r.Poller, old, elems)
	if cap(dst.elems) != cap(old) {
		dst.leaveElems(old)
	}
	dst.length = uint32(len(dst.elems))
	return true
}

// SkipHoles is quiet.Context.SkipHoles.
func (r *RealmPart) SkipHoles(o *Object, i, end int64) int64 {
	elems := o.elems
	if i < 0 || i >= end || i < int64(len(elems)) && !elems[i].IsHole() || i < int64(len(o.numericElements())) {
		return i
	}
	// The callers check for an interruption only at some of the elements, which holes may separate,
	// and the prototype chain can be long.
	if r.InterruptFlag.Load() {
		r.Poll()
	}
	if !o.HolesAreAbsent() {
		return i
	}
	if o.flags&ObjSparse != 0 {
		// The elements are looked at only up to the next index in the shape, so that a walk over
		// those indices looks at each element once.
		idxs := r.sparseIndices.list(o)
		if p, _ := slices.BinarySearch(idxs, uint32(i)); p < len(idxs) {
			end = min(end, int64(idxs[p]))
		}
	}
	j, n := end, min(end, int64(len(elems)))
	if stop := i + PollInterval; n > stop {
		// The caller continues from there.
		j, n = stop, stop
	}
	for k := i; k < n; k++ {
		if !elems[k].IsHole() {
			return k
		}
	}
	return j
}

// SkipHolesBackward is quiet.Context.SkipHolesBackward.
func (r *RealmPart) SkipHolesBackward(o *Object, i, end int64) int64 {
	elems := o.elems
	if i <= end || i < 0 || i > MaxArrayIndex || i < int64(len(elems)) && !elems[i].IsHole() || i < int64(len(o.numericElements())) {
		return i
	}
	// See [RealmPart.SkipHoles].
	if r.InterruptFlag.Load() {
		r.Poll()
	}
	if !o.HolesAreAbsent() {
		return i
	}
	if o.flags&ObjSparse != 0 {
		// The elements are looked at only down to the next index in the shape, as in SkipHoles.
		idxs := r.sparseIndices.list(o)
		p, found := slices.BinarySearch(idxs, uint32(i))
		if found {
			p++
		}
		if p > 0 {
			end = max(end, int64(idxs[p-1]))
		}
	}
	if nums := o.numericElements(); nums != nil {
		return max(end, int64(len(nums))-1)
	}
	k := min(i, int64(len(elems))-1)
	j := max(end, k-PollInterval, -1)
	for ; k > j; k-- {
		if !elems[k].IsHole() {
			return k
		}
	}
	return j
}

// ElemCount returns how many elements o holds in memory, holes included.
func (o *Object) ElemCount() int {
	if n := len(o.elems); n != 0 {
		return n
	}
	if p, ok := o.Ext.(*packedNumElems); ok {
		return len(p.values)
	}
	return 0
}

// ElemRoom returns how many elements the storage of the elements of o has room for.
func (o *Object) ElemRoom() int {
	if room := cap(o.elems); room != 0 {
		return room
	}
	if p, ok := o.Ext.(*packedNumElems); ok {
		return cap(p.values)
	}
	return 0
}

// ElementAt returns the element i of o, and reports whether o holds it.
func (o *Object) ElementAt(i uint32) (Value, bool) {
	if int64(i) < int64(len(o.elems)) {
		if e := o.elems[i]; !e.IsHole() {
			return e, true
		}
	}
	if nums := o.numericElements(); uint(i) < uint(len(nums)) {
		return Number(nums[i]), true
	}
	return Value{}, false
}

// HasElementAt reports whether o, which must have property storage, holds the element i.
func (o *Object) HasElementAt(i uint32) bool {
	return int64(i) < int64(len(o.elems)) && !o.elems[i].IsHole() || uint(i) < uint(len(o.numericElements()))
}

// StoreElement stores v, which can be a hole, as the element i of o. HasElementAt must report true
// for i.
func (o *Object) StoreElement(i uint32, v Value) {
	if ValidateExec && !o.HasElementAt(i) {
		panic("storage: StoreElement of an element that does not exist")
	}
	o.storeElem(i, v)
}

func (o *Object) storeElem(i uint32, v Value) {
	if p := o.packedNums(); p != nil && v.O == NumberObject {
		p.values[i] = v.N
		return
	}
	o.ownElems()
	o.elems[i] = v
}

// HasElement reports whether o holds an element, checking for an interruption while it looks at
// many.
func (o *Object) HasElement() bool {
	if len(o.numericElements()) != 0 {
		return true
	}
	for i, e := range o.elems {
		if !e.IsHole() {
			return true
		}
		if i%PollInterval == PollInterval-1 {
			o.shape.Space.Poll()
		}
	}
	return false
}

// HasHole reports whether one of the elements of o is a hole.
func (o *Object) HasHole() bool {
	return containsHole(o.elems)
}

// PushElement appends v to the elements of the array o and sets its length.
func (o *Object) PushElement(v Value) {
	o.AppendElem(v)
	o.length = uint32(o.ElemCount())
}

// CopyDenseElements returns a copy of the elements of the array o when none of them is missing.
func (p *Poller) CopyDenseElements(o *Object) ([]Value, bool) {
	if nums := o.numericElements(); nums != nil && uint64(o.length) <= uint64(len(nums)) {
		elems := make([]Value, o.length)
		boxNums(p, elems, nums[:o.length])
		return elems, true
	}
	if n := o.length; uint64(n) <= uint64(len(o.elems)) && !containsHole(o.elems[:n]) {
		return appendPolled(p, nil, o.elems[:n]), true
	}
	return nil, false
}

// TruncateElements removes the elements of the array o from n on and sets its length to n.
func (p *Poller) TruncateElements(o *Object, n uint32) {
	if packed := o.packedNums(); packed != nil {
		if uint64(n) < uint64(len(packed.values)) {
			removed := len(packed.values) - int(n)
			if removed >= PollChunk {
				p.Poll()
			}
			packed.values = packed.values[:n]
			o.length = n
			if o.elemsOversized() {
				p.reallocElems(o)
			} else if removed > PollChunk {
				o.clipElems()
			}
		}
		o.length = n
		return
	}
	if int64(n) < int64(len(o.elems)) {
		o.ownElems()
		tail := o.elems[n:]
		o.elems = o.elems[:n]
		oversized := o.elemsOversized()
		if len(tail) > PollChunk {
			// The tail, which may be left uncleared, stays out of reach of later growth.
			o.clipElems()
		}
		o.length = n
		if oversized {
			// The tail goes away with the storage.
			p.reallocElems(o)
		} else if len(tail) == 1 {
			clearLeft(&tail[0])
		} else {
			clearPolled(p, tail)
		}
	}
	o.length = n
}

// AddMappedElement adds the element i of the arguments object o, which it holds, as an index
// property that maps to a parameter.
func (r *RealmPart) AddMappedElement(o *Object, i uint32) {
	// A mapped property stays out of elems, so that every access to it takes a path that consults the map.
	r.Add(o, r.IndexName(i), Undefined, FlagsData)
	o.elems[i] = Hole
	o.flags |= ObjSparse
}

// NewArrayCopy returns a new array with a copy of elems as its elements.
func (r *RealmPart) NewArrayCopy(elems []Value) *Object {
	a, values := NewTailObjectWithStorage(len(elems))
	a.Kind = KindArray
	a.Class = ClassArray
	a.shape = r.Shapes.Array
	a.elems = values
	copy(a.elems, elems)
	a.length = uint32(len(elems))
	return a
}

// NewArrayOfConstants returns a new array of the constants in run, the linked constants of an
// array literal, which no other code uses. The first call stores an array of them in run[0] and
// clears the rest of run. The returned arrays share its elements until their own change.
func (r *RealmPart) NewArrayOfConstants(run []Value) *Object {
	// A constant is never an array.
	t := run[0].O
	if t == nil || t.Kind != KindArray {
		// The arrays keep only t and its elements reachable, not all the constants of the
		// function.
		t = r.NewArrayCopy(run)
		run[0] = Value{O: t}
		clear(run[1:])
	}
	a := (&PropertyObject{
		Kind:   KindArray,
		Class:  ClassArray,
		Pinned: SharedElems,
		shape:  r.Shapes.Array,
		length: t.length,
	}).Init()
	a.InitElems(slices.Clip(t.elems))
	return a
}

// NewEmptyArrayWithRoom returns a new empty array with room for n elements.
func (r *RealmPart) NewEmptyArrayWithRoom(n int) *Object {
	a, values := NewTailObjectWithStorage(n)
	a.Kind = KindArray
	a.Class = ClassArray
	a.shape = r.Shapes.Array
	a.InitElems(values[:0])
	return a
}

// NewArrayWithRoom returns an array of length undefined elements with room for n named properties.
func (r *RealmPart) NewArrayWithRoom(length, n int) *Object {
	var a *Object
	var values []Value
	if length+n <= MaxInlineObjectValues {
		a, values = NewTailObjectWithStorage(length + n)
		a.InitElems(values[:length:length])
		values = values[length:]
	} else {
		// Keep the named properties with the object so they cannot retain the elements
		// after the array shrinks.
		a, values = NewTailObjectWithStorage(n)
		a.InitElems(make([]Value, length))
		a.Pinned &^= PinnedElems
	}
	a.Kind = KindArray
	a.Class = ClassArray
	a.shape = r.Shapes.Array
	a.length = uint32(length)
	a.InitSlots(values[:0])
	return a
}

// NewObjectFromProperties returns a new ordinary object whose prototype is Object.prototype,
// with the data properties keys[i]: vals[i] created in order as CreateDataProperty creates them.
// A repeated key keeps its first position and takes the last value. It does not keep the slices.
func (r *RealmPart) NewObjectFromProperties(keys []PropKey, vals []Value) *Object {
	o, slots := NewObjectWithStorage(len(vals))
	o.Kind = KindObject
	o.shape = r.Shapes.Object
	o.InitSlots(slots[:0])
	for i, k := range keys {
		v := vals[i]
		if k.Key == nil {
			if int64(k.Idx) < int64(len(o.elems)) && !o.elems[k.Idx].IsHole() {
				o.elems[k.Idx] = v
				continue
			}
			if o.flags&ObjSparse != 0 {
				if key := r.IndexKey(k.Idx); key != nil {
					if _, ok := o.shape.Lookup(key); ok {
						r.Define(o, key, v, FlagsData)
						continue
					}
				}
			}
			r.AddIndex(o, k.Idx, v, FlagsData)
			continue
		}
		// A transition exists only for a key that the shape does not have.
		if s := o.shape; !s.Private {
			if t := s.Transitions.Get(ShapeKey{
				Key:   k.Key,
				Flags: FlagsData,
			}); t != nil {
				o.shape = t
				o.AppendSlot(v)
				continue
			}
		}
		r.Define(o, k.Key, v, FlagsData)
	}
	return o
}
