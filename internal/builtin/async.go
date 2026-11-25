// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// functionKindConstructor creates the constructor name, which calls f, of the functions whose
// prototype object is proto, such as GeneratorFunction. It is not a global.
func functionKindConstructor(cx *engine.ExecutionContext, name wtf8.String, f storage.Native, proto *storage.Object) {
	c := cx.NewNativeConstructor(name, 1, f)
	cx.SetPrototypeOf(c, cx.Intrinsic(storage.IntrinsicFunction))
	cx.Define(c, cx.Key(storage.KeyPrototype), storage.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(storage.KeyConstructor), storage.ObjectValue(c), storage.FlagConfigurable)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString(name), storage.FlagConfigurable)
}

var (
	nativeAsyncFunctionConstructor          = engine.RegisterNative(asyncFunctionConstructor)
	nativeAsyncGeneratorFunctionConstructor = engine.RegisterNative(asyncGeneratorFunctionConstructor)
	nativeAsyncGeneratorNext                = engine.RegisterNative(asyncGeneratorNext)
	nativeAsyncGeneratorReturn              = engine.RegisterNative(asyncGeneratorReturn)
	nativeAsyncGeneratorThrow               = engine.RegisterNative(asyncGeneratorThrow)
	nativeArrayFromAsync                    = engine.RegisterNative(arrayFromAsync)
)

func initAsync(cx *engine.ExecutionContext) {
	functionKindConstructor(cx, "AsyncFunction", nativeAsyncFunctionConstructor, cx.Intrinsic(storage.IntrinsicAsyncFunctionPrototype))
	functionKindConstructor(cx, "AsyncGeneratorFunction", nativeAsyncGeneratorFunctionConstructor, cx.Intrinsic(storage.IntrinsicAsyncGeneratorFunctionPrototype))

	gp := cx.Intrinsic(storage.IntrinsicAsyncGeneratorPrototype)
	method(cx, gp, "next", 1, nativeAsyncGeneratorNext)
	method(cx, gp, "return", 1, nativeAsyncGeneratorReturn)
	method(cx, gp, "throw", 1, nativeAsyncGeneratorThrow)
	cx.Define(gp, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("AsyncGenerator"), storage.FlagConfigurable)
	gf := cx.Intrinsic(storage.IntrinsicAsyncGeneratorFunctionPrototype)
	cx.Define(gf, cx.Key(storage.KeyPrototype), storage.ObjectValue(gp), storage.FlagConfigurable)
	cx.Define(gp, cx.Key(storage.KeyConstructor), storage.ObjectValue(gf), storage.FlagConfigurable)

	method(cx, cx.Intrinsic(storage.IntrinsicArray), "fromAsync", 1, nativeArrayFromAsync)
}

func asyncFunctionConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.CreateDynamicFunction(engine.AsyncFunction, args)
}

func asyncGeneratorFunctionConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.CreateDynamicFunction(engine.AsyncGeneratorFunction, args)
}

func asyncGeneratorNext(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ResumeAsyncGenerator(this, engine.ResumeNext, arg(args, 0))
}

func asyncGeneratorReturn(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ResumeAsyncGenerator(this, engine.ResumeReturn, arg(args, 0))
}

func asyncGeneratorThrow(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ResumeAsyncGenerator(this, engine.ResumeThrow, arg(args, 0))
}

// getIteratorFromMethod implements GetIteratorFromMethod.
func getIteratorFromMethod(cx *engine.ExecutionContext, v storage.Value, m *storage.Object) (it, next storage.Value) {
	it = cx.Call(m, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the iterator method is not an object")
	}
	return it, cx.GetV(it, cx.Intern("next"))
}

// fromAsync is the state of a call of Array.fromAsync.
type fromAsync struct {
	pc      *engine.PromiseCapability
	array   *storage.Object
	k       int64
	mapper  storage.Value
	thisArg storage.Value

	// it and next are the async iterator and its next method, or undefined for an array-like.
	it, next storage.Value

	// arrayLike and length are the array-like object and its length.
	arrayLike *storage.Object
	length    int64
}

func arrayFromAsync(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	pc := cx.NewPromiseCapability(storage.ObjectValue(cx.Intrinsic(storage.IntrinsicPromise)))
	s := &fromAsync{
		pc:      pc,
		mapper:  arg(args, 1),
		thisArg: arg(args, 2),
	}
	items := arg(args, 0)
	custom := this.IsCallable() && cx.IsConstructor(this.Object())
	cx.IfAbruptRejectPromise(pc, func() {
		if !s.mapper.IsUndefined() && !s.mapper.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(s.mapper) + " is not a function")
		}
		if m := cx.GetMethod(items, cx.WellKnownSymbol(storage.SymbolAsyncIterator)); m != nil {
			s.it, s.next = getIteratorFromMethod(cx, items, m)
		} else if m := cx.GetMethod(items, cx.WellKnownSymbol(storage.SymbolIterator)); m != nil {
			s.it, s.next = cx.CreateAsyncFromSyncIterator(getIteratorFromMethod(cx, items, m))
		}
		if s.it.IsObject() {
			if custom {
				s.array = cx.Construct(this.Object(), nil, this.Object())
			} else {
				s.array = cx.ArrayCreate(0, cx.Intrinsic(storage.IntrinsicArrayPrototype))
			}
			s.step(cx)
			return
		}
		s.arrayLike = cx.ToObject(items)
		s.length = cx.LengthOfArrayLike(s.arrayLike)
		if custom {
			s.array = cx.Construct(this.Object(), []storage.Value{storage.Number(float64(s.length))}, this.Object())
		} else {
			if s.length > storage.MaxArrayIndex+1 {
				cx.ThrowRangeError("invalid array length")
			}
			s.array = cx.ArrayCreate(uint32(s.length), cx.Intrinsic(storage.IntrinsicArrayPrototype))
		}
		s.stepArrayLike(cx)
	})
	return pc.Promise()
}

