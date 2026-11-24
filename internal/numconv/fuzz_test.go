// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv_test

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// digits maps the bytes of s to digits of a radix.
func digits(s string, radix int) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, len(s))
	for i := range b {
		c := s[i]
		d := numconv.DigitValue(c)
		if d == 36 {
			d = int(c)
		}
		b[i] = alphabet[d%radix]
	}
	return string(b)
}

// nearest returns the number nearest to x, and -0 for a zero or an underflow when neg is set.
func nearest(x *big.Rat, neg bool) float64 {
	f, _ := x.Float64()
	if neg && f == 0 {
		return math.Copysign(0, -1)
	}
	return f
}

// same reports whether x and y are the same number, which tells 0 and -0 apart.
func same(x, y float64) bool {
	return math.Float64bits(x) == math.Float64bits(y)
}

// clip returns the start of s for messages.
func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "... (" + strconv.Itoa(len(s)) + " bytes)"
	}
	return s
}

func FuzzStringToNumberDecimal(f *testing.F) {
	for _, exp := range []string{"0", "1", "308", "309", "-324", "-325", "16", "4294967296", "-4294967296", "18446744073709551616", "4016", "-800"} {
		f.Add("1", "", exp, uint16(0), uint16(0), uint16(0), false)
		f.Add("0", "9007199254740993", exp, uint16(4000), uint16(0), uint16(0), false)
		f.Add("1", "", exp, uint16(0), uint16(780), uint16(0), true)
	}
	for _, s := range []string{"9007199254740993", "9223372036854775807", "18446744073709551615", "179769313486231580793", "2.4703282292062327208", "49406564584124654417"} {
		ip, fp, _ := strings.Cut(s, ".")
		f.Add(ip, fp, "0", uint16(0), uint16(0), uint16(800), false)
		f.Add(ip, fp+"1", "-324", uint16(800), uint16(0), uint16(0), true)
		f.Add(ip, fp+"1", "288", uint16(0), uint16(2), uint16(1000), true)
	}
	// zeros, expZeros and lastZeros are the numbers of zeros after the point, before the
	// exponent, and before the last digit of the fraction.
	f.Fuzz(func(t *testing.T, ip, fp, exp string, zeros, expZeros, lastZeros uint16, neg bool) {
		if len(ip)+len(fp) > 2<<10 || len(exp) > 32 {
			t.Skip("too long")
		}
		ip = digits(ip, 10)
		fp = strings.Repeat("0", int(zeros)%5000) + digits(fp, 10)
		if lastZeros > 0 {
			fp += strings.Repeat("0", int(lastZeros)%2000) + "1"
		}
		if ip == "" && fp == "" {
			ip = "0"
		}
		expSign := ""
		if strings.HasPrefix(exp, "-") {
			expSign, exp = "-", exp[1:]
		}
		exp = digits(exp, 10)

		s := ip + "." + fp
		mantissa, _ := new(big.Int).SetString(ip+fp, 10)
		scale := new(big.Int)
		if exp != "" {
			scale.SetString(expSign+exp, 10)
			s += "e" + expSign + strings.Repeat("0", int(expZeros)%2000) + exp
		}
		scale.Sub(scale, big.NewInt(int64(len(fp))))
		if neg {
			s = "-" + s
			mantissa.Neg(mantissa)
		}

		// An exponent beyond the number of the digits and 400 decides the result alone.
		limit := big.NewInt(int64(len(ip) + len(fp) + 400))
		var want float64
		switch {
		case mantissa.Sign() == 0 || scale.Cmp(new(big.Int).Neg(limit)) < 0:
			want = nearest(new(big.Rat), neg)
		case scale.Cmp(limit) > 0:
			want = math.Inf(mantissa.Sign())
		case scale.Sign() < 0:
			p := new(big.Int).Exp(big.NewInt(10), scale.Neg(scale), nil)
			want = nearest(new(big.Rat).SetFrac(mantissa, p), neg)
		default:
			p := new(big.Int).Exp(big.NewInt(10), scale, nil)
			want = nearest(new(big.Rat).SetInt(p.Mul(p, mantissa)), neg)
		}
		if got := numconv.StringToNumber(wtf8.String(s), nil); !same(got, want) {
			t.Errorf("StringToNumber(%q) = %v, want %v", clip(s), got, want)
		}
		if got := numconv.ParseFloat(wtf8.String(" "+s+".5e+"), nil); !same(got, want) {
			t.Errorf("ParseFloat(%q) = %v, want %v", clip(s), got, want)
		}
	})
}

