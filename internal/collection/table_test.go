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
		tab.Set(engine.Number(float64(i)), engine.Undefined)
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
		tab.Delete(engine.Number(float64(i)))
	}
	tab.Set(engine.Number(5), engine.Undefined)
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
	tab.Set(engine.Number(1), engine.Undefined)
	tab.Set(engine.Number(2), engine.Undefined)
	if got, want := rest(&tab, &c), []float64{1, 2}; !slices.Equal(got, want) {
		t.Errorf("after Clear: got %v, want %v", got, want)
	}
}

func TestSameValueZero(t *testing.T) {
	vm := engine.NewVM(nil)
	// Below and above the size at which the table indexes its keys.
	for _, filler := range []int{0, 20} {
		var tab collection.Table
		for i := range filler {
			tab.Set(engine.Number(float64(1000+i)), engine.Undefined)
		}
		tab.Set(engine.Number(math.Copysign(0, -1)), engine.Number(1))
		tab.Set(engine.Number(math.NaN()), engine.Number(2))
		tab.Set(vm.NewString("key"), engine.Number(3))
		o := engine.ObjectValue(vm.NewObject(nil))
		tab.Set(o, engine.Number(4))

		for _, tc := range []struct {
			key  engine.Value
			want float64
		}{
			{
				key:  engine.Number(0),
				want: 1,
			},
			{
				key:  engine.Number(math.Float64frombits(0x7ff8_0000_dead_beef)),
				want: 2,
			},
			{
				// A string with the same contents that is another object.
				key:  vm.NewString(wtf8.String([]byte("key"))),
				want: 3,
			},
			{
				key:  o,
				want: 4,
			},
		} {
			v, ok := tab.Get(tc.key)
			if !ok || v.Number() != tc.want {
				t.Errorf("filler %d: Get(%s) = %v, %t; want %v", filler, vm.DisplayString(tc.key), vm.DisplayString(v), ok, tc.want)
			}
		}
		if tab.Has(engine.ObjectValue(vm.NewObject(nil))) {
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
	vm := engine.NewVM(nil)
	big64 := func() engine.Value {
		return vm.NewBigInt(new(big.Int).Lsh(big.NewInt(1), 64))
	}
	// Below and above the size at which the table indexes its keys.
	for _, filler := range []int{0, 20} {
		var tab collection.Table
		for i := range filler {
			tab.Set(engine.Number(float64(1000+i)), engine.Undefined)
		}
		// The bits of -1n are those of a NaN, and the bits of the smallest int64 are those of -0.
		tab.Set(engine.BigIntFromInt64(-1), engine.Number(1))
		tab.Set(engine.BigIntFromInt64(math.MinInt64), engine.Number(2))
		tab.Set(big64(), engine.Number(3))
		tab.Set(vm.NewBigInt(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 64))), engine.Number(4))
		// A key of many bits is stored and found quickly.
		huge := new(big.Int).Lsh(big.NewInt(3), 1<<26)
		tab.Set(vm.NewBigInt(huge), engine.Number(5))

		for _, tc := range []struct {
			key  engine.Value
			want float64
		}{
			{
				key:  engine.BigIntFromInt64(-1),
				want: 1,
			},
			{
				key:  engine.BigIntFromInt64(math.MinInt64),
				want: 2,
			},
			{
				// A BigInt with the same value that is another object.
				key:  big64(),
				want: 3,
			},
			{
				key:  vm.NewBigInt(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 64))),
				want: 4,
			},
			{
				key:  vm.NewBigInt(new(big.Int).Set(huge)),
				want: 5,
			},
		} {
			v, ok := tab.Get(tc.key)
			if !ok || v.Number() != tc.want {
				t.Errorf("filler %d: Get of %d bits = %v, %t; want %v", filler, tc.key.BigInt().BitLen(), vm.DisplayString(v), ok, tc.want)
			}
		}
		if tab.Has(engine.BigIntFromInt64(0)) {
			t.Errorf("filler %d: 0n is found", filler)
		}
	}
}

// TestFindAfterDeletes tests that every live key is found while deleted entries are in the
// table, after the table grows, and in a clone.
func TestFindAfterDeletes(t *testing.T) {
	vm := engine.NewVM(nil)
	// A new string for every call, so that keys are found by their contents.
	key := func(i int) engine.Value {
		switch i % 3 {
		case 0:
			return engine.Number(float64(i))
		case 1:
			return vm.NewString(wtf8.String(strconv.Itoa(i)))
		default:
			return vm.NewString(wtf8.String("a key longer than sixteen bytes " + strconv.Itoa(i)))
		}
	}
	check := func(name string, tab *collection.Table, n int) {
		for i := range n {
			deleted := i < 300 && i%4 == 0
			if v, ok := tab.Get(key(i)); ok == deleted || ok && v.Number() != float64(i) {
				t.Errorf("%s: Get(%d) = %s, %t", name, i, vm.DisplayString(v), ok)
			}
		}
	}
	var tab collection.Table
	for i := range 300 {
		tab.Set(key(i), engine.Number(float64(i)))
	}
	// Too few deletions to compact the table.
	for i := 0; i < 300; i += 4 {
		tab.Delete(key(i))
	}
	check("after deletes", &tab, 300)
	for i := 300; i < 1000; i++ {
		tab.Set(key(i), engine.Number(float64(i)))
	}
	check("after growth", &tab, 1000)
	check("clone", tab.Clone(), 1000)
}

