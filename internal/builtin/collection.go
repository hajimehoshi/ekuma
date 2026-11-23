// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
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
	t, _ := engine.MapData(this).(*collection.Table)
	if t == nil {
		cx.ThrowTypeError("Map.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

// mapOf returns the [[MapData]] of this, or nil when this is not a Map object.
func mapOf(this engine.Value) *collection.Table {
	t, _ := engine.MapData(this).(*collection.Table)
	return t
}

// setOf returns the [[SetData]] of this, or nil when this is not a Set object.
func setOf(this engine.Value) *collection.Table {
	t, _ := engine.SetData(this).(*collection.Table)
	return t
}

func thisSet(cx *engine.ExecutionContext, this engine.Value, name string) *collection.Table {
	t, _ := engine.SetData(this).(*collection.Table)
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
			cx.Call(adder, engine.ObjectValue(target), k, v)
		}
		return true
	})
}

// iterationUnobservable reports whether iterating the Map or Set o calls the method iter as its
// @@iterator, which proto holds, and the method next, which iterProto holds, finding both
// without running anything that JavaScript could observe.
func iterationUnobservable(cx *engine.ExecutionContext, o, proto, iter, iterProto, next *engine.Object, nextKey engine.PropKey) bool {
	key := engine.NamedKey(cx.WellKnownSymbol(engine.SymbolIterator))
	if _, ok := cx.GetOwnProperty(o, key); ok || cx.ProtoOf(o) != proto {
		return false
	}
	return hasOwnMethod(cx, proto, key, iter) && hasOwnMethod(cx, iterProto, nextKey, next)
}

func hasOwnMethod(cx *engine.ExecutionContext, proto *engine.Object, key engine.PropKey, method *engine.Object) bool {
	d, ok := cx.GetOwnProperty(proto, key)
	return ok && d.IsData() && d.Value().Object() == method
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
	adder := cx.Get(o, engine.NamedKey(name))
	if !adder.IsCallable() {
		cx.ThrowTypeError("the " + wtf8.ToValidUTF8(name.Str()) + " method of the new collection is not a function")
	}
	return adder.Object()
}

