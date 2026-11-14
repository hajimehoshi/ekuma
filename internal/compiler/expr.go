// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

var binaryOps = map[ast.Op]bytecode.Opcode{
	ast.OpAdd:        bytecode.OpAdd,
	ast.OpSub:        bytecode.OpSub,
	ast.OpMul:        bytecode.OpMul,
	ast.OpDiv:        bytecode.OpDiv,
	ast.OpMod:        bytecode.OpMod,
	ast.OpExp:        bytecode.OpExp,
	ast.OpShl:        bytecode.OpShl,
	ast.OpShr:        bytecode.OpShr,
	ast.OpUShr:       bytecode.OpUShr,
	ast.OpBitAnd:     bytecode.OpBitAnd,
	ast.OpBitOr:      bytecode.OpBitOr,
	ast.OpBitXor:     bytecode.OpBitXor,
	ast.OpEq:         bytecode.OpEq,
	ast.OpNe:         bytecode.OpNe,
	ast.OpStrictEq:   bytecode.OpStrictEq,
	ast.OpStrictNe:   bytecode.OpStrictNe,
	ast.OpLt:         bytecode.OpLt,
	ast.OpLe:         bytecode.OpLe,
	ast.OpGt:         bytecode.OpGt,
	ast.OpGe:         bytecode.OpGe,
	ast.OpIn:         bytecode.OpIn,
	ast.OpInstanceof: bytecode.OpInstanceof,
}

// constOps are the operators that have a form with a constant right operand.
var constOps = map[ast.Op]bytecode.Opcode{
	ast.OpAdd:    bytecode.OpAddK,
	ast.OpSub:    bytecode.OpSubK,
	ast.OpMul:    bytecode.OpMulK,
	ast.OpDiv:    bytecode.OpDivK,
	ast.OpMod:    bytecode.OpModK,
	ast.OpBitOr:  bytecode.OpBitOrK,
	ast.OpBitAnd: bytecode.OpBitAndK,
	ast.OpShl:    bytecode.OpShlK,
	ast.OpShr:    bytecode.OpShrK,
	ast.OpUShr:   bytecode.OpUShrK,
}

// numberLit returns the value of e when it is a numeric literal, possibly negated.
func numberLit(e ast.Expr) (float64, bool) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return e.Value, true
	case *ast.UnaryExpr:
		if e.Op == ast.OpNeg {
			if n, ok := e.Arg.(*ast.NumberLit); ok {
				return -n.Value, true
			}
		}
	}
	return 0, false
}

// pure reports whether evaluating e cannot run any other code.
func pure(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit, *ast.Identifier, *ast.ThisExpr, *ast.BigIntLit:
		return true
	case *ast.UnaryExpr:
		return (e.Op == ast.OpNeg || e.Op == ast.OpNot) && pure(e.Arg)
	}
	return false
}

// singleWrite reports whether compiling e into a register writes that register only
// after everything e reads was read, so that the register may be one that e reads.
func singleWrite(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit, *ast.Identifier, *ast.ThisExpr,
		*ast.BigIntLit, *ast.BinaryExpr, *ast.CallExpr, *ast.NewExpr, *ast.Function, *ast.RegExpLit:
		return true
	case *ast.MemberExpr:
		return !e.Optional
	case *ast.UnaryExpr:
		return e.Op != ast.OpDelete
	}
	return false
}

// clobbers reports whether evaluating the expressions rest can change the local variable name.
func (f *fn) clobbers(name string, rest ...ast.Expr) bool {
	for _, e := range rest {
		if e == nil || pure(e) {
			continue
		}
		if f.mutated[name] || f.anyMutated {
			return true
		}
		found := false
		assignedNames(e, false, func(s string) {
			if s == name {
				found = true
			}
		})
		if found {
			return true
		}
	}
	return false
}

// operand compiles e and returns the register that holds its value until the expressions rest,
// which are evaluated afterwards, are done.
func (f *fn) operand(e ast.Expr, rest ...ast.Expr) int32 {
	if id, ok := e.(*ast.Identifier); ok {
		if r := f.localReg(id.Name); r >= 0 {
			if !f.clobbers(id.Name, rest...) {
				return r
			}
			t := f.newReg()
			f.emit(bytecode.OpMove, t, r, 0)
			return t
		}
	}
	t := f.newReg()
	f.expr(e, t)
	return t
}

// exprAny compiles e and returns the register that holds its value.
func (f *fn) exprAny(e ast.Expr) int32 {
	return f.operand(e)
}

// effect compiles e for its side effects only.
func (f *fn) effect(e ast.Expr) {
	mark := f.top
	switch e := e.(type) {
	case *ast.AssignExpr:
		f.assign(e, -1)
	case *ast.UpdateExpr:
		f.update(e, -1)
	case *ast.CallExpr:
		t := f.newReg()
		f.call(e, t)
	case *ast.SequenceExpr:
		for _, x := range e.Exprs {
			f.effect(x)
		}
	case *ast.Identifier:
		if f.localReg(e.Name) < 0 {
			f.expr(e, f.newReg())
		}
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit, *ast.Function:
	default:
		f.expr(e, f.newReg())
	}
	f.free(mark)
}

