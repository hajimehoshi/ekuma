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
func (ag *agent) callAsync(f *Object, this Value, args []Value) Value {
	g := newGenerator(f, this, args)
	g.async = asyncFunction
	cx := g.fp.context
	var promise *Object
	cx.Run(func() {
		promise = cx.NewPromiseObject(cx.Intrinsic(IntrinsicPromisePrototype))
		g.object = promise
		g.asyncFunctionResume(ResumeNext, Undefined)
	})
	return Value{o: promise}
}

// takeThrown returns the pending exception and clears it.
func (ag *agent) takeThrown() Value {
	e := ag.thrown
	ag.thrown = Undefined
	return e
}

// resumeAfterAwait continues the async function or async generator g after an await.
func (g *generator) resumeAfterAwait(mode ResumeMode, v Value) {
	if g.async == asyncFunction {
		g.asyncFunctionResume(mode, v)
		return
	}
	g.fp.context.asyncGeneratorResume(g, mode, v)
}

// asyncFunctionResume runs the async function g from its suspension until it awaits or
// completes, and settles its promise when it completes.
func (g *generator) asyncFunctionResume(mode ResumeMode, v Value) {
	defer g.abortOnPanic()
	cx := g.fp.context
	promise := g.object
	f, fp, this := g.f, g.fp, g.this
	for {
		var r Value
		var done bool
		if !cx.protect(func() {
			r, done = cx.resume(g, mode, v)
		}) {
			cx.rejectPromise(promise, cx.takeThrown())
			return
		}
		if done {
			if cx.Quiet().tryResolvePromise(promise, r) {
				return
			}
			// The code that reading then runs sees the function at its return, as does the error
			// for a promise that resolves to itself.
			g.f, g.fp, g.this = f, fp, this
			g.underFrame(func() {
				cx.resolvePromise(cx.realm, promise, r)
			})
			g.complete()
			return
		}
		if g.tryAwait(r, reactionAwait) {
			return
		}
		mode, v = ResumeThrow, cx.takeThrown()
	}
}

// underFrame calls f, which must not throw, with the frame of g at its suspension or return.
func (g *generator) underFrame(f func()) {
	cx := g.fp.context
	n := len(cx.frames)
	state := g.state
	cx.pushGenFrame(g, 0)
	f()
	cx.unwindTo(n)
	g.state = state
}

// asyncGeneratorResume runs the async generator g from its suspension until it awaits,
// yields with no request left, or completes.
func (resumer *ExecutionContext) asyncGeneratorResume(g *generator, mode ResumeMode, v Value) {
	a := g.async
	for {
		var r Value
		var done bool
		ok := resumer.protect(func() {
			r, done = resumer.resume(g, mode, v)
		})
		if !ok || done {
			if !ok {
				r = resumer.takeThrown()
			}
			// A resume refused at the depth limit consumes only the current request.
			a.draining = g.state == genCompleted
			resumer.asyncGeneratorCompleteStep(g, r, !ok, true)
			if a.draining {
				resumer.asyncGeneratorDrainQueue(g)
				return
			}
		} else if g.awaiting {
			if g.tryAwait(r, reactionAwait) {
				return
			}
			mode, v = ResumeThrow, resumer.takeThrown()
			continue
		} else {
			// AsyncGeneratorYield: the generator stays executing while it settles the request.
			g.awaiting = true
			resumer.asyncGeneratorCompleteStep(g, r, false, false)
		}
		if len(a.queue) == 0 {
			g.awaiting = false
			return
		}
		next := &a.queue[0]
		mode, v = next.mode, next.value
		if mode == ResumeReturn {
			// AsyncGeneratorUnwrapYieldResumption awaits the value to return.
			if g.tryAwait(v, reactionAwaitReturn) {
				return
			}
			mode, v = ResumeThrow, resumer.takeThrown()
		}
	}
}

// asyncGeneratorCompleteStep implements AsyncGeneratorCompleteStep for a completion with
// the value v, which is a throw completion when thrown is set.
func (cx *ExecutionContext) asyncGeneratorCompleteStep(g *generator, v Value, thrown, done bool) {
	a := g.async
	next := a.queue[0]
	copy(a.queue, a.queue[1:])
	a.queue = withoutLast(a.queue)
	if thrown {
		cx.RejectCapability(next.capability, v)
		return
	}
	cx.ResolveCapability(next.capability, cx.CreateIterResultObject(v, done))
}

// asyncGeneratorDrainQueue implements AsyncGeneratorDrainQueue.
func (cx *ExecutionContext) asyncGeneratorDrainQueue(g *generator) {
	a := g.async
	for len(a.queue) > 0 {
		next := &a.queue[0]
		switch next.mode {
		case ResumeReturn:
			cx.asyncGeneratorAwaitReturn(g)
			return
		case ResumeThrow:
			cx.asyncGeneratorCompleteStep(g, next.value, true, true)
		default:
			cx.asyncGeneratorCompleteStep(g, Undefined, false, true)
		}
	}
	a.draining = false
}

