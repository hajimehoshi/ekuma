// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// enterFunction switches the parser state to the function fn, pushes its scope,
// and returns the state to pass to leaveFunction.
func (p *parser) enterFunction(fn *ast.Function) fnState {
	p.enter()
	saved := p.f
	f := fnState{
		strict:     saved.strict,
		labelBase:  len(p.labels),
		inFunction: !fn.FieldInit && !fn.StaticBlock,
		yield:      fn.Generator,
		await:      fn.Async,
	}
	if fn.Arrow {
		f.superProp = saved.superProp
		f.superCall = saved.superCall
		f.newTarget = saved.newTarget
		f.noArguments = saved.noArguments
		f.awaitReserved = p.module
	} else {
		f.superProp = fn.Method || fn.FieldInit || fn.StaticBlock
		f.superCall = fn.DerivedConstructor
		f.newTarget = true
		f.noArguments = fn.FieldInit || fn.StaticBlock
		f.awaitReserved = p.module || fn.StaticBlock
	}
	p.f = f
	fn.Scope = p.pushScope(ast.ScopeFunction, true).ast
	return saved
}

// leaveFunction records the collected facts in fn and restores the state of the enclosing function.
func (p *parser) leaveFunction(fn *ast.Function, saved fnState) {
	p.popScope()
	use := p.f.use
	fn.Strict = p.f.strict
	fn.HasDirectEval = use&useDirectEval != 0
	fn.UsesArguments = use&useArguments != 0
	fn.UsesThis = use&useThis != 0
	fn.UsesSuperProperty = use&useSuperProp != 0
	fn.UsesSuperCall = use&useSuperCall != 0
	fn.UsesNewTarget = use&useNewTarget != 0
	p.f = saved
	if fn.Arrow {
		p.f.use |= use
	}
	p.leave()
}

// parseFormalParams parses formal parameters up to, and not including, the token end.
func (p *parser) parseFormalParams(fn *ast.Function, end tokKind) {
	for p.t.kind != end {
		start := p.t.start
		if p.t.kind == tEllipsis {
			p.next()
			target := p.parseBindingTarget()
			fn.Params = append(fn.Params, &ast.RestElement{
				Span:   span(start, p.prevEnd),
				Target: target,
			})
			if p.t.kind != end {
				p.failTok("rest parameter must be the last parameter")
			}
			break
		}
		fn.Params = append(fn.Params, p.parseBindingElement())
		if p.t.kind != end {
			p.expect(tComma)
		}
	}
	setParamInfo(fn)
}

// setParamInfo sets fn.SimpleParams and fn.Length from fn.Params.
func setParamInfo(fn *ast.Function) {
	fn.SimpleParams = true
	fn.Length = -1
	for i, prm := range fn.Params {
		switch prm.(type) {
		case *ast.Identifier:
		case *ast.AssignPattern, *ast.RestElement:
			fn.SimpleParams = false
			if fn.Length < 0 {
				fn.Length = i
			}
		default:
			fn.SimpleParams = false
		}
	}
	if fn.Length < 0 {
		fn.Length = len(fn.Params)
	}
}

// declareParams checks and declares the parameters of fn. The strictness of the function must be final.
func (p *parser) declareParams(fn *ast.Function) {
	mode := declParam
	if fn.SimpleParams && !p.f.strict && !fn.Arrow && !fn.Method {
		mode = declParamDup
	}
	for _, prm := range fn.Params {
		p.declarePattern(prm, mode)
	}
}

// parseDirectives parses a directive prologue and applies a Use Strict Directive.
// fn is the function whose body starts here, or nil for a script.
// The returned statements include the first statement after the prologue, if any was parsed.
func (p *parser) parseDirectives(fn *ast.Function) []ast.Stmt {
	var body []ast.Stmt
	var octalPos int32
	for p.t.kind == tString {
		start, end, octal := p.t.start, p.t.end, p.t.octal
		stmt := p.parseStatement(ctxList)
		body = append(body, stmt)
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			break
		}
		lit, ok := es.Expr.(*ast.StringLit)
		if !ok || lit.StartOff != start || lit.EndOff != end {
			break
		}
		es.Directive = true
		if octal && octalPos == 0 {
			octalPos = start + 1
		}
		if p.src[start+1:end-1] != "use strict" {
			continue
		}
		if fn != nil && !fn.SimpleParams {
			p.fail(int(start), "\"use strict\" is not allowed in a function with non-simple parameters")
		}
		if octalPos != 0 {
			p.fail(int(octalPos-1), "octal literals and octal escape sequences are not allowed in strict mode")
		}
		p.f.strict = true
	}
	return body
}

// parseFunctionRest parses the parameters and the body of fn. The current token is '('
// on entry and the closing '}' on return. outerStrict is the strictness of the enclosing code.
func (p *parser) parseFunctionRest(fn *ast.Function, outerStrict bool) {
	p.expect(tLParen)
	p.f.inParams = true
	p.parseFormalParams(fn, tRParen)
	p.f.inParams = false
	p.next()
	p.expect(tLBrace)
	body := p.parseDirectives(fn)
	p.declareParams(fn)
	if fn.Name != nil && p.f.strict && !outerStrict {
		p.checkStrictName(fn.Name)
	}
	fn.Body = p.parseStatementList(body, tRBrace)
	fn.EndOff = p.t.end
}

