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

// TestWeakCollectionsCollected checks that the entry of a key in a collection is found after the
// other collections of the key are collected, and that the value of a collected collection is
// collected once the key joins another collection.
func TestWeakCollectionsCollected(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var k = {}, c = new WeakMap();
		function make() {
			var a = new WeakMap(), b = new WeakMap(), v = {};
			a.set(k, v); b.set(k, {}); c.set(k, 3);
			return [new WeakRef(a), new WeakRef(b), new WeakRef(v)];
		}
		var refs = make();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "reuse", `
		var d = new WeakMap();
		d.set(k, 4);
		[refs[0].deref() === undefined, refs[1].deref() === undefined, c.get(k), d.get(k)].join();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true,3,4"; got != want {
		t.Errorf("after the collections were collected: got %s, want %s", got, want)
	}
	runtime.GC()
	v, err = runScript(t.Context(), &vm, "value", `refs[2].deref() === undefined`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Errorf("the value of a collected collection is alive")
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
