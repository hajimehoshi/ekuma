// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// VM is a JavaScript virtual machine with the standard built-in objects.
// The zero value is ready to use. A VM must not be copied after first use.
// A VM and its values and realms must not be used from more than one goroutine at a time.
type VM struct {
	e *engine.Host

	initialRealm *Realm

	// args is the storage of the arguments of the functions implemented in Go that are running.
	args []Value

	// callArgs is the storage of the arguments of the calls from Go that are running.
	callArgs []engine.Value

	interrupts *interruptRequests

	// valuePath holds the maps and slices that ValueOf is converting, from the outermost one.
	valuePath []containerID
}

// containerID identifies a map by its pointer, or a slice by its first element and length.
type containerID struct {
	mapPtr uintptr
	elem0  *any
	len    int
}

// interruptRequest is the request of a call to stop, which the context of the call makes when
// it is done.
type interruptRequest struct {
	cause error

	// released is whether the call has ended.
	released bool
}

// interruptRequests are the requests of the running calls whose contexts are done.
type interruptRequests struct {
	m sync.Mutex

	// pending are the requests in the order in which they were made. The engine is asked to stop
	// with the cause of the first one for as long as there is one.
	pending []*interruptRequest
}

// request adds req with cause, unless its call has ended. It may be called from any goroutine.
func (r *interruptRequests) request(e *engine.Host, req *interruptRequest, cause error) {
	r.m.Lock()
	defer r.m.Unlock()
	if req.released {
		return
	}
	req.cause = cause
	r.pending = append(r.pending, req)
	if len(r.pending) == 1 {
		e.Interrupt(cause)
	}
}

// release ends the call of req and withdraws req. The requests of the other calls stay.
func (r *interruptRequests) release(e *engine.Host, req *interruptRequest) {
	r.m.Lock()
	defer r.m.Unlock()
	req.released = true
	i := slices.Index(r.pending, req)
	if i < 0 {
		return
	}
	r.pending = slices.Delete(r.pending, i, i+1)
	if len(r.pending) == 0 {
		// The request may not have been observed: it must not stop a later call.
		e.ClearInterrupt()
		return
	}
	// The engine is asked again, as the withdrawn request may be the one that it has.
	e.Interrupt(r.pending[0].cause)
}

func (vm *VM) hostContext() *engine.ExecutionContext {
	return vm.e.ActiveContext()
}

