// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen_powtables.go
//go:generate go run gen_powtest.go

// Package xmath implements floating-point functions more accurate than those of package
// math.
package xmath

import (
	"math"
	"math/bits"
)

// The table layout. gen_powtables.go holds the same values.
const (
	logScale = 256
	logFirst = 181
	expN     = 128
)

// Pow returns x**y as IEEE 754 pow defines it, with an error below 0.51 units in the last
// place.
func Pow(x, y float64) float64 {
	switch {
	case y == 0:
		return 1
	case y == 1:
		return x
	case x != x || y != y:
		return math.NaN()
	case math.IsInf(y, 0):
		switch ax := math.Abs(x); {
		case ax == 1:
			return 1
		case (ax > 1) == (y > 0):
			return math.Inf(1)
		default:
			return 0
		}
	}

	// y is finite and nonzero here.
	neg := false
	if math.Signbit(x) {
		switch {
		case y != math.Trunc(y):
			if x == 0 || math.IsInf(x, 0) {
				break
			}
			return math.NaN()
		case isOdd(y):
			neg = true
		}
		x = -x
	}

	var r float64
	switch {
	case x == 0:
		if y < 0 {
			r = math.Inf(1)
		}
	case math.IsInf(x, 0):
		if y > 0 {
			r = x
		}
	case x == 1:
		r = 1
	case y == 2:
		r = x * x
	case y == -1:
		r = 1 / x
	case y == 0.5:
		r = math.Sqrt(x)
	default:
		var ok bool
		if r, ok = powExact(x, y); !ok {
			r = powPositive(x, y)
		}
	}
	if neg {
		return -r
	}
	return r
}

// isOdd reports whether the integer y is odd.
func isOdd(y float64) bool {
	if math.Abs(y) >= 1<<53 {
		return false
	}
	return int64(y)&1 != 0
}

// powExact returns x**y for a finite x > 0 and a positive integer y when x**y is a
// normal number whose significand fits in 64 bits. ok is false otherwise.
func powExact(x, y float64) (r float64, ok bool) {
	if y < 0 || y > 64 || y != math.Trunc(y) {
		return 0, false
	}
	frac, exp := math.Frexp(x)
	m := uint64(frac * (1 << 53))
	z := bits.TrailingZeros64(m)
	m >>= z
	exp -= 53 - z
	// x = m 2**exp with an odd m.
	n := int(y)
	p := uint64(1)
	for range n {
		hi, lo := bits.Mul64(p, m)
		if hi != 0 {
			return 0, false
		}
		p = lo
	}
	e := exp * n
	if bits.Len64(p)-1+e < -1022 {
		return 0, false
	}
	return math.Ldexp(float64(p), e), true
}

// powPositive returns x**y for a finite x > 0 other than 1 and a finite y.
func powPositive(x, y float64) float64 {
	lh, ll := logDD(x)
	th := float64(y * lh)
	switch {
	case th > 710:
		return math.Inf(1)
	case th < -746:
		return 0
	}
	tl := math.FMA(y, lh, -th) + y*ll
	return expDD(th, tl)
}

// The conversions float64(a * b) keep the products rounded, which the error terms below
// rely on, from being fused into later additions.

// twoSum returns a+b and the rounding error of the sum.
func twoSum(a, b float64) (s, e float64) {
	s = a + b
	bb := s - a
	e = (a - (s - bb)) + (b - bb)
	return s, e
}

// fastTwoSum returns a+b and the rounding error of the sum for |a| >= |b| or a = 0.
func fastTwoSum(a, b float64) (s, e float64) {
	s = a + b
	e = b - (s - a)
	return s, e
}

