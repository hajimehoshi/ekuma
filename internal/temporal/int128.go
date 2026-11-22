// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"math/big"
	"math/bits"
)

// Int128 is a signed 128-bit integer in two's complement.
// The zero value is 0.
type Int128 struct {
	hi int64
	lo uint64
}

// I128 returns x as an Int128.
func I128(x int64) Int128 {
	return Int128{
		hi: x >> 63,
		lo: uint64(x),
	}
}

// Add returns x + y.
func (x Int128) Add(y Int128) Int128 {
	lo, carry := bits.Add64(x.lo, y.lo, 0)
	return Int128{
		hi: x.hi + y.hi + int64(carry),
		lo: lo,
	}
}

// Sub returns x - y.
func (x Int128) Sub(y Int128) Int128 {
	lo, borrow := bits.Sub64(x.lo, y.lo, 0)
	return Int128{
		hi: x.hi - y.hi - int64(borrow),
		lo: lo,
	}
}

// Neg returns -x.
func (x Int128) Neg() Int128 {
	return Int128{}.Sub(x)
}

// Sign returns -1, 0 or 1 by the sign of x.
func (x Int128) Sign() int {
	switch {
	case x.hi < 0:
		return -1
	case x.hi == 0 && x.lo == 0:
		return 0
	}
	return 1
}

// IsZero reports whether x is 0.
func (x Int128) IsZero() bool {
	return x.hi == 0 && x.lo == 0
}

// Cmp returns -1, 0 or 1 when x is less than, equal to or greater than y.
func (x Int128) Cmp(y Int128) int {
	switch {
	case x.hi < y.hi:
		return -1
	case x.hi > y.hi:
		return 1
	case x.lo < y.lo:
		return -1
	case x.lo > y.lo:
		return 1
	}
	return 0
}

// Abs returns |x|.
// For -2^127, it returns x because 2^127 is outside the Int128 range.
func (x Int128) Abs() Int128 {
	if x.hi < 0 {
		return x.Neg()
	}
	return x
}

// Int64 returns x as an int64, and whether it fits.
func (x Int128) Int64() (int64, bool) {
	v := int64(x.lo)
	return v, x.hi == v>>63
}

// MulInt64 returns x × m. The product must fit in 128 bits.
func (x Int128) MulInt64(m int64) Int128 {
	neg := (x.hi < 0) != (m < 0)
	ax := x.Abs()
	am := uint64(m)
	if m < 0 {
		am = uint64(-m)
	}
	hi, lo := bits.Mul64(ax.lo, am)
	hi += uint64(ax.hi) * am
	r := Int128{
		hi: int64(hi),
		lo: lo,
	}
	if neg {
		return r.Neg()
	}
	return r
}

// Mul64 returns a × b.
func Mul64(a, b int64) Int128 {
	return I128(a).MulInt64(b)
}

// QuoRemInt64 returns the quotient and remainder of x / d truncated towards zero.
// d must be nonzero and the quotient must fit in an Int128.
func (x Int128) QuoRemInt64(d int64) (Int128, int64) {
	neg := x.hi < 0
	ax := x.Abs()
	ad := uint64(d)
	if d < 0 {
		ad = uint64(-d)
	}
	qhi := uint64(ax.hi) / ad
	rhi := uint64(ax.hi) % ad
	qlo, r := bits.Div64(rhi, ax.lo, ad)
	q := Int128{
		hi: int64(qhi),
		lo: qlo,
	}
	rem := int64(r)
	if neg != (d < 0) {
		q = q.Neg()
	}
	if neg {
		rem = -rem
	}
	return q, rem
}

// FloorDivInt64 returns floor(x / d) and the remainder x - floor(x / d) × d, which has the
// sign of d. d must be nonzero and the quotient must fit in an Int128.
func (x Int128) FloorDivInt64(d int64) (Int128, int64) {
	q, r := x.QuoRemInt64(d)
	if r != 0 && (r < 0) != (d < 0) {
		q = q.Sub(I128(1))
		r += d
	}
	return q, r
}

// QuoRem returns the quotient and remainder of x / d truncated towards zero.
// d must be nonzero and the quotient must fit in an Int128.
func (x Int128) QuoRem(d Int128) (Int128, Int128) {
	if dd, ok := d.Int64(); ok && dd != -1<<63 {
		q, r := x.QuoRemInt64(dd)
		return q, I128(r)
	}
	q, r := new(big.Int).QuoRem(x.Big(), d.Big(), new(big.Int))
	qq, _ := Int128FromBig(q)
	rr, _ := Int128FromBig(r)
	return qq, rr
}

// Big returns x as a big.Int.
func (x Int128) Big() *big.Int {
	b := new(big.Int).SetUint64(uint64(x.Abs().hi))
	b.Lsh(b, 64)
	b.Or(b, new(big.Int).SetUint64(x.Abs().lo))
	if x.hi < 0 {
		b.Neg(b)
	}
	return b
}

// Int128FromBig returns b as an Int128, and whether it fits.
func Int128FromBig(b *big.Int) (Int128, bool) {
	if n := b.BitLen(); n > 127 {
		// Only -2^127 fits with a 128-bit magnitude.
		if n == 128 && b.Sign() < 0 && b.TrailingZeroBits() == 127 {
			return Int128{
				hi: -1 << 63,
			}, true
		}
		return Int128{}, false
	}
	a := new(big.Int).Abs(b)
	lo := new(big.Int).And(a, new(big.Int).SetUint64(^uint64(0))).Uint64()
	hi := new(big.Int).Rsh(a, 64).Uint64()
	x := Int128{
		hi: int64(hi),
		lo: lo,
	}
	if b.Sign() < 0 {
		x = x.Neg()
	}
	return x, true
}

// String returns x in decimal.
func (x Int128) String() string {
	if v, ok := x.Int64(); ok {
		return big.NewInt(v).String()
	}
	return x.Big().String()
}

// Float64 returns the Number nearest to x.
func (x Int128) Float64() float64 {
	if v, ok := x.Int64(); ok && v > -1<<53 && v < 1<<53 {
		return float64(v)
	}
	f, _ := new(big.Float).SetInt(x.Big()).Float64()
	return f
}

// DivToFloat64 returns the Number nearest to the exact quotient x / d. d must not be 0.
func DivToFloat64(x, d Int128) float64 {
	f, _ := new(big.Rat).SetFrac(x.Big(), d.Big()).Float64()
	return f
}
