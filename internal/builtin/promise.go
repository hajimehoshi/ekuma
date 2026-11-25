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

// aggregateError returns the AggregateError of the collected values.
func (e *promiseElements) aggregateError(cx *engine.ExecutionContext) engine.Value {
	err := cx.NewError(engine.ErrorKindAggregate, "all promises were rejected")
	cx.Define(err.Object(), cx.Key(engine.KeyErrors), engine.ObjectValue(cx.NewArray(e.take())), engine.FlagsHidden)
	return err
}

// promiseElement is the state of the functions that settle an element of a promise combinator.
type promiseElement struct {
	alreadyCalled bool
	index         int
	elements      *promiseElements
	capability    *engine.PromiseCapability
}

// resolveWith completes the element with v, and resolves the capability with the array of the
// collected values when it was the last.
func (el *promiseElement) resolveWith(cx *engine.ExecutionContext, v engine.Value) {
	e := el.elements
	e.values[el.index] = v
	if e.complete() {
		cx.ResolveCapability(el.capability, engine.ObjectValue(cx.NewArray(e.take())))
	}
}

// promiseAllResolveElement is the code of the Promise.all resolve element functions. Their state
// is a *promiseElement.
func promiseAllResolveElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*promiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, arg(args, 0))
	return engine.Undefined
}

// promiseAllSettledResolveElement is the code of the Promise.allSettled resolve element functions.
// Their state is a *promiseElement, which the reject element function shares.
func promiseAllSettledResolveElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*promiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, fulfilledResult(cx, arg(args, 0)))
	return engine.Undefined
}

// promiseAllSettledRejectElement is the code of the Promise.allSettled reject element functions.
// Their state is a *promiseElement, which the resolve element function shares.
func promiseAllSettledRejectElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*promiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, rejectedResult(cx, arg(args, 0)))
	return engine.Undefined
}

// promiseAnyRejectElement is the code of the Promise.any reject element functions. Their state is
// a *promiseElement.
func promiseAnyRejectElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*promiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	e := el.elements
	e.values[el.index] = arg(args, 0)
	if e.complete() {
		cx.RejectCapability(el.capability, e.aggregateError(cx))
	}
	return engine.Undefined
}

// fulfilledResult returns the object that describes an element fulfilled with v.
func fulfilledResult(cx *engine.ExecutionContext, v engine.Value) engine.Value {
	return settledResult(cx, "fulfilled", cx.Key(engine.KeyValue), v)
}

// rejectedResult returns the object that describes an element rejected with v.
func rejectedResult(cx *engine.ExecutionContext, v engine.Value) engine.Value {
	return settledResult(cx, "rejected", cx.Key(engine.KeyReason), v)
}

// settledResult returns the object that describes a settled element, with v under key.
func settledResult(cx *engine.ExecutionContext, status wtf8.String, key *engine.Object, v engine.Value) engine.Value {
	return engine.ObjectValue(cx.NewObjectFromProperties(
		[]engine.PropKey{engine.NamedKey(cx.Key(engine.KeyStatus)), engine.NamedKey(key)},
		[]engine.Value{cx.NewString(status), v},
	))
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

// keyedPromiseElement is the state of the functions that settle an element of a keyed promise
// combinator.
type keyedPromiseElement struct {
	alreadyCalled bool
	index         int
	elements      *keyedPromiseElements
	capability    *engine.PromiseCapability
}

// resolveWith completes the element with v, and resolves the capability when it was the last.
func (el *keyedPromiseElement) resolveWith(cx *engine.ExecutionContext, v engine.Value) {
	el.elements.values[el.index] = v
	el.elements.resolveIfDone(cx, el.capability)
}

// promiseAllKeyedResolveElement is the code of the Promise.allKeyed resolve element functions.
// Their state is a *keyedPromiseElement.
func promiseAllKeyedResolveElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*keyedPromiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, arg(args, 0))
	return engine.Undefined
}

// promiseAllSettledKeyedResolveElement is the code of the Promise.allSettledKeyed resolve element
// functions. Their state is a *keyedPromiseElement, which the reject element function shares.
func promiseAllSettledKeyedResolveElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*keyedPromiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, fulfilledResult(cx, arg(args, 0)))
	return engine.Undefined
}

