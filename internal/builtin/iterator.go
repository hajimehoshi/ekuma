// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// iterRecord is an Iterator Record without [[Done]].
type iterRecord struct {
	it   engine.Value
	next engine.Value
}

// stepValue implements IteratorStepValue.
func (r *iterRecord) stepValue(cx engine.ExecutionContext) (engine.Value, bool) {
	return cx.IteratorStepValue(r.it, r.next)
}

// iteratorKeys holds the property keys that the iterator helpers use.
type iteratorKeys struct {
	next *engine.Object
	ret  *engine.Object
}

// getIteratorDirect implements GetIteratorDirect.
func (k *iteratorKeys) getIteratorDirect(vm *engine.VM, it engine.Value) iterRecord {
	return iterRecord{
		it:   it,
		next: vm.GetV(it, k.next),
	}
}

// helperBody is the algorithm of an iterator helper object.
type helperBody interface {
	// step returns the next value of the helper, or reports false when the helper is done.
	step(cx engine.ExecutionContext) (engine.Value, bool)

	// close closes the underlying iterators when return is called before the helper is done.
	close(cx engine.ExecutionContext)
}

type helperState uint8

const (
	helperSuspendedStart helperState = iota
	helperSuspendedYield
	helperExecuting
	helperCompleted
)

// iteratorHelper is the state of an iterator helper object, which behaves like a generator.
type iteratorHelper struct {
	state helperState
	body  helperBody
}

// resumeHelper runs a next or return resumption and reports whether the helper yielded.
func resumeHelper(cx engine.ExecutionContext, this engine.Value, mode engine.ResumeMode) (engine.Value, bool) {
	state, bodyContext := engine.IteratorStateAndContext(this)
	h, _ := state.(*iteratorHelper)
	if h == nil {
		method := "next"
		if mode == engine.ResumeReturn {
			method = "return"
		}
		cx.ThrowTypeError("Iterator Helper.prototype." + method + " requires that 'this' be an Iterator Helper")
	}
	switch h.state {
	case helperExecuting:
		cx.ThrowTypeError("generator is already running")
	case helperCompleted:
		return engine.Undefined, false
	case helperSuspendedStart:
		if mode == engine.ResumeReturn {
			// Before the first yield, the return method closes the underlying iterators itself.
			h.state = helperCompleted
			h.body.close(cx)
			return engine.Undefined, false
		}
	}

	if cx == bodyContext {
		return h.resumeBody(cx, mode)
	}
	var v engine.Value
	var yielded bool
	bodyContext.Run(func() {
		v, yielded = h.resumeBody(bodyContext, mode)
	})
	return v, yielded
}

// resumeBody runs in the helper's creation realm.
func (h *iteratorHelper) resumeBody(cx engine.ExecutionContext, mode engine.ResumeMode) (v engine.Value, yielded bool) {
	h.state = helperExecuting
	defer func() {
		if yielded {
			h.state = helperSuspendedYield
		} else {
			h.state = helperCompleted
		}
	}()
	if mode == engine.ResumeReturn {
		h.body.close(cx)
		return engine.Undefined, false
	}
	return h.body.step(cx)
}

