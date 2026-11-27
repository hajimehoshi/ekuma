// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// frame is the activation of a function written in JavaScript.
// Frames are reused in place, so the code that pushes one assigns every field. It assigns the
// pointer fields in one statement, which lets the compiler test for a write barrier once.
type frame struct {
	fp *storage.FuncProto
	cl *storage.Object
	pc int

	// base is the stack index of register 0. For a generator, whose registers are not on the stack,
	// it is the stack index from which the frame may place the arguments of its calls.
	base int

	this storage.Value

	// ret is the register of the calling frame that receives the result.
	ret int32

	// isNew is set when the function was called as a constructor that creates its own this.
	isNew bool

	// plainReturn is set when the frame is no activation of a generator and the function of the
	// calling frame belongs to the realm of its own function.
	plainReturn bool

	argc int

	// extraBase is the stack index of the arguments beyond the parameters, which stay below
	// the registers when the function needs them, and -1 otherwise.
	extraBase int

	newTarget *storage.Object

	// gen is the generator that the frame is an activation of.
	gen *generator

	// scopes are the dynamic scopes entered by with statements, innermost last.
	scopes []*storage.Object
}

// leaveScopes leaves the dynamic scopes of fr beyond the first n. It stays out of line to keep
// exec small.
//
//go:noinline
func (fr *frame) leaveScopes(n int) {
	for len(fr.scopes) > n {
		fr.scopes = storage.WithoutLast(fr.scopes)
	}
}

// thrownErrorKinds maps the error kinds of OpThrowError to [ErrorKind] values.
var thrownErrorKinds = [...]ErrorKind{
	bytecode.ThrowTypeError:      ErrorKindType,
	bytecode.ThrowReferenceError: ErrorKindReference,
}

// primObjects maps the values that OpJumpNotSEqP compares with to their o.
var primObjects = [...]*storage.Object{
	bytecode.PrimUndefined: nil,
	bytecode.PrimNull:      storage.NullObject,
	bytecode.PrimFalse:     storage.FalseObject,
	bytecode.PrimTrue:      storage.TrueObject,
}

// maxFrames is the call depth at which a RangeError is thrown.
const maxFrames = 10000

var tdzObject = storage.NewMarker(nil)

// tdz is the content of a binding that is not initialized yet.
var tdz = storage.Value{O: tdzObject}

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

	// execCatch: unwind found a handler and execution can continue there.
	execCatch
)

// stackHeadroom is the number of values that [agent.ensureStack] keeps free above what it is asked for.
const stackHeadroom = 64

func (ag *agent) ensureStack(n int) {
	ag.stackHigh = max(ag.stackHigh, n)
	if n+stackHeadroom <= len(ag.stack) {
		if validateExec {
			ag.moveStack()
		}
		return
	}
	s := make([]storage.Value, max(n+stackHeadroom, len(ag.stack)*2))
	copy(s, ag.stack)
	if validateExec {
		ag.abandonStack()
	}
	ag.stack = s
	// The open upvalues point into the old stack.
	for _, u := range ag.openUpvals {
		u.P = &s[u.Index]
	}
}

// maxIdleStack is the number of stack slots that the stack keeps between calls into the engine.
const maxIdleStack = 1 << 16

// releaseStack moves the values in use to a smaller stack when the stack is larger than
// maxIdleStack.
func (ag *agent) releaseStack() {
	if validateExec {
		ag.checkIdleStack()
	}
	if len(ag.stack) <= maxIdleStack || ag.sp+stackHeadroom > maxIdleStack {
		return
	}
	var s []storage.Value
	if ag.sp > 0 {
		s = make([]storage.Value, ag.sp+stackHeadroom)
		copy(s, ag.stack[:ag.sp])
	}
	if validateExec {
		ag.abandonStack()
	}
	ag.stack = s
	ag.stackHigh = ag.sp
	// The open upvalues point into the old stack.
	for _, u := range ag.openUpvals {
		u.P = &s[u.Index]
	}
}

// clearStale clears the stack slots and frames above the ones in use, which the calls since the
// outermost call into the engine started may have left values in.
func (ag *agent) clearStale() {
	clear(ag.stack[ag.sp:min(ag.stackHigh+stackHeadroom, len(ag.stack))])
	ag.stackHigh = ag.sp
	n := len(ag.frames)
	clear(ag.frames[n:min(ag.framesHigh, cap(ag.frames))])
	ag.framesHigh = n
}

// appendFrame appends a frame whose fields the caller must initialize. The frames have room for
// at most maxFrames, which the caller does not exceed.
func (ag *agent) appendFrame() *frame {
	n := len(ag.frames)
	if n == cap(ag.frames) {
		frames := make([]frame, n+1, max(min(2*n, maxFrames), n+1))
		copy(frames, ag.frames)
		ag.frames = frames
	} else {
		ag.frames = ag.frames[:n+1]
	}
	ag.framesHigh = max(ag.framesHigh, len(ag.frames))
	return &ag.frames[n]
}

// pushFrame starts the activation of the closure f, whose argc arguments are already at base.
// It returns the stack index of register 0 of the new frame.
func (ag *agent) pushFrame(f *storage.Object, this storage.Value, base, argc int, ret int32, isNew bool, newTarget *storage.Object) int {
	// exec builds the same frame inline for OpCall and OpCallCall in its fast switch, and for
	// OpApplyFwd in the switch after it, when the callee needs no surplus arguments; a change
	// here belongs there too.
	fp := f.Fn.Code
	fp.Prepare()
	extraBase := -1
	if argc > fp.Nparams && fp.NeedsExtra {
		// The surplus arguments stay where they are, and the registers start above them.
		extraBase = base + fp.Nparams
		newBase := base + argc
		ag.ensureStack(newBase + fp.Nregs + 1)
		// The parameters move above the surplus arguments, so the two places do not overlap.
		stack := ag.stack
		for i := range fp.Nparams {
			stack[newBase+i] = stack[base+i]
		}
		base = newBase
	} else {
		ag.ensureStack(base + fp.Nregs + 1)
	}
	if from, to := fp.FirstCleared(argc), fp.Nclear; from < to {
		clearFrameRegs(ag.stack[base:], from, to)
	}
	fr := ag.appendFrame()
	fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = fp, f, this, newTarget, nil, nil
	fr.pc, fr.base, fr.ret, fr.isNew, fr.argc, fr.extraBase = 0, base, ret, isNew, argc, extraBase
	n := len(ag.frames) - 1
	fr.plainReturn = n > 0 && ag.frames[n-1].fp.Realm == fp.Realm
	ag.sp = base + fp.Nregs + 1
	return base
}

func clearFrameRegs(regs []storage.Value, first, end int) {
	for j := first; j < end; j++ {
		regs[j] = storage.Value{}
	}
}

// frameRegs returns fr's registers, including the slot for discarded results.
// The slice is only valid until stack growth or reentry.
func (ag *agent) frameRegs(fr *frame) []storage.Value {
	return slices.Clip(fr.registers(ag.stack))
}

// placeCallArgs returns the stack index of the argc outgoing arguments starting at firstReg.
// The caller's register slice stays valid.
func (ag *agent) placeCallArgs(firstReg, argc int) int {
	fr := &ag.frames[len(ag.frames)-1]
	if fr.gen == nil {
		return fr.base + firstReg
	}
	base := fr.base
	ag.ensureStack(base + argc)
	copy(ag.stack[base:], slices.Clip(fr.gen.regs)[firstReg:firstReg+argc])
	return base
}

// registers returns the registers of fr given its agent's stack.
func (fr *frame) registers(stack []storage.Value) []storage.Value {
	// Keep the storage selection small enough to inline in exec.
	if fr.gen != nil {
		return fr.gen.regs
	}
	return stack[fr.base:][:fr.fp.Nregs+1]
}

// captureRegister returns the shared upvalue for a register of fr.
func (ag *agent) captureRegister(fr *frame, index int) *storage.Upvalue {
	if fr.gen != nil {
		return fr.gen.findUpval(index)
	}
	return ag.findUpval(fr.base + index)
}

// closeFrameUpvals detaches captured registers of fr starting at from.
func (ag *agent) closeFrameUpvals(fr *frame, from int) {
	if fr.gen != nil {
		fr.gen.closeUpvals(from)
	} else if len(ag.openUpvals) > 0 {
		ag.closeUpvals(fr.base + from)
	}
}

// extraArgs returns the arguments of the frame fr beyond its parameters.
// The result is only valid until the stack grows.
func (ag *agent) extraArgs(fr *frame) []storage.Value {
	if fr.gen != nil {
		return fr.gen.extra
	}
	if fr.extraBase < 0 {
		return nil
	}
	return ag.stack[fr.extraBase : fr.extraBase+fr.argc-fr.fp.Nparams]
}

// findUpval returns the upvalue for the stack slot index.
func (ag *agent) findUpval(index int) *storage.Upvalue {
	i := len(ag.openUpvals)
	const tail = 4
	if i > 2*tail && ag.openUpvals[i-tail].Index > index {
		// Every slot in the tail is above index.
		i = sort.Search(i-tail, func(j int) bool {
			return ag.openUpvals[j].Index > index
		})
	} else {
		for i > 0 && ag.openUpvals[i-1].Index > index {
			i--
		}
	}
	if i > 0 && ag.openUpvals[i-1].Index == index {
		return ag.openUpvals[i-1]
	}
	u := &storage.Upvalue{
		P:     &ag.stack[index],
		Index: index,
	}
	ag.openUpvals = append(ag.openUpvals, nil)
	copy(ag.openUpvals[i+1:], ag.openUpvals[i:])
	ag.openUpvals[i] = u
	return u
}

// closeUpvals detaches the upvalues that refer to the stack from index from on.
func (ag *agent) closeUpvals(from int) {
	n := len(ag.openUpvals)
	for n > 0 && ag.openUpvals[n-1].Index >= from {
		u := ag.openUpvals[n-1]
		u.Val = *u.P
		u.P = &u.Val
		ag.openUpvals[n-1] = nil
		n--
	}
	ag.openUpvals = ag.openUpvals[:n]
}

// unwind finds the handler for the pending exception, dropping the frames that have none.
// It returns execCatch at a handler, or the completion that left the frame at stop.
func (ag *agent) unwind(stop int) (storage.Value, execStatus) {
	for {
		n := len(ag.frames) - 1
		fr := &ag.frames[n]
		pc := int32(fr.pc - 1)
		closing := ag.thrown.O == closeSignal
		for _, h := range fr.fp.Handlers {
			if pc < h.Start || pc >= h.End || (closing && !h.Finally) {
				continue
			}
			// The registers from h.Reg on belong to the scopes the handler leaves,
			// so their captured variables are detached before h.Reg is overwritten.
			ag.closeFrameUpvals(fr, int(h.Reg))
			regs := ag.frameRegs(fr)
			regs[h.Reg] = ag.thrown
			if h.Finally && !closing && ag.captureThrowSites {
				regs[h.Reg] = ag.saveException()
			}
			ag.thrown = storage.Undefined
			ag.dropNatives(n)
			fr.pc = int(h.Target)
			if n := int(h.Scopes); n < len(fr.scopes) {
				fr.leaveScopes(n)
			}
			ag.sp = fr.top()
			return storage.Undefined, execCatch
		}
		var closeValue storage.Value
		if fr.gen != nil {
			closeValue = fr.gen.complete()
		} else if len(ag.openUpvals) > 0 {
			ag.closeUpvals(fr.base)
		}
		*fr = frame{}
		ag.frames = ag.frames[:n]
		if n == stop {
			if closing {
				ag.thrown = storage.Undefined
				ag.dropNatives(n)
				return closeValue, execReturn
			}
			return storage.Undefined, execThrow
		}
	}
}

// top returns the first stack index above the registers of the frame.
func (fr *frame) top() int {
	if fr.gen != nil {
		return fr.base
	}
	return fr.base + fr.fp.Nregs + 1
}

// run executes the top frame until the frame at index stop returns, and returns its value.
// An exception that leaves that frame is thrown to the Go caller. r is the realm of the frame.
func (cx *ExecutionContext) run(stop int, r *storage.RealmPart) storage.Value {
	v, ok := cx.runFrame(stop, r)
	if !ok {
		panic(errThrow)
	}
	return v
}

