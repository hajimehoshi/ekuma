// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"
	"weak"
)

// WeakTable is the [[WeakMapData]] of a WeakMap or the [[WeakSetData]] of a WeakSet.
// Every key must be a value for which CanBeHeldWeakly holds.
type WeakTable struct {
	self weak.Pointer[WeakTable]
	keys *weakTableKeys

	prims *weakPrims
}

// weakTableKeys tracks the key stores that hold heap values for a table.
// It can outlive the table so its cleanup can release those values.
type weakTableKeys struct {
	mu sync.Mutex

	table   weak.Pointer[WeakTable]
	entries map[weak.Pointer[weakEntries]]struct{}
	sweepAt int
}

const minWeakSweep = 8

func (t *WeakTable) addKey(w weak.Pointer[weakEntries]) {
	if t.keys == nil {
		t.keys = &weakTableKeys{
			table:   t.self,
			entries: make(map[weak.Pointer[weakEntries]]struct{}),
			sweepAt: minWeakSweep,
		}
		runtime.AddCleanup(t, (*weakTableKeys).clear, t.keys)
	}
	t.keys.add(w)
}

func (keys *weakTableKeys) add(w weak.Pointer[weakEntries]) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	if len(keys.entries) >= keys.sweepAt {
		for key := range keys.entries {
			if key.Value() == nil {
				delete(keys.entries, key)
			}
		}
		keys.sweepAt = max(minWeakSweep, 2*len(keys.entries))
	}
	keys.entries[w] = struct{}{}
}

func (keys *weakTableKeys) remove(w weak.Pointer[weakEntries]) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	delete(keys.entries, w)
}

func (keys *weakTableKeys) clear() {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	for key := range keys.entries {
		if w := key.Value(); w != nil {
			w.delete(keys.table)
			runtime.KeepAlive(w)
		}
	}
	keys.entries = nil
}

// weakEntries belongs to one key. Heap values stay on the key so that a value
// that references its key cannot keep the key alive through the table.
type weakEntries struct {
	firstTable weak.Pointer[WeakTable]
	first      Value
	extra      *weakExtra
}

type weakExtra struct {
	// self is set by the agent before publishing the store to any table
	// cleanup. From then on mu guards firstTable, first and rest.
	self weak.Pointer[weakEntries]
	mu   sync.Mutex
	rest map[weak.Pointer[WeakTable]]Value

	// Only the agent uses the key's identity for the tables' primitive entries.
	key     weak.Pointer[Object]
	keyHash uint32
}

func (w *weakEntries) ensureExtra() *weakExtra {
	if w.extra == nil {
		w.extra = &weakExtra{}
	}
	return w.extra
}

// weakPrims holds the table's secondary primitive entries. Only the agent changes it.
type weakPrims struct {
	// slots holds one-based indices into the entries. A zero slot is empty.
	// Deleted entries keep their slot until reuse or a rebuild.
	slots  []int
	blocks [][]weakPrimEntry
	count  int // Includes deleted entries and collected keys until rebuild.
}

// weakPrimEntry holds a weak key and the kind and payload of a value with no
// collectable referent.
type weakPrimEntry struct {
	key  weak.Pointer[Object]
	n    float64
	hash uint32
	kind Kind
}

var weakPrimObjects = [...]*Object{
	KindNumber: NumberObject,
	KindNull:   NullObject,
	KindFalse:  FalseObject,
	KindTrue:   TrueObject,
	KindBigInt: BigInt64Object,
}

func isWeakPrimitive(v Value) bool {
	return v.O == nil || v.O.Kind <= KindTrue || v.O == BigInt64Object
}

var weakKeyID atomic.Uint64

func (ex *weakExtra) initKey(k *Object) {
	ex.key = weak.Make(k)
	ex.keyHash = max(1, uint32(weakKeyID.Add(1)*0x9e3779b97f4a7c15>>32))
}

// The first two blocks have eight entries each. Each later block starts at
// the index equal to its length, so the top bit of the index selects it.
const weakPrimBlockShift = 3

func (p *weakPrims) at(index int) *weakPrimEntry {
	block := p.blocks[bits.Len(uint(index>>weakPrimBlockShift))]
	return &block[index&(len(block)-1)]
}

// probe returns the matching bucket or a bucket available for insertion.
// An empty slot ends the probe. A deleted entry can be reused, but an existing
// match may be farther along the probe sequence.
func (p *weakPrims) probe(key weak.Pointer[Object], hash uint32) (int, bool) {
	mask := len(p.slots) - 1
	free := -1
	for slot := int(hash) & mask; ; slot = (slot + 1) & mask {
		index := p.slots[slot]
		if index == 0 {
			if free >= 0 {
				return free, false
			}
			return slot, false
		}
		entry := p.at(index - 1)
		if entry.hash == hash && entry.key == key {
			return slot, true
		}
		if free < 0 && entry.key == (weak.Pointer[Object]{}) {
			free = slot
		}
	}
}

func (p *weakPrims) grow() {
	var live int
	for index := range p.count {
		entry := p.at(index)
		if entry.key.Value() == nil {
			*entry = weakPrimEntry{}
			continue
		}
		if live != index {
			*p.at(live) = *entry
			*entry = weakPrimEntry{}
		}
		live++
	}
	p.count = live

	capacity := len(p.slots)
	if live*2 >= capacity {
		p.slots = make([]int, max(16, 2*capacity))
	} else {
		clear(p.slots)
	}
	for index := range live {
		entry := p.at(index)
		slot, _ := p.probe(entry.key, entry.hash)
		p.slots[slot] = index + 1
	}
}

