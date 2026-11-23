// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// PromiseState is a value of [[PromiseState]].
type PromiseState uint8

const (
	PromisePending PromiseState = iota
	PromiseFulfilled
	PromiseRejected
)

// promiseData holds the internal slots of a Promise object.
type promiseData struct {
	state     PromiseState
	isHandled bool

	// result is the [[PromiseResult]].
	result Value

	// reactions are the pairs of [[PromiseFulfillReactions]] and [[PromiseRejectReactions]]
	// that PerformPromiseThen appends together, in order.
	reactions []pendingReaction
}

type reactionKind uint8

const (
	// reactionHandlers calls onFulfilled or onRejected and settles the capability with the result.
	reactionHandlers reactionKind = iota

	// reactionAwait resumes the suspended async function or async generator gen with
	// the value, or throws the reason into it.
	reactionAwait

	// reactionAwaitReturn resumes the async generator gen with a return of the value,
	// or throws the reason into it.
	reactionAwaitReturn

	// reactionAsyncGeneratorReturn completes the request at the front of the queue of
	// the async generator gen, as AsyncGeneratorAwaitReturn does.
	reactionAsyncGeneratorReturn

	// reactionUnwrap resolves the capability with an iterator result of the value with done,
	// as AsyncFromSyncIteratorContinuation does. A rejection first closes iterator if present.
	reactionUnwrap

	// reactionNative calls native.
	reactionNative
)

// pendingReaction pairs the fulfill and reject reactions registered by PerformPromiseThen.
type pendingReaction struct {
	action                  reactionAction
	onFulfilled, onRejected *Object
	continuationData        any

	// builtinRealm is the creation realm of an implicit built-in handler.
	builtinRealm *Realm
}

// reactionAction describes what the selected reaction does with its argument.
type reactionAction struct {
	kind reactionKind
	done bool

	// capability is the [[Capability]], or nil when it is undefined.
	capability *PromiseCapability
	gen        *generator
	native     AwaitHandler
	iterator   *Object
}

// AwaitHandler continues a built-in async function after an Await: v is the value of
// the awaited promise, or its rejection reason when rejected is set.
type AwaitHandler func(vm *VM, v Value, rejected bool)

// PromiseCapability is a PromiseCapability Record.
type PromiseCapability struct {
	promise *Object

	// resolve and reject are the [[Resolve]] and [[Reject]] functions. For a promise that
	// the [VM] created itself, they are nil until something asks for them.
	resolve, reject *Object

	// intrinsic is set when the [VM] created the promise and its resolving functions are
	// the ones of CreateResolvingFunctions, which then share alreadyResolved.
	intrinsic       bool
	alreadyResolved bool
}

// Promise returns the [[Promise]] of c.
func (c *PromiseCapability) Promise() Value {
	return Value{o: c.promise}
}

// jobContext is the host state in effect while a job or cleanup callback runs.
type jobContext struct {
	continuationData any

	// enteredRealm stays fixed across ordinary cross-realm calls.
	enteredRealm *Realm
}

func (vm *VM) captureJobContext() jobContext {
	return jobContext{
		continuationData: vm.continuationData,
		enteredRealm:     vm.curRealm,
	}
}

// withJobContext runs f with the captured continuation data and enters the job's realm.
func (vm *VM) withJobContext(c jobContext, f func()) {
	previous := vm.jobContext
	vm.jobContext = c
	defer func() { vm.jobContext = previous }()
	if c.enteredRealm != nil && c.enteredRealm != vm.curRealm {
		vm.InRealm(c.enteredRealm, f)
		return
	}
	f()
}

type jobKind uint8

const (
	jobReaction jobKind = iota
	jobThenable
	jobResolve
	jobCallback
)

// job is a pending microtask.
type job struct {
	kind jobKind

	// rejected is set for a rejection reaction.
	rejected bool

	context  jobContext
	reaction reactionAction

	// argument is the settlement value, rejection reason or thenable.
	argument Value

	// promise is the promise to resolve in a thenable or resolve job.
	promise *Object

	// callback is the selected reaction handler, then method or host callback.
	callback *Object
}

