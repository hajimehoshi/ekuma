// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode

// IsTailCall reports whether an instruction of op can replace the running frame with the frame
// of a callee.
func (op Opcode) IsTailCall() bool {
	switch op {
	case OpTailCall, OpTailCallUndef, OpTailCallSpread, OpTailCallEval, OpTailCallEvalSpread,
		OpTailApplyFwd, OpTailCallFwd:
		return true
	}
	return false
}

// Covers reports whether h catches the exceptions of the instruction at pc.
func (h Handler) Covers(pc int) bool {
	return int(h.Start) <= pc && pc < int(h.End)
}

// NextPC returns the index of the instruction after the one at pc of code, which must be valid.
func NextPC(code []Instr, pc int) int {
	if opcodes[code[pc].Op].operands.Extra {
		return pc + 2
	}
	return pc + 1
}

// CanContinueAt reports whether the instruction at pc of code, which must be valid, can continue
// at to by itself, without throwing.
func CanContinueAt(code []Instr, pc, to int) bool {
	in, x, ops := instrAt(code, pc)
	switch opcodes[in.Op].flow {
	case FlowNext, FlowNextOrSkip:
		if to == NextPC(code, pc) {
			return true
		}
	}
	for field := FieldA; field <= FieldXB; field++ {
		if ops.kind(field) == OperandTarget && int(operand(in, x, field)) == to {
			return true
		}
	}
	return false
}

// MayRead reports whether the instruction at pc of code, which must be valid, may read the
// register r as [Operands.Reads] describes.
func MayRead(code []Instr, pc, r int) bool {
	in, x, ops := instrAt(code, pc)
	return ops.covers(in, x, ops.Reads, r)
}

// MayWrite reports whether the instruction at pc of code, which must be valid, may write the
// register r as [Operands.Writes] describes.
func MayWrite(code []Instr, pc, r int) bool {
	in, x, ops := instrAt(code, pc)
	return ops.covers(in, x, ops.Writes, r)
}

// covers reports whether one of the runs of registers rs of the instruction in, followed by x,
// holds the register r.
func (ops *Operands) covers(in, x Instr, rs []Regs, r int) bool {
	for _, w := range rs {
		start := operand(in, x, w.At)
		if start < 0 {
			continue
		}
		first := int(start) + w.Offset
		if r < first {
			continue
		}
		if w.Len == RegsToEnd {
			return true
		}
		n := w.Len
		if w.Counted {
			n += ops.count(in, x)
		}
		if r < first+n {
			return true
		}
	}
	return false
}

// instrAt returns the instruction at pc of code, its OpExtra if it takes one, and the
// description of its operands.
func instrAt(code []Instr, pc int) (in, x Instr, ops *Operands) {
	in = code[pc]
	ops = &opcodes[in.Op].operands
	if ops.Extra {
		x = code[pc+1]
	}
	return in, x, ops
}

// count returns the count operand of the instruction in, followed by x when it takes an OpExtra,
// or 0 when it has none.
func (ops *Operands) count(in, x Instr) int {
	for field := FieldA; field <= FieldXB; field++ {
		if ops.kind(field) == OperandCount {
			return int(operand(in, x, field))
		}
	}
	return 0
}

// MarkFusions records which consecutive instructions can run together.
func (f *Func) MarkFusions() {
	for pc := range f.Code {
		first := &f.Code[pc]
		first.FuseNext = pc+1 < len(f.Code) && fusionPair(*first, f.Code[pc+1], pc)
	}
}

func fusionPair(first, second Instr, pc int) bool {
	switch first.Op {
	case OpGetGlobal, OpGetGlobalStrict, OpGetGlobalOrUndef, OpGetGlobalOrUndefStrict,
		OpGetProp, OpGetLength, OpGetArgsLength, OpGetIndex, OpGetIndexInt, OpGetArgsIndex, OpGetArgsIndexInt,
		OpMul, OpMulAdd, OpMulK, OpIterNext, OpCallUndef:
		return second.Op == OpAdd &&
			(second.A == second.B && second.C == first.A || second.A == second.C && second.B == first.A)
	case OpInc, OpDec, OpPostInc, OpPostDec, OpIncUpval, OpDecUpval, OpAddK, OpSubK:
		return (second.Op == OpJump || second.Op >= OpLoopIf && second.Op <= OpLoopGeK) && int(second.A) <= pc
	}
	return false
}
