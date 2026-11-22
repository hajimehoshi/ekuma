// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestForeignValueEntry(t *testing.T) {
	for _, name := range []string{
		"ValueOf", "RealmValueOf", "Nested", "Set", "SetIndex", "Invoke", "Call", "New",
		"Result", "Exception", "SyntheticModule", "InstanceOf", "LoaderException", "ImportMeta",
		"GetWithValueKey", "SetWithValueKey", "DeleteWithValueKey", "CallWithValueKey",
	} {
		t.Run(name, func(t *testing.T) {
			var a, b ekuma.VM
			foreign := a.ValueOf(map[string]any{"x": 42})
			f := b.FuncOf("identity", func(this ekuma.Value, args []ekuma.Value) (any, error) {
				return args[0], nil
			})
			defer func() {
				if got := recover(); got != nil {
					if _, ok := got.(string); !ok {
						t.Errorf("panic = %v (%T), want a string", got, got)
					}
				} else {
					t.Error("foreign value was accepted without a panic")
				}
			}()
			switch name {
			case "ValueOf":
				b.ValueOf(foreign)
			case "RealmValueOf":
				b.NewRealm().ValueOf(foreign)
			case "Nested":
				b.ValueOf([]any{map[string]any{"nested": foreign}})
			case "Set":
				_ = b.Global().Set("foreign", foreign)
			case "SetIndex":
				_ = b.ValueOf([]any{}).SetIndex(0, foreign)
			case "Invoke":
				_, _ = f.Invoke(t.Context(), 1, foreign)
			case "Call":
				_, _ = b.ValueOf(map[string]any{"f": f}).Call(t.Context(), "f", foreign)
			case "New":
				ctor, err := b.Global().Get("Object")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = ctor.New(t.Context(), foreign)
			case "Result", "Exception":
				callback := b.FuncOf("foreign", func(this ekuma.Value, args []ekuma.Value) (any, error) {
					if name == "Exception" {
						return nil, fmt.Errorf("wrapped: %w", &ekuma.ScriptError{Value: foreign})
					}
					return foreign, nil
				})
				_, _ = callback.Invoke(t.Context())
			case "SyntheticModule":
				b.NewRealm().NewSyntheticModule("foreign", foreign)
			case "InstanceOf":
				ctor, err := b.Global().Get("Object")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = foreign.InstanceOf(ctor)
			case "LoaderException":
				b.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
					return nil, &ekuma.ScriptError{Value: foreign}
				})
				m, err := b.ParseModule("main", `import "foreign"`)
				if err != nil {
					t.Fatal(err)
				}
				_ = m.LoadRequestedModules()
			case "ImportMeta":
				b.SetImportMeta(func(m *ekuma.Module) map[string]any {
					return map[string]any{"foreign": foreign}
				})
				m, err := b.ParseModule("main", `import.meta.foreign`)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = m.Evaluate(t.Context())
			case "GetWithValueKey":
				_, _ = b.Global().GetWithValueKey(foreign)
			case "SetWithValueKey":
				_ = b.Global().SetWithValueKey(foreign, 1)
			case "DeleteWithValueKey":
				_ = b.Global().DeleteWithValueKey(foreign)
			case "CallWithValueKey":
				_, _ = b.Global().CallWithValueKey(t.Context(), foreign)
			}
		})
	}
}

func TestForeignValueKinds(t *testing.T) {
	for _, src := range []string{`"key"`, `Symbol("key")`, `(() => {})`, `new SharedArrayBuffer(4)`} {
		t.Run(src, func(t *testing.T) {
			var a, b ekuma.VM
			v, err := runScript(t.Context(), &a, "", src)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if got := recover(); got != nil {
					if _, ok := got.(string); !ok {
						t.Errorf("panic = %v (%T), want a string", got, got)
					}
				} else {
					t.Error("foreign value was accepted without a panic")
				}
			}()
			b.ValueOf(v)
		})
	}
}

func TestVMIndependentValues(t *testing.T) {
	var a, b ekuma.VM
	values, err := runScript(t.Context(), &a, "", `[undefined, null, true, 42, 3n, 2n ** 100n]`)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 6 {
		v, err := values.Index(i)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Global().Set("value", v); err != nil {
			t.Fatal(err)
		}
		got, err := runScript(t.Context(), &b, "", `({[value]: value})[value]`)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(v) {
			t.Errorf("value %d did not survive transfer", i)
		}
	}
}

// TestValueIntOutOfRange checks the int of the numbers that an int cannot hold.
func TestValueIntOutOfRange(t *testing.T) {
	var vm ekuma.VM
	for _, tt := range []struct {
		x    float64
		want int
	}{
		{
			x:    math.NaN(),
			want: 0,
		},
		{
			x:    math.Inf(1),
			want: math.MaxInt,
		},
		{
			x:    -1e300,
			want: math.MinInt,
		},
	} {
		if got := vm.ValueOf(tt.x).Int(); got != tt.want {
			t.Errorf("Int() of %v = %d, want %d", tt.x, got, tt.want)
		}
	}
}

