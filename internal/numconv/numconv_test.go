// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv_test

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
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
		{in: 1<<63 - 1024, want: -1024},
		{in: 1<<63 + 2048, want: 2048},
		{in: 1 << 63, want: 0},
		{in: -(1<<63 + 4096), want: -4096},
	}
	for _, tt := range tests {
		if got := numconv.ToInt32(tt.in); got != tt.want {
			t.Errorf("ToInt32(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestToUint32(t *testing.T) {
	tests := []struct {
		in   float64
		want uint32
	}{
		{in: -1.9, want: 4294967295},
		{in: 4294967295, want: 4294967295},
		{in: 1<<63 - 1024, want: 4294966272},
		{in: 1<<63 + 2048, want: 2048},
		{in: math.Inf(-1), want: 0},
	}
	for _, tt := range tests {
		if got := numconv.ToUint32(tt.in); got != tt.want {
			t.Errorf("ToUint32(%v) = %d, want %d", tt.in, got, tt.want)
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

// TestBigIntConversions checks the conversions of integers that take several steps against
// math/big.
func TestBigIntConversions(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	const symbols = "0123456789abcdefghijklmnopqrstuvwxyz"
	for _, radix := range []int{3, 10, 16, 36} {
		for _, n := range []int{1, 8192, 8193, 40000, 100000} {
			var sb strings.Builder
			sb.WriteByte(symbols[1+r.IntN(radix-1)])
			for range n - 1 {
				sb.WriteByte(symbols[r.IntN(radix)])
			}
			// Runs of zeros fall on the boundaries of the steps.
			s := sb.String()[:(n+1)/2] + strings.Repeat("0", n/2)
			want, _ := new(big.Int).SetString(s, radix)
			got, ok := numconv.ParseBigInt(s, radix, nil)
			if !ok || got.Cmp(want) != 0 {
				t.Errorf("ParseBigInt of %d symbols in radix %d: got %v, %t", n, radix, got, ok)
				continue
			}
			x := new(big.Int).Neg(want)
			x.Add(x, big.NewInt(1))
			if got, want := numconv.FormatBigInt(x, radix, nil), x.Text(radix); got != want {
				t.Errorf("FormatBigInt of %d symbols in radix %d differs from Text", n, radix)
			}
		}
	}
}

// TestBigIntArithmetic checks MulBigInt and QuoRemBigInt with steps of a few words, so that
// small integers take many steps.
func TestBigIntArithmetic(t *testing.T) {
	defer numconv.SetArithLeafWords(3)()
	r := rand.New(rand.NewPCG(3, 4))
	random := func() *big.Int {
		x := new(big.Int)
		for range r.IntN(40) {
			x.Lsh(x, 64)
			// Words of all ones or zeros make carries and borrows.
			switch r.IntN(4) {
			case 0:
			case 1:
				x.Or(x, new(big.Int).SetUint64(math.MaxUint64))
			default:
				x.Or(x, new(big.Int).SetUint64(r.Uint64()))
			}
		}
		x.Rsh(x, uint(r.IntN(64)))
		if r.IntN(2) == 0 {
			x.Neg(x)
		}
		return x
	}
	for range 20000 {
		x, y := random(), random()
		if got, want := numconv.MulBigInt(x, y, nil), new(big.Int).Mul(x, y); got.Cmp(want) != 0 {
			t.Errorf("MulBigInt(%v, %v): got %v, want %v", x, y, got, want)
		}
		if y.Sign() == 0 {
			continue
		}
		q, rem := numconv.QuoRemBigInt(x, y, nil)
		wq, wr := new(big.Int).QuoRem(x, y, new(big.Int))
		if q.Cmp(wq) != 0 || rem.Cmp(wr) != 0 {
			t.Errorf("QuoRemBigInt(%v, %v): got %v, %v, want %v, %v", x, y, q, rem, wq, wr)
		}
	}
	// The conversions divide and multiply by powers in many steps.
	for _, n := range []int{1 << 16, 1<<17 + 12345} {
		x := new(big.Int)
		for range n / 64 {
			x.Lsh(x, 64)
			x.Or(x, new(big.Int).SetUint64(r.Uint64()))
		}
		s := numconv.FormatBigInt(x, 10, nil)
		if s != x.Text(10) {
			t.Errorf("FormatBigInt of %d bits differs from Text", n)
		}
		if y, ok := numconv.ParseBigInt(s, 10, nil); !ok || y.Cmp(x) != 0 {
			t.Errorf("ParseBigInt of %d bits: got %t, want the integer", n, ok)
		}
	}
}

func TestStringToBigIntLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want error
	}{
		{
			in:   "0xff",
			want: nil,
		},
		{
			in:   "-256",
			want: numconv.ErrBigIntTooLarge,
		},
		{
			// The digits are checked before the size.
			in:   "1" + strings.Repeat("0", 100) + "x",
			want: numconv.ErrNotBigInt,
		},
	} {
		if _, err := numconv.StringToBigInt(tc.in, 8, nil); !errors.Is(err, tc.want) {
			t.Errorf("StringToBigInt(%q, 8): got %v, want %v", tc.in, err, tc.want)
		}
	}
}
