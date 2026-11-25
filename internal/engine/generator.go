// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

type genState uint8

const (
	genSuspendedStart genState = iota
	genSuspendedYield
	genRunning
	genCompleted
)

// ResumeMode is the way a suspended generator is continued.
type ResumeMode uint8

const (
	ResumeNext   ResumeMode = bytecode.ResumeNext
	ResumeThrow  ResumeMode = bytecode.ResumeThrow
	ResumeReturn ResumeMode = bytecode.ResumeReturn
)

// closeSignal is thrown through a suspended generator to make it return:
// only finally handlers intercept it. A JavaScript finally block saves the return value in its frame.
var closeSignal = storage.NewMarker(nil)

// generator is the suspended activation of a generator or async function.
// Its registers are not on the stack, so that closures can keep referring to them.
type generator struct {
	// object is the generator object, or the promise of an async function.
	object *Object

	f     *Object
	fp    *funcProto // The linked code carries the body's bound context across suspensions.
	this  Value
	argc  int
	extra []Value

	state genState
	pc    int
	regs  []Value

	// scopes are the with scopes that are open at the suspension.
	scopes []*Object

	// resumeReg receives the value passed to the resumption.
	resumeReg int32

	// mode is how the generator was resumed, for the code after a yield to look at.
	mode ResumeMode

	// awaiting is set when the suspension is an await and not a yield.
	awaiting bool

	// delegating is set when the code after the suspension handles every way of resuming itself.
	delegating bool

	closeValue Value

	// openUpvals are the upvalues that point into regs, ordered by register index.
	openUpvals []*upvalue

	// async is the state of an async function or async generator.
	async *asyncState
}

// abortOnPanic closes an activation abandoned by an interruption, internal failure or host panic.
func (g *generator) abortOnPanic() {
	if r := recover(); r != nil {
		if r != any(errThrow) {
			g.abort()
		}
		panic(r)
	}
}

// abort closes an abandoned activation without running JavaScript.
func (g *generator) abort() {
	g.complete()
	if g.async != nil && g.async != asyncFunction {
		g.async.queue = nil
		g.async.draining = false
	}
}

// complete releases the activation and returns its pending return value.
func (g *generator) complete() Value {
	if len(g.openUpvals) > 0 {
		g.closeUpvals(0)
	}
	v := g.closeValue
	*g = generator{
		state: genCompleted,
		async: g.async,
	}
	return v
}

// completeAt is complete for a return, and keeps the position after it.
func (g *generator) completeAt(pc int) {
	g.complete()
	g.pc = pc
}

func (g *generator) findUpval(index int) *upvalue {
	i := len(g.openUpvals)
	if i > 0 && g.openUpvals[i-1].Index > index {
		i = sort.Search(i-1, func(j int) bool {
			return g.openUpvals[j].Index > index
		})
	}
	if i > 0 && g.openUpvals[i-1].Index == index {
		return g.openUpvals[i-1]
	}
	u := &upvalue{
		P:     &g.regs[index],
		Index: index,
	}
	g.openUpvals = slices.Insert(g.openUpvals, i, u)
	return u
}

// closeUpvals detaches the upvalues that refer to the registers from index from on.
func (g *generator) closeUpvals(from int) {
	n := len(g.openUpvals)
	for n > 0 && g.openUpvals[n-1].Index >= from {
		n--
		u := g.openUpvals[n]
		u.Val = *u.P
		u.P = &u.Val
	}
	clear(g.openUpvals[n:])
	g.openUpvals = g.openUpvals[:n]
}

// newGenerator creates the activation of the generator or async function f without an object.
func newGenerator(f *Object, this Value, args []Value) *generator {
	fp := f.Fn.Code
	fp.Prepare()
	g := &generator{
		f:    f,
		fp:   fp,
		this: this,
		argc: len(args),
		regs: make([]Value, fp.Nregs+1),
	}
	copy(g.regs[:fp.Nparams], args)
	if len(args) > fp.Nparams && fp.NeedsExtra {
		g.extra = append(g.extra, args[fp.Nparams:]...)
	}
	return g
}

// pushGenerator creates the activation of a generator or async generator for a call of f and
// pushes its frame, so that the caller can run it up to its initial suspension, which creates
// the generator object.
func (ag *agent) pushGenerator(f *Object, this Value, args []Value, ret int32) {
	g := newGenerator(f, this, args)
	if g.fp.IsAsync {
		g.async = &asyncState{}
	}
	ag.pushGenFrame(g, ret)
}

