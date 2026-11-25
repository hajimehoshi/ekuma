// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// mapIterator is the state of a Map Iterator object.
type mapIterator struct {
	// table is nil once the iterator is done.
	table  *collection.Table
	cursor collection.Cursor
	kind   engine.IterationKind
}

// setIterator is the state of a Set Iterator object.
type setIterator struct {
	// table is nil once the iterator is done.
	table  *collection.Table
	cursor collection.Cursor
	kind   engine.IterationKind
}

func thisMap(cx *engine.ExecutionContext, this engine.Value, name string) *collection.Table {
	t, _ := storage.MapData(this).(*collection.Table)
	if t == nil {
		cx.ThrowTypeError("Map.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

// mapOf returns the [[MapData]] of this, or nil when this is not a Map object.
func mapOf(this engine.Value) *collection.Table {
	t, _ := storage.MapData(this).(*collection.Table)
	return t
}

// setOf returns the [[SetData]] of this, or nil when this is not a Set object.
func setOf(this engine.Value) *collection.Table {
	t, _ := storage.SetData(this).(*collection.Table)
	return t
}

func thisSet(cx *engine.ExecutionContext, this engine.Value, name string) *collection.Table {
	t, _ := storage.SetData(this).(*collection.Table)
	if t == nil {
		cx.ThrowTypeError("Set.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

// addEntriesFromIterable implements AddEntriesFromIterable. When fast is not nil, it is
// called instead of adder, which must then be the original set method of the collection.
func addEntriesFromIterable(cx *engine.ExecutionContext, target *engine.Object, iterable engine.Value, adder *engine.Object, fast func(k, v engine.Value)) {
	cx.Iterate(iterable, func(e engine.Value) bool {
		if !e.IsObject() {
			cx.ThrowTypeError("iterator value " + cx.DisplayString(e) + " is not an entry object")
		}
		k := cx.Index(e, 0)
		v := cx.Index(e, 1)
		if fast != nil {
			fast(k, v)
		} else {
			cx.Call(adder, storage.ObjectValue(target), k, v)
		}
		return true
	})
}

// iterationUnobservable reports whether iterating the Map or Set o calls the method iter as its
// @@iterator, which proto holds, and the method next, which iterProto holds.
func iterationUnobservable(q engine.QuietContext, o, proto, iter, iterProto, next *engine.Object) bool {
	return q.InheritsMethod(o, proto, q.WellKnownSymbol(engine.SymbolIterator), iter) && q.HasOwnMethod(iterProto, q.Key(engine.KeyNext), next)
}

// newMapCopy returns the Map that the constructor makes with proto from iterable when that calls
// the original methods of a Map only. It reports false otherwise.
func newMapCopy(q engine.QuietContext, proto *engine.Object, iterable engine.Value) (*engine.Object, bool) {
	src := mapOf(iterable)
	if src == nil || proto != q.Intrinsic(engine.IntrinsicMapPrototype) {
		return nil, false
	}
	if !q.HasOwnMethod(proto, q.Key(engine.KeySet), q.Intrinsic(engine.IntrinsicMapPrototypeSet)) {
		return nil, false
	}
	if !iterationUnobservable(q, iterable.Object(), proto, q.Intrinsic(engine.IntrinsicMapPrototypeEntries), q.Intrinsic(engine.IntrinsicMapIteratorPrototype), q.Intrinsic(engine.IntrinsicMapIteratorPrototypeNext)) {
		return nil, false
	}
	return q.NewMapObject(proto, src.Clone(q.Poll)), true
}

// newSetCopy returns the Set that the constructor makes with proto from iterable when that calls
// the original methods of a Set only. It reports false otherwise.
func newSetCopy(q engine.QuietContext, proto *engine.Object, iterable engine.Value) (*engine.Object, bool) {
	src := setOf(iterable)
	if src == nil || proto != q.Intrinsic(engine.IntrinsicSetPrototype) {
		return nil, false
	}
	if !q.HasOwnMethod(proto, q.Key(engine.KeyAdd), q.Intrinsic(engine.IntrinsicSetPrototypeAdd)) {
		return nil, false
	}
	if !iterationUnobservable(q, iterable.Object(), proto, q.Intrinsic(engine.IntrinsicSetPrototypeValues), q.Intrinsic(engine.IntrinsicSetIteratorPrototype), q.Intrinsic(engine.IntrinsicSetIteratorPrototypeNext)) {
		return nil, false
	}
	return q.NewSetObject(proto, src.Clone(q.Poll)), true
}

// reserveProbe is the number of elements of an array after which a collection made from the
// array makes room for up to maxReserve of them, if they all had different keys.
const reserveProbe = 16

// maxReserve is the number of elements of an array after which a collection made from the array
// makes room for all of them, if they all had different keys and the array has at least twice as
// many.
const maxReserve = 1 << 16

// reserveAfter makes room in t for the entries of an array of n elements after the first i
// elements were added, when those had different keys.
func reserveAfter(t *collection.Table, i, n int) {
	switch {
	case i == reserveProbe && t.Size() == reserveProbe && n >= 2*reserveProbe:
		// The keys of the rest may repeat, so a long array gets room for only part of it first.
		t.Reserve(min(n, maxReserve))
	case i == maxReserve && t.Size() == maxReserve && n >= 2*maxReserve:
		// Rebuilding the index for all of them pays off only for a much longer array.
		t.Reserve(n)
	}
}

// adderOf returns the method name of the new collection o, or throws a TypeError.
func adderOf(cx *engine.ExecutionContext, o *engine.Object, name *engine.Object) *engine.Object {
	adder := cx.Get(o, storage.NamedKey(name))
	if !adder.IsCallable() {
		cx.ThrowTypeError("the " + wtf8.ToValidUTF8(name.Str()) + " method of the new collection is not a function")
	}
	return adder.Object()
}

// speciesGetter defines the getter of Symbol.species that returns this on the constructor c.
func speciesGetter(cx *engine.ExecutionContext, c *engine.Object) {
	getter(cx, c, cx.WellKnownSymbol(engine.SymbolSpecies), "[Symbol.species]", nativeReturnThis)
}

// nativeReturnThis is the getters and methods that return their this value.
var nativeReturnThis = engine.RegisterNative(returnThis)

func returnThis(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return this
}

// groupBy implements GroupBy. It calls add with every value and the result of the callback for it.
func groupBy(cx *engine.ExecutionContext, items, cb engine.Value, add func(k, v engine.Value)) {
	cx.RequireObjectCoercible(items, "groupBy")
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	k := 0.0
	cx.Iterate(items, func(v engine.Value) bool {
		key := cx.Call(cb.Object(), storage.Undefined, v, storage.Number(k))
		add(key, v)
		k++
		return true
	})
}

// appendToGroup adds v at the end of the array g of a group, which no code has seen yet.
func appendToGroup(cx *engine.ExecutionContext, g *engine.Object, v engine.Value) {
	if _, ok := cx.Quiet().AppendElements(g, []engine.Value{v}); ok {
		return
	}
	cx.CreateDataPropertyOrThrow(g, bigKey(cx, cx.LengthOfArrayLike(g)), v)
}

func initMapAndSet(cx *engine.ExecutionContext) {
	initMap(cx)
	initSet(cx)
	initObjectGroupBy(cx)
}

// The kernels of the methods are the methods for a Map object.
var (
	nativeMapConstructor = engine.RegisterNative(mapConstructor)
	nativeMapGroupBy     = engine.RegisterNative(mapGroupBy)
	nativeMapClear       = engine.RegisterNative(mapClear)
	nativeMapDelete      = engine.RegisterNativeWithKernels(mapDelete, engine.Kernels{
		Method1:  mapDeleteKernel,
		Contract: shortContract,
	})
	nativeMapGet = engine.RegisterNativeWithKernels(mapGet, engine.Kernels{
		Method1:  mapGetKernel,
		Contract: shortContract,
	})
	nativeMapHas = engine.RegisterNativeWithKernels(mapHas, engine.Kernels{
		Method1:  mapHasKernel,
		Contract: shortContract,
	})
	nativeMapSet = engine.RegisterNativeWithKernels(mapSet, engine.Kernels{
		Method2:  mapSetKernel,
		Contract: shortContract,
	})
	nativeMapGetOrInsert         = engine.RegisterNative(mapGetOrInsert)
	nativeMapGetOrInsertComputed = engine.RegisterNative(mapGetOrInsertComputed)
	nativeMapForEach             = engine.RegisterNative(mapForEach)
	nativeMapSize                = engine.RegisterNativeWithKernels(mapSize, engine.Kernels{
		Method0:  mapSizeKernel,
		Contract: shortContract,
	})
	nativeMapKeys         = engine.RegisterNative(mapIterationNative("keys", engine.IterateKeys))
	nativeMapValues       = engine.RegisterNative(mapIterationNative("values", engine.IterateValues))
	nativeMapEntries      = engine.RegisterNative(mapIterationNative("entries", engine.IterateEntries))
	nativeMapIteratorNext = engine.RegisterPlainIteratorNext(engine.IteratorClosureRealm, "next method called on an object that is not a Map Iterator", mapIteratorStep, mapIteratorPairStep)
)

func initMap(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicMapPrototype)
	ctor := constructor(cx, "Map", 0, engine.IntrinsicMapPrototype, nativeMapConstructor)
	speciesGetter(cx, ctor)
	method(cx, ctor, "groupBy", 2, nativeMapGroupBy)

	method(cx, proto, "clear", 0, nativeMapClear)
	method(cx, proto, "delete", 1, nativeMapDelete)
	method(cx, proto, "get", 1, nativeMapGet)
	method(cx, proto, "has", 1, nativeMapHas)
	set := method(cx, proto, "set", 2, nativeMapSet)
	cx.SetIntrinsic(engine.IntrinsicMapPrototypeSet, set)
	method(cx, proto, "getOrInsert", 2, nativeMapGetOrInsert)
	method(cx, proto, "getOrInsertComputed", 2, nativeMapGetOrInsertComputed)
	method(cx, proto, "forEach", 1, nativeMapForEach)
	getter(cx, proto, cx.Key(engine.KeySize), "size", nativeMapSize)

	iterProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	cx.SetIntrinsic(engine.IntrinsicMapIteratorPrototype, iterProto)
	method(cx, proto, "keys", 0, nativeMapKeys)
	method(cx, proto, "values", 0, nativeMapValues)
	entries := method(cx, proto, "entries", 0, nativeMapEntries)
	cx.SetIntrinsic(engine.IntrinsicMapPrototypeEntries, entries)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolIterator), storage.ObjectValue(entries), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Map"), engine.FlagConfigurable)

	next := method(cx, iterProto, "next", 0, nativeMapIteratorNext)
	cx.SetIntrinsic(engine.IntrinsicMapIteratorPrototypeNext, next)
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Map Iterator"), engine.FlagConfigurable)
}

func mapConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Map requires 'new'")
	}
	targetProto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicMapPrototype)
	iterable := arg(args, 0)
	if iterable.IsObject() {
		if m, ok := newMapCopy(cx.Quiet(), targetProto, iterable); ok {
			return storage.ObjectValue(m)
		}
	}
	t := &collection.Table{}
	m := cx.NewMapObject(targetProto, t)
	if iterable.IsNullish() {
		return storage.ObjectValue(m)
	}
	adder := adderOf(cx, m, cx.Key(engine.KeySet))
	var fast func(k, v engine.Value)
	if adder == cx.Intrinsic(engine.IntrinsicMapPrototypeSet) {
		n := storage.ElementCount(iterable)
		var i int
		fast = func(k, v engine.Value) {
			t.Set(k, v)
			i++
			reserveAfter(t, i, n)
		}
	}
	addEntriesFromIterable(cx, m, iterable, adder, fast)
	if fast != nil {
		t.Trim()
	}
	return storage.ObjectValue(m)
}

func mapGroupBy(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := &collection.Table{}
	groupBy(cx, arg(args, 0), arg(args, 1), func(k, v engine.Value) {
		g, ok := t.Get(k)
		if !ok {
			g = storage.ObjectValue(cx.NewArray(nil))
			t.Append(k, g)
		}
		appendToGroup(cx, g.Object(), v)
	})
	return storage.ObjectValue(cx.NewMapObject(cx.Intrinsic(engine.IntrinsicMapPrototype), t))
}

func mapClear(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	thisMap(cx, this, "clear").Clear()
	return storage.Undefined
}

func mapDelete(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(thisMap(cx, this, "delete").Delete(arg(args, 0)))
}

func mapDeleteKernel(q engine.QuietContext, this, k engine.Value) (engine.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Bool(t.Delete(k)), true
	}
	return storage.Undefined, false
}

