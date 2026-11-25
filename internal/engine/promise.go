// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

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
	result storage.Value

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
	onFulfilled, onRejected *storage.Object
	continuationData        any

	// builtinContext binds an implicit built-in handler at registration.
	builtinContext *ExecutionContext
}

// reactionAction describes what the selected reaction does with its argument.
type reactionAction struct {
	kind reactionKind
	done bool

	// capability is the [[Capability]], or nil when it is undefined.
	capability *PromiseCapability
	gen        *generator
	native     AwaitHandler
	iterator   *storage.Object
}

// AwaitHandler continues a built-in async function after an Await: v is the value of
// the awaited promise, or its rejection reason when rejected is set.
type AwaitHandler func(cx *ExecutionContext, v storage.Value, rejected bool)

// PromiseCapability is a PromiseCapability Record.
type PromiseCapability struct {
	promise *storage.Object

	// resolve and reject are the [[Resolve]] and [[Reject]] functions. For a promise that
	// the agent created itself, they are nil until something asks for them.
	resolve, reject *storage.Object

	// intrinsic is set when the agent created the promise and its resolving functions are
	// the ones of CreateResolvingFunctions, which then share alreadyResolved.
	intrinsic       bool
	alreadyResolved bool
}

// Promise returns the [[Promise]] of c.
func (c *PromiseCapability) Promise() storage.Value {
	return storage.Value{O: c.promise}
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

	context          *ExecutionContext
	continuationData any
	reaction         reactionAction

	// argument is the settlement value, rejection reason or thenable.
	argument storage.Value

	// promise is the promise to resolve in a thenable or resolve job.
	promise *storage.Object

	// callback is the selected reaction handler, then method or host callback.
	callback *storage.Object
}

// IsPromise implements IsPromise.
func IsPromise(v storage.Value) bool {
	return v.IsObject() && v.O.Class == storage.ClassPromise
}

// PromiseStateOf returns the [[PromiseState]] and the [[PromiseResult]] of the promise p.
func PromiseStateOf(p storage.Value) (PromiseState, storage.Value) {
	d := promiseOf(p.O)
	return d.state, d.result
}

func promiseOf(o *storage.Object) *promiseData {
	return o.ExtState().(*promiseData)
}

// NewPromiseObject returns a pending Promise object with the prototype proto.
func (cx *ExecutionContext) NewPromiseObject(proto *storage.Object) *storage.Object {
	return newPromiseObject(cx.ShapeFor(proto))
}

func newPromiseObject(s *storage.Shape) *storage.Object {
	w := &promiseObject{
		state: promiseData{
			result: storage.Undefined,
		},
	}
	return w.InitWithState(s, storage.ClassPromise, &w.state)
}

// promiseObject is a Promise object together with its state, so that creating one is a single
// allocation.
type promiseObject struct {
	propertyObject
	state promiseData
}

// newIntrinsicCapability returns the capability of a new promise of %Promise%,
// as NewPromiseCapability(%Promise%) does.
func (cx *ExecutionContext) newIntrinsicCapability() *PromiseCapability {
	proto := cx.Intrinsic(storage.IntrinsicPromisePrototype)
	return &PromiseCapability{
		promise:   newPromiseObject(cx.ShapeForRealm(proto, cx.realmPart)),
		intrinsic: true,
	}
}

// CreateResolvingFunctions implements CreateResolvingFunctions for the promise p.
// The result is a capability whose resolving functions are those functions.
func (cx *ExecutionContext) CreateResolvingFunctions(p *storage.Object) *PromiseCapability {
	c := &PromiseCapability{
		promise:   p,
		intrinsic: true,
	}
	cx.createResolvingFunctions(c)
	return c
}

func (cx *ExecutionContext) createResolvingFunctions(c *PromiseCapability) {
	c.resolve = cx.NewNativeWithState("", 1, nativePromiseResolveFunction, c)
	c.reject = cx.NewNativeWithState("", 1, nativePromiseRejectFunction, c)
}

// promiseResolveFunction is the code of the promise resolve functions. Their state is a
// *PromiseCapability.
func promiseResolveFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	c := cx.ActiveNativeState().(*PromiseCapability)
	if !c.alreadyResolved {
		c.alreadyResolved = true
		cx.resolvePromise(c.resolvingRealm(), c.promise, argAt(args, 0))
	}
	return storage.Undefined
}

