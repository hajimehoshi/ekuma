// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the operations for Go code outside the engine. Unlike the rest of the engine,
// they report a JavaScript exception as an error.

// GlobalObject returns the global object.
func (cx *ExecutionContext) GlobalObject() storage.Value {
	return storage.Value{O: cx.realm.Global}
}

// ExceptionLocation identifies the throw site. Line and Column are 1-based,
// Column counts UTF-16 code units, and zero indicates an unknown position.
type ExceptionLocation struct {
	File       string
	Line       int
	Column     int
	SourceLine wtf8.String
}

// SetExceptionReporter sets the current realm's handler for exceptions escaping
// an outermost host call or a queued callback. The handler receives the thrown
// value and its source location before the next microtask runs.
func (cx *ExecutionContext) SetExceptionReporter(report func(storage.Value, ExceptionLocation)) {
	cx.realm.exceptionReporter = report
}

// ExceptionDescription describes a thrown Error or proxy without invoking JavaScript.
// It returns undefined when the host should convert the thrown value to a string.
// objectToString and errorToString are the reporting realm's original built-in methods.
func (cx *ExecutionContext) ExceptionDescription(v, objectToString, errorToString storage.Value) storage.Value {
	if !v.IsObject() {
		return storage.Undefined
	}
	o := v.O
	for o.Class == storage.ClassProxy {
		st := o.ExtState().(*storage.ProxyState)
		if st.IsGlobal() {
			// Shell globals are ordinary receivers for exception reporting.
			if o == v.O {
				return storage.Undefined
			}
			break
		}
		o = st.Target
		if o == nil {
			return cx.NewString("null")
		}
	}
	if o == v.O && o.Class != storage.ClassError {
		return storage.Undefined
	}
	if o.Fn != nil {
		src := cx.NewString(o.SourceText())
		if n := src.StringLength(); n > 128 {
			return cx.NewString(src.Substring(0, 111) + "...<omitted>..." + src.Substring(n-2, n))
		}
		return src
	}
	method, _ := storage.DiagnosticDataProperty(o, cx.Key(storage.KeyToString))
	if o.Class == storage.ClassError || storage.IsStrictlyEqual(method, errorToString) {
		name, _ := storage.DiagnosticDataProperty(o, cx.Key(storage.KeyName))
		message, _ := storage.DiagnosticDataProperty(o, cx.Key(storage.KeyMessage))
		if !message.IsString() {
			message = cx.Strings.Empty
		}
		if !name.IsString() || name.StringLength() == 0 {
			return message
		}
		if message.StringLength() == 0 {
			return name
		}
		return cx.ConcatStrings([]storage.Value{name, cx.NewString(": "), message})
	}
	if storage.IsStrictlyEqual(method, objectToString) {
		ctor, _ := storage.DiagnosticDataProperty(o, cx.Key(storage.KeyConstructor))
		if ctor.IsCallable() && ctor.O.Class != storage.ClassProxy {
			if name := diagnosticFunctionName(ctor.O.Fn); name != "" {
				return cx.NewString("#<" + name + ">")
			}
		}
	}
	return cx.NewString("[object Object]")
}

func diagnosticFunctionName(fn *storage.Function) wtf8.String {
	const limit = 1000
	var prefix wtf8.Buffer
	for fn.Bound != nil {
		prefix.WriteString("bound ")
		if prefix.Len() >= limit {
			return prefix.String()[:limit]
		}
		fn = fn.Bound.Target.Fn
	}
	name := fn.NativeName()
	if fn.Code != nil {
		name = fn.Code.Name
	}
	end := wtf8.Seek(name, wtf8.Pos{}, limit-prefix.Len())
	return prefix.String() + wtf8.Slice(name, wtf8.Pos{}, end)
}

// NewFunction returns a function implemented by f. name is UTF-8.
func (cx *ExecutionContext) NewFunction(name string, f NativeFunc) storage.Value {
	return storage.Value{O: cx.newGoFunction(wtf8.FromValidUTF8(name), f)}
}

// newGoFunction returns a function with the code f, which need not be registered.
func (cx *ExecutionContext) newGoFunction(name wtf8.String, f NativeFunc) *storage.Object {
	if f == nil {
		panic("engine: nil function")
	}
	return cx.NewNativeWithState(name, 0, nativeGoFunction, f)
}

// goFunction is the code of the functions that newGoFunction makes. Their state is a [NativeFunc].
func goFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.ActiveNativeState().(NativeFunc)(cx, this, args)
}