// expr compiles e and leaves its value in the register dst.
func (f *fn) expr(e ast.Expr, dst int32) {
	switch e := e.(type) {
	case *ast.NumberLit:
		f.number(e.Value, dst)
	case *ast.StringLit:
		f.emit(bytecode.OpLoadK, dst, f.constStr(e.Value), 0)
	case *ast.BigIntLit:
		f.emit(bytecode.OpLoadK, dst, f.constant(bytecode.Const{
			Kind: bytecode.ConstBigInt,
			Str:  e.Value,
		}), 0)
	case *ast.BoolLit:
		if e.Value {
			f.emit(bytecode.OpLoadTrue, dst, 0, 0)
		} else {
			f.emit(bytecode.OpLoadFalse, dst, 0, 0)
		}
	case *ast.NullLit:
		f.emit(bytecode.OpLoadNull, dst, 0, 0)
	case *ast.Identifier:
		if e.Name == "undefined" {
			if r := f.resolve(e.Name); r.kind == refGlobal && f.dynDepth(e.Name) == 0 {
				// The global undefined cannot be changed.
				f.emit(bytecode.OpLoadUndef, dst, 0, 0)
				return
			}
		}
		f.setRefPos(e, e.Name)
		f.loadVar(e.Name, dst, false)
	case *ast.ThisExpr:
		f.loadVar("this", dst, false)
	case *ast.NewTargetExpr:
		f.loadVar("new.target", dst, false)
	case *ast.RegExpLit:
		f.f.RegExps = append(f.f.RegExps, bytecode.RegExp{
			Pattern: e.Pattern,
			Flags:   e.Flags,
		})
		f.emit(bytecode.OpRegExp, dst, int32(len(f.f.RegExps)-1), 0)
	case *ast.TemplateLit:
		f.template(e, dst)
	case *ast.TaggedTemplate:
		f.taggedTemplate(e, dst)
	case *ast.ArrayLit:
		f.arrayLit(e, dst)
	case *ast.ObjectLit:
		f.objectLit(e, dst)
	case *ast.Function:
		name := ""
		if e.Name != nil {
			name = e.Name.Name
			if e != f.c.dynamic {
				f.c.isExpression[e] = true
			}
		}
		f.closure(e, dst, name)
	case *ast.Class:
		f.class(e, dst, "")
	case *ast.UnaryExpr:
		f.unary(e, dst)
	case *ast.UpdateExpr:
		f.update(e, dst)
	case *ast.BinaryExpr:
		f.binary(e, dst)
	case *ast.LogicalExpr:
		f.logical(e, dst)
	case *ast.PrivateInExpr:
		mark := f.top
		name := f.newReg()
		f.loadVar("#"+e.Name.Name, name, false)
		obj := f.exprAny(e.Right)
		f.emit(bytecode.OpHasPrivate, dst, name, obj)
		f.free(mark)
	case *ast.AssignExpr:
		f.assign(e, dst)
	case *ast.CondExpr:
		no := f.jumpIfFalse(e.Test)
		f.expr(e.Consequent, dst)
		end := f.jump()
		f.patchAll(no)
		f.expr(e.Alternate, dst)
		f.patch(end)
	case *ast.CallExpr:
		f.call(e, dst)
	case *ast.NewExpr:
		f.newExpr(e, dst)
	case *ast.MemberExpr:
		f.member(e, dst)
	case *ast.OptionalChain:
		f.optionalChain(e, dst)
	case *ast.SequenceExpr:
		for i, x := range e.Exprs {
			if i < len(e.Exprs)-1 {
				f.effect(x)
			} else {
				f.expr(x, dst)
			}
		}
	case *ast.YieldExpr:
		f.yield(e, dst)
	case *ast.AwaitExpr:
		mark := f.top
		v := f.exprAny(e.Arg)
		f.emit(bytecode.OpAwait, dst, v, 0)
		f.free(mark)
	case *ast.SuperExpr:
		f.c.fail(e, "'super' keyword unexpected here")
	case *ast.SpreadElement:
		f.c.fail(e, "unexpected spread element")
	case *ast.ImportCall:
		f.importCall(e, dst)
	case *ast.ImportMetaExpr:
		f.emit(bytecode.OpImport, dst, -1, 0)
	default:
		f.c.fail(e, "unsupported expression %T", e)
	}
}

func (f *fn) number(n float64, dst int32) {
	if i := int32(n); float64(i) == n && (i != 0 || !math.Signbit(n)) {
		f.emit(bytecode.OpLoadInt, dst, i, 0)
		return
	}
	f.emit(bytecode.OpLoadK, dst, f.constNum(n), 0)
}

func (f *fn) binary(n *ast.BinaryExpr, dst int32) {
	mark := f.top
	l := f.operand(n.Left, n.Right)
	if k, ok := numberLit(n.Right); ok {
		if op, ok := constOps[n.Op]; ok {
			f.emit(op, dst, l, f.constNum(k))
			f.free(mark)
			return
		}
	}
	r := f.exprAny(n.Right)
	f.emit(binaryOps[n.Op], dst, l, r)
	f.free(mark)
}

func (f *fn) logical(n *ast.LogicalExpr, dst int32) {
	f.expr(n.Left, dst)
	var skip int
	switch n.Op {
	case ast.OpAnd:
		skip = f.emit(bytecode.OpJumpIfNot, 0, dst, 0)
	case ast.OpOr:
		skip = f.emit(bytecode.OpJumpIf, 0, dst, 0)
	default:
		skip = f.emit(bytecode.OpJumpIfNotNullish, 0, dst, 0)
	}
	f.expr(n.Right, dst)
	f.patch(skip)
}

var compareJumps = map[ast.Op]bytecode.Opcode{
	ast.OpLt:       bytecode.OpJumpNotLt,
	ast.OpLe:       bytecode.OpJumpNotLe,
	ast.OpGt:       bytecode.OpJumpNotGt,
	ast.OpGe:       bytecode.OpJumpNotGe,
	ast.OpEq:       bytecode.OpJumpNotEq,
	ast.OpNe:       bytecode.OpJumpNotNe,
	ast.OpStrictEq: bytecode.OpJumpNotSEq,
	ast.OpStrictNe: bytecode.OpJumpNotSNe,
}

var compareConstJumps = map[ast.Op]bytecode.Opcode{
	ast.OpLt:       bytecode.OpJumpNotLtK,
	ast.OpLe:       bytecode.OpJumpNotLeK,
	ast.OpGt:       bytecode.OpJumpNotGtK,
	ast.OpGe:       bytecode.OpJumpNotGeK,
	ast.OpStrictEq: bytecode.OpJumpNotSEqK,
	ast.OpStrictNe: bytecode.OpJumpNotSNeK,
}

// jumpIfFalse compiles the condition e and returns the jumps that are taken when it is false.
func (f *fn) jumpIfFalse(e ast.Expr) []int {
	mark := f.top
	defer f.free(mark)
	switch n := e.(type) {
	case *ast.BinaryExpr:
		if op, ok := compareJumps[n.Op]; ok {
			l := f.operand(n.Left, n.Right)
			if k, isNum := numberLit(n.Right); isNum {
				if kop, ok := compareConstJumps[n.Op]; ok {
					return []int{f.emit(kop, 0, l, f.constNum(k))}
				}
			}
			r := f.exprAny(n.Right)
			return []int{f.emit(op, 0, l, r)}
		}
	case *ast.LogicalExpr:
		switch n.Op {
		case ast.OpAnd:
			return append(f.jumpIfFalse(n.Left), f.jumpIfFalse(n.Right)...)
		case ast.OpOr:
			yes := f.jumpIfTrue(n.Left)
			no := f.jumpIfFalse(n.Right)
			f.patchAll(yes)
			return no
		}
	case *ast.UnaryExpr:
		if n.Op == ast.OpNot {
			return f.jumpIfTrue(n.Arg)
		}
	case *ast.BoolLit:
		if n.Value {
			return nil
		}
		return []int{f.jump()}
	}
	r := f.exprAny(e)
	return []int{f.emit(bytecode.OpJumpIfNot, 0, r, 0)}
}

