// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"container/heap"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// This file holds the Atomics operations on shared data blocks and the waiters of Atomics.wait
// and Atomics.waitAsync.
//
// The VMs that share a data block are the agents of an agent cluster, and may run on different
// goroutines. The mutex of a SharedDataBlock is the critical section of all its waiter lists,
// and every access to its bytes holds it, so that the Atomics operations are atomic and the other
// accesses are free of data races.
//
// The waiter lists are stored on their data block, so that a waiter that nothing can notify any
// more is collected with its block. A waiter of Atomics.waitAsync with a finite timeout is also
// held by its VM until it times out, as its promise is then resolved.
//
// A waiter of Atomics.waitAsync that another agent notifies is posted to the mailbox of its VM,
// which resolves its promise on its own goroutine: at its next poll for an interruption, when
// it runs the jobs, or in [VM.AwaitWaiters].
//
// The VM has no timers. A timeout job runs when the jobs run after its time: whenever the
// promise jobs are drained, the waiters whose time has passed are notified with "timed-out".

// waiter is a Waiter Record.
type waiter struct {
	// agent is the VM that waits.
	agent *VM

	// promise is the promise of [[PromiseCapability]] of an asynchronous wait.
	promise *Object

	// done is closed when a synchronous wait is notified.
	done chan struct{}

	// block and byteIndex are the place of the waiter list.
	block     *SharedDataBlock
	byteIndex int

	// listed reports whether the waiter is in its waiter list. It is guarded by block.mu.
	listed bool

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

// mailbox receives what the other agents of a VM's cluster send it. Its methods may be called
// from any goroutine.
type mailbox struct {
	mu sync.Mutex

	// notified are the waiters of Atomics.waitAsync of the VM that other agents notified, oldest
	// first. It is guarded by mu.
	notified []*waiter

	// count is len(notified), for checking without mu.
	count atomic.Int32

	// wake holds a token when the VM may have something to do: a waiter was posted or an
	// interruption was requested. A blocked VM waits for it.
	wake chan struct{}
}

func newMailbox() *mailbox {
	return &mailbox{
		wake: make(chan struct{}, 1),
	}
}

// poke wakes the VM if it is blocked.
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

// SetAgentCanSuspend sets whether the agent of vm can suspend, which Atomics.wait requires.
// A VM that can suspend must not run on a thread that must not block.
func (vm *VM) SetAgentCanSuspend(canSuspend bool) {
	vm.canSuspend = canSuspend
}

// AgentCanSuspend implements AgentCanSuspend.
func (vm *VM) AgentCanSuspend() bool {
	return vm.canSuspend
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
// [VM.NotifyWaiters] in any VM notifies the waiter, or to "timed-out" after the timeout.
//
// An interruption ends a synchronous wait.
func (vm *VM) Wait(t *TypedArray, i int, v uint64, timeout float64, async bool) (Value, bool) {
	blk := t.data.block
	w := &waiter{
		agent:      vm,
		block:      blk,
		byteIndex:  t.offset + i*t.typ.Size(),
		timedIndex: -1,
	}
	if async {
		w.promise = vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype))
	} else {
		w.done = make(chan struct{})
		if vm.interrupt.Load() {
			vm.Poll()
		}
	}
	add := timeout > 0
	if !blk.compareAndAdd(w, t.typ, v, add) {
		return vm.NewString("not-equal"), false
	}
	if !add {
		return vm.NewString("timed-out"), false
	}
	if !async {
		return vm.suspend(w, timeout), false
	}
	vm.asyncWaiters++
	// A timeout that a time.Duration cannot hold, about 292 years, never expires.
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		vm.timedWaiters.add(w, time.Now().Add(time.Duration(ns)))
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
			blk.waiterLists = map[int][]*waiter{}
		}
		blk.waiterLists[w.byteIndex] = append(blk.waiterLists[w.byteIndex], w)
		w.listed = true
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
	w.listed = false
	l := blk.waiterLists[w.byteIndex]
	if i := slices.Index(l, w); i >= 0 {
		l = slices.Delete(l, i, i+1)
	}
	if len(l) == 0 {
		delete(blk.waiterLists, w.byteIndex)
	} else {
		blk.waiterLists[w.byteIndex] = l
	}
	return true
}

// removeWaiters implements RemoveWaiters: it removes and returns the first count waiters of
// byteIndex. count must not be negative and may be +Inf.
func (blk *SharedDataBlock) removeWaiters(byteIndex int, count float64) []*waiter {
	blk.mu.Lock()
	defer blk.mu.Unlock()
	l := blk.waiterLists[byteIndex]
	n := len(l)
	if count < float64(n) {
		n = int(count)
	}
	if n == 0 {
		return nil
	}
	ws := make([]*waiter, n)
	copy(ws, l)
	for _, w := range ws {
		w.listed = false
	}
	m := copy(l, l[n:])
	clear(l[m:])
	if m == 0 {
		delete(blk.waiterLists, byteIndex)
	} else {
		blk.waiterLists[byteIndex] = l[:m]
	}
	return ws
}