// IsPromise implements IsPromise.
func IsPromise(v Value) bool {
	return v.IsObject() && v.o.class == classPromise
}

// PromiseStateOf returns the [[PromiseState]] and the [[PromiseResult]] of the promise p.
func PromiseStateOf(p Value) (PromiseState, Value) {
	d := promiseOf(p.o)
	return d.state, d.result
}

func promiseOf(o *Object) *promiseData {
	return o.extState().(*promiseData)
}

// NewPromiseObject returns a pending Promise object with the prototype proto.
func (vm *VM) NewPromiseObject(proto *Object) *Object {
	return newPromiseObject(vm.shapeFor(proto))
}

func newPromiseObject(s *shape) *Object {
	w := &promiseObject{
		state: promiseData{
			result: Undefined,
		},
	}
	w.kind = kindObject
	w.shape = s
	o := w.init()
	o.class = classPromise
	o.setExtState(&w.state)
	return o
}

// promiseObject is a Promise object together with its state, so that creating one is a single
// allocation.
type promiseObject struct {
	propertyObject
	state promiseData
}

// newIntrinsicCapability returns the capability of a new promise of %Promise%,
// as NewPromiseCapability(%Promise%) does.
func (vm *VM) newIntrinsicCapability() *PromiseCapability {
	return &PromiseCapability{
		promise:   vm.NewPromiseObject(vm.CurrentRealm().Intrinsic(IntrinsicPromisePrototype)),
		intrinsic: true,
	}
}

// CreateResolvingFunctions implements CreateResolvingFunctions for the promise p.
// The result is a capability whose resolving functions are those functions.
func (vm *VM) CreateResolvingFunctions(p *Object) *PromiseCapability {
	c := &PromiseCapability{
		promise:   p,
		intrinsic: true,
	}
	vm.createResolvingFunctions(c)
	return c
}

func (vm *VM) createResolvingFunctions(c *PromiseCapability) {
	c.resolve = vm.NewNative("", 1, func(vm *VM, this Value, args []Value) Value {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.resolvePromise(c.resolvingRealm(), c.promise, argAt(args, 0))
		}
		return Undefined
	})
	c.reject = vm.NewNative("", 1, func(vm *VM, this Value, args []Value) Value {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.rejectPromise(c.promise, argAt(args, 0))
		}
		return Undefined
	})
}

func argAt(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return Undefined
}

// NewPromiseCapability implements NewPromiseCapability.
func (vm *VM) NewPromiseCapability(c Value) *PromiseCapability {
	if !c.IsObject() || !vm.IsConstructor(c.o) {
		vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(c)+" is not a constructor")
	}
	if c.o == vm.CurrentRealm().Intrinsic(IntrinsicPromise) {
		// Constructing %Promise% with an executor observes nothing that this does not.
		return vm.newIntrinsicCapability()
	}
	var resolve, reject Value
	resolve, reject = Undefined, Undefined
	executor := vm.NewNative("", 2, func(vm *VM, this Value, args []Value) Value {
		if !resolve.IsUndefined() {
			vm.ThrowTypeError(vm.CurrentRealm(), "promise capability resolve function is already set")
		}
		if !reject.IsUndefined() {
			vm.ThrowTypeError(vm.CurrentRealm(), "promise capability reject function is already set")
		}
		resolve, reject = argAt(args, 0), argAt(args, 1)
		return Undefined
	})
	p := vm.Construct(c.o, []Value{{o: executor}}, c.o)
	if !resolve.IsCallable() {
		vm.ThrowTypeError(vm.CurrentRealm(), "promise capability resolve function is not callable")
	}
	if !reject.IsCallable() {
		vm.ThrowTypeError(vm.CurrentRealm(), "promise capability reject function is not callable")
	}
	return &PromiseCapability{
		promise: p,
		resolve: resolve.o,
		reject:  reject.o,
	}
}

// ensureResolvingFunctions materializes the deferred functions of an intrinsic capability.
func (vm *VM) ensureResolvingFunctions(c *PromiseCapability) {
	if c.resolve == nil {
		vm.InRealm(c.promise.CreationRealm(), func() { vm.createResolvingFunctions(c) })
	}
}