// jumpIfTrue compiles the condition e and returns the jumps that are taken when it is true.
func (f *fn) jumpIfTrue(e ast.Expr) []int {
	mark := f.top
	defer f.free(mark)
	switch n := e.(type) {
	case *ast.LogicalExpr:
		switch n.Op {
		case ast.OpOr:
			return append(f.jumpIfTrue(n.Left), f.jumpIfTrue(n.Right)...)
		case ast.OpAnd:
			no := f.jumpIfFalse(n.Left)
			yes := f.jumpIfTrue(n.Right)
			f.patchAll(no)
			return yes
		}
	case *ast.UnaryExpr:
		if n.Op == ast.OpNot {
			return f.jumpIfFalse(n.Arg)
		}
	case *ast.BinaryExpr:
		if _, ok := compareJumps[n.Op]; ok {
			// Jump over a jump: the fused instructions only exist for the false case.
			no := f.jumpIfFalse(e)
			yes := f.jump()
			f.patchAll(no)
			return []int{yes}
		}
	}
	r := f.exprAny(e)
	return []int{f.emit(bytecode.OpJumpIf, 0, r, 0)}
}

func (f *fn) unary(n *ast.UnaryExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	switch n.Op {
	case ast.OpTypeof:
		if id, ok := n.Arg.(*ast.Identifier); ok && f.localReg(id.Name) < 0 {
			if r := f.resolve(id.Name); r.kind == refGlobal {
				f.loadVar(id.Name, dst, true)
				f.emit(bytecode.OpTypeof, dst, dst, 0)
				return
			}
		}
		v := f.exprAny(n.Arg)
		f.emit(bytecode.OpTypeof, dst, v, 0)
	case ast.OpVoid:
		f.effect(n.Arg)
		f.emit(bytecode.OpLoadUndef, dst, 0, 0)
	case ast.OpDelete:
		f.delete(n, dst)
	case ast.OpNeg:
		if k, ok := numberLit(n); ok {
			f.number(k, dst)
			return
		}
		v := f.exprAny(n.Arg)
		f.emit(bytecode.OpNeg, dst, v, 0)
	case ast.OpPos:
		v := f.exprAny(n.Arg)
		f.emit(bytecode.OpPlus, dst, v, 0)
	case ast.OpNot:
		v := f.exprAny(n.Arg)
		f.emit(bytecode.OpNot, dst, v, 0)
	case ast.OpBitNot:
		v := f.exprAny(n.Arg)
		f.emit(bytecode.OpBitNot, dst, v, 0)
	default:
		f.c.fail(n, "unsupported unary operator %s", n.Op)
	}
}

func (f *fn) strictFlag() int32 {
	if f.f.Strict {
		return 1
	}
	return 0
}

func (f *fn) delete(n *ast.UnaryExpr, dst int32) {
	switch arg := n.Arg.(type) {
	case *ast.MemberExpr:
		if _, ok := arg.Object.(*ast.SuperExpr); ok {
			// Deleting a super property is a ReferenceError after this and the key are evaluated.
			f.loadVar("this", f.newReg(), false)
			if arg.Computed {
				f.effect(arg.Property)
			}
			f.emit(bytecode.OpThrowError, bytecode.ThrowReferenceError, f.constStr("unsupported reference to 'super'"), 0)
			return
		}
		obj := f.operand(arg.Object, arg.Property)
		key := f.newReg()
		if arg.Computed {
			f.expr(arg.Property, key)
		} else {
			f.emit(bytecode.OpLoadK, key, f.constStr(arg.Property.(*ast.Identifier).Name), 0)
		}
		f.setPos(arg.Property.Start())
		f.emit(bytecode.OpDeleteProp, dst, obj, key)
		f.emit(bytecode.OpNop, f.strictFlag(), 0, 0)
	case *ast.OptionalChain:
		// delete a?.b: the result is true when the chain short-circuits.
		f.chains = append(f.chains, nil)
		if m, ok := arg.Expr.(*ast.MemberExpr); ok {
			f.delete(&ast.UnaryExpr{
				Span: n.Span,
				Op:   ast.OpDelete,
				Arg:  m,
			}, dst)
		} else {
			f.effect(arg.Expr)
			f.emit(bytecode.OpLoadTrue, dst, 0, 0)
		}
		end := f.jump()
		f.patchAll(f.chains[len(f.chains)-1])
		f.chains = f.chains[:len(f.chains)-1]
		f.emit(bytecode.OpLoadTrue, dst, 0, 0)
		f.patch(end)
	case *ast.Identifier:
		end := -1
		if scope := f.resolveName(arg.Name); scope >= 0 {
			skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
			f.emit(bytecode.OpDeleteScoped, dst, scope, f.key(arg.Name))
			end = f.jump()
			f.patch(skip)
		}
		if r := f.resolve(arg.Name); r.kind != refGlobal {
			f.emit(bytecode.OpLoadFalse, dst, 0, 0)
		} else {
			f.emit(bytecode.OpDeleteName, dst, f.key(arg.Name), 0)
		}
		if end >= 0 {
			f.patch(end)
		}
	default:
		f.effect(n.Arg)
		f.emit(bytecode.OpLoadTrue, dst, 0, 0)
	}
}

// update compiles ++ and --. dst is -1 when the value is not needed.
func (f *fn) update(n *ast.UpdateExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	op := bytecode.OpInc
	if n.Op == ast.OpDec {
		op = bytecode.OpDec
	}
	switch arg := n.Arg.(type) {
	case *ast.Identifier:
		if r := f.localReg(arg.Name); r >= 0 {
			if b, _ := f.lookupLocal(arg.Name); b.kind == bindVar || b.kind == bindLet {
				switch {
				case dst < 0 || dst == r:
					f.emit(op, r, r, 0)
				case n.Prefix:
					f.emit(op, r, r, 0)
					f.emit(bytecode.OpMove, dst, r, 0)
				default:
					f.emit(bytecode.OpToNumeric, dst, r, 0)
					f.emit(op, r, dst, 0)
				}
				return
			}
		}
		scope := f.resolveName(arg.Name)
		old := f.newReg()
		f.loadVarFrom(arg.Name, old, false, scope)
		res := f.newReg()
		if n.Prefix || dst < 0 {
			f.emit(op, res, old, 0)
			f.storeVarTo(n, arg.Name, res, false, scope)
			if dst >= 0 {
				f.emit(bytecode.OpMove, dst, res, 0)
			}
			return
		}
		f.emit(bytecode.OpToNumeric, old, old, 0)
		f.emit(op, res, old, 0)
		f.storeVarTo(n, arg.Name, res, false, scope)
		f.emit(bytecode.OpMove, dst, old, 0)
	case *ast.MemberExpr:
		t := f.assignTarget(arg)
		old := f.newReg()
		f.loadTarget(t, old)
		res := f.newReg()
		if n.Prefix || dst < 0 {
			f.emit(op, res, old, 0)
			f.storeTarget(t, res)
			if dst >= 0 {
				f.emit(bytecode.OpMove, dst, res, 0)
			}
			return
		}
		f.emit(bytecode.OpToNumeric, old, old, 0)
		f.emit(op, res, old, 0)
		f.storeTarget(t, res)
		f.emit(bytecode.OpMove, dst, old, 0)
	case *ast.CallExpr:
		f.callTarget(arg)
	default:
		f.c.fail(n, "invalid update target")
	}
}

