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
func (r *iterRecord) stepValue(cx *engine.ExecutionContext) (engine.Value, bool) {
	return cx.IteratorStepValue(r.it, r.next)
}

// iteratorKeys holds the property keys that the iterator helpers use.
type iteratorKeys struct {
	next *engine.Object
	ret  *engine.Object
}

// getIteratorDirect implements GetIteratorDirect.
func (k *iteratorKeys) getIteratorDirect(cx *engine.ExecutionContext, it engine.Value) iterRecord {
	return iterRecord{
		it:   it,
		next: cx.GetV(it, k.next),
	}
}

// helperBody is the algorithm of an iterator helper object.
type helperBody interface {
	// step returns the next value of the helper, or reports false when the helper is done.
	step(cx *engine.ExecutionContext) (engine.Value, bool)

	// close closes the underlying iterators when return is called before the helper is done.
	close(cx *engine.ExecutionContext)
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
func resumeHelper(cx *engine.ExecutionContext, this engine.Value, mode engine.ResumeMode) (engine.Value, bool) {
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
			body := h.body
			h.body = nil
			body.close(cx)
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
func (h *iteratorHelper) resumeBody(cx *engine.ExecutionContext, mode engine.ResumeMode) (v engine.Value, yielded bool) {
	h.state = helperExecuting
	defer func() {
		if yielded {
			h.state = helperSuspendedYield
		} else {
			h.state = helperCompleted
			h.body = nil
		}
	}()
	if mode == engine.ResumeReturn {
		h.body.close(cx)
		return engine.Undefined, false
	}
	return h.body.step(cx)
}

// initIteratorHelpers defines Iterator and the iterator helpers.
func initIteratorHelpers(cx *engine.ExecutionContext) {
	k := &iteratorKeys{
		next: cx.Intern("next"),
		ret:  cx.Intern("return"),
	}
	proto := cx.Intrinsic(engine.IntrinsicIteratorPrototype)
	var ctor *engine.Object
	ctor = cx.NewNativeConstructor("Iterator", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil || nt == ctor {
			cx.ThrowTypeError("Iterator cannot be constructed directly")
		}
		return engine.ObjectValue(cx.NewInstance(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicIteratorPrototype)))
	})
	cx.DefineConstructorPrototype(ctor, engine.IntrinsicIteratorPrototype)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Iterator"), engine.ObjectValue(ctor), engine.FlagsHidden)

	// The properties that Iterator.prototype had in earlier editions are accessors whose setters
	// ignore the prototype.
	ignoringAccessor := func(key *engine.Object, name string, v engine.Value) {
		g := cx.NewNative(wtf8.String("get "+name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return v
		})
		s := cx.NewNative(wtf8.String("set "+name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			setterThatIgnoresPrototypeProperties(cx, this, proto, engine.NamedKey(key), arg(args, 0), "Iterator.prototype setter")
			return engine.Undefined
		})
		cx.DefineAccessor(proto, key, g, s, engine.FlagConfigurable)
	}
	ignoringAccessor(cx.Key(engine.KeyConstructor), "constructor", engine.ObjectValue(ctor))
	ignoringAccessor(cx.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", cx.SharedString("Iterator"))

	symMethod(cx, proto, cx.WellKnownSymbol(engine.SymbolDispose), "[Symbol.dispose]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if m := cx.GetMethod(this, k.ret); m != nil {
			cx.Call(m, this)
		}
		return engine.Undefined
	})

	helperProto := cx.NewObject(proto)
	iteratorNext(cx, helperProto, engine.IteratorClosureRealm, func(cx *engine.ExecutionContext, this engine.Value) (engine.Value, bool) {
		return resumeHelper(cx, this, engine.ResumeNext)
	})
	method(cx, helperProto, "return", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		resumeHelper(cx, this, engine.ResumeReturn)
		return cx.CreateIterResultObject(engine.Undefined, true)
	})
	cx.Define(helperProto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Iterator Helper"), engine.FlagConfigurable)
	newHelper := func(cx *engine.ExecutionContext, body helperBody) engine.Value {
		return engine.ObjectValue(cx.NewIteratorObject(helperProto, &iteratorHelper{body: body}))
	}

	initIteratorFrom(cx, k, ctor)
	initIteratorConcat(cx, ctor, newHelper)
	initIteratorZip(cx, ctor, newHelper)
	initLazyIteratorMethods(cx, k, newHelper)
	initEagerIteratorMethods(cx, k)
}

