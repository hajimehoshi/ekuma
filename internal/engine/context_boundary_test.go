// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"
)

func TestExecutionContextBoundary(t *testing.T) {
	prog := checkProgram(t, "../../...")
	executionContext := prog.typ(t, "ExecutionContext")
	host, realm, agent := prog.typ(t, "Host"), prog.typ(t, "realm"), prog.typ(t, "agent")

	// Only dispatch, restoration and the host entry point may observe the active binding. The
	// interpreter loop also reads it where the active context is that of the running frame.
	for _, typ := range prog.forms(executionContext) {
		if method, _, _ := types.LookupFieldOrMethod(types.NewPointer(typ), true, typ.Obj().Pkg(), "ActiveContext"); method != nil {
			t.Error("ExecutionContext exposes the active context to built-ins")
		}
	}
	prog.checkUses(t,
		useRule{
			what: "active context access",
			objs: []types.Object{prog.field(t, "agent", "active")},
			owners: []string{
				"ExecutionContext.activate", "Host.ActiveContext", "ExecutionContext.runFrame", "ExecutionContext.protect",
				"agent.exec", "agent.checkActive",
			},
		},
		useRule{
			what: "context activation",
			objs: prog.function(t, "ExecutionContext.activate"),
			owners: []string{
				"NewExecutionContext", "ExecutionContext.Run", "ExecutionContext.enter", "ExecutionContext.enterJob",
				"ExecutionContext.enterBoundary", "agent.exec", "ExecutionContext.runFrame", "ExecutionContext.callNative",
				"ExecutionContext.protect",
			},
		},
	)

	checkActiveReads(t, prog)

	for _, p := range prog.pkgs {
		if path := strings.TrimPrefix(p.path, modulePath+"/internal/"); path != "json" && path != "builtin" && !strings.HasPrefix(path, "builtin/") {
			continue
		}
		info := &p.info
		for _, file := range p.files {
			ast.Inspect(file, func(n ast.Node) bool {
				f, ok := n.(*ast.FuncLit)
				if !ok || len(f.Type.Params.List) == 0 || !isType(info.TypeOf(f.Type.Params.List[0].Type), executionContext) {
					return true
				}
				ast.Inspect(f.Body, func(n ast.Node) bool {
					id, ok := n.(*ast.Ident)
					if ok {
						if v, ok := info.Uses[id].(*types.Var); ok && !v.IsField() && v.Pos() < f.Pos() && isType(v.Type(), executionContext) {
							t.Errorf("%v: callback captures the outer execution context %s", p.fset.Position(id.Pos()), id.Name)
						}
					}
					return true
				})
				return true
			})
		}
		// Check inferred expressions too, including handles obtained from a factory.
		for expr, tv := range info.Types {
			if isType(tv.Type, host) {
				t.Errorf("%v: built-in uses the engine host handle", p.fset.Position(expr.Pos()))
			}
			if isType(tv.Type, realm) {
				t.Errorf("%v: built-in uses the raw engine realm", p.fset.Position(expr.Pos()))
			}
			if isType(tv.Type, agent) {
				t.Errorf("%v: built-in uses the raw engine agent", p.fset.Position(expr.Pos()))
			}
		}
	}
}

// TestFrameRegisterBoundary confines raw register storage to frame operations,
// interpreter fast paths, call setup and the independent runtime checks.
func TestFrameRegisterBoundary(t *testing.T) {
	prog := checkProgram(t, "../../...")
	prog.checkUses(t,
		useRule{
			what: "direct stack access",
			objs: []types.Object{prog.field(t, "agent", "stack")},
			owners: []string{
				"agent.frameRegs", "agent.placeCallArgs", "agent.extraArgs", "agent.reload", "agent.findUpval",
				"NewExecutionContext", "agent.ensureStack", "agent.releaseStack", "agent.clearStale", "agent.pushFrame", "agent.exec",
				"ExecutionContext.callOnStack", "ExecutionContext.callSlow", "ExecutionContext.callBound",
				"ExecutionContext.replaceFrame", "ExecutionContext.unbind",
				"agent.checkStep", "agent.moveStack", "agent.stackUsed", "agent.abandonStack", "agent.holdArgs", "agent.checkIdleStack",
			},
		},
		useRule{
			what: "direct regs access",
			objs: []types.Object{prog.field(t, "generator", "regs")},
			owners: []string{
				"frame.registers", "agent.placeCallArgs", "agent.exec", "agent.checkStep",
				"newGenerator", "generator.findUpval", "ExecutionContext.resume",
			},
		},
		useRule{
			what:   "direct registers access",
			objs:   prog.function(t, "frame.registers"),
			owners: []string{"agent.frameRegs", "agent.exec"},
		},
		useRule{
			what:   "direct findUpval access",
			objs:   prog.function(t, "agent.findUpval"),
			owners: []string{"agent.captureRegister"},
		},
		useRule{
			what:   "direct findUpval access",
			objs:   prog.function(t, "generator.findUpval"),
			owners: []string{"agent.captureRegister"},
		},
	)
}

// checkActiveReads tests that the interpreter loop uses the active context in place, so that no copy
// of it outlives a change of the running frame, and installs contexts only through activate.
func checkActiveReads(t *testing.T, prog *program) {
	active := prog.field(t, "agent", "active")
	var reads int
	for _, p := range prog.pkgs {
		for _, file := range p.files {
			for _, decl := range file.Decls {
				if prog.owner(&p, decl, []string{"agent.exec"}) != "agent.exec" {
					continue
				}
				var stack []ast.Node
				ast.Inspect(decl, func(n ast.Node) bool {
					if n == nil {
						stack = stack[:len(stack)-1]
						return true
					}
					if sel, ok := n.(*ast.SelectorExpr); ok && p.info.Uses[sel.Sel] == active {
						reads++
						inPlace := false
						switch parent := stack[len(stack)-1].(type) {
						case *ast.SelectorExpr:
							inPlace = true
						case *ast.BinaryExpr:
							inPlace = parent.Op == token.EQL || parent.Op == token.NEQ
						case *ast.CallExpr:
							inPlace = slices.Contains(parent.Args, ast.Expr(sel))
						}
						if !inPlace {
							t.Errorf("%v: the interpreter loop stores the active context", p.fset.Position(sel.Pos()))
						}
					}
					stack = append(stack, n)
					return true
				})
			}
		}
	}
	if reads == 0 {
		t.Error("the interpreter loop never reads the active context")
	}
}
