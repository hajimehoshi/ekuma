// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"encoding/binary"
	"math"

	"github.com/hajimehoshi/ekuma/internal/element"
)

// TypedArray is the state of a typed array object.
type TypedArray struct {
	// buffer is [[ViewedArrayBuffer]] and Data is its data block.
	buffer *Object
	Data   *ArrayBuffer

	Offset int

	// FixedLength is [[ArrayLength]] in elements, unless Tracking is set: then the length follows
	// the buffer.
	FixedLength int
	Tracking    bool

	Type element.Type

	// NumberStore reports whether the elements are numbers and the buffer is mutable and not
	// shared, so that a number is stored without a conversion.
	NumberStore bool
}

// NewTypedArrayObject returns a typed array with the prototype proto whose elements of the type t
// are stored in the buffer object buffer from byteOffset on. length is the number of elements, or -1
// for a typed array whose length tracks the length of the buffer.
func (r *RealmPart) NewTypedArrayObject(proto *Object, t element.Type, buffer *Object, byteOffset, length int) *Object {
	data := buffer.ExtState().(*ArrayBuffer)
	w := &typedArrayObject{
		state: TypedArray{
			buffer:      buffer,
			Data:        data,
			Offset:      byteOffset,
			FixedLength: max(length, 0),
			Tracking:    length < 0,
			Type:        t,
			NumberStore: !t.IsBigInt() && !data.Immutable && data.block == nil,
		},
	}
	return w.InitExotic(r.RootShape(proto, r, TypedArrayRoot), ClassTypedArray, &w.state)
}

// typedArrayObject is a typed array object together with its state, so that creating one is
// a single allocation.
type typedArrayObject struct {
	PropertyObject
	state TypedArray
}

// TypedArrayData returns the state of the typed array v, or nil when v is not a typed array.
func TypedArrayData(v Value) *TypedArray {
	if v.IsObject() && v.O.Class == ClassTypedArray {
		return v.O.ExtState().(*TypedArray)
	}
	return nil
}

// ElementType returns [[TypedArrayName]] of t as an element type.
func (t *TypedArray) ElementType() element.Type {
	return t.Type
}

// Buffer returns [[ViewedArrayBuffer]] of t.
func (t *TypedArray) Buffer() *Object {
	return t.buffer
}

// ArrayBuffer returns the data block of [[ViewedArrayBuffer]] of t.
func (t *TypedArray) ArrayBuffer() *ArrayBuffer {
	return t.Data
}

// ByteOffset returns [[ByteOffset]] of t.
func (t *TypedArray) ByteOffset() int {
	return t.Offset
}

// IsLengthTracking reports whether [[ArrayLength]] of t is auto.
func (t *TypedArray) IsLengthTracking() bool {
	return t.Tracking
}

// Length implements TypedArrayLength. It reports false when the typed array is out of bounds,
// which includes a detached buffer.
func (t *TypedArray) Length() (int, bool) {
	n := len(t.Data.Data)
	if t.Data.state != bufferPlain {
		n = int(t.Data.slowLength.Load())
	}
	return t.lengthOf(n)
}

// lengthOf implements TypedArrayLength given the buffer byte length, or -1 for a detached buffer.
func (t *TypedArray) lengthOf(n int) (int, bool) {
	if t.Tracking {
		if t.Offset > n {
			return 0, false
		}
		return (n - t.Offset) / t.Type.Size(), true
	}
	if t.Offset+t.FixedLength*t.Type.Size() > n {
		return 0, false
	}
	return t.FixedLength, true
}

// IsTypedArrayFixedLength implements IsTypedArrayFixedLength.
func (t *TypedArray) IsTypedArrayFixedLength() bool {
	return !t.Tracking && (t.Data.block != nil || !t.Data.IsResizable())
}

// Lock returns the bytes of the elements of t, or nil when t is out of bounds, as
// [ArrayBuffer.Lock] does for the buffer. It must be paired with [TypedArray.Unlock]. The slice
// must not be modified when the buffer is immutable.
func (t *TypedArray) Lock() []byte {
	b := t.Data.Lock()
	if t.Data.state == bufferDetached {
		return nil
	}
	n, ok := t.lengthOf(len(b))
	if !ok {
		return nil
	}
	return b[t.Offset : t.Offset+n*t.Type.Size()]
}

