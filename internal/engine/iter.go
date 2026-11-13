// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// getIterator implements GetIterator and returns the iterator and its next method.
func (vm *VM) getIterator(v Value, async bool) (it, next Value) {
	var m *Object
	if async {
		m = vm.GetMethod(v, vm.K.SymAsyncIterator)
		if m == nil {
			sync, syncNext := vm.getIterator(v, false)
			return vm.asyncFromSyncIterator(sync, syncNext)
		}
	} else {
		m = vm.GetMethod(v, vm.K.SymIterator)
	}
	if m == nil {
		vm.ThrowTypeError(vm.DisplayString(v) + " is not iterable")
	}
	it = vm.Call(m, v)
	if !it.IsObject() {
		vm.ThrowTypeError("Result of the Symbol.iterator method is not an object")
	}
	return it, vm.GetV(it, vm.K.next)
}

// iteratorStep calls next and returns the value of the result, or false when the iterator is done.
func (vm *VM) iteratorStep(it, next Value) (Value, bool) {
	r := vm.CallValue(next, it)
	if !r.IsObject() {
		vm.ThrowTypeError("Iterator result " + vm.DisplayString(r) + " is not an object")
	}
	if Truthy(vm.GetV(r, vm.K.done)) {
		return Undefined, false
	}
	return vm.GetV(r, vm.K.Value), true
}

// iteratorClose implements IteratorClose. With pending set, an exception is already
// being propagated and the errors of closing are dropped.
func (vm *VM) iteratorClose(it Value, pending bool) {
	if pending {
		saved := vm.thrown
		vm.protect(func() {
			if m := vm.GetMethod(it, vm.K.ret); m != nil {
				vm.Call(m, it)
			}
		})
		vm.thrown = saved
		return
	}
	m := vm.GetMethod(it, vm.K.ret)
	if m == nil {
		return
	}
	if r := vm.Call(m, it); !r.IsObject() {
		vm.ThrowTypeError("Iterator result " + vm.DisplayString(r) + " is not an object")
	}
}

// iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (vm *VM) Iterate(v Value, f func(Value) bool) {
	if v.O != nil && v.O.Kind == KindArray && vm.arrayIterationPlain(v.O) {
		o := v.O
		for i := uint32(0); i < o.Length; i++ {
			e := Undefined
			if int(i) < len(o.Elems) && !o.Elems[i].IsHole() {
				e = o.Elems[i]
			} else {
				e = vm.GetKey(v, PropKey{Idx: i})
			}
			if !f(e) {
				return
			}
		}
		return
	}
	it, next := vm.getIterator(v, false)
	for {
		e, ok := vm.iteratorStep(it, next)
		if !ok {
			return
		}
		var more bool
		if !vm.protect(func() {
			more = f(e)
		}) {
			vm.iteratorClose(it, true)
			panic(errThrow)
		}
		if !more {
			vm.iteratorClose(it, false)
			return
		}
	}
}

// appendIterable appends the values that iterating v yields to the array o.
func (vm *VM) appendIterable(o *Object, v Value) {
	vm.Iterate(v, func(e Value) bool {
		o.Elems = append(o.Elems, e)
		o.Length = uint32(len(o.Elems))
		return true
	})
}

// newIterResult implements CreateIterResultObject.
func (vm *VM) NewIterResult(v Value, done bool) *Object {
	return &Object{
		Kind:  kindObject,
		Shape: vm.iterResultShape,
		Slots: []Value{v, Bool(done)},
	}
}

// forIn is the state of a for-in loop.
type forIn struct {
	obj     *Object
	keys    []PropKey
	pos     int
	visited map[PropKey]struct{}
}

// newForIn returns the iterator of a for-in loop over v.
func (vm *VM) newForIn(v Value) Value {
	it := &forIn{}
	if !v.IsNullish() {
		it.obj = vm.ToObject(v)
		it.keys = vm.OwnKeys(it.obj)
	}
	return Value{
		O: &Object{
			Kind:  kindObject,
			Class: classForIn,
			Ext:   it,
		},
	}
}

// forInNext returns the next key of the for-in iterator o.
func (vm *VM) forInNext(o *Object) (Value, bool) {
	it := o.Ext.(*forIn)
	for it.obj != nil {
		for it.pos < len(it.keys) {
			k := it.keys[it.pos]
			it.pos++
			if k.IsSymbol() {
				continue
			}
			if _, seen := it.visited[k]; seen {
				continue
			}
			// A property that was deleted in the meantime is skipped, and one that is not
			// enumerable still hides the properties of the same name further up.
			d, ok := vm.GetOwnProperty(it.obj, k)
			if !ok {
				continue
			}
			if it.visited == nil {
				it.visited = map[PropKey]struct{}{}
			}
			it.visited[k] = struct{}{}
			if d.Flags&FlagEnumerable == 0 {
				continue
			}
			if k.Key == nil {
				return vm.NewString(strconv.FormatUint(uint64(k.Idx), 10)), true
			}
			return Value{O: k.Key}, true
		}
		it.obj = vm.ProtoOf(it.obj)
		it.pos = 0
		it.keys = nil
		if it.obj != nil {
			it.keys = vm.OwnKeys(it.obj)
		}
	}
	return Undefined, false
}

// newArguments returns the arguments object of the frame fr.
func (vm *VM) newArguments(fr *frame, regs []Value, mapped bool) *Object {
	o := &Object{
		Kind:  kindObject,
		Class: ClassArguments,
		Shape: vm.argumentsShape,
		Elems: vm.frameArgs(fr, regs),
	}
	o.Slots = []Value{Number(float64(fr.argc)), Value{O: vm.FnArrayValues}}
	if mapped {
		vm.Add(o, vm.K.callee, Value{O: fr.cl}, FlagsHidden)
	} else {
		vm.Add(o, vm.K.callee, NewAccessor(vm.ThrowTypeErrorFunc, vm.ThrowTypeErrorFunc), FlagAccessor)
	}
	return o
}

// templateObject returns the template object of a tagged template, creating it on first use.
func (vm *VM) templateObject(t *templateSite) *Object {
	if t.object != nil {
		return t.object
	}
	freeze := func(vals []Value) *Object {
		a := vm.NewArray(nil)
		for i, v := range vals {
			vm.Add(a, vm.indexName(uint32(i)), v, FlagEnumerable)
		}
		a.Length = uint32(len(vals))
		a.Flags |= ObjSparse | ObjFrozenLength | ObjNotExtensible
		return a
	}
	raw := freeze(t.raw)
	raw.makePrivate()
	o := freeze(t.cooked)
	vm.Add(o, vm.K.Raw, Value{O: raw}, 0)
	o.makePrivate()
	t.object = o
	return o
}

// stringFromGo returns the Go string s, which is taken to be UTF-8, as a value.
func (vm *VM) StringFromGo(s string) Value {
	return vm.NewString(wtf8.FromValidUTF8(s))
}
