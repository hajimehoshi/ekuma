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

var (
	nativeWeakMapConstructor         = engine.RegisterNative(weakMapConstructor)
	nativeWeakMapDelete              = engine.RegisterNative(weakMapDelete)
	nativeWeakMapGet                 = engine.RegisterNative(weakMapGet)
	nativeWeakMapHas                 = engine.RegisterNative(weakMapHas)
	nativeWeakMapSet                 = engine.RegisterNative(weakMapSet)
	nativeWeakMapGetOrInsert         = engine.RegisterNative(weakMapGetOrInsert)
	nativeWeakMapGetOrInsertComputed = engine.RegisterNative(weakMapGetOrInsertComputed)
)

func initWeakMap(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakMapPrototype, proto)
	constructor(cx, "WeakMap", 0, engine.IntrinsicWeakMapPrototype, nativeWeakMapConstructor)

	method(cx, proto, "delete", 1, nativeWeakMapDelete)
	method(cx, proto, "get", 1, nativeWeakMapGet)
	method(cx, proto, "has", 1, nativeWeakMapHas)
	set := method(cx, proto, "set", 2, nativeWeakMapSet)
	cx.SetIntrinsic(engine.IntrinsicWeakMapPrototypeSet, set)
	method(cx, proto, "getOrInsert", 2, nativeWeakMapGetOrInsert)
	method(cx, proto, "getOrInsertComputed", 2, nativeWeakMapGetOrInsertComputed)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakMap"), engine.FlagConfigurable)
}

func weakMapConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor WeakMap requires 'new'")
	}
	m := cx.NewWeakMapObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakMapPrototype))
	iterable := arg(args, 0)
	if iterable.IsNullish() {
		return engine.ObjectValue(m)
	}
	adder := adderOf(cx, m, cx.Key(engine.KeySet))
	var fast func(k, v engine.Value)
	if adder == cx.Intrinsic(engine.IntrinsicWeakMapPrototypeSet) {
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
}

func weakMapDelete(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakMap(cx, this, "delete")
	k := arg(args, 0)
	return engine.Bool(cx.CanBeHeldWeakly(k) && t.Delete(k))
}

func weakMapGet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakMap(cx, this, "get")
	k := arg(args, 0)
	if !cx.CanBeHeldWeakly(k) {
		return engine.Undefined
	}
	v, _ := t.Get(k)
	return v
}

func weakMapHas(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakMap(cx, this, "has")
	k := arg(args, 0)
	return engine.Bool(cx.CanBeHeldWeakly(k) && t.Has(k))
}

func weakMapSet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakMap(cx, this, "set")
	k := arg(args, 0)
	requireHoldable(cx, k, "as weak map key")
	t.Set(k, arg(args, 1))
	return this
}

func weakMapGetOrInsert(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakMap(cx, this, "getOrInsert")
	k, v := arg(args, 0), arg(args, 1)
	requireHoldable(cx, k, "as weak map key")
	if old, ok := t.Get(k); ok {
		return old
	}
	t.Set(k, v)
	return v
}

func weakMapGetOrInsertComputed(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func thisWeakSet(cx *engine.ExecutionContext, this engine.Value, name string) *engine.WeakTable {
	t := engine.WeakSetData(this)
	if t == nil {
		cx.ThrowTypeError("WeakSet.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return t
}

var (
	nativeWeakSetConstructor = engine.RegisterNative(weakSetConstructor)
	nativeWeakSetAdd         = engine.RegisterNative(weakSetAdd)
	nativeWeakSetDelete      = engine.RegisterNative(weakSetDelete)
	nativeWeakSetHas         = engine.RegisterNative(weakSetHas)
)

func initWeakSet(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakSetPrototype, proto)
	constructor(cx, "WeakSet", 0, engine.IntrinsicWeakSetPrototype, nativeWeakSetConstructor)

	add := method(cx, proto, "add", 1, nativeWeakSetAdd)
	cx.SetIntrinsic(engine.IntrinsicWeakSetPrototypeAdd, add)
	method(cx, proto, "delete", 1, nativeWeakSetDelete)
	method(cx, proto, "has", 1, nativeWeakSetHas)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakSet"), engine.FlagConfigurable)
}

func weakSetConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor WeakSet requires 'new'")
	}
	s := cx.NewWeakSetObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakSetPrototype))
	iterable := arg(args, 0)
	if iterable.IsNullish() {
		return engine.ObjectValue(s)
	}
	adder := adderOf(cx, s, cx.Key(engine.KeyAdd))
	original := adder == cx.Intrinsic(engine.IntrinsicWeakSetPrototypeAdd)
	t := engine.WeakSetData(engine.ObjectValue(s))
	cx.Iterate(iterable, func(e engine.Value) bool {
		if original && cx.CanBeHeldWeakly(e) {
			t.Set(e, engine.True)
		} else {
			// This includes the TypeError of the original method.
			cx.Call(adder, engine.ObjectValue(s), e)
		}
		return true
	})
	return engine.ObjectValue(s)
}