// speciesGetter defines the getter of Symbol.species that returns this on the constructor c.
func speciesGetter(cx *engine.ExecutionContext, c *engine.Object) {
	getter(cx, c, cx.WellKnownSymbol(engine.SymbolSpecies), "[Symbol.species]", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
}

// groupBy implements GroupBy. It calls add with every value and the result of the callback for it.
func groupBy(cx *engine.ExecutionContext, items, cb engine.Value, add func(k, v engine.Value)) {
	cx.RequireObjectCoercible(items, "groupBy")
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	k := 0.0
	cx.Iterate(items, func(v engine.Value) bool {
		key := cx.Call(cb.Object(), engine.Undefined, v, engine.Number(k))
		add(key, v)
		k++
		return true
	})
}

// appendToGroup adds v at the end of the array g of a group, which no code has seen yet.
func appendToGroup(cx *engine.ExecutionContext, g *engine.Object, v engine.Value) {
	if _, ok := cx.AppendElements(g, []engine.Value{v}); ok {
		return
	}
	cx.CreateDataPropertyOrThrow(g, bigKey(cx, cx.LengthOfArrayLike(g)), v)
}

func initMapAndSet(cx *engine.ExecutionContext) {
	initMap(cx)
	initSet(cx)
	initObjectGroupBy(cx)
}

func initMap(cx *engine.ExecutionContext) {
	proto := cx.Realm().Intrinsic(engine.IntrinsicMapPrototype)
	setKey := cx.Intern("set")
	nextKey := engine.NamedKey(cx.Intern("next"))
	// The original methods that iterating a Map calls.
	var mapSet, mapEntries, mapIterProto, mapIterNext *engine.Object
	ctor := constructor(cx, "Map", 0, engine.IntrinsicMapPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor Map requires 'new'")
		}
		t := &collection.Table{}
		targetProto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicMapPrototype)
		m := cx.NewMapObject(targetProto, t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(m)
		}
		// A getter for set can modify or retain m. Bulk copying requires a fresh, unobserved table.
		if src := mapOf(iterable); src != nil && targetProto == proto &&
			hasOwnMethod(cx, proto, engine.NamedKey(setKey), mapSet) &&
			iterationUnobservable(cx, iterable.Object(), proto, mapEntries, mapIterProto, mapIterNext, nextKey) {
			t.CopyFrom(src, cx.Poll)
			return engine.ObjectValue(m)
		}
		adder := adderOf(cx, m, setKey)
		var fast func(k, v engine.Value)
		if adder == mapSet {
			n := engine.ElementCount(iterable)
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
		return engine.ObjectValue(m)
	})
	speciesGetter(cx, ctor)
	method(cx, ctor, "groupBy", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := &collection.Table{}
		groupBy(cx, arg(args, 0), arg(args, 1), func(k, v engine.Value) {
			g, ok := t.Get(k)
			if !ok {
				g = engine.ObjectValue(cx.NewArray(nil))
				t.Append(k, g)
			}
			appendToGroup(cx, g.Object(), v)
		})
		return engine.ObjectValue(cx.NewMapObject(cx.Realm().Intrinsic(engine.IntrinsicMapPrototype), t))
	})

	method(cx, proto, "clear", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		thisMap(cx, this, "clear").Clear()
		return engine.Undefined
	})
	// The kernels of the methods are the methods for a Map object.
	mapDelete := method(cx, proto, "delete", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(cx, this, "delete").Delete(arg(args, 0)))
	})
	cx.SetMethodKernels(mapDelete, nil, func(cx *engine.ExecutionContext, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Bool(t.Delete(k)), true
		}
		return engine.Undefined, false
	}, nil)
	mapGet := method(cx, proto, "get", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v, _ := thisMap(cx, this, "get").Get(arg(args, 0))
		return v
	})
	cx.SetMethodKernels(mapGet, nil, func(cx *engine.ExecutionContext, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			v, _ := t.Get(k)
			return v, true
		}
		return engine.Undefined, false
	}, nil)
	mapHas := method(cx, proto, "has", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(cx, this, "has").Has(arg(args, 0)))
	})
	cx.SetMethodKernels(mapHas, nil, func(cx *engine.ExecutionContext, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Bool(t.Has(k)), true
		}
		return engine.Undefined, false
	}, nil)
	mapSet = method(cx, proto, "set", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		thisMap(cx, this, "set").Set(arg(args, 0), arg(args, 1))
		return this
	})
	cx.SetMethodKernels(mapSet, nil, nil, func(cx *engine.ExecutionContext, this, k, v engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			t.Set(k, v)
			return this, true
		}
		return engine.Undefined, false
	})
	method(cx, proto, "getOrInsert", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(cx, this, "getOrInsert")
		k, v := arg(args, 0), arg(args, 1)
		if old, ok := t.Get(k); ok {
			return old
		}
		t.Append(k, v)
		return v
	})
	method(cx, proto, "getOrInsertComputed", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(cx, this, "getOrInsertComputed")
		k, cb := arg(args, 0), arg(args, 1)
		if !cb.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
		}
		if k.IsNumber() && k.Number() == 0 {
			k = engine.Number(0)
		}
		if old, ok := t.Get(k); ok {
			return old
		}
		v := cx.Call(cb.Object(), engine.Undefined, k)
		t.Set(k, v)
		return v
	})
	method(cx, proto, "forEach", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(cx, this, "forEach")
		cb := arg(args, 0)
		if !cb.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
		}
		thisArg := arg(args, 1)
		// The callee does not keep its arguments, so one slice serves every call.
		argv := []engine.Value{engine.Undefined, engine.Undefined, this}
		c := t.Start()
		for {
			k, v, ok := t.Next(&c)
			if !ok {
				return engine.Undefined
			}
			argv[0], argv[1], argv[2] = v, k, this
			cx.Call(cb.Object(), thisArg, argv...)
		}
	})
	mapSize := getter(cx, proto, cx.Intern("size"), "size", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisMap(cx, this, "size").Size()))
	})
	cx.SetMethodKernels(mapSize, func(cx *engine.ExecutionContext, this engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Number(float64(t.Size())), true
		}
		return engine.Undefined, false
	}, nil, nil)

	iterProto := cx.NewObject(cx.Realm().Intrinsic(engine.IntrinsicIteratorPrototype))
	mapIterProto = iterProto
	iterate := func(name string, kind engine.IterationKind) *engine.Object {
		return method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			t := thisMap(cx, this, name)
			return engine.ObjectValue(cx.NewIteratorObject(iterProto, &mapIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("keys", engine.IterateKeys)
	iterate("values", engine.IterateValues)
	mapEntries = iterate("entries", engine.IterateEntries)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(mapEntries), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Map"), engine.FlagConfigurable)

	mapIterNext = plainIteratorNext(cx, iterProto, engine.IteratorClosureRealm, "next method called on an object that is not a Map Iterator", func(cx *engine.ExecutionContext, iterator *engine.Object, it *mapIterator) (engine.Value, bool) {
		if it.table == nil {
			return engine.Undefined, false
		}
		k, v, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, false
		}
		switch it.kind {
		case engine.IterateKeys:
			return k, true
		case engine.IterateValues:
			return v, true
		}
		return engine.ObjectValue(iterator.CreationRealm().Context().NewArrayPair(k, v)), true
	})
	cx.SetIteratorPairStep(mapIterNext, func(state any) (engine.Value, engine.Value, engine.PairStep) {
		it, _ := state.(*mapIterator)
		if it == nil || it.kind != engine.IterateEntries {
			return engine.Undefined, engine.Undefined, engine.PairDeclined
		}
		if it.table == nil {
			return engine.Undefined, engine.Undefined, engine.PairDone
		}
		k, v, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, engine.Undefined, engine.PairDone
		}
		return k, v, engine.PairYielded
	})
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Map Iterator"), engine.FlagConfigurable)
}

