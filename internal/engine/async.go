// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// asyncState is the state of an async generator beyond that of a generator.
type asyncState struct {
	// queue is the [[AsyncGeneratorQueue]].
	queue []asyncGeneratorRequest

	// draining is set in the state draining-queue, while the generator is completed.
	draining bool
}

// asyncFunction is the asyncState of every async function, which only marks it as one.
var asyncFunction = &asyncState{}

// asyncGeneratorRequest is an AsyncGeneratorRequest Record.
type asyncGeneratorRequest struct {
	mode       ResumeMode
	value      Value
	capability *PromiseCapability
}

type asyncGeneratorState uint8

const (
	asyncGenSuspendedStart asyncGeneratorState = iota
	asyncGenSuspendedYield
	asyncGenExecuting
	asyncGenDrainingQueue
	asyncGenCompleted
)

// asyncGeneratorStateOf returns the [[AsyncGeneratorState]] of g.
func asyncGeneratorStateOf(g *generator) asyncGeneratorState {
	switch g.state {
	case genSuspendedStart:
		return asyncGenSuspendedStart
	case genSuspendedYield:
		// An async generator that waits for a promise is still executing.
		if g.awaiting {
			return asyncGenExecuting
		}
		return asyncGenSuspendedYield
	case genRunning:
		return asyncGenExecuting
	}
	if g.async.draining {
		return asyncGenDrainingQueue
	}
	return asyncGenCompleted
}

// callAsync calls the async function f, which is not an async generator, from Go.
func (vm *VM) callAsync(f *Object, this Value, args []Value) Value {
	g := vm.newGenerator(f, this, args)
	g.async = asyncFunction
	vm.InRealm((*Realm)(f.fn.code.realm), func() {
		g.object = vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype))
		g.state = genSuspendedStart
		vm.asyncFunctionResume(g, ResumeNext, Undefined)
	})
	return Value{o: g.object}
}

// takeThrown returns the pending exception and clears it.
func (vm *VM) takeThrown() Value {
	e := vm.thrown
	vm.thrown = Undefined
	return e
}

// asyncResume continues the async function or async generator g after an await.
func (vm *VM) asyncResume(g *generator, mode ResumeMode, v Value) {
	if g.async == asyncFunction {
		vm.asyncFunctionResume(g, mode, v)
		return
	}
	vm.asyncGeneratorResume(g, mode, v)
}

// asyncFunctionResume runs the async function g from its suspension until it awaits or
// completes, and settles its promise when it completes.
func (vm *VM) asyncFunctionResume(g *generator, mode ResumeMode, v Value) {
	defer g.abortOnPanic()
	for {
		var r Value
		var done bool
		if !vm.protect(func() {
			r, done = vm.resume(g, mode, v)
		}) {
			vm.rejectPromise(g.object, vm.takeThrown())
			return
		}
		if done {
			vm.resolvePromise(g.object, r)
			return
		}
		if vm.tryAwait(g, r, reactionAwait) {
			return
		}
		mode, v = ResumeThrow, vm.takeThrown()
	}
}

// asyncGeneratorResume runs the async generator g from its suspension until it awaits,
// yields with no request left, or completes.
func (vm *VM) asyncGeneratorResume(g *generator, mode ResumeMode, v Value) {
	a := g.async
	resumerRealm := vm.CurrentRealm()
	for {
		var r Value
		var done bool
		if !vm.protect(func() {
			r, done = vm.resume(g, mode, v)
		}) {
			a.draining = true
			vm.asyncGeneratorCompleteStep(g, resumerRealm, vm.takeThrown(), true, true)
			vm.asyncGeneratorDrainQueue(g)
			return
		}
		if done {
			a.draining = true
			vm.asyncGeneratorCompleteStep(g, resumerRealm, r, false, true)
			vm.asyncGeneratorDrainQueue(g)
			return
		}
		if g.awaiting {
			if vm.tryAwait(g, r, reactionAwait) {
				return
			}
			mode, v = ResumeThrow, vm.takeThrown()
			continue
		}

		// AsyncGeneratorYield: the generator stays executing while it settles the request.
		g.awaiting = true
		vm.asyncGeneratorCompleteStep(g, resumerRealm, r, false, false)
		if len(a.queue) == 0 {
			g.awaiting = false
			return
		}
		next := &a.queue[0]
		mode, v = next.mode, next.value
		if mode == ResumeReturn {
			// AsyncGeneratorUnwrapYieldResumption awaits the value to return.
			if vm.tryAwait(g, v, reactionAwaitReturn) {
				return
			}
			mode, v = ResumeThrow, vm.takeThrown()
		}
	}
}

