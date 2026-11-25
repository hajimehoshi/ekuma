// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"container/heap"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/waiterlist"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the Atomics operations on shared data blocks and the waiters of Atomics.wait
// and Atomics.waitAsync.
//
// Agents that share a data block belong to the same agent cluster, and may run on different
// goroutines. The mutex of a SharedDataBlock is the critical section of all its waiter lists,
// and every access to its bytes holds it, so that the Atomics operations are atomic and the other
// accesses are free of data races.
//
// The waiter lists are stored on their data block, so that a waiter that nothing can notify any
// more is collected with its block. The lists refer to their waiters weakly, and the agent of a
// waiter of Atomics.waitAsync holds it until its promise is resolved, so that an agent that is
// dropped while waiting is collected even when its block lives on. Such a waiter leaves its
// list without being notified.
//
// A waiter of Atomics.waitAsync that another agent notifies is posted to the mailbox of its agent,
// which resolves its promise on its own goroutine: at its next poll for an interruption, when
// it runs the jobs, or in [agent.AwaitJobs].
//
// The agent has no timers. A timeout job runs when the jobs run after its time: whenever the
// promise jobs are drained, the waiters whose time has passed are notified with "timed-out".

// waiter is a Waiter Record. The mutex of its block guards its place in the waiter lists.
type waiter = waiterlist.Waiter[waiterData]

// waiterData is what the engine keeps in a waiter. The mutex of block does not guard it.
type waiterData struct {
	agent *agent

	// promise is the promise of [[PromiseCapability]] of an asynchronous wait.
	promise *Object

	// promiseContext is the execution context that created promise.
	promiseContext *ExecutionContext

	// done is closed when a synchronous wait is notified.
	done chan struct{}

	// block is the data block whose waiter lists the waiter is added to.
	block *SharedDataBlock

	// deadline is [[TimeoutTime]] of a waiter with a finite timeout.
	deadline time.Time

	// seq orders the waiters with the same deadline by their creation.
	seq uint64

	// timedIndex is the index of the waiter in a waiterHeap, or -1.
	timedIndex int
}

// waiterHeap is a heap of waiters ordered by deadline.
type waiterHeap []*waiter

func (h waiterHeap) Len() int {
	return len(h)
}

func (h waiterHeap) Less(i, j int) bool {
	if !h[i].Data.deadline.Equal(h[j].Data.deadline) {
		return h[i].Data.deadline.Before(h[j].Data.deadline)
	}
	return h[i].Data.seq < h[j].Data.seq
}

func (h waiterHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].Data.timedIndex = i
	h[j].Data.timedIndex = j
}

func (h *waiterHeap) Push(x any) {
	w := x.(*waiter)
	w.Data.timedIndex = len(*h)
	*h = append(*h, w)
}

func (h *waiterHeap) Pop() any {
	old := *h
	w := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	w.Data.timedIndex = -1
	return w
}

// mailbox receives what the other agents of an agent's cluster send it. Its methods may be called
// from any goroutine.
type mailbox struct {
	mu sync.Mutex

	// notified are the waiters of Atomics.waitAsync of the agent that other agents notified, oldest
	// first. It is guarded by mu.
	notified []*waiter

	// count is len(notified), for checking without mu.
	count atomic.Int32

	// wake holds a token when the agent may have something to do: a waiter was posted or an
	// interruption was requested. A blocked agent waits for it.
	wake chan struct{}
}

func newMailbox() *mailbox {
	return &mailbox{
		wake: make(chan struct{}, 1),
	}
}

// isEmpty reports whether there are no notified waiters.
func (m *mailbox) isEmpty() bool {
	return m.count.Load() == 0
}

// poke wakes the agent if it is blocked.
func (m *mailbox) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *mailbox) add(w *waiter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified = append(m.notified, w)
	m.count.Store(int32(len(m.notified)))
}

// take removes and returns the notified waiters.
func (m *mailbox) take() []*waiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.notified
	m.notified = nil
	m.count.Store(0)
	return ws
}

// SetAgentCanSuspend sets whether the agent can suspend, which Atomics.wait requires.
// An agent that can suspend must not run on a thread that must not block.
func (ag *agent) SetAgentCanSuspend(canSuspend bool) {
	ag.canSuspend = canSuspend
}

