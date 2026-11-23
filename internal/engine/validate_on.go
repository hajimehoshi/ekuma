// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package engine

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// validateExec says whether the interpreter checks the instructions that it runs against the
// opcode table. It panics when an instruction changes a register that [bytecode.Opcode.Operands]
// does not list among its writes, or continues where neither [bytecode.Opcode.Flow] nor a jump
// target lets it.
const validateExec = true

// execChecks is the state of the checks of the instructions.
type execChecks struct {
	// steps holds, by frame index, the instruction that the frame started last. Only the first
	// top of them can be valid.
	steps []execStep
	top   int
}

// execStep is an instruction that a frame started, and the registers before it.
type execStep struct {
	valid bool

	// fp, base and gen identify the frame.
	fp   *funcProto
	base int
	gen  *generator

	pc   int
	regs []Value
}

// enterChecks forgets the instructions of the running frame and of the frames after it, which
// an interpreter loop that starts or resumes the running frame leaves unchecked.
func (ag *agent) enterChecks() {
	ag.checks.forget(len(ag.frames) - 1)
}

// leaveChecks forgets the instructions of the frames that have ended or been suspended when an
// interpreter loop returns.
func (ag *agent) leaveChecks() {
	ag.checks.forget(len(ag.frames))
}

// forget forgets the instructions of the frames from the index n on, without keeping their
// values reachable.
func (c *execChecks) forget(n int) {
	for k := n; k < c.top; k++ {
		step := &c.steps[k]
		clear(step.regs)
		*step = execStep{
			regs: step.regs[:0],
		}
	}
	c.top = min(c.top, n)
}

// checkStep checks the instruction that the running frame started last, which continues at pc
// with the registers regs, and records the instruction at pc. fp is the code of the frame.
func (ag *agent) checkStep(fp *funcProto, pc int, regs []Value) {
	c := &ag.checks
	n := len(ag.frames) - 1
	fr := &ag.frames[n]
	// The frames after the running one have ended.
	c.forget(n + 1)
	c.top = n + 1
	for len(c.steps) < c.top {
		c.steps = append(c.steps, execStep{})
	}
	s := &c.steps[n]
	if s.valid && s.fp == fp && s.base == fr.base && s.gen == fr.gen {
		ag.checkInstr(s, pc, regs)
	}
	s.valid = true
	s.fp, s.base, s.gen, s.pc = fp, fr.base, fr.gen, pc
	// The slot after the registers receives the results that the loop discards.
	s.regs = append(s.regs[:0], regs[:fp.nregs]...)
}

// checkInstr checks the instruction of s, after which its frame continues at pc with the
// registers regs. The instruction may also have run the instruction after it, when its flow
// lets that one follow, and a throw continues at a handler.
func (ag *agent) checkInstr(s *execStep, pc int, regs []Value) {
	regs = regs[:len(s.regs)]
	code := s.fp.code
	bad, reached := ag.fits(s, regs, pc, s.pc, -1)
	if reached && bad < 0 {
		return
	}
	if second := nextPC(code, s.pc); second < len(code) && follows(code, s.pc, second) {
		b, r := ag.fits(s, regs, pc, s.pc, second)
		if r && b < 0 {
			return
		}
		if r && !reached {
			bad, reached = b, r
		}
	}
	switch {
	case !reached && isTailCall(code[s.pc].Op):
		// The callee took the place of the frame.
	case !reached:
		panic(ag.failure(s, fmt.Sprintf("continued at %d", pc)))
	default:
		panic(ag.failure(s, fmt.Sprintf("changed the register %d and continued at %d", bad, pc)))
	}
}

// fits reports whether the instructions at first and second of the frame of s, or the one at
// first alone when second is -1, can continue at to, and returns a register that they changed
// without writing it, or -1. The registers of the frame were s.regs before and are regs after.
func (ag *agent) fits(s *execStep, regs []Value, to, first, second int) (bad int, reached bool) {
	code := s.fp.code
	last := first
	if second >= 0 {
		last = second
	}
	handlerReg := -1
	if !follows(code, last, to) {
		// A throw continues at a handler, which receives the thrown value.
		found := false
		for _, h := range s.fp.Handlers {
			if int(h.Target) == to && (covers(h, first) || covers(h, last)) {
				handlerReg = int(h.Reg)
				found = true
				break
			}
		}
		if !found {
			return -1, false
		}
	}
	for r := range regs {
		if sameBits(s.regs[r], regs[r]) || r == handlerReg || writesAt(code, first, r) || second >= 0 && writesAt(code, second, r) || ag.hasOpenUpval(s, r) {
			continue
		}
		return r, true
	}
	return -1, true
}

