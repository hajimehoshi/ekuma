// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type hint uint8

const (
	hintDefault hint = iota
	hintNumber
	hintString
)

// toPrimitive implements ToPrimitive.
func (vm *VM) toPrimitive(v Value, h hint) Value {
	if !v.IsObject() {
		return v
	}
	if m := vm.GetMethod(v, vm.K.SymToPrimitive); m != nil {
		name := "default"
		switch h {
		case hintNumber:
			name = "number"
		case hintString:
			name = "string"
		}
		r := vm.Call(m, v, vm.NewString(name))
		if r.IsObject() {
			vm.ThrowTypeError("Cannot convert object to primitive value")
		}
		return r
	}
	first, second := vm.K.valueOf, vm.K.ToString
	if h == hintString {
		first, second = second, first
	}
	for _, name := range [2]*Object{first, second} {
		if m := vm.GetV(v, name); m.IsCallable() {
			if r := vm.Call(m.O, v); !r.IsObject() {
				return r
			}
		}
	}
	vm.ThrowTypeError("Cannot convert object to primitive value")
	return Undefined
}

// getMethod implements GetMethod: the callable property key of v, or nil when it is undefined or null.
func (vm *VM) GetMethod(v Value, key *Object) *Object {
	m := vm.GetV(v, key)
	if m.IsNullish() {
		return nil
	}
	if !m.IsCallable() {
		vm.ThrowTypeError(vm.KeyString(PropKey{Key: key}) + " is not a function")
	}
	return m.O
}

// toNumber implements ToNumber.
func (vm *VM) ToNumber(v Value) float64 {
	o := v.O
	if o == nil {
		return v.N
	}
	switch o.Kind {
	case kindUndefined:
		return math.NaN()
	case kindNull, kindFalse:
		return 0
	case kindTrue:
		return 1
	case KindString:
		return numconv.StringToNumber(o.Str)
	case kindSymbol:
		vm.ThrowTypeError("Cannot convert a Symbol value to a number")
	case kindBigInt:
		vm.ThrowTypeError("Cannot convert a BigInt value to a number")
	}
	return vm.ToNumber(vm.toPrimitive(v, hintNumber))
}

// toNumeric implements ToNumeric: the result is a number or a BigInt.
func (vm *VM) ToNumeric(v Value) Value {
	if v.O == nil {
		return v
	}
	if v.O.Kind >= kindObject {
		v = vm.toPrimitive(v, hintNumber)
	}
	if v.O != nil && v.O.Kind == kindBigInt {
		return v
	}
	return Value{N: vm.ToNumber(v)}
}

// toInteger implements ToIntegerOrInfinity.
func (vm *VM) ToInteger(v Value) float64 {
	if v.O == nil {
		return numconv.ToIntegerOrInfinity(v.N)
	}
	return numconv.ToIntegerOrInfinity(vm.ToNumber(v))
}

func (vm *VM) ToInt32(v Value) int32 {
	if v.O == nil {
		return numconv.ToInt32(v.N)
	}
	return numconv.ToInt32(vm.ToNumber(v))
}

func (vm *VM) ToUint32(v Value) uint32 {
	if v.O == nil {
		return numconv.ToUint32(v.N)
	}
	return numconv.ToUint32(vm.ToNumber(v))
}

// toString implements ToString.
func (vm *VM) ToString(v Value) string {
	o := v.O
	if o == nil {
		return numconv.NumberToString(v.N)
	}
	switch o.Kind {
	case KindString:
		return o.Str
	case kindUndefined:
		return "undefined"
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case kindSymbol:
		vm.ThrowTypeError("Cannot convert a Symbol value to a string")
	case kindBigInt:
		return o.BigInt().String()
	}
	return vm.ToString(vm.toPrimitive(v, hintString))
}

// toStringValue implements ToString and returns the result as a value.
func (vm *VM) ToStringValue(v Value) Value {
	if v.O != nil && v.O.Kind == KindString {
		return v
	}
	return vm.NewString(vm.ToString(v))
}