func weakSetAdd(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakSet(cx, this, "add")
	v := arg(args, 0)
	requireHoldable(cx, v, "in weak set")
	t.Set(v, engine.True)
	return this
}

func weakSetDelete(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakSet(cx, this, "delete")
	v := arg(args, 0)
	return engine.Bool(cx.CanBeHeldWeakly(v) && t.Delete(v))
}

func weakSetHas(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisWeakSet(cx, this, "has")
	v := arg(args, 0)
	return engine.Bool(cx.CanBeHeldWeakly(v) && t.Has(v))
}

var (
	nativeWeakRefConstructor = engine.RegisterNative(weakRefConstructor)
	nativeWeakRefDeref       = engine.RegisterNative(weakRefDeref)
)

func initWeakRef(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicWeakRefPrototype, proto)
	constructor(cx, "WeakRef", 1, engine.IntrinsicWeakRefPrototype, nativeWeakRefConstructor)
	method(cx, proto, "deref", 0, nativeWeakRefDeref)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("WeakRef"), engine.FlagConfigurable)
}

func weakRefConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor WeakRef requires 'new'")
	}
	target := arg(args, 0)
	requireHoldable(cx, target, "as WeakRef target")
	r := cx.NewWeakRefObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicWeakRefPrototype), target)
	cx.AddToKeptObjects(target.Object())
	return engine.ObjectValue(r)
}

func weakRefDeref(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	v, ok := cx.WeakRefDeref(this)
	if !ok {
		cx.ThrowTypeError("WeakRef.prototype.deref called on incompatible receiver " + cx.DisplayString(this))
	}
	return v
}

func thisFinalizationRegistry(cx *engine.ExecutionContext, this engine.Value, name string) *engine.FinalizationRegistry {
	r := engine.FinalizationRegistryData(this)
	if r == nil {
		cx.ThrowTypeError("FinalizationRegistry.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return r
}

var (
	nativeFinalizationRegistryConstructor = engine.RegisterNative(finalizationRegistryConstructor)
	nativeFinalizationRegistryRegister    = engine.RegisterNative(finalizationRegistryRegister)
	nativeFinalizationRegistryUnregister  = engine.RegisterNative(finalizationRegistryUnregister)
)

func initFinalizationRegistry(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicFinalizationRegistryPrototype, proto)
	constructor(cx, "FinalizationRegistry", 1, engine.IntrinsicFinalizationRegistryPrototype, nativeFinalizationRegistryConstructor)

	method(cx, proto, "register", 2, nativeFinalizationRegistryRegister)
	method(cx, proto, "unregister", 1, nativeFinalizationRegistryUnregister)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("FinalizationRegistry"), engine.FlagConfigurable)
}

func finalizationRegistryConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor FinalizationRegistry requires 'new'")
	}
	cb := arg(args, 0)
	if !cb.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(cb) + " is not a function")
	}
	return engine.ObjectValue(cx.NewFinalizationRegistryObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicFinalizationRegistryPrototype), cb.Object()))
}

func finalizationRegistryRegister(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func finalizationRegistryUnregister(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := thisFinalizationRegistry(cx, this, "unregister")
	token := arg(args, 0)
	requireHoldable(cx, token, "as unregister token")
	return engine.Bool(r.Unregister(token, cx.Poll))
}
