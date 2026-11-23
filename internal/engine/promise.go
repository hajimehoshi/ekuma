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
	reactions []promiseReaction
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
	// as AsyncFromSyncIteratorContinuation does. A rejection first closes onRejected when it is
	// an object, which is the iterator that the reject handler of AsyncFromSyncIteratorContinuation
	// closes.
	reactionUnwrap

	// reactionNative calls native.
	reactionNative
)

// promiseReaction is a PromiseReaction Record, or the equivalent of one whose handler is
// an abstract closure of the specification that is not observable as a function.
type promiseReaction struct {
	kind reactionKind
	done bool

	// capability is the [[Capability]], or nil when it is undefined.
	capability *PromiseCapability

	// onFulfilled and onRejected are the handlers, whose o is nil when the handler is empty.
	onFulfilled, onRejected Value

	gen    *generator
	native AwaitHandler

	context jobContext
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
	enteredRealm *realm
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
		vm.InRealm((*Realm)(c.enteredRealm), f)
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

	// rejected selects the reject reaction of a reaction job.
	rejected bool

	// reaction holds the reaction of a reaction job and the context of any job.
	reaction promiseReaction

	// argument is the value of a reaction job, or the thenable of a thenable job.
	argument Value

	// promise is the promise to resolve in a thenable or resolve job.
	promise *Object

	// callback is the then method of a thenable job or the function of a callback job.
	callback Value
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
		promise:   vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype)),
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
			vm.resolvePromise(c.promise, argAt(args, 0))
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
		vm.ThrowTypeError(vm.DisplayString(c) + " is not a constructor")
	}
	if c.o == vm.Intrinsic(IntrinsicPromise) {
		// Constructing %Promise% with an executor observes nothing that this does not.
		return vm.newIntrinsicCapability()
	}
	var resolve, reject Value
	resolve, reject = Undefined, Undefined
	executor := vm.NewNative("", 2, func(vm *VM, this Value, args []Value) Value {
		if !resolve.IsUndefined() {
			vm.ThrowTypeError("promise capability resolve function is already set")
		}
		if !reject.IsUndefined() {
			vm.ThrowTypeError("promise capability reject function is already set")
		}
		resolve, reject = argAt(args, 0), argAt(args, 1)
		return Undefined
	})
	p := vm.Construct(c.o, []Value{{o: executor}}, c.o)
	if !resolve.IsCallable() {
		vm.ThrowTypeError("promise capability resolve function is not callable")
	}
	if !reject.IsCallable() {
		vm.ThrowTypeError("promise capability reject function is not callable")
	}
	return &PromiseCapability{
		promise: p,
		resolve: resolve.o,
		reject:  reject.o,
	}
}

// CapabilityResolve returns the [[Resolve]] function of c.
func (vm *VM) CapabilityResolve(c *PromiseCapability) Value {
	if c.resolve == nil {
		vm.createResolvingFunctions(c)
	}
	return Value{o: c.resolve}
}

// CapabilityReject returns the [[Reject]] function of c.
func (vm *VM) CapabilityReject(c *PromiseCapability) Value {
	if c.reject == nil {
		vm.createResolvingFunctions(c)
	}
	return Value{o: c.reject}
}

// ResolveCapability calls the [[Resolve]] function of c with v.
func (vm *VM) ResolveCapability(c *PromiseCapability, v Value) {
	if c.intrinsic {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.resolvePromise(c.promise, v)
		}
		return
	}
	vm.Call(c.resolve, Undefined, v)
}