// reject rejects the promise with e.
func (s *fromAsync) reject(cx *engine.ExecutionContext, e storage.Value) {
	cx.RejectCapability(s.pc, e)
}

// closeAndReject closes the iterator and then rejects the promise with e, as
// IfAbruptCloseAsyncIterator does.
func (s *fromAsync) closeAndReject(cx *engine.ExecutionContext, e storage.Value) {
	cx.AsyncIteratorClose(s.it, e, true, func(cx *engine.ExecutionContext, v storage.Value, thrown bool) {
		s.reject(cx, v)
	})
}

// step calls the next method of the iterator and awaits the result. It throws when that fails.
func (s *fromAsync) step(cx *engine.ExecutionContext) {
	if s.k >= numconv.MaxSafeInteger {
		s.closeAndReject(cx, cx.NewError(engine.ErrorKindType, "too many elements for Array.fromAsync"))
		return
	}
	r := cx.CallValue(s.next, s.it)
	cx.Await(r, s.onNext)
}

func (s *fromAsync) onNext(cx *engine.ExecutionContext, r storage.Value, rejected bool) {
	if rejected {
		s.reject(cx, r)
		return
	}
	var done bool
	var v storage.Value
	if e, ok := cx.Completion(func() {
		if !r.IsObject() {
			cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
		}
		done = storage.ToBoolean(cx.GetV(r, cx.Intern("done")))
		if done {
			setLength(cx, s.array, s.k)
			return
		}
		v = cx.GetV(r, cx.Key(storage.KeyValue))
	}); !ok {
		s.reject(cx, e)
		return
	}
	if done {
		cx.ResolveCapability(s.pc, storage.ObjectValue(s.array))
		return
	}
	if s.mapper.IsUndefined() {
		s.define(cx, v)
		return
	}
	var mapped storage.Value
	if e, ok := cx.Completion(func() {
		mapped = cx.Call(s.mapper.Object(), s.thisArg, v, storage.Number(float64(s.k)))
		cx.Await(mapped, s.onMapped)
	}); !ok {
		s.closeAndReject(cx, e)
	}
}

func (s *fromAsync) onMapped(cx *engine.ExecutionContext, v storage.Value, rejected bool) {
	if rejected {
		s.closeAndReject(cx, v)
		return
	}
	s.define(cx, v)
}

func (s *fromAsync) define(cx *engine.ExecutionContext, v storage.Value) {
	if e, ok := cx.Completion(func() {
		cx.CreateDataPropertyOrThrow(s.array, bigKey(cx, s.k), v)
	}); !ok {
		s.closeAndReject(cx, e)
		return
	}
	s.k++
	if e, ok := cx.Completion(func() {
		s.step(cx)
	}); !ok {
		s.reject(cx, e)
	}
}

// stepArrayLike reads and awaits the next element of the array-like, or resolves the promise
// when there is none. It throws when that fails.
func (s *fromAsync) stepArrayLike(cx *engine.ExecutionContext) {
	if s.k >= s.length {
		setLength(cx, s.array, s.length)
		cx.ResolveCapability(s.pc, storage.ObjectValue(s.array))
		return
	}
	v := getAt(cx, s.arrayLike, s.k)
	cx.Await(v, s.onArrayLikeValue)
}

func (s *fromAsync) onArrayLikeValue(cx *engine.ExecutionContext, v storage.Value, rejected bool) {
	cx.IfAbruptRejectPromise(s.pc, func() {
		if rejected {
			cx.Throw(v)
		}
		if s.mapper.IsUndefined() {
			s.defineArrayLike(cx, v)
			return
		}
		mapped := cx.Call(s.mapper.Object(), s.thisArg, v, storage.Number(float64(s.k)))
		cx.Await(mapped, func(cx *engine.ExecutionContext, v storage.Value, rejected bool) {
			cx.IfAbruptRejectPromise(s.pc, func() {
				if rejected {
					cx.Throw(v)
				}
				s.defineArrayLike(cx, v)
			})
		})
	})
}

func (s *fromAsync) defineArrayLike(cx *engine.ExecutionContext, v storage.Value) {
	cx.CreateDataPropertyOrThrow(s.array, bigKey(cx, s.k), v)
	s.k++
	s.stepArrayLike(cx)
}
