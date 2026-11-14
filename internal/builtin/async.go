// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// functionKindConstructor creates the constructor name of the functions of kind whose prototype
// object is proto, such as GeneratorFunction. It is not a global.
func functionKindConstructor(vm *engine.VM, name string, kind engine.FunctionKind, proto *engine.Object) {
	var c *engine.Object
	c = vm.NewNativeConstructor(name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.CreateDynamicFunction(c, vm.GetNewTarget(), kind, args)
	})
	vm.SetPrototypeOf(c, vm.Intrinsic(engine.IntrinsicFunction))
	vm.Define(c, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagConfigurable)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString(name), engine.FlagConfigurable)
}

func initAsync(vm *engine.VM) {
	functionKindConstructor(vm, "AsyncFunction", engine.AsyncFunction, vm.Intrinsic(engine.IntrinsicAsyncFunctionPrototype))
	functionKindConstructor(vm, "AsyncGeneratorFunction", engine.AsyncGeneratorFunction, vm.Intrinsic(engine.IntrinsicAsyncGeneratorFunctionPrototype))

	gp := vm.Intrinsic(engine.IntrinsicAsyncGeneratorPrototype)
	resume := func(name string, mode engine.ResumeMode) {
		method(vm, gp, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return vm.ResumeAsyncGenerator(this, mode, arg(args, 0))
		})
	}
	resume("next", engine.ResumeNext)
	resume("return", engine.ResumeReturn)
	resume("throw", engine.ResumeThrow)
	vm.Define(gp, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("AsyncGenerator"), engine.FlagConfigurable)
	gf := vm.Intrinsic(engine.IntrinsicAsyncGeneratorFunctionPrototype)
	vm.Define(gf, vm.Key(engine.KeyPrototype), engine.ObjectValue(gp), engine.FlagConfigurable)
	vm.Define(gp, vm.Key(engine.KeyConstructor), engine.ObjectValue(gf), engine.FlagConfigurable)

	method(vm, vm.Intrinsic(engine.IntrinsicArray), "fromAsync", 1, arrayFromAsync)
}

// getIteratorFromMethod implements GetIteratorFromMethod.
func getIteratorFromMethod(vm *engine.VM, v engine.Value, m *engine.Object) (it, next engine.Value) {
	it = vm.Call(m, v)
	if !it.IsObject() {
		vm.ThrowTypeError("result of the iterator method is not an object")
	}
	return it, vm.GetV(it, vm.Intern("next"))
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

func arrayFromAsync(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	pc := vm.NewPromiseCapability(engine.ObjectValue(vm.Intrinsic(engine.IntrinsicPromise)))
	s := &fromAsync{
		pc:      pc,
		mapper:  arg(args, 1),
		thisArg: arg(args, 2),
	}
	items := arg(args, 0)
	custom := this.IsCallable() && vm.IsConstructor(this.Object())
	vm.IfAbruptRejectPromise(pc, func() {
		if !s.mapper.IsUndefined() && !s.mapper.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(s.mapper) + " is not a function")
		}
		if m := vm.GetMethod(items, vm.WellKnownSymbol(engine.SymbolAsyncIterator)); m != nil {
			s.it, s.next = getIteratorFromMethod(vm, items, m)
		} else if m := vm.GetMethod(items, vm.WellKnownSymbol(engine.SymbolIterator)); m != nil {
			s.it, s.next = vm.CreateAsyncFromSyncIterator(getIteratorFromMethod(vm, items, m))
		}
		if s.it.IsObject() {
			if custom {
				s.array = vm.Construct(this.Object(), nil, this.Object())
			} else {
				s.array = vm.ArrayCreate(0, vm.Intrinsic(engine.IntrinsicArrayPrototype))
			}
			s.step(vm)
			return
		}
		s.arrayLike = vm.ToObject(items)
		s.length = vm.LengthOfArrayLike(s.arrayLike)
		if custom {
			s.array = vm.Construct(this.Object(), []engine.Value{engine.Number(float64(s.length))}, this.Object())
		} else {
			if s.length > engine.MaxArrayIndex+1 {
				vm.ThrowRangeError("invalid array length")
			}
			s.array = vm.ArrayCreate(uint32(s.length), vm.Intrinsic(engine.IntrinsicArrayPrototype))
		}
		s.stepArrayLike(vm)
	})
	return pc.Promise()
}