// AgentCanSuspend implements AgentCanSuspend.
func (ag *agent) AgentCanSuspend() bool {
	return ag.canSuspend
}

// AtomicOp is the operation of [TypedArray.AtomicReadModifyWrite].
type AtomicOp uint8

const (
	AtomicAdd AtomicOp = iota
	AtomicAnd
	AtomicExchange
	AtomicOr
	AtomicSub
	AtomicXor

	// AtomicCompareExchange stores the replacement when the element equals the operand.
	AtomicCompareExchange
)

// AtomicReadModifyWrite implements the steps of AtomicReadModifyWrite and Atomics.compareExchange
// that follow RevalidateAtomicAccess: it applies op with the encoding v to the element i of t,
// which must be a valid integer index, and returns the encoding that the element had. replacement
// is the encoding that AtomicCompareExchange stores.
func (t *TypedArray) AtomicReadModifyWrite(i int, op AtomicOp, v, replacement uint64) uint64 {
	b := t.Lock()
	defer t.Unlock()
	e := b[i*t.typ.Size():]
	old := element.Load(e, t.typ)
	var x uint64
	switch op {
	case AtomicAdd:
		x = old + v
	case AtomicAnd:
		x = old & v
	case AtomicExchange:
		x = v
	case AtomicOr:
		x = old | v
	case AtomicSub:
		x = old - v
	case AtomicXor:
		x = old ^ v
	case AtomicCompareExchange:
		if !element.Equal(t.typ, old, v) {
			return old
		}
		x = replacement
	}
	element.Store(e, t.typ, x)
	return old
}

// AtomicLoad returns the encoding of the element i of t, which must be a valid integer index,
// as a sequentially consistent read.
func (t *TypedArray) AtomicLoad(i int) uint64 {
	x, _ := t.LoadElement(i)
	return x
}

// AtomicStore stores the encoding x as the element i of t, which must be a valid integer index,
// as a sequentially consistent write.
func (t *TypedArray) AtomicStore(i int, x uint64) {
	t.StoreElement(i, x)
}

// Wait implements the steps of DoWait from the critical section on, for the element i of the
// Int32Array or BigInt64Array t on a shared buffer, the encoding v and a timeout in milliseconds
// that is not negative and may be +Inf. A synchronous wait suspends the agent, which must be able
// to, and returns "ok", "not-equal" or "timed-out". An asynchronous wait returns "not-equal" or
// "timed-out" without waiting, or reports true and returns a promise that resolves to "ok" when
// [ExecutionContext.NotifyWaiters] in any agent notifies the waiter, or to "timed-out" after the timeout.
//
// An interruption ends a synchronous wait.
func (cx *ExecutionContext) Wait(t *TypedArray, i int, v uint64, timeout float64, async bool) (Value, bool) {
	blk := t.data.block
	byteIndex := t.offset + i*t.typ.Size()
	w := &waiter{
		Data: waiterData{
			agent:      cx.agent,
			block:      blk,
			timedIndex: -1,
		},
	}
	if async {
		w.Data.promise = cx.NewPromiseObject(cx.Intrinsic(IntrinsicPromisePrototype))
		w.Data.promiseContext = cx
	} else {
		w.Data.done = make(chan struct{})
		if cx.InterruptFlag.Load() {
			cx.Poll()
		}
	}
	add := timeout > 0
	if !blk.compareAndAdd(w, byteIndex, t.typ, v, add) {
		return cx.NewString("not-equal"), false
	}
	if !add {
		return cx.NewString("timed-out"), false
	}
	if !async {
		return cx.suspend(w, timeout), false
	}
	cx.asyncWaiters.add(w)
	// A timeout that a time.Duration cannot hold, about 292 years, never expires.
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		cx.asyncWaiters.setDeadline(w, cx.Now().Add(time.Duration(ns)))
	}
	return Value{O: w.Data.promise}, true
}

