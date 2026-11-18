// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initShadowRealm(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	vm.SetIntrinsic(engine.IntrinsicShadowRealmPrototype, proto)
	constructor(vm, "ShadowRealm", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor ShadowRealm requires 'new'")
		}
		p := vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicShadowRealmPrototype))
		r := vm.CreateRealm(func() {
			Install(vm)
		})
		return engine.ObjectValue(vm.NewShadowRealmObject(p, r))
	})
	method(vm, proto, "evaluate", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := vm.ValidateShadowRealmObject(this)
		src := arg(args, 0)
		if !src.IsString() {
			vm.ThrowTypeError("ShadowRealm.prototype.evaluate requires a string")
		}
		return performShadowRealmEval(vm, src.Str(), r)
	})
	method(vm, proto, "importValue", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		r := vm.ValidateShadowRealmObject(this)
		specifier := vm.ToString(arg(args, 0))
		exportName := arg(args, 1)
		if !exportName.IsString() {
			vm.ThrowTypeError("the export name of ShadowRealm.prototype.importValue must be a string")
		}
		return shadowRealmImportValue(vm, specifier, exportName, r)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("ShadowRealm"), engine.FlagConfigurable)
}

// performShadowRealmEval implements PerformShadowRealmEval with the current realm as the
// caller realm.
func performShadowRealmEval(vm *engine.VM, src string, evalRealm *engine.Realm) engine.Value {
	v, ok := vm.EvalInRealm(evalRealm, src)
	if !ok {
		vm.ThrowTypeError("the evaluation in the ShadowRealm threw")
	}
	return getWrappedValue(vm, v)
}

// shadowRealmImportValue implements ShadowRealmImportValue with the current realm as the
// caller realm.
func shadowRealmImportValue(vm *engine.VM, specifier string, exportName engine.Value, evalRealm *engine.Realm) engine.Value {
	inner := vm.ImportInRealm(evalRealm, specifier)
	onFulfilled := vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		exports := arg(args, 0).Object()
		k := vm.ToPropertyKey(exportName)
		if !vm.HasOwnProperty(exports, k) {
			vm.ThrowTypeError("the module '" + specifier + "' has no export named '" + exportName.Str() + "'")
		}
		return getWrappedValue(vm, vm.Get(exports, k))
	})
	onRejected := vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("cannot import '" + specifier + "' in the ShadowRealm")
		return engine.Undefined
	})
	c := vm.NewPromiseCapability(engine.ObjectValue(vm.Intrinsic(engine.IntrinsicPromise)))
	return vm.PerformPromiseThen(engine.ObjectValue(inner), engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected), c)
}

// getWrappedValue implements GetWrappedValue with the current realm as the caller realm.
func getWrappedValue(vm *engine.VM, v engine.Value) engine.Value {
	if !v.IsObject() {
		return v
	}
	if !v.IsCallable() {
		vm.ThrowTypeError("only primitives and callable objects can cross the boundary of a ShadowRealm")
	}
	return engine.ObjectValue(wrappedFunctionCreate(vm, v.Object()))
}

// wrappedFunctionCreate implements WrappedFunctionCreate with the current realm as the caller
// realm. A wrapped function is a native function of that realm.
func wrappedFunctionCreate(vm *engine.VM, target *engine.Object) *engine.Object {
	f := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var result engine.Value
		if _, ok := vm.Completion(func() {
			result = ordinaryWrappedFunctionCall(vm, target, this, args)
		}); !ok {
			vm.ThrowTypeError("the wrapped function threw")
		}
		return result
	})
	if _, ok := vm.Completion(func() {
		copyNameAndLength(vm, f, target, "", 0)
	}); !ok {
		vm.ThrowTypeError("cannot wrap the function for the ShadowRealm")
	}
	return f
}

// ordinaryWrappedFunctionCall implements OrdinaryWrappedFunctionCall for the wrapped function
// of target, whose realm is the current one.
func ordinaryWrappedFunctionCall(vm *engine.VM, target *engine.Object, this engine.Value, args []engine.Value) engine.Value {
	targetRealm := vm.GetFunctionRealm(target)
	wrappedArgs := make([]engine.Value, len(args))
	var wrappedThis engine.Value
	vm.InRealm(targetRealm, func() {
		for i, a := range args {
			wrappedArgs[i] = getWrappedValue(vm, a)
		}
		wrappedThis = getWrappedValue(vm, this)
	})
	return getWrappedValue(vm, vm.Call(target, wrappedThis, wrappedArgs...))
}