// TestNilFunc checks that a nil Func panics when the function is created.
func TestNilFunc(t *testing.T) {
	for name, f := range map[string]func(vm *ekuma.VM){
		"ValueOf": func(vm *ekuma.VM) { vm.ValueOf(ekuma.Func(nil)) },
		"FuncOf":  func(vm *ekuma.VM) { vm.FuncOf("f", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			var vm ekuma.VM
			defer func() {
				if recover() == nil {
					t.Error("a nil Func was accepted without a panic")
				}
			}()
			f(&vm)
		})
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

func TestValueNotComparable(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[ekuma.Value](), reflect.TypeFor[engine.Value]()} {
		if typ.Comparable() {
			t.Errorf("%v must not be comparable", typ)
		}
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
	v, err := runScript(ctx, &vm, "zero", `[typeof f(), f(0) === 0, f(undefined) === undefined, f(null) === null, f("s"), f(1.5)].join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
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
	if _, err := runScript(ctx, &unused, "unused", "1"); err != nil {
		t.Errorf("runScript on a copy of an unused VM: %v", err)
	}
	if _, err := runScript(ctx, &vm, "used", "1"); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	used := vm
	defer func() {
		if recover() == nil {
			t.Errorf("runScript on a copy of a used VM did not panic")
		}
	}()
	_, _ = runScript(ctx, &used, "copy", "1")
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
		{in: vm.FuncOf("f", func(this ekuma.Value, args []ekuma.Value) (any, error) { return nil, nil }), want: "<function>"},
	}
	for _, tt := range tests {
		if got := tt.in.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestHTMLDDAType(t *testing.T) {
	var vm ekuma.VM
	dda := ekuma.NewHTMLDDA(vm.InitialRealm())
	if got := dda.Type(); got != ekuma.TypeFunction {
		t.Errorf("HTMLDDA.Type() = %v, want %v", got, ekuma.TypeFunction)
	}
}

func TestValues(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	global := vm.Global()
	if _, err := runScript(ctx, &vm, "setup", `
		var obj = { n: 1, get twice() { return this.n * 2; }, add(a, b) { return this.n + a + b; } };
		function Point(x, y) { this.x = x; this.y = y; }
		function sum(...a) { return a.reduce((s, v) => s + v, 0); }
		const lexical = 1;`); err != nil {
		t.Fatalf("runScript: %v", err)
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
	json, err := runScript(ctx, &vm, "describe", `(function(a) { return a.map(x => x === null ? "null" : typeof x === "object" ? "k=" + x.k : x).join(); })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
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
	ex := (*ekuma.ScriptError)(nil)
	if _, err := obj.Call(ctx, "missing"); !errors.As(err, &ex) {
		t.Errorf("calling a missing method: got %v, want *ekuma.ScriptError", err)
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

func TestValueLargeIndex(t *testing.T) {
	const key = "9007199254740993"
	index, err := strconv.Atoi(key)
	if errors.Is(err, strconv.ErrRange) {
		t.Skip("index requires a 64-bit int")
	}
	if err != nil {
		t.Fatal(err)
	}
	var vm ekuma.VM
	obj := vm.ValueOf(map[string]any{key: "original"})
	if got, err := obj.Index(index); err != nil || got.String() != "original" {
		t.Errorf("Index(%d) = %v, %v, want original", index, got, err)
	}
	if err := obj.SetIndex(index, "updated"); err != nil {
		t.Errorf("SetIndex(%d): %v", index, err)
	}
	if got, err := obj.Get(key); err != nil || got.String() != "updated" {
		t.Errorf("Get(%q) = %v, %v, want updated", key, got, err)
	}
}

func TestValueKeys(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	keys, err := runScript(ctx, &vm, "keys", `({ sym: Symbol("k"), obj: { toString() { return "n"; } }, iter: Symbol.iterator })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	sym, err := keys.Get("sym")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	o := vm.ValueOf(map[string]any{"n": 1})
	if err := o.SetWithValueKey(sym, 2); err != nil {
		t.Errorf("SetWithValueKey: %v", err)
	}
	if v, err := o.GetWithValueKey(sym); err != nil || v.Int() != 2 {
		t.Errorf("o[sym] = %v, %v, want 2", v, err)
	}
	if err := o.DeleteWithValueKey(sym); err != nil {
		t.Errorf("DeleteWithValueKey: %v", err)
	}
	if v, err := o.GetWithValueKey(sym); err != nil || !v.IsUndefined() {
		t.Errorf("o[sym] after DeleteWithValueKey = %v, %v, want undefined", v, err)
	}

	// An object key is converted the way o[key] converts it.
	obj, err := keys.Get("obj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, err := o.GetWithValueKey(obj); err != nil || v.Int() != 1 {
		t.Errorf("o[obj] = %v, %v, want 1", v, err)
	}

	// A store that fails converts its key once.
	counted, err := runScript(ctx, &vm, "counted", `var n = 0; ({ frozen: Object.freeze({}), key: { toString() { n++; return "x"; } }, count() { return n; } })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	frozen, err := counted.Get("frozen")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	key, err := counted.Get("key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var serr *ekuma.ScriptError
	if err := frozen.SetWithValueKey(key, 1); !errors.As(err, &serr) {
		t.Errorf("SetWithValueKey on a frozen object: got %v, want *ekuma.ScriptError", err)
	}
	if v, err := counted.Call(ctx, "count"); err != nil || v.Int() != 1 {
		t.Errorf("key conversions = %v, %v, want 1", v, err)
	}

	iter, err := keys.Get("iter")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	it, err := vm.ValueOf([]any{"a"}).CallWithValueKey(ctx, iter)
	if err != nil {
		t.Fatalf("CallWithValueKey: %v", err)
	}
	next, err := it.Call(ctx, "next")
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if v, err := next.Get("value"); err != nil || v.String() != "a" {
		t.Errorf("first value = %v, %v, want a", v, err)
	}
}

func TestFloatNaN(t *testing.T) {
	var vm ekuma.VM
	// The NaNs differ in their bits.
	for _, src := range []string{
		`0/0`,
		`NaN`,
		`new Float64Array(new Uint32Array([1, 0xfff80000]).buffer)[0]`,
	} {
		v, err := runScript(t.Context(), &vm, "nan", src)
		if err != nil {
			t.Fatalf("runScript(%q): %v", src, err)
		}
		if got, want := math.Float64bits(v.Float()), uint64(0x7ff8000000000000); got != want {
			t.Errorf("%s: got %#x, want %#x", src, got, want)
		}
	}
}

// TestBigIntTooLarge checks that a Go BigInt beyond the largest BigInt of 2^30 bits does not
// enter the VM.
func TestBigIntTooLarge(t *testing.T) {
	var vm ekuma.VM
	tooLarge := new(big.Int).Lsh(big.NewInt(1), 1<<30)
	if got := recoverHostPanic(func() { vm.ValueOf(tooLarge) }); got == nil {
		t.Errorf("ValueOf of a BigInt of 2^30+1 bits did not panic")
	}
	if err := vm.Global().Set("tooLarge", vm.FuncOf("tooLarge", func(ekuma.Value, []ekuma.Value) (any, error) {
		return tooLarge, nil
	})); err != nil {
		t.Fatal(err)
	}
	if got := run(t, &vm, `try { tooLarge(); "no error"; } catch (e) { e instanceof RangeError; }`); got != "true" {
		t.Errorf("a Func result of 2^30+1 bits: got %s, want a RangeError", got)
	}
}

func TestBigInt(t *testing.T) {
	var vm ekuma.VM
	for _, want := range []string{
		"1180591620717411303425",
		"-36893488147419103232",
		// The ends of the range of int64 and the values just outside it.
		"9223372036854775807",
		"-9223372036854775808",
		"9223372036854775808",
		"-9223372036854775809",
	} {
		x, _ := new(big.Int).SetString(want, 10)
		v := vm.ValueOf(x)
		x.SetInt64(0)
		js, err := runScript(t.Context(), &vm, "bigint", want+"n")
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if !v.Equal(js) {
			t.Errorf("ValueOf(%s) is not equal to %sn", want, want)
		}
		if got := js.BigInt(); got.String() != want {
			t.Errorf("BigInt() of %sn = %v, want %s", want, got, want)
		}
		if got := js.Type(); got != ekuma.TypeBigInt {
			t.Errorf("Type() of %sn = %v, want %v", want, got, ekuma.TypeBigInt)
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
		v, err := runScript(t.Context(), &vm, "test", src)
		if err != nil {
			t.Fatalf("runScript: %v", err)
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

// TestNewArrayBuffer tests that Go and JavaScript see the writes of each other at once through
// views of an ArrayBuffer over a Go slice at different byte offsets.
func TestNewArrayBuffer(t *testing.T) {
	var vm ekuma.VM
	b := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := vm.Global().Set("buf", vm.NewArrayBuffer(b)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// poke writes b while JavaScript runs, as WebAssembly code does.
	poke := vm.FuncOf("poke", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		b[args[0].Int()] = byte(args[1].Int())
		return nil, nil
	})
	if err := vm.Global().Set("poke", poke); err != nil {
		t.Fatalf("Set: %v", err)
	}
	b[0] = 99
	got := run(t, &vm, `
		var u8 = new Uint8Array(buf);
		var tail = new Uint8Array(buf, 4, 2);
		var dv = new DataView(buf, 2);
		u8[1] = 88;
		tail[1] = 77;
		dv.setUint16(4, 0x0102, true);
		poke(3, 66);
		[buf.byteLength, u8[0], dv.getUint8(1)].join()`)
	if want := "8,99,66"; got != want {
		t.Errorf("JavaScript reads: got %s, want %s", got, want)
	}
	if want := []byte{99, 88, 3, 66, 5, 77, 2, 1}; !slices.Equal(b, want) {
		t.Errorf("Go reads: got %v, want %v", b, want)
	}

	// A view created before a write from Go sees it.
	b[4] = 55
	if got, want := run(t, &vm, "tail[0]"), "55"; got != want {
		t.Errorf("tail[0]: got %s, want %s", got, want)
	}
}

func TestDataViewBounds(t *testing.T) {
	var vm ekuma.VM
	if err := vm.Global().Set("hostBuffer", vm.NewArrayBuffer(nil)); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `
		const hostView = new DataView(hostBuffer);
		const immutableView = new DataView(new ArrayBuffer(0).sliceToImmutable());
		const buffer = new ArrayBuffer(1, {maxByteLength: 1});
		const fixedView = new DataView(buffer, 0, 1);
		buffer.resize(0);
	`)
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			src:  "hostView.getUint8(0)",
			want: "RangeError",
		},
		{
			src:  "hostView.setUint8(0, 1)",
			want: "RangeError",
		},
		{
			src:  "immutableView.getUint8(0)",
			want: "RangeError",
		},
		{
			src:  "fixedView.getUint8(0)",
			want: "TypeError",
		},
	} {
		got := run(t, &vm, "try { "+tc.src+" } catch (e) { e.constructor.name }")
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.src, got, tc.want)
		}
	}
}

// TestRealmNewArrayBuffer tests that an ArrayBuffer of a realm is an instance of the ArrayBuffer
// of that realm, and that an empty one is not detached.
func TestRealmNewArrayBuffer(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	buf := r.NewArrayBuffer(nil)
	ctor, err := r.Global().Get("ArrayBuffer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok, err := buf.InstanceOf(ctor); err != nil || !ok {
		t.Errorf("InstanceOf: got %v, %v, want true", ok, err)
	}
	if got, err := buf.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached: got %v, %v, want false", got, err)
	}
}

// TestDetachArrayBuffer tests that detaching an ArrayBuffer over a Go slice from a Go function,
// as WebAssembly memory grows, leaves every view without bytes, and that the replacement buffer
// has the new storage.
func TestDetachArrayBuffer(t *testing.T) {
	var vm ekuma.VM
	mem := []byte{1, 2, 3, 4}
	buf := vm.NewArrayBuffer(mem)
	if err := vm.Global().Set("buf", buf); err != nil {
		t.Fatalf("Set: %v", err)
	}
	grow := vm.FuncOf("grow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		vm.DetachArrayBuffer(buf)
		// Detaching a detached buffer does nothing.
		vm.DetachArrayBuffer(buf)
		mem = append(mem, 0, 0, 0, 0)
		buf = vm.NewArrayBuffer(mem)
		return buf, nil
	})
	if err := vm.Global().Set("grow", grow); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := run(t, &vm, `
		var old = buf;
		var u8 = new Uint8Array(old);
		var tracking = new Uint8Array(old, 1);
		var dv = new DataView(old);
		u8[0] = 10;
		var nu = grow();
		u8[1] = 20;
		var dvError;
		try {
			dv.getUint8(0);
		} catch (e) {
			dvError = e.constructor.name;
		}
		new Uint8Array(nu)[4] = 30;
		[old.detached, old.byteLength, u8.length, u8[0], tracking.length, dvError, nu.detached, nu.byteLength].join()`)
	if want := "true,0,0,,0,TypeError,false,8"; got != want {
		t.Errorf("JavaScript reads: got %s, want %s", got, want)
	}
	if want := []byte{10, 2, 3, 4, 30, 0, 0, 0}; !slices.Equal(mem, want) {
		t.Errorf("Go reads: got %v, want %v", mem, want)
	}

	// An immutable buffer cannot be detached.
	im, err := runScript(t.Context(), &vm, "test", "new ArrayBuffer(1).transferToImmutable()")
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	func() {
		defer func() {
			if x := recover(); x == nil {
				t.Errorf("DetachArrayBuffer of an immutable buffer: got no panic")
			} else if _, ok := x.(*ekuma.ValueError); !ok {
				t.Errorf("DetachArrayBuffer of an immutable buffer: got panic %v, want a *ValueError", x)
			}
		}()
		vm.DetachArrayBuffer(im)
	}()
	if got, err := im.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached: got %v, %v, want false", got, err)
	}
}

func TestDetachArrayBufferCopiedVM(t *testing.T) {
	var vm ekuma.VM
	buf := vm.NewArrayBuffer([]byte{1})
	copied := vm
	func() {
		defer func() {
			if recover() == nil {
				t.Error("DetachArrayBuffer on a copy of a used VM did not panic")
			}
		}()
		copied.DetachArrayBuffer(buf)
	}()
	if got, err := buf.Get("detached"); err != nil || got.Bool() {
		t.Errorf("detached after using the copy: got %v, %v, want false", got, err)
	}
}

// TestNewArrayBufferTransfer tests that JavaScript can neither transfer nor resize an ArrayBuffer
// over a Go slice, and that its views keep the slice.
func TestNewArrayBufferTransfer(t *testing.T) {
	var vm ekuma.VM
	b := []byte{1, 2}
	if err := vm.Global().Set("buf", vm.NewArrayBuffer(b)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got := run(t, &vm, `
		var u8 = new Uint8Array(buf);
		var errors = [];
		for (const f of [
			() => buf.transfer(),
			() => buf.transferToFixedLength(),
			() => buf.transferToImmutable(),
			() => buf.resize(1),
		]) {
			try {
				f();
				errors.push("none");
			} catch (e) {
				errors.push(e.constructor.name);
			}
		}
		u8[1] = 3;
		[errors, buf.detached, buf.resizable].join()`)
	if want := "TypeError,TypeError,TypeError,TypeError,false,false"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if want := []byte{1, 3}; !slices.Equal(b, want) {
		t.Errorf("Go reads: got %v, want %v", b, want)
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
			return nil, &ekuma.ScriptError{Value: e}
		},
		"throwValue": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, &ekuma.ScriptError{Value: args[0]}
		},
		"rethrow": func(this ekuma.Value, args []ekuma.Value) (any, error) {
			// The error of a nested script passes through unchanged, even when it is wrapped.
			_, err := runScript(ctx, &vm, "inner", `throw globalThis.marker = {}`)
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
	v, err := runScript(ctx, &vm, "errors", `
		function attempt(f, ...a) { try { f(...a); } catch (e) { return e; } }
		var g = attempt(goError), t = attempt(typeError), s = attempt(throwValue, "str"), r = attempt(rethrow);
		keep(t);
		[g instanceof Error, g.constructor === Error, g.message, t instanceof TypeError, t.message, s, r === marker].join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "true,true,from Go,true,bad type,str,true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := caught.Type(); got != ekuma.TypeObject {
		t.Errorf("the thrown TypeError reached the function as %v, want object", got)
	}

	// An error that no script catches comes back from Script.Evaluate as a *ScriptError.
	_, err = runScript(ctx, &vm, "uncaught", `typeError()`)
	if ex := (*ekuma.ScriptError)(nil); !errors.As(err, &ex) {
		t.Errorf("got %T, want *ekuma.ScriptError", err)
	}

	// A ScriptError describes any thrown value, including one that belongs to no VM.
	for _, v := range []ekuma.Value{ekuma.Undefined, vm.ValueOf(1), vm.ValueOf("s"), vm.Global()} {
		if msg := (&ekuma.ScriptError{Value: v}).Error(); msg == "" {
			t.Errorf("the message of a ScriptError for %v is empty", v)
		}
	}
}

// TestErrorUnwrap checks that an exception made from a Go error unwraps to that error.
func TestErrorUnwrap(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	fail := vm.FuncOf("fail", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return nil, fmt.Errorf("fail: %w", io.EOF)
	})
	if err := vm.Global().Set("fail", fail); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for _, tc := range []struct {
		name   string
		src    string
		wantIs bool
	}{
		{
			name:   "uncaught",
			src:    `fail()`,
			wantIs: true,
		},
		{
			name:   "rejected promise",
			src:    `(async () => fail())()`,
			wantIs: true,
		},
		{
			name:   "rethrown",
			src:    `try { fail(); } catch (e) { throw e; }`,
			wantIs: true,
		},
		{
			name:   "thrown by script",
			src:    `try { fail(); } catch (e) { throw new Error(e.message); }`,
			wantIs: false,
		},
	} {
		v, err := runScript(ctx, &vm, tc.name, tc.src)
		if err == nil {
			st, r := v.PromiseState()
			if st != ekuma.PromiseRejected {
				t.Errorf("%s: got %v, want rejected", tc.name, st)
				continue
			}
			err = &ekuma.ScriptError{Value: r}
		}
		ex, ok := errors.AsType[*ekuma.ScriptError](err)
		if !ok {
			t.Errorf("%s: got %T, want *ekuma.ScriptError", tc.name, err)
			continue
		}
		if got := errors.Is(err, io.EOF); got != tc.wantIs {
			t.Errorf("%s: errors.Is(err, io.EOF) = %v, want %v", tc.name, got, tc.wantIs)
		}
		if !tc.wantIs && ex.Unwrap() != nil {
			t.Errorf("%s: Unwrap() = %v, want nil", tc.name, ex.Unwrap())
		}
	}
}

func TestErrors(t *testing.T) {
	ctx := t.Context()
	var vm ekuma.VM
	if _, err := runScript(ctx, &vm, "syntax", `var = 1`); err == nil {
		t.Errorf("a syntax error was not reported")
	} else if se := (*ekuma.SyntaxError)(nil); !errors.As(err, &se) {
		t.Errorf("got %T, want *ekuma.SyntaxError", err)
	}
	if _, err := runScript(ctx, &vm, "throw", `null.x`); err == nil {
		t.Errorf("an exception was not reported")
	} else if ex := (*ekuma.ScriptError)(nil); !errors.As(err, &ex) {
		t.Errorf("got %T, want *ekuma.ScriptError", err)
	}
	// The VM stays usable after an exception.
	v, err := runScript(ctx, &vm, "after", `1 + 1`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if n := v.Float(); n != 2 {
		t.Errorf("got %v, want 2", n)
	}
}

func TestHostJobBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		src   string
		call  func(context.Context, ekuma.Value) error
		throw bool
	}{
		{
			name: "getter",
			src:  `({ get x() { queue(); return 1; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := v.Get("x")
				return err
			},
		},
		{
			name: "setter",
			src:  `({ set x(v) { queue(); } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				return v.Set("x", 1)
			},
		},
		{
			name: "delete trap",
			src:  `new Proxy({}, { deleteProperty() { queue(); return true; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				return v.Delete("x")
			},
		},
		{
			name: "instanceof",
			src:  `({ [Symbol.hasInstance]() { queue(); return true; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := ekuma.Undefined.InstanceOf(v)
				return err
			},
		},
		{
			name: "throwing function",
			src:  `(function() { queue(); throw sentinel; })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := v.Invoke(ctx)
				return err
			},
			throw: true,
		},
		{
			name: "throwing method",
			src:  `({ x() { queue(); throw sentinel; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := v.Call(ctx, "x")
				return err
			},
			throw: true,
		},
		{
			name: "throwing method getter",
			src:  `({ get x() { queue(); throw sentinel; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := v.Call(ctx, "x")
				return err
			},
			throw: true,
		},
		{
			name: "throwing constructor",
			src:  `(function() { queue(); throw sentinel; })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := v.New(ctx)
				return err
			},
			throw: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			sentinel, err := runScript(t.Context(), &vm, "setup", `
				var sentinel = {};
				function queue() { Promise.resolve().then(() => { throw sentinel; }); }
				sentinel;
			`)
			if err != nil {
				t.Fatal(err)
			}
			v, err := runScript(t.Context(), &vm, "target", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			var reports int
			vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
				reports++
				if !reason.Equal(sentinel) {
					t.Error("rejection lost its identity")
				}
			})
			err = tc.call(t.Context(), v)
			if tc.throw {
				var ex *ekuma.ScriptError
				if !errors.As(err, &ex) || !ex.Value.Equal(sentinel) {
					t.Errorf("got %v, want the original thrown value", err)
				}
			} else if err != nil {
				t.Error(err)
			}
			if reports != 1 {
				t.Errorf("rejection reports before return = %d, want 1", reports)
			}
		})
	}
}

func TestReentrantGetterJobs(t *testing.T) {
	var vm ekuma.VM
	read := vm.FuncOf("read", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		_, err := args[0].Get("x")
		return nil, err
	})
	if err := vm.Global().Set("read", read); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "nested", `
		var ran = false;
		var sentinel = {};
		try {
			read({ get x() { Promise.resolve().then(() => ran = true); throw sentinel; } });
		} catch (e) {
			if (e !== sentinel) throw new Error("wrong exception");
		}
		ran;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Truthy() {
		t.Error("jobs ran during the reentrant getter")
	}
	v, err = vm.Global().Get("ran")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Truthy() {
		t.Error("jobs did not run at the outermost return")
	}
}

func TestThrowingCallJobInterruption(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := vm.Global().Set("cancel", vm.FuncOf("cancel", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		cancel()
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "setup", `
		var ran = false;
		(function() {
			Promise.resolve().then(() => { cancel(); for (;;) {} });
			Promise.resolve().then(() => ran = true);
			throw {};
		});
	`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Invoke(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Invoke: got %v, want context.Canceled", err)
	}
	v, err = runScript(t.Context(), &vm, "after", `ran`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Truthy() {
		t.Error("interrupted jobs were not discarded")
	}
}

func TestModuleLoaderJobBoundary(t *testing.T) {
	loaderErr := errors.New("loader failed")
	var vm ekuma.VM
	var reported bool
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		reported = true
	})
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		_, err := runScript(t.Context(), &vm, "loader", `Promise.reject("loader");`)
		if err != nil {
			t.Fatal(err)
		}
		if reported {
			t.Error("jobs ran inside the module loader")
		}
		return nil, loaderErr
	})
	m, err := vm.ParseModule("main", `import "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.LoadRequestedModules(); !errors.Is(err, loaderErr) {
		t.Errorf("LoadRequestedModules: got %v, want the loader error", err)
	}
	if !reported {
		t.Error("jobs did not run after the module loader failed")
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
	_, err := runScript(t.Context(), &vm, "order", `
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
	v, err := runScript(t.Context(), &vm, "log", `log.join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "call,script,job"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCancel(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "loop", `for (;;) {}`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	// A cancellation does not affect later calls.
	v, err := runScript(t.Context(), &vm, "after", `var n = 0; for (var i = 0; i < 1000; i++) n += i; n`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if n := v.Int(); n != 499500 {
		t.Errorf("got %v, want 499500", n)
	}
	// A context that is already done runs nothing.
	done, cancelDone := context.WithCancel(t.Context())
	cancelDone()
	if _, err := runScript(done, &vm, "never", `globalThis.ran = true`); !errors.Is(err, context.Canceled) {
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

// TestCancelLoopConditions tests that a context stops the loops whose conditions jump back.
func TestCancelLoopConditions(t *testing.T) {
	for _, src := range []string{
		`var i = 0, n = 1; while (i < n) {}`,
		`var i = 0, n = 1; while (i <= n) {}`,
		`var i = 1, z = 0; while (i > z) {}`,
		`var i = 1, z = 0; while (i >= z) {}`,
		`var i = 0; while (i < 1) {}`,
		`var i = 0; while (i <= 1) {}`,
		`var i = 1; while (i > 0) {}`,
		`var i = 1; while (i >= 0) {}`,
		`var o = {}; while (o) {}`,
		`var o = {}; while (o !== null) {}`,
		`var u; while (u === undefined) {}`,
		`var u; while (u == null) {}`,
		`var o = {}; while (o != null) {}`,
		// Conditions that take the full comparison or ToBoolean.
		`var s = "a"; while (s < "b") {}`,
		`var x = 0n; while (x < 1) {}`,
		`var s = "s"; do {} while (s);`,
	} {
		var vm ekuma.VM
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		_, err := runScript(ctx, &vm, "loop", src)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want context.DeadlineExceeded", src, err)
		}
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
	_, err := runScript(ctx, &vm, "nested", `
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
	if v, err := runScript(t.Context(), &vm, "after", `1 + 1`); err != nil || v.Int() != 2 {
		t.Errorf("after the interruption: %v, %v, want 2", v, err)
	}
}

func checkArgumentStorage(t *testing.T, vm *ekuma.VM, wantArgs, wantCallArgs int) {
	t.Helper()
	args, callArgs, cleared := ekuma.ArgumentStorage(vm)
	if args != wantArgs || callArgs != wantCallArgs || !cleared {
		t.Errorf("argument storage = (%d, %d, cleared=%v), want (%d, %d, cleared=true)", args, callArgs, cleared, wantArgs, wantCallArgs)
	}
}

func TestArgumentStorageInterrupted(t *testing.T) {
	var vm ekuma.VM
	loop, err := runScript(t.Context(), &vm, "loop", `(object, cancel) => { cancel(); for (;;) {} }`)
	if err != nil {
		t.Fatal(err)
	}
	returned := false
	outer := vm.FuncOf("outer", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		v, err := loop.Invoke(ctx, args[0], ekuma.Func(func(ekuma.Value, []ekuma.Value) (any, error) {
			cancel()
			return nil, nil
		}))
		returned = true
		return v, err
	})
	if err := vm.Global().Set("outer", outer); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		_, _ = runScript(t.Context(), &vm, "outer", `outer({})`)
		if returned {
			t.Error("host callback returned after interruption")
		}
		checkArgumentStorage(t, &vm, 0, 0)
	}
}

func TestArgumentStorageConversionPanicReentrant(t *testing.T) {
	var vm ekuma.VM
	inner := vm.FuncOf("inner", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return args[0], nil
	})
	outer := vm.FuncOf("outer", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("invalid argument conversion did not panic")
				}
			}()
			_, _ = inner.Invoke(t.Context(), args[0], struct{}{})
		}()
		checkArgumentStorage(t, &vm, 1, 1)
		v, err := inner.Invoke(t.Context(), args[0])
		if err != nil {
			t.Fatal(err)
		}
		checkArgumentStorage(t, &vm, 1, 1)
		if !v.Equal(args[0]) {
			t.Error("nested call changed the outer argument")
		}
		return args[0], nil
	})
	object := vm.ValueOf(map[string]any{})
	v, err := outer.Invoke(t.Context(), object)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(object) {
		t.Error("outer call did not preserve its argument")
	}
	checkArgumentStorage(t, &vm, 0, 0)
}

func TestCancelNestedContext(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, errors.New("nested cancellation")} {
		t.Run(cause.Error(), func(t *testing.T) {
			var vm ekuma.VM
			var returned bool
			invoke := vm.FuncOf("invoke", func(this ekuma.Value, args []ekuma.Value) (any, error) {
				ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
				defer cancel()
				v, err := args[0].Invoke(ctx)
				returned = true
				return v, err
			})
			if err := vm.Global().Set("invoke", invoke); err != nil {
				t.Fatalf("Set: %v", err)
			}
			_, err := runScript(context.Background(), &vm, "nested", `
				var caught = false;
				try { invoke(() => { while (true) {} }); } catch (e) { caught = true; }`)
			if !errors.Is(err, cause) {
				t.Errorf("got %v, want %v", err, cause)
			}
			if returned {
				t.Error("the Func continued after the interruption")
			}
			if v, err := runScript(t.Context(), &vm, "after", `caught ? 0 : 1 + 1`); err != nil || v.Int() != 2 {
				t.Errorf("after the interruption: %v, %v, want 2", v, err)
			}
		})
	}
}

// TestCancelNestedDoneContext checks that a nested call with a context that is already done
// stops everything, as a context that becomes done during the call does.
func TestCancelNestedDoneContext(t *testing.T) {
	cause := errors.New("done before the call")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	for _, call := range []func(ekuma.Value) error{
		func(f ekuma.Value) error {
			_, err := f.Invoke(ctx)
			return err
		},
		func(f ekuma.Value) error {
			_, err := f.Call(ctx, "toString")
			return err
		},
	} {
		var vm ekuma.VM
		var returned bool
		if err := vm.Global().Set("h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
			err := call(args[0])
			returned = true
			return nil, err
		})); err != nil {
			t.Fatalf("Set: %v", err)
		}
		v, err := runScript(t.Context(), &vm, "nested", `var caught = false; try { h(() => {}); } catch (e) { caught = true; } 1`)
		if !errors.Is(err, cause) {
			t.Errorf("got %v, %v, want %v", v, err, cause)
		}
		if returned {
			t.Error("the Func continued after the interruption")
		}
		if v, err := runScript(t.Context(), &vm, "after", `caught ? 0 : 1 + 1`); err != nil || v.Int() != 2 {
			t.Errorf("after the interruption: %v, %v, want 2", v, err)
		}
		// The outermost call returns the cause without running.
		if _, err := runScript(ctx, &vm, "outermost", `caught = true`); !errors.Is(err, cause) {
			t.Errorf("outermost call: got %v, want %v", err, cause)
		}
		if v, err := runScript(t.Context(), &vm, "after", `caught ? 0 : 1 + 1`); err != nil || v.Int() != 2 {
			t.Errorf("after the outermost call: %v, %v, want 2", v, err)
		}
	}
}

// TestCancelNestedUnobserved checks that a nested call that returns without observing the
// cancellation of its context leaves the cancellation to the calls around it.
func TestCancelNestedUnobserved(t *testing.T) {
	var vm ekuma.VM
	global := vm.Global()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	slow := vm.FuncOf("slow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		cancel()
		// The cancellation is requested on another goroutine.
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	})
	if err := global.Set("h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return slow.Invoke(ctx)
	})); err != nil {
		t.Fatalf("Set: %v", err)
	}
	errTooLong := errors.New("the script was not stopped")
	var ticks int
	if err := global.Set("tick", vm.FuncOf("tick", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		ticks++
		if ticks > 1_000_000 {
			return nil, errTooLong
		}
		return nil, nil
	})); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := runScript(ctx, &vm, "nested", `h(); for (;;) { tick(); }`); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	// The cancellation does not affect later calls.
	if v, err := runScript(t.Context(), &vm, "after", `for (var i = 0; i < 1000; i++) {} 1 + 1`); err != nil || v.Int() != 2 {
		t.Errorf("after the cancellation: %v, %v, want 2", v, err)
	}
}

