// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"math/big"
	"math/bits"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func isIntegralNumber(v storage.Value) bool {
	return v.IsNumber() && v.Number() == math.Trunc(v.Number()) && !math.IsInf(v.Number(), 0)
}

func thisNumber(cx *engine.ExecutionContext, this storage.Value, what string) float64 {
	if this.IsNumber() {
		return this.Number()
	}
	return thisNumberObject(cx, this, what)
}

// thisNumberObject is thisNumber for a receiver that is not a number.
func thisNumberObject(cx *engine.ExecutionContext, this storage.Value, what string) float64 {
	n, ok := storage.ThisNumberValue(this)
	if !ok {
		cx.ThrowTypeError("Number.prototype." + what + " requires that 'this' be a Number")
	}
	return n
}

func requireThisBoolean(cx *engine.ExecutionContext, ok bool, what string) {
	if !ok {
		cx.ThrowTypeError("Boolean.prototype." + what + " requires that 'this' be a Boolean")
	}
}

var (
	nativeNumberConstructor   = engine.RegisterNative(numberConstructor)
	nativeNumberIsInteger     = engine.RegisterNative(numberIsInteger)
	nativeNumberIsSafeInteger = engine.RegisterNative(numberIsSafeInteger)
	nativeNumberIsFinite      = engine.RegisterNative(numberIsFinite)
	nativeNumberIsNaN         = engine.RegisterNative(numberIsNaN)
	nativeNumberValueOf       = engine.RegisterNative(numberValueOf)
	nativeNumberToString      = engine.RegisterNativeWithKernels(numberToString, engine.Kernels{
		Method0:  quiet.NumberToStringKernel0,
		Method1:  quiet.NumberToStringKernel1,
		Contract: shortContract,
	})
	nativeNumberToLocaleString = engine.RegisterNative(numberToLocaleString)
	nativeNumberToFixed        = engine.RegisterNativeWithKernels(numberToFixed, engine.Kernels{
		Method0:  quiet.NumberToFixedKernel0,
		Method1:  quiet.NumberToFixedKernel1,
		Contract: shortContract,
	})
	nativeNumberToPrecision = engine.RegisterNativeWithKernels(numberToPrecision, engine.Kernels{
		Method0:  quiet.NumberToStringKernel0,
		Method1:  quiet.NumberToPrecisionKernel1,
		Contract: shortContract,
	})
	nativeNumberToExponential = engine.RegisterNativeWithKernels(numberToExponential, engine.Kernels{
		Method0:  quiet.NumberToExponentialKernel0,
		Method1:  quiet.NumberToExponentialKernel1,
		Contract: shortContract,
	})
	nativeBooleanConstructor = engine.RegisterNative(booleanConstructor)
	nativeBooleanValueOf     = engine.RegisterNative(booleanValueOf)
	nativeBooleanToString    = engine.RegisterNative(booleanToString)
)

