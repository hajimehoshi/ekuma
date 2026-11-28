// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// TestArrayStorageShrinks tests that removing most elements of an array gives up the storage
// that they used.
func TestArrayStorageShrinks(t *testing.T) {
	const fill = `var a = []; for (var i = 0; i < 10000; i++) a.push(i);`
	tests := []struct {
		name   string
		remove string
	}{
		{
			name:   "length",
			remove: `a.length = 10;`,
		},
		{
			name:   "pop",
			remove: `while (a.length > 10) a.pop();`,
		},
		{
			name:   "shift",
			remove: `while (a.length > 10) a.shift();`,
		},
		{
			name:   "splice",
			remove: `a.splice(5, a.length - 10);`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cx := newTestContext(t)
			v, err := runScript(cx, tt.name, fill+tt.remove+`a`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := storage.ElementCount(v); got != 10 {
				t.Fatalf("got %d elements, want 10", got)
			}
			if got := engine.ElementStorage(v); got > 100 {
				t.Errorf("got storage for %d elements, want at most 100", got)
			}
		})
	}
}

// TestShiftedStorageAfterTruncation tests that the storage that shift leaves unused before the
// elements of an array still counts after a truncation that leaves the end of the storage unused.
func TestShiftedStorageAfterTruncation(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "truncation", `
		var a = new Array(2000000).fill(0);
		for (var i = 0; i < 20000; i++) a.shift();
		a.length = 800000;
		a;
	`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := engine.ElementStorage(v), 820000; got != want {
		t.Errorf("got storage for %d elements, want %d", got, want)
	}
}

// TestStackReleased tests that a call with many arguments does not keep its stack slots
// after it returns to Go, and that the stack keeps as many slots as a new one has.
func TestStackReleased(t *testing.T) {
	const args = 1 << 18
	cx := newTestContext(t)
	if _, err := runScript(cx, "apply", `function f() {} f.apply(null, new Array(262144).fill(0));`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	got := engine.StackSize(cx)
	if got >= args {
		t.Errorf("got %d stack slots, want fewer than %d", got, args)
	}
	// The checks of the interpreter drop the stack between calls into the engine.
	if engine.ValidateExec {
		return
	}
	fresh, _ := engine.NewExecutionContext(nil)
	if want := engine.StackSize(fresh); got < want {
		t.Errorf("got %d stack slots, want at least %d as a new stack has", got, want)
	}
}

// TestJobQueueReleased tests that the job queue gives up its storage once many jobs ran.
func TestJobQueueReleased(t *testing.T) {
	const jobs = 10000
	cx := newTestContext(t)
	if _, err := runScript(cx, "jobs", `for (var i = 0; i < 10000; i++) Promise.resolve().then(() => {});`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := engine.JobStorage(cx); got >= jobs {
		t.Errorf("got storage for %d jobs, want fewer than %d", got, jobs)
	}
}
