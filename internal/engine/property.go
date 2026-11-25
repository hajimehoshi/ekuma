// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
)

// This file holds the operations on properties that run no code.

type namedLookupKind uint8

const (
	lookupAbsent      namedLookupKind = iota // The chain ended. A miss or property addition can be cached.
	lookupSlot                               // A slot supplies the value or accessor.
	lookupLength                             // The implicit length of an array or string has no slot.
	lookupNumericMiss                        // A typed array answers undefined on reads and handles stores itself.
	lookupDelegate                           // The holder must perform each operation.
)

type namedLookup struct {
	kind   namedLookupKind
	holder *Object
	prop   prop
}

func (r namedLookup) length(receiver Value) Value {
	if r.holder.kind == kindString {
		return Number(float64(receiver.StringLength()))
	}
	return Number(float64(r.holder.length))
}

// lookup finds the named property key on o and its prototype chain.
func (r *realmPart) lookup(o *Object, key *Object) namedLookup {
	for h := o; h != nil; h = h.shape.proto {
		switch h.shape.family {
		case delegatedRoot:
			return namedLookup{
				kind:   lookupDelegate,
				holder: h,
			}
		case arrayRoot, primitiveStringRoot:
			if key == r.Key(KeyLength) {
				return namedLookup{
					kind:   lookupLength,
					holder: h,
				}
			}
		case typedArrayRoot:
			if isCanonicalNumericString(key) {
				return namedLookup{
					kind:   lookupNumericMiss,
					holder: h,
				}
			}
		case lazyFunctionRoot:
			if r.isLazyKey(key) {
				r.materialize(h)
			}
		}
		if p, ok := h.shape.lookup(key); ok {
			return namedLookup{
				kind:   lookupSlot,
				holder: h,
				prop:   p,
			}
		}
	}
	return namedLookup{
		kind: lookupAbsent,
	}
}

// readNamedSlot reads p and returns its getter separately for the caller to invoke.
func (sp *space) readNamedSlot(start, holder *Object, p prop, c *propCache) (val Value, getter *Object) {
	val = holder.slots[p.slot]
	if p.flags&FlagAccessor != 0 {
		// Objects of one shape can hold different own accessors, and replacing an own accessor
		// keeps the shape, so the cache of an own accessor remembers its slot, not its getter.
		if c != nil && holder == start {
			c.shape = start.shape
			c.kind = cacheOwnGetter
			c.slot = p.slot
		}
		g := val.accessor().getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && holder != start {
			c.setInherited(start.shape, cacheGetter, sp.protoEpoch)
			c.holder = g
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.shape = start.shape
			c.slot = p.slot
			c.kind = cacheOwn
		} else {
			c.setInherited(start.shape, cacheProto, sp.protoEpoch)
			c.ref = &holder.slots[p.slot]
		}
	}
	return val, nil
}

// rememberRead records the location of p for subsequent reads from start's shape.
func (c *propCache) rememberRead(sp *space, start, holder *Object, p prop) {
	s := start.shape
	if s.readID == 0 {
		if sp.lastReadShapeID == ^uint32(0) {
			return
		}
		sp.lastReadShapeID++
		s.readID = sp.lastReadShapeID
	}
	r := c.reads
	if r == nil {
		r = &namedReads{
			shapes: make(map[uint32]namedRead),
		}
		c.reads = r
	}
	if r.space != sp || r.epoch != sp.protoEpoch || len(r.shapes) >= 64 {
		clear(r.shapes)
		r.space = sp
		r.epoch = sp.protoEpoch
	}
	var depth int32
	for h := start; h != holder; h = h.shape.proto {
		depth++
	}
	r.shapes[s.readID] = namedRead{
		depth: depth,
		count: s.count,
		prop:  p,
	}
}

// dataSlot is the slot of a data property in the object that holds it.
type dataSlot struct {
	holder *Object
	slot   int32
}

// lookupDataSlot returns the named data slot on o or its prototypes, or a zero slot
// if the lookup cannot establish one without running code.
func (r *realmPart) lookupDataSlot(o, key *Object) dataSlot {
	l := r.lookup(o, key)
	if l.kind != lookupSlot || l.prop.flags&FlagAccessor != 0 {
		return dataSlot{}
	}
	return dataSlot{
		holder: l.holder,
		slot:   l.prop.slot,
	}
}

// value returns the current value of the slot.
func (d *dataSlot) value() Value {
	return d.holder.slots[d.slot]
}

// takeDataProperties gives the empty ordinary object dst the properties of from and reports
// true when creating them on dst one by one would give dst the shape and the elements of from.
// Otherwise it does nothing and reports false.
func (dst *Object) takeDataProperties(from *Object) bool {
	s := takenShape(dst, from)
	if s == nil || containsHole(from.elems) {
		return false
	}
	dst.shape = s
	dst.fillSlots(from.slots[:s.count])
	// The keys of the elements come first, in index order, so the elements of dst are those of
	// from.
	if len(from.elems) != 0 {
		dst.initElems(slices.Clone(from.elems))
	}
	return true
}

