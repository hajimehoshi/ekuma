// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"maps"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// CompileEval compiles the program prog as the eval code of a direct eval at the call site
// that scope describes, or of an indirect eval when scope describes no variables. The returned
// function reaches the variables of the call site as its upvalues and returns the completion
// value of the code. poll, when not nil, is called now and then during a long compilation, and
// may stop it by panicking.
func CompileEval(prog *ast.Program, file string, scope *bytecode.EvalScope, poll func()) (f *bytecode.Func, err error) {
	c := newCompiler(prog, file)
	defer c.release()
	c.poll = poll
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			f, err = nil, e
		}
	}()
	return validated(c.compileEval(scope)), nil
}

// evalOuter is what the eval code being compiled sees of its call site.
type evalOuter struct {
	scope *bytecode.EvalScope
	vars  map[string]*bytecode.EvalVar

	// binds holds the bindings that stand for the variables of the call site.
	binds map[string]*binding

	// hoisted holds the names of the block-level functions that are also var-scoped.
	hoisted map[string]bool
}

var evalBindKinds = [...]bindKind{
	bytecode.EvalVarVar:      bindVar,
	bytecode.EvalVarLet:      bindLet,
	bytecode.EvalVarConst:    bindConst,
	bytecode.EvalVarClass:    bindClass,
	bytecode.EvalVarFuncName: bindFuncName,
	bytecode.EvalVarPseudo:   bindPseudo,
}

var evalVarKinds = [...]bytecode.EvalVarKind{
	bindVar:      bytecode.EvalVarVar,
	bindLet:      bytecode.EvalVarLet,
	bindConst:    bytecode.EvalVarConst,
	bindClass:    bytecode.EvalVarClass,
	bindFuncName: bytecode.EvalVarFuncName,
	bindPseudo:   bytecode.EvalVarPseudo,
}

// resolve finds the variable name of the call site for the eval code f, as an upvalue of f.
func (e *evalOuter) resolve(f *fn, name string) (ref, bool) {
	v, ok := e.vars[name]
	if !ok {
		return ref{}, false
	}
	b := e.binds[name]
	if b == nil {
		b = &binding{
			name: name,
			kind: evalBindKinds[v.Kind],
			tdz:  v.Check,
		}
		e.binds[name] = b
	}
	if i, ok := f.upvalIndex(b); ok {
		return ref{
			kind:  refUpval,
			b:     b,
			index: i,
		}, true
	}
	return ref{
		kind: refUpval,
		b:    b,
		index: f.addUpval(b, bytecode.UpvalDesc{
			Local: v.Local,
			Index: v.Index,
		}),
	}, true
}

// The functions nested in the nodes that hasDirectEval searches.
const (
	evalOwn    = iota // none
	evalArrows        // the arrow functions, and those nested in them
	evalAll           // all
)

// hasDirectEval reports whether a direct eval call occurs in the nodes, or in the functions
// nested in them that nested, one of the eval constants, says.
func hasDirectEval(nested int, nodes ...ast.Node) bool {
	found := false
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Function:
			return nested == evalAll || nested == evalArrows && n.Arrow
		case *ast.CallExpr:
			if id, ok := n.Callee.(*ast.Identifier); ok && id.Name == "eval" && !n.Optional {
				found = true
			}
		}
		return !found
	}
	for _, n := range nodes {
		if !isNil(n) {
			walk(n, visit)
		}
	}
	return found
}

// mayCapture reports whether e can create a closure or run a direct eval.
func mayCapture(e ast.Expr) bool {
	found := false
	walk(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Function, *ast.Class:
			found = true
		case *ast.CallExpr:
			if id, ok := n.Callee.(*ast.Identifier); ok && id.Name == "eval" && !n.Optional {
				found = true
			}
		}
		return !found
	})
	return found
}

// arrowsHaveDirectEval reports whether a direct eval call occurs in an arrow function
// nested in the function node, or in one nested in such an arrow function.
func arrowsHaveDirectEval(node *ast.Function) bool {
	found := false
	walk(node, func(n ast.Node) bool {
		if g, ok := n.(*ast.Function); ok && g != node {
			if g.Arrow && g.HasDirectEval {
				found = true
			}
			return false
		}
		return !found
	})
	return found
}

// pushEvalScope emits the scope that the direct evals of the code that follows declare
// their variables in.
func (f *fn) pushEvalScope() {
	f.emit(bytecode.OpEvalScope, 0, 0, 0)
	f.withDepth++
	f.evalLevel = f.withDepth
}

