// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// disposableStack holds the internal slots of a DisposableStack or AsyncDisposableStack object.
type disposableStack struct {
	disposed bool
	dc       *engine.DisposeCapability
}

func initDisposableStacks(vm *engine.VM) {
	initDisposableStack(vm, false)
	initDisposableStack(vm, true)

	symMethod(vm, vm.Intrinsic(engine.IntrinsicAsyncIteratorPrototype), vm.WellKnownSymbol(engine.SymbolAsyncDispose), "[Symbol.asyncDispose]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pc := vm.NewPromiseCapability(engine.ObjectValue(vm.Intrinsic(engine.IntrinsicPromise)))
		vm.IfAbruptRejectPromise(pc, func() {
			m := vm.GetMethod(this, vm.Intern("return"))
			if m == nil {
				vm.ResolveCapability(pc, engine.Undefined)
				return
			}
			r := vm.Call(m, this)
			vm.Await(r, func(vm *engine.VM, v engine.Value, rejected bool) {
				if rejected {
					vm.RejectCapability(pc, v)
					return
				}
				vm.ResolveCapability(pc, engine.Undefined)
			})
		})
		return pc.Promise()
	})
}

// initDisposableStack creates DisposableStack, or AsyncDisposableStack when async is set.
func initDisposableStack(vm *engine.VM, async bool) {
	name := "DisposableStack"
	hint := engine.SyncDispose
	if async {
		name = "AsyncDisposableStack"
		hint = engine.AsyncDispose
	}
	proto := vm.NewOrdinaryObject()
	var ctor *engine.Object
	newStack := func(nt *engine.Object, dc *engine.DisposeCapability) engine.Value {
		s := &disposableStack{
			dc: dc,
		}
		return engine.ObjectValue(vm.NewDisposableStackObject(vm.GetPrototypeFromConstructor(nt, proto), async, s))
	}
	ctor = constructor(vm, name, 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor " + name + " requires 'new'")
		}
		return newStack(nt, engine.NewDisposeCapability())
	})

	// thisStack returns the state of this, or throws a TypeError naming the method.
	thisStack := func(vm *engine.VM, this engine.Value, method string) *disposableStack {
		s, _ := engine.DisposableStackState(this, async).(*disposableStack)
		if s == nil {
			vm.ThrowTypeError(name + ".prototype." + method + " called on incompatible receiver " + vm.DisplayString(this))
		}
		return s
	}
	// pendingStack is thisStack for a method that requires the stack not to be disposed.
	pendingStack := func(vm *engine.VM, this engine.Value, method string) *disposableStack {
		s := thisStack(vm, this, method)
		if s.disposed {
			vm.ThrowReferenceError(name + " is already disposed")
		}
		return s
	}

	method(vm, proto, "adopt", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(vm, this, "adopt")
		v, onDispose := arg(args, 0), arg(args, 1)
		if !onDispose.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(onDispose) + " is not a function")
		}
		f := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return vm.Call(onDispose.Object(), engine.Undefined, v)
		})
		vm.AddDisposableResource(s.dc, engine.Undefined, hint, f)
		return v
	})
	method(vm, proto, "defer", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(vm, this, "defer")
		onDispose := arg(args, 0)
		if !onDispose.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(onDispose) + " is not a function")
		}
		vm.AddDisposableResource(s.dc, engine.Undefined, hint, onDispose.Object())
		return engine.Undefined
	})
	var dispose *engine.Object
	if async {
		dispose = method(vm, proto, "disposeAsync", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			pc := vm.NewPromiseCapability(engine.ObjectValue(vm.Intrinsic(engine.IntrinsicPromise)))
			s, _ := engine.DisposableStackState(this, async).(*disposableStack)
			switch {
			case s == nil:
				vm.RejectCapability(pc, vm.NewError(engine.ErrorKindType, "AsyncDisposableStack.prototype.disposeAsync called on incompatible receiver "+vm.DisplayString(this)))
			case s.disposed:
				vm.ResolveCapability(pc, engine.Undefined)
			default:
				s.disposed = true
				vm.DisposeResources(s.dc, engine.Undefined, false, func(vm *engine.VM, v engine.Value, thrown bool) {
					if thrown {
						vm.RejectCapability(pc, v)
						return
					}
					vm.ResolveCapability(pc, v)
				})
			}
			return pc.Promise()
		})
		vm.Define(proto, vm.WellKnownSymbol(engine.SymbolAsyncDispose), engine.ObjectValue(dispose), engine.FlagsHidden)
	} else {
		dispose = method(vm, proto, "dispose", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisStack(vm, this, "dispose")
			if s.disposed {
				return engine.Undefined
			}
			s.disposed = true
			var result engine.Value
			var failed bool
			vm.DisposeResources(s.dc, engine.Undefined, false, func(vm *engine.VM, v engine.Value, thrown bool) {
				result, failed = v, thrown
			})
			if failed {
				vm.Throw(result)
			}
			return result
		})
		vm.Define(proto, vm.WellKnownSymbol(engine.SymbolDispose), engine.ObjectValue(dispose), engine.FlagsHidden)
	}
	getter(vm, proto, vm.Intern("disposed"), "disposed", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisStack(vm, this, "disposed").disposed)
	})
	method(vm, proto, "move", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(vm, this, "move")
		moved := newStack(ctor, s.dc)
		s.dc = engine.NewDisposeCapability()
		s.disposed = true
		return moved
	})
	method(vm, proto, "use", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(vm, this, "use")
		v := arg(args, 0)
		vm.AddDisposableResource(s.dc, v, hint, nil)
		return v
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString(name), engine.FlagConfigurable)
}
