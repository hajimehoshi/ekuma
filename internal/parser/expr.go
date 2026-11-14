// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/regexp"
)

func span(start, end int32) ast.Span {
	return ast.Span{
		StartOff: start,
		EndOff:   end,
	}
}

// parseExpression parses a comma expression.
func (p *parser) parseExpression() ast.Expr {
	e := p.parseAssign()
	if p.t.kind != tComma {
		return e
	}
	exprs := []ast.Expr{e}
	for p.eat(tComma) {
		exprs = append(exprs, p.parseAssign())
	}
	return &ast.SequenceExpr{
		Span:  span(e.Start(), p.prevEnd),
		Exprs: exprs,
	}
}

// parseExpressionIn parses a comma expression in which the in operator is allowed.
func (p *parser) parseExpressionIn() ast.Expr {
	noIn := p.f.noIn
	p.f.noIn = false
	e := p.parseExpression()
	p.f.noIn = noIn
	return e
}

// parseAssignIn parses an assignment expression in which the in operator is allowed.
func (p *parser) parseAssignIn() ast.Expr {
	noIn := p.f.noIn
	p.f.noIn = false
	e := p.parseAssign()
	p.f.noIn = noIn
	return e
}

// parseAssign parses an assignment expression.
func (p *parser) parseAssign() ast.Expr {
	e, cover := p.parseAssignCover()
	if cover != 0 {
		p.fail(int(cover-1), "invalid shorthand property initializer")
	}
	return e
}

// parseAssignCover parses an assignment expression that may turn out to be a pattern.
// A non-zero cover is the offset plus one of an error that applies unless the result is converted to a pattern.
func (p *parser) parseAssignCover() (result ast.Expr, cover int32) {
	p.enter()
	savedCover, savedCoverNode := p.cover, p.coverNode
	p.cover, p.coverNode = 0, nil

	if p.f.yield && p.isWord("yield") {
		result = p.parseYield()
	} else {
		p.arrowStart = p.t.start
		left := p.parseConditional()
		result = left
		if isAssignOp(p.t.kind) && !p.isArrowEnd(left) {
			op := assignOps[p.t.kind-tAssign]
			var target ast.Pattern
			if op == ast.OpNone {
				target = p.toAssignTarget(left)
				if p.coverNode == left {
					p.cover = 0
				}
			} else if op != ast.OpAnd && op != ast.OpOr && op != ast.OpCoalesce && p.isCallTarget(left) {
				target = left.(*ast.CallExpr)
			} else {
				target = p.toSimpleTarget(left)
			}
			p.next()
			value := p.parseAssign()
			result = &ast.AssignExpr{
				Span:   span(left.Start(), p.prevEnd),
				Op:     op,
				Target: target,
				Value:  value,
			}
		}
	}

	cover = p.cover
	if cover != 0 && p.coverNode != result {
		p.fail(int(cover-1), "invalid shorthand property initializer")
	}
	p.cover, p.coverNode = savedCover, savedCoverNode
	p.leave()
	return result, cover
}

// isArrowEnd reports whether e is an arrow function whose last token is the previous token.
// No operator can be applied to such an expression.
func (p *parser) isArrowEnd(e ast.Expr) bool {
	f, ok := e.(*ast.Function)
	return ok && f.Arrow && f.EndOff == p.prevEnd
}

func (p *parser) parseYield() ast.Expr {
	start := p.t.start
	if p.f.inParams {
		p.failTok("yield expression is not allowed in formal parameters")
	}
	if p.f.yieldPos == 0 {
		p.f.yieldPos = start + 1
	}
	p.next()
	y := &ast.YieldExpr{}
	if !p.t.nl {
		switch p.t.kind {
		case tMul:
			p.next()
			y.Delegate = true
			y.Arg = p.parseAssign()
		case tRParen, tRBrack, tRBrace, tComma, tSemi, tColon, tEOF, tIn:
		default:
			y.Arg = p.parseAssign()
		}
	}
	y.Span = span(start, p.prevEnd)
	return y
}

func (p *parser) parseConditional() ast.Expr {
	test := p.parseBinary(1)
	if p.t.kind != tQuestion || p.isArrowEnd(test) {
		return test
	}
	p.next()
	cons := p.parseAssignIn()
	p.expect(tColon)
	alt := p.parseAssign()
	return &ast.CondExpr{
		Span:       span(test.Start(), p.prevEnd),
		Test:       test,
		Consequent: cons,
		Alternate:  alt,
	}
}

func isLogical(e ast.Expr, coalesce bool) bool {
	l, ok := e.(*ast.LogicalExpr)
	return ok && (l.Op == ast.OpCoalesce) == coalesce
}

