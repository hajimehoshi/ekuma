// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"weak"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

// toString converts v to a string the way the JavaScript function String does.
func toString(t *testing.T, vm *ekuma.VM, v ekuma.Value) string {
	t.Helper()
	s, err := vm.Global().Call(t.Context(), "String", v)
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	return s.String()
}

// TestAllocationsPerOperation covers the number of allocations of common operations.
func TestAllocationsPerOperation(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector changes the number of allocations")
	}
	if validateEnabled {
		t.Skip("the checks of the interpreter change the number of allocations")
	}
	tests := []struct {
		name  string
		setup string
		body  string
		want  float64
	}{
		{
			name:  "arrow function capturing this and a parameter",
			setup: `var o = {x: 1, m(a) { return () => this.x + a; }};`,
			body:  `o.m(1);`,
			want:  1,
		},
		{
			name:  "spread of an array",
			setup: `var src = []; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `[...src];`,
			want:  2,
		},
		{
			name:  "spread of an object into a literal",
			setup: `var src = {a: 1, b: 2, c: 3, d: 4};`,
			body:  `({...src});`,
			want:  2,
		},
		{
			name:  "map",
			setup: `var src = []; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `src.map(x => x);`,
			want:  3,
		},
		{
			name: "push onto an empty array",
			body: `var a = []; for (var j = 0; j < 16; j++) a.push(j);`,
			want: 3,
		},
		{
			name:  "concatenation of two long strings",
			setup: `var a = "a".repeat(20), b = "b".repeat(20);`,
			body:  `a + b;`,
			want:  2,
		},
		{
			name:  "template literal",
			setup: `var a = "0";`,
			body:  "`x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}`;",
			want:  2,
		},
		{
			name:  "stores into an array filled from the end",
			setup: `var a = []; for (var j = 199; j >= 0; j--) a[j] = j;`,
			body:  `for (var j = 0; j < 200; j++) a[j] = j;`,
			want:  0,
		},
		{
			name:  "compound assignments to elements with large indices",
			setup: `var a = new Array(3000).fill(0);`,
			body:  `a[2000] += 1; a[2001]++;`,
			want:  0,
		},
		{
			name:  "construction of instances whose properties differ after the first",
			setup: `function V(k) { this.a = 1; if (k) { this.b = 1; this.c = 1; this.d = 1; this.e = 1; this.f = 1; } else { this.f = 1; this.e = 1; this.d = 1; this.c = 1; this.b = 1; } }`,
			body:  `new V(0); new V(1);`,
			want:  2,
		},
		{
			name:  "construction of an instance of a subclass",
			setup: `class P { constructor(x, y) { this.x = x; this.y = y; } } class Q extends P { constructor(x) { super(x, 2); } }`,
			body:  `new Q(1);`,
			want:  1,
		},
		{
			name:  "for-in over an array",
			setup: `var src = [], s = 0; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `for (var k in src) s += src[k];`,
			want:  1,
		},
		{
			name:  "for-in over an object with index keys",
			setup: `var src = {x: 0}, s = 0; for (var j = 0; j < 16; j++) src[j] = j;`,
			body:  `for (var k in src) s += src[k];`,
			want:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var vm ekuma.VM
			src := tt.setup + "\n(function(n) { for (var i = 0; i < n; i++) { " + tt.body + " } })"
			fn, err := runScript(ctx, &vm, tt.name, src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			const n = 100
			allocs := func(n int) float64 {
				return testing.AllocsPerRun(10, func() {
					if _, err := fn.Invoke(ctx, n); err != nil {
						t.Fatal(err)
					}
				})
			}
			if got := (allocs(n) - allocs(0)) / n; got != tt.want {
				t.Errorf("allocations per operation: got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAppendedStringsToGo covers strings that a script builds by appending to one string:
// each one that reached Go keeps its contents while the script appends further.
func TestAppendedStringsToGo(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	fn, err := runScript(ctx, &vm, "append", `(function() {
		var r = "";
		return function(s) { r += s; return r; };
	})()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	var got []string
	for range 100 {
		v, err := fn.Invoke(ctx, "ab")
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		got = append(got, v.String())
	}
	for i, s := range got {
		if want := strings.Repeat("ab", i+1); s != want {
			t.Errorf("string %d: got %q, want %q", i, s, want)
		}
	}
}

// TestPrependedStringsToGo covers strings that a script builds by prepending to one string:
// each one has its contents when it reaches Go, even after the script prepended further.
func TestPrependedStringsToGo(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	fn, err := runScript(ctx, &vm, "prepend", `(function() {
		var r = "";
		return function(s) { r = s + r; return r; };
	})()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	var got []ekuma.Value
	for range 100 {
		v, err := fn.Invoke(ctx, "ab")
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		got = append(got, v)
	}
	for i, v := range got {
		if s, want := v.String(), strings.Repeat("ab", i+1); s != want {
			t.Errorf("string %d: got %q, want %q", i, s, want)
		}
	}
}

// TestAtomicsNotifyWaitAsync checks that Atomics.notify wakes the waiters of its index in
// order, as many as its count, and returns how many it woke.
func TestAtomicsNotifyWaitAsync(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "notify", `
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
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "2,0,1,0"; got != want {
		t.Errorf("notify results: got %q, want %q", got, want)
	}
	v, err = runScript(t.Context(), &vm, "log", `log.join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "0ok,1ok,2ok"; got != want {
		t.Errorf("resolutions: got %q, want %q", got, want)
	}
}

// TestAtomicsWaitAsyncTimeout checks that a finite timeout resolves the promise with
// "timed-out" when the jobs run after it expired.
func TestAtomicsWaitAsyncTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		_, err := runScript(t.Context(), &vm, "wait", `
			var log = [];
			var ia = new Int32Array(new SharedArrayBuffer(16));
			Atomics.waitAsync(ia, 0, 0, 1).value.then(r => log.push("short " + r));
			Atomics.waitAsync(ia, 0, 0).value.then(r => log.push("long " + r));
		`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		// The fake clock of the bubble reaches the deadline.
		time.Sleep(time.Millisecond)
		v, err := runScript(t.Context(), &vm, "expire", `log.join()`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), ""; got != want {
			t.Errorf("log before the jobs ran: got %q, want %q", got, want)
		}
		// The timed-out waiter is no longer in the list.
		v, err = runScript(t.Context(), &vm, "notify", `String(Atomics.notify(ia, 0))`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "1"; got != want {
			t.Errorf("notify: got %q, want %q", got, want)
		}
		v, err = runScript(t.Context(), &vm, "log", `log.join()`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "short timed-out,long ok"; got != want {
			t.Errorf("log: got %q, want %q", got, want)
		}
	})
}

// TestAtomicsWaitAsyncManyWaiters checks that the waiters of one index are added and woken in
// time linear in their number, in order.
func TestAtomicsWaitAsyncManyWaiters(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithTimeout(t.Context(), linearTimeDeadline())
	defer cancel()
	v, err := runScript(ctx, &vm, "wait", `
		var log = [];
		var ia = new Int32Array(new SharedArrayBuffer(16));
		for (var i = 0; i < 80000; i++) {
			let n = i;
			Atomics.waitAsync(ia, 0, 0).value.then(() => { if (n < 3 || n >= 79998) log.push(n); });
		}
		[Atomics.notify(ia, 0, 2), Atomics.notify(ia, 0, 1), Atomics.notify(ia, 0)].join();
	`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "2,1,79997"; got != want {
		t.Errorf("notify results: got %q, want %q", got, want)
	}
	v, err = runScript(ctx, &vm, "log", `log.join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "0,1,2,79998,79999"; got != want {
		t.Errorf("woken waiters: got %q, want %q", got, want)
	}
}

// TestAtomicsWaitAsyncTimeoutsAmongWaiters checks that waiters that time out leave their list
// and the others are woken in order.
func TestAtomicsWaitAsyncTimeoutsAmongWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		_, err := runScript(t.Context(), &vm, "wait", `
			var log = [];
			var ia = new Int32Array(new SharedArrayBuffer(16));
			for (var i = 0; i < 200; i++) {
				let n = i;
				// Every third waiter times out.
				Atomics.waitAsync(ia, 0, 0, i % 3 == 0 ? 1 : Infinity).value.then(r => { if (r == "ok") log.push(n); });
			}
		`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		// The fake clock of the bubble reaches the deadline, and the next run expires the waiters.
		time.Sleep(time.Millisecond)
		if _, err := runScript(t.Context(), &vm, "expire", ``); err != nil {
			t.Fatalf("runScript: %v", err)
		}
		// The waiters added after the timeouts sweep the list.
		v, err := runScript(t.Context(), &vm, "notify", `
			for (var i = 200; i < 300; i++) {
				let n = i;
				Atomics.waitAsync(ia, 0, 0).value.then(() => log.push(n));
			}
			String(Atomics.notify(ia, 0, 5));
		`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "5"; got != want {
			t.Errorf("first notify: got %q, want %q", got, want)
		}
		// 133 of the first 200 waiters stay, and 100 more come after them.
		v, err = runScript(t.Context(), &vm, "rest", `String(Atomics.notify(ia, 0))`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "228"; got != want {
			t.Errorf("second notify: got %q, want %q", got, want)
		}
		v, err = runScript(t.Context(), &vm, "log", `log.slice(0, 5).join() + " " + log.slice(128, 135).join() + " " + log.length`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "1,2,4,5,7 193,194,196,197,199,200,201 233"; got != want {
			t.Errorf("woken waiters: got %q, want %q", got, want)
		}
	})
}

// linearTimeDeadline returns a deadline that linear handling meets by far and quadratic handling
// misses. It is looser on js, where wasm in a browser runs slower and shares the CPU.
func linearTimeDeadline() time.Duration {
	if runtime.GOOS == "js" {
		return 30 * time.Second
	}
	return 5 * time.Second
}

// skipAgents skips a test that runs agents on several goroutines, which a single thread cannot.
func skipAgents(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "js" {
		t.Skip("agents need threads")
	}
}

// runIn runs src in a new VM on another goroutine, with the global sab sharing the data block of
// the SharedArrayBuffer v, and returns the result as a string.
func runIn(t *testing.T, v ekuma.Value, src string) string {
	t.Helper()
	blk := v.SharedDataBlock()
	var got string
	var err error
	var wg sync.WaitGroup
	wg.Go(func() {
		var vm ekuma.VM
		if err = vm.Global().Set("sab", blk); err != nil {
			return
		}
		var r ekuma.Value
		r, err = runScript(context.Background(), &vm, "agent", src)
		got = r.String()
	})
	wg.Wait()
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	return got
}

func TestAtomicsWaitCannotBlock(t *testing.T) {
	var vm ekuma.VM
	vm.SetCanBlock(true)
	vm.SetCanBlock(false)
	got := run(t, &vm, `try { Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0); "returned"; } catch (e) { e.constructor.name; }`)
	if want := "TypeError"; got != want {
		t.Errorf("Atomics.wait: got %s, want %s", got, want)
	}
}

func TestSharedArrayBufferSliceSameDataBlock(t *testing.T) {
	var vm ekuma.VM
	source, err := runScript(t.Context(), &vm, "source", `
		const source = new SharedArrayBuffer(4);
		const bytes = new Uint8Array(source);
		bytes.set([10, 20, 30, 40]);
		source.constructor = { [Symbol.species]: function () { return alias; } };
		source;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Global().Set("alias", source.SharedDataBlock()); err != nil {
		t.Fatal(err)
	}
	_, err = source.Call(t.Context(), "slice", 1, 3)
	var ex *ekuma.ScriptError
	if !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "TypeError") {
		t.Errorf("slice: got %v, want a TypeError", err)
	}
	if got, want := run(t, &vm, `bytes.join()`), "10,20,30,40"; got != want {
		t.Errorf("source bytes after slice: got %s, want %s", got, want)
	}
}

func TestSharedDataBlockOfNonShared(t *testing.T) {
	var vm ekuma.VM
	ab, err := runScript(t.Context(), &vm, "ab", `new ArrayBuffer(4)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		r := recover()
		var verr *ekuma.ValueError
		if err, ok := r.(error); !ok || !errors.As(err, &verr) {
			t.Errorf("SharedDataBlock of an ArrayBuffer: got panic %v, want a *ValueError", r)
		}
	}()
	ab.SharedDataBlock()
}

func TestAtomicsWaitInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		vm.SetCanBlock(true)
		run(t, &vm, `globalThis.ta = new Int32Array(new SharedArrayBuffer(4));`)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := runScript(ctx, &vm, "wait", `Atomics.wait(ta, 0, 0)`); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("runScript: got %v, want %v", err, context.DeadlineExceeded)
		}
		// The interrupted wait left its waiter list.
		if got, want := run(t, &vm, `Atomics.notify(ta, 0)`), "0"; got != want {
			t.Errorf("Atomics.notify: got %s, want %s", got, want)
		}
	})
}

