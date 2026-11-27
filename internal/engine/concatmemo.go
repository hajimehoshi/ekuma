// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// concatMemo holds the last two strings that concatenations in a realm made, so that a
// concatenation with the contents of one returns it instead of a new string.
//
// It takes the results longer than a short string and at most storage.MaxStackConcatBytes bytes
// that no builder takes. Each is a string without a builder whose text is a copy of its own, made
// by [ExecutionContext.memoizedConcat], so the memo keeps at most two of them alive, with what
// they cache about their contents, none while it is skipped, and never an operand.
type concatMemo struct {
	recent [2]*storage.Object

	// next picks the string to replace.
	next uint8

	gate memoGate
}

// memoGate skips a memo that seldom hits.
type memoGate struct {
	// hits and misses count the lookups since the last check.
	hits   uint32
	misses uint32

	// skip is the number of operations left to make without the memo.
	skip uint32
}

const (
	// memoWindow is the number of misses after which the gate checks the hits.
	memoWindow = 1024

	// The memo is skipped for memoSkip operations when fewer than one lookup in memoRate hit.
	memoRate = 8
	memoSkip = 1 << 16
)

// miss counts a miss. It reports whether the memo is skipped from now on.
func (g *memoGate) miss() bool {
	if g.misses++; g.misses == memoWindow {
		return g.check()
	}
	return false
}

// check ends a window of lookups. It reports whether the memo is skipped from now on.
//
//go:noinline
func (g *memoGate) check() bool {
	// A miss costs more than a concatenation without the memo: the comparisons and keeping the
	// result. A realm whose concatenations seldom repeat would pay that on each.
	skip := uint64(g.hits)*(memoRate-1) < uint64(g.misses)
	if skip {
		g.skip = memoSkip
	}
	g.hits, g.misses = 0, 0
	return skip
}

// memoizedConcat returns the string whose contents are the n bytes of buf, longer than a short
// string, as the result of a concatenation with the concatenation count count.
func (cx *ExecutionContext) memoizedConcat(buf *[storage.MaxStackConcatBytes]byte, n int, count storage.ConcatCount) storage.Value {
	m := &cx.realm.concatMemo
	if m.gate.skip != 0 {
		m.gate.skip--
		return cx.NewConcatenatedString(wtf8.String(buf[:n]), count)
	}
	k := buf[:n]
	// A run of the same concatenation, such as one that each leaf of a tree repeats, finds its
	// result here. A string from the memo keeps the concatenation count it was made with.
	for _, s := range &m.recent {
		if s != nil && string(s.Text()) == string(k) {
			m.gate.hits++
			return storage.Value{O: s}
		}
	}
	v := cx.NewConcatenatedString(wtf8.String(k), count)
	m.recent[m.next&1] = v.O
	m.next++
	if m.gate.miss() {
		// A memo that is skipped keeps no string alive.
		clear(m.recent[:])
	}
	return v
}