// initIteratorHelpers defines Iterator and the iterator helpers.
func initIteratorHelpers(vm *engine.VM) {
	k := &iteratorKeys{
		next: vm.Intern("next"),
		ret:  vm.Intern("return"),
	}
	proto := vm.CurrentRealm().Intrinsic(engine.IntrinsicIteratorPrototype)
	var ctor *engine.Object
	ctor = vm.NewNativeConstructor("Iterator", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil || nt == ctor {
			vm.ThrowTypeError(vm.CurrentRealm(), "Iterator cannot be constructed directly")
		}
		return engine.ObjectValue(vm.NewInstance(vm.GetPrototypeFromConstructor(nt, engine.IntrinsicIteratorPrototype)))
	})
	vm.DefineConstructorPrototype(ctor, engine.IntrinsicIteratorPrototype)
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Iterator"), engine.ObjectValue(ctor), engine.FlagsHidden)

	// The properties that Iterator.prototype had in earlier editions are accessors whose setters
	// ignore the prototype.
	ignoringAccessor := func(key *engine.Object, name string, v engine.Value) {
		g := vm.NewNative(wtf8.String("get "+name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return v
		})
		s := vm.NewNative(wtf8.String("set "+name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			setterThatIgnoresPrototypeProperties(vm, this, proto, engine.NamedKey(key), arg(args, 0), "Iterator.prototype setter")
			return engine.Undefined
		})
		vm.DefineAccessor(proto, key, g, s, engine.FlagConfigurable)
	}
	ignoringAccessor(vm.Key(engine.KeyConstructor), "constructor", engine.ObjectValue(ctor))
	ignoringAccessor(vm.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", vm.SharedString("Iterator"))

	symMethod(vm, proto, vm.WellKnownSymbol(engine.SymbolDispose), "[Symbol.dispose]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if m := vm.GetMethod(this, k.ret); m != nil {
			vm.Call(m, this)
		}
		return engine.Undefined
	})

	helperProto := vm.NewObject(proto)
	iteratorNext(vm, helperProto, engine.IteratorClosureRealm, func(vm *engine.VM, this engine.Value) (engine.Value, bool) {
		return resumeHelper(vm.NativeContext(), this, engine.ResumeNext)
	})
	method(vm, helperProto, "return", 0, engine.WithExecutionContext(func(cx engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		resumeHelper(cx, this, engine.ResumeReturn)
		return cx.CreateIterResultObject(engine.Undefined, true)
	}))
	vm.Define(helperProto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Iterator Helper"), engine.FlagConfigurable)
	newHelper := func(body helperBody) engine.Value {
		return engine.ObjectValue(vm.NewIteratorObject(helperProto, &iteratorHelper{body: body}))
	}

	initIteratorFrom(vm, k, ctor)
	initIteratorConcat(vm, ctor, newHelper)
	initIteratorZip(vm, ctor, newHelper)
	initLazyIteratorMethods(vm, k, newHelper)
	initEagerIteratorMethods(vm, k)
}

// wrappedIterator is the state of an object that Iterator.from wraps an iterator in.
type wrappedIterator struct {
	iterRecord
}

func initIteratorFrom(vm *engine.VM, k *iteratorKeys, ctor *engine.Object) {
	wrapProto := vm.NewObject(vm.CurrentRealm().Intrinsic(engine.IntrinsicIteratorPrototype))
	thisWrapped := func(vm *engine.VM, this engine.Value, what string) *wrappedIterator {
		w, _ := engine.IteratorState(this).(*wrappedIterator)
		if w == nil {
			vm.ThrowTypeError(vm.CurrentRealm(), "%WrapForValidIteratorPrototype%."+what+" called on an incompatible receiver")
		}
		return w
	}
	method(vm, wrapProto, "next", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		w := thisWrapped(vm, this, "next")
		return vm.CallValue(w.next, w.it)
	})
	method(vm, wrapProto, "return", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		w := thisWrapped(vm, this, "return")
		m := vm.GetMethod(w.it, k.ret)
		if m == nil {
			return vm.CurrentRealm().CreateIterResultObject(engine.Undefined, true)
		}
		return vm.Call(m, w.it)
	})
	method(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var r iterRecord
		r.it, r.next = vm.NativeContext().GetIteratorFlattenable(arg(args, 0), true)
		if vm.OrdinaryHasInstance(engine.ObjectValue(ctor), r.it) {
			return r.it
		}
		return engine.ObjectValue(vm.NewIteratorObject(wrapProto, &wrappedIterator{iterRecord: r}))
	})
}

// concatHelper is the helper that Iterator.concat returns.
type concatHelper struct {
	iterables []engine.Value
	methods   []*engine.Object
	pos       int
	inner     iterRecord
	active    bool
}

func (h *concatHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	for {
		if h.active {
			if v, ok := h.inner.stepValue(cx); ok {
				return v, true
			}
			h.active = false
		}
		if h.pos >= len(h.iterables) {
			return engine.Undefined, false
		}
		iterable, method := h.iterables[h.pos], h.methods[h.pos]
		h.iterables[h.pos] = engine.Undefined
		h.pos++
		h.inner.it, h.inner.next = cx.GetIteratorFromMethod(iterable, method)
		h.active = true
	}
}

func (h *concatHelper) close(cx engine.ExecutionContext) {
	if h.active {
		cx.IteratorClose(h.inner.it)
	}
}

