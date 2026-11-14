// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package ekuma is a JavaScript engine written in pure Go.
//
// The API follows the one of syscall/js where it can. The differences come from JavaScript
// running inside the process: there can be several VMs, an exception is returned as an error
// and not thrown as a panic, and the methods that run a script or call a function take
// a context.Context that stops them.
//
// A context stops everything that runs under it: when a Func reads a property or calls
// a function, that is stopped by the context of the call that led to the Func.
package ekuma

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/builtins"
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

// ValueError is the error of a method of Value that does not support the type of the value.
// The methods that return no error panic with it.
type ValueError struct {
	Method string
	Type   Type
}

func (e *ValueError) Error() string {
	return "ekuma: call of " + e.Method + " on " + e.Type.String()
}

// Value is a JavaScript value. The zero value is undefined.
//
// Values cannot be compared with ==. Equal compares the way the JavaScript operator === does.
type Value struct {
	// _ makes Value uncomparable.
	_ [0]func()

	n float64

	// o is nil for undefined, numberTag for the number n, and the engine's object for anything else.
	o *engine.Object
}

// numberTag marks a Value that is a number. It is never passed to the engine.
var numberTag = &engine.Object{}

// fromEngine converts a value of the engine to a Value.
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

// Bool returns the boolean v. It panics with a *ValueError when v is not a boolean.
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

// Float returns the number v. It panics with a *ValueError when v is not a number.
func (v Value) Float() float64 {
	if v.o != numberTag {
		panic(&ValueError{
			Method: "Value.Float",
			Type:   v.Type(),
		})
	}
	return v.n
}

// Int returns the number v truncated to an int. It panics with a *ValueError when v is not a number.
func (v Value) Int() int {
	if v.o != numberTag {
		panic(&ValueError{
			Method: "Value.Int",
			Type:   v.Type(),
		})
	}
	return int(v.n)
}

// BigInt returns the BigInt v as a new *big.Int. It panics with a *ValueError when v is not a BigInt.
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

// vm returns the VM that the value belongs to. Only objects, functions and strings have one.
func (v Value) vm(method string) (*VM, error) {
	if v.o != nil && v.o != numberTag {
		if e := v.o.VM(); e != nil {
			return e.Host.(*VM), nil
		}
	}
	return nil, &ValueError{
		Method: method,
		Type:   v.Type(),
	}
}

// Get returns the property name of v.
//
// The error is a *ValueError when v is not an object, a function or a string,
// and an *Error when a getter throws.
func (v Value) Get(name string) (Value, error) {
	vm, err := v.vm("Value.Get")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.e.HostGet(v.engine(), vm.e.StringFromGo(name))
	return fromEngine(r), vm.publicError(err)
}

// Set stores ValueOf(x) as the property name of v, the way an assignment in strict mode code does.
// The errors are those of Get.
func (v Value) Set(name string, x any) error {
	vm, err := v.vm("Value.Set")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostSet(v.engine(), vm.e.StringFromGo(name), vm.valueOf(x)))
}

// Delete deletes the property name of v, the way the delete operator in strict mode code does.
// The errors are those of Get.
func (v Value) Delete(name string) error {
	vm, err := v.vm("Value.Delete")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostDelete(v.engine(), vm.e.StringFromGo(name)))
}

// Index returns the element i of v. The errors are those of Get.
func (v Value) Index(i int) (Value, error) {
	vm, err := v.vm("Value.Index")
	if err != nil {
		return Undefined, err
	}
	r, err := vm.e.HostGet(v.engine(), engine.Number(float64(i)))
	return fromEngine(r), vm.publicError(err)
}

// SetIndex stores ValueOf(x) as the element i of v. The errors are those of Get.
func (v Value) SetIndex(i int, x any) error {
	vm, err := v.vm("Value.SetIndex")
	if err != nil {
		return err
	}
	return vm.publicError(vm.e.HostSet(v.engine(), engine.Number(float64(i)), vm.valueOf(x)))
}

