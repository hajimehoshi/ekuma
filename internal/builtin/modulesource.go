// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// initAbstractModuleSource creates %AbstractModuleSource%, which is not a global.
func initAbstractModuleSource(cx *engine.ExecutionContext) {
	proto := cx.NewObject(cx.Realm().Intrinsic(engine.IntrinsicObjectPrototype))
	c := cx.NewNativeConstructor("AbstractModuleSource", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.ThrowTypeError("AbstractModuleSource cannot be constructed")
		return engine.Undefined
	})
	cx.Define(c, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	getter(cx, proto, cx.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		name, ok := cx.ModuleSourceClassName(this)
		if !ok {
			return engine.Undefined
		}
		return cx.StringFromGo(name)
	})
	cx.SetIntrinsic(engine.IntrinsicAbstractModuleSource, c)
	cx.SetIntrinsic(engine.IntrinsicAbstractModuleSourcePrototype, proto)
}
