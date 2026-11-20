// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"math/bits"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// copyCandidate is a variable that a closure captures as a shared upvalue.
type copyCandidate struct {
	// fn is the code of the closure, and index is the index of the upvalue in fn.Upvals.
	fn    *bytecode.Func
	index int32

	// b is the binding of the variable in the enclosing function.
	b *binding
}

// opFlow is what promoteCopies needs to know about an opcode, from [bytecode.Opcode.Operands]
// and [bytecode.Opcode.Flow].
type opFlow struct {
	// writes are the runs of registers that the instruction writes. writesA says that they are
	// the register a alone.
	writes  []bytecode.Regs
	writesA bool

	// targets and counts are the fields that hold jump targets and register counts, by bit.
	// target is the field of the target when there is one.
	targets uint8
	counts  uint8
	target  bytecode.Field

	extra bool
	flow  bytecode.Flow

	// plain says that the instruction writes at most the register a, has no jump targets and
	// no OpExtra, and continues with the next instruction.
	plain bool
}

var opFlows [256]opFlow

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
			writesA: len(ops.Writes) == 1 && ops.Writes[0] == bytecode.Regs{At: bytecode.FieldA, Len: 1},
			extra:   ops.Extra,
			flow:    flow,
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
		fl.plain = (len(fl.writes) == 0 || fl.writesA) && fl.targets == 0 && !fl.extra && fl.flow == bytecode.FlowNext
		opFlows[op] = fl
	}
}

// The states of a candidate in promoteCopies.
const (
	copyUnseen uint8 = iota
	copyFinal
	copyShared
)

// copyRun is the candidates from lo to hi of the closure fn.
type copyRun struct {
	fn     *bytecode.Func
	lo, hi int
}

// copyScratch is the storage of promoteCopies that compilations reuse.
type copyScratch struct {
	// states holds the state of every candidate.
	states []uint8

	// runs holds the candidates of each closure, which are next to each other.
	runs []copyRun

	// sites holds the indices of the instructions that create closures.
	sites []int32

	// regs holds the registers of the candidates in increasing order, and present marks them
	// by register. regBit holds the index in regs of every register that present marks.
	regs    []int32
	present []uint64
	regBit  []int32

	// checked holds, by index in regs, the binding whose name was last looked up for the
	// register, and nameOK whether no arguments object or nested function can change a variable
	// of that name.
	checked []*binding
	nameOK  []bool

	// after[pc] holds, in a set of bits by index in regs, the registers that the instruction at
	// pc writes or that an instruction that can run after it writes. A set takes words words,
	// and mask holds one.
	after []uint64
	mask  []uint64
	words int
}

func (s *copyScratch) trim() {
	if cap(s.after) > maxFreeCode {
		*s = copyScratch{}
	}
}

// bit returns the index in s.regs of the register r, or -1.
func (s *copyScratch) bit(r int32) int {
	if r < s.regs[0] || r > s.regs[len(s.regs)-1] || s.present[r>>6]&(1<<(r&63)) == 0 {
		return -1
	}
	return int(s.regBit[r])
}