// asyncGeneratorCompleteStep implements AsyncGeneratorCompleteStep for a completion with
// the value v, which is a throw completion when thrown is set.
func (vm *VM) asyncGeneratorCompleteStep(g *generator, realm *Realm, v Value, thrown, done bool) {
	a := g.async
	next := a.queue[0]
	n := copy(a.queue, a.queue[1:])
	a.queue[n] = asyncGeneratorRequest{}
	a.queue = a.queue[:n]
	if thrown {
		vm.RejectCapability(next.capability, v)
		return
	}
	vm.ResolveCapability(next.capability, vm.CreateIterResultObject(realm, v, done))
}

// asyncGeneratorDrainQueue implements AsyncGeneratorDrainQueue.
func (vm *VM) asyncGeneratorDrainQueue(g *generator) {
	a := g.async
	for len(a.queue) > 0 {
		next := &a.queue[0]
		switch next.mode {
		case ResumeReturn:
			vm.asyncGeneratorAwaitReturn(g)
			return
		case ResumeThrow:
			vm.asyncGeneratorCompleteStep(g, vm.CurrentRealm(), next.value, true, true)
		default:
			vm.asyncGeneratorCompleteStep(g, vm.CurrentRealm(), Undefined, false, true)
		}
	}
	a.draining = false
}

// asyncGeneratorAwaitReturn implements AsyncGeneratorAwaitReturn.
func (vm *VM) asyncGeneratorAwaitReturn(g *generator) {
	v := g.async.queue[0].value
	if !vm.protect(func() {
		vm.awaitValue(v, reactionAction{
			kind: reactionAsyncGeneratorReturn,
			gen:  g,
		})
	}) {
		vm.asyncGeneratorCompleteStep(g, vm.CurrentRealm(), vm.takeThrown(), true, true)
		vm.asyncGeneratorDrainQueue(g)
	}
}

// ResumeAsyncGenerator implements the next, return and throw methods of async generators,
// selected by mode, for the receiver gen. It returns the promise of the iterator result.
func (vm *VM) ResumeAsyncGenerator(gen Value, mode ResumeMode, v Value) Value {
	c := vm.newIntrinsicCapability()
	var g *generator
	if gen.IsObject() {
		g, _ = gen.o.extState().(*generator)
	}
	if g == nil || g.async == nil || g.async == asyncFunction {
		vm.RejectCapability(c, vm.NewError(ErrorKindType, "method called on an object that is not an async generator"))
		return c.Promise()
	}
	defer g.abortOnPanic()
	a := g.async
	state := asyncGeneratorStateOf(g)
	if mode == ResumeThrow && state == asyncGenSuspendedStart {
		g.state = genCompleted
		g.regs = nil
		state = asyncGenCompleted
	}
	if state == asyncGenCompleted {
		switch mode {
		case ResumeNext:
			vm.ResolveCapability(c, vm.CreateIterResultObject(vm.CurrentRealm(), Undefined, true))
			return c.Promise()
		case ResumeThrow:
			vm.RejectCapability(c, v)
			return c.Promise()
		}
	}
	a.queue = append(a.queue, asyncGeneratorRequest{
		mode:       mode,
		value:      v,
		capability: c,
	})
	switch state {
	case asyncGenSuspendedStart:
		if mode == ResumeReturn {
			g.state = genCompleted
			g.regs = nil
			a.draining = true
			vm.asyncGeneratorAwaitReturn(g)
			break
		}
		vm.asyncGeneratorResume(g, mode, v)
	case asyncGenCompleted:
		// Only a return request gets here.
		a.draining = true
		vm.asyncGeneratorAwaitReturn(g)
	case asyncGenSuspendedYield:
		if mode == ResumeReturn {
			// AsyncGeneratorUnwrapYieldResumption awaits the value to return.
			g.awaiting = true
			if vm.tryAwait(g, v, reactionAwaitReturn) {
				break
			}
			mode, v = ResumeThrow, vm.takeThrown()
		}
		vm.asyncGeneratorResume(g, mode, v)
	}
	return c.Promise()
}

