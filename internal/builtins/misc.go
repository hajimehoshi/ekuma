// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtins

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func initNumber(vm *engine.VM) {
	proto := vm.NumberProto
	proto.Class = engine.ClassNumber
	proto.Ext = 0.0
	ctor := constructor(vm, "Number", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := 0.0
		if len(args) > 0 {
			v := vm.ToNumeric(args[0])
			if v.O != nil {
				n, _ = new(big.Float).SetInt(v.O.BigInt()).Float64()
			} else {
				n = v.N
			}
		}
		if nt := vm.NewTarget; nt != nil {
			w := vm.NewObject(vm.ProtoFromConstructor(nt, vm.NumberProto))
			w.Class = engine.ClassNumber
			w.Ext = n
			return engine.Value{O: w}
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
		return v.IsNumber() && v.N == math.Trunc(v.N) && !math.IsInf(v.N, 0)
	}
	method(vm, ctor, "isInteger", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(isInteger(arg(args, 0)))
	})
	method(vm, ctor, "isSafeInteger", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(isInteger(v) && math.Abs(v.N) <= numconv.MaxSafeInteger)
	})
	method(vm, ctor, "isFinite", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(v.IsNumber() && v.N == v.N && !math.IsInf(v.N, 0))
	})
	method(vm, ctor, "isNaN", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		return engine.Bool(v.IsNumber() && v.N != v.N)
	})
	g := vm.Global
	for _, name := range []string{"parseFloat", "parseInt"} {
		if p, ok := g.Shape.Lookup(vm.Intern(name)); ok {
			vm.Define(ctor, vm.Intern(name), g.Slots[p.Slot], engine.FlagsHidden)
		}
	}

	thisNumber := func(vm *engine.VM, this engine.Value, what string) float64 {
		if this.IsNumber() {
			return this.N
		}
		if this.IsObject() && this.O.Class == engine.ClassNumber {
			return this.O.Ext.(float64)
		}
		vm.ThrowTypeError("Number.prototype." + what + " requires that 'this' be a Number")
		return 0
	}
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisNumber(vm, this, "valueOf"))
	})
	method(vm, proto, "toString", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toString")
		radix := 10.0
		if r := arg(args, 0); !r.IsUndefined() {
			radix = vm.ToInteger(r)
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
		d := vm.ToInteger(arg(args, 0))
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toFixed() digits argument must be between 0 and 100")
		}
		if n != n || math.IsInf(n, 0) || math.Abs(n) >= 1e21 {
			return vm.NewString(numconv.NumberToString(n))
		}
		s := strconv.FormatFloat(n, 'f', int(d), 64)
		if n == 0 || (strings.HasPrefix(s, "-") && strings.Trim(s, "-0.") == "") {
			s = strings.TrimPrefix(s, "-")
		}
		return vm.NewString(s)
	})
	method(vm, proto, "toPrecision", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toPrecision")
		if arg(args, 0).IsUndefined() {
			return vm.NewString(numconv.NumberToString(n))
		}
		p := vm.ToInteger(args[0])
		if n != n || math.IsInf(n, 0) {
			return vm.NewString(numconv.NumberToString(n))
		}
		if p < 1 || p > 100 {
			vm.ThrowRangeError("toPrecision() argument must be between 1 and 100")
		}
		e := 0
		if n != 0 {
			es := strconv.FormatFloat(n, 'e', int(p)-1, 64)
			e, _ = strconv.Atoi(es[strings.IndexByte(es, 'e')+1:])
		}
		if e < -6 || e >= int(p) {
			return vm.NewString(fixExponent(strconv.FormatFloat(n, 'e', int(p)-1, 64)))
		}
		return vm.NewString(strconv.FormatFloat(n, 'f', int(p)-1-e, 64))
	})
	method(vm, proto, "toExponential", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := thisNumber(vm, this, "toExponential")
		d := vm.ToInteger(arg(args, 0))
		if n != n || math.IsInf(n, 0) {
			return vm.NewString(numconv.NumberToString(n))
		}
		if d < 0 || d > 100 {
			vm.ThrowRangeError("toExponential() argument must be between 0 and 100")
		}
		prec := int(d)
		if arg(args, 0).IsUndefined() {
			prec = -1
		}
		return vm.NewString(fixExponent(strconv.FormatFloat(n, 'e', prec, 64)))
	})

	bproto := vm.BooleanProto
	bproto.Class = engine.ClassBoolean
	bproto.Ext = false
	constructor(vm, "Boolean", 1, bproto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := engine.Truthy(arg(args, 0))
		if nt := vm.NewTarget; nt != nil {
			w := vm.NewObject(vm.ProtoFromConstructor(nt, vm.BooleanProto))
			w.Class = engine.ClassBoolean
			w.Ext = b
			return engine.Value{O: w}
		}
		return engine.Bool(b)
	})
	thisBool := func(vm *engine.VM, this engine.Value, what string) bool {
		if this.IsBool() {
			return this.O == engine.TrueObject
		}
		if this.IsObject() && this.O.Class == engine.ClassBoolean {
			return this.O.Ext.(bool)
		}
		vm.ThrowTypeError("Boolean.prototype." + what + " requires that 'this' be a Boolean")
		return false
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

// fixExponent rewrites the exponent that Go formats with at least two digits to the JavaScript form.
func fixExponent(s string) string {
	i := strings.IndexByte(s, 'e')
	if i < 0 {
		return s
	}
	mant, exp := s[:i], s[i+1:]
	sign := exp[:1]
	digits := strings.TrimLeft(exp[1:], "0")
	if digits == "" {
		digits = "0"
	}
	return mant + "e" + sign + digits
}

func initSymbol(vm *engine.VM) {
	proto := vm.SymbolProto
	ctor := constructor(vm, "Symbol", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if vm.NewTarget != nil {
			vm.ThrowTypeError("Symbol is not a constructor")
		}
		d := arg(args, 0)
		if d.IsUndefined() {
			return engine.Value{O: vm.NewSymbol("", false)}
		}
		return engine.Value{O: vm.NewSymbol(vm.ToString(d), true)}
	})
	k := &vm.K
	for _, e := range []struct {
		name string
		sym  *engine.Object
	}{
		{"iterator", k.SymIterator}, {"asyncIterator", k.SymAsyncIterator}, {"hasInstance", k.SymHasInstance},
		{"toPrimitive", k.SymToPrimitive}, {"toStringTag", k.SymToStringTag}, {"species", k.SymSpecies},
		{"isConcatSpreadable", k.SymIsConcatSpreadable}, {"unscopables", k.SymUnscopables},
		{"match", k.SymMatch}, {"matchAll", k.SymMatchAll}, {"replace", k.SymReplace},
		{"search", k.SymSearch}, {"split", k.SymSplit},
	} {
		constant(vm, ctor, e.name, engine.Value{O: e.sym})
	}
	method(vm, ctor, "for", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		key := vm.ToString(arg(args, 0))
		if s, ok := vm.SymbolRegistry[key]; ok {
			return engine.Value{O: s}
		}
		s := vm.NewSymbol(key, true)
		vm.SymbolRegistry[key] = s
		return engine.Value{O: s}
	})
	method(vm, ctor, "keyFor", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := arg(args, 0)
		if !s.IsSymbol() {
			vm.ThrowTypeError(vm.DisplayString(s) + " is not a symbol")
		}
		if r, ok := vm.SymbolRegistry[s.O.Str]; ok && r == s.O {
			return vm.NewString(s.O.Str)
		}
		return engine.Undefined
	})
	thisSymbol := func(vm *engine.VM, this engine.Value, what string) *engine.Object {
		if this.IsSymbol() {
			return this.O
		}
		if this.IsObject() && this.O.Class == engine.ClassSymbol {
			return this.O.Ext.(*engine.Object)
		}
		vm.ThrowTypeError("Symbol.prototype." + what + " requires that 'this' be a Symbol")
		return nil
	}
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString("Symbol(" + thisSymbol(vm, this, "toString").Str + ")")
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Value{O: thisSymbol(vm, this, "valueOf")}
	})
	getter(vm, proto, vm.Intern("description"), "description", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisSymbol(vm, this, "description")
		if s.Ext == nil {
			return engine.Undefined
		}
		return vm.NewString(s.Str)
	})
	vm.Define(proto, k.SymToStringTag, vm.NewString("Symbol"), engine.FlagConfigurable)
	fn := vm.NewNative("[Symbol.toPrimitive]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Value{O: thisSymbol(vm, this, "[Symbol.toPrimitive]")}
	})
	vm.Define(proto, k.SymToPrimitive, engine.Value{O: fn}, engine.FlagConfigurable)
}

