// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

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
	var vm ekuma.VM
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

func TestVMCopy(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	// A copy of an unused VM is a VM of its own.
	unused := vm
	if _, err := unused.RunScript(ctx, "unused", "1"); err != nil {
		t.Errorf("RunScript on a copy of an unused VM: %v", err)
	}
	if _, err := vm.RunScript(ctx, "used", "1"); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	used := vm
	defer func() {
		if recover() == nil {
			t.Errorf("RunScript on a copy of a used VM did not panic")
		}
	}()
	_, _ = used.RunScript(ctx, "copy", "1")
}

func TestValueString(t *testing.T) {
	var vm ekuma.VM
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

func TestValues(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
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

func TestBigInt(t *testing.T) {
	var vm ekuma.VM
	for _, want := range []string{"1180591620717411303425", "-36893488147419103232"} {
		x, _ := new(big.Int).SetString(want, 10)
		v := vm.ValueOf(x)
		x.SetInt64(0)
		js, err := vm.RunScript(t.Context(), "bigint", want+"n")
		if err != nil {
			t.Fatalf("RunScript: %v", err)
		}
		if !v.Equal(js) {
			t.Errorf("ValueOf(%s) is not equal to %sn", want, want)
		}
		got := v.BigInt()
		if got.String() != want {
			t.Errorf("BigInt() = %v, want %s", got, want)
		}
		got.SetInt64(0)
		if got := v.BigInt(); got.String() != want {
			t.Errorf("BigInt() after modifying a previous result = %v, want %s", got, want)
		}
	}

	defer func() {
		ve, ok := recover().(*ekuma.ValueError)
		if !ok || ve.Method != "Value.BigInt" || ve.Type != ekuma.TypeNumber {
			t.Errorf("BigInt() of a number panicked with %v, want a *ekuma.ValueError", ve)
		}
	}()
	vm.ValueOf(1).BigInt()
}

// TestCopyBytes tests CopyBytesToGo and CopyBytesToJS.
func TestCopyBytes(t *testing.T) {
	var vm ekuma.VM
	run := func(src string) ekuma.Value {
		t.Helper()
		v, err := vm.RunScript(t.Context(), "test", src)
		if err != nil {
			t.Fatalf("RunScript: %v", err)
		}
		return v
	}

	// The copy is truncated to the shorter side.
	dst := make([]byte, 2)
	if n := ekuma.CopyBytesToGo(dst, run("new Uint8Array([1, 2, 3])")); n != 2 || !slices.Equal(dst, []byte{1, 2}) {
		t.Errorf("CopyBytesToGo: got %d, %v, want 2, [1 2]", n, dst)
	}
	dst = make([]byte, 3)
	if n := ekuma.CopyBytesToGo(dst, run("new Uint8ClampedArray([4, 5])")); n != 2 || !slices.Equal(dst, []byte{4, 5, 0}) {
		t.Errorf("CopyBytesToGo with a Uint8ClampedArray: got %d, %v, want 2, [4 5 0]", n, dst)
	}

	a := run("var a = new Uint8ClampedArray(3); a")
	if n := ekuma.CopyBytesToJS(a, []byte{6, 7, 8, 9}); n != 3 {
		t.Errorf("CopyBytesToJS: got %d, want 3", n)
	}
	if got := run("a.join()").String(); got != "6,7,8" {
		t.Errorf("CopyBytesToJS: got %s, want 6,7,8", got)
	}

	// A view on a detached buffer has no bytes.
	d := run("var d = new Uint8Array(2); d.buffer.transfer(); d")
	if n := ekuma.CopyBytesToGo(make([]byte, 2), d); n != 0 {
		t.Errorf("CopyBytesToGo with a detached buffer: got %d, want 0", n)
	}
	if n := ekuma.CopyBytesToJS(d, []byte{1, 2}); n != 0 {
		t.Errorf("CopyBytesToJS with a detached buffer: got %d, want 0", n)
	}

	expectValueError := func(name string, f func()) {
		t.Helper()
		defer func() {
			t.Helper()
			r := recover()
			var verr *ekuma.ValueError
			if err, ok := r.(error); !ok || !errors.As(err, &verr) {
				t.Errorf("%s: got panic %v, want a *ValueError", name, r)
			}
		}()
		f()
	}
	expectValueError("CopyBytesToGo with an Int8Array", func() {
		ekuma.CopyBytesToGo(make([]byte, 1), run("new Int8Array(1)"))
	})
	im := run("var im = new Uint8Array(new Uint8Array([1]).buffer.transferToImmutable()); im")
	expectValueError("CopyBytesToJS with an immutable buffer", func() {
		ekuma.CopyBytesToJS(im, []byte{2})
	})
	if got := run("im[0]").Int(); got != 1 {
		t.Errorf("immutable element: got %d, want 1", got)
	}
	// An immutable buffer can still be read.
	dst = make([]byte, 1)
	if n := ekuma.CopyBytesToGo(dst, im); n != 1 || dst[0] != 1 {
		t.Errorf("CopyBytesToGo with an immutable buffer: got %d, %v, want 1, [1]", n, dst)
	}
}

func TestFuncErrors(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	var caught ekuma.Value
	funcs := map[string]ekuma.Func{
		"goError": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, errors.New("from Go")
		},
		"typeError": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			c, err := vm.Global().Get("TypeError")
			if err != nil {
				return nil, err
			}
			e, err := c.New(ctx, "bad type")
			if err != nil {
				return nil, err
			}
			return nil, &ekuma.Error{Value: e}
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

func TestErrors(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
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

// TestJobOrder checks when the jobs run relative to the script and to calls from Go.
func TestJobOrder(t *testing.T) {
	var vm ekuma.VM
	call := vm.FuncOf("call", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The jobs do not run while the script that called this is still running.
		return args[0].Invoke(context.Background())
	})
	if err := vm.Global().Set("call", call); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, err := vm.RunScript(t.Context(), "order", `
		var log = [];
		Promise.resolve().then(() => log.push("job"));
		call(() => log.push("call"));
		log.push("script");
		throw new Error("thrown");
	`)
	if err == nil {
		t.Errorf("got no error, want the thrown one")
	}
	// The jobs run after a script that throws.
	v, err := vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "call,script,job"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCancel(t *testing.T) {
	var vm ekuma.VM
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

// TestCancelNested checks that a context stops what a Func does on behalf of the script.
func TestCancelNested(t *testing.T) {
	var vm ekuma.VM
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

// TestCancelRegExp checks that a match that backtracks for too long can be stopped.
func TestCancelRegExp(t *testing.T) {
	var vm ekuma.VM
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

// TestCancelJSON checks that parsing a long text can be stopped.
func TestCancelJSON(t *testing.T) {
	var vm ekuma.VM
	if _, err := vm.RunScript(t.Context(), "setup", `var text = "[" + "[0],".repeat(5000000) + "0]";`); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := vm.RunScript(ctx, "parse", `JSON.parse(text)`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	v, err := vm.RunScript(t.Context(), "after", `JSON.stringify(JSON.parse('{"a":[1]}'))`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), `{"a":[1]}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCancelJobs checks that a context stops a script whose jobs never run out, and that
// the jobs that were pending then are dropped without affecting later calls.
func TestCancelJobs(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "then",
			src:  `var count = 0; function f() { count++; Promise.resolve().then(f); } f();`,
		},
		{
			name: "await",
			src:  `var count = 0; (async () => { for (;;) { count++; await null; } })();`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if _, err := vm.RunScript(ctx, "endless", tt.src); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			if _, err := vm.RunScript(t.Context(), "after", `var before = count; var r = 0; Promise.resolve(5).then(v => { r = v; });`); err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			v, err := vm.RunScript(t.Context(), "check", `[count > 0, count === before, r].join()`)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got, want := v.String(), "true,true,5"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestProxyInterrupt checks that loops over the internal methods of proxies can be stopped.
func TestProxyInterrupt(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "a prototype chain that loops through a proxy without traps",
			src: `var t = {};
				var p = new Proxy(t, {});
				Object.setPrototypeOf(t, p);
				p instanceof Array`,
		},
		{
			name: "a trap that makes a proxy its own prototype",
			src: `var p = new Proxy({}, { getPrototypeOf() { return p; } });
				for (var k in p) {}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if _, err := vm.RunScript(ctx, tt.name, tt.src); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
		})
	}
}

// TestCancelModule checks that a context stops a module whose top level loops forever.
func TestCancelModule(t *testing.T) {
	for _, src := range []string{
		`for (;;) {}`,
		`await null; for (;;) {}`,
	} {
		var vm ekuma.VM
		m, err := vm.ParseModule("endless", src)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		if _, err := m.Evaluate(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want context.DeadlineExceeded", src, err)
		}
		cancel()
	}
}

func TestPromiseState(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		state  ekuma.PromiseState
		result string
	}{
		{
			name:   "pending",
			src:    `new Promise(() => {})`,
			state:  ekuma.PromisePending,
			result: "<undefined>",
		},
		{
			name:   "fulfilled",
			src:    `Promise.resolve("ok")`,
			state:  ekuma.PromiseFulfilled,
			result: "ok",
		},
		{
			name:   "rejected",
			src:    `Promise.reject("no")`,
			state:  ekuma.PromiseRejected,
			result: "no",
		},
		{
			name:   "subclass",
			src:    `class P extends Promise {}; P.resolve("sub")`,
			state:  ekuma.PromiseFulfilled,
			result: "sub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			p, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatal(err)
			}
			if !p.IsPromise() {
				t.Fatalf("IsPromise is false")
			}
			state, result := p.PromiseState()
			if state != tt.state {
				t.Errorf("got state %v, want %v", state, tt.state)
			}
			if got := result.String(); got != tt.result {
				t.Errorf("got result %q, want %q", got, tt.result)
			}
		})
	}

	var vm ekuma.VM
	thenable, err := vm.RunScript(t.Context(), "thenable", `({ then(f) { f(1); } })`)
	if err != nil {
		t.Fatal(err)
	}
	if thenable.IsPromise() {
		t.Errorf("a thenable is a promise")
	}
	defer func() {
		if _, ok := recover().(*ekuma.ValueError); !ok {
			t.Errorf("PromiseState of a thenable did not panic with a *ValueError")
		}
	}()
	thenable.PromiseState()
}

// isInstance reports whether v is an instance of the global constructor name.
func isInstance(t *testing.T, vm *ekuma.VM, v ekuma.Value, name string) bool {
	t.Helper()
	c, err := vm.Global().Get(name)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := v.InstanceOf(c)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// TestModuleSteps checks that the steps of a module can be taken in any order and again.
func TestModuleSteps(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"dep": `export let v = "dep";`,
	})
	m, err := vm.ParseModule("main", `export { v } from "dep";`)
	if err != nil {
		t.Fatal(err)
	}

	// Namespace loads and links m first.
	ns, err := m.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	var ex *ekuma.Error
	if _, err := ns.Get("v"); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "ReferenceError") {
		t.Errorf("reading an export before the evaluation: got %v, want a ReferenceError", err)
	}
	if err := m.LoadRequestedModules(); err != nil {
		t.Errorf("LoadRequestedModules: %v", err)
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link: %v", err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, _ := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Errorf("got %v, want fulfilled", st)
	}
	p2, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !p2.Equal(p) {
		t.Errorf("a second Evaluate returned another promise")
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link after Evaluate: %v", err)
	}
	ns2, err := m.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	if !ns2.Equal(ns) {
		t.Errorf("a second Namespace returned another object")
	}
	if v, err := ns.Get("v"); err != nil || v.String() != "dep" {
		t.Errorf("got %v, %v, want dep", v, err)
	}
	if got := l.loads["dep"]; got != 1 {
		t.Errorf("the loader was called %d times for dep, want 1", got)
	}
}

