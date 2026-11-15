// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// stmtCtx is the position that a statement is parsed in.
type stmtCtx uint8

const (
	// ctxList is a statement list, where every declaration is allowed.
	ctxList stmtCtx = iota
	// ctxSingle is the body of a loop, a with statement, or a labelled statement that is itself such a body.
	ctxSingle
	// ctxIf is a branch of an if statement.
	ctxIf
	// ctxLabel is the body of a labelled statement of a statement list.
	ctxLabel
)

func (p *parser) parseStatementList(body []ast.Stmt, end tokKind) []ast.Stmt {
	mark := p.stmts.mark()
	for _, s := range body {
		p.stmts.push(s)
	}
	for p.t.kind != end {
		if p.t.kind == tEOF {
			p.unexpected()
		}
		p.stmts.push(p.parseStatement(ctxList))
	}
	return p.stmts.take(mark)
}

func (p *parser) atModuleTop() bool {
	return p.module && p.scope.parent == nil
}

func (p *parser) parseStatement(ctx stmtCtx) ast.Stmt {
	p.enter()
	s := p.parseStatementInner(ctx)
	p.leave()
	return s
}

func (p *parser) parseStatementInner(ctx stmtCtx) ast.Stmt {
	run := p.labelRun
	p.labelRun = 0
	start := p.t.start

	switch p.t.kind {
	case tLBrace:
		return p.parseBlock(false)
	case tSemi:
		p.next()
		return &ast.EmptyStmt{
			Span: span(start, p.prevEnd),
		}
	case tVar:
		d := p.parseVarDecl(declVar, false)
		p.consumeSemicolon()
		return d
	case tConst:
		if ctx != ctxList {
			p.failTok("lexical declaration is not allowed here")
		}
		d := p.parseVarDecl(declConst, false)
		p.consumeSemicolon()
		return d
	case tFunction:
		switch {
		case ctx == ctxList:
			return p.parseFunctionDecl(start, false, ctx, false)
		case p.f.strict || ctx == ctxSingle:
			p.failTok("function declaration is not allowed here")
		case ctx == ctxLabel:
			return p.parseFunctionDecl(start, false, ctx, false)
		}
		scope := p.pushScope(ast.ScopeBlock, false).ast
		d := p.parseFunctionDecl(start, false, ctx, false)
		p.popScope()
		return newNode(&p.nodes.block, ast.BlockStmt{
			Span:  d.Span,
			Body:  []ast.Stmt{d},
			Scope: scope,
		})
	case tClass:
		if ctx != ctxList {
			p.failTok("class declaration is not allowed here")
		}
		cls := p.parseClass(true, false)
		return &ast.ClassDecl{
			Span:  cls.Span,
			Class: cls,
		}
	case tAt:
		if ctx != ctxList {
			p.failTok("class declaration is not allowed here")
		}
		decorators := p.parseDecorators()
		if p.t.kind == tExport && p.atModuleTop() {
			return p.parseDecoratedExport(decorators)
		}
		if p.t.kind != tClass {
			p.unexpected()
		}
		cls := p.parseClass(true, false)
		cls.Decorators = decorators
		cls.StartOff = start
		return &ast.ClassDecl{
			Span:  cls.Span,
			Class: cls,
		}
	case tIf:
		p.next()
		p.expect(tLParen)
		test := p.parseExpressionIn()
		p.expect(tRParen)
		cons := p.parseStatement(ctxIf)
		var alt ast.Stmt
		if p.eat(tElse) {
			alt = p.parseStatement(ctxIf)
		}
		return newNode(&p.nodes.ifStmt, ast.IfStmt{
			Span:       span(start, p.prevEnd),
			Test:       test,
			Consequent: cons,
			Alternate:  alt,
		})
	case tFor:
		p.markLoopLabels(run)
		return p.parseFor()
	case tWhile:
		p.markLoopLabels(run)
		p.next()
		p.expect(tLParen)
		test := p.parseExpressionIn()
		p.expect(tRParen)
		body := p.parseLoopBody()
		return &ast.WhileStmt{
			Span: span(start, p.prevEnd),
			Test: test,
			Body: body,
		}
	case tDo:
		p.markLoopLabels(run)
		p.next()
		body := p.parseLoopBody()
		p.expect(tWhile)
		p.expect(tLParen)
		test := p.parseExpressionIn()
		p.expect(tRParen)
		p.eat(tSemi)
		return &ast.DoWhileStmt{
			Span: span(start, p.prevEnd),
			Body: body,
			Test: test,
		}
	case tReturn:
		if !p.f.inFunction {
			p.failTok("return is not allowed outside of a function")
		}
		p.next()
		var arg ast.Expr
		if !p.t.nl && p.t.kind != tSemi && p.t.kind != tRBrace && p.t.kind != tEOF {
			arg = p.parseExpressionIn()
		}
		p.consumeSemicolon()
		return newNode(&p.nodes.ret, ast.ReturnStmt{
			Span: span(start, p.prevEnd),
			Arg:  arg,
		})
	case tBreak, tContinue:
		return p.parseBreakContinue()
	case tThrow:
		p.next()
		if p.t.nl {
			p.failTok("line break is not allowed after throw")
		}
		arg := p.parseExpressionIn()
		p.consumeSemicolon()
		return &ast.ThrowStmt{
			Span: span(start, p.prevEnd),
			Arg:  arg,
		}
	case tTry:
		return p.parseTry()
	case tSwitch:
		return p.parseSwitch()
	case tWith:
		if p.f.strict {
			p.failTok("with statement is not allowed in strict mode")
		}
		p.next()
		p.expect(tLParen)
		obj := p.parseExpressionIn()
		p.expect(tRParen)
		body := p.parseStatement(ctxSingle)
		return &ast.WithStmt{
			Span:   span(start, p.prevEnd),
			Object: obj,
			Body:   body,
		}
	case tDebugger:
		p.next()
		p.consumeSemicolon()
		return &ast.DebuggerStmt{
			Span: span(start, p.prevEnd),
		}
	case tImport:
		if pk := p.peek(); pk.kind != tLParen && pk.kind != tDot {
			if ctx != ctxList || !p.atModuleTop() {
				p.failTok("import declaration is only allowed at the top level of a module")
			}
			return p.parseImport()
		}
	case tExport:
		if ctx != ctxList || !p.atModuleTop() {
			p.failTok("export declaration is only allowed at the top level of a module")
		}
		return p.parseExport()
	case tIdent:
		if p.t.esc {
			break
		}
		switch p.t.str {
		case "let":
			pk := p.peek()
			if pk.kind != tIdent && pk.kind != tLBrack && pk.kind != tLBrace {
				break
			}
			if ctx == ctxList {
				d := p.parseVarDecl(declLet, false)
				p.consumeSemicolon()
				return d
			}
			if pk.kind == tLBrack {
				p.failTok("lexical declaration is not allowed here")
			}
		case "using":
			if !p.atUsingDecl(false) {
				break
			}
			p.checkUsingAllowed(ctx)
			d := p.parseVarDecl(declUsing, false)
			p.consumeSemicolon()
			return d
		case "await":
			if !p.atUsingDecl(true) {
				break
			}
			p.checkUsingAllowed(ctx)
			d := p.parseVarDecl(declAwaitUsing, false)
			p.consumeSemicolon()
			return d
		case "async":
			if pk := p.peek(); pk.kind == tFunction && !pk.nl {
				if ctx != ctxList {
					p.failTok("async function declaration is not allowed here")
				}
				p.next()
				return p.parseFunctionDecl(start, true, ctx, false)
			}
		}
	}

	var e ast.Expr
	if p.t.kind == tIdent && p.t.str == "arguments" && p.peek().kind == tColon {
		// A label is not an identifier reference, so the restrictions on arguments do not apply.
		e = newNode(&p.nodes.identifier, ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		})
		p.next()
	} else {
		e = p.parseExpressionIn()
	}
	if id, ok := e.(*ast.Identifier); ok && p.t.kind == tColon && !id.Parenthesized && id.EndOff == p.prevEnd {
		for _, l := range p.labels[p.f.labelBase:] {
			if l.name == id.Name {
				p.fail(int(id.StartOff), "label '"+id.Name+"' has already been declared")
			}
		}
		p.labels = append(p.labels, label{
			name: id.Name,
		})
		p.next()
		p.labelRun = run + 1
		bodyCtx := ctxSingle
		if ctx == ctxList || ctx == ctxLabel {
			bodyCtx = ctxLabel
		}
		body := p.parseStatement(bodyCtx)
		p.labels = p.labels[:len(p.labels)-1]
		return &ast.LabeledStmt{
			Span:  span(start, p.prevEnd),
			Label: id,
			Body:  body,
		}
	}
	p.consumeSemicolon()
	return newNode(&p.nodes.exprStmt, ast.ExprStmt{
		Span: span(start, p.prevEnd),
		Expr: e,
	})
}

