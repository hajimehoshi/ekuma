// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NativeFunc is a function implemented in Go.
//
// args is only valid until the function returns. An exception is thrown with the Throw methods of
// [ExecutionContext], which do not return.
type NativeFunc func(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value

// nativeFast holds the forms of a native function that its callers use instead of calling it.
type nativeFast struct {
	// step or framedStep is set for the next method of a built-in iterator. It is what the
	// function does without creating the iterator result. step records the frame of the function
	// where it has to, or declines, returning stepDeclined and false without doing anything. The
	// caller of framedStep records it.
	step       IteratorStepFunc
	framedStep IteratorStepFunc

	// pairStep is set for the next method of a built-in iterator that can yield pairs. It is
	// what the function does without creating the iterator result or the pair.
	pairStep IteratorPairStepFunc

	// arrayIndex is the indexed element read of the original array iterator next method.
	arrayIndex func(cx *ExecutionContext, o *storage.Object, i uint32) storage.Value

	// number1 and number2 compute the function for exactly one or two arguments that are
	// numbers.
	number1 NumberKernel1
	number2 NumberKernel2

	// method0, method1 and method2 compute the function for its this value and exactly no, one
	// or two arguments, unless they decline.
	method0 MethodKernel0
	method1 MethodKernel1
	method2 MethodKernel2

	// contract holds for all of the forms, and for what the interpreter computes for the
	// extremum of the function.
	contract ShortcutContract
}

// callMethod calls the method kernel in the realm r for this and the arguments args. It reports false when
// there is none for as many arguments or the kernel declines.
func (k *nativeFast) callMethod(r *storage.RealmPart, this storage.Value, args []storage.Value) (storage.Value, bool) {
	switch {
	case len(args) == 0 && k.method0 != nil:
		return k.method0(quiet.ContextOf(r), this)
	case len(args) == 1 && k.method1 != nil:
		return k.method1(quiet.ContextOf(r), this, args[0])
	case len(args) == 2 && k.method2 != nil:
		return k.method2(quiet.ContextOf(r), this, args[0], args[1])
	}
	return storage.Value{}, false
}

// hasIteratorFast reports whether f is a native function of the realm of cx with fast forms.
func (cx *ExecutionContext) hasIteratorFast(f *storage.Function) bool {
	return f.Info != nil && f.Info.Native.HasFast() && f.Info.Realm == cx.realmPart
}

// iteratorPairStepOf returns the pair step of the next method f of a built-in iterator in realm r, or nil.
func iteratorPairStepOf(f *storage.Function, r *storage.RealmPart) IteratorPairStepFunc {
	info := f.Info
	if info == nil || !info.Native.HasFast() || info.Realm != r {
		return nil
	}
	return nativeFastOf(info.Native).pairStep
}

func (cx *ExecutionContext) initKeys() {
	for k, name := range [storage.CommonKeyCount]wtf8.String{
		storage.KeyLength: "length", storage.KeyPrototype: "prototype", storage.KeyConstructor: "constructor", storage.KeyName: "name",
		storage.KeyMessage: "message", storage.KeyStack: "stack", storage.KeyCause: "cause", storage.KeyErrors: "errors",
		storage.KeyError: "error", storage.KeySuppressed: "suppressed",
		storage.KeyToString: "toString", storage.KeyValueOf: "valueOf", storage.KeyToJSON: "toJSON",
		storage.KeyCall: "call", storage.KeyApply: "apply", storage.KeyBind: "bind",
		storage.KeyGet: "get", storage.KeySet: "set", storage.KeyValue: "value", storage.KeyWritable: "writable",
		storage.KeyEnumerable: "enumerable", storage.KeyConfigurable: "configurable",
		storage.KeyNext: "next", storage.KeyDone: "done", storage.KeyReturn: "return", storage.KeyThrow: "throw", storage.KeyThen: "then",
		storage.KeyCallee: "callee", storage.KeyCaller: "caller", storage.KeyArguments: "arguments",
		storage.KeyIndex: "index", storage.KeyInput: "input", storage.KeyGroups: "groups", storage.KeyLastIndex: "lastIndex",
		storage.KeyRaw: "raw", storage.KeySource: "source", storage.KeyFlags: "flags", storage.KeyDefault: "default",
		storage.KeyGetPrototypeOf: "getPrototypeOf", storage.KeySetPrototypeOf: "setPrototypeOf",
		storage.KeyIsExtensible: "isExtensible", storage.KeyPreventExtensions: "preventExtensions",
		storage.KeyGetOwnPropertyDescriptor: "getOwnPropertyDescriptor", storage.KeyDefineProperty: "defineProperty",
		storage.KeyHas: "has", storage.KeyDeleteProperty: "deleteProperty", storage.KeyOwnKeys: "ownKeys", storage.KeyConstruct: "construct",
		storage.KeyAdd: "add", storage.KeyKeys: "keys", storage.KeySize: "size", storage.KeyStatus: "status", storage.KeyReason: "reason",
		storage.KeyMode: "mode", storage.KeyPadding: "padding",
		storage.KeyToLocaleString: "toLocaleString", storage.KeyToISOString: "toISOString",
		storage.KeyHasIndices: "hasIndices", storage.KeyGlobal: "global", storage.KeyIgnoreCase: "ignoreCase",
		storage.KeyMultiline: "multiline", storage.KeyDotAll: "dotAll", storage.KeyUnicode: "unicode",
		storage.KeyUnicodeSets: "unicodeSets", storage.KeySticky: "sticky",
		storage.KeyExec: "exec",
	} {
		cx.Keys.Common[k] = cx.Intern(name)
	}
	for s, name := range [storage.WellKnownSymbolCount]wtf8.String{
		storage.SymbolIterator: "Symbol.iterator", storage.SymbolAsyncIterator: "Symbol.asyncIterator",
		storage.SymbolHasInstance: "Symbol.hasInstance", storage.SymbolToPrimitive: "Symbol.toPrimitive",
		storage.SymbolToStringTag: "Symbol.toStringTag", storage.SymbolSpecies: "Symbol.species",
		storage.SymbolIsConcatSpreadable: "Symbol.isConcatSpreadable", storage.SymbolUnscopables: "Symbol.unscopables",
		storage.SymbolMatch: "Symbol.match", storage.SymbolMatchAll: "Symbol.matchAll", storage.SymbolReplace: "Symbol.replace",
		storage.SymbolSearch: "Symbol.search", storage.SymbolSplit: "Symbol.split",
		storage.SymbolDispose: "Symbol.dispose", storage.SymbolAsyncDispose: "Symbol.asyncDispose",
		storage.SymbolMetadata: "Symbol.metadata",
	} {
		cx.Keys.WellKnown[s] = cx.NewSymbol(name, true)
	}
}

// InterruptedError reports the cause of an interruption by [agent.Interrupt].
type InterruptedError struct {
	Cause error
}

func (e *InterruptedError) Error() string {
	if e.Cause == nil {
		return "ekuma: interrupted"
	}
	return "ekuma: interrupted: " + e.Cause.Error()
}

func (e *InterruptedError) Unwrap() error {
	return e.Cause
}

// PanicError is the cause of the end of the module evaluations that a Go panic passed through.
type PanicError struct {
	// Value is the value of the panic.
	Value any
}

func (e *PanicError) Error() string {
	return "ekuma: panic"
}

func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

func isHostPanic(value any) bool {
	switch value.(type) {
	case *throwSignal, *storage.InterruptSignal:
		return false
	default:
		return true
	}
}

// Interrupt stops execution with an [InterruptedError] carrying cause at the next loop
// or call. It may be called from any goroutine and remains pending until observed or
// cleared. Interrupted module evaluations are rejected with an Error whose message is
// "ekuma: interrupted" and whose [storage.GoError] is cause, or the [InterruptedError] when
// cause is nil.
func (ag *agent) Interrupt(cause error) {
	ag.InterruptCause.Store(&cause)
	ag.InterruptFlag.Store(true)
	ag.mailbox.poke()
}

// ClearInterrupt withdraws a request of [agent.Interrupt] that no script has observed.
func (ag *agent) ClearInterrupt() {
	ag.InterruptFlag.Store(false)
	ag.InterruptCause.Store(nil)
}

// Poll stops the script when an interruption was requested. It also takes the notifications of
// other agents, which raise the interrupt flag too. A loop calls it only between runs of many
// steps, such as [storage.PollInterval] steps.
func (ag *agent) Poll() {
	ag.PollYield()
	ag.ResumeInterruption()
	if ag.InterruptFlag.Load() {
		ag.InterruptFlag.Store(false)
		ag.receiveNotifications()
		ag.Raise()
	}
}

// throwSignal is what a thrown JavaScript exception unwinds Go frames with.
// The exception itself is agent.thrown.
type throwSignal struct{}

var errThrow = &throwSignal{}

// nativeCall is a running call of a native function or a proxy, which stack traces show as
// [nativeCall.showsFrame] says.
type nativeCall struct {
	f *storage.Object

	// receiver supplies the type name of the receiver in stack traces.
	receiver storage.Value

	// frames is the number of frames when the call started.
	frames int

	// newTarget is the constructor that the function is being constructed for, and nil in a
	// call.
	newTarget *storage.Object
}

// pushNative records the start of the call of the native function f for newTarget, which is nil
// in a call. A shortcut records the callee selected by method lookup. It throws a RangeError
// when maxNatives calls of native functions are running.
func (cx *ExecutionContext) pushNative(f *storage.Object, this storage.Value, newTarget *storage.Object) {
	n := len(cx.natives)
	if n == cap(cx.natives) {
		cx.growNatives()
	}
	cx.natives = cx.natives[:n+1]
	c := &cx.natives[n]
	c.f, c.receiver, c.frames, c.newTarget = f, this, len(cx.frames), newTarget
}

// growNatives makes room for one more call of a native function, or throws a RangeError when
// maxNatives calls are running.
//
//go:noinline
func (cx *ExecutionContext) growNatives() {
	if len(cx.natives) >= maxNatives {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	// The capacity never exceeds maxNatives, so that a push checks only for a full slice.
	s := slices.Grow(cx.natives, 1)
	cx.natives = s[:len(s):min(cap(s), maxNatives)]
}

// callNative dispatches a native call or construction. A proxy inherits its caller's context.
func (cx *ExecutionContext) callNative(f *storage.Object, this storage.Value, args []storage.Value, newTarget *storage.Object) storage.Value {
	if r := f.Fn.Info.Realm; r != nil && r != cx.realmPart {
		cx = executionContextOf(r)
	}
	if previous := cx.activate(); previous != cx {
		defer previous.activate()
	}
	return cx.invokeNative(f, this, args, newTarget)
}

// invokeNative runs f with the already active context.
func (cx *ExecutionContext) invokeNative(f *storage.Object, this storage.Value, args []storage.Value, newTarget *storage.Object) storage.Value {
	if validateExec {
		defer cx.releaseArgs(cx.holdArgs(args))
	}
	// This is pushNative written out, since pushNative does not inline. A change there belongs
	// here too.
	ag := cx.agent
	n := len(ag.natives)
	if n >= cap(ag.natives) {
		cx.growNatives()
		return cx.invokeNative(f, this, args, newTarget)
	}
	ag.natives = ag.natives[:n+1]
	c := &ag.natives[n]
	c.f, c.receiver, c.frames, c.newTarget = f, this, len(ag.frames), newTarget
	r := nativeCodeOf(f.Fn.Info.Native)(cx, this, args)
	cx.popNative()
	return r
}

// showsFrame reports whether stack traces show the call c, and the functions that c calls have
// no caller. A proxy shows its target or its trap instead, and a frameless function nothing.
func (c *nativeCall) showsFrame() bool {
	return c.f.Class != storage.ClassProxy && !c.f.Fn.Frameless()
}

// popNative records the end of the innermost call of a native function.
func (ag *agent) popNative() {
	n := len(ag.natives) - 1
	c := &ag.natives[n]
	ag.natives = ag.natives[:n]
	*c = nativeCall{}
}

// truncateNatives drops the calls of native functions after the first n.
func (ag *agent) truncateNatives(n int) {
	ag.natives = storage.Truncated(ag.natives, n)
}

// dropNatives drops the calls of native functions that started when there were more than
// frames frames, which an exception ended.
func (ag *agent) dropNatives(frames int) {
	n := len(ag.natives)
	for n > 0 && ag.natives[n-1].frames > frames {
		n--
		ag.natives[n] = nativeCall{}
	}
	ag.natives = ag.natives[:n]
}

// agent is an ECMAScript agent. It must not be used from more than one goroutine at a time.
type agent struct {
	// checks is the state of the checks of the interpreter in builds with the build tag
	// ekumavalidate. Being first, it changes no offset in other builds, where it is empty.
	checks execChecks

	// trackJobRealms selects handler realms for jobs in agents with multiple realms or global proxies.
	trackJobRealms bool

	stack  []storage.Value
	frames []frame

	// natives are the calls of native functions that are running, innermost last.
	natives []nativeCall

	// openUpvals are the upvalues that refer to stack slots, ordered by index.
	openUpvals []*storage.Upvalue

	// sp is the first stack index that is not in use by the running function.
	// The call-free interpreter loop synchronizes it before handing off to slow dispatch.
	sp int

	// stackHigh is the stack index below which values may remain from the calls since the
	// outermost call into the engine started, apart from the headroom of [agent.ensureStack].
	stackHigh int

	// framesHigh is the number of elements of the array of frames that may remain from the
	// calls since the outermost call into the engine started.
	framesHigh int

	// depth is the number of interpreter loops that are running.
	depth int

	// entered is the number of calls from Go code outside the engine that are running.
	entered int

	// abortCause defers job and module abandonment until the outermost call ends.
	abortCause error

	thrown   storage.Value
	thrownAt throwSite

	// captureThrowSites follows the reporter of the outermost host call across realms.
	captureThrowSites bool

	// space comes after the stack and the frames, which the interpreter loop reads most and
	// which need the offsets that fit in load instructions.
	space

	jobs jobQueue

	// rejections are the unhandled rejections, for HostPromiseRejectionTracker.
	rejections rejectionTracker

	// asyncWaiters are the waiters of Atomics.waitAsync.
	asyncWaiters asyncWaiters

	// mailbox receives the notifications of other agents.
	mailbox *mailbox

	// canSuspend is AgentCanSuspend.
	canSuspend bool

	// finalization receives the cells of FinalizationRegistry objects whose target was collected,
	// or is nil until the first registry is created.
	finalization *finalizationQueue

	// regExps caches compiled patterns by their flags and source.
	regExps map[bytecode.RegExp]*regexp.Regexp

	active *ExecutionContext

	// callerRealm is the realm that was current when the innermost interpreter loop started.
	callerRealm *storage.RealmPart

	// timeZone is the time zone of local time, or nil for the one of the host.
	timeZone datetime.TimeZone

	defaultLocale string

	// availableDefaultLocale is the locale of [agent.SetAvailableDefaultLocale], or empty.
	availableDefaultLocale string

	// randomSource gives the numbers of Math.random, or is nil for the global source of
	// math/rand/v2.
	randomSource rand.Source

	modules moduleHost

	moduleEvaluations moduleEvaluations

	// hostContext is the host context of the running script, job or cleanup.
	hostContext

	// madeHTMLDDA reports whether the agent made an object with the [[IsHTMLDDA]] internal slot.
	madeHTMLDDA bool

	// clock gives the current time, or is nil for time.Now. SetClock keeps HasHostClock up with it.
	clock func() time.Time
}

// initialStackSize is the number of values that the stack of an agent has room for from the start.
// It is large enough for the Go runtime to allocate it on pages of its own, which the interpreter
// loop runs measurably faster with.
const initialStackSize = 1 << 12

// NewExecutionContext creates an agent and returns its initial realm's context and host handle.
// The realm starts without the standard built-ins.
func NewExecutionContext(hostDefined any) (*ExecutionContext, *Host) {
	host := &Host{
		HostDefinedValue:    hostDefined,
		stack:               make([]storage.Value, initialStackSize),
		SymbolRegistry:      map[wtf8.String]*storage.Object{},
		thrown:              storage.Undefined,
		mailbox:             newMailbox(),
		MaxBigIntResultBits: MaxBigIntBits,
	}
	r := host.newRealm()
	cx := &r.context
	cx.activate()
	cx.SymbolShape = &storage.Shape{
		Space: cx.space,
		Small: []storage.ShapeEntry{},
	}
	cx.initRealmPrototypes()

	cx.Strings.StartKeeping()
	cx.Strings.Init(r.Shapes.String)
	cx.initKeys()
	for t, name := range typeofNames {
		cx.Typeofs[t] = storage.ObjectValue(cx.Intern(name))
	}
	cx.InitMissHolder(r.Shapes.NullProto[storage.OrdinaryRoot])
	cx.initRealmGlobals()
	cx.Strings.EndKeeping()
	return cx, host
}

// ErrorPrototypeIntrinsic returns the intrinsic that is the prototype of the native error type k.
func ErrorPrototypeIntrinsic(k ErrorKind) storage.Intrinsic {
	return storage.IntrinsicErrorPrototype + storage.Intrinsic(k)
}

// TypedArrayConstructorIntrinsic returns the intrinsic constructor for the element type t.
func TypedArrayConstructorIntrinsic(t element.Type) storage.Intrinsic {
	return storage.IntrinsicInt8Array + storage.Intrinsic(t)
}

// TypedArrayPrototypeIntrinsic returns the intrinsic prototype for the element type t.
func TypedArrayPrototypeIntrinsic(t element.Type) storage.Intrinsic {
	return storage.IntrinsicInt8ArrayPrototype + storage.Intrinsic(t)
}

// IntrinsicValue returns the intrinsic object id of the execution realm, which must be set, as a value.
func (cx *ExecutionContext) IntrinsicValue(id storage.Intrinsic) storage.Value {
	return storage.Value{
		O: cx.realm.Intrinsics[id],
	}
}

// ErrorKind is a native error type.
type ErrorKind uint8

const (
	ErrorKindError ErrorKind = iota
	ErrorKindType
	ErrorKindRange
	ErrorKindReference
	ErrorKindSyntax
	ErrorKindEval
	ErrorKindURI
	ErrorKindAggregate
	ErrorKindSuppressed
	ErrorKindCount
)

// Throw throws v as a JavaScript exception. It does not return.
func (ag *agent) Throw(v storage.Value) {
	ag.recordThrow(v)
	panic(errThrow)
}

func (ag *agent) recordThrow(v storage.Value) {
	if ag.captureThrowSites {
		ag.thrownAt = throwSite{}
		if n := len(ag.frames); n > 0 {
			fr := &ag.frames[n-1]
			ag.thrownAt = throwSite{
				code: fr.fp.Func,
				pc:   fr.pc - 1,
			}
		}
	}
	ag.thrown = v
}

type throwSite struct {
	code *bytecode.Func
	pc   int
}

func (s throwSite) location() ExceptionLocation {
	var loc ExceptionLocation
	if s.code != nil {
		loc.File = s.code.File
		if offset := s.code.SourceOffset(s.pc); offset >= 0 {
			loc.SourceLine, loc.Line, loc.Column = s.code.Lines.SourceLine(offset)
		}
	}
	return loc
}

// A finally handler keeps its pending exception in a private bytecode register.
// Its source location must survive any further exceptions handled by the finalizer.
type savedException struct {
	value storage.Value
	at    throwSite
}

func (ag *agent) saveException() storage.Value {
	return storage.Value{O: storage.NewMarker(&savedException{
		value: ag.thrown,
		at:    ag.thrownAt,
	})}
}

func unpackException(v storage.Value) (storage.Value, *throwSite) {
	if v.O != nil && v.O.Kind == storage.KindHole {
		if saved, ok := v.O.Ext.(*savedException); ok {
			return saved.value, &saved.at
		}
	}
	return v, nil
}

func (ag *agent) restoreException(v storage.Value) {
	value, site := unpackException(v)
	ag.thrown = value
	if site != nil {
		ag.thrownAt = *site
	}
}

// newError returns a new error object of the given type in the execution realm.
func (cx *ExecutionContext) newError(kind ErrorKind, msg string) *storage.Object {
	e := cx.NewErrorObject(cx.Intrinsic(ErrorPrototypeIntrinsic(kind)))
	// A message that quotes a value of the script can be longer than a string may be.
	cx.Define(e, cx.Key(storage.KeyMessage), cx.NewString(storage.Clip(wtf8.FromValidUTF8(msg), storage.MaxStringLength, "")), storage.FlagsHidden)
	e.SetExtState(cx.newErrorData(e, msg, len(cx.frames), len(cx.natives)))
	return e
}

func (cx *ExecutionContext) throwError(kind ErrorKind, msg string) {
	cx.Throw(storage.Value{O: cx.newError(kind, msg)})
}

// ThrowTypeError throws a TypeError in the execution realm with the given message. It does not return.
func (cx *ExecutionContext) ThrowTypeError(msg string) {
	cx.throwError(ErrorKindType, msg)
}

// ThrowRangeError throws a RangeError in the execution realm with the given message. It does not return.
func (cx *ExecutionContext) ThrowRangeError(msg string) {
	cx.throwError(ErrorKindRange, msg)
}

// ThrowReferenceError throws a ReferenceError in the execution realm with the given message. It does not return.
func (cx *ExecutionContext) ThrowReferenceError(msg string) {
	cx.throwError(ErrorKindReference, msg)
}

// ThrowSyntaxError throws a SyntaxError in the execution realm with the given message. It does not return.
func (cx *ExecutionContext) ThrowSyntaxError(msg string) {
	cx.throwError(ErrorKindSyntax, msg)
}

// RecordStack records the stack trace of the new error object e, whose message is msg in UTF-8,
// as what [storage.ErrorStack] returns. The running native function constructs e for newTarget:
// the trace leaves out that function, and the frames up to the call of newTarget when newTarget
// is running.
func (ag *agent) RecordStack(e *storage.Object, msg string, newTarget *storage.Object) {
	frames, natives := len(ag.frames), max(len(ag.natives)-1, 0)
	for i := frames - 1; i >= 0; i-- {
		if ag.frames[i].cl == newTarget {
			frames = i
			for natives > 0 && ag.natives[natives-1].frames > i {
				natives--
			}
			break
		}
	}
	e.SetExtState(ag.newErrorData(e, msg, frames, natives))
}

// maxStackFrames is the number of calls that a stack trace shows.
const maxStackFrames = 16

// newErrorData returns the error data of the new error e, whose stack trace shows the first
// frames frames and the first natives calls of native functions. The names that the trace
// shows are read now, and the trace is formatted when it is first read.
func (ag *agent) newErrorData(e *storage.Object, msg string, frames, natives int) *storage.ErrorData {
	d := &storage.ErrorData{
		Name:    "Error",
		Msg:     msg,
		Pending: true,
	}
	if v, ok := storage.DiagnosticDataProperty(e, ag.Key(storage.KeyName)); ok && v.IsString() {
		d.Name = v.Str()
	}
	if n := min(frames+natives, maxStackFrames); n > 0 {
		d.Calls = make([]storage.StackCall, 0, n)
	}
	globalThis := e.CreationRealm().GlobalThis
	j := natives - 1
	for i := frames - 1; len(d.Calls) < maxStackFrames; i-- {
		// A native function called from the frame i runs inside it.
		for ; j >= 0 && ag.natives[j].frames > i && len(d.Calls) < maxStackFrames; j-- {
			c := &ag.natives[j]
			if !c.showsFrame() {
				continue
			}
			d.Calls = append(d.Calls, storage.StackCall{
				Name:  c.f.Fn.NativeName(),
				Recv:  ag.receiverName(globalThis, c.newTarget != nil, c.receiver),
				IsNew: c.newTarget != nil,
			})
		}
		if i < 0 || len(d.Calls) >= maxStackFrames {
			break
		}
		fr := &ag.frames[i]
		c := storage.StackCall{
			Fp:    fr.fp,
			Name:  fr.fp.Name,
			Pc:    fr.pc,
			IsNew: fr.newTarget != nil,
		}
		if fr.fp.IsEval {
			c.Name = "eval"
		} else {
			c.Recv = ag.receiverName(globalThis, c.IsNew, fr.this)
		}
		d.Calls = append(d.Calls, c)
	}
	return d
}

// receiverName returns the type name that a stack trace shows for a call with this, or an
// empty string when the call shows no receiver.
func (ag *agent) receiverName(globalThis *storage.Object, isNew bool, this storage.Value) wtf8.String {
	if isNew || this.IsObject() && this.O == globalThis || this.IsNullish() {
		return ""
	}
	return ag.typeName(this)
}

// typeName returns the name of the type of v that a stack trace shows for a method called on v.
// It runs no JavaScript.
func (ag *agent) typeName(v storage.Value) wtf8.String {
	switch {
	case v.IsString():
		return "String"
	case v.IsNumber():
		return "Number"
	case v.IsBool():
		return "Boolean"
	case v.IsSymbol():
		return "Symbol"
	case v.IsBigInt():
		return "BigInt"
	}
	o := v.O
	if v.IsCallable() {
		if n, ok := storage.DiagnosticDataProperty(o, ag.Key(storage.KeyName)); ok && n.IsString() && n.Str() != "" {
			return n.Str()
		}
		return "Function"
	}
	if c, ok := storage.DiagnosticDataProperty(o, ag.Key(storage.KeyConstructor)); ok && c.IsCallable() {
		if n, ok := storage.DiagnosticDataProperty(c.O, ag.Key(storage.KeyName)); ok && n.IsString() && n.Str() != "" && n.Str() != "Object" {
			return n.Str()
		}
	}
	if t, ok := storage.DiagnosticDataProperty(o, ag.WellKnownSymbol(storage.SymbolToStringTag)); ok && t.IsString() && t.Str() != "" {
		return t.Str()
	}
	return "Object"
}

// takeException converts the pending exception to a Go error.
func (ag *agent) takeException() error {
	v := ag.thrown
	ag.thrown = storage.Undefined
	return ag.ErrorOf(v)
}

// NewError returns a new error object of the given type in the execution realm with the message msg, which is UTF-8.
func (cx *ExecutionContext) NewError(kind ErrorKind, msg string) storage.Value {
	return storage.Value{O: cx.newError(kind, msg)}
}

// DisplayString describes v for messages, without running any JavaScript. A long string is
// shortened.
func (ag *agent) DisplayString(v storage.Value) string {
	return storage.DisplayString(v)
}

// protect runs f and reports whether it completed without throwing.
// When it reports false, the exception is in cx.thrown.
func (cx *ExecutionContext) protect(f func()) (ok bool) {
	frames := len(cx.frames)
	natives := len(cx.natives)
	sp := cx.sp
	depth := cx.depth
	saved := cx.active
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			saved.activate()
			cx.unwindTo(frames)
			cx.truncateNatives(natives)
			cx.sp = sp
			cx.depth = depth
			ok = false
		}
	}()
	f()
	return true
}

// unwindTo drops the frames above the first n, closing their upvalues.
func (ag *agent) unwindTo(n int) {
	if len(ag.frames) <= n {
		return
	}
	if len(ag.openUpvals) > 0 {
		ag.closeUpvals(ag.frames[n].base)
	}
	for i := n; i < len(ag.frames); i++ {
		ag.frames[i] = frame{}
	}
	ag.frames = ag.frames[:n]
}