func initIteratorConcat(vm *engine.VM, ctor *engine.Object, newHelper func(helperBody) engine.Value) {
	method(vm, ctor, "concat", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		h := &concatHelper{
			iterables: make([]engine.Value, len(args)),
			methods:   make([]*engine.Object, len(args)),
		}
		for i, item := range args {
			if !item.IsObject() {
				vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(item)+" is not an object")
			}
			m := vm.GetMethod(item, vm.WellKnownSymbol(engine.SymbolIterator))
			if m == nil {
				vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(item)+" is not iterable")
			}
			h.iterables[i] = item
			h.methods[i] = m
		}
		return newHelper(h)
	})
}

type zipMode uint8

const (
	zipShortest zipMode = iota
	zipLongest
	zipStrict
)

// zipHelper is the helper that Iterator.zip and Iterator.zipKeyed return. It implements IteratorZip.
type zipHelper struct {
	// open and next hold the iterators and their next methods in input order.
	// An open entry is undefined while stepping that iterator and after it finishes.
	open    []engine.Value
	next    []engine.Value
	mode    zipMode
	padding []engine.Value

	// keys are the property keys of the results of Iterator.zipKeyed. The results of Iterator.zip
	// are arrays when keys is nil.
	keys []engine.PropKey
}

func (h *zipHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	var results []engine.Value
	cx.IfAbruptCloseIterators(h.open, func() {
		results = h.nextRow(cx)
	})
	if results == nil {
		h.close(cx)
		return engine.Undefined, false
	}
	if h.keys == nil {
		return cx.NewArrayOf(results), true
	}
	o := cx.NewObject(nil)
	for i, key := range h.keys {
		cx.CreateDataPropertyOrThrow(o, key, results[i])
	}
	return engine.ObjectValue(o), true
}

// nextRow returns the next row, or nil when iteration is done.
func (h *zipHelper) nextRow(cx engine.ExecutionContext) []engine.Value {
	results := make([]engine.Value, len(h.open))
	var yielded bool
	for i, it := range h.open {
		if it.IsUndefined() {
			results[i] = h.padding[i]
			continue
		}
		h.open[i] = engine.Undefined
		v, ok := cx.IteratorStepValue(it, h.next[i])
		if ok {
			h.open[i] = it
			results[i] = v
			yielded = true
			continue
		}
		switch h.mode {
		case zipShortest:
			return nil
		case zipStrict:
			if i == 0 && h.remainingDone(cx) {
				return nil
			}
			cx.ThrowTypeError("Iterator.zip: an iterator finished before the others in strict mode")
		}
		results[i] = h.padding[i]
	}
	if !yielded {
		return nil
	}
	return results
}

// remainingDone checks the remaining iterators when the first finishes in strict mode.
func (h *zipHelper) remainingDone(cx engine.ExecutionContext) bool {
	for i := 1; i < len(h.open); i++ {
		it := h.open[i]
		h.open[i] = engine.Undefined
		if cx.IteratorStep(it, h.next[i]) {
			h.open[i] = it
			return false
		}
	}
	return true
}

func (h *zipHelper) close(cx engine.ExecutionContext) {
	cx.IteratorCloseAll(h.open)
}

