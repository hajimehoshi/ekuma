// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NativeFunc is a function implemented in Go.
//
// args is only valid until the function returns. An exception is thrown with the Throw methods of
// [ExecutionContext], which do not return.
type NativeFunc func(cx *ExecutionContext, this Value, args []Value) Value

// function is the state of a callable object.
type function struct {
	// code is the compiled code of a function written in JavaScript.
	code *funcProto

	// upvals are the variables of enclosing functions that code refers to.
	upvals []*upvalue

	native NativeFunc

	// info describes native functions with their own realm.
	info *nativeInfo

	bound *boundFunc

	// home is the object that super property accesses in a method start from.
	home *Object

	// fields are the elements that a class constructor adds to every instance, or nil when
	// there are none.
	fields *[]classElement

	// shared marks the initial state shared by closures of the same function.
	shared bool

	// materialized is set once the initial properties, such as length and name, exist as slots.
	materialized bool

	// constructor says whether the function can be called with new.
	constructor bool

	// derived marks the constructor of a class with an extends clause.
	derived bool

	// classConstructor marks a function that throws when called without new.
	classConstructor bool

	// scopes points to the dynamic scopes that were open where the function was created,
	// innermost last, or is nil when none were.
	scopes *[]*Object
}

// nativeInfo is the description of a native function.
type nativeInfo struct {
	// name and length are the initial values of the properties name and length.
	name   wtf8.String
	length int32

	// extremum says that the function is Math.min or Math.max, which the interpreter computes
	// for two numbers.
	extremum Extremum

	// frameless says that calls of the function show no frame, as [VM.SetFrameless] describes.
	frameless bool

	// prototype is the intrinsic of realm that the property prototype of the function holds,
	// when fixedPrototype is set. The property can then be neither changed nor deleted.
	prototype      Intrinsic
	fixedPrototype bool

	// fast holds the forms of the function that its callers use instead of calling it, or is
	// nil.
	fast *nativeFast

	// realm is the [[Realm]] of the function.
	realm *Realm
}

// nativeFast holds the forms of a native function that its callers use instead of calling it.
type nativeFast struct {
	// step is set for the next method of a built-in iterator. It is what the function does
	// without creating the iterator result.
	step IteratorStepFunc

	// pairStep is set for the next method of a built-in iterator that can yield pairs. It is
	// what the function does without creating the iterator result or the pair.
	pairStep IteratorPairStepFunc

	// number1 and number2 compute the function for exactly one or two arguments that are
	// numbers.
	number1 NumberKernel1
	number2 NumberKernel2

	// method0, method1 and method2 compute the function for its this value and exactly no, one
	// or two arguments, unless they decline.
	method0 MethodKernel0
	method1 MethodKernel1
	method2 MethodKernel2
}

// callMethod calls the method kernel for this and the arguments args. It reports false when
// there is none for as many arguments or the kernel declines.
func (k *nativeFast) callMethod(cx *ExecutionContext, this Value, args []Value) (Value, bool) {
	switch {
	case len(args) == 0 && k.method0 != nil:
		return k.method0(cx, this)
	case len(args) == 1 && k.method1 != nil:
		return k.method1(cx, this, args[0])
	case len(args) == 2 && k.method2 != nil:
		return k.method2(cx, this, args[0], args[1])
	}
	return Value{}, false
}

// nativeName returns the initial value of the property name of the native function f.
func (f *function) nativeName() wtf8.String {
	if f.info == nil {
		return ""
	}
	return f.info.name
}

// frameless reports whether f, which may be nil, is a native function whose calls show no frame.
func (f *function) frameless() bool {
	return f != nil && f.info != nil && f.info.frameless
}

// iteratorStep returns the step function of the next method f of a built-in iterator in realm r, or nil.
func (f *function) iteratorStep(r *Realm) IteratorStepFunc {
	if f.info == nil || f.info.realm != r || f.info.fast == nil {
		return nil
	}
	return f.info.fast.step
}

// iteratorPairStep is iteratorStep for the pair step.
func (f *function) iteratorPairStep(r *Realm) IteratorPairStepFunc {
	if f.info == nil || f.info.realm != r || f.info.fast == nil {
		return nil
	}
	return f.info.fast.pairStep
}

type boundFunc struct {
	target *Object
	this   Value
	args   []Value
}

// upvalue is a variable of an enclosing function. p points to the variable: into the registers
// of that function while it is running, and to val afterwards.
type upvalue struct {
	p *Value

	// index is the stack index or, for a generator, the register that p points to while the upvalue is open.
	index int
	val   Value
}