// atUsingDecl reports whether a using declaration, or an await using declaration when
// await is set, starts at the current token, which is the unescaped word using or await.
func (p *parser) atUsingDecl(await bool) bool {
	if !await {
		pk := p.peek()
		return pk.kind == tIdent && !pk.nl
	}
	if !p.f.await {
		return false
	}
	u, pk := p.peek2()
	return u.kind == tIdent && !u.esc && u.str == "using" && !u.nl && pk.kind == tIdent && !pk.nl
}

// checkUsingAllowed fails when a using declaration is not allowed in the statement position ctx.
func (p *parser) checkUsingAllowed(ctx stmtCtx) {
	switch {
	case ctx != ctxList:
		p.failTok("lexical declaration is not allowed here")
	case p.scope.ast.Kind == ast.ScopeSwitch:
		p.failTok("using declaration is not allowed directly in a case clause")
	case p.scope.ast.Kind == ast.ScopeScript && !p.f.inFunction:
		p.failTok("using declaration is not allowed at the top level of a script")
	}
}

// markLoopLabels marks the last run labels as labels of an iteration statement.
func (p *parser) markLoopLabels(run int) {
	for i := len(p.labels) - run; i < len(p.labels); i++ {
		p.labels[i].loop = true
	}
}

func (p *parser) parseLoopBody() ast.Stmt {
	p.f.loopDepth++
	p.f.breakDepth++
	body := p.parseStatement(ctxSingle)
	p.f.loopDepth--
	p.f.breakDepth--
	return body
}

