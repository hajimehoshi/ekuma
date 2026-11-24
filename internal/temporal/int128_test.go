// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/temporal"
)

var (
	maxInt128 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	minInt128 = new(big.Int).Not(maxInt128)
)

// int128Boundaries are the integers around which the arithmetic of an Int128 changes its path.
func int128Boundaries() []*big.Int {
	vs := []*big.Int{new(big.Int), big.NewInt(1), big.NewInt(-1), big.NewInt(10), minInt128, maxInt128}
	for _, n := range []uint{63, 64, 126} {
		for d := int64(-1); d <= 1; d++ {
			v := new(big.Int).Lsh(big.NewInt(1), n)
			v.Add(v, big.NewInt(d))
			vs = append(vs, v, new(big.Int).Neg(v))
		}
	}
	return vs
}

// int128FromParts returns the Int128 of the two's complement hi and lo, and its value.
func int128FromParts(hi int64, lo uint64) (temporal.Int128, *big.Int) {
	b := new(big.Int).Lsh(big.NewInt(hi), 64)
	b.Or(b, new(big.Int).SetUint64(lo))
	x, _ := temporal.Int128FromBig(b)
	return x, b
}

// parts returns the two's complement of b, which fits in 128 bits.
func parts(b *big.Int) (hi int64, lo uint64) {
	return new(big.Int).Rsh(b, 64).Int64(), new(big.Int).And(b, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
}

func fitsInt128(b *big.Int) bool {
	return b.Cmp(minInt128) >= 0 && b.Cmp(maxInt128) <= 0
}

func FuzzInt128(f *testing.F) {
	for _, x := range int128Boundaries() {
		hi, lo := parts(x)
		for _, y := range int128Boundaries() {
			h, l := parts(y)
			f.Add(hi, lo, h, l)
		}
	}
	f.Fuzz(func(t *testing.T, xhi int64, xlo uint64, yhi int64, ylo uint64) {
		x, bx := int128FromParts(xhi, xlo)
		y, by := int128FromParts(yhi, ylo)
		if got := x.Big(); got.Cmp(bx) != 0 {
			t.Fatalf("Big() = %v, want %v", got, bx)
		}

		// check compares got with want, which has no meaning unless it fits.
		check := func(what string, got temporal.Int128, want *big.Int) {
			t.Helper()
			if fitsInt128(want) && got.Big().Cmp(want) != 0 {
				t.Errorf("%s of %v and %v: got %v, want %v", what, bx, by, got.Big(), want)
			}
		}
		check("Add", x.Add(y), new(big.Int).Add(bx, by))
		check("Sub", x.Sub(y), new(big.Int).Sub(bx, by))
		check("Neg", x.Neg(), new(big.Int).Neg(bx))
		check("Abs", x.Abs(), new(big.Int).Abs(bx))
		if got, want := x.Sign(), bx.Sign(); got != want {
			t.Errorf("Sign of %v: got %d, want %d", bx, got, want)
		}
		if got, want := x.IsZero(), bx.Sign() == 0; got != want {
			t.Errorf("IsZero of %v: got %t", bx, got)
		}
		if got, want := x.Cmp(y), bx.Cmp(by); got != want {
			t.Errorf("Cmp of %v and %v: got %d, want %d", bx, by, got, want)
		}
		if got, want := x.String(), bx.String(); got != want {
			t.Errorf("String of %v: got %s", bx, got)
		}
		if i, ok := x.Int64(); ok != bx.IsInt64() || ok && i != bx.Int64() {
			t.Errorf("Int64 of %v: got %d, %t", bx, i, ok)
		}
		if got, _ := new(big.Rat).SetInt(bx).Float64(); x.Float64() != got {
			t.Errorf("Float64 of %v: got %v, want %v", bx, x.Float64(), got)
		}

		m := int64(ylo)
		bm := big.NewInt(m)
		check("MulInt64 by the low half", x.MulInt64(m), new(big.Int).Mul(bx, bm))
		check("Mul64 of the low halves", temporal.Mul64(int64(xlo), m), new(big.Int).Mul(big.NewInt(int64(xlo)), bm))
		if q, r := new(big.Int).QuoRem(bx, nonzero(bm), new(big.Int)); m != 0 && fitsInt128(q) {
			gq, gr := x.QuoRemInt64(m)
			check("the quotient of QuoRemInt64 by the low half", gq, q)
			check("the remainder of QuoRemInt64 by the low half", temporal.I128(gr), r)

			if r.Sign() != 0 && r.Sign() != bm.Sign() {
				q.Sub(q, big.NewInt(1))
				r.Add(r, bm)
			}
			gq, gr = x.FloorDivInt64(m)
			check("the quotient of FloorDivInt64 by the low half", gq, q)
			check("the remainder of FloorDivInt64 by the low half", temporal.I128(gr), r)
		}
		if q, r := new(big.Int).QuoRem(bx, nonzero(by), new(big.Int)); by.Sign() != 0 && fitsInt128(q) {
			gq, gr := x.QuoRem(y)
			check("the quotient of QuoRem", gq, q)
			check("the remainder of QuoRem", gr, r)
		}
	})
}

// nonzero returns b, or 1 for 0.
func nonzero(b *big.Int) *big.Int {
	if b.Sign() == 0 {
		return big.NewInt(1)
	}
	return b
}

// roundToIncrement returns x rounded to a multiple of the positive increment, with the mode
// of a name of [temporal.RoundingModeNames].
func roundToIncrement(x, increment *big.Int, mode string) *big.Int {
	floor, rest := new(big.Int).DivMod(x, increment, new(big.Int))
	if rest.Sign() == 0 {
		return x
	}
	half := rest.Lsh(rest, 1).Cmp(increment)
	positive := x.Sign() > 0
	up := half > 0
	switch mode {
	case "ceil":
		up = true
	case "floor":
		up = false
	case "expand":
		up = positive
	case "trunc":
		up = !positive
	case "halfCeil":
		up = half >= 0
	case "halfExpand":
		up = half > 0 || half == 0 && positive
	case "halfTrunc":
		up = half > 0 || half == 0 && !positive
	case "halfEven":
		up = half > 0 || half == 0 && floor.Bit(0) != 0
	}
	if up {
		floor.Add(floor, big.NewInt(1))
	}
	return floor.Mul(floor, increment)
}

func FuzzRoundToIncrement(f *testing.F) {
	for mode := range len(temporal.RoundingModeNames) {
		for _, x := range int128Boundaries() {
			hi, lo := parts(x)
			f.Add(hi, lo, int64(0), uint64(2), uint8(mode))
			f.Add(hi, lo, int64(0), uint64(math.MaxInt64), uint8(mode))
			// 2^126+1, whose remainders overflow an Int128 when doubled.
			f.Add(hi, lo, int64(1)<<62, uint64(1), uint8(mode))
		}
	}
	f.Fuzz(func(t *testing.T, xhi int64, xlo uint64, ihi int64, ilo uint64, modeIndex uint8) {
		x, bx := int128FromParts(xhi, xlo)
		increment, bi := int128FromParts(ihi, ilo)
		mode := temporal.RoundingMode(modeIndex) % temporal.RoundingMode(len(temporal.RoundingModeNames))
		name := temporal.RoundingModeNames[mode]
		if bi.Sign() > 0 {
			if want := roundToIncrement(bx, bi, name); fitsInt128(want) {
				if got := temporal.RoundInt128ToIncrement(x, increment, mode); got.Big().Cmp(want) != 0 {
					t.Errorf("RoundInt128ToIncrement(%v, %v, %s) = %v, want %v", bx, bi, name, got, want)
				}
			}
		}

		inc := int64(ilo)
		if inc <= 0 {
			return
		}
		bi = big.NewInt(inc)
		// The modes away from and toward zero round as if x were positive.
		asIfPositive := map[string]string{
			"expand":     "ceil",
			"trunc":      "floor",
			"halfExpand": "halfCeil",
			"halfTrunc":  "halfFloor",
		}[name]
		if asIfPositive == "" {
			asIfPositive = name
		}
		if want := roundToIncrement(bx, bi, asIfPositive); fitsInt128(want) {
			if got := temporal.RoundNumberToIncrementAsIfPositive(x, inc, mode); got.Big().Cmp(want) != 0 {
				t.Errorf("RoundNumberToIncrementAsIfPositive(%v, %v, %s) = %v, want %v", bx, inc, name, got, want)
			}
		}
		n := int64(xlo)
		if want := roundToIncrement(big.NewInt(n), bi, name); want.IsInt64() {
			if got := temporal.RoundNumberToIncrement(n, inc, mode); got != want.Int64() {
				t.Errorf("RoundNumberToIncrement(%v, %v, %s) = %v, want %v", n, inc, name, got, want)
			}
		}
	})
}

func FuzzTimeDurationFromComponents(f *testing.F) {
	for _, v := range []float64{0, 1, math.Copysign(0, -1), 1 << 53, -(1 << 53), 1 << 62, 1 << 63, 1 << 64, -(1 << 64), 1<<83 - 1<<30} {
		f.Add(0.0, 0.0, 0.0, 0.0, 0.0, v)
		f.Add(1.0, -2.0, 3.0, v, v, v)
	}
	f.Add(float64(1<<41), float64(1<<47), float64(1<<53-1), float64(1<<62), float64(1<<72), float64(1<<82))
	f.Fuzz(func(t *testing.T, hours, minutes, seconds, milliseconds, microseconds, nanoseconds float64) {
		units := [...]int64{temporal.NsPerHour, temporal.NsPerMinute, temporal.NsPerSecond, temporal.NsPerMillisecond, temporal.NsPerMicrosecond, 1}
		fields := [...]float64{hours, minutes, seconds, milliseconds, microseconds, nanoseconds}
		want := new(big.Int)
		for i := range fields {
			v := math.Trunc(fields[i])
			// A valid duration has integers below 2^53 seconds.
			if !(math.Abs(v)*float64(units[i]) < 1<<53*1e9) {
				t.Skip("out of range")
			}
			fields[i] = v
			b, _ := new(big.Float).SetFloat64(v).Int(nil)
			want.Add(want, b.Mul(b, big.NewInt(units[i])))
		}
		got := temporal.TimeDurationFromComponents(fields[0], fields[1], fields[2], fields[3], fields[4], fields[5])
		if got.Big().Cmp(want) != 0 {
			t.Errorf("TimeDurationFromComponents(%v) = %v, want %v", fields, got, want)
		}
	})
}