// withArgs converts the arguments of a call with ValueOf and passes them to f.
func (vm *VM) withArgs(args []any, f func(args []engine.Value) (engine.Value, error)) (engine.Value, error) {
	n := len(vm.callArgs)
	for _, a := range args {
		vm.callArgs = append(vm.callArgs, vm.valueOf(a))
	}
	v, err := f(vm.callArgs[n:len(vm.callArgs):len(vm.callArgs)])
	clear(vm.callArgs[n:])
	vm.callArgs = vm.callArgs[:n]
	return v, err
}

// Call calls the method name of v with v as this. The arguments are converted with ValueOf.
// The errors are those of Get, and a method that does not exist is an *Error holding a TypeError.
// When ctx is done, the call stops at its next loop iteration or call, and the error is the cause of ctx.
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

// Invoke calls the function v with undefined as this. The arguments are converted with ValueOf.
// The error is a *ValueError when v is not a function, and an *Error when it throws.
func (v Value) Invoke(ctx context.Context, args ...any) (Value, error) {
	return v.invoke(ctx, "Value.Invoke", false, args)
}

// New calls the function v as a constructor, as the new operator does. The arguments are
// converted with ValueOf. The errors are those of Invoke.
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
// The errors are those of Get, for t.
func (v Value) InstanceOf(t Value) (bool, error) {
	vm, err := t.vm("Value.InstanceOf")
	if err != nil {
		return false, err
	}
	r, err := vm.e.HostInstanceOf(v.engine(), t.engine())
	return r, vm.publicError(err)
}

// Func is a function implemented in Go.
//
// args is only valid until the function returns. The result is converted with ValueOf.
//
// A non-nil error is thrown as a JavaScript exception: an *Error throws its value,
// and any other error throws a new Error object with the message of the error.
type Func func(this Value, args []Value) (any, error)

// VM is a JavaScript virtual machine. It must not be used from more than one goroutine at a time,
// and neither must its values.
type VM struct {
	e *engine.VM

	// args is the storage of the arguments of the functions implemented in Go that are running.
	args []Value

	// callArgs is the storage of the arguments of the calls from Go that are running.
	callArgs []engine.Value
}

// NewVM creates a VM with the standard built-in objects.
func NewVM() *VM {
	e := engine.New()
	builtins.Install(e)
	vm := &VM{e: e}
	e.Host = vm
	return vm
}

// Global returns the global object.
func (vm *VM) Global() Value {
	return fromEngine(vm.e.GlobalObject())
}

// ValueOf returns x as a JavaScript value:
//
//	| Go                     | JavaScript             |
//	| ---------------------- | ---------------------- |
//	| Value                  | [its value]            |
//	| Func                   | function               |
//	| nil                    | null                   |
//	| bool                   | boolean                |
//	| integers and floats    | number                 |
//	| *big.Int               | bigint                 |
//	| string                 | string                 |
//	| []any                  | new array              |
//	| map[string]any         | new object             |
//
// A string is taken to be UTF-8. The keys of a map become properties in sorted order.
// ValueOf panics when x is not one of the expected types.
func (vm *VM) ValueOf(x any) Value {
	return fromEngine(vm.valueOf(x))
}

func (vm *VM) valueOf(x any) engine.Value {
	switch x := x.(type) {
	case Value:
		return x.engine()
	case Func:
		return vm.funcOf("", x)
	case nil:
		return engine.Null
	case bool:
		return engine.Bool(x)
	case int:
		return engine.Number(float64(x))
	case int8:
		return engine.Number(float64(x))
	case int16:
		return engine.Number(float64(x))
	case int32:
		return engine.Number(float64(x))
	case int64:
		return engine.Number(float64(x))
	case uint:
		return engine.Number(float64(x))
	case uint8:
		return engine.Number(float64(x))
	case uint16:
		return engine.Number(float64(x))
	case uint32:
		return engine.Number(float64(x))
	case uint64:
		return engine.Number(float64(x))
	case uintptr:
		return engine.Number(float64(x))
	case float32:
		return engine.Number(float64(x))
	case float64:
		return engine.Number(x)
	case *big.Int:
		return vm.e.NewBigInt(new(big.Int).Set(x))
	case string:
		return vm.e.StringFromGo(x)
	case []any:
		elems := make([]engine.Value, len(x))
		for i, e := range x {
			elems[i] = vm.valueOf(e)
		}
		return vm.e.NewArrayOf(elems)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		o := vm.e.NewPlainObject()
		for _, k := range keys {
			// Storing a property on a new plain object cannot fail.
			_ = vm.e.HostSet(o, vm.e.StringFromGo(k), vm.valueOf(x[k]))
		}
		return o
	}
	panic(fmt.Sprintf("ekuma: ValueOf: invalid value of type %T", x))
}

