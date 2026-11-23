// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestNativeCallLimit(t *testing.T) {
	cx := newTestContext(t)
	var depth, deepest int
	var nest engine.Value
	nest = cx.NewFunction("nest", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		depth++
		deepest = max(deepest, depth)
		defer func() {
			depth--
		}()
		cx.Call(nest.Object(), engine.Undefined)
		return engine.Undefined
	})
	if err := cx.HostSet(cx.GlobalObject(), cx.StringFromGo("nest"), nest); err != nil {
		t.Fatalf("HostSet: %v", err)
	}
	v, err := runScript(cx, "nest", `try { nest(); 0; } catch (e) { e instanceof RangeError ? 1 : 2; }`)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if got := v.Number(); got != 1 {
		t.Errorf("got %v, want 1 (a RangeError)", got)
	}
	if deepest != engine.MaxNatives {
		t.Errorf("got %d nested calls, want %d", deepest, engine.MaxNatives)
	}
}

func TestDerivedReturnErrorLocation(t *testing.T) {
	cx := newTestContext(t)
	var ctor engine.Value
	cx.CreateRealm(func(cx *engine.ExecutionContext) {
		builtin.Install(cx)
		var err error
		ctor, err = runScript(cx, "ctor.js", `(class extends Object {
	constructor() {
		return 1;
	}
})`)
		if err != nil {
			t.Fatal(err)
		}
	})
	if err := cx.HostSet(cx.GlobalObject(), cx.StringFromGo("Ctor"), ctor); err != nil {
		t.Fatal(err)
	}
	callerRealm := cx.Realm()
	var loc engine.ExceptionLocation
	cx.SetExceptionReporter(func(thrown engine.Value, site engine.ExceptionLocation) {
		loc = site
		if thrown.Object().CreationRealm() != callerRealm {
			t.Error("derived constructor return error was created outside the caller's realm")
		}
		if got := cx.ProtoOf(thrown.Object()); got != callerRealm.Intrinsic(engine.IntrinsicTypeErrorPrototype) {
			t.Error("derived constructor return error has the wrong prototype")
		}
	})
	if _, err := runScript(cx, "caller.js", "new Ctor()"); err == nil {
		t.Error("expected the derived constructor to throw")
	}
	if loc.File != "ctor.js" || loc.Line != 3 || loc.Column != 3 || loc.SourceLine != "\t\treturn 1;" {
		t.Errorf("got throw site %+v, want the return at ctor.js:3:3", loc)
	}
}
