// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"sync"
	"sync/atomic"
	"weak"
)

// This file holds the weak collections, WeakRef and FinalizationRegistry.
//
// The entries of a WeakMap or a WeakSet are stored on their keys, in objectMeta.weak, and the
// collection does not refer to its keys. An entry is then reachable only through its key, which
// gives the ephemeron behavior on the garbage collector of Go without any tracing of the engine:
// a value that refers to its own key does not keep the pair alive. The entries refer to their
// collection weakly, so that a key does not keep a collection alive.

// WeakTable is the [[WeakMapData]] of a WeakMap or the [[WeakSetData]] of a WeakSet.
// Every key must be a value for which CanBeHeldWeakly holds.
type WeakTable struct {
	self weak.Pointer[WeakTable]
}

// weakEntries are the entries of the weak collections whose key is one object or symbol.
type weakEntries struct {
	vals map[weak.Pointer[WeakTable]]Value

	// sweepAt is the number of entries at which an insertion first drops the entries
	// of the collections that were collected.
	sweepAt int
}

func newWeakTable() *WeakTable {
	t := &WeakTable{}
	t.self = weak.Make(t)
	return t
}

// NewWeakMapObject returns a WeakMap object without entries with the prototype proto.
func (vm *VM) NewWeakMapObject(proto *Object) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakMap
	o.ext = newWeakTable()
	return o
}

// WeakMapData returns the [[WeakMapData]] of the WeakMap object v, or nil when v is not a WeakMap object.
func WeakMapData(v Value) *WeakTable {
	if v.IsObject() && v.o.class == classWeakMap {
		return v.o.ext.(*WeakTable)
	}
	return nil
}

// NewWeakSetObject returns a WeakSet object without entries with the prototype proto.
func (vm *VM) NewWeakSetObject(proto *Object) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakSet
	o.ext = newWeakTable()
	return o
}

// WeakSetData returns the [[WeakSetData]] of the WeakSet object v, or nil when v is not a WeakSet object.
func WeakSetData(v Value) *WeakTable {
	if v.IsObject() && v.o.class == classWeakSet {
		return v.o.ext.(*WeakTable)
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
	m := k.o.meta
	if m == nil {
		return Undefined, false
	}
	v, ok := m.weak.vals[t.self]
	if !ok {
		return Undefined, false
	}
	return v, true
}

// Has reports whether t has an entry with the key k.
func (t *WeakTable) Has(k Value) bool {
	_, ok := t.Get(k)
	return ok
}

// Set sets the value of the entry with the key k.
func (t *WeakTable) Set(k, v Value) {
	m := k.o.meta
	if m == nil {
		m = &objectMeta{}
		k.o.meta = m
	}
	w := &m.weak
	if w.vals == nil {
		w.vals = map[weak.Pointer[WeakTable]]Value{}
	}
	if _, ok := w.vals[t.self]; !ok && len(w.vals) >= w.sweepAt {
		for c := range w.vals {
			if c.Value() == nil {
				delete(w.vals, c)
			}
		}
		w.sweepAt = max(8, 2*len(w.vals))
	}
	w.vals[t.self] = v
}

// Delete deletes the entry with the key k and reports whether there was one.
func (t *WeakTable) Delete(k Value) bool {
	m := k.o.meta
	if m == nil {
		return false
	}
	if _, ok := m.weak.vals[t.self]; !ok {
		return false
	}
	delete(m.weak.vals, t.self)
	return true
}

// NewWeakRefObject returns a WeakRef object with the prototype proto whose target is v,
// for which CanBeHeldWeakly must hold.
func (vm *VM) NewWeakRefObject(proto *Object, v Value) *Object {
	o := vm.NewObject(proto)
	o.class = classWeakRef
	o.ext = weak.Make(v.o)
	return o
}

// WeakRefDeref implements WeakRefDeref for the WeakRef object v.
// It reports false when v is not a WeakRef object.
func (vm *VM) WeakRefDeref(v Value) (Value, bool) {
	if !v.IsObject() || v.o.class != classWeakRef {
		return Undefined, false
	}
	t := v.o.ext.(weak.Pointer[Object]).Value()
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

	queue *finalizationQueue

	// cells are the registrations by their identifiers.
	cells map[uint64]*finalizationCell

	// tokens maps an unregister token to the identifiers of its cells.
	tokens map[weak.Pointer[Object]][]uint64

	lastID uint64
}

// finalizationCell is a record of the [[Cells]] of a FinalizationRegistry.
type finalizationCell struct {
	held     Value
	token    weak.Pointer[Object]
	hasToken bool
	cleanup  runtime.Cleanup
}

// finalizationQueue holds the cells whose target was collected. The cleanup goroutine of the
// Go runtime adds to it, and the VM takes from it between jobs.
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
		queue:    vm.weak.finalization,
		cells:    map[uint64]*finalizationCell{},
	}
	r.self = weak.Make(r)
	o := vm.NewObject(proto)
	o.class = classFinalizationRegistry
	o.ext = r
	return o
}

// FinalizationRegistryData returns the state of the FinalizationRegistry object v,
// or nil when v is not a FinalizationRegistry object.
func FinalizationRegistryData(v Value) *FinalizationRegistry {
	if v.IsObject() && v.o.class == classFinalizationRegistry {
		return v.o.ext.(*FinalizationRegistry)
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
		r.tokens[c.token] = append(r.tokens[c.token], id)
	}
	q := r.queue
	c.cleanup = runtime.AddCleanup(target.o, func(d deadCell) {
		q.push(d)
	}, deadCell{
		registry: r.self,
		id:       id,
	})
	r.cells[id] = c
}

// Unregister removes the cells whose unregister token is token and reports whether there was any.
// token must be a value for which CanBeHeldWeakly holds.
func (r *FinalizationRegistry) Unregister(token Value) bool {
	w := weak.Make(token.o)
	ids, ok := r.tokens[w]
	if !ok {
		return false
	}
	delete(r.tokens, w)
	for _, id := range ids {
		if c, ok := r.cells[id]; ok {
			c.cleanup.Stop()
			delete(r.cells, id)
		}
	}
	return len(ids) > 0
}

// remove removes the cell id, whose target was collected.
func (r *FinalizationRegistry) remove(id uint64, c *finalizationCell) {
	delete(r.cells, id)
	if !c.hasToken {
		return
	}
	ids := r.tokens[c.token]
	for i, x := range ids {
		if x == id {
			ids = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(ids) == 0 {
		delete(r.tokens, c.token)
	} else {
		r.tokens[c.token] = ids
	}
}

// runFinalizationCleanups calls the cleanup callbacks for the targets that were collected.
func (vm *VM) runFinalizationCleanups() {
	q := vm.weak.finalization
	if q == nil || !q.pending.Load() {
		return
	}
	for {
		d, ok := q.pop()
		if !ok {
			return
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
		vm.Call(r.callback, Undefined, c.held)
	}
}
