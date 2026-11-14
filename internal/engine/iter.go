// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
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
		vm.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	return it, vm.GetV(it, vm.K.next)
}

// GetIterator implements GetIterator for a sync iterator and returns the iterator and its next method.
func (vm *VM) GetIterator(v Value) (it, next Value) {
	return vm.getIterator(v, false)
}

// IteratorStepFunc is the next method of a built-in iterator, without the iterator result:
// it returns the next value of the iterator this, or reports false when the iterator is done.
// It throws a TypeError when this is not an iterator of its kind.
type IteratorStepFunc func(vm *VM, this Value) (Value, bool)

// NewIteratorNext returns the next method of a built-in iterator, which returns the iterator
// result for what step returns.
func (vm *VM) NewIteratorNext(step IteratorStepFunc) *Object {
	o := vm.NewNative("next", 0, func(vm *VM, this Value, args []Value) Value {
		v, ok := step(vm, this)
		if !ok {
			return vm.CreateIterResultObject(Undefined, true)
		}
		return vm.CreateIterResultObject(v, false)
	})
	o.fn.step = step
	return o
}

// Outcomes of iterNext.
const (
	iterValue = iota
	iterDone
	iterFailed
)

// iterNext calls the next method next of the iterator it, as for-of and destructuring do.
// It returns the value with iterValue, or iterDone when the iterator is done. When the result
// is not an object or reading it throws, it returns iterFailed with the exception pending:
// the iterator then counts as finished. An exception that next throws propagates.
//
// The next methods of the built-in iterators and of generators run without creating
// the iterator result, whose reading would have no observable effect.
func (vm *VM) iterNext(it, next Value) (Value, int) {
	f := next.o
	var r Value
	switch {
	case f != nil && f.fn != nil && f.fn.step != nil:
		if vm.interrupt.Load() {
			vm.Poll()
		}
		v, ok := f.fn.step(vm, it)
		if !ok {
			return Undefined, iterDone
		}
		return v, iterValue
	case f != nil && f == vm.FnGeneratorNext && isSyncGenerator(it):
		if vm.interrupt.Load() {
			vm.Poll()
		}
		g := it.o.ext.(*generator)
		v, done := vm.resume(g, ResumeNext, Undefined)
		if done {
			return Undefined, iterDone
		}
		if !g.delegating {
			return v, iterValue
		}
		// yield* passes the result object of the inner iterator through.
		r = v
	default:
		r = vm.CallValue(next, it)
	}
	var v Value
	var done bool
	if !vm.protect(func() {
		if !r.IsObject() {
			vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
		}
		done = ToBoolean(vm.GetV(r, vm.K.done))
		if !done {
			v = vm.GetV(r, vm.K.Value)
		}
	}) {
		return Undefined, iterFailed
	}
	if done {
		return Undefined, iterDone
	}
	return v, iterValue
}

// IteratorStepValue implements IteratorStepValue for the iterator it and its next method next:
// it returns the next value, or reports false when the iterator is done.
func (vm *VM) IteratorStepValue(it, next Value) (Value, bool) {
	return vm.iteratorStep(it, next)
}

// IteratorStep implements IteratorStep for the iterator it and its next method next,
// without the result: it reports false when the iterator is done.
func (vm *VM) IteratorStep(it, next Value) bool {
	f := next.o
	if f != nil && f.fn != nil && f.fn.step != nil {
		if vm.interrupt.Load() {
			vm.Poll()
		}
		_, ok := f.fn.step(vm, it)
		return ok
	}
	r := vm.CallValue(next, it)
	if !r.IsObject() {
		vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
	}
	return !ToBoolean(vm.GetV(r, vm.K.done))
}

// iteratorStep calls next and returns the value of the result, or false when the iterator is done.
func (vm *VM) iteratorStep(it, next Value) (Value, bool) {
	v, st := vm.iterNext(it, next)
	if st == iterFailed {
		panic(errThrow)
	}
	return v, st == iterValue
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
		vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
	}
}

// IteratorClose implements IteratorClose for a normal completion.
func (vm *VM) IteratorClose(it Value) {
	vm.iteratorClose(it, false)
}

// IteratorCloseAll implements IteratorCloseAll for a normal completion:
// it closes the iterators its in reverse order.
func (vm *VM) IteratorCloseAll(its []Value) {
	for i, it := range slices.Backward(its) {
		if !vm.protect(func() {
			vm.iteratorClose(it, false)
		}) {
			for j := i - 1; j >= 0; j-- {
				vm.iteratorClose(its[j], true)
			}
			panic(errThrow)
		}
	}
}

