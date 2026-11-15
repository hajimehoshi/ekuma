// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// walk calls visit for n and, while visit returns true, for the nodes below it.
// Nil nodes are skipped.
func walk(n ast.Node, visit func(ast.Node) bool) {
	if isNil(n) || !visit(n) {
		return
	}
	w := func(m ast.Node) {
		walk(m, visit)
	}
	switch n := n.(type) {
	case *ast.TemplateLit:
		for _, e := range n.Exprs {
			w(e)
		}
	case *ast.TaggedTemplate:
		w(n.Tag)
		w(n.Quasi)
	case *ast.ImportCall:
		w(n.Source)
		w(n.Options)
	case *ast.ArrayLit:
		for _, e := range n.Elements {
			w(e)
		}
	case *ast.ObjectLit:
		for i := range n.Properties {
			p := &n.Properties[i]
			if p.Computed {
				w(p.Key)
			}
			w(p.Value)
		}
	case *ast.Function:
		for _, p := range n.Params {
			w(p)
		}
		for _, s := range n.Body {
			w(s)
		}
		w(n.ExprBody)
	case *ast.Class:
		w(n.Super)
		for i := range n.Members {
			m := &n.Members[i]
			if m.Computed {
				w(m.Key)
			}
			if m.Value != nil {
				w(m.Value)
			}
		}
	case *ast.UnaryExpr:
		w(n.Arg)
	case *ast.UpdateExpr:
		w(n.Arg)
	case *ast.BinaryExpr:
		w(n.Left)
		w(n.Right)
	case *ast.LogicalExpr:
		w(n.Left)
		w(n.Right)
	case *ast.PrivateInExpr:
		w(n.Right)
	case *ast.AssignExpr:
		w(n.Target)
		w(n.Value)
	case *ast.CondExpr:
		w(n.Test)
		w(n.Consequent)
		w(n.Alternate)
	case *ast.CallExpr:
		w(n.Callee)
		for _, a := range n.Args {
			w(a)
		}
	case *ast.NewExpr:
		w(n.Callee)
		for _, a := range n.Args {
			w(a)
		}
	case *ast.MemberExpr:
		w(n.Object)
		if n.Computed {
			w(n.Property)
		}
	case *ast.OptionalChain:
		w(n.Expr)
	case *ast.SequenceExpr:
		for _, e := range n.Exprs {
			w(e)
		}
	case *ast.SpreadElement:
		w(n.Arg)
	case *ast.YieldExpr:
		w(n.Arg)
	case *ast.AwaitExpr:
		w(n.Arg)
	case *ast.ObjectPattern:
		for i := range n.Properties {
			p := &n.Properties[i]
			if p.Computed {
				w(p.Key)
			}
			w(p.Value)
		}
		w(n.Rest)
	case *ast.ArrayPattern:
		for _, e := range n.Elements {
			w(e)
		}
	case *ast.AssignPattern:
		w(n.Target)
		w(n.Default)
	case *ast.RestElement:
		w(n.Target)
	case *ast.ExprStmt:
		w(n.Expr)
	case *ast.VarDecl:
		for i := range n.Decls {
			w(n.Decls[i].Target)
			w(n.Decls[i].Init)
		}
	case *ast.FuncDecl:
		w(n.Func)
	case *ast.ClassDecl:
		w(n.Class)
	case *ast.BlockStmt:
		for _, s := range n.Body {
			w(s)
		}
	case *ast.IfStmt:
		w(n.Test)
		w(n.Consequent)
		w(n.Alternate)
	case *ast.ForStmt:
		w(n.Init)
		w(n.Test)
		w(n.Update)
		w(n.Body)
	case *ast.ForInStmt:
		if n.Decl != nil {
			w(n.Decl)
		}
		w(n.Target)
		w(n.Right)
		w(n.Body)
	case *ast.ForOfStmt:
		if n.Decl != nil {
			w(n.Decl)
		}
		w(n.Target)
		w(n.Right)
		w(n.Body)
	case *ast.WhileStmt:
		w(n.Test)
		w(n.Body)
	case *ast.DoWhileStmt:
		w(n.Body)
		w(n.Test)
	case *ast.ReturnStmt:
		w(n.Arg)
	case *ast.ThrowStmt:
		w(n.Arg)
	case *ast.TryStmt:
		w(n.Block)
		w(n.Param)
		if n.Handler != nil {
			w(n.Handler)
		}
		if n.Finalizer != nil {
			w(n.Finalizer)
		}
	case *ast.SwitchStmt:
		w(n.Discriminant)
		for i := range n.Cases {
			w(n.Cases[i].Test)
			for _, s := range n.Cases[i].Body {
				w(s)
			}
		}
	case *ast.LabeledStmt:
		w(n.Body)
	case *ast.WithStmt:
		w(n.Object)
		w(n.Body)
	case *ast.ExportNamedDecl:
		w(n.Decl)
	case *ast.ExportDefaultDecl:
		w(n.Decl)
		w(n.Expr)
	}
}

// isNil reports whether n is nil or holds a nil pointer.
func isNil(n ast.Node) bool {
	switch n := n.(type) {
	case nil:
		return true
	case *ast.Identifier:
		return n == nil
	case *ast.Function:
		return n == nil
	case *ast.Class:
		return n == nil
	case *ast.BlockStmt:
		return n == nil
	case *ast.VarDecl:
		return n == nil
	case *ast.TemplateLit:
		return n == nil
	}
	return false
}

// patternNames calls f for every identifier that the pattern p binds or assigns.
func patternNames(p ast.Pattern, f func(*ast.Identifier)) {
	switch p := p.(type) {
	case *ast.Identifier:
		f(p)
	case *ast.ObjectPattern:
		for i := range p.Properties {
			patternNames(p.Properties[i].Value, f)
		}
		if p.Rest != nil {
			patternNames(p.Rest, f)
		}
	case *ast.ArrayPattern:
		for _, e := range p.Elements {
			if e != nil {
				patternNames(e, f)
			}
		}
	case *ast.AssignPattern:
		patternNames(p.Target, f)
	case *ast.RestElement:
		patternNames(p.Target, f)
	}
}

// assignedNames calls f for the name of every variable that the code below n assigns to.
func assignedNames(n ast.Node, f func(string)) {
	assignments(n, func(name string, nested bool) {
		f(name)
	})
}

// assignments calls f for the name of every variable that the code below n assigns to.
// nested says whether the assignment is inside a function nested in n.
func assignments(n ast.Node, f func(name string, nested bool)) {
	depth := 0
	var visit func(ast.Node) bool
	target := func(p ast.Pattern) {
		patternNames(p, func(id *ast.Identifier) {
			f(id.Name, depth > 0)
		})
	}
	visit = func(m ast.Node) bool {
		switch m := m.(type) {
		case *ast.Function:
			if m == n {
				return true
			}
			depth++
			for _, p := range m.Params {
				walk(p, visit)
			}
			for _, s := range m.Body {
				walk(s, visit)
			}
			walk(m.ExprBody, visit)
			depth--
			return false
		case *ast.AssignExpr:
			target(m.Target)
		case *ast.UpdateExpr:
			if id, ok := m.Arg.(*ast.Identifier); ok {
				target(id)
			}
		case *ast.ForInStmt:
			if m.Target != nil {
				target(m.Target)
			}
		case *ast.ForOfStmt:
			if m.Target != nil {
				target(m.Target)
			}
		}
		return true
	}
	walk(n, visit)
}
