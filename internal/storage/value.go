// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package storage holds the values, the objects and the shapes of JavaScript, and the operations on
// them that run no code.
package storage

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type Kind uint8

const (
	KindUndefined Kind = iota
	KindNumber
	KindNull
	KindFalse
	KindTrue
	KindString
	KindSymbol
	KindBigInt

	// KindHole is the content of an array element that does not exist. It never reaches JavaScript.
	KindHole

	// KindAccessor is the content of the slot of an accessor property. It never reaches JavaScript.
	KindAccessor

	// KindHTMLDDA is an object with the [[IsHTMLDDA]] internal slot (B.3.6).
	// It comes before KindObject, so that a check for KindObject or above excludes it.
	KindHTMLDDA
	KindObject
	KindArray
	KindFunction
)

// Every kind from KindFirstObject on is an object in the JavaScript sense.
const KindFirstObject = KindHTMLDDA

// Value is a JavaScript value. The zero value is undefined.
// Values cannot be compared with ==; use [IsStrictlyEqual], [SameValue], or [SameValueZero].
type Value struct {
	// _ makes [Value] uncomparable.
	_ [0]func()

	N float64

	// O is nil for undefined, NumberObject for the number N, BigInt64Object for the BigInt
	// whose two's complement bits N holds, and the object for anything else.
	// Every string of at most shortstrings.MaxBytes bytes holds its bytes in N.
	// Its object carries the byte length and realm, and may also be an interned key.
	O *Object
}

var NumberObject = &Object{Kind: KindNumber, objectTail: &zeroTail}

var NullObject = &Object{Kind: KindNull, objectTail: &zeroTail}

var FalseObject = &Object{Kind: KindFalse, objectTail: &zeroTail}

var TrueObject = &Object{Kind: KindTrue, objectTail: &zeroTail}

var HoleObject = &Object{Kind: KindHole, objectTail: &zeroTail}

// BigInt64Object is the O of every BigInt that fits in an int64. Every other BigInt is an
// object of its own, so each BigInt has exactly one representation.
var BigInt64Object = &Object{Kind: KindBigInt, objectTail: &zeroTail}

// Undefined is the value undefined.
var Undefined Value

// Null is the value null.
var Null = Value{O: NullObject}

// False is the value false.
var False = Value{O: FalseObject}

// True is the value true.
var True = Value{O: TrueObject}

var Hole = Value{O: HoleObject}

// NewMarker returns a new value with the internal state state that never reaches JavaScript.
func NewMarker(state any) *Object {
	return (&PropertyObject{
		Kind: KindHole,
		Ext:  state,
	}).Init()
}

// Number returns the number n as a value.
func Number(n float64) Value {
	return Value{
		N: n,
		O: NumberObject,
	}
}

// BigIntFromInt64 returns the BigInt i.
func BigIntFromInt64(i int64) Value {
	return Value{
		N: math.Float64frombits(uint64(i)),
		O: BigInt64Object,
	}
}

// Bool returns the boolean b as a value.
func Bool(b bool) Value {
	o := FalseObject
	if b {
		o = TrueObject
	}
	return Value{O: o}
}

// ObjectValue returns o as a value.
func ObjectValue(o *Object) Value {
	v := Value{
		O: o,
	}
	if v.IsShortString() {
		v.N = math.Float64frombits(shortstrings.Pack(o.Str()))
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
	return v.StringObject()
}

// Number returns the number v. v must be a number.
func (v Value) Number() float64 {
	return v.N
}

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool {
	return v.O == NumberObject
}

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool {
	return v.O == nil
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.O == NullObject
}

// IsBool reports whether v is a boolean.
func (v Value) IsBool() bool {
	return v.O == TrueObject || v.O == FalseObject
}

// IsString reports whether v is a string.
func (v Value) IsString() bool {
	return v.O != nil && v.O.Kind == KindString
}

// IsSymbol reports whether v is a symbol.
func (v Value) IsSymbol() bool {
	return v.O != nil && v.O.Kind == KindSymbol
}

// IsBigInt reports whether v is a BigInt.
func (v Value) IsBigInt() bool {
	return v.O != nil && v.O.Kind == KindBigInt
}

// BigInt64 returns the BigInt v as an int64, and reports whether v is a BigInt that fits in one.
func (v Value) BigInt64() (int64, bool) {
	if v.O != BigInt64Object {
		return 0, false
	}
	return int64(math.Float64bits(v.N)), true
}

// IsObject reports whether v is an object, which includes functions and arrays.
func (v Value) IsObject() bool {
	return v.O != nil && v.O.Kind >= KindFirstObject
}

// IsNullish reports whether v is undefined or null.
func (v Value) IsNullish() bool {
	return v.O == nil || v.O == NullObject
}

func (v Value) IsHTMLDDA() bool {
	return v.O != nil && v.O.Kind == KindHTMLDDA
}

func (v Value) IsHole() bool {
	return v.O == HoleObject
}

// IsCallable implements IsCallable.
func (v Value) IsCallable() bool {
	return v.O != nil && v.O.Fn != nil
}

// ToBoolean implements ToBoolean.
func ToBoolean(v Value) bool {
	o := v.O
	if o == nil {
		return false
	}
	if o == NumberObject {
		return v.N != 0 && v.N == v.N
	}
	switch o.Kind {
	case KindNull, KindFalse, KindHTMLDDA:
		return false
	case KindString:
		return len(o.Text()) != 0
	case KindBigInt:
		// Zero fits in an int64.
		return o != BigInt64Object || math.Float64bits(v.N) != 0
	}
	return true
}

// The results of the typeof operator.
const (
	TypeofUndefined = iota
	TypeofNumber
	TypeofBoolean
	TypeofString
	TypeofSymbol
	TypeofBigInt
	TypeofFunction
	TypeofObject
	TypeofCount
)

// typeOf returns the typeof constant of the result of the typeof operator for v.
func typeOf(v Value) int {
	o := v.O
	if o == nil {
		return TypeofUndefined
	}
	if o == NumberObject {
		return TypeofNumber
	}
	switch o.Kind {
	case KindHTMLDDA:
		return TypeofUndefined
	case KindFalse, KindTrue:
		return TypeofBoolean
	case KindString:
		return TypeofString
	case KindSymbol:
		return TypeofSymbol
	case KindBigInt:
		return TypeofBigInt
	}
	if o.Fn != nil {
		return TypeofFunction
	}
	return TypeofObject
}

func (sp *Space) TypeofValue(v Value) Value {
	return sp.Typeofs[typeOf(v)]
}

// MaxArrayIndex is the largest array index, 2**32 - 2.
const MaxArrayIndex = 1<<32 - 2

// NumberIndex returns the array index that the number n stands for as a property key.
func NumberIndex(n float64) (uint32, bool) {
	i := uint32(n)
	return i, float64(i) == n && i <= MaxArrayIndex
}

// StringIndex returns the array index whose canonical string form is s.
func StringIndex(s wtf8.String) (uint32, bool) {
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

// SetBigInt64 stores the BigInt i in the register r.
func SetBigInt64(r *Value, i int64) {
	if r.O != BigInt64Object {
		r.O = BigInt64Object
	}
	r.N = math.Float64frombits(uint64(i))
}

// SetNumber stores the number n in the register r.
func SetNumber(r *Value, n float64) {
	// Over a number only N changes. Leaving the object pointer as it is needs no write barrier
	// while the garbage collector marks.
	if r.O != NumberObject {
		r.O = NumberObject
	}
	r.N = n
}
