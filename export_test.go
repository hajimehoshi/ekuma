// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"errors"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// SetTimeZone sets the time zone of local time in vm to loc.
func SetTimeZone(vm *VM, loc *time.Location) {
	vm.initIfNeeded()
	vm.e.SetDefaultTimeZone(datetime.LocationZone(loc))
}

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

// Module is a module record.
type Module struct {
	m *engine.Module
}

// ParseModule parses src as the source text of a module named file.
// The error is a *SyntaxError.
func ParseModule(vm *VM, file, src string) (*Module, error) {
	vm.initIfNeeded()
	m := &Module{}
	em, err := vm.e.ParseModule(file, src, m)
	if err != nil {
		return nil, vm.publicError(err)
	}
	m.m = em
	return m, nil
}

// NewDefaultModule returns a module whose default export is v.
func NewDefaultModule(vm *VM, v Value) *Module {
	vm.initIfNeeded()
	m := &Module{}
	m.m = vm.e.CreateDefaultExportSyntheticModule(v.engine(), m)
	return m
}

// ModuleLoader returns the module that specifier names for the script or module named referrer,
// which is empty for code that has neither. attrs are the import attributes.
// The error is an *Error, a *SyntaxError, or another error, which is thrown as an Error.
type ModuleLoader func(referrer, specifier string, attrs map[string]string) (*Module, error)

// SetModuleLoader makes vm load the modules that it imports with load.
func SetModuleLoader(vm *VM, load ModuleLoader) {
	vm.initIfNeeded()
	vm.e.SetHostLoadImportedModule(func(e *engine.VM, referrer *engine.Module, file string, req engine.ModuleRequest) (*engine.Module, error) {
		attrs := map[string]string{}
		for _, a := range req.Attributes {
			attrs[wtf8.ToValidUTF8(a.Key)] = wtf8.ToValidUTF8(a.Value)
		}
		m, err := load(file, wtf8.ToValidUTF8(req.Specifier), attrs)
		if err != nil {
			if se, ok := errors.AsType[*SyntaxError](err); ok {
				return nil, &engine.SyntaxError{
					File:    se.File,
					Line:    se.Line,
					Message: se.Message,
				}
			}
			if ex, ok := errors.AsType[*Error](err); ok {
				return nil, &engine.Error{Value: ex.Value.engine()}
			}
			return nil, err
		}
		return m.m, nil
	})
}

// SetImportMeta makes vm give the import.meta object of a module the properties that props returns.
func SetImportMeta(vm *VM, props func(m *Module) map[string]any) {
	vm.initIfNeeded()
	vm.e.SetHostGetImportMetaProperties(func(e *engine.VM, m *engine.Module) []engine.ImportMetaProperty {
		var list []engine.ImportMetaProperty
		for k, v := range props(m.HostDefined().(*Module)) {
			list = append(list, engine.ImportMetaProperty{
				Key:   engine.NamedKey(e.Intern(k)),
				Value: vm.valueOf(v),
			})
		}
		return list
	})
}

// RunModule loads the modules that m imports, links m and evaluates it, and returns the promise
// of the evaluation. The error is a *SyntaxError or an *Error when loading or linking fails, and
// the cause of ctx when ctx is done.
func RunModule(ctx context.Context, vm *VM, m *Module) (Value, error) {
	vm.initIfNeeded()
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.e.RunModule(m.m)
	})
}
