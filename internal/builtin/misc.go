// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"bytes"
	"math"
	"math/big"
	"math/bits"
	"strconv"

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
			} else if i, ok := v.BigInt64(); ok {
				n = float64(i)
			} else {
				n, _ = new(big.Float).SetInt(v.BigInt()).Float64()
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
			return vm.ToStringValue(engine.Number(n))
		}
		return vm.NewString(numconv.NumberToRadixString(n, int(radix)))
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.ToStringValue(engine.Number(thisNumber(vm, this, "toLocaleString")))
	})
	method(vm, proto, "toFixed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toFixed")
		d := vm.ToIntegerOrInfinity(arg(args, 0))
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toFixed() digits argument must be between 0 and 100")
		}
		if n != n || math.IsInf(n, 0) || math.Abs(n) >= 1e21 {
			return vm.ToStringValue(engine.Number(n))
		}
		var buf [numberBufSize]byte
		s := appendHalfUp(buf[:0], n, 'f', int(d))
		if n == 0 && s[0] == '-' {
			// -0 is formatted as 0.
			s = s[1:]
		}
		return vm.NewStringBytes(s)
	})
	method(vm, proto, "toPrecision", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toPrecision")
		if arg(args, 0).IsUndefined() {
			return vm.ToStringValue(engine.Number(n))
		}
		p := vm.ToIntegerOrInfinity(args[0])
		if n != n || math.IsInf(n, 0) {
			return vm.ToStringValue(engine.Number(n))
		}
		if p < 1 || p > 100 {
			vm.ThrowRangeError("toPrecision() argument must be between 1 and 100")
		}
		if n == 0 {
			// -0 is formatted as 0.
			n = 0
		}
		var buf [numberBufSize]byte
		if n != 0 {
			es := appendHalfUp(buf[:0], n, 'e', int(p)-1)
			e := exponent(es)
			if e < -6 || e >= int(p) {
				return vm.NewStringBytes(fixExponent(es))
			}
			return vm.NewStringBytes(precisionFixed(es, int(p), e))
		}
		return vm.NewStringBytes(appendHalfUp(buf[:0], n, 'f', int(p)-1))
	})
	method(vm, proto, "toExponential", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toExponential")
		d := vm.ToIntegerOrInfinity(arg(args, 0))
		if n != n || math.IsInf(n, 0) {
			return vm.ToStringValue(engine.Number(n))
		}
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toExponential() argument must be between 0 and 100")
		}
		if n == 0 {
			// -0 is formatted as 0.
			n = 0
		}
		var buf [numberBufSize]byte
		if arg(args, 0).IsUndefined() {
			return vm.NewStringBytes(fixExponent(strconv.AppendFloat(buf[:0], n, 'e', -1, 64)))
		}
		return vm.NewStringBytes(fixExponent(appendHalfUp(buf[:0], n, 'e', int(d))))
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

// numberBufSize is the size of a buffer that holds most results of formatting a number with a precision.
const numberBufSize = 128

// appendHalfUp appends n formatted like [strconv.AppendFloat] with the format fmt ('e' or 'f') and
// the precision prec (non-negative), except that an exact tie rounds away from zero.
func appendHalfUp(b []byte, n float64, fmt byte, prec int) []byte {
	if !isTie(n, fmt, prec) {
		return strconv.AppendFloat(b, n, fmt, prec, 64)
	}
	// With one more digit, a tie is exact and ends in 5. Dropping that 5 and incrementing
	// the remaining digits rounds it to the larger magnitude.
	start := len(b)
	b = strconv.AppendFloat(b, n, fmt, prec+1, 64)
	var exp []byte
	if fmt == 'e' {
		i := bytes.IndexByte(b[start:], 'e') + start
		exp = append([]byte(nil), b[i:]...)
		b = b[:i]
	}
	b = b[:len(b)-1]
	if prec == 0 {
		// Drop the decimal point.
		b = b[:len(b)-1]
	}
	carry := true
	for i := len(b) - 1; i >= start && carry; i-- {
		switch c := b[i]; {
		case c == '9':
			b[i] = '0'
		case c >= '0' && c < '9':
			b[i]++
			carry = false
		}
	}
	if carry {
		// All the digits were 9: the result gains a leading 1.
		i := start
		if b[i] == '-' {
			i++
		}
		b = append(b, 0)
		copy(b[i+1:], b[i:])
		b[i] = '1'
		if fmt == 'e' {
			// Keep one digit before the point: 10.0e+05 becomes 1.00e+06.
			if prec > 0 {
				b[i+1], b[i+2] = b[i+2], b[i+1]
			}
			b = b[:len(b)-1]
			e, _ := strconv.Atoi(string(exp[1:]))
			return appendExponent(b, e+1)
		}
	}
	return append(b, exp...)
}

// precisionFixed rewrites p rounded exponential digits in fixed notation for -6 <= e < p.
func precisionFixed(s []byte, p, e int) []byte {
	var start int
	if s[0] == '-' {
		start = 1
	}
	// Remove the point and exponent, leaving only the sign and significant digits.
	if p > 1 {
		copy(s[start+1:], s[start+2:start+p+1])
	}
	s = s[:start+p]
	if e < 0 {
		s = s[:start+p+1-e]
		copy(s[start+1-e:], s[start:start+p])
		s[start], s[start+1] = '0', '.'
		for i := start + 2; i < start+1-e; i++ {
			s[i] = '0'
		}
	} else if e+1 < p {
		s = s[:len(s)+1]
		point := start + e + 1
		copy(s[point+1:], s[point:len(s)-1])
		s[point] = '.'
	}
	return s
}

