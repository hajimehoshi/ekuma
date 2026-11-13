// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// parseModuleName parses an import or export name: an IdentifierName or a string literal.
func (p *parser) parseModuleName() ast.ModuleName {
	n := ast.ModuleName{
		Span: span(p.t.start, p.t.end),
		Name: p.t.str,
	}
	switch {
	case p.t.kind == tString:
		if hasLoneSurrogate(p.t.str) {
			p.failTok("module export name must not contain a lone surrogate")
		}
		n.IsString = true
	case p.t.kind.isWord():
	default:
		p.unexpected()
	}
	p.next()
	return n
}

func (p *parser) expectWord(s string) {
	if !p.isWord(s) {
		p.unexpected()
	}
	p.next()
}

// parseImportBinding parses and declares the local name of an import.
func (p *parser) parseImportBinding() *ast.Identifier {
	if p.t.kind != tIdent {
		p.unexpected()
	}
	id := &ast.Identifier{
		Span: span(p.t.start, p.t.end),
		Name: p.t.str,
	}
	p.checkIdent(id.Name, id.StartOff, true)
	p.declareLexical(id.Name, id.StartOff, ast.DeclImport, nil, false)
	p.next()
	return id
}

func (p *parser) parseModuleSource() *ast.StringLit {
	if p.t.kind != tString {
		p.unexpected()
	}
	return p.parseStringLit()
}

// parseImportAttributes parses an optional with clause.
func (p *parser) parseImportAttributes() []ast.ImportAttribute {
	if !p.eat(tWith) {
		return nil
	}
	p.expect(tLBrace)
	attrs := []ast.ImportAttribute{}
	for p.t.kind != tRBrace {
		if p.t.kind != tString && !p.t.kind.isWord() {
			p.unexpected()
		}
		a := ast.ImportAttribute{
			Key: p.t.str,
		}
		a.StartOff = p.t.start
		for i := range attrs {
			if attrs[i].Key == a.Key {
				p.failTok("duplicate import attribute '" + a.Key + "'")
			}
		}
		p.next()
		p.expect(tColon)
		if p.t.kind != tString {
			p.unexpected()
		}
		a.Value = p.parseStringLit()
		a.EndOff = p.prevEnd
		attrs = append(attrs, a)
		if p.t.kind != tRBrace {
			p.expect(tComma)
		}
	}
	p.next()
	return attrs
}

// parseImport parses an import declaration. The current token is the import keyword.
func (p *parser) parseImport() ast.Stmt {
	d := &ast.ImportDecl{}
	d.StartOff = p.t.start
	p.next()
	if p.t.kind != tString {
		more := true
		if p.t.kind == tIdent {
			start := p.t.start
			local := p.parseImportBinding()
			d.Specifiers = append(d.Specifiers, ast.ImportSpecifier{
				Span:  span(start, p.prevEnd),
				Kind:  ast.ImportDefault,
				Local: local,
			})
			more = p.eat(tComma)
		}
		switch {
		case !more:
		case p.t.kind == tMul:
			start := p.t.start
			p.next()
			p.expectWord("as")
			local := p.parseImportBinding()
			d.Specifiers = append(d.Specifiers, ast.ImportSpecifier{
				Span:  span(start, p.prevEnd),
				Kind:  ast.ImportNamespace,
				Local: local,
			})
		case p.t.kind == tLBrace:
			p.next()
			for p.t.kind != tRBrace {
				start := p.t.start
				plain := p.t.kind == tIdent
				imported := p.parseModuleName()
				var local *ast.Identifier
				if p.isWord("as") {
					p.next()
					local = p.parseImportBinding()
				} else {
					if !plain {
						p.fail(int(start), "import name '"+imported.Name+"' requires a local binding")
					}
					local = &ast.Identifier{
						Span: imported.Span,
						Name: imported.Name,
					}
					p.checkIdent(local.Name, local.StartOff, true)
					p.declareLexical(local.Name, local.StartOff, ast.DeclImport, nil, false)
				}
				d.Specifiers = append(d.Specifiers, ast.ImportSpecifier{
					Span:     span(start, p.prevEnd),
					Kind:     ast.ImportNamed,
					Imported: imported,
					Local:    local,
				})
				if p.t.kind != tRBrace {
					p.expect(tComma)
				}
			}
			p.next()
		default:
			p.unexpected()
		}
		p.expectWord("from")
	}
	d.Source = p.parseModuleSource()
	d.Attributes = p.parseImportAttributes()
	p.consumeSemicolon()
	d.EndOff = p.prevEnd
	return d
}

