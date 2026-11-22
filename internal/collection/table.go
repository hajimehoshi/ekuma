// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package collection implements the ordered tables of the keyed collections Map and Set.
package collection

import (
	"encoding/binary"
	"hash/maphash"
	"math"
	"math/big"
	"math/bits"
	"math/rand/v2"
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// smallSize is the number of live entries up to which a table finds keys by a linear scan.
const smallSize = 4

// minDeleted is the number of deleted entries below which a table is never compacted.
const minDeleted = 8

// minShrinkEntries is the capacity up to which a table keeps the storage of its entries however
// few entries remain.
const minShrinkEntries = 64

// blockShift is the base-2 logarithm of blockSize.
const blockShift = 6

// blockSize is the number of entries of a block. A table that has room for at most blockSize
// entries keeps them in one slice instead of blocks.
const blockSize = 1 << blockShift

// minSlots is the smallest number of slots of an indexed table.
const minSlots = 8

// shortKeyBytes is the length in bytes up to which the slots locate a string key.
const shortKeyBytes = 16

// canonicalNaN is the key of every NaN.
const canonicalNaN = 0x7ff8000000000001

// The seeds of the hashes of keys, which differ from process to process.
var (
	seed         = maphash.MakeSeed()
	seed0, seed1 = rand.Uint64(), rand.Uint64() | 1
)

// Table is the list of entries of a Map or a Set, the [[MapData]] or [[SetData]] internal slot.
// Keys are compared with SameValueZero, and entries are kept in insertion order.
// The zero value is an empty table.
type Table struct {
	// small holds the entries while the table has room for at most blockSize of them, and
	// blocks from then on, when small is nil. The entries are in insertion order, including the
	// deleted ones that compaction has not removed yet. The storage after the entries is zero.
	small  []entry
	blocks []*[blockSize]entry

	// n is the number of entries, including the deleted ones.
	n int

	// live is the number of entries that are not deleted.
	live int

	// head is the number of leading entries that are all deleted.
	head int

	// indexed says that slots, strs and objs locate every live entry by its key. Only an
	// indexed table keeps its entries in blocks.
	indexed bool

	// slots is an open-addressing table of the entries whose keys are numbers, strings of at
	// most shortKeyBytes bytes or BigInts, probed linearly from the hash of a key. Its length
	// is a power of two of at least twice the capacity of the storage of the entries. The slot
	// of a deleted entry stays until the slots are rebuilt.
	slots []slot

	// strs maps the longer strings to their indices in entries by their contents, and objs
	// every other key by identity.
	strs map[wtf8.String]int32
	objs map[*engine.Object]int32

	// gen is the generation that cursors start in, or nil when no cursor was ever created.
	gen *generation
}

// deletedKey is private to this package and cannot be supplied as a collection key.
var deletedKey = new(engine.Object)

// deletedEntry is what a deleted entry becomes.
var deletedEntry = entry{
	key: engine.ObjectValue(deletedKey),
}

type entry struct {
	key   engine.Value
	value engine.Value
}

// slot locates the entry at index, whose key has the hash hash. An empty slot has the index -1.
type slot struct {
	hash  uint32
	index int32
}

func (e *entry) deleted() bool {
	return !e.key.IsString() && e.key.Object() == deletedKey
}

// entryAt returns the entry at the index i of the storage small or blocks of a table.
func entryAt(small []entry, blocks []*[blockSize]entry, i int) *entry {
	if blocks == nil {
		return &small[i]
	}
	return &blocks[i>>blockShift][i&(blockSize-1)]
}

// at returns the entry at the index i.
func (t *Table) at(i int) *entry {
	return entryAt(t.small, t.blocks, i)
}

// capacity returns the number of entries that the storage has room for.
func (t *Table) capacity() int {
	if t.blocks == nil {
		return len(t.small)
	}
	return len(t.blocks) << blockShift
}

// grow adds room for at least one more entry.
func (t *Table) grow() {
	if t.blocks == nil && len(t.small) < blockSize {
		t.resizeSmall(min(max(2*len(t.small), 1), blockSize))
		return
	}
	t.toBlocks()
	t.blocks = append(t.blocks, new([blockSize]entry))
}

// resizeSmall moves the entries of a table without blocks to storage with room for n entries.
func (t *Table) resizeSmall(n int) {
	small := make([]entry, n)
	copy(small, t.small[:t.n])
	t.small = small
}

// toBlocks moves the entries into blocks unless they are in blocks already.
func (t *Table) toBlocks() {
	if t.blocks != nil {
		return
	}
	var b *[blockSize]entry
	if len(t.small) == blockSize {
		b = (*[blockSize]entry)(t.small)
	} else {
		b = new([blockSize]entry)
		copy(b[:], t.small[:t.n])
	}
	t.blocks = append(t.blocks, b)
	t.small = nil
}

// setStorage gives an empty table storage with room for n entries.
func (t *Table) setStorage(n int) {
	t.small, t.blocks = nil, nil
	if n <= blockSize {
		if n > 0 {
			t.small = make([]entry, n)
		}
		return
	}
	t.blocks = make([]*[blockSize]entry, (n+blockSize-1)>>blockShift)
	for i := range t.blocks {
		t.blocks[i] = new([blockSize]entry)
	}
}

// generation is a period of the life of a table between two compactions.
// A cursor that was positioned in a generation that ended translates its position
// through the following generations.
type generation struct {
	// next is the generation that followed this one, or nil for the current one.
	next *generation

	// removed lists the positions of the deleted entries that the compaction ending this
	// generation removed, in increasing order.
	removed []int32

	// cleared says that the generation ended because the table was cleared.
	cleared bool
}

// Cursor is a position in a table that stays valid across insertions, deletions,
// compactions and clearing, as the iteration over the list of entries in the specification does.
type Cursor struct {
	gen *generation
	pos int
}

// numberKey returns the map key of the number n: -0 is the same key as +0 and every NaN is one key.
func numberKey(n float64) uint64 {
	if n == 0 {
		return 0
	}
	if n != n {
		return canonicalNaN
	}
	return math.Float64bits(n)
}

// bigKey appends the map key of x to buf and returns the result: its sign and the bytes of its
// magnitude.
func bigKey(buf []byte, x *big.Int) []byte {
	n := (x.BitLen() + 7) / 8
	buf = slices.Grow(buf, 1+n)[:1+n]
	buf[0] = byte(x.Sign() + 1)
	x.FillBytes(buf[1:])
	return buf
}

// Size returns the number of entries.
func (t *Table) Size() int {
	return t.live
}

// find returns the index of the live entry with the key k, or -1.
func (t *Table) find(k engine.Value) int {
	switch {
	case !t.indexed:
		return t.scan(k)
	case k.IsNumber():
		n := numberKey(k.Number())
		i, _ := t.probeNumber(n, hashNumber(n))
		return i
	case hashed(k):
		i, _ := t.probe(k, hashKey(k))
		return i
	}
	var i int32
	var ok bool
	if k.IsString() {
		i, ok = t.strs[k.Str()]
	} else {
		i, ok = t.objs[k.Object()]
	}
	if !ok {
		return -1
	}
	return int(i)
}

// locate returns the hash of the key k, which the slots locate, and what a probe for k returns.
func (t *Table) locate(k engine.Value) (h uint32, i int, s uint32) {
	if k.IsNumber() {
		n := numberKey(k.Number())
		h = hashNumber(n)
		i, s = t.probeNumber(n, h)
		return h, i, s
	}
	h = hashKey(k)
	i, s = t.probe(k, h)
	return h, i, s
}

// hashed reports whether the slots, rather than strs or objs, locate the key k.
func hashed(k engine.Value) bool {
	return k.IsNumber() || k.IsBigInt() || k.IsString() && k.StringByteLength() <= shortKeyBytes
}

// mapIndex records that the key k, which the slots do not locate, is at the index i.
func (t *Table) mapIndex(k engine.Value, i int) {
	if k.IsString() {
		if t.strs == nil {
			t.strs = map[wtf8.String]int32{}
		}
		t.strs[k.Str()] = int32(i)
		return
	}
	if t.objs == nil {
		t.objs = map[*engine.Object]int32{}
	}
	t.objs[k.Object()] = int32(i)
}

// mapUnindex forgets the location of the key k, which the slots do not locate.
func (t *Table) mapUnindex(k engine.Value) {
	if k.IsString() {
		delete(t.strs, k.Str())
		return
	}
	delete(t.objs, k.Object())
}

// mix mixes a and b into a hash.
func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return hi ^ lo
}

