// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// initNumber defines Number and Boolean.
// parseFloat and parseInt are the functions to define as properties of Number.
func initNumber(vm *engine.VM, parseFloat, parseInt *engine.Object) {
	proto := vm.Intrinsic(engine.IntrinsicNumberPrototype)
	ctor := constructor(vm, "Number", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := 0.0
		if len(args) > 0 {
			v := vm.ToNumeric(args[0])
			if v.IsNumber() {
				n = v.Number()
			} else {
				n, _ = new(big.Float).SetInt(v.Object().BigInt()).Float64()
			}
		}
		if nt := vm.GetNewTarget(); nt != nil {
			return engine.ObjectValue(vm.NewNumberObject(n, vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicNumberPrototype))))
		}
		return engine.Number(n)
	})
	constant(vm, ctor, "MAX_SAFE_INTEGER", engine.Number(numconv.MaxSafeInteger))
	constant(vm, ctor, "MIN_SAFE_INTEGER", engine.Number(-numconv.MaxSafeInteger))
	constant(vm, ctor, "MAX_VALUE", engine.Number(math.MaxFloat64))
	constant(vm, ctor, "MIN_VALUE", engine.Number(math.SmallestNonzeroFloat64))
	constant(vm, ctor, "EPSILON", engine.Number(math.Nextafter(1, 2)-1))
	constant(vm, ctor, "POSITIVE_INFINITY", engine.Number(math.Inf(1)))
	constant(vm, ctor, "NEGATIVE_INFINITY", engine.Number(math.Inf(-1)))
	constant(vm, ctor, "NaN", engine.Number(math.NaN()))
	isInteger := func(v engine.Value) bool {
		return v.IsNumber() && v.Number() == math.Trunc(v.Number()) && !math.IsInf(v.Number(), 0)
	}
	method(vm, ctor, "isInteger", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(isInteger(arg(args, 0)))
	})
	method(vm, ctor, "isSafeInteger", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(isInteger(v) && math.Abs(v.Number()) <= numconv.MaxSafeInteger)
	})
	method(vm, ctor, "isFinite", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(v.IsNumber() && v.Number() == v.Number() && !math.IsInf(v.Number(), 0))
	})
	method(vm, ctor, "isNaN", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(v.IsNumber() && v.Number() != v.Number())
	})
	vm.Define(ctor, vm.Intern("parseFloat"), engine.ObjectValue(parseFloat), engine.FlagsHidden)
	vm.Define(ctor, vm.Intern("parseInt"), engine.ObjectValue(parseInt), engine.FlagsHidden)

	thisNumber := func(vm *engine.VM, this engine.Value, what string) float64 {
		n, ok := engine.ThisNumberValue(this)
		if !ok {
			vm.ThrowTypeError("Number.prototype." + what + " requires that 'this' be a Number")
		}
		return n
	}
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisNumber(vm, this, "valueOf"))
	})
	method(vm, proto, "toString", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toString")
		radix := 10.0
		if r := arg(args, 0); !r.IsUndefined() {
			radix = vm.ToIntegerOrInfinity(r)
		}
		if radix < 2 || radix > 36 {
			vm.ThrowRangeError("toString() radix must be between 2 and 36")
		}
		if radix == 10 {
			return vm.NewString(numconv.NumberToString(n))
		}
		return vm.NewString(numconv.NumberToRadixString(n, int(radix)))
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(numconv.NumberToString(thisNumber(vm, this, "toLocaleString")))
	})
	method(vm, proto, "toFixed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toFixed")
		d := vm.ToIntegerOrInfinity(arg(args, 0))
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toFixed() digits argument must be between 0 and 100")
		}
		if n != n || math.IsInf(n, 0) || math.Abs(n) >= 1e21 {
			return vm.NewString(numconv.NumberToString(n))
		}
		s := formatHalfUp(n, 'f', int(d))
		if n == 0 {
			// -0 is formatted as 0.
			s = strings.TrimPrefix(s, "-")
		}
		return vm.NewString(s)
	})
	method(vm, proto, "toPrecision", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toPrecision")
		if arg(args, 0).IsUndefined() {
			return vm.NewString(numconv.NumberToString(n))
		}
		p := vm.ToIntegerOrInfinity(args[0])
		if n != n || math.IsInf(n, 0) {
			return vm.NewString(numconv.NumberToString(n))
		}
		if p < 1 || p > 100 {
			vm.ThrowRangeError("toPrecision() argument must be between 1 and 100")
		}
		if n == 0 {
			// -0 is formatted as 0.
			n = 0
		}
		e := 0
		if n != 0 {
			es := formatHalfUp(n, 'e', int(p)-1)
			e, _ = strconv.Atoi(es[strings.IndexByte(es, 'e')+1:])
		}
		if e < -6 || e >= int(p) {
			return vm.NewString(fixExponent(formatHalfUp(n, 'e', int(p)-1)))
		}
		return vm.NewString(formatHalfUp(n, 'f', int(p)-1-e))
	})
	method(vm, proto, "toExponential", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toExponential")
		d := vm.ToIntegerOrInfinity(arg(args, 0))
		if n != n || math.IsInf(n, 0) {
			return vm.NewString(numconv.NumberToString(n))
		}
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toExponential() argument must be between 0 and 100")
		}
		if n == 0 {
			// -0 is formatted as 0.
			n = 0
		}
		if arg(args, 0).IsUndefined() {
			return vm.NewString(fixExponent(strconv.FormatFloat(n, 'e', -1, 64)))
		}
		return vm.NewString(fixExponent(formatHalfUp(n, 'e', int(d))))
	})

	bproto := vm.Intrinsic(engine.IntrinsicBooleanPrototype)
	constructor(vm, "Boolean", 1, bproto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := engine.ToBoolean(arg(args, 0))
		if nt := vm.GetNewTarget(); nt != nil {
			return engine.ObjectValue(vm.NewBooleanObject(b, vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicBooleanPrototype))))
		}
		return engine.Bool(b)
	})
	thisBool := func(vm *engine.VM, this engine.Value, what string) bool {
		b, ok := engine.ThisBooleanValue(this)
		if !ok {
			vm.ThrowTypeError("Boolean.prototype." + what + " requires that 'this' be a Boolean")
		}
		return b
	}
	method(vm, bproto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisBool(vm, this, "valueOf"))
	})
	method(vm, bproto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if thisBool(vm, this, "toString") {
			return vm.NewString("true")
		}
		return vm.NewString("false")
	})
}

