// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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
		if r := f.assignableReg(p.Name); r >= 0 {
			if r != src {
				f.emit(bytecode.OpMove, r, src, 0)
			}
			return
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
		if id, ok := p.Target.(*ast.Identifier); ok && !id.Parenthesized {
			f.named(p.Default, v, wtf8.String(id.Name))
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
	if len(p.Properties) > 0 {
		f.setPos(p.Properties[0].Key.Start())
	} else {
		f.setPos(p.Start())
	}
	f.emit(bytecode.OpRequireCoercible, src, 0, 0)

	// With a rest element, the keys that were read are kept in consecutive registers.
	keys := int32(-1)
	if p.Rest != nil {
		keys = f.newRegs(len(p.Properties))
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
				f.emit(bytecode.OpLoadK, keys+int32(i), f.constStr(name), 0)
			}
		default:
			k = f.newReg()
			if keys >= 0 {
				k = keys + int32(i)
			}
			f.expr(prop.Key, k)
			f.emit(bytecode.OpToPropertyKey, k, k, -1)
		}
		if static && mode == patAssign && f.copyPropertyToGlobal(prop, src, name) {
			f.free(mark)
			continue
		}
		// A reference as the target is evaluated after the key and before the get.
		ref, ok := f.elementRef(prop.Value, mode)
		v := int32(-1)
		if !ok {
			v = f.targetReg(prop.Value, mode)
		}
		if v < 0 {
			v = f.newReg()
		}
		f.setPos(prop.Key.Start())
		if static {
			f.emitGetProp(v, src, f.site(name))
		} else {
			f.emit(bytecode.OpGetIndex, v, src, k)
		}
		if ok {
			f.storeElement(ref, v)
		} else {
			f.destructure(prop.Value, v, mode)
		}
		f.free(mark)
	}
	if p.Rest != nil {
		// A reference as the target is evaluated before the properties are copied.
		ref, ok := f.elementRef(p.Rest, mode)
		v := f.newReg()
		f.emit(bytecode.OpCopyPropsExcept, v, src, keys)
		f.emit(bytecode.OpExtra, int32(len(p.Properties)), 0, 0)
		if ok {
			f.storeElement(ref, v)
		} else {
			f.destructure(p.Rest, v, mode)
		}
	}
}

// copyPropertyToGlobal compiles a named property assigned to a global whose binding needs a lookup.
func (f *fn) copyPropertyToGlobal(prop *ast.PatternProperty, src int32, key wtf8.String) bool {
	if f.c.conservativeAssignments {
		return false
	}
	id, ok := prop.Value.(*ast.Identifier)
	if !ok || f.dynDepth(id.Name) != 0 || f.resolve(id.Name).kind != refGlobal || f.c.globalLexical(id.Name) {
		return false
	}
	scope := f.newRegs(2)
	f.setPos(id.Start())
	copyProp := f.emit(bytecode.OpCopyPropToGlobal, scope, src, 0)
	// On a cache miss the scope is resolved and the ordinary read and store follow.
	f.setPos(prop.Key.Start())
	f.emitGetProp(scope+1, src, f.site(key))
	f.storeVarTo(id, id.Name, scope+1, false, scope)
	f.patchC(copyProp)
	return true
}

func (f *fn) arrayPattern(p *ast.ArrayPattern, src int32, mode patMode) {
	it := f.newRegs(2)
	f.emit(bytecode.OpGetIterator, it, src, bytecode.IterSync)
	if f.f.IsGenerator {
		// A yield* in the pattern can return from the generator.
		f.control = append(f.control, &ctl{
			kind: ctlArrayPattern,
			reg:  it,
		})
	}
	base := f.top
	start := f.pc()
	for _, e := range p.Elements {
		mark := f.top
		if e == nil {
			f.elision(it)
			f.free(mark)
			continue
		}
		// A reference as the target is evaluated before the iterator steps.
		ref, prepared := f.elementRef(e, mode)
		v := f.newReg()
		if _, rest := e.(*ast.RestElement); rest {
			f.emit(bytecode.OpNewArray, v, 0, 0)
			t := f.newReg()
			loop := f.pc()
			done := f.emit(bytecode.OpIterNext, t, it, 0)
			f.emit(bytecode.OpArrayPush, v, t, 0)
			f.jumpTo(loop)
			f.patchC(done)
		} else {
			if !prepared {
				if r := f.targetReg(e, mode); r >= 0 {
					v = r
				}
			}
			// The step stores undefined when the iterator is done.
			f.emit(bytecode.OpIterNext, v, it, f.pc()+1)
		}
		if prepared {
			f.storeElement(ref, v)
		} else {
			f.destructure(e, v, mode)
		}
		f.free(mark)
	}
	end := f.pc()
	if f.f.IsGenerator {
		f.control = f.control[:len(f.control)-1]
	}
	f.emit(bytecode.OpIterClose, it, -1, 0)
	skip := f.jump()

	// An exception while the iterator is not exhausted closes it.
	exc := f.newReg()
	f.addHandler(start, end, f.pc(), exc, true)
	f.emit(bytecode.OpCloseUpvals, base, 0, 0)
	f.emit(bytecode.OpIterClose, it, exc, 0)
	f.emit(bytecode.OpRethrow, exc, -1, 0)
	f.patch(skip)
}

