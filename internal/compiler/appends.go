// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// An OpAppend or OpAppendK to a variable may leave in its register a string that the next one
// extends in place. This is right only while nothing else refers to the string, so every
// other read of the register is preceded by an OpSeal, which makes the string an ordinary one.
// The compiler emits the seals once it has compiled an OpAppend to the variable, and settleAppends
// checks the finished code: when a read can see the register as an OpAppend left it, or when code
// can read the register without an instruction that names it, the appends to the variable become
// OpAdds and OpConcatKs again.

// seal emits an OpSeal of the register of b when b has OpAppends, before an instruction that reads
// the register.
func (f *fn) seal(b *binding) {
	if b.appended {
		f.emit(bytecode.OpSeal, b.reg, 0, 0)
	}
}

// sealLocal is seal for the variable name when it is a local of f that can be read without a
// check.
func (f *fn) sealLocal(name string) {
	if b := f.uncheckedLocal(name); b != nil {
		f.seal(b)
	}
}

// appends reports whether an assignment of local + value to the local variable name in the
// register local can be an OpAppend, and prepares for it. The assignment must be one that
// reads the variable after evaluating value.
func (f *fn) appends(name string, local int32, value ast.Expr) bool {
	b := f.uncheckedLocal(name)
	if b == nil || b.reg != local {
		return false
	}
	if b.appended {
		return true
	}
	if f.node == nil || f.eval != nil || f.anyMutated || f.withsAround() > 0 || name == "arguments" {
		return false
	}
	// A variable whose first append may not add a string is left alone, so that seals do not
	// slow down the reads of variables that hold numbers.
	if !stringValued(value) {
		return false
	}
	// An arguments object can read and write a parameter.
	if f.isParam(name) && (f.node.UsesArguments || f.mutated[name]) {
		return false
	}
	// A nested function can read the variable through an upvalue.
	if f.node.Nested != 0 && f.nestedMentions(name) {
		return false
	}
	b.appended = true
	f.appendRegs = append(f.appendRegs, b.reg)
	return true
}

// emitAppend compiles the assignment local = local + value of the local variable that appends
// reported true for, and leaves its value in dst unless dst is -1.
func (f *fn) emitAppend(local int32, value ast.Expr, dst int32) {
	if s, ok := value.(*ast.StringLit); ok {
		f.emit(bytecode.OpAppendK, local, f.constStr(s.Value), 0)
	} else {
		mark := f.top
		r := f.immediateOperand(value)
		f.emit(bytecode.OpAppend, local, r, 0)
		f.free(mark)
	}
	if dst >= 0 && dst != local {
		f.emit(bytecode.OpSeal, local, 0, 0)
		f.emit(bytecode.OpMove, dst, local, 0)
	}
}

// emitConcatK compiles the assignment local = local + s of a local variable that has no
// OpAppends, and leaves its value in dst unless dst is -1.
func (f *fn) emitConcatK(local int32, s *ast.StringLit, dst int32) {
	f.emit(bytecode.OpConcatK, local, local, f.constStr(s.Value))
	if dst >= 0 && dst != local {
		f.emit(bytecode.OpMove, dst, local, 0)
	}
}

// stringValued reports whether the value of e is always a string.
func stringValued(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.StringLit, *ast.TemplateLit:
		return true
	case *ast.BinaryExpr:
		return e.Op == ast.OpAdd && (stringValued(e.Left) || stringValued(e.Right))
	case *ast.CondExpr:
		return stringValued(e.Consequent) && stringValued(e.Alternate)
	}
	return false
}

// nestedMentions reports whether a function or a class nested in f mentions the name.
func (f *fn) nestedMentions(name string) bool {
	found := false
	visit := func(n ast.Node) bool {
		switch n.(type) {
		case *ast.Function, *ast.Class:
			found = found || mentions(n, name)
			return false
		}
		return !found
	}
	for _, p := range f.node.Params {
		walk(p, visit)
	}
	for _, s := range f.node.Body {
		walk(s, visit)
	}
	walk(f.node.ExprBody, visit)
	return found
}

// settleAppends turns the OpAppends and OpAppendKs to a variable back into OpAdds and OpConcatKs
// unless the code of f, which must be complete, reads the register of the variable only where no
// string that an append may extend can be in it.
func (f *fn) settleAppends() {
	if len(f.appendRegs) == 0 {
		return
	}
	regs := f.appendRegs
	f.appendRegs = f.appendRegs[:0]
	code := f.f.Code
	var refused uint64
	if len(regs) > 64 || f.readsByName() {
		refused = ^uint64(0)
	} else {
		refused = f.hiddenReads(regs) | f.unsealedReads(regs)
	}
	if refused == 0 {
		return
	}
	// Bindings of scopes that do not overlap can share a register, which regs then holds more
	// than once.
	isRefused := func(reg int32) bool {
		for k, r := range regs {
			if r == reg && (k >= 64 || refused&(1<<k) != 0) {
				return true
			}
		}
		return false
	}
	for pc := range code {
		in := &code[pc]
		if in.Op != bytecode.OpAppend && in.Op != bytecode.OpAppendK || !isRefused(in.A) {
			continue
		}
		op := bytecode.OpAdd
		if in.Op == bytecode.OpAppendK {
			op = bytecode.OpConcatK
		}
		*in = bytecode.Instr{
			Op: op,
			A:  in.A,
			B:  in.A,
			C:  in.B,
		}
	}
}

