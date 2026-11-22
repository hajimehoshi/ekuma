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