// asyncGeneratorAwaitReturn implements AsyncGeneratorAwaitReturn.
func (cx *ExecutionContext) asyncGeneratorAwaitReturn(g *generator) {
	g.async.draining = true
	v := g.async.queue[0].value
	if !cx.protect(func() {
		cx.awaitValue(v, reactionAction{
			kind: reactionAsyncGeneratorReturn,
			gen:  g,
		})
	}) {
		cx.asyncGeneratorCompleteStep(g, cx.takeThrown(), true, true)
		cx.asyncGeneratorDrainQueue(g)
	}
}

// ResumeAsyncGenerator implements the next, return and throw methods of async generators,
// selected by mode, for the receiver gen. It returns the promise of the iterator result.
func (cx *ExecutionContext) ResumeAsyncGenerator(gen Value, mode ResumeMode, v Value) Value {
	c := cx.newIntrinsicCapability()
	var g *generator
	if gen.IsObject() {
		g, _ = gen.o.extState().(*generator)
	}
	if g == nil || g.async == nil || g.async == asyncFunction {
		cx.RejectCapability(c, cx.NewError(ErrorKindType, "method called on an object that is not an async generator"))
		return c.Promise()
	}
	defer g.abortOnPanic()
	a := g.async
	state := asyncGeneratorStateOf(g)
	if mode != ResumeNext && state == asyncGenSuspendedStart {
		g.complete()
		state = asyncGenCompleted
	}
	if state == asyncGenCompleted {
		switch mode {
		case ResumeNext:
			cx.ResolveCapability(c, cx.CreateIterResultObject(Undefined, true))
			return c.Promise()
		case ResumeThrow:
			cx.RejectCapability(c, v)
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
		cx.asyncGeneratorResume(g, mode, v)
	case asyncGenCompleted:
		// Only a return request gets here.
		cx.asyncGeneratorAwaitReturn(g)
	case asyncGenSuspendedYield:
		if mode == ResumeReturn {
			// AsyncGeneratorUnwrapYieldResumption awaits the value to return.
			g.awaiting = true
			if g.tryAwait(v, reactionAwaitReturn) {
				break
			}
			mode, v = ResumeThrow, cx.takeThrown()
		}
		cx.asyncGeneratorResume(g, mode, v)
	}
	return c.Promise()
}

// asyncFromSyncIterator is the [[SyncIteratorRecord]] of an %AsyncFromSyncIteratorPrototype% object.
type asyncFromSyncIterator struct {
	iterator, next Value
}

// asyncFromSyncIterator implements CreateAsyncFromSyncIterator for the iterator it and
// its next method, and returns the async iterator and its next method.
func (cx *ExecutionContext) asyncFromSyncIterator(it, next Value) (Value, Value) {
	if cx.realm.asyncFromSyncProto == nil {
		cx.initAsyncFromSyncIteratorPrototype()
	}
	o := cx.NewObject(cx.realm.asyncFromSyncProto)
	o.class = classIterator
	o.ext = &asyncFromSyncIterator{
		iterator: it,
		next:     next,
	}
	return Value{o: o}, cx.GetV(Value{o: o}, cx.Key(KeyNext))
}

// CreateAsyncFromSyncIterator implements CreateAsyncFromSyncIterator for the iterator it and
// its next method, and returns the async iterator and its next method.
func (cx *ExecutionContext) CreateAsyncFromSyncIterator(it, next Value) (Value, Value) {
	return cx.asyncFromSyncIterator(it, next)
}

func (cx *ExecutionContext) thisAsyncFromSyncIterator(this Value) *asyncFromSyncIterator {
	var s *asyncFromSyncIterator
	if this.IsObject() {
		s, _ = this.o.extState().(*asyncFromSyncIterator)
	}
	if s == nil {
		cx.ThrowTypeError("method called on an object that is not an async-from-sync iterator")
	}
	return s
}

// initAsyncFromSyncIteratorPrototype creates %AsyncFromSyncIteratorPrototype%.
func (cx *ExecutionContext) initAsyncFromSyncIteratorPrototype() {
	proto := cx.NewObject(cx.Intrinsic(IntrinsicAsyncIteratorPrototype))
	cx.realm.asyncFromSyncProto = proto
	cx.Define(proto, cx.Key(KeyNext), Value{o: cx.NewNative("next", 0, func(cx *ExecutionContext, this Value, args []Value) Value {
		s := cx.thisAsyncFromSyncIterator(this)
		c := cx.newIntrinsicCapability()
		var result Value
		if !cx.IfAbruptRejectPromise(c, func() {
			if len(args) > 0 {
				result = cx.CallValue(s.next, s.iterator, args[0])
			} else {
				result = cx.CallValue(s.next, s.iterator)
			}
			if !result.IsObject() {
				cx.ThrowTypeError("iterator result " + cx.DisplayString(result) + " is not an object")
			}
		}) {
			return c.Promise()
		}
		return cx.asyncFromSyncIteratorContinuation(result, c, s.iterator, true)
	})}, FlagsHidden)
	cx.Define(proto, cx.Key(KeyReturn), Value{o: cx.NewNative("return", 0, func(cx *ExecutionContext, this Value, args []Value) Value {
		s := cx.thisAsyncFromSyncIterator(this)
		c := cx.newIntrinsicCapability()
		var result Value
		var hasReturn bool
		if !cx.IfAbruptRejectPromise(c, func() {
			m := cx.GetMethod(s.iterator, cx.Key(KeyReturn))
			if m == nil {
				return
			}
			hasReturn = true
			if len(args) > 0 {
				result = cx.Call(m, s.iterator, args[0])
			} else {
				result = cx.Call(m, s.iterator)
			}
		}) {
			return c.Promise()
		}
		if !hasReturn {
			cx.ResolveCapability(c, cx.CreateIterResultObject(argAt(args, 0), true))
			return c.Promise()
		}
		if !result.IsObject() {
			cx.RejectCapability(c, cx.NewError(ErrorKindType, "iterator result "+cx.DisplayString(result)+" is not an object"))
			return c.Promise()
		}
		return cx.asyncFromSyncIteratorContinuation(result, c, s.iterator, false)
	})}, FlagsHidden)
	cx.Define(proto, cx.Key(keyThrow), Value{o: cx.NewNative("throw", 0, func(cx *ExecutionContext, this Value, args []Value) Value {
		s := cx.thisAsyncFromSyncIterator(this)
		c := cx.newIntrinsicCapability()
		var result Value
		var hasThrow bool
		if !cx.IfAbruptRejectPromise(c, func() {
			m := cx.GetMethod(s.iterator, cx.Key(keyThrow))
			if m == nil {
				cx.iteratorClose(s.iterator, false)
				return
			}
			hasThrow = true
			if len(args) > 0 {
				result = cx.Call(m, s.iterator, args[0])
			} else {
				result = cx.Call(m, s.iterator)
			}
		}) {
			return c.Promise()
		}
		if !hasThrow {
			cx.RejectCapability(c, cx.NewError(ErrorKindType, "the iterator does not provide a 'throw' method"))
			return c.Promise()
		}
		if !result.IsObject() {
			cx.RejectCapability(c, cx.NewError(ErrorKindType, "iterator result "+cx.DisplayString(result)+" is not an object"))
			return c.Promise()
		}
		return cx.asyncFromSyncIteratorContinuation(result, c, s.iterator, true)
	})}, FlagsHidden)
}

// asyncFromSyncIteratorContinuation implements AsyncFromSyncIteratorContinuation.
func (cx *ExecutionContext) asyncFromSyncIteratorContinuation(result Value, c *PromiseCapability, it Value, closeOnRejection bool) Value {
	var done bool
	var value Value
	if !cx.IfAbruptRejectPromise(c, func() {
		done = ToBoolean(cx.GetV(result, cx.Key(keyDone)))
		value = cx.GetV(result, cx.Key(KeyValue))
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
	if !cx.protect(func() {
		cx.awaitValue(value, r)
	}) {
		e := cx.takeThrown()
		if !done && closeOnRejection {
			cx.iteratorClose(it, true)
		}
		cx.RejectCapability(c, e)
	}
	return c.Promise()
}

// Completion runs f and reports whether it completed normally. When it throws,
// the result is the exception.
func (cx *ExecutionContext) Completion(f func()) (Value, bool) {
	if cx.protect(f) {
		return Undefined, true
	}
	return cx.takeThrown(), false
}

// AsyncIteratorClose implements AsyncIteratorClose for a built-in async function: it closes
// the iterator it for the completion with the value v, which is a throw completion when
// thrown is set, and calls k with the resulting completion, now or in a later job.
func (cx *ExecutionContext) AsyncIteratorClose(it Value, v Value, thrown bool, k AwaitHandler) {
	fail := func(e Value) {
		if thrown {
			k(cx, v, true)
			return
		}
		k(cx, e, true)
	}
	var m *Object
	if e, ok := cx.Completion(func() {
		m = cx.GetMethod(it, cx.Key(KeyReturn))
	}); !ok {
		fail(e)
		return
	}
	if m == nil {
		k(cx, v, thrown)
		return
	}
	var r Value
	if e, ok := cx.Completion(func() {
		r = cx.Call(m, it)
	}); !ok {
		fail(e)
		return
	}
	if e, ok := cx.Completion(func() {
		cx.Await(r, func(cx *ExecutionContext, r Value, rejected bool) {
			switch {
			case thrown:
				k(cx, v, true)
			case rejected:
				k(cx, r, true)
			case !r.IsObject():
				k(cx, cx.NewError(ErrorKindType, "iterator result "+cx.DisplayString(r)+" is not an object"), true)
			default:
				k(cx, v, false)
			}
		})
	}); !ok {
		fail(e)
	}
}