// initIfNeeded creates the engine of vm on first use, and panics when vm is a copy of a [VM] in
// use.
func (vm *VM) initIfNeeded() {
	if vm.e == nil {
		cx, host := engine.NewExecutionContext(vm)
		vm.e = host
		vm.interrupts = &interruptRequests{}
		builtin.Install(cx)
		vm.initialRealm = vm.realmOf(cx)
		return
	}
	if vm.e.HostDefined() != any(vm) {
		panic("ekuma: VM used through a copy")
	}
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
// A function of the type underlying [Func] is taken as a [Func].
// ValueOf panics when x is not one of the expected types, or is a nil pointer or a nil [Func],
// or a *big.Int of more than 2^30 bits, the largest BigInt.
// It panics for a string, symbol, object or function of another VM,
// including values nested in arrays and maps, and for a map or slice that contains itself.
//
// The objects that ValueOf creates belong to the initial realm.
func (vm *VM) ValueOf(x any) Value {
	vm.initIfNeeded()
	return fromEngine(vm.valueOf(vm.initialRealm.cx, x))
}

// valueOf implements [Realm.ValueOf] in the execution realm.
func (vm *VM) valueOf(cx *engine.ExecutionContext, x any) engine.Value {
	switch x := x.(type) {
	case Value:
		return vm.acceptValue(x)
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
		if x == nil {
			panic("ekuma: ValueOf: nil *big.Int")
		}
		if x.IsInt64() {
			return engine.BigIntFromInt64(x.Int64())
		}
		if x.BitLen() > engine.MaxBigIntBits {
			panic(fmt.Sprintf("ekuma: ValueOf: BigInt of %d bits, more than %d", x.BitLen(), engine.MaxBigIntBits))
		}
		return cx.NewBigInt(new(big.Int).Set(x))
	case string:
		return cx.StringFromGo(x)
	case func(Value, []Value) (any, error):
		return vm.valueOf(cx, Func(x))
	case Func, *SharedDataBlock, []any, map[string]any:
		return vm.objectOf(cx, x)
	}
	panic(fmt.Sprintf("ekuma: ValueOf: invalid value of type %T", x))
}

// acceptValue checks ownership before passing v to the engine.
func (vm *VM) acceptValue(v Value) engine.Value {
	if o := v.value.Object(); o != nil {
		if !vm.e.Owns(o) {
			panic("ekuma: value belongs to another VM: " + v.String())
		}
	}
	return v.value
}

// objectOf converts a host value into an object of the execution realm.
func (vm *VM) objectOf(cx *engine.ExecutionContext, x any) engine.Value {
	switch x := x.(type) {
	case Func:
		if x == nil {
			panic("ekuma: ValueOf: nil Func")
		}
		return funcOf(cx, "", x)
	case *SharedDataBlock:
		if x == nil || x.b == nil {
			panic("ekuma: ValueOf: invalid *SharedDataBlock")
		}
		proto := cx.Intrinsic(engine.IntrinsicSharedArrayBufferPrototype)
		return engine.ObjectValue(cx.NewSharedArrayBufferObject(proto, x.b))
	case []any:
		if len(x) > 0 {
			defer vm.leaveContainer(vm.enterContainer(containerID{elem0: &x[0], len: len(x)}))
		}
		elems := make([]engine.Value, len(x))
		for i, e := range x {
			elems[i] = vm.valueOf(cx, e)
		}
		return cx.NewArrayOf(elems)
	case map[string]any:
		if len(x) > 0 {
			defer vm.leaveContainer(vm.enterContainer(containerID{mapPtr: reflect.ValueOf(x).Pointer()}))
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		o := cx.NewPlainObject()
		for _, k := range keys {
			cx.CreateDataPropertyOrThrow(o.Object(), cx.ToPropertyKey(cx.StringFromGo(k)), vm.valueOf(cx, x[k]))
		}
		return o
	}
	panic("not reached")
}

// enterContainer adds id to vm.valuePath and returns its index there. It panics when id is
// already on the path.
func (vm *VM) enterContainer(id containerID) int {
	if slices.Contains(vm.valuePath, id) {
		panic("ekuma: ValueOf: cyclic value")
	}
	vm.valuePath = append(vm.valuePath, id)
	return len(vm.valuePath) - 1
}

// leaveContainer removes the entries of vm.valuePath from the index i.
func (vm *VM) leaveContainer(i int) {
	clear(vm.valuePath[i:])
	vm.valuePath = vm.valuePath[:i]
}

// Func is a function implemented in Go.
//
// args is valid only until the function returns. The result is converted with [Realm.ValueOf]
// of the realm of the function, except that a *big.Int result of more than 2^30 bits throws a
// RangeError, as a BigInt operation of JavaScript with such a result does.
//
// A non-nil error is thrown as a JavaScript exception: a [*ScriptError] throws its value,
// and any other error throws a new Error object made from the error, with its message.
// [ScriptError.Unwrap] of a [*ScriptError] holding that object returns the error.
type Func func(this Value, args []Value) (any, error)

// FuncOf returns a JavaScript function of the initial realm that calls f. name is the name
// of the function, which stack traces show. FuncOf panics when f is nil.
func (vm *VM) FuncOf(name string, f Func) Value {
	vm.initIfNeeded()
	return vm.initialRealm.FuncOf(name, f)
}

// funcOf implements [Realm.FuncOf] for the execution context.
func funcOf(cx *engine.ExecutionContext, name string, f Func) engine.Value {
	if f == nil {
		panic("ekuma: FuncOf: nil Func")
	}
	return engine.ObjectValue(cx.NewNativeWithState(wtf8.FromValidUTF8(name), 0, nativeHostFunction, f))
}

// nativeHostFunction leads to the code that makes its function objects.
var nativeHostFunction engine.Native

func init() {
	nativeHostFunction = engine.RegisterNative(hostFunction)
}

// hostFunction is the code of the functions that [Realm.FuncOf] makes. Their state is a [Func].
func hostFunction(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := cx.ActiveNativeState().(Func)
	vm := cx.HostDefined().(*VM)
	n := len(vm.args)
	if len(args) > 0 {
		defer vm.dropArgs(n)
		for _, a := range args {
			vm.args = append(vm.args, fromEngine(a))
		}
	}
	res, err := f(fromEngine(this), vm.args[n:len(vm.args):len(vm.args)])
	cx.ResumeInterruption()
	if err != nil {
		cx.Throw(vm.thrownValue(cx, err))
	}
	if b, ok := res.(*big.Int); ok && b != nil && b.BitLen() > engine.MaxBigIntBits {
		cx.ThrowRangeError("maximum BigInt size exceeded")
	}
	return vm.valueOf(cx, res)
}

// dropArgs removes the arguments from the index n on.
func (vm *VM) dropArgs(n int) {
	dropped := vm.args[n:]
	vm.args = vm.args[:n]
	if len(dropped) > 4 {
		clear(dropped)
		return
	}
	// Clearing a few values one by one costs less than the setup of clear.
	for i := len(dropped) - 1; i >= 0; i-- {
		dropped[i] = Value{}
	}
}

// withArgs converts the arguments of a call with [VM.ValueOf] and passes them to f.
func (vm *VM) withArgs(args []any, f func(args []engine.Value) (engine.Value, error)) (engine.Value, error) {
	n := len(vm.callArgs)
	defer func() {
		clear(vm.callArgs[n:])
		vm.callArgs = vm.callArgs[:n]
	}()
	for _, a := range args {
		vm.callArgs = append(vm.callArgs, vm.valueOf(vm.initialRealm.cx, a))
	}
	return f(vm.callArgs[n:len(vm.callArgs):len(vm.callArgs)])
}

// run calls f, which executes JavaScript, and stops it when ctx is done.
func (vm *VM) run(ctx context.Context, f func() (engine.Value, error)) (Value, error) {
	if err := ctx.Err(); err != nil {
		cause := context.Cause(ctx)
		if vm.e.Running() {
			// Inside running JavaScript, the call stops everything, as when ctx becomes done
			// during the call: the interruption unwinds through the callers to the outermost
			// call.
			var req interruptRequest
			vm.interrupts.request(vm.e, &req, cause)
			defer vm.interrupts.release(vm.e, &req)
			vm.e.Poll()
		}
		return Undefined, cause
	}
	if ctx.Done() != nil {
		var req interruptRequest
		stop := context.AfterFunc(ctx, func() {
			vm.interrupts.request(vm.e, &req, context.Cause(ctx))
		})
		defer func() {
			if !stop() {
				vm.interrupts.release(vm.e, &req)
			}
		}()
	}
	v, err := f()
	return fromEngine(v), publicError(err)
}

// ErrRunning is returned by [VM.AwaitJobs] when the VM is running JavaScript.
var ErrRunning = errors.New("ekuma: AwaitJobs called while JavaScript runs")

// AwaitJobs runs pending jobs, waiting for an Atomics.waitAsync waiter to be
// notified or time out if necessary. It reports true if jobs were run, or
// false without blocking if there are no pending jobs or asynchronous waiters.
// It returns [ErrRunning] if the VM is running JavaScript, or the cause of ctx
// if ctx is already done or interrupts the wait or job execution.
func (vm *VM) AwaitJobs(ctx context.Context) (bool, error) {
	vm.initIfNeeded()
	if vm.e.Running() {
		return false, ErrRunning
	}
	var waited bool
	_, err := vm.run(ctx, func() (engine.Value, error) {
		var err error
		if waited, err = vm.e.AwaitJobs(); !waited {
			return engine.Undefined, err
		}
		return engine.Undefined, vm.hostContext().RunJobs()
	})
	return waited, err
}

// SetLocale sets the default locale for locale-sensitive operations in every realm.
// A locale that is not available uses its longest available prefix, such as "ja" for "ja-XX",
// and an empty locale or one without an available prefix uses the host default. Existing Intl
// instances keep their locale. A malformed locale returns an error without changing the default.
func (vm *VM) SetLocale(locale string) error {
	vm.initIfNeeded()
	if locale != "" {
		t, ok := localetag.Parse(locale)
		if !ok {
			return fmt.Errorf("ekuma: invalid locale %q", locale)
		}
		locale = t.String()
	}
	vm.e.SetDefaultLocale(locale)
	return nil
}

// SetTimeZone sets loc as the local time zone for Date, the system time zone for Temporal,
// and the default time zone for new Intl.DateTimeFormat instances.
// A nil loc restores the local time zone of the host.
// Date uses loc's rules when loc is [time.Local] or the platform does not recognize its
// name. Otherwise Date uses the platform's rules for that name.
// Temporal and Intl resolve the zone by identifier even for [time.Local]. If loc has no
// usable identifier, they use its current UTC offset.
func (vm *VM) SetTimeZone(loc *time.Location) {
	vm.initIfNeeded()
	if loc == nil {
		vm.e.SetDefaultTimeZone(nil)
		return
	}
	if loc != time.Local {
		if _, _, z, ok := datetime.LoadZone(loc.String()); ok {
			vm.e.SetDefaultTimeZone(z)
			return
		}
	}
	vm.e.SetDefaultTimeZone(datetime.LocationZone(loc))
}

// SetClock sets now as the clock that gives the current time to Date, Temporal.Now, the
// default date of Intl.DateTimeFormat, and the timeouts of Atomics.wait and Atomics.waitAsync.
// A nil now restores [time.Now]. Only the instant of a time matters, not its location;
// [VM.SetTimeZone] sets the time zone. A timeout ends no earlier than the clock passes it, and
// for Atomics.wait no earlier than its duration in real time. A time outside the range of Date
// reads as the nearest end of the range. now is called on the goroutine that runs JavaScript in
// the VM.
func (vm *VM) SetClock(now func() time.Time) {
	vm.initIfNeeded()
	vm.e.SetClock(now)
}

// SetRandomSource sets src as the source of random numbers for Math.random. A nil src restores
// the default, a source that is seeded randomly. The source applies to the later calls of
// Math.random, which return the numbers that [rand.New](src).Float64 would.
func (vm *VM) SetRandomSource(src rand.Source) {
	vm.initIfNeeded()
	vm.e.SetRandomSource(src)
}

// SetCanBlock sets whether Atomics.wait may block the VM. The default is false.
func (vm *VM) SetCanBlock(canBlock bool) {
	vm.initIfNeeded()
	vm.e.SetAgentCanSuspend(canBlock)
}

// SetUnhandledRejection sets f as the callback for each promise rejected with reason
// that has no rejection handler. f is called when the outermost call from Go into
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
	vm.e.SetHostUnhandledRejection(func(cx *engine.ExecutionContext, promise, reason engine.Value) {
		f(fromEngine(promise), fromEngine(reason))
	})
}

// WithContinuationData calls f with data as the continuation data, and restores the previous
// continuation data when f returns or panics.
//
// A promise job runs with the continuation data that was current when it was registered: when
// then was called, await was reached, or a promise was resolved with a thenable. The cleanup
// callback of a FinalizationRegistry runs with the continuation data that was current when the
// registry was made.
func (vm *VM) WithContinuationData(data any, f func()) {
	vm.initIfNeeded()
	prev := vm.e.ContinuationData()
	vm.e.SetContinuationData(data)
	defer vm.e.SetContinuationData(prev)
	f()
}

// ContinuationData returns the continuation data set by the innermost [VM.WithContinuationData]
// call or running job, or nil if there is none.
func (vm *VM) ContinuationData() any {
	vm.initIfNeeded()
	return vm.e.ContinuationData()
}
