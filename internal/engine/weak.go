// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"weak"
)

// This file holds the weak collections, WeakRef and FinalizationRegistry.
//
// A key owns its entries, so a value that refers back to its key can be collected with it.
// Each collection tracks the keys' entry stores weakly. Its cleanup removes its value from
// every store that outlives the collection.

// WeakTable is the [[WeakMapData]] of a WeakMap or the [[WeakSetData]] of a WeakSet.
// Every key must be a value for which CanBeHeldWeakly holds.
type WeakTable struct {
	// Operations on keys keep the table alive until its entries have been read or updated.
	keys *weakTableKeys
}

// weakTableKeys identifies a table in its keys' entries and can outlive the table.
type weakTableKeys struct {
	mu sync.Mutex

	// entries holds the entry stores of the table's keys weakly. The stores of collected keys
	// stay until a sweep.
	entries map[weak.Pointer[weakEntries]]struct{}

	// sweepAt is the number of entries at which adding a key first drops the stores of the
	// collected keys.
	sweepAt int
}

// minWeakSweep is the number of key stores of a table below which adding a key never sweeps.
const minWeakSweep = 8

func (t *WeakTable) addKey(w *weakEntries) {
	keys := t.keys
	keys.mu.Lock()
	defer keys.mu.Unlock()
	if keys.entries == nil {
		keys.entries = make(map[weak.Pointer[weakEntries]]struct{})
		runtime.AddCleanup(t, (*weakTableKeys).clear, keys)
	}
	if len(keys.entries) >= keys.sweepAt {
		for key := range keys.entries {
			if key.Value() == nil {
				delete(keys.entries, key)
			}
		}
		keys.sweepAt = max(minWeakSweep, 2*len(keys.entries))
	}
	keys.entries[w.self] = struct{}{}
}

func (keys *weakTableKeys) remove(w *weakEntries) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	delete(keys.entries, w.self)
}

func (keys *weakTableKeys) clear() {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	for key := range keys.entries {
		if w := key.Value(); w != nil {
			w.delete(keys)
			runtime.KeepAlive(w)
		}
	}
	keys.entries = nil
}

// weakEntries are the entries of the weak collections whose key is one object or symbol.
// One entry is held in first, and only the entries of the other collections are in rest.
type weakEntries struct {
	// The VM and the runtime cleanups of several collections can access the same key.
	mu         sync.Mutex
	firstTable *weakTableKeys
	first      Value
	rest       map[*weakTableKeys]Value

	// self is the weak pointer by which the collections refer to the store.
	self weak.Pointer[weakEntries]
}

func newWeakTable() *WeakTable {
	return &WeakTable{
		keys: &weakTableKeys{},
	}
}

// NewWeakMapObject returns a WeakMap object without entries with the prototype proto.
func (vm *VM) NewWeakMapObject(proto *Object) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakMap
	o.setExtState(newWeakTable())
	return o
}

// WeakMapData returns the [[WeakMapData]] of the WeakMap object v, or nil when v is not a WeakMap object.
func WeakMapData(v Value) *WeakTable {
	if v.IsObject() && v.o.class == classWeakMap {
		return v.o.extState().(*WeakTable)
	}
	return nil
}

// NewWeakSetObject returns a WeakSet object without entries with the prototype proto.
func (vm *VM) NewWeakSetObject(proto *Object) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakSet
	o.setExtState(newWeakTable())
	return o
}

// WeakSetData returns the [[WeakSetData]] of the WeakSet object v, or nil when v is not a WeakSet object.
func WeakSetData(v Value) *WeakTable {
	if v.IsObject() && v.o.class == classWeakSet {
		return v.o.extState().(*WeakTable)
	}
	return nil
}

// CanBeHeldWeakly implements CanBeHeldWeakly.
func (vm *VM) CanBeHeldWeakly(v Value) bool {
	if v.IsObject() {
		return true
	}
	if !v.IsSymbol() {
		return false
	}
	_, registered := vm.KeyForSymbol(v.o)
	return !registered
}

// Get returns the value of the entry with the key k.
func (t *WeakTable) Get(k Value) (Value, bool) {
	m := k.o.meta()
	if m == nil || m.weak == nil {
		return Undefined, false
	}
	defer runtime.KeepAlive(t)
	w := m.weak
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.firstTable == t.keys {
		return w.first, true
	}
	v, ok := w.rest[t.keys]
	return v, ok
}

// Has reports whether t has an entry with the key k.
func (t *WeakTable) Has(k Value) bool {
	_, ok := t.Get(k)
	return ok
}

// Set sets the value of the entry with the key k.
func (t *WeakTable) Set(k, v Value) {
	defer runtime.KeepAlive(t)
	m := k.o.ensureMeta()
	if m.weak == nil {
		w := &weakEntries{}
		w.self = weak.Make(w)
		m.weak = w
	}
	if m.weak.set(t.keys, v) {
		t.addKey(m.weak)
	}
}

// set reports whether it added a new entry.
func (w *weakEntries) set(table *weakTableKeys, v Value) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.firstTable == table {
		w.first = v
		return false
	}
	_, exists := w.rest[table]
	if w.firstTable == nil && !exists {
		w.firstTable = table
		w.first = v
		return true
	}
	if w.rest == nil {
		w.rest = make(map[*weakTableKeys]Value)
	}
	w.rest[table] = v
	return !exists
}