// CapabilityResolve returns the [[Resolve]] function of c.
func (vm *VM) CapabilityResolve(c *PromiseCapability) Value {
	vm.ensureResolvingFunctions(c)
	return Value{o: c.resolve}
}

// CapabilityReject returns the [[Reject]] function of c.
func (vm *VM) CapabilityReject(c *PromiseCapability) Value {
	vm.ensureResolvingFunctions(c)
	return Value{o: c.reject}
}

// resolvingRealm returns the realm of an intrinsic capability's resolving functions.
func (c *PromiseCapability) resolvingRealm() *Realm {
	if c.resolve != nil {
		// A thenable job creates its own resolving functions in the job's realm.
		return c.resolve.fn.info.realm
	}
	return c.promise.CreationRealm()
}

// canSettleDirectly reports whether the intrinsic resolving functions run in the current realm.
func (c *PromiseCapability) canSettleDirectly(vm *VM) bool {
	return c.intrinsic && (!vm.trackJobRealms || c.resolvingRealm() == vm.CurrentRealm())
}

// ResolveCapability calls the [[Resolve]] function of c with v.
func (vm *VM) ResolveCapability(c *PromiseCapability, v Value) {
	if c.canSettleDirectly(vm) {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.resolvePromise(c.resolvingRealm(), c.promise, v)
		}
		return
	}
	vm.Call(vm.CapabilityResolve(c).o, Undefined, v)
}

// RejectCapability calls the [[Reject]] function of c with v.
func (vm *VM) RejectCapability(c *PromiseCapability, v Value) {
	if c.canSettleDirectly(vm) {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.rejectPromise(c.promise, v)
		}
		return
	}
	vm.Call(vm.CapabilityReject(c).o, Undefined, v)
}

// IfAbruptRejectPromise implements IfAbruptRejectPromise for what f does: when f throws,
// it rejects c with the exception and reports false.
func (vm *VM) IfAbruptRejectPromise(c *PromiseCapability, f func()) bool {
	if vm.protect(f) {
		return true
	}
	e := vm.thrown
	vm.thrown = Undefined
	vm.RejectCapability(c, e)
	return false
}

// resolvePromise is what a promise resolve function does with resolution once it was not
// already resolved: the promise resolution procedure.
func (vm *VM) resolvePromise(resolverRealm *Realm, p *Object, resolution Value) {
	if resolution.o == p {
		vm.rejectPromise(p, vm.NewError(resolverRealm, ErrorKindType, "chaining cycle detected for promise"))
		return
	}
	if !resolution.IsObject() {
		vm.fulfillPromise(p, resolution)
		return
	}
	var then Value
	if !vm.protect(func() {
		then = vm.GetV(resolution, vm.Key(keyThen))
	}) {
		e := vm.thrown
		vm.thrown = Undefined
		vm.rejectPromise(p, e)
		return
	}
	if !then.IsCallable() {
		vm.fulfillPromise(p, resolution)
		return
	}
	vm.enqueueThenableJob(p, resolution, then)
}

// enqueueThenableJob enqueues NewPromiseResolveThenableJob with the current continuation data.
func (vm *VM) enqueueThenableJob(p *Object, thenable, then Value) {
	vm.enqueuePromiseJob(&job{
		kind: jobThenable,
		context: jobContext{
			continuationData: vm.continuationData,
			enteredRealm:     vm.promiseJobCallbackRealm(then.o),
		},
		argument: thenable,
		promise:  p,
		callback: then.o,
	}, nil)
}

// fulfillPromise implements FulfillPromise.
func (vm *VM) fulfillPromise(p *Object, v Value) {
	d := promiseOf(p)
	reactions := d.reactions
	d.reactions = nil
	d.result = v
	d.state = PromiseFulfilled
	vm.triggerPromiseReactions(reactions, v, false)
}

// rejectPromise implements RejectPromise.
func (vm *VM) rejectPromise(p *Object, reason Value) {
	d := promiseOf(p)
	reactions := d.reactions
	d.reactions = nil
	d.result = reason
	d.state = PromiseRejected
	if !d.isHandled {
		vm.hostPromiseRejectionTracker(p, false)
	}
	vm.triggerPromiseReactions(reactions, reason, true)
}

