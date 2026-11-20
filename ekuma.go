// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package ekuma is a JavaScript engine written in pure Go.
//
// The API follows [syscall/js] where possible. The differences come from JavaScript
// running inside the process: there can be several VMs, an exception is returned as an error
// and not thrown as a panic, and the methods that run a script or call a function take
// a [context.Context] that stops them.
//
// A context stops everything that runs under it: when a [Func] reads a property or calls
// a function, that is stopped by the context of the call that led to the [Func]. When
// a [Func] makes a call with another context and that context stops the call, everything
// stops: the outermost Go call that executes JavaScript returns the cause of that context,
// and the [Func] does not continue. A [Func] that recovers the stop runs no more JavaScript,
// and the stop continues when it returns. A context that becomes done after its call returns
// has no effect.
//
// Internal operation failures return an [*InternalError] from the outermost Go call
// executing JavaScript. They stop execution without running JavaScript catch or finally
// blocks, discard pending jobs, and reject unfinished module evaluations. Side effects remain.
//
// Pending promise jobs run before the outermost Go call that executes JavaScript returns,
// including property operations and calls that throw. Calls reentered from host callbacks
// leave jobs to the outermost call. An interruption discards pending jobs.
// Operations without a context can run JavaScript and jobs without a cancellation limit,
// unless reentered under a call with a context.
// [VM.AwaitJobs] waits for asynchronous notifications and runs their jobs.
//
// A panic in a [Func], or in another host function called by JavaScript, passes
// through Go calls unchanged after restoring the VM's execution state. Host callbacks
// may recover panics from reentrant calls. At the outermost boundary, pending jobs and
// unhandled rejection reports are discarded and unfinished module evaluations are rejected.
// Abandoned synchronous module evaluations are rejected before returning to the host.
// Aborted generators are closed without running JavaScript catch or finally blocks.
// Side effects remain, and promises for abandoned async work may remain pending.
// Host callbacks must not call [runtime.Goexit], which leaves the VM unusable.
//
// The ekumavalidate build tag enables internal checks on the bytecode the compiler emits
// and on its execution by the VM.
package ekuma

import (
	"errors"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// ValueError reports an operation that is unsupported for a [Value]'s type.
// Operations that do not return an error panic with a [*ValueError] instead.
type ValueError struct {
	Method string
	Type   Type
}

func (e *ValueError) Error() string {
	return "ekuma: call of " + e.Method + " on " + e.Type.String()
}

// SyntaxError reports a failure to parse or compile a script or module.
type SyntaxError struct {
	// File is the name of the script.
	File string

	// Line is the 1-based line number of the error.
	Line int

	Message string
}

func (e *SyntaxError) Error() string {
	return "SyntaxError: " + e.Message + " (" + e.File + ":" + strconv.Itoa(e.Line) + ")"
}

// InternalError is a failure of an internal engine operation.
type InternalError struct {
	// Err is the underlying failure.
	Err error
}

func (e *InternalError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *InternalError) Unwrap() error {
	return e.Err
}

// ScriptError is a JavaScript exception: one that reached Go, or one for a [Func] to throw.
type ScriptError struct {
	// Value is the thrown value.
	Value Value
}

// Error returns the stack trace or the message of a thrown error object,
// and a description of any other thrown value. It runs no JavaScript.
func (e *ScriptError) Error() string {
	return engine.ThrownMessage(e.Value.value)
}

// Unwrap returns the Go error that e.Value was made from, or nil when it was not.
func (e *ScriptError) Unwrap() error {
	return publicInternalError(engine.GoError(e.Value.value))
}

// publicError converts an error of the engine to the error types of this package.
func publicError(err error) error {
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
		return &ScriptError{Value: fromEngine(ex.Value)}
	}
	return publicInternalError(err)
}

func publicInternalError(err error) error {
	if e, ok := err.(*xerrors.InternalError); ok {
		return &InternalError{
			Err: e,
		}
	}
	return err
}

// thrownValue returns the JavaScript value that the error of a [Func] throws.
func (vm *VM) thrownValue(err error) engine.Value {
	if ex, ok := errors.AsType[*ScriptError](err); ok {
		return vm.acceptValue(ex.Value)
	}
	return vm.e.NewGoError(err)
}
