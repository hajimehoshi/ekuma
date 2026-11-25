// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"math"
	"math/big"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// Type is the type of a JavaScript value. Null has type [TypeNull], and
// callable objects have type [TypeFunction], including those with [[IsHTMLDDA]].
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
// Values cannot be compared with ==. Use [Value.Equal] to compare them with JavaScript
// === semantics.
//
// Strings, symbols, objects and functions belong to their creating VM and may pass between
// its realms. Passing them into another VM panics, including through
// host callback results and exceptions. Other primitives may pass between VMs.
// To share SharedArrayBuffer memory, pass its [SharedDataBlock] to [VM.ValueOf].
type Value struct {
	value engine.Value
}

// canonicalNaN is the NaN that [Value.Float] returns.
var canonicalNaN = math.Float64frombits(0x7ff8000000000000)

// fromEngine converts a value of the engine to a [Value].
func fromEngine(v engine.Value) Value {
	return Value{
		value: v,
	}
}

var (
	// Undefined is the value undefined.
	Undefined Value

	// Null is the value null.
	Null = fromEngine(storage.Null)
)

// Type returns the type of v.
func (v Value) Type() Type {
	e := v.value
	switch {
	case e.IsUndefined():
		return TypeUndefined
	case e.IsNumber():
		return TypeNumber
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
	return v.value.IsUndefined()
}

// IsNull reports whether v is null.
func (v Value) IsNull() bool {
	return v.value.IsNull()
}

// IsNaN reports whether v is the number NaN.
func (v Value) IsNaN() bool {
	return v.value.IsNumber() && math.IsNaN(v.value.Number())
}

// Truthy returns the boolean that v converts to.
func (v Value) Truthy() bool {
	return storage.ToBoolean(v.value)
}

// Bool returns the boolean v. It panics with a [*ValueError] when v is not a boolean.
func (v Value) Bool() bool {
	e := v.value
	if !e.IsBool() {
		panic(&ValueError{
			Method: "Value.Bool",
			Type:   v.Type(),
		})
	}
	return storage.ToBoolean(e)
}

// Float returns the number v. A NaN always has the bits 0x7ff8000000000000. Float panics with
// a [*ValueError] when v is not a number.
func (v Value) Float() float64 {
	if !v.value.IsNumber() {
		panic(&ValueError{
			Method: "Value.Float",
			Type:   v.Type(),
		})
	}
	n := v.value.Number()
	if math.IsNaN(n) {
		return canonicalNaN
	}
	return n
}

// Int returns the number v truncated toward zero to an int. NaN gives 0, and a number below or
// above the range of an int gives [math.MinInt] or [math.MaxInt]. It panics with a
// [*ValueError]
// when v is not a number.
func (v Value) Int() int {
	if !v.value.IsNumber() {
		panic(&ValueError{
			Method: "Value.Int",
			Type:   v.Type(),
		})
	}
	n := v.value.Number()
	switch {
	case math.IsNaN(n):
		return 0
	case n >= math.MaxInt:
		return math.MaxInt
	case n <= math.MinInt:
		return math.MinInt
	}
	return int(n)
}

// BigInt returns the BigInt v as a new [*big.Int]. It panics with a [*ValueError] when v is not
// a BigInt.
func (v Value) BigInt() *big.Int {
	e := v.value
	if !e.IsBigInt() {
		panic(&ValueError{
			Method: "Value.BigInt",
			Type:   v.Type(),
		})
	}
	return new(big.Int).Set(e.BigInt())
}

// String returns the string v as UTF-8, with lone surrogates replaced by U+FFFD.
// For a value of another type, it returns a description such as "<object>" or "<number: 42>"
// and runs no JavaScript.
func (v Value) String() string {
	switch t := v.Type(); t {
	case TypeString:
		return engine.GoString(v.value)
	case TypeBoolean:
		return "<boolean: " + strconv.FormatBool(v.Bool()) + ">"
	case TypeNumber:
		return "<number: " + strconv.FormatFloat(v.value.Number(), 'g', -1, 64) + ">"
	default:
		return "<" + t.String() + ">"
	}
}

// Equal reports whether v and w are equal according to the JavaScript === operator.
func (v Value) Equal(w Value) bool {
	return storage.IsStrictlyEqual(v.value, w.value)
}

// vm returns the [VM] that the object, function or string v belongs to.
func (v Value) vm(method string) (*VM, error) {
	if o := v.value.Object(); o != nil && !v.value.IsSymbol() {
		if host, ok := o.HostDefined().(*VM); ok {
			return host, nil
		}
	}
	return nil, &ValueError{
		Method: method,
		Type:   v.Type(),
	}
}

// Get returns the value of the property named name on v.
//
// The error is a [*ValueError] when v is not an object, a function or a string,
// and a [*ScriptError] when the operation throws a JavaScript exception.
func (v Value) Get(name string) (Value, error) {
	vm, err := v.vm("Value.Get")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.hostContext().HostGet(v.value, vm.hostContext().StringFromGo(name))
	return fromEngine(r), publicError(err)
}

// GetWithValueKey is like [Value.Get], but key is converted to a property key the way the
// expression v[key] converts it, and a conversion that throws is a [*ScriptError]. It panics if key
// belongs to a different VM from v.
func (v Value) GetWithValueKey(key Value) (Value, error) {
	vm, err := v.vm("Value.GetWithValueKey")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.hostContext().HostGet(v.value, vm.acceptValue(key))
	return fromEngine(r), publicError(err)
}

// Set sets the property named name on v to [VM.ValueOf](x), as an assignment in strict
// mode does. The errors are those of [Value.Get].
func (v Value) Set(name string, x any) error {
	vm, err := v.vm("Value.Set")
	if err != nil {
		return err
	}
	return publicError(vm.hostContext().HostSet(v.value, vm.hostContext().StringFromGo(name), vm.valueOf(vm.initialRealm.cx, x)))
}

// SetWithValueKey is like [Value.Set], but takes key as [Value.GetWithValueKey] does.
func (v Value) SetWithValueKey(key Value, x any) error {
	vm, err := v.vm("Value.SetWithValueKey")
	if err != nil {
		return err
	}
	return publicError(vm.hostContext().HostSet(v.value, vm.acceptValue(key), vm.valueOf(vm.initialRealm.cx, x)))
}

// Delete deletes the property named name from v, as the delete operator in strict mode does.
// The errors are those of [Value.Get].
func (v Value) Delete(name string) error {
	vm, err := v.vm("Value.Delete")
	if err != nil {
		return err
	}
	return publicError(vm.hostContext().HostDelete(v.value, vm.hostContext().StringFromGo(name)))
}

// DeleteWithValueKey is like [Value.Delete], but takes key as [Value.GetWithValueKey] does.
func (v Value) DeleteWithValueKey(key Value) error {
	vm, err := v.vm("Value.DeleteWithValueKey")
	if err != nil {
		return err
	}
	return publicError(vm.hostContext().HostDelete(v.value, vm.acceptValue(key)))
}

func (vm *VM) indexKey(i int) engine.Value {
	if uint64(i) <= engine.MaxArrayIndex {
		return storage.Number(float64(i))
	}
	return vm.hostContext().StringFromGo(strconv.Itoa(i))
}

// Index returns the property of v named by the decimal representation of i.
// The errors are those of [Value.Get].
func (v Value) Index(i int) (Value, error) {
	vm, err := v.vm("Value.Index")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.hostContext().HostGet(v.value, vm.indexKey(i))
	return fromEngine(r), publicError(err)
}

// SetIndex sets the property of v named by the decimal representation of i to [VM.ValueOf](x).
// The errors are those of [Value.Get].
func (v Value) SetIndex(i int, x any) error {
	vm, err := v.vm("Value.SetIndex")
	if err != nil {
		return err
	}
	return publicError(vm.hostContext().HostSet(v.value, vm.indexKey(i), vm.valueOf(vm.initialRealm.cx, x)))
}

// Call calls the method named name on v with v as this. The arguments are converted with
// [VM.ValueOf].
// The errors are those of [Value.Get], and a method that does not exist is a [*ScriptError] holding
// a TypeError. When ctx is done, the call stops at its next loop iteration or call,
// and the error is the cause of ctx.
func (v Value) Call(ctx context.Context, name string, args ...any) (Value, error) {
	vm, err := v.vm("Value.Call")
	if err != nil {
		return Undefined, err
	}
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.withArgs(args, func(a []engine.Value) (engine.Value, error) {
			return vm.hostContext().HostCallMethod(v.value, vm.hostContext().StringFromGo(name), a)
		})
	})
}

