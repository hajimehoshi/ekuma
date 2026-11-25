// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/bits"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

// SetElement stores v as the own property of o with the integer index i when
// [Element] returns that property and o is not a typed array, and reports whether it did.
func (o *Object) SetElement(i int64, v Value) bool {
	if o.objectProperties != nil && i >= 0 && i < int64(len(o.elems)) && !o.elems[i].isHole() {
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

// arrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func (r *realmPart) arrayCtorPlain(o *Object) bool {
	if o.shape != r.shapes.array {
		return false
	}
	ctor := r.Intrinsic(IntrinsicArray)
	c := r.lookupDataSlot(r.Intrinsic(IntrinsicArrayPrototype), r.Key(KeyConstructor))
	if c.holder == nil || c.value().o != ctor {
		return false
	}
	p, found := ctor.getOwn(r.WellKnownSymbol(SymbolSpecies))
	return found && p.flags&FlagAccessor != 0 && ctor.slots[p.slot].accessor().getter == r.Intrinsic(IntrinsicArraySpeciesGetter)
}

// appendElements is [QuietContext.AppendElements].
func (r *realmPart) appendElements(o *Object, vals []Value) (uint32, bool) {
	if !o.elemsResizable() || !r.newElementsPlain(o) {
		return 0, false
	}
	o.reserveElems(len(vals))
	o.elems = append(o.elems, vals...)
	o.length = uint32(len(o.elems))
	return o.length, true
}

// prependElements is [QuietContext.PrependElements].
func (r *realmPart) prependElements(o *Object, vals []Value) (uint32, bool) {
	if !o.elemsMovable() || !r.newElementsPlain(o) {
		return 0, false
	}
	n, k := len(o.elems), len(vals)
	if n > pollChunk {
		r.poll()
	}
	if cap(o.elems)-n >= k {
		o.elems = o.elems[:n+k]
		copy(o.elems[k:], o.elems[:n])
	} else {
		elems := make([]Value, n+k, n+k+max((n+k)/2, minElems))
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
	if !o.elemsResizable() || len(o.elems) == 0 {
		return Value{}, false
	}
	// Holes before the last element stay holes after it is removed.
	n := len(o.elems) - 1
	v := o.elems[n]
	if v.isHole() {
		return Value{}, false
	}
	o.removeLastElem()
	o.length = uint32(n)
	if o.elemsOversized() {
		o.shape.space.reallocElems(o)
	}
	return v, true
}

// RemoveFirstElement removes the first element of the array o and returns it.
// It reports false and does nothing when o is empty or needs the generic algorithm.
func (o *Object) RemoveFirstElement() (Value, bool) {
	if !o.elemsResizable() || len(o.elems) == 0 {
		return Value{}, false
	}
	v := o.elems[0]
	if v.isHole() {
		return Value{}, false
	}
	// A hole after the first element moves with the elements when no prototype can have
	// an element.
	if !o.holesAreAbsent() && containsHole(o.elems) {
		return Value{}, false
	}
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
		o.shape.space.reallocElems(o)
	}
	return v, true
}

// fillElements is [QuietContext.FillElements].
func (r *realmPart) fillElements(o *Object, n, start, end int64, v Value) bool {
	if !o.elemsChangeable() || int64(o.length) != n {
		return false
	}
	// Storing past the stored elements must not skip an index.
	if start < 0 || start > int64(len(o.elems)) || end > n || !o.holesAreAbsent() {
		return false
	}
	for i := start; i < end; i++ {
		if r.interrupt.Load() {
			r.poll()
		}
		if i < int64(len(o.elems)) {
			o.elems[i] = v
		} else {
			o.appendElem(v)
		}
	}
	return true
}

// searchElements is [QuietContext.SearchElements].
func (r *realmPart) searchElements(o *Object, i, end int64, target Value, sameValueZero bool) (found, next int64) {
	// Only strings and BigInts can take long to compare.
	if t := target.o; t != nil && (len(t.str) > maxQuickCompare || t.kind == kindBigInt) {
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
				if e.isHole() {
					return -1, i
				}
				if eq(e, target) {
					return i, i
				}
			}
			if i > end && i&(PollInterval-1) == PollInterval-1 {
				r.poll()
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
			r.poll()
		}
	}
	return -1, i
}

