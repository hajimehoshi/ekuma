// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// BenchmarkWorkload runs testdata/workload.js, a synthetic workload shaped like a scene of a
// 2D tile-based game. One iteration is one frame of the scene.
func BenchmarkWorkload(b *testing.B) {
	src, err := os.ReadFile("testdata/workload.js")
	if err != nil {
		b.Fatal(err)
	}
	for _, plugins := range []int{12, 0} {
		b.Run(fmt.Sprintf("plugins=%d", plugins), func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "workload.js", string(src)); err != nil {
				b.Fatal(err)
			}
			if _, err := runScript(ctx, &vm, "setup", fmt.Sprintf("setup(60, %d)", plugins)); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// gameDataJSON returns JSON text shaped like the data files of a 2D role-playing game:
// an array of n records with mixed fields, nested arrays of numbers and some non-ASCII strings.
func gameDataJSON(n int) string {
	names := []string{"Slime", "Bat", "スライム", "コウモリ", "Goblin", "ゴブリン王", "Dragon", "Ωmega"}
	seed := uint32(1)
	rnd := func(m int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % m
	}
	var b strings.Builder
	b.WriteString("[null")
	for i := 1; i <= n; i++ {
		name := names[rnd(len(names))]
		fmt.Fprintf(&b, `,{"id":%d,"name":"%s %d","note":"<hp:%d>\n<drop:%d>","battlerName":"%s","battlerHue":%d,`,
			i, name, i, rnd(1000), rnd(50), name, rnd(360))
		fmt.Fprintf(&b, `"exp":%d,"gold":%d,"rate":%d.%02d,"hidden":%t,"script":null,`, rnd(10000), rnd(5000), rnd(10), rnd(100), rnd(2) == 0)
		b.WriteString(`"params":[`)
		for j := range 8 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", rnd(999)+1)
		}
		b.WriteString(`],"dropItems":[`)
		for j := range 3 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"kind":%d,"dataId":%d,"denominator":%d}`, rnd(4), rnd(200), rnd(10)+1)
		}
		b.WriteString(`],"traits":[`)
		for j := range rnd(4) + 1 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"code":%d,"dataId":%d,"value":%d.%d}`, rnd(70), rnd(10), rnd(2), rnd(10))
		}
		b.WriteString(`]}`)
	}
	b.WriteString("]")
	return b.String()
}