// compareAndAdd reports whether the element of the type t at byteIndex has the encoding v,
// and then adds w to the waiter list of byteIndex when add is set.
func (blk *SharedDataBlock) compareAndAdd(w *waiter, byteIndex int, t element.Type, v uint64, add bool) bool {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	if !element.Equal(t, element.Load(blk.data[byteIndex:], t), v) {
		return false
	}
	if add {
		blk.waiterLists.Add(byteIndex, w)
	}
	return true
}

// unlist removes w from its waiter list and reports whether it was there.
func (blk *SharedDataBlock) unlist(w *waiter) bool {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	return blk.waiterLists.Remove(w)
}

// removeWaiters implements RemoveWaiters: it removes and returns the first count waiters of
// byteIndex. count must not be negative and may be +Inf.
func (blk *SharedDataBlock) removeWaiters(byteIndex int, count float64) []*waiter {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	return blk.waiterLists.RemoveFirst(byteIndex, count)
}

// suspend implements SuspendThisAgent for the synchronous waiter w, which is in its list,
// and returns the result of the wait.
func (cx *ExecutionContext) suspend(w *waiter, timeout float64) Value {
	// An interruption leaves the list.
	defer w.Data.block.unlist(w)
	var deadline time.Time
	var timer *time.Timer
	var expired <-chan time.Time
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		deadline = cx.Now().Add(time.Duration(ns))
		timer = time.NewTimer(time.Duration(ns))
		defer timer.Stop()
		expired = timer.C
	}
	for {
		select {
		case <-w.Data.done:
			return cx.NewString("ok")
		case <-expired:
			// The timer measures real time, which the clock may run behind.
			if d := deadline.Sub(cx.Now()); d > 0 {
				timer.Reset(d)
				continue
			}
			if w.Data.block.unlist(w) {
				return cx.NewString("timed-out")
			}
			// A notification came first.
			return cx.NewString("ok")
		case <-cx.mailbox.wake:
			// The posted waiters wait for the end of the wait.
			if cx.InterruptCause.Load() != nil {
				cx.Poll()
			}
		}
	}
}

// NotifyWaiters implements RemoveWaiters and NotifyWaiter for the waiters of byteIndex of the
// shared buffer b, which may belong to any agent: it notifies the first count waiters, and returns
// how many it notified. count must not be negative and may be +Inf.
func (cx *ExecutionContext) NotifyWaiters(b *ArrayBuffer, byteIndex int, count float64) int {
	ws := b.block.removeWaiters(byteIndex, count)
	for _, w := range ws {
		switch {
		case w.Data.done != nil:
			close(w.Data.done)
		case w.Data.agent == cx.agent:
			cx.resolveWaiter(w, "ok")
		default:
			w.Data.agent.post(w)
		}
	}
	return len(ws)
}

// post hands the waiter w of ag, which another agent notified, to ag. It may be called from any
// goroutine. The interrupt flag makes a running script take it at its next poll.
func (ag *agent) post(w *waiter) {
	ag.mailbox.add(w)
	ag.InterruptFlag.Store(true)
	ag.mailbox.poke()
}

// resolveWaiter implements EnqueueResolveInAgentJob for the asynchronous waiter w of ag, which is
// no longer in its list.
func (ag *agent) resolveWaiter(w *waiter, result wtf8.String) {
	ag.asyncWaiters.remove(w)
	// Settling a waiter is an agent task. Its promise reactions use their own
	// realms' microtask queues.
	ag.jobs.enqueue(job{
		kind:     jobResolve,
		promise:  w.Data.promise,
		argument: w.Data.promiseContext.NewString(result),
		context:  w.Data.promiseContext,
	})
}

// receiveNotifications queues the resolve jobs of the waiters that other agents notified,
// and reports whether there were any.
func (ag *agent) receiveNotifications() bool {
	if ag.mailbox.isEmpty() {
		return false
	}
	for _, w := range ag.mailbox.take() {
		ag.resolveWaiter(w, "ok")
	}
	return true
}

// AwaitJobs waits for pending jobs, a waiter notification or a timeout. It returns
// false if there are no jobs or waiters, or an InterruptedError if interrupted.
func (ag *agent) AwaitJobs() (bool, error) {
	for {
		if cause := ag.InterruptCause.Load(); cause != nil {
			return false, &InterruptedError{
				Cause: *cause,
			}
		}
		if !ag.jobs.isEmpty() || ag.finalizationPending() || !ag.mailbox.isEmpty() {
			return true, nil
		}
		if ag.asyncWaiters.isEmpty() {
			return false, nil
		}
		if ag.awaitWake() {
			return true, nil
		}
	}
}

