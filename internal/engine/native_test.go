// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestNativeCallLimit(t *testing.T) {
	vm := newVM(t)
	var depth, deepest int
	var nest engine.Value
	nest = vm.NewFunction("nest", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		depth++
		deepest = max(deepest, depth)
		defer func() {
			depth--
		}()
		vm.Call(nest.Object(), engine.Undefined)
		return engine.Undefined
	})
	if err := vm.HostSet(vm.GlobalObject(), vm.StringFromGo("nest"), nest); err != nil {
		t.Fatalf("HostSet: %v", err)
	}
	v, err := runScript(vm, "nest", `try { nest(); 0; } catch (e) { e instanceof RangeError ? 1 : 2; }`)
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
