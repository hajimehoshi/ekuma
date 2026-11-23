// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// PollInterval is the number of steps of a loop that runs no JavaScript, such as reading,
// comparing or storing an element, between two checks for an interruption.
const PollInterval = 1 << 16

// pollChunk is the number of elements that a bulk copy moves between two checks for an
// interruption.
const pollChunk = 1 << 20

// Pacer counts the steps of a loop that runs no JavaScript and checks for an interruption
// every [PollInterval] steps. The zero value is ready to use.
type Pacer struct {
	steps int
}

// Step counts one step, and calls [VM.Poll] when a check is due.
func (p *Pacer) Step(cx *ExecutionContext) {
	p.steps++
	if p.steps >= PollInterval {
		p.steps = 0
		cx.Poll()
	}
}

// PollAt calls [VM.Poll] when i ends a run of [PollInterval] indices, so that a loop over
// consecutive indices checks for an interruption regularly.
func (vm *VM) PollAt(i int64) {
	if i&(PollInterval-1) == PollInterval-1 {
		vm.Poll()
	}
}

// appendPolled returns append(dst, src...), checking for an interruption while it copies many
// elements. When the call stops, dst is unchanged except past its length.
func appendPolled[T any](cx *ExecutionContext, dst, src []T) []T {
	if len(dst)+len(src) <= pollChunk {
		return append(dst, src...)
	}
	cx.Poll()
	n := len(dst)
	if cap(dst)-n < len(src) {
		grown := make([]T, n, max(n+len(src), cap(dst)+cap(dst)/4))
		CopyPolled(cx, grown, dst)
		dst = grown
	}
	dst = dst[:n+len(src)]
	CopyPolled(cx, dst[n:], src)
	return dst
}

// clearPolled is clear, checking for an interruption while it clears many elements.
func clearPolled[T any](cx *ExecutionContext, s []T) {
	for i := 0; i < len(s); i += pollChunk {
		if i > 0 {
			cx.Poll()
		}
		clear(s[i:min(i+pollChunk, len(s))])
	}
}

// CopyPolled is copy for slices that do not overlap, checking for an interruption while it
// copies many elements.
func CopyPolled[T any](cx *ExecutionContext, dst, src []T) int {
	n := min(len(dst), len(src))
	for i := 0; i < n; i += pollChunk {
		if i > 0 {
			cx.Poll()
		}
		copy(dst[i:n], src[i:min(i+pollChunk, n)])
	}
	return n
}
