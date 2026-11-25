// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/types"
	"slices"
	"testing"
)

// codeTypes are the types whose values running code takes or leads to.
var codeTypes = []string{
	"agent", "realm", "ExecutionContext", "Host",
	"frame", "nativeCall", "generator", "asyncState",
	"job", "Module", "waiterData",
	"quietJobs", "Script", "scriptOrModule", "asyncWaiters", "waiterHeap", "mailbox", "moduleEvaluations", "jobQueue", "rejectionTracker",
}

// quietEngineOperations are the operations of quiet.Context that reach the engine, with the reasons.
// TestQuietContextBoundary checks them.
var quietEngineOperations = map[string]string{
	"observePush": "samples the running frame",
}

// signatureExceptions are the other functions that mention values of the types without taking or
// returning any, with the reasons.
var signatureExceptions = map[string]string{
	"nativeFastOf":              "reads the native table, whose functions take an execution context",
	"RegisterPlainIteratorNext": "registers function literals that take an execution context",
	"addNativeFast":             "fills the native table",
	"isSyncGenerator":           "reads the kind of a generator",
}

// TestQuietSignatures tests that the functions that neither take nor return an execution context
// or another value that running code needs do not get one elsewhere. All the functions of the engine
// without such a value in their signatures count, and all the functions with a quiet.Context, which
// must have no such value in their signatures.
//
// As the test follows signatures, it sees neither what a function value captures nor what an
// interface value holds, except that it rejects assertions to the types. TestQuietContextBoundary
// sees the calls of both.
func TestQuietSignatures(t *testing.T) {
	prog := checkProgram(t, "../../...")
	quietContext := prog.typ(t, "Context")
	var forbidden []*types.Named
	for _, name := range codeTypes {
		forbidden = append(forbidden, prog.typ(t, name))
	}
	// leads returns the name of a type in forbidden that typ names, or an empty string, and find
	// does so without known, the results of leads.
	var leads, find func(typ types.Type) string
	known := map[types.Type]string{}
	tuple := func(tup *types.Tuple) string {
		for v := range tup.Variables() {
			if name := leads(v.Type()); name != "" {
				return name
			}
		}
		return ""
	}
	leads = func(typ types.Type) string {
		if typ == nil {
			return ""
		}
		if name, ok := known[typ]; ok {
			return name
		}
		name := find(typ)
		known[typ] = name
		return name
	}
	find = func(typ types.Type) string {
		for _, f := range forbidden {
			if isType(typ, f) {
				return f.Obj().Name()
			}
		}
		switch typ := types.Unalias(typ).(type) {
		case *types.Pointer:
			return leads(typ.Elem())
		case *types.Slice:
			return leads(typ.Elem())
		case *types.Array:
			return leads(typ.Elem())
		case *types.Chan:
			return leads(typ.Elem())
		case *types.Map:
			if name := leads(typ.Key()); name != "" {
				return name
			}
			return leads(typ.Elem())
		case *types.Tuple:
			return tuple(typ)
		case *types.Signature:
			if name := tuple(typ.Params()); name != "" {
				return name
			}
			return tuple(typ.Results())
		case *types.Struct:
			for f := range typ.Fields() {
				if name := leads(f.Type()); name != "" {
					return name
				}
			}
		case *types.Named:
			for arg := range typ.TypeArgs().Types() {
				if name := leads(arg); name != "" {
					return name
				}
			}
			// The fields of a declared struct type are judged where they are selected, and the
			// methods of an interface where they are called.
			switch typ.Underlying().(type) {
			case *types.Struct, *types.Interface, *types.Basic:
				return ""
			}
			// A type can name itself.
			known[typ] = ""
			return leads(typ.Underlying())
		}
		return ""
	}
	signature := func(sig *types.Signature) string {
		if r := sig.Recv(); r != nil {
			if name := leads(r.Type()); name != "" {
				return name
			}
		}
		return leads(sig)
	}
	hasQuiet := func(sig *types.Signature) bool {
		isQuiet := func(v *types.Var) bool {
			return isType(v.Type(), quietContext)
		}
		if r := sig.Recv(); r != nil && isQuiet(r) {
			return true
		}
		return slices.ContainsFunc(slices.Collect(sig.Params().Variables()), isQuiet)
	}

	exceptions := map[*types.Func]string{}
	for name := range quietEngineOperations {
		exceptions[prog.implementation(t, name)] = name
	}
	for name := range signatureExceptions {
		exceptions[prog.implementation(t, name)] = name
	}
	used := map[string]bool{}

	for _, p := range prog.pkgs {
		info := &p.info
		// check checks body, which belongs to a function of the rule if covered is set. mention
		// takes what breaks the rule.
		var check func(body ast.Node, covered bool, mention func(n ast.Node, what string))
		check = func(body ast.Node, covered bool, mention func(n ast.Node, what string)) {
			ast.Inspect(body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok && lit.Body != body {
					sig := info.TypeOf(lit).(*types.Signature)
					name := leads(sig)
					switch {
					case hasQuiet(sig):
						if name != "" {
							t.Errorf("%v: function with a quiet.Context has %s in its signature", p.fset.Position(lit.Pos()), name)
						}
						check(lit.Body, true, func(n ast.Node, what string) {
							t.Errorf("%v: function literal with a quiet.Context mentions %s", p.fset.Position(n.Pos()), what)
						})
					case name != "":
						// The literal runs with what its callers pass.
						check(lit.Body, false, mention)
					default:
						check(lit.Body, covered, mention)
					}
					return false
				}
				if !covered {
					return true
				}
				checkType := func(typ ast.Expr, what string) {
					if name := leads(info.TypeOf(typ)); name != "" {
						mention(typ, what+" "+name)
					} else if _, ok := info.TypeOf(typ).Underlying().(*types.Signature); ok {
						mention(typ, what+" a function type")
					}
				}
				switch n := n.(type) {
				case *ast.TypeAssertExpr:
					if n.Type != nil {
						checkType(n.Type, "an assertion to")
					}
				case *ast.IndexExpr:
					if info.Types[n.Index].IsType() {
						checkType(n.Index, "a type argument")
					}
				case *ast.IndexListExpr:
					for _, e := range n.Indices {
						checkType(e, "a type argument")
					}
				case *ast.CaseClause:
					for _, e := range n.List {
						if info.Types[e].IsType() {
							checkType(e, "an assertion to")
						}
					}
				}
				if expr, ok := n.(ast.Expr); ok {
					tv := info.Types[expr]
					if id, ok := n.(*ast.Ident); ok && tv.Type == nil {
						if obj := info.Uses[id]; obj != nil {
							if _, ok := obj.(*types.TypeName); !ok {
								tv.Type = obj.Type()
							}
						}
					}
					if tv.Type == nil || tv.IsType() {
						return true
					}
					if name := leads(tv.Type); name != "" {
						mention(n, name)
					}
				}
				return true
			})
		}
		for _, file := range p.files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					// The initial values of variables have no context to run code with.
					check(decl, false, func(ast.Node, string) {})
					continue
				}
				if fn.Body == nil {
					continue
				}
				f := info.Defs[fn.Name].(*types.Func)
				sig := f.Type().(*types.Signature)
				name := signature(sig)
				quiet := hasQuiet(sig)
				// An initialization function runs before there is an agent.
				initializes := sig.Recv() == nil && f.Name() == "init"
				if quiet && name != "" {
					t.Errorf("%v: function with a quiet.Context has %s in its signature", p.fset.Position(fn.Pos()), name)
				}
				reported := false
				check(fn.Body, quiet || p.isEngine() && name == "" && !initializes, func(n ast.Node, what string) {
					if exception := exceptions[f]; exception != "" {
						used[exception] = true
						return
					}
					if !reported {
						t.Errorf("%v: %s mentions %s, which its signature does not name", p.fset.Position(n.Pos()), f.FullName(), what)
					}
					reported = true
				})
			}
		}
	}
	for _, name := range exceptions {
		if !used[name] {
			t.Errorf("%s needs no exception", name)
		}
	}
}
