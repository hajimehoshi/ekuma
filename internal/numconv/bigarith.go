// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math"
	"math/big"
	"math/bits"
)

// MaxBigIntBits is the largest number of bits of the magnitude of a BigInt.
const MaxBigIntBits = 1 << 30

// BigIntDigitsTooLarge reports whether the integer of the valid digits s in the radix, which is
// from 2 to 36, has more than MaxBigIntBits bits. poll, which may be nil, is called between the
// steps of a long check and may stop it by panicking.
func BigIntDigitsTooLarge(s string, radix int, poll func()) bool {
	s = s[skipZeros(s, 0, poll):]
	if s == "" {
		return false
	}
	if bits.OnesCount(uint(radix)) == 1 {
		k := bits.TrailingZeros(uint(radix))
		return (len(s)-1)*k+bits.Len(uint(DigitValue(s[0]))) > MaxBigIntBits
	}
	// n digits have more than (n-1) log2(radix) and at most n log2(radix) bits. The margin of
	// a bit covers the rounding of the logarithm.
	l := math.Log2(float64(radix))
	switch {
	case float64(len(s)-1)*l > MaxBigIntBits+1:
		return true
	case float64(len(s))*l < MaxBigIntBits-1:
		return false
	}
	i, _ := ParseBigInt(s, radix, poll)
	return i.BitLen() > MaxBigIntBits
}

// arithLeafWords is the size in words up to which MulBigInt multiplies, and QuoRemBigInt divides
// by, an integer in one step of math/big.
var arithLeafWords = (1 << 20) / bits.UintSize

// MulBigInt returns x × y. poll, which may be nil, is called between the steps of a long
// multiplication and may stop it by panicking.
func MulBigInt(x, y *big.Int, poll func()) *big.Int {
	if len(x.Bits()) <= arithLeafWords && len(y.Bits()) <= arithLeafWords {
		return new(big.Int).Mul(x, y)
	}
	z := mulAbs(absOf(x), absOf(y), poll)
	if x.Sign()*y.Sign() < 0 {
		z.Neg(z)
	}
	return z
}

// QuoRemBigInt returns the quotient truncated toward zero and the remainder of x / y as
// [big.Int.QuoRem] does, for y that is not zero. poll, which may be nil, is called between
// the steps of a long division and may stop it by panicking.
func QuoRemBigInt(x, y *big.Int, poll func()) (*big.Int, *big.Int) {
	if len(x.Bits()) <= 2*arithLeafWords {
		return new(big.Int).QuoRem(x, y, new(big.Int))
	}
	q, r := quoRemAbs(absOf(x), absOf(y), nil, poll)
	if x.Sign()*y.Sign() < 0 {
		q.Neg(q)
	}
	if x.Sign() < 0 {
		r.Neg(r)
	}
	return q, r
}

// PowBitLen returns the bit length of a to the power of n, for |a| > 1 and n ≥ 0, or a number
// at most two below it. It is exact when a is a power of two.
func PowBitLen(a *big.Int, n int64) int64 {
	l := int64(a.BitLen())
	if int64(a.TrailingZeroBits()) == l-1 {
		return (l-1)*n + 1
	}
	// log2|a| from its leading words is off by far less than a bit times n, so the floor of the
	// product is the exact one or one off.
	w := a.Bits()
	k := max(0, len(w)-3)
	var top float64
	for i := len(w) - 1; i >= k; i-- {
		top = top*(1<<bits.UintSize) + float64(w[i])
	}
	lg := math.Log2(top) + float64(k*bits.UintSize)
	return int64(math.Floor(float64(n) * lg))
}

// ExpBigInt sets r, which is not a, to a to the power of the non-negative n. poll, which may be
// nil, is called between the steps of a long exponentiation and may stop it by panicking.
func ExpBigInt(r, a *big.Int, n int64, poll func()) {
	if k := a.TrailingZeroBits(); a.BitLen() == int(k)+1 {
		// A power of two is a shift.
		r.Lsh(big.NewInt(1), k*uint(n))
		if a.Sign() < 0 && n&1 != 0 {
			r.Neg(r)
		}
		return
	}
	r.SetInt64(1)
	for i := bits.Len64(uint64(n)) - 1; i >= 0; i-- {
		if poll != nil {
			poll()
		}
		r.Set(MulBigInt(r, r, poll))
		if n>>i&1 != 0 {
			r.Set(MulBigInt(r, a, poll))
		}
	}
}

// absOf returns the absolute value of x, which shares the magnitude of x.
func absOf(x *big.Int) *big.Int {
	return new(big.Int).SetBits(x.Bits())
}

// lowWords returns the integer of the n lowest words of the non-negative x, which shares them.
func lowWords(x *big.Int, n int) *big.Int {
	w := x.Bits()
	return new(big.Int).SetBits(w[:min(n, len(w))])
}

// highWords returns the integer of the words of the non-negative x above the n lowest ones,
// which shares them.
func highWords(x *big.Int, n int) *big.Int {
	w := x.Bits()
	return new(big.Int).SetBits(w[min(n, len(w)):])
}

