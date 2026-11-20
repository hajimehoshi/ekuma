// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xmath"
)

// frame is the activation of a function written in JavaScript.
// Frames are reused in place, so the code that pushes one assigns every field. It assigns the
// pointer fields in one statement, which lets the compiler test for a write barrier once.
type frame struct {
	fp *funcProto
	cl *Object
	pc int

	// base is the stack index of register 0. For a generator, whose registers are not on the stack,
	// it is the stack index from which the frame may place the arguments of its calls.
	base int

	this Value

	// ret is the register of the calling frame that receives the result.
	ret int32

	// isNew is set when the function was called as a constructor that creates its own this.
	isNew bool

	argc int

	// extraBase is the stack index of the arguments beyond the parameters, which stay below
	// the registers when the function needs them, and -1 otherwise.
	extraBase int

	newTarget *Object

	// gen is the generator that the frame is an activation of.
	gen *generator

	// scopes are the dynamic scopes entered by with statements, innermost last.
	scopes []*Object
}

// thrownErrorKinds maps the error kinds of OpThrowError to the VM's.
var thrownErrorKinds = [...]ErrorKind{
	bytecode.ThrowTypeError:      ErrorKindType,
	bytecode.ThrowReferenceError: ErrorKindReference,
}

// primObjects maps the values that OpJumpNotSEqP compares with to their o.
var primObjects = [...]*Object{
	bytecode.PrimUndefined: nil,
	bytecode.PrimNull:      nullObject,
	bytecode.PrimFalse:     falseObject,
	bytecode.PrimTrue:      trueObject,
}

// maxFrames is the call depth at which a RangeError is thrown.
const maxFrames = 10000

var tdzObject = &Object{kind: kindHole}

// tdz is the content of a binding that is not initialized yet.
var tdz = Value{o: tdzObject}

type execStatus uint8

const (
	// execReturn: the frame the loop was started for returned.
	execReturn execStatus = iota

	// execThrow: an exception left the frame the loop was started for.
	execThrow

	// execPanic: Go code called by the loop threw. The frames are still in place.
	execPanic

	// execYield: the generator the loop was started for suspended.
	execYield
)

// stackHeadroom is the number of values that [VM.ensureStack] keeps free above what it is asked for.
const stackHeadroom = 64

func (vm *VM) ensureStack(n int) {
	vm.stackHigh = max(vm.stackHigh, n)
	if n+stackHeadroom <= len(vm.stack) {
		return
	}
	s := make([]Value, max(n+stackHeadroom, len(vm.stack)*2))
	copy(s, vm.stack)
	vm.stack = s
	// The open upvalues point into the old stack.
	for _, u := range vm.openUpvals {
		u.p = &s[u.index]
	}
}

// maxIdleStack is the number of stack slots that the stack keeps between calls into the engine.
const maxIdleStack = 1 << 16

// releaseStack moves the values in use to a smaller stack when the stack is larger than
// maxIdleStack.
func (vm *VM) releaseStack() {
	if len(vm.stack) <= maxIdleStack || vm.sp+stackHeadroom > maxIdleStack {
		return
	}
	var s []Value
	if vm.sp > 0 {
		s = make([]Value, vm.sp+stackHeadroom)
		copy(s, vm.stack[:vm.sp])
	}
	vm.stack = s
	vm.stackHigh = vm.sp
	// The open upvalues point into the old stack.
	for _, u := range vm.openUpvals {
		u.p = &s[u.index]
	}
}

// clearStale clears the stack slots and frames above the ones in use, which the calls since the
// outermost call into the engine started may have left values in.
func (vm *VM) clearStale() {
	clear(vm.stack[vm.sp:min(vm.stackHigh+stackHeadroom, len(vm.stack))])
	vm.stackHigh = vm.sp
	n := len(vm.frames)
	clear(vm.frames[n:min(vm.framesHigh, cap(vm.frames))])
	vm.framesHigh = n
}

// appendFrame appends a frame whose fields the caller must initialize.
func (vm *VM) appendFrame() *frame {
	n := len(vm.frames)
	if n == cap(vm.frames) {
		vm.frames = append(vm.frames, frame{})
	} else {
		vm.frames = vm.frames[:n+1]
	}
	vm.framesHigh = max(vm.framesHigh, len(vm.frames))
	return &vm.frames[n]
}

// pushFrame starts the activation of the closure f, whose argc arguments are already at base.
// It returns the stack index of register 0 of the new frame.
func (vm *VM) pushFrame(f *Object, this Value, base, argc int, ret int32, isNew bool, newTarget *Object) int {
	// exec builds the same frame inline for OpCall and OpCallCall in its fast switch, and for
	// OpApplyFwd in the switch after it, when the callee needs no surplus arguments; a change
	// here belongs there too.
	fp := f.fn.code
	fp.prepare()
	extraBase := -1
	if argc > fp.nparams && fp.needsExtra {
		// The surplus arguments stay where they are, and the registers start above them.
		extraBase = base + fp.nparams
		newBase := base + argc
		vm.ensureStack(newBase + fp.nregs + 1)
		// The parameters move above the surplus arguments, so the two places do not overlap.
		stack := vm.stack
		for i := range fp.nparams {
			stack[newBase+i] = stack[base+i]
		}
		base = newBase
	} else {
		vm.ensureStack(base + max(argc, fp.nregs+1))
	}
	clearFrameRegs(vm.stack[base:], fp.firstCleared(argc), fp.nclear)
	fr := vm.appendFrame()
	fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = fp, f, this, newTarget, nil, nil
	fr.pc, fr.base, fr.ret, fr.isNew, fr.argc, fr.extraBase = 0, base, ret, isNew, argc, extraBase
	vm.sp = base + fp.nregs + 1
	return base
}

// setNumber stores the number n in the register r.
func setNumber(r *Value, n float64) {
	// Over a number only n changes. Leaving the object pointer as it is needs no write barrier
	// while the garbage collector marks.
	if r.o != numberObject {
		r.o = numberObject
	}
	r.n = n
}

func clearFrameRegs(regs []Value, first, end int) {
	for j := first; j < end; j++ {
		regs[j] = Value{}
	}
}

// extraArgs returns the arguments of the frame fr beyond its parameters.
// The result is only valid until the stack grows.
func (vm *VM) extraArgs(fr *frame) []Value {
	if fr.gen != nil {
		return fr.gen.extra
	}
	if fr.extraBase < 0 {
		return nil
	}
	return vm.stack[fr.extraBase : fr.extraBase+fr.argc-fr.fp.nparams]
}

// findUpval returns the upvalue for the stack slot index.
func (vm *VM) findUpval(index int) *upvalue {
	i := len(vm.openUpvals)
	const tail = 4
	if i > 2*tail && vm.openUpvals[i-tail].index > index {
		// Every slot in the tail is above index.
		i = sort.Search(i-tail, func(j int) bool {
			return vm.openUpvals[j].index > index
		})
	} else {
		for i > 0 && vm.openUpvals[i-1].index > index {
			i--
		}
	}
	if i > 0 && vm.openUpvals[i-1].index == index {
		return vm.openUpvals[i-1]
	}
	u := &upvalue{
		p:     &vm.stack[index],
		index: index,
	}
	vm.openUpvals = append(vm.openUpvals, nil)
	copy(vm.openUpvals[i+1:], vm.openUpvals[i:])
	vm.openUpvals[i] = u
	return u
}

// closeUpvals detaches the upvalues that refer to the stack from index from on.
func (vm *VM) closeUpvals(from int) {
	n := len(vm.openUpvals)
	for n > 0 && vm.openUpvals[n-1].index >= from {
		u := vm.openUpvals[n-1]
		u.val = *u.p
		u.p = &u.val
		vm.openUpvals[n-1] = nil
		n--
	}
	vm.openUpvals = vm.openUpvals[:n]
}

// unwind finds the handler for the pending exception, dropping the frames that have none.
// It reports false when the frame at index stop was dropped without a handler.
func (vm *VM) unwind(stop int) bool {
	for {
		n := len(vm.frames) - 1
		fr := &vm.frames[n]
		pc := int32(fr.pc - 1)
		closing := vm.thrown.o == closeSignal
		for _, h := range fr.fp.Handlers {
			if pc < h.Start || pc >= h.End || (closing && !h.Finally) {
				continue
			}
			// The registers from h.Reg on belong to the scopes the handler leaves,
			// so their captured variables are detached before h.Reg is overwritten.
			var regs []Value
			if fr.gen != nil {
				regs = fr.gen.regs
				fr.gen.closeUpvals(int(h.Reg))
			} else {
				regs = vm.stack[fr.base:]
				if len(vm.openUpvals) > 0 {
					vm.closeUpvals(fr.base + int(h.Reg))
				}
			}
			regs[h.Reg] = vm.thrown
			vm.thrown = Undefined
			vm.dropNatives(n)
			fr.pc = int(h.Target)
			fr.scopes = fr.scopes[:min(len(fr.scopes), int(h.Scopes))]
			vm.sp = fr.top()
			return true
		}
		if fr.gen != nil {
			fr.gen.state = genCompleted
			fr.gen.regs = nil
		} else if len(vm.openUpvals) > 0 {
			vm.closeUpvals(fr.base)
		}
		*fr = frame{}
		vm.frames = vm.frames[:n]
		if n == stop {
			return false
		}
	}
}

// top returns the first stack index above the registers of the frame.
func (fr *frame) top() int {
	if fr.gen != nil {
		return fr.base
	}
	return fr.base + fr.fp.nregs + 1
}

