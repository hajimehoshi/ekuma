// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
)

type namedLookupKind uint8

const (
	LookupAbsent      namedLookupKind = iota // The chain ended. A miss or property addition can be cached.
	LookupSlot                               // A slot supplies the value or accessor.
	LookupLength                             // The implicit length of an array or string has no slot.
	LookupNumericMiss                        // A typed array answers undefined on reads and handles stores itself.
	LookupDelegate                           // The holder must perform each operation.
)

type NamedLookup struct {
	Kind   namedLookupKind
	Holder *Object
	Prop   Prop
}

func (r NamedLookup) Length(receiver Value) Value {
	if r.Holder.Kind == KindString {
		return Number(float64(receiver.StringLength()))
	}
	return Number(float64(r.Holder.Length))
}

// Lookup finds the named property key on o and its prototype chain.
func (r *RealmPart) Lookup(o *Object, key *Object) NamedLookup {
	for h := o; h != nil; h = h.Shape.Proto {
		switch h.Shape.Family {
		case DelegatedRoot:
			return NamedLookup{
				Kind:   LookupDelegate,
				Holder: h,
			}
		case ArrayRoot, PrimitiveStringRoot:
			if key == r.Key(KeyLength) {
				return NamedLookup{
					Kind:   LookupLength,
					Holder: h,
				}
			}
		case TypedArrayRoot:
			if IsCanonicalNumericString(key) {
				return NamedLookup{
					Kind:   LookupNumericMiss,
					Holder: h,
				}
			}
		case LazyFunctionRoot:
			if r.IsLazyKey(key) {
				r.Materialize(h)
			}
		}
		if p, ok := h.Shape.Lookup(key); ok {
			return NamedLookup{
				Kind:   LookupSlot,
				Holder: h,
				Prop:   p,
			}
		}
	}
	return NamedLookup{
		Kind: LookupAbsent,
	}
}

// ReadNamedSlot reads p and returns its getter separately for the caller to invoke.
func (sp *Space) ReadNamedSlot(start, holder *Object, p Prop, c *PropCache) (val Value, getter *Object) {
	val = holder.Slots[p.Slot]
	if p.Flags&FlagAccessor != 0 {
		// Objects of one shape can hold different own accessors, and replacing an own accessor
		// keeps the shape, so the cache of an own accessor remembers its slot, not its getter.
		if c != nil && holder == start {
			c.Shape = start.Shape
			c.Kind = CacheOwnGetter
			c.Slot = p.Slot
		}
		g := val.Accessor().Getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && holder != start {
			c.SetInherited(start.Shape, CacheGetter, sp.ProtoEpoch)
			c.Holder = g
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.Shape = start.Shape
			c.Slot = p.Slot
			c.Kind = CacheOwn
		} else {
			c.SetInherited(start.Shape, CacheProto, sp.ProtoEpoch)
			c.Ref = &holder.Slots[p.Slot]
		}
	}
	return val, nil
}

// RememberRead records the location of p for subsequent reads from start's shape.
func (c *PropCache) RememberRead(sp *Space, start, holder *Object, p Prop) {
	s := start.Shape
	if s.ReadID == 0 {
		if sp.lastReadShapeID == ^uint32(0) {
			return
		}
		sp.lastReadShapeID++
		s.ReadID = sp.lastReadShapeID
	}
	r := c.Reads
	if r == nil {
		r = &namedReads{
			Shapes: make(map[uint32]namedRead),
		}
		c.Reads = r
	}
	if r.Space != sp || r.Epoch != sp.ProtoEpoch || len(r.Shapes) >= 64 {
		clear(r.Shapes)
		r.Space = sp
		r.Epoch = sp.ProtoEpoch
	}
	var depth int32
	for h := start; h != holder; h = h.Shape.Proto {
		depth++
	}
	r.Shapes[s.ReadID] = namedRead{
		Depth: depth,
		Count: s.Count,
		Prop:  p,
	}
}

// dataSlot is the slot of a data property in the object that holds it.
type dataSlot struct {
	Holder *Object
	slot   int32
}

// LookupDataSlot returns the named data slot on o or its prototypes, or a zero slot
// if the lookup cannot establish one without running code.
func (r *RealmPart) LookupDataSlot(o, key *Object) dataSlot {
	l := r.Lookup(o, key)
	if l.Kind != LookupSlot || l.Prop.Flags&FlagAccessor != 0 {
		return dataSlot{}
	}
	return dataSlot{
		Holder: l.Holder,
		slot:   l.Prop.Slot,
	}
}

// Value returns the current Value of the slot.
func (d *dataSlot) Value() Value {
	return d.Holder.Slots[d.slot]
}

// TakeDataProperties gives the empty ordinary object dst the properties of from and reports
// true when creating them on dst one by one would give dst the shape and the elements of from.
// Otherwise it does nothing and reports false.
func (dst *Object) TakeDataProperties(from *Object) bool {
	s := takenShape(dst, from)
	if s == nil || ContainsHole(from.Elems) {
		return false
	}
	dst.Shape = s
	dst.fillSlots(from.Slots[:s.Count])
	// The keys of the elements come first, in index order, so the elements of dst are those of
	// from.
	if len(from.Elems) != 0 {
		dst.InitElems(slices.Clone(from.Elems))
	}
	return true
}

