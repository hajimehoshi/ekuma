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
