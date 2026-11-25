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
			cx := newTestContext(t)
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
	cx, _ := engine.NewExecutionContext(nil)
	o := cx.NewString("abc").Object()
	if _, ok := engine.Element(o, 0); ok {
		t.Error("Element reported a stored element for a primitive")
	}
	if o.SetElement(0, engine.True) {
		t.Error("SetElement reported a stored element for a primitive")
	}
}

func TestCachedReadThroughLazyPrototype(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	key := cx.Intern("value")
	var receiver *engine.Object
	proto := cx.NewLazyObject(nil, func(cx *engine.ExecutionContext, proto *engine.Object) {
		cx.Define(receiver, key, engine.Number(2), engine.FlagsData)
		cx.Define(proto, key, engine.Number(1), engine.FlagsData)
	})
	receiver = cx.NewObject(proto)

	var cache engine.PropertyCache
	for _, want := range []float64{1, 2} {
		got := cx.GetVCached(engine.ObjectValue(receiver), engine.NamedKey(key), &cache)
		if !got.IsNumber() || got.Number() != want {
			t.Errorf("read returned %v, want %v", got, want)
		}
	}
}

// Each operation gets a fresh object so an earlier lookup cannot initialize it.
func TestLazyPropertyOperations(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
	}{
		{
			name:   "indexed getter",
			script: `const child = Object.create(subject); child[1] === child;`,
		},
		{
			name:   "own descriptor",
			script: `Object.getOwnPropertyDescriptor(subject, "0")?.value;`,
		},
		{
			name:   "inherited query key",
			script: `["installed", "ByInit"].join("") in subject;`,
		},
		{
			name:   "inherited read-only index",
			script: `Reflect.set(Object.create(subject), 0, false);`,
		},
		{
			name:   "redefine index",
			script: `Reflect.defineProperty(subject, 1, {value: 2});`,
		},
		{
			name:   "delete index",
			script: `delete subject[0]; Reflect.ownKeys(subject).includes("0");`,
		},
		{
			name: "array species constructor",
			script: `
				const proto = Object.getPrototypeOf(subject);
				proto.constructor = Array;
				delete Array.prototype.constructor;
				Object.setPrototypeOf(Array.prototype, subject);
				[].slice();
				Object.hasOwn(proto, "installedByInit");
			`,
		},
		{
			name: "array iterator",
			script: `
				const proto = Object.getPrototypeOf(subject);
				proto[Symbol.iterator] = Array.prototype.values;
				delete Array.prototype[Symbol.iterator];
				Object.setPrototypeOf(Array.prototype, subject);
				for (const value of []) {}
				Object.hasOwn(proto, "installedByInit");
			`,
		},
		{
			name:   "enumerate",
			script: `Reflect.ownKeys(subject).join();`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runLazyPropertyOperation(t, tc.script, true)
			want := runLazyPropertyOperation(t, tc.script, false)
			if got != want {
				t.Errorf("lazy object returned %q, eagerly initialized object returned %q", got, want)
			}
		})
	}
}

func runLazyPropertyOperation(t *testing.T, script string, lazy bool) string {
	t.Helper()
	cx := newTestContext(t)
	proto := cx.NewObject(nil)
	init := func(cx *engine.ExecutionContext, o *engine.Object) {
		var data engine.PropDesc
		data.SetValue(engine.True)
		data.SetConfigurable(true)
		cx.DefinePropertyOrThrow(o, engine.IndexKey(0), data)
		var accessor engine.PropDesc
		accessor.SetGetter(cx.NewFunction("get 1", func(_ *engine.ExecutionContext, this engine.Value, _ []engine.Value) engine.Value {
			return this
		}).Object())
		cx.DefinePropertyOrThrow(o, engine.IndexKey(1), accessor)
		cx.Define(proto, cx.Intern("installedByInit"), engine.True, engine.FlagsData)
	}
	var subject *engine.Object
	if lazy {
		subject = cx.NewLazyObject(proto, init)
	} else {
		subject = cx.NewObject(proto)
		init(cx, subject)
	}
	cx.Define(cx.GlobalObject().Object(), cx.Intern("subject"), engine.ObjectValue(subject), engine.FlagsData)
	v, err := runScript(cx, "lookup", script)
	if err != nil {
		t.Fatalf("lazy=%t: %v", lazy, err)
	}
	return string(cx.ToString(v))
}

func TestGlobalReadThroughLazyPrototype(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	declare, err := cx.ParseScript("declare", "let fromLazy = 2;")
	if err != nil {
		t.Fatal(err)
	}
	global := cx.GlobalObject().Object()
	proto := cx.NewLazyObject(cx.ProtoOf(global), func(cx *engine.ExecutionContext, o *engine.Object) {
		cx.Define(o, cx.Intern("fromLazy"), engine.Number(1), engine.FlagsData)
		declare.CallScript()
	})
	cx.SetPrototypeOf(global, proto)
	v, err := runScript(cx, "read", "fromLazy")
	if err != nil {
		t.Fatal(err)
	}
	if !v.IsNumber() || v.Number() != 2 {
		t.Errorf("global read returned %v, want 2", v)
	}
}

func TestPlainOwnPropertyQueryDefersLazyInitialization(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	o := cx.NewLazyObject(nil, func(*engine.ExecutionContext, *engine.Object) {
		t.Error("side-effect-free query ran the initializer")
	})
	if _, ok := engine.HasOwnPropertyPlain(cx.Quiet(), o, engine.Number(0)); ok {
		t.Error("side-effect-free query accepted an uninitialized object")
	}
}

func TestCachedStringReadAcrossRealms(t *testing.T) {
	a, _ := engine.NewExecutionContext(nil)
	key := a.Intern("value")
	a.Define(a.Intrinsic(engine.IntrinsicStringPrototype), key, engine.Number(1), engine.FlagsData)
	b := a.CreateRealm(func(cx *engine.ExecutionContext) {
		cx.Define(cx.Intrinsic(engine.IntrinsicStringPrototype), key, engine.Number(2), engine.FlagsData)
	})

	str := a.NewString("text")
	var cache engine.PropertyCache
	if got := a.GetVCached(str, engine.NamedKey(key), &cache); !got.IsNumber() || got.Number() != 1 {
		t.Errorf("read in first realm returned %v, want 1", got)
	}
	if got := b.GetVCached(str, engine.NamedKey(key), &cache); !got.IsNumber() || got.Number() != 2 {
		t.Errorf("read in second realm returned %v, want 2", got)
	}
}

func TestGetPrototypeFromConstructorMissingIntrinsic(t *testing.T) {
	cx := newTestContext(t)
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