// initNumber defines Number and Boolean.
// parseFloat and parseInt are the functions to define as properties of Number.
func initNumber(cx *engine.ExecutionContext, parseFloat, parseInt *storage.Object) {
	proto := cx.Intrinsic(storage.IntrinsicNumberPrototype)
	ctor := constructor(cx, "Number", 1, storage.IntrinsicNumberPrototype, nativeNumberConstructor)
	constant(cx, ctor, "MAX_SAFE_INTEGER", storage.Number(numconv.MaxSafeInteger))
	constant(cx, ctor, "MIN_SAFE_INTEGER", storage.Number(-numconv.MaxSafeInteger))
	constant(cx, ctor, "MAX_VALUE", storage.Number(math.MaxFloat64))
	constant(cx, ctor, "MIN_VALUE", storage.Number(math.SmallestNonzeroFloat64))
	constant(cx, ctor, "EPSILON", storage.Number(math.Nextafter(1, 2)-1))
	constant(cx, ctor, "POSITIVE_INFINITY", storage.Number(math.Inf(1)))
	constant(cx, ctor, "NEGATIVE_INFINITY", storage.Number(math.Inf(-1)))
	constant(cx, ctor, "NaN", storage.Number(math.NaN()))
	method(cx, ctor, "isInteger", 1, nativeNumberIsInteger)
	method(cx, ctor, "isSafeInteger", 1, nativeNumberIsSafeInteger)
	method(cx, ctor, "isFinite", 1, nativeNumberIsFinite)
	method(cx, ctor, "isNaN", 1, nativeNumberIsNaN)
	cx.Define(ctor, cx.Intern("parseFloat"), storage.ObjectValue(parseFloat), storage.FlagsHidden)
	cx.Define(ctor, cx.Intern("parseInt"), storage.ObjectValue(parseInt), storage.FlagsHidden)

	method(cx, proto, "valueOf", 0, nativeNumberValueOf)
	method(cx, proto, "toString", 1, nativeNumberToString)
	method(cx, proto, "toLocaleString", 0, nativeNumberToLocaleString)
	method(cx, proto, "toFixed", 1, nativeNumberToFixed)
	method(cx, proto, "toPrecision", 1, nativeNumberToPrecision)
	method(cx, proto, "toExponential", 1, nativeNumberToExponential)

	bproto := cx.Intrinsic(storage.IntrinsicBooleanPrototype)
	constructor(cx, "Boolean", 1, storage.IntrinsicBooleanPrototype, nativeBooleanConstructor)
	method(cx, bproto, "valueOf", 0, nativeBooleanValueOf)
	method(cx, bproto, "toString", 0, nativeBooleanToString)
}

func numberConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n := 0.0
	if len(args) > 0 {
		v := cx.ToNumeric(args[0])
		if v.IsNumber() {
			n = v.Number()
		} else if i, ok := v.BigInt64(); ok {
			n = float64(i)
		} else {
			n, _ = new(big.Float).SetInt(v.BigInt()).Float64()
		}
	}
	if nt := cx.GetNewTarget(); nt != nil {
		return storage.ObjectValue(cx.NewNumberObject(n, cx.GetPrototypeFromConstructor(nt, storage.IntrinsicNumberPrototype)))
	}
	return storage.Number(n)
}

func numberIsInteger(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(isIntegralNumber(arg(args, 0)))
}

func numberIsSafeInteger(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	v := arg(args, 0)
	return storage.Bool(isIntegralNumber(v) && math.Abs(v.Number()) <= numconv.MaxSafeInteger)
}

func numberIsFinite(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	v := arg(args, 0)
	return storage.Bool(v.IsNumber() && v.Number() == v.Number() && !math.IsInf(v.Number(), 0))
}

func numberIsNaN(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	v := arg(args, 0)
	return storage.Bool(v.IsNumber() && v.Number() != v.Number())
}

func numberValueOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(thisNumber(cx, this, "valueOf"))
}

func numberToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n := thisNumber(cx, this, "toString")
	radix := 10.0
	if r := arg(args, 0); !r.IsUndefined() {
		radix = cx.ToIntegerOrInfinity(r)
	}
	if radix < 2 || radix > 36 {
		cx.ThrowRangeError("toString() radix must be between 2 and 36")
	}
	if radix == 10 {
		return cx.ToStringValue(storage.Number(n))
	}
	return cx.NewString(numconv.NumberToRadixString(n, int(radix)))
}

func numberToLocaleString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ToStringValue(storage.Number(thisNumber(cx, this, "toLocaleString")))
}

func numberToFixed(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n := thisNumber(cx, this, "toFixed")
	d := cx.ToIntegerOrInfinity(arg(args, 0))
	if d < 0 || d > 100 {
		cx.ThrowRangeError("toFixed() digits argument must be between 0 and 100")
	}
	if n != n || math.IsInf(n, 0) || math.Abs(n) >= 1e21 {
		return cx.ToStringValue(storage.Number(n))
	}
	var buf [numconv.NumberFormatBufSize]byte
	return cx.NewASCIIStringBytes(numconv.AppendFixed(buf[:0], n, int(d)))
}

