// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// SetTimeZone sets the time zone of local time in vm to loc.
func SetTimeZone(vm *VM, loc *time.Location) {
	vm.e.SetDefaultTimeZone(datetime.LocationZone(loc))
}

// DetachArrayBuffer detaches the ArrayBuffer v. The error is a TypeError for a value
// that is not an ArrayBuffer or for an immutable ArrayBuffer.
func DetachArrayBuffer(vm *VM, v Value) error {
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