// parseBlock parses a block. catchBody reports the block of a catch clause that has a parameter.
func (p *parser) parseBlock(catchBody bool) *ast.BlockStmt {
	start := p.t.start
	if p.t.kind != tLBrace {
		p.unexpected()
	}
	s := p.pushScope(ast.ScopeBlock, false)
	s.catchBody = catchBody
	p.next()
	b := newNode(&p.nodes.block, ast.BlockStmt{
		Scope: s.ast,
	})
	b.Body = p.parseStatementList(nil, tRBrace)
	p.popScope()
	p.next()
	b.Span = span(start, p.prevEnd)
	return b
}

// parseVarDecl parses a var, let or const declaration without its semicolon.
// inFor reports the head of a for statement, where the caller checks the initializers.
func (p *parser) parseVarDecl(mode declMode, inFor bool) *ast.VarDecl {
	d := newNode(&p.nodes.varDecl, ast.VarDecl{})
	d.StartOff = p.t.start
	switch mode {
	case declVar:
		d.Kind = ast.DeclVar
	case declLet:
		d.Kind = ast.DeclLet
	case declUsing:
		d.Kind = ast.DeclUsing
	case declAwaitUsing:
		d.Kind = ast.DeclAwaitUsing
		p.next()
	default:
		d.Kind = ast.DeclConst
	}
	p.next()
	for {
		start := p.t.start
		if (mode == declUsing || mode == declAwaitUsing) && p.t.kind != tIdent {
			p.failTok("a using declaration cannot have a binding pattern")
		}
		target := p.parseBindingTarget()
		p.declarePattern(target, mode)
		var init ast.Expr
		if p.eat(tAssign) {
			init = p.parseAssign()
		} else if !inFor {
			p.checkMissingInit(d.Kind, target)
		}
		d.Decls = append(d.Decls, ast.Declarator{
			Span:   span(start, p.prevEnd),
			Target: target,
			Init:   init,
		})
		if !p.eat(tComma) {
			break
		}
	}
	d.EndOff = p.prevEnd
	return d
}

func (p *parser) checkMissingInit(kind ast.DeclKind, target ast.Pattern) {
	switch kind {
	case ast.DeclConst:
		p.fail(int(target.Start()), "missing initializer in const declaration")
	case ast.DeclUsing, ast.DeclAwaitUsing:
		p.fail(int(target.Start()), "missing initializer in using declaration")
	}
	if _, ok := target.(*ast.Identifier); !ok {
		p.fail(int(target.Start()), "missing initializer in destructuring declaration")
	}
}

