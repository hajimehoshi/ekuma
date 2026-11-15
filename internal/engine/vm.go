// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"errors"
	"math/rand/v2"
	"sync/atomic"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NativeFunc is a function implemented in Go.
//
// args is only valid until the function returns. An exception is thrown with the Throw methods of
// [VM], which do not return.
type NativeFunc func(vm *VM, this Value, args []Value) Value

// function is the state of a callable object.
type function struct {
	// code is the compiled code of a function written in JavaScript.
	code *funcProto

	// upvals are the variables of enclosing functions that code refers to.
	upvals []*upvalue

	native NativeFunc

	// info describes a function created by [VM.NewNative], and is nil otherwise.
	info *nativeInfo

	bound *boundFunc

	// home is the object that super property accesses in a method start from.
	home *Object

	// fields are the elements that a class constructor adds to every instance, or nil when
	// there are none.
	fields *[]classElement

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
	name   string
	length int32

	// step is set for the next method of a built-in iterator. It is what the function does
	// without creating the iterator result.
	step IteratorStepFunc

	// realm is the [[Realm]] of the function.
	realm *realm
}

// nativeName returns the initial value of the property name of the native function f.
func (f *function) nativeName() string {
	if f.info == nil {
		return ""
	}
	return f.info.name
}

// iteratorStep returns the step function of the next method f of a built-in iterator, or nil.
func (f *function) iteratorStep() IteratorStepFunc {
	if f.info == nil {
		return nil
	}
	return f.info.step
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

// interruptSignal is what an interruption unwinds Go frames with. No JavaScript code can catch it.
type interruptSignal struct {
	// cause is the cause of the interruption.
	cause error
}

// ErrInterrupted is the error of a script that was stopped by [VM.Interrupt].
var ErrInterrupted = errors.New("ekuma: interrupted")

// Interrupt makes the running script stop with [ErrInterrupted] as soon as it reaches a loop
// or a call. It may be called from any goroutine. The request stays pending until a script
// observes it, so it also stops the next script when none is running. The module evaluations
// that the interruption ends are rejected with an Error whose message is that of
// [ErrInterrupted] and that [GoError] maps to cause, or to [ErrInterrupted] when cause is nil.
func (vm *VM) Interrupt(cause error) {
	if cause == nil {
		cause = ErrInterrupted
	}
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
// other agents, which raise the interrupt flag too.
func (vm *VM) Poll() {
	vm.pollTick()
	if vm.interrupt.Load() {
		vm.interrupt.Store(false)
		vm.receiveNotifications()
		if c := vm.interruptCause.Swap(nil); c != nil {
			panic(&interruptSignal{
				cause: *c,
			})
		}
	}
}

// throwSignal is what a thrown JavaScript exception unwinds Go frames with.
// The exception itself is VM.thrown.
type throwSignal struct{}

var errThrow = &throwSignal{}

// nativeCall is a running call of a native function, which stack traces show.
type nativeCall struct {
	f    *Object
	this Value

	// frames is the number of frames when the call started.
	frames int

	isNew bool
}

// pushNative records the start of the call of the native function f.
func (vm *VM) pushNative(f *Object, this Value, isNew bool) {
	vm.natives = append(vm.natives, nativeCall{
		f:      f,
		this:   this,
		frames: len(vm.frames),
		isNew:  isNew,
	})
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
	// hostDefined is the value that [VM.HostDefined] returns.
	hostDefined any

	stack  []Value
	frames []frame

	// natives are the calls of native functions that are running, innermost last.
	natives []nativeCall

	// openUpvals are the upvalues that refer to stack slots, ordered by index.
	openUpvals []*upvalue

	// sp is the first stack index that is not in use by the running function.
	sp int

	// depth is the number of interpreter loops that are running.
	depth int

	interrupt interruptFlag

	// interruptCause is the cause of the pending interruption.
	interruptCause atomic.Pointer[error]

	// entered is the number of calls from Go code outside the engine that are running.
	entered int

	// newTarget is the constructor a native function is being constructed for, and nil in a plain call.
	newTarget *Object

	thrown Value

	keys keys

	strings     stringTable
	emptyString Value

	// missHolder stands in as the holder of a property that was not found.
	missHolder *Object

	// protoEpoch changes whenever the properties or the prototype of a prototype object change.
	protoEpoch uint32

	// globalEpoch changes whenever a global lexical binding is declared.
	globalEpoch uint32

	sparseIndices sparseIndexCache

	jobs jobQueue

	// rejections are the unhandled rejections, for HostPromiseRejectionTracker.
	rejections rejectionTracker

	// timedWaiters are the waiters of Atomics.waitAsync with a finite timeout.
	timedWaiters timedWaiters

	// asyncWaiters is the number of waiters of Atomics.waitAsync whose promise is not resolved.
	asyncWaiters int

	// mailbox receives the notifications of other agents.
	mailbox *mailbox

	// canSuspend is AgentCanSuspend.
	canSuspend bool

	weak weakState

	// symbolRegistry is the GlobalSymbolRegistry List, by key.
	symbolRegistry map[string]*Object

	// regExps caches compiled patterns by their flags and source.
	regExps map[string]*regexp.Regexp

	// realm is the state of the current realm, whose record is curRealm. The record of the
	// current realm is stale: the state lives here until another realm becomes current.
	realm

	curRealm *realm

	// callerRealm is the realm that was current when the innermost interpreter loop started.
	callerRealm *realm

	// timeZone is the time zone of local time, or nil for the one of the host.
	timeZone datetime.TimeZone

	// randomSource gives the numbers of Math.random, or is nil for the global source of
	// math/rand/v2.
	randomSource rand.Source

	modules moduleHost

	builtins builtinLayout
}

// realm is a realm: the intrinsic objects, the global environment and what is derived from them.
type realm struct {
	intrinsics [intrinsicCount]*Object

	// asyncFromSyncProto is %AsyncFromSyncIteratorPrototype%, or nil until it is needed.
	asyncFromSyncProto *Object

	global *Object

	// globalLex holds the top-level let, const and class declarations.
	globalLex *Object

	shapes shapeTable

	arrayIter arrayIterCache

	regExpOriginals regExpOriginals

	// regExpBorrowers are the functions marked by [VM.LendRegExpLiterals], and
	// regExpReceiverBorrowers those marked by [VM.LendRegExpLiteralReceivers].
	regExpBorrowers, regExpReceiverBorrowers []*Object

	// evals caches the compiled code of eval.
	evals evalCache

	// referrer is the referrer of the code of the realm that has no ScriptOrModule.
	referrer *scriptOrModule
}

// Realm is a realm other than the one a [VM] is created with.
type Realm realm

// switchRealm makes r the current realm.
//
//go:noinline
func (vm *VM) switchRealm(r *realm) {
	// A switch is rare. Out of line, the copy does not grow the interpreter loop.
	*vm.curRealm = vm.realm
	vm.realm = *r
	vm.curRealm = r
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
	IntrinsicPromise
	IntrinsicArray
	IntrinsicObject
	IntrinsicFunction
	IntrinsicRegExp

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

// Intrinsic returns the intrinsic object id, or nil before it is set.
func (vm *VM) Intrinsic(id Intrinsic) *Object {
	return vm.intrinsics[id]
}

// SetIntrinsic makes o the intrinsic object id.
func (vm *VM) SetIntrinsic(id Intrinsic, o *Object) {
	vm.intrinsics[id] = o
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
	vm.thrown = v
	panic(errThrow)
}

// newError returns a new error object of the given type.
func (vm *VM) newError(kind ErrorKind, msg string) *Object {
	e := vm.NewObject(vm.Intrinsic(ErrorPrototypeIntrinsic(kind)))
	e.class = classError
	vm.Define(e, vm.Key(KeyMessage), vm.NewString(wtf8.FromValidUTF8(msg)), FlagsHidden)
	e.ext = &errorData{
		stack: vm.stackTrace(e, msg, len(vm.frames), len(vm.natives)),
	}
	return e
}

func (vm *VM) throwError(kind ErrorKind, msg string) {
	vm.Throw(Value{o: vm.newError(kind, msg)})
}

// ThrowTypeError throws a TypeError with the given message. It does not return.
func (vm *VM) ThrowTypeError(msg string) {
	vm.throwError(ErrorKindType, msg)
}

// ThrowRangeError throws a RangeError with the given message. It does not return.
func (vm *VM) ThrowRangeError(msg string) {
	vm.throwError(ErrorKindRange, msg)
}

// ThrowReferenceError throws a ReferenceError with the given message. It does not return.
func (vm *VM) ThrowReferenceError(msg string) {
	vm.throwError(ErrorKindReference, msg)
}

func (vm *VM) throwSyntaxError(msg string) {
	vm.throwError(ErrorKindSyntax, msg)
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
	e.ext = &errorData{
		stack: vm.stackTrace(e, msg, frames, natives),
	}
}

// maxStackFrames is the number of calls that a stack trace shows.
const maxStackFrames = 16

// stackTrace returns the stack trace of a new error, which shows the first frames frames and
// the first natives calls of native functions.
func (vm *VM) stackTrace(e *Object, msg string, frames, natives int) string {
	var b wtf8.Builder
	name := "Error"
	if v, ok := vm.lookupData(e, vm.Key(KeyName)); ok && v.IsString() {
		name = v.o.str
	}
	b.WriteString(name)
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(wtf8.FromValidUTF8(msg))
	}
	n := 0
	j := natives - 1
	for i := frames - 1; n < maxStackFrames; i-- {
		// A native function called from the frame i runs inside it.
		for ; j >= 0 && vm.natives[j].frames > i && n < maxStackFrames; j-- {
			c := &vm.natives[j]
			if c.f.class == classProxy {
				// Calling a proxy calls its target or its trap, which the trace shows instead.
				continue
			}
			vm.writeCall(&b, c.f.fn.nativeName(), c.isNew, c.this, "<anonymous>")
			n++
		}
		if i < 0 || n >= maxStackFrames {
			break
		}
		vm.writeFrame(&b, &vm.frames[i])
		n++
	}
	return b.String()
}

// writeFrame writes the line of the stack trace for the frame fr to b.
func (vm *VM) writeFrame(b *wtf8.Builder, fr *frame) {
	fp := fr.fp
	name := fp.name
	if fp.IsEval {
		name = "eval"
	}
	var loc string
	if fp.File != "" {
		loc = wtf8.FromValidUTF8(fp.File)
		// pc is past the instruction that is running.
		if line, column := fp.Position(max(fr.pc-1, 0)); line > 0 {
			loc += ":" + itoa(line) + ":" + itoa(column)
		}
	}
	this := fr.this
	if fp.IsEval {
		this = Undefined
	}
	vm.writeCall(b, name, fr.newTarget != nil, this, loc)
}

// writeCall writes the line of the stack trace for a call of the function name with this to b.
// loc is where the function runs, or empty when it is unknown.
func (vm *VM) writeCall(b *wtf8.Builder, name string, isNew bool, this Value, loc string) {
	b.WriteString("\n    at ")
	desc := name
	switch {
	case isNew:
		desc = "new " + cmp.Or(name, "<anonymous>")
	case this.IsObject() && this.o == vm.global, this.IsNullish():
	default:
		desc = vm.typeName(this) + "." + cmp.Or(name, "<anonymous>")
	}
	switch {
	case desc == "":
		b.WriteString(cmp.Or(loc, "<anonymous>"))
	case loc == "":
		b.WriteString(desc)
	default:
		b.WriteString(desc)
		b.WriteString(" (")
		b.WriteString(loc)
		b.WriteString(")")
	}
}

// typeName returns the name of the type of v that a stack trace shows for a method called on v.
// It runs no JavaScript.
func (vm *VM) typeName(v Value) string {
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
		if n, ok := vm.lookupData(o, vm.Key(KeyName)); ok && n.IsString() && n.o.str != "" {
			return n.o.str
		}
		return "Function"
	}
	if c, ok := vm.lookupData(o, vm.Key(KeyConstructor)); ok && c.IsCallable() {
		if n, ok := vm.lookupData(c.o, vm.Key(KeyName)); ok && n.IsString() && n.o.str != "" && n.o.str != "Object" {
			return n.o.str
		}
	}
	if t, ok := vm.lookupData(o, vm.WellKnownSymbol(SymbolToStringTag)); ok && t.IsString() && t.o.str != "" {
		return t.o.str
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
	msg := ""
	if v.IsObject() {
		if s, ok := vm.lookupData(v.o, vm.Key(KeyStack)); ok && s.IsString() {
			msg = s.o.str
		} else if s, ok := ErrorStack(v); ok {
			msg = s
		} else if s, ok := vm.lookupData(v.o, vm.Key(KeyMessage)); ok && s.IsString() {
			msg = s.o.str
		} else {
			msg = "uncaught exception"
		}
	} else {
		msg = "uncaught " + wtf8.ToValidUTF8(vm.DisplayString(v))
	}
	return &Error{
		Value: v,
		msg:   wtf8.ToValidUTF8(msg),
	}
}

// NewError returns a new error object of the given type with the message msg, which is UTF-8.
func (vm *VM) NewError(kind ErrorKind, msg string) Value {
	return Value{o: vm.newError(kind, msg)}
}

// DisplayString describes v for messages, without running any JavaScript.
func (vm *VM) DisplayString(v Value) string {
	o := v.o
	if o == nil {
		return numconv.NumberToString(v.n)
	}
	switch o.kind {
	case kindUndefined:
		return "undefined"
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case kindString:
		return o.str
	case kindSymbol:
		return "Symbol(" + o.str + ")"
	case kindBigInt:
		return o.BigInt().String()
	case kindArray:
		return "[object Array]"
	}
	if o.fn != nil {
		return "[object Function]"
	}
	return "[object Object]"
}

// protect runs f and reports whether it completed without throwing.
// When it reports false, the exception is in vm.thrown.
func (vm *VM) protect(f func()) (ok bool) {
	frames := len(vm.frames)
	natives := len(vm.natives)
	sp := vm.sp
	depth := vm.depth
	curRealm := vm.curRealm
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			if vm.curRealm != curRealm {
				vm.switchRealm(curRealm)
			}
			vm.unwindTo(frames)
			vm.truncateNatives(natives)
			vm.sp = sp
			vm.depth = depth
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
