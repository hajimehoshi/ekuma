// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import "math"

// RoundFloat16 returns f rounded to the nearest binary16 value, ties to even.
func RoundFloat16(f float64) float64 {
	if f != f || math.IsInf(f, 0) || f == 0 {
		return f
	}
	a := math.Abs(f)
	// 65520 is halfway between the largest binary16 value and 2^16, and rounds to infinity.
	if a >= 65520 {
		return math.Copysign(math.Inf(1), f)
	}
	_, exp := math.Frexp(a)
	// The unit in the last place of binary16 at a, with the subnormals below 2^-14.
	e := max(exp-1, -14)
	ulp := math.Ldexp(1, e-10)
	return math.Copysign(math.RoundToEven(a/ulp)*ulp, f)
}

// Float16Bits returns the binary16 encoding of f rounded to binary16.
func Float16Bits(f float64) uint16 {
	var sign uint16
	if math.Signbit(f) {
		sign = 0x8000
	}
	switch {
	case f != f:
		return 0x7e00
	case math.IsInf(f, 0):
		return sign | 0x7c00
	case f == 0:
		return sign
	}
	a := math.Abs(RoundFloat16(f))
	if math.IsInf(a, 0) {
		return sign | 0x7c00
	}
	if a < 0x1p-14 {
		return sign | uint16(a*0x1p24)
	}
	_, exp := math.Frexp(a)
	e := exp - 1
	m := uint16((math.Ldexp(a, -e) - 1) * 1024)
	return sign | uint16(e+15)<<10 | m
}

// Float16FromBits returns the number that the binary16 encoding h stands for.
func Float16FromBits(h uint16) float64 {
	e := int(h>>10) & 0x1f
	m := float64(h & 0x3ff)
	var f float64
	switch e {
	case 0:
		f = m * 0x1p-24
	case 0x1f:
		if m != 0 {
			return math.NaN()
		}
		f = math.Inf(1)
	default:
		f = math.Ldexp(1+m/1024, e-15)
	}
	if h&0x8000 != 0 {
		f = -f
	}
	return f
}
