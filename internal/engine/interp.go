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

	// plainReturn is set when the frame is no activation of a generator and the function of the
	// calling frame belongs to the realm of its own function.
	plainReturn bool

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

// thrownErrorKinds maps the error kinds of OpThrowError to [ErrorKind] values.
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

	// execCatch: unwind found a handler and execution can continue there.
	execCatch
)

// stackHeadroom is the number of values that [agent.ensureStack] keeps free above what it is asked for.
const stackHeadroom = 64

func (ag *agent) ensureStack(n int) {
	ag.stackHigh = max(ag.stackHigh, n)
	if n+stackHeadroom <= len(ag.stack) {
		return
	}
	s := make([]Value, max(n+stackHeadroom, len(ag.stack)*2))
	copy(s, ag.stack)
	ag.stack = s
	// The open upvalues point into the old stack.
	for _, u := range ag.openUpvals {
		u.p = &s[u.index]
	}
}

// maxIdleStack is the number of stack slots that the stack keeps between calls into the engine.
const maxIdleStack = 1 << 16

// releaseStack moves the values in use to a smaller stack when the stack is larger than
// maxIdleStack.
func (ag *agent) releaseStack() {
	if len(ag.stack) <= maxIdleStack || ag.sp+stackHeadroom > maxIdleStack {
		return
	}
	var s []Value
	if ag.sp > 0 {
		s = make([]Value, ag.sp+stackHeadroom)
		copy(s, ag.stack[:ag.sp])
	}
	ag.stack = s
	ag.stackHigh = ag.sp
	// The open upvalues point into the old stack.
	for _, u := range ag.openUpvals {
		u.p = &s[u.index]
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
func (ag *agent) pushFrame(f *Object, this Value, base, argc int, ret int32, isNew bool, newTarget *Object) int {
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
		ag.ensureStack(newBase + fp.nregs + 1)
		// The parameters move above the surplus arguments, so the two places do not overlap.
		stack := ag.stack
		for i := range fp.nparams {
			stack[newBase+i] = stack[base+i]
		}
		base = newBase
	} else {
		ag.ensureStack(base + max(argc, fp.nregs+1))
	}
	if from, to := fp.firstCleared(argc), fp.nclear; from < to {
		clearFrameRegs(ag.stack[base:], from, to)
	}
	fr := ag.appendFrame()
	fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = fp, f, this, newTarget, nil, nil
	fr.pc, fr.base, fr.ret, fr.isNew, fr.argc, fr.extraBase = 0, base, ret, isNew, argc, extraBase
	n := len(ag.frames) - 1
	fr.plainReturn = n > 0 && ag.frames[n-1].fp.context.realm == fp.context.realm
	ag.sp = base + fp.nregs + 1
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
func (ag *agent) extraArgs(fr *frame) []Value {
	if fr.gen != nil {
		return fr.gen.extra
	}
	if fr.extraBase < 0 {
		return nil
	}
	return ag.stack[fr.extraBase : fr.extraBase+fr.argc-fr.fp.nparams]
}

// findUpval returns the upvalue for the stack slot index.
func (ag *agent) findUpval(index int) *upvalue {
	i := len(ag.openUpvals)
	const tail = 4
	if i > 2*tail && ag.openUpvals[i-tail].index > index {
		// Every slot in the tail is above index.
		i = sort.Search(i-tail, func(j int) bool {
			return ag.openUpvals[j].index > index
		})
	} else {
		for i > 0 && ag.openUpvals[i-1].index > index {
			i--
		}
	}
	if i > 0 && ag.openUpvals[i-1].index == index {
		return ag.openUpvals[i-1]
	}
	u := &upvalue{
		p:     &ag.stack[index],
		index: index,
	}
	ag.openUpvals = append(ag.openUpvals, nil)
	copy(ag.openUpvals[i+1:], ag.openUpvals[i:])
	ag.openUpvals[i] = u
	return u
}

// closeUpvals detaches the upvalues that refer to the stack from index from on.
func (ag *agent) closeUpvals(from int) {
	n := len(ag.openUpvals)
	for n > 0 && ag.openUpvals[n-1].index >= from {
		u := ag.openUpvals[n-1]
		u.val = *u.p
		u.p = &u.val
		ag.openUpvals[n-1] = nil
		n--
	}
	ag.openUpvals = ag.openUpvals[:n]
}

// unwind finds the handler for the pending exception, dropping the frames that have none.
// It returns execCatch at a handler, or the completion that left the frame at stop.
func (ag *agent) unwind(stop int) (Value, execStatus) {
	for {
		n := len(ag.frames) - 1
		fr := &ag.frames[n]
		pc := int32(fr.pc - 1)
		closing := ag.thrown.o == closeSignal
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
				regs = ag.stack[fr.base:]
				if len(ag.openUpvals) > 0 {
					ag.closeUpvals(fr.base + int(h.Reg))
				}
			}
			regs[h.Reg] = ag.thrown
			if h.Finally && !closing && ag.captureThrowSites {
				regs[h.Reg] = ag.saveException()
			}
			ag.thrown = Undefined
			ag.dropNatives(n)
			fr.pc = int(h.Target)
			fr.scopes = fr.scopes[:min(len(fr.scopes), int(h.Scopes))]
			ag.sp = fr.top()
			return Undefined, execCatch
		}
		var closeValue Value
		if fr.gen != nil {
			closeValue = fr.gen.complete()
		} else if len(ag.openUpvals) > 0 {
			ag.closeUpvals(fr.base)
		}
		*fr = frame{}
		ag.frames = ag.frames[:n]
		if n == stop {
			if closing {
				ag.thrown = Undefined
				ag.dropNatives(n)
				return closeValue, execReturn
			}
			return Undefined, execThrow
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
func (cx *ExecutionContext) run(stop int) Value {
	v, ok := cx.runFrame(stop)
	if !ok {
		panic(errThrow)
	}
	return v
}

// runFrame executes the frame at stop until it returns, yields, or unwinds with an exception.
// An unwound frame leaves the exception pending and returns false.
// At the depth limit, the frames from stop are dropped and a RangeError is thrown.
func (cx *ExecutionContext) runFrame(stop int) (Value, bool) {
	if cx.depth >= maxDepth {
		cx.unwindTo(stop)
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	cx.depth++
	// The frames may belong to other realms than the code that started the loop.
	saved, outer := cx.active, cx.callerRealm
	cx.callerRealm = cx.realm
	for {
		v, st := cx.exec(stop)
		if validateExec {
			cx.leaveChecks()
		}
		if st == execPanic {
			v, st = cx.unwind(stop)
			if st == execCatch {
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
		r = cx.frames[n-1].fp.context.realm
	}
	cx.frames[n].pc = pc
	cx.recordThrow(r.context.NewError(kind, msg))
	// The constructor has returned. Unwind from before its handlers.
	cx.frames[n].pc = 0
	panic(errThrow)
}

// restArray returns the arguments of fr starting at index start as a fresh array.
func (cx *ExecutionContext) restArray(fr *frame, regs []Value, start int) *Object {
	n := min(fr.argc, fr.fp.nparams)
	extra := cx.extraArgs(fr)
	if start >= n {
		return cx.NewArrayCopy(extra[start-n:])
	}
	args := make([]Value, 0, fr.argc-start)
	args = append(args, regs[start:n]...)
	args = append(args, extra...)
	return cx.NewArray(args)
}

// exec is the interpreter loop. See run.
func (ag *agent) exec(stop int) (result Value, status execStatus) {
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
	fp.context.activate()
	if validateExec {
		ag.enterChecks()
	}
	// The capacity of regs is never read: it is sliced and passed on only through slices.Clip,
	// so that the loop does not carry it from one instruction to the next.
	var regs []Value
	if fp.heapRegs {
		regs = fr.gen.regs
	} else {
		regs = ag.stack[fr.base : fr.base+fp.nregs+1]
	}

	for {
		if validateExec {
			ag.checkStep(fp, pc, slices.Clip(regs))
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
		case bytecode.OpRequireCoercible:
			if !regs[i.A].IsNullish() {
				continue
			}
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
			regs[i.A] = *ag.frames[len(ag.frames)-1].cl.fn.upvals[i.B].p
			continue
		case bytecode.OpSetUpval:
			*ag.frames[len(ag.frames)-1].cl.fn.upvals[i.B].p = regs[i.A]
			continue
		case bytecode.OpCopyPropToGlobal:
			g := &fp.globals[fp.code[i.C-1].C]
			if h := g.holder; h == nil || h.shape != g.shape {
				break
			}
			regs[i.A] = Value{}
			c := &fp.caches[fp.code[pc].C]
			o := regs[i.B].o
			if o == nil || c.shape != o.shape {
				continue
			}
			if c.kind == cacheOwnGetter || c.kind == cacheGetter {
				// The binding is resolved. Dispatch the read with its own call site.
				i = &fp.code[pc]
				pc++
				break
			}
			var v Value
			switch c.kind {
			case cacheOwn:
				v = o.slots[c.slot]
			case cacheProto:
				if c.epoch != ag.protoEpoch {
					continue
				}
				v = *c.ref
			default:
				continue
			}
			regs[i.A+1] = v
			*g.ref = v
			pc = int(i.C)
			continue
		case bytecode.OpResolveGlobal:
			c := &fp.globals[i.C]
			if h := c.holder; h == nil || h.shape != c.shape {
				break
			}
			regs[i.A] = Value{}
			// A named read may supply the value of the assignment.
			if n := &fp.code[pc]; n.Op == bytecode.OpGetProp {
				c := &fp.caches[n.C]
				if o := regs[n.B].o; o != nil && c.kind == cacheOwn && c.shape == o.shape {
					regs[n.A] = o.slots[c.slot]
					pc++
				}
			}
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
					if e.kind != cacheProto || e.epoch != ag.protoEpoch {
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
					if e.kind != cacheProto || e.epoch != ag.protoEpoch {
						break
					}
					w = *e.ref
				} else {
					w = o.slots[e.slot]
				}
				regs[a] = w
				// Consume the next named read of w if its cache supplies a data property.
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
						if e.kind != cacheProto || e.epoch != ag.protoEpoch {
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
				if e.kind == cacheAdd && o.objectProperties != nil && e.epoch == ag.protoEpoch && o.flags&objIsProto == 0 {
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
			// The destination and the cache are read first, so that the instruction need not
			// stay in registers across the tests, as in OpGetProp.
			if a, c, o := i.A, &fp.caches[i.C], regs[i.B].o; o != nil {
				// The length of a string known to be ASCII, as in Value.StringLength.
				if o.kind == kindString && o.flags&(objASCIIKnown|objASCII) == objASCIIKnown|objASCII {
					setNumber(&regs[a], float64(len(o.str)))
					continue
				}
				// The length of a fixed-length typed array in the bounds of its buffer, when the
				// cache says that the length is the original getter, as in getNamed. The data of a
				// shared or detached buffer is empty, so only an empty view of one passes the
				// bounds check.
				if t, ok := o.ext.(*TypedArray); ok && !t.tracking && t.offset+t.length*t.typ.Size() <= len(t.data.data) {
					if o.shape == c.shape && c.kind == cacheGetter && c.epoch == ag.protoEpoch && c.holder == fp.context.realm.intrinsics[IntrinsicTypedArrayPrototypeLengthGetter] {
						setNumber(&regs[a], float64(t.length))
						continue
					}
				}
				// A length data property that the cache locates, as in OpGetProp.
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
				}
				if o.shape == e.shape {
					if e.kind == cacheOwn {
						regs[a] = o.slots[e.slot]
						continue
					}
					if e.kind == cacheProto && e.epoch == ag.protoEpoch {
						regs[a] = *e.ref
						continue
					}
				}
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
				// The elements of a typed array of numbers other than Float16 whose bytes are all in
				// the data of its buffer, as in typedArrayGet. The checks and the destination come
				// before the data is loaded: a path to the switch below, or a use of i, that needs
				// a register the load took makes the compiler store loop values at the head of the
				// loop on amd64. The functions of encoding/binary do not inline into exec, so the
				// bytes are combined here.
				if t, ok := o.ext.(*TypedArray); ok && t.typ < ElementBigInt64 && t.typ != ElementFloat16 {
					if j := int(k.n); float64(j) == k.n && !t.tracking && uint(j) < uint(t.length) {
						if r, s := &regs[i.A], t.typ.Size(); t.offset+t.length*s <= len(t.data.data) {
							b := t.data.data[t.offset+j*s:]
							var x float64
							switch t.typ {
							case ElementInt8:
								x = float64(int8(b[0]))
							case ElementUint8, ElementUint8C:
								x = float64(b[0])
							case ElementInt16:
								_ = b[1]
								x = float64(int16(uint16(b[0]) | uint16(b[1])<<8))
							case ElementUint16:
								_ = b[1]
								x = float64(uint16(b[0]) | uint16(b[1])<<8)
							case ElementInt32:
								_ = b[3]
								x = float64(int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
							case ElementUint32:
								_ = b[3]
								x = float64(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
							case ElementFloat32:
								_ = b[3]
								x = float64(math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
							default:
								_ = b[7]
								x = math.Float64frombits(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
									uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
							}
							setNumber(r, x)
							continue
						}
					}
				}
			} else if o != nil && o.kind == kindString && o.flags&(objASCIIKnown|objASCII) == objASCIIKnown|objASCII && k.o == numberObject {
				// A unit of a string known to be ASCII, as in asciiUnitAt. An index in its range is an
				// own property that nothing shadows.
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.str)) {
					if o.flags&objReversed != 0 {
						j = len(o.str) - 1 - j
					}
					regs[i.A] = ag.strings.ascii[o.str[j]]
					continue
				}
			}
		case bytecode.OpGetIndexInt:
			if o := regs[i.B].o; o != nil && o.objectProperties != nil && uint(i.C) < uint(len(o.elems)) {
				if e := o.elems[i.C]; e.o != holeObject {
					regs[i.A] = e
					continue
				}
			}
			// The same as OpGetIndex for a typed array.
			if o := regs[i.B].o; o != nil {
				if t, ok := o.ext.(*TypedArray); ok && t.typ < ElementBigInt64 && t.typ != ElementFloat16 && !t.tracking && uint(i.C) < uint(t.length) {
					if j, r, s := int(i.C), &regs[i.A], t.typ.Size(); t.offset+t.length*s <= len(t.data.data) {
						b := t.data.data[t.offset+j*s:]
						var x float64
						switch t.typ {
						case ElementInt8:
							x = float64(int8(b[0]))
						case ElementUint8, ElementUint8C:
							x = float64(b[0])
						case ElementInt16:
							_ = b[1]
							x = float64(int16(uint16(b[0]) | uint16(b[1])<<8))
						case ElementUint16:
							_ = b[1]
							x = float64(uint16(b[0]) | uint16(b[1])<<8)
						case ElementInt32:
							_ = b[3]
							x = float64(int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
						case ElementUint32:
							_ = b[3]
							x = float64(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
						case ElementFloat32:
							_ = b[3]
							x = float64(math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24))
						default:
							_ = b[7]
							x = math.Float64frombits(uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
								uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56)
						}
						setNumber(r, x)
						continue
					}
				}
				// The same as OpGetIndex for a string.
				if o.kind == kindString && o.flags&(objASCIIKnown|objASCII) == objASCIIKnown|objASCII && uint(i.C) < uint(len(o.str)) {
					j := int(i.C)
					if o.flags&objReversed != 0 {
						j = len(o.str) - 1 - j
					}
					regs[i.A] = ag.strings.ascii[o.str[j]]
					continue
				}
			}
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict:
			// OpSetIndexInt's case at the end of this switch has the same fast paths for the key in
			// the instruction; a change to one belongs in the other.
			v, k := regs[i.A], regs[i.B]
			if o := v.o; o != nil && o.objectProperties != nil && k.o == numberObject {
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.elems)) {
					if o.elems[j].o != holeObject {
						o.elems[j] = regs[i.C]
						continue
					}
					// A store into a hole takes the switch below.
					break
				}
				// The stores of a number into a typed array of numbers other than Float16, as in
				// typedArraySet, with every check before the data is loaded as in OpGetIndex. A
				// number beyond the range of int64, NaN included, goes to the switch below, so that
				// an integer element takes the low bytes of int64(f).
				if t, ok := o.ext.(*TypedArray); ok && t.numberStore && t.typ != ElementFloat16 {
					if j, x := int(k.n), regs[i.C]; x.o == numberObject && float64(j) == k.n && !t.tracking && uint(j) < uint(t.length) && numconv.InInt64Range(x.n) {
						if s := t.typ.Size(); t.offset+t.length*s <= len(t.data.data) {
							b, f := t.data.data[t.offset+j*s:], x.n
							// The most common types are tested first.
							if u := uint64(int64(f)); t.typ == ElementInt32 || t.typ == ElementUint32 {
								_ = b[3]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
								b[2] = byte(u >> 16)
								b[3] = byte(u >> 24)
							} else if t.typ == ElementFloat64 {
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
							} else if t.typ == ElementInt8 || t.typ == ElementUint8 {
								b[0] = byte(u)
							} else if t.typ == ElementFloat32 {
								bits := math.Float32bits(float32(f))
								_ = b[3]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
							} else if t.typ == ElementUint8C {
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
				// conditions of arrayProtoPlain written out.
				if j := int(k.n); j == len(o.elems) && j < cap(o.elems) &&
					o.kind == kindArray && uint32(j) == o.length && o.flags == 0 && float64(j) == k.n && o.shape == fp.context.realm.shapes.array {
					if a := fp.context.realm.intrinsics[IntrinsicArrayPrototype]; len(a.elems) == 0 && a.flags&objSparse == 0 {
						if p := a.shape.proto; p == fp.context.realm.intrinsics[IntrinsicObjectPrototype] && len(p.elems) == 0 && p.flags&objSparse == 0 {
							// Reading x first keeps i.C from being read again after the
							// store to the length.
							x := regs[i.C]
							o.elems = o.elems[:j+1]
							o.elems[j] = x
							o.length++
							continue
						}
					}
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
			// IsLooselyEqual, for the operands it can tell without a call, as in looseEquals:
			// two numbers, one value twice, an undefined or null operand, two objects, and two
			// interned strings, which differ in content when they are different objects.
			x, y := regs[i.B], regs[i.C]
			eq, known := false, true
			if x.o == numberObject && y.o == numberObject {
				eq = x.n == y.n
			} else if x.o == y.o && math.Float64bits(x.n) == math.Float64bits(y.n) {
				eq = true
			} else if xn, yn := x.IsNullish(), y.IsNullish(); xn || yn {
				eq = xn && yn || x.isHTMLDDA() || y.isHTMLDDA()
			} else if x.o.kind >= kindFirstObject && y.o.kind >= kindFirstObject {
				eq = x.o == y.o
			} else if x.o == y.o || x.o.flags&y.o.flags&objInterned == 0 {
				known = false
			}
			if known {
				regs[i.A] = Bool(eq == (i.Op == bytecode.OpEq))
				continue
			}
		case bytecode.OpNot:
			// ToBoolean, for the kinds it can tell without a call, as in OpJumpIf.
			if v := regs[i.B]; v.o == numberObject {
				regs[i.A] = Bool(v.n == 0 || v.n != v.n)
				continue
			} else if o := v.o; o == nil || o.kind <= kindFalse {
				regs[i.A] = Bool(true)
				continue
			} else if o.kind == kindTrue || o.kind >= kindObject {
				// kindHTMLDDA is below kindObject and takes the slow path.
				regs[i.A] = Bool(false)
				continue
			}
		case bytecode.OpJump:
			if int(i.A) >= pc || !ag.interrupt.Load() {
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
			// The same cases as OpEq.
			x, y := regs[i.B], regs[i.C]
			eq, known := false, true
			if x.o == numberObject && y.o == numberObject {
				eq = x.n == y.n
			} else if x.o == y.o && math.Float64bits(x.n) == math.Float64bits(y.n) {
				eq = true
			} else if xn, yn := x.IsNullish(), y.IsNullish(); xn || yn {
				eq = xn && yn || x.isHTMLDDA() || y.isHTMLDDA()
			} else if x.o.kind >= kindFirstObject && y.o.kind >= kindFirstObject {
				eq = x.o == y.o
			} else if x.o == y.o || x.o.flags&y.o.flags&objInterned == 0 {
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
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if x.n != y.n {
					pc = int(i.A)
				}
				continue
			} else if x.o == y.o && math.Float64bits(x.n) == math.Float64bits(y.n) {
				continue
			} else if x.o != y.o && x.o != nil && y.o != nil && x.o.flags&y.o.flags&objInterned != 0 {
				pc = int(i.A)
				continue
			}
		case bytecode.OpJumpNotSNe:
			// The same cases as OpJumpNotSEq.
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if x.n == y.n {
					pc = int(i.A)
				}
				continue
			} else if x.o == y.o && math.Float64bits(x.n) == math.Float64bits(y.n) {
				pc = int(i.A)
				continue
			} else if x.o != y.o && x.o != nil && y.o != nil && x.o.flags&y.o.flags&objInterned != 0 {
				continue
			}
		case bytecode.OpStrictEq, bytecode.OpStrictNe:
			// The same cases as OpJumpNotSEq.
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				regs[i.A] = Bool((x.n == y.n) == (i.Op == bytecode.OpStrictEq))
				continue
			} else if x.o == y.o && math.Float64bits(x.n) == math.Float64bits(y.n) {
				regs[i.A] = Bool(i.Op == bytecode.OpStrictEq)
				continue
			} else if x.o != y.o && x.o != nil && y.o != nil && x.o.flags&y.o.flags&objInterned != 0 {
				regs[i.A] = Bool(i.Op != bytecode.OpStrictEq)
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
				if co.shape == c.shape && c.kind == cacheOwn && c.epoch == ag.protoEpoch && co.class == classFunction && co.fn != nil && co.fn.bound == nil {
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
				// A data property read through the cache, as in OpGetProp's case. An immediately
				// following OpCall on the same register runs here too.
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
					if e.kind != cacheProto || e.epoch != ag.protoEpoch {
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
			if ag.interrupt.Load() {
				break
			}
			// f is the callee and first the register of its first argument. The this value is in
			// the register before first, and is read only once the frame is certain, so that it
			// takes no registers while the frame is checked.
			b := int(i.B)
			f, first, argc := regs[b].o, b+2, int(i.C)
			if i.Op == bytecode.OpCallCall {
				if f != fp.context.realm.intrinsics[IntrinsicFunctionPrototypeCall] {
					f = nil
				} else {
					f, first, argc = regs[b+1].o, b+3, max(argc-1, 0)
				}
			}
			if !fp.heapRegs && f != nil && f.fn != nil {
				if c := f.fn.code; c != nil && c.inlineFrames && c.context == fp.context {
					n := len(ag.frames)
					argBase := ag.frames[n-1].base + first
					end := argBase + max(argc, c.nregs+1)
					// The frames have room for at most maxFrames.
					if (argc <= c.nparams || !c.needsExtra) && end+stackHeadroom <= len(ag.stack) && n < cap(ag.frames) {
						var this Value
						if i.Op != bytecode.OpCallUndef && (i.Op != bytecode.OpCallCall || i.C > 0) {
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
						ag.frames[n-1].pc = pc
						ag.stackHigh = max(ag.stackHigh, end)
						if from, to := c.firstCleared(argc), c.nclear; from < to {
							clearFrameRegs(ag.stack[argBase:], from, to)
						}
						ag.frames = ag.frames[:n+1]
						ag.framesHigh = max(ag.framesHigh, n+1)
						fr := &ag.frames[n]
						fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
						// The callee belongs to the current realm, which is that of the running frame.
						fr.pc, fr.base, fr.ret, fr.isNew, fr.plainReturn, fr.argc, fr.extraBase = 0, argBase, i.A, false, true, argc, -1
						ag.sp = argBase + c.nregs + 1
						fp, pc = c, 0
						regs = ag.stack[argBase:ag.sp]
						if r := c.thisReg; r >= 0 && (!c.thisSloppy || this.o != nil && this.o.kind >= kindFirstObject) {
							// Do what the first instruction does, and start after it.
							regs[r] = this
							pc = 1
						}
						continue
					}
				} else if c == nil && argc == 2 {
					// Math.min and Math.max of two numbers. Go's min and max of floats give NaN
					// when an argument is NaN and order -0 below +0, as the specification does.
					if info := f.fn.info; info != nil && info.extremum != ExtremumNone {
						if x, y := regs[first], regs[first+1]; x.o == numberObject && y.o == numberObject {
							if info.extremum == ExtremumMin {
								setNumber(&regs[i.A], min(x.n, y.n))
							} else {
								setNumber(&regs[i.A], max(x.n, y.n))
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
			r := Value{}
			if i.Op == bytecode.OpReturn {
				r = regs[i.A]
			}
			n := len(ag.frames) - 1
			if done := &ag.frames[n]; done.plainReturn && n != stop && (len(ag.openUpvals) == 0 || ag.openUpvals[len(ag.openUpvals)-1].index < done.base) {
				if done.isNew && (r.o == nil || r.o.kind < kindFirstObject) {
					r = done.this
				}
				ret := done.ret
				ag.frames = ag.frames[:n]
				fr := &ag.frames[n-1]
				fp = fr.fp
				pc = fr.pc
				if fp.heapRegs {
					regs = fr.gen.regs
					ag.sp = fr.base
				} else {
					regs = ag.stack[fr.base : fr.base+fp.nregs+1]
					ag.sp = fr.base + fp.nregs + 1
				}
				regs[ret] = r
				continue
			}
		case bytecode.OpGetArgsLength:
			// The argument count, while the arguments object is not materialized.
			if a := &regs[i.A]; regs[i.B].o == nil {
				setNumber(a, float64(ag.frames[len(ag.frames)-1].argc))
				continue
			}
		case bytecode.OpGetArgsIndex:
			// An argument, while the arguments object is not materialized, where the call left
			// it: in a parameter register, or among the surplus arguments below the registers.
			// The destination is read before the tests and the argument after them, as in
			// OpGetIndex.
			if a, k := &regs[i.A], regs[i.C]; regs[i.B].o == nil && k.o == numberObject {
				fr := &ag.frames[len(ag.frames)-1]
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(fr.argc) {
					if n := fp.nparams; j < n {
						*a = regs[j]
					} else {
						*a = ag.stack[fr.extraBase+j-n]
					}
					continue
				}
			}
		case bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
			// The fast paths of OpSetIndex's case, in the same order, for the key b.
			if o := regs[i.A].o; o != nil && o.objectProperties != nil {
				j := int(i.B)
				if uint(j) < uint(len(o.elems)) {
					if o.elems[j].o != holeObject {
						o.elems[j] = regs[i.C]
						continue
					}
					break
				}
				if t, ok := o.ext.(*TypedArray); ok && t.numberStore && t.typ != ElementFloat16 {
					if x := regs[i.C]; x.o == numberObject && !t.tracking && uint(j) < uint(t.length) && numconv.InInt64Range(x.n) {
						if s := t.typ.Size(); t.offset+t.length*s <= len(t.data.data) {
							b, f := t.data.data[t.offset+j*s:], x.n
							// The most common types are tested first.
							if u := uint64(int64(f)); t.typ == ElementInt32 || t.typ == ElementUint32 {
								_ = b[3]
								b[0] = byte(u)
								b[1] = byte(u >> 8)
								b[2] = byte(u >> 16)
								b[3] = byte(u >> 24)
							} else if t.typ == ElementFloat64 {
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
							} else if t.typ == ElementInt8 || t.typ == ElementUint8 {
								b[0] = byte(u)
							} else if t.typ == ElementFloat32 {
								bits := math.Float32bits(float32(f))
								_ = b[3]
								b[0] = byte(bits)
								b[1] = byte(bits >> 8)
								b[2] = byte(bits >> 16)
								b[3] = byte(bits >> 24)
							} else if t.typ == ElementUint8C {
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
				if j == len(o.elems) && j < cap(o.elems) && o.kind == kindArray && uint32(j) == o.length && o.flags == 0 && o.shape == fp.context.realm.shapes.array {
					if a := fp.context.realm.intrinsics[IntrinsicArrayPrototype]; len(a.elems) == 0 && a.flags&objSparse == 0 {
						if p := a.shape.proto; p == fp.context.realm.intrinsics[IntrinsicObjectPrototype] && len(p.elems) == 0 && p.flags&objSparse == 0 {
							x := regs[i.C]
							o.elems = o.elems[:j+1]
							o.elems[j] = x
							o.length++
							continue
						}
					}
				}
			}
		case bytecode.OpLoopIf:
			// The ToBoolean of OpJumpIf. A pending interruption takes the switch below.
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
				t = true
			} else {
				known = false
			}
			if known {
				if !t {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n < y.n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n <= y.n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGt:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n > y.n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGe:
			if x, y := regs[i.B], regs[i.C]; x.o == numberObject && y.o == numberObject {
				if !(x.n >= y.n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLtK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n < fp.consts[i.C].n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopLeK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n <= fp.consts[i.C].n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGtK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n > fp.consts[i.C].n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopGeK:
			if x := regs[i.B]; x.o == numberObject {
				if !(x.n >= fp.consts[i.C].n) {
					continue
				}
				if !ag.interrupt.Load() {
					pc = int(i.A)
					continue
				}
			}
		case bytecode.OpLoopSEqP:
			if regs[i.B].o != primObjects[i.C] {
				continue
			}
			if !ag.interrupt.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopSNeP:
			if regs[i.B].o == primObjects[i.C] {
				continue
			}
			if !ag.interrupt.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopNullish:
			// The kind of an object is read only when it can be an IsHTMLDDA object.
			if x := regs[i.B]; !x.IsNullish() && (i.C == 0 || !ag.madeHTMLDDA || !x.isHTMLDDA()) {
				continue
			}
			if !ag.interrupt.Load() {
				pc = int(i.A)
				continue
			}
		case bytecode.OpLoopNotNullish:
			if x := regs[i.B]; x.IsNullish() || i.C != 0 && ag.madeHTMLDDA && x.isHTMLDDA() {
				continue
			}
			if !ag.interrupt.Load() {
				pc = int(i.A)
				continue
			}
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

		// Slow property operations take their operands by value across reentry.
		var receiver, key, rhs Value
		var setter, name *Object
		var site *propCache

		// Every instruction that the fast paths above leave, in full.
		switch i.Op {
		case bytecode.OpGetUpvalChecked:
			v := *ag.frames[len(ag.frames)-1].cl.fn.upvals[i.B].p
			if v.o == tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				fp.context.throwTDZ(fp.keys[i.C])
			}
			regs[i.A] = v
		case bytecode.OpCheckTDZ:
			if regs[i.A].o == tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				fp.context.throwTDZ(fp.keys[i.B])
			}
		case bytecode.OpThrowConstAssign:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(fp.keys[i.A].str) + "'")
		case bytecode.OpThrowError:
			ag.frames[len(ag.frames)-1].pc = pc
			// Error text is an interned constant.
			fp.context.throwError(thrownErrorKinds[i.A], wtf8.ToValidUTF8(fp.consts[i.B].o.str))

		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			goto readProperty
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
			rhs = regs[i.A]
			goto writeProperty
		case bytecode.OpCopyPropToGlobal, bytecode.OpResolveGlobal:
			goto readProperty
		case bytecode.OpDeclareGlobalVar:
			ag.frames[len(ag.frames)-1].pc = pc
			var fn Value
			if i.C >= 0 {
				fn = regs[i.C]
			}
			fp.context.declareGlobalVar(fp.keys[i.A], int(i.B), fn)
		case bytecode.OpDeclareGlobalLex:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.declareGlobalLex(fp.keys[i.A], i.B != 0)
		case bytecode.OpInitGlobalLex:
			fp.context.initGlobalLex(fp.keys[i.B], regs[i.A])
		case bytecode.OpCheckGlobalDecl:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.checkGlobalDecl(fp.keys[i.A], int(i.B))
		case bytecode.OpDeclareAnnexB:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = Bool(fp.context.declareGlobalAnnexB(fp.keys[i.B], int(i.C)))

		case bytecode.OpCloseUpvals:
			if fp.heapRegs {
				ag.frames[len(ag.frames)-1].gen.closeUpvals(int(i.A))
			} else if len(ag.openUpvals) > 0 {
				ag.closeUpvals(ag.frameBase() + int(i.A))
			}
		case bytecode.OpClosure:
			p := fp.protos[i.B]
			o, copies := newClosure(p)
			fn := o.fn
			o.kind = kindFunction
			o.class = classFunction
			o.flags = objExotic
			o.shape = fp.context.realm.shapes.function
			if p.isGenerator || p.isAsync {
				o.shape = fp.context.functionShapeFor(p.closurePrototype())
			}
			for j, d := range p.Upvals {
				switch {
				case !d.Local:
					fn.upvals[j] = ag.frames[len(ag.frames)-1].cl.fn.upvals[d.Index]
				case d.ByValue:
					u := &copies[0]
					copies = copies[1:]
					u.val = regs[d.Index]
					u.p = &u.val
					fn.upvals[j] = u
				case fp.heapRegs:
					fn.upvals[j] = ag.frames[len(ag.frames)-1].gen.findUpval(int(d.Index))
				default:
					fn.upvals[j] = ag.findUpval(ag.frameBase() + int(d.Index))
				}
			}
			regs[i.A] = Value{o: o}

		case bytecode.OpLoadThis:
			regs[i.A] = ag.frames[len(ag.frames)-1].this
		case bytecode.OpLoadThisSloppy:
			t := ag.frames[len(ag.frames)-1].this
			if t.o == nil || t.o.kind < kindFirstObject {
				if t.IsNullish() {
					t = Value{o: fp.context.realm.globalThis}
				} else {
					t = Value{o: fp.context.ToObject(t)}
				}
			}
			regs[i.A] = t
		case bytecode.OpLoadCallee:
			regs[i.A] = Value{o: ag.frames[len(ag.frames)-1].cl}
		case bytecode.OpLoadNewTarget:
			if nt := ag.frames[len(ag.frames)-1].newTarget; nt != nil {
				regs[i.A] = Value{o: nt}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpLoadHome:
			if h := ag.frames[len(ag.frames)-1].cl.fn.home; h != nil {
				regs[i.A] = Value{o: h}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpArguments:
			cur := &ag.frames[len(ag.frames)-1]
			regs[i.A] = Value{o: fp.context.newArguments(cur, slices.Clip(regs), i.B != 0)}
		case bytecode.OpRestArgs:
			cur := &ag.frames[len(ag.frames)-1]
			var rest *Object
			if int(i.B) < cur.argc {
				rest = fp.context.restArray(cur, slices.Clip(regs), int(i.B))
			} else {
				rest = fp.context.NewArray(nil)
			}
			regs[i.A] = Value{o: rest}
		case bytecode.OpGetArgsLength, bytecode.OpGetArgsIndex:
			// A prototype getter must see the arguments object shared by later reads.
			ag.frames[len(ag.frames)-1].pc = pc
			receiver = fp.context.forwardedArguments(slices.Clip(regs), i.B)
			if i.Op == bytecode.OpGetArgsLength {
				name = ag.Key(KeyLength)
			} else {
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
			c := &fp.caches[i.C]
			o := v.o
			if o == numberObject {
				// getNamed starts at Number.prototype for a number. Its own slot
				// stays valid for its shape, and the method receives v as this.
				o = fp.context.realm.intrinsics[IntrinsicNumberPrototype]
				if c.kind == cacheOwn && c.shape == o.shape {
					regs[i.A] = o.slots[c.slot]
					continue
				}
			}
			if o != nil {
				if e := c.entry(o.shape); e != nil {
					if e.kind == cacheGetter && e.epoch == ag.protoEpoch {
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
			receiver, name, site = v, fp.keys[i.C], c
			goto readProperty
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			v := regs[i.A]
			c := &fp.caches[i.B]
			if o := v.o; o != nil {
				if e := c.entry(o.shape); e != nil {
					switch e.kind {
					case cacheAdd:
						if o.objectProperties != nil && e.epoch == ag.protoEpoch && o.flags&objIsProto == 0 {
							o.shape = e.next
							o.appendSlot(regs[i.C])
							continue
						}
					case cacheSetter:
						if e.epoch == ag.protoEpoch {
							setter, receiver, rhs = e.holder, v, regs[i.C]
							goto writeProperty
						}
					}
				}
			}
			receiver, name, rhs, site = v, fp.keys[i.B], regs[i.C], c
			goto writeProperty
		case bytecode.OpGetLength:
			v := regs[i.B]
			if v.IsString() {
				setNumber(&regs[i.A], float64(v.StringLength()))
				continue
			}
			receiver, name, site = v, ag.Key(KeyLength), &fp.caches[i.C]
			goto readProperty
		case bytecode.OpGetIndex:
			receiver, key = regs[i.B], regs[i.C]
			goto readProperty
		case bytecode.OpGetIndexInt:
			receiver = regs[i.B]
			goto readProperty
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
			// OpSetIndexInt and OpSetIndexIntStrict follow OpSetIndex and OpSetIndexStrict.
			v, k := regs[i.A], Value{}
			if i.Op < bytecode.OpSetIndexInt {
				k = regs[i.B]
			} else {
				k = Number(float64(i.B))
			}
			if o := v.o; o != nil && o.objectProperties != nil && k.o == numberObject {
				if j := int(k.n); j == len(o.elems) && o.kind == kindArray && uint32(j) == o.length && o.flags == 0 && float64(j) == k.n && o.shape == fp.context.realm.shapes.array && fp.context.arrayProtoPlain() {
					// Appending to a plain array.
					o.appendElem(regs[i.C])
					o.length++
					continue
				}
			}
			receiver, key, rhs = v, k, regs[i.C]
			goto writeProperty
		case bytecode.OpSuperBase:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = fp.context.superBase(regs[i.B])
		case bytecode.OpGetSuper:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			ext := fp.code[pc]
			pc++
			if ext.B == 0 {
				v, _ := fp.context.getSuper(fp.context.superBase(regs[ext.A]), regs[i.C], regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			v, key := fp.context.getSuper(regs[ext.A], regs[i.C], regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.C] = fp.context.KeyValue(key)
			regs[i.A] = v
		case bytecode.OpSetSuper, bytecode.OpSetSuperStrict:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			superBase := regs[fp.code[pc].A]
			pc++
			fp.context.setSuper(superBase, regs[i.B], regs[i.C], regs[i.A], i.Op == bytecode.OpSetSuperStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDeleteProp, bytecode.OpDeletePropStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.deleteValue(regs[i.B], regs[i.C], i.Op == bytecode.OpDeletePropStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpIn:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				fp.context.ThrowTypeError("cannot use 'in' operator to search for a key in a non-object")
			}
			r := fp.context.HasProperty(o.o, fp.context.QueryKey(regs[i.C]))
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpInstanceof:
			site := &fp.caches[fp.code[pc].A]
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.instanceOf(regs[i.B], regs[i.C], site)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpHasPrivate:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.B]
			if !o.IsObject() {
				fp.context.ThrowTypeError("cannot use 'in' operator to search for a private field in a non-object")
			}
			_, ok := fp.context.privateStorage(o.o).shape.lookup(regs[i.C].o)
			regs[i.A] = Bool(ok)
		case bytecode.OpGetPrivate:
			v := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			val, getter := fp.context.getPrivate(v, regs[i.C].o)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetPrivate:
			v := regs[i.A]
			ag.frames[len(ag.frames)-1].pc = pc
			if setter := fp.context.setPrivate(v, regs[i.B].o, regs[i.C]); setter != nil {
				val := regs[i.C]
				argBase = ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(argBase + 1)
				ag.stack[argBase] = val
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
			ag.frames[len(ag.frames)-1].pc = pc
			if i.C >= 0 {
				if b := regs[i.C]; b.IsNullish() {
					fp.context.ThrowTypeError("cannot read properties of " + ag.DisplayString(b) + " (reading '" + ag.DisplayString(v) + "')")
				}
			}
			k := fp.context.KeyValue(fp.context.ToPropertyKey(v))
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = k

		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			if x.o != nil && y.o != nil && x.o.kind == kindString && y.o.kind == kindString {
				regs[i.A] = fp.context.Concat(x, y)
				continue
			}
			v := fp.context.addValues(x, y)
			regs = ag.reload(regs, ag.frameBase(), fp)
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
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.addValues(x, fp.consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
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
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.negate(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPlus:
			x := regs[i.B]
			if x.o == numberObject {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			n := fp.context.ToNumber(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Number(n)
		case bytecode.OpNot:
			regs[i.A] = Bool(!ToBoolean(regs[i.B]))
		case bytecode.OpBitNot:
			x := regs[i.B]
			if x.o == numberObject && numconv.InInt64Range(x.n) {
				regs[i.A] = Number(float64(^int32(int64(x.n))))
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.bitNot(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpToNumeric:
			x := regs[i.B]
			if x.o == numberObject {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.ToNumeric(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpPostInc, bytecode.OpPostDec:
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.ToNumeric(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			d := int64(1)
			if i.Op == bytecode.OpPostDec {
				d = -1
			}
			// Stepping a number or a BigInt runs no JavaScript.
			regs[i.B] = fp.context.step(v, d)
		case bytecode.OpToString:
			x := regs[i.B]
			if x.o != nil && x.o.kind == kindString {
				regs[i.A] = x
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.ToStringValue(x)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpConcat:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = fp.context.ConcatStrings(slices.Clip(regs)[i.B : i.B+i.C])
		case bytecode.OpTypeof:
			regs[i.A] = ag.typeofValue(regs[i.B])
		case bytecode.OpInc, bytecode.OpDec:
			x := regs[i.B]
			ag.frames[len(ag.frames)-1].pc = pc
			d := int64(1)
			if i.Op == bytecode.OpDec {
				d = -1
			}
			v := fp.context.step(x, d)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpLt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.compare(x, y, true) == cmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpLe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := fp.context.compare(y, x, false)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpGt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.compare(y, x, false) == cmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(r)
		case bytecode.OpGe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := fp.context.compare(x, y, true)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpEq, bytecode.OpNe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.looseEquals(regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(r == (i.Op == bytecode.OpEq))
		case bytecode.OpStrictEq:
			regs[i.A] = Bool(IsStrictlyEqual(regs[i.B], regs[i.C]))
		case bytecode.OpStrictNe:
			regs[i.A] = Bool(!IsStrictlyEqual(regs[i.B], regs[i.C]))

		case bytecode.OpJump:
			if int(i.A) < pc && ag.interrupt.Load() {
				// A loop whose condition is not tested at its end jumps backwards with this
				// instruction.
				ag.frames[len(ag.frames)-1].pc = pc
				ag.Poll()
			}
			pc = int(i.A)
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			if ToBoolean(regs[i.B]) == (i.Op == bytecode.OpJumpIf) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.compare(x, y, true) == cmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := fp.context.compare(y, x, false)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGt:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.compare(y, x, false) == cmpLess
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGe:
			x, y := regs[i.B], regs[i.C]
			ag.frames[len(ag.frames)-1].pc = pc
			c := fp.context.compare(x, y, true)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.looseEquals(regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
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
				ag.frames[len(ag.frames)-1].pc = pc
				x = fp.context.ToNumeric(fp.context.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.o != numberObject {
					if fp.context.compareBigIntMixed(x, fp.consts[i.C]) != cmpLess {
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
				ag.frames[len(ag.frames)-1].pc = pc
				x = fp.context.ToNumeric(fp.context.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.o != numberObject {
					if c := fp.context.compareBigIntMixed(x, fp.consts[i.C]); c != cmpLess && c != cmpEqual {
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
				ag.frames[len(ag.frames)-1].pc = pc
				x = fp.context.ToNumeric(fp.context.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.o != numberObject {
					if fp.context.compareBigIntMixed(x, fp.consts[i.C]) != cmpGreater {
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
				ag.frames[len(ag.frames)-1].pc = pc
				x = fp.context.ToNumeric(fp.context.ToPrimitive(x, HintNumber))
				regs = ag.reload(regs, ag.frameBase(), fp)
				if x.o != numberObject {
					if c := fp.context.compareBigIntMixed(x, fp.consts[i.C]); c != cmpGreater && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n >= fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpLoopIf:
			if ToBoolean(regs[i.B]) {
				if ag.interrupt.Load() {
					ag.frames[len(ag.frames)-1].pc = pc
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopLt, bytecode.OpLoopLe, bytecode.OpLoopGt, bytecode.OpLoopGe:
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.loopHolds(i.Op, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			if r {
				if ag.interrupt.Load() {
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopLtK, bytecode.OpLoopLeK, bytecode.OpLoopGtK, bytecode.OpLoopGeK:
			ag.frames[len(ag.frames)-1].pc = pc
			r := fp.context.loopHoldsK(i.Op, regs[i.B], fp.consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			if r {
				if ag.interrupt.Load() {
					ag.Poll()
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopSEqP, bytecode.OpLoopSNeP, bytecode.OpLoopNullish, bytecode.OpLoopNotNullish:
			if ag.loopHoldsP(i.Op, regs[i.B], i.C) {
				if ag.interrupt.Load() {
					ag.frames[len(ag.frames)-1].pc = pc
					ag.Poll()
				}
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
			if regs[b].o == fp.context.realm.intrinsics[IntrinsicFunctionPrototypeCall] {
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
			if !regs[argsReg].IsUndefined() || regs[b].o != fp.context.realm.intrinsics[IntrinsicFunctionPrototypeApply] {
				// Once exposed, the arguments object's length and indexed properties can change.
				regs[b+2] = regs[i.C]
				regs[b+3] = fp.context.forwardedArguments(slices.Clip(regs), argsReg)
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, 2, i.A, false, nil
				if i.Op == bytecode.OpTailApplyFwd {
					argBase += ag.frameBase()
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
				if f := regs[b+1].o; !fp.heapRegs && f != nil && f.fn != nil && !ag.interrupt.Load() {
					if c := f.fn.code; c != nil && c.inlineFrames && c.context == fp.context {
						n := len(ag.frames)
						cur := &ag.frames[n-1]
						argc := cur.argc
						argBase := cur.base + fp.nregs + 1
						end := argBase + max(argc, c.nregs+1)
						// The frames have room for at most maxFrames.
						if (argc <= fp.nparams || cur.extraBase >= 0) && (argc <= c.nparams || !c.needsExtra) && end+stackHeadroom <= len(ag.stack) && n < cap(ag.frames) {
							k := min(argc, fp.nparams)
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
							if from, to := c.firstCleared(argc), c.nclear; from < to {
								clearFrameRegs(ag.stack[argBase:], from, to)
							}
							ag.frames = ag.frames[:n+1]
							ag.framesHigh = max(ag.framesHigh, n+1)
							fr := &ag.frames[n]
							fr.fp, fr.cl, fr.this, fr.newTarget, fr.gen, fr.scopes = c, f, this, nil, nil, nil
							// The callee belongs to the current realm, which is that of the running frame.
							fr.pc, fr.base, fr.ret, fr.isNew, fr.plainReturn, fr.argc, fr.extraBase = 0, argBase, i.A, false, true, argc, -1
							ag.sp = argBase + c.nregs + 1
							fp, pc = c, 0
							regs = ag.stack[argBase:ag.sp]
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
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := fp.context.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), -1); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpCallSpread, bytecode.OpNewSpread, bytecode.OpSuperSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].o, regs[b+1], i.A, false, nil
			args := regs[b+2].o.elems
			argc = len(args)
			argBase = ag.frames[len(ag.frames)-1].top()
			ag.ensureStack(argBase + argc)
			copy(ag.stack[argBase:], args)
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
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := fp.context.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), int(i.C)); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], b+2, int(i.C), i.A, false, nil
			if i.Op == bytecode.OpTailCallEval {
				argBase += ag.frameBase()
				goto tailCall
			}
			goto callRegs
		case bytecode.OpTailCallUndef:
			regs[i.B+1] = Undefined
			fallthrough
		case bytecode.OpTailCall:
			b := int(i.B)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[b].o, regs[b+1], ag.frameBase()+b+2, int(i.C), i.A, false, nil
			goto tailCall
		case bytecode.OpTailCallEvalSpread:
			// A direct eval, or the spread call of the case below for any other function.
			pc++
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := fp.context.callEval(slices.Clip(regs), i, int(fp.code[pc-1].A), -1); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
				continue
			}
			fallthrough
		case bytecode.OpTailCallSpread:
			b := int(i.B)
			f, callThis, ret, isNew, newTarget = regs[b].o, regs[b+1], i.A, false, nil
			args := regs[b+2].o.elems
			argc = len(args)
			argBase = ag.sp
			ag.ensureStack(argBase + argc)
			copy(ag.stack[argBase:], args)
			goto tailCall
		case bytecode.OpNew:
			b := int(i.B)
			site := fp.code[pc].A
			pc++
			f, argBase, argc, ret = regs[b].o, b+2, int(i.C), i.A
			if fp.heapRegs {
				top := ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(top + argc)
				copy(ag.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += ag.frameBase()
			}
			if f != nil && f.fn != nil && f.fn.code != nil && f.fn.constructor && !f.fn.derived {
				// The common case: a plain constructor function, whose prototype property is cached.
				c := &fp.caches[site]
				var proto Value
				if f.shape == c.shape && c.kind == cacheOwn {
					proto = f.slots[c.slot]
				} else {
					ag.frames[len(ag.frames)-1].pc = pc
					proto, _ = fp.context.getNamed(Value{o: f}, ag.Key(KeyPrototype), c)
				}
				if proto.o == nil || proto.o.kind < kindFirstObject {
					proto = Value{o: f.fn.code.context.realm.intrinsics[IntrinsicObjectPrototype]}
				}
				var shape *shape
				if proto.o == c.holder && c.next != nil && c.next.realm == f.fn.code.context.realm {
					shape = c.next
				} else {
					shape = fp.context.shapeForRealm(proto.o, f.fn.code.context.realm)
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
				top := ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(top + argc)
				copy(ag.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += ag.frameBase()
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
			n := len(ag.frames) - 1
			done := &ag.frames[n]
			if i.Op == bytecode.OpReturnDerived {
				if !r.IsObject() {
					if !r.IsUndefined() {
						fp.context.throwReturnError(stop, n, pc, ErrorKindType, "derived constructors may only return object or undefined")
					}
					r = regs[i.B]
					if r.o == tdzObject {
						fp.context.throwReturnError(stop, n, pc, ErrorKindReference, "must call super constructor in derived class before accessing 'this' or returning from derived constructor")
					}
				}
			} else if done.isNew && (r.o == nil || r.o.kind < kindFirstObject) {
				r = done.this
			}
			if g := done.gen; g != nil {
				g.complete()
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
			fp.context.activate()
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
				ag.sp = fr.base
			} else {
				regs = ag.stack[fr.base : fr.base+fp.nregs+1]
				ag.sp = fr.base + fp.nregs + 1
			}
			regs[ret] = r
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
			if i.B >= 0 && v.o == closeSignal {
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
			o := fp.context.newGeneratorObject(g)
			ret = cur.ret
			*cur = frame{}
			ag.frames = ag.frames[:n]
			if n == stop {
				return Value{o: o}, execReturn
			}
			fr := &ag.frames[n-1]
			fp = fr.fp
			fp.context.activate()
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
				ag.sp = fr.base
			} else {
				regs = ag.stack[fr.base : fr.base+fp.nregs+1]
				ag.sp = fr.base + fp.nregs + 1
			}
			regs[ret] = Value{o: o}
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
			if regs[i.A+1].o == closeSignal {
				g := ag.frames[len(ag.frames)-1].gen
				regs[i.A+1] = g.closeValue
				g.closeValue = Undefined
				regs[i.A] = Number(float64(i.B))
			} else {
				regs[i.A] = Number(1)
			}
		case bytecode.OpGenResumeMode:
			g := ag.frames[len(ag.frames)-1].gen
			regs[i.A] = Number(float64(g.mode))
			g.mode = ResumeNext
		case bytecode.OpYieldDelegateResult:
			ag.frames[len(ag.frames)-1].pc = pc
			r := regs[i.B]
			if !r.IsObject() {
				fp.context.ThrowTypeError("iterator result " + ag.DisplayString(r) + " is not an object")
			}
			done := ToBoolean(fp.context.GetV(r, ag.Key(keyDone)))
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !done {
				pc = int(i.C)
				continue
			}
			v := fp.context.GetV(r, ag.Key(KeyValue))
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v

		case bytecode.OpNewObject:
			o, slots := newObjectWithStorage(int(i.C))
			o.kind = kindObject
			o.shape = fp.context.realm.shapes.object
			o.slots = slots[:0]
			regs[i.A] = Value{o: o}
		case bytecode.OpNewArray:
			o, elems := newObjectWithStorage(int(i.C))
			o.kind = kindArray
			o.class = classArray
			o.shape = fp.context.realm.shapes.array
			o.elems = elems[:0]
			regs[i.A] = Value{o: o}
		case bytecode.OpNewArrayK:
			regs[i.A] = Value{o: fp.context.NewArrayCopy(fp.consts[i.B : i.B+i.C])}
		case bytecode.OpArrayPush:
			o := regs[i.A].o
			o.appendElem(regs[i.B])
			o.length = uint32(len(o.elems))
		case bytecode.OpArrayHole:
			o := regs[i.A].o
			o.appendElem(hole)
			o.length = uint32(len(o.elems))
		case bytecode.OpArraySpread:
			ag.frames[len(ag.frames)-1].pc = pc
			o := regs[i.A].o
			fp.context.appendIterable(o, regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpSpreadArgs:
			ag.frames[len(ag.frames)-1].pc = pc
			o := fp.context.spreadArgs(regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Value{o: o}
		case bytecode.OpRegExp:
			ag.frames[len(ag.frames)-1].pc = pc
			regs[i.A] = Value{o: fp.context.newRegExpLiteral(&fp.regExps[i.B])}
		case bytecode.OpRegExpArg:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.lendRegExpLiteral(slices.Clip(regs), i.A, i.C, &fp.regExps[i.B])
		case bytecode.OpRegExpMethod:
			ag.frames[len(ag.frames)-1].pc = pc
			m, this := fp.context.regExpLiteralMethod(&fp.regExps[i.B], fp.keys[i.C], &fp.caches[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A], regs[i.A+1] = m, Value{o: this}
		case bytecode.OpTemplate:
			regs[i.A] = Value{o: fp.context.templateObject(&fp.templates[i.B])}
		case bytecode.OpDefineField:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.defineField(regs[i.A].o, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDefineFieldK:
			o := regs[i.A].o
			c := &fp.caches[i.B]
			if o.shape == c.shape && c.kind == cacheAdd {
				o.shape = c.next
				o.appendSlot(regs[i.C])
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.defineFieldK(o, fp.keys[i.B], regs[i.C], c)
		case bytecode.OpDefineMethod:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.defineMethod(regs[i.A].o, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDefineGetter, bytecode.OpDefineSetter:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			enumerable := fp.code[pc].A != 0
			pc++
			fp.context.defineAccessor(regs[i.A].o, regs[i.B], regs[i.C].o, i.Op == bytecode.OpDefineSetter, enumerable)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpSetFuncName:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.setFuncName(regs[i.A], regs[i.B], int(i.C))
		case bytecode.OpSetHome:
			regs[i.A].o.ownFunction().home = regs[i.B].o
		case bytecode.OpCopyProps:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.copyDataProperties(regs[i.A].o, regs[i.B], nil)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpCopyPropsExcept:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			n := int(fp.code[pc].A)
			pc++
			o := fp.context.NewOrdinaryObject()
			excluded := append([]Value(nil), slices.Clip(regs)[i.C:int(i.C)+n]...)
			fp.context.copyDataProperties(o, regs[i.B], excluded)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Value{o: o}
		case bytecode.OpSetProto:
			if p := regs[i.B]; p.IsObject() {
				fp.context.setProto(regs[i.A].o, p.o)
			} else if p.IsNull() {
				fp.context.setProto(regs[i.A].o, nil)
			}
		case bytecode.OpClass:
			ag.frames[len(ag.frames)-1].pc = pc
			parent := hole
			if i.C >= 0 {
				parent = regs[i.C]
			}
			proto := fp.context.setupClass(regs[i.B].o, parent)
			regs = ag.reload(regs, ag.frameBase(), fp)
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
				ag.frames[len(ag.frames)-1].pc = pc
				fp.context.initFields(regs[i.A].o, regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
			}
		case bytecode.OpAddField:
			ag.frames[len(ag.frames)-1].pc = pc + 1
			kind := int(fp.code[pc].A)
			pc++
			fp.context.addClassElement(regs[i.A].o, regs[i.B], regs[i.C], kind)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpCheckThisUninit:
			if regs[i.A].o != tdzObject {
				ag.frames[len(ag.frames)-1].pc = pc
				fp.context.ThrowReferenceError("super constructor may only be called once")
			}
		case bytecode.OpRequireCoercible:
			ag.frames[len(ag.frames)-1].pc = pc
			if regs[i.A].IsUndefined() {
				fp.context.ThrowTypeError("cannot destructure 'undefined' as it is undefined")
			}
			fp.context.ThrowTypeError("cannot destructure 'null' as it is null")

		case bytecode.OpForIn:
			ag.frames[len(ag.frames)-1].pc = pc
			it := fp.context.newForIn(regs[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = it
		case bytecode.OpForInNext:
			ag.frames[len(ag.frames)-1].pc = pc
			k, ok := fp.context.forInNext(regs[i.B].o)
			regs = ag.reload(regs, ag.frameBase(), fp)
			if !ok {
				pc = int(i.C)
				continue
			}
			regs[i.A] = k
		case bytecode.OpGetIterator:
			v := regs[i.B]
			if i.C == bytecode.IterSync && v.o != nil && v.o.kind == kindArray && fp.context.arrayIterationPlain(v.o) {
				// Iterate a plain array by index: the next method slot holds the index.
				regs[i.A] = v
				regs[i.A+1] = Number(0)
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			it, next := fp.context.getIterator(v, i.C == bytecode.IterAsync)
			regs = ag.reload(regs, ag.frameBase(), fp)
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
				regs[i.B+1].n = idx.n + 1
				if uint(j) < uint(len(o.elems)) && o.elems[j].o != holeObject {
					regs[i.A] = o.elems[j]
					continue
				}
				ag.frames[len(ag.frames)-1].pc = pc
				// A getter that throws finishes the iterator.
				regs[i.B] = Undefined
				v := fp.context.plainArrayIteratorIndex(o, j)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.B] = Value{o: o}
				regs[i.A] = v
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			{
				// An iterator whose step throws counts as finished, so that it is not closed.
				it := regs[i.B]
				regs[i.B] = Undefined
				v, st := fp.context.iterNext(it, regs[i.B+1])
				regs = ag.reload(regs, ag.frameBase(), fp)
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
		case bytecode.OpIterNextPair:
			// The elements of the next value go to their registers without the value when nothing
			// can tell. When the step has run and something can tell, the pair becomes the value of
			// the following OpIterNext, which is skipped. Otherwise the instructions after this one
			// step the iterator, which is already cleared when the step found it finished.
			b := fp.code[pc].A
			pc++
			it, next := regs[i.B], regs[i.B+1]
			if it.o == nil {
				continue
			}
			if next.o == numberObject {
				// A plain array iterated by index, whose element is itself a plain array of at
				// least two elements.
				o, j := it.o, uint32(next.n)
				if j >= o.length || uint(j) >= uint(len(o.elems)) {
					continue
				}
				e := o.elems[j].o
				if e == nil || e.shape != fp.context.realm.shapes.array || e.length < 2 || len(e.elems) < 2 || e.elems[0].isHole() || e.elems[1].isHole() || !ag.pairDestructuringPlain(fp.context.realm) {
					continue
				}
				regs[i.B+1].n = next.n + 1
				regs[i.A] = e.elems[0]
				regs[b] = e.elems[1]
				pc = int(i.C)
				continue
			}
			if next.o == nil || next.o.fn == nil || ag.interrupt.Load() {
				continue
			}
			var x, y Value
			var st PairStep
			pairRealm := fp.context.realm
			if step := next.o.fn.iteratorPairStep(fp.context.realm); step != nil {
				x, y, st = step(IteratorState(it))
				if st == PairYielded {
					pairRealm = it.o.shape.realm
				}
			} else if next.o == fp.context.realm.intrinsics[IntrinsicArrayIteratorPrototypeNext] {
				// Reading an array entry can run JavaScript. A throwing step finishes the iterator.
				ag.frames[len(ag.frames)-1].pc = pc
				regs[i.B] = Undefined
				x, y, st = fp.context.arrayIteratorPairStep(it, next)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.B] = it
			} else {
				continue
			}
			switch st {
			case PairYielded:
				if !ag.pairDestructuringPlain(pairRealm) {
					// The outer iterator has stepped. Supply its pair to the ordinary binding.
					regs[fp.code[pc].A] = Value{o: pairRealm.context.NewArrayPair(x, y)}
					pc++
					continue
				}
				regs[i.A] = x
				regs[b] = y
				pc = int(i.C)
			case PairDone:
				regs[i.B] = Undefined
			}
			// A finished iterator is left for the step after this instruction.
		case bytecode.OpIterClose:
			it := regs[i.A]
			if it.IsUndefined() {
				continue
			}
			regs[i.A] = Undefined
			ag.frames[len(ag.frames)-1].pc = pc
			pending := i.B >= 0 && regs[i.B].o != closeSignal
			if index := regs[i.A+1]; index.o == numberObject {
				fp.context.closePlainArrayIterator(it.o, uint32(index.n), pending)
			} else {
				fp.context.iteratorClose(it, pending)
			}
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpIterCall:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				it := regs[i.B]
				m := fp.context.GetMethod(it, fp.keys[i.C])
				regs = ag.reload(regs, ag.frameBase(), fp)
				if m == nil {
					regs[i.A] = Undefined
					regs[i.A+1] = False
					continue
				}
				r := fp.context.Call(m, it, regs[i.A])
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = True
			}
		case bytecode.OpIterReturn:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				it := regs[i.B]
				regs[i.B] = Undefined
				r, ok := fp.context.iterReturn(it)
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = r
				regs[i.A+1] = Bool(ok)
			}
		case bytecode.OpIterCheckResult:
			if !regs[i.A].IsObject() {
				ag.frames[len(ag.frames)-1].pc = pc
				fp.context.throwNotIterResult(regs[i.A])
			}
		case bytecode.OpIterResultValue:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				v, done := fp.context.iterResultValue(regs[i.B])
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
				dc := fp.context.addDisposableOp(regs[i.B], regs[i.A], DisposeHint(i.C))
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = dc
			}
		case bytecode.OpDisposeBegin:
			beginDisposeOp(regs[i.A], regs[i.B], regs[i.C])
		case bytecode.OpDisposeStep:
			ag.frames[len(ag.frames)-1].pc = pc
			{
				v, await := fp.context.disposeStepOp(regs[i.B])
				regs = ag.reload(regs, ag.frameBase(), fp)
				if !await {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}
		case bytecode.OpDisposeError:
			fp.context.disposeError(regs[i.A].o.ext.(*DisposeCapability), regs[i.B])
		case bytecode.OpDisposeEnd:
			if v, thrown := endDisposeOp(regs[i.A]); thrown {
				regs[i.A] = Undefined
				ag.restoreException(v)
				goto throw
			}
			regs[i.A] = Undefined

		case bytecode.OpWith:
			ag.frames[len(ag.frames)-1].pc = pc
			o := fp.context.ToObject(regs[i.A])
			cur := &ag.frames[len(ag.frames)-1]
			cur.scopes = append(cur.scopes, o)
		case bytecode.OpPopScope:
			cur := &ag.frames[len(ag.frames)-1]
			cur.scopes = cur.scopes[:len(cur.scopes)-1]
		case bytecode.OpResolveName:
			ag.frames[len(ag.frames)-1].pc = pc
			o := fp.context.findScope(ag.frames[len(ag.frames)-1].cl, fp.keys[i.B], int(i.C))
			regs = ag.reload(regs, ag.frameBase(), fp)
			if o != nil {
				regs[i.A] = Value{o: o}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpGetScoped, bytecode.OpGetScopedStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.getScoped(regs[i.B].o, fp.keys[i.C], i.Op == bytecode.OpGetScopedStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpSetScoped, bytecode.OpSetScopedStrict:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.setScoped(regs[i.B].o, fp.keys[i.C], regs[i.A], i.Op == bytecode.OpSetScopedStrict)
			regs = ag.reload(regs, ag.frameBase(), fp)
		case bytecode.OpDeleteScoped:
			ag.frames[len(ag.frames)-1].pc = pc
			ok := fp.context.DeleteProp(regs[i.B].o, PropKey{key: fp.keys[i.C]})
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(ok)
		case bytecode.OpDeleteName:
			ag.frames[len(ag.frames)-1].pc = pc
			ok := fp.context.deleteName(fp.keys[i.B])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = Bool(ok)
		case bytecode.OpCaptureScopes:
			regs[i.A].o.ownFunction().scopes = ag.openScopes(ag.frames[len(ag.frames)-1].cl)
		case bytecode.OpEvalScope:
			fp.context.pushEvalScope()
		case bytecode.OpScopeThis:
			regs[i.A] = scopeThis(regs[i.B])
		case bytecode.OpDeclareEvalVar:
			ag.frames[len(ag.frames)-1].pc = pc
			fp.context.declareEvalVar(ag.frames[len(ag.frames)-1].cl, fp.keys[i.A], int(i.B), slices.Clip(regs), int(i.C))
		case bytecode.OpImport, bytecode.OpImportMeta:
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.importOp(slices.Clip(regs), i)
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
		case bytecode.OpAutoAccessor:
			fp.context.autoAccessorOp(slices.Clip(regs), i)
		case bytecode.OpDecorateBegin, bytecode.OpDecorateAdd, bytecode.OpDecorateClass, bytecode.OpDecorateElement, bytecode.OpDecorateApply, bytecode.OpDecorateFinish:
			if i.Op == bytecode.OpDecorateElement {
				// Past the OpExtra, which holds the Elem kind.
				pc++
			}
			ag.frames[len(ag.frames)-1].pc = pc
			if v, ok := fp.context.decorateOp(slices.Clip(regs), i, &fp.code[pc-1]); ok {
				regs = ag.reload(regs, ag.frameBase(), fp)
				regs[i.A] = v
			} else {
				regs = ag.reload(regs, ag.frameBase(), fp)
			}

		default:
			panic("ekuma: unknown bytecode.Opcode")
		}
		continue

	arith:
		{
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.arith(i.Op, regs[i.B], regs[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	arithK:
		{
			ag.frames[len(ag.frames)-1].pc = pc
			v := fp.context.arith(i.Op.RegOperand(), regs[i.B], fp.consts[i.C])
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[i.A] = v
			continue
		}

	readProperty:
		{
			fr := &ag.frames[len(ag.frames)-1]
			base, dst := fr.base, i.A
			fr.pc = pc
			var val Value
			var getter *Object
			switch i.Op {
			case bytecode.OpGetProp, bytecode.OpGetPropChain, bytecode.OpGetMethod, bytecode.OpGetLength:
				val, getter = fp.context.getNamedAtSite(receiver, name, site)
			case bytecode.OpGetArgsLength:
				val, getter = fp.context.getNamed(receiver, name, nil)
			case bytecode.OpGetIndex, bytecode.OpGetArgsIndex:
				val, getter = fp.context.getValue(receiver, key)
			case bytecode.OpGetIndexInt:
				val, getter = fp.context.getIndex(receiver, uint32(i.C))
			case bytecode.OpCopyPropToGlobal:
				// The following global store owns the cache and fills it only for writable bindings.
				val = fp.context.resolveGlobal(fp.keys[fp.code[i.C-1].B], nil)
			case bytecode.OpResolveGlobal:
				val = fp.context.resolveGlobal(fp.keys[i.B], &fp.globals[i.C])
			case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
				orUndef := i.Op == bytecode.OpGetGlobalOrUndef || i.Op == bytecode.OpGetGlobalOrUndefStrict
				strict := i.Op == bytecode.OpGetGlobalStrict || i.Op == bytecode.OpGetGlobalOrUndefStrict
				val = fp.context.getGlobal(fp.keys[i.B], &fp.globals[i.C], orUndef, strict)
			default:
				panic("ekuma: unexpected property read opcode: " + i.Op.String())
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
					setter, ok = fp.context.setNamed(receiver, name, rhs, site)
					if !ok && i.Op == bytecode.OpSetPropStrict {
						fp.context.throwReadOnly(receiver, PropKey{key: name})
					}
				}
			case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
				fr.pc = pc
				fp.context.setGlobal(fp.keys[i.B], rhs, &fp.globals[i.C], i.Op == bytecode.OpSetGlobalStrict)
			case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
				fr.pc = pc
				setter = fp.context.setValue(receiver, key, rhs, i.Op == bytecode.OpSetIndexStrict || i.Op == bytecode.OpSetIndexIntStrict)
			default:
				panic("ekuma: unexpected property write opcode: " + i.Op.String())
			}
			if setter != nil {
				argBase = ag.frames[len(ag.frames)-1].top()
				ag.ensureStack(argBase + 1)
				ag.stack[argBase] = rhs
			}
			// Both the lookup and making room for a setter argument can move the stack.
			regs = ag.reload(regs, base, fp)
			if setter != nil {
				f, callThis, argc, ret, isNew, newTarget = setter, receiver, 1, int32(fp.nregs), false, nil
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
		if (i.Op == bytecode.OpCallFwd || i.Op == bytecode.OpTailCallFwd) && (!regs[i.C].IsUndefined() || !ag.arrayIterationUnmodified(fp.context.realm)) {
			ag.frames[len(ag.frames)-1].pc = pc
			args := fp.context.spreadArgs(fp.context.forwardedArguments(slices.Clip(regs), i.C)).elems
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
			n := min(argc, fp.nparams)
			copy(ag.stack[argBase:], slices.Clip(regs)[:n])
			copy(ag.stack[argBase+n:], ag.extraArgs(&ag.frames[len(ag.frames)-1]))
		}
		if i.Op == bytecode.OpTailApplyFwd || i.Op == bytecode.OpTailCallFwd {
			goto tailCall
		}
		goto call

	callRegs:
		// The arguments are in the registers from argBase on.
		if fp.heapRegs {
			top := ag.frames[len(ag.frames)-1].top()
			ag.ensureStack(top + argc)
			copy(ag.stack[top:], slices.Clip(regs)[argBase:argBase+argc])
			argBase = top
		} else {
			argBase += ag.frameBase()
		}

	call:
		// The arguments are on the stack from argBase on.
		{
			if ag.interrupt.Load() {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.Poll()
			}
			if f == nil || f.fn == nil {
				ag.frames[len(ag.frames)-1].pc = pc
				ag.throwNotCallable(fp, pc, slices.Clip(regs))
			}
			fn := f.fn
			if c := fn.code; c != nil && !c.isGenerator && !c.isAsync && (!fn.classConstructor || isNew || newTarget != nil) {
				if len(ag.frames) >= maxFrames {
					ag.frames[len(ag.frames)-1].pc = pc
					fp.context.ThrowRangeError("maximum call stack size exceeded")
				}
				if site := c.returnedProperty; site != nil && !isNew && newTarget == nil && c.context == fp.context && callThis.IsObject() {
					if e := site.entry(callThis.o.shape); e != nil && e.kind == cacheOwn {
						// Preparing the arguments may have grown the stack.
						regs = ag.reload(regs, ag.frameBase(), fp)
						regs[ret] = callThis.o.slots[e.slot]
						continue
					}
				}
				ag.frames[len(ag.frames)-1].pc = pc
				base := ag.pushFrame(f, callThis, argBase, argc, ret, isNew, newTarget)
				c.context.activate()
				fp = c
				regs = ag.stack[base : base+c.nregs+1]
				pc = 0
				if r := c.thisReg; r >= 0 {
					// Do what the first instruction does, and start after it.
					if t := ag.frames[len(ag.frames)-1].this; !c.thisSloppy || t.o != nil && t.o.kind >= kindFirstObject {
						regs[r] = t
						pc = 1
					}
				}
				continue
			}
			ag.frames[len(ag.frames)-1].pc = pc
			if fn.native != nil {
				info := fn.info
				// A native function with a kernel, which needs none of the bookkeeping of a call.
				// The registers are reloaded, since a path to here may have grown the stack.
				if info != nil && info.fast != nil && !isNew && newTarget == nil {
					k, a := info.fast, ag.stack[argBase:argBase+argc:argBase+argc]
					if argc == 1 && k.number1 != nil && a[0].o == numberObject {
						x := k.number1(a[0].n)
						regs = ag.reload(regs, ag.frameBase(), fp)
						setNumber(&regs[ret], x)
						continue
					}
					if argc == 2 && k.number2 != nil && a[0].o == numberObject && a[1].o == numberObject {
						x := k.number2(a[0].n, a[1].n)
						regs = ag.reload(regs, ag.frameBase(), fp)
						setNumber(&regs[ret], x)
						continue
					}
					// Cross-realm native calls install the callee's context before a host hook can reenter.
					if info.realm == fp.context.realm {
						if v, ok := k.callMethod(fp.context, callThis, a); ok {
							regs = ag.reload(regs, ag.frameBase(), fp)
							regs[ret] = v
							continue
						}
					}
				}
				// A native function of the current realm is called directly.
				if info == nil || info.realm == fp.context.realm {
					ag.ensureStack(argBase + argc)
					saved := ag.sp
					ag.sp = argBase + argc
					r := fp.context.invokeNative(f, callThis, ag.stack[argBase:argBase+argc:argBase+argc], nil)
					ag.sp = saved
					regs = ag.reload(regs, ag.frameBase(), fp)
					regs[ret] = r
					continue
				}
			}
			if fn.bound != nil && !isNew && newTarget == nil && len(fn.bound.args) == 0 {
				f, callThis = ag.unbindPlain(f)
				goto call
			}
			if c := fn.code; c != nil && c.isGenerator && !c.isAsync && fn.bound == nil {
				// Run the generator up to its initial suspension as an ordinary frame.
				ag.pushGenerator(f, callThis, ag.stack[argBase:argBase+argc], ret)
				fr := &ag.frames[len(ag.frames)-1]
				fp = fr.fp
				fp.context.activate()
				regs = fr.gen.regs
				pc = 0
				continue
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := fp.context.callSlow(f, callThis, ag.stack[argBase:argBase+argc:argBase+argc], isNew, newTarget)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = r
			continue
		}

	tailCall:
		// A call in tail position, whose arguments are on the stack from argBase on.
		ag.frames[len(ag.frames)-1].pc = pc
		if f, callThis, argBase, argc = fp.context.unbind(f, callThis, argBase, argc); !fp.context.replaceFrame(f, callThis, argBase, argc) {
			goto call
		}
		{
			fr := &ag.frames[len(ag.frames)-1]
			fp = fr.fp
			fp.context.activate()
			regs = ag.stack[fr.base : fr.base+fp.nregs+1]
			pc = 0
			continue
		}

	construct:
		// new f(...) for anything but a plain constructor function.
		{
			ag.frames[len(ag.frames)-1].pc = pc
			if f == nil || f.fn == nil || !fp.context.IsConstructor(f) {
				ag.throwNotConstructor(fp, pc, slices.Clip(regs))
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := fp.context.Construct(f, ag.stack[argBase:argBase+argc:argBase+argc], f)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = Value{o: r}
			continue
		}

	superCall:
		// super(...): f is the super constructor and newTarget the new.target of the running one.
		{
			ag.frames[len(ag.frames)-1].pc = pc
			parent := f
			if parent == nil || parent.fn == nil || !fp.context.IsConstructor(parent) {
				fp.context.ThrowTypeError("super constructor is not a constructor")
			}
			ag.ensureStack(argBase + argc)
			saved := ag.sp
			ag.sp = argBase + argc
			r := fp.context.Construct(parent, ag.stack[argBase:argBase+argc:argBase+argc], newTarget)
			ag.sp = saved
			regs = ag.reload(regs, ag.frameBase(), fp)
			regs[ret] = Value{o: r}
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
			fp.context.activate()
			pc = fr.pc
			if fp.heapRegs {
				regs = fr.gen.regs
			} else {
				regs = ag.stack[fr.base : fr.base+fp.nregs+1]
			}
		}
	}
}

// unbindPlain returns the target and the this value that a call of the bound function f
// passes its arguments to, following bound functions without bound arguments.
func (ag *agent) unbindPlain(f *Object) (*Object, Value) {
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
func (cx *ExecutionContext) unbind(f *Object, this Value, argBase, argc int) (*Object, Value, int, int) {
	for f != nil && f.fn != nil {
		if b := f.fn.bound; b != nil {
			if k := len(b.args); k > 0 {
				cx.ensureStack(argBase + argc + k)
				copy(cx.stack[argBase+k:], cx.stack[argBase:argBase+argc])
				copy(cx.stack[argBase:], b.args)
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
		case cx.Intrinsic(IntrinsicFunctionPrototypeCall):
			f, this = this.o, Undefined
			if argc > 0 {
				this = cx.stack[argBase]
				argBase++
				argc--
			}
			continue
		case cx.Intrinsic(IntrinsicFunctionPrototypeApply):
			var list []Value
			if argc > 1 {
				if a := cx.stack[argBase+1]; !a.IsNullish() {
					var ok bool
					if list, ok = a.o.plainElems(); !ok {
						return f, this, argBase, argc
					}
				}
			}
			f, this = this.o, Undefined
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
func (cx *ExecutionContext) replaceFrame(f *Object, this Value, argBase, argc int) bool {
	n := len(cx.frames) - 1
	cur := &cx.frames[n]
	if f == nil || f.fn == nil || cur.isNew || cur.newTarget != nil {
		return false
	}
	if c := f.fn.code; c == nil || c.isGenerator || c.isAsync || f.fn.classConstructor {
		return false
	}
	if cx.interrupt.Load() {
		cx.Poll()
	}
	// The arguments go where the running frame's were.
	start := cur.base
	if cur.extraBase >= 0 {
		start = cur.extraBase - cur.fp.nparams
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
func (ag *agent) reload(regs []Value, base int, fp *funcProto) []Value {
	if fp.heapRegs {
		return regs
	}
	return ag.stack[base : base+len(regs)]
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

// loopHolds reports whether the comparison of the loop jump op, one of [bytecode.OpLoopLt]
// through [bytecode.OpLoopGe], holds for x and y.
func (cx *ExecutionContext) loopHolds(op bytecode.Opcode, x, y Value) bool {
	switch op {
	case bytecode.OpLoopLt:
		return cx.compare(x, y, true) == cmpLess
	case bytecode.OpLoopLe:
		c := cx.compare(y, x, false)
		return c == cmpGreater || c == cmpEqual
	case bytecode.OpLoopGt:
		return cx.compare(y, x, false) == cmpLess
	default:
		c := cx.compare(x, y, true)
		return c == cmpGreater || c == cmpEqual
	}
}

// loopHoldsK reports whether the comparison of the loop jump op, one of [bytecode.OpLoopLtK]
// through [bytecode.OpLoopGeK], holds for x and the number k.
func (cx *ExecutionContext) loopHoldsK(op bytecode.Opcode, x, k Value) bool {
	if x.o != numberObject {
		x = cx.ToNumeric(cx.ToPrimitive(x, HintNumber))
		if x.o != numberObject {
			c := cx.compareBigIntMixed(x, k)
			switch op {
			case bytecode.OpLoopLtK:
				return c == cmpLess
			case bytecode.OpLoopLeK:
				return c == cmpLess || c == cmpEqual
			case bytecode.OpLoopGtK:
				return c == cmpGreater
			default:
				return c == cmpGreater || c == cmpEqual
			}
		}
	}
	switch op {
	case bytecode.OpLoopLtK:
		return x.n < k.n
	case bytecode.OpLoopLeK:
		return x.n <= k.n
	case bytecode.OpLoopGtK:
		return x.n > k.n
	default:
		return x.n >= k.n
	}
}

// loopHoldsP reports whether the comparison of the loop jump op, one of [bytecode.OpLoopSEqP]
// through [bytecode.OpLoopNotNullish], holds for x and the operand c.
func (ag *agent) loopHoldsP(op bytecode.Opcode, x Value, c int32) bool {
	switch op {
	case bytecode.OpLoopSEqP:
		return x.o == primObjects[c]
	case bytecode.OpLoopSNeP:
		return x.o != primObjects[c]
	case bytecode.OpLoopNullish:
		return x.IsNullish() || c != 0 && x.isHTMLDDA()
	default:
		return !x.IsNullish() && (c == 0 || !x.isHTMLDDA())
	}
}
