// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

func TestWeakCollectionsCollected(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var k = {}, c = new WeakMap();
		function make() {
			var a = new WeakMap(), b = new WeakMap();
			a.set(k, {}); b.set(k, {}); c.set(k, {n: 3});
			return [a, b, a.get(k), b.get(k)].map(o => new WeakRef(o));
		}
		var refs = make();
	`); err != nil {
		t.Fatal(err)
	}
	// The map cleanups release the values for a subsequent GC. The key stays idle.
	const want = "false,false,false,false"
	var live string
	for range 20 {
		runtime.GC()
		v, err := runScript(t.Context(), &vm, "refs", `refs.map(r => r.deref() !== undefined).join()`)
		if err != nil {
			t.Fatal(err)
		}
		live = v.String()
		if live == want {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if live != want {
		t.Errorf("reachable (map a, map b, value a, value b): got %s, want %s", live, want)
	}
	v, err := runScript(t.Context(), &vm, "reuse", `
		var d = new WeakMap();
		d.set(k, 4);
		[c.get(k).n, d.get(k)].join();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "3,4"; got != want {
		t.Errorf("after the collections were collected: got %s, want %s", got, want)
	}
}

func TestWeakMapValueReferencesKey(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var m = new WeakMap();
		var ref = (() => {
			let k = {};
			m.set(k, {key: k});
			return new WeakRef(k);
		})();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "key", `ref.deref() === undefined`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Error("the value kept its WeakMap key alive")
	}
}

// TestFinalizationRegistrySharedToken checks the cleanup of many cells that share an unregister
// token, and that unregistering the token then removes the cells that are left.
func TestFinalizationRegistrySharedToken(t *testing.T) {
	const n = 600000
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var token = {}, kept = [], count = 0, sum = 0;
		var r = new FinalizationRegistry(h => { count++; sum += h; });
		for (let i = 0; i < `+strconv.Itoa(n)+`; i++) {
			let o = {};
			if (i % 3 === 0) kept.push(o);
			r.register(o, i, token);
		}
	`); err != nil {
		t.Fatal(err)
	}
	// The cleanup of all the cells finishes well within the timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for {
		runtime.GC()
		if _, err := vm.AwaitJobs(ctx); err != nil {
			t.Fatalf("AwaitJobs: %v", err)
		}
		v, err := runScript(ctx, &vm, "count", `count`)
		if err != nil {
			t.Fatal(err)
		}
		if v.Float() == 2*n/3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("the cleanup ended after the deadline: %v", err)
	}
	// The sum does not fit in 32 bits.
	var want int64
	for i := range int64(n) {
		if i%3 != 0 {
			want += i
		}
	}
	v, err := runScript(ctx, &vm, "unregister", `[sum, r.unregister(token), r.unregister(token)].join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), strconv.FormatInt(want, 10)+",true,false"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFinalizationRegistryMicrotasksAndThrow(t *testing.T) {
	t.Run("drains promise jobs and reports unhandled rejections", func(t *testing.T) {
		var vm ekuma.VM
		var rejected string
		vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
			rejected = reason.String()
		})
		if _, err := runScript(t.Context(), &vm, "setup", `
			var cleanupRan = false, microtaskRan = false;
			var reg = new FinalizationRegistry(() => {
				cleanupRan = true;
				Promise.resolve().then(() => { microtaskRan = true; });
				Promise.reject("cleanup-rej");
			});
			reg.register({}, 1);
		`); err != nil {
			t.Fatal(err)
		}
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
		v, err := runScript(t.Context(), &vm, "check", `[cleanupRan, microtaskRan].join()`)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := v.String(), "true,true"; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
		if rejected != "cleanup-rej" {
			t.Errorf("unhandled rejection: got %q, want %q", rejected, "cleanup-rej")
		}
	})

	t.Run("throwing cleanup does not abort remaining cleanups or fail caller", func(t *testing.T) {
		var vm ekuma.VM
		if _, err := runScript(t.Context(), &vm, "setup", `
			var secondRan = false;
			var r1 = new FinalizationRegistry(() => { throw new Error("cleanup boom"); });
			var r2 = new FinalizationRegistry(() => { secondRan = true; });
			r1.register({}, 1);
			r2.register({}, 2);
		`); err != nil {
			t.Fatal(err)
		}
		s, err := vm.ParseScript("eval", `1 + 1`)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			runtime.GC()
			v, err := s.Evaluate(t.Context())
			if err != nil {
				t.Fatalf("Evaluate failed due to cleanup throw: %v", err)
			}
			if v.Int() != 2 {
				t.Fatalf("got %d, want 2", v.Int())
			}
			done, err := vm.Global().Get("secondRan")
			if err != nil {
				t.Fatal(err)
			}
			if done.Bool() {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("second cleanup did not run")
			}
			time.Sleep(time.Millisecond)
		}
	})
}
