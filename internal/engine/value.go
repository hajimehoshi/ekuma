// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package engine implements the JavaScript virtual machine: values, objects, the interpreter
// and the abstract operations of the language. The built-in objects are in the package builtin.
package engine

import (
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type kind uint8

const (
	kindUndefined kind = iota
	kindNumber
	kindNull
	kindFalse
	kindTrue
	kindString
	kindSymbol
	kindBigInt

	// kindHole is the content of an array element that does not exist. It never reaches JavaScript.
	kindHole

	// kindAccessor is the content of the slot of an accessor property. It never reaches JavaScript.
	kindAccessor

	// kindHTMLDDA is an object with the [[IsHTMLDDA]] internal slot (B.3.6).
	// It comes before kindObject, so that a check for kindObject or above excludes it.
	kindHTMLDDA
	kindObject
	kindArray
	kindFunction
)

// Every kind from kindFirstObject on is an object in the JavaScript sense.
const kindFirstObject = kindHTMLDDA

// Value is a JavaScript value. The zero value is undefined.
// Values cannot be compared with ==; use [IsStrictlyEqual], [SameValue], or [SameValueZero].
type Value struct {
	// _ makes [Value] uncomparable.
	_ [0]func()

	n float64

	// o is nil for undefined, numberObject for the number n, bigInt64Object for the BigInt
	// whose two's complement bits n holds, and the object for anything else.
	// Every string of at most shortstrings.MaxBytes bytes holds its bytes in n.
	// Its object carries the byte length and realm, and may also be an interned key.
	o *Object
}

var (
	numberObject = &Object{kind: kindNumber}
	nullObject   = &Object{kind: kindNull}
	falseObject  = &Object{kind: kindFalse}
	trueObject   = &Object{kind: kindTrue}
	holeObject   = &Object{kind: kindHole}

	// bigInt64Object is the o of every BigInt that fits in an int64. Every other BigInt is an
	// object of its own holding a *big.Int, so each BigInt has exactly one representation.
	bigInt64Object = &Object{kind: kindBigInt}

	// Undefined is the value undefined.
	Undefined Value

	// Null is the value null.
	Null = Value{o: nullObject}

	// False is the value false.
	False = Value{o: falseObject}

	// True is the value true.
	True = Value{o: trueObject}

	hole = Value{o: holeObject}
)

// Number returns the number n as a value.
func Number(n float64) Value {
	return Value{
		n: n,
		o: numberObject,
	}
}

// BigIntFromInt64 returns the BigInt i.
func BigIntFromInt64(i int64) Value {
	return Value{
		n: math.Float64frombits(uint64(i)),
		o: bigInt64Object,
	}
}

// Bool returns the boolean b as a value.
func Bool(b bool) Value {
	o := falseObject
	if b {
		o = trueObject
	}
	return Value{o: o}
}

// ObjectValue returns o as a value.
func ObjectValue(o *Object) Value {
	v := Value{
		o: o,
	}
	if v.isShortString() {
		v.n = math.Float64frombits(shortstrings.Pack(o.Str()))
	}
	return v
}

// Object returns the object, string, symbol or BigInt that v is. It returns nil for a number
// or undefined, and one shared object for null, for each boolean, and for every BigInt that
// fits in an int64.
// For primitive strings, the returned object's identity is not guaranteed.
func (v Value) Object() *Object {
	if v.IsNumber() {
		return nil
	}
	return v.stringObject()
}

// Number returns the number v. v must be a number.
func (v Value) Number() float64 {
	return v.n
}

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool {
	return v.o == numberObject
}

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool {
	return v.o == nil
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.o == nullObject
}

// IsBool reports whether v is a boolean.
func (v Value) IsBool() bool {
	return v.o == trueObject || v.o == falseObject
}

// IsString reports whether v is a string.
func (v Value) IsString() bool {
	return v.o != nil && v.o.kind == kindString
}

// IsSymbol reports whether v is a symbol.
func (v Value) IsSymbol() bool {
	return v.o != nil && v.o.kind == kindSymbol
}

// IsBigInt reports whether v is a BigInt.
func (v Value) IsBigInt() bool {
	return v.o != nil && v.o.kind == kindBigInt
}

// BigInt64 returns the BigInt v as an int64, and reports whether v is a BigInt that fits in one.
func (v Value) BigInt64() (int64, bool) {
	if v.o != bigInt64Object {
		return 0, false
	}
	return int64(math.Float64bits(v.n)), true
}

// BigInt returns the BigInt v, which must not be modified.
func (v Value) BigInt() *big.Int {
	if i, ok := v.BigInt64(); ok {
		return big.NewInt(i)
	}
	return v.o.extState().(*big.Int)
}

// IsObject reports whether v is an object, which includes functions and arrays.
func (v Value) IsObject() bool {
	return v.o != nil && v.o.kind >= kindFirstObject
}

// IsNullish reports whether v is undefined or null.
func (v Value) IsNullish() bool {
	return v.o == nil || v.o == nullObject
}

func (v Value) isHTMLDDA() bool {
	return v.o != nil && v.o.kind == kindHTMLDDA
}

func (v Value) isHole() bool {
	return v.o == holeObject
}

// IsCallable implements IsCallable.
func (v Value) IsCallable() bool {
	return v.o != nil && v.o.fn != nil
}

// ToBoolean implements ToBoolean.
func ToBoolean(v Value) bool {
	o := v.o
	if o == nil {
		return false
	}
	if o == numberObject {
		return v.n != 0 && v.n == v.n
	}
	switch o.kind {
	case kindNull, kindFalse, kindHTMLDDA:
		return false
	case kindString:
		return len(o.str) != 0
	case kindBigInt:
		// Zero fits in an int64.
		return o != bigInt64Object || math.Float64bits(v.n) != 0
	}
	return true
}

// The results of the typeof operator.
const (
	typeofUndefined = iota
	typeofNumber
	typeofBoolean
	typeofString
	typeofSymbol
	typeofBigInt
	typeofFunction
	typeofObject
	typeofCount
)

// typeofNames are the results of the typeof operator, indexed by the typeof constants.
var typeofNames = [typeofCount]wtf8.String{
	typeofUndefined: "undefined",
	typeofNumber:    "number",
	typeofBoolean:   "boolean",
	typeofString:    "string",
	typeofSymbol:    "symbol",
	typeofBigInt:    "bigint",
	typeofFunction:  "function",
	typeofObject:    "object",
}

// typeOf returns the typeof constant of the result of the typeof operator for v.
func typeOf(v Value) int {
	o := v.o
	if o == nil {
		return typeofUndefined
	}
	if o == numberObject {
		return typeofNumber
	}
	switch o.kind {
	case kindHTMLDDA:
		return typeofUndefined
	case kindFalse, kindTrue:
		return typeofBoolean
	case kindString:
		return typeofString
	case kindSymbol:
		return typeofSymbol
	case kindBigInt:
		return typeofBigInt
	}
	if o.fn != nil {
		return typeofFunction
	}
	return typeofObject
}

// IsStrictlyEqual implements IsStrictlyEqual.
func IsStrictlyEqual(a, b Value) bool {
	if a.o == numberObject || b.o == numberObject {
		return a.o == b.o && a.n == b.n
	}
	if a.isShortString() || b.isShortString() {
		return a.IsString() && b.IsString() && len(a.o.str) == len(b.o.str) && math.Float64bits(a.n) == math.Float64bits(b.n)
	}
	if a.o == b.o {
		return a.o != bigInt64Object || math.Float64bits(a.n) == math.Float64bits(b.n)
	}
	if a.o == nil || b.o == nil || a.o.kind != b.o.kind {
		return false
	}
	switch a.o.kind {
	case kindString:
		return sameStrings(a.o, b.o)
	case kindBigInt:
		// A BigInt that fits in an int64 never equals one that does not.
		return a.o != bigInt64Object && b.o != bigInt64Object && a.o.extState().(*big.Int).Cmp(b.o.extState().(*big.Int)) == 0
	}
	return false
}

// SameValueZero implements SameValueZero.
func SameValueZero(a, b Value) bool {
	if a.o == numberObject && b.o == numberObject {
		return a.n == b.n || (a.n != a.n && b.n != b.n)
	}
	return IsStrictlyEqual(a, b)
}

// SameValue implements SameValue.
func SameValue(a, b Value) bool {
	if a.o == numberObject && b.o == numberObject {
		if a.n != a.n {
			return b.n != b.n
		}
		return a.n == b.n && math.Signbit(a.n) == math.Signbit(b.n)
	}
	return IsStrictlyEqual(a, b)
}

// RelativeIndex resolves an index argument that counts from the end when negative
// against a length, clamping the result to the range from 0 to length.
func RelativeIndex(f float64, length int) int {
	f = numconv.ToIntegerOrInfinity(f)
	if f < 0 {
		f += float64(length)
		if f < 0 {
			return 0
		}
		return int(f)
	}
	if f > float64(length) {
		return length
	}
	return int(f)
}

// MaxArrayIndex is the largest array index, 2**32 - 2.
const MaxArrayIndex = 1<<32 - 2

// numberIndex returns the array index that the number n stands for as a property key.
func numberIndex(n float64) (uint32, bool) {
	i := uint32(n)
	return i, float64(i) == n && i <= MaxArrayIndex
}

// stringIndex returns the array index whose canonical string form is s.
func stringIndex(s wtf8.String) (uint32, bool) {
	if len(s) == 0 || len(s) > 10 || s[0] < '0' || s[0] > '9' || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	var n uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	if n > MaxArrayIndex {
		return 0, false
	}
	return uint32(n), true
}
