// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"runtime"
	"sync/atomic"
)

// js/wasm runs one goroutine at a time and switches only when the running one yields.
// A busy script yields now and then, so that the goroutine that calls [agent.Interrupt] can run.

// yieldTicks is the number of ticks between two yields.
const yieldTicks = 1 << 12

// interruptFlag is the flag of a pending interruption.
// Each Load counts a tick and yields every yieldTicks ticks.
type interruptFlag struct {
	b     atomic.Bool
	ticks uint32
}

// Load reports whether the flag is set.
func (f *interruptFlag) Load() bool {
	f.ticks++
	if f.ticks >= yieldTicks {
		f.yield()
	}
	return f.b.Load()
}

// Store sets the flag to v.
func (f *interruptFlag) Store(v bool) {
	f.b.Store(v)
}

// yield lets the other goroutines run, and starts counting the ticks again.
func (f *interruptFlag) yield() {
	f.ticks = 0
	runtime.Gosched()
}

// pollYield yields at each call of [agent.Poll], as its callers call it only between runs of many
// steps. The goroutines that request an interruption may need more than one yield to run.
func (p *poller) pollYield() {
	p.interrupt.yield()
}
