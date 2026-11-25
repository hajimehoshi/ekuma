// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"encoding/binary"
	"math"
	"math/big"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/waiterlist"
)

// This file holds ArrayBuffer, SharedArrayBuffer, the typed arrays and DataView objects.
// Elements are stored in little-endian byte order on every platform.

// ArrayBuffer is the data block of an ArrayBuffer or SharedArrayBuffer object.
type ArrayBuffer struct {
	// data is the data block of a buffer that is not shared, and nil for a shared buffer.
	data []byte

	// block is the data block of a shared buffer, and nil otherwise.
	block *SharedDataBlock

	// max is [[ArrayBufferMaxByteLength]], or -1 for a buffer whose length is fixed.
	max int

	// state is bufferDetached or bufferShared for a buffer whose bytes are not in data,
	// so that one check sends both to the slow paths.
	state bufferState

	immutable bool

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
	return b.data
}

// Lock returns the contents of b. For a shared b, it also keeps the other agents from accessing
// the data block until [ArrayBuffer.Unlock] is called. No JavaScript may run and no other shared
// data block may be locked in between. The slice is valid until then.
func (b *ArrayBuffer) Lock() []byte {
	if b.block != nil {
		b.block.mu.Lock()
		return b.block.data
	}
	return b.data
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
	return len(b.data)
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
	return b.immutable
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
// [ExecutionContext.NewSharedArrayBufferObject].
func (b *ArrayBuffer) SharedDataBlock() *SharedDataBlock {
	return b.block
}

// Resize changes the length of the resizable b, which is not shared, to n, which must not exceed
// [ArrayBuffer.MaxByteLength]. New bytes are zero.
func (b *ArrayBuffer) Resize(n int) {
	b.data = resizeBytes(b.data, n, b.max)
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

// SharedDataBlock is a Shared Data Block: the bytes of a SharedArrayBuffer, which the agents of an
// agent cluster share. Its methods may be called from any goroutine.
type SharedDataBlock struct {
	// mu guards data and waiterLists. Every access to the bytes holds it.
	mu sync.Mutex

	data []byte

	// length is len(data), which only grows, for reading without mu.
	length atomic.Int64

	// max is [[ArrayBufferMaxByteLength]], or -1 for a block whose length is fixed.
	max int

	// waiterLists are the waiter lists by byte index. A waiter is held by its agent: by the
	// suspended call of a synchronous wait, and by the agent of an asynchronous one.
	waiterLists waiterlist.Lists[waiterData]
}

func (blk *SharedDataBlock) byteLength() int {
	return int(blk.length.Load())
}

// CopyBufferBytes copies n bytes from src at srcOffset to dst at dstOffset, as copy does when the
// two have the same data block. Both ranges must be within the buffers, which are not detached.
func CopyBufferBytes(dst *ArrayBuffer, dstOffset int, src *ArrayBuffer, srcOffset, n int) {
	if dst.block == nil && src.block == nil {
		copy(dst.data[dstOffset:dstOffset+n], src.data[srcOffset:srcOffset+n])
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

// checkBufferLength throws a RangeError when a data block of n bytes cannot be created.
func (cx *ExecutionContext) checkBufferLength(n int64) {
	if n > MaxBufferLength {
		cx.ThrowRangeError("array buffer allocation failed")
	}
}

// NewArrayBufferObject implements AllocateArrayBuffer and AllocateSharedArrayBuffer:
// it returns a buffer object of byteLength zero bytes with the prototype proto.
// maxByteLength is -1 for a buffer whose length is fixed. It throws a RangeError
// when the lengths are too large.
func (cx *ExecutionContext) NewArrayBufferObject(proto *Object, byteLength, maxByteLength int64, shared bool) *Object {
	cx.checkBufferLength(byteLength)
	cx.checkBufferLength(maxByteLength)
	if shared {
		blk := &SharedDataBlock{
			data: make([]byte, byteLength),
			max:  int(maxByteLength),
		}
		blk.length.Store(byteLength)
		return cx.NewSharedArrayBufferObject(proto, blk)
	}
	return cx.newArrayBufferFromBytes(proto, make([]byte, byteLength), int(maxByteLength))
}

// NewSharedArrayBufferObject returns a SharedArrayBuffer with the prototype proto over the data
// block blk, which may come from another agent.
func (cx *ExecutionContext) NewSharedArrayBufferObject(proto *Object, blk *SharedDataBlock) *Object {
	o := cx.NewObject(proto)
	o.class = classArrayBuffer
	o.ext = &ArrayBuffer{
		block:      blk,
		max:        blk.max,
		state:      bufferShared,
		slowLength: &blk.length,
	}
	return o
}

// newArrayBufferFromBytes returns an ArrayBuffer with the prototype proto whose data is b,
// which it keeps. maxByteLength is -1 for a buffer whose length is fixed and must not be
// smaller than len(b) otherwise.
func (cx *ExecutionContext) newArrayBufferFromBytes(proto *Object, b []byte, maxByteLength int) *Object {
	w := &arrayBufferObject{
		state: ArrayBuffer{
			data: b,
			max:  maxByteLength,
		},
	}
	w.kind = kindObject
	w.shape = cx.shapeFor(proto)
	o := w.init()
	o.class = classArrayBuffer
	o.setExtState(&w.state)
	return o
}

// arrayBufferObject is an ArrayBuffer object together with its state, so that creating one is
// a single allocation.
type arrayBufferObject struct {
	propertyObject
	state ArrayBuffer
}

// ArrayBufferData returns the [[ArrayBufferData]] of v, or nil when v is neither an ArrayBuffer
// nor a SharedArrayBuffer object.
func ArrayBufferData(v Value) *ArrayBuffer {
	if v.IsObject() && v.o.class == classArrayBuffer {
		return v.o.extState().(*ArrayBuffer)
	}
	return nil
}

// NewImmutableArrayBufferFromBytes returns an immutable ArrayBuffer with the prototype proto
// whose data is b, which it keeps. b must not be modified afterwards.
func (cx *ExecutionContext) NewImmutableArrayBufferFromBytes(proto *Object, b []byte) *Object {
	o := cx.newArrayBufferFromBytes(proto, b, -1)
	o.extState().(*ArrayBuffer).immutable = true
	return o
}

// NewHostArrayBufferObject returns an ArrayBuffer of fixed length with the prototype proto whose
// data block is b, which it keeps. Its [[ArrayBufferDetachKey]] is set, so that only the host
// detaches it. len(b) must not exceed [MaxBufferLength].
func (cx *ExecutionContext) NewHostArrayBufferObject(proto *Object, b []byte) *Object {
	// The capacity is cut, so that no operation reaches the bytes of the host beyond len(b).
	o := cx.newArrayBufferFromBytes(proto, b[:len(b):len(b)], -1)
	o.extState().(*ArrayBuffer).hasDetachKey = true
	return o
}

// Detach implements the steps of DetachArrayBuffer that follow its checks: b has no data block
// afterwards. b must be neither shared nor immutable. Detaching a detached b does nothing.
func (b *ArrayBuffer) Detach() {
	b.data = nil
	b.state = bufferDetached
	b.slowLength = detachedLength
}

// TransferArrayBuffer implements the end of ArrayBufferCopyAndDetach: it returns a new ArrayBuffer
// with the prototype proto that has the first newByteLength bytes of the buffer object o,
// zero-filled beyond its length, and detaches o. maxByteLength is -1 for a buffer whose length is fixed.
// The new buffer is immutable when immutable is set. o must be mutable and have no detach key.
// It throws a RangeError when the lengths are too large.
func (cx *ExecutionContext) TransferArrayBuffer(o *Object, proto *Object, newByteLength, maxByteLength int64, immutable bool) *Object {
	cx.checkBufferLength(newByteLength)
	cx.checkBufferLength(maxByteLength)
	b := o.extState().(*ArrayBuffer)
	n := int(newByteLength)
	var data []byte
	if old := len(b.data); n <= cap(b.data) && n >= cap(b.data)/2 {
		// The data block moves to the new buffer.
		data = b.data[:n]
		if n > old {
			clear(data[old:])
		}
	} else {
		data = make([]byte, n)
		copy(data, b.data)
	}
	b.Detach()
	r := cx.newArrayBufferFromBytes(proto, data, int(maxByteLength))
	r.extState().(*ArrayBuffer).immutable = immutable
	return r
}

// ElementBits returns the encoding of the numeric value v as an element of the type t.
// v must be a BigInt for a BigInt type and a number otherwise.
func ElementBits(t element.Type, v Value) uint64 {
	if t.IsBigInt() {
		if i, ok := v.BigInt64(); ok {
			return uint64(i)
		}
		return element.EncodeBigInt(v.BigInt())
	}
	return element.EncodeNumber(t, v.n)
}

// ElementFromBits returns the value that the encoding x of an element of the type t stands for.
func ElementFromBits(t element.Type, x uint64) Value {
	switch t {
	case element.BigInt64:
		return BigIntFromInt64(int64(x))
	case element.BigUint64:
		if x <= math.MaxInt64 {
			return BigIntFromInt64(int64(x))
		}
		return NewBigInt(new(big.Int).SetUint64(x))
	}
	return Number(element.DecodeNumber(t, x))
}

// GetValueFromBuffer implements GetValueFromBuffer for the bytes b, which hold at least one element of the type t.
func (ag *agent) GetValueFromBuffer(b []byte, t element.Type, littleEndian bool) Value {
	return ElementFromBits(t, element.LoadEndian(b, t, littleEndian))
}

// SetValueInBuffer implements SetValueInBuffer for the bytes b, which hold at least one element of the type t.
// v must be a BigInt for a BigInt type and a number otherwise.
func SetValueInBuffer(b []byte, t element.Type, v Value, littleEndian bool) {
	element.StoreEndian(b, t, ElementBits(t, v), littleEndian)
}

// ToElementValue converts v as TypedArraySetElement does for an element of the type t:
// by ToBigInt for a BigInt type and by ToNumber otherwise.
func (cx *ExecutionContext) ToElementValue(t element.Type, v Value) Value {
	if t.IsBigInt() {
		return cx.ToBigInt(v)
	}
	if v.o == numberObject {
		return v
	}
	return Number(cx.ToNumber(v))
}

// TypedArray is the state of a typed array object.
type TypedArray struct {
	// buffer is [[ViewedArrayBuffer]] and data is its data block.
	buffer *Object
	data   *ArrayBuffer

	offset int

	// length is [[ArrayLength]] in elements, unless tracking is set: then the length follows the buffer.
	length   int
	tracking bool

	typ element.Type

	// numberStore reports whether the elements are numbers and the buffer is mutable and not
	// shared, so that a number is stored without a conversion.
	numberStore bool
}

// NewTypedArrayObject returns a typed array with the prototype proto whose elements of the type t
// are stored in the buffer object buffer from byteOffset on. length is the number of elements, or -1
// for a typed array whose length tracks the length of the buffer.
func (cx *ExecutionContext) NewTypedArrayObject(proto *Object, t element.Type, buffer *Object, byteOffset, length int) *Object {
	data := buffer.extState().(*ArrayBuffer)
	w := &typedArrayObject{
		state: TypedArray{
			buffer:      buffer,
			data:        data,
			offset:      byteOffset,
			length:      max(length, 0),
			tracking:    length < 0,
			typ:         t,
			numberStore: !t.IsBigInt() && !data.immutable && data.block == nil,
		},
	}
	w.kind = kindObject
	w.shape = cx.rootShape(proto, cx.realm, typedArrayRoot)
	o := w.init()
	o.class = classTypedArray
	o.flags |= objExotic
	o.setExtState(&w.state)
	return o
}

// typedArrayObject is a typed array object together with its state, so that creating one is
// a single allocation.
type typedArrayObject struct {
	propertyObject
	state TypedArray
}

// TypedArrayData returns the state of the typed array v, or nil when v is not a typed array.
func TypedArrayData(v Value) *TypedArray {
	if v.IsObject() && v.o.class == classTypedArray {
		return v.o.extState().(*TypedArray)
	}
	return nil
}

// ElementType returns [[TypedArrayName]] of t as an element type.
func (t *TypedArray) ElementType() element.Type {
	return t.typ
}

// Buffer returns [[ViewedArrayBuffer]] of t.
func (t *TypedArray) Buffer() *Object {
	return t.buffer
}

// ArrayBuffer returns the data block of [[ViewedArrayBuffer]] of t.
func (t *TypedArray) ArrayBuffer() *ArrayBuffer {
	return t.data
}

// ByteOffset returns [[ByteOffset]] of t.
func (t *TypedArray) ByteOffset() int {
	return t.offset
}

// IsLengthTracking reports whether [[ArrayLength]] of t is auto.
func (t *TypedArray) IsLengthTracking() bool {
	return t.tracking
}

// Length implements TypedArrayLength. It reports false when the typed array is out of bounds,
// which includes a detached buffer.
func (t *TypedArray) Length() (int, bool) {
	n := len(t.data.data)
	if t.data.state != bufferPlain {
		n = int(t.data.slowLength.Load())
	}
	return t.lengthOf(n)
}

// lengthOf implements TypedArrayLength given the buffer byte length, or -1 for a detached buffer.
func (t *TypedArray) lengthOf(n int) (int, bool) {
	if t.tracking {
		if t.offset > n {
			return 0, false
		}
		return (n - t.offset) / t.typ.Size(), true
	}
	if t.offset+t.length*t.typ.Size() > n {
		return 0, false
	}
	return t.length, true
}

// isTypedArrayFixedLength implements IsTypedArrayFixedLength.
func (t *TypedArray) isTypedArrayFixedLength() bool {
	return !t.tracking && (t.data.block != nil || !t.data.IsResizable())
}

// Lock returns the bytes of the elements of t, or nil when t is out of bounds, as
// [ArrayBuffer.Lock] does for the buffer. It must be paired with [TypedArray.Unlock]. The slice
// must not be modified when the buffer is immutable.
func (t *TypedArray) Lock() []byte {
	b := t.data.Lock()
	if t.data.state == bufferDetached {
		return nil
	}
	n, ok := t.lengthOf(len(b))
	if !ok {
		return nil
	}
	return b[t.offset : t.offset+n*t.typ.Size()]
}

// Unlock ends the access of [TypedArray.Lock].
func (t *TypedArray) Unlock() {
	t.data.Unlock()
}

// LoadElement returns the encoding of the element i of t, or reports false when i is not a valid
// integer index.
func (t *TypedArray) LoadElement(i int) (uint64, bool) {
	b := t.Lock()
	defer t.Unlock()
	size := t.typ.Size()
	if i < 0 || i >= len(b)/size {
		return 0, false
	}
	return element.Load(b[i*size:], t.typ), true
}

// StoreElement stores the encoding x as the element i of t, which is mutable, when i is a valid
// integer index.
func (t *TypedArray) StoreElement(i int, x uint64) {
	b := t.Lock()
	defer t.Unlock()
	size := t.typ.Size()
	if i >= 0 && i < len(b)/size {
		element.Store(b[i*size:], t.typ, x)
	}
}

// element returns the bytes of the element i of t, or reports false when i is not a valid integer index.
func (t *TypedArray) element(i uint32) ([]byte, bool) {
	size := t.typ.Size()
	data := t.data.data
	var n int
	if t.tracking {
		n = (len(data) - t.offset) / size
	} else if t.offset+t.length*size <= len(data) {
		n = t.length
	}
	if n <= 0 || uint(i) >= uint(n) {
		return nil, false
	}
	p := t.offset + int(i)*size
	return data[p : p+size], true
}

// loadElement returns the element of the type t at the start of b.
func loadElement(t element.Type, b []byte) Value {
	switch t {
	case element.Int8:
		return Number(float64(int8(b[0])))
	case element.Uint8, element.Uint8Clamped:
		return Number(float64(b[0]))
	case element.Int16:
		return Number(float64(int16(binary.LittleEndian.Uint16(b))))
	case element.Uint16:
		return Number(float64(binary.LittleEndian.Uint16(b)))
	case element.Int32:
		return Number(float64(int32(binary.LittleEndian.Uint32(b))))
	case element.Uint32:
		return Number(float64(binary.LittleEndian.Uint32(b)))
	case element.Float16:
		return Number(numconv.Float16FromBits(binary.LittleEndian.Uint16(b)))
	case element.Float32:
		return Number(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
	case element.Float64:
		return Number(math.Float64frombits(binary.LittleEndian.Uint64(b)))
	}
	return ElementFromBits(t, binary.LittleEndian.Uint64(b))
}

// typedArrayGet implements TypedArrayGetElement. It returns undefined when i is not a valid integer index.
func (ag *agent) typedArrayGet(o *Object, i uint32) Value {
	t := o.extState().(*TypedArray)
	if data := t.data.data; !t.tracking && uint(i) < uint(t.length) {
		switch t.typ {
		case element.Int32:
			if p := t.offset + int(i)*4; t.offset+t.length*4 <= len(data) {
				return Number(float64(int32(binary.LittleEndian.Uint32(data[p:]))))
			}
		case element.Float64:
			if p := t.offset + int(i)*8; t.offset+t.length*8 <= len(data) {
				return Number(math.Float64frombits(binary.LittleEndian.Uint64(data[p:])))
			}
		case element.Uint8, element.Uint8Clamped:
			if p := t.offset + int(i); t.offset+t.length <= len(data) {
				return Number(float64(data[p]))
			}
		}
	}
	b, ok := t.element(i)
	if !ok {
		return ag.typedArrayGetSlow(o, i)
	}
	return loadElement(t.typ, b)
}

// typedArrayGetSlow implements TypedArrayGetElement for the typed array o when the bytes of the
// element i are not in the data of its buffer: when the buffer is shared or i is not a valid
// integer index.
func (ag *agent) typedArrayGetSlow(o *Object, i uint32) Value {
	t := o.extState().(*TypedArray)
	if t.data.block == nil {
		return Undefined
	}
	x, ok := t.LoadElement(int(i))
	if !ok {
		return Undefined
	}
	return ElementFromBits(t.typ, x)
}

// typedArraySet implements [[Set]] of the typed array o for an array index and the receiver o.
// It reports false when the buffer is immutable.
func (cx *ExecutionContext) typedArraySet(o *Object, i uint32, val Value) bool {
	t := o.extState().(*TypedArray)
	if val.o == numberObject && t.numberStore {
		if data, f := t.data.data, val.n; !t.tracking && uint(i) < uint(t.length) {
			switch t.typ {
			case element.Int32:
				if p := t.offset + int(i)*4; t.offset+t.length*4 <= len(data) && f >= -2147483648 && f <= 2147483647 {
					binary.LittleEndian.PutUint32(data[p:], uint32(int32(f)))
					return true
				}
			case element.Float64:
				if p := t.offset + int(i)*8; t.offset+t.length*8 <= len(data) {
					binary.LittleEndian.PutUint64(data[p:], math.Float64bits(f))
					return true
				}
			case element.Uint8:
				if p := t.offset + int(i); t.offset+t.length <= len(data) && f >= -2147483648 && f <= 2147483647 {
					data[p] = byte(int32(f))
					return true
				}
			}
		}
		if b, ok := t.element(i); ok {
			element.StoreNumber(b, t.typ, val.n)
		}
		return true
	}
	if t.data.immutable {
		return false
	}
	cx.typedArraySetElement(t, i, val)
	return true
}

// typedArraySetElement implements TypedArraySetElement for the typed array t on a mutable buffer.
func (cx *ExecutionContext) typedArraySetElement(t *TypedArray, i uint32, val Value) {
	val = cx.ToElementValue(t.typ, val)
	if t.data.block != nil {
		t.StoreElement(int(i), ElementBits(t.typ, val))
		return
	}
	// The conversion may have resized or detached the buffer.
	if b, ok := t.element(i); ok {
		element.Store(b, t.typ, ElementBits(t.typ, val))
	}
}

// typedArrayImmutable reports whether the buffer of the typed array o is immutable.
func typedArrayImmutable(o *Object) bool {
	return o.extState().(*TypedArray).data.immutable
}

// typedArrayFlags returns the attributes of the elements of the typed array o.
func typedArrayFlags(o *Object) uint8 {
	if typedArrayImmutable(o) {
		return FlagEnumerable
	}
	return FlagsData
}

// typedArrayHas implements IsValidIntegerIndex for an array index.
func (ag *agent) typedArrayHas(o *Object, i uint32) bool {
	t := o.extState().(*TypedArray)
	if _, ok := t.element(i); ok {
		return true
	}
	if t.data.block != nil {
		n, ok := t.Length()
		return ok && uint(i) < uint(n)
	}
	return false
}

// typedArrayLength returns the length of o, or 0 when o is out of bounds.
func (ag *agent) typedArrayLength(o *Object) int {
	n, _ := o.extState().(*TypedArray).Length()
	return n
}

// typedArrayDefine implements [[DefineOwnProperty]] of a typed array for an array index.
func (cx *ExecutionContext) typedArrayDefine(o *Object, i uint32, d PropDesc) bool {
	if !cx.typedArrayHas(o, i) {
		return false
	}
	if typedArrayImmutable(o) {
		// ValidateAndApplyPropertyDescriptor against a non-configurable, non-writable element.
		if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable != 0 {
			return false
		}
		if d.has&HasEnumerable != 0 && d.flags&FlagEnumerable == 0 {
			return false
		}
		if d.IsAccessor() {
			return false
		}
		if d.has&HasWritable != 0 && d.flags&FlagWritable != 0 {
			return false
		}
		return d.has&HasValue == 0 || SameValue(d.value, cx.typedArrayGet(o, i))
	}
	if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable == 0 {
		return false
	}
	if d.has&HasEnumerable != 0 && d.flags&FlagEnumerable == 0 {
		return false
	}
	if d.IsAccessor() {
		return false
	}
	if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		return false
	}
	if d.has&HasValue != 0 {
		cx.typedArraySetElement(o.extState().(*TypedArray), i, d.value)
	}
	return true
}

// typedArraySetNamed implements [[Set]] of the typed array o for a canonical numeric string that is
// not an array index, which is never a valid integer index.
func (cx *ExecutionContext) typedArraySetNamed(o *Object, val Value, receiver Value) {
	if receiver.o == o {
		cx.ToElementValue(o.extState().(*TypedArray).typ, val)
	}
}

// maxNumberStringLen bounds the length of the string of a number, such as
// "-1.2345678901234567e-308".
const maxNumberStringLen = 32

// isCanonicalNumericString reports whether CanonicalNumericIndexString returns a number for the string key.
// The array indices are not named keys, so they are not considered.
func isCanonicalNumericString(key *Object) bool {
	if key.kind != kindString {
		return false
	}
	s := key.str
	if len(s) == 0 {
		return false
	}
	if c := s[0]; c != '-' && c != 'I' && c != 'N' && (c < '0' || c > '9') {
		return false
	}
	if s == "-0" {
		return true
	}
	// The string of a number is short.
	if len(s) > maxNumberStringLen {
		return false
	}
	return numconv.NumberToString(numconv.StringToNumber(s, nil)) == s
}

// isNumericKey reports whether k is an array index or a canonical numeric string,
// which a typed array handles without looking at its properties.
func isNumericKey(k PropKey) bool {
	return k.key == nil || isCanonicalNumericString(k.key)
}

// NewDataViewObject returns a DataView with the prototype proto on the buffer object buffer
// from byteOffset on. byteLength is -1 for a view whose length tracks the length of the buffer.
func (cx *ExecutionContext) NewDataViewObject(proto *Object, buffer *Object, byteOffset, byteLength int) *Object {
	o := cx.NewObject(proto)
	o.class = classDataView
	o.ext = &DataView{
		buffer:   buffer,
		data:     buffer.extState().(*ArrayBuffer),
		offset:   byteOffset,
		length:   max(byteLength, 0),
		tracking: byteLength < 0,
	}
	return o
}

// DataView is the state of a DataView object.
type DataView struct {
	buffer   *Object
	data     *ArrayBuffer
	offset   int
	length   int
	tracking bool
}

// DataViewData returns the state of the DataView v, or nil when v is not a DataView.
func DataViewData(v Value) *DataView {
	if v.IsObject() && v.o.class == classDataView {
		return v.o.extState().(*DataView)
	}
	return nil
}

// Buffer returns [[ViewedArrayBuffer]] of d.
func (d *DataView) Buffer() *Object {
	return d.buffer
}

// ByteOffset returns [[ByteOffset]] of d.
func (d *DataView) ByteOffset() int {
	return d.offset
}

// ByteLength implements GetViewByteLength. It reports false when the view is out of bounds,
// which includes a detached buffer.
func (d *DataView) ByteLength() (int, bool) {
	if d.data.state == bufferDetached {
		return 0, false
	}
	return d.byteLengthOf(d.data.ByteLength())
}

// byteLengthOf implements GetViewByteLength for a buffer of n bytes that is not detached.
func (d *DataView) byteLengthOf(n int) (int, bool) {
	if d.tracking {
		if d.offset > n {
			return 0, false
		}
		return n - d.offset, true
	}
	if d.offset+d.length > n {
		return 0, false
	}
	return d.length, true
}

// Lock locks the buffer as [ArrayBuffer.Lock] does and returns the bytes that d views.
// It reports false when the buffer is detached or d is out of bounds. Always pair Lock with
// [DataView.Unlock]. The slice must not be modified when the buffer is immutable.
func (d *DataView) Lock() ([]byte, bool) {
	b := d.data.Lock()
	if d.data.state == bufferDetached {
		return nil, false
	}
	n, ok := d.byteLengthOf(len(b))
	if !ok {
		return nil, false
	}
	return b[d.offset : d.offset+n], true
}

// Unlock ends the access of [DataView.Lock].
func (d *DataView) Unlock() {
	d.data.Unlock()
}