func (p *parser) parseBinary(minPrec int) ast.Expr {
	var left ast.Expr
	if p.t.kind == tPrivateName {
		if minPrec > precRel || p.f.noIn {
			p.unexpected()
		}
		name := &ast.PrivateName{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.privateRef(name.Name, name.StartOff)
		p.next()
		if p.t.kind != tIn {
			p.unexpected()
		}
		p.next()
		right := p.parseBinary(precRel + 1)
		left = &ast.PrivateInExpr{
			Span:  span(name.StartOff, p.prevEnd),
			Name:  name,
			Right: right,
		}
	} else {
		left = p.parseExponent()
		if p.isArrowEnd(left) {
			return left
		}
	}

	for {
		k := p.t.kind
		info := binaryOps[k]
		if info.prec == 0 || int(info.prec) < minPrec || k == tIn && p.f.noIn {
			break
		}
		leftParen := left == p.lastParen
		p.next()
		right := p.parseBinary(int(info.prec) + 1)
		switch info.op {
		case ast.OpCoalesce:
			if isLogical(left, false) && !leftParen || isLogical(right, false) && right != p.lastParen {
				p.fail(int(left.Start()), "?? cannot be mixed with || or && without parentheses")
			}
		case ast.OpOr, ast.OpAnd:
			if isLogical(left, true) && !leftParen {
				p.fail(int(left.Start()), "?? cannot be mixed with || or && without parentheses")
			}
		default:
			left = &ast.BinaryExpr{
				Span:  span(left.Start(), p.prevEnd),
				Op:    info.op,
				Left:  left,
				Right: right,
			}
			continue
		}
		left = &ast.LogicalExpr{
			Span:  span(left.Start(), p.prevEnd),
			Op:    info.op,
			Left:  left,
			Right: right,
		}
	}
	return left
}

func (p *parser) parseExponent() ast.Expr {
	e := p.parseUnary()
	if p.t.kind != tExp || p.isArrowEnd(e) {
		return e
	}
	if e != p.lastParen {
		switch e.(type) {
		case *ast.UnaryExpr, *ast.AwaitExpr:
			p.failTok("unary operator used immediately before exponentiation expression")
		}
	}
	p.next()
	p.enter()
	right := p.parseExponent()
	p.leave()
	return &ast.BinaryExpr{
		Span:  span(e.Start(), p.prevEnd),
		Op:    ast.OpExp,
		Left:  e,
		Right: right,
	}
}

func (p *parser) parseUnary() ast.Expr {
	start := p.t.start
	var op ast.Op
	switch p.t.kind {
	case tNot:
		op = ast.OpNot
	case tTilde:
		op = ast.OpBitNot
	case tAdd:
		op = ast.OpPos
	case tSub:
		op = ast.OpNeg
	case tTypeof:
		op = ast.OpTypeof
	case tVoid:
		op = ast.OpVoid
	case tDelete:
		op = ast.OpDelete
	case tInc, tDec:
		op = ast.OpInc
		if p.t.kind == tDec {
			op = ast.OpDec
		}
		p.next()
		p.enter()
		arg := p.parseUnary()
		p.leave()
		p.checkUpdateTarget(arg)
		return &ast.UpdateExpr{
			Span:   span(start, p.prevEnd),
			Op:     op,
			Prefix: true,
			Arg:    arg,
		}
	case tIdent:
		if p.f.await && p.isWord("await") {
			if p.f.inParams {
				p.failTok("await expression is not allowed in formal parameters")
			}
			if p.f.awaitPos == 0 {
				p.f.awaitPos = start + 1
			}
			p.next()
			p.enter()
			arg := p.parseUnary()
			p.leave()
			return &ast.AwaitExpr{
				Span: span(start, p.prevEnd),
				Arg:  arg,
			}
		}
		return p.parsePostfix()
	default:
		return p.parsePostfix()
	}

	p.next()
	p.enter()
	arg := p.parseUnary()
	p.leave()
	if op == ast.OpDelete {
		target := arg
		if c, ok := target.(*ast.OptionalChain); ok {
			target = c.Expr
		}
		switch t := target.(type) {
		case *ast.Identifier:
			if p.f.strict {
				p.fail(int(start), "delete of an unqualified identifier in strict mode")
			}
		case *ast.MemberExpr:
			if _, ok := t.Property.(*ast.PrivateName); ok && !t.Computed {
				p.fail(int(start), "private fields cannot be deleted")
			}
		}
	}
	return &ast.UnaryExpr{
		Span: span(start, p.prevEnd),
		Op:   op,
		Arg:  arg,
	}
}

func (p *parser) parsePostfix() ast.Expr {
	e := p.parseLeftHandSide()
	if (p.t.kind != tInc && p.t.kind != tDec) || p.t.nl || p.isArrowEnd(e) {
		return e
	}
	p.checkUpdateTarget(e)
	op := ast.OpInc
	if p.t.kind == tDec {
		op = ast.OpDec
	}
	p.next()
	return &ast.UpdateExpr{
		Span: span(e.Start(), p.prevEnd),
		Op:   op,
		Arg:  e,
	}
}

func (p *parser) checkUpdateTarget(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Identifier:
		if p.f.strict && (e.Name == "eval" || e.Name == "arguments") {
			p.fail(int(e.StartOff), "'"+e.Name+"' cannot be bound or assigned in strict mode")
		}
	case *ast.MemberExpr:
	default:
		if p.isCallTarget(e) {
			return
		}
		p.fail(int(e.Start()), "invalid operand of an increment or decrement")
	}
}

