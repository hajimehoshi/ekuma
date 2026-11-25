// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// disposableStack holds the internal slots of a DisposableStack or AsyncDisposableStack object.
type disposableStack struct {
	disposed bool
	dc       *engine.DisposeCapability
}

var nativeAsyncIteratorAsyncDispose = engine.RegisterNative(asyncIteratorAsyncDispose)

func initDisposableStacks(cx *engine.ExecutionContext) {
	initDisposableStack(cx)
	initAsyncDisposableStack(cx)

	symMethod(cx, cx.Intrinsic(engine.IntrinsicAsyncIteratorPrototype), cx.WellKnownSymbol(engine.SymbolAsyncDispose), "[Symbol.asyncDispose]", 0, nativeAsyncIteratorAsyncDispose)
}

func asyncIteratorAsyncDispose(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pc := cx.NewPromiseCapability(storage.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
	cx.IfAbruptRejectPromise(pc, func() {
		m := cx.GetMethod(this, cx.Intern("return"))
		if m == nil {
			cx.ResolveCapability(pc, storage.Undefined)
			return
		}
		r := cx.Call(m, this)
		cx.Await(r, func(cx *engine.ExecutionContext, v engine.Value, rejected bool) {
			if rejected {
				cx.RejectCapability(pc, v)
				return
			}
			cx.ResolveCapability(pc, storage.Undefined)
		})
	})
	return pc.Promise()
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

// newDisposableStack returns a new stack with hint and the prototype proto.
func newDisposableStack(cx *engine.ExecutionContext, hint engine.DisposeHint, proto *engine.Object, dc *engine.DisposeCapability) engine.Value {
	s := &disposableStack{
		dc: dc,
	}
	return storage.ObjectValue(cx.NewDisposableStackObject(proto, hint == engine.AsyncDispose, s))
}

// thisDisposableStack returns the state of the stack this with hint, or throws a TypeError naming
// the method.
func thisDisposableStack(cx *engine.ExecutionContext, hint engine.DisposeHint, this engine.Value, method string) *disposableStack {
	s, _ := storage.DisposableStackState(this, hint == engine.AsyncDispose).(*disposableStack)
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

// adoptedResource is the state of the dispose function of a value that a stack adopted.
type adoptedResource struct {
	value     engine.Value
	onDispose *engine.Object
}

// disposeAdopted is the code of the dispose functions of adopted values. Their state is an
// *adoptedResource.
func disposeAdopted(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := cx.ActiveNativeState().(*adoptedResource)
	return cx.Call(r.onDispose, storage.Undefined, r.value)
}

var nativeDisposableStackDispose = engine.RegisterNative(disposableStackDispose)

func initDisposableStack(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicDisposableStackPrototype, proto)
	constructor(cx, "DisposableStack", 0, engine.IntrinsicDisposableStackPrototype, nativeDisposableStackConstructor)

	method(cx, proto, "adopt", 2, nativeDisposableStackAdopt)
	method(cx, proto, "defer", 1, nativeDisposableStackDefer)
	dispose := method(cx, proto, "dispose", 0, nativeDisposableStackDispose)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolDispose), storage.ObjectValue(dispose), engine.FlagsHidden)
	getter(cx, proto, cx.Intern("disposed"), "disposed", nativeDisposableStackDisposed)
	method(cx, proto, "move", 0, nativeDisposableStackMove)
	method(cx, proto, "use", 1, nativeDisposableStackUse)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("DisposableStack"), engine.FlagConfigurable)
}

var (
	nativeDisposableStackConstructor = engine.RegisterNative(disposableStackConstructorNative(engine.SyncDispose))
	nativeDisposableStackAdopt       = engine.RegisterNative(disposableStackAdoptNative(engine.SyncDispose))
	nativeDisposableStackDefer       = engine.RegisterNative(disposableStackDeferNative(engine.SyncDispose))
	nativeDisposableStackDisposed    = engine.RegisterNative(disposableStackDisposedNative(engine.SyncDispose))
	nativeDisposableStackMove        = engine.RegisterNative(disposableStackMoveNative(engine.SyncDispose))
	nativeDisposableStackUse         = engine.RegisterNative(disposableStackUseNative(engine.SyncDispose))
)

func disposableStackDispose(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisDisposableStack(cx, engine.SyncDispose, this, "dispose")
	if s.disposed {
		return storage.Undefined
	}
	s.disposed = true
	var result engine.Value
	var failed bool
	cx.DisposeResources(s.dc, storage.Undefined, false, func(cx *engine.ExecutionContext, v engine.Value, thrown bool) {
		result, failed = v, thrown
	})
	if failed {
		cx.Throw(result)
	}
	return result
}

var nativeAsyncDisposableStackDisposeAsync = engine.RegisterNative(asyncDisposableStackDisposeAsync)

