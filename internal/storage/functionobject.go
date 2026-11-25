// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// closureObject is a function object together with its function state.
type closureObject struct {
	PropertyObject
	state Function
}

// closureObjectWith is a closureObject together with the storage of its upvalues and of the
// upvalues that hold copies of variables.
type closureObjectWith[U, C any] struct {
	closureObject

	// copies comes first so that an empty C adds no padding at the end.
	copies C
	upvals U
}

// NewClosure returns an object with the function state of a new closure of p, whose upvalues are
// nil, and the storage for its copied upvalues. The object is zero otherwise.
func NewClosure(p *FuncProto) (*Object, []Upvalue) {
	if len(p.Upvals) == 0 && !p.IsMethod && !p.IsClassConstructor {
		if p.plainFunction == nil {
			p.plainFunction = &Function{
				Code:        p,
				Constructor: p.IsConstructor,
				shared:      true,
			}
		}
		return (&PropertyObject{
			Fn: p.plainFunction,
		}).Init(), nil
	}
	o, copies := newClosureObject(len(p.Upvals), p.Ncopied)
	o.Fn.Code = p
	o.Fn.Constructor = p.IsConstructor
	o.Fn.ClassConstructor = p.IsClassConstructor
	o.Fn.Derived = p.IsDerived
	return o, copies
}

// OwnFunction gives o its own function state before an instance-specific change.
func (o *Object) OwnFunction() *Function {
	if o.Fn.shared {
		f := *o.Fn
		f.shared = false
		o.Fn = &f
	}
	return o.Fn
}

// newClosureObject returns a zero object whose function state has n nil upvalues, and
// storage for the ncopied of them that hold copies of variables. Small closures are a single
// allocation.
func newClosureObject(n, ncopied int) (*Object, []Upvalue) {
	var c *closureObject
	var upvals []*Upvalue
	var copies []Upvalue
	switch n<<2 | ncopied {
	case 0:
		c = &closureObject{}
	case 1 << 2:
		w := &closureObjectWith[[1]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 1<<2 | 1:
		w := &closureObjectWith[[1]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2 << 2:
		w := &closureObjectWith[[2]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 2<<2 | 1:
		w := &closureObjectWith[[2]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2<<2 | 2:
		w := &closureObjectWith[[2]*Upvalue, [2]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3 << 2:
		w := &closureObjectWith[[3]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 3<<2 | 1:
		w := &closureObjectWith[[3]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 2:
		w := &closureObjectWith[[3]*Upvalue, [2]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 3:
		w := &closureObjectWith[[3]*Upvalue, [3]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	default:
		// The capacities fill an allocation size class.
		switch {
		case n <= 5 && ncopied == 0:
			w := &closureObjectWith[[5]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 7 && ncopied == 0:
			w := &closureObjectWith[[7]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 11 && ncopied == 0:
			w := &closureObjectWith[[11]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied == 0:
			w := &closureObjectWith[[15]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied > 4 && ncopied <= 8:
			w := &closureObjectWith[[15]*Upvalue, [8]Upvalue]{}
			c, upvals, copies = &w.closureObject, w.upvals[:n:n], w.copies[:ncopied:ncopied]
		default:
			c = &closureObject{}
			upvals = make([]*Upvalue, n)
			if ncopied > 0 {
				copies = make([]Upvalue, ncopied)
			}
		}
	}
	c.state.Upvals = upvals
	c.Fn = &c.state
	return c.Init(), copies
}

// NewFunctionObject returns a new function object with the shape s and the function state fn,
// whose initial properties are implicit (see [RealmPart.FunctionShapeFor]).
func NewFunctionObject(s *Shape, fn *Function) *Object {
	return (&PropertyObject{
		Kind:  KindFunction,
		Class: ClassFunction,
		flags: ObjExotic,
		shape: s,
		Fn:    fn,
	}).Init()
}

// InitFunction makes o, which must be zero, what [NewFunctionObject] returns, with the internal
// state state.
func (o *PropertyObject) InitFunction(s *Shape, fn *Function, state any) *Object {
	o.Kind = KindFunction
	o.Class = ClassFunction
	o.flags = ObjExotic
	o.shape = s
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
	o.shape = s
	o.Fn = fn
	o.slots = room[:0]
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
	o.flags &^= ObjExotic
	if r := o.shape.Realm; o.shape == r.Shapes.Function {
		o.shape = r.Shapes.MaterializedFunction
	} else {
		r.rebuildShape(o, o.shape.Proto, OrdinaryRoot)
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

// IsLazyKey reports whether key names a property that [RealmPart.Materialize] creates.
func (sp *Space) IsLazyKey(key *Object) bool {
	return key == sp.Key(KeyPrototype) || key == sp.Key(KeyName) || key == sp.Key(KeyLength) || key == sp.Key(KeyCaller) || key == sp.Key(KeyArguments)
}

// OwnFunctionPrototypeSlot returns the own prototype slot of an ordinary function, or nil.
func (r *RealmPart) OwnFunctionPrototypeSlot(ctor *Object, site *PropCache) *Value {
	if ctor.Class != ClassFunction {
		return nil
	}
	if ctor.flags&ObjExotic != 0 {
		r.Materialize(ctor)
	}
	p, ok := ctor.shape.Lookup(r.Key(KeyPrototype))
	if !ok {
		return nil
	}
	slot := &ctor.slots[p.Slot]
	if site != nil && ctor.flags&ObjExotic == 0 && slot.IsObject() {
		// Caching the constructor's own prototype slot also relies on @@hasInstance staying inherited.
		site.SetInherited(ctor.shape, CacheOwn, r.ProtoEpoch)
		site.Slot = p.Slot
	}
	return slot
}

// StartMaterialized makes the function o, which has no properties, one with the shape s and no
// implicit properties, and returns its state.
func (o *Object) StartMaterialized(s *Shape) *Function {
	fn := o.OwnFunction()
	fn.Materialized = true
	o.flags &^= ObjExotic
	o.shape = s
	return fn
}
