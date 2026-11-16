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