func FuzzRadixDigits(f *testing.F) {
	for radix := 2; radix <= 36; radix++ {
		for _, n := range []uint{53, 54, 63, 64, 127, 128, 1023, 1024, 1025} {
			for d := int64(-1); d <= 1; d++ {
				x := new(big.Int).Lsh(big.NewInt(1), n)
				f.Add(x.Add(x, big.NewInt(d)).Text(radix), uint8(radix), uint16(1))
			}
		}
	}
	// A halfway integer that only a digit far behind decides.
	f.Add("20000000000001"+strings.Repeat("0", 100)+"1", uint8(16), uint16(1))
	f.Add("zy", uint8(36), uint16(6000))
	f.Add("98", uint8(10), uint16(6000))
	// repeat is the number of copies of s, for integers that are converted in several steps.
	f.Fuzz(func(t *testing.T, s string, radixByte uint8, repeat uint16) {
		radix := int(radixByte)%35 + 2
		if radixByte <= 36 {
			radix = max(int(radixByte), 2)
		}
		if len(s) > 2<<10 || len(s)*int(repeat) > 12<<10 {
			t.Skip("too long")
		}
		s = strings.Repeat(digits(s, radix), max(int(repeat), 1))
		if s == "" {
			t.Skip("no digits")
		}
		x, _ := new(big.Int).SetString(s, radix)

		got, ok := numconv.ParseBigInt(s, radix, nil)
		if !ok || got.Cmp(x) != 0 {
			t.Errorf("ParseBigInt(%q, %d) = %v, %t", clip(s), radix, got, ok)
		}
		for _, v := range []*big.Int{x, new(big.Int).Neg(x)} {
			poll := func() {}
			if got := string(numconv.FormatBigInt(v, radix, poll)); got != v.Text(radix) {
				t.Errorf("FormatBigInt(%q, %d) = %q", clip(v.Text(radix)), radix, clip(got))
			}
		}

		want := nearest(new(big.Rat).SetInt(x), false)
		if f, ok := numconv.ParseRadixDigits(s, radix, nil); !ok || !same(f, want) {
			t.Errorf("ParseRadixDigits(%q, %d) = %v, %t; want %v", clip(s), radix, f, ok, want)
		}
		if f := numconv.ParseInt(wtf8.String("-"+s), radix, nil); !same(f, -want) {
			t.Errorf("ParseInt(%q, %d) = %v, want %v", clip("-"+s), radix, f, -want)
		}

		prefix, ok := map[int]string{
			2:  "0b",
			8:  "0o",
			10: "",
			16: "0x",
		}[radix]
		if !ok {
			return
		}
		const maxBits = 1 << 10
		b, err := numconv.StringToBigInt(wtf8.String(prefix+s), maxBits, nil)
		switch {
		case x.BitLen() > maxBits:
			if !errors.Is(err, numconv.ErrBigIntTooLarge) {
				t.Errorf("StringToBigInt(%q): got %v, want %v", clip(prefix+s), err, numconv.ErrBigIntTooLarge)
			}
		case err != nil || b.Cmp(x) != 0:
			t.Errorf("StringToBigInt(%q) = %v, %v", clip(prefix+s), b, err)
		}
	})
}

// specDecimal lays out the number whose shortest digits are those of n as Number::toString does.
func specDecimal(n float64) string {
	switch {
	case n != n:
		return "NaN"
	case n == 0:
		return "0"
	case n < 0:
		return "-" + specDecimal(-n)
	case math.IsInf(n, 1):
		return "Infinity"
	}
	mantissa, e, _ := strings.Cut(strconv.FormatFloat(n, 'e', -1, 64), "e")
	d := strings.Replace(mantissa, ".", "", 1)
	k := len(d)
	exp, _ := strconv.Atoi(e)
	// The n of the specification, named after the point.
	point := exp + 1
	switch {
	case k <= point && point <= 21:
		return d + strings.Repeat("0", point-k)
	case 0 < point && point <= 21:
		return d[:point] + "." + d[point:]
	case -6 < point && point <= 0:
		return "0." + strings.Repeat("0", -point) + d
	}
	s := d[:1]
	if k > 1 {
		s += "." + d[1:]
	}
	if point > 0 {
		return s + "e+" + strconv.Itoa(point-1)
	}
	return s + "e-" + strconv.Itoa(1-point)
}

