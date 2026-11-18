// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// setUpContinuationData defines run(data, f), which calls f with data as the continuation
// data, and current(), which returns the continuation data.
func setUpContinuationData(t *testing.T, vm *ekuma.VM) {
	t.Helper()
	funcs := map[string]ekuma.Func{
		"run": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			var result ekuma.Value
			var err error
			vm.WithContinuationData(args[0].String(), func() {
				result, err = args[1].Invoke(context.Background())
			})
			return result, err
		},
		"current": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return vm.ContinuationData(), nil
		},
	}
	for name, f := range funcs {
		if err := vm.Global().Set(name, vm.FuncOf(name, f)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContinuationData(t *testing.T) {
	tests := []struct {
		name string
		srcs []string
		want string
	}{
		{
			name: "await in overlapping scopes",
			srcs: []string{`
var seen = [];
run("A", async () => { await 0; seen.push(current()); });
run("B", async () => { await 0; seen.push(current()); });`},
			want: "A,B",
		},
		{
			name: "handlers registered in different scopes and resolved in another",
			srcs: []string{`
var seen = [];
var resolve;
var p = new Promise(r => { resolve = r; });
run("A", () => p.then(() => seen.push(current())));
run("B", () => p.then(() => seen.push(current())));
run("C", () => resolve());`},
			want: "A,B",
		},
		{
			name: "awaited thenable rejected in another scope",
			srcs: []string{`
var seen = [];
var reject;
var thenable = {
	then(_, r) {
		seen.push("then " + current());
		reject = r;
	},
};
run("A", async () => {
	try {
		await thenable;
	} catch {
		seen.push("catch " + current());
	}
});`, `
run("B", () => reject());`},
			want: "then A,catch A",
		},
		{
			name: "nested scope",
			srcs: []string{`
var seen = [];
run("A", () => {
	run("B", async () => { await 0; seen.push(current()); });
	seen.push(current());
});`},
			want: "A,B",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			setUpContinuationData(t, &vm)
			for _, src := range tt.srcs {
				run(t, &vm, src)
			}
			if got := run(t, &vm, `seen.join()`); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// pendingReaction makes a pending promise with the reaction handler handler, registered with
// the continuation data "A", and returns the function that resolves the promise.
func pendingReaction(t *testing.T, vm *ekuma.VM, handler string) ekuma.Value {
	t.Helper()
	setUpContinuationData(t, vm)
	run(t, vm, `
var resolve;
var p = new Promise(r => { resolve = r; });
run("A", () => p.then(`+handler+`));`)
	resolve, err := vm.Global().Get("resolve")
	if err != nil {
		t.Fatal(err)
	}
	return resolve
}

// TestContinuationDataJobPanic checks that a job whose host function panics leaves the
// continuation data of the call that ran the job.
func TestContinuationDataJobPanic(t *testing.T) {
	var vm ekuma.VM
	marker := new(int)
	if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
		panic(marker)
	})); err != nil {
		t.Fatal(err)
	}
	resolve := pendingReaction(t, &vm, `() => bad()`)
	vm.WithContinuationData("B", func() {
		if got := recoverHostPanic(func() {
			_, _ = resolve.Invoke(t.Context())
		}); got != marker {
			t.Errorf("recovered %v, want the panic of bad", got)
		}
		if got, want := vm.ContinuationData(), "B"; got != want {
			t.Errorf("ContinuationData: got %v, want %v", got, want)
		}
	})
}

// TestContinuationDataJobCanceled checks that a job stopped by a context leaves the
// continuation data of the call that ran the job.
func TestContinuationDataJobCanceled(t *testing.T) {
	var vm ekuma.VM
	resolve := pendingReaction(t, &vm, `() => { for (;;) {} }`)
	vm.WithContinuationData("B", func() {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, err := resolve.Invoke(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Invoke: got %v, want context.DeadlineExceeded", err)
		}
		if got, want := vm.ContinuationData(), "B"; got != want {
			t.Errorf("ContinuationData: got %v, want %v", got, want)
		}
	})
}

func TestWithContinuationDataPanic(t *testing.T) {
	var vm ekuma.VM
	marker := new(int)
	vm.WithContinuationData("outer", func() {
		if got := recoverHostPanic(func() {
			vm.WithContinuationData("inner", func() {
				panic(marker)
			})
		}); got != marker {
			t.Errorf("recovered %v, want %v", got, marker)
		}
		if got, want := vm.ContinuationData(), "outer"; got != want {
			t.Errorf("ContinuationData: got %v, want %v", got, want)
		}
	})
}

func TestContinuationDataFinalizationRegistry(t *testing.T) {
	var vm ekuma.VM
	setUpContinuationData(t, &vm)
	run(t, &vm, `
globalThis.seen = "pending";
run("A", () => {
	globalThis.registry = new FinalizationRegistry(() => { seen = current(); });
});
registry.register({}, 0);`)
	// The Go runtime queues the cleanups on its own goroutine some time after a collection.
	deadline := time.Now().Add(10 * time.Second)
	for {
		runtime.GC()
		ok, err := vm.AwaitJobs(t.Context())
		if err != nil {
			t.Fatalf("AwaitJobs: %v", err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("AwaitJobs: no cleanup became pending")
		}
		time.Sleep(time.Millisecond)
	}
	if got, want := run(t, &vm, `seen`), "A"; got != want {
		t.Errorf("seen: got %q, want %q", got, want)
	}
}
