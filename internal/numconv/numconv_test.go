// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv_test

import (
	"errors"
	"math"
	"math/big"
	"math/bits"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestNumberToString(t *testing.T) {
	tests := []struct {
		in   float64
		want wtf8.String
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
		got := numconv.StringToNumber(wtf8.String(tt.in), nil)
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
		if got := numconv.ParseInt(wtf8.String(tt.in), tt.radix, nil); got != tt.want {
			t.Errorf("ParseInt(%q, %d) = %v, want %v", tt.in, tt.radix, got, tt.want)
		}
	}
}

// TestBigIntConversions checks the conversions of integers that take several steps against
// math/big.
func TestBigIntConversions(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	const symbols = "0123456789abcdefghijklmnopqrstuvwxyz"
	for _, radix := range []int{2, 3, 8, 10, 16, 32, 36} {
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
			if got, want := numconv.FormatBigInt(x, radix, nil), wtf8.String(x.Text(radix)); got != want {
				t.Errorf("FormatBigInt of %d symbols in radix %d differs from Text", n, radix)
			}
		}
	}
}

func TestParseBigIntPoll(t *testing.T) {
	// A long string of digits in a power-of-two radix is converted in polled steps.
	for _, radix := range []int{2, 8, 16} {
		var polls int
		s := strings.Repeat("1", 1<<20)
		if _, ok := numconv.ParseBigInt(s, radix, func() { polls++ }); !ok {
			t.Errorf("ParseBigInt in radix %d: not an integer", radix)
		}
		if polls < 16 {
			t.Errorf("ParseBigInt in radix %d: got %d polls, want at least 16", radix, polls)
		}
	}
}

func TestFormatBigIntPowerOfTwoPoll(t *testing.T) {
	var polled bool
	x := new(big.Int).Lsh(big.NewInt(-3), 100000)
	got := numconv.FormatBigInt(x, 8, func() { polled = true })
	if string(got) != x.Text(8) || !polled {
		t.Errorf("large octal BigInt: correct digits %t, polled %t", string(got) == x.Text(8), polled)
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
		if s != wtf8.String(x.Text(10)) {
			t.Errorf("FormatBigInt of %d bits differs from Text", n)
		}
		if y, ok := numconv.ParseBigInt(string(s), 10, nil); !ok || y.Cmp(x) != 0 {
			t.Errorf("ParseBigInt of %d bits: got %t, want the integer", n, ok)
		}
	}
}

func TestExpBigInt(t *testing.T) {
	for _, a := range []int64{3, -7, -8} {
		for _, n := range []int64{0, 1000, 1001} {
			var polls int
			var got big.Int
			numconv.ExpBigInt(&got, big.NewInt(a), n, func() { polls++ })
			if want := new(big.Int).Exp(big.NewInt(a), big.NewInt(n), nil); got.Cmp(want) != 0 {
				t.Errorf("ExpBigInt(%d, %d) = %v, want %v", a, n, &got, want)
			}
			// A power of a power of two is a shift, which has no steps.
			if steps := bits.Len64(uint64(n)); a != -8 && polls < steps {
				t.Errorf("ExpBigInt(%d, %d): got %d polls, want at least %d", a, n, polls, steps)
			}
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
		if _, err := numconv.StringToBigInt(wtf8.String(tc.in), 8, nil); !errors.Is(err, tc.want) {
			t.Errorf("StringToBigInt(%q, 8): got %v, want %v", tc.in, err, tc.want)
		}
	}
}

func TestStringToBigIntWhitespacePoll(t *testing.T) {
	var polled bool
	s := wtf8.String(strings.Repeat("　", 1<<15))
	got, err := numconv.StringToBigInt(s, 8, func() { polled = true })
	if err != nil || got.Sign() != 0 || !polled {
		t.Errorf("StringToBigInt of whitespace = %v, %v, polled %t", got, err, polled)
	}
}

func TestBigIntDigitsTooLargeLeadingZerosPoll(t *testing.T) {
	var polled bool
	s := strings.Repeat("0", 1<<17) + "1"
	if numconv.BigIntDigitsTooLarge(s, 16, func() { polled = true }) || !polled {
		t.Error("small hexadecimal BigInt with leading zeros should pass the size check and poll")
	}
}

func TestBigIntDigitsTooLarge(t *testing.T) {
	// 2^28 hexadecimal digits have 2^30 bits when the first is at least 8.
	s := "1" + strings.Repeat("0", 1<<28)
	for _, tc := range []struct {
		digits string
		want   bool
	}{
		{
			digits: "8" + s[1:len(s)-1],
			want:   false,
		},
		{
			digits: s,
			want:   true,
		},
	} {
		if got := numconv.BigIntDigitsTooLarge(tc.digits, 16, nil); got != tc.want {
			t.Errorf("BigIntDigitsTooLarge of %d digits: got %t, want %t", len(tc.digits), got, tc.want)
		}
	}
}

func TestStringToNumberLong(t *testing.T) {
	zeros := strings.Repeat("0", 70000)
	for _, s := range []string{
		// Halfway between two numbers up to a digit far away.
		"9007199254740993" + zeros,
		"9007199254740993" + zeros + "1",
		"9007199254740993" + zeros + "e-70000",
		"9007199254740993" + zeros + "1e-70001",
		"-9007199254740993." + zeros + "1",
		"9007199254740992.5" + zeros + "1",
		"0." + zeros + "12345678901234567890123",
		"-0." + zeros,
		zeros + "." + zeros + "e999999",
		zeros + "1.5",
		"1" + zeros + "e-70000",
		"1" + zeros,
		strings.Repeat("123456789", 9000) + "e-80000",
		"2.4703282292062327" + zeros + "1e-324",
		"9007199254740993" + strings.Repeat("0", 790) + "1e-791",
	} {
		// A rational number holds the exact value, which strconv.ParseFloat misplaces for
		// more than 800 digits.
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("big.Rat.SetString(%.24q...) failed", s)
		}
		want, _ := r.Float64()
		if strings.HasPrefix(s, "-") {
			want = math.Copysign(want, -1)
		}
		if got := numconv.StringToNumber(wtf8.String(s), nil); got != want || math.Signbit(got) != math.Signbit(want) {
			t.Errorf("StringToNumber(%.24q...) = %v, want %v", s, got, want)
		}
	}
}

