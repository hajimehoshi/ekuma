// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// TestStringIndexKey tests reads and stores with string keys that are array indices. Each key is
// used more than once, so that the later uses find the index that the first one found.
func TestStringIndexKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want wtf8.String
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
			cx := newVM(t)
			v, err := runScript(cx, "index", tc.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := cx.ToString(v); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrimitiveElements(t *testing.T) {
	cx := engine.HostContext(engine.NewVM(nil))
	o := cx.NewString("abc").Object()
	if _, ok := o.Element(0); ok {
		t.Error("Element reported a stored element for a primitive")
	}
	if o.SetElement(0, engine.True) {
		t.Error("SetElement reported a stored element for a primitive")
	}
}

func TestGetPrototypeFromConstructorMissingIntrinsic(t *testing.T) {
	cx := newVM(t)
	var target engine.Value
	cx.CreateRealm(func(cx *engine.ExecutionContext) {
		var err error
		target, err = runScript(cx, "target", `function Target() {} Target.prototype = null; Target;`)
		if err != nil {
			t.Fatal(err)
		}
	})

	// Only the initial realm has the Date built-in.
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for the missing Date prototype in the target's realm")
		}
	}()
	cx.GetPrototypeFromConstructor(target.Object(), engine.IntrinsicDatePrototype)
}