// run executes the top frame until the frame at index stop returns, and returns its value.
// An exception that leaves that frame is thrown to the Go caller.
func (vm *VM) run(stop int) Value {
	if vm.depth >= maxDepth {
		vm.unwindTo(stop)
		vm.ThrowRangeError("maximum call stack size exceeded")
	}
	vm.depth++
	// The frames may belong to other realms than the code that started the loop.
	saved, outer := vm.curRealm, vm.callerRealm
	vm.callerRealm = saved
	for {
		v, st := vm.exec(stop)
		if validateExec {
			vm.leaveChecks()
		}
		switch st {
		case execReturn, execYield:
			vm.depth--
			vm.callerRealm = outer
			if vm.curRealm != saved {
				vm.switchRealm(saved)
			}
			return v
		case execPanic:
			if vm.unwind(stop) {
				continue
			}
		}
		vm.depth--
		vm.callerRealm = outer
		if vm.curRealm != saved {
			vm.switchRealm(saved)
		}
		panic(errThrow)
	}
}

// enterCallerRealm makes the realm of the caller of the frame at index n current, where stop
// is the frame that the running interpreter loop was started for.
func (vm *VM) enterCallerRealm(stop, n int) {
	r := vm.callerRealm
	if n > stop {
		r = vm.frames[n-1].fp.realm
	}
	if r != vm.curRealm {
		vm.switchRealm(r)
	}
}

// throwReturnError throws an error of the given kind for the return at pc of the frame at index n,
// where stop is as for enterCallerRealm. No handler of that frame catches it, and the error belongs
// to the realm of the caller.
func (vm *VM) throwReturnError(stop, n, pc int, kind ErrorKind, msg string) {
	vm.frames[n].pc = pc
	vm.enterCallerRealm(stop, n)
	e := vm.newError(kind, msg)
	// The frame has finished running, so it unwinds from pc 0, which lies before every handler.
	vm.frames[n].pc = 0
	vm.Throw(Value{o: e})
}

// restArray returns the arguments of fr starting at index start as a fresh array.
func (vm *VM) restArray(fr *frame, regs []Value, start int) *Object {
	n := min(fr.argc, fr.fp.nparams)
	extra := vm.extraArgs(fr)
	if start >= n {
		return vm.NewArrayCopy(extra[start-n:])
	}
	args := make([]Value, 0, fr.argc-start)
	args = append(args, regs[start:n]...)
	args = append(args, extra...)
	return vm.NewArray(args)
}