// parseFunctionExpr parses a function expression. The current token is the function keyword.
func (p *parser) parseFunctionExpr(start int32, async bool) ast.Expr {
	p.next()
	fn := &ast.Function{
		Async:     async,
		Generator: p.eat(tMul),
	}
	fn.StartOff = start
	outerStrict := p.f.strict
	saved := p.enterFunction(fn)
	if p.t.kind != tLParen {
		if p.t.kind != tIdent {
			p.unexpected()
		}
		fn.Name = &ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.checkIdent(fn.Name.Name, fn.Name.StartOff, true)
		p.next()
	}
	p.parseFunctionRest(fn, outerStrict)
	p.leaveFunction(fn, saved)
	p.next()
	return fn
}

// parseMethod parses the parameters and the body of the method fn, whose kind flags are set.
// The current token is '('.
func (p *parser) parseMethod(fn *ast.Function, start int32) {
	fn.StartOff = start
	saved := p.enterFunction(fn)
	if p.t.kind != tLParen {
		p.unexpected()
	}
	paramsStart := p.t.start
	p.parseFunctionRest(fn, true)
	switch {
	case fn.Getter:
		if len(fn.Params) != 0 {
			p.fail(int(paramsStart), "getter must not have parameters")
		}
	case fn.Setter:
		if len(fn.Params) != 1 {
			p.fail(int(paramsStart), "setter must have exactly one parameter")
		}
		if _, ok := fn.Params[0].(*ast.RestElement); ok {
			p.fail(int(paramsStart), "setter parameter must not be a rest parameter")
		}
	}
	p.leaveFunction(fn, saved)
	p.next()
}

// parseFunctionDecl parses a function declaration. The current token is the function keyword.
// ctx is the statement context, and anonymous permits a missing name, as in a default export.
func (p *parser) parseFunctionDecl(start int32, async bool, ctx stmtCtx, anonymous bool) *ast.FuncDecl {
	p.next()
	fn := &ast.Function{
		Async:     async,
		Generator: p.eat(tMul),
	}
	fn.StartOff = start
	plain := !fn.Async && !fn.Generator
	if ctx != ctxList && !plain {
		p.fail(int(start), "generator and async functions cannot be declared here")
	}
	decl := &ast.FuncDecl{
		Func: fn,
	}

	if p.t.kind == tIdent {
		fn.Name = &ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.checkIdent(fn.Name.Name, fn.Name.StartOff, true)
		p.next()
		p.declareFunction(decl, plain)
	} else if !anonymous || p.t.kind != tLParen {
		p.unexpected()
	} else {
		p.declareLexical("*default*", start, ast.DeclFunction, fn, false)
	}

	outerStrict := p.f.strict
	saved := p.enterFunction(fn)
	p.parseFunctionRest(fn, outerStrict)
	p.leaveFunction(fn, saved)
	p.next()
	decl.Span = fn.Span
	return decl
}

// declareFunction declares the name of a function declaration in the current scope.
func (p *parser) declareFunction(decl *ast.FuncDecl, plain bool) {
	fn := decl.Func
	s := p.scope
	if s.varScope && s.ast.Kind != ast.ScopeModule {
		p.declareVar(fn.Name.Name, fn.Name.StartOff, fn)
		return
	}
	sloppy := plain && !p.f.strict
	p.declareLexical(fn.Name.Name, fn.Name.StartOff, ast.DeclFunction, fn, sloppy)
	if sloppy {
		s.hoist = append(s.hoist, hoistCand{
			decl: decl,
			own:  true,
		})
	}
}

// parseClass parses a class. The current token is the class keyword.
// decl selects a declaration, whose name is declared in the current scope,
// and anonymous permits a declaration without a name, as in a default export.
func (p *parser) parseClass(decl, anonymous bool) *ast.Class {
	p.enter()
	cls := &ast.Class{}
	cls.StartOff = p.t.start
	savedStrict := p.f.strict
	p.f.strict = true
	p.next()

	if p.t.kind == tIdent {
		cls.Name = &ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.checkIdent(cls.Name.Name, cls.Name.StartOff, true)
		p.next()
		if decl {
			p.declareLexical(cls.Name.Name, cls.Name.StartOff, ast.DeclClass, nil, false)
		}
	} else if decl {
		if !anonymous {
			p.unexpected()
		}
		p.declareLexical("*default*", cls.StartOff, ast.DeclClass, nil, false)
	}

	s := p.pushScope(ast.ScopeClass, false)
	cls.Scope = s.ast
	if cls.Name != nil {
		s.ast.Decls = append(s.ast.Decls, ast.Decl{
			Name: cls.Name.Name,
			Kind: ast.DeclClass,
			Pos:  cls.Name.StartOff,
		})
	}
	if p.eat(tExtends) {
		cls.Super = p.parseLeftHandSide()
	}
	p.expect(tLBrace)

	cs := &classState{
		outer: p.class,
	}
	p.class = cs
	for p.t.kind != tRBrace {
		if p.eat(tSemi) {
			continue
		}
		cls.Members = append(cls.Members, ast.ClassMember{})
		p.parseClassMember(cls, &cls.Members[len(cls.Members)-1])
	}
	p.class = cs.outer
	for _, r := range cs.refs {
		if cs.declared(r.name) >= 0 {
			continue
		}
		if cs.outer != nil {
			cs.outer.refs = append(cs.outer.refs, r)
			continue
		}
		p.privateRef(r.name, r.pos)
	}

	p.popScope()
	p.f.strict = savedStrict
	p.next()
	cls.EndOff = p.prevEnd
	p.leave()
	return cls
}

