// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"container/heap"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"weak"

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

// waiter is a Waiter Record.
type waiter struct {
	agent *agent

	// promise is the promise of [[PromiseCapability]] of an asynchronous wait.
	promise *Object

	// promiseContext is the execution context that created promise.
	promiseContext *ExecutionContext

	// done is closed when a synchronous wait is notified.
	done chan struct{}

	// block and byteIndex are the place of the waiter list.
	block     *SharedDataBlock
	byteIndex int

	// listed reports whether the waiter is in its waiter list, and pos is its position there.
	// They are guarded by block.mu.
	listed bool
	pos    int

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
	if !h[i].deadline.Equal(h[j].deadline) {
		return h[i].deadline.Before(h[j].deadline)
	}
	return h[i].seq < h[j].seq
}

func (h waiterHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].timedIndex = i
	h[j].timedIndex = j
}

func (h *waiterHeap) Push(x any) {
	w := x.(*waiter)
	w.timedIndex = len(*h)
	*h = append(*h, w)
}

func (h *waiterHeap) Pop() any {
	old := *h
	w := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	w.timedIndex = -1
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
	old := LoadElementBits(e, t.typ)
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
		if !elementBitsEqual(t.typ, old, v) {
			return old
		}
		x = replacement
	}
	StoreElementBits(e, t.typ, x)
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

// elementBitsEqual reports whether the encodings x and y are equal in the size of an element of
// the type t.
func elementBitsEqual(t ElementType, x, y uint64) bool {
	mask := uint64(math.MaxUint64) >> (64 - 8*t.Size())
	return x&mask == y&mask
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
	w := &waiter{
		agent:      cx.agent,
		block:      blk,
		byteIndex:  t.offset + i*t.typ.Size(),
		timedIndex: -1,
	}
	if async {
		w.promise = cx.NewPromiseObject(cx.realm.Intrinsic(IntrinsicPromisePrototype))
		w.promiseContext = cx
	} else {
		w.done = make(chan struct{})
		if cx.interrupt.Load() {
			cx.Poll()
		}
	}
	add := timeout > 0
	if !blk.compareAndAdd(w, t.typ, v, add) {
		return cx.NewString("not-equal"), false
	}
	if !add {
		return cx.NewString("timed-out"), false
	}
	if !async {
		return cx.suspend(w, timeout), false
	}
	if cx.asyncWaiters == nil {
		cx.asyncWaiters = map[*waiter]struct{}{}
	}
	cx.asyncWaiters[w] = struct{}{}
	// A timeout that a time.Duration cannot hold, about 292 years, never expires.
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		cx.timedWaiters.add(w, cx.Now().Add(time.Duration(ns)))
	}
	return Value{o: w.promise}, true
}

// compareAndAdd reports whether the element of the type t at w.byteIndex has the encoding v,
// and then adds w to its waiter list when add is set.
func (blk *SharedDataBlock) compareAndAdd(w *waiter, t ElementType, v uint64, add bool) bool {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	if !elementBitsEqual(t, LoadElementBits(blk.data[w.byteIndex:], t), v) {
		return false
	}
	if add {
		if blk.waiterLists == nil {
			blk.waiterLists = map[int]*waiterList{}
		}
		l := blk.waiterLists[w.byteIndex]
		if l == nil {
			l = &waiterList{}
			blk.waiterLists[w.byteIndex] = l
		}
		l.add(w)
	}
	return true
}

// unlist removes w from its waiter list and reports whether it was there.
func (blk *SharedDataBlock) unlist(w *waiter) bool {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	if !w.listed {
		return false
	}
	l := blk.waiterLists[w.byteIndex]
	l.remove(w)
	if l.listed == 0 {
		delete(blk.waiterLists, w.byteIndex)
	}
	return true
}

// removeWaiters implements RemoveWaiters: it removes and returns the first count waiters of
// byteIndex. count must not be negative and may be +Inf.
func (blk *SharedDataBlock) removeWaiters(byteIndex int, count float64) []*waiter {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	l := blk.waiterLists[byteIndex]
	if l == nil {
		return nil
	}
	ws := l.removeFirst(count)
	if l.listed == 0 {
		delete(blk.waiterLists, byteIndex)
	}
	return ws
}

// minWaiterSweep is the length of a waiter list from which adding to it may sweep it.
const minWaiterSweep = 16

// waiterList is a waiter list, in the order in which the waiters were added. A removed or
// collected waiter leaves an empty entry until a sweep, which runs when the list has doubled
// since the last one.
type waiterList struct {
	// entries holds the waiters from the position base on.
	entries []weak.Pointer[waiter]
	base    int

	// listed is the number of the waiters in entries that are listed, collected ones included.
	listed int

	// swept is the length of entries after the last sweep.
	swept int
}

// add adds w at the end of l.
func (l *waiterList) add(w *waiter) {
	if len(l.entries) >= 2*max(l.swept, minWaiterSweep) {
		l.sweep()
	}
	w.listed = true
	w.pos = l.base + len(l.entries)
	l.entries = append(l.entries, weak.Make(w))
	l.listed++
}

// remove removes the listed waiter w from l.
func (l *waiterList) remove(w *waiter) {
	w.listed = false
	l.entries[w.pos-l.base] = weak.Pointer[waiter]{}
	l.listed--
}

