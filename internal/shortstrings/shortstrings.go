// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package shortstrings operates on packed canonical WTF-8 strings.
package shortstrings

import (
	"cmp"
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
func UnitAt(data uint64, i int) (u uint16) {
	// Keep the ASCII check and result projection small enough to inline.
	if data&0x8080808080808080 == 0 {
		return uint16(byte(data >> (8 * i)))
	}
	u, _ = decodeAt(data, i)
	return
}

// CodePointAt returns the code point starting at UTF-16 index i. i must be in range.
func CodePointAt(data uint64, i int) (r rune) {
	if data&0x8080808080808080 == 0 {
		return rune(byte(data >> (8 * i)))
	}
	_, r = decodeAt(data, i)
	return
}

// decodeAt returns the code unit and forward code point at UTF-16 index i.
func decodeAt(data uint64, i int) (uint16, rune) {
	for {
		b := byte(data)
		switch {
		case b < 0x80:
			if i == 0 {
				return uint16(b), rune(b)
			}
			data >>= 8
		case b < 0xe0:
			if i == 0 {
				u := uint16(b&0x1f)<<6 | uint16(data>>8&0x3f)
				return u, rune(u)
			}
			data >>= 16
		case b < 0xf0:
			if i == 0 {
				u := uint16(b&0x0f)<<12 | uint16(data>>8&0x3f)<<6 | uint16(data>>16&0x3f)
				return u, rune(u)
			}
			data >>= 24
		default:
			if i < 2 {
				r := rune(b&7)<<18 | rune(data>>8&0x3f)<<12 | rune(data>>16&0x3f)<<6 | rune(data>>24&0x3f)
				if i == 0 {
					return 0xd800 + uint16((r-0x10000)>>10), r
				}
				u := 0xdc00 + uint16((r-0x10000)&0x3ff)
				return u, rune(u)
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
				u, _ := decodeAt(data, half)
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
			high, _ := decodeAt(tail, 0)
			low, _ := decodeAt(b, 0)
			r := 0x10000 + uint32(high-0xd800)<<10 + uint32(low-0xdc00)
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

// Compare orders the packed strings by UTF-16 code units and returns -1, 0, or 1.
// aN and bN are their byte lengths.
func Compare(a uint64, aN int, b uint64, bN int) int {
	i := bits.TrailingZeros64(a^b) / 8
	if i >= min(aN, bN) {
		return cmp.Compare(aN, bN)
	}
	x, y := byte(a>>(8*i)), byte(b>>(8*i))
	if (x >= 0xf0 && y >= 0xed && y < 0xf0) || (y >= 0xf0 && x >= 0xed && x < 0xf0) {
		// These differing lead bytes are code point boundaries. Supplementary
		// characters sort by their surrogate units among these BMP characters.
		return compareUnits(a>>(8*i), aN-i, b>>(8*i), bN-i)
	}
	return cmp.Compare(x, y)
}

func compareUnits(a uint64, aN int, b uint64, bN int) int {
	aLen, bLen := Len(a, aN), Len(b, bN)
	for i := range min(aLen, bLen) {
		x, _ := decodeAt(a, i)
		y, _ := decodeAt(b, i)
		if x != y {
			return cmp.Compare(x, y)
		}
	}
	return cmp.Compare(aLen, bLen)
}

// IsWellFormed reports whether the packed string contains no lone surrogates.
func IsWellFormed(data uint64) bool {
	return loneSurrogateMask(data) == 0
}

// ToWellFormed replaces lone surrogates with U+FFFD in the packed string, preserving its byte length.
func ToWellFormed(data uint64) uint64 {
	for mask := loneSurrogateMask(data); mask != 0; mask &= mask - 1 {
		shift := bits.TrailingZeros64(mask) - 7
		data = data&^(uint64(0xffffff)<<shift) | uint64(0xbdbfef)<<shift
	}
	return data
}

// loneSurrogateMask marks bit 7 of each lone surrogate's leading byte.
func loneSurrogateMask(data uint64) uint64 {
	if IsASCII(data) {
		return 0
	}
	const lowBits = 0x7f7f7f7f7f7f7f7f
	x := data ^ 0xedededededededed
	// Masking before the addition prevents carries between bytes.
	leads := ^((x & lowBits) + lowBits | x) & 0x8080808080808080
	// A 0xed lead followed by a continuation byte with bit 5 set encodes a lone surrogate.
	return leads & (data >> 6)
}

// ToUpperASCII converts data to uppercase. data must be a packed ASCII string.
func ToUpperASCII(data uint64) uint64 {
	// ASCII bytes leave room for these additions without carrying between bytes.
	letters := (data + 0x1f1f1f1f1f1f1f1f) &^ (data + 0x0505050505050505) & 0x8080808080808080
	return data &^ (letters >> 2)
}

// ToLowerASCII converts data to lowercase. data must be a packed ASCII string.
func ToLowerASCII(data uint64) uint64 {
	letters := (data + 0x3f3f3f3f3f3f3f3f) &^ (data + 0x2525252525252525) & 0x8080808080808080
	return data | (letters >> 2)
}