// CommonKey identifies a string property key that the engine provides.
type CommonKey uint8

const (
	KeyLength CommonKey = iota
	KeyPrototype
	KeyConstructor
	KeyName
	KeyMessage
	KeyStack
	KeyCause
	KeyErrors
	KeyError
	KeySuppressed
	KeyToString
	keyValueOf
	keyToJSON
	keyCall
	keyApply
	keyBind
	KeyGet
	KeySet
	KeyValue
	KeyWritable
	KeyEnumerable
	KeyConfigurable
	keyNext
	keyDone
	keyReturn
	keyThrow
	keyThen
	keyCallee
	keyCaller
	keyArguments
	keyIndex
	keyInput
	keyGroups
	keyLastIndex
	KeyRaw
	keySource
	keyFlags
	keyDefault
	keyGetPrototypeOf
	keySetPrototypeOf
	keyIsExtensible
	keyPreventExtensions
	keyGetOwnPropertyDescriptor
	keyDefineProperty
	keyHas
	keyDeleteProperty
	keyOwnKeys
	keyConstruct
	commonKeyCount
)

// WellKnownSymbol identifies a well-known symbol.
type WellKnownSymbol uint8

const (
	SymbolIterator WellKnownSymbol = iota
	SymbolAsyncIterator
	SymbolHasInstance
	SymbolToPrimitive
	SymbolToStringTag
	SymbolSpecies
	SymbolIsConcatSpreadable
	SymbolUnscopables
	SymbolMatch
	SymbolMatchAll
	SymbolReplace
	SymbolSearch
	SymbolSplit
	SymbolDispose
	SymbolAsyncDispose
	SymbolMetadata
	wellKnownSymbolCount
)

// keys are the property keys that the engine itself uses.
type keys struct {
	common    [commonKeyCount]*Object
	wellKnown [wellKnownSymbolCount]*Object
}

// Key returns the string property key k.
func (vm *VM) Key(k CommonKey) *Object {
	return vm.keys.common[k]
}

// WellKnownSymbol returns the well-known symbol s.
func (vm *VM) WellKnownSymbol(s WellKnownSymbol) *Object {
	return vm.keys.wellKnown[s]
}

func (cx *ExecutionContext) initKeys() {
	for k, name := range [commonKeyCount]wtf8.String{
		KeyLength: "length", KeyPrototype: "prototype", KeyConstructor: "constructor", KeyName: "name",
		KeyMessage: "message", KeyStack: "stack", KeyCause: "cause", KeyErrors: "errors",
		KeyError: "error", KeySuppressed: "suppressed",
		KeyToString: "toString", keyValueOf: "valueOf", keyToJSON: "toJSON",
		keyCall: "call", keyApply: "apply", keyBind: "bind",
		KeyGet: "get", KeySet: "set", KeyValue: "value", KeyWritable: "writable",
		KeyEnumerable: "enumerable", KeyConfigurable: "configurable",
		keyNext: "next", keyDone: "done", keyReturn: "return", keyThrow: "throw", keyThen: "then",
		keyCallee: "callee", keyCaller: "caller", keyArguments: "arguments",
		keyIndex: "index", keyInput: "input", keyGroups: "groups", keyLastIndex: "lastIndex",
		KeyRaw: "raw", keySource: "source", keyFlags: "flags", keyDefault: "default",
		keyGetPrototypeOf: "getPrototypeOf", keySetPrototypeOf: "setPrototypeOf",
		keyIsExtensible: "isExtensible", keyPreventExtensions: "preventExtensions",
		keyGetOwnPropertyDescriptor: "getOwnPropertyDescriptor", keyDefineProperty: "defineProperty",
		keyHas: "has", keyDeleteProperty: "deleteProperty", keyOwnKeys: "ownKeys", keyConstruct: "construct",
	} {
		cx.keys.common[k] = cx.Intern(name)
	}
	for s, name := range [wellKnownSymbolCount]wtf8.String{
		SymbolIterator: "Symbol.iterator", SymbolAsyncIterator: "Symbol.asyncIterator",
		SymbolHasInstance: "Symbol.hasInstance", SymbolToPrimitive: "Symbol.toPrimitive",
		SymbolToStringTag: "Symbol.toStringTag", SymbolSpecies: "Symbol.species",
		SymbolIsConcatSpreadable: "Symbol.isConcatSpreadable", SymbolUnscopables: "Symbol.unscopables",
		SymbolMatch: "Symbol.match", SymbolMatchAll: "Symbol.matchAll", SymbolReplace: "Symbol.replace",
		SymbolSearch: "Symbol.search", SymbolSplit: "Symbol.split",
		SymbolDispose: "Symbol.dispose", SymbolAsyncDispose: "Symbol.asyncDispose",
		SymbolMetadata: "Symbol.metadata",
	} {
		cx.keys.wellKnown[s] = cx.NewSymbol(name, true)
	}
}