// hashNumber returns the hash of a number key whose [numberKey] is n.
func hashNumber(n uint64) uint32 {
	return uint32(mix(n^seed0, seed1))
}

// hashKey returns the hash of the key k, which the slots locate. Keys that are the same for
// SameValueZero have the same hash.
func hashKey(k engine.Value) uint32 {
	switch {
	case k.IsNumber():
		return hashNumber(numberKey(k.Number()))
	case k.IsString():
		n := k.StringByteLength()
		// The bytes after the string stay zero, and the length tells the strings apart that
		// differ only in trailing zero bytes.
		var buf [shortKeyBytes]byte
		k.AppendStringBytes(buf[:0])
		a, b := binary.LittleEndian.Uint64(buf[:8]), binary.LittleEndian.Uint64(buf[8:])
		return uint32(mix(a^seed0, b^seed1^uint64(n)))
	default:
		if n, ok := k.BigInt64(); ok {
			return uint32(mix(uint64(n)^seed1, seed0))
		}
		var buf [64]byte
		return uint32(maphash.Bytes(seed, bigKey(buf[:0], k.BigInt())))
	}
}

// probeNumber looks for the live entry with the number key whose [numberKey] is n and whose
// hash is h. It returns the index of the entry, or -1 and the empty slot where the probe ended.
func (t *Table) probeNumber(n uint64, h uint32) (int, uint32) {
	slots, small, blocks := t.slots, t.small, t.blocks
	mask := uint32(len(slots) - 1)
	// The hash is compared first, so that a probe reads the entries only for a likely match.
	// The number keys are stored as numberKey returns them.
	for s := h & mask; ; s = (s + 1) & mask {
		sl := slots[s]
		if sl.index < 0 {
			return -1, s
		}
		if sl.hash != h {
			continue
		}
		if a := entryAt(small, blocks, int(sl.index)).key; a.IsNumber() && math.Float64bits(a.Number()) == n {
			return int(sl.index), 0
		}
	}
}

