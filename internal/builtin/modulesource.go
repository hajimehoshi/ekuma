// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

var (
	nativeAbstractModuleSourceConstructor = engine.RegisterNative(abstractModuleSourceConstructor)
	nativeAbstractModuleSourceToStringTag = engine.RegisterNative(abstractModuleSourceToStringTag)
)

// initAbstractModuleSource creates %AbstractModuleSource%, which is not a global.
func initAbstractModuleSource(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Intrinsic(engine.IntrinsicObjectPrototype))
	c := cx.NewNativeConstructor("AbstractModuleSource", 0, nativeAbstractModuleSourceConstructor)
	cx.Define(c, cx.Key(engine.KeyPrototype), storage.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), storage.ObjectValue(c), engine.FlagsHidden)
	getter(cx, proto, cx.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", nativeAbstractModuleSourceToStringTag)
	cx.SetIntrinsic(engine.IntrinsicAbstractModuleSource, c)
	cx.SetIntrinsic(engine.IntrinsicAbstractModuleSourcePrototype, proto)
}

func abstractModuleSourceConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cx.ThrowTypeError("AbstractModuleSource cannot be constructed")
	return storage.Undefined
}

func abstractModuleSourceToStringTag(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	name, ok := cx.ModuleSourceClassName(this)
	if !ok {
		return storage.Undefined
	}
	return cx.StringFromGo(name)
}