// parseMemberName parses the property name after a dot.
func (p *parser) parseMemberName() ast.Expr {
	if p.t.kind == tPrivateName {
		name := &ast.PrivateName{
			Span: span(p.t.start, p.t.end),
			Name: p.t.str,
		}
		p.privateRef(name.Name, name.StartOff)
		p.next()
		return name
	}
	if !p.t.kind.isWord() {
		p.unexpected()
	}
	id := &ast.Identifier{
		Span: span(p.t.start, p.t.end),
		Name: p.t.str,
	}
	p.next()
	return id
}

func (p *parser) parseLeftHandSide() ast.Expr {
	start := p.t.start
	var e ast.Expr
	switch p.t.kind {
	case tNew:
		e = p.parseNew()
	case tSuper:
		e = p.parseSuper(true)
	case tImport:
		e = p.parseImportExpr(true)
	default:
		e = p.parsePrimary()
		if p.isArrowEnd(e) {
			return e
		}
	}

	var chain bool
	for {
		switch p.t.kind {
		case tDot:
			p.next()
			prop := p.parseMemberName()
			e = &ast.MemberExpr{
				Span:     span(start, p.prevEnd),
				Object:   e,
				Property: prop,
			}
		case tQuestionDot:
			chain = true
			p.next()
			switch p.t.kind {
			case tLParen:
				args := p.parseArguments()
				e = &ast.CallExpr{
					Span:     span(start, p.prevEnd),
					Callee:   e,
					Args:     args,
					Optional: true,
				}
			case tLBrack:
				p.next()
				prop := p.parseExpressionIn()
				p.expect(tRBrack)
				e = &ast.MemberExpr{
					Span:     span(start, p.prevEnd),
					Object:   e,
					Property: prop,
					Computed: true,
					Optional: true,
				}
			case tTemplate:
				p.failTok("tagged template cannot be used in an optional chain")
			default:
				prop := p.parseMemberName()
				e = &ast.MemberExpr{
					Span:     span(start, p.prevEnd),
					Object:   e,
					Property: prop,
					Optional: true,
				}
			}
		case tLBrack:
			p.next()
			prop := p.parseExpressionIn()
			p.expect(tRBrack)
			e = &ast.MemberExpr{
				Span:     span(start, p.prevEnd),
				Object:   e,
				Property: prop,
				Computed: true,
			}
		case tLParen:
			if id, ok := e.(*ast.Identifier); ok && id == p.plainAsync && start == p.arrowStart && !id.Parenthesized && !p.t.nl {
				var arrow bool
				e, arrow = p.parseAsyncCall(id)
				if arrow {
					return e
				}
				continue
			}
			args := p.parseArguments()
			if id, ok := e.(*ast.Identifier); ok && id.Name == "eval" {
				p.f.use |= useDirectEval
				p.scope.ast.ContainsDirectEval = true
			}
			e = &ast.CallExpr{
				Span:   span(start, p.prevEnd),
				Callee: e,
				Args:   args,
			}
		case tTemplate:
			if chain {
				p.failTok("tagged template cannot be used in an optional chain")
			}
			quasi := p.parseTemplate(true)
			e = &ast.TaggedTemplate{
				Span:  span(start, p.prevEnd),
				Tag:   e,
				Quasi: quasi,
			}
		default:
			if chain {
				e = &ast.OptionalChain{
					Span: span(start, p.prevEnd),
					Expr: e,
				}
			}
			return e
		}
	}
}

func (p *parser) parseArguments() []ast.Expr {
	p.next()
	noIn := p.f.noIn
	p.f.noIn = false
	var args []ast.Expr
	for p.t.kind != tRParen {
		if p.t.kind == tEllipsis {
			start := p.t.start
			p.next()
			arg := p.parseAssign()
			args = append(args, &ast.SpreadElement{
				Span: span(start, p.prevEnd),
				Arg:  arg,
			})
		} else {
			args = append(args, p.parseAssign())
		}
		if p.t.kind != tRParen {
			p.expect(tComma)
		}
	}
	p.next()
	p.f.noIn = noIn
	return args
}

// arrowHead is the state saved around a construct that can turn out to be an arrow function head.
type arrowHead struct {
	base          *scope
	mark          int
	use           usage
	yieldPos      int32
	awaitPos      int32
	awaitIdentPos int32
	noIn          bool
}

