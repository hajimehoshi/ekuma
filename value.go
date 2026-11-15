// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"math"
	"math/big"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// Type is the type of a JavaScript value, as the typeof operator sees it,
// except that null has a type of its own.
type Type int

const (
	TypeUndefined Type = iota
	TypeNull
	TypeBoolean
	TypeNumber
	TypeString
	TypeSymbol
	TypeBigInt
	TypeObject
	TypeFunction
)

func (t Type) String() string {
	switch t {
	case TypeUndefined:
		return "undefined"
	case TypeNull:
		return "null"
	case TypeBoolean:
		return "boolean"
	case TypeNumber:
		return "number"
	case TypeString:
		return "string"
	case TypeSymbol:
		return "symbol"
	case TypeBigInt:
		return "bigint"
	case TypeObject:
		return "object"
	case TypeFunction:
		return "function"
	}
	return "Type(" + strconv.Itoa(int(t)) + ")"
}

// Value is a JavaScript value. The zero value is undefined.
//
// Values cannot be compared with ==. [Value.Equal] compares the way the JavaScript operator ===
// does.
type Value struct {
	// _ makes [Value] uncomparable.
	_ [0]func()

	n float64

	// o is nil for undefined, numberTag for the number n, and the engine's object for anything else.
	o *engine.Object
}

// numberTag marks a [Value] that is a number. It is never passed to the engine.
var numberTag = &engine.Object{}

// canonicalNaN is the NaN that [Value.Float] returns.
var canonicalNaN = math.Float64frombits(0x7ff8000000000000)

// fromEngine converts a value of the engine to a [Value].
func fromEngine(v engine.Value) Value {
	switch {
	case v.IsNumber():
		return Value{
			n: v.Number(),
			o: numberTag,
		}
	case v.IsUndefined():
		return Value{}
	}
	return Value{o: v.Object()}
}

// engine converts v to a value of the engine.
func (v Value) engine() engine.Value {
	switch v.o {
	case nil:
		return engine.Undefined
	case numberTag:
		return engine.Number(v.n)
	}
	return engine.ObjectValue(v.o)
}

var (
	// Undefined is the value undefined.
	Undefined Value

	// Null is the value null.
	Null = fromEngine(engine.Null)
)

// Type returns the type of v.
func (v Value) Type() Type {
	switch v.o {
	case nil:
		return TypeUndefined
	case numberTag:
		return TypeNumber
	}
	e := v.engine()
	switch {
	case e.IsNull():
		return TypeNull
	case e.IsBool():
		return TypeBoolean
	case e.IsString():
		return TypeString
	case e.IsSymbol():
		return TypeSymbol
	case e.IsCallable():
		return TypeFunction
	case e.IsObject():
		return TypeObject
	}
	return TypeBigInt
}

// IsUndefined reports whether v is undefined.
func (v Value) IsUndefined() bool {
	return v.o == nil
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.o == Null.o
}

// IsNaN reports whether v is the number NaN.
func (v Value) IsNaN() bool {
	return v.o == numberTag && v.n != v.n
}

// Truthy returns the boolean that v converts to.
func (v Value) Truthy() bool {
	return engine.ToBoolean(v.engine())
}

// Bool returns the boolean v. It panics with a [*ValueError] when v is not a boolean.
func (v Value) Bool() bool {
	e := v.engine()
	if !e.IsBool() {
		panic(&ValueError{
			Method: "Value.Bool",
			Type:   v.Type(),
		})
	}
	return e == engine.True
}

// Float returns the number v. A NaN always has the bits 0x7ff8000000000000. Float panics with
// a [*ValueError] when v is not a number.
func (v Value) Float() float64 {
	if v.o != numberTag {
		panic(&ValueError{
			Method: "Value.Float",
			Type:   v.Type(),
		})
	}
	if v.n != v.n {
		return canonicalNaN
	}
	return v.n
}

// Int returns the number v truncated to an int. It panics with a [*ValueError] when v is not
// a number.
func (v Value) Int() int {
	if v.o != numberTag {
		panic(&ValueError{
			Method: "Value.Int",
			Type:   v.Type(),
		})
	}
	return int(v.n)
}

