// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// Realm is a realm of a [VM]: a global object with its own standard built-in objects.
//
// The realms of a VM share its promise jobs, settings and other state, except for their
// global objects and built-in objects. Values may pass between realms. A realm is not a
// security boundary: an object of a realm gives access to the built-in objects of its realm.
type Realm struct {
	vm *VM
	r  *engine.Realm
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
	return vm.realmOf(vm.e.CreateRealm(func() {
		builtin.Install(vm.e)
	}))
}

// realmOf returns the [Realm] of the engine realm r.
func (vm *VM) realmOf(r *engine.Realm) *Realm {
	if pr, ok := vm.e.RealmHostDefined(r).(*Realm); ok {
		return pr
	}
	pr := &Realm{
		vm: vm,
		r:  r,
	}
	vm.e.SetRealmHostDefined(r, pr)
	return pr
}

// in runs f with r as the current realm of its VM.
func (r *Realm) in(f func()) {
	r.vm.initIfNeeded()
	r.vm.e.InRealm(r.r, f)
}

// Global returns the global object of r.
func (r *Realm) Global() Value {
	var v engine.Value
	r.in(func() {
		v = r.vm.e.GlobalObject()
	})
	return fromEngine(v)
}

// ValueOf is like [VM.ValueOf], but the objects that it creates belong to r.
func (r *Realm) ValueOf(x any) Value {
	r.vm.initIfNeeded()
	return fromEngine(r.vm.valueOf(r, x))
}

// FuncOf is like [VM.FuncOf], but the function belongs to r: it runs in r, and the errors
// that it throws are objects of r.
func (r *Realm) FuncOf(name string, f Func) Value {
	var v engine.Value
	r.in(func() {
		v = r.vm.funcOf(r, name, f)
	})
	return fromEngine(v)
}

// NewArrayBuffer is like [VM.NewArrayBuffer], but the ArrayBuffer belongs to r.
func (r *Realm) NewArrayBuffer(b []byte) Value {
	if len(b) > engine.MaxBufferLength {
		panic("ekuma: NewArrayBuffer: the slice is too long for an ArrayBuffer")
	}
	var v engine.Value
	r.in(func() {
		proto := r.vm.e.Intrinsic(engine.IntrinsicArrayBufferPrototype)
		v = engine.ObjectValue(r.vm.e.NewHostArrayBufferObject(proto, b))
	})
	return fromEngine(v)
}

// ParseScript parses src as the source text of a script of r. name identifies the script in error
// messages and stack traces, and in the [ModuleRequest.ReferrerName] of its import() calls.
// The error is a [*SyntaxError] when src is not a valid script.
func (r *Realm) ParseScript(name, src string) (*Script, error) {
	var es *engine.Script
	var err error
	r.in(func() {
		es, err = r.vm.e.ParseScript(name, src)
	})
	if err != nil {
		return nil, r.vm.publicError(err)
	}
	return &Script{
		vm: r.vm,
		s:  es,
	}, nil
}

// ParseModule parses src as the source text of a module of r. name identifies the module in error
// messages and stack traces, and in the [ModuleRequest.ReferrerName] of its imports.
// The error is a [*SyntaxError] when src is not a valid module.
func (r *Realm) ParseModule(name, src string) (*Module, error) {
	m := r.newModule(name)
	var em *engine.Module
	var err error
	r.in(func() {
		em, err = r.vm.e.ParseModule(name, src, m)
	})
	if err != nil {
		return nil, r.vm.publicError(err)
	}
	m.m = em
	return m, nil
}

// ParseJSONModule returns a JSON module of r: a module whose only export is default, with the
// value returned by JSON.parse in r for src. The error is a [*ScriptError] holding a JavaScript
// SyntaxError when src is not valid JSON.
func (r *Realm) ParseJSONModule(name, src string) (*Module, error) {
	m := r.newModule(name)
	var em *engine.Module
	var err error
	r.in(func() {
		em, err = r.vm.e.ParseJSONModule(src, m)
	})
	if err != nil {
		return nil, r.vm.publicError(err)
	}
	m.m = em
	return m, nil
}

// NewSyntheticModule returns a module of r whose only export is default, with the value v.
// It panics if v belongs to another VM.
func (r *Realm) NewSyntheticModule(name string, v Value) *Module {
	m := r.newModule(name)
	r.in(func() {
		m.m = r.vm.e.CreateDefaultExportSyntheticModule(r.vm.acceptValue(v), m)
	})
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