func (p *weakPrims) set(ex *weakExtra, v Value) {
	if len(p.slots) == 0 {
		p.grow()
	}
	slot, found := p.probe(ex.key, ex.keyHash)
	if !found && p.slots[slot] == 0 && p.count >= len(p.slots)*7/10 {
		p.grow()
		slot, _ = p.probe(ex.key, ex.keyHash)
	}
	kind := KindUndefined
	if v.O != nil {
		kind = v.O.Kind
	}
	entry := weakPrimEntry{
		key:  ex.key,
		n:    v.N,
		hash: ex.keyHash,
		kind: kind,
	}
	if index := p.slots[slot]; index != 0 {
		*p.at(index - 1) = entry
		return
	}
	index := p.count
	if bits.Len(uint(index>>weakPrimBlockShift)) == len(p.blocks) {
		block := make([]weakPrimEntry, max(1<<weakPrimBlockShift, index))
		p.blocks = append(p.blocks, block)
	}
	*p.at(index) = entry
	p.count++
	p.slots[slot] = index + 1
}

func (t *WeakTable) setPrim(ex *weakExtra, v Value) {
	if t.prims == nil {
		t.prims = &weakPrims{}
	}
	t.prims.set(ex, v)
}

func (t *WeakTable) findPrim(ex *weakExtra) *weakPrimEntry {
	if p := t.prims; p != nil && ex != nil && ex.keyHash != 0 {
		if slot, found := p.probe(ex.key, ex.keyHash); found {
			return p.at(p.slots[slot] - 1)
		}
	}
	return nil
}

func (t *WeakTable) deletePrim(ex *weakExtra) bool {
	entry := t.findPrim(ex)
	if entry == nil {
		return false
	}
	*entry = weakPrimEntry{}
	return true
}

func NewWeakTable() *WeakTable {
	return &WeakTable{}
}

// Get returns the value of the entry with the key k.
func (t *WeakTable) Get(k Value) (Value, bool) {
	w := k.O.weak()
	if w == nil || t.self == (weak.Pointer[WeakTable]{}) {
		return Undefined, false
	}
	defer runtime.KeepAlive(t)
	if v, ok := w.get(t.self); ok {
		return v, true
	}
	if entry := t.findPrim(w.extra); entry != nil {
		return Value{
			N: entry.n,
			O: weakPrimObjects[entry.kind],
		}, true
	}
	return Undefined, false
}

func (w *weakEntries) get(table weak.Pointer[WeakTable]) (Value, bool) {
	ex := w.extra
	if ex != nil && ex.self != (weak.Pointer[weakEntries]{}) {
		ex.mu.Lock()
		defer ex.mu.Unlock()
	}
	if w.firstTable == table {
		return w.first, true
	}
	if ex == nil {
		return Undefined, false
	}
	v, ok := ex.rest[table]
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
	defer runtime.KeepAlive(k.O)
	if t.self == (weak.Pointer[WeakTable]{}) {
		t.self = weak.Make(t)
	}

	w := k.O.ensureWeak()
	primitive := isWeakPrimitive(v)
	wasTracked := w.set(t, k.O, v, primitive)
	if primitive {
		if wasTracked {
			t.keys.remove(w.extra.self)
		}
	} else if !wasTracked {
		ex := w.ensureExtra()
		if ex.self == (weak.Pointer[weakEntries]{}) {
			ex.self = weak.Make(w)
		}
		t.addKey(ex.self)
	}
}

// set reports whether the previous value needed table cleanup.
func (w *weakEntries) set(t *WeakTable, k *Object, v Value, primitive bool) bool {
	ex := w.extra
	if ex != nil && ex.self != (weak.Pointer[weakEntries]{}) {
		ex.mu.Lock()
		defer ex.mu.Unlock()
	}
	if w.firstTable == t.self {
		wasTracked := !isWeakPrimitive(w.first)
		w.first = v
		return wasTracked
	}
	if w.firstTable.Value() == nil {
		// This table might already have an entry outside the inline slot.
		var wasTracked bool
		if ex != nil && (t.prims != nil || ex.rest != nil) {
			_, wasTracked = ex.rest[t.self]
			delete(ex.rest, t.self)
			t.deletePrim(ex)
		}
		w.firstTable = t.self
		w.first = v
		return wasTracked
	}

	ex = w.ensureExtra()
	_, wasTracked := ex.rest[t.self]
	if primitive {
		delete(ex.rest, t.self)
		if ex.keyHash == 0 {
			ex.initKey(k)
		}
		t.setPrim(ex, v)
		return wasTracked
	}
	t.deletePrim(ex)
	if ex.rest == nil {
		ex.rest = make(map[weak.Pointer[WeakTable]]Value)
	}
	ex.rest[t.self] = v
	return wasTracked
}

// Delete deletes the entry with the key k and reports whether there was one.
func (t *WeakTable) Delete(k Value) bool {
	w := k.O.weak()
	if w == nil || t.self == (weak.Pointer[WeakTable]{}) {
		return false
	}
	defer runtime.KeepAlive(t)
	deleted, tracked := w.delete(t.self)
	if tracked {
		t.keys.remove(w.extra.self)
	}
	return deleted || t.deletePrim(w.extra)
}

// delete reports whether it deleted an entry and whether that entry needed table cleanup.
func (w *weakEntries) delete(table weak.Pointer[WeakTable]) (bool, bool) {
	ex := w.extra
	if ex != nil && ex.self != (weak.Pointer[weakEntries]{}) {
		ex.mu.Lock()
		defer ex.mu.Unlock()
	}
	if w.firstTable == table {
		tracked := !isWeakPrimitive(w.first)
		w.firstTable = weak.Pointer[WeakTable]{}
		w.first = Undefined
		return true, tracked
	}
	if ex != nil {
		if _, ok := ex.rest[table]; ok {
			delete(ex.rest, table)
			return true, true
		}
	}
	return false, false
}