// BigInt returns the BigInt v as a new [*big.Int]. It panics with a [*ValueError] when v is not
// a BigInt.
func (v Value) BigInt() *big.Int {
	e := v.engine()
	if !e.IsBigInt() {
		panic(&ValueError{
			Method: "Value.BigInt",
			Type:   v.Type(),
		})
	}
	return new(big.Int).Set(e.Object().BigInt())
}

// String returns the string v as UTF-8, with lone surrogates replaced by U+FFFD.
// For a value of another type, it returns a description such as "<object>" or "<number: 42>"
// and runs no JavaScript.
func (v Value) String() string {
	switch t := v.Type(); t {
	case TypeString:
		return engine.GoString(v.engine())
	case TypeBoolean:
		return "<boolean: " + strconv.FormatBool(v.Bool()) + ">"
	case TypeNumber:
		return "<number: " + strconv.FormatFloat(v.n, 'g', -1, 64) + ">"
	default:
		return "<" + t.String() + ">"
	}
}

// Equal reports whether v and w are equal the way the JavaScript operator === compares.
func (v Value) Equal(w Value) bool {
	return engine.IsStrictlyEqual(v.engine(), w.engine())
}

// vm returns the [VM] that the value belongs to. Only objects, functions and strings have one.
func (v Value) vm(method string) (*VM, error) {
	if v.o != nil && v.o != numberTag {
		if e := v.o.VM(); e != nil {
			return e.HostDefined().(*VM), nil
		}
	}
	return nil, &ValueError{
		Method: method,
		Type:   v.Type(),
	}
}

// Get returns the property name of v.
//
// The error is a [*ValueError] when v is not an object, a function or a string,
// and an [*Error] when a getter throws.
func (v Value) Get(name string) (Value, error) {
	vm, err := v.vm("Value.Get")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.e.HostGet(v.engine(), vm.e.StringFromGo(name))
	return fromEngine(r), vm.publicError(err)
}

// Set stores [VM.ValueOf](x) as the property name of v, the way an assignment in strict mode code
// does. The errors are those of [Value.Get].
func (v Value) Set(name string, x any) error {
	vm, err := v.vm("Value.Set")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostSet(v.engine(), vm.e.StringFromGo(name), vm.valueOf(x)))
}

// Delete deletes the property name of v, the way the delete operator in strict mode code does.
// The errors are those of [Value.Get].
func (v Value) Delete(name string) error {
	vm, err := v.vm("Value.Delete")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostDelete(v.engine(), vm.e.StringFromGo(name)))
}

// Index returns the element i of v. The errors are those of [Value.Get].
func (v Value) Index(i int) (Value, error) {
	vm, err := v.vm("Value.Index")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.e.HostGet(v.engine(), engine.Number(float64(i)))
	return fromEngine(r), vm.publicError(err)
}

// SetIndex stores [VM.ValueOf](x) as the element i of v. The errors are those of [Value.Get].
func (v Value) SetIndex(i int, x any) error {
	vm, err := v.vm("Value.SetIndex")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostSet(v.engine(), engine.Number(float64(i)), vm.valueOf(x)))
}

// Call calls the method name of v with v as this. The arguments are converted with [VM.ValueOf].
// The errors are those of [Value.Get], and a method that does not exist is an [*Error] holding
// a TypeError. When ctx is done, the call stops at its next loop iteration or call, and the error
// is the cause of ctx.
func (v Value) Call(ctx context.Context, name string, args ...any) (Value, error) {
	vm, err := v.vm("Value.Call")
	if err != nil {
		return Undefined, err
	}
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.withArgs(args, func(a []engine.Value) (engine.Value, error) {
			return vm.e.HostCallMethod(v.engine(), vm.e.StringFromGo(name), a)
		})
	})
}

// Invoke calls the function v with undefined as this. The arguments are converted with
// [VM.ValueOf]. The error is a [*ValueError] when v is not a function, and an [*Error] when it
// throws.
func (v Value) Invoke(ctx context.Context, args ...any) (Value, error) {
	return v.invoke(ctx, "Value.Invoke", false, args)
}

// New calls the function v as a constructor, as the new operator does. The arguments are
// converted with [VM.ValueOf]. The errors are those of [Value.Invoke].
func (v Value) New(ctx context.Context, args ...any) (Value, error) {
	return v.invoke(ctx, "Value.New", true, args)
}

