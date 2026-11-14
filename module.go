// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"errors"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Module is a module record of a [VM].
//
// Running a module takes three steps, each of which takes the earlier ones first when they
// have not completed: [Module.LoadRequestedModules], [Module.Link] and [Module.Evaluate].
// [Module.Namespace] takes the first two. Any order of calls is valid, and a step that has
// completed does nothing when it is taken again.
type Module struct {
	vm   *VM
	m    *engine.Module
	name string
}

// ModuleRequest is a request to load a module.
type ModuleRequest struct {
	// Referrer is the importing module, or nil for a script or for code that has neither.
	Referrer *Module

	// ReferrerName is the name of the importing script or module, or empty.
	ReferrerName string

	// Specifier is the module specifier of the import.
	Specifier string

	// Attributes are the import attributes of the with clause.
	Attributes map[string]string
}

// SetModuleLoader sets f as the implementation of HostLoadImportedModule, which import
// declarations and import() use. f must return the same [*Module] for the same module, and
// a [*Module] of vm.
//
// The error of f is thrown the way the error of a [Func] is, except that a [*SyntaxError]
// throws a JavaScript SyntaxError. import() rejects its promise with it, and the methods of
// [Module] return it as an [*Error].
//
// Without a loader, or with a nil f, every import fails with a TypeError. The loader applies
// to the modules that are loaded after the call: a module that loaded keeps the modules that
// it loaded.
func (vm *VM) SetModuleLoader(f func(req ModuleRequest) (*Module, error)) {
	vm.initIfNeeded()
	if f == nil {
		vm.e.SetHostLoadImportedModule(nil)
		return
	}
	vm.e.SetHostLoadImportedModule(func(e *engine.VM, referrer *engine.Module, file string, req engine.ModuleRequest) (*engine.Module, error) {
		r := ModuleRequest{
			ReferrerName: file,
			Specifier:    wtf8.ToValidUTF8(req.Specifier),
			Attributes:   map[string]string{},
		}
		if referrer != nil {
			r.Referrer = referrer.HostDefined().(*Module)
		}
		for _, a := range req.Attributes {
			r.Attributes[wtf8.ToValidUTF8(a.Key)] = wtf8.ToValidUTF8(a.Value)
		}
		m, err := f(r)
		if err != nil {
			return nil, engineError(err)
		}
		if m == nil {
			return nil, nil
		}
		if m.vm != vm {
			panic("ekuma: module loader returned a Module of another VM")
		}
		return m.m, nil
	})
}

// engineError converts an error of the host to the error types of the engine.
func engineError(err error) error {
	if se, ok := errors.AsType[*SyntaxError](err); ok {
		return &engine.SyntaxError{
			File:    se.File,
			Line:    se.Line,
			Message: se.Message,
		}
	}
	if ex, ok := errors.AsType[*Error](err); ok {
		return &engine.Error{Value: ex.Value.engine()}
	}
	return err
}

// SetImportMeta sets f as the implementation of HostGetImportMetaProperties: f returns the
// properties, converted with [VM.ValueOf], that the import.meta object of the module m gets
// when it is created. The object is created when the code of m first uses it, so f applies to
// the import.meta objects that are created after the call. A nil f gives no properties.
func (vm *VM) SetImportMeta(f func(m *Module) map[string]any) {
	vm.initIfNeeded()
	if f == nil {
		vm.e.SetHostGetImportMetaProperties(nil)
		return
	}
	vm.e.SetHostGetImportMetaProperties(func(e *engine.VM, m *engine.Module) []engine.ImportMetaProperty {
		props := f(m.HostDefined().(*Module))
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		list := make([]engine.ImportMetaProperty, 0, len(keys))
		for _, k := range keys {
			list = append(list, engine.ImportMetaProperty{
				Key:   e.ToPropertyKey(e.StringFromGo(k)),
				Value: vm.valueOf(props[k]),
			})
		}
		return list
	})
}

// ParseModule parses src as the source text of a module. name names the module in error
// messages and stack traces, and in the [ModuleRequest.ReferrerName] of its imports.
// The error is a [*SyntaxError] when src is not a valid module.
func (vm *VM) ParseModule(name, src string) (*Module, error) {
	vm.initIfNeeded()
	m := &Module{
		vm:   vm,
		name: name,
	}
	em, err := vm.e.ParseModule(name, src, m)
	if err != nil {
		return nil, vm.publicError(err)
	}
	m.m = em
	return m, nil
}

// ParseJSONModule returns a JSON module: a module whose only export is default, with the
// value that JSON.parse returns for src. The error is an [*Error] holding a JavaScript
// SyntaxError when src is not valid JSON.
func (vm *VM) ParseJSONModule(name, src string) (*Module, error) {
	vm.initIfNeeded()
	m := &Module{
		vm:   vm,
		name: name,
	}
	em, err := vm.e.ParseJSONModule(src, m)
	if err != nil {
		return nil, vm.publicError(err)
	}
	m.m = em
	return m, nil
}

// NewSyntheticModule returns a module whose only export is default, with the value v.
func (vm *VM) NewSyntheticModule(name string, v Value) *Module {
	vm.initIfNeeded()
	m := &Module{
		vm:   vm,
		name: name,
	}
	m.m = vm.e.CreateDefaultExportSyntheticModule(v.engine(), m)
	return m
}

// Name returns the name that m was created with.
func (m *Module) Name() string {
	return m.name
}

// LoadRequestedModules loads the modules that m and its dependencies import, with the loader
// of the VM (see [VM.SetModuleLoader]). [Module.Link], [Module.Evaluate] and [Module.Namespace]
// call it, so calling it is needed only to load before linking. The error is the error of the
// loader, or an [*Error] holding a TypeError when there is no loader.
func (m *Module) LoadRequestedModules() error {
	m.vm.initIfNeeded()
	return m.vm.publicError(m.vm.e.LoadRequestedModules(m.m))
}

// Link links m and its dependencies: it resolves their imports, which runs no JavaScript.
// It calls [Module.LoadRequestedModules] first. [Module.Evaluate] and [Module.Namespace] call
// it, so calling it is needed only to link before evaluating. The error is the error of
// [Module.LoadRequestedModules], or an [*Error] holding a JavaScript SyntaxError for an import
// that does not resolve. A failed Link can be taken again.
func (m *Module) Link() error {
	m.vm.initIfNeeded()
	return m.vm.publicError(m.vm.e.Link(m.m))
}

// Evaluate evaluates m and its dependencies, runs the pending jobs, and returns the promise
// of the evaluation. It calls [Module.Link] first. An exception of a module rejects the
// promise, and evaluating m again returns the same promise.
//
// The error is the error of [Module.Link]. When ctx is done, the evaluation stops at its next
// loop iteration or call, and the error is the cause of ctx. Any call that a context stops
// ends the evaluations in progress with an Error, which rejects their promises:
// [Error.Unwrap] of an [*Error] holding such a reason returns the cause of ctx.
func (m *Module) Evaluate(ctx context.Context) (Value, error) {
	vm := m.vm
	vm.initIfNeeded()
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.e.Evaluate(m.m)
	})
}

// Namespace returns the module namespace object of m. It calls [Module.Link] first, and the
// error is the error of [Module.Link]. Before m is evaluated, reading an export that is not
// initialized yet throws a ReferenceError.
func (m *Module) Namespace() (Value, error) {
	m.vm.initIfNeeded()
	v, err := m.vm.e.GetModuleNamespace(m.m)
	return fromEngine(v), m.vm.publicError(err)
}
