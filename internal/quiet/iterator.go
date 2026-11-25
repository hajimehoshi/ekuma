// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// IterationKind identifies what an iterator yields.
type IterationKind uint8

const (
	IterateKeys IterationKind = iota
	IterateValues
	IterateEntries
)

// PairStep is the outcome of a step of an iterator that yields pairs.
type PairStep uint8

const (
	// PairDeclined says that the step did nothing.
	PairDeclined PairStep = iota

	// PairDone says that the iterator is done.
	PairDone

	// PairYielded says that the iterator yielded the two values of a pair.
	PairYielded
)

type StringIterator struct {
	Str wtf8.String
	pos int
}

func StringIteratorStep(q Context, _ *storage.Object, it *StringIterator) (storage.Value, bool) {
	if it.pos >= len(it.Str) {
		return storage.Undefined, false
	}
	_, size := wtf8.DecodeRune(it.Str[it.pos:])
	s := it.Str[it.pos : it.pos+size]
	it.pos += size
	return q.NewString(s), true
}
