// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// NewObjectWithState returns a new object with the shape s, the class c and the internal state
// state, and no properties.
func NewObjectWithState(s *Shape, c class, state any) *Object {
	return (&PropertyObject{
		Kind:  KindObject,
		Class: c,
		shape: s,
		Ext:   state,
	}).Init()
}

// initWithClass makes o, whose internal state and flags must be zero, an object with the shape s
// and the class c.
func (o *PropertyObject) initWithClass(s *Shape, c class) *Object {
	o.Kind = KindObject
	o.Class = c
	o.shape = s
	return o.Init()
}

// InitWithState makes o, which must be zero, what [NewObjectWithState] returns.
func (o *PropertyObject) InitWithState(s *Shape, c class, state any) *Object {
	o.Kind = KindObject
	o.Class = c
	o.shape = s
	o.Ext = state
	return o.Init()
}

// NewExoticObject is [NewObjectWithState] for an object whose named properties cannot be found by
// looking at its shape alone.
func NewExoticObject(s *Shape, c class, state any) *Object {
	return (&PropertyObject{
		Kind:  KindObject,
		Class: c,
		flags: ObjExotic,
		shape: s,
		Ext:   state,
	}).Init()
}

// InitExotic makes o, which must be zero, what [NewExoticObject] returns.
func (o *PropertyObject) InitExotic(s *Shape, c class, state any) *Object {
	o.flags = ObjExotic
	return o.InitWithState(s, c, state)
}

// NewNamespaceObject is [NewExoticObject] for a module namespace object, which is not extensible.
func NewNamespaceObject(s *Shape, state any) *Object {
	return (&PropertyObject{
		Kind:  KindObject,
		Class: ClassModuleNamespace,
		flags: ObjExotic | ObjNotExtensible,
		shape: s,
		Ext:   state,
	}).Init()
}

// NewGlobalObject returns a new object for global bindings with the shape s and room for n
// properties. The inline caches of global variables point into its slots.
func NewGlobalObject(s *Shape, n int) *Object {
	o := NewObject(s)
	o.InitSlots(make([]Value, 0, n))
	o.Pinned = PinnedGlobalSlots
	return o
}

// iterResult is an iterator result object together with the storage of its two properties,
// so that creating one is a single allocation.
type iterResult struct {
	PropertyObject
	props [2]Value
}

// CreateIterResultObject creates an iterator result in the realm r.
func (r *RealmPart) CreateIterResultObject(v Value, done bool) Value {
	result := &iterResult{
		Kind:   KindObject,
		Pinned: PinnedSlots,
		shape:  r.Shapes.IterResult,
		props:  [2]Value{v, Bool(done)},
	}
	result.slots = result.props[:]
	return Value{O: result.Init()}
}

// NewArgumentsObject returns an arguments object of args followed by extra, without its callee.
func (r *RealmPart) NewArgumentsObject(args, extra []Value) *Object {
	argc := len(args) + len(extra)
	var a *argumentsObject
	var elems []Value
	switch argc {
	case 0:
		a, elems = &argumentsObject{}, []Value{}
	case 1:
		w := &argumentsObjectWith[[1]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 2:
		w := &argumentsObjectWith[[2]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 3:
		w := &argumentsObjectWith[[3]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 4:
		w := &argumentsObjectWith[[4]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	default:
		a, elems = &argumentsObject{}, make([]Value, 0, argc)
	}
	elems = append(elems, args...)
	elems = append(elems, extra...)
	o := a.initWithClass(r.Shapes.Arguments, ClassArguments)
	a.elems = elems
	a.storage = [3]Value{Number(float64(argc)), {O: r.Intrinsic(IntrinsicArrayPrototypeValues)}}
	a.slots = a.storage[:2]
	a.Pinned = PinnedSlots | PinnedElems
	return o
}

// argumentsObjectWith is an arguments object together with the storage S of its elements.
type argumentsObjectWith[S any] struct {
	argumentsObject
	elems S
}

// argumentsObject is an arguments object together with the storage of its length,
// its iterator and its callee, so that creating one is a single allocation.
type argumentsObject struct {
	PropertyObject
	storage [3]Value
}
