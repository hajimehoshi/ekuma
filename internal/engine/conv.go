// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"
	"math"
	"math/big"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Hint is the preferred type of ToPrimitive.
type Hint uint8

const (
	HintDefault Hint = iota
	HintNumber
	HintString
)

// ToPrimitive implements ToPrimitive.
func (vm *VM) ToPrimitive(v Value, h Hint) Value {
	if !v.IsObject() {
		return v
	}
	if m := vm.GetMethod(v, vm.WellKnownSymbol(SymbolToPrimitive)); m != nil {
		var name wtf8.String = "default"
		switch h {
		case HintNumber:
			name = "number"
		case HintString:
			name = "string"
		}
		r := vm.Call(m, v, vm.NewString(name))
		if r.IsObject() {
			vm.ThrowTypeError("cannot convert object to primitive value")
		}
		return r
	}
	return vm.OrdinaryToPrimitive(v.o, h)
}

// OrdinaryToPrimitive implements OrdinaryToPrimitive. h is [HintString] or [HintNumber].
func (vm *VM) OrdinaryToPrimitive(o *Object, h Hint) Value {
	first, second := vm.Key(keyValueOf), vm.Key(KeyToString)
	if h == HintString {
		first, second = second, first
	}
	v := Value{o: o}
	for _, name := range [2]*Object{first, second} {
		if m := vm.GetV(v, name); m.IsCallable() {
			if r := vm.Call(m.o, v); !r.IsObject() {
				return r
			}
		}
	}
	vm.ThrowTypeError("cannot convert object to primitive value")
	return Undefined
}

// GetMethod implements GetMethod: the callable property key of v, or nil when it is undefined or null.
func (vm *VM) GetMethod(v Value, key *Object) *Object {
	m := vm.GetV(v, key)
	if m.IsNullish() {
		return nil
	}
	if !m.IsCallable() {
		vm.ThrowTypeError(wtf8.ToValidUTF8(vm.KeyString(PropKey{key: key})) + " is not a function")
	}
	return m.o
}

// ToNumber implements ToNumber.
func (vm *VM) ToNumber(v Value) float64 {
	o := v.o
	if o == nil {
		return math.NaN()
	}
	if o == numberObject {
		return v.n
	}
	switch o.kind {
	case kindNull, kindFalse:
		return 0
	case kindTrue:
		return 1
	case kindString:
		if len(o.str) <= shortstrings.MaxBytes {
			if n, ok := shortstrings.ParseDecimal(math.Float64bits(v.n), len(o.str)); ok {
				return float64(n)
			}
		}
		return numconv.StringToNumber(v.Str(), vm.Poll)
	case kindSymbol:
		vm.ThrowTypeError("cannot convert a Symbol value to a number")
	case kindBigInt:
		vm.ThrowTypeError("cannot convert a BigInt value to a number")
	}
	return vm.ToNumber(vm.ToPrimitive(v, HintNumber))
}

// ToNumeric implements ToNumeric: the result is a number or a BigInt.
func (vm *VM) ToNumeric(v Value) Value {
	if v.o == numberObject {
		return v
	}
	if v.IsObject() {
		v = vm.ToPrimitive(v, HintNumber)
	}
	if v.o != nil && v.o.kind == kindBigInt {
		return v
	}
	return Number(vm.ToNumber(v))
}

// ToIntegerOrInfinity implements ToIntegerOrInfinity.
func (vm *VM) ToIntegerOrInfinity(v Value) float64 {
	if v.o == numberObject {
		return numconv.ToIntegerOrInfinity(v.n)
	}
	return numconv.ToIntegerOrInfinity(vm.ToNumber(v))
}

// ToInt32 implements ToInt32.
func (vm *VM) ToInt32(v Value) int32 {
	if v.o == numberObject {
		return numconv.ToInt32(v.n)
	}
	return numconv.ToInt32(vm.ToNumber(v))
}

// ToUint32 implements ToUint32.
func (vm *VM) ToUint32(v Value) uint32 {
	if v.o == numberObject {
		return numconv.ToUint32(v.n)
	}
	return numconv.ToUint32(vm.ToNumber(v))
}

// ToString implements ToString.
func (vm *VM) ToString(v Value) wtf8.String {
	o := v.o
	if o == nil {
		return "undefined"
	}
	if o == numberObject {
		return numconv.NumberToString(v.n)
	}
	switch o.kind {
	case kindString:
		return v.Str()
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case kindSymbol:
		vm.ThrowTypeError("cannot convert a Symbol value to a string")
	case kindBigInt:
		return vm.BigIntToString(v, 10)
	}
	return vm.ToString(vm.ToPrimitive(v, HintString))
}

// ToStringValue implements ToString and returns the result as a value.
func (vm *VM) ToStringValue(v Value) Value {
	if v.o != nil && v.o.kind == kindString {
		return v
	}
	return vm.toStringValue(v)
}

