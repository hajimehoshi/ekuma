// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// toAssignTarget converts the left side of a plain assignment, or the head of a for-in or for-of
// statement, into an assignment target. e must be the most recently parsed expression.
func (p *parser) toAssignTarget(e ast.Expr) ast.Pattern {
	switch e.(type) {
	case *ast.ObjectLit, *ast.ArrayLit:
		if pos := p.patErrOf(e); pos != 0 {
			p.fail(int(pos-1), "invalid destructuring target")
		}
		return p.toPattern(e, false)
	}
	if p.isCallTarget(e) {
		return e.(*ast.CallExpr)
	}
	return p.toSimpleTarget(e)
}

// isCallTarget reports whether e is a function call that sloppy mode accepts as the target of
// an assignment other than a logical one, of an update or of a for-in or for-of head.
func (p *parser) isCallTarget(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok || p.f.strict {
		return false
	}
	_, super := c.Callee.(*ast.SuperExpr)
	return !super
}

// toSimpleTarget checks that e is a simple assignment target.
func (p *parser) toSimpleTarget(e ast.Expr) ast.Pattern {
	switch e := e.(type) {
	case *ast.Identifier:
		if p.f.strict && (e.Name == "eval" || e.Name == "arguments") {
			p.fail(int(e.StartOff), "'"+e.Name+"' cannot be bound or assigned in strict mode")
		}
		return e
	case *ast.MemberExpr:
		return e
	}
	p.fail(int(e.Start()), "invalid assignment target")
	return nil
}

// toPattern converts an expression into a pattern. binding selects a binding pattern, as in
// arrow function parameters, over an assignment pattern.
func (p *parser) toPattern(e ast.Expr, binding bool) ast.Pattern {
	switch e := e.(type) {
	case *ast.Identifier:
		if binding {
			if e.Parenthesized {
				p.fail(int(e.StartOff), "invalid binding target")
			}
			return e
		}
		return p.toSimpleTarget(e)
	case *ast.MemberExpr:
		if binding {
			p.fail(int(e.StartOff), "invalid binding target")
		}
		return e
	case *ast.ArrayLit:
		pat := &ast.ArrayPattern{
			Span: e.Span,
		}
		if len(e.Elements) > 0 {
			pat.Elements = make([]ast.Pattern, len(e.Elements))
		}
		for i, elem := range e.Elements {
			if elem == nil {
				continue
			}
			if s, ok := elem.(*ast.SpreadElement); ok {
				if i != len(e.Elements)-1 {
					p.fail(int(s.StartOff), "rest element must be the last element")
				}
				pat.Elements[i] = &ast.RestElement{
					Span:   s.Span,
					Target: p.toPattern(s.Arg, binding),
				}
				continue
			}
			pat.Elements[i] = p.toElement(elem, binding)
		}
		return pat
	case *ast.ObjectLit:
		pat := &ast.ObjectPattern{
			Span: e.Span,
		}
		for i := range e.Properties {
			prop := &e.Properties[i]
			switch prop.Kind {
			case ast.PropertyInit:
				pat.Properties = append(pat.Properties, ast.PatternProperty{
					Span:      prop.Span,
					Computed:  prop.Computed,
					Shorthand: prop.Shorthand,
					Key:       prop.Key,
					Value:     p.toElement(prop.Value, binding),
				})
			case ast.PropertySpread:
				if i != len(e.Properties)-1 {
					p.fail(int(prop.StartOff), "rest element must be the last element")
				}
				switch arg := prop.Value.(type) {
				case *ast.Identifier:
					pat.Rest = p.toPattern(arg, binding)
				case *ast.MemberExpr:
					pat.Rest = p.toPattern(arg, binding)
				default:
					p.fail(int(prop.Value.Start()), "invalid rest element target")
				}
			default:
				p.fail(int(prop.StartOff), "invalid destructuring target")
			}
		}
		return pat
	}
	p.fail(int(e.Start()), "invalid destructuring target")
	return nil
}

// toElement converts an expression into a pattern element, which may carry a default value.
func (p *parser) toElement(e ast.Expr, binding bool) ast.Pattern {
	if a, ok := e.(*ast.AssignExpr); ok && a.Op == ast.OpNone {
		if binding {
			p.checkBindingPattern(a.Target)
		}
		return &ast.AssignPattern{
			Span:    a.Span,
			Target:  a.Target,
			Default: a.Value,
		}
	}
	return p.toPattern(e, binding)
}

