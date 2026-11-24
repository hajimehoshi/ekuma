// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// TestQuietContextBoundary requires that no function with a QuietContext can run script or host
// code, and that built-ins cannot get around that.
func TestQuietContextBoundary(t *testing.T) {
	const enginePath = "github.com/hajimehoshi/ekuma/internal/engine"
	// The functions that run script or host code. Reading a field that holds a function counts too.
	doors := map[string]bool{
		"(*" + enginePath + ".ExecutionContext).runFrame":     true,
		"(*" + enginePath + ".ExecutionContext).callNative":   true,
		"(*" + enginePath + ".ExecutionContext).invokeNative": true,
		"(*" + enginePath + ".agent).exec":                    true,
		"(*" + enginePath + ".agent).InitLazyObject":          true,
	}
	// An interruption unwinds to the host, which runs nothing in between.
	poll := "(*" + enginePath + ".agent).Poll"

	// uses holds what a declared function mentions, by the full name of the function.
	uses := map[string][]string{}
	methods := map[string][]string{}
	interfaceUses := map[string][]string{}
	var quiet []string
	pos := map[string]token.Position{}
	for _, p := range checkPackages(t, "../...", "../../intl/...") {
		info := p.info
		builtin := p.path != enginePath
		hasQuiet := func(sig *types.Signature) bool {
			if r := sig.Recv(); r != nil && engineType(r.Type(), "QuietContext") {
				return true
			}
			for v := range sig.Params().Variables() {
				if engineType(v.Type(), "QuietContext") {
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
				if sel, ok := expr.(*ast.SelectorExpr); ok && !inQuiet && engineType(info.TypeOf(sel.X), "QuietContext") {
					// What a predicate reports holds only until code runs.
					for _, prefix := range []string{"Has", "Inherits", "Is"} {
						if strings.HasPrefix(sel.Sel.Name, prefix) {
							t.Errorf("%v: function without a QuietContext asks %s", p.fset.Position(n.Pos()), sel.Sel.Name)
						}
					}
				}
				switch {
				case tv.Type == nil || tv.IsType():
				case inQuiet && engineType(tv.Type, "ExecutionContext"):
					t.Errorf("%v: function with a QuietContext uses an ExecutionContext", p.fset.Position(n.Pos()))
				case !inQuiet && engineType(tv.Type, "QuietContext") && !allowed[expr]:
					t.Errorf("%v: QuietContext outlives the expression that makes it", p.fset.Position(n.Pos()))
				}
				return true
			})
		}
		for id, obj := range info.Defs {
			if v, ok := obj.(*types.Var); ok && (v.IsField() || v.Parent() == p.pkg.Scope()) && engineType(v.Type(), "QuietContext") {
				t.Errorf("%v: %s holds a QuietContext", p.fset.Position(id.Pos()), id.Name)
			}
		}
		for _, file := range p.files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				f := info.Defs[fn.Name].(*types.Func)
				sig := f.Type().(*types.Signature)
				name := f.Origin().FullName()
				pos[name] = p.fset.Position(fn.Pos())
				if sig.Recv() != nil {
					methods[f.Name()] = append(methods[f.Name()], name)
				}
				if hasQuiet(sig) {
					quiet = append(quiet, name)
					checkSignature(fn, sig)
				}
				if builtin {
					checkBuiltin(fn.Body, hasQuiet(sig))
				}
				written := map[*ast.Ident]bool{}
				// record adds what body mentions to uses[name].
				var record func(name string, body ast.Node)
				record = func(name string, body ast.Node) {
					ast.Inspect(body, func(n ast.Node) bool {
						switch n := n.(type) {
						case *ast.FuncLit:
							if n.Body == body || !hasQuiet(info.TypeOf(n).(*types.Signature)) {
								break
							}
							// The literal runs when the engine calls it, not in the function around it.
							lit := p.fset.Position(n.Pos()).String()
							pos[lit] = p.fset.Position(n.Pos())
							quiet = append(quiet, lit)
							record(lit, n.Body)
							return false
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
								if isFunc && (o.IsField() && !written[n] || global) && strings.HasPrefix(o.Pkg().Path(), "github.com/hajimehoshi/ekuma") {
									uses[name] = append(uses[name], "the function "+o.Name())
									doors["the function "+o.Name()] = true
								}
							}
						}
						return true
					})
				}
				record(name, fn.Body)
			}
		}
	}
	for name := range doors {
		if !strings.HasPrefix(name, "the function ") && pos[name].Line == 0 {
			t.Errorf("no function %s", name)
		}
	}
	if pos[poll].Line == 0 {
		t.Errorf("no function %s", poll)
	}
	if len(quiet) == 0 {
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
			if doors[name] {
				path := name
				for name != root {
					name = from[name]
					path = name + " > " + path
				}
				t.Errorf("%v: can run code: %s", pos[root], strings.ReplaceAll(path, "github.com/hajimehoshi/ekuma/internal/", ""))
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
