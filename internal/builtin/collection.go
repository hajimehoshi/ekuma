// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// mapIterator is the state of a Map Iterator object.
type mapIterator struct {
	// table is nil once the iterator is done.
	table  *collection.Table
	cursor collection.Cursor
	kind   int
}

// setIterator is the state of a Set Iterator object.
type setIterator struct {
	// table is nil once the iterator is done.
	table  *collection.Table
	cursor collection.Cursor
	kind   int
}

func thisMap(vm *engine.VM, this engine.Value, name string) *collection.Table {
	t, _ := engine.MapData(this).(*collection.Table)
	if t == nil {
		vm.ThrowTypeError("Map.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
	}
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

// adderOf returns the method name of the new collection o, or throws a TypeError.
func adderOf(vm *engine.VM, o *engine.Object, name *engine.Object) *engine.Object {
	adder := vm.Get(o, engine.NamedKey(name))
	if !adder.IsCallable() {
		vm.ThrowTypeError("the " + name.Str() + " method of the new collection is not a function")
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

func initMapAndSet(vm *engine.VM) {
	initMap(vm)
	initSet(vm)
	initObjectGroupBy(vm)
}

func initMap(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicMapPrototype)
	setKey := vm.Intern("set")
	var mapSet *engine.Object
	ctor := constructor(vm, "Map", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor Map requires 'new'")
		}
		t := &collection.Table{}
		m := vm.NewMapObject(vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicMapPrototype)), t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(m)
		}
		adder := adderOf(vm, m, setKey)
		var fast func(k, v engine.Value)
		if adder == mapSet {
			fast = t.Set
		}
		addEntriesFromIterable(vm, m, iterable, adder, fast)
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
			vm.AppendElements(g.Object(), []engine.Value{v})
		})
		return engine.ObjectValue(vm.NewMapObject(vm.Intrinsic(engine.IntrinsicMapPrototype), t))
	})

	method(vm, proto, "clear", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisMap(vm, this, "clear").Clear()
		return engine.Undefined
	})
	method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(vm, this, "delete").Delete(arg(args, 0)))
	})
	method(vm, proto, "get", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v, _ := thisMap(vm, this, "get").Get(arg(args, 0))
		return v
	})
	method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisMap(vm, this, "has").Has(arg(args, 0)))
	})
	mapSet = method(vm, proto, "set", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisMap(vm, this, "set").Set(arg(args, 0), arg(args, 1))
		return this
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
	getter(vm, proto, vm.Intern("size"), "size", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisMap(vm, this, "size").Size()))
	})

	iterProto := vm.NewObject(vm.Intrinsic(engine.IntrinsicIteratorPrototype))
	iterate := func(name string, kind int) *engine.Object {
		return method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t := thisMap(vm, this, name)
			return engine.ObjectValue(vm.NewIteratorObject(iterProto, &mapIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("keys", iterKeys)
	iterate("values", iterValues)
	entries := iterate("entries", iterEntries)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(entries), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Map"), engine.FlagConfigurable)

	iteratorNext(vm, iterProto, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		it, _ := engine.IteratorState(this).(*mapIterator)
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not a Map Iterator")
		}
		if it.table == nil {
			return engine.Undefined, false
		}
		k, v, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, false
		}
		switch it.kind {
		case iterKeys:
			return k, true
		case iterValues:
			return v, true
		}
		return engine.ObjectValue(vm.NewArrayPair(k, v)), true
	})
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Map Iterator"), engine.FlagConfigurable)
}

// setRecord is a Set Record.
type setRecord struct {
	obj  *engine.Object
	size float64
	has  *engine.Object
	keys *engine.Object

	// next is the property key "next".
	next *engine.Object
}

// setLikeKeys are the property keys that the Set methods read from their argument.
type setLikeKeys struct {
	size, has, keys, next *engine.Object
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
	return setRecord{
		obj:  o,
		size: size,
		has:  has.Object(),
		keys: keys.Object(),
		next: k.next,
	}
}

// otherHas returns ToBoolean(Call(r.[[Has]], r.[[SetObject]], « e »)).
func (r *setRecord) otherHas(vm *engine.VM, e engine.Value) bool {
	return engine.ToBoolean(vm.Call(r.has, engine.ObjectValue(r.obj), e))
}

// eachKey iterates the iterator that GetIteratorFromMethod returns for the keys method of r.
// When f returns false, the iteration stops and the iterator is closed.
func (r *setRecord) eachKey(vm *engine.VM, f func(engine.Value) bool) {
	it := vm.Call(r.keys, engine.ObjectValue(r.obj))
	if !it.IsObject() {
		vm.ThrowTypeError("the keys method of a set-like object must return an object")
	}
	next := vm.GetV(it, r.next)
	if !next.IsCallable() {
		vm.ThrowTypeError("the iterator of a set-like object must have a next method")
	}
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
	var setAdd *engine.Object
	ctor := constructor(vm, "Set", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor Set requires 'new'")
		}
		t := &collection.Table{}
		s := vm.NewSetObject(vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicSetPrototype)), t)
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(s)
		}
		adder := adderOf(vm, s, addKey)
		if adder == setAdd {
			vm.Iterate(iterable, func(e engine.Value) bool {
				t.Set(e, e)
				return true
			})
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

	setAdd = method(vm, proto, "add", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "add")
		v := arg(args, 0)
		if !t.Has(v) {
			t.Append(v, v)
		}
		return this
	})
	method(vm, proto, "clear", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisSet(vm, this, "clear").Clear()
		return engine.Undefined
	})
	method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(vm, this, "delete").Delete(arg(args, 0)))
	})
	method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisSet(vm, this, "has").Has(arg(args, 0)))
	})
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
	getter(vm, proto, keys.size, "size", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisSet(vm, this, "size").Size()))
	})

	method(vm, proto, "union", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisSet(vm, this, "union")
		r := getSetRecord(vm, arg(args, 0), keys)
		result := t.Clone()
		r.eachKey(vm, func(e engine.Value) bool {
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
			var keys []engine.Value
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
		result := t.Clone()
		r.eachKey(vm, func(e engine.Value) bool {
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
	iterate := func(name string, kind int) *engine.Object {
		return method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t := thisSet(vm, this, name)
			return engine.ObjectValue(vm.NewIteratorObject(iterProto, &setIterator{
				table:  t,
				cursor: t.Start(),
				kind:   kind,
			}))
		})
	}
	iterate("entries", iterEntries)
	values := iterate("values", iterValues)
	vm.Define(proto, keys.keys, engine.ObjectValue(values), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(values), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Set"), engine.FlagConfigurable)

	iteratorNext(vm, iterProto, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		it, _ := engine.IteratorState(this).(*setIterator)
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not a Set Iterator")
		}
		if it.table == nil {
			return engine.Undefined, false
		}
		k, _, ok := it.table.Next(&it.cursor)
		if !ok {
			it.table = nil
			return engine.Undefined, false
		}
		if it.kind == iterEntries {
			return engine.ObjectValue(vm.NewArrayPair(k, k)), true
		}
		return k, true
	})
	vm.Define(iterProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Set Iterator"), engine.FlagConfigurable)
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
			vm.AppendElements(g, []engine.Value{v})
		})
		o := vm.NewObject(nil)
		for _, k := range keys {
			vm.CreateDataPropertyOrThrow(o, k, engine.ObjectValue(groups[k]))
		}
		return engine.ObjectValue(o)
	})
}