func (v Value) invoke(ctx context.Context, method string, construct bool, args []any) (Value, error) {
	if v.Type() != TypeFunction {
		return Undefined, &ValueError{
			Method: method,
			Type:   v.Type(),
		}
	}
	vm, err := v.vm(method)
	if err != nil {
		return Undefined, err
	}
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.withArgs(args, func(a []engine.Value) (engine.Value, error) {
			if construct {
				return vm.e.HostConstruct(v.engine(), a)
			}
			return vm.e.HostCall(v.engine(), engine.Undefined, a)
		})
	})
}

// InstanceOf reports whether v is an instance of the constructor t, as the instanceof operator does.
// The errors are those of [Value.Get], for t.
func (v Value) InstanceOf(t Value) (bool, error) {
	vm, err := t.vm("Value.InstanceOf")
	if err != nil {
		return false, err
	}
	r, err := vm.e.HostInstanceOf(v.engine(), t.engine())
	return r, vm.publicError(err)
}

// PromiseState is the state of a promise.
type PromiseState int

const (
	PromisePending PromiseState = iota
	PromiseFulfilled
	PromiseRejected
)

func (s PromiseState) String() string {
	switch s {
	case PromisePending:
		return "pending"
	case PromiseFulfilled:
		return "fulfilled"
	case PromiseRejected:
		return "rejected"
	}
	return "PromiseState(" + strconv.Itoa(int(s)) + ")"
}

// IsPromise reports whether v is a promise: a Promise object, which a mere thenable is not.
func (v Value) IsPromise() bool {
	return engine.IsPromise(v.engine())
}

// PromiseState returns the state of the promise v and, unless it is pending, its result: the
// value it was fulfilled with or the reason it was rejected with. It runs no JavaScript and does
// not mark a rejected promise as handled. It panics with a [*ValueError] when v is not
// a promise.
func (v Value) PromiseState() (PromiseState, Value) {
	e := v.engine()
	if !engine.IsPromise(e) {
		panic(&ValueError{
			Method: "Value.PromiseState",
			Type:   v.Type(),
		})
	}
	s, r := engine.PromiseStateOf(e)
	switch s {
	case engine.PromiseFulfilled:
		return PromiseFulfilled, fromEngine(r)
	case engine.PromiseRejected:
		return PromiseRejected, fromEngine(r)
	}
	return PromisePending, Undefined
}

// uint8ArrayData returns the state of v when v is a Uint8Array or a Uint8ClampedArray,
// and panics with a [*ValueError] otherwise.
func uint8ArrayData(v Value, method string) *engine.TypedArray {
	if t := engine.TypedArrayData(v.engine()); t != nil {
		if e := t.ElementType(); e == engine.ElementUint8 || e == engine.ElementUint8C {
			return t
		}
	}
	panic(&ValueError{
		Method: method,
		Type:   v.Type(),
	})
}

// CopyBytesToGo copies bytes from the Uint8Array or Uint8ClampedArray src to dst
// and returns the number of bytes copied, which is the minimum of their lengths.
// A detached or out of bounds src has no bytes. It panics with a [*ValueError] when src
// is neither a Uint8Array nor a Uint8ClampedArray.
func CopyBytesToGo(dst []byte, src Value) int {
	t := uint8ArrayData(src, "CopyBytesToGo")
	b := t.Lock()
	defer t.Unlock()
	return copy(dst, b)
}

// CopyBytesToJS copies bytes from src to the Uint8Array or Uint8ClampedArray dst
// and returns the number of bytes copied, which is the minimum of their lengths.
// A detached or out of bounds dst has no bytes. It panics with a [*ValueError] when dst
// is neither a Uint8Array nor a Uint8ClampedArray, or is backed by an immutable ArrayBuffer.
func CopyBytesToJS(dst Value, src []byte) int {
	t := uint8ArrayData(dst, "CopyBytesToJS")
	if t.ArrayBuffer().IsImmutable() {
		panic(&ValueError{
			Method: "CopyBytesToJS",
			Type:   dst.Type(),
		})
	}
	b := t.Lock()
	defer t.Unlock()
	return copy(b, src)
}