func (p *parser) beginArrowHead() arrowHead {
	h := arrowHead{
		base:          p.arrowBase,
		mark:          len(p.pending),
		use:           p.f.use,
		yieldPos:      p.f.yieldPos,
		awaitPos:      p.f.awaitPos,
		awaitIdentPos: p.f.awaitIdentPos,
		noIn:          p.f.noIn,
	}
	p.arrowBase = p.scope
	p.f.use = 0
	p.f.yieldPos = 0
	p.f.awaitPos = 0
	p.f.awaitIdentPos = 0
	p.f.noIn = false
	return h
}

// endArrowHead restores the state saved in h and returns the usage collected inside the construct.
// It reports the errors that apply when the construct is an arrow function head.
func (p *parser) endArrowHead(h *arrowHead, arrow, async bool) usage {
	inner := p.f.use
	if arrow {
		if p.f.yieldPos != 0 {
			p.fail(int(p.f.yieldPos-1), "yield expression is not allowed in formal parameters")
		}
		if p.f.awaitPos != 0 {
			p.fail(int(p.f.awaitPos-1), "await expression is not allowed in formal parameters")
		}
		if async && p.f.awaitIdentPos != 0 {
			p.fail(int(p.f.awaitIdentPos-1), "await is not a valid identifier here")
		}
	}
	p.arrowBase = h.base
	if !arrow && h.base != p.scope {
		p.pending = p.pending[:h.mark]
	}
	p.f.use = h.use | inner
	if h.yieldPos != 0 {
		p.f.yieldPos = h.yieldPos
	}
	if h.awaitPos != 0 {
		p.f.awaitPos = h.awaitPos
	}
	if h.awaitIdentPos != 0 {
		p.f.awaitIdentPos = h.awaitIdentPos
	}
	p.f.noIn = h.noIn
	return inner
}

// patErrOf returns the offset plus one of the error that applies when e, which was just parsed
// as an element of a possible pattern, becomes part of a pattern. Zero means none.
func (p *parser) patErrOf(e ast.Expr) int32 {
	if e == p.lastParen {
		switch e.(type) {
		case *ast.ObjectLit, *ast.ArrayLit, *ast.AssignExpr:
			return e.Start() + 1
		}
	}
	if e == p.patNode {
		return p.patErr
	}
	return 0
}

// parseAsyncCall parses the arguments after the identifier async, which are either the arguments of
// a call or the parameters of an async arrow function. The current token is '('.
func (p *parser) parseAsyncCall(id *ast.Identifier) (e ast.Expr, arrow bool) {
	h := p.beginArrowHead()
	p.next()
	var args []ast.Expr
	var cover, patErr int32
	for p.t.kind != tRParen {
		if p.t.kind == tEllipsis {
			start := p.t.start
			p.next()
			arg, c := p.parseAssignCover()
			if cover == 0 {
				cover = c
			}
			if patErr == 0 {
				patErr = p.patErrOf(arg)
			}
			args = append(args, &ast.SpreadElement{
				Span: span(start, p.prevEnd),
				Arg:  arg,
			})
			if p.t.kind == tComma && patErr == 0 {
				patErr = p.t.start + 1
			}
		} else {
			arg, c := p.parseAssignCover()
			if cover == 0 {
				cover = c
			}
			if patErr == 0 {
				patErr = p.patErrOf(arg)
			}
			args = append(args, arg)
		}
		if p.t.kind != tRParen {
			p.expect(tComma)
		}
	}
	p.next()

	if p.t.kind != tArrow || p.t.nl {
		p.endArrowHead(&h, false, false)
		if cover != 0 {
			p.fail(int(cover-1), "invalid shorthand property initializer")
		}
		return &ast.CallExpr{
			Span:   span(id.StartOff, p.prevEnd),
			Callee: id,
			Args:   args,
		}, false
	}

	inner := p.endArrowHead(&h, true, true)
	if patErr != 0 {
		p.fail(int(patErr-1), "invalid destructuring target")
	}
	params := make([]ast.Pattern, len(args))
	for i, a := range args {
		if s, ok := a.(*ast.SpreadElement); ok {
			if i != len(args)-1 {
				p.fail(int(s.StartOff), "rest parameter must be the last parameter")
			}
			params[i] = &ast.RestElement{
				Span:   s.Span,
				Target: p.toPattern(s.Arg, true),
			}
			continue
		}
		params[i] = p.toElement(a, true)
	}
	return p.parseArrow(id.StartOff, params, true, inner, h.mark), true
}

