// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// bigIntObject holds a BigInt and its integer header in one allocation.
type bigIntObject struct {
	Object
	integer big.Int
}

// NewBigInt returns the BigInt i, which must not be modified afterward.
func (vm *VM) NewBigInt(i *big.Int) Value {
	if i.IsInt64() {
		return BigIntFromInt64(i.Int64())
	}
	o := &bigIntObject{
		kind: kindBigInt,
	}
	// SetBits shares the immutable magnitude without retaining i's header.
	o.integer.SetBits(i.Bits())
	if i.Sign() < 0 {
		o.integer.Neg(&o.integer)
	}
	o.setExtState(&o.integer)
	return Value{o: &o.Object}
}

// MaxBigIntBits is the largest number of bits of the magnitude of a BigInt.
const MaxBigIntBits = numconv.MaxBigIntBits

// checkBigIntBits throws a RangeError when a BigInt result of n bits exceeds the limit.
func (vm *VM) checkBigIntBits(n int64) {
	if n > vm.maxBigIntResultBits {
		vm.ThrowRangeError(vm.CurrentRealm(), "maximum BigInt size exceeded")
	}
}

const bigIntProductWords = 256 / bits.UintSize

// multiplyBigInts returns the product of two BigInts.
func (vm *VM) multiplyBigInts(x, y *big.Int) Value {
	if x.Sign() == 0 || y.Sign() == 0 {
		return BigIntFromInt64(0)
	}
	if len(x.Bits())+len(y.Bits()) > bigIntProductWords {
		// The product has the sum of the bits of x and y, or one less.
		vm.checkBigIntBits(int64(x.BitLen()) + int64(y.BitLen()) - 1)
		z := numconv.MulBigInt(x, y, vm.Poll)
		vm.checkBigIntBits(int64(z.BitLen()))
		return vm.NewBigInt(z)
	}
	o := &struct {
		bigIntObject
		magnitude [bigIntProductWords]big.Word
	}{}
	o.kind = kindBigInt
	// A product needs at most the sum of its operands' word lengths.
	// The buffer belongs only to this result and is immutable once published.
	o.integer.SetBits(o.magnitude[:0])
	o.integer.Mul(x, y)
	if o.integer.IsInt64() {
		return BigIntFromInt64(o.integer.Int64())
	}
	o.setExtState(&o.integer)
	return Value{o: &o.Object}
}

const bigIntSumWords = 256 / bits.UintSize

// addBigInts returns the sum of two BigInts.
func (vm *VM) addBigInts(a, b Value) Value {
	x, y := a.BigInt(), b.BigInt()
	if a.o == bigInt64Object && b.o == bigInt64Object ||
		x.Sign() != y.Sign() || len(x.Bits()) > bigIntSumWords || len(y.Bits()) > bigIntSumWords {
		z := new(big.Int).Add(x, y)
		vm.checkBigIntBits(int64(z.BitLen()))
		return vm.NewBigInt(z)
	}
	o := &struct {
		bigIntObject
		magnitude [bigIntSumWords + 1]big.Word
	}{}
	o.kind = kindBigInt
	// Addition needs one extra word for a carry. The storage belongs only to
	// this result and is never reused after the value is published.
	o.integer.SetBits(o.magnitude[:0])
	o.integer.Add(x, y)
	if o.integer.IsInt64() {
		return BigIntFromInt64(o.integer.Int64())
	}
	o.setExtState(&o.integer)
	return Value{o: &o.Object}
}

// maxDisplayBigIntBits is the number of bits up to which a message shows every digit of a BigInt.
const maxDisplayBigIntBits = 1 << 12

// displayBigInt describes the BigInt v for messages. A long one is shown as its leading
// hexadecimal digits and its number of bits, which take no division to find.
func displayBigInt(v Value) string {
	if i, ok := v.BigInt64(); ok {
		return strconv.FormatInt(i, 10)
	}
	x := v.BigInt()
	n := x.BitLen()
	if n <= maxDisplayBigIntBits {
		return string(numconv.FormatBigInt(x, 10, nil))
	}
	// The shift is a multiple of 4, so the digits are those of x.
	lead := new(big.Int).Rsh(new(big.Int).Abs(x), uint(n-64)&^3)
	sign := ""
	if x.Sign() < 0 {
		sign = "-"
	}
	return sign + "0x" + lead.Text(16) + "... (" + strconv.Itoa(n) + " bits)"
}