// interruptSignal is what an interruption unwinds Go frames with. No JavaScript code can catch it.
type interruptSignal struct {
	// cause is the cause of the interruption.
	cause error
}

// InterruptedError reports the cause of an interruption by [VM.Interrupt].
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
	case *throwSignal, *interruptSignal:
		return false
	default:
		return true
	}
}

// Interrupt stops execution with an [InterruptedError] carrying cause at the next loop
// or call. It may be called from any goroutine and remains pending until observed or
// cleared. Interrupted module evaluations are rejected with an Error whose message is
// "ekuma: interrupted" and whose [GoError] is cause, or the [InterruptedError] when cause is nil.
func (vm *VM) Interrupt(cause error) {
	vm.interruptCause.Store(&cause)
	vm.interrupt.Store(true)
	vm.mailbox.poke()
}

// ClearInterrupt withdraws a request of [VM.Interrupt] that no script has observed.
func (vm *VM) ClearInterrupt() {
	vm.interrupt.Store(false)
	vm.interruptCause.Store(nil)
}

// Poll stops the script when an interruption was requested. It also takes the notifications of
// other agents, which raise the interrupt flag too. A loop calls it only between runs of many
// steps, such as [PollInterval] steps.
func (vm *VM) Poll() {
	vm.pollYield()
	vm.ResumeInterruption()
	if vm.interrupt.Load() {
		vm.interrupt.Store(false)
		vm.receiveNotifications()
		if c := vm.interruptCause.Swap(nil); c != nil {
			vm.raised = &interruptSignal{
				cause: *c,
			}
			panic(vm.raised)
		}
	}
}

// ResumeInterruption panics again with an interruption that a host callback recovered before
// the interruption reached the outermost call from Go.
func (vm *VM) ResumeInterruption() {
	if vm.raised != nil {
		panic(vm.raised)
	}
}

// throwSignal is what a thrown JavaScript exception unwinds Go frames with.
// The exception itself is VM.thrown.
type throwSignal struct{}

var errThrow = &throwSignal{}

// nativeCall is a running call of a native function or a proxy, which stack traces show as
// [nativeCall.showsFrame] says.
type nativeCall struct {
	f *Object

	// receiver supplies the type name of the receiver in stack traces.
	receiver Value

	// frames is the number of frames when the call started.
	frames int

	// newTarget is the constructor that the function is being constructed for, and nil in a
	// call.
	newTarget *Object
}