func numberToPrecision(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n := thisNumber(cx, this, "toPrecision")
	if arg(args, 0).IsUndefined() {
		return cx.ToStringValue(storage.Number(n))
	}
	p := cx.ToIntegerOrInfinity(args[0])
	if n != n || math.IsInf(n, 0) {
		return cx.ToStringValue(storage.Number(n))
	}
	if p < 1 || p > 100 {
		cx.ThrowRangeError("toPrecision() argument must be between 1 and 100")
	}
	var buf [numconv.NumberFormatBufSize]byte
	return cx.NewASCIIStringBytes(numconv.AppendPrecision(buf[:0], n, int(p)))
}

func numberToExponential(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n := thisNumber(cx, this, "toExponential")
	d := cx.ToIntegerOrInfinity(arg(args, 0))
	if n != n || math.IsInf(n, 0) {
		return cx.ToStringValue(storage.Number(n))
	}
	if d < 0 || d > 100 {
		cx.ThrowRangeError("toExponential() argument must be between 0 and 100")
	}
	var buf [numconv.NumberFormatBufSize]byte
	if arg(args, 0).IsUndefined() {
		return cx.NewASCIIStringBytes(numconv.AppendExponentialAuto(buf[:0], n))
	}
	return cx.NewASCIIStringBytes(numconv.AppendExponential(buf[:0], n, int(d)))
}

func booleanConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := storage.ToBoolean(arg(args, 0))
	if nt := cx.GetNewTarget(); nt != nil {
		return storage.ObjectValue(cx.NewBooleanObject(b, cx.GetPrototypeFromConstructor(nt, storage.IntrinsicBooleanPrototype)))
	}
	return storage.Bool(b)
}

func booleanValueOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b, ok := storage.ThisBooleanValue(this)
	requireThisBoolean(cx, ok, "valueOf")
	return storage.Bool(b)
}

func booleanToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b, ok := storage.ThisBooleanValue(this)
	requireThisBoolean(cx, ok, "toString")
	if b {
		return cx.NewString("true")
	}
	return cx.NewString("false")
}

func requireThisSymbol(cx *engine.ExecutionContext, ok bool, what string) {
	if !ok {
		cx.ThrowTypeError("Symbol.prototype." + what + " requires that 'this' be a Symbol")
	}
}

var (
	nativeSymbolConstructor = engine.RegisterNative(symbolConstructor)
	nativeSymbolFor         = engine.RegisterNative(symbolFor)
	nativeSymbolKeyFor      = engine.RegisterNative(symbolKeyFor)
	nativeSymbolToString    = engine.RegisterNative(symbolToString)
	nativeSymbolValueOf     = engine.RegisterNative(symbolValueOf)
	nativeSymbolDescription = engine.RegisterNative(symbolDescription)
	nativeSymbolToPrimitive = engine.RegisterNative(symbolToPrimitive)
)

func initSymbol(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(storage.IntrinsicSymbolPrototype)
	ctor := constructor(cx, "Symbol", 0, storage.IntrinsicSymbolPrototype, nativeSymbolConstructor)
	for sym, name := range [...]string{
		storage.SymbolIterator: "iterator", storage.SymbolAsyncIterator: "asyncIterator", storage.SymbolHasInstance: "hasInstance",
		storage.SymbolToPrimitive: "toPrimitive", storage.SymbolToStringTag: "toStringTag", storage.SymbolSpecies: "species",
		storage.SymbolIsConcatSpreadable: "isConcatSpreadable", storage.SymbolUnscopables: "unscopables",
		storage.SymbolMatch: "match", storage.SymbolMatchAll: "matchAll", storage.SymbolReplace: "replace",
		storage.SymbolSearch: "search", storage.SymbolSplit: "split",
		storage.SymbolDispose: "dispose", storage.SymbolAsyncDispose: "asyncDispose",
		storage.SymbolMetadata: "metadata",
	} {
		constant(cx, ctor, wtf8.String(name), storage.ObjectValue(cx.WellKnownSymbol(storage.WellKnownSymbol(sym))))
	}
	method(cx, ctor, "for", 1, nativeSymbolFor)
	method(cx, ctor, "keyFor", 1, nativeSymbolKeyFor)
	method(cx, proto, "toString", 0, nativeSymbolToString)
	method(cx, proto, "valueOf", 0, nativeSymbolValueOf)
	getter(cx, proto, cx.Intern("description"), "description", nativeSymbolDescription)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Symbol"), storage.FlagConfigurable)
	fn := cx.NewNative("[Symbol.toPrimitive]", 1, nativeSymbolToPrimitive)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToPrimitive), storage.ObjectValue(fn), storage.FlagConfigurable)
}

func symbolConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if cx.GetNewTarget() != nil {
		cx.ThrowTypeError("Symbol is not a constructor")
	}
	d := arg(args, 0)
	if d.IsUndefined() {
		return storage.ObjectValue(cx.NewSymbol("", false))
	}
	return storage.ObjectValue(cx.NewSymbol(cx.ToString(d), true))
}

func symbolFor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.ObjectValue(cx.RegisteredSymbol(cx.ToString(arg(args, 0))))
}

func symbolKeyFor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := arg(args, 0)
	if !s.IsSymbol() {
		cx.ThrowTypeError(cx.DisplayString(s) + " is not a symbol")
	}
	key, ok := cx.KeyForSymbol(s.Object())
	if !ok {
		return storage.Undefined
	}
	return cx.NewString(key)
}

func symbolToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s, ok := storage.ThisSymbolValue(this)
	requireThisSymbol(cx, ok, "toString")
	desc, _ := storage.SymbolDescription(s)
	checkStringLength(cx, len(desc)+8)
	return cx.NewString("Symbol(" + desc + ")")
}

func symbolValueOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s, ok := storage.ThisSymbolValue(this)
	requireThisSymbol(cx, ok, "valueOf")
	return storage.ObjectValue(s)
}

func symbolDescription(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s, ok := storage.ThisSymbolValue(this)
	requireThisSymbol(cx, ok, "description")
	desc, ok := storage.SymbolDescription(s)
	if !ok {
		return storage.Undefined
	}
	return cx.NewString(desc)
}

func symbolToPrimitive(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s, ok := storage.ThisSymbolValue(this)
	requireThisSymbol(cx, ok, "[Symbol.toPrimitive]")
	return storage.ObjectValue(s)
}

var (
	nativeMathRandom = engine.RegisterNative(mathRandom)
	nativeMathMax    = engine.RegisterNativeWithKernels(mathMax, engine.Kernels{
		Number2:  mathMaxKernel,
		Contract: shortContract,
	})
	nativeMathMin = engine.RegisterNativeWithKernels(mathMin, engine.Kernels{
		Number2:  mathMinKernel,
		Contract: shortContract,
	})
	nativeMathSumPrecise = engine.RegisterNative(mathSumPrecise)
	nativeMathHypot      = engine.RegisterNative(mathHypot)
)

func initMath(cx *engine.ExecutionContext) {
	m := cx.NewLazyObject(cx.Intrinsic(storage.IntrinsicObjectPrototype), initMathProperties)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Math"), storage.ObjectValue(m), storage.FlagsHidden)
}

