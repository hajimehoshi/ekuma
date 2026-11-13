// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

func (f *fn) stmts(list []ast.Stmt) {
	for _, s := range list {
		f.stmt(s, nil)
	}
}

// enterScope opens a block scope for the declarations of s, which may be nil.
// It reports whether a scope was opened; the caller then has to call leaveScope.
func (f *fn) enterScope(s *ast.Scope, alwaysCheck bool) bool {
	if s == nil || len(s.Decls) == 0 {
		return false
	}
	sc := f.pushScope()
	f.control = append(f.control, &ctl{
		kind: ctlScope,
		reg:  sc.firstReg,
	})
	f.declareLexical(s, alwaysCheck)
	return true
}

func (f *fn) leaveScope() {
	f.control = f.control[:len(f.control)-1]
	f.popScope()
}

// declareLexical creates the let, const, class and function bindings of the scope s
// in the current scope and instantiates the functions.
func (f *fn) declareLexical(s *ast.Scope, alwaysCheck bool) {
	funcs := map[string]*ast.Function{}
	var order []string
	for _, d := range s.Decls {
		switch d.Kind {
		case ast.DeclLet, ast.DeclConst, ast.DeclClass:
			kind := bindLet
			switch d.Kind {
			case ast.DeclConst:
				kind = bindConst
			case ast.DeclClass:
				kind = bindClass
			}
			b := f.declare(d.Name, kind)
			b.tdz = true
			b.alwaysCheck = alwaysCheck
			f.emit(bytecode.OpLoadTDZ, b.reg, 0, 0)
		case ast.DeclFunction:
			if _, ok := funcs[d.Name]; !ok {
				order = append(order, d.Name)
				f.declare(d.Name, bindLet)
			}
			funcs[d.Name] = d.Func
		}
	}
	for _, name := range order {
		b, _ := f.lookupLocal(name)
		f.closure(funcs[name], b.reg, name)
	}
}

// initBinding stores the register src to the declared variable name as its first value.
func (f *fn) initBinding(n ast.Node, name string, src int32) {
	b, _ := f.lookupLocal(name)
	if b == nil {
		f.storeVar(n, name, src, true)
		return
	}
	if b.reg != src {
		f.emit(bytecode.OpMove, b.reg, src, 0)
	}
	b.tdz = false
}

func (f *fn) varDecl(n *ast.VarDecl) {
	for i := range n.Decls {
		d := &n.Decls[i]
		mark := f.top
		id, isID := d.Target.(*ast.Identifier)
		switch {
		case !isID:
			v := f.exprAny(d.Init)
			f.destructure(d.Target, v, declMode(n.Kind))
		case n.Kind == ast.DeclVar:
			if d.Init == nil {
				break
			}
			if r := f.localReg(id.Name); r >= 0 {
				if singleWrite(d.Init) {
					f.named(d.Init, r, id.Name)
				} else {
					t := f.newReg()
					f.named(d.Init, t, id.Name)
					f.emit(bytecode.OpMove, r, t, 0)
				}
				break
			}
			t := f.newReg()
			f.named(d.Init, t, id.Name)
			f.storeVar(d, id.Name, t, false)
		default:
			b, _ := f.lookupLocal(id.Name)
			switch {
			case b == nil:
				// A lexical declaration at the top level of a script.
				t := f.newReg()
				if d.Init != nil {
					f.named(d.Init, t, id.Name)
				} else {
					f.emit(bytecode.OpLoadUndef, t, 0, 0)
				}
				f.storeVar(d, id.Name, t, true)
			case d.Init == nil:
				f.emit(bytecode.OpLoadUndef, b.reg, 0, 0)
				b.tdz = false
			case singleWrite(d.Init):
				f.named(d.Init, b.reg, id.Name)
				b.tdz = false
			default:
				t := f.newReg()
				f.named(d.Init, t, id.Name)
				f.emit(bytecode.OpMove, b.reg, t, 0)
				b.tdz = false
			}
		}
		f.free(mark)
	}
}