// assignDataProperties is [QuietContext.AssignDataProperties].
func (r *realmPart) assignDataProperties(to, from *Object) bool {
	s := takenShape(to, from)
	if s == nil || len(from.elems) != 0 {
		return false
	}
	// Set creates an own data property when the prototype lookup finds a writable data property or no property.
	for t := s; t.parent != nil; t = t.parent {
		switch l := r.lookup(to.shape.proto, t.key); l.kind {
		case lookupAbsent:
		case lookupSlot:
			if l.prop.flags&(FlagAccessor|FlagWritable) != FlagWritable {
				return false
			}
		default:
			return false
		}
	}
	to.shape = s
	to.fillSlots(from.slots[:s.count])
	return true
}

// takenShape returns the shape that dst, an empty ordinary object, would have after creating
// the properties of from other than its elements on it one by one, when that shape is the shape
// of from and from has only enumerable, writable and configurable data properties with string
// keys besides its elements. Otherwise it returns nil.
func takenShape(dst, from *Object) *shape {
	if dst.kind != kindObject || dst.class != classObject || dst.flags&(objNotExtensible|objIsProto|objExotic|objSparse) != 0 || dst.shape.count != 0 || len(dst.elems) != 0 {
		return nil
	}
	// An ordinary object that is not sparse has the properties with array index keys in its
	// elements, and the others in insertion order, which is the order of its slots.
	if from.kind != kindObject || from.class != classObject || from.flags&(objExotic|objSparse) != 0 {
		return nil
	}
	s := from.shape
	if s.private || s.flatBased || s.count == 0 {
		return nil
	}
	t := s
	for ; t.parent != nil; t = t.parent {
		if t.flags != FlagsData || t.key.kind != kindString {
			return nil
		}
	}
	// The root fixes the prototype, so the shape of dst would be reached through the same
	// transitions.
	if t != dst.shape {
		return nil
	}
	return s
}

// inheritsHasInstance reports whether the @@hasInstance property of c is the one that it
// inherits from %Function.prototype% of the current realm. A false result does not mean the
// opposite.
func (r *realmPart) inheritsHasInstance(c *Object) bool {
	// The property of %Function.prototype% is neither writable nor configurable, so only the
	// objects before it on the chain can hide it.
	fp := r.Intrinsic(IntrinsicFunctionPrototype)
	key := r.WellKnownSymbol(SymbolHasInstance)
	for h := c; h != fp; h = h.shape.proto {
		// The properties that an unmaterialized function lacks have string keys.
		if h == nil || h.class != classFunction && (h.class != classObject || h.flags&objExotic != 0) {
			return false
		}
		if _, ok := h.shape.lookup(key); ok {
			return false
		}
	}
	return true
}

// getVWithoutCall is [QuietContext.GetVWithoutCall]. It fills c, which may be nil, when the lookup can be
// repeated from the cache.
func (r *realmPart) getVWithoutCall(v Value, key *Object, c *propCache) (val Value, ok bool) {
	start := v.o
	switch {
	case v.IsObject():
	case v.IsNumber():
		start = r.Intrinsic(IntrinsicNumberPrototype)
	case v.IsBool():
		start = r.Intrinsic(IntrinsicBooleanPrototype)
	case v.IsSymbol():
		start = r.Intrinsic(IntrinsicSymbolPrototype)
	case v.IsBigInt():
		start = r.Intrinsic(IntrinsicBigIntPrototype)
	case v.IsString() && key != r.Key(KeyLength):
		start = r.Intrinsic(IntrinsicStringPrototype)
	default:
		return Undefined, false
	}
	l := r.lookup(start, key)
	switch l.kind {
	case lookupAbsent, lookupNumericMiss:
		if c != nil {
			// Both misses are stable for this shape and prototype epoch.
			c.keep(start.shape)
			c.setInherited(start.shape, cacheProto, r.protoEpoch)
			c.ref = &r.missHolder.slots[0]
		}
		return Undefined, true
	case lookupLength:
		return l.length(v), true
	case lookupSlot:
		if l.prop.flags&FlagAccessor == 0 {
			if c != nil {
				c.keep(start.shape)
			}
			val, _ = r.readNamedSlot(start, l.holder, l.prop, c)
			return val, true
		}
	}
	return Undefined, false
}

// hasOwnMethod is [QuietContext.HasOwnMethod].
func (o *Object) hasOwnMethod(key, method *Object) bool {
	if o.flags&objExotic != 0 {
		return false
	}
	p, ok := o.shape.lookup(key)
	return ok && p.flags&FlagAccessor == 0 && o.slots[p.slot].o == method
}

// inheritsMethod is [QuietContext.InheritsMethod].
func (o *Object) inheritsMethod(proto, key, method *Object) bool {
	if o.flags&objExotic != 0 || o.shape.proto != proto {
		return false
	}
	if _, ok := o.shape.lookup(key); ok {
		return false
	}
	return proto.hasOwnMethod(key, method)
}

// diagnosticDataProperty reads stored properties for stack traces and exception descriptions.
// It skips the property operations of exotic objects.
func diagnosticDataProperty(o, key *Object) (Value, bool) {
	for h := o; h != nil; h = h.shape.proto {
		if p, ok := h.shape.lookup(key); ok {
			if p.flags&FlagAccessor != 0 {
				break
			}
			return h.slots[p.slot], true
		}
	}
	return Undefined, false
}
