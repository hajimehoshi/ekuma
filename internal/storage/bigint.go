// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math"
	"math/big"
	"math/bits"
	"unsafe"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// bigIntObject is the allocation behind every KindBigInt object except BigInt64Object.
type bigIntObject struct {
	plainObject
	integer big.Int
}

const (
	_ = -uint(unsafe.Offsetof(bigIntObject{}.Object))
	_ = -uint(unsafe.Sizeof(bigIntObject{}) ^ (unsafe.Sizeof(Object{}) + unsafe.Sizeof(big.Int{})))
)

func (o *bigIntObject) init(words []big.Word, negative bool) Value {
	o.Kind = KindBigInt
	o.integer.SetBits(words)
	if negative {
		o.integer.Neg(&o.integer)
	}
	return Value{O: o.plainObject.Init()}
}

func (o *Object) heapBigInt() *big.Int {
	if o == nil || o.Kind != KindBigInt || o == BigInt64Object {
		panic("storage: expected a heap BigInt")
	}
	return &(*bigIntObject)(unsafe.Pointer(o)).integer
}

// BigInt returns the BigInt v, which must not be modified.
func (v Value) BigInt() *big.Int {
	if i, ok := v.BigInt64(); ok {
		return big.NewInt(i)
	}
	return v.O.heapBigInt()
}

// NewBigInt returns the BigInt i, which must not be modified afterward.
func NewBigInt(i *big.Int) Value {
	if i.IsInt64() {
		return BigIntFromInt64(i.Int64())
	}
	return (&bigIntObject{}).init(i.Bits(), i.Sign() < 0)
}

// BigIntInlineWords is the word count up to which arithmetic uses a bounded scratch buffer.
const BigIntInlineWords = 512 / bits.UintSize

// NewBigIntFromWords copies a normalized magnitude of up to [BigIntInlineWords]+1 words.
func NewBigIntFromWords(words []big.Word, negative bool) Value {
	n := len(words)
	if n == 0 {
		return BigIntFromInt64(0)
	}
	if n <= 64/bits.UintSize {
		magnitude := uint64(words[0])
		if bits.UintSize == 32 && n == 2 {
			magnitude |= uint64(words[1]) << 32
		}
		if negative && magnitude <= 1<<63 {
			return BigIntFromInt64(int64(-magnitude))
		}
		if !negative && magnitude <= math.MaxInt64 {
			return BigIntFromInt64(int64(magnitude))
		}
	}
	var o *bigIntObject
	var magnitude []big.Word
	switch {
	case n <= 192/bits.UintSize:
		result := &struct {
			bigIntObject
			words [192 / bits.UintSize]big.Word
		}{}
		o, magnitude = &result.bigIntObject, result.words[:n:n]
	case n <= 320/bits.UintSize:
		result := &struct {
			bigIntObject
			words [320 / bits.UintSize]big.Word
		}{}
		o, magnitude = &result.bigIntObject, result.words[:n:n]
	default:
		result := &struct {
			bigIntObject
			words [BigIntInlineWords + 1]big.Word
		}{}
		o, magnitude = &result.bigIntObject, result.words[:n:n]
	}
	copy(magnitude, words)
	return o.init(magnitude, negative)
}

// Add64 returns x + y, and reports whether it does not overflow.
func Add64(x, y int64) (int64, bool) {
	s := x + y
	return s, (s > x) == (y > 0)
}

// Sub64 returns x - y, and reports whether it does not overflow.
func Sub64(x, y int64) (int64, bool) {
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
		r, ok = Sub64(x, y)
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
