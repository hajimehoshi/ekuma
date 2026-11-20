// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// TestArrayWithLengthFilledFromEnd tests that filling an array created with its length from the
// end keeps every element in the storage of the elements, which needs no key for an index.
func TestArrayWithLengthFilledFromEnd(t *testing.T) {
	vm := newVM(t)
	before := engine.InternedCount(vm)
	if _, err := runScript(vm, "fill", `o = new Array(500); for (i = 499; i >= 0; i--) o[i] = i;`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := engine.InternedCount(vm); got != before {
		t.Errorf("got %d interned strings, want %d", got, before)
	}
}

func TestPushCapacityAfterLongRuns(t *testing.T) {
	vm := newVM(t)
	v, err := runScript(vm, "build", `
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
	v, err = runScript(vm, "short", "build(8)")
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.ElementStorage(v); got > 8 {
		t.Errorf("capacity after a short run: got %d, want at most 8", got)
	}
}