// wrappedIterator is the state of an object that Iterator.from wraps an iterator in.
type wrappedIterator struct {
	iterRecord
}

func initIteratorFrom(cx *engine.ExecutionContext, k *iteratorKeys, ctor *engine.Object) {
	wrapProto := cx.NewObject(cx.Intrinsic(engine.IntrinsicIteratorPrototype))
	thisWrapped := func(cx *engine.ExecutionContext, this engine.Value, what string) *wrappedIterator {
		w, _ := engine.IteratorState(this).(*wrappedIterator)
		if w == nil {
			cx.ThrowTypeError("%WrapForValidIteratorPrototype%." + what + " called on an incompatible receiver")
		}
		return w
	}
	method(cx, wrapProto, "next", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		w := thisWrapped(cx, this, "next")
		return cx.CallValue(w.next, w.it)
	})
	method(cx, wrapProto, "return", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		w := thisWrapped(cx, this, "return")
		m := cx.GetMethod(w.it, k.ret)
		if m == nil {
			return cx.CreateIterResultObject(engine.Undefined, true)
		}
		return cx.Call(m, w.it)
	})
	method(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var r iterRecord
		r.it, r.next = cx.GetIteratorFlattenable(arg(args, 0), true)
		if cx.OrdinaryHasInstance(engine.ObjectValue(ctor), r.it) {
			return r.it
		}
		return engine.ObjectValue(cx.NewIteratorObject(wrapProto, &wrappedIterator{iterRecord: r}))
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

func (h *concatHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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
		h.methods[h.pos] = nil
		h.pos++
		h.inner.it, h.inner.next = cx.GetIteratorFromMethod(iterable, method)
		h.active = true
	}
}

func (h *concatHelper) close(cx *engine.ExecutionContext) {
	if h.active {
		cx.IteratorClose(h.inner.it)
	}
}

