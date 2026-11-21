// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"runtime"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// An unsupported call ends the test even if its script catches exceptions.
type v8GCWithArguments struct{}

func installV8GC(vm *engine.VM) {
	gc := vm.NewFunction("gc", func(vm *engine.VM, _ engine.Value, args []engine.Value) engine.Value {
		if len(args) != 0 {
			readV8GCOptions(vm, args[0])
			panic(v8GCWithArguments{})
		}
		runtime.GC()
		return engine.Undefined
	})
	// The V8 extension declares gc as a global var.
	vm.Define(vm.GlobalObject().Object(), vm.Intern("gc"), gc, engine.FlagWritable|engine.FlagEnumerable)
}

// d8 reads these properties in order before selecting a collection. A getter
// can throw even when the collection itself would be unsupported.
func readV8GCOptions(vm *engine.VM, options engine.Value) {
	if !options.IsObject() {
		return
	}
	typ := vm.GetV(options, vm.Intern("type"))
	vm.GetV(options, vm.Intern("execution"))
	vm.GetV(options, vm.Intern("flavor"))
	if typ.IsString() && typ.Str() == "major-snapshot" {
		vm.GetV(options, vm.Intern("filename"))
	}
}

// CheckExposeGC checks the flag and the gc binding in d8 contexts.
func (host *V8) CheckExposeGC(t *testing.T) {
	test := v8Test{
		file:  "test/mjsunit/gc.js",
		flags: []string{"--expose-gc"},
	}
	check := func(src string, want suiteOutcome) {
		t.Helper()
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte(src),
			},
		}
		if got := test.run(t.Context(), host, checkout, testTimeout); got.outcome != want {
			t.Errorf("outcome = %v, want %v (%s)", got.outcome, want, got.reason)
		}
	}
	check(`// Flags: --expose_gc
		const d = Object.getOwnPropertyDescriptor(globalThis, "gc");
		if (d.value !== gc || !d.writable || !d.enumerable || d.configurable) throw Error("gc descriptor");
		const r = Realm.create();
		if (gc === Realm.eval(r, "gc") || !Realm.eval(r, "gc instanceof Function && gc() === undefined")) {
			throw Error("child realm gc");
		}
		if (new ShadowRealm().evaluate("gc()") !== undefined) throw Error("ShadowRealm gc");
	`, suitePassed)
	check(`if ("gc" in globalThis || Realm.eval(Realm.create(), "'gc' in globalThis")) throw Error("unexpected gc");`, suitePassed)
	check(`// Flags: --expose-gc --allow-natives-syntax
		throw Error("ran with an unsupported flag");`, suiteSkipped)
	check(`// Flags: --expose-gc
		const token = {};
		const reads = [];
		let caught;
		try {
			gc({
				get type() { reads.push("type"); return "major-snapshot"; },
				get execution() { reads.push("execution"); return "async"; },
				get flavor() { reads.push("flavor"); },
				get filename() { reads.push("filename"); throw token; },
			});
		} catch (e) { caught = e; }
		if (caught !== token) throw Error("expected the filename getter's exception");
		if (reads.join() !== "type,execution,flavor,filename") throw Error("gc option reads: " + reads);
	`, suitePassed)
	check(`// Flags: --expose-gc
		try {
			gc({type: new String("major-snapshot"), execution: "async", get filename() { throw 0; }});
		} catch {}
	`, suiteSkipped)
}