func initIteratorZip(vm *engine.VM, ctor *engine.Object, newHelper func(helperBody) engine.Value) {
	modeKey := vm.Intern("mode")
	paddingKey := vm.Intern("padding")
	// options reads the options of Iterator.zip and Iterator.zipKeyed and returns the mode and
	// the padding option.
	options := func(vm *engine.VM, iterables, opts engine.Value) (zipMode, engine.Value) {
		if !iterables.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(iterables)+" is not an object")
		}
		// GetOptionsObject.
		if opts.IsUndefined() {
			return zipShortest, engine.Undefined
		}
		if !opts.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "the options must be an object")
		}
		mode := zipShortest
		switch m := vm.GetV(opts, modeKey); {
		case m.IsUndefined():
		case m.IsString() && m.Str() == "shortest":
		case m.IsString() && m.Str() == "longest":
			mode = zipLongest
		case m.IsString() && m.Str() == "strict":
			mode = zipStrict
		default:
			vm.ThrowTypeError(vm.CurrentRealm(), "the mode must be \"shortest\", \"longest\" or \"strict\"")
		}
		pad := engine.Undefined
		if mode == zipLongest {
			pad = vm.GetV(opts, paddingKey)
			if !pad.IsUndefined() && !pad.IsObject() {
				vm.ThrowTypeError(vm.CurrentRealm(), "the padding must be an object")
			}
		}
		return mode, pad
	}
	newZip := func(open, next []engine.Value, mode zipMode) *zipHelper {
		h := &zipHelper{
			open: open,
			next: next,
			mode: mode,
		}
		if mode == zipLongest {
			h.padding = make([]engine.Value, len(open))
		}
		return h
	}

	method(vm, ctor, "zip", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cx := vm.NativeContext()
		iterables := arg(args, 0)
		mode, pad := options(vm, iterables, arg(args, 1))
		input, inputNext := vm.GetIterator(iterables)
		open := []engine.Value{input}
		var next []engine.Value
		for {
			var v engine.Value
			var ok bool
			vm.IfAbruptCloseIterators(open[1:], func() {
				v, ok = vm.IteratorStepValue(input, inputNext)
			})
			if !ok {
				break
			}
			var r iterRecord
			vm.IfAbruptCloseIterators(open, func() {
				r.it, r.next = cx.GetIteratorFlattenable(v, false)
			})
			open = append(open, r.it)
			next = append(next, r.next)
		}
		open[0] = engine.Undefined
		h := newZip(open[1:], next, mode)
		if mode == zipLongest && !pad.IsUndefined() {
			var padIt, padNext engine.Value
			vm.IfAbruptCloseIterators(h.open, func() {
				padIt, padNext = vm.GetIterator(pad)
			})
			using := true
			for i := range h.padding {
				var v engine.Value
				vm.IfAbruptCloseIterators(h.open, func() {
					v, using = vm.IteratorStepValue(padIt, padNext)
				})
				if !using {
					break
				}
				h.padding[i] = v
			}
			if using {
				vm.IfAbruptCloseIterators(h.open, func() {
					vm.IteratorClose(padIt)
				})
			}
		}
		return newHelper(h)
	})

	method(vm, ctor, "zipKeyed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cx := vm.NativeContext()
		iterables := arg(args, 0)
		mode, pad := options(vm, iterables, arg(args, 1))
		o := iterables.Object()
		var open, next []engine.Value
		var keys []engine.PropKey
		for _, key := range vm.OwnKeys(o) {
			var r iterRecord
			var add bool
			vm.IfAbruptCloseIterators(open, func() {
				d, ok := vm.GetOwnProperty(o, key)
				if !ok || !d.Enumerable() {
					return
				}
				v := vm.Get(o, key)
				if v.IsUndefined() {
					return
				}
				r.it, r.next = cx.GetIteratorFlattenable(v, false)
				add = true
			})
			if add {
				keys = append(keys, key)
				open = append(open, r.it)
				next = append(next, r.next)
			}
		}
		h := newZip(open, next, mode)
		h.keys = keys
		if keys == nil {
			h.keys = []engine.PropKey{}
		}
		if mode == zipLongest && !pad.IsUndefined() {
			p := pad.Object()
			for i, key := range keys {
				vm.IfAbruptCloseIterators(h.open, func() {
					h.padding[i] = vm.Get(p, key)
				})
			}
		}
		return newHelper(h)
	})
}

// receiver returns this as the iterator of an Iterator.prototype method.
func receiver(vm *engine.VM, this engine.Value, name string) engine.Value {
	if !this.IsObject() {
		vm.ThrowTypeError(vm.CurrentRealm(), "Iterator.prototype."+name+" called on non-object")
	}
	return this
}

// closeAndThrowTypeError closes the iterator it and throws a TypeError, as the methods
// of Iterator.prototype do for an invalid argument.
func closeAndThrowTypeError(vm *engine.VM, it engine.Value, msg string) {
	vm.IfAbruptCloseIterator(it, func() {
		vm.ThrowTypeError(vm.CurrentRealm(), msg)
	})
}

// closeAndThrowRangeError is closeAndThrowTypeError for a RangeError.
func closeAndThrowRangeError(vm *engine.VM, it engine.Value, msg string) {
	vm.IfAbruptCloseIterator(it, func() {
		vm.ThrowRangeError(vm.CurrentRealm(), msg)
	})
}