// IfAbruptCloseIterator implements IfAbruptCloseIterator for what f does: when f throws,
// it closes the iterator it, ignoring any error of closing, and throws the exception again.
func (vm *VM) IfAbruptCloseIterator(it Value, f func()) {
	if vm.protect(f) {
		return
	}
	vm.iteratorClose(it, true)
	panic(errThrow)
}

// IfAbruptCloseIterators implements IfAbruptCloseIterators for what f does: when f throws,
// it closes the iterators its in reverse order, ignoring any error of closing, and throws
// the exception again.
func (vm *VM) IfAbruptCloseIterators(its []Value, f func()) {
	if vm.protect(f) {
		return
	}
	for _, it := range slices.Backward(its) {
		vm.iteratorClose(it, true)
	}
	panic(errThrow)
}

// Iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (vm *VM) Iterate(v Value, f func(Value) bool) {
	if v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
		o := v.o
		for i := uint32(0); i < o.length; i++ {
			e := Undefined
			if int(i) < len(o.elems) && !o.elems[i].isHole() {
				e = o.elems[i]
			} else {
				e = vm.Index(v, i)
			}
			if !f(e) {
				return
			}
		}
		return
	}
	it, next := vm.getIterator(v, false)
	vm.IterateRecord(it, next, f)
}

// IterateRecord is Iterate for the iterator record of the iterator it and its next method next.
func (vm *VM) IterateRecord(it, next Value, f func(Value) bool) {
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
		o.elems = append(o.elems, e)
		o.length = uint32(len(o.elems))
		return true
	})
}

// iterResult is an iterator result object together with the storage of its two properties,
// so that creating one is a single allocation.
type iterResult struct {
	Object
	props [2]Value
}

// CreateIterResultObject implements CreateIterResultObject.
func (vm *VM) CreateIterResultObject(v Value, done bool) Value {
	r := &iterResult{
		kind:  kindObject,
		shape: vm.iterResultShape,
		props: [2]Value{v, Bool(done)},
	}
	r.slots = r.props[:]
	return Value{o: &r.Object}
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
		o: &Object{
			kind:  kindObject,
			class: classForIn,
			ext:   it,
		},
	}
}

// forInNext returns the next key of the for-in iterator o.
func (vm *VM) forInNext(o *Object) (Value, bool) {
	it := o.ext.(*forIn)
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
			if k.key == nil {
				return vm.NewString(strconv.FormatUint(uint64(k.idx), 10)), true
			}
			return Value{o: k.key}, true
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
		kind:  kindObject,
		class: classArguments,
		shape: vm.argumentsShape,
		elems: vm.frameArgs(fr, regs),
	}
	o.slots = []Value{Number(float64(fr.argc)), {o: vm.FnArrayValues}}
	if mapped {
		vm.add(o, vm.K.callee, Value{o: fr.cl}, FlagsHidden)
	} else {
		vm.add(o, vm.K.callee, newAccessor(vm.ThrowTypeErrorFunc, vm.ThrowTypeErrorFunc), FlagAccessor)
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
			vm.add(a, vm.indexName(uint32(i)), v, FlagEnumerable)
		}
		a.length = uint32(len(vals))
		a.flags |= objSparse | objFrozenLength | objNotExtensible
		return a
	}
	raw := freeze(t.raw)
	raw.makePrivate()
	o := freeze(t.cooked)
	vm.add(o, vm.K.Raw, Value{o: raw}, 0)
	o.makePrivate()
	t.object = o
	return o
}

// StringFromGo returns the Go string s, which is taken to be UTF-8, as a value.
func (vm *VM) StringFromGo(s string) Value {
	return vm.NewString(wtf8.FromValidUTF8(s))
}

// iterReturn calls the return method of the iterator it without arguments, as
// AsyncIteratorClose does, and returns the result. It reports false when it is undefined
// or has no return method.
func (vm *VM) iterReturn(it Value) (Value, bool) {
	if it.IsUndefined() {
		return Undefined, false
	}
	m := vm.GetMethod(it, vm.K.ret)
	if m == nil {
		return Undefined, false
	}
	return vm.Call(m, it), true
}

func (vm *VM) throwNotIterResult(r Value) {
	vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
}

// iterResultValue returns the value of the iterator result r, or reports true when it is done.
func (vm *VM) iterResultValue(r Value) (Value, bool) {
	if !r.IsObject() {
		vm.throwNotIterResult(r)
	}
	if ToBoolean(vm.GetV(r, vm.K.done)) {
		return Undefined, true
	}
	return vm.GetV(r, vm.K.Value), false
}