// asyncFromSyncIterator is the [[SyncIteratorRecord]] of an %AsyncFromSyncIteratorPrototype% object.
type asyncFromSyncIterator struct {
	iterator, next Value
}

// asyncFromSyncIterator implements CreateAsyncFromSyncIterator for the iterator it and
// its next method, and returns the async iterator and its next method.
func (vm *VM) asyncFromSyncIterator(it, next Value) (Value, Value) {
	if vm.asyncFromSyncProto == nil {
		vm.initAsyncFromSyncIteratorPrototype()
	}
	o := vm.NewObject(vm.asyncFromSyncProto)
	o.class = classIterator
	o.ext = &asyncFromSyncIterator{
		iterator: it,
		next:     next,
	}
	return Value{o: o}, vm.GetV(Value{o: o}, vm.Key(keyNext))
}

// CreateAsyncFromSyncIterator implements CreateAsyncFromSyncIterator for the iterator it and
// its next method, and returns the async iterator and its next method.
func (vm *VM) CreateAsyncFromSyncIterator(it, next Value) (Value, Value) {
	return vm.asyncFromSyncIterator(it, next)
}

func (vm *VM) thisAsyncFromSyncIterator(this Value) *asyncFromSyncIterator {
	var s *asyncFromSyncIterator
	if this.IsObject() {
		s, _ = this.o.extState().(*asyncFromSyncIterator)
	}
	if s == nil {
		vm.ThrowTypeError("method called on an object that is not an async-from-sync iterator")
	}
	return s
}

// initAsyncFromSyncIteratorPrototype creates %AsyncFromSyncIteratorPrototype%.
func (vm *VM) initAsyncFromSyncIteratorPrototype() {
	proto := vm.NewObject(vm.Intrinsic(IntrinsicAsyncIteratorPrototype))
	vm.asyncFromSyncProto = proto
	vm.Define(proto, vm.Key(keyNext), Value{o: vm.NewNative("next", 0, func(vm *VM, this Value, args []Value) Value {
		s := vm.thisAsyncFromSyncIterator(this)
		c := vm.newIntrinsicCapability()
		var result Value
		if !vm.IfAbruptRejectPromise(c, func() {
			if len(args) > 0 {
				result = vm.CallValue(s.next, s.iterator, args[0])
			} else {
				result = vm.CallValue(s.next, s.iterator)
			}
			if !result.IsObject() {
				vm.ThrowTypeError("iterator result " + vm.DisplayString(result) + " is not an object")
			}
		}) {
			return c.Promise()
		}
		return vm.asyncFromSyncIteratorContinuation(result, c, s.iterator, true)
	})}, FlagsHidden)
	vm.Define(proto, vm.Key(keyReturn), Value{o: vm.NewNative("return", 0, func(vm *VM, this Value, args []Value) Value {
		s := vm.thisAsyncFromSyncIterator(this)
		c := vm.newIntrinsicCapability()
		var result Value
		var hasReturn bool
		if !vm.IfAbruptRejectPromise(c, func() {
			m := vm.GetMethod(s.iterator, vm.Key(keyReturn))
			if m == nil {
				return
			}
			hasReturn = true
			if len(args) > 0 {
				result = vm.Call(m, s.iterator, args[0])
			} else {
				result = vm.Call(m, s.iterator)
			}
		}) {
			return c.Promise()
		}
		if !hasReturn {
			vm.ResolveCapability(c, vm.CreateIterResultObject(vm.CurrentRealm(), argAt(args, 0), true))
			return c.Promise()
		}
		if !result.IsObject() {
			vm.RejectCapability(c, vm.NewError(ErrorKindType, "iterator result "+vm.DisplayString(result)+" is not an object"))
			return c.Promise()
		}
		return vm.asyncFromSyncIteratorContinuation(result, c, s.iterator, false)
	})}, FlagsHidden)
	vm.Define(proto, vm.Key(keyThrow), Value{o: vm.NewNative("throw", 0, func(vm *VM, this Value, args []Value) Value {
		s := vm.thisAsyncFromSyncIterator(this)
		c := vm.newIntrinsicCapability()
		var result Value
		var hasThrow bool
		if !vm.IfAbruptRejectPromise(c, func() {
			m := vm.GetMethod(s.iterator, vm.Key(keyThrow))
			if m == nil {
				vm.iteratorClose(s.iterator, false)
				return
			}
			hasThrow = true
			if len(args) > 0 {
				result = vm.Call(m, s.iterator, args[0])
			} else {
				result = vm.Call(m, s.iterator)
			}
		}) {
			return c.Promise()
		}
		if !hasThrow {
			vm.RejectCapability(c, vm.NewError(ErrorKindType, "the iterator does not provide a 'throw' method"))
			return c.Promise()
		}
		if !result.IsObject() {
			vm.RejectCapability(c, vm.NewError(ErrorKindType, "iterator result "+vm.DisplayString(result)+" is not an object"))
			return c.Promise()
		}
		return vm.asyncFromSyncIteratorContinuation(result, c, s.iterator, true)
	})}, FlagsHidden)
}

