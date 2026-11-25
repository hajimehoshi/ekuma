// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// DisposeHint is the hint of a disposable resource.
type DisposeHint uint8

const (
	SyncDispose DisposeHint = iota
	AsyncDispose
)

// disposableResource is a DisposableResource Record. A nil method is undefined.
type disposableResource struct {
	value  storage.Value
	hint   DisposeHint
	method *storage.Object
}

// DisposeCapability is a DisposeCapability Record.
type DisposeCapability struct {
	stack []disposableResource

	// completion and thrown are the completion of a running DisposeResources:
	// a throw completion of completion when thrown is set, and a normal one otherwise.
	completion storage.Value
	thrown     bool

	needsAwait, hasAwaited bool
}

// NewDisposeCapability implements NewDisposeCapability.
func NewDisposeCapability() *DisposeCapability {
	return &DisposeCapability{}
}

// AddDisposableResource implements AddDisposableResource. A nil method is not present.
func (cx *ExecutionContext) AddDisposableResource(dc *DisposeCapability, v storage.Value, hint DisposeHint, method *storage.Object) {
	if method != nil {
		if !(storage.Value{O: method}).IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(storage.Value{O: method}) + " is not a function")
		}
		dc.stack = append(dc.stack, disposableResource{
			value:  storage.Undefined,
			hint:   hint,
			method: method,
		})
		return
	}
	if v.IsNullish() {
		if hint == SyncDispose {
			return
		}
		dc.stack = append(dc.stack, disposableResource{
			value: storage.Undefined,
			hint:  hint,
		})
		return
	}
	if !v.IsObject() {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not an object and cannot be disposed")
	}
	method = cx.getDisposeMethod(v, hint)
	if method == nil {
		cx.ThrowTypeError(cx.DisplayString(v) + " is not disposable")
	}
	dc.stack = append(dc.stack, disposableResource{
		value:  v,
		hint:   hint,
		method: method,
	})
}

// getDisposeMethod implements GetDisposeMethod. It returns nil for undefined.
func (cx *ExecutionContext) getDisposeMethod(v storage.Value, hint DisposeHint) *storage.Object {
	if hint == SyncDispose {
		return cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolDispose))
	}
	if m := cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolAsyncDispose)); m != nil {
		return m
	}
	m := cx.GetMethod(v, cx.WellKnownSymbol(storage.SymbolDispose))
	if m == nil {
		return nil
	}
	return cx.NewNativeWithState("", 0, nativeAsyncDisposeFunction, m)
}

// asyncDisposeFunction is the code of the functions that GetDisposeMethod makes for a synchronous
// dispose method. Their state is the method, an *Object.
func asyncDisposeFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	m := cx.ActiveNativeState().(*storage.Object)
	c := cx.newIntrinsicCapability()
	if e, ok := cx.Completion(func() {
		cx.Call(m, this)
	}); !ok {
		cx.RejectCapability(c, e)
		return storage.Value{O: c.promise}
	}
	cx.ResolveCapability(c, storage.Undefined)
	return storage.Value{O: c.promise}
}

// beginDispose starts DisposeResources on dc for the completion v, which is a throw
// completion when thrown is set.
func (dc *DisposeCapability) beginDispose(v storage.Value, thrown bool) {
	dc.completion = v
	dc.thrown = thrown
	dc.needsAwait = false
	dc.hasAwaited = false
}

// disposeStep continues DisposeResources on dc until it has to await. It reports the value
// to await and true, or false when every resource is disposed.
func (cx *ExecutionContext) disposeStep(dc *DisposeCapability) (storage.Value, bool) {
	for len(dc.stack) > 0 {
		r := dc.stack[len(dc.stack)-1]
		if r.hint == SyncDispose && dc.needsAwait && !dc.hasAwaited {
			dc.needsAwait = false
			return storage.Undefined, true
		}
		dc.stack = storage.WithoutLast(dc.stack)
		if r.method == nil {
			dc.needsAwait = true
			continue
		}
		var result storage.Value
		if e, ok := cx.Completion(func() {
			result = cx.Call(r.method, r.value)
		}); !ok {
			cx.disposeError(dc, e)
			continue
		}
		if r.hint == AsyncDispose {
			dc.hasAwaited = true
			return result, true
		}
	}
	dc.stack = nil
	if dc.needsAwait && !dc.hasAwaited {
		dc.needsAwait = false
		return storage.Undefined, true
	}
	return storage.Undefined, false
}

