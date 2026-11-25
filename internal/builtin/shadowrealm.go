// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func initShadowRealm(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	cx.SetIntrinsic(engine.IntrinsicShadowRealmPrototype, proto)
	constructor(cx, "ShadowRealm", 0, engine.IntrinsicShadowRealmPrototype, shadowRealmConstructor)
	method(cx, proto, "evaluate", 1, shadowRealmEvaluate)
	method(cx, proto, "importValue", 2, shadowRealmImportValueMethod)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("ShadowRealm"), engine.FlagConfigurable)
}

func shadowRealmConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor ShadowRealm requires 'new'")
	}
	p := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicShadowRealmPrototype)
	r := cx.CreateRealm(func(cx *engine.ExecutionContext) {
		Install(cx)
	})
	return engine.ObjectValue(cx.NewShadowRealmObject(p, r))
}

func shadowRealmEvaluate(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := cx.ValidateShadowRealmObject(this)
	src := arg(args, 0)
	if !src.IsString() {
		cx.ThrowTypeError("ShadowRealm.prototype.evaluate requires a string")
	}
	return performShadowRealmEval(cx, src.Str(), r)
}

func shadowRealmImportValueMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := cx.ValidateShadowRealmObject(this)
	specifier := cx.ToString(arg(args, 0))
	exportName := arg(args, 1)
	if !exportName.IsString() {
		cx.ThrowTypeError("the export name of ShadowRealm.prototype.importValue must be a string")
	}
	return shadowRealmImportValue(cx, specifier, exportName, r)
}

// performShadowRealmEval implements PerformShadowRealmEval with the current realm as the
// caller realm.
func performShadowRealmEval(cx *engine.ExecutionContext, src wtf8.String, target *engine.ExecutionContext) engine.Value {
	v, ok := cx.EvalInRealm(target, src)
	if !ok {
		cx.ThrowTypeError("the evaluation in the ShadowRealm threw")
	}
	return getWrappedValue(cx, v)
}

// shadowRealmImportValue implements ShadowRealmImportValue with the current realm as the
// caller realm.
func shadowRealmImportValue(cx *engine.ExecutionContext, specifier wtf8.String, exportName engine.Value, target *engine.ExecutionContext) engine.Value {
	inner := target.ImportInRealm(specifier)
	st := &shadowRealmImport{
		specifier:  specifier,
		exportName: exportName,
	}
	onFulfilled := cx.NewNativeWithState("", 1, shadowRealmExportGetter, st)
	onRejected := cx.NewNativeWithState("", 1, shadowRealmImportRejected, st)
	c := cx.NewPromiseCapability(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
	return cx.PerformPromiseThen(engine.ObjectValue(inner), engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected), c)
}

// shadowRealmImport is the state of the functions that handle the result of an import in a
// ShadowRealm.
type shadowRealmImport struct {
	specifier  wtf8.String
	exportName engine.Value
}

// shadowRealmExportGetter is the code of the ExportGetter functions. Their state is a
// *shadowRealmImport.
func shadowRealmExportGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	st := cx.ActiveNativeState().(*shadowRealmImport)
	exports := arg(args, 0).Object()
	k := cx.ToPropertyKey(st.exportName)
	if !cx.HasOwnProperty(exports, k) {
		cx.ThrowTypeError("the module '" + wtf8.ToValidUTF8(st.specifier) + "' has no export named '" + wtf8.ToValidUTF8(st.exportName.Str()) + "'")
	}
	return getWrappedValue(cx, cx.Get(exports, k))
}

// shadowRealmImportRejected is the code of the functions that throw for a failed import. Their
// state is a *shadowRealmImport.
func shadowRealmImportRejected(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	st := cx.ActiveNativeState().(*shadowRealmImport)
	cx.ThrowTypeError("cannot import '" + wtf8.ToValidUTF8(st.specifier) + "' in the ShadowRealm")
	return engine.Undefined
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
	f := cx.NewNativeWithState("", 0, wrappedFunction, target)
	if _, ok := cx.Completion(func() {
		copyNameAndLength(cx, f, target, "", 0)
	}); !ok {
		cx.ThrowTypeError("cannot wrap the function for the ShadowRealm")
	}
	return f
}

// wrappedFunction is the code of the wrapped functions. Their state is the target, an
// *engine.Object.
func wrappedFunction(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	target := cx.ActiveNativeState().(*engine.Object)
	var result engine.Value
	if _, ok := cx.Completion(func() {
		result = ordinaryWrappedFunctionCall(cx, target, this, args)
	}); !ok {
		cx.ThrowTypeError("the wrapped function threw")
	}
	return result
}

// ordinaryWrappedFunctionCall implements OrdinaryWrappedFunctionCall for the wrapped function
// of target, whose realm is the current one.
func ordinaryWrappedFunctionCall(cx *engine.ExecutionContext, target *engine.Object, this engine.Value, args []engine.Value) engine.Value {
	targetContext := cx.FunctionContext(target)
	wrappedArgs := make([]engine.Value, len(args))
	var wrappedThis engine.Value
	targetContext.Run(func() {
		for i, a := range args {
			wrappedArgs[i] = getWrappedValue(targetContext, a)
		}
		wrappedThis = getWrappedValue(targetContext, this)
	})
	return getWrappedValue(cx, cx.Call(target, wrappedThis, wrappedArgs...))
}
