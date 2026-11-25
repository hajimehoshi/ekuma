// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
)

// Property attributes.
const (
	FlagEnumerable uint8 = 1 << iota
	FlagWritable
	FlagConfigurable
	FlagAccessor

	FlagsData   = FlagEnumerable | FlagWritable | FlagConfigurable
	FlagsHidden = FlagWritable | FlagConfigurable
)

// getOwn returns the own named property key of o.
func (o *Object) getOwn(key *Object) (Prop, bool) {
	return o.shape.Lookup(key)
}

// Add gives o the new own property key. The caller has checked that o has no such property.
func (sp *Space) Add(o *Object, key *Object, val Value, flags uint8) {
	s := o.shape
	if !s.Private && (s.Count >= PrivateShapeThreshold || sp.Builtins.building) {
		s = s.privateCopy()
		o.shape = s
		if sp.Builtins.building {
			sp.Builtins.Objects = append(sp.Builtins.Objects, o)
		}
	}
	if s.Private {
		// Growing the slots moves the addresses held by global variable caches, which
		// rely on the shape. No code runs while the built-ins are created.
		if s.renewOnAdd || len(o.slots) == cap(o.slots) && !sp.Builtins.building {
			s = s.handOver()
			o.shape = s
		}
		s.Table[key] = Prop{
			Slot:  int32(len(s.Order)),
			Flags: flags,
		}
		s.Order = append(s.Order, key)
		s.Count++
	} else {
		o.shape = s.With(key, flags)
	}
	o.AppendSlot(val)
	sp.TouchProto(o)
}

// Define creates or replaces the own named property key of o with the given attributes,
// without any of the checks of the JavaScript operations. An object of
// ExecutionContext.NewLazyObject must be initialized already.
func (r *RealmPart) Define(o *Object, key *Object, val Value, flags uint8) {
	if ValidateExec && o.Class == ClassLazy {
		panic("engine: property definition on an uninitialized lazy object")
	}
	if o.flags&ObjExotic != 0 && o.Fn != nil {
		r.Materialize(o)
	}
	p, ok := o.shape.Lookup(key)
	if !ok {
		r.Add(o, key, val, flags)
		return
	}
	if p.Flags != flags {
		r.setFlags(o, key, p, flags)
	}
	o.slots[p.Slot] = val
	r.TouchProto(o)
}

// DefineAccessor creates or replaces the own accessor property key of o with the given
// attributes. getter and setter may be nil.
func (r *RealmPart) DefineAccessor(o *Object, key *Object, getter, setter *Object, flags uint8) {
	r.Define(o, key, NewAccessor(getter, setter), flags|FlagAccessor)
}

// setFlags changes the attributes of the existing own property key of o.
func (sp *Space) setFlags(o *Object, key *Object, p Prop, flags uint8) {
	// An inline cache may remember the shape, so the changed layout needs another one.
	o.RenewShape()
	o.shape.Table[key] = Prop{
		Slot:  p.Slot,
		Flags: flags,
	}
	sp.TouchProto(o)
}

// ReplaceOwn stores the value and attributes of the existing own property k of o.
func (r *RealmPart) ReplaceOwn(o *Object, k PropKey, val Value, flags uint8) {
	name := k.Key
	if name == nil {
		if int64(k.Idx) < int64(len(o.elems)) && !o.elems[k.Idx].IsHole() {
			if flags == FlagsData {
				o.elems[k.Idx] = val
				return
			}
			// The property no longer fits in elems.
			o.elems[k.Idx] = Hole
			o.flags |= ObjSparse
			r.Add(o, r.IndexName(k.Idx), val, flags)
			return
		}
		name = r.IndexName(k.Idx)
	}
	r.Define(o, name, val, flags)
}

// removeFromPrivate deletes the own named property key of o, which must exist, from the private
// shape of o. The new shape takes over the storage of the old one, which only o has.
func (sp *Space) removeFromPrivate(o *Object, key *Object) {
	old := o.shape
	s := old.handOver()
	o.shape = s
	s.Count--
	i := s.Table[key].Slot
	delete(s.Table, key)
	s.Order[i] = nil
	o.slots[i] = Value{}
	// Trailing deleted slots go at once, and the others once they outnumber the keys, which
	// bounds the cost of compacting by that of the deletions.
	n := len(s.Order)
	for n > 0 && s.Order[n-1] == nil {
		n--
	}
	s.Order = s.Order[:n]
	o.truncateSlots(n)
	if n-int(s.Count) > int(s.Count) {
		o.compact()
	}
	sp.sparseIndices.remove(old, s, key)
	sp.TouchProto(o)
}

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
	return Number(float64(r.Holder.length))
}