// callTarget evaluates the call n used as an assignment target and throws the ReferenceError
// for assigning to it.
func (f *fn) callTarget(n *ast.CallExpr) {
	f.effect(n)
	f.emit(bytecode.OpThrowError, bytecode.ThrowReferenceError, f.constStr("invalid assignment target"), 0)
}

// memberTarget is a property reference whose object and key are evaluated.
type memberTarget struct {
	node *ast.MemberExpr
	obj  int32

	// key is the register of a computed key or of a private name, and -1 for a named property.
	key int32

	// For a super property: the registers of this and of the home object,
	// or of the super base when superBase is set.
	super     bool
	superBase bool
	this      int32
	home      int32
}

// target evaluates the object and the key of the property reference n for a read.
// The key of a super property is converted by the read.
func (f *fn) target(n *ast.MemberExpr) memberTarget {
	t := memberTarget{
		node: n,
		key:  -1,
	}
	if _, ok := n.Object.(*ast.SuperExpr); ok {
		t.super = true
		t.this = f.newReg()
		f.loadVar("this", t.this, false)
		t.key = f.newReg()
		if n.Computed {
			f.expr(n.Property, t.key)
		} else {
			f.emit(bytecode.OpLoadK, t.key, f.constStr(n.Property.(*ast.Identifier).Name), 0)
		}
		t.home = f.newReg()
		f.loadVar("%home", t.home, false)
		return t
	}
	if p, ok := n.Property.(*ast.PrivateName); ok && !n.Computed {
		t.obj = f.operand(n.Object)
		t.key = f.newReg()
		f.loadVar("#"+p.Name, t.key, false)
		return t
	}
	if n.Computed {
		t.obj = f.operand(n.Object, n.Property)
		t.key = f.newReg()
		f.expr(n.Property, t.key)
		return t
	}
	t.obj = f.operand(n.Object)
	return t
}

// assignTarget evaluates the property reference n for a store, which may follow a read.
func (f *fn) assignTarget(n *ast.MemberExpr) memberTarget {
	t := f.target(n)
	if t.super {
		// The reference holds the super base from its evaluation on.
		f.emit(bytecode.OpSuperBase, t.home, t.home, 0)
		t.superBase = true
	}
	return t
}

// loadTarget reads the property t into dst.
func (f *fn) loadTarget(t memberTarget, dst int32) {
	n := t.node
	f.setPos(n.Property.Start())
	switch {
	case t.super:
		var superBase int32
		if t.superBase {
			superBase = 1
		}
		f.emit(bytecode.OpGetSuper, dst, t.key, t.this)
		f.emit(bytecode.OpNop, t.home, superBase, 0)
	case t.key < 0:
		name := n.Property.(*ast.Identifier).Name
		f.emit(bytecode.OpGetProp, dst, t.obj, f.site(name))
	case !n.Computed:
		f.emit(bytecode.OpGetPrivate, dst, t.obj, t.key)
	default:
		// A compound assignment converts the key once, before the read and after the base is checked.
		f.emit(bytecode.OpToPropertyKey, t.key, t.key, t.obj+1)
		f.emit(bytecode.OpGetIndex, dst, t.obj, t.key)
	}
}

// storeTarget stores the register src to the property t.
func (f *fn) storeTarget(t memberTarget, src int32) {
	n := t.node
	f.setPos(n.Property.Start())
	switch {
	case t.super:
		if !t.superBase {
			panic("compiler: store to a super property without its super base")
		}
		f.emit(bytecode.OpSetSuper, t.key, src, t.this)
		f.emit(bytecode.OpNop, f.strictFlag(), t.home, 0)
	case t.key < 0:
		name := n.Property.(*ast.Identifier).Name
		op := bytecode.OpSetProp
		if f.f.Strict {
			op = bytecode.OpSetPropStrict
		}
		f.emit(op, t.obj, f.site(name), src)
	case !n.Computed:
		f.emit(bytecode.OpSetPrivate, t.obj, t.key, src)
	default:
		op := bytecode.OpSetIndex
		if f.f.Strict {
			op = bytecode.OpSetIndexStrict
		}
		f.emit(op, t.obj, t.key, src)
	}
}

// member compiles the property read n.
func (f *fn) member(n *ast.MemberExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	if _, ok := n.Object.(*ast.SuperExpr); ok {
		t := f.target(n)
		f.loadTarget(t, dst)
		return
	}
	if p, ok := n.Property.(*ast.PrivateName); ok && !n.Computed {
		obj := f.operand(n.Object)
		f.chainCheck(n.Optional, obj)
		name := f.newReg()
		f.loadVar("#"+p.Name, name, false)
		f.setPos(n.Property.Start())
		f.emit(bytecode.OpGetPrivate, dst, obj, name)
		return
	}
	if !n.Computed {
		obj := f.operand(n.Object)
		f.chainCheck(n.Optional, obj)
		name := n.Property.(*ast.Identifier).Name
		f.setPos(n.Property.Start())
		if name == "length" {
			f.emit(bytecode.OpGetLength, dst, obj, f.site(name))
			return
		}
		f.emit(bytecode.OpGetProp, dst, obj, f.site(name))
		return
	}
	obj := f.operand(n.Object, n.Property)
	f.chainCheck(n.Optional, obj)
	if k, ok := n.Property.(*ast.NumberLit); ok {
		if i := int32(k.Value); float64(i) == k.Value && i >= 0 {
			f.setPos(n.Property.Start())
			f.emit(bytecode.OpGetIndexInt, dst, obj, i)
			return
		}
	}
	if s, ok := n.Property.(*ast.StringLit); ok && !isIndexString(s.Value) {
		f.setPos(n.Property.Start())
		f.emit(bytecode.OpGetProp, dst, obj, f.site(s.Value))
		return
	}
	key := f.exprAny(n.Property)
	f.setPos(n.Property.Start())
	f.emit(bytecode.OpGetIndex, dst, obj, key)
}

// isIndexString reports whether s may be the canonical form of an array index.
func isIndexString(s string) bool {
	return len(s) > 0 && s[0] >= '0' && s[0] <= '9'
}

