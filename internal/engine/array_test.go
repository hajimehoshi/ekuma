// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// TestArrayWithLengthFilledFromEnd tests that filling an array created with its length from the
// end keeps every element in the storage of the elements, which needs no key for an index.
func TestArrayWithLengthFilledFromEnd(t *testing.T) {
	cx := newTestContext(t)
	before := engine.InternedCount(cx)
	if _, err := runScript(cx, "fill", `o = new Array(500); for (i = 499; i >= 0; i--) o[i] = i;`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := engine.InternedCount(cx); got != before {
		t.Errorf("got %d interned strings, want %d", got, before)
	}
}

func TestPushCapacityAfterLongRuns(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "build", `
		function build(n, a = []) {
			while (a.length < n) a.push(a.length);
			return a;
		}
		build(256);
		build(256);
		// Leave a 64-element shifted prefix in the old backing storage.
		const shifted = new Array(128).fill(0);
		for (let i = 0; i < 64; i++) shifted.shift();
		shifted.length = 0;
		build(8, shifted);
	`)
	if err != nil {
		t.Fatal(err)
	}
	// Filling the first grown buffer can reserve space based on past runs.
	if got := engine.ElementStorage(v); got <= 8 || got > 64 {
		t.Errorf("capacity after long runs: got %d, want more than 8 and at most 64", got)
	}
	v, err = runScript(cx, "short", "build(8)")
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.ElementStorage(v); got > 8 {
		t.Errorf("capacity after a short run: got %d, want at most 8", got)
	}
}

func TestNumericArrayOperationsKeepPackedStorage(t *testing.T) {
	const setup = `
		function numbers() {
			const a = [];
			for (let i = 0; i < 41; i++) a[i] = i;
			return a;
		}
	`
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "spread after a numeric prefix",
			src:  `[-1, ...numbers(), ...[41, 42]]`,
			want: `a.length === 44 && a[0] === -1 && a[41] === 40 && a[43] === 42`,
		},
		{
			name: "spread into call arguments",
			src: `(() => {
				const a = numbers();
				function f(x) { a[0] = 100; return x + arguments[40]; }
				if (f(...a) !== 40) throw Error();
				return a;
			})()`,
			want: `a.length === 41 && a[0] === 100`,
		},
		{
			name: "multiple arguments to push across growth",
			src: `(() => {
				const a = [];
				for (let i = 0; i < 40; i += 2) a.push(i, i + 1);
				return a;
			})()`,
			want: `a.length === 40 && a[15] === 15 && a[16] === 16 && a[39] === 39`,
		},
		{
			name: "splice moves elements in both directions",
			src: `(() => {
				const a = numbers();
				const removed = a.splice(38, 1, -1, -2);
				if (removed.length !== 1 || removed[0] !== 38 || a.pop() !== 40) throw Error();
				removed[0] = 0;
				a.splice(38, 2);
				return a;
			})()`,
			want: `a.length === 39 && a[37] === 37 && a[38] === 39`,
		},
		{
			name: "splice returns a packed copy after a mixed insertion",
			src: `(() => {
				const a = numbers();
				const removed = a.splice(1, 2, "mixed");
				if (a.length !== 40 || a[1] !== "mixed" || a[2] !== 3) throw Error();
				return removed;
			})()`,
			want: `a.length === 2 && a[0] === 1 && a[1] === 2`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			v, err := runScript(cx, tc.name, setup+`var a = `+tc.src+`; if (!(`+tc.want+`)) throw Error(); a;`)
			if err != nil {
				t.Fatal(err)
			}
			if n := v.O.Raw().NumCount(); n != storage.ElementCount(v) || n == 0 {
				t.Errorf("got %d packed numbers in an array of %d elements", n, storage.ElementCount(v))
			}
		})
	}
}
