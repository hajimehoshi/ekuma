// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"maps"
	"weak"
)

// sweptTable maps keys to values without keeping alive for good the values that nothing else
// refers to. The zero value is an empty table.
//
// A value is found under its key for as long as anything else refers to it. Once it was
// collected, its key is not found, and the entry is removed when the table is swept.
type sweptTable[K comparable, V any] struct {
	Entries map[K]sweptRef[V]

	// sweepAt is the number of entries at which the next insertion sweeps the table.
	sweepAt int
}

// sweptRef is the value of an entry of a [sweptTable]: strong until the first sweep after its
// insertion, and weak from then on.
type sweptRef[V any] struct {
	strong *V
	weak   weak.Pointer[V]
}

// get returns the value under key, or nil.
func (t *sweptTable[K, V]) get(key K) *V {
	r, ok := t.Entries[key]
	if !ok {
		return nil
	}
	if r.strong != nil {
		return r.strong
	}
	return r.weak.Value()
}

// put sets the value under key to v, which must not be nil. A table that has fewer than
// minSweep entries is not swept. The values for which keep reports true are kept alive by the
// table; keep may be nil.
func (t *sweptTable[K, V]) put(key K, v *V, minSweep int, keep func(*V) bool) {
	if t.Entries == nil {
		t.Entries = map[K]sweptRef[V]{}
	}
	if len(t.Entries) >= max(t.sweepAt, minSweep) {
		t.sweep(keep)
	}
	t.Entries[key] = sweptRef[V]{strong: v}
}

// sweep removes the entries whose values were collected and makes the others weak, except
// those for which keep reports true.
func (t *sweptTable[K, V]) sweep(keep func(*V) bool) {
	// A weak pointer costs far more to make than a lookup, so only the entries that live
	// until a sweep get one.
	n := len(t.Entries)
	for k, r := range t.Entries {
		if r.strong != nil {
			if keep != nil && keep(r.strong) {
				continue
			}
			t.Entries[k] = sweptRef[V]{weak: weak.Make(r.strong)}
			continue
		}
		if r.weak.Value() == nil {
			delete(t.Entries, k)
		}
	}
	// A map keeps the memory of its largest size, so a table that lost most of its entries
	// is copied to a new map.
	if len(t.Entries) < n/2 {
		m := make(map[K]sweptRef[V], len(t.Entries))
		maps.Copy(m, t.Entries)
		t.Entries = m
	}
	// The entries made weak now are counted as alive although many may be collected by the
	// next sweep. Growing by half before that keeps the size within about three times the
	// number of entries that stay alive, and the time of the sweeps within a constant per
	// insertion.
	n = len(t.Entries)
	t.sweepAt = n + n/2
}
