// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// InvokeInRealm calls f with r as the entered realm.
func InvokeInRealm(ctx context.Context, r *Realm, f Value) error {
	var err error
	r.in(func() {
		_, err = f.Invoke(ctx)
	})
	return err
}

// DeferUnhandledRejections postpones rejection reports until TakeUnhandledRejection.
func DeferUnhandledRejections(vm *VM) {
	vm.initIfNeeded()
	vm.e.DeferUnhandledRejections()
}

// TakeUnhandledRejection returns the first deferred rejection still unhandled, if any.
func TakeUnhandledRejection(vm *VM) error {
	reason, ok := vm.e.TakeUnhandledRejection()
	if !ok {
		return nil
	}
	return &ScriptError{
		Value: fromEngine(reason),
	}
}

// IsEnteredRealm reports whether r is the VM's entered realm.
// Jobs record an entered realm only in VMs with global proxies.
func IsEnteredRealm(r *Realm) bool {
	return r.vm.e.IsEnteredRealm(r.r)
}

// ParseScriptValue parses the JavaScript string src as a script of r.
func ParseScriptValue(r *Realm, name string, src Value) (*Script, error) {
	return r.parseScript(name, src.value.Str())
}

// InstallGlobalProxy installs and returns a global proxy in r. Property access in r
// on globals outside its access group goes through foreignView. When shareAccess
// is true, r joins the entered realm's group.
func InstallGlobalProxy(r *Realm, foreignView Value, shareAccess bool) Value {
	var proxy engine.Value
	r.in(func() {
		proxy = r.vm.e.InstallGlobalProxy(foreignView.value.Object(), shareAccess)
	})
	return fromEngine(proxy)
}

// DetachGlobalProxy disconnects the global proxy installed in r.
func DetachGlobalProxy(r *Realm) {
	r.in(r.vm.e.DetachGlobalProxy)
}

// CanAccessReceiver reports whether r may use receiver in a checked host call.
func CanAccessReceiver(r *Realm, receiver Value) bool {
	var allowed bool
	r.in(func() {
		allowed = r.vm.e.CanAccessReceiver(receiver.value)
	})
	return allowed
}

// NewHTMLDDA returns an object of r with the [[IsHTMLDDA]] internal slot, which returns null when called.
func NewHTMLDDA(r *Realm) Value {
	var v engine.Value
	r.in(func() {
		v = r.vm.e.NewHTMLDDA(func(*engine.VM, engine.Value, []engine.Value) engine.Value {
			return engine.Null
		})
	})
	return fromEngine(v)
}

// AbstractModuleSource returns %AbstractModuleSource% of r.
func AbstractModuleSource(r *Realm) Value {
	var v engine.Value
	r.in(func() {
		v = engine.ObjectValue(r.vm.e.Intrinsic(engine.IntrinsicAbstractModuleSource))
	})
	return fromEngine(v)
}

// NewSourceModule returns a module of vm whose only export is a default of undefined and
// whose module source is an instance of %AbstractModuleSource% named className.
func NewSourceModule(vm *VM, name, className string) *Module {
	m := vm.NewSyntheticModule(name, Undefined)
	proto := vm.e.NewObject(vm.e.Intrinsic(engine.IntrinsicAbstractModuleSourcePrototype))
	m.m.SetModuleSource(vm.e.NewModuleSourceObject(proto, className))
	return m
}

func ArgumentStorage(vm *VM) (argsLen, callArgsLen int, cleared bool) {
	for _, v := range vm.args[len(vm.args):cap(vm.args)] {
		if !v.IsUndefined() {
			return len(vm.args), len(vm.callArgs), false
		}
	}
	for _, v := range vm.callArgs[len(vm.callArgs):cap(vm.callArgs)] {
		if !v.IsUndefined() {
			return len(vm.args), len(vm.callArgs), false
		}
	}
	return len(vm.args), len(vm.callArgs), true
}

// MaxStringLength is the largest length a string may have, in bytes.
const MaxStringLength = engine.MaxStringLength

// InternalFailureFunc returns a native function that fails with cause.
func InternalFailureFunc(vm *VM, cause error) Value {
	vm.initIfNeeded()
	return fromEngine(vm.e.NewFunction("fail", func(*engine.VM, engine.Value, []engine.Value) engine.Value {
		panic(&xerrors.InternalError{
			Op:  "test operation",
			Err: cause,
		})
	}))
}
