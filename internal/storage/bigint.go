// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// BigIntObject holds a BigInt and its integer header in one allocation.
type BigIntObject struct {
	Object
	Integer big.Int
}

// NewBigInt returns the BigInt i, which must not be modified afterward.
func NewBigInt(i *big.Int) Value {
	if i.IsInt64() {
		return BigIntFromInt64(i.Int64())
	}
	o := &BigIntObject{}
	// SetBits shares the immutable magnitude without retaining i's header.
	o.Integer.SetBits(i.Bits())
	if i.Sign() < 0 {
		o.Integer.Neg(&o.Integer)
	}
	return o.InitBigInt(&o.Integer)
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

// Add64 returns x + y, and reports whether it does not overflow.
func Add64(x, y int64) (int64, bool) {
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

// ArithBigInt64 is arith for two BigInts that fit in an int64. It reports false when the result
// does not fit in an int64 or the operation throws, which arith then handles with big.Int.
func ArithBigInt64(op bytecode.Opcode, x, y int64) (Value, bool) {
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

// CompareInt64Number compares the integer x with the number f.
func CompareInt64Number(x int64, f float64) CmpResult {
	switch {
	case f != f:
		return CmpUndefined
	case f >= 1<<63:
		return CmpLess
	case f < -1<<63:
		return CmpGreater
	}
	// t lies in the range of int64 and converts exactly.
	t := math.Trunc(f)
	if c := CompareInt64s(x, int64(t)); c != CmpEqual {
		return c
	}
	return CompareNumbers(t, f)
}

func CompareInt64s(x, y int64) CmpResult {
	switch {
	case x < y:
		return CmpLess
	case x > y:
		return CmpGreater
	}
	return CmpEqual
}

// Reverse returns the result of comparing b with a, given that of comparing a with b.
func (c CmpResult) Reverse() CmpResult {
	switch c {
	case CmpLess:
		return CmpGreater
	case CmpGreater:
		return CmpLess
	}
	return c
}
