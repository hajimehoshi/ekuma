// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// getIterator implements GetIterator and returns the iterator and its next method.
func (vm *VM) getIterator(v Value, async bool) (it, next Value) {
	var m *Object
	if async {
		m = vm.GetMethod(v, vm.WellKnownSymbol(SymbolAsyncIterator))
		if m == nil {
			sync, syncNext := vm.getIterator(v, false)
			return vm.asyncFromSyncIterator(sync, syncNext)
		}
	} else {
		m = vm.GetMethod(v, vm.WellKnownSymbol(SymbolIterator))
	}
	if m == nil {
		vm.ThrowTypeError(vm.DisplayString(v) + " is not iterable")
	}
	it = vm.Call(m, v)
	if !it.IsObject() {
		vm.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	next = vm.GetV(it, vm.Key(keyNext))
	if next.o == numberObject {
		// A number in the next method slot of the bytecode marks a plain array iterated by
		// index, so a numeric next method is held by a function that throws as calling it does.
		n := next
		next = Value{o: vm.NewNative("next", 0, func(vm *VM, this Value, args []Value) Value {
			return vm.CallValue(n, this, args...)
		})}
	}
	return it, next
}

// GetIterator implements GetIterator for a sync iterator and returns the iterator and its next method.
func (vm *VM) GetIterator(v Value) (it, next Value) {
	return vm.getIterator(v, false)
}

// IteratorStepFunc is the next method of a built-in iterator, without the iterator result:
// it returns the next value of the iterator this, or reports false when the iterator is done.
// It throws a TypeError when this is not an iterator of its kind.
type IteratorStepFunc func(vm *VM, this Value) (Value, bool)

// PairStep is the outcome of an [IteratorPairStepFunc].
type PairStep uint8

const (
	// PairDeclined says that the step did nothing.
	PairDeclined PairStep = iota

	// PairDone says that the iterator is done.
	PairDone

	// PairYielded says that the iterator yielded an array of the two values.
	PairYielded
)

// IteratorPairStepFunc is the next method of a built-in iterator, without the iterator result,
// for an iterator that yields arrays of two elements: it returns the elements of the next array
// that the iterator this yields, without creating the array. It declines when the iterator does
// not yield such arrays, when this is not an iterator of its kind, or when stepping the iterator
// could run JavaScript.
type IteratorPairStepFunc func(vm *VM, this Value) (a, b Value, st PairStep)

// SetIteratorPairStep makes step the pair step of next, which NewIteratorNext returned.
func (vm *VM) SetIteratorPairStep(next *Object, step IteratorPairStepFunc) {
	next.fn.info.fast.pairStep = step
}

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
	o.fn.info.fast = &nativeFast{
		step: step,
	}
	return o
}

// IterationKind identifies what an iterator yields.
type IterationKind uint8

const (
	IterateKeys IterationKind = iota
	IterateValues
	IterateEntries
)

type arrayIterator struct {
	object    *Object
	nextIndex int64
	kind      IterationKind
}

// NewArrayIterator returns an array iterator over the array-like object o.
func (vm *VM) NewArrayIterator(o *Object, kind IterationKind) *Object {
	return vm.newArrayIterator(o, kind, 0)
}

func (vm *VM) newArrayIterator(o *Object, kind IterationKind, nextIndex int64) *Object {
	return vm.NewIteratorObject(vm.Intrinsic(IntrinsicArrayIteratorPrototype), &arrayIterator{
		object:    o,
		nextIndex: nextIndex,
		kind:      kind,
	})
}

