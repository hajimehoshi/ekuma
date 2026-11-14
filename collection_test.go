// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestWeakCollectionsShareKeys checks that the weak collections that have the same key keep
// their entries apart, for an object and for a symbol key.
func TestWeakCollectionsShareKeys(t *testing.T) {
	const src = `var r = [];
		[{}, Symbol("s")].forEach(function(k) {
			var m1 = new WeakMap(), m2 = new WeakMap(), s = new WeakSet();
			m1.set(k, 1);
			m2.set(k, 2);
			s.add(k);
			m1.delete(k);
			r.push(m1.has(k), m2.get(k), s.has(k));
			s.delete(k);
			r.push(m2.get(k), s.has(k));
		});
		r.join()`
	vm := ekuma.NewVM()
	v, err := vm.RunScript(t.Context(), "weak", src)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := toString(t, vm, v), "false,2,true,2,false,false,2,true,2,false"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