func mapGet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	v, _ := thisMap(cx, this, "get").Get(arg(args, 0))
	return v
}

func mapGetKernel(q engine.QuietContext, this, k engine.Value) (engine.Value, bool) {
	if t := mapOf(this); t != nil {
		v, _ := t.Get(k)
		return v, true
	}
	return storage.Undefined, false
}

func mapHas(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(thisMap(cx, this, "has").Has(arg(args, 0)))
}

func mapHasKernel(q engine.QuietContext, this, k engine.Value) (engine.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Bool(t.Has(k)), true
	}
	return storage.Undefined, false
}

func mapSet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	thisMap(cx, this, "set").Set(arg(args, 0), arg(args, 1))
	return this
}

func mapSetKernel(q engine.QuietContext, this, k, v engine.Value) (engine.Value, bool) {
	if t := mapOf(this); t != nil {
		t.Set(k, v)
		return this, true
	}
	return storage.Undefined, false
}

func mapGetOrInsert(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisMap(cx, this, "getOrInsert")
	k, v := arg(args, 0), arg(args, 1)
	if old, ok := t.Get(k); ok {
		return old
	}
	t.Append(k, v)
	return v
}

func mapGetOrInsertComputed(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisMap(cx, this, "getOrInsertComputed")
	k, cb := arg(args, 0), arg(args, 1)
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	if k.IsNumber() && k.Number() == 0 {
		k = storage.Number(0)
	}
	if old, ok := t.Get(k); ok {
		return old
	}
	v := cx.Call(cb.Object(), storage.Undefined, k)
	t.Set(k, v)
	return v
}

