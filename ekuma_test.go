// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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
			want: "length,name,prototype,x",
		},
		{
			name: "function own keys after a definition",
			src:  `function f() {} Object.defineProperty(f, "x", { value: 1 }); Object.getOwnPropertyNames(f).join()`,
			want: "length,name,prototype,x",
		},
		{
			name: "function own keys after deleting an initial property",
			src:  `function f() {} delete f.name; f.x = 1; Object.getOwnPropertyNames(f).join()`,
			want: "length,prototype,x",
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
			want: "length,name,prototype,x|length,name,prototype,x",
		},
		{
			name: "function own keys after a store cached for an object with the new prototype",
			src: `function set(o) { o.x = 1; }
				function f() {}
				Object.setPrototypeOf(f, null);
				set(Object.create(null)); set(Object.create(null)); set(f);
				Object.getOwnPropertyNames(f).join()`,
			want: "length,name,prototype,x",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			vm := ekuma.New()
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

func TestErrors(t *testing.T) {
	ctx := t.Context()
	vm := ekuma.New()
	if _, err := vm.RunScript(ctx, "syntax", `var = 1`); err == nil {
		t.Errorf("a syntax error was not reported")
	} else if se := (*ekuma.SyntaxError)(nil); !errors.As(err, &se) {
		t.Errorf("got %T, want *ekuma.SyntaxError", err)
	}
	if _, err := vm.RunScript(ctx, "throw", `null.x`); err == nil {
		t.Errorf("an exception was not reported")
	} else if ex := (*ekuma.Error)(nil); !errors.As(err, &ex) {
		t.Errorf("got %T, want *ekuma.Error", err)
	}
	// The VM stays usable after an exception.
	v, err := vm.RunScript(ctx, "after", `1 + 1`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if n := v.Float(); n != 2 {
		t.Errorf("got %v, want 2", n)
	}
}

func TestValueSize(t *testing.T) {
	// A Value is as large as its number and its pointer alone.
	type layout struct {
		n float64
		o *byte
	}
	got := reflect.TypeFor[ekuma.Value]().Size()
	want := reflect.TypeFor[layout]().Size()
	if got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestZeroValue(t *testing.T) {
	ctx := t.Context()
	var zero ekuma.Value
	if !zero.IsUndefined() {
		t.Errorf("the zero Value is not undefined")
	}
	if !zero.Equal(ekuma.Undefined) {
		t.Errorf("the zero Value is not equal to Undefined")
	}
	vm := ekuma.New()
	f := vm.FuncOf("f", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) == 0 {
			// Nothing is returned explicitly.
			return ekuma.Value{}, nil
		}
		return args[0], nil
	})
	if err := vm.Global().Set("f", f); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := vm.RunScript(ctx, "zero", `[typeof f(), f(0) === 0, f(undefined) === undefined, f(null) === null, f("s"), f(1.5)].join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "undefined,true,true,true,s,1.5"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCancel(t *testing.T) {
	vm := ekuma.New()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := vm.RunScript(ctx, "loop", `for (;;) {}`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	// A cancellation does not affect later calls.
	v, err := vm.RunScript(t.Context(), "after", `var n = 0; for (var i = 0; i < 1000; i++) n += i; n`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if n := v.Int(); n != 499500 {
		t.Errorf("got %v, want 499500", n)
	}
	// A context that is already done runs nothing.
	done, cancelDone := context.WithCancel(t.Context())
	cancelDone()
	if _, err := vm.RunScript(done, "never", `globalThis.ran = true`); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	ran, err := vm.Global().Get("ran")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ran.IsUndefined() {
		t.Errorf("a script ran under a context that was already done")
	}
}

// TestCancelRegExp checks that a match that backtracks for too long can be stopped.
func TestCancelRegExp(t *testing.T) {
	vm := ekuma.New()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := vm.RunScript(ctx, "backtrack", `/^(a+)+$/.test("a".repeat(40) + "b")`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	v, err := vm.RunScript(t.Context(), "after", `"a1b22".replace(/\d+/g, function(m) { return "<" + m + ">"; })`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "a<1>b<22>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFuncErrors(t *testing.T) {
	ctx := t.Context()
	vm := ekuma.New()
	var caught ekuma.Value
	funcs := map[string]ekuma.Func{
		"goError": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, errors.New("from Go")
		},
		"typeError": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, vm.NewError(ekuma.KindTypeError, "bad type")
		},
		"throwValue": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, &ekuma.Error{Value: args[0]}
		},
		"rethrow": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			// The error of a nested script passes through unchanged, even when it is wrapped.
			_, err := vm.RunScript(ctx, "inner", `throw globalThis.marker = {}`)
			return nil, fmt.Errorf("wrapped: %w", err)
		},
		"keep": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			caught = args[0]
			return ekuma.Undefined, nil
		},
	}
	for name, f := range funcs {
		if err := vm.Global().Set(name, vm.FuncOf(name, f)); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	v, err := vm.RunScript(ctx, "errors", `
		function attempt(f, ...a) { try { f(...a); } catch (e) { return e; } }
		var g = attempt(goError), t = attempt(typeError), s = attempt(throwValue, "str"), r = attempt(rethrow);
		keep(t);
		[g instanceof Error, g.constructor === Error, g.message, t instanceof TypeError, t.message, s, r === marker].join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "true,true,from Go,true,bad type,str,true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := caught.Type(); got != ekuma.TypeObject {
		t.Errorf("the thrown TypeError reached the function as %v, want object", got)
	}

	// An error that no script catches comes back from RunScript as an *Error.
	_, err = vm.RunScript(ctx, "uncaught", `typeError()`)
	if ex := (*ekuma.Error)(nil); !errors.As(err, &ex) {
		t.Errorf("got %T, want *ekuma.Error", err)
	}

	// An Error describes any thrown value, including one that belongs to no VM.
	for _, v := range []ekuma.Value{ekuma.Undefined, vm.ValueOf(1), vm.ValueOf("s"), vm.Global()} {
		if msg := (&ekuma.Error{Value: v}).Error(); msg == "" {
			t.Errorf("the message of an Error for %v is empty", v)
		}
	}
}

func TestValues(t *testing.T) {
	ctx := t.Context()
	vm := ekuma.New()
	global := vm.Global()
	if _, err := vm.RunScript(ctx, "setup", `
		var obj = { n: 1, get twice() { return this.n * 2; }, add(a, b) { return this.n + a + b; } };
		function Point(x, y) { this.x = x; this.y = y; }
		function sum(...a) { return a.reduce((s, v) => s + v, 0); }
		const lexical = 1;`); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	obj, err := global.Get("obj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := obj.Set("n", 5); err != nil {
		t.Errorf("Set: %v", err)
	}
	if v, err := obj.Get("twice"); err != nil || v.Int() != 10 {
		t.Errorf("twice = %v, %v, want 10", v, err)
	}
	if v, err := obj.Call(ctx, "add", 1, 2); err != nil || v.Int() != 8 {
		t.Errorf("add = %v, %v, want 8", v, err)
	}

	sum, err := global.Get("sum")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, err := sum.Invoke(ctx, 1, 2.5, int8(3)); err != nil || v.Float() != 6.5 {
		t.Errorf("sum = %v, %v, want 6.5", v, err)
	}

	point, err := global.Get("Point")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	p, err := point.New(ctx, 3, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v, err := p.Get("y"); err != nil || v.Int() != 4 {
		t.Errorf("y = %v, %v, want 4", v, err)
	}
	if ok, err := p.InstanceOf(point); err != nil || !ok {
		t.Errorf("InstanceOf = %v, %v, want true", ok, err)
	}
	if err := p.Delete("y"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if v, err := p.Get("y"); err != nil || !v.IsUndefined() {
		t.Errorf("y after Delete = %v, %v, want undefined", v, err)
	}

	// Go values convert to JavaScript values, including nested ones.
	arr := vm.ValueOf([]any{"a", nil, map[string]any{"k": true}})
	if n, err := arr.Get("length"); err != nil || n.Int() != 3 {
		t.Errorf("length = %v, %v, want 3", n, err)
	}
	if err := arr.SetIndex(3, "z"); err != nil {
		t.Errorf("SetIndex: %v", err)
	}
	json, err := vm.RunScript(ctx, "describe", `(function(a) { return a.map(x => x === null ? "null" : typeof x === "object" ? "k=" + x.k : x).join(); })`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if v, err := json.Invoke(ctx, arr); err != nil || v.String() != "a,null,k=true,z" {
		t.Errorf("describe = %v, %v, want a,null,k=true,z", v, err)
	}
	if v, err := arr.Index(0); err != nil || !v.Equal(vm.ValueOf("a")) {
		t.Errorf("Index(0) = %v, %v, want a", v, err)
	}

	// A string has properties too, and its length counts UTF-16 code units.
	if n, err := vm.ValueOf("h\u00e9\U0001F600").Get("length"); err != nil || n.Int() != 4 {
		t.Errorf("length of a string = %v, %v, want 4", n, err)
	}

	// The refusals of JavaScript come back as exceptions, and a value of the wrong type as a ValueError.
	ex := (*ekuma.Error)(nil)
	if _, err := obj.Call(ctx, "missing"); !errors.As(err, &ex) {
		t.Errorf("calling a missing method: got %v, want *ekuma.Error", err)
	}
	ve := (*ekuma.ValueError)(nil)
	if _, err := obj.Invoke(ctx); !errors.As(err, &ve) {
		t.Errorf("invoking an object: got %v, want *ekuma.ValueError", err)
	}
	if err := ekuma.Undefined.Set("x", 1); !errors.As(err, &ve) {
		t.Errorf("setting a property of undefined: got %v, want *ekuma.ValueError", err)
	}
	if _, err := vm.ValueOf(1).Get("x"); !errors.As(err, &ve) {
		t.Errorf("getting a property of a number: got %v, want *ekuma.ValueError", err)
	}
	// A lexical declaration is not a property of the global object.
	if v, err := global.Get("lexical"); err != nil || !v.IsUndefined() {
		t.Errorf("Get(lexical) = %v, %v, want undefined", v, err)
	}
}

func TestValueString(t *testing.T) {
	vm := ekuma.New()
	tests := []struct {
		in   ekuma.Value
		want string
	}{
		{in: vm.ValueOf("s"), want: "s"},
		{in: vm.ValueOf(42), want: "<number: 42>"},
		{in: vm.ValueOf(true), want: "<boolean: true>"},
		{in: ekuma.Undefined, want: "<undefined>"},
		{in: ekuma.Null, want: "<null>"},
		{in: vm.Global(), want: "<object>"},
		{in: vm.FuncOf("f", nil), want: "<function>"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

// TestCancelNested checks that a context stops what a Func does on behalf of the script.
func TestCancelNested(t *testing.T) {
	vm := ekuma.New()
	global := vm.Global()
	returned := false
	read := vm.FuncOf("read", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The getter never returns, and this call has no context of its own.
		v, err := args[0].Get("forever")
		returned = true
		return v, err
	})
	if err := global.Set("read", read); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err := vm.RunScript(ctx, "nested", `
		var caught = false;
		try { read({ get forever() { for (;;) {} } }); } catch (e) { caught = true; }`)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	if returned {
		t.Errorf("the Func continued after the interruption")
	}
	if v, err := global.Get("caught"); err != nil || v.Truthy() {
		t.Errorf("caught = %v, %v: the script caught the interruption", v, err)
	}
	// The VM stays usable.
	if v, err := vm.RunScript(t.Context(), "after", `1 + 1`); err != nil || v.Int() != 2 {
		t.Errorf("after the interruption: %v, %v, want 2", v, err)
	}
}
