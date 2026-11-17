// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// maxBigIntBits is the largest number of bits that BigInt.asUintN creates a value of.
const maxBigIntBits = 1 << 30

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

// initBigInt defines BigInt.
func initBigInt(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicBigIntPrototype)
	ctor := constructor(vm, "BigInt", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if vm.GetNewTarget() != nil {
			vm.ThrowTypeError("BigInt is not a constructor")
		}
		p := vm.ToPrimitive(arg(args, 0), engine.HintNumber)
		if p.IsNumber() {
			return numberToBigInt(vm, p.Number())
		}
		return vm.ToBigInt(p)
	})
	method(vm, ctor, "asIntN", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		bits := toIndex(vm, arg(args, 0))
		x := vm.ToBigInt(arg(args, 1))
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
		m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		r := new(big.Int).Mod(i, m)
		if r.Bit(int(bits-1)) != 0 {
			r.Sub(r, m)
		}
		return vm.NewBigInt(r)
	})
	method(vm, ctor, "asUintN", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		bits := toIndex(vm, arg(args, 0))
		x := vm.ToBigInt(arg(args, 1))
		if n, ok := x.BigInt64(); ok && bits <= 64 {
			u := uint64(n)
			if bits < 64 {
				u &= 1<<bits - 1
			}
			if u <= math.MaxInt64 {
				return engine.BigIntFromInt64(int64(u))
			}
			return vm.NewBigInt(new(big.Int).SetUint64(u))
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
			return vm.NewBigInt(new(big.Int).SetUint64(u))
		}
		if bits > maxBigIntBits {
			vm.ThrowRangeError("maximum BigInt size exceeded")
		}
		m := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		return vm.NewBigInt(new(big.Int).Mod(i, m))
	})

	thisBigInt := func(vm *engine.VM, this engine.Value, what string) engine.Value {
		v, ok := engine.ThisBigIntValue(this)
		if !ok {
			vm.ThrowTypeError("BigInt.prototype." + what + " requires that 'this' be a BigInt")
		}
		return v
	}
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := thisBigInt(vm, this, "toString")
		radix := 10.0
		if r := arg(args, 0); !r.IsUndefined() {
			radix = vm.ToIntegerOrInfinity(r)
		}
		if radix < 2 || radix > 36 {
			vm.ThrowRangeError("toString() radix must be between 2 and 36")
		}
		if n, ok := x.BigInt64(); ok {
			return vm.NewString(strconv.FormatInt(n, int(radix)))
		}
		return vm.NewString(x.BigInt().Text(int(radix)))
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := thisBigInt(vm, this, "toLocaleString")
		return vm.NewString(vm.ToString(x))
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return thisBigInt(vm, this, "valueOf")
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("BigInt"), engine.FlagConfigurable)
}

// numberToBigInt implements NumberToBigInt.
func numberToBigInt(vm *engine.VM, n float64) engine.Value {
	if n != math.Trunc(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError("the number " + numconv.NumberToString(n) + " cannot be converted to a BigInt because it is not an integer")
	}
	if n >= -1<<63 && n < 1<<63 {
		return engine.BigIntFromInt64(int64(n))
	}
	i, _ := big.NewFloat(n).Int(nil)
	return vm.NewBigInt(i)
}

// toIndex implements ToIndex.
func toIndex(vm *engine.VM, v engine.Value) int64 {
	i := vm.ToIntegerOrInfinity(v)
	if i < 0 || i > numconv.MaxSafeInteger {
		vm.ThrowRangeError("invalid index " + vm.DisplayString(v))
	}
	return int64(i)
}
