// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection

import (
	"fmt"

	"github.com/hajimehoshi/ekuma/internal/storage"
)

// EntryStorage returns the number of entries that the storage of t has room for.
func EntryStorage(t *Table) int {
	return t.capacity()
}

// BlockPointerStorage returns the number of pointers to blocks of entries that t has room for,
// and the first place of that storage, which identifies it, or nil when there is none.
func BlockPointerStorage(t *Table) (int, **[blockSize]entry) {
	if cap(t.blocks) == 0 {
		return 0, nil
	}
	return cap(t.blocks), &t.blocks[:1][0]
}

// SetHashSeeds sets the seeds of the hashes of the numbers, the short strings and the BigInts that
// fit in an int64, and returns a function that restores them. No table may outlive a change.
func SetHashSeeds(s0, s1 uint64) func() {
	old0, old1 := seed0, seed1
	seed0, seed1 = s0, s1
	return func() {
		seed0, seed1 = old0, old1
	}
}

// CheckSlotRoom returns an error when a probe for a missing key of t might not end.
func CheckSlotRoom(t *Table) error {
	if t.indexed && len(t.slots) < 2*t.capacity() {
		return fmt.Errorf("%d slots for storage of %d entries", len(t.slots), t.capacity())
	}
	return nil
}

// CheckInvariants returns an error when the structure of t is inconsistent.
func CheckInvariants(t *Table) error {
	if err := t.checkEntries(); err != nil {
		return err
	}
	if err := t.checkIndex(); err != nil {
		return err
	}
	if t.gen != nil && (t.gen.next != nil || t.gen.removed != nil || t.gen.cleared) {
		return fmt.Errorf("the current generation has ended")
	}
	return nil
}

// checkEntries checks the storage of the entries and their counts.
func (t *Table) checkEntries() error {
	if t.blocks != nil && t.small != nil {
		return fmt.Errorf("entries in both a slice and blocks")
	}
	if t.n > t.capacity() {
		return fmt.Errorf("%d entries in storage of %d", t.n, t.capacity())
	}
	if len(t.small) > blockSize {
		return fmt.Errorf("a slice of %d entries", len(t.small))
	}
	for i, b := range t.blocks[:cap(t.blocks)] {
		if (b != nil) != (i < len(t.blocks)) {
			return fmt.Errorf("block pointer %d of %d: nil is %t", i, len(t.blocks), b == nil)
		}
	}
	if cap(t.blocks)/4 > len(t.blocks) {
		return fmt.Errorf("room for %d block pointers, %d used", cap(t.blocks), len(t.blocks))
	}
	if !t.indexed && (t.blocks != nil || t.live > smallSize) {
		return fmt.Errorf("%d entries are not indexed, blocks is nil: %t", t.live, t.blocks == nil)
	}

	live, head := 0, 0
	for i := range t.n {
		e := t.at(i)
		if !e.deleted() {
			live++
			continue
		}
		if i == head {
			head++
		}
		if !e.value.IsUndefined() {
			return fmt.Errorf("the deleted entry %d keeps a value", i)
		}
	}
	for i := t.n; i < t.capacity(); i++ {
		if e := t.at(i); !e.key.IsUndefined() || !e.value.IsUndefined() {
			return fmt.Errorf("the unused entry %d of %d keeps a key or a value", i, t.n)
		}
	}
	if t.live != live || t.head != head {
		return fmt.Errorf("live = %d and head = %d, want %d and %d", t.live, t.head, live, head)
	}
	return nil
}

// checkIndex checks that the slots and the maps locate exactly the live entries.
func (t *Table) checkIndex() error {
	if !t.indexed {
		if t.slots != nil || t.strs != nil || t.objs != nil {
			return fmt.Errorf("a table that is not indexed keeps an index")
		}
		return nil
	}
	if err := CheckSlotRoom(t); err != nil {
		return err
	}
	if len(t.slots)&(len(t.slots)-1) != 0 {
		return fmt.Errorf("%d slots", len(t.slots))
	}

	// slotOfEntry holds the slot of each entry, or -1.
	slotOfEntry := make([]int, t.n)
	for i := range slotOfEntry {
		slotOfEntry[i] = -1
	}
	for s, sl := range t.slots {
		if sl.index < 0 {
			continue
		}
		if int(sl.index) >= t.n {
			return fmt.Errorf("slot %d locates the entry %d of %d", s, sl.index, t.n)
		}
		if o := slotOfEntry[sl.index]; o >= 0 {
			return fmt.Errorf("slots %d and %d locate the entry %d", o, s, sl.index)
		}
		slotOfEntry[sl.index] = s
	}

	strs, objs := 0, 0
	for i := range t.n {
		e := t.at(i)
		if e.deleted() {
			continue
		}
		if !hashed(e.key) {
			if slotOfEntry[i] >= 0 {
				return fmt.Errorf("a slot locates the entry %d, whose key is in a map", i)
			}
			var at int32
			var ok bool
			if e.key.IsString() {
				strs++
				at, ok = t.strs[e.key.Str()]
			} else {
				objs++
				at, ok = t.objs[e.key.Object()]
			}
			if !ok || int(at) != i {
				return fmt.Errorf("a map locates the entry %d at %d, found: %t", i, at, ok)
			}
			continue
		}
		if e.key.IsNumber() && e.key.Number() == 0 && !storage.SameValue(e.key, storage.Number(0)) {
			return fmt.Errorf("the entry %d has the key -0", i)
		}
		s := slotOfEntry[i]
		if s < 0 {
			return fmt.Errorf("no slot locates the entry %d", i)
		}
		h := hashKey(e.key)
		if t.slots[s].hash != h {
			return fmt.Errorf("the slot of the entry %d has the hash %#x, want %#x", i, t.slots[s].hash, h)
		}
		mask := len(t.slots) - 1
		for p := int(h) & mask; p != s; p = (p + 1) & mask {
			if t.slots[p].index < 0 {
				return fmt.Errorf("the empty slot %d cuts the entry %d off", p, i)
			}
		}
	}
	// The maps hold nothing else.
	if len(t.strs) != strs || len(t.objs) != objs {
		return fmt.Errorf("maps of %d strings and %d objects, want %d and %d", len(t.strs), len(t.objs), strs, objs)
	}
	return nil
}
