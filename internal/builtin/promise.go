// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// invoke implements Invoke for the property key of v.
func invoke(vm *engine.VM, v engine.Value, key *engine.Object, args ...engine.Value) engine.Value {
	return vm.CallValue(vm.GetV(v, key), v, args...)
}

// getPromiseResolve implements GetPromiseResolve.
func getPromiseResolve(vm *engine.VM, c engine.Value) engine.Value {
	r := vm.GetV(c, vm.Intern("resolve"))
	if !r.IsCallable() {
		vm.ThrowTypeError(vm.CurrentRealm(), "the resolve method of the promise constructor is not a function")
	}
	return r
}

// promiseCombinator implements the steps that Promise.all, Promise.allSettled, Promise.any
// and Promise.race share. perform is called for every value of the iterable with the result
// of the resolve method of c, as their Perform operations do, and done when the iterator is
// exhausted. The iterator is closed when the call of the resolve method or perform throws.
func promiseCombinator(vm *engine.VM, c, iterable engine.Value, perform func(c *engine.PromiseCapability, next engine.Value), done func(c *engine.PromiseCapability)) engine.Value {
	pc := vm.NewPromiseCapability(c)
	var resolve engine.Value
	if !vm.IfAbruptRejectPromise(pc, func() {
		resolve = getPromiseResolve(vm, c)
	}) {
		return pc.Promise()
	}
	var it, next engine.Value
	if !vm.IfAbruptRejectPromise(pc, func() {
		it, next = vm.GetIterator(iterable)
	}) {
		return pc.Promise()
	}
	vm.IfAbruptRejectPromise(pc, func() {
		for {
			v, ok := vm.IteratorStepValue(it, next)
			if !ok {
				done(pc)
				return
			}
			vm.IfAbruptCloseIterator(it, func() {
				perform(pc, vm.CallValue(resolve, c, v))
			})
		}
	})
	return pc.Promise()
}

// promiseElements are the values of Promise.all and the like with the count of
// the elements that are not settled yet.
type promiseElements struct {
	values    []engine.Value
	remaining int
}

// keyedPromiseElements are the keys and values of Promise.allKeyed and
// Promise.allSettledKeyed with the count of the elements that are not settled yet.
type keyedPromiseElements struct {
	keys      []engine.PropKey
	values    []engine.Value
	remaining int
}

// resolveIfDone resolves pc with the result object when no element is left unsettled.
func (e *keyedPromiseElements) resolveIfDone(vm *engine.VM, pc *engine.PromiseCapability) {
	e.remaining--
	if e.remaining == 0 {
		vm.ResolveCapability(pc, engine.ObjectValue(createKeyedPromiseCombinatorResultObject(vm, e.keys, e.values)))
	}
}

// createKeyedPromiseCombinatorResultObject implements CreateKeyedPromiseCombinatorResultObject.
func createKeyedPromiseCombinatorResultObject(vm *engine.VM, keys []engine.PropKey, values []engine.Value) *engine.Object {
	o := vm.NewObject(nil)
	for i, k := range keys {
		vm.CreateDataPropertyOrThrow(o, k, values[i])
	}
	return o
}

// promiseKeyedCombinator implements Promise.allKeyed, or Promise.allSettledKeyed when
// settled is true.
func promiseKeyedCombinator(vm *engine.VM, c, promises engine.Value, settled bool, name string) engine.Value {
	pc := vm.NewPromiseCapability(c)
	var resolve engine.Value
	if !vm.IfAbruptRejectPromise(pc, func() {
		resolve = getPromiseResolve(vm, c)
	}) {
		return pc.Promise()
	}
	if !promises.IsObject() {
		vm.RejectCapability(pc, vm.NewError(vm.CurrentRealm(), engine.ErrorKindType, name+" called on non-object"))
		return pc.Promise()
	}
	vm.IfAbruptRejectPromise(pc, func() {
		performPromiseAllKeyed(vm, settled, promises.Object(), c, pc, resolve)
	})
	return pc.Promise()
}

