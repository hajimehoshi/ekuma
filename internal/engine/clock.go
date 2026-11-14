// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// DefaultTimeZone returns the time zone of local time. It is the local time zone of the host
// unless [VM.SetDefaultTimeZone] changed it.
func (vm *VM) DefaultTimeZone() datetime.TimeZone {
	if vm.timeZone == nil {
		return datetime.Local()
	}
	return vm.timeZone
}

// SetDefaultTimeZone sets the time zone of local time to tz.
func (vm *VM) SetDefaultTimeZone(tz datetime.TimeZone) {
	vm.timeZone = tz
}

// SystemUTCEpochMilliseconds returns the current time as a time value.
func (vm *VM) SystemUTCEpochMilliseconds() float64 {
	return float64(time.Now().UnixMilli())
}
