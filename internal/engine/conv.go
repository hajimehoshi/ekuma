// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
func (cx *ExecutionContext) ToPrimitive(v Value, h Hint) Value {
	if !v.IsObject() {
		return v
	}
	if m := cx.GetMethod(v, cx.WellKnownSymbol(SymbolToPrimitive)); m != nil {
		var name wtf8.String = "default"
		switch h {
		case HintNumber:
			name = "number"
		case HintString:
			name = "string"
		}
		r := cx.Call(m, v, cx.NewString(name))
		if r.IsObject() {
			cx.ThrowTypeError("cannot convert object to primitive value")
		}
		return r
	}
	return cx.OrdinaryToPrimitive(v.O, h)
}

// OrdinaryToPrimitive implements OrdinaryToPrimitive. h is [HintString] or [HintNumber].
func (cx *ExecutionContext) OrdinaryToPrimitive(o *Object, h Hint) Value {
	first, second := cx.Key(keyValueOf), cx.Key(KeyToString)
	if h == HintString {
		first, second = second, first
	}
	v := Value{O: o}
	for _, name := range [2]*Object{first, second} {
		if m := cx.GetV(v, name); m.IsCallable() {
			if r := cx.Call(m.O, v); !r.IsObject() {
				return r
			}
		}
	}
	cx.ThrowTypeError("cannot convert object to primitive value")
	return storage.Undefined
}

// GetMethod implements GetMethod: the callable property key of v, or nil when it is undefined or null.
func (cx *ExecutionContext) GetMethod(v Value, key *Object) *Object {
	m := cx.GetV(v, key)
	if m.IsNullish() {
		return nil
	}
	if !m.IsCallable() {
		cx.ThrowTypeError(wtf8.ToValidUTF8(cx.KeyString(storage.NamedKey(key))) + " is not a function")
	}
	return m.O
}

// ToNumber implements ToNumber.
func (cx *ExecutionContext) ToNumber(v Value) float64 {
	o := v.O
	if o == nil {
		return math.NaN()
	}
	if o == storage.NumberObject {
		return v.N
	}
	switch o.Kind {
	case kindNull, kindFalse:
		return 0
	case kindTrue:
		return 1
	case kindString:
		if len(o.Text) <= shortstrings.MaxBytes {
			if n, ok := shortstrings.ParseDecimal(math.Float64bits(v.N), len(o.Text)); ok {
				return float64(n)
			}
		}
		return numconv.StringToNumber(v.Str(), cx.Poll)
	case kindSymbol:
		cx.ThrowTypeError("cannot convert a Symbol value to a number")
	case kindBigInt:
		cx.ThrowTypeError("cannot convert a BigInt value to a number")
	}
	return cx.ToNumber(cx.ToPrimitive(v, HintNumber))
}

// ToNumeric implements ToNumeric: the result is a number or a BigInt.
func (cx *ExecutionContext) ToNumeric(v Value) Value {
	if v.O == storage.NumberObject {
		return v
	}
	if v.IsObject() {
		v = cx.ToPrimitive(v, HintNumber)
	}
	if v.O != nil && v.O.Kind == kindBigInt {
		return v
	}
	return storage.Number(cx.ToNumber(v))
}

// ToIntegerOrInfinity implements ToIntegerOrInfinity.
func (cx *ExecutionContext) ToIntegerOrInfinity(v Value) float64 {
	if v.O == storage.NumberObject {
		return numconv.ToIntegerOrInfinity(v.N)
	}
	return numconv.ToIntegerOrInfinity(cx.ToNumber(v))
}

// ToInt32 implements ToInt32.
func (cx *ExecutionContext) ToInt32(v Value) int32 {
	if v.O == storage.NumberObject {
		return numconv.ToInt32(v.N)
	}
	return numconv.ToInt32(cx.ToNumber(v))
}

// ToUint32 implements ToUint32.
func (cx *ExecutionContext) ToUint32(v Value) uint32 {
	if v.O == storage.NumberObject {
		return numconv.ToUint32(v.N)
	}
	return numconv.ToUint32(cx.ToNumber(v))
}

// ToString implements ToString.
func (cx *ExecutionContext) ToString(v Value) wtf8.String {
	o := v.O
	if o == nil {
		return "undefined"
	}
	if o == storage.NumberObject {
		return numconv.NumberToString(v.N)
	}
	switch o.Kind {
	case kindString:
		return v.Str()
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case kindSymbol:
		cx.ThrowTypeError("cannot convert a Symbol value to a string")
	case kindBigInt:
		return cx.BigIntToString(v, 10)
	}
	return cx.ToString(cx.ToPrimitive(v, HintString))
}

// ToStringValue implements ToString and returns the result as a value.
func (cx *ExecutionContext) ToStringValue(v Value) Value {
	if v.O != nil && v.O.Kind == kindString {
		return v
	}
	return cx.toStringValue(v)
}

