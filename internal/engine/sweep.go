// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

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
	entries map[K]sweptRef[V]

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
	r, ok := t.entries[key]
	if !ok {
		return nil
	}
	if r.strong != nil {
		return r.strong
	}
	return r.weak.Value()
}

// put sets the value under key to v, which must not be nil. A table that has fewer than
// minSweep entries is not swept.
func (t *sweptTable[K, V]) put(key K, v *V, minSweep int) {
	if t.entries == nil {
		t.entries = map[K]sweptRef[V]{}
	}
	if len(t.entries) >= max(t.sweepAt, minSweep) {
		t.sweep()
	}
	t.entries[key] = sweptRef[V]{strong: v}
}

// sweep removes the entries whose values were collected and makes the others weak.
func (t *sweptTable[K, V]) sweep() {
	// A weak pointer costs far more to make than a lookup, so only the entries that live
	// until a sweep get one.
	n := len(t.entries)
	for k, r := range t.entries {
		if r.strong != nil {
			t.entries[k] = sweptRef[V]{weak: weak.Make(r.strong)}
			continue
		}
		if r.weak.Value() == nil {
			delete(t.entries, k)
		}
	}
	// A map keeps the memory of its largest size, so a table that lost most of its entries
	// is copied to a new map.
	if len(t.entries) < n/2 {
		m := make(map[K]sweptRef[V], len(t.entries))
		maps.Copy(m, t.entries)
		t.entries = m
	}
	// The entries made weak now are counted as alive although many may be collected by the
	// next sweep. Growing by half before that keeps the size within about three times the
	// number of entries that stay alive, and the time of the sweeps within a constant per
	// insertion.
	n = len(t.entries)
	t.sweepAt = n + n/2
}
