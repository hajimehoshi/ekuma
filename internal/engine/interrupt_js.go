// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"sync/atomic"
)

// js/wasm runs one goroutine at a time and switches only when the running one yields.
// A busy script yields now and then, so that the goroutine that calls [VM.Interrupt] can run.
const (
	// yieldTicks is the number of ticks between two yields.
	yieldTicks = 1 << 12

	// pollTicks is the number of ticks that a call of [VM.Poll] counts for, as its callers call it
	// only every so many steps.
	pollTicks = 1 << 8
)

// interruptFlag is the flag of a pending interruption.
// Each Load counts a tick and yields every yieldTicks ticks.
type interruptFlag struct {
	b     atomic.Bool
	ticks uint32
}

// Load reports whether the flag is set.
func (f *interruptFlag) Load() bool {
	f.tick(1)
	return f.b.Load()
}

// Store sets the flag to v.
func (f *interruptFlag) Store(v bool) {
	f.b.Store(v)
}

func (f *interruptFlag) tick(n uint32) {
	f.ticks += n
	if f.ticks >= yieldTicks {
		f.ticks = 0
		runtime.Gosched()
	}
}

// pollTick accounts for a call of [VM.Poll].
func (vm *VM) pollTick() {
	vm.interrupt.tick(pollTicks - 1)
}