// callableArg returns the function argument of an Iterator.prototype method, closing it and throwing
// a TypeError when f is not callable.
func callableArg(vm *engine.VM, it, f engine.Value) *engine.Object {
	if !f.IsCallable() {
		closeAndThrowTypeError(vm, it, vm.DisplayString(f)+" is not a function")
	}
	return f.Object()
}

// limitArg converts the limit argument of take and drop, closing it when the argument is invalid.
func limitArg(vm *engine.VM, it, limit engine.Value) float64 {
	var n float64
	vm.IfAbruptCloseIterator(it, func() {
		n = vm.ToNumber(limit)
	})
	if n != n {
		closeAndThrowRangeError(vm, it, vm.DisplayString(limit)+" must be a number")
	}
	n = numconv.ToIntegerOrInfinity(n)
	if n < 0 || (n > numconv.MaxSafeInteger && !math.IsInf(n, 1)) {
		closeAndThrowRangeError(vm, it, vm.DisplayString(limit)+" must be between 0 and 2^53 - 1")
	}
	return n
}

// sizeArg checks the size argument of chunks and windows, closing it when the argument is invalid.
func sizeArg(vm *engine.VM, it, size engine.Value) int {
	if !size.IsNumber() || size.Number() != math.Trunc(size.Number()) || math.IsInf(size.Number(), 0) {
		closeAndThrowTypeError(vm, it, vm.DisplayString(size)+" must be an integer")
	}
	n := size.Number()
	if n < 1 || n > math.MaxUint32 {
		closeAndThrowRangeError(vm, it, vm.DisplayString(size)+" must be between 1 and 2^32 - 1")
	}
	return int(n)
}

type mapHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
}

func (h *mapHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	v, ok := h.src.stepValue(cx)
	if !ok {
		return engine.Undefined, false
	}
	var r engine.Value
	cx.IfAbruptCloseIterator(h.src.it, func() {
		r = cx.Call(h.f, engine.Undefined, v, engine.Number(h.counter))
	})
	h.counter++
	return r, true
}

func (h *mapHelper) close(cx engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type filterHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
}

func (h *filterHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	for {
		v, ok := h.src.stepValue(cx)
		if !ok {
			return engine.Undefined, false
		}
		var selected bool
		cx.IfAbruptCloseIterator(h.src.it, func() {
			selected = engine.ToBoolean(cx.Call(h.f, engine.Undefined, v, engine.Number(h.counter)))
		})
		h.counter++
		if selected {
			return v, true
		}
	}
}