func (p *parser) parseParen() ast.Expr {
	start := p.t.start
	canArrow := p.arrowStart == start
	h := p.beginArrowHead()
	p.next()

	var first ast.Expr
	var exprs []ast.Expr
	var rest *ast.RestElement
	var cover, patErr, trailingComma int32
	for p.t.kind != tRParen {
		if p.t.kind == tEllipsis {
			restStart := p.t.start
			p.next()
			target := p.parseBindingTarget()
			rest = &ast.RestElement{
				Span:   span(restStart, p.prevEnd),
				Target: target,
			}
			if p.t.kind != tRParen {
				p.unexpected()
			}
			break
		}
		e, c := p.parseAssignCover()
		if cover == 0 {
			cover = c
		}
		if patErr == 0 {
			patErr = p.patErrOf(e)
		}
		if first == nil {
			first = e
		} else {
			if exprs == nil {
				exprs = append(exprs, first)
			}
			exprs = append(exprs, e)
		}
		if p.t.kind != tComma {
			break
		}
		commaPos := p.t.start
		p.next()
		if p.t.kind == tRParen {
			trailingComma = commaPos + 1
		}
	}
	end := p.t.end
	p.expect(tRParen)

	if canArrow && p.t.kind == tArrow && !p.t.nl {
		inner := p.endArrowHead(&h, true, false)
		if patErr != 0 {
			p.fail(int(patErr-1), "invalid destructuring target")
		}
		var params []ast.Pattern
		if exprs != nil {
			params = make([]ast.Pattern, 0, len(exprs)+1)
			for _, e := range exprs {
				params = append(params, p.toElement(e, true))
			}
		} else if first != nil {
			params = append(params, p.toElement(first, true))
		}
		if rest != nil {
			params = append(params, rest)
		}
		return p.parseArrow(start, params, false, inner, h.mark)
	}

	p.endArrowHead(&h, false, false)
	if rest != nil {
		p.fail(int(rest.StartOff), "unexpected token '...'")
	}
	if first == nil {
		p.fail(int(end-1), "unexpected token ')'")
	}
	if trailingComma != 0 {
		p.fail(int(trailingComma-1), "unexpected token ','")
	}
	if cover != 0 {
		p.fail(int(cover-1), "invalid shorthand property initializer")
	}
	result := first
	if exprs != nil {
		result = &ast.SequenceExpr{
			Span:  span(first.Start(), exprs[len(exprs)-1].End()),
			Exprs: exprs,
		}
	}
	if id, ok := result.(*ast.Identifier); ok {
		id.Parenthesized = true
	}
	p.lastParen = result
	return result
}

// parseArrow parses an arrow function from its '=>' token on.
// use is the usage collected in the parameters, and mark is the index in p.pending of the first scope
// created in the parameters, or a negative value when the parameters cannot contain scopes.
func (p *parser) parseArrow(start int32, params []ast.Pattern, async bool, use usage, mark int) ast.Expr {
	fn := &ast.Function{
		Params: params,
		Arrow:  true,
		Async:  async,
	}
	fn.StartOff = start
	setParamInfo(fn)

	outer := p.scope
	noIn := p.f.noIn
	saved := p.enterFunction(fn)
	if mark >= 0 {
		n := len(p.pending)
		registered := outer == p.arrowBase
		if registered {
			n--
		}
		for _, s := range p.pending[mark:n] {
			s.Parent = fn.Scope
			if s.ContainsDirectEval {
				fn.Scope.ContainsDirectEval = true
			}
		}
		p.pending = p.pending[:mark]
		if registered {
			p.pending = append(p.pending, fn.Scope)
		}
	}
	p.f.use = use
	if use&useDirectEval != 0 {
		fn.Scope.ContainsDirectEval = true
	}

	p.next()
	if p.t.kind == tLBrace {
		p.next()
		body := p.parseDirectives(fn)
		p.declareParams(fn)
		fn.Body = p.parseStatementList(body, tRBrace)
		fn.EndOff = p.t.end
		p.leaveFunction(fn, saved)
		p.next()
		return fn
	}
	p.declareParams(fn)
	p.f.noIn = noIn
	fn.ExprBody = p.parseAssign()
	fn.EndOff = p.prevEnd
	p.leaveFunction(fn, saved)
	return fn
}

func (p *parser) parseNew() ast.Expr {
	start := p.t.start
	p.enter()
	p.next()
	if p.eat(tDot) {
		if !p.isWord("target") {
			p.unexpected()
		}
		if !p.f.newTarget {
			p.fail(int(start), "new.target is not allowed here")
		}
		p.f.use |= useNewTarget
		p.next()
		p.leave()
		return &ast.NewTargetExpr{
			Span: span(start, p.prevEnd),
		}
	}

	calleeStart := p.t.start
	var callee ast.Expr
	switch p.t.kind {
	case tNew:
		callee = p.parseNew()
	case tSuper:
		callee = p.parseSuper(false)
	case tImport:
		callee = p.parseImportExpr(false)
	default:
		callee = p.parsePrimary()
	}
loop:
	for {
		switch p.t.kind {
		case tDot:
			p.next()
			prop := p.parseMemberName()
			callee = &ast.MemberExpr{
				Span:     span(calleeStart, p.prevEnd),
				Object:   callee,
				Property: prop,
			}
		case tLBrack:
			p.next()
			prop := p.parseExpressionIn()
			p.expect(tRBrack)
			callee = &ast.MemberExpr{
				Span:     span(calleeStart, p.prevEnd),
				Object:   callee,
				Property: prop,
				Computed: true,
			}
		case tTemplate:
			quasi := p.parseTemplate(true)
			callee = &ast.TaggedTemplate{
				Span:  span(calleeStart, p.prevEnd),
				Tag:   callee,
				Quasi: quasi,
			}
		case tQuestionDot:
			p.failTok("optional chain cannot be used as the operand of new")
		default:
			break loop
		}
	}
	var args []ast.Expr
	if p.t.kind == tLParen {
		args = p.parseArguments()
	}
	p.leave()
	return &ast.NewExpr{
		Span:   span(start, p.prevEnd),
		Callee: callee,
		Args:   args,
	}
}