// triggerPromiseReactions implements TriggerPromiseReactions.
func (vm *VM) triggerPromiseReactions(reactions []pendingReaction, arg Value, rejected bool) {
	for i := range reactions {
		vm.enqueueReactionJob(&reactions[i], arg, rejected)
	}
}

// PerformPromiseThen implements PerformPromiseThen for the promise p. c is nil when
// the result capability is undefined, and the result is then undefined.
func (vm *VM) PerformPromiseThen(p Value, onFulfilled, onRejected Value, c *PromiseCapability) Value {
	r := pendingReaction{
		action: reactionAction{
			kind:       reactionHandlers,
			capability: c,
		},
		continuationData: vm.continuationData,
	}
	if onFulfilled.IsCallable() {
		r.onFulfilled = onFulfilled.o
	}
	if onRejected.IsCallable() {
		r.onRejected = onRejected.o
	}
	vm.addReaction(p.o, &r)
	if c == nil {
		return Undefined
	}
	return Value{o: c.promise}
}

// addReaction is the part of PerformPromiseThen after the reaction records are created.
func (vm *VM) addReaction(p *Object, r *pendingReaction) {
	d := promiseOf(p)
	switch d.state {
	case PromisePending:
		d.reactions = append(d.reactions, *r)
	case PromiseFulfilled:
		vm.enqueueReactionJob(r, d.result, false)
	case PromiseRejected:
		if !d.isHandled {
			vm.hostPromiseRejectionTracker(p, true)
		}
		vm.enqueueReactionJob(r, d.result, true)
	}
	d.isHandled = true
}

func (vm *VM) addNativeReaction(p *Object, handler AwaitHandler) {
	vm.addReaction(p, &pendingReaction{
		action: reactionAction{
			kind:   reactionNative,
			native: handler,
		},
		continuationData: vm.continuationData,
		builtinRealm:     vm.curRealm,
	})
}

// enqueueReactionJob implements NewPromiseReactionJob and HostEnqueuePromiseJob.
func (vm *VM) enqueueReactionJob(r *pendingReaction, arg Value, rejected bool) {
	handler, other := r.onFulfilled, r.onRejected
	if rejected {
		handler, other = other, handler
	}
	jobRealm := r.builtinRealm
	if handler != nil {
		jobRealm = vm.promiseJobCallbackRealm(handler)
	}
	vm.enqueuePromiseJob(&job{
		kind: jobReaction,
		context: jobContext{
			continuationData: r.continuationData,
			enteredRealm:     jobRealm,
		},
		reaction: r.action,
		rejected: rejected,
		argument: arg,
		callback: handler,
	}, other)
}

// PromiseResolve implements PromiseResolve for the constructor c.
func (vm *VM) PromiseResolve(c *Object, x Value) Value {
	if IsPromise(x) {
		if ctor := vm.GetV(x, vm.Key(KeyConstructor)); ctor.o == c {
			return x
		}
	}
	pc := vm.NewPromiseCapability(Value{o: c})
	vm.ResolveCapability(pc, x)
	return Value{o: pc.promise}
}

// Await implements Await for a built-in async function: k continues the function in
// a later job with the value that v settles to. An exception that PromiseResolve throws
// propagates, and k is not called then.
func (vm *VM) Await(v Value, k AwaitHandler) {
	vm.awaitValue(v, reactionAction{
		kind:   reactionNative,
		native: k,
	})
}

// tryAwait suspends g on v, or leaves the exception pending if PromiseResolve throws.
func (vm *VM) tryAwait(g *generator, v Value, kind reactionKind) bool {
	action := reactionAction{
		kind: kind,
		gen:  g,
	}
	if !v.IsObject() {
		// A primitive only queues the fulfill handler created in the activation's realm.
		vm.enqueueReactionJob(&pendingReaction{
			action:           action,
			continuationData: vm.continuationData,
			builtinRealm:     g.f.fn.code.realm,
		}, v, false)
		return true
	}
	return vm.protect(func() {
		if vm.trackJobRealms && g.f.fn.code.realm != vm.curRealm {
			vm.InRealm(g.f.fn.code.realm, func() { vm.awaitValue(v, action) })
			return
		}
		vm.awaitValue(v, action)
	})
}