func declMode(k ast.DeclKind) patMode {
	if k == ast.DeclVar {
		return patAssign
	}
	return patInit
}

// findTarget returns the construct that a break or continue with the given label targets.
func (f *fn) findTarget(n ast.Node, label *ast.Identifier, isContinue bool) *ctl {
	for i := len(f.control) - 1; i >= 0; i-- {
		c := f.control[i]
		if c.kind != ctlLoop {
			continue
		}
		if label == nil {
			if c.isLoop || (!isContinue && c.isSwitch) {
				return c
			}
			continue
		}
		for _, l := range c.labels {
			if l == label.Name {
				return c
			}
		}
	}
	f.c.fail(n, "no target for the jump")
	return nil
}

// exit emits the jump out of the constructs above target on the control stack.
// For exitReturn, target is nil and value is the register of the result.
func (f *fn) exit(kind exitKind, target *ctl, value int32) {
	for i := len(f.control) - 1; i >= 0; i-- {
		c := f.control[i]
		if c == target {
			break
		}
		switch c.kind {
		case ctlLoop:
			if c.hasIter {
				f.emit(bytecode.OpIterClose, c.reg, 0, 0)
			}
		case ctlScope:
			f.emit(bytecode.OpCloseUpvals, c.reg, 0, 0)
		case ctlWith:
			f.emit(bytecode.OpPopScope, 0, 0, 0)
		case ctlFinally:
			// Continue the jump after the finally block.
			id := int32(len(c.pending) + 2)
			c.pending = append(c.pending, pendingExit{
				id:     id,
				kind:   kind,
				target: target,
			})
			if kind == exitReturn && value != c.valueReg {
				f.emit(bytecode.OpMove, c.valueReg, value, 0)
			}
			f.emit(bytecode.OpLoadInt, c.kindReg, id, 0)
			c.toFinal = append(c.toFinal, f.jump())
			return
		}
	}
	switch kind {
	case exitBreak:
		if target.hasIter {
			f.emit(bytecode.OpIterClose, target.reg, 0, 0)
		}
		target.breaks = append(target.breaks, f.jump())
	case exitContinue:
		target.continues = append(target.continues, f.jump())
	case exitReturn:
		if f.node != nil && f.node.DerivedConstructor {
			b, _ := f.lookupLocal("this")
			f.emit(bytecode.OpReturnDerived, value, b.reg, 0)
			return
		}
		f.emit(bytecode.OpReturn, value, 0, 0)
	}
}

func (f *fn) addHandler(start, end, target, reg int32, finally bool) {
	f.f.Handlers = append(f.f.Handlers, bytecode.Handler{
		Start:   start,
		End:     end,
		Target:  target,
		Reg:     reg,
		Finally: finally,
		Scopes:  int32(f.withDepth),
	})
}

