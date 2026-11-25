// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// This file creates the objects that have a class and an internal state, other than functions and
// arrays.

// newObjectWithState returns a new object with the shape s, the class c and the internal state
// state, and no properties.
func newObjectWithState(s *shape, c class, state any) *Object {
	return (&propertyObject{
		kind:  kindObject,
		class: c,
		shape: s,
		ext:   state,
	}).init()
}

// initWithClass makes o, whose internal state and flags must be zero, an object with the shape s
// and the class c.
func (o *propertyObject) initWithClass(s *shape, c class) *Object {
	o.kind = kindObject
	o.class = c
	o.shape = s
	return o.init()
}

// initWithState makes o, which must be zero, what [newObjectWithState] returns.
func (o *propertyObject) initWithState(s *shape, c class, state any) *Object {
	o.kind = kindObject
	o.class = c
	o.shape = s
	o.ext = state
	return o.init()
}

// newExoticObject is [newObjectWithState] for an object whose named properties cannot be found by
// looking at its shape alone.
func newExoticObject(s *shape, c class, state any) *Object {
	return (&propertyObject{
		kind:  kindObject,
		class: c,
		flags: objExotic,
		shape: s,
		ext:   state,
	}).init()
}

// initExotic makes o, which must be zero, what [newExoticObject] returns.
func (o *propertyObject) initExotic(s *shape, c class, state any) *Object {
	o.flags = objExotic
	return o.initWithState(s, c, state)
}

// newNamespaceObject is [newExoticObject] for a module namespace object, which is not extensible.
func newNamespaceObject(s *shape, state any) *Object {
	return (&propertyObject{
		kind:  kindObject,
		class: classModuleNamespace,
		flags: objExotic | objNotExtensible,
		shape: s,
		ext:   state,
	}).init()
}

// newProxyObject is [newExoticObject] for a proxy, whose [[Call]] and [[Construct]] are fn. fn is
// nil for a proxy that is not callable.
func newProxyObject(s *shape, state any, fn *function) *Object {
	return (&propertyObject{
		kind:  kindObject,
		class: classProxy,
		flags: objExotic,
		shape: s,
		fn:    fn,
		ext:   state,
	}).init()
}

// newGlobalObject returns a new object for global bindings with the shape s and room for n
// properties. The inline caches of global variables point into its slots.
func newGlobalObject(s *shape, n int) *Object {
	o := newObject(s)
	o.initSlots(make([]Value, 0, n))
	o.pinned = pinnedGlobalSlots
	return o
}

// iterResult is an iterator result object together with the storage of its two properties,
// so that creating one is a single allocation.
type iterResult struct {
	propertyObject
	props [2]Value
}

// CreateIterResultObject creates an iterator result in the realm r.
func (r *realmPart) CreateIterResultObject(v Value, done bool) Value {
	result := &iterResult{
		kind:   kindObject,
		pinned: pinnedSlots,
		shape:  r.shapes.iterResult,
		props:  [2]Value{v, Bool(done)},
	}
	result.slots = result.props[:]
	return Value{o: result.init()}
}

// newArgumentsObject returns an arguments object of args followed by extra, without its callee.
func (r *realmPart) newArgumentsObject(args, extra []Value) *Object {
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
	o := a.initWithClass(r.shapes.arguments, classArguments)
	a.elems = elems
	a.storage = [3]Value{Number(float64(argc)), {o: r.Intrinsic(IntrinsicArrayPrototypeValues)}}
	a.slots = a.storage[:2]
	a.pinned = pinnedSlots | pinnedElems
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
	propertyObject
	storage [3]Value
}
