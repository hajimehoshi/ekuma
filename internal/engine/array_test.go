// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"runtime"
	"testing"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// TestArrayWithLengthFilledFromEnd tests that filling an array created with its length from the
// end keeps every element in the storage of the elements, which needs no key for an index.
func TestArrayWithLengthFilledFromEnd(t *testing.T) {
	cx := newTestContext(t)
	before := engine.InternedCount(cx)
	if _, err := runScript(cx, "fill", `o = new Array(500); for (i = 499; i >= 0; i--) o[i] = i;`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := engine.InternedCount(cx); got != before {
		t.Errorf("got %d interned strings, want %d", got, before)
	}
}

func TestPushCapacityAfterLongRuns(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "build", `
		function build(n, a = []) {
			while (a.length < n) a.push(a.length);
			return a;
		}
		build(256);
		build(256);
		// Leave a 64-element shifted prefix in the old backing storage.
		const shifted = new Array(128).fill(0);
		for (let i = 0; i < 64; i++) shifted.shift();
		shifted.length = 0;
		build(8, shifted);
	`)
	if err != nil {
		t.Fatal(err)
	}
	// Filling the first grown buffer can reserve space based on past runs.
	if got := engine.ElementStorage(v); got <= 8 || got > 64 {
		t.Errorf("capacity after long runs: got %d, want more than 8 and at most 64", got)
	}
	v, err = runScript(cx, "short", "build(8)")
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.ElementStorage(v); got > 8 {
		t.Errorf("capacity after a short run: got %d, want at most 8", got)
	}
}

// TestPushCapacityOfInPlacePushes tests that the pushes that the interpreter runs in place leave the
// hints for the capacity of later arrays that the generic calls leave.
func TestPushCapacityOfInPlacePushes(t *testing.T) {
	var capacities []int
	for _, push := range []string{"a.push(a.length)", "Reflect.apply(Array.prototype.push, a, [a.length])"} {
		cx := newTestContext(t)
		v, err := runScript(cx, "build", `
			function build(n) {
				var a = [];
				while (a.length < n) `+push+`;
				return a;
			}
			build(40);
			build(40);
			build(8);
		`)
		if err != nil {
			t.Fatal(err)
		}
		capacities = append(capacities, engine.ElementStorage(v))
	}
	if capacities[0] != capacities[1] {
		t.Errorf("capacity after the pushes in place: got %d, want %d after the generic calls", capacities[0], capacities[1])
	}
}

func TestIndexedAppendAfterPrototypeMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
	}{
		{
			name:   "define inherited setter",
			change: `Object.defineProperty(Array.prototype, 1, {set: record});`,
		},
		{
			name: "insert proxy in prototype chain",
			change: `
				Object.setPrototypeOf(Array.prototype, new Proxy(Object.prototype, {
					set(target, key, value, receiver) {
						if (key !== "1") return Reflect.set(target, key, value, receiver);
						record.call(receiver, value);
						return true;
					}
				}));
			`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			v, err := runScript(cx, "append", `
				function literal(a, value) { a[1] = value; }
				function indexed(a, i, value) { a[i] = value; }
				function push(a, value) { return a.push(value); }
				function one() { const a = []; a.push(0); return a; }
				literal(one(), 9);
				indexed(one(), 1, 9);
				push(one(), 9);
				const a = one(), b = one(), c = one();
				let seen = 0;
				function record(value) {
					if (this === a || this === b || this === c) seen = 10 * seen + value;
				}
				`+tc.change+`
				literal(a, 2);
				indexed(b, 1, 3);
				seen === 23 && push(c, 4) === 2 && seen === 234 && c.length === 2 &&
					!Object.hasOwn(a, 1) && !Object.hasOwn(b, 1) && !Object.hasOwn(c, 1);
			`)
			if err != nil {
				t.Fatal(err)
			}
			if v.O != storage.TrueObject {
				t.Errorf("indexed appends returned %v, want true", v)
			}
		})
	}
}