// stmt compiles the statement s. labels are the labels directly in front of it.
func (f *fn) stmt(s ast.Stmt, labels []string) {
	mark := f.top
	defer f.free(mark)
	f.setLine(s)
	switch s := s.(type) {
	case *ast.ExprStmt:
		if f.completion >= 0 {
			f.expr(s.Expr, f.completion)
		} else {
			f.effect(s.Expr)
		}
	case *ast.VarDecl:
		f.varDecl(s)
	case *ast.FuncDecl:
		if s.AnnexBHoist {
			f.annexBHoist(s)
		}
	case *ast.ClassDecl:
		name := s.Class.Name.Name
		t := f.newReg()
		f.class(s.Class, t, name)
		f.initBinding(s, name, t)
	case *ast.BlockStmt:
		f.block(s, labels)
	case *ast.EmptyStmt, *ast.DebuggerStmt:
	case *ast.IfStmt:
		no := f.jumpIfFalse(s.Test)
		f.stmt(s.Consequent, nil)
		if s.Alternate == nil {
			f.patchAll(no)
			break
		}
		end := f.jump()
		f.patchAll(no)
		f.stmt(s.Alternate, nil)
		f.patch(end)
	case *ast.ForStmt:
		f.forStmt(s, labels)
	case *ast.ForInStmt:
		f.forIn(s, labels)
	case *ast.ForOfStmt:
		f.forOf(s, labels)
	case *ast.WhileStmt:
		c := f.pushLoop(labels)
		top := f.pc()
		exit := f.jumpIfFalse(s.Test)
		f.stmt(s.Body, nil)
		f.patchAll(c.continues)
		f.jumpTo(top)
		f.patchAll(exit)
		f.popLoop(c)
	case *ast.DoWhileStmt:
		c := f.pushLoop(labels)
		top := f.pc()
		f.stmt(s.Body, nil)
		f.patchAll(c.continues)
		exit := f.jumpIfFalse(s.Test)
		f.jumpTo(top)
		f.patchAll(exit)
		f.popLoop(c)
	case *ast.ReturnStmt:
		v := f.newReg()
		if s.Arg != nil {
			f.expr(s.Arg, v)
		} else {
			f.emit(bytecode.OpLoadUndef, v, 0, 0)
		}
		f.exit(exitReturn, nil, v)
	case *ast.BreakStmt:
		f.exit(exitBreak, f.findTarget(s, s.Label, false), 0)
	case *ast.ContinueStmt:
		f.exit(exitContinue, f.findTarget(s, s.Label, true), 0)
	case *ast.ThrowStmt:
		v := f.exprAny(s.Arg)
		f.emit(bytecode.OpThrow, v, 0, 0)
	case *ast.TryStmt:
		f.try(s)
	case *ast.SwitchStmt:
		f.switchStmt(s, labels)
	case *ast.LabeledStmt:
		labels = append(labels, s.Label.Name)
		switch s.Body.(type) {
		case *ast.ForStmt, *ast.ForInStmt, *ast.ForOfStmt, *ast.WhileStmt, *ast.DoWhileStmt, *ast.LabeledStmt, *ast.SwitchStmt, *ast.BlockStmt:
			f.stmt(s.Body, labels)
		default:
			c := &ctl{
				kind:   ctlLoop,
				labels: labels,
			}
			f.control = append(f.control, c)
			f.stmt(s.Body, nil)
			f.popLoop(c)
		}
	case *ast.WithStmt:
		v := f.exprAny(s.Object)
		f.emit(bytecode.OpWith, v, 0, 0)
		f.withDepth++
		f.control = append(f.control, &ctl{kind: ctlWith})
		start := f.pc()
		f.stmt(s.Body, nil)
		end := f.pc()
		f.control = f.control[:len(f.control)-1]
		f.withDepth--
		f.emit(bytecode.OpPopScope, 0, 0, 0)
		_ = start
		_ = end
	default:
		f.c.fail(s, "unsupported statement %T", s)
	}
}

func (f *fn) pushLoop(labels []string) *ctl {
	c := &ctl{
		kind:   ctlLoop,
		labels: labels,
		isLoop: true,
	}
	f.control = append(f.control, c)
	return c
}

// popLoop ends the construct c: the break jumps continue here.
func (f *fn) popLoop(c *ctl) {
	f.control = f.control[:len(f.control)-1]
	f.patchAll(c.breaks)
}

func (f *fn) block(s *ast.BlockStmt, labels []string) {
	var c *ctl
	if len(labels) > 0 {
		c = &ctl{
			kind:   ctlLoop,
			labels: labels,
		}
		f.control = append(f.control, c)
	}
	scoped := f.enterScope(s.Scope, false)
	f.stmts(s.Body)
	if scoped {
		f.leaveScope()
	}
	if c != nil {
		f.popLoop(c)
	}
}