// checkBindingPattern checks that an assignment pattern is also a valid binding pattern.
func (p *parser) checkBindingPattern(pat ast.Pattern) {
	switch pat := pat.(type) {
	case *ast.Identifier:
		if pat.Parenthesized {
			p.fail(int(pat.StartOff), "invalid binding target")
		}
	case *ast.ObjectPattern:
		for i := range pat.Properties {
			p.checkBindingPattern(pat.Properties[i].Value)
		}
		if pat.Rest != nil {
			p.checkBindingPattern(pat.Rest)
		}
	case *ast.ArrayPattern:
		for _, e := range pat.Elements {
			if e != nil {
				p.checkBindingPattern(e)
			}
		}
	case *ast.AssignPattern:
		p.checkBindingPattern(pat.Target)
	case *ast.RestElement:
		p.checkBindingPattern(pat.Target)
	default:
		p.fail(int(pat.Start()), "invalid binding target")
	}
}

// parseBindingTarget parses a binding identifier or a binding pattern. It does not declare the bound names.
func (p *parser) parseBindingTarget() ast.Pattern {
	start := p.t.start
	switch p.t.kind {
	case tIdent:
		id := &ast.Identifier{
			Span: span(start, p.t.end),
			Name: p.t.str,
		}
		// The binding is checked when it is declared, possibly in a function state other than the
		// current one, so a use of await that may belong to an enclosing async arrow head is noted here.
		p.noteAwaitIdent(id.Name, start)
		p.next()
		return id
	case tLBrack:
		p.enter()
		p.next()
		pat := &ast.ArrayPattern{}
		for p.t.kind != tRBrack {
			if p.t.kind == tComma {
				p.next()
				pat.Elements = append(pat.Elements, nil)
				continue
			}
			if p.t.kind == tEllipsis {
				restStart := p.t.start
				p.next()
				target := p.parseBindingTarget()
				pat.Elements = append(pat.Elements, &ast.RestElement{
					Span:   span(restStart, p.prevEnd),
					Target: target,
				})
				if p.t.kind != tRBrack {
					p.failTok("rest element must be the last element")
				}
				break
			}
			pat.Elements = append(pat.Elements, p.parseBindingElement())
			if p.t.kind != tRBrack {
				p.expect(tComma)
			}
		}
		p.next()
		p.leave()
		pat.Span = span(start, p.prevEnd)
		return pat
	case tLBrace:
		p.enter()
		p.next()
		pat := &ast.ObjectPattern{}
		for p.t.kind != tRBrace {
			propStart := p.t.start
			if p.t.kind == tEllipsis {
				p.next()
				if p.t.kind != tIdent {
					p.unexpected()
				}
				pat.Rest = p.parseBindingTarget()
				if p.t.kind != tRBrace {
					p.failTok("rest element must be the last element")
				}
				break
			}
			keyKind := p.t.kind
			key, computed := p.parsePropertyKey(false)
			prop := ast.PatternProperty{
				Computed: computed,
				Key:      key,
			}
			if p.eat(tColon) {
				prop.Value = p.parseBindingElement()
			} else {
				if keyKind != tIdent {
					if keyKind.isWord() {
						p.fail(int(propStart), "unexpected token '"+key.(*ast.Identifier).Name+"'")
					}
					p.unexpected()
				}
				id := key.(*ast.Identifier)
				p.noteAwaitIdent(id.Name, propStart)
				prop.Shorthand = true
				prop.Value = id
				if p.eat(tAssign) {
					def := p.parseAssignIn()
					prop.Value = &ast.AssignPattern{
						Span:    span(propStart, p.prevEnd),
						Target:  id,
						Default: def,
					}
				}
			}
			prop.Span = span(propStart, p.prevEnd)
			pat.Properties = append(pat.Properties, prop)
			if p.t.kind != tRBrace {
				p.expect(tComma)
			}
		}
		p.next()
		p.leave()
		pat.Span = span(start, p.prevEnd)
		return pat
	}
	p.unexpected()
	return nil
}

// parseBindingElement parses a binding target with an optional default value.
func (p *parser) parseBindingElement() ast.Pattern {
	start := p.t.start
	target := p.parseBindingTarget()
	if !p.eat(tAssign) {
		return target
	}
	def := p.parseAssignIn()
	return &ast.AssignPattern{
		Span:    span(start, p.prevEnd),
		Target:  target,
		Default: def,
	}
}
