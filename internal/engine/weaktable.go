// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"sync"
	"weak"
)

// This file holds the entries of the weak collections.
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
	// The agent and the runtime cleanups of several collections can access the same key.
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
