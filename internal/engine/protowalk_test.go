// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/types"
	"testing"
)

// The rules that a function that reads prototypes follows. An object with internal methods of its
// own, such as a proxy, a lazy object, a module namespace or a typed array, can answer a lookup
// differently than its stored properties do.
const (
	protoDeclines = "gives no answer past an object that can answer differently"
	protoAsks     = "leaves each object that can answer differently to its internal methods"
	protoOrder    = "compares objects and looks nothing up, and stops at proxies or asks them"
	protoKnown    = "relies on an earlier walk, or on a prototype of a known kind"
	protoRaw      = "reads stored properties only, for diagnostics"
	protoCopies   = "passes the prototype on or compares it without looking anything up"
)

// protoReads are the functions that read Shape.Proto, with the number of reads, except the copies
// to the same field of another shape.
var protoReads = useCounts{
	protoDeclines: {
		"realmPart.Lookup":               1,
		"realmPart.inheritsHasInstance":  1,
		"Object.inheritsMethod":          1,
		"Object.inheritsNoIndex":         2,
		"space.noArrayIteratorReturn":    1,
		"Context.delegates":              1,
		"realmPart.assignDataProperties": 1,
		"ExecutionContext.forInAhead":    1,
	},
	protoAsks: {
		"ExecutionContext.getIndex":             1,
		"ExecutionContext.getProp":              1,
		"ExecutionContext.setProp":              1,
		"ExecutionContext.HasProperty":          1,
		"ExecutionContext.applyClassDecorators": 1,
		"ExecutionContext.ProtoOf":              1,
	},
	protoOrder: {
		"ExecutionContext.SetPrototypeOf": 3,
		"defaultHasInstanceCall.run":      1,
	},
	"instanceof stops at proxies, and indexed appends check the intrinsic prototypes": {
		"agent.execFast": 5,
	},
	protoKnown: {
		"ExecutionContext.getNamedAtSite": 1,
		"propCache.rememberRead":          1,
		"realmPart.arrayProtoPlain":       2,
		"realmPart.newElementsPlain":      1,
	},
	protoRaw: {
		"diagnosticDataProperty": 1,
	},
	protoCopies: {
		"ExecutionContext.protoStart": 1,
		"realmPart.materialize":       1,
		"realmPart.dateShape":         1,
		"realmPart.regExpShape":       1,
		"realmPart.densify":           1,
		"space.endLazy":               1,
		"space.setProto":              1,
	},
}

// protoQueries are the functions that ask an object for its prototype with ProtoOf, with the
// number of calls.
var protoQueries = useCounts{
	protoAsks: {
		"ExecutionContext.ProtoOf":             1,
		"ExecutionContext.proxyGetPrototypeOf": 2,
		"ExecutionContext.proxySetPrototypeOf": 1,
		"ExecutionContext.forInNextObject":     1,
		"builtin.objectLookupAccessor":         1,
	},
	protoOrder: {
		"ExecutionContext.hasPrototype": 2,
		"builtin.objectIsPrototypeOf":   2,
	},
	protoCopies: {
		"ExecutionContext.superBase":           1,
		"ExecutionContext.BoundFunctionCreate": 1,
		"builtin.objectProtoGetter":            1,
		"builtin.objectGetPrototypeOf":         1,
		"builtin.reflectGetPrototypeOf":        1,
	},
	protoKnown: {
		"builtin/intl.initArrayToLocaleString": 1,
	},
}

// TestPrototypeWalks tests that the functions that read prototypes are those in protoReads and
// protoQueries, so that each new walk over a prototype chain is checked against the objects that it
// can pass.
//
// The test does not see a walk whose steps are in a listed function, reflection, unsafe, test
// files, the files of the ekumavalidate build that check the others, and files that the build
// constraints of the running test exclude.
func TestPrototypeWalks(t *testing.T) {
	prog := checkProgram(t, "../../...")
	proto := prog.field(t, "shape", "proto")
	prog.checkUseCounts(t, "read of a prototype", []types.Object{proto}, protoReads, func(info *types.Info, file *ast.File) map[*ast.Ident]bool {
		return storesAndCopies(info, file, proto)
	})
	queries := append(prog.function(t, "ExecutionContext.ProtoOf"), prog.function(t, "ExecutionContext.proxyGetPrototypeOf")...)
	prog.checkUseCounts(t, "query of a prototype", queries, protoQueries, nil)
}

// storesAndCopies returns the uses of field in file that store to it, or that are the value
// of the field in a composite literal.
func storesAndCopies(info *types.Info, file *ast.File, field *types.Var) map[*ast.Ident]bool {
	ids := map[*ast.Ident]bool{}
	add := func(e ast.Expr) {
		switch e := ast.Unparen(e).(type) {
		case *ast.Ident:
			ids[e] = true
		case *ast.SelectorExpr:
			ids[e.Sel] = true
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for _, l := range n.Lhs {
				add(l)
			}
		case *ast.KeyValueExpr:
			if id, ok := n.Key.(*ast.Ident); ok && used(info.Uses[id]) == field {
				add(n.Key)
				add(n.Value)
			}
		}
		return true
	})
	return ids
}
