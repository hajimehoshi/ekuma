// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestRealmIntrinsic(t *testing.T) {
	cx := engine.HostContext(engine.NewVM(nil))
	var want *engine.Object
	created := cx.CreateRealm(func(cx *engine.ExecutionContext) {
		want = cx.ProtoOf(cx.GlobalObject().Object())
		if got := cx.Realm().Intrinsic(engine.IntrinsicPromisePrototype); got != nil {
			t.Errorf("unset Promise prototype = %p, want nil", got)
		}
	})
	if got := created.Intrinsic(engine.IntrinsicObjectPrototype); got != want {
		t.Errorf("Object prototype = %p, want %p from the selected realm", got, want)
	}
}

func TestCreateRealmThrow(t *testing.T) {
	machine := engine.NewVM(nil)
	cx := engine.HostContext(machine)
	caller := cx.Realm()
	var created *engine.Realm
	thrown, ok := cx.Completion(func() {
		defer func() {
			if engine.HostContext(machine).Realm() != caller {
				t.Error("CreateRealm did not restore its caller after the installer threw")
			}
		}()
		cx.CreateRealm(func(cx *engine.ExecutionContext) {
			created = cx.Realm()
			cx.Throw(engine.ObjectValue(created.Intrinsic(engine.IntrinsicObjectPrototype)))
		})
	})
	if ok {
		t.Fatal("CreateRealm returned normally")
	}
	result := created.CreateIterResultObject(engine.Undefined, true)
	if got := cx.ProtoOf(result.Object()); got != thrown.Object() {
		t.Error("the saved realm produced an iterator result with the wrong prototype after the throw")
	}
}
