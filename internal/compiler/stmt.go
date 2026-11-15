// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"slices"

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
		case ast.DeclLet, ast.DeclConst, ast.DeclClass, ast.DeclUsing, ast.DeclAwaitUsing:
			kind := bindLet
			switch d.Kind {
			case ast.DeclConst, ast.DeclUsing, ast.DeclAwaitUsing:
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
	b.unset = false
	b.initialized = true
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
			scope := f.resolveName(id.Name)
			t := f.newReg()
			f.named(d.Init, t, id.Name)
			f.storeVarTo(d, id.Name, t, false, scope)
		case n.Kind == ast.DeclUsing || n.Kind == ast.DeclAwaitUsing:
			t := f.newReg()
			f.named(d.Init, t, id.Name)
			f.initUsing(d, n.Kind, id.Name, t)
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
				b.initialized = true
			case singleWrite(d.Init):
				f.named(d.Init, b.reg, id.Name)
				b.tdz = false
				b.initialized = true
			default:
				t := f.newReg()
				f.named(d.Init, t, id.Name)
				f.emit(bytecode.OpMove, b.reg, t, 0)
				b.tdz = false
				b.initialized = true
			}
		}
		f.free(mark)
	}
}

// initUsing adds the value of the register src as a resource of the using or await using
// declaration of the kind k, and then initializes the binding name with it.
func (f *fn) initUsing(n ast.Node, k ast.DeclKind, name string, src int32) {
	b, sc := f.lookupLocal(name)
	if b == nil && f.module {
		// A declaration of the top level of a module, whose binding is an upvalue.
		sc = f.varScope
	}
	if sc == nil || !sc.hasDispose {
		f.c.fail(n, "using declaration outside of a disposing scope")
	}
	hint := int32(bytecode.DisposeSync)
	if k == ast.DeclAwaitUsing {
		hint = bytecode.DisposeAsync
	}
	f.emit(bytecode.OpUsing, src, sc.dispose, hint)
	sc.used = true
	if b == nil {
		f.storeVar(n, name, src, true)
		return
	}
	f.emit(bytecode.OpMove, b.reg, src, 0)
	b.tdz = false
	b.initialized = true
}

func declMode(k ast.DeclKind) patMode {
	if k == ast.DeclVar {
		return patAssign
	}
	return patInit
}

// findTarget returns the construct that a break or continue with the given label targets.
func (f *fn) findTarget(n ast.Node, label *ast.Identifier, isContinue bool) *ctl {
	for _, c := range slices.Backward(f.control) {
		if c.kind != ctlLoop {
			continue
		}
		if label == nil {
			if c.isLoop || (!isContinue && c.isSwitch) {
				return c
			}
			continue
		}
		if slices.Contains(c.labels, label.Name) {
			return c
		}
	}
	f.c.fail(n, "no target for the jump")
	return nil
}

