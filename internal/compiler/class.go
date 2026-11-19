// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"strconv"

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
		b.NumRegs = 2
		emit(bytecode.OpLoadCallee, 0, 0, 0)
		emit(bytecode.OpLoadThis, 1, 0, 0)
		emit(bytecode.OpInitFields, 0, 1, 0)
		emit(bytecode.OpReturnUndef, 0, 0, 0)
		return b
	}
	// constructor(...args) { super(...args); }
	b.NeedsExtra = true
	b.NumRegs = 4
	emit(bytecode.OpLoadCallee, 0, 0, 0)
	emit(bytecode.OpSuperBase, 0, 0, 0)
	emit(bytecode.OpLoadNewTarget, 1, 0, 0)
	emit(bytecode.OpRestArgs, 2, 0, 0)
	emit(bytecode.OpSuperSpread, 0, 0, 0)
	emit(bytecode.OpLoadCallee, 1, 0, 0)
	emit(bytecode.OpInitFields, 1, 0, 0)
	emit(bytecode.OpReturn, 0, 0, 0)
	return b
}

// class compiles the class n into dst. name is the value of the name property. When the
// register nameKey is not -1 and n has no name, the name is that of the property key in it.
func (f *fn) class(n *ast.Class, dst int32, name string, nameKey int32) {
	mark := f.top
	defer f.free(mark)
	if n.Name != nil {
		name = n.Name.Name
		nameKey = -1
	}

	// state holds the state of the evaluation of a class with decorators. The decorators of
	// the class are outside the class body.
	state := int32(-1)
	if n.Decorators != nil || n.MemberDecorators {
		state = f.newReg()
		if nameKey >= 0 {
			f.decorateStep(bytecode.DecBegin, -1, nameKey, state, 0)
		} else {
			r := f.newReg()
			f.emit(bytecode.OpLoadK, r, f.constStr(name), 0)
			f.decorateStep(bytecode.DecBegin, -1, r, state, 0)
			f.free(r)
		}
		for _, d := range n.Decorators {
			f.decorator(state, d)
		}
		f.decorateStep(bytecode.DecClass, state, -1, -1, 0)
	}

	f.classDepth++
	defer func() {
		f.classDepth--
	}()
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
		if f.scope.lookup("#"+p.Name) != nil {
			continue
		}
		b := f.declare("#"+p.Name, bindPseudo)
		isMethod := int32(0)
		if m.Kind == ast.MemberMethod {
			isMethod = 1
		}
		f.emit(bytecode.OpPrivateName, b.reg, f.key(p.Name), isMethod)
	}
	// The key of a field whose name is known only at run time is a variable of the class
	// scope, so that its initializer can name an anonymous function after it.
	for i := range n.Members {
		m := &n.Members[i]
		if m.Kind != ast.MemberField && m.Kind != ast.MemberAccessor || m.Value == nil || !isAnonymousFunction(m.Value.ExprBody) {
			continue
		}
		if _, ok := keyName(m.Key, m.Computed); ok {
			continue
		}
		if _, private := m.Key.(*ast.PrivateName); private {
			continue
		}
		v := "%fieldkey" + strconv.Itoa(i)
		f.declare(v, bindPseudo)
		if f.c.fieldKeys == nil {
			f.c.fieldKeys = map[*ast.Function]string{}
		}
		f.c.fieldKeys[m.Value] = v
	}

	parent := int32(-1)
	if n.Super != nil {
		parent = f.newReg()
		f.expr(n.Super, parent)
	}

	// ctor holds the constructor and ctor+1 the prototype object.
	ctor := f.newRegs(2)
	var code *bytecode.Func
	if n.Constructor != nil {
		code = f.c.compileFunction(f, n.Constructor, name)
	} else {
		code = f.c.defaultConstructor(name, n.Super != nil)
	}
	// The source text of a class constructor is the whole class.
	code.Source = f.c.prog.Text(n)
	f.f.Protos = append(f.f.Protos, code)
	f.emit(bytecode.OpClosure, ctor, int32(len(f.f.Protos)-1), 0)
	f.captureScopes(ctor)
	f.emit(bytecode.OpClass, ctor, 0, parent)
	if nameKey >= 0 {
		f.emit(bytecode.OpSetFuncName, ctor, nameKey, bytecode.NamePrefixNone)
	}
	proto := ctor + 1

	if state >= 0 {
		f.decoratedMembers(n, state, ctor)
		f.decorateStep(bytecode.DecApply, state, ctor, ctor, 0)
		if inner != nil {
			f.emit(bytecode.OpMove, inner.reg, ctor, 0)
			inner.tdz = false
			inner.initialized = true
		}
		f.decorateStep(bytecode.DecFinish, state, ctor, -1, 0)
		f.emit(bytecode.OpMove, dst, ctor, 0)
		f.control = f.control[:len(f.control)-1]
		f.popScope()
		return
	}

	// The static fields and blocks run after everything else is defined.
	type static struct {
		key, value int32
		elem       int32
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
			f.emit(bytecode.OpExtra, elem, 0, 0)
			f.free(mm)
		case ast.MemberField:
			// The key and the initializer stay in registers until the static fields run.
			k := f.propertyKey(m.Key, m.Computed)
			elem := int32(bytecode.ElemField)
			v, ok := f.fieldValue(m)
			if ok {
				elem |= bytecode.ElemValue
			} else {
				v = f.fieldInit(m, obj, k, private)
			}
			if m.Static {
				statics = append(statics, static{
					key:   k,
					value: v,
					elem:  elem,
				})
				break
			}
			f.emit(bytecode.OpAddField, ctor, k, v)
			f.emit(bytecode.OpExtra, elem, 0, 0)
		case ast.MemberAccessor:
			mm := f.top
			k := f.propertyKey(m.Key, m.Computed)
			acc := f.newRegs(3)
			f.emit(bytecode.OpAutoAccessor, acc, k, 0)
			fieldElem := int32(bytecode.ElemField)
			v := f.fieldInit(m, obj, k, private)
			if v < 0 {
				v = f.newReg()
				f.emit(bytecode.OpLoadUndef, v, 0, 0)
				fieldElem |= bytecode.ElemValue
			}
			if private {
				elem := int32(0)
				if m.Static {
					elem = bytecode.ElemStatic
				}
				f.emit(bytecode.OpAddField, ctor, k, acc+1)
				f.emit(bytecode.OpExtra, bytecode.ElemGetter|elem, 0, 0)
				f.emit(bytecode.OpAddField, ctor, k, acc+2)
				f.emit(bytecode.OpExtra, bytecode.ElemSetter|elem, 0, 0)
			} else {
				f.emit(bytecode.OpDefineGetter, obj, k, acc+1)
				f.emit(bytecode.OpExtra, 0, 0, 0)
				f.emit(bytecode.OpDefineSetter, obj, k, acc+2)
				f.emit(bytecode.OpExtra, 0, 0, 0)
			}
			if m.Static {
				// The backing storage and the initializer stay in registers until the static fields run.
				statics = append(statics, static{
					key:   acc,
					value: v,
					elem:  fieldElem,
				})
				break
			}
			f.emit(bytecode.OpAddField, ctor, acc, v)
			f.emit(bytecode.OpExtra, fieldElem, 0, 0)
			f.free(mm)
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
		inner.initialized = true
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
		f.emit(bytecode.OpExtra, s.elem|bytecode.ElemStatic, 0, 0)
	}

	f.emit(bytecode.OpMove, dst, ctor, 0)
	f.control = f.control[:len(f.control)-1]
	f.popScope()
}

