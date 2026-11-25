// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// TestNativeRegistrations checks that native functions are registered only by the declarations of
// package-level variables and by init functions, which run once per process.
func TestNativeRegistrations(t *testing.T) {
	prog := checkProgram(t, "../../...")
	native := prog.typ(t, "Native")
	isRegistration := func(o types.Object) bool {
		f, ok := o.(*types.Func)
		if !ok || !strings.HasPrefix(strings.ToLower(f.Name()), "register") {
			return false
		}
		results := f.Type().(*types.Signature).Results()
		return results.Len() == 1 && isType(results.At(0).Type(), native)
	}
	var found bool
	for _, p := range prog.pkgs {
		var check func(root ast.Node, allowed bool)
		check = func(root ast.Node, allowed bool) {
			ast.Inspect(root, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncLit:
					if n != root {
						check(n, false)
						return false
					}
				case *ast.Ident:
					if !isRegistration(p.info.Uses[n]) {
						break
					}
					found = true
					if !allowed {
						t.Errorf("%v: native function registered outside the initialization of a package", p.fset.Position(n.Pos()))
					}
				}
				return true
			})
		}
		for _, file := range p.files {
			for _, decl := range file.Decls {
				if f, ok := decl.(*ast.FuncDecl); ok {
					check(f, f.Recv == nil && f.Name.Name == "init" || isRegistration(p.info.Defs[f.Name]))
				} else {
					check(decl, true)
				}
			}
		}
	}
	if !found {
		t.Error("no registrations")
	}
}

func TestInvalidNatives(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	for name, f := range map[string]func(){
		"registration of nil":       func() { engine.RegisterNative(nil) },
		"call of no native":         func() { cx.Call(cx.NewNative("", 0, engine.Native{}), engine.Undefined) },
		"function of a nil Go func": func() { cx.NewFunction("", nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			f()
		}()
	}
}
