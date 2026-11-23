// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func initShadowRealm(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Realm().Intrinsic(engine.IntrinsicObjectPrototype))
	cx.SetIntrinsic(engine.IntrinsicShadowRealmPrototype, proto)
	constructor(cx, "ShadowRealm", 0, engine.IntrinsicShadowRealmPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor ShadowRealm requires 'new'")
		}
		p := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicShadowRealmPrototype)
		r := cx.CreateRealm(func(cx *engine.ExecutionContext) {
			Install(cx)
		})
		return engine.ObjectValue(cx.NewShadowRealmObject(p, r))
	})
	method(cx, proto, "evaluate", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := cx.ValidateShadowRealmObject(this)
		src := arg(args, 0)
		if !src.IsString() {
			cx.ThrowTypeError("ShadowRealm.prototype.evaluate requires a string")
		}
		return performShadowRealmEval(cx, src.Str(), r)
	})
	method(cx, proto, "importValue", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		r := cx.ValidateShadowRealmObject(this)
		specifier := cx.ToString(arg(args, 0))
		exportName := arg(args, 1)
		if !exportName.IsString() {
			cx.ThrowTypeError("the export name of ShadowRealm.prototype.importValue must be a string")
		}
		return shadowRealmImportValue(cx, specifier, exportName, r)
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("ShadowRealm"), engine.FlagConfigurable)
}

// performShadowRealmEval implements PerformShadowRealmEval with the current realm as the
// caller realm.
func performShadowRealmEval(cx *engine.ExecutionContext, src wtf8.String, evalRealm *engine.Realm) engine.Value {
	v, ok := cx.EvalInRealm(evalRealm, src)
	if !ok {
		cx.ThrowTypeError("the evaluation in the ShadowRealm threw")
	}
	return getWrappedValue(cx, v)
}

// shadowRealmImportValue implements ShadowRealmImportValue with the current realm as the
// caller realm.
func shadowRealmImportValue(cx *engine.ExecutionContext, specifier wtf8.String, exportName engine.Value, evalRealm *engine.Realm) engine.Value {
	inner := cx.ImportInRealm(evalRealm, specifier)
	onFulfilled := cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		exports := arg(args, 0).Object()
		k := cx.ToPropertyKey(exportName)
		if !cx.HasOwnProperty(exports, k) {
			cx.ThrowTypeError("the module '" + wtf8.ToValidUTF8(specifier) + "' has no export named '" + wtf8.ToValidUTF8(exportName.Str()) + "'")
		}
		return getWrappedValue(cx, cx.Get(exports, k))
	})
	onRejected := cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.ThrowTypeError("cannot import '" + wtf8.ToValidUTF8(specifier) + "' in the ShadowRealm")
		return engine.Undefined
	})
	c := cx.NewPromiseCapability(engine.ObjectValue(cx.Realm().Intrinsic(engine.IntrinsicPromise)))
	return cx.PerformPromiseThen(engine.ObjectValue(inner), engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected), c)
}

// getWrappedValue implements GetWrappedValue with the current realm as the caller realm.
func getWrappedValue(cx *engine.ExecutionContext, v engine.Value) engine.Value {
	if !v.IsObject() {
		return v
	}
	if !v.IsCallable() {
		cx.ThrowTypeError("only primitives and callable objects can cross the boundary of a ShadowRealm")
	}
	return engine.ObjectValue(wrappedFunctionCreate(cx, v.Object()))
}

// wrappedFunctionCreate implements WrappedFunctionCreate with the current realm as the caller
// realm. A wrapped function is a native function of that realm.
func wrappedFunctionCreate(cx *engine.ExecutionContext, target *engine.Object) *engine.Object {
	f := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var result engine.Value
		if _, ok := cx.Completion(func() {
			result = ordinaryWrappedFunctionCall(cx, target, this, args)
		}); !ok {
			cx.ThrowTypeError("the wrapped function threw")
		}
		return result
	})
	if _, ok := cx.Completion(func() {
		copyNameAndLength(cx, f, target, "", 0)
	}); !ok {
		cx.ThrowTypeError("cannot wrap the function for the ShadowRealm")
	}
	return f
}

// ordinaryWrappedFunctionCall implements OrdinaryWrappedFunctionCall for the wrapped function
// of target, whose realm is the current one.
func ordinaryWrappedFunctionCall(cx *engine.ExecutionContext, target *engine.Object, this engine.Value, args []engine.Value) engine.Value {
	targetRealm := cx.GetFunctionRealm(target)
	wrappedArgs := make([]engine.Value, len(args))
	var wrappedThis engine.Value
	targetRealm.Context().Run(func() {
		cx := targetRealm.Context()
		for i, a := range args {
			wrappedArgs[i] = getWrappedValue(cx, a)
		}
		wrappedThis = getWrappedValue(cx, this)
	})
	return getWrappedValue(cx, cx.Call(target, wrappedThis, wrappedArgs...))
}
