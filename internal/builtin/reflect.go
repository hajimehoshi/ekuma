// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func requireReflectTarget(cx *engine.ExecutionContext, v engine.Value, name string) {
	if !v.IsObject() {
		throwReflectTarget(cx, name)
	}
}

//go:noinline
func throwReflectTarget(cx *engine.ExecutionContext, name string) {
	cx.ThrowTypeError("Reflect." + name + " called on non-object")
}

// requireReflectArgumentList throws the TypeError of CreateListFromArrayLike for a v that is not an
// object.
func requireReflectArgumentList(cx *engine.ExecutionContext, v engine.Value, name string) {
	if !v.IsObject() {
		throwReflectArgumentList(cx, name)
	}
}

//go:noinline
func throwReflectArgumentList(cx *engine.ExecutionContext, name string) {
	cx.ThrowTypeError("Reflect." + name + " requires an array-like object as the argument list")
}

var (
	nativeReflectApply                    = engine.RegisterNative(reflectApply)
	nativeReflectConstruct                = engine.RegisterNative(reflectConstruct)
	nativeReflectDefineProperty           = engine.RegisterNative(reflectDefineProperty)
	nativeReflectDeleteProperty           = engine.RegisterNative(reflectDeleteProperty)
	nativeReflectGet                      = engine.RegisterNative(reflectGet)
	nativeReflectGetOwnPropertyDescriptor = engine.RegisterNative(reflectGetOwnPropertyDescriptor)
	nativeReflectGetPrototypeOf           = engine.RegisterNative(reflectGetPrototypeOf)
	nativeReflectHas                      = engine.RegisterNative(reflectHas)
	nativeReflectIsExtensible             = engine.RegisterNative(reflectIsExtensible)
	nativeReflectOwnKeys                  = engine.RegisterNative(reflectOwnKeys)
	nativeReflectPreventExtensions        = engine.RegisterNative(reflectPreventExtensions)
	nativeReflectSet                      = engine.RegisterNative(reflectSet)
	nativeReflectSetPrototypeOf           = engine.RegisterNative(reflectSetPrototypeOf)
)

func initReflect(cx *engine.ExecutionContext) {
	r := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Reflect"), storage.ObjectValue(r), engine.FlagsHidden)
	cx.Define(r, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Reflect"), engine.FlagConfigurable)

	// Reflect.apply and Reflect.construct show no frame, as Function.prototype.call and apply.
	apply := method(cx, r, "apply", 3, nativeReflectApply)
	cx.SetFrameless(apply)
	construct := method(cx, r, "construct", 2, nativeReflectConstruct)
	cx.SetFrameless(construct)
	method(cx, r, "defineProperty", 3, nativeReflectDefineProperty)
	method(cx, r, "deleteProperty", 2, nativeReflectDeleteProperty)
	method(cx, r, "get", 2, nativeReflectGet)
	method(cx, r, "getOwnPropertyDescriptor", 2, nativeReflectGetOwnPropertyDescriptor)
	method(cx, r, "getPrototypeOf", 1, nativeReflectGetPrototypeOf)
	method(cx, r, "has", 2, nativeReflectHas)
	method(cx, r, "isExtensible", 1, nativeReflectIsExtensible)
	method(cx, r, "ownKeys", 1, nativeReflectOwnKeys)
	method(cx, r, "preventExtensions", 1, nativeReflectPreventExtensions)
	method(cx, r, "set", 3, nativeReflectSet)
	method(cx, r, "setPrototypeOf", 2, nativeReflectSetPrototypeOf)
}

func reflectApply(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := arg(args, 0)
	if !f.IsCallable() {
		cx.ThrowTypeError("Reflect.apply requires a function as the target")
	}
	requireReflectArgumentList(cx, arg(args, 2), "apply")
	return cx.Call(f.Object(), args[1], listFromArrayLike(cx, args[2])...)
}

func reflectConstruct(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := arg(args, 0)
	if !f.IsObject() || !cx.IsConstructor(f.Object()) {
		cx.ThrowTypeError("Reflect.construct requires a constructor as the target")
	}
	nt := f
	if len(args) > 2 {
		nt = args[2]
		if !nt.IsObject() || !cx.IsConstructor(nt.Object()) {
			cx.ThrowTypeError("Reflect.construct requires a constructor as the new target")
		}
	}
	requireReflectArgumentList(cx, arg(args, 1), "construct")
	list := listFromArrayLike(cx, args[1])
	return storage.ObjectValue(cx.Construct(f.Object(), list, nt.Object()))
}

func reflectDefineProperty(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "defineProperty")
	o := args[0].Object()
	k := cx.ToPropertyKey(arg(args, 1))
	return storage.Bool(cx.DefineOwnProperty(o, k, cx.ToPropertyDescriptor(arg(args, 2))))
}

func reflectDeleteProperty(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "deleteProperty")
	o := args[0].Object()
	return storage.Bool(cx.DeleteProp(o, cx.ToPropertyKey(arg(args, 1))))
}

func reflectGet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "get")
	o := args[0].Object()
	k := cx.ToPropertyKey(arg(args, 1))
	receiver := args[0]
	if len(args) > 2 {
		receiver = args[2]
	}
	return cx.GetWithReceiver(o, k, receiver)
}

func reflectGetOwnPropertyDescriptor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "getOwnPropertyDescriptor")
	o := args[0].Object()
	d, ok := cx.GetOwnProperty(o, cx.ToPropertyKey(arg(args, 1)))
	if !ok {
		return storage.Undefined
	}
	return cx.FromPropertyDescriptor(d)
}

func reflectGetPrototypeOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "getPrototypeOf")
	if p := cx.ProtoOf(args[0].Object()); p != nil {
		return storage.ObjectValue(p)
	}
	return storage.Null
}

func reflectHas(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "has")
	o := args[0].Object()
	return storage.Bool(cx.QueryProperty(o, cx.QueryKey(arg(args, 1))))
}

func reflectIsExtensible(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "isExtensible")
	return storage.Bool(cx.IsExtensible(args[0].Object()))
}

func reflectOwnKeys(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "ownKeys")
	keys := cx.OwnKeys(args[0].Object())
	out := make([]engine.Value, len(keys))
	var pacer engine.Pacer
	for i, k := range keys {
		pacer.Step(cx)
		out[i] = cx.KeyValue(k)
	}
	return storage.ObjectValue(cx.NewArray(out))
}

func reflectPreventExtensions(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "preventExtensions")
	return storage.Bool(cx.PreventExtensions(args[0].Object()))
}

func reflectSet(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "set")
	o := args[0].Object()
	k := cx.ToPropertyKey(arg(args, 1))
	receiver := args[0]
	if len(args) > 3 {
		receiver = args[3]
	}
	return storage.Bool(cx.SetWithReceiver(o, k, arg(args, 2), receiver))
}

func reflectSetPrototypeOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	requireReflectTarget(cx, arg(args, 0), "setPrototypeOf")
	o := args[0].Object()
	p := arg(args, 1)
	if !p.IsObject() && !p.IsNull() {
		cx.ThrowTypeError("object prototype may only be an Object or null: " + cx.DisplayString(p))
	}
	var po *engine.Object
	if p.IsObject() {
		po = p.Object()
	}
	return storage.Bool(cx.SetPrototypeOf(o, po))
}