// formatHalfUp formats n like [strconv.FormatFloat] with the format fmt ('e' or 'f') and the
// precision prec (non-negative), except that an exact tie rounds away from zero.
func formatHalfUp(n float64, fmt byte, prec int) string {
	// A tie shows as a trailing 5 with one more digit, and that string is then exactly n.
	t := strconv.FormatFloat(n, fmt, prec+1, 64)
	mant, _, _ := strings.Cut(t, "e")
	if mant[len(mant)-1] == '5' {
		if r, ok := new(big.Rat).SetString(t); ok && r.Cmp(new(big.Rat).SetFloat64(n)) == 0 {
			// The next float away from zero is past the tie and closer to it than to the next tie,
			// so it rounds to the larger magnitude.
			n = math.Nextafter(n, math.Copysign(math.Inf(1), n))
		}
	}
	return strconv.FormatFloat(n, fmt, prec, 64)
}

// fixExponent rewrites the exponent that Go formats with at least two digits to the JavaScript form.
func fixExponent(s string) string {
	mant, exp, ok := strings.Cut(s, "e")
	if !ok {
		return s
	}
	sign := exp[:1]
	digits := strings.TrimLeft(exp[1:], "0")
	if digits == "" {
		digits = "0"
	}
	return mant + "e" + sign + digits
}