// promiseRejectFunction is the code of the promise reject functions. Their state is a
// *PromiseCapability.
func promiseRejectFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	c := cx.ActiveNativeState().(*PromiseCapability)
	if !c.alreadyResolved {
		c.alreadyResolved = true
		cx.rejectPromise(c.promise, argAt(args, 0))
	}
	return storage.Undefined
}

func argAt(args []storage.Value, i int) storage.Value {
	if i < len(args) {
		return args[i]
	}
	return storage.Undefined
}

// NewPromiseCapability implements NewPromiseCapability.
func (cx *ExecutionContext) NewPromiseCapability(c storage.Value) *PromiseCapability {
	if !c.IsObject() || !cx.IsConstructor(c.O) {
		cx.ThrowTypeError(cx.DisplayString(c) + " is not a constructor")
	}
	if c.O == cx.Intrinsic(storage.IntrinsicPromise) {
		// Constructing %Promise% with an executor observes nothing that this does not.
		return cx.newIntrinsicCapability()
	}
	fs := &resolvingFunctions{
		resolve: storage.Undefined,
		reject:  storage.Undefined,
	}
	executor := cx.NewNativeWithState("", 2, nativeCapabilityExecutor, fs)
	p := cx.Construct(c.O, []storage.Value{{O: executor}}, c.O)
	if !fs.resolve.IsCallable() {
		cx.ThrowTypeError("promise capability resolve function is not callable")
	}
	if !fs.reject.IsCallable() {
		cx.ThrowTypeError("promise capability reject function is not callable")
	}
	return &PromiseCapability{
		promise: p,
		resolve: fs.resolve.O,
		reject:  fs.reject.O,
	}
}

// resolvingFunctions is the Record that NewPromiseCapability collects the resolving functions in.
type resolvingFunctions struct {
	resolve, reject storage.Value
}

// capabilityExecutor is the code of the executors of NewPromiseCapability. Their state is a
// *resolvingFunctions.
func capabilityExecutor(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	fs := cx.ActiveNativeState().(*resolvingFunctions)
	if !fs.resolve.IsUndefined() {
		cx.ThrowTypeError("promise capability resolve function is already set")
	}
	if !fs.reject.IsUndefined() {
		cx.ThrowTypeError("promise capability reject function is already set")
	}
	fs.resolve, fs.reject = argAt(args, 0), argAt(args, 1)
	return storage.Undefined
}

// ensureResolvingFunctions materializes the deferred functions of an intrinsic capability.
func (ag *agent) ensureResolvingFunctions(c *PromiseCapability) {
	if c.resolve == nil {
		CreationContext(c.promise).createResolvingFunctions(c)
	}
}

// CapabilityResolve returns the [[Resolve]] function of c.
func (ag *agent) CapabilityResolve(c *PromiseCapability) storage.Value {
	ag.ensureResolvingFunctions(c)
	return storage.Value{O: c.resolve}
}

// CapabilityReject returns the [[Reject]] function of c.
func (ag *agent) CapabilityReject(c *PromiseCapability) storage.Value {
	ag.ensureResolvingFunctions(c)
	return storage.Value{O: c.reject}
}

// resolvingRealm returns the realm of an intrinsic capability's resolving functions.
func (c *PromiseCapability) resolvingRealm() *storage.RealmPart {
	if c.resolve != nil {
		// A thenable job creates its own resolving functions in the job's realm.
		return c.resolve.Fn.Info.Realm
	}
	return c.promise.CreationRealm()
}

// canSettleDirectly reports whether the intrinsic resolving functions run in the current realm.
func (c *PromiseCapability) canSettleDirectly(cx *ExecutionContext) bool {
	return c.intrinsic && (!cx.trackJobRealms || c.resolvingRealm() == cx.realmPart)
}

// ResolveCapability calls the [[Resolve]] function of c with v.
func (cx *ExecutionContext) ResolveCapability(c *PromiseCapability, v storage.Value) {
	if c.canSettleDirectly(cx) {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			cx.resolvePromise(c.resolvingRealm(), c.promise, v)
		}
		return
	}
	cx.Call(cx.CapabilityResolve(c).O, storage.Undefined, v)
}

