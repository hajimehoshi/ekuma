// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"math/big"
	"math/bits"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// low64 returns i modulo 2^64.
func low64(i *big.Int) uint64 {
	var u uint64
	for k, w := range i.Bits() {
		if k*bits.UintSize >= 64 {
			break
		}
		u |= uint64(w) << (k * bits.UintSize)
	}
	if i.Sign() < 0 {
		// The low bits of the two's complement.
		u = -u
	}
	return u
}

func thisBigInt(cx *engine.ExecutionContext, this engine.Value, what string) engine.Value {
	if this.IsBigInt() {
		return this
	}
	return thisBigIntObject(cx, this, what)
}

// thisBigIntObject is thisBigInt for a receiver that is not a BigInt.
func thisBigIntObject(cx *engine.ExecutionContext, this engine.Value, what string) engine.Value {
	v, ok := engine.ThisBigIntValue(this)
	if !ok {
		cx.ThrowTypeError("BigInt.prototype." + what + " requires that 'this' be a BigInt")
	}
	return v
}

var (
	// The kernel converts an integer that fits in an int64, which cannot throw.
	nativeBigIntConstructor = engine.RegisterNativeWithKernels(bigIntConstructor, engine.Kernels{
		Method1:  bigIntConstructorKernel,
		Contract: shortContract,
	})
	nativeBigIntAsIntN         = engine.RegisterNative(bigIntAsIntN)
	nativeBigIntAsUintN        = engine.RegisterNative(bigIntAsUintN)
	nativeBigIntToString       = engine.RegisterNative(bigIntToString)
	nativeBigIntToLocaleString = engine.RegisterNative(bigIntToLocaleString)
	nativeBigIntValueOf        = engine.RegisterNative(bigIntValueOf)
)

// initBigInt defines BigInt.
func initBigInt(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicBigIntPrototype)
	ctor := constructor(cx, "BigInt", 1, engine.IntrinsicBigIntPrototype, nativeBigIntConstructor)
	method(cx, ctor, "asIntN", 2, nativeBigIntAsIntN)
	method(cx, ctor, "asUintN", 2, nativeBigIntAsUintN)

	method(cx, proto, "toString", 0, nativeBigIntToString)
	method(cx, proto, "toLocaleString", 0, nativeBigIntToLocaleString)
	method(cx, proto, "valueOf", 0, nativeBigIntValueOf)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("BigInt"), engine.FlagConfigurable)
}

func bigIntConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if cx.GetNewTarget() != nil {
		cx.ThrowTypeError("BigInt is not a constructor")
	}
	p := cx.ToPrimitive(arg(args, 0), engine.HintNumber)
	if p.IsNumber() {
		return numberToBigInt(cx, p.Number())
	}
	return cx.ToBigInt(p)
}

func bigIntConstructorKernel(q engine.QuietContext, this, x engine.Value) (engine.Value, bool) {
	if x.IsNumber() {
		if n := x.Number(); n == math.Trunc(n) && n >= -1<<63 && n < 1<<63 {
			return engine.BigIntFromInt64(int64(n)), true
		}
	}
	return engine.Undefined, false
}

func bigIntAsIntN(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	bits := toIndex(cx, arg(args, 0))
	x := cx.ToBigInt(arg(args, 1))
	if bits == 0 {
		return engine.BigIntFromInt64(0)
	}
	if n, ok := x.BigInt64(); ok {
		if bits >= 64 {
			return x
		}
		// Keep the low bits and extend the sign bit among them.
		s := 64 - bits
		return engine.BigIntFromInt64(n << s >> s)
	}
	i := x.BigInt()
	// i already lies in [-2^(bits-1), 2^(bits-1)).
	if int64(i.BitLen()) < bits {
		return x
	}
	if bits <= 64 {
		s := 64 - bits
		return engine.BigIntFromInt64(int64(low64(i)) << s >> s)
	}
	// The low bits of the two's complement of i.
	m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	r := new(big.Int).And(i, new(big.Int).Sub(m, big.NewInt(1)))
	if r.Bit(int(bits-1)) != 0 {
		r.Sub(r, m)
	}
	return cx.NewBigInt(r)
}

func bigIntAsUintN(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	bits := toIndex(cx, arg(args, 0))
	x := cx.ToBigInt(arg(args, 1))
	if n, ok := x.BigInt64(); ok && bits <= 64 {
		u := uint64(n)
		if bits < 64 {
			u &= 1<<bits - 1
		}
		if u <= math.MaxInt64 {
			return engine.BigIntFromInt64(int64(u))
		}
		return cx.NewBigInt(new(big.Int).SetUint64(u))
	}
	i := x.BigInt()
	if i.Sign() >= 0 && int64(i.BitLen()) <= bits {
		return x
	}
	if bits <= 64 {
		u := low64(i)
		if bits < 64 {
			u &= 1<<bits - 1
		}
		if u <= math.MaxInt64 {
			return engine.BigIntFromInt64(int64(u))
		}
		return cx.NewBigInt(new(big.Int).SetUint64(u))
	}
	if bits > engine.MaxBigIntBits {
		cx.ThrowRangeError("maximum BigInt size exceeded")
	}
	// The low bits of the two's complement of i.
	m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	return cx.NewBigInt(new(big.Int).And(i, m.Sub(m, big.NewInt(1))))
}

func bigIntToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	x := thisBigInt(cx, this, "toString")
	radix := 10.0
	if r := arg(args, 0); !r.IsUndefined() {
		radix = cx.ToIntegerOrInfinity(r)
	}
	if radix < 2 || radix > 36 {
		cx.ThrowRangeError("toString() radix must be between 2 and 36")
	}
	return cx.NewString(cx.BigIntToString(x, int(radix)))
}

func bigIntToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	x := thisBigInt(cx, this, "toLocaleString")
	return cx.NewString(cx.ToString(x))
}

func bigIntValueOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return thisBigInt(cx, this, "valueOf")
}

// numberToBigInt implements NumberToBigInt.
func numberToBigInt(cx *engine.ExecutionContext, n float64) engine.Value {
	if n != math.Trunc(n) || math.IsInf(n, 0) {
		cx.ThrowRangeError("the number " + wtf8.ToValidUTF8(numconv.NumberToString(n)) + " cannot be converted to a BigInt because it is not an integer")
	}
	if n >= -1<<63 && n < 1<<63 {
		return engine.BigIntFromInt64(int64(n))
	}
	i, _ := big.NewFloat(n).Int(nil)
	return cx.NewBigInt(i)
}

// toIndex implements ToIndex.
func toIndex(cx *engine.ExecutionContext, v engine.Value) int64 {
	i := cx.ToIntegerOrInfinity(v)
	if i < 0 || i > numconv.MaxSafeInteger {
		cx.ThrowRangeError("invalid index " + cx.DisplayString(v))
	}
	return int64(i)
}