func initMath(vm *engine.VM) {
	m := vm.NewObject(vm.ObjectProto)
	vm.Define(vm.Global, vm.Intern("Math"), engine.Value{O: m}, engine.FlagsHidden)
	vm.Define(m, vm.K.SymToStringTag, vm.NewString("Math"), engine.FlagConfigurable)
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
			if len(args) > 0 && args[0].O == nil {
				return engine.Value{N: f(args[0].N)}
			}
			return engine.Value{N: f(vm.ToNumber(arg(args, 0)))}
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
		return math.Floor(x + 0.5)
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
		return engine.Number(rand.Float64())
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
	symMethod(vm, vm.IteratorProto, vm.K.SymIterator, "[Symbol.iterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	symMethod(vm, vm.AsyncIteratorProto, vm.K.SymAsyncIterator, "[Symbol.asyncIterator]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})

	vm.FnArrayIterNext = method(vm, vm.ArrayIteratorProto, "next", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var it *arrayIterator
		if this.IsObject() {
			it, _ = this.O.Ext.(*arrayIterator)
		}
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not an array iterator")
		}
		if it.obj == nil {
			return engine.Value{O: vm.NewIterResult(engine.Undefined, true)}
		}
		n := lengthOf(vm, it.obj)
		i := it.pos
		if i >= n {
			it.obj = nil
			return engine.Value{O: vm.NewIterResult(engine.Undefined, true)}
		}
		it.pos++
		switch it.kind {
		case iterKeys:
			return engine.Value{O: vm.NewIterResult(engine.Number(float64(i)), false)}
		case iterValues:
			return engine.Value{O: vm.NewIterResult(getAt(vm, it.obj, i), false)}
		}
		pair := vm.NewArray([]engine.Value{engine.Number(float64(i)), getAt(vm, it.obj, i)})
		return engine.Value{O: vm.NewIterResult(engine.Value{O: pair}, false)}
	})
	vm.Define(vm.ArrayIteratorProto, vm.K.SymToStringTag, vm.NewString("Array Iterator"), engine.FlagConfigurable)

	method(vm, vm.StringIteratorProto, "next", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var it *stringIterator
		if this.IsObject() {
			it, _ = this.O.Ext.(*stringIterator)
		}
		if it == nil {
			vm.ThrowTypeError("next method called on an object that is not a string iterator")
		}
		if it.pos >= len(it.str) {
			return engine.Value{O: vm.NewIterResult(engine.Undefined, true)}
		}
		_, size := wtf8.DecodeRune(it.str[it.pos:])
		s := it.str[it.pos : it.pos+size]
		it.pos += size
		return engine.Value{O: vm.NewIterResult(vm.NewString(s), false)}
	})
	vm.Define(vm.StringIteratorProto, vm.K.SymToStringTag, vm.NewString("String Iterator"), engine.FlagConfigurable)

	gp := vm.GeneratorProto
	resume := func(name string, mode engine.ResumeMode) {
		method(vm, gp, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			var g *engine.Generator
			if this.IsObject() {
				g, _ = this.O.Ext.(*engine.Generator)
			}
			if g == nil || g.Async != nil {
				vm.ThrowTypeError(name + " method called on an object that is not a generator")
			}
			v, done := vm.Resume(g, mode, arg(args, 0))
			if !done && g.Delegating {
				// yield* passes the result object of the inner iterator through.
				return v
			}
			return engine.Value{O: vm.NewIterResult(v, done)}
		})
	}
	resume("next", engine.ResumeNext)
	resume("return", engine.ResumeReturn)
	resume("throw", engine.ResumeThrow)
	vm.Define(gp, vm.K.SymToStringTag, vm.NewString("Generator"), engine.FlagConfigurable)

	// The prototype chain of generator functions.
	gf := vm.GeneratorFunctionProto
	vm.Define(gf, vm.K.Prototype, engine.Value{O: gp}, engine.FlagConfigurable)
	vm.Define(gp, vm.K.Constructor, engine.Value{O: gf}, engine.FlagConfigurable)
	vm.Define(gf, vm.K.SymToStringTag, vm.NewString("GeneratorFunction"), engine.FlagConfigurable)
}