// CallWithValueKey is like [Value.Call], but takes key as [Value.GetWithValueKey] does.
func (v Value) CallWithValueKey(ctx context.Context, key Value, args ...any) (Value, error) {
	vm, err := v.vm("Value.CallWithValueKey")
	if err != nil {
		return Undefined, err
	}
	k := vm.acceptValue(key)
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.withArgs(args, func(a []engine.Value) (engine.Value, error) {
			return vm.hostContext().HostCallMethod(v.value, k, a)
		})
	})
}

// Invoke calls the function v with undefined as this. The arguments are converted with
// [VM.ValueOf]. The error is a [*ValueError] when v is not a function, and a [*ScriptError] when it
// throws. When ctx is done, the call stops at its next loop iteration or call,
// and the error is the cause of ctx.
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
				return vm.hostContext().HostConstruct(v.value, a)
			}
			return vm.hostContext().HostCall(v.value, storage.Undefined, a)
		})
	})
}

// InstanceOf reports whether v is an instance of the constructor t, as the instanceof
// operator does.
// The errors are those of [Value.Get], for t. It panics if v
// belongs to a different VM from t.
func (v Value) InstanceOf(t Value) (bool, error) {
	vm, err := t.vm("Value.InstanceOf")
	if err != nil {
		return false, err
	}
	r, err := vm.hostContext().HostInstanceOf(vm.acceptValue(v), t.value)
	return r, publicError(err)
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

// IsPromise reports whether v is a Promise object. A mere thenable is not a Promise object.
func (v Value) IsPromise() bool {
	return engine.IsPromise(v.value)
}

// PromiseState returns the state of the promise v and, unless it is pending, its result: the
// value it was fulfilled with or the reason it was rejected with. It runs no JavaScript
// and does
// not mark a rejected promise as handled. It panics with a [*ValueError] when v is not
// a promise.
func (v Value) PromiseState() (PromiseState, Value) {
	e := v.value
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

// SharedDataBlock is the memory of a SharedArrayBuffer, usable by any VM on any goroutine.
type SharedDataBlock struct {
	b *engine.SharedDataBlock
}

// SharedDataBlock returns the data block of the SharedArrayBuffer v. It panics with
// a [*ValueError] when v is not a SharedArrayBuffer.
func (v Value) SharedDataBlock() *SharedDataBlock {
	if b := engine.ArrayBufferData(v.value); b != nil && b.IsShared() {
		return &SharedDataBlock{b: b.SharedDataBlock()}
	}
	panic(&ValueError{
		Method: "Value.SharedDataBlock",
		Type:   v.Type(),
	})
}

// uint8ArrayData returns the state of v when v is a Uint8Array or a Uint8ClampedArray,
// and panics with a [*ValueError] otherwise.
func uint8ArrayData(v Value, method string) *engine.TypedArray {
	if t := engine.TypedArrayData(v.value); t != nil {
		if e := t.ElementType(); e == element.Uint8 || e == element.Uint8Clamped {
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

// NewArrayBuffer returns an ArrayBuffer of the initial realm whose bytes are b, without copying.
// Its length is len(b) and does not change.
//
// The VM reads and writes b only during calls into this package, and never after the ArrayBuffer
// is detached with [VM.DetachArrayBuffer]. Go may read and write b whenever it may call the VM,
// including in a [Func], and either side sees the writes of the other at once.
//
// JavaScript cannot detach the ArrayBuffer: its transfer, transferToFixedLength and
// transferToImmutable methods throw a TypeError. NewArrayBuffer panics when b is longer than an
// ArrayBuffer may be.
func (vm *VM) NewArrayBuffer(b []byte) Value {
	vm.initIfNeeded()
	return vm.initialRealm.NewArrayBuffer(b)
}

// DetachArrayBuffer detaches the ArrayBuffer v, so that v and its views have no bytes.
// Detaching a detached ArrayBuffer does nothing. DetachArrayBuffer panics with a [*ValueError]
// when v is not a mutable ArrayBuffer.
func (vm *VM) DetachArrayBuffer(v Value) {
	vm.initIfNeeded()
	vm.acceptValue(v)
	if b := engine.ArrayBufferData(v.value); b != nil && !b.IsShared() && !b.IsImmutable() {
		b.Detach()
		return
	}
	panic(&ValueError{
		Method: "VM.DetachArrayBuffer",
		Type:   v.Type(),
	})
}
