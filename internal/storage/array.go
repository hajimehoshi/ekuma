// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math/bits"
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

// sparseIndexCache holds the array indices that the keys of one shape contain.
type sparseIndexCache struct {
	// shape and count identify the shape whose array index keys indices holds in ascending
	// order. A private shape only grows in place, so its count tells whether the keys changed.
	shape   *Shape
	count   int32
	indices []uint32
}

// list returns the array indices that o stores in its shape, in ascending order.
// The result must not be modified.
func (c *sparseIndexCache) list(o *Object) []uint32 {
	s := o.Shape
	if c.shape == s && c.count == s.Count {
		if ValidateExec {
			c.checkSparseIndices(s)
		}
		return c.indices
	}
	// addedKeys appends at most maxSparseIndexInserts keys, so buf holds them all.
	var buf [maxSparseIndexInserts]*Object
	if added, ok := c.addedKeys(s, buf[:0]); ok {
		idxs := c.indices
		for _, k := range added {
			if k.Kind != KindString {
				continue
			}
			if i, ok := StringIndex(k.Text); ok {
				if p, found := slices.BinarySearch(idxs, i); !found {
					idxs = slices.Insert(idxs, p, i)
				}
			}
		}
		c.shape = s
		c.count = s.Count
		c.indices = idxs
		return idxs
	}
	idxs := c.indices[:0]
	for _, k := range s.Keys() {
		if k.Kind != KindString {
			continue
		}
		if i, ok := StringIndex(k.Text); ok {
			idxs = append(idxs, i)
		}
	}
	slices.Sort(idxs)
	c.shape = s
	c.count = s.Count
	c.indices = idxs
	return idxs
}

// remove updates the cache for the deletion of key from the shape old, which made the shape s.
func (c *sparseIndexCache) remove(old, s *Shape, key *Object) {
	if c.shape != old || c.count != old.Count {
		return
	}
	if key.Kind == KindString {
		if i, ok := StringIndex(key.Text); ok {
			if p, found := slices.BinarySearch(c.indices, i); found {
				c.indices = slices.Delete(c.indices, p, p+1)
			}
		}
	}
	c.shape = s
	c.count = s.Count
}

// maxSparseIndexInserts is the number of new keys above which sparseIndexCache.list rebuilds
// the list instead of inserting each one.
const maxSparseIndexInserts = 16

// addedKeys returns the keys that s has in addition to those of the cached shape, appended to
// buf, and reports whether s has all of those keys in the same slots and they are at most
// maxSparseIndexInserts.
func (c *sparseIndexCache) addedKeys(s *Shape, buf []*Object) ([]*Object, bool) {
	cached, n := c.shape, c.count
	if cached == nil || s.Count < n {
		return nil, false
	}
	if s.Private {
		// A private shape only grows in place.
		if cached != s || s.Count-n > maxSparseIndexInserts {
			return nil, false
		}
		// Without deletions since, the added keys are the last ones.
		return s.Order[len(s.Order)-int(s.Count-n):], true
	}
	if cached.Private {
		return nil, false
	}
	if s.Count-n > maxSparseIndexInserts {
		return nil, false
	}
	added := buf
	t := s
	for ; t.Count > n; t = t.Parent {
		if t.Parent == nil {
			return nil, false
		}
		added = append(added, t.Key)
	}
	if t != cached {
		return nil, false
	}
	return added, true
}

// growElems reports whether storing the index i should extend elems.
func growElems(elems []Value, i uint32) bool {
	n := int64(len(elems))
	return int64(i) < int64(cap(elems)) || int64(i) <= n+n/2+16
}

// AddIndex creates the data property with the array index i on o, which has no such own property.
// It reports false when o does not allow it.
func (r *RealmPart) AddIndex(o *Object, i uint32, val Value, flags uint8) bool {
	if o.Flags&ObjNotExtensible != 0 {
		return false
	}
	if o.Kind == KindArray && i >= o.Length {
		if o.Flags&ObjFrozenLength != 0 {
			return false
		}
		o.Length = i + 1
	}
	n := len(o.Elems)
	if flags == FlagsData {
		if uint(i) < uint(n) {
			o.Elems[i] = val
			return true
		}
		if growElems(o.Elems, i) {
			o.ReserveElems(int(i) + 1 - n)
			for uint32(len(o.Elems)) < i {
				o.pushElem(Hole)
			}
			o.pushElem(val)
			// The elements may have reached the indices in the shape. Checking only when their
			// number passes a power of two bounds the cost of the checks by that of the growth.
			if o.Flags&ObjSparse != 0 && bits.Len(uint(n)) != bits.Len(uint(len(o.Elems))) {
				r.densify(o)
			}
			return true
		}
	}
	o.Flags |= ObjSparse
	r.Add(o, r.IndexName(i), val, flags)
	// Checking only when the number of properties in the shape reaches a power of two bounds
	// the cost of the checks by that of the additions. The elements would need more than i
	// slots.
	if c := int64(o.Shape.Count); flags == FlagsData && c&(c-1) == 0 && (int64(len(o.Elems))+c)*denseFactor > int64(i) {
		r.densify(o)
	}
	return true
}