func mapForEach(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisMap(cx, this, "forEach")
	cb := arg(args, 0)
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	thisArg := arg(args, 1)
	// The callee does not keep its arguments, so one slice serves every call.
	argv := []engine.Value{storage.Undefined, storage.Undefined, this}
	c := t.Start()
	for {
		k, v, ok := t.Next(&c)
		if !ok {
			return storage.Undefined
		}
		argv[0], argv[1], argv[2] = v, k, this
		cx.Call(cb.Object(), thisArg, argv...)
	}
}

func mapSize(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(float64(thisMap(cx, this, "size").Size()))
}

func mapSizeKernel(q engine.QuietContext, this engine.Value) (engine.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Number(float64(t.Size())), true
	}
	return storage.Undefined, false
}

// mapIterationNative returns the method name of Map.prototype, which returns a Map Iterator of kind.
//
//go:noinline
func mapIterationNative(name string, kind engine.IterationKind) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(cx, this, name)
		return storage.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(engine.IntrinsicMapIteratorPrototype), &mapIterator{
			table:  t,
			cursor: t.Start(),
			kind:   kind,
		}))
	}
}

func mapIteratorStep(q engine.QuietContext, iterator *engine.Object, it *mapIterator) (engine.Value, bool) {
	if it.table == nil {
		return storage.Undefined, false
	}
	k, v, ok := it.table.Next(&it.cursor)
	if !ok {
		it.table = nil
		it.cursor = collection.Cursor{}
		return storage.Undefined, false
	}
	switch it.kind {
	case engine.IterateKeys:
		return k, true
	case engine.IterateValues:
		return v, true
	}
	return storage.ObjectValue(engine.CreationQuietContext(iterator).NewArrayPair(k, v)), true
}