// promiseAllSettledKeyedRejectElement is the code of the Promise.allSettledKeyed reject element
// functions. Their state is a *keyedPromiseElement, which the resolve element function shares.
func promiseAllSettledKeyedRejectElement(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	el := cx.ActiveNativeState().(*keyedPromiseElement)
	if el.alreadyCalled {
		return engine.Undefined
	}
	el.alreadyCalled = true
	el.resolveWith(cx, rejectedResult(cx, arg(args, 0)))
	return engine.Undefined
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
		el := &keyedPromiseElement{
			index:      i,
			elements:   e,
			capability: pc,
		}
		var onFulfilled, onRejected engine.Value
		if settled {
			onFulfilled = engine.ObjectValue(cx.NewNativeWithState("", 1, promiseAllSettledKeyedResolveElement, el))
			onRejected = engine.ObjectValue(cx.NewNativeWithState("", 1, promiseAllSettledKeyedRejectElement, el))
		} else {
			onFulfilled = engine.ObjectValue(cx.NewNativeWithState("", 1, promiseAllKeyedResolveElement, el))
			onRejected = cx.CapabilityReject(pc)
		}
		invoke(cx, next, cx.Key(engine.KeyThen), onFulfilled, onRejected)
	}
	e.resolveIfDone(cx, pc)
}

// promiseFinally is the state of the then finally and catch finally functions.
type promiseFinally struct {
	onFinally   *engine.Object
	constructor *engine.Object
}

// promiseThenFinally is the code of the then finally functions. Their state is a *promiseFinally.
func promiseThenFinally(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := cx.ActiveNativeState().(*promiseFinally)
	value := arg(args, 0)
	result := cx.Call(f.onFinally, engine.Undefined)
	p := cx.PromiseResolve(f.constructor, result)
	valueThunk := cx.NewNativeWithState("", 0, promiseValueThunk, value)
	return invoke(cx, p, cx.Key(engine.KeyThen), engine.ObjectValue(valueThunk))
}

// promiseCatchFinally is the code of the catch finally functions. Their state is a *promiseFinally.
func promiseCatchFinally(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := cx.ActiveNativeState().(*promiseFinally)
	reason := arg(args, 0)
	result := cx.Call(f.onFinally, engine.Undefined)
	p := cx.PromiseResolve(f.constructor, result)
	thrower := cx.NewNativeWithState("", 0, promiseThrower, reason)
	return invoke(cx, p, cx.Key(engine.KeyThen), engine.ObjectValue(thrower))
}

// promiseValueThunk is the code of the functions that return the value of a then finally function.
// Their state is the value, an [engine.Value].
func promiseValueThunk(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.ActiveNativeState().(engine.Value)
}

// promiseThrower is the code of the functions that throw the reason of a catch finally function.
// Their state is the reason, an [engine.Value].
func promiseThrower(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cx.Throw(cx.ActiveNativeState().(engine.Value))
	return engine.Undefined
}

func initPromise(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicPromisePrototype)
	ctor := constructor(cx, "Promise", 1, engine.IntrinsicPromisePrototype, promiseConstructor)
	cx.SetIntrinsic(engine.IntrinsicPromise, ctor)
	speciesGetter(cx, ctor)

	method(cx, ctor, "all", 1, promiseAll)
	method(cx, ctor, "allSettled", 1, promiseAllSettled)
	method(cx, ctor, "allKeyed", 1, promiseAllKeyed)
	method(cx, ctor, "allSettledKeyed", 1, promiseAllSettledKeyed)
	method(cx, ctor, "any", 1, promiseAny)
	method(cx, ctor, "race", 1, promiseRace)
	method(cx, ctor, "reject", 1, promiseReject)
	method(cx, ctor, "resolve", 1, promiseResolve)
	method(cx, ctor, "try", 1, promiseTry)
	method(cx, ctor, "withResolvers", 0, promiseWithResolvers)

	method(cx, proto, "catch", 1, promiseCatch)
	method(cx, proto, "finally", 1, promiseFinallyMethod)
	method(cx, proto, "then", 2, promiseThen)

	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Promise"), engine.FlagConfigurable)
}

func promiseConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func promiseAll(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	e := newPromiseElements()
	return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
		el := &promiseElement{
			index:      e.reserve(),
			elements:   &e,
			capability: pc,
		}
		onFulfilled := cx.NewNativeWithState("", 1, promiseAllResolveElement, el)
		invoke(cx, next, cx.Key(engine.KeyThen), engine.ObjectValue(onFulfilled), cx.CapabilityReject(pc))
	}, func(pc *engine.PromiseCapability) {
		if e.complete() {
			cx.CallCapabilityResolve(pc, engine.ObjectValue(cx.NewArray(e.take())))
		}
	})
}

func promiseAllSettled(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	e := newPromiseElements()
	return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
		el := &promiseElement{
			index:      e.reserve(),
			elements:   &e,
			capability: pc,
		}
		onFulfilled := cx.NewNativeWithState("", 1, promiseAllSettledResolveElement, el)
		onRejected := cx.NewNativeWithState("", 1, promiseAllSettledRejectElement, el)
		invoke(cx, next, cx.Key(engine.KeyThen), engine.ObjectValue(onFulfilled), engine.ObjectValue(onRejected))
	}, func(pc *engine.PromiseCapability) {
		if e.complete() {
			cx.CallCapabilityResolve(pc, engine.ObjectValue(cx.NewArray(e.take())))
		}
	})
}

func promiseAllKeyed(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return promiseKeyedCombinator(cx, this, arg(args, 0), false, "Promise.allKeyed")
}

func promiseAllSettledKeyed(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return promiseKeyedCombinator(cx, this, arg(args, 0), true, "Promise.allSettledKeyed")
}

func promiseAny(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	e := newPromiseElements()
	return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
		el := &promiseElement{
			index:      e.reserve(),
			elements:   &e,
			capability: pc,
		}
		onRejected := cx.NewNativeWithState("", 1, promiseAnyRejectElement, el)
		invoke(cx, next, cx.Key(engine.KeyThen), cx.CapabilityResolve(pc), engine.ObjectValue(onRejected))
	}, func(pc *engine.PromiseCapability) {
		if e.complete() {
			cx.Throw(e.aggregateError(cx))
		}
	})
}

func promiseRace(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return promiseCombinator(cx, this, arg(args, 0), func(pc *engine.PromiseCapability, next engine.Value) {
		invoke(cx, next, cx.Key(engine.KeyThen), cx.CapabilityResolve(pc), cx.CapabilityReject(pc))
	}, func(pc *engine.PromiseCapability) {})
}

func promiseReject(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	pc := cx.NewPromiseCapability(this)
	cx.RejectCapability(pc, arg(args, 0))
	return pc.Promise()
}

func promiseResolve(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("Promise.resolve called on non-object")
	}
	return cx.PromiseResolve(this.Object(), arg(args, 0))
}

func promiseTry(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func promiseWithResolvers(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func promiseCatch(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return invoke(cx, this, cx.Key(engine.KeyThen), engine.Undefined, arg(args, 0))
}

func promiseFinallyMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("Promise.prototype.finally called on non-object")
	}
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicPromise))
	onFinally := arg(args, 0)
	thenFinally, catchFinally := onFinally, onFinally
	if onFinally.IsCallable() {
		f := &promiseFinally{
			onFinally:   onFinally.Object(),
			constructor: c,
		}
		thenFinally = engine.ObjectValue(cx.NewNativeWithState("", 1, promiseThenFinally, f))
		catchFinally = engine.ObjectValue(cx.NewNativeWithState("", 1, promiseCatchFinally, f))
	}
	return invoke(cx, this, cx.Key(engine.KeyThen), thenFinally, catchFinally)
}

func promiseThen(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !engine.IsPromise(this) {
		cx.ThrowTypeError("Promise.prototype.then called on incompatible receiver " + cx.DisplayString(this))
	}
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicPromise))
	pc := cx.NewPromiseCapability(engine.ObjectValue(c))
	return cx.PerformPromiseThen(this, arg(args, 0), arg(args, 1), pc)
}