// RejectCapability calls the [[Reject]] function of c with v.
func (vm *VM) RejectCapability(c *PromiseCapability, v Value) {
	if c.intrinsic {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			vm.rejectPromise(c.promise, v)
		}
		return
	}
	vm.Call(c.reject, Undefined, v)
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
func (vm *VM) resolvePromise(p *Object, resolution Value) {
	if resolution.o == p {
		vm.rejectPromise(p, vm.NewError(ErrorKindType, "chaining cycle detected for promise"))
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
	vm.enqueueMicrotask(job{
		kind: jobThenable,
		reaction: promiseReaction{
			context: vm.captureJobContext(),
		},
		argument: thenable,
		promise:  p,
		callback: then,
	})
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
func (vm *VM) triggerPromiseReactions(reactions []promiseReaction, arg Value, rejected bool) {
	for i := range reactions {
		vm.enqueueMicrotask(job{
			kind:     jobReaction,
			rejected: rejected,
			reaction: reactions[i],
			argument: arg,
		})
	}
}

// PerformPromiseThen implements PerformPromiseThen for the promise p. c is nil when
// the result capability is undefined, and the result is then undefined.
func (vm *VM) PerformPromiseThen(p Value, onFulfilled, onRejected Value, c *PromiseCapability) Value {
	r := promiseReaction{
		kind:       reactionHandlers,
		capability: c,
	}
	if onFulfilled.IsCallable() {
		r.onFulfilled = onFulfilled
	}
	if onRejected.IsCallable() {
		r.onRejected = onRejected
	}
	vm.addReaction(p.o, &r)
	if c == nil {
		return Undefined
	}
	return Value{o: c.promise}
}

// addReaction is the part of PerformPromiseThen after the reaction records are created.
// The reaction captures the host context for its job.
func (vm *VM) addReaction(p *Object, r *promiseReaction) {
	r.context = vm.captureJobContext()
	d := promiseOf(p)
	switch d.state {
	case PromisePending:
		d.reactions = append(d.reactions, *r)
	case PromiseFulfilled:
		vm.enqueueMicrotask(job{
			kind:     jobReaction,
			reaction: *r,
			argument: d.result,
		})
	case PromiseRejected:
		if !d.isHandled {
			vm.hostPromiseRejectionTracker(p, true)
		}
		vm.enqueueMicrotask(job{
			kind:     jobReaction,
			rejected: true,
			reaction: *r,
			argument: d.result,
		})
	}
	d.isHandled = true
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
	vm.awaitValue(v, &promiseReaction{
		kind:   reactionNative,
		native: k,
	})
}

// tryAwait suspends g on v, or leaves the exception pending if PromiseResolve throws.
func (vm *VM) tryAwait(g *generator, v Value, kind reactionKind) bool {
	r := promiseReaction{
		kind: kind,
		gen:  g,
	}
	if !v.IsObject() {
		// A primitive only queues the fulfill handler created in the activation's realm.
		r.context = jobContext{
			continuationData: vm.continuationData,
			enteredRealm:     g.f.fn.code.realm,
		}
		vm.enqueueMicrotask(job{
			kind:     jobReaction,
			reaction: r,
			argument: v,
		})
		return true
	}
	return vm.protect(func() {
		if vm.trackJobRealms && g.f.fn.code.realm != vm.curRealm {
			vm.InRealm((*Realm)(g.f.fn.code.realm), func() { vm.awaitValue(v, &r) })
			return
		}
		vm.awaitValue(v, &r)
	})
}

// awaitValue performs PromiseResolve(%Promise%, v) and PerformPromiseThen with the reaction r.
func (vm *VM) awaitValue(v Value, r *promiseReaction) {
	// The jobs enqueued here run r without addReaction.
	r.context = vm.captureJobContext()
	// The promise that PromiseResolve creates is only needed for a thenable: any other value
	// would settle it at once, so the reaction job can be enqueued directly.
	switch {
	case IsPromise(v):
		if ctor := vm.GetV(v, vm.Key(KeyConstructor)); ctor.o == vm.Intrinsic(IntrinsicPromise) {
			vm.addReaction(v.o, r)
			return
		}
	case !v.IsObject():
		vm.enqueueMicrotask(job{
			kind:     jobReaction,
			reaction: *r,
			argument: v,
		})
		return
	default:
		var then Value
		if e, ok := vm.Completion(func() {
			then = vm.GetV(v, vm.Key(keyThen))
		}); !ok {
			vm.enqueueMicrotask(job{
				kind:     jobReaction,
				rejected: true,
				reaction: *r,
				argument: e,
			})
			return
		}
		if !then.IsCallable() {
			vm.enqueueMicrotask(job{
				kind:     jobReaction,
				reaction: *r,
				argument: v,
			})
			return
		}
		p := vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype))
		vm.enqueueThenableJob(p, v, then)
		vm.addReaction(p, r)
		return
	}
	p := vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype))
	vm.resolvePromise(p, v)
	vm.addReaction(p, r)
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