// TestCancelNestedContextUnobserved checks that the context of a nested call that returned
// does not stop the calls around it.
func TestCancelNestedContextUnobserved(t *testing.T) {
	var vm ekuma.VM
	global := vm.Global()
	if err := global.Set("h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		slow := vm.FuncOf("slow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
			cancel()
			// The cancellation is requested on another goroutine.
			time.Sleep(50 * time.Millisecond)
			return nil, nil
		})
		return slow.Invoke(ctx)
	})); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := runScript(t.Context(), &vm, "nested", `h(); var n = 0; for (var i = 0; i < 1000; i++) n += i; n`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.Int(), 499500; got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

// TestCancelNestedRecovered checks that a Func that recovers the interruption of a nested call
// does not stop it, whether the outer or the inner context stopped the call.
func TestCancelNestedRecovered(t *testing.T) {
	cause := errors.New("inner cancellation")
	for _, tc := range []struct {
		name     string
		outerCtx func(t *testing.T) context.Context
		innerCtx func(t *testing.T) context.Context
		want     error
	}{
		{
			name: "outer",
			outerCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			innerCtx: func(t *testing.T) context.Context { return t.Context() },
			want:     context.DeadlineExceeded,
		},
		{
			name:     "inner",
			outerCtx: func(t *testing.T) context.Context { return t.Context() },
			innerCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
				t.Cleanup(cancel)
				return ctx
			},
			want: cause,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			var recovered, ranAgain bool
			loop, err := runScript(t.Context(), &vm, "loop", `() => { for (;;) {} }`)
			if err != nil {
				t.Fatal(err)
			}
			again := vm.FuncOf("again", func(ekuma.Value, []ekuma.Value) (any, error) {
				ranAgain = true
				return nil, nil
			})
			invoke := vm.FuncOf("invoke", func(ekuma.Value, []ekuma.Value) (res any, err error) {
				defer func() {
					if r := recover(); r != nil {
						recovered = true
						// A recovered interruption stops the later calls too.
						func() {
							defer func() { _ = recover() }()
							_, _ = again.Invoke(t.Context())
						}()
						res, err = nil, fmt.Errorf("recovered: %v", r)
					}
				}()
				return loop.Invoke(tc.innerCtx(t))
			})
			if err := vm.Global().Set("invoke", invoke); err != nil {
				t.Fatal(err)
			}
			_, err = runScript(tc.outerCtx(t), &vm, "nested", `
				var caught = false, after = false;
				try { invoke(); } catch (e) { caught = true; }
				after = true;`)
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
			if !recovered {
				t.Error("the Func did not recover the interruption")
			}
			if ranAgain {
				t.Error("a call after the recovered interruption ran")
			}
			if v, err := runScript(t.Context(), &vm, "after", `caught || after ? 0 : 1 + 1`); err != nil || v.Int() != 2 {
				t.Errorf("after the interruption: %v, %v, want 2", v, err)
			}
		})
	}
}