func mapIteratorPairStep(state any) (engine.Value, engine.Value, engine.PairStep) {
	it, _ := state.(*mapIterator)
	if it == nil || it.kind != engine.IterateEntries {
		return storage.Undefined, storage.Undefined, engine.PairDeclined
	}
	if it.table == nil {
		return storage.Undefined, storage.Undefined, engine.PairDone
	}
	k, v, ok := it.table.Next(&it.cursor)
	if !ok {
		it.table = nil
		it.cursor = collection.Cursor{}
		return storage.Undefined, storage.Undefined, engine.PairDone
	}
	return k, v, engine.PairYielded
}

// setRecord is a Set Record.
type setRecord struct {
	obj  *engine.Object
	size float64
	has  *engine.Object
	keys *engine.Object

	// table is the [[SetData]] of obj when obj is a Set and has is %Set.prototype.has%,
	// so that a lookup in table gives the result of calling has.
	table *collection.Table

	// pace counts the lookups in table, which run no JavaScript, between checks for an
	// interruption.
	pace engine.Pacer
}

// getSetRecord implements GetSetRecord.
func getSetRecord(cx *engine.ExecutionContext, v engine.Value) setRecord {
	if !v.IsObject() {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not an object")
	}
	o := v.Object()
	n := cx.ToNumber(cx.Get(o, storage.NamedKey(cx.Key(engine.KeySize))))
	if n != n {
		cx.ThrowTypeError("the size of a set-like object must be a number")
	}
	size := cx.ToIntegerOrInfinity(storage.Number(n))
	if size < 0 {
		cx.ThrowRangeError("the size of a set-like object must not be negative")
	}
	has := cx.Get(o, storage.NamedKey(cx.Key(engine.KeyHas)))
	if !has.IsCallable() {
		cx.ThrowTypeError("the has method of a set-like object must be a function")
	}
	keys := cx.Get(o, storage.NamedKey(cx.Key(engine.KeyKeys)))
	if !keys.IsCallable() {
		cx.ThrowTypeError("the keys method of a set-like object must be a function")
	}
	r := setRecord{
		obj:  o,
		size: size,
		has:  has.Object(),
		keys: keys.Object(),
	}
	if r.has == cx.Intrinsic(engine.IntrinsicSetPrototypeHas) {
		r.table, _ = storage.SetData(v).(*collection.Table)
	}
	return r
}

