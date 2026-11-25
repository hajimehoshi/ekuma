// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// Pacer counts the steps of a loop that runs no JavaScript and checks for an interruption
// every [storage.PollInterval] steps. The zero value is ready to use.
type Pacer struct {
	steps int
}

// Step counts one step, and calls [agent.Poll] when a check is due.
func (p *Pacer) Step(cx *ExecutionContext) {
	p.steps++
	if p.steps >= storage.PollInterval {
		p.steps = 0
		cx.Poll()
	}
}

// Steps counts n steps, and calls [agent.Poll] when a check is due.
func (p *Pacer) Steps(cx *ExecutionContext, n int) {
	p.steps += n
	if p.steps >= storage.PollInterval {
		p.steps = 0
		cx.Poll()
	}
}

// PollAt calls [agent.Poll] when i ends a run of [storage.PollInterval] indices, so that a loop over
// consecutive indices checks for an interruption regularly.
func (ag *agent) PollAt(i int64) {
	if i&(storage.PollInterval-1) == storage.PollInterval-1 {
		ag.Poll()
	}
}

// PollStep calls [agent.Poll] when an interruption may be pending. It is for every step of a loop
// whose steps can be long, such as looking up a property in a long prototype chain.
func (ag *agent) PollStep() {
	if ag.InterruptFlag.Load() {
		ag.Poll()
	}
}

// CopyPolled is copy for slices that do not overlap, checking for an interruption while it
// copies many elements.
func CopyPolled[T any](cx *ExecutionContext, dst, src []T) int {
	return storage.CopyPolled(&cx.Poller, dst, src)
}
