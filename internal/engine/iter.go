// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// getIterator implements GetIterator and returns the iterator and its next method.
func (cx *ExecutionContext) getIterator(v Value, async bool) (it, next Value) {
	var m *Object
	if async {
		m = cx.GetMethod(v, cx.WellKnownSymbol(SymbolAsyncIterator))
		if m == nil {
			sync, syncNext := cx.getIterator(v, false)
			return cx.asyncFromSyncIterator(sync, syncNext)
		}
	} else {
		m = cx.GetMethod(v, cx.WellKnownSymbol(SymbolIterator))
	}
	if m == nil {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not iterable")
	}
	it = cx.Call(m, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	next = cx.GetV(it, cx.Key(keyNext))
	if next.o == numberObject {
		// A number in the next method slot of the bytecode marks a plain array iterated by
		// index, so a numeric next method is held by a function that throws as calling it does.
		n := next
		next = Value{o: cx.NewNative("next", 0, func(cx *ExecutionContext, this Value, args []Value) Value {
			return cx.CallValue(n, this, args...)
		})}
	}
	return it, next
}

// GetIterator implements GetIterator for a sync iterator and returns the iterator and its next method.
func (cx *ExecutionContext) GetIterator(v Value) (it, next Value) {
	return cx.getIterator(v, false)
}

// GetIteratorFromMethod returns the iterator and next method obtained by calling method on v.
func (cx *ExecutionContext) GetIteratorFromMethod(v Value, method *Object) (it, next Value) {
	return cx.iteratorRecord(cx.Call(method, v))
}

// GetIteratorFlattenable returns an iterator and its next method, accepting strings when iterateStrings is set.
func (cx *ExecutionContext) GetIteratorFlattenable(v Value, iterateStrings bool) (it, next Value) {
	if !v.IsObject() && (!iterateStrings || !v.IsString()) {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not an iterator or an iterable object")
	}
	it = v
	if m := cx.GetMethod(v, cx.WellKnownSymbol(SymbolIterator)); m != nil {
		it = cx.Call(m, v)
	}
	return cx.iteratorRecord(it)
}

func (cx *ExecutionContext) iteratorRecord(it Value) (Value, Value) {
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	return it, cx.GetV(it, cx.Key(keyNext))
}

// IteratorStepFunc is the next method of a built-in iterator, without the iterator result:
// it returns the next value of the iterator this, or reports false when the iterator is done.
// It throws a TypeError when this is not an iterator of its kind.
type IteratorStepFunc func(cx *ExecutionContext, this Value) (Value, bool)

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

// IteratorPairStepFunc advances an entries iterator that yields pairs in its creation realm.
// It must decline incompatible state without advancing, and must neither throw nor run JavaScript.
type IteratorPairStepFunc func(state any) (a, b Value, st PairStep)

// SetIteratorPairStep registers the pair step of a built-in next method.
func (ag *agent) SetIteratorPairStep(next *Object, step IteratorPairStepFunc) {
	next.fn.info.fast.pairStep = step
}

// IteratorYieldRealm identifies where a built-in iterator creates a yielded result.
type IteratorYieldRealm uint8

const (
	// IteratorMethodRealm is the realm of the next method.
	IteratorMethodRealm IteratorYieldRealm = iota

	// IteratorClosureRealm is the creation realm of an iterator produced by CreateIteratorFromClosure.
	IteratorClosureRealm
)

// newIteratorNext creates the result-returning form of an iterator's next method.
func (cx *ExecutionContext) newIteratorNext(yieldRealm IteratorYieldRealm, step IteratorStepFunc) *Object {
	return cx.NewNative("next", 0, func(cx *ExecutionContext, this Value, args []Value) Value {
		v, ok := step(cx, this)
		if !ok {
			return cx.CreateIterResultObject(Undefined, true)
		}
		if yieldRealm == IteratorClosureRealm {
			return this.o.CreationContext().CreateIterResultObject(v, false)
		}
		return cx.CreateIterResultObject(v, false)
	})
}

// NewIteratorNext returns the next method of a built-in iterator, which returns the iterator
// result for what step returns.
func (cx *ExecutionContext) NewIteratorNext(yieldRealm IteratorYieldRealm, step IteratorStepFunc) *Object {
	o := cx.newIteratorNext(yieldRealm, step)
	o.fn.info.fast = &nativeFast{
		step: func(cx *ExecutionContext, this Value) (Value, bool) {
			cx.pushNative(o, this, nil)
			v, ok := step(cx, this)
			cx.popNative()
			return v, ok
		},
	}
	return o
}

// NewPlainIteratorNext returns a next method for iterators with state T.
// For a compatible receiver, step must neither throw nor run JavaScript.
func NewPlainIteratorNext[T any](cx *ExecutionContext, yieldRealm IteratorYieldRealm, receiverError string, step func(*ExecutionContext, *Object, T) (Value, bool)) *Object {
	next := cx.newIteratorNext(yieldRealm, func(cx *ExecutionContext, this Value) (Value, bool) {
		state, ok := IteratorState(this).(T)
		if !ok {
			cx.ThrowTypeError(receiverError)
		}
		return step(cx, this.o, state)
	})
	next.fn.info.fast = &nativeFast{
		step: func(cx *ExecutionContext, this Value) (Value, bool) {
			if state, ok := IteratorState(this).(T); ok {
				return step(cx, this.o, state)
			}
			// The receiver error belongs to the native next call.
			return cx.Call(next, this), false
		},
	}
	return next
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
func (cx *ExecutionContext) NewArrayIterator(o *Object, kind IterationKind) *Object {
	return cx.newArrayIterator(o, kind, 0)
}

func (cx *ExecutionContext) newArrayIterator(o *Object, kind IterationKind, nextIndex int64) *Object {
	return cx.NewIteratorObject(cx.Intrinsic(IntrinsicArrayIteratorPrototype), &arrayIterator{
		object:    o,
		nextIndex: nextIndex,
		kind:      kind,
	})
}

// NewArrayIteratorNext returns the next method of array iterators.
func (cx *ExecutionContext) NewArrayIteratorNext() *Object {
	next := cx.newIteratorNext(IteratorMethodRealm, func(cx *ExecutionContext, this Value) (Value, bool) {
		it, _ := IteratorState(this).(*arrayIterator)
		v, st := it.valueStep(cx, true)
		return v, st == PairYielded
	})
	next.fn.info.fast = &nativeFast{
		step: func(cx *ExecutionContext, this Value) (Value, bool) {
			it, _ := IteratorState(this).(*arrayIterator)
			v, st := it.valueStep(cx, false)
			if st == PairDeclined {
				cx.pushNative(next, this, nil)
				v, st = it.valueStep(cx, true)
				cx.popNative()
			}
			return v, st == PairYielded
		},
	}
	return next
}

func (cx *ExecutionContext) arrayIteratorPairStep(it, next Value) (Value, Value, PairStep) {
	state, _ := IteratorState(it).(*arrayIterator)
	if state == nil || state.kind != IterateEntries {
		return Undefined, Undefined, PairDeclined
	}
	key, value, st := state.step(cx, false)
	if st == PairDeclined {
		cx.pushNative(next.o, it, nil)
		key, value, st = state.step(cx, true)
		cx.popNative()
	}
	return key, value, st
}

func (it *arrayIterator) valueStep(cx *ExecutionContext, allowCalls bool) (Value, PairStep) {
	key, value, st := it.step(cx, allowCalls)
	if st != PairYielded {
		return Undefined, st
	}
	switch it.kind {
	case IterateKeys:
		return key, st
	case IterateValues:
		return value, st
	default:
		return Value{o: cx.NewArrayPair(key, value)}, st
	}
}

// step advances the iterator, declining before any observable operation when allowCalls is false.
func (it *arrayIterator) step(cx *ExecutionContext, allowCalls bool) (key, value Value, st PairStep) {
	if it == nil {
		if !allowCalls {
			return Undefined, Undefined, PairDeclined
		}
		cx.ThrowTypeError("next method called on an object that is not an array iterator")
	}
	o := it.object
	if o == nil {
		return Undefined, Undefined, PairDone
	}
	// Reading length can call next on this iterator again.
	i := it.nextIndex
	var length int64
	switch {
	case o.kind == kindArray:
		length = int64(o.length)
	case o.class == classTypedArray:
		t := o.extState().(*TypedArray)
		if it.kind != IterateKeys && i <= MaxArrayIndex {
			if b, ok := t.element(uint32(i)); ok {
				value = cx.loadElement(t.typ, b)
				it.nextIndex = i + 1
				cx.PollAt(i)
				return Number(float64(i)), value, PairYielded
			}
		}
		n, ok := t.Length()
		if !ok {
			if !allowCalls {
				return Undefined, Undefined, PairDeclined
			}
			cx.ThrowTypeError("cannot iterate a detached or out of bounds typed array")
		}
		length = int64(n)
	default:
		if !allowCalls {
			return Undefined, Undefined, PairDeclined
		}
		length = cx.LengthOfArrayLike(o)
	}
	if i >= length {
		it.object = nil
		return Undefined, Undefined, PairDone
	}
	key = Number(float64(i))
	if it.kind == IterateKeys {
		it.nextIndex = i + 1
		return key, Undefined, PairYielded
	}
	if i < int64(len(o.elems)) && !o.elems[i].isHole() {
		value = o.elems[i]
		it.nextIndex = i + 1
		cx.PollAt(i)
		return key, value, PairYielded
	}
	if !allowCalls {
		return Undefined, Undefined, PairDeclined
	}
	it.nextIndex = i + 1
	cx.Poll()
	return key, cx.Get(o, cx.ToPropertyKey(key)), PairYielded
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
func (cx *ExecutionContext) iterNext(it, next Value) (Value, int) {
	f := next.o
	var step IteratorStepFunc
	if f != nil && f.fn != nil {
		step = f.fn.iteratorStep(cx.realm)
	}
	var r Value
	switch {
	case step != nil:
		if cx.interrupt.Load() {
			cx.Poll()
		}
		v, ok := step(cx, it)
		if !ok {
			return Undefined, iterDone
		}
		return v, iterValue
	case f != nil && f == cx.Intrinsic(IntrinsicGeneratorPrototypeNext) && isSyncGenerator(it):
		if cx.interrupt.Load() {
			cx.Poll()
		}
		g := it.o.extState().(*generator)
		cx.pushNative(f, it, nil)
		v, done := cx.resume(g, ResumeNext, Undefined)
		cx.popNative()
		if done {
			return Undefined, iterDone
		}
		if !g.delegating {
			return v, iterValue
		}
		// yield* passes the result object of the inner iterator through.
		r = v
	default:
		r = cx.CallValue(next, it)
	}
	var v Value
	var done bool
	if !cx.protect(func() {
		if !r.IsObject() {
			cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
		}
		done = ToBoolean(cx.GetV(r, cx.Key(keyDone)))
		if !done {
			v = cx.GetV(r, cx.Key(KeyValue))
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
func (cx *ExecutionContext) IteratorStepValue(it, next Value) (Value, bool) {
	return cx.iteratorStep(it, next)
}

// IteratorStep implements IteratorStep for the iterator it and its next method next,
// without the result: it reports false when the iterator is done.
func (cx *ExecutionContext) IteratorStep(it, next Value) bool {
	if f := next.o; f != nil && f.fn != nil {
		if step := f.fn.iteratorStep(cx.realm); step != nil {
			if cx.interrupt.Load() {
				cx.Poll()
			}
			_, ok := step(cx, it)
			return ok
		}
	}
	r := cx.CallValue(next, it)
	if !r.IsObject() {
		cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
	}
	return !ToBoolean(cx.GetV(r, cx.Key(keyDone)))
}

// iteratorStep calls next and returns the value of the result, or false when the iterator is done.
func (cx *ExecutionContext) iteratorStep(it, next Value) (Value, bool) {
	v, st := cx.iterNext(it, next)
	if st == iterFailed {
		panic(errThrow)
	}
	return v, st == iterValue
}

// iteratorClose implements IteratorClose. With pending set, an exception is already
// being propagated and the errors of closing are dropped.
func (cx *ExecutionContext) iteratorClose(it Value, pending bool) {
	if pending {
		saved, site := cx.thrown, cx.thrownAt
		cx.protect(func() {
			if m := cx.GetMethod(it, cx.Key(keyReturn)); m != nil {
				cx.Call(m, it)
			}
		})
		cx.thrown, cx.thrownAt = saved, site
		return
	}
	m := cx.GetMethod(it, cx.Key(keyReturn))
	if m == nil {
		return
	}
	if r := cx.Call(m, it); !r.IsObject() {
		cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
	}
}

// IteratorClose implements IteratorClose for a normal completion.
func (cx *ExecutionContext) IteratorClose(it Value) {
	cx.iteratorClose(it, false)
}

// IteratorCloseAll implements IteratorCloseAll for a normal completion:
// it closes the iterators its in reverse order, skipping undefined entries.
func (cx *ExecutionContext) IteratorCloseAll(its []Value) {
	cx.iteratorCloseAll(its, false)
}

// IfAbruptCloseIterator implements IfAbruptCloseIterator for what f does: when f throws,
// it closes the iterator it, ignoring any error of closing, and throws the exception again.
func (cx *ExecutionContext) IfAbruptCloseIterator(it Value, f func()) {
	if cx.protect(f) {
		return
	}
	cx.iteratorClose(it, true)
	panic(errThrow)
}

// IfAbruptCloseIterators implements IfAbruptCloseIterators for what f does: when f throws,
// it closes the iterators its in reverse order, skipping undefined entries, and rethrows
// the original exception.
func (cx *ExecutionContext) IfAbruptCloseIterators(its []Value, f func()) {
	if cx.protect(f) {
		return
	}
	cx.iteratorCloseAll(its, true)
}

func (cx *ExecutionContext) iteratorCloseAll(its []Value, pending bool) {
	for _, it := range slices.Backward(its) {
		if it.IsUndefined() {
			continue
		}
		if pending {
			cx.iteratorClose(it, true)
		} else {
			pending = !cx.protect(func() {
				cx.iteratorClose(it, false)
			})
		}
	}
	if pending {
		panic(errThrow)
	}
}

// closePlainArrayIterator materializes an array iterator at nextIndex if looking up return
// can expose it to JavaScript.
func (cx *ExecutionContext) closePlainArrayIterator(o *Object, nextIndex uint32, pending bool) {
	key := cx.Key(keyReturn)
	for p := cx.Intrinsic(IntrinsicArrayIteratorPrototype); p != nil; p = p.shape.proto {
		if _, ok := p.getOwn(key); ok || p.flags&objExotic != 0 {
			it := cx.newArrayIterator(o, IterateValues, int64(nextIndex))
			cx.iteratorClose(Value{o: it}, pending)
			return
		}
	}
}

// Iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (cx *ExecutionContext) Iterate(v Value, f func(Value) bool) {
	if v.o != nil && v.o.kind == kindArray && cx.arrayIterationPlain(v.o) {
		cx.iteratePlainArray(v.o, f)
		return
	}
	it, next := cx.getIterator(v, false)
	cx.IterateRecord(it, next, f)
}

// IterateFromMethod is [ExecutionContext.Iterate] for the iterator that GetIteratorFromMethod(v, method)
// returns.
func (cx *ExecutionContext) IterateFromMethod(v Value, method *Object, f func(Value) bool) {
	if method == cx.Intrinsic(IntrinsicArrayPrototypeValues) && v.o != nil && v.o.kind == kindArray && cx.arrayIterationPlain(v.o) {
		cx.iteratePlainArray(v.o, f)
		return
	}
	it := cx.Call(method, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	cx.IterateRecord(it, cx.GetV(it, cx.Key(keyNext)), f)
}

// iteratePlainArray iterates o by index with the original array values and next methods.
func (cx *ExecutionContext) iteratePlainArray(o *Object, f func(Value) bool) {
	var nextIndex uint32
	cx.iterate(func() (Value, bool) {
		i := nextIndex
		if i >= o.length {
			return Undefined, false
		}
		if cx.interrupt.Load() {
			cx.Poll()
		}
		nextIndex++
		if uint(i) < uint(len(o.elems)) && !o.elems[i].isHole() {
			return o.elems[i], true
		}
		return cx.plainArrayIteratorIndex(o, i), true
	}, func(pending bool) {
		cx.closePlainArrayIterator(o, nextIndex, pending)
	}, f)
}

// plainArrayIteratorIndex reads an element outside dense storage with the native next frame
// that a getter or proxy can observe.
func (cx *ExecutionContext) plainArrayIteratorIndex(o *Object, i uint32) Value {
	if o.flags&objSparse == 0 && cx.holesAreAbsent(o) {
		return Undefined
	}
	array := Value{o: o}
	// A native frame uses its receiver only to name it in a stack trace. An indexed
	// iterator has no own properties, so its prototype supplies the same name.
	proto := cx.Intrinsic(IntrinsicArrayIteratorPrototype)
	cx.pushNative(cx.Intrinsic(IntrinsicArrayIteratorPrototypeNext), Value{o: proto}, nil)
	v := cx.Index(array, i)
	cx.popNative()
	return v
}

// IterateRecord is [ExecutionContext.Iterate] for the iterator record of the iterator it and its next method
// next.
func (cx *ExecutionContext) IterateRecord(it, next Value, f func(Value) bool) {
	cx.iterate(func() (Value, bool) {
		return cx.iteratorStep(it, next)
	}, func(pending bool) {
		cx.iteratorClose(it, pending)
	}, f)
}

func (cx *ExecutionContext) iterate(step func() (Value, bool), closeIterator func(pending bool), f func(Value) bool) {
	// Only a callback that stops or throws closes the iterator.
	var closeNeeded bool
	ok := cx.protect(func() {
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
func (cx *ExecutionContext) spreadArgs(v Value) *Object {
	if s := v.o; s != nil && s.kind == kindArray && cx.arrayIterationPlain(s) {
		if _, ok := s.plainElems(); ok {
			return s
		}
	}
	o := cx.NewArray(nil)
	cx.appendIterable(o, v)
	return o
}

// appendIterable appends the values that iterating v yields to the array o.
func (cx *ExecutionContext) appendIterable(o *Object, v Value) {
	if s := v.o; s != nil && s.kind == kindArray {
		o.elems = reserveElems(o.elems, len(s.elems))
	}
	cx.Iterate(v, func(e Value) bool {
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

// CreateIterResultObject creates an iterator result in the execution realm.
func (cx *ExecutionContext) CreateIterResultObject(v Value, done bool) Value {
	result := &iterResult{
		kind:  kindObject,
		shape: cx.realm.shapes.iterResult,
		props: [2]Value{v, Bool(done)},
	}
	result.slots = result.props[:]
	return Value{o: result.init()}
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
func (cx *ExecutionContext) newForIn(v Value) Value {
	it := &forIn{
		kind:  kindObject,
		class: classForIn,
	}
	it.setExtState(it)
	if !v.IsNullish() {
		it.obj = cx.ToObject(v)
		it.indices, it.length, it.keys = cx.forInOwnKeys(it.obj)
	}
	return Value{o: it.init()}
}

// forInOwnKeys returns the own property keys of the receiver o of a for-in loop in the order of
// [ExecutionContext.OwnKeys], where symbols may be left out: the indices below indices, then the key length
// if length is set, then keys.
func (cx *ExecutionContext) forInOwnKeys(o *Object) (indices uint32, length bool, keys []PropKey) {
	if o.class == classProxy || o.class == classModuleNamespace {
		return 0, false, cx.OwnKeys(o)
	}
	if o.flags&objExotic != 0 {
		cx.resolve(o)
	}
	// The own keys of any other object are its elements, the length of an array, and the
	// keys of its shape.
	if o.flags&(objExotic|objSparse) != 0 || o.class == classString || slices.ContainsFunc(o.elems, Value.isHole) {
		return 0, false, cx.OwnKeys(o)
	}
	return uint32(len(o.elems)), o.kind == kindArray, o.shape.stringKeys()
}

// forInNext returns the next key of the for-in iterator o.
func (cx *ExecutionContext) forInNext(o *Object) (Value, bool) {
	it := o.extState().(*forIn)
	for it.obj != nil {
		for it.index < it.indices {
			i := it.index
			it.index++
			if obj := it.obj; int64(i) < int64(len(obj.elems)) && !obj.elems[i].isHole() {
				return cx.indexString(i), true
			}
			k := PropKey{idx: i}
			d, ok := cx.GetOwnProperty(it.obj, k)
			if !ok {
				it.missing = append(it.missing, k)
				continue
			}
			if d.flags&FlagEnumerable != 0 {
				return cx.indexString(i), true
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
			d, ok := cx.GetOwnProperty(it.obj, k)
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
			return cx.KeyValue(k), true
		}
		cx.forInNextObject(it)
	}
	return Undefined, false
}

// forInNextObject moves the for-in iterator it from the object whose keys it visited to the
// prototype of that object.
func (cx *ExecutionContext) forInNextObject(it *forIn) {
	proto := cx.ProtoOf(it.obj)
	if !cx.forInMayVisit(proto) {
		it.obj = nil
		return
	}
	if it.visited == nil {
		cx.forInVisitReceiver(it)
	}
	it.obj = proto
	it.index, it.indices = 0, 0
	it.keys = cx.OwnKeys(proto)
	it.pos = 0
}

// forInMayVisit reports whether a for-in loop might visit a key of o or of its prototypes. When
// it reports false, visiting the keys of those objects would have no observable effect.
func (cx *ExecutionContext) forInMayVisit(o *Object) bool {
	for ; o != nil; o = o.shape.proto {
		if o.class == classProxy || o.class == classModuleNamespace {
			return true
		}
		if o.flags&objExotic != 0 {
			cx.resolve(o)
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
func (ag *agent) forInVisitReceiver(it *forIn) {
	it.visited = make(map[PropKey]struct{}, int(it.indices)+len(it.keys)+1)
	for i := range it.indices {
		it.visited[PropKey{idx: i}] = struct{}{}
	}
	if it.length {
		it.visited[PropKey{key: ag.Key(KeyLength)}] = struct{}{}
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
func (cx *ExecutionContext) templateObject(t *templateSite) *Object {
	if t.object != nil {
		return t.object
	}
	freeze := func(vals []Value) *Object {
		a := cx.NewArray(nil)
		for i, v := range vals {
			cx.add(a, cx.indexName(uint32(i)), v, FlagEnumerable)
		}
		a.length = uint32(len(vals))
		a.flags |= objSparse | objFrozenLength | objNotExtensible
		return a
	}
	raw := freeze(t.raw)
	raw.makePrivate()
	o := freeze(t.cooked)
	cx.add(o, cx.Key(KeyRaw), Value{o: raw}, 0)
	o.makePrivate()
	t.object = o
	return o
}

// StringFromGo returns the Go string s, which is taken to be UTF-8, as a value.
func (cx *ExecutionContext) StringFromGo(s string) Value {
	return cx.NewString(wtf8.FromValidUTF8(s))
}

// iterReturn calls the return method of the iterator it without arguments, as
// AsyncIteratorClose does, and returns the result. It reports false when it is undefined
// or has no return method.
func (cx *ExecutionContext) iterReturn(it Value) (Value, bool) {
	if it.IsUndefined() {
		return Undefined, false
	}
	m := cx.GetMethod(it, cx.Key(keyReturn))
	if m == nil {
		return Undefined, false
	}
	return cx.Call(m, it), true
}

func (cx *ExecutionContext) throwNotIterResult(r Value) {
	cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
}

// iterResultValue returns the value of the iterator result r, or reports true when it is done.
func (cx *ExecutionContext) iterResultValue(r Value) (Value, bool) {
	if !r.IsObject() {
		cx.throwNotIterResult(r)
	}
	if ToBoolean(cx.GetV(r, cx.Key(keyDone))) {
		return Undefined, true
	}
	return cx.GetV(r, cx.Key(KeyValue)), false
}

// IteratorToList implements IteratorToList(GetIteratorFromMethod(v, method)).
func (cx *ExecutionContext) IteratorToList(v Value, method *Object) []Value {
	if method == cx.Intrinsic(IntrinsicArrayPrototypeValues) && v.o != nil && v.o.kind == kindArray && cx.arrayIterationPlain(v.o) {
		if n := v.o.length; uint64(n) <= uint64(len(v.o.elems)) && !slices.ContainsFunc(v.o.elems[:n], Value.isHole) {
			return slices.Clone(v.o.elems[:n])
		}
		// The length of a sparse array says nothing about the memory it holds.
		vals := make([]Value, 0, len(v.o.elems))
		cx.iteratePlainArray(v.o, func(e Value) bool {
			vals = append(vals, e)
			return true
		})
		return vals
	}
	it := cx.Call(method, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	var vals []Value
	cx.IterateRecord(it, cx.GetV(it, cx.Key(keyNext)), func(e Value) bool {
		vals = append(vals, e)
		return true
	})
	return vals
}
