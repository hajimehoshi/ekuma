// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initWeak(cx *engine.ExecutionContext) {
	initWeakMap(cx)
	initWeakSet(cx)
	initWeakRef(cx)
	initFinalizationRegistry(cx)
}

// requireHoldable throws a TypeError when v cannot be held weakly.
func requireHoldable(cx *engine.ExecutionContext, v engine.Value, what string) {
	if !cx.CanBeHeldWeakly(v) {
		cx.ThrowTypeError("invalid value used " + what + ": " + cx.DisplayString(v))
	}
}

func thisWeakMap(cx *engine.ExecutionContext, this engine.Value, name string) *engine.WeakTable {
	t := engine.WeakMapData(this)
	if t == nil {
		cx.ThrowTypeError("WeakMap.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

func initWeakMap(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakMapPrototype, proto)
	setKey := cx.Intern("set")
	var weakMapSet *engine.Object
	constructor(cx, "WeakMap", 0, engine.IntrinsicWeakMapPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor WeakMap requires 'new'")
		}
		m := cx.NewWeakMapObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakMapPrototype))
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(m)
		}
		adder := adderOf(cx, m, setKey)
		var fast func(k, v engine.Value)
		if adder == weakMapSet {
			t := engine.WeakMapData(engine.ObjectValue(m))
			fast = func(k, v engine.Value) {
				if !cx.CanBeHeldWeakly(k) {
					// The TypeError belongs to the native set call.
					cx.Call(adder, engine.ObjectValue(m), k, v)
					return
				}
				t.Set(k, v)
			}
		}
		addEntriesFromIterable(cx, m, iterable, adder, fast)
		return engine.ObjectValue(m)
	})

	method(cx, proto, "delete", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "delete")
		k := arg(args, 0)
		return engine.Bool(cx.CanBeHeldWeakly(k) && t.Delete(k))
	})
	method(cx, proto, "get", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "get")
		k := arg(args, 0)
		if !cx.CanBeHeldWeakly(k) {
			return engine.Undefined
		}
		v, _ := t.Get(k)
		return v
	})
	method(cx, proto, "has", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "has")
		k := arg(args, 0)
		return engine.Bool(cx.CanBeHeldWeakly(k) && t.Has(k))
	})
	weakMapSet = method(cx, proto, "set", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "set")
		k := arg(args, 0)
		requireHoldable(cx, k, "as weak map key")
		t.Set(k, arg(args, 1))
		return this
	})
	method(cx, proto, "getOrInsert", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "getOrInsert")
		k, v := arg(args, 0), arg(args, 1)
		requireHoldable(cx, k, "as weak map key")
		if old, ok := t.Get(k); ok {
			return old
		}
		t.Set(k, v)
		return v
	})
	method(cx, proto, "getOrInsertComputed", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakMap(cx, this, "getOrInsertComputed")
		k, cb := arg(args, 0), arg(args, 1)
		if !cb.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
		}
		requireHoldable(cx, k, "as weak map key")
		if old, ok := t.Get(k); ok {
			return old
		}
		v := cx.Call(cb.Object(), engine.Undefined, k)
		t.Set(k, v)
		return v
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakMap"), engine.FlagConfigurable)
}

func thisWeakSet(cx *engine.ExecutionContext, this engine.Value, name string) *engine.WeakTable {
	t := engine.WeakSetData(this)
	if t == nil {
		cx.ThrowTypeError("WeakSet.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

func initWeakSet(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakSetPrototype, proto)
	addKey := cx.Intern("add")
	var weakSetAdd *engine.Object
	constructor(cx, "WeakSet", 0, engine.IntrinsicWeakSetPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor WeakSet requires 'new'")
		}
		s := cx.NewWeakSetObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakSetPrototype))
		iterable := arg(args, 0)
		if iterable.IsNullish() {
			return engine.ObjectValue(s)
		}
		adder := adderOf(cx, s, addKey)
		t := engine.WeakSetData(engine.ObjectValue(s))
		cx.Iterate(iterable, func(e engine.Value) bool {
			if adder == weakSetAdd && cx.CanBeHeldWeakly(e) {
				t.Set(e, engine.True)
			} else {
				// This includes the TypeError of the original method.
				cx.Call(adder, engine.ObjectValue(s), e)
			}
			return true
		})
		return engine.ObjectValue(s)
	})

	weakSetAdd = method(cx, proto, "add", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(cx, this, "add")
		v := arg(args, 0)
		requireHoldable(cx, v, "in weak set")
		t.Set(v, engine.True)
		return this
	})
	method(cx, proto, "delete", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(cx, this, "delete")
		v := arg(args, 0)
		return engine.Bool(cx.CanBeHeldWeakly(v) && t.Delete(v))
	})
	method(cx, proto, "has", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisWeakSet(cx, this, "has")
		v := arg(args, 0)
		return engine.Bool(cx.CanBeHeldWeakly(v) && t.Has(v))
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakSet"), engine.FlagConfigurable)
}

func initWeakRef(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakRefPrototype, proto)
	constructor(cx, "WeakRef", 1, engine.IntrinsicWeakRefPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor WeakRef requires 'new'")
		}
		target := arg(args, 0)
		requireHoldable(cx, target, "as WeakRef target")
		r := cx.NewWeakRefObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakRefPrototype), target)
		cx.AddToKeptObjects(target.Object())
		return engine.ObjectValue(r)
	})
	method(cx, proto, "deref", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v, ok := cx.WeakRefDeref(this)
		if !ok {
			cx.ThrowTypeError("WeakRef.prototype.deref called on incompatible receiver " + cx.DisplayString(this))
		}
		return v
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakRef"), engine.FlagConfigurable)
}

func thisFinalizationRegistry(cx *engine.ExecutionContext, this engine.Value, name string) *engine.FinalizationRegistry {
	r := engine.FinalizationRegistryData(this)
	if r == nil {
		cx.ThrowTypeError("FinalizationRegistry.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return r
}

func initFinalizationRegistry(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicFinalizationRegistryPrototype, proto)
	constructor(cx, "FinalizationRegistry", 1, engine.IntrinsicFinalizationRegistryPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor FinalizationRegistry requires 'new'")
		}
		cb := arg(args, 0)
		if !cb.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
		}
		return engine.ObjectValue(cx.NewFinalizationRegistryObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicFinalizationRegistryPrototype), cb.Object()))
	})

	method(cx, proto, "register", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := thisFinalizationRegistry(cx, this, "register")
		target, held, token := arg(args, 0), arg(args, 1), arg(args, 2)
		requireHoldable(cx, target, "as FinalizationRegistry target")
		if engine.SameValue(target, held) {
			cx.ThrowTypeError("the target and the held value of a FinalizationRegistry registration must differ")
		}
		if !cx.CanBeHeldWeakly(token) {
			if !token.IsUndefined() {
				cx.ThrowTypeError("invalid value used as unregister token: " + cx.DisplayString(token))
			}
		}
		r.Register(target, held, token)
		return engine.Undefined
	})
	method(cx, proto, "unregister", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := thisFinalizationRegistry(cx, this, "unregister")
		token := arg(args, 0)
		requireHoldable(cx, token, "as unregister token")
		return engine.Bool(r.Unregister(token, cx.Poll))
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("FinalizationRegistry"), engine.FlagConfigurable)
}