// otherHas returns ToBoolean(Call(r.[[Has]], r.[[SetObject]], « e »)).
func (r *setRecord) otherHas(cx *engine.ExecutionContext, e engine.Value) bool {
	if r.table != nil {
		r.pace.Step(cx)
		return r.table.Has(e)
	}
	return storage.ToBoolean(cx.Call(r.has, storage.ObjectValue(r.obj), e))
}

// keysIterator implements GetKeysIterator(r) and returns the iterator and its next method.
func (r *setRecord) keysIterator(cx *engine.ExecutionContext) (it, next engine.Value) {
	it = cx.Call(r.keys, storage.ObjectValue(r.obj))
	if !it.IsObject() {
		cx.ThrowTypeError("the keys method of a set-like object must return an object")
	}
	next = cx.GetV(it, cx.Key(engine.KeyNext))
	if !next.IsCallable() {
		cx.ThrowTypeError("the iterator of a set-like object must have a next method")
	}
	return it, next
}

// eachKey iterates the iterator that GetKeysIterator returns for r.
// When f returns false, the iteration stops and the iterator is closed.
func (r *setRecord) eachKey(cx *engine.ExecutionContext, f func(engine.Value) bool) {
	it, next := r.keysIterator(cx)
	cx.IterateRecord(it, next, f)
}

func newSet(cx *engine.ExecutionContext, t *collection.Table) engine.Value {
	return storage.ObjectValue(cx.NewSetObject(cx.Intrinsic(engine.IntrinsicSetPrototype), t))
}