func covers(h bytecode.Handler, pc int) bool {
	return int(h.Start) <= pc && pc < int(h.End)
}

// nextPC returns the index of the instruction after the one at pc of code.
func nextPC(code []bytecode.Instr, pc int) int {
	if code[pc].Op.Operands().Extra {
		return pc + 2
	}
	return pc + 1
}

// follows reports whether the flow or a jump target of the instruction at pc of code lets the
// instruction at to follow it.
func follows(code []bytecode.Instr, pc, to int) bool {
	in := &code[pc]
	ops := in.Op.Operands()
	var x *bytecode.Instr
	if ops.Extra {
		x = &code[pc+1]
	}
	switch in.Op.Flow() {
	case bytecode.FlowNext:
		if to == nextPC(code, pc) {
			return true
		}
	case bytecode.FlowNextOrSkip:
		if to == nextPC(code, pc) || to == pc+2 {
			return true
		}
	}
	for f, k := range [...]bytecode.OperandKind{ops.A, ops.B, ops.C, ops.XA, ops.XB} {
		if k == bytecode.OperandTarget && int(operandOf(in, x, bytecode.Field(f))) == to {
			return true
		}
	}
	return false
}

// writesAt reports whether the instruction at pc of code may write the register r.
func writesAt(code []bytecode.Instr, pc, r int) bool {
	in := &code[pc]
	ops := in.Op.Operands()
	var x *bytecode.Instr
	if ops.Extra {
		x = &code[pc+1]
	}
	return writes(in, x, ops, int32(r))
}

// writes reports whether the instruction in, whose OpExtra is x, may write the register r by
// ops.
func writes(in, x *bytecode.Instr, ops bytecode.Operands, r int32) bool {
	for _, w := range ops.Writes {
		start := operandOf(in, x, w.At)
		if start < 0 {
			continue
		}
		first := start + int32(w.Offset)
		if r < first {
			continue
		}
		if w.Len == bytecode.RegsToEnd {
			return true
		}
		end := first + int32(w.Len)
		if w.Counted {
			for f, k := range [...]bytecode.OperandKind{ops.A, ops.B, ops.C, ops.XA, ops.XB} {
				if k == bytecode.OperandCount {
					end += operandOf(in, x, bytecode.Field(f))
				}
			}
		}
		if r < end {
			return true
		}
	}
	return false
}

// operandOf returns the operand fd of the instruction in, whose OpExtra is x.
func operandOf(in, x *bytecode.Instr, fd bytecode.Field) int32 {
	switch fd {
	case bytecode.FieldA:
		return in.A
	case bytecode.FieldB:
		return in.B
	case bytecode.FieldC:
		return in.C
	case bytecode.FieldXA:
		return x.A
	default:
		return x.B
	}
}

func isTailCall(op bytecode.Opcode) bool {
	switch op {
	case bytecode.OpTailCall, bytecode.OpTailCallUndef, bytecode.OpTailCallSpread, bytecode.OpTailCallEval,
		bytecode.OpTailCallEvalSpread, bytecode.OpTailApplyFwd, bytecode.OpTailCallFwd:
		return true
	}
	return false
}

// hasOpenUpval reports whether a closure or an arguments object shares the register r of the
// frame of s, so that code other than the instruction of s may have changed it.
func (ag *agent) hasOpenUpval(s *execStep, r int) bool {
	ups, index := ag.openUpvals, s.base+r
	if s.gen != nil {
		ups, index = s.gen.openUpvals, r
	}
	_, ok := slices.BinarySearchFunc(ups, index, func(u *upvalue, i int) int {
		return cmp.Compare(u.index, i)
	})
	return ok
}

// failure returns the message of a failed check of the instruction of s.
func (ag *agent) failure(s *execStep, msg string) string {
	in := s.fp.code[s.pc]
	return fmt.Sprintf("ekumavalidate: %s at %d of %s (%s): %s", in.Op, s.pc, s.fp.name, s.fp.File, msg)
}

// sameBits reports whether a and b are the same bits.
func sameBits(a, b Value) bool {
	return a.o == b.o && math.Float64bits(a.n) == math.Float64bits(b.n)
}