func (h *filterHelper) close(cx engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type takeHelper struct {
	src       iterRecord
	remaining float64
}

func (h *takeHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	if h.remaining == 0 {
		cx.IteratorClose(h.src.it)
		return engine.Undefined, false
	}
	if !math.IsInf(h.remaining, 1) {
		h.remaining--
	}
	return h.src.stepValue(cx)
}

func (h *takeHelper) close(cx engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type dropHelper struct {
	src       iterRecord
	remaining float64
}

func (h *dropHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	for h.remaining > 0 {
		if !math.IsInf(h.remaining, 1) {
			h.remaining--
		}
		if !cx.IteratorStep(h.src.it, h.src.next) {
			return engine.Undefined, false
		}
	}
	return h.src.stepValue(cx)
}

func (h *dropHelper) close(cx engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type flatMapHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
	inner   iterRecord
	active  bool
}

func (h *flatMapHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	for {
		if h.active {
			var v engine.Value
			var ok bool
			cx.IfAbruptCloseIterator(h.src.it, func() {
				v, ok = h.inner.stepValue(cx)
			})
			if ok {
				return v, true
			}
			h.active = false
		}
		v, ok := h.src.stepValue(cx)
		if !ok {
			return engine.Undefined, false
		}
		cx.IfAbruptCloseIterator(h.src.it, func() {
			mapped := cx.Call(h.f, engine.Undefined, v, engine.Number(h.counter))
			h.inner.it, h.inner.next = cx.GetIteratorFlattenable(mapped, false)
		})
		h.counter++
		h.active = true
	}
}

func (h *flatMapHelper) close(cx engine.ExecutionContext) {
	if h.active {
		cx.IfAbruptCloseIterator(h.src.it, func() {
			cx.IteratorClose(h.inner.it)
		})
	}
	cx.IteratorClose(h.src.it)
}

// chunksHelper is the helper that chunks and windows return.
type chunksHelper struct {
	src     iterRecord
	size    int
	windows bool
	// allowPartial is set for windows when undersized is "allow-partial".
	allowPartial bool
	exhausted    bool
	buf          []engine.Value
}

func (h *chunksHelper) step(cx engine.ExecutionContext) (engine.Value, bool) {
	if h.exhausted {
		return engine.Undefined, false
	}
	for {
		v, ok := h.src.stepValue(cx)
		if !ok {
			h.exhausted = true
			buf := h.buf
			h.buf = nil
			if len(buf) == 0 || (h.windows && (!h.allowPartial || len(buf) == h.size)) {
				return engine.Undefined, false
			}
			return cx.NewArrayOf(buf), true
		}
		if !h.windows {
			h.buf = append(h.buf, v)
			if len(h.buf) == h.size {
				buf := h.buf
				h.buf = nil
				return cx.NewArrayOf(buf), true
			}
			continue
		}
		if len(h.buf) == h.size {
			copy(h.buf, h.buf[1:])
			h.buf[len(h.buf)-1] = v
		} else {
			h.buf = append(h.buf, v)
		}
		if len(h.buf) == h.size {
			return cx.NewArrayOf(append([]engine.Value(nil), h.buf...)), true
		}
	}
}

func (h *chunksHelper) close(cx engine.ExecutionContext) {
	if !h.exhausted {
		cx.IteratorClose(h.src.it)
	}
}

// initLazyIteratorMethods defines the methods of Iterator.prototype that return iterator helpers.
func initLazyIteratorMethods(vm *engine.VM, k *iteratorKeys, newHelper func(helperBody) engine.Value) {
	proto := vm.CurrentRealm().Intrinsic(engine.IntrinsicIteratorPrototype)
	method(vm, proto, "map", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "map")
		f := callableArg(vm, it, arg(args, 0))
		return newHelper(&mapHelper{
			src: k.getIteratorDirect(vm, it),
			f:   f,
		})
	})
	method(vm, proto, "filter", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "filter")
		f := callableArg(vm, it, arg(args, 0))
		return newHelper(&filterHelper{
			src: k.getIteratorDirect(vm, it),
			f:   f,
		})
	})
	method(vm, proto, "take", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "take")
		n := limitArg(vm, it, arg(args, 0))
		return newHelper(&takeHelper{
			src:       k.getIteratorDirect(vm, it),
			remaining: n,
		})
	})
	method(vm, proto, "drop", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "drop")
		n := limitArg(vm, it, arg(args, 0))
		return newHelper(&dropHelper{
			src:       k.getIteratorDirect(vm, it),
			remaining: n,
		})
	})
	method(vm, proto, "flatMap", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "flatMap")
		f := callableArg(vm, it, arg(args, 0))
		return newHelper(&flatMapHelper{
			src: k.getIteratorDirect(vm, it),
			f:   f,
		})
	})
	method(vm, proto, "chunks", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "chunks")
		size := sizeArg(vm, it, arg(args, 0))
		return newHelper(&chunksHelper{
			src:  k.getIteratorDirect(vm, it),
			size: size,
		})
	})
	method(vm, proto, "windows", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "windows")
		size := sizeArg(vm, it, arg(args, 0))
		allowPartial := false
		switch u := arg(args, 1); {
		case u.IsUndefined():
		case u.IsString() && u.Str() == "only-full":
		case u.IsString() && u.Str() == "allow-partial":
			allowPartial = true
		default:
			closeAndThrowTypeError(vm, it, "undersized must be \"only-full\" or \"allow-partial\"")
		}
		return newHelper(&chunksHelper{
			src:          k.getIteratorDirect(vm, it),
			size:         size,
			windows:      true,
			allowPartial: allowPartial,
		})
	})
}

