// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initWeak(vm *engine.VM) {
	initWeakMap(vm)
	initWeakSet(vm)
	initWeakRef(vm)
	initFinalizationRegistry(vm)
}

// requireHoldable throws a TypeError when v cannot be held weakly.
func requireHoldable(vm *engine.VM, v engine.Value, what string) {
	if !vm.CanBeHeldWeakly(v) {
		vm.ThrowTypeError("invalid value used " + what + ": " + vm.DisplayString(v))
	}
}

func initWeakMap(vm *engine.VM) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(engine.IntrinsicWeakMapPrototype, proto)
	setKey := vm.Intern("set")
	var weakMapSet *engine.Object
	constructor(vm, "WeakMap", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor WeakMap requires 'new'")
		}
		m := vm.NewWeakMapObject(vm.GetPrototypeFromConstructor(nt, proto))
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(m)
		}
		adder := adderOf(vm, m, setKey)
		var fast func(k, v engine.Value)
		if adder == weakMapSet {
			t := engine.WeakMapData(engine.ObjectValue(m))
			fast = func(k, v engine.Value) {
				requireHoldable(vm, k, "as weak map key")
				t.Set(k, v)
			}
		}
		addEntriesFromIterable(vm, m, iterable, adder, fast)
		return engine.ObjectValue(m)
	})

	thisWeakMap := func(vm *engine.VM, this engine.Value, name string) *engine.WeakTable {
		t := engine.WeakMapData(this)
		if t == nil {
			vm.ThrowTypeError("WeakMap.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
		}
		return t
	}
	method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "delete")
		k := arg(args, 0)
		return engine.Bool(vm.CanBeHeldWeakly(k) && t.Delete(k))
	})
	method(vm, proto, "get", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "get")
		k := arg(args, 0)
		if !vm.CanBeHeldWeakly(k) {
			return engine.Undefined
		}
		v, _ := t.Get(k)
		return v
	})
	method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "has")
		k := arg(args, 0)
		return engine.Bool(vm.CanBeHeldWeakly(k) && t.Has(k))
	})
	weakMapSet = method(vm, proto, "set", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "set")
		k := arg(args, 0)
		requireHoldable(vm, k, "as weak map key")
		t.Set(k, arg(args, 1))
		return this
	})
	method(vm, proto, "getOrInsert", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "getOrInsert")
		k, v := arg(args, 0), arg(args, 1)
		requireHoldable(vm, k, "as weak map key")
		if old, ok := t.Get(k); ok {
			return old
		}
		t.Set(k, v)
		return v
	})
	method(vm, proto, "getOrInsertComputed", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(vm, this, "getOrInsertComputed")
		k, cb := arg(args, 0), arg(args, 1)
		if !cb.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
		}
		requireHoldable(vm, k, "as weak map key")
		if old, ok := t.Get(k); ok {
			return old
		}
		v := vm.Call(cb.Object(), engine.Undefined, k)
		t.Set(k, v)
		return v
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("WeakMap"), engine.FlagConfigurable)
}

func initWeakSet(vm *engine.VM) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(engine.IntrinsicWeakSetPrototype, proto)
	addKey := vm.Intern("add")
	var weakSetAdd *engine.Object
	constructor(vm, "WeakSet", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor WeakSet requires 'new'")
		}
		s := vm.NewWeakSetObject(vm.GetPrototypeFromConstructor(nt, proto))
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(s)
		}
		adder := adderOf(vm, s, addKey)
		t := engine.WeakSetData(engine.ObjectValue(s))
		vm.Iterate(iterable, func(e engine.Value) bool {
			if adder == weakSetAdd {
				requireHoldable(vm, e, "in weak set")
				t.Set(e, engine.True)
			} else {
				vm.Call(adder, engine.ObjectValue(s), e)
			}
			return true
		})
		return engine.ObjectValue(s)
	})

	thisWeakSet := func(vm *engine.VM, this engine.Value, name string) *engine.WeakTable {
		t := engine.WeakSetData(this)
		if t == nil {
			vm.ThrowTypeError("WeakSet.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
		}
		return t
	}
	weakSetAdd = method(vm, proto, "add", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(vm, this, "add")
		v := arg(args, 0)
		requireHoldable(vm, v, "in weak set")
		t.Set(v, engine.True)
		return this
	})
	method(vm, proto, "delete", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(vm, this, "delete")
		v := arg(args, 0)
		return engine.Bool(vm.CanBeHeldWeakly(v) && t.Delete(v))
	})
	method(vm, proto, "has", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(vm, this, "has")
		v := arg(args, 0)
		return engine.Bool(vm.CanBeHeldWeakly(v) && t.Has(v))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("WeakSet"), engine.FlagConfigurable)
}

func initWeakRef(vm *engine.VM) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(engine.IntrinsicWeakRefPrototype, proto)
	constructor(vm, "WeakRef", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor WeakRef requires 'new'")
		}
		target := arg(args, 0)
		requireHoldable(vm, target, "as WeakRef target")
		r := vm.NewWeakRefObject(vm.GetPrototypeFromConstructor(nt, proto), target)
		vm.AddToKeptObjects(target.Object())
		return engine.ObjectValue(r)
	})
	method(vm, proto, "deref", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v, ok := vm.WeakRefDeref(this)
		if !ok {
			vm.ThrowTypeError("WeakRef.prototype.deref called on incompatible receiver " + vm.DisplayString(this))
		}
		return v
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("WeakRef"), engine.FlagConfigurable)
}

func initFinalizationRegistry(vm *engine.VM) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(engine.IntrinsicFinalizationRegistryPrototype, proto)
	constructor(vm, "FinalizationRegistry", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor FinalizationRegistry requires 'new'")
		}
		cb := arg(args, 0)
		if !cb.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(cb) + " is not a function")
		}
		return engine.ObjectValue(vm.NewFinalizationRegistryObject(vm.GetPrototypeFromConstructor(nt, proto), cb.Object()))
	})

	thisRegistry := func(vm *engine.VM, this engine.Value, name string) *engine.FinalizationRegistry {
		r := engine.FinalizationRegistryData(this)
		if r == nil {
			vm.ThrowTypeError("FinalizationRegistry.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
		}
		return r
	}
	method(vm, proto, "register", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := thisRegistry(vm, this, "register")
		target, held, token := arg(args, 0), arg(args, 1), arg(args, 2)
		requireHoldable(vm, target, "as FinalizationRegistry target")
		if engine.SameValue(target, held) {
			vm.ThrowTypeError("the target and the held value of a FinalizationRegistry registration must differ")
		}
		if !vm.CanBeHeldWeakly(token) {
			if !token.IsUndefined() {
				vm.ThrowTypeError("invalid value used as unregister token: " + vm.DisplayString(token))
			}
		}
		r.Register(target, held, token)
		return engine.Undefined
	})
	method(vm, proto, "unregister", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := thisRegistry(vm, this, "unregister")
		token := arg(args, 0)
		requireHoldable(vm, token, "as unregister token")
		return engine.Bool(r.Unregister(token))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("FinalizationRegistry"), engine.FlagConfigurable)
}
