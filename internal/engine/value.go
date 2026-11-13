// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package engine implements the JavaScript virtual machine: values, objects, the interpreter
// and the abstract operations of the language. The built-in objects are in the package builtins.
package engine

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

type kind uint8

const (
	kindUndefined kind = iota
	kindNull
	kindFalse
	kindTrue
	KindString
	kindSymbol
	kindBigInt

	// kindHole is the content of an array element that does not exist. It never reaches JavaScript.
	kindHole

	// kindAccessor is the content of the slot of an accessor property. It never reaches JavaScript.
	kindAccessor

	// Every kind from kindObject on is an object in the JavaScript sense.
	kindObject
	KindArray
	KindFunction
)

// Value is a JavaScript value.
//
// A nil o means the number n. Every other value is identified by o,
// which makes a Value two words and keeps numbers off the heap.
type Value struct {
	N float64
	O *Object
}

var (
	undefinedObject = &Object{Kind: kindUndefined}
	nullObject      = &Object{Kind: kindNull}
	falseObject     = &Object{Kind: kindFalse}
	TrueObject      = &Object{Kind: kindTrue}
	holeObject      = &Object{Kind: kindHole}

	// Undefined is the value undefined.
	Undefined = Value{O: undefinedObject}

	// Null is the value null.
	Null = Value{O: nullObject}

	// False is the value false.
	False = Value{O: falseObject}

	// True is the value true.
	True = Value{O: TrueObject}

	hole = Value{O: holeObject}
)

// Number returns the number n as a value.
func Number(n float64) Value {
	return Value{N: n}
}

// Bool returns the boolean b as a value.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool {
	return v.O == nil
}

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool {
	return v.O == undefinedObject
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.O == nullObject
}

// IsBool reports whether v is a boolean.
func (v Value) IsBool() bool {
	return v.O == TrueObject || v.O == falseObject
}

// IsString reports whether v is a string.
func (v Value) IsString() bool {
	return v.O != nil && v.O.Kind == KindString
}

// IsSymbol reports whether v is a symbol.
func (v Value) IsSymbol() bool {
	return v.O != nil && v.O.Kind == kindSymbol
}

// IsObject reports whether v is an object, which includes functions and arrays.
func (v Value) IsObject() bool {
	return v.O != nil && v.O.Kind >= kindObject
}

func (v Value) IsNullish() bool {
	return v.O == undefinedObject || v.O == nullObject
}

func (v Value) IsHole() bool {
	return v.O == holeObject
}

func (v Value) IsCallable() bool {
	return v.O != nil && v.O.Fn != nil
}

// truthy implements ToBoolean.
func Truthy(v Value) bool {
	o := v.O
	if o == nil {
		return v.N != 0 && v.N == v.N
	}
	switch o.Kind {
	case kindUndefined, kindNull, kindFalse:
		return false
	case KindString:
		return o.Str != ""
	case kindBigInt:
		return o.BigInt().Sign() != 0
	}
	return true
}

// typeOf implements the typeof operator.
func typeOf(v Value) string {
	o := v.O
	if o == nil {
		return "number"
	}
	switch o.Kind {
	case kindUndefined:
		return "undefined"
	case kindFalse, kindTrue:
		return "boolean"
	case KindString:
		return "string"
	case kindSymbol:
		return "symbol"
	case kindBigInt:
		return "bigint"
	}
	if o.Fn != nil {
		return "function"
	}
	return "object"
}

// strictEquals implements IsStrictlyEqual.
func StrictEquals(a, b Value) bool {
	if a.O == nil || b.O == nil {
		return a.O == b.O && a.N == b.N
	}
	if a.O == b.O {
		return true
	}
	if a.O.Kind != b.O.Kind {
		return false
	}
	switch a.O.Kind {
	case KindString:
		return a.O.Str == b.O.Str
	case kindBigInt:
		return a.O.BigInt().Cmp(b.O.BigInt()) == 0
	}
	return false
}

// sameValueZero implements SameValueZero.
func SameValueZero(a, b Value) bool {
	if a.O == nil && b.O == nil {
		return a.N == b.N || (a.N != a.N && b.N != b.N)
	}
	return StrictEquals(a, b)
}

// sameValue implements SameValue.
func SameValue(a, b Value) bool {
	if a.O == nil && b.O == nil {
		if a.N != a.N {
			return b.N != b.N
		}
		return a.N == b.N && math.Signbit(a.N) == math.Signbit(b.N)
	}
	return StrictEquals(a, b)
}

// relativeIndex resolves an index argument that counts from the end when negative
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

// maxArrayIndex is the largest array index, 2**32 - 2.
const MaxArrayIndex = 1<<32 - 2

// numberIndex returns the array index that the number n stands for as a property key.
func numberIndex(n float64) (uint32, bool) {
	i := uint32(n)
	return i, float64(i) == n && i <= MaxArrayIndex
}

// stringIndex returns the array index whose canonical string form is s.
func stringIndex(s string) (uint32, bool) {
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
