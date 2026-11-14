// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// TestScripts runs scripts and compares the string form of their last value.
func TestScripts(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "arithmetic",
			src:  `1 + 2 * 3 - 4 / 2`,
			want: "5",
		},
		{
			name: "string concatenation",
			src:  `"a" + 1 + 2 + "b" + (1 + 2)`,
			want: "a12b3",
		},
		{
			name: "closure counter",
			src: `function counter() { var n = 0; return function() { n++; return n; }; }
				var c = counter(); c(); c(); c()`,
			want: "3",
		},
		{
			name: "recursion",
			src:  `function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2); } fib(20)`,
			want: "6765",
		},
		{
			name: "prototype method through alias chain",
			src: `function A() { this.x = 1; }
				A.prototype.get = function(d) { return this.x + d; };
				var alias = A.prototype.get;
				A.prototype.get = function() { return alias.apply(this, arguments) * 2; };
				new A().get(4)`,
			want: "10",
		},
		{
			name: "class with fields, accessors and inheritance",
			src: `class P { #secret = 7; base = 1; static count = 0;
					constructor(x) { this.x = x; P.count++; }
					get secret() { return this.#secret; }
					sum() { return this.x + this.base; } }
				class Q extends P { extra = 10; constructor(x) { super(x); }
					sum() { return super.sum() + this.extra + this.secret; } }
				new Q(5).sum() + ":" + P.count`,
			want: "23:1",
		},
		{
			name: "super property in a method that uses this only through super",
			src: `class A { m() { return this; } set x(v) { this.y = v; } }
				class B extends A { m() { return super.m(); } setX() { super.x = 1; } }
				var b = new B(); b.setX(); (b.m() === b) + ":" + b.y`,
			want: "true:1",
		},
		{
			name: "computed super property evaluation order",
			src: `var log = [];
				var p1 = { get p() { log.push("get1"); return 1; }, set p(v) { log.push("set1:" + v); } };
				var p2 = { get p() { log.push("get2"); return 2; }, set p(v) { log.push("set2:" + v); } };
				var obj = { __proto__: p1,
					read() { super[key]; },
					assign() { super[key] = rhs(); },
					compound() { super[key] += rhs(); } };
				var key = { toString() { log.push("key"); Object.setPrototypeOf(obj, p2); return "p"; } };
				function rhs() { log.push("rhs"); Object.setPrototypeOf(obj, p2); return 10; }
				for (var m of ["read", "assign", "compound"]) { Object.setPrototypeOf(obj, p1); obj[m](); }
				log.join()`,
			want: "key,get1,rhs,key,set1:10,key,get1,rhs,set1:11",
		},
		{
			name: "delete of a super property before super()",
			src: `var log = [];
				class D extends Object { constructor() { delete super[log.push("key")]; } }
				try { new D(); } catch (e) { log.push(e.name); }
				log.join()`,
			want: "ReferenceError",
		},
		{
			name: "let per iteration and closures",
			src:  `var fs = []; for (let i = 0; i < 3; i++) fs.push(() => i); fs.map(f => f()).join()`,
			want: "0,1,2",
		},
		{
			name: "temporal dead zone",
			src:  `var r; try { x; let x = 1; } catch (e) { r = e instanceof ReferenceError; } r`,
			want: "true",
		},
		{
			name: "const assignment",
			src:  `var r; const k = 1; try { k = 2; } catch (e) { r = e.constructor.name; } r + k`,
			want: "TypeError1",
		},
		{
			name: "destructuring",
			src: `var {a, b: [c, d = 4, ...rest], ...others} = {a: 1, b: [2, undefined, 5, 6], e: 7, f: 8};
				[a, c, d, rest, Object.keys(others)].join("|")`,
			want: "1|2|4|5,6|e,f",
		},
		{
			name: "object destructuring assignment evaluates property targets first",
			src: `var log = [], o = {};
				var src = { get a() { log.push("get a"); }, get b() { log.push("get b"); return 2; } };
				({ a: (log.push("target a"), o).x = (log.push("default"), 1), ...(log.push("target rest"), o).r } = src);
				log.join() + "|" + o.x + o.r.b`,
			want: "target a,get a,default,target rest,get b|12",
		},
		{
			name: "spread and rest",
			src:  `function f(a, ...r) { return a + r.length; } f(...[1, 2, 3], ...[4])`,
			want: "4",
		},
		{
			name: "try finally with return and break",
			src: `var log = [];
				function f() { try { return "r"; } finally { log.push("f1"); } }
				out: for (var i = 0; i < 3; i++) { try { if (i == 1) break out; } finally { log.push("i" + i); } }
				f() + ":" + log.join()`,
			want: "r:i0,i1,f1",
		},
		{
			name: "exception through native callback",
			src:  `var r; try { [1, 2].forEach(function(x) { if (x == 2) throw new Error("boom"); }); } catch (e) { r = e.message; } r`,
			want: "boom",
		},
		{
			name: "generator",
			src: `function* g(n) { for (var i = 0; i < n; i++) { var x = yield i; if (x) i += x; } return "done"; }
				var it = g(5), out = []; out.push(it.next().value); out.push(it.next(2).value); out.push(it.next().value);
				out.push(it.next().value); out.join() + [...g(3)].join("")`,
			want: "0,3,4,done012",
		},
		{
			name: "generator return runs finally",
			src: `var log = []; function* g() { try { yield 1; yield 2; } finally { log.push("cleanup"); } }
				for (var v of g()) { log.push(v); break; } log.join()`,
			want: "1,cleanup",
		},
		{
			name: "yield delegation",
			src:  `function* a() { yield 1; return 2; } function* b() { var r = yield* a(); yield r; } [...b()].join()`,
			want: "1,2",
		},
		{
			name: "for in and for of",
			src: `var o = {x: 1, y: 2}, ks = []; for (var k in o) ks.push(k);
				var s = 0; for (const v of [1, 2, 3]) s += v; ks.join() + s`,
			want: "x,y6",
		},
		{
			name: "switch with fallthrough",
			src:  `function f(x) { var r = ""; switch (x) { case 1: r += "a"; case 2: r += "b"; break; default: r += "d"; } return r; } f(1) + f(2) + f(3)`,
			want: "abbd",
		},
		{
			name: "getters, setters and defineProperty",
			src: `var o = { _v: 1, get v() { return this._v; }, set v(x) { this._v = x * 2; } };
				o.v = 5; Object.defineProperty(o, "w", { value: 3, enumerable: false });
				o.v + ":" + o.w + ":" + Object.keys(o).join()`,
			want: "10:3:_v,v",
		},
		{
			name: "optional chaining and nullish",
			src:  `var o = { a: { b: 0 } }; [o?.a?.b ?? "x", o.z?.b.c ?? "y", o.f?.()].join()`,
			want: "0,y,",
		},
		{
			name: "template literal and tagged template",
			src:  "function tag(s, ...v) { return s.raw.join('-') + v.join(''); } `a${1 + 1}b` + tag`x${1}y${2}z`",
			want: "a2bx-y-z12",
		},
		{
			name: "string as UTF-16",
			src:  `var s = "a\u{1F600}b"; [s.length, s.charCodeAt(1), s.charCodeAt(2), s[3], [...s].length, s.slice(1, 2).length].join()`,
			want: "4,55357,56832,b,3,1",
		},
		{
			name: "lone surrogates join into a pair",
			src:  `("\uD83D" + "\uDE00") === "\u{1F600}"`,
			want: "true",
		},
		{
			name: "array methods",
			src: `var a = [5, 1, 4]; a.push(3); a.sort(); a.splice(1, 1, "x", "y");
				[a.join(), a.indexOf("y"), a.slice(-2).join(), a.reduce((s, v) => s + v, ""), a.length].join("|")`,
			want: "1,x,y,4,5|2|4,5|1xy45|5",
		},
		{
			name: "sparse array",
			src:  `var a = []; a[5] = 1; a[100000] = 2; [a.length, 3 in a, 5 in a, a[100000], Object.keys(a).join()].join("|")`,
			want: "100001|false|true|2|5,100000",
		},
		{
			name: "sparse array walk sees changes made by the callback",
			src: `var a = []; a[10] = 1; a[100000] = 2; var seen = [], back = [];
				a.forEach(function (v, i) { seen.push(i); if (i === 10) { a[50000] = 3; Array.prototype[70000] = 4; } });
				delete Array.prototype[70000];
				a.reduceRight(function (acc, v, i) { back.push(i); if (i === 100000) a[20] = 5; return acc; }, 0);
				[seen.join(), back.join(), a.lastIndexOf(1)].join("|")`,
			want: "10,50000,70000,100000|100000,50000,20,10|10",
		},
		{
			name: "walk of a sparse array with many indices sees indices added by the callback",
			src: `var a = []; for (var i = 0; i < 200; i++) a[i * 1000 + 500] = i; var seen = [];
				a.forEach(function (v, i) { if (i === 500) { a.x = 0; a[1700] = -1; a[300] = -2; } if (i < 3000) seen.push(i); });
				seen.join()`,
			want: "500,1500,1700,2500",
		},
		{
			name: "named function expression and arguments",
			src:  `var f = function fact(n) { return n <= 1 ? 1 : n * fact(n - 1); }; (function() { return arguments.length + f(arguments[0]); })(4, 5)`,
			want: "26",
		},
		{
			name: "labelled continue",
			src:  `var r = ""; a: for (var i = 0; i < 3; i++) { for (var j = 0; j < 3; j++) { if (j == 1) continue a; r += i + "" + j; } } r`,
			want: "001020",
		},
		{
			name: "symbols as keys",
			src:  `var s = Symbol("k"), o = { [s]: 1, x: 2 }; [o[s], Object.keys(o).length, typeof s, s.toString(), Object.getOwnPropertySymbols(o).length].join()`,
			want: "1,1,symbol,Symbol(k),1",
		},
		{
			name: "bigint",
			src:  `(2n ** 64n + 1n).toString === undefined ? "" : String(2n ** 64n + 1n) + typeof 1n`,
			want: "18446744073709551617bigint",
		},
		{
			name: "number formatting",
			src:  `[0.1 + 0.2, 1e21, 1e-7, -0, 123456789012345680000, (255).toString(16), (1.005).toFixed(2), 5e-324].join("|")`,
			want: "0.30000000000000004|1e+21|1e-7|0|123456789012345680000|ff|1.00|5e-324",
		},
		{
			name: "this in sloppy and strict functions",
			src:  `function s() { return this === globalThis; } function t() { "use strict"; return this === undefined; } s() + ":" + t()`,
			want: "true:true",
		},
		{
			name: "global let and var",
			src:  `let gl = 1; var gv = 2; function rd() { return gl + gv + (typeof nope); } rd() + ":" + ("gv" in globalThis) + ("gl" in globalThis)`,
			want: "3undefined:truefalse",
		},
		{
			name: "Number.parseFloat and Number.parseInt are the globals",
			src:  `(Number.parseFloat === parseFloat) + ":" + (Number.parseInt === parseInt) + ":" + Number.parseInt("ff", 16)`,
			want: "true:true:255",
		},
		{
			name: "function own keys after an assignment",
			src:  `function f() {} f.x = 1; Object.getOwnPropertyNames(f).join()`,
			want: "length,name,arguments,caller,prototype,x",
		},
		{
			name: "function own keys after a definition",
			src:  `function f() {} Object.defineProperty(f, "x", { value: 1 }); Object.getOwnPropertyNames(f).join()`,
			want: "length,name,arguments,caller,prototype,x",
		},
		{
			name: "function own keys after deleting an initial property",
			src:  `function f() {} delete f.name; f.x = 1; Object.getOwnPropertyNames(f).join()`,
			want: "length,arguments,caller,prototype,x",
		},
		{
			name: "arrow function own keys",
			src:  `var f = () => {}; f.x = 1; Object.getOwnPropertyNames(f).join()`,
			want: "length,name,x",
		},
		{
			name: "function own keys after a store cached for an object with the same prototype",
			src: `function set(o) { o.x = 1; }
				function f() {}
				function* g() {}
				var r = [];
				for (var fn of [f, g]) {
					var proto = Object.getPrototypeOf(fn);
					set(Object.create(proto)); set(Object.create(proto)); set(fn);
					r.push(Object.getOwnPropertyNames(fn).join());
				}
				r.join("|")`,
			want: "length,name,arguments,caller,prototype,x|length,name,prototype,x",
		},
		{
			name: "function own keys after a store cached for an object with the new prototype",
			src: `function set(o) { o.x = 1; }
				function f() {}
				Object.setPrototypeOf(f, null);
				set(Object.create(null)); set(Object.create(null)); set(f);
				Object.getOwnPropertyNames(f).join()`,
			want: "length,name,arguments,caller,prototype,x",
		},
		{
			name: "slice with an argument that shrinks the array",
			src: `var a = [1, 2, 3, 4, 5, 6, 7, 8];
				var r = a.slice({ valueOf() { a.length = 0; a.unshift(1); return 7; } }, { valueOf() { return 0; } });
				[r.length, a.join()].join("|")`,
			want: "0|1",
		},
		{
			name: "join with a separator that shrinks the array",
			src:  `var a = [1, 2, 3]; a.join({ toString() { a.length = 1; return "-"; } })`,
			want: "1--",
		},
		{
			name: "flat with a depth that grows the array",
			src:  `var a = [1, [2]]; a.flat({ valueOf() { a.push(3); return 1; } }).join()`,
			want: "1,2",
		},
		{
			name: "concat into a species array keeps its length until the end",
			src: `var created, seen;
				class C extends Array { static get [Symbol.species]() { return function() { return created = new Array(5); }; } }
				var c = new C(); c.push(1, 2);
				var r = c.concat({ get [Symbol.isConcatSpreadable]() { seen = created.length; return false; } });
				[seen, r.length].join()`,
			want: "5,3",
		},
		{
			name: "own accessors of objects that share a shape",
			src: `function make(d) { return { get x() { return d; }, set x(v) { d += v; } }; }
				var r = [];
				for (var d = 0; d < 2; d++) { var o = make(d); o.x = 10; r.push(o.x); }
				Object.defineProperty(o, "x", { get() { return "new"; } });
				r.push(o.x); r.join()`,
			want: "10,11,new",
		},
		{
			name: "for-of calls a throwing return method once when left normally",
			src: `var n = 0;
				var it = { [Symbol.iterator]() { return { next() { return { done: false }; }, return() { n++; throw 1; } }; } };
				try { for (var x of it) break; } catch (e) {}
				outer: for (var i = 0; i < 2; i++) { try { for (var x of it) continue outer; } catch (e) {} }
				n`,
			want: "3",
		},
		{
			name: "for-of reports a throwing return method when a generator returns",
			src: `var it = { [Symbol.iterator]() { return { next() { return { done: false }; }, return() { throw "closed"; } }; } };
				function* g() { for (var x of it) yield 1; }
				var r = g(); r.next();
				var e; try { r.return(); } catch (x) { e = x; } e`,
			want: "closed",
		},
		{
			name: "store to length after an array prototype's length is frozen",
			src: `var a = [1]; function G() {} G.prototype = a;
				function set(o) { o.length = 5; }
				set(new G());
				Object.defineProperty(a, "length", { writable: false });
				var g = new G(); set(g);
				Object.getOwnPropertyNames(g).length + "," + g.length`,
			want: "0,1",
		},
		{
			name: "arguments passed on through super",
			src: `class A { constructor(x) { this.x = x; } m(y) { return this.x + y; } }
				class B extends A { constructor() { super(...arguments); } m() { return super.m.apply(this, arguments); }
					n() { return super.apply(this, arguments); } }
				A.prototype.apply = function(t, a) { return a.length; };
				new B(5).x + "," + new B(5).m(1) + "," + new B(5).n(1, 2)`,
			want: "5,6,2",
		},
		{
			name: "with scope outside a block binding",
			src: `var o = {x: 1, y: 2, this: 3}; var r = [];
				with (o) { let x = 5; { let y = 6; r.push(x, y, this === globalThis); } }
				r.join()`,
			want: "5,6,true",
		},
		{
			name: "with scope resolved before a destructuring step",
			src: `var o = {a: 0, b: 0, c: 0, d: 0}; var a, b, c, d;
				var src = {get p() { delete o.a; return 1; }};
				function* g() { delete o.b; yield 2; delete o.c; yield 3; }
				with (o) { ({p: a} = src); var [b, ...c] = g(); ({...d} = {get q() { delete o.d; }}); }
				[o.a, o.b, o.c, typeof o.d, a, b, c].join()`,
			want: "1,2,3,object,,,",
		},
		{
			name: "mapped arguments outlive the call",
			src: `function f(a) { return [arguments, function() { return a; }, function(v) { a = v; }]; }
				var r = f(1); r[0][0] = 2; var x = r[1](); r[2](3);
				function* g(a) { yield arguments; yield a; }
				var it = g(1); it.next().value[0] = 4;
				x + "," + r[0][0] + "," + it.next().value`,
			want: "2,3,4",
		},
		{
			name: "mapped arguments with a repeated parameter name",
			src:  `function f(a, a) { arguments[0] = 3; arguments[1] = 4; return a; } f(1, 2)`,
			want: "4",
		},
		{
			name: "mapped arguments for a method called apply",
			src: `var o = { apply(t, args) { args[0] = 2; return args.callee === f; } };
				function f(a) { return o.apply(this, arguments) + "," + a; } f(1)`,
			want: "true,2",
		},
		{
			name: "join and includes across runs of holes",
			src: `var r = [];
				var a = []; a.length = 2 ** 32 - 1; a[3] = "x";
				try { a.join("ab".repeat(1000)); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				var s = "\uDC00\uD800";
				r.push([, , "c", , ].join(s) === s + s + "c" + s);
				var c = [{ toString() { Array.prototype[2] = "p"; return "o"; } }, , , ]; c.length = 4;
				r.push(c.join("-")); delete Array.prototype[2];
				var d = []; d[100000] = 1;
				r.push(d.includes(undefined), d.includes(undefined, 100000));
				r.join()`,
			want: "true,true,o--p-,true,false",
		},
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
			ctx := t.Context()
			vm := ekuma.NewVM()
			v, err := vm.RunScript(ctx, tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// toString converts v to a string the way the JavaScript function String does.
func toString(t *testing.T, vm *ekuma.VM, v ekuma.Value) string {
	t.Helper()
	s, err := vm.Global().Call(t.Context(), "String", v)
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	return s.String()
}

// TestAnnexBGlobalSkip covers the block-level functions of sloppy code that get no global
// variable, which test262 does not.
func TestAnnexBGlobalSkip(t *testing.T) {
	tests := []struct {
		name    string
		scripts []string
		want    string
	}{
		{
			name: "lexical declaration of an earlier script",
			scripts: []string{
				`let f = 1;`,
				`{ function f() {} } f`,
			},
			want: "1",
		},
		{
			name: "non-extensible global object",
			scripts: []string{
				`var r = []; Object.preventExtensions(globalThis);`,
				`{ function g() {} r.push(typeof g); }
				r.push(typeof g);
				r.join()`,
			},
			want: "function,undefined",
		},
		{
			name: "eval code",
			scripts: []string{
				`let h = 1; eval("{ function h() {} }"); h`,
			},
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			var v ekuma.Value
			for _, src := range tt.scripts {
				var err error
				v, err = vm.RunScript(t.Context(), tt.name, src)
				if err != nil {
					t.Fatalf("RunScript: %v", err)
				}
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDirectEval covers what test262 does not: the lifetime of the variables that eval code
// captures, and the reuse of compiled eval code.
func TestDirectEval(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "closures made by eval code outlive the caller",
			src: `function make(n) {
					var count = n;
					let step = 10;
					return eval("(function () { count += step; return count; })");
				}
				var a = make(1), b = make(2);
				a(); a(); b();
				[a(), b()].join()`,
			want: "31,22",
		},
		{
			name: "one source at one site sees the current values",
			src: `function f(x) {
					var out = [];
					for (let i = 0; i < 3; i++) {
						out.push(eval("x + i"));
					}
					return out.join();
				}
				[f(1), f(10)].join("/")`,
			want: "1,2,3/10,11,12",
		},
		{
			name: "one source at two sites sees the scope of each",
			src: `var src = "var v = typeof x == 'undefined' ? 'none' : x; v";
				function f(x) {
					let r = [eval(src)];
					{
						let x = "block";
						r.push(eval(src));
					}
					r.push(v);
					return r.join();
				}
				f("param")`,
			want: "param,block,block",
		},
		{
			name: "a lone surrogate in the source joins an escaped surrogate into a pair",
			src: `var r = [
					eval("'\\uD83D" + "\uDE00'") === "\u{1F600}",
					eval("'\uD83D" + "\\uDE00'") === "\u{1F600}",
				];
				try {
					eval("var a\uD83D;");
				} catch (e) {
					r.push(e instanceof SyntaxError);
				}
				r.join()`,
			want: "true,true,true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIterationWithoutResults covers the iteration of built-in iterators and generators
// that does not create iterator results.
func TestIterationWithoutResults(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the next method of another kind of built-in iterator throws",
			src: `var it = new Set([1]).values();
				it.next = new Map().keys().next;
				var r;
				try { for (var x of it) {} r = "no error"; } catch (e) { r = e instanceof TypeError; }
				r`,
			want: "true",
		},
		{
			name: "the result that yield* passes through is read by the loop",
			src: `var reads = [];
				var inner = {
					[Symbol.iterator]() { return this; },
					next() {
						return {
							get done() { reads.push("done"); return false; },
							get value() { reads.push("value"); return 1; },
						};
					},
				};
				function* g() { yield* inner; }
				for (var x of g()) { reads.push(x); break; }
				reads.join()`,
			want: "done,done,value,1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestForAwaitCloseOnReturn checks that closing the iterator of a for-await loop reports its
// error when a generator returns from inside the loop body.
func TestForAwaitCloseOnReturn(t *testing.T) {
	tests := []struct {
		name string
		ret  string
		want string
	}{
		{
			name: "throw",
			ret:  `throw "closed";`,
			want: "closed",
		},
		{
			name: "non-object result",
			ret:  `return 1;`,
			want: "TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			src := `
				var it = { [Symbol.asyncIterator]() { return { next() { return { done: false }; }, return() { ` + tt.ret + ` } }; } };
				async function* g() { for await (var x of it) yield 1; }
				var got = "";
				var r = g();
				r.next().then(() => r.return()).then(() => { got = "fulfilled"; }, e => { got = e instanceof TypeError ? "TypeError" : String(e); });
			`
			if _, err := vm.RunScript(t.Context(), tt.name, src); err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			v, err := vm.RunScript(t.Context(), "got", `got`)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUsing covers the ways out of a scope with using declarations that test262 does not.
func TestUsing(t *testing.T) {
	const res = `var log = [];
		function res(name, error) {
			return { [Symbol.dispose]() { log.push(name); if (error) throw error; } };
		}
		`
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "break, continue and return dispose the resources they leave",
			src: res + `for (var i = 0; i < 2; i++) {
					using a = res("a" + i);
					if (i == 0) continue;
					break;
				}
				(function () { using b = res("b"); { using c = res("c"); return; } })();
				log.join()`,
			want: "a0,a1,c,b",
		},
		{
			name: "an error of disposing on a break is thrown",
			src: res + `var r;
				try { for (;;) { using a = res("a", "boom"); break; } } catch (e) { r = e; }
				log.push(r);
				log.join()`,
			want: "a,boom",
		},
		{
			name: "for-of disposes the binding before it closes the iterator",
			src: res + `var iterable = {
					[Symbol.iterator]() {
						return {
							next() { return { value: res("v"), done: false }; },
							return() { log.push("close"); return {}; },
						};
					},
				};
				for (using v of iterable) break;
				try { for (using v of iterable) throw "body"; } catch (e) { log.push(e); }
				log.join()`,
			want: "v,close,v,close,body",
		},
		{
			name: "return of a generator disposes, and an error of disposing replaces the return",
			src: res + `function* g(error) { using a = res("a", error); yield; }
				var it = g();
				it.next();
				log.push(JSON.stringify(it.return(1)));
				it = g("boom");
				it.next();
				try { it.return(2); } catch (e) { log.push(e); }
				log.push(it.next().done);
				log.join()`,
			want: `a,{"value":1,"done":true},a,boom,true`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAwaitUsingOnReturn checks that the return method of an async generator awaits the
// disposal of the await using declarations it leaves.
func TestAwaitUsingOnReturn(t *testing.T) {
	vm := ekuma.NewVM()
	src := `var log = [];
		async function* g(error) {
			await using a = {
				async [Symbol.asyncDispose]() { await null; log.push("disposed"); if (error) throw error; },
			};
			yield;
		}
		var it = g(), it2 = g("boom");
		it.next().then(() => it.return(1)).then(r => log.push(JSON.stringify(r)));
		it2.next().then(() => it2.return(2)).catch(e => log.push(e));`
	if _, err := vm.RunScript(t.Context(), "await using", src); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	v, err := vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := toString(t, vm, v), `disposed,disposed,{"value":1,"done":true},boom`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestBigIntAsNWithLargeBits covers BigInt.asIntN and BigInt.asUintN with a bit count
// far beyond what a BigInt can hold, which test262 does not pin down.
func TestBigIntAsNWithLargeBits(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a value that already fits is returned",
			src:  `[BigInt.asIntN(2 ** 53 - 1, -5n), BigInt.asUintN(2 ** 53 - 1, 5n)].join()`,
			want: "-5,5",
		},
		{
			name: "a negative value for asUintN is too large",
			src:  `try { BigInt.asUintN(2 ** 53 - 1, -1n); "no error"; } catch (e) { e instanceof RangeError; }`,
			want: "true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestJSON covers the behaviour of JSON.parse and JSON.stringify that depends on the
// representation of strings and on the depth of nesting.
func TestJSON(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a gap whose copies form a pair",
			src:  `JSON.stringify([[1]], null, "\uDC00\uD800").split("\n")[2] === "\uDC00𐀀\uD8001"`,
			want: "true",
		},
		{
			name: "index keys and repeated index keys",
			src: `var o = JSON.parse('{"9999999":1,"1":2,"x":3,"0":4,"9999999":5,"1":6}');
				Object.keys(o).map(function(k) { return k + "=" + o[k]; }).join()`,
			want: "0=4,1=6,9999999=5,x=3",
		},
		{
			name: "deep nesting parses and a deep reviver walk or stringify throws a RangeError",
			src: `var n = 100000, a = JSON.parse("[".repeat(n) + "]".repeat(n)), d = 0;
				for (var x = a; x.length; x = x[0]) d++;
				var r = [d];
				try { JSON.stringify(a); } catch (e) { r.push(e instanceof RangeError); }
				try { JSON.parse("[".repeat(n) + "]".repeat(n), function(k, v) { return v; }); } catch (e) { r.push(e instanceof RangeError); }
				r.join()`,
			want: "99999,true,true",
		},
		{
			name: "stringify called from toJSON",
			src:  `JSON.stringify({a: {toJSON: function() { return JSON.stringify([1, "b"]); }}, c: "d"})`,
			want: `{"a":"[1,\"b\"]","c":"d"}`,
		},
		{
			name: "stringify sees changes that its calls make to later objects",
			src: `var r1 = {v: 0}, r2 = {v: 1};
				var a = JSON.stringify([r1, {toJSON: function() { Object.defineProperty(r2, "v", {get: function() { return "g"; }, enumerable: true}); return 0; }}, r2]);
				var b = JSON.stringify({a: {}, b: {get x() { Object.prototype.toJSON = function() { return "p"; }; return 1; }}, c: {}});
				delete Object.prototype.toJSON;
				a + b`,
			want: `[{"v":0},0,{"v":"g"}]{"a":{},"b":{"x":1},"c":"p"}`,
		},
		{
			name: "parse after a syntax error",
			src: `try { JSON.parse('[1,{"a":[2,'); } catch (e) {}
				JSON.stringify(JSON.parse('[3,{"b":4}]'))`,
			want: `[3,{"b":4}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

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

// TestTypedArray runs scripts about typed arrays whose expected results test262 does not fix.
func TestTypedArray(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			// The specification leaves the byte order of typed arrays to the platform.
			name: "little-endian",
			src: `[
				new Uint8Array(new Uint16Array([0x0102]).buffer),
				new Uint8Array(new Int32Array([-2]).buffer),
				new Uint8Array(new Float32Array([1.5]).buffer),
				new Uint8Array(new Float64Array([-2]).buffer),
				new Uint8Array(new Float16Array([1.5]).buffer),
				new Uint8Array(new BigInt64Array([-0x0102n]).buffer),
			].join(" ")`,
			want: "2,1 254,255,255,255 0,0,192,63 0,0,0,0,0,0,0,192 0,62 254,254,255,255,255,255,255,255",
		},
		{
			// A data block that cannot be created is a RangeError instead of an allocation failure.
			name: "allocation limit",
			src: `var r = [];
				for (var f of [() => new ArrayBuffer(2 ** 33), () => new Float64Array(2 ** 31), () => new ArrayBuffer(0, {maxByteLength: 2 ** 33})]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				}
				r.join()`,
			want: "RangeError,RangeError,RangeError",
		},
		{
			// The length of a typed array keeps following its getter and own properties
			// after repeated reads of the same site.
			name: "length",
			src: `var a = new Uint8Array(4), r = [];
				function len(x) { return x.length; }
				for (var i = 0; i < 10; i++) len(a);
				r.push(len(a));
				Object.defineProperty(Object.getPrototypeOf(Uint8Array.prototype), "length", {get() { return 7; }});
				r.push(len(a));
				Object.defineProperty(a, "length", {value: 9});
				r.push(len(a));
				r.join()`,
			want: "4,7,9",
		},
		{
			// A store into an immutable buffer fails before the value is converted,
			// whether or not the index is valid and whatever the receiver is.
			name: "immutable element store",
			src: `var ta = new Uint8Array(new Uint8Array([1, 2]).buffer.transferToImmutable()), r = [], calls = 0;
				var v = {valueOf() { calls++; return 9; }};
				for (var i = 0; i < 3; i++) { ta[0] = 7; ta[1] = v; }
				r.push(ta.join(), calls);
				try { (function() { "use strict"; ta[0] = 7; })(); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.push(Reflect.set(ta, 0, 1), Reflect.set(ta, 5, 1), Reflect.set(ta, "-0", 1), Reflect.set(Object.create(ta), 0, 1));
				r.join()`,
			want: "1,2,0,TypeError,false,false,false,false",
		},
		{
			// The elements of an immutable buffer are non-writable and non-configurable,
			// so a typed array on one can be frozen.
			name: "immutable element descriptors",
			src: `var ta = new Uint8Array(new ArrayBuffer(2).transferToImmutable()), r = [];
				var d = Object.getOwnPropertyDescriptor(ta, 0);
				r.push(d.writable, d.enumerable, d.configurable);
				r.push(
					Reflect.defineProperty(ta, 0, {value: 0}),
					Reflect.defineProperty(ta, 0, {value: 1}),
					Reflect.defineProperty(ta, 0, {value: "0"}),
					Reflect.defineProperty(ta, 0, {writable: true}),
					Reflect.defineProperty(ta, 0, {writable: false, enumerable: true, configurable: false}));
				r.push(Object.isFrozen(ta), Object.isFrozen(Object.freeze(ta)));
				try { Object.freeze(new Uint8Array(2)); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.join()`,
			want: "false,true,false,true,false,false,false,true,false,true,TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), "test", tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetachImmutableArrayBuffer tests that $262.detachArrayBuffer refuses an immutable buffer.
func TestDetachImmutableArrayBuffer(t *testing.T) {
	vm := ekuma.NewVM()
	b, err := vm.RunScript(t.Context(), "test", "new ArrayBuffer(1).transferToImmutable()")
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if err := ekuma.DetachArrayBuffer(vm, b); err == nil {
		t.Errorf("DetachArrayBuffer: got no error")
	}
	if got, err := b.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached: got %v, %v, want false", got, err)
	}
}

// TestAtomicsNotifyWaitAsync checks that Atomics.notify wakes the waiters of its index in
// order, as many as its count, and returns how many it woke.
func TestAtomicsNotifyWaitAsync(t *testing.T) {
	vm := ekuma.NewVM()
	v, err := vm.RunScript(t.Context(), "notify", `
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
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "2,0,1,0"; got != want {
		t.Errorf("notify results: got %q, want %q", got, want)
	}
	v, err = vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "0ok,1ok,2ok"; got != want {
		t.Errorf("resolutions: got %q, want %q", got, want)
	}
}

// TestAtomicsWaitAsyncTimeout checks that a finite timeout resolves the promise with
// "timed-out" when the jobs run after it expired.
func TestAtomicsWaitAsyncTimeout(t *testing.T) {
	vm := ekuma.NewVM()
	_, err := vm.RunScript(t.Context(), "wait", `
		var log = [];
		var ia = new Int32Array(new SharedArrayBuffer(16));
		Atomics.waitAsync(ia, 0, 0, 1).value.then(r => log.push("short " + r));
		Atomics.waitAsync(ia, 0, 0).value.then(r => log.push("long " + r));
	`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	v, err := vm.RunScript(t.Context(), "expire", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), ""; got != want {
		t.Errorf("log before the jobs ran: got %q, want %q", got, want)
	}
	// The timed-out waiter is no longer in the list.
	v, err = vm.RunScript(t.Context(), "notify", `String(Atomics.notify(ia, 0))`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "1"; got != want {
		t.Errorf("notify: got %q, want %q", got, want)
	}
	v, err = vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "short timed-out,long ok"; got != want {
		t.Errorf("log: got %q, want %q", got, want)
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
	loc, err := time.LoadLocationFromTZData("Test/"+std.abbr, b)
	if err != nil {
		t.Fatalf("LoadLocationFromTZData: %v", err)
	}
	return loc
}

// TestDateOffsetTransitions checks local times at the transitions of time zones:
// a skipped local time takes the offset before the transition, a repeated one is the
// earlier instant, and the offset stays defined at the limits of the time values.
func TestDateOffsetTransitions(t *testing.T) {
	vm := ekuma.NewVM()
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
		ekuma.SetTimeZone(vm, tc.zone)
		v, err := vm.RunScript(t.Context(), "date", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}