// appendExponent appends the exponent e in the form of [strconv.AppendFloat] with the format 'e'.
func appendExponent(b []byte, e int) []byte {
	b = append(b, 'e')
	if e < 0 {
		b = append(b, '-')
		e = -e
	} else {
		b = append(b, '+')
	}
	if e < 10 {
		b = append(b, '0')
	}
	return strconv.AppendInt(b, int64(e), 10)
}

// isTie reports whether the finite number n lies exactly halfway between the two nearest numbers
// that the format fmt ('e' or 'f') shows with the precision prec.
func isTie(n float64, fmt byte, prec int) bool {
	if n == 0 {
		return false
	}
	u := math.Float64bits(n)
	m := u & (1<<52 - 1)
	e2 := int(u >> 52 & 0x7ff)
	if e2 == 0 {
		e2 = -1074
	} else {
		m |= 1 << 52
		e2 -= 1075
	}
	tz := bits.TrailingZeros64(m)
	m >>= tz
	e2 += tz
	// |n| is m×2^e2 with m odd, so its exact decimal expansion has k fraction digits
	// and, when k is positive, ends in 5.
	k := max(0, -e2)
	if fmt == 'f' {
		return k == prec+1
	}
	if k > 0 {
		// The expansion has e10+1+k significant digits, where e10 is the decimal exponent of n,
		// which lies within one of the bounds from the binary exponent.
		e := e2 + bits.Len64(m) - 1
		lo := int(math.Floor(float64(e)*math.Log10(2))) - 1
		hi := int(math.Floor(float64(e+1)*math.Log10(2))) + 1
		if prec+2 < lo+1+k || prec+2 > hi+1+k {
			return false
		}
	}
	// A tie shows as a trailing 5 with one more digit, and that string is then exactly n.
	var buf [numberBufSize]byte
	t := strconv.AppendFloat(buf[:0], n, 'e', prec+1, 64)
	if t[bytes.IndexByte(t, 'e')-1] != '5' {
		return false
	}
	r, ok := new(big.Rat).SetString(string(t))
	return ok && r.Cmp(new(big.Rat).SetFloat64(n)) == 0
}

// exponent returns the exponent of the number s that [strconv.AppendFloat] formats with the format 'e'.
func exponent(s []byte) int {
	i := bytes.IndexByte(s, 'e')
	e := 0
	for _, c := range s[i+2:] {
		e = e*10 + int(c-'0')
	}
	if s[i+1] == '-' {
		return -e
	}
	return e
}

// fixExponent rewrites in place the exponent that Go formats with at least two digits to the JavaScript form.
func fixExponent(s []byte) []byte {
	i := bytes.IndexByte(s, 'e')
	if i < 0 {
		return s
	}
	digits := s[i+2:]
	j := 0
	for j < len(digits)-1 && digits[j] == '0' {
		j++
	}
	return append(s[:i+2], digits[j:]...)
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
		engine.SymbolMetadata: "metadata",
	} {
		constant(vm, ctor, wtf8.String(name), engine.ObjectValue(vm.WellKnownSymbol(engine.WellKnownSymbol(sym))))
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
		checkStringLength(vm, len(desc)+8)
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
		constant(vm, m, wtf8.String(c.name), engine.Number(c.value))
	}
	unary := func(name string, f func(float64) float64) {
		method(vm, m, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
	unary("cosh", cosh)
	unary("exp", exp)
	unary("expm1", expm1)
	unary("floor", math.Floor)
	unary("fround", func(x float64) float64 {
		return float64(float32(x))
	})
	unary("f16round", f16round)
	unary("log", math.Log)
	unary("log1p", math.Log1p)
	unary("log10", log10)
	unary("log2", log2)
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
	unary("sinh", sinh)
	unary("sqrt", math.Sqrt)
	unary("tan", math.Tan)
	unary("tanh", math.Tanh)
	unary("trunc", math.Trunc)
	unary("clz32", func(x float64) float64 {
		return float64(bits.LeadingZeros32(numconv.ToUint32(x)))
	})
	method(vm, m, "atan2", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		y := vm.ToNumber(arg(args, 0))
		return engine.Number(atan2(y, vm.ToNumber(arg(args, 1))))
	})
	method(vm, m, "pow", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := vm.ToNumber(arg(args, 0))
		y := vm.ToNumber(arg(args, 1))
		return engine.Number(engine.NumberPow(x, y))
	})
	method(vm, m, "imul", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		x := vm.ToInt32(arg(args, 0))
		return engine.Number(float64(x * vm.ToInt32(arg(args, 1))))
	})
	method(vm, m, "random", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(vm.RandomNumber())
	})
	minmax := func(name string, start float64, pick func(a, b float64) bool) {
		method(vm, m, wtf8.String(name), 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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

	arrayIterNext := iteratorNext(vm, vm.Intrinsic(engine.IntrinsicArrayIteratorPrototype), (*engine.VM).ArrayIteratorStep)
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
		return method(vm, gp, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