// BigIntToString implements BigInt::toString. It throws a RangeError when the result would be
// longer than MaxStringLength.
func (vm *VM) BigIntToString(v Value, radix int) wtf8.String {
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
			vm.ThrowRangeError(vm.CurrentRealm(), "invalid string length")
		}
	} else if float64(n-1)/math.Log2(float64(radix)) > MaxStringLength {
		vm.ThrowRangeError(vm.CurrentRealm(), "invalid string length")
	}
	s := numconv.FormatBigInt(x, radix, vm.Poll)
	if len(s) > MaxStringLength {
		vm.ThrowRangeError(vm.CurrentRealm(), "invalid string length")
	}
	return s
}

// add64 returns x + y, and reports whether it does not overflow.
func add64(x, y int64) (int64, bool) {
	s := x + y
	return s, (s > x) == (y > 0)
}

// sub64 returns x - y, and reports whether it does not overflow.
func sub64(x, y int64) (int64, bool) {
	d := x - y
	return d, (d < x) == (y > 0)
}

// mul64 returns x * y, and reports whether it does not overflow.
func mul64(x, y int64) (int64, bool) {
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	// The unsigned product of the two's complements exceeds the signed one by y·2⁶⁴ when x is
	// negative and by x·2⁶⁴ when y is negative.
	hi -= uint64(x>>63)&uint64(y) + uint64(y>>63)&uint64(x)
	p := int64(lo)
	return p, hi == uint64(p>>63)
}

// arithBigInt64 is arith for two BigInts that fit in an int64. It reports false when the result
// does not fit in an int64 or the operation throws, which arith then handles with big.Int.
func arithBigInt64(op bytecode.Opcode, x, y int64) (Value, bool) {
	var r int64
	ok := true
	switch op {
	case bytecode.OpSub:
		r, ok = sub64(x, y)
	case bytecode.OpMul:
		r, ok = mul64(x, y)
	case bytecode.OpDiv:
		if y == 0 || y == -1 && x == math.MinInt64 {
			return Value{}, false
		}
		r = x / y
	case bytecode.OpMod:
		if y == 0 {
			return Value{}, false
		}
		r = x % y
	case bytecode.OpExp:
		if y < 0 {
			return Value{}, false
		}
		r = 1
		for b := x; ; {
			if y&1 != 0 {
				if r, ok = mul64(r, b); !ok {
					return Value{}, false
				}
			}
			if y >>= 1; y == 0 {
				break
			}
			if b, ok = mul64(b, b); !ok {
				return Value{}, false
			}
		}
	case bytecode.OpBitOr:
		r = x | y
	case bytecode.OpBitAnd:
		r = x & y
	case bytecode.OpBitXor:
		r = x ^ y
	case bytecode.OpShl, bytecode.OpShr:
		if op == bytecode.OpShr {
			if y == math.MinInt64 {
				return Value{}, false
			}
			y = -y
		}
		switch {
		case y >= 0:
			if y >= 64 {
				return Value{}, false
			}
			r = x << y
			ok = r>>y == x
		case y <= -64:
			r = x >> 63
		default:
			r = x >> -y
		}
	default:
		return Value{}, false
	}
	if !ok {
		return Value{}, false
	}
	return BigIntFromInt64(r), true
}

// compareInt64Number compares the integer x with the number f.
func compareInt64Number(x int64, f float64) cmpResult {
	switch {
	case f != f:
		return cmpUndefined
	case f >= 1<<63:
		return cmpLess
	case f < -1<<63:
		return cmpGreater
	}
	// t lies in the range of int64 and converts exactly.
	t := math.Trunc(f)
	if c := compareInt64s(x, int64(t)); c != cmpEqual {
		return c
	}
	return compareNumbers(t, f)
}

func compareInt64s(x, y int64) cmpResult {
	switch {
	case x < y:
		return cmpLess
	case x > y:
		return cmpGreater
	}
	return cmpEqual
}

// reverse returns the result of comparing b with a, given that of comparing a with b.
func (c cmpResult) reverse() cmpResult {
	switch c {
	case cmpLess:
		return cmpGreater
	case cmpGreater:
		return cmpLess
	}
	return c
}

// bigExpStepBits is the size in bits of a power above which bigExp computes it.
const bigExpStepBits = 1 << 16

// powBitLen returns the bit length of a to the power of n, for |a| > 1 and n ≥ 0, or a number
// at most two below it. It is exact when a is a power of two.
func powBitLen(a *big.Int, n int64) int64 {
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

// bigExp sets r to a to the power of the non-negative n, checking for an interruption between
// the multiplications.
func (vm *VM) bigExp(r, a *big.Int, n int64) {
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
		vm.Poll()
		r.Set(numconv.MulBigInt(r, r, vm.Poll))
		if n>>i&1 != 0 {
			r.Set(numconv.MulBigInt(r, a, vm.Poll))
		}
	}
}