func initIteratorConcat(cx *engine.ExecutionContext, ctor *engine.Object, newHelper func(*engine.ExecutionContext, helperBody) engine.Value) {
	method(cx, ctor, "concat", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		h := &concatHelper{
			iterables: make([]engine.Value, len(args)),
			methods:   make([]*engine.Object, len(args)),
		}
		for i, item := range args {
			if !item.IsObject() {
				cx.ThrowTypeError(cx.DisplayString(item) + " is not an object")
			}
			m := cx.GetMethod(item, cx.WellKnownSymbol(engine.SymbolIterator))
			if m == nil {
				cx.ThrowTypeError(cx.DisplayString(item) + " is not iterable")
			}
			h.iterables[i] = item
			h.methods[i] = m
		}
		return newHelper(cx, h)
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

func (h *zipHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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
func (h *zipHelper) nextRow(cx *engine.ExecutionContext) []engine.Value {
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
		h.next[i] = engine.Undefined
		results[i] = h.padding[i]
	}
	if !yielded {
		return nil
	}
	return results
}

// remainingDone checks the remaining iterators when the first finishes in strict mode.
func (h *zipHelper) remainingDone(cx *engine.ExecutionContext) bool {
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

func (h *zipHelper) close(cx *engine.ExecutionContext) {
	cx.IteratorCloseAll(h.open)
}

func initIteratorZip(cx *engine.ExecutionContext, ctor *engine.Object, newHelper func(*engine.ExecutionContext, helperBody) engine.Value) {
	modeKey := cx.Intern("mode")
	paddingKey := cx.Intern("padding")
	// options reads the options of Iterator.zip and Iterator.zipKeyed and returns the mode and
	// the padding option.
	options := func(cx *engine.ExecutionContext, iterables, opts engine.Value) (zipMode, engine.Value) {
		if !iterables.IsObject() {
			cx.ThrowTypeError(cx.DisplayString(iterables) + " is not an object")
		}
		// GetOptionsObject.
		if opts.IsUndefined() {
			return zipShortest, engine.Undefined
		}
		if !opts.IsObject() {
			cx.ThrowTypeError("the options must be an object")
		}
		mode := zipShortest
		switch m := cx.GetV(opts, modeKey); {
		case m.IsUndefined():
		case m.IsString() && m.Str() == "shortest":
		case m.IsString() && m.Str() == "longest":
			mode = zipLongest
		case m.IsString() && m.Str() == "strict":
			mode = zipStrict
		default:
			cx.ThrowTypeError("the mode must be \"shortest\", \"longest\" or \"strict\"")
		}
		pad := engine.Undefined
		if mode == zipLongest {
			pad = cx.GetV(opts, paddingKey)
			if !pad.IsUndefined() && !pad.IsObject() {
				cx.ThrowTypeError("the padding must be an object")
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

	method(cx, ctor, "zip", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		iterables := arg(args, 0)
		mode, pad := options(cx, iterables, arg(args, 1))
		input, inputNext := cx.GetIterator(iterables)
		open := []engine.Value{input}
		var next []engine.Value
		for {
			var v engine.Value
			var ok bool
			cx.IfAbruptCloseIterators(open[1:], func() {
				v, ok = cx.IteratorStepValue(input, inputNext)
			})
			if !ok {
				break
			}
			var r iterRecord
			cx.IfAbruptCloseIterators(open, func() {
				r.it, r.next = cx.GetIteratorFlattenable(v, false)
			})
			open = append(open, r.it)
			next = append(next, r.next)
		}
		open[0] = engine.Undefined
		h := newZip(open[1:], next, mode)
		if mode == zipLongest && !pad.IsUndefined() {
			var padIt, padNext engine.Value
			cx.IfAbruptCloseIterators(h.open, func() {
				padIt, padNext = cx.GetIterator(pad)
			})
			using := true
			for i := range h.padding {
				var v engine.Value
				cx.IfAbruptCloseIterators(h.open, func() {
					v, using = cx.IteratorStepValue(padIt, padNext)
				})
				if !using {
					break
				}
				h.padding[i] = v
			}
			if using {
				cx.IfAbruptCloseIterators(h.open, func() {
					cx.IteratorClose(padIt)
				})
			}
		}
		return newHelper(cx, h)
	})

	method(cx, ctor, "zipKeyed", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		iterables := arg(args, 0)
		mode, pad := options(cx, iterables, arg(args, 1))
		o := iterables.Object()
		var open, next []engine.Value
		var keys []engine.PropKey
		for _, key := range cx.OwnKeys(o) {
			var r iterRecord
			var add bool
			cx.IfAbruptCloseIterators(open, func() {
				d, ok := cx.GetOwnProperty(o, key)
				if !ok || !d.Enumerable() {
					return
				}
				v := cx.Get(o, key)
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
				cx.IfAbruptCloseIterators(h.open, func() {
					h.padding[i] = cx.Get(p, key)
				})
			}
		}
		return newHelper(cx, h)
	})
}

// receiver returns this as the iterator of an Iterator.prototype method.
func receiver(cx *engine.ExecutionContext, this engine.Value, name string) engine.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("Iterator.prototype." + name + " called on non-object")
	}
	return this
}

// closeAndThrowTypeError closes the iterator it and throws a TypeError, as the methods
// of Iterator.prototype do for an invalid argument.
func closeAndThrowTypeError(cx *engine.ExecutionContext, it engine.Value, msg string) {
	cx.IfAbruptCloseIterator(it, func() {
		cx.ThrowTypeError(msg)
	})
}

// closeAndThrowRangeError is closeAndThrowTypeError for a RangeError.
func closeAndThrowRangeError(cx *engine.ExecutionContext, it engine.Value, msg string) {
	cx.IfAbruptCloseIterator(it, func() {
		cx.ThrowRangeError(msg)
	})
}

// callableArg returns the function argument of an Iterator.prototype method, closing it and throwing
// a TypeError when f is not callable.
func callableArg(cx *engine.ExecutionContext, it, f engine.Value) *engine.Object {
	if !f.IsCallable() {
		closeAndThrowTypeError(cx, it, cx.DisplayString(f)+" is not a function")
	}
	return f.Object()
}

// limitArg converts the limit argument of take and drop, closing it when the argument is invalid.
func limitArg(cx *engine.ExecutionContext, it, limit engine.Value) float64 {
	var n float64
	cx.IfAbruptCloseIterator(it, func() {
		n = cx.ToNumber(limit)
	})
	if n != n {
		closeAndThrowRangeError(cx, it, cx.DisplayString(limit)+" must be a number")
	}
	n = numconv.ToIntegerOrInfinity(n)
	if n < 0 || (n > numconv.MaxSafeInteger && !math.IsInf(n, 1)) {
		closeAndThrowRangeError(cx, it, cx.DisplayString(limit)+" must be between 0 and 2^53 - 1")
	}
	return n
}

// sizeArg checks the size argument of chunks and windows, closing it when the argument is invalid.
func sizeArg(cx *engine.ExecutionContext, it, size engine.Value) int {
	if !size.IsNumber() || size.Number() != math.Trunc(size.Number()) || math.IsInf(size.Number(), 0) {
		closeAndThrowTypeError(cx, it, cx.DisplayString(size)+" must be an integer")
	}
	n := size.Number()
	if n < 1 || n > math.MaxUint32 {
		closeAndThrowRangeError(cx, it, cx.DisplayString(size)+" must be between 1 and 2^32 - 1")
	}
	return int(n)
}

type mapHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
}

func (h *mapHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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

func (h *mapHelper) close(cx *engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type filterHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
}

func (h *filterHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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

func (h *filterHelper) close(cx *engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type takeHelper struct {
	src       iterRecord
	remaining float64
}

func (h *takeHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
	if h.remaining == 0 {
		cx.IteratorClose(h.src.it)
		return engine.Undefined, false
	}
	if !math.IsInf(h.remaining, 1) {
		h.remaining--
	}
	return h.src.stepValue(cx)
}

func (h *takeHelper) close(cx *engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type dropHelper struct {
	src       iterRecord
	remaining float64
}

func (h *dropHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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

func (h *dropHelper) close(cx *engine.ExecutionContext) {
	cx.IteratorClose(h.src.it)
}

type flatMapHelper struct {
	src     iterRecord
	f       *engine.Object
	counter float64
	inner   iterRecord
	active  bool
}

func (h *flatMapHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
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

func (h *flatMapHelper) close(cx *engine.ExecutionContext) {
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

func (h *chunksHelper) step(cx *engine.ExecutionContext) (engine.Value, bool) {
	if h.exhausted {
		return engine.Undefined, false
	}
	for {
		v, ok := h.src.stepValue(cx)
		if !ok {
			h.exhausted = true
			h.src = iterRecord{}
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

func (h *chunksHelper) close(cx *engine.ExecutionContext) {
	if !h.exhausted {
		cx.IteratorClose(h.src.it)
	}
}

// initLazyIteratorMethods defines the methods of Iterator.prototype that return iterator helpers.
func initLazyIteratorMethods(cx *engine.ExecutionContext, k *iteratorKeys, newHelper func(*engine.ExecutionContext, helperBody) engine.Value) {
	proto := cx.Intrinsic(engine.IntrinsicIteratorPrototype)
	method(cx, proto, "map", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "map")
		f := callableArg(cx, it, arg(args, 0))
		return newHelper(cx, &mapHelper{
			src: k.getIteratorDirect(cx, it),
			f:   f,
		})
	})
	method(cx, proto, "filter", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "filter")
		f := callableArg(cx, it, arg(args, 0))
		return newHelper(cx, &filterHelper{
			src: k.getIteratorDirect(cx, it),
			f:   f,
		})
	})
	method(cx, proto, "take", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "take")
		n := limitArg(cx, it, arg(args, 0))
		return newHelper(cx, &takeHelper{
			src:       k.getIteratorDirect(cx, it),
			remaining: n,
		})
	})
	method(cx, proto, "drop", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "drop")
		n := limitArg(cx, it, arg(args, 0))
		return newHelper(cx, &dropHelper{
			src:       k.getIteratorDirect(cx, it),
			remaining: n,
		})
	})
	method(cx, proto, "flatMap", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "flatMap")
		f := callableArg(cx, it, arg(args, 0))
		return newHelper(cx, &flatMapHelper{
			src: k.getIteratorDirect(cx, it),
			f:   f,
		})
	})
	method(cx, proto, "chunks", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "chunks")
		size := sizeArg(cx, it, arg(args, 0))
		return newHelper(cx, &chunksHelper{
			src:  k.getIteratorDirect(cx, it),
			size: size,
		})
	})
	method(cx, proto, "windows", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "windows")
		size := sizeArg(cx, it, arg(args, 0))
		allowPartial := false
		switch u := arg(args, 1); {
		case u.IsUndefined():
		case u.IsString() && u.Str() == "only-full":
		case u.IsString() && u.Str() == "allow-partial":
			allowPartial = true
		default:
			closeAndThrowTypeError(cx, it, "undersized must be \"only-full\" or \"allow-partial\"")
		}
		return newHelper(cx, &chunksHelper{
			src:          k.getIteratorDirect(cx, it),
			size:         size,
			windows:      true,
			allowPartial: allowPartial,
		})
	})
}