// mulAbs returns x × y for the non-negative x and y. The steps of math/big that it takes
// multiply integers of at most arithLeafWords words.
func mulAbs(x, y *big.Int, poll func()) *big.Int {
	nx, ny := len(x.Bits()), len(y.Bits())
	if nx < ny {
		x, y, nx, ny = y, x, ny, nx
	}
	if nx <= arithLeafWords {
		return new(big.Int).Mul(x, y)
	}
	if poll != nil {
		poll()
	}
	k := nx / 2
	x0, x1 := lowWords(x, k), highWords(x, k)
	shift := uint(k * bits.UintSize)
	if ny <= k {
		// x1 y 2^shift + x0 y.
		z := mulAbs(x1, y, poll)
		z.Lsh(z, shift)
		return z.Add(z, mulAbs(x0, y, poll))
	}
	// Karatsuba: z2 2^(2 shift) + (z1 - z2 - z0) 2^shift + z0.
	y0, y1 := lowWords(y, k), highWords(y, k)
	z0 := mulAbs(x0, y0, poll)
	z2 := mulAbs(x1, y1, poll)
	z1 := mulAbs(new(big.Int).Add(x0, x1), new(big.Int).Add(y0, y1), poll)
	z1.Sub(z1, z0)
	z1.Sub(z1, z2)
	z2.Lsh(z2, 2*shift)
	z1.Lsh(z1, shift)
	z2.Add(z2, z1)
	return z2.Add(z2, z0)
}

var bigOne = big.NewInt(1)

// pow2 returns 2 to the power of n.
func pow2(n uint) *big.Int {
	return new(big.Int).Lsh(bigOne, n)
}

// reciprocal returns ⌊2^(2m) / y⌋ for the positive y of m bits.
func reciprocal(y *big.Int, poll func()) *big.Int {
	m := y.BitLen()
	r := approxReciprocal(y, poll)
	t := new(big.Int).Sub(pow2(2*uint(m)), mulAbs(y, r, poll))
	for t.Sign() < 0 {
		r.Sub(r, bigOne)
		t.Add(t, y)
	}
	for t.Cmp(y) >= 0 {
		r.Add(r, bigOne)
		t.Sub(t, y)
	}
	return r
}

// approxReciprocal returns ⌊2^(2m) / y⌋ give or take a few units for the positive y of m bits.
func approxReciprocal(y *big.Int, poll func()) *big.Int {
	m := y.BitLen()
	if len(y.Bits()) <= arithLeafWords {
		return new(big.Int).Quo(pow2(2*uint(m)), y)
	}
	if poll != nil {
		poll()
	}
	// The reciprocal of the top h bits of y approximates that of y to about h bits, which a
	// step of Newton's method, r + r (2^(2m) - y r) / 2^(2m), about doubles. The error
	// 2^(2m) - y r is below 2^(2m-h+3) in magnitude, so the low bits of r and of the error
	// change the step by less than a unit.
	h := m/2 + 1
	r := approxReciprocal(new(big.Int).Rsh(y, uint(m-h)), poll)
	r.Lsh(r, uint(m-h))
	e := new(big.Int).Sub(pow2(2*uint(m)), mulAbs(y, r, poll))
	sr, se := uint(h-4), uint(m-3)
	d := MulBigInt(new(big.Int).Rsh(r, sr), e.Rsh(e, se), poll)
	return r.Add(r, d.Rsh(d, 2*uint(m)-sr-se))
}

// quoRemAbs returns the quotient and the remainder of x / y for the non-negative x and the
// positive y. recip is reciprocal(y), or nil for quoRemAbs to compute it when it needs it. It
// divides in the base 2^c by digits of c bits, so that a step of math/big
// divides an integer of about twice the size of y, or at most twice arithLeafWords words.
func quoRemAbs(x, y, recip *big.Int, poll func()) (*big.Int, *big.Int) {
	if x.CmpAbs(y) < 0 {
		return new(big.Int), new(big.Int).Set(x)
	}
	// The digits are whole words, and a digit with a remainder below y is below y 2^c.
	nw := max(len(y.Bits())-1, arithLeafWords)
	m := y.BitLen()
	if len(y.Bits()) <= arithLeafWords {
		recip = nil
	} else if recip == nil {
		recip = reciprocal(y, poll)
	}
	xw := x.Bits()
	qw := make([]big.Word, len(xw))
	r := new(big.Int)
	c := uint(nw * bits.UintSize)
	for hi := len(xw); hi > 0; {
		if poll != nil {
			poll()
		}
		lo := max(hi-nw, 0)
		if hi-lo < nw {
			// The top digit is narrower.
			r.Lsh(r, uint((hi-lo)*bits.UintSize))
		} else {
			r.Lsh(r, c)
		}
		r.Add(r, new(big.Int).SetBits(xw[lo:hi]))
		var q *big.Int
		if recip == nil {
			q, r = new(big.Int).QuoRem(r, y, new(big.Int))
		} else {
			q, r = quoRemRecip(r, y, recip, m, poll)
		}
		copy(qw[lo:hi], q.Bits())
		hi = lo
	}
	return new(big.Int).SetBits(qw), r
}

// quoRemRecip returns the quotient and the remainder of n / y for the non-negative n below
// 2^(2m), where m is the number of bits of y and recip is ⌊2^(2m) / y⌋.
func quoRemRecip(n, y, recip *big.Int, m int, poll func()) (*big.Int, *big.Int) {
	// The low m-1 bits of n change the estimate by less than a unit.
	q := mulAbs(new(big.Int).Rsh(n, uint(m-1)), recip, poll)
	q.Rsh(q, uint(m+1))
	r := new(big.Int).Sub(n, mulAbs(q, y, poll))
	for r.Sign() < 0 {
		q.Sub(q, bigOne)
		r.Add(r, y)
	}
	for r.Cmp(y) >= 0 {
		q.Add(q, bigOne)
		r.Sub(r, y)
	}
	return q, r
}