// chainCheck emits the short circuit of an optional chain for the value in reg.
func (f *fn) chainCheck(optional bool, reg int32) {
	if !optional {
		return
	}
	if len(f.chains) == 0 {
		panic("compiler: optional access outside of an optional chain")
	}
	j := f.emit(bytecode.OpJumpIfNullish, 0, reg, 0)
	f.chains[len(f.chains)-1] = append(f.chains[len(f.chains)-1], j)
}

func (f *fn) optionalChain(n *ast.OptionalChain, dst int32) {
	f.chains = append(f.chains, nil)
	f.expr(n.Expr, dst)
	end := f.jump()
	f.patchAll(f.chains[len(f.chains)-1])
	f.chains = f.chains[:len(f.chains)-1]
	f.emit(bytecode.OpLoadUndef, dst, 0, 0)
	f.patch(end)
}

// hasSpread reports whether one of the arguments is a spread element.
func hasSpread(args []ast.Expr) bool {
	for _, a := range args {
		if _, ok := a.(*ast.SpreadElement); ok {
			return true
		}
	}
	return false
}

// spreadArgs builds the array of the arguments args in the register dst.
func (f *fn) spreadArgs(args []ast.Expr, dst int32) {
	f.emit(bytecode.OpNewArray, dst, 0, int32(len(args)))
	for _, a := range args {
		mark := f.top
		if s, ok := a.(*ast.SpreadElement); ok {
			v := f.exprAny(s.Arg)
			f.emit(bytecode.OpArraySpread, dst, v, 0)
		} else {
			v := f.exprAny(a)
			f.emit(bytecode.OpArrayPush, dst, v, 0)
		}
		f.free(mark)
	}
}

// isArguments reports whether e is a reference to the arguments of the function f itself,
// which f only forwards.
func (f *fn) isForwardedArguments(e ast.Expr) bool {
	id, ok := e.(*ast.Identifier)
	return ok && id.Name == "arguments" && f.forwardsArgs
}

// call compiles the call n.
func (f *fn) call(n *ast.CallExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	f.setPos(n.Start())

	if _, ok := n.Callee.(*ast.SuperExpr); ok {
		f.superCall(n, dst)
		return
	}

	// The callee goes to base, the this value to base+1 and the arguments follow.
	base := f.newRegs(2)
	op := bytecode.OpCall
	// pos is the position of the call: the property name of a method call, the name of
	// the function, or the arguments after any other callee.
	pos := n.Start()
	callee := n.Callee
	switch c := callee.(type) {
	case *ast.MemberExpr:
		if _, ok := c.Object.(*ast.SuperExpr); ok {
			t := f.target(c)
			f.loadTarget(t, base)
			f.emit(bytecode.OpMove, base+1, t.this, 0)
			f.free(base + 2)
			break
		}
		pos = c.Property.Start()
		f.expr(c.Object, base+1)
		f.chainCheck(c.Optional, base+1)
		switch {
		case !c.Computed:
			if p, ok := c.Property.(*ast.PrivateName); ok {
				name := f.newReg()
				f.loadVar("#"+p.Name, name, false)
				f.setPos(pos)
				f.emit(bytecode.OpGetPrivate, base, base+1, name)
				f.free(name)
				break
			}
			name := c.Property.(*ast.Identifier).Name
			f.setPos(pos)
			f.emit(bytecode.OpGetProp, base, base+1, f.site(name))
			switch name {
			case "call":
				if !hasSpread(n.Args) {
					op = bytecode.OpCallCall
				}
			case "apply":
				if len(n.Args) == 2 && f.isForwardedArguments(n.Args[1]) {
					f.expr(n.Args[0], f.newReg())
					// One more register for the arguments object of the general case.
					f.newReg()
					f.setPos(pos)
					f.emit(bytecode.OpApplyFwd, base, mappedArguments(f.node), 0)
					f.result(dst, base)
					return
				}
			}
		default:
			key := f.exprAny(c.Property)
			f.setPos(pos)
			f.emit(bytecode.OpGetIndex, base, base+1, key)
			f.free(base + 2)
		}
	case *ast.Identifier:
		mark := f.top
		if scope := f.resolveName(c.Name); scope >= 0 {
			skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
			f.emit(bytecode.OpGetScoped, base, scope, f.key(c.Name))
			if f.evalScopesAround() {
				f.emit(bytecode.OpScopeThis, base+1, scope, 0)
			} else {
				f.emit(bytecode.OpMove, base+1, scope, 0)
			}
			end := f.jump()
			f.patch(skip)
			f.loadStatic(c.Name, base, false)
			f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
			f.patch(end)
			f.free(mark)
		} else {
			f.expr(callee, base)
			f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
		}
		if c.Name == "eval" && !n.Optional {
			op = bytecode.OpCallEval
		}
	default:
		pos = callee.End()
		f.expr(callee, base)
		f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
	}
	f.chainCheck(n.Optional, base)

	if len(n.Args) == 1 && op != bytecode.OpCallEval {
		if s, ok := n.Args[0].(*ast.SpreadElement); ok && f.isForwardedArguments(s.Arg) {
			f.setPos(pos)
			f.emit(bytecode.OpCallFwd, base, 0, 0)
			f.result(dst, base)
			return
		}
	}
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(pos)
		if op == bytecode.OpCallEval {
			f.emit(bytecode.OpCallEval, base, -1, f.evalSite())
		}
		f.emit(bytecode.OpCallSpread, base, 0, 0)
		f.result(dst, base)
		return
	}
	for _, a := range n.Args {
		f.expr(a, f.newReg())
	}
	site := int32(0)
	if op == bytecode.OpCallEval {
		site = f.evalSite()
	}
	f.setPos(pos)
	f.emit(op, base, int32(len(n.Args)), site)
	f.result(dst, base)
}

// result moves the result of a call from the register src to dst.
func (f *fn) result(dst, src int32) {
	if dst != src {
		f.emit(bytecode.OpMove, dst, src, 0)
	}
}

func (f *fn) superCall(n *ast.CallExpr, dst int32) {
	base := f.newRegs(2)
	f.loadVar("%callee", base, false)
	f.loadVar("new.target", base+1, false)
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(n.Start())
		f.emit(bytecode.OpSuperSpread, base, 0, 0)
	} else {
		for _, a := range n.Args {
			f.expr(a, f.newReg())
		}
		f.setPos(n.Start())
		f.emit(bytecode.OpSuperCall, base, int32(len(n.Args)), 0)
	}
	// The result becomes this, which must not be initialized yet.
	r := f.resolve("this")
	t := f.newReg()
	switch r.kind {
	case refLocal:
		f.emit(bytecode.OpMove, t, r.b.reg, 0)
	case refUpval:
		f.emit(bytecode.OpGetUpval, t, r.index, 0)
	}
	f.emit(bytecode.OpCheckThisUninit, t, 0, 0)
	switch r.kind {
	case refLocal:
		f.emit(bytecode.OpMove, r.b.reg, base, 0)
	case refUpval:
		f.emit(bytecode.OpSetUpval, base, r.index, 0)
	}
	callee := f.newReg()
	f.loadVar("%callee", callee, false)
	f.emit(bytecode.OpInitFields, callee, base, 0)
	f.result(dst, base)
}