func (p *parser) addExport(name string, pos int32) {
	if p.exported == nil {
		p.exported = map[string]struct{}{}
	}
	if _, ok := p.exported[name]; ok {
		p.fail(int(pos), "duplicate export '"+name+"'")
	}
	p.exported[name] = struct{}{}
}

// addPatternExports records every name bound by pat as an export.
func (p *parser) addPatternExports(pat ast.Pattern) {
	switch pat := pat.(type) {
	case *ast.Identifier:
		p.addExport(pat.Name, pat.StartOff)
	case *ast.ObjectPattern:
		for i := range pat.Properties {
			p.addPatternExports(pat.Properties[i].Value)
		}
		if pat.Rest != nil {
			p.addPatternExports(pat.Rest)
		}
	case *ast.ArrayPattern:
		for _, e := range pat.Elements {
			if e != nil {
				p.addPatternExports(e)
			}
		}
	case *ast.AssignPattern:
		p.addPatternExports(pat.Target)
	case *ast.RestElement:
		p.addPatternExports(pat.Target)
	}
}

// parseExport parses an export declaration. The current token is the export keyword.
func (p *parser) parseExport() ast.Stmt {
	start := p.t.start
	p.next()
	switch {
	case p.t.kind == tMul:
		d := &ast.ExportAllDecl{}
		p.next()
		if p.isWord("as") {
			p.next()
			name := p.parseModuleName()
			p.addExport(name.Name, name.StartOff)
			d.Exported = &name
		}
		p.expectWord("from")
		d.Source = p.parseModuleSource()
		d.Attributes = p.parseImportAttributes()
		p.consumeSemicolon()
		d.Span = span(start, p.prevEnd)
		return d

	case p.t.kind == tDefault:
		p.addExport("default", p.t.start)
		p.next()
		d := &ast.ExportDefaultDecl{}
		declStart := p.t.start
		switch {
		case p.t.kind == tFunction:
			d.Decl = p.parseFunctionDecl(declStart, false, ctxList, true)
		case p.t.kind == tClass:
			cls := p.parseClass(true, true)
			d.Decl = &ast.ClassDecl{
				Span:  cls.Span,
				Class: cls,
			}
		default:
			if p.isWord("async") {
				if pk := p.peek(); pk.kind == tFunction && !pk.nl {
					p.next()
					d.Decl = p.parseFunctionDecl(declStart, true, ctxList, true)
					break
				}
			}
			d.Expr = p.parseAssignIn()
			p.declareLexical("*default*", declStart, ast.DeclConst, nil, false)
			p.consumeSemicolon()
		}
		d.Span = span(start, p.prevEnd)
		return d

	case p.t.kind == tLBrace:
		d := &ast.ExportNamedDecl{}
		p.next()

		// localKinds holds the token kind of each local name, which matters without a from clause.
		var localKinds []tokKind
		for p.t.kind != tRBrace {
			specStart := p.t.start
			localKinds = append(localKinds, p.t.kind)
			local := p.parseModuleName()
			exported := local
			if p.isWord("as") {
				p.next()
				exported = p.parseModuleName()
			}
			d.Specifiers = append(d.Specifiers, ast.ExportSpecifier{
				Span:     span(specStart, p.prevEnd),
				Local:    local,
				Exported: exported,
			})
			if p.t.kind != tRBrace {
				p.expect(tComma)
			}
		}
		p.next()
		if p.isWord("from") {
			p.next()
			d.Source = p.parseModuleSource()
			d.Attributes = p.parseImportAttributes()
		} else {
			for i := range d.Specifiers {
				local := &d.Specifiers[i].Local
				if localKinds[i] != tIdent {
					p.fail(int(local.StartOff), "export of '"+local.Name+"' requires a local binding")
				}
				p.checkIdent(local.Name, local.StartOff, false)
				p.exportLocals = append(p.exportLocals, exportLocal{
					name: local.Name,
					pos:  local.StartOff,
				})
			}
		}
		for i := range d.Specifiers {
			p.addExport(d.Specifiers[i].Exported.Name, d.Specifiers[i].Exported.StartOff)
		}
		p.consumeSemicolon()
		d.Span = span(start, p.prevEnd)
		return d
	}

	decl := p.parseStatement(ctxList)
	switch s := decl.(type) {
	case *ast.VarDecl:
		for i := range s.Decls {
			p.addPatternExports(s.Decls[i].Target)
		}
	case *ast.FuncDecl:
		p.addExport(s.Func.Name.Name, s.Func.Name.StartOff)
	case *ast.ClassDecl:
		p.addExport(s.Class.Name.Name, s.Class.Name.StartOff)
	default:
		p.fail(int(decl.Start()), "export requires a declaration")
	}
	return &ast.ExportNamedDecl{
		Span: span(start, p.prevEnd),
		Decl: decl,
	}
}
