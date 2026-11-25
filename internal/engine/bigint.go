// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// MaxBigIntBits is the largest number of bits of the magnitude of a BigInt.
const MaxBigIntBits = numconv.MaxBigIntBits

// checkBigIntBits throws a RangeError when a BigInt result of n bits exceeds the limit.
func (cx *ExecutionContext) checkBigIntBits(n int64) {
	if n > cx.MaxBigIntResultBits {
		cx.ThrowRangeError("maximum BigInt size exceeded")
	}
}

const bigIntProductWords = 256 / bits.UintSize

// multiplyBigInts returns the product of two BigInts.
func (cx *ExecutionContext) multiplyBigInts(x, y *big.Int) Value {
	if x.Sign() == 0 || y.Sign() == 0 {
		return storage.BigIntFromInt64(0)
	}
	if len(x.Bits())+len(y.Bits()) > bigIntProductWords {
		// The product has the sum of the bits of x and y, or one less.
		cx.checkBigIntBits(int64(x.BitLen()) + int64(y.BitLen()) - 1)
		z := numconv.MulBigInt(x, y, cx.Poll)
		cx.checkBigIntBits(int64(z.BitLen()))
		return storage.NewBigInt(z)
	}
	o := &struct {
		bigIntObject
		magnitude [bigIntProductWords]big.Word
	}{}
	// A product needs at most the sum of its operands' word lengths.
	// The buffer belongs only to this result and is immutable once published.
	o.Integer.SetBits(o.magnitude[:0])
	o.Integer.Mul(x, y)
	if o.Integer.IsInt64() {
		return storage.BigIntFromInt64(o.Integer.Int64())
	}
	return o.InitBigInt(&o.Integer)
}

const bigIntSumWords = 256 / bits.UintSize

// addBigInts returns the sum of two BigInts.
func (cx *ExecutionContext) addBigInts(a, b Value) Value {
	x, y := a.BigInt(), b.BigInt()
	if a.O == storage.BigInt64Object && b.O == storage.BigInt64Object ||
		x.Sign() != y.Sign() || len(x.Bits()) > bigIntSumWords || len(y.Bits()) > bigIntSumWords {
		z := new(big.Int).Add(x, y)
		cx.checkBigIntBits(int64(z.BitLen()))
		return storage.NewBigInt(z)
	}
	o := &struct {
		bigIntObject
		magnitude [bigIntSumWords + 1]big.Word
	}{}
	// Addition needs one extra word for a carry. The storage belongs only to
	// this result and is never reused after the value is published.
	o.Integer.SetBits(o.magnitude[:0])
	o.Integer.Add(x, y)
	if o.Integer.IsInt64() {
		return storage.BigIntFromInt64(o.Integer.Int64())
	}
	return o.InitBigInt(&o.Integer)
}

// BigIntToString implements BigInt::toString. It throws a RangeError when the result would be
// longer than MaxStringLength.
func (cx *ExecutionContext) BigIntToString(v Value, radix int) wtf8.String {
	if i, ok := v.BigInt64(); ok {
		return wtf8.String(strconv.FormatInt(i, radix))
	}
	x := v.BigInt()
	n := x.BitLen()
	// In a power-of-two radix 2^k, x has ceil(BitLen(x)/k) digits. In any other radix, it has
	// more than (BitLen(x)-1)/log2(radix) digits.
	if bits.OnesCount(uint(radix)) == 1 {
		k := bits.TrailingZeros(uint(radix))
		var sign int
		if x.Sign() < 0 {
			sign = 1
		}
		if sign+(n+k-1)/k > MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	} else if float64(n-1)/math.Log2(float64(radix)) > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	s := numconv.FormatBigInt(x, radix, cx.Poll)
	if len(s) > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	return s
}

// bigExpStepBits is the size in bits of a power above which numconv.ExpBigInt computes it.
const bigExpStepBits = 1 << 16