// searchElementsSlow is [realmPart.searchElements] for a target that can take long to
// compare. The target is not a number, so both comparisons are IsStrictlyEqual.
func (r *realmPart) searchElementsSlow(o *Object, i, end int64, target Value) (found, next int64) {
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
		if e.isHole() {
			// A caller that resumes after the hole does not count what was read before it.
			r.poll()
			return -1, i
		}
		if IsStrictlyEqual(e, target) {
			return i, i
		}
		if read += n; read >= compareBudget {
			read = 0
			r.poll()
		}
	}
	return -1, i
}

// joinStrings is [QuietContext.JoinStrings].
func (r *realmPart) joinStrings(o *Object, length int64, sep wtf8.String) (Value, bool) {
	if o.kind != kindArray || int64(o.length) != length || int64(len(o.elems)) != length || !wtf8.IsASCII(sep) {
		return Value{}, false
	}
	elems := o.elems
	n := int64(len(sep)) * int64(max(len(elems)-1, 0))
	for i, e := range elems {
		if e.o == nil || e.o.kind != kindString || !e.IsASCII() {
			return Value{}, false
		}
		if n += int64(len(e.o.str)); n > MaxStringLength {
			return Value{}, false
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.poll()
		}
	}
	var b strings.Builder
	b.Grow(int(n))
	var buf [shortstrings.MaxBytes]byte
	for i, e := range elems {
		if i > 0 {
			b.WriteString(string(sep))
		}
		if e.isShortString() {
			b.Write(e.AppendStringBytes(buf[:0]))
		} else {
			b.WriteString(string(e.o.Str()))
		}
		if i&(PollInterval-1) == PollInterval-1 {
			r.poll()
		}
	}
	v := r.NewString(wtf8.String(b.String()))
	if !v.isShortString() {
		// A string longer than a short one is a new object.
		v.o.flags |= objASCIIKnown | objASCII
	}
	return v, true
}

// reverseElements is [QuietContext.ReverseElements].
func (r *realmPart) reverseElements(o *Object) bool {
	if !o.isDense() {
		return false
	}
	for i, j := 0, len(o.elems)-1; i < j; i, j = i+1, j-1 {
		r.pollAt(int64(i))
		o.elems[i], o.elems[j] = o.elems[j], o.elems[i]
	}
	return true
}

// copyElementRange is [QuietContext.CopyElementRange].
func (r *realmPart) copyElementRange(o *Object, start, end int64) (*Object, bool) {
	if !o.elemsComplete() {
		return nil, false
	}
	if start < 0 || end > int64(len(o.elems)) {
		return nil, false
	}
	var elems []Value
	if start < end {
		elems = o.elems[start:end]
	}
	if containsHole(elems) || !r.arrayCtorPlain(o) {
		return nil, false
	}
	return r.NewArray(appendPolled(&r.poller, nil, elems)), true
}

// spliceElements is [QuietContext.SpliceElements].
func (r *realmPart) spliceElements(o *Object, length, start, del int64, items []Value) (*Object, bool) {
	if length != int64(len(o.elems)) || start < 0 || start > length || del < 0 || del > length-start {
		return nil, false
	}
	if !o.elemsMovable() || !r.arrayCtorPlain(o) || !r.arrayProtoPlain() {
		return nil, false
	}
	removed := appendPolled(&r.poller, nil, o.elems[start:start+del])
	n, k := int(length), len(items)
	newLen := n - int(del) + k
	if n-int(start+del) > pollChunk {
		r.poll()
	}
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
	// The removed elements stay out of reach in the storage that the elements keep.
	clear(elems[newLen:])
	o.elems = elems[:newLen]
	o.length = uint32(newLen)
	if o.elemsOversized() {
		r.reallocElems(o)
	}
	return r.NewArray(removed), true
}