func (vm *VM) toStringValue(v Value) Value {
	if v.IsObject() {
		return vm.ToStringValue(vm.ToPrimitive(v, HintString))
	}
	if v.IsNumber() {
		var buf [32]byte
		text := numconv.AppendNumber(buf[:0], v.n)
		if len(text) == 1 {
			return vm.strings.ascii[text[0]]
		}
		if len(text) <= shortstrings.MaxBytes {
			return vm.shortString(wtf8.String(text))
		}
		return vm.NewString(wtf8.String(text))
	}
	return vm.NewString(vm.ToString(v))
}

// ToObject implements ToObject.
func (vm *VM) ToObject(v Value) *Object {
	o := v.o
	if o != nil && o.kind >= kindFirstObject {
		return o
	}
	if o == nil {
		vm.ThrowTypeError("cannot convert undefined or null to object")
	}
	if o == numberObject {
		w := vm.NewObject(vm.Intrinsic(IntrinsicNumberPrototype))
		w.class = classNumber
		w.setExtState(v.n)
		return w
	}
	switch o.kind {
	case kindNull:
		vm.ThrowTypeError("cannot convert undefined or null to object")
	case kindFalse, kindTrue:
		w := vm.NewObject(vm.Intrinsic(IntrinsicBooleanPrototype))
		w.class = classBoolean
		w.setExtState(o.kind == kindTrue)
		return w
	case kindString:
		return vm.NewStringObject(v.stringObject(), vm.Intrinsic(IntrinsicStringPrototype))
	case kindSymbol:
		w := vm.NewObject(vm.Intrinsic(IntrinsicSymbolPrototype))
		w.class = classSymbol
		w.setExtState(o)
		return w
	case kindBigInt:
		w := vm.NewObject(vm.Intrinsic(IntrinsicBigIntPrototype))
		w.class = classBigInt
		w.setExtState(v)
		return w
	}
	panic("ekuma: internal value reached ToObject")
}

// NewStringObject returns the String object that wraps the string s.
func (vm *VM) NewStringObject(s *Object, proto *Object) *Object {
	w := vm.NewObject(proto)
	w.class = classString
	w.setExtState(s)
	vm.add(w, vm.Key(KeyLength), Number(float64(s.StringLength())), 0)
	return w
}

// RequireObjectCoercible throws a TypeError when v is undefined or null.
func (vm *VM) RequireObjectCoercible(v Value, what string) {
	if v.IsNullish() {
		vm.ThrowTypeError(what + " called on null or undefined")
	}
}

// ToBigInt implements ToBigInt. The result is a BigInt.
func (vm *VM) ToBigInt(v Value) Value {
	p := vm.ToPrimitive(v, HintNumber)
	if p.o == numberObject {
		vm.ThrowTypeError("cannot convert " + wtf8.ToValidUTF8(numconv.NumberToString(p.n)) + " to a BigInt")
	}
	if p.IsUndefined() {
		vm.ThrowTypeError("cannot convert undefined to a BigInt")
	}
	switch p.o.kind {
	case kindBigInt:
		return p
	case kindNull:
		vm.ThrowTypeError("cannot convert " + wtf8.ToValidUTF8(vm.ToString(p)) + " to a BigInt")
	case kindFalse:
		return BigIntFromInt64(0)
	case kindTrue:
		return BigIntFromInt64(1)
	case kindString:
		i, err := numconv.StringToBigInt(p.Str(), MaxBigIntBits, vm.Poll)
		switch {
		case errors.Is(err, numconv.ErrNotBigInt):
			vm.throwSyntaxError("cannot convert " + vm.DisplayString(p) + " to a BigInt")
		case errors.Is(err, numconv.ErrBigIntTooLarge):
			vm.ThrowRangeError("maximum BigInt size exceeded")
		}
		return vm.NewBigInt(i)
	case kindSymbol:
		vm.ThrowTypeError("cannot convert a Symbol value to a BigInt")
	}
	panic("ekuma: internal value reached ToBigInt")
}

// looseEquals implements IsLooselyEqual.
func (vm *VM) looseEquals(a, b Value) bool {
	for {
		if a.o == numberObject && b.o == numberObject {
			return a.n == b.n
		}
		if a.o != nil && b.o != nil && a.o.kind == b.o.kind {
			return IsStrictlyEqual(a, b)
		}
		an, bn := a.IsNullish(), b.IsNullish()
		if an || bn {
			// B.3.6.2: an object with [[IsHTMLDDA]] equals undefined and null.
			return an && bn || a.isHTMLDDA() || b.isHTMLDDA()
		}
		ak, bk := kindOf(a), kindOf(b)
		switch {
		case a.IsObject() && b.IsObject():
			return a.o == b.o
		case a.IsObject():
			a = vm.ToPrimitive(a, HintDefault)
		case b.IsObject():
			b = vm.ToPrimitive(b, HintDefault)
		case a.IsBool():
			a = Number(vm.ToNumber(a))
		case b.IsBool():
			b = Number(vm.ToNumber(b))
		case ak == kindSymbol || bk == kindSymbol:
			return false
		case ak == kindBigInt || bk == kindBigInt:
			return vm.compareBigIntMixed(a, b) == cmpEqual
		default:
			// A number and a string.
			return vm.ToNumber(a) == vm.ToNumber(b)
		}
	}
}