// The kernels of the methods are the methods for a Set object.
var (
	nativeSetConstructor = engine.RegisterNative(setConstructor)
	nativeSetAdd         = engine.RegisterNativeWithKernels(setAdd, engine.Kernels{
		Method1:  setAddKernel,
		Contract: shortContract,
	})
	nativeSetClear  = engine.RegisterNative(setClear)
	nativeSetDelete = engine.RegisterNativeWithKernels(setDelete, engine.Kernels{
		Method1:  setDeleteKernel,
		Contract: shortContract,
	})
	nativeSetHas = engine.RegisterNativeWithKernels(setHas, engine.Kernels{
		Method1:  setHasKernel,
		Contract: shortContract,
	})
	nativeSetForEach = engine.RegisterNative(setForEach)
	nativeSetSize    = engine.RegisterNativeWithKernels(setSize, engine.Kernels{
		Method0:  setSizeKernel,
		Contract: shortContract,
	})
	nativeSetUnion               = engine.RegisterNative(setUnion)
	nativeSetIntersection        = engine.RegisterNative(setIntersection)
	nativeSetDifference          = engine.RegisterNative(setDifference)
	nativeSetSymmetricDifference = engine.RegisterNative(setSymmetricDifference)
	nativeSetIsSubsetOf          = engine.RegisterNative(setIsSubsetOf)
	nativeSetIsSupersetOf        = engine.RegisterNative(setIsSupersetOf)
	nativeSetIsDisjointFrom      = engine.RegisterNative(setIsDisjointFrom)
	nativeSetEntries             = engine.RegisterNative(setIterationNative("entries", engine.IterateEntries))
	nativeSetValues              = engine.RegisterNative(setIterationNative("values", engine.IterateValues))
	nativeSetIteratorNext        = engine.RegisterPlainIteratorNext(engine.IteratorClosureRealm, "next method called on an object that is not a Set Iterator", setIteratorStep, setIteratorPairStep)
)

func initSet(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicSetPrototype)
	ctor := constructor(cx, "Set", 0, engine.IntrinsicSetPrototype, nativeSetConstructor)
	speciesGetter(cx, ctor)

	add := method(cx, proto, "add", 1, nativeSetAdd)
	cx.SetIntrinsic(engine.IntrinsicSetPrototypeAdd, add)
	method(cx, proto, "clear", 0, nativeSetClear)
	method(cx, proto, "delete", 1, nativeSetDelete)
	has := method(cx, proto, "has", 1, nativeSetHas)
	cx.SetIntrinsic(engine.IntrinsicSetPrototypeHas, has)
	method(cx, proto, "forEach", 1, nativeSetForEach)
	getter(cx, proto, cx.Key(engine.KeySize), "size", nativeSetSize)

	method(cx, proto, "union", 1, nativeSetUnion)
	method(cx, proto, "intersection", 1, nativeSetIntersection)
	method(cx, proto, "difference", 1, nativeSetDifference)
	method(cx, proto, "symmetricDifference", 1, nativeSetSymmetricDifference)
	method(cx, proto, "isSubsetOf", 1, nativeSetIsSubsetOf)
	method(cx, proto, "isSupersetOf", 1, nativeSetIsSupersetOf)
	method(cx, proto, "isDisjointFrom", 1, nativeSetIsDisjointFrom)

	iterProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	cx.SetIntrinsic(engine.IntrinsicSetIteratorPrototype, iterProto)
	method(cx, proto, "entries", 0, nativeSetEntries)
	values := method(cx, proto, "values", 0, nativeSetValues)
	cx.SetIntrinsic(engine.IntrinsicSetPrototypeValues, values)
	cx.Define(proto, cx.Key(engine.KeyKeys), storage.ObjectValue(values), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolIterator), storage.ObjectValue(values), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Set"), engine.FlagConfigurable)

	next := method(cx, iterProto, "next", 0, nativeSetIteratorNext)
	cx.SetIntrinsic(engine.IntrinsicSetIteratorPrototypeNext, next)
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Set Iterator"), engine.FlagConfigurable)
}

func setConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor Set requires 'new'")
	}
	targetProto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicSetPrototype)
	iterable := arg(args, 0)
	if iterable.IsObject() {
		if s, ok := newSetCopy(cx.Quiet(), targetProto, iterable); ok {
			return storage.ObjectValue(s)
		}
	}
	t := &collection.Table{}
	s := cx.NewSetObject(targetProto, t)
	if iterable.IsNullish() {
		return storage.ObjectValue(s)
	}
	adder := adderOf(cx, s, cx.Key(engine.KeyAdd))
	if adder == cx.Intrinsic(engine.IntrinsicSetPrototypeAdd) {
		n := storage.ElementCount(iterable)
		var i int
		cx.Iterate(iterable, func(e engine.Value) bool {
			t.Set(e, e)
			i++
			reserveAfter(t, i, n)
			return true
		})
		t.Trim()
		return storage.ObjectValue(s)
	}
	cx.Iterate(iterable, func(e engine.Value) bool {
		cx.Call(adder, storage.ObjectValue(s), e)
		return true
	})
	return storage.ObjectValue(s)
}

func setAdd(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "add")
	v := arg(args, 0)
	if !t.Has(v) {
		t.Append(v, v)
	}
	return this
}

func setAddKernel(q engine.QuietContext, this, v engine.Value) (engine.Value, bool) {
	t := setOf(this)
	if t == nil {
		return storage.Undefined, false
	}
	if !t.Has(v) {
		t.Append(v, v)
	}
	return this, true
}

func setClear(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	thisSet(cx, this, "clear").Clear()
	return storage.Undefined
}

func setDelete(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(thisSet(cx, this, "delete").Delete(arg(args, 0)))
}

func setDeleteKernel(q engine.QuietContext, this, v engine.Value) (engine.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Bool(t.Delete(v)), true
	}
	return storage.Undefined, false
}

func setHas(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(thisSet(cx, this, "has").Has(arg(args, 0)))
}

func setHasKernel(q engine.QuietContext, this, v engine.Value) (engine.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Bool(t.Has(v)), true
	}
	return storage.Undefined, false
}

func setForEach(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "forEach")
	cb := arg(args, 0)
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	thisArg := arg(args, 1)
	argv := []engine.Value{storage.Undefined, storage.Undefined, this}
	c := t.Start()
	for {
		k, _, ok := t.Next(&c)
		if !ok {
			return storage.Undefined
		}
		argv[0], argv[1], argv[2] = k, k, this
		cx.Call(cb.Object(), thisArg, argv...)
	}
}

func setSize(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(float64(thisSet(cx, this, "size").Size()))
}

func setSizeKernel(q engine.QuietContext, this engine.Value) (engine.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Number(float64(t.Size())), true
	}
	return storage.Undefined, false
}

func setUnion(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "union")
	r := getSetRecord(cx, arg(args, 0))
	it, next := r.keysIterator(cx)
	result := t.Clone(cx.Poll)
	cx.IterateRecord(it, next, func(e engine.Value) bool {
		if !result.Has(e) {
			result.Append(e, e)
		}
		return true
	})
	return newSet(cx, result)
}

func setIntersection(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "intersection")
	r := getSetRecord(cx, arg(args, 0))
	result := &collection.Table{}
	if float64(t.Size()) <= r.size {
		c := t.Start()
		for {
			e, _, ok := t.Next(&c)
			if !ok {
				break
			}
			if r.otherHas(cx, e) && !result.Has(e) {
				result.Append(e, e)
			}
		}
	} else {
		r.eachKey(cx, func(e engine.Value) bool {
			if t.Has(e) && !result.Has(e) {
				result.Append(e, e)
			}
			return true
		})
	}
	return newSet(cx, result)
}

func setDifference(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "difference")
	r := getSetRecord(cx, arg(args, 0))
	result := t.Clone(cx.Poll)
	if float64(t.Size()) <= r.size {
		// The result is not visible to JavaScript yet, so its keys can be taken in advance.
		keys := make([]engine.Value, 0, result.Size())
		c := result.Start()
		for {
			e, _, ok := result.Next(&c)
			if !ok {
				break
			}
			keys = append(keys, e)
		}
		for _, e := range keys {
			if r.otherHas(cx, e) {
				result.Delete(e)
			}
		}
	} else {
		r.eachKey(cx, func(e engine.Value) bool {
			result.Delete(e)
			return true
		})
	}
	return newSet(cx, result)
}