// radixPower returns radix to the power e.
func radixPower(radix, e int) *big.Rat {
	if e < 0 {
		return new(big.Rat).Inv(radixPower(radix, -e))
	}
	return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(int64(radix)), big.NewInt(int64(e)), nil))
}

// checkRadixDigits reports an error when s is not the shortest representation in a radix that rounds
// to n, which is positive and finite, or when another one of the same length is closer to n.
func checkRadixDigits(t *testing.T, n float64, radix int, s string) {
	ip, fp, _ := strings.Cut(s, ".")
	significand, ok := new(big.Int).SetString(ip+fp, radix)
	if !ok {
		t.Errorf("NumberToRadixString(%v, %d) = %q, which is not digits", n, radix, s)
		return
	}
	exponent := -len(fp)
	value := new(big.Rat).Mul(new(big.Rat).SetInt(significand), radixPower(radix, exponent))
	if got, _ := value.Float64(); got != n {
		t.Errorf("NumberToRadixString(%v, %d) = %q, which is %v", n, radix, s, got)
		return
	}
	if fp == "" && n <= numconv.MaxSafeInteger {
		return
	}
	for significand.Sign() > 0 {
		q, r := new(big.Int).QuoRem(significand, big.NewInt(int64(radix)), new(big.Int))
		if r.Sign() != 0 {
			break
		}
		significand = q
		exponent++
	}

	// neighbors returns the multiples of the radix to the power e that enclose n.
	exact := new(big.Rat).SetFloat64(n)
	neighbors := func(e int) [2]*big.Rat {
		unit := radixPower(radix, e)
		q := new(big.Rat).Quo(exact, unit)
		below := new(big.Int).Quo(q.Num(), q.Denom())
		above := new(big.Int).Add(below, big.NewInt(1))
		return [2]*big.Rat{
			new(big.Rat).Mul(new(big.Rat).SetInt(below), unit),
			new(big.Rat).Mul(new(big.Rat).SetInt(above), unit),
		}
	}
	// A single digit cannot be shorter.
	if len(significand.Text(radix)) > 1 {
		for _, x := range neighbors(exponent + 1) {
			if f, _ := x.Float64(); f == n && x.Sign() > 0 {
				t.Errorf("NumberToRadixString(%v, %d) = %q is not the shortest: %v is shorter", n, radix, s, x)
			}
		}
	}
	distance := func(x *big.Rat) *big.Rat {
		d := new(big.Rat).Sub(x, exact)
		return d.Abs(d)
	}
	for _, x := range neighbors(exponent) {
		if f, _ := x.Float64(); f == n && x.Sign() > 0 && distance(x).Cmp(distance(value)) < 0 {
			t.Errorf("NumberToRadixString(%v, %d) = %q is not the closest: %v is closer", n, radix, s, x)
		}
	}
}

func FuzzNumberToString(f *testing.F) {
	for _, n := range []float64{
		0, math.Copysign(0, -1), 0.1, -0.5, 1e-6, 1e-7, 123456789.125, 1e15, 1e21, 1e20, 123e18, 5e-324, 4.5e-323,
		math.MaxFloat64, math.SmallestNonzeroFloat64 * 3, 0x1p-1022, 1 << 53, 1<<53 + 2, 1 << 63, 1 << 64, 0x1p127, 0x1p128,
		math.Inf(-1), math.NaN(),
	} {
		for _, radix := range []uint8{2, 3, 10, 16, 36} {
			f.Add(math.Float64bits(n), radix)
		}
	}
	f.Fuzz(func(t *testing.T, bits uint64, radixByte uint8) {
		n := math.Float64frombits(bits)
		want := specDecimal(n)
		if got := string(numconv.NumberToString(n)); got != want {
			t.Errorf("NumberToString(%v) = %q, want %q", n, got, want)
		}

		radix := int(radixByte)
		if radix < 2 || radix > 36 {
			radix = radix%35 + 2
		}
		if radix == 10 {
			return
		}
		s := string(numconv.NumberToRadixString(n, radix))
		if n != n || math.IsInf(n, 0) || n == 0 {
			if s != want {
				t.Errorf("NumberToRadixString(%v, %d) = %q, want %q", n, radix, s, want)
			}
			return
		}
		if strings.HasPrefix(s, "-") != (n < 0) {
			t.Errorf("NumberToRadixString(%v, %d) = %q", n, radix, s)
		}
		checkRadixDigits(t, math.Abs(n), radix, strings.TrimPrefix(s, "-"))
	})
}