// awaitWake blocks until the mailbox of ag is poked or the earliest timeout of its waiters
// passes, and reports whether the timeout passed.
func (ag *agent) awaitWake() bool {
	var expired <-chan time.Time
	deadline, ok := ag.asyncWaiters.earliestDeadline()
	if ok {
		d := deadline.Sub(ag.Now())
		if d <= 0 {
			return true
		}
		t := time.NewTimer(d)
		defer t.Stop()
		expired = t.C
	}
	select {
	case <-expired:
		// The timer measures real time, which the clock may run behind.
		return !deadline.After(ag.Now())
	case <-ag.mailbox.wake:
		return false
	}
}

// Running reports whether a call from Go into ag is running.
func (ag *agent) Running() bool {
	return ag.entered > 0
}

// RunJobs runs the pending jobs, as the end of an outermost call into cx does. It must not be
// called while [agent.Running] reports true.
func (cx *ExecutionContext) RunJobs() error {
	// enter drains the pending jobs after this empty callback returns.
	_, err := cx.enter(func() Value {
		return storage.Undefined
	})
	return err
}

// expireWaiters runs the timeout jobs of the waiters whose deadline has passed, and reports
// whether there were any.
func (cx *ExecutionContext) expireWaiters() bool {
	if !cx.asyncWaiters.hasDeadline() {
		return false
	}
	now := cx.Now()
	var expired bool
	for {
		w, ok := cx.asyncWaiters.popExpired(now)
		if !ok {
			break
		}
		// A waiter that another agent notified first is in the mailbox.
		if w.Data.block.unlist(w) {
			cx.resolveWaiter(w, "timed-out")
			expired = true
		}
	}
	return expired
}

// asyncWaiters holds the waiters of Atomics.waitAsync of an agent whose promise is not resolved.
// The waiter lists of the data blocks refer to them weakly, so that an agent that is dropped while
// waiting is collected.
type asyncWaiters struct {
	// timed holds the waiters with a deadline. Each of them is also in all.
	timed waiterHeap

	// seq numbers the waiters of timed by their creation.
	seq uint64

	// all holds all the waiters. A waiter that popExpired returned stays in it until its promise is
	// resolved, which waits for the mailbox when another agent notified the waiter first.
	all map[*waiter]struct{}
}

func (s *asyncWaiters) isEmpty() bool {
	return len(s.all) == 0
}

func (s *asyncWaiters) hasDeadline() bool {
	return len(s.timed) != 0
}

// add adds w without a deadline.
func (s *asyncWaiters) add(w *waiter) {
	if s.all == nil {
		s.all = map[*waiter]struct{}{}
	}
	s.all[w] = struct{}{}
}

// setDeadline gives the added waiter w, which has no deadline, the given deadline.
func (s *asyncWaiters) setDeadline(w *waiter, deadline time.Time) {
	w.Data.deadline = deadline
	w.Data.seq = s.seq
	s.seq++
	heap.Push(&s.timed, w)
}

// remove removes w and its deadline, if it has one.
func (s *asyncWaiters) remove(w *waiter) {
	delete(s.all, w)
	if w.Data.timedIndex >= 0 {
		heap.Remove(&s.timed, w.Data.timedIndex)
	}
}

// earliestDeadline returns the earliest deadline, or reports false when there is none.
func (s *asyncWaiters) earliestDeadline() (time.Time, bool) {
	if len(s.timed) == 0 {
		return time.Time{}, false
	}
	return s.timed[0].Data.deadline, true
}

// popExpired returns the waiter with the earliest deadline, if that is not after now. It removes
// the deadline but not the waiter.
func (s *asyncWaiters) popExpired(now time.Time) (*waiter, bool) {
	if len(s.timed) == 0 || s.timed[0].Data.deadline.After(now) {
		return nil, false
	}
	return heap.Pop(&s.timed).(*waiter), true
}
