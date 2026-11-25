// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math/big"
)

// PollInterval is the number of steps of a loop that runs no JavaScript, such as reading,
// comparing or storing an element, between two checks for an interruption.
const PollInterval = 1 << 16

// PollChunk is the number of elements that a bulk copy moves between two checks for an
// interruption.
const PollChunk = 1 << 20

// compareBudget is the number of bytes that comparisons with one value read between two checks
// for an interruption. A single comparison can read more.
const compareBudget = 1 << 22

// maxQuickCompare is the largest number of bytes whose comparison counts as one step.
const maxQuickCompare = compareBudget / PollInterval

// PollAtIndex is agent.PollAt with [Poller.Poll].
func (r *RealmPart) PollAtIndex(i int64) {
	if i&(PollInterval-1) == PollInterval-1 {
		r.Poll()
	}
}

// comparedBytes returns the largest number of bytes that a comparison of v with another value by
// [IsStrictlyEqual] reads, or 0 when that is a small constant.
func comparedBytes(v Value) int {
	if v.O == nil {
		return 0
	}
	switch v.O.Kind {
	case KindString:
		// Only strings of the same length are compared.
		return len(v.O.Text)
	case KindBigInt:
		if v.O != BigInt64Object {
			return v.O.ExtState().(*big.Int).BitLen() / 8
		}
	}
	return 0
}

// AppendPolled returns append(dst, src...), checking for an interruption while it copies many
// elements. When the call stops, dst is unchanged except past its length.
func AppendPolled[T any](p *Poller, dst, src []T) []T {
	if len(dst)+len(src) <= PollChunk {
		return append(dst, src...)
	}
	p.Poll()
	n := len(dst)
	if cap(dst)-n < len(src) {
		grown := make([]T, n, max(n+len(src), cap(dst)+cap(dst)/4))
		CopyPolled(p, grown, dst)
		dst = grown
	}
	dst = dst[:n+len(src)]
	CopyPolled(p, dst[n:], src)
	return dst
}

func CopyPolled[T any](p *Poller, dst, src []T) int {
	n := min(len(dst), len(src))
	for i := 0; i < n; i += PollChunk {
		if i > 0 {
			p.Poll()
		}
		copy(dst[i:n], src[i:min(i+PollChunk, n)])
	}
	return n
}