// probe is probeNumber for the key k, which is a string or a BigInt that the slots locate.
func (t *Table) probe(k engine.Value, h uint32) (int, uint32) {
	slots, small, blocks := t.slots, t.small, t.blocks
	mask := uint32(len(slots) - 1)
	s := h & mask
	if k.IsString() {
		for ; ; s = (s + 1) & mask {
			sl := slots[s]
			if sl.index < 0 {
				return -1, s
			}
			if sl.hash == h && engine.IsStrictlyEqual(entryAt(small, blocks, int(sl.index)).key, k) {
				return int(sl.index), 0
			}
		}
	}
	if x, ok := k.BigInt64(); ok {
		for ; ; s = (s + 1) & mask {
			sl := slots[s]
			if sl.index < 0 {
				return -1, s
			}
			if sl.hash != h {
				continue
			}
			if y, ok := entryAt(small, blocks, int(sl.index)).key.BigInt64(); ok && y == x {
				return int(sl.index), 0
			}
		}
	}
	for ; ; s = (s + 1) & mask {
		sl := slots[s]
		if sl.index < 0 {
			return -1, s
		}
		if sl.hash != h {
			continue
		}
		if a := entryAt(small, blocks, int(sl.index)).key; a.IsBigInt() && engine.SameValueZero(a, k) {
			return int(sl.index), 0
		}
	}
}

