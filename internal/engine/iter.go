// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/storage"
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
	return cx.iteratorRecord(cx.Call(m, v))
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
	return it, cx.GetV(it, cx.Key(KeyNext))
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

	// PairYielded says that the iterator yielded the two values of a pair.
	PairYielded
)

// IteratorPairStepFunc advances an entries iterator that yields pairs in its creation realm.
// It must decline incompatible state without advancing, and must neither throw nor run JavaScript.
type IteratorPairStepFunc func(state any) (a, b Value, st PairStep)

// IteratorYieldRealm identifies where a built-in iterator creates a yielded result.
type IteratorYieldRealm uint8

const (
	// IteratorMethodRealm is the realm of the next method.
	IteratorMethodRealm IteratorYieldRealm = iota

	// IteratorClosureRealm is the creation realm of an iterator produced by CreateIteratorFromClosure.
	IteratorClosureRealm
)

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

// nativeArrayIteratorNext is the next method of array iterators.
var nativeArrayIteratorNext = registerNative(arrayIteratorNext, &nativeFast{
	step:       arrayIteratorFastStep,
	arrayIndex: arrayIteratorIndex,
	contract: ShortcutContract{
		Frame: FrameRecorded,
		Pace:  PacePolled,
	},
})

// NewArrayIteratorNext returns the next method of array iterators.
func (cx *ExecutionContext) NewArrayIteratorNext() *Object {
	return cx.NewNative("next", 0, nativeArrayIteratorNext)
}

// arrayIteratorNext is the code of the next method of array iterators.
func arrayIteratorNext(cx *ExecutionContext, this Value, args []Value) Value {
	it, _ := storage.IteratorState(this).(*arrayIterator)
	key, value, st := it.stepPlain(cx.Quiet())
	if st == PairDeclined {
		key, value, st = it.step(cx)
	}
	if st != PairYielded {
		return cx.CreateIterResultObject(storage.Undefined, true)
	}
	return cx.CreateIterResultObject(it.result(cx.Quiet(), key, value), false)
}

// arrayIteratorFastStep is the step of the next method of array iterators of the running realm.
func arrayIteratorFastStep(cx *ExecutionContext, this Value) (Value, bool) {
	it, _ := storage.IteratorState(this).(*arrayIterator)
	key, value, st := it.stepPlain(cx.Quiet())
	if st == PairDeclined {
		cx.pushNative(cx.Intrinsic(IntrinsicArrayIteratorPrototypeNext), this, nil)
		key, value, st = it.step(cx)
		cx.popNative()
	}
	if st != PairYielded {
		return storage.Undefined, false
	}
	return it.result(cx.Quiet(), key, value), true
}

// arrayIteratorIndex reads the element i of o for the next method of array iterators of the running
// realm.
func arrayIteratorIndex(cx *ExecutionContext, o *Object, i uint32) Value {
	// Only the receiver's type name is visible on a native frame. The
	// unmaterialized iterator inherits that name from its prototype.
	proto := cx.Intrinsic(IntrinsicArrayIteratorPrototype)
	cx.pushNative(cx.Intrinsic(IntrinsicArrayIteratorPrototypeNext), Value{O: proto}, nil)
	cx.Poll()
	v := cx.Index(Value{O: o}, i)
	cx.popNative()
	return v
}

