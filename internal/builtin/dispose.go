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

	symMethod(cx, cx.Realm().Intrinsic(engine.IntrinsicAsyncIteratorPrototype), cx.WellKnownSymbol(engine.SymbolAsyncDispose), "[Symbol.asyncDispose]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		pc := cx.NewPromiseCapability(engine.ObjectValue(cx.Realm().Intrinsic(engine.IntrinsicPromise)))
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

// initDisposableStack creates DisposableStack, or AsyncDisposableStack when async is set.
func initDisposableStack(cx *engine.ExecutionContext, async bool) {
	name := "DisposableStack"
	hint := engine.SyncDispose
	protoID := engine.IntrinsicDisposableStackPrototype
	if async {
		name = "AsyncDisposableStack"
		hint = engine.AsyncDispose
		protoID = engine.IntrinsicAsyncDisposableStackPrototype
	}
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(protoID, proto)
	var ctor *engine.Object
	newStack := func(cx *engine.ExecutionContext, nt *engine.Object, dc *engine.DisposeCapability) engine.Value {
		s := &disposableStack{
			dc: dc,
		}
		return engine.ObjectValue(cx.NewDisposableStackObject(cx.GetPrototypeFromConstructor(nt, protoID), async, s))
	}
	ctor = constructor(cx, wtf8.String(name), 0, protoID, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor " + name + " requires 'new'")
		}
		return newStack(cx, nt, engine.NewDisposeCapability())
	})

	// thisStack returns the state of this, or throws a TypeError naming the method.
	thisStack := func(cx *engine.ExecutionContext, this engine.Value, method string) *disposableStack {
		s, _ := engine.DisposableStackState(this, async).(*disposableStack)
		if s == nil {
			cx.ThrowTypeError(name + ".prototype." + method + " called on incompatible receiver " + cx.DisplayString(this))
		}
		return s
	}
	// pendingStack is thisStack for a method that requires the stack not to be disposed.
	pendingStack := func(cx *engine.ExecutionContext, this engine.Value, method string) *disposableStack {
		s := thisStack(cx, this, method)
		if s.disposed {
			cx.ThrowReferenceError(name + " is already disposed")
		}
		return s
	}

	method(cx, proto, "adopt", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(cx, this, "adopt")
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
		s := pendingStack(cx, this, "defer")
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
			pc := cx.NewPromiseCapability(engine.ObjectValue(cx.Realm().Intrinsic(engine.IntrinsicPromise)))
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
			s := thisStack(cx, this, "dispose")
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
		return engine.Bool(thisStack(cx, this, "disposed").disposed)
	})
	method(cx, proto, "move", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(cx, this, "move")
		moved := newStack(cx, ctor, s.dc)
		s.dc = engine.NewDisposeCapability()
		s.disposed = true
		return moved
	})
	method(cx, proto, "use", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := pendingStack(cx, this, "use")
		v := arg(args, 0)
		cx.AddDisposableResource(s.dc, v, hint, nil)
		return v
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString(wtf8.String(name)), engine.FlagConfigurable)
}
