// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// disposableStack holds the internal slots of a DisposableStack or AsyncDisposableStack object.
type disposableStack struct {
	disposed bool
	dc       *engine.DisposeCapability
}

func initDisposableStacks(cx *engine.ExecutionContext) {
	initDisposableStack(cx, false)
	initDisposableStack(cx, true)

	symMethod(cx, cx.Intrinsic(engine.IntrinsicAsyncIteratorPrototype), cx.WellKnownSymbol(engine.SymbolAsyncDispose), "[Symbol.asyncDispose]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		pc := cx.NewPromiseCapability(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
		cx.IfAbruptRejectPromise(pc, func() {
			m := cx.GetMethod(this, cx.Intern("return"))
			if m == nil {
				cx.ResolveCapability(pc, engine.Undefined)
				return
			}
			r := cx.Call(m, this)
			cx.Await(r, func(cx *engine.ExecutionContext, v engine.Value, rejected bool) {
				if rejected {
					cx.RejectCapability(pc, v)
					return
				}
				cx.ResolveCapability(pc, engine.Undefined)
			})
		})
		return pc.Promise()
	})
}

// disposableStackName returns the name of the constructor of the stacks with hint.
func disposableStackName(hint engine.DisposeHint) string {
	if hint == engine.AsyncDispose {
		return "AsyncDisposableStack"
	}
	return "DisposableStack"
}

// disposableStackPrototype returns the default prototype of the stacks with hint.
func disposableStackPrototype(hint engine.DisposeHint) engine.Intrinsic {
	if hint == engine.AsyncDispose {
		return engine.IntrinsicAsyncDisposableStackPrototype
	}
	return engine.IntrinsicDisposableStackPrototype
}

// newDisposableStack returns a new stack with hint for the new target nt.
func newDisposableStack(cx *engine.ExecutionContext, hint engine.DisposeHint, nt *engine.Object, dc *engine.DisposeCapability) engine.Value {
	s := &disposableStack{
		dc: dc,
	}
	proto := cx.GetPrototypeFromConstructor(nt, disposableStackPrototype(hint))
	return engine.ObjectValue(cx.NewDisposableStackObject(proto, hint == engine.AsyncDispose, s))
}

// thisDisposableStack returns the state of the stack this with hint, or throws a TypeError naming
// the method.
func thisDisposableStack(cx *engine.ExecutionContext, hint engine.DisposeHint, this engine.Value, method string) *disposableStack {
	s, _ := engine.DisposableStackState(this, hint == engine.AsyncDispose).(*disposableStack)
	if s == nil {
		cx.ThrowTypeError(disposableStackName(hint) + ".prototype." + method + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return s
}

// thisPendingDisposableStack is thisDisposableStack for a method that requires the stack not to be
// disposed.
func thisPendingDisposableStack(cx *engine.ExecutionContext, hint engine.DisposeHint, this engine.Value, method string) *disposableStack {
	s := thisDisposableStack(cx, hint, this, method)
	if s.disposed {
		cx.ThrowReferenceError(disposableStackName(hint) + " is already disposed")
	}
	return s
}

// initDisposableStack creates DisposableStack, or AsyncDisposableStack when async is set.
func initDisposableStack(cx *engine.ExecutionContext, async bool) {
	hint := engine.SyncDispose
	if async {
		hint = engine.AsyncDispose
	}
	name := disposableStackName(hint)
	protoID := disposableStackPrototype(hint)
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(protoID, proto)
	var ctor *engine.Object
	ctor = constructor(cx, wtf8.String(name), 0, protoID, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor " + name + " requires 'new'")
		}
		return newDisposableStack(cx, hint, nt, engine.NewDisposeCapability())
	})

	method(cx, proto, "adopt", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "adopt")
		v, onDispose := arg(args, 0), arg(args, 1)
		if !onDispose.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(onDispose) + " is not a function")
		}
		f := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return cx.Call(onDispose.Object(), engine.Undefined, v)
		})
		cx.AddDisposableResource(s.dc, engine.Undefined, hint, f)
		return v
	})
	method(cx, proto, "defer", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "defer")
		onDispose := arg(args, 0)
		if !onDispose.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(onDispose) + " is not a function")
		}
		cx.AddDisposableResource(s.dc, engine.Undefined, hint, onDispose.Object())
		return engine.Undefined
	})
	var dispose *engine.Object
	if async {
		dispose = method(cx, proto, "disposeAsync", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			pc := cx.NewPromiseCapability(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
			s, _ := engine.DisposableStackState(this, async).(*disposableStack)
			switch {
			case s == nil:
				cx.RejectCapability(pc, cx.NewError(engine.ErrorKindType, "AsyncDisposableStack.prototype.disposeAsync called on incompatible receiver "+cx.DisplayString(this)))
			case s.disposed:
				cx.ResolveCapability(pc, engine.Undefined)
			default:
				s.disposed = true
				cx.DisposeResources(s.dc, engine.Undefined, false, func(cx *engine.ExecutionContext, v engine.Value, thrown bool) {
					if thrown {
						cx.RejectCapability(pc, v)
						return
					}
					cx.ResolveCapability(pc, v)
				})
			}
			return pc.Promise()
		})
		cx.Define(proto, cx.WellKnownSymbol(engine.SymbolAsyncDispose), engine.ObjectValue(dispose), engine.FlagsHidden)
	} else {
		dispose = method(cx, proto, "dispose", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisDisposableStack(cx, engine.SyncDispose, this, "dispose")
			if s.disposed {
				return engine.Undefined
			}
			s.disposed = true
			var result engine.Value
			var failed bool
			cx.DisposeResources(s.dc, engine.Undefined, false, func(cx *engine.ExecutionContext, v engine.Value, thrown bool) {
				result, failed = v, thrown
			})
			if failed {
				cx.Throw(result)
			}
			return result
		})
		cx.Define(proto, cx.WellKnownSymbol(engine.SymbolDispose), engine.ObjectValue(dispose), engine.FlagsHidden)
	}
	getter(cx, proto, cx.Intern("disposed"), "disposed", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisDisposableStack(cx, hint, this, "disposed").disposed)
	})
	method(cx, proto, "move", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "move")
		moved := newDisposableStack(cx, hint, ctor, s.dc)
		s.dc = engine.NewDisposeCapability()
		s.disposed = true
		return moved
	})
	method(cx, proto, "use", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "use")
		v := arg(args, 0)
		cx.AddDisposableResource(s.dc, v, hint, nil)
		return v
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString(wtf8.String(name)), engine.FlagConfigurable)
}