// densify moves the array index properties of o from its shape to elems, and clears ObjSparse,
// when they are all writable, enumerable and configurable data properties and elems would have
// at most denseFactor slots per element.
func (r *RealmPart) densify(o *Object) {
	if o.Kind != KindArray && o.Class != ClassObject || o.Flags&(ObjExotic|ObjNotExtensible) != 0 {
		return
	}
	// The new shape adds the named properties to root, which keeps its own properties, if any,
	// in the first base slots.
	s := o.Shape
	var root *Shape
	var base int
	if s.Private {
		root = r.RootShape(s.Proto, s.Realm, s.Family)
	} else {
		root = s
		for root.Parent != nil {
			root = root.Parent
		}
		base = int(root.Count)
	}
	keys := s.Keys()
	var count int64
	end := int64(len(o.Elems))
	for j, k := range keys {
		if k.Kind != KindString {
			continue
		}
		i, ok := StringIndex(k.Text)
		if !ok {
			continue
		}
		if j < base {
			return
		}
		if p, _ := s.Lookup(k); p.Flags != FlagsData {
			return
		}
		count++
		end = max(end, int64(i)+1)
	}
	if (int64(len(o.Elems))+count)*denseFactor < end {
		return
	}
	present := count
	for _, e := range o.Elems {
		if !e.IsHole() {
			present++
		}
	}
	if present*denseFactor < end {
		return
	}

	o.ReserveElems(int(end) - len(o.Elems))
	for int64(len(o.Elems)) < end {
		o.pushElem(Hole)
	}
	o.Shape = root
	slots := o.detachSlots(base, len(o.Slots)-int(count))
	for _, k := range keys[base:] {
		p, _ := s.Lookup(k)
		if k.Kind == KindString {
			if i, ok := StringIndex(k.Text); ok {
				o.Elems[i] = slots[p.Slot]
				continue
			}
		}
		r.Add(o, k, slots[p.Slot], p.Flags)
	}
	o.leaveSlots(slots)
	o.Flags &^= ObjSparse
	if o.Kind != KindArray {
		// The shape may be one that o had before, with other slots. The inline caches of
		// global variables point into the slots of the global object while it keeps its
		// shape, so o gets a new one.
		o.MakePrivate()
	}
	r.TouchProto(o)
}

// ReplaceOwn stores the value and attributes of the existing own property k of o.
func (r *RealmPart) ReplaceOwn(o *Object, k PropKey, val Value, flags uint8) {
	name := k.Key
	if name == nil {
		if int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole() {
			if flags == FlagsData {
				o.Elems[k.Idx] = val
				return
			}
			// The property no longer fits in elems.
			o.Elems[k.Idx] = Hole
			o.Flags |= ObjSparse
			r.Add(o, r.IndexName(k.Idx), val, flags)
			return
		}
		name = r.IndexName(k.Idx)
	}
	r.Define(o, name, val, flags)
}

// arrayIterCache is what ArrayIterationPlain found for arrays with the initial shape.
type arrayIterCache struct {
	// iter and next are the data properties that iterating such an array reads.
	iter dataSlot
	next dataSlot

	// Epoch is the value of agent.ProtoEpoch at which iter and next were found.
	Epoch   uint32
	checked bool
	// found reports whether iter and next were found as data properties.
	found bool
	// NoReturn reports whether array iterators had no return method at epoch.
	NoReturn bool
}

// ArrayIterationPlain reports whether o uses the original array values and next methods.
// Reading an element can still invoke a getter.
func (r *RealmPart) ArrayIterationPlain(o *Object) bool {
	if ValidateExec {
		r.checkPlainArray(o)
	}
	return o.Shape == r.Shapes.Array && r.ArrayIterationUnmodified(r)
}

// ArrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (sp *Space) ArrayIterationUnmodified(r *RealmPart) bool {
	c := &r.ArrayIter
	if c.Epoch != sp.ProtoEpoch || !c.checked {
		c.checked = true
		c.Epoch = sp.ProtoEpoch
		c.iter = r.LookupDataSlot(r.Intrinsics[IntrinsicArrayPrototype], sp.WellKnownSymbol(SymbolIterator))
		c.next = r.LookupDataSlot(r.Intrinsics[IntrinsicArrayIteratorPrototype], sp.Key(KeyNext))
		c.found = c.iter.Holder != nil && c.next.Holder != nil
		c.NoReturn = sp.NoArrayIteratorReturn(r.Intrinsics[IntrinsicArrayIteratorPrototype])
	}
	if ValidateExec {
		r.checkArrayIteration()
	}
	// Assigning to an existing data property does not move ProtoEpoch, so the values are
	// compared on every call.
	return c.found && c.iter.Value().O == r.Intrinsics[IntrinsicArrayPrototypeValues] &&
		c.next.Value().O == r.Intrinsics[IntrinsicArrayIteratorPrototypeNext]
}

// NoArrayIteratorReturn reports whether array iterators have no return method, which closing
// one calls. The prototypes that it looks at count as prototypes from then on, so that adding a
// property to one moves ProtoEpoch.
func (sp *Space) NoArrayIteratorReturn(proto *Object) bool {
	key := sp.Key(KeyReturn)
	for p := proto; p != nil; p = p.Shape.Proto {
		p.Flags |= ObjIsProto
		if _, ok := p.getOwn(key); ok || p.Flags&ObjExotic != 0 {
			return false
		}
	}
	return true
}