// fieldValue loads the initial value of the field m into a fresh register and returns the
// register when m has no initializer or one that is a literal, which evaluates to the same
// value every time without running any code. It reports false otherwise.
func (f *fn) fieldValue(m *ast.ClassMember) (int32, bool) {
	if m.Value == nil {
		v := f.newReg()
		f.emit(bytecode.OpLoadUndef, v, 0, 0)
		return v, true
	}
	switch e := m.Value.ExprBody.(type) {
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit:
	case *ast.Identifier:
		// The global undefined cannot be changed.
		if e.Name != "undefined" || f.resolve(e.Name).kind != refGlobal || f.dynDepth(e.Name) != 0 {
			return -1, false
		}
	default:
		return -1, false
	}
	v := f.newReg()
	f.expr(m.Value.ExprBody, v)
	return v, true
}

// fieldInit compiles the initializer of the field or auto-accessor m with the home object obj
// and the property key in the register k into a fresh register, and returns the register,
// or -1 when m has no initializer.
func (f *fn) fieldInit(m *ast.ClassMember, obj, k int32, private bool) int32 {
	if m.Value == nil {
		return -1
	}
	v := f.newReg()
	if fname, ok := keyName(m.Key, m.Computed); ok {
		f.c.nameField(m.Value, fname)
	} else if private {
		f.c.nameField(m.Value, "#"+m.Key.(*ast.PrivateName).Name)
	} else if name, ok := f.c.fieldKeys[m.Value]; ok {
		b, _ := f.lookupLocal(name)
		f.emit(bytecode.OpMove, b.reg, k, 0)
	}
	f.closure(m.Value, v, "")
	f.emit(bytecode.OpSetHome, v, obj, 0)
	return v
}

