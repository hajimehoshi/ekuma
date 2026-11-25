// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection_test

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// rest returns the keys from the cursor c on as numbers.
func rest(t *collection.Table, c *collection.Cursor) []float64 {
	var out []float64
	for {
		k, _, ok := t.Next(c)
		if !ok {
			return out
		}
		out = append(out, k.Number())
	}
}

func seq(from, to int) []float64 {
	var out []float64
	for i := from; i < to; i++ {
		out = append(out, float64(i))
	}
	return out
}

func TestCursorAcrossCompaction(t *testing.T) {
	var tab collection.Table
	for i := range 100 {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
	}
	c := tab.Start()
	if got := rest(&tab, &c); len(got) != 100 {
		t.Fatalf("got %d keys, want 100", len(got))
	}
	c = tab.Start()
	for range 10 {
		tab.Next(&c)
	}
	// Deleting most entries compacts the table several times, and a deleted key that is
	// added again moves to the end.
	for i := range 80 {
		tab.Delete(storage.Number(float64(i)))
	}
	tab.Set(storage.Number(5), storage.Undefined)
	want := append(seq(80, 100), 5)
	if got := rest(&tab, &c); !slices.Equal(got, want) {
		t.Errorf("after compaction: got %v, want %v", got, want)
	}
	if got := tab.Size(); got != 21 {
		t.Errorf("Size: got %d, want 21", got)
	}

	c = tab.Start()
	tab.Next(&c)
	tab.Clear()
	tab.Set(storage.Number(1), storage.Undefined)
	tab.Set(storage.Number(2), storage.Undefined)
	if got, want := rest(&tab, &c), []float64{1, 2}; !slices.Equal(got, want) {
		t.Errorf("after Clear: got %v, want %v", got, want)
	}
}

func TestSameValueZero(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	// Below and above the size at which the table indexes its keys.
	for _, filler := range []int{0, 20} {
		var tab collection.Table
		for i := range filler {
			tab.Set(storage.Number(float64(1000+i)), storage.Undefined)
		}
		tab.Set(storage.Number(math.Copysign(0, -1)), storage.Number(1))
		tab.Set(storage.Number(math.NaN()), storage.Number(2))
		tab.Set(cx.NewString("key"), storage.Number(3))
		o := storage.ObjectValue(cx.NewObject(nil))
		tab.Set(o, storage.Number(4))

		for _, tc := range []struct {
			key  engine.Value
			want float64
		}{
			{
				key:  storage.Number(0),
				want: 1,
			},
			{
				key:  storage.Number(math.Float64frombits(0x7ff8_0000_dead_beef)),
				want: 2,
			},
			{
				// A string with the same contents that is another object.
				key:  cx.NewString(wtf8.String([]byte("key"))),
				want: 3,
			},
			{
				key:  o,
				want: 4,
			},
		} {
			v, ok := tab.Get(tc.key)
			if !ok || v.Number() != tc.want {
				t.Errorf("filler %d: Get(%s) = %v, %t; want %v", filler, cx.DisplayString(tc.key), cx.DisplayString(v), ok, tc.want)
			}
		}
		if tab.Has(storage.ObjectValue(cx.NewObject(nil))) {
			t.Errorf("filler %d: another object is found", filler)
		}
		c := tab.Start()
		for {
			k, _, ok := tab.Next(&c)
			if !ok {
				t.Errorf("filler %d: no zero key", filler)
				break
			}
			if k.IsNumber() && k.Number() == 0 {
				if math.Signbit(k.Number()) {
					t.Errorf("filler %d: the key -0 is not stored as +0", filler)
				}
				break
			}
		}
	}
}

func TestSameValueZeroBigInts(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	big64 := func() engine.Value {
		return storage.NewBigInt(new(big.Int).Lsh(big.NewInt(1), 64))
	}
	// Below and above the size at which the table indexes its keys.
	for _, filler := range []int{0, 20} {
		var tab collection.Table
		for i := range filler {
			tab.Set(storage.Number(float64(1000+i)), storage.Undefined)
		}
		// The bits of -1n are those of a NaN, and the bits of the smallest int64 are those of -0.
		tab.Set(storage.BigIntFromInt64(-1), storage.Number(1))
		tab.Set(storage.BigIntFromInt64(math.MinInt64), storage.Number(2))
		tab.Set(big64(), storage.Number(3))
		tab.Set(storage.NewBigInt(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 64))), storage.Number(4))
		// A key of many bits is stored and found quickly.
		huge := new(big.Int).Lsh(big.NewInt(3), 1<<26)
		tab.Set(storage.NewBigInt(huge), storage.Number(5))

		for _, tc := range []struct {
			key  engine.Value
			want float64
		}{
			{
				key:  storage.BigIntFromInt64(-1),
				want: 1,
			},
			{
				key:  storage.BigIntFromInt64(math.MinInt64),
				want: 2,
			},
			{
				// A BigInt with the same value that is another object.
				key:  big64(),
				want: 3,
			},
			{
				key:  storage.NewBigInt(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 64))),
				want: 4,
			},
			{
				key:  storage.NewBigInt(new(big.Int).Set(huge)),
				want: 5,
			},
		} {
			v, ok := tab.Get(tc.key)
			if !ok || v.Number() != tc.want {
				t.Errorf("filler %d: Get of %d bits = %v, %t; want %v", filler, tc.key.BigInt().BitLen(), cx.DisplayString(v), ok, tc.want)
			}
		}
		if tab.Has(storage.BigIntFromInt64(0)) {
			t.Errorf("filler %d: 0n is found", filler)
		}
	}
}

