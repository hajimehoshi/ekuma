// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

type patMode uint8

const (
	// patAssign stores to existing variables and properties.
	patAssign patMode = iota

	// patInit initializes the variables of a lexical declaration.
	patInit
)

// destructure stores the value in the register src to the pattern p.
func (f *fn) destructure(p ast.Pattern, src int32, mode patMode) {
	mark := f.top
	defer f.free(mark)
	switch p := p.(type) {
	case *ast.Identifier:
		if mode == patInit {
			f.initBinding(p, p.Name, src)
			return
		}
		if r := f.localReg(p.Name); r >= 0 {
			if b, _ := f.lookupLocal(p.Name); b.kind == bindVar || b.kind == bindLet {
				if r != src {
					f.emit(bytecode.OpMove, r, src, 0)
				}
				return
			}
		}
		f.storeVar(p, p.Name, src, false)
	case *ast.MemberExpr:
		t := f.assignTarget(p)
		f.storeTarget(t, src)
	case *ast.CallExpr:
		f.callTarget(p)
	case *ast.AssignPattern:
		v := f.newReg()
		f.emit(bytecode.OpMove, v, src, 0)
		skip := f.emit(bytecode.OpJumpIfNotUndef, 0, v, 0)
		if id, ok := p.Target.(*ast.Identifier); ok {
			f.named(p.Default, v, id.Name)
		} else {
			f.expr(p.Default, v)
		}
		f.patch(skip)
		f.destructure(p.Target, v, mode)
	case *ast.ObjectPattern:
		f.objectPattern(p, src, mode)
	case *ast.ArrayPattern:
		f.arrayPattern(p, src, mode)
	case *ast.RestElement:
		f.destructure(p.Target, src, mode)
	default:
		f.c.fail(p, "unsupported pattern %T", p)
	}
}

func (f *fn) objectPattern(p *ast.ObjectPattern, src int32, mode patMode) {
	f.emit(bytecode.OpRequireCoercible, src, 0, 0)

	// With a rest element, the keys that were read are kept in consecutive registers.
	keys := int32(-1)
	if p.Rest != nil {
		keys = f.newRegs(len(p.Properties) + 1)
	}
	for i := range p.Properties {
		prop := &p.Properties[i]
		mark := f.top
		name, static := keyName(prop.Key, prop.Computed)
		static = static && !isIndexString(name)
		var k int32
		switch {
		case static:
			if keys >= 0 {
				f.emit(bytecode.OpLoadK, keys+1+int32(i), f.constStr(name), 0)
			}
		default:
			k = f.newReg()
			if keys >= 0 {
				k = keys + 1 + int32(i)
			}
			f.expr(prop.Key, k)
			f.emit(bytecode.OpToPropertyKey, k, k, 0)
		}
		// A property reference as the target is evaluated after the key and before the get.
		target, def, member := f.memberElement(prop.Value, mode)
		v := f.newReg()
		if static {
			f.emit(bytecode.OpGetProp, v, src, f.site(name))
		} else {
			f.emit(bytecode.OpGetIndex, v, src, k)
		}
		if member {
			f.storeElement(target, def, v)
		} else {
			f.destructure(prop.Value, v, mode)
		}
		f.free(mark)
	}
	if p.Rest != nil {
		// A property reference as the target is evaluated before the properties are copied.
		target, _, member := f.memberElement(p.Rest, mode)
		v := f.newReg()
		f.emit(bytecode.OpCopyPropsExcept, v, src, keys)
		f.emit(bytecode.OpNop, int32(len(p.Properties)), 0, 0)
		if member {
			f.storeTarget(target, v)
		} else {
			f.destructure(p.Rest, v, mode)
		}
	}
}

func (f *fn) arrayPattern(p *ast.ArrayPattern, src int32, mode patMode) {
	it := f.newRegs(2)
	f.emit(bytecode.OpGetIterator, it, src, bytecode.IterSync)
	base := f.top
	start := f.pc()
	for _, e := range p.Elements {
		mark := f.top
		// A property reference as the target is evaluated before the iterator steps.
		target, def, member := f.memberElement(e, mode)
		v := f.newReg()
		if rest, ok := e.(*ast.RestElement); ok {
			f.emit(bytecode.OpNewArray, v, 0, 0)
			t := f.newReg()
			loop := f.pc()
			done := f.emit(bytecode.OpIterNext, t, it, 0)
			f.emit(bytecode.OpArrayPush, v, t, 0)
			f.jumpTo(loop)
			f.patchC(done)
			if member {
				f.storeTarget(target, v)
			} else {
				f.destructure(rest.Target, v, mode)
			}
			f.free(mark)
			continue
		}
		done := f.emit(bytecode.OpIterNext, v, it, 0)
		ok := f.jump()
		f.patchC(done)
		f.emit(bytecode.OpLoadUndef, v, 0, 0)
		f.patch(ok)
		switch {
		case member:
			f.storeElement(target, def, v)
		case e != nil:
			f.destructure(e, v, mode)
		}
		f.free(mark)
	}
	end := f.pc()
	f.emit(bytecode.OpIterClose, it, 0, 0)
	skip := f.jump()

	// An exception while the iterator is not exhausted closes it.
	exc := f.newReg()
	f.addHandler(start, end, f.pc(), exc, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	f.emit(bytecode.OpIterClose, it, exc+1, 0)
	f.emit(bytecode.OpRethrow, exc, 0, 0)
	f.patch(skip)
}

// memberElement evaluates the property reference that is the target of the assignment element e, if any.
// def is the element's initializer, or nil.
func (f *fn) memberElement(e ast.Pattern, mode patMode) (target memberTarget, def ast.Expr, ok bool) {
	if mode != patAssign {
		return memberTarget{}, nil, false
	}
	t := e
	if r, ok := t.(*ast.RestElement); ok {
		t = r.Target
	} else if a, ok := t.(*ast.AssignPattern); ok {
		t, def = a.Target, a.Default
	}
	m, ok := t.(*ast.MemberExpr)
	if !ok {
		return memberTarget{}, nil, false
	}
	return f.assignTarget(m), def, true
}

// storeElement stores the value in the register v, or def when v is undefined, to target.
func (f *fn) storeElement(target memberTarget, def ast.Expr, v int32) {
	if def != nil {
		skip := f.emit(bytecode.OpJumpIfNotUndef, 0, v, 0)
		f.expr(def, v)
		f.patch(skip)
	}
	f.storeTarget(target, v)
}