// kindOf returns the kind of v.
func kindOf(v Value) kind {
	if v.o == nil {
		return kindUndefined
	}
	return v.o.kind
}

type cmpResult uint8

const (
	cmpLess cmpResult = iota
	cmpEqual
	cmpGreater
	cmpUndefined
)

// compareBigIntMixed compares a BigInt with a BigInt, a number or a string, in either order.
func (vm *VM) compareBigIntMixed(a, b Value) cmpResult {
	if x, ok := a.BigInt64(); ok {
		if y, ok := b.BigInt64(); ok {
			return compareInt64s(x, y)
		}
		if b.o == numberObject {
			return compareInt64Number(x, b.n)
		}
	} else if y, ok := b.BigInt64(); ok && a.o == numberObject {
		return compareInt64Number(y, a.n).reverse()
	}
	// toBig converts v to a BigInt or a number. huge is the sign of a string whose value is
	// beyond the size of BigInts, or 0.
	toBig := func(v Value) (i *big.Int, f float64, huge int, ok bool) {
		if v.o == numberObject {
			return nil, v.n, 0, true
		}
		if v.o == nil {
			return nil, 0, 0, false
		}
		switch v.o.kind {
		case kindBigInt:
			return v.BigInt(), 0, 0, true
		case kindString:
			i, err := numconv.StringToBigInt(v.Str(), MaxBigIntBits, vm.Poll)
			switch {
			case errors.Is(err, numconv.ErrNotBigInt):
				return nil, 0, 0, false
			case errors.Is(err, numconv.ErrBigIntTooLarge):
				return nil, 0, i.Sign(), true
			}
			return i, 0, 0, true
		}
		return nil, 0, 0, false
	}
	x, xf, xHuge, ok := toBig(a)
	if !ok {
		return cmpUndefined
	}
	y, yf, yHuge, ok := toBig(b)
	if !ok {
		return cmpUndefined
	}
	// The other operand of a string is a BigInt, which is smaller in magnitude.
	switch {
	case xHuge != 0:
		return cmpResult(xHuge + 1)
	case yHuge != 0:
		return cmpResult(-yHuge + 1)
	}
	switch {
	case x != nil && y != nil:
		return cmpResult(x.Cmp(y) + 1)
	case x != nil:
		return compareBigFloat(x, yf)
	case y != nil:
		return compareBigFloat(y, xf).reverse()
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
	if a.o == numberObject && b.o == numberObject {
		return compareNumbers(a.n, b.n)
	}
	if leftFirst {
		a = vm.ToPrimitive(a, HintNumber)
		b = vm.ToPrimitive(b, HintNumber)
	} else {
		b = vm.ToPrimitive(b, HintNumber)
		a = vm.ToPrimitive(a, HintNumber)
	}
	if a.IsString() && b.IsString() {
		return cmpResult(CompareStrings(a, b) + 1)
	}
	if kindOf(a) == kindBigInt || kindOf(b) == kindBigInt {
		if !a.IsNumber() && !a.IsBigInt() && !a.IsString() {
			a = Number(vm.ToNumber(a))
		}
		if !b.IsNumber() && !b.IsBigInt() && !b.IsString() {
			b = Number(vm.ToNumber(b))
		}
		return vm.compareBigIntMixed(a, b)
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
	if x, ok := a.BigInt64(); ok {
		if y, ok := b.BigInt64(); ok {
			if s, ok := add64(x, y); ok {
				return BigIntFromInt64(s)
			}
		}
	}
	a = vm.ToPrimitive(a, HintDefault)
	b = vm.ToPrimitive(b, HintDefault)
	as, bs := a.IsString(), b.IsString()
	if as && bs {
		return vm.Concat(a, b)
	}
	if as || bs {
		var x, y wtf8.String
		if as {
			x = a.rawString()
		} else {
			x = vm.ToString(a)
		}
		if bs {
			y = b.rawString()
		} else {
			y = vm.ToString(b)
		}
		return vm.concat(a, x, b, y)
	}
	a, b = vm.ToNumeric(a), vm.ToNumeric(b)
	if a.o == numberObject && b.o == numberObject {
		return Number(a.n + b.n)
	}
	if a.o == numberObject || b.o == numberObject {
		vm.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	return vm.addBigInts(a, b)
}

// MaxStringLength is the largest length a string may have, in bytes.
// It is smaller where an int has 32 bits.
const MaxStringLength = 1<<(27+3*(strconv.IntSize/64)) - 1