func (f *fn) newExpr(n *ast.NewExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	f.setPos(n.Start())
	base := f.newRegs(2)
	f.expr(n.Callee, base)
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(n.Start())
		f.emit(bytecode.OpNewSpread, base, 0, 0)
		f.result(dst, base)
		return
	}
	for _, a := range n.Args {
		f.expr(a, f.newReg())
	}
	f.setPos(n.Start())
	f.emit(bytecode.OpNew, base, int32(len(n.Args)), f.site("prototype"))
	f.result(dst, base)
}

func (f *fn) template(n *ast.TemplateLit, dst int32) {
	mark := f.top
	defer f.free(mark)
	acc := f.newReg()
	f.emit(bytecode.OpLoadK, acc, f.constStr(n.Quasis[0].Cooked), 0)
	for i, e := range n.Exprs {
		t := f.newReg()
		f.expr(e, t)
		f.emit(bytecode.OpToString, t, t, 0)
		f.emit(bytecode.OpAdd, acc, acc, t)
		if s := n.Quasis[i+1].Cooked; s != "" {
			f.emit(bytecode.OpLoadK, t, f.constStr(s), 0)
			f.emit(bytecode.OpAdd, acc, acc, t)
		}
		f.free(t)
	}
	f.emit(bytecode.OpMove, dst, acc, 0)
}

func (f *fn) taggedTemplate(n *ast.TaggedTemplate, dst int32) {
	mark := f.top
	defer f.free(mark)
	base := f.newRegs(2)
	switch c := n.Tag.(type) {
	case *ast.MemberExpr:
		if _, ok := c.Object.(*ast.SuperExpr); ok {
			t := f.target(c)
			f.loadTarget(t, base)
			f.emit(bytecode.OpMove, base+1, t.this, 0)
			f.free(base + 2)
			break
		}
		t := f.target(c)
		f.loadTarget(t, base)
		f.emit(bytecode.OpMove, base+1, t.obj, 0)
		f.free(base + 2)
	default:
		f.expr(n.Tag, base)
		f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
	}
	var site bytecode.Template
	for _, q := range n.Quasi.Quasis {
		site.Cooked = append(site.Cooked, q.Cooked)
		site.HasCooked = append(site.HasCooked, q.HasCooked)
		site.Raw = append(site.Raw, q.Raw)
	}
	f.f.Templates = append(f.f.Templates, site)
	f.emit(bytecode.OpTemplate, f.newReg(), int32(len(f.f.Templates)-1), 0)
	for _, e := range n.Quasi.Exprs {
		f.expr(e, f.newReg())
	}
	f.emit(bytecode.OpCall, base, int32(len(n.Quasi.Exprs)+1), 0)
	f.result(dst, base)
}

func (f *fn) arrayLit(n *ast.ArrayLit, dst int32) {
	mark := f.top
	defer f.free(mark)
	arr := f.newReg()
	f.emit(bytecode.OpNewArray, arr, 0, int32(len(n.Elements)))
	for _, e := range n.Elements {
		m := f.top
		switch e := e.(type) {
		case nil:
			f.emit(bytecode.OpArrayHole, arr, 0, 0)
		case *ast.SpreadElement:
			v := f.exprAny(e.Arg)
			f.emit(bytecode.OpArraySpread, arr, v, 0)
		default:
			v := f.exprAny(e)
			f.emit(bytecode.OpArrayPush, arr, v, 0)
		}
		f.free(m)
	}
	f.emit(bytecode.OpMove, dst, arr, 0)
}

// keyName returns the name of a property key that is known at compile time.
func keyName(key ast.Expr, computed bool) (string, bool) {
	if computed {
		return "", false
	}
	switch k := key.(type) {
	case *ast.Identifier:
		return k.Name, true
	case *ast.StringLit:
		return k.Value, true
	}
	return "", false
}

// isAnonymousFunction reports whether e is a function or class expression without a name.
func isAnonymousFunction(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Function:
		return e.Name == nil
	case *ast.Class:
		return e.Name == nil
	}
	return false
}

// named compiles e into dst. When e is an anonymous function or class, it gets the name name.
func (f *fn) named(e ast.Expr, dst int32, name string) {
	switch e := e.(type) {
	case *ast.Function:
		if e.Name == nil {
			f.closure(e, dst, name)
			return
		}
	case *ast.Class:
		if e.Name == nil {
			f.class(e, dst, name)
			return
		}
	}
	f.expr(e, dst)
}

// propertyKey compiles the key of a property definition into a fresh register.
func (f *fn) propertyKey(key ast.Expr, computed bool) int32 {
	k := f.newReg()
	if computed {
		f.expr(key, k)
		f.emit(bytecode.OpToPropertyKey, k, k, 0)
		return k
	}
	switch key := key.(type) {
	case *ast.Identifier:
		f.emit(bytecode.OpLoadK, k, f.constStr(key.Name), 0)
	case *ast.PrivateName:
		f.loadVar("#"+key.Name, k, false)
	default:
		f.expr(key, k)
		f.emit(bytecode.OpToPropertyKey, k, k, 0)
	}
	return k
}

func (f *fn) objectLit(n *ast.ObjectLit, dst int32) {
	mark := f.top
	defer f.free(mark)
	obj := f.newReg()
	f.emit(bytecode.OpNewObject, obj, 0, int32(namedProperties(n)))
	for i := range n.Properties {
		p := &n.Properties[i]
		m := f.top
		switch p.Kind {
		case ast.PropertySpread:
			v := f.exprAny(p.Value)
			f.emit(bytecode.OpCopyProps, obj, v, 0)
		case ast.PropertyInit:
			name, static := keyName(p.Key, p.Computed)
			switch {
			case static && name == "__proto__" && !p.Shorthand:
				v := f.exprAny(p.Value)
				f.emit(bytecode.OpSetProto, obj, v, 0)
			case static && !isIndexString(name):
				v := f.newReg()
				f.named(p.Value, v, name)
				f.emit(bytecode.OpDefineFieldK, obj, f.site(name), v)
			default:
				k := f.propertyKey(p.Key, p.Computed)
				v := f.newReg()
				f.expr(p.Value, v)
				if isAnonymousFunction(p.Value) {
					f.emit(bytecode.OpSetFuncName, v, k, bytecode.NamePrefixNone)
				}
				f.emit(bytecode.OpDefineField, obj, k, v)
			}
		case ast.PropertyMethod, ast.PropertyGet, ast.PropertySet:
			f.method(obj, p.Key, p.Computed, p.Value.(*ast.Function), p.Kind, true)
		}
		f.free(m)
	}
	f.emit(bytecode.OpMove, dst, obj, 0)
}