// awaitValue performs PromiseResolve and registers the implicit handlers for an await.
func (vm *VM) awaitValue(v Value, action reactionAction) {
	var p *Object
	var rejected bool
	switch {
	case IsPromise(v):
		if ctor := vm.GetV(v, vm.Key(KeyConstructor)); ctor.o == vm.CurrentRealm().Intrinsic(IntrinsicPromise) {
			p = v.o
		} else {
			p = vm.NewPromiseObject(vm.CurrentRealm().Intrinsic(IntrinsicPromisePrototype))
			vm.resolvePromise(vm.CurrentRealm(), p, v)
		}
	case v.IsObject():
		var then Value
		if e, ok := vm.Completion(func() {
			then = vm.GetV(v, vm.Key(keyThen))
		}); !ok {
			v, rejected = e, true
		} else if then.IsCallable() {
			p = vm.NewPromiseObject(vm.CurrentRealm().Intrinsic(IntrinsicPromisePrototype))
			vm.enqueueThenableJob(p, v, then)
		}
	}
	r := pendingReaction{
		action:           action,
		continuationData: vm.continuationData,
		builtinRealm:     vm.curRealm,
	}
	if p != nil {
		vm.addReaction(p, &r)
		return
	}
	// PromiseResolve would have settled immediately with v.
	vm.enqueueReactionJob(&r, v, rejected)
}

// hostPromiseRejectionTracker implements HostPromiseRejectionTracker. handle selects
// the "handle" operation, and the "reject" operation otherwise.
//
// The promises that are rejected without a handler when the job queue is drained are
// the unhandled rejections, which notifyRejectedPromises reports.
func (vm *VM) hostPromiseRejectionTracker(p *Object, handle bool) {
	if !handle {
		vm.rejections.add(p)
		return
	}
	vm.rejections.remove(p)
}

// UnhandledRejectionFunc is the type of the function that is told about a promise that was
// rejected with reason and has no handler.
type UnhandledRejectionFunc func(vm *VM, promise, reason Value)

// SetHostUnhandledRejection sets f as the function that is told about the unhandled
// rejections, in the order of rejection, after the jobs ran. f may call into vm. Without
// one, the unhandled rejections are discarded.
func (vm *VM) SetHostUnhandledRejection(f UnhandledRejectionFunc) {
	vm.rejections.notify = f
}

// DeferUnhandledRejections postpones rejection reports across job checkpoints.
func (vm *VM) DeferUnhandledRejections() {
	vm.rejections.deferred = true
}

// TakeUnhandledRejection ends deferral, returning the first unhandled reason and clearing all reports.
func (vm *VM) TakeUnhandledRejection() (Value, bool) {
	t := &vm.rejections
	t.deferred = false
	if len(t.promises) == 0 {
		return Undefined, false
	}
	reason := promiseOf(t.promises[0]).result
	t.reset()
	return reason, true
}

// rejectionTracker holds the rejected promises without a handler.
type rejectionTracker struct {
	// promises are the rejected promises without a handler, in the order of rejection.
	promises []*Object

	// notify is the function that is told about the unhandled rejections, or nil.
	notify UnhandledRejectionFunc

	// deferred keeps pending rejections across job checkpoints.
	deferred bool
}

func (t *rejectionTracker) add(p *Object) {
	t.promises = append(t.promises, p)
}

// remove removes p, if it is there.
func (t *rejectionTracker) remove(p *Object) {
	for i, q := range t.promises {
		if q == p {
			last := len(t.promises) - 1
			copy(t.promises[i:], t.promises[i+1:])
			t.promises[last] = nil
			t.promises = t.promises[:last]
			return
		}
	}
}

// reset removes all the promises.
func (t *rejectionTracker) reset() {
	clear(t.promises)
	t.promises = t.promises[:0]
}

// maxIdleJobs is the number of jobs that the storage of the job queue keeps room for when the
// queue drains.
const maxIdleJobs = 1024

// jobQueue is a queue of pending microtasks.
type jobQueue struct {
	// jobs holds the pending jobs from head on.
	jobs []job
	head int
}

