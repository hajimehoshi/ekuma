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

func thisMap(vm *engine.VM, this engine.Value, name string) *collection.Table {
	t, _ := engine.MapData(this).(*collection.Table)
	if t == nil {
		vm.ThrowTypeError("Map.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
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

func thisSet(vm *engine.VM, this engine.Value, name string) *collection.Table {
	t, _ := engine.SetData(this).(*collection.Table)
	if t == nil {
		vm.ThrowTypeError("Set.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
	}
	return t
}

// addEntriesFromIterable implements AddEntriesFromIterable. When fast is not nil, it is
// called instead of adder, which must then be the original set method of the collection.
func addEntriesFromIterable(vm *engine.VM, target *engine.Object, iterable engine.Value, adder *engine.Object, fast func(k, v engine.Value)) {
	vm.Iterate(iterable, func(e engine.Value) bool {
		if !e.IsObject() {
			vm.ThrowTypeError("iterator value " + vm.DisplayString(e) + " is not an entry object")
		}
		k := vm.Index(e, 0)
		v := vm.Index(e, 1)
		if fast != nil {
			fast(k, v)
		} else {
			vm.Call(adder, engine.ObjectValue(target), k, v)
		}
		return true
	})
}

// iterationUnobservable reports whether iterating the Map or Set o calls the method iter as its
// @@iterator, which proto holds, and the method next, which iterProto holds, finding both
// without running anything that JavaScript could observe.
func iterationUnobservable(vm *engine.VM, o, proto, iter, iterProto, next *engine.Object, nextKey engine.PropKey) bool {
	key := engine.NamedKey(vm.WellKnownSymbol(engine.SymbolIterator))
	if _, ok := vm.GetOwnProperty(o, key); ok || vm.ProtoOf(o) != proto {
		return false
	}
	return hasOwnMethod(vm, proto, key, iter) && hasOwnMethod(vm, iterProto, nextKey, next)
}

func hasOwnMethod(vm *engine.VM, proto *engine.Object, key engine.PropKey, method *engine.Object) bool {
	d, ok := vm.GetOwnProperty(proto, key)
	return ok && d.IsData() && d.Value().Object() == method
}

// reserveProbe is the number of elements of an array after which a collection made from the
// array makes room for all of them, if they all had different keys.
const reserveProbe = 16

// reserveAfter makes room in t for the entries of an array of n elements after the first i
// elements were added, when those had different keys.
func reserveAfter(t *collection.Table, i, n int) {
	if i == reserveProbe && t.Size() == reserveProbe && n >= 2*reserveProbe {
		t.Reserve(n)
	}
}

// adderOf returns the method name of the new collection o, or throws a TypeError.
func adderOf(vm *engine.VM, o *engine.Object, name *engine.Object) *engine.Object {
	adder := vm.Get(o, engine.NamedKey(name))
	if !adder.IsCallable() {
		vm.ThrowTypeError("the " + wtf8.ToValidUTF8(name.Str()) + " method of the new collection is not a function")
	}
	return adder.Object()
}

// speciesGetter defines the getter of Symbol.species that returns this on the constructor c.
func speciesGetter(vm *engine.VM, c *engine.Object) {
	getter(vm, c, vm.WellKnownSymbol(engine.SymbolSpecies), "[Symbol.species]", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
}

// groupBy implements GroupBy. It calls add with every value and the result of the callback for it.
func groupBy(vm *engine.VM, items, cb engine.Value, add func(k, v engine.Value)) {
	vm.RequireObjectCoercible(items, "groupBy")
	if !cb.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
	}
	k := 0.0
	vm.Iterate(items, func(v engine.Value) bool {
		key := vm.Call(cb.Object(), engine.Undefined, v, engine.Number(k))
		add(key, v)
		k++
		return true
	})
}

// appendToGroup adds v at the end of the array g of a group, which no code has seen yet.
func appendToGroup(vm *engine.VM, g *engine.Object, v engine.Value) {
	if _, ok := vm.AppendElements(g, []engine.Value{v}); ok {
		return
	}
	vm.CreateDataPropertyOrThrow(g, bigKey(vm, vm.LengthOfArrayLike(g)), v)
}

func initMapAndSet(vm *engine.VM) {
	initMap(vm)
	initSet(vm)
	initObjectGroupBy(vm)
}

func initMap(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicMapPrototype)
	setKey := vm.Intern("set")
	nextKey := engine.NamedKey(vm.Intern("next"))
	// The original methods that iterating a Map calls.
	var mapSet, mapEntries, mapIterProto, mapIterNext *engine.Object
	ctor := constructor(vm, "Map", 0, engine.IntrinsicMapPrototype, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor Map requires 'new'")
		}
		t := &collection.Table{}
		targetProto := vm.GetPrototypeFromConstructor(nt, engine.IntrinsicMapPrototype)
		m := vm.NewMapObject(targetProto, t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(m)
		}
		// A getter for set can modify or retain m. Bulk copying requires a fresh, unobserved table.
		if src := mapOf(iterable); src != nil && targetProto == proto &&
			hasOwnMethod(vm, proto, engine.NamedKey(setKey), mapSet) &&
			iterationUnobservable(vm, iterable.Object(), proto, mapEntries, mapIterProto, mapIterNext, nextKey) {
			t.CopyFrom(src, vm.Poll)
			return engine.ObjectValue(m)
		}
		adder := adderOf(vm, m, setKey)
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
		addEntriesFromIterable(vm, m, iterable, adder, fast)
		if fast != nil {
			t.Trim()
		}
		return engine.ObjectValue(m)
	})
	speciesGetter(vm, ctor)
	method(vm, ctor, "groupBy", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := &collection.Table{}
		groupBy(vm, arg(args, 0), arg(args, 1), func(k, v engine.Value) {
			g, ok := t.Get(k)
			if !ok {
				g = engine.ObjectValue(vm.NewArray(nil))
				t.Append(k, g)
			}
			appendToGroup(vm, g.Object(), v)
		})
		return engine.ObjectValue(vm.NewMapObject(vm.Intrinsic(engine.IntrinsicMapPrototype), t))
	})

	method(vm, proto, "clear", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisMap(vm, this, "clear").Clear()
		return engine.Undefined
	})
	// The kernels of the methods are the methods for a Map object.
	mapDelete := method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(vm, this, "delete").Delete(arg(args, 0)))
	})
	vm.SetMethodKernels(mapDelete, nil, func(vm *engine.VM, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Bool(t.Delete(k)), true
		}
		return engine.Undefined, false
	}, nil)
	mapGet := method(vm, proto, "get", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v, _ := thisMap(vm, this, "get").Get(arg(args, 0))
		return v
	})
	vm.SetMethodKernels(mapGet, nil, func(vm *engine.VM, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			v, _ := t.Get(k)
			return v, true
		}
		return engine.Undefined, false
	}, nil)
	mapHas := method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(vm, this, "has").Has(arg(args, 0)))
	})
	vm.SetMethodKernels(mapHas, nil, func(vm *engine.VM, this, k engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Bool(t.Has(k)), true
		}
		return engine.Undefined, false
	}, nil)
	mapSet = method(vm, proto, "set", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisMap(vm, this, "set").Set(arg(args, 0), arg(args, 1))
		return this
	})
	vm.SetMethodKernels(mapSet, nil, nil, func(vm *engine.VM, this, k, v engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			t.Set(k, v)
			return this, true
		}
		return engine.Undefined, false
	})
	method(vm, proto, "getOrInsert", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(vm, this, "getOrInsert")
		k, v := arg(args, 0), arg(args, 1)
		if old, ok := t.Get(k); ok {
			return old
		}
		t.Append(k, v)
		return v
	})
	method(vm, proto, "getOrInsertComputed", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(vm, this, "getOrInsertComputed")
		k, cb := arg(args, 0), arg(args, 1)
		if !cb.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
		}
		if k.IsNumber() && k.Number() == 0 {
			k = engine.Number(0)
		}
		if old, ok := t.Get(k); ok {
			return old
		}
		v := vm.Call(cb.Object(), engine.Undefined, k)
		t.Set(k, v)
		return v
	})
	method(vm, proto, "forEach", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisMap(vm, this, "forEach")
		cb := arg(args, 0)
		if !cb.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
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
			vm.Call(cb.Object(), thisArg, argv...)
		}
	})
	mapSize := getter(vm, proto, vm.Intern("size"), "size", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisMap(vm, this, "size").Size()))
	})
	vm.SetMethodKernels(mapSize, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		if t := mapOf(this); t != nil {
			return engine.Number(float64(t.Size())), true
		}
		return engine.Undefined, false
	}, nil, nil)

	iterProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicIteratorPrototype))
	mapIterProto = iterProto
	iterate := func(name string, kind engine.IterationKind) *engine.Object {
		return method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t := thisMap(vm, this, name)
			return engine.ObjectValue(vm.NewIteratorObject(iterProto, &mapIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("keys", engine.IterateKeys)
	iterate("values", engine.IterateValues)
	mapEntries = iterate("entries", engine.IterateEntries)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(mapEntries), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Map"), engine.FlagConfigurable)

	mapIterNext = plainIteratorNext(vm, iterProto, "next method called on an object that is not a Map Iterator", func(vm *engine.VM, it *mapIterator) (engine.Value, bool) {
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
		return engine.ObjectValue(vm.NewArrayPair(k, v)), true
	})
	vm.SetIteratorPairStep(mapIterNext, func(vm *engine.VM, this engine.Value) (engine.Value, engine.Value, engine.PairStep) {
		it, _ := engine.IteratorState(this).(*mapIterator)
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
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Map Iterator"), engine.FlagConfigurable)
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
}

// setLikeKeys are the property keys that the Set methods read from their argument.
type setLikeKeys struct {
	size, has, keys, next *engine.Object

	// setHas is %Set.prototype.has%.
	setHas *engine.Object
}

// getSetRecord implements GetSetRecord.
func getSetRecord(vm *engine.VM, v engine.Value, k *setLikeKeys) setRecord {
	if !v.IsObject() {
		vm.ThrowTypeError(vm.DisplayString(v) + " is not an object")
	}
	o := v.Object()
	n := vm.ToNumber(vm.Get(o, engine.NamedKey(k.size)))
	if n != n {
		vm.ThrowTypeError("the size of a set-like object must be a number")
	}
	size := vm.ToIntegerOrInfinity(engine.Number(n))
	if size < 0 {
		vm.ThrowRangeError("the size of a set-like object must not be negative")
	}
	has := vm.Get(o, engine.NamedKey(k.has))
	if !has.IsCallable() {
		vm.ThrowTypeError("the has method of a set-like object must be a function")
	}
	keys := vm.Get(o, engine.NamedKey(k.keys))
	if !keys.IsCallable() {
		vm.ThrowTypeError("the keys method of a set-like object must be a function")
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
func (r *setRecord) otherHas(vm *engine.VM, e engine.Value) bool {
	if r.table != nil {
		return r.table.Has(e)
	}
	return engine.ToBoolean(vm.Call(r.has, engine.ObjectValue(r.obj), e))
}

// keysIterator implements GetKeysIterator(r) and returns the iterator and its next method.
func (r *setRecord) keysIterator(vm *engine.VM) (it, next engine.Value) {
	it = vm.Call(r.keys, engine.ObjectValue(r.obj))
	if !it.IsObject() {
		vm.ThrowTypeError("the keys method of a set-like object must return an object")
	}
	next = vm.GetV(it, r.next)
	if !next.IsCallable() {
		vm.ThrowTypeError("the iterator of a set-like object must have a next method")
	}
	return it, next
}

// eachKey iterates the iterator that GetKeysIterator returns for r.
// When f returns false, the iteration stops and the iterator is closed.
func (r *setRecord) eachKey(vm *engine.VM, f func(engine.Value) bool) {
	it, next := r.keysIterator(vm)
	vm.IterateRecord(it, next, f)
}

func initSet(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicSetPrototype)
	addKey := vm.Intern("add")
	keys := &setLikeKeys{
		size: vm.Intern("size"),
		has:  vm.Intern("has"),
		keys: vm.Intern("keys"),
		next: vm.Intern("next"),
	}
	// The original methods that iterating a Set calls.
	var setAdd, setValues, setIterProto, setIterNext *engine.Object
	ctor := constructor(vm, "Set", 0, engine.IntrinsicSetPrototype, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor Set requires 'new'")
		}
		t := &collection.Table{}
		targetProto := vm.GetPrototypeFromConstructor(nt, engine.IntrinsicSetPrototype)
		s := vm.NewSetObject(targetProto, t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(s)
		}
		if src := setOf(iterable); src != nil && targetProto == proto &&
			hasOwnMethod(vm, proto, engine.NamedKey(addKey), setAdd) &&
			iterationUnobservable(vm, iterable.Object(), proto, setValues, setIterProto, setIterNext, engine.NamedKey(keys.next)) {
			t.CopyFrom(src, vm.Poll)
			return engine.ObjectValue(s)
		}
		adder := adderOf(vm, s, addKey)
		if adder == setAdd {
			n := engine.ElementCount(iterable)
			var i int
			vm.Iterate(iterable, func(e engine.Value) bool {
				t.Set(e, e)
				i++
				reserveAfter(t, i, n)
				return true
			})
			t.Trim()
			return engine.ObjectValue(s)
		}
		vm.Iterate(iterable, func(e engine.Value) bool {
			vm.Call(adder, engine.ObjectValue(s), e)
			return true
		})
		return engine.ObjectValue(s)
	})
	speciesGetter(vm, ctor)

	newSet := func(t *collection.Table) engine.Value {
		return engine.ObjectValue(vm.NewSetObject(vm.Intrinsic(engine.IntrinsicSetPrototype), t))
	}

	// The kernels of the methods are the methods for a Set object.
	setAdd = method(vm, proto, "add", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "add")
		v := arg(args, 0)
		if !t.Has(v) {
			t.Append(v, v)
		}
		return this
	})
	vm.SetMethodKernels(setAdd, nil, func(vm *engine.VM, this, v engine.Value) (engine.Value, bool) {
		t := setOf(this)
		if t == nil {
			return engine.Undefined, false
		}
		if !t.Has(v) {
			t.Append(v, v)
		}
		return this, true
	}, nil)
	method(vm, proto, "clear", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisSet(vm, this, "clear").Clear()
		return engine.Undefined
	})
	setDelete := method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(vm, this, "delete").Delete(arg(args, 0)))
	})
	vm.SetMethodKernels(setDelete, nil, func(vm *engine.VM, this, v engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Bool(t.Delete(v)), true
		}
		return engine.Undefined, false
	}, nil)
	keys.setHas = method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(vm, this, "has").Has(arg(args, 0)))
	})
	vm.SetMethodKernels(keys.setHas, nil, func(vm *engine.VM, this, v engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Bool(t.Has(v)), true
		}
		return engine.Undefined, false
	}, nil)
	method(vm, proto, "forEach", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "forEach")
		cb := arg(args, 0)
		if !cb.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
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
			vm.Call(cb.Object(), thisArg, argv...)
		}
	})
	setSize := getter(vm, proto, keys.size, "size", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisSet(vm, this, "size").Size()))
	})
	vm.SetMethodKernels(setSize, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		if t := setOf(this); t != nil {
			return engine.Number(float64(t.Size())), true
		}
		return engine.Undefined, false
	}, nil, nil)

	method(vm, proto, "union", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "union")
		r := getSetRecord(vm, arg(args, 0), keys)
		it, next := r.keysIterator(vm)
		result := t.Clone()
		vm.IterateRecord(it, next, func(e engine.Value) bool {
			if !result.Has(e) {
				result.Append(e, e)
			}
			return true
		})
		return newSet(result)
	})
	method(vm, proto, "intersection", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "intersection")
		r := getSetRecord(vm, arg(args, 0), keys)
		result := &collection.Table{}
		if float64(t.Size()) <= r.size {
			c := t.Start()
			for {
				e, _, ok := t.Next(&c)
				if !ok {
					break
				}
				if r.otherHas(vm, e) && !result.Has(e) {
					result.Append(e, e)
				}
			}
		} else {
			r.eachKey(vm, func(e engine.Value) bool {
				if t.Has(e) && !result.Has(e) {
					result.Append(e, e)
				}
				return true
			})
		}
		return newSet(result)
	})
	method(vm, proto, "difference", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "difference")
		r := getSetRecord(vm, arg(args, 0), keys)
		result := t.Clone()
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
				if r.otherHas(vm, e) {
					result.Delete(e)
				}
			}
		} else {
			r.eachKey(vm, func(e engine.Value) bool {
				result.Delete(e)
				return true
			})
		}
		return newSet(result)
	})
	method(vm, proto, "symmetricDifference", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "symmetricDifference")
		r := getSetRecord(vm, arg(args, 0), keys)
		it, next := r.keysIterator(vm)
		result := t.Clone()
		vm.IterateRecord(it, next, func(e engine.Value) bool {
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
		return newSet(result)
	})
	method(vm, proto, "isSubsetOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "isSubsetOf")
		r := getSetRecord(vm, arg(args, 0), keys)
		if float64(t.Size()) > r.size {
			return engine.False
		}
		c := t.Start()
		for {
			e, _, ok := t.Next(&c)
			if !ok {
				return engine.True
			}
			if !r.otherHas(vm, e) {
				return engine.False
			}
		}
	})
	method(vm, proto, "isSupersetOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "isSupersetOf")
		r := getSetRecord(vm, arg(args, 0), keys)
		if float64(t.Size()) < r.size {
			return engine.False
		}
		result := true
		r.eachKey(vm, func(e engine.Value) bool {
			result = t.Has(e)
			return result
		})
		return engine.Bool(result)
	})
	method(vm, proto, "isDisjointFrom", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "isDisjointFrom")
		r := getSetRecord(vm, arg(args, 0), keys)
		if float64(t.Size()) <= r.size {
			c := t.Start()
			for {
				e, _, ok := t.Next(&c)
				if !ok {
					return engine.True
				}
				if r.otherHas(vm, e) {
					return engine.False
				}
			}
		}
		result := true
		r.eachKey(vm, func(e engine.Value) bool {
			result = !t.Has(e)
			return result
		})
		return engine.Bool(result)
	})

	iterProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicIteratorPrototype))
	setIterProto = iterProto
	iterate := func(name string, kind engine.IterationKind) *engine.Object {
		return method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t := thisSet(vm, this, name)
			return engine.ObjectValue(vm.NewIteratorObject(iterProto, &setIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("entries", engine.IterateEntries)
	setValues = iterate("values", engine.IterateValues)
	vm.Define(proto, keys.keys, engine.ObjectValue(setValues), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(setValues), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Set"), engine.FlagConfigurable)

	setIterNext = plainIteratorNext(vm, iterProto, "next method called on an object that is not a Set Iterator", func(vm *engine.VM, it *setIterator) (engine.Value, bool) {
		if it.table == nil {
			return engine.Undefined, false
		}
		k, _, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, false
		}
		if it.kind == engine.IterateEntries {
			return engine.ObjectValue(vm.NewArrayPair(k, k)), true
		}
		return k, true
	})
	vm.SetIteratorPairStep(setIterNext, func(vm *engine.VM, this engine.Value) (engine.Value, engine.Value, engine.PairStep) {
		it, _ := engine.IteratorState(this).(*setIterator)
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
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Set Iterator"), engine.FlagConfigurable)
}

func initObjectGroupBy(vm *engine.VM) {
	method(vm, vm.Intrinsic(engine.IntrinsicObject), "groupBy", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var keys []engine.PropKey
		groups := map[engine.PropKey]*engine.Object{}
		groupBy(vm, arg(args, 0), arg(args, 1), func(k, v engine.Value) {
			pk := vm.ToPropertyKey(k)
			g, ok := groups[pk]
			if !ok {
				g = vm.NewArray(nil)
				groups[pk] = g
				keys = append(keys, pk)
			}
			appendToGroup(vm, g, v)
		})
		o := vm.NewObject(nil)
		for _, k := range keys {
			vm.CreateDataPropertyOrThrow(o, k, engine.ObjectValue(groups[k]))
		}
		return engine.ObjectValue(o)
	})
}
