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
	kindString
	kindSymbol
	kindBigInt

	// kindHole is the content of an array element that does not exist. It never reaches JavaScript.
	kindHole

	// kindAccessor is the content of the slot of an accessor property. It never reaches JavaScript.
	kindAccessor

	// Every kind from kindObject on is an object in the JavaScript sense.
	kindObject
	kindArray
	kindFunction
)

// Value is a JavaScript value.
//
// A nil o means the number n. Every other value is identified by o,
// which makes a Value two words and keeps numbers off the heap.
type Value struct {
	n float64
	o *Object
}

var (
	undefinedObject = &Object{kind: kindUndefined}
	nullObject      = &Object{kind: kindNull}
	falseObject     = &Object{kind: kindFalse}
	trueObject      = &Object{kind: kindTrue}
	holeObject      = &Object{kind: kindHole}

	// Undefined is the value undefined.
	Undefined = Value{o: undefinedObject}

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
	return Value{n: n}
}

// Bool returns the boolean b as a value.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// ObjectValue returns o as a value.
func ObjectValue(o *Object) Value {
	return Value{o: o}
}

// Object returns the object, string or symbol that v is. It returns nil for a number.
func (v Value) Object() *Object {
	return v.o
}

// Number returns the number v. v must be a number.
func (v Value) Number() float64 {
	return v.n
}

// IsNumber reports whether v is a number.
func (v Value) IsNumber() bool {
	return v.o == nil
}

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool {
	return v.o == undefinedObject
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

// IsObject reports whether v is an object, which includes functions and arrays.
func (v Value) IsObject() bool {
	return v.o != nil && v.o.kind >= kindObject
}

// IsNullish reports whether v is undefined or null.
func (v Value) IsNullish() bool {
	return v.o == undefinedObject || v.o == nullObject
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
		return v.n != 0 && v.n == v.n
	}
	switch o.kind {
	case kindUndefined, kindNull, kindFalse:
		return false
	case kindString:
		return o.str != ""
	case kindBigInt:
		return o.BigInt().Sign() != 0
	}
	return true
}

// typeOf implements the typeof operator.
func typeOf(v Value) string {
	o := v.o
	if o == nil {
		return "number"
	}
	switch o.kind {
	case kindUndefined:
		return "undefined"
	case kindFalse, kindTrue:
		return "boolean"
	case kindString:
		return "string"
	case kindSymbol:
		return "symbol"
	case kindBigInt:
		return "bigint"
	}
	if o.fn != nil {
		return "function"
	}
	return "object"
}

// IsStrictlyEqual implements IsStrictlyEqual.
func IsStrictlyEqual(a, b Value) bool {
	if a.o == nil || b.o == nil {
		return a.o == b.o && a.n == b.n
	}
	if a.o == b.o {
		return true
	}
	if a.o.kind != b.o.kind {
		return false
	}
	switch a.o.kind {
	case kindString:
		return a.o.str == b.o.str
	case kindBigInt:
		return a.o.BigInt().Cmp(b.o.BigInt()) == 0
	}
	return false
}

// SameValueZero implements SameValueZero.
func SameValueZero(a, b Value) bool {
	if a.o == nil && b.o == nil {
		return a.n == b.n || (a.n != a.n && b.n != b.n)
	}
	return IsStrictlyEqual(a, b)
}

// SameValue implements SameValue.
func SameValue(a, b Value) bool {
	if a.o == nil && b.o == nil {
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