func initSymbol(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicSymbolPrototype)
	ctor := constructor(vm, "Symbol", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if vm.GetNewTarget() != nil {
			vm.ThrowTypeError("Symbol is not a constructor")
		}
		d := arg(args, 0)
		if d.IsUndefined() {
			return engine.ObjectValue(vm.NewSymbol("", false))
		}
		return engine.ObjectValue(vm.NewSymbol(vm.ToString(d), true))
	})
	for sym, name := range [...]string{
		engine.SymbolIterator: "iterator", engine.SymbolAsyncIterator: "asyncIterator", engine.SymbolHasInstance: "hasInstance",
		engine.SymbolToPrimitive: "toPrimitive", engine.SymbolToStringTag: "toStringTag", engine.SymbolSpecies: "species",
		engine.SymbolIsConcatSpreadable: "isConcatSpreadable", engine.SymbolUnscopables: "unscopables",
		engine.SymbolMatch: "match", engine.SymbolMatchAll: "matchAll", engine.SymbolReplace: "replace",
		engine.SymbolSearch: "search", engine.SymbolSplit: "split",
		engine.SymbolDispose: "dispose", engine.SymbolAsyncDispose: "asyncDispose",
	} {
		constant(vm, ctor, name, engine.ObjectValue(vm.WellKnownSymbol(engine.WellKnownSymbol(sym))))
	}
	method(vm, ctor, "for", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(vm.RegisteredSymbol(vm.ToString(arg(args, 0))))
	})
	method(vm, ctor, "keyFor", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := arg(args, 0)
		if !s.IsSymbol() {
			vm.ThrowTypeError(vm.DisplayString(s) + " is not a symbol")
		}
		key, ok := vm.KeyForSymbol(s.Object())
		if !ok {
			return engine.Undefined
		}
		return vm.NewString(key)
	})
	thisSymbol := func(vm *engine.VM, this engine.Value, what string) *engine.Object {
		s, ok := engine.ThisSymbolValue(this)
		if !ok {
			vm.ThrowTypeError("Symbol.prototype." + what + " requires that 'this' be a Symbol")
		}
		return s
	}
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		desc, _ := engine.SymbolDescription(thisSymbol(vm, this, "toString"))
		return vm.NewString("Symbol(" + desc + ")")
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(thisSymbol(vm, this, "valueOf"))
	})
	getter(vm, proto, vm.Intern("description"), "description", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		desc, ok := engine.SymbolDescription(thisSymbol(vm, this, "description"))
		if !ok {
			return engine.Undefined
		}
		return vm.NewString(desc)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Symbol"), engine.FlagConfigurable)
	fn := vm.NewNative("[Symbol.toPrimitive]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(thisSymbol(vm, this, "[Symbol.toPrimitive]"))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToPrimitive), engine.ObjectValue(fn), engine.FlagConfigurable)
}

func initMath(vm *engine.VM) {
	m := vm.NewOrdinaryObject()
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Math"), engine.ObjectValue(m), engine.FlagsHidden)
	vm.Define(m, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Math"), engine.FlagConfigurable)
	for name, v := range map[string]float64{
		"E":       math.E,
		"LN10":    math.Ln10,
		"LN2":     math.Ln2,
		"LOG10E":  math.Log10E,
		"LOG2E":   math.Log2E,
		"PI":      math.Pi,
		"SQRT1_2": math.Sqrt2 / 2,
		"SQRT2":   math.Sqrt2,
	} {
		constant(vm, m, name, engine.Number(v))
	}
	unary := func(name string, f func(float64) float64) {
		method(vm, m, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if len(args) > 0 && args[0].IsNumber() {
				return engine.Number(f(args[0].Number()))
			}
			return engine.Number(f(vm.ToNumber(arg(args, 0))))
		})
	}
	unary("abs", math.Abs)
	unary("acos", math.Acos)
	unary("acosh", math.Acosh)
	unary("asin", math.Asin)
	unary("asinh", math.Asinh)
	unary("atan", math.Atan)
	unary("atanh", math.Atanh)
	unary("cbrt", math.Cbrt)
	unary("ceil", math.Ceil)
	unary("cos", math.Cos)
	unary("cosh", math.Cosh)
	unary("exp", math.Exp)
	unary("expm1", math.Expm1)
	unary("floor", math.Floor)
	unary("fround", func(x float64) float64 {
		return float64(float32(x))
	})
	unary("f16round", f16round)
	unary("log", math.Log)
	unary("log1p", math.Log1p)
	unary("log10", math.Log10)
	unary("log2", math.Log2)
	unary("round", func(x float64) float64 {
		if x != x || math.IsInf(x, 0) || x == 0 {
			return x
		}
		if x < 0 && x >= -0.5 {
			return math.Copysign(0, -1)
		}
		r := math.Floor(x)
		if x-r >= 0.5 {
			r++
		}
		return r
	})
	unary("sign", func(x float64) float64 {
		switch {
		case x > 0:
			return 1
		case x < 0:
			return -1
		}
		return x
	})
	unary("sin", math.Sin)
	unary("sinh", math.Sinh)
	unary("sqrt", math.Sqrt)
	unary("tan", math.Tan)
	unary("tanh", math.Tanh)
	unary("trunc", math.Trunc)
	unary("clz32", func(x float64) float64 {
		n := numconv.ToUint32(x)
		c := 0
		for ; c < 32 && n&(1<<31) == 0; c++ {
			n <<= 1
		}
		return float64(c)
	})
	method(vm, m, "atan2", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		y := vm.ToNumber(arg(args, 0))
		return engine.Number(math.Atan2(y, vm.ToNumber(arg(args, 1))))
	})
	method(vm, m, "pow", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := vm.ToNumber(arg(args, 0))
		return engine.Number(engine.NumberPow(x, vm.ToNumber(arg(args, 1))))
	})
	method(vm, m, "imul", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := vm.ToInt32(arg(args, 0))
		return engine.Number(float64(x * vm.ToInt32(arg(args, 1))))
	})
	method(vm, m, "random", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(vm.RandomNumber())
	})
	minmax := func(name string, start float64, pick func(a, b float64) bool) {
		method(vm, m, name, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			r := start
			nan := false
			for _, a := range args {
				n := vm.ToNumber(a)
				if n != n {
					nan = true
				}
				// The zeros are ordered by sign.
				if pick(n, r) || (n == 0 && r == 0 && math.Signbit(n) != math.Signbit(r) && math.Signbit(n) == (start > 0)) {
					r = n
				}
			}
			if nan {
				return engine.Number(math.NaN())
			}
			return engine.Number(r)
		})
	}
	minmax("max", math.Inf(-1), func(a, b float64) bool {
		return a > b
	})
	minmax("min", math.Inf(1), func(a, b float64) bool {
		return a < b
	})
	method(vm, m, "sumPrecise", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(sumPrecise(vm, arg(args, 0)))
	})
	method(vm, m, "hypot", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nums := make([]float64, len(args))
		for i, a := range args {
			nums[i] = vm.ToNumber(a)
		}
		r := 0.0
		nan := false
		for _, n := range nums {
			if math.IsInf(n, 0) {
				return engine.Number(math.Inf(1))
			}
			if n != n {
				nan = true
			}
			r = math.Hypot(r, n)
		}
		if nan {
			return engine.Number(math.NaN())
		}
		return engine.Number(r)
	})
}

