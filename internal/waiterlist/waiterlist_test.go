// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package waiterlist_test

import (
	"math"
	"runtime"
	"slices"
	"testing"
	"time"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/waiterlist"
)

// TestCollectedWaiters checks that the waiters that only the lists refer to are collected, and
// that a sweep and RemoveFirst skip them.
func TestCollectedWaiters(t *testing.T) {
	// 40 waiters are added below. The first 32 of them fill the list to its first sweep.
	for _, c := range []struct {
		name string

		// dropAt is the number of the waiters that are added before the odd ones are collected.
		dropAt int
	}{
		{
			name:   "sweep",
			dropAt: 32,
		},
		{
			name:   "RemoveFirst",
			dropAt: 40,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var lists waiterlist.Lists[int]
			var held []*waiterlist.Waiter[int]
			var dropped []weak.Pointer[waiterlist.Waiter[int]]
			add := func(i int) {
				w := &waiterlist.Waiter[int]{
					Data: i,
				}
				lists.Add(4, w)
				if i < c.dropAt && i%2 != 0 {
					dropped = append(dropped, weak.Make(w))
					return
				}
				held = append(held, w)
			}
			for i := range c.dropAt {
				add(i)
			}
			deadline := time.Now().Add(10 * time.Second)
			for slices.ContainsFunc(dropped, func(w weak.Pointer[waiterlist.Waiter[int]]) bool {
				return w.Value() != nil
			}) {
				if time.Now().After(deadline) {
					t.Fatal("the dropped waiters were not collected")
				}
				runtime.GC()
			}
			for i := c.dropAt; i < 40; i++ {
				add(i)
			}

			got := lists.RemoveFirst(4, 5)
			if lists.Remove(got[0]) {
				t.Error("Remove of a removed waiter: got true, want false")
			}
			// The waiters must be removed at their new positions.
			if !lists.Remove(held[8]) {
				t.Error("Remove: got false, want true")
			}
			got = append(got, lists.RemoveFirst(4, math.Inf(1))...)
			if want := slices.Delete(slices.Clone(held), 8, 9); !slices.Equal(got, want) {
				t.Errorf("got %d waiters, want %d others in their order", len(got), len(want))
			}
			runtime.KeepAlive(held)
		})
	}
}
