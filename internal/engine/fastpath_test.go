// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	_ "embed"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

//go:embed interp.go
var interpSource string

// fastPathCalls are the calls that the fast paths of the interpreter loop may make: builtins and
// conversions that compile to plain instructions, and helpers that the compiler inlines there.
var fastPathCalls = map[string]bool{
	"len":                  true,
	"cap":                  true,
	"min":                  true,
	"max":                  true,
	"float64":              true,
	"int":                  true,
	"int32":                true,
	"int64":                true,
	"uint":                 true,
	"uint32":               true,
	"Number":               true,
	"Bool":                 true,
	"clearFrameRegs":       true,
	"setNumber":            true,
	"numconv.InInt64Range": true,
	"vm.interrupt.Load":    true,
	"vm.Intrinsic":         true,
}

// fastPathMethods are the methods that the fast paths may call on a value of any name.
var fastPathMethods = map[string]bool{
	"IsNullish":    true,
	"firstCleared": true,
	"isHTMLDDA":    true,
}

// TestExecFastPathsCallNothing tests that the first switch of the interpreter loop calls no
// function and allocates nothing. Any call there makes the compiler keep the values that the loop
// carries on the stack for every instruction.
func TestExecFastPathsCallNothing(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "interp.go", interpSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	fast := fastSwitch(f)
	if fast == nil {
		t.Fatal("no switch on i.Op at the start of the loop of (*VM).exec")
	}
	ast.Inspect(fast, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			name := types.ExprString(n.Fun)
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && fastPathMethods[sel.Sel.Name] {
				return true
			}
			if !fastPathCalls[name] {
				t.Errorf("%v: call of %s", fset.Position(n.Pos()), name)
			}
		case *ast.FuncLit, *ast.GoStmt, *ast.DeferStmt, *ast.TypeAssertExpr, *ast.SendStmt, *ast.SelectStmt:
			t.Errorf("%v: %T", fset.Position(n.Pos()), n)
		case *ast.CompositeLit:
			if _, ok := n.Type.(*ast.Ident); !ok {
				t.Errorf("%v: literal of %s", fset.Position(n.Pos()), types.ExprString(n.Type))
			}
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				t.Errorf("%v: channel receive", fset.Position(n.Pos()))
			}
			if _, ok := n.X.(*ast.CompositeLit); ok && n.Op == token.AND {
				t.Errorf("%v: allocation", fset.Position(n.Pos()))
			}
		case *ast.BinaryExpr:
			for _, x := range []ast.Expr{n.X, n.Y} {
				if l, ok := x.(*ast.BasicLit); ok && l.Kind == token.STRING {
					t.Errorf("%v: string concatenation", fset.Position(n.Pos()))
				}
			}
		}
		return true
	})
}

// fastSwitch returns the first switch statement in the loop of (*VM).exec, or nil.
func fastSwitch(f *ast.File) *ast.SwitchStmt {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "exec" || fn.Recv == nil {
			continue
		}
		for _, s := range fn.Body.List {
			loop, ok := s.(*ast.ForStmt)
			if !ok {
				continue
			}
			for _, s := range loop.Body.List {
				if sw, ok := s.(*ast.SwitchStmt); ok && types.ExprString(sw.Tag) == "i.Op" {
					return sw
				}
			}
		}
	}
	return nil
}