// TestFindAfterDeletes tests that every live key is found while deleted entries are in the
// table, after the table grows, and in a clone.
func TestFindAfterDeletes(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	// A new string for every call, so that keys are found by their contents.
	key := func(i int) engine.Value {
		switch i % 3 {
		case 0:
			return storage.Number(float64(i))
		case 1:
			return cx.NewString(wtf8.String(strconv.Itoa(i)))
		default:
			return cx.NewString(wtf8.String("a key longer than sixteen bytes " + strconv.Itoa(i)))
		}
	}
	check := func(name string, tab *collection.Table, n int) {
		for i := range n {
			deleted := i < 300 && i%4 == 0
			if v, ok := tab.Get(key(i)); ok == deleted || ok && v.Number() != float64(i) {
				t.Errorf("%s: Get(%d) = %s, %t", name, i, cx.DisplayString(v), ok)
			}
		}
	}
	var tab collection.Table
	for i := range 300 {
		tab.Set(key(i), storage.Number(float64(i)))
	}
	// Too few deletions to compact the table.
	for i := 0; i < 300; i += 4 {
		tab.Delete(key(i))
	}
	check("after deletes", &tab, 300)
	for i := 300; i < 1000; i++ {
		tab.Set(key(i), storage.Number(float64(i)))
	}
	check("after growth", &tab, 1000)
	check("clone", tab.Clone(nil), 1000)
}

// TestGrowthByUnhashedKeys tests that number keys are found and missed when the storage of the
// entries grows only by keys that the slots do not locate.
func TestGrowthByUnhashedKeys(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	var tab collection.Table
	var numbers int
	for i := range 1000 {
		if tab.Size() == collection.EntryStorage(&tab) {
			tab.Set(cx.NewString(wtf8.String("a key longer than sixteen bytes "+strconv.Itoa(i))), storage.Undefined)
			continue
		}
		tab.Set(storage.Number(float64(numbers)), storage.Undefined)
		numbers++
	}
	for i := range numbers + 1 {
		if got, want := tab.Has(storage.Number(float64(i))), i < numbers; got != want {
			t.Errorf("Has(%d) = %t, want %t", i, got, want)
		}
	}
}

// TestStorageShrinks tests that deleting most entries gives up the storage that they used.
func TestStorageShrinks(t *testing.T) {
	var tab collection.Table
	for i := range 10000 {
		tab.Append(storage.Number(float64(i)), storage.Undefined)
	}
	for i := range 9990 {
		tab.Delete(storage.Number(float64(i)))
	}
	if got := collection.EntryStorage(&tab); got > 100 {
		t.Errorf("got storage for %d entries, want at most 100", got)
	}
	// The entries left are still found.
	if !tab.Has(storage.Number(9995)) {
		t.Errorf("Has(9995) = false, want true")
	}
}

// TestReserveBeforeEntries tests that a table that reserves room before its first entries finds
// them.
func TestReserveBeforeEntries(t *testing.T) {
	var tab collection.Table
	tab.Reserve(1000)
	for i := range 10 {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
	}
	for i := range 11 {
		if got, want := tab.Has(storage.Number(float64(i))), i < 10; got != want {
			t.Errorf("Has(%d) = %t, want %t", i, got, want)
		}
	}
}

// TestCopyFromPolls tests that copying many entries polls and copies every entry.
func TestCopyFromPolls(t *testing.T) {
	var src collection.Table
	n := 2 * engine.PollInterval
	for i := range n {
		src.Append(storage.Number(float64(i)), storage.Undefined)
	}
	var polls int
	var dst collection.Table
	dst.CopyFrom(&src, func() {
		polls++
	})
	if polls == 0 {
		t.Errorf("no polls while copying %d entries", n)
	}
	if got := dst.Size(); got != n {
		t.Errorf("Size: got %d, want %d", got, n)
	}
	if !dst.Has(storage.Number(float64(n - 1))) {
		t.Errorf("Has(%d) = false, want true", n-1)
	}
}

func TestCopyFromAfterDeletingAllEntries(t *testing.T) {
	var src, dst collection.Table
	src.Set(storage.Number(1), storage.Number(42))
	dst.Set(storage.Number(2), storage.Undefined)
	cursor := dst.Start()
	dst.Next(&cursor)
	dst.Delete(storage.Number(2))
	dst.CopyFrom(&src, nil)
	key, value, ok := dst.Next(&cursor)
	if !ok || key.Number() != 1 || value.Number() != 42 {
		t.Errorf("next copied entry = (%v, %v, %t), want (1, 42, true)", key, value, ok)
	}
}