// FuncOf returns a JavaScript function that calls f. name is the name of the function,
// which stack traces show.
func (vm *VM) FuncOf(name string, f Func) Value {
	return fromEngine(vm.funcOf(name, f))
}

func (vm *VM) funcOf(name string, f Func) engine.Value {
	return vm.e.NewFunction(name, func(_ *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := len(vm.args)
		for _, a := range args {
			vm.args = append(vm.args, fromEngine(a))
		}
		r, err := f(fromEngine(this), vm.args[n:len(vm.args):len(vm.args)])
		clear(vm.args[n:])
		vm.args = vm.args[:n]
		if err != nil {
			vm.e.Throw(vm.thrownValue(err))
		}
		return vm.valueOf(r)
	})
}

// thrownValue returns the JavaScript value that the error of a Func throws.
func (vm *VM) thrownValue(err error) engine.Value {
	if ex, ok := errors.AsType[*Error](err); ok {
		return ex.Value.engine()
	}
	return vm.e.NewError(engine.ErrorKindError, err.Error())
}

// SyntaxError is a script that could not be parsed or compiled.
type SyntaxError struct {
	// File is the name of the script.
	File string

	// Line is the 1-based line of the error.
	Line int

	Message string
}

func (e *SyntaxError) Error() string {
	return "SyntaxError: " + e.Message + " (" + e.File + ":" + strconv.Itoa(e.Line) + ")"
}

// Error is a JavaScript exception: one that reached Go, or one for a Func to throw.
type Error struct {
	// Value is the thrown value.
	Value Value
}

// Error returns the stack trace or the message of a thrown error object,
// and a description of any other thrown value. It runs no JavaScript.
func (e *Error) Error() string {
	return engine.ThrownMessage(e.Value.engine())
}

// publicError converts an error of the engine to the error types of this package.
func (vm *VM) publicError(err error) error {
	if err == nil {
		return nil
	}
	if se, ok := errors.AsType[*engine.SyntaxError](err); ok {
		return &SyntaxError{
			File:    se.File,
			Line:    se.Line,
			Message: se.Message,
		}
	}
	if ex, ok := errors.AsType[*engine.Error](err); ok {
		return &Error{Value: fromEngine(ex.Value)}
	}
	return err
}

// run calls f, which executes JavaScript, and stops it when ctx is done.
func (vm *VM) run(ctx context.Context, f func() (engine.Value, error)) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Undefined, context.Cause(ctx)
	}
	if ctx.Done() != nil {
		fired := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			vm.e.Interrupt()
			close(fired)
		})
		defer func() {
			if !stop() {
				// The request may not have been observed: it must not stop a later call.
				<-fired
				vm.e.ClearInterrupt()
			}
		}()
	}
	v, err := f()
	if errors.Is(err, engine.ErrInterrupted) {
		return Undefined, context.Cause(ctx)
	}
	return fromEngine(v), vm.publicError(err)
}

// RunScript evaluates src as a script and returns the value of its last expression statement.
// file names the script in error messages.
//
// The error is a *SyntaxError when src is not a valid script and an *Error when the script throws.
// When ctx is done, the script stops at its next loop iteration or call, and the error is
// the cause of ctx.
func (vm *VM) RunScript(ctx context.Context, file, src string) (Value, error) {
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.e.RunScript(file, src)
	})
}