// decorateStep emits the [bytecode.OpDecorate] step of the Dec kind step with the
// operands a, b and c, and the Elem kind elem.
func (f *fn) decorateStep(step int32, a, b, c, elem int32) {
	f.emit(bytecode.OpDecorate, a, b, c)
	f.emit(bytecode.OpExtra, step, elem, 0)
}

// decorator evaluates the decorator e and adds it to the decorators of the next element of
// the class whose evaluation state is in the register state.
func (f *fn) decorator(state int32, e ast.Expr) {
	mark := f.top
	defer f.free(mark)
	f.setPos(e.Start())
	// The decorator goes to base and its receiver to base+1.
	base := f.newRegs(2)
	switch e := e.(type) {
	case *ast.MemberExpr:
		f.expr(e.Object, base+1)
		f.setPos(e.Property.Start())
		if p, ok := e.Property.(*ast.PrivateName); ok {
			name := f.newReg()
			f.loadVar("#"+p.Name, name, false)
			f.emit(bytecode.OpGetPrivate, base, base+1, name)
			break
		}
		f.emit(bytecode.OpGetProp, base, base+1, f.site(e.Property.(*ast.Identifier).Name))
	case *ast.Identifier:
		if scope := f.resolveName(e.Name); scope >= 0 {
			skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
			f.emit(f.getScopedOp(), base, scope, f.key(e.Name))
			if f.evalScopesAround() {
				f.emit(bytecode.OpScopeThis, base+1, scope, 0)
			} else {
				f.emit(bytecode.OpMove, base+1, scope, 0)
			}
			end := f.jump()
			f.patch(skip)
			f.loadStatic(e.Name, base, false)
			f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
			f.patch(end)
			break
		}
		f.expr(e, base)
		f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
	default:
		f.expr(e, base)
		f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
	}
	f.decorateStep(bytecode.DecAdd, state, base, base+1, 0)
}

// decoratedMembers compiles the members of the class n, which has decorators, into steps of
// the evaluation whose state is in the register state. ctor holds the constructor and ctor+1
// the prototype.
func (f *fn) decoratedMembers(n *ast.Class, state, ctor int32) {
	proto := ctor + 1
	for i := range n.Members {
		m := &n.Members[i]
		mark := f.top
		for _, d := range m.Decorators {
			f.decorator(state, d)
		}
		obj := proto
		elem := int32(0)
		if m.Static {
			obj = ctor
			elem = bytecode.ElemStatic
		}
		_, private := m.Key.(*ast.PrivateName)
		private = private && !m.Computed
		k, v := int32(-1), int32(-1)
		switch m.Kind {
		case ast.MemberConstructor:
			continue
		case ast.MemberMethod, ast.MemberGetter, ast.MemberSetter:
			k = f.propertyKey(m.Key, m.Computed)
			v = f.newReg()
			name, static := keyName(m.Key, m.Computed)
			if private {
				name, static = "#"+m.Key.(*ast.PrivateName).Name, true
			}
			prefix := int32(bytecode.NamePrefixNone)
			kind := int32(bytecode.ElemMethod)
			switch m.Kind {
			case ast.MemberGetter:
				prefix = bytecode.NamePrefixGet
				name = "get " + name
				kind = bytecode.ElemGetter
			case ast.MemberSetter:
				prefix = bytecode.NamePrefixSet
				name = "set " + name
				kind = bytecode.ElemSetter
			}
			elem |= kind
			f.closure(m.Value, v, name)
			if !static {
				f.emit(bytecode.OpSetFuncName, v, k, prefix)
			}
			f.emit(bytecode.OpSetHome, v, obj, 0)
		case ast.MemberField:
			k = f.propertyKey(m.Key, m.Computed)
			v = f.fieldInit(m, obj, k, private)
			elem |= bytecode.ElemField
		case ast.MemberAccessor:
			k = f.propertyKey(m.Key, m.Computed)
			v = f.fieldInit(m, obj, k, private)
			elem |= bytecode.ElemAccessor
		case ast.MemberStaticBlock:
			v = f.newReg()
			f.closure(m.Value, v, "")
			f.emit(bytecode.OpSetHome, v, ctor, 0)
			elem |= bytecode.ElemStaticBlock
		}
		f.decorateStep(bytecode.DecElement, state, k, v, elem)
		f.free(mark)
	}
}