// scan is find for a table that is not indexed.
func (t *Table) scan(k engine.Value) int {
	entries := t.small[:t.n]
	switch {
	case k.IsNumber():
		n := numberKey(k.Number())
		for i := range entries {
			e := &entries[i]
			if e.key.IsNumber() && numberKey(e.key.Number()) == n && !e.deleted() {
				return i
			}
		}
	case k.IsString():
		for i := range entries {
			if e := &entries[i]; engine.IsStrictlyEqual(e.key, k) {
				return i
			}
		}
	case k.IsBigInt():
		for i := range entries {
			if e := &entries[i]; e.key.IsBigInt() && engine.SameValueZero(e.key, k) {
				return i
			}
		}
	default:
		o := k.Object()
		for i := range entries {
			if e := &entries[i]; !e.deleted() && !e.key.IsNumber() && !e.key.IsString() && e.key.Object() == o {
				return i
			}
		}
	}
	return -1
}

// slotCount returns the number of slots for storage with room for n entries.
func slotCount(n int) int {
	c := minSlots
	for c < 2*n {
		c *= 2
	}
	return c
}

// reindex locates every live entry again, with as many slots as the storage of the entries calls
// for.
func (t *Table) reindex() {
	clear(t.strs)
	clear(t.objs)
	t.slots = emptySlots(t.slots, slotCount(t.capacity()))
	mask := uint32(len(t.slots) - 1)
	for i := range t.n {
		e := t.at(i)
		switch {
		case e.deleted():
		case hashed(e.key):
			h := hashKey(e.key)
			s := h & mask
			for t.slots[s].index >= 0 {
				s = (s + 1) & mask
			}
			t.slots[s] = slot{
				hash:  h,
				index: int32(i),
			}
		default:
			t.mapIndex(e.key, i)
		}
	}
	t.indexed = true
}

// growSlots moves the slots of the live entries into as many slots as the storage of the entries
// calls for.
func (t *Table) growSlots() {
	old := t.slots
	t.slots = emptySlots(nil, slotCount(t.capacity()))
	mask := uint32(len(t.slots) - 1)
	for _, sl := range old {
		if sl.index < 0 || t.at(int(sl.index)).deleted() {
			continue
		}
		s := sl.hash & mask
		for t.slots[s].index >= 0 {
			s = (s + 1) & mask
		}
		t.slots[s] = sl
	}
}

// emptySlots returns n empty slots, in the storage of slots when it is large enough and not
// much larger.
func emptySlots(slots []slot, n int) []slot {
	if cap(slots) >= n && cap(slots) <= 2*n {
		slots = slots[:n]
	} else {
		slots = make([]slot, n)
	}
	for i := range slots {
		slots[i] = slot{
			index: -1,
		}
	}
	return slots
}

// Get returns the value of the entry with the key k.
func (t *Table) Get(k engine.Value) (engine.Value, bool) {
	i := t.find(k)
	if i < 0 {
		return engine.Undefined, false
	}
	return t.at(i).value, true
}

// Has reports whether the table has an entry with the key k.
func (t *Table) Has(k engine.Value) bool {
	return t.find(k) >= 0
}

// Set sets the value of the entry with the key k, appending a new entry when there is none.
// A key -0 is stored as +0.
func (t *Table) Set(k, v engine.Value) {
	if !t.indexed || !hashed(k) {
		if i := t.find(k); i >= 0 {
			t.at(i).value = v
			return
		}
		t.Append(k, v)
		return
	}
	h, i, s := t.locate(k)
	if i >= 0 {
		t.at(i).value = v
		return
	}
	t.appendHashed(k, v, h, s)
}

