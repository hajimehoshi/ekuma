// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// nativeTableSize is the number of entries of the table of the native functions. It is a power of
// two, so that masking an index keeps it in range.
const nativeTableSize = 1 << 10

// Native identifies a registered native function. The zero value identifies none.
type Native struct {
	index uint16
}

// nativeTable holds the registered native functions. It does not change after the initialization
// of the packages.
var nativeTable struct {
	// codes holds the code of the functions by the index of their [Native].
	codes [nativeTableSize]NativeFunc

	// fasts holds what callers use instead of calling the functions by the index of their
	// [Native], or nil.
	fasts [nativeTableSize]*nativeFast

	// count is the number of registered functions. The entry at index 0 is that of the
	// zero Native, whose calls panic.
	count int
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
	nativeTable.fasts[i] = fast
	nativeTable.count = i
	return Native{
		index: uint16(i),
	}
}

// code returns the code of n.
func (n Native) code() NativeFunc {
	return nativeTable.codes[n.index%nativeTableSize]
}

// fast returns what the callers of n use instead of calling it, or nil.
func (n Native) fast() *nativeFast {
	return nativeTable.fasts[n.index%nativeTableSize]
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
			return cx.CreateIterResultObject(Undefined, true)
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
func RegisterPlainIteratorNext[T any](yieldRealm IteratorYieldRealm, receiverError string, step func(QuietContext, *Object, T) (Value, bool), pairStep IteratorPairStepFunc) Native {
	code := func(cx *ExecutionContext, this Value, args []Value) Value {
		state, ok := IteratorState(this).(T)
		if !ok {
			cx.ThrowTypeError(receiverError)
		}
		v, ok := step(cx.Quiet(), this.o, state)
		if !ok {
			return cx.CreateIterResultObject(Undefined, true)
		}
		return newIteratorResult(cx, yieldRealm, this, v)
	}
	return registerNative(code, &nativeFast{
		step: func(cx *ExecutionContext, this Value) (Value, bool) {
			if state, ok := IteratorState(this).(T); ok {
				return step(cx.Quiet(), this.o, state)
			}
			// The receiver error belongs to the native next call.
			return Value{o: stepDeclined}, false
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
		return this.o.CreationContext().CreateIterResultObject(v, false)
	}
	return cx.CreateIterResultObject(v, false)
}
