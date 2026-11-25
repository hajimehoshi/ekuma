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
	proto := cx.NewObject(cx.Intrinsic(storage.IntrinsicObjectPrototype))
	c := cx.NewNativeConstructor("AbstractModuleSource", 0, nativeAbstractModuleSourceConstructor)
	cx.Define(c, cx.Key(storage.KeyPrototype), storage.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(storage.KeyConstructor), storage.ObjectValue(c), storage.FlagsHidden)
	getter(cx, proto, cx.WellKnownSymbol(storage.SymbolToStringTag), "[Symbol.toStringTag]", nativeAbstractModuleSourceToStringTag)
	cx.SetIntrinsic(storage.IntrinsicAbstractModuleSource, c)
	cx.SetIntrinsic(storage.IntrinsicAbstractModuleSourcePrototype, proto)
}

func abstractModuleSourceConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cx.ThrowTypeError("AbstractModuleSource cannot be constructed")
	return storage.Undefined
}

func abstractModuleSourceToStringTag(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	name, ok := cx.ModuleSourceClassName(this)
	if !ok {
		return storage.Undefined
	}
	return cx.StringFromGo(name)
}
