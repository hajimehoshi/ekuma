// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv_test

import (
	"math"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

func TestNumberToString(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{in: 0, want: "0"},
		{in: math.Copysign(0, -1), want: "0"},
		{in: -1.5, want: "-1.5"},
		{in: 0.30000000000000004, want: "0.30000000000000004"},
		{in: 1e21, want: "1e+21"},
		{in: 123456789012345680000, want: "123456789012345680000"},
		{in: 1e-6, want: "0.000001"},
		{in: 1e-7, want: "1e-7"},
		{in: 1.5e-10, want: "1.5e-10"},
		{in: 5e-324, want: "5e-324"},
		{in: math.NaN(), want: "NaN"},
		{in: math.Inf(-1), want: "-Infinity"},
	}
	for _, tt := range tests {
		if got := numconv.NumberToString(tt.in); got != tt.want {
			t.Errorf("NumberToString(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStringToNumber(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		in   string
		want float64
	}{
		{in: "", want: 0},
		{in: " \t\n12 ", want: 12},
		{in: "007", want: 7},
		{in: "-1.5e3", want: -1500},
		{in: ".5", want: 0.5},
		{in: "5.", want: 5},
		{in: "0x1F", want: 31},
		{in: "0b101", want: 5},
		{in: "0o17", want: 15},
		{in: "-0x1F", want: nan},
		{in: "Infinity", want: math.Inf(1)},
		{in: "-Infinity", want: math.Inf(-1)},
		{in: "infinity", want: nan},
		{in: "1_000", want: nan},
		{in: "1e", want: nan},
		{in: "0x", want: nan},
		{in: "12px", want: nan},
		{in: "1e400", want: math.Inf(1)},
		{in: "0x20000000000001", want: 9007199254740992},
	}
	for _, tt := range tests {
		got := numconv.StringToNumber(tt.in)
		if got != tt.want && !(got != got && tt.want != tt.want) {
			t.Errorf("StringToNumber(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestToInt32(t *testing.T) {
	tests := []struct {
		in   float64
		want int32
	}{
		{in: 1.9, want: 1},
		{in: -1.9, want: -1},
		{in: 2147483648, want: -2147483648},
		{in: 4294967296 + 5, want: 5},
		{in: -4294967296 - 5, want: -5},
		{in: math.NaN(), want: 0},
		{in: math.Inf(1), want: 0},
		{in: 1e30, want: 0},
	}
	for _, tt := range tests {
		if got := numconv.ToInt32(tt.in); got != tt.want {
			t.Errorf("ToInt32(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		in    string
		radix int
		want  float64
	}{
		{in: "  42px", radix: 0, want: 42},
		{in: "-0x1f", radix: 0, want: -31},
		{in: "0x1f", radix: 10, want: 0},
		{in: "zz", radix: 36, want: 1295},
		{in: "12", radix: 2, want: 1},
	}
	for _, tt := range tests {
		if got := numconv.ParseInt(tt.in, tt.radix); got != tt.want {
			t.Errorf("ParseInt(%q, %d) = %v, want %v", tt.in, tt.radix, got, tt.want)
		}
	}
}
