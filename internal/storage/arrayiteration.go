// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// arrayIterCache is what ArrayIterationPlain found for arrays with the initial shape.
type arrayIterCache struct {
	// iter and next are the data properties that iterating such an array reads.
	iter dataSlot
	next dataSlot

	// Epoch is the value of agent.ProtoEpoch at which iter and next were found.
	Epoch   uint32
	checked bool
	// found reports whether iter and next were found as data properties.
	found bool
	// NoReturn reports whether array iterators had no return method at epoch.
	NoReturn bool
}

// ArrayIterationPlain reports whether o uses the original array values and next methods.
// Reading an element can still invoke a getter.
func (r *RealmPart) ArrayIterationPlain(o *Object) bool {
	if ValidateExec {
		r.checkPlainArray(o)
	}
	return o.Shape == r.Shapes.Array && r.ArrayIterationUnmodified(r)
}

// ArrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (sp *Space) ArrayIterationUnmodified(r *RealmPart) bool {
	c := &r.ArrayIter
	if c.Epoch != sp.ProtoEpoch || !c.checked {
		c.checked = true
		c.Epoch = sp.ProtoEpoch
		c.iter = r.LookupDataSlot(r.Intrinsics[IntrinsicArrayPrototype], sp.WellKnownSymbol(SymbolIterator))
		c.next = r.LookupDataSlot(r.Intrinsics[IntrinsicArrayIteratorPrototype], sp.Key(KeyNext))
		c.found = c.iter.Holder != nil && c.next.Holder != nil
		c.NoReturn = sp.NoArrayIteratorReturn(r.Intrinsics[IntrinsicArrayIteratorPrototype])
	}
	if ValidateExec {
		r.checkArrayIteration()
	}
	// Assigning to an existing data property does not move ProtoEpoch, so the values are
	// compared on every call.
	return c.found && c.iter.Value().O == r.Intrinsics[IntrinsicArrayPrototypeValues] &&
		c.next.Value().O == r.Intrinsics[IntrinsicArrayIteratorPrototypeNext]
}

// NoArrayIteratorReturn reports whether array iterators have no return method, which closing
// one calls. The prototypes that it looks at count as prototypes from then on, so that adding a
// property to one moves ProtoEpoch.
func (sp *Space) NoArrayIteratorReturn(proto *Object) bool {
	key := sp.Key(KeyReturn)
	for p := proto; p != nil; p = p.Shape.Proto {
		p.Flags |= ObjIsProto
		if _, ok := p.getOwn(key); ok || p.Flags&ObjExotic != 0 {
			return false
		}
	}
	return true
}

// PairDestructuringPlain reports whether destructuring a new array of two elements into two
// bindings runs nothing that JavaScript could observe.
func (sp *Space) PairDestructuringPlain(r *RealmPart) bool {
	return sp.ArrayIterationUnmodified(r) && r.ArrayIter.NoReturn
}