// promoteCopies makes the upvalues of f.copyCands copies of their variables when no write to
// the variable can run after any creation of the closure. The code of f must be complete.
func (f *fn) promoteCopies() {
	cands := f.copyCands
	if len(cands) == 0 || f.node == nil || f.anyMutated {
		return
	}
	code := f.f.Code
	s := &f.c.scratch.copies
	s.sites = s.sites[:0]
	for pc := range code {
		switch code[pc].Op {
		case bytecode.OpWith, bytecode.OpResolveName:
			// A dynamic scope can reach the variables by name.
			return
		case bytecode.OpClosure:
			s.sites = append(s.sites, int32(pc))
		}
	}
	if len(s.sites) == 0 {
		return
	}

	s.present = resize(s.present, (f.f.NumRegs+63)/64)
	clear(s.present)
	defer func() {
		clear(s.runs)
		s.runs = s.runs[:0]
	}()
	for i, c := range cands {
		s.present[c.b.reg>>6] |= 1 << (c.b.reg & 63)
		if len(s.runs) > 0 && s.runs[len(s.runs)-1].fn == c.fn {
			s.runs[len(s.runs)-1].hi = i + 1
			continue
		}
		s.runs = append(s.runs, copyRun{fn: c.fn, lo: i, hi: i + 1})
	}
	s.regBit = resize(s.regBit, f.f.NumRegs)
	s.regs = s.regs[:0]
	for k, m := range s.present {
		for ; m != 0; m &= m - 1 {
			r := int32(k*64 + bits.TrailingZeros64(m))
			s.regBit[r] = int32(len(s.regs))
			s.regs = append(s.regs, r)
		}
	}
	w := (len(s.regs) + 63) / 64
	s.words = w
	s.after = resize(s.after, len(code)*w)
	clear(s.after)
	if w == 1 {
		s.flowOneWord(code, f.f.Handlers, int(s.sites[0]))
	} else {
		s.mask = resize(s.mask, w)
		s.flowWords(code, f.f.Handlers, int(s.sites[0]))
	}

	s.states = resize(s.states, len(cands))
	clear(s.states)
	for _, pc := range s.sites {
		p := f.f.Protos[code[pc].B]
		a := s.after[int(pc)*w : int(pc)*w+w]
		for _, r := range s.runs {
			if r.fn != p {
				continue
			}
			for i := r.lo; i < r.hi; i++ {
				if s.states[i] == copyShared {
					continue
				}
				if b := s.regBit[cands[i].b.reg]; a[b>>6]&(1<<(b&63)) != 0 {
					s.states[i] = copyShared
				} else {
					s.states[i] = copyFinal
				}
			}
		}
	}

	s.checked = resize(s.checked, len(s.regs))
	defer clear(s.checked)
	s.nameOK = resize(s.nameOK, len(s.regs))
	for i, c := range cands {
		if s.states[i] != copyFinal {
			continue
		}
		b := s.regBit[c.b.reg]
		if s.checked[b] != c.b {
			// An arguments object or a nested function can change the variable without an
			// instruction of f naming its register. Variables of different scopes can share a
			// register.
			s.checked[b] = c.b
			s.nameOK[b] = !f.mutated[c.b.name] && !f.nestedAssigns(c.b.name)
		}
		if s.nameOK[b] {
			c.fn.Upvals[c.index].ByValue = true
		}
	}
}

// The flow functions fill s.after for the instructions of code that can run after the
// instruction at low, which starts the first creation of a closure, and leave the others zero.
// The first pass goes over the code in reverse order, so that the instructions after an
// instruction are done before it unless a jump or a handler goes backwards. Only then do more
// passes follow, from the last instruction that goes backwards, which leave out the writes that
// the first pass added.

// flowOneWord is the flow function for sets of one word.
func (s *copyScratch) flowOneWord(code []bytecode.Instr, handlers []bytecode.Handler, low int) {
	after := s.after[:len(code)]
	lo, hi := s.regs[0], s.regs[len(s.regs)-1]
	start := len(code) - 1
	for pass := 0; ; pass++ {
		// m is the set of the instruction after the one at pc.
		var m uint64
		if next := s.next(code, start); next < len(code) {
			m = after[next]
		}
		changed := false
		back := -1
		for pc := start; pc >= low; pc-- {
			in := &code[pc]
			if in.Op == bytecode.OpExtra {
				continue
			}
			fl := &opFlows[in.Op]
			a := after[pc]
			if fl.flow != bytecode.FlowEnd {
				a |= m
			}
			if pass == 0 && fl.writesA {
				if r := in.A; r >= lo && r <= hi && s.present[r>>6]&(1<<(r&63)) != 0 {
					a |= 1 << s.regBit[r]
				}
			}
			if !fl.plain {
				if fl.flow == bytecode.FlowNextOrSkip && pc+2 < len(code) {
					a |= after[pc+2]
				}
				if pass == 0 && !fl.writesA {
					for _, r := range fl.writes {
						if r.At <= bytecode.FieldC && operandOf(in, nil, r.At)+int32(r.Offset) > hi {
							continue
						}
						if i, j := s.written(code, pc, fl, r); i < j {
							a |= (^uint64(0) >> (64 - (j - i))) << i
						}
					}
				}
				if fl.targets != 0 {
					for _, t := range s.targets(code, pc, fl) {
						if t < 0 {
							break
						}
						a |= after[t]
						if int(t) <= pc {
							back = max(back, pc)
							low = min(low, int(t))
						}
					}
				}
			}
			for _, h := range handlers {
				if pc < int(h.Start) || pc >= int(h.End) {
					continue
				}
				// A throw continues at the target of the handler, which receives the thrown
				// value in its register.
				a |= after[h.Target]
				if b := s.bit(h.Reg); b >= 0 {
					a |= 1 << b
				}
				if int(h.Target) <= pc {
					back = max(back, pc)
					low = min(low, int(h.Target))
				}
			}
			if a != after[pc] {
				after[pc] = a
				changed = true
			}
			m = a
		}
		if back < 0 || pass > 0 && !changed {
			return
		}
		start = back
	}
}

