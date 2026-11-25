// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"slices"
	"sync/atomic"
)

// ArrayBuffer is the data block of an ArrayBuffer or SharedArrayBuffer object.
type ArrayBuffer struct {
	// Data is the data block of a buffer that is not shared, and nil for a shared buffer.
	Data []byte

	// block is the data block of a shared buffer, and nil otherwise.
	block *SharedDataBlock

	// max is [[ArrayBufferMaxByteLength]], or -1 for a buffer whose length is fixed.
	max int

	// state is bufferDetached or bufferShared for a buffer whose bytes are not in Data,
	// so that one check sends both to the slow paths.
	state bufferState

	Immutable bool

	// hasDetachKey reports whether [[ArrayBufferDetachKey]] is set, which only the host's buffers
	// have, so that JavaScript cannot detach them.
	hasDetachKey bool

	// slowLength is the length of a buffer whose state is not bufferPlain: that of its shared
	// data block, or -1 when it is detached.
	slowLength *atomic.Int64
}

// detachedLength is the slowLength of the detached buffers.
var detachedLength = func() *atomic.Int64 {
	var n atomic.Int64
	n.Store(-1)
	return &n
}()

// bufferState is the state of an [ArrayBuffer] that its fast paths do not handle.
type bufferState uint8

const (
	bufferPlain bufferState = iota
	bufferDetached
	bufferShared
)

// Bytes returns the contents of b, or nil when b is shared or detached. The slice stays valid
// until b is resized or detached, and must not be modified when b is immutable. The bytes
// of a shared buffer are reached with [ArrayBuffer.Lock].
func (b *ArrayBuffer) Bytes() []byte {
	return b.Data
}

// Lock returns the contents of b. For a shared b, it also keeps the other agents from accessing
// the data block until [ArrayBuffer.Unlock] is called. No JavaScript may run and no other shared
// data block may be locked in between. The slice is valid until then.
func (b *ArrayBuffer) Lock() []byte {
	if b.block != nil {
		b.block.mu.Lock()
		return b.block.data
	}
	return b.Data
}

// Unlock ends the access of [ArrayBuffer.Lock].
func (b *ArrayBuffer) Unlock() {
	if b.block != nil {
		b.block.mu.Unlock()
	}
}

// ByteLength returns the length of b in bytes, which is 0 when b is detached.
func (b *ArrayBuffer) ByteLength() int {
	if b.block != nil {
		return b.block.byteLength()
	}
	return len(b.Data)
}

// MaxByteLength returns [[ArrayBufferMaxByteLength]] of a resizable or growable b,
// and its length otherwise.
func (b *ArrayBuffer) MaxByteLength() int {
	if b.max < 0 {
		return b.ByteLength()
	}
	return b.max
}

// IsResizable reports whether b is a resizable ArrayBuffer or a growable SharedArrayBuffer.
func (b *ArrayBuffer) IsResizable() bool {
	return b.max >= 0
}

// IsDetached implements IsDetachedBuffer.
func (b *ArrayBuffer) IsDetached() bool {
	return b.state == bufferDetached
}

// IsShared implements IsSharedArrayBuffer.
func (b *ArrayBuffer) IsShared() bool {
	return b.block != nil
}

// IsImmutable implements IsImmutableBuffer.
func (b *ArrayBuffer) IsImmutable() bool {
	return b.Immutable
}

// HasDetachKey reports whether [[ArrayBufferDetachKey]] of b is not empty.
func (b *ArrayBuffer) HasDetachKey() bool {
	return b.hasDetachKey
}

// SameDataBlock reports whether b and c have the same data block.
func (b *ArrayBuffer) SameDataBlock(c *ArrayBuffer) bool {
	return b == c || b.block != nil && b.block == c.block
}

// SharedDataBlock returns the data block of the shared buffer b, or nil when b is not shared.
// Another agent, which may run on another goroutine, creates a SharedArrayBuffer over the block with
// [RealmPart.NewSharedArrayBufferObject].
func (b *ArrayBuffer) SharedDataBlock() *SharedDataBlock {
	return b.block
}

// Resize changes the length of the resizable b, which is not shared, to n, which must not exceed
// [ArrayBuffer.MaxByteLength]. New bytes are zero.
func (b *ArrayBuffer) Resize(n int) {
	b.Data = resizeBytes(b.Data, n, b.max)
}

// Grow implements the growing of the growable SharedArrayBuffer b to n bytes, which must not exceed
// [ArrayBuffer.MaxByteLength]. It reports false, without changing b, when n is smaller than the
// length, which another agent may have grown.
func (b *ArrayBuffer) Grow(n int) bool {
	blk := b.block
	blk.mu.Lock()
	defer blk.mu.Unlock()
	if n < len(blk.data) {
		return false
	}
	blk.data = resizeBytes(blk.data, n, b.max)
	blk.length.Store(int64(n))
	return true
}

