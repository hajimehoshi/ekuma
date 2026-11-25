// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// NewFunctionObject returns a new function object with the shape s and the function state fn,
// whose initial properties are implicit (see [RealmPart.FunctionShapeFor]).
func NewFunctionObject(s *Shape, fn *Function) *Object {
	return (&PropertyObject{
		Kind:  KindFunction,
		Class: ClassFunction,
		Flags: ObjExotic,
		Shape: s,
		Fn:    fn,
	}).Init()
}

// InitFunction makes o, which must be zero, what [NewFunctionObject] returns, with the internal
// state state.
func (o *PropertyObject) InitFunction(s *Shape, fn *Function, state any) *Object {
	o.Kind = KindFunction
	o.Class = ClassFunction
	o.Flags = ObjExotic
	o.Shape = s
	o.Fn = fn
	o.Ext = state
	return o.Init()
}

// InitMaterializedFunction makes o, which must be zero, a function object with the shape s and the
// function state fn, none of whose properties are implicit. room, which is in the allocation of o,
// is the storage of its properties.
func (o *PropertyObject) InitMaterializedFunction(s *Shape, fn *Function, room []Value) *Object {
	o.Kind = KindFunction
	o.Class = ClassFunction
	o.Shape = s
	o.Fn = fn
	o.Slots = room[:0]
	o.Pinned = PinnedSlots
	return o.Init()
}

// MakeHTMLDDA gives the function object o the internal slot [[IsHTMLDDA]].
func (o *Object) MakeHTMLDDA() {
	o.Kind = KindHTMLDDA
}

// Materialize creates the properties that a function has from the start but that are only
// stored once something looks at them.
func (r *RealmPart) Materialize(o *Object) {
	if o.Fn.Materialized {
		return
	}
	f := o.OwnFunction()
	f.Materialized = true
	o.Flags &^= ObjExotic
	if r := o.Shape.Realm; o.Shape == r.Shapes.Function {
		o.Shape = r.Shapes.MaterializedFunction
	} else {
		r.rebuildShape(o, o.Shape.Proto, OrdinaryRoot)
	}
	if c := f.Code; c != nil {
		r.Add(o, r.Key(KeyLength), Number(float64(c.Length)), FlagConfigurable)
		r.Add(o, r.Key(KeyName), r.SharedString(c.Name), FlagConfigurable)
		if c.LegacyCallerArguments {
			// Accessors, as the values change while the properties are neither writable nor
			// configurable.
			r.Add(o, r.Key(KeyArguments), NewAccessor(c.Realm.Intrinsics[IntrinsicFunctionArgumentsGetter], nil), FlagAccessor)
			r.Add(o, r.Key(KeyCaller), NewAccessor(c.Realm.Intrinsics[IntrinsicFunctionCallerGetter], nil), FlagAccessor)
		}
		switch {
		case c.IsGenerator:
			proto := c.Realm.Intrinsics[IntrinsicGeneratorPrototype]
			if c.IsAsync {
				proto = c.Realm.Intrinsics[IntrinsicAsyncGeneratorPrototype]
			}
			p := NewObject(r.ShapeForRealm(proto, c.Realm))
			r.Add(o, r.Key(KeyPrototype), Value{O: p}, FlagWritable)
		case f.Constructor:
			proto := c.Realm.Intrinsics[IntrinsicObjectPrototype]
			p := NewObject(r.ShapeForRealm(proto, c.Realm))
			r.Add(p, r.Key(KeyConstructor), Value{O: o}, FlagsHidden)
			flags := FlagWritable
			if f.ClassConstructor {
				flags = 0
			}
			r.Add(o, r.Key(KeyPrototype), Value{O: p}, flags)
		}
		return
	}
	var length int32
	if f.Info != nil {
		length = f.Info.Length
	}
	r.Add(o, r.Key(KeyLength), Number(float64(length)), FlagConfigurable)
	r.Add(o, r.Key(KeyName), r.SharedString(f.NativeName()), FlagConfigurable)
}

// IsLazyKey reports whether key names a property that materialize creates.
func (sp *Space) IsLazyKey(key *Object) bool {
	return key == sp.Key(KeyPrototype) || key == sp.Key(KeyName) || key == sp.Key(KeyLength) || key == sp.Key(KeyCaller) || key == sp.Key(KeyArguments)
}

// OwnFunctionPrototypeSlot returns the own prototype slot of an ordinary function, or nil.
func (r *RealmPart) OwnFunctionPrototypeSlot(ctor *Object, site *PropCache) *Value {
	if ctor.Class != ClassFunction {
		return nil
	}
	if ctor.Flags&ObjExotic != 0 {
		r.Materialize(ctor)
	}
	p, ok := ctor.Shape.Lookup(r.Key(KeyPrototype))
	if !ok {
		return nil
	}
	slot := &ctor.Slots[p.Slot]
	if site != nil && ctor.Flags&ObjExotic == 0 && slot.IsObject() {
		// Caching the constructor's own prototype slot also relies on @@hasInstance staying inherited.
		site.SetInherited(ctor.Shape, CacheOwn, r.ProtoEpoch)
		site.Slot = p.Slot
	}
	return slot
}

// StartMaterialized makes the function o, which has no properties, one with the shape s and no
// implicit properties, and returns its state.
func (o *Object) StartMaterialized(s *Shape) *Function {
	fn := o.OwnFunction()
	fn.Materialized = true
	o.Flags &^= ObjExotic
	o.Shape = s
	return fn
}