func (p *parser) parseFor() ast.Stmt {
	start := p.t.start
	p.next()
	var isAwait bool
	if p.isWord("await") {
		if !p.f.await {
			p.failTok("for await is only allowed in async functions and modules")
		}
		isAwait = true
		p.next()
	}
	p.expect(tLParen)
	scope := p.pushScope(ast.ScopeFor, false).ast

	var decl *ast.VarDecl
	var init ast.Expr
	var cover int32
	var startsWithLet, asyncOf bool
	p.f.noIn = true
	switch {
	case p.t.kind == tSemi:
	case p.t.kind == tVar:
		decl = p.parseVarDecl(declVar, true)
	case p.t.kind == tConst:
		decl = p.parseVarDecl(declConst, true)
	case p.isWord("using") && p.atForUsingDecl():
		decl = p.parseVarDecl(declUsing, true)
	case p.isWord("await") && p.atUsingDecl(true):
		decl = p.parseVarDecl(declAwaitUsing, true)
	case p.isWord("let"):
		if pk := p.peek(); pk.kind == tIdent || pk.kind == tLBrack || pk.kind == tLBrace {
			decl = p.parseVarDecl(declLet, true)
		} else {
			startsWithLet = true
			init, cover = p.parseAssignCover()
		}
	case isAwait && p.isWord("async"):
		if pk := p.peek(); pk.kind == tIdent && !pk.esc && pk.str == "of" {
			init = newNode(&p.nodes.identifier, ast.Identifier{
				Span: span(p.t.start, p.t.end),
				Name: p.t.str,
			})
			p.next()
		} else {
			init, cover = p.parseAssignCover()
		}
	default:
		asyncStart := p.isWord("async")
		init, cover = p.parseAssignCover()
		if id, ok := init.(*ast.Identifier); ok && asyncStart && !id.Parenthesized {
			asyncOf = true
		}
	}
	if init != nil && p.t.kind == tComma {
		if cover != 0 {
			p.fail(int(cover-1), "invalid shorthand property initializer")
		}
		exprs := []ast.Expr{init}
		for p.eat(tComma) {
			exprs = append(exprs, p.parseAssign())
		}
		init = &ast.SequenceExpr{
			Span:  span(exprs[0].Start(), p.prevEnd),
			Exprs: exprs,
		}
	}
	p.f.noIn = false

	isOf := p.isWord("of")
	if (isOf || p.t.kind == tIn) && (decl != nil || init != nil) {
		if isAwait && !isOf {
			p.failTok("for await requires an of clause")
		}
		var target ast.Pattern
		if decl != nil {
			if len(decl.Decls) != 1 {
				p.fail(int(decl.StartOff), "a for-in or for-of statement declares exactly one binding")
			}
			if !isOf && (decl.Kind == ast.DeclUsing || decl.Kind == ast.DeclAwaitUsing) {
				p.fail(int(decl.StartOff), "a for-in statement cannot declare a using binding")
			}
			d := &decl.Decls[0]
			if d.Init != nil {
				_, isIdent := d.Target.(*ast.Identifier)
				if isOf || p.f.strict || decl.Kind != ast.DeclVar || !isIdent {
					p.fail(int(d.Init.Start()), "a for-in or for-of binding cannot have an initializer")
				}
			}
		} else {
			if isOf && (startsWithLet || asyncOf) {
				p.fail(int(init.Start()), "the left side of a for-of statement cannot start with let or be async")
			}
			target = p.toAssignTarget(init)
		}
		p.next()
		var right ast.Expr
		if isOf {
			right = p.parseAssign()
		} else {
			right = p.parseExpression()
		}
		p.expect(tRParen)
		body := p.parseLoopBody()
		p.popScope()
		if isOf {
			return &ast.ForOfStmt{
				Span:   span(start, p.prevEnd),
				Decl:   decl,
				Target: target,
				Right:  right,
				Body:   body,
				Await:  isAwait,
				Scope:  scope,
			}
		}
		return &ast.ForInStmt{
			Span:   span(start, p.prevEnd),
			Decl:   decl,
			Target: target,
			Right:  right,
			Body:   body,
			Scope:  scope,
		}
	}

	if isAwait {
		p.failTok("for await requires an of clause")
	}
	if cover != 0 {
		p.fail(int(cover-1), "invalid shorthand property initializer")
	}
	f := &ast.ForStmt{
		Scope: scope,
	}
	if decl != nil {
		for i := range decl.Decls {
			if decl.Decls[i].Init == nil {
				p.checkMissingInit(decl.Kind, decl.Decls[i].Target)
			}
		}
		f.Init = decl
	} else if init != nil {
		f.Init = init
	}
	p.expect(tSemi)
	if p.t.kind != tSemi {
		f.Test = p.parseExpression()
	}
	p.expect(tSemi)
	if p.t.kind != tRParen {
		f.Update = p.parseExpression()
	}
	p.expect(tRParen)
	f.Body = p.parseLoopBody()
	p.popScope()
	f.Span = span(start, p.prevEnd)
	return f
}

