// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// invoke implements Invoke for the property key of v.
func invoke(cx *engine.ExecutionContext, v engine.Value, key *engine.Object, args ...engine.Value) engine.Value {
	return cx.CallValue(cx.GetV(v, key), v, args...)
}

// getPromiseResolve implements GetPromiseResolve.
func getPromiseResolve(cx *engine.ExecutionContext, c engine.Value) engine.Value {
	r := cx.GetV(c, cx.Intern("resolve"))
	if !r.IsCallable() {
		cx.ThrowTypeError("the resolve method of the promise constructor is not a function")
	}
	return r
}

// promiseCombinator implements the steps that Promise.all, Promise.allSettled, Promise.any
// and Promise.race share. perform is called for every value of the iterable with the result
// of the resolve method of c, as their Perform operations do, and done when the iterator is
// exhausted. The iterator is closed when the call of the resolve method or perform throws.
func promiseCombinator(cx *engine.ExecutionContext, c, iterable engine.Value, perform func(c *engine.PromiseCapability, next engine.Value), done func(c *engine.PromiseCapability)) engine.Value {
	pc := cx.NewPromiseCapability(c)
	var resolve engine.Value
	if !cx.IfAbruptRejectPromise(pc, func() {
		resolve = getPromiseResolve(cx, c)
	}) {
		return pc.Promise()
	}
	var it, next engine.Value
	if !cx.IfAbruptRejectPromise(pc, func() {
		it, next = cx.GetIterator(iterable)
	}) {
		return pc.Promise()
	}
	cx.IfAbruptRejectPromise(pc, func() {
		for {
			v, ok := cx.IteratorStepValue(it, next)
			if !ok {
				done(pc)
				return
			}
			cx.IfAbruptCloseIterator(it, func() {
				perform(pc, cx.CallValue(resolve, c, v))
			})
		}
	})
	return pc.Promise()
}

// promiseElements collects promise combinator results in input order.
type promiseElements struct {
	values    []engine.Value
	remaining int
}

func newPromiseElements() promiseElements {
	// The iteration itself counts as pending until it is exhausted.
	return promiseElements{
		remaining: 1,
	}
}

func (e *promiseElements) reserve() int {
	index := len(e.values)
	e.values = append(e.values, engine.Undefined)
	e.remaining++
	return index
}

// complete counts one pending contribution as done and reports whether it was the last.
func (e *promiseElements) complete() bool {
	e.remaining--
	return e.remaining == 0
}

// take transfers the collected values to the caller.
func (e *promiseElements) take() []engine.Value {
	vals := e.values
	// Element callbacks can stay reachable after settlement.
	e.values = nil
	return vals
}

// keyedPromiseElements pairs each reserved slot with its property key.
type keyedPromiseElements struct {
	promiseElements
	keys []engine.PropKey
}

// resolveIfDone resolves pc with the result object when no element is left unsettled.
func (e *keyedPromiseElements) resolveIfDone(cx *engine.ExecutionContext, pc *engine.PromiseCapability) {
	if e.complete() {
		keys, vals := e.keys, e.take()
		e.keys = nil
		cx.ResolveCapability(pc, engine.ObjectValue(createKeyedPromiseCombinatorResultObject(cx, keys, vals)))
	}
}

// createKeyedPromiseCombinatorResultObject implements CreateKeyedPromiseCombinatorResultObject.
func createKeyedPromiseCombinatorResultObject(cx *engine.ExecutionContext, keys []engine.PropKey, values []engine.Value) *engine.Object {
	o := cx.NewObject(nil)
	for i, k := range keys {
		cx.CreateDataPropertyOrThrow(o, k, values[i])
	}
	return o
}

// promiseKeyedCombinator implements Promise.allKeyed, or Promise.allSettledKeyed when
// settled is true.
func promiseKeyedCombinator(cx *engine.ExecutionContext, c, promises engine.Value, settled bool, name string) engine.Value {
	pc := cx.NewPromiseCapability(c)
	var resolve engine.Value
	if !cx.IfAbruptRejectPromise(pc, func() {
		resolve = getPromiseResolve(cx, c)
	}) {
		return pc.Promise()
	}
	if !promises.IsObject() {
		cx.RejectCapability(pc, cx.NewError(engine.ErrorKindType, name+" called on non-object"))
		return pc.Promise()
	}
	cx.IfAbruptRejectPromise(pc, func() {
		performPromiseAllKeyed(cx, settled, promises.Object(), c, pc, resolve)
	})
	return pc.Promise()
}

