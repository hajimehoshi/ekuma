// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// IsStrictlyEqual implements IsStrictlyEqual.
func IsStrictlyEqual(a, b Value) bool {
	if a.O == NumberObject || b.O == NumberObject {
		return a.O == b.O && a.N == b.N
	}
	if a.IsShortString() || b.IsShortString() {
		return a.IsString() && b.IsString() && len(a.O.Text()) == len(b.O.Text()) && math.Float64bits(a.N) == math.Float64bits(b.N)
	}
	if a.O == b.O {
		return a.O != BigInt64Object || math.Float64bits(a.N) == math.Float64bits(b.N)
	}
	if a.O == nil || b.O == nil || a.O.Kind != b.O.Kind {
		return false
	}
	switch a.O.Kind {
	case KindString:
		return sameStrings(a.O, b.O)
	case KindBigInt:
		// A BigInt that fits in an int64 never equals one that does not.
		return a.O != BigInt64Object && b.O != BigInt64Object && a.O.heapBigInt().Cmp(b.O.heapBigInt()) == 0
	}
	return false
}

// SameValueZero implements SameValueZero.
func SameValueZero(a, b Value) bool {
	if a.O == NumberObject && b.O == NumberObject {
		return a.N == b.N || (a.N != a.N && b.N != b.N)
	}
	return IsStrictlyEqual(a, b)
}

// SameValue implements SameValue.
func SameValue(a, b Value) bool {
	if a.O == NumberObject && b.O == NumberObject {
		if a.N != a.N {
			return b.N != b.N
		}
		return a.N == b.N && math.Signbit(a.N) == math.Signbit(b.N)
	}
	return IsStrictlyEqual(a, b)
}

type CmpResult uint8

const (
	CmpLess CmpResult = iota
	CmpEqual
	CmpGreater
	CmpUndefined
)

// Reverse returns the result of comparing b with a, given that of comparing a with b.
func (c CmpResult) Reverse() CmpResult {
	switch c {
	case CmpLess:
		return CmpGreater
	case CmpGreater:
		return CmpLess
	}
	return c
}

func CompareNumbers(x, y float64) CmpResult {
	switch {
	case x < y:
		return CmpLess
	case x > y:
		return CmpGreater
	case x == y:
		return CmpEqual
	}
	return CmpUndefined
}

// CompareInt64Number compares the integer x with the number f.
func CompareInt64Number(x int64, f float64) CmpResult {
	switch {
	case f != f:
		return CmpUndefined
	case f >= 1<<63:
		return CmpLess
	case f < -1<<63:
		return CmpGreater
	}
	// t lies in the range of int64 and converts exactly.
	t := math.Trunc(f)
	if c := CompareInt64s(x, int64(t)); c != CmpEqual {
		return c
	}
	return CompareNumbers(t, f)
}

func CompareInt64s(x, y int64) CmpResult {
	switch {
	case x < y:
		return CmpLess
	case x > y:
		return CmpGreater
	}
	return CmpEqual
}

// CompareStrings orders a and b by UTF-16 code units and returns -1, 0, or 1.
// Both values must be strings.
func CompareStrings(a, b Value) int {
	aN, bN := a.StringByteLength(), b.StringByteLength()
	if aN <= shortstrings.MaxBytes && bN <= shortstrings.MaxBytes {
		return shortstrings.Compare(math.Float64bits(a.N), aN, math.Float64bits(b.N), bN)
	}
	return wtf8.Compare(a.Str(), b.Str())
}
