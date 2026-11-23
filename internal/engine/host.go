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

// ExceptionLocation identifies the throw site. Line and Column are 1-based,
// Column counts UTF-16 code units, and zero indicates an unknown position.
type ExceptionLocation struct {
	File       string
	Line       int
	Column     int
	SourceLine wtf8.String
}

// SetExceptionReporter sets the current realm's handler for exceptions escaping
// an outermost host call or a queued callback. The handler receives the thrown
// value and its source location before the next microtask runs.
func (vm *VM) SetExceptionReporter(report func(Value, ExceptionLocation)) {
	vm.exceptionReporter = report
}

// ExceptionDescription describes a thrown Error or proxy without invoking JavaScript.
// It returns undefined when the host should convert the thrown value to a string.
// objectToString and errorToString are the reporting realm's original built-in methods.
func (vm *VM) ExceptionDescription(v, objectToString, errorToString Value) Value {
	if !v.IsObject() {
		return Undefined
	}
	o := v.o
	for o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.isGlobal() {
			// Shell globals are ordinary receivers for exception reporting.
			if o == v.o {
				return Undefined
			}
			break
		}
		o = st.target
		if o == nil {
			return vm.NewString("null")
		}
	}
	if o == v.o && o.class != classError {
		return Undefined
	}
	if o.fn != nil {
		src := vm.NewString(o.SourceText())
		if n := src.StringLength(); n > 128 {
			return vm.NewString(src.Substring(0, 111) + "...<omitted>..." + src.Substring(n-2, n))
		}
		return src
	}
	method, _ := vm.lookupData(o, vm.Key(KeyToString))
	if o.class == classError || IsStrictlyEqual(method, errorToString) {
		name, _ := vm.lookupData(o, vm.Key(KeyName))
		message, _ := vm.lookupData(o, vm.Key(KeyMessage))
		if !message.IsString() {
			message = vm.emptyString
		}
		if !name.IsString() || name.StringLength() == 0 {
			return message
		}
		if message.StringLength() == 0 {
			return name
		}
		return vm.ConcatStrings([]Value{name, vm.NewString(": "), message})
	}
	if IsStrictlyEqual(method, objectToString) {
		ctor, _ := vm.lookupData(o, vm.Key(KeyConstructor))
		if ctor.IsCallable() && ctor.o.class != classProxy {
			if name := diagnosticFunctionName(ctor.o.fn); name != "" {
				return vm.NewString("#<" + name + ">")
			}
		}
	}
	return vm.NewString("[object Object]")
}

func diagnosticFunctionName(fn *function) wtf8.String {
	const limit = 1000
	var prefix wtf8.Buffer
	for fn.bound != nil {
		prefix.WriteString("bound ")
		if prefix.Len() >= limit {
			return prefix.String()[:limit]
		}
		fn = fn.bound.target.fn
	}
	name := fn.nativeName()
	if fn.code != nil {
		name = fn.code.name
	}
	end := wtf8.Seek(name, wtf8.Pos{}, limit-prefix.Len())
	return prefix.String() + wtf8.Slice(name, wtf8.Pos{}, end)
}

// NewFunction returns a function implemented by f. name is UTF-8.
func (vm *VM) NewFunction(name string, f NativeFunc) Value {
	return Value{o: vm.NewNative(wtf8.FromValidUTF8(name), 0, f)}
}

// NewHTMLDDA returns a function implemented by f that has the [[IsHTMLDDA]] internal slot.
func (vm *VM) NewHTMLDDA(f NativeFunc) Value {
	o := vm.NewNative("", 0, f)
	o.kind = kindHTMLDDA
	vm.madeHTMLDDA = true
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
		if setter := vm.setValue(v, key, val, true); setter != nil {
			vm.Call(setter, v, val)
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
			vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(key)+" is not a function")
		}
		return vm.CallValue(f, v, args...)
	})
}

// HostConstruct implements new f(...args).
func (vm *VM) HostConstruct(f Value, args []Value) (Value, error) {
	return vm.enter(func() Value {
		if !f.IsCallable() || !vm.IsConstructor(f.o) {
			vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(f)+" is not a constructor")
		}
		return Value{o: vm.Construct(f.o, args, f.o)}
	})
}

// HostResolve resolves the promise of c with v, as the [[Resolve]] function of c does.
func (vm *VM) HostResolve(c *PromiseCapability, v Value) error {
	_, err := vm.enter(func() Value {
		vm.ResolveCapability(c, v)
		return Undefined
	})
	return err
}

// HostReject rejects the promise of c with v, as the [[Reject]] function of c does.
func (vm *VM) HostReject(c *PromiseCapability, v Value) error {
	_, err := vm.enter(func() Value {
		vm.RejectCapability(c, v)
		return Undefined
	})
	return err
}

// HostInstanceOf implements v instanceof t.
func (vm *VM) HostInstanceOf(v, t Value) (bool, error) {
	r, err := vm.enter(func() Value {
		return Bool(vm.instanceOf(v, t, nil))
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
		return "uncaught " + vm.DisplayString(v)
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
