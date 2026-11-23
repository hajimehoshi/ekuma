// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// functionKindConstructor creates the constructor name of the functions of kind whose prototype
// object is proto, such as GeneratorFunction. It is not a global.
func functionKindConstructor(cx *engine.ExecutionContext, name wtf8.String, kind engine.FunctionKind, proto *engine.Object) {
	var c *engine.Object
	c = cx.NewNativeConstructor(name, 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.CreateDynamicFunction(c, cx.GetNewTarget(), kind, args)
	})
	cx.SetPrototypeOf(c, cx.Intrinsic(engine.IntrinsicFunction))
	cx.Define(c, cx.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagConfigurable)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString(name), engine.FlagConfigurable)
}

func initAsync(cx *engine.ExecutionContext) {
	functionKindConstructor(cx, "AsyncFunction", engine.AsyncFunction, cx.Intrinsic(engine.IntrinsicAsyncFunctionPrototype))
	functionKindConstructor(cx, "AsyncGeneratorFunction", engine.AsyncGeneratorFunction, cx.Intrinsic(engine.IntrinsicAsyncGeneratorFunctionPrototype))

	gp := cx.Intrinsic(engine.IntrinsicAsyncGeneratorPrototype)
	resume := func(name string, mode engine.ResumeMode) {
		method(cx, gp, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return cx.ResumeAsyncGenerator(this, mode, arg(args, 0))
		})
	}
	resume("next", engine.ResumeNext)
	resume("return", engine.ResumeReturn)
	resume("throw", engine.ResumeThrow)
	cx.Define(gp, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("AsyncGenerator"), engine.FlagConfigurable)
	gf := cx.Intrinsic(engine.IntrinsicAsyncGeneratorFunctionPrototype)
	cx.Define(gf, cx.Key(engine.KeyPrototype), engine.ObjectValue(gp), engine.FlagConfigurable)
	cx.Define(gp, cx.Key(engine.KeyConstructor), engine.ObjectValue(gf), engine.FlagConfigurable)

	method(cx, cx.Intrinsic(engine.IntrinsicArray), "fromAsync", 1, arrayFromAsync)
}

// getIteratorFromMethod implements GetIteratorFromMethod.
func getIteratorFromMethod(cx *engine.ExecutionContext, v engine.Value, m *engine.Object) (it, next engine.Value) {
	it = cx.Call(m, v)
	if !it.IsObject() {
		cx.ThrowTypeError("result of the iterator method is not an object")
	}
	return it, cx.GetV(it, cx.Intern("next"))
}

// fromAsync is the state of a call of Array.fromAsync.
type fromAsync struct {
	pc      *engine.PromiseCapability
	array   *engine.Object
	k       int64
	mapper  engine.Value
	thisArg engine.Value

	// it and next are the async iterator and its next method, or undefined for an array-like.
	it, next engine.Value

	// arrayLike and length are the array-like object and its length.
	arrayLike *engine.Object
	length    int64
}

func arrayFromAsync(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pc := cx.NewPromiseCapability(engine.ObjectValue(cx.Intrinsic(engine.IntrinsicPromise)))
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
		if m := cx.GetMethod(items, cx.WellKnownSymbol(engine.SymbolAsyncIterator)); m != nil {
			s.it, s.next = getIteratorFromMethod(cx, items, m)
		} else if m := cx.GetMethod(items, cx.WellKnownSymbol(engine.SymbolIterator)); m != nil {
			s.it, s.next = cx.CreateAsyncFromSyncIterator(getIteratorFromMethod(cx, items, m))
		}
		if s.it.IsObject() {
			if custom {
				s.array = cx.Construct(this.Object(), nil, this.Object())
			} else {
				s.array = cx.ArrayCreate(0, cx.Intrinsic(engine.IntrinsicArrayPrototype))
			}
			s.step(cx)
			return
		}
		s.arrayLike = cx.ToObject(items)
		s.length = cx.LengthOfArrayLike(s.arrayLike)
		if custom {
			s.array = cx.Construct(this.Object(), []engine.Value{engine.Number(float64(s.length))}, this.Object())
		} else {
			if s.length > engine.MaxArrayIndex+1 {
				cx.ThrowRangeError("invalid array length")
			}
			s.array = cx.ArrayCreate(uint32(s.length), cx.Intrinsic(engine.IntrinsicArrayPrototype))
		}
		s.stepArrayLike(cx)
	})
	return pc.Promise()
}

// reject rejects the promise with e.
func (s *fromAsync) reject(cx *engine.ExecutionContext, e engine.Value) {
	cx.RejectCapability(s.pc, e)
}

// closeAndReject closes the iterator and then rejects the promise with e, as
// IfAbruptCloseAsyncIterator does.
func (s *fromAsync) closeAndReject(cx *engine.ExecutionContext, e engine.Value) {
	cx.AsyncIteratorClose(s.it, e, true, func(cx *engine.ExecutionContext, v engine.Value, thrown bool) {
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

func (s *fromAsync) onNext(cx *engine.ExecutionContext, r engine.Value, rejected bool) {
	if rejected {
		s.reject(cx, r)
		return
	}
	var done bool
	var v engine.Value
	if e, ok := cx.Completion(func() {
		if !r.IsObject() {
			cx.ThrowTypeError("iterator result " + cx.DisplayString(r) + " is not an object")
		}
		done = engine.ToBoolean(cx.GetV(r, cx.Intern("done")))
		if done {
			setLength(cx, s.array, s.k)
			return
		}
		v = cx.GetV(r, cx.Key(engine.KeyValue))
	}); !ok {
		s.reject(cx, e)
		return
	}
	if done {
		cx.ResolveCapability(s.pc, engine.ObjectValue(s.array))
		return
	}
	if s.mapper.IsUndefined() {
		s.define(cx, v)
		return
	}
	var mapped engine.Value
	if e, ok := cx.Completion(func() {
		mapped = cx.Call(s.mapper.Object(), s.thisArg, v, engine.Number(float64(s.k)))
		cx.Await(mapped, s.onMapped)
	}); !ok {
		s.closeAndReject(cx, e)
	}
}

func (s *fromAsync) onMapped(cx *engine.ExecutionContext, v engine.Value, rejected bool) {
	if rejected {
		s.closeAndReject(cx, v)
		return
	}
	s.define(cx, v)
}

func (s *fromAsync) define(cx *engine.ExecutionContext, v engine.Value) {
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
		cx.ResolveCapability(s.pc, engine.ObjectValue(s.array))
		return
	}
	v := getAt(cx, s.arrayLike, s.k)
	cx.Await(v, s.onArrayLikeValue)
}

func (s *fromAsync) onArrayLikeValue(cx *engine.ExecutionContext, v engine.Value, rejected bool) {
	cx.IfAbruptRejectPromise(s.pc, func() {
		if rejected {
			cx.Throw(v)
		}
		if s.mapper.IsUndefined() {
			s.defineArrayLike(cx, v)
			return
		}
		mapped := cx.Call(s.mapper.Object(), s.thisArg, v, engine.Number(float64(s.k)))
		cx.Await(mapped, func(cx *engine.ExecutionContext, v engine.Value, rejected bool) {
			cx.IfAbruptRejectPromise(s.pc, func() {
				if rejected {
					cx.Throw(v)
				}
				s.defineArrayLike(cx, v)
			})
		})
	})
}

func (s *fromAsync) defineArrayLike(cx *engine.ExecutionContext, v engine.Value) {
	cx.CreateDataPropertyOrThrow(s.array, bigKey(cx, s.k), v)
	s.k++
	s.stepArrayLike(cx)
}