func TestAtomicsWaitAsyncNotifiedByAnotherVM(t *testing.T) {
	skipAgents(t)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "wait", `
globalThis.result = "pending";
const sab = new SharedArrayBuffer(8);
Atomics.waitAsync(new Int32Array(sab), 1, 0).value.then(v => { result = v; });
sab`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runIn(t, sab, `String(Atomics.notify(new Int32Array(sab), 1))`), "1"; got != want {
		t.Errorf("Atomics.notify: got %s, want %s", got, want)
	}
	ok, err := vm.AwaitJobs(t.Context())
	if err != nil || !ok {
		t.Fatalf("AwaitJobs: got %t, %v, want true, nil", ok, err)
	}
	result, err := vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "ok"; got != want {
		t.Errorf("result: got %s, want %s", got, want)
	}
	// The resolved waiter is gone, so nothing is left to wait for.
	if ok, err := vm.AwaitJobs(t.Context()); ok || err != nil {
		t.Errorf("AwaitJobs without waiters: got %t, %v, want false, nil", ok, err)
	}
}

// TestHostFunctionRecursion checks that recursion through host functions, which runs no
// interpreter loop, throws a RangeError.
func TestHostFunctionRecursion(t *testing.T) {
	var vm ekuma.VM
	invoke := vm.FuncOf("invoke", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return args[0].Invoke(t.Context(), args[0])
	})
	if err := vm.Global().Set("invoke", invoke); err != nil {
		t.Fatal(err)
	}
	_, err := runScript(t.Context(), &vm, "recursion", `invoke(invoke)`)
	var ex *ekuma.ScriptError
	if !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "RangeError") {
		t.Errorf("got %v, want a RangeError", err)
	}
}

