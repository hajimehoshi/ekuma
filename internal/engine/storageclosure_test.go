// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// callableExceptions are the fields and parameters of TestQuietCallables that can hold a function or
// a value with methods, with the reasons. The caller of a function answers for what it passes.
var callableExceptions = map[string]string{
	"storage.ErrorData.GoErr":                "kept for the host",
	"storage.Poller.InterruptCause":          "kept for the host",
	"storage.InterruptSignal.Cause":          "kept for the host",
	"regexp.Matcher.poller":                  "quiet passes the context of each match",
	"(*storage.sweptTable[K, V]).put.keep":   "the storage package passes it",
	"(*storage.sweptTable[K, V]).sweep.keep": "the storage package passes it",
	"(storage.Value).UnitIndexOf.poll":       "called during the call",
	"(storage.Value).UnitLastIndexOf.poll":   "called during the call",
	"(storage.Value).shortStringIndex.poll":  "called during the call",
	"(*storage.Object).UnitIndexOf.poll":     "called during the call",
	"(*storage.Object).UnitLastIndexOf.poll": "called during the call",
	"(*storage.Object).findUnits.poll":       "called during the call",
}

// TestQuietCallables tests that the fields, the variables and the parameters of the storage and
// quiet packages hold no function and no value of an interface with methods, by which the packages
// could call what they cannot import. The fields of the types that they lead to in the other packages
// of the module count too.
//
// The test does not see the values of interfaces without methods, for whose assertions there is
// TestQuietSignatures, nor what the types of the standard library hold.
func TestQuietCallables(t *testing.T) {
	prog := checkProgram(t, "../../...")
	found := map[string]bool{}
	seen := map[string]bool{}
	var visit func(typ types.Type, path string, pos token.Position)
	report := func(path, what string, pos token.Position) {
		path = strings.ReplaceAll(path, modulePath+"/internal/", "")
		if callableExceptions[path] == "" {
			t.Errorf("%v: %s can hold %s", pos, path, what)
		}
		found[path] = true
	}
	visit = func(typ types.Type, path string, pos token.Position) {
		switch typ := types.Unalias(typ).(type) {
		case *types.Named:
			for arg := range typ.TypeArgs().Types() {
				visit(arg, path, pos)
			}
			obj := typ.Obj()
			if _, ok := typ.Underlying().(*types.Interface); ok {
				visit(typ.Underlying(), path, pos)
				return
			}
			if obj.Pkg() == nil || !strings.HasPrefix(obj.Pkg().Path(), modulePath) || seen[typ.String()] {
				return
			}
			seen[typ.String()] = true
			visit(typ.Underlying(), obj.Pkg().Path()+"."+obj.Name(), prog.pkgs[0].fset.Position(obj.Pos()))
		case *types.Pointer:
			visit(typ.Elem(), path, pos)
		case *types.Slice:
			visit(typ.Elem(), path, pos)
		case *types.Array:
			visit(typ.Elem(), path, pos)
		case *types.Chan:
			visit(typ.Elem(), path, pos)
		case *types.Map:
			visit(typ.Key(), path, pos)
			visit(typ.Elem(), path, pos)
		case *types.Struct:
			for f := range typ.Fields() {
				if f.Name() != "_" {
					visit(f.Type(), path+"."+f.Name(), pos)
				}
			}
		case *types.Signature:
			report(path, "a function", pos)
		case *types.Interface:
			if typ.NumMethods() > 0 {
				report(path, "a value with methods", pos)
			}
		}
	}
	for _, p := range prog.engine {
		if p.path == enginePath {
			continue
		}
		for id, obj := range p.info.Defs {
			pos := p.fset.Position(id.Pos())
			switch obj := obj.(type) {
			case *types.TypeName:
				if !obj.IsAlias() {
					visit(obj.Type(), p.path+"."+obj.Name(), pos)
				}
			case *types.Var:
				if obj.Parent() == p.pkg.Scope() {
					visit(obj.Type(), p.path+"."+obj.Name(), pos)
				}
			case *types.Func:
				for v := range obj.Signature().Params().Variables() {
					visit(v.Type(), obj.FullName()+"."+v.Name(), pos)
				}
			}
		}
	}
	for path := range callableExceptions {
		if !found[path] {
			t.Errorf("%s needs no exception", path)
		}
	}
}