// annexBHoist assigns the function that the block-level declaration s declares to the
// variable of the same name of the enclosing function or script.
func (f *fn) annexBHoist(s *ast.FuncDecl) {
	name := s.Func.Name.Name
	inner, _ := f.lookupLocal(name)
	if inner == nil {
		return
	}
	root := f.scope
	for root.parent != nil {
		root = root.parent
	}
	if outer, ok := root.vars[name]; ok {
		if outer != inner {
			f.emit(bytecode.OpMove, outer.reg, inner.reg, 0)
		}
		return
	}
	if f.isScript {
		k := f.site(name)
		f.emit(bytecode.OpSetGlobal, inner.reg, k, k)
	}
}

func (f *fn) forStmt(s *ast.ForStmt, labels []string) {
	scoped := f.enterScope(s.Scope, false)
	switch init := s.Init.(type) {
	case nil:
	case *ast.VarDecl:
		if init != nil {
			f.varDecl(init)
		}
	case ast.Expr:
		f.effect(init)
	}
	c := f.pushLoop(labels)
	top := f.pc()
	var exit []int
	if s.Test != nil {
		exit = f.jumpIfFalse(s.Test)
	}
	f.stmt(s.Body, nil)
	f.patchAll(c.continues)
	if scoped {
		// Every iteration has its own copy of the loop variables.
		f.emit(bytecode.OpCloseUpvals, f.scope.firstReg, 0, 0)
	}
	if s.Update != nil {
		f.effect(s.Update)
	}
	f.jumpTo(top)
	f.patchAll(exit)
	f.popLoop(c)
	if scoped {
		f.leaveScope()
	}
}

// bindLoopVar stores the register v to the variable of a for-in or for-of loop.
func (f *fn) bindLoopVar(decl *ast.VarDecl, target ast.Pattern, v int32) {
	if decl != nil {
		f.destructure(decl.Decls[0].Target, v, declMode(decl.Kind))
		return
	}
	f.destructure(target, v, patAssign)
}

func (f *fn) forIn(s *ast.ForInStmt, labels []string) {
	it := f.newReg()
	{
		// The loop variables are in their dead zone while the object expression is evaluated.
		scoped := f.enterScope(s.Scope, false)
		if s.Decl != nil && s.Decl.Kind == ast.DeclVar && s.Decl.Decls[0].Init != nil {
			f.varDecl(s.Decl)
		}
		src := f.exprAny(s.Right)
		f.emit(bytecode.OpForIn, it, src, 0)
		if scoped {
			f.leaveScope()
		}
	}
	c := f.pushLoop(labels)
	top := f.pc()
	scoped := f.enterScope(s.Scope, false)
	v := f.newReg()
	next := f.emit(bytecode.OpForInNext, v, it, 0)
	f.bindLoopVar(s.Decl, s.Target, v)
	f.stmt(s.Body, nil)
	f.patchAll(c.continues)
	if scoped {
		f.leaveScope()
	}
	f.jumpTo(top)
	f.patchC(next)
	f.popLoop(c)
}

