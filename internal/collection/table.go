// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package collection implements the ordered tables of the keyed collections Map and Set.
package collection

import (
	"math"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// smallSize is the number of live entries up to which a table finds keys by a linear scan.
const smallSize = 4

// minDeleted is the number of deleted entries below which a table is never compacted.
const minDeleted = 8

// canonicalNaN is the key of every NaN.
const canonicalNaN = 0x7ff8000000000001

// Table is the list of entries of a Map or a Set, the [[MapData]] or [[SetData]] internal slot.
// Keys are compared with SameValueZero, and entries are kept in insertion order.
// The zero value is an empty table.
type Table struct {
	// entries holds the entries in insertion order, including the deleted ones that
	// compaction has not removed yet.
	entries []entry

	// live is the number of entries that are not deleted.
	live int

	// indexed says that the maps below locate every live entry by its key.
	indexed bool

	// nums, strs, bigs and objs map the keys to their indices in entries: numbers by the bits
	// of the canonical value, strings and BigInts by their contents, and everything else by identity.
	nums map[uint64]int32
	strs map[string]int32
	bigs map[string]int32
	objs map[*engine.Object]int32

	// gen is the generation that cursors start in, or nil when no cursor was ever created.
	gen *generation
}

type entry struct {
	key     engine.Value
	value   engine.Value
	deleted bool
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

// Size returns the number of entries.
func (t *Table) Size() int {
	return t.live
}

// find returns the index in entries of the live entry with the key k, or -1.
func (t *Table) find(k engine.Value) int {
	if !t.indexed {
		return t.scan(k)
	}
	var i int32
	var ok bool
	switch {
	case k.IsNumber():
		i, ok = t.nums[numberKey(k.Number())]
	case k.IsString():
		i, ok = t.strs[k.Object().Str()]
	case k.IsBigInt():
		i, ok = t.bigs[k.Object().BigInt().String()]
	default:
		i, ok = t.objs[k.Object()]
	}
	if !ok {
		return -1
	}
	return int(i)
}

// scan is find for a table that is not indexed. The key of a deleted entry is the number 0.
func (t *Table) scan(k engine.Value) int {
	switch {
	case k.IsNumber():
		n := numberKey(k.Number())
		for i := range t.entries {
			e := &t.entries[i]
			if e.key.IsNumber() && numberKey(e.key.Number()) == n && !e.deleted {
				return i
			}
		}
	case k.IsString():
		s := k.Object().Str()
		for i := range t.entries {
			if e := &t.entries[i]; e.key.IsString() && e.key.Object().Str() == s {
				return i
			}
		}
	case k.IsBigInt():
		for i := range t.entries {
			if e := &t.entries[i]; e.key.IsBigInt() && engine.SameValueZero(e.key, k) {
				return i
			}
		}
	default:
		o := k.Object()
		for i := range t.entries {
			if t.entries[i].key.Object() == o {
				return i
			}
		}
	}
	return -1
}

// index records that the key k is at the index i of entries.
func (t *Table) index(k engine.Value, i int) {
	switch {
	case k.IsNumber():
		if t.nums == nil {
			t.nums = map[uint64]int32{}
		}
		t.nums[numberKey(k.Number())] = int32(i)
	case k.IsString():
		if t.strs == nil {
			t.strs = map[string]int32{}
		}
		t.strs[k.Object().Str()] = int32(i)
	case k.IsBigInt():
		if t.bigs == nil {
			t.bigs = map[string]int32{}
		}
		t.bigs[k.Object().BigInt().String()] = int32(i)
	default:
		if t.objs == nil {
			t.objs = map[*engine.Object]int32{}
		}
		t.objs[k.Object()] = int32(i)
	}
}

// unindex forgets the location of the key k.
func (t *Table) unindex(k engine.Value) {
	switch {
	case k.IsNumber():
		delete(t.nums, numberKey(k.Number()))
	case k.IsString():
		delete(t.strs, k.Object().Str())
	case k.IsBigInt():
		delete(t.bigs, k.Object().BigInt().String())
	default:
		delete(t.objs, k.Object())
	}
}

// reindex rebuilds the maps from entries.
func (t *Table) reindex() {
	clear(t.nums)
	clear(t.strs)
	clear(t.bigs)
	clear(t.objs)
	for i := range t.entries {
		if e := &t.entries[i]; !e.deleted {
			t.index(e.key, i)
		}
	}
	t.indexed = true
}

// Get returns the value of the entry with the key k.
func (t *Table) Get(k engine.Value) (engine.Value, bool) {
	i := t.find(k)
	if i < 0 {
		return engine.Undefined, false
	}
	return t.entries[i].value, true
}

// Has reports whether the table has an entry with the key k.
func (t *Table) Has(k engine.Value) bool {
	return t.find(k) >= 0
}

// Set sets the value of the entry with the key k, appending a new entry when there is none.
// A key -0 is stored as +0.
func (t *Table) Set(k, v engine.Value) {
	if i := t.find(k); i >= 0 {
		t.entries[i].value = v
		return
	}
	t.Append(k, v)
}

// Append appends an entry with the key k, which the table must not have yet.
// A key -0 is stored as +0.
func (t *Table) Append(k, v engine.Value) {
	if k.IsNumber() && k.Number() == 0 {
		k = engine.Number(0)
	}
	t.entries = append(t.entries, entry{
		key:   k,
		value: v,
	})
	t.live++
	switch {
	case t.indexed:
		t.index(k, len(t.entries)-1)
	case t.live > smallSize:
		t.reindex()
	}
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

// deleteAt deletes the live entry at the index i of entries.
func (t *Table) deleteAt(i int) {
	e := &t.entries[i]
	if t.indexed {
		t.unindex(e.key)
	}
	*e = entry{deleted: true}
	t.live--
	if dead := len(t.entries) - t.live; dead >= minDeleted && dead >= t.live {
		t.compact()
	}
}

// compact removes the deleted entries.
func (t *Table) compact() {
	var removed []int32
	if t.gen != nil {
		removed = make([]int32, 0, len(t.entries)-t.live)
	}
	n := 0
	for i, e := range t.entries {
		if e.deleted {
			if t.gen != nil {
				removed = append(removed, int32(i))
			}
			continue
		}
		t.entries[n] = e
		n++
	}
	clear(t.entries[n:])
	t.entries = t.entries[:n]
	if t.gen != nil {
		next := &generation{}
		t.gen.removed = removed
		t.gen.next = next
		t.gen = next
	}
	if t.indexed {
		t.reindex()
	}
}

// Clear deletes every entry.
func (t *Table) Clear() {
	t.entries = nil
	t.live = 0
	t.indexed = false
	t.nums, t.strs, t.bigs, t.objs = nil, nil, nil, nil
	if t.gen != nil {
		next := &generation{}
		t.gen.cleared = true
		t.gen.next = next
		t.gen = next
	}
}

// Clone returns a new table with the same entries.
func (t *Table) Clone() *Table {
	c := &Table{
		entries: make([]entry, 0, t.live),
	}
	for _, e := range t.entries {
		if !e.deleted {
			c.entries = append(c.entries, e)
		}
	}
	c.live = len(c.entries)
	if c.live > smallSize {
		c.reindex()
	}
	return c
}

// Start returns a cursor at the first entry.
func (t *Table) Start() Cursor {
	if t.gen == nil {
		t.gen = &generation{}
	}
	return Cursor{gen: t.gen}
}

// Next returns the entry at the cursor c, which must have been created by Start of t,
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
	for c.pos < len(t.entries) {
		e := &t.entries[c.pos]
		c.pos++
		if !e.deleted {
			return e.key, e.value, true
		}
	}
	return engine.Undefined, engine.Undefined, false
}
