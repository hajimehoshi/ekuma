// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// ExecutionContext binds synchronous operations to their execution realm.
type ExecutionContext struct {
	vm    *VM
	realm *Realm
}

func (vm *VM) executionContext() ExecutionContext {
	return ExecutionContext{
		vm:    vm,
		realm: vm.curRealm,
	}
}

// activate installs cx as the running realm, preserving the entered realm.
// It returns the realm to restore, or nil if cx was already active.
func (cx ExecutionContext) activate() *Realm {
	caller := cx.vm.curRealm
	if caller == cx.realm {
		return nil
	}
	cx.vm.switchRealm(cx.realm)
	return caller
}

// ThrowTypeError throws a TypeError in the execution realm.
func (cx ExecutionContext) ThrowTypeError(msg string) {
	cx.vm.ThrowTypeError(cx.realm, msg)
}

// NativeContext returns the context bound at entry to the running native function.
func (vm *VM) NativeContext() ExecutionContext {
	return vm.natives[len(vm.natives)-1].context
}

// WithExecutionContext adapts f for registration as a native function or constructor.
func WithExecutionContext(f func(ExecutionContext, Value, []Value) Value) NativeFunc {
	return func(vm *VM, this Value, args []Value) Value {
		return f(vm.NativeContext(), this, args)
	}
}

// Run runs f in the execution realm.
func (cx ExecutionContext) Run(f func()) {
	cx.vm.InRealm(cx.realm, f)
}

// CreateIterResultObject creates an iterator result in the execution realm.
func (cx ExecutionContext) CreateIterResultObject(v Value, done bool) Value {
	return cx.realm.CreateIterResultObject(v, done)
}

// NewArrayOf returns an array in the execution realm with the given elements, which it keeps.
func (cx ExecutionContext) NewArrayOf(elems []Value) Value {
	return ObjectValue(cx.realm.newArray(elems))
}

// NewObject returns an empty object in the execution realm with the given prototype.
func (cx ExecutionContext) NewObject(proto *Object) *Object {
	return newObject(cx.vm.shapeForRealm(proto, cx.realm))
}

// Call calls f from the execution realm.
func (cx ExecutionContext) Call(f *Object, this Value, args ...Value) Value {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	return cx.vm.Call(f, this, args...)
}

// CreateDataPropertyOrThrow implements CreateDataPropertyOrThrow.
func (cx ExecutionContext) CreateDataPropertyOrThrow(o *Object, key PropKey, v Value) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	cx.vm.CreateDataPropertyOrThrow(o, key, v)
}

// IteratorStepValue returns the next value, or reports false when the iterator is done.
func (cx ExecutionContext) IteratorStepValue(it, next Value) (Value, bool) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	return cx.vm.IteratorStepValue(it, next)
}

// IteratorStep advances the iterator and reports whether it yielded.
func (cx ExecutionContext) IteratorStep(it, next Value) bool {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	return cx.vm.IteratorStep(it, next)
}

// IteratorClose closes the iterator for a normal completion.
func (cx ExecutionContext) IteratorClose(it Value) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	cx.vm.IteratorClose(it)
}

// IteratorCloseAll closes the iterators in reverse order, skipping undefined entries.
func (cx ExecutionContext) IteratorCloseAll(its []Value) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	cx.vm.IteratorCloseAll(its)
}

// IfAbruptCloseIterator closes it and rethrows the exception when f throws.
func (cx ExecutionContext) IfAbruptCloseIterator(it Value, f func()) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	cx.vm.IfAbruptCloseIterator(it, f)
}

// IfAbruptCloseIterators closes the iterators and rethrows the exception when f throws.
func (cx ExecutionContext) IfAbruptCloseIterators(its []Value, f func()) {
	if caller := cx.activate(); caller != nil {
		defer cx.vm.switchRealm(caller)
	}
	cx.vm.IfAbruptCloseIterators(its, f)
}
