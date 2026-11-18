// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the operations for Go code outside the engine. Unlike the rest of the engine,
// they report a JavaScript exception as an error.

// HostDefined returns the value that [NewVM] was given.
func (vm *VM) HostDefined() any {
	return vm.hostDefined
}

// GlobalObject returns the global object.
func (vm *VM) GlobalObject() Value {
	return Value{o: vm.global}
}

// NewFunction returns a function implemented by f. name is UTF-8.
func (vm *VM) NewFunction(name string, f NativeFunc) Value {
	return Value{o: vm.NewNative(wtf8.FromValidUTF8(name), 0, f)}
}

// NewHTMLDDA returns a function implemented by f that has the [[IsHTMLDDA]] internal slot.
func (vm *VM) NewHTMLDDA(f NativeFunc) Value {
	o := vm.NewNative("", 0, f)
	o.kind = kindHTMLDDA
	return Value{o: o}
}

// NewPlainObject returns a new empty object.
func (vm *VM) NewPlainObject() Value {
	return Value{o: vm.NewOrdinaryObject()}
}

// NewArrayOf returns a new array with the given elements, which it keeps.
func (vm *VM) NewArrayOf(elems []Value) Value {
	return Value{o: vm.NewArray(elems)}
}

// GoString returns the content of the string v as UTF-8, with lone surrogates replaced by U+FFFD.
func GoString(v Value) string {
	return wtf8.ToValidUTF8(v.Str())
}

// HostGet returns v[key].
func (vm *VM) HostGet(v, key Value) (Value, error) {
	return vm.enter(func() Value {
		val, getter := vm.getValue(v, key)
		if getter != nil {
			return vm.Call(getter, v)
		}
		return val
	})
}

// HostSet stores v[key] = val the way strict mode code does.
func (vm *VM) HostSet(v, key, val Value) error {
	_, err := vm.enter(func() Value {
		setter, ok := vm.setValue(v, key, val)
		switch {
		case setter != nil:
			vm.Call(setter, v, val)
		case !ok:
			vm.throwReadOnly(v, vm.ToPropertyKey(key))
		}
		return Undefined
	})
	return err
}

// HostDelete deletes v[key] the way strict mode code does.
func (vm *VM) HostDelete(v, key Value) error {
	_, err := vm.enter(func() Value {
		return vm.deleteValue(v, key, true)
	})
	return err
}

// HostCall calls f with the given this value and arguments.
func (vm *VM) HostCall(f, this Value, args []Value) (Value, error) {
	return vm.enter(func() Value {
		return vm.CallValue(f, this, args...)
	})
}

// HostCallMethod calls the method v[key] with v as this.
func (vm *VM) HostCallMethod(v, key Value, args []Value) (Value, error) {
	return vm.enter(func() Value {
		f, getter := vm.getValue(v, key)
		if getter != nil {
			f = vm.Call(getter, v)
		}
		if !f.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(key) + " is not a function")
		}
		return vm.CallValue(f, v, args...)
	})
}

// HostConstruct implements new f(...args).
func (vm *VM) HostConstruct(f Value, args []Value) (Value, error) {
	return vm.enter(func() Value {
		if !f.IsCallable() || !vm.IsConstructor(f.o) {
			vm.ThrowTypeError(vm.DisplayString(f) + " is not a constructor")
		}
		return Value{o: vm.Construct(f.o, args, f.o)}
	})
}

// HostInstanceOf implements v instanceof t.
func (vm *VM) HostInstanceOf(v, t Value) (bool, error) {
	r, err := vm.enter(func() Value {
		return Bool(vm.instanceOf(v, t))
	})
	return ToBoolean(r), err
}

// ThrownMessage describes the thrown value v for a Go error, without running any JavaScript.
func ThrownMessage(v Value) string {
	var vm *VM
	if v.o != nil {
		vm = v.o.VM()
	}
	if vm == nil {
		// A primitive other than a string: its description needs no VM.
		return "uncaught " + wtf8.ToValidUTF8(vm.DisplayString(v))
	}
	return vm.errorOf(v).Error()
}

// ContinuationData returns the continuation data, the value of the host that promise
// reactions, awaits and FinalizationRegistry objects capture when they are made, and that
// their jobs run with.
func (vm *VM) ContinuationData() any {
	return vm.continuationData
}

// SetContinuationData sets data as the continuation data.
func (vm *VM) SetContinuationData(data any) {
	vm.continuationData = data
}