func TestStringToNumberLongExponent(t *testing.T) {
	exponent := strings.Repeat("0", 780) + "4294967296"
	if got := numconv.StringToNumber(wtf8.String("1e"+exponent), nil); got != math.Inf(1) {
		t.Errorf("positive exponent: got %v, want +Inf", got)
	}
	if got := numconv.StringToNumber(wtf8.String("1e-"+exponent), nil); got != 0 || math.Signbit(got) {
		t.Errorf("negative exponent: got %v, want +0", got)
	}

	// The exponent cancels the leading zeros, leaving the halfway integer 2^53+1.
	s := "0." + strings.Repeat("0", 4000) + "9007199254740993e4016"
	if got := numconv.StringToNumber(wtf8.String(s), nil); got != 1<<53 {
		t.Errorf("halfway integer: got %v, want %v", got, float64(1<<53))
	}
}

func TestStringToNumberManyDigits(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 300 {
		// The halfway point between two numbers, with a digit perturbing it far away.
		f := math.Float64frombits(r.Uint64() &^ (1 << 63))
		if math.IsInf(f, 0) || math.IsNaN(f) {
			continue
		}
		next := math.Nextafter(f, math.Inf(1))
		if math.IsInf(next, 0) {
			continue
		}
		mid := new(big.Rat).Add(new(big.Rat).SetFloat64(f), new(big.Rat).SetFloat64(next))
		mid.Quo(mid, big.NewRat(2, 1))
		digits := mid.FloatString(700 + r.IntN(1800))
		switch r.IntN(3) {
		case 0:
			digits += "1"
		case 1:
			digits = digits[:len(digits)-r.IntN(40)]
		}
		want, _ := new(big.Rat).SetString(digits)
		w, _ := want.Float64()
		if got := numconv.StringToNumber(wtf8.String(digits), nil); got != w {
			t.Errorf("StringToNumber(%.40q...) = %v, want %v", digits, got, w)
		}
	}
}

func TestStringToNumberPolls(t *testing.T) {
	long := wtf8.String(strings.Repeat("1", 1<<20))
	for _, tc := range []struct {
		name string
		conv func(poll func()) float64
	}{
		{
			name: "digits",
			conv: func(poll func()) float64 { return numconv.StringToNumber(long, poll) },
		},
		{
			name: "leading white space",
			conv: func(poll func()) float64 {
				return numconv.StringToNumber(wtf8.String(strings.Repeat(" ", 1<<20)+"1"), poll)
			},
		},
		{
			name: "trailing white space",
			conv: func(poll func()) float64 {
				return numconv.StringToNumber(wtf8.String("1"+strings.Repeat("　", 1<<18)), poll)
			},
		},
		{
			name: "leading zeros",
			conv: func(poll func()) float64 {
				return numconv.StringToNumber(wtf8.String(strings.Repeat("0", 1<<20)+"1"), poll)
			},
		},
		{
			name: "hexadecimal",
			conv: func(poll func()) float64 {
				return numconv.StringToNumber(wtf8.String("0x"+strings.Repeat("0", 1<<20)+"1"), poll)
			},
		},
		{
			name: "parseFloat",
			conv: func(poll func()) float64 { return numconv.ParseFloat(long+"x", poll) },
		},
		{
			name: "parseInt",
			conv: func(poll func()) float64 { return numconv.ParseInt(long+"x", 10, poll) },
		},
	} {
		polls := 0
		got := tc.conv(func() { polls++ })
		if got != got || got == 0 {
			t.Errorf("%s: got %v", tc.name, got)
		}
		if polls == 0 {
			t.Errorf("%s: no polls", tc.name)
		}
	}
}