// TestModuleEvaluationError checks that an exception of a module rejects the promise of
// Evaluate, and that evaluating the module again returns the same promise.
func TestModuleEvaluationError(t *testing.T) {
	var vm ekuma.VM
	m, err := vm.ParseModule("main", `throw "thrown";`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseRejected || r.String() != "thrown" {
		t.Errorf("got %v with %v, want rejected with thrown", st, r)
	}
	p2, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !p2.Equal(p) {
		t.Errorf("a second Evaluate returned another promise")
	}
}

// TestModuleLoaderErrors checks the error that each kind of error of the loader becomes.
func TestModuleLoaderErrors(t *testing.T) {
	var vm ekuma.VM
	thrown := vm.ValueOf(map[string]any{})
	tests := []struct {
		name string
		err  error

		// ctor is the constructor of the thrown value, or empty for thrown itself.
		ctor string
	}{
		{
			name: "Error",
			err:  &ekuma.Error{Value: thrown},
		},
		{
			name: "SyntaxError",
			err: &ekuma.SyntaxError{
				File:    "dep",
				Line:    1,
				Message: "bad",
			},
			ctor: "SyntaxError",
		},
		{
			name: "other",
			err:  errors.New("no module"),
			ctor: "Error",
		},
		{
			name: "no loader",
			ctor: "TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err != nil {
				vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
					return nil, tt.err
				})
			} else {
				vm.SetModuleLoader(nil)
			}
			m, err := vm.ParseModule("main", `import "dep";`)
			if err != nil {
				t.Fatal(err)
			}
			var ex *ekuma.Error
			if err := m.LoadRequestedModules(); !errors.As(err, &ex) {
				t.Fatalf("got %v, want an *Error", err)
			}
			if tt.ctor == "" {
				if !ex.Value.Equal(thrown) {
					t.Errorf("got %v, want the value of the *Error", ex.Value)
				}
				return
			}
			c, err := ex.Value.Get("constructor")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := c.Get("name"); err != nil || n.String() != tt.ctor {
				t.Errorf("got an instance of %v, want %s", n, tt.ctor)
			}
		})
	}
}