func isClassKeyEnd(k tokKind) bool {
	switch k {
	case tLParen, tAssign, tSemi, tRBrace:
		return true
	}
	return false
}

func (p *parser) parseClassMember(cls *ast.Class, m *ast.ClassMember) {
	memberStart := p.t.start
	var key ast.Expr
	if p.isWord("static") {
		id := &ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.next()
		if isClassKeyEnd(p.t.kind) {
			key = id
		} else {
			m.Static = true
		}
	}

	start := p.t.start
	if key != nil {
		start = memberStart
	}
	if m.Static && p.t.kind == tLBrace {
		fn := &ast.Function{
			StaticBlock: true,
		}
		fn.StartOff = start
		saved := p.enterFunction(fn)
		p.next()
		fn.Body = p.parseStatementList(nil, tRBrace)
		fn.EndOff = p.t.end
		p.leaveFunction(fn, saved)
		p.next()
		m.Kind = ast.MemberStaticBlock
		m.Value = fn
		m.Span = span(memberStart, p.prevEnd)
		return
	}

	var async, generator bool
	if key == nil && p.t.kind == tIdent && !p.t.esc && (p.t.str == "get" || p.t.str == "set" || p.t.str == "async") {
		id := &ast.Identifier{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.next()
		switch {
		case isClassKeyEnd(p.t.kind) || id.Name == "async" && p.t.nl:
			key = id
		case id.Name == "get":
			m.Kind = ast.MemberGetter
		case id.Name == "set":
			m.Kind = ast.MemberSetter
		default:
			async = true
			generator = p.eat(tMul)
		}
	} else if key == nil && p.eat(tMul) {
		generator = true
	}
	if key == nil {
		key, m.Computed = p.parsePropertyKey(true)
	}
	m.Key = key

	// name is the property name when it is known statically.
	var name string
	if !m.Computed {
		switch k := key.(type) {
		case *ast.Identifier:
			name = k.Name
		case *ast.StringLit:
			name = k.Value
		}
	}
	private, _ := key.(*ast.PrivateName)

	if m.Kind == ast.MemberMethod && !async && !generator && p.t.kind != tLParen {
		m.Kind = ast.MemberField
		if name == "constructor" || m.Static && name == "prototype" {
			p.fail(int(key.Start()), "invalid class field name '"+name+"'")
		}
		if private != nil {
			flags := uint8(privOther)
			if m.Static {
				flags |= privStatic
			}
			p.privateDecl(private.Name, private.StartOff, flags)
		}
		if p.eat(tAssign) {
			fn := &ast.Function{
				FieldInit: true,
			}
			fn.StartOff = p.t.start
			saved := p.enterFunction(fn)
			fn.ExprBody = p.parseAssignIn()
			fn.EndOff = p.prevEnd
			p.leaveFunction(fn, saved)
			m.Value = fn
		}
		m.Span = span(memberStart, p.prevEnd)
		p.consumeSemicolon()
		return
	}

	fn := &ast.Function{
		Method:    true,
		Async:     async,
		Generator: generator,
		Getter:    m.Kind == ast.MemberGetter,
		Setter:    m.Kind == ast.MemberSetter,
	}
	switch {
	case private != nil:
		flags := uint8(privOther)
		switch m.Kind {
		case ast.MemberGetter:
			flags = privGetter
		case ast.MemberSetter:
			flags = privSetter
		}
		if m.Static {
			flags |= privStatic
		}
		p.privateDecl(private.Name, private.StartOff, flags)
	case !m.Static && name == "constructor":
		if m.Kind != ast.MemberMethod || async || generator {
			p.fail(int(key.Start()), "class constructor must be a plain method")
		}
		if cls.Constructor != nil {
			p.fail(int(key.Start()), "a class may only have one constructor")
		}
		m.Kind = ast.MemberConstructor
		fn.ClassConstructor = true
		fn.DerivedConstructor = cls.Super != nil
		cls.Constructor = fn
	case m.Static && name == "prototype":
		p.fail(int(key.Start()), "classes may not have a static property named 'prototype'")
	}
	p.parseMethod(fn, start)
	m.Value = fn
	m.Span = span(memberStart, p.prevEnd)
}
