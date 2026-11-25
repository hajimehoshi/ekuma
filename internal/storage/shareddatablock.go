// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"sync"
	"sync/atomic"
)

// SharedDataBlock is a Shared Data Block: the bytes of a SharedArrayBuffer, which the agents of an
// agent cluster share. Its methods may be called from any goroutine.
type SharedDataBlock struct {
	// mu guards data and WaiterLists. Every access to the bytes holds it.
	mu sync.Mutex

	data []byte

	// length is len(data), which only grows, for reading without mu.
	length atomic.Int64

	// max is [[ArrayBufferMaxByteLength]], or -1 for a block whose length is fixed.
	max int

	// WaiterLists are the waiter lists of the block, or nil until there is a waiter. They belong
	// to the engine. Only its waiterListsOf and compareAndAdd use the field, between Lock and
	// Unlock.
	WaiterLists any
}

// NewSharedDataBlock returns a block of byteLength zero bytes. maxByteLength is -1 for a block whose
// length is fixed.
func NewSharedDataBlock(byteLength, maxByteLength int64) *SharedDataBlock {
	blk := &SharedDataBlock{
		data: make([]byte, byteLength),
		max:  int(maxByteLength),
	}
	blk.length.Store(byteLength)
	return blk
}

// Lock returns the bytes of blk and keeps the other agents from accessing them and the waiter lists
// until [SharedDataBlock.Unlock] is called.
func (blk *SharedDataBlock) Lock() []byte {
	blk.mu.Lock()
	return blk.data
}

// Unlock ends the access of [SharedDataBlock.Lock].
func (blk *SharedDataBlock) Unlock() {
	blk.mu.Unlock()
}

func (blk *SharedDataBlock) byteLength() int {
	return int(blk.length.Load())
}
