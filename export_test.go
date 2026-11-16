// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// DetachArrayBuffer detaches the ArrayBuffer v. The error is a TypeError for a value
// that is not an ArrayBuffer or for an immutable ArrayBuffer.
func DetachArrayBuffer(vm *VM, v Value) error {
	vm.initIfNeeded()
	e := v.engine()
	b := engine.ArrayBufferData(e)
	if b == nil || b.IsShared() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "not an ArrayBuffer"))}
	}
	if b.IsImmutable() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "cannot detach an immutable ArrayBuffer"))}
	}
	vm.e.DetachArrayBuffer(e.Object())
	return nil
}

// NewHTMLDDA returns an object of r with the [[IsHTMLDDA]] internal slot, which returns null when called.
func NewHTMLDDA(r *Realm) Value {
	var v engine.Value
	r.in(func() {
		v = r.vm.e.NewHTMLDDA(func(*engine.VM, engine.Value, []engine.Value) engine.Value {
			return engine.Null
		})
	})
	return fromEngine(v)
}

// AbstractModuleSource returns %AbstractModuleSource% of r.
func AbstractModuleSource(r *Realm) Value {
	var v engine.Value
	r.in(func() {
		v = engine.ObjectValue(r.vm.e.Intrinsic(engine.IntrinsicAbstractModuleSource))
	})
	return fromEngine(v)
}

// NewSourceModule returns a module of vm whose only export is a default of undefined and
// whose module source is an instance of %AbstractModuleSource% named className.
func NewSourceModule(vm *VM, name, className string) *Module {
	m := vm.NewSyntheticModule(name, Undefined)
	proto := vm.e.NewObject(vm.e.Intrinsic(engine.IntrinsicAbstractModuleSourcePrototype))
	m.m.SetModuleSource(vm.e.NewModuleSourceObject(proto, className))
	return m
}

func ArgumentStorage(vm *VM) (argsLen, callArgsLen int, cleared bool) {
	for _, v := range vm.args[len(vm.args):cap(vm.args)] {
		if v.o != nil || v.n != 0 {
			return len(vm.args), len(vm.callArgs), false
		}
	}
	for _, v := range vm.callArgs[len(vm.callArgs):cap(vm.callArgs)] {
		if v != (engine.Value{}) {
			return len(vm.args), len(vm.callArgs), false
		}
	}
	return len(vm.args), len(vm.callArgs), true
}