func initIterators(vm *engine.VM) {
	symMethod(vm, vm.Intrinsic(engine.IntrinsicIteratorPrototype), vm.WellKnownSymbol(engine.SymbolIterator), "[Symbol.iterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	symMethod(vm, vm.Intrinsic(engine.IntrinsicAsyncIteratorPrototype), vm.WellKnownSymbol(engine.SymbolAsyncIterator), "[Symbol.asyncIterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})

	arrayIterNext := iteratorNext(vm, vm.Intrinsic(engine.IntrinsicArrayIteratorPrototype), func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		it, _ := engine.IteratorState(this).(*arrayIterator)
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not an array iterator")
		}
		if it.obj == nil {
			return engine.Undefined, false
		}
		var n int64
		if t := engine.TypedArrayData(engine.ObjectValue(it.obj)); t != nil {
			l, ok := t.Length()
			if !ok {
				vm.ThrowTypeError("cannot iterate a detached or out of bounds typed array")
			}
			n = int64(l)
		} else {
			n = vm.LengthOfArrayLike(it.obj)
		}
		i := it.pos
		if i >= n {
			it.obj = nil
			return engine.Undefined, false
		}
		it.pos++
		switch it.kind {
		case iterKeys:
			return engine.Number(float64(i)), true
		case iterValues:
			return getAt(vm, it.obj, i), true
		}
		return engine.ObjectValue(vm.NewArrayPair(engine.Number(float64(i)), getAt(vm, it.obj, i))), true
	})
	vm.SetIntrinsic(engine.IntrinsicArrayIteratorPrototypeNext, arrayIterNext)
	vm.Define(vm.Intrinsic(engine.IntrinsicArrayIteratorPrototype), vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Array Iterator"), engine.FlagConfigurable)

	iteratorNext(vm, vm.Intrinsic(engine.IntrinsicStringIteratorPrototype), func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		it, _ := engine.IteratorState(this).(*stringIterator)
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not a string iterator")
		}
		if it.pos >= len(it.str) {
			return engine.Undefined, false
		}
		_, size := wtf8.DecodeRune(it.str[it.pos:])
		s := it.str[it.pos : it.pos+size]
		it.pos += size
		return vm.NewString(s), true
	})
	vm.Define(vm.Intrinsic(engine.IntrinsicStringIteratorPrototype), vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("String Iterator"), engine.FlagConfigurable)

	gp := vm.Intrinsic(engine.IntrinsicGeneratorPrototype)
	resume := func(name string, mode engine.ResumeMode) *engine.Object {
		return method(vm, gp, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			r, ok := vm.ResumeGenerator(this, mode, arg(args, 0))
			if !ok {
				vm.ThrowTypeError(name + " method called on an object that is not a generator")
			}
			return r
		})
	}
	vm.SetIntrinsic(engine.IntrinsicGeneratorPrototypeNext, resume("next", engine.ResumeNext))
	resume("return", engine.ResumeReturn)
	resume("throw", engine.ResumeThrow)
	vm.Define(gp, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Generator"), engine.FlagConfigurable)

	// The prototype chain of generator functions.
	gf := vm.Intrinsic(engine.IntrinsicGeneratorFunctionPrototype)
	vm.Define(gf, vm.Key(engine.KeyPrototype), engine.ObjectValue(gp), engine.FlagConfigurable)
	vm.Define(gp, vm.Key(engine.KeyConstructor), engine.ObjectValue(gf), engine.FlagConfigurable)
	functionKindConstructor(vm, "GeneratorFunction", engine.GeneratorFunction, gf)
}

