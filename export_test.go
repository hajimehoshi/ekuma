// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// DetachArrayBuffer detaches the ArrayBuffer v. The error is a TypeError for a value
// that is not an ArrayBuffer or for an immutable ArrayBuffer.
func DetachArrayBuffer(vm *VM, v Value) error {
	vm.initIfNeeded()
	e := v.engine()
	b := engine.ArrayBufferData(e)
	if b == nil || b.IsShared() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "not an ArrayBuffer"))}
	}
	if b.IsImmutable() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "cannot detach an immutable ArrayBuffer"))}
	}
	vm.e.DetachArrayBuffer(e.Object())
	return nil
}

// SharedDataBlock is the data block of a SharedArrayBuffer, which VMs on other goroutines may share.
type SharedDataBlock = engine.SharedDataBlock

// SharedDataBlockOf returns the data block of the SharedArrayBuffer v, or nil when v is not one.
func SharedDataBlockOf(v Value) *SharedDataBlock {
	b := engine.ArrayBufferData(v.engine())
	if b == nil {
		return nil
	}
	return b.SharedDataBlock()
}

// NewSharedArrayBuffer returns a SharedArrayBuffer of vm over the data block blk.
func NewSharedArrayBuffer(vm *VM, blk *SharedDataBlock) Value {
	vm.initIfNeeded()
	proto := vm.e.Intrinsic(engine.IntrinsicSharedArrayBufferPrototype)
	return fromEngine(engine.ObjectValue(vm.e.NewSharedArrayBufferObject(proto, blk)))
}

// SetAgentCanSuspend sets whether Atomics.wait may suspend vm.
func SetAgentCanSuspend(vm *VM, canSuspend bool) {
	vm.initIfNeeded()
	vm.e.SetAgentCanSuspend(canSuspend)
}

// AwaitWaiters blocks until a waiter of Atomics.waitAsync of vm is notified by another agent or
// times out, and then runs the jobs. It reports false when vm has no such waiter. The error is
// the cause of ctx when ctx is done.
func AwaitWaiters(ctx context.Context, vm *VM) (bool, error) {
	vm.initIfNeeded()
	var waited bool
	_, err := vm.run(ctx, func() (engine.Value, error) {
		if waited = vm.e.AwaitWaiters(); !waited {
			return engine.Undefined, nil
		}
		return engine.Undefined, vm.e.RunJobs()
	})
	if err == nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	return waited, err
}

// Realm is a realm of a VM other than the one the VM starts with.
type Realm struct {
	r *engine.Realm
}

// NewRealm creates a realm of vm with the standard built-in objects.
func NewRealm(vm *VM) *Realm {
	vm.initIfNeeded()
	return &Realm{
		r: vm.e.CreateRealm(func() {
			builtin.Install(vm.e)
		}),
	}
}

// InRealm runs f with r as the current realm of vm, in which the methods of vm work.
func InRealm(vm *VM, r *Realm, f func()) {
	vm.e.InRealm(r.r, f)
}

// NewHTMLDDA returns an object of vm with the [[IsHTMLDDA]] internal slot, which returns null when called.
func NewHTMLDDA(vm *VM) Value {
	vm.initIfNeeded()
	return fromEngine(vm.e.NewHTMLDDA(func(*engine.VM, engine.Value, []engine.Value) engine.Value {
		return engine.Null
	}))
}

// AbstractModuleSource returns %AbstractModuleSource% of the current realm of vm.
func AbstractModuleSource(vm *VM) Value {
	vm.initIfNeeded()
	return fromEngine(engine.ObjectValue(vm.e.Intrinsic(engine.IntrinsicAbstractModuleSource)))
}

// NewSourceModule returns a module of vm whose only export is a default of undefined and
// whose module source is an instance of %AbstractModuleSource% named className.
func NewSourceModule(vm *VM, name, className string) *Module {
	m := vm.NewSyntheticModule(name, Undefined)
	proto := vm.e.NewObject(vm.e.Intrinsic(engine.IntrinsicAbstractModuleSourcePrototype))
	m.m.SetModuleSource(vm.e.NewModuleSourceObject(proto, className))
	return m
}
