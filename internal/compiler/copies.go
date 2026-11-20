// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// copyCandidate is a variable that a closure captures as a shared upvalue.
type copyCandidate struct {
	// fn is the code of the closure, and index is the index of the upvalue in fn.Upvals.
	fn    *bytecode.Func
	index int32

	// reg is the register of the variable, whose binding in the enclosing function is b.
	reg int32
	b   *binding
}

// opFlow is what promoteCopies needs to know about an opcode, from [bytecode.Opcode.Operands]
// and [bytecode.Opcode.Flow].
type opFlow struct {
	// writes are the runs of registers that the instruction writes. headA says that the first
	// of them is the register a alone, and rest holds the runs after it, or all of writes.
	writes []bytecode.Regs
	headA  bool
	rest   []bytecode.Regs

	// targets and counts are the fields that hold jump targets and register counts, by bit.
	// target is the field of the target when there is one.
	targets uint8
	counts  uint8
	target  bytecode.Field

	extra bool
	flow  bytecode.Flow

	// special says that the opcode creates a closure or reaches variables by name.
	special bool
}

var opFlows [256]opFlow

// The bits of opScans.
const (
	// scanEnd says that the flow is [bytecode.FlowEnd].
	scanEnd = 1 << iota

	// scanHeadA says that opFlow.headA is set.
	scanHeadA

	// scanSlow says that opFlow.rest, opFlow.targets or opFlow.special is set.
	scanSlow
)

// opScans holds, by opcode, what the inner loop of settle needs to know.
var opScans [256]uint8

var flowFields = [...]bytecode.Field{bytecode.FieldA, bytecode.FieldB, bytecode.FieldC, bytecode.FieldXA, bytecode.FieldXB}

func init() {
	for op := range opFlows {
		o := bytecode.Opcode(op)
		flow := o.Flow()
		if flow == 0 {
			// Not an opcode.
			continue
		}
		ops := o.Operands()
		fl := opFlow{
			writes:  ops.Writes,
			rest:    ops.Writes,
			extra:   ops.Extra,
			flow:    flow,
			special: o == bytecode.OpClosure || o == bytecode.OpWith || o == bytecode.OpResolveName,
		}
		if len(ops.Writes) > 0 && ops.Writes[0] == (bytecode.Regs{At: bytecode.FieldA, Len: 1}) {
			fl.headA = true
			fl.rest = ops.Writes[1:]
		}
		for i, k := range [...]bytecode.OperandKind{ops.A, ops.B, ops.C, ops.XA, ops.XB} {
			switch k {
			case bytecode.OperandTarget:
				fl.targets |= 1 << i
				fl.target = flowFields[i]
			case bytecode.OperandCount:
				fl.counts |= 1 << i
			}
		}
		opFlows[op] = fl

		if flow == bytecode.FlowEnd {
			opScans[op] |= scanEnd
		}
		if fl.headA {
			opScans[op] |= scanHeadA
		}
		if len(fl.rest) > 0 || fl.targets != 0 || fl.special {
			opScans[op] |= scanSlow
		}
	}
}

// The states of a candidate in promoteCopies.
const (
	// copyUnseen is a candidate whose closure no instruction creates.
	copyUnseen uint8 = iota

	// copyOpen is a candidate that only a search decides.
	copyOpen

	copyFinal
	copyShared
)

// maxSearches is the most creations of closures that promoteCopies searches from in one
// function.
const maxSearches = 8

// copyRun is the candidates from lo to hi of the closure fn.
type copyRun struct {
	fn     *bytecode.Func
	lo, hi int

	// next is the index in copyScratch.runs of the next run of fn, or -1.
	next int32
}

// copyWrites is the first and the last instruction found to write a register, or
// math.MaxInt32 and -1.
type copyWrites struct {
	next, last int32
}

// copyScratch is the storage of promoteCopies that compilations reuse.
type copyScratch struct {
	// states holds the state of every candidate.
	states []uint8

	// runs holds the candidates of each closure, which are next to each other, and protoRuns
	// holds, by index in [bytecode.Func.Protos], the index in runs of the first run of the
	// closure, or -1.
	runs      []copyRun
	protoRuns []int32

	// top is the highest register of the candidates.
	top int32

	// sites holds the indices of the instructions that create closures whose candidates settle
	// left open there.
	sites []int32

	// writes holds, by register up to top, the instructions found to write the register.
	writes []copyWrites

	// finals and opens hold the indices of the candidates that settle made final or left open.
	// A candidate in them may have changed its state since.
	finals []int32
	opens  []int32

	// checked holds, by register up to top, the binding whose name was last looked up for the
	// register, and nameOK whether no arguments object or nested function can change a variable
	// of that name.
	checked []*binding
	nameOK  []bool

	// For search, wanted holds, by register up to top, 1 for a register that the search looks
	// for and 2 for one that it found written. seen marks the instructions that the search
	// reached, and stack holds those whose successors it has not visited yet.
	wanted []uint8
	seen   []uint64
	stack  []int32
}

