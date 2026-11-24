// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package element provides the element types of typed arrays and the encodings of their elements.
package element

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// Type is the element type of a typed array.
type Type uint8

const (
	Int8 Type = iota
	Uint8
	Uint8Clamped
	Int16
	Uint16
	Int32
	Uint32
	Float16
	Float32
	Float64
	BigInt64
	BigUint64

	// TypeCount is the number of element types.
	TypeCount
)

var sizes = [TypeCount]uint8{1, 1, 1, 2, 2, 4, 4, 2, 4, 8, 8, 8}

// Size returns the element size of t in bytes.
func (t Type) Size() int {
	return int(sizes[t])
}

// IsBigInt reports whether the elements of t are BigInts.
func (t Type) IsBigInt() bool {
	return t >= BigInt64
}

// IsInteger reports whether t is an integer type that is not Uint8Clamped, as the Atomics functions require.
func (t Type) IsInteger() bool {
	return t != Uint8Clamped && t <= Uint32 || t.IsBigInt()
}

// Load returns the encoding of the element of the type t at the start of b in little-endian byte order.
func Load(b []byte, t Type) uint64 {
	return load(b, t.Size(), true)
}

// Store stores the encoding x of an element of the type t at the start of b in little-endian byte order.
func Store(b []byte, t Type, x uint64) {
	store(b, t.Size(), x, true)
}

// LoadEndian returns the encoding of the element of the type t at the start of b in the given byte order.
func LoadEndian(b []byte, t Type, littleEndian bool) uint64 {
	return load(b, t.Size(), littleEndian)
}

// StoreEndian stores the encoding x of an element of the type t at the start of b in the given byte order.
func StoreEndian(b []byte, t Type, x uint64, littleEndian bool) {
	store(b, t.Size(), x, littleEndian)
}

// load returns the n bytes at the start of b as an unsigned integer in the given byte order.
func load(b []byte, n int, littleEndian bool) uint64 {
	if littleEndian {
		switch n {
		case 1:
			return uint64(b[0])
		case 2:
			return uint64(binary.LittleEndian.Uint16(b))
		case 4:
			return uint64(binary.LittleEndian.Uint32(b))
		}
		return binary.LittleEndian.Uint64(b)
	}
	switch n {
	case 1:
		return uint64(b[0])
	case 2:
		return uint64(binary.BigEndian.Uint16(b))
	case 4:
		return uint64(binary.BigEndian.Uint32(b))
	}
	return binary.BigEndian.Uint64(b)
}

// store stores the low n bytes of x at the start of b in the given byte order.
func store(b []byte, n int, x uint64, littleEndian bool) {
	if littleEndian {
		switch n {
		case 1:
			b[0] = byte(x)
		case 2:
			binary.LittleEndian.PutUint16(b, uint16(x))
		case 4:
			binary.LittleEndian.PutUint32(b, uint32(x))
		default:
			binary.LittleEndian.PutUint64(b, x)
		}
		return
	}
	switch n {
	case 1:
		b[0] = byte(x)
	case 2:
		binary.BigEndian.PutUint16(b, uint16(x))
	case 4:
		binary.BigEndian.PutUint32(b, uint32(x))
	default:
		binary.BigEndian.PutUint64(b, x)
	}
}

// EncodeNumber returns the encoding of the number f as an element of the type t, which is not a BigInt type.
func EncodeNumber(t Type, f float64) uint64 {
	switch t {
	case Int8, Uint8, Int16, Uint16, Int32, Uint32:
		if f >= -2147483648 && f <= 2147483647 {
			return uint64(uint32(int32(f)))
		}
		return uint64(numconv.ToUint32(f))
	case Uint8Clamped:
		return uint64(numconv.ToUint8Clamp(f))
	case Float16:
		return uint64(numconv.Float16Bits(f))
	case Float32:
		return uint64(math.Float32bits(float32(f)))
	}
	return math.Float64bits(f)
}

// DecodeNumber returns the number that the encoding x of an element of the type t,
// which is not a BigInt type, stands for.
func DecodeNumber(t Type, x uint64) float64 {
	switch t {
	case Int8:
		return float64(int8(x))
	case Uint8, Uint8Clamped:
		return float64(uint8(x))
	case Int16:
		return float64(int16(x))
	case Uint16:
		return float64(uint16(x))
	case Int32:
		return float64(int32(x))
	case Uint32:
		return float64(uint32(x))
	case Float16:
		return numconv.Float16FromBits(uint16(x))
	case Float32:
		return float64(math.Float32frombits(uint32(x)))
	}
	return math.Float64frombits(x)
}

var uint64Mask = new(big.Int).SetUint64(math.MaxUint64)

// EncodeBigInt returns i modulo 2^64, the encoding of i as an element of a BigInt type.
func EncodeBigInt(i *big.Int) uint64 {
	if i.IsInt64() {
		return uint64(i.Int64())
	}
	if i.IsUint64() {
		return i.Uint64()
	}
	// And works on the two's complement of a negative number.
	return new(big.Int).And(i, uint64Mask).Uint64()
}

// Convert returns the encoding x of an element of the type from as an element of the type to,
// as reading the element and storing the value would. Both types must be BigInt types or neither.
func Convert(from, to Type, x uint64) uint64 {
	if from == to || from.IsBigInt() {
		// BigInt64 and BigUint64 have the same encoding modulo 2^64.
		return x
	}
	return EncodeNumber(to, DecodeNumber(from, x))
}

// Equal reports whether the encodings x and y are equal in the size of an element of the type t.
func Equal(t Type, x, y uint64) bool {
	mask := uint64(math.MaxUint64) >> (64 - 8*t.Size())
	return x&mask == y&mask
}

// StoreNumber stores the number f as an element of the type t, which is not a BigInt type,
// at the start of b in little-endian byte order.
func StoreNumber(b []byte, t Type, f float64) {
	switch t {
	case Int8, Uint8:
		if f >= -2147483648 && f <= 2147483647 {
			b[0] = byte(int32(f))
			return
		}
		b[0] = byte(numconv.ToUint32(f))
	case Uint8Clamped:
		b[0] = numconv.ToUint8Clamp(f)
	case Int16, Uint16:
		if f >= -2147483648 && f <= 2147483647 {
			binary.LittleEndian.PutUint16(b, uint16(int32(f)))
			return
		}
		binary.LittleEndian.PutUint16(b, uint16(numconv.ToUint32(f)))
	case Int32, Uint32:
		if f >= -2147483648 && f <= 2147483647 {
			binary.LittleEndian.PutUint32(b, uint32(int32(f)))
			return
		}
		binary.LittleEndian.PutUint32(b, numconv.ToUint32(f))
	case Float32:
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(f)))
	case Float64:
		binary.LittleEndian.PutUint64(b, math.Float64bits(f))
	default:
		Store(b, t, EncodeNumber(t, f))
	}
}
