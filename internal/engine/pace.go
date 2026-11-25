// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "math/big"

// PollInterval is the number of steps of a loop that runs no JavaScript, such as reading,
// comparing or storing an element, between two checks for an interruption.
const PollInterval = 1 << 16

// pollChunk is the number of elements that a bulk copy moves between two checks for an
// interruption.
const pollChunk = 1 << 20

// compareBudget is the number of bytes that comparisons with one value read between two checks
// for an interruption. A single comparison can read more.
const compareBudget = 1 << 22

// maxQuickCompare is the largest number of bytes whose comparison counts as one step.
const maxQuickCompare = compareBudget / PollInterval

// Pacer counts the steps of a loop that runs no JavaScript and checks for an interruption
// every [PollInterval] steps. The zero value is ready to use.
type Pacer struct {
	steps int
}

// Step counts one step, and calls [agent.Poll] when a check is due.
func (p *Pacer) Step(cx *ExecutionContext) {
	p.steps++
	if p.steps >= PollInterval {
		p.steps = 0
		cx.Poll()
	}
}

// Steps counts n steps, and calls [agent.Poll] when a check is due.
func (p *Pacer) Steps(cx *ExecutionContext, n int) {
	p.steps += n
	if p.steps >= PollInterval {
		p.steps = 0
		cx.Poll()
	}
}

// PollAt calls [agent.Poll] when i ends a run of [PollInterval] indices, so that a loop over
// consecutive indices checks for an interruption regularly.
func (ag *agent) PollAt(i int64) {
	if i&(PollInterval-1) == PollInterval-1 {
		ag.Poll()
	}
}

// pollAt is [agent.PollAt] with [poller.poll].
func (r *realmPart) pollAt(i int64) {
	if i&(PollInterval-1) == PollInterval-1 {
		r.poll()
	}
}

// comparedBytes returns the largest number of bytes that a comparison of v with another value by
// [IsStrictlyEqual] reads, or 0 when that is a small constant.
func comparedBytes(v Value) int {
	if v.o == nil {
		return 0
	}
	switch v.o.kind {
	case kindString:
		// Only strings of the same length are compared.
		return len(v.o.str)
	case kindBigInt:
		if v.o != bigInt64Object {
			return v.o.extState().(*big.Int).BitLen() / 8
		}
	}
	return 0
}

// appendPolled returns append(dst, src...), checking for an interruption while it copies many
// elements. When the call stops, dst is unchanged except past its length.
func appendPolled[T any](p *poller, dst, src []T) []T {
	if len(dst)+len(src) <= pollChunk {
		return append(dst, src...)
	}
	p.poll()
	n := len(dst)
	if cap(dst)-n < len(src) {
		grown := make([]T, n, max(n+len(src), cap(dst)+cap(dst)/4))
		copyPolled(p, grown, dst)
		dst = grown
	}
	dst = dst[:n+len(src)]
	copyPolled(p, dst[n:], src)
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
	return copyPolled(&cx.poller, dst, src)
}

func copyPolled[T any](p *poller, dst, src []T) int {
	n := min(len(dst), len(src))
	for i := 0; i < n; i += pollChunk {
		if i > 0 {
			p.poll()
		}
		copy(dst[i:n], src[i:min(i+pollChunk, n)])
	}
	return n
}
