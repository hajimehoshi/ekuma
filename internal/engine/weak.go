// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/storage"
)

// This file holds the weak collections, WeakRef and FinalizationRegistry.

// NewWeakMapObject returns a WeakMap object without entries with the prototype proto.
func (cx *ExecutionContext) NewWeakMapObject(proto *Object) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classWeakMap, storage.NewWeakTable())
}

// WeakMapData returns the [[WeakMapData]] of the WeakMap object v, or nil when v is not a WeakMap object.
func WeakMapData(v Value) *WeakTable {
	if v.IsObject() && v.O.Class == classWeakMap {
		return v.O.ExtState().(*WeakTable)
	}
	return nil
}

// NewWeakSetObject returns a WeakSet object without entries with the prototype proto.
func (cx *ExecutionContext) NewWeakSetObject(proto *Object) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classWeakSet, storage.NewWeakTable())
}

// WeakSetData returns the [[WeakSetData]] of the WeakSet object v, or nil when v is not a WeakSet object.
func WeakSetData(v Value) *WeakTable {
	if v.IsObject() && v.O.Class == classWeakSet {
		return v.O.ExtState().(*WeakTable)
	}
	return nil
}

// NewWeakRefObject returns a WeakRef object with the prototype proto whose target is v,
// for which CanBeHeldWeakly must hold.
func (cx *ExecutionContext) NewWeakRefObject(proto *Object, v Value) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classWeakRef, weak.Make(v.O))
}

// FinalizationRegistry is the state of a FinalizationRegistry object.
type FinalizationRegistry struct {
	self weak.Pointer[FinalizationRegistry]

	// callback is the [[CleanupCallback]].
	callback *Object

	// Cleanup callbacks enter the context captured when the registry was created.
	context          *ExecutionContext
	continuationData any

	queue *finalizationQueue

	// cells are the registrations by their identifiers.
	cells map[uint64]*finalizationCell

	// cleanups are the cleanups of the targets of cells. They are stopped when the registry is
	// collected, as nothing can run its callback any more.
	cleanups *cellCleanups

	// tokens maps an unregister token to the identifiers of its cells.
	tokens map[weak.Pointer[Object]][]uint64

	lastID uint64
}

// finalizationCell is a record of the [[Cells]] of a FinalizationRegistry.
type finalizationCell struct {
	held     Value
	token    weak.Pointer[Object]
	hasToken bool

	// tokenIndex is the index of the cell in the identifiers of the cells of its token.
	tokenIndex int
}

// cellCleanups holds the cleanups of the targets of the cells of a FinalizationRegistry by the
// identifiers of the cells. It refers to nothing else, so that it can outlive its registry.
type cellCleanups struct {
	m map[uint64]runtime.Cleanup
}

// stopAll stops the cleanups of the cells of a registry that was collected.
func (c *cellCleanups) stopAll() {
	for _, cleanup := range c.m {
		cleanup.Stop()
	}
}

// finalizationQueue holds the cells whose target was collected. The cleanup goroutine of the
// Go runtime adds to it, and the agent takes from it between jobs.
type finalizationQueue struct {
	pending atomic.Bool

	mu   sync.Mutex
	dead []deadCell
}

// deadCell identifies a cell without keeping its registry alive.
type deadCell struct {
	registry weak.Pointer[FinalizationRegistry]
	id       uint64
}

func (q *finalizationQueue) push(d deadCell) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.dead = append(q.dead, d)
	q.pending.Store(true)
}

func (q *finalizationQueue) pop() (deadCell, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.dead) == 0 {
		q.pending.Store(false)
		return deadCell{}, false
	}
	d := q.dead[0]
	q.dead = q.dead[1:]
	return d, true
}

// NewFinalizationRegistryObject returns a FinalizationRegistry object with the prototype proto
// and the cleanup callback callback.
func (cx *ExecutionContext) NewFinalizationRegistryObject(proto *Object, callback *Object) *Object {
	if cx.finalization == nil {
		cx.finalization = &finalizationQueue{}
	}
	r := &FinalizationRegistry{
		callback:         callback,
		context:          cx,
		continuationData: cx.continuationData,
		queue:            cx.finalization,
		cells:            map[uint64]*finalizationCell{},
		cleanups: &cellCleanups{
			m: map[uint64]runtime.Cleanup{},
		},
	}
	r.self = weak.Make(r)
	runtime.AddCleanup(r, (*cellCleanups).stopAll, r.cleanups)
	return storage.NewObjectWithState(cx.ShapeFor(proto), classFinalizationRegistry, r)
}