func (cx *ExecutionContext) toStringValue(v Value) Value {
	if v.IsObject() {
		return cx.ToStringValue(cx.ToPrimitive(v, HintString))
	}
	if v.IsNumber() {
		return cx.Quiet().NumberToString(v.N)
	}
	return cx.NewString(cx.ToString(v))
}

// ToObject implements ToObject.
func (cx *ExecutionContext) ToObject(v Value) *Object {
	o := v.O
	if o != nil && o.Kind >= kindFirstObject {
		return o
	}
	if o == nil {
		cx.ThrowTypeError("cannot convert undefined or null to object")
	}
	if o == storage.NumberObject {
		return storage.NewObjectWithState(cx.ShapeFor(cx.Intrinsic(IntrinsicNumberPrototype)), classNumber, v.N)
	}
	switch o.Kind {
	case kindNull:
		cx.ThrowTypeError("cannot convert undefined or null to object")
	case kindFalse, kindTrue:
		return storage.NewObjectWithState(cx.ShapeFor(cx.Intrinsic(IntrinsicBooleanPrototype)), classBoolean, o.Kind == kindTrue)
	case kindString:
		return cx.NewStringObject(v.StringObject(), cx.Intrinsic(IntrinsicStringPrototype))
	case kindSymbol:
		return storage.NewObjectWithState(cx.ShapeFor(cx.Intrinsic(IntrinsicSymbolPrototype)), classSymbol, o)
	case kindBigInt:
		return storage.NewObjectWithState(cx.ShapeFor(cx.Intrinsic(IntrinsicBigIntPrototype)), classBigInt, v)
	}
	panic("ekuma: internal value reached ToObject")
}

// NewStringObject returns the String object that wraps the string s.
func (cx *ExecutionContext) NewStringObject(s *Object, proto *Object) *Object {
	w := storage.NewObjectWithState(cx.ShapeFor(proto), classString, s)
	cx.Add(w, cx.Key(KeyLength), storage.Number(float64(s.StringLength())), 0)
	return w
}

// RequireObjectCoercible throws a TypeError when v is undefined or null.
func (cx *ExecutionContext) RequireObjectCoercible(v Value, what string) {
	if v.IsNullish() {
		cx.ThrowTypeError(what + " called on null or undefined")
	}
}

// ToBigInt implements ToBigInt. The result is a BigInt.
func (cx *ExecutionContext) ToBigInt(v Value) Value {
	p := cx.ToPrimitive(v, HintNumber)
	if p.O == storage.NumberObject {
		cx.ThrowTypeError("cannot convert " + wtf8.ToValidUTF8(numconv.NumberToString(p.N)) + " to a BigInt")
	}
	if p.IsUndefined() {
		cx.ThrowTypeError("cannot convert undefined to a BigInt")
	}
	switch p.O.Kind {
	case kindBigInt:
		return p
	case kindNull:
		cx.ThrowTypeError("cannot convert " + wtf8.ToValidUTF8(cx.ToString(p)) + " to a BigInt")
	case kindFalse:
		return storage.BigIntFromInt64(0)
	case kindTrue:
		return storage.BigIntFromInt64(1)
	case kindString:
		i, err := numconv.StringToBigInt(p.Str(), MaxBigIntBits, cx.Poll)
		switch {
		case errors.Is(err, numconv.ErrNotBigInt):
			cx.ThrowSyntaxError("cannot convert " + cx.DisplayString(p) + " to a BigInt")
		case errors.Is(err, numconv.ErrBigIntTooLarge):
			cx.ThrowRangeError("maximum BigInt size exceeded")
		}
		return storage.NewBigInt(i)
	case kindSymbol:
		cx.ThrowTypeError("cannot convert a Symbol value to a BigInt")
	}
	panic("ekuma: internal value reached ToBigInt")
}

// looseEquals implements IsLooselyEqual.
func (cx *ExecutionContext) looseEquals(a, b Value) bool {
	for {
		if a.O == storage.NumberObject && b.O == storage.NumberObject {
			return a.N == b.N
		}
		if a.O != nil && b.O != nil && a.O.Kind == b.O.Kind {
			return storage.IsStrictlyEqual(a, b)
		}
		an, bn := a.IsNullish(), b.IsNullish()
		if an || bn {
			// B.3.6.2: an object with [[IsHTMLDDA]] equals undefined and null.
			return an && bn || a.IsHTMLDDA() || b.IsHTMLDDA()
		}
		ak, bk := kindOf(a), kindOf(b)
		switch {
		case a.IsObject() && b.IsObject():
			return a.O == b.O
		case a.IsObject():
			a = cx.ToPrimitive(a, HintDefault)
		case b.IsObject():
			b = cx.ToPrimitive(b, HintDefault)
		case a.IsBool():
			a = storage.Number(cx.ToNumber(a))
		case b.IsBool():
			b = storage.Number(cx.ToNumber(b))
		case ak == kindSymbol || bk == kindSymbol:
			return false
		case ak == kindBigInt || bk == kindBigInt:
			return cx.compareBigIntMixed(a, b) == cmpEqual
		default:
			// A number and a string.
			return cx.ToNumber(a) == cx.ToNumber(b)
		}
	}
}