// CallCapabilityResolve calls the [[Resolve]] function of c with v, as [ExecutionContext.ResolveCapability]
// does, and shows the frame of the function where the call can be observed.
func (cx *ExecutionContext) CallCapabilityResolve(c *PromiseCapability, v storage.Value) {
	if !cx.quietJobs().tryCallCapabilityResolve(c, v) {
		cx.Call(cx.CapabilityResolve(c).O, storage.Undefined, v)
	}
}

func (q quietJobs) tryCallCapabilityResolve(c *PromiseCapability, v storage.Value) bool {
	if !c.canSettleDirectly(q.cx) {
		return false
	}
	if c.alreadyResolved {
		return true
	}
	then, ok := q.cx.Quiet().ResolutionThen(c.promise, v)
	if !ok {
		return false
	}
	c.alreadyResolved = true
	q.cx.resolvePromiseWithThen(c.promise, v, then)
	return true
}

// RejectCapability calls the [[Reject]] function of c with v.
func (cx *ExecutionContext) RejectCapability(c *PromiseCapability, v storage.Value) {
	if c.canSettleDirectly(cx) {
		if !c.alreadyResolved {
			c.alreadyResolved = true
			cx.rejectPromise(c.promise, v)
		}
		return
	}
	cx.Call(cx.CapabilityReject(c).O, storage.Undefined, v)
}

// IfAbruptRejectPromise implements IfAbruptRejectPromise for what f does: when f throws,
// it rejects c with the exception and reports false.
func (cx *ExecutionContext) IfAbruptRejectPromise(c *PromiseCapability, f func()) bool {
	if cx.protect(f) {
		return true
	}
	e := cx.thrown
	cx.thrown = storage.Undefined
	cx.RejectCapability(c, e)
	return false
}

// resolvePromise is what a promise resolve function does with resolution once it was not
// already resolved: the promise resolution procedure.
func (cx *ExecutionContext) resolvePromise(resolverRealm *storage.RealmPart, p *storage.Object, resolution storage.Value) {
	if resolution.O == p {
		cx.rejectPromise(p, executionContextOf(resolverRealm).NewError(ErrorKindType, "chaining cycle detected for promise"))
		return
	}
	if !resolution.IsObject() {
		cx.fulfillPromise(p, resolution)
		return
	}
	var then storage.Value
	if !cx.protect(func() {
		then = cx.GetV(resolution, cx.Key(storage.KeyThen))
	}) {
		e := cx.thrown
		cx.thrown = storage.Undefined
		cx.rejectPromise(p, e)
		return
	}
	cx.resolvePromiseWithThen(p, resolution, then)
}

// tryResolvePromise resolves p with v, or leaves p pending when resolution needs its caller's frame.
func (q quietJobs) tryResolvePromise(p *storage.Object, v storage.Value) bool {
	then, ok := q.cx.Quiet().ResolutionThen(p, v)
	if ok {
		q.cx.resolvePromiseWithThen(p, v, then)
	}
	return ok
}

// resolvePromiseWithThen is the part of resolvePromise after the read of then from resolution.
func (cx *ExecutionContext) resolvePromiseWithThen(p *storage.Object, resolution, then storage.Value) {
	if !then.IsCallable() {
		cx.fulfillPromise(p, resolution)
		return
	}
	cx.enqueueThenableJob(p, resolution, then)
}

// enqueueThenableJob enqueues NewPromiseResolveThenableJob with the current continuation data.
func (cx *ExecutionContext) enqueueThenableJob(p *storage.Object, thenable, then storage.Value) {
	cx.enqueuePromiseJob(job{
		kind:             jobThenable,
		context:          cx.promiseJobContext(then.O),
		continuationData: cx.continuationData,
		argument:         thenable,
		promise:          p,
		callback:         then.O,
	})
}

// fulfillPromise implements FulfillPromise.
func (cx *ExecutionContext) fulfillPromise(p *storage.Object, v storage.Value) {
	d := promiseOf(p)
	reactions := d.reactions
	d.reactions = nil
	d.result = v
	d.state = PromiseFulfilled
	cx.triggerPromiseReactions(reactions, v, false)
}

