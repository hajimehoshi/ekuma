// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"container/heap"
	"math"
	"time"
)

// This file holds the waiters of Atomics.waitAsync.
//
// The waiter lists are stored on their SharedArrayBuffer, so that a waiter that nothing can
// notify any more is collected with its buffer. A waiter with a finite timeout is also held by
// the VM until it times out, as its promise is then resolved.
//
// The VM has no timers. A timeout job runs when the jobs run after its time: whenever the
// promise jobs are drained, the waiters whose time has passed are notified with "timed-out".

// waiter is a Waiter Record of an asynchronous wait.
type waiter struct {
	// promise is the promise of [[PromiseCapability]].
	promise *Object

	// list is the waiter list that holds the waiter, or nil once it is removed.
	list *waiterList

	// deadline is [[TimeoutTime]] of a waiter with a finite timeout.
	deadline time.Time

	// seq orders the waiters with the same deadline by their creation.
	seq uint64

	// timedIndex is the index of the waiter in a waiterHeap, or -1.
	timedIndex int
}

// waiterList is a WaiterList: the waiters of one byte index of a shared data block, oldest first.
type waiterList struct {
	waiters []*waiter

	// buffer and byteIndex are the place of the list in its buffer's waiterLists.
	buffer    *ArrayBuffer
	byteIndex int
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

// AddWaiter implements the steps of DoWait in async mode that follow a successful comparison:
// it adds a waiter for byteIndex of the shared buffer b and returns its promise. The promise
// resolves to "ok" when [VM.NotifyWaiters] notifies the waiter, or to "timed-out" after timeout
// milliseconds unless timeout is +Inf. timeout must be positive.
func (vm *VM) AddWaiter(b *ArrayBuffer, byteIndex int, timeout float64) Value {
	if b.waiterLists == nil {
		b.waiterLists = map[int]*waiterList{}
	}
	l := b.waiterLists[byteIndex]
	if l == nil {
		l = &waiterList{
			buffer:    b,
			byteIndex: byteIndex,
		}
		b.waiterLists[byteIndex] = l
	}
	w := &waiter{
		promise:    vm.NewPromiseObject(vm.Intrinsic(IntrinsicPromisePrototype)),
		list:       l,
		timedIndex: -1,
	}
	l.waiters = append(l.waiters, w)
	// A timeout that a time.Duration cannot hold, about 292 years, never expires.
	if ns := timeout * float64(time.Millisecond); ns < math.MaxInt64 {
		vm.timedWaiters.add(w, time.Now().Add(time.Duration(ns)))
	}
	return Value{o: w.promise}
}

// NotifyWaiters implements RemoveWaiters and NotifyWaiter for the waiters of byteIndex of the
// shared buffer b: it notifies the first count waiters, and returns how many it notified.
// count must not be negative and may be +Inf.
func (vm *VM) NotifyWaiters(b *ArrayBuffer, byteIndex int, count float64) int {
	l := b.waiterLists[byteIndex]
	if l == nil {
		return 0
	}
	n := len(l.waiters)
	if count < float64(n) {
		n = int(count)
	}
	for _, w := range l.waiters[:n] {
		w.list = nil
		vm.timedWaiters.remove(w)
		vm.enqueueResolveInAgentJob(w.promise, vm.NewString("ok"))
	}
	m := copy(l.waiters, l.waiters[n:])
	clear(l.waiters[m:])
	l.waiters = l.waiters[:m]
	if m == 0 {
		delete(b.waiterLists, byteIndex)
	}
	return n
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
		w.list.remove(w)
		w.list = nil
		vm.enqueueResolveInAgentJob(w.promise, vm.NewString("timed-out"))
		expired = true
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

// remove removes w from l, and l from its buffer when l becomes empty.
func (l *waiterList) remove(w *waiter) {
	for i, x := range l.waiters {
		if x == w {
			copy(l.waiters[i:], l.waiters[i+1:])
			l.waiters[len(l.waiters)-1] = nil
			l.waiters = l.waiters[:len(l.waiters)-1]
			break
		}
	}
	if len(l.waiters) == 0 {
		delete(l.buffer.waiterLists, l.byteIndex)
	}
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