func (s *copyScratch) trim() {
	// Some buffers grow with the code or the registers, the others with the candidates or the
	// closures.
	code := max(cap(s.writes), cap(s.sites), cap(s.seen)*64, cap(s.stack))
	cands := max(cap(s.states), cap(s.runs), cap(s.protoRuns))
	if code > maxFreeCode || cands > maxFreeCands {
		*s = copyScratch{}
	}
}

// promoteCopies makes the upvalues of f.copyCands copies of their variables when no write to
// the variable can run after any creation of the closure. The code of f must be complete.
func (f *fn) promoteCopies() {
	if len(f.copyCands) > 0 && f.node != nil && !f.anyMutated {
		f.promoteCands(f.copyCands)
	}
}

// promoteCands does the work of promoteCopies for the candidates cands of f.
func (f *fn) promoteCands(cands []copyCandidate) {
	code := f.f.Code
	first := -1
	for pc := range code {
		op := code[pc].Op
		if op == bytecode.OpWith || op == bytecode.OpResolveName {
			// A dynamic scope can reach the variables by name.
			return
		}
		if op == bytecode.OpClosure {
			first = pc
			break
		}
	}
	if first < 0 {
		return
	}

	s := &f.c.scratch.copies
	s.setCands(cands, f.f.Protos)
	// The runs refer to the code of the closures, which the scratch must not keep.
	defer func() {
		clear(s.runs)
		s.runs = s.runs[:0]
	}()
	open := s.settle(code, f.f.Handlers, cands, first)
	if open < 0 {
		return
	}
	f.promote(cands, s.finals, copyFinal)
	if open == 0 {
		return
	}
	if len(s.sites) > maxSearches {
		// Every search can go over all the code, so their number is bounded. The open
		// candidates stay shared.
		return
	}
	s.wanted = resize(s.wanted, int(s.top)+1)
	clear(s.wanted)
	for _, pc := range s.sites {
		s.search(code, f.f.Handlers, cands, pc)
	}
	f.promote(cands, s.opens, copyOpen)
}

// promote makes copies of the upvalues of the candidates of list that are in the state state,
// unless an arguments object or a nested function can change their variables.
func (f *fn) promote(cands []copyCandidate, list []int32, state uint8) {
	s := &f.c.scratch.copies
	s.checked = resize(s.checked, int(s.top)+1)
	s.nameOK = resize(s.nameOK, int(s.top)+1)
	for _, i := range list {
		if s.states[i] != state {
			continue
		}
		c := &cands[i]
		r := c.reg
		if s.checked[r] != c.b {
			// An arguments object or a nested function can change the variable without an
			// instruction of f naming its register. Variables of different scopes can share a
			// register.
			s.checked[r] = c.b
			s.nameOK[r] = !f.mutated[c.b.name] && !f.nestedAssigns(c.b.name)
		}
		if s.nameOK[r] {
			c.fn.Upvals[c.index].ByValue = true
		}
	}
	for _, i := range list {
		s.checked[cands[i].reg] = nil
	}
}

// setCands sets s.runs and s.protoRuns to the runs of cands and the closures of protos, and
// s.top to the highest register of cands.
func (s *copyScratch) setCands(cands []copyCandidate, protos []*bytecode.Func) {
	runs := s.runs[:0]
	lo := 0
	top := int32(0)
	for i := range cands {
		c := &cands[i]
		top = max(top, c.reg)
		if c.fn != cands[lo].fn {
			runs = append(runs, copyRun{
				fn:   cands[lo].fn,
				lo:   lo,
				hi:   i,
				next: -2,
			})
			lo = i
		}
	}
	s.runs = append(runs, copyRun{
		fn:   cands[lo].fn,
		lo:   lo,
		hi:   len(cands),
		next: -2,
	})
	s.top = top

	s.protoRuns = resize(s.protoRuns, len(protos))
	for k := range s.protoRuns {
		s.protoRuns[k] = -1
	}
	// The runs usually come in the order of protos.
	j := 0
	for k, p := range protos {
		if j < len(s.runs) && s.runs[j].fn == p {
			s.runs[j].next = -1
			s.protoRuns[k] = int32(j)
			j++
		}
	}
	for j := range s.runs {
		r := &s.runs[j]
		if r.next != -2 {
			continue
		}
		r.next = -1
		if k := slices.Index(protos, r.fn); k >= 0 {
			r.next = s.protoRuns[k]
			s.protoRuns[k] = int32(j)
		}
	}
}

