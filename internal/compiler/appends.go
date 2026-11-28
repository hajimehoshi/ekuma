// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math"
	"math/bits"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// Appending or prepending to a variable can leave a string in its register that the next
// instruction growing it may extend in place. An OpSeal precedes other reads that can retain
// a reference to the string. Reading its length needs no seal. settleAppends disables in-place
// growth when the finished code can read the string through a hidden or unsealed reference.

// seal shares the string in b before an instruction can retain a reference to it.
func (f *fn) seal(b *binding) {
	if b.appended && !b.loopSealed {
		f.emit(bytecode.OpSeal, b.reg, 0, 0)
	}
}

// sealLoop emits, before the loop s, an OpSeal of each variable with OpAppends that s mentions but
// does not append to, so that its reads in s need no seals. It returns the mark to pass to
// endLoop after s.
func (f *fn) sealLoop(s ast.Stmt) int {
	mark := len(f.loopSeals)
	for _, b := range f.appendBindings {
		if b.loopSealed || f.uncheckedLocal(b.name) != b {
			continue
		}
		if mentioned, appended, ok := f.loopUse(s, b.name); !ok || !mentioned || appended {
			continue
		}
		f.emit(bytecode.OpSeal, b.reg, 0, 0)
		b.loopSealed = true
		f.loopSeals = append(f.loopSeals, b)
	}
	return mark
}

// endLoop ends what sealLoop started for the loop that returned mark.
func (f *fn) endLoop(mark int) {
	for _, b := range f.loopSeals[mark:] {
		b.loopSealed = false
	}
	clear(f.loopSeals[mark:])
	f.loopSeals = f.loopSeals[:mark]
}

// maxLoopScanNodes is the number of nodes that the walks of loopUse visit at most in a function,
// so that the walks of nested loops stay linear in the size of the function.
const maxLoopScanNodes = 1 << 16

// loopScan is the state of the walks of loopUse in a function.
type loopScan struct {
	name      string
	mentioned bool
	appended  bool

	// nodes is the number of nodes visited so far in the function.
	nodes int

	// visit is the function that walks the nodes, made once.
	visit func(ast.Node) bool
}

// loopUse reports whether n mentions the variable name, and whether it has an assignment to the
// variable that can be an append. ok is false when the function walked too many nodes to tell.
func (f *fn) loopUse(n ast.Node, name string) (mentioned, appended, ok bool) {
	ls := &f.loopScan
	if ls.nodes >= maxLoopScanNodes {
		return false, false, false
	}
	if ls.visit == nil {
		ls.visit = ls.node
	}
	ls.name = name
	ls.mentioned = false
	ls.appended = false
	walk(n, ls.visit)
	return ls.mentioned, ls.appended, ls.nodes < maxLoopScanNodes
}

func (ls *loopScan) node(n ast.Node) bool {
	ls.nodes++
	if ls.appended || ls.nodes >= maxLoopScanNodes {
		return false
	}
	switch n := n.(type) {
	case *ast.Identifier:
		ls.mentioned = ls.mentioned || n.Name == ls.name
	case *ast.AssignExpr:
		id, ok := n.Target.(*ast.Identifier)
		if !ok || id.Name != ls.name {
			break
		}
		switch n.Op {
		case ast.OpAdd:
			ls.appended = true
		case ast.OpNone:
			if v, ok := n.Value.(*ast.BinaryExpr); ok && v.Op == ast.OpAdd {
				l, lok := v.Left.(*ast.Identifier)
				r, rok := v.Right.(*ast.Identifier)
				ls.appended = lok && l.Name == ls.name || rok && r.Name == ls.name
			}
		}
	}
	return !ls.appended
}

// sealLocal is seal for the variable name when it is a local of f that can be read without a
// check.
func (f *fn) sealLocal(name string) {
	if b := f.uncheckedLocal(name); b != nil {
		f.seal(b)
	}
}