// performPromiseAllKeyed implements PerformPromiseAllKeyed. settled selects the all-settled variant.
func performPromiseAllKeyed(vm *engine.VM, settled bool, promises *engine.Object, c engine.Value, pc *engine.PromiseCapability, resolve engine.Value) {
	keyThen := vm.Intern("then")
	keyStatus := vm.Intern("status")
	keyReason := vm.Intern("reason")
	e := &keyedPromiseElements{
		remaining: 1,
	}
	for _, k := range vm.OwnKeys(promises) {
		d, ok := vm.GetOwnProperty(promises, k)
		if !ok || !d.Enumerable() {
			continue
		}
		v := vm.Get(promises, k)
		i := len(e.values)
		e.keys = append(e.keys, k)
		e.values = append(e.values, engine.Undefined)
		next := vm.CallValue(resolve, c, v)
		called := false
		var onFulfilled, onRejected engine.Value
		if settled {
			element := func(status string, key *engine.Object) engine.Value {
				return engine.ObjectValue(vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
					if called {
						return engine.Undefined
					}
					called = true
					o := vm.NewObjectFromProperties(
						[]engine.PropKey{engine.NamedKey(keyStatus), engine.NamedKey(key)},
						[]engine.Value{vm.NewString(wtf8.String(status)), arg(args, 0)},
					)
					e.values[i] = engine.ObjectValue(o)
					e.resolveIfDone(vm, pc)
					return engine.Undefined
				}))
			}
			onFulfilled = element("fulfilled", vm.Key(engine.KeyValue))
			onRejected = element("rejected", keyReason)
		} else {
			onFulfilled = engine.ObjectValue(vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				e.resolveIfDone(vm, pc)
				return engine.Undefined
			}))
			onRejected = vm.CapabilityReject(pc)
		}
		e.remaining++
		invoke(vm, next, keyThen, onFulfilled, onRejected)
	}
	e.resolveIfDone(vm, pc)
}