// toObject implements ToObject.
func (vm *VM) ToObject(v Value) *Object {
	o := v.O
	if o != nil && o.Kind >= kindObject {
		return o
	}
	if o == nil {
		w := vm.NewObject(vm.NumberProto)
		w.Class = ClassNumber
		w.Ext = v.N
		return w
	}
	switch o.Kind {
	case kindUndefined, kindNull:
		vm.ThrowTypeError("Cannot convert undefined or null to object")
	case kindFalse, kindTrue:
		w := vm.NewObject(vm.BooleanProto)
		w.Class = ClassBoolean
		w.Ext = o.Kind == kindTrue
		return w
	case KindString:
		return vm.NewStringObject(o, vm.StringProto)
	case kindSymbol:
		w := vm.NewObject(vm.SymbolProto)
		w.Class = ClassSymbol
		w.Ext = o
		return w
	case kindBigInt:
		w := vm.NewObject(vm.BigintProto)
		w.Class = classBigInt
		w.Ext = o
		return w
	}
	panic("ekuma: internal value reached ToObject")
}

// newStringObject returns the String object that wraps the string s.
func (vm *VM) NewStringObject(s *Object, proto *Object) *Object {
	w := vm.NewObject(proto)
	w.Class = ClassString
	w.Ext = s
	vm.Add(w, vm.K.Length, Number(float64(s.StringLength())), 0)
	return w
}

// requireObjectCoercible throws a TypeError when v is undefined or null.
func (vm *VM) RequireObjectCoercible(v Value, what string) {
	if v.IsNullish() {
		vm.ThrowTypeError(what + " called on null or undefined")
	}
}

func (vm *VM) newBigInt(i *big.Int) Value {
	return Value{
		O: &Object{
			Kind: kindBigInt,
			Ext:  i,
		},
	}
}

// looseEquals implements IsLooselyEqual.
func (vm *VM) looseEquals(a, b Value) bool {
	for {
		if a.O == nil && b.O == nil {
			return a.N == b.N
		}
		if a.O != nil && b.O != nil && a.O.Kind == b.O.Kind {
			return StrictEquals(a, b)
		}
		an, bn := a.IsNullish(), b.IsNullish()
		if an || bn {
			return an && bn
		}
		ak, bk := kindNumber(a), kindNumber(b)
		switch {
		case a.IsObject() && b.IsObject():
			return a.O == b.O
		case a.IsObject():
			a = vm.toPrimitive(a, hintDefault)
		case b.IsObject():
			b = vm.toPrimitive(b, hintDefault)
		case a.IsBool():
			a = Value{N: vm.ToNumber(a)}
		case b.IsBool():
			b = Value{N: vm.ToNumber(b)}
		case ak == kindSymbol || bk == kindSymbol:
			return false
		case ak == kindBigInt || bk == kindBigInt:
			return compareBigIntMixed(a, b) == cmpEqual
		default:
			// A number and a string.
			return vm.ToNumber(a) == vm.ToNumber(b)
		}
	}
}

// kindNumber returns the kind of v, with kindUndefined standing for a number never being asked about.
func kindNumber(v Value) kind {
	if v.O == nil {
		return kindObject
	}
	return v.O.Kind
}

type cmpResult uint8

const (
	cmpLess cmpResult = iota
	cmpEqual
	cmpGreater
	cmpUndefined
)

// compareBigIntMixed compares a BigInt with a BigInt, a number or a string, in either order.
func compareBigIntMixed(a, b Value) cmpResult {
	toBig := func(v Value) (*big.Int, float64, bool) {
		if v.O == nil {
			return nil, v.N, true
		}
		switch v.O.Kind {
		case kindBigInt:
			return v.O.BigInt(), 0, true
		case KindString:
			i := numconv.StringToBigInt(v.O.Str)
			return i, 0, i != nil
		}
		return nil, 0, false
	}
	x, xf, ok := toBig(a)
	if !ok {
		return cmpUndefined
	}
	y, yf, ok := toBig(b)
	if !ok {
		return cmpUndefined
	}
	switch {
	case x != nil && y != nil:
		return cmpResult(x.Cmp(y) + 1)
	case x != nil:
		return compareBigFloat(x, yf)
	case y != nil:
		switch compareBigFloat(y, xf) {
		case cmpLess:
			return cmpGreater
		case cmpGreater:
			return cmpLess
		case cmpEqual:
			return cmpEqual
		}
		return cmpUndefined
	}
	return cmpUndefined
}