// performPromiseAllKeyed implements PerformPromiseAllKeyed. settled selects the all-settled variant.
func performPromiseAllKeyed(cx *engine.ExecutionContext, settled bool, promises *engine.Object, c engine.Value, pc *engine.PromiseCapability, resolve engine.Value) {
	e := &keyedPromiseElements{
		promiseElements: newPromiseElements(),
	}
	for _, k := range cx.OwnKeys(promises) {
		d, ok := cx.GetOwnProperty(promises, k)
		if !ok || !d.Enumerable() {
			continue
		}
		v := cx.Get(promises, k)
		i := e.reserve()
		e.keys = append(e.keys, k)
		next := cx.CallValue(resolve, c, v)
		called := false
		var onFulfilled, onRejected engine.Value
		if settled {
			element := func(status string, key engine.CommonKey) engine.Value {
				return engine.ObjectValue(cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
					if called {
						return engine.Undefined
					}
					called = true
					o := cx.NewObjectFromProperties(
						[]engine.PropKey{engine.NamedKey(cx.Key(engine.KeyStatus)), engine.NamedKey(cx.Key(key))},
						[]engine.Value{cx.NewString(wtf8.String(status)), arg(args, 0)},
					)
					e.values[i] = engine.ObjectValue(o)
					e.resolveIfDone(cx, pc)
					return engine.Undefined
				}))
			}
			onFulfilled = element("fulfilled", engine.KeyValue)
			onRejected = element("rejected", engine.KeyReason)
		} else {
			onFulfilled = engine.ObjectValue(cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				e.resolveIfDone(cx, pc)
				return engine.Undefined
			}))
			onRejected = cx.CapabilityReject(pc)
		}
		invoke(cx, next, cx.Key(engine.KeyThen), onFulfilled, onRejected)
	}
	e.resolveIfDone(cx, pc)
}