// setRecord is a Set Record.
type setRecord struct {
	obj  *engine.Object
	size float64
	has  *engine.Object
	keys *engine.Object

	// next is the property key "next".
	next *engine.Object

	// table is the [[SetData]] of obj when obj is a Set and has is %Set.prototype.has%,
	// so that a lookup in table gives the result of calling has.
	table *collection.Table

	// pace counts the lookups in table, which run no JavaScript, between checks for an
	// interruption.
	pace engine.Pacer
}

// setLikeKeys are the property keys that the Set methods read from their argument.
type setLikeKeys struct {
	size, has, keys, next *engine.Object

	// setHas is %Set.prototype.has%.
	setHas *engine.Object
}

// getSetRecord implements GetSetRecord.
func getSetRecord(cx *engine.ExecutionContext, v engine.Value, k *setLikeKeys) setRecord {
	if !v.IsObject() {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not an object")
	}
	o := v.Object()
	n := cx.ToNumber(cx.Get(o, engine.NamedKey(k.size)))
	if n != n {
		cx.ThrowTypeError("the size of a set-like object must be a number")
	}
	size := cx.ToIntegerOrInfinity(engine.Number(n))
	if size < 0 {
		cx.ThrowRangeError("the size of a set-like object must not be negative")
	}
	has := cx.Get(o, engine.NamedKey(k.has))
	if !has.IsCallable() {
		cx.ThrowTypeError("the has method of a set-like object must be a function")
	}
	keys := cx.Get(o, engine.NamedKey(k.keys))
	if !keys.IsCallable() {
		cx.ThrowTypeError("the keys method of a set-like object must be a function")
	}
	r := setRecord{
		obj:  o,
		size: size,
		has:  has.Object(),
		keys: keys.Object(),
		next: k.next,
	}
	if r.has == k.setHas {
		r.table, _ = engine.SetData(v).(*collection.Table)
	}
	return r
}

// otherHas returns ToBoolean(Call(r.[[Has]], r.[[SetObject]], « e »)).
func (r *setRecord) otherHas(cx *engine.ExecutionContext, e engine.Value) bool {
	if r.table != nil {
		r.pace.Step(cx)
		return r.table.Has(e)
	}
	return engine.ToBoolean(cx.Call(r.has, engine.ObjectValue(r.obj), e))
}