// FinalizationRegistryData returns the state of the FinalizationRegistry object v,
// or nil when v is not a FinalizationRegistry object.
func FinalizationRegistryData(v Value) *FinalizationRegistry {
	if v.IsObject() && v.O.Class == classFinalizationRegistry {
		return v.O.ExtState().(*FinalizationRegistry)
	}
	return nil
}

// Register adds a cell with the target target, the held value held and the unregister
// token token. target, and token unless it is undefined, must be values for which
// CanBeHeldWeakly holds.
func (r *FinalizationRegistry) Register(target, held, token Value) {
	r.lastID++
	id := r.lastID
	c := &finalizationCell{held: held}
	if !token.IsUndefined() {
		c.token = weak.Make(token.O)
		c.hasToken = true
		if r.tokens == nil {
			r.tokens = map[weak.Pointer[Object]][]uint64{}
		}
		c.tokenIndex = len(r.tokens[c.token])
		r.tokens[c.token] = append(r.tokens[c.token], id)
	}
	q := r.queue
	r.cleanups.m[id] = runtime.AddCleanup(target.O, func(d deadCell) {
		q.push(d)
	}, deadCell{
		registry: r.self,
		id:       id,
	})
	r.cells[id] = c
}

// Unregister removes the cells whose unregister token is token and reports whether there was any.
// token must be a value for which CanBeHeldWeakly holds. poll, which may be nil, is called now
// and then over many cells and may stop the removal by panicking, leaving the cells that were
// not removed yet.
func (r *FinalizationRegistry) Unregister(token Value, poll func()) bool {
	w := weak.Make(token.O)
	ids, ok := r.tokens[w]
	if !ok {
		return false
	}
	for i, id := range slices.Backward(ids) {
		if poll != nil && i%(1<<12) == 0 {
			poll()
		}
		r.cleanups.m[id].Stop()
		delete(r.cleanups.m, id)
		delete(r.cells, id)
		r.tokens[w] = ids[:i]
	}
	delete(r.tokens, w)
	return true
}

// remove removes the cell id, whose target was collected.
func (r *FinalizationRegistry) remove(id uint64, c *finalizationCell) {
	delete(r.cells, id)
	delete(r.cleanups.m, id)
	if !c.hasToken {
		return
	}
	// The last cell of the token takes the place of the removed one.
	ids := r.tokens[c.token]
	last := len(ids) - 1
	if moved := ids[last]; moved != id {
		ids[c.tokenIndex] = moved
		r.cells[moved].tokenIndex = c.tokenIndex
	}
	ids[last] = 0
	if last == 0 {
		delete(r.tokens, c.token)
	} else {
		r.tokens[c.token] = ids[:last]
	}
}

// finalizationPending reports whether a target of a FinalizationRegistry was collected and its
// cleanup callback has not run yet.
func (ag *agent) finalizationPending() bool {
	q := ag.finalization
	return q != nil && q.pending.Load()
}

// runFinalizationCleanups calls the cleanup callbacks for the targets that were collected,
// and reports whether any callback ran.
func (cx *ExecutionContext) runFinalizationCleanups() bool {
	if !cx.finalizationPending() {
		return false
	}
	q := cx.finalization
	ran := false
	for {
		if cx.InterruptFlag.Load() {
			cx.Poll()
		}
		d, ok := q.pop()
		if !ok {
			return ran
		}
		r := d.registry.Value()
		if r == nil {
			continue
		}
		c, ok := r.cells[d.id]
		if !ok {
			continue
		}
		r.remove(d.id, c)
		ran = true
		r.context.enterJob(r.continuationData, func() {
			cx := r.context
			if !cx.protect(func() {
				cx.Call(r.callback, storage.Undefined, c.held)
			}) {
				thrown := cx.takeThrown()
				cx.reportUncaughtException(thrown, cx.thrownAt)
			}
		})
		cx.ResumeInterruption()
		if cx.abortCause != nil {
			return ran
		}
		cx.ClearKeptObjects()
	}
}
