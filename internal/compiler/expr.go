// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

// constOperand returns the constant that the operator op takes for the right operand k: the
// operand as the operator converts it, so that the constant of a bitwise operator is an int32
// and that of a shift is a count below 32.
func constOperand(op bytecode.Opcode, k float64) float64 {
	switch op {
	case bytecode.OpBitOrK, bytecode.OpBitAndK:
		return float64(numconv.ToInt32(k))
	case bytecode.OpShlK, bytecode.OpShrK, bytecode.OpUShrK:
		return float64(numconv.ToUint32(k) & 31)
	}
	return k
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
func (f *fn) pure(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit, *ast.ThisExpr, *ast.BigIntLit:
		return true
	case *ast.Identifier:
		return f.localReg(e.Name) >= 0
	case *ast.UnaryExpr:
		if e.Op == ast.OpNot {
			return f.pure(e.Arg)
		}
		_, ok := numberLit(e)
		return ok
	}
	return false
}

// singleWrite reports whether compiling e into a register writes that register only
// after everything e reads was read, so that the register may be one that e reads.
func (f *fn) singleWrite(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit, *ast.Identifier, *ast.ThisExpr,
		*ast.BigIntLit, *ast.BinaryExpr, *ast.CallExpr, *ast.NewExpr, *ast.Function, *ast.RegExpLit,
		*ast.ObjectLit, *ast.ArrayLit, *ast.TemplateLit:
		return true
	case *ast.MemberExpr:
		return !e.Optional
	case *ast.SequenceExpr:
		// The expressions before the last one are evaluated for their effects only.
		return f.singleWrite(e.Exprs[len(e.Exprs)-1])
	case *ast.CondExpr:
		return f.singleWrite(e.Consequent) && f.singleWrite(e.Alternate)
	case *ast.AssignExpr:
		switch target := e.Target.(type) {
		case *ast.MemberExpr:
			// For simple assignments, require a result held in a temporary register.
			if e.Op == ast.OpNone {
				_, identifier := e.Value.(*ast.Identifier)
				return !identifier
			}
			return true
		case *ast.Identifier:
			// The assignment writes the local variable, and writes the register only when it
			// copies the variable to it at the end.
			if f.assignableReg(target.Name) < 0 {
				return false
			}
			switch e.Op {
			case ast.OpNone:
				return true
			case ast.OpAnd, ast.OpOr, ast.OpCoalesce:
				return false
			}
			return !f.clobbers(target.Name, e.Value)
		}
		return false
	case *ast.UnaryExpr:
		return e.Op != ast.OpDelete
	}
	return false
}

// clobbers reports whether evaluating the expressions rest can change the local variable name.
func (f *fn) clobbers(name string, rest ...ast.Expr) bool {
	for _, e := range rest {
		if e == nil || f.pure(e) {
			continue
		}
		if f.otherCodeAssigns(name) {
			return true
		}
		// nestedAssigns covers the nested functions, so only the own code is left.
		if assignsTo(e, name) {
			return true
		}
	}
	return false
}

// otherCodeAssigns reports whether code run by an operation, such as a getter or a function
// called from it, can change the local variable name.
func (f *fn) otherCodeAssigns(name string) bool {
	return f.mutated[name] || f.anyMutated || f.nestedAssigns(name)
}

// operand compiles e and returns the register that holds its value until the expressions rest,
// which are evaluated afterwards, are done.
func (f *fn) operand(e ast.Expr, rest ...ast.Expr) int32 {
	switch e := e.(type) {
	case *ast.ThisExpr:
		// Only super() assigns this, and localReg has no register for the this that
		// super() initializes.
		if r := f.localReg("this"); r >= 0 {
			return r
		}
	case *ast.Identifier:
		if r := f.localReg(e.Name); r >= 0 {
			if !f.clobbers(e.Name, rest...) {
				return r
			}
			t := f.newReg()
			f.emit(bytecode.OpMove, t, r, 0)
			return t
		}
	case *ast.AssignExpr:
		// An assignment to a local variable leaves its value in the variable.
		if id, ok := e.Target.(*ast.Identifier); ok {
			if r := f.assignableReg(id.Name); r >= 0 && !f.otherCodeAssigns(id.Name) && !f.clobbers(id.Name, rest...) {
				f.assign(e, -1)
				return r
			}
		}
	}
	return f.exprNew(e)
}

// exprNew compiles e into a newly allocated register and returns it.
func (f *fn) exprNew(e ast.Expr) int32 {
	r := f.newReg()
	f.exprInto(e, r, true)
	return r
}

// condNew compiles the conditional n into the register dst, which nothing else reads before n is
// done, when n is x ? x : y for a local variable x, and reports whether it did.
func (f *fn) condNew(n *ast.CondExpr, dst int32) bool {
	if t, ok := n.Test.(*ast.Identifier); ok {
		// Reading a local variable twice is reading it once, so x ? x : y is x || y. y cannot
		// read dst, which x is written to first.
		if c, ok := n.Consequent.(*ast.Identifier); ok && c.Name == t.Name && f.localReg(t.Name) >= 0 {
			f.expr(n.Test, dst)
			skip := f.emit(bytecode.OpJumpIf, 0, dst, 0)
			f.expr(n.Alternate, dst)
			f.patch(skip)
			return true
		}
	}
	return false
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
		f.call(e, -1)
	case *ast.NewExpr:
		f.newExpr(e, -1)
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
	f.exprInto(e, dst, false)
}