// keysIterator implements GetKeysIterator(r) and returns the iterator and its next method.
func (r *setRecord) keysIterator(cx *engine.ExecutionContext) (it, next engine.Value) {
	it = cx.Call(r.keys, engine.ObjectValue(r.obj))
	if !it.IsObject() {
		cx.ThrowTypeError("the keys method of a set-like object must return an object")
	}
	next = cx.GetV(it, r.next)
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

func initSet(cx *engine.ExecutionContext) {
	proto := cx.Realm().Intrinsic(engine.IntrinsicSetPrototype)
	addKey := cx.Intern("add")
	keys := &setLikeKeys{
		size: cx.Intern("size"),
		has:  cx.Intern("has"),
		keys: cx.Intern("keys"),
		next: cx.Intern("next"),
	}
	// The original methods that iterating a Set calls.
	var setAdd, setValues, setIterProto, setIterNext *engine.Object
	ctor := constructor(cx, "Set", 0, engine.IntrinsicSetPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor Set requires 'new'")
		}
		t := &collection.Table{}
		targetProto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicSetPrototype)
		s := cx.NewSetObject(targetProto, t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(s)
		}
		if src := setOf(iterable); src != nil && targetProto == proto &&
			hasOwnMethod(cx, proto, engine.NamedKey(addKey), setAdd) &&
			iterationUnobservable(cx, iterable.Object(), proto, setValues, setIterProto, setIterNext, engine.NamedKey(keys.next)) {
			t.CopyFrom(src, cx.Poll)
			return engine.ObjectValue(s)
		}
		adder := adderOf(cx, s, addKey)
		if adder == setAdd {
			n := engine.ElementCount(iterable)
			var i int
			cx.Iterate(iterable, func(e engine.Value) bool {
				t.Set(e, e)
				i++
				reserveAfter(t, i, n)
				return true
			})
			t.Trim()
			return engine.ObjectValue(s)
		}
		cx.Iterate(iterable, func(e engine.Value) bool {
			cx.Call(adder, engine.ObjectValue(s), e)
			return true
		})
		return engine.ObjectValue(s)
	})
	speciesGetter(cx, ctor)

	newSet := func(cx *engine.ExecutionContext, t *collection.Table) engine.Value {
		return engine.ObjectValue(cx.NewSetObject(cx.Realm().Intrinsic(engine.IntrinsicSetPrototype), t))
	}

	// The kernels of the methods are the methods for a Set object.
	setAdd = method(cx, proto, "add", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "add")
		v := arg(args, 0)
		if !t.Has(v) {
			t.Append(v, v)
		}
		return this
	})
	cx.SetMethodKernels(setAdd, nil, func(cx *engine.ExecutionContext, this, v engine.Value) (engine.Value, bool) {
		t := setOf(this)
		if t == nil {
			return engine.Undefined, false
		}
		if !t.Has(v) {
			t.Append(v, v)
		}
		return this, true
	}, nil)
	method(cx, proto, "clear", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		thisSet(cx, this, "clear").Clear()
		return engine.Undefined
	})
	setDelete := method(cx, proto, "delete", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(cx, this, "delete").Delete(arg(args, 0)))
	})
	cx.SetMethodKernels(setDelete, nil, func(cx *engine.ExecutionContext, this, v engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Bool(t.Delete(v)), true
		}
		return engine.Undefined, false
	}, nil)
	keys.setHas = method(cx, proto, "has", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(cx, this, "has").Has(arg(args, 0)))
	})
	cx.SetMethodKernels(keys.setHas, nil, func(cx *engine.ExecutionContext, this, v engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Bool(t.Has(v)), true
		}
		return engine.Undefined, false
	}, nil)
	method(cx, proto, "forEach", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "forEach")
		cb := arg(args, 0)
		if !cb.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
		}
		thisArg := arg(args, 1)
		argv := []engine.Value{engine.Undefined, engine.Undefined, this}
		c := t.Start()
		for {
			k, _, ok := t.Next(&c)
			if !ok {
				return engine.Undefined
			}
			argv[0], argv[1], argv[2] = k, k, this
			cx.Call(cb.Object(), thisArg, argv...)
		}
	})
	setSize := getter(cx, proto, keys.size, "size", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisSet(cx, this, "size").Size()))
	})
	cx.SetMethodKernels(setSize, func(cx *engine.ExecutionContext, this engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Number(float64(t.Size())), true
		}
		return engine.Undefined, false
	}, nil, nil)

	method(cx, proto, "union", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "union")
		r := getSetRecord(cx, arg(args, 0), keys)
		it, next := r.keysIterator(cx)
		result := t.Clone(cx.Poll)
		cx.IterateRecord(it, next, func(e engine.Value) bool {
			if !result.Has(e) {
				result.Append(e, e)
			}
			return true
		})
		return newSet(cx, result)
	})
	method(cx, proto, "intersection", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "intersection")
		r := getSetRecord(cx, arg(args, 0), keys)
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
	})
	method(cx, proto, "difference", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "difference")
		r := getSetRecord(cx, arg(args, 0), keys)
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
	})
	method(cx, proto, "symmetricDifference", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "symmetricDifference")
		r := getSetRecord(cx, arg(args, 0), keys)
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
	})
	method(cx, proto, "isSubsetOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "isSubsetOf")
		r := getSetRecord(cx, arg(args, 0), keys)
		if float64(t.Size()) > r.size {
			return engine.False
		}
		c := t.Start()
		for {
			e, _, ok := t.Next(&c)
			if !ok {
				return engine.True
			}
			if !r.otherHas(cx, e) {
				return engine.False
			}
		}
	})
	method(cx, proto, "isSupersetOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "isSupersetOf")
		r := getSetRecord(cx, arg(args, 0), keys)
		if float64(t.Size()) < r.size {
			return engine.False
		}
		result := true
		r.eachKey(cx, func(e engine.Value) bool {
			result = t.Has(e)
			return result
		})
		return engine.Bool(result)
	})
	method(cx, proto, "isDisjointFrom", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(cx, this, "isDisjointFrom")
		r := getSetRecord(cx, arg(args, 0), keys)
		if float64(t.Size()) <= r.size {
			c := t.Start()
			for {
				e, _, ok := t.Next(&c)
				if !ok {
					return engine.True
				}
				if r.otherHas(cx, e) {
					return engine.False
				}
			}
		}
		result := true
		r.eachKey(cx, func(e engine.Value) bool {
			result = !t.Has(e)
			return result
		})
		return engine.Bool(result)
	})

	iterProto := cx.NewObject(cx.Realm().Intrinsic(engine.IntrinsicIteratorPrototype))
	setIterProto = iterProto
	iterate := func(name string, kind engine.IterationKind) *engine.Object {
		return method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			t := thisSet(cx, this, name)
			return engine.ObjectValue(cx.NewIteratorObject(iterProto, &setIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("entries", engine.IterateEntries)
	setValues = iterate("values", engine.IterateValues)
	cx.Define(proto, keys.keys, engine.ObjectValue(setValues), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(setValues), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Set"), engine.FlagConfigurable)

	setIterNext = plainIteratorNext(cx, iterProto, engine.IteratorClosureRealm, "next method called on an object that is not a Set Iterator", func(cx *engine.ExecutionContext, iterator *engine.Object, it *setIterator) (engine.Value, bool) {
		if it.table == nil {
			return engine.Undefined, false
		}
		k, _, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, false
		}
		if it.kind == engine.IterateEntries {
			return engine.ObjectValue(iterator.CreationRealm().Context().NewArrayPair(k, k)), true
		}
		return k, true
	})
	cx.SetIteratorPairStep(setIterNext, func(state any) (engine.Value, engine.Value, engine.PairStep) {
		it, _ := state.(*setIterator)
		if it == nil || it.kind != engine.IterateEntries {
			return engine.Undefined, engine.Undefined, engine.PairDeclined
		}
		if it.table == nil {
			return engine.Undefined, engine.Undefined, engine.PairDone
		}
		k, _, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, engine.Undefined, engine.PairDone
		}
		return k, k, engine.PairYielded
	})
	cx.Define(iterProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Set Iterator"), engine.FlagConfigurable)
}

func initObjectGroupBy(cx *engine.ExecutionContext) {
	method(cx, cx.Realm().Intrinsic(engine.IntrinsicObject), "groupBy", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
			cx.CreateDataPropertyOrThrow(o, k, engine.ObjectValue(groups[k]))
		}
		return engine.ObjectValue(o)
	})
}