// namedProperties returns the number of properties of n that have a name known at compile time.
func namedProperties(n *ast.ObjectLit) int {
	var c int
	for i := range n.Properties {
		p := &n.Properties[i]
		if p.Kind == ast.PropertySpread {
			continue
		}
		if name, static := keyName(p.Key, p.Computed); static && name != "__proto__" && !isIndexString(name) {
			c++
		}
	}
	return c
}

// method defines the method, getter or setter fnNode on the object in the register obj.
func (f *fn) method(obj int32, key ast.Expr, computed bool, fnNode *ast.Function, kind ast.PropertyKind, enumerable bool) {
	k := f.propertyKey(key, computed)
	v := f.newReg()
	name, static := keyName(key, computed)
	if p, ok := key.(*ast.PrivateName); ok && !computed {
		name, static = "#"+p.Name, true
	}
	prefix := int32(bytecode.NamePrefixNone)
	switch kind {
	case ast.PropertyGet:
		prefix = bytecode.NamePrefixGet
		name = "get " + name
	case ast.PropertySet:
		prefix = bytecode.NamePrefixSet
		name = "set " + name
	}
	f.closure(fnNode, v, name)
	if !static {
		f.emit(bytecode.OpSetFuncName, v, k, prefix)
	}
	flag := int32(0)
	if enumerable {
		flag = 1
	}
	switch kind {
	case ast.PropertyGet:
		f.emit(bytecode.OpDefineGetter, obj, k, v)
		f.emit(bytecode.OpNop, flag, 0, 0)
	case ast.PropertySet:
		f.emit(bytecode.OpDefineSetter, obj, k, v)
		f.emit(bytecode.OpNop, flag, 0, 0)
	default:
		if enumerable {
			f.emit(bytecode.OpSetHome, v, obj, 0)
			f.emit(bytecode.OpDefineField, obj, k, v)
		} else {
			f.emit(bytecode.OpDefineMethod, obj, k, v)
		}
	}
}

// compoundOp returns the instruction of a compound assignment operator.
func compoundOp(op ast.Op) bytecode.Opcode {
	return binaryOps[op]
}

// assign compiles the assignment n. dst is -1 when the value is not needed.
func (f *fn) assign(n *ast.AssignExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	logical := n.Op == ast.OpAnd || n.Op == ast.OpOr || n.Op == ast.OpCoalesce

	shortCircuit := func(reg int32) int {
		switch n.Op {
		case ast.OpAnd:
			return f.emit(bytecode.OpJumpIfNot, 0, reg, 0)
		case ast.OpOr:
			return f.emit(bytecode.OpJumpIf, 0, reg, 0)
		}
		return f.emit(bytecode.OpJumpIfNotNullish, 0, reg, 0)
	}

	switch target := n.Target.(type) {
	case *ast.Identifier:
		name := target.Name
		inferred := ""
		if !target.Parenthesized {
			inferred = name
		}
		local := f.localReg(name)
		if local >= 0 {
			if b, _ := f.lookupLocal(name); b.kind != bindVar && b.kind != bindLet {
				local = -1
			}
		}
		switch {
		case n.Op == ast.OpNone:
			if local >= 0 {
				if singleWrite(n.Value) {
					f.named(n.Value, local, inferred)
				} else {
					t := f.newReg()
					f.named(n.Value, t, inferred)
					f.emit(bytecode.OpMove, local, t, 0)
				}
				if dst >= 0 && dst != local {
					f.emit(bytecode.OpMove, dst, local, 0)
				}
				return
			}
			scope := f.resolveName(name)
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.named(n.Value, t, inferred)
			f.storeVarTo(n, name, t, false, scope)
		case logical:
			scope := f.resolveName(name)
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.loadVarFrom(name, t, false, scope)
			skip := shortCircuit(t)
			f.named(n.Value, t, inferred)
			f.storeVarTo(n, name, t, false, scope)
			f.patch(skip)
		default:
			if local >= 0 && !f.clobbers(name, n.Value) {
				if k, ok := numberLit(n.Value); ok {
					if op, ok := constOps[n.Op]; ok {
						f.emit(op, local, local, f.constNum(k))
						if dst >= 0 && dst != local {
							f.emit(bytecode.OpMove, dst, local, 0)
						}
						return
					}
				}
				r := f.exprAny(n.Value)
				f.emit(compoundOp(n.Op), local, local, r)
				if dst >= 0 && dst != local {
					f.emit(bytecode.OpMove, dst, local, 0)
				}
				return
			}
			scope := f.resolveName(name)
			old := f.newReg()
			f.loadVarFrom(name, old, false, scope)
			r := f.exprAny(n.Value)
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.emit(compoundOp(n.Op), t, old, r)
			f.storeVarTo(n, name, t, false, scope)
		}
	case *ast.MemberExpr:
		t := f.assignTarget(target)
		switch {
		case n.Op == ast.OpNone:
			v := f.exprAny(n.Value)
			f.storeTarget(t, v)
			if dst >= 0 {
				f.result(dst, v)
			}
		case logical:
			v := f.newReg()
			f.loadTarget(t, v)
			skip := shortCircuit(v)
			f.expr(n.Value, v)
			f.storeTarget(t, v)
			f.patch(skip)
			if dst >= 0 {
				f.emit(bytecode.OpMove, dst, v, 0)
			}
		default:
			old := f.newReg()
			f.loadTarget(t, old)
			r := f.exprAny(n.Value)
			v := f.newReg()
			f.emit(compoundOp(n.Op), v, old, r)
			f.storeTarget(t, v)
			if dst >= 0 {
				f.emit(bytecode.OpMove, dst, v, 0)
			}
		}
	case *ast.CallExpr:
		f.callTarget(target)
	default:
		v := f.exprAny(n.Value)
		f.destructure(n.Target, v, patAssign)
		if dst >= 0 {
			f.result(dst, v)
		}
	}
}

func (f *fn) yield(n *ast.YieldExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	if n.Delegate {
		if f.f.IsAsync {
			f.asyncYieldDelegate(n, dst)
			return
		}
		f.yieldDelegate(n, dst)
		return
	}
	v := f.newReg()
	if n.Arg != nil {
		f.expr(n.Arg, v)
	} else {
		f.emit(bytecode.OpLoadUndef, v, 0, 0)
	}
	if f.f.IsAsync {
		f.emit(bytecode.OpAwait, v, v, 0)
	}
	f.emit(bytecode.OpYield, dst, v, 0)
}