func TestReserveAfterDeletingAllEntries(t *testing.T) {
	var tab collection.Table
	for i := range 8 {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
	}
	for i := range 8 {
		tab.Delete(storage.Number(float64(i)))
	}
	tab.Reserve(64)
	for i := range 64 {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
	}
	if got := tab.Size(); got != 64 {
		t.Errorf("size = %d, want 64", got)
	}
}

// TestShrinkGivesUpBlockPointers tests that a table that had room for many entries gives up the
// storage of its pointers to blocks, after Trim and after compacting, for one in proportion to its
// entries.
func TestShrinkGivesUpBlockPointers(t *testing.T) {
	const peak, left = 64 * 1024, 100
	for _, tt := range []struct {
		name   string
		fill   func(tab *collection.Table)
		shrink func(tab *collection.Table)
	}{
		{
			name: "trim",
			fill: func(tab *collection.Table) {
				tab.Reserve(peak)
			},
			shrink: func(tab *collection.Table) {
				for i := range left {
					tab.Set(storage.Number(float64(i)), storage.Undefined)
				}
				tab.Trim()
			},
		},
		{
			name: "compact",
			fill: func(tab *collection.Table) {
				for i := range peak {
					tab.Set(storage.Number(float64(i)), storage.Undefined)
				}
			},
			shrink: func(tab *collection.Table) {
				// Deleting most entries compacts the table.
				for i := range peak - left {
					tab.Delete(storage.Number(float64(i)))
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var tab collection.Table
			tt.fill(&tab)
			_, before := collection.BlockPointerStorage(&tab)
			tt.shrink(&tab)
			n, after := collection.BlockPointerStorage(&tab)
			if after == before {
				t.Errorf("kept the storage of the block pointers for %d entries", peak)
			}
			if max := 16; n > max {
				t.Errorf("got room for %d block pointers, want at most %d", n, max)
			}
		})
	}
}

// TestQueue tests that a table from which the oldest entry is deleted for every new one finds
// its entries and keeps storage in proportion to them.
func TestQueue(t *testing.T) {
	const live = 1000
	var tab collection.Table
	var most int
	for i := range 100 * live {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
		if i >= live {
			tab.Delete(storage.Number(float64(i - live)))
		}
		most = max(most, collection.EntryStorage(&tab))
	}
	if most > 3*live {
		t.Errorf("got storage for up to %d entries, want at most %d", most, 3*live)
	}
	if got := tab.Size(); got != live {
		t.Errorf("Size: got %d, want %d", got, live)
	}
	var missing int
	for i := 99 * live; i < 100*live; i++ {
		if !tab.Has(storage.Number(float64(i))) {
			missing++
		}
	}
	if missing > 0 {
		t.Errorf("%d of the last %d keys are not found", missing, live)
	}
}

func TestDeletedUndefinedKey(t *testing.T) {
	for _, filler := range []int{0, 20} {
		var tab collection.Table
		for i := range filler {
			tab.Set(storage.Number(float64(i)), storage.Undefined)
		}
		tab.Set(storage.Undefined, storage.Undefined)
		if !tab.Delete(storage.Undefined) {
			t.Fatalf("filler %d: deleting undefined failed", filler)
		}
		if tab.Has(storage.Undefined) {
			t.Errorf("filler %d: deleted undefined key is still present", filler)
		}
		clone := tab.Clone(nil)
		if clone.Has(storage.Undefined) {
			t.Errorf("filler %d: clone contains deleted undefined key", filler)
		}
		tab.Set(storage.Undefined, storage.Undefined)
		if v, ok := tab.Get(storage.Undefined); !ok || !v.IsUndefined() {
			t.Errorf("filler %d: reinserted undefined key is missing or has the wrong value", filler)
		}
		c := tab.Start()
		for range filler {
			tab.Next(&c)
		}
		if k, v, ok := tab.Next(&c); !ok || !k.IsUndefined() || !v.IsUndefined() {
			t.Errorf("filler %d: reinserted undefined entry is not last", filler)
		}
		if _, _, ok := tab.Next(&c); ok {
			t.Errorf("filler %d: iteration returned a deleted entry", filler)
		}
	}
}

func TestCursorAfterFrontDeletes(t *testing.T) {
	var tab collection.Table
	for i := range 20 {
		tab.Set(storage.Number(float64(i)), storage.Undefined)
	}
	c := tab.Start()
	tab.Next(&c)
	tab.Next(&c)
	// Too few deletions to compact the table: the deleted entries stay in place.
	for _, k := range []float64{0, 1, 2, 3, 4, 7, 5, 6} {
		tab.Delete(storage.Number(k))
	}
	if got, want := rest(&tab, &c), seq(8, 20); !slices.Equal(got, want) {
		t.Errorf("after deleting the first entries: got %v, want %v", got, want)
	}
	tab.Clear()
	tab.Set(storage.Number(1), storage.Undefined)
	tab.Set(storage.Number(2), storage.Undefined)
	c = tab.Start()
	if got, want := rest(&tab, &c), []float64{1, 2}; !slices.Equal(got, want) {
		t.Errorf("after Clear: got %v, want %v", got, want)
	}
}