// evalSite records what a direct eval at the code being compiled sees, and returns the
// index of the record.
func (f *fn) evalSite() int32 {
	es := bytecode.EvalScope{
		Strict: f.strict(),
		Depth:  int32(f.withsAround()),
	}
	for g := f; g != nil; g = g.parent {
		if g.eval != nil {
			es.NewTarget = g.eval.scope.NewTarget
			es.SuperProperty = g.eval.scope.SuperProperty
			es.SuperCall = g.eval.scope.SuperCall
			es.InFieldInit = g.eval.scope.InFieldInit
			break
		}
		if g.node == nil {
			break
		}
		if !g.node.Arrow {
			n := g.node
			es.NewTarget = true
			es.SuperProperty = n.Method || n.FieldInit || n.StaticBlock
			es.SuperCall = n.DerivedConstructor
			es.InFieldInit = n.FieldInit
			break
		}
	}

	// The scope of the function f that is the variable environment of non-strict eval code,
	// which is nil for the code of the parameters.
	var varEnv *scope
	switch {
	case es.Strict:
	case f.eval != nil:
		es.LazyArguments = f.eval.scope.LazyArguments
		if f.eval.scope.VarDepth > 0 {
			es.VarDepth = int32(f.withDepth) + f.eval.scope.VarDepth
		}
	case f.isScript:
	default:
		es.LazyArguments = f.lazyArgs
		// A non-strict function has the scope of OpEvalScope when it contains a direct eval.
		if f.evalLevel == 0 {
			f.c.fail(f.node, "direct eval without a variable scope")
		}
		es.VarDepth = int32(f.withDepth - f.evalLevel + 1)
		for s := f.scope; s != nil; s = s.parent {
			if s == f.varScope {
				varEnv = s
			}
		}
	}

	seen := map[string]bool{}
	add := func(name string, kind bindKind, place bytecode.EvalPlace) {
		seen[name] = true
		r := f.resolve(name)
		v := bytecode.EvalVar{
			Name:  name,
			Kind:  evalVarKinds[kind],
			Place: place,
			Check: r.needsCheck() || kind == bindLet || kind == bindConst || kind == bindClass,
			Depth: f.dynDepth(name),
		}
		switch r.kind {
		case refLocal:
			v.Local = true
			v.Index = r.b.reg
		case refUpval:
			v.Index = r.index
		}
		es.Vars = append(es.Vars, v)
	}
	inner := true
	for g := f; g != nil; g = g.parent {
		for s := g.scope; s != nil; s = s.parent {
			if g == f {
				s.captured = true
			}
			for _, b := range s.sortedBinds() {
				name := b.name
				if seen[name] {
					continue
				}
				place := bytecode.EvalPlaceOuter
				switch {
				case g != f:
				case b.kind == bindFuncName:
				case s == varEnv:
					place = bytecode.EvalPlaceInner
					if b.kind == bindVar {
						place = bytecode.EvalPlaceVarEnv
					}
				case !inner:
				case b.catch:
					place = bytecode.EvalPlaceCatch
				default:
					place = bytecode.EvalPlaceInner
				}
				add(name, b.kind, place)
			}
			if s == varEnv {
				inner = false
			}
		}
		if g.eval != nil {
			for _, name := range slices.Sorted(maps.Keys(g.eval.vars)) {
				if !seen[name] {
					v := g.eval.vars[name]
					add(name, evalBindKinds[v.Kind], v.Place)
				}
			}
		}
	}
	slices.SortFunc(es.Vars, func(a, b bytecode.EvalVar) int {
		return strings.Compare(a.Name, b.Name)
	})
	f.f.EvalScopes = append(f.f.EvalScopes, es)
	return int32(len(f.f.EvalScopes) - 1)
}