// f16round returns x rounded to the nearest binary16 value, ties to even.
func f16round(x float64) float64 {
	if x != x || math.IsInf(x, 0) || x == 0 {
		return x
	}
	a := math.Abs(x)
	// 65520 is halfway between the largest binary16 value and 2^16, and rounds to infinity.
	if a >= 65520 {
		return math.Copysign(math.Inf(1), x)
	}
	_, exp := math.Frexp(a)
	// The unit in the last place of binary16 at a, with the subnormals below 2^-14.
	e := max(exp-1, -14)
	ulp := math.Ldexp(1, e-10)
	return math.Copysign(math.RoundToEven(a/ulp)*ulp, x)
}

// sumPreciseBits is the precision that holds the exact sum of up to 2^53 finite numbers.
const sumPreciseBits = 1074 + 1024 + 53 + 1

// sumPrecise implements Math.sumPrecise.
func sumPrecise(vm *engine.VM, items engine.Value) float64 {
	vm.RequireObjectCoercible(items, "Math.sumPrecise")
	it, next := vm.GetIterator(items)
	const (
		minusZero = iota
		finite
		plusInf
		minusInf
		notANumber
	)
	state := minusZero
	sum := new(big.Float).SetPrec(sumPreciseBits)
	var x big.Float
	count := 0.0
	vm.IterateRecord(it, next, func(v engine.Value) bool {
		count++
		if count >= 1<<53 {
			vm.ThrowRangeError("Math.sumPrecise: too many values")
		}
		if !v.IsNumber() {
			vm.ThrowTypeError("Math.sumPrecise: " + vm.DisplayString(v) + " is not a number")
		}
		n := v.Number()
		switch {
		case state == notANumber:
		case n != n:
			state = notANumber
		case math.IsInf(n, 1):
			if state == minusInf {
				state = notANumber
			} else {
				state = plusInf
			}
		case math.IsInf(n, -1):
			if state == plusInf {
				state = notANumber
			} else {
				state = minusInf
			}
		case n == 0 && math.Signbit(n):
		case state == minusZero || state == finite:
			state = finite
			sum.Add(sum, x.SetFloat64(n))
		}
		return true
	})
	switch state {
	case notANumber:
		return math.NaN()
	case plusInf:
		return math.Inf(1)
	case minusInf:
		return math.Inf(-1)
	case minusZero:
		return math.Copysign(0, -1)
	}
	f, _ := sum.Float64()
	return f
}