// disposeError records that disposing a resource of dc threw e.
func (cx *ExecutionContext) disposeError(dc *DisposeCapability, e storage.Value) {
	if !dc.thrown {
		dc.completion = e
		dc.thrown = true
		return
	}
	s := cx.NewErrorObject(cx.Intrinsic(ErrorPrototypeIntrinsic(ErrorKindSuppressed)))
	cx.Define(s, cx.Key(storage.KeyError), e, storage.FlagsHidden)
	suppressed, _ := unpackException(dc.completion)
	cx.Define(s, cx.Key(storage.KeySuppressed), suppressed, storage.FlagsHidden)
	s.SetExtState(cx.newErrorData(s, "", len(cx.frames), len(cx.natives)))
	dc.completion = storage.Value{O: s}
}

// endDispose ends DisposeResources on dc and returns its completion.
func (dc *DisposeCapability) endDispose() (storage.Value, bool) {
	v, thrown := dc.completion, dc.thrown
	dc.completion = storage.Undefined
	return v, thrown
}

// DisposeResources implements DisposeResources for a built-in function, for the completion v,
// which is a throw completion when thrown is set. k receives the resulting completion, now or,
// when a disposal awaits, in a later job.
func (cx *ExecutionContext) DisposeResources(dc *DisposeCapability, v storage.Value, thrown bool, k AwaitHandler) {
	dc.beginDispose(v, thrown)
	cx.continueDispose(dc, k)
}

func (cx *ExecutionContext) continueDispose(dc *DisposeCapability, k AwaitHandler) {
	for {
		v, await := cx.disposeStep(dc)
		if !await {
			v, thrown := dc.endDispose()
			k(cx, v, thrown)
			return
		}
		e, ok := cx.Completion(func() {
			cx.Await(v, func(cx *ExecutionContext, v storage.Value, rejected bool) {
				if rejected {
					cx.disposeError(dc, v)
				}
				cx.continueDispose(dc, k)
			})
		})
		if ok {
			return
		}
		cx.disposeError(dc, e)
	}
}

// addDisposableOp adds v with hint to the dispose capability that the register value d holds,
// creating it when d is undefined, and returns the new register value.
func (cx *ExecutionContext) addDisposableOp(v, d storage.Value, hint DisposeHint) storage.Value {
	if d.IsUndefined() {
		d = storage.Value{O: storage.NewObjectWithState(nil, storage.ClassObject, NewDisposeCapability())}
	}
	cx.AddDisposableResource(d.O.ExtState().(*DisposeCapability), v, hint, nil)
	return d
}

// beginDisposeOp starts DisposeResources on the dispose capability that the register value d
// holds, if any, for the completion v of the kind k, which is a throw when it is 1.
// A generator that is being closed has a return completion.
func beginDisposeOp(d, v, k storage.Value) {
	if d.IsUndefined() {
		return
	}
	d.O.ExtState().(*DisposeCapability).beginDispose(v, k.O == storage.NumberObject && k.N == 1 && v.O != closeSignal)
}

// disposeStepOp is disposeStep on the dispose capability that the register value d holds, if any.
func (cx *ExecutionContext) disposeStepOp(d storage.Value) (storage.Value, bool) {
	if d.IsUndefined() {
		return storage.Undefined, false
	}
	return cx.disposeStep(d.O.ExtState().(*DisposeCapability))
}

// endDisposeOp is endDispose on the dispose capability that the register value d holds.
// It reports false when d holds none.
func endDisposeOp(d storage.Value) (storage.Value, bool) {
	if d.IsUndefined() {
		return storage.Undefined, false
	}
	return d.O.ExtState().(*DisposeCapability).endDispose()
}
