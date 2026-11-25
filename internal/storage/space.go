// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"cmp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Function is the state of a callable object.
type Function struct {
	// Code is the compiled Code of a function written in JavaScript.
	Code *FuncProto

	// Upvals are the variables of enclosing functions that code refers to.
	Upvals []*Upvalue

	// Info describes a native function. It is nil for any other function.
	Info *NativeInfo

	Bound *BoundFunc

	// Home is the object that super property accesses in a method start from.
	Home *Object

	// Fields are the elements that a class constructor adds to every instance, or nil when
	// there are none.
	Fields *[]ClassElement

	// shared marks the initial state shared by closures of the same function.
	shared bool

	// Materialized is set once the initial properties, such as length and name, exist as slots.
	Materialized bool

	// Constructor says whether the function can be called with new.
	Constructor bool

	// Derived marks the constructor of a class with an extends clause.
	Derived bool

	// ClassConstructor marks a function that throws when called without new.
	ClassConstructor bool

	// Scopes points to the dynamic Scopes that were open where the function was created,
	// innermost last, or is nil when none were.
	Scopes *[]*Object
}

// NativeInfo is the description of a native function.
type NativeInfo struct {
	// Native is the Native function.
	Native Native

	// Name and length are the initial values of the properties Name and length.
	Name   wtf8.String
	Length int32

	// Extremum says that the function is Math.min or Math.max, which the interpreter computes
	// for two numbers.
	Extremum Extremum

	// Frameless says that calls of the function show no frame, as agent.SetFrameless describes.
	Frameless bool

	// Prototype is the intrinsic of realm that the property Prototype of the function holds,
	// when FixedPrototype is set. The property can then be neither changed nor deleted.
	Prototype      Intrinsic
	FixedPrototype bool

	// Realm is the [[Realm]] of the function, or nil for a proxy.
	Realm *RealmPart
}

// ProxyInfo describes every callable proxy. It must not be modified.
var ProxyInfo NativeInfo

// ChangeableInfo returns the description of the native function f, which must not be a proxy, for
// changing it.
func (f *Function) ChangeableInfo() *NativeInfo {
	if f.Info == &ProxyInfo {
		panic("engine: change of the description of a proxy")
	}
	return f.Info
}

// NativeName returns the initial value of the property name of the native function f.
func (f *Function) NativeName() wtf8.String {
	if f.Info == nil {
		return ""
	}
	return f.Info.Name
}

// Frameless reports whether f, which may be nil, is a native function whose calls show no frame.
func (f *Function) Frameless() bool {
	return f != nil && f.Info != nil && f.Info.Frameless
}

type BoundFunc struct {
	Target *Object
	This   Value
	Args   []Value
}

// Upvalue is a variable of an enclosing function. p points to the variable: into the registers
// of that function while it is running, and to val afterwards.
type Upvalue struct {
	P *Value

	// Index is the stack Index or, for a generator, the register that p points to while the upvalue is open.
	Index int
	Val   Value
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
	KeyValueOf
	KeyToJSON
	KeyCall
	KeyApply
	KeyBind
	KeyGet
	KeySet
	KeyValue
	KeyWritable
	KeyEnumerable
	KeyConfigurable
	KeyNext
	KeyDone
	KeyReturn
	KeyThrow
	KeyThen
	KeyCallee
	KeyCaller
	KeyArguments
	KeyIndex
	KeyInput
	KeyGroups
	KeyLastIndex
	KeyRaw
	KeySource
	KeyFlags
	KeyDefault
	KeyGetPrototypeOf
	KeySetPrototypeOf
	KeyIsExtensible
	KeyPreventExtensions
	KeyGetOwnPropertyDescriptor
	KeyDefineProperty
	KeyHas
	KeyDeleteProperty
	KeyOwnKeys
	KeyConstruct
	KeyAdd
	KeyKeys
	KeySize
	KeyStatus
	KeyReason
	KeyMode
	KeyPadding
	KeyToLocaleString
	KeyToISOString
	KeyHasIndices
	KeyGlobal
	KeyIgnoreCase
	KeyMultiline
	KeyDotAll
	KeyUnicode
	KeyUnicodeSets
	KeySticky
	KeyExec
	CommonKeyCount
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
	WellKnownSymbolCount
)

// keys are the property keys that the engine itself uses.
type keys struct {
	Common    [CommonKeyCount]*Object
	WellKnown [WellKnownSymbolCount]*Object
}

// Key returns the string property key k.
func (sp *Space) Key(k CommonKey) *Object {
	return sp.Keys.Common[k]
}

// WellKnownSymbol returns the well-known symbol s.
func (sp *Space) WellKnownSymbol(s WellKnownSymbol) *Object {
	return sp.Keys.WellKnown[s]
}

// InterruptSignal is what an interruption unwinds Go frames with. No JavaScript code can catch it.
type InterruptSignal struct {
	// Cause is the Cause of the interruption.
	Cause error
}

// Poll is agent.Poll without taking notifications. It leaves the flag set, so that the next
// Poll of the agent takes them.
func (p *Poller) Poll() {
	p.PollYield()
	p.ResumeInterruption()
	if p.InterruptFlag.Load() {
		p.Raise()
	}
}