// TestCancelFillGeneric checks that Array.prototype.fill on an object that is not an array stops
// when its context is done.
func TestCancelFillGeneric(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := vm.Global().Set("cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
		cancel()
		// The cancellation is requested on another goroutine.
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	// Writes through the typed array prototype store nothing, and the length is small enough
	// for the loop to end quickly when it does not stop.
	_, err := runScript(ctx, &vm, "fill", `
		var o = Object.create(new Uint8Array(0));
		Object.defineProperty(o, "length", { value: 4 * 65536 });
		var after = false;
		Array.prototype.fill.call(o, 0, { valueOf() { cancel(); return 0; } });
		after = true;`)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if v, err := vm.Global().Get("after"); err != nil || v.Truthy() {
		t.Errorf("after = %v, %v: the script continued after fill", v, err)
	}
}

func TestInternalError(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set("fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
		t.Fatal(err)
	}
	_, err := runScript(t.Context(), &vm, "failure", `
  var caught = false, finalized = false, jobRan = false;
  function* work() { try { fail(); } catch (e) { caught = true; } finally { finalized = true; } }
  var iterator = work();
  Promise.resolve().then(() => { jobRan = true; });
  iterator.next();
 `)
	var internal *ekuma.InternalError
	if !errors.As(err, &internal) || !errors.Is(err, cause) {
		t.Errorf("Evaluate: %T %v, want InternalError wrapping cause", err, err)
	}
	if _, ok := errors.AsType[*ekuma.ScriptError](err); ok {
		t.Errorf("internal failure became a ScriptError: %v", err)
	}
	v, err := runScript(t.Context(), &vm, "after", `!caught && !finalized && !jobRan && iterator.next().done`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Error("abandoned execution resumed")
	}
}

func TestInternalErrorReentrant(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	fail := ekuma.InternalFailureFunc(&vm, cause)
	var continued bool
	if err := vm.Global().Set("host", vm.FuncOf("host", func(ekuma.Value, []ekuma.Value) (any, error) {
		_, err := fail.Invoke(t.Context())
		continued = true
		return nil, err
	})); err != nil {
		t.Fatal(err)
	}
	_, err := runScript(t.Context(), &vm, "nested", `try { host(); } catch (e) {}`)
	if !errors.Is(err, cause) {
		t.Errorf("Evaluate: %v, want cause", err)
	}
	if continued {
		t.Error("host callback continued after internal failure")
	}
}

func TestInternalErrorRecoveredByHost(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	fail := ekuma.InternalFailureFunc(&vm, cause)
	if err := vm.Global().Set("host", vm.FuncOf("host", func(ekuma.Value, []ekuma.Value) (any, error) {
		recovered := recoverHostPanic(func() { _, _ = fail.Invoke(t.Context()) })
		if recovered == nil {
			t.Error("inner call did not unwind")
		}
		return 7, nil
	})); err != nil {
		t.Fatal(err)
	}
	_, err := runScript(t.Context(), &vm, "recovered", `
  var jobRan = false;
  Promise.resolve().then(() => { jobRan = true; });
  host();
 `)
	if !errors.Is(err, cause) {
		t.Errorf("Evaluate: %v, want recovered internal failure", err)
	}
	v, err := runScript(t.Context(), &vm, "after", `jobRan`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool() {
		t.Error("jobs ran after a recovered internal failure")
	}
}

func TestInternalErrorModule(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set("fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
		t.Fatal(err)
	}
	m, err := vm.ParseModule("failure", `fail();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); !errors.Is(err, cause) {
		t.Errorf("Evaluate: %v, want cause", err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state, reason := p.PromiseState()
	if state != ekuma.PromiseRejected {
		t.Errorf("promise state: %v, want rejected", state)
		return
	}
	var internal *ekuma.InternalError
	exception := &ekuma.ScriptError{
		Value: reason,
	}
	if err := exception.Unwrap(); !errors.As(err, &internal) || !errors.Is(err, cause) {
		t.Errorf("rejection cause: %T %v, want InternalError wrapping cause", err, err)
	}
}

func TestInternalErrorJob(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set("fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
		t.Fatal(err)
	}
	_, err := runScript(t.Context(), &vm, "job", `Promise.resolve().then(fail);`)
	if !errors.Is(err, cause) {
		t.Errorf("job failure: %v, want cause", err)
	}
}

// TestGoPanic checks that a Go panic that passes through the VM leaves it as usable as
// an interruption does.
func TestGoPanic(t *testing.T) {
	var vm, foreignVM ekuma.VM
	foreign := foreignVM.Global()
	global := vm.Global()
	// Returning another VM's object raises an ownership panic.
	if err := global.Set("bad", vm.FuncOf("bad", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return foreign, nil
	})); err != nil {
		t.Fatalf("Set: %v", err)
	}
	panics := func(f func()) (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		f()
		return false
	}
	// state is the recursion depth that is left and the stack trace of a new script.
	state := func() string {
		t.Helper()
		v, err := runScript(t.Context(), &vm, "state", `
			function d(n) { try { return d(n + 1); } catch (e) { return n; } }
			d(0) + "\n" + new Error().stack`)
		if err != nil {
			t.Fatalf("runScript: %v", err)
		}
		return v.String()
	}
	want := state()

	for range 3 {
		if !panics(func() {
			_, _ = runScript(t.Context(), &vm, "panic", `function f(n) { return n ? f(n - 1) : bad(); } f(100)`)
		}) {
			t.Fatal("the script did not panic")
		}
	}
	if got := state(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// The panic ends the module evaluation in progress.
	m, err := vm.ParseModule("panic", `bad();`)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	if !panics(func() {
		_, _ = m.Evaluate(t.Context())
	}) {
		t.Fatal("the module did not panic")
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, _ := p.PromiseState(); st != ekuma.PromiseRejected {
		t.Errorf("got %v, want rejected", st)
	}
}

func recoverHostPanic(f func()) (value any) {
	defer func() { value = recover() }()
	f()
	return nil
}

// TestHostPanicReentrantRecovery checks that a host can recover and continue its caller.
func TestHostPanicReentrantRecovery(t *testing.T) {
	var vm ekuma.VM
	marker := new(int)
	if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
		panic(marker)
	})); err != nil {
		t.Fatal(err)
	}
	inner, err := vm.ParseScript("inner", `
		function abandoned() {
			let x = 42; saved = () => x;
			try { bad(); } catch (_) { caught = true; } finally { finalized = true; }
		}
		Promise.resolve().then(() => jobs++);
		abandoned();`)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Global().Set("recoverInner", vm.FuncOf("recoverInner", func(ekuma.Value, []ekuma.Value) (any, error) {
		// Repeated failures exhaust the call limit if abandoned frames remain.
		for range 400 {
			if got := recoverHostPanic(func() { _, _ = inner.Evaluate(t.Context()) }); got != marker {
				t.Errorf("panic = %v, want original marker", got)
			}
		}
		return 7, nil
	})); err != nil {
		t.Fatal(err)
	}
	if got := run(t, &vm, `
		var saved, caught = false, finalized = false, jobs = 0;
		function outer() { let x = 10; return recoverInner() + x; }
		[outer(), saved(), caught, finalized].join()`); got != "17,42,false,false" {
		t.Errorf("result = %s", got)
	}
	if got := run(t, &vm, `jobs`); got != "0" {
		t.Errorf("abandoned jobs ran: %s", got)
	}
}

// TestHostPanicGenerators checks that aborted activations close without running finally.
func TestHostPanicGenerators(t *testing.T) {
	var vm ekuma.VM
	marker := new(int)
	if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
		panic(marker)
	})); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `
		var saved, finalized = false;
		var g = (function* () {
			let x = 42; saved = () => x;
			try { bad(); } finally { finalized = true; }
		})();
		var ag = (async function* () { bad(); })();`)
	for _, src := range []string{`g.next()`, `ag.next()`} {
		if got := recoverHostPanic(func() { _, _ = runScript(t.Context(), &vm, "generator", src) }); got != marker {
			t.Errorf("panic = %v, want original marker", got)
		}
	}
	if got := run(t, &vm, `[g.next().done, saved(), finalized].join()`); got != "true,42,false" {
		t.Errorf("generator after abort = %s", got)
	}
	run(t, &vm, `var done; ag.next().then(r => done = r.done);`)
	if got := run(t, &vm, `done`); got != "true" {
		t.Errorf("async generator after abort = %s", got)
	}
}

// TestHostPanicReentrantModule checks that a recovered panic cannot expose a partial module.
func TestHostPanicReentrantModule(t *testing.T) {
	var vm ekuma.VM
	marker := errors.New("host panic")
	if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
		panic(marker)
	})); err != nil {
		t.Fatal(err)
	}
	m, err := vm.ParseModule("abandoned", `bad()`)
	if err != nil {
		t.Fatal(err)
	}
	vm.SetModuleLoader(func(ekuma.ModuleRequest) (*ekuma.Module, error) { return m, nil })
	importer, err := vm.ParseModule("importer", `import "abandoned"; globalThis.importerRan = true;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Global().Set("recoverModule", vm.FuncOf("recoverModule", func(ekuma.Value, []ekuma.Value) (any, error) {
		if got := recoverHostPanic(func() { _, _ = m.Evaluate(t.Context()) }); got != marker {
			t.Errorf("panic = %v, want original marker", got)
			return nil, nil
		}
		p, err := importer.Evaluate(t.Context())
		if err != nil {
			t.Errorf("Evaluate importer: %v", err)
			return nil, nil
		}
		state, reason := p.PromiseState()
		if state != ekuma.PromiseRejected {
			t.Errorf("importer state = %v, want rejected", state)
		}
		if err := (&ekuma.ScriptError{Value: reason}); !errors.Is(err, marker) {
			t.Errorf("importer error = %v, want original cause", err)
		}
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	if got := run(t, &vm, `var importerRan = false; recoverModule(); importerRan`); got != "false" {
		t.Errorf("importer ran against partial module: %s", got)
	}
}

// TestHostPanicJobRecovery checks that recovered panics stop the current job drain.
func TestHostPanicJobRecovery(t *testing.T) {
	for _, reports := range []bool{false, true} {
		t.Run(fmt.Sprintf("reports=%t", reports), func(t *testing.T) {
			var vm ekuma.VM
			marker := new(int)
			if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
				panic(marker)
			})); err != nil {
				t.Fatal(err)
			}
			inner, err := vm.ParseScript("inner", `bad()`)
			if err != nil {
				t.Fatal(err)
			}
			recoverInner := func() {
				if got := recoverHostPanic(func() { _, _ = inner.Evaluate(t.Context()) }); got != marker {
					t.Errorf("panic = %v, want original marker", got)
				}
			}
			if reports {
				var notified int
				vm.SetUnhandledRejection(func(ekuma.Value, ekuma.Value) {
					notified++
					recoverInner()
				})
				run(t, &vm, `Promise.reject(1); Promise.reject(2);`)
				if notified != 1 {
					t.Errorf("reports = %d, want 1", notified)
				}
			} else {
				if err := vm.Global().Set("recoverInner", vm.FuncOf("recoverInner", func(ekuma.Value, []ekuma.Value) (any, error) {
					recoverInner()
					return nil, nil
				})); err != nil {
					t.Fatal(err)
				}
				run(t, &vm, `var jobs = 0; Promise.resolve().then(recoverInner); Promise.resolve().then(() => jobs++);`)
				if got := run(t, &vm, `jobs`); got != "0" {
					t.Errorf("abandoned jobs ran: %s", got)
				}
			}
		})
	}
}

// TestHostPanicAsyncGeneratorStep checks aborts after the interpreter frame is gone.
func TestHostPanicAsyncGeneratorStep(t *testing.T) {
	for _, src := range []string{
		`var g = (async function* () {})(); g.return({get then() { bad(); }});`,
		`var g = (async function* () { await 0; })();
		 Object.defineProperty(Object.prototype, "then", { configurable: true, get() {
			 delete Object.prototype.then; bad();
		 }}); g.next();`,
	} {
		t.Run(src, func(t *testing.T) {
			var vm ekuma.VM
			marker := new(int)
			if err := vm.Global().Set("bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
				panic(marker)
			})); err != nil {
				t.Fatal(err)
			}
			if got := recoverHostPanic(func() { _, _ = runScript(t.Context(), &vm, "step", src) }); got != marker {
				t.Errorf("panic = %v, want original marker", got)
				return
			}
			run(t, &vm, `var done; g.next().then(r => done = r.done);`)
			if got := run(t, &vm, `done`); got != "true" {
				t.Errorf("async generator after abort = %s", got)
			}
		})
	}
}

// TestCancelRegExp checks that a match that backtracks for too long can be stopped.
func TestCancelRegExp(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "backtrack", `/^(a+)+$/.test("a".repeat(40) + "b")`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	// Each match of a global replace is cheap, but there are many of them.
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "global", `"x".repeat(1 << 26).replace(/x/g, "y")`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("global replace: got %v, want context.DeadlineExceeded", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "escape", `RegExp.escape(",".repeat(1 << 26))`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("escape: got %v, want context.DeadlineExceeded", err)
	}
	v, err := runScript(t.Context(), &vm, "after", `"a1b22".replace(/\d+/g, function(m) { return "<" + m + ">"; })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "a<1>b<22>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCancelArrayIteration checks that iterating or filling a long array can be stopped.
func TestCancelArrayIteration(t *testing.T) {
	var vm ekuma.VM
	for _, src := range []string{
		`new Set(new Array(2 ** 32 - 1)).size`,
		`new Uint8Array(new Array(2 ** 32 - 1)).length`,
		`new Array(2 ** 32 - 1).fill(0).length`,
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		_, err := runScript(ctx, &vm, "iterate", src)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want context.DeadlineExceeded", src, err)
		}
	}
}

// TestCancelJSON checks that parsing a long text can be stopped.
func TestCancelJSON(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `var text = "[" + "[0],".repeat(5000000) + "0]";`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "parse", `JSON.parse(text)`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	v, err := runScript(t.Context(), &vm, "after", `JSON.stringify(JSON.parse('{"a":[1]}'))`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), `{"a":[1]}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestCancelBuiltins checks that a long call of a built-in function that runs no script code
// stops soon after its context is done.
func TestCancelBuiltins(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup string
		src   string
	}{
		{
			name:  "bulk copy",
			setup: `var a = new Array(1 << 22).fill(0);`,
			src:   `a.concat(a, a, a)`,
		},
		{
			name:  "sort",
			setup: `var a = []; for (let i = 0; i < 1 << 20; i++) a.push(Math.random());`,
			src:   `a.sort()`,
		},
		{
			name:  "sort in runs",
			setup: `var a = new Float64Array(1 << 23); for (let i = 0; i < a.length; i++) a[i] = Math.random();`,
			src:   `a.sort()`,
		},
		{
			name:  "string building",
			setup: `var s = "x".repeat(1 << 24);`,
			src:   `s.replaceAll("x", "yy")`,
		},
		{
			name:  "BigInt conversion",
			setup: `var s = "9".repeat(2e6);`,
			src:   `BigInt(s)`,
		},
		{
			// The marks have no boundary of a normalization form between them.
			name:  "normalization of a run of marks",
			setup: `var s = "a" + "\u0327\u0328\u0301".repeat(2 ** 22);`,
			src:   `s.normalize()`,
		},
		{
			name:  "property enumeration",
			setup: `var o = {}; for (let i = 0; i < 1 << 20; i++) o["k" + i] = i;`,
			src:   `Object.assign({}, o)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			if _, err := runScript(t.Context(), &vm, "setup", tc.setup); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := runScript(ctx, &vm, "call", tc.src)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("the call stopped after %v", d)
			}
		})
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
			if _, err := runScript(ctx, &vm, "endless", tt.src); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			if _, err := runScript(t.Context(), &vm, "after", `var before = count; var r = 0; Promise.resolve(5).then(v => { r = v; });`); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			v, err := runScript(t.Context(), &vm, "check", `[count > 0, count === before, r].join()`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
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
			if _, err := runScript(ctx, &vm, tt.name, tt.src); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
		})
	}
}

// TestBigIntInterrupt checks that a context stops long operations on huge BigInts promptly.
func TestBigIntInterrupt(t *testing.T) {
	var vm ekuma.VM
	// Powers of three have dense bits, which math/big takes long to multiply and divide.
	if _, err := runScript(t.Context(), &vm, "prefix", `var y = 3n ** 16000000n, x = y * y, z = (x << 25400000n) + y;`); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		`x * x`,
		`z / y`,
		`z % y`,
		// The lexer converts the digits to decimal.
		`eval("0x" + "f".repeat(2 ** 23) + "n")`,
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		start := time.Now()
		_, err := runScript(ctx, &vm, src, src)
		// Each of these takes seconds in one step of math/big.
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("%s: stopped after %v", src, elapsed)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want context.DeadlineExceeded", src, err)
		}
		cancel()
	}
}

// TestCancelModule checks that a context stops a module whose top level loops forever.
func TestCancelModule(t *testing.T) {
	errStop := errors.New("stop")
	for _, src := range []string{
		`for (;;) {}`,
		`await null; for (;;) {}`,
	} {
		for _, cause := range []error{context.DeadlineExceeded, errStop} {
			var vm ekuma.VM
			m, err := vm.ParseModule("endless", src)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
			if _, err := m.Evaluate(ctx); !errors.Is(err, cause) {
				t.Errorf("%s: got %v, want %v", src, err, cause)
			}
			cancel()

			// The interruption evaluates the module with an Error made from the cause, which an
			// importer gets too.
			vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
				return m, nil
			})
			importer, err := vm.ParseModule("importer", `import "endless";`)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range []*ekuma.Module{m, importer} {
				p, err := m.Evaluate(t.Context())
				if err != nil {
					t.Fatalf("%s: Evaluate %s: %v", src, m.Name(), err)
				}
				st, r := p.PromiseState()
				if st != ekuma.PromiseRejected {
					t.Errorf("%s: %s: got %v, want rejected", src, m.Name(), st)
					continue
				}
				if err := (&ekuma.ScriptError{Value: r}); !errors.Is(err, cause) {
					t.Errorf("%s: %s: got reason %v, want an Error made from %v", src, m.Name(), err, cause)
				}
			}
		}
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
			p, err := runScript(t.Context(), &vm, tt.name, tt.src)
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
	thenable, err := runScript(t.Context(), &vm, "thenable", `({ then(f) { f(1); } })`)
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

func TestUnhandledRejection(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "reject",
			src:  `Promise.reject("a"); Promise.reject("b")`,
			want: []string{"a", "b"},
		},
		{
			name: "handled by a job",
			src:  `const p = Promise.reject("a"); Promise.resolve().then(() => p.catch(() => {}))`,
			want: nil,
		},
		{
			name: "state read",
			src:  `inspect(Promise.reject("a"))`,
			want: []string{"a"},
		},
		{
			name: "async function",
			src:  `(async () => { throw "a"; })()`,
			want: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			var got []string
			vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
				got = append(got, reason.String())
			})
			inspect := vm.FuncOf("inspect", func(this ekuma.Value, args []ekuma.Value) (any, error) {
				args[0].PromiseState()
				return nil, nil
			})
			if err := vm.Global().Set("inspect", inspect); err != nil {
				t.Fatal(err)
			}
			if _, err := runScript(t.Context(), &vm, tt.name, tt.src); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	// A report can run JavaScript that rejects another promise.
	var vm ekuma.VM
	var got []string
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		got = append(got, reason.String())
		if reason.String() == "a" {
			if _, err := runScript(t.Context(), &vm, "report", `Promise.resolve().then(() => { throw "b"; })`); err != nil {
				t.Error(err)
			}
		}
	})
	if _, err := runScript(t.Context(), &vm, "reentry", `Promise.reject("a")`); err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
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
	var ex *ekuma.ScriptError
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

func TestDeferredModuleRejectionReporting(t *testing.T) {
	var vm ekuma.VM
	var reported []ekuma.Value
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		reported = append(reported, promise)
	})
	modules := newMemoryModules(&vm, map[string]string{
		"dep": `throw "deferred";`,
		"main": `import defer * as ns from "dep";
			try { ns.x; } catch (e) { globalThis.result = Promise.reject(e); }`,
	})
	want := modules.run(t, "main")
	if len(reported) != 1 || !reported[0].Equal(want) {
		t.Errorf("reported promises = %v, want only the promise created in catch", reported)
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
			name: "ScriptError",
			err:  &ekuma.ScriptError{Value: thrown},
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
			var ex *ekuma.ScriptError
			if err := m.LoadRequestedModules(); !errors.As(err, &ex) {
				t.Fatalf("got %v, want a *ScriptError", err)
			}
			if tt.ctor == "" {
				if !ex.Value.Equal(thrown) {
					t.Errorf("got %v, want the value of the *ScriptError", ex.Value)
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
	var ex *ekuma.ScriptError
	if err := bad.Link(); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}
}

// TestModuleLoaderReentry checks that linking a module from inside the loader that loads it
// is an error rather than a recursion.
func TestModuleLoaderReentry(t *testing.T) {
	var vm ekuma.VM
	var reentryErr error
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		reentryErr = req.Referrer.Link()
		return vm.ParseModule(req.Specifier, `export const v = 1;`)
	})
	m, err := vm.ParseModule("main", `import { v } from "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link: %v", err)
	}
	var ex *ekuma.ScriptError
	if !errors.As(reentryErr, &ex) || !isInstance(t, &vm, ex.Value, "TypeError") {
		t.Errorf("got %v from the re-entrant Link, want a TypeError", reentryErr)
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

	var ex *ekuma.ScriptError
	if _, err := vm.ParseJSONModule("bad.json", `{`); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}
}

func TestModuleSource(t *testing.T) {
	var vm ekuma.VM
	src := ekuma.NewSourceModule(vm.InitialRealm(), "src", "TestModuleSource")
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return src, nil
	})
	m, err := vm.ParseModule("main", `import source s from "src";
		const d = await import.source("src");
		globalThis.result = (s === d) + " " + Object.prototype.toString.call(s);`)
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
	if got, want := v.String(), "true [object TestModuleSource]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPrototypeCollected(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "make", `
		function make() {
			class C {}
			return new C();
		}
		var refs = [];
		for (let i = 0; i < 1000; i++) {
			refs.push(new WeakRef(Object.getPrototypeOf(make())));
		}
	`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "count", `refs.filter(r => r.deref() !== undefined).length`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	// The inline caches of make may keep the most recent prototypes alive.
	if got, max := v.Float(), 2.0; got > max {
		t.Errorf("got %v live prototypes, want at most %v", got, max)
	}
}

// TestTemporariesCollected checks that the objects of a call that returned to Go are not kept
// alive by the VM.
func TestTemporariesCollected(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "make", `
		function make() {
			return [new WeakRef({}), new WeakRef(this)];
		}
		var refs = make.call({});
	`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "deref", `refs.map(r => r.deref() === undefined).join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	// The first object was a register of make, and the second its this.
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("collected: got %s, want %s", got, want)
	}
}

