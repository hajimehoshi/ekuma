// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Realm is a realm of a [VM]: a global object with its own standard built-in objects.
//
// The realms of a VM share its promise jobs, settings and other state, except for their
// global objects and built-in objects. Values may pass between realms. A realm is not a
// security boundary: an object of a realm gives access to the built-in objects of its realm.
type Realm struct {
	vm *VM
	cx *engine.ExecutionContext
}

// InitialRealm returns the realm created with vm, in which the methods of vm operate.
func (vm *VM) InitialRealm() *Realm {
	vm.initIfNeeded()
	return vm.initialRealm
}

// Global returns the global object of the initial realm.
func (vm *VM) Global() Value {
	vm.initIfNeeded()
	return vm.initialRealm.Global()
}

// NewRealm returns a new realm of vm.
func (vm *VM) NewRealm() *Realm {
	vm.initIfNeeded()
	return vm.realmOf(vm.e.CreateRealm(builtin.Install))
}

// realmOf returns the public realm for cx.
func (vm *VM) realmOf(cx *engine.ExecutionContext) *Realm {
	if pr, ok := cx.RealmHostDefined().(*Realm); ok {
		return pr
	}
	pr := &Realm{
		vm: vm,
		cx: cx,
	}
	cx.SetRealmHostDefined(pr)
	return pr
}

// in runs f with r as the current realm of its VM.
func (r *Realm) in(f func()) {
	r.context().Run(f)
}

func (r *Realm) context() *engine.ExecutionContext {
	r.vm.initIfNeeded()
	return r.cx
}

// Global returns the global object of r.
func (r *Realm) Global() Value {
	return fromEngine(r.context().GlobalObject())
}

// ValueOf is like [VM.ValueOf], but the objects that it creates belong to r.
func (r *Realm) ValueOf(x any) Value {
	return fromEngine(r.vm.valueOf(r.context(), x))
}

// FuncOf is like [VM.FuncOf], but the function belongs to and runs in r.
// If f returns a [*ScriptError], directly or wrapped, the function throws its stored value,
// which may be a primitive or an object from any realm of the same VM. For any other
// non-nil error, the function throws a new Error object in r.
func (r *Realm) FuncOf(name string, f Func) Value {
	return fromEngine(funcOf(r.context(), name, f))
}

// NewArrayBuffer is like [VM.NewArrayBuffer], but the ArrayBuffer belongs to r.
func (r *Realm) NewArrayBuffer(b []byte) Value {
	if len(b) > engine.MaxBufferLength {
		panic("ekuma: NewArrayBuffer: the slice is too long for an ArrayBuffer")
	}
	cx := r.context()
	proto := cx.Intrinsic(engine.IntrinsicArrayBufferPrototype)
	return fromEngine(storage.ObjectValue(cx.NewHostArrayBufferObject(proto, b)))
}

// NewPromise returns a new pending promise of r and the resolver that settles it.
func (r *Realm) NewPromise() (Value, *PromiseResolver) {
	cx := r.context()
	c := cx.NewPromiseCapability(storage.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
	return fromEngine(c.Promise()), &PromiseResolver{
		realm: r,
		c:     c,
	}
}

// ParseScript parses src as the source text of a script of r. name identifies the script in error
// messages and stack traces, and in the [ModuleRequest.ReferrerName] of its import() calls.
// The error is a [*SyntaxError] when src is not a valid script.
func (r *Realm) ParseScript(name, src string) (*Script, error) {
	return r.parseScript(name, wtf8.FromValidUTF8(src))
}

func (r *Realm) parseScript(name string, src wtf8.String) (*Script, error) {
	es, err := r.context().ParseScript(name, src)
	if err != nil {
		return nil, publicError(err)
	}
	return &Script{
		vm: r.vm,
		s:  es,
	}, nil
}

// BindScript returns the script of u for r: the script that [Realm.ParseScript] of r returns
// for the name and source text of u.
func (r *Realm) BindScript(u *UnboundScript) *Script {
	es := r.context().BindScript(u.s)
	return &Script{
		vm: r.vm,
		s:  es,
	}
}

// ParseModule parses src as the source text of a module of r. name identifies the module in error
// messages and stack traces, and in the [ModuleRequest.ReferrerName] of its imports.
// The error is a [*SyntaxError] when src is not a valid module.
func (r *Realm) ParseModule(name, src string) (*Module, error) {
	m := r.newModule(name)
	em, err := r.context().ParseModule(name, src, m)
	if err != nil {
		return nil, publicError(err)
	}
	m.m = em
	return m, nil
}

// ParseJSONModule returns a JSON module of r: a module whose only export is default, with the
// value returned by JSON.parse in r for src. The error is a [*ScriptError] holding a JavaScript
// SyntaxError when src is not valid JSON.
func (r *Realm) ParseJSONModule(name, src string) (*Module, error) {
	m := r.newModule(name)
	em, err := r.context().ParseJSONModule(src, m)
	if err != nil {
		return nil, publicError(err)
	}
	m.m = em
	return m, nil
}

// NewSyntheticModule returns a module of r whose only export is default, with the value v.
// It panics if v belongs to another VM.
func (r *Realm) NewSyntheticModule(name string, v Value) *Module {
	m := r.newModule(name)
	m.m = r.context().CreateDefaultExportSyntheticModule(r.vm.acceptValue(v), m)
	return m
}

// newModule returns a [Module] of r without its module record.
func (r *Realm) newModule(name string) *Module {
	return &Module{
		vm:    r.vm,
		realm: r,
		name:  name,
	}
}
