// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// getIterator implements GetIterator and returns the iterator and its next method.
func (cx *ExecutionContext) getIterator(v storage.Value, async bool) (it, next storage.Value) {
	var m *storage.Object
	if async {
		m = cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolAsyncIterator))
		if m == nil {
			sync, syncNext := cx.getIterator(v, false)
			return cx.asyncFromSyncIterator(sync, syncNext)
		}
	} else {
		m = cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolIterator))
	}
	if m == nil {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not iterable")
	}
	return cx.iteratorRecord(cx.Call(m, v))
}

// GetIterator implements GetIterator for a sync iterator and returns the iterator and its next method.
func (cx *ExecutionContext) GetIterator(v storage.Value) (it, next storage.Value) {
	return cx.getIterator(v, false)
}

// GetIteratorFromMethod returns the iterator and next method obtained by calling method on v.
func (cx *ExecutionContext) GetIteratorFromMethod(v storage.Value, method *storage.Object) (it, next storage.Value) {
	return cx.iteratorRecord(cx.Call(method, v))
}

// GetIteratorFlattenable returns an iterator and its next method, accepting strings when iterateStrings is set.
func (cx *ExecutionContext) GetIteratorFlattenable(v storage.Value, iterateStrings bool) (it, next storage.Value) {
	if !v.IsObject() && (!iterateStrings || !v.IsString()) {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not an iterator or an iterable object")
	}
	it = v
	if m := cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolIterator)); m != nil {
		it = cx.Call(m, v)
	}
	return cx.iteratorRecord(it)
}

func (cx *ExecutionContext) iteratorRecord(it storage.Value) (storage.Value, storage.Value) {
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	return it, cx.GetV(it, cx.Key(storage.KeyNext))
}

// IteratorStepFunc is the next method of a built-in iterator, without the iterator result:
// it returns the next value of the iterator this, or reports false when the iterator is done.
// It throws a TypeError when this is not an iterator of its kind.
type IteratorStepFunc func(cx *ExecutionContext, this storage.Value) (storage.Value, bool)

// IteratorPairStepFunc advances an entries iterator that yields pairs in its creation realm.
// It must decline incompatible state without advancing, and must neither throw nor run JavaScript.
type IteratorPairStepFunc func(state any) (a, b storage.Value, st quiet.PairStep)

// IteratorYieldRealm identifies where a built-in iterator creates a yielded result.
type IteratorYieldRealm uint8

const (
	// IteratorMethodRealm is the realm of the next method.
	IteratorMethodRealm IteratorYieldRealm = iota

	// IteratorClosureRealm is the creation realm of an iterator produced by CreateIteratorFromClosure.
	IteratorClosureRealm
)

// NewArrayIterator returns an array iterator over the array-like object o.
func (cx *ExecutionContext) NewArrayIterator(o *storage.Object, kind quiet.IterationKind) *storage.Object {
	return cx.newArrayIterator(o, kind, 0)
}

