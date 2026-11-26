// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"
	"math/big"
	"math/bits"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// BigIntObject holds a BigInt and its integer header in one allocation.
type BigIntObject struct {
	PropertyObject
	Integer big.Int
}

// InitBigInt makes o, which must be zero, the BigInt i, which must not fit in an int64.
func (o *BigIntObject) InitBigInt(i *big.Int) Value {
	o.Kind = KindBigInt
	o.Ext = i
	return Value{O: o.Init()}
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

// ArithBigInt64 is ExecutionContext.arith for two BigInts that fit in an int64. It reports false
// when the result does not fit in an int64 or the operation throws.
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
