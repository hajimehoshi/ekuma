// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"encoding/binary"
	"math"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// This file holds the operations of ArrayBuffer, SharedArrayBuffer and typed array objects that throw
// or convert values.

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
		return cx.NewSharedArrayBufferObject(proto, storage.NewSharedDataBlock(byteLength, maxByteLength))
	}
	return cx.NewArrayBufferFromBytes(proto, make([]byte, byteLength), int(maxByteLength))
}

// TransferArrayBuffer implements the end of ArrayBufferCopyAndDetach: it returns a new ArrayBuffer
// with the prototype proto that has the first newByteLength bytes of the buffer object o,
// zero-filled beyond its length, and detaches o. maxByteLength is -1 for a buffer whose length is fixed.
// The new buffer is immutable when immutable is set. o must be mutable and have no detach key.
// It throws a RangeError when the lengths are too large.
func (cx *ExecutionContext) TransferArrayBuffer(o *Object, proto *Object, newByteLength, maxByteLength int64, immutable bool) *Object {
	cx.checkBufferLength(newByteLength)
	cx.checkBufferLength(maxByteLength)
	b := o.ExtState().(*storage.ArrayBuffer)
	n := int(newByteLength)
	var data []byte
	if old := len(b.Data); n <= cap(b.Data) && n >= cap(b.Data)/2 {
		// The data block moves to the new buffer.
		data = b.Data[:n]
		if n > old {
			clear(data[old:])
		}
	} else {
		data = make([]byte, n)
		copy(data, b.Data)
	}
	b.Detach()
	r := cx.NewArrayBufferFromBytes(proto, data, int(maxByteLength))
	r.ExtState().(*storage.ArrayBuffer).Immutable = immutable
	return r
}

// ToElementValue converts v as TypedArraySetElement does for an element of the type t:
// by ToBigInt for a BigInt type and by ToNumber otherwise.
func (cx *ExecutionContext) ToElementValue(t element.Type, v Value) Value {
	if t.IsBigInt() {
		return cx.ToBigInt(v)
	}
	if v.O == storage.NumberObject {
		return v
	}
	return storage.Number(cx.ToNumber(v))
}

// typedArraySet implements [[Set]] of the typed array o for an array index and the receiver o.
// It reports false when the buffer is immutable.
func (cx *ExecutionContext) typedArraySet(o *Object, i uint32, val Value) bool {
	t := o.ExtState().(*storage.TypedArray)
	if val.O == storage.NumberObject && t.NumberStore {
		if data, f := t.Data.Data, val.N; !t.Tracking && uint(i) < uint(t.FixedLength) {
			switch t.Type {
			case element.Int32:
				if p := t.Offset + int(i)*4; t.Offset+t.FixedLength*4 <= len(data) && f >= -2147483648 && f <= 2147483647 {
					binary.LittleEndian.PutUint32(data[p:], uint32(int32(f)))
					return true
				}
			case element.Float64:
				if p := t.Offset + int(i)*8; t.Offset+t.FixedLength*8 <= len(data) {
					binary.LittleEndian.PutUint64(data[p:], math.Float64bits(f))
					return true
				}
			case element.Uint8:
				if p := t.Offset + int(i); t.Offset+t.FixedLength <= len(data) && f >= -2147483648 && f <= 2147483647 {
					data[p] = byte(int32(f))
					return true
				}
			}
		}
		if b, ok := t.ElementBytes(i); ok {
			element.StoreNumber(b, t.Type, val.N)
		}
		return true
	}
	if t.Data.Immutable {
		return false
	}
	cx.typedArraySetElement(t, i, val)
	return true
}

// typedArraySetElement implements TypedArraySetElement for the typed array t on a mutable buffer.
func (cx *ExecutionContext) typedArraySetElement(t *storage.TypedArray, i uint32, val Value) {
	val = cx.ToElementValue(t.Type, val)
	if t.Data.IsShared() {
		t.StoreElement(int(i), storage.ElementBits(t.Type, val))
		return
	}
	// The conversion may have resized or detached the buffer.
	if b, ok := t.ElementBytes(i); ok {
		element.Store(b, t.Type, storage.ElementBits(t.Type, val))
	}
}

// typedArrayDefine implements [[DefineOwnProperty]] of a typed array for an array index.
func (cx *ExecutionContext) typedArrayDefine(o *Object, i uint32, d PropDesc) bool {
	if !storage.TypedArrayHas(o, i) {
		return false
	}
	if storage.TypedArrayImmutable(o) {
		// ValidateAndApplyPropertyDescriptor against a non-configurable, non-writable element.
		if d.HasTrue(HasConfigurable, FlagConfigurable) {
			return false
		}
		if d.HasFalse(HasEnumerable, FlagEnumerable) {
			return false
		}
		if d.IsAccessor() {
			return false
		}
		if d.HasTrue(HasWritable, FlagWritable) {
			return false
		}
		return !d.HasField(HasValue) || storage.SameValue(d.Value(), storage.TypedArrayGet(o, i))
	}
	if d.HasFalse(HasConfigurable, FlagConfigurable) {
		return false
	}
	if d.HasFalse(HasEnumerable, FlagEnumerable) {
		return false
	}
	if d.IsAccessor() {
		return false
	}
	if d.HasFalse(HasWritable, FlagWritable) {
		return false
	}
	if d.HasField(HasValue) {
		cx.typedArraySetElement(o.ExtState().(*storage.TypedArray), i, d.Value())
	}
	return true
}

// typedArraySetNamed implements [[Set]] of the typed array o for a canonical numeric string that is
// not an array index, which is never a valid integer index.
func (cx *ExecutionContext) typedArraySetNamed(o *Object, val Value, receiver Value) {
	if receiver.O == o {
		cx.ToElementValue(o.ExtState().(*storage.TypedArray).Type, val)
	}
}

// isNumericKey reports whether k is an array index or a canonical numeric string,
// which a typed array handles without looking at its properties.
func isNumericKey(k PropKey) bool {
	return k.Key == nil || storage.IsCanonicalNumericString(k.Key)
}