// TestAtomicsWaitAsyncDroppedVM checks that a VM with a pending waiter of Atomics.waitAsync is
// collected while the data block that it waits on lives on.
func TestAtomicsWaitAsyncDroppedVM(t *testing.T) {
	var owner ekuma.VM
	sab, err := runScript(t.Context(), &owner, "sab", `new SharedArrayBuffer(4)`)
	if err != nil {
		t.Fatal(err)
	}
	blk := sab.SharedDataBlock()
	dropped := func() weak.Pointer[ekuma.VM] {
		vm := new(ekuma.VM)
		if err := vm.Global().Set("sab", blk); err != nil {
			t.Fatal(err)
		}
		run(t, vm, `Atomics.waitAsync(new Int32Array(sab), 0, 0).value.then(() => {});`)
		return weak.Make(vm)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for dropped.Value() != nil {
		if time.Now().After(deadline) {
			t.Fatal("the dropped VM was not collected")
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	if got, want := runIn(t, sab, `String(Atomics.notify(new Int32Array(sab), 0))`), "0"; got != want {
		t.Errorf("Atomics.notify: got %s, want %s", got, want)
	}
}

func TestAwaitJobsCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		run(t, &vm, `Atomics.waitAsync(new Int32Array(new SharedArrayBuffer(4)), 0, 0);`)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := vm.AwaitJobs(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("AwaitJobs: got %v, want %v", err, context.DeadlineExceeded)
		}
	})
}

// TestAwaitJobsNotifiedBeforeTimeout checks that the timeout of a notified waiter does not end
// the wait for another waiter.
func TestAwaitJobsNotifiedBeforeTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		run(t, &vm, `
			var ia = new Int32Array(new SharedArrayBuffer(8));
			Atomics.waitAsync(ia, 0, 0, 1);
			Atomics.waitAsync(ia, 1, 0);
			Atomics.notify(ia, 0);
		`)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := vm.AwaitJobs(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("AwaitJobs: got %v, want %v", err, context.DeadlineExceeded)
		}
	})
}

func TestAwaitJobsCancellationAfterLastJob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		vm.SetUnhandledRejection(func(ekuma.Value, ekuma.Value) { cancel() })
		run(t, &vm, `Atomics.waitAsync(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 1)
			.value.then(() => { throw 42; });`)
		if ran, err := vm.AwaitJobs(ctx); !ran || err != nil {
			t.Errorf("AwaitJobs = %t, %v, want true, nil", ran, err)
		}
		if ctx.Err() == nil {
			t.Error("the rejection handler did not cancel the context")
		}
	})
}

func TestAwaitJobsWhileRunning(t *testing.T) {
	var vm ekuma.VM
	var awaitErr error
	f := vm.FuncOf("awaitJobs", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		_, awaitErr = vm.AwaitJobs(t.Context())
		return nil, nil
	})
	if err := vm.Global().Set("awaitJobs", f); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `awaitJobs();`)
	if !errors.Is(awaitErr, ekuma.ErrRunning) {
		t.Errorf("AwaitJobs: got %v, want %v", awaitErr, ekuma.ErrRunning)
	}
}

func TestAwaitJobsRunsFinalization(t *testing.T) {
	var vm ekuma.VM
	run(t, &vm, `
globalThis.held = "pending";
globalThis.registry = new FinalizationRegistry(h => { held = h; });
registry.register({}, "cleaned");`)
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
	held, err := vm.Global().Get("held")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := held.String(), "cleaned"; got != want {
		t.Errorf("held: got %s, want %s", got, want)
	}
}

