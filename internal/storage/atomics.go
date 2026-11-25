// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/element"
)

// AtomicOp is the operation of [TypedArray.AtomicReadModifyWrite].
type AtomicOp uint8

const (
	AtomicAdd AtomicOp = iota
	AtomicAnd
	AtomicExchange
	AtomicOr
	AtomicSub
	AtomicXor

	// AtomicCompareExchange stores the replacement when the element equals the operand.
	AtomicCompareExchange
)

// AtomicReadModifyWrite implements the steps of AtomicReadModifyWrite and Atomics.compareExchange
// that follow RevalidateAtomicAccess: it applies op with the encoding v to the element i of t,
// which must be a valid integer index, and returns the encoding that the element had. replacement
// is the encoding that AtomicCompareExchange stores.
func (t *TypedArray) AtomicReadModifyWrite(i int, op AtomicOp, v, replacement uint64) uint64 {
	b := t.Lock()
	defer t.Unlock()
	e := b[i*t.Type.Size():]
	old := element.Load(e, t.Type)
	var x uint64
	switch op {
	case AtomicAdd:
		x = old + v
	case AtomicAnd:
		x = old & v
	case AtomicExchange:
		x = v
	case AtomicOr:
		x = old | v
	case AtomicSub:
		x = old - v
	case AtomicXor:
		x = old ^ v
	case AtomicCompareExchange:
		if !element.Equal(t.Type, old, v) {
			return old
		}
		x = replacement
	}
	element.Store(e, t.Type, x)
	return old
}

// AtomicLoad returns the encoding of the element i of t, which must be a valid integer index,
// as a sequentially consistent read.
func (t *TypedArray) AtomicLoad(i int) uint64 {
	x, _ := t.LoadElement(i)
	return x
}

// AtomicStore stores the encoding x as the element i of t, which must be a valid integer index,
// as a sequentially consistent write.
func (t *TypedArray) AtomicStore(i int, x uint64) {
	t.StoreElement(i, x)
}
