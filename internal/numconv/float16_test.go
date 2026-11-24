// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv_test

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

func TestFloat16(t *testing.T) {
	negZero := math.Copysign(0, -1)
	tests := []struct {
		in    float64
		round float64
		bits  uint16
	}{
		{in: negZero, round: negZero, bits: 0x8000},
		{in: math.NaN(), round: math.NaN(), bits: 0x7e00},
		{in: math.Inf(-1), round: math.Inf(-1), bits: 0xfc00},
		// The largest finite value, and the midpoint above it.
		{in: math.Nextafter(65520, 0), round: 65504, bits: 0x7bff},
		{in: -65520, round: math.Inf(-1), bits: 0xfc00},
		// Ties to even, down and up.
		{in: 1 + 0x1p-11, round: 1, bits: 0x3c00},
		{in: 1 + 0x3p-11, round: 1 + 0x1p-9, bits: 0x3c02},
		// Rounding to the next power of 2.
		{in: math.Nextafter(2, 0), round: 2, bits: 0x4000},
		// Subnormals: a tie to zero, the smallest, and rounding to the smallest normal value.
		{in: -0x1p-25, round: negZero, bits: 0x8000},
		{in: math.Nextafter(0x1p-25, 1), round: 0x1p-24, bits: 0x0001},
		{in: math.Nextafter(0x1p-14, 0), round: 0x1p-14, bits: 0x0400},
	}
	for _, tt := range tests {
		got := numconv.RoundFloat16(tt.in)
		if got != tt.round && (got == got || tt.round == tt.round) || math.Signbit(got) != math.Signbit(tt.round) {
			t.Errorf("RoundFloat16(%v) = %v, want %v", tt.in, got, tt.round)
		}
		if got := numconv.Float16Bits(tt.in); got != tt.bits {
			t.Errorf("Float16Bits(%v) = %#04x, want %#04x", tt.in, got, tt.bits)
		}
	}
}

func TestFloat16RoundTrip(t *testing.T) {
	for i := range 1 << 16 {
		h := uint16(i)
		want := h
		if h&0x7fff > 0x7c00 {
			// Every NaN has one encoding.
			want = 0x7e00
		}
		if got := numconv.Float16Bits(numconv.Float16FromBits(h)); got != want {
			t.Errorf("Float16Bits(Float16FromBits(%#04x)) = %#04x, want %#04x", h, got, want)
		}
	}
}
