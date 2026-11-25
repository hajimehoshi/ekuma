// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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
	cx := r.cx
	r.in(func() {
		key := storage.NamedKey(cx.Key(storage.KeyToString))
		objectToString := cx.Get(r.cx.Intrinsic(storage.IntrinsicObjectPrototype), key)
		errorToString := cx.Get(r.cx.Intrinsic(storage.IntrinsicErrorPrototype), key)
		cx.SetExceptionReporter(func(thrown storage.Value, loc engine.ExceptionLocation) {
			var source storage.Value
			if loc.File != "" {
				source = cx.StringFromGo(loc.File)
			}
			description := cx.ExceptionDescription(thrown, objectToString, errorToString)
			cx.Call(report.value.Object(), storage.Undefined, thrown, source,
				storage.Number(float64(loc.Line)), storage.Number(float64(max(loc.Column, 1))), description, cx.NewString(loc.SourceLine))
		})
	})
}

// CallScript evaluates s from a running shell function.
func CallScript(s *Script) Value {
	return fromEngine(s.s.CallScript())
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
	r.cx.Poll()
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
// Promise jobs record an entered realm only in VMs with multiple realms or global proxies.
func IsEnteredRealm(r *Realm) bool {
	return r.cx.IsEnteredRealm()
}

// ObjectCreationRealm returns the host realm of the object v, or nil if it has none.
func ObjectCreationRealm(v Value) *Realm {
	o := v.value.Object()
	cx := engine.CreationContext(o)
	if cx == nil {
		return nil
	}
	host, _ := cx.RealmHostDefined().(*Realm)
	return host
}

// ParseScriptValue parses the JavaScript string src as a script of r.
func ParseScriptValue(r *Realm, name string, src Value) (*Script, error) {
	return r.parseScript(name, src.value.Str())
}

// ParseConservativeScript is [Realm.ParseScript] without the compiler's assignment shortcuts.
func ParseConservativeScript(r *Realm, name, src string) (*Script, error) {
	// The ordinary path makes the errors of an invalid script.
	if _, err := r.ParseScript(name, src); err != nil {
		return nil, err
	}
	prog, err := parser.ParseScript(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		return nil, err
	}
	code, err := compiler.CompileConservativeScript(prog, name)
	if err != nil {
		return nil, err
	}
	return &Script{
		vm: r.vm,
		s:  r.context().NewScript(code),
	}, nil
}

// InstallGlobalProxy installs a d8 global proxy in r, reusing the proxy of previous if supplied.
func InstallGlobalProxy(r *Realm, foreignView Value, previous *Realm, shareAccess bool) Value {
	var prev *engine.ExecutionContext
	if previous != nil {
		prev = previous.cx
	}
	var proxy storage.Value
	r.in(func() {
		cx := r.cx
		proxy = cx.InstallGlobalProxy(foreignView.value.Object(), prev, shareAccess)
		cx.SetPrototypeOf(cx.GlobalObject().Object(), newV8GlobalPrototype(cx))
	})
	return fromEngine(proxy)
}

var nativeV8GlobalConstructor = engine.RegisterNative(v8GlobalConstructor)

func v8GlobalConstructor(cx *engine.ExecutionContext, this storage.Value, _ []storage.Value) storage.Value {
	if target := cx.GetNewTarget(); target != nil {
		return storage.ObjectValue(cx.NewObject(cx.GetPrototypeFromConstructor(target, storage.IntrinsicObjectPrototype)))
	}
	if this.IsNullish() {
		return cx.GetV(cx.GlobalObject(), cx.Intern("globalThis"))
	}
	return storage.ObjectValue(cx.ToObject(this))
}

func newV8GlobalPrototype(cx *engine.ExecutionContext) *storage.Object {
	proto := cx.NewObject(cx.Intrinsic(storage.IntrinsicObjectPrototype))
	ctor := cx.NewNativeConstructor("", 0, nativeV8GlobalConstructor)
	cx.Define(ctor, cx.Key(storage.KeyPrototype), storage.ObjectValue(proto), storage.FlagWritable)
	cx.Define(proto, cx.Key(storage.KeyConstructor), storage.ObjectValue(ctor), storage.FlagsHidden)
	return proto
}

// NewRealmShared provides operations to install and update Realm.shared in vm.
func NewRealmShared(vm *VM) Value {
	vm.initIfNeeded()
	key := vm.hostContext().Intern("shared")
	propKey := vm.hostContext().KeyOfString("shared")
	var shared storage.Value
	targets := map[*storage.Object]uint8{}
	install := func(_ Value, args []Value) (any, error) {
		target := args[0].value.Object()
		vm.hostContext().Define(target, key, shared, storage.FlagsData)
		targets[target] = storage.FlagsData
		return Undefined, nil
	}
	changed := func(_ Value, args []Value) (any, error) {
		target := args[0].value.Object()
		if _, linked := targets[target]; !linked {
			return Undefined, nil
		}
		d, ok := vm.hostContext().GetOwnProperty(target, propKey)
		if !ok || d.IsAccessor() {
			delete(targets, target)
			return Undefined, nil
		}
		var flags uint8
		if d.Writable() {
			flags |= storage.FlagWritable
		}
		if d.Enumerable() {
			flags |= storage.FlagEnumerable
		}
		if d.Configurable() {
			flags |= storage.FlagConfigurable
		}
		targets[target] = flags
		if !storage.SameValue(shared, d.Value()) {
			shared = d.Value()
			// A frozen Realm still observes writes made through another realm.
			for other, flags := range targets {
				vm.hostContext().Define(other, key, shared, flags)
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
	r.in(r.cx.DetachGlobalProxy)
}

// CanAccessReceiver reports whether r may use receiver in a checked host call.
func CanAccessReceiver(r *Realm, receiver Value) bool {
	var allowed bool
	r.in(func() {
		allowed = r.cx.CanAccessReceiver(receiver.value)
	})
	return allowed
}

// NewHTMLDDA returns an object of r with the [[IsHTMLDDA]] internal slot, which returns null when called.
func NewHTMLDDA(r *Realm) Value {
	var v storage.Value
	r.in(func() {
		v = r.cx.NewHTMLDDA(func(*engine.ExecutionContext, storage.Value, []storage.Value) storage.Value {
			return storage.Null
		})
	})
	return fromEngine(v)
}

// AbstractModuleSource returns %AbstractModuleSource% of r.
func AbstractModuleSource(r *Realm) Value {
	return fromEngine(storage.ObjectValue(r.cx.Intrinsic(storage.IntrinsicAbstractModuleSource)))
}

// NewSourceModule returns a module of r whose only export is a default of undefined and
// whose module source is an instance of %AbstractModuleSource% named className.
func NewSourceModule(r *Realm, name, className string) *Module {
	m := r.NewSyntheticModule(name, Undefined)
	r.in(func() {
		cx := r.cx
		proto := cx.NewObject(r.cx.Intrinsic(storage.IntrinsicAbstractModuleSourcePrototype))
		m.m.SetModuleSource(cx.NewModuleSourceObject(proto, className))
	})
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
const MaxStringLength = storage.MaxStringLength

// InternalFailureFunc returns a native function that fails with cause.
func InternalFailureFunc(vm *VM, cause error) Value {
	vm.initIfNeeded()
	return fromEngine(vm.hostContext().NewFunction("fail", func(*engine.ExecutionContext, storage.Value, []storage.Value) storage.Value {
		panic(&xerrors.InternalError{
			Op:  "test operation",
			Err: cause,
		})
	}))
}
