// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
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

func initReflect(cx *engine.ExecutionContext) {
	r := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Reflect"), engine.ObjectValue(r), engine.FlagsHidden)
	cx.Define(r, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Reflect"), engine.FlagConfigurable)

	// Reflect.apply and Reflect.construct show no frame, as Function.prototype.call and apply.
	apply := method(cx, r, "apply", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		f := arg(args, 0)
		if !f.IsCallable() {
			cx.ThrowTypeError("Reflect.apply requires a function as the target")
		}
		requireReflectArgumentList(cx, arg(args, 2), "apply")
		return cx.Call(f.Object(), args[1], listFromArrayLike(cx, args[2])...)
	})
	cx.SetFrameless(apply)
	construct := method(cx, r, "construct", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		return engine.ObjectValue(cx.Construct(f.Object(), list, nt.Object()))
	})
	cx.SetFrameless(construct)
	method(cx, r, "defineProperty", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "defineProperty")
		o := args[0].Object()
		k := cx.ToPropertyKey(arg(args, 1))
		return engine.Bool(cx.DefineOwnProperty(o, k, cx.ToPropertyDescriptor(arg(args, 2))))
	})
	method(cx, r, "deleteProperty", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "deleteProperty")
		o := args[0].Object()
		return engine.Bool(cx.DeleteProp(o, cx.ToPropertyKey(arg(args, 1))))
	})
	method(cx, r, "get", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "get")
		o := args[0].Object()
		k := cx.ToPropertyKey(arg(args, 1))
		receiver := args[0]
		if len(args) > 2 {
			receiver = args[2]
		}
		return cx.GetWithReceiver(o, k, receiver)
	})
	method(cx, r, "getOwnPropertyDescriptor", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "getOwnPropertyDescriptor")
		o := args[0].Object()
		d, ok := cx.GetOwnProperty(o, cx.ToPropertyKey(arg(args, 1)))
		if !ok {
			return engine.Undefined
		}
		return cx.FromPropertyDescriptor(d)
	})
	method(cx, r, "getPrototypeOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "getPrototypeOf")
		if p := cx.ProtoOf(args[0].Object()); p != nil {
			return engine.ObjectValue(p)
		}
		return engine.Null
	})
	method(cx, r, "has", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "has")
		o := args[0].Object()
		return engine.Bool(cx.QueryProperty(o, cx.QueryKey(arg(args, 1))))
	})
	method(cx, r, "isExtensible", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "isExtensible")
		return engine.Bool(cx.IsExtensible(args[0].Object()))
	})
	method(cx, r, "ownKeys", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "ownKeys")
		keys := cx.OwnKeys(args[0].Object())
		out := make([]engine.Value, len(keys))
		var pacer engine.Pacer
		for i, k := range keys {
			pacer.Step(cx)
			out[i] = cx.KeyValue(k)
		}
		return engine.ObjectValue(cx.NewArray(out))
	})
	method(cx, r, "preventExtensions", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "preventExtensions")
		return engine.Bool(cx.PreventExtensions(args[0].Object()))
	})
	method(cx, r, "set", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		requireReflectTarget(cx, arg(args, 0), "set")
		o := args[0].Object()
		k := cx.ToPropertyKey(arg(args, 1))
		receiver := args[0]
		if len(args) > 3 {
			receiver = args[3]
		}
		return engine.Bool(cx.SetWithReceiver(o, k, arg(args, 2), receiver))
	})
	method(cx, r, "setPrototypeOf", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		return engine.Bool(cx.SetPrototypeOf(o, po))
	})
}