// exprInto is like expr. With fresh, nothing else reads dst before e is done.
func (f *fn) exprInto(e ast.Expr, dst int32, fresh bool) {
	f.c.step()
	switch e := e.(type) {
	case *ast.NumberLit:
		f.number(e.Value, dst)
	case *ast.StringLit:
		f.emit(bytecode.OpLoadK, dst, f.constStr(e.Value), 0)
	case *ast.BigIntLit:
		f.emit(bytecode.OpLoadK, dst, f.constant(bytecode.Const{
			Kind: bytecode.ConstBigInt,
			Str:  wtf8.String(e.Value),
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
		r := f.resolve(e.Name)
		if e.Name == "undefined" && r.kind == refGlobal && f.dynDepth(e.Name) == 0 {
			// The global undefined cannot be changed.
			f.emit(bytecode.OpLoadUndef, dst, 0, 0)
			return
		}
		f.setRefPos(e, e.Name, r)
		f.loadVarRef(e.Name, r, dst, false)
	case *ast.ThisExpr:
		f.loadVar("this", dst, false)
	case *ast.NewTargetExpr:
		f.loadVar("new.target", dst, false)
	case *ast.RegExpLit:
		f.emit(bytecode.OpRegExp, dst, f.regExpSite(e), 0)
	case *ast.TemplateLit:
		f.template(e, dst)
	case *ast.TaggedTemplate:
		f.taggedTemplate(e, dst)
	case *ast.ArrayLit:
		f.arrayLit(e, dst, fresh)
	case *ast.ObjectLit:
		if fresh {
			f.objectLitIn(e, dst)
		} else {
			f.objectLit(e, dst)
		}
	case *ast.Function:
		name := ""
		if e.Name != nil {
			name = e.Name.Name
			if e != f.c.dynamic {
				if f.c.isExpression == nil {
					f.c.isExpression = map[*ast.Function]bool{}
				}
				f.c.isExpression[e] = true
			}
		}
		f.closure(e, dst, wtf8.String(name))
	case *ast.Class:
		f.class(e, dst, "", -1)
	case *ast.UnaryExpr:
		f.unary(e, dst)
	case *ast.UpdateExpr:
		f.update(e, dst)
	case *ast.BinaryExpr:
		if !f.stringChain(e, dst) {
			f.binary(e, dst)
		}
	case *ast.LogicalExpr:
		f.logical(e, dst)
	case *ast.PrivateInExpr:
		mark := f.top
		name := f.newReg()
		f.loadVar("#"+e.Name.Name, name, false)
		obj := f.exprAny(e.Right)
		f.emit(bytecode.OpHasPrivate, dst, obj, name)
		f.free(mark)
	case *ast.AssignExpr:
		f.assign(e, dst)
	case *ast.CondExpr:
		if fresh && f.condNew(e, dst) {
			return
		}
		no := f.jumpIfFalse(e.Test)
		f.expr(e.Consequent, dst)
		end := f.jump()
		f.patchJumps(no)
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
		f.emit(bytecode.OpImportMeta, dst, 0, 0)
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

// binary compiles n after stringChain has ruled out concatenating it in one instruction.
func (f *fn) binary(n *ast.BinaryExpr, dst int32) {
	mark := f.top
	var l int32
	if left, ok := n.Left.(*ast.BinaryExpr); ok && n.Op == ast.OpAdd && left.Op == ast.OpAdd {
		// Every addition on the left has the same head. Compile the prefix without
		// walking it again to check for a string literal.
		l = f.newReg()
		f.c.step()
		f.binary(left, l)
	} else {
		l = f.immediateOperand(n.Left, n.Right)
	}
	if k, ok := numberLit(n.Right); ok {
		if op, ok := constOps[n.Op]; ok {
			f.emit(op, dst, l, f.constNum(constOperand(op, k)))
			f.free(mark)
			return
		}
	}
	r := f.immediateOperand(n.Right)
	if n.Op == ast.OpIn {
		// OpIn takes the object before the key.
		l, r = r, l
	}
	f.emit(binaryOps[n.Op], dst, l, r)
	if n.Op == ast.OpInstanceof {
		f.emit(bytecode.OpExtra, f.site("prototype"), 0, 0)
	}
	f.free(mark)
}

// stringChain compiles a chain of at least two additions whose leftmost operand is a string
// literal, such as "a" + x + "b", as one concatenation, and reports whether n is such a chain.
func (f *fn) stringChain(n *ast.BinaryExpr, dst int32) bool {
	adds := 0
	var hasExpr bool
	e := ast.Expr(n)
	for {
		b, ok := e.(*ast.BinaryExpr)
		if !ok || b.Op != ast.OpAdd {
			break
		}
		if _, ok := b.Right.(*ast.StringLit); !ok {
			hasExpr = true
		}
		adds++
		e = b.Left
	}
	if _, ok := e.(*ast.StringLit); !ok || adds < 2 {
		return false
	}
	mark := f.top
	defer f.free(mark)
	// Every intermediate sum is a string, so each other operand is converted as an addition to
	// a string converts it, right after it is evaluated. The parts go into consecutive
	// registers and are concatenated at once.
	var empty int32
	if hasExpr {
		empty = f.newReg()
		f.emit(bytecode.OpLoadK, empty, f.constStr(""), 0)
	}
	first := f.top
	f.stringChainParts(n, empty)
	switch f.top - first {
	case 0:
		f.emit(bytecode.OpLoadK, dst, f.constStr(""), 0)
	case 1:
		f.emit(bytecode.OpMove, dst, first, 0)
	default:
		f.emit(bytecode.OpConcat, dst, first, f.top-first)
	}
	return true
}

// stringChainParts puts the operands of the string chain e into consecutive registers from
// f.top on, leaving out empty literals. empty holds the empty string.
func (f *fn) stringChainParts(e ast.Expr, empty int32) {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == ast.OpAdd {
		f.stringChainParts(b.Left, empty)
		e = b.Right
	}
	if s, ok := e.(*ast.StringLit); ok {
		if s.Value != "" {
			f.emit(bytecode.OpLoadK, f.newReg(), f.constStr(s.Value), 0)
		}
		return
	}
	t := f.newReg()
	r := int32(-1)
	switch e := e.(type) {
	case *ast.Identifier:
		r = f.localReg(e.Name)
	case *ast.ThisExpr:
		r = f.localReg("this")
	}
	if r < 0 {
		f.expr(e, t)
		r = t
	}
	f.emit(bytecode.OpAdd, t, empty, r)
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

// primLiteral returns the operand c of OpJumpNotSEqP for e when e is undefined, null or a boolean
// literal, and -1 otherwise.
func (f *fn) primLiteral(e ast.Expr) int32 {
	switch e := e.(type) {
	case *ast.NullLit:
		return bytecode.PrimNull
	case *ast.BoolLit:
		if e.Value {
			return bytecode.PrimTrue
		}
		return bytecode.PrimFalse
	case *ast.Identifier:
		// The global undefined cannot be changed.
		if e.Name == "undefined" && f.resolve(e.Name).kind == refGlobal && f.dynDepth(e.Name) == 0 {
			return bytecode.PrimUndefined
		}
	case *ast.UnaryExpr:
		if _, ok := e.Arg.(*ast.NumberLit); ok && e.Op == ast.OpVoid {
			return bytecode.PrimUndefined
		}
	}
	return -1
}

// literalJump compiles the comparison n of a value with undefined, null or a boolean as a jump
// that is taken when the result of n is want, and reports whether n is such a comparison.
func (f *fn) literalJump(n *ast.BinaryExpr, want bool) (int, bool) {
	x, p := n.Left, f.primLiteral(n.Right)
	if p < 0 {
		x, p = n.Right, f.primLiteral(n.Left)
	}
	if p < 0 {
		return 0, false
	}
	switch n.Op {
	case ast.OpStrictEq, ast.OpStrictNe:
		op := bytecode.OpJumpNotSNeP
		if (n.Op == ast.OpStrictEq) != want {
			op = bytecode.OpJumpNotSEqP
		}
		return f.emit(op, 0, f.immediateOperand(x), p), true
	case ast.OpEq, ast.OpNe:
		if p != bytecode.PrimUndefined && p != bytecode.PrimNull {
			return 0, false
		}
		op := bytecode.OpJumpIfNotNullish
		if (n.Op == ast.OpEq) == want {
			op = bytecode.OpJumpIfNullish
		}
		// An object with [[IsHTMLDDA]] is loosely equal to null.
		return f.emit(op, 0, f.immediateOperand(x), 1), true
	}
	return 0, false
}

// jumpIfFalse compiles the condition e and returns the jumps that are taken when it is false.
func (f *fn) jumpIfFalse(e ast.Expr) jumps {
	mark := f.top
	defer f.free(mark)
	switch n := e.(type) {
	case *ast.BinaryExpr:
		if j, ok := f.literalJump(n, false); ok {
			return jumpAt(j)
		}
		if op, ok := compareJumps[n.Op]; ok {
			l := f.immediateOperand(n.Left, n.Right)
			if k, isNum := numberLit(n.Right); isNum {
				if kop, ok := compareConstJumps[n.Op]; ok {
					return jumpAt(f.emit(kop, 0, l, f.constNum(k)))
				}
			}
			r := f.immediateOperand(n.Right)
			return jumpAt(f.emit(op, 0, l, r))
		}
	case *ast.LogicalExpr:
		switch n.Op {
		case ast.OpAnd:
			left := f.jumpIfFalse(n.Left)
			return left.join(f.jumpIfFalse(n.Right))
		case ast.OpOr:
			yes := f.jumpIfTrue(n.Left)
			no := f.jumpIfFalse(n.Right)
			f.patchJumps(yes)
			return no
		}
	case *ast.UnaryExpr:
		if n.Op == ast.OpNot {
			return f.jumpIfTrue(n.Arg)
		}
	case *ast.BoolLit:
		if n.Value {
			return jumps{}
		}
		return jumpAt(f.jump())
	}
	r := f.exprAny(e)
	return jumpAt(f.emit(bytecode.OpJumpIfNot, 0, r, 0))
}

// jumpIfTrue compiles the condition e and returns the jumps that are taken when it is true.
func (f *fn) jumpIfTrue(e ast.Expr) jumps {
	mark := f.top
	defer f.free(mark)
	switch n := e.(type) {
	case *ast.LogicalExpr:
		switch n.Op {
		case ast.OpOr:
			left := f.jumpIfTrue(n.Left)
			return left.join(f.jumpIfTrue(n.Right))
		case ast.OpAnd:
			no := f.jumpIfFalse(n.Left)
			yes := f.jumpIfTrue(n.Right)
			f.patchJumps(no)
			return yes
		}
	case *ast.UnaryExpr:
		if n.Op == ast.OpNot {
			return f.jumpIfFalse(n.Arg)
		}
	case *ast.BinaryExpr:
		if j, ok := f.literalJump(n, true); ok {
			return jumpAt(j)
		}
		if _, ok := compareJumps[n.Op]; ok {
			// Jump over a jump: the fused instructions only exist for the false case.
			no := f.jumpIfFalse(e)
			yes := f.jump()
			f.patchJumps(no)
			return jumpAt(yes)
		}
	}
	r := f.exprAny(e)
	return jumpAt(f.emit(bytecode.OpJumpIf, 0, r, 0))
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

// strict reports whether the code being compiled is strict mode code. A class is strict even
// inside sloppy code.
func (f *fn) strict() bool {
	return f.f.Strict || f.classDepth > 0
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
		f.chainCheck(arg.Optional, obj)
		key := f.newReg()
		if arg.Computed {
			f.expr(arg.Property, key)
		} else {
			f.emit(bytecode.OpLoadK, key, f.constStr(wtf8.String(arg.Property.(*ast.Identifier).Name)), 0)
		}
		f.setPos(arg.Property.Start())
		op := bytecode.OpDeleteProp
		if f.strict() {
			op = bytecode.OpDeletePropStrict
		}
		f.emit(op, dst, obj, key)
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
			f.emit(bytecode.OpDeleteScoped, dst, scope, f.key(wtf8.String(arg.Name)))
			end = f.jump()
			f.patch(skip)
		}
		if r := f.resolve(arg.Name); r.kind != refGlobal {
			f.emit(bytecode.OpLoadFalse, dst, 0, 0)
		} else {
			f.emit(bytecode.OpDeleteName, dst, f.key(wtf8.String(arg.Name)), 0)
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
	op, postOp := bytecode.OpInc, bytecode.OpPostInc
	if n.Op == ast.OpDec {
		op, postOp = bytecode.OpDec, bytecode.OpPostDec
	}
	switch arg := n.Arg.(type) {
	case *ast.Identifier:
		if r := f.assignableReg(arg.Name); r >= 0 {
			switch {
			case dst < 0 || dst == r:
				f.emit(op, r, r, 0)
			case n.Prefix:
				f.emit(op, r, r, 0)
				f.emit(bytecode.OpMove, dst, r, 0)
			default:
				f.emit(postOp, dst, r, 0)
			}
			return
		}
		scope := f.resolveName(arg.Name)
		old := f.newReg()
		f.loadVarFrom(arg.Name, f.resolve(arg.Name), old, false, scope)
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
		// The getter and ToNumeric can run code.
		t := f.assignTargetBefore(arg, nil, false, true, true)
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

	// keySrc is the register that loadTarget converts into key, or -1 when key holds the key.
	keySrc int32

	// literal is set for a computed key that is a literal array index, which needs no register:
	// index is that index.
	literal bool
	index   int32

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
	return f.targetBefore(n, nil, false, false, false)
}

// targetBefore is like target for a reference that stays in use while the expressions rest are
// evaluated. With storeOnly, the reference is only stored to, so a computed key is not converted
// before the store and may stay in the register of a local variable. opaque reports whether
// other code, such as a getter, also runs while the reference is in use. With readNext, loadTarget
// reads the reference right away, so a computed key may stay in the register of a local variable
// until then.
func (f *fn) targetBefore(n *ast.MemberExpr, rest []ast.Expr, storeOnly, opaque, readNext bool) memberTarget {
	t := memberTarget{
		node:   n,
		key:    -1,
		keySrc: -1,
	}
	if _, ok := n.Object.(*ast.SuperExpr); ok {
		t.super = true
		t.this = f.newReg()
		f.loadVar("this", t.this, false)
		t.key = f.newReg()
		if n.Computed {
			f.expr(n.Property, t.key)
		} else {
			f.emit(bytecode.OpLoadK, t.key, f.constStr(wtf8.String(n.Property.(*ast.Identifier).Name)), 0)
		}
		t.home = f.newReg()
		f.loadVar("%home", t.home, false)
		return t
	}
	if p, ok := n.Property.(*ast.PrivateName); ok && !n.Computed {
		t.obj = f.heldOperand(n.Object, opaque, rest...)
		t.key = f.newReg()
		f.loadVar("#"+p.Name, t.key, false)
		return t
	}
	if n.Computed {
		t.obj = f.heldOperand(n.Object, opaque, append([]ast.Expr{n.Property}, rest...)...)
		if i, ok := literalIndex(n.Property); ok {
			t.literal, t.index = true, i
			return t
		}
		if storeOnly {
			// The store reads the key right after rest.
			t.key = f.immediateOperand(n.Property, rest...)
			return t
		}
		t.key = f.newReg()
		if readNext {
			t.keySrc = f.localKey(n.Property)
			if t.keySrc >= 0 {
				return t
			}
		}
		f.expr(n.Property, t.key)
		return t
	}
	t.obj = f.heldOperand(n.Object, opaque, rest...)
	return t
}

// localKey compiles the key e when its value ends up in the register of a local variable and
// returns that register, and returns -1 without compiling e otherwise.
func (f *fn) localKey(e ast.Expr) int32 {
	switch e := e.(type) {
	case *ast.Identifier:
		return f.localReg(e.Name)
	case *ast.UpdateExpr:
		if id, ok := e.Arg.(*ast.Identifier); ok && e.Prefix {
			if r := f.assignableReg(id.Name); r >= 0 {
				f.expr(e, r)
				return r
			}
		}
	}
	return -1
}

// heldOperand is like operand, and the register also holds the value while other code runs
// when opaque is set.
func (f *fn) heldOperand(e ast.Expr, opaque bool, rest ...ast.Expr) int32 {
	if id, ok := e.(*ast.Identifier); ok && opaque {
		if r := f.localReg(id.Name); r >= 0 && f.otherCodeAssigns(id.Name) {
			t := f.newReg()
			f.emit(bytecode.OpMove, t, r, 0)
			return t
		}
	}
	return f.operand(e, rest...)
}

// sourceOperand compiles the source e of a destructuring to the pattern p and returns the
// register that holds its value until the destructuring is done.
func (f *fn) sourceOperand(e ast.Expr, p ast.Pattern) int32 {
	switch e := e.(type) {
	case *ast.Identifier:
		if r := f.localReg(e.Name); r >= 0 && !patternAssigns(p, e.Name) && !f.otherCodeAssigns(e.Name) {
			return r
		}
	case *ast.ThisExpr:
		return f.operand(e)
	}
	return f.exprNew(e)
}

// assignTarget evaluates the property reference n for a store, which may follow a read.
func (f *fn) assignTarget(n *ast.MemberExpr) memberTarget {
	return f.assignTargetBefore(n, nil, false, false, false)
}

// assignTargetBefore is like assignTarget, with rest, storeOnly, opaque and readNext as for
// targetBefore.
func (f *fn) assignTargetBefore(n *ast.MemberExpr, rest []ast.Expr, storeOnly, opaque, readNext bool) memberTarget {
	t := f.targetBefore(n, rest, storeOnly, opaque, readNext)
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
		f.emit(bytecode.OpGetSuper, dst, t.this, t.key)
		f.emit(bytecode.OpExtra, t.home, superBase, 0)
	case t.literal:
		f.emit(bytecode.OpGetIndexInt, dst, t.obj, t.index)
	case t.key < 0:
		name := n.Property.(*ast.Identifier).Name
		f.emitGetProp(dst, t.obj, f.site(wtf8.String(name)))
	case !n.Computed:
		f.emit(bytecode.OpGetPrivate, dst, t.obj, t.key)
	default:
		// A compound assignment converts the key once, before the read and after the base is checked.
		src := t.key
		if t.keySrc >= 0 {
			src = t.keySrc
		}
		f.emit(bytecode.OpToPropertyKey, t.key, src, t.obj)
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
		op := bytecode.OpSetSuper
		if f.strict() {
			op = bytecode.OpSetSuperStrict
		}
		f.emit(op, t.this, t.key, src)
		f.emit(bytecode.OpExtra, t.home, 0, 0)
	case t.literal:
		op := bytecode.OpSetIndexInt
		if f.strict() {
			op = bytecode.OpSetIndexIntStrict
		}
		f.emit(op, t.obj, t.index, src)
	case t.key < 0:
		name := n.Property.(*ast.Identifier).Name
		op := bytecode.OpSetProp
		if f.strict() {
			op = bytecode.OpSetPropStrict
		}
		f.emit(op, t.obj, f.site(wtf8.String(name)), src)
	case !n.Computed:
		f.emit(bytecode.OpSetPrivate, t.obj, t.key, src)
	default:
		op := bytecode.OpSetIndex
		if f.strict() {
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
	if id, ok := n.Object.(*ast.Identifier); ok && id.Name == "arguments" && !n.Optional {
		// forwardsArguments allows only reads of length and computed keys.
		if args := f.forwardedArgs; args != nil && f.localReg("arguments") == args.reg {
			if !n.Computed {
				f.setPos(n.Property.Start())
				f.emit(bytecode.OpGetArgsLength, dst, args.reg, 0)
				return
			}
			key := f.immediateOperand(n.Property)
			f.setPos(n.Property.Start())
			f.emit(bytecode.OpGetArgsIndex, dst, args.reg, key)
			return
		}
	}
	if !n.Computed {
		obj := f.operand(n.Object)
		f.chainCheck(n.Optional, obj)
		name := n.Property.(*ast.Identifier).Name
		f.setPos(n.Property.Start())
		if name == "length" {
			f.emit(bytecode.OpGetLength, dst, obj, f.site(wtf8.String(name)))
			return
		}
		f.emitGetProp(dst, obj, f.site(wtf8.String(name)))
		return
	}
	obj := f.operand(n.Object, n.Property)
	f.chainCheck(n.Optional, obj)
	if i, ok := literalIndex(n.Property); ok {
		f.setPos(n.Property.Start())
		f.emit(bytecode.OpGetIndexInt, dst, obj, i)
		return
	}
	if s, ok := n.Property.(*ast.StringLit); ok && !isIndexString(s.Value) {
		f.setPos(n.Property.Start())
		f.emitGetProp(dst, obj, f.site(s.Value))
		return
	}
	key := f.immediateOperand(n.Property)
	f.setPos(n.Property.Start())
	f.emit(bytecode.OpGetIndex, dst, obj, key)
}

// literalIndex reports the array index that the number or string literal e denotes, when the
// index fits an int32.
func literalIndex(e ast.Expr) (int32, bool) {
	switch e := e.(type) {
	case *ast.NumberLit:
		if i := int32(e.Value); float64(i) == e.Value && i >= 0 {
			return i, true
		}
	case *ast.StringLit:
		return int32Index(e.Value)
	}
	return 0, false
}

// immediateOperand is like operand for a value that the instruction right after the expressions
// rest reads.
func (f *fn) immediateOperand(e ast.Expr, rest ...ast.Expr) int32 {
	// A prefix update of a local variable leaves its result in the variable.
	if update, ok := e.(*ast.UpdateExpr); ok && update.Prefix {
		if id, ok := update.Arg.(*ast.Identifier); ok {
			if r := f.assignableReg(id.Name); r >= 0 && !f.clobbers(id.Name, rest...) {
				f.expr(e, r)
				return r
			}
		}
	}
	return f.operand(e, rest...)
}

// int32Index returns the array index whose canonical string form is s, if it fits in an int32.
func int32Index(s wtf8.String) (int32, bool) {
	i, err := strconv.ParseInt(string(s), 10, 32)
	if err != nil || i < 0 || strconv.FormatInt(i, 10) != string(s) {
		return 0, false
	}
	return int32(i), true
}

// isIndexString reports whether s may be the canonical form of an array index.
func isIndexString(s wtf8.String) bool {
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

// spreadArgs puts an array of the values of the arguments args in the register dst.
func (f *fn) spreadArgs(args []ast.Expr, dst int32) {
	if len(args) == 1 {
		if s, ok := args[0].(*ast.SpreadElement); ok {
			mark := f.top
			v := f.exprAny(s.Arg)
			f.emit(bytecode.OpSpreadArgs, dst, v, 0)
			f.free(mark)
			return
		}
	}
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

// forwardedArgument returns the arguments identifier of a forwarding call, or nil.
func forwardedArgument(n *ast.CallExpr) *ast.Identifier {
	if isSuperCallee(n.Callee) {
		return nil
	}
	var arg ast.Expr
	switch len(n.Args) {
	case 1:
		if s, ok := n.Args[0].(*ast.SpreadElement); ok {
			arg = s.Arg
		}
	case 2:
		if m, ok := n.Callee.(*ast.MemberExpr); ok && !m.Computed && !hasSpread(n.Args) {
			if p, ok := m.Property.(*ast.Identifier); ok && p.Name == "apply" {
				arg = n.Args[1]
			}
		}
	}
	if id, ok := arg.(*ast.Identifier); ok && id.Name == "arguments" {
		return id
	}
	return nil
}

// callMember returns the member that supplies the receiver, and whether its optional chain
// ends before the call.
func callMember(callee ast.Expr) (*ast.MemberExpr, bool) {
	if chain, ok := callee.(*ast.OptionalChain); ok {
		m, ok := chain.Expr.(*ast.MemberExpr)
		return m, ok
	}
	m, _ := callee.(*ast.MemberExpr)
	return m, false
}

// localReceiver returns the local register holding the object of a named method, or -1.
func (f *fn) localReceiver(object ast.Expr) int32 {
	switch o := object.(type) {
	case *ast.Identifier:
		return f.localReg(o.Name)
	case *ast.ThisExpr:
		return f.localReg("this")
	}
	return -1
}

// callReference evaluates the callee into base. It reports whether it put a receiver in base+1.
func (f *fn) callReference(callee ast.Expr, base int32) bool {
	if c, chained := callMember(callee); c != nil {
		if chained {
			f.chains = append(f.chains, nil)
		}
		f.methodReference(c, base)
		if chained {
			end := f.jump()
			f.patchAll(f.chains[len(f.chains)-1])
			f.chains = f.chains[:len(f.chains)-1]
			f.emit(bytecode.OpLoadUndef, base, 0, 0)
			f.patch(end)
		}
		return true
	}
	if id, ok := callee.(*ast.Identifier); ok {
		mark := f.top
		if scope := f.resolveName(id.Name); scope >= 0 {
			skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
			f.emit(f.getScopedOp(), base, scope, f.key(wtf8.String(id.Name)))
			if f.evalScopesAround() {
				f.emit(bytecode.OpScopeThis, base+1, scope, 0)
			} else {
				f.emit(bytecode.OpMove, base+1, scope, 0)
			}
			end := f.jump()
			f.patch(skip)
			f.loadStatic(id.Name, f.resolve(id.Name), base, false)
			f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
			f.patch(end)
			f.free(mark)
			return true
		}
	}
	f.expr(callee, base)
	return false
}

// methodReference puts the method and its receiver in base and base+1.
func (f *fn) methodReference(c *ast.MemberExpr, base int32) {
	mark := f.top
	defer f.free(mark)
	if _, ok := c.Object.(*ast.SuperExpr); ok {
		t := f.target(c)
		f.loadTarget(t, base)
		f.emit(bytecode.OpMove, base+1, t.this, 0)
		return
	}
	if p, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
		if obj := f.localReceiver(c.Object); obj >= 0 {
			f.chainCheck(c.Optional, obj)
			f.setPos(p.Start())
			f.emit(bytecode.OpGetMethod, base, obj, f.site(wtf8.String(p.Name)))
			return
		}
	}
	f.expr(c.Object, base+1)
	f.chainCheck(c.Optional, base+1)
	switch p := c.Property.(type) {
	case *ast.PrivateName:
		name := f.newReg()
		f.loadVar("#"+p.Name, name, false)
		f.setPos(p.Start())
		f.emit(bytecode.OpGetPrivate, base, base+1, name)
		return
	case *ast.Identifier:
		if !c.Computed {
			f.setPos(p.Start())
			f.emitGetProp(base, base+1, f.site(wtf8.String(p.Name)))
			return
		}
	}
	key := f.exprAny(c.Property)
	f.setPos(c.Property.Start())
	f.emit(bytecode.OpGetIndex, base, base+1, key)
}

// call compiles the call n and leaves its result in dst, or drops it when dst is -1.
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
	var argsFirst bool
	var regExpArg *ast.RegExpLit
	// receiver is the site of a regular expression literal that is the this value, or -1.
	receiver := int32(-1)
	pos := n.Callee.End()
	var eval bool
	if c, chained := callMember(n.Callee); c != nil {
		pos = c.Property.Start()
		if _, super := c.Object.(*ast.SuperExpr); super {
			pos = n.Start()
		} else if p, ok := c.Property.(*ast.Identifier); ok && !c.Computed {
			if lit, ok := c.Object.(*ast.RegExpLit); ok && !hasSpread(n.Args) && (p.Name == "exec" || p.Name == "test") {
				// The call decides whether the literal's object needs to be made.
				receiver = f.regExpSite(lit)
				f.setPos(pos)
				f.emit(bytecode.OpRegExpMethod, base, receiver, f.site(wtf8.String(p.Name)))
			}
			if !c.Optional && !n.Optional && !chained && p.Name != "call" && p.Name != "apply" && f.localReceiver(c.Object) >= 0 && f.earlyArgs(n) {
				// The call can follow the method read right away.
				for _, a := range n.Args {
					f.expr(a, f.newReg())
				}
				argsFirst = true
			}
			switch p.Name {
			case "call":
				if !hasSpread(n.Args) {
					op = bytecode.OpCallCall
				}
			case "match", "replace", "replaceAll", "search", "split":
				// The call decides whether its first argument's RegExp object can escape.
				if len(n.Args) > 0 && !hasSpread(n.Args) {
					regExpArg, _ = n.Args[0].(*ast.RegExpLit)
				}
			}
		}
	} else if id, ok := n.Callee.(*ast.Identifier); ok {
		pos = n.Start()
		eval = id.Name == "eval" && !n.Optional
	}
	if receiver < 0 && !f.callReference(n.Callee, base) {
		if eval {
			f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
		} else {
			op = f.undefinedThis(n.Args, base)
		}
	}
	if eval {
		op = bytecode.OpCallEval
	}
	f.chainCheck(n.Optional, base)

	if args := f.forwardedArgs; args != nil && forwardedArgument(n) != nil && f.localReg("arguments") == args.reg {
		if len(n.Args) == 1 {
			f.setPos(pos)
			op := bytecode.OpCallFwd
			if f.isTail(n) {
				op = bytecode.OpTailCallFwd
			}
			f.emit(op, callDst(dst, base), base, args.reg)
			return
		}
		this := f.operand(n.Args[0])
		// An ordinary apply call takes its two arguments in base+2 and base+3.
		for f.top < base+4 {
			f.newReg()
		}
		f.setPos(pos)
		op := bytecode.OpApplyFwd
		if f.isTail(n) {
			op = bytecode.OpTailApplyFwd
		}
		f.emit(op, callDst(dst, base), base, this)
		f.emit(bytecode.OpExtra, args.reg, 0, 0)
		return
	}
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(pos)
		evalOp, spreadOp := bytecode.OpCallEvalSpread, bytecode.OpCallSpread
		if f.isTail(n) {
			evalOp, spreadOp = bytecode.OpTailCallEvalSpread, bytecode.OpTailCallSpread
		}
		if op == bytecode.OpCallEval {
			site := f.evalSite()
			f.emit(evalOp, callDst(dst, base), base, 0)
			f.emit(bytecode.OpExtra, site, 0, 0)
			return
		}
		f.emit(spreadOp, callDst(dst, base), base, 0)
		return
	}
	for i, a := range n.Args {
		if argsFirst {
			break
		}
		if i == 0 && regExpArg != nil {
			f.newReg()
			continue
		}
		f.exprNew(a)
	}
	site := int32(-1)
	if op == bytecode.OpCallEval {
		site = f.evalSite()
	}
	if f.isTail(n) {
		switch op {
		case bytecode.OpCall, bytecode.OpCallCall:
			op = bytecode.OpTailCall
		case bytecode.OpCallUndef:
			op = bytecode.OpTailCallUndef
		case bytecode.OpCallEval:
			op = bytecode.OpTailCallEval
		}
	}
	f.setPos(pos)
	if regExpArg != nil {
		f.emit(bytecode.OpRegExpArg, base+2, f.regExpSite(regExpArg), base)
	}
	if receiver >= 0 {
		f.emit(bytecode.OpRegExpArg, base+1, receiver, base)
	}
	f.emit(op, callDst(dst, base), base, int32(len(n.Args)))
	if site >= 0 {
		f.emit(bytecode.OpExtra, site, 0, 0)
	}
}

// earlyArgs reports whether the arguments of the call n can be evaluated before its callee is read,
// as they are literals, the this value or local variables that no getter can assign, and n is not
// a tail call.
func (f *fn) earlyArgs(n *ast.CallExpr) bool {
	if len(n.Args) == 0 || f.isTail(n) {
		return false
	}
	for _, a := range n.Args {
		switch a := a.(type) {
		case *ast.NumberLit, *ast.StringLit, *ast.BoolLit, *ast.NullLit:
		case *ast.ThisExpr:
			if f.localReg("this") < 0 {
				return false
			}
		case *ast.Identifier:
			if f.localReg(a.Name) < 0 || f.otherCodeAssigns(a.Name) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// undefinedThis returns the opcode of a call with the arguments args and its window at base, whose
// this value is undefined. It loads undefined into base+1 unless the opcode does so itself.
func (f *fn) undefinedThis(args []ast.Expr, base int32) bytecode.Opcode {
	if hasSpread(args) {
		f.emit(bytecode.OpLoadUndef, base+1, 0, 0)
		return bytecode.OpCall
	}
	return bytecode.OpCallUndef
}

// regExpSite adds the regular expression literal e to the function and returns its index.
func (f *fn) regExpSite(e *ast.RegExpLit) int32 {
	f.f.RegExps = append(f.f.RegExps, bytecode.RegExp{
		Pattern: e.Pattern,
		Flags:   e.Flags,
	})
	return int32(len(f.f.RegExps) - 1)
}

// callDst returns the destination operand of a call with its window at base whose result goes to
// dst, or is dropped when dst is -1.
func callDst(dst, base int32) int32 {
	if dst < 0 {
		// A dropped result replaces the callee, whose register is free after the call.
		return base
	}
	return dst
}

// result moves the value of the register src to dst, unless dst is -1 or src itself.
func (f *fn) result(dst, src int32) {
	if dst >= 0 && dst != src {
		f.emit(bytecode.OpMove, dst, src, 0)
	}
}

func (f *fn) superCall(n *ast.CallExpr, dst int32) {
	base := f.newRegs(2)
	f.loadVar("%callee", base, false)
	// GetSuperConstructor comes before the arguments.
	f.emit(bytecode.OpSuperBase, base, base, 0)
	f.loadVar("new.target", base+1, false)
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(n.Start())
		f.emit(bytecode.OpSuperSpread, base, base, 0)
	} else {
		for _, a := range n.Args {
			f.exprNew(a)
		}
		f.setPos(n.Start())
		f.emit(bytecode.OpSuperCall, base, base, int32(len(n.Args)))
	}
	// The result becomes this, which must not be initialized yet, so it reaches dst only
	// after the check.
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

// newExpr compiles the new expression n, with dst as for call.
func (f *fn) newExpr(n *ast.NewExpr, dst int32) {
	mark := f.top
	defer f.free(mark)
	f.setPos(n.Start())
	base := f.newRegs(2)
	f.expr(n.Callee, base)
	if hasSpread(n.Args) {
		f.spreadArgs(n.Args, f.newReg())
		f.setPos(n.Start())
		f.emit(bytecode.OpNewSpread, callDst(dst, base), base, 0)
		return
	}
	for _, a := range n.Args {
		f.exprNew(a)
	}
	f.setPos(n.Start())
	f.emit(bytecode.OpNew, callDst(dst, base), base, int32(len(n.Args)))
	f.emit(bytecode.OpExtra, f.site("prototype"), 0, 0)
}

func (f *fn) template(n *ast.TemplateLit, dst int32) {
	if len(n.Exprs) == 0 {
		f.emit(bytecode.OpLoadK, dst, f.constStr(n.Quasis[0].Cooked), 0)
		return
	}
	mark := f.top
	defer f.free(mark)
	// The parts go into consecutive registers and are concatenated at once, after each
	// substitution was converted to a string in order.
	if s := n.Quasis[0].Cooked; s != "" {
		f.emit(bytecode.OpLoadK, f.newReg(), f.constStr(s), 0)
	}
	for i, e := range n.Exprs {
		t := f.newReg()
		f.expr(e, t)
		f.emit(bytecode.OpToString, t, t, 0)
		if s := n.Quasis[i+1].Cooked; s != "" {
			f.emit(bytecode.OpLoadK, f.newReg(), f.constStr(s), 0)
		}
	}
	if f.top-mark == 1 {
		f.emit(bytecode.OpMove, dst, mark, 0)
		return
	}
	f.emit(bytecode.OpConcat, dst, mark, f.top-mark)
}

func (f *fn) taggedTemplate(n *ast.TaggedTemplate, dst int32) {
	mark := f.top
	defer f.free(mark)
	base := f.newRegs(2)
	op, tailOp := bytecode.OpCall, bytecode.OpTailCall
	if !f.callReference(n.Tag, base) {
		op, tailOp = bytecode.OpCallUndef, bytecode.OpTailCallUndef
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
	if f.isTail(n) {
		op = tailOp
	}
	f.emit(op, callDst(dst, base), base, int32(len(n.Quasi.Exprs)+1))
}

func (f *fn) arrayLit(n *ast.ArrayLit, dst int32, fresh bool) {
	// Constants read no register, so dst can receive the array right away.
	if f.constArray(n, dst) {
		return
	}
	mark := f.top
	defer f.free(mark)
	arr := dst
	if !fresh {
		// An element can read the previous value of dst.
		arr = f.newReg()
	}
	room := int32(len(n.Elements))
	if room == 0 {
		// An empty literal usually gets elements pushed onto it.
		room = 4
	}
	f.emit(bytecode.OpNewArray, arr, 0, room)
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
	f.result(dst, arr)
}

// constArray builds the array of n in the register dst from a run of constants, and reports
// whether it did, which it does when there is at least one element and [fn.literalConst] knows
// the value of each. Otherwise it leaves the constants unchanged.
func (f *fn) constArray(n *ast.ArrayLit, dst int32) bool {
	if len(n.Elements) == 0 {
		return false
	}
	start := len(f.f.Consts)
	for _, e := range n.Elements {
		f.c.step()
		c, ok := f.literalConst(e)
		if !ok {
			clear(f.f.Consts[start:])
			f.f.Consts = f.f.Consts[:start]
			return false
		}
		f.f.Consts = append(f.f.Consts, c)
	}
	f.emit(bytecode.OpNewArrayK, dst, int32(start), int32(len(n.Elements)))
	return true
}

// literalConst returns the value of e when it is a primitive value that the compiler knows.
func (f *fn) literalConst(e ast.Expr) (bytecode.Const, bool) {
	if n, ok := numberLit(e); ok {
		return bytecode.Const{
			Kind: bytecode.ConstNumber,
			Num:  n,
		}, true
	}
	switch e := e.(type) {
	case *ast.StringLit:
		return bytecode.Const{
			Kind: bytecode.ConstString,
			Str:  e.Value,
		}, true
	case *ast.BigIntLit:
		return bytecode.Const{
			Kind: bytecode.ConstBigInt,
			Str:  wtf8.String(e.Value),
		}, true
	}
	if p := f.primLiteral(e); p >= 0 {
		return bytecode.Const{
			Kind: primConsts[p],
		}, true
	}
	return bytecode.Const{}, false
}

// primConsts maps the results of [fn.primLiteral] to the kinds of their constants.
var primConsts = [...]bytecode.ConstKind{
	bytecode.PrimUndefined: bytecode.ConstUndefined,
	bytecode.PrimNull:      bytecode.ConstNull,
	bytecode.PrimFalse:     bytecode.ConstFalse,
	bytecode.PrimTrue:      bytecode.ConstTrue,
}

// keyName returns the name of a property key that is known at compile time.
func keyName(key ast.Expr, computed bool) (wtf8.String, bool) {
	if computed {
		return "", false
	}
	switch k := key.(type) {
	case *ast.Identifier:
		return wtf8.String(k.Name), true
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
func (f *fn) named(e ast.Expr, dst int32, name wtf8.String) {
	switch e := e.(type) {
	case *ast.Function:
		if e.Name == nil {
			f.closure(e, dst, name)
			return
		}
	case *ast.Class:
		if e.Name == nil {
			f.class(e, dst, name, -1)
			return
		}
	}
	f.expr(e, dst)
}

// namedByKey compiles e into dst. When e is an anonymous function or class, it gets the name
// of the property key in the register key.
func (f *fn) namedByKey(e ast.Expr, dst, key int32) {
	switch e := e.(type) {
	case *ast.Function:
		if e.Name == nil {
			f.closure(e, dst, "")
			f.emit(bytecode.OpSetFuncName, dst, key, bytecode.NamePrefixNone)
			return
		}
	case *ast.Class:
		if e.Name == nil {
			f.class(e, dst, "", key)
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
		f.emit(bytecode.OpToPropertyKey, k, k, -1)
		return k
	}
	switch key := key.(type) {
	case *ast.Identifier:
		f.emit(bytecode.OpLoadK, k, f.constStr(wtf8.String(key.Name)), 0)
	case *ast.PrivateName:
		f.loadVar("#"+key.Name, k, false)
	default:
		f.expr(key, k)
		f.emit(bytecode.OpToPropertyKey, k, k, -1)
	}
	return k
}

func (f *fn) objectLit(n *ast.ObjectLit, dst int32) {
	mark := f.top
	defer f.free(mark)
	obj := f.newReg()
	f.objectLitIn(n, obj)
	f.emit(bytecode.OpMove, dst, obj, 0)
}

// objectLitIn builds the object of n in the register obj.
func (f *fn) objectLitIn(n *ast.ObjectLit, obj int32) {
	mark := f.top
	defer f.free(mark)
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
				var v int32
				if isAnonymousFunction(p.Value) {
					v = f.newReg()
					f.named(p.Value, v, name)
				} else {
					v = f.exprAny(p.Value)
				}
				f.emit(bytecode.OpDefineFieldK, obj, f.site(name), v)
			default:
				k := f.propertyKey(p.Key, p.Computed)
				v := f.newReg()
				f.namedByKey(p.Value, v, k)
				f.emit(bytecode.OpDefineField, obj, k, v)
			}
		case ast.PropertyMethod, ast.PropertyGet, ast.PropertySet:
			f.method(obj, p.Key, p.Computed, p.Value.(*ast.Function), p.Kind, true)
		}
		f.free(m)
	}
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
		name, static = wtf8.String("#"+p.Name), true
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
		f.emit(bytecode.OpExtra, flag, 0, 0)
	case ast.PropertySet:
		f.emit(bytecode.OpDefineSetter, obj, k, v)
		f.emit(bytecode.OpExtra, flag, 0, 0)
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
		local := f.assignableReg(name)
		switch {
		case n.Op == ast.OpNone:
			if local >= 0 {
				if f.chainedStore(n.Value, name, local) {
					if dst >= 0 && dst != local {
						f.emit(bytecode.OpMove, dst, local, 0)
					}
					return
				}
				if f.singleWrite(n.Value) {
					f.named(n.Value, local, wtf8.String(inferred))
				} else {
					t := f.newReg()
					f.named(n.Value, t, wtf8.String(inferred))
					f.emit(bytecode.OpMove, local, t, 0)
				}
				if dst >= 0 && dst != local {
					f.emit(bytecode.OpMove, dst, local, 0)
				}
				return
			}
			scope := f.resolveName(name)
			// A strict assignment to a global that does not exist yet throws even when the
			// value creates the global.
			resolved := int32(-1)
			if scope < 0 && f.strict() && !f.pure(n.Value) && f.undeclaredGlobal(name) {
				resolved = f.newReg()
				f.emit(bytecode.OpResolveGlobal, resolved, f.key(wtf8.String(name)), f.globalCache())
			}
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.named(n.Value, t, wtf8.String(inferred))
			if resolved >= 0 {
				ok := f.emit(bytecode.OpJumpIf, 0, resolved, 0)
				f.emit(bytecode.OpThrowError, bytecode.ThrowReferenceError, f.constStr(wtf8.String(name+" is not defined")), 0)
				f.patch(ok)
			}
			f.storeVarTo(n, name, t, false, scope)
		case logical:
			scope := f.resolveName(name)
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.loadVarFrom(name, f.resolve(name), t, false, scope)
			skip := shortCircuit(t)
			f.named(n.Value, t, wtf8.String(inferred))
			f.storeVarTo(n, name, t, false, scope)
			f.patch(skip)
		default:
			if local >= 0 && !f.clobbers(name, n.Value) {
				if k, ok := numberLit(n.Value); ok {
					if op, ok := constOps[n.Op]; ok {
						f.emit(op, local, local, f.constNum(constOperand(op, k)))
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
			f.loadVarFrom(name, f.resolve(name), old, false, scope)
			r := f.exprAny(n.Value)
			t := dst
			if t < 0 {
				t = f.newReg()
			}
			f.emit(compoundOp(n.Op), t, old, r)
			f.storeVarTo(n, name, t, false, scope)
		}
	case *ast.MemberExpr:
		// A compound or logical assignment runs the getter after the reference.
		t := f.assignTargetBefore(target, []ast.Expr{n.Value}, n.Op == ast.OpNone, n.Op != ast.OpNone, n.Op != ast.OpNone)
		switch {
		case n.Op == ast.OpNone:
			// The result is the value read before the store, which can run code.
			v := f.heldOperand(n.Value, dst >= 0)
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
		v := f.sourceOperand(n.Value, n.Target)
		f.destructure(n.Target, v, patAssign)
		if dst >= 0 {
			f.result(dst, v)
		}
	}
}

// chainedStore compiles e, the value assigned to the local variable name in the register local,
// when e is a property assignment whose value can be computed straight into local, and reports
// whether it did.
func (f *fn) chainedStore(e ast.Expr, name string, local int32) bool {
	inner, ok := e.(*ast.AssignExpr)
	if !ok || inner.Op != ast.OpNone {
		return false
	}
	m, ok := inner.Target.(*ast.MemberExpr)
	if !ok || m.Optional {
		return false
	}
	if _, ok := m.Object.(*ast.SuperExpr); ok {
		return false
	}
	if _, ok := m.Property.(*ast.PrivateName); ok {
		return false
	}
	// The variable gets the value before the store, which can throw, so only code that cannot
	// see the variable may run between them, and the reference must not read the variable.
	if !f.unobserved(name) || mentions(m, name) || !f.singleWrite(inner.Value) {
		return false
	}
	mark := f.top
	defer f.free(mark)
	t := f.assignTargetBefore(m, []ast.Expr{inner.Value}, true, false, false)
	f.expr(inner.Value, local)
	f.storeTarget(t, local)
	return true
}

// unobserved reports whether the local variable name can be read only by the code of f outside
// exception handlers.
func (f *fn) unobserved(name string) bool {
	// Other code reads a register only through the closures of the function, which read the
	// registers of the variables they capture, a direct eval, and the arguments object or
	// Function.prototype.arguments for a parameter. Code run after a throw reads it only in
	// a catch or finally block of the function.
	if f.node == nil || f.node.Nested != 0 || f.anyMutated || f.catchDepth > 0 {
		return false
	}
	if name == "arguments" || f.isParam(name) {
		return false
	}
	for _, c := range f.control {
		if c.kind == ctlFinally {
			return false
		}
	}
	return true
}

// isParam reports whether name is a parameter of f.
func (f *fn) isParam(name string) bool {
	found := false
	for _, p := range f.node.Params {
		patternNames(p, func(id *ast.Identifier) {
			if id.Name == name {
				found = true
			}
		})
	}
	return found
}

// mentions reports whether the identifier name occurs in n.
func mentions(n ast.Node, name string) bool {
	found := false
	walk(n, func(m ast.Node) bool {
		if id, ok := m.(*ast.Identifier); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
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
	f.emit(bytecode.OpCall, res, callBase, 1)
	f.free(callBase)
	step := f.jump()

	// throw: res = it.throw(recv), or close the iterator and fail when there is no throw method.
	f.patch(onThrow)
	f.emit(bytecode.OpIterCall, recv, it, f.key("throw"))
	hasThrow := f.emit(bytecode.OpJumpIf, 0, recv+1, 0)
	f.emit(bytecode.OpIterClose, it, -1, 0)
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
	f.emit(bytecode.OpCall, callBase, callBase, 1)
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
	f.emitGetProp(recv, res, f.site("value"))
	f.emit(bytecode.OpYield, recv, recv, 1)
	f.emit(bytecode.OpGenResumeMode, mode, 0, 0)
	f.jumpTo(loop)
	f.patch(end)
}