// parseSuper parses super together with the property access or call that must follow it.
func (p *parser) parseSuper(allowCall bool) ast.Expr {
	start := p.t.start
	sup := &ast.SuperExpr{
		Span: span(start, p.t.end),
	}
	p.next()
	switch p.t.kind {
	case tLParen:
		if !allowCall || !p.f.superCall {
			p.fail(int(start), "super call is not allowed here")
		}
		p.f.use |= useSuperCall
		args := p.parseArguments()
		return &ast.CallExpr{
			Span:   span(start, p.prevEnd),
			Callee: sup,
			Args:   args,
		}
	case tDot:
		if !p.f.superProp {
			p.fail(int(start), "super property access is not allowed here")
		}
		// A super property reference resolves the this binding.
		p.f.use |= useSuperProp | useThis
		p.next()
		if !p.t.kind.isWord() {
			p.unexpected()
		}
		prop := p.parseMemberName()
		return &ast.MemberExpr{
			Span:     span(start, p.prevEnd),
			Object:   sup,
			Property: prop,
		}
	case tLBrack:
		if !p.f.superProp {
			p.fail(int(start), "super property access is not allowed here")
		}
		// A super property reference resolves the this binding.
		p.f.use |= useSuperProp | useThis
		p.next()
		prop := p.parseExpressionIn()
		p.expect(tRBrack)
		return &ast.MemberExpr{
			Span:     span(start, p.prevEnd),
			Object:   sup,
			Property: prop,
			Computed: true,
		}
	}
	p.fail(int(start), "super must be followed by a property access or a call")
	return nil
}

// parseImportExpr parses import.meta or, when allowCall is set, a dynamic import.
func (p *parser) parseImportExpr(allowCall bool) ast.Expr {
	start := p.t.start
	p.next()
	switch p.t.kind {
	case tDot:
		p.next()
		if !p.isWord("meta") {
			p.unexpected()
		}
		if !p.module {
			p.fail(int(start), "import.meta is only allowed in a module")
		}
		p.next()
		return &ast.ImportMetaExpr{
			Span: span(start, p.prevEnd),
		}
	case tLParen:
		if !allowCall {
			p.fail(int(start), "import call cannot be the operand of new")
		}
		p.next()
		c := &ast.ImportCall{}
		c.Source = p.parseAssignIn()
		if p.eat(tComma) && p.t.kind != tRParen {
			c.Options = p.parseAssignIn()
			p.eat(tComma)
		}
		p.expect(tRParen)
		c.Span = span(start, p.prevEnd)
		return c
	}
	p.unexpected()
	return nil
}

func (p *parser) checkOctal() {
	if p.t.octal && p.f.strict {
		p.failTok("octal literals and octal escape sequences are not allowed in strict mode")
	}
}

func (p *parser) parseStringLit() *ast.StringLit {
	p.checkOctal()
	lit := &ast.StringLit{
		Span:  span(p.t.start, p.t.end),
		Value: p.t.str,
	}
	p.next()
	return lit
}

func (p *parser) parsePrimary() ast.Expr {
	start := p.t.start
	switch p.t.kind {
	case tIdent:
		return p.parseIdentPrimary()
	case tNumber:
		p.checkOctal()
		lit := &ast.NumberLit{
			Span:  span(start, p.t.end),
			Value: p.t.num,
		}
		p.next()
		return lit
	case tBigInt:
		lit := &ast.BigIntLit{
			Span:  span(start, p.t.end),
			Value: p.t.str,
		}
		p.next()
		return lit
	case tString:
		return p.parseStringLit()
	case tTemplate:
		return p.parseTemplate(false)
	case tDiv, tDivAssign:
		p.rescanRegExp()
		if err := regexp.Validate(p.t.str, p.t.raw); err != nil {
			p.fail(int(start), err.Error())
		}
		lit := &ast.RegExpLit{
			Span:    span(start, p.t.end),
			Pattern: p.t.str,
			Flags:   p.t.raw,
		}
		p.next()
		return lit
	case tThis:
		p.f.use |= useThis
		p.next()
		return &ast.ThisExpr{
			Span: span(start, p.prevEnd),
		}
	case tNull:
		p.next()
		return &ast.NullLit{
			Span: span(start, p.prevEnd),
		}
	case tTrue, tFalse:
		lit := &ast.BoolLit{
			Span:  span(start, p.t.end),
			Value: p.t.kind == tTrue,
		}
		p.next()
		return lit
	case tLParen:
		return p.parseParen()
	case tLBrack:
		return p.parseArray()
	case tLBrace:
		return p.parseObject()
	case tFunction:
		return p.parseFunctionExpr(start, false)
	case tClass:
		return p.parseClass(false, false)
	}
	p.unexpected()
	return nil
}