// logDD returns the natural logarithm of a finite x > 0 as a double-double with a relative
// error below 2**-68.
func logDD(x float64) (hi, lo float64) {
	bits := math.Float64bits(x)
	k := int(bits>>52) - 1023
	if k == -1023 {
		// x is subnormal.
		bits = math.Float64bits(x * (1 << 54))
		k = int(bits>>52) - 1023 - 54
	}
	// f is in [1, 2).
	f := math.Float64frombits(bits&(1<<52-1) | 1023<<52)
	if f >= math.Sqrt2 {
		f /= 2
		k++
	}
	// f is in [sqrt(1/2), sqrt(2)) and x = f 2**k.

	e := &logTable[int(f*logScale)-logFirst]

	// r = f c - 1 exactly as a double-double. |r| <= 2**-8.
	p := float64(f * e.c)
	pe := math.FMA(f, e.c, -p)
	rh, rl := twoSum(p-1, pe)

	// log(1+r) = r - r**2/2 + r**3 q(r).
	sh := float64(rh * rh)
	sl := math.FMA(rh, rh, -sh) + 2*rh*rl
	ah, al := twoSum(rh, -sh/2)
	q := 1.0 / 9
	q = q*rh - 1.0/8
	q = q*rh + 1.0/7
	q = q*rh - 1.0/6
	q = q*rh + 1.0/5
	q = q*rh - 1.0/4
	q = q*rh + 1.0/3
	q *= sh * rh

	// log(x) = k log(2) - log(c) + log(1+r).
	fk := float64(k)
	hi, lo = twoSum(fk*ln2Hi, e.logHi)
	var l float64
	hi, l = twoSum(hi, ah)
	lo += l + (fk*ln2Lo + e.logLo + al + rl - sl/2 + q)
	return fastTwoSum(hi, lo)
}

// expDD returns e**(hi+lo) rounded once for |hi| <= 746 and |lo| <= ulp(hi).
func expDD(hi, lo float64) float64 {
	// hi+lo = (n/expN) log(2) + r.
	n := math.RoundToEven(hi * invLn2N)
	rh := hi - n*ln2NHi
	rl := lo - n*ln2NLo
	rh, rl = twoSum(rh, rl)
	// |rh| <= log(2)/(2 expN) + a little.

	// e**r - 1 = rh + (rl + rh**2 p(rh)).
	p := 1.0 / 720
	p = p*rh + 1.0/120
	p = p*rh + 1.0/24
	p = p*rh + 1.0/6
	p = p*rh + 1.0/2
	pl := rl + rh*rh*p

	ni := int(n)
	j := ni & (expN - 1)
	k := (ni - j) / expN
	t := &exp2Table[j]

	// m = 2**(j/expN) e**r as a double-double in [2**(-1/(2 expN)), 2).
	m1 := float64(t.hi * rh)
	m1e := math.FMA(t.hi, rh, -m1)
	mh, ml := twoSum(t.hi, m1)
	ml += m1e + (t.lo + t.hi*pl + t.lo*rh)

	if k <= -1022 {
		mh, ml = fastTwoSum(mh, ml)
		return scaleSubnormal(mh, ml, k)
	}
	m := mh + ml
	if k > 1023 {
		return m * 2 * math.Float64frombits(uint64(k-1+1023)<<52)
	}
	return m * math.Float64frombits(uint64(k+1023)<<52)
}

// scaleSubnormal returns (hi+lo) 2**k rounded once to a multiple of 2**-1074 for
// hi in [0.5, 2), |lo| <= ulp(hi)/2, and k <= -1022.
func scaleSubnormal(hi, lo float64, k int) float64 {
	if k < -1077 {
		return 0
	}
	// hi+lo in units of 2**-1074. Both scalings are exact.
	s := math.Float64frombits(uint64(k+1074+1023) << 52)
	h := hi * s
	l := lo * s
	n := math.RoundToEven(h)
	// d is in [-0.5, 0.5], and 0.5-d and -0.5-d are exact.
	switch d := h - n; {
	case l > 0.5-d:
		n++
	case l < -0.5-d:
		n--
	}
	return n * math.Float64frombits(1)
}
