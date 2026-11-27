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
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// maxMovedStack is the number of stack slots that every request for room moves. A larger stack
// moves less often, by as much as it is larger.
const maxMovedStack = 1 << 12

// abandonedStack is stack storage that the interpreter has moved away from.
type abandonedStack struct {
	slots []storage.Value

	// args are the slots that running calls still read their arguments from, in order of start.
	args []stackRange

	// depth is the depth of the outermost interpreter loop whose registers were here.
	depth int
}

// reads reports whether a running call may read the slot i.
func (a *abandonedStack) reads(i int) bool {
	return slices.ContainsFunc(a.args, func(r stackRange) bool {
		return r.start <= i && i < r.end
	})
}

// stackRange is the stack slots from start up to end.
type stackRange struct {
	start, end int
}

// execChecks is the state of the checks of the instructions.
type execChecks struct {
	// steps holds, by frame index, the instruction that the frame started last. Only the first
	// top of them can be valid.
	steps []execStep
	top   int

	observe func(*storage.FuncProto, int)

	// abandoned is the stack storage that interpreter loops may still refer to, ordered by depth.
	abandoned []abandonedStack

	// held says that an interpreter loop has loaded its registers from the current storage, and
	// heldDepth is the depth of the outermost such loop.
	held      bool
	heldDepth int

	// retired is the storage that nothing refers to any more, oldest first, for later moves.
	retired [][]storage.Value

	// args are the arguments of running calls in the current storage.
	args []stackRange

	// requests is the number of requests for room since the stack last moved.
	requests int
}

func (c *execChecks) observeSteps(observe func(*storage.FuncProto, int)) {
	c.observe = observe
}

// execStep is an instruction that a frame started, and the registers before it.
type execStep struct {
	valid bool

	// fp, base and gen identify the frame.
	fp   *storage.FuncProto
	base int
	gen  *generator

	pc   int
	regs []storage.Value
}

// enterChecks forgets the instructions of the running frame and of the frames after it, which
// an interpreter loop that starts or resumes the running frame leaves unchecked.
func (ag *agent) enterChecks() {
	ag.checks.forget(len(ag.frames) - 1)
}