// rejectPromise implements RejectPromise.
func (cx *ExecutionContext) rejectPromise(p *storage.Object, reason storage.Value) {
	d := promiseOf(p)
	reactions := d.reactions
	d.reactions = nil
	d.result = reason
	d.state = PromiseRejected
	if !d.isHandled {
		cx.hostPromiseRejectionTracker(p, false)
	}
	cx.triggerPromiseReactions(reactions, reason, true)
}

// triggerPromiseReactions implements TriggerPromiseReactions.
func (cx *ExecutionContext) triggerPromiseReactions(reactions []pendingReaction, arg storage.Value, rejected bool) {
	for i := range reactions {
		cx.enqueueReactionJob(&reactions[i], arg, rejected)
	}
}

// PerformPromiseThen implements PerformPromiseThen for the promise p. c is nil when
// the result capability is undefined, and the result is then undefined.
func (cx *ExecutionContext) PerformPromiseThen(p storage.Value, onFulfilled, onRejected storage.Value, c *PromiseCapability) storage.Value {
	r := pendingReaction{
		action: reactionAction{
			kind:       reactionHandlers,
			capability: c,
		},
		continuationData: cx.continuationData,
	}
	if onFulfilled.IsCallable() {
		r.onFulfilled = onFulfilled.O
	}
	if onRejected.IsCallable() {
		r.onRejected = onRejected.O
	}
	cx.addReaction(p.O, &r)
	if c == nil {
		return storage.Undefined
	}
	return storage.Value{O: c.promise}
}

// addReaction is the part of PerformPromiseThen after the reaction records are created.
func (cx *ExecutionContext) addReaction(p *storage.Object, r *pendingReaction) {
	d := promiseOf(p)
	switch d.state {
	case PromisePending:
		d.reactions = append(d.reactions, *r)
	case PromiseFulfilled:
		cx.enqueueReactionJob(r, d.result, false)
	case PromiseRejected:
		if !d.isHandled {
			cx.hostPromiseRejectionTracker(p, true)
		}
		cx.enqueueReactionJob(r, d.result, true)
	}
	d.isHandled = true
}

func (cx *ExecutionContext) addNativeReaction(p *storage.Object, handler AwaitHandler) {
	cx.addReaction(p, &pendingReaction{
		action: reactionAction{
			kind:   reactionNative,
			native: handler,
		},
		continuationData: cx.continuationData,
		builtinContext:   cx,
	})
}

// enqueueReactionJob implements NewPromiseReactionJob and HostEnqueuePromiseJob.
func (cx *ExecutionContext) enqueueReactionJob(r *pendingReaction, arg storage.Value, rejected bool) {
	handler, other := r.onFulfilled, r.onRejected
	if rejected {
		handler, other = other, handler
	}
	jobContext := r.builtinContext
	if r.action.kind == reactionHandlers {
		// The host uses the other handler's realm for an absent handler,
		// falling back to the enqueueing realm when both are absent.
		callback := handler
		if callback == nil {
			callback = other
		}
		jobContext = cx.promiseJobContext(callback)
	}
	cx.enqueuePromiseJob(job{
		kind:             jobReaction,
		context:          jobContext,
		continuationData: r.continuationData,
		reaction:         r.action,
		rejected:         rejected,
		argument:         arg,
		callback:         handler,
	})
}

// PromiseResolve implements PromiseResolve for the constructor c.
func (cx *ExecutionContext) PromiseResolve(c *storage.Object, x storage.Value) storage.Value {
	if IsPromise(x) {
		if ctor := cx.GetV(x, cx.Key(storage.KeyConstructor)); ctor.O == c {
			return x
		}
	}
	pc := cx.NewPromiseCapability(storage.Value{O: c})
	cx.ResolveCapability(pc, x)
	return storage.Value{O: pc.promise}
}

// Await implements Await for a built-in async function: k continues the function in
// a later job with the value that v settles to. An exception that PromiseResolve throws
// propagates, and k is not called then.
func (cx *ExecutionContext) Await(v storage.Value, k AwaitHandler) {
	cx.awaitValue(v, reactionAction{
		kind:   reactionNative,
		native: k,
	})
}