// Unlock ends the access of [TypedArray.Lock].
func (t *TypedArray) Unlock() {
	t.Data.Unlock()
}

// LoadElement returns the encoding of the element i of t, or reports false when i is not a valid
// integer index.
func (t *TypedArray) LoadElement(i int) (uint64, bool) {
	b := t.Lock()
	defer t.Unlock()
	size := t.Type.Size()
	if i < 0 || i >= len(b)/size {
		return 0, false
	}
	return element.Load(b[i*size:], t.Type), true
}

// StoreElement stores the encoding x as the element i of t, which is mutable, when i is a valid
// integer index.
func (t *TypedArray) StoreElement(i int, x uint64) {
	b := t.Lock()
	defer t.Unlock()
	size := t.Type.Size()
	if i >= 0 && i < len(b)/size {
		element.Store(b[i*size:], t.Type, x)
	}
}

// ElementBytes returns the bytes of the element i of t. It reports false when i is not a valid
// integer index or the buffer is shared.
func (t *TypedArray) ElementBytes(i uint32) ([]byte, bool) {
	size := t.Type.Size()
	data := t.Data.Data
	var n int
	if t.Tracking {
		n = (len(data) - t.Offset) / size
	} else if t.Offset+t.FixedLength*size <= len(data) {
		n = t.FixedLength
	}
	if n <= 0 || uint(i) >= uint(n) {
		return nil, false
	}
	p := t.Offset + int(i)*size
	return data[p : p+size], true
}

// TypedArrayGet implements TypedArrayGetElement. It returns undefined when i is not a valid integer index.
func TypedArrayGet(o *Object, i uint32) Value {
	t := o.ExtState().(*TypedArray)
	if data := t.Data.Data; !t.Tracking && uint(i) < uint(t.FixedLength) {
		switch t.Type {
		case element.Int32:
			if p := t.Offset + int(i)*4; t.Offset+t.FixedLength*4 <= len(data) {
				return Number(float64(int32(binary.LittleEndian.Uint32(data[p:]))))
			}
		case element.Float64:
			if p := t.Offset + int(i)*8; t.Offset+t.FixedLength*8 <= len(data) {
				return Number(math.Float64frombits(binary.LittleEndian.Uint64(data[p:])))
			}
		case element.Uint8, element.Uint8Clamped:
			if p := t.Offset + int(i); t.Offset+t.FixedLength <= len(data) {
				return Number(float64(data[p]))
			}
		}
	}
	b, ok := t.ElementBytes(i)
	if !ok {
		return typedArrayGetSlow(o, i)
	}
	return ElementFromBytes(t.Type, b)
}

// typedArrayGetSlow implements TypedArrayGetElement for the typed array o when the bytes of the
// element i are not in the data of its buffer: when the buffer is shared or i is not a valid
// integer index.
func typedArrayGetSlow(o *Object, i uint32) Value {
	t := o.ExtState().(*TypedArray)
	if t.Data.block == nil {
		return Undefined
	}
	x, ok := t.LoadElement(int(i))
	if !ok {
		return Undefined
	}
	return ElementFromBits(t.Type, x)
}

// TypedArrayImmutable reports whether the buffer of the typed array o is immutable.
func TypedArrayImmutable(o *Object) bool {
	return o.ExtState().(*TypedArray).Data.Immutable
}

// TypedArrayFlags returns the attributes of the elements of the typed array o.
func TypedArrayFlags(o *Object) uint8 {
	if TypedArrayImmutable(o) {
		return FlagEnumerable
	}
	return FlagsData
}

// TypedArrayHas implements IsValidIntegerIndex for an array index.
func TypedArrayHas(o *Object, i uint32) bool {
	t := o.ExtState().(*TypedArray)
	if _, ok := t.ElementBytes(i); ok {
		return true
	}
	if t.Data.block != nil {
		n, ok := t.Length()
		return ok && uint(i) < uint(n)
	}
	return false
}

// TypedArrayLength returns the length of o, or 0 when o is out of bounds.
func TypedArrayLength(o *Object) int {
	n, _ := o.ExtState().(*TypedArray).Length()
	return n
}
