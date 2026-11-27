// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"runtime"
	"strconv"
	"testing"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// TestConcatMemo tests when a concatenation with the contents of an earlier one allocates.
func TestConcatMemo(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	a, b := cx.NewString("String for key "), cx.NewString("0.123456789")
	parts := []storage.Value{cx.NewString("String for"), cx.NewString(" key 0.123456789")}
	concat := func() { cx.Concat(a, b) }
	concatParts := func() { cx.ConcatStrings(parts) }

	if n := testing.AllocsPerRun(10, concat); n != 0 {
		t.Errorf("a repeated concatenation: %v allocations, want 0", n)
	}
	if n := testing.AllocsPerRun(10, concatParts); n != 0 {
		t.Errorf("a concatenation of parts with the same contents: %v allocations, want 0", n)
	}
	if got, want := cx.ConcatStrings(parts).Str(), wtf8.String("String for key 0.123456789"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Concatenations whose contents seldom repeat make the memo step aside for a while, keeping
	// none of their results alive.
	var unique []weak.Pointer[storage.Object]
	for i := range engine.MemoWindow {
		unique = append(unique, weak.Make(cx.Concat(a, cx.NewString(wtf8.String(strconv.Itoa(i)))).O))
	}
	runtime.GC()
	var kept int
	for _, w := range unique {
		if w.Value() != nil {
			kept++
		}
	}
	if kept != 0 {
		t.Errorf("results of unique concatenations kept alive: %d, want 0", kept)
	}
	if n := testing.AllocsPerRun(10, concat); n == 0 {
		t.Errorf("a repeated concatenation after unique ones: %v allocations, want some", n)
	}
	for range engine.MemoSkip {
		concat()
	}
	if n := testing.AllocsPerRun(10, concat); n != 0 {
		t.Errorf("a repeated concatenation after the memo stepped aside: %v allocations, want 0", n)
	}
}