func (q *jobQueue) isEmpty() bool {
	return q.head >= len(q.jobs)
}

func (q *jobQueue) enqueue(j job) {
	q.jobs = append(q.jobs, j)
}

// dequeue removes and returns the oldest job. The queue must not be empty.
func (q *jobQueue) dequeue() job {
	j := q.jobs[q.head]
	q.jobs[q.head] = job{}
	q.head++
	if q.isEmpty() {
		if cap(q.jobs) > maxIdleJobs {
			q.jobs = nil
		} else {
			q.jobs = q.jobs[:0]
		}
		q.head = 0
	} else if q.head >= maxIdleJobs && q.head*2 >= len(q.jobs) {
		// Drop the jobs that ran, so that a queue that is never empty does not grow.
		n := len(q.jobs) - q.head
		if n < cap(q.jobs)/4 {
			jobs := make([]job, n, 2*n)
			copy(jobs, q.jobs[q.head:])
			q.jobs = jobs
		} else {
			copy(q.jobs, q.jobs[q.head:])
			clear(q.jobs[n:])
			q.jobs = q.jobs[:n]
		}
		q.head = 0
	}
	return j
}

// promiseJobCallbackRealm implements the GetFunctionRealm step of both promise job constructors.
func (vm *VM) promiseJobCallbackRealm(callback *Object) *Realm {
	if vm.trackJobRealms {
		if r := vm.functionRealmOrNil(callback); r != nil {
			return r
		}
		// A revoked proxy gets its TypeError from the realm creating the job.
	}
	return vm.curRealm
}

func (vm *VM) enqueuePromiseJob(j *job, otherHandler *Object) {
	if j.context.enteredRealm == nil {
		// An empty handler leaves the realm to the host. Use the other handler's
		// microtask queue when available, falling back to the enqueueing realm.
		j.context.enteredRealm = vm.curRealm
		if vm.trackJobRealms && otherHandler != nil {
			j.context.enteredRealm = vm.promiseJobCallbackRealm(otherHandler)
		}
	}
	if vm.trackJobRealms && vm.hasDetachedGlobal(j.context.enteredRealm) {
		return
	}
	vm.jobs.enqueue(*j)
}

// QueueMicrotask queues a call to callback with no arguments and reports an exception
// in the callback's creation realm.
// It throws a TypeError if callback is not callable.
func (vm *VM) QueueMicrotask(callback Value) {
	if !callback.IsCallable() {
		vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(callback)+" is not a function")
	}
	if vm.hasDetachedGlobal(vm.curRealm) {
		return
	}
	vm.jobs.enqueue(job{
		kind: jobCallback,
		context: jobContext{
			continuationData: vm.continuationData,
			enteredRealm:     callback.o.CreationRealm(),
		},
		callback: callback.o,
	})
}

func (vm *VM) runJob(j *job) {
	switch j.kind {
	case jobCallback:
		if thrown, ok := vm.Completion(func() {
			vm.Call(j.callback, Undefined)
		}); !ok {
			vm.reportUncaughtException(thrown, vm.thrownAt)
		}
		return
	case jobResolve:
		vm.resolvePromise(j.context.enteredRealm, j.promise, j.argument)
		return
	case jobThenable:
		vm.protect(func() {
			vm.promiseResolveThenableJob(j.promise, j.argument, j.callback)
		})
		vm.thrown = Undefined
		return
	}
	r := &j.reaction
	if r.gen != nil {
		defer r.gen.abortOnPanic()
	}
	switch r.kind {
	case reactionAwait:
		mode := ResumeNext
		if j.rejected {
			mode = ResumeThrow
		}
		vm.asyncResume(r.gen, mode, j.argument)
		return
	case reactionAwaitReturn:
		mode := ResumeReturn
		if j.rejected {
			mode = ResumeThrow
		}
		vm.asyncResume(r.gen, mode, j.argument)
		return
	case reactionAsyncGeneratorReturn:
		g := r.gen
		vm.asyncGeneratorCompleteStep(g, j.context.enteredRealm, j.argument, j.rejected, true)
		vm.asyncGeneratorDrainQueue(g)
		return
	}
	vm.protect(func() {
		vm.promiseReactionJob(j)
	})
	vm.thrown = Undefined
}

