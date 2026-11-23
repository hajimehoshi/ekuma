// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
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
			if got := engine.ElementCount(v); got != 10 {
				t.Fatalf("got %d elements, want 10", got)
			}
			if got := engine.ElementStorage(v); got > 100 {
				t.Errorf("got storage for %d elements, want at most 100", got)
			}
		})
	}
}

// TestStackReleased tests that a call with many arguments does not keep its stack slots
// after it returns to Go.
func TestStackReleased(t *testing.T) {
	const args = 1 << 18
	cx := newTestContext(t)
	if _, err := runScript(cx, "apply", `function f() {} f.apply(null, new Array(262144).fill(0));`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := engine.StackSize(cx); got >= args {
		t.Errorf("got %d stack slots, want fewer than %d", got, args)
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