func FuzzIntegerConversions(f *testing.F) {
	for _, n := range []float64{0, math.Copysign(0, -1), 0.5, -0.5, math.NaN(), math.Inf(1), math.MaxFloat64, 1 << 31, 1 << 32, 1<<53 - 1, 1 << 53, 1 << 63, 0x1.fffffffffffffp62, 1 << 64, 0x1p127, 0x1.8p84, 0x1.00000002p84} {
		f.Add(math.Float64bits(n))
		f.Add(math.Float64bits(-n))
	}
	f.Fuzz(func(t *testing.T, bits uint64) {
		n := math.Float64frombits(bits)
		trunc := new(big.Int)
		finite := n == n && !math.IsInf(n, 0)
		if finite {
			new(big.Float).SetFloat64(n).Int(trunc)
		}
		want := uint32(new(big.Int).And(trunc, big.NewInt(math.MaxUint32)).Uint64())
		if got := numconv.ToUint32(n); got != want {
			t.Errorf("ToUint32(%v) = %d, want %d", n, got, want)
		}
		if got := numconv.ToInt32(n); got != int32(want) {
			t.Errorf("ToInt32(%v) = %d, want %d", n, got, int32(want))
		}

		integer := n
		switch {
		case n != n:
			integer = 0
		case finite:
			integer, _ = new(big.Float).SetInt(trunc).Float64()
		}
		if got := numconv.ToIntegerOrInfinity(n); !same(got, integer) {
			t.Errorf("ToIntegerOrInfinity(%v) = %v, want %v", n, got, integer)
		}
		length := int64(0)
		switch {
		case integer >= numconv.MaxSafeInteger:
			length = numconv.MaxSafeInteger
		case integer > 0:
			length = trunc.Int64()
		}
		if got := numconv.ToLength(n); got != length {
			t.Errorf("ToLength(%v) = %d, want %d", n, got, length)
		}
	})
}

func FuzzBigIntArithmetic(f *testing.F) {
	ones := func(n int) []byte {
		return []byte(strings.Repeat("\xff", n))
	}
	power := func(n int) []byte {
		return append([]byte{1}, make([]byte, n)...)
	}
	sizes := []int{7, 8, 16, 32, 33, 64, 200}
	for _, x := range sizes {
		for _, y := range sizes {
			f.Add(ones(x), ones(y), uint8(0))
			f.Add(power(x), ones(y), uint8(1))
			f.Add(ones(x), power(y), uint8(2))
			f.Add(power(x), power(y), uint8(3))
		}
	}
	f.Fuzz(func(t *testing.T, xb, yb []byte, config uint8) {
		if len(xb)+len(yb) > 2<<10 {
			t.Skip("too long")
		}
		// Small steps reach the recursion at small sizes.
		defer numconv.SetArithLeafWords(int(config>>2)%4 + 1)()
		x, y := new(big.Int).SetBytes(xb), new(big.Int).SetBytes(yb)
		if config&1 != 0 {
			x.Neg(x)
		}
		if config&2 != 0 {
			y.Neg(y)
		}
		poll := func() {}
		if got, want := numconv.MulBigInt(x, y, poll), new(big.Int).Mul(x, y); got.Cmp(want) != 0 {
			t.Errorf("MulBigInt(%#x, %#x) = %#x, want %#x", x, y, got, want)
		}
		if y.Sign() == 0 {
			return
		}
		q, r := new(big.Int).QuoRem(x, y, new(big.Int))
		if gq, gr := numconv.QuoRemBigInt(x, y, poll); gq.Cmp(q) != 0 || gr.Cmp(r) != 0 {
			t.Errorf("QuoRemBigInt(%#x, %#x) = %#x, %#x; want %#x, %#x", x, y, gq, gr, q, r)
		}
	})
}