// tryAwait suspends g, whose context is cx, on v, or leaves the exception pending if PromiseResolve
// throws.
func (cx *ExecutionContext) tryAwait(g *generator, v storage.Value, kind reactionKind) bool {
	action := reactionAction{
		kind: kind,
		gen:  g,
	}
	if cx.quietJobs().tryAwait(v, action) {
		return true
	}
	if kind != reactionAwait {
		return cx.protect(func() {
			cx.awaitValue(v, action)
		})
	}
	// The code that the read runs sees the function at its await.
	var ok bool
	g.underFrame(func() {
		ok = cx.protect(func() {
			cx.awaitValue(v, action)
		})
	})
	return ok
}

// tryAwait registers action for v, or does nothing and returns false when a property read can run code.
func (q quietJobs) tryAwait(v storage.Value, action reactionAction) bool {
	cx := q.cx
	r := pendingReaction{
		action:           action,
		continuationData: cx.continuationData,
		builtinContext:   cx,
	}
	if IsPromise(v) {
		ctor := cx.LookupDataSlot(v.O, cx.Key(storage.KeyConstructor))
		if ctor.Holder == nil {
			return false
		}
		if ctor.Value().O == cx.Intrinsic(storage.IntrinsicPromise) {
			cx.addReaction(v.O, &r)
			return true
		}
	}
	then, ok := cx.Quiet().PromiseThen(v)
	if !ok {
		return false
	}
	if then.IsCallable() {
		p := cx.NewPromiseObject(cx.Intrinsic(storage.IntrinsicPromisePrototype))
		cx.enqueueThenableJob(p, v, then)
		cx.addReaction(p, &r)
	} else {
		// PromiseResolve would immediately fulfill with v.
		cx.enqueueReactionJob(&r, v, false)
	}
	return true
}

// awaitValue performs PromiseResolve and registers the implicit handlers for an await.
func (cx *ExecutionContext) awaitValue(v storage.Value, action reactionAction) {
	var p *storage.Object
	var rejected bool
	switch {
	case IsPromise(v):
		if ctor := cx.GetV(v, cx.Key(storage.KeyConstructor)); ctor.O == cx.Intrinsic(storage.IntrinsicPromise) {
			p = v.O
		} else {
			p = cx.NewPromiseObject(cx.Intrinsic(storage.IntrinsicPromisePrototype))
			cx.resolvePromise(cx.realmPart, p, v)
		}
	case v.IsObject():
		var then storage.Value
		if e, ok := cx.Completion(func() {
			then = cx.GetV(v, cx.Key(storage.KeyThen))
		}); !ok {
			v, rejected = e, true
		} else if then.IsCallable() {
			p = cx.NewPromiseObject(cx.Intrinsic(storage.IntrinsicPromisePrototype))
			cx.enqueueThenableJob(p, v, then)
		}
	}
	r := pendingReaction{
		action:           action,
		continuationData: cx.continuationData,
		builtinContext:   cx,
	}
	if p != nil {
		cx.addReaction(p, &r)
		return
	}
	// PromiseResolve would have settled immediately with v.
	cx.enqueueReactionJob(&r, v, rejected)
}

// hostPromiseRejectionTracker implements HostPromiseRejectionTracker. handle selects
// the "handle" operation, and the "reject" operation otherwise.
//
// The promises that are rejected without a handler when the job queue is drained are
// the unhandled rejections, which notifyRejectedPromises reports.
func (ag *agent) hostPromiseRejectionTracker(p *storage.Object, handle bool) {
	if !handle {
		ag.rejections.add(p)
		return
	}
	ag.rejections.remove(p)
}

// UnhandledRejectionFunc is the type of the function that is told about a promise that was
// rejected with reason and has no handler.
type UnhandledRejectionFunc func(cx *ExecutionContext, promise, reason storage.Value)

// SetHostUnhandledRejection sets f as the function that is told about the unhandled
// rejections, in the order of rejection, after the jobs ran. f may call into ag. Without
// one, the unhandled rejections are discarded.
func (ag *agent) SetHostUnhandledRejection(f UnhandledRejectionFunc) {
	ag.rejections.setNotify(f)
}

// DeferUnhandledRejections postpones rejection reports across job checkpoints.
func (ag *agent) DeferUnhandledRejections() {
	ag.rejections.deferReports()
}

