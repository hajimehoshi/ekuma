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

// activate installs the running realm and preserves the host's entered realm.
func (cx ExecutionContext) activate() {
	if cx.vm.curRealm != cx.realm {
		cx.vm.switchRealm(cx.realm)
	}
}

// ThrowTypeError throws a TypeError in the execution realm.
func (cx ExecutionContext) ThrowTypeError(msg string) {
	cx.vm.ThrowTypeError(cx.realm, msg)
}

// WithExecutionContext adapts f for registration as a native function or constructor.
func WithExecutionContext(f func(ExecutionContext, Value, []Value) Value) NativeFunc {
	return func(vm *VM, this Value, args []Value) Value {
		return f(vm.natives[len(vm.natives)-1].context, this, args)
	}
}