// flowWords is the flow function for sets of more than one word.
func (s *copyScratch) flowWords(code []bytecode.Instr, handlers []bytecode.Handler, low int) {
	w := s.words
	m := s.mask[:w]
	hi := s.regs[len(s.regs)-1]
	start := len(code) - 1
	for pass := 0; ; pass++ {
		// m is the set of the instruction after the one at pc, and then that of the one at pc.
		clear(m)
		if next := s.next(code, start); next < len(code) {
			copy(m, s.after[next*w:next*w+w])
		}
		changed := false
		back := -1
		for pc := start; pc >= low; pc-- {
			in := &code[pc]
			if in.Op == bytecode.OpExtra {
				continue
			}
			fl := &opFlows[in.Op]
			if fl.flow == bytecode.FlowEnd {
				clear(m)
			}
			if !fl.plain {
				if fl.flow == bytecode.FlowNextOrSkip && pc+2 < len(code) {
					orWords(m, s.after[(pc+2)*w:(pc+2)*w+w])
				}
				if fl.targets != 0 {
					for _, t := range s.targets(code, pc, fl) {
						if t < 0 {
							break
						}
						orWords(m, s.after[int(t)*w:int(t)*w+w])
						if int(t) <= pc {
							back = max(back, pc)
							low = min(low, int(t))
						}
					}
				}
			}
			if pass == 0 {
				for _, r := range fl.writes {
					if r.At <= bytecode.FieldC && operandOf(in, nil, r.At)+int32(r.Offset) > hi {
						continue
					}
					i, j := s.written(code, pc, fl, r)
					for ; i < j; i++ {
						m[i>>6] |= 1 << (i & 63)
					}
				}
			}
			for _, h := range handlers {
				if pc < int(h.Start) || pc >= int(h.End) {
					continue
				}
				orWords(m, s.after[int(h.Target)*w:int(h.Target)*w+w])
				if b := s.bit(h.Reg); b >= 0 {
					m[b>>6] |= 1 << (b & 63)
				}
				if int(h.Target) <= pc {
					back = max(back, pc)
					low = min(low, int(h.Target))
				}
			}
			a := s.after[pc*w : pc*w+w]
			for k, v := range m[:len(a)] {
				if v |= a[k]; v != a[k] {
					a[k] = v
					changed = true
				}
				m[k] = v
			}
		}
		if back < 0 || pass > 0 && !changed {
			return
		}
		start = back
	}
}

// orWords sets a to the union of a and b.
func orWords(a, b []uint64) {
	for k, v := range b[:len(a)] {
		a[k] |= v
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

// written returns the indices in s.regs from i to j of the registers that the instruction at pc
// of code, whose flow is fl, writes by its run r.
func (s *copyScratch) written(code []bytecode.Instr, pc int, fl *opFlow, r bytecode.Regs) (i, j int) {
	in := &code[pc]
	var x *bytecode.Instr
	if fl.extra {
		x = &code[pc+1]
	}
	start := operandOf(in, x, r.At)
	if start < 0 {
		return 0, 0
	}
	lo, hi := s.regs[0], s.regs[len(s.regs)-1]
	first := start + int32(r.Offset)
	if first > hi {
		return 0, 0
	}
	if r.Len == 1 && !r.Counted {
		if b := s.bit(first); b >= 0 {
			return b, b + 1
		}
		return 0, 0
	}
	end := hi + 1
	if r.Len != bytecode.RegsToEnd {
		end = first + int32(r.Len)
		if r.Counted {
			for i, fd := range flowFields {
				if fl.counts&(1<<i) != 0 {
					end += operandOf(in, x, fd)
				}
			}
		}
	}
	if end <= lo {
		return 0, 0
	}
	i, _ = slices.BinarySearch(s.regs, first)
	j, _ = slices.BinarySearch(s.regs, end)
	return i, j
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