func (c *compiler) compileEval(es *bytecode.EvalScope) *bytecode.Func {
	prog := c.prog
	f := c.newFn(nil, nil, "")
	ev := &evalOuter{
		scope:   es,
		vars:    map[string]*bytecode.EvalVar{},
		binds:   map[string]*binding{},
		hoisted: map[string]bool{},
	}
	for i := range es.Vars {
		ev.vars[es.Vars[i].Name] = &es.Vars[i]
	}
	f.eval = ev
	c.evalCode = true
	f.f.Strict = prog.Strict
	f.f.IsArrow = true
	f.f.IsEval = true
	f.anyMutated = prog.Scope.ContainsDirectEval

	// The eval code of an indirect eval sees no call site, and its this is the one it is called with.
	if ev.vars["this"] == nil {
		this := f.declare("this", bindPseudo)
		f.emit(bytecode.OpLoadThis, this.reg, 0, 0)
	}
	f.completion = f.newReg()
	f.emit(bytecode.OpLoadUndef, f.completion, 0, 0)

	// EvalDeclarationInstantiation. Strict eval code has a variable environment of its own,
	// whose variables are registers. The variables of other eval code live in the variable
	// environment of the call site.
	strict := prog.Strict
	funcs := map[string]*ast.Function{}
	var funcOrder []string
	var varNames []string
	var lexicals []*binding
	// annexBOnly holds the hoisted names of block-level functions that are not also var names.
	annexBOnly := map[string]bool{}
	for _, d := range prog.Scope.Decls {
		switch d.Kind {
		case ast.DeclVar:
			varNames = append(varNames, d.Name)
		case ast.DeclAnnexBVar:
			if strict {
				break
			}
			// A catch parameter does not stop the hoisting (Annex B.3.4).
			if v := ev.vars[d.Name]; v != nil && v.Place == bytecode.EvalPlaceInner {
				break
			}
			ev.hoisted[d.Name] = true
			if !slices.Contains(varNames, d.Name) {
				annexBOnly[d.Name] = true
				varNames = append(varNames, d.Name)
			}
		case ast.DeclFunction:
			if _, ok := funcs[d.Name]; !ok {
				funcOrder = append(funcOrder, d.Name)
			}
			funcs[d.Name] = d.Func
		case ast.DeclLet, ast.DeclConst, ast.DeclClass:
			kind := bindLet
			switch d.Kind {
			case ast.DeclConst:
				kind = bindConst
			case ast.DeclClass:
				kind = bindClass
			}
			lb := f.declare(d.Name, kind)
			lb.tdz = true
			lexicals = append(lexicals, lb)
		}
	}
	if !strict {
		for _, d := range prog.Scope.Decls {
			if d.Kind != ast.DeclVar && d.Kind != ast.DeclFunction {
				continue
			}
			if v := ev.vars[d.Name]; v != nil && v.Place == bytecode.EvalPlaceInner {
				c.failAt(d.Pos, "identifier '%s' has already been declared", d.Name)
			}
		}
	}

	switch {
	case strict:
		for _, name := range funcOrder {
			f.declare(name, bindVar)
		}
		for _, name := range varNames {
			f.declare(name, bindVar)
		}
		f.f.NumClear = int(f.top)
		for _, lb := range lexicals {
			f.emit(bytecode.OpLoadTDZ, lb.reg, 0, 0)
		}
		for _, name := range funcOrder {
			fb, _ := f.lookupLocal(name)
			f.closure(funcs[name], fb.reg, name)
		}
	case es.VarDepth == 0:
		f.f.NumClear = int(f.top)
		for _, name := range funcOrder {
			f.emit(bytecode.OpCheckGlobalDecl, f.key(name), bytecode.DeclFunction, 0)
		}
		for _, name := range varNames {
			if funcs[name] == nil && !annexBOnly[name] {
				f.emit(bytecode.OpCheckGlobalDecl, f.key(name), bytecode.DeclVar, 0)
			}
		}
		for _, name := range varNames {
			if funcs[name] == nil && annexBOnly[name] {
				f.declareAnnexBGlobal(name, bytecode.GlobalVarDeletable)
			}
		}
		for _, lb := range lexicals {
			f.emit(bytecode.OpLoadTDZ, lb.reg, 0, 0)
		}
		for _, name := range funcOrder {
			t := f.newReg()
			f.closure(funcs[name], t, name)
			f.emit(bytecode.OpDeclareGlobalVar, f.key(name), bytecode.GlobalVarFunc|bytecode.GlobalVarDeletable, t)
			f.free(t)
		}
		for _, name := range varNames {
			if funcs[name] == nil && !annexBOnly[name] {
				f.emit(bytecode.OpDeclareGlobalVar, f.key(name), bytecode.GlobalVarDeletable, -1)
			}
		}
	default:
		f.f.NumClear = int(f.top)
		for _, lb := range lexicals {
			f.emit(bytecode.OpLoadTDZ, lb.reg, 0, 0)
		}
		for _, name := range funcOrder {
			t := f.newReg()
			f.closure(funcs[name], t, name)
			f.storeEvalVar(name, t)
			f.free(t)
		}
		for _, name := range varNames {
			if funcs[name] != nil {
				continue
			}
			if v := ev.vars[name]; v != nil && v.Place == bytecode.EvalPlaceVarEnv {
				continue
			}
			f.emit(bytecode.OpDeclareEvalVar, f.key(name), es.VarDepth, -1)
		}
	}

	f.stmts(prog.Body)
	f.emit(bytecode.OpReturn, f.completion, 0, 0)
	return f.finish()
}

// storeEvalVar emits the assignment of the register src to the variable name of the variable
// environment of the non-strict eval code f, which a scope of OpEvalScope holds unless the
// variable is one of the call site, and declares the variable when it is not.
func (f *fn) storeEvalVar(name string, src int32) {
	if v := f.eval.vars[name]; v != nil && v.Place == bytecode.EvalPlaceVarEnv {
		r, _ := f.eval.resolve(f, name)
		f.emit(bytecode.OpSetUpval, src, r.index, 0)
		return
	}
	f.emit(bytecode.OpDeclareEvalVar, f.key(name), int32(f.withDepth)+f.eval.scope.VarDepth, src)
}

// failAt aborts the compilation with an error at the byte offset pos.
func (c *compiler) failAt(pos int32, format string, args ...any) {
	c.fail(&ast.Identifier{
		StartOff: pos,
		EndOff:   pos,
	}, format, args...)
}
