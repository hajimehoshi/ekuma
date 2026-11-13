// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

type genState uint8

const (
	genSuspendedStart genState = iota
	genSuspendedYield
	genRunning
	genCompleted
)

type ResumeMode uint8

const (
	ResumeNext ResumeMode = iota
	ResumeThrow
	ResumeReturn
)

// closeSignal is thrown through a suspended generator to make it return:
// only finally blocks intercept it. The value to return is generator.closeValue.
var closeSignal = &Object{Kind: kindHole}

// generator is the suspended activation of a generator or async function.
// Its registers are not on the stack, so that closures can keep referring to them.
type Generator struct {
	// object is the generator object, or the promise of an async function.
	object *Object

	f         *Object
	this      Value
	newTarget *Object
	argc      int
	extra     []Value

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
	Delegating bool

	closeValue Value

	// openUpvals are the upvalues that point into regs.
	openUpvals []*upvalue

	// async is the state of an async function or async generator.
	Async *asyncState
}

func (g *Generator) findUpval(index int) *upvalue {
	for _, u := range g.openUpvals {
		if u.index == index {
			return u
		}
	}
	u := &upvalue{
		p:     &g.regs[index],
		index: index,
	}
	g.openUpvals = append(g.openUpvals, u)
	return u
}

// closeUpvals detaches the upvalues that refer to the registers from index from on.
func (g *Generator) closeUpvals(from int) {
	n := 0
	for _, u := range g.openUpvals {
		if u.index < from {
			g.openUpvals[n] = u
			n++
			continue
		}
		u.val = *u.p
		u.p = &u.val
	}
	clear(g.openUpvals[n:])
	g.openUpvals = g.openUpvals[:n]
}

// newGenerator creates the activation of the generator or async function f without an object.
func (vm *VM) newGenerator(f *Object, this Value, args []Value) *Generator {
	fp := f.Fn.Code
	g := &Generator{
		f:    f,
		this: this,
		argc: len(args),
		regs: make([]Value, fp.nregs+1),
	}
	n := copy(g.regs[:fp.nparams], args)
	for i := n; i < fp.nclear; i++ {
		g.regs[i] = Undefined
	}
	if len(args) > fp.nparams && fp.needsExtra {
		g.extra = append(g.extra, args[fp.nparams:]...)
	}
	return g
}

// pushGenerator creates a generator for a call of f and pushes its frame, so that the caller
// can run it up to its initial suspension.
func (vm *VM) pushGenerator(f *Object, this Value, args []Value, ret int32) {
	g := vm.newGenerator(f, this, args)
	proto := vm.GeneratorProto
	if p := vm.GetV(Value{O: f}, vm.K.Prototype); p.IsObject() {
		proto = p.O
	}
	o := vm.NewObject(proto)
	o.Class = classGenerator
	o.Ext = g
	g.object = o
	g.state = genRunning
	vm.pushGenFrame(g, ret)
}

// pushGenFrame pushes the frame that continues g at g.pc.
func (vm *VM) pushGenFrame(g *Generator, ret int32) {
	base := vm.sp
	vm.frames = append(vm.frames, frame{
		fp:        g.f.Fn.Code,
		cl:        g.f,
		pc:        g.pc,
		base:      base,
		this:      g.this,
		ret:       ret,
		argc:      g.argc,
		extraBase: -1,
		newTarget: g.newTarget,
		gen:       g,
		scopes:    g.scopes,
	})
}

// callCoroutine calls the generator or async function f from Go.
func (vm *VM) callCoroutine(f *Object, this Value, args []Value) Value {
	c := f.Fn.Code
	if c.isAsync {
		return vm.callAsync(f, this, args)
	}
	sp := vm.sp
	vm.pushGenerator(f, this, args, 0)
	r := vm.run(len(vm.frames) - 1)
	vm.sp = sp
	return r
}

// resume continues the suspended generator g. It returns the yielded or returned value
// and whether g completed. An exception that leaves g is thrown.
func (vm *VM) Resume(g *Generator, mode ResumeMode, v Value) (Value, bool) {
	switch g.state {
	case genRunning:
		vm.ThrowTypeError("Generator is already running")
	case genCompleted:
		switch mode {
		case ResumeThrow:
			vm.Throw(v)
		case ResumeReturn:
			return v, true
		}
		return Undefined, true
	case genSuspendedStart:
		switch mode {
		case ResumeThrow:
			g.state = genCompleted
			g.regs = nil
			vm.Throw(v)
		case ResumeReturn:
			g.state = genCompleted
			g.regs = nil
			return v, true
		}
	}
	if len(vm.frames) >= maxFrames {
		vm.ThrowRangeError("Maximum call stack size exceeded")
	}
	sp := vm.sp
	started := g.state == genSuspendedYield
	g.state = genRunning
	g.mode = mode
	vm.pushGenFrame(g, 0)
	stop := len(vm.frames) - 1
	if started {
		g.regs[g.resumeReg] = v
		// The code after an await or a plain yield does not look at the mode: deliver
		// a throw or a return as an exception at the suspension.
		switch mode {
		case ResumeThrow:
			vm.thrown = v
		case ResumeReturn:
			g.closeValue = v
			vm.thrown = Value{O: closeSignal}
		}
		if mode != ResumeNext && !g.Delegating {
			g.mode = ResumeNext
			if !vm.unwind(stop) {
				vm.sp = sp
				if vm.thrown.O == closeSignal {
					vm.thrown = Undefined
					return g.closeValue, true
				}
				panic(errThrow)
			}
		} else {
			vm.thrown = Undefined
		}
	}
	var r Value
	if !vm.protect(func() {
		r = vm.run(stop)
	}) {
		vm.sp = sp
		if vm.thrown.O == closeSignal {
			vm.thrown = Undefined
			return g.closeValue, true
		}
		panic(errThrow)
	}
	vm.sp = sp
	return r, g.state == genCompleted
}