func initMathProperties(cx *engine.ExecutionContext, m *storage.Object) {
	cx.Define(m, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Math"), storage.FlagConfigurable)
	for _, c := range []struct {
		name  string
		value float64
	}{
		{name: "E", value: math.E},
		{name: "LN10", value: math.Ln10},
		{name: "LN2", value: math.Ln2},
		{name: "LOG10E", value: math.Log10E},
		{name: "LOG2E", value: math.Log2E},
		{name: "PI", value: math.Pi},
		{name: "SQRT1_2", value: math.Sqrt2 / 2},
		{name: "SQRT2", value: math.Sqrt2},
	} {
		constant(cx, m, wtf8.String(c.name), storage.Number(c.value))
	}
	method(cx, m, "abs", 1, nativeMathAbs)
	method(cx, m, "acos", 1, nativeMathAcos)
	method(cx, m, "acosh", 1, nativeMathAcosh)
	method(cx, m, "asin", 1, nativeMathAsin)
	method(cx, m, "asinh", 1, nativeMathAsinh)
	method(cx, m, "atan", 1, nativeMathAtan)
	method(cx, m, "atanh", 1, nativeMathAtanh)
	method(cx, m, "cbrt", 1, nativeMathCbrt)
	method(cx, m, "ceil", 1, nativeMathCeil)
	method(cx, m, "cos", 1, nativeMathCos)
	method(cx, m, "cosh", 1, nativeMathCosh)
	method(cx, m, "exp", 1, nativeMathExp)
	method(cx, m, "expm1", 1, nativeMathExpm1)
	method(cx, m, "floor", 1, nativeMathFloor)
	method(cx, m, "fround", 1, nativeMathFround)
	method(cx, m, "f16round", 1, nativeMathF16round)
	method(cx, m, "log", 1, nativeMathLog)
	method(cx, m, "log1p", 1, nativeMathLog1p)
	method(cx, m, "log10", 1, nativeMathLog10)
	method(cx, m, "log2", 1, nativeMathLog2)
	method(cx, m, "round", 1, nativeMathRound)
	method(cx, m, "sign", 1, nativeMathSign)
	method(cx, m, "sin", 1, nativeMathSin)
	method(cx, m, "sinh", 1, nativeMathSinh)
	method(cx, m, "sqrt", 1, nativeMathSqrt)
	method(cx, m, "tan", 1, nativeMathTan)
	method(cx, m, "tanh", 1, nativeMathTanh)
	method(cx, m, "trunc", 1, nativeMathTrunc)
	method(cx, m, "clz32", 1, nativeMathClz32)
	method(cx, m, "atan2", 2, nativeMathAtan2)
	method(cx, m, "pow", 2, nativeMathPow)
	method(cx, m, "imul", 2, nativeMathImul)
	method(cx, m, "random", 0, nativeMathRandom)
	// Go's min and max of floats give NaN when an argument is NaN and order -0 below +0, as the
	// specification does.
	maxFn := method(cx, m, "max", 2, nativeMathMax)
	cx.SetExtremum(maxFn, engine.ExtremumMax)
	minFn := method(cx, m, "min", 2, nativeMathMin)
	cx.SetExtremum(minFn, engine.ExtremumMin)
	method(cx, m, "sumPrecise", 1, nativeMathSumPrecise)
	method(cx, m, "hypot", 2, nativeMathHypot)
}

var (
	nativeMathAbs      = registerMathUnary(math.Abs)
	nativeMathAcos     = registerMathUnary(math.Acos)
	nativeMathAcosh    = registerMathUnary(math.Acosh)
	nativeMathAsin     = registerMathUnary(math.Asin)
	nativeMathAsinh    = registerMathUnary(math.Asinh)
	nativeMathAtan     = registerMathUnary(math.Atan)
	nativeMathAtanh    = registerMathUnary(math.Atanh)
	nativeMathCbrt     = registerMathUnary(math.Cbrt)
	nativeMathCeil     = registerMathUnary(math.Ceil)
	nativeMathCos      = registerMathUnary(math.Cos)
	nativeMathCosh     = registerMathUnary(cosh)
	nativeMathExp      = registerMathUnary(exp)
	nativeMathExpm1    = registerMathUnary(expm1)
	nativeMathFloor    = registerMathUnary(math.Floor)
	nativeMathFround   = registerMathUnary(mathFroundKernel)
	nativeMathF16round = registerMathUnary(numconv.RoundFloat16)
	nativeMathLog      = registerMathUnary(math.Log)
	nativeMathLog1p    = registerMathUnary(math.Log1p)
	nativeMathLog10    = registerMathUnary(log10)
	nativeMathLog2     = registerMathUnary(log2)
	nativeMathRound    = registerMathUnary(mathRoundKernel)
	nativeMathSign     = registerMathUnary(mathSignKernel)
	nativeMathSin      = registerMathUnary(math.Sin)
	nativeMathSinh     = registerMathUnary(sinh)
	nativeMathSqrt     = registerMathUnary(math.Sqrt)
	nativeMathTan      = registerMathUnary(math.Tan)
	nativeMathTanh     = registerMathUnary(math.Tanh)
	nativeMathTrunc    = registerMathUnary(math.Trunc)
	nativeMathClz32    = registerMathUnary(mathClz32Kernel)
	nativeMathAtan2    = registerMathBinary(atan2)
	nativeMathPow      = registerMathBinary(numconv.NumberPow)
	nativeMathImul     = registerMathBinary(mathImulKernel)
)

// registerMathUnary registers the function of Math that f is the kernel of.
func registerMathUnary(f func(x float64) float64) storage.Native {
	return engine.RegisterNativeWithKernels(mathUnaryNative(f), engine.Kernels{
		Number1:  f,
		Contract: shortContract,
	})
}

// mathUnaryNative returns the function of Math that f is the kernel of.
//
//go:noinline
func mathUnaryNative(f func(x float64) float64) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		if len(args) > 0 && args[0].IsNumber() {
			return storage.Number(f(args[0].Number()))
		}
		return storage.Number(f(cx.ToNumber(arg(args, 0))))
	}
}