// BenchmarkJSON parses and stringifies generated game data.
func BenchmarkJSON(b *testing.B) {
	text := gameDataJSON(3000)
	ctx := b.Context()
	var vm ekuma.VM
	if err := vm.Global().Set("text", text); err != nil {
		b.Fatal(err)
	}
	if _, err := runScript(ctx, &vm, "setup", `
		function parse() { return JSON.parse(text); }
		var data = parse();
		function stringify() { return JSON.stringify(data); }`); err != nil {
		b.Fatal(err)
	}
	b.Run("parse", func(b *testing.B) {
		b.SetBytes(int64(len(text)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := vm.Global().Call(ctx, "parse"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("stringify", func(b *testing.B) {
		out, err := vm.Global().Call(ctx, "stringify")
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(out.String())))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := vm.Global().Call(ctx, "stringify"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// collectionBenchJS defines setup(coll, kind, n), which creates a Map or a Set of n keys of
// the given kind, and run(op, total), which performs total operations op on it.
const collectionBenchJS = `
function makeKeys(kind, n) {
	var keys = [];
	for (var i = 0; i < n; i++) {
		if (kind === "int") keys.push(i);
		else if (kind === "string") keys.push("key" + i);
		else keys.push({id: i});
	}
	return keys;
}
function fill(isMap, keys) {
	var c = isMap ? new Map() : new Set();
	for (var i = 0; i < keys.length; i++) {
		if (isMap) c.set(keys[i], i); else c.add(keys[i]);
	}
	return c;
}
var isMap, keys, c;
function setup(coll, kind, n) {
	isMap = coll === "Map";
	keys = makeKeys(kind, n);
	c = fill(isMap, keys);
}
function run(op, total) {
	var n = keys.length, sum = 0;
	for (var done = 0; done < total; done += n) {
		var m = Math.min(n, total - done), i;
		switch (op) {
		case "set":
			c = isMap ? new Map() : new Set();
			if (isMap) for (i = 0; i < m; i++) c.set(keys[i], i);
			else for (i = 0; i < m; i++) c.add(keys[i]);
			break;
		case "get":
			for (i = 0; i < m; i++) sum += c.get(keys[i]);
			break;
		case "has":
			for (i = 0; i < m; i++) if (c.has(keys[i])) sum++;
			break;
		case "delete":
			for (i = 0; i < m; i++) c.delete(keys[i]);
			if (isMap) for (i = 0; i < m; i++) c.set(keys[i], i);
			else for (i = 0; i < m; i++) c.add(keys[i]);
			break;
		case "iterate":
			i = 0;
			for (var v of c.values()) if (++i >= m) break;
			break;
		case "forEach":
			c.forEach(function(v) { sum++; });
			break;
		}
	}
	return sum;
}
`

// BenchmarkCollection measures Map and Set operations. One iteration is one operation on one
// key; delete counts deleting a key and adding it again, and iterate and forEach visiting one entry.
func BenchmarkCollection(b *testing.B) {
	for _, coll := range []string{"Map", "Set"} {
		for _, op := range []string{"set", "get", "has", "delete", "iterate", "forEach"} {
			if coll == "Set" && op == "get" {
				continue
			}
			for _, kind := range []string{"int", "string", "object"} {
				for _, n := range []int{8, 1000, 100000} {
					b.Run(fmt.Sprintf("%s/%s/%s/n=%d", coll, op, kind, n), func(b *testing.B) {
						ctx := b.Context()
						var vm ekuma.VM
						if _, err := runScript(ctx, &vm, "collection.js", collectionBenchJS); err != nil {
							b.Fatal(err)
						}
						if _, err := runScript(ctx, &vm, "setup", fmt.Sprintf("setup(%q, %q, %d)", coll, kind, n)); err != nil {
							b.Fatal(err)
						}
						b.ReportAllocs()
						b.ResetTimer()
						if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d)", op, b.N)); err != nil {
							b.Fatal(err)
						}
					})
				}
			}
		}
	}
}

const weakMapBenchJS = `
var a = new WeakMap(), b = new WeakMap(), keys = [];
for (var i = 0; i < 1024; i++) {
	keys.push({});
	a.set(keys[i], i);
}
var both = [];
for (var i = 0; i < 1024; i++) {
	both.push({});
	a.set(both[i], i);
	b.set(both[i], i);
}
function run(op, n) {
	var sum = 0, i;
	switch (op) {
	case "set":
		for (i = 0; i < n; i++) a.set({}, i);
		break;
	case "get":
		for (i = 0; i < n; i++) sum += a.get(keys[i & 1023]);
		break;
	case "has":
		for (i = 0; i < n; i++) if (a.has(keys[i & 1023])) sum++;
		break;
	case "delete":
		for (i = 0; i < n; i++) {
			a.delete(keys[i & 1023]);
			a.set(keys[i & 1023], i);
		}
		break;
	case "set2":
		for (i = 0; i < n; i++) {
			var k = {};
			a.set(k, i);
			b.set(k, i);
		}
		break;
	case "get2":
		for (i = 0; i < n; i++) sum += a.get(both[i & 1023]) + b.get(both[i & 1023]);
		break;
	}
	return sum;
}
`

// BenchmarkWeakMap measures WeakMap operations. One iteration is one operation: set adds a fresh
// key, delete deletes a key and adds it again, and set2 and get2 use a key of two WeakMaps,
// setting a fresh key in both and getting a key from both.
func BenchmarkWeakMap(b *testing.B) {
	for _, op := range []string{"set", "get", "has", "delete", "set2", "get2"} {
		b.Run(op, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "weakmap.js", weakMapBenchJS); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d)", op, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkEval measures eval. One iteration is one call: eval evaluates a short expression
// at one call site, whose compiled code is reused; marked calls a function that contains a
// direct eval it does not reach, and unmarked the same function without the eval; function
// creates a function with new Function and calls it.
func BenchmarkEval(b *testing.B) {
	const src = `
		function evalExpr(x) { return eval("x * 2 + 1"); }
		function marked(x, y) {
			if (x < 0) return eval("y");
			var s = 0;
			for (var i = 0; i < 4; i++) s += x * i + y;
			return s;
		}
		function unmarked(x, y) {
			if (x < 0) return y;
			var s = 0;
			for (var i = 0; i < 4; i++) s += x * i + y;
			return s;
		}
		var runs = {
			eval(n) { var s = 0; for (var i = 0; i < n; i++) s += evalExpr(i); return s; },
			marked(n) { var s = 0; for (var i = 0; i < n; i++) s += marked(i, 1); return s; },
			unmarked(n) { var s = 0; for (var i = 0; i < n; i++) s += unmarked(i, 1); return s; },
			function(n) { var s = 0; for (var i = 0; i < n; i++) s += new Function("x", "return x * 2 + 1")(i); return s; },
		};`
	for _, name := range []string{"eval", "marked", "unmarked", "function"} {
		b.Run(name, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "eval.js", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("runs[%q](%d)", name, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkGenerator measures generator creation and resumption.
func BenchmarkGenerator(b *testing.B) {
	const src = `
		function* one() { yield 1; }
		function* many(n) {
			for (let i = 0; i < n; i++) yield 1;
		}
		function create(n) {
			let sum = 0;
			for (let i = 0; i < n; i++) sum += one().next().value;
			if (sum !== n) throw new Error("incorrect result");
		}
		function resume(n) {
			let sum = 0;
			for (const x of many(n)) sum += x;
			if (sum !== n) throw new Error("incorrect result");
		}
	`
	for _, name := range []string{"create", "resume"} {
		b.Run(name, func(b *testing.B) {
			var vm ekuma.VM
			if _, err := runScript(b.Context(), &vm, "generator.js", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("%s(%d)", name, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkAsync measures promises and async functions. One iteration is one await of
// a resolved promise, one call of an async function that awaits once, one step of
// a for-await loop over an async generator, or one awaited promise combinator of three promises.
func BenchmarkAsync(b *testing.B) {
	const src = `
		const p = Promise.resolve(1);
		async function await_(n) {
			for (let i = 0; i < n; i++) await p;
		}
		async function awaitOnce() {
			await p;
		}
		function call(n) {
			for (let i = 0; i < n; i++) awaitOnce();
		}
		async function* gen(n) {
			for (let i = 0; i < n; i++) yield i;
		}
		async function forAwait(n) {
			for await (const x of gen(n));
		}
		async function all(n) {
			const q = Promise.resolve(2), r = Promise.resolve(3);
			for (let i = 0; i < n; i++) await Promise.all([p, q, r]);
		}
		async function allSettled(n) {
			const q = Promise.reject(2);
			for (let i = 0; i < n; i++) await Promise.allSettled([p, q, p]);
		}
		async function allSettledKeyed(n) {
			const q = Promise.reject(2);
			for (let i = 0; i < n; i++) await Promise.allSettledKeyed({a: p, b: q, c: p});
		}
	`
	for _, name := range []string{"await", "call", "forAwait", "all", "allSettled", "allSettledKeyed"} {
		b.Run(name, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "async.js", src); err != nil {
				b.Fatal(err)
			}
			f := name
			if f == "await" {
				f = "await_"
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("%s(%d)", f, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// typedArrayBenchJS defines setup(kind, n), which creates two arrays of n elements of the given
// kind ("Array" for plain arrays of numbers), and run(op, total), which performs op on total elements.
const typedArrayBenchJS = `
var a, b;
function make(kind, n, v) {
	if (kind === "Array") {
		var r = [];
		for (var i = 0; i < n; i++) r.push(v);
		return r;
	}
	return new globalThis[kind](n).fill(v);
}
function setup(kind, n) {
	a = make(kind, n, 0);
	b = make(kind, n, 1);
}
function setupInt32(buffer, n) {
	a = new Int32Array(new globalThis[buffer](4 * n));
}
function run(op, total) {
	var n = a.length, s = 0, i, m;
	for (var done = 0; done < total; done += n) {
		m = Math.min(n, total - done);
		switch (op) {
		case "read":
			for (i = 0; i < m; i++) s += a[i];
			break;
		case "write":
			for (i = 0; i < m; i++) a[i] = i;
			break;
		case "readLength":
			for (i = 0; i < a.length; i++) s += a[i];
			break;
		case "set":
			a.set(b);
			break;
		case "fill":
			a.fill(done);
			break;
		case "subarray":
			for (i = 0; i < m; i++) s += a.subarray(1, 9).length;
			break;
		case "atomicsAdd":
			for (i = 0; i < m; i++) s += Atomics.add(a, i, 1);
			break;
		}
	}
	return s;
}
`

// BenchmarkTypedArray measures typed array element access and bulk operations. One iteration
// is one element read, written, set or filled, or one subarray created. readLength reads the
// length in the condition of the loop.
func BenchmarkTypedArray(b *testing.B) {
	for _, op := range []string{"read", "readLength", "write", "set", "fill", "subarray"} {
		for _, kind := range []string{"Float64Array", "Float32Array", "Int32Array", "Uint8Array", "Array"} {
			if kind == "Array" && op != "read" && op != "readLength" && op != "write" {
				continue
			}
			b.Run(op+"/"+kind, func(b *testing.B) {
				ctx := b.Context()
				var vm ekuma.VM
				if _, err := runScript(ctx, &vm, "typedarray.js", typedArrayBenchJS); err != nil {
					b.Fatal(err)
				}
				if _, err := runScript(ctx, &vm, "setup", fmt.Sprintf("setup(%q, 1024)", kind)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d)", op, b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkSharedTypedArray compares the element accesses of an Int32Array over a
// SharedArrayBuffer with those of one over an ArrayBuffer, in one VM. One iteration is one
// element read or written, or one Atomics.add.
func BenchmarkSharedTypedArray(b *testing.B) {
	for _, op := range []string{"read", "write", "atomicsAdd"} {
		for _, buffer := range []string{"ArrayBuffer", "SharedArrayBuffer"} {
			b.Run(op+"/"+buffer, func(b *testing.B) {
				ctx := b.Context()
				var vm ekuma.VM
				if _, err := runScript(ctx, &vm, "typedarray.js", typedArrayBenchJS); err != nil {
					b.Fatal(err)
				}
				if _, err := runScript(ctx, &vm, "setup", fmt.Sprintf("setupInt32(%q, 1024)", buffer)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d)", op, b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkSparseArray measures the array methods that skip or read holes on a sparse array of
// length 100000 that has 10 elements. One iteration is one call of the method. The methods that
// change the array run on a new array in each iteration.
func BenchmarkSparseArray(b *testing.B) {
	for _, method := range []string{"every", "forEach", "reduce", "indexOf", "lastIndexOf", "join", "toLocaleString", "includes", "copyWithin", "shift", "unshift", "spliceRemove", "spliceInsert", "reverse"} {
		b.Run(method, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "sparse.js", sparseArrayBenchJS); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d)", method, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

const sparseArrayBenchJS = `
function sparse() {
	var a = [];
	for (var i = 0; i < 10; i++) {
		a[i * 10000 + 5000] = i;
	}
	a.length = 100000;
	return a;
}
var a = sparse();
function run(method, n) {
	var s = 0;
	for (var i = 0; i < n; i++) {
		switch (method) {
		case "every":
			if (a.every(function (x) { return x >= 0; })) s++;
			break;
		case "forEach":
			a.forEach(function (x) { s += x; });
			break;
		case "reduce":
			s += a.reduce(function (acc, x) { return acc + x; }, 0);
			break;
		case "indexOf":
			s += a.indexOf(-1);
			break;
		case "lastIndexOf":
			s += a.lastIndexOf(-1);
			break;
		case "join":
			s += a.join().length;
			break;
		case "toLocaleString":
			s += a.toLocaleString().length;
			break;
		case "includes":
			if (a.includes(-1)) s++;
			break;
		case "copyWithin":
			s += sparse().copyWithin(1, 0).length;
			break;
		case "shift":
			sparse().shift();
			break;
		case "unshift":
			s += sparse().unshift(-1);
			break;
		case "spliceRemove":
			s += sparse().splice(1, 1).length;
			break;
		case "spliceInsert":
			s += sparse().splice(1, 0, -1).length;
			break;
		case "reverse":
			s += sparse().reverse().length;
			break;
		}
	}
	return s;
}
`

// BenchmarkSparseArrayGrowing walks a sparse array whose callback stores a new sparse index
// at every step.
func BenchmarkSparseArrayGrowing(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "sparse.js", sparseArrayGrowingBenchJS); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%d, %d)", n, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

const sparseArrayGrowingBenchJS = `
function run(k, n) {
	var s = 0;
	for (var j = 0; j < n; j++) {
		var a = [];
		for (var i = 0; i < k; i++) {
			a[i * 1000 + 500] = i;
		}
		var len = a.length;
		a.forEach(function (x, i) {
			s += x;
			a[len + i] = x;
		});
	}
	return s;
}
`

const proxyBenchJS = `
var target = {
	x: 1,
	m() { return this.x; },
};
var objects = {
	plain: target,
	proxy: new Proxy(target, {}),
	getTrap: new Proxy(target, {
		get(t, k, r) { return Reflect.get(t, k, r); },
	}),
};
function run(kind, op, n) {
	var o = objects[kind];
	var s = 0;
	if (op === "get") {
		for (var i = 0; i < n; i++) s += o.x;
	} else {
		for (var i = 0; i < n; i++) s += o.m();
	}
	return s;
}
`

// BenchmarkProxy measures property reads and method calls on a plain object, on a proxy of it
// without traps, and on a proxy of it with a get trap. One iteration is one read or one call.
func BenchmarkProxy(b *testing.B) {
	for _, op := range []string{"get", "call"} {
		for _, kind := range []string{"plain", "proxy", "getTrap"} {
			b.Run(op+"/"+kind, func(b *testing.B) {
				ctx := b.Context()
				var vm ekuma.VM
				if _, err := runScript(ctx, &vm, "proxy.js", proxyBenchJS); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %q, %d)", kind, op, b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

const instanceofBenchJS = `
function F() {}
class A {}
class B extends A {}
const values = {
	function: [new F(), {}],
	subclass: [new B(), new A()],
};
function run(kind, c, n) {
	const [hit, miss] = values[kind];
	let count = 0;
	for (let i = 0; i < n; i++) {
		if ((i & 1 ? miss : hit) instanceof c) {
			count++;
		}
	}
	return count;
}
`

// BenchmarkInstanceof evaluates instanceof with an ordinary function and with a derived class
// on the right-hand side. One iteration is one instanceof.
func BenchmarkInstanceof(b *testing.B) {
	for _, kind := range []struct {
		name string
		ctor string
	}{
		{
			name: "function",
			ctor: "F",
		},
		{
			name: "subclass",
			ctor: "B",
		},
	} {
		b.Run(kind.name, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "instanceof.js", instanceofBenchJS); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %s, %d)", kind.name, kind.ctor, b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkImportedCall calls, in a loop, a function that another module exports, and compares
// that with the same code in a script. One iteration is one call.
func BenchmarkImportedCall(b *testing.B) {
	const lib = `function add(a, b) { return a + b; }`
	const loop = `function run(n) { let s = 0; for (let i = 0; i < n; i++) { s = add(s, i); } return s; }`
	b.Run("script", func(b *testing.B) {
		var vm ekuma.VM
		if _, err := runScript(b.Context(), &vm, "script", lib+loop); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
			b.Fatal(err)
		}
	})
	b.Run("module", func(b *testing.B) {
		var vm ekuma.VM
		l := newMemoryModules(&vm, map[string]string{
			"lib":  "export " + lib,
			"main": `import { add } from "lib";` + loop + `globalThis.run = run;`,
		})
		m, err := l.load(ekuma.ModuleRequest{Specifier: "main"})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := m.Evaluate(b.Context()); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
			b.Fatal(err)
		}
	})
}

// BenchmarkNewVM creates a VM and initialises it, alone and followed by a trivial script.
func BenchmarkNewVM(b *testing.B) {
	b.Run("init", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var vm ekuma.VM
			vm.Global()
		}
	})
	b.Run("script", func(b *testing.B) {
		b.ReportAllocs()
		ctx := b.Context()
		for b.Loop() {
			var vm ekuma.VM
			if _, err := runScript(ctx, &vm, "script", "1 + 1"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkTailCall compares a loop written as tail recursion in strict code with the same
// loop as a while statement and with the same recursion in sloppy code, which grows the stack.
// The other cases are tail recursion through call, through apply forwarding the arguments and
// through a spread of the arguments.
func BenchmarkTailCall(b *testing.B) {
	const n = 1000
	ctx := b.Context()
	var vm ekuma.VM
	if _, err := runScript(ctx, &vm, "setup", `
		function tail(n, acc) { "use strict"; return n === 0 ? acc : tail(n - 1, acc + n); }
		function loop(n, acc) { "use strict"; while (n !== 0) { acc += n; n--; } return acc; }
		function sloppy(n, acc) { return n === 0 ? acc : sloppy(n - 1, acc + n); }
		function call(n, acc) { "use strict"; return n === 0 ? acc : call.call(this, n - 1, acc + n); }
		function apply(n, acc) { "use strict"; return n === 0 ? acc : applyStep.apply(this, arguments); }
		function applyStep(n, acc) { "use strict"; return apply(n - 1, acc + n); }
		function applyArray(n, acc) { "use strict"; return n === 0 ? acc : applyArray.apply(this, [n - 1, acc + n]); }
		function spread(n, acc) { "use strict"; return n === 0 ? acc : spreadStep(...arguments); }
		function spreadStep(n, acc) { "use strict"; return spread(n - 1, acc + n); }`); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"tail", "loop", "sloppy", "call", "apply", "applyArray", "spread"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := vm.Global().Call(ctx, name, n, 0); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/iter")
		})
	}
}

// BenchmarkRegExpLiteral measures the common uses of a regular expression literal. One iteration
// evaluates the expression once. Each literal case has a hoisted twin that evaluates the literal
// once before the loop, so the difference between the two is the cost of the RegExp object.
func BenchmarkRegExpLiteral(b *testing.B) {
	const setup = `
		var s = "the quick brown fox jumps over the lazy dog";
		var f = function(m) { return m; };
		function countExec(re) { var c = 0; while (re.exec(s)) c++; return c; }
		function countTest(re) { var c = 0; while (re.test(s)) c++; return c; }`
	cases := []struct {
		name string
		lit  string
		expr string
	}{
		{name: "replace/str", lit: `/o/g`, expr: `s.replace(LIT, "0")`},
		{name: "replace/fn", lit: `/o/g`, expr: `s.replace(LIT, f)`},
		{name: "match", lit: `/b(r)own/`, expr: `s.match(LIT)`},
		{name: "matchAll", lit: `/o/g`, expr: `[...s.matchAll(LIT)].length`},
		{name: "split", lit: `/ /`, expr: `s.split(LIT)`},
		{name: "search", lit: `/fox/`, expr: `s.search(LIT)`},
		{name: "test", lit: `/fox/`, expr: `LIT.test(s)`},
		{name: "exec", lit: `/f(o)x/`, expr: `LIT.exec(s)`},
		{name: "stored", lit: `/o/g`, expr: `(re = LIT, s.replace(re, "0"))`},
		{name: "loop/g", lit: `/o/g`, expr: `countExec(LIT)`},
		{name: "loop/y", lit: `/\w+|\s+/y`, expr: `countTest(LIT)`},
	}
	for _, c := range cases {
		for _, kind := range []string{"literal", "hoisted"} {
			b.Run(c.name+"/"+kind, func(b *testing.B) {
				lit, expr := "null", strings.Replace(c.expr, "LIT", c.lit, 1)
				if kind == "hoisted" {
					lit, expr = c.lit, strings.Replace(c.expr, "LIT", "lit", 1)
				}
				src := fmt.Sprintf(`%s
					function run(n) { var lit = %s, re, o; for (var i = 0; i < n; i++) o = %s; return o; }`, setup, lit, expr)
				ctx := b.Context()
				var vm ekuma.VM
				if _, err := runScript(ctx, &vm, "regexp.js", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringReplace measures string-pattern replacement. One iteration is one call.
func BenchmarkStringReplace(b *testing.B) {
	for _, expr := range []string{
		`s.replace("a", "x")`,
		`s.replace("z", "x")`,
		`s.replace("", "$&x")`,
		`s.replace("a", f)`,
		`s.replaceAll("a", "x")`,
		`s.replaceAll("", "x")`,
	} {
		b.Run(expr, func(b *testing.B) {
			var vm ekuma.VM
			if _, err := runScript(b.Context(), &vm, "setup", `var s = "😀abcabcabcabcabcabcabcabc"; var f = function(m, p) { return p; }; function run(n) { var o; for (var i = 0; i < n; i++) o = `+expr+`; return o; }`); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkStringSplitEmpty(b *testing.B) {
	for _, c := range []struct {
		name  string
		text  string
		limit string
	}{
		{
			name:  "ASCII",
			text:  strings.Repeat("a", 1024),
			limit: "undefined",
		},
		{
			name:  "Limited",
			text:  strings.Repeat("a", 1024),
			limit: "8",
		},
		{
			name:  "UTF16",
			text:  strings.Repeat("😀", 32),
			limit: "undefined",
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			ctx := b.Context()
			var vm ekuma.VM
			src := fmt.Sprintf(`var s = %q;
				function run(n) { var out; for (var i = 0; i < n; i++) out = s.split("", %s); return out; }`, c.text, c.limit)
			if _, err := runScript(ctx, &vm, "split.js", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkURIEncode(b *testing.B) {
	safe := strings.Repeat("abcXYZ09-_.!~*'()", 128)
	for _, name := range []string{"encodeURI", "encodeURIComponent"} {
		for _, c := range []struct {
			name string
			text string
		}{
			{
				name: "Unchanged",
				text: safe,
			},
			{
				name: "LateEscape",
				text: safe + " ",
			},
			{
				name: "EarlyEscape",
				text: " " + safe,
			},
			{
				name: "Reserved",
				text: strings.Repeat(";/?:@&=+$,#", 128),
			},
			{
				name: "Unicode",
				text: strings.Repeat("日本語😀", 128),
			},
		} {
			b.Run(name+"/"+c.name, func(b *testing.B) {
				var vm ekuma.VM
				global := vm.Global()
				input := vm.ValueOf(c.text)
				ctx := b.Context()
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := global.Call(ctx, name, input); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkUint8ArrayDecodeInto(b *testing.B) {
	for _, setter := range []struct {
		method string
		input  string
	}{
		{
			method: "setFromHex",
			input:  "00",
		},
		{
			method: "setFromBase64",
			input:  "AA==",
		},
	} {
		for _, buffer := range []string{"ArrayBuffer", "SharedArrayBuffer"} {
			b.Run(setter.method+"/"+buffer, func(b *testing.B) {
				var vm ekuma.VM
				_, err := runScript(b.Context(), &vm, "setup", fmt.Sprintf(`
					const dst = new Uint8Array(new %s(65536));
					function decode() { return dst.%s(%q); }
				`, buffer, setter.method, setter.input))
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				for b.Loop() {
					if _, err := vm.Global().Call(b.Context(), "decode"); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkDateGetters(b *testing.B) {
	for _, expression := range []string{
		"d.getUTCMilliseconds()",
		"d.getUTCHours()",
		"d.getUTCFullYear()",
		"d.getUTCMonth()",
		"d.getUTCDate()",
		"d.getMilliseconds()",
		"d.getMonth()",
		"d.getTime()%1000",
	} {
		b.Run(expression, func(b *testing.B) {
			ctx := context.Background()
			var vm ekuma.VM
			fn, err := runScript(ctx, &vm, "date-getter", `(() => {
				const d = new Date(1700000000123);
				return () => `+expression+`;
			})()`)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := fn.Invoke(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPropertyDescriptor measures retrieving or forwarding one property descriptor.
func BenchmarkPropertyDescriptor(b *testing.B) {
	for _, c := range []struct {
		name  string
		setup string
		expr  string
	}{
		{
			name:  "Data",
			setup: `const o = {x: 1};`,
			expr:  `Object.getOwnPropertyDescriptor(o, "x")`,
		},
		{
			name:  "Accessor",
			setup: `const o = {get x() { return 1; }};`,
			expr:  `Reflect.getOwnPropertyDescriptor(o, "x")`,
		},
		{
			name: "Partial",
			setup: `const d = {value: 1};
				const o = new Proxy({}, {defineProperty(target, key, desc) { last = desc; return true; }});`,
			expr: `Reflect.defineProperty(o, "x", d)`,
		},
		{
			name: "Empty",
			setup: `const d = {};
				const o = new Proxy({}, {defineProperty(target, key, desc) { last = desc; return true; }});`,
			expr: `Reflect.defineProperty(o, "x", d)`,
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			var vm ekuma.VM
			src := `let last; ` + c.setup + ` function run(n) { for (let i = 0; i < n; i++) { ` + c.expr + `; } }`
			if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkBuiltinResult measures creating a fixed-size result record.
func BenchmarkBuiltinResult(b *testing.B) {
	for _, c := range []struct {
		name  string
		setup string
		expr  string
	}{
		{
			name:  "ProxyRevocable",
			setup: `const target = {}, handler = {};`,
			expr:  `Proxy.revocable(target, handler)`,
		},
		{
			name: "PromiseWithResolvers",
			expr: `Promise.withResolvers()`,
		},
		{
			name:  "AtomicsWaitAsync",
			setup: `const a = new Int32Array(new SharedArrayBuffer(4));`,
			expr:  `Atomics.waitAsync(a, 0, 1)`,
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			var vm ekuma.VM
			src := c.setup + ` let last; function run(n) { for (let i = 0; i < n; i++) last = ` + c.expr + `; }`
			if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkIntlParts measures formatting one value or range as part records.
func BenchmarkIntlParts(b *testing.B) {
	for _, c := range []struct {
		name  string
		setup string
		expr  string
	}{
		{
			name:  "Number",
			setup: `const f = new Intl.NumberFormat("en-US");`,
			expr:  `f.formatToParts(1234567.89)`,
		},
		{
			name:  "NumberRange",
			setup: `const f = new Intl.NumberFormat("en-US");`,
			expr:  `f.formatRangeToParts(1234.5, 6789.5)`,
		},
		{
			name:  "RelativeTime",
			setup: `const f = new Intl.RelativeTimeFormat("en-US");`,
			expr:  `f.formatToParts(1234, "day")`,
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			var vm ekuma.VM
			src := c.setup + ` let last; function run(n) { for (let i = 0; i < n; i++) last = ` + c.expr + `; }`
			if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkIntlSegmentRecords measures creating segment data or resolved options.
func BenchmarkIntlSegmentRecords(b *testing.B) {
	for _, c := range []struct {
		name  string
		setup string
		expr  string
	}{
		{
			name:  "Grapheme",
			setup: `const s = new Intl.Segmenter("en", {granularity:"grapheme"}).segment("Hello world");`,
			expr:  `s.containing(1)`,
		},
		{
			name:  "Word",
			setup: `const s = new Intl.Segmenter("en", {granularity:"word"}).segment("Hello world");`,
			expr:  `s.containing(1)`,
		},
		{
			name:  "Options",
			setup: `const s = new Intl.Segmenter("en");`,
			expr:  `s.resolvedOptions()`,
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			var vm ekuma.VM
			src := c.setup + ` let last; function run(n) { for (let i = 0; i < n; i++) last = ` + c.expr + `; }`
			if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkIntlResolvedOptions measures creating Intl resolved-options records.
func BenchmarkIntlResolvedOptions(b *testing.B) {
	for _, c := range []struct {
		name  string
		setup string
		expr  string
	}{
		{
			name:  "List",
			setup: `const f = new Intl.ListFormat("en");`,
			expr:  `f.resolvedOptions()`,
		},
		{
			name:  "DisplayLanguage",
			setup: `const f = new Intl.DisplayNames("en", {type:"language"});`,
			expr:  `f.resolvedOptions()`,
		},
		{
			name:  "DisplayRegion",
			setup: `const f = new Intl.DisplayNames("en", {type:"region"});`,
			expr:  `f.resolvedOptions()`,
		},
		{
			name:  "Duration",
			setup: `const f = new Intl.DurationFormat("en");`,
			expr:  `f.resolvedOptions()`,
		},
	} {
		b.Run(c.name, func(b *testing.B) {
			var vm ekuma.VM
			src := c.setup + ` let last; function run(n) { for (let i = 0; i < n; i++) last = ` + c.expr + `; }`
			if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// BenchmarkStringBuildLength builds a string while reading its UTF-16 length.
func BenchmarkStringBuildLength(b *testing.B) {
	for _, direction := range []string{"Append", "Prepend"} {
		for _, chunk := range []string{"abcdef", "é😀"} {
			b.Run(direction+"/"+chunk, func(b *testing.B) {
				expression := "s + chunk"
				if direction == "Prepend" {
					expression = "chunk + s"
				}
				var vm ekuma.VM
				src := fmt.Sprintf(`function run(n) {
						const chunk = %q;
						let sum = 0;
						for (let j = 0; j < n; j++) {
							let s = "";
							for (let i = 0; i < 20000; i++) { s = %s; sum += s.length; }
						}
						return sum;
					}`, chunk, expression)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringCharacterAccess measures one character-access method call.
func BenchmarkStringCharacterAccess(b *testing.B) {
	for _, s := range []string{"abcdefgh", "é日", "😀😀", "abcdefghijklmnopqrstuvwxyz", "日本語の長い文字列"} {
		for _, form := range []string{"Literal", "Created"} {
			for _, method := range []string{"charAt", "at", "charCodeAt", "codePointAt"} {
				b.Run(form+"/"+s+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					expression := fmt.Sprintf("%q", s)
					if form == "Created" {
						expression = fmt.Sprintf("%q.slice(0, -1)", s+"!")
					}
					src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(1); return result; }`, expression, method)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringSubstring measures one substring operation.
func BenchmarkStringSubstring(b *testing.B) {
	for _, s := range []string{"abcdefgh", "é日a", "😀😀", "abcdefghijklmnopqrstuvwxyz", "日本語の長い文字列"} {
		for _, form := range []string{"Literal", "Created"} {
			for _, expr := range []string{"s.slice(1, 3)", "s.substring(1, 3)", "s.substr(1, 2)"} {
				b.Run(form+"/"+s+"/"+expr, func(b *testing.B) {
					var vm ekuma.VM
					expression := fmt.Sprintf("%q", s)
					if form == "Created" {
						expression = fmt.Sprintf("%q.slice(0, -1)", s+"!")
					}
					src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = %s; return result; }`, expression, expr)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringAffixes measures one prefix or suffix check.
func BenchmarkStringAffixes(b *testing.B) {
	for _, tc := range []struct {
		text    string
		pattern string
		start   int
		end     int
	}{
		{
			text:    "abcdefgh",
			pattern: `"bc"`,
			start:   1,
			end:     3,
		},
		{
			text:    "é日a",
			pattern: `"日"`,
			start:   1,
			end:     2,
		},
		{
			text:    "😀😀",
			pattern: `"\ud83d"`,
			start:   0,
			end:     1,
		},
		{
			text:    "abcdefghijklmnopqrstuvwxyz",
			pattern: `"bc"`,
			start:   1,
			end:     3,
		},
		{
			text:    "日本語の長い文字列",
			pattern: `"本語"`,
			start:   1,
			end:     3,
		},
	} {
		for _, form := range []string{"Literal", "CreatedReceiver", "CreatedBoth"} {
			for _, method := range []string{"startsWith", "endsWith"} {
				b.Run(form+"/"+tc.text+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					receiver := fmt.Sprintf("%q", tc.text)
					pattern := tc.pattern
					if form != "Literal" {
						receiver = fmt.Sprintf("%q.slice(0, -1)", tc.text+"!")
					}
					if form == "CreatedBoth" {
						pattern = "(" + pattern + " + '!').slice(0, -1)"
					}
					position := tc.start
					if method == "endsWith" {
						position = tc.end
					}
					src := fmt.Sprintf(`var s = %s, p = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(p, %d); return result; }`, receiver, pattern, method, position)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringSearch measures one string search.
func BenchmarkStringSearch(b *testing.B) {
	for _, tc := range []struct {
		text    string
		pattern string
	}{
		{
			text:    "abababab",
			pattern: `"ba"`,
		},
		{
			text:    "abcdefgh",
			pattern: `"zz"`,
		},
		{
			text:    "é日é",
			pattern: `"é"`,
		},
		{
			text:    "😀😀",
			pattern: `"\ude00"`,
		},
		{
			text:    "abababababababababab",
			pattern: `"ba"`,
		},
		{
			text:    "日本語の長い文字列",
			pattern: `"語"`,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			for _, expr := range []string{"s.indexOf(p, 1)", "s.lastIndexOf(p)", "s.includes(p, 1)"} {
				b.Run(form+"/"+tc.text+"/"+expr, func(b *testing.B) {
					var vm ekuma.VM
					receiver := fmt.Sprintf("%q", tc.text)
					if form == "Created" {
						receiver = fmt.Sprintf("%q.slice(0, -1)", tc.text+"!")
					}
					src := fmt.Sprintf(`var s = %s, p = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = %s; return result; }`, receiver, tc.pattern, expr)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringConcat measures one binary string concatenation.
func BenchmarkStringConcat(b *testing.B) {
	for _, tc := range []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "ASCII",
			left:  `"foo"`,
			right: `"bar"`,
		},
		{
			name:  "OneByte",
			left:  `"tag"`,
			right: `":"`,
		},
		{
			name:  "Unicode",
			left:  `"é"`,
			right: `"日"`,
		},
		{
			name:  "JoinPair",
			left:  `"\ud83d"`,
			right: `"\ude00"`,
		},
		{
			name:  "JoinFits",
			left:  `"ab\ud83d"`,
			right: `"\ude00cd"`,
		},
		{
			name:  "Overflow",
			left:  `"abcde"`,
			right: `"fghi"`,
		},
		{
			name:  "Long",
			left:  `"abcdefghijklmnop"`,
			right: `"qrstuvwxyz"`,
		},
	} {
		for _, form := range []string{"Literal", "Created", "Mixed"} {
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				left, right := tc.left, tc.right
				if form != "Literal" {
					left = "(" + left + " + '!').slice(0, -1)"
				}
				if form == "Created" {
					right = "(" + right + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var a = %s, b = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = a + b; return result; }`, left, right)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringCompare measures one relational string comparison.
func BenchmarkStringCompare(b *testing.B) {
	for _, tc := range []struct {
		name  string
		left  string
		right string
	}{
		{
			name:  "ASCII",
			left:  `"abc"`,
			right: `"abd"`,
		},
		{
			name:  "CommonPrefix",
			left:  `"abcdefgx"`,
			right: `"abcdefgy"`,
		},
		{
			name:  "Equal",
			left:  `"abcdefgh"`,
			right: `"abcdefgh"`,
		},
		{
			name:  "NULPrefix",
			left:  `"abc"`,
			right: `"abc\u0000"`,
		},
		{
			name:  "Unicode",
			left:  `"é日"`,
			right: `"é月"`,
		},
		{
			name:  "SurrogateOrder",
			left:  `"\ue000"`,
			right: `"😀"`,
		},
		{
			name:  "SharedSurrogate",
			left:  `"\ud83d\ue000"`,
			right: `"😀"`,
		},
		{
			name:  "Long",
			left:  `"abcdefghijklmnopqrstuvwxya"`,
			right: `"abcdefghijklmnopqrstuvwxyb"`,
		},
		{
			name:  "ShortLong",
			left:  `"abcdefg"`,
			right: `"abcdefghijklmnopqrstuvwxyz"`,
		},
	} {
		for _, form := range []string{"Literal", "Created", "Mixed"} {
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				left, right := tc.left, tc.right
				if form != "Literal" {
					left = "(" + left + " + '!').slice(0, -1)"
				}
				if form == "Created" {
					right = "(" + right + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var a = %s, b = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = a < b; return result; }`, left, right)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringWellFormed measures well-formedness checking and repair.
func BenchmarkStringWellFormed(b *testing.B) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{
			name: "ASCII",
			text: `"abcdefgh"`,
		},
		{
			name: "Unicode",
			text: `"\ud7ff日"`,
		},
		{
			name: "Paired",
			text: `"😀😀"`,
		},
		{
			name: "LoneAtEnd",
			text: `"abcde\ud800"`,
		},
		{
			name: "TwoLone",
			text: `"\udfff\udbff"`,
		},
		{
			name: "LongValid",
			text: `"日本語の長い文字列"`,
		},
		{
			name: "LongInvalid",
			text: `"日本語\ud800の長い文字列"`,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			for _, method := range []string{"isWellFormed", "toWellFormed"} {
				b.Run(form+"/"+tc.name+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					receiver := tc.text
					if form == "Created" {
						receiver = "(" + receiver + " + '!').slice(0, -1)"
					}
					src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(); return result; }`, receiver, method)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringCodePointAt measures one code point lookup.
func BenchmarkStringCodePointAt(b *testing.B) {
	for _, tc := range []struct {
		name  string
		text  string
		index int
	}{
		{
			name:  "ASCII",
			text:  `"abcdefgh"`,
			index: 3,
		},
		{
			name:  "BMP",
			text:  `"é日"`,
			index: 1,
		},
		{
			name:  "FirstPair",
			text:  `"😀😀"`,
			index: 0,
		},
		{
			name:  "LaterPair",
			text:  `"😀😀"`,
			index: 2,
		},
		{
			name:  "LowSurrogate",
			text:  `"😀😀"`,
			index: 1,
		},
		{
			name:  "LoneSurrogate",
			text:  `"\ud800x"`,
			index: 0,
		},
		{
			name:  "LongASCII",
			text:  `"abcdefghijklmnopqrstuvwxyz"`,
			index: 10,
		},
		{
			name:  "LongPair",
			text:  `"日本語😀の長い文字列abcdefgh"`,
			index: 3,
		},
		{
			name:  "LongMixedASCII",
			text:  `"日本語😀の長い文字列abcdefgh"`,
			index: 12,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				receiver := tc.text
				if form == "Created" {
					receiver = "(" + receiver + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.codePointAt(%d); return result; }`, receiver, tc.index)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringASCIICase measures ASCII case conversion and Unicode fallbacks.
func BenchmarkStringASCIICase(b *testing.B) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{
			name: "Mixed",
			text: `"aBcDeFgH"`,
		},
		{
			name: "Upper",
			text: `"ABCDEFGH"`,
		},
		{
			name: "Lower",
			text: `"abcdefgh"`,
		},
		{
			name: "Symbols",
			text: `"aZ09!_"`,
		},
		{
			name: "NUL",
			text: `"a\u0000Z"`,
		},
		{
			name: "Unicode",
			text: `"Straße"`,
		},
		{
			name: "FinalSigma",
			text: `"ΟΣ"`,
		},
		{
			name: "LoneSurrogate",
			text: `"a\ud800"`,
		},
		{
			name: "LongASCII",
			text: `"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"`,
		},
		{
			name: "LongLower",
			text: `"abcdefghijklmnopqrstuvwxyz"`,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			for _, method := range []string{"toUpperCase", "toLowerCase"} {
				b.Run(form+"/"+tc.name+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					receiver := tc.text
					if form == "Created" {
						receiver = "(" + receiver + " + '!').slice(0, -1)"
					}
					src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(); return result; }`, receiver, method)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringTrim measures trimming and its Unicode and long-string fallbacks.
func BenchmarkStringTrim(b *testing.B) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{
			name: "Whitespace",
			text: `" \tfoo \r\n"`,
		},
		{
			name: "Unchanged",
			text: `"abcdefgh"`,
		},
		{
			name: "AllWhitespace",
			text: `"\t\n\v\f\r "`,
		},
		{
			name: "NUL",
			text: `" \u0000 "`,
		},
		{
			name: "UnicodeWhitespace",
			text: `"\u00a0日　"`,
		},
		{
			name: "UnicodeUnchanged",
			text: `"é日"`,
		},
		{
			name: "LoneSurrogate",
			text: `" \ud800 "`,
		},
		{
			name: "LongWhitespace",
			text: `"  abcdefghijklmnopqrstuvwxyz  "`,
		},
		{
			name: "LongUnchanged",
			text: `"abcdefghijklmnopqrstuvwxyz"`,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			for _, method := range []string{"trim", "trimStart", "trimEnd"} {
				b.Run(form+"/"+tc.name+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					receiver := tc.text
					if form == "Created" {
						receiver = "(" + receiver + " + '!').slice(0, -1)"
					}
					src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(); return result; }`, receiver, method)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkStringRepeat measures trivial counts, packed results and fallback repetition.
func BenchmarkStringRepeat(b *testing.B) {
	for _, tc := range []struct {
		name  string
		text  string
		count int
	}{
		{
			name:  "Zero",
			text:  `"abc"`,
			count: 0,
		},
		{
			name:  "One",
			text:  `"abc"`,
			count: 1,
		},
		{
			name:  "ASCII",
			text:  `"ab"`,
			count: 4,
		},
		{
			name:  "OneByte",
			text:  `"x"`,
			count: 8,
		},
		{
			name:  "Unicode",
			text:  `"é"`,
			count: 3,
		},
		{
			name:  "Supplementary",
			text:  `"😀"`,
			count: 2,
		},
		{
			name:  "SelfJoin",
			text:  `"\ude00\ud83d"`,
			count: 2,
		},
		{
			name:  "Overflow",
			text:  `"abc"`,
			count: 3,
		},
		{
			name:  "LongResult",
			text:  `"abc"`,
			count: 16,
		},
		{
			name:  "LongOne",
			text:  `"abcdefghijklmnopqrstuvwxyz"`,
			count: 1,
		},
		{
			name:  "LongInput",
			text:  `"abcdefghijklmnopqrstuvwxyz"`,
			count: 2,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				receiver := tc.text
				if form == "Created" {
					receiver = "(" + receiver + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.repeat(%d); return result; }`, receiver, tc.count)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringPad measures short padding, mixed representations and fallback results.
func BenchmarkStringPad(b *testing.B) {
	for _, tc := range []struct {
		name   string
		text   string
		fill   string
		target int
	}{
		{
			name:   "ASCII",
			text:   `"ab"`,
			fill:   `"xy"`,
			target: 8,
		},
		{
			name:   "Unicode",
			text:   `"é"`,
			fill:   `"日"`,
			target: 3,
		},
		{
			name:   "TruncatedPair",
			text:   `"x"`,
			fill:   `"😀"`,
			target: 2,
		},
		{
			name:   "BoundaryJoin",
			text:   `"\ude00"`,
			fill:   `"\ud83d"`,
			target: 2,
		},
		{
			name:   "LongFill",
			text:   `"ab"`,
			fill:   `"abcdefghijklmnopqrstuvwxyz"`,
			target: 8,
		},
		{
			name:   "Default",
			text:   `"ab"`,
			fill:   `undefined`,
			target: 8,
		},
		{
			name:   "Empty",
			text:   `"ab"`,
			fill:   `""`,
			target: 8,
		},
		{
			name:   "NoOp",
			text:   `"abcdefgh"`,
			fill:   `"x"`,
			target: 4,
		},
		{
			name:   "LongResult",
			text:   `"ab"`,
			fill:   `"xy"`,
			target: 64,
		},
		{
			name:   "LongInput",
			text:   `"abcdefghijklmnopqrstuvwxyz"`,
			fill:   `"xy"`,
			target: 32,
		},
	} {
		for _, form := range []string{"Literal", "Created", "Mixed"} {
			for _, method := range []string{"padStart", "padEnd"} {
				b.Run(form+"/"+tc.name+"/"+method, func(b *testing.B) {
					var vm ekuma.VM
					receiver, fill := tc.text, tc.fill
					if form != "Literal" {
						receiver = "(" + receiver + " + '!').slice(0, -1)"
					}
					if form == "Created" && fill != "undefined" {
						fill = "(" + fill + " + '!').slice(0, -1)"
					}
					src := fmt.Sprintf(`var s = %s, f = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = s.%s(%d, f); return result; }`, receiver, fill, method, tc.target)
					if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
						b.Fatal(err)
					}
				})
			}
		}
	}
}

// BenchmarkShortStringCandidates measures remaining materialization candidates and controls.
func BenchmarkShortStringCandidates(b *testing.B) {
	for _, tc := range []struct {
		name, text, fill, expression, setup string
	}{
		{
			name:       "Index",
			text:       `"abcabc"`,
			fill:       `"bc"`,
			expression: `s.indexOf(f)`,
			setup:      ``,
		},
		{
			name:       "LastIndex",
			text:       `"abcabc"`,
			fill:       `"bc"`,
			expression: `s.lastIndexOf(f)`,
			setup:      ``,
		},
		{
			name:       "Includes",
			text:       `"abcabc"`,
			fill:       `"bc"`,
			expression: `s.includes(f)`,
			setup:      ``,
		},
		{
			name:       "LongIndex",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"xyz"`,
			expression: `s.indexOf(f)`,
			setup:      ``,
		},
		{
			name:       "Split",
			text:       `"a,b,c"`,
			fill:       `","`,
			expression: `s.split(f)`,
			setup:      ``,
		},
		{
			name:       "SplitUnits",
			text:       `"ab😀"`,
			fill:       `""`,
			expression: `s.split(f)`,
			setup:      ``,
		},
		{
			name:       "LongSplit",
			text:       `"ab,cd,ef,gh,ij,kl,mn,op"`,
			fill:       `","`,
			expression: `s.split(f)`,
			setup:      ``,
		},
		{
			name:       "Replace",
			text:       `"abcabc"`,
			fill:       `"bc"`,
			expression: `s.replace(f, "x")`,
			setup:      ``,
		},
		{
			name:       "ReplaceAll",
			text:       `"abcabc"`,
			fill:       `"bc"`,
			expression: `s.replaceAll(f, "x")`,
			setup:      ``,
		},
		{
			name:       "ReplaceMiss",
			text:       `"abcabc"`,
			fill:       `"z"`,
			expression: `s.replace(f, "x")`,
			setup:      ``,
		},
		{
			name:       "LongReplace",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"abc"`,
			expression: `s.replace(f, "x")`,
			setup:      ``,
		},
		{
			name:       "Normalize",
			text:       `"abcdef"`,
			fill:       `"NFC"`,
			expression: `s.normalize(f)`,
			setup:      ``,
		},
		{
			name:       "NormalizeUnicode",
			text:       `"é日"`,
			fill:       `"NFD"`,
			expression: `s.normalize(f)`,
			setup:      ``,
		},
		{
			name:       "LongNormalize",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"NFC"`,
			expression: `s.normalize(f)`,
			setup:      ``,
		},
		{
			name:       "LocaleUpper",
			text:       `"abc"`,
			fill:       `""`,
			expression: `s.toLocaleUpperCase()`,
			setup:      ``,
		},
		{
			name:       "LocaleLower",
			text:       `"ABC"`,
			fill:       `""`,
			expression: `s.toLocaleLowerCase()`,
			setup:      ``,
		},
		{
			name:       "LocaleCompare",
			text:       `"abc"`,
			fill:       `"abd"`,
			expression: `s.localeCompare(f)`,
			setup:      ``,
		},
		{
			name:       "Concat",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: `s.concat(f, "ef")`,
			setup:      ``,
		},
		{
			name:       "LongConcat",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"xy"`,
			expression: `s.concat(f, "ef")`,
			setup:      ``,
		},
		{
			name:       "MultiConcat",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: `s + f + s`,
			setup:      ``,
		},
		{
			name:       "MultiJoin",
			text:       `"\ud83d"`,
			fill:       `"\ude00"`,
			expression: `s + f + "ab"`,
			setup:      ``,
		},
		{
			name:       "FromCharCode",
			text:       `"abc"`,
			fill:       `""`,
			expression: `String.fromCharCode(97, 98, 99)`,
			setup:      ``,
		},
		{
			name:       "FromCodePoint",
			text:       `"abc"`,
			fill:       `""`,
			expression: `String.fromCodePoint(0x1f600)`,
			setup:      ``,
		},
		{
			name:       "ArrayJoin",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: `a.join("")`,
			setup:      `var a = [s, f, s];`,
		},
		{
			name:       "ToNumber",
			text:       `"123456"`,
			fill:       `""`,
			expression: `+s`,
			setup:      ``,
		},
		{
			name:       "NumberString",
			text:       `"abc"`,
			fill:       `""`,
			expression: `String(i % 100000)`,
			setup:      ``,
		},
		{
			name:       "JSON",
			text:       `"ab"`,
			fill:       `""`,
			expression: `JSON.stringify(s)`,
			setup:      ``,
		},
		{
			name:       "MixedEquality",
			text:       `"abcdef"`,
			fill:       `"abcdef"`,
			expression: `s === "abcdef"`,
			setup:      ``,
		},
		{
			name:       "StartsWith",
			text:       `"abcdef"`,
			fill:       `"abc"`,
			expression: `s.startsWith(f)`,
			setup:      ``,
		},
		{
			name:       "Sort",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: `a.toSorted()`,
			setup:      `var a = [f, s, "a", f, s];`,
		},
		{
			name:       "Iterator",
			text:       `"ab😀"`,
			fill:       `""`,
			expression: `Array.from(s)`,
			setup:      ``,
		},
		{
			name:       "Raw",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: `String.raw(a, f)`,
			setup:      `var a = { raw: [s, s] };`,
		},
		{
			name:       "ConcatJoin",
			text:       `"\ud83d"`,
			fill:       `"\ude00"`,
			expression: `s.concat(f, s, f)`,
			setup:      ``,
		},
		{
			name:       "FromCharCodeLong",
			text:       `"abc"`,
			fill:       `""`,
			expression: `String.fromCharCode(65,66,67,68,69,70,71,72,73,74,75,76)`,
			setup:      ``,
		},
		{
			name:       "ArrayJoinLong",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"cd"`,
			expression: `a.join(",")`,
			setup:      `var a = [s, f, s];`,
		},
		{
			name:       "NumberMethod",
			text:       `"abc"`,
			fill:       `""`,
			expression: `(i % 100000).toString()`,
			setup:      ``,
		},
		{
			name:       "NumberFixed",
			text:       `"abc"`,
			fill:       `""`,
			expression: `(i % 100000).toFixed(2)`,
			setup:      ``,
		},
		{
			name:       "LongNumberString",
			text:       `"abc"`,
			fill:       `""`,
			expression: `String(1234567890123456 + i)`,
			setup:      ``,
		},
		{
			name:       "PropertyRead",
			text:       `"abc"`,
			fill:       `""`,
			expression: `o[s]`,
			setup:      `var o = {abc: 1};`,
		},
		{
			name:       "MapReadSmall",
			text:       `"abc"`,
			fill:       `""`,
			expression: `m.get(s)`,
			setup:      `var m = new Map([["abc", 1], ["def", 2]]);`,
		},
		{
			name:       "MapReadIndexed",
			text:       `"abc"`,
			fill:       `""`,
			expression: `m.get(s)`,
			setup:      `var m = new Map([["abc", 1]]); for (var j = 0; j < 40; j++) m.set("k"+j, j);`,
		},
		{
			name:       "PropertyWrite",
			text:       `"abc"`,
			fill:       `""`,
			expression: `o[s] = i`,
			setup:      `var o = {abc: 1};`,
		},
		{
			name:       "Template",
			text:       `"ab"`,
			fill:       `"cd"`,
			expression: "`${s}${f}${s}`",
		},
		{
			name:       "TemplateJoin",
			text:       `"\ud83d"`,
			fill:       `"\ude00"`,
			expression: "`${s}${f}ab`",
		},
		{
			name:       "LongTemplate",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `"xy"`,
			expression: "`${s}${f}${s}`",
		},
		{
			name:       "HTMLBold",
			text:       `"x"`,
			fill:       `""`,
			expression: "s.bold()",
		},
		{
			name:       "HTMLLong",
			text:       `"abcdefghijklmnopqrstuvwxyz"`,
			fill:       `""`,
			expression: "s.bold()",
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				receiver, fill := tc.text, tc.fill
				if form == "Created" {
					receiver = "(" + receiver + " + '!').slice(0, -1)"
					fill = "(" + fill + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var s = %s, f = %s; %s function run(n) { var result; for (var i = 0; i < n; i++) result = %s; return result; }`, receiver, fill, tc.setup, tc.expression)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkStringToNumber measures decimal conversion and general-parser controls.
func BenchmarkStringToNumber(b *testing.B) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{
			name: "Digits",
			text: `"123456"`,
		},
		{
			name: "EightDigits",
			text: `"99999999"`,
		},
		{
			name: "OneDigit",
			text: `"7"`,
		},
		{
			name: "LeadingZeros",
			text: `"00001234"`,
		},
		{
			name: "Signed",
			text: `"-123456"`,
		},
		{
			name: "Fraction",
			text: `"123.456"`,
		},
		{
			name: "Exponent",
			text: `"12.34e2"`,
		},
		{
			name: "Whitespace",
			text: `" 123456 "`,
		},
		{
			name: "Empty",
			text: `""`,
		},
		{
			name: "InvalidSuffix",
			text: `"1234567x"`,
		},
		{
			name: "Radix",
			text: `"0x1234"`,
		},
		{
			name: "LongDigits",
			text: `"123456789012345"`,
		},
		{
			name: "Number",
			text: `123456`,
		},
	} {
		for _, form := range []string{"Literal", "Created"} {
			if tc.name == "Number" && form == "Created" {
				continue
			}
			b.Run(form+"/"+tc.name, func(b *testing.B) {
				var vm ekuma.VM
				receiver := tc.text
				if form == "Created" {
					receiver = "(" + receiver + " + '!').slice(0, -1)"
				}
				src := fmt.Sprintf(`var s = %s; function run(n) { var result; for (var i = 0; i < n; i++) result = +s; return result; }`, receiver)
				if _, err := runScript(b.Context(), &vm, "setup", src); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(b.Context(), &vm, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}

// BenchmarkObjectCopy copies the properties of an object literal in a loop with Object.assign
// or a spread, or lists its keys. One iteration is one operation.
func BenchmarkObjectCopy(b *testing.B) {
	const js = `
		function source(n) {
			const o = {};
			for (let i = 0; i < n; i++) o["k" + i] = i;
			return o;
		}
		function run(op, n, count) {
			const src = source(n);
			let r;
			switch (op) {
			case "assign":
				for (let i = 0; i < count; i++) r = Object.assign({}, src);
				break;
			case "assignOnto":
				for (let i = 0; i < count; i++) r = Object.assign({a: 1}, src);
				break;
			case "spread":
				for (let i = 0; i < count; i++) r = {...src};
				break;
			case "keys":
				for (let i = 0; i < count; i++) r = Object.keys(src);
				break;
			}
			return r;
		}`
	for _, op := range []string{"assign", "assignOnto", "spread", "keys"} {
		for _, n := range []int{8, 64} {
			b.Run(fmt.Sprintf("%s/%d", op, n), func(b *testing.B) {
				ctx := b.Context()
				var vm ekuma.VM
				if _, err := runScript(ctx, &vm, "copy.js", js); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				if _, err := runScript(ctx, &vm, "run", fmt.Sprintf("run(%q, %d, %d)", op, n, b.N)); err != nil {
					b.Fatal(err)
				}
			})
		}
	}
}