// initEagerIteratorMethods defines the methods of Iterator.prototype that consume the iterator.
func initEagerIteratorMethods(cx *engine.ExecutionContext, k *iteratorKeys) {
	proto := cx.Intrinsic(engine.IntrinsicIteratorPrototype)
	method(cx, proto, "reduce", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "reduce")
		f := callableArg(cx, it, arg(args, 0))
		r := k.getIteratorDirect(cx, it)
		var acc engine.Value
		counter := 0.0
		if len(args) < 2 {
			v, ok := cx.IteratorStepValue(r.it, r.next)
			if !ok {
				cx.ThrowTypeError("reduce of empty iterator with no initial value")
			}
			acc = v
			counter = 1
		} else {
			acc = args[1]
		}
		cx.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			acc = cx.Call(f, engine.Undefined, acc, v, engine.Number(counter))
			counter++
			return true
		})
		return acc
	})
	method(cx, proto, "toArray", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "toArray")
		r := k.getIteratorDirect(cx, it)
		var items []engine.Value
		for {
			v, ok := cx.IteratorStepValue(r.it, r.next)
			if !ok {
				return cx.NewArrayOf(items)
			}
			items = append(items, v)
		}
	})
	method(cx, proto, "forEach", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "forEach")
		f := callableArg(cx, it, arg(args, 0))
		r := k.getIteratorDirect(cx, it)
		counter := 0.0
		cx.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			cx.Call(f, engine.Undefined, v, engine.Number(counter))
			counter++
			return true
		})
		return engine.Undefined
	})
	// search stops at the first value for which the predicate's truth value equals want.
	// It returns that value and true, or undefined and false if the iterator is exhausted.
	search := func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value, name string, want bool) (engine.Value, bool) {
		it := receiver(cx, this, name)
		f := callableArg(cx, it, arg(args, 0))
		r := k.getIteratorDirect(cx, it)
		counter := 0.0
		found := false
		result := engine.Undefined
		cx.IterateRecord(r.it, r.next, func(v engine.Value) bool {
			if engine.ToBoolean(cx.Call(f, engine.Undefined, v, engine.Number(counter))) == want {
				found = true
				result = v
				return false
			}
			counter++
			return true
		})
		return result, found
	}
	method(cx, proto, "some", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		_, found := search(cx, this, args, "some", true)
		return engine.Bool(found)
	})
	method(cx, proto, "every", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		_, found := search(cx, this, args, "every", false)
		return engine.Bool(!found)
	})
	method(cx, proto, "find", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v, _ := search(cx, this, args, "find", true)
		return v
	})
	method(cx, proto, "includes", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "includes")
		target := arg(args, 0)
		toSkip := 0.0
		if s := arg(args, 1); !s.IsUndefined() {
			n := s.Number()
			if !s.IsNumber() || (n != math.Trunc(n) && !math.IsInf(n, 0)) {
				closeAndThrowTypeError(cx, it, cx.DisplayString(s)+" must be an integer")
			}
			if n < 0 || (!math.IsInf(n, 1) && n > numconv.MaxSafeInteger) {
				closeAndThrowRangeError(cx, it, cx.DisplayString(s)+" must be between 0 and 2^53 - 1")
			}
			toSkip = n
		}
		r := k.getIteratorDirect(cx, it)
		skipped := 0.0
		found := false
		for {
			v, ok := cx.IteratorStepValue(r.it, r.next)
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
		cx.IteratorClose(r.it)
		return engine.Bool(found)
	})
	method(cx, proto, "join", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		it := receiver(cx, this, "join")
		sep := wtf8.String(",")
		if s := arg(args, 0); !s.IsUndefined() {
			cx.IfAbruptCloseIterator(it, func() {
				sep = cx.ToString(s)
			})
		}
		r := k.getIteratorDirect(cx, it)
		var sb wtf8.Buffer
		first := true
		for {
			v, ok := cx.IteratorStepValue(r.it, r.next)
			if !ok {
				return cx.NewString(sb.String())
			}
			if !first {
				sb.WriteString(sep)
			}
			first = false
			if !v.IsNullish() {
				cx.IfAbruptCloseIterator(it, func() {
					sb.WriteString(cx.ToString(v))
				})
			}
			if sb.Len() > engine.MaxStringLength {
				closeAndThrowRangeError(cx, it, "invalid string length")
			}
		}
	})
}
