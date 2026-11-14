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
// that is not an ArrayBuffer.
func DetachArrayBuffer(vm *VM, v Value) error {
	e := v.engine()
	if b := engine.ArrayBufferData(e); b == nil || b.IsShared() {
		return &Error{Value: fromEngine(vm.e.NewError(engine.ErrorKindType, "not an ArrayBuffer"))}
	}
	vm.e.DetachArrayBuffer(e.Object())
	return nil
}
