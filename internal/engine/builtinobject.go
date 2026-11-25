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

// newRegExpObject returns a new RegExp object with the shape s, whose only property is lastIndex,
// and the internal state state.
func newRegExpObject(s *shape, state any) *Object {
	o, slots := newObjectWithStorage(1)
	o.kind = kindObject
	o.class = classRegExp
	o.shape = s
	o.initSlots(slots)
	o.slots[0] = Number(0)
	o.ext = state
	return o
}

// newGlobalObject returns a new object for global bindings with the shape s and room for n
// properties. The inline caches of global variables point into its slots.
func newGlobalObject(s *shape, n int) *Object {
	o := newObject(s)
	o.initSlots(make([]Value, 0, n))
	o.pinned = pinnedGlobalSlots
	return o
}
