// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// This file holds the encoding of elements in the bytes of buffers.
// Elements are stored in little-endian byte order on every platform.

// ElementBits returns the encoding of the numeric value v as an element of the type t.
// v must be a BigInt for a BigInt type and a number otherwise.
func ElementBits(t element.Type, v Value) uint64 {
	if t.IsBigInt() {
		if i, ok := v.BigInt64(); ok {
			return uint64(i)
		}
		return element.EncodeBigInt(v.BigInt())
	}
	return element.EncodeNumber(t, v.N)
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
func GetValueFromBuffer(b []byte, t element.Type, littleEndian bool) Value {
	return ElementFromBits(t, element.LoadEndian(b, t, littleEndian))
}

// SetValueInBuffer implements SetValueInBuffer for the bytes b, which hold at least one element of the type t.
// v must be a BigInt for a BigInt type and a number otherwise.
func SetValueInBuffer(b []byte, t element.Type, v Value, littleEndian bool) {
	element.StoreEndian(b, t, ElementBits(t, v), littleEndian)
}

// ElementFromBytes returns the element of the type t at the start of b.
func ElementFromBytes(t element.Type, b []byte) Value {
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
