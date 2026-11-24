// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin_test

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// pow10 returns 10 to the power e.
func pow10(e int) *big.Rat {
	if e < 0 {
		return new(big.Rat).Inv(pow10(-e))
	}
	return new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(e)), nil))
}

// roundHalfUp returns the integer nearest to the non-negative x, the larger one for a tie.
func roundHalfUp(x *big.Rat) *big.Int {
	y := new(big.Rat).Add(x, big.NewRat(1, 2))
	return new(big.Int).Quo(y.Num(), y.Denom())
}

// significant returns the positive x rounded to p digits, and the exponent of the first one.
func significant(x *big.Rat, p int) (string, int) {
	// The bit lengths estimate the exponent within one, and the digits correct the estimate.
	e := int(math.Floor(float64(x.Num().BitLen()-x.Denom().BitLen())*math.Log10(2))) - 1
	for x.Cmp(pow10(e+1)) >= 0 {
		e++
	}
	d := roundHalfUp(new(big.Rat).Mul(x, pow10(p-1-e))).String()
	if len(d) > p {
		d = d[:p]
		e++
	}
	return d, e
}

// point inserts a decimal point into the digits d after the first n ones.
func point(d string, n int) string {
	if n == len(d) {
		return d
	}
	return d[:n] + "." + d[n:]
}

// exponential lays out the digits d, of which the first one has the exponent e.
func exponential(d string, e int) string {
	if e < 0 {
		return point(d, 1) + "e-" + strconv.Itoa(-e)
	}
	return point(d, 1) + "e+" + strconv.Itoa(e)
}

func FuzzNumberWithPrecision(f *testing.F) {
	for _, n := range []float64{
		0, math.Copysign(0, -1), 0.5, 1.5, 2.5, -1.375, 255.0625, 9.99, -0.0001, 1e-6, 1e-7, 0x1p-17, 0x1p-23, 0x1p-64, 0x1p-65,
		99999.5, 123456789012.1, 12345678.1, 999999999999999.9, 1e20, 1e21, 1<<53 - 1, 1 << 53, 0x1.fffffffffffffp62, 1 << 63,
		0x1.fffffffffffffp63, 1 << 64, 0x1p127, 5e-324, 0x1p-1022, math.MaxFloat64, math.Inf(1), math.NaN(),
	} {
		for _, digits := range []uint8{0, 1, 2, 17, 18, 19, 20, 21, 100} {
			f.Add(math.Float64bits(n), digits)
		}
	}

	cx, _ := engine.NewExecutionContext(nil)
	builtin.Install(cx)
	proto := cx.Intrinsic(engine.IntrinsicNumberPrototype)
	f.Fuzz(func(t *testing.T, bits uint64, digitsByte uint8) {
		n := math.Float64frombits(bits)
		digits := int(digitsByte) % 101
		check := func(method string, arg int, want string) {
			t.Helper()
			var got string
			cx.Run(func() {
				m := cx.Get(proto, cx.KeyOfString(wtf8.FromValidUTF8(method)))
				got = string(cx.Call(m.Object(), engine.Number(n), engine.Number(float64(arg))).Str())
			})
			if got != want {
				t.Errorf("(%v).%s(%d) = %q, want %q", n, method, arg, got, want)
			}
		}

		if n != n || math.IsInf(n, 0) {
			want := string(numconv.NumberToString(n))
			check("toFixed", digits, want)
			check("toExponential", digits, want)
			check("toPrecision", max(digits, 1), want)
			return
		}
		sign := ""
		if n < 0 {
			sign = "-"
		}
		x := new(big.Rat).SetFloat64(math.Abs(n))

		fixed := string(numconv.NumberToString(n))
		if x.Cmp(pow10(21)) < 0 {
			d := roundHalfUp(new(big.Rat).Mul(x, pow10(digits))).String()
			d = strings.Repeat("0", max(digits+1-len(d), 0)) + d
			fixed = sign + point(d, len(d)-digits)
		}
		check("toFixed", digits, fixed)

		// round returns the p digits that the other two methods lay out, and the exponent.
		round := func(p int) (string, int) {
			if n == 0 {
				return strings.Repeat("0", p), 0
			}
			return significant(x, p)
		}
		d, e := round(digits + 1)
		check("toExponential", digits, sign+exponential(d, e))

		p := max(digits, 1)
		d, e = round(p)
		want := ""
		switch {
		case e < -6 || e >= p:
			want = exponential(d, e)
		case e < 0:
			want = "0." + strings.Repeat("0", -e-1) + d
		default:
			want = point(d, e+1)
		}
		check("toPrecision", p, sign+want)
	})
}