// initEagerIteratorMethods defines the methods of Iterator.prototype that consume the iterator.
func initEagerIteratorMethods(vm *engine.VM, k *iteratorKeys) {
	proto := vm.CurrentRealm().Intrinsic(engine.IntrinsicIteratorPrototype)
	method(vm, proto, "reduce", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "reduce")
		f := callableArg(vm, it, arg(args, 0))
		r := k.getIteratorDirect(vm, it)
		var acc engine.Value
		counter := 0.0
		if len(args) < 2 {
			v, ok := vm.IteratorStepValue(r.it, r.next)
			if !ok {
				vm.ThrowTypeError(vm.CurrentRealm(), "reduce of empty iterator with no initial value")
			}
			acc = v
			counter = 1
		} else {
			acc = args[1]
		}
		vm.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			acc = vm.Call(f, engine.Undefined, acc, v, engine.Number(counter))
			counter++
			return true
		})
		return acc
	})
	method(vm, proto, "toArray", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "toArray")
		r := k.getIteratorDirect(vm, it)
		var items []engine.Value
		for {
			v, ok := vm.IteratorStepValue(r.it, r.next)
			if !ok {
				return vm.NewArrayOf(items)
			}
			items = append(items, v)
		}
	})
	method(vm, proto, "forEach", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "forEach")
		f := callableArg(vm, it, arg(args, 0))
		r := k.getIteratorDirect(vm, it)
		counter := 0.0
		vm.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			vm.Call(f, engine.Undefined, v, engine.Number(counter))
			counter++
			return true
		})
		return engine.Undefined
	})
	// search stops at the first value for which the predicate's truth value equals want.
	// It returns that value and true, or undefined and false if the iterator is exhausted.
	search := func(vm *engine.VM, this engine.Value, args []engine.Value, name string, want bool) (engine.Value, bool) {
		it := receiver(vm, this, name)
		f := callableArg(vm, it, arg(args, 0))
		r := k.getIteratorDirect(vm, it)
		counter := 0.0
		found := false
		result := engine.Undefined
		vm.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			if engine.ToBoolean(vm.Call(f, engine.Undefined, v, engine.Number(counter))) == want {
				found = true
				result = v
				return false
			}
			counter++
			return true
		})
		return result, found
	}
	method(vm, proto, "some", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		_, found := search(vm, this, args, "some", true)
		return engine.Bool(found)
	})
	method(vm, proto, "every", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		_, found := search(vm, this, args, "every", false)
		return engine.Bool(!found)
	})
	method(vm, proto, "find", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v, _ := search(vm, this, args, "find", true)
		return v
	})
	method(vm, proto, "includes", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "includes")
		target := arg(args, 0)
		toSkip := 0.0
		if s := arg(args, 1); !s.IsUndefined() {
			n := s.Number()
			if !s.IsNumber() || (n != math.Trunc(n) && !math.IsInf(n, 0)) {
				closeAndThrowTypeError(vm, it, vm.DisplayString(s)+" must be an integer")
			}
			if n < 0 || (!math.IsInf(n, 1) && n > numconv.MaxSafeInteger) {
				closeAndThrowRangeError(vm, it, vm.DisplayString(s)+" must be between 0 and 2^53 - 1")
			}
			toSkip = n
		}
		r := k.getIteratorDirect(vm, it)
		skipped := 0.0
		found := false
		for {
			v, ok := vm.IteratorStepValue(r.it, r.next)
			if !ok {
				return engine.False
			}
			if skipped < toSkip {
				skipped++
				continue
			}
			if engine.SameValueZero(v, target) {
				found = true
				break
			}
		}
		vm.IteratorClose(r.it)
		return engine.Bool(found)
	})
	method(vm, proto, "join", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(vm, this, "join")
		sep := wtf8.String(",")
		if s := arg(args, 0); !s.IsUndefined() {
			vm.IfAbruptCloseIterator(it, func() {
				sep = vm.ToString(s)
			})
		}
		r := k.getIteratorDirect(vm, it)
		var sb wtf8.Buffer
		first := true
		for {
			v, ok := vm.IteratorStepValue(r.it, r.next)
			if !ok {
				return vm.NewString(sb.String())
			}
			if !first {
				sb.WriteString(sep)
			}
			first = false
			if !v.IsNullish() {
				vm.IfAbruptCloseIterator(it, func() {
					sb.WriteString(vm.ToString(v))
				})
			}
			if sb.Len() > engine.MaxStringLength {
				closeAndThrowRangeError(vm, it, "invalid string length")
			}
		}
	})
}