func setSymmetricDifference(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "symmetricDifference")
	r := getSetRecord(cx, arg(args, 0))
	it, next := r.keysIterator(cx)
	result := t.Clone(cx.Poll)
	cx.IterateRecord(it, next, func(e engine.Value) bool {
		inResult := result.Has(e)
		if t.Has(e) {
			if inResult {
				result.Delete(e)
			}
		} else if !inResult {
			result.Append(e, e)
		}
		return true
	})
	return newSet(cx, result)
}

func setIsSubsetOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "isSubsetOf")
	r := getSetRecord(cx, arg(args, 0))
	if float64(t.Size()) > r.size {
		return storage.False
	}
	c := t.Start()
	for {
		e, _, ok := t.Next(&c)
		if !ok {
			return storage.True
		}
		if !r.otherHas(cx, e) {
			return storage.False
		}
	}
}

func setIsSupersetOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "isSupersetOf")
	r := getSetRecord(cx, arg(args, 0))
	if float64(t.Size()) < r.size {
		return storage.False
	}
	result := true
	r.eachKey(cx, func(e engine.Value) bool {
		result = t.Has(e)
		return result
	})
	return storage.Bool(result)
}

func setIsDisjointFrom(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisSet(cx, this, "isDisjointFrom")
	r := getSetRecord(cx, arg(args, 0))
	if float64(t.Size()) <= r.size {
		c := t.Start()
		for {
			e, _, ok := t.Next(&c)
			if !ok {
				return storage.True
			}
			if r.otherHas(cx, e) {
				return storage.False
			}
		}
	}
	result := true
	r.eachKey(cx, func(e engine.Value) bool {
		result = !t.Has(e)
		return result
	})
	return storage.Bool(result)
}

// setIterationNative returns the method name of Set.prototype, which returns a Set Iterator of kind.
//
//go:noinline
func setIterationNative(name string, kind engine.IterationKind) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, name)
		return storage.ObjectValue(cx.NewIteratorObject(cx.Intrinsic(engine.IntrinsicSetIteratorPrototype), &setIterator{
			table:  t,
			cursor: t.Start(),
			kind:   kind,
		}))
	}
}

func setIteratorStep(q engine.QuietContext, iterator *engine.Object, it *setIterator) (engine.Value, bool) {
	if it.table == nil {
		return storage.Undefined, false
	}
	k, _, ok := it.table.Next(&it.cursor)
	if !ok {
		it.table = nil
		it.cursor = collection.Cursor{}
		return storage.Undefined, false
	}
	if it.kind == engine.IterateEntries {
		return storage.ObjectValue(engine.CreationQuietContext(iterator).NewArrayPair(k, k)), true
	}
	return k, true
}

func setIteratorPairStep(state any) (engine.Value, engine.Value, engine.PairStep) {
	it, _ := state.(*setIterator)
	if it == nil || it.kind != engine.IterateEntries {
		return storage.Undefined, storage.Undefined, engine.PairDeclined
	}
	if it.table == nil {
		return storage.Undefined, storage.Undefined, engine.PairDone
	}
	k, _, ok := it.table.Next(&it.cursor)
	if !ok {
		it.table = nil
		it.cursor = collection.Cursor{}
		return storage.Undefined, storage.Undefined, engine.PairDone
	}
	return k, k, engine.PairYielded
}

// nativeObjectGroupBy is Object.groupBy.
var nativeObjectGroupBy = engine.RegisterNative(objectGroupBy)

func initObjectGroupBy(cx *engine.ExecutionContext) {
	method(cx, cx.Intrinsic(engine.IntrinsicObject), "groupBy", 2, nativeObjectGroupBy)
}

func objectGroupBy(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	var keys []engine.PropKey
	groups := map[engine.PropKey]*engine.Object{}
	groupBy(cx, arg(args, 0), arg(args, 1), func(k, v engine.Value) {
		pk := cx.ToPropertyKey(k)
		g, ok := groups[pk]
		if !ok {
			g = cx.NewArray(nil)
			groups[pk] = g
			keys = append(keys, pk)
		}
		appendToGroup(cx, g, v)
	})
	o := cx.NewObject(nil)
	for _, k := range keys {
		cx.CreateDataPropertyOrThrow(o, k, storage.ObjectValue(groups[k]))
	}
	return storage.ObjectValue(o)
}
