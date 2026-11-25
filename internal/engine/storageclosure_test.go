// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
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
	"agent", "realm", "ExecutionContext", "QuietContext", "Host",
	"frame", "nativeCall", "generator", "asyncState",
	"job", "Module", "waiterData",
}

// storageClosureExceptions are the fields that may lead from storage to the rest of the engine,
// with the reasons.
var storageClosureExceptions = map[string]string{
	"nativeInfo.fast":   "copy of the fast forms in the native table",
	"funcProto.context": "context that runs the code",
	"funcProto.script":  "referrer of imports",
}

// TestStorageClosure tests that the types of storage do not lead to the rest of the engine, except
// through storageClosureExceptions, and that each exception still leads there.
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
	exceptions := map[*types.Var]string{}
	for name := range storageClosureExceptions {
		typ, field, _ := strings.Cut(name, ".")
		exceptions[prog.field(t, typ, field)] = name
	}

	for _, path := range forbiddenEdges(roots, forbidden, exceptions) {
		t.Errorf("storage leads to the engine: %s", path)
	}
	if t.Failed() {
		// A path that is no exception can share what an exception leads to.
		return
	}
	for f, name := range exceptions {
		delete(exceptions, f)
		if len(forbiddenEdges(roots, forbidden, exceptions)) == 0 {
			t.Errorf("exception %s no longer leads to the engine", name)
		}
		exceptions[f] = name
	}
}

// forbiddenEdges returns a path for each place in the types reachable from roots, without the
// fields in excluded, that names a type in forbidden. Paths do not continue through forbidden.
func forbiddenEdges(roots, forbidden []*types.Named, excluded map[*types.Var]string) []string {
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
				if _, ok := excluded[f.Origin()]; ok {
					continue
				}
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