// settle sets s.states, s.sites, s.finals and s.opens from code, whose first creation of a
// closure is at first. It decides the candidates that the order of the code decides, and
// leaves open the others whose closure is created. settle returns the number of open
// candidates, or -1 when a dynamic scope can reach the variables by name.
func (s *copyScratch) settle(code []bytecode.Instr, handlers []bytecode.Handler, cands []copyCandidate, first int) int {
	writes := resize(s.writes, int(s.top)+1)
	s.writes = writes
	for r := range writes {
		writes[r] = copyWrites{
			next: math.MaxInt32,
			last: -1,
		}
	}
	states := resize(s.states, len(cands))
	s.states = states
	clear(states)
	s.sites = s.sites[:0]
	s.finals = s.finals[:0]
	s.opens = s.opens[:0]

	// reach is the lowest target of the handlers and of the jumps found so far that go
	// backwards.
	reach := int32(math.MaxInt32)
	for _, h := range handlers {
		if h.End <= h.Start {
			continue
		}
		// A throw continues at the target of the handler, which receives the thrown value in its
		// register.
		if r := h.Reg; uint32(r) < uint32(len(writes)) {
			writes[r].last = max(writes[r].last, h.End-1)
		}
		if h.Target < h.End {
			reach = min(reach, h.Target)
		}
	}

	// The code goes in reverse order, so that writes is known for the instructions from pc on.
	// end is the first instruction from pc on whose flow is [bytecode.FlowEnd].
	top := s.top
	end := int32(len(code))
	for pc := len(code) - 1; pc >= first; pc-- {
		// The instructions that do not need the rest of the loop take the inner loop, which
		// calls nothing.
		for ; pc >= first; pc-- {
			in := &code[pc]
			k := opScans[in.Op]
			if k&scanEnd != 0 {
				end = int32(pc)
			}
			if k&scanHeadA != 0 && uint32(in.A) < uint32(len(writes)) {
				w := &writes[in.A]
				w.next = int32(pc)
				w.last = max(w.last, int32(pc))
			}
			if k&scanSlow != 0 {
				break
			}
		}
		if pc < first {
			break
		}
		in := &code[pc]
		fl := &opFlows[in.Op]
		for _, r := range fl.rest {
			if r.At <= bytecode.FieldC && operandOf(in, nil, r.At)+int32(r.Offset) > top {
				continue
			}
			from, to := writtenRegs(code, pc, fl, r, top)
			for k := from; k < to; k++ {
				w := &writes[k]
				w.next = int32(pc)
				w.last = max(w.last, int32(pc))
			}
		}
		if fl.targets != 0 {
			for _, t := range s.targets(code, pc, fl) {
				if t < 0 {
					break
				}
				if int(t) <= pc {
					reach = min(reach, t)
				}
			}
		}
		if !fl.special {
			continue
		}
		if in.Op != bytecode.OpClosure {
			// A dynamic scope can reach the variables by name.
			return -1
		}
		open := false
		for j := s.protoRuns[in.B]; j >= 0; j = s.runs[j].next {
			r := &s.runs[j]
			for i := r.lo; i < r.hi; i++ {
				st := states[i]
				if st == copyShared {
					continue
				}
				// A write up to end runs after the creation. Without a write from the
				// creation on and anything going back to before it, nothing can write the
				// register after the creation.
				switch w := writes[cands[i].reg]; {
				case w.next <= end:
					states[i] = copyShared
				case w.last < int32(pc) && reach >= int32(pc):
					if st == copyUnseen {
						states[i] = copyFinal
						s.finals = append(s.finals, int32(i))
					}
				default:
					open = true
					if st != copyOpen {
						states[i] = copyOpen
						s.opens = append(s.opens, int32(i))
					}
				}
			}
		}
		if open {
			s.sites = append(s.sites, int32(pc))
		}
	}
	return len(s.opens)
}