// Append appends an entry with the key k, which the table must not have yet.
// A key -0 is stored as +0.
func (t *Table) Append(k, v engine.Value) {
	if t.indexed && hashed(k) {
		h, _, s := t.locate(k)
		t.appendHashed(k, v, h, s)
		return
	}
	if k.IsNumber() {
		k = engine.Number(math.Float64frombits(numberKey(k.Number())))
	}
	t.makeRoom()
	*t.at(t.n) = entry{
		key:   k,
		value: v,
	}
	t.n++
	t.live++
	switch {
	case t.indexed:
		t.mapIndex(k, t.n-1)
	case t.live > smallSize:
		t.reindex()
	}
}

// appendHashed appends an entry with the key k of the hash h, which the table must not have
// yet and whose probe ended at the slot s.
func (t *Table) appendHashed(k, v engine.Value, h, s uint32) {
	if k.IsNumber() {
		// probeNumber compares the stored bits.
		k = engine.Number(math.Float64frombits(numberKey(k.Number())))
	}
	if t.makeRoom() {
		_, _, s = t.locate(k)
	}
	*t.at(t.n) = entry{
		key:   k,
		value: v,
	}
	t.slots[s] = slot{
		hash:  h,
		index: int32(t.n),
	}
	t.n++
	t.live++
}

// makeRoom makes room for one more entry, and reports whether the slots were rebuilt.
func (t *Table) makeRoom() bool {
	if t.n < t.capacity() {
		return false
	}
	if dead := t.n - t.live; dead >= minDeleted && dead >= t.live {
		// The storage of the deleted entries makes the room.
		t.compact()
		return t.indexed
	}
	t.grow()
	if !t.indexed || slotCount(t.capacity()) <= len(t.slots) {
		return false
	}
	t.growSlots()
	return true
}

// Delete deletes the entry with the key k and reports whether there was one.
func (t *Table) Delete(k engine.Value) bool {
	i := t.find(k)
	if i < 0 {
		return false
	}
	t.deleteAt(i)
	return true
}

// deleteAt deletes the live entry at the index i.
func (t *Table) deleteAt(i int) {
	e := t.at(i)
	if t.indexed && !hashed(e.key) {
		t.mapUnindex(e.key)
	}
	*e = deletedEntry
	t.live--
	if i == t.head {
		for t.head < t.n && t.at(t.head).deleted() {
			t.head++
		}
	}
	// Iterations skip the deleted entries before head at once, so a table whose deleted entries
	// are mostly before head is compacted only to give up storage.
	if dead := t.n - t.live; dead >= minDeleted && dead >= t.live && (dead-t.head >= t.live || t.oversized()) {
		t.compact()
	}
}

// oversized reports whether the storage of the entries is large enough and at most a quarter
// used by live entries, so that compaction gives up most of it.
func (t *Table) oversized() bool {
	return t.capacity() > minShrinkEntries && t.live < t.capacity()/4
}

// compact removes the deleted entries.
func (t *Table) compact() {
	var removed []int32
	if t.gen != nil {
		removed = make([]int32, 0, t.n-t.live)
	}
	n := 0
	for i := range t.n {
		e := t.at(i)
		if e.deleted() {
			if t.gen != nil {
				removed = append(removed, int32(i))
			}
			continue
		}
		if n != i {
			*t.at(n) = *e
		}
		n++
	}
	for i := n; i < t.n; i++ {
		*t.at(i) = entry{}
	}
	t.n = n
	t.head = 0
	// The maps keep their room until they are rebuilt.
	shrink := t.oversized()
	if shrink {
		t.shrink()
	}
	if t.gen != nil {
		next := &generation{}
		t.gen.removed = removed
		t.gen.next = next
		t.gen = next
	}
	if t.indexed {
		if shrink {
			t.strs = withRoom(t.strs, len(t.strs))
			t.objs = withRoom(t.objs, len(t.objs))
		}
		t.reindex()
	}
}

// shrink moves the entries to storage with room for twice as many.
func (t *Table) shrink() {
	if want := 2 * t.n; want <= blockSize {
		small, blocks := t.small, t.blocks
		t.setStorage(want)
		for i := range t.n {
			t.small[i] = *entryAt(small, blocks, i)
		}
		return
	}
	t.trimBlocks(2 * t.n)
}