// TestModuleLinkRetry checks that a Link that failed can be taken again, and that an import
// that does not resolve is an error of Link.
func TestModuleLinkRetry(t *testing.T) {
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return nil, errors.New("not yet")
	})
	m, err := vm.ParseModule("main", `import { v } from "dep"; globalThis.result = v;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Link(); err == nil {
		t.Fatal("Link succeeded with a failing loader")
	}
	var referrer *ekuma.Module
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		referrer = req.Referrer
		return vm.ParseModule(req.Specifier, `export const v = "linked";`)
	})
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Errorf("got %v with %v, want fulfilled", st, r)
	}
	if referrer != m {
		t.Errorf("the Referrer of the request is not the importing module")
	}

	bad, err := vm.ParseModule("bad", `import { missing } from "dep2";`)
	if err != nil {
		t.Fatal(err)
	}
	var ex *ekuma.Error
	if err := bad.Link(); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}
}

// TestModuleOfAnotherVM checks that a loader that returns a Module of another VM panics.
func TestModuleOfAnotherVM(t *testing.T) {
	var vm, other ekuma.VM
	dep := other.NewSyntheticModule("dep", ekuma.Undefined)
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return dep, nil
	})
	m, err := vm.ParseModule("main", `import "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Errorf("LoadRequestedModules did not panic")
		}
	}()
	_ = m.LoadRequestedModules()
}

// TestSyntheticModules checks the modules of NewSyntheticModule and ParseJSONModule.
func TestSyntheticModules(t *testing.T) {
	var vm ekuma.VM
	syn := vm.NewSyntheticModule("syn", vm.ValueOf("synthetic"))
	j, err := vm.ParseJSONModule("data.json", `{"a": [1, 2]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := j.Name(), "data.json"; got != want {
		t.Errorf("got name %q, want %q", got, want)
	}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if req.Attributes["type"] == "json" {
			return j, nil
		}
		return syn, nil
	})
	m, err := vm.ParseModule("main", `import s from "syn";
		import j from "data.json" with { type: "json" };
		globalThis.result = s + " " + j.a.join();`)
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
	if got, want := v.String(), "synthetic 1,2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var ex *ekuma.Error
	if _, err := vm.ParseJSONModule("bad.json", `{`); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}
}
