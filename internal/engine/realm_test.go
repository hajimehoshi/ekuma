// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

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
			vm.Throw(engine.ObjectValue(vm.Intrinsic(engine.IntrinsicObjectPrototype)))
		})
	})
	if ok {
		t.Fatal("CreateRealm returned normally")
	}
	result := vm.CreateIterResultObject(created, engine.Undefined, true)
	if got := vm.ProtoOf(result.Object()); got != thrown.Object() {
		t.Error("the saved realm produced an iterator result with the wrong prototype after the throw")
	}
}