// TakeUnhandledRejection ends deferral, returning the first unhandled reason and clearing all reports.
func (ag *agent) TakeUnhandledRejection() (storage.Value, bool) {
	p := ag.rejections.endDeferral()
	if p == nil {
		return storage.Undefined, false
	}
	return promiseOf(p).result, true
}

// rejectionTracker holds the rejected promises without a handler.
type rejectionTracker struct {
	// promises are the rejected promises without a handler, in the order of rejection.
	promises []*storage.Object

	// notify is the function that is told about the unhandled rejections, or nil.
	notify UnhandledRejectionFunc

	// deferred keeps pending rejections across job checkpoints.
	deferred bool
}

func (t *rejectionTracker) setNotify(f UnhandledRejectionFunc) {
	t.notify = f
}

func (t *rejectionTracker) add(p *storage.Object) {
	t.promises = append(t.promises, p)
}

// remove removes p, if it is there.
func (t *rejectionTracker) remove(p *storage.Object) {
	for i, q := range t.promises {
		if q == p {
			copy(t.promises[i:], t.promises[i+1:])
			t.promises = storage.WithoutLast(t.promises)
			return
		}
	}
}

// reset removes all the promises.
func (t *rejectionTracker) reset() {
	t.promises = storage.Truncated(t.promises, 0)
}

// deferReports keeps the promises from takeForReport until endDeferral.
func (t *rejectionTracker) deferReports() {
	t.deferred = true
}

// endDeferral ends the deferral of the reports and removes all the promises. It returns the first
// one, or nil when there is none.
func (t *rejectionTracker) endDeferral() *storage.Object {
	t.deferred = false
	if len(t.promises) == 0 {
		return nil
	}
	p := t.promises[0]
	t.reset()
	return p
}

// takeForReport removes and returns the promises to report, in the order of rejection. There is
// none while the reports are deferred. Without a function to tell, the promises are discarded.
func (t *rejectionTracker) takeForReport() []*storage.Object {
	if len(t.promises) == 0 || t.deferred {
		return nil
	}
	if t.notify == nil {
		t.reset()
		return nil
	}
	promises := t.promises
	t.promises = nil
	return promises
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

// promiseJobContext implements the GetFunctionRealm step of both promise job constructors.
func (cx *ExecutionContext) promiseJobContext(callback *storage.Object) *ExecutionContext {
	if cx.trackJobRealms && callback != nil {
		if r := cx.functionRealmOrNil(callback); r != nil {
			return executionContextOf(r)
		}
		// A revoked proxy gets its TypeError from the realm creating the job.
	}
	return cx
}

func (ag *agent) enqueuePromiseJob(j job) {
	if ag.trackJobRealms && ag.hasDetachedGlobal(j.context.realm) {
		return
	}
	ag.jobs.enqueue(j)
}

// QueueMicrotask queues a call to callback with no arguments and reports an exception
// in the callback's creation realm.
// It throws a TypeError if callback is not callable.
func (cx *ExecutionContext) QueueMicrotask(callback storage.Value) {
	if !callback.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(callback) + " is not a function")
	}
	if cx.hasDetachedGlobal(cx.realm) {
		return
	}
	cx.jobs.enqueue(job{
		kind:             jobCallback,
		context:          CreationContext(callback.O),
		continuationData: cx.continuationData,
		callback:         callback.O,
	})
}

func (j *job) run() {
	cx := j.context
	switch j.kind {
	case jobCallback:
		if thrown, ok := cx.Completion(func() {
			cx.Call(j.callback, storage.Undefined)
		}); !ok {
			cx.reportUncaughtException(thrown, cx.thrownAt)
		}
		return
	case jobResolve:
		cx.resolvePromise(cx.realmPart, j.promise, j.argument)
		return
	case jobThenable:
		cx.protect(func() {
			cx.promiseResolveThenableJob(j.promise, j.argument, j.callback)
		})
		cx.thrown = storage.Undefined
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
		cx.resumeAfterAwait(r.gen, mode, j.argument)
		return
	case reactionAwaitReturn:
		mode := ResumeReturn
		if j.rejected {
			mode = ResumeThrow
		}
		cx.resumeAfterAwait(r.gen, mode, j.argument)
		return
	case reactionAsyncGeneratorReturn:
		g := r.gen
		// A completed return resumes the handler created by the draining context.
		cx.asyncGeneratorCompleteStep(g, j.argument, j.rejected, true)
		cx.asyncGeneratorDrainQueue(g)
		return
	}
	cx.protect(func() {
		cx.promiseReactionJob(j)
	})
	cx.thrown = storage.Undefined
}