// removeFirst removes and returns the first count waiters of l that are not collected. count
// must not be negative and may be +Inf.
func (l *waiterList) removeFirst(count float64) []*waiter {
	var ws []*waiter
	n := 0
	for ; n < len(l.entries) && float64(len(ws)) < count; n++ {
		e := l.entries[n]
		if e == (weak.Pointer[waiter]{}) {
			continue
		}
		l.listed--
		if w := e.Value(); w != nil {
			w.listed = false
			ws = append(ws, w)
		}
	}
	clear(l.entries[:n])
	l.entries = l.entries[n:]
	l.base += n
	return ws
}

// sweep removes the empty entries and those of the collected waiters from l.
func (l *waiterList) sweep() {
	n := 0
	for _, e := range l.entries {
		if e == (weak.Pointer[waiter]{}) {
			continue
		}
		w := e.Value()
		if w == nil {
			l.listed--
			continue
		}
		w.pos = l.base + n
		l.entries[n] = e
		n++
	}
	clear(l.entries[n:])
	l.entries = l.entries[:n]
	l.swept = n
}

// suspend implements SuspendThisAgent for the synchronous waiter w, which is in its list,
// and returns the result of the wait.
func (cx *ExecutionContext) suspend(w *waiter, timeout float64) Value {
	// An interruption leaves the list.
	defer w.block.unlist(w)
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
		case <-w.done:
			return cx.NewString("ok")
		case <-expired:
			// The timer measures real time, which the clock may run behind.
			if d := deadline.Sub(cx.Now()); d > 0 {
				timer.Reset(d)
				continue
			}
			if w.block.unlist(w) {
				return cx.NewString("timed-out")
			}
			// A notification came first.
			return cx.NewString("ok")
		case <-cx.mailbox.wake:
			// The posted waiters wait for the end of the wait.
			if cx.interruptCause.Load() != nil {
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
		case w.done != nil:
			close(w.done)
		case w.agent == cx.agent:
			cx.timedWaiters.remove(w)
			cx.resolveWaiter(w, "ok")
		default:
			w.agent.post(w)
		}
	}
	return len(ws)
}

// post hands the waiter w of ag, which another agent notified, to ag. It may be called from any
// goroutine. The interrupt flag makes a running script take it at its next poll.
func (ag *agent) post(w *waiter) {
	ag.mailbox.add(w)
	ag.interrupt.Store(true)
	ag.mailbox.poke()
}

// resolveWaiter implements EnqueueResolveInAgentJob for the asynchronous waiter w of ag, which is
// no longer in its list.
func (ag *agent) resolveWaiter(w *waiter, result wtf8.String) {
	delete(ag.asyncWaiters, w)
	// Settling a waiter is an agent task. Its promise reactions use their own
	// realms' microtask queues.
	ag.jobs.enqueue(job{
		kind:     jobResolve,
		promise:  w.promise,
		argument: w.promiseContext.NewString(result),
		context:  w.promiseContext,
	})
}

// receiveNotifications queues the resolve jobs of the waiters that other agents notified,
// and reports whether there were any.
func (ag *agent) receiveNotifications() bool {
	if ag.mailbox.count.Load() == 0 {
		return false
	}
	for _, w := range ag.mailbox.take() {
		ag.timedWaiters.remove(w)
		ag.resolveWaiter(w, "ok")
	}
	return true
}

// AwaitJobs waits for pending jobs, a waiter notification or a timeout. It returns
// false if there are no jobs or waiters, or an InterruptedError if interrupted.
func (ag *agent) AwaitJobs() (bool, error) {
	for {
		if cause := ag.interruptCause.Load(); cause != nil {
			return false, &InterruptedError{
				Cause: *cause,
			}
		}
		if !ag.jobs.isEmpty() || ag.finalizationPending() || ag.mailbox.count.Load() != 0 {
			return true, nil
		}
		if len(ag.asyncWaiters) == 0 {
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
	var deadline time.Time
	var expired <-chan time.Time
	if !ag.timedWaiters.isEmpty() {
		deadline = ag.timedWaiters.heap[0].deadline
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
		return Undefined
	})
	return err
}

// expireWaiters runs the timeout jobs of the waiters whose deadline has passed, and reports
// whether there were any.
func (cx *ExecutionContext) expireWaiters() bool {
	if cx.timedWaiters.isEmpty() {
		return false
	}
	now := cx.Now()
	var expired bool
	for {
		w, ok := cx.timedWaiters.popExpired(now)
		if !ok {
			break
		}
		// A waiter that another agent notified first is in the mailbox.
		if w.block.unlist(w) {
			cx.resolveWaiter(w, "timed-out")
			expired = true
		}
	}
	return expired
}

// timedWaiters holds the waiters with a finite timeout.
type timedWaiters struct {
	heap waiterHeap

	// seq numbers the waiters by their creation.
	seq uint64
}

func (t *timedWaiters) isEmpty() bool {
	return len(t.heap) == 0
}

// add adds w with the given deadline.
func (t *timedWaiters) add(w *waiter, deadline time.Time) {
	w.deadline = deadline
	w.seq = t.seq
	t.seq++
	heap.Push(&t.heap, w)
}

// remove removes w, if it is there.
func (t *timedWaiters) remove(w *waiter) {
	if w.timedIndex >= 0 {
		heap.Remove(&t.heap, w.timedIndex)
	}
}

// popExpired removes and returns the waiter with the earliest deadline, if that is not after now.
func (t *timedWaiters) popExpired(now time.Time) (*waiter, bool) {
	if len(t.heap) == 0 || t.heap[0].deadline.After(now) {
		return nil, false
	}
	return heap.Pop(&t.heap).(*waiter), true
}