func (f *fn) forOf(s *ast.ForOfStmt, labels []string) {
	it := f.newRegs(2)
	{
		scoped := f.enterScope(s.Scope, false)
		src := f.exprAny(s.Right)
		async := int32(0)
		if s.Await {
			async = 1
		}
		f.emit(bytecode.OpGetIterator, it, src, async)
		if scoped {
			f.leaveScope()
		}
	}
	if s.Await {
		f.c.fail(s, "for await is not supported yet")
	}
	c := f.pushLoop(labels)
	c.hasIter = true
	c.reg = it
	top := f.pc()
	base := f.top
	scoped := f.enterScope(s.Scope, false)
	v := f.newReg()
	next := f.emit(bytecode.OpIterNext, v, it, 0)
	start := f.pc()
	f.bindLoopVar(s.Decl, s.Target, v)
	f.stmt(s.Body, nil)
	f.patchAll(c.continues)
	if scoped {
		f.leaveScope()
	}
	end := f.pc()
	f.jumpTo(top)

	// An exception in the body closes the iterator.
	exc := f.newReg()
	f.addHandler(start, end, f.pc(), exc, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	f.emit(bytecode.OpIterClose, it, 1, 0)
	f.emit(bytecode.OpRethrow, exc, 0, 0)

	f.patchC(next)
	f.popLoop(c)
}

func (f *fn) switchStmt(s *ast.SwitchStmt, labels []string) {
	d := f.newReg()
	f.expr(s.Discriminant, d)
	c := &ctl{
		kind:     ctlLoop,
		labels:   labels,
		isSwitch: true,
	}
	f.control = append(f.control, c)
	scoped := f.enterScope(s.Scope, true)

	jumps := make([]int, len(s.Cases))
	def := -1
	for i := range s.Cases {
		cs := &s.Cases[i]
		if cs.Test == nil {
			def = i
			continue
		}
		mark := f.top
		t := f.exprAny(cs.Test)
		eq := f.newReg()
		f.emit(bytecode.OpStrictEq, eq, d, t)
		jumps[i] = f.emit(bytecode.OpJumpIf, 0, eq, 0)
		f.free(mark)
	}
	miss := f.jump()
	for i := range s.Cases {
		if i == def {
			f.patch(miss)
			miss = -1
		} else {
			f.patch(jumps[i])
		}
		f.stmts(s.Cases[i].Body)
	}
	if miss >= 0 {
		f.patch(miss)
	}
	if scoped {
		f.leaveScope()
	}
	f.popLoop(c)
}

func (f *fn) try(s *ast.TryStmt) {
	var fin *ctl
	if s.Finalizer != nil {
		fin = &ctl{
			kind:     ctlFinally,
			kindReg:  f.newReg(),
			valueReg: f.newReg(),
		}
		f.control = append(f.control, fin)
	}
	base := f.top
	start := f.pc()
	f.block(s.Block, nil)
	end := f.pc()

	var done []int
	if s.Handler != nil {
		done = append(done, f.jump())
		sc := f.pushScope()
		f.control = append(f.control, &ctl{
			kind: ctlScope,
			reg:  sc.firstReg,
		})
		exc := f.newReg()
		f.addHandler(start, end, f.pc(), exc, false)
		f.emit(bytecode.OpCloseUpvals, base, 0, 0)
		if s.Param != nil {
			if s.CatchScope != nil {
				for _, d := range s.CatchScope.Decls {
					b := f.declare(d.Name, bindLet)
					b.tdz = false
				}
			}
			f.destructure(s.Param, exc, patInit)
		}
		f.block(s.Handler, nil)
		f.leaveScope()
	}
	if fin == nil {
		f.patchAll(done)
		return
	}

	f.control = f.control[:len(f.control)-1]
	covered := f.pc()
	f.patchAll(done)
	f.emit(bytecode.OpLoadInt, fin.kindReg, 0, 0)
	normal := f.jump()

	// An exception from the try or the catch block: remember it and run the finally block.
	f.addHandler(start, covered, f.pc(), fin.valueReg, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	f.emit(bytecode.OpLoadInt, fin.kindReg, 1, 0)

	f.patch(normal)
	f.patchAll(fin.toFinal)
	saved := f.completion
	if saved >= 0 {
		// The value of a finally block is not the value of the statement.
		f.completion = f.newReg()
	}
	f.block(s.Finalizer, nil)
	f.completion = saved

	// Continue with whatever led to the finally block.
	completed := f.emit(bytecode.OpJumpIfInt, 0, fin.kindReg, 0)
	dispatch := make([]int, len(fin.pending))
	for i, p := range fin.pending {
		dispatch[i] = f.emit(bytecode.OpJumpIfInt, 0, fin.kindReg, p.id)
	}
	f.emit(bytecode.OpRethrow, fin.valueReg, 0, 0)
	for i, p := range fin.pending {
		f.patch(dispatch[i])
		f.exit(p.kind, p.target, fin.valueReg)
	}
	f.patch(completed)
}
