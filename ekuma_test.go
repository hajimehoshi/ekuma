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
	"testing/synctest"
	"time"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestForeignValueEntry(t *testing.T) {
	for _, name := range []string{
		"ValueOf", "RealmValueOf", "Nested", "Set", "SetIndex", "Invoke", "Call", "New",
		"Result", "Exception", "SyntheticModule", "DefaultExportSyntheticModule", "InstanceOf", "LoaderException", "ImportMeta",
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
				_ = b.Global().Set(t.Context(), "foreign", foreign)
			case "SetIndex":
				_ = b.ValueOf([]any{}).SetIndex(t.Context(), 0, foreign)
			case "Invoke":
				_, _ = f.Invoke(t.Context(), 1, foreign)
			case "Call":
				_, _ = b.ValueOf(map[string]any{"f": f}).Call(t.Context(), "f", foreign)
			case "New":
				ctor, err := b.Global().Get(t.Context(), "Object")
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
				b.NewSyntheticModule("native", map[string]ekuma.Value{
					"foreign": foreign,
				})
			case "DefaultExportSyntheticModule":
				b.NewRealm().NewDefaultExportSyntheticModule("foreign", foreign)
			case "InstanceOf":
				ctor, err := b.Global().Get(t.Context(), "Object")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = foreign.InstanceOf(t.Context(), ctor)
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
				_, _ = b.Global().GetWithValueKey(t.Context(), foreign)
			case "SetWithValueKey":
				_ = b.Global().SetWithValueKey(t.Context(), foreign, 1)
			case "DeleteWithValueKey":
				_ = b.Global().DeleteWithValueKey(t.Context(), foreign)
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
		v, err := values.Index(t.Context(), i)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Global().Set(t.Context(), "value", v); err != nil {
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
	for _, typ := range []reflect.Type{reflect.TypeFor[ekuma.Value](), reflect.TypeFor[storage.Value]()} {
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
	if err := vm.Global().Set(ctx, "f", f); err != nil {
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
	obj, err := global.Get(ctx, "obj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := obj.Set(ctx, "n", 5); err != nil {
		t.Errorf("Set: %v", err)
	}
	if v, err := obj.Get(ctx, "twice"); err != nil || v.Int() != 10 {
		t.Errorf("twice = %v, %v, want 10", v, err)
	}
	if v, err := obj.Call(ctx, "add", 1, 2); err != nil || v.Int() != 8 {
		t.Errorf("add = %v, %v, want 8", v, err)
	}

	sum, err := global.Get(ctx, "sum")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, err := sum.Invoke(ctx, 1, 2.5, int8(3)); err != nil || v.Float() != 6.5 {
		t.Errorf("sum = %v, %v, want 6.5", v, err)
	}

	point, err := global.Get(ctx, "Point")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	p, err := point.New(ctx, 3, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if v, err := p.Get(ctx, "y"); err != nil || v.Int() != 4 {
		t.Errorf("y = %v, %v, want 4", v, err)
	}
	if ok, err := p.InstanceOf(ctx, point); err != nil || !ok {
		t.Errorf("InstanceOf = %v, %v, want true", ok, err)
	}
	if err := p.Delete(ctx, "y"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if v, err := p.Get(ctx, "y"); err != nil || !v.IsUndefined() {
		t.Errorf("y after Delete = %v, %v, want undefined", v, err)
	}

	// Go values convert to JavaScript values, including nested ones.
	arr := vm.ValueOf([]any{"a", nil, map[string]any{"k": true}})
	if n, err := arr.Get(ctx, "length"); err != nil || n.Int() != 3 {
		t.Errorf("length = %v, %v, want 3", n, err)
	}
	if err := arr.SetIndex(ctx, 3, "z"); err != nil {
		t.Errorf("SetIndex: %v", err)
	}
	json, err := runScript(ctx, &vm, "describe", `(function(a) { return a.map(x => x === null ? "null" : typeof x === "object" ? "k=" + x.k : x).join(); })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if v, err := json.Invoke(ctx, arr); err != nil || v.String() != "a,null,k=true,z" {
		t.Errorf("describe = %v, %v, want a,null,k=true,z", v, err)
	}
	if v, err := arr.Index(ctx, 0); err != nil || !v.Equal(vm.ValueOf("a")) {
		t.Errorf("Index(0) = %v, %v, want a", v, err)
	}

	// A string has properties too, and its length counts UTF-16 code units.
	if n, err := vm.ValueOf("h\u00e9\U0001F600").Get(ctx, "length"); err != nil || n.Int() != 4 {
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
	if err := ekuma.Undefined.Set(ctx, "x", 1); !errors.As(err, &ve) {
		t.Errorf("setting a property of undefined: got %v, want *ekuma.ValueError", err)
	}
	if _, err := vm.ValueOf(1).Get(ctx, "x"); !errors.As(err, &ve) {
		t.Errorf("getting a property of a number: got %v, want *ekuma.ValueError", err)
	}
	// A lexical declaration is not a property of the global object.
	if v, err := global.Get(ctx, "lexical"); err != nil || !v.IsUndefined() {
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
	if got, err := obj.Index(t.Context(), index); err != nil || got.String() != "original" {
		t.Errorf("Index(%d) = %v, %v, want original", index, got, err)
	}
	if err := obj.SetIndex(t.Context(), index, "updated"); err != nil {
		t.Errorf("SetIndex(%d): %v", index, err)
	}
	if got, err := obj.Get(t.Context(), key); err != nil || got.String() != "updated" {
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
	sym, err := keys.Get(ctx, "sym")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	o := vm.ValueOf(map[string]any{"n": 1})
	if err := o.SetWithValueKey(ctx, sym, 2); err != nil {
		t.Errorf("SetWithValueKey: %v", err)
	}
	if v, err := o.GetWithValueKey(ctx, sym); err != nil || v.Int() != 2 {
		t.Errorf("o[sym] = %v, %v, want 2", v, err)
	}
	if err := o.DeleteWithValueKey(ctx, sym); err != nil {
		t.Errorf("DeleteWithValueKey: %v", err)
	}
	if v, err := o.GetWithValueKey(ctx, sym); err != nil || !v.IsUndefined() {
		t.Errorf("o[sym] after DeleteWithValueKey = %v, %v, want undefined", v, err)
	}

	// An object key is converted the way o[key] converts it.
	obj, err := keys.Get(ctx, "obj")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v, err := o.GetWithValueKey(ctx, obj); err != nil || v.Int() != 1 {
		t.Errorf("o[obj] = %v, %v, want 1", v, err)
	}

	// A store that fails converts its key once.
	counted, err := runScript(ctx, &vm, "counted", `var n = 0; ({ frozen: Object.freeze({}), key: { toString() { n++; return "x"; } }, count() { return n; } })`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	frozen, err := counted.Get(ctx, "frozen")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	key, err := counted.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var serr *ekuma.ScriptError
	if err := frozen.SetWithValueKey(ctx, key, 1); !errors.As(err, &serr) {
		t.Errorf("SetWithValueKey on a frozen object: got %v, want *ekuma.ScriptError", err)
	}
	if v, err := counted.Call(ctx, "count"); err != nil || v.Int() != 1 {
		t.Errorf("key conversions = %v, %v, want 1", v, err)
	}

	iter, err := keys.Get(ctx, "iter")
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
	if v, err := next.Get(ctx, "value"); err != nil || v.String() != "a" {
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
	if err := vm.Global().Set(t.Context(), "tooLarge", vm.FuncOf("tooLarge", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	if err := vm.Global().Set(t.Context(), "buf", vm.NewArrayBuffer(b)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// poke writes b while JavaScript runs, as WebAssembly code does.
	poke := vm.FuncOf("poke", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		b[args[0].Int()] = byte(args[1].Int())
		return nil, nil
	})
	if err := vm.Global().Set(t.Context(), "poke", poke); err != nil {
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
	if err := vm.Global().Set(t.Context(), "hostBuffer", vm.NewArrayBuffer(nil)); err != nil {
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
	ctor, err := r.Global().Get(t.Context(), "ArrayBuffer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok, err := buf.InstanceOf(t.Context(), ctor); err != nil || !ok {
		t.Errorf("InstanceOf: got %v, %v, want true", ok, err)
	}
	if got, err := buf.Get(t.Context(), "detached"); err != nil || got.Bool() {
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
	if err := vm.Global().Set(t.Context(), "buf", buf); err != nil {
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
	if err := vm.Global().Set(t.Context(), "grow", grow); err != nil {
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
	if got, err := im.Get(t.Context(), "detached"); err != nil || got.Bool() {
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
	if got, err := buf.Get(t.Context(), "detached"); err != nil || got.Bool() {
		t.Errorf("detached after using the copy: got %v, %v, want false", got, err)
	}
}

// TestNewArrayBufferTransfer tests that JavaScript can neither transfer nor resize an ArrayBuffer
// over a Go slice, and that its views keep the slice.
func TestNewArrayBufferTransfer(t *testing.T) {
	var vm ekuma.VM
	b := []byte{1, 2}
	if err := vm.Global().Set(t.Context(), "buf", vm.NewArrayBuffer(b)); err != nil {
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
			c, err := vm.Global().Get(ctx, "TypeError")
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
		if err := vm.Global().Set(ctx, name, vm.FuncOf(name, f)); err != nil {
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
	if err := vm.Global().Set(ctx, "fail", fail); err != nil {
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
				_, err := v.Get(ctx, "x")
				return err
			},
		},
		{
			name: "setter",
			src:  `({ set x(v) { queue(); } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				return v.Set(ctx, "x", 1)
			},
		},
		{
			name: "delete trap",
			src:  `new Proxy({}, { deleteProperty() { queue(); return true; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				return v.Delete(ctx, "x")
			},
		},
		{
			name: "instanceof",
			src:  `({ [Symbol.hasInstance]() { queue(); return true; } })`,
			call: func(ctx context.Context, v ekuma.Value) error {
				_, err := ekuma.Undefined.InstanceOf(ctx, v)
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
		_, err := args[0].Get(t.Context(), "x")
		return nil, err
	})
	if err := vm.Global().Set(t.Context(), "read", read); err != nil {
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
	v, err = vm.Global().Get(t.Context(), "ran")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Truthy() {
		t.Error("jobs did not run at the outermost return")
	}
}

func TestGetCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "getter",
			body: `stop(); for (let i = 0; i < 2; i++) {}`,
		},
		{
			name: "promise job",
			body: `Promise.resolve().then(() => { stop(); for (let i = 0; i < 2; i++) {} });`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var vm ekuma.VM
				ctx, cancel := context.WithCancelCause(t.Context())
				defer cancel(nil)
				cause := errors.New("stop property read")
				stop := vm.FuncOf("stop", func(ekuma.Value, []ekuma.Value) (any, error) {
					cancel(cause)
					synctest.Wait()
					return nil, nil
				})
				if err := vm.Global().Set(t.Context(), "stop", stop); err != nil {
					t.Fatal(err)
				}
				obj, err := runScript(t.Context(), &vm, "setup", `({ get result() { `+tc.body+` return 1; }, stable: 42 })`)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := obj.Get(ctx, "result"); !errors.Is(err, cause) {
					t.Errorf("Get(result): got %v, want %v", err, cause)
				}
				if got, err := obj.Get(t.Context(), "stable"); err != nil || !got.Equal(vm.ValueOf(42)) {
					t.Errorf("Get(stable) after cancellation: got %v, %v, want 42, nil", got, err)
				}
			})
		})
	}
}

func TestThrowingCallJobInterruption(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(this ekuma.Value, args []ekuma.Value) (any, error) {
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

// TestJobOrder checks when the jobs run relative to the script and to calls from Go.
func TestJobOrder(t *testing.T) {
	var vm ekuma.VM
	call := vm.FuncOf("call", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The jobs do not run while the script that called this is still running.
		return args[0].Invoke(context.Background())
	})
	if err := vm.Global().Set(t.Context(), "call", call); err != nil {
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
	ran, err := vm.Global().Get(t.Context(), "ran")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ran.IsUndefined() {
		t.Errorf("a script ran under a context that was already done")
	}
}

// TestCancelBeforeCalls checks that no call starts after a host callback cancels the evaluation,
// whichever way the call is made.
func TestCancelBeforeCalls(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "host function",
			src:  `step(); step();`,
		},
		{
			name: "function",
			src:  `step(); f();`,
		},
		{
			name: "apply of the arguments",
			src:  `function g() { step(); return f.apply(this, arguments); } g();`,
		},
		{
			// Compiling the code would throw a SyntaxError.
			name: "direct eval",
			src:  `step(); eval("(");`,
		},
		{
			name: "tail call",
			src:  `function g() { "use strict"; step(); return f(); } g();`,
		},
		{
			name: "callback of a built-in",
			src:  `[1, 2].forEach(function (x) { if (x === 1) step(); else ran = true; });`,
		},
		{
			name: "host function called back by a built-in",
			src:  `[1, 2].forEach(step);`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var vm ekuma.VM
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var steps int
				step := vm.FuncOf("step", func(ekuma.Value, []ekuma.Value) (any, error) {
					steps++
					cancel()
					// Wait until the VM has received the cancellation.
					synctest.Wait()
					return nil, nil
				})
				if err := vm.Global().Set(t.Context(), "step", step); err != nil {
					t.Fatal(err)
				}
				// The fast paths take calls of functions that have run, when the VM has room for
				// more frames.
				src := `var ran = false; function f() { ran = true; }
					(function nest(n) { if (n > 0) nest(n - 1); else f(); })(8);
					ran = false;` + tc.src
				if _, err := runScript(ctx, &vm, "cancel", src); !errors.Is(err, context.Canceled) {
					t.Errorf("got %v, want context.Canceled", err)
				}
				if steps != 1 {
					t.Errorf("got %d calls of step, want 1", steps)
				}
				ran, err := vm.Global().Get(t.Context(), "ran")
				if err != nil {
					t.Fatal(err)
				}
				if ran.Bool() {
					t.Errorf("a function ran after the cancellation")
				}
			})
		})
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
		// The getter inherits the deadline of the outer call.
		v, err := args[0].Get(context.Background(), "forever")
		returned = true
		return v, err
	})
	if err := global.Set(t.Context(), "read", read); err != nil {
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
	if v, err := global.Get(t.Context(), "caught"); err != nil || v.Truthy() {
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
	if err := vm.Global().Set(t.Context(), "outer", outer); err != nil {
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

func TestCanceledGeneratorRequests(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("stop generator")
	cancel(cause)
	if err := vm.Global().Set(t.Context(), "stop", vm.FuncOf("stop", func(_ ekuma.Value, args []ekuma.Value) (any, error) {
		return args[0].Invoke(ctx)
	})); err != nil {
		t.Fatal(err)
	}
	obj, err := runScript(t.Context(), &vm, "setup", `
		var g = (function* () { stop(() => {}); })();
		var ag = (async function* () {})();
		({
			get sync() { return g.next(); },
			get async() { return ag.return({ get then() { stop(() => {}); } }); }
		})`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sync", "async"} {
		if _, err := obj.Get(t.Context(), name); err != cause {
			t.Errorf("Get(%q) returned %T %v, want the cancellation cause", name, err, err)
		}
	}
	done, err := runScript(t.Context(), &vm, "sync", "g.next().done")
	if err != nil || !done.Truthy() {
		t.Errorf("g.next().done = %v, %v, want true", done, err)
	}
	run(t, &vm, "var done; ag.next().then(r => done = r.done)")
	if got := run(t, &vm, "done"); got != "true" {
		t.Errorf("next async generator request: done = %s, want true", got)
	}
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
			if err := vm.Global().Set(t.Context(), "invoke", invoke); err != nil {
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
		if err := vm.Global().Set(t.Context(), "h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
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
	if err := global.Set(t.Context(), "h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return slow.Invoke(ctx)
	})); err != nil {
		t.Fatalf("Set: %v", err)
	}
	errTooLong := errors.New("the script was not stopped")
	var ticks int
	if err := global.Set(t.Context(), "tick", vm.FuncOf("tick", func(this ekuma.Value, args []ekuma.Value) (any, error) {
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
	if err := global.Set(t.Context(), "h", vm.FuncOf("h", func(this ekuma.Value, args []ekuma.Value) (any, error) {
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
			if err := vm.Global().Set(t.Context(), "invoke", invoke); err != nil {
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
	if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	if v, err := vm.Global().Get(t.Context(), "after"); err != nil || v.Truthy() {
		t.Errorf("after = %v, %v: the script continued after fill", v, err)
	}
}

// TestCancelSetMethod checks that a Set method that looks the elements up in the table of its Set
// argument, without calls, stops when its context is done.
func TestCancelSetMethod(t *testing.T) {
	var vm ekuma.VM
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
		cancel()
		// The cancellation is requested on another goroutine.
		time.Sleep(50 * time.Millisecond)
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	// The set is small enough for the method to end quickly when it does not stop.
	_, err := runScript(ctx, &vm, "set", `
		var s = new Set();
		for (let i = 0; i < 2 * 65536; i++) s.add(i);
		Object.defineProperty(s, "size", { get() { cancel(); return 2 * 65536; } });
		var after = false;
		s.isSubsetOf(s);
		after = true;`)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
	if v, err := vm.Global().Get(t.Context(), "after"); err != nil || v.Truthy() {
		t.Errorf("after = %v, %v: the script continued after isSubsetOf", v, err)
	}
}

// TestCancelSearches checks that a search or a sort stops when its context is done, also when it
// looks at few elements or finds few matches but reads many bytes.
func TestCancelSearches(t *testing.T) {
	// The inputs are small enough for the searches to end quickly when they do not stop.
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "long strings",
			src: `
				var x = "x".repeat(1 << 12), a = new Array(1 << 12).fill(x + "a");
				a.indexOf(x + "b", { valueOf() { cancel(); return 0; } });`,
		},
		{
			name: "long strings backward",
			src: `
				var x = "x".repeat(1 << 12), a = new Array(1 << 12).fill(x + "a");
				a.lastIndexOf(x + "b", { valueOf() { cancel(); return a.length - 1; } });`,
		},
		{
			name: "long strings between holes",
			src: `
				var x = "x".repeat(1 << 12), s = x + "a", a = [];
				for (let i = 0; i < 1 << 13; i += 2) a[i] = s;
				a.indexOf(x + "b", { valueOf() { cancel(); return 0; } });`,
		},
		{
			name: "long strings between holes backward",
			src: `
				var x = "x".repeat(1 << 12), s = x + "a", a = [];
				for (let i = 0; i < 1 << 13; i += 2) a[i] = s;
				a.lastIndexOf(x + "b", { valueOf() { cancel(); return a.length - 1; } });`,
		},
		{
			name: "large BigInts",
			src: `
				var x = 1n << 32768n, a = new Array(1 << 12).fill(x + 1n);
				a.indexOf(x + 2n, { valueOf() { cancel(); return 0; } });`,
		},
		{
			name: "split",
			src: `
				var s = ("x".repeat(1 << 12) + ",").repeat(64);
				s.split(",", { valueOf() { cancel(); return 100; } });`,
		},
		{
			name: "replaceAll",
			src: `
				var s = ("x".repeat(1 << 12) + ",").repeat(64);
				s.replaceAll(",", { toString() { cancel(); return ";"; } });`,
		},
		{
			name: "sort of long strings",
			src: `
				var x = "x".repeat(1 << 12), a = [x + "b", x + "a"];
				var o = { length: 1 << 10 };
				for (let i = 0; i < o.length; i++) o[i] = a[i % 2];
				// The last element is read right before the sort.
				Object.defineProperty(o, o.length - 1, { get() { cancel(); return x; } });
				Array.prototype.toSorted.call(o);`,
		},
		{
			name: "sort of long strings and numbers",
			src: `
				var x = "x".repeat(1 << 12), a = [x + "b", x + "a"];
				var o = { length: 1 << 10 };
				for (let i = 0; i < o.length; i++) o[i] = a[i % 2];
				// The last element is read right before the sort.
				Object.defineProperty(o, o.length - 1, { get() { cancel(); return 1; } });
				Array.prototype.toSorted.call(o);`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var canceled bool
			if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
				canceled = true
				cancel()
				// The cancellation is requested on another goroutine.
				time.Sleep(50 * time.Millisecond)
				return nil, nil
			})); err != nil {
				t.Fatal(err)
			}
			_, err := runScript(ctx, &vm, "search", tc.src+"\nvar after = true;")
			if !canceled {
				t.Fatal("the script did not cancel the context")
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("got %v, want context.Canceled", err)
			}
			if v, err := vm.Global().Get(t.Context(), "after"); err != nil || v.Truthy() {
				t.Errorf("after = %v, %v: the script continued after the search", v, err)
			}
		})
	}
}

// TestCancelScans checks that a loop of a built-in or of a for-in statement that looks for elements or
// properties stops at its next step when its context is done, however few steps it takes.
func TestCancelScans(t *testing.T) {
	// No step after the cancellation calls a function, which would stop the script.
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "for-in",
			src: `
				var o = Object.create({ a: 1 });
				for (var k in (cancel(), o)) var after = true;`,
		},
		{
			name: "holes",
			src:  `[1, , 2].indexOf(3, { valueOf() { cancel(); return 0; } });`,
		},
		{
			name: "holes backward",
			src:  `[1, , 2].lastIndexOf(3, { valueOf() { cancel(); return 2; } });`,
		},
		{
			name: "elements of nested arrays",
			src:  `[[1, 2], [3, 4]].flat({ valueOf() { cancel(); return 1; } });`,
		},
		{
			name: "push",
			src:  `Array.prototype.push.call({ get length() { cancel(); return 0; } }, 1, 2);`,
		},
		{
			name: "concat",
			src:  `[].concat({ get [Symbol.isConcatSpreadable]() { cancel(); } }, {});`,
		},
		{
			name: "Object.assign",
			src:  `Object.assign({}, { get a() { cancel(); }, b: 1 });`,
		},
		{
			name: "Object.assign of data properties",
			src: `
				var empty = new Proxy({}, { ownKeys() { cancel(); return []; } });
				Object.assign({}, empty, { a: 1 });`,
		},
		{
			name: "Object.defineProperties",
			src:  `Object.defineProperties({}, { get a() { cancel(); return {}; }, b: {} });`,
		},
		{
			name: "ownKeys trap",
			src: `
				var keys = { get 0() { cancel(); return "a"; }, 1: "b", length: 2 };
				Reflect.ownKeys(new Proxy({}, { ownKeys() { return keys; } }));`,
		},
		{
			name: "Iterator.concat",
			src:  `Iterator.concat({ get [Symbol.iterator]() { cancel(); return () => {}; } }, []);`,
		},
		{
			name: "Iterator.zipKeyed",
			src:  `Iterator.zipKeyed({ get a() { cancel(); }, b: undefined });`,
		},
		{
			name: "padding of Iterator.zipKeyed",
			src: `
				var it = { next() {} };
				Iterator.zipKeyed({ a: it, b: it }, { mode: "longest", padding: { get a() { cancel(); } } });`,
		},
		{
			name: "replacer array",
			src: `
				var keys = [, , ,];
				Object.defineProperty(keys, 0, { get() { cancel(); } });
				JSON.stringify(1, keys);`,
		},
		{
			name: "objects to serialize",
			src:  `JSON.stringify([{ toJSON() { cancel(); } }, []]);`,
		},
		{
			name: "properties to serialize",
			src:  `JSON.stringify({ get a() { cancel(); }, b: 1 });`,
		},
		{
			name: "holes to serialize",
			src: `
				var a = [, , ,];
				Object.defineProperty(a, 0, { get() { cancel(); } });
				JSON.stringify(a);`,
		},
		{
			name: "replace",
			src: `
				// The matcher checks for an interruption when it first runs.
				/x/.test("x");
				var re = /x/g, exec = re.exec;
				Object.defineProperty(re, "exec", {
					get() { Object.defineProperty(re, "exec", { value: exec }); cancel(); return exec; },
					configurable: true,
				});
				re[Symbol.replace]("xx", "y");`,
		},
		{
			name: "split",
			src: `
				/x/.test("x");
				class R extends RegExp {
					get exec() { Object.defineProperty(this, "exec", { value: undefined }); cancel(); }
				}
				new R("x")[Symbol.split]("yy");`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var vm ekuma.VM
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
					if ctx.Err() != nil {
						t.Errorf("the scan continued after the cancellation")
					}
					cancel()
					// Wait until the VM has received the cancellation.
					synctest.Wait()
					return nil, nil
				})); err != nil {
					t.Fatal(err)
				}
				_, err := runScript(ctx, &vm, "scan", tc.src+"\nvar after = true;")
				if !errors.Is(err, context.Canceled) {
					t.Errorf("got %v, want context.Canceled", err)
				}
				if v, err := vm.Global().Get(t.Context(), "after"); err != nil || v.Truthy() {
					t.Errorf("after = %v, %v: the script continued after the scan", v, err)
				}
			})
		})
	}
}

func TestInternalError(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set(t.Context(), "fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
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
	if err := vm.Global().Set(t.Context(), "host", vm.FuncOf("host", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	if err := vm.Global().Set(t.Context(), "host", vm.FuncOf("host", func(ekuma.Value, []ekuma.Value) (any, error) {
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

func TestInternalErrorJob(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set(t.Context(), "fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
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
	if err := global.Set(t.Context(), "bad", vm.FuncOf("bad", func(this ekuma.Value, args []ekuma.Value) (any, error) {
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
	if err := vm.Global().Set(t.Context(), "bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	if err := vm.Global().Set(t.Context(), "recoverInner", vm.FuncOf("recoverInner", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	if err := vm.Global().Set(t.Context(), "bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
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

// TestHostPanicJobRecovery checks that recovered panics stop the current job drain.
func TestHostPanicJobRecovery(t *testing.T) {
	for _, reports := range []bool{false, true} {
		t.Run(fmt.Sprintf("reports=%t", reports), func(t *testing.T) {
			var vm ekuma.VM
			marker := new(int)
			if err := vm.Global().Set(t.Context(), "bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
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
				if err := vm.Global().Set(t.Context(), "recoverInner", vm.FuncOf("recoverInner", func(ekuma.Value, []ekuma.Value) (any, error) {
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
			if err := vm.Global().Set(t.Context(), "bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
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
	// The scripts cancel the context once their jobs are running.
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "then",
			src:  `var count = 0; function f() { if (++count === 10) cancel(); Promise.resolve().then(f); } f();`,
		},
		{
			name: "await",
			src:  `var count = 0; (async () => { for (;;) { if (++count === 10) cancel(); await null; } })();`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if err := vm.Global().Set(t.Context(), "cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
				cancel()
				return nil, nil
			})); err != nil {
				t.Fatal(err)
			}
			if _, err := runScript(ctx, &vm, "endless", tt.src); !errors.Is(err, context.Canceled) {
				t.Errorf("got %v, want context.Canceled", err)
			}
			if _, err := runScript(t.Context(), &vm, "after", `var before = count; var r = 0; Promise.resolve(5).then(v => { r = v; });`); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			v, err := runScript(t.Context(), &vm, "check", `[count >= 10, count === before, r].join()`)
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

// TestRepeatedCopyInterrupt checks that operations that read one object after another check for an
// interruption between the objects.
func TestRepeatedCopyInterrupt(t *testing.T) {
	// No source runs anything that checks for an interruption after the call of stop, not even at
	// its end.
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "Object.assign",
			src:  `Object.assign({}, { get a() { stop(); } }, { b: 1 })`,
		},
		{
			name: "concat",
			src:  `[].concat({ [Symbol.isConcatSpreadable]: true, get length() { stop(); return 0; } }, [,])`,
		},
		{
			name: "flat",
			src: `var holes = [0];
				while (holes.length < 70000) holes[Math.floor(holes.length * 1.4) + 1] = 0;
				for (var i in holes) delete holes[i];
				var nested = [0, holes];
				Object.defineProperty(nested, 0, { get() { stop(); } });
				nested.flat()`,
		},
		{
			name: "object spread",
			src:  `({ ...{ get a() { stop(); } }, ...{ b: 1 } })`,
		},
		{
			name: "object rest",
			src: `var source = { get a() { stop(); } };
				for (var i = 0; i < 300; i++) source["k" + i] = 0;
				var rest = new Function("source", "var { " + Array.from({ length: 300 }, (_, i) => "k" + i + ": v").join() + ", ...r } = source; done = true");
				rest(source)`,
		},
		{
			name: "JSON.stringify",
			src:  `JSON.stringify([{ get a() { stop(); } }, {}])`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			cause := errors.New("interrupted")
			withdraw := func() {}
			if err := vm.Global().Set(t.Context(), "stop", vm.FuncOf("stop", func(this ekuma.Value, args []ekuma.Value) (any, error) {
				withdraw = ekuma.RequestInterrupt(vm.InitialRealm(), cause)
				return nil, nil
			})); err != nil {
				t.Fatal(err)
			}
			if _, err := runScript(t.Context(), &vm, tt.name, "var done = false; "+tt.src+"; done = true"); !errors.Is(err, cause) {
				t.Errorf("got %v, want %v", err, cause)
			}
			withdraw()
			done, err := vm.Global().Get(t.Context(), "done")
			if err != nil {
				t.Fatal(err)
			}
			if done.Bool() {
				t.Error("the operation was not interrupted")
			}
		})
	}
}

// TestMoveWithHoles checks methods that move the elements of an array with runs of holes of
// several lengths, on an array of a subclass, against the same methods on an ordinary object, for
// which every index is visited.
func TestMoveWithHoles(t *testing.T) {
	const src = `class Sub extends Array {}
		// Alternating runs of elements and holes of the given lengths.
		function fill(o, runs) {
			var n = 0;
			runs.forEach((len, j) => {
				for (var i = 0; i < len; i++, n++) {
					if (j % 2 === 0) o[n] = n;
				}
			});
			o.length = n;
			return o;
		}
		var calls = {
			shift: o => Array.prototype.shift.call(o),
			unshift: o => Array.prototype.unshift.call(o, ...new Array(150).fill("u")),
			remove: o => Array.prototype.splice.call(o, 3, 150),
			insert: o => Array.prototype.splice.call(o, 3, 10, ...new Array(140).fill("s")),
			copyForward: o => Array.prototype.copyWithin.call(o, 100, 3),
			copyBackward: o => Array.prototype.copyWithin.call(o, 3, 100),
		};
		var show = o => o.length + ":" + Object.keys(o).map(k => k + "=" + o[k]).join();
		var bad = [];
		for (var runs of [[5, 255, 1], [5, 256, 1], [5, 257, 1], [1, 127, 2, 130, 1], [1, 200, 1, 64, 1, 64, 100], [200, 500, 3], [3, 300, 200, 300], [1, 1, 1, 900, 1]]) {
			for (var name in calls) {
				var a = fill(new Sub(), runs), o = fill({}, runs);
				calls[name](a);
				calls[name](o);
				var got = show(a), want = o.length + ":" + Object.keys(o).filter(k => k !== "length").map(k => k + "=" + o[k]).join();
				if (got !== want) bad.push(name + " " + runs);
			}
		}
		bad.join("; ")`
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "holes", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.String(); got != "" {
		t.Errorf("different results: %s", got)
	}
}

// TestForInVisitedIndices checks which keys of prototypes a for-in loop over an array visits.
func TestForInVisitedIndices(t *testing.T) {
	const src = `function keys(o, visit) {
			var ks = [];
			for (var k in o) {
				ks.push(k);
				visit?.(k);
			}
			return ks.join();
		}
		function array(proto) {
			return Object.setPrototypeOf(Object.assign([10, 11, 12, 13], { a: 1, b: 2 }), proto);
		}
		var proto = () => Object.setPrototypeOf({ 0: 0, 2: 0, 5: 0, a: 0, b: 0, c: 0 }, { 2: 0, 3: 0, 5: 0, 7: 0, b: 0, d: 0 });
		var hidden = array(proto());
		Object.defineProperty(hidden, "c", { value: 1, enumerable: false });
		var removed = array(proto()), cut = array(proto());
		[
			keys(hidden),
			// Keys that are gone when they come up are visited on the first prototype that has them.
			keys(removed, k => { if (k === "0") { delete removed[2]; delete removed[3]; delete removed.b; } }),
			keys(cut, k => { if (k === "1") cut.length = 2; }),
		].join(" ")`
	var vm ekuma.VM
	v, err := runScript(t.Context(), &vm, "keys", src)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "0,1,2,3,a,b,5,7,d 0,1,a,2,5,b,c,3,7,d 0,1,a,b,2,5,c,3,7,d"; got != want {
		t.Errorf("got %s, want %s", got, want)
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
		`y ** 4n`,
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
			if err := vm.Global().Set(t.Context(), "inspect", inspect); err != nil {
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
	c, err := vm.Global().Get(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := v.InstanceOf(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	return ok
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
		got, err := v.Index(t.Context(), i)
		if err != nil {
			t.Fatalf("Index: %v", err)
		}
		if !got.Equal(other) {
			t.Errorf("%s is not the global object of the realm of the script", name)
		}
	}
	if got, err := v.Index(t.Context(), 2); err != nil || !got.Bool() {
		t.Errorf("an array literal does not get the Array.prototype of the realm of the script: %v, %v", got, err)
	}
	if got, err := other.Get(t.Context(), "inOther"); err != nil || got.Int() != 1 {
		t.Errorf("inOther in the realm of the script: got %v, %v, want 1", got, err)
	}
	if got, err := vm.Global().Get(t.Context(), "inOther"); err != nil || !got.IsUndefined() {
		t.Errorf("inOther in the realm of the VM: got %v, %v, want undefined", got, err)
	}
}

// TestRealmValues checks which realm the values that Go creates belong to.
func TestRealmValues(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	g := vm.Global()
	if err := g.Set(t.Context(), "other", r.Global()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	inner := r.FuncOf("inner", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The methods of the VM work in the initial realm even in a function of another realm.
		if err := g.Set(t.Context(), "fromVM", vm.ValueOf([]any{})); err != nil {
			return nil, err
		}
		if err := g.Set(t.Context(), "fromSet", []any{}); err != nil {
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
		if err := g.Set(t.Context(), name, v); err != nil {
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
			if err := vm.Global().Set(t.Context(), "target", r.Global()); err != nil {
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
			if err := vm.Global().Set(t.Context(), "converted", v); err != nil {
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
	f, err := v.Get(t.Context(), "0")
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
	if err := vm.Global().Set(t.Context(), "converted", v); err != nil {
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

// TestRealmRestored checks that the realm of a VM is current again after a function of another
// realm throws or is interrupted.
func TestRealmRestored(t *testing.T) {
	var vm ekuma.VM
	if err := vm.Global().Set(t.Context(), "other", vm.NewRealm().Global()); err != nil {
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

// TestRealmCatch checks that a function of another realm continues in its own realm after catching
// an exception that passed through a native function.
func TestRealmCatch(t *testing.T) {
	var vm ekuma.VM
	if err := vm.Global().Set(t.Context(), "other", vm.NewRealm().Global()); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := runScript(t.Context(), &vm, "catch", `
		var throws = function() { [1].forEach(function() { throw 1; }); };
		var catches = other.Function("f", "try { f(); } catch (e) {} try { null.x; } catch (e) { return e; }");
		catches(throws) instanceof other.TypeError`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if !v.Bool() {
		t.Errorf("after the catch, the realm of the function is not current")
	}
}

var inlineStringSink storage.Value
var inlineTextSink wtf8.String

func BenchmarkInlineString(b *testing.B) {
	for _, text := range []wtf8.String{"abcdefgh", "abcdefghi"} {
		b.Run(fmt.Sprintf("construct/%d", len(text)), func(b *testing.B) {
			cx, _ := engine.NewExecutionContext(nil)
			b.ReportAllocs()
			for b.Loop() {
				inlineStringSink = cx.NewString(text)
			}
		})
		b.Run(fmt.Sprintf("roundtrip/%d", len(text)), func(b *testing.B) {
			cx, _ := engine.NewExecutionContext(nil)
			b.ReportAllocs()
			for b.Loop() {
				inlineTextSink = cx.ToString(cx.NewString(text))
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
	cx, _ := engine.NewExecutionContext(nil)
	otherContext, _ := engine.NewExecutionContext(nil)
	for _, s := range []wtf8.String{"", "a", "ab", "abcdefgh", "abcdefghi", "ab\x00", "\x00\x00\x00\x00\x00\x00\xc2\x80", "日本", "\xed\xa0\x80"} {
		t.Run(fmt.Sprintf("%x", s), func(t *testing.T) {
			v := cx.NewString(s)
			if got := cx.ToString(v); got != s {
				t.Errorf("ToString = %q, want %q", got, s)
			}
			if !storage.IsStrictlyEqual(v, otherContext.NewString(s)) {
				t.Error("equal strings from different VMs differ")
			}
			if len(s) > 0 {
				different := []byte(s)
				different[0] ^= 1
				if storage.IsStrictlyEqual(v, cx.NewString(wtf8.String(different))) {
					t.Error("different payloads compare equal")
				}
			}
			boxed := storage.ObjectValue(v.Object())
			if !storage.IsStrictlyEqual(v, boxed) {
				t.Error("materialized value differs")
			}
			if storage.IsStrictlyEqual(v, cx.NewString(s+"\x00")) {
				t.Error("different lengths compare equal")
			}
			if got := storage.ToBoolean(v); got != (s != "") {
				t.Errorf("ToBoolean = %v", got)
			}
		})
	}
}

func TestInlineStringIndexAllocations(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	s := cx.NewString("abc")
	if got := testing.AllocsPerRun(100, func() { cx.Index(s, 1) }); got != 0 {
		t.Errorf("packed string index allocations = %v, want 0", got)
	}
}

func TestInlineStringCodeUnits(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
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
			v := cx.NewString(tc.text)
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

func TestExoticSetPrototypeOfShapeIsolation(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unmaterialized function setPrototypeOf isolation",
			src: `
				const p = {};
				function getLen(o) { return o.length; }
				function f(a, b) {}
				Object.setPrototypeOf(f, p);
				getLen(Object.create(p));
				String(getLen(f));
			`,
			want: "2",
		},
		{
			name: "lazy Temporal setPrototypeOf isolation",
			src: `
				const p = {};
				function getInstant(o) { return o.Instant; }
				Object.setPrototypeOf(Temporal, p);
				getInstant(Object.create(p));
				typeof getInstant(Temporal);
			`,
			want: "function",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			if got := run(t, &vm, tc.src); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBigIntShiftMinInt64(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `[123n >> (1n << 63n), (-5n) >> (1n << 63n)].join(",")`)
	if want := "0,-1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecoratorAddInitializerAfterThrow(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `
		let savedClass, classErr = "";
		try {
			@((v, ctx) => { savedClass = ctx.addInitializer; throw 1; })
			class E {}
		} catch {}
		try {
			savedClass(() => {});
		} catch (e) {
			classErr = e.name;
		}

		let savedElem, elemErr = "";
		try {
			class F {
				@((v, ctx) => { savedElem = ctx.addInitializer; throw 1; })
				m() {}
			}
		} catch {}
		try {
			savedElem(() => {});
		} catch (e) {
			elemErr = e.name;
		}

		[classErr, elemErr].join(",");
	`)
	if want := "TypeError,TypeError"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDirectEvalDestructuredCatch(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `
		let catchErr = "";
		try {
			try { throw { e: 1 }; } catch ({ e }) { eval("var e = 2"); }
		} catch (e) {
			catchErr = e.name;
		}
		catchErr;
	`)
	if want := "SyntaxError"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDirectEvalParamDefaultArguments(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `
		const errName = (fn) => {
			try {
				fn();
				return "ok";
			} catch (e) {
				return e.name;
			}
		};
		const a = errName(() => (function f(x = eval("var arguments = 42")) { return arguments; })());
		const b = errName(() => (function f(x = eval("function arguments() { return 7; }")) { return arguments(); })());
		const c = errName(() => (function f(x = eval("var arguments = 42")) { var arguments; return arguments; })());
		const d = errName(() => (function f(arguments = eval("var arguments = 42")) { return arguments; })());
		const e = errName(() => (function f({ arguments } = eval("var arguments = 42")) { return arguments; })());
		const f = (function outer() {
			const arrow = (x = eval("var arguments = 42")) => arguments;
			return arrow();
		})();
		const g = (function f({ arguments } = { arguments: 7 }) { return arguments; })();
		const h = (function f(x = eval("{ function arguments() {} }")) { return typeof arguments; })();

		[a, b, c, d, e, f, g, h].join(",");
	`)
	if want := "SyntaxError,SyntaxError,SyntaxError,SyntaxError,SyntaxError,42,7,object"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPromiseTrySpec(t *testing.T) {
	// ES2026 §27.2.4.8 creates the promise capability before calling the callback.
	// Ekuma follows ECMA-262 #3883. It checks for an object receiver, calls the
	// callback, then uses PromiseResolve on success. The pinned Test262 fixture
	// expects this to preserve a returned promise whose constructor is the receiver.
	// Recheck call order and promise identity when adopting a new edition.
	// https://tc39.es/ecma262/2026/multipage/control-abstraction-objects.html#sec-promise.try
	// https://github.com/tc39/ecma262/commit/9235b15340da045e06309b24f82c5e0711ad58f9
	// https://raw.githubusercontent.com/tc39/test262/2e58f87d8350ad51224617a041c6e041d52f8444/test/built-ins/Promise/try/avoids-wrap.js
	var vm ekuma.VM
	got := run(t, &vm, `
		const p = Promise.resolve(1);
		const same = (Promise.try(() => p) === p);

		let nonObjRan = false;
		let nonObjErr = "";
		try {
			Promise.try.call(null, () => { nonObjRan = true; });
		} catch (e) {
			nonObjErr = e.name;
		}

		let nonCtorRan = false;
		let nonCtorErr = "";
		try {
			Promise.try.call({}, () => { nonCtorRan = true; });
		} catch (e) {
			nonCtorErr = e.name;
		}

		const order = [];
		function C(exec) { order.push("C"); exec(() => {}, () => {}); }
		Promise.try.call(C, () => { order.push("cb"); });

		[same, nonObjRan, nonObjErr, nonCtorRan, nonCtorErr, order.join("/")].join(",");
	`)
	if want := "true,false,TypeError,true,TypeError,cb/C"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGeneratorAndAsyncForwardedArgumentsIndex(t *testing.T) {
	var vm ekuma.VM
	run(t, &vm, `
		function* g0() { return [arguments[0], arguments[1], arguments[2]].join(":"); }
		function* g1(a) { return [a, arguments[0], arguments[1], arguments[2]].join(":"); }
		async function a0() { return [arguments[0], arguments[1]].join(":"); }
		async function a1(a) { return [a, arguments[0], arguments[1]].join(":"); }
		async function* ag0() { yield arguments[0]; yield arguments[1]; }

		let out = [];
		out.push(g0(10, 20).next().value);
		out.push(g1(1, 2, 3).next().value);
		a0(30, 40).then(v => out.push(v));
		a1(5, 6).then(v => out.push(v));
		(async () => {
			const it = ag0(70, 80);
			out.push((await it.next()).value + ":" + (await it.next()).value);
		})();
		globalThis.__out = out;
	`)
	if _, err := vm.AwaitJobs(t.Context()); err != nil {
		t.Fatalf("AwaitJobs: %v", err)
	}
	res := run(t, &vm, `globalThis.__out.join("|")`)
	if want := "10:20:|1:1:2:3|30:40|5:5:6|70:80"; res != want {
		t.Errorf("got %q, want %q", res, want)
	}
}

func TestAsyncArrowYieldParamInGenerator(t *testing.T) {
	var vm ekuma.VM
	cases := []string{
		`function* g() { return async yield => yield; }`,
		`function* g() { return async ([yield]) => 1; }`,
		`async function* g() { return async yield => 1; }`,
	}
	for _, src := range cases {
		if _, err := vm.ParseScript("test", src); err == nil {
			t.Errorf("ParseScript(%q) succeeded, want SyntaxError", src)
		}
	}
}

func TestClassFieldKeywordBeforeDecorator(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `
		function dec(v) { return v; }
		class C {
			static
			@dec
			a = 1
			get
			@dec
			b = 2
			set
			@dec
			c = 3
		}
		const inst = new C();
		[
			Object.hasOwn(inst, "static"),
			Object.hasOwn(inst, "get"),
			Object.hasOwn(inst, "set"),
			inst.a,
			inst.b,
			inst.c,
		].join(",");
	`)
	if want := "true,true,true,1,2,3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	for _, bad := range []string{
		`class C { static @dec x = 1 }`,
		`class C { get @dec x = 1 }`,
		`class C { set @dec x = 1 }`,
	} {
		if _, err := vm.ParseScript("test", bad); err == nil {
			t.Errorf("ParseScript(%q) succeeded, want SyntaxError", bad)
		}
	}
}

func TestTemporalNudgeRounding(t *testing.T) {
	var vm ekuma.VM
	got := run(t, &vm, `
		Temporal.PlainDateTime.from("2023-03-31T00:00:00").until(
			"2024-02-29T12:00:00",
			{ largestUnit: "year", smallestUnit: "month", roundingMode: "trunc" },
		).toString();
	`)
	if want := "P11M"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