// appendElementsOf is [QuietContext.AppendElementsOf].
func (r *realmPart) appendElementsOf(dst *Object, n int64, src *Object) bool {
	if !dst.elemsResizable() || int64(dst.length) != n || dst.shape != r.shapes.array {
		return false
	}
	elems, ok := src.plainElems()
	if !ok {
		return false
	}
	old := dst.elems
	dst.elems = appendPolled(&r.poller, old, elems)
	if cap(dst.elems) != cap(old) {
		dst.leaveElems(old)
	}
	dst.length = uint32(len(dst.elems))
	return true
}

// skipHoles is [QuietContext.SkipHoles].
func (r *realmPart) skipHoles(o *Object, i, end int64) int64 {
	if i < 0 || i >= end || i < int64(len(o.elems)) && !o.elems[i].isHole() || !o.holesAreAbsent() {
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
		idxs := r.sparseIndices.list(o)
		if p, _ := slices.BinarySearch(idxs, uint32(i)); p < len(idxs) && int64(idxs[p]) < j {
			j = int64(idxs[p])
		}
	}
	return j
}

// skipHolesBackward is [QuietContext.SkipHolesBackward].
func (r *realmPart) skipHolesBackward(o *Object, i, end int64) int64 {
	if i <= end || i < 0 || i > MaxArrayIndex || i < int64(len(o.elems)) && !o.elems[i].isHole() || !o.holesAreAbsent() {
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
	shape   *shape
	count   int32
	indices []uint32
}

// list returns the array indices that o stores in its shape, in ascending order.
// The result must not be modified.
func (c *sparseIndexCache) list(o *Object) []uint32 {
	s := o.shape
	if c.shape == s && c.count == s.count {
		if validateExec {
			c.checkSparseIndices(s)
		}
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

// growElems reports whether storing the index i should extend elems.
func growElems(elems []Value, i uint32) bool {
	n := int64(len(elems))
	return int64(i) < int64(cap(elems)) || int64(i) <= n+n/2+16
}

// addIndex creates the data property with the array index i on o, which has no such own property.
// It reports false when o does not allow it.
func (r *realmPart) addIndex(o *Object, i uint32, val Value, flags uint8) bool {
	if o.flags&objNotExtensible != 0 {
		return false
	}
	if o.kind == kindArray && i >= o.length {
		if o.flags&objFrozenLength != 0 {
			return false
		}
		o.length = i + 1
	}
	n := len(o.elems)
	if flags == FlagsData {
		if uint(i) < uint(n) {
			o.elems[i] = val
			return true
		}
		if growElems(o.elems, i) {
			o.reserveElems(int(i) + 1 - n)
			for uint32(len(o.elems)) < i {
				o.pushElem(hole)
			}
			o.pushElem(val)
			// The elements may have reached the indices in the shape. Checking only when their
			// number passes a power of two bounds the cost of the checks by that of the growth.
			if o.flags&objSparse != 0 && bits.Len(uint(n)) != bits.Len(uint(len(o.elems))) {
				r.densify(o)
			}
			return true
		}
	}
	o.flags |= objSparse
	r.add(o, r.indexName(i), val, flags)
	// Checking only when the number of properties in the shape reaches a power of two bounds
	// the cost of the checks by that of the additions. The elements would need more than i
	// slots.
	if c := int64(o.shape.count); flags == FlagsData && c&(c-1) == 0 && (int64(len(o.elems))+c)*denseFactor > int64(i) {
		r.densify(o)
	}
	return true
}

// densify moves the array index properties of o from its shape to elems, and clears objSparse,
// when they are all writable, enumerable and configurable data properties and elems would have
// at most denseFactor slots per element.
func (r *realmPart) densify(o *Object) {
	if o.kind != kindArray && o.class != classObject || o.flags&(objExotic|objNotExtensible) != 0 {
		return
	}
	// The new shape adds the named properties to root, which keeps its own properties, if any,
	// in the first base slots.
	s := o.shape
	var root *shape
	var base int
	if s.private {
		root = r.rootShape(s.proto, s.realm, s.family)
	} else {
		root = s
		for root.parent != nil {
			root = root.parent
		}
		base = int(root.count)
	}
	keys := s.keys()
	var count int64
	end := int64(len(o.elems))
	for j, k := range keys {
		if k.kind != kindString {
			continue
		}
		i, ok := stringIndex(k.str)
		if !ok {
			continue
		}
		if j < base {
			return
		}
		if p, _ := s.lookup(k); p.flags != FlagsData {
			return
		}
		count++
		end = max(end, int64(i)+1)
	}
	if (int64(len(o.elems))+count)*denseFactor < end {
		return
	}
	present := count
	for _, e := range o.elems {
		if !e.isHole() {
			present++
		}
	}
	if present*denseFactor < end {
		return
	}

	o.reserveElems(int(end) - len(o.elems))
	for int64(len(o.elems)) < end {
		o.pushElem(hole)
	}
	o.shape = root
	slots := o.detachSlots(base, len(o.slots)-int(count))
	for _, k := range keys[base:] {
		p, _ := s.lookup(k)
		if k.kind == kindString {
			if i, ok := stringIndex(k.str); ok {
				o.elems[i] = slots[p.slot]
				continue
			}
		}
		r.add(o, k, slots[p.slot], p.flags)
	}
	o.leaveSlots(slots)
	o.flags &^= objSparse
	if o.kind != kindArray {
		// The shape may be one that o had before, with other slots. The inline caches of
		// global variables point into the slots of the global object while it keeps its
		// shape, so o gets a new one.
		o.makePrivate()
	}
	r.touchProto(o)
}

// replaceOwn stores the value and attributes of the existing own property k of o.
func (r *realmPart) replaceOwn(o *Object, k PropKey, val Value, flags uint8) {
	name := k.key
	if name == nil {
		if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
			if flags == FlagsData {
				o.elems[k.idx] = val
				return
			}
			// The property no longer fits in elems.
			o.elems[k.idx] = hole
			o.flags |= objSparse
			r.add(o, r.indexName(k.idx), val, flags)
			return
		}
		name = r.indexName(k.idx)
	}
	r.Define(o, name, val, flags)
}

// arrayIterCache is what arrayIterationPlain found for arrays with the initial shape.
type arrayIterCache struct {
	// iter and next are the data properties that iterating such an array reads.
	iter dataSlot
	next dataSlot

	// epoch is the value of agent.protoEpoch at which iter and next were found.
	epoch   uint32
	checked bool
	// found reports whether iter and next were found as data properties.
	found bool
	// noReturn reports whether array iterators had no return method at epoch.
	noReturn bool
}

// arrayIterationPlain reports whether o uses the original array values and next methods.
// Reading an element can still invoke a getter.
func (r *realmPart) arrayIterationPlain(o *Object) bool {
	if validateExec {
		r.checkPlainArray(o)
	}
	return o.shape == r.shapes.array && r.arrayIterationUnmodified(r)
}

// arrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (sp *space) arrayIterationUnmodified(r *realmPart) bool {
	c := &r.arrayIter
	if c.epoch != sp.protoEpoch || !c.checked {
		c.checked = true
		c.epoch = sp.protoEpoch
		c.iter = r.lookupDataSlot(r.intrinsics[IntrinsicArrayPrototype], sp.WellKnownSymbol(SymbolIterator))
		c.next = r.lookupDataSlot(r.intrinsics[IntrinsicArrayIteratorPrototype], sp.Key(KeyNext))
		c.found = c.iter.holder != nil && c.next.holder != nil
		c.noReturn = sp.noArrayIteratorReturn(r.intrinsics[IntrinsicArrayIteratorPrototype])
	}
	if validateExec {
		r.checkArrayIteration()
	}
	// Assigning to an existing data property does not move protoEpoch, so the values are
	// compared on every call.
	return c.found && c.iter.value().o == r.intrinsics[IntrinsicArrayPrototypeValues] &&
		c.next.value().o == r.intrinsics[IntrinsicArrayIteratorPrototypeNext]
}

// noArrayIteratorReturn reports whether array iterators have no return method, which closing
// one calls. The prototypes that it looks at count as prototypes from then on, so that adding a
// property to one moves protoEpoch.
func (sp *space) noArrayIteratorReturn(proto *Object) bool {
	key := sp.Key(KeyReturn)
	for p := proto; p != nil; p = p.shape.proto {
		p.flags |= objIsProto
		if _, ok := p.getOwn(key); ok || p.flags&objExotic != 0 {
			return false
		}
	}
	return true
}
