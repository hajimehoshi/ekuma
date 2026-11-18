// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package shortstrings operates on packed canonical WTF-8 strings.
package shortstrings

import (
	"encoding/binary"
	"math/bits"
)

// MaxBytes is the maximum byte length of a packed string.
const MaxBytes = 8

// Pack returns the zero-padded payload of s, which must contain at most MaxBytes bytes.
func Pack(s string) uint64 {
	var data [MaxBytes]byte
	copy(data[:], s)
	return binary.LittleEndian.Uint64(data[:])
}

// Bytes returns the bytes of a packed string, including its zero padding.
func Bytes(data uint64) [MaxBytes]byte {
	var b [MaxBytes]byte
	binary.LittleEndian.PutUint64(b[:], data)
	return b
}

// String returns the packed string whose byte length is n.
func String(data uint64, n int) string {
	// Keep the conversion separate from the byte fill so callers can inline it on wasm.
	b := Bytes(data)
	return string(b[:n])
}

// IsASCII reports whether every byte of the packed string is below 0x80.
func IsASCII(data uint64) bool {
	return data&0x8080808080808080 == 0
}

// Len returns the UTF-16 length of the packed string whose byte length is n.
func Len(data uint64, n int) int {
	if IsASCII(data) {
		return n
	}
	return lenSlow(data, n)
}

func lenSlow(data uint64, n int) int {
	// Continuation bytes add no code units; four-byte sequences add a second unit.
	// Zero padding contributes to neither mask.
	continuations := (data >> 7) &^ (data >> 6) & 0x0101010101010101
	leads := data & (data >> 1) & (data >> 2) & (data >> 3) & 0x1010101010101010
	return n - bits.OnesCount64(continuations) + bits.OnesCount64(leads)
}

// UnitAt returns the UTF-16 code unit at i in the packed string. i must be in range.
func UnitAt(data uint64, i int) uint16 {
	if IsASCII(data) {
		return uint16(byte(data >> (8 * i)))
	}
	return unitAtSlow(data, i)
}

func unitAtSlow(data uint64, i int) uint16 {
	for {
		b := byte(data)
		switch {
		case b < 0x80:
			if i == 0 {
				return uint16(b)
			}
			data >>= 8
		case b < 0xe0:
			if i == 0 {
				return uint16(b&0x1f)<<6 | uint16(data>>8&0x3f)
			}
			data >>= 16
		case b < 0xf0:
			if i == 0 {
				return uint16(b&0x0f)<<12 | uint16(data>>8&0x3f)<<6 | uint16(data>>16&0x3f)
			}
			data >>= 24
		default:
			if i < 2 {
				r := uint32(b&7)<<18 | uint32(data>>8&0x3f)<<12 | uint32(data>>16&0x3f)<<6 | uint32(data>>24&0x3f)
				r -= 0x10000
				if i == 0 {
					return 0xd800 + uint16(r>>10)
				}
				return 0xdc00 + uint16(r&0x3ff)
			}
			data >>= 32
			i--
		}
		i--
	}
}