func TestSharedArrayBufferGrownByAnotherVM(t *testing.T) {
	skipAgents(t)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "grow", `
const sab = new SharedArrayBuffer(4, {maxByteLength: 16});
globalThis.ta = new Int32Array(sab);
sab`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runIn(t, sab, `sab.grow(12); new Int32Array(sab)[2] = 7; String(sab.byteLength)`), "12"; got != want {
		t.Errorf("byteLength in the other VM: got %s, want %s", got, want)
	}
	if got, want := run(t, &vm, `[ta.buffer.byteLength, ta.length, ta[2]].join()`), "12,3,7"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAtomicsAcrossVMs(t *testing.T) {
	skipAgents(t)
	const (
		agents = 4
		n      = 2000
	)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "main", `globalThis.ta = new Int32Array(new SharedArrayBuffer(4 * (2 + 4))); ta.buffer`)
	if err != nil {
		t.Fatal(err)
	}
	// Each agent counts with Atomics.add and with a compareExchange loop, and writes its own
	// element without atomics.
	src := `
const ta = new Int32Array(sab);
for (let i = 0; i < ` + strconv.Itoa(n) + `; i++) {
	Atomics.add(ta, 0, 1);
	for (;;) {
		const old = Atomics.load(ta, 1);
		if (Atomics.compareExchange(ta, 1, old, old + 1) === old) {
			break;
		}
	}
	ta[2 + id] = i;
}`
	blk := sab.SharedDataBlock()
	var wg sync.WaitGroup
	errs := make([]error, agents)
	for id := range agents {
		wg.Go(func() {
			var avm ekuma.VM
			if err := avm.Global().Set("sab", blk); err != nil {
				errs[id] = err
				return
			}
			if err := avm.Global().Set("id", id); err != nil {
				errs[id] = err
				return
			}
			_, errs[id] = runScript(context.Background(), &avm, "agent", src)
		})
	}
	wg.Wait()
	for id, err := range errs {
		if err != nil {
			t.Fatalf("agent %d: %v", id, err)
		}
	}
	want := strconv.Itoa(agents*n) + "," + strconv.Itoa(agents*n)
	for range agents {
		want += "," + strconv.Itoa(n-1)
	}
	if got := run(t, &vm, `[...ta].join()`); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// zoneType is a local time type of a time zone.
type zoneType struct {
	// offset is the offset from UTC in seconds.
	offset int32
	abbr   string
}

// daylightSavingZone returns a time zone that is std except from start until end, two instants
// in Unix seconds, when it is dst. The data is built here so that the test does not depend on
// the time zone database of the machine.
func daylightSavingZone(t *testing.T, std, dst zoneType, start, end uint32) *time.Location {
	t.Helper()
	return namedDaylightSavingZone(t, "Test/"+std.abbr, std, dst, start, end)
}

// namedDaylightSavingZone is [daylightSavingZone] with the name name.
func namedDaylightSavingZone(t *testing.T, name string, std, dst zoneType, start, end uint32) *time.Location {
	t.Helper()
	be := binary.BigEndian
	var b []byte
	b = append(b, "TZif"...)
	b = append(b, make([]byte, 16)...)
	// The counts of UT indicators, standard indicators, leap seconds, transitions, types and abbreviation bytes.
	for _, n := range []uint32{0, 0, 0, 2, 2, uint32(len(std.abbr) + len(dst.abbr) + 2)} {
		b = be.AppendUint32(b, n)
	}
	b = be.AppendUint32(b, start)
	b = be.AppendUint32(b, end)
	b = append(b, 1, 0)
	b = be.AppendUint32(b, uint32(std.offset))
	b = append(b, 0, 0)
	b = be.AppendUint32(b, uint32(dst.offset))
	b = append(b, 1, byte(len(std.abbr)+1))
	b = append(b, std.abbr+"\x00"+dst.abbr+"\x00"...)
	loc, err := time.LoadLocationFromTZData(name, b)
	if err != nil {
		t.Fatalf("LoadLocationFromTZData: %v", err)
	}
	return loc
}

// TestDateOffsetTransitions checks local times at the transitions of time zones:
// a skipped local time takes the offset before the transition, a repeated one is the
// earlier instant, and the offset stays defined at the limits of the time values.
func TestDateOffsetTransitions(t *testing.T) {
	var vm ekuma.VM
	// Central Europe in 2021.
	centralEurope := daylightSavingZone(t, zoneType{offset: 1 * 3600, abbr: "CET"}, zoneType{offset: 2 * 3600, abbr: "CEST"},
		1616893200, // 2021-03-28T01:00:00Z
		1635642000, // 2021-10-31T01:00:00Z
	)
	// Lord Howe Island from 2020 to 2021, whose daylight saving time shifts by 30 minutes.
	lordHowe := daylightSavingZone(t, zoneType{offset: 10*3600 + 30*60, abbr: "+1030"}, zoneType{offset: 11 * 3600, abbr: "+11"},
		1601739000, // 2020-10-03T15:30:00Z
		1617462000, // 2021-04-03T15:00:00Z
	)
	// Cuba in 2021, whose transitions are at local midnight.
	cuba := daylightSavingZone(t, zoneType{offset: -5 * 3600, abbr: "CST"}, zoneType{offset: -4 * 3600, abbr: "CDT"},
		1615698000, // 2021-03-14T05:00:00Z
		1636261200, // 2021-11-07T05:00:00Z
	)
	tests := []struct {
		zone *time.Location
		src  string
		want string
	}{
		{
			zone: centralEurope,
			src:  `new Date(2021, 2, 28, 2, 30).toISOString()`,
			want: "2021-03-28T01:30:00.000Z",
		},
		{
			zone: centralEurope,
			src:  `new Date("2021-03-28T02:30").getHours()`,
			want: "3",
		},
		{
			zone: centralEurope,
			src:  `new Date(2021, 9, 31, 2, 30).toISOString()`,
			want: "2021-10-31T00:30:00.000Z",
		},
		{
			zone: centralEurope,
			src:  `new Date(Date.UTC(2021, 9, 31, 1, 30)).toString()`,
			want: "Sun Oct 31 2021 02:30:00 GMT+0100 (CET)",
		},
		{
			zone: centralEurope,
			src:  `var d = new Date(Date.UTC(2021, 9, 31, 1, 30)); Date.parse(d.toString()) === d.getTime()`,
			want: "true",
		},
		{
			zone: centralEurope,
			src:  `[new Date(-8.64e15).getTimezoneOffset(), new Date(8.64e15).getTimezoneOffset()].join()`,
			want: "-60,-60",
		},
		{
			zone: lordHowe,
			src:  `new Date(2020, 9, 4, 2, 15).toISOString()`,
			want: "2020-10-03T15:45:00.000Z",
		},
		{
			zone: lordHowe,
			src:  `new Date(2021, 3, 4, 1, 45).toISOString()`,
			want: "2021-04-03T14:45:00.000Z",
		},
		{
			zone: cuba,
			src:  `new Date(2021, 2, 14).toISOString()`,
			want: "2021-03-14T05:00:00.000Z",
		},
		{
			zone: cuba,
			src:  `new Date(2021, 10, 7).toISOString()`,
			want: "2021-11-07T04:00:00.000Z",
		},
	}
	for _, tc := range tests {
		vm.SetTimeZone(tc.zone)
		v, err := runScript(t.Context(), &vm, "date", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestTemporalTimeZone checks that Temporal and Intl take a system time zone whose name the
// platform does not know from VM.SetTimeZone as the offset time zone of its current offset,
// which Date agrees with, and that the name is not a time zone identifier.
func TestTemporalTimeZone(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(daylightSavingZone(t, zoneType{offset: 1 * 3600, abbr: "CET"}, zoneType{offset: 2 * 3600, abbr: "CEST"},
		1616893200, // 2021-03-28T01:00:00Z
		1635642000, // 2021-10-31T01:00:00Z
	))
	const src = `[
		Temporal.Now.timeZoneId(),
		Temporal.Now.zonedDateTimeISO().offset,
		new Date().getTimezoneOffset(),
		new Intl.DateTimeFormat("en", { timeZoneName: "longOffset" }).formatToParts().find(p => p.type === "timeZoneName").value,
		(() => { try { Temporal.ZonedDateTime.from("2021-06-01T12:00[Test/CET]"); } catch (e) { return e instanceof RangeError; } })(),
	].join()`
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{
			now:  time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
			want: "+02:00,+02:00,-120,GMT+02:00,true",
		},
		{
			now:  time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
			want: "+01:00,+01:00,-60,GMT+01:00,true",
		},
	} {
		vm.SetClock(func() time.Time { return tc.now })
		v, err := runScript(t.Context(), &vm, "temporal", src)
		if err != nil {
			t.Errorf("%v: %v", tc.now, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.now, got, tc.want)
		}
	}
}

func TestTemporalBoaVistaTransitions(t *testing.T) {
	var vm ekuma.VM
	// Boa Vista observed daylight saving time for just one week in October 2000.
	run(t, &vm, `function transition(instant, direction) {
		const z = Temporal.Instant.from(instant).toZonedDateTimeISO("America/Boa_Vista");
		const change = z.getTimeZoneTransition(direction);
		return change && change.toInstant();
	}`)
	if got, want := run(t, &vm, `transition("2000-10-01T00:00Z", "next")`), "2000-10-08T04:00:00Z"; got != want {
		t.Errorf("next transition: got %s, want %s", got, want)
	}
	if got, want := run(t, &vm, `transition("2000-10-20T00:00Z", "previous")`), "2000-10-15T03:00:00Z"; got != want {
		t.Errorf("previous transition: got %s, want %s", got, want)
	}
	if got, want := run(t, &vm, `Temporal.ZonedDateTime.from("2000-10-08T12:00[America/Boa_Vista]").toInstant()`), "2000-10-08T15:00:00Z"; got != want {
		t.Errorf("local noon: got %s, want %s", got, want)
	}
}

func TestTemporalTransitionAtLeapYearEnd(t *testing.T) {
	var vm ekuma.VM
	const src = `Temporal.Instant.from("2000-12-31T12:00Z").toZonedDateTimeISO("Europe/Berlin")
		.getTimeZoneTransition("next")?.toInstant()`
	if got, want := run(t, &vm, src), "2001-03-25T01:00:00Z"; got != want {
		t.Errorf("next transition: got %s, want %s", got, want)
	}
}

// memoryModules loads the modules of sources by name, ignoring the referrer, and keeps one
// record per name. loads counts the calls of the loader for each name.
type memoryModules struct {
	vm      *ekuma.VM
	sources map[string]string
	modules map[string]*ekuma.Module
	loads   map[string]int
}

func newMemoryModules(vm *ekuma.VM, sources map[string]string) *memoryModules {
	l := &memoryModules{
		vm:      vm,
		sources: sources,
		modules: map[string]*ekuma.Module{},
		loads:   map[string]int{},
	}
	vm.SetModuleLoader(l.load)
	return l
}

func (l *memoryModules) load(req ekuma.ModuleRequest) (*ekuma.Module, error) {
	specifier := req.Specifier
	l.loads[specifier]++
	if m, ok := l.modules[specifier]; ok {
		return m, nil
	}
	src, ok := l.sources[specifier]
	if !ok {
		return nil, fmt.Errorf("no module %q", specifier)
	}
	m, err := l.vm.ParseModule(specifier, src)
	if err != nil {
		return nil, err
	}
	l.modules[specifier] = m
	return m, nil
}

// run runs the module name and returns the value of the global variable result
// after the evaluation completed.
func (l *memoryModules) run(t *testing.T, name string) ekuma.Value {
	t.Helper()
	m, err := l.load(ekuma.ModuleRequest{Specifier: name})
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Fatalf("the evaluation is %v: %v", st, r)
	}
	v, err := l.vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestModules(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{
			// An import is a live binding: it sees the later assignments of the exporter.
			name: "live binding",
			sources: map[string]string{
				"main": `import { count, increment } from "counter";
					const seen = [count];
					increment();
					seen.push(count);
					globalThis.result = seen.join();`,
				"counter": `export let count = 0;
					export function increment() { count++; }`,
			},
			want: "0,1",
		},
		{
			name: "assignment before an exported const is initialized",
			sources: map[string]string{
				"main": `import { value as alias } from "main";
					const errors = [];
					for (const assign of [() => alias = 1, () => eval("alias = 1"), () => value = 1]) {
						try { assign(); } catch (e) { errors.push(e.constructor.name); }
					}
					export const value = 2;
					globalThis.result = errors.join();`,
			},
			want: "TypeError,TypeError,ReferenceError",
		},
		{
			// a finished its asynchronous evaluation as a dependency of c before import()
			// asks to evaluate it.
			name: "import of an asynchronous module evaluated as a dependency",
			sources: map[string]string{
				"main": `import "c";
					globalThis.result = String((await import("a")).x);`,
				"c": `import "a";`,
				"a": `export let x = 1; await null; x = 2;`,
			},
			want: "2",
		},
		{
			// b runs first and calls a function of a before a's body ran: the function was
			// instantiated when a was linked.
			name: "cycle with a hoisted function",
			sources: map[string]string{
				"a": `import { fromB } from "b";
					export function greet(s) { return "hello " + s; }
					globalThis.result = fromB;`,
				"b": `import { greet } from "a";
					export const fromB = greet("b");`,
			},
			want: "hello b",
		},
		{
			name: "cycle with an exported deferred namespace",
			sources: map[string]string{
				"main": `import { ns, read } from "a";
					const fromB = read();
					globalThis.result = [ns === fromB, globalThis.runs ?? 0, fromB.x, ns.x, globalThis.runs].join();`,
				"a": `import { read } from "b";
					import defer * as ns from "dep";
					export { ns, read };`,
				"b": `import { ns } from "a";
					export function read() { return ns; }`,
				"dep": `globalThis.runs = (globalThis.runs ?? 0) + 1;
					export const x = 42;`,
			},
			want: "true,0,42,42,1",
		},
		{
			// A module that two modules import, and that a dynamic import loads again,
			// runs once, and all of them see the same namespace object.
			name: "identity",
			sources: map[string]string{
				"main": `import * as ns from "shared";
					import "other";
					globalThis.result = import("shared").then(ns2 =>
						[globalThis.runs, ns === ns2, ns === globalThis.fromOther].join());`,
				"other": `import * as ns from "shared";
					globalThis.fromOther = ns;`,
				"shared": `globalThis.runs = (globalThis.runs ?? 0) + 1;`,
			},
			want: "1,true,true",
		},
		{
			// The decorators of an exported class may precede or follow export.
			name: "decorated exports",
			sources: map[string]string{
				"main": `import D, { A, B } from "dep";
					globalThis.result = [A.tag, B.tag, D.tag].join();`,
				"dep": `function tag(v, c) { v.tag = c.name; }
					@tag export class A {}
					export @tag class B {}
					export default @tag class {}`,
			},
			want: "A,B,default",
		},
		{
			// The modules of a dynamic import evaluate after the importing module.
			name: "dynamic import",
			sources: map[string]string{
				"main": `const log = globalThis.log = [];
					const p = import("dep").then(ns => log.push(ns.default));
					log.push("main");
					globalThis.result = p.then(() => log.join());`,
				"dep": `export default "dep";`,
			},
			want: "main,dep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			l := newMemoryModules(&vm, tt.sources)
			names := []string{"main", "a"}
			name := names[0]
			if _, ok := tt.sources[name]; !ok {
				name = names[1]
			}
			v := l.run(t, name)
			if v.Type() == ekuma.TypeObject {
				// A promise of the result.
				var got ekuma.Value
				f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
					got = args[0]
					return nil, nil
				})
				if _, err := v.Call(t.Context(), "then", f); err != nil {
					t.Fatal(err)
				}
				v = got
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestModuleLoadedOnce checks that the loader is asked once for a request of a referrer
// that loaded.
func TestModuleLoadedOnce(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = Promise.all([import("dep"), import("dep")]).then(([a, b]) => a === b);`,
		"dep":  `export {};`,
	})
	v := l.run(t, "main")
	var same ekuma.Value
	f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		same = args[0]
		return nil, nil
	})
	if _, err := v.Call(t.Context(), "then", f); err != nil {
		t.Fatal(err)
	}
	if !same.Truthy() {
		t.Errorf("the two imports gave different namespace objects")
	}
	if got := l.loads["dep"]; got != 1 {
		t.Errorf("the loader was called %d times for dep, want 1", got)
	}
}

func TestDynamicImportAttributeGetterDeletesNextProperty(t *testing.T) {
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if got := req.Attributes; len(got) != 1 || got["type"] != "json" {
			t.Errorf("loader attributes = %v, want only type=json", got)
		}
		return vm.ParseJSONModule(req.Specifier, `"loaded"`)
	})
	p, err := runScript(t.Context(), &vm, "main", `
		import("data.json", { with: {
			get type() { delete this.extra; return "json"; },
			extra: "unused",
		} }).then(ns => ns.default)`)
	if err != nil {
		t.Fatal(err)
	}
	if st, v := p.PromiseState(); st != ekuma.PromiseFulfilled || v.String() != "loaded" {
		t.Errorf("import result = %v with %v, want fulfilled with loaded", st, v)
	}
}

// TestDynamicImportLoadError checks that an error of the loader rejects the promise of a
// dynamic import with the thrown value, and that a later import asks the loader again.
func TestDynamicImportLoadError(t *testing.T) {
	var vm ekuma.VM
	thrown, err := runScript(t.Context(), &vm, "thrown", `({})`)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		return nil, &ekuma.ScriptError{Value: thrown}
	})
	if err := vm.Global().Set("thrown", thrown); err != nil {
		t.Fatal(err)
	}
	if _, err := runScript(t.Context(), &vm, "main", `
		var result = [];
		import("missing").catch(e => result.push(e === thrown));
		import("missing").catch(e => result.push(e === thrown));`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result.join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
}

// TestModuleReferrer checks that the loader gets the name of the script or module that
// contains the import, including from eval code and from a function made by Function.
func TestModuleReferrer(t *testing.T) {
	var vm ekuma.VM
	var referrers []string
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		referrers = append(referrers, req.ReferrerName+":"+req.Specifier)
		if req.Specifier == "mod" {
			return vm.ParseModule("mod", `import("fromModule"); eval('import("fromEval")');`)
		}
		return nil, errors.New("no module")
	})
	if _, err := runScript(t.Context(), &vm, "script", `
		import("mod");
		(0, eval)('import("fromIndirectEval")');
		Function('import("fromFunction")')();`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"script:mod",
		"script:fromIndirectEval",
		"script:fromFunction",
		"mod:fromModule",
		"mod:fromEval",
	}
	if fmt.Sprint(referrers) != fmt.Sprint(want) {
		t.Errorf("got %q, want %q", referrers, want)
	}
}

// TestShadowRealmImportValue checks that each ShadowRealm loads a module for itself, once,
// in its own realm.
func TestShadowRealmImportValue(t *testing.T) {
	var vm ekuma.VM
	realms := map[*ekuma.Realm]bool{}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		if req.Realm == vm.InitialRealm() {
			t.Errorf("the request of a ShadowRealm has the initial realm")
		}
		realms[req.Realm] = true
		return req.Realm.ParseModule(req.Specifier, `globalThis.count = (globalThis.count ?? 0) + 1; export const count = globalThis.count;`)
	})
	if _, err := runScript(t.Context(), &vm, "main", `
		var result;
		const a = new ShadowRealm(), b = new ShadowRealm();
		Promise.all([a.importValue("mod", "count"), a.importValue("mod", "count"), b.importValue("mod", "count")]).then(v => result = v.join());`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1,1,1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
	if len(realms) != 2 {
		t.Errorf("the requests had %d realms, want 2", len(realms))
	}
}

// TestShadowRealmCancel checks that an interruption in a ShadowRealm is not caught as an
// error, and leaves the realm of the script current.
func TestShadowRealmCancel(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "evaluate",
			src:  `r.evaluate("for (;;) {}")`,
		},
		{
			name: "wrapped function",
			src:  `r.evaluate("() => { for (;;) {} }")()`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			// The deadline may pass before the loop starts on a slow machine, so the variables
			// are declared without it.
			if _, err := runScript(t.Context(), &vm, "setup", `var caught = false; var r = new ShadowRealm();`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			_, err := runScript(ctx, &vm, "loop", `try { `+tt.src+` } catch (e) { caught = true; }`)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			v, err := runScript(t.Context(), &vm, "after", `typeof caught + " " + caught`)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := v.String(), "boolean false"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestImportMeta checks that import.meta has the properties that the host gives it.
func TestImportMeta(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = import.meta.url;`,
	})
	vm.SetImportMeta(func(m *ekuma.Module) map[string]any {
		return map[string]any{
			"url": "memory:main",
		}
	})
	if got, want := l.run(t, "main").String(), "memory:main"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImportMetaReentry(t *testing.T) {
	for _, panicOnce := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%t", panicOnce), func(t *testing.T) {
			var vm ekuma.VM
			l := newMemoryModules(&vm, map[string]string{
				"main": `export function meta() { return import.meta; } globalThis.result = meta;`,
			})
			meta := l.run(t, "main")
			marker := new(int)
			var inner ekuma.Value
			var calls int
			vm.SetImportMeta(func(*ekuma.Module) map[string]any {
				calls++
				if calls == 1 {
					var err error
					inner, err = meta.Invoke(t.Context())
					if err != nil {
						t.Errorf("reentrant meta(): %v", err)
					}
					if panicOnce {
						panic(marker)
					}
				}
				return map[string]any{"self": inner}
			})
			wantCalls := 1
			if panicOnce {
				if got := recoverHostPanic(func() { _, _ = meta.Invoke(t.Context()) }); got != marker {
					t.Errorf("panic = %v, want original marker", got)
				}
				wantCalls++
			}
			if got := run(t, &vm, `const meta = result; meta() === meta().self`); got != "true" {
				t.Error("import.meta.self did not refer to the outer call's object")
			}
			if calls != wantCalls {
				t.Errorf("import.meta hook ran %d times, want %d", calls, wantCalls)
			}
		})
	}
}

