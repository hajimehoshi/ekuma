// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// nativeTableSize is the number of entries of the table of the native functions. It is a power of
// two, so that masking an index keeps it in range.
const nativeTableSize = 1 << 10

// maxNativeFasts is the number of entries of the table of the fast forms. Every uint8 is in range.
const maxNativeFasts = 1 << 8

// nativeTable holds the registered native functions. It does not change after the initialization
// of the packages.
var nativeTable struct {
	// codes holds the code of the functions by the index of their [Native].
	codes [nativeTableSize]NativeFunc

	// fasts holds what callers use instead of calling the functions by the FastIndex of their
	// [Native]. The entry at index 0 is nil.
	fasts [maxNativeFasts]*nativeFast

	// count is the number of registered functions. The entry at index 0 is that of the
	// zero Native, whose calls panic.
	count int

	// fastCount is the number of registered functions with fast forms.
	fastCount int
}

func init() {
	nativeTable.codes[0] = func(*ExecutionContext, Value, []Value) Value {
		panic("engine: call of no native function")
	}
}

// RegisterNative registers the native function with the code f. It must be called only in the
// initialization of a package.
func RegisterNative(f NativeFunc) Native {
	return registerNative(f, nil)
}

func registerNative(f NativeFunc, fast *nativeFast) Native {
	if f == nil {
		panic("engine: registration of a nil native function")
	}
	i := nativeTable.count + 1
	if i >= nativeTableSize {
		panic("engine: too many native functions")
	}
	nativeTable.codes[i] = f
	nativeTable.count = i
	return Native{
		Index:     uint16(i),
		FastIndex: addNativeFast(fast),
	}
}

// addNativeFast adds fast, which may be nil, to the table and returns its index.
func addNativeFast(fast *nativeFast) uint8 {
	if fast == nil {
		return 0
	}
	i := nativeTable.fastCount + 1
	if i >= maxNativeFasts {
		panic("engine: too many native functions with fast forms")
	}
	nativeTable.fasts[i] = fast
	nativeTable.fastCount = i
	return uint8(i)
}

// nativeCodeOf returns the code of n.
func nativeCodeOf(n Native) NativeFunc {
	return nativeTable.codes[n.Index%nativeTableSize]
}

// nativeFastOf returns what the callers of n use instead of calling it, or nil.
func nativeFastOf(n Native) *nativeFast {
	return nativeTable.fasts[uint(n.FastIndex)]
}

// Kernels are the kernels of a native function. Any of them may be nil.
type Kernels struct {
	// Number1 and Number2 stand for the calls with exactly one and two arguments that are numbers.
	// They must return what the function returns for those arguments.
	Number1 NumberKernel1
	Number2 NumberKernel2

	// Method0, Method1 and Method2 are tried for the calls with exactly no, one and two arguments.
	// When they report true, the result must be what the function returns for the call.
	Method0 MethodKernel0
	Method1 MethodKernel1
	Method2 MethodKernel2

	// Contract is the contract of the kernels.
	Contract ShortcutContract
}

// RegisterNativeWithKernels is [RegisterNative] for a function with the kernels k.
func RegisterNativeWithKernels(f NativeFunc, k Kernels) Native {
	return registerNative(f, &nativeFast{
		number1:  k.Number1,
		number2:  k.Number2,
		method0:  k.Method0,
		method1:  k.Method1,
		method2:  k.Method2,
		contract: k.Contract,
	})
}

// RegisterIteratorNext registers the next method of built-in iterators, which returns the iterator
// result for what step returns. It must be called only in the initialization of a package.
func RegisterIteratorNext(yieldRealm IteratorYieldRealm, step IteratorStepFunc) Native {
	code := func(cx *ExecutionContext, this Value, args []Value) Value {
		v, ok := step(cx, this)
		if !ok {
			return cx.CreateIterResultObject(storage.Undefined, true)
		}
		return newIteratorResult(cx, yieldRealm, this, v)
	}
	return registerNative(code, &nativeFast{
		framedStep: step,
		contract: ShortcutContract{
			Frame: FrameRecorded,
			Pace:  PacePolled,
		},
	})
}

// RegisterPlainIteratorNext registers the next method of the built-in iterators with state T.
// For a compatible receiver, step must neither throw nor run JavaScript. pairStep is the pair step
// of the method, for which the same contract holds, or nil. RegisterPlainIteratorNext must be
// called only in the initialization of a package.
func RegisterPlainIteratorNext[T any](yieldRealm IteratorYieldRealm, receiverError string, step func(quiet.Context, *Object, T) (Value, bool), pairStep IteratorPairStepFunc) Native {
	code := func(cx *ExecutionContext, this Value, args []Value) Value {
		state, ok := storage.IteratorState(this).(T)
		if !ok {
			cx.ThrowTypeError(receiverError)
		}
		v, ok := step(cx.Quiet(), this.O, state)
		if !ok {
			return cx.CreateIterResultObject(storage.Undefined, true)
		}
		return newIteratorResult(cx, yieldRealm, this, v)
	}
	return registerNative(code, &nativeFast{
		step: func(cx *ExecutionContext, this Value) (Value, bool) {
			if state, ok := storage.IteratorState(this).(T); ok {
				return step(cx.Quiet(), this.O, state)
			}
			// The receiver error belongs to the native next call.
			return Value{O: stepDeclined}, false
		},
		pairStep: pairStep,
		contract: ShortcutContract{
			Frame: FrameUnobservable,
			Pace:  PaceShort,
		},
	})
}

// newIteratorResult returns the result of the next method of the built-in iterator this that yields v.
func newIteratorResult(cx *ExecutionContext, yieldRealm IteratorYieldRealm, this, v Value) Value {
	if yieldRealm == IteratorClosureRealm {
		return CreationContext(this.O).CreateIterResultObject(v, false)
	}
	return cx.CreateIterResultObject(v, false)
}
