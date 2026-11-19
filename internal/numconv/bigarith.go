// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math/big"
	"math/bits"
)

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
