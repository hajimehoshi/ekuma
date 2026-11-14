// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// DisposeHint is the hint of a disposable resource.
type DisposeHint uint8

const (
	SyncDispose DisposeHint = iota
	AsyncDispose
)

// disposableResource is a DisposableResource Record. A nil method is undefined.
type disposableResource struct {
	value  Value
	hint   DisposeHint
	method *Object
}

// DisposeCapability is a DisposeCapability Record.
type DisposeCapability struct {
	stack []disposableResource

	// completion and thrown are the completion of a running DisposeResources:
	// a throw completion of completion when thrown is set, and a normal one otherwise.
	completion Value
	thrown     bool

	needsAwait, hasAwaited bool
}

// NewDisposeCapability implements NewDisposeCapability.
func NewDisposeCapability() *DisposeCapability {
	return &DisposeCapability{}
}

// AddDisposableResource implements AddDisposableResource. A nil method is not present.
func (vm *VM) AddDisposableResource(dc *DisposeCapability, v Value, hint DisposeHint, method *Object) {
	if method != nil {
		if !(Value{o: method}).IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(Value{o: method}) + " is not a function")
		}
		dc.stack = append(dc.stack, disposableResource{
			value:  Undefined,
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
			value: Undefined,
			hint:  hint,
		})
		return
	}
	if !v.IsObject() {
		vm.ThrowTypeError(vm.DisplayString(v) + " is not an object and cannot be disposed")
	}
	method = vm.getDisposeMethod(v, hint)
	if method == nil {
		vm.ThrowTypeError(vm.DisplayString(v) + " is not disposable")
	}
	dc.stack = append(dc.stack, disposableResource{
		value:  v,
		hint:   hint,
		method: method,
	})
}

// getDisposeMethod implements GetDisposeMethod. It returns nil for undefined.
func (vm *VM) getDisposeMethod(v Value, hint DisposeHint) *Object {
	if hint == SyncDispose {
		return vm.GetMethod(v, vm.WellKnownSymbol(SymbolDispose))
	}
	if m := vm.GetMethod(v, vm.WellKnownSymbol(SymbolAsyncDispose)); m != nil {
		return m
	}
	m := vm.GetMethod(v, vm.WellKnownSymbol(SymbolDispose))
	if m == nil {
		return nil
	}
	return vm.NewNative("", 0, func(vm *VM, this Value, args []Value) Value {
		c := vm.newIntrinsicCapability()
		if e, ok := vm.Completion(func() {
			vm.Call(m, this)
		}); !ok {
			vm.RejectCapability(c, e)
			return Value{o: c.promise}
		}
		vm.ResolveCapability(c, Undefined)
		return Value{o: c.promise}
	})
}

// beginDispose starts DisposeResources on dc for the completion v, which is a throw
// completion when thrown is set.
func (dc *DisposeCapability) beginDispose(v Value, thrown bool) {
	dc.completion = v
	dc.thrown = thrown
	dc.needsAwait = false
	dc.hasAwaited = false
}

// disposeStep continues DisposeResources on dc until it has to await. It reports the value
// to await and true, or false when every resource is disposed.
func (vm *VM) disposeStep(dc *DisposeCapability) (Value, bool) {
	for len(dc.stack) > 0 {
		r := dc.stack[len(dc.stack)-1]
		if r.hint == SyncDispose && dc.needsAwait && !dc.hasAwaited {
			dc.needsAwait = false
			return Undefined, true
		}
		dc.stack[len(dc.stack)-1] = disposableResource{}
		dc.stack = dc.stack[:len(dc.stack)-1]
		if r.method == nil {
			dc.needsAwait = true
			continue
		}
		var result Value
		if e, ok := vm.Completion(func() {
			result = vm.Call(r.method, r.value)
		}); !ok {
			vm.disposeError(dc, e)
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
		return Undefined, true
	}
	return Undefined, false
}

// disposeError records that disposing a resource of dc threw e.
func (vm *VM) disposeError(dc *DisposeCapability, e Value) {
	if !dc.thrown {
		dc.completion = e
		dc.thrown = true
		return
	}
	s := vm.NewErrorObject(vm.Intrinsic(ErrorPrototypeIntrinsic(ErrorKindSuppressed)))
	vm.Define(s, vm.Key(KeyError), e, FlagsHidden)
	vm.Define(s, vm.Key(KeySuppressed), dc.completion, FlagsHidden)
	vm.RecordStack(s, "")
	dc.completion = Value{o: s}
}

// endDispose ends DisposeResources on dc and returns its completion.
func (dc *DisposeCapability) endDispose() (Value, bool) {
	v, thrown := dc.completion, dc.thrown
	dc.completion = Undefined
	return v, thrown
}

// DisposeResources implements DisposeResources for a built-in function, for the completion v,
// which is a throw completion when thrown is set. k receives the resulting completion, now or,
// when a disposal awaits, in a later job.
func (vm *VM) DisposeResources(dc *DisposeCapability, v Value, thrown bool, k AwaitHandler) {
	dc.beginDispose(v, thrown)
	vm.continueDispose(dc, k)
}

func (vm *VM) continueDispose(dc *DisposeCapability, k AwaitHandler) {
	for {
		v, await := vm.disposeStep(dc)
		if !await {
			v, thrown := dc.endDispose()
			k(vm, v, thrown)
			return
		}
		e, ok := vm.Completion(func() {
			vm.Await(v, func(vm *VM, v Value, rejected bool) {
				if rejected {
					vm.disposeError(dc, v)
				}
				vm.continueDispose(dc, k)
			})
		})
		if ok {
			return
		}
		vm.disposeError(dc, e)
	}
}

// addDisposableOp adds v with hint to the dispose capability that the register value d holds,
// creating it when d is undefined, and returns the new register value.
func (vm *VM) addDisposableOp(v, d Value, hint DisposeHint) Value {
	if d.IsUndefined() {
		d = Value{o: &Object{
			kind: kindObject,
			ext:  NewDisposeCapability(),
		}}
	}
	vm.AddDisposableResource(d.o.ext.(*DisposeCapability), v, hint, nil)
	return d
}

// beginDisposeOp starts DisposeResources on the dispose capability that the register value d
// holds, if any, for the completion v of the kind k, which is a throw when it is 1.
// A generator that is being closed has a return completion.
func beginDisposeOp(d, v, k Value) {
	if d.IsUndefined() {
		return
	}
	d.o.ext.(*DisposeCapability).beginDispose(v, k.o == nil && k.n == 1 && v.o != closeSignal)
}

// disposeStepOp is disposeStep on the dispose capability that the register value d holds, if any.
func (vm *VM) disposeStepOp(d Value) (Value, bool) {
	if d.IsUndefined() {
		return Undefined, false
	}
	return vm.disposeStep(d.o.ext.(*DisposeCapability))
}

// endDisposeOp is endDispose on the dispose capability that the register value d holds.
// It reports false when d holds none.
func endDisposeOp(d Value) (Value, bool) {
	if d.IsUndefined() {
		return Undefined, false
	}
	return d.o.ext.(*DisposeCapability).endDispose()
}