// targetReg returns the register that destructuring stores to for the target e when that
// store is a plain register write, or -1.
func (f *fn) targetReg(e ast.Pattern, mode patMode) int32 {
	if f.c.conservativeAssignments {
		return -1
	}
	id, ok := e.(*ast.Identifier)
	if !ok {
		return -1
	}
	if mode == patInit {
		if b, _ := f.lookupLocal(id.Name); b != nil {
			return b.reg
		}
		return -1
	}
	return f.assignableReg(id.Name)
}

// assignableReg returns the register of the local variable name when an assignment to it is a
// plain register write, or -1.
func (f *fn) assignableReg(name string) int32 {
	if f.c.conservativeAssignments {
		return -1
	}
	if b := f.uncheckedLocal(name); b != nil && (b.kind == bindVar || b.kind == bindLet) {
		return b.reg
	}
	return -1
}

// elision steps the iterator it, it+1 without reading the value of the iterator result, as
// IteratorStep does for an Elision.
func (f *fn) elision(it int32) {
	t := f.newRegs(3)
	f.emit(bytecode.OpTypeof, t, it+1, 0)
	f.emit(bytecode.OpLoadK, t+1, f.constStr("number"), 0)
	generic := f.emit(bytecode.OpJumpNotSEq, 0, t, t+1)
	// A plain array iterated by index has no iterator result to skip.
	arrayDone := f.emit(bytecode.OpIterNext, t, it, 0)
	arrayStepped := f.jump()

	f.patch(generic)
	finished := f.emit(bytecode.OpJumpIfUndef, 0, it, 0)
	// The iterator counts as finished while it steps, so that a step that throws does not close it.
	f.emit(bytecode.OpMove, t, it, 0)
	f.emit(bytecode.OpLoadUndef, it, 0, 0)
	f.emit(bytecode.OpMove, t+1, it+1, 0)
	f.emit(bytecode.OpMove, t+2, t, 0)
	f.emit(bytecode.OpCall, t+1, t+1, 0)
	f.emit(bytecode.OpIterCheckResult, t+1, 0, 0)
	f.emitGetProp(t+1, t+1, f.site("done"))
	done := f.emit(bytecode.OpJumpIf, 0, t+1, 0)
	f.emit(bytecode.OpMove, it, t, 0)

	f.patchC(arrayDone)
	f.patch(arrayStepped)
	f.patch(finished)
	f.patch(done)
}

// elementRef is the reference that is the target of an assignment element, evaluated before
// the element's value.
type elementRef struct {
	// member is the property reference, when isMember is set.
	member   memberTarget
	isMember bool

	// id is the identifier whose assignmentScope result is in scope.
	id    *ast.Identifier
	scope int32

	// def is the element's initializer, or nil.
	def ast.Expr
}

// elementRef evaluates the reference that is the target of the assignment element e when
// its evaluation must precede the property read, iterator step or initializer.
func (f *fn) elementRef(e ast.Pattern, mode patMode) (ref elementRef, ok bool) {
	if mode != patAssign {
		return elementRef{}, false
	}
	t := e
	if r, ok := t.(*ast.RestElement); ok {
		t = r.Target
	} else if a, ok := t.(*ast.AssignPattern); ok {
		t, ref.def = a.Target, a.Default
	}
	switch t := t.(type) {
	case *ast.MemberExpr:
		// Reading the value, which can run code, and the initializer follow the reference.
		ref.member = f.assignTargetBefore(t, ref.def, false, true, false)
		ref.isMember = true
		return ref, true
	case *ast.Identifier:
		ref.scope = f.assignmentScope(t, t.Name)
		if ref.scope < 0 {
			return elementRef{}, false
		}
		ref.id = t
		return ref, true
	}
	return elementRef{}, false
}

// storeElement stores the value in the register v, or the initializer when v is undefined, to ref.
func (f *fn) storeElement(ref elementRef, v int32) {
	if ref.def != nil {
		skip := f.emit(bytecode.OpJumpIfNotUndef, 0, v, 0)
		if ref.isMember || ref.id.Parenthesized {
			f.expr(ref.def, v)
		} else {
			f.named(ref.def, v, wtf8.String(ref.id.Name))
		}
		f.patch(skip)
	}
	if ref.isMember {
		f.storeTarget(ref.member, v)
		return
	}
	f.storeVarTo(ref.id, ref.id.Name, v, false, ref.scope)
}