// TestCancelTailCalls checks that a context stops endless tail recursion.
func TestCancelTailCalls(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "loop", `"use strict"; function f(n) { return g(n + 1); } function g(n) { return f(n); } f(0)`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
}

// TestScriptRealm checks that a script evaluates in the realm that it was parsed in.
func TestScriptRealm(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	other := r.Global()
	s, err := r.ParseScript("other", `var inOther = 1; [globalThis, this, Object.getPrototypeOf([]) === Array.prototype]`)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}
	v, err := s.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for i, name := range []string{"globalThis", "this"} {
		got, err := v.Index(i)
		if err != nil {
			t.Fatalf("Index: %v", err)
		}
		if !got.Equal(other) {
			t.Errorf("%s is not the global object of the realm of the script", name)
		}
	}
	if got, err := v.Index(2); err != nil || !got.Bool() {
		t.Errorf("an array literal does not get the Array.prototype of the realm of the script: %v, %v", got, err)
	}
	if got, err := other.Get("inOther"); err != nil || got.Int() != 1 {
		t.Errorf("inOther in the realm of the script: got %v, %v, want 1", got, err)
	}
	if got, err := vm.Global().Get("inOther"); err != nil || !got.IsUndefined() {
		t.Errorf("inOther in the realm of the VM: got %v, %v, want undefined", got, err)
	}
}

