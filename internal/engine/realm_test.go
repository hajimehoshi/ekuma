// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestRealmIntrinsic(t *testing.T) {
	vm := engine.NewVM(nil)
	var want *engine.Object
	created := vm.CreateRealm(func() {
		want = vm.ProtoOf(vm.GlobalObject().Object())
		if got := vm.CurrentRealm().Intrinsic(engine.IntrinsicPromisePrototype); got != nil {
			t.Errorf("unset Promise prototype = %p, want nil", got)
		}
	})
	if got := created.Intrinsic(engine.IntrinsicObjectPrototype); got != want {
		t.Errorf("Object prototype = %p, want %p from the selected realm", got, want)
	}
}

func TestCreateRealmThrow(t *testing.T) {
	vm := engine.NewVM(nil)
	caller := vm.CurrentRealm()
	var created *engine.Realm
	thrown, ok := vm.Completion(func() {
		defer func() {
			if vm.CurrentRealm() != caller {
				t.Error("CreateRealm did not restore its caller after the installer threw")
			}
		}()
		vm.CreateRealm(func() {
			created = vm.CurrentRealm()
			vm.Throw(engine.ObjectValue(created.Intrinsic(engine.IntrinsicObjectPrototype)))
		})
	})
	if ok {
		t.Fatal("CreateRealm returned normally")
	}
	result := created.CreateIterResultObject(engine.Undefined, true)
	if got := vm.ProtoOf(result.Object()); got != thrown.Object() {
		t.Error("the saved realm produced an iterator result with the wrong prototype after the throw")
	}
}