// registerMathBinary registers the function of Math that f is the kernel of.
func registerMathBinary(f func(x, y float64) float64) storage.Native {
	return engine.RegisterNativeWithKernels(mathBinaryNative(f), engine.Kernels{
		Number2:  f,
		Contract: shortContract,
	})
}

// mathBinaryNative returns the function of Math that f is the kernel of.
//
//go:noinline
func mathBinaryNative(f func(x, y float64) float64) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		x := cx.ToNumber(arg(args, 0))
		return storage.Number(f(x, cx.ToNumber(arg(args, 1))))
	}
}

func mathFroundKernel(x float64) float64 {
	return float64(float32(x))
}

func mathRoundKernel(x float64) float64 {
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
}

func mathSignKernel(x float64) float64 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return x
}

func mathClz32Kernel(x float64) float64 {
	return float64(bits.LeadingZeros32(numconv.ToUint32(x)))
}

func mathImulKernel(x, y float64) float64 {
	return float64(numconv.ToInt32(x) * numconv.ToInt32(y))
}

func mathRandom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(cx.RandomNumber())
}

func mathMax(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	r := math.Inf(-1)
	for _, a := range args {
		r = max(r, cx.ToNumber(a))
	}
	return storage.Number(r)
}

func mathMaxKernel(x, y float64) float64 {
	return max(x, y)
}

func mathMin(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	r := math.Inf(1)
	for _, a := range args {
		r = min(r, cx.ToNumber(a))
	}
	return storage.Number(r)
}

func mathMinKernel(x, y float64) float64 {
	return min(x, y)
}

func mathSumPrecise(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(sumPrecise(cx, arg(args, 0)))
}

func mathHypot(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	var scale, sum, correction float64
	var inf, nan bool
	for _, a := range args {
		n := math.Abs(cx.ToNumber(a))
		switch {
		case math.IsInf(n, 0):
			inf = true
		case math.IsNaN(n):
			nan = true
		case n != 0:
			// The sum and its rounding correction are in units of scale².
			if n > scale {
				ratio := scale / n
				sum = sum * ratio * ratio
				correction = correction * ratio * ratio
				scale = n
			}
			scaled := n / scale
			term := scaled*scaled - correction
			next := sum + term
			correction = (next - sum) - term
			sum = next
		}
	}
	if inf {
		return storage.Number(math.Inf(1))
	}
	if nan {
		return storage.Number(math.NaN())
	}
	return storage.Number(scale * math.Sqrt(sum))
}