// pushNative records the start of the call of the native function f for newTarget, which is nil
// in a call. It throws a RangeError when maxNatives calls of native functions are running.
func (cx *ExecutionContext) pushNative(f *Object, this Value, newTarget *Object) {
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
func (cx *ExecutionContext) callNative(f *Object, this Value, args []Value, newTarget *Object) Value {
	if info := f.fn.info; info != nil {
		cx = info.realm.Context()
	}
	if previous := cx.activate(); previous != cx {
		defer previous.activate()
	}
	return cx.invokeNative(f, this, args, newTarget)
}

// invokeNative runs f with the already active context.
func (cx *ExecutionContext) invokeNative(f *Object, this Value, args []Value, newTarget *Object) Value {
	// This is pushNative written out, since pushNative does not inline. A change there belongs
	// here too.
	n := len(cx.natives)
	if n == cap(cx.natives) {
		cx.growNatives()
	}
	cx.natives = cx.natives[:n+1]
	c := &cx.natives[n]
	c.f, c.receiver, c.frames, c.newTarget = f, this, len(cx.frames), newTarget
	r := f.fn.native(cx, this, args)
	cx.popNative()
	return r
}

// showsFrame reports whether stack traces show the call c, and the functions that c calls have
// no caller. A proxy shows its target or its trap instead, and a frameless function nothing.
func (c *nativeCall) showsFrame() bool {
	return c.f.class != classProxy && !c.f.fn.frameless()
}

// popNative records the end of the innermost call of a native function.
func (vm *VM) popNative() {
	n := len(vm.natives) - 1
	vm.natives[n] = nativeCall{}
	vm.natives = vm.natives[:n]
}

// truncateNatives drops the calls of native functions after the first n.
func (vm *VM) truncateNatives(n int) {
	clear(vm.natives[n:])
	vm.natives = vm.natives[:n]
}

// dropNatives drops the calls of native functions that started when there were more than
// frames frames, which an exception ended.
func (vm *VM) dropNatives(frames int) {
	n := len(vm.natives)
	for n > 0 && vm.natives[n-1].frames > frames {
		n--
		vm.natives[n] = nativeCall{}
	}
	vm.natives = vm.natives[:n]
}

// VM is a JavaScript virtual machine, which is an agent of the specification. It must not be used
// from more than one goroutine at a time.
type VM struct {
	// checks is the state of the checks of the interpreter in builds with the build tag
	// ekumavalidate. Being first, it changes no offset in other builds, where it is empty.
	checks execChecks

	// hostDefined is the value that [VM.HostDefined] returns.
	hostDefined any

	// trackJobRealms selects handler realms for jobs in VMs with multiple realms or global proxies.
	trackJobRealms bool

	stack  []Value
	frames []frame

	// natives are the calls of native functions that are running, innermost last.
	natives []nativeCall

	// openUpvals are the upvalues that refer to stack slots, ordered by index.
	openUpvals []*upvalue

	// sp is the first stack index that is not in use by the running function.
	sp int

	// stackHigh is the stack index below which values may remain from the calls since the
	// outermost call into the engine started, apart from the headroom of [VM.ensureStack].
	stackHigh int

	// framesHigh is the number of elements of the array of frames that may remain from the
	// calls since the outermost call into the engine started.
	framesHigh int

	// depth is the number of interpreter loops that are running.
	depth int

	interrupt interruptFlag

	// interruptCause is the cause of the pending interruption.
	interruptCause atomic.Pointer[error]

	// raised is the interruption that is unwinding to the outermost call from Go.
	raised *interruptSignal

	// entered is the number of calls from Go code outside the engine that are running.
	entered int

	// abortCause defers job and module abandonment until the outermost call ends.
	abortCause error

	thrown   Value
	thrownAt throwSite

	// captureThrowSites follows the reporter of the outermost host call across realms.
	captureThrowSites bool

	keys keys

	strings     stringTable
	emptyString Value

	// missHolder holds undefined in its one slot, where the cache entries of properties that
	// were not found point.
	missHolder *Object

	// protoEpoch changes whenever the properties or the prototype of a prototype object change,
	// or its slots move.
	protoEpoch uint32

	lastReadShapeID uint32

	sparseIndices sparseIndexCache

	jobs jobQueue

	// rejections are the unhandled rejections, for HostPromiseRejectionTracker.
	rejections rejectionTracker

	// timedWaiters are the waiters of Atomics.waitAsync with a finite timeout.
	timedWaiters timedWaiters

	// asyncWaiters are the waiters of Atomics.waitAsync whose promise is not resolved. The waiter
	// lists of the data blocks refer to them weakly, so that a VM that is dropped while waiting
	// is collected.
	asyncWaiters map[*waiter]struct{}

	// mailbox receives the notifications of other agents.
	mailbox *mailbox

	// canSuspend is AgentCanSuspend.
	canSuspend bool

	weak weakState

	// symbolRegistry is the GlobalSymbolRegistry List, by key.
	symbolRegistry map[wtf8.String]*Object

	// regExps caches compiled patterns by their flags and source.
	regExps map[bytecode.RegExp]*regexp.Regexp

	active *ExecutionContext

	// callerRealm is the realm that was current when the innermost interpreter loop started.
	callerRealm *Realm

	// timeZone is the time zone of local time, or nil for the one of the host.
	timeZone datetime.TimeZone

	defaultLocale string

	// randomSource gives the numbers of Math.random, or is nil for the global source of
	// math/rand/v2.
	randomSource rand.Source

	modules moduleHost

	builtins builtinLayout

	// indexStrings holds the strings of the array indices below indexStringCount that were
	// made, by index, and nil for the others.
	indexStrings []*Object

	// symbolShape is the shape of the symbols of the VM. It records the VM that they belong
	// to, and no lookup starts from it.
	symbolShape *shape

	// hostContext is the host context of the running script, job or cleanup.
	hostContext

	// matchResultShapes are the shapes of RegExp match results of the realm that made them last.
	matchResultShapes matchResultShapes

	// clock gives the current time, or is nil for time.Now.
	clock func() time.Time

	// maxBigIntResultBits is the bit limit for checked BigInt arithmetic results.
	// Tests lower it to exercise overflow with small operands.
	maxBigIntResultBits int64

	// typeofs are the interned results of the typeof operator, indexed by the typeof constants.
	typeofs [typeofCount]Value

	// madeHTMLDDA reports whether the VM made an object with the [[IsHTMLDDA]] internal slot.
	madeHTMLDDA bool
}

// initialStackSize is the number of values that the stack of a VM has room for from the start.
// It is large enough for the Go runtime to allocate it on pages of its own, which the interpreter
// loop runs measurably faster with.
const initialStackSize = 1 << 12

// NewVM creates a [VM] without the built-in objects. hostDefined is what [VM.HostDefined] returns.
func NewVM(hostDefined any) *VM {
	machine := &VM{
		hostDefined:         hostDefined,
		stack:               make([]Value, initialStackSize),
		symbolRegistry:      map[wtf8.String]*Object{},
		thrown:              Undefined,
		mailbox:             newMailbox(),
		maxBigIntResultBits: MaxBigIntBits,
	}
	r := machine.newRealm()
	cx := r.Context()
	cx.activate()
	cx.symbolShape = cx.newShape(nil, nil)
	cx.initRealmPrototypes()

	cx.emptyString = Value{
		o: &Object{
			kind:  kindString,
			flags: objInterned | objKept,
			shape: r.shapes.string,
		},
	}
	cx.strings.interned.entries = make(map[wtf8.String]sweptRef[Object], internedInitialSize)
	cx.strings.keepNew = true
	cx.strings.interned.put("", cx.emptyString.o, internedMinSweep, isKept)
	for i := range cx.strings.ascii {
		cx.strings.ascii[i] = ObjectValue(cx.Intern(wtf8.String(rune(i))))
	}
	cx.initKeys()
	for t, name := range typeofNames {
		cx.typeofs[t] = ObjectValue(cx.Intern(name))
	}
	cx.missHolder = (&propertyObject{
		kind:  kindObject,
		shape: r.shapes.nullProto,
		slots: []Value{Undefined},
	}).init()
	cx.initRealmGlobals()
	cx.strings.keepNew = false
	return machine
}

// Intrinsic identifies an intrinsic object.
type Intrinsic uint8

const (
	IntrinsicObjectPrototype Intrinsic = iota
	IntrinsicFunctionPrototype
	IntrinsicArrayPrototype
	IntrinsicStringPrototype
	IntrinsicNumberPrototype
	IntrinsicBooleanPrototype
	IntrinsicSymbolPrototype
	IntrinsicBigIntPrototype
	IntrinsicIteratorPrototype
	IntrinsicAsyncIteratorPrototype
	IntrinsicArrayIteratorPrototype
	IntrinsicStringIteratorPrototype
	IntrinsicGeneratorPrototype
	IntrinsicAsyncGeneratorPrototype
	IntrinsicGeneratorFunctionPrototype
	IntrinsicAsyncFunctionPrototype
	IntrinsicAsyncGeneratorFunctionPrototype
	IntrinsicRegExpPrototype
	IntrinsicPromisePrototype
	IntrinsicMapPrototype
	IntrinsicSetPrototype
	IntrinsicDatePrototype
	IntrinsicSharedArrayBufferPrototype
	IntrinsicArrayBufferPrototype
	IntrinsicDataViewPrototype
	IntrinsicWeakMapPrototype
	IntrinsicWeakSetPrototype
	IntrinsicWeakRefPrototype
	IntrinsicFinalizationRegistryPrototype
	IntrinsicDisposableStackPrototype
	IntrinsicAsyncDisposableStackPrototype
	IntrinsicShadowRealmPrototype
	IntrinsicAbstractModuleSourcePrototype
	IntrinsicPromise
	IntrinsicArray
	IntrinsicObject
	IntrinsicFunction
	IntrinsicRegExp
	IntrinsicAbstractModuleSource

	IntrinsicTemporal

	// The Temporal namespace initializes this range of prototypes.
	IntrinsicTemporalInstantPrototype
	IntrinsicTemporalDurationPrototype
	IntrinsicTemporalPlainDatePrototype
	IntrinsicTemporalPlainTimePrototype
	IntrinsicTemporalPlainDateTimePrototype
	IntrinsicTemporalPlainYearMonthPrototype
	IntrinsicTemporalPlainMonthDayPrototype
	IntrinsicTemporalZonedDateTimePrototype

	// The prototypes of the native error types are in the order of [ErrorKind].
	IntrinsicErrorPrototype
	IntrinsicTypeErrorPrototype
	IntrinsicRangeErrorPrototype
	IntrinsicReferenceErrorPrototype
	IntrinsicSyntaxErrorPrototype
	IntrinsicEvalErrorPrototype
	IntrinsicURIErrorPrototype
	IntrinsicAggregateErrorPrototype
	IntrinsicSuppressedErrorPrototype

	IntrinsicThrowTypeError

	// The getters of the caller and arguments properties of non-strict ordinary functions.
	IntrinsicFunctionCallerGetter
	IntrinsicFunctionArgumentsGetter

	// The prototypes of the typed array constructors are in the order of [ElementType].
	IntrinsicInt8ArrayPrototype
	IntrinsicUint8ArrayPrototype
	IntrinsicUint8ClampedArrayPrototype
	IntrinsicInt16ArrayPrototype
	IntrinsicUint16ArrayPrototype
	IntrinsicInt32ArrayPrototype
	IntrinsicUint32ArrayPrototype
	IntrinsicFloat16ArrayPrototype
	IntrinsicFloat32ArrayPrototype
	IntrinsicFloat64ArrayPrototype
	IntrinsicBigInt64ArrayPrototype
	IntrinsicBigUint64ArrayPrototype

	// The initial values of these functions are what the engine treats specially.
	IntrinsicFunctionPrototypeCall
	IntrinsicFunctionPrototypeApply
	IntrinsicEval
	IntrinsicFunctionPrototypeHasInstance
	IntrinsicArrayPrototypeValues
	IntrinsicArrayIteratorPrototypeNext
	IntrinsicArraySpeciesGetter
	IntrinsicTypedArrayPrototypeLengthGetter
	IntrinsicGeneratorPrototypeNext

	// The functions that the engine calls.
	IntrinsicJSONParse

	intrinsicCount
)

// ErrorPrototypeIntrinsic returns the intrinsic that is the prototype of the native error type k.
func ErrorPrototypeIntrinsic(k ErrorKind) Intrinsic {
	return IntrinsicErrorPrototype + Intrinsic(k)
}

// TypedArrayPrototypeIntrinsic returns the intrinsic prototype for the element type t.
func TypedArrayPrototypeIntrinsic(t ElementType) Intrinsic {
	return IntrinsicInt8ArrayPrototype + Intrinsic(t)
}

// Intrinsic returns the intrinsic object id of r, or nil before it is set.
func (r *Realm) Intrinsic(id Intrinsic) *Object {
	return r.intrinsics[id]
}

// SetIntrinsic makes o the intrinsic object id.
func (cx *ExecutionContext) SetIntrinsic(id Intrinsic, o *Object) {
	cx.realm.intrinsics[id] = o
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

// Error is a JavaScript exception that reached Go.
type Error struct {
	// Value is the thrown value.
	Value Value

	msg string
}

func (e *Error) Error() string {
	return e.msg
}

// Throw throws v as a JavaScript exception. It does not return.
func (vm *VM) Throw(v Value) {
	vm.recordThrow(v)
	panic(errThrow)
}

func (vm *VM) recordThrow(v Value) {
	if vm.captureThrowSites {
		vm.thrownAt = throwSite{}
		if n := len(vm.frames); n > 0 {
			fr := &vm.frames[n-1]
			vm.thrownAt = throwSite{
				code: fr.fp.Func,
				pc:   fr.pc - 1,
			}
		}
	}
	vm.thrown = v
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
	value Value
	at    throwSite
}

func (vm *VM) saveException() Value {
	return Value{o: &Object{
		kind: kindHole,
		ext: &savedException{
			value: vm.thrown,
			at:    vm.thrownAt,
		},
	}}
}

func unpackException(v Value) (Value, *throwSite) {
	if v.o != nil && v.o.kind == kindHole {
		if saved, ok := v.o.ext.(*savedException); ok {
			return saved.value, &saved.at
		}
	}
	return v, nil
}

func (vm *VM) restoreException(v Value) {
	value, site := unpackException(v)
	vm.thrown = value
	if site != nil {
		vm.thrownAt = *site
	}
}

// newError returns a new error object of the given type in the execution realm.
func (cx *ExecutionContext) newError(kind ErrorKind, msg string) *Object {
	e := cx.NewErrorObject(cx.realm.Intrinsic(ErrorPrototypeIntrinsic(kind)))
	// A message that quotes a value of the script can be longer than a string may be.
	cx.Define(e, cx.Key(KeyMessage), cx.NewString(clip(wtf8.FromValidUTF8(msg), MaxStringLength, "")), FlagsHidden)
	e.setExtState(cx.newErrorData(e, msg, len(cx.frames), len(cx.natives)))
	return e
}

func (cx *ExecutionContext) throwError(kind ErrorKind, msg string) {
	cx.Throw(Value{o: cx.newError(kind, msg)})
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
// as what [ErrorStack] returns. The running native function constructs e for newTarget: the trace
// leaves out that function, and the frames up to the call of newTarget when newTarget is running.
func (vm *VM) RecordStack(e *Object, msg string, newTarget *Object) {
	frames, natives := len(vm.frames), max(len(vm.natives)-1, 0)
	for i := frames - 1; i >= 0; i-- {
		if vm.frames[i].cl == newTarget {
			frames = i
			for natives > 0 && vm.natives[natives-1].frames > i {
				natives--
			}
			break
		}
	}
	e.setExtState(vm.newErrorData(e, msg, frames, natives))
}

// maxStackFrames is the number of calls that a stack trace shows.
const maxStackFrames = 16

// newErrorData returns the error data of the new error e, whose stack trace shows the first
// frames frames and the first natives calls of native functions. The names that the trace
// shows are read now, and the trace is formatted when it is first read.
func (vm *VM) newErrorData(e *Object, msg string, frames, natives int) *errorData {
	d := &errorData{
		name:    "Error",
		msg:     msg,
		pending: true,
	}
	if v, ok := vm.lookupData(e, vm.Key(KeyName)); ok && v.IsString() {
		d.name = v.Str()
	}
	if n := min(frames+natives, maxStackFrames); n > 0 {
		d.calls = make([]stackCall, 0, n)
	}
	globalThis := e.CreationRealm().globalThis
	j := natives - 1
	for i := frames - 1; len(d.calls) < maxStackFrames; i-- {
		// A native function called from the frame i runs inside it.
		for ; j >= 0 && vm.natives[j].frames > i && len(d.calls) < maxStackFrames; j-- {
			c := &vm.natives[j]
			if !c.showsFrame() {
				continue
			}
			d.calls = append(d.calls, stackCall{
				name:  c.f.fn.nativeName(),
				recv:  vm.receiverName(globalThis, c.newTarget != nil, c.receiver),
				isNew: c.newTarget != nil,
			})
		}
		if i < 0 || len(d.calls) >= maxStackFrames {
			break
		}
		fr := &vm.frames[i]
		c := stackCall{
			fp:    fr.fp,
			name:  fr.fp.name,
			pc:    fr.pc,
			isNew: fr.newTarget != nil,
		}
		if fr.fp.IsEval {
			c.name = "eval"
		} else {
			c.recv = vm.receiverName(globalThis, c.isNew, fr.this)
		}
		d.calls = append(d.calls, c)
	}
	return d
}

// receiverName returns the type name that a stack trace shows for a call with this, or an
// empty string when the call shows no receiver.
func (vm *VM) receiverName(globalThis *Object, isNew bool, this Value) wtf8.String {
	if isNew || this.IsObject() && this.o == globalThis || this.IsNullish() {
		return ""
	}
	return vm.typeName(this)
}

// stackCall is a line of a stack trace.
type stackCall struct {
	// fp is the function that runs, or nil for a native function.
	fp *funcProto

	// name is the name of the function.
	name wtf8.String

	// recv is the type name of the receiver of a method call, or empty.
	recv wtf8.String

	// pc is past the instruction of fp that is running.
	pc int

	isNew bool
}

const stackFramePrefix = "\n    at "

// formatStack returns a stack trace starting with the WTF-8 name and UTF-8 message,
// followed by one line per call.
func formatStack(name wtf8.String, msg string, calls []stackCall) wtf8.String {
	// ASCII punctuation separates the WTF-8 fields, so surrogate pairs cannot span writes.
	var b strings.Builder
	b.Grow(len(calls) * len(stackFramePrefix))
	b.WriteString(string(name))
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(string(wtf8.FromValidUTF8(msg)))
	}
	for i := range calls {
		calls[i].write(&b)
	}
	return clip(wtf8.String(b.String()), MaxStringLength, "")
}

// maxFrameName is the length in bytes up to which a stack trace shows a name.
const maxFrameName = 1 << 10

// clip returns s, or its longest prefix that ends at a character boundary and leaves room for
// the suffix within max bytes, followed by the suffix.
func clip[S ~string](s S, max int, suffix S) S {
	if len(s) <= max {
		return s
	}
	i := max - len(suffix)
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + suffix
}

// write writes the line of the stack trace for c to b.
func (c *stackCall) write(b *strings.Builder) {
	b.WriteString(stackFramePrefix)
	name := clip(c.name, maxFrameName, "...")
	switch {
	case c.isNew:
		b.WriteString("new ")
		name = cmp.Or(name, "<anonymous>")
	case c.recv != "":
		b.WriteString(string(clip(c.recv, maxFrameName, "...")))
		b.WriteByte('.')
		name = cmp.Or(name, "<anonymous>")
	}
	if name != "" {
		b.WriteString(string(name))
		if c.fp != nil && c.fp.File == "" {
			return
		}
		b.WriteString(" (")
	}
	if fp := c.fp; fp != nil && fp.File != "" {
		b.WriteString(string(wtf8.FromValidUTF8(fp.File)))
		if line, column := fp.Position(max(c.pc-1, 0)); line > 0 {
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(line))
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(column))
		}
	} else {
		b.WriteString("<anonymous>")
	}
	if name != "" {
		b.WriteByte(')')
	}
}

