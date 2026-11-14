// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NativeFunc is a function implemented in Go.
//
// args is only valid until the function returns. An exception is thrown with the Throw methods of VM,
// which do not return.
type NativeFunc func(vm *VM, this Value, args []Value) Value

// function is the state of a callable object.
type function struct {
	// code is the compiled code of a function written in JavaScript.
	code *funcProto

	// upvals are the variables of enclosing functions that code refers to.
	upvals []*upvalue

	native NativeFunc

	// step is set for the next method of a built-in iterator. It is what native does
	// without creating the iterator result.
	step IteratorStepFunc

	// name is the initial value of the property name of a native function.
	name string

	bound *boundFunc

	// home is the object that super property accesses in a method start from.
	home *Object

	// fields are the elements that a class constructor adds to every instance.
	fields []classElement

	// length is the initial value of the property length of a native function.
	length int32

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
	keyProxy
	keyRevoke
	keySource
	keyFlags
	keyDefault
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
type interruptSignal struct{}

var errInterrupt = &interruptSignal{}

// ErrInterrupted is the error of a script that was stopped by VM.Interrupt.
var ErrInterrupted = errors.New("ekuma: interrupted")

// Interrupt makes the running script stop with ErrInterrupted as soon as it reaches a loop
// or a call. It may be called from any goroutine. The request stays pending until a script
// observes it, so it also stops the next script when none is running.
func (vm *VM) Interrupt() {
	vm.interrupt.Store(true)
}

// ClearInterrupt withdraws a request of Interrupt that no script has observed.
func (vm *VM) ClearInterrupt() {
	vm.interrupt.Store(false)
}

// Poll stops the script when an interruption was requested.
func (vm *VM) Poll() {
	vm.pollTick()
	if vm.interrupt.Load() {
		vm.interrupt.Store(false)
		panic(errInterrupt)
	}
}

// throwSignal is what a thrown JavaScript exception unwinds Go frames with.
// The exception itself is VM.thrown.
type throwSignal struct{}

var errThrow = &throwSignal{}

// VM is a JavaScript virtual machine. It must not be used from more than one goroutine at a time.
type VM struct {
	// hostDefined is the value that HostDefined returns.
	hostDefined any

	stack  []Value
	frames []frame

	// openUpvals are the upvalues that refer to stack slots, ordered by index.
	openUpvals []*upvalue

	// sp is the first stack index that is not in use by the running function.
	sp int

	// depth is the number of interpreter loops that are running.
	depth int

	interrupt interruptFlag

	// entered is the number of calls from Go code outside the engine that are running.
	entered int

	// newTarget is the constructor a native function is being constructed for, and nil in a plain call.
	newTarget *Object

	thrown Value

	global *Object

	// globalLex holds the top-level let, const and class declarations.
	globalLex *Object

	keys keys

	shapes shapeTable

	strings     stringTable
	emptyString Value

	// missHolder stands in as the holder of a property that was not found.
	missHolder *Object

	// protoEpoch changes whenever the properties or the prototype of a prototype object change.
	protoEpoch uint32

	// globalEpoch changes whenever a global lexical binding is declared.
	globalEpoch uint32

	arrayIter     arrayIterCache
	sparseIndices sparseIndexCache

	jobs jobQueue

	// rejections are the unhandled rejections, for HostPromiseRejectionTracker.
	rejections rejectionTracker

	// timedWaiters are the waiters of Atomics.waitAsync with a finite timeout.
	timedWaiters timedWaiters

	weak weakState

	// symbolRegistry is the GlobalSymbolRegistry List, by key.
	symbolRegistry map[string]*Object

	// regExps caches compiled patterns by their flags and source.
	regExps map[string]*regexp.Regexp

	realm

	// timeZone is the time zone of local time, or nil for the one of the host.
	timeZone datetime.TimeZone

	// evals caches the compiled code of eval.
	evals evalCache
}

// realm is the set of intrinsic objects.
type realm struct {
	intrinsics [intrinsicCount]*Object

	// asyncFromSyncProto is %AsyncFromSyncIteratorPrototype%, or nil until it is needed.
	asyncFromSyncProto *Object
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
	IntrinsicPromise
	IntrinsicArray
	IntrinsicObject
	IntrinsicFunction
	IntrinsicRegExp

	// The prototypes of the native error types are in the order of ErrorKind.
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
	vm.RecordStack(e, msg)
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
// as what ErrorStack returns.
func (vm *VM) RecordStack(e *Object, msg string) {
	e.ext = vm.stackTrace(e, msg)
}

// stackTrace returns the stack trace of a new error.
func (vm *VM) stackTrace(e *Object, msg string) string {
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
	for i := len(vm.frames) - 1; i >= 0 && i >= len(vm.frames)-16; i-- {
		fp := vm.frames[i].fp
		b.WriteString("\n    at ")
		if fp.name == "" {
			b.WriteString("<anonymous>")
		} else {
			b.WriteString(fp.name)
		}
		if fp.File != "" {
			b.WriteString(" (")
			b.WriteString(wtf8.FromValidUTF8(fp.File))
			// pc is past the instruction that is running.
			if line, column := fp.Position(max(vm.frames[i].pc-1, 0)); line > 0 {
				b.WriteString(":")
				b.WriteString(itoa(line))
				b.WriteString(":")
				b.WriteString(itoa(column))
			}
			b.WriteString(")")
		}
	}
	return b.String()
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
	sp := vm.sp
	depth := vm.depth
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			vm.unwindTo(frames)
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
