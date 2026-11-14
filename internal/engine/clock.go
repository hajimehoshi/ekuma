// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/rand/v2"
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

// SetDefaultTimeZone sets the time zone of local time to tz. A nil tz restores the local time
// zone of the host.
func (vm *VM) SetDefaultTimeZone(tz datetime.TimeZone) {
	vm.timeZone = tz
}

// SystemUTCEpochMilliseconds returns the current time as a time value.
func (vm *VM) SystemUTCEpochMilliseconds() float64 {
	return float64(time.Now().UnixMilli())
}

// SetRandomSource sets src as the source of [VM.RandomNumber]. A nil src restores the global
// source of math/rand/v2.
func (vm *VM) SetRandomSource(src rand.Source) {
	vm.randomSource = src
}

// RandomNumber returns a number in [0, 1) for Math.random. With a source, it is the number
// that [rand.Rand.Float64] would return.
func (vm *VM) RandomNumber() float64 {
	if vm.randomSource == nil {
		return rand.Float64()
	}
	// The low 53 bits, as rand.Rand.Float64 takes them.
	return float64(vm.randomSource.Uint64()<<11>>11) / (1 << 53)
}
