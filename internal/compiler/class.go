// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// defaultConstructor returns the code of the constructor of a class that does not write one.
func (c *compiler) defaultConstructor(name string, derived bool) *bytecode.Func {
	b := &bytecode.Func{
		Name:               name,
		File:               c.file,
		Strict:             true,
		IsConstructor:      true,
		IsClassConstructor: true,
		IsDerived:          derived,
	}
	emit := func(op bytecode.Opcode, a, b2, c2 int32) {
		b.Code = append(b.Code, bytecode.Instr{
			Op: op,
			A:  a,
			B:  b2,
			C:  c2,
		})
	}
	if !derived {
		b.Source = "constructor() {}"
		b.NumRegs = 2
		emit(bytecode.OpLoadCallee, 0, 0, 0)
		emit(bytecode.OpLoadThis, 1, 0, 0)
		emit(bytecode.OpInitFields, 0, 1, 0)
		emit(bytecode.OpReturnUndef, 0, 0, 0)
		return b
	}
	// constructor(...args) { super(...args); }
	b.Source = "constructor(...args) { super(...args); }"
	b.NeedsExtra = true
	b.NumRegs = 4
	emit(bytecode.OpLoadCallee, 0, 0, 0)
	emit(bytecode.OpLoadNewTarget, 1, 0, 0)
	emit(bytecode.OpRestArgs, 2, 0, 0)
	emit(bytecode.OpSuperSpread, 0, 0, 0)
	emit(bytecode.OpLoadCallee, 1, 0, 0)
	emit(bytecode.OpInitFields, 1, 0, 0)
	emit(bytecode.OpReturn, 0, 0, 0)
	return b
}

// class compiles the class n into dst. name is the value of the name property.
func (f *fn) class(n *ast.Class, dst int32, name string) {
	mark := f.top
	defer f.free(mark)
	if n.Name != nil {
		name = n.Name.Name
	}

	sc := f.pushScope()
	f.control = append(f.control, &ctl{
		kind: ctlScope,
		reg:  sc.firstReg,
	})
	var inner *binding
	if n.Name != nil {
		inner = f.declare(n.Name.Name, bindConst)
		inner.tdz = true
		f.emit(bytecode.OpLoadTDZ, inner.reg, 0, 0)
	}
	// Private names are variables of the class scope, so that closures find them.
	for i := range n.Members {
		m := &n.Members[i]
		p, ok := m.Key.(*ast.PrivateName)
		if !ok || m.Computed {
			continue
		}
		if _, exists := f.scope.vars["#"+p.Name]; exists {
			continue
		}
		b := f.declare("#"+p.Name, bindPseudo)
		isMethod := int32(0)
		if m.Kind == ast.MemberMethod {
			isMethod = 1
		}
		f.emit(bytecode.OpPrivateName, b.reg, f.key(p.Name), isMethod)
	}

	parent := int32(-1)
	if n.Super != nil {
		parent = f.newReg()
		f.expr(n.Super, parent)
	}

	// ctor holds the constructor and ctor+1 the prototype object.
	ctor := f.newRegs(2)
	if n.Constructor != nil {
		f.closure(n.Constructor, ctor, name)
	} else {
		f.f.Protos = append(f.f.Protos, f.c.defaultConstructor(name, n.Super != nil))
		f.emit(bytecode.OpClosure, ctor, int32(len(f.f.Protos)-1), 0)
	}
	f.emit(bytecode.OpClass, ctor, 0, parent)
	proto := ctor + 1

	// The static fields and blocks run after everything else is defined.
	type static struct {
		key, value int32
		block      bool
	}
	var statics []static

	for i := range n.Members {
		m := &n.Members[i]
		obj := proto
		if m.Static {
			obj = ctor
		}
		_, private := m.Key.(*ast.PrivateName)
		private = private && !m.Computed
		switch m.Kind {
		case ast.MemberConstructor:
		case ast.MemberMethod, ast.MemberGetter, ast.MemberSetter:
			kind := ast.PropertyMethod
			elem := int32(bytecode.ElemMethod)
			switch m.Kind {
			case ast.MemberGetter:
				kind = ast.PropertyGet
				elem = bytecode.ElemGetter
			case ast.MemberSetter:
				kind = ast.PropertySet
				elem = bytecode.ElemSetter
			}
			if !private {
				mm := f.top
				f.method(obj, m.Key, m.Computed, m.Value, kind, false)
				f.free(mm)
				break
			}
			mm := f.top
			k := f.propertyKey(m.Key, false)
			v := f.newReg()
			fname := "#" + m.Key.(*ast.PrivateName).Name
			switch m.Kind {
			case ast.MemberGetter:
				fname = "get " + fname
			case ast.MemberSetter:
				fname = "set " + fname
			}
			f.closure(m.Value, v, fname)
			f.emit(bytecode.OpSetHome, v, obj, 0)
			if m.Static {
				elem |= bytecode.ElemStatic
			}
			f.emit(bytecode.OpAddField, ctor, k, v)
			f.emit(bytecode.OpNop, elem, 0, 0)
			f.free(mm)
		case ast.MemberField:
			// The key and the initializer stay in registers until the static fields run.
			k := f.propertyKey(m.Key, m.Computed)
			v := int32(-1)
			if m.Value != nil {
				v = f.newReg()
				if fname, ok := keyName(m.Key, m.Computed); ok {
					f.c.fieldNames[m.Value] = fname
				} else if private {
					f.c.fieldNames[m.Value] = "#" + m.Key.(*ast.PrivateName).Name
				}
				f.closure(m.Value, v, "")
				f.emit(bytecode.OpSetHome, v, obj, 0)
			}
			if m.Static {
				statics = append(statics, static{
					key:   k,
					value: v,
				})
				break
			}
			f.emit(bytecode.OpAddField, ctor, k, v)
			f.emit(bytecode.OpNop, bytecode.ElemField, 0, 0)
		case ast.MemberStaticBlock:
			v := f.newReg()
			f.closure(m.Value, v, "")
			f.emit(bytecode.OpSetHome, v, ctor, 0)
			statics = append(statics, static{
				value: v,
				block: true,
			})
		}
	}

	if inner != nil {
		f.emit(bytecode.OpMove, inner.reg, ctor, 0)
		inner.tdz = false
	}
	for _, s := range statics {
		if s.block {
			base := f.newRegs(2)
			f.emit(bytecode.OpMove, base, s.value, 0)
			f.emit(bytecode.OpMove, base+1, ctor, 0)
			f.emit(bytecode.OpCall, base, 0, 0)
			f.free(base)
			continue
		}
		f.emit(bytecode.OpAddField, ctor, s.key, s.value)
		f.emit(bytecode.OpNop, bytecode.ElemField|bytecode.ElemStatic, 0, 0)
	}

	f.emit(bytecode.OpMove, dst, ctor, 0)
	f.control = f.control[:len(f.control)-1]
	f.popScope()
}