// yieldDelegate compiles yield*.
func (f *fn) yieldDelegate(n *ast.YieldExpr, dst int32) {
	// it and it+1 hold the iterator and its next method.
	it := f.newRegs(2)
	src := f.exprAny(n.Arg)
	f.emit(bytecode.OpGetIterator, it, src, bytecode.IterSyncNext)

	// recv holds the value that was sent in, and recv+1 whether the called method exists.
	recv := f.newRegs(2)
	mode := f.newReg()
	res := f.newReg()
	f.emit(bytecode.OpLoadUndef, recv, 0, 0)
	f.emit(bytecode.OpLoadInt, mode, bytecode.ResumeNext, 0)

	loop := f.pc()
	onThrow := f.emit(bytecode.OpJumpIfInt, 0, mode, bytecode.ResumeThrow)
	onReturn := f.emit(bytecode.OpJumpIfInt, 0, mode, bytecode.ResumeReturn)

	// next: res = next.call(it, recv)
	callBase := f.newRegs(3)
	f.emit(bytecode.OpMove, callBase, it+1, 0)
	f.emit(bytecode.OpMove, callBase+1, it, 0)
	f.emit(bytecode.OpMove, callBase+2, recv, 0)
	f.emit(bytecode.OpCall, callBase, 1, 0)
	f.emit(bytecode.OpMove, res, callBase, 0)
	f.free(callBase)
	step := f.jump()

	// throw: res = it.throw(recv), or close the iterator and fail when there is no throw method.
	f.patch(onThrow)
	f.emit(bytecode.OpIterCall, recv, it, f.key("throw"))
	hasThrow := f.emit(bytecode.OpJumpIf, 0, recv+1, 0)
	f.emit(bytecode.OpIterClose, it, 0, 0)
	f.emit(bytecode.OpThrowError, bytecode.ThrowTypeError, f.constStr("the iterator does not provide a 'throw' method"), 0)
	f.patch(hasThrow)
	f.emit(bytecode.OpMove, res, recv, 0)
	step2 := f.jump()

	// return: res = it.return(recv), or return recv when there is no return method.
	f.patch(onReturn)
	ret := f.newReg()
	f.emit(bytecode.OpMove, ret, recv, 0)
	f.emit(bytecode.OpIterCall, recv, it, f.key("return"))
	hasReturn := f.emit(bytecode.OpJumpIf, 0, recv+1, 0)
	f.exit(exitReturn, nil, ret)
	f.patch(hasReturn)
	f.emit(bytecode.OpMove, res, recv, 0)
	// A finished inner iterator makes the generator return its value.
	notDone := f.emit(bytecode.OpYieldDelegateResult, ret, res, 0)
	f.exit(exitReturn, nil, ret)
	f.free(ret)

	f.patch(step)
	f.patch(step2)
	done := f.emit(bytecode.OpYieldDelegateResult, dst, res, 0)
	end := f.jump()
	f.patchC(done)
	f.patchC(notDone)
	f.emit(bytecode.OpYield, recv, res, 1)
	f.emit(bytecode.OpGenResumeMode, mode, 0, 0)
	f.jumpTo(loop)
	f.patch(end)
}

// asyncYieldDelegate compiles yield* in an async generator.
func (f *fn) asyncYieldDelegate(n *ast.YieldExpr, dst int32) {
	// it and it+1 hold the iterator and its next method.
	it := f.newRegs(2)
	src := f.exprAny(n.Arg)
	f.emit(bytecode.OpGetIterator, it, src, bytecode.IterAsync)

	// recv holds the value that was sent in, and recv+1 whether the called method exists.
	recv := f.newRegs(2)
	mode := f.newReg()
	res := f.newReg()
	f.emit(bytecode.OpLoadUndef, recv, 0, 0)
	f.emit(bytecode.OpLoadInt, mode, bytecode.ResumeNext, 0)

	loop := f.pc()
	onThrow := f.emit(bytecode.OpJumpIfInt, 0, mode, bytecode.ResumeThrow)
	onReturn := f.emit(bytecode.OpJumpIfInt, 0, mode, bytecode.ResumeReturn)

	// next: res = await next.call(it, recv)
	callBase := f.newRegs(3)
	f.emit(bytecode.OpMove, callBase, it+1, 0)
	f.emit(bytecode.OpMove, callBase+1, it, 0)
	f.emit(bytecode.OpMove, callBase+2, recv, 0)
	f.emit(bytecode.OpCall, callBase, 1, 0)
	f.emit(bytecode.OpAwait, res, callBase, 0)
	f.free(callBase)
	step := f.jump()

	// throw: res = await it.throw(recv), or close the iterator and fail when there is no throw method.
	f.patch(onThrow)
	f.emit(bytecode.OpIterCall, recv, it, f.key("throw"))
	hasThrow := f.emit(bytecode.OpJumpIf, 0, recv+1, 0)
	f.asyncIterClose(it)
	f.emit(bytecode.OpThrowError, bytecode.ThrowTypeError, f.constStr("the iterator does not provide a 'throw' method"), 0)
	f.patch(hasThrow)
	f.emit(bytecode.OpAwait, res, recv, 0)
	step2 := f.jump()

	// return: res = await it.return(recv), or return await recv when there is no return method.
	f.patch(onReturn)
	ret := f.newReg()
	f.emit(bytecode.OpMove, ret, recv, 0)
	f.emit(bytecode.OpIterCall, recv, it, f.key("return"))
	hasReturn := f.emit(bytecode.OpJumpIf, 0, recv+1, 0)
	f.emit(bytecode.OpAwait, ret, ret, 0)
	f.exit(exitReturn, nil, ret)
	f.patch(hasReturn)
	f.emit(bytecode.OpAwait, res, recv, 0)
	// A finished inner iterator makes the generator return its value.
	notDone := f.emit(bytecode.OpYieldDelegateResult, ret, res, 0)
	f.exit(exitReturn, nil, ret)
	f.free(ret)

	f.patch(step)
	f.patch(step2)
	done := f.emit(bytecode.OpYieldDelegateResult, dst, res, 0)
	end := f.jump()
	f.patchC(done)
	f.patchC(notDone)
	f.emit(bytecode.OpGetProp, recv, res, f.site("value"))
	f.emit(bytecode.OpYield, recv, recv, 1)
	f.emit(bytecode.OpGenResumeMode, mode, 0, 0)
	f.jumpTo(loop)
	f.patch(end)
}
