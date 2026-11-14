// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// TestAtomicsNotifyWaitAsync checks that Atomics.notify wakes the waiters of its index in
// order, as many as its count, and returns how many it woke.
func TestAtomicsNotifyWaitAsync(t *testing.T) {
	vm := ekuma.NewVM()
	v, err := vm.RunScript(t.Context(), "notify", `
		var log = [];
		var ia = new Int32Array(new SharedArrayBuffer(16));
		for (var i = 0; i < 3; i++) {
			let n = i;
			Atomics.waitAsync(ia, 1, 0).value.then(r => log.push(n + r));
		}
		Atomics.waitAsync(ia, 2, 0).value.then(r => log.push("other" + r));
		[Atomics.notify(ia, 1, 2), Atomics.notify(ia, 0), Atomics.notify(ia, 1), Atomics.notify(ia, 1)].join();
	`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "2,0,1,0"; got != want {
		t.Errorf("notify results: got %q, want %q", got, want)
	}
	v, err = vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "0ok,1ok,2ok"; got != want {
		t.Errorf("resolutions: got %q, want %q", got, want)
	}
}

// TestAtomicsWaitAsyncTimeout checks that a finite timeout resolves the promise with
// "timed-out" when the jobs run after it expired.
func TestAtomicsWaitAsyncTimeout(t *testing.T) {
	vm := ekuma.NewVM()
	_, err := vm.RunScript(t.Context(), "wait", `
		var log = [];
		var ia = new Int32Array(new SharedArrayBuffer(16));
		Atomics.waitAsync(ia, 0, 0, 1).value.then(r => log.push("short " + r));
		Atomics.waitAsync(ia, 0, 0).value.then(r => log.push("long " + r));
	`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	v, err := vm.RunScript(t.Context(), "expire", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), ""; got != want {
		t.Errorf("log before the jobs ran: got %q, want %q", got, want)
	}
	// The timed-out waiter is no longer in the list.
	v, err = vm.RunScript(t.Context(), "notify", `String(Atomics.notify(ia, 0))`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "1"; got != want {
		t.Errorf("notify: got %q, want %q", got, want)
	}
	v, err = vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "short timed-out,long ok"; got != want {
		t.Errorf("log: got %q, want %q", got, want)
	}
}