// NewHTMLDDA returns a function implemented by f that has the [[IsHTMLDDA]] internal slot.
func (cx *ExecutionContext) NewHTMLDDA(f NativeFunc) storage.Value {
	o := cx.newGoFunction("", f)
	o.MakeHTMLDDA()
	cx.madeHTMLDDA = true
	return storage.Value{O: o}
}

// NewPlainObject returns a new empty object.
func (cx *ExecutionContext) NewPlainObject() storage.Value {
	return storage.Value{O: cx.NewOrdinaryObject()}
}

// NewArrayOf returns a new array with the given elements, which it keeps.
func (cx *ExecutionContext) NewArrayOf(elems []storage.Value) storage.Value {
	return storage.Value{O: cx.NewArray(elems)}
}

// GoString returns the content of the string v as UTF-8, with lone surrogates replaced by U+FFFD.
func GoString(v storage.Value) string {
	return wtf8.ToValidUTF8(v.Str())
}

// HostGet returns v[key].
func (cx *ExecutionContext) HostGet(v, key storage.Value) (storage.Value, error) {
	return cx.enter(func() storage.Value {
		val, getter := cx.getValue(v, key)
		if getter != nil {
			return cx.Call(getter, v)
		}
		return val
	})
}

// HostSet stores v[key] = val the way strict mode code does.
func (cx *ExecutionContext) HostSet(v, key, val storage.Value) error {
	_, err := cx.enter(func() storage.Value {
		if setter := cx.setValue(v, key, val, true); setter != nil {
			cx.Call(setter, v, val)
		}
		return storage.Undefined
	})
	return err
}

// HostDelete deletes v[key] the way strict mode code does.
func (cx *ExecutionContext) HostDelete(v, key storage.Value) error {
	_, err := cx.enter(func() storage.Value {
		return cx.deleteValue(v, key, true)
	})
	return err
}

// HostCall calls f with the given this value and arguments.
func (cx *ExecutionContext) HostCall(f, this storage.Value, args []storage.Value) (storage.Value, error) {
	return cx.enter(func() storage.Value {
		return cx.CallValue(f, this, args...)
	})
}

// HostCallMethod calls the method v[key] with v as this.
func (cx *ExecutionContext) HostCallMethod(v, key storage.Value, args []storage.Value) (storage.Value, error) {
	return cx.enter(func() storage.Value {
		f, getter := cx.getValue(v, key)
		if getter != nil {
			f = cx.Call(getter, v)
		}
		if !f.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(key) + " is not a function")
		}
		return cx.CallValue(f, v, args...)
	})
}

// HostConstruct implements new f(...args).
func (cx *ExecutionContext) HostConstruct(f storage.Value, args []storage.Value) (storage.Value, error) {
	return cx.enter(func() storage.Value {
		if !f.IsCallable() || !cx.IsConstructor(f.O) {
			cx.ThrowTypeError(cx.DisplayString(f) + " is not a constructor")
		}
		return storage.Value{O: cx.Construct(f.O, args, f.O)}
	})
}

// HostResolve resolves the promise of c with v, as the [[Resolve]] function of c does.
func (cx *ExecutionContext) HostResolve(c *PromiseCapability, v storage.Value) error {
	_, err := cx.enter(func() storage.Value {
		cx.ResolveCapability(c, v)
		return storage.Undefined
	})
	return err
}

// HostReject rejects the promise of c with v, as the [[Reject]] function of c does.
func (cx *ExecutionContext) HostReject(c *PromiseCapability, v storage.Value) error {
	_, err := cx.enter(func() storage.Value {
		cx.RejectCapability(c, v)
		return storage.Undefined
	})
	return err
}

// HostInstanceOf implements v instanceof t.
func (cx *ExecutionContext) HostInstanceOf(v, t storage.Value) (bool, error) {
	r, err := cx.enter(func() storage.Value {
		return storage.Bool(cx.instanceOf(v, t, nil))
	})
	return storage.ToBoolean(r), err
}

// ThrownMessage describes the thrown value v for a Go error, without running any JavaScript.
func ThrownMessage(v storage.Value) string {
	if v.O == nil || v.O.Shape == nil {
		// A primitive other than a string: its description needs no space.
		return "uncaught " + storage.DisplayString(v)
	}
	return v.O.Shape.Space.ErrorOf(v).Error()
}

// ContinuationData returns the continuation data, the value of the host that promise
// reactions, awaits and FinalizationRegistry objects capture when they are made, and that
// their jobs run with.
func (ag *agent) ContinuationData() any {
	return ag.continuationData
}

// SetContinuationData sets data as the continuation data.
func (ag *agent) SetContinuationData(data any) {
	ag.continuationData = data
}