// promiseReactionJob is the job of NewPromiseReactionJob.
func (cx *ExecutionContext) promiseReactionJob(j *job) {
	r, arg, rejected := &j.reaction, j.argument, j.rejected
	switch r.kind {
	case reactionNative:
		r.native(cx, arg, rejected)
		return
	case reactionUnwrap:
		if !rejected {
			cx.ResolveCapability(r.capability, cx.CreateIterResultObject(arg, r.done))
			return
		}
		if r.iterator != nil {
			cx.iteratorClose(storage.Value{O: r.iterator}, true)
		}
		cx.RejectCapability(r.capability, arg)
		return
	}
	result := arg
	abrupt := rejected
	if j.callback != nil {
		abrupt = !cx.protect(func() {
			result = cx.Call(j.callback, storage.Undefined, arg)
		})
		if abrupt {
			result = cx.thrown
			cx.thrown = storage.Undefined
		}
	}
	if r.capability == nil {
		return
	}
	if abrupt {
		cx.RejectCapability(r.capability, result)
	} else {
		cx.ResolveCapability(r.capability, result)
	}
}

// promiseResolveThenableJob is the job of NewPromiseResolveThenableJob.
func (cx *ExecutionContext) promiseResolveThenableJob(p *storage.Object, thenable storage.Value, then *storage.Object) {
	c := cx.CreateResolvingFunctions(p)
	if !cx.protect(func() {
		cx.Call(then, thenable, storage.Value{O: c.resolve}, storage.Value{O: c.reject})
	}) {
		cx.RejectCapability(c, cx.takeThrown())
	}
}

// runJobs runs the pending jobs, the timeout jobs of the expired waiters and the resolve jobs of
// the waiters that other agents notified until none is left, and then the cleanup callbacks of
// FinalizationRegistry objects. It does nothing unless it is called by the outermost call
// into the engine, as jobs only run when no other JavaScript is running.
func (cx *ExecutionContext) runJobs() {
	if cx.entered != 1 || cx.abortCause != nil {
		return
	}
	for {
		for {
			for !cx.jobs.isEmpty() {
				if cx.InterruptFlag.Load() {
					cx.Poll()
				}
				j := cx.jobs.dequeue()
				// Detachment drops microtasks even if they were already queued.
				if !cx.trackJobRealms || j.kind == jobResolve || !cx.hasDetachedGlobal(j.context.realm) {
					j.context.enterJob(j.continuationData, j.run)
				}
				if cx.abortCause != nil {
					return
				}
				cx.ClearKeptObjects()
				// A timeout job runs as soon as its time has passed, even while promise jobs keep
				// queuing more.
				if cx.asyncWaiters.hasDeadline() {
					cx.expireWaiters()
				}
			}
			received := cx.receiveNotifications()
			if !cx.expireWaiters() && !received {
				break
			}
		}
		// The reports can queue jobs and reject promises.
		if cx.notifyRejectedPromises() {
			if cx.abortCause != nil {
				return
			}
			continue
		}
		if cx.abortCause != nil || !cx.runFinalizationCleanups() {
			break
		}
	}
}

// notifyRejectedPromises reports the unhandled rejections and removes them, the way the
// HTML standard notifies about rejected promises. It reports whether there were any to
// report.
func (cx *ExecutionContext) notifyRejectedPromises() bool {
	t := &cx.rejections
	if len(t.promises) == 0 {
		return false
	}
	// Promises rejected by the reports are added to a new list, and reported next.
	promises := t.takeForReport()
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
		t.notify(cx, storage.Value{O: p}, d.result)
		cx.ResumeInterruption()
		if cx.abortCause != nil {
			break
		}
	}
	return len(promises) != 0
}

// dropJobs discards the pending jobs.
func (ag *agent) dropJobs() {
	ag.jobs = jobQueue{}
	ag.rejections.reset()
}
