// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"time"
)

// DefaultTimeZone returns the time zone of local time. It is time.Local unless SetDefaultTimeZone changed it.
func (vm *VM) DefaultTimeZone() *time.Location {
	if vm.timeZone == nil {
		return time.Local
	}
	return vm.timeZone
}

// SetDefaultTimeZone sets the time zone of local time to loc.
func (vm *VM) SetDefaultTimeZone(loc *time.Location) {
	vm.timeZone = loc
}

// SystemUTCEpochMilliseconds returns the current time as a time value.
func (vm *VM) SystemUTCEpochMilliseconds() float64 {
	return float64(time.Now().UnixMilli())
}