// leaveChecks forgets the instructions of the frames that have ended or been suspended when an
// interpreter loop returns.
func (ag *agent) leaveChecks() {
	ag.checkAbandoned(ag.depth)
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
func (ag *agent) checkStep(fp *storage.FuncProto, pc int, regs []storage.Value) {
	c := &ag.checks
	n := len(ag.frames) - 1
	fr := &ag.frames[n]
	if want := fr.fp.Nregs + 1; len(regs) != want {
		panic(validationFailure(fp, pc, fmt.Sprintf("register count is %d, want %d", len(regs), want)))
	}
	var first *storage.Value
	if fr.gen != nil {
		first = &fr.gen.regs[0]
	} else {
		first = &ag.stack[fr.base]
	}
	if &regs[0] != first {
		panic(validationFailure(fp, pc, "registers do not belong to the running frame"))
	}
	if i := ag.staleWrite(ag.depth); i >= 0 {
		panic(validationFailure(fp, pc, fmt.Sprintf("wrote stale registers at the stack slot %d before the instruction", i)))
	}
	for r := range regs {
		if storage.IsStale(regs[r]) {
			panic(validationFailure(fp, pc, fmt.Sprintf("register %d has a value read from stale registers", r)))
		}
	}
	ag.checkActive(fp)
	ag.holdStack()
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
	s.regs = append(s.regs[:0], regs[:fp.Nregs]...)
	if c.observe != nil {
		c.observe(fp, pc)
	}
}

// moveStack moves the values of the stack to fresh storage.
func (ag *agent) moveStack() {
	c := &ag.checks
	c.requests++
	if len(ag.stack) == 0 || ag.stackUsed() > c.requests*maxMovedStack {
		return
	}
	used := ag.stackUsed()
	s := c.storage(min(2*used, len(ag.stack)))
	clear(s[copy(s, ag.stack[:used]):])
	ag.abandonStack()
	ag.stack = s
	for _, u := range ag.openUpvals {
		u.P = &s[u.Index]
	}
}

// stackUsed returns the number of stack slots that may have been written.
func (ag *agent) stackUsed() int {
	return min(ag.stackHigh+stackHeadroom, len(ag.stack))
}

// maxRetired is the number of storages that stay out of use before the first one is used again,
// so that a reference that outlives a few moves is caught.
const maxRetired = 8

// storage returns stack storage of n slots whose values the caller must overwrite.
func (c *execChecks) storage(n int) []storage.Value {
	if len(c.retired) < maxRetired {
		return make([]storage.Value, n)
	}
	s := c.retired[0]
	c.retired = slices.Delete(c.retired, 0, 1)
	for i := range s {
		if !sameBits(s[i], storage.StalePoison) {
			panic(fmt.Sprintf("engine: wrote to abandoned storage at the stack slot %d", i))
		}
	}
	if cap(s) < n {
		return make([]storage.Value, n)
	}
	return s[:n]
}

// retire keeps the storage of a, which nothing refers to any more, for a later move.
func (c *execChecks) retire(a abandonedStack) {
	if len(a.args) == 0 && cap(a.slots) <= maxMovedStack && len(c.retired) <= maxRetired {
		c.retired = append(c.retired, a.slots)
	}
}

// abandonStack poisons the storage of the stack, whose values the caller has copied to the
// storage that takes its place. The arguments of running calls stay, as a call may keep reading
// them from where it got them.
func (ag *agent) abandonStack() {
	c := &ag.checks
	a := abandonedStack{
		// The slots above the ones in use have never been written.
		slots: ag.stack[:ag.stackUsed()],
		args:  slices.Clone(c.args),
		depth: c.heldDepth,
	}
	slices.SortFunc(a.args, func(x, y stackRange) int {
		return cmp.Compare(x.start, y.start)
	})
	next := 0
	for _, r := range a.args {
		fillStale(a.slots[next:max(r.start, next)])
		next = max(r.end, next)
	}
	fillStale(a.slots[next:])
	if c.held {
		c.abandoned = append(c.abandoned, a)
	} else {
		c.retire(a)
	}
	c.held = false
	c.args = c.args[:0]
	c.requests = 0
}

func fillStale(slots []storage.Value) {
	for i := range slots {
		slots[i] = storage.StalePoison
	}
}

// holdStack records that the running interpreter loop has registers in the current storage.
func (ag *agent) holdStack() {
	c := &ag.checks
	if !c.held || ag.depth < c.heldDepth {
		c.held = true
		c.heldDepth = ag.depth
	}
}

// holdArgs keeps args readable when the stack moves, until releaseArgs is called with
// the result. Arguments on the stack must end at its top.
func (ag *agent) holdArgs(args []storage.Value) int {
	c := &ag.checks
	for _, v := range args {
		storage.CheckValue(v)
	}
	n := len(c.args)
	if start := ag.sp - len(args); len(args) > 0 && start >= 0 && ag.sp <= len(ag.stack) && &ag.stack[start] == &args[0] {
		c.args = append(c.args, stackRange{
			start: start,
			end:   ag.sp,
		})
	}
	return n
}

// releaseArgs ends the effect of the call of holdArgs that returned n.
func (ag *agent) releaseArgs(n int) {
	// Moving the stack may have dropped the range.
	ag.checks.args = ag.checks.args[:min(n, len(ag.checks.args))]
}

// staleWrite returns a slot that was written after the stack moved away from it, or -1. It checks
// and forgets the storage that only loops from depth on referred to.
func (ag *agent) staleWrite(depth int) int {
	c := &ag.checks
	for n := len(c.abandoned) - 1; n >= 0 && c.abandoned[n].depth >= depth; n-- {
		a := c.abandoned[n]
		c.abandoned[n] = abandonedStack{}
		c.abandoned = c.abandoned[:n]
		for i := range a.slots {
			if !sameBits(a.slots[i], storage.StalePoison) && !a.reads(i) {
				return i
			}
		}
		c.retire(a)
	}
	return -1
}

// checkAbandoned checks the storage that only loops from depth on referred to.
func (ag *agent) checkAbandoned(depth int) {
	if i := ag.staleWrite(depth); i >= 0 {
		panic(fmt.Sprintf("engine: wrote stale registers at the stack slot %d", i))
	}
}

// checkIdleStack checks the storage that the calls into the engine have moved away from, after
// the outermost one. It drops the stack when nothing uses it, so that the stack of the next call
// is small and moves at every request.
func (ag *agent) checkIdleStack() {
	ag.checkAbandoned(0)
	if ag.sp > 0 || len(ag.openUpvals) > 0 {
		return
	}
	ag.abandonStack()
	ag.checks.abandoned = nil
	ag.stack = nil
	ag.stackHigh = 0
}

// checkInstr checks the instruction of s, after which its frame continues at pc with the
// registers regs. A dispatch may include a second instruction or continue at an exception handler.
func (ag *agent) checkInstr(s *execStep, pc int, regs []storage.Value) {
	regs = regs[:len(s.regs)]
	code := s.fp.Code
	bad, reached := ag.fits(s, regs, pc, s.pc, -1)
	if reached && bad < 0 {
		return
	}
	if second := bytecode.NextPC(code, s.pc); second < len(code) && canFuse(code[s.pc], code[second]) {
		b, r := ag.fits(s, regs, pc, s.pc, second)
		if r && b < 0 {
			return
		}
		if r && !reached {
			bad, reached = b, r
		}
	}
	switch {
	case !reached && code[s.pc].Op.IsTailCall():
		// The callee took the place of the frame.
	case !reached:
		panic(validationFailure(s.fp, s.pc, fmt.Sprintf("continued at %d", pc)))
	default:
		panic(validationFailure(s.fp, s.pc, fmt.Sprintf("changed the register %d and continued at %d", bad, pc)))
	}
}

// canFuse reports whether the interpreter can consume both instructions in one dispatch.
func canFuse(first, second bytecode.Instr) bool {
	switch first.Op {
	case bytecode.OpCopyPropToGlobal, bytecode.OpResolveGlobal:
		return second.Op == bytecode.OpGetProp
	case bytecode.OpGetPropChain:
		return (second.Op == bytecode.OpGetProp || second.Op == bytecode.OpGetPropChain) && second.B == first.A
	case bytecode.OpIncGetIndex, bytecode.OpPostIncGetIndex:
		return second.Op == bytecode.OpGetIndex && second.C == first.A
	case bytecode.OpGetMethod:
		return second.Op == bytecode.OpCall && second.B == first.A
	case bytecode.OpIterNextPair:
		return second.Op == bytecode.OpIterNext && second.B == first.B
	}
	return false
}

// fits reports whether the instructions at first and second of the frame of s, or the one at
// first alone when second is -1, can continue at to, and returns a register that they changed
// without writing it, or -1. The registers of the frame were s.regs before and are regs after.
func (ag *agent) fits(s *execStep, regs []storage.Value, to, first, second int) (bad int, reached bool) {
	code := s.fp.Code
	var continues bool
	last := first
	if second < 0 {
		continues = bytecode.CanContinueAt(code, first, to)
	} else {
		last = second
		continues = to == bytecode.NextPC(code, second)
	}
	handlerReg := -1
	if !continues {
		// A throw continues at a handler, which receives the thrown value.
		found := false
		for _, h := range s.fp.Handlers {
			if int(h.Target) == to && h.Covers(last) {
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
		if sameBits(s.regs[r], regs[r]) || r == handlerReg || bytecode.MayWrite(code, first, r) || second >= 0 && bytecode.MayWrite(code, second, r) || ag.hasOpenUpval(s, r) {
			continue
		}
		return r, true
	}
	return -1, true
}

// hasOpenUpval reports whether a closure or an arguments object shares the register r of the
// frame of s, so that code other than the instruction of s may have changed it.
func (ag *agent) hasOpenUpval(s *execStep, r int) bool {
	ups, index := ag.openUpvals, s.base+r
	if s.gen != nil {
		ups, index = s.gen.openUpvals, r
	}
	_, ok := slices.BinarySearchFunc(ups, index, func(u *storage.Upvalue, i int) int {
		return cmp.Compare(u.Index, i)
	})
	return ok
}

// validationFailure describes a failed check at pc.
func validationFailure(fp *storage.FuncProto, pc int, msg string) string {
	in := fp.Code[pc]
	return fmt.Sprintf("engine: %s at %d of %s (%s): %s", in.Op, pc, fp.Name, fp.File, msg)
}

// checkActive checks that the active context is that of the running frame, whose code is fp.
func (ag *agent) checkActive(fp *storage.FuncProto) {
	if ag.active.realmPart != fp.Realm {
		panic(fmt.Sprintf("engine: active context is not that of the running frame of %s", fp.Name))
	}
}

// checkAppend checks that the conditions of the interpreter loop for an append to the array o agree
// with those of the storage operations.
func (cx *ExecutionContext) checkAppend(o *storage.Object) {
	if !o.ElemsResizable() || !cx.NewElementsPlain(o) {
		panic("engine: the interpreter loop appends to an array that is not plain")
	}
}

// sameBits reports whether a and b are the same bits.
func sameBits(a, b storage.Value) bool {
	return a.O == b.O && math.Float64bits(a.N) == math.Float64bits(b.N)
}

const validateExec = storage.ValidateExec
