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

// TestQuietContextBoundary checks that quiet operations cannot run script or host code
// or use the property lookup reserved for diagnostics.
func TestQuietContextBoundary(t *testing.T) {
	prog := checkProgram(t, "../../...")
	executionContext := prog.typ(t, "ExecutionContext")
	quietContext := prog.typ(t, "QuietContext")
	pairStepFunc := prog.typ(t, "IteratorPairStepFunc")

	// A quiet operation must preserve observable property lookups. Calling code and
	// looking past an exotic object through its stored properties both break that contract.
	const runsCode = "can run code"
	forbidden := map[string]string{}
	for name, reason := range map[string]string{
		"diagnosticDataProperty":        "uses diagnostic property lookup",
		"ExecutionContext.runFrame":     runsCode,
		"ExecutionContext.callNative":   runsCode,
		"ExecutionContext.invokeNative": runsCode,
		"agent.exec":                    runsCode,
		"agent.InitLazyObject":          runsCode,
	} {
		for _, f := range prog.function(t, name) {
			forbidden[f.(*types.Func).FullName()] = reason
		}
	}
	// An interruption unwinds to the host, which runs nothing in between.
	poll := prog.implementation(t, "agent.Poll").FullName()

	// Functions without a QuietContext that the interpreter and the built-ins rely on not to run
	// code: they tell that arrays and reads are plain, or read, store or take values in one step.
	var quiet []string
	for _, name := range []string{
		"ExecutionContext.DefineAccessor",
		"ExecutionContext.arrayIterationPlain",
		"ExecutionContext.arrayProtoPlain",
		"ExecutionContext.replaceOwn",
		"Object.Element",
		"Object.ElementList",
		"Object.RemoveFirstElement",
		"Object.RemoveLastElement",
		"Object.SetElement",
		"Object.plainElems",
		"agent.pairDestructuringPlain",
		"IsArrayPlain",
		"takeDataProperties",
	} {
		for _, f := range prog.function(t, name) {
			quiet = append(quiet, f.(*types.Func).FullName())
		}
	}
	listed := len(quiet)
	pairSteps := map[string]bool{}

	// Only these diagnostics may inspect stored data without applying property operations.
	storageLookups := map[string][]string{
		"diagnosticDataProperty": {
			"ExecutionContext.ExceptionDescription",
			"space.errorOf",
			"agent.newErrorData",
			"agent.typeName",
		},
	}

	// uses holds what a declaration mentions, by the full name of a function or the position of another declaration.
	uses := map[string][]string{}
	methods := map[string][]string{}
	interfaceUses := map[string][]string{}
	pos := map[string]token.Position{}
	for _, p := range prog.pkgs {
		if !strings.HasPrefix(p.path, modulePath+"/internal/") && p.path != modulePath+"/intl" && !strings.HasPrefix(p.path, modulePath+"/intl/") {
			continue
		}
		info := p.info
		builtin := !p.isEngine()
		hasQuiet := func(sig *types.Signature) bool {
			if r := sig.Recv(); r != nil && isType(r.Type(), quietContext) {
				return true
			}
			for v := range sig.Params().Variables() {
				if isType(v.Type(), quietContext) {
					return true
				}
			}
			return false
		}
		checkSignature := func(n ast.Node, sig *types.Signature) {
			for v := range sig.Params().Variables() {
				typ := v.Type()
				if list, ok := typ.Underlying().(*types.Slice); ok {
					typ = list.Elem()
				}
				if _, ok := typ.Underlying().(*types.Signature); ok {
					t.Errorf("%v: function with a QuietContext takes the function %s", p.fset.Position(n.Pos()), v.Name())
				}
			}
		}
		// checkBuiltin checks the function body, which has a QuietContext when inQuiet is set.
		var checkBuiltin func(body ast.Node, inQuiet bool)
		checkBuiltin = func(body ast.Node, inQuiet bool) {
			allowed := map[ast.Expr]bool{}
			ast.Inspect(body, func(n ast.Node) bool {
				if n, ok := n.(*ast.FuncLit); ok && n != body {
					sig := info.TypeOf(n).(*types.Signature)
					if hasQuiet(sig) {
						checkSignature(n, sig)
					}
					checkBuiltin(n.Body, inQuiet || hasQuiet(sig))
					return false
				}
				switch n := n.(type) {
				case *ast.CallExpr:
					for _, arg := range n.Args {
						allowed[ast.Unparen(arg)] = true
					}
					if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
						allowed[ast.Unparen(sel.X)] = true
					}
				}
				expr, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				tv := info.Types[expr]
				if id, ok := expr.(*ast.Ident); ok && tv.Type == nil && info.Uses[id] != nil {
					tv.Type = info.Uses[id].Type()
				}
				if sel, ok := expr.(*ast.SelectorExpr); ok && !inQuiet && isType(info.TypeOf(sel.X), quietContext) {
					// What a predicate reports holds only until code runs.
					for _, prefix := range []string{"Has", "Inherits", "Is"} {
						if strings.HasPrefix(sel.Sel.Name, prefix) {
							t.Errorf("%v: function without a QuietContext asks %s", p.fset.Position(n.Pos()), sel.Sel.Name)
						}
					}
				}
				switch {
				case tv.Type == nil || tv.IsType():
				case inQuiet && isType(tv.Type, executionContext):
					t.Errorf("%v: function with a QuietContext uses an ExecutionContext", p.fset.Position(n.Pos()))
				case !inQuiet && isType(tv.Type, quietContext) && !allowed[expr]:
					t.Errorf("%v: QuietContext outlives the expression that makes it", p.fset.Position(n.Pos()))
				}
				return true
			})
		}
		for id, obj := range info.Defs {
			if v, ok := obj.(*types.Var); ok && (v.IsField() || v.Parent() == p.pkg.Scope()) && isType(v.Type(), quietContext) {
				t.Errorf("%v: %s holds a QuietContext", p.fset.Position(id.Pos()), id.Name)
			}
		}
		for _, file := range p.files {
			for _, decl := range file.Decls {
				var name string
				var body ast.Node
				var inQuiet bool
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if decl.Body == nil {
						continue
					}
					f := info.Defs[decl.Name].(*types.Func)
					sig := f.Type().(*types.Signature)
					name, body, inQuiet = f.Origin().FullName(), decl.Body, hasQuiet(sig)
					if sig.Recv() != nil {
						methods[f.Name()] = append(methods[f.Name()], name)
					}
					if inQuiet {
						quiet = append(quiet, name)
						checkSignature(decl, sig)
					}
				case *ast.GenDecl:
					name, body = p.fset.Position(decl.Pos()).String(), decl
				}
				pos[name] = p.fset.Position(decl.Pos())
				if builtin {
					checkBuiltin(body, inQuiet)
				}
				written := map[*ast.Ident]bool{}
				// pairStep holds the literals that are pair steps, which have no context.
				pairStep := map[*ast.FuncLit]bool{}
				// record adds what body mentions to uses[name].
				var record func(name string, body ast.Node)
				record = func(name string, body ast.Node) {
					ast.Inspect(body, func(n ast.Node) bool {
						switch n := n.(type) {
						case *ast.FuncLit:
							if n.Body == body || !pairStep[n] && !hasQuiet(info.TypeOf(n).(*types.Signature)) {
								break
							}
							// The literal runs when the engine calls it, not in the function around it.
							lit := p.fset.Position(n.Pos()).String()
							pos[lit] = p.fset.Position(n.Pos())
							quiet = append(quiet, lit)
							record(lit, n.Body)
							return false
						case *ast.CallExpr:
							sig, ok := info.TypeOf(n.Fun).Underlying().(*types.Signature)
							if !ok || info.Types[n.Fun].IsType() {
								break
							}
							for i := range min(sig.Params().Len(), len(n.Args)) {
								if !isType(sig.Params().At(i).Type(), pairStepFunc) {
									continue
								}
								if info.Types[n.Args[i]].IsNil() {
									continue
								}
								switch arg := ast.Unparen(n.Args[i]).(type) {
								case *ast.FuncLit:
									pairStep[arg] = true
									pairSteps[p.fset.Position(arg.Pos()).String()] = true
									continue
								case *ast.Ident:
									if f, ok := info.Uses[arg].(*types.Func); ok {
										quiet = append(quiet, f.FullName())
										pairSteps[f.FullName()] = true
										continue
									}
								}
								t.Errorf("%v: pair step is neither a function literal nor a declared function", p.fset.Position(n.Args[i].Pos()))
							}
						case *ast.AssignStmt:
							for _, l := range n.Lhs {
								if sel, ok := l.(*ast.SelectorExpr); ok {
									written[sel.Sel] = true
								}
							}
						case *ast.BinaryExpr:
							// Comparing a function with nil does not call it.
							if sel, ok := n.X.(*ast.SelectorExpr); ok {
								if id, ok := n.Y.(*ast.Ident); ok && id.Name == "nil" {
									written[sel.Sel] = true
								}
							}
						case *ast.KeyValueExpr:
							if id, ok := n.Key.(*ast.Ident); ok {
								written[id] = true
							}
						case *ast.Ident:
							switch o := info.Uses[n].(type) {
							case *types.Func:
								if r := o.Type().(*types.Signature).Recv(); r != nil && types.IsInterface(r.Type()) {
									interfaceUses[name] = append(interfaceUses[name], o.Name())
									break
								}
								uses[name] = append(uses[name], o.Origin().FullName())
							case *types.Var:
								_, isFunc := o.Type().Underlying().(*types.Signature)
								global := o.Pkg() != nil && o.Parent() == o.Pkg().Scope()
								if isFunc && (o.IsField() && !written[n] || global) && strings.HasPrefix(o.Pkg().Path(), modulePath) {
									uses[name] = append(uses[name], "the function "+o.Name())
									forbidden["the function "+o.Name()] = runsCode
								}
							}
						}
						return true
					})
				}
				record(name, body)
			}
		}
	}
	for lookup, callers := range storageLookups {
		// The functions may forward to each other.
		var lookups []string
		for _, f := range prog.function(t, lookup) {
			lookups = append(lookups, f.(*types.Func).FullName())
		}
		allowed := map[string]bool{}
		for _, caller := range callers {
			allowed[prog.implementation(t, caller).FullName()] = true
		}
		called := map[string]bool{}
		for name, us := range uses {
			if slices.Contains(lookups, name) || !slices.ContainsFunc(us, func(u string) bool {
				return slices.Contains(lookups, u)
			}) {
				continue
			}
			called[name] = true
			if !allowed[name] {
				t.Errorf("%v: %s looks properties up with %s", pos[name], name, lookup)
			}
		}
		for name := range allowed {
			if !called[name] {
				t.Errorf("%s does not use %s", name, lookup)
			}
		}
	}
	for name := range pairSteps {
		if pos[name].Line == 0 {
			t.Errorf("no pair step %s", name)
		}
	}
	if len(pairSteps) == 0 {
		t.Error("no pair step")
	}
	if len(quiet) == listed {
		t.Error("no function with a QuietContext")
	}
	for name, ms := range interfaceUses {
		for _, m := range ms {
			uses[name] = append(uses[name], methods[m]...)
		}
	}
	seen := map[string]bool{}
	for _, root := range quiet {
		clear(seen)
		from := map[string]string{}
		work := []string{root}
		seen[root] = true
		for len(work) > 0 {
			name := work[0]
			work = work[1:]
			if reason := forbidden[name]; reason != "" {
				path := name
				for name != root {
					name = from[name]
					path = name + " > " + path
				}
				t.Errorf("%v: %s: %s", pos[root], reason, strings.ReplaceAll(path, "github.com/hajimehoshi/ekuma/internal/", ""))
				break
			}
			if name == poll {
				continue
			}
			for _, u := range uses[name] {
				if !seen[u] {
					seen[u] = true
					from[u] = name
					work = append(work, u)
				}
			}
		}
	}
}