// trimBlocks gives up the blocks beyond those that n entries need.
func (t *Table) trimBlocks(n int) {
	k := (n + blockSize - 1) >> blockShift
	clear(t.blocks[k:])
	t.blocks = slices.Clip(t.blocks[:k])
}

// Clear deletes every entry.
func (t *Table) Clear() {
	t.small, t.blocks = nil, nil
	t.n = 0
	t.live = 0
	t.head = 0
	t.indexed = false
	t.slots, t.strs, t.objs = nil, nil, nil
	if t.gen != nil {
		next := &generation{}
		t.gen.cleared = true
		t.gen.next = next
		t.gen = next
	}
}

// Clone returns a new table with the same entries.
func (t *Table) Clone() *Table {
	c := &Table{}
	c.setStorage(t.live)
	for i := range t.n {
		if e := t.at(i); !e.deleted() {
			*c.at(c.n) = *e
			c.n++
		}
	}
	c.live = c.n
	if c.live > smallSize {
		// The maps of t hold the keys of its live entries only.
		c.strs = withRoom(t.strs, len(t.strs))
		c.objs = withRoom(t.objs, len(t.objs))
		c.reindex()
	}
	return c
}

// Reserve makes room for n entries in all, for keys of the kinds of the present keys in the
// same proportions.
func (t *Table) Reserve(n int) {
	if n <= t.capacity() {
		return
	}
	if t.blocks == nil && n <= blockSize {
		t.resizeSmall(n)
	} else {
		t.toBlocks()
		for t.capacity() < n {
			t.blocks = append(t.blocks, new([blockSize]entry))
		}
		if !t.indexed {
			t.reindex()
			return
		}
	}
	if !t.indexed || t.live == 0 {
		return
	}
	share := func(kind int) int {
		return int(int64(n) * int64(kind) / int64(t.live))
	}
	t.strs = withRoom(t.strs, share(len(t.strs)))
	t.objs = withRoom(t.objs, share(len(t.objs)))
	t.reindex()
}

// Trim gives up the room for entries that is mostly unused.
func (t *Table) Trim() {
	if t.capacity() < 2*t.n+minDeleted {
		return
	}
	switch {
	case t.blocks == nil:
		t.resizeSmall(t.n)
	case t.n <= blockSize:
		blocks := t.blocks
		t.setStorage(t.n)
		for i := range t.n {
			t.small[i] = blocks[0][i]
		}
	default:
		t.trimBlocks(t.n)
	}
	if t.indexed {
		t.strs = withRoom(t.strs, len(t.strs))
		t.objs = withRoom(t.objs, len(t.objs))
		t.reindex()
	}
}

// withRoom returns a new empty map with room for n keys when m has keys, and nil otherwise.
func withRoom[K comparable](m map[K]int32, n int) map[K]int32 {
	if len(m) == 0 {
		return nil
	}
	return make(map[K]int32, n)
}

// Start returns a cursor at the first entry.
func (t *Table) Start() Cursor {
	if t.gen == nil {
		t.gen = &generation{}
	}
	return Cursor{gen: t.gen}
}

// Next returns the entry at the cursor c, which must have been created by [Table.Start] of t,
// and moves c past it. It reports false when there is no entry left.
func (t *Table) Next(c *Cursor) (key, value engine.Value, ok bool) {
	for c.gen.next != nil {
		g := c.gen
		if g.cleared {
			c.pos = 0
		} else {
			c.pos -= sort.Search(len(g.removed), func(i int) bool {
				return int(g.removed[i]) >= c.pos
			})
		}
		c.gen = g.next
	}
	// The entries before head are deleted.
	c.pos = max(c.pos, t.head)
	for c.pos < t.n {
		e := t.at(c.pos)
		c.pos++
		if !e.deleted() {
			return e.key, e.value, true
		}
	}
	return engine.Undefined, engine.Undefined, false
}