// suspend implements SuspendThisAgent for the synchronous waiter w, which is in its list,
// and returns the result of the wait.
func (vm *VM) suspend(w *waiter, timeout float64) Value {
	// An interruption leaves the list.
	defer w.block.unlist(w)
	var expired <-chan time.Time
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		t := time.NewTimer(time.Duration(ns))
		defer t.Stop()
		expired = t.C
	}
	for {
		select {
		case <-w.done:
			return vm.NewString("ok")
		case <-expired:
			if w.block.unlist(w) {
				return vm.NewString("timed-out")
			}
			// A notification came first.
			return vm.NewString("ok")
		case <-vm.mailbox.wake:
			// The posted waiters wait for the end of the wait.
			if vm.interruptCause.Load() != nil {
				vm.Poll()
			}
		}
	}
}

// NotifyWaiters implements RemoveWaiters and NotifyWaiter for the waiters of byteIndex of the
// shared buffer b, which may belong to any agent: it notifies the first count waiters, and returns
// how many it notified. count must not be negative and may be +Inf.
func (vm *VM) NotifyWaiters(b *ArrayBuffer, byteIndex int, count float64) int {
	ws := b.block.removeWaiters(byteIndex, count)
	for _, w := range ws {
		switch {
		case w.done != nil:
			close(w.done)
		case w.agent == vm:
			vm.timedWaiters.remove(w)
			vm.resolveWaiter(w, "ok")
		default:
			w.agent.post(w)
		}
	}
	return len(ws)
}

// post hands the waiter w of vm, which another agent notified, to vm. It may be called from any
// goroutine. The interrupt flag makes a running script take it at its next poll.
func (vm *VM) post(w *waiter) {
	vm.mailbox.add(w)
	vm.interrupt.Store(true)
	vm.mailbox.poke()
}

// resolveWaiter implements EnqueueResolveInAgentJob for the asynchronous waiter w of vm, which is
// no longer in its list.
func (vm *VM) resolveWaiter(w *waiter, result string) {
	vm.asyncWaiters--
	vm.enqueueResolveInAgentJob(w.promise, vm.NewString(result))
}

// receiveNotifications queues the resolve jobs of the waiters that other agents notified,
// and reports whether there were any.
func (vm *VM) receiveNotifications() bool {
	if vm.mailbox.count.Load() == 0 {
		return false
	}
	for _, w := range vm.mailbox.take() {
		vm.timedWaiters.remove(w)
		vm.resolveWaiter(w, "ok")
	}
	return true
}

// AwaitWaiters blocks until a waiter of Atomics.waitAsync of vm is notified by another agent or
// reaches its timeout, and reports true; its promise is then resolved when the jobs next run.
// It reports false without blocking when vm has no such waiter, and when an interruption is
// requested.
func (vm *VM) AwaitWaiters() bool {
	for {
		if vm.asyncWaiters == 0 || vm.interruptCause.Load() != nil {
			return false
		}
		if vm.mailbox.count.Load() != 0 || vm.awaitWake() {
			return true
		}
	}
}

// awaitWake blocks until the mailbox of vm is poked or the earliest timeout of its waiters
// passes, and reports whether the timeout passed.
func (vm *VM) awaitWake() bool {
	var expired <-chan time.Time
	if !vm.timedWaiters.isEmpty() {
		d := time.Until(vm.timedWaiters.heap[0].deadline)
		if d <= 0 {
			return true
		}
		t := time.NewTimer(d)
		defer t.Stop()
		expired = t.C
	}
	select {
	case <-expired:
		return true
	case <-vm.mailbox.wake:
		return false
	}
}

// Running reports whether a call from Go into vm is running.
func (vm *VM) Running() bool {
	return vm.entered > 0
}

// RunJobs runs the pending jobs, as the end of an outermost call into vm does. It must not be
// called while [VM.Running] reports true.
func (vm *VM) RunJobs() error {
	_, err := vm.enter(func() Value {
		vm.runJobs()
		return Undefined
	})
	return err
}

// expireWaiters runs the timeout jobs of the waiters whose deadline has passed, and reports
// whether there were any.
func (vm *VM) expireWaiters() bool {
	if vm.timedWaiters.isEmpty() {
		return false
	}
	now := time.Now()
	var expired bool
	for {
		w, ok := vm.timedWaiters.popExpired(now)
		if !ok {
			break
		}
		// A waiter that another agent notified first is in the mailbox.
		if w.block.unlist(w) {
			vm.resolveWaiter(w, "timed-out")
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

// enqueueResolveInAgentJob implements EnqueueResolveInAgentJob for the promise p of an
// intrinsic capability whose resolving functions were never created.
func (vm *VM) enqueueResolveInAgentJob(p *Object, v Value) {
	vm.enqueueJob(job{
		kind:     jobResolve,
		promise:  p,
		argument: v,
	})
}