func compareBigFloat(x *big.Int, f float64) cmpResult {
	switch {
	case f != f:
		return cmpUndefined
	case math.IsInf(f, 1):
		return cmpLess
	case math.IsInf(f, -1):
		return cmpGreater
	}
	return cmpResult(new(big.Float).SetInt(x).Cmp(big.NewFloat(f)) + 1)
}

// compare implements IsLessThan for a and b, converting a first when leftFirst is set.
func (vm *VM) compare(a, b Value, leftFirst bool) cmpResult {
	if a.O == nil && b.O == nil {
		return compareNumbers(a.N, b.N)
	}
	if leftFirst {
		a = vm.toPrimitive(a, hintNumber)
		b = vm.toPrimitive(b, hintNumber)
	} else {
		b = vm.toPrimitive(b, hintNumber)
		a = vm.toPrimitive(a, hintNumber)
	}
	if a.IsString() && b.IsString() {
		return cmpResult(wtf8.Compare(a.O.Str, b.O.Str) + 1)
	}
	if kindNumber(a) == kindBigInt || kindNumber(b) == kindBigInt {
		if a.O != nil && a.O.Kind != kindBigInt && a.O.Kind != KindString {
			a = Value{N: vm.ToNumber(a)}
		}
		if b.O != nil && b.O.Kind != kindBigInt && b.O.Kind != KindString {
			b = Value{N: vm.ToNumber(b)}
		}
		return compareBigIntMixed(a, b)
	}
	return compareNumbers(vm.ToNumber(a), vm.ToNumber(b))
}

func compareNumbers(x, y float64) cmpResult {
	switch {
	case x < y:
		return cmpLess
	case x > y:
		return cmpGreater
	case x == y:
		return cmpEqual
	}
	return cmpUndefined
}

// addValues implements the + operator for operands that are not both numbers.
func (vm *VM) addValues(a, b Value) Value {
	a = vm.toPrimitive(a, hintDefault)
	b = vm.toPrimitive(b, hintDefault)
	as, bs := a.IsString(), b.IsString()
	if as || bs {
		var x, y string
		if as {
			x = a.O.Str
		} else {
			x = vm.ToString(a)
		}
		if bs {
			y = b.O.Str
		} else {
			y = vm.ToString(b)
		}
		return vm.concat(a, x, b, y)
	}
	a, b = vm.ToNumeric(a), vm.ToNumeric(b)
	if a.O == nil && b.O == nil {
		return Value{N: a.N + b.N}
	}
	if a.O == nil || b.O == nil {
		vm.ThrowTypeError("Cannot mix BigInt and other types, use explicit conversions")
	}
	return vm.newBigInt(new(big.Int).Add(a.O.BigInt(), b.O.BigInt()))
}

// maxStringLength is the largest length a string may have, in bytes.
const MaxStringLength = 1<<30 - 1

// concat returns the concatenation of the strings x and y, which are the contents of a and b
// when those are strings.
func (vm *VM) concat(a Value, x string, b Value, y string) Value {
	if x == "" && b.IsString() {
		return b
	}
	if y == "" && a.IsString() {
		return a
	}
	if len(x)+len(y) > MaxStringLength {
		vm.ThrowRangeError("Invalid string length")
	}
	var s string
	if len(y) >= 3 && len(x) >= 3 && wtf8.JoinsPair(x, y) {
		s = wtf8.Concat(x, y)
	} else {
		s = x + y
	}
	return vm.NewString(s)
}