// TestRealmValues checks which realm the values that Go creates belong to.
func TestRealmValues(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	g := vm.Global()
	if err := g.Set("other", r.Global()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	inner := r.FuncOf("inner", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The methods of the VM work in the initial realm even in a function of another realm.
		if err := g.Set("fromVM", vm.ValueOf([]any{})); err != nil {
			return nil, err
		}
		if err := g.Set("fromSet", []any{}); err != nil {
			return nil, err
		}
		return nil, errors.New("thrown")
	})
	values := map[string]ekuma.Value{
		"array":  r.ValueOf([]any{}),
		"object": r.ValueOf(map[string]any{}),
		"realmFn": r.ValueOf(ekuma.Func(func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return []any{}, nil
		})),
		"inner": inner,
		"vmFn": vm.FuncOf("vmFn", func(this ekuma.Value, args []ekuma.Value) (any, error) {
			return nil, nil
		}),
	}
	for name, v := range values {
		if err := g.Set(name, v); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}
	v, err := runScript(t.Context(), &vm, "check", `
		let error;
		try { inner(); } catch (e) { error = e; }
		[
			other.Array !== Array,
			Object.getPrototypeOf(array) === other.Array.prototype,
			Object.getPrototypeOf(object) === other.Object.prototype,
			Object.getPrototypeOf(realmFn) === other.Function.prototype,
			Object.getPrototypeOf(realmFn()) === other.Array.prototype,
			Object.getPrototypeOf(inner) === other.Function.prototype,
			error instanceof other.Error,
			Object.getPrototypeOf(vmFn) === Function.prototype,
			Object.getPrototypeOf(fromVM) === Array.prototype,
			Object.getPrototypeOf(fromSet) === Array.prototype,
		].join()`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got, want := v.String(), "true,true,true,true,true,true,true,true,true,true"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestValueOfMapOwnProperties(t *testing.T) {
	for _, name := range []string{"VM", "Realm"} {
		t.Run(name, func(t *testing.T) {
			var vm ekuma.VM
			r := vm.InitialRealm()
			valueOf := vm.ValueOf
			if name == "Realm" {
				r = vm.NewRealm()
				valueOf = r.ValueOf
			}
			if err := vm.Global().Set("target", r.Global()); err != nil {
				t.Fatal(err)
			}
			run(t, &vm, `
				var setterCalls = 0;
				Object.defineProperty(target.Object.prototype, "blocked", {
					set() { setterCalls++; throw new Error("setter called"); }
				});
			`)
			v := valueOf(map[string]any{
				"z":       1,
				"blocked": 42,
				"__proto__": map[string]any{
					"injected": true,
				},
			})
			if err := vm.Global().Set("converted", v); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				src  string
				want string
			}{
				{
					src:  `Object.getPrototypeOf(converted) === target.Object.prototype`,
					want: "true",
				},
				{
					src:  `Object.keys(converted).join()`,
					want: "__proto__,blocked,z",
				},
				{
					src:  `converted.__proto__.injected`,
					want: "true",
				},
				{
					src:  `Object.getPrototypeOf(converted.__proto__) === target.Object.prototype`,
					want: "true",
				},
				{
					src:  `converted.blocked`,
					want: "42",
				},
				{
					src:  `setterCalls`,
					want: "0",
				},
				{
					src: `Object.keys(converted).every(key => {
						const d = Object.getOwnPropertyDescriptor(converted, key);
						return "value" in d && d.writable && d.enumerable && d.configurable;
					})`,
					want: "true",
				},
			} {
				if got := run(t, &vm, tc.src); got != tc.want {
					t.Errorf("%s = %s, want %s", tc.src, got, tc.want)
				}
			}
		})
	}
}