// Raise panics with the pending interruption, if there is one.
func (p *Poller) Raise() {
	if c := p.InterruptCause.Swap(nil); c != nil {
		p.Raised = &InterruptSignal{
			Cause: *c,
		}
		panic(p.Raised)
	}
}

// ResumeInterruption panics again with an interruption that a host callback recovered before
// the interruption reached the outermost call from Go.
func (p *Poller) ResumeInterruption() {
	if p.Raised != nil {
		panic(p.Raised)
	}
}

// Poller is what a long operation needs to stop at an interruption.
type Poller struct {
	InterruptFlag interruptFlag

	// InterruptCause is the cause of the pending interruption.
	InterruptCause atomic.Pointer[error]

	// Raised is the interruption that is unwinding to the outermost call from Go.
	Raised *InterruptSignal
}

// Space is the state of an agent that its objects and values need: the property keys and the
// strings, the symbols, the shapes that belong to no realm, the epochs and IDs that the inline
// caches rely on, and the objects that WeakRefs keep alive. The objects of an agent lead to its
// space through their shapes. It also holds what operations on them need besides: the state of
// polling, the clock and the RegExp matcher.
//
// It must not refer to the agent, a realm, a frame or an execution context, except that a shape
// records its realm. hostDefined belongs to the host and is opaque to the engine.
type Space struct {
	// poller comes first, as the interpreter loop reads its flag often.
	Poller

	Keys keys

	Strings stringTable

	// MissHolder holds undefined in its one slot, where the cache entries of properties that
	// were not found point.
	MissHolder *Object

	// ProtoEpoch changes whenever the properties or the prototype of a prototype object change,
	// or its slots move.
	ProtoEpoch uint32

	lastReadShapeID uint32

	sparseIndices sparseIndexCache

	// IndexStrings holds the strings of the array indices below indexStringCount that were
	// made, by index, and nil for the others.
	IndexStrings []*Object

	// SymbolShape is the shape of the symbols of the agent. It records the space that they belong
	// to, and no lookup starts from it.
	SymbolShape *Shape

	// Typeofs are the interned results of the typeof operator, indexed by the typeof constants.
	Typeofs [TypeofCount]Value

	// matchResultShapes are the shapes of RegExp match results of the realm that made them last.
	matchResultShapes matchResultShapes

	Builtins builtinLayout

	// MaxBigIntResultBits is the bit limit for checked BigInt arithmetic results.
	// Tests lower it to exercise overflow with small operands.
	MaxBigIntResultBits int64

	// SymbolRegistry is the GlobalSymbolRegistry List, by key.
	SymbolRegistry map[wtf8.String]*Object

	// keptObjects is the [[KeptAlive]] list.
	keptObjects keptObjects

	// Clock gives the current time, or is nil for time.Now.
	Clock func() time.Time

	// RegExpMatcher matches regular expressions. Nothing that a match runs starts another one.
	RegExpMatcher regexp.Matcher

	// HostDefinedValue is the value that [Space.HostDefined] returns.
	HostDefinedValue any
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
	IntrinsicMapIteratorPrototype
	IntrinsicSetIteratorPrototype
	IntrinsicRegExpStringIteratorPrototype
	IntrinsicIteratorHelperPrototype
	IntrinsicWrapForValidIteratorPrototype
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
	IntrinsicIterator
	IntrinsicArrayBuffer
	IntrinsicSharedArrayBuffer
	IntrinsicAbstractModuleSource

	IntrinsicTemporal

	IntrinsicIntl
	IntrinsicIntlDateTimeFormat
	IntrinsicIntlNumberFormat
	IntrinsicIntlSegmentsPrototype
	IntrinsicIntlSegmentIteratorPrototype

	// The Temporal namespace initializes this range of prototypes.
	IntrinsicTemporalInstantPrototype
	IntrinsicTemporalDurationPrototype
	IntrinsicTemporalPlainDatePrototype
	IntrinsicTemporalPlainTimePrototype
	IntrinsicTemporalPlainDateTimePrototype
	IntrinsicTemporalPlainYearMonthPrototype
	IntrinsicTemporalPlainMonthDayPrototype
	IntrinsicTemporalZonedDateTimePrototype

	// The Intl namespace initializes this range of prototypes.
	IntrinsicIntlCollatorPrototype
	IntrinsicIntlDateTimeFormatPrototype
	IntrinsicIntlDisplayNamesPrototype
	IntrinsicIntlDurationFormatPrototype
	IntrinsicIntlListFormatPrototype
	IntrinsicIntlLocalePrototype
	IntrinsicIntlNumberFormatPrototype
	IntrinsicIntlPluralRulesPrototype
	IntrinsicIntlRelativeTimeFormatPrototype
	IntrinsicIntlSegmenterPrototype

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

	// The getters of the caller and arguments properties of non-strict ordinary functions.
	IntrinsicFunctionCallerGetter
	IntrinsicFunctionArgumentsGetter

	// The typed array constructors are in the order of [element.Type].
	IntrinsicInt8Array
	IntrinsicUint8Array
	IntrinsicUint8ClampedArray
	IntrinsicInt16Array
	IntrinsicUint16Array
	IntrinsicInt32Array
	IntrinsicUint32Array
	IntrinsicFloat16Array
	IntrinsicFloat32Array
	IntrinsicFloat64Array
	IntrinsicBigInt64Array
	IntrinsicBigUint64Array

	// The prototypes of the typed array constructors are in the order of [element.Type].
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

	// The initial values of these functions are what the built-ins treat specially.
	IntrinsicRegExpPrototypeExec
	IntrinsicMapPrototypeSet
	IntrinsicMapPrototypeEntries
	IntrinsicMapIteratorPrototypeNext
	IntrinsicSetPrototypeAdd
	IntrinsicSetPrototypeHas
	IntrinsicSetPrototypeValues
	IntrinsicSetIteratorPrototypeNext
	IntrinsicWeakMapPrototypeSet
	IntrinsicWeakSetPrototypeAdd

	// The functions that the engine calls.
	IntrinsicJSONParse

	intrinsicCount
)