// search decides the open candidates of the closure that the instruction at site of code
// creates. A candidate is shared when an instruction that can run after the creation writes
// its register. s.wanted must be zero, and search leaves it so.
func (s *copyScratch) search(code []bytecode.Instr, handlers []bytecode.Handler, cands []copyCandidate, site int32) {
	wanted := s.wanted
	n := 0
	for j := s.protoRuns[code[site].B]; j >= 0; j = s.runs[j].next {
		r := &s.runs[j]
		for i := r.lo; i < r.hi; i++ {
			if s.states[i] == copyOpen && wanted[cands[i].reg] == 0 {
				wanted[cands[i].reg] = 1
				n++
			}
		}
	}
	if n == 0 {
		return
	}

	seen := resize(s.seen, (len(code)+63)/64)
	s.seen = seen
	clear(seen)
	stack := s.stack[:0]
	push := func(pc int32) {
		if seen[pc>>6]&(1<<(pc&63)) == 0 {
			seen[pc>>6] |= 1 << (pc & 63)
			stack = append(stack, pc)
		}
	}
	push(site)
	for n > 0 && len(stack) > 0 {
		pc := int(stack[len(stack)-1])
		stack = stack[:len(stack)-1]
		in := &code[pc]
		if in.Op == bytecode.OpExtra {
			continue
		}
		fl := &opFlows[in.Op]
		for _, r := range fl.writes {
			from, to := writtenRegs(code, pc, fl, r, s.top)
			for k := from; k < to; k++ {
				if wanted[k] == 1 {
					wanted[k] = 2
					n--
				}
			}
		}
		if fl.flow != bytecode.FlowEnd {
			if next := s.next(code, pc); next < len(code) {
				push(int32(next))
			}
		}
		if fl.flow == bytecode.FlowNextOrSkip && pc+2 < len(code) {
			push(int32(pc + 2))
		}
		if fl.targets != 0 {
			for _, t := range s.targets(code, pc, fl) {
				if t < 0 {
					break
				}
				push(t)
			}
		}
		for _, h := range handlers {
			if pc < int(h.Start) || pc >= int(h.End) {
				continue
			}
			// A throw continues at the target of the handler, which receives the thrown value
			// in its register.
			push(h.Target)
			if r := h.Reg; uint32(r) < uint32(len(wanted)) && wanted[r] == 1 {
				wanted[r] = 2
				n--
			}
		}
	}
	s.stack = stack

	for j := s.protoRuns[code[site].B]; j >= 0; j = s.runs[j].next {
		r := &s.runs[j]
		for i := r.lo; i < r.hi; i++ {
			if s.states[i] == copyOpen && wanted[cands[i].reg] == 2 {
				s.states[i] = copyShared
			}
		}
	}
	for j := s.protoRuns[code[site].B]; j >= 0; j = s.runs[j].next {
		r := &s.runs[j]
		for i := r.lo; i < r.hi; i++ {
			wanted[cands[i].reg] = 0
		}
	}
}

// next returns the index of the instruction after the one at pc of code.
func (s *copyScratch) next(code []bytecode.Instr, pc int) int {
	if opFlows[code[pc].Op].extra {
		return pc + 2
	}
	return pc + 1
}

// targets returns the jump targets of the instruction at pc of code, whose flow is fl, followed
// by -1 when there are fewer than three.
func (s *copyScratch) targets(code []bytecode.Instr, pc int, fl *opFlow) [3]int32 {
	in := &code[pc]
	var x *bytecode.Instr
	if fl.extra {
		x = &code[pc+1]
	}
	ts := [3]int32{-1, -1, -1}
	if fl.targets&(fl.targets-1) == 0 {
		ts[0] = operandOf(in, x, fl.target)
		return ts
	}
	k := 0
	for i, fd := range flowFields {
		if fl.targets&(1<<i) != 0 {
			ts[k] = operandOf(in, x, fd)
			k++
		}
	}
	return ts
}

// writtenRegs returns the registers from first to end, none of them above top, that the
// instruction at pc of code, whose flow is fl, writes by its run r.
func writtenRegs(code []bytecode.Instr, pc int, fl *opFlow, r bytecode.Regs, top int32) (first, end int32) {
	in := &code[pc]
	var x *bytecode.Instr
	if fl.extra {
		x = &code[pc+1]
	}
	start := operandOf(in, x, r.At)
	if start < 0 {
		return 0, 0
	}
	first = start + int32(r.Offset)
	if first > top {
		return 0, 0
	}
	if r.Len == bytecode.RegsToEnd {
		return first, top + 1
	}
	end = first + int32(r.Len)
	if r.Counted {
		for i, fd := range flowFields {
			if fl.counts&(1<<i) != 0 {
				end += operandOf(in, x, fd)
			}
		}
	}
	return first, min(end, top+1)
}

func resize[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	return s[:n]
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