// promiseReactionJob is the job of NewPromiseReactionJob.
func (vm *VM) promiseReactionJob(j *job) {
	r, arg, rejected := &j.reaction, j.argument, j.rejected
	switch r.kind {
	case reactionNative:
		r.native(vm, arg, rejected)
		return
	case reactionUnwrap:
		if !rejected {
			vm.ResolveCapability(r.capability, j.context.enteredRealm.CreateIterResultObject(arg, r.done))
			return
		}
		if r.iterator != nil {
			vm.iteratorClose(Value{o: r.iterator}, true)
		}
		vm.RejectCapability(r.capability, arg)
		return
	}
	result := arg
	abrupt := rejected
	if j.callback != nil {
		abrupt = !vm.protect(func() {
			result = vm.Call(j.callback, Undefined, arg)
		})
		if abrupt {
			result = vm.thrown
			vm.thrown = Undefined
		}
	}
	if r.capability == nil {
		return
	}
	if abrupt {
		vm.RejectCapability(r.capability, result)
	} else {
		vm.ResolveCapability(r.capability, result)
	}
}

// promiseResolveThenableJob is the job of NewPromiseResolveThenableJob.
func (vm *VM) promiseResolveThenableJob(p *Object, thenable Value, then *Object) {
	c := vm.CreateResolvingFunctions(p)
	if !vm.protect(func() {
		vm.Call(then, thenable, Value{o: c.resolve}, Value{o: c.reject})
	}) {
		vm.RejectCapability(c, vm.takeThrown())
	}
}

// runJobs runs the pending jobs, the timeout jobs of the expired waiters and the resolve jobs of
// the waiters that other agents notified until none is left, and then the cleanup callbacks of
// FinalizationRegistry objects. It does nothing unless it is called by the outermost call
// into the engine, as jobs only run when no other JavaScript is running.
func (vm *VM) runJobs() {
	if vm.entered != 1 || vm.abortCause != nil {
		return
	}
	for {
		for {
			for !vm.jobs.isEmpty() {
				if vm.interrupt.Load() {
					vm.Poll()
				}
				j := vm.jobs.dequeue()
				// Detachment drops microtasks even if they were already queued.
				if !vm.trackJobRealms || j.kind == jobResolve || !vm.hasDetachedGlobal(j.context.enteredRealm) {
					vm.withJobContext(j.context, func() {
						vm.runJob(&j)
					})
				}
				if vm.abortCause != nil {
					return
				}
				vm.clearKeptObjects()
				// A timeout job runs as soon as its time has passed, even while promise jobs keep
				// queuing more.
				if !vm.timedWaiters.isEmpty() {
					vm.expireWaiters()
				}
			}
			received := vm.receiveNotifications()
			if !vm.expireWaiters() && !received {
				break
			}
		}
		// The reports can queue jobs and reject promises.
		if vm.notifyRejectedPromises() {
			if vm.abortCause != nil {
				return
			}
			continue
		}
		if vm.abortCause != nil || !vm.runFinalizationCleanups() {
			break
		}
	}
}

// notifyRejectedPromises reports the unhandled rejections and removes them, the way the
// HTML standard notifies about rejected promises. It reports whether there were any to
// report.
func (vm *VM) notifyRejectedPromises() bool {
	t := &vm.rejections
	if len(t.promises) == 0 || t.deferred {
		return false
	}
	if t.notify == nil {
		t.reset()
		return false
	}
	// Promises rejected by the reports are added to a new list, and reported next.
	promises := t.promises
	t.promises = nil
	for _, p := range promises {
		// An earlier report can handle p.
		d := promiseOf(p)
		if d.isHandled {
			continue
		}
		// An earlier report can also remove the function.
		if t.notify == nil {
			break
		}
		t.notify(vm, Value{o: p}, d.result)
		vm.ResumeInterruption()
		if vm.abortCause != nil {
			break
		}
	}
	return true
}

// dropJobs discards the pending jobs.
func (vm *VM) dropJobs() {
	vm.jobs = jobQueue{}
	vm.rejections.reset()
}
