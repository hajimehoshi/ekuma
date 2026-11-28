// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/storage"
)

func newKeys(n int) []storage.Value {
	keys := make([]storage.Value, n)
	for i := range keys {
		keys[i] = storage.Value{O: storage.NewObject(nil)}
	}
	return keys
}

// useLightly deletes and sets the entries of keys in t a few times in each of several cycles of
// garbage collection, and stops early once done reports true. It returns the values it set last.
func useLightly(t *storage.WeakTable, keys []storage.Value, value func(i int) storage.Value, done func() bool) []storage.Value {
	last := make([]storage.Value, len(keys))
	for range 10 {
		runtime.GC()
		if done() {
			break
		}
		for i := range 128 {
			j := i % len(keys)
			t.Delete(keys[j])
			last[j] = value(i)
			t.Set(keys[j], last[j])
		}
	}
	return last
}

// TestWeakTableReleasesPrimitiveEntries tests that a table gives up the storage of its secondary
// primitive entries once most of them were deleted or their keys collected, and the table has
// been used only lightly for a while.
func TestWeakTableReleasesPrimitiveEntries(t *testing.T) {
	for _, collect := range []bool{false, true} {
		name := "deleted"
		if collect {
			name = "collected"
		}
		t.Run(name, func(t *testing.T) {
			const n, kept = 4000, 8
			// first holds the first entry of each key, so that the entries of table are secondary.
			first, table := storage.NewWeakTable(), storage.NewWeakTable()
			keys := newKeys(n)
			for i, k := range keys {
				first.Set(k, storage.Number(0))
				table.Set(k, storage.Number(float64(i)))
			}
			slots, block := storage.WeakPrimStorage(table)
			if collect {
				clear(keys[2*kept:])
			} else {
				for _, k := range keys[2*kept:] {
					table.Delete(k)
				}
			}
			steady, toggled := keys[:kept], keys[kept:2*kept]

			last := useLightly(table, toggled, func(i int) storage.Value {
				return storage.Number(float64(i))
			}, func() bool {
				return slots.Value() == nil && block.Value() == nil
			})
			if slots.Value() != nil {
				t.Error("the old slots are still reachable")
			}
			if block.Value() != nil {
				t.Error("the last old block is still reachable")
			}
			for i, k := range steady {
				if v, ok := table.Get(k); !ok || v.Number() != float64(i) {
					t.Errorf("steady entry %d: got %v, %t, want %d, true", i, v.Number(), ok, i)
				}
			}
			for i, k := range toggled {
				if v, ok := table.Get(k); !ok || !storage.SameValue(v, last[i]) {
					t.Errorf("toggled entry %d: got %v, %t, want %v, true", i, v.Number(), ok, last[i].Number())
				}
			}
			runtime.KeepAlive(first)
		})
	}
}

// TestWeakTableShrinkKeepsValueCleanup tests that a table that gave up the storage for its
// deleted heap values still releases the remaining values once it is collected.
func TestWeakTableShrinkKeepsValueCleanup(t *testing.T) {
	const n, kept = 4000, 8
	table := storage.NewWeakTable()
	keys := newKeys(n)
	for _, k := range keys {
		table.Set(k, storage.Value{O: storage.NewObject(nil)})
	}
	for _, k := range keys[2*kept:] {
		table.Delete(k)
	}
	keys = keys[:2*kept]
	useLightly(table, keys[kept:], func(int) storage.Value {
		return storage.Value{O: storage.NewObject(nil)}
	}, func() bool {
		return false
	})

	values := make([]weak.Pointer[storage.Object], len(keys))
	for i, k := range keys {
		v, _ := table.Get(k)
		values[i] = weak.Make(v.O)
	}
	table = nil
	released := func() bool {
		for _, v := range values {
			if v.Value() != nil {
				return false
			}
		}
		return true
	}
	// The values are released by a cleanup after the table is collected.
	for i := 0; i < 20 && !released(); i++ {
		runtime.GC()
	}
	if !released() {
		t.Error("the values of a collected table are still reachable")
	}
	runtime.KeepAlive(keys)
}
