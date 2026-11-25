// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"
)

// storageRoots are the types whose reachable types are the state of values and properties.
var storageRoots = []string{"Object", "Value", "shape"}

// nonStorageTypes are the types that storage must not reach. A type that leads to one of these
// needs no entry.
var nonStorageTypes = []string{
	"agent", "realm", "ExecutionContext", "Context", "Host",
	"frame", "nativeCall", "generator", "asyncState",
	"job", "Module", "waiterData",
}

// TestStorageClosure tests that the types of storage do not lead to the rest of the engine.
//
// A type leads to the types of its fields and elements, its type arguments, and the types in its
// function signatures and in the methods of its interfaces, also in other packages. The types that
// an interface value holds at run time, such as those of Object.ext, and the variables that a
// function value captures are invisible to the test.
//
// Renaming a type or a field that the lists name, or moving a type out of enginePackages, fails the
// test until the lists follow. A type can move between enginePackages freely. A type that is
// split off from one in nonStorageTypes counts as storage unless it leads to one of them.
func TestStorageClosure(t *testing.T) {
	prog := checkProgram(t, enginePackages...)
	var roots, forbidden []*types.Named
	for _, name := range storageRoots {
		roots = append(roots, prog.typ(t, name))
	}
	for _, name := range nonStorageTypes {
		forbidden = append(forbidden, prog.typ(t, name))
	}
	for _, path := range forbiddenEdges(roots, forbidden) {
		t.Errorf("storage leads to the engine: %s", path)
	}
	if len(forbiddenEdges([]*types.Named{prog.typ(t, "nativeFast")}, forbidden)) == 0 {
		t.Error("nativeFast, which is no storage, does not lead to the engine")
	}
}

// forbiddenEdges returns a path for each place in the types reachable from roots that names a type
// in forbidden. Paths do not continue through forbidden.
func forbiddenEdges(roots, forbidden []*types.Named) []string {
	type item struct {
		typ  *types.Named
		path string
	}
	var queue []item
	var edges []string
	// Instances of a generic type can be identical without being the same.
	seen := map[string]bool{}

	// visit follows typ, which path leads to. owner names the declared type that contains it.
	var visit func(typ types.Type, path, owner string)
	visitTuple := func(tuple *types.Tuple, path, owner string) {
		for v := range tuple.Variables() {
			visit(v.Type(), path, owner)
		}
	}
	visit = func(typ types.Type, path, owner string) {
		switch typ := types.Unalias(typ).(type) {
		case *types.Named:
			for arg := range typ.TypeArgs().Types() {
				visit(arg, path, owner)
			}
			if slices.ContainsFunc(forbidden, func(f *types.Named) bool {
				return isType(typ, f)
			}) {
				edges = append(edges, path+" -> "+typ.Obj().Name())
				return
			}
			if key := typ.String(); !seen[key] {
				seen[key] = true
				queue = append(queue, item{
					typ:  typ,
					path: path,
				})
			}
		case *types.Pointer:
			visit(typ.Elem(), path, owner)
		case *types.Slice:
			visit(typ.Elem(), path, owner)
		case *types.Array:
			visit(typ.Elem(), path, owner)
		case *types.Chan:
			visit(typ.Elem(), path, owner)
		case *types.Map:
			visit(typ.Key(), path, owner)
			visit(typ.Elem(), path, owner)
		case *types.Struct:
			for f := range typ.Fields() {
				name := owner + "." + f.Name()
				visit(f.Type(), path+" -> "+name, name)
			}
		case *types.Signature:
			visitTuple(typ.Params(), path, owner)
			visitTuple(typ.Results(), path, owner)
		case *types.Interface:
			for m := range typ.Methods() {
				name := owner + "." + m.Name()
				visit(m.Type(), path+" -> "+name, name)
			}
		}
	}
	for _, root := range roots {
		visit(root, root.Obj().Name(), "")
	}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		visit(it.typ.Underlying(), it.path, it.typ.Obj().Name())
	}
	return edges
}

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