func initPromise(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicPromisePrototype)
	ctor := constructor(vm, "Promise", 1, engine.IntrinsicPromisePrototype, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise constructor cannot be invoked without 'new'")
		}
		executor := arg(args, 0)
		if !executor.IsCallable() {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise resolver "+vm.DisplayString(executor)+" is not a function")
		}
		p := vm.NewPromiseObject(vm.GetPrototypeFromConstructor(nt, engine.IntrinsicPromisePrototype))
		rf := vm.CreateResolvingFunctions(p)
		vm.IfAbruptRejectPromise(rf, func() {
			vm.Call(executor.Object(), engine.Undefined, vm.CapabilityResolve(rf), vm.CapabilityReject(rf))
		})
		return engine.ObjectValue(p)
	})
	vm.SetIntrinsic(engine.IntrinsicPromise, ctor)
	speciesGetter(vm, ctor)

	keyThen := vm.Intern("then")
	keyStatus := vm.Intern("status")
	keyReason := vm.Intern("reason")

	method(vm, ctor, "all", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		e := &promiseElements{
			remaining: 1,
		}
		return promiseCombinator(vm, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := len(e.values)
			e.values = append(e.values, engine.Undefined)
			called := false
			onFulfilled := vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				e.remaining--
				if e.remaining == 0 {
					vm.ResolveCapability(pc, engine.ObjectValue(vm.NewArray(slices.Clone(e.values))))
				}
				return engine.Undefined
			})
			e.remaining++
			invoke(vm, next, keyThen, engine.ObjectValue(onFulfilled), vm.CapabilityReject(pc))
		}, func(pc *engine.PromiseCapability) {
			e.remaining--
			if e.remaining == 0 {
				vm.ResolveCapability(pc, engine.ObjectValue(vm.NewArray(slices.Clone(e.values))))
			}
		})
	})

	method(vm, ctor, "allSettled", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		e := &promiseElements{
			remaining: 1,
		}
		return promiseCombinator(vm, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := len(e.values)
			e.values = append(e.values, engine.Undefined)
			called := false
			element := func(status string, key *engine.Object) *engine.Object {
				return vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
					if called {
						return engine.Undefined
					}
					called = true
					o := vm.NewObjectFromProperties(
						[]engine.PropKey{engine.NamedKey(keyStatus), engine.NamedKey(key)},
						[]engine.Value{vm.NewString(wtf8.String(status)), arg(args, 0)},
					)
					e.values[i] = engine.ObjectValue(o)
					e.remaining--
					if e.remaining == 0 {
						vm.ResolveCapability(pc, engine.ObjectValue(vm.NewArray(slices.Clone(e.values))))
					}
					return engine.Undefined
				})
			}
			onFulfilled := element("fulfilled", vm.Key(engine.KeyValue))
			onRejected := element("rejected", keyReason)
			e.remaining++
			invoke(vm, next, keyThen, engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected))
		}, func(pc *engine.PromiseCapability) {
			e.remaining--
			if e.remaining == 0 {
				vm.ResolveCapability(pc, engine.ObjectValue(vm.NewArray(slices.Clone(e.values))))
			}
		})
	})

	method(vm, ctor, "allKeyed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return promiseKeyedCombinator(vm, this, arg(args, 0), false, "Promise.allKeyed")
	})

	method(vm, ctor, "allSettledKeyed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return promiseKeyedCombinator(vm, this, arg(args, 0), true, "Promise.allSettledKeyed")
	})

	method(vm, ctor, "any", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		e := &promiseElements{
			remaining: 1,
		}
		aggregate := func() engine.Value {
			err := vm.NewError(vm.CurrentRealm(), engine.ErrorKindAggregate, "all promises were rejected")
			vm.Define(err.Object(), vm.Key(engine.KeyErrors), engine.ObjectValue(vm.NewArray(slices.Clone(e.values))), engine.FlagsHidden)
			return err
		}
		return promiseCombinator(vm, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := len(e.values)
			e.values = append(e.values, engine.Undefined)
			called := false
			onRejected := vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				e.remaining--
				if e.remaining == 0 {
					vm.RejectCapability(pc, aggregate())
				}
				return engine.Undefined
			})
			e.remaining++
			invoke(vm, next, keyThen, vm.CapabilityResolve(pc), engine.ObjectValue(onRejected))
		}, func(pc *engine.PromiseCapability) {
			e.remaining--
			if e.remaining == 0 {
				vm.Throw(aggregate())
			}
		})
	})

	method(vm, ctor, "race", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return promiseCombinator(vm, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			invoke(vm, next, keyThen, vm.CapabilityResolve(pc), vm.CapabilityReject(pc))
		}, func(pc *engine.PromiseCapability) {})
	})

	method(vm, ctor, "reject", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pc := vm.NewPromiseCapability(this)
		vm.RejectCapability(pc, arg(args, 0))
		return pc.Promise()
	})

	method(vm, ctor, "resolve", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise.resolve called on non-object")
		}
		return vm.PromiseResolve(this.Object(), arg(args, 0))
	})

	method(vm, ctor, "try", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise.try called on non-object")
		}
		var status engine.Value
		e, ok := vm.Completion(func() {
			status = vm.CallValue(arg(args, 0), engine.Undefined, args[min(1, len(args)):]...)
		})
		if ok {
			return vm.PromiseResolve(this.Object(), status)
		}
		pc := vm.NewPromiseCapability(this)
		vm.RejectCapability(pc, e)
		return pc.Promise()
	})

	method(vm, ctor, "withResolvers", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		pc := vm.NewPromiseCapability(this)
		o := vm.NewObjectFromProperties(
			[]engine.PropKey{
				engine.NamedKey(vm.Intern("promise")),
				engine.NamedKey(vm.Intern("resolve")),
				engine.NamedKey(vm.Intern("reject")),
			},
			[]engine.Value{pc.Promise(), vm.CapabilityResolve(pc), vm.CapabilityReject(pc)},
		)
		return engine.ObjectValue(o)
	})

	method(vm, proto, "catch", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return invoke(vm, this, keyThen, engine.Undefined, arg(args, 0))
	})

	method(vm, proto, "finally", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise.prototype.finally called on non-object")
		}
		c := speciesConstructor(vm, this.Object(), vm.Intrinsic(engine.IntrinsicPromise))
		onFinally := arg(args, 0)
		thenFinally, catchFinally := onFinally, onFinally
		if onFinally.IsCallable() {
			thenFinally = engine.ObjectValue(vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				value := arg(args, 0)
				result := vm.Call(onFinally.Object(), engine.Undefined)
				p := vm.PromiseResolve(c, result)
				valueThunk := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
					return value
				})
				return invoke(vm, p, keyThen, engine.ObjectValue(valueThunk))
			}))
			catchFinally = engine.ObjectValue(vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				reason := arg(args, 0)
				result := vm.Call(onFinally.Object(), engine.Undefined)
				p := vm.PromiseResolve(c, result)
				thrower := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
					vm.Throw(reason)
					return engine.Undefined
				})
				return invoke(vm, p, keyThen, engine.ObjectValue(thrower))
			}))
		}
		return invoke(vm, this, keyThen, thenFinally, catchFinally)
	})

	method(vm, proto, "then", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !engine.IsPromise(this) {
			vm.ThrowTypeError(vm.CurrentRealm(), "Promise.prototype.then called on incompatible receiver "+vm.DisplayString(this))
		}
		c := speciesConstructor(vm, this.Object(), vm.Intrinsic(engine.IntrinsicPromise))
		pc := vm.NewPromiseCapability(engine.ObjectValue(c))
		return vm.PerformPromiseThen(this, arg(args, 0), arg(args, 1), pc)
	})

	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Promise"), engine.FlagConfigurable)
}