func (cx *ExecutionContext) arrayIteratorPairStep(it, next Value) (Value, Value, PairStep) {
	state, _ := storage.IteratorState(it).(*arrayIterator)
	if state == nil || state.kind != IterateEntries {
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	key, value, st := state.stepPlain(cx.Quiet())
	if st == PairDeclined {
		cx.pushNative(next.O, it, nil)
		key, value, st = state.step(cx)
		cx.popNative()
	}
	return key, value, st
}

// result returns what the iterator yields for key and value.
func (it *arrayIterator) result(q QuietContext, key, value Value) Value {
	switch it.kind {
	case IterateKeys:
		return key
	case IterateValues:
		return value
	default:
		return Value{O: q.NewArrayPair(key, value)}
	}
}

// stepPlain is step when that neither runs code nor throws. It declines without advancing
// otherwise.
func (it *arrayIterator) stepPlain(q QuietContext) (key, value Value, st PairStep) {
	if it == nil {
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	r := q.realm
	o := it.object
	if o == nil {
		return storage.Undefined, storage.Undefined, PairDone
	}
	i := it.nextIndex
	var length int64
	switch {
	case o.Kind == kindArray:
		length = int64(o.Length)
	case o.Class == classTypedArray:
		t := o.ExtState().(*TypedArray)
		if it.kind != IterateKeys && i <= MaxArrayIndex {
			if b, ok := t.element(uint32(i)); ok {
				value = loadElement(t.typ, b)
				it.nextIndex = i + 1
				r.PollAtIndex(i)
				return storage.Number(float64(i)), value, PairYielded
			}
		}
		n, ok := t.Length()
		if !ok {
			return storage.Undefined, storage.Undefined, PairDeclined
		}
		length = int64(n)
	default:
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	if i >= length {
		it.object = nil
		return storage.Undefined, storage.Undefined, PairDone
	}
	key = storage.Number(float64(i))
	if it.kind == IterateKeys {
		it.nextIndex = i + 1
		return key, storage.Undefined, PairYielded
	}
	if i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		value = o.Elems[i]
		it.nextIndex = i + 1
		r.PollAtIndex(i)
		return key, value, PairYielded
	}
	return storage.Undefined, storage.Undefined, PairDeclined
}

// step advances the iterator.
func (it *arrayIterator) step(cx *ExecutionContext) (key, value Value, st PairStep) {
	if it == nil {
		cx.ThrowTypeError("next method called on an object that is not an array iterator")
	}
	o := it.object
	if o == nil {
		return storage.Undefined, storage.Undefined, PairDone
	}
	// Reading length can call next on this iterator again.
	i := it.nextIndex
	var length int64
	if o.Class == classTypedArray {
		n, ok := o.ExtState().(*TypedArray).Length()
		if !ok {
			cx.ThrowTypeError("cannot iterate a detached or out of bounds typed array")
		}
		length = int64(n)
	} else {
		length = cx.LengthOfArrayLike(o)
	}
	if i >= length {
		it.object = nil
		return storage.Undefined, storage.Undefined, PairDone
	}
	key = storage.Number(float64(i))
	it.nextIndex = i + 1
	if it.kind == IterateKeys {
		return key, storage.Undefined, PairYielded
	}
	if i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		value = o.Elems[i]
		cx.PollAt(i)
		return key, value, PairYielded
	}
	cx.Poll()
	return key, cx.Get(o, cx.ToPropertyKey(key)), PairYielded
}

// Outcomes of iterNext.
const (
	iterValue = iota
	iterDone
	iterFailed
)

// stepDeclined is the value of a step that declines. No iterator yields it.
var stepDeclined = storage.NewMarker(nil)

// iterNext calls the next method next of the iterator it, as for-of and destructuring do.
// It returns the value with iterValue, or iterDone when the iterator is done. When the result
// is not an object or reading it throws, it returns iterFailed with the exception pending:
// the iterator then counts as finished. An exception that next throws propagates.
//
// The next methods of the built-in iterators and of generators run without creating
// the iterator result, whose reading would have no observable effect.
func (cx *ExecutionContext) iterNext(it, next Value) (Value, int) {
	f := next.O
	if f != nil && f.Fn != nil && cx.hasIteratorFast(f.Fn) {
		if fast := nativeFastOf(f.Fn.Info.Native); fast.step != nil {
			if cx.InterruptFlag.Load() {
				cx.Poll()
			}
			v, ok := fast.step(cx, it)
			if ok {
				return v, iterValue
			}
			if v.O != stepDeclined {
				return storage.Undefined, iterDone
			}
		} else if fast.framedStep != nil {
			v, ok := cx.stepFramed(f, fast.framedStep, it)
			if !ok {
				return storage.Undefined, iterDone
			}
			return v, iterValue
		}
	}
	var r Value
	switch {
	case f != nil && f == cx.Intrinsic(IntrinsicGeneratorPrototypeNext) && isSyncGenerator(it):
		if cx.InterruptFlag.Load() {
			cx.Poll()
		}
		g := it.O.ExtState().(*generator)
		cx.pushNative(f, it, nil)
		v, done := cx.resume(g, ResumeNext, storage.Undefined)
		cx.popNative()
		if done {
			return storage.Undefined, iterDone
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
		done = storage.ToBoolean(cx.GetV(r, cx.Key(keyDone)))
		if !done {
			v = cx.GetV(r, cx.Key(KeyValue))
		}
	}) {
		return storage.Undefined, iterFailed
	}
	if done {
		return storage.Undefined, iterDone
	}
	return v, iterValue
}

// stepFramed steps the iterator it with step in the frame of its next method next.
func (cx *ExecutionContext) stepFramed(next *Object, step IteratorStepFunc, it Value) (Value, bool) {
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	cx.pushNative(next, it, nil)
	v, ok := step(cx, it)
	cx.popNative()
	return v, ok
}

// IteratorStepValue implements IteratorStepValue for the iterator it and its next method next:
// it returns the next value, or reports false when the iterator is done.
func (cx *ExecutionContext) IteratorStepValue(it, next Value) (Value, bool) {
	return cx.iteratorStep(it, next)
}

// IteratorStep implements IteratorStep for the iterator it and its next method next,
// without the result: it reports false when the iterator is done.
func (cx *ExecutionContext) IteratorStep(it, next Value) bool {
	if f := next.O; f != nil && f.Fn != nil && cx.hasIteratorFast(f.Fn) {
		if fast := nativeFastOf(f.Fn.Info.Native); fast.step != nil {
			if cx.InterruptFlag.Load() {
				cx.Poll()
			}
			if v, ok := fast.step(cx, it); ok || v.O != stepDeclined {
				return ok
			}
		} else if fast.framedStep != nil {
			_, ok := cx.stepFramed(f, fast.framedStep, it)
			return ok
		}
	}
	r := cx.CallValue(next, it)
	if !r.IsObject() {
		cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
	}
	return !storage.ToBoolean(cx.GetV(r, cx.Key(keyDone)))
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
			if m := cx.GetMethod(it, cx.Key(KeyReturn)); m != nil {
				cx.Call(m, it)
			}
		})
		cx.thrown, cx.thrownAt = saved, site
		return
	}
	m := cx.GetMethod(it, cx.Key(KeyReturn))
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
	if c := &cx.realm.ArrayIter; c.Epoch == cx.ProtoEpoch && c.NoReturn {
		if validateExec {
			cx.CheckNoIteratorReturn()
		}
		return
	}
	if cx.NoArrayIteratorReturn(cx.Intrinsic(IntrinsicArrayIteratorPrototype)) {
		return
	}
	it := cx.newArrayIterator(o, IterateValues, int64(nextIndex))
	cx.iteratorClose(Value{O: it}, pending)
}

// Iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (cx *ExecutionContext) Iterate(v Value, f func(Value) bool) {
	if v.O != nil && v.O.Kind == kindArray && cx.ArrayIterationPlain(v.O) {
		cx.iteratePlainArray(v.O, f)
		return
	}
	it, next := cx.getIterator(v, false)
	cx.IterateRecord(it, next, f)
}

// IterateFromMethod is [ExecutionContext.Iterate] for the iterator that GetIteratorFromMethod(v, method)
// returns.
func (cx *ExecutionContext) IterateFromMethod(v Value, method *Object, f func(Value) bool) {
	if method == cx.Intrinsic(IntrinsicArrayPrototypeValues) && v.O != nil && v.O.Kind == kindArray && cx.ArrayIterationPlain(v.O) {
		cx.iteratePlainArray(v.O, f)
		return
	}
	it := cx.Call(method, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	cx.IterateRecord(it, cx.GetV(it, cx.Key(KeyNext)), f)
}

// iteratePlainArray iterates o by index with the original array values and next methods.
func (cx *ExecutionContext) iteratePlainArray(o *Object, f func(Value) bool) {
	var nextIndex uint32
	cx.iterate(func() (Value, bool) {
		i := nextIndex
		if i >= o.Length {
			return storage.Undefined, false
		}
		if cx.InterruptFlag.Load() {
			cx.Poll()
		}
		nextIndex++
		if uint(i) < uint(len(o.Elems)) && !o.Elems[i].IsHole() {
			return o.Elems[i], true
		}
		return cx.plainArrayIteratorIndex(o, i), true
	}, func(pending bool) {
		cx.closePlainArrayIterator(o, nextIndex, pending)
	}, f)
}

// plainArrayIteratorIndex reads an element outside dense storage with the native next frame
// that a getter or proxy can observe.
func (cx *ExecutionContext) plainArrayIteratorIndex(o *Object, i uint32) Value {
	if o.Flags&objSparse == 0 && o.HolesAreAbsent() {
		return storage.Undefined
	}
	// ArrayIterationPlain proved this method was selected when iteration started.
	next := cx.Intrinsic(IntrinsicArrayIteratorPrototypeNext)
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	return nativeFastOf(next.Fn.Info.Native).arrayIndex(cx, o, i)
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
	if s := v.O; s != nil && s.Kind == kindArray && cx.ArrayIterationPlain(s) {
		if _, ok := s.PlainElems(); ok {
			return s
		}
	}
	o := cx.NewArray(nil)
	cx.appendIterable(o, v)
	return o
}

// appendIterable appends the values that iterating v yields to the array o.
func (cx *ExecutionContext) appendIterable(o *Object, v Value) {
	if s := v.O; s != nil && s.Kind == kindArray {
		o.ReserveElems(len(s.Elems))
	}
	cx.Iterate(v, func(e Value) bool {
		o.AppendElem(e)
		o.Length = uint32(len(o.Elems))
		return true
	})
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

	// visited is nil while obj is the receiver. The keys visited on the receiver are its keys
	// above and the key length if length is set, less those in missing, which the receiver no
	// longer had when they came up, in this order.
	//
	// Once obj is a prototype, the visited keys are those in visited and the indices below
	// receiverIndices less those in missing, which holds only such indices.
	visited         map[PropKey]struct{}
	length          bool
	receiverIndices uint32
	missing         []PropKey
}

// newForIn returns the iterator of a for-in loop over v.
func (cx *ExecutionContext) newForIn(v Value) Value {
	it := &forIn{}
	o := it.InitWithState(nil, classForIn, it)
	if !v.IsNullish() {
		it.obj = cx.ToObject(v)
		it.indices, it.length, it.keys = cx.forInOwnKeys(it.obj)
	}
	return Value{O: o}
}

// forInOwnKeys returns the own property keys of the receiver o of a for-in loop in the order of
// [ExecutionContext.OwnKeys], where symbols may be left out: the indices below indices, then the key length
// if length is set, then keys.
func (cx *ExecutionContext) forInOwnKeys(o *Object) (indices uint32, length bool, keys []PropKey) {
	if o.Class == classProxy || o.Class == classModuleNamespace {
		return 0, false, cx.OwnKeys(o)
	}
	if o.Flags&objExotic != 0 {
		cx.resolve(o)
	}
	// The own keys of any other object are its elements, the length of an array, and the
	// keys of its shape.
	if o.Flags&(objExotic|objSparse) != 0 || o.Class == classString || storage.ContainsHole(o.Elems) {
		return 0, false, cx.OwnKeys(o)
	}
	return uint32(len(o.Elems)), o.Kind == kindArray, o.Shape.StringKeys()
}

// forInNext returns the next key of the for-in iterator o.
func (cx *ExecutionContext) forInNext(o *Object) (Value, bool) {
	it := o.ExtState().(*forIn)
	for it.obj != nil {
		for it.index < it.indices {
			i := it.index
			it.index++
			if obj := it.obj; int64(i) < int64(len(obj.Elems)) && !obj.Elems[i].IsHole() {
				return cx.indexString(i), true
			}
			cx.PollAt(int64(i))
			k := storage.IndexKey(i)
			d, ok := cx.GetOwnProperty(it.obj, k)
			if !ok {
				it.missing = append(it.missing, k)
				continue
			}
			if d.Enumerable() {
				return cx.indexString(i), true
			}
		}
		for it.pos < len(it.keys) {
			k := it.keys[it.pos]
			it.pos++
			if k.IsSymbol() {
				continue
			}
			if it.visited != nil && it.hasVisited(k) {
				continue
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
			if !d.Enumerable() {
				continue
			}
			return cx.KeyValue(k), true
		}
		cx.forInNextObject(it)
	}
	return storage.Undefined, false
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
	for ; o != nil; o = o.Shape.Proto {
		if o.Class == classProxy || o.Class == classModuleNamespace {
			return true
		}
		if o.Flags&objExotic != 0 {
			cx.resolve(o)
			if o.Flags&objExotic != 0 {
				return true
			}
		}
		if o.Class == classString && o.ExtState().(*Object).StringLength() > 0 {
			return true
		}
		if slices.ContainsFunc(o.Elems, func(e Value) bool { return !e.IsHole() }) {
			return true
		}
		if o.Shape.HasEnumerableString() {
			return true
		}
	}
	return false
}

// forInVisitReceiver records the keys that the for-in iterator it visited on the receiver,
// which it is done with, in it.visited.
func (ag *agent) forInVisitReceiver(it *forIn) {
	it.receiverIndices = it.indices
	it.visited = make(map[PropKey]struct{}, len(it.keys)+1)
	if it.length {
		it.visited[storage.NamedKey(ag.Key(KeyLength))] = struct{}{}
	}
	for i, k := range it.keys {
		ag.PollAt(int64(i))
		if !k.IsSymbol() {
			it.visited[k] = struct{}{}
		}
	}
	// The missing indices below receiverIndices come first.
	n, _ := slices.BinarySearchFunc(it.missing, it.receiverIndices, func(k PropKey, end uint32) int {
		if k.IsIndex() && k.Idx < end {
			return -1
		}
		return 1
	})
	for i, k := range it.missing[n:] {
		ag.PollAt(int64(i))
		delete(it.visited, k)
	}
	it.missing = slices.Clip(it.missing[:n])
}

// hasVisited reports whether the for-in iterator it visited the key k. It must have moved on to a
// prototype.
func (it *forIn) hasVisited(k PropKey) bool {
	if k.IsIndex() && k.Idx < it.receiverIndices {
		_, missing := slices.BinarySearchFunc(it.missing, k.Idx, func(k PropKey, i uint32) int {
			return cmp.Compare(k.Idx, i)
		})
		if !missing {
			return true
		}
	}
	_, ok := it.visited[k]
	return ok
}

// templateObject returns the template object of a tagged template, creating it on first use.
func (cx *ExecutionContext) templateObject(t *templateSite) *Object {
	if t.Object != nil {
		return t.Object
	}
	raw := cx.NewTemplateArray(t.Raw)
	raw.MakePrivate()
	o := cx.NewTemplateArray(t.Cooked)
	cx.Add(o, cx.Key(KeyRaw), Value{O: raw}, 0)
	o.MakePrivate()
	t.Object = o
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
		return storage.Undefined, false
	}
	m := cx.GetMethod(it, cx.Key(KeyReturn))
	if m == nil {
		return storage.Undefined, false
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
	if storage.ToBoolean(cx.GetV(r, cx.Key(keyDone))) {
		return storage.Undefined, true
	}
	return cx.GetV(r, cx.Key(KeyValue)), false
}

// IteratorToList implements IteratorToList(GetIteratorFromMethod(v, method)).
func (cx *ExecutionContext) IteratorToList(v Value, method *Object) []Value {
	if method == cx.Intrinsic(IntrinsicArrayPrototypeValues) && v.O != nil && v.O.Kind == kindArray && cx.ArrayIterationPlain(v.O) {
		if n := v.O.Length; uint64(n) <= uint64(len(v.O.Elems)) && !storage.ContainsHole(v.O.Elems[:n]) {
			return storage.AppendPolled(&cx.Poller, nil, v.O.Elems[:n])
		}
		// The length of a sparse array says nothing about the memory it holds.
		vals := make([]Value, 0, len(v.O.Elems))
		cx.iteratePlainArray(v.O, func(e Value) bool {
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
	cx.IterateRecord(it, cx.GetV(it, cx.Key(KeyNext)), func(e Value) bool {
		vals = append(vals, e)
		return true
	})
	return vals
}
