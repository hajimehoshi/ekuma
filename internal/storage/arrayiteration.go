// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// arrayIterCache is what [Space.ArrayIterationUnmodified] found for arrays with the initial shape.
type arrayIterCache struct {
	// IterRef and NextRef point to the live data slots for iteration at Epoch, or are both nil.
	// The interpreter reads these slots without modifying them.
	IterRef *Value
	NextRef *Value

	// Epoch is the value of [Space.ProtoEpoch] at which the slots were found.
	Epoch   uint32
	checked bool
	// NoReturn reports whether array iterators had no return method at Epoch.
	NoReturn bool
}

// hasOriginalMethods reports whether the cached slots still hold the original iterator methods.
func (c *arrayIterCache) hasOriginalMethods(r *RealmPart) bool {
	// Existing data slots can change value while the epoch stays the same.
	return c.IterRef != nil && c.IterRef.O == r.Intrinsics[IntrinsicArrayPrototypeValues] &&
		c.NextRef.O == r.Intrinsics[IntrinsicArrayIteratorPrototypeNext]
}

// ArrayIterationPlain reports whether o uses the original array values and next methods.
// Reading an element can still invoke a getter.
func (r *RealmPart) ArrayIterationPlain(o *Object) bool {
	if ValidateExec {
		r.CheckPlainArrayIteration(o)
	}
	return o.shape == r.Shapes.Array && r.ArrayIterationUnmodified(r)
}

// ArrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (sp *Space) ArrayIterationUnmodified(r *RealmPart) bool {
	c := &r.ArrayIter
	if c.Epoch != sp.ProtoEpoch || !c.checked {
		iter := r.LookupDataSlot(r.Intrinsics[IntrinsicArrayPrototype], sp.WellKnownSymbol(SymbolIterator))
		next := r.LookupDataSlot(r.Intrinsics[IntrinsicArrayIteratorPrototype], sp.Key(KeyNext))
		c.IterRef, c.NextRef = nil, nil
		if iter.Holder != nil && next.Holder != nil {
			c.IterRef = &iter.Holder.slots[iter.slot]
			c.NextRef = &next.Holder.slots[next.slot]
		}
		c.NoReturn = sp.NoArrayIteratorReturn(r.Intrinsics[IntrinsicArrayIteratorPrototype])
		c.checked = true
		c.Epoch = sp.ProtoEpoch
	}
	if ValidateExec {
		r.checkArrayIteration()
	}
	return c.hasOriginalMethods(r)
}

// NoArrayIteratorReturn reports whether array iterators have no return method, which closing
// one calls. The prototypes that it looks at count as prototypes from then on, so that adding a
// property to one moves ProtoEpoch.
func (sp *Space) NoArrayIteratorReturn(proto *Object) bool {
	key := sp.Key(KeyReturn)
	for p := proto; p != nil; p = p.shape.Proto {
		p.flags |= ObjIsProto
		if _, ok := p.getOwn(key); ok || p.flags&ObjExotic != 0 {
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