// Delete deletes the entry with the key k and reports whether there was one.
func (t *WeakTable) Delete(k Value) bool {
	defer runtime.KeepAlive(t)
	m := k.o.meta()
	if m == nil || m.weak == nil || !m.weak.delete(t.keys) {
		return false
	}
	t.keys.remove(m.weak)
	return true
}

func (w *weakEntries) delete(table *weakTableKeys) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.firstTable == table {
		w.firstTable = nil
		w.first = Undefined
		return true
	}
	if _, ok := w.rest[table]; !ok {
		return false
	}
	delete(w.rest, table)
	return true
}

// NewWeakRefObject returns a WeakRef object with the prototype proto whose target is v,
// for which CanBeHeldWeakly must hold.
func (vm *VM) NewWeakRefObject(proto *Object, v Value) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakRef
	o.setExtState(weak.Make(v.o))
	return o
}

// WeakRefDeref implements WeakRefDeref for the WeakRef object v.
// It reports false when v is not a WeakRef object.
func (vm *VM) WeakRefDeref(v Value) (Value, bool) {
	if !v.IsObject() || v.o.class != classWeakRef {
		return Undefined, false
	}
	t := v.o.extState().(weak.Pointer[Object]).Value()
	if t == nil {
		return Undefined, true
	}
	vm.AddToKeptObjects(t)
	return Value{o: t}, true
}

// weakState is the state of an agent for WeakRef and FinalizationRegistry objects.
type weakState struct {
	// keptAlive is the [[KeptAlive]] list: the targets of WeakRef objects that must stay alive
	// until the current job ends.
	keptAlive map[*Object]struct{}

	// finalization receives the cells of FinalizationRegistry objects whose target was collected,
	// or is nil until the first registry is created.
	finalization *finalizationQueue
}

// AddToKeptObjects implements AddToKeptObjects: o stays alive until the current job ends.
func (vm *VM) AddToKeptObjects(o *Object) {
	if vm.weak.keptAlive == nil {
		vm.weak.keptAlive = map[*Object]struct{}{}
	}
	vm.weak.keptAlive[o] = struct{}{}
}

// clearKeptObjects implements ClearKeptObjects.
// It must be called when a job ends: after each promise job, and when the outermost call into
// the engine returns.
func (vm *VM) clearKeptObjects() {
	clear(vm.weak.keptAlive)
}

// FinalizationRegistry is the state of a FinalizationRegistry object.
type FinalizationRegistry struct {
	self weak.Pointer[FinalizationRegistry]

	// callback is the [[CleanupCallback]].
	callback *Object

	// context is the host context captured when the registry was created.
	context jobContext

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
// Go runtime adds to it, and the [VM] takes from it between jobs.
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
func (vm *VM) NewFinalizationRegistryObject(proto *Object, callback *Object) *Object {
	if vm.weak.finalization == nil {
		vm.weak.finalization = &finalizationQueue{}
	}
	r := &FinalizationRegistry{
		callback: callback,
		context:  vm.captureJobContext(),
		queue:    vm.weak.finalization,
		cells:    map[uint64]*finalizationCell{},
		cleanups: &cellCleanups{
			m: map[uint64]runtime.Cleanup{},
		},
	}
	r.self = weak.Make(r)
	runtime.AddCleanup(r, (*cellCleanups).stopAll, r.cleanups)
	o := vm.NewObject(proto)
	o.class = classFinalizationRegistry
	o.setExtState(r)
	return o
}

// FinalizationRegistryData returns the state of the FinalizationRegistry object v,
// or nil when v is not a FinalizationRegistry object.
func FinalizationRegistryData(v Value) *FinalizationRegistry {
	if v.IsObject() && v.o.class == classFinalizationRegistry {
		return v.o.extState().(*FinalizationRegistry)
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
		c.token = weak.Make(token.o)
		c.hasToken = true
		if r.tokens == nil {
			r.tokens = map[weak.Pointer[Object]][]uint64{}
		}
		c.tokenIndex = len(r.tokens[c.token])
		r.tokens[c.token] = append(r.tokens[c.token], id)
	}
	q := r.queue
	r.cleanups.m[id] = runtime.AddCleanup(target.o, func(d deadCell) {
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
	w := weak.Make(token.o)
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
func (vm *VM) finalizationPending() bool {
	q := vm.weak.finalization
	return q != nil && q.pending.Load()
}

// runFinalizationCleanups calls the cleanup callbacks for the targets that were collected,
// and reports whether any callback ran.
func (vm *VM) runFinalizationCleanups() bool {
	if !vm.finalizationPending() {
		return false
	}
	q := vm.weak.finalization
	ran := false
	for {
		if vm.interrupt.Load() {
			vm.Poll()
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
		vm.withJobContext(r.context, func() {
			if !vm.protect(func() {
				vm.Call(r.callback, Undefined, c.held)
			}) {
				thrown := vm.takeThrown()
				vm.reportUncaughtException(thrown, vm.thrownAt)
			}
		})
		vm.ResumeInterruption()
		if vm.abortCause != nil {
			return ran
		}
		vm.clearKeptObjects()
	}
}