// nativeStringIteratorNext is %StringIteratorPrototype%.next.
var nativeStringIteratorNext = engine.RegisterPlainIteratorNext(engine.IteratorClosureRealm, "next method called on an object that is not a string iterator", quiet.StringIteratorStep, nil)

var nativeGeneratorFunctionConstructor = engine.RegisterNative(generatorFunctionConstructor)

func initIterators(cx *engine.ExecutionContext) {
	symMethod(cx, cx.Intrinsic(storage.IntrinsicIteratorPrototype), cx.WellKnownSymbol(storage.SymbolIterator), "[Symbol.iterator]", 0, nativeReturnThis)
	symMethod(cx, cx.Intrinsic(storage.IntrinsicAsyncIteratorPrototype), cx.WellKnownSymbol(storage.SymbolAsyncIterator), "[Symbol.asyncIterator]", 0, nativeReturnThis)

	arrayIterNext := cx.NewArrayIteratorNext()
	cx.Define(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototype), cx.Intern("next"), storage.ObjectValue(arrayIterNext), storage.FlagsHidden)
	cx.SetIntrinsic(storage.IntrinsicArrayIteratorPrototypeNext, arrayIterNext)
	cx.Define(cx.Intrinsic(storage.IntrinsicArrayIteratorPrototype), cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Array Iterator"), storage.FlagConfigurable)

	method(cx, cx.Intrinsic(storage.IntrinsicStringIteratorPrototype), "next", 0, nativeStringIteratorNext)
	cx.Define(cx.Intrinsic(storage.IntrinsicStringIteratorPrototype), cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("String Iterator"), storage.FlagConfigurable)

	gp := cx.Intrinsic(storage.IntrinsicGeneratorPrototype)
	cx.SetIntrinsic(storage.IntrinsicGeneratorPrototypeNext, method(cx, gp, "next", 1, nativeGeneratorNext))
	method(cx, gp, "return", 1, nativeGeneratorReturn)
	method(cx, gp, "throw", 1, nativeGeneratorThrow)
	cx.Define(gp, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Generator"), storage.FlagConfigurable)

	// The prototype chain of generator functions.
	gf := cx.Intrinsic(storage.IntrinsicGeneratorFunctionPrototype)
	cx.Define(gf, cx.Key(storage.KeyPrototype), storage.ObjectValue(gp), storage.FlagConfigurable)
	cx.Define(gp, cx.Key(storage.KeyConstructor), storage.ObjectValue(gf), storage.FlagConfigurable)
	functionKindConstructor(cx, "GeneratorFunction", nativeGeneratorFunctionConstructor, gf)
}

var (
	nativeGeneratorNext   = engine.RegisterNative(generatorNative("next", engine.ResumeNext))
	nativeGeneratorReturn = engine.RegisterNative(generatorNative("return", engine.ResumeReturn))
	nativeGeneratorThrow  = engine.RegisterNative(generatorNative("throw", engine.ResumeThrow))
)

// generatorNative returns the method name of generators, which resumes a generator in mode.
//
//go:noinline
func generatorNative(name string, mode engine.ResumeMode) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		r, ok := cx.ResumeGenerator(this, mode, arg(args, 0))
		if !ok {
			cx.ThrowTypeError(name + " method called on an object that is not a generator")
		}
		return r
	}
}

func generatorFunctionConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.CreateDynamicFunction(engine.GeneratorFunction, args)
}

// expOverflow is the largest argument for which e**x is finite.
const expOverflow = 7.09782712893383973096e+02