// TestLongImportChain checks that loading, linking and evaluating a long chain of imports
// does not exhaust the Go stack.
func TestLongImportChain(t *testing.T) {
	const n = 20000
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		var i int
		if _, err := fmt.Sscanf(req.Specifier, "m%d", &i); err != nil {
			return nil, err
		}
		src := fmt.Sprintf(`export * from "m%d"; export const v%d = %d;`, i+1, i, i)
		if i == n {
			src = fmt.Sprintf(`export const v%d = %d;`, i, i)
		}
		return vm.ParseModule(req.Specifier, src)
	})
	m, err := vm.ParseModule("main", fmt.Sprintf(`import { v%d } from "m0"; globalThis.result = v%d;`, n, n))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Int(); got != n {
		t.Errorf("got %d, want %d", got, n)
	}
}

// TestTailCallStack checks the stack trace of an error thrown after tail calls, which replaced
// the frames of their callers.
func TestTailCallStack(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "trace.js", `"use strict";
function a(n) { return b(n + 1); }
function b(n) { return c.call(null, n + 1); }
function c(n) { throw new Error("x" + n); }
function outer() { try { a(1); } catch (e) { return e.stack; } }
outer()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	want := "Error: x3\n    at c (trace.js:4:23)\n    at outer (trace.js:5:26)\n    at trace.js:6:1"
	if got := v.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// runScript parses src as a script of vm and evaluates it.
func runScript(ctx context.Context, vm *ekuma.VM, name, src string) (ekuma.Value, error) {
	return runScriptInRealm(ctx, vm.InitialRealm(), name, src)
}

func runScriptInRealm(ctx context.Context, realm *ekuma.Realm, name, src string) (ekuma.Value, error) {
	s, err := realm.ParseScript(name, src)
	if err != nil {
		return ekuma.Undefined, err
	}
	return s.Evaluate(ctx)
}

func TestStringConcatenationChain(t *testing.T) {
	// A chain of additions led by a string literal converts each operand right after evaluating
	// it, with the default hint, and stops at the first conversion that throws.
	const src = `var log = [];
		function f(name, v) { log.push(name); return v; }
		var o = {valueOf() { log.push("valueOf"); return 1; }, toString() { return "T"; }};
		var p = {[Symbol.toPrimitive](hint) { log.push(hint); return 2; }};
		var d = new Date(0);
		d.toString = () => "D";
		function mixed(x) { return x + (x = 2) + ("[" + x + "]"); }
		var r = ["a" + f("o", o) + "-" + f("p", p) + d + 10n + "" + 1.5];
		try { "a" + f("x", 1) + f("s", Symbol()) + f("y", 2); } catch (e) { r.push(e instanceof TypeError); }
		r.push(log.join(), "" + "" + "", "x" + "" + "", mixed(1));
		r.join("|")`
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "chain.js", src)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "a1-2D101.5|true|o,valueOf,p,default,x,s||x|3[2]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStringLengthLimits(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "built-in results longer than the string length limit",
			src: `var s = "x".repeat(` + strconv.Itoa(ekuma.MaxStringLength) + `);
				var r = [];
				for (var f of [
					() => s.replace(/x/, "xx$'"),
					() => s.replace("x", () => "xx"),
					() => String.raw({raw: {length: 2 ** 53 - 1, 0: "x", 1: s}}),
					() => "x".concat(s, {toString() { throw 0; }}),
					() => Error.prototype.toString.call({name: s, message: "x"}),
					() => RegExp.prototype.toString.call({source: s, flags: ""}),
					() => Symbol(s).toString(),
					() => Object.prototype.toString.call({get [Symbol.toStringTag]() { return s; }}),
					() => Function("x", s, ""),
					() => ({[Symbol(s)]: function () {}}),
				]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				}
				// The matches after the one whose replacement is too long are found first.
				var re = /x/g;
				try { "axbx".replace(re, s); } catch (e) { r.push(e instanceof RangeError, re.lastIndex, RegExp.leftContext); }
				r.join()`,
			want: "true,true,true,true,true,true,true,true,true,true,true,0,axb",
		},
		{
			name: "BigInt strings longer than the string length limit",
			src:  `try { (1n << (2n ** 30n - 1n)).toString(2); "no error"; } catch (e) { e instanceof RangeError; }`,
			want: "true",
		},
		{
			name: "hexadecimal strings longer than the string length limit",
			src:  `try { new Uint8Array(` + strconv.Itoa((ekuma.MaxStringLength+1)/2) + `).toHex(); "no error"; } catch (e) { e instanceof RangeError; }`,
			want: "true",
		},
		{
			name: "concatenations longer than the string length limit",
			src: `var s = "x".repeat(` + strconv.Itoa((ekuma.MaxStringLength+1)/2) + `);
				var r = [];
				for (var f of [() => s + s, () => ` + "`${s}${s}`" + `, () => "" + s + s]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				}
				r.join()`,
			want: "true,true,true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScriptCompletionValues(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a statement without a value makes the script value undefined",
			src:  `1; if (false) {}`,
			want: "undefined",
		},
		{
			name: "an empty catch block makes the try statement value undefined",
			src:  `1; try { 2; throw 0; } catch (e) {}`,
			want: "undefined",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUnboundScript checks that VMs binding the same unbound script, each on its own goroutine,
// keep their own globals.
func TestUnboundScript(t *testing.T) {
	u, err := ekuma.ParseUnboundScript("count.js", `globalThis.count = (globalThis.count ?? 0) + 1;
globalThis.count;`)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			var vm ekuma.VM
			s := vm.BindScript(u)
			for want := 1; want <= 2; want++ {
				v, err := s.Evaluate(t.Context())
				if err != nil {
					t.Error(err)
					return
				}
				if got := v.Int(); got != want {
					t.Errorf("evaluation %d: got %d, want %d", want, got, want)
				}
			}
		})
	}
	wg.Wait()
}

// TestUnboundScriptNames checks that the name of an unbound script is in its syntax errors and in
// the stack traces of its bound scripts.
func TestUnboundScriptNames(t *testing.T) {
	_, err := ekuma.ParseUnboundScript("bad.js", "\n\nlet x = ;")
	se, ok := errors.AsType[*ekuma.SyntaxError](err)
	if !ok {
		t.Fatalf("got %v, want a *SyntaxError", err)
	}
	if se.File != "bad.js" || se.Line != 3 {
		t.Errorf("got %s:%d, want bad.js:3", se.File, se.Line)
	}

	u, err := ekuma.ParseUnboundScript("app.js", `function f() {
  throw new Error("boom");
}
try { f(); } catch (e) { e.stack; }`)
	if err != nil {
		t.Fatal(err)
	}
	var vm ekuma.VM
	v, err := vm.BindScript(u).Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := "Error: boom\n    at f (app.js:2:9)\n    at app.js:4:7"
	if got := v.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestUnboundScriptStackTraces checks that VMs binding the same unbound script, each on its own
// goroutine, get the positions of its code in their stack traces.
func TestUnboundScriptStackTraces(t *testing.T) {
	// The throw is more than 512 bytes into a line with non-ASCII characters.
	src := `const s = "` + strings.Repeat("é", 300) + `"; function f() { throw new Error("boom"); }
try { f(); } catch (e) { e.stack; }`
	u, err := ekuma.ParseUnboundScript("app.js", src)
	if err != nil {
		t.Fatal(err)
	}
	want := "Error: boom\n    at f (app.js:1:336)\n    at app.js:2:7"
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			var vm ekuma.VM
			v, err := vm.BindScript(u).Evaluate(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			if got := v.String(); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
	wg.Wait()
}

// TestBindScriptRealm checks that a script bound to a realm runs in that realm.
func TestBindScriptRealm(t *testing.T) {
	u, err := ekuma.ParseUnboundScript("global.js", `globalThis`)
	if err != nil {
		t.Fatal(err)
	}
	var vm ekuma.VM
	r := vm.NewRealm()
	v, err := r.BindScript(u).Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(r.Global()) {
		t.Error("the script did not run in the realm that it is bound to")
	}
}