func TestValueOfUnnamedFunc(t *testing.T) {
	var vm ekuma.VM
	v := vm.ValueOf([]any{func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return 7, nil
	}})
	f, err := v.Get("0")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.Invoke(t.Context()); err != nil || got.Int() != 7 {
		t.Errorf("Invoke: %v, %v, want 7", got, err)
	}
}

func TestValueOfNilBigInt(t *testing.T) {
	var vm ekuma.VM
	r := recoverHostPanic(func() { vm.ValueOf((*big.Int)(nil)) })
	if r == nil {
		t.Fatal("ValueOf did not panic")
	}
	if _, ok := r.(runtime.Error); ok {
		t.Errorf("ValueOf panicked with a runtime error: %v", r)
	}
}

func TestValueOfCycles(t *testing.T) {
	var vm ekuma.VM
	cyclicMap := map[string]any{}
	cyclicMap["a"] = []any{cyclicMap}
	cyclicSlice := []any{1, nil}
	cyclicSlice[1] = map[string]any{"s": cyclicSlice}
	for _, tc := range []struct {
		name string
		x    any
	}{
		{
			name: "map",
			x:    cyclicMap,
		},
		{
			name: "slice",
			x:    cyclicSlice,
		},
	} {
		if recoverHostPanic(func() { vm.ValueOf(tc.x) }) == nil {
			t.Errorf("%s: ValueOf did not panic", tc.name)
		}
	}

	// A shared subtree, and a prefix of a slice inside that slice, are not cycles.
	shared := []any{1, 2}
	prefix := []any{3, nil}
	prefix[1] = prefix[:1]
	v := vm.ValueOf(map[string]any{
		"a":      shared,
		"b":      shared,
		"prefix": prefix,
	})
	if err := vm.Global().Set("converted", v); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			src:  `converted.a.join() + ";" + converted.b.join() + ";" + (converted.a === converted.b)`,
			want: "1,2;1,2;false",
		},
		{
			src:  `JSON.stringify(converted.prefix)`,
			want: "[3,[3]]",
		},
	} {
		if got := run(t, &vm, tc.src); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.src, got, tc.want)
		}
	}
}