// asyncFromSyncIteratorContinuation implements AsyncFromSyncIteratorContinuation.
func (vm *VM) asyncFromSyncIteratorContinuation(result Value, c *PromiseCapability, it Value, closeOnRejection bool) Value {
	var done bool
	var value Value
	if !vm.IfAbruptRejectPromise(c, func() {
		done = ToBoolean(vm.GetV(result, vm.Key(keyDone)))
		value = vm.GetV(result, vm.Key(KeyValue))
	}) {
		return c.Promise()
	}
	r := reactionAction{
		kind:       reactionUnwrap,
		done:       done,
		capability: c,
	}
	if !done && closeOnRejection {
		r.iterator = it.o
	}
	if !vm.protect(func() {
		vm.awaitValue(value, r)
	}) {
		e := vm.takeThrown()
		if !done && closeOnRejection {
			vm.iteratorClose(it, true)
		}
		vm.RejectCapability(c, e)
	}
	return c.Promise()
}

// Completion runs f and reports whether it completed normally. When it throws,
// the result is the exception.
func (vm *VM) Completion(f func()) (Value, bool) {
	if vm.protect(f) {
		return Undefined, true
	}
	return vm.takeThrown(), false
}

// AsyncIteratorClose implements AsyncIteratorClose for a built-in async function: it closes
// the iterator it for the completion with the value v, which is a throw completion when
// thrown is set, and calls k with the resulting completion, now or in a later job.
func (vm *VM) AsyncIteratorClose(it Value, v Value, thrown bool, k AwaitHandler) {
	fail := func(e Value) {
		if thrown {
			k(vm, v, true)
			return
		}
		k(vm, e, true)
	}
	var m *Object
	if e, ok := vm.Completion(func() {
		m = vm.GetMethod(it, vm.Key(keyReturn))
	}); !ok {
		fail(e)
		return
	}
	if m == nil {
		k(vm, v, thrown)
		return
	}
	var r Value
	if e, ok := vm.Completion(func() {
		r = vm.Call(m, it)
	}); !ok {
		fail(e)
		return
	}
	if e, ok := vm.Completion(func() {
		vm.Await(r, func(vm *VM, r Value, rejected bool) {
			switch {
			case thrown:
				k(vm, v, true)
			case rejected:
				k(vm, r, true)
			case !r.IsObject():
				k(vm, vm.NewError(ErrorKindType, "iterator result "+vm.DisplayString(r)+" is not an object"), true)
			default:
				k(vm, v, false)
			}
		})
	}); !ok {
		fail(e)
	}
}