// typeName returns the name of the type of v that a stack trace shows for a method called on v.
// It runs no JavaScript.
func (vm *VM) typeName(v Value) wtf8.String {
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
	o := v.o
	if v.IsCallable() {
		if n, ok := vm.lookupData(o, vm.Key(KeyName)); ok && n.IsString() && n.Str() != "" {
			return n.Str()
		}
		return "Function"
	}
	if c, ok := vm.lookupData(o, vm.Key(KeyConstructor)); ok && c.IsCallable() {
		if n, ok := vm.lookupData(c.o, vm.Key(KeyName)); ok && n.IsString() && n.Str() != "" && n.Str() != "Object" {
			return n.Str()
		}
	}
	if t, ok := vm.lookupData(o, vm.WellKnownSymbol(SymbolToStringTag)); ok && t.IsString() && t.Str() != "" {
		return t.Str()
	}
	return "Object"
}

// lookupData returns the data property key of o or its prototypes, without running any JavaScript.
func (vm *VM) lookupData(o *Object, key *Object) (Value, bool) {
	h, p, ok := find(o, key)
	if !ok || p.flags&FlagAccessor != 0 {
		return Undefined, false
	}
	return h.slots[p.slot], true
}

// takeException converts the pending exception to a Go error.
func (vm *VM) takeException() error {
	v := vm.thrown
	vm.thrown = Undefined
	return vm.errorOf(v)
}

