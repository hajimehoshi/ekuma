// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
	"weak"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
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
			name: "Math.atan2 of negative arguments whose quotient underflows",
			src:  `[Math.atan2(-5e-324, -2.5), Math.atan2(-1e-200, -1e200)].join()`,
			want: "-3.141592653589793,-3.141592653589793",
		},
		{
			name: "Math functions near the ends of the double range",
			src: `[Math.cosh(710), Math.sinh(-710), Math.cosh(711), Math.expm1(709.782712893384),
					Math.exp(-745), Math.cosh(709.5), Math.sinh(-709.5)].join()`,
			want: "1.1169973830808557e+308,-1.1169973830808557e+308,Infinity,1.7976931348622732e+308,5e-324," +
				"6.774931596573164e+307,-6.774931596573164e+307",
		},
		{
			name: "Math.log10 of powers of ten",
			src: `var bad = [];
				for (var e = -307; e <= 308; e++) {
					if (Math.log10(Number("1e" + e)) !== e) bad.push(e);
				}
				bad.join()`,
			want: "",
		},
		{
			name: "Math.log2 just above 1",
			src:  `Math.log2(1.0000000000000002)`,
			want: "3.203426503814917e-16",
		},
		{
			name: "10 to an integer power",
			src: `var bad = [];
				for (var e = -330; e <= 310; e++) {
					if (Math.pow(10, e) !== Number("1e" + e) || 10 ** e !== Number("1e" + e)) bad.push(e);
				}
				bad.join()`,
			want: "",
		},
		{
			name: "bitwise and shift operators with a literal outside the int32 range",
			src: `var h = -123456789, v = 5, w = 7;
				w |= 0x80000000;
				[h & 0xFFFFFFFF, h | 0x80000000, h << 1e21, h >> 33, h >>> 32, v | 1e21, v & 1e21,
				 0 | 4294967301, v | 1e400, w].join()`,
			want: "-123456789,-123456789,-123456789,-61728395,4171510507,-559939579,0,5,5,-2147483641",
		},
		{
			name: "a parenthesized destructuring target does not name its default",
			src: `var y, z;
				({a: (y) = function() {}} = {});
				[(z) = function() {}] = [];
				JSON.stringify([y.name, z.name])`,
			want: `["",""]`,
		},
		{
			name: "typed array set from an overlapping long view of the same type",
			src: `var a = new Uint8Array(70000);
				for (var i = 0; i < a.length; i++) a[i] = i & 0xff;
				a.set(a.subarray(0, 69999), 1);
				[a[0], a[1], a[65536], a[65537], a[69999]].join()`,
			want: "0,0,255,0,110",
		},
		{
			name: "toLowerCase of a long string keeps its characters whole",
			src: `var s = "\u00e9" + "a".repeat(65532) + "\u20ac" + "b".repeat(10);
				s.toLowerCase() === s`,
			want: "true",
		},
		{
			name: "empty separator split counts UTF-16 units with uint32 limits",
			src: `var s = "😀x";
				[s.split("", 4294967297).map(c => c.charCodeAt(0)).join(),
				 s.split("", -1).map(c => c.charCodeAt(0)).join()].join(";")`,
			want: "55357;55357,56832,120",
		},
		{
			name: "string concatenation",
			src:  `"a" + 1 + 2 + "b" + (1 + 2)`,
			want: "a12b3",
		},
		{
			name: "appending to a string that another variable holds",
			src: `var r = "";
				for (var i = 0; i < 40; i++) r += "ab";
				var s = r;
				var t = r + "X";
				var u = s + "Y";
				var v = t + t;
				r += "Z";
				[t.slice(78), u.slice(78), r.slice(78), s.slice(78), v.length, v.slice(79, 83),
					t === s + "X", u === r.slice(0, 80) + "Y"].join(" ")`,
			want: "abX abY abZ ab 162 bXab true true",
		},
		{
			name: "appended string as a property key and in comparisons",
			src: `var k = "";
				for (var i = 0; i < 40; i++) k += "k";
				var o = {};
				o[k] = 1;
				var k2 = k + "!";
				o[k2] = 2;
				var m = new Map([[k, "m"]]);
				var lit = "k".repeat(40);
				[o[lit], o[lit + "!"], m.get(lit), k === lit, k < k2, Object.keys(o).join().length].join(" ")`,
			want: "1 2 m true true 82",
		},
		{
			name: "appending non-ASCII strings while reading the length and the end",
			src: `var s = "", n = 0, bad = [];
				for (var i = 0; i < 40; i++) { s += "a"; n++; }
				var parts = ["\u00e9", "x", "\u{1F600}"];
				for (var i = 0; i < 60; i++) {
					var p = parts[i % 3];
					s += p;
					n += p.length;
					if (s.length !== n || s.charCodeAt(n - 1) !== p.charCodeAt(p.length - 1) || s.at(-p.length) !== p[0]) {
						bad.push(i);
					}
				}
				bad.length + " " + s.length + " " + (s.slice(-4) === "\u00e9x\u{1F600}") + " " + s.indexOf("\u00e9")`,
			want: "0 120 true 40",
		},
		{
			name: "random access to a long non-ASCII string with pairs across every 64th code unit",
			src: `var s = "bc", u = [0x62, 0x63];
				for (var i = 0; i < 300; i++) { s += "a\u{1F600}日"; u.push(0x61, 0xD83D, 0xDE00, 0x65E5); }
				s += "\uD800"; u.push(0xD800);
				var n = s.length, bad = 0;
				for (var i = 0; i < n; i++) {
					var j = (i * 7919) % n;
					if (s.charCodeAt(j) !== u[j] || s.slice(j, j + 1).charCodeAt(0) !== u[j]) bad++;
				}
				for (var k = n; k >= 0; k -= 61) {
					var want = -1;
					for (var j = Math.min(k, n - 2); j >= 0 && want < 0; j--) if (u[j] === 0x61 && u[j + 1] === 0xD83D) want = j;
					if (s.lastIndexOf("a\u{1F600}", k) !== want) bad++;
				}
				[n, bad, s.lastIndexOf("日"), s.lastIndexOf("日\uD800")].join(" ")`,
			want: "1203 0 1201 1201",
		},
		{
			name: "lone surrogates joined by concatenation",
			src: `var h = "\uD83D", l = "\uDE00";
				var s = "";
				for (var i = 0; i < 40; i++) s += "a";
				var t = s + h;
				var u = t + l;
				var w = ` + "`${s}${h}${l}`" + `;
				[t.length, u.length, u.codePointAt(40).toString(16), u === s + "\u{1F600}", w === u,
					` + "`${h}${l}`" + ` === "\u{1F600}"].join(" ")`,
			want: "41 42 1f600 true true true",
		},
		{
			name: "string search at surrogate boundaries",
			src: `var s = "😀x😀", h = "\ud83d", l = "\ude00";
				[s.indexOf(h), s.indexOf(l, 1), s.indexOf(h, 1), s.indexOf(l + "x" + h),
				 s.lastIndexOf(h), s.lastIndexOf(l, 3), s.lastIndexOf(l + "x" + h),
				 s.includes(h), s.indexOf("😀", 1), s.indexOf("x", 1), s.indexOf(h + "x")].join()`,
			want: "0,1,3,1,3,1,1,true,3,2,-1",
		},
		{
			name: "empty string search inside surrogate pair",
			src:  `["😀".indexOf("", 1), "😀".lastIndexOf("", 1)].join()`,
			want: "1,1",
		},
		{
			name: "string prefix and suffix at surrogate boundaries",
			src: `var s = "😀", h = "\ud83d", l = "\ude00";
				[s.startsWith(h), s.startsWith(l, 1), s.endsWith(l), s.endsWith(h, 1),
				 s.startsWith(s, 1), s.endsWith(s, 1)].join()`,
			want: "true,true,true,true,false,false",
		},
		{
			name: "string split at surrogate boundaries",
			src: `JSON.stringify(["😀".split("\ud83d"), "😀".split("\ude00"),
				"😀x😀".split("\ude00x\ud83d"), "😀😀".split("\ud83d", 2)])`,
			want: `[["","\ude00"],["\ud83d",""],["\ud83d","\ude00"],["","\ude00"]]`,
		},
		{
			name: "string replacement at surrogate boundaries",
			src: `JSON.stringify(["😀".replace("\ud83d", "_"), "😀😀".replaceAll("\ude00", "_"),
				"😀".replaceAll("", "_"), "😀".replace("\ude00", "[$&][$\u0060][$']")])`,
			want: `["_\ude00","\ud83d_\ud83d_","_\ud83d_\ude00_","\ud83d[\ude00][\ud83d][]"]`,
		},
		{
			name: "string replacement callback code unit positions",
			src: `var positions = [];
				"😀".replaceAll("", (match, pos) => { positions.push(pos); return ""; });
				"😀😀".replaceAll("\ude00", (match, pos) => { positions.push(pos); return ""; });
				positions.join()`,
			want: "0,1,2,1,3",
		},
		{
			name: "template literals",
			src: `var log = [];
				var a = {toString() { log.push("a"); return "A"; }};
				var b = {toString() { log.push("b"); return "B"; }, valueOf() { return "ignored"; }};
				function f(x) { log.push("f" + x); return x; }
				var t = ` + "`<${a}|${f(1)}|${b}${''}${f(2)}>`" + `;
				var out = "";
				for (var i = 0; i < 50; i++) out = ` + "`${out}${i % 10}`" + `;
				[t, log.join(), out.length, out.slice(-5), ` + "`${''}`" + `.length, ` + "`x`" + `].join(" ")`,
			want: "<A|1|B2> a,f1,b,f2 50 56789 0 x",
		},
		{
			name: "prepending to a string that another variable holds",
			src: `var r = "";
				for (var i = 0; i < 40; i++) r = "ab" + r;
				var s = r;
				var t = "X" + r;
				var u = "Y" + s;
				var g = u + "G";
				var v = t + t;
				var w = r + "Z";
				var e = s + t;
				r = "Q" + r;
				var q = "";
				for (var i = 0; i < 40; i++) q = "cd" + q;
				var qq = q + q;
				[t.slice(0, 3), u.slice(0, 3), g.slice(-2), r.slice(0, 3), s.slice(0, 2), w.slice(-3), v.length,
					v.slice(79, 84), t === "X" + s, u === "Y" + "ab".repeat(40),
					e === "ab".repeat(40) + "X" + "ab".repeat(40), qq === "cd".repeat(80)].join(" ")`,
			want: "Xab Yab bG Qab ab abZ 162 abXab true true true true",
		},
		{
			name: "prepended strings read in every way",
			src: `function pre(s) {
					var cs = Array.from(s), r = "";
					for (var i = cs.length; i-- > 0;) r = cs[i] + r;
					return r;
				}
				var d = "9876543210".repeat(4);
				var o = {};
				o[d] = 1;
				var o2 = {};
				o2[pre(d)] = 2;
				[pre(d).length, o[pre(d)], o2[d], pre(d) in o, o.hasOwnProperty(pre(d)),
					new Map([[d, "m"]]).get(pre(d)), new Set([pre(d)]).has(d),
					pre(d) === d, d === pre(d), pre(d) == d, "9876" < pre(d), pre(d) < "9877",
					[pre(d), "99", "9"].sort()[1] === d,
					pre(d)[1], pre(d).charCodeAt(39), pre(d).at(-2), pre(d).slice(10, 13), pre(d).substring(37),
					pre(d).indexOf("10", 5), pre(d).lastIndexOf("98"), pre(d).startsWith("987"), pre(d).split("0").length,
					/(\d)0(\d)/.exec(pre(d)).index, pre(d).replace(/0/g, "").length, pre(d).match(/9/g).length,
					JSON.stringify(pre(d)) === '"' + d + '"', JSON.parse(pre('{"a":[1,2,3],"b":"xyzxyzxyzxyzxyz"}')).b,
					+pre("1234567890123456789012345678901234"), BigInt(pre(d)) === BigInt(d), pre(d) == BigInt(d),
					BigInt(d) - 1n < pre(d), eval(pre("1+2+3+4+5+6+7+8+9+10+11+12+13+14")),
					(0, eval)(pre("'abcdefghij' + 'klmnopqrstuvwxyz'.length"))].join(" ")`,
			want: "40 1 2 true true m true true true true true true true 8 48 1 987 210 8 30 true 5 8 36 4 true xyzxyzxyzxyzxyz 1.2345678901234568e+33 true true true 105 abcdefghij16",
		},
		{
			name: "reading units and comparing a string while prepending to it",
			src: `var s = "", t = "", bad = [];
				for (var i = 0; i < 200; i++) {
					var c = String.fromCharCode(97 + i % 26);
					s = c + s;
					t = c + t;
					if (s[0] !== c || s.charCodeAt(s.length - 1) !== 97 || s.at(-2) !== (i ? "b" : undefined) ||
						s !== t || s === "x" || s === "q".repeat(i)) bad.push(i);
				}
				s = "é" + s;
				bad.length + " " + s.length + " " + s.slice(0, 4) + " " + s.charCodeAt(0).toString(16) + " " + (s === "é" + t)`,
			want: "0 201 érqp e9 true",
		},
		{
			name: "prepending non-ASCII strings while reading the length",
			src: `var s = "", n = 0, bad = [];
				for (var i = 0; i < 40; i++) { s = "a" + s; n++; }
				var parts = ["é", "x", "\u{1F600}"];
				for (var i = 0; i < 60; i++) {
					var p = parts[i % 3];
					s = p + s;
					n += p.length;
					if (s.length !== n) bad.push(i);
				}
				bad.length + " " + s.length + " " + (s.slice(0, 4) === "\u{1F600}xé") + " " + s.indexOf("a") + " " + s.codePointAt(0).toString(16)`,
			want: "0 120 true 80 1f600",
		},
		{
			name: "lone surrogates joined by prepending",
			src: `var h = "\uD83D", l = "\uDE00";
				var s = "", e = h;
				for (var i = 0; i < 40; i++) { s = "a" + s; e = "a" + e; }
				var t = l + s;
				var t2 = l + s;
				var x = e + t2;
				var w = ` + "`${h}${t}`" + `;
				var u = h + t;
				[t.length, u.length, u.codePointAt(0).toString(16), u === "\u{1F600}" + s, w === u,
					x.length, x === "a".repeat(40) + "\u{1F600}" + "a".repeat(40)].join(" ")`,
			want: "41 42 1f600 true true 82 true",
		},
		{
			name: "prepending in template literals",
			src: `var out = "";
				for (var i = 0; i < 50; i++) out = ` + "`${i % 10}-${out}`" + `;
				var mid = ` + "`<${out}>`" + `;
				var dbl = ` + "`${out}|${out}`" + `;
				var same = ` + "`${''}${out}`" + `;
				[out.length, out.slice(0, 6), mid.slice(0, 4), mid.slice(-3), dbl.length, same === out,
					out === Array.from({length: 50}, (_, i) => (49 - i) % 10 + "-").join("")].join(" ")`,
			want: "100 9-8-7- <9-8 0-> 201 true true",
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
			name: "properties of shapes of every size",
			src: `var out = [];
				for (var n = 1; n <= 10; n++) {
					var o = {};
					for (var i = 0; i < n; i++) o["p" + i] = i;
					var s = 0;
					for (var i = 0; i < n; i++) s += o["p" + i];
					var a = Object.assign({}, o), b = Object.assign({}, o);
					Object.defineProperty(a, "p0", {enumerable: false});
					delete b["p" + (n - 1)];
					out.push(s + ":" + ("q" in o) + ":" + Object.keys(a).length + ":" + Object.keys(b).join(""));
				}
				out.join(" ")`,
			want: "0:false:0: 1:false:1:p0 3:false:2:p0p1 6:false:3:p0p1p2 10:false:4:p0p1p2p3 " +
				"15:false:5:p0p1p2p3p4 21:false:6:p0p1p2p3p4p5 28:false:7:p0p1p2p3p4p5p6 " +
				"36:false:8:p0p1p2p3p4p5p6p7 45:false:9:p0p1p2p3p4p5p6p7p8",
		},
		{
			name: "repeated deletes from one object",
			src: `var o = {a: 1, b: 2, c: 3, d: 4, e: 5};
				function get(o) { return o.d; }
				var out = [get(o)];
				delete o.b; out.push(get(o));
				delete o.a; o.f = 6; out.push(get(o));
				delete o.e; delete o.d; out.push(get(o), Object.keys(o).join(), Object.values(o).join());
				var a = []; a[100000] = 0;
				for (var i = 0; i < 6; i++) a[i * 1000] = i;
				delete a[2000]; out.push(Object.keys(a).join());
				delete a[0]; a[500] = 7; out.push(Object.keys(a).join(), a[500], a[5000]);
				out.join("|")`,
			want: "4|4|4||c,f|3,6|0,1000,3000,4000,5000,100000|500,1000,3000,4000,5000,100000|7|5",
		},
		{
			name: "properties added to built-in objects",
			src: `var out = [], before = Object.getOwnPropertyNames(Math).length;
				Math.extra = 1;
				var names = Object.getOwnPropertyNames(Math);
				var s = 0;
				for (var i = 0; i < 3; i++) s += Math.abs(-1) + Math.extra;
				out.push(names.length - before, names[names.length - 1], Object.keys(Math).join(), s);
				for (var i = 0; i < 9; i++) Boolean["p" + i] = i;
				out.push(Object.getOwnPropertyNames(Boolean).slice(0, 4).join(), Boolean.length, Boolean.p8, Object.keys(Boolean).length);
				delete Boolean.p0;
				Object.defineProperty(Math, "abs", {enumerable: true});
				out.push(Object.getOwnPropertyNames(Boolean).length, Boolean.name, Object.keys(Math).join());
				out.join("|")`,
			want: "1|extra|extra|6|length,name,prototype,p0|1|8|9|11|Boolean|abs,extra",
		},
		{
			name: "objects left on shapes that a larger object passed through",
			src: `function make(n) { var o = {}; for (var i = 0; i < n; i++) o["q" + i] = i; return o; }
				var a = make(12), b = make(11), c = make(10);
				[a.q11, b.q10, b.q11, c.q9, c.q10, c.q3, a.q0].map(String).join()`,
			want: "11,10,undefined,9,undefined,3,0",
		},
		{
			name: "private elements consult a proxy's isExtensible trap",
			src: `var log = [];
				function trap(name) { return { isExtensible(t) { log.push(name); return Reflect.isExtensible(t); } }; }
				class C extends function (o) { return o; } {
					#a() {} get #b() { return 1; } set #b(v) {} #f = log.push("f"); #g = 1;
					static has(o) { return #a in o && #b in o && #f in o && #g in o; } }
				var p = new Proxy({}, trap("x"));
				new C(p);
				var r = log.join() + " " + C.has(p);
				log = [];
				var q = new Proxy(Object.preventExtensions({}), trap("y"));
				try { new C(q); } catch (e) { r += " " + e.name; }
				r + " " + log.join() + " " + C.has(q)`,
			want: "x,f,x,x true TypeError y false",
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
			name: "return before super() inside try",
			src: `var log = [];
				class D extends Object { constructor() { try { return; } catch (e) { log.push("caught"); } } }
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
			name: "many top-level declarations and private names",
			src: strings.Repeat("var v0, v1, v2, v3, v4, v5, v6, v7, v8, v9;", 2) + `var w0, w1, w2, w3, w4, w5, w6, w7, w8, w9;
				let lexA = 1; class K {} { function annexF() {} }
				var r = [lexA, "lexA" in globalThis, typeof K, typeof annexF];
				(function () {
					"use strict";
					w0 = String(1);
					try { undeclaredU = String(1); } catch (e) { r.push(e.constructor.name); }
				})();
				var names = Array.from({length: 20}, (_, i) => "#p" + i + " = " + i + ";").join("");
				r.push(eval("new (class { " + names + " get #q() { return 1; } set #q(v) {} m() { return this.#p19 + this.#q; } })().m()"));
				try { eval("(class { " + names + " #p3; })"); } catch (e) { r.push(e.constructor.name); }
				r.join()`,
			want: "1,false,function,function,ReferenceError,20,SyntaxError",
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
			name: "an elision in array destructuring does not read the value of the iterator result",
			src: `function iter(log) {
					return {[Symbol.iterator]() { return {
						next() { log.push("n"); return {get done() { log.push("done"); return false; }, get value() { log.push("val"); return 1; }}; },
						return() { log.push("ret"); return {}; },
					}; }};
				}
				var out = [], log = [], a;
				var [,] = iter(log); out.push(log.join()); log = [];
				[a, , ] = iter(log); out.push(log.join()); log = [];
				(function([, x]) {})(iter(log)); out.push(log.join()); log = [];
				for (var [,] of [iter(log)]); out.push(log.join()); log = [];
				try { var [,] = {[Symbol.iterator]() { return {next() { throw 1; }, return() { log.push("ret"); }}; }}; } catch (e) { log.push("caught"); }
				var [, , b] = [1, 2, 3];
				out.push(log.join(), b);
				out.join("|")`,
			want: "n,done,ret|n,done,val,n,done,ret|n,done,n,done,val,ret|n,done,ret|caught|3",
		},
		{
			name: "deleting many properties keeps the order and values of the others",
			src: `var o = {}, n = 20000;
				for (var i = 0; i < n; i++) o["k" + i] = i;
				for (var i = 0; i < n; i += 2) delete o["k" + i];
				var odd = Object.keys(o);
				o.k0 = "again";
				delete o.k1;
				Object.setPrototypeOf(o, {inherited: 1});
				o.last = "x";
				for (var i = 3; i < n - 100; i += 2) delete o["k" + i];
				Object.freeze(o);
				var keys = [];
				for (var k in o) keys.push(k);
				var a = [];
				a[79999] = 0;
				for (var i = 0; i < 8000; i++) a[(i * 7919) % 79999] = 8000 - i;
				a.sort((x, y) => x - y);
				[odd.length, odd[0], odd[n / 2 - 1], keys.length, keys[0], keys.slice(-3).join("/"), o.k19999,
					JSON.stringify(o).length, Object.isFrozen(o), a[0], a[7999], a[8000], 8000 in a, a.length].join()`,
			want: "10000,k1,k19999,53,k19901,k0/last/inherited,19999,775,true,0,7999,8000,true,80000",
		},
		{
			name: "a long chain of bound functions",
			src: `var f = function g() {}, a = f.bind(null), b = a.bind(null), c = a.bind(null);
				for (var i = 0; i < 200000; i++) f = f.bind(null);
				[f.name.length, f.name.slice(0, 12), f.name.slice(-7), a.name, b.name, c.name].join()`,
			want: "1200001,bound bound ,bound g,bound g,bound bound g,bound bound g",
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
			name: "object spread copies enumerable data properties into an independent object",
			src: `var src = {a: 1, b: 2}, c = {...src};
				c.a = 3; c.x = 4; src.y = 5;
				var f = {...Object.freeze({p: 1, q: 2})}; f.p = 6; f.r = 7;
				var n = {...Object.defineProperty({u: 1, v: 2}, "u", {enumerable: false})};
				[src.a, "x" in src, "y" in c, Object.keys(c), Object.isFrozen(f), Object.keys(f), f.p, Object.keys(n)].join("|")`,
			want: "1|false|false|a,b,x|false|p,q,r|6|v",
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
			name: "array filled from the end",
			src: `var a = []; for (var i = 199; i >= 0; i--) { a[i] = i * 2; if (i === 150) a.x = "x"; }
				var k = Object.keys(a);
				[a.length, a[0], a[17], a[199], k.length, k[0], k[1], k[199], k[200], a.indexOf(300), a.lastIndexOf(2),
					JSON.stringify(Object.getOwnPropertyDescriptor(a, 180))].join("|")`,
			want: `200|0|34|398|201|0|1|199|x|150|1|{"value":360,"writable":true,"enumerable":true,"configurable":true}`,
		},
		{
			name: "array with a few holes",
			src: `var a = []; a[40] = 40; for (var i = 0; i < 40; i++) if (i % 10 !== 5) a[i] = i;
				var n = 0; a.forEach(function () { n++; });
				[a.length, 5 in a, 6 in a, a[40], n, Object.keys(a).slice(4, 7).join(), a.indexOf(undefined), a.includes(undefined),
					a.findIndex(function (v) { return v === undefined; })].join("|")`,
			want: "41|false|true|40|37|4,6,7|-1|true|5",
		},
		{
			name: "pop with holes",
			src: `var a = [, 1, 2]; var b = [1, , ];
				Array.prototype[1] = "p";
				var r = [a.pop(), a.length, 0 in a, b.pop(), b.length, b.hasOwnProperty(1)];
				delete Array.prototype[1];
				r.join("|")`,
			want: "2|2|false|p|1|false",
		},
		{
			name: "array filled from the end keeps the attributes of its elements",
			src: `var a = [];
				Object.defineProperty(a, 150, { value: "ro", writable: false, enumerable: true, configurable: true });
				Object.defineProperty(a, 120, { get: function () { return "g"; }, enumerable: false, configurable: true });
				for (var i = 199; i >= 0; i--) if (i !== 150 && i !== 120) a[i] = i;
				a[150] = "changed";
				var k = Object.keys(a);
				[a[150], Object.getOwnPropertyDescriptor(a, 150).writable, a[120], k.length, k.indexOf("120"), a.length].join("|")`,
			want: "ro|false|g|199|-1|200",
		},
		{
			name: "array filled from the end becomes sparse and dense again",
			src: `var a = []; for (var i = 99; i >= 0; i--) a[i] = i;
				a[1000000] = "far";
				var k = Object.keys(a);
				var r = [a.length, k.length, k[99], k[100], 500 in a, a[1000000]];
				a.length = 50;
				k = Object.keys(a);
				r.push(a.length, k.length, k[49], 1000000 in a, a[60]);
				a[300] = 3;
				r.push(a.length, Object.keys(a).slice(-2).join());
				for (var i = 299; i >= 50; i--) a[i] = -i;
				r.push(Object.keys(a).length, a.lastIndexOf(3), a[100], a.indexOf(-50));
				r.join("|")`,
			want: "1000001|101|99|1000000|false|far|50|50|49|false||301|49,300|301|300|-100|50",
		},
		{
			name: "object with index keys filled from the end",
			src: `var o = { first: 1 }; for (var i = 99; i >= 0; i--) { o[i] = i; if (i === 50) o.mid = 2; }
				var k = Object.keys(o);
				[k.length, k[0], k[99], k[100], k[101], o[75], JSON.stringify(o).slice(-26)].join("|")`,
			want: `102|0|99|first|mid|75|"99":99,"first":1,"mid":2}`,
		},
		{
			name: "prototype filled from the end",
			src: `var p = {}; p[99] = 0; p.m = function () { return "m"; };
				var o = Object.create(p);
				function f() { return o.m() + o[50]; }
				var r = [f()];
				for (var i = 98; i >= 0; i--) p[i] = i;
				r.push(f(), f());
				r.join()`,
			want: "mundefined,m50,m50",
		},
		{
			name: "arguments filled from the end keep their mapping",
			src: `function f(a, b) {
					for (var i = 99; i >= 2; i--) arguments[i] = i;
					a = "a"; arguments[1] = "b";
					return [arguments[0], b, arguments.length, arguments[50]].join();
				}
				f(1, 2)`,
			want: "a,b,2,50",
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
			name: "caller of a sloppy function",
			src: `function f() { return f.caller; }
				function g() { return f(); }
				var a = () => f();
				var o = { m() { return f(); } };
				[f.caller === null, f() === null, g() === g, a() === a, o.m() === o.m, f.caller === null].join()`,
			want: "true,true,true,true,true,true",
		},
		{
			name: "caller hidden from a sloppy function",
			src: `function f() { return f.caller; }
				function s() { "use strict"; var c = f(); return c; }
				function* gen() { yield f(); }
				var r;
				(async function() { r = f(); })();
				function n() { return [0].map(f)[0]; }
				[s(), gen().next().value, r, n()].map(v => v === null).join()`,
			want: "true,true,true,true",
		},
		{
			name: "caller skips eval code",
			src: `function f() { return f.caller; }
				function g() { return eval("f()"); }
				function h() { return eval("eval('f()')"); }
				[g() === g, h() === h].join()`,
			want: "true,true",
		},
		{
			// The arguments beyond the parameters outlive the registers they arrived in.
			name: "arguments of a sloppy function",
			src: `function f(a) {
					a = 3;
					var args = f.arguments;
					var first = args[0];
					args[0] = 9;
					return [args.length, first, args[1], a, args.callee === f, args === f.arguments].join();
				}
				function r(n) { var inner = n ? r(n - 1) : ""; return r.arguments[0] + inner; }
				[String(f.arguments), f(1, 2), r(2)].join("|")`,
			want: "null|2,3,2,3,true,false|210",
		},
		{
			name: "getter of caller",
			src: `function f() {}
				function g() {}
				var get = Object.getOwnPropertyDescriptor(f, "caller").get;
				var r = [typeof get, get === Object.getOwnPropertyDescriptor(g, "caller").get];
				try { get.call(function() { "use strict"; }); } catch (e) { r.push(e.constructor.name); }
				r.join()`,
			want: "function,true,TypeError",
		},
		{
			name: "slice with an argument that shrinks the array",
			src: `var a = [1, 2, 3, 4, 5, 6, 7, 8];
				var r = a.slice({ valueOf() { a.length = 0; a.unshift(1); return 7; } }, { valueOf() { return 0; } });
				[r.length, a.join()].join("|")`,
			want: "0|1",
		},
		{
			name: "shift and push on a long queue",
			src: `var q = [], r = [];
				for (var i = 0; i < 100; i++) q.push(i);
				for (var i = 0; i < 150; i++) { r.push(q.shift()); if (i % 2) q.push(100 + i); }
				[q.length, r.slice(95, 105).join(), q.join()].join("|")`,
			want: "25|95,96,97,98,99,101,103,105,107,109|201,203,205,207,209,211,213,215,217,219,221,223,225,227,229,231,233,235,237,239,241,243,245,247,249",
		},
		{
			name: "shift moves holes",
			src: `var a = [0, 1, , 3]; for (var i = 4; i < 40; i++) a.push(i);
				a.shift();
				[a.length, 0 in a, 1 in a, 2 in a, a[2]].join()`,
			want: "39,true,false,true,3",
		},
		{
			name: "shift of holes reads the prototype",
			src: `Array.prototype[2] = "p";
				var a = [0, 1, , 3]; for (var i = 4; i < 40; i++) a.push(i);
				a.shift();
				[a.length, a.hasOwnProperty(1), a[1], 38 in a].join()`,
			want: "39,true,p,true",
		},
		{
			name: "unshift and splice move holes",
			src: `var a = [0, 1, , 3]; a.unshift(9);
				var b = [0, 1, , 3, 4]; var r = b.splice(1, 2, 7);
				var c = [0, 1, 2, , 4]; c.splice(0, 1); c.splice(1, 0, "x", "y");
				[a.length, a.hasOwnProperty(3), a[3], r.length, r.hasOwnProperty(1), b.join(), c.length, c.hasOwnProperty(4), c.hasOwnProperty(2), c[2]].join()`,
			want: "5,false,,2,false,0,7,3,4,6,false,true,y",
		},
		{
			name: "unshift and splice of holes read the prototype",
			src: `Array.prototype[2] = "p";
				var a = [0, 1, , 3]; a.unshift(9);
				var b = [0, 1, , 3, 4]; var r = b.splice(1, 2, 7);
				var c = [0, 1, 2, , 4]; c.splice(0, 1); c.splice(1, 0, "x", "y");
				[a.length, a.hasOwnProperty(3), a[3], r.length, r.hasOwnProperty(1), b.join(), c.length, c.hasOwnProperty(4), c.hasOwnProperty(2), c[2]].join()`,
			want: "5,true,p,2,true,0,7,3,4,6,true,true,y",
		},
		{
			name: "unshift into an array with room",
			src: `var a = []; for (var i = 0; i < 10; i++) a.unshift(i, -i);
				a.push(99); a.unshift(); a.join()`,
			want: "9,-9,8,-8,7,-7,6,-6,5,-5,4,-4,3,-3,2,-2,1,-1,0,0,99",
		},
		{
			name: "fill through a setter on the prototype",
			src: `var log = [];
				Object.defineProperty(Array.prototype, 1, {set(v) { log.push(v); }, configurable: true});
				var a = new Array(3).fill(7);
				[a.hasOwnProperty(0), a.hasOwnProperty(1), a.hasOwnProperty(2), log.join()].join()`,
			want: "true,false,true,7",
		},
		{
			name: "fill with a start that shrinks the array",
			src:  `var a = [1, 2, 3, 4]; a.fill(9, { valueOf() { a.length = 1; return 1; } }); a.join()`,
			want: "1,9,9,9",
		},
		{
			name: "fill of holes and past the elements",
			src:  `var a = [1, , 3]; a.length = 6; a.fill(0, 1, 5); [a.join(), 5 in a, new Array(3000).fill(2).lastIndexOf(2)].join("|")`,
			want: "1,0,0,0,0,|false|2999",
		},
		{
			name: "freeze of an object with its own shape after a cached store",
			src: `"use strict";
				var o = {}, r; for (var i = 0; i < 200; i++) o["k" + i] = i;
				function set(v) { o.k5 = v; }
				set(1); set(2);
				Object.freeze(o);
				try { set(3); } catch (e) { r = e.constructor.name; }
				[o.k5, r, Object.isFrozen(o), Object.keys(o).length, Object.getOwnPropertyDescriptor(o, "k199").writable].join()`,
			want: "2,TypeError,true,200,false",
		},
		{
			name: "compound assignment with number keys",
			src: `var a = [1, 2], o = {}, r;
				a[-0] += 5;
				o[4294967295] = 1; o[4294967295] += 1; o[2 ** 32 - 1]++;
				try { var u; u[5000] += 1; } catch (e) { r = e.message; }
				[a.join(), o["4294967295"], Object.keys(o).join(), r].join("|")`,
			want: "6,2|3|4294967295|cannot read properties of undefined (reading '5000')",
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
			name: "current realm Array species value",
			src: `class Other extends Array {}
				Object.defineProperty(Array, Symbol.species, {value: Other, configurable: true});
				var result = [1].map(x => x + 1);
				[result instanceof Other, result.length, result[0]].join()`,
			want: "true,1,2",
		},
		{
			name: "current realm Array species getter",
			src: `var calls = 0, receiver;
				Object.defineProperty(Array, Symbol.species, {get() {
					calls++; receiver = this; return Array;
				}, configurable: true});
				var result = [1, 2].slice(1);
				[calls, receiver === Array, result.join()].join()`,
			want: "1,true,2",
		},
		{
			name: "current realm Array species getter throws before splice mutates",
			src: `var sentinel = {}, caught, a = [1, 2];
				Object.defineProperty(Array, Symbol.species, {get() { throw sentinel; }});
				try { a.splice(0, 1); } catch (e) { caught = e; }
				[caught === sentinel, a.join()].join("|")`,
			want: "true|1,2",
		},
		{
			name: "current realm Array species is not a constructor",
			src: `Object.defineProperty(Array, Symbol.species, {value: {}});
				var caught;
				try { [1].map(x => x); } catch (e) { caught = e; }
				caught instanceof TypeError`,
			want: "true",
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
				function read(o) { return String(o.x); }
				var r = [];
				for (var d = 0; d < 2; d++) { var o = make(d); o.x = 10; r.push(read(o)); }
				Object.defineProperty(o, "x", { get() { return "new"; } });
				r.push(read(o));
				Object.defineProperty(o, "x", { get: undefined });
				r.push(read(o)); r.join()`,
			want: "10,11,new,undefined",
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
			name: "a numeric next method throws a TypeError when called",
			src: `var it = { [Symbol.iterator]() { return { next: 0 }; } };
				var log = [];
				try { var [a] = it; } catch (e) { log.push(e instanceof TypeError); }
				try { for (var x of it); } catch (e) { log.push(e instanceof TypeError); }
				try { [...it]; } catch (e) { log.push(e instanceof TypeError); }
				log.join()`,
			want: "true,true,true",
		},
		{
			name: "built-ins close an iterator when their callback throws or they stop, not when next throws",
			src: `var log = [];
				function mk(nextThrows) { return { [Symbol.iterator]() { var i = 0; return { next() { if (nextThrows && i == 1) throw "next"; i++; return { done: i > 4, value: i }; }, return() { log.push("return"); throw "ret"; } }; } }; }
				var add = Set.prototype.add;
				Set.prototype.add = function (v) { if (v == 2) throw "add"; return add.call(this, v); };
				try { new Set(mk(false)); } catch (e) { log.push(e); }
				try { new Set(mk(true)); } catch (e) { log.push(e); }
				Set.prototype.add = add;
				var other = { size: 1, has() {}, keys() { return mk(false)[Symbol.iterator](); } };
				try { new Set([1, 2, 3]).isSupersetOf(other); } catch (e) { log.push(e); }
				log.join()`,
			want: "return,add,next,return,ret",
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
			name: "normalize of normalized, unnormalized and ill-formed strings",
			src: `["é日", "é", "\uD800é\uDC00", "ẛ̣"].map(s => ["NFC", "NFD", "NFKC"].map(f =>
				[...s.normalize(f)].map(c => c.charCodeAt(0).toString(16)).join(".")).join(",")).join(" ")`,
			want: "e9.65e5,65.301.65e5,e9.65e5 e9,65.301,e9 d800.e9.dc00,d800.65.301.dc00,d800.e9.dc00 1e9b.323,17f.323.307,1e69",
		},
		{
			name: "normalize of long runs of combining marks, also across the chunks of a long string",
			src: `var below = "̖", acute = "́", x = "x".repeat(65500);
				var a = "e" + (below + acute).repeat(20), b = x + "e" + (acute + below).repeat(100);
				[
					a.normalize("NFD") === "e" + below.repeat(20) + acute.repeat(20),
					a.normalize("NFC") === "é" + below.repeat(20) + acute.repeat(19),
					b.normalize("NFD") === x + "e" + below.repeat(100) + acute.repeat(100),
					b.normalize("NFKC") === x + "é" + below.repeat(100) + acute.repeat(99),
				].join()`,
			want: "true,true,true,true",
		},
		{
			name: "BigInt.asUintN and asIntN of at most 64 bits on values beyond 64 bits",
			src: `var xs = [2n ** 64n + 5n, -(2n ** 64n) - 5n, 2n ** 100n - 1n, -(2n ** 70n) + 3n], bs = [1, 32, 63, 64];
				xs.map(x => bs.map(b => BigInt.asUintN(b, x) + "/" + BigInt.asIntN(b, x)).join(" ")).join("|")`,
			want: "1/-1 5/5 5/5 5/5|1/-1 4294967291/-5 9223372036854775803/-5 18446744073709551611/-5|" +
				"1/-1 4294967295/-1 9223372036854775807/-1 18446744073709551615/-1|1/-1 3/3 3/3 3/3",
		},
		{
			name: "BigInt exponentiation by a huge exponent",
			src: `var r = [0n ** (2n ** 64n), 1n ** (2n ** 64n), (-1n) ** (2n ** 64n + 1n)];
				try { (2n ** 256n) ** (2n ** 24n); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				r.join()`,
			want: "0,1,-1,true",
		},
		{
			name: "arguments passed on past a parameter default",
			src: `function g() { return arguments[arguments.length - 1]; }
				function f(a = 1) { return g.apply(null, arguments); }
				function h(a, b = a) { return g(...arguments); }
				var o = { set s(a = 1) { this.r = g(...arguments); } }; o.s = undefined;
				[f(undefined), h(1, undefined), o.r].join()`,
			want: ",,",
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
			name: "replace results longer than the string length limit",
			src: `var s = "x".repeat(` + strconv.Itoa(ekuma.MaxStringLength) + `);
				var r = [];
				for (var f of [() => s.replace(/x/, "xx$'"), () => s.replace("x", () => "xx")]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				}
				r.join()`,
			want: "true,true",
		},
		{
			name: "concatenations longer than the string length limit",
			src: `var s = "x".repeat(` + strconv.Itoa((ekuma.MaxStringLength+1)/2) + `);
				var r = [];
				for (var f of [() => s + s, () => ` + "`${s}${s}`" + `]) {
					try { f(); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); }
				}
				r.join()`,
			want: "true,true",
		},
		{
			name: "anonymous functions named after keys known at run time",
			src: `var k = "a";
				class C { [k] = () => {}; 128 = class { static n = this.name; }; }
				var c = new C();
				var o = { [k]: class { static n = this.name; } };
				[c.a.name, c[128].n, o.a.n].join()`,
			want: "a,128,a",
		},
		{
			name: "source of a RegExp with an escaped line separator",
			src: `var r = RegExp("\\\u2028");
				[r.source, RegExp("\\\u2029").source, eval("/" + r.source + "/").test("\u2028")].join()`,
			want: `\u2028,\u2029,true`,
		},
		{
			name: "order of the constants of Math",
			src:  `Object.getOwnPropertyNames(Math).slice(0, 8).join()`,
			want: "E,LN10,LN2,LOG10E,LOG2E,PI,SQRT1_2,SQRT2",
		},
		{
			name: "Array.from gets the iterator method once",
			src: `var n = 0;
				var o = { get [Symbol.iterator]() { n++; return function() { return [1, 2].values(); }; } };
				Array.from(o).join() + "|" + n`,
			want: "1,2|1",
		},
		{
			name: "Array.from of an array-like too long for an array",
			src:  `try { Array.from({ length: 2 ** 32 }); "no error" } catch (e) { e instanceof RangeError }`,
			want: "true",
		},
		{
			name: "sort deletes every index after the sorted values",
			src: `var deleted = [];
				var p = new Proxy([, , 0], { deleteProperty(t, k) { deleted.push(k); return delete t[k]; } });
				Array.prototype.sort.call(p);
				deleted.join()`,
			want: "1,2",
		},
		{
			name: "groupBy with an index accessor on Array.prototype",
			src: `Object.defineProperty(Array.prototype, "0", { get() { throw 1; }, set(v) { throw 2; } });
				var m = Map.groupBy(["a", "b"], () => "k");
				var o = Object.groupBy(["a"], () => "k");
				[m.get("k").length, Object.getOwnPropertyDescriptor(m.get("k"), "1").value, o.k.length].join()`,
			want: "2,b,1",
		},
		{
			name: "union and symmetricDifference copy the set after getting the keys iterator",
			src: `var s = new Set([1, 2, 3]);
				var other = { size: 0, has() {}, keys() { return { get next() { s.clear(); s.add(4); return () => ({ done: true }); } }; } };
				[...s.union(other)].join() + "|" + [...s.symmetricDifference(other)].join()`,
			want: "4|4",
		},
		{
			name: "Set methods call a has method of a set other than the built-in one",
			src: `var other = new Set([1, 2, 3]), calls = 0;
				other.has = function (x) { calls++; return x === 2; };
				var a = new Set([1, 2]).intersection(other), b = new Set([1, 2, 3, 4, 5, 6]).difference(new Set([2, 4]));
				[...a].join() + "|" + calls + "|" + [...b].join()`,
			want: "2|2|1,3,5,6",
		},
		{
			name: "Map and Set from arrays with many distinct keys of several kinds and duplicates",
			src: `var o = {}, src = [];
				for (var i = 0; i < 16; i++) src.push(i);
				for (var i = 0; i < 40; i++) src.push("k" + i % 20, i % 8, o, 10n ** 30n);
				var s = new Set(src), m = new Map(src.map((k, i) => [k, i]));
				[s.size, m.size, s.has("k19"), s.has(7), s.has(o), s.has(10n ** 30n), m.get("k0"), m.get(o), [...s].slice(14, 19).join()].join(" ")`,
			want: "38 38 true true true true 96 174 14,15,k0,[object Object],1000000000000000000000000000000",
		},
		{
			name: "DataView with a length whose conversion detaches the buffer",
			src: `var ab = new ArrayBuffer(16);
				try { new DataView(ab, 8, { valueOf() { ab.transfer(); return 8; } }); "no error" } catch (e) { e.constructor.name }`,
			want: "TypeError",
		},
		{
			name: "RegExp exec after converting lastIndex recompiles the RegExp",
			src: `var re = /a/;
				re.lastIndex = { valueOf() { re.compile("b"); return 0; } };
				re[Symbol.match]("b") !== null`,
			want: "true",
		},
		{
			name: "RegExp search restores lastIndex before reading the index",
			src: `var log = [], n = 0;
				var o = {
					get lastIndex() { log.push("get"); return n++; },
					set lastIndex(v) { log.push("set"); },
					exec() { log.push("exec"); return { get index() { log.push("index"); return 3; } }; },
				};
				RegExp.prototype[Symbol.search].call(o, "x") + ":" + log.join()`,
			want: "3:get,exec,get,set,index",
		},
		{
			name: "RegExp split reads exec only in RegExpExec",
			src: `var gets = 0;
				class R extends RegExp { get exec() { gets++; return RegExp.prototype.exec; } }
				"abc".split(new R("b")).join() + ":" + gets`,
			want: "a,c:3",
		},
		{
			name: "fields of a base class are initialized before the parameters",
			src: `var log = [];
				class A { x = log.push("field"); constructor(a = log.push("param")) {} }
				new A();
				log.join()`,
			want: "field,param",
		},
		{
			name: "the heritage and computed keys of a class are strict",
			src: `var r = [];
				try { class B extends (Object.freeze({}).x = 1, Object) {} r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				try { class C { [(Object.freeze({}).y = 1, "m")]() {} } r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.join()`,
			want: "TypeError,TypeError",
		},
		{
			name: "super gets the parent constructor before the arguments and checks it after",
			src: `class B {}
				class D extends B { constructor() { super(Object.setPrototypeOf(D, null)); } }
				var token = {};
				class E extends B { constructor() { super((() => { throw token; })()); } }
				Object.setPrototypeOf(E, Math.sin);
				var r;
				try { new E(); } catch (e) { r = e === token; }
				(new D() instanceof B) + "," + r`,
			want: "true,true",
		},
		{
			name: "delete of an optional chain whose last link short-circuits",
			src: `var o = { a: undefined };
				[delete undefined?.x, delete o.a?.["x"], delete o?.a].join()`,
			want: "true,true,true",
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
		{
			name: "compiling a RegExp from a literal leaves the other objects of the literal alone",
			src: `function f() { return /a/g; }
				var r1 = f(), r2 = f();
				r1.compile("b", "i");
				[r1 !== r2, r1.source, r1.flags, r2.source, r2.flags, r2.test("A"), f().test("a")].join()`,
			want: "true,b,i,a,g,false,true",
		},
		{
			name: "Object.prototype.toString of primitives",
			src: `var ts = Object.prototype.toString;
				Object.defineProperty(String.prototype, Symbol.toStringTag, { value: "Str" });
				[ts.call(1), ts.call("s"), ts.call(true), ts.call(Symbol()), ts.call(1n)].join()`,
			want: "[object Number],[object Str],[object Boolean],[object Symbol],[object BigInt]",
		},
		{
			name: "Object.prototype.toString passes a wrapper of a primitive to a getter",
			src: `Object.defineProperty(Number.prototype, Symbol.toStringTag, {
					get() { "use strict"; return typeof this; },
				});
				Object.prototype.toString.call(1)`,
			want: "[object object]",
		},
		{
			name: "Object.prototype.toString passes a wrapper of a primitive to a proxy",
			src: `Object.setPrototypeOf(Boolean.prototype, new Proxy({}, {
					get(t, k, r) { return typeof r; },
				}));
				Object.prototype.toString.call(true)`,
			want: "[object object]",
		},
		{
			name: "array iteration modified after an iteration",
			src: `[...[1, 2]];
				Array.prototype[Symbol.iterator] = function* () { yield "X"; };
				String([...[1, 2]])`,
			want: "X",
		},
		{
			name: "forwarded arguments with a modified array iterator next",
			src: `function g(...r) { return r.length; }
				(function (a, b) { return g(...arguments); })(1, 2);
				Object.getPrototypeOf([][Symbol.iterator]()).next = function () { return { done: true }; };
				(function (a, b) { return g(...arguments); })(1, 2)`,
			want: "0",
		},
		{
			name: "push and unshift through a setter on the prototype of a subclass",
			src: `var log = [];
				class A extends Array {}
				Object.defineProperty(A.prototype, 0, { set(v) { log.push(v); }, get() { return "G"; } });
				new A().push(1);
				new A().unshift(2);
				log.join()`,
			want: "1,2",
		},
		{
			name: "exact ties finer than one ulp",
			src: `[(2 ** -24).toExponential(15), (0.45).toFixed(53), (2 ** -24).toPrecision(16),
					(-9.5).toExponential(0), (99.5).toFixed(0)].join(" ")`,
			want: "5.960464477539063e-8 0.45000000000000001110223024625156540423631668090820313 5.960464477539063e-8 -1e+1 100",
		},
		{
			name: "remainder of negative zero by a register",
			src:  `var x = -0, y = 1; 1 / (x % y)`,
			want: "-Infinity",
		},
		{
			// A parameter expression keeps the arguments object even with a function named
			// arguments, and so stops the block-level function from being hoisted.
			name: "block-level function arguments and parameter expressions",
			src: `function f(a = 0) {
					function arguments() { return "top"; }
					{ function arguments() { return "block"; } }
					return arguments();
				}
				function g({a}) {
					function arguments() { return "top"; }
					{ function arguments() { return "block"; } }
					return arguments();
				}
				f() + " " + g({})`,
			want: "top block",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var vm ekuma.VM
			v, err := runScript(ctx, &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
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

// TestCapturedVariables covers closures that see variables which change or are not yet
// initialized when the closures are created.
func TestCapturedVariables(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "parameter assigned after the closure is created",
			src:  `function f(a) { const g = () => a; a = 2; return g(); } f(1)`,
			want: "2",
		},
		{
			name: "parameter assigned by a nested function",
			src:  `function f(a) { const g = () => () => a; (() => { a = 3; })(); return g()(); } f(1)`,
			want: "3",
		},
		{
			name: "parameter assigned by eval",
			src:  `function f(a) { const g = () => a; eval("a = 4"); return g(); } f(1)`,
			want: "4",
		},
		{
			name: "parameter assigned by eval in a nested function",
			src:  `function f(a) { const g = () => a; (() => eval("a = 5"))(); return g(); } f(1)`,
			want: "5",
		},
		{
			name: "parameter assigned in a with statement",
			src:  `function f(a) { const g = () => a; with ({}) { a = 6; } return g(); } f(1)`,
			want: "6",
		},
		{
			name: "parameter assigned through arguments",
			src:  `function f(a) { const g = () => a; (() => { arguments[0] = 7; })(); return g(); } f(1)`,
			want: "7",
		},
		{
			name: "parameter declared again by var",
			src:  `function f(a) { const g = () => a; for (var a of [8]); return g(); } f(1)`,
			want: "8",
		},
		{
			name: "generator suspended across an assignment",
			src: `function* gen(a) { let x = a; const g = () => x + a; yield g(); x = 10; yield g(); }
				[...gen(1)].join()`,
			want: "2,11",
		},
		{
			name: "per-iteration bindings",
			src: `var fs = [];
				for (const x of [1, 2]) { const y = x * 10; fs.push(() => x + y); }
				for (let i = 0, k = 100; i < 2; i++) fs.push(() => i + k);
				fs.map(f => f()).join()`,
			want: "11,22,100,101",
		},
		{
			name: "block variable of a region that throws",
			src: `var f;
				try { let v = 1; f = () => v; v = 2; throw "T"; } catch (e) {}
				f()`,
			want: "2",
		},
		{
			name: "loop variable of a generator that returns",
			src: `var f;
				function* gen() { for (let v of [1, 2]) { f = () => v; if (v == 5) v = "x"; yield v; } }
				var it = gen(); it.next(); it.return("r");
				f()`,
			want: "1",
		},
		{
			name: "let read before its initialization",
			src: `function f() {
					const g = () => x;
					function h() { return x; }
					let r = [];
					try { g(); } catch (e) { r.push(e.name); }
					try { h(); } catch (e) { r.push(e.name); }
					let x = 1;
					return r.concat(g(), h()).join();
				}
				f()`,
			want: "ReferenceError,ReferenceError,1,1",
		},
		{
			name: "function declarations instantiated together",
			src:  `function f() { { function g() { return typeof h; } function h() {} return g(); } } f()`,
			want: "function",
		},
		{
			name: "let whose initialization a switch skips",
			src: `var g;
				switch (1) { case 0: let x = 1; case 1: g = () => x; }
				var r; try { g(); } catch (e) { r = e.name; } r`,
			want: "ReferenceError",
		},
		{
			name: "default values that refer to later bindings",
			src: `function f(a = () => b, b = 2) { return a(); }
				var r; try { throw {}; } catch ({c = () => d, d = 3}) { r = c(); }
				f() + r`,
			want: "5",
		},
		{
			name: "this before and after super",
			src: `class B extends Object {
					constructor() {
						const f = () => this;
						let r;
						try { f(); } catch (e) { r = e.name; }
						super();
						this.r = r + (f() === this);
					}
				}
				new B().r`,
			want: "ReferenceErrortrue",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var vm ekuma.VM
			v, err := runScript(ctx, &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAllocationsPerOperation covers the number of allocations of common operations.
func TestAllocationsPerOperation(t *testing.T) {
	if raceEnabled {
		t.Skip("the race detector changes the number of allocations")
	}
	tests := []struct {
		name  string
		setup string
		body  string
		want  float64
	}{
		{
			name:  "arrow function capturing this and a parameter",
			setup: `var o = {x: 1, m(a) { return () => this.x + a; }};`,
			body:  `o.m(1);`,
			want:  1,
		},
		{
			name:  "spread of an array",
			setup: `var src = []; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `[...src];`,
			want:  2,
		},
		{
			name:  "spread of an object into a literal",
			setup: `var src = {a: 1, b: 2, c: 3, d: 4};`,
			body:  `({...src});`,
			want:  2,
		},
		{
			name:  "map",
			setup: `var src = []; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `src.map(x => x);`,
			want:  3,
		},
		{
			name: "push onto an empty array",
			body: `var a = []; for (var j = 0; j < 16; j++) a.push(j);`,
			want: 4,
		},
		{
			name:  "concatenation of two long strings",
			setup: `var a = "a".repeat(20), b = "b".repeat(20);`,
			body:  `a + b;`,
			want:  2,
		},
		{
			name:  "template literal",
			setup: `var a = "0";`,
			body:  "`x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}x${a}`;",
			want:  2,
		},
		{
			name:  "stores into an array filled from the end",
			setup: `var a = []; for (var j = 199; j >= 0; j--) a[j] = j;`,
			body:  `for (var j = 0; j < 200; j++) a[j] = j;`,
			want:  0,
		},
		{
			name:  "compound assignments to elements with large indices",
			setup: `var a = new Array(3000).fill(0);`,
			body:  `a[2000] += 1; a[2001]++;`,
			want:  0,
		},
		{
			name:  "construction of an instance of a subclass",
			setup: `class P { constructor(x, y) { this.x = x; this.y = y; } } class Q extends P { constructor(x) { super(x, 2); } }`,
			body:  `new Q(1);`,
			want:  1,
		},
		{
			name:  "for-in over an array",
			setup: `var src = [], s = 0; for (var j = 0; j < 16; j++) src.push(j);`,
			body:  `for (var k in src) s += src[k];`,
			want:  1,
		},
		{
			name:  "for-in over an object with index keys",
			setup: `var src = {x: 0}, s = 0; for (var j = 0; j < 16; j++) src[j] = j;`,
			body:  `for (var k in src) s += src[k];`,
			want:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			var vm ekuma.VM
			src := tt.setup + "\n(function(n) { for (var i = 0; i < n; i++) { " + tt.body + " } })"
			fn, err := runScript(ctx, &vm, tt.name, src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			const n = 100
			allocs := func(n int) float64 {
				return testing.AllocsPerRun(10, func() {
					if _, err := fn.Invoke(ctx, n); err != nil {
						t.Fatal(err)
					}
				})
			}
			if got := (allocs(n) - allocs(0)) / n; got != tt.want {
				t.Errorf("allocations per operation: got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestAppendedStringsToGo covers strings that a script builds by appending to one string:
// each one that reached Go keeps its contents while the script appends further.
func TestAppendedStringsToGo(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	fn, err := runScript(ctx, &vm, "append", `(function() {
		var r = "";
		return function(s) { r += s; return r; };
	})()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	var got []string
	for range 100 {
		v, err := fn.Invoke(ctx, "ab")
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		got = append(got, v.String())
	}
	for i, s := range got {
		if want := strings.Repeat("ab", i+1); s != want {
			t.Errorf("string %d: got %q, want %q", i, s, want)
		}
	}
}

// TestPrependedStringsToGo covers strings that a script builds by prepending to one string:
// each one has its contents when it reaches Go, even after the script prepended further.
func TestPrependedStringsToGo(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	fn, err := runScript(ctx, &vm, "prepend", `(function() {
		var r = "";
		return function(s) { r = s + r; return r; };
	})()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	var got []ekuma.Value
	for range 100 {
		v, err := fn.Invoke(ctx, "ab")
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		got = append(got, v)
	}
	for i, v := range got {
		if s, want := v.String(), strings.Repeat("ab", i+1); s != want {
			t.Errorf("string %d: got %q, want %q", i, s, want)
		}
	}
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
			var vm ekuma.VM
			var v ekuma.Value
			for _, src := range tt.scripts {
				var err error
				v, err = runScript(t.Context(), &vm, tt.name, src)
				if err != nil {
					t.Fatalf("runScript: %v", err)
				}
			}
			if got := toString(t, &vm, v); got != tt.want {
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
		{
			name: "eval declarations shadow the name of a function expression",
			src: `[
					(function f() { var before = typeof f; eval("var f = 1"); return before + typeof f; })(),
					(function f() { eval("function f() { return 1; }"); return f(); })(),
					(function f() { eval("var f"); f = 2; return f; })(),
				].join()`,
			want: "functionnumber,1,2",
		},
		{
			name: "eval declarations in parameter defaults shadow the name of a function expression",
			src: `[
					(function f(x = eval("var f = 3"), y = () => f) { return typeof f + y(); })(),
					(function f(x = () => f, y = eval("var f = 4")) { return x(); })(),
					(function f(x = eval("1"), y = () => typeof f) { return y(); })(),
				].join()`,
			want: "number3,4,function",
		},
		{
			name: "a block-level function of eval code is hoisted over a catch parameter",
			src: `(function () {
					try { throw 1; } catch (g) { eval("{ function g() {} }"); }
					return typeof g;
				})()`,
			want: "function",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
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
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestForIn covers the keys that for-in loops visit on objects with elements and on their
// prototypes.
func TestForIn(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "own keys hide the keys of prototypes",
			src: `var p2 = {p: 0, 1: 0, length: 0, m: 0};
				var p1 = Object.create(p2, {m: {value: 0, enumerable: false}});
				var a = Object.setPrototypeOf([10, 20, 30], p1);
				a.n = 0;
				var keys = [];
				for (var k in a) keys.push(k);
				keys.join()`,
			want: "0,1,2,n,p",
		},
		{
			name: "keys deleted before they come up do not hide the keys of prototypes",
			src: `var a = Object.setPrototypeOf([10, 20, 30], {0: 0, 1: 0, x: 0, y: 0});
				a.x = 0;
				a.y = 0;
				var keys = [];
				for (var k in a) {
					keys.push(k);
					if (k === "0") {
						delete a[0];
						delete a[1];
						delete a.y;
					}
				}
				keys.join()`,
			want: "0,2,x,1,y",
		},
		{
			name: "keys added during the loop come up only from prototypes",
			src: `var a = Object.setPrototypeOf([10, 20], {3: 0});
				var keys = [];
				for (var k in a) {
					keys.push(k);
					if (k === "0") {
						a[2] = 30;
						a[3] = 40;
					}
				}
				keys.join()`,
			want: "0,1,3",
		},
		{
			name: "holes",
			src: `var a = Object.setPrototypeOf([10, , 30], {1: 0});
				var keys = [];
				for (var k in a) keys.push(k);
				keys.join()`,
			want: "0,2,1",
		},
		{
			name: "keys are strings",
			src: `var a = [];
				for (var i = 0; i < 1100; i++) a.push(i);
				var n = 0, bad = 0;
				for (var round = 0; round < 2; round++) {
					for (var k in a) {
						n++;
						if (typeof k !== "string" || k !== "" + a[k]) bad++;
					}
				}
				var o = {7: 0, 2000: 0};
				var cat = [];
				for (var k in o) cat.push(k + 1);
				[n, bad, cat.join()].join()`,
			want: "2200,0,71,20001",
		},
		{
			name: "prototype that changes between loops",
			src: `var p = Object.defineProperty({}, "h", {value: 0, configurable: true});
				var o = Object.create(p);
				o.a = 0;
				function keys() { var s = ""; for (var k in o) s += k; return s; }
				var r = [keys()];
				p.x = 0;
				r.push(keys());
				delete p.x;
				r.push(keys());
				p.y = 0;
				r.push(keys());
				Object.defineProperty(p, "h", {enumerable: true});
				r.push(keys());
				r.join()`,
			want: "a,ax,a,ay,ahy",
		},
		{
			name: "proxy as a prototype",
			src: `var log = [];
				var p = new Proxy({a: 1, b: 2}, {
					ownKeys(t) { log.push("ownKeys"); return Reflect.ownKeys(t); },
					getOwnPropertyDescriptor(t, k) { log.push("gopd:" + k); return Reflect.getOwnPropertyDescriptor(t, k); },
					getPrototypeOf(t) { log.push("getPrototypeOf"); return null; },
				});
				var o = Object.setPrototypeOf({a: 0}, p);
				var keys = [];
				for (var k in o) keys.push(k);
				keys.join() + " " + log.join()`,
			want: "a,b ownKeys,gopd:b,getPrototypeOf",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
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
			var vm ekuma.VM
			src := `
				var it = { [Symbol.asyncIterator]() { return { next() { return { done: false }; }, return() { ` + tt.ret + ` } }; } };
				async function* g() { for await (var x of it) yield 1; }
				var got = "";
				var r = g();
				r.next().then(() => r.return()).then(() => { got = "fulfilled"; }, e => { got = e instanceof TypeError ? "TypeError" : String(e); });
			`
			if _, err := runScript(t.Context(), &vm, tt.name, src); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			v, err := runScript(t.Context(), &vm, "got", `got`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
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
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAwaitUsingOnReturn checks that the return method of an async generator awaits the
// disposal of the await using declarations it leaves.
func TestAwaitUsingOnReturn(t *testing.T) {
	var vm ekuma.VM
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
	if _, err := runScript(t.Context(), &vm, "await using", src); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	v, err := runScript(t.Context(), &vm, "log", `log.join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := toString(t, &vm, v), `disposed,disposed,{"value":1,"done":true},boom`; got != want {
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
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
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
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
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
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "weak", src)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := toString(t, &vm, v), "false,2,true,2,false,false,2,true,2,false"; got != want {
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
			name: "setFromHex even UTF-16 length beyond capacity",
			src: `var a = new Uint8Array([255]);
				var r = a.setFromHex("00éa");
				[r.read, r.written, a[0]].join()`,
			want: "2,1,0",
		},
		{
			name: "setFromHex odd UTF-16 length before writing",
			src: `var a = new Uint8Array([255]), syntaxError = false;
				try { a.setFromHex("00é"); } catch (e) { syntaxError = e instanceof SyntaxError; }
				[syntaxError, a[0]].join()`,
			want: "true,255",
		},
		{
			name: "setFromHex invalid Unicode pair after writing",
			src: `var a = new Uint8Array([255, 255]), syntaxError = false;
				try { a.setFromHex("00éa"); } catch (e) { syntaxError = e instanceof SyntaxError; }
				[syntaxError, ...a].join()`,
			want: "true,0,255",
		},
		{
			name: "base64 and hex decoding around whole chunks",
			src: `function r(f) { try { return f(); } catch (e) { return e.name; } }
				var a = new Uint8Array(4), b = new Uint8Array(8);
				[Uint8Array.fromBase64("AQID BAUG\nBw==").join(),
					Uint8Array.fromBase64("AQIDBA-_", {alphabet: "base64url"}).join(),
					JSON.stringify(a.setFromBase64("AQIDBAUG")), a.join(),
					r(() => b.setFromBase64("AQIDBAUG*AAA")), b.join(),
					Uint8Array.fromBase64("AQIDBA", {lastChunkHandling: "stop-before-partial"}).join(),
					Uint8Array.fromHex("00ff10Ab").join(),
					JSON.stringify(a.setFromHex("0102030405")), r(() => a.setFromHex("0102zz")), a.join()].join(" ")`,
			want: `1,2,3,4,5,6,7 1,2,3,4,15,191 {"read":4,"written":3} 1,2,3,0 SyntaxError 1,2,3,4,5,6,0,0 1,2,3 0,255,16,171 {"read":8,"written":4} SyntaxError 1,2,3,4`,
		},
		{
			name: "construction from arrays that start with numbers",
			src: `var v = {valueOf() { return -1; }};
				[new Int8Array([1, 300, "7", v, 2.9]), Int16Array.from([1, 70000, "7", v]), Float32Array.from([1, 2], x => x * 2),
					new Uint8Array([1, , 3]), Uint8Array.from({length: 2, 0: 9})].join(" ")`,
			want: "1,44,7,-1,2 1,4464,7,-1 2,4 1,0,3 9,0",
		},
		{
			name: "searches for negative and out-of-range numbers",
			src: `[Int8Array, Int16Array, Int32Array, Uint8Array, Uint16Array, Uint8ClampedArray, Float32Array].map(C => {
				var a = new C([5, -1, 255, 65535, -129, 0.5, 7, -1, 0]);
				return [a.indexOf(-1), a.lastIndexOf(-1), a.includes(255), a.indexOf(65535), a.indexOf(-129), a.indexOf(0.5), a.lastIndexOf(0), a.indexOf(-0)].join();
			}).join(" ")`,
			want: "1,7,false,-1,-1,-1,8,5 1,7,true,-1,4,-1,8,5 1,7,true,3,4,-1,8,5 -1,-1,true,-1,-1,-1,8,5 -1,-1,true,1,-1,-1,8,5 -1,-1,true,-1,-1,-1,8,1 1,7,true,3,4,5,8,8",
		},
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
			// A maxByteLength above 2^31 either keeps the buffer resizable or, where an int
			// cannot hold it, is a RangeError.
			name: "large maxByteLength",
			src: `var r = [];
				for (var f of [() => new ArrayBuffer(0, {maxByteLength: 2 ** 32 - 1}), () => new SharedArrayBuffer(0, {maxByteLength: 2 ** 32 - 1})]) {
					try {
						var b = f();
						r.push((b.resizable ?? b.growable) && b.maxByteLength === 2 ** 32 - 1 ? "ok" : "fixed");
					} catch (e) {
						r.push(e instanceof RangeError ? "ok" : e.constructor.name);
					}
				}
				r.join()`,
			want: "ok,ok",
		},
		{
			// An index whose byte offset does not fit in 32 bits is out of bounds.
			name: "large index",
			src: `var a = new Float64Array([1.5]), r = [];
				for (var i of [2 ** 29, 2 ** 31, 2 ** 32 - 2]) {
					a[i] = 2;
					r.push(a[i], a[0]);
				}
				r.join()`,
			want: ",1.5,,1.5,,1.5",
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
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, "test", tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRegExpModified tests that the RegExp built-ins follow modifications of RegExp objects,
// %RegExp.prototype% and %RegExp% made after the built-ins ran on unmodified ones.
func TestRegExpModified(t *testing.T) {
	// warm runs the built-ins on unmodified RegExp objects first.
	const warm = `var log = [];
		var warm = ["abab".replace(/b/g, "x"), "a,b".split(/,/), "abab".match(/b/g), /b/g.flags, [..."ab".matchAll(/b/g)]];
		`
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "assigned exec",
			src: `RegExp.prototype.exec = function (s) { log.push("exec"); return null; };
				["abab".replace(/b/g, "x"), /b/.test("abab"), "abab".match(/b/g), "a,b".split(/,/), log.length].join(" ")`,
			want: "abab false  a,b 6",
		},
		{
			name: "redefined flag getter",
			src: `Object.defineProperty(RegExp.prototype, "global", {get() { log.push("global"); return false; }});
				[/b/g.flags, "abab".replace(/b/g, "x"), log.length].join(" ")`,
			want: " axab 2",
		},
		{
			name: "own properties",
			src: `var re = /b/g, li = /b/y, frozen = Object.freeze(/b/g);
				Object.defineProperty(re, "flags", {value: ""});
				li.lastIndex = {valueOf() { log.push("lastIndex"); return 1; }};
				var r = ["abab".replace(re, "x"), "abab".replace(li, "x"), log.join()];
				try { "abab".replace(frozen, "x"); r.push("no error"); } catch (e) { r.push(e.constructor.name); }
				r.join(" ")`,
			want: "axab axab lastIndex TypeError",
		},
		{
			name: "subclass",
			src: `class R extends RegExp { exec(s) { log.push(this.flags); return super.exec(s); } }
				["abab".replace(new R("b", "g"), "x"), "a,b".split(new R(",")), log.join()].join(" ")`,
			want: "axax a,b g,g,g,y,y,y",
		},
		{
			name: "species constructor",
			src: `function C(p, f) { log.push("C " + f); return new RegExp(p, f); }
				C[Symbol.species] = C;
				RegExp.prototype.constructor = C;
				var r = ["a,b".split(/,/), [..."abab".matchAll(/b/g)].length];
				delete RegExp.prototype.constructor;
				Object.defineProperty(RegExp, Symbol.species, {get() { log.push("species"); return C; }});
				r.push(Object.getPrototypeOf(/b/g).constructor === Object, log.join());
				r.join(" ")`,
			want: "a,b 2 true C y,C g",
		},
		{
			name: "species getter of RegExp",
			src: `Object.defineProperty(RegExp, Symbol.species, {get() { log.push("species"); return RegExp; }});
				["a,b".split(/,/), [..."abab".matchAll(/b/g)].length, log.join()].join(" ")`,
			want: "a,b 2 species,species",
		},
		{
			name: "assigned Symbol.match",
			src: `RegExp.prototype[Symbol.match] = 0;
				"abab".replaceAll(/b/, "x")`,
			want: "axab",
		},
		{
			name: "exec changed during replace",
			src: `var re = /b/g, n = 0;
				Object.defineProperty(re, "exec", {get() {
					if (++n < 3) return RegExp.prototype.exec;
					return function (s) { log.push("exec"); return n === 3 ? {0: "c", index: 5, length: 1} : null; };
				}});
				["abcabc".replace(re, "[$&]"), log.join()].join(" ")`,
			want: "a[b]ca[b][c] exec,exec",
		},
		{
			name: "prototype modified during split",
			src: `var re = /,/;
				var r = ["a,b;c".split(re, {valueOf() { re.compile(";"); return 10; }})];
				r.push("a,b".split(/,/, {valueOf() { RegExp.prototype.exec = function () { log.push("exec"); return null; }; return 10; }}));
				r.push(log.length > 0);
				r.join(" ")`,
			want: "a,b;c a,b true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, "test", warm+tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRegExpLiteralLending tests that a regular expression literal passed to a String method, or
// whose exec or test method is called, evaluates to a new RegExp object each time wherever script
// code can see the object.
func TestRegExpLiteralLending(t *testing.T) {
	// seen collects the RegExp objects that script code receives. check reports how many
	// distinct ones there are, which is the number of evaluations of literals that reach script
	// code when each makes a new object.
	const prelude = `var seen = [];
		function check() {
			return new Set(seen).size + " " + seen.every(function (r) { return r instanceof RegExp; });
		}
		var exec = RegExp.prototype.exec;
		// patchExec makes the next call of exec record its RegExp object.
		function patchExec() {
			RegExp.prototype.exec = function (s) { seen.push(this); RegExp.prototype.exec = exec; return exec.call(this, s); };
		}
		`
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "assigned exec",
			src: `RegExp.prototype.exec = function (s) { seen.push(this); return null; };
				for (var i = 0; i < 2; i++) {
					"ab".replace(/b/g, "x"); "ab".replace(/b/, function () { return "x"; });
					"ab".match(/b/); "ab".search(/b/);
				}
				check()`,
			want: "8 true",
		},
		{
			name: "assigned String.prototype.replace",
			src: `String.prototype.replace = function (re) { seen.push(re); return re.lastIndex++; };
				var r = [];
				for (var i = 0; i < 2; i++) r.push("ab".replace(/b/y, "x"));
				r.join() + " " + check()`,
			want: "0,0 2 true",
		},
		{
			name: "symbol methods",
			src: `var r = [];
				[Symbol.replace, Symbol.match, Symbol.search, Symbol.split].forEach(function (k) {
					var f = RegExp.prototype[k];
					Object.defineProperty(RegExp.prototype, k, {get() { seen.push(this); return f; }});
					for (var i = 0; i < 2; i++) r.push("ab".replace(/b/g, "x"), "ab".match(/b/)[0], "ab".search(/b/), "a,b".split(/,/));
					Object.defineProperty(RegExp.prototype, k, {value: f});
					r.push(check());
					seen = [];
				});
				r.join(" ")`,
			want: "ax b 1 a,b ax b 1 a,b 2 true ax b 1 a,b ax b 1 a,b 4 true ax b 1 a,b ax b 1 a,b 2 true ax b 1 a,b ax b 1 a,b 2 true",
		},
		{
			name: "assigned Symbol.replace",
			src: `RegExp.prototype[Symbol.replace] = function (s, v) { seen.push(this); return this.lastIndex; };
				var r = [];
				for (var i = 0; i < 2; i++) r.push("ab".replace(/b/g, "x"));
				r.join() + " " + check()`,
			want: "0,0 2 true",
		},
		{
			name: "replacer",
			src: `function f(s, depth) {
					return s.replace(/(b)/g, function (m, p1, pos) {
						"use strict";
						var inner = depth > 0 ? f("abab", depth - 1) : "";
						return "[" + (this === undefined) + arguments.length + pos + inner + "]";
					});
				}
				f("abab", 1)`,
			want: "a[true41a[true41]a[true43]]a[true43a[true41]a[true43]]",
		},
		{
			name: "subject converted by script code",
			src: `var s = new String("ab");
				s.toString = function () { patchExec(); return "ab"; };
				for (var i = 0; i < 2; i++) { s.replace(/b/g, "x"); s.match(/b/); }
				check()`,
			want: "4 true",
		},
		{
			name: "other arguments converted by script code",
			src: `var v = {toString() { patchExec(); return "x"; }, valueOf() { patchExec(); return 5; }};
				var r = [];
				for (var i = 0; i < 2; i++) r.push("ab".replace(/b/g, v), "a,b".split(/,/, v));
				r.join(" ") + " " + check()`,
			want: "ax a,b ax a,b 4 true",
		},
		{
			name: "callee that is not the String method",
			src: `function f(x) { return x.replace(/b/g, "x"); }
				var o = {replace(re) { return re; }};
				var a = f(o);
				a.lastIndex = 7;
				var r = [f("abab"), f("abab")];
				seen.push(a, f(o));
				r.join() + " " + a.lastIndex + " " + seen[1].lastIndex + " " + check()`,
			want: "axax,axax 7 0 2 true",
		},
		{
			name: "sticky and global literals in loops",
			src: `var r = [];
				for (var i = 0; i < 3; i++) r.push("aaa".replace(/a/y, "b"), "aXaX".match(/X/g).length, "aXa".search(/a/y), "aXa".split(/a/y).length);
				r.join()`,
			want: "baa,2,0,3,baa,2,0,3,baa,2,0,3",
		},
		{
			name: "method getter",
			src: `var test = RegExp.prototype.test;
				Object.defineProperty(RegExp.prototype, "test", {get() { seen.push(this); return test; }});
				var r = [];
				for (var i = 0; i < 2; i++) r.push(/b/.test("ab"));
				r.join() + " " + check()`,
			want: "true,true 2 true",
		},
		{
			name: "method found through a proxy",
			src: `delete RegExp.prototype.exec;
				Object.setPrototypeOf(RegExp.prototype, new Proxy({}, {get(t, k, r) { seen.push(r); return function () { return 1; }; }}));
				var r = [];
				for (var i = 0; i < 2; i++) r.push(/b/.exec("ab"));
				r.join() + " " + check()`,
			want: "1,1 2 true",
		},
		{
			name: "assigned test",
			src: `RegExp.prototype.test = function (s) { seen.push(this); return this.lastIndex++; };
				var r = [];
				for (var i = 0; i < 2; i++) r.push(/b/y.test("ab"));
				r.join() + " " + check()`,
			want: "0,0 2 true",
		},
		{
			name: "receiver used by script code",
			src: `var s = {toString() { patchExec(); return "ab"; }};
				var r = [];
				for (var i = 0; i < 2; i++) r.push(/b/.test(s), /b/.test((patchExec(), "ab")));
				r.join() + " " + check()`,
			want: "true,true,true,true 4 true",
		},
		{
			name: "receiver of a call that evaluates the literal again",
			src: `var out = [];
				function f(depth) { out.push(/a/g.test(depth > 0 ? (f(depth - 1), "a") : "a"), /a/y.exec("aa").index); }
				f(2);
				out.join()`,
			want: "true,0,true,0,true,0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, "test", prelude+tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDetachImmutableArrayBuffer tests that $262.detachArrayBuffer refuses an immutable buffer.
func TestDetachImmutableArrayBuffer(t *testing.T) {
	var vm ekuma.VM
	b, err := runScript(t.Context(), &vm, "test", "new ArrayBuffer(1).transferToImmutable()")
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if err := ekuma.DetachArrayBuffer(&vm, b); err == nil {
		t.Errorf("DetachArrayBuffer: got no error")
	}
	if got, err := b.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached: got %v, %v, want false", got, err)
	}
}

// TestAtomicsNotifyWaitAsync checks that Atomics.notify wakes the waiters of its index in
// order, as many as its count, and returns how many it woke.
func TestAtomicsNotifyWaitAsync(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "notify", `
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
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "2,0,1,0"; got != want {
		t.Errorf("notify results: got %q, want %q", got, want)
	}
	v, err = runScript(t.Context(), &vm, "log", `log.join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "0ok,1ok,2ok"; got != want {
		t.Errorf("resolutions: got %q, want %q", got, want)
	}
}

// TestAtomicsWaitAsyncTimeout checks that a finite timeout resolves the promise with
// "timed-out" when the jobs run after it expired.
func TestAtomicsWaitAsyncTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		_, err := runScript(t.Context(), &vm, "wait", `
			var log = [];
			var ia = new Int32Array(new SharedArrayBuffer(16));
			Atomics.waitAsync(ia, 0, 0, 1).value.then(r => log.push("short " + r));
			Atomics.waitAsync(ia, 0, 0).value.then(r => log.push("long " + r));
		`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		// The fake clock of the bubble reaches the deadline.
		time.Sleep(time.Millisecond)
		v, err := runScript(t.Context(), &vm, "expire", `log.join()`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), ""; got != want {
			t.Errorf("log before the jobs ran: got %q, want %q", got, want)
		}
		// The timed-out waiter is no longer in the list.
		v, err = runScript(t.Context(), &vm, "notify", `String(Atomics.notify(ia, 0))`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "1"; got != want {
			t.Errorf("notify: got %q, want %q", got, want)
		}
		v, err = runScript(t.Context(), &vm, "log", `log.join()`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if got, want := v.String(), "short timed-out,long ok"; got != want {
			t.Errorf("log: got %q, want %q", got, want)
		}
	})
}

// skipAgents skips a test that runs agents on several goroutines, which a single thread cannot.
func skipAgents(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "js" {
		t.Skip("agents need threads")
	}
}

// runIn runs src in a new VM on another goroutine, with the global sab sharing the data block of
// the SharedArrayBuffer v, and returns the result as a string.
func runIn(t *testing.T, v ekuma.Value, src string) string {
	t.Helper()
	blk := v.SharedDataBlock()
	var got string
	var err error
	var wg sync.WaitGroup
	wg.Go(func() {
		var vm ekuma.VM
		if err = vm.Global().Set("sab", blk); err != nil {
			return
		}
		var r ekuma.Value
		r, err = runScript(context.Background(), &vm, "agent", src)
		got = r.String()
	})
	wg.Wait()
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	return got
}

func TestAtomicsWaitCannotBlock(t *testing.T) {
	var vm ekuma.VM
	vm.SetCanBlock(true)
	vm.SetCanBlock(false)
	got := run(t, &vm, `try { Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0); "returned"; } catch (e) { e.constructor.name; }`)
	if want := "TypeError"; got != want {
		t.Errorf("Atomics.wait: got %s, want %s", got, want)
	}
}

func TestSharedDataBlockOfNonShared(t *testing.T) {
	var vm ekuma.VM
	ab, err := runScript(t.Context(), &vm, "ab", `new ArrayBuffer(4)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		r := recover()
		var verr *ekuma.ValueError
		if err, ok := r.(error); !ok || !errors.As(err, &verr) {
			t.Errorf("SharedDataBlock of an ArrayBuffer: got panic %v, want a *ValueError", r)
		}
	}()
	ab.SharedDataBlock()
}

func TestAtomicsWaitInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		vm.SetCanBlock(true)
		run(t, &vm, `globalThis.ta = new Int32Array(new SharedArrayBuffer(4));`)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := runScript(ctx, &vm, "wait", `Atomics.wait(ta, 0, 0)`); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("runScript: got %v, want %v", err, context.DeadlineExceeded)
		}
		// The interrupted wait left its waiter list.
		if got, want := run(t, &vm, `Atomics.notify(ta, 0)`), "0"; got != want {
			t.Errorf("Atomics.notify: got %s, want %s", got, want)
		}
	})
}

func TestAtomicsWaitAsyncNotifiedByAnotherVM(t *testing.T) {
	skipAgents(t)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "wait", `
globalThis.result = "pending";
const sab = new SharedArrayBuffer(8);
Atomics.waitAsync(new Int32Array(sab), 1, 0).value.then(v => { result = v; });
sab`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runIn(t, sab, `String(Atomics.notify(new Int32Array(sab), 1))`), "1"; got != want {
		t.Errorf("Atomics.notify: got %s, want %s", got, want)
	}
	ok, err := vm.AwaitJobs(t.Context())
	if err != nil || !ok {
		t.Fatalf("AwaitJobs: got %t, %v, want true, nil", ok, err)
	}
	result, err := vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "ok"; got != want {
		t.Errorf("result: got %s, want %s", got, want)
	}
	// The resolved waiter is gone, so nothing is left to wait for.
	if ok, err := vm.AwaitJobs(t.Context()); ok || err != nil {
		t.Errorf("AwaitJobs without waiters: got %t, %v, want false, nil", ok, err)
	}
}

// TestNativeRecursion checks that recursion through native functions, which runs no
// interpreter loop, throws a RangeError.
func TestNativeRecursion(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
	}{
		{
			name: "native calls",
			src: `var f = x => x;
				for (let i = 0; i < 20000; i++) f = Function.prototype.call.bind(f, null);
				f(1)`,
		},
		{
			name: "iterator steps",
			src: `var it = [1].values();
				for (let i = 0; i < 20000; i++) it = it.map(Math.abs);
				it.next()`,
		},
		{
			name: "host functions",
			src:  `invoke(invoke)`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			invoke := vm.FuncOf("invoke", func(this ekuma.Value, args []ekuma.Value) (any, error) {
				return args[0].Invoke(t.Context(), args[0])
			})
			if err := vm.Global().Set("invoke", invoke); err != nil {
				t.Fatal(err)
			}
			_, err := runScript(t.Context(), &vm, "recursion", tt.src)
			var ex *ekuma.Error
			if !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "RangeError") {
				t.Errorf("got %v, want a RangeError", err)
			}
		})
	}
}

// TestAtomicsWaitAsyncDroppedVM checks that a VM with a pending waiter of Atomics.waitAsync is
// collected while the data block that it waits on lives on.
func TestAtomicsWaitAsyncDroppedVM(t *testing.T) {
	var owner ekuma.VM
	sab, err := runScript(t.Context(), &owner, "sab", `new SharedArrayBuffer(4)`)
	if err != nil {
		t.Fatal(err)
	}
	blk := sab.SharedDataBlock()
	dropped := func() weak.Pointer[ekuma.VM] {
		vm := new(ekuma.VM)
		if err := vm.Global().Set("sab", blk); err != nil {
			t.Fatal(err)
		}
		run(t, vm, `Atomics.waitAsync(new Int32Array(sab), 0, 0).value.then(() => {});`)
		return weak.Make(vm)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for dropped.Value() != nil {
		if time.Now().After(deadline) {
			t.Fatal("the dropped VM was not collected")
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	if got, want := runIn(t, sab, `String(Atomics.notify(new Int32Array(sab), 0))`), "0"; got != want {
		t.Errorf("Atomics.notify: got %s, want %s", got, want)
	}
}

func TestAwaitJobsCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var vm ekuma.VM
		run(t, &vm, `Atomics.waitAsync(new Int32Array(new SharedArrayBuffer(4)), 0, 0);`)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := vm.AwaitJobs(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("AwaitJobs: got %v, want %v", err, context.DeadlineExceeded)
		}
	})
}

func TestAwaitJobsWhileRunning(t *testing.T) {
	var vm ekuma.VM
	var awaitErr error
	f := vm.FuncOf("awaitJobs", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		_, awaitErr = vm.AwaitJobs(t.Context())
		return nil, nil
	})
	if err := vm.Global().Set("awaitJobs", f); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `awaitJobs();`)
	if !errors.Is(awaitErr, ekuma.ErrRunning) {
		t.Errorf("AwaitJobs: got %v, want %v", awaitErr, ekuma.ErrRunning)
	}
}

func TestAwaitJobsRunsFinalization(t *testing.T) {
	var vm ekuma.VM
	run(t, &vm, `
globalThis.held = "pending";
globalThis.registry = new FinalizationRegistry(h => { held = h; });
registry.register({}, "cleaned");`)
	// The Go runtime queues the cleanups on its own goroutine some time after a collection.
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
	held, err := vm.Global().Get("held")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := held.String(), "cleaned"; got != want {
		t.Errorf("held: got %s, want %s", got, want)
	}
}

func TestSharedArrayBufferGrownByAnotherVM(t *testing.T) {
	skipAgents(t)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "grow", `
const sab = new SharedArrayBuffer(4, {maxByteLength: 16});
globalThis.ta = new Int32Array(sab);
sab`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runIn(t, sab, `sab.grow(12); new Int32Array(sab)[2] = 7; String(sab.byteLength)`), "12"; got != want {
		t.Errorf("byteLength in the other VM: got %s, want %s", got, want)
	}
	if got, want := run(t, &vm, `[ta.buffer.byteLength, ta.length, ta[2]].join()`), "12,3,7"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAtomicsAcrossVMs(t *testing.T) {
	skipAgents(t)
	const (
		agents = 4
		n      = 2000
	)
	var vm ekuma.VM
	sab, err := runScript(t.Context(), &vm, "main", `globalThis.ta = new Int32Array(new SharedArrayBuffer(4 * (2 + 4))); ta.buffer`)
	if err != nil {
		t.Fatal(err)
	}
	// Each agent counts with Atomics.add and with a compareExchange loop, and writes its own
	// element without atomics.
	src := `
const ta = new Int32Array(sab);
for (let i = 0; i < ` + strconv.Itoa(n) + `; i++) {
	Atomics.add(ta, 0, 1);
	for (;;) {
		const old = Atomics.load(ta, 1);
		if (Atomics.compareExchange(ta, 1, old, old + 1) === old) {
			break;
		}
	}
	ta[2 + id] = i;
}`
	blk := sab.SharedDataBlock()
	var wg sync.WaitGroup
	errs := make([]error, agents)
	for id := range agents {
		wg.Go(func() {
			var avm ekuma.VM
			if err := avm.Global().Set("sab", blk); err != nil {
				errs[id] = err
				return
			}
			if err := avm.Global().Set("id", id); err != nil {
				errs[id] = err
				return
			}
			_, errs[id] = runScript(context.Background(), &avm, "agent", src)
		})
	}
	wg.Wait()
	for id, err := range errs {
		if err != nil {
			t.Fatalf("agent %d: %v", id, err)
		}
	}
	want := strconv.Itoa(agents*n) + "," + strconv.Itoa(agents*n)
	for range agents {
		want += "," + strconv.Itoa(n-1)
	}
	if got := run(t, &vm, `[...ta].join()`); got != want {
		t.Errorf("got %s, want %s", got, want)
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
	var vm ekuma.VM
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
		vm.SetTimeZone(tc.zone)
		v, err := runScript(t.Context(), &vm, "date", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestTemporalTimeZone checks that Temporal takes the system time zone from VM.SetTimeZone, both
// as Temporal.Now.timeZoneId and as a named time zone, with its transitions.
func TestTemporalTimeZone(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(daylightSavingZone(t, zoneType{offset: 1 * 3600, abbr: "CET"}, zoneType{offset: 2 * 3600, abbr: "CEST"},
		1616893200, // 2021-03-28T01:00:00Z
		1635642000, // 2021-10-31T01:00:00Z
	))
	tests := []struct {
		src  string
		want string
	}{
		{
			src:  `Temporal.Now.timeZoneId()`,
			want: "Test/CET",
		},
		{
			src:  `Temporal.Now.zonedDateTimeISO().timeZoneId`,
			want: "Test/CET",
		},
		{
			// A skipped local time moves forward by the length of the gap.
			src:  `Temporal.ZonedDateTime.from("2021-03-28T02:30[test/cet]").toString()`,
			want: "2021-03-28T03:30:00+02:00[Test/CET]",
		},
		{
			src:  `Temporal.ZonedDateTime.from("2021-06-01T12:00[Test/CET]").getTimeZoneTransition("next").toString()`,
			want: "2021-10-31T02:00:00+01:00[Test/CET]",
		},
		{
			src:  `Temporal.PlainDateTime.from("2021-10-31T02:30").toZonedDateTime("Test/CET", { disambiguation: "later" }).offset`,
			want: "+01:00",
		},
	}
	for _, tc := range tests {
		v, err := runScript(t.Context(), &vm, "temporal", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestTemporalCalendars checks the calendars other than ISO 8601 that package intl adds to
// Temporal. The expected values are those of V8 with ICU4X.
func TestTemporalCalendars(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{
			src: `const d = Temporal.PlainDate.from("2024-03-15[u-ca=hebrew]");
				[d.era, d.eraYear, d.year, d.monthCode, d.day, d.weekOfYear, d.monthsInYear].join(" ")`,
			want: "am 5784 5784 M06 5  13",
		},
		{
			src:  `Temporal.PlainDate.from({calendar: "gregory", era: "bce", eraYear: 1, month: 1, day: 1}).year`,
			want: "0",
		},
		{
			// An era and an era year replace the year.
			src:  `Temporal.PlainDate.from("2024-03-15[u-ca=gregory]").with({era: "bce", eraYear: 1}).toString()`,
			want: "0000-03-15[u-ca=gregory]",
		},
		{
			// A month day string keeps the month code and the day of its date in the calendar.
			src:  `Temporal.PlainMonthDay.from("2023-01-01[u-ca=hebrew]").toString()`,
			want: "1972-12-13[u-ca=hebrew]",
		},
		{
			src:  `new Intl.DateTimeFormat("en-u-ca-islamic").resolvedOptions().calendar`,
			want: "islamic-tbla",
		},
	}
	for _, tc := range tests {
		var vm ekuma.VM
		v, err := runScript(t.Context(), &vm, "calendar", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestTemporalNamespace checks that the Temporal namespace, whose contents are created when
// they are first needed, looks the same to operations that come first.
func TestTemporalNamespace(t *testing.T) {
	tests := []struct {
		src  string
		want string
	}{
		{
			src:  `Temporal.x = 1; Object.keys(Temporal).join() + " " + Object.getOwnPropertyNames(Temporal).indexOf("x")`,
			want: "x 9",
		},
		{
			src:  `Object.defineProperty(Temporal, "x", { value: 1 }); Object.getOwnPropertyNames(Temporal).slice(-2).join()`,
			want: "Now,x",
		},
		{
			src:  `delete Temporal.Now; "Now" in Temporal`,
			want: "false",
		},
		{
			src:  `Object.freeze(Temporal); [Object.isFrozen(Temporal), typeof Temporal.Instant].join()`,
			want: "true,function",
		},
		{
			src:  `new Date(0).toTemporalInstant() instanceof Temporal.Instant`,
			want: "true",
		},
		{
			src:  `Object.prototype.toString.call(Temporal) + " " + JSON.stringify(Temporal)`,
			want: "[object Temporal] {}",
		},
	}
	for _, tc := range tests {
		var vm ekuma.VM
		v, err := runScript(t.Context(), &vm, "temporal", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, &vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}

// TestProxy covers the behaviour of proxies that test262 does not pin down: the paths that
// look properties up without the internal methods, and what proxies do to them.
func TestProxy(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a proxy installed as a prototype after a load site cached a hit",
			src: `function f(o) { return o.x; }
				var proto = {};
				var o = Object.create(proto);
				o.y = 1;
				var mid = Object.create(Object.prototype);
				mid.x = "mid";
				Object.setPrototypeOf(proto, mid);
				var before = f(o);
				Object.setPrototypeOf(proto, new Proxy({}, { get(t, k, r) { return k + ":" + (r === o); } }));
				before + " " + f(o)`,
			want: "mid x:true",
		},
		{
			name: "a proxy installed as a prototype after a load site cached a miss",
			src: `function f(o) { return o.x; }
				var proto = {};
				var o = Object.create(proto);
				var before = f(o);
				Object.setPrototypeOf(proto, new Proxy({}, { get() { return "trap"; } }));
				before + " " + f(o)`,
			want: "undefined trap",
		},
		{
			name: "a proxy installed as a prototype after a store site cached an addition",
			src: `var log = "";
				function g(o) { o.y = 1; }
				var proto = {};
				g(Object.create(proto));
				Object.setPrototypeOf(proto, new Proxy({}, { set(t, k, v, r) { log += "set:" + k; return true; } }));
				var o = Object.create(proto);
				g(o);
				log + " " + Object.hasOwn(o, "y")`,
			want: "set:y false",
		},
		{
			name: "appending to an array whose prototype inherits from a proxy",
			src: `var log = "";
				var arr = [1, 2];
				arr[2] = 3;
				Object.setPrototypeOf(Array.prototype, new Proxy({}, { set(t, k, v, r) { log += "set:" + k; return true; } }));
				arr[3] = 4;
				Object.setPrototypeOf(Array.prototype, Object.prototype);
				log + " " + arr.length`,
			want: "set:3 3",
		},
		{
			name: "stores to primitives reach a proxy on the prototype chain",
			src: `var log = "";
				Object.setPrototypeOf(Number.prototype, new Proxy({}, {
					set(t, k, v, r) { log += k + ":" + typeof r + " "; return true; },
				}));
				(1).x = 1;
				(1)[2] = 1;
				Object.setPrototypeOf(Number.prototype, Object.prototype);
				log`,
			want: "x:number 2:number ",
		},
		{
			name: "an index store to a primitive calls an inherited setter",
			src: `var log = "";
				Object.defineProperty(Object.prototype, "5", { set(v) { log += "setter:" + v; }, configurable: true });
				(1)[5] = "a";
				"abcdefg"[5] = "b";
				delete Object.prototype[5];
				log`,
			want: "setter:a",
		},
		{
			name: "global variables with a proxy on the prototype chain of the global object",
			src: `var log = [];
				var target = { inherited: 1 };
				Object.setPrototypeOf(globalThis, new Proxy(target, {
					has(t, k) { if (typeof k === "string") log.push("has:" + k); return Reflect.has(t, k); },
					get(t, k, r) { if (typeof k === "string") log.push("get:" + k); return Reflect.get(t, k, r); },
					set(t, k, v, r) { log.push("set:" + k); return true; },
				}));
				inherited;
				typeof missing;
				inherited = 2;
				(function() { "use strict"; inherited = 3; })();
				log.join()`,
			want: "has:inherited,has:inherited,get:inherited,has:missing,has:inherited,has:inherited,set:inherited,has:inherited,has:inherited,set:inherited",
		},
		{
			name: "super property accesses with a proxy as the prototype of the home object",
			src: `var log = [];
				var proto = new Proxy({}, {
					get(t, k, r) { log.push("get:" + k + ":" + (r === obj)); return 1; },
					set(t, k, v, r) { log.push("set:" + k + ":" + (r === obj)); return true; },
				});
				var obj = { __proto__: proto, m() { super.y = 2; return super.x; } };
				obj.m() + " " + log.join()`,
			want: "1 set:y:true,get:x:true",
		},
		{
			name: "for-in over a proxy",
			src: `var log = [];
				var target = Object.create({ a: 1, b: 2, d: 5 });
				Object.defineProperty(target, "a", { value: 0, enumerable: false });
				target.b = 3;
				target.c = 4;
				var p = new Proxy(target, {
					ownKeys(t) { log.push("ownKeys"); return Reflect.ownKeys(t); },
					getOwnPropertyDescriptor(t, k) { log.push("gopd:" + k); return Reflect.getOwnPropertyDescriptor(t, k); },
					getPrototypeOf(t) { log.push("getPrototypeOf"); return Reflect.getPrototypeOf(t); },
				});
				var keys = [];
				for (var k in p) keys.push(k);
				keys.join() + " " + log.join()`,
			want: "b,c,d ownKeys,gopd:a,gopd:b,gopd:c,getPrototypeOf",
		},
		{
			name: "instanceof with proxies",
			src: `var log = [];
				function F() {}
				var P = new Proxy(F, { get(t, k, r) { log.push(String(k)); return Reflect.get(t, k, r); } });
				var q = new Proxy({}, { getPrototypeOf(t) { log.push("getPrototypeOf"); return F.prototype; } });
				(new F() instanceof P) + " " + (q instanceof F) + " " + log.join()`,
			want: "true true Symbol(Symbol.hasInstance),prototype,getPrototypeOf",
		},
		{
			name: "class fields on a proxy receiver",
			src: `var log = [];
				class Base {
					constructor() {
						return new Proxy({}, { defineProperty(t, k, d) { log.push(k); return Reflect.defineProperty(t, k, d); } });
					}
				}
				class C extends Base {
					x = 1;
					#y = 2;
					static y(o) { return o.#y; }
					static hasY(o) { return #y in o; }
				}
				var c = new C();
				log.join() + " " + C.y(c) + " " + C.hasY(c) + " " + Reflect.ownKeys(c).join()`,
			want: "x 2 true x",
		},
		{
			name: "private fields are no own keys",
			src: `class A {
					#x = 1;
					static setX(o) { o.#x = 2; return o.#x; }
				}
				var a = new A();
				Object.freeze(a);
				var keys = [];
				Object.getOwnPropertySymbols(new Proxy(a, { ownKeys(t) { var k = Reflect.ownKeys(t); keys = k; return k; } }));
				A.setX(a) + " " + keys.length`,
			want: "2 0",
		},
		{
			name: "a call through a proxy adds no line to stack traces",
			src: `function f() { throw new Error(); }
				function stack(g) { try { g(); } catch (e) { return e.stack.split("\n").length; } }
				var p = new Proxy(f, {});
				stack(function() { p(); }) === stack(function() { f(); })`,
			want: "true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProxyDeepChains checks that internal methods that recurse through proxies without
// running JavaScript end in a RangeError.
func TestProxyDeepChains(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "a chain of proxies",
			src: `var p = {};
				for (var i = 0; i < 100000; i++) p = new Proxy(p, {});
				p.x`,
		},
		{
			name: "a proxy on the prototype chain of its target",
			src: `var t = {};
				var p = new Proxy(t, {});
				Object.setPrototypeOf(t, p);
				"x" in p`,
		},
		{
			name: "a chain of callable proxies",
			src: `var f = function() {};
				for (var i = 0; i < 100000; i++) f = new Proxy(f, {});
				f()`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, `try { `+tt.src+`; "no error" } catch (e) { e instanceof RangeError }`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != "true" {
				t.Errorf("got %q, want a RangeError", got)
			}
		})
	}
}

// memoryModules loads the modules of sources by name, ignoring the referrer, and keeps one
// record per name. loads counts the calls of the loader for each name.
type memoryModules struct {
	vm      *ekuma.VM
	sources map[string]string
	modules map[string]*ekuma.Module
	loads   map[string]int
}

func newMemoryModules(vm *ekuma.VM, sources map[string]string) *memoryModules {
	l := &memoryModules{
		vm:      vm,
		sources: sources,
		modules: map[string]*ekuma.Module{},
		loads:   map[string]int{},
	}
	vm.SetModuleLoader(l.load)
	return l
}

func (l *memoryModules) load(req ekuma.ModuleRequest) (*ekuma.Module, error) {
	specifier := req.Specifier
	l.loads[specifier]++
	if m, ok := l.modules[specifier]; ok {
		return m, nil
	}
	src, ok := l.sources[specifier]
	if !ok {
		return nil, fmt.Errorf("no module %q", specifier)
	}
	m, err := l.vm.ParseModule(specifier, src)
	if err != nil {
		return nil, err
	}
	l.modules[specifier] = m
	return m, nil
}

// run runs the module name and returns the value of the global variable result
// after the evaluation completed.
func (l *memoryModules) run(t *testing.T, name string) ekuma.Value {
	t.Helper()
	m, err := l.load(ekuma.ModuleRequest{Specifier: name})
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Fatalf("the evaluation is %v: %v", st, r)
	}
	v, err := l.vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestModules(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{
			// An import is a live binding: it sees the later assignments of the exporter.
			name: "live binding",
			sources: map[string]string{
				"main": `import { count, increment } from "counter";
					const seen = [count];
					increment();
					seen.push(count);
					globalThis.result = seen.join();`,
				"counter": `export let count = 0;
					export function increment() { count++; }`,
			},
			want: "0,1",
		},
		{
			// a finished its asynchronous evaluation as a dependency of c before import()
			// asks to evaluate it.
			name: "import of an asynchronous module evaluated as a dependency",
			sources: map[string]string{
				"main": `import "c";
					globalThis.result = String((await import("a")).x);`,
				"c": `import "a";`,
				"a": `export let x = 1; await null; x = 2;`,
			},
			want: "2",
		},
		{
			// b runs first and calls a function of a before a's body ran: the function was
			// instantiated when a was linked.
			name: "cycle with a hoisted function",
			sources: map[string]string{
				"a": `import { fromB } from "b";
					export function greet(s) { return "hello " + s; }
					globalThis.result = fromB;`,
				"b": `import { greet } from "a";
					export const fromB = greet("b");`,
			},
			want: "hello b",
		},
		{
			// A module that two modules import, and that a dynamic import loads again,
			// runs once, and all of them see the same namespace object.
			name: "identity",
			sources: map[string]string{
				"main": `import * as ns from "shared";
					import "other";
					globalThis.result = import("shared").then(ns2 =>
						[globalThis.runs, ns === ns2, ns === globalThis.fromOther].join());`,
				"other": `import * as ns from "shared";
					globalThis.fromOther = ns;`,
				"shared": `globalThis.runs = (globalThis.runs ?? 0) + 1;`,
			},
			want: "1,true,true",
		},
		{
			// The decorators of an exported class may precede or follow export.
			name: "decorated exports",
			sources: map[string]string{
				"main": `import D, { A, B } from "dep";
					globalThis.result = [A.tag, B.tag, D.tag].join();`,
				"dep": `function tag(v, c) { v.tag = c.name; }
					@tag export class A {}
					export @tag class B {}
					export default @tag class {}`,
			},
			want: "A,B,default",
		},
		{
			// The modules of a dynamic import evaluate after the importing module.
			name: "dynamic import",
			sources: map[string]string{
				"main": `const log = globalThis.log = [];
					const p = import("dep").then(ns => log.push(ns.default));
					log.push("main");
					globalThis.result = p.then(() => log.join());`,
				"dep": `export default "dep";`,
			},
			want: "main,dep",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			l := newMemoryModules(&vm, tt.sources)
			names := []string{"main", "a"}
			name := names[0]
			if _, ok := tt.sources[name]; !ok {
				name = names[1]
			}
			v := l.run(t, name)
			if v.Type() == ekuma.TypeObject {
				// A promise of the result.
				var got ekuma.Value
				f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
					got = args[0]
					return nil, nil
				})
				if _, err := v.Call(t.Context(), "then", f); err != nil {
					t.Fatal(err)
				}
				v = got
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestModuleLoadedOnce checks that the loader is asked once for a request of a referrer
// that loaded.
func TestModuleLoadedOnce(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = Promise.all([import("dep"), import("dep")]).then(([a, b]) => a === b);`,
		"dep":  `export {};`,
	})
	v := l.run(t, "main")
	var same ekuma.Value
	f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		same = args[0]
		return nil, nil
	})
	if _, err := v.Call(t.Context(), "then", f); err != nil {
		t.Fatal(err)
	}
	if !same.Truthy() {
		t.Errorf("the two imports gave different namespace objects")
	}
	if got := l.loads["dep"]; got != 1 {
		t.Errorf("the loader was called %d times for dep, want 1", got)
	}
}

// TestDynamicImportLoadError checks that an error of the loader rejects the promise of a
// dynamic import with the thrown value, and that a later import asks the loader again.
func TestDynamicImportLoadError(t *testing.T) {
	var vm ekuma.VM
	thrown, err := runScript(t.Context(), &vm, "thrown", `({})`)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		return nil, &ekuma.Error{Value: thrown}
	})
	if err := vm.Global().Set("thrown", thrown); err != nil {
		t.Fatal(err)
	}
	if _, err := runScript(t.Context(), &vm, "main", `
		var result = [];
		import("missing").catch(e => result.push(e === thrown));
		import("missing").catch(e => result.push(e === thrown));`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result.join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
}

// TestModuleReferrer checks that the loader gets the name of the script or module that
// contains the import, including from eval code and from a function made by Function.
func TestModuleReferrer(t *testing.T) {
	var vm ekuma.VM
	var referrers []string
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		referrers = append(referrers, req.ReferrerName+":"+req.Specifier)
		if req.Specifier == "mod" {
			return vm.ParseModule("mod", `import("fromModule"); eval('import("fromEval")');`)
		}
		return nil, errors.New("no module")
	})
	if _, err := runScript(t.Context(), &vm, "script", `
		import("mod");
		(0, eval)('import("fromIndirectEval")');
		Function('import("fromFunction")')();`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"script:mod",
		"script:fromIndirectEval",
		"script:fromFunction",
		"mod:fromModule",
		"mod:fromEval",
	}
	if fmt.Sprint(referrers) != fmt.Sprint(want) {
		t.Errorf("got %q, want %q", referrers, want)
	}
}

// TestShadowRealmImportValue checks that each ShadowRealm loads a module for itself, once,
// in its own realm.
func TestShadowRealmImportValue(t *testing.T) {
	var vm ekuma.VM
	realms := map[*ekuma.Realm]bool{}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		if req.Realm == vm.InitialRealm() {
			t.Errorf("the request of a ShadowRealm has the initial realm")
		}
		realms[req.Realm] = true
		return req.Realm.ParseModule(req.Specifier, `globalThis.count = (globalThis.count ?? 0) + 1; export const count = globalThis.count;`)
	})
	if _, err := runScript(t.Context(), &vm, "main", `
		var result;
		const a = new ShadowRealm(), b = new ShadowRealm();
		Promise.all([a.importValue("mod", "count"), a.importValue("mod", "count"), b.importValue("mod", "count")]).then(v => result = v.join());`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1,1,1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
	if len(realms) != 2 {
		t.Errorf("the requests had %d realms, want 2", len(realms))
	}
}

// TestShadowRealmCancel checks that an interruption in a ShadowRealm is not caught as an
// error, and leaves the realm of the script current.
func TestShadowRealmCancel(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "evaluate",
			src:  `r.evaluate("for (;;) {}")`,
		},
		{
			name: "wrapped function",
			src:  `r.evaluate("() => { for (;;) {} }")()`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			// The deadline may pass before the loop starts on a slow machine, so the variables
			// are declared without it.
			if _, err := runScript(t.Context(), &vm, "setup", `var caught = false; var r = new ShadowRealm();`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			_, err := runScript(ctx, &vm, "loop", `try { `+tt.src+` } catch (e) { caught = true; }`)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			v, err := runScript(t.Context(), &vm, "after", `typeof caught + " " + caught`)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := v.String(), "boolean false"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestImportMeta checks that import.meta has the properties that the host gives it.
func TestImportMeta(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = import.meta.url;`,
	})
	vm.SetImportMeta(func(m *ekuma.Module) map[string]any {
		return map[string]any{
			"url": "memory:main",
		}
	})
	if got, want := l.run(t, "main").String(), "memory:main"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestLongImportChain checks that loading, linking and evaluating a long chain of imports
// does not exhaust the Go stack.
func TestLongImportChain(t *testing.T) {
	const n = 20000
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		var i int
		if _, err := fmt.Sscanf(req.Specifier, "m%d", &i); err != nil {
			return nil, err
		}
		src := fmt.Sprintf(`export * from "m%d"; export const v%d = %d;`, i+1, i, i)
		if i == n {
			src = fmt.Sprintf(`export const v%d = %d;`, i, i)
		}
		return vm.ParseModule(req.Specifier, src)
	})
	m, err := vm.ParseModule("main", fmt.Sprintf(`import { v%d } from "m0"; globalThis.result = v%d;`, n, n))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := vm.Global().Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Int(); got != n {
		t.Errorf("got %d, want %d", got, n)
	}
}

// TestTailCalls covers the calls in tail position that test262 does not: what reaches the callee,
// and the calls that must still grow the stack.
func TestTailCalls(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "mutual recursion a million deep",
			src: `"use strict";
				function even(n) { return n === 0 ? true : odd(n - 1); }
				function odd(n) { return n === 0 ? false : even(n - 1); }
				even(1e6)`,
			want: "true",
		},
		{
			name: "more arguments than the frame of the caller holds",
			src: `"use strict";
				function small(n, acc) {
					return n === 0 ? acc : big(n, acc, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20);
				}
				function big(n, acc, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15, a16, a17, a18, a19, a20) {
					return small(n - 1, acc + a1 + a20);
				}
				small(1e5, 0)`,
			want: "2100000",
		},
		{
			name: "surplus arguments, arguments and rest parameters",
			src: `"use strict";
				function f(n) { return n === 0 ? arguments.length : g(n, 1, 2, 3); }
				function g(n, ...rest) { return f(n - 1, ...rest, rest.length); }
				f(1e5)`,
			want: "5",
		},
		{
			name: "bound function",
			src: `"use strict";
				var h = f.bind("t", 1);
				function f(k, n) { return n === 0 ? this + k : h(n - 1); }
				h(1e5)`,
			want: "t1",
		},
		{
			name: "call a million deep",
			src: `"use strict";
				function f(n, acc) { return n === 0 ? this.k + acc : f.call(this, n - 1, acc + 1); }
				f.call({k: 1}, 1e6, 0)`,
			want: "1000001",
		},
		{
			name: "call through a bound function",
			src: `"use strict";
				var c = Function.prototype.call.bind(f, "t");
				function f(n) { return n === 0 ? this : c(n - 1); }
				c(1e6)`,
			want: "t",
		},
		{
			name: "apply forwarding arguments a million deep",
			src: `"use strict";
				function f(n, acc) { return g.apply(this, arguments); }
				function g(n, acc, x) { return n === 0 ? acc + x : f(n - 1, acc + 1, x); }
				f(1e6, 0, "x")`,
			want: "1000000x",
		},
		{
			name: "apply with an array or no array a million deep",
			src: `"use strict";
				function f(n, acc) { return n === 0 ? acc : n % 2 ? f.apply(null, [n - 1, acc + 1]) : g.apply(n - 1); }
				function g() { return f(this, 0); }
				f(1e6, 0)`,
			want: "1",
		},
		{
			name: "apply with an array-like",
			src: `"use strict";
				var log = [];
				var args = { get length() { log.push("length"); return 2; }, get 0() { log.push(0); return 3; }, 1: 4 };
				function f(a, b) { log.push("f"); return a + b; }
				function g() { return f.apply(null, args); }
				var r = g();
				function h() { return f.apply(null, { get length() { throw "x"; } }); }
				var e = (function () { try { return h(); } catch (e) { return e; } })();
				log.join() + " " + r + " " + e`,
			want: "length,0,f 7 x",
		},
		{
			name: "apply that is not Function.prototype.apply",
			src: `"use strict";
				var o = { apply(t, args) { return args[0] === 0 ? args.length : f(args[0] - 1, 2); } };
				function f(n) { return o.apply(null, arguments); }
				f(1e6)`,
			want: "2",
		},
		{
			name: "spread arguments a million deep",
			src: `"use strict";
				function f(n) { return g(...arguments); }
				function g(n, a, b) { return n === 0 ? a + b : f(n - 1, a, b); }
				f(1e6, 1, 2)`,
			want: "3",
		},
		{
			name: "native function",
			src: `"use strict";
				function f(n) { return n === 0 ? Math.max(n, 7) : Reflect.apply(g, null, [n]); }
				function g(n) { return f(n - 1); }
				f(50)`,
			want: "7",
		},
		{
			name: "closure of a per-iteration binding",
			src: `"use strict";
				var fns = [];
				function loop() {
					for (let i = 0; i < 3; i++) {
						fns.push(() => i);
						if (i === 1) return done(7, 8, 9);
					}
				}
				function done(a, b, c) { var x = a, y = b, z = c; return fns.map(f => f()).join() + x + y + z; }
				loop()`,
			want: "0,1789",
		},
		{
			name: "constructor and generator",
			src: `"use strict";
				function g() { return 5; }
				function F() { this.x = 1; return g(); }
				function* gen() { yield 2; }
				function h() { return gen(); }
				new F().x + h().next().value`,
			want: "3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNonTailCallsOverflow checks that calls that are not in tail position still end in a RangeError.
func TestNonTailCallsOverflow(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "sloppy code",
			src:  `function f(n) { return n === 0 ? 0 : f(n - 1); } f(1e6)`,
		},
		{
			name: "try block",
			src: `function f(n) {
					"use strict";
					try { return n === 0 ? 0 : f(n - 1); } catch (e) { throw e; }
				}
				f(1e6)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, `try { `+tt.src+`; "no error" } catch (e) { e instanceof RangeError }`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != "true" {
				t.Errorf("got %q, want a RangeError", got)
			}
		})
	}
}

// TestTailCallStack checks the stack trace of an error thrown after tail calls, which replaced
// the frames of their callers.
func TestTailCallStack(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "trace.js", `"use strict";
function a(n) { return b(n + 1); }
function b(n) { return c.call(null, n + 1); }
function c(n) { throw new Error("x" + n); }
function outer() { try { a(1); } catch (e) { return e.stack; } }
outer()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	want := "Error: x3\n    at c (trace.js:4:23)\n    at outer (trace.js:5:26)\n    at trace.js:6:1"
	if got := v.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestDecorators covers the evaluation of decorators and auto-accessors, of which test262
// only checks the syntax.
func TestDecorators(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			// The decorators are evaluated in source order with the computed keys, and applied
			// after all of them, the closest to the element first: the methods, getters, setters
			// and accessors, static before instance ones, then the fields, then the class.
			name: "order",
			src: `var log = [];
				function dec(name) {
					log.push("eval " + name);
					return function (value, ctx) {
						log.push(name);
						ctx.addInitializer(function () { log.push("init " + name); });
					};
				}
				@dec("c1") @dec("c2")
				class C {
					@dec("m1") @dec("m2") m() {}
					[(log.push("key"), "k")]() {}
					@dec("f") f = log.push("field f");
					@dec("sf") static sf = log.push("static field sf");
					@dec("a") accessor a;
					@dec("sm") static sm() {}
					static { log.push("static block"); }
				}
				log.push("new");
				new C();
				log.join()`,
			want: "eval c1,eval c2,eval m1,eval m2,key,eval f,eval sf,eval a,eval sm," +
				"sm,m2,m1,a,sf,f,c2,c1," +
				"init sm,static field sf,init sf,static block,init c2,init c1," +
				"new,init m2,init m1,field f,init f,init a",
		},
		{
			name: "context objects",
			src: `var out = [], s = Symbol("s");
				function dec(value, ctx) {
					out.push(Object.keys(ctx).join("/") + ":" + ctx.kind + ":" + String(ctx.name) + ":" + ctx.static + ":" + ctx.private +
						":" + Object.keys(ctx.access ?? {}).join("/"));
				}
				@dec class C {
					@dec static #m() {}
					@dec get g() {}
					@dec set [s](v) {}
					@dec accessor #a;
					@dec 1 = 0;
				}
				out.join(" ")`,
			want: "kind/access/static/private/name/addInitializer/metadata:method:#m:true:true:get/has " +
				"kind/access/static/private/name/addInitializer/metadata:getter:g:false:false:get/has " +
				"kind/access/static/private/name/addInitializer/metadata:setter:Symbol(s):false:false:set/has " +
				"kind/access/static/private/name/addInitializer/metadata:accessor:#a:false:true:get/set/has " +
				"kind/access/static/private/name/addInitializer/metadata:field:1:false:false:get/set/has " +
				"kind/name/addInitializer/metadata:class:C:undefined:undefined:",
		},
		{
			// A decorator is called with the object of a member expression as its this value.
			name: "receivers",
			src: `"use strict";
				var out = [], ns = {
					dec() { out.push(this === ns); },
					make() { return function () { out.push(String(this)); }; },
				};
				@ns.dec @(ns.dec) @((0, ns.dec)) @ns.make() class C {}
				out.join()`,
			want: "undefined,false,true,true",
		},
		{
			name: "invalid results and late initializers",
			src: `var out = [], saved;
				for (const [ret, src] of [[1, "m() {}"], [{}, "f"], [1, "accessor a"], [{init: 1}, "accessor a"], [1, ""]]) {
					globalThis.r = () => ret;
					try {
						eval(src ? "(class { @r " + src + " })" : "(@r class {})");
						out.push("none");
					} catch (e) {
						out.push(e.constructor.name);
					}
				}
				class D { @((v, c) => { saved = c; }) m() {} }
				try { saved.addInitializer(() => {}); } catch (e) { out.push(e.constructor.name); }
				try { (class { @((v, c) => { c.addInitializer(1); }) m() {} }); } catch (e) { out.push(e.constructor.name); }
				out.join()`,
			want: "TypeError,TypeError,TypeError,TypeError,TypeError,TypeError,TypeError",
		},
		{
			name: "access",
			src: `var acc = {};
				function keep(name) { return (v, c) => { acc[name] = c.access; }; }
				class P {
					@keep("x") x = 1;
					@keep("y") #y = 2;
					@keep("m") #m() { return this.#y; }
				}
				var p = new P(), out = [];
				out.push(acc.x.get(p), acc.y.get(p), acc.x.has(p), acc.y.has(p), acc.y.has({}), acc.m.get(p).call(p));
				acc.x.set(p, 10);
				acc.y.set(p, 20);
				out.push(p.x, acc.y.get(p), acc.x.get.length, acc.x.set.length, acc.x.has.name);
				try { acc.y.get({}); } catch (e) { out.push(e.constructor.name); }
				try { acc.x.get(1); } catch (e) { out.push(e.constructor.name); }
				out.join()`,
			want: "1,2,true,true,false,2,10,20,1,2,has,TypeError,TypeError",
		},
		{
			// The class that a class decorator returns gets the class binding and the static
			// fields, and is the this value of the static blocks and the initializers.
			name: "replaced class",
			src: `var orig, inner, out = [];
				@((v, c) => { c.addInitializer(function () { out.push(this === inner); }); })
				@((v, c) => { orig = v; return class extends v {}; })
				class C {
					static s = this;
					static { inner = C; out.push(this === C); }
				}
				out.push(C === inner, C !== orig, Object.hasOwn(C, "s"), Object.hasOwn(orig, "s"));
				out.join()`,
			want: "true,true,true,true,true,false",
		},
		{
			name: "decorated methods and accessors",
			src: `function twice(v) { return function () { return v.call(this) * 2; }; }
				class S {
					@twice m() { return 1; }
					@twice get g() { return 2; }
					@twice static #sm() { return 3; }
					@((v) => ({ get() { return v.get.call(this) * 2; }, set(x) { v.set.call(this, x + 1); } })) accessor a = 3;
					@((v) => ({ init: x => x + 100 })) @((v) => ({ init: x => x * 2 })) accessor #b = 1;
					@((v) => x => x + 1) @((v) => x => x * 3) f = 2;
					b() { return this.#b; }
					static sm() { return S.#sm(); }
				}
				var s = new S(), a = s.a;
				s.a = 5;
				[s.m(), s.g, S.sm(), a, s.a, s.b(), s.f].join()`,
			want: "2,4,6,6,12,102,7",
		},
		{
			name: "auto-accessors",
			src: `var k = "x";
				class A {
					accessor [k] = 1;
					static accessor #s = 2;
					static s() { return A.#s; }
				}
				class B extends A {}
				var d = Object.getOwnPropertyDescriptor(A.prototype, "x"), a = new A(), b = new A(), out = [];
				a.x = 3;
				out.push(a.x, b.x, A.s(), Object.keys(a).length, d.enumerable, d.get.name, d.set.name, d.get.length, d.set.length);
				try { d.get.call({}); } catch (e) { out.push(e.constructor.name); }
				out.join()`,
			want: "3,1,2,0,false,get x,set x,0,1,TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecoratorMetadata covers the decorator metadata proposal
// (https://github.com/tc39/proposal-decorator-metadata), for which test262 has no tests. The
// steps cited are those of ClassDefinitionEvaluation in the proposal's spec text.
func TestDecoratorMetadata(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "Symbol.metadata",
			src: `var d = Object.getOwnPropertyDescriptor(Symbol, "metadata");
				var f = Object.getOwnPropertyDescriptor(Function.prototype, Symbol.metadata);
				[typeof Symbol.metadata, Symbol.metadata.description, d.writable, d.enumerable, d.configurable,
					f.value, f.writable, f.enumerable, f.configurable, (function () {})[Symbol.metadata]].join()`,
			want: "symbol,Symbol.metadata,false,false,false,,false,false,false,",
		},
		{
			// The context gets metadata with CreateDataPropertyOrThrow after addInitializer,
			// and the class gets it as a writable, non-enumerable, configurable property.
			name: "properties",
			src: `var ctx;
				@((v, c) => { ctx = c; })
				class C {}
				var m = C[Symbol.metadata], d = Object.getOwnPropertyDescriptor(C, Symbol.metadata);
				var cd = Object.getOwnPropertyDescriptor(ctx, "metadata");
				[Object.keys(ctx).join(" "), cd.value === m, cd.writable, cd.enumerable, cd.configurable,
					d.writable, d.enumerable, d.configurable, Object.getPrototypeOf(m), Reflect.ownKeys(m).length].join()`,
			want: "kind name addInitializer metadata,true,true,true,true,true,false,true,,0",
		},
		{
			name: "one object for all decorators of a class",
			src: `var seen = [];
				function dec(v, c) { seen.push(c.metadata); c.metadata[c.kind + (c.static ? " static" : "") + (c.private ? " private" : "")] = true; }
				@dec @dec
				class C {
					@dec m() {}
					@dec get g() { return 1; }
					@dec set s(v) {}
					@dec f = 1;
					@dec accessor a = 1;
					@dec static sm() {}
					@dec static sf = 1;
					@dec #pm() {}
					@dec #pf = 1;
					@dec static accessor #pa = 1;
				}
				var m = C[Symbol.metadata];
				[seen.length, seen.every(x => x === m), Object.keys(m).sort().join("|")].join()`,
			want: "12,true,accessor|accessor static private|class|field|field private|field static|getter|method|method private|method static|setter",
		},
		{
			// A class expression evaluated twice runs ClassDefinitionEvaluation twice, so each
			// class gets its own object.
			name: "fresh object per evaluation",
			src: `var classes = [];
				for (var i = 0; i < 2; i++) {
					classes.push(class { @((v, c) => { c.metadata.i = i; }) m() {} });
				}
				var a = classes[0][Symbol.metadata], b = classes[1][Symbol.metadata];
				[a !== b, a.i, b.i].join()`,
			want: "true,0,1",
		},
		{
			name: "inheritance",
			src: `function meta(key, value) { return (v, c) => { c.metadata[key] = value; }; }
				function append(value) { return (v, c) => { c.metadata.list = [...(c.metadata.list ?? []), value]; }; }
				@meta("a", "x") @append("x")
				class A { @meta("b", "y") m() {} }
				class B extends A {}
				@append("z")
				class C extends B { @meta("b", "z") m() {} }
				var am = A[Symbol.metadata], cm = C[Symbol.metadata];
				[Object.getPrototypeOf(cm) === am, B[Symbol.metadata] === am, Object.hasOwn(B, Symbol.metadata),
					cm.a, cm.b, am.b, cm.list.join(" "), am.list.join(" ")].join()`,
			want: "true,true,false,x,z,y,x z,x",
		},
		{
			// The parent metadata is read with Get once all elements are evaluated, before the
			// first decorator applies, and it is the prototype only when it is an object.
			name: "parent metadata",
			src: `var log = [];
				function dec(name) {
					log.push("eval " + name);
					return (v, c) => { log.push(name + " " + Object.getPrototypeOf(c.metadata)?.p); };
				}
				class P {
					static get [Symbol.metadata]() { log.push("get " + (this === P)); return { p: 1 }; }
				}
				@dec("c")
				class C extends P {
					@dec("f") f;
					[(log.push("key"), "k")]() {}
					@dec("m") m() {}
				}
				var out = [log.join()];
				for (var parent of [1, undefined, "s", Symbol(), null]) {
					class Q { static [Symbol.metadata] = parent; }
					class R extends Q { @(() => {}) m() {} }
					out.push(Object.getPrototypeOf(R[Symbol.metadata]));
				}
				@(() => {}) class N extends null {}
				out.push(Object.getPrototypeOf(N[Symbol.metadata]));
				out.join(";")`,
			want: "eval c,eval f,key,eval m,get true,m 1,f 1,c 1;;;;;;",
		},
		{
			// The metadata property is defined on the class that the class decorators returned,
			// after they applied and before the static elements and the initializers run.
			name: "order",
			src: `var out = [], orig, m;
				function has(v) { return Object.hasOwn(v, Symbol.metadata); }
				@((v, c) => { c.addInitializer(function () { out.push("class init " + has(this)); }); })
				@((v, c) => { out.push("class " + has(v)); orig = v; m = c.metadata; return class extends v {}; })
				class C {
					@((v, c) => { c.addInitializer(function () { out.push("method init " + has(this)); }); })
					static m() {}
					static f = out.push("field " + (C[Symbol.metadata] === m));
					static { out.push("block " + (this[Symbol.metadata] === m)); }
				}
				out.push(has(orig), C[Symbol.metadata] === m);
				out.join()`,
			want: "class false,method init true,field true,block true,class init true,false,true",
		},
		{
			name: "class that cannot get the property",
			src: `try {
					@(() => Object.freeze(function () {}))
					class C {}
					"no error";
				} catch (e) {
					e.constructor.name;
				}`,
			want: "TypeError",
		},
		{
			// A class gets the property only when hasDecorators is true.
			name: "undecorated class",
			src: `@(() => {}) class A {}
				class B { accessor x; static {} }
				class C extends A { accessor x; }
				[Object.hasOwn(B, Symbol.metadata), B[Symbol.metadata], Object.hasOwn(C, Symbol.metadata),
					C[Symbol.metadata] === A[Symbol.metadata]].join()`,
			want: "false,,false,true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			v, err := runScript(t.Context(), &vm, tt.name, tt.src)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := toString(t, &vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// runScript parses src as a script of vm and evaluates it.
func runScript(ctx context.Context, vm *ekuma.VM, name, src string) (ekuma.Value, error) {
	s, err := vm.ParseScript(name, src)
	if err != nil {
		return ekuma.Undefined, err
	}
	return s.Evaluate(ctx)
}

func TestUint8ArrayDecodeIntoResizableViews(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "base64 growth during options",
			src: `
				const buffer = new SharedArrayBuffer(2, {maxByteLength: 8});
				const tracking = new Uint8Array(buffer, 1);
				const fixed = new Uint8Array(buffer, 1, 1);
				const result = tracking.setFromBase64("AQID", {
					get lastChunkHandling() { buffer.grow(4); return "loose"; }
				});
				const limited = fixed.setFromBase64("BA==");
				[result.read, result.written, limited.read, limited.written,
				 new Uint8Array(buffer).join()].join(";")`,
			want: "4;3;4;1;0,4,2,3",
		},
		{
			name: "base64 shrink during options",
			src: `
				const buffer = new ArrayBuffer(4, {maxByteLength: 8});
				const dst = new Uint8Array(buffer, 1, 3);
				const calls = [];
				try {
					dst.setFromBase64("AQID", {
						get alphabet() { calls.push("alphabet"); buffer.resize(2); return "base64"; },
						get lastChunkHandling() { calls.push("last"); return "loose"; }
					});
				} catch (e) { calls.push(e instanceof TypeError); }
				calls.join()`,
			want: "alphabet,last,true",
		},
		{
			name: "hex grown shared view partial write",
			src: `
				const buffer = new SharedArrayBuffer(2, {maxByteLength: 8});
				const dst = new Uint8Array(buffer, 1);
				buffer.grow(4);
				let syntaxError = false;
				try { dst.setFromHex("0102gg"); } catch (e) { syntaxError = e instanceof SyntaxError; }
				[syntaxError, new Uint8Array(buffer).join()].join(";")`,
			want: "true;0,1,2,0",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			got, err := runScript(t.Context(), &vm, "test", tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Errorf("got %q, want %q", got.String(), tt.want)
			}
		})
	}
}