func (vm *VM) enqueueMicrotask(j job) {
	if vm.trackJobRealms {
		j.reaction.context.enteredRealm = vm.jobRealm(&j)
		if vm.hasDetachedGlobal(j.reaction.context.enteredRealm) {
			return
		}
	}
	vm.jobs.enqueue(j)
}

// QueueMicrotask queues a call to callback with no arguments and reports an exception
// in the callback's creation realm.
// It throws a TypeError if callback is not callable.
func (vm *VM) QueueMicrotask(callback Value) {
	if !callback.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(callback) + " is not a function")
	}
	if vm.hasDetachedGlobal(vm.curRealm) {
		return
	}
	context := vm.captureJobContext()
	context.enteredRealm = (*realm)(callback.o.CreationRealm())
	vm.jobs.enqueue(job{
		kind: jobCallback,
		reaction: promiseReaction{
			context: context,
		},
		callback: callback,
	})
}

// jobRealm returns the realm entered for a job. A missing or revoked handler
// falls back to the other reaction handler and then to the enqueueing realm.
func (vm *VM) jobRealm(j *job) *realm {
	primary, secondary := j.reaction.onFulfilled, j.reaction.onRejected
	switch {
	case j.kind == jobThenable:
		primary = j.callback
	case j.kind == jobReaction && j.reaction.kind != reactionHandlers:
		return j.reaction.context.enteredRealm
	case j.rejected:
		primary, secondary = secondary, primary
	}
	for _, h := range [2]Value{primary, secondary} {
		if h.IsCallable() {
			if r := vm.functionRealmOrNil(h.o); r != nil {
				return r
			}
		}
	}
	return vm.curRealm
}

func (vm *VM) runJob(j *job) {
	switch j.kind {
	case jobCallback:
		if thrown, ok := vm.Completion(func() {
			vm.Call(j.callback.o, Undefined)
		}); !ok {
			vm.reportUncaughtException(thrown, vm.thrownAt)
		}
		return
	case jobResolve:
		vm.resolvePromise(j.promise, j.argument)
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
		vm.asyncGeneratorCompleteStep(g, j.argument, j.rejected, true)
		vm.asyncGeneratorDrainQueue(g)
		return
	}
	vm.protect(func() {
		vm.promiseReactionJob(r, j.argument, j.rejected)
	})
	vm.thrown = Undefined
}

// promiseReactionJob is the job of NewPromiseReactionJob.
func (vm *VM) promiseReactionJob(r *promiseReaction, arg Value, rejected bool) {
	switch r.kind {
	case reactionNative:
		r.native(vm, arg, rejected)
		return
	case reactionUnwrap:
		if !rejected {
			vm.ResolveCapability(r.capability, vm.CreateIterResultObject(arg, r.done))
			return
		}
		if r.onRejected.IsObject() {
			vm.iteratorClose(r.onRejected, true)
		}
		vm.RejectCapability(r.capability, arg)
		return
	}
	handler := r.onFulfilled
	if rejected {
		handler = r.onRejected
	}
	result := arg
	abrupt := rejected
	if handler.o != nil {
		abrupt = !vm.protect(func() {
			result = vm.Call(handler.o, Undefined, arg)
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
func (vm *VM) promiseResolveThenableJob(p *Object, thenable, then Value) {
	c := vm.CreateResolvingFunctions(p)
	if !vm.protect(func() {
		vm.Call(then.o, thenable, Value{o: c.resolve}, Value{o: c.reject})
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
				if !vm.trackJobRealms || j.kind == jobResolve || !vm.hasDetachedGlobal(j.reaction.context.enteredRealm) {
					vm.withJobContext(j.reaction.context, func() {
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
