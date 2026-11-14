// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// maxBigIntBits is the largest number of bits that BigInt.asUintN creates a value of.
const maxBigIntBits = 1 << 30

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
		i := x.Object().BigInt()
		if bits == 0 {
			return vm.NewBigInt(new(big.Int))
		}
		// i already lies in [-2^(bits-1), 2^(bits-1)).
		if int64(i.BitLen()) < bits {
			return x
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
		i := x.Object().BigInt()
		if i.Sign() >= 0 && int64(i.BitLen()) <= bits {
			return x
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
		return vm.NewString(x.Object().BigInt().Text(int(radix)))
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := thisBigInt(vm, this, "toLocaleString")
		return vm.NewString(x.Object().BigInt().String())
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return thisBigInt(vm, this, "valueOf")
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("BigInt"), engine.FlagConfigurable)
}

// numberToBigInt implements NumberToBigInt.
func numberToBigInt(vm *engine.VM, n float64) engine.Value {
	if n != math.Trunc(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError("the number " + numconv.NumberToString(n) + " cannot be converted to a BigInt because it is not an integer")
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