func initPromise(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicPromisePrototype)
	ctor := constructor(cx, "Promise", 1, engine.IntrinsicPromisePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("Promise constructor cannot be invoked without 'new'")
		}
		executor := arg(args, 0)
		if !executor.IsCallable() {
			cx.ThrowTypeError("Promise resolver " + cx.DisplayString(executor) + " is not a function")
		}
		p := cx.NewPromiseObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicPromisePrototype))
		rf := cx.CreateResolvingFunctions(p)
		cx.IfAbruptRejectPromise(rf, func() {
			cx.Call(executor.Object(), engine.Undefined, cx.CapabilityResolve(rf), cx.CapabilityReject(rf))
		})
		return engine.ObjectValue(p)
	})
	cx.SetIntrinsic(engine.IntrinsicPromise, ctor)
	speciesGetter(cx, ctor)

	method(cx, ctor, "all", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		e := newPromiseElements()
		return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := e.reserve()
			called := false
			onFulfilled := cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				if e.complete() {
					cx.ResolveCapability(pc, engine.ObjectValue(cx.NewArray(e.take())))
				}
				return engine.Undefined
			})
			invoke(cx, next, cx.Key(engine.KeyThen), engine.ObjectValue(onFulfilled), cx.CapabilityReject(pc))
		}, func(pc *engine.PromiseCapability) {
			if e.complete() {
				cx.CallCapabilityResolve(pc, engine.ObjectValue(cx.NewArray(e.take())))
			}
		})
	})

	method(cx, ctor, "allSettled", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		e := newPromiseElements()
		return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := e.reserve()
			called := false
			element := func(status string, key engine.CommonKey) *engine.Object {
				return cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
					if called {
						return engine.Undefined
					}
					called = true
					o := cx.NewObjectFromProperties(
						[]engine.PropKey{engine.NamedKey(cx.Key(engine.KeyStatus)), engine.NamedKey(cx.Key(key))},
						[]engine.Value{cx.NewString(wtf8.String(status)), arg(args, 0)},
					)
					e.values[i] = engine.ObjectValue(o)
					if e.complete() {
						cx.ResolveCapability(pc, engine.ObjectValue(cx.NewArray(e.take())))
					}
					return engine.Undefined
				})
			}
			onFulfilled := element("fulfilled", engine.KeyValue)
			onRejected := element("rejected", engine.KeyReason)
			invoke(cx, next, cx.Key(engine.KeyThen), engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected))
		}, func(pc *engine.PromiseCapability) {
			if e.complete() {
				cx.CallCapabilityResolve(pc, engine.ObjectValue(cx.NewArray(e.take())))
			}
		})
	})

	method(cx, ctor, "allKeyed", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return promiseKeyedCombinator(cx, this, arg(args, 0), false, "Promise.allKeyed")
	})

	method(cx, ctor, "allSettledKeyed", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return promiseKeyedCombinator(cx, this, arg(args, 0), true, "Promise.allSettledKeyed")
	})

	method(cx, ctor, "any", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		e := newPromiseElements()
		aggregate := func(cx *engine.ExecutionContext) engine.Value {
			err := cx.NewError(engine.ErrorKindAggregate, "all promises were rejected")
			cx.Define(err.Object(), cx.Key(engine.KeyErrors), engine.ObjectValue(cx.NewArray(e.take())), engine.FlagsHidden)
			return err
		}
		return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			i := e.reserve()
			called := false
			onRejected := cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				if called {
					return engine.Undefined
				}
				called = true
				e.values[i] = arg(args, 0)
				if e.complete() {
					cx.RejectCapability(pc, aggregate(cx))
				}
				return engine.Undefined
			})
			invoke(cx, next, cx.Key(engine.KeyThen), cx.CapabilityResolve(pc), engine.ObjectValue(onRejected))
		}, func(pc *engine.PromiseCapability) {
			if e.complete() {
				cx.Throw(aggregate(cx))
			}
		})
	})

	method(cx, ctor, "race", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
			invoke(cx, next, cx.Key(engine.KeyThen), cx.CapabilityResolve(pc), cx.CapabilityReject(pc))
		}, func(pc *engine.PromiseCapability) {})
	})

	method(cx, ctor, "reject", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		pc := cx.NewPromiseCapability(this)
		cx.RejectCapability(pc, arg(args, 0))
		return pc.Promise()
	})

	method(cx, ctor, "resolve", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("Promise.resolve called on non-object")
		}
		return cx.PromiseResolve(this.Object(), arg(args, 0))
	})

	method(cx, ctor, "try", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("Promise.try called on non-object")
		}
		var status engine.Value
		e, ok := cx.Completion(func() {
			status = cx.CallValue(arg(args, 0), engine.Undefined, args[min(1, len(args)):]...)
		})
		if ok {
			return cx.PromiseResolve(this.Object(), status)
		}
		pc := cx.NewPromiseCapability(this)
		cx.RejectCapability(pc, e)
		return pc.Promise()
	})

	method(cx, ctor, "withResolvers", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		pc := cx.NewPromiseCapability(this)
		o := cx.NewObjectFromProperties(
			[]engine.PropKey{
				engine.NamedKey(cx.Intern("promise")),
				engine.NamedKey(cx.Intern("resolve")),
				engine.NamedKey(cx.Intern("reject")),
			},
			[]engine.Value{pc.Promise(), cx.CapabilityResolve(pc), cx.CapabilityReject(pc)},
		)
		return engine.ObjectValue(o)
	})

	method(cx, proto, "catch", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return invoke(cx, this, cx.Key(engine.KeyThen), engine.Undefined, arg(args, 0))
	})

	method(cx, proto, "finally", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsObject() {
			cx.ThrowTypeError("Promise.prototype.finally called on non-object")
		}
		c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicPromise))
		onFinally := arg(args, 0)
		thenFinally, catchFinally := onFinally, onFinally
		if onFinally.IsCallable() {
			thenFinally = engine.ObjectValue(cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				value := arg(args, 0)
				result := cx.Call(onFinally.Object(), engine.Undefined)
				p := cx.PromiseResolve(c, result)
				valueThunk := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
					return value
				})
				return invoke(cx, p, cx.Key(engine.KeyThen), engine.ObjectValue(valueThunk))
			}))
			catchFinally = engine.ObjectValue(cx.NewNative("", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
				reason := arg(args, 0)
				result := cx.Call(onFinally.Object(), engine.Undefined)
				p := cx.PromiseResolve(c, result)
				thrower := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
					cx.Throw(reason)
					return engine.Undefined
				})
				return invoke(cx, p, cx.Key(engine.KeyThen), engine.ObjectValue(thrower))
			}))
		}
		return invoke(cx, this, cx.Key(engine.KeyThen), thenFinally, catchFinally)
	})

	method(cx, proto, "then", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !engine.IsPromise(this) {
			cx.ThrowTypeError("Promise.prototype.then called on incompatible receiver " + cx.DisplayString(this))
		}
		c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicPromise))
		pc := cx.NewPromiseCapability(engine.ObjectValue(c))
		return cx.PerformPromiseThen(this, arg(args, 0), arg(args, 1), pc)
	})

	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Promise"), engine.FlagConfigurable)
}