// AssignDataProperties is QuietContext.AssignDataProperties.
func (r *RealmPart) AssignDataProperties(to, from *Object) bool {
	s := takenShape(to, from)
	if s == nil || len(from.Elems) != 0 {
		return false
	}
	// Set creates an own data property when the prototype lookup finds a writable data property or no property.
	for t := s; t.Parent != nil; t = t.Parent {
		switch l := r.Lookup(to.Shape.Proto, t.Key); l.Kind {
		case LookupAbsent:
		case LookupSlot:
			if l.Prop.Flags&(FlagAccessor|FlagWritable) != FlagWritable {
				return false
			}
		default:
			return false
		}
	}
	to.Shape = s
	to.fillSlots(from.Slots[:s.Count])
	return true
}

// takenShape returns the shape that dst, an empty ordinary object, would have after creating
// the properties of from other than its elements on it one by one, when that shape is the shape
// of from and from has only enumerable, writable and configurable data properties with string
// keys besides its elements. Otherwise it returns nil.
func takenShape(dst, from *Object) *Shape {
	if dst.Kind != KindObject || dst.Class != ClassObject || dst.Flags&(ObjNotExtensible|ObjIsProto|ObjExotic|ObjSparse) != 0 || dst.Shape.Count != 0 || len(dst.Elems) != 0 {
		return nil
	}
	// An ordinary object that is not sparse has the properties with array index keys in its
	// elements, and the others in insertion order, which is the order of its slots.
	if from.Kind != KindObject || from.Class != ClassObject || from.Flags&(ObjExotic|ObjSparse) != 0 {
		return nil
	}
	s := from.Shape
	if s.Private || s.flatBased || s.Count == 0 {
		return nil
	}
	t := s
	for ; t.Parent != nil; t = t.Parent {
		if t.Flags != FlagsData || t.Key.Kind != KindString {
			return nil
		}
	}
	// The root fixes the prototype, so the shape of dst would be reached through the same
	// transitions.
	if t != dst.Shape {
		return nil
	}
	return s
}

// InheritsHasInstance reports whether the @@hasInstance property of c is the one that it
// inherits from %Function.prototype% of the current realm. A false result does not mean the
// opposite.
func (r *RealmPart) InheritsHasInstance(c *Object) bool {
	// The property of %Function.prototype% is neither writable nor configurable, so only the
	// objects before it on the chain can hide it.
	fp := r.Intrinsic(IntrinsicFunctionPrototype)
	key := r.WellKnownSymbol(SymbolHasInstance)
	for h := c; h != fp; h = h.Shape.Proto {
		// The properties that an unmaterialized function lacks have string keys.
		if h == nil || h.Class != ClassFunction && (h.Class != ClassObject || h.Flags&ObjExotic != 0) {
			return false
		}
		if _, ok := h.Shape.Lookup(key); ok {
			return false
		}
	}
	return true
}

// GetVWithoutCall is QuietContext.GetVWithoutCall. It fills c, which may be nil, when the lookup can be
// repeated from the cache.
func (r *RealmPart) GetVWithoutCall(v Value, key *Object, c *PropCache) (val Value, ok bool) {
	start := v.O
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
	l := r.Lookup(start, key)
	switch l.Kind {
	case LookupAbsent, LookupNumericMiss:
		if c != nil {
			// Both misses are stable for this shape and prototype epoch.
			c.Keep(start.Shape)
			c.SetInherited(start.Shape, CacheProto, r.ProtoEpoch)
			c.Ref = &r.MissHolder.Slots[0]
		}
		return Undefined, true
	case LookupLength:
		return l.Length(v), true
	case LookupSlot:
		if l.Prop.Flags&FlagAccessor == 0 {
			if c != nil {
				c.Keep(start.Shape)
			}
			val, _ = r.ReadNamedSlot(start, l.Holder, l.Prop, c)
			return val, true
		}
	}
	return Undefined, false
}

// HasOwnMethod is QuietContext.HasOwnMethod.
func (o *Object) HasOwnMethod(key, method *Object) bool {
	if o.Flags&ObjExotic != 0 {
		return false
	}
	p, ok := o.Shape.Lookup(key)
	return ok && p.Flags&FlagAccessor == 0 && o.Slots[p.Slot].O == method
}

// InheritsMethod is QuietContext.InheritsMethod.
func (o *Object) InheritsMethod(proto, key, method *Object) bool {
	if o.Flags&ObjExotic != 0 || o.Shape.Proto != proto {
		return false
	}
	if _, ok := o.Shape.Lookup(key); ok {
		return false
	}
	return proto.HasOwnMethod(key, method)
}

// DiagnosticDataProperty reads stored properties for stack traces and exception descriptions.
// It skips the property operations of exotic objects.
func DiagnosticDataProperty(o, key *Object) (Value, bool) {
	for h := o; h != nil; h = h.Shape.Proto {
		if p, ok := h.Shape.Lookup(key); ok {
			if p.Flags&FlagAccessor != 0 {
				break
			}
			return h.Slots[p.Slot], true
		}
	}
	return Undefined, false
}
