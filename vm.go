// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

// VM is a JavaScript virtual machine with the standard built-in objects.
// The zero value is ready to use. A VM must not be copied after first use.
// It must not be used from more than one goroutine at a time, and neither must its values.
type VM struct {
	e *engine.VM

	// args is the storage of the arguments of the functions implemented in Go that are running.
	args []Value

	// callArgs is the storage of the arguments of the calls from Go that are running.
	callArgs []engine.Value
}

// initIfNeeded creates the engine of vm on first use, and panics when vm is a copy of a [VM] in
// use.
func (vm *VM) initIfNeeded() {
	if vm.e == nil {
		vm.e = engine.NewVM(vm)
		builtin.Install(vm.e)
		return
	}
	if vm.e.HostDefined() != any(vm) {
		panic("ekuma: VM used through a copy")
	}
}

// Global returns the global object.
func (vm *VM) Global() Value {
	vm.initIfNeeded()
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
//	| *SharedDataBlock       | new SharedArrayBuffer  |
//	| string                 | string                 |
//	| []any                  | new array              |
//	| map[string]any         | new object             |
//
// A string is taken to be UTF-8. The keys of a map become properties in sorted order.
// ValueOf panics when x is not one of the expected types or is a nil pointer.
func (vm *VM) ValueOf(x any) Value {
	vm.initIfNeeded()
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
	case *SharedDataBlock:
		if x == nil || x.b == nil {
			panic("ekuma: ValueOf: invalid *SharedDataBlock")
		}
		proto := vm.e.Intrinsic(engine.IntrinsicSharedArrayBufferPrototype)
		return engine.ObjectValue(vm.e.NewSharedArrayBufferObject(proto, x.b))
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

// Func is a function implemented in Go.
//
// args is only valid until the function returns. The result is converted with [VM.ValueOf].
//
// A non-nil error is thrown as a JavaScript exception: an [*Error] throws its value,
// and any other error throws a new Error object made from the error, with its message.
// [Error.Unwrap] of an [*Error] holding that object returns the error.
type Func func(this Value, args []Value) (any, error)

// FuncOf returns a JavaScript function that calls f. name is the name of the function,
// which stack traces show.
func (vm *VM) FuncOf(name string, f Func) Value {
	vm.initIfNeeded()
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

// withArgs converts the arguments of a call with [VM.ValueOf] and passes them to f.
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

// run calls f, which executes JavaScript, and stops it when ctx is done.
func (vm *VM) run(ctx context.Context, f func() (engine.Value, error)) (Value, error) {
	if err := ctx.Err(); err != nil {
		return Undefined, context.Cause(ctx)
	}
	if ctx.Done() != nil {
		fired := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			vm.e.Interrupt(context.Cause(ctx))
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
// The error is a [*SyntaxError] when src is not a valid script and an [*Error] when the script
// throws. When ctx is done, the script stops at its next loop iteration or call, and the error
// is the cause of ctx.
func (vm *VM) RunScript(ctx context.Context, file, src string) (Value, error) {
	vm.initIfNeeded()
	return vm.run(ctx, func() (engine.Value, error) {
		return vm.e.RunScript(file, src)
	})
}

// ErrRunning is the error of [VM.AwaitJobs] called while the VM runs JavaScript.
var ErrRunning = errors.New("ekuma: AwaitJobs called while JavaScript runs")

// AwaitJobs runs pending jobs, waiting for an Atomics.waitAsync waiter to be
// notified or time out if necessary. It reports true if jobs were run, or
// false without blocking if there are no asynchronous waiters.
// It returns [ErrRunning] if the VM is running JavaScript, or the cause of ctx
// if ctx is done.
func (vm *VM) AwaitJobs(ctx context.Context) (bool, error) {
	vm.initIfNeeded()
	if vm.e.Running() {
		return false, ErrRunning
	}
	var waited bool
	_, err := vm.run(ctx, func() (engine.Value, error) {
		if waited = vm.e.AwaitWaiters(); !waited {
			return engine.Undefined, nil
		}
		return engine.Undefined, vm.e.RunJobs()
	})
	if err == nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	return waited, err
}

// SetTimeZone sets the time zone of local time to loc. A nil loc restores the default, the
// local time zone of the host. The time zone applies to the later conversions of Date
// between local time and UTC.
func (vm *VM) SetTimeZone(loc *time.Location) {
	vm.initIfNeeded()
	if loc == nil {
		vm.e.SetDefaultTimeZone(nil)
		return
	}
	vm.e.SetDefaultTimeZone(datetime.LocationZone(loc))
}

// SetRandomSource sets src as the source of the numbers of Math.random. A nil src restores
// the default, a source that is seeded randomly. The source applies to the later calls of
// Math.random, which return the numbers that rand.New(src).Float64 would.
func (vm *VM) SetRandomSource(src rand.Source) {
	vm.initIfNeeded()
	vm.e.SetRandomSource(src)
}

// SetCanBlock sets whether Atomics.wait may block the VM. The default is false.
func (vm *VM) SetCanBlock(canBlock bool) {
	vm.initIfNeeded()
	vm.e.SetAgentCanSuspend(canBlock)
}

// SetUnhandledRejection sets f as the function that is told about each promise that was
// rejected with reason and has no handler. f is called when the outermost call from Go into
// vm has run the pending jobs, once for each such promise, in the order of rejection. A
// promise is reported once, and a handler added later does not undo the report. This
// includes the promise that [Module.Evaluate] returns for a module that throws.
//
// f may call into vm and run JavaScript. The jobs that this queues, and the reports of the
// promises that this rejects, run before the outermost call returns.
//
// The reports that are made after the call go to f, including those of promises that were
// rejected earlier in a call that is running. A nil f stops the reports, and the promises
// that would have been reported are discarded.
func (vm *VM) SetUnhandledRejection(f func(promise, reason Value)) {
	vm.initIfNeeded()
	if f == nil {
		vm.e.SetHostUnhandledRejection(nil)
		return
	}
	vm.e.SetHostUnhandledRejection(func(e *engine.VM, promise, reason engine.Value) {
		f(fromEngine(promise), fromEngine(reason))
	})
}
