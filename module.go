// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"errors"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Module is a module record of a [VM].
//
// Running a module involves three steps: [Module.LoadRequestedModules], [Module.Link] and
// [Module.Evaluate]. Each step first completes any unfinished prerequisites.
// [Module.Namespace] performs the first two steps. These methods may be called in any order;
// completed steps are not repeated. A call from inside the module loader that
// has to load a module that is still being loaded returns a [*ScriptError] holding a TypeError.
type Module struct {
	vm    *VM
	realm *Realm
	m     *engine.Module
	name  string
}

// ModuleRequest is a request to load a module.
type ModuleRequest struct {
	// Realm is the realm of the code requesting the import.
	Realm *Realm

	// Referrer is the importing module, or nil if the import is not requested by a module.
	Referrer *Module

	// ReferrerName is the name of the importing script or module, or empty.
	ReferrerName string

	// Specifier is the module specifier of the import.
	Specifier string

	// Attributes are the import attributes of the with clause.
	Attributes map[string]string
}

// SetModuleLoader sets f as the implementation of HostLoadImportedModule, which import
// declarations and import() use. f must return a [*Module] belonging to vm and return the
// same [*Module] for repeated requests for the same module.
//
// The error of f is thrown the way the error of a [Func] is, except that a [*SyntaxError]
// throws a JavaScript SyntaxError. import() rejects its promise with it, and the methods of
// [Module] return it as a [*ScriptError].
//
// Unsupported attributes cause a SyntaxError when loading a static import and cause import()
// to reject its promise with a TypeError.
//
// An import fails with a TypeError when there is no loader, when f returns a nil [*Module]
// without an error, or when the module is being loaded by a call that has not finished.
// A nil f removes the loader. The loader applies to subsequent module loads. Already loaded
// modules retain their loaded dependencies.
func (vm *VM) SetModuleLoader(f func(req ModuleRequest) (*Module, error)) {
	vm.initIfNeeded()
	if f == nil {
		vm.e.SetHostLoadImportedModule(nil)
		return
	}
	vm.e.SetHostLoadImportedModule(func(cx *engine.ExecutionContext, referrer *engine.Module, file string, referrerContext *engine.ExecutionContext, req engine.ModuleRequest) (*engine.Module, error) {
		r := ModuleRequest{
			Realm:        vm.realmOf(referrerContext),
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
			return nil, vm.engineError(err)
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
func (vm *VM) engineError(err error) error {
	if se, ok := errors.AsType[*SyntaxError](err); ok {
		return &engine.SyntaxError{
			File:    se.File,
			Line:    se.Line,
			Message: se.Message,
		}
	}
	if ex, ok := errors.AsType[*ScriptError](err); ok {
		return &storage.Error{Value: vm.acceptValue(ex.Value)}
	}
	return err
}

// SetImportMeta sets f as the implementation of HostGetImportMetaProperties. On first use of
// import.meta in a module, f supplies its properties, which are converted with [Realm.ValueOf]
// in the module's realm. A nil f supplies no properties.
//
// A reentrant access during f sees the same object, which receives the properties after f
// returns. If f panics, the next access retries initialization on that object.
func (vm *VM) SetImportMeta(f func(m *Module) map[string]any) {
	vm.initIfNeeded()
	if f == nil {
		vm.e.SetHostGetImportMetaProperties(nil)
		return
	}
	vm.e.SetHostGetImportMetaProperties(func(cx *engine.ExecutionContext, m *engine.Module) []engine.ImportMetaProperty {
		pm := m.HostDefined().(*Module)
		props := f(pm)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		list := make([]engine.ImportMetaProperty, 0, len(keys))
		for _, k := range keys {
			list = append(list, engine.ImportMetaProperty{
				Key:   cx.ToPropertyKey(cx.StringFromGo(k)),
				Value: vm.valueOf(pm.realm.cx, props[k]),
			})
		}
		return list
	})
}

// ParseModule is [Realm.ParseModule] of the initial realm.
func (vm *VM) ParseModule(name, src string) (*Module, error) {
	vm.initIfNeeded()
	return vm.initialRealm.ParseModule(name, src)
}

// ParseJSONModule is [Realm.ParseJSONModule] of the initial realm.
func (vm *VM) ParseJSONModule(name, src string) (*Module, error) {
	vm.initIfNeeded()
	return vm.initialRealm.ParseJSONModule(name, src)
}

// NewSyntheticModule is [Realm.NewSyntheticModule] of the initial realm.
func (vm *VM) NewSyntheticModule(name string, v Value) *Module {
	vm.initIfNeeded()
	return vm.initialRealm.NewSyntheticModule(name, v)
}

// Name returns the name that m was created with.
func (m *Module) Name() string {
	return m.name
}

// LoadRequestedModules loads the modules that m and its dependencies import, with the loader
// of the VM. [Module.Link], [Module.Evaluate] and [Module.Namespace] call it, so calling it
// is needed only to load before linking. A failed import returns a [*ScriptError] holding
// the value thrown as described by [VM.SetModuleLoader]. When the loader returns an ordinary
// Go error, [errors.Is] matches that error through the returned [*ScriptError].
func (m *Module) LoadRequestedModules() error {
	m.vm.initIfNeeded()
	return publicError(m.m.LoadRequestedModules())
}

// Link links m and its dependencies: it resolves their imports without evaluating module code.
// It calls [Module.LoadRequestedModules] first. [Module.Evaluate] and [Module.Namespace] call
// it, so calling it is needed only to link before evaluating. The error is the error of
// [Module.LoadRequestedModules], or a [*ScriptError] holding a JavaScript SyntaxError for an import
// that does not resolve. Link may be retried after a failure.
func (m *Module) Link() error {
	m.vm.initIfNeeded()
	return publicError(m.m.Link())
}

// Evaluate evaluates m and its dependencies and returns a promise for the evaluation.
// It calls [Module.Link] first. An exception thrown by a module rejects the promise.
// Evaluating a source text module again returns the same promise. A synthetic or JSON
// module returns a new fulfilled promise each time.
//
// Pending promise jobs run before the outermost Go call that executes JavaScript returns.
// An Evaluate call made from a host callback can return with jobs still pending.
// An interruption discards pending jobs.
//
// The error is the error of [Module.Link]. When ctx is done, the evaluation stops at its next
// loop iteration or call, and the error is the cause of ctx. Any call that a context stops
// ends the evaluations in progress with an Error, which rejects their promises:
// [ScriptError.Unwrap] of a [*ScriptError] holding such a reason returns the cause of ctx.
// Internal operation failures return an [*InternalError].
func (m *Module) Evaluate(ctx context.Context) (Value, error) {
	vm := m.vm
	vm.initIfNeeded()
	return vm.run(ctx, func() (storage.Value, error) {
		return m.m.Evaluate()
	})
}

// Namespace returns the module namespace object of m. It calls [Module.Link] first, and the
// error is the error of [Module.Link]. Before m is evaluated, reading an export that is not
// initialized yet throws a ReferenceError.
func (m *Module) Namespace() (Value, error) {
	m.vm.initIfNeeded()
	v, err := m.m.GetModuleNamespace()
	return fromEngine(v), publicError(err)
}