// appends prepares b for in-place string growth. The caller must preserve operand evaluation order.
func (f *fn) appends(b *binding, value ast.Expr) bool {
	if b.appended {
		return true
	}
	name := b.name
	if f.node == nil || f.eval != nil || f.anyMutated || f.withsAround() > 0 || name == "arguments" {
		return false
	}
	// A variable whose first append may not add a string is left alone, so that seals do not
	// slow down the reads of variables that hold numbers.
	if !stringValued(value) {
		return false
	}
	// An append outside the loops of the function runs once each time the code runs, so that
	// extending its result in place gains little, and the reads of the variable would need seals.
	if !f.inLoop() {
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
	f.appendBindings = append(f.appendBindings, b)
	return true
}

// inLoop reports whether the code being compiled is in a loop of f.
func (f *fn) inLoop() bool {
	for _, c := range f.control {
		if c.isLoop {
			return true
		}
	}
	return false
}

// lengthOf returns the local variable with appends whose length n reads without an optional
// chain, or nil.
func (f *fn) lengthOf(n *ast.MemberExpr) *binding {
	if len(f.appendRegs) == 0 {
		return nil
	}
	id, ok := n.Object.(*ast.Identifier)
	if !ok || n.Computed || n.Optional || n.Property.(*ast.Identifier).Name != "length" {
		return nil
	}
	if b := f.uncheckedLocal(id.Name); b != nil && b.appended {
		return b
	}
	return nil
}

// emitStringGrowth compiles an append or prepend to local and leaves its value in dst if needed.
func (f *fn) emitStringGrowth(local int32, value ast.Expr, dst int32, op bytecode.Opcode) {
	if s, ok := value.(*ast.StringLit); ok {
		if op == bytecode.OpAppend {
			op = bytecode.OpAppendK
		} else {
			op = bytecode.OpPrependK
		}
		f.emit(op, local, f.constStr(s.Value), 0)
	} else {
		mark := f.top
		r := f.immediateOperand(value)
		f.emit(op, local, r, 0)
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

// settleAppends disables in-place string growth for registers with unsafe reads in the
// completed code of f.
func (f *fn) settleAppends() {
	if len(f.appendRegs) == 0 {
		return
	}
	regs := f.appendRegs
	f.appendRegs = f.appendRegs[:0]
	clear(f.appendBindings)
	f.appendBindings = f.appendBindings[:0]
	code := f.f.Code
	var refused uint64
	if len(regs) > 64 || f.readsByName() {
		refused = ^uint64(0)
	} else {
		unsealed, _ := f.exclusiveReads(regs, false)
		refused = f.hiddenReads(regs) | unsealed
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
		switch in.Op {
		case bytecode.OpAppend, bytecode.OpAppendK, bytecode.OpPrepend, bytecode.OpPrependK:
			if !isRefused(in.A) {
				continue
			}
		default:
			continue
		}
		switch in.Op {
		case bytecode.OpAppend:
			in.Op, in.B, in.C = bytecode.OpAdd, in.A, in.B
		case bytecode.OpAppendK:
			in.Op, in.B, in.C = bytecode.OpConcatK, in.A, in.B
		case bytecode.OpPrepend:
			in.Op, in.C = bytecode.OpAdd, in.A
		case bytecode.OpPrependK:
			in.C = 1
		}
	}
}

func (f *fn) canAccumulate(reg int32) bool {
	// Legacy function.arguments can read parameters even when the function never uses arguments.
	if len(f.accumRegs) > 64 || int(reg) < f.f.NumParams || f.node == nil || f.eval != nil ||
		f.anyMutated || f.withsAround() > 0 || !f.inLoop() {
		return false
	}
	if !slices.Contains(f.accumRegs, reg) {
		f.accumRegs = append(f.accumRegs, reg)
	}
	return true
}

// settleAccums disallows ownership of registers with hidden readers and seals values before
// explicit escapes. Registers may be reused by bindings with disjoint scopes.
func (f *fn) settleAccums() {
	regs := f.accumRegs
	if len(regs) == 0 {
		return
	}
	var refused uint64
	var seals []uint64
	allRefused := len(regs) > 64 || f.readsByName()
	if !allRefused {
		refused, seals = f.exclusiveReads(regs, true)
		refused |= f.hiddenReads(regs)
	}
	if allRefused || refused != 0 {
		for pc := range f.f.Code {
			in := &f.f.Code[pc]
			if in.Accumulate && (allRefused || refused&(1<<slices.Index(regs, in.A)) != 0) {
				in.Accumulate = false
			}
		}
	}
	f.insertAccumSeals(regs, seals, refused)
}

func (f *fn) insertAccumSeals(regs []int32, seals []uint64, refused uint64) {
	var count int
	for _, mask := range seals {
		count += bits.OnesCount64(mask &^ refused)
	}
	if count == 0 {
		return
	}
	b := f.f
	code := make([]bytecode.Instr, 0, len(b.Code)+count)
	// A jump to an instruction must run any seals immediately before it.
	newPCs := make([]int32, len(b.Code)+1)
	for pc, in := range b.Code {
		newPCs[pc] = int32(len(code))
		for mask := seals[pc] &^ refused; mask != 0; mask &= mask - 1 {
			code = append(code, bytecode.Instr{
				Op: bytecode.OpSeal,
				A:  regs[bits.TrailingZeros64(mask)],
			})
		}
		code = append(code, in)
	}
	newPCs[len(b.Code)] = int32(len(code))
	for pc := range code {
		in := &code[pc]
		mask := opFlows[in.Op].targets
		for ; mask != 0; mask &= mask - 1 {
			switch flowFields[bits.TrailingZeros8(mask)] {
			case bytecode.FieldA:
				in.A = newPCs[in.A]
			case bytecode.FieldB:
				in.B = newPCs[in.B]
			case bytecode.FieldC:
				in.C = newPCs[in.C]
			case bytecode.FieldXA:
				x := &code[pc+1]
				x.A = newPCs[x.A]
			case bytecode.FieldXB:
				x := &code[pc+1]
				x.B = newPCs[x.B]
			}
		}
	}
	for i := range b.Handlers {
		h := &b.Handlers[i]
		h.Start, h.End, h.Target = newPCs[h.Start], newPCs[h.End], newPCs[h.Target]
	}
	for i := range b.Positions {
		b.Positions[i].PC = newPCs[b.Positions[i].PC]
	}
	b.Code = code
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

// exclusiveReads tracks registers that may own an exclusive value. A string must already
// have a seal at an escaping read. For a BigInt, record where to insert a seal, or refuse
// ownership when an instruction must stay adjacent to its predecessor.
func (f *fn) exclusiveReads(regs []int32, bigint bool) (refused uint64, seals []uint64) {
	code := f.f.Code
	handlers := f.f.Handlers
	// in holds, by instruction, the set of the registers that may own a value before it.
	in := make([]uint64, len(code))
	seen := make([]bool, len(code))
	if bigint {
		seals = make([]uint64, len(code))
	}
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
		grows := inst.Op == bytecode.OpAppend || inst.Op == bytecode.OpAppendK ||
			inst.Op == bytecode.OpPrepend || inst.Op == bytecode.OpPrependK && inst.C == 0
		if bigint {
			grows = inst.Accumulate
		}
		before := in[pc]
		after := before
		for k, r := range regs {
			bit := uint64(1) << k
			switch {
			case inst.Op == bytecode.OpSeal && inst.A == r:
				after &^= bit
				continue
			case grows && inst.A == r:
				if !bigint && before&bit != 0 && (inst.Op == bytecode.OpAppend || inst.Op == bytecode.OpPrepend) && inst.B == r {
					refused |= bit
				}
				after |= bit
				continue
			}
			if before&bit != 0 && f.escapesExclusive(pc, r, bigint) {
				if bigint && canInsertSeal(code, pc) {
					seals[pc] |= bit
					after &^= bit
				} else {
					refused |= bit
				}
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
	return refused, seals
}

func (f *fn) escapesExclusive(pc int, reg int32, bigint bool) bool {
	code := f.f.Code
	op := code[pc].Op
	switch {
	case op == bytecode.OpCloseUpvals:
		// hiddenReads already refuses any register an upvalue refers to.
		return false
	case op == bytecode.OpRestArgs:
		return int(reg) < f.f.NumParams
	case !bigint && op == bytecode.OpGetLength:
		return false
	case bigint && readsBigIntWithoutEscape(op):
		return false
	}
	return bytecode.MayRead(code, pc, int(reg))
}

func readsBigIntWithoutEscape(op bytecode.Opcode) bool {
	// Addition and subtraction can return a zero operand's counterpart. The engine
	// seals it when the destination does not own that reference.
	switch {
	case op >= bytecode.OpAdd && op <= bytecode.OpUShrK,
		op >= bytecode.OpLt && op <= bytecode.OpStrictNe,
		op >= bytecode.OpJumpIf && op <= bytecode.OpLoopNotNullish:
		return true
	}
	switch op {
	case bytecode.OpMulAdd, bytecode.OpNeg, bytecode.OpNot, bytecode.OpBitNot, bytecode.OpTypeof,
		bytecode.OpToString, bytecode.OpInc, bytecode.OpDec, bytecode.OpCheckTDZ:
		return true
	}
	return false
}

func canInsertSeal(code []bytecode.Instr, pc int) bool {
	if pc == 0 {
		return true
	}
	prev := code[pc-1].Op
	if opFlows[prev].flow == bytecode.FlowNextOrSkip || prev == bytecode.OpCopyPropToGlobal {
		return false
	}
	return pc < 2 || prev != bytecode.OpExtra || code[pc-2].Op != bytecode.OpIterNextPair
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