func (p *parser) parseIdentPrimary() ast.Expr {
	start := p.t.start
	canArrow := p.arrowStart == start
	id := &ast.Identifier{
		Span: span(start, p.t.end),
		Name: p.t.str,
	}
	async := p.isWord("async")
	p.next()
	if async {
		p.plainAsync = id
		if p.t.kind == tFunction && !p.t.nl {
			return p.parseFunctionExpr(start, true)
		}
		if canArrow && p.t.kind == tIdent && !p.t.nl {
			param := &ast.Identifier{
				Span: span(p.t.start, p.t.end),
				Name: p.t.str,
			}
			p.next()
			if p.t.kind != tArrow || p.t.nl {
				p.unexpected()
			}
			return p.parseArrow(start, []ast.Pattern{param}, true, 0, -1)
		}
	}
	if canArrow && p.t.kind == tArrow && !p.t.nl {
		p.noteAwaitIdent(id.Name, start)
		return p.parseArrow(start, []ast.Pattern{id}, false, 0, -1)
	}
	p.checkIdent(id.Name, start, false)
	if id.Name == "arguments" {
		if p.f.noArguments {
			p.fail(int(start), "arguments is not allowed in a class field initializer or a static block")
		}
		p.f.use |= useArguments
	}
	return id
}

func (p *parser) parseTemplate(tagged bool) *ast.TemplateLit {
	t := &ast.TemplateLit{}
	t.StartOff = p.t.start
	for {
		if !p.t.cooked && !tagged {
			p.failTok("invalid escape sequence in template literal")
		}
		t.Quasis = append(t.Quasis, ast.TemplateElement{
			Span:      span(p.t.start, p.t.end),
			Cooked:    p.t.str,
			Raw:       p.t.raw,
			HasCooked: p.t.cooked,
		})
		tail := p.t.tail
		p.next()
		if tail {
			break
		}
		t.Exprs = append(t.Exprs, p.parseExpressionIn())
		if p.t.kind != tRBrace {
			p.unexpected()
		}
		p.rescanTemplateContinuation()
	}
	t.EndOff = p.prevEnd
	return t
}

func (p *parser) parseArray() ast.Expr {
	arr := &ast.ArrayLit{}
	arr.StartOff = p.t.start
	p.next()
	noIn := p.f.noIn
	p.f.noIn = false
	var cover, patErr int32
	for p.t.kind != tRBrack {
		if p.t.kind == tComma {
			p.next()
			arr.Elements = append(arr.Elements, nil)
			continue
		}
		if p.t.kind == tEllipsis {
			start := p.t.start
			p.next()
			arg, c := p.parseAssignCover()
			if cover == 0 {
				cover = c
			}
			if patErr == 0 {
				patErr = p.patErrOf(arg)
			}
			arr.Elements = append(arr.Elements, &ast.SpreadElement{
				Span: span(start, p.prevEnd),
				Arg:  arg,
			})
			if p.t.kind == tComma && patErr == 0 {
				patErr = p.t.start + 1
			}
		} else {
			e, c := p.parseAssignCover()
			if cover == 0 {
				cover = c
			}
			if patErr == 0 {
				patErr = p.patErrOf(e)
			}
			arr.Elements = append(arr.Elements, e)
		}
		if p.t.kind != tRBrack {
			p.expect(tComma)
		}
	}
	p.next()
	p.f.noIn = noIn
	arr.EndOff = p.prevEnd
	if cover != 0 {
		p.cover, p.coverNode = cover, arr
	}
	p.patNode, p.patErr = arr, patErr
	return arr
}

func (p *parser) parseObject() ast.Expr {
	obj := &ast.ObjectLit{}
	obj.StartOff = p.t.start
	p.next()
	noIn := p.f.noIn
	p.f.noIn = false
	var cover, patErr int32
	var hasProto bool
	for p.t.kind != tRBrace {
		obj.Properties = append(obj.Properties, ast.Property{})
		prop := &obj.Properties[len(obj.Properties)-1]
		p.parseProperty(prop, &cover, &patErr, &hasProto)
		if p.t.kind != tRBrace {
			if prop.Kind == ast.PropertySpread && p.t.kind == tComma && patErr == 0 {
				patErr = p.t.start + 1
			}
			p.expect(tComma)
		}
	}
	p.next()
	p.f.noIn = noIn
	obj.EndOff = p.prevEnd
	if cover != 0 {
		p.cover, p.coverNode = cover, obj
	}
	p.patNode, p.patErr = obj, patErr
	return obj
}

