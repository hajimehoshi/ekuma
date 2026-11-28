// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"maps"
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

	// room is the largest number of entries that a check has seen since entries was made.
	room  int
	usage weakUsage
}

const minWeakSweep = 8

// minShrinkWeakMap is the room up to which a map of weak entries is kept however few entries
// remain.
const minShrinkWeakMap = 64

// weakMapOversized reports whether a map with room for room entries is large enough and at most
// a quarter used by its n entries.
func weakMapOversized(n, room int) bool {
	return room > minShrinkWeakMap && n < room/4
}

// shrunk returns a copy of m with room for n entries.
func shrunk[M ~map[K]V, K comparable, V any](m M, n int) M {
	c := make(M, n)
	maps.Copy(c, m)
	return c
}

// gcEpoch counts the cycles of garbage collection that the weak tables have seen end.
var gcEpoch struct {
	mu   sync.Mutex
	n    uint32
	mark weak.Pointer[gcMark]
}

// gcMark is referred to only weakly, so that the next cycle of garbage collection collects it.
// Its pointer keeps it out of the tiny allocations that share their memory with other objects.
type gcMark struct {
	_ *byte
}

// currentGCEpoch returns a count that has grown by one if a cycle of garbage collection ended
// since it last grew.
func currentGCEpoch() uint32 {
	gcEpoch.mu.Lock()
	defer gcEpoch.mu.Unlock()
	if gcEpoch.mark.Value() == nil {
		gcEpoch.n++
		gcEpoch.mark = weak.Make(&gcMark{})
	}
	return gcEpoch.n
}

// weakUsage tracks the insertions into the storage of a weak table, so that the storage gives up
// only room that it has not needed for a whole cycle of garbage collection.
type weakUsage struct {
	inserts int    // The insertions since the last check that counted them.
	checked uint32 // The GC epoch of that check.
}

// weakCheckInterval is the number of insertions between checks of a weak table's storage. It is
// a power of two.
const weakCheckInterval = 256

// inserted counts an insertion and reports whether the storage should be checked.
func (u *weakUsage) inserted() bool {
	u.inserts++
	return u.inserts&(weakCheckInterval-1) == 0
}

// recentInserts returns the insertions since the last call that returned them, and reports
// whether a whole cycle of garbage collection has passed since then. Only a call that reports
// true starts a new count.
func (u *weakUsage) recentInserts() (int, bool) {
	epoch := currentGCEpoch()
	if epoch-u.checked < 2 {
		return 0, false
	}
	n := u.inserts
	u.inserts, u.checked = 0, epoch
	return n, true
}

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
		keys.sweep()
	}
	keys.entries[w] = struct{}{}
	if keys.usage.inserted() {
		keys.check()
	}
}

func (keys *weakTableKeys) remove(w weak.Pointer[weakEntries]) {
	keys.mu.Lock()
	defer keys.mu.Unlock()
	delete(keys.entries, w)
}

// sweep drops the stores of collected keys. keys.mu must be held.
func (keys *weakTableKeys) sweep() {
	for key := range keys.entries {
		if key.Value() == nil {
			delete(keys.entries, key)
		}
	}
	keys.sweepAt = max(minWeakSweep, 2*len(keys.entries))
}

// check moves the entries to a smaller map when neither they nor the recent insertions need
// most of the room. keys.mu must be held.
func (keys *weakTableKeys) check() {
	keys.room = max(keys.room, len(keys.entries))
	if keys.room <= minShrinkWeakMap {
		return
	}
	recent, ok := keys.usage.recentInserts()
	if !ok || !weakMapOversized(recent, keys.room) {
		return
	}
	if !weakMapOversized(len(keys.entries)+recent, keys.room) && !weakMapOversized(keys.sampledLive()+recent, keys.room) {
		return
	}
	keys.sweep()
	if need := len(keys.entries) + recent; weakMapOversized(need, keys.room) {
		keys.entries = shrunk(keys.entries, need)
		keys.room = need
	}
}