// newGeneratorObject creates the generator object of g, whose parameters are instantiated.
func (cx *ExecutionContext) newGeneratorObject(g *generator) *Object {
	r := g.fp.Realm
	proto := r.Intrinsics[IntrinsicGeneratorPrototype]
	if g.async != nil {
		proto = r.Intrinsics[IntrinsicAsyncGeneratorPrototype]
	}
	if p := cx.GetV(Value{O: g.f}, cx.Key(KeyPrototype)); p.IsObject() {
		proto = p.O
	}
	o := storage.NewObjectWithState(cx.ShapeForRealm(proto, r), classGenerator, g)
	g.object = o
	return o
}

// pushGenFrame pushes the frame that continues g at g.pc.
func (ag *agent) pushGenFrame(g *generator, ret int32) {
	if validateExec {
		// The body can move the stack before its frame ends.
		ag.moveStack()
	}
	base := ag.sp
	fr := ag.appendFrame()
	fr.fp = g.fp
	fr.cl = g.f
	fr.pc = g.pc
	fr.base = base
	fr.this = g.this
	fr.ret = ret
	fr.isNew = false
	fr.plainReturn = false
	fr.argc = g.argc
	fr.extraBase = -1
	fr.newTarget = nil
	fr.gen = g
	fr.scopes = g.scopes
	g.state = genRunning
}

// callCoroutine calls the generator, async function or async generator f from Go.
func (cx *ExecutionContext) callCoroutine(f *Object, this Value, args []Value) Value {
	c := f.Fn.Code
	if c.IsAsync && !c.IsGenerator {
		return cx.callAsync(f, this, args)
	}
	sp := cx.sp
	cx.pushGenerator(f, this, args, 0)
	r := cx.run(len(cx.frames)-1, c.Realm)
	cx.sp = sp
	return r
}

// resume continues the suspended generator g. It returns the yielded or returned value
// and whether g completed. An exception that leaves g is thrown.
func (resumer *ExecutionContext) resume(g *generator, mode ResumeMode, v Value) (Value, bool) {
	switch g.state {
	case genRunning:
		resumer.ThrowTypeError("generator is already running")
	case genCompleted:
		switch mode {
		case ResumeThrow:
			resumer.Throw(v)
		case ResumeReturn:
			return v, true
		}
		return storage.Undefined, true
	case genSuspendedStart:
		switch mode {
		case ResumeThrow:
			g.complete()
			resumer.Throw(v)
		case ResumeReturn:
			g.complete()
			return v, true
		}
	}
	// The limits are checked while g can still be resumed later.
	if len(resumer.frames) >= maxFrames || resumer.depth >= maxDepth {
		resumer.ThrowRangeError("maximum call stack size exceeded")
	}
	sp := resumer.sp
	started := g.state == genSuspendedYield
	g.mode = mode
	resumer.pushGenFrame(g, 0)
	stop := len(resumer.frames) - 1
	if started {
		g.regs[g.resumeReg] = v
		resumer.thrown = storage.Undefined
	}
	if started && mode != ResumeNext && !g.delegating {
		// The code after an await or a plain yield does not look at the mode: deliver
		// a throw or a return as an exception at the suspension.
		switch mode {
		case ResumeThrow:
			if resumer.captureThrowSites {
				resumer.thrownAt = throwSite{
					code: g.fp.Func,
					pc:   g.pc - 1,
				}
			}
			resumer.thrown = v
		case ResumeReturn:
			g.closeValue = v
			resumer.thrown = Value{O: closeSignal}
		}
		g.mode = ResumeNext
		r, status := resumer.unwind(stop)
		if status != execCatch {
			resumer.sp = sp
			if status == execReturn {
				return r, true
			}
			panic(errThrow)
		}
	}
	r, ok := resumer.runFrame(stop, g.fp.Realm)
	resumer.sp = sp
	if !ok {
		resumer.dropNatives(stop)
		panic(errThrow)
	}
	return r, g.state == genCompleted
}

// isSyncGenerator reports whether v is a generator object that is not an async generator.
func isSyncGenerator(v Value) bool {
	if !v.IsObject() {
		return false
	}
	g, _ := v.O.ExtState().(*generator)
	return g != nil && g.async == nil
}

// ResumeGenerator continues the generator gen with the value v and returns the iterator result.
// It reports false, without running anything, when gen is not a generator.
func (cx *ExecutionContext) ResumeGenerator(gen Value, mode ResumeMode, v Value) (Value, bool) {
	var g *generator
	if gen.IsObject() {
		g, _ = gen.O.ExtState().(*generator)
	}
	if g == nil || g.async != nil {
		return storage.Undefined, false
	}
	r, done := cx.resume(g, mode, v)
	if !done {
		if g.delegating {
			// yield* passes the result object of the inner iterator through.
			return r, true
		}
		// Yield creates its result before returning to the resuming method's realm.
		return cx.contextOf(g.fp.Realm).CreateIterResultObject(r, false), true
	}
	return cx.CreateIterResultObject(r, true), true
}