// resizeBytes returns data resized to n bytes, which must not exceed maxLen. New bytes are zero.
func resizeBytes(data []byte, n, maxLen int) []byte {
	old := len(data)
	if n <= cap(data) {
		data = data[:n]
		if n > old {
			clear(data[old:n])
		}
		return data
	}
	// The capacity grows geometrically, so that growing by small steps takes linear time.
	d := make([]byte, n, min(max(n, 2*cap(data)), maxLen))
	copy(d, data)
	return d
}

// CopyBufferBytes copies n bytes from src at srcOffset to dst at dstOffset, as copy does when the
// two have the same data block. Both ranges must be within the buffers, which are not detached.
func CopyBufferBytes(dst *ArrayBuffer, dstOffset int, src *ArrayBuffer, srcOffset, n int) {
	if dst.block == nil && src.block == nil {
		copy(dst.Data[dstOffset:dstOffset+n], src.Data[srcOffset:srcOffset+n])
		return
	}
	if dst.SameDataBlock(src) {
		copyWithin(dst, dstOffset, srcOffset, n)
		return
	}
	// The source is read before the destination is locked, as one agent locks one block at a time.
	writeBytes(dst, dstOffset, readBytes(src, srcOffset, n))
}

// readBytes returns the n bytes of b at offset, copied when b is shared.
func readBytes(b *ArrayBuffer, offset, n int) []byte {
	d := b.Lock()
	defer b.Unlock()
	if b.block != nil {
		return slices.Clone(d[offset : offset+n])
	}
	return d[offset : offset+n]
}

// writeBytes copies src to b at offset.
func writeBytes(b *ArrayBuffer, offset int, src []byte) {
	d := b.Lock()
	defer b.Unlock()
	copy(d[offset:offset+len(src)], src)
}

// copyWithin copies n bytes of b from srcOffset to dstOffset.
func copyWithin(b *ArrayBuffer, dstOffset, srcOffset, n int) {
	d := b.Lock()
	defer b.Unlock()
	copy(d[dstOffset:dstOffset+n], d[srcOffset:srcOffset+n])
}

// NewSharedArrayBufferObject returns a SharedArrayBuffer with the prototype proto over the data
// block blk, which may come from another agent.
func (r *RealmPart) NewSharedArrayBufferObject(proto *Object, blk *SharedDataBlock) *Object {
	return NewObjectWithState(r.ShapeFor(proto), ClassArrayBuffer, &ArrayBuffer{
		block:      blk,
		max:        blk.max,
		state:      bufferShared,
		slowLength: &blk.length,
	})
}

// NewArrayBufferFromBytes returns an ArrayBuffer with the prototype proto whose data is b,
// which it keeps. maxByteLength is -1 for a buffer whose length is fixed and must not be
// smaller than len(b) otherwise.
func (r *RealmPart) NewArrayBufferFromBytes(proto *Object, b []byte, maxByteLength int) *Object {
	w := &arrayBufferObject{
		state: ArrayBuffer{
			Data: b,
			max:  maxByteLength,
		},
	}
	return w.InitWithState(r.ShapeFor(proto), ClassArrayBuffer, &w.state)
}

// arrayBufferObject is an ArrayBuffer object together with its state, so that creating one is
// a single allocation.
type arrayBufferObject struct {
	PropertyObject
	state ArrayBuffer
}

// ArrayBufferData returns the [[ArrayBufferData]] of v, or nil when v is neither an ArrayBuffer
// nor a SharedArrayBuffer object.
func ArrayBufferData(v Value) *ArrayBuffer {
	if v.IsObject() && v.O.Class == ClassArrayBuffer {
		return v.O.ExtState().(*ArrayBuffer)
	}
	return nil
}

// NewImmutableArrayBufferFromBytes returns an immutable ArrayBuffer with the prototype proto
// whose data is b, which it keeps. b must not be modified afterwards.
func (r *RealmPart) NewImmutableArrayBufferFromBytes(proto *Object, b []byte) *Object {
	o := r.NewArrayBufferFromBytes(proto, b, -1)
	o.ExtState().(*ArrayBuffer).Immutable = true
	return o
}

// NewHostArrayBufferObject returns an ArrayBuffer of fixed length with the prototype proto whose
// data block is b, which it keeps. Its [[ArrayBufferDetachKey]] is set, so that only the host
// detaches it. len(b) must not exceed the largest length of a buffer.
func (r *RealmPart) NewHostArrayBufferObject(proto *Object, b []byte) *Object {
	// The capacity is cut, so that no operation reaches the bytes of the host beyond len(b).
	o := r.NewArrayBufferFromBytes(proto, b[:len(b):len(b)], -1)
	o.ExtState().(*ArrayBuffer).hasDetachKey = true
	return o
}

// Detach implements the steps of DetachArrayBuffer that follow its checks: b has no data block
// afterwards. b must be neither shared nor immutable. Detaching a detached b does nothing.
func (b *ArrayBuffer) Detach() {
	b.Data = nil
	b.state = bufferDetached
	b.slowLength = detachedLength
}
