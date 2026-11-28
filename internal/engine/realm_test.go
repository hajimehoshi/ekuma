// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func TestRealmIntrinsic(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	var want *storage.Object
	created := cx.CreateRealm(func(cx *engine.ExecutionContext) {
		want = cx.ProtoOf(cx.GlobalObject().Object())
		if got := cx.Intrinsic(storage.IntrinsicPromisePrototype); got != nil {
			t.Errorf("unset Promise prototype = %p, want nil", got)
		}
	})
	if got := created.Intrinsic(storage.IntrinsicObjectPrototype); got != want {
		t.Errorf("Object prototype = %p, want %p from the selected realm", got, want)
	}
}

func TestBinaryInitializerDefersForGlobalMutations(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	var calls int
	cx.SetBinaryInitializer(func(cx *engine.ExecutionContext) {
		calls++
		proto := cx.NewOrdinaryObject()
		cx.SetIntrinsic(storage.IntrinsicArrayBufferPrototype, proto)
		cx.FillBinaryGlobal("ArrayBuffer", proto)
	})
	cx.ReserveBinaryGlobal("ArrayBuffer")
	cx.ReserveBinaryGlobal("SharedArrayBuffer")
	cx.ReserveBinaryGlobal("DataView")

	g := cx.GlobalObject().Object()
	key := cx.KeyOfString("ArrayBuffer")
	if !cx.HasOwnProperty(g, key) {
		t.Error("reserved global is missing")
	}
	var attrs storage.PropDesc
	attrs.SetWritable(false)
	cx.DefinePropertyOrThrow(g, key, attrs)
	cx.Set(g, cx.KeyOfString("SharedArrayBuffer"), storage.Undefined)
	if !cx.DeleteProp(g, cx.KeyOfString("DataView")) {
		t.Error("reserved global could not be deleted")
	}
	if calls != 0 {
		t.Errorf("global mutations ran the initializer %d times, want 0", calls)
	}
	cx.Intrinsic(storage.IntrinsicArrayBufferPrototype)
	cx.Intrinsic(storage.IntrinsicArrayBufferPrototype)
	if calls != 1 {
		t.Errorf("two intrinsic reads ran the initializer %d times in total, want 1", calls)
	}
}

func TestCreateRealmThrow(t *testing.T) {
	cx, host := engine.NewExecutionContext(nil)
	var created *engine.ExecutionContext
	thrown, ok := cx.Completion(func() {
		defer func() {
			if host.ActiveContext() != cx {
				t.Error("CreateRealm did not restore its caller after the installer threw")
			}
		}()
		cx.CreateRealm(func(cx *engine.ExecutionContext) {
			created = cx
			cx.Throw(storage.ObjectValue(created.Intrinsic(storage.IntrinsicObjectPrototype)))
		})
	})
	if ok {
		t.Fatal("CreateRealm returned normally")
	}
	result := created.CreateIterResultObject(storage.Undefined, true)
	if got := cx.ProtoOf(result.Object()); got != thrown.Object() {
		t.Error("the saved realm produced an iterator result with the wrong prototype after the throw")
	}
}