// runFrame executes the frame at stop, whose realm is r, until it returns, yields, or unwinds with an
// exception. An unwound frame leaves the exception pending and returns false.
// At the depth limit, the frames from stop are dropped and a RangeError is thrown.
func (cx *ExecutionContext) runFrame(stop int, r *storage.RealmPart) (storage.Value, bool) {
	if cx.depth >= maxDepth {
		cx.unwindTo(stop)
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	cx.depth++
	// The frames may belong to other realms than the code that started the loop.
	saved, outer := cx.active, cx.callerRealm
	cx.callerRealm = cx.realmPart
	for {
		if r != cx.active.realmPart {
			executionContextOf(r).activate()
		}
		v, st := cx.exec(stop)
		if validateExec {
			cx.leaveChecks()
		}
		if st == execPanic {
			v, st = cx.unwind(stop)
			if st == execCatch {
				r = cx.frames[len(cx.frames)-1].fp.Realm
				continue
			}
		}
		cx.depth--
		cx.callerRealm = outer
		saved.activate()
		return v, st != execThrow
	}
}

// throwReturnError throws in the caller's realm after the constructor at frame n returns.
func (cx *ExecutionContext) throwReturnError(stop, n, pc int, kind ErrorKind, msg string) {
	r := cx.callerRealm
	if n > stop {
		r = cx.frames[n-1].fp.Realm
	}
	cx.frames[n].pc = pc
	cx.recordThrow(executionContextOf(r).NewError(kind, msg))
	// The constructor has returned. Unwind from before its handlers.
	cx.frames[n].pc = 0
	panic(errThrow)
}

// restArray returns the arguments of fr starting at index start as a fresh array.
func (cx *ExecutionContext) restArray(fr *frame, start int) *storage.Object {
	n := min(fr.argc, fr.fp.Nparams)
	extra := cx.extraArgs(fr)
	if start >= n {
		return cx.NewArrayCopy(extra[start-n:])
	}
	args := make([]storage.Value, 0, fr.argc-start)
	args = append(args, cx.frameRegs(fr)[start:n]...)
	args = append(args, extra...)
	return cx.NewArray(args)
}

// exec is the interpreter loop. See run.
func (ag *agent) exec(stop int) (result storage.Value, status execStatus) {
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			ag.sp = ag.frames[len(ag.frames)-1].top()
			status = execPanic
		}
	}()

	// The closure of the running function is read from its frame where it is needed, so that
	// the loop does not carry it from one instruction to the next.
	fr := &ag.frames[len(ag.frames)-1]
	fp := fr.fp
	pc := fr.pc
	// The active context is that of the running frame wherever exec reads it. runFrame installs it.
	if validateExec {
		ag.enterChecks()
	}
	// The capacity of regs is never read: it is sliced and passed on only through slices.Clip,
	// so that the loop does not carry it from one instruction to the next.
	regs := fr.registers(ag.stack)

	for {
		if validateExec {
			ag.checkStep(fp, pc, slices.Clip(regs))
		}
		i := &fp.Code[pc]
		pc++
		// The fast paths of the common instructions. Nothing in this switch calls a function
		// (TestExecFastPathsCallNothing checks it), so the values that the loop carries need
		// no stack copies on these paths. An instruction that leaves it runs in the switch
		// below, which handles every case of it. Undefined is written as Value{}, which needs
		// no load.
		switch i.Op {
		case bytecode.OpLoadK:
			regs[i.A] = fp.Consts[i.B]
			continue
		case bytecode.OpLoadInt:
			storage.SetNumber(&regs[i.A], float64(i.B))
			continue
		case bytecode.OpLoadUndef:
			regs[i.A] = storage.Value{}
			continue
		case bytecode.OpLoadNull:
			regs[i.A] = storage.Null
			continue
		case bytecode.OpLoadTrue:
			regs[i.A] = storage.True
			continue
		case bytecode.OpLoadFalse:
			regs[i.A] = storage.False
			continue
		case bytecode.OpLoadTDZ:
			regs[i.A] = tdz
			continue
		case bytecode.OpMove:
			regs[i.A] = regs[i.B]
			continue
		case bytecode.OpRequireCoercible:
			if !regs[i.A].IsNullish() {
				continue
			}
		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			c := &fp.Globals[i.C]
			if h := c.Holder; h != nil && h.Shape() == c.Shape {
				if validateExec {
					ag.active.checkGlobalHit(fp, pc-1, fp.Keys[i.B], c, false)
				}
				regs[i.A] = *c.Ref
				continue
			}
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict, bytecode.OpPutGlobal, bytecode.OpPutGlobalStrict:
			// Only setGlobal fills the cache of an assignment. A cached binding also resolves
			// the reference of OpPutGlobal.
			c := &fp.Globals[i.C]
			if h := c.Holder; h != nil && h.Shape() == c.Shape {
				if validateExec {
					ag.active.checkGlobalHit(fp, pc-1, fp.Keys[i.B], c, true)
				}
				*c.Ref = regs[i.A]
				continue
			}
		case bytecode.OpGetUpval:
			regs[i.A] = *ag.frames[len(ag.frames)-1].cl.Fn.Upvals[i.B].P
			continue
		case bytecode.OpSetUpval:
			*ag.frames[len(ag.frames)-1].cl.Fn.Upvals[i.B].P = regs[i.A]
			continue
		case bytecode.OpCopyPropToGlobal:
			g := &fp.Globals[fp.Code[i.C-1].C]
			if h := g.Holder; h == nil || h.Shape() != g.Shape {
				break
			}
			if validateExec {
				ag.active.checkGlobalHit(fp, int(i.C-1), fp.Keys[fp.Code[i.C-1].B], g, true)
			}
			regs[i.A] = storage.Value{}
			c := &fp.Caches[fp.Code[pc].C]
			o := regs[i.B].O
			if o == nil || c.Shape != o.Shape() {
				continue
			}
			if c.Kind == storage.CacheOwnGetter || c.Kind == storage.CacheGetter {
				// The binding is resolved. Dispatch the read with its own call site.
				i = &fp.Code[pc]
				pc++
				break
			}
			var v storage.Value
			switch c.Kind {
			case storage.CacheOwn:
				v = o.Raw().Slot(int(c.Slot))
			case storage.CacheProto:
				if c.Epoch != ag.ProtoEpoch {
					continue
				}
				v = *c.Ref
			default:
				continue
			}
			if validateExec {
				ag.active.checkEntryRead(fp, pc, regs[i.B], o, fp.Keys[fp.Code[pc].C], &c.CacheEntry)
			}
			regs[i.A+1] = v
			*g.Ref = v
			pc = int(i.C)
			continue
		case bytecode.OpResolveGlobal:
			c := &fp.Globals[i.C]
			if h := c.Holder; h == nil || h.Shape() != c.Shape {
				break
			}
			if validateExec {
				ag.active.checkGlobalExists(fp, pc-1, fp.Keys[i.B], c)
			}
			regs[i.A] = storage.Value{}
			// A named read may supply the value of the assignment.
			if n := &fp.Code[pc]; n.Op == bytecode.OpGetProp {
				c := &fp.Caches[n.C]
				if o := regs[n.B].O; o != nil && c.Kind == storage.CacheOwn && c.Shape == o.Shape() {
					if validateExec {
						ag.active.checkEntryRead(fp, pc, regs[n.B], o, fp.Keys[n.C], &c.CacheEntry)
					}
					regs[n.A] = o.Raw().Slot(int(c.Slot))
					pc++
				}
			}
			continue
		case bytecode.OpGetProp:
			// The destination is read first, so that the instruction need not stay in
			// registers across the tests of the cache.
			if a, o := i.A, regs[i.B].O; o != nil {
				c := &fp.Caches[i.C]
				e := &c.CacheEntry
				if o.Shape() != e.Shape {
					e = &c.Second
					if o.Shape() != e.Shape {
						break
					}
				}
				if e.Kind != storage.CacheOwn {
					if e.Kind != storage.CacheProto || e.Epoch != ag.ProtoEpoch {
						break
					}
					if validateExec {
						ag.active.checkEntryRead(fp, pc-1, regs[i.B], o, fp.Keys[i.C], e)
					}
					regs[a] = *e.Ref
					continue
				}
				if validateExec {
					ag.active.checkEntryRead(fp, pc-1, regs[i.B], o, fp.Keys[i.C], e)
				}
				regs[a] = o.Raw().Slot(int(e.Slot))
				continue
			}
		case bytecode.OpGetPropChain:
			if a, o := i.A, regs[i.B].O; o != nil {
				c := &fp.Caches[i.C]
				e := &c.CacheEntry
				if o.Shape() != e.Shape {
					e = &c.Second
					if o.Shape() != e.Shape {
						break
					}
				}
				var w storage.Value
				if e.Kind != storage.CacheOwn {
					if e.Kind != storage.CacheProto || e.Epoch != ag.ProtoEpoch {
						break
					}
					w = *e.Ref
				} else {
					w = o.Raw().Slot(int(e.Slot))
				}
				if validateExec {
					ag.active.checkEntryRead(fp, pc-1, regs[i.B], o, fp.Keys[i.C], e)
				}
				regs[a] = w
				// Consume the next named read of w if its cache supplies a data property.
				if o := w.O; o != nil {
					n := &fp.Code[pc]
					c := &fp.Caches[n.C]
					e := &c.CacheEntry
					if o.Shape() != e.Shape {
						e = &c.Second
						if o.Shape() != e.Shape {
							continue
						}
					}
					if e.Kind != storage.CacheOwn {
						if e.Kind != storage.CacheProto || e.Epoch != ag.ProtoEpoch {
							continue
						}
						if validateExec {
							ag.active.checkEntryRead(fp, pc, w, o, fp.Keys[n.C], e)
						}
						regs[n.A] = *e.Ref
					} else {
						if validateExec {
							ag.active.checkEntryRead(fp, pc, w, o, fp.Keys[n.C], e)
						}
						regs[n.A] = o.Raw().Slot(int(e.Slot))
					}
					pc++
				}
				continue
			}
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			if o := regs[i.A].O; o != nil {
				c := &fp.Caches[i.B]
				e := &c.CacheEntry
				if o.Shape() != e.Shape {
					e = &c.Second
					if o.Shape() != e.Shape {
						break
					}
				}
				if e.Kind == storage.CacheOwn {
					if validateExec {
						ag.active.checkStoreHit(fp, pc-1, regs[i.A], fp.Keys[i.B], o, e.Slot)
					}
					o.Raw().SetSlot(int(e.Slot), regs[i.C])
					continue
				}
				if e.Kind == storage.CacheAdd && o.HasProperties() && e.Epoch == ag.ProtoEpoch && o.Flags()&storage.ObjIsProto == 0 {
					if n := o.Raw().SlotCount(); n < o.Raw().SlotRoom() {
						if validateExec {
							ag.active.checkAddHit(fp, pc-1, o, fp.Keys[i.B], e.Next)
						}
						o.Raw().SetShape(e.Next)
						o.Raw().ExtendSlots(n + 1)
						o.Raw().SetSlot(n, regs[i.C])
						continue
					}
				}
			}
		case bytecode.OpGetArgsLength:
			// The argument count, while the arguments object is not materialized. Once it is,
			// the length of the object, as in OpGetLength.
			if a := &regs[i.A]; regs[i.B].O == nil {
				storage.SetNumber(a, float64(ag.frames[len(ag.frames)-1].argc))
				continue
			}
			fallthrough
		case bytecode.OpGetLength:
			if o := regs[i.B].O; o != nil && o.Kind == storage.KindArray {
				if validateExec {
					ag.active.checkLengthHit(fp, pc-1, regs[i.B], float64(o.Length()))
				}
				storage.SetNumber(&regs[i.A], float64(o.Length()))
				continue
			}
			// The destination and the cache are read first, so that the instruction need not
			// stay in registers across the tests, as in OpGetProp.
			if a, c, o := i.A, &fp.Caches[i.C], regs[i.B].O; o != nil {
				// The length of a string known to be ASCII, as in Value.StringLength.
				if o.Kind == storage.KindString && o.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII) == storage.ObjASCIIKnown|storage.ObjASCII {
					if validateExec {
						ag.active.checkLengthHit(fp, pc-1, regs[i.B], float64(len(o.Text())))
					}
					storage.SetNumber(&regs[a], float64(len(o.Text())))
					continue
				}
				// The length of a fixed-length typed array in the bounds of its buffer, when the
				// cache says that the length is the original getter, as in getNamed. The data of a
				// shared or detached buffer is empty, so only an empty view of one passes the
				// bounds check.
				if t, ok := o.Ext.(*storage.TypedArray); ok && !t.Tracking && t.Offset+t.FixedLength*t.Type.Size() <= len(t.Data.Data) {
					if o.Shape() == c.Shape && c.Kind == storage.CacheGetter && c.Epoch == ag.ProtoEpoch && c.Holder == ag.active.realm.Intrinsics[storage.IntrinsicTypedArrayPrototypeLengthGetter] {
						if validateExec {
							ag.active.checkTypedArrayLengthHit(fp, pc-1, regs[i.B], float64(t.FixedLength))
						}
						storage.SetNumber(&regs[a], float64(t.FixedLength))
						continue
					}
				}
				// A length data property that the cache locates, as in OpGetProp.
				e := &c.CacheEntry
				if o.Shape() != e.Shape {
					e = &c.Second
				}
				if o.Shape() == e.Shape {
					if e.Kind == storage.CacheOwn {
						if validateExec {
							ag.active.checkEntryRead(fp, pc-1, regs[i.B], o, ag.Key(storage.KeyLength), e)
						}
						regs[a] = o.Raw().Slot(int(e.Slot))
						continue
					}
					if e.Kind == storage.CacheProto && e.Epoch == ag.ProtoEpoch {
						if validateExec {
							ag.active.checkEntryRead(fp, pc-1, regs[i.B], o, ag.Key(storage.KeyLength), e)
						}
						regs[a] = *e.Ref
						continue
					}
				}
			}
		case bytecode.OpGetIndex:
			v, k := regs[i.B], regs[i.C]
			if o := v.O; o != nil && k.O == storage.NumberObject {
				if j := int(k.N); float64(j) == k.N && uint(j) < uint(o.Raw().ElemCount()) {
					if e := o.Raw().Elem(j); e.O != storage.HoleObject {
						regs[i.A] = e
						continue
					}
				}
				// A unit of a string known to be ASCII, as in asciiUnitAt. An index in its range is an
				// own property that nothing shadows.
				if o.Kind == storage.KindString && o.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII) == storage.ObjASCIIKnown|storage.ObjASCII {
					if j := int(k.N); float64(j) == k.N && uint(j) < uint(len(o.Text())) {
						if o.Flags()&storage.ObjReversed != 0 {
							j = len(o.Text()) - 1 - j
						}
						regs[i.A] = ag.Strings.Ascii[o.Text()[j]]
						continue
					}
				}
				// The elements of a typed array of numbers other than Float16 whose bytes are all in
				// the data of its buffer, as in TypedArrayGet. The checks and the destination come
				// before the data is loaded: a path to the switch below, or a use of i, that needs
				// a register the load took makes the compiler store loop values at the head of the
				// loop on amd64. The functions of encoding/binary do not inline into exec, so the
				// bytes are combined here.
				if t, ok := o.Ext.(*storage.TypedArray); ok && t.Type < element.BigInt64 && t.Type != element.Float16 {
					if j := int(k.N); float64(j) == k.N && !t.Tracking && uint(j) < uint(t.FixedLength) {
						if r, s := &regs[i.A], t.Type.Size(); t.Offset+t.FixedLength*s <= len(t.Data.Data) {
							b := t.Data.Data[t.Offset+j*s:]
							var x float64
							switch t.Type {
							case element.Int8:
								x = float64(int8(b[0]))
							case element.Uint8, element.Uint8Clamped:
								x = float64(b[0])
							case element.Int16:
								_ = b[1]
								x = float64(int16(uint16(b[0]) | uint16(b[1])<<8))
							case element.Uint16:
								_ = b[1]
								x = float64(uint16(b[0]) | uint16(b[1])<<8)
							case element.Int32:
								_ = b[3]
								x = float64(int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
							case element.Uint32:
								_ = b[3]
								x = float64(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
							case element.Float32:
								_ = b[3]
								x = float64(math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
							default:
								_ = b[7]
								x = math.Float64frombits(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
									uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
							}
							storage.SetNumber(r, x)
							continue
						}
					}
				}
			}
		case bytecode.OpGetIndexInt:
			if o := regs[i.B].O; o != nil && uint(i.C) < uint(o.Raw().ElemCount()) {
				if e := o.Raw().Elem(int(i.C)); e.O != storage.HoleObject {
					regs[i.A] = e
					continue
				}
			}
			// The same as OpGetIndex for a typed array.
			if o := regs[i.B].O; o != nil {
				if t, ok := o.Ext.(*storage.TypedArray); ok && t.Type < element.BigInt64 && t.Type != element.Float16 && !t.Tracking && uint(i.C) < uint(t.FixedLength) {
					if j, r, s := int(i.C), &regs[i.A], t.Type.Size(); t.Offset+t.FixedLength*s <= len(t.Data.Data) {
						b := t.Data.Data[t.Offset+j*s:]
						var x float64
						switch t.Type {
						case element.Int8:
							x = float64(int8(b[0]))
						case element.Uint8, element.Uint8Clamped:
							x = float64(b[0])
						case element.Int16:
							_ = b[1]
							x = float64(int16(uint16(b[0]) | uint16(b[1])<<8))
						case element.Uint16:
							_ = b[1]
							x = float64(uint16(b[0]) | uint16(b[1])<<8)
						case element.Int32:
							_ = b[3]
							x = float64(int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
						case element.Uint32:
							_ = b[3]
							x = float64(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
						case element.Float32:
							_ = b[3]
							x = float64(math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
						default:
							_ = b[7]
							x = math.Float64frombits(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
								uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
						}
						storage.SetNumber(r, x)
						continue
					}
				}
				// The same as OpGetIndex for a string.
				if o.Kind == storage.KindString && o.Flags()&(storage.ObjASCIIKnown|storage.ObjASCII) == storage.ObjASCIIKnown|storage.ObjASCII && uint(i.C) < uint(len(o.Text())) {
					j := int(i.C)
					if o.Flags()&storage.ObjReversed != 0 {
						j = len(o.Text()) - 1 - j
					}
					regs[i.A] = ag.Strings.Ascii[o.Text()[j]]
					continue
				}
			}
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict:
			// OpSetIndexInt's case at the end of this switch has the same fast paths for the key in
			// the instruction; a change to one belongs in the other.
			v, k := regs[i.A], regs[i.B]
			if o := v.O; o != nil && k.O == storage.NumberObject {
				if j := int(k.N); float64(j) == k.N && uint(j) < uint(o.Raw().ElemCount()) {
					if o.Raw().Elem(j).O != storage.HoleObject && !o.Raw().ElemsShared() {
						o.Raw().SetElem(j, regs[i.C])
						continue
					}
					// A store into a hole or into shared elements takes the switch below.
					break
				}
				// The stores of a number into a typed array of numbers other than Float16, as in
				// typedArraySet, with every check before the data is loaded as in OpGetIndex. A
				// number beyond the range of int64, NaN included, goes to the switch below, so that
				// an integer element takes the low bytes of int64(f).
				if t, ok := o.Ext.(*storage.TypedArray); ok && t.NumberStore && t.Type != element.Float16 {
					if j, x := int(k.N), regs[i.C]; x.O == storage.NumberObject && float64(j) == k.N && !t.Tracking && uint(j) < uint(t.FixedLength) && numconv.InInt64Range(x.N) {
						if s := t.Type.Size(); t.Offset+t.FixedLength*s <= len(t.Data.Data) {
							b, f := t.Data.Data[t.Offset+j*s:], x.N
							// The most common types are tested first.
							if u := uint64(int64(f)); t.Type == element.Int32 || t.Type == element.Uint32 {
								_ = b[3]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
								b[2] = byte(u >> 16)
								b[3] = byte(u >> 24)
							} else if t.Type == element.Float64 {
								bits := math.Float64bits(f)
								_ = b[7]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
								b[4] = byte(bits >> 32)
								b[5] = byte(bits >> 40)
								b[6] = byte(bits >> 48)
								b[7] = byte(bits >> 56)
							} else if t.Type == element.Int8 || t.Type == element.Uint8 {
								b[0] = byte(u)
							} else if t.Type == element.Float32 {
								bits := math.Float32bits(float32(f))
								_ = b[3]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
							} else if t.Type == element.Uint8Clamped {
								// ToUint8Clamp, which rounds half to even.
								c := byte(0)
								if f >= 255 {
									c = 255
								} else if f > 0 {
									c = byte(f)
									if r := f - float64(c); r > 0.5 || r == 0.5 && c&1 != 0 {
										c++
									}
								}
								b[0] = c
							} else {
								_ = b[1]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
							}
							continue
						}
					}
					// The append below does not apply to a typed array.
					break
				}
				// An append to a plain array that has room for it, as in the switch below, with the
				// conditions of ElemsResizable and NewElementsPlain written out.
				if j := int(k.N); j == o.Raw().ElemCount() && j < o.Raw().ElemRoom() &&
					o.Kind == storage.KindArray && uint32(j) == o.Length() && o.Flags() == 0 && float64(j) == k.N && o.Shape() == ag.active.realm.Shapes.Array {
					if a := ag.active.realm.Intrinsics[storage.IntrinsicArrayPrototype]; a.Raw().ElemCount() == 0 && a.Flags()&storage.ObjSparse == 0 {
						if p := a.Shape().Proto; p == ag.active.realm.Intrinsics[storage.IntrinsicObjectPrototype] && p.Raw().ElemCount() == 0 && p.Flags()&storage.ObjSparse == 0 {
							if validateExec {
								ag.active.checkAppend(o)
							}
							// Reading x first keeps i.C from being read again after the
							// store to the length.
							x := regs[i.C]
							o.Raw().ExtendElems(j + 1)
							o.Raw().SetElem(j, x)
							o.Raw().SetLength(o.Length() + 1)
							continue
						}
					}
				}
			}
		case bytecode.OpAdd:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N+y.N)
				continue
			}
		case bytecode.OpSub:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N-y.N)
				continue
			}
		case bytecode.OpMul:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N*y.N)
				continue
			}
		case bytecode.OpDiv:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N/y.N)
				continue
			}
		case bytecode.OpMod:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if xi, yi := int32(x.N), int32(y.N); xi > 0 && yi > 0 && float64(xi) == x.N && float64(yi) == y.N {
					storage.SetNumber(&regs[i.A], float64(xi%yi))
					continue
				}
			}
		case bytecode.OpBitAnd:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))&int32(int64(y.N))))
				continue
			}
		case bytecode.OpBitOr:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))|int32(int64(y.N))))
				continue
			}
		case bytecode.OpBitXor:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))^int32(int64(y.N))))
				continue
			}
		case bytecode.OpShl:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))<<(uint32(int64(y.N))&31)))
				continue
			}
		case bytecode.OpShr:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))>>(uint32(int64(y.N))&31)))
				continue
			}
		case bytecode.OpUShr:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject && numconv.InInt64Range(x.N) && numconv.InInt64Range(y.N) {
				storage.SetNumber(&regs[i.A], float64(uint32(int64(x.N))>>(uint32(int64(y.N))&31)))
				continue
			}
		case bytecode.OpAddK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N+fp.Consts[i.C].N)
				continue
			}
		case bytecode.OpSubK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N-fp.Consts[i.C].N)
				continue
			}
		case bytecode.OpMulK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N*fp.Consts[i.C].N)
				continue
			}
		case bytecode.OpDivK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N/fp.Consts[i.C].N)
				continue
			}
		case bytecode.OpModK:
			if x, y := regs[i.B], fp.Consts[i.C]; x.O == storage.NumberObject {
				if xi, yi := int32(x.N), int32(y.N); xi > 0 && yi > 0 && float64(xi) == x.N && float64(yi) == y.N {
					storage.SetNumber(&regs[i.A], float64(xi%yi))
					continue
				}
			}
		case bytecode.OpBitAndK:
			if x := regs[i.B]; x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))&int32(fp.Consts[i.C].N)))
				continue
			}
		case bytecode.OpBitOrK:
			if x := regs[i.B]; x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))|int32(fp.Consts[i.C].N)))
				continue
			}
		case bytecode.OpShlK:
			if x := regs[i.B]; x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))<<(uint32(fp.Consts[i.C].N)&31)))
				continue
			}
		case bytecode.OpShrK:
			if x := regs[i.B]; x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				storage.SetNumber(&regs[i.A], float64(int32(int64(x.N))>>(uint32(fp.Consts[i.C].N)&31)))
				continue
			}
		case bytecode.OpUShrK:
			if x := regs[i.B]; x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				storage.SetNumber(&regs[i.A], float64(uint32(int64(x.N))>>(uint32(fp.Consts[i.C].N)&31)))
				continue
			}
		case bytecode.OpNeg:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], -x.N)
				continue
			}
		case bytecode.OpInc:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N+1)
				continue
			}
		case bytecode.OpDec:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N-1)
				continue
			}
		case bytecode.OpPostInc:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N)
				storage.SetNumber(&regs[i.B], x.N+1)
				continue
			}
		case bytecode.OpPostDec:
			if x := regs[i.B]; x.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N)
				storage.SetNumber(&regs[i.B], x.N-1)
				continue
			}
		case bytecode.OpIncGetIndex:
			if x := regs[i.B]; x.O == storage.NumberObject {
				k := x.N + 1
				storage.SetNumber(&regs[i.A], k)
				// The next instruction is an OpGetIndex with the key k. It runs here only when it
				// reads an element, as in its own fast path, and runs on its own otherwise.
				n := &fp.Code[pc]
				if o := regs[n.B].O; o != nil {
					if j := int(k); float64(j) == k && uint(j) < uint(o.Raw().ElemCount()) {
						if e := o.Raw().Elem(j); e.O != storage.HoleObject {
							regs[n.A] = e
							pc++
						}
					}
				}
				continue
			}
		case bytecode.OpPostIncGetIndex:
			// Only an integer key can read an element, and a counter that is not one takes the
			// switch below. The test comes before the stores: a write barrier does not keep
			// floating-point registers, so no float may be needed after the second store.
			if x := regs[i.B]; x.O == storage.NumberObject {
				if k, j := x.N, int(x.N); float64(j) == k {
					storage.SetNumber(&regs[i.A], k)
					storage.SetNumber(&regs[i.B], k+1)
					// As in OpIncGetIndex.
					n := &fp.Code[pc]
					if o := regs[n.B].O; o != nil && uint(j) < uint(o.Raw().ElemCount()) {
						if e := o.Raw().Elem(j); e.O != storage.HoleObject {
							regs[n.A] = e
							pc++
						}
					}
					continue
				}
			}
		case bytecode.OpLt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Bool(x.N < y.N)
				continue
			}
		case bytecode.OpLe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Bool(x.N <= y.N)
				continue
			}
		case bytecode.OpGt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Bool(x.N > y.N)
				continue
			}
		case bytecode.OpGe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Bool(x.N >= y.N)
				continue
			}
		case bytecode.OpEq, bytecode.OpNe:
			// IsLooselyEqual, for the operands it can tell without a call, as in looseEquals:
			// two numbers, one value twice, an undefined or null operand, two objects, and two
			// interned strings, which differ in content when they are different objects.
			x, y := regs[i.B], regs[i.C]
			eq, known := false, true
			if x.O == storage.NumberObject && y.O == storage.NumberObject {
				eq = x.N == y.N
			} else if x.O == y.O && math.Float64bits(x.N) == math.Float64bits(y.N) {
				eq = true
			} else if xn, yn := x.IsNullish(), y.IsNullish(); xn || yn {
				eq = xn && yn || x.IsHTMLDDA() || y.IsHTMLDDA()
			} else if x.O.Kind >= storage.KindFirstObject && y.O.Kind >= storage.KindFirstObject {
				eq = x.O == y.O
			} else if x.O == y.O || x.O.Flags()&y.O.Flags()&storage.ObjInterned == 0 {
				known = false
			}
			if known {
				regs[i.A] = storage.Bool(eq == (i.Op == bytecode.OpEq))
				continue
			}
		case bytecode.OpNot:
			// ToBoolean, for the kinds it can tell without a call, as in OpJumpIf.
			if v := regs[i.B]; v.O == storage.NumberObject {
				regs[i.A] = storage.Bool(v.N == 0 || v.N != v.N)
				continue
			} else if o := v.O; o == nil || o.Kind <= storage.KindFalse {
				regs[i.A] = storage.Bool(true)
				continue
			} else if o.Kind == storage.KindTrue || o.Kind >= storage.KindObject {
				// storage.KindHTMLDDA is below storage.KindObject and takes the slow path.
				regs[i.A] = storage.Bool(false)
				continue
			}
		case bytecode.OpJump:
			if int(i.A) >= pc || !ag.InterruptFlag.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			// ToBoolean, for the kinds it can tell without a call.
			v := regs[i.B]
			t, known := false, true
			if o := v.O; o == storage.NumberObject {
				t = v.N != 0 && v.N == v.N
			} else if o == nil {
				t = false
			} else if k := o.Kind; k == storage.KindTrue {
				t = true
			} else if k <= storage.KindFalse {
				t = false
			} else if k >= storage.KindObject {
				// storage.KindHTMLDDA is below storage.KindObject and takes the slow path.
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
			if x := regs[i.B]; x.IsNullish() || i.C != 0 && x.IsHTMLDDA() {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfNotNullish:
			if x := regs[i.B]; !x.IsNullish() && (i.C == 0 || !x.IsHTMLDDA()) {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfUndef:
			if regs[i.B].O == nil {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfNotUndef:
			if regs[i.B].O != nil {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotLt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N < y.N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotLe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N <= y.N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N > y.N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N >= y.N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			// The same cases as OpEq.
			x, y := regs[i.B], regs[i.C]
			eq, known := false, true
			if x.O == storage.NumberObject && y.O == storage.NumberObject {
				eq = x.N == y.N
			} else if x.O == y.O && math.Float64bits(x.N) == math.Float64bits(y.N) {
				eq = true
			} else if xn, yn := x.IsNullish(), y.IsNullish(); xn || yn {
				eq = xn && yn || x.IsHTMLDDA() || y.IsHTMLDDA()
			} else if x.O.Kind >= storage.KindFirstObject && y.O.Kind >= storage.KindFirstObject {
				eq = x.O == y.O
			} else if x.O == y.O || x.O.Flags()&y.O.Flags()&storage.ObjInterned == 0 {
				known = false
			}
			if known {
				if eq != (i.Op == bytecode.OpJumpNotEq) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotSEq:
			// The cases of IsStrictlyEqual that need no call: two numbers, one value twice, and two
			// interned strings, which differ in content when they are different objects.
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if x.N != y.N {
					pc = int(i.A)
				}
				continue
			} else if x.O == y.O && math.Float64bits(x.N) == math.Float64bits(y.N) {
				continue
			} else if x.O != y.O && x.O != nil && y.O != nil && x.O.Flags()&y.O.Flags()&storage.ObjInterned != 0 {
				pc = int(i.A)
				continue
			}
		case bytecode.OpJumpNotSNe:
			// The same cases as OpJumpNotSEq.
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if x.N == y.N {
					pc = int(i.A)
				}
				continue
			} else if x.O == y.O && math.Float64bits(x.N) == math.Float64bits(y.N) {
				pc = int(i.A)
				continue
			} else if x.O != y.O && x.O != nil && y.O != nil && x.O.Flags()&y.O.Flags()&storage.ObjInterned != 0 {
				continue
			}
		case bytecode.OpStrictEq, bytecode.OpStrictNe:
			// The same cases as OpJumpNotSEq.
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Bool((x.N == y.N) == (i.Op == bytecode.OpStrictEq))
				continue
			} else if x.O == y.O && math.Float64bits(x.N) == math.Float64bits(y.N) {
				regs[i.A] = storage.Bool(i.Op == bytecode.OpStrictEq)
				continue
			} else if x.O != y.O && x.O != nil && y.O != nil && x.O.Flags()&y.O.Flags()&storage.ObjInterned != 0 {
				regs[i.A] = storage.Bool(i.Op != bytecode.OpStrictEq)
				continue
			}
		case bytecode.OpJumpNotLtK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N < fp.Consts[i.C].N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotLeK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N <= fp.Consts[i.C].N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGtK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N > fp.Consts[i.C].N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotGeK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N >= fp.Consts[i.C].N) {
					pc = int(i.A)
				}
				continue
			}
		case bytecode.OpJumpNotSEqK:
			if x := regs[i.B]; x.O != storage.NumberObject || x.N != fp.Consts[i.C].N {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSNeK:
			if x := regs[i.B]; x.O == storage.NumberObject && x.N == fp.Consts[i.C].N {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSEqP:
			if regs[i.B].O != primObjects[i.C] {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpNotSNeP:
			if regs[i.B].O == primObjects[i.C] {
				pc = int(i.A)
			}
			continue
		case bytecode.OpJumpIfInt:
			if x := regs[i.B]; x.O == storage.NumberObject && x.N == float64(i.C) {
				pc = int(i.A)
			}
			continue
		case bytecode.OpInstanceof:
			// An ordinary function whose prototype property the cache holds, with the
			// @@hasInstance of %Function.prototype%: OrdinaryHasInstance walks the prototype
			// chain of b, unless a proxy is on it.
			if co := regs[i.C].O; co != nil && !ag.InterruptFlag.Load() {
				c := &fp.Caches[fp.Code[pc].A]
				if co.Shape() == c.Shape && c.Kind == storage.CacheOwn && c.Epoch == ag.ProtoEpoch && co.Class == storage.ClassFunction && co.Fn != nil && co.Fn.Bound == nil {
					if p := co.Raw().Slot(int(c.Slot)).O; p != nil && p.Kind >= storage.KindFirstObject {
						r, known := false, true
						if o := regs[i.B].O; o != nil && o.Kind >= storage.KindFirstObject {
							for h := o; ; {
								if h.Class == storage.ClassProxy {
									known = false
									break
								}
								if h = h.Shape().Proto; h == nil {
									break
								}
								if h == p {
									r = true
									break
								}
							}
						}
						if known {
							if validateExec {
								ag.active.checkInstanceofHit(fp, pc-1, regs[i.B], co, c.Slot, r)
							}
							regs[i.A] = storage.Bool(r)
							pc++
							continue
						}
					}
				}
			}
		case bytecode.OpArrayPush:
			if o := regs[i.A].O; o.Raw().ElemCount() < o.Raw().ElemRoom() {
				n := o.Raw().ElemCount()
				o.Raw().ExtendElems(n + 1)
				o.Raw().SetElem(n, regs[i.B])
				o.Raw().SetLength(uint32(n + 1))
				continue
			}
		case bytecode.OpDefineFieldK:
			o := regs[i.A].O
			if c := &fp.Caches[i.B]; o.Shape() == c.Shape && c.Kind == storage.CacheAdd {
				if n := o.Raw().SlotCount(); n < o.Raw().SlotRoom() {
					if validateExec {
						ag.active.checkDefineHit(fp, pc-1, o, fp.Keys[i.B], c.Next)
					}
					o.Raw().SetShape(c.Next)
					o.Raw().ExtendSlots(n + 1)
					o.Raw().SetSlot(n, regs[i.C])
					continue
				}
			}
		case bytecode.OpGetMethod, bytecode.OpCall, bytecode.OpCallUndef, bytecode.OpCallCall:
			if i.Op == bytecode.OpGetMethod {
				// A data property read through the cache, as in OpGetProp's case. An immediately
				// following OpCall on the same register runs here too.
				// The this value goes in place first, as the case below does too, so that it
				// does not stay in registers across the tests of the cache.
				a := i.A
				v := regs[i.B]
				regs[a+1] = v
				o := v.O
				if o == nil {
					break
				}
				c := &fp.Caches[i.C]
				e := &c.CacheEntry
				if o.Shape() != e.Shape {
					e = &c.Second
					if o.Shape() != e.Shape {
						break
					}
				}
				var m storage.Value
				if e.Kind != storage.CacheOwn {
					if e.Kind != storage.CacheProto || e.Epoch != ag.ProtoEpoch {
						break
					}
					m = *e.Ref
				} else {
					m = o.Raw().Slot(int(e.Slot))
				}
				if validateExec {
					ag.active.checkEntryRead(fp, pc-1, v, o, fp.Keys[i.C], e)
				}
				n := &fp.Code[pc]
				regs[a] = m
				if n.Op != bytecode.OpCall || n.B != a {
					continue
				}
				i = n
				pc++
			}
			if ag.InterruptFlag.Load() {
				break
			}
			// f is the callee and first the register of its first argument. The this value is in
			// the register before first, and is read only once the frame is certain, so that it
			// takes no registers while the frame is checked.
			b := int(i.B)
			f, first, argc := regs[b].O, b+2, int(i.C)
			if i.Op == bytecode.OpCallCall {
				if f != ag.active.realm.Intrinsics[storage.IntrinsicFunctionPrototypeCall] {
					f = nil
				} else {
					f, first, argc = regs[b+1].O, b+3, max(argc-1, 0)
				}
			}
			if !fp.HeapRegs && f != nil && f.Fn != nil {
				if c := f.Fn.Code; c != nil && c.InlineFrames && c.Realm == fp.Realm {
					n := len(ag.frames)
					argBase := ag.frames[n-1].base + first
					// The caller's registers already cover all the arguments.
					end := argBase + c.Nregs + 1
					// The frames have room for at most maxFrames.
					if (argc <= c.Nparams || !c.NeedsExtra) && end+stackHeadroom <= len(ag.stack) && n < cap(ag.frames) {
						var this storage.Value
						if i.Op != bytecode.OpCallUndef && (i.Op != bytecode.OpCallCall || i.C > 0) {
							this = regs[first-1]
						}
						if site := c.ReturnedProperty; site != nil && this.IsObject() {
							if e := site.Entry(this.O.Shape()); e != nil && e.Kind == storage.CacheOwn {
								if validateExec {
									ag.active.checkReturnedProperty(c, this, e)
								}
								regs[i.A] = this.O.Raw().Slot(int(e.Slot))
								continue
							}
						}
						// Reuse the available stack and frame storage for a prepared call. This builds
						// the frame that pushFrame would, without calling it, as nothing in this
						// switch may call; OpApplyFwd's case in the switch below builds it the same
						// way, and a change to one of the three belongs in the others.
						ag.frames[n-1].pc = pc
						ag.stackHigh = max(ag.stackHigh, end)
						if from, to := c.FirstCleared(argc), c.Nclear; from < to {
							clearFrameRegs(ag.stack[argBase:], from, to)
						}
						ag.frames = ag.frames[:n+1]
						ag.framesHigh = max(ag.framesHigh, n+1)
						fr := &ag.frames[n]
						fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
						// The callee belongs to the current realm, which is that of the running frame.
						fr.pc, fr.base, fr.ret, fr.isNew, fr.plainReturn, fr.argc, fr.extraBase = 0, argBase, i.A, false, true, argc, -1
						ag.sp = end
						fp, pc = c, 0
						regs = ag.stack[argBase:end]
						if r := c.ThisReg; r >= 0 && (!c.ThisSloppy || this.O != nil && this.O.Kind >= storage.KindFirstObject) {
							// Do what the first instruction does, and start after it.
							regs[r] = this
							pc = 1
						}
						continue
					}
				} else if c == nil && argc == 2 {
					// Math.min and Math.max of two numbers. Go's min and max of floats give NaN
					// when an argument is NaN and order -0 below +0, as the specification does.
					if info := f.Fn.Info; info != nil && info.Extremum != ExtremumNone {
						if x, y := regs[first], regs[first+1]; x.O == storage.NumberObject && y.O == storage.NumberObject {
							if info.Extremum == ExtremumMin {
								storage.SetNumber(&regs[i.A], min(x.N, y.N))
							} else {
								storage.SetNumber(&regs[i.A], max(x.N, y.N))
							}
							continue
						}
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
			r := storage.Value{}
			if i.Op == bytecode.OpReturn {
				r = regs[i.A]
			}
			n := len(ag.frames) - 1
			if done := &ag.frames[n]; done.plainReturn && n != stop && (len(ag.openUpvals) == 0 || ag.openUpvals[len(ag.openUpvals)-1].Index < done.base) {
				if done.isNew && (r.O == nil || r.O.Kind < storage.KindFirstObject) {
					r = done.this
				}
				ret := done.ret
				ag.frames = ag.frames[:n]
				fr := &ag.frames[n-1]
				fp = fr.fp
				pc = fr.pc
				if fp.HeapRegs {
					regs = fr.gen.regs
					ag.sp = fr.base
				} else {
					regs = ag.stack[fr.base : fr.base+fp.Nregs+1]
					ag.sp = fr.base + fp.Nregs + 1
				}
				regs[ret] = r
				continue
			}
		case bytecode.OpGetArgsIndex:
			// An argument, while the arguments object is not materialized, where the call left
			// it: in a parameter register, or among the surplus arguments below the registers.
			// Once it is, an element that the object stores, as in OpGetIndex. The destination
			// is read before the tests and the argument after them, as in OpGetIndex.
			if a, k := &regs[i.A], regs[i.C]; k.O == storage.NumberObject {
				if o := regs[i.B].O; o == nil {
					fr := &ag.frames[len(ag.frames)-1]
					if j := int(k.N); float64(j) == k.N && uint(j) < uint(fr.argc) {
						if n := fp.Nparams; j < n {
							*a = regs[j]
						} else {
							*a = ag.stack[fr.extraBase+j-n]
						}
						continue
					}
				} else if j := int(k.N); float64(j) == k.N && uint(j) < uint(o.Raw().ElemCount()) {
					if e := o.Raw().Elem(j); e.O != storage.HoleObject {
						*a = e
						continue
					}
				}
			}
		case bytecode.OpGetArgsIndexInt:
			// The fast paths of OpGetArgsIndex's case for the index c.
			if o := regs[i.B].O; o == nil {
				fr := &ag.frames[len(ag.frames)-1]
				if j := int(i.C); uint(j) < uint(fr.argc) {
					if n := fp.Nparams; j < n {
						regs[i.A] = regs[j]
					} else {
						regs[i.A] = ag.stack[fr.extraBase+j-n]
					}
					continue
				}
			} else if j := int(i.C); uint(j) < uint(o.Raw().ElemCount()) {
				if e := o.Raw().Elem(j); e.O != storage.HoleObject {
					regs[i.A] = e
					continue
				}
			}
		case bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
			// The fast paths of OpSetIndex's case, in the same order, for the key b.
			if o := regs[i.A].O; o != nil {
				j := int(i.B)
				if uint(j) < uint(o.Raw().ElemCount()) {
					if o.Raw().Elem(j).O != storage.HoleObject && !o.Raw().ElemsShared() {
						o.Raw().SetElem(j, regs[i.C])
						continue
					}
					break
				}
				if t, ok := o.Ext.(*storage.TypedArray); ok && t.NumberStore && t.Type != element.Float16 {
					if x := regs[i.C]; x.O == storage.NumberObject && !t.Tracking && uint(j) < uint(t.FixedLength) && numconv.InInt64Range(x.N) {
						if s := t.Type.Size(); t.Offset+t.FixedLength*s <= len(t.Data.Data) {
							b, f := t.Data.Data[t.Offset+j*s:], x.N
							// The most common types are tested first.
							if u := uint64(int64(f)); t.Type == element.Int32 || t.Type == element.Uint32 {
								_ = b[3]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
								b[2] = byte(u >> 16)
								b[3] = byte(u >> 24)
							} else if t.Type == element.Float64 {
								bits := math.Float64bits(f)
								_ = b[7]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
								b[4] = byte(bits >> 32)
								b[5] = byte(bits >> 40)
								b[6] = byte(bits >> 48)
								b[7] = byte(bits >> 56)
							} else if t.Type == element.Int8 || t.Type == element.Uint8 {
								b[0] = byte(u)
							} else if t.Type == element.Float32 {
								bits := math.Float32bits(float32(f))
								_ = b[3]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
							} else if t.Type == element.Uint8Clamped {
								// ToUint8Clamp, which rounds half to even.
								c := byte(0)
								if f >= 255 {
									c = 255
								} else if f > 0 {
									c = byte(f)
									if r := f - float64(c); r > 0.5 || r == 0.5 && c&1 != 0 {
										c++
									}
								}
								b[0] = c
							} else {
								_ = b[1]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
							}
							continue
						}
					}
					break
				}
				if j == o.Raw().ElemCount() && j < o.Raw().ElemRoom() && o.Kind == storage.KindArray && uint32(j) == o.Length() && o.Flags() == 0 && o.Shape() == ag.active.realm.Shapes.Array {
					if a := ag.active.realm.Intrinsics[storage.IntrinsicArrayPrototype]; a.Raw().ElemCount() == 0 && a.Flags()&storage.ObjSparse == 0 {
						if p := a.Shape().Proto; p == ag.active.realm.Intrinsics[storage.IntrinsicObjectPrototype] && p.Raw().ElemCount() == 0 && p.Flags()&storage.ObjSparse == 0 {
							if validateExec {
								ag.active.checkAppend(o)
							}
							x := regs[i.C]
							o.Raw().ExtendElems(j + 1)
							o.Raw().SetElem(j, x)
							o.Raw().SetLength(o.Length() + 1)
							continue
						}
					}
				}
			}
		case bytecode.OpLoopIf:
			// The ToBoolean of OpJumpIf. A pending interruption takes the switch below.
			v := regs[i.B]
			t, known := false, true
			if o := v.O; o == storage.NumberObject {
				t = v.N != 0 && v.N == v.N
			} else if o == nil {
				t = false
			} else if k := o.Kind; k == storage.KindTrue {
				t = true
			} else if k <= storage.KindFalse {
				t = false
			} else if k >= storage.KindObject {
				t = true
			} else {
				known = false
			}
			if known {
				if !t {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N < y.N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N <= y.N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGt:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N > y.N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGe:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				if !(x.N >= y.N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLtK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N < fp.Consts[i.C].N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLeK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N <= fp.Consts[i.C].N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGtK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N > fp.Consts[i.C].N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGeK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				if !(x.N >= fp.Consts[i.C].N) {
					continue
				}
				if !ag.InterruptFlag.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopSEqP:
			if regs[i.B].O != primObjects[i.C] {
				continue
			}
			if !ag.InterruptFlag.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopSNeP:
			if regs[i.B].O == primObjects[i.C] {
				continue
			}
			if !ag.InterruptFlag.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopNullish:
			// The kind of an object is read only when it can be an IsHTMLDDA object.
			if x := regs[i.B]; !x.IsNullish() && (i.C == 0 || !ag.madeHTMLDDA || !x.IsHTMLDDA()) {
				continue
			}
			if !ag.InterruptFlag.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopNotNullish:
			if x := regs[i.B]; x.IsNullish() || i.C != 0 && ag.madeHTMLDDA && x.IsHTMLDDA() {
				continue
			}
			if !ag.InterruptFlag.Load() {
				pc = int(i.A)
				continue
			}
		}
		// The state of a call in preparation, shared by every instruction that ends in a call.
		var (
			f           *storage.Object
			callThis    storage.Value
			firstArgReg int
			argBase     int
			argc        int
			ret         int32
			isNew       bool
			newTarget   *storage.Object
		)

		// Slow property operations take their operands by value across reentry.
		var receiver, key, rhs storage.Value
		var setter, name *storage.Object
		var site *storage.PropCache

		// Every instruction that the fast paths above leave, in full.
		switch i.Op {
		case bytecode.OpGetUpvalChecked:
			v := *ag.frames[len(ag.frames)-1].cl.Fn.Upvals[i.B].P
			if v.O == tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.throwTDZ(fp.Keys[i.C])
			}
			regs[i.A] = v
		case bytecode.OpCheckTDZ:
			if regs[i.A].O == tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.throwTDZ(fp.Keys[i.B])
			}
		case bytecode.OpThrowConstAssign:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(fp.Keys[i.A].Text()) + "'")
		case bytecode.OpThrowError:
			ag.frames[len(ag.frames)-1].pc = pc
			// Error text is an interned constant.
			ag.active.throwError(thrownErrorKinds[i.A], wtf8.ToValidUTF8(fp.Consts[i.B].O.Text()))

		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			goto readProperty
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict, bytecode.OpPutGlobal, bytecode.OpPutGlobalStrict:
			rhs = regs[i.A]
			goto writeProperty
		case bytecode.OpCopyPropToGlobal, bytecode.OpResolveGlobal:
			goto readProperty
		case bytecode.OpDeclareGlobalVar:
			ag.frames[len(ag.frames)-1].pc = pc
			var fn storage.Value
			if i.C >= 0 {
				fn = regs[i.C]
			}
			ag.active.declareGlobalVar(fp.Keys[i.A], int(i.B), fn)
		case bytecode.OpDeclareGlobalLex:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.declareGlobalLex(fp.Keys[i.A], i.B != 0)
		case bytecode.OpInitGlobalLex:
			ag.active.initGlobalLex(fp.Keys[i.B], regs[i.A])
		case bytecode.OpCheckGlobalDecl:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.checkGlobalDecl(fp.Keys[i.A], int(i.B))
		case bytecode.OpDeclareAnnexB:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = storage.Bool(ag.active.declareGlobalAnnexB(fp.Keys[i.B], int(i.C)))

		case bytecode.OpCloseUpvals:
			ag.closeFrameUpvals(&ag.frames[len(ag.frames)-1], int(i.A))
		case bytecode.OpClosure:
			p := fp.Protos[i.B]
			o, copies := storage.NewClosure(p)
			fn := o.Fn
			o.Kind = storage.KindFunction
			o.Class = storage.ClassFunction
			o.Raw().SetFlags(storage.ObjExotic)
			o.Raw().SetShape(ag.active.realm.Shapes.Function)
			if p.IsGenerator || p.IsAsync {
				o.Raw().SetShape(ag.active.FunctionShapeFor(p.ClosurePrototype()))
			}
			cur := &ag.frames[len(ag.frames)-1]
			for j, d := range p.Upvals {
				switch {
				case !d.Local:
					fn.Upvals[j] = cur.cl.Fn.Upvals[d.Index]
				case d.ByValue:
					u := &copies[0]
					copies = copies[1:]
					u.Val = regs[d.Index]
					u.P = &u.Val
					fn.Upvals[j] = u
				default:
					fn.Upvals[j] = ag.captureRegister(cur, int(d.Index))
				}
			}
			regs[i.A] = storage.Value{O: o}

		case bytecode.OpLoadThis:
			regs[i.A] = ag.frames[len(ag.frames)-1].this
		case bytecode.OpLoadThisSloppy:
			t := ag.frames[len(ag.frames)-1].this
			if t.O == nil || t.O.Kind < storage.KindFirstObject {
				if t.IsNullish() {
					t = storage.Value{O: ag.active.realm.GlobalThis}
				} else {
					t = storage.Value{O: ag.active.ToObject(t)}
				}
			}
			regs[i.A] = t
		case bytecode.OpLoadCallee:
			regs[i.A] = storage.Value{O: ag.frames[len(ag.frames)-1].cl}
		case bytecode.OpLoadNewTarget:
			if nt := ag.frames[len(ag.frames)-1].newTarget; nt != nil {
				regs[i.A] = storage.Value{O: nt}
			} else {
				regs[i.A] = storage.Undefined
			}
		case bytecode.OpLoadHome:
			if h := ag.frames[len(ag.frames)-1].cl.Fn.Home; h != nil {
				regs[i.A] = storage.Value{O: h}
			} else {
				regs[i.A] = storage.Undefined
			}
		case bytecode.OpArguments:
			cur := &ag.frames[len(ag.frames)-1]
			regs[i.A] = storage.Value{O: ag.active.newArguments(cur, i.B != 0)}
		case bytecode.OpRestArgs:
			cur := &ag.frames[len(ag.frames)-1]
			var rest *storage.Object
			if int(i.B) < cur.argc {
				rest = ag.active.restArray(cur, int(i.B))
			} else {
				rest = ag.active.NewArray(nil)
			}
			regs[i.A] = storage.Value{O: rest}
		case bytecode.OpGetArgsLength, bytecode.OpGetArgsIndex, bytecode.OpGetArgsIndexInt:
			receiver = regs[i.B]
			if receiver.IsUndefined() {
				// A prototype getter must see the arguments object shared by later reads.
				ag.frames[len(ag.frames)-1].pc = pc
				receiver = ag.active.forwardedArguments(i.B)
			}
			switch i.Op {
			case bytecode.OpGetArgsLength:
				name, site = ag.Key(storage.KeyLength), &fp.Caches[i.C]
			case bytecode.OpGetArgsIndex:
				key = regs[i.C]
			}
			goto readProperty

		case bytecode.OpGetMethod:
			// The this value is in place before a getter runs.
			regs[i.A+1] = regs[i.B]
			fallthrough
		case bytecode.OpGetProp, bytecode.OpGetPropChain:
			// Data properties in the cache take the fast path above. After an OpGetPropChain
			// that ends up here, the next instruction runs on its own.
			v := regs[i.B]
			c := &fp.Caches[i.C]
			o := v.O
			if o == storage.NumberObject {
				// getNamed starts at Number.prototype for a number. Its own slot
				// stays valid for its shape, and the method receives v as this.
				o = ag.active.realm.Intrinsics[storage.IntrinsicNumberPrototype]
				if c.Kind == storage.CacheOwn && c.Shape == o.Shape() {
					if validateExec {
						ag.active.checkEntryRead(fp, pc-1, v, o, fp.Keys[i.C], &c.CacheEntry)
					}
					regs[i.A] = o.Raw().Slot(int(c.Slot))
					continue
				}
			}
			if o != nil {
				if e := c.Entry(o.Shape()); e != nil {
					if e.Kind == storage.CacheGetter && e.Epoch == ag.ProtoEpoch {
						if validateExec {
							ag.active.checkGetterHit(fp, pc-1, v, fp.Keys[i.C], e.Holder)
						}
						f, callThis, argc, ret = e.Holder, v, 0, i.A
						goto callAbove
					}
					if e.Kind == storage.CacheOwnGetter {
						if validateExec {
							ag.active.checkOwnGetterHit(fp, pc-1, v, fp.Keys[i.C], o, e.Slot)
						}
						g := o.Raw().Slot(int(e.Slot)).Accessor().Getter
						if g == nil {
							regs[i.A] = storage.Undefined
							continue
						}
						f, callThis, argc, ret = g, v, 0, i.A
						goto callAbove
					}
				}
			}
			receiver, name, site = v, fp.Keys[i.C], c
			goto readProperty
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			v := regs[i.A]
			c := &fp.Caches[i.B]
			if o := v.O; o != nil {
				if e := c.Entry(o.Shape()); e != nil {
					switch e.Kind {
					case storage.CacheAdd:
						if o.HasProperties() && e.Epoch == ag.ProtoEpoch && o.Flags()&storage.ObjIsProto == 0 {
							if validateExec {
								ag.active.checkAddHit(fp, pc-1, o, fp.Keys[i.B], e.Next)
							}
							o.Raw().SetShape(e.Next)
							o.AppendSlot(regs[i.C])
							continue
						}
					case storage.CacheSetter:
						if e.Epoch == ag.ProtoEpoch {
							if validateExec {
								ag.active.checkSetterHit(fp, pc-1, v, fp.Keys[i.B], e.Holder)
							}
							setter, receiver, rhs = e.Holder, v, regs[i.C]
							goto writeProperty
						}
					}
				}
				// A store to the length of a plain array of a number that is a length no
				// greater than the current one runs no code, but a long truncation polls.
				// The kind turns most other receivers away with one load.
				if o.Kind == storage.KindArray && o.Shape() == ag.active.realm.Shapes.Array && fp.Keys[i.B] == ag.Key(storage.KeyLength) {
					// NaN fails the comparison.
					if k := regs[i.C]; k.O == storage.NumberObject && k.N >= 0 && k.N <= float64(o.Length()) && float64(uint32(k.N)) == k.N {
						ag.frames[len(ag.frames)-1].pc = pc
						if ag.active.truncateArray(o, uint32(k.N)) {
							continue
						}
					}
				}
			}
			receiver, name, rhs, site = v, fp.Keys[i.B], regs[i.C], c
			goto writeProperty
		case bytecode.OpGetLength:
			v := regs[i.B]
			if v.IsString() {
				if validateExec {
					ag.active.checkLengthHit(fp, pc-1, v, float64(v.StringLength()))
				}
				storage.SetNumber(&regs[i.A], float64(v.StringLength()))
				continue
			}
			receiver, name, site = v, ag.Key(storage.KeyLength), &fp.Caches[i.C]
			goto readProperty
		case bytecode.OpGetIndex:
			receiver, key = regs[i.B], regs[i.C]
			goto readProperty
		case bytecode.OpGetIndexInt:
			receiver = regs[i.B]
			goto readProperty
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
			// OpSetIndexInt and OpSetIndexIntStrict follow OpSetIndex and OpSetIndexStrict.
			v, k := regs[i.A], storage.Value{}
			if i.Op < bytecode.OpSetIndexInt {
				k = regs[i.B]
			} else {
				k = storage.Number(float64(i.B))
			}
			if o := v.O; o != nil && k.O == storage.NumberObject {
				if j := int(k.N); j == o.Raw().ElemCount() && o.Kind == storage.KindArray && uint32(j) == o.Length() && o.Flags() == 0 && float64(j) == k.N && o.Shape() == ag.active.realm.Shapes.Array && ag.active.ArrayProtoPlain() {
					// Appending to a plain array, as in the first switch.
					if validateExec {
						ag.active.checkAppend(o)
					}
					o.AppendElem(regs[i.C])
					o.Raw().SetLength(o.Length() + 1)
					continue
				}
			}
			receiver, key, rhs = v, k, regs[i.C]
			goto writeProperty
		case bytecode.OpSuperBase:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = ag.active.superBase(regs[i.B])
		case bytecode.OpGetSuper:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			ext := fp.Code[pc]
			pc++
			if ext.B == 0 {
				v, _ := ag.active.getSuper(ag.active.superBase(regs[ext.A]), regs[i.C], regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			v, key := ag.active.getSuper(regs[ext.A], regs[i.C], regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.C] = ag.active.KeyValue(key)
			regs[i.A] = v
		case bytecode.OpSetSuper, bytecode.OpSetSuperStrict:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			superBase := regs[fp.Code[pc].A]
			pc++
			ag.active.setSuper(superBase, regs[i.B], regs[i.C], regs[i.A], i.Op == bytecode.OpSetSuperStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDeleteProp, bytecode.OpDeletePropStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.deleteValue(regs[i.B], regs[i.C], i.Op == bytecode.OpDeletePropStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpIn:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				ag.active.ThrowTypeError("cannot use 'in' operator to search for a key in a non-object")
			}
			var r bool
			if k := ag.active.QueryKey(regs[i.C]); k.Missing() {
				r = ag.active.queryMissing(o.O, k)
			} else {
				r = ag.active.HasProperty(o.O, storage.PropKey(k))
			}
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(r)
		case bytecode.OpInstanceof:
			site := &fp.Caches[fp.Code[pc].A]
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.instanceOf(regs[i.B], regs[i.C], site)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(r)
		case bytecode.OpHasPrivate:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				ag.active.ThrowTypeError("cannot use 'in' operator to search for a private field in a non-object")
			}
			_, ok := ag.active.privateStorage(o.O).Shape().Lookup(regs[i.C].O)
			regs[i.A] = storage.Bool(ok)
		case bytecode.OpGetPrivate:
			v := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			val, getter := ag.active.getPrivate(v, regs[i.C].O)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetPrivate:
			v := regs[i.A]
			ag.frames[len(ag.frames)-1].pc = pc
			if setter := ag.active.setPrivate(v, regs[i.B].O, regs[i.C]); setter != nil {
				val := regs[i.C]
				argBase = ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(argBase + 1)
				ag.stack[argBase] = val
				regs = ag.reload(regs, ag.frameBase(), fp)
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.Nregs), false, nil
				goto call
			}
		case bytecode.OpToPropertyKey:
			v := regs[i.B]
			// The read and the write of a compound assignment convert an index alike.
			if v.O != nil && (v.O.Kind == storage.KindString || v.O.Kind == storage.KindSymbol) || v.O == storage.NumberObject && i.C >= 0 && float64(uint32(v.N)) == v.N {
				regs[i.A] = v
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			if i.C >= 0 {
				if b := regs[i.C]; b.IsNullish() {
					ag.active.ThrowTypeError("cannot read properties of " + ag.DisplayString(b) + " (reading '" + ag.DisplayString(v) + "')")
				}
			}
			k := ag.active.KeyValue(ag.active.ToPropertyKey(v))
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = k

		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			if x.O != nil && y.O != nil && x.O.Kind == storage.KindString && y.O.Kind == storage.KindString {
				regs[i.A] = ag.active.Concat(x, y)
				continue
			}
			v := ag.active.addValues(x, y)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpMod:
			if x, y := regs[i.B], regs[i.C]; x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Number(math.Mod(x.N, y.N))
				continue
			}
			goto arith
		case bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpBitOr, bytecode.OpBitAnd, bytecode.OpBitXor, bytecode.OpShl, bytecode.OpShr, bytecode.OpUShr:
			goto arith
		case bytecode.OpExp:
			x, y := regs[i.B], regs[i.C]
			if x.O == storage.NumberObject && y.O == storage.NumberObject {
				regs[i.A] = storage.Number(numconv.NumberPow(x.N, y.N))
				continue
			}
			goto arith
		case bytecode.OpAddK:
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.addValues(x, fp.Consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpModK:
			if x := regs[i.B]; x.O == storage.NumberObject {
				regs[i.A] = storage.Number(math.Mod(x.N, fp.Consts[i.C].N))
				continue
			}
			goto arithK
		case bytecode.OpSubK, bytecode.OpMulK, bytecode.OpDivK, bytecode.OpBitOrK, bytecode.OpBitAndK, bytecode.OpShlK, bytecode.OpShrK, bytecode.OpUShrK:
			goto arithK
		case bytecode.OpNeg:
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.negate(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPlus:
			x := regs[i.B]
			if x.O == storage.NumberObject {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			n := ag.active.ToNumber(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Number(n)
		case bytecode.OpNot:
			regs[i.A] = storage.Bool(!storage.ToBoolean(regs[i.B]))
		case bytecode.OpBitNot:
			x := regs[i.B]
			if x.O == storage.NumberObject && numconv.InInt64Range(x.N) {
				regs[i.A] = storage.Number(float64(^int32(int64(x.N))))
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.bitNot(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpToNumeric:
			x := regs[i.B]
			if x.O == storage.NumberObject {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.ToNumeric(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPostInc, bytecode.OpPostDec, bytecode.OpPostIncGetIndex:
			// After an OpPostIncGetIndex, the next instruction runs on its own.
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.ToNumeric(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			d := int64(1)
			if i.Op == bytecode.OpPostDec {
				d = -1
			}
			// Stepping a number or a BigInt runs no JavaScript.
			regs[i.B] = ag.active.step(v, d)
		case bytecode.OpToString:
			x := regs[i.B]
			if x.O != nil && x.O.Kind == storage.KindString {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.ToStringValue(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpConcat:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = ag.active.ConcatStrings(slices.Clip(regs)[i.B : i.B+i.C])
		case bytecode.OpTypeof:
			regs[i.A] = ag.TypeofValue(regs[i.B])
		case bytecode.OpInc, bytecode.OpDec, bytecode.OpIncGetIndex:
			// After an OpIncGetIndex, the next instruction runs on its own.
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			d := int64(1)
			if i.Op == bytecode.OpDec {
				d = -1
			}
			v := ag.active.step(x, d)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpLt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.compare(x, y, true) == storage.CmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(r)
		case bytecode.OpLe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := ag.active.compare(y, x, false)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(c == storage.CmpGreater || c == storage.CmpEqual)
		case bytecode.OpGt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.compare(y, x, false) == storage.CmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(r)
		case bytecode.OpGe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := ag.active.compare(x, y, true)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(c == storage.CmpGreater || c == storage.CmpEqual)
		case bytecode.OpEq, bytecode.OpNe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.looseEquals(regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(r == (i.Op == bytecode.OpEq))
		case bytecode.OpStrictEq:
			regs[i.A] = storage.Bool(storage.IsStrictlyEqual(regs[i.B], regs[i.C]))
		case bytecode.OpStrictNe:
			regs[i.A] = storage.Bool(!storage.IsStrictlyEqual(regs[i.B], regs[i.C]))

		case bytecode.OpJump:
			if int(i.A) < pc && ag.InterruptFlag.Load() {
				// A loop whose condition is not tested at its end jumps backwards with this
				// instruction.
				ag.frames[len(ag.frames)-1].pc = pc
				ag.Poll()
			}
			pc = int(i.A)
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			if storage.ToBoolean(regs[i.B]) == (i.Op == bytecode.OpJumpIf) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.compare(x, y, true) == storage.CmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := ag.active.compare(y, x, false)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !(c == storage.CmpGreater || c == storage.CmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.compare(y, x, false) == storage.CmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := ag.active.compare(x, y, true)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !(c == storage.CmpGreater || c == storage.CmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.looseEquals(regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			if r != (i.Op == bytecode.OpJumpNotEq) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSEq:
			if !storage.IsStrictlyEqual(regs[i.B], regs[i.C]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNe:
			if storage.IsStrictlyEqual(regs[i.B], regs[i.C]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLtK:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				ag.frames[len(ag.frames)-1].pc = pc
				x = ag.active.ToNumeric(ag.active.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.O != storage.NumberObject {
					if ag.active.compareBigIntMixed(x, fp.Consts[i.C]) != storage.CmpLess {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N < fp.Consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLeK:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				ag.frames[len(ag.frames)-1].pc = pc
				x = ag.active.ToNumeric(ag.active.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.O != storage.NumberObject {
					if c := ag.active.compareBigIntMixed(x, fp.Consts[i.C]); c != storage.CmpLess && c != storage.CmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N <= fp.Consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGtK:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				ag.frames[len(ag.frames)-1].pc = pc
				x = ag.active.ToNumeric(ag.active.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.O != storage.NumberObject {
					if ag.active.compareBigIntMixed(x, fp.Consts[i.C]) != storage.CmpGreater {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N > fp.Consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGeK:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				ag.frames[len(ag.frames)-1].pc = pc
				x = ag.active.ToNumeric(ag.active.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.O != storage.NumberObject {
					if c := ag.active.compareBigIntMixed(x, fp.Consts[i.C]); c != storage.CmpGreater && c != storage.CmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N >= fp.Consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpLoopIf:
			if storage.ToBoolean(regs[i.B]) {
				if ag.InterruptFlag.Load() {
					ag.frames[len(ag.frames)-1].pc = pc
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopLt, bytecode.OpLoopLe, bytecode.OpLoopGt, bytecode.OpLoopGe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.loopHolds(i.Op, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			if r {
				if ag.InterruptFlag.Load() {
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopLtK, bytecode.OpLoopLeK, bytecode.OpLoopGtK, bytecode.OpLoopGeK:
			ag.frames[len(ag.frames)-1].pc = pc
			r := ag.active.loopHoldsK(i.Op, regs[i.B], fp.Consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			if r {
				if ag.InterruptFlag.Load() {
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopSEqP, bytecode.OpLoopSNeP, bytecode.OpLoopNullish, bytecode.OpLoopNotNullish:
			if ag.loopHoldsP(i.Op, regs[i.B], i.C) {
				if ag.InterruptFlag.Load() {
					ag.frames[len(ag.frames)-1].pc = pc
					ag.Poll()
				}
				pc = int(i.A)
			}

		case bytecode.OpCallUndef:
			regs[i.B+1] = storage.Undefined
			fallthrough
		case bytecode.OpCall:
			// Calls of ordinary functions that fit take the inline frame of the fast path above.
			b := int(i.B)
			f, callThis, firstArgReg, argc, ret, isNew, newTarget = regs[b].O, regs[b+1], b+2, int(i.C), i.A, false, nil
			goto callRegs
		case bytecode.OpCallCall:
			b := int(i.B)
			if regs[b].O == ag.active.realm.Intrinsics[storage.IntrinsicFunctionPrototypeCall] {
				f, callThis, firstArgReg, argc, ret, isNew, newTarget = regs[b+1].O, regs[b+2], b+3, int(i.C)-1, i.A, false, nil
				if argc < 0 {
					callThis, argc = storage.Undefined, 0
				}
			} else {
				f, callThis, firstArgReg, argc, ret, isNew, newTarget = regs[b].O, regs[b+1], b+2, int(i.C), i.A, false, nil
			}
			goto callRegs
		case bytecode.OpApplyFwd, bytecode.OpTailApplyFwd:
			argsReg := fp.Code[pc].A
			pc++
			b := int(i.B)
			if !regs[argsReg].IsUndefined() || regs[b].O != ag.active.realm.Intrinsics[storage.IntrinsicFunctionPrototypeApply] {
				// Once exposed, the arguments object's length and indexed properties can change.
				regs[b+2] = regs[i.C]
				regs[b+3] = ag.active.forwardedArguments(argsReg)
				f, callThis, firstArgReg, argc, ret, isNew, newTarget = regs[b].O, regs[b+1], b+2, 2, i.A, false, nil
				if i.Op == bytecode.OpTailApplyFwd {
					argBase = ag.frameBase() + firstArgReg
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
				if f := regs[b+1].O; !fp.HeapRegs && f != nil && f.Fn != nil && !ag.InterruptFlag.Load() {
					if c := f.Fn.Code; c != nil && c.InlineFrames && c.Realm == fp.Realm {
						n := len(ag.frames)
						cur := &ag.frames[n-1]
						argc := cur.argc
						argBase := cur.base + fp.Nregs + 1
						end := argBase + max(argc, c.Nregs+1)
						// The frames have room for at most maxFrames.
						if (argc <= fp.Nparams || cur.extraBase >= 0) && (argc <= c.Nparams || !c.NeedsExtra) && end+stackHeadroom <= len(ag.stack) && n < cap(ag.frames) {
							k := min(argc, fp.Nparams)
							for j := range k {
								ag.stack[argBase+j] = regs[j]
							}
							for j := k; j < argc; j++ {
								ag.stack[argBase+j] = ag.stack[cur.extraBase+j-k]
							}
							// The frame is pushed as in OpCall's case in the fast switch.
							this := regs[i.C]
							cur.pc = pc
							ag.stackHigh = max(ag.stackHigh, end)
							if from, to := c.FirstCleared(argc), c.Nclear; from < to {
								clearFrameRegs(ag.stack[argBase:], from, to)
							}
							ag.frames = ag.frames[:n+1]
							ag.framesHigh = max(ag.framesHigh, n+1)
							fr := &ag.frames[n]
							fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
							// The callee belongs to the current realm, which is that of the running frame.
							fr.pc, fr.base, fr.ret, fr.isNew, fr.plainReturn, fr.argc, fr.extraBase = 0, argBase, i.A, false, true, argc, -1
							ag.sp = argBase + c.Nregs + 1
							fp, pc = c, 0
							regs = ag.stack[argBase:ag.sp]
							if r := c.ThisReg; r >= 0 && (!c.ThisSloppy || this.O != nil && this.O.Kind >= storage.KindFirstObject) {
								regs[r] = this
								pc = 1
							}
							continue
						}
					}
				}
			}
			f, callThis = regs[b+1].O, regs[i.C]
			goto forward
		case bytecode.OpCallFwd, bytecode.OpTailCallFwd:
			b := int(i.B)
			f, callThis = regs[b].O, regs[b+1]
			goto forward
		case bytecode.OpCallEvalSpread:
			// A direct eval, or the spread call of the case below for any other function.
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := ag.active.callEval(slices.Clip(regs), i, int(fp.Code[pc-1].A), -1); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpCallSpread, bytecode.OpNewSpread, bytecode.OpSuperSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].O, regs[b+1], i.A, false, nil
			args := regs[b+2].O.Raw().Elems()
			argc = len(args)
			argBase = ag.frames[len(ag.frames)-1].top()
			ag.ensureStack(argBase + argc)
			copy(ag.stack[argBase:], args)
			regs = ag.reload(regs, ag.frameBase(), fp)
			switch i.Op {
			case bytecode.OpNewSpread:
				goto construct
			case bytecode.OpSuperSpread:
				newTarget = regs[b+1].O
				goto superCall
			}
			goto call
		case bytecode.OpCallEval, bytecode.OpTailCallEval:
			b := int(i.B)
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := ag.active.callEval(slices.Clip(regs), i, int(fp.Code[pc-1].A), int(i.C)); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			f, callThis, firstArgReg, argc, ret, isNew, newTarget = regs[b].O, regs[b+1], b+2, int(i.C), i.A, false, nil
			if i.Op == bytecode.OpTailCallEval {
				argBase = ag.frameBase() + firstArgReg
				goto tailCall
			}
			goto callRegs
		case bytecode.OpTailCallUndef:
			regs[i.B+1] = storage.Undefined
			fallthrough
		case bytecode.OpTailCall:
			b := int(i.B)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].O, regs[b+1], ag.frameBase()+b+2, int(i.C), i.A, false, nil
			goto tailCall
		case bytecode.OpTailCallEvalSpread:
			// A direct eval, or the spread call of the case below for any other function.
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := ag.active.callEval(slices.Clip(regs), i, int(fp.Code[pc-1].A), -1); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpTailCallSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].O, regs[b+1], i.A, false, nil
			args := regs[b+2].O.Raw().Elems()
			argc = len(args)
			argBase = ag.sp
			ag.ensureStack(argBase + argc)
			copy(ag.stack[argBase:], args)
			goto tailCall
		case bytecode.OpNew:
			b := int(i.B)
			site := fp.Code[pc].A
			pc++
			f, argc, ret = regs[b].O, int(i.C), i.A
			argBase = ag.placeCallArgs(b+2, argc)
			if f != nil && f.Fn != nil && f.Fn.Code != nil && f.Fn.Constructor && !f.Fn.Derived {
				// The common case: a plain constructor function, whose prototype property is cached.
				c := &fp.Caches[site]
				var proto storage.Value
				if f.Shape() == c.Shape && c.Kind == storage.CacheOwn {
					if validateExec {
						ag.active.checkEntryRead(fp, pc-2, storage.Value{O: f}, f, ag.Key(storage.KeyPrototype), &c.CacheEntry)
					}
					proto = f.Raw().Slot(int(c.Slot))
				} else {
					ag.frames[len(ag.frames)-1].pc = pc
					proto, _ = ag.active.getNamed(storage.Value{O: f}, ag.Key(storage.KeyPrototype), c)
					regs = ag.reload(regs, ag.frameBase(), fp)
				}
				if proto.O == nil || proto.O.Kind < storage.KindFirstObject {
					proto = storage.Value{O: f.Fn.Code.Realm.Intrinsics[storage.IntrinsicObjectPrototype]}
				}
				var shape *storage.Shape
				if proto.O == c.Holder && c.Next != nil && c.Next.Realm == f.Fn.Code.Realm {
					if validateExec {
						ag.active.checkInstanceShape(fp, pc-2, proto.O, f.Fn.Code.Realm, c.Next)
					}
					shape = c.Next
				} else {
					shape = ag.active.ShapeForRealm(proto.O, f.Fn.Code.Realm)
					if c.Kind == storage.CacheOwn {
						c.Holder, c.Next = proto.O, shape
					}
				}
				callThis = storage.Value{O: storage.NewInstance(shape)}
				isNew, newTarget = true, f
				goto call
			}
			goto construct
		case bytecode.OpSuperCall:
			b := int(i.B)
			f, newTarget, argc, ret = regs[b].O, regs[b+1].O, int(i.C), i.A
			argBase = ag.placeCallArgs(b+2, argc)
			goto superCall

		case bytecode.OpReturn, bytecode.OpReturnUndef, bytecode.OpReturnDerived:
			// The fast switch above pops the frame the same way for the returns that need none
			// of the calls here.
			r := storage.Undefined
			if i.Op != bytecode.OpReturnUndef {
				r = regs[i.A]
			}
			n := len(ag.frames) - 1
			done := &ag.frames[n]
			if i.Op == bytecode.OpReturnDerived {
				if !r.IsObject() {
					if !r.IsUndefined() {
						ag.active.throwReturnError(stop, n, pc, ErrorKindType, "derived constructors may only return object or undefined")
					}
					r = regs[i.B]
					if r.O == tdzObject {
						ag.active.throwReturnError(stop, n, pc, ErrorKindReference, "must call super constructor in derived class before accessing 'this' or returning from derived constructor")
					}
				}
			} else if done.isNew && (r.O == nil || r.O.Kind < storage.KindFirstObject) {
				r = done.this
			}
			if g := done.gen; g != nil {
				g.completeAt(pc)
			} else if len(ag.openUpvals) > 0 {
				ag.closeUpvals(done.base)
			}
			ret = done.ret
			ag.frames = ag.frames[:n]
			if n == stop {
				return r, execReturn
			}
			fr := &ag.frames[n-1]
			fp = fr.fp
			pc = fr.pc
			regs = fr.registers(ag.stack)
			ag.sp = fr.top()
			regs[ret] = r
			if fp.Realm != ag.active.realmPart {
				goto enter
			}
		case bytecode.OpThrow:
			if ag.captureThrowSites {
				ag.thrownAt = throwSite{
					code: fp.Func,
					pc:   pc - 1,
				}
			}
			ag.thrown = regs[i.A]
			goto throw
		case bytecode.OpRethrow:
			v := regs[i.A]
			if i.B >= 0 && v.O == closeSignal {
				v = regs[i.B]
			}
			ag.restoreException(v)
			goto throw

		case bytecode.OpInitialYield:
			n := len(ag.frames) - 1
			cur := &ag.frames[n]
			g := cur.gen
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedStart
			o := ag.active.newGeneratorObject(g)
			ret = cur.ret
			*cur = frame{}
			ag.frames = ag.frames[:n]
			if n == stop {
				return storage.Value{O: o}, execReturn
			}
			fr := &ag.frames[n-1]
			fp = fr.fp
			pc = fr.pc
			regs = fr.registers(ag.stack)
			ag.sp = fr.top()
			regs[ret] = storage.Value{O: o}
			if fp.Realm != ag.active.realmPart {
				goto enter
			}
		case bytecode.OpYield, bytecode.OpAwait:
			n := len(ag.frames) - 1
			cur := &ag.frames[n]
			g := cur.gen
			v := regs[i.B]
			g.resumeReg = i.A
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedYield
			g.awaiting = i.Op == bytecode.OpAwait
			g.delegating = i.C != 0
			*cur = frame{}
			ag.frames = ag.frames[:n]
			// A generator only ever runs as the frame its loop was started for.
			return v, execYield
		case bytecode.OpGenFinally:
			if regs[i.A+1].O == closeSignal {
				g := ag.frames[len(ag.frames)-1].gen
				regs[i.A+1] = g.closeValue
				g.closeValue = storage.Undefined
				regs[i.A] = storage.Number(float64(i.B))
			} else {
				regs[i.A] = storage.Number(1)
			}
		case bytecode.OpGenResumeMode:
			g := ag.frames[len(ag.frames)-1].gen
			regs[i.A] = storage.Number(float64(g.mode))
			g.mode = ResumeNext
		case bytecode.OpYieldDelegateResult:
			ag.frames[len(ag.frames)-1].pc = pc
			r := regs[i.B]
			if !r.IsObject() {
				ag.active.ThrowTypeError("iterator result " + ag.DisplayString(r) + " is not an object")
			}
			done := storage.ToBoolean(ag.active.GetV(r, ag.Key(storage.KeyDone)))
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !done {
				pc = int(i.C)
				continue
			}
			v := ag.active.GetV(r, ag.Key(storage.KeyValue))
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpNewObject:
			o, slots := storage.NewObjectWithStorage(int(i.C))
			o.Kind = storage.KindObject
			o.Raw().SetShape(ag.active.realm.Shapes.Object)
			o.InitSlots(slots[:0])
			regs[i.A] = storage.Value{O: o}
		case bytecode.OpNewArray:
			o, elems := storage.NewTailObjectWithStorage(int(i.C))
			o.Kind = storage.KindArray
			o.Class = storage.ClassArray
			o.Raw().SetShape(ag.active.realm.Shapes.Array)
			o.InitElems(elems[:0])
			regs[i.A] = storage.Value{O: o}
		case bytecode.OpNewArrayK:
			regs[i.A] = storage.Value{O: ag.active.NewArrayOfConstants(fp.Consts[i.B : i.B+i.C])}
		case bytecode.OpArrayPush:
			o := regs[i.A].O
			o.PushElement(regs[i.B])
		case bytecode.OpArrayHole:
			o := regs[i.A].O
			o.PushElement(storage.Hole)
		case bytecode.OpArraySpread:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.A].O
			ag.active.appendIterable(o, regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpSpreadArgs:
			ag.frames[len(ag.frames)-1].pc = pc
			o := ag.active.spreadArgs(regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Value{O: o}
		case bytecode.OpRegExp:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = storage.Value{O: ag.active.newRegExpLiteral(&fp.RegExps[i.B])}
		case bytecode.OpRegExpArg:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.lendRegExpLiteral(slices.Clip(regs), i.A, i.C, &fp.RegExps[i.B])
		case bytecode.OpRegExpMethod:
			ag.frames[len(ag.frames)-1].pc = pc
			m, this := ag.active.regExpLiteralMethod(&fp.RegExps[i.B], fp.Keys[i.C], &fp.Caches[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A], regs[i.A+1] = m, storage.Value{O: this}
		case bytecode.OpTemplate:
			regs[i.A] = storage.Value{O: ag.active.templateObject(&fp.Templates[i.B])}
		case bytecode.OpDefineField:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.defineField(regs[i.A].O, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDefineFieldK:
			o := regs[i.A].O
			c := &fp.Caches[i.B]
			if o.Shape() == c.Shape && c.Kind == storage.CacheAdd {
				if validateExec {
					ag.active.checkDefineHit(fp, pc-1, o, fp.Keys[i.B], c.Next)
				}
				o.Raw().SetShape(c.Next)
				o.AppendSlot(regs[i.C])
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.defineFieldK(o, fp.Keys[i.B], regs[i.C], c)
		case bytecode.OpDefineMethod:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.defineMethod(regs[i.A].O, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDefineGetter, bytecode.OpDefineSetter:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			enumerable := fp.Code[pc].A != 0
			pc++
			ag.active.defineAccessor(regs[i.A].O, regs[i.B], regs[i.C].O, i.Op == bytecode.OpDefineSetter, enumerable)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpSetFuncName:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.setFuncName(regs[i.A], regs[i.B], int(i.C))
		case bytecode.OpSetHome:
			regs[i.A].O.OwnFunction().Home = regs[i.B].O
		case bytecode.OpCopyProps:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.copyDataProperties(regs[i.A].O, regs[i.B], nil)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpCopyPropsExcept:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			n := int(fp.Code[pc].A)
			pc++
			o := ag.active.NewOrdinaryObject()
			excluded := append([]storage.Value(nil), slices.Clip(regs)[i.C:int(i.C)+n]...)
			ag.active.copyDataProperties(o, regs[i.B], excluded)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Value{O: o}
		case bytecode.OpSetProto:
			if p := regs[i.B]; p.IsObject() {
				ag.active.SetProto(regs[i.A].O, p.O)
			} else if p.IsNull() {
				ag.active.SetProto(regs[i.A].O, nil)
			}
		case bytecode.OpClass:
			ag.frames[len(ag.frames)-1].pc = pc
			parent := storage.Hole
			if i.C >= 0 {
				parent = regs[i.C]
			}
			proto := ag.active.setupClass(regs[i.B].O, parent)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Value{O: proto}
		case bytecode.OpPrivateName:
			name := storage.NewPrivateName(fp.Keys[i.B].Text())
			if i.C != 0 {
				name.FreezeLength()
			}
			regs[i.A] = storage.Value{O: name}
		case bytecode.OpInitFields:
			if regs[i.A].O.Fn.Fields != nil {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.initFields(regs[i.A].O, regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
			}
		case bytecode.OpAddField:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			kind := int(fp.Code[pc].A)
			pc++
			ag.active.addClassElement(regs[i.A].O, regs[i.B], regs[i.C], kind)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpCheckThisUninit:
			if regs[i.A].O != tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.ThrowReferenceError("super constructor may only be called once")
			}
		case bytecode.OpRequireCoercible:
			ag.frames[len(ag.frames)-1].pc = pc
			if regs[i.A].IsUndefined() {
				ag.active.ThrowTypeError("cannot destructure 'undefined' as it is undefined")
			}
			ag.active.ThrowTypeError("cannot destructure 'null' as it is null")

		case bytecode.OpForIn:
			ag.frames[len(ag.frames)-1].pc = pc
			it := ag.active.newForIn(regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = it
		case bytecode.OpForInNext:
			ag.frames[len(ag.frames)-1].pc = pc
			k, ok := ag.active.forInNext(regs[i.B].O)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !ok {
				pc = int(i.C)
				continue
			}
			regs[i.A] = k
		case bytecode.OpGetIterator:
			v := regs[i.B]
			if i.C == bytecode.IterSync && v.O != nil && v.O.Kind == storage.KindArray && ag.active.ArrayIterationPlain(v.O) {
				// A numeric iterator slot identifies an indexed array with the original next method.
				regs[i.A] = storage.Number(0)
				regs[i.A+1] = v
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			it, next := ag.active.getIterator(v, i.C == bytecode.IterAsync)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = it
			regs[i.A+1] = next
		case bytecode.OpIterNext:
			if regs[i.B].O == nil {
				// The iterator finished earlier.
				regs[i.A] = storage.Undefined
				pc = int(i.C)
				continue
			}
			if idx := regs[i.B]; idx.O == storage.NumberObject {
				o := regs[i.B+1].O
				j := uint32(idx.N)
				if j >= o.Length() {
					// Mark the iterator as finished, so that closing it does nothing.
					regs[i.B] = storage.Undefined
					regs[i.B+1] = storage.Undefined
					regs[i.A] = storage.Undefined
					pc = int(i.C)
					continue
				}
				regs[i.B].N = idx.N + 1
				if uint(j) < uint(o.Raw().ElemCount()) && o.Raw().Elem(int(j)).O != storage.HoleObject {
					regs[i.A] = o.Raw().Elem(int(j))
					continue
				}
				ag.frames[len(ag.frames)-1].pc = pc
				// A getter that throws finishes the iterator.
				regs[i.B] = storage.Undefined
				regs[i.B+1] = storage.Undefined
				v := ag.active.plainArrayIteratorIndex(o, j)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.B] = storage.Number(idx.N + 1)
				regs[i.B+1] = storage.Value{O: o}
				regs[i.A] = v
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			{
				// An iterator whose step throws counts as finished, so that it is not closed.
				it := regs[i.B]
				regs[i.B] = storage.Undefined
				v, st := ag.active.iterNext(it, regs[i.B+1])
				regs = ag.reload(regs, ag.frameBase(), fp)
				switch st {
				case iterValue:
					regs[i.B] = it
					regs[i.A] = v
				case iterDone:
					regs[i.A] = storage.Undefined
					pc = int(i.C)
				default:
					goto throw
				}
			}
		case bytecode.OpIterNextPair:
			// The elements of the next value go to their registers without the value when nothing
			// can tell. When the step has run and something can tell, the pair becomes the value of
			// the following OpIterNext, which is skipped. Otherwise the instructions after this one
			// step the iterator, which is already cleared when the step found it finished.
			b := fp.Code[pc].A
			pc++
			it, next := regs[i.B], regs[i.B+1]
			if it.O == nil {
				continue
			}
			if it.O == storage.NumberObject {
				// A plain array iterated by index, whose element is itself a plain array of at
				// least two elements.
				o, j := next.O, uint32(it.N)
				if j >= o.Length() || uint(j) >= uint(o.Raw().ElemCount()) {
					continue
				}
				e := o.Raw().Elem(int(j)).O
				if e == nil || e.Shape() != ag.active.realm.Shapes.Array || e.Length() < 2 || e.Raw().ElemCount() < 2 || e.Raw().Elem(0).IsHole() || e.Raw().Elem(1).IsHole() || !ag.PairDestructuringPlain(ag.active.realmPart) {
					continue
				}
				regs[i.B].N = it.N + 1
				regs[i.A] = e.Raw().Elem(0)
				regs[b] = e.Raw().Elem(1)
				pc = int(i.C)
				continue
			}
			if next.O == nil || next.O.Fn == nil || ag.InterruptFlag.Load() {
				continue
			}
			var x, y storage.Value
			var st quiet.PairStep
			pairRealm := ag.active.realmPart
			if step := iteratorPairStepOf(next.O.Fn, ag.active.realmPart); step != nil {
				x, y, st = step(storage.IteratorState(it))
				if st == quiet.PairYielded {
					pairRealm = it.O.Shape().Realm
				}
			} else if next.O == ag.active.realm.Intrinsics[storage.IntrinsicArrayIteratorPrototypeNext] {
				// Reading an array entry can run JavaScript. A throwing step finishes the iterator.
				ag.frames[len(ag.frames)-1].pc = pc
				regs[i.B] = storage.Undefined
				x, y, st = ag.active.arrayIteratorPairStep(it, next)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.B] = it
			} else {
				continue
			}
			switch st {
			case quiet.PairYielded:
				if !ag.PairDestructuringPlain(pairRealm) {
					// The outer iterator has stepped. Supply its pair to the ordinary binding.
					regs[fp.Code[pc].A] = storage.Value{O: pairRealm.NewArrayPair(x, y)}
					pc++
					continue
				}
				regs[i.A] = x
				regs[b] = y
				pc = int(i.C)
			case quiet.PairDone:
				regs[i.B] = storage.Undefined
			}
			// A finished iterator is left for the step after this instruction.
		case bytecode.OpIterClose:
			it := regs[i.A]
			if it.IsUndefined() {
				continue
			}
			regs[i.A] = storage.Undefined
			ag.frames[len(ag.frames)-1].pc = pc
			pending := i.B >= 0 && regs[i.B].O != closeSignal
			if it.O == storage.NumberObject {
				o := regs[i.A+1].O
				regs[i.A+1] = storage.Undefined
				ag.active.closePlainArrayIterator(o, uint32(it.N), pending)
			} else {
				ag.active.iteratorClose(it, pending)
			}
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpIterCall:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				it := regs[i.B]
				m := ag.active.GetMethod(it, fp.Keys[i.C])
				regs = ag.reload(regs, ag.frameBase(), fp)
				if m == nil {
					regs[i.A] = storage.Undefined
					regs[i.A+1] = storage.False
					continue
				}
				r := ag.active.Call(m, it, regs[i.A])
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = storage.True
			}
		case bytecode.OpIterReturn:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				it := regs[i.B]
				regs[i.B] = storage.Undefined
				r, ok := ag.active.iterReturn(it)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = storage.Bool(ok)
			}
		case bytecode.OpIterCheckResult:
			if !regs[i.A].IsObject() {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.throwNotIterResult(regs[i.A])
			}
		case bytecode.OpIterResultValue:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				v, done := ag.active.iterResultValue(regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
				if done {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}

		case bytecode.OpUsing:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				dc := ag.active.addDisposableOp(regs[i.B], regs[i.A], DisposeHint(i.C))
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = dc
			}
		case bytecode.OpDisposeBegin:
			beginDisposeOp(regs[i.A], regs[i.B], regs[i.C])
		case bytecode.OpDisposeStep:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				v, await := ag.active.disposeStepOp(regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
				if !await {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}
		case bytecode.OpDisposeError:
			ag.active.disposeError(regs[i.A].O.Ext.(*DisposeCapability), regs[i.B])
		case bytecode.OpDisposeEnd:
			if v, thrown := endDisposeOp(regs[i.A]); thrown {
				regs[i.A] = storage.Undefined
				ag.restoreException(v)
				goto throw
			}
			regs[i.A] = storage.Undefined

		case bytecode.OpWith:
			ag.frames[len(ag.frames)-1].pc = pc
			o := ag.active.ToObject(regs[i.A])
			cur := &ag.frames[len(ag.frames)-1]
			cur.scopes = append(cur.scopes, o)
		case bytecode.OpPopScope:
			cur := &ag.frames[len(ag.frames)-1]
			cur.leaveScopes(len(cur.scopes) - 1)
		case bytecode.OpResolveName:
			ag.frames[len(ag.frames)-1].pc = pc
			o := ag.active.findScope(ag.frames[len(ag.frames)-1].cl, fp.Keys[i.B], int(i.C))
			regs = ag.reload(regs, ag.frameBase(), fp)
			if o != nil {
				regs[i.A] = storage.Value{O: o}
			} else {
				regs[i.A] = storage.Undefined
			}
		case bytecode.OpGetScoped, bytecode.OpGetScopedStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.getScoped(regs[i.B].O, fp.Keys[i.C], i.Op == bytecode.OpGetScopedStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpSetScoped, bytecode.OpSetScopedStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.setScoped(regs[i.B].O, fp.Keys[i.C], regs[i.A], i.Op == bytecode.OpSetScopedStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDeleteScoped:
			ag.frames[len(ag.frames)-1].pc = pc
			ok := ag.active.DeleteProp(regs[i.B].O, storage.PropKey{Key: fp.Keys[i.C]})
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(ok)
		case bytecode.OpDeleteName:
			ag.frames[len(ag.frames)-1].pc = pc
			ok := ag.active.deleteName(fp.Keys[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = storage.Bool(ok)
		case bytecode.OpCaptureScopes:
			regs[i.A].O.OwnFunction().Scopes = ag.openScopes(ag.frames[len(ag.frames)-1].cl)
		case bytecode.OpEvalScope:
			ag.active.pushEvalScope()
		case bytecode.OpScopeThis:
			regs[i.A] = scopeThis(regs[i.B])
		case bytecode.OpDeclareEvalVar:
			ag.frames[len(ag.frames)-1].pc = pc
			ag.active.declareEvalVar(ag.frames[len(ag.frames)-1].cl, fp.Keys[i.A], int(i.B), slices.Clip(regs), int(i.C))
		case bytecode.OpImport, bytecode.OpImportMeta:
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.importOp(slices.Clip(regs), i)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpAutoAccessor:
			ag.active.autoAccessorOp(slices.Clip(regs), i)
		case bytecode.OpDecorateBegin, bytecode.OpDecorateAdd, bytecode.OpDecorateClass, bytecode.OpDecorateElement, bytecode.OpDecorateApply, bytecode.OpDecorateFinish:
			if i.Op == bytecode.OpDecorateElement {
				// Past the OpExtra, which holds the Elem kind.
				pc++
			}
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := ag.active.decorateOp(slices.Clip(regs), i, &fp.Code[pc-1]); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
			} else {
				regs = ag.reload(regs, ag.frameBase(), fp)
			}

		default:
			panic("engine: unknown opcode")
		}
		continue

	arith:
		{
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.arith(i.Op, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	arithK:
		{
			ag.frames[len(ag.frames)-1].pc = pc
			v := ag.active.arith(i.Op.RegOperand(), regs[i.B], fp.Consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	readProperty:
		{
			fr := &ag.frames[len(ag.frames)-1]
			base, dst := fr.base, i.A
			fr.pc = pc
			var val storage.Value
			var getter *storage.Object
			switch i.Op {
			case bytecode.OpGetProp, bytecode.OpGetPropChain, bytecode.OpGetMethod, bytecode.OpGetLength, bytecode.OpGetArgsLength:
				val, getter = ag.active.getNamedAtSite(receiver, name, site)
			case bytecode.OpGetIndex, bytecode.OpGetArgsIndex:
				val, getter = ag.active.getValue(receiver, key)
			case bytecode.OpGetIndexInt, bytecode.OpGetArgsIndexInt:
				val, getter = ag.active.getIndex(receiver, uint32(i.C))
			case bytecode.OpCopyPropToGlobal:
				// The following global store owns the cache and fills it only for writable bindings.
				val = ag.active.resolveGlobal(fp.Keys[fp.Code[i.C-1].B], nil)
			case bytecode.OpResolveGlobal:
				val = ag.active.resolveGlobal(fp.Keys[i.B], &fp.Globals[i.C])
			case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
				orUndef := i.Op == bytecode.OpGetGlobalOrUndef || i.Op == bytecode.OpGetGlobalOrUndefStrict
				strict := i.Op == bytecode.OpGetGlobalStrict || i.Op == bytecode.OpGetGlobalOrUndefStrict
				val = ag.active.getGlobal(fp.Keys[i.B], &fp.Globals[i.C], orUndef, strict)
			default:
				panic("engine: unexpected property read opcode: " + i.Op.String())
			}
			regs = ag.reload(regs, base, fp)
			if getter != nil {
				f, callThis, argc, ret = getter, receiver, 0, dst
				goto callAbove
			}
			regs[dst] = val
			continue
		}

	writeProperty:
		{
			fr := &ag.frames[len(ag.frames)-1]
			base := fr.base
			switch i.Op {
			case bytecode.OpSetProp, bytecode.OpSetPropStrict:
				if setter == nil {
					fr.pc = pc
					var ok bool
					setter, ok = ag.active.setNamed(receiver, name, rhs, site)
					if !ok && i.Op == bytecode.OpSetPropStrict {
						ag.active.throwReadOnly(receiver, storage.PropKey{Key: name})
					}
				}
			case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
				fr.pc = pc
				ag.active.setGlobal(fp.Keys[i.B], rhs, &fp.Globals[i.C], i.Op == bytecode.OpSetGlobalStrict)
			case bytecode.OpPutGlobal, bytecode.OpPutGlobalStrict:
				fr.pc = pc
				ag.active.putGlobal(fp.Keys[i.B], rhs, &fp.Globals[i.C], i.Op == bytecode.OpPutGlobalStrict)
			case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
				fr.pc = pc
				setter = ag.active.setValue(receiver, key, rhs, i.Op == bytecode.OpSetIndexStrict || i.Op == bytecode.OpSetIndexIntStrict)
			default:
				panic("engine: unexpected property write opcode: " + i.Op.String())
			}
			if setter != nil {
				argBase = ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(argBase + 1)
				ag.stack[argBase] = rhs
			}
			// Both the lookup and making room for a setter argument can move the stack.
			regs = ag.reload(regs, base, fp)
			if setter != nil {
				f, callThis, argc, ret, isNew, newTarget = setter, receiver, 1, int32(fp.Nregs), false, nil
				goto call
			}
			continue
		}

	callAbove:
		// A call without arguments, such as a getter.
		argBase = ag.frames[len(ag.frames)-1].top()
		isNew, newTarget = false, nil
		goto call

	forward:
		// A call with the arguments of the running function.
		if (i.Op == bytecode.OpCallFwd || i.Op == bytecode.OpTailCallFwd) && (!regs[i.C].IsUndefined() || !ag.ArrayIterationUnmodified(ag.active.realmPart)) {
			ag.frames[len(ag.frames)-1].pc = pc
			args := ag.active.spreadArgs(ag.active.forwardedArguments(i.C)).Raw().Elems()
			argc, ret, isNew, newTarget = len(args), i.A, false, nil
			argBase = ag.frames[len(ag.frames)-1].top()
			ag.ensureStack(argBase + argc)
			copy(ag.stack[argBase:], args)
			regs = ag.reload(regs, ag.frameBase(), fp)
		} else {
			cur := &ag.frames[len(ag.frames)-1]
			argc, ret, isNew, newTarget = cur.argc, i.A, false, nil
			argBase = cur.top()
			ag.ensureStack(argBase + argc)
			regs = ag.reload(regs, cur.base, fp)
			n := min(argc, fp.Nparams)
			copy(ag.stack[argBase:], slices.Clip(regs)[:n])
			copy(ag.stack[argBase+n:], ag.extraArgs(cur))
		}
		if i.Op == bytecode.OpTailApplyFwd || i.Op == bytecode.OpTailCallFwd {
			goto tailCall
		}
		goto call

	callRegs:
		argBase = ag.placeCallArgs(firstArgReg, argc)

	call:
		// regs is current and the arguments are on the stack from argBase on.
		{
			if ag.InterruptFlag.Load() {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.Poll()
				regs = ag.reload(regs, ag.frameBase(), fp)
			}
			if f == nil || f.Fn == nil {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.active.throwNotCallable(fp, pc, slices.Clip(regs))
			}
			fn := f.Fn
			if c := fn.Code; c != nil && !c.IsGenerator && !c.IsAsync && (!fn.ClassConstructor || isNew || newTarget != nil) {
				if len(ag.frames) >= maxFrames {
					ag.frames[len(ag.frames)-1].pc = pc
					ag.active.ThrowRangeError("maximum call stack size exceeded")
				}
				if site := c.ReturnedProperty; site != nil && !isNew && newTarget == nil && c.Realm == fp.Realm && callThis.IsObject() {
					if e := site.Entry(callThis.O.Shape()); e != nil && e.Kind == storage.CacheOwn {
						if validateExec {
							ag.active.checkReturnedProperty(c, callThis, e)
						}
						regs[ret] = callThis.O.Raw().Slot(int(e.Slot))
						continue
					}
				}
				ag.frames[len(ag.frames)-1].pc = pc
				base := ag.pushFrame(f, callThis, argBase, argc, ret, isNew, newTarget)
				fp = c
				regs = ag.stack[base : base+c.Nregs+1]
				pc = 0
				if r := c.ThisReg; r >= 0 {
					// Do what the first instruction does, and start after it.
					if t := ag.frames[len(ag.frames)-1].this; !c.ThisSloppy || t.O != nil && t.O.Kind >= storage.KindFirstObject {
						regs[r] = t
						pc = 1
					}
				}
				if fp.Realm != ag.active.realmPart {
					goto enter
				}
				continue
			}
			if info := fn.Info; info != nil {
				// The instructions that construct with a native function call Construct.
				if validateExec && (isNew || newTarget != nil) {
					panic("engine: construction in a call of a native function")
				}
				if info.Native.HasFast() {
					k := nativeFastOf(info.Native)
					if argc == 1 && k.number1 != nil {
						if a := ag.stack[argBase]; a.O == storage.NumberObject {
							x := k.number1(a.N)
							storage.SetNumber(&regs[ret], x)
							continue
						}
					} else if argc == 2 && k.number2 != nil {
						if a, b := ag.stack[argBase], ag.stack[argBase+1]; a.O == storage.NumberObject && b.O == storage.NumberObject {
							x := k.number2(a.N, b.N)
							storage.SetNumber(&regs[ret], x)
							continue
						}
					}
					// Numeric kernels cannot observe the frame. Record the call for the other paths.
					ag.frames[len(ag.frames)-1].pc = pc
					if info.Realm == ag.active.realmPart {
						if v, ok := k.callMethod(info.Realm, callThis, ag.stack[argBase:argBase+argc:argBase+argc]); ok {
							regs = ag.reload(regs, ag.frameBase(), fp)
							regs[ret] = v
							continue
						}
					}
				} else {
					ag.frames[len(ag.frames)-1].pc = pc
				}
				// A native function of the current realm and a proxy are called directly.
				if info.Realm == ag.active.realmPart || info.Realm == nil {
					ag.ensureStack(argBase + argc)
					saved := ag.sp
					ag.sp = argBase + argc
					r := ag.active.invokeNative(f, callThis, ag.stack[argBase:argBase+argc:argBase+argc], nil)
					ag.sp = saved
					regs = ag.reload(regs, ag.frameBase(), fp)
					regs[ret] = r
					continue
				}
			} else {
				ag.frames[len(ag.frames)-1].pc = pc
			}
			if fn.Bound != nil && !isNew && newTarget == nil && len(fn.Bound.Args) == 0 {
				f, callThis = ag.unbindPlain(f)
				goto call
			}
			if c := fn.Code; c != nil && c.IsGenerator && !c.IsAsync && fn.Bound == nil {
				// Run the generator up to its initial suspension as an ordinary frame.
				ag.pushGenerator(f, callThis, ag.stack[argBase:argBase+argc], ret)
				fr := &ag.frames[len(ag.frames)-1]
				fp = fr.fp
				regs = fr.gen.regs
				pc = 0
				if fp.Realm != ag.active.realmPart {
					goto enter
				}
				continue
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := ag.active.callSlow(f, callThis, ag.stack[argBase:argBase+argc:argBase+argc], isNew, newTarget)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = r
			continue
		}

	tailCall:
		// A call in tail position, whose arguments are on the stack from argBase on.
		ag.frames[len(ag.frames)-1].pc = pc
		if f, callThis, argBase, argc = ag.active.unbind(f, callThis, argBase, argc); !ag.active.replaceFrame(f, callThis, argBase, argc) {
			regs = ag.reload(regs, ag.frameBase(), fp)
			goto call
		}
		{
			fr := &ag.frames[len(ag.frames)-1]
			fp = fr.fp
			regs = ag.stack[fr.base : fr.base+fp.Nregs+1]
			pc = 0
			if fp.Realm != ag.active.realmPart {
				goto enter
			}
			continue
		}

	construct:
		// new f(...) for anything but a plain constructor function.
		{
			ag.frames[len(ag.frames)-1].pc = pc
			if f == nil || f.Fn == nil || !ag.active.IsConstructor(f) {
				ag.active.throwNotConstructor(fp, pc, slices.Clip(regs))
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := ag.active.Construct(f, ag.stack[argBase:argBase+argc:argBase+argc], f)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = storage.Value{O: r}
			continue
		}

	superCall:
		// super(...): f is the super constructor and newTarget the new.target of the running one.
		{
			ag.frames[len(ag.frames)-1].pc = pc
			parent := f
			if parent == nil || parent.Fn == nil || !ag.active.IsConstructor(parent) {
				ag.active.ThrowTypeError("super constructor is not a constructor")
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := ag.active.Construct(parent, ag.stack[argBase:argBase+argc:argBase+argc], newTarget)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = storage.Value{O: r}
			continue
		}

	throw:
		ag.frames[len(ag.frames)-1].pc = pc
		if v, st := ag.unwind(stop); st != execCatch {
			return v, st
		}
		{
			fr := &ag.frames[len(ag.frames)-1]
			fp = fr.fp
			pc = fr.pc
			regs = fr.registers(ag.stack)
			if fp.Realm == ag.active.realmPart {
				continue
			}
		}

	enter:
		// The frame that runs from here on belongs to another realm than the active context.
		// The branch on ok predicts well, where a selection would delay the loads through the context.
		if cx, ok := fp.Realm.Owner.(*ExecutionContext); ok {
			ag.install(cx)
		}
	}
}

// unbindPlain returns the target and the this value that a call of the bound function f
// passes its arguments to, following bound functions without bound arguments.
func (ag *agent) unbindPlain(f *storage.Object) (*storage.Object, storage.Value) {
	var this storage.Value
	for b := f.Fn.Bound; b != nil && len(b.Args) == 0; b = f.Fn.Bound {
		f, this = b.Target, b.This
	}
	return f, this
}

// unbind returns the function that a tail call of f ends up calling, with its this value and
// the stack index and count of its arguments, for the argc arguments at argBase on the stack.
// A bound function stands for its target, with the bound arguments inserted before the others,
// Function.prototype.call of the running realm for its callable this value, and
// Function.prototype.apply of the running realm for its callable this value when its argument
// list can be made without running JavaScript.
func (cx *ExecutionContext) unbind(f *storage.Object, this storage.Value, argBase, argc int) (*storage.Object, storage.Value, int, int) {
	for f != nil && f.Fn != nil {
		if b := f.Fn.Bound; b != nil {
			if k := len(b.Args); k > 0 {
				cx.ensureStack(argBase + argc + k)
				copy(cx.stack[argBase+k:], cx.stack[argBase:argBase+argc])
				copy(cx.stack[argBase:], b.Args)
				argc += k
			}
			f, this = b.Target, b.This
			continue
		}
		if this.O == nil || this.O.Fn == nil {
			break
		}
		// Function.prototype.call and apply end in a tail call of their this value.
		switch f {
		case cx.Intrinsic(storage.IntrinsicFunctionPrototypeCall):
			f, this = this.O, storage.Undefined
			if argc > 0 {
				this = cx.stack[argBase]
				argBase++
				argc--
			}
			continue
		case cx.Intrinsic(storage.IntrinsicFunctionPrototypeApply):
			var list []storage.Value
			if argc > 1 {
				if a := cx.stack[argBase+1]; !a.IsNullish() {
					var ok bool
					if list, ok = a.O.PlainElems(); !ok {
						return f, this, argBase, argc
					}
				}
			}
			f, this = this.O, storage.Undefined
			if argc > 0 {
				this = cx.stack[argBase]
			}
			// The list goes above the arguments, which may be in the registers of the running frame.
			argBase = max(cx.sp, argBase+argc)
			argc = len(list)
			cx.ensureStack(argBase + argc)
			copy(cx.stack[argBase:], list)
			continue
		}
		break
	}
	return f, this, argBase, argc
}

// replaceFrame replaces the running frame with a frame of f for the argc arguments at argBase on
// the stack, which returns to the caller of the running frame. It reports false, changing nothing,
// when f is not an ordinary function written in JavaScript or the running frame constructs an object.
func (cx *ExecutionContext) replaceFrame(f *storage.Object, this storage.Value, argBase, argc int) bool {
	n := len(cx.frames) - 1
	cur := &cx.frames[n]
	if f == nil || f.Fn == nil || cur.isNew || cur.newTarget != nil {
		return false
	}
	if c := f.Fn.Code; c == nil || c.IsGenerator || c.IsAsync || f.Fn.ClassConstructor {
		return false
	}
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	// The arguments go where the running frame's were.
	start := cur.base
	if cur.extraBase >= 0 {
		start = cur.extraBase - cur.fp.Nparams
	}
	if len(cx.openUpvals) > 0 {
		cx.closeUpvals(start)
	}
	end := max(cx.sp, argBase+argc)
	copy(cx.stack[start:], cx.stack[argBase:argBase+argc])
	ret := cur.ret
	*cur = frame{}
	cx.frames = cx.frames[:n]
	cx.pushFrame(f, this, start, argc, ret, false, nil)
	// Nothing above the new frame is in use any more.
	if cx.sp < end {
		clear(cx.stack[cx.sp:end])
	}
	return true
}

// frameBase returns the stack index of the registers of the running frame.
func (ag *agent) frameBase() int {
	return ag.frames[len(ag.frames)-1].base
}

// reload returns the registers of the running frame after a call that may have moved the stack.
func (ag *agent) reload(regs []storage.Value, base int, fp *storage.FuncProto) []storage.Value {
	if validateExec {
		ag.checkActive(fp)
	}
	if fp.HeapRegs {
		return regs
	}
	if validateExec {
		ag.holdStack()
	}
	return ag.stack[base : base+len(regs)]
}

// loopHolds reports whether the comparison of the loop jump op, one of [bytecode.OpLoopLt]
// through [bytecode.OpLoopGe], holds for x and y.
func (cx *ExecutionContext) loopHolds(op bytecode.Opcode, x, y storage.Value) bool {
	switch op {
	case bytecode.OpLoopLt:
		return cx.compare(x, y, true) == storage.CmpLess
	case bytecode.OpLoopLe:
		c := cx.compare(y, x, false)
		return c == storage.CmpGreater || c == storage.CmpEqual
	case bytecode.OpLoopGt:
		return cx.compare(y, x, false) == storage.CmpLess
	default:
		c := cx.compare(x, y, true)
		return c == storage.CmpGreater || c == storage.CmpEqual
	}
}

// loopHoldsK reports whether the comparison of the loop jump op, one of [bytecode.OpLoopLtK]
// through [bytecode.OpLoopGeK], holds for x and the number k.
func (cx *ExecutionContext) loopHoldsK(op bytecode.Opcode, x, k storage.Value) bool {
	if x.O != storage.NumberObject {
		x = cx.ToNumeric(cx.ToPrimitive(x, HintNumber))
		if x.O != storage.NumberObject {
			c := cx.compareBigIntMixed(x, k)
			switch op {
			case bytecode.OpLoopLtK:
				return c == storage.CmpLess
			case bytecode.OpLoopLeK:
				return c == storage.CmpLess || c == storage.CmpEqual
			case bytecode.OpLoopGtK:
				return c == storage.CmpGreater
			default:
				return c == storage.CmpGreater || c == storage.CmpEqual
			}
		}
	}
	switch op {
	case bytecode.OpLoopLtK:
		return x.N < k.N
	case bytecode.OpLoopLeK:
		return x.N <= k.N
	case bytecode.OpLoopGtK:
		return x.N > k.N
	default:
		return x.N >= k.N
	}
}

// loopHoldsP reports whether the comparison of the loop jump op, one of [bytecode.OpLoopSEqP]
// through [bytecode.OpLoopNotNullish], holds for x and the operand c.
func (ag *agent) loopHoldsP(op bytecode.Opcode, x storage.Value, c int32) bool {
	switch op {
	case bytecode.OpLoopSEqP:
		return x.O == primObjects[c]
	case bytecode.OpLoopSNeP:
		return x.O != primObjects[c]
	case bytecode.OpLoopNullish:
		return x.IsNullish() || c != 0 && x.IsHTMLDDA()
	default:
		return !x.IsNullish() && (c == 0 || !x.IsHTMLDDA())
	}
}