func isPropertyKeyEnd(k tokKind) bool {
	switch k {
	case tLParen, tColon, tComma, tRBrace, tAssign:
		return true
	}
	return false
}

// parsePropertyKey parses a property name and reports whether it is computed.
func (p *parser) parsePropertyKey(allowPrivate bool) (ast.Expr, bool) {
	start := p.t.start
	switch k := p.t.kind; {
	case k.isWord():
		id := &ast.Identifier{
			Span: span(start, p.t.end),
			Name: p.t.str,
		}
		p.next()
		return id, false
	case k == tString:
		return p.parseStringLit(), false
	case k == tNumber:
		p.checkOctal()
		lit := &ast.NumberLit{
			Span:  span(start, p.t.end),
			Value: p.t.num,
		}
		p.next()
		return lit, false
	case k == tBigInt:
		lit := &ast.BigIntLit{
			Span:  span(start, p.t.end),
			Value: p.t.str,
		}
		p.next()
		return lit, false
	case k == tLBrack:
		p.next()
		e := p.parseAssignIn()
		p.expect(tRBrack)
		return e, true
	case k == tPrivateName && allowPrivate:
		name := &ast.PrivateName{
			Span: span(start, p.t.end),
			Name: p.t.str,
		}
		p.next()
		return name, false
	}
	p.unexpected()
	return nil, false
}

func (p *parser) parseProperty(prop *ast.Property, cover, patErr *int32, hasProto *bool) {
	start := p.t.start
	if p.t.kind == tEllipsis {
		p.next()
		arg, c := p.parseAssignCover()
		if *cover == 0 {
			*cover = c
		}
		if *patErr == 0 {
			*patErr = p.patErrOf(arg)
		}
		prop.Span = span(start, p.prevEnd)
		prop.Kind = ast.PropertySpread
		prop.Value = arg
		return
	}

	var async, generator bool
	keyKind := p.t.kind
	if p.t.kind == tIdent && !p.t.esc && (p.t.str == "get" || p.t.str == "set" || p.t.str == "async") {
		id := &ast.Identifier{
			Span: span(start, p.t.end),
			Name: p.t.str,
		}
		p.next()
		switch {
		case isPropertyKeyEnd(p.t.kind) || id.Name == "async" && p.t.nl:
			prop.Key = id
		case id.Name == "get":
			prop.Kind = ast.PropertyGet
		case id.Name == "set":
			prop.Kind = ast.PropertySet
		default:
			async = true
			generator = p.eat(tMul)
		}
	} else if p.eat(tMul) {
		generator = true
	}
	if prop.Key == nil {
		keyKind = p.t.kind
		prop.Key, prop.Computed = p.parsePropertyKey(false)
	}

	switch {
	case prop.Kind != ast.PropertyInit || async || generator || p.t.kind == tLParen:
		fn := &ast.Function{
			Method:    true,
			Async:     async,
			Generator: generator,
			Getter:    prop.Kind == ast.PropertyGet,
			Setter:    prop.Kind == ast.PropertySet,
		}
		if prop.Kind == ast.PropertyInit {
			prop.Kind = ast.PropertyMethod
		}
		p.parseMethod(fn, start)
		prop.Value = fn
	case p.t.kind == tColon:
		p.next()
		value, c := p.parseAssignCover()
		if *cover == 0 {
			*cover = c
		}
		if *patErr == 0 {
			*patErr = p.patErrOf(value)
		}
		prop.Value = value
		if !prop.Computed {
			var isProto bool
			switch k := prop.Key.(type) {
			case *ast.Identifier:
				isProto = k.Name == "__proto__"
			case *ast.StringLit:
				isProto = k.Value == "__proto__"
			}
			if isProto {
				if *hasProto && *cover == 0 {
					*cover = prop.Key.Start() + 1
				}
				*hasProto = true
			}
		}
	default:
		if keyKind != tIdent {
			if keyKind.isWord() {
				p.fail(int(start), "unexpected token '"+prop.Key.(*ast.Identifier).Name+"'")
			}
			p.unexpected()
		}
		id := prop.Key.(*ast.Identifier)
		p.checkIdent(id.Name, id.StartOff, false)
		if id.Name == "arguments" {
			if p.f.noArguments {
				p.fail(int(id.StartOff), "arguments is not allowed in a class field initializer or a static block")
			}
			p.f.use |= useArguments
		}
		prop.Shorthand = true
		prop.Value = id
		if p.t.kind == tAssign {
			if *cover == 0 {
				*cover = p.t.start + 1
			}
			p.next()
			def := p.parseAssignIn()
			prop.Value = &ast.AssignExpr{
				Span:   span(id.StartOff, p.prevEnd),
				Target: p.toSimpleTarget(id),
				Value:  def,
			}
		}
	}
	prop.Span = span(start, p.prevEnd)
}
