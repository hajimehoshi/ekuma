// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"runtime"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestWeakCollectionsSharedKey checks the entries of one key in several weak collections
// through deletions in different orders and additions after deletions.
func TestWeakCollectionsSharedKey(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "two collections, first deleted first",
			src: `
				var a = new WeakMap(), b = new WeakMap(), k = {};
				a.set(k, 1); b.set(k, 2);
				check(a.delete(k) && !a.has(k) && b.get(k) === 2);
				check(b.delete(k) && !b.has(k) && !a.delete(k) && !b.delete(k));
				b.set(k, 3); a.set(k, 4);
				check(a.get(k) === 4 && b.get(k) === 3);
			`,
		},
		{
			name: "two collections, second deleted first",
			src: `
				var a = new WeakMap(), b = new WeakSet(), k = {};
				a.set(k, 1); b.add(k);
				check(b.delete(k) && !b.has(k) && a.get(k) === 1);
				b.add(k);
				check(b.has(k) && a.get(k) === 1);
				check(a.delete(k) && a.get(k) === undefined && b.has(k));
				a.set(k, 5);
				check(a.get(k) === 5 && b.has(k));
			`,
		},
		{
			name: "three collections",
			src: `
				var a = new WeakMap(), b = new WeakMap(), c = new WeakMap(), k = {};
				a.set(k, 1); b.set(k, 2); c.set(k, 3);
				check(b.delete(k) && a.get(k) === 1 && !b.has(k) && c.get(k) === 3);
				check(a.delete(k) && !a.has(k) && !b.has(k) && c.get(k) === 3);
				b.set(k, 4); a.set(k, 5);
				check(a.get(k) === 5 && b.get(k) === 4 && c.get(k) === 3);
				c.set(k, 6); a.set(k, 7);
				check(a.get(k) === 7 && b.get(k) === 4 && c.get(k) === 6);
				check(c.delete(k) && b.delete(k) && a.delete(k));
				check(!a.has(k) && !b.has(k) && !c.has(k));
			`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			src := `function check(ok) { if (!ok) throw new Error("check failed"); }` + tc.src
			if _, err := runScript(t.Context(), &vm, "test", src); err != nil {
				t.Error(err)
			}
		})
	}
}

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
