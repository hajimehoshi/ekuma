// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initReflect(vm *engine.VM) {
	r := vm.NewOrdinaryObject()
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Reflect"), engine.ObjectValue(r), engine.FlagsHidden)
	vm.Define(r, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Reflect"), engine.FlagConfigurable)

	target := func(vm *engine.VM, v engine.Value, name string) *engine.Object {
		if !v.IsObject() {
			vm.ThrowTypeError("Reflect." + name + " called on non-object")
		}
		return v.Object()
	}
	// argumentList implements CreateListFromArrayLike, which requires an object.
	argumentList := func(vm *engine.VM, v engine.Value, name string) []engine.Value {
		if !v.IsObject() {
			vm.ThrowTypeError("Reflect." + name + " requires an array-like object as the argument list")
		}
		return listFromArrayLike(vm, v)
	}

	method(vm, r, "apply", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		f := arg(args, 0)
		if !f.IsCallable() {
			vm.ThrowTypeError("Reflect.apply requires a function as the target")
		}
		return vm.Call(f.Object(), arg(args, 1), argumentList(vm, arg(args, 2), "apply")...)
	})
	method(vm, r, "construct", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		f := arg(args, 0)
		if !f.IsObject() || !vm.IsConstructor(f.Object()) {
			vm.ThrowTypeError("Reflect.construct requires a constructor as the target")
		}
		nt := f
		if len(args) > 2 {
			nt = args[2]
			if !nt.IsObject() || !vm.IsConstructor(nt.Object()) {
				vm.ThrowTypeError("Reflect.construct requires a constructor as the new target")
			}
		}
		list := argumentList(vm, arg(args, 1), "construct")
		return engine.ObjectValue(vm.Construct(f.Object(), list, nt.Object()))
	})
	method(vm, r, "defineProperty", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "defineProperty")
		k := vm.ToPropertyKey(arg(args, 1))
		return engine.Bool(vm.DefineOwnProperty(o, k, toDescriptor(vm, arg(args, 2))))
	})
	method(vm, r, "deleteProperty", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "deleteProperty")
		return engine.Bool(vm.DeleteProp(o, vm.ToPropertyKey(arg(args, 1))))
	})
	method(vm, r, "get", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "get")
		k := vm.ToPropertyKey(arg(args, 1))
		receiver := args[0]
		if len(args) > 2 {
			receiver = args[2]
		}
		return vm.GetWithReceiver(o, k, receiver)
	})
	method(vm, r, "getOwnPropertyDescriptor", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "getOwnPropertyDescriptor")
		d, ok := vm.GetOwnProperty(o, vm.ToPropertyKey(arg(args, 1)))
		if !ok {
			return engine.Undefined
		}
		return fromDescriptor(vm, d)
	})
	method(vm, r, "getPrototypeOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if p := vm.ProtoOf(target(vm, arg(args, 0), "getPrototypeOf")); p != nil {
			return engine.ObjectValue(p)
		}
		return engine.Null
	})
	method(vm, r, "has", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "has")
		return engine.Bool(vm.HasProperty(o, vm.ToPropertyKey(arg(args, 1))))
	})
	method(vm, r, "isExtensible", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(vm.IsExtensible(target(vm, arg(args, 0), "isExtensible")))
	})
	method(vm, r, "ownKeys", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var out []engine.Value
		for _, k := range vm.OwnKeys(target(vm, arg(args, 0), "ownKeys")) {
			if !k.IsPrivateName() {
				out = append(out, vm.KeyValue(k))
			}
		}
		return engine.ObjectValue(vm.NewArray(out))
	})
	method(vm, r, "preventExtensions", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(vm.PreventExtensions(target(vm, arg(args, 0), "preventExtensions")))
	})
	method(vm, r, "set", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "set")
		k := vm.ToPropertyKey(arg(args, 1))
		receiver := args[0]
		if len(args) > 3 {
			receiver = args[3]
		}
		return engine.Bool(vm.SetWithReceiver(o, k, arg(args, 2), receiver))
	})
	method(vm, r, "setPrototypeOf", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := target(vm, arg(args, 0), "setPrototypeOf")
		p := arg(args, 1)
		if !p.IsObject() && !p.IsNull() {
			vm.ThrowTypeError("object prototype may only be an Object or null: " + vm.DisplayString(p))
		}
		var po *engine.Object
		if p.IsObject() {
			po = p.Object()
		}
		return engine.Bool(vm.SetPrototypeOf(o, po))
	})
}
