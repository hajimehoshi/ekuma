// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// initAbstractModuleSource creates %AbstractModuleSource%, which is not a global.
func initAbstractModuleSource(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	c := vm.NewNativeConstructor("AbstractModuleSource", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("AbstractModuleSource cannot be constructed")
		return engine.Undefined
	})
	vm.Define(c, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	getter(vm, proto, vm.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		name, ok := vm.ModuleSourceClassName(this)
		if !ok {
			return engine.Undefined
		}
		return vm.StringFromGo(name)
	})
	vm.SetIntrinsic(engine.IntrinsicAbstractModuleSource, c)
	vm.SetIntrinsic(engine.IntrinsicAbstractModuleSourcePrototype, proto)
}
