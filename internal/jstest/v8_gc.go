// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"runtime"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// v8GCEnabled contains the test VMs running with --expose-gc.
var v8GCEnabled sync.Map

// An unsupported call ends the test even if its script catches exceptions.
type v8GCWithArguments struct{}

func init() {
	builtin.RegisterExtension(func(vm *engine.VM) {
		host, _ := vm.HostDefined().(*ekuma.VM)
		if _, ok := v8GCEnabled.Load(host); !ok {
			return
		}
		gc := vm.NewFunction("gc", func(_ *engine.VM, _ engine.Value, args []engine.Value) engine.Value {
			if len(args) != 0 {
				panic(v8GCWithArguments{})
			}
			runtime.GC()
			return engine.Undefined
		})
		// The V8 extension declares gc as a global var.
		vm.Define(vm.GlobalObject().Object(), vm.Intern("gc"), gc, engine.FlagWritable|engine.FlagEnumerable)
	})
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
		try { gc({execution: "async"}); } catch {}`, suiteSkipped)
}