// ArrayIteratorStep advances the array iterator this and reports whether it yielded a value.
func (vm *VM) ArrayIteratorStep(this Value) (Value, bool) {
	it, _ := IteratorState(this).(*arrayIterator)
	if it == nil {
		vm.ThrowTypeError("next method called on an object that is not an array iterator")
	}
	o := it.object
	if o == nil {
		return Undefined, false
	}
	// Reading length can call next on this iterator again.
	i := it.nextIndex
	var length int64
	if t := TypedArrayData(Value{o: o}); t != nil {
		n, ok := t.Length()
		if !ok {
			vm.ThrowTypeError("cannot iterate a detached or out of bounds typed array")
		}
		length = int64(n)
	} else {
		length = vm.LengthOfArrayLike(o)
	}
	if i >= length {
		it.object = nil
		return Undefined, false
	}
	it.nextIndex = i + 1
	key := Number(float64(i))
	if it.kind == IterateKeys {
		return key, true
	}
	v, ok := o.Element(i)
	if ok {
		vm.PollAt(i)
	} else {
		vm.Poll()
		v = vm.Get(o, vm.ToPropertyKey(key))
	}
	if it.kind == IterateValues {
		return v, true
	}
	return Value{o: vm.NewArrayPair(key, v)}, true
}

// ArrayIteratorPairStep is the pair step of array iterators, which yield arrays of two elements
// as entries.
func (vm *VM) ArrayIteratorPairStep(this Value) (a, b Value, st PairStep) {
	it, _ := IteratorState(this).(*arrayIterator)
	if it == nil || it.kind != IterateEntries {
		return Undefined, Undefined, PairDeclined
	}
	o := it.object
	if o == nil {
		return Undefined, Undefined, PairDone
	}
	// Only the length and the elements of an array are read without running anything.
	if o.kind != kindArray {
		return Undefined, Undefined, PairDeclined
	}
	i := it.nextIndex
	if i >= int64(o.length) {
		it.object = nil
		return Undefined, Undefined, PairDone
	}
	v, ok := o.Element(i)
	if !ok {
		return Undefined, Undefined, PairDeclined
	}
	it.nextIndex = i + 1
	vm.PollAt(i)
	return Number(float64(i)), v, PairYielded
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
	var step IteratorStepFunc
	if f != nil && f.fn != nil {
		step = f.fn.iteratorStep(vm.curRealm)
	}
	var r Value
	switch {
	case step != nil:
		if vm.interrupt.Load() {
			vm.Poll()
		}
		v, ok := step(vm, it)
		if !ok {
			return Undefined, iterDone
		}
		return v, iterValue
	case f != nil && f == vm.Intrinsic(IntrinsicGeneratorPrototypeNext) && isSyncGenerator(it):
		if vm.interrupt.Load() {
			vm.Poll()
		}
		g := it.o.extState().(*generator)
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
		done = ToBoolean(vm.GetV(r, vm.Key(keyDone)))
		if !done {
			v = vm.GetV(r, vm.Key(KeyValue))
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
	if f := next.o; f != nil && f.fn != nil {
		if step := f.fn.iteratorStep(vm.curRealm); step != nil {
			if vm.interrupt.Load() {
				vm.Poll()
			}
			vm.pushNative(f, it, nil)
			_, ok := step(vm, it)
			vm.popNative()
			return ok
		}
	}
	r := vm.CallValue(next, it)
	if !r.IsObject() {
		vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
	}
	return !ToBoolean(vm.GetV(r, vm.Key(keyDone)))
}

// iteratorStep calls next and returns the value of the result, or false when the iterator is done.
//
// The step function of a built-in iterator, which counts as a native call here, may step
// another iterator in turn.
func (vm *VM) iteratorStep(it, next Value) (Value, bool) {
	var v Value
	var st int
	if f := next.o; f != nil && f.fn != nil && f.fn.iteratorStep(vm.curRealm) != nil {
		vm.pushNative(f, it, nil)
		v, st = vm.iterNext(it, next)
		vm.popNative()
	} else {
		v, st = vm.iterNext(it, next)
	}
	if st == iterFailed {
		panic(errThrow)
	}
	return v, st == iterValue
}

// iteratorClose implements IteratorClose. With pending set, an exception is already
// being propagated and the errors of closing are dropped.
func (vm *VM) iteratorClose(it Value, pending bool) {
	if pending {
		saved, site := vm.thrown, vm.thrownAt
		vm.protect(func() {
			if m := vm.GetMethod(it, vm.Key(keyReturn)); m != nil {
				vm.Call(m, it)
			}
		})
		vm.thrown, vm.thrownAt = saved, site
		return
	}
	m := vm.GetMethod(it, vm.Key(keyReturn))
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
// it closes the iterators its in reverse order, skipping undefined entries.
func (vm *VM) IteratorCloseAll(its []Value) {
	vm.iteratorCloseAll(its, false)
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
// it closes the iterators its in reverse order, skipping undefined entries, and rethrows
// the original exception.
func (vm *VM) IfAbruptCloseIterators(its []Value, f func()) {
	if vm.protect(f) {
		return
	}
	vm.iteratorCloseAll(its, true)
}

func (vm *VM) iteratorCloseAll(its []Value, pending bool) {
	for _, it := range slices.Backward(its) {
		if it.IsUndefined() {
			continue
		}
		if pending {
			vm.iteratorClose(it, true)
		} else {
			pending = !vm.protect(func() {
				vm.iteratorClose(it, false)
			})
		}
	}
	if pending {
		panic(errThrow)
	}
}

// closePlainArrayIterator materializes an array iterator at nextIndex if looking up return
// can expose it to JavaScript.
func (vm *VM) closePlainArrayIterator(o *Object, nextIndex uint32, pending bool) {
	key := vm.Key(keyReturn)
	for p := vm.Intrinsic(IntrinsicArrayIteratorPrototype); p != nil; p = p.shape.proto {
		if _, ok := p.getOwn(key); ok || p.flags&objExotic != 0 {
			it := vm.newArrayIterator(o, IterateValues, int64(nextIndex))
			vm.iteratorClose(Value{o: it}, pending)
			return
		}
	}
}

// Iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (vm *VM) Iterate(v Value, f func(Value) bool) {
	if v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
		vm.iteratePlainArray(v.o, f)
		return
	}
	it, next := vm.getIterator(v, false)
	vm.IterateRecord(it, next, f)
}

// IterateFromMethod is [VM.Iterate] for the iterator that GetIteratorFromMethod(v, method)
// returns.
func (vm *VM) IterateFromMethod(v Value, method *Object, f func(Value) bool) {
	if method == vm.Intrinsic(IntrinsicArrayPrototypeValues) && v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
		vm.iteratePlainArray(v.o, f)
		return
	}
	it := vm.Call(method, v)
	if !it.IsObject() {
		vm.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	vm.IterateRecord(it, vm.GetV(it, vm.Key(keyNext)), f)
}

// iteratePlainArray iterates o by index with the original array values and next methods.
func (vm *VM) iteratePlainArray(o *Object, f func(Value) bool) {
	var nextIndex uint32
	vm.iterate(func() (Value, bool) {
		i := nextIndex
		if i >= o.length {
			return Undefined, false
		}
		if vm.interrupt.Load() {
			vm.Poll()
		}
		nextIndex++
		if uint(i) < uint(len(o.elems)) && !o.elems[i].isHole() {
			return o.elems[i], true
		}
		return vm.Index(Value{o: o}, i), true
	}, func(pending bool) {
		vm.closePlainArrayIterator(o, nextIndex, pending)
	}, f)
}

// IterateRecord is [VM.Iterate] for the iterator record of the iterator it and its next method
// next.
func (vm *VM) IterateRecord(it, next Value, f func(Value) bool) {
	vm.iterate(func() (Value, bool) {
		return vm.iteratorStep(it, next)
	}, func(pending bool) {
		vm.iteratorClose(it, pending)
	}, f)
}

func (vm *VM) iterate(step func() (Value, bool), closeIterator func(pending bool), f func(Value) bool) {
	// Only a callback that stops or throws closes the iterator.
	var closeNeeded bool
	ok := vm.protect(func() {
		for {
			e, ok := step()
			if !ok {
				return
			}
			closeNeeded = true
			if !f(e) {
				return
			}
			closeNeeded = false
		}
	})
	if closeNeeded {
		closeIterator(!ok)
	}
	if !ok {
		panic(errThrow)
	}
}

// spreadArgs returns an array of the values that iterating v yields, for a spread call that
// copies them before running any JavaScript. The array is v itself when iterating v reads its
// elements without running JavaScript.
func (vm *VM) spreadArgs(v Value) *Object {
	if s := v.o; s != nil && s.kind == kindArray && vm.arrayIterationPlain(s) {
		if _, ok := s.plainElems(); ok {
			return s
		}
	}
	o := vm.NewArray(nil)
	vm.appendIterable(o, v)
	return o
}

// appendIterable appends the values that iterating v yields to the array o.
func (vm *VM) appendIterable(o *Object, v Value) {
	if s := v.o; s != nil && s.kind == kindArray {
		o.elems = reserveElems(o.elems, len(s.elems))
	}
	vm.Iterate(v, func(e Value) bool {
		o.elems = appendElem(o.elems, e)
		o.length = uint32(len(o.elems))
		return true
	})
}

// iterResult is an iterator result object together with the storage of its two properties,
// so that creating one is a single allocation.
type iterResult struct {
	propertyObject
	props [2]Value
}

// CreateIterResultObject implements CreateIterResultObject.
func (vm *VM) CreateIterResultObject(v Value, done bool) Value {
	r := &iterResult{
		kind:  kindObject,
		shape: vm.shapes.iterResult,
		props: [2]Value{v, Bool(done)},
	}
	r.slots = r.props[:]
	return Value{o: r.init()}
}

// forIn is a for-in iterator object together with the state of its loop, so that creating
// one is a single allocation.
type forIn struct {
	propertyObject

	// obj is the object whose keys come next, or nil when the loop is done.
	obj *Object

	// The keys of obj that are left are the indices from index on that are below indices,
	// which are elements of the receiver, followed by keys[pos:].
	index, indices uint32
	keys           []PropKey
	pos            int

	// visited holds the keys that were visited, once obj is a prototype, and is nil while obj
	// is the receiver. The keys visited on the receiver are its keys above and the key length
	// if length is set, less those in missing, which the receiver no longer had when they
	// came up.
	visited map[PropKey]struct{}
	length  bool
	missing []PropKey
}

// newForIn returns the iterator of a for-in loop over v.
func (vm *VM) newForIn(v Value) Value {
	it := &forIn{
		kind:  kindObject,
		class: classForIn,
	}
	it.setExtState(it)
	if !v.IsNullish() {
		it.obj = vm.ToObject(v)
		it.indices, it.length, it.keys = vm.forInOwnKeys(it.obj)
	}
	return Value{o: it.init()}
}

// forInOwnKeys returns the own property keys of the receiver o of a for-in loop in the order of
// [VM.OwnKeys], where symbols may be left out: the indices below indices, then the key length
// if length is set, then keys.
func (vm *VM) forInOwnKeys(o *Object) (indices uint32, length bool, keys []PropKey) {
	if o.class == classProxy || o.class == classModuleNamespace {
		return 0, false, vm.OwnKeys(o)
	}
	if o.flags&objExotic != 0 {
		vm.resolve(o)
	}
	// The own keys of any other object are its elements, the length of an array, and the
	// keys of its shape.
	if o.flags&(objExotic|objSparse) != 0 || o.class == classString || slices.ContainsFunc(o.elems, Value.isHole) {
		return 0, false, vm.OwnKeys(o)
	}
	return uint32(len(o.elems)), o.kind == kindArray, o.shape.stringKeys()
}

// forInNext returns the next key of the for-in iterator o.
func (vm *VM) forInNext(o *Object) (Value, bool) {
	it := o.extState().(*forIn)
	for it.obj != nil {
		for it.index < it.indices {
			i := it.index
			it.index++
			if obj := it.obj; int64(i) < int64(len(obj.elems)) && !obj.elems[i].isHole() {
				return vm.indexString(i), true
			}
			k := PropKey{idx: i}
			d, ok := vm.GetOwnProperty(it.obj, k)
			if !ok {
				it.missing = append(it.missing, k)
				continue
			}
			if d.flags&FlagEnumerable != 0 {
				return vm.indexString(i), true
			}
		}
		for it.pos < len(it.keys) {
			k := it.keys[it.pos]
			it.pos++
			if k.IsSymbol() {
				continue
			}
			if it.visited != nil {
				if _, seen := it.visited[k]; seen {
					continue
				}
			}
			// A property that was deleted in the meantime is skipped, and one that is not
			// enumerable still hides the properties of the same name further up.
			d, ok := vm.GetOwnProperty(it.obj, k)
			if !ok {
				if it.visited == nil {
					it.missing = append(it.missing, k)
				}
				continue
			}
			if it.visited != nil {
				it.visited[k] = struct{}{}
			}
			if d.flags&FlagEnumerable == 0 {
				continue
			}
			return vm.KeyValue(k), true
		}
		vm.forInNextObject(it)
	}
	return Undefined, false
}

// forInNextObject moves the for-in iterator it from the object whose keys it visited to the
// prototype of that object.
func (vm *VM) forInNextObject(it *forIn) {
	proto := vm.ProtoOf(it.obj)
	if !vm.forInMayVisit(proto) {
		it.obj = nil
		return
	}
	if it.visited == nil {
		vm.forInVisitReceiver(it)
	}
	it.obj = proto
	it.index, it.indices = 0, 0
	it.keys = vm.OwnKeys(proto)
	it.pos = 0
}

// forInMayVisit reports whether a for-in loop might visit a key of o or of its prototypes. When
// it reports false, visiting the keys of those objects would have no observable effect.
func (vm *VM) forInMayVisit(o *Object) bool {
	for ; o != nil; o = o.shape.proto {
		if o.class == classProxy || o.class == classModuleNamespace {
			return true
		}
		if o.flags&objExotic != 0 {
			vm.resolve(o)
			if o.flags&objExotic != 0 {
				return true
			}
		}
		if o.class == classString && o.extState().(*Object).StringLength() > 0 {
			return true
		}
		if slices.ContainsFunc(o.elems, func(e Value) bool { return !e.isHole() }) {
			return true
		}
		if o.shape.hasEnumerableString() {
			return true
		}
	}
	return false
}

// forInVisitReceiver records the keys that the for-in iterator it visited on the receiver,
// which it is done with, in it.visited.
func (vm *VM) forInVisitReceiver(it *forIn) {
	it.visited = make(map[PropKey]struct{}, int(it.indices)+len(it.keys)+1)
	for i := range it.indices {
		it.visited[PropKey{idx: i}] = struct{}{}
	}
	if it.length {
		it.visited[PropKey{key: vm.Key(KeyLength)}] = struct{}{}
	}
	for _, k := range it.keys {
		if !k.IsSymbol() {
			it.visited[k] = struct{}{}
		}
	}
	for _, k := range it.missing {
		delete(it.visited, k)
	}
	it.missing = nil
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
	vm.add(o, vm.Key(KeyRaw), Value{o: raw}, 0)
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
	m := vm.GetMethod(it, vm.Key(keyReturn))
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
	if ToBoolean(vm.GetV(r, vm.Key(keyDone))) {
		return Undefined, true
	}
	return vm.GetV(r, vm.Key(KeyValue)), false
}

// IteratorToList implements IteratorToList(GetIteratorFromMethod(v, method)).
func (vm *VM) IteratorToList(v Value, method *Object) []Value {
	if method == vm.Intrinsic(IntrinsicArrayPrototypeValues) && v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
		if n := v.o.length; uint64(n) <= uint64(len(v.o.elems)) && !slices.ContainsFunc(v.o.elems[:n], Value.isHole) {
			return slices.Clone(v.o.elems[:n])
		}
		// The length of a sparse array says nothing about the memory it holds.
		vals := make([]Value, 0, len(v.o.elems))
		vm.iteratePlainArray(v.o, func(e Value) bool {
			vals = append(vals, e)
			return true
		})
		return vals
	}
	it := vm.Call(method, v)
	if !it.IsObject() {
		vm.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	var vals []Value
	vm.IterateRecord(it, vm.GetV(it, vm.Key(keyNext)), func(e Value) bool {
		vals = append(vals, e)
		return true
	})
	return vals
}