// exit emits the jump out of the constructs above target on the control stack.
// For exitReturn, target is nil and value is the register of the result.
func (f *fn) exit(kind exitKind, target *ctl, value int32) {
	for _, c := range slices.Backward(f.control) {
		if c == target {
			break
		}
		switch c.kind {
		case ctlLoop:
			if c.hasIter {
				f.iterClose(c)
			}
		case ctlScope:
			f.emit(bytecode.OpCloseUpvals, c.reg, 0, 0)
		case ctlWith:
			f.emit(bytecode.OpPopScope, 0, 0, 0)
		case ctlFinalizer:
			// A break or continue out of a finally block carries the value of the block.
			if kind != exitReturn {
				f.emit(bytecode.OpMove, c.valueReg, c.reg, 0)
			}
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
			f.iterClose(target)
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

// iterClose closes the iterator of the for-of or for-await loop c that is left normally.
func (f *fn) iterClose(c *ctl) {
	if c.asyncIter {
		f.asyncIterClose(c.reg)
		return
	}
	f.emit(bytecode.OpIterClose, c.reg, 0, 0)
}

// asyncIterClose closes the async iterator it, it+1 for a normal completion, as
// AsyncIteratorClose does.
func (f *fn) asyncIterClose(it int32) {
	mark := f.top
	r := f.newRegs(2)
	f.emit(bytecode.OpIterReturn, r, it, 0)
	skip := f.emit(bytecode.OpJumpIfNot, 0, r+1, 0)
	f.emit(bytecode.OpAwait, r, r, 0)
	f.emit(bytecode.OpIterCheckResult, r, 0, 0)
	f.patch(skip)
	f.free(mark)
}

// usingKinds reports whether the scope s, which may be nil, has using declarations, and
// whether any of them is an await using declaration.
func usingKinds(s *ast.Scope) (using, async bool) {
	if s == nil {
		return false, false
	}
	for _, d := range s.Decls {
		switch d.Kind {
		case ast.DeclUsing:
			using = true
		case ast.DeclAwaitUsing:
			using, async = true, true
		}
	}
	return using, async
}

// disposing compiles body so that every way out of it disposes the resources that the using
// declarations of the current scope add, as DisposeResources does. async reports whether the
// disposal awaits.
func (f *fn) disposing(async bool, body func()) {
	sc := f.scope
	sc.dispose = f.newReg()
	sc.hasDispose = true
	f.emit(bytecode.OpLoadUndef, sc.dispose, 0, 0)
	fin := &ctl{
		kind:     ctlFinally,
		kindReg:  f.newReg(),
		valueReg: f.newReg(),
		disposes: sc,
	}
	v := f.newReg()
	f.control = append(f.control, fin)
	base := f.top
	start := f.pc()
	body()
	f.control = f.control[:len(f.control)-1]
	f.enterFinally(fin, start, base, nil)

	f.emit(bytecode.OpDisposeBegin, sc.dispose, fin.valueReg, fin.kindReg)
	top := f.pc()
	step := f.emit(bytecode.OpDisposeStep, v, sc.dispose, 0)
	if async {
		awaitStart := f.pc()
		f.emit(bytecode.OpAwait, v, v, 0)
		awaitEnd := f.pc()
		f.jumpTo(top)
		f.addHandler(awaitStart, awaitEnd, f.pc(), v, false)
		f.emit(bytecode.OpDisposeError, sc.dispose, v, 0)
		f.jumpTo(top)
	}
	f.patchC(step)
	f.emit(bytecode.OpDisposeEnd, sc.dispose, 0, 0)
	sc.hasDispose = false
	f.leaveFinally(fin)
}

// enterFinally ends the region that the finally block of fin covers, which starts at start, and
// starts the code that runs for every completion of the region. base is the first register of
// the region. done lists the jumps that complete the region normally.
func (f *fn) enterFinally(fin *ctl, start int32, base int32, done []int) {
	covered := f.pc()
	f.patchAll(done)
	f.emit(bytecode.OpLoadInt, fin.kindReg, 0, 0)
	normal := f.jump()

	// An exception from the region: remember it and run the finally block.
	f.addHandler(start, covered, f.pc(), fin.valueReg, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	f.emit(bytecode.OpLoadInt, fin.kindReg, 1, 0)

	f.patch(normal)
	f.patchAll(fin.toFinal)
}

// leaveFinally continues after the finally block of fin with whatever led to it.
func (f *fn) leaveFinally(fin *ctl) {
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
	f.setPos(s.Start())
	switch s.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.ForInStmt, *ast.ForOfStmt, *ast.WhileStmt, *ast.DoWhileStmt,
		*ast.SwitchStmt, *ast.TryStmt, *ast.WithStmt:
		// The value of these statements is undefined unless a statement in them has a value.
		if f.completion >= 0 {
			f.emit(bytecode.OpLoadUndef, f.completion, 0, 0)
		}
	}
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
		f.class(s.Class, t, name, -1)
		f.initBinding(s, name, t)
	case *ast.ImportDecl, *ast.ExportAllDecl:
	case *ast.ExportNamedDecl:
		if s.Decl != nil {
			f.stmt(s.Decl, nil)
		}
	case *ast.ExportDefaultDecl:
		f.exportDefault(s)
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
			if f.canTailCall() {
				f.markTails(s.Arg)
			}
			f.expr(s.Arg, v)
			f.tails = f.tails[:0]
			if f.f.IsAsync && f.f.IsGenerator {
				f.emit(bytecode.OpAwait, v, v, 0)
			}
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
	if using, async := usingKinds(s.Scope); using {
		f.disposing(async, func() {
			f.stmts(s.Body)
		})
	} else {
		f.stmts(s.Body)
	}
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
	if f.eval != nil && !f.f.Strict {
		if !f.eval.hoisted[name] {
			return
		}
		if f.eval.scope.VarDepth == 0 {
			f.setAnnexBGlobal(name, inner.reg)
			return
		}
		f.storeEvalVar(name, inner.reg)
		return
	}
	if outer, ok := f.varScope.vars[name]; ok {
		if outer != inner {
			f.emit(bytecode.OpMove, outer.reg, inner.reg, 0)
		}
		return
	}
	if f.isScript {
		f.setAnnexBGlobal(name, inner.reg)
	}
}

// setAnnexBGlobal assigns the register src to the global variable name of a block-level function,
// unless the top-level code found that the variable could not be declared.
func (f *fn) setAnnexBGlobal(name string, src int32) {
	j := -1
	if r, ok := f.annexBRegs[name]; ok {
		j = f.emit(bytecode.OpJumpIfNot, 0, r, 0)
	}
	k := f.site(name)
	f.emit(bytecode.OpSetGlobal, src, k, k)
	if j >= 0 {
		f.patch(j)
	}
}

func (f *fn) forStmt(s *ast.ForStmt, labels []string) {
	scoped := f.enterScope(s.Scope, false)
	if using, async := usingKinds(s.Scope); using {
		f.disposing(async, func() {
			f.forLoop(s, labels, scoped)
		})
	} else {
		f.forLoop(s, labels, scoped)
	}
	if scoped {
		f.leaveScope()
	}
}

// forLoop compiles the for statement s in its scope, if scoped.
func (f *fn) forLoop(s *ast.ForStmt, labels []string, scoped bool) {
	switch init := s.Init.(type) {
	case nil:
	case *ast.VarDecl:
		if init != nil {
			f.varDecl(init)
		}
	case ast.Expr:
		f.effect(init)
	}
	if scoped && f.scope.captured {
		// The first iteration has its own copy too, apart from the closures of the
		// initializers.
		f.emit(bytecode.OpCloseUpvals, f.scope.firstReg, 0, 0)
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
}

// bindLoopVar stores the register v to the variable of a for-in or for-of loop.
func (f *fn) bindLoopVar(decl *ast.VarDecl, target ast.Pattern, v int32) {
	if decl != nil {
		if decl.Kind == ast.DeclUsing || decl.Kind == ast.DeclAwaitUsing {
			d := &decl.Decls[0]
			f.initUsing(d, decl.Kind, d.Target.(*ast.Identifier).Name, v)
			return
		}
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
		kind := int32(bytecode.IterSync)
		if s.Await {
			kind = bytecode.IterAsync
		}
		f.emit(bytecode.OpGetIterator, it, src, kind)
		if scoped {
			f.leaveScope()
		}
	}
	if s.Await {
		f.forAwaitOf(s, labels, it)
		return
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
	f.loopBody(s, v)
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
	f.emit(bytecode.OpIterClose, it, exc+1, 0)
	f.emit(bytecode.OpRethrow, exc, 0, 0)

	f.patchC(next)
	f.popLoop(c)
}

// loopBody compiles one iteration of the for-of or for-await statement s for the value v.
func (f *fn) loopBody(s *ast.ForOfStmt, v int32) {
	if using, async := usingKinds(s.Scope); using {
		f.disposing(async, func() {
			f.bindLoopVar(s.Decl, s.Target, v)
			f.stmt(s.Body, nil)
		})
		return
	}
	f.bindLoopVar(s.Decl, s.Target, v)
	f.stmt(s.Body, nil)
}

// forAwaitOf compiles the loop of a for-await statement over the async iterator it, it+1.
func (f *fn) forAwaitOf(s *ast.ForOfStmt, labels []string, it int32) {
	c := f.pushLoop(labels)
	c.hasIter = true
	c.asyncIter = true
	c.reg = it
	top := f.pc()
	base := f.top
	scoped := f.enterScope(s.Scope, false)
	v := f.newReg()
	call := f.newRegs(2)
	f.emit(bytecode.OpMove, call, it+1, 0)
	f.emit(bytecode.OpMove, call+1, it, 0)
	f.emit(bytecode.OpCall, call, 0, 0)
	f.emit(bytecode.OpAwait, call, call, 0)
	next := f.emit(bytecode.OpIterResultValue, v, call, 0)
	f.free(call)
	start := f.pc()
	f.loopBody(s, v)
	f.patchAll(c.continues)
	if scoped {
		f.leaveScope()
	}
	end := f.pc()
	f.jumpTo(top)

	// An exception in the body closes the iterator. An exception of closing it is dropped,
	// unless the generator is returning, which the exception then replaces.
	exc := f.newReg()
	f.addHandler(start, end, f.pc(), exc, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	closeExc := f.newReg()
	closeStart := f.pc()
	f.asyncIterClose(it)
	closeEnd := f.pc()
	f.emit(bytecode.OpRethrow, exc, 0, 0)
	f.addHandler(closeStart, closeEnd, f.pc(), closeExc, false)
	f.emit(bytecode.OpRethrow, exc, closeExc+1, 0)

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
	if s.Handler != nil {
		f.catchDepth++
	}
	f.block(s.Block, nil)
	if s.Handler != nil {
		f.catchDepth--
	}
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
					b.catch = true
				}
			}
			f.destructure(s.Param, exc, patInit)
		}
		if f.completion >= 0 {
			// The value of the statement is undefined unless the catch block has a value.
			f.emit(bytecode.OpLoadUndef, f.completion, 0, 0)
		}
		f.block(s.Handler, nil)
		f.leaveScope()
	}
	if fin == nil {
		f.patchAll(done)
		return
	}

	f.control = f.control[:len(f.control)-1]
	f.enterFinally(fin, start, base, done)
	saved := f.completion
	if saved >= 0 {
		// The value of a finally block is the value of the statement only when the block
		// breaks or continues.
		f.completion = f.newReg()
		f.emit(bytecode.OpLoadUndef, f.completion, 0, 0)
		f.control = append(f.control, &ctl{
			kind:     ctlFinalizer,
			reg:      f.completion,
			valueReg: saved,
		})
	}
	f.block(s.Finalizer, nil)
	if saved >= 0 {
		f.control = f.control[:len(f.control)-1]
	}
	f.completion = saved
	f.leaveFinally(fin)
}

// canTailCall reports whether a call that ends a return statement or the body of an arrow
// function at the current point is in tail position, as IsInTailPosition defines it.
func (f *fn) canTailCall() bool {
	if !f.f.Strict || f.f.IsGenerator || f.f.IsAsync || f.node == nil || f.node.ClassConstructor || f.catchDepth > 0 {
		return false
	}
	for _, c := range f.control {
		if c.hasIter {
			return false
		}
		// A finally block, or using declarations that precede the call.
		if c.kind == ctlFinally && (c.disposes == nil || c.disposes.used) {
			return false
		}
	}
	return true
}

// markTails records the calls in tail position of e, an expression in tail position.
func (f *fn) markTails(e ast.Expr) {
	switch e := e.(type) {
	case *ast.CallExpr:
		if _, ok := e.Callee.(*ast.SuperExpr); !ok {
			f.tails = append(f.tails, e)
		}
	case *ast.TaggedTemplate:
		f.tails = append(f.tails, e)
	case *ast.CondExpr:
		f.markTails(e.Consequent)
		f.markTails(e.Alternate)
	case *ast.LogicalExpr:
		f.markTails(e.Right)
	case *ast.SequenceExpr:
		f.markTails(e.Exprs[len(e.Exprs)-1])
	case *ast.OptionalChain:
		if c, ok := e.Expr.(*ast.CallExpr); ok {
			f.tails = append(f.tails, c)
		}
	}
}

// isTail reports whether the call e is in tail position.
func (f *fn) isTail(e ast.Expr) bool {
	return len(f.tails) > 0 && slices.Contains(f.tails, e)
}