// atForUsingDecl reports whether a using declaration starts at the current token, the unescaped
// word using, in the head of a for statement. using of is a declaration only when an
// initializer follows.
func (p *parser) atForUsingDecl() bool {
	pk, pk2 := p.peek2()
	if pk.kind != tIdent || pk.nl {
		return false
	}
	return pk.esc || pk.str != "of" || pk2.kind == tAssign
}

func (p *parser) parseBreakContinue() ast.Stmt {
	start := p.t.start
	isBreak := p.t.kind == tBreak
	p.next()
	var id *ast.Identifier
	if p.t.kind == tIdent && !p.t.nl {
		id = newNode(&p.nodes.identifier, ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		})
		p.checkIdent(id.Name, id.StartOff, false)
		found := false
		for _, l := range p.labels[p.f.labelBase:] {
			if l.name != id.Name {
				continue
			}
			if !isBreak && !l.loop {
				p.failTok("continue target '" + id.Name + "' is not an iteration statement")
			}
			found = true
			break
		}
		if !found {
			p.failTok("undefined label '" + id.Name + "'")
		}
		p.next()
	} else if isBreak && p.f.breakDepth == 0 {
		p.fail(int(start), "break is not allowed outside of a loop or a switch")
	} else if !isBreak && p.f.loopDepth == 0 {
		p.fail(int(start), "continue is not allowed outside of a loop")
	}
	p.consumeSemicolon()
	if isBreak {
		return &ast.BreakStmt{
			Span:  span(start, p.prevEnd),
			Label: id,
		}
	}
	return &ast.ContinueStmt{
		Span:  span(start, p.prevEnd),
		Label: id,
	}
}

func (p *parser) parseTry() ast.Stmt {
	t := &ast.TryStmt{}
	t.StartOff = p.t.start
	p.next()
	t.Block = p.parseBlock(false)
	if p.eat(tCatch) {
		if p.eat(tLParen) {
			s := p.pushScope(ast.ScopeCatch, false)
			t.CatchScope = s.ast
			t.Param = p.parseBindingTarget()
			mode := declCatch
			if _, ok := t.Param.(*ast.Identifier); ok {
				mode = declSimpleCatch
			}
			p.declarePattern(t.Param, mode)
			p.expect(tRParen)
			t.Handler = p.parseBlock(true)
			p.popScope()
		} else {
			t.Handler = p.parseBlock(false)
		}
	}
	if p.eat(tFinally) {
		t.Finalizer = p.parseBlock(false)
	}
	if t.Handler == nil && t.Finalizer == nil {
		p.failTok("try requires a catch or a finally clause")
	}
	t.EndOff = p.prevEnd
	return t
}

func (p *parser) parseSwitch() ast.Stmt {
	sw := &ast.SwitchStmt{}
	sw.StartOff = p.t.start
	p.next()
	p.expect(tLParen)
	sw.Discriminant = p.parseExpressionIn()
	p.expect(tRParen)
	p.expect(tLBrace)
	s := p.pushScope(ast.ScopeSwitch, false)
	sw.Scope = s.ast
	p.f.breakDepth++
	var hasDefault bool
	for p.t.kind != tRBrace {
		var c ast.SwitchCase
		c.StartOff = p.t.start
		switch p.t.kind {
		case tCase:
			p.next()
			c.Test = p.parseExpressionIn()
		case tDefault:
			if hasDefault {
				p.failTok("a switch statement may only have one default clause")
			}
			hasDefault = true
			p.next()
		default:
			p.unexpected()
		}
		p.expect(tColon)
		for p.t.kind != tCase && p.t.kind != tDefault && p.t.kind != tRBrace {
			if p.t.kind == tEOF {
				p.unexpected()
			}
			c.Body = append(c.Body, p.parseStatement(ctxList))
		}
		c.EndOff = p.prevEnd
		sw.Cases = append(sw.Cases, c)
	}
	p.f.breakDepth--
	p.popScope()
	p.next()
	sw.EndOff = p.prevEnd
	return sw
}
