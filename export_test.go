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

// SetExceptionReporter installs the shell's uncaught exception handler in r.
func SetExceptionReporter(r *Realm, report Value) {
	vm := r.vm.e
	r.in(func() {
		key := engine.NamedKey(vm.Key(engine.KeyToString))
		objectToString := vm.Get(vm.Intrinsic(engine.IntrinsicObjectPrototype), key)
		errorToString := vm.Get(vm.Intrinsic(engine.IntrinsicErrorPrototype), key)
		vm.SetExceptionReporter(func(thrown engine.Value, loc engine.ExceptionLocation) {
			var source engine.Value
			if loc.File != "" {
				source = vm.StringFromGo(loc.File)
			}
			description := vm.ExceptionDescription(thrown, objectToString, errorToString)
			vm.Call(report.value.Object(), engine.Undefined, thrown, source,
				engine.Number(float64(loc.Line)), engine.Number(float64(max(loc.Column, 1))), description, vm.NewString(loc.SourceLine))
		})
	})
}

// CallScript evaluates s from a running shell function.
func CallScript(s *Script) Value {
	return fromEngine(s.vm.e.CallScript(s.s))
}

// EvaluateScriptTask reports whether s completed normally before its job checkpoint.
func EvaluateScriptTask(ctx context.Context, s *Script) (bool, error) {
	_, completed, err := s.evaluate(ctx)
	return completed, err
}

// RequestInterrupt schedules an interruption and returns the function that withdraws it.
func RequestInterrupt(r *Realm, cause error) func() {
	var req interruptRequest
	r.vm.interrupts.request(r.vm.e, &req, cause)
	return func() { r.vm.interrupts.release(r.vm.e, &req) }
}

// InterruptNow interrupts the running JavaScript call with cause.
func InterruptNow(r *Realm, cause error) {
	withdraw := RequestInterrupt(r, cause)
	defer withdraw()
	r.vm.e.Poll()
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

// ObjectCreationRealm returns the host realm of the object v, or nil if it has none.
func ObjectCreationRealm(v Value) *Realm {
	o := v.value.Object()
	r := o.CreationRealm()
	if r == nil {
		return nil
	}
	host, _ := o.VM().RealmHostDefined(r).(*Realm)
	return host
}

// ParseScriptValue parses the JavaScript string src as a script of r.
func ParseScriptValue(r *Realm, name string, src Value) (*Script, error) {
	return r.parseScript(name, src.value.Str())
}

// InstallGlobalProxy installs a d8 global proxy in r, reusing the proxy of previous if supplied.
func InstallGlobalProxy(r *Realm, foreignView Value, previous *Realm, shareAccess bool) Value {
	var prev *engine.Realm
	if previous != nil {
		prev = previous.r
	}
	var proxy engine.Value
	r.in(func() {
		vm := r.vm.e
		proxy = vm.InstallGlobalProxy(foreignView.value.Object(), prev, shareAccess)
		vm.SetPrototypeOf(vm.GlobalObject().Object(), newV8GlobalPrototype(vm, proxy))
	})
	return fromEngine(proxy)
}

func newV8GlobalPrototype(vm *engine.VM, globalThis engine.Value) *engine.Object {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("", 0, func(vm *engine.VM, this engine.Value, _ []engine.Value) engine.Value {
		if target := vm.GetNewTarget(); target != nil {
			instanceProto := vm.GetPrototypeFromConstructor(target, engine.IntrinsicObjectPrototype)
			return engine.ObjectValue(vm.NewObject(instanceProto))
		}
		if this.IsNullish() {
			return globalThis
		}
		return engine.ObjectValue(vm.ToObject(this))
	})
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), engine.FlagWritable)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	return proto
}

// NewRealmShared provides operations to install and update Realm.shared in vm.
func NewRealmShared(vm *VM) Value {
	vm.initIfNeeded()
	key := vm.e.Intern("shared")
	propKey := vm.e.KeyOfString("shared")
	var shared engine.Value
	targets := map[*engine.Object]uint8{}
	install := func(_ Value, args []Value) (any, error) {
		target := args[0].value.Object()
		vm.e.Define(target, key, shared, engine.FlagsData)
		targets[target] = engine.FlagsData
		return Undefined, nil
	}
	changed := func(_ Value, args []Value) (any, error) {
		target := args[0].value.Object()
		if _, linked := targets[target]; !linked {
			return Undefined, nil
		}
		d, ok := vm.e.GetOwnProperty(target, propKey)
		if !ok || d.IsAccessor() {
			delete(targets, target)
			return Undefined, nil
		}
		var flags uint8
		if d.Writable() {
			flags |= engine.FlagWritable
		}
		if d.Enumerable() {
			flags |= engine.FlagEnumerable
		}
		if d.Configurable() {
			flags |= engine.FlagConfigurable
		}
		targets[target] = flags
		if !engine.SameValue(shared, d.Value()) {
			shared = d.Value()
			// A frozen Realm still observes writes made through another realm.
			for other, flags := range targets {
				vm.e.Define(other, key, shared, flags)
			}
		}
		return Undefined, nil
	}
	return vm.ValueOf(map[string]any{
		"install": install,
		"changed": changed,
	})
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