// errorOf returns the Go error for the thrown value v.
func (vm *VM) errorOf(v Value) *Error {
	var msg string
	if v.IsObject() {
		if s, ok := vm.lookupData(v.o, vm.Key(KeyStack)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else if s, ok := ErrorStack(v); ok {
			msg = wtf8.ToValidUTF8(s)
		} else if s, ok := vm.lookupData(v.o, vm.Key(KeyMessage)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else {
			msg = "uncaught exception"
		}
	} else {
		msg = "uncaught " + vm.DisplayString(v)
	}
	return &Error{
		Value: v,
		msg:   msg,
	}
}

// NewError returns a new error object of the given type in r with the message msg, which is UTF-8.
func (cx *ExecutionContext) NewError(kind ErrorKind, msg string) Value {
	return Value{o: cx.newError(kind, msg)}
}

// maxDisplayString is the length in bytes up to which a message shows a string.
const maxDisplayString = 1 << 10

// DisplayString describes v for messages, without running any JavaScript. A long string is
// shortened.
func (vm *VM) DisplayString(v Value) string {
	o := v.o
	if o == nil {
		return "undefined"
	}
	if o == numberObject {
		return string(numconv.NumberToString(v.n))
	}
	switch o.kind {
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case kindString:
		return wtf8.ToValidUTF8(clip(v.Str(), maxDisplayString, "..."))
	case kindSymbol:
		return "Symbol(" + wtf8.ToValidUTF8(o.str) + ")"
	case kindBigInt:
		return displayBigInt(v)
	case kindArray:
		return "[object Array]"
	}
	if o.fn != nil {
		return "[object Function]"
	}
	return "[object Object]"
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
func (vm *VM) unwindTo(n int) {
	if len(vm.frames) <= n {
		return
	}
	if len(vm.openUpvals) > 0 {
		vm.closeUpvals(vm.frames[n].base)
	}
	for i := n; i < len(vm.frames); i++ {
		vm.frames[i] = frame{}
	}
	vm.frames = vm.frames[:n]
}
