// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// This file creates function objects.

// newFunctionObject returns a new function object with the shape s and the function state fn,
// whose initial properties are implicit (see [realmPart.functionShapeFor]).
func newFunctionObject(s *shape, fn *function) *Object {
	return (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: s,
		fn:    fn,
	}).init()
}

// initFunction makes o, which must be zero, what [newFunctionObject] returns, with the internal
// state state.
func (o *propertyObject) initFunction(s *shape, fn *function, state any) *Object {
	o.kind = kindFunction
	o.class = classFunction
	o.flags = objExotic
	o.shape = s
	o.fn = fn
	o.ext = state
	return o.init()
}

// initMaterializedFunction makes o, which must be zero, a function object with the shape s and the
// function state fn, none of whose properties are implicit. room, which is in the allocation of o,
// is the storage of its properties.
func (o *propertyObject) initMaterializedFunction(s *shape, fn *function, room []Value) *Object {
	o.kind = kindFunction
	o.class = classFunction
	o.shape = s
	o.fn = fn
	o.slots = room[:0]
	o.pinned = pinnedSlots
	return o.init()
}

// makeHTMLDDA gives the function object o the internal slot [[IsHTMLDDA]].
func (o *Object) makeHTMLDDA() {
	o.kind = kindHTMLDDA
}

// materialize creates the properties that a function has from the start but that are only
// stored once something looks at them.
func (r *realmPart) materialize(o *Object) {
	if o.fn.materialized {
		return
	}
	f := o.ownFunction()
	f.materialized = true
	o.flags &^= objExotic
	if r := o.shape.realm; o.shape == r.shapes.function {
		o.shape = r.shapes.materializedFunction
	} else {
		r.rebuildShape(o, o.shape.proto, ordinaryRoot)
	}
	if c := f.code; c != nil {
		r.add(o, r.Key(KeyLength), Number(float64(c.length)), FlagConfigurable)
		r.add(o, r.Key(KeyName), r.SharedString(c.name), FlagConfigurable)
		if c.legacyCallerArguments {
			// Accessors, as the values change while the properties are neither writable nor
			// configurable.
			r.add(o, r.Key(keyArguments), newAccessor(c.realm.intrinsics[IntrinsicFunctionArgumentsGetter], nil), FlagAccessor)
			r.add(o, r.Key(keyCaller), newAccessor(c.realm.intrinsics[IntrinsicFunctionCallerGetter], nil), FlagAccessor)
		}
		switch {
		case c.isGenerator:
			proto := c.realm.intrinsics[IntrinsicGeneratorPrototype]
			if c.isAsync {
				proto = c.realm.intrinsics[IntrinsicAsyncGeneratorPrototype]
			}
			p := newObject(r.shapeForRealm(proto, c.realm))
			r.add(o, r.Key(KeyPrototype), Value{o: p}, FlagWritable)
		case f.constructor:
			proto := c.realm.intrinsics[IntrinsicObjectPrototype]
			p := newObject(r.shapeForRealm(proto, c.realm))
			r.add(p, r.Key(KeyConstructor), Value{o: o}, FlagsHidden)
			flags := FlagWritable
			if f.classConstructor {
				flags = 0
			}
			r.add(o, r.Key(KeyPrototype), Value{o: p}, flags)
		}
		return
	}
	var length int32
	if f.info != nil {
		length = f.info.length
	}
	r.add(o, r.Key(KeyLength), Number(float64(length)), FlagConfigurable)
	r.add(o, r.Key(KeyName), r.SharedString(f.nativeName()), FlagConfigurable)
}

// isLazyKey reports whether key names a property that materialize creates.
func (sp *space) isLazyKey(key *Object) bool {
	return key == sp.Key(KeyPrototype) || key == sp.Key(KeyName) || key == sp.Key(KeyLength) || key == sp.Key(keyCaller) || key == sp.Key(keyArguments)
}

// ownFunctionPrototypeSlot returns the own prototype slot of an ordinary function, or nil.
func (r *realmPart) ownFunctionPrototypeSlot(ctor *Object, site *propCache) *Value {
	if ctor.class != classFunction {
		return nil
	}
	if ctor.flags&objExotic != 0 {
		r.materialize(ctor)
	}
	p, ok := ctor.shape.lookup(r.Key(KeyPrototype))
	if !ok {
		return nil
	}
	slot := &ctor.slots[p.slot]
	if site != nil && ctor.flags&objExotic == 0 && slot.IsObject() {
		// Caching the constructor's own prototype slot also relies on @@hasInstance staying inherited.
		site.setInherited(ctor.shape, cacheOwn, r.protoEpoch)
		site.slot = p.slot
	}
	return slot
}

// startMaterialized makes the function o, which has no properties, one with the shape s and no
// implicit properties, and returns its state.
func (o *Object) startMaterialized(s *shape) *function {
	fn := o.ownFunction()
	fn.materialized = true
	o.flags &^= objExotic
	o.shape = s
	return fn
}