// kindOf returns the kind of v.
func kindOf(v Value) kind {
	if v.O == nil {
		return kindUndefined
	}
	return v.O.Kind
}

// compareBigIntMixed compares a BigInt with a BigInt, a number or a string, in either order.
func (cx *ExecutionContext) compareBigIntMixed(a, b Value) cmpResult {
	if x, ok := a.BigInt64(); ok {
		if y, ok := b.BigInt64(); ok {
			return storage.CompareInt64s(x, y)
		}
		if b.O == storage.NumberObject {
			return storage.CompareInt64Number(x, b.N)
		}
	} else if y, ok := b.BigInt64(); ok && a.O == storage.NumberObject {
		return storage.CompareInt64Number(y, a.N).Reverse()
	}
	// toBig converts v to a BigInt or a number. huge is the sign of a string whose value is
	// beyond the size of BigInts, or 0.
	toBig := func(v Value) (i *big.Int, f float64, huge int, ok bool) {
		if v.O == storage.NumberObject {
			return nil, v.N, 0, true
		}
		if v.O == nil {
			return nil, 0, 0, false
		}
		switch v.O.Kind {
		case kindBigInt:
			return v.BigInt(), 0, 0, true
		case kindString:
			i, err := numconv.StringToBigInt(v.Str(), MaxBigIntBits, cx.Poll)
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
		return compareBigFloat(y, xf).Reverse()
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
func (cx *ExecutionContext) compare(a, b Value, leftFirst bool) cmpResult {
	if a.O == storage.NumberObject && b.O == storage.NumberObject {
		return storage.CompareNumbers(a.N, b.N)
	}
	if leftFirst {
		a = cx.ToPrimitive(a, HintNumber)
		b = cx.ToPrimitive(b, HintNumber)
	} else {
		b = cx.ToPrimitive(b, HintNumber)
		a = cx.ToPrimitive(a, HintNumber)
	}
	if a.IsString() && b.IsString() {
		return cmpResult(storage.CompareStrings(a, b) + 1)
	}
	if kindOf(a) == kindBigInt || kindOf(b) == kindBigInt {
		if !a.IsNumber() && !a.IsBigInt() && !a.IsString() {
			a = storage.Number(cx.ToNumber(a))
		}
		if !b.IsNumber() && !b.IsBigInt() && !b.IsString() {
			b = storage.Number(cx.ToNumber(b))
		}
		return cx.compareBigIntMixed(a, b)
	}
	return storage.CompareNumbers(cx.ToNumber(a), cx.ToNumber(b))
}

// addValues implements the + operator for operands that are not both numbers.
func (cx *ExecutionContext) addValues(a, b Value) Value {
	if x, ok := a.BigInt64(); ok {
		if y, ok := b.BigInt64(); ok {
			if s, ok := storage.Add64(x, y); ok {
				return storage.BigIntFromInt64(s)
			}
		}
	}
	if a.IsObject() {
		a = cx.ToPrimitive(a, HintDefault)
	}
	if b.IsObject() {
		b = cx.ToPrimitive(b, HintDefault)
	}
	as, bs := a.IsString(), b.IsString()
	if as && bs {
		return cx.Concat(a, b)
	}
	if as || bs {
		// The sum of a number and the empty string, in either order, is the string of the number.
		// The str of every kind of string has the byte length of its contents.
		if as && b.O == storage.NumberObject && len(a.O.Text) == 0 {
			return cx.toStringValue(b)
		}
		if bs && a.O == storage.NumberObject && len(b.O.Text) == 0 {
			return cx.toStringValue(a)
		}
		var x, y wtf8.String
		if as {
			x = a.RawString()
		} else {
			x = cx.ToString(a)
		}
		if bs {
			y = b.RawString()
		} else {
			y = cx.ToString(b)
		}
		return cx.concat(a, x, b, y)
	}
	a, b = cx.ToNumeric(a), cx.ToNumeric(b)
	if a.O == storage.NumberObject && b.O == storage.NumberObject {
		return storage.Number(a.N + b.N)
	}
	if a.O == storage.NumberObject || b.O == storage.NumberObject {
		cx.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	return cx.addBigInts(a, b)
}

// LengthOfArrayLike implements LengthOfArrayLike.
func (cx *ExecutionContext) LengthOfArrayLike(o *Object) int64 {
	if o.Kind == kindArray {
		return int64(o.Length)
	}
	return numconv.ToLength(cx.ToNumber(cx.GetV(Value{O: o}, cx.Key(KeyLength))))
}
