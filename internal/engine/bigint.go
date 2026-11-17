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
	o.ext = &o.integer
	return Value{o: &o.Object}
}

// bigIntString returns the BigInt v in the radix r. poll is as for [numconv.FormatBigInt].
func bigIntString(v Value, r int, poll func()) string {
	if i, ok := v.BigInt64(); ok {
		return strconv.FormatInt(i, r)
	}
	return numconv.FormatBigInt(v.BigInt(), r, poll)
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

// bigExp sets r to a to the power of the non-negative n, checking for an interruption between
// the multiplications.
func (vm *VM) bigExp(r, a *big.Int, n int64) {
	r.SetInt64(1)
	for i := bits.Len64(uint64(n)) - 1; i >= 0; i-- {
		vm.Poll()
		r.Mul(r, r)
		if n>>i&1 != 0 {
			r.Mul(r, a)
		}
	}
}