// readsByName reports whether the code of f can reach its variables by name, through a direct
// eval or a with statement.
func (f *fn) readsByName() bool {
	for _, in := range f.f.Code {
		switch in.Op {
		case bytecode.OpCallEval, bytecode.OpCallEvalSpread, bytecode.OpTailCallEval, bytecode.OpTailCallEvalSpread,
			bytecode.OpEvalScope, bytecode.OpWith, bytecode.OpResolveName:
			return true
		}
	}
	return false
}

// hiddenReads returns the set of the indices in regs of the registers that code can read without
// an instruction of f that names them: through a closure that captures the register, or for a
// parameter, through the arguments of the frame.
func (f *fn) hiddenReads(regs []int32) uint64 {
	var refused uint64
	for _, p := range f.f.Protos {
		for _, d := range p.Upvals {
			if !d.Local {
				continue
			}
			for k, r := range regs {
				if d.Index == r {
					refused |= 1 << k
				}
			}
		}
	}
	// An arguments object, which can stay mapped to the parameters, or the forwarded arguments
	// read the parameters whenever they are read.
	argsRead := false
	for _, in := range f.f.Code {
		switch in.Op {
		case bytecode.OpArguments, bytecode.OpGetArgsLength, bytecode.OpGetArgsIndex, bytecode.OpGetArgsIndexInt,
			bytecode.OpApplyFwd, bytecode.OpCallFwd, bytecode.OpTailApplyFwd, bytecode.OpTailCallFwd:
			argsRead = true
		}
	}
	if argsRead {
		for k, r := range regs {
			if int(r) < f.f.NumParams {
				refused |= 1 << k
			}
		}
	}
	return refused
}

// unsealedReads returns the set of the indices in regs of the registers that an instruction other
// than an append to the register can read while it may hold a string that an append left.
func (f *fn) unsealedReads(regs []int32) uint64 {
	code := f.f.Code
	handlers := f.f.Handlers
	// in holds, by instruction, the set of the registers that may hold such a string before it.
	in := make([]uint64, len(code))
	seen := make([]bool, len(code))
	var refused uint64
	stack := []int32{0}
	seen[0] = true
	flow := func(pc int, state uint64) {
		if pc >= len(code) {
			return
		}
		if seen[pc] && in[pc]|state == in[pc] {
			return
		}
		in[pc] |= state
		seen[pc] = true
		stack = append(stack, int32(pc))
	}
	var s copyScratch
	for len(stack) > 0 {
		pc := int(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		inst := code[pc]
		if inst.Op == bytecode.OpExtra {
			continue
		}
		fl := &opFlows[inst.Op]
		before := in[pc]
		after := before
		for k, r := range regs {
			bit := uint64(1) << k
			switch {
			case inst.Op == bytecode.OpSeal && inst.A == r:
				after &^= bit
				continue
			case (inst.Op == bytecode.OpAppend || inst.Op == bytecode.OpAppendK) && inst.A == r:
				if before&bit != 0 && inst.Op == bytecode.OpAppend && inst.B == r {
					refused |= bit
				}
				after |= bit
				continue
			}
			// An OpCloseUpvals reads only the registers that upvalues refer to, which
			// hiddenReads refuses.
			if before&bit != 0 && (inst.Op != bytecode.OpCloseUpvals && bytecode.MayRead(code, pc, int(r)) || inst.Op == bytecode.OpRestArgs && int(r) < f.f.NumParams) {
				refused |= bit
			}
			if definitelyWrites(code, pc, fl, r) {
				after &^= bit
			}
		}
		if fl.flow != bytecode.FlowEnd {
			flow(s.next(code, pc), after)
		}
		if fl.flow == bytecode.FlowNextOrSkip {
			flow(pc+2, after)
		}
		if fl.targets != 0 {
			for _, t := range s.targets(code, pc, fl) {
				if t < 0 {
					break
				}
				flow(int(t), after)
			}
		}
		for _, h := range handlers {
			if h.Covers(pc) {
				// An instruction that throws may have written its registers or not.
				flow(int(h.Target), before|after)
			}
		}
	}
	return refused
}

// definitelyWrites reports whether the instruction at pc of code, whose flow is fl, writes the
// register r whenever it continues in the function.
func definitelyWrites(code []bytecode.Instr, pc int, fl *opFlow, r int32) bool {
	// An instruction that can jump may do so without the write, and a call writes the registers
	// from its arguments on only when the frame of the callee takes their place.
	if fl.targets != 0 {
		return false
	}
	for _, w := range fl.writes {
		if w.Len == bytecode.RegsToEnd {
			continue
		}
		if first, end := writtenRegs(code, pc, fl, w, math.MaxInt32-1); first <= r && r < end {
			return true
		}
	}
	return false
}