// TestGrowthByUnhashedKeys tests that number keys are found and missed when the storage of the
// entries grows only by keys that the slots do not locate.
func TestGrowthByUnhashedKeys(t *testing.T) {
	vm := engine.NewVM(nil)
	var tab collection.Table
	var numbers int
	for i := range 1000 {
		if tab.Size() == collection.EntryStorage(&tab) {
			tab.Set(vm.NewString(wtf8.String("a key longer than sixteen bytes "+strconv.Itoa(i))), engine.Undefined)
			continue
		}
		tab.Set(engine.Number(float64(numbers)), engine.Undefined)
		numbers++
	}
	for i := range numbers + 1 {
		if got, want := tab.Has(engine.Number(float64(i))), i < numbers; got != want {
			t.Errorf("Has(%d) = %t, want %t", i, got, want)
		}
	}
}

// TestStorageShrinks tests that deleting most entries gives up the storage that they used.
func TestStorageShrinks(t *testing.T) {
	var tab collection.Table
	for i := range 10000 {
		tab.Append(engine.Number(float64(i)), engine.Undefined)
	}
	for i := range 9990 {
		tab.Delete(engine.Number(float64(i)))
	}
	if got := collection.EntryStorage(&tab); got > 100 {
		t.Errorf("got storage for %d entries, want at most 100", got)
	}
	// The entries left are still found.
	if !tab.Has(engine.Number(9995)) {
		t.Errorf("Has(9995) = false, want true")
	}
}

// TestReserveBeforeEntries tests that a table that reserves room before its first entries finds
// them.
func TestReserveBeforeEntries(t *testing.T) {
	var tab collection.Table
	tab.Reserve(1000)
	for i := range 10 {
		tab.Set(engine.Number(float64(i)), engine.Undefined)
	}
	for i := range 11 {
		if got, want := tab.Has(engine.Number(float64(i))), i < 10; got != want {
			t.Errorf("Has(%d) = %t, want %t", i, got, want)
		}
	}
}

// TestCopyFromPolls tests that copying many entries polls and copies every entry.
func TestCopyFromPolls(t *testing.T) {
	var src collection.Table
	n := 2 * engine.PollInterval
	for i := range n {
		src.Append(engine.Number(float64(i)), engine.Undefined)
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
	if !dst.Has(engine.Number(float64(n - 1))) {
		t.Errorf("Has(%d) = false, want true", n-1)
	}
}

// TestQueue tests that a table from which the oldest entry is deleted for every new one finds
// its entries and keeps storage in proportion to them.
func TestQueue(t *testing.T) {
	const live = 1000
	var tab collection.Table
	var most int
	for i := range 100 * live {
		tab.Set(engine.Number(float64(i)), engine.Undefined)
		if i >= live {
			tab.Delete(engine.Number(float64(i - live)))
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
		if !tab.Has(engine.Number(float64(i))) {
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
			tab.Set(engine.Number(float64(i)), engine.Undefined)
		}
		tab.Set(engine.Undefined, engine.Undefined)
		if !tab.Delete(engine.Undefined) {
			t.Fatalf("filler %d: deleting undefined failed", filler)
		}
		if tab.Has(engine.Undefined) {
			t.Errorf("filler %d: deleted undefined key is still present", filler)
		}
		clone := tab.Clone()
		if clone.Has(engine.Undefined) {
			t.Errorf("filler %d: clone contains deleted undefined key", filler)
		}
		tab.Set(engine.Undefined, engine.Undefined)
		if v, ok := tab.Get(engine.Undefined); !ok || !v.IsUndefined() {
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
		tab.Set(engine.Number(float64(i)), engine.Undefined)
	}
	c := tab.Start()
	tab.Next(&c)
	tab.Next(&c)
	// Too few deletions to compact the table: the deleted entries stay in place.
	for _, k := range []float64{0, 1, 2, 3, 4, 7, 5, 6} {
		tab.Delete(engine.Number(k))
	}
	if got, want := rest(&tab, &c), seq(8, 20); !slices.Equal(got, want) {
		t.Errorf("after deleting the first entries: got %v, want %v", got, want)
	}
	tab.Clear()
	tab.Set(engine.Number(1), engine.Undefined)
	tab.Set(engine.Number(2), engine.Undefined)
	c = tab.Start()
	if got, want := rest(&tab, &c), []float64{1, 2}; !slices.Equal(got, want) {
		t.Errorf("after Clear: got %v, want %v", got, want)
	}
}