// reject rejects the promise with e.
func (s *fromAsync) reject(vm *engine.VM, e engine.Value) {
	vm.RejectCapability(s.pc, e)
}

// closeAndReject closes the iterator and then rejects the promise with e, as
// IfAbruptCloseAsyncIterator does.
func (s *fromAsync) closeAndReject(vm *engine.VM, e engine.Value) {
	vm.AsyncIteratorClose(s.it, e, true, func(vm *engine.VM, v engine.Value, thrown bool) {
		s.reject(vm, v)
	})
}

// step calls the next method of the iterator and awaits the result. It throws when that fails.
func (s *fromAsync) step(vm *engine.VM) {
	if s.k >= numconv.MaxSafeInteger {
		s.closeAndReject(vm, vm.NewError(engine.ErrorKindType, "too many elements for Array.fromAsync"))
		return
	}
	r := vm.CallValue(s.next, s.it)
	vm.Await(r, s.onNext)
}

func (s *fromAsync) onNext(vm *engine.VM, r engine.Value, rejected bool) {
	if rejected {
		s.reject(vm, r)
		return
	}
	var done bool
	var v engine.Value
	if e, ok := vm.Completion(func() {
		if !r.IsObject() {
			vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
		}
		done = engine.ToBoolean(vm.GetV(r, vm.Intern("done")))
		if done {
			setLength(vm, s.array, s.k)
			return
		}
		v = vm.GetV(r, vm.Key(engine.KeyValue))
	}); !ok {
		s.reject(vm, e)
		return
	}
	if done {
		vm.ResolveCapability(s.pc, engine.ObjectValue(s.array))
		return
	}
	if s.mapper.IsUndefined() {
		s.define(vm, v)
		return
	}
	var mapped engine.Value
	if e, ok := vm.Completion(func() {
		mapped = vm.Call(s.mapper.Object(), s.thisArg, v, engine.Number(float64(s.k)))
		vm.Await(mapped, s.onMapped)
	}); !ok {
		s.closeAndReject(vm, e)
	}
}

func (s *fromAsync) onMapped(vm *engine.VM, v engine.Value, rejected bool) {
	if rejected {
		s.closeAndReject(vm, v)
		return
	}
	s.define(vm, v)
}

func (s *fromAsync) define(vm *engine.VM, v engine.Value) {
	if e, ok := vm.Completion(func() {
		vm.CreateDataPropertyOrThrow(s.array, bigKey(vm, s.k), v)
	}); !ok {
		s.closeAndReject(vm, e)
		return
	}
	s.k++
	if e, ok := vm.Completion(func() {
		s.step(vm)
	}); !ok {
		s.reject(vm, e)
	}
}

// stepArrayLike reads and awaits the next element of the array-like, or resolves the promise
// when there is none. It throws when that fails.
func (s *fromAsync) stepArrayLike(vm *engine.VM) {
	if s.k >= s.length {
		setLength(vm, s.array, s.length)
		vm.ResolveCapability(s.pc, engine.ObjectValue(s.array))
		return
	}
	v := getAt(vm, s.arrayLike, s.k)
	vm.Await(v, s.onArrayLikeValue)
}

func (s *fromAsync) onArrayLikeValue(vm *engine.VM, v engine.Value, rejected bool) {
	vm.IfAbruptRejectPromise(s.pc, func() {
		if rejected {
			vm.Throw(v)
		}
		if s.mapper.IsUndefined() {
			s.defineArrayLike(vm, v)
			return
		}
		mapped := vm.Call(s.mapper.Object(), s.thisArg, v, engine.Number(float64(s.k)))
		vm.Await(mapped, func(vm *engine.VM, v engine.Value, rejected bool) {
			vm.IfAbruptRejectPromise(s.pc, func() {
				if rejected {
					vm.Throw(v)
				}
				s.defineArrayLike(vm, v)
			})
		})
	})
}

func (s *fromAsync) defineArrayLike(vm *engine.VM, v engine.Value) {
	vm.CreateDataPropertyOrThrow(s.array, bigKey(vm, s.k), v)
	s.k++
	s.stepArrayLike(vm)
}