// sampledLive estimates the number of stores whose keys are alive from a few of them.
func (keys *weakTableKeys) sampledLive() int {
	const samples = 8
	var seen, alive int
	for key := range keys.entries {
		if key.Value() != nil {
			alive++
		}
		if seen++; seen == samples {
			break
		}
	}
	if seen == 0 {
		return 0
	}
	return alive * len(keys.entries) / seen
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

	// restRoom is the largest number of entries in rest since it was made, once that is more
	// than minShrinkWeakMap.
	restRoom int

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

func (ex *weakExtra) setRest(table weak.Pointer[WeakTable], v Value) {
	if ex.rest == nil {
		ex.rest = make(map[weak.Pointer[WeakTable]]Value)
	}
	ex.rest[table] = v
	if n := len(ex.rest); n > minShrinkWeakMap {
		ex.restRoom = max(ex.restRoom, n)
	}
}

func (ex *weakExtra) deleteRest(table weak.Pointer[WeakTable]) {
	delete(ex.rest, table)
	if weakMapOversized(len(ex.rest), ex.restRoom) {
		ex.rest = shrunk(ex.rest, len(ex.rest))
		ex.restRoom = len(ex.rest)
	}
}

// weakPrims holds the table's secondary primitive entries. Only the agent changes it.
type weakPrims struct {
	// slots holds one-based indices into the entries. A zero slot is empty.
	// Deleted entries keep their slot until reuse or a rebuild.
	slots   []int
	blocks  [][]weakPrimEntry
	count   int // Includes deleted entries and collected keys until rebuild.
	deleted int // The deleted entries among count.
	usage   weakUsage
}

// minWeakPrimSlots is the smallest number of slots of a table's primitive entries.
const minWeakPrimSlots = 16

// minShrinkWeakPrimSlots is the number of slots up to which a table keeps the storage of its
// primitive entries however few entries remain.
const minShrinkWeakPrimSlots = 64

// weakPrimRoom returns the number of entries that a table with the given number of slots holds
// until its next rebuild.
func weakPrimRoom(slots int) int {
	return slots * 7 / 10
}

// oversized reports whether the slots are large enough and n entries use at most a quarter of
// their room.
func (p *weakPrims) oversized(n int) bool {
	return len(p.slots) > minShrinkWeakPrimSlots && n < weakPrimRoom(len(p.slots))/4
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

// rebuild drops the deleted entries and those of collected keys, and sizes the slots for the
// entries that remain. When shrink is true, it also gives up the room beyond what the remaining
// entries and recent insertions need.
func (p *weakPrims) rebuild(recent int, shrink bool) {
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
	p.deleted = 0

	size := len(p.slots)
	switch {
	case live*2 >= size:
		size = max(minWeakPrimSlots, 2*size)
	case shrink && p.oversized(live+recent):
		// The smallest power of two above twice the entries and insertions.
		size = max(minWeakPrimSlots, 1<<bits.Len(uint(2*(live+recent))))
	}
	if size != len(p.slots) {
		p.slots = make([]int, size)
	} else {
		clear(p.slots)
	}
	// No entry is stored past the room before the next rebuild.
	if n := weakPrimBlockCount(weakPrimRoom(size)); n < len(p.blocks) {
		clear(p.blocks[n:])
		p.blocks = p.blocks[:n]
	}
	for index := range live {
		entry := p.at(index)
		slot, _ := p.probe(entry.key, entry.hash)
		p.slots[slot] = index + 1
	}
}

// weakPrimBlockCount returns the number of blocks that hold the first n entries. n must be
// positive.
func weakPrimBlockCount(n int) int {
	return bits.Len(uint((n-1)>>weakPrimBlockShift)) + 1
}

// sampledLive estimates the number of entries whose keys are alive from a few of them.
func (p *weakPrims) sampledLive() int {
	const samples = 8
	if p.count == 0 {
		return 0
	}
	var alive int
	for i := range samples {
		if p.at((2*i+1)*p.count/(2*samples)).key.Value() != nil {
			alive++
		}
	}
	return alive * p.count / samples
}

// check rebuilds the table into smaller storage when neither its entries nor the recent
// insertions need most of the room.
func (p *weakPrims) check() {
	if len(p.slots) <= minShrinkWeakPrimSlots {
		return
	}
	recent, ok := p.usage.recentInserts()
	if !ok || !p.oversized(recent) {
		return
	}
	if p.oversized(p.count-p.deleted+recent) || p.oversized(p.sampledLive()+recent) {
		p.rebuild(recent, true)
	}
}

func (p *weakPrims) set(ex *weakExtra, v Value) {
	if len(p.slots) == 0 {
		p.rebuild(0, false)
	}
	slot, found := p.probe(ex.key, ex.keyHash)
	if !found && p.slots[slot] == 0 && p.count >= weakPrimRoom(len(p.slots)) {
		var recent int
		var shrink bool
		if len(p.slots) > minShrinkWeakPrimSlots {
			recent, shrink = p.usage.recentInserts()
		}
		p.rebuild(recent, shrink)
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
		if found {
			return
		}
		// The probe found a deleted entry to reuse.
		p.deleted--
	} else {
		p.appendEntry(slot, entry)
	}
	if p.usage.inserted() {
		p.check()
	}
}

// appendEntry stores entry after the others and puts its index in slot.
func (p *weakPrims) appendEntry(slot int, entry weakPrimEntry) {
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
	return t.prims != nil && t.prims.delete(ex)
}

// delete deletes the entry of the key of ex and reports whether there was one.
func (p *weakPrims) delete(ex *weakExtra) bool {
	if ex == nil || ex.keyHash == 0 {
		return false
	}
	slot, found := p.probe(ex.key, ex.keyHash)
	if !found {
		return false
	}
	*p.at(p.slots[slot] - 1) = weakPrimEntry{}
	p.deleted++
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
			if _, wasTracked = ex.rest[t.self]; wasTracked {
				ex.deleteRest(t.self)
			}
			t.deletePrim(ex)
		}
		w.firstTable = t.self
		w.first = v
		return wasTracked
	}

	ex = w.ensureExtra()
	_, wasTracked := ex.rest[t.self]
	if primitive {
		if wasTracked {
			ex.deleteRest(t.self)
		}
		if ex.keyHash == 0 {
			ex.initKey(k)
		}
		t.setPrim(ex, v)
		return wasTracked
	}
	t.deletePrim(ex)
	ex.setRest(t.self, v)
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
			ex.deleteRest(table)
			return true, true
		}
	}
	return false, false
}