func initAsyncDisposableStack(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicAsyncDisposableStackPrototype, proto)
	constructor(cx, "AsyncDisposableStack", 0, engine.IntrinsicAsyncDisposableStackPrototype, nativeAsyncDisposableStackConstructor)

	method(cx, proto, "adopt", 2, nativeAsyncDisposableStackAdopt)
	method(cx, proto, "defer", 1, nativeAsyncDisposableStackDefer)
	dispose := method(cx, proto, "disposeAsync", 0, nativeAsyncDisposableStackDisposeAsync)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolAsyncDispose), storage.ObjectValue(dispose), engine.FlagsHidden)
	getter(cx, proto, cx.Intern("disposed"), "disposed", nativeAsyncDisposableStackDisposed)
	method(cx, proto, "move", 0, nativeAsyncDisposableStackMove)
	method(cx, proto, "use", 1, nativeAsyncDisposableStackUse)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("AsyncDisposableStack"), engine.FlagConfigurable)
}

var (
	nativeAsyncDisposableStackConstructor = engine.RegisterNative(disposableStackConstructorNative(engine.AsyncDispose))
	nativeAsyncDisposableStackAdopt       = engine.RegisterNative(disposableStackAdoptNative(engine.AsyncDispose))
	nativeAsyncDisposableStackDefer       = engine.RegisterNative(disposableStackDeferNative(engine.AsyncDispose))
	nativeAsyncDisposableStackDisposed    = engine.RegisterNative(disposableStackDisposedNative(engine.AsyncDispose))
	nativeAsyncDisposableStackMove        = engine.RegisterNative(disposableStackMoveNative(engine.AsyncDispose))
	nativeAsyncDisposableStackUse         = engine.RegisterNative(disposableStackUseNative(engine.AsyncDispose))
)

func asyncDisposableStackDisposeAsync(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pc := cx.NewPromiseCapability(storage.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
	s, _ := storage.DisposableStackState(this, true).(*disposableStack)
	switch {
	case s == nil:
		cx.RejectCapability(pc, cx.NewError(engine.ErrorKindType, "AsyncDisposableStack.prototype.disposeAsync called on incompatible receiver "+cx.DisplayString(this)))
	case s.disposed:
		cx.ResolveCapability(pc, storage.Undefined)
	default:
		s.disposed = true
		cx.DisposeResources(s.dc, storage.Undefined, false, func(cx *engine.ExecutionContext, v engine.Value, thrown bool) {
			if thrown {
				cx.RejectCapability(pc, v)
				return
			}
			cx.ResolveCapability(pc, v)
		})
	}
	return pc.Promise()
}

// disposableStackConstructorNative returns the constructor of the stacks with hint.
//
//go:noinline
func disposableStackConstructorNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor " + disposableStackName(hint) + " requires 'new'")
		}
		return newDisposableStack(cx, hint, cx.GetPrototypeFromConstructor(nt, disposableStackPrototype(hint)), engine.NewDisposeCapability())
	}
}

var nativeDisposeAdopted = engine.RegisterNative(disposeAdopted)

// disposableStackAdoptNative returns the method adopt of the stacks with hint.
//
//go:noinline
func disposableStackAdoptNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "adopt")
		v, onDispose := arg(args, 0), arg(args, 1)
		if !onDispose.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(onDispose) + " is not a function")
		}
		f := cx.NewNativeWithState("", 0, nativeDisposeAdopted, &adoptedResource{
			value:     v,
			onDispose: onDispose.Object(),
		})
		cx.AddDisposableResource(s.dc, storage.Undefined, hint, f)
		return v
	}
}

// disposableStackDeferNative returns the method defer of the stacks with hint.
//
//go:noinline
func disposableStackDeferNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "defer")
		onDispose := arg(args, 0)
		if !onDispose.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(onDispose) + " is not a function")
		}
		cx.AddDisposableResource(s.dc, storage.Undefined, hint, onDispose.Object())
		return storage.Undefined
	}
}

// disposableStackDisposedNative returns the getter of the property disposed of the stacks with hint.
//
//go:noinline
func disposableStackDisposedNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return storage.Bool(thisDisposableStack(cx, hint, this, "disposed").disposed)
	}
}

// disposableStackMoveNative returns the method move of the stacks with hint.
//
//go:noinline
func disposableStackMoveNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "move")
		moved := newDisposableStack(cx, hint, cx.Intrinsic(disposableStackPrototype(hint)), s.dc)
		s.dc = engine.NewDisposeCapability()
		s.disposed = true
		return moved
	}
}

// disposableStackUseNative returns the method use of the stacks with hint.
//
//go:noinline
func disposableStackUseNative(hint engine.DisposeHint) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPendingDisposableStack(cx, hint, this, "use")
		v := arg(args, 0)
		cx.AddDisposableResource(s.dc, v, hint, nil)
		return v
	}
}
