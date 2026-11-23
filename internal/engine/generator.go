// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
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
// only finally blocks intercept it. The value to return is generator.closeValue.
var closeSignal = &Object{kind: kindHole}

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
	g.closeUpvals(0)
	g.state = genCompleted
	g.regs, g.scopes, g.extra = nil, nil, nil
	g.this, g.closeValue = Undefined, Undefined
	if g.async != nil && g.async != asyncFunction {
		g.async.queue = nil
		g.async.draining = false
	}
}

func (g *generator) findUpval(index int) *upvalue {
	i := len(g.openUpvals)
	if i > 0 && g.openUpvals[i-1].index > index {
		i = sort.Search(i-1, func(j int) bool {
			return g.openUpvals[j].index > index
		})
	}
	if i > 0 && g.openUpvals[i-1].index == index {
		return g.openUpvals[i-1]
	}
	u := &upvalue{
		p:     &g.regs[index],
		index: index,
	}
	g.openUpvals = slices.Insert(g.openUpvals, i, u)
	return u
}

// closeUpvals detaches the upvalues that refer to the registers from index from on.
func (g *generator) closeUpvals(from int) {
	n := len(g.openUpvals)
	for n > 0 && g.openUpvals[n-1].index >= from {
		n--
		u := g.openUpvals[n]
		u.val = *u.p
		u.p = &u.val
	}
	clear(g.openUpvals[n:])
	g.openUpvals = g.openUpvals[:n]
}

// newGenerator creates the activation of the generator or async function f without an object.
func newGenerator(f *Object, this Value, args []Value) *generator {
	fp := f.fn.code
	fp.prepare()
	g := &generator{
		f:    f,
		fp:   fp,
		this: this,
		argc: len(args),
		regs: make([]Value, fp.nregs+1),
	}
	copy(g.regs[:fp.nparams], args)
	if len(args) > fp.nparams && fp.needsExtra {
		g.extra = append(g.extra, args[fp.nparams:]...)
	}
	return g
}

// pushGenerator creates the activation of a generator or async generator for a call of f and
// pushes its frame, so that the caller can run it up to its initial suspension, which creates
// the generator object.
func (vm *VM) pushGenerator(f *Object, this Value, args []Value, ret int32) {
	g := newGenerator(f, this, args)
	if g.fp.isAsync {
		g.async = &asyncState{}
	}
	g.state = genRunning
	vm.pushGenFrame(g, ret)
}

// newGeneratorObject creates the generator object of g, whose parameters are instantiated.
func (cx *ExecutionContext) newGeneratorObject(g *generator) *Object {
	r := g.fp.context.realm
	proto := r.intrinsics[IntrinsicGeneratorPrototype]
	if g.async != nil {
		proto = r.intrinsics[IntrinsicAsyncGeneratorPrototype]
	}
	if p := cx.GetV(Value{o: g.f}, cx.Key(KeyPrototype)); p.IsObject() {
		proto = p.o
	}
	o := g.fp.context.NewObject(proto)
	o.class = classGenerator
	o.setExtState(g)
	g.object = o
	return o
}

// pushGenFrame pushes the frame that continues g at g.pc.
func (vm *VM) pushGenFrame(g *generator, ret int32) {
	base := vm.sp
	fr := vm.appendFrame()
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
}

// callCoroutine calls the generator, async function or async generator f from Go.
func (cx *ExecutionContext) callCoroutine(f *Object, this Value, args []Value) Value {
	c := f.fn.code
	if c.isAsync && !c.isGenerator {
		return cx.callAsync(f, this, args)
	}
	sp := cx.sp
	cx.pushGenerator(f, this, args, 0)
	r := cx.run(len(cx.frames) - 1)
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
		return Undefined, true
	case genSuspendedStart:
		switch mode {
		case ResumeThrow:
			g.state = genCompleted
			g.regs = nil
			resumer.Throw(v)
		case ResumeReturn:
			g.state = genCompleted
			g.regs = nil
			return v, true
		}
	}
	if len(resumer.frames) >= maxFrames {
		resumer.ThrowRangeError("maximum call stack size exceeded")
	}
	sp := resumer.sp
	started := g.state == genSuspendedYield
	g.state = genRunning
	g.mode = mode
	resumer.pushGenFrame(g, 0)
	stop := len(resumer.frames) - 1
	if started {
		g.regs[g.resumeReg] = v
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
			resumer.thrown = Value{o: closeSignal}
		}
		if mode != ResumeNext && !g.delegating {
			g.mode = ResumeNext
			if !resumer.unwind(stop) {
				resumer.sp = sp
				if resumer.thrown.o == closeSignal {
					resumer.thrown = Undefined
					return g.closeValue, true
				}
				panic(errThrow)
			}
		} else {
			resumer.thrown = Undefined
		}
	}
	r, ok := resumer.runFrame(stop)
	resumer.sp = sp
	if !ok {
		resumer.dropNatives(stop)
		if resumer.thrown.o == closeSignal {
			resumer.thrown = Undefined
			return g.closeValue, true
		}
		panic(errThrow)
	}
	return r, g.state == genCompleted
}

// isSyncGenerator reports whether v is a generator object that is not an async generator.
func isSyncGenerator(v Value) bool {
	if !v.IsObject() {
		return false
	}
	g, _ := v.o.extState().(*generator)
	return g != nil && g.async == nil
}

// ResumeGenerator continues the generator gen with the value v and returns the iterator result.
// It reports false, without running anything, when gen is not a generator.
func (cx *ExecutionContext) ResumeGenerator(gen Value, mode ResumeMode, v Value) (Value, bool) {
	var g *generator
	if gen.IsObject() {
		g, _ = gen.o.extState().(*generator)
	}
	if g == nil || g.async != nil {
		return Undefined, false
	}
	r, done := cx.resume(g, mode, v)
	if !done {
		if g.delegating {
			// yield* passes the result object of the inner iterator through.
			return r, true
		}
		// Yield creates its result before returning to the resuming method's realm.
		return g.fp.context.CreateIterResultObject(r, false), true
	}
	return cx.CreateIterResultObject(r, true), true
}