// exp returns e**x.
func exp(x float64) float64 {
	// Halving the argument keeps the subnormal results from being flushed to zero.
	if x < -708 {
		h := math.Exp(x / 2)
		return h * h
	}
	y := math.Exp(x)
	// math.Exp on amd64 overflows from 1023.5·ln 2 on (go.dev/issue/81995).
	// TODO: Remove this fallback once go.mod requires a Go release with the fix.
	if math.IsInf(y, 1) && x <= expOverflow {
		return expNearOverflow(x)
	}
	return y
}

// expNearOverflow returns e**x for 709 ≤ x ≤ expOverflow.
func expNearOverflow(x float64) float64 {
	// ln 2 = ln2Hi + ln2Lo, where 1024·ln2Hi has few enough bits to subtract from x exactly.
	const (
		ln2Hi = 6.93147180369123816490e-01
		ln2Lo = 1.90821492927058770002e-10
	)
	// e**x = 2**1024 · e**(hi-lo), and hi-lo rounds to r with the error (hi-r)-lo.
	hi := x - 1024*ln2Hi
	lo := 1024 * ln2Lo
	r := hi - lo
	m := math.Expm1(r)
	y := 1 + (m + (1+m)*((hi-r)-lo))
	return y * 0x1p1023 * 2
}

// expm1 returns e**x - 1.
func expm1(x float64) float64 {
	if x >= expOverflow {
		return exp(x)
	}
	return math.Expm1(x)
}

// cosh returns the hyperbolic cosine of x.
func cosh(x float64) float64 {
	// math.Cosh overflows wherever math.Exp does.
	if a := math.Abs(x); a > 709 {
		return halfExp(a)
	}
	return math.Cosh(x)
}

// sinh returns the hyperbolic sine of x.
func sinh(x float64) float64 {
	// math.Sinh overflows wherever math.Exp does.
	if a := math.Abs(x); a > 709 {
		return math.Copysign(halfExp(a), x)
	}
	return math.Sinh(x)
}

// halfExp returns e**a / 2 for a > 709, which is cosh(a) and sinh(a) to double precision.
func halfExp(a float64) float64 {
	if a > expOverflow {
		h := math.Exp(a / 2)
		return h * (h / 2)
	}
	return exp(a) / 2
}

// atan2 returns the arc tangent of y/x, using the signs of both to pick the quadrant.
func atan2(y, x float64) float64 {
	r := math.Atan2(y, x)
	// With y < 0 and x < 0, a quotient that underflows to +0 must still give the third
	// quadrant.
	if r == math.Pi && y < 0 {
		return -math.Pi
	}
	return r
}

// log10 returns the decimal logarithm of x, exactly n for the normal double nearest to 10**n.
func log10(x float64) float64 {
	r := math.Log10(x)
	// A subnormal nearest to 10**n is too far from it for n to be its logarithm.
	if n := math.Round(r); n != r && n >= -307 && math.Abs(r-n) < 1e-12 && x == numconv.Pow10(int(n)) {
		return n
	}
	return r
}

// log2 returns the binary logarithm of x.
func log2(x float64) float64 {
	// math.Log2 adds the exponent to the logarithm of the fraction, which cancels just above 1.
	if x > 1 && x < 2 {
		return math.Log(x) * (1 / math.Ln2)
	}
	return math.Log2(x)
}

// sumPreciseBits is the precision that holds the exact sum of up to 2^53 finite numbers.
const sumPreciseBits = 1074 + 1024 + 53 + 1

// sumPrecise implements Math.sumPrecise.
func sumPrecise(cx *engine.ExecutionContext, items storage.Value) float64 {
	cx.RequireObjectCoercible(items, "Math.sumPrecise")
	it, next := cx.GetIterator(items)
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
	cx.IterateRecord(it, next, func(v storage.Value) bool {
		count++
		if count >= 1<<53 {
			cx.ThrowRangeError("Math.sumPrecise: too many values")
		}
		if !v.IsNumber() {
			cx.ThrowTypeError("Math.sumPrecise: " + cx.DisplayString(v) + " is not a number")
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
