// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math/bits"
	"slices"
)

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
	s := o.shape
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
			if i, ok := StringIndex(k.Text()); ok {
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
		if i, ok := StringIndex(k.Text()); ok {
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
		if i, ok := StringIndex(key.Text()); ok {
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

// denseFactor is the largest number of slots per element for which [RealmPart.densify] moves the
// array index properties of an object into elems.
const denseFactor = 4

// growElems reports whether storing the index i should extend elems.
func growElems(elems []Value, i uint32) bool {
	n := int64(len(elems))
	return int64(i) < int64(cap(elems)) || int64(i) <= n+n/2+16
}

// AddIndex creates the data property with the array index i on o, which has no such own property.
// It reports false when o does not allow it.
func (r *RealmPart) AddIndex(o *Object, i uint32, val Value, flags uint8) bool {
	if o.flags&ObjNotExtensible != 0 {
		return false
	}
	if p := o.packedNums(); p != nil {
		var extra int
		if flags == FlagsData && uint(i) == uint(len(p.values)) {
			if val.O == NumberObject {
				o.appendNum(p, val.N)
				o.length = max(o.length, i+1)
				return true
			}
			extra = 1
		}
		o.unpackNums(p, extra)
	}
	if o.Kind == KindArray && i >= o.length {
		if o.flags&ObjFrozenLength != 0 {
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
		if n == packElemsThreshold && uint(i) == uint(n) && n == cap(o.elems) && val.O == NumberObject && o.tryPackNums(val.N) {
			return true
		}
		if growElems(o.elems, i) {
			o.ReserveElems(int(i) + 1 - n)
			for uint32(len(o.elems)) < i {
				o.pushElem(Hole)
			}
			o.pushElem(val)
			// The elements may have reached the indices in the shape. Checking only when their
			// number passes a power of two bounds the cost of the checks by that of the growth.
			if o.flags&ObjSparse != 0 && bits.Len(uint(n)) != bits.Len(uint(len(o.elems))) {
				r.densify(o)
			}
			return true
		}
	}
	o.flags |= ObjSparse
	r.Add(o, r.IndexName(i), val, flags)
	// Checking only when the number of properties in the shape reaches a power of two bounds
	// the cost of the checks by that of the additions. The elements would need more than i
	// slots.
	if c := int64(o.shape.Count); flags == FlagsData && c&(c-1) == 0 && (int64(len(o.elems))+c)*denseFactor > int64(i) {
		r.densify(o)
	}
	return true
}

// densify moves the array index properties of o from its shape to elems, and clears ObjSparse,
// when they are all writable, enumerable and configurable data properties and elems would have
// at most denseFactor slots per element.
func (r *RealmPart) densify(o *Object) {
	if o.Kind != KindArray && o.Class != ClassObject || o.flags&(ObjExotic|ObjNotExtensible) != 0 {
		return
	}
	// The new shape adds the named properties to root, which keeps its own properties, if any,
	// in the first base slots.
	s := o.shape
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
	end := int64(len(o.elems))
	for j, k := range keys {
		if k.Kind != KindString {
			continue
		}
		i, ok := StringIndex(k.Text())
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
	if (int64(len(o.elems))+count)*denseFactor < end {
		return
	}
	present := count
	for _, e := range o.elems {
		if !e.IsHole() {
			present++
		}
	}
	if present*denseFactor < end {
		return
	}

	o.ReserveElems(int(end) - len(o.elems))
	for int64(len(o.elems)) < end {
		o.pushElem(Hole)
	}
	o.shape = root
	slots := o.detachSlots(base, len(o.slots)-int(count))
	for _, k := range keys[base:] {
		p, _ := s.Lookup(k)
		if k.Kind == KindString {
			if i, ok := StringIndex(k.Text()); ok {
				o.elems[i] = slots[p.Slot]
				continue
			}
		}
		r.Add(o, k, slots[p.Slot], p.Flags)
	}
	o.leaveSlots(slots)
	o.flags &^= ObjSparse
	if o.Kind != KindArray {
		// The shape may be one that o had before, with other slots. The inline caches of
		// global variables point into the slots of the global object while it keeps its
		// shape, so o gets a new one.
		o.MakePrivate()
	}
	r.TouchProto(o)
}