// exec is the interpreter loop. See run.
func (vm *VM) exec(stop int) (result Value, status execStatus) {
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			vm.sp = vm.frames[len(vm.frames)-1].top()
			status = execPanic
		}
	}()

	// The closure of the running function is read from its frame where it is needed, so that
	// the loop does not carry it from one instruction to the next.
	fr := &vm.frames[len(vm.frames)-1]
	fp := fr.fp
	pc := fr.pc
	if fp.realm != vm.curRealm {
		vm.switchRealm(fp.realm)
	}
	if validateExec {
		vm.enterChecks()
	}
	// The capacity of regs is never read: it is sliced and passed on only through slices.Clip,
	// so that the loop does not carry it from one instruction to the next.
	var regs []Value
	if fp.heapRegs {
		regs = fr.gen.regs
	} else {
		regs = vm.stack[fr.base : fr.base+fp.nregs+1]
	}
	// The state of a call in preparation, shared by every instruction that ends in a call.
	var (
		f         *Object
		callThis  Value
		argBase   int
		argc      int
		ret       int32
		isNew     bool
		newTarget *Object
	)

	for {
		if validateExec {
			vm.checkStep(fp, pc, slices.Clip(regs))
		}
		i := &fp.code[pc]
		pc++
		// The fast paths of the common instructions. Nothing in this switch calls a function
		// (TestExecFastPathsCallNothing checks it), so the values that the loop carries need
		// no stack copies on these paths. An instruction that leaves it runs in the switch
		// below, which handles every case of it. Undefined is written as Value{}, which needs
		// no load.
		switch i.Op {
		case bytecode.OpLoadK:
			regs[i.A] = fp.consts[i.B]
			continue
		case bytecode.OpLoadInt:
			setNumber(&regs[i.A], float64(i.B))
			continue
		case bytecode.OpLoadUndef:
			regs[i.A] = Value{}
			continue
		case bytecode.OpLoadNull:
			regs[i.A] = Null
			continue
		case bytecode.OpLoadTrue:
			regs[i.A] = True
			continue
		case bytecode.OpLoadFalse:
			regs[i.A] = False
			continue
		case bytecode.OpLoadTDZ:
			regs[i.A] = tdz
			continue
		case bytecode.OpMove:
			regs[i.A] = regs[i.B]
			continue
		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			c := &fp.globals[i.C]
			if h := c.holder; h != nil && h.shape == c.shape {
				regs[i.A] = *c.ref
				continue
			}
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
			// Only setGlobal fills the cache of an assignment.
			c := &fp.globals[i.C]
			if h := c.holder; h != nil && h.shape == c.shape {
				*c.ref = regs[i.A]
				continue
			}
		case bytecode.OpGetUpval:
			regs[i.A] = *vm.frames[len(vm.frames)-1].cl.fn.upvals[i.B].p
			continue
		case bytecode.OpSetUpval:
			*vm.frames[len(vm.frames)-1].cl.fn.upvals[i.B].p = regs[i.A]
			continue
		case bytecode.OpGetProp:
			// The destination is read first, so that the instruction need not stay in
			// registers across the tests of the cache.
			if a, o := i.A, regs[i.B].o; o != nil {
				c := &fp.caches[i.C]
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
					if o.shape != e.shape {
						break
					}
				}
				if e.kind != cacheOwn {
					if e.kind != cacheProto || e.epoch != vm.protoEpoch {
						break
					}
					regs[a] = *e.ref
					continue
				}
				regs[a] = o.slots[e.slot]
				continue
			}
		case bytecode.OpGetPropChain:
			if a, o := i.A, regs[i.B].o; o != nil {
				c := &fp.caches[i.C]
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
					if o.shape != e.shape {
						break
					}
				}
				var w Value
				if e.kind != cacheOwn {
					if e.kind != cacheProto || e.epoch != vm.protoEpoch {
						break
					}
					w = *e.ref
				} else {
					w = o.slots[e.slot]
				}
				regs[a] = w
				// The next instruction is an OpGetProp of w. It runs here only when it reads a
				// data property through its cache, and runs on its own otherwise.
				if o := w.o; o != nil {
					n := &fp.code[pc]
					c := &fp.caches[n.C]
					e := &c.cacheEntry
					if o.shape != e.shape {
						e = &c.second
						if o.shape != e.shape {
							continue
						}
					}
					if e.kind != cacheOwn {
						if e.kind != cacheProto || e.epoch != vm.protoEpoch {
							continue
						}
						regs[n.A] = *e.ref
					} else {
						regs[n.A] = o.slots[e.slot]
					}
					pc++
				}
				continue
			}
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			if o := regs[i.A].o; o != nil {
				c := &fp.caches[i.B]
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
					if o.shape != e.shape {
						break
					}
				}
				if e.kind == cacheOwn {
					o.slots[e.slot] = regs[i.C]
					continue
				}
				if e.kind == cacheAdd && o.objectProperties != nil && e.epoch == vm.protoEpoch && o.flags&objIsProto == 0 {
					if n := len(o.slots); n < cap(o.slots) {
						o.shape = e.next
						o.slots = o.slots[:n+1]
						o.slots[n] = regs[i.C]
						continue
					}
				}
			}
		case bytecode.OpGetLength:
			if o := regs[i.B].o; o != nil && o.kind == kindArray {
				setNumber(&regs[i.A], float64(o.length))
				continue
			}
		case bytecode.OpGetIndex:
			v, k := regs[i.B], regs[i.C]
			if o := v.o; o != nil && o.objectProperties != nil && k.o == numberObject {
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.elems)) {
					if e := o.elems[j]; e.o != holeObject {
						regs[i.A] = e
						continue
					}
				}
			}
		case bytecode.OpGetIndexInt:
			if o := regs[i.B].o; o != nil && o.objectProperties != nil && uint(i.C) < uint(len(o.elems)) {
				if e := o.elems[i.C]; e.o != holeObject {
					regs[i.A] = e
					continue
				}
			}
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict:
			v, k := regs[i.A], regs[i.B]
			if o := v.o; o != nil && o.objectProperties != nil && k.o == numberObject {
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.elems)) && o.elems[j].o != holeObject {
					o.elems[j] = regs[i.C]
					continue
				}
			}
		case bytecode.OpAdd:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				setNumber(&regs[i.A], x.n+y.n)
				continue
			}
		case bytecode.OpSub:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				setNumber(&regs[i.A], x.n-y.n)
				continue
			}
		case bytecode.OpMul:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				setNumber(&regs[i.A], x.n*y.n)
				continue
			}
		case bytecode.OpDiv:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				setNumber(&regs[i.A], x.n/y.n)
				continue
			}
		case bytecode.OpMod:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if xi, yi := int32(x.n), int32(y.n); xi > 0 && yi > 0 && float64(xi) == x.n && float64(yi) == y.n {
					setNumber(&regs[i.A], float64(xi%yi))
					continue
				}
			}
		case bytecode.OpBitAnd:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))&int32(int64(y.n))))
				continue
			}
		case bytecode.OpBitOr:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))|int32(int64(y.n))))
				continue
			}
		case bytecode.OpBitXor:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))^int32(int64(y.n))))
				continue
			}
		case bytecode.OpShl:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))<<(uint32(int64(y.n))&31)))
				continue
			}
		case bytecode.OpShr:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))>>(uint32(int64(y.n))&31)))
				continue
			}
		case bytecode.OpUShr:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject && numconv.InInt64Range(x.n) && numconv.InInt64Range(y.n) {
				setNumber(&regs[i.A], float64(uint32(int64(x.n))>>(uint32(int64(y.n))&31)))
				continue
			}
		case bytecode.OpAddK:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n+fp.consts[i.C].n)
				continue
			}
		case bytecode.OpSubK:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n-fp.consts[i.C].n)
				continue
			}
		case bytecode.OpMulK:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n*fp.consts[i.C].n)
				continue
			}
		case bytecode.OpDivK:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n/fp.consts[i.C].n)
				continue
			}
		case bytecode.OpModK:
			if x, y := regs[i.B], fp.consts[i.C]; x.o == numberObject {
				if xi, yi := int32(x.n), int32(y.n); xi > 0 && yi > 0 && float64(xi) == x.n && float64(yi) == y.n {
					setNumber(&regs[i.A], float64(xi%yi))
					continue
				}
			}
		case bytecode.OpBitAndK:
			if x := regs[i.B]; x.o == numberObject && numconv.InInt64Range(x.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))&int32(fp.consts[i.C].n)))
				continue
			}
		case bytecode.OpBitOrK:
			if x := regs[i.B]; x.o == numberObject && numconv.InInt64Range(x.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))|int32(fp.consts[i.C].n)))
				continue
			}
		case bytecode.OpShlK:
			if x := regs[i.B]; x.o == numberObject && numconv.InInt64Range(x.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))<<(uint32(fp.consts[i.C].n)&31)))
				continue
			}
		case bytecode.OpShrK:
			if x := regs[i.B]; x.o == numberObject && numconv.InInt64Range(x.n) {
				setNumber(&regs[i.A], float64(int32(int64(x.n))>>(uint32(fp.consts[i.C].n)&31)))
				continue
			}
		case bytecode.OpUShrK:
			if x := regs[i.B]; x.o == numberObject && numconv.InInt64Range(x.n) {
				setNumber(&regs[i.A], float64(uint32(int64(x.n))>>(uint32(fp.consts[i.C].n)&31)))
				continue
			}
		case bytecode.OpNeg:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], -x.n)
				continue
			}
		case bytecode.OpInc:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n+1)
				continue
			}
		case bytecode.OpDec:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n-1)
				continue
			}
		case bytecode.OpPostInc:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n)
				setNumber(&regs[i.B], x.n+1)
				continue
			}
		case bytecode.OpPostDec:
			if x := regs[i.B]; x.o == numberObject {
				setNumber(&regs[i.A], x.n)
				setNumber(&regs[i.B], x.n-1)
				continue
			}
		case bytecode.OpLt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool(x.n < y.n)
				continue
			}
		case bytecode.OpLe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool(x.n <= y.n)
				continue
			}
		case bytecode.OpGt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool(x.n > y.n)
				continue
			}
		case bytecode.OpGe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool(x.n >= y.n)
				continue
			}
		case bytecode.OpEq, bytecode.OpNe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool((x.n == y.n) == (i.Op == bytecode.OpEq))
				continue
			}
		case bytecode.OpJump:
			if int(i.A) >= pc || !vm.interrupt.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			// ToBoolean, for the kinds it can tell without a call.
			v := regs[i.B]
			t, known := false, true
			if o := v.o; o == numberObject {
				t = v.n != 0 && v.n == v.n
			} else if o == nil {
				t = false
			} else if k := o.kind; k == kindTrue {
				t = true
			} else if k <= kindFalse {
				t = false
			} else if k >= kindObject {
				// kindHTMLDDA is below kindObject and takes the slow path.
				t = true
			} else {
				known = false
			}
			if known {
				if t == (i.Op == bytecode.OpJumpIf) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpIfNullish:
			if x := regs[i.B]; x.IsNullish() || i.C != 0 && x.isHTMLDDA() {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfNotNullish:
			if x := regs[i.B]; !x.IsNullish() && (i.C == 0 || !x.isHTMLDDA()) {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfUndef:
			if regs[i.B].o == nil {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfNotUndef:
			if regs[i.B].o != nil {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotLt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n < y.n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotLe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n <= y.n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n > y.n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n >= y.n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if (x.n == y.n) != (i.Op == bytecode.OpJumpNotEq) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotSEq:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if x.n != y.n {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotSNe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if x.n == y.n {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotLtK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n < fp.consts[i.C].n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotLeK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n <= fp.consts[i.C].n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGtK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n > fp.consts[i.C].n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGeK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n >= fp.consts[i.C].n) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotSEqK:
			if x := regs[i.B]; x.o != numberObject || x.n != fp.consts[i.C].n {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSNeK:
			if x := regs[i.B]; x.o == numberObject && x.n == fp.consts[i.C].n {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSEqP:
			if regs[i.B].o != primObjects[i.C] {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSNeP:
			if regs[i.B].o == primObjects[i.C] {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfInt:
			if x := regs[i.B]; x.o == numberObject && x.n == float64(i.C) {
				pc = int(i.A)
			}
			continue
		case bytecode.OpInstanceof:
			// An ordinary function whose prototype property the cache holds, with the
			// @@hasInstance of %Function.prototype%: OrdinaryHasInstance walks the prototype
			// chain of b, unless a proxy is on it.
			if co := regs[i.C].o; co != nil {
				c := &fp.caches[fp.code[pc].A]
				if co.shape == c.shape && c.kind == cacheOwn && c.epoch == vm.protoEpoch && co.class == classFunction && co.fn != nil && co.fn.bound == nil {
					if p := co.slots[c.slot].o; p != nil && p.kind >= kindFirstObject {
						r, known := false, true
						if o := regs[i.B].o; o != nil && o.kind >= kindFirstObject {
							for h := o; ; {
								if h.class == classProxy {
									known = false
									break
								}
								if h = h.shape.proto; h == nil {
									break
								}
								if h == p {
									r = true
									break
								}
							}
						}
						if known {
							regs[i.A] = Bool(r)
							pc++
							continue
						}
					}
				}
			}
		case bytecode.OpArrayPush:
			if o := regs[i.A].o; len(o.elems) < cap(o.elems) {
				n := len(o.elems)
				o.elems = o.elems[:n+1]
				o.elems[n] = regs[i.B]
				o.length = uint32(n + 1)
				continue
			}
		case bytecode.OpDefineFieldK:
			o := regs[i.A].o
			if c := &fp.caches[i.B]; o.shape == c.shape && c.kind == cacheAdd {
				if n := len(o.slots); n < cap(o.slots) {
					o.shape = c.next
					o.slots = o.slots[:n+1]
					o.slots[n] = regs[i.C]
					continue
				}
			}
		case bytecode.OpGetMethod, bytecode.OpCall, bytecode.OpCallUndef, bytecode.OpCallCall:
			if i.Op == bytecode.OpGetMethod {
				// A data property read through the cache, as in OpGetProp's case. The call of
				// the method without arguments, when it is the next instruction, runs here too.
				// The this value goes in place first, as the case below does too, so that it
				// does not stay in registers across the tests of the cache.
				a := i.A
				v := regs[i.B]
				regs[a+1] = v
				o := v.o
				if o == nil {
					break
				}
				c := &fp.caches[i.C]
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
					if o.shape != e.shape {
						break
					}
				}
				var m Value
				if e.kind != cacheOwn {
					if e.kind != cacheProto || e.epoch != vm.protoEpoch {
						break
					}
					m = *e.ref
				} else {
					m = o.slots[e.slot]
				}
				n := &fp.code[pc]
				regs[a] = m
				if n.Op != bytecode.OpCall || n.B != a {
					continue
				}
				i = n
				pc++
			}
			// f is the callee and first the register of its first argument. The this value is in
			// the register before first, and is read only once the frame is certain, so that it
			// takes no registers while the frame is checked.
			b := int(i.B)
			f, first, argc := regs[b].o, b+2, int(i.C)
			if i.Op == bytecode.OpCallCall {
				if f != vm.Intrinsic(IntrinsicFunctionPrototypeCall) {
					f = nil
				} else {
					f, first, argc = regs[b+1].o, b+3, max(argc-1, 0)
				}
			}
			if !fp.heapRegs && f != nil && f.fn != nil {
				if c := f.fn.code; c != nil && c.inlineFrames && c.realm == vm.curRealm {
					n := len(vm.frames)
					argBase := vm.frames[n-1].base + first
					end := argBase + max(argc, c.nregs+1)
					if (argc <= c.nparams || !c.needsExtra) && end+stackHeadroom <= len(vm.stack) && n < cap(vm.frames) && n < maxFrames {
						var this Value
						switch {
						case i.Op == bytecode.OpCallUndef:
							regs[first-1] = Value{}
						case i.Op != bytecode.OpCallCall || i.C > 0:
							this = regs[first-1]
						}
						if site := c.returnedProperty; site != nil && this.IsObject() {
							if e := site.entry(this.o.shape); e != nil && e.kind == cacheOwn {
								regs[i.A] = this.o.slots[e.slot]
								continue
							}
						}
						// Reuse the available stack and frame storage for a prepared call. This builds
						// the frame that pushFrame would, without calling it, as nothing in this
						// switch may call; OpApplyFwd's case in the switch below builds it the same
						// way, and a change to one of the three belongs in the others.
						vm.frames[n-1].pc = pc
						vm.stackHigh = max(vm.stackHigh, end)
						clearFrameRegs(vm.stack[argBase:], c.firstCleared(argc), c.nclear)
						vm.frames = vm.frames[:n+1]
						vm.framesHigh = max(vm.framesHigh, n+1)
						fr := &vm.frames[n]
						fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
						fr.pc, fr.base, fr.ret, fr.isNew, fr.argc, fr.extraBase = 0, argBase, i.A, false, argc, -1
						vm.sp = argBase + c.nregs + 1
						fp, pc = c, 0
						regs = vm.stack[argBase:vm.sp]
						if r := c.thisReg; r >= 0 && (!c.thisSloppy || this.o != nil && this.o.kind >= kindFirstObject) {
							// Do what the first instruction does, and start after it.
							regs[r] = this
							pc = 1
						}
						continue
					}
				}
			}
		case bytecode.OpReturn, bytecode.OpReturnUndef:
			// The frame is popped as in the OpReturn case below, which handles generators,
			// upvalues to close, the return out of the loop and a change of realm; a change to
			// the popping belongs in both. The open upvalues are sorted by stack index, so
			// none refers to the registers of the frame when the last one lies below them.
			// The value is read before the tests, so that the instruction need not stay in
			// registers across them.
			r := Value{}
			if i.Op == bytecode.OpReturn {
				r = regs[i.A]
			}
			n := len(vm.frames) - 1
			if done := &vm.frames[n]; done.gen == nil && n != stop && vm.frames[n-1].fp.realm == vm.curRealm && (len(vm.openUpvals) == 0 || vm.openUpvals[len(vm.openUpvals)-1].index < done.base) {
				if done.isNew && (r.o == nil || r.o.kind < kindFirstObject) {
					r = done.this
				}
				ret := done.ret
				vm.frames = vm.frames[:n]
				fr := &vm.frames[n-1]
				fp = fr.fp
				pc = fr.pc
				if fp.heapRegs {
					regs = fr.gen.regs
					vm.sp = fr.base
				} else {
					regs = vm.stack[fr.base : fr.base+fp.nregs+1]
					vm.sp = fr.base + fp.nregs + 1
				}
				regs[ret] = r
				continue
			}
		}
		// Every instruction that the fast paths above leave, in full.
		switch i.Op {
		case bytecode.OpCheckTDZ:
			if regs[i.A].o == tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwTDZ(fp.keys[i.B])
			}
		case bytecode.OpThrowConstAssign:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(fp.keys[i.A].str) + "'")
		case bytecode.OpThrowError:
			vm.frames[len(vm.frames)-1].pc = pc
			// Error text is an interned constant.
			vm.throwError(thrownErrorKinds[i.A], wtf8.ToValidUTF8(fp.consts[i.B].o.str))

		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			c := &fp.globals[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			orUndef := i.Op == bytecode.OpGetGlobalOrUndef || i.Op == bytecode.OpGetGlobalOrUndefStrict
			strict := i.Op == bytecode.OpGetGlobalStrict || i.Op == bytecode.OpGetGlobalOrUndefStrict
			v := vm.getGlobal(fp.keys[i.B], c, orUndef, strict)
			regs[i.A] = v
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
			c := &fp.globals[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setGlobal(fp.keys[i.B], regs[i.A], c, i.Op == bytecode.OpSetGlobalStrict)
		case bytecode.OpResolveGlobal:
			c := &fp.globals[i.C]
			if h := c.holder; h != nil && h.shape == c.shape {
				regs[i.A] = True
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.resolveGlobal(fp.keys[i.B], c)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpDeclareGlobalVar:
			vm.frames[len(vm.frames)-1].pc = pc
			var fn Value
			if i.C >= 0 {
				fn = regs[i.C]
			}
			vm.declareGlobalVar(fp.keys[i.A], int(i.B), fn)
		case bytecode.OpDeclareGlobalLex:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareGlobalLex(fp.keys[i.A], i.B != 0)
		case bytecode.OpInitGlobalLex:
			vm.initGlobalLex(fp.keys[i.B], regs[i.A])
		case bytecode.OpCheckGlobalDecl:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.checkGlobalDecl(fp.keys[i.A], int(i.B))
		case bytecode.OpDeclareAnnexB:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Bool(vm.declareGlobalAnnexB(fp.keys[i.B], int(i.C)))

		case bytecode.OpCloseUpvals:
			if fp.heapRegs {
				vm.frames[len(vm.frames)-1].gen.closeUpvals(int(i.A))
			} else if len(vm.openUpvals) > 0 {
				vm.closeUpvals(vm.frameBase() + int(i.A))
			}
		case bytecode.OpClosure:
			p := fp.protos[i.B]
			o, copies := newClosure(p)
			fn := o.fn
			o.kind = kindFunction
			o.class = classFunction
			o.flags = objExotic
			o.shape = vm.shapes.function
			if p.isGenerator || p.isAsync {
				o.shape = vm.functionShapeFor(vm.functionProtoFor(p))
			}
			for j, d := range p.Upvals {
				switch {
				case !d.Local:
					fn.upvals[j] = vm.frames[len(vm.frames)-1].cl.fn.upvals[d.Index]
				case d.ByValue:
					u := &copies[0]
					copies = copies[1:]
					u.val = regs[d.Index]
					u.p = &u.val
					fn.upvals[j] = u
				case fp.heapRegs:
					fn.upvals[j] = vm.frames[len(vm.frames)-1].gen.findUpval(int(d.Index))
				default:
					fn.upvals[j] = vm.findUpval(vm.frameBase() + int(d.Index))
				}
			}
			regs[i.A] = Value{o: o}

		case bytecode.OpLoadThis:
			regs[i.A] = vm.frames[len(vm.frames)-1].this
		case bytecode.OpLoadThisSloppy:
			t := vm.frames[len(vm.frames)-1].this
			if t.o == nil || t.o.kind < kindFirstObject {
				if t.IsNullish() {
					t = Value{o: vm.global}
				} else {
					t = Value{o: vm.ToObject(t)}
				}
			}
			regs[i.A] = t
		case bytecode.OpLoadCallee:
			regs[i.A] = Value{o: vm.frames[len(vm.frames)-1].cl}
		case bytecode.OpLoadNewTarget:
			if nt := vm.frames[len(vm.frames)-1].newTarget; nt != nil {
				regs[i.A] = Value{o: nt}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpLoadHome:
			if h := vm.frames[len(vm.frames)-1].cl.fn.home; h != nil {
				regs[i.A] = Value{o: h}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpArguments:
			cur := &vm.frames[len(vm.frames)-1]
			regs[i.A] = Value{o: vm.newArguments(cur, slices.Clip(regs), i.B != 0)}
		case bytecode.OpRestArgs:
			cur := &vm.frames[len(vm.frames)-1]
			var rest *Object
			if int(i.B) < cur.argc {
				rest = vm.restArray(cur, slices.Clip(regs), int(i.B))
			} else {
				rest = vm.NewArray(nil)
			}
			regs[i.A] = Value{o: rest}

		case bytecode.OpGetMethod:
			// The this value is in place before a getter runs.
			regs[i.A+1] = regs[i.B]
			fallthrough
		case bytecode.OpGetProp, bytecode.OpGetPropChain:
			// Data properties in the cache take the fast path above. After an OpGetPropChain
			// that ends up here, the next instruction runs on its own.
			v := regs[i.B]
			c := &fp.caches[i.C]
			o := v.o
			if o == numberObject {
				// getNamed starts at Number.prototype for a number. Its own slot
				// stays valid for its shape, and the method receives v as this.
				o = vm.Intrinsic(IntrinsicNumberPrototype)
				if c.kind == cacheOwn && c.shape == o.shape {
					regs[i.A] = o.slots[c.slot]
					continue
				}
			}
			if o != nil {
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
				}
				if o.shape == e.shape {
					if e.kind == cacheGetter && e.epoch == vm.protoEpoch {
						f, callThis, argc, ret = e.holder, v, 0, i.A
						goto callAbove
					}
					if e.kind == cacheOwnGetter {
						g := o.slots[e.slot].accessor().getter
						if g == nil {
							regs[i.A] = Undefined
							continue
						}
						f, callThis, argc, ret = g, v, 0, i.A
						goto callAbove
					}
				}
			}
			goto getNamed
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			v := regs[i.A]
			c := &fp.caches[i.B]
			var setter *Object
			if o := v.o; o != nil {
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
				}
				if o.shape == e.shape {
					switch e.kind {
					case cacheAdd:
						if o.objectProperties != nil && e.epoch == vm.protoEpoch && o.flags&objIsProto == 0 {
							o.shape = e.next
							o.slots = append(o.slots, regs[i.C])
							continue
						}
					case cacheSetter:
						if e.epoch == vm.protoEpoch {
							setter = e.holder
						}
					}
				}
			}
			if setter == nil {
				vm.frames[len(vm.frames)-1].pc = pc
				var ok bool
				setter, ok = vm.setNamed(v, fp.keys[i.B], regs[i.C], c)
				if !ok && i.Op == bytecode.OpSetPropStrict {
					vm.throwReadOnly(v, PropKey{key: fp.keys[i.B]})
				}
			}
			if setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpGetLength:
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getNamedAtSite(v, vm.Key(KeyLength), &fp.caches[i.C])
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpGetIndex:
			v, k := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getValue(v, k)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpGetIndexInt:
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getIndex(v, uint32(i.C))
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict:
			v, k := regs[i.A], regs[i.B]
			if o := v.o; o != nil && o.objectProperties != nil && k.o == numberObject {
				if j := int(k.n); j == len(o.elems) && o.kind == kindArray && uint32(j) == o.length && o.flags == 0 && float64(j) == k.n && o.shape == vm.shapes.array && vm.arrayProtoPlain() {
					// Appending to a plain array.
					o.elems = appendElem(o.elems, regs[i.C])
					o.length++
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			// Converting the key can run code that changes the register of the value.
			val := regs[i.C]
			setter := vm.setValue(v, k, val, i.Op == bytecode.OpSetIndexStrict)
			if setter != nil {
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpSuperBase:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = vm.superBase(regs[i.B])
		case bytecode.OpGetSuper:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			ext := fp.code[pc]
			pc++
			if ext.B == 0 {
				v, _ := vm.getSuper(vm.superBase(regs[ext.A]), regs[i.C], regs[i.B])
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			v, key := vm.getSuper(regs[ext.A], regs[i.C], regs[i.B])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.C] = vm.KeyValue(key)
			regs[i.A] = v
		case bytecode.OpSetSuper, bytecode.OpSetSuperStrict:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			superBase := regs[fp.code[pc].A]
			pc++
			vm.setSuper(superBase, regs[i.B], regs[i.C], regs[i.A], i.Op == bytecode.OpSetSuperStrict)
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpDeleteProp, bytecode.OpDeletePropStrict:
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.deleteValue(regs[i.B], regs[i.C], i.Op == bytecode.OpDeletePropStrict)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpIn:
			vm.frames[len(vm.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				vm.ThrowTypeError("cannot use 'in' operator to search for a key in a non-object")
			}
			r := vm.HasProperty(o.o, vm.QueryKey(regs[i.C]))
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpInstanceof:
			site := &fp.caches[fp.code[pc].A]
			pc++
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.instanceOf(regs[i.B], regs[i.C], site)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpHasPrivate:
			vm.frames[len(vm.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				vm.ThrowTypeError("cannot use 'in' operator to search for a private field in a non-object")
			}
			_, ok := o.o.shape.lookup(regs[i.C].o)
			regs[i.A] = Bool(ok)
		case bytecode.OpGetPrivate:
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getPrivate(v, regs[i.C].o)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetPrivate:
			v := regs[i.A]
			vm.frames[len(vm.frames)-1].pc = pc
			if setter := vm.setPrivate(v, regs[i.B].o, regs[i.C]); setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpToPropertyKey:
			v := regs[i.B]
			// The read and the write of a compound assignment convert an index alike.
			if v.o != nil && (v.o.kind == kindString || v.o.kind == kindSymbol) || v.o == numberObject && i.C >= 0 && float64(uint32(v.n)) == v.n {
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if i.C >= 0 {
				if b := regs[i.C]; b.IsNullish() {
					vm.ThrowTypeError("cannot read properties of " + vm.DisplayString(b) + " (reading '" + vm.DisplayString(v) + "')")
				}
			}
			k := vm.KeyValue(vm.ToPropertyKey(v))
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = k

		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			if x.o != nil && y.o != nil && x.o.kind == kindString && y.o.kind == kindString {
				regs[i.A] = vm.Concat(x, y)
				continue
			}
			v := vm.addValues(x, y)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpMod:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Number(math.Mod(x.n, y.n))
				continue
			}
			goto arith
		case bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpBitOr, bytecode.OpBitAnd, bytecode.OpBitXor, bytecode.OpShl, bytecode.OpShr, bytecode.OpUShr:
			goto arith
		case bytecode.OpExp:
			x, y := regs[i.B], regs[i.C]
			if x.o == numberObject && y.o == numberObject {
				regs[i.A] = Number(NumberPow(x.n, y.n))
				continue
			}
			goto arith
		case bytecode.OpAddK:
			x := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.addValues(x, fp.consts[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpModK:
			if x := regs[i.B]; x.o == numberObject {
				regs[i.A] = Number(math.Mod(x.n, fp.consts[i.C].n))
				continue
			}
			goto arithK
		case bytecode.OpSubK, bytecode.OpMulK, bytecode.OpDivK, bytecode.OpBitOrK, bytecode.OpBitAndK, bytecode.OpShlK, bytecode.OpShrK, bytecode.OpUShrK:
			goto arithK
		case bytecode.OpNeg:
			x := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.negate(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPlus:
			x := regs[i.B]
			if x.o == numberObject {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			n := vm.ToNumber(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Number(n)
		case bytecode.OpNot:
			regs[i.A] = Bool(!ToBoolean(regs[i.B]))
		case bytecode.OpBitNot:
			x := regs[i.B]
			if x.o == numberObject && numconv.InInt64Range(x.n) {
				regs[i.A] = Number(float64(^int32(int64(x.n))))
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.bitNot(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpToNumeric:
			x := regs[i.B]
			if x.o == numberObject {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToNumeric(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPostInc, bytecode.OpPostDec:
			x := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToNumeric(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
			d := int64(1)
			if i.Op == bytecode.OpPostDec {
				d = -1
			}
			// Stepping a number or a BigInt runs no JavaScript.
			regs[i.B] = vm.step(v, d)
		case bytecode.OpToString:
			x := regs[i.B]
			if x.o != nil && x.o.kind == kindString {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToStringValue(x)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpConcat:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = vm.ConcatStrings(slices.Clip(regs)[i.B : i.B+i.C])
		case bytecode.OpTypeof:
			regs[i.A] = vm.typeofValue(regs[i.B])
		case bytecode.OpInc, bytecode.OpDec:
			x := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			d := int64(1)
			if i.Op == bytecode.OpDec {
				d = -1
			}
			v := vm.step(x, d)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpLt:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(x, y, true) == cmpLess
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpLe:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(y, x, false)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpGt:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(y, x, false) == cmpLess
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpGe:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(x, y, true)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpEq, bytecode.OpNe:
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.looseEquals(regs[i.B], regs[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(r == (i.Op == bytecode.OpEq))
		case bytecode.OpStrictEq:
			regs[i.A] = Bool(IsStrictlyEqual(regs[i.B], regs[i.C]))
		case bytecode.OpStrictNe:
			regs[i.A] = Bool(!IsStrictlyEqual(regs[i.B], regs[i.C]))

		case bytecode.OpJump:
			if int(i.A) < pc && vm.interrupt.Load() {
				// Every loop jumps backwards with this instruction.
				vm.frames[len(vm.frames)-1].pc = pc
				vm.Poll()
			}
			pc = int(i.A)
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			if ToBoolean(regs[i.B]) == (i.Op == bytecode.OpJumpIf) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLt:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(x, y, true) == cmpLess
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLe:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(y, x, false)
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGt:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(y, x, false) == cmpLess
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGe:
			x, y := regs[i.B], regs[i.C]
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(x, y, true)
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.looseEquals(regs[i.B], regs[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			if r != (i.Op == bytecode.OpJumpNotEq) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSEq:
			if !IsStrictlyEqual(regs[i.B], regs[i.C]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNe:
			if IsStrictlyEqual(regs[i.B], regs[i.C]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLtK:
			x := regs[i.B]
			if x.o != numberObject {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, vm.frameBase(), fp)
				if x.o != numberObject {
					if vm.compareBigIntMixed(x, fp.consts[i.C]) != cmpLess {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n < fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLeK:
			x := regs[i.B]
			if x.o != numberObject {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, vm.frameBase(), fp)
				if x.o != numberObject {
					if c := vm.compareBigIntMixed(x, fp.consts[i.C]); c != cmpLess && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n <= fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGtK:
			x := regs[i.B]
			if x.o != numberObject {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, vm.frameBase(), fp)
				if x.o != numberObject {
					if vm.compareBigIntMixed(x, fp.consts[i.C]) != cmpGreater {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n > fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGeK:
			x := regs[i.B]
			if x.o != numberObject {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, vm.frameBase(), fp)
				if x.o != numberObject {
					if c := vm.compareBigIntMixed(x, fp.consts[i.C]); c != cmpGreater && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n >= fp.consts[i.C].n) {
				pc = int(i.A)
			}

		case bytecode.OpCallUndef:
			regs[i.B+1] = Undefined
			fallthrough
		case bytecode.OpCall:
			// Calls of ordinary functions that fit take the inline frame of the fast path above.
			b := int(i.B)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, int(i.C), i.A, false, nil
			goto callRegs
		case bytecode.OpCallCall:
			b := int(i.B)
			if regs[b].o == vm.Intrinsic(IntrinsicFunctionPrototypeCall) {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[b+1].o, regs[b+2], b+3, int(i.C)-1, i.A, false, nil
				if argc < 0 {
					callThis, argc = Undefined, 0
				}
			} else {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, int(i.C), i.A, false, nil
			}
			goto callRegs
		case bytecode.OpApplyFwd, bytecode.OpTailApplyFwd:
			argsReg := fp.code[pc].A
			pc++
			b := int(i.B)
			if !regs[argsReg].IsUndefined() || regs[b].o != vm.Intrinsic(IntrinsicFunctionPrototypeApply) {
				// Once exposed, the arguments object's length and indexed properties can change.
				regs[b+2] = regs[i.C]
				regs[b+3] = vm.forwardedArguments(slices.Clip(regs), argsReg)
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, 2, i.A, false, nil
				if i.Op == bytecode.OpTailApplyFwd {
					argBase += vm.frameBase()
					goto tailCall
				}
				goto callRegs
			}
			if i.Op == bytecode.OpApplyFwd {
				// f.apply(this, arguments) for an ordinary function f written in JavaScript: the
				// arguments of the running function go above its registers, and it resumes after
				// the OpExtra that follows the instruction. This is not a fast path above, as the
				// registers its tests take there made the loop store the values it carries
				// before every instruction where registers are few, as on amd64.
				if f := regs[b+1].o; !fp.heapRegs && f != nil && f.fn != nil {
					if c := f.fn.code; c != nil && c.inlineFrames && c.realm == vm.curRealm {
						n := len(vm.frames)
						cur := &vm.frames[n-1]
						argc := cur.argc
						argBase := cur.base + fp.nregs + 1
						end := argBase + max(argc, c.nregs+1)
						if (argc <= fp.nparams || cur.extraBase >= 0) && (argc <= c.nparams || !c.needsExtra) && end+stackHeadroom <= len(vm.stack) && n < cap(vm.frames) && n < maxFrames {
							k := min(argc, fp.nparams)
							for j := range k {
								vm.stack[argBase+j] = regs[j]
							}
							for j := k; j < argc; j++ {
								vm.stack[argBase+j] = vm.stack[cur.extraBase+j-k]
							}
							// The frame is pushed as in OpCall's case in the fast switch.
							this := regs[i.C]
							cur.pc = pc
							vm.stackHigh = max(vm.stackHigh, end)
							clearFrameRegs(vm.stack[argBase:], c.firstCleared(argc), c.nclear)
							vm.frames = vm.frames[:n+1]
							vm.framesHigh = max(vm.framesHigh, n+1)
							fr := &vm.frames[n]
							fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
							fr.pc, fr.base, fr.ret, fr.isNew, fr.argc, fr.extraBase = 0, argBase, i.A, false, argc, -1
							vm.sp = argBase + c.nregs + 1
							fp, pc = c, 0
							regs = vm.stack[argBase:vm.sp]
							if r := c.thisReg; r >= 0 && (!c.thisSloppy || this.o != nil && this.o.kind >= kindFirstObject) {
								regs[r] = this
								pc = 1
							}
							continue
						}
					}
				}
			}
			f, callThis = regs[b+1].o, regs[i.C]
			goto forward
		case bytecode.OpCallFwd, bytecode.OpTailCallFwd:
			b := int(i.B)
			f, callThis = regs[b].o, regs[b+1]
			goto forward
		case bytecode.OpCallEvalSpread:
			// A direct eval, or the spread call of the case below for any other function.
			pc++
			vm.frames[len(vm.frames)-1].pc = pc
			if v, ok := vm.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), -1); ok {
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpCallSpread, bytecode.OpNewSpread, bytecode.OpSuperSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].o, regs[b+1], i.A, false, nil
			args := regs[b+2].o.elems
			argc = len(args)
			argBase = vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			switch i.Op {
			case bytecode.OpNewSpread:
				goto construct
			case bytecode.OpSuperSpread:
				newTarget = regs[b+1].o
				goto superCall
			}
			goto call
		case bytecode.OpCallEval, bytecode.OpTailCallEval:
			b := int(i.B)
			pc++
			vm.frames[len(vm.frames)-1].pc = pc
			if v, ok := vm.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), int(i.C)); ok {
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, int(i.C), i.A, false, nil
			if i.Op == bytecode.OpTailCallEval {
				argBase += vm.frameBase()
				goto tailCall
			}
			goto callRegs
		case bytecode.OpTailCallUndef:
			regs[i.B+1] = Undefined
			fallthrough
		case bytecode.OpTailCall:
			b := int(i.B)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], vm.frameBase()+b+2, int(i.C), i.A, false, nil
			goto tailCall
		case bytecode.OpTailCallEvalSpread:
			// A direct eval, or the spread call of the case below for any other function.
			pc++
			vm.frames[len(vm.frames)-1].pc = pc
			if v, ok := vm.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), -1); ok {
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpTailCallSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].o, regs[b+1], i.A, false, nil
			args := regs[b+2].o.elems
			argc = len(args)
			argBase = vm.sp
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			goto tailCall
		case bytecode.OpNew:
			b := int(i.B)
			site := fp.code[pc].A
			pc++
			f, argBase, argc, ret = regs[b].o, b+2, int(i.C), i.A
			if fp.heapRegs {
				top := vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(top + argc)
				copy(vm.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += vm.frameBase()
			}
			if f != nil && f.fn != nil && f.fn.code != nil && f.fn.constructor && !f.fn.derived {
				// The common case: a plain constructor function, whose prototype property is cached.
				c := &fp.caches[site]
				var proto Value
				if f.shape == c.shape && c.kind == cacheOwn {
					proto = f.slots[c.slot]
				} else {
					vm.frames[len(vm.frames)-1].pc = pc
					proto, _ = vm.getNamed(Value{o: f}, vm.Key(KeyPrototype), c)
				}
				if proto.o == nil || proto.o.kind < kindFirstObject {
					proto = Value{o: vm.realmIntrinsic(f.fn.code.realm, IntrinsicObjectPrototype)}
				}
				var shape *shape
				if proto.o == c.holder && c.next != nil {
					shape = c.next
				} else {
					shape = vm.shapeFor(proto.o)
					if c.kind == cacheOwn {
						c.holder, c.next = proto.o, shape
					}
				}
				callThis = Value{o: newInstance(shape)}
				isNew, newTarget = true, f
				goto call
			}
			goto construct
		case bytecode.OpSuperCall:
			b := int(i.B)
			argBase, argc, ret = b+2, int(i.C), i.A
			if fp.heapRegs {
				top := vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(top + argc)
				copy(vm.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += vm.frameBase()
			}
			f, newTarget = regs[b].o, regs[b+1].o
			goto superCall

		case bytecode.OpReturn, bytecode.OpReturnUndef, bytecode.OpReturnDerived:
			// The fast switch above pops the frame the same way for the returns that need none
			// of the calls here.
			r := Undefined
			if i.Op != bytecode.OpReturnUndef {
				r = regs[i.A]
			}
			n := len(vm.frames) - 1
			done := &vm.frames[n]
			if i.Op == bytecode.OpReturnDerived {
				if !r.IsObject() {
					if !r.IsUndefined() {
						vm.throwReturnError(stop, n, pc, ErrorKindType, "derived constructors may only return object or undefined")
					}
					r = regs[i.B]
					if r.o == tdzObject {
						vm.throwReturnError(stop, n, pc, ErrorKindReference, "must call super constructor in derived class before accessing 'this' or returning from derived constructor")
					}
				}
			} else if done.isNew && (r.o == nil || r.o.kind < kindFirstObject) {
				r = done.this
			}
			if g := done.gen; g != nil {
				g.state = genCompleted
				g.regs = nil
			} else if len(vm.openUpvals) > 0 {
				vm.closeUpvals(done.base)
			}
			ret = done.ret
			vm.frames = vm.frames[:n]
			if n == stop {
				return r, execReturn
			}
			fr := &vm.frames[n-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
				vm.sp = fr.base
			} else {
				regs = vm.stack[fr.base : fr.base+fp.nregs+1]
				vm.sp = fr.base + fp.nregs + 1
			}
			regs[ret] = r
		case bytecode.OpThrow:
			vm.thrown = regs[i.A]
			goto throw
		case bytecode.OpRethrow:
			vm.thrown = regs[i.A]
			if i.B >= 0 && vm.thrown.o == closeSignal {
				vm.thrown = regs[i.B]
			}
			goto throw

		case bytecode.OpInitialYield:
			n := len(vm.frames) - 1
			cur := &vm.frames[n]
			g := cur.gen
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedStart
			o := vm.newGeneratorObject(g)
			ret = cur.ret
			*cur = frame{}
			vm.frames = vm.frames[:n]
			if n == stop {
				return Value{o: o}, execReturn
			}
			fr := &vm.frames[n-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
				vm.sp = fr.base
			} else {
				regs = vm.stack[fr.base : fr.base+fp.nregs+1]
				vm.sp = fr.base + fp.nregs + 1
			}
			regs[ret] = Value{o: o}
		case bytecode.OpYield, bytecode.OpAwait:
			n := len(vm.frames) - 1
			cur := &vm.frames[n]
			g := cur.gen
			v := regs[i.B]
			g.resumeReg = i.A
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedYield
			g.awaiting = i.Op == bytecode.OpAwait
			g.delegating = i.C != 0
			*cur = frame{}
			vm.frames = vm.frames[:n]
			// A generator only ever runs as the frame its loop was started for.
			return v, execYield
		case bytecode.OpGenResumeMode:
			g := vm.frames[len(vm.frames)-1].gen
			regs[i.A] = Number(float64(g.mode))
			g.mode = ResumeNext
		case bytecode.OpYieldDelegateResult:
			vm.frames[len(vm.frames)-1].pc = pc
			r := regs[i.B]
			if !r.IsObject() {
				vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
			}
			done := ToBoolean(vm.GetV(r, vm.Key(keyDone)))
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !done {
				pc = int(i.C)
				continue
			}
			v := vm.GetV(r, vm.Key(KeyValue))
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpNewObject:
			o, slots := newObjectWithStorage(int(i.C))
			o.kind = kindObject
			o.shape = vm.shapes.object
			o.slots = slots[:0]
			regs[i.A] = Value{o: o}
		case bytecode.OpNewArray:
			o, elems := newObjectWithStorage(int(i.C))
			o.kind = kindArray
			o.class = classArray
			o.shape = vm.shapes.array
			o.elems = elems[:0]
			regs[i.A] = Value{o: o}
		case bytecode.OpNewArrayK:
			regs[i.A] = Value{o: vm.NewArrayCopy(fp.consts[i.B : i.B+i.C])}
		case bytecode.OpArrayPush:
			o := regs[i.A].o
			o.elems = appendElem(o.elems, regs[i.B])
			o.length = uint32(len(o.elems))
		case bytecode.OpArrayHole:
			o := regs[i.A].o
			o.elems = appendElem(o.elems, hole)
			o.length = uint32(len(o.elems))
		case bytecode.OpArraySpread:
			vm.frames[len(vm.frames)-1].pc = pc
			o := regs[i.A].o
			vm.appendIterable(o, regs[i.B])
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpSpreadArgs:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.spreadArgs(regs[i.B])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Value{o: o}
		case bytecode.OpRegExp:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Value{o: vm.newRegExpLiteral(&fp.regExps[i.B])}
		case bytecode.OpRegExpArg:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.lendRegExpLiteral(slices.Clip(regs), i.A, i.C, &fp.regExps[i.B])
		case bytecode.OpRegExpMethod:
			vm.frames[len(vm.frames)-1].pc = pc
			m, this := vm.regExpLiteralMethod(&fp.regExps[i.B], fp.keys[i.C], &fp.caches[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A], regs[i.A+1] = m, Value{o: this}
		case bytecode.OpTemplate:
			regs[i.A] = Value{o: vm.templateObject(&fp.templates[i.B])}
		case bytecode.OpDefineField:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineField(regs[i.A].o, regs[i.B], regs[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpDefineFieldK:
			o := regs[i.A].o
			c := &fp.caches[i.B]
			if o.shape == c.shape && c.kind == cacheAdd {
				o.shape = c.next
				o.slots = append(o.slots, regs[i.C])
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineFieldK(o, fp.keys[i.B], regs[i.C], c)
		case bytecode.OpDefineMethod:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineMethod(regs[i.A].o, regs[i.B], regs[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpDefineGetter, bytecode.OpDefineSetter:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			enumerable := fp.code[pc].A != 0
			pc++
			vm.defineAccessor(regs[i.A].o, regs[i.B], regs[i.C].o, i.Op == bytecode.OpDefineSetter, enumerable)
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpSetFuncName:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setFuncName(regs[i.A], regs[i.B], int(i.C))
		case bytecode.OpSetHome:
			regs[i.A].o.ownFunction().home = regs[i.B].o
		case bytecode.OpCopyProps:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.copyDataProperties(regs[i.A].o, regs[i.B], nil)
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpCopyPropsExcept:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			n := int(fp.code[pc].A)
			pc++
			o := vm.NewOrdinaryObject()
			excluded := append([]Value(nil), slices.Clip(regs)[i.C:int(i.C)+n]...)
			vm.copyDataProperties(o, regs[i.B], excluded)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Value{o: o}
		case bytecode.OpSetProto:
			if p := regs[i.B]; p.IsObject() {
				vm.setProto(regs[i.A].o, p.o)
			} else if p.IsNull() {
				vm.setProto(regs[i.A].o, nil)
			}
		case bytecode.OpClass:
			vm.frames[len(vm.frames)-1].pc = pc
			parent := hole
			if i.C >= 0 {
				parent = regs[i.C]
			}
			proto := vm.setupClass(regs[i.B].o, parent)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Value{o: proto}
		case bytecode.OpPrivateName:
			name := &Object{
				kind:  kindSymbol,
				class: classPrivateName,
				str:   fp.keys[i.B].str,
			}
			if i.C != 0 {
				name.flags |= objFrozenLength
			}
			regs[i.A] = Value{o: name}
		case bytecode.OpInitFields:
			if regs[i.A].o.fn.fields != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.initFields(regs[i.A].o, regs[i.B])
				regs = vm.reload(regs, vm.frameBase(), fp)
			}
		case bytecode.OpAddField:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			kind := int(fp.code[pc].A)
			pc++
			vm.addClassElement(regs[i.A].o, regs[i.B], regs[i.C], kind)
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpCheckThisUninit:
			if regs[i.A].o != tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.ThrowReferenceError("super constructor may only be called once")
			}
		case bytecode.OpRequireCoercible:
			if regs[i.A].IsNullish() {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.ThrowTypeError("cannot destructure '" + vm.DisplayString(regs[i.A]) + "' as it is " + vm.DisplayString(regs[i.A]))
			}

		case bytecode.OpForIn:
			vm.frames[len(vm.frames)-1].pc = pc
			it := vm.newForIn(regs[i.B])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = it
		case bytecode.OpForInNext:
			vm.frames[len(vm.frames)-1].pc = pc
			k, ok := vm.forInNext(regs[i.B].o)
			regs = vm.reload(regs, vm.frameBase(), fp)
			if !ok {
				pc = int(i.C)
				continue
			}
			regs[i.A] = k
		case bytecode.OpGetIterator:
			v := regs[i.B]
			if i.C == bytecode.IterSync && v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
				// Iterate a plain array by index: the next method slot holds the index.
				regs[i.A] = v
				regs[i.A+1] = Number(0)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			it, next := vm.getIterator(v, i.C == bytecode.IterAsync)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = it
			regs[i.A+1] = next
		case bytecode.OpIterNext:
			if regs[i.B].o == nil {
				// The iterator finished earlier.
				regs[i.A] = Undefined
				pc = int(i.C)
				continue
			}
			if idx := regs[i.B+1]; idx.o == numberObject {
				o := regs[i.B].o
				j := uint32(idx.n)
				if j >= o.length {
					// Mark the iterator as finished, so that closing it does nothing.
					regs[i.B] = Undefined
					regs[i.A] = Undefined
					pc = int(i.C)
					continue
				}
				regs[i.B+1] = Number(idx.n + 1)
				if uint(j) < uint(len(o.elems)) && o.elems[j].o != holeObject {
					regs[i.A] = o.elems[j]
					continue
				}
				vm.frames[len(vm.frames)-1].pc = pc
				// A getter that throws finishes the iterator.
				regs[i.B] = Undefined
				v := vm.Index(Value{o: o}, j)
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.B] = Value{o: o}
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			{
				// An iterator whose step throws counts as finished, so that it is not closed.
				it := regs[i.B]
				regs[i.B] = Undefined
				v, st := vm.iterNext(it, regs[i.B+1])
				regs = vm.reload(regs, vm.frameBase(), fp)
				switch st {
				case iterValue:
					regs[i.B] = it
					regs[i.A] = v
				case iterDone:
					regs[i.A] = Undefined
					pc = int(i.C)
				default:
					goto throw
				}
			}
		case bytecode.OpIterClose:
			it := regs[i.A]
			if it.IsUndefined() {
				continue
			}
			regs[i.A] = Undefined
			vm.frames[len(vm.frames)-1].pc = pc
			pending := i.B >= 0 && regs[i.B].o != closeSignal
			if index := regs[i.A+1]; index.o == numberObject {
				vm.closePlainArrayIterator(it.o, uint32(index.n), pending)
			} else {
				vm.iteratorClose(it, pending)
			}
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpIterCall:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				it := regs[i.B]
				m := vm.GetMethod(it, fp.keys[i.C])
				regs = vm.reload(regs, vm.frameBase(), fp)
				if m == nil {
					regs[i.A] = Undefined
					regs[i.A+1] = False
					continue
				}
				r := vm.Call(m, it, regs[i.A])
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = True
			}
		case bytecode.OpIterReturn:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				it := regs[i.B]
				regs[i.B] = Undefined
				r, ok := vm.iterReturn(it)
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = Bool(ok)
			}
		case bytecode.OpIterCheckResult:
			if !regs[i.A].IsObject() {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwNotIterResult(regs[i.A])
			}
		case bytecode.OpIterResultValue:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				v, done := vm.iterResultValue(regs[i.B])
				regs = vm.reload(regs, vm.frameBase(), fp)
				if done {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}

		case bytecode.OpUsing:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				dc := vm.addDisposableOp(regs[i.B], regs[i.A], DisposeHint(i.C))
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = dc
			}
		case bytecode.OpDisposeBegin:
			beginDisposeOp(regs[i.A], regs[i.B], regs[i.C])
		case bytecode.OpDisposeStep:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				v, await := vm.disposeStepOp(regs[i.B])
				regs = vm.reload(regs, vm.frameBase(), fp)
				if !await {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}
		case bytecode.OpDisposeError:
			vm.disposeError(regs[i.A].o.ext.(*DisposeCapability), regs[i.B])
		case bytecode.OpDisposeEnd:
			if v, thrown := endDisposeOp(regs[i.A]); thrown {
				regs[i.A] = Undefined
				vm.thrown = v
				goto throw
			}
			regs[i.A] = Undefined

		case bytecode.OpWith:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.ToObject(regs[i.A])
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = append(cur.scopes, o)
		case bytecode.OpPopScope:
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = cur.scopes[:len(cur.scopes)-1]
		case bytecode.OpResolveName:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.findScope(vm.frames[len(vm.frames)-1].cl, fp.keys[i.B], int(i.C))
			regs = vm.reload(regs, vm.frameBase(), fp)
			if o != nil {
				regs[i.A] = Value{o: o}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpGetScoped, bytecode.OpGetScopedStrict:
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.getScoped(regs[i.B].o, fp.keys[i.C], i.Op == bytecode.OpGetScopedStrict)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpSetScoped, bytecode.OpSetScopedStrict:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setScoped(regs[i.B].o, fp.keys[i.C], regs[i.A], i.Op == bytecode.OpSetScopedStrict)
			regs = vm.reload(regs, vm.frameBase(), fp)
		case bytecode.OpDeleteScoped:
			vm.frames[len(vm.frames)-1].pc = pc
			ok := vm.DeleteProp(regs[i.B].o, PropKey{key: fp.keys[i.C]})
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = Bool(ok)
		case bytecode.OpDeleteName:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Bool(vm.deleteName(fp.keys[i.B]))
		case bytecode.OpCaptureScopes:
			regs[i.A].o.ownFunction().scopes = vm.openScopes(vm.frames[len(vm.frames)-1].cl)
		case bytecode.OpEvalScope:
			vm.pushEvalScope()
		case bytecode.OpScopeThis:
			regs[i.A] = scopeThis(regs[i.B])
		case bytecode.OpDeclareEvalVar:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareEvalVar(vm.frames[len(vm.frames)-1].cl, fp.keys[i.A], int(i.B), slices.Clip(regs), int(i.C))
		case bytecode.OpImport, bytecode.OpImportMeta:
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.importOp(slices.Clip(regs), i)
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpAutoAccessor:
			vm.autoAccessorOp(slices.Clip(regs), i)
		case bytecode.OpDecorateBegin, bytecode.OpDecorateAdd, bytecode.OpDecorateClass, bytecode.OpDecorateElement, bytecode.OpDecorateApply, bytecode.OpDecorateFinish:
			if i.Op == bytecode.OpDecorateElement {
				// Past the OpExtra, which holds the Elem kind.
				pc++
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if v, ok := vm.decorateOp(slices.Clip(regs), i, &fp.code[pc-1]); ok {
				regs = vm.reload(regs, vm.frameBase(), fp)
				regs[i.A] = v
			} else {
				regs = vm.reload(regs, vm.frameBase(), fp)
			}

		default:
			panic("ekuma: unknown bytecode.Opcode")
		}
		continue

	arith:
		{
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.arith(i.Op, regs[i.B], regs[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	arithK:
		{
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.arith(i.Op.RegOperand(), regs[i.B], fp.consts[i.C])
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	getNamed:
		// The generic path of OpGetProp and OpGetPropChain.
		{
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getNamedAtSite(v, fp.keys[i.C], &fp.caches[i.C])
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
			continue
		}

	callAbove:
		// A call without arguments, such as a getter.
		argBase = vm.frames[len(vm.frames)-1].top()
		isNew, newTarget = false, nil
		goto call

	forward:
		// A call with the arguments of the running function.
		if (i.Op == bytecode.OpCallFwd || i.Op == bytecode.OpTailCallFwd) && (!regs[i.C].IsUndefined() || !vm.arrayIterationUnmodified()) {
			vm.frames[len(vm.frames)-1].pc = pc
			args := vm.spreadArgs(vm.forwardedArguments(slices.Clip(regs), i.C)).elems
			argc, ret, isNew, newTarget = len(args), i.A, false, nil
			argBase = vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			regs = vm.reload(regs, vm.frameBase(), fp)
		} else {
			cur := &vm.frames[len(vm.frames)-1]
			argc, ret, isNew, newTarget = cur.argc, i.A, false, nil
			argBase = cur.top()
			vm.ensureStack(argBase + argc)
			n := min(argc, fp.nparams)
			copy(vm.stack[argBase:], slices.Clip(regs)[:n])
			copy(vm.stack[argBase+n:], vm.extraArgs(&vm.frames[len(vm.frames)-1]))
		}
		if i.Op == bytecode.OpTailApplyFwd || i.Op == bytecode.OpTailCallFwd {
			goto tailCall
		}
		goto call

	callRegs:
		// The arguments are in the registers from argBase on.
		if fp.heapRegs {
			top := vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(top + argc)
			copy(vm.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
			argBase = top
		} else {
			argBase += vm.frameBase()
		}

	call:
		// The arguments are on the stack from argBase on.
		{
			if f == nil || f.fn == nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwNotCallable(fp, pc, slices.Clip(regs))
			}
			fn := f.fn
			if c := fn.code; c != nil && !c.isGenerator && !c.isAsync && (!fn.classConstructor || isNew || newTarget != nil) {
				if len(vm.frames) >= maxFrames {
					vm.frames[len(vm.frames)-1].pc = pc
					vm.ThrowRangeError("maximum call stack size exceeded")
				}
				if site := c.returnedProperty; site != nil && !isNew && newTarget == nil && c.realm == vm.curRealm && callThis.IsObject() {
					if e := site.entry(callThis.o.shape); e != nil && e.kind == cacheOwn {
						// Preparing the arguments may have grown the stack.
						regs = vm.reload(regs, vm.frameBase(), fp)
						regs[ret] = callThis.o.slots[e.slot]
						continue
					}
				}
				vm.frames[len(vm.frames)-1].pc = pc
				base := vm.pushFrame(f, callThis, argBase, argc, ret, isNew, newTarget)
				if c.realm != vm.curRealm {
					vm.switchRealm(c.realm)
				}
				fp = c
				regs = vm.stack[base : base+c.nregs+1]
				pc = 0
				if r := c.thisReg; r >= 0 {
					// Do what the first instruction does, and start after it.
					if t := vm.frames[len(vm.frames)-1].this; !c.thisSloppy || t.o != nil && t.o.kind >= kindFirstObject {
						regs[r] = t
						pc = 1
					}
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if fn.bound != nil && !isNew && newTarget == nil && len(fn.bound.args) == 0 {
				f, callThis = vm.unbindPlain(f)
				goto call
			}
			if c := fn.code; c != nil && c.isGenerator && !c.isAsync && fn.bound == nil {
				// Run the generator up to its initial suspension as an ordinary frame.
				vm.pushGenerator(f, callThis, vm.stack[argBase:argBase+argc], ret)
				if c.realm != vm.curRealm {
					vm.switchRealm(c.realm)
				}
				fr := &vm.frames[len(vm.frames)-1]
				fp = c
				regs = fr.gen.regs
				pc = 0
				continue
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.callSlow(f, callThis, vm.stack[argBase:argBase+argc:argBase+argc], isNew, newTarget)
			vm.sp = saved
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[ret] = r
			continue
		}

	tailCall:
		// A call in tail position, whose arguments are on the stack from argBase on.
		vm.frames[len(vm.frames)-1].pc = pc
		if f, callThis, argBase, argc = vm.unbind(f, callThis, argBase, argc); !vm.replaceFrame(f, callThis, argBase, argc) {
			goto call
		}
		{
			fr := &vm.frames[len(vm.frames)-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
			regs = vm.stack[fr.base : fr.base+fp.nregs+1]
			pc = 0
			continue
		}

	construct:
		// new f(...) for anything but a plain constructor function.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			if f == nil || f.fn == nil || !vm.IsConstructor(f) {
				vm.throwNotConstructor(fp, pc, slices.Clip(regs))
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(f, vm.stack[argBase:argBase+argc:argBase+argc], f)
			vm.sp = saved
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[ret] = Value{o: r}
			continue
		}

	superCall:
		// super(...): f is the super constructor and newTarget the new.target of the running one.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			parent := f
			if parent == nil || parent.fn == nil || !vm.IsConstructor(parent) {
				vm.ThrowTypeError("super constructor is not a constructor")
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(parent, vm.stack[argBase:argBase+argc:argBase+argc], newTarget)
			vm.sp = saved
			regs = vm.reload(regs, vm.frameBase(), fp)
			regs[ret] = Value{o: r}
			continue
		}

	throw:
		vm.frames[len(vm.frames)-1].pc = pc
		if !vm.unwind(stop) {
			return Undefined, execThrow
		}
		{
			fr := &vm.frames[len(vm.frames)-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
			} else {
				regs = vm.stack[fr.base : fr.base+fp.nregs+1]
			}
		}
	}
}

// unbindPlain returns the target and the this value that a call of the bound function f
// passes its arguments to, following bound functions without bound arguments.
func (vm *VM) unbindPlain(f *Object) (*Object, Value) {
	var this Value
	for b := f.fn.bound; b != nil && len(b.args) == 0; b = f.fn.bound {
		f, this = b.target, b.this
	}
	return f, this
}

// unbind returns the function that a tail call of f ends up calling, with its this value and
// the stack index and count of its arguments, for the argc arguments at argBase on the stack.
// A bound function stands for its target, with the bound arguments inserted before the others,
// Function.prototype.call of the running realm for its callable this value, and
// Function.prototype.apply of the running realm for its callable this value when its argument
// list can be made without running JavaScript.
func (vm *VM) unbind(f *Object, this Value, argBase, argc int) (*Object, Value, int, int) {
	for f != nil && f.fn != nil {
		if b := f.fn.bound; b != nil {
			if k := len(b.args); k > 0 {
				vm.ensureStack(argBase + argc + k)
				copy(vm.stack[argBase+k:], vm.stack[argBase:argBase+argc])
				copy(vm.stack[argBase:], b.args)
				argc += k
			}
			f, this = b.target, b.this
			continue
		}
		if this.o == nil || this.o.fn == nil {
			break
		}
		// Function.prototype.call and apply end in a tail call of their this value.
		switch f {
		case vm.Intrinsic(IntrinsicFunctionPrototypeCall):
			f, this = this.o, Undefined
			if argc > 0 {
				this = vm.stack[argBase]
				argBase++
				argc--
			}
			continue
		case vm.Intrinsic(IntrinsicFunctionPrototypeApply):
			var list []Value
			if argc > 1 {
				if a := vm.stack[argBase+1]; !a.IsNullish() {
					var ok bool
					if list, ok = a.o.plainElems(); !ok {
						return f, this, argBase, argc
					}
				}
			}
			f, this = this.o, Undefined
			if argc > 0 {
				this = vm.stack[argBase]
			}
			// The list goes above the arguments, which may be in the registers of the running frame.
			argBase = max(vm.sp, argBase+argc)
			argc = len(list)
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], list)
			continue
		}
		break
	}
	return f, this, argBase, argc
}

// replaceFrame replaces the running frame with a frame of f for the argc arguments at argBase on
// the stack, which returns to the caller of the running frame. It reports false, changing nothing,
// when f is not an ordinary function written in JavaScript or the running frame constructs an object.
func (vm *VM) replaceFrame(f *Object, this Value, argBase, argc int) bool {
	n := len(vm.frames) - 1
	cur := &vm.frames[n]
	if f == nil || f.fn == nil || cur.isNew || cur.newTarget != nil {
		return false
	}
	if c := f.fn.code; c == nil || c.isGenerator || c.isAsync || f.fn.classConstructor {
		return false
	}
	if vm.interrupt.Load() {
		vm.Poll()
	}
	// The arguments go where the running frame's were.
	start := cur.base
	if cur.extraBase >= 0 {
		start = cur.extraBase - cur.fp.nparams
	}
	if len(vm.openUpvals) > 0 {
		vm.closeUpvals(start)
	}
	end := max(vm.sp, argBase+argc)
	copy(vm.stack[start:], vm.stack[argBase:argBase+argc])
	ret := cur.ret
	*cur = frame{}
	vm.frames = vm.frames[:n]
	vm.pushFrame(f, this, start, argc, ret, false, nil)
	// Nothing above the new frame is in use any more.
	if vm.sp < end {
		clear(vm.stack[vm.sp:end])
	}
	return true
}

// frameBase returns the stack index of the registers of the running frame.
func (vm *VM) frameBase() int {
	return vm.frames[len(vm.frames)-1].base
}

// reload returns the registers of the running frame after a call that may have moved the stack.
func (vm *VM) reload(regs []Value, base int, fp *funcProto) []Value {
	if fp.heapRegs {
		return regs
	}
	return vm.stack[base : base+fp.nregs+1]
}

// NumberPow implements Number::exponentiate.
func NumberPow(x, y float64) float64 {
	// The exponents NaN and ±Infinity with a base of ±1 give NaN, unlike in IEEE 754.
	if y != y || (math.IsInf(y, 0) && (x == 1 || x == -1)) {
		return math.NaN()
	}
	// math.Pow is off by a unit in the last place for some powers of ten.
	if x == 10 && y == math.Trunc(y) && math.Abs(y) <= 400 {
		return numconv.Pow10(int(y))
	}
	return xmath.Pow(x, y)
}
