// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Function is the state of a callable object.
type Function struct {
	// Code is the compiled code of a function written in JavaScript.
	Code *FuncProto

	// Upvals are the variables of enclosing functions that Code refers to.
	Upvals []*Upvalue

	// Info describes a native function. It is nil for any other function.
	Info *NativeInfo

	Bound *BoundFunc

	// Home is the object that super property accesses in a method start from.
	Home *Object

	// Fields are the elements that a class constructor adds to every instance, or nil when
	// there are none.
	Fields *[]ClassElement

	// shared marks the initial state shared by closures of the same function.
	shared bool

	// Materialized is set once the initial properties, such as length and name, exist as slots.
	Materialized bool

	// Constructor says whether the function can be called with new.
	Constructor bool

	// Derived marks the constructor of a class with an extends clause.
	Derived bool

	// ClassConstructor marks a function that throws when called without new.
	ClassConstructor bool

	// Scopes points to the dynamic scopes that were open where the function was created,
	// innermost last, or is nil when none were.
	Scopes *[]*Object
}

// ChangeableInfo returns the description of the native function f, which must not be a proxy, for
// changing it.
func (f *Function) ChangeableInfo() *NativeInfo {
	if f.Info == &ProxyInfo {
		panic("engine: change of the description of a proxy")
	}
	return f.Info
}

// NativeName returns the initial value of the property name of the native function f.
func (f *Function) NativeName() wtf8.String {
	if f.Info == nil {
		return ""
	}
	return f.Info.Name
}

// Frameless reports whether f, which may be nil, is a native function whose calls show no frame.
func (f *Function) Frameless() bool {
	return f != nil && f.Info != nil && f.Info.Frameless
}

// CapturedScopes returns the dynamic scopes that were open where f was created, innermost last.
func (f *Function) CapturedScopes() []*Object {
	if f.Scopes == nil {
		return nil
	}
	return *f.Scopes
}

// SourceText returns the result of Function.prototype.toString for the function f.
func (f *Object) SourceText() wtf8.String {
	fn := f.Fn
	if fn.Code != nil && fn.Code.Source != "" {
		return fn.Code.Source
	}
	name := fn.NativeName()
	if fn.Code != nil {
		name = fn.Code.Name
	}
	// The name is a PropertyName after an optional accessor word, and is left out when it is not.
	n := strings.TrimPrefix(strings.TrimPrefix(string(name), "get "), "set ")
	if !parser.IsIdentifierName(n) && !strings.HasPrefix(n, "[") {
		name = ""
	}
	return "function " + name + "() { [native code] }"
}

type BoundFunc struct {
	Target *Object
	This   Value
	Args   []Value
}

// Upvalue is a variable of an enclosing function. P points to the variable: into the registers
// of that function while it is running, and to Val afterwards.
type Upvalue struct {
	P *Value

	// Index is the stack index or, for a generator, the register that P points to while the upvalue is open.
	Index int
	Val   Value
}
