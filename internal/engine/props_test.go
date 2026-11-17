// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// TestStringIndexKey tests reads and stores with string keys that are array indices. Each key is
// used more than once, so that the later uses find the index that the first one found.
func TestStringIndexKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "get",
			src: `
				Array.prototype[1] = "p";
				const a = [0, , 2];
				const hole = "1", own = "2";
				a[hole] === "p" && a[hole] === "p" && a[own] === 2 && a[own] === 2;
			`,
			want: "true",
		},
		{
			name: "set",
			src: `
				let log = "";
				Object.defineProperty(Array.prototype, "1", {set(v) { log += v; }});
				const a = [0, , 2];
				const hole = "1", own = "2";
				a[hole] = "x";
				a[hole] = "y";
				a[own] = "z";
				a[own] = "w";
				log + Object.hasOwn(a, 1) + a[2];
			`,
			want: "xyfalsew",
		},
		{
			name: "string length",
			src: `
				const a = [];
				a[1234] = 0;
				let n;
				for (const k in a) {
					a[k];
					n = k.length;
				}
				String(n);
			`,
			want: "4",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := newVM(t)
			v, err := runScript(vm, "index", tc.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := vm.ToString(v); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrimitiveElements(t *testing.T) {
	vm := engine.NewVM(nil)
	o := vm.NewString("abc").Object()
	if _, ok := o.Element(0); ok {
		t.Error("Element reported a stored element for a primitive")
	}
	if o.SetElement(0, engine.True) {
		t.Error("SetElement reported a stored element for a primitive")
	}
}
