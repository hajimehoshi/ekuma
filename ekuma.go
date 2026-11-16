// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package ekuma is a JavaScript engine written in pure Go.
//
// The API follows the one of [syscall/js] where it can. The differences come from JavaScript
// running inside the process: there can be several VMs, an exception is returned as an error
// and not thrown as a panic, and the methods that run a script or call a function take
// a [context.Context] that stops them.
//
// A context stops everything that runs under it: when a [Func] reads a property or calls
// a function, that is stopped by the context of the call that led to the [Func].
//
// Pending promise jobs run before the outermost Go call that executes JavaScript returns,
// including property operations and calls that throw. Calls reentered from host callbacks
// leave jobs to the outermost call. An interruption discards pending jobs.
// Operations without a context can run JavaScript and jobs without a cancellation limit,
// unless reentered under a call with a context.
// [VM.AwaitJobs] waits for asynchronous notifications and runs their jobs.
package ekuma

import (
	"errors"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// ValueError is the error of an operation on a [Value] that does not support the value.
// The operations that return no error panic with it.
type ValueError struct {
	Method string
	Type   Type
}

func (e *ValueError) Error() string {
	return "ekuma: call of " + e.Method + " on " + e.Type.String()
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

// Error is a JavaScript exception: one that reached Go, or one for a [Func] to throw.
type Error struct {
	// Value is the thrown value.
	Value Value
}

// Error returns the stack trace or the message of a thrown error object,
// and a description of any other thrown value. It runs no JavaScript.
func (e *Error) Error() string {
	return engine.ThrownMessage(e.Value.engine())
}

// Unwrap returns the Go error that e.Value was made from, or nil when it was not.
func (e *Error) Unwrap() error {
	return engine.GoError(e.Value.engine())
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

// thrownValue returns the JavaScript value that the error of a [Func] throws.
func (vm *VM) thrownValue(err error) engine.Value {
	if ex, ok := errors.AsType[*Error](err); ok {
		return vm.acceptValue(ex.Value)
	}
	return vm.e.NewGoError(err)
}