// TestRealmModules checks that modules belong to their realms, and that the module loader is
// told the realm of each import.
func TestRealmModules(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	got := map[string]*ekuma.Realm{}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		got[req.ReferrerName+":"+req.Specifier] = req.Realm
		if req.Specifier == "data" {
			return req.Realm.ParseJSONModule("data", `{}`)
		}
		return req.Realm.ParseModule(req.Specifier, `export default globalThis;`)
	})
	main, err := r.ParseModule("main", `
		import g from "lib";
		import data from "data" with { type: "json" };
		export const ok = g === globalThis && Object.getPrototypeOf(data) === Object.prototype;
		export const dynamic = import("dyn");`)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	if _, err := main.Evaluate(t.Context()); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	ns, err := main.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	if ok, err := ns.Get("ok"); err != nil || !ok.Bool() {
		t.Errorf("the imports of a module of a realm do not belong to the realm: %v, %v", ok, err)
	}
	if _, err := runScript(t.Context(), &vm, "script", `import("fromDefault")`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	for k, want := range map[string]*ekuma.Realm{
		"main:lib":           r,
		"main:data":          r,
		"main:dyn":           r,
		"script:fromDefault": vm.InitialRealm(),
	} {
		if got[k] != want {
			t.Errorf("the realm of the request %s is not the expected one", k)
		}
	}
}

// TestRealmRestored checks that the realm of a VM is current again after a function of another
// realm throws or is interrupted.
func TestRealmRestored(t *testing.T) {
	var vm ekuma.VM
	if err := vm.Global().Set("other", vm.NewRealm().Global()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := runScript(t.Context(), &vm, "setup", `var mark = 1;
		var throws = other.eval("(function() { throw new Error('x'); })");
		var loops = other.eval("(function() { for (;;) {} })");`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	check := func(what string) {
		t.Helper()
		v, err := runScript(t.Context(), &vm, "check", `typeof mark === "number" && Object.getPrototypeOf([]) === Array.prototype`)
		if err != nil {
			t.Fatalf("runScript after %s: %v", what, err)
		}
		if !v.Bool() {
			t.Errorf("after %s, the realm of the VM is not current", what)
		}
	}
	if _, err := runScript(t.Context(), &vm, "throw", `throws()`); err == nil {
		t.Errorf("got no error, want the error of the other realm")
	}
	check("an exception")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := runScript(ctx, &vm, "loop", `loops()`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	check("an interruption")
}

var inlineStringSink engine.Value
var inlineTextSink wtf8.String

func BenchmarkInlineString(b *testing.B) {
	for _, text := range []wtf8.String{"abcdefgh", "abcdefghi"} {
		b.Run(fmt.Sprintf("construct/%d", len(text)), func(b *testing.B) {
			vm := engine.NewVM(nil)
			b.ReportAllocs()
			for b.Loop() {
				inlineStringSink = vm.NewString(text)
			}
		})
		b.Run(fmt.Sprintf("roundtrip/%d", len(text)), func(b *testing.B) {
			vm := engine.NewVM(nil)
			b.ReportAllocs()
			for b.Loop() {
				inlineTextSink = vm.ToString(vm.NewString(text))
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{
			name: "retain",
			body: `let a = new Array(1000); for (let i=0;i<1000;i++) a[i] = "id" + i; return a[999] === "id999";`,
		},
		{
			name: "keys",
			body: `let o = {}; for (let i=0;i<1000;i++) o["id" + i] = i; let s=0; for (let i=0;i<1000;i++) s+=o["id" + i]; return s===499500;`,
		},
		{
			name: "slice",
			body: `let s="abcdefghijklmno", n=0; for (let i=0;i<1000;i++) n+=s.slice(i%8,i%8+4).length; return n===4000;`,
		},
		{
			name: "map",
			body: `let m=new Map(); for(let i=0;i<1000;i++) m.set("id"+i,i); let n=0; for(let i=0;i<1000;i++) n+=m.get("id"+i); return n===499500;`,
		},
		{
			name: "compare",
			body: `let n=0; for(let i=0;i<1000;i++) {let a="id"+i,b="id"+i; if(a===b) n++;} return n===1000;`,
		},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var vm ekuma.VM
			if _, err := runScript(b.Context(), &vm, "setup", "function runInline(){"+tc.body+"}"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				v, err := vm.Global().Call(b.Context(), "runInline")
				if err != nil {
					b.Fatal(err)
				}
				if !v.Bool() {
					b.Error("incorrect result")
				}
			}
		})
	}
}

func TestInlineStringBoundaries(t *testing.T) {
	vm := engine.NewVM(nil)
	otherVM := engine.NewVM(nil)
	for _, s := range []wtf8.String{"", "a", "ab", "abcdefgh", "abcdefghi", "ab\x00", "\x00\x00\x00\x00\x00\x00\xc2\x80", "日本", "\xed\xa0\x80"} {
		t.Run(fmt.Sprintf("%x", s), func(t *testing.T) {
			v := vm.NewString(s)
			if got := vm.ToString(v); got != s {
				t.Errorf("ToString = %q, want %q", got, s)
			}
			if !engine.IsStrictlyEqual(v, otherVM.NewString(s)) {
				t.Error("equal strings from different VMs differ")
			}
			if len(s) > 0 {
				different := []byte(s)
				different[0] ^= 1
				if engine.IsStrictlyEqual(v, vm.NewString(wtf8.String(different))) {
					t.Error("different payloads compare equal")
				}
			}
			boxed := engine.ObjectValue(v.Object())
			if !engine.IsStrictlyEqual(v, boxed) {
				t.Error("materialized value differs")
			}
			if engine.IsStrictlyEqual(v, vm.NewString(s+"\x00")) {
				t.Error("different lengths compare equal")
			}
			if got := engine.ToBoolean(v); got != (s != "") {
				t.Errorf("ToBoolean = %v", got)
			}
		})
	}
}

func TestInlineStringIndexAllocations(t *testing.T) {
	vm := engine.NewVM(nil)
	s := vm.NewString("abc")
	if got := testing.AllocsPerRun(100, func() { vm.Index(s, 1) }); got != 0 {
		t.Errorf("packed string index allocations = %v, want 0", got)
	}
}

func TestInlineStringCodeUnits(t *testing.T) {
	vm := engine.NewVM(nil)
	for _, tc := range []struct {
		text  wtf8.String
		units []uint16
	}{
		{
			text:  "ab\x00defgh",
			units: []uint16{'a', 'b', 0, 'd', 'e', 'f', 'g', 'h'},
		},
		{
			text:  "ab😀cd",
			units: []uint16{'a', 'b', 0xd83d, 0xde00, 'c', 'd'},
		},
	} {
		t.Run(fmt.Sprintf("%x", tc.text), func(t *testing.T) {
			v := vm.NewString(tc.text)
			if got := v.StringLength(); got != len(tc.units) {
				t.Errorf("StringLength = %d, want %d", got, len(tc.units))
			}
			for i, want := range tc.units {
				if got := v.UnitAt(i); got != want {
					t.Errorf("UnitAt(%d) = %#x, want %#x", i, got, want)
				}
			}
		})
	}
}

func TestBigIntSizeLimit(t *testing.T) {
	// Every result is at most 2^30 bits.
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "powers of two",
			src:  `[2n ** 40000000n === 1n << 40000000n, 4n ** 20000000n === 1n << 40000000n, (-2n) ** 33554433n === -(1n << 33554433n)].join()`,
			want: "true,true,true",
		},
		{
			name: "power of more than 2^25 bits",
			src:  `(3n ** 22000000n).toString(16).length`,
			want: "8717294",
		},
		{
			name: "powers beyond the limit",
			src:  `var r = []; for (var f of [() => 2n ** (2n ** 30n), () => 3n ** 700000000n, () => (2n ** 64n + 1n) ** 20000000n, () => 2n ** (2n ** 64n)]) { try { f(); r.push("no error"); } catch (e) { r.push(e instanceof RangeError); } } r.join()`,
			want: "true,true,true,true",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			if got := run(t, &vm, tc.src); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}
