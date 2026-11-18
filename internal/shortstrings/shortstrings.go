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

// Slice returns the packed substring and its byte length. start and end must be ordered UTF-16 indices within the string.
func Slice(data uint64, start, end int) (uint64, int) {
	if start == end {
		return 0, 0
	}
	if IsASCII(data) {
		n := end - start
		return (data >> (8 * start)) & (uint64(1)<<(8*n) - 1), n
	}
	return sliceSlow(data, start, end)
}

func sliceSlow(data uint64, start, end int) (uint64, int) {
	var result uint64
	var n, unit int
	for unit < end {
		size, units := 1, 1
		switch b := byte(data); {
		case b >= 0xf0:
			size, units = 4, 2
		case b >= 0xe0:
			size = 3
		case b >= 0xc0:
			size = 2
		}
		if unit+units > start {
			part, partSize := data, size
			if units == 2 && (unit < start || unit+units > end) {
				// A cut through a four-byte sequence leaves a three-byte lone surrogate.
				var half int
				if unit < start {
					half = 1
				}
				u := unitAtSlow(data, half)
				part = 0x8080ed | uint64(u>>6&0x3f)<<8 | uint64(u&0x3f)<<16
				partSize = 3
			}
			result |= (part & (uint64(1)<<(8*partSize) - 1)) << (8 * n)
			n += partSize
		}
		data >>= 8 * size
		unit += units
	}
	return result, n
}

// Index returns the first UTF-16 index of sub at or after from, or -1.
// n and subN are byte lengths, and from must be nonnegative.
func Index(data uint64, n int, sub uint64, subN int, from int) int {
	return index(data, n, sub, subN, from, false)
}

// LastIndex returns the last UTF-16 index of sub at or before from, or -1.
// n and subN are byte lengths, and from must be nonnegative.
func LastIndex(data uint64, n int, sub uint64, subN int, from int) int {
	return index(data, n, sub, subN, from, true)
}

func index(data uint64, n int, sub uint64, subN int, from int, backward bool) int {
	// A canonical substring cannot use more bytes than its source.
	if subN > n {
		return -1
	}
	ascii := IsASCII(data)
	length, subLen := n, subN
	if ascii {
		if !IsASCII(sub) {
			return -1
		}
	} else {
		length = Len(data, n)
		subLen = Len(sub, subN)
	}
	last := length - subLen
	if backward {
		from = min(from, last)
	}
	if from < 0 || from > last {
		return -1
	}
	if subN == 0 {
		return from
	}
	step := 1
	if backward {
		step = -1
	}
	if ascii {
		mask := uint64(1)<<(8*subN) - 1
		for i := from; i >= 0 && i <= last; i += step {
			if data>>(8*i)&mask == sub {
				return i
			}
		}
		return -1
	}
	for i := from; i >= 0 && i <= last; i += step {
		part, size := Slice(data, i, i+subLen)
		if size == subN && part == sub {
			return i
		}
	}
	return -1
}

// Concat returns the packed concatenation and its byte length, or false if it does not fit.
// aN and bN are the operands' byte lengths.
func Concat(a uint64, aN int, b uint64, bN int) (uint64, int, bool) {
	n := aN + bN
	if n > MaxBytes+2 {
		return 0, 0, false
	}
	if aN >= 3 && bN >= 3 {
		tail := a >> (8 * (aN - 3))
		if tail&0xf0ff == 0xa0ed && b&0xf0ff == 0xb0ed {
			// Joining the boundary surrogates replaces six bytes with four.
			high := uint32(unitAtSlow(tail, 0) - 0xd800)
			low := uint32(unitAtSlow(b, 0) - 0xdc00)
			r := 0x10000 + high<<10 + low
			pair := uint64(0xf0|r>>18) | uint64(0x80|r>>12&0x3f)<<8 |
				uint64(0x80|r>>6&0x3f)<<16 | uint64(0x80|r&0x3f)<<24
			prefix := a & (uint64(1)<<(8*(aN-3)) - 1)
			return prefix | pair<<(8*(aN-3)) | (b>>24)<<(8*(aN+1)), n - 2, true
		}
	}
	if n > MaxBytes {
		return 0, 0, false
	}
	return a | b<<(8*aN), n, true
}