func TestIndexedPrototypePromotion(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "boxed",
			src:  `({0: "boxed"});`,
		},
		{
			name: "packed",
			src:  `const a = []; for (let i = 0; i < 24; i++) a.push(i); a;`,
		},
		{
			name: "sparse",
			src:  `Object.defineProperty({}, 500, {value: 1});`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			v, err := runScript(cx, "prototype", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			o := v.Object()
			if tc.name == "packed" && o.Raw().NumCount() == 0 {
				t.Fatal("prototype fixture has no packed elements")
			}
			space := o.Shape().Space
			if space.ProtoElemsDirty {
				t.Fatal("indexed object dirtied the prototype summary before promotion")
			}
			cx.NewObject(o)
			if !space.ProtoElemsDirty {
				t.Error("promoting an indexed object left the prototype summary clear")
			}
		})
	}
}

func TestNumericArrayOperationsKeepPackedStorage(t *testing.T) {
	const setup = `
		function numbers() {
			const a = [];
			for (let i = 0; i < 41; i++) a[i] = i;
			return a;
		}
	`
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "spread after a numeric prefix",
			src:  `[-1, ...numbers(), ...[41, 42]]`,
			want: `a.length === 44 && a[0] === -1 && a[41] === 40 && a[43] === 42`,
		},
		{
			name: "spread into call arguments",
			src: `(() => {
				const a = numbers();
				function f(x) { a[0] = 100; return x + arguments[40]; }
				if (f(...a) !== 40) throw Error();
				return a;
			})()`,
			want: `a.length === 41 && a[0] === 100`,
		},
		{
			name: "multiple arguments to push across growth",
			src: `(() => {
				const a = [];
				for (let i = 0; i < 40; i += 2) a.push(i, i + 1);
				return a;
			})()`,
			want: `a.length === 40 && a[15] === 15 && a[16] === 16 && a[39] === 39`,
		},
		{
			name: "splice moves elements in both directions",
			src: `(() => {
				const a = numbers();
				const removed = a.splice(38, 1, -1, -2);
				if (removed.length !== 1 || removed[0] !== 38 || a.pop() !== 40) throw Error();
				removed[0] = 0;
				a.splice(38, 2);
				return a;
			})()`,
			want: `a.length === 39 && a[37] === 37 && a[38] === 39`,
		},
		{
			name: "splice returns a packed copy after a mixed insertion",
			src: `(() => {
				const a = numbers();
				const removed = a.splice(1, 2, "mixed");
				if (a.length !== 40 || a[1] !== "mixed" || a[2] !== 3) throw Error();
				return removed;
			})()`,
			want: `a.length === 2 && a[0] === 1 && a[1] === 2`,
		},
		{
			name: "destructuring rest copies a packed suffix",
			src: `(() => {
				const a = numbers();
				const [, ...rest] = a;
				rest[0] = -1;
				a[2] = -2;
				if (a[1] !== 1 || rest[1] !== 2) throw Error();
				return rest;
			})()`,
			want: `a.length === 40 && a[0] === -1 && a[39] === 40`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			v, err := runScript(cx, tc.name, setup+`var a = `+tc.src+`; if (!(`+tc.want+`)) throw Error(); a;`)
			if err != nil {
				t.Fatal(err)
			}
			if n := v.O.Raw().NumCount(); n != storage.ElementCount(v) || n == 0 {
				t.Errorf("got %d packed numbers in an array of %d elements", n, storage.ElementCount(v))
			}
		})
	}
}

// TestUnpackedSliceReleasesNumbers tests that a slice of a packed array keeps its old numbers
// unreachable once a store of another value boxes its elements.
func TestUnpackedSliceReleasesNumbers(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "slice", `
		var a = [];
		for (var i = 0; i < 100; i++) a.push(i);
		var b = a.slice();
		b;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v.O.Raw().NumCount() == 0 {
		t.Fatal("the slice of a packed array is not packed")
	}
	w := weak.Make(&v.O.Raw().Nums()[0])
	if _, err := runScript(cx, "unpack", `b[0] = "x";`); err != nil {
		t.Fatal(err)
	}
	if v.O.Raw().NumCount() != 0 {
		t.Fatal("the slice is still packed after a store of a string")
	}
	runtime.GC()
	if w.Value() != nil {
		t.Error("the unpacked slice keeps its old numbers reachable")
	}
	runtime.KeepAlive(v)
}
