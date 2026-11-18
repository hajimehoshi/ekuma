// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"slices"

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
		for _, d := range n.Decorators {
			w(d)
		}
		w(n.Super)
		for i := range n.Members {
			m := &n.Members[i]
			for _, d := range m.Decorators {
				w(d)
			}
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

// assignTarget returns the pattern whose variables the node n assigns to, or nil.
func assignTarget(n ast.Node) ast.Pattern {
	switch n := n.(type) {
	case *ast.AssignExpr:
		return n.Target
	case *ast.UpdateExpr:
		if id, ok := n.Arg.(*ast.Identifier); ok {
			return id
		}
	case *ast.ForInStmt:
		return n.Target
	case *ast.ForOfStmt:
		return n.Target
	}
	return nil
}

// assignedNames calls f for the name of every variable that the code below n assigns to.
func assignedNames(n ast.Node, f func(string)) {
	name := func(id *ast.Identifier) {
		f(id.Name)
	}
	walk(n, func(m ast.Node) bool {
		if p := assignTarget(m); p != nil {
			patternNames(p, name)
		}
		return true
	})
}

// assignIndex tells which functions assign to a variable name. A walk of the syntax tree
// numbers every entry into a function and every exit from it, so that the span of a function
// holds the spans of the functions nested in it.
type assignIndex struct {
	spans map[*ast.Function]funcSpan

	// exits maps a name to the exit numbers of the functions whose own code assigns to it,
	// in ascending order.
	exits map[string][]int32

	// lists is the storage that new lists of exits are taken from. Its length is the part in use.
	lists []int32

	// names holds the names that the functions being walked assign to, innermost last.
	names []string

	// ticks is the last number given.
	ticks int32
}

// funcSpan holds the numbers of the entry into a function and of the exit from it. The zero
// value is the span of no function.
type funcSpan struct {
	enter int32
	exit  int32
}

// add numbers the function node and the functions nested in it, which must not be numbered
// yet, and returns the span of node.
func (x *assignIndex) add(node *ast.Function) funcSpan {
	if x.spans == nil {
		x.spans = map[*ast.Function]funcSpan{}
		x.exits = map[string][]int32{}
	}

	name := func(id *ast.Identifier) {
		x.names = append(x.names, id.Name)
	}
	var function func(*ast.Function) funcSpan
	visit := func(n ast.Node) bool {
		if g, ok := n.(*ast.Function); ok {
			function(g)
			return false
		}
		if p := assignTarget(n); p != nil {
			patternNames(p, name)
		}
		return true
	}
	function = func(g *ast.Function) funcSpan {
		x.ticks++
		enter := x.ticks
		own := len(x.names)
		for _, p := range g.Params {
			walk(p, visit)
		}
		for _, s := range g.Body {
			walk(s, visit)
		}
		walk(g.ExprBody, visit)
		x.ticks++
		s := funcSpan{
			enter: enter,
			exit:  x.ticks,
		}
		for _, n := range x.names[own:] {
			e := x.exits[n]
			if len(e) > 0 && e[len(e)-1] == s.exit {
				continue
			}
			if e == nil {
				e = x.newList()
			}
			x.exits[n] = append(e, s.exit)
		}
		x.names = x.names[:own]
		x.spans[g] = s
		return s
	}
	return function(node)
}

// newList returns an empty list of exits with room for two, taken from x.lists.
func (x *assignIndex) newList() []int32 {
	n := len(x.lists)
	if cap(x.lists)-n < 2 {
		x.lists = make([]int32, 0, max(64, 2*cap(x.lists)))
		n = 0
	}
	x.lists = x.lists[:n+2]
	return x.lists[n : n : n+2]
}

// reset empties x and drops its references to the syntax tree, keeping its storage.
func (x *assignIndex) reset() {
	clear(x.spans)
	clear(x.exits)
	x.lists = x.lists[:0]
	clear(x.names[:cap(x.names)])
	x.names = x.names[:0]
	x.ticks = 0
}

// assignedIn reports whether the code of a function whose exit number is in (lo, hi] assigns
// to name.
func (x *assignIndex) assignedIn(name string, lo, hi int32) bool {
	e := x.exits[name]
	i, _ := slices.BinarySearch(e, lo+1)
	return i < len(e) && e[i] <= hi
}

// assigns reports whether the code of the function f, or of a function nested in it, assigns
// to the variable name. It is false for a script, a module or eval code.
func (f *fn) assigns(name string) bool {
	return f.c.scratch.assigns.assignedIn(name, f.span.enter, f.span.exit)
}

// nestedAssigns reports whether the code of a function nested in the function f assigns to the
// variable name. It is false for a script, a module or eval code.
func (f *fn) nestedAssigns(name string) bool {
	return f.c.scratch.assigns.assignedIn(name, f.span.enter, f.span.exit-1)
}