func (cx *ExecutionContext) newArrayIterator(o *storage.Object, kind quiet.IterationKind, nextIndex int64) *storage.Object {
	return cx.NewIteratorObject(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototype), &quiet.ArrayIterator{
		Object:    o,
		NextIndex: nextIndex,
		Kind:      kind,
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
func (cx *ExecutionContext) NewArrayIteratorNext() *storage.Object {
	return cx.NewNative("next", 0, nativeArrayIteratorNext)
}

// arrayIteratorNext is the code of the next method of array iterators.
func arrayIteratorNext(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	it, _ := storage.IteratorState(this).(*quiet.ArrayIterator)
	key, value, st := it.StepPlain(cx.Quiet())
	if st == quiet.PairDeclined {
		key, value, st = arrayIteratorStep(it, cx)
	}
	if st != quiet.PairYielded {
		return cx.CreateIterResultObject(storage.Undefined, true)
	}
	return cx.CreateIterResultObject(it.Result(cx.Quiet(), key, value), false)
}

// arrayIteratorFastStep is the step of the next method of array iterators of the running realm.
func arrayIteratorFastStep(cx *ExecutionContext, this storage.Value) (storage.Value, bool) {
	it, _ := storage.IteratorState(this).(*quiet.ArrayIterator)
	key, value, st := it.StepPlain(cx.Quiet())
	if st == quiet.PairDeclined {
		cx.pushNative(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototypeNext), this, nil)
		key, value, st = arrayIteratorStep(it, cx)
		cx.popNative()
	}
	if st != quiet.PairYielded {
		return storage.Undefined, false
	}
	return it.Result(cx.Quiet(), key, value), true
}

// arrayIteratorIndex reads the element i of o for the next method of array iterators of the running
// realm.
func arrayIteratorIndex(cx *ExecutionContext, o *storage.Object, i uint32) storage.Value {
	// Only the receiver's type name is visible on a native frame. The
	// unmaterialized iterator inherits that name from its prototype.
	proto := cx.Intrinsic(storage.IntrinsicArrayIteratorPrototype)
	cx.pushNative(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototypeNext), storage.Value{O: proto}, nil)
	cx.Poll()
	v := cx.Index(storage.Value{O: o}, i)
	cx.popNative()
	return v
}

func (cx *ExecutionContext) arrayIteratorPairStep(it, next storage.Value) (storage.Value, storage.Value, quiet.PairStep) {
	state, _ := storage.IteratorState(it).(*quiet.ArrayIterator)
	if state == nil || state.Kind != quiet.IterateEntries {
		return storage.Undefined, storage.Undefined, quiet.PairDeclined
	}
	key, value, st := state.StepPlain(cx.Quiet())
	if st == quiet.PairDeclined {
		cx.pushNative(next.O, it, nil)
		key, value, st = arrayIteratorStep(state, cx)
		cx.popNative()
	}
	return key, value, st
}

