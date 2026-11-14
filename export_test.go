// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// DetachArrayBuffer detaches the ArrayBuffer v. The error is a TypeError for a value
// that is not an ArrayBuffer or for an immutable ArrayBuffer.
func DetachArrayBuffer(vm *VM, v Value) error {
	vm.initIfNeeded()
	e := v.engine()
	b := engine.ArrayBufferData(e)
	if b == nil || b.IsShared() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "not an ArrayBuffer"))}
	}
	if b.IsImmutable() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "cannot detach an immutable ArrayBuffer"))}
	}
	vm.e.DetachArrayBuffer(e.Object())
	return nil
}