// Lookup finds the named property key on o and its prototype chain.
func (r *RealmPart) Lookup(o *Object, key *Object) NamedLookup {
	for h := o; h != nil; h = h.shape.Proto {
		switch h.shape.Family {
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
		if p, ok := h.shape.Lookup(key); ok {
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
	val = holder.slots[p.Slot]
	if p.Flags&FlagAccessor != 0 {
		// Objects of one shape can hold different own accessors, and replacing an own accessor
		// keeps the shape, so the cache of an own accessor remembers its slot, not its getter.
		if c != nil && holder == start {
			c.Shape = start.shape
			c.Kind = CacheOwnGetter
			c.Slot = p.Slot
		}
		g := val.Accessor().Getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && holder != start {
			c.SetInherited(start.shape, CacheGetter, sp.ProtoEpoch)
			c.Holder = g
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.Shape = start.shape
			c.Slot = p.Slot
			c.Kind = CacheOwn
		} else {
			c.SetInherited(start.shape, CacheProto, sp.ProtoEpoch)
			c.Ref = &holder.slots[p.Slot]
		}
	}
	return val, nil
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

// Value returns the current value of the slot.
func (d *dataSlot) Value() Value {
	return d.Holder.slots[d.slot]
}

// TakeDataProperties gives the empty ordinary object dst the properties of from and reports
// true when creating them on dst one by one would give dst the shape and the elements of from.
// Otherwise it does nothing and reports false.
func (dst *Object) TakeDataProperties(from *Object) bool {
	s := takenShape(dst, from)
	if s == nil || containsHole(from.elems) {
		return false
	}
	dst.shape = s
	dst.fillSlots(from.slots[:s.Count])
	// The keys of the elements come first, in index order, so the elements of dst are those of
	// from.
	if len(from.elems) != 0 {
		dst.InitElems(slices.Clone(from.elems))
	}
	return true
}

// AssignDataProperties is quiet.Context.AssignDataProperties.
func (r *RealmPart) AssignDataProperties(to, from *Object) bool {
	s := takenShape(to, from)
	if s == nil || len(from.elems) != 0 {
		return false
	}
	// Set creates an own data property when the prototype lookup finds a writable data property or no property.
	for t := s; t.Parent != nil; t = t.Parent {
		switch l := r.Lookup(to.shape.Proto, t.Key); l.Kind {
		case LookupAbsent:
		case LookupSlot:
			if l.Prop.Flags&(FlagAccessor|FlagWritable) != FlagWritable {
				return false
			}
		default:
			return false
		}
	}
	to.shape = s
	to.fillSlots(from.slots[:s.Count])
	return true
}

// takenShape returns the shape that dst, an empty ordinary object, would have after creating
// the properties of from other than its elements on it one by one, when that shape is the shape
// of from and from has only enumerable, writable and configurable data properties with string
// keys besides its elements. Otherwise it returns nil.
func takenShape(dst, from *Object) *Shape {
	if dst.Kind != KindObject || dst.Class != ClassObject || dst.flags&(ObjNotExtensible|ObjIsProto|ObjExotic|ObjSparse) != 0 || dst.shape.Count != 0 || len(dst.elems) != 0 {
		return nil
	}
	// An ordinary object that is not sparse has the properties with array index keys in its
	// elements, and the others in insertion order, which is the order of its slots.
	if from.Kind != KindObject || from.Class != ClassObject || from.flags&(ObjExotic|ObjSparse) != 0 {
		return nil
	}
	s := from.shape
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
	if t != dst.shape {
		return nil
	}
	return s
}

// InheritsHasInstance reports whether the @@hasInstance property of c is the one that it
// inherits from %Function.prototype% of r. A false result does not mean the
// opposite.
func (r *RealmPart) InheritsHasInstance(c *Object) bool {
	// The property of %Function.prototype% is neither writable nor configurable, so only the
	// objects before it on the chain can hide it.
	fp := r.Intrinsic(IntrinsicFunctionPrototype)
	key := r.WellKnownSymbol(SymbolHasInstance)
	for h := c; h != fp; h = h.shape.Proto {
		// The properties that an unmaterialized function lacks have string keys.
		if h == nil || h.Class != ClassFunction && (h.Class != ClassObject || h.flags&ObjExotic != 0) {
			return false
		}
		if _, ok := h.shape.Lookup(key); ok {
			return false
		}
	}
	return true
}

// GetVWithoutCall is quiet.Context.GetVWithoutCall. It fills c, which may be nil, when the lookup can be
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
			c.Keep(start.shape)
			c.SetInherited(start.shape, CacheProto, r.ProtoEpoch)
			c.Ref = &r.MissHolder.slots[0]
		}
		return Undefined, true
	case LookupLength:
		return l.Length(v), true
	case LookupSlot:
		if l.Prop.Flags&FlagAccessor == 0 {
			if c != nil {
				c.Keep(start.shape)
			}
			val, _ = r.ReadNamedSlot(start, l.Holder, l.Prop, c)
			return val, true
		}
	}
	return Undefined, false
}

// HasOwnMethod is quiet.Context.HasOwnMethod.
func (o *Object) HasOwnMethod(key, method *Object) bool {
	if o.flags&ObjExotic != 0 {
		return false
	}
	p, ok := o.shape.Lookup(key)
	return ok && p.Flags&FlagAccessor == 0 && o.slots[p.Slot].O == method
}

// InheritsMethod is quiet.Context.InheritsMethod.
func (o *Object) InheritsMethod(proto, key, method *Object) bool {
	if o.flags&ObjExotic != 0 || o.shape.Proto != proto {
		return false
	}
	if _, ok := o.shape.Lookup(key); ok {
		return false
	}
	return proto.HasOwnMethod(key, method)
}

// DiagnosticDataProperty reads stored properties for stack traces and exception descriptions.
// It skips the property operations of exotic objects.
func DiagnosticDataProperty(o, key *Object) (Value, bool) {
	for h := o; h != nil; h = h.shape.Proto {
		if p, ok := h.shape.Lookup(key); ok {
			if p.Flags&FlagAccessor != 0 {
				break
			}
			return h.slots[p.Slot], true
		}
	}
	return Undefined, false
}

// HasOwnPropertyPlain is HasOwnProperty of o and the key k when finding out neither throws nor
// runs code, and takes a short time. It reports false as ok otherwise.
func (r *RealmPart) HasOwnPropertyPlain(o *Object, k Value) (has, ok bool) {
	var key QueryKey
	switch {
	case k.IsNumber():
		i, ok := NumberIndex(k.N)
		if !ok {
			return false, false
		}
		key = QueryKey(IndexKey(i))
	case k.IsString():
		if k.IsShortString() && k.O.flags&ObjInterned == 0 {
			key = r.ShortQueryKey(k)
		} else {
			key = r.QueryStringKey(k.O)
		}
	case k.O != nil && k.O.Kind == KindSymbol:
		key = QueryKey(NamedKey(k.O))
	default:
		return false, false
	}
	if key.Key == nil {
		if o.IndicesInElements() {
			return int64(key.Idx) < int64(len(o.elems)) && !o.elems[key.Idx].IsHole(), true
		}
		if o.DelegatesPropertyAccess() {
			return false, false
		}
		if o.objectProperties != nil && int64(key.Idx) < int64(len(o.elems)) && !o.elems[key.Idx].IsHole() {
			return true, true
		}
		switch o.Class {
		case ClassTypedArray:
			return TypedArrayHas(o, key.Idx), true
		case ClassString:
			s := o.ExtState().(*Object)
			if uint(key.Idx) < uint(s.StringLength()) {
				return true, true
			}
		}
		if o.flags&ObjSparse == 0 {
			return false, true
		}
		name := r.IndexKey(key.Idx)
		if name == nil {
			return false, true
		}
		_, has = o.shape.Lookup(name)
		return has, true
	}
	switch o.shape.Family {
	case OrdinaryRoot:
	case ArrayRoot:
		if key.Key == r.Key(KeyLength) {
			return true, true
		}
	case LazyFunctionRoot:
		if r.IsLazyKey(key.Key) {
			r.Materialize(o)
		}
	default:
		return false, false
	}
	if key.Missing() {
		return false, true
	}
	_, has = o.shape.Lookup(key.Key)
	return has, true
}

// SlotValue returns the value or the accessors of p, an own property of o.
func (o *Object) SlotValue(p Prop) Value {
	return o.slots[p.Slot]
}

// SlotCount returns the number of slots of o.
func (o *Object) SlotCount() int {
	return len(o.slots)
}

// StoreData stores v in p, an own property of o, and reports whether p is a writable data property.
func (o *Object) StoreData(p Prop, v Value) bool {
	if p.Flags&(FlagAccessor|FlagWritable) != FlagWritable {
		return false
	}
	o.slots[p.Slot] = v
	return true
}

// InitBinding sets the value of the binding key of the global lexical bindings o.
func (o *Object) InitBinding(key *Object, v Value) {
	p, _ := o.shape.Lookup(key)
	o.slots[p.Slot] = v
}

// StoreMapped stores v in p, an index property of the arguments object o that maps to a parameter.
func (o *Object) StoreMapped(p Prop, v Value) {
	if ValidateExec && o.Class != ClassArguments {
		panic("engine: StoreMapped on an object of another class")
	}
	o.slots[p.Slot] = v
}

// Remove deletes the own named property key of o, which must exist.
func (sp *Space) Remove(o, key *Object) {
	old := o.shape
	if old.Private {
		sp.removeFromPrivate(o, key)
		return
	}
	// The shared shape without key has the later properties in the slots after the one of key,
	// so their values move down by one.
	if s := old.Without(key); s != nil {
		p, _ := old.Lookup(key)
		o.removeSlot(int(p.Slot))
		o.shape = s
		sp.TouchProto(o)
		return
	}
	// Otherwise removing a property moves the later ones, so the object gets a shape that no
	// inline cache has seen.
	s := &Shape{
		Space:   old.Space,
		Realm:   old.Realm,
		Proto:   old.Proto,
		Family:  old.Family,
		Table:   make(map[*Object]Prop, old.Count),
		Private: true,
	}
	slots := make([]Value, 0, len(o.slots))
	for _, k := range old.Keys() {
		if k == key {
			continue
		}
		q, _ := old.Lookup(k)
		s.Table[k] = Prop{
			Slot:  s.Count,
			Flags: q.Flags,
		}
		s.Order = append(s.Order, k)
		s.Count++
		slots = append(slots, o.slots[q.Slot])
	}
	o.replaceSlots(slots)
	o.shape = s
	sp.TouchProto(o)
}