// arrayIteratorStep advances the iterator.
func arrayIteratorStep(it *quiet.ArrayIterator, cx *ExecutionContext) (key, value storage.Value, st quiet.PairStep) {
	if it == nil {
		cx.ThrowTypeError("next method called on an object that is not an array iterator")
	}
	o := it.Object
	if o == nil {
		return storage.Undefined, storage.Undefined, quiet.PairDone
	}
	// Reading length can call next on this iterator again.
	i := it.NextIndex
	var length int64
	if o.Class == storage.ClassTypedArray {
		n, ok := o.ExtState().(*storage.TypedArray).Length()
		if !ok {
			cx.ThrowTypeError("cannot iterate a detached or out of bounds typed array")
		}
		length = int64(n)
	} else {
		length = cx.LengthOfArrayLike(o)
	}
	if i >= length {
		it.Object = nil
		return storage.Undefined, storage.Undefined, quiet.PairDone
	}
	key = storage.Number(float64(i))
	it.NextIndex = i + 1
	if it.Kind == quiet.IterateKeys {
		return key, storage.Undefined, quiet.PairYielded
	}
	if e, ok := o.ElementAt(uint32(i)); ok && i <= storage.MaxArrayIndex {
		value = e
		cx.PollAt(i)
		return key, value, quiet.PairYielded
	}
	cx.Poll()
	return key, cx.Get(o, cx.ToPropertyKey(key)), quiet.PairYielded
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
func (cx *ExecutionContext) iterNext(it, next storage.Value) (storage.Value, int) {
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
	var r storage.Value
	switch {
	case f != nil && f == cx.Intrinsic(storage.IntrinsicGeneratorPrototypeNext) && isSyncGenerator(it):
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
	var v storage.Value
	var done bool
	if !cx.protect(func() {
		if !r.IsObject() {
			cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
		}
		done = storage.ToBoolean(cx.GetV(r, cx.Key(storage.KeyDone)))
		if !done {
			v = cx.GetV(r, cx.Key(storage.KeyValue))
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
func (cx *ExecutionContext) stepFramed(next *storage.Object, step IteratorStepFunc, it storage.Value) (storage.Value, bool) {
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
func (cx *ExecutionContext) IteratorStepValue(it, next storage.Value) (storage.Value, bool) {
	return cx.iteratorStep(it, next)
}

// IteratorStep implements IteratorStep for the iterator it and its next method next,
// without the result: it reports false when the iterator is done.
func (cx *ExecutionContext) IteratorStep(it, next storage.Value) bool {
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
	return !storage.ToBoolean(cx.GetV(r, cx.Key(storage.KeyDone)))
}

// iteratorStep calls next and returns the value of the result, or false when the iterator is done.
func (cx *ExecutionContext) iteratorStep(it, next storage.Value) (storage.Value, bool) {
	v, st := cx.iterNext(it, next)
	if st == iterFailed {
		panic(errThrow)
	}
	return v, st == iterValue
}

// iteratorClose implements IteratorClose. With pending set, an exception is already
// being propagated and the errors of closing are dropped.
func (cx *ExecutionContext) iteratorClose(it storage.Value, pending bool) {
	if pending {
		saved, site := cx.thrown, cx.thrownAt
		cx.protect(func() {
			if m := cx.GetMethod(it, cx.Key(storage.KeyReturn)); m != nil {
				cx.Call(m, it)
			}
		})
		cx.thrown, cx.thrownAt = saved, site
		return
	}
	m := cx.GetMethod(it, cx.Key(storage.KeyReturn))
	if m == nil {
		return
	}
	if r := cx.Call(m, it); !r.IsObject() {
		cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
	}
}

// IteratorClose implements IteratorClose for a normal completion.
func (cx *ExecutionContext) IteratorClose(it storage.Value) {
	cx.iteratorClose(it, false)
}

// IteratorCloseAll implements IteratorCloseAll for a normal completion:
// it closes the iterators its in reverse order, skipping undefined entries.
func (cx *ExecutionContext) IteratorCloseAll(its []storage.Value) {
	cx.iteratorCloseAll(its, false)
}

// IfAbruptCloseIterator implements IfAbruptCloseIterator for what f does: when f throws,
// it closes the iterator it, ignoring any error of closing, and throws the exception again.
func (cx *ExecutionContext) IfAbruptCloseIterator(it storage.Value, f func()) {
	if cx.protect(f) {
		return
	}
	cx.iteratorClose(it, true)
	panic(errThrow)
}

// IfAbruptCloseIterators implements IfAbruptCloseIterators for what f does: when f throws,
// it closes the iterators its in reverse order, skipping undefined entries, and rethrows
// the original exception.
func (cx *ExecutionContext) IfAbruptCloseIterators(its []storage.Value, f func()) {
	if cx.protect(f) {
		return
	}
	cx.iteratorCloseAll(its, true)
}

func (cx *ExecutionContext) iteratorCloseAll(its []storage.Value, pending bool) {
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
func (cx *ExecutionContext) closePlainArrayIterator(o *storage.Object, nextIndex uint32, pending bool) {
	if c := &cx.realm.ArrayIter; c.Epoch == cx.ProtoEpoch && c.NoReturn {
		if validateExec {
			cx.CheckNoIteratorReturn()
		}
		return
	}
	if cx.NoArrayIteratorReturn(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototype)) {
		return
	}
	it := cx.newArrayIterator(o, quiet.IterateValues, int64(nextIndex))
	cx.iteratorClose(storage.Value{O: it}, pending)
}

// Iterate calls f for every value that iterating v yields. When f returns false,
// the iteration stops and the iterator is closed.
func (cx *ExecutionContext) Iterate(v storage.Value, f func(storage.Value) bool) {
	if v.O != nil && v.O.Kind == storage.KindArray && cx.ArrayIterationPlain(v.O) {
		cx.iteratePlainArray(v.O, f)
		return
	}
	it, next := cx.getIterator(v, false)
	cx.IterateRecord(it, next, f)
}

// IterateFromMethod is [ExecutionContext.Iterate] for the iterator that GetIteratorFromMethod(v, method)
// returns.
func (cx *ExecutionContext) IterateFromMethod(v storage.Value, method *storage.Object, f func(storage.Value) bool) {
	if method == cx.Intrinsic(storage.IntrinsicArrayPrototypeValues) && v.O != nil && v.O.Kind == storage.KindArray && cx.ArrayIterationPlain(v.O) {
		cx.iteratePlainArray(v.O, f)
		return
	}
	it := cx.Call(method, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	cx.IterateRecord(it, cx.GetV(it, cx.Key(storage.KeyNext)), f)
}

// iteratePlainArray iterates o by index with the original array values and next methods.
func (cx *ExecutionContext) iteratePlainArray(o *storage.Object, f func(storage.Value) bool) {
	var nextIndex uint32
	cx.iterate(func() (storage.Value, bool) {
		i := nextIndex
		if i >= o.Length() {
			return storage.Undefined, false
		}
		if cx.InterruptFlag.Load() {
			cx.Poll()
		}
		nextIndex++
		if e, ok := o.ElementAt(i); ok {
			return e, true
		}
		return cx.plainArrayIteratorIndex(o, i), true
	}, func(pending bool) {
		cx.closePlainArrayIterator(o, nextIndex, pending)
	}, f)
}

// plainArrayIteratorIndex reads an element outside dense storage with the native next frame
// that a getter or proxy can observe.
func (cx *ExecutionContext) plainArrayIteratorIndex(o *storage.Object, i uint32) storage.Value {
	if o.Flags()&storage.ObjSparse == 0 && o.HolesAreAbsent() {
		return storage.Undefined
	}
	// ArrayIterationPlain proved this method was selected when iteration started.
	next := cx.Intrinsic(storage.IntrinsicArrayIteratorPrototypeNext)
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	return nativeFastOf(next.Fn.Info.Native).arrayIndex(cx, o, i)
}

// IterateRecord is [ExecutionContext.Iterate] for the iterator record of the iterator it and its next method
// next.
func (cx *ExecutionContext) IterateRecord(it, next storage.Value, f func(storage.Value) bool) {
	cx.iterate(func() (storage.Value, bool) {
		return cx.iteratorStep(it, next)
	}, func(pending bool) {
		cx.iteratorClose(it, pending)
	}, f)
}

func (cx *ExecutionContext) iterate(step func() (storage.Value, bool), closeIterator func(pending bool), f func(storage.Value) bool) {
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
func (cx *ExecutionContext) spreadArgs(v storage.Value) *storage.Object {
	if s := v.O; s != nil && s.Kind == storage.KindArray && cx.ArrayIterationPlain(s) {
		if _, ok := s.PlainElems(); ok {
			return s
		}
	}
	o := cx.NewArray(nil)
	cx.appendIterable(o, v)
	return o
}

// appendIterable appends the values that iterating v yields to the array o.
func (cx *ExecutionContext) appendIterable(o *storage.Object, v storage.Value) {
	if s := v.O; s != nil && s.Kind == storage.KindArray {
		cx.ReserveElementsForIteration(o, s)
	}
	cx.Iterate(v, func(e storage.Value) bool {
		o.PushElement(e)
		return true
	})
}

// forIn is a for-in iterator object together with the state of its loop, so that creating
// one is a single allocation.
type forIn struct {
	propertyObject

	// obj is the object whose keys come next, or nil when the loop is done.
	obj *storage.Object

	// ahead is the object last found on the prototype chain whose keys the loop might visit: obj or
	// an object after it. The objects between them only hide keys. It is nil before the first search.
	ahead *storage.Object

	// The keys of obj that are left are the indices from index on that are below indices,
	// which are elements of the receiver, followed by keys[pos:].
	index, indices uint32
	keys           []storage.PropKey
	pos            int

	// visited is nil while obj is the receiver. The keys visited on the receiver are its keys
	// above and the key length if length is set, less those in missing, which the receiver no
	// longer had when they came up, in this order.
	//
	// Once obj is a prototype, the visited keys are those in visited and the indices below
	// receiverIndices less those in missing, which holds only such indices.
	visited         map[storage.PropKey]struct{}
	length          bool
	receiverIndices uint32
	missing         []storage.PropKey
}

// newForIn returns the iterator of a for-in loop over v.
func (cx *ExecutionContext) newForIn(v storage.Value) storage.Value {
	it := &forIn{}
	o := it.InitWithState(nil, storage.ClassForIn, it)
	if !v.IsNullish() {
		it.obj = cx.ToObject(v)
		it.indices, it.length, it.keys = cx.forInOwnKeys(it.obj)
	}
	return storage.Value{O: o}
}

// forInOwnKeys returns the own property keys of the receiver o of a for-in loop in the order of
// [ExecutionContext.OwnKeys], where symbols may be left out: the indices below indices, then the key length
// if length is set, then keys.
func (cx *ExecutionContext) forInOwnKeys(o *storage.Object) (indices uint32, length bool, keys []storage.PropKey) {
	if o.Class == storage.ClassProxy || o.Class == storage.ClassModuleNamespace {
		return 0, false, cx.OwnKeys(o)
	}
	if o.Flags()&storage.ObjExotic != 0 {
		cx.resolve(o)
	}
	// The own keys of any other object are its elements, the length of an array, and the
	// keys of its shape.
	if o.Flags()&(storage.ObjExotic|storage.ObjSparse) != 0 || o.Class == storage.ClassString || o.HasHole() {
		return 0, false, cx.OwnKeys(o)
	}
	return uint32(o.ElemCount()), o.Kind == storage.KindArray, o.Shape().StringKeys()
}

// forInNext returns the next key of the for-in iterator o.
func (cx *ExecutionContext) forInNext(o *storage.Object) (storage.Value, bool) {
	it := o.ExtState().(*forIn)
	for it.obj != nil {
		for it.index < it.indices {
			i := it.index
			it.index++
			if obj := it.obj; obj.HasElementAt(i) {
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
		// The post statement runs only after a key that does not come up.
		for ; it.pos < len(it.keys); cx.PollAt(int64(it.pos)) {
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
	if it.ahead == nil || it.ahead == it.obj {
		if it.ahead = cx.forInAhead(proto); it.ahead == nil {
			it.obj = nil
			return
		}
	} else {
		cx.PollStep()
	}
	if it.visited == nil {
		cx.forInVisitReceiver(it)
	}
	it.obj = proto
	it.index, it.indices = 0, 0
	it.keys = cx.OwnKeys(proto)
	it.pos = 0
}

// forInAhead returns the first of o and its prototypes whose keys a for-in loop might visit. When
// it returns nil, visiting the keys of those objects would have no observable effect.
func (cx *ExecutionContext) forInAhead(o *storage.Object) *storage.Object {
	for ; o != nil; o = o.Shape().Proto {
		cx.PollStep()
		if o.Class == storage.ClassProxy || o.Class == storage.ClassModuleNamespace {
			return o
		}
		if o.Flags()&storage.ObjExotic != 0 {
			cx.resolve(o)
			if o.Flags()&storage.ObjExotic != 0 {
				return o
			}
		}
		if o.Class == storage.ClassString && o.ExtState().(*storage.Object).StringLength() > 0 {
			return o
		}
		if o.HasElement() {
			return o
		}
		if o.Shape().HasEnumerableString() {
			return o
		}
	}
	return nil
}

// forInVisitReceiver records the keys that the for-in iterator it visited on the receiver,
// which it is done with, for [forIn.hasVisited].
func (ag *agent) forInVisitReceiver(it *forIn) {
	it.receiverIndices = it.indices
	it.visited = make(map[storage.PropKey]struct{}, len(it.keys)+1)
	if it.length {
		it.visited[storage.NamedKey(ag.Key(storage.KeyLength))] = struct{}{}
	}
	for i, k := range it.keys {
		ag.PollAt(int64(i))
		if !k.IsSymbol() {
			it.visited[k] = struct{}{}
		}
	}
	// The missing indices below receiverIndices come first.
	n, _ := slices.BinarySearchFunc(it.missing, it.receiverIndices, func(k storage.PropKey, end uint32) int {
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
func (it *forIn) hasVisited(k storage.PropKey) bool {
	if k.IsIndex() && k.Idx < it.receiverIndices {
		_, missing := slices.BinarySearchFunc(it.missing, k.Idx, func(k storage.PropKey, i uint32) int {
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
func (cx *ExecutionContext) templateObject(t *storage.TemplateSite) *storage.Object {
	if t.Object != nil {
		return t.Object
	}
	raw := cx.NewTemplateArray(t.Raw)
	raw.MakePrivate()
	o := cx.NewTemplateArray(t.Cooked)
	cx.Add(o, cx.Key(storage.KeyRaw), storage.Value{O: raw}, 0)
	o.MakePrivate()
	t.Object = o
	return o
}

// StringFromGo returns the Go string s, which is taken to be UTF-8, as a value.
func (cx *ExecutionContext) StringFromGo(s string) storage.Value {
	return cx.NewString(wtf8.FromValidUTF8(s))
}

// iterReturn calls the return method of the iterator it without arguments, as
// AsyncIteratorClose does, and returns the result. It reports false when it is undefined
// or has no return method.
func (cx *ExecutionContext) iterReturn(it storage.Value) (storage.Value, bool) {
	if it.IsUndefined() {
		return storage.Undefined, false
	}
	m := cx.GetMethod(it, cx.Key(storage.KeyReturn))
	if m == nil {
		return storage.Undefined, false
	}
	return cx.Call(m, it), true
}

func (cx *ExecutionContext) throwNotIterResult(r storage.Value) {
	cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
}

// iterResultValue returns the value of the iterator result r, or reports true when it is done.
func (cx *ExecutionContext) iterResultValue(r storage.Value) (storage.Value, bool) {
	if !r.IsObject() {
		cx.throwNotIterResult(r)
	}
	if storage.ToBoolean(cx.GetV(r, cx.Key(storage.KeyDone))) {
		return storage.Undefined, true
	}
	return cx.GetV(r, cx.Key(storage.KeyValue)), false
}

// IteratorToList implements IteratorToList(GetIteratorFromMethod(v, method)).
func (cx *ExecutionContext) IteratorToList(v storage.Value, method *storage.Object) []storage.Value {
	if method == cx.Intrinsic(storage.IntrinsicArrayPrototypeValues) && v.O != nil && v.O.Kind == storage.KindArray && cx.ArrayIterationPlain(v.O) {
		if elems, ok := cx.CopyDenseElements(v.O); ok {
			return elems
		}
		// The length of a sparse array says nothing about the memory it holds.
		vals := make([]storage.Value, 0, v.O.ElemCount())
		cx.iteratePlainArray(v.O, func(e storage.Value) bool {
			vals = append(vals, e)
			return true
		})
		return vals
	}
	it := cx.Call(method, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the Symbol.iterator method is not an object")
	}
	var vals []storage.Value
	cx.IterateRecord(it, cx.GetV(it, cx.Key(storage.KeyNext)), func(e storage.Value) bool {
		vals = append(vals, e)
		return true
	})
	return vals
}