// Intrinsic returns the intrinsic object id of the execution realm, or nil before it is set.
func (r *RealmPart) Intrinsic(id Intrinsic) *Object {
	return r.Intrinsics[id]
}

// Error is a JavaScript exception that reached Go.
type Error struct {
	// Value is the thrown value.
	Value Value

	msg string
}

func (e *Error) Error() string {
	return e.msg
}

// StackCall is a line of a stack trace.
type StackCall struct {
	// Fp is the function that runs, or nil for a native function.
	Fp *FuncProto

	// Name is the Name of the function.
	Name wtf8.String

	// Recv is the type name of the receiver of a method call, or empty.
	Recv wtf8.String

	// Pc is past the instruction of fp that is running.
	Pc int

	IsNew bool
}

const stackFramePrefix = "\n    at "

// formatStack returns a stack trace starting with the WTF-8 name and UTF-8 message,
// followed by one line per call.
func formatStack(name wtf8.String, msg string, calls []StackCall) wtf8.String {
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
	return Clip(wtf8.String(b.String()), MaxStringLength, "")
}

// maxFrameName is the length in bytes up to which a stack trace shows a name.
const maxFrameName = 1 << 10

// Clip returns s, or its longest prefix that ends at a character boundary and leaves room for
// the suffix within max bytes, followed by the suffix.
func Clip[S ~string](s S, max int, suffix S) S {
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
func (c *StackCall) write(b *strings.Builder) {
	b.WriteString(stackFramePrefix)
	name := Clip(c.Name, maxFrameName, "...")
	switch {
	case c.IsNew:
		b.WriteString("new ")
		name = cmp.Or(name, "<anonymous>")
	case c.Recv != "":
		b.WriteString(string(Clip(c.Recv, maxFrameName, "...")))
		b.WriteByte('.')
		name = cmp.Or(name, "<anonymous>")
	}
	if name != "" {
		b.WriteString(string(name))
		if c.Fp != nil && c.Fp.File == "" {
			return
		}
		b.WriteString(" (")
	}
	if fp := c.Fp; fp != nil && fp.File != "" {
		b.WriteString(string(wtf8.FromValidUTF8(fp.File)))
		if line, column := fp.Position(max(c.Pc-1, 0)); line > 0 {
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

// ErrorOf returns the Go error for the thrown value v.
func (sp *Space) ErrorOf(v Value) *Error {
	var msg string
	if v.IsObject() {
		if s, ok := DiagnosticDataProperty(v.O, sp.Key(KeyStack)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else if s, ok := ErrorStack(v); ok {
			msg = wtf8.ToValidUTF8(s)
		} else if s, ok := DiagnosticDataProperty(v.O, sp.Key(KeyMessage)); ok && s.IsString() {
			msg = wtf8.ToValidUTF8(s.Str())
		} else {
			msg = "uncaught exception"
		}
	} else {
		msg = "uncaught " + DisplayString(v)
	}
	return &Error{
		Value: v,
		msg:   msg,
	}
}

// maxDisplayString is the length in bytes up to which a message shows a string.
const maxDisplayString = 1 << 10

// DisplayString is agent.DisplayString.
func DisplayString(v Value) string {
	if ValidateExec {
		CheckValue(v)
	}
	o := v.O
	if o == nil {
		return "undefined"
	}
	if o == NumberObject {
		return string(numconv.NumberToString(v.N))
	}
	switch o.Kind {
	case KindNull:
		return "null"
	case KindFalse:
		return "false"
	case KindTrue:
		return "true"
	case KindString:
		return wtf8.ToValidUTF8(Clip(v.Str(), maxDisplayString, "..."))
	case KindSymbol:
		return "Symbol(" + wtf8.ToValidUTF8(o.Text) + ")"
	case KindBigInt:
		return displayBigInt(v)
	case KindArray:
		return "[object Array]"
	}
	if o.Fn != nil {
		return "[object Function]"
	}
	return "[object Object]"
}
