// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"
	"sync/atomic"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NativeFunc is a function implemented in Go.
//
// args is only valid until the function returns. An exception is thrown with the Throw methods of VM,
// which do not return.
type NativeFunc func(vm *VM, this Value, args []Value) Value

// function is the state of a callable object.
type Function struct {
	// code is the compiled code of a function written in JavaScript.
	Code *funcProto

	// upvals are the variables of enclosing functions that code refers to.
	upvals []*upvalue

	native NativeFunc

	// name and length are the initial values of the properties of a native function.
	Name   string
	length int

	Bound *BoundFunc

	// home is the object that super property accesses in a method start from.
	home *Object

	// fields are the elements that a class constructor adds to every instance.
	fields []classElement

	// materialized is set once the properties length, name and prototype exist as slots.
	Materialized bool

	// constructor says whether the function can be called with new.
	Constructor bool

	// derived marks the constructor of a class with an extends clause.
	derived bool

	// classConstructor marks a function that throws when called without new.
	classConstructor bool
}

type BoundFunc struct {
	Target *Object
	This   Value
	Args   []Value
}

// upvalue is a variable of an enclosing function. p points to the variable: into the registers
// of that function while it is running, and to val afterwards.
type upvalue struct {
	p *Value

	// index is the stack index or, for a generator, the register that p points to while the upvalue is open.
	index int
	val   Value
}

// keys are the property keys that the engine itself uses.
type keys struct {
	Length, Prototype, Constructor, Name, Message, Stack, Cause, Errors          *Object
	ToString, valueOf, toJSON, call, apply, bind                                 *Object
	Get, Set, Value, Writable, Enumerable, Configurable                          *Object
	next, done, ret, throw, then, callee, caller, arguments                      *Object
	index, input, groups, lastIndex, Raw, proxy, revoke, source, flags, default_ *Object

	SymIterator, SymAsyncIterator, SymHasInstance, SymToPrimitive, SymToStringTag *Object
	SymSpecies, SymIsConcatSpreadable, SymUnscopables                             *Object
	SymMatch, SymMatchAll, SymReplace, SymSearch, SymSplit                        *Object
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
	// Host is for the package that wraps the VM to find its own state.
	Host any

	stack  []Value
	frames []frame

	// openUpvals are the upvalues that refer to stack slots, ordered by index.
	openUpvals []*upvalue

	// sp is the first stack index that is not in use by the running function.
	sp int

	// depth is the number of interpreter loops that are running.
	depth int

	interrupt atomic.Bool

	// entered is the number of calls from Go code outside the engine that are running.
	entered int

	// newTarget is the constructor a native function is being constructed for, and nil in a plain call.
	NewTarget *Object

	thrown Value

	Global *Object

	// globalLex holds the top-level let, const and class declarations.
	globalLex *Object

	interned    map[string]*Object
	childShapes map[*Object]*Shape
	K           keys

	nullProtoShape  *Shape
	objectShape     *Shape
	ArrayShape      *Shape
	FunctionShape   *Shape
	stringShape     *Shape
	iterResultShape *Shape
	argumentsShape  *Shape

	EmptyString  Value
	asciiStrings [128]Value

	// missHolder stands in as the holder of a property that was not found.
	missHolder *Object

	// protoEpoch changes whenever the properties or the prototype of a prototype object change.
	protoEpoch uint32

	// globalEpoch changes whenever a global lexical binding is declared.
	globalEpoch uint32

	// arrayIterEpoch is the value of protoEpoch at which arrayIterPlain was computed.
	arrayIterEpoch   uint32
	arrayIterPlain   bool
	arrayIterChecked bool

	// fnCall, fnApply and fnEval are the original Function.prototype.call, Function.prototype.apply and eval.
	FnCall, FnApply, FnEval, FnHasInstance *Object

	// fnArrayValues and fnArrayIterNext are the original Array.prototype.values and the next method of array iterators.
	FnArrayValues, FnArrayIterNext, FnArraySpecies *Object

	// joining holds the arrays that Array.prototype.join is converting, to stop at cycles.
	Joining []*Object

	// jobs is the queue of pending promise jobs.
	jobs []func()

	SymbolRegistry map[string]*Object

	realm
}

// realm is the set of intrinsic objects.
type realm struct {
	ObjectProto, FunctionProto, ArrayProto, StringProto, NumberProto, BooleanProto *Object
	SymbolProto, BigintProto, ErrorProto, IteratorProto, AsyncIteratorProto        *Object
	ArrayIteratorProto, StringIteratorProto, GeneratorProto, AsyncGeneratorProto   *Object
	GeneratorFunctionProto, AsyncFunctionProto, AsyncGeneratorFunctionProto        *Object
	RegexpProto, PromiseProto, MapProto, SetObjectProto, DateProto                 *Object
	promiseCtor, ArrayCtor, ObjectCtor, FunctionCtor                               *Object

	// errorProtos are the prototypes of the native error types, by errorKind.
	ErrorProtos [ErrorKindCount]*Object

	ThrowTypeErrorFunc *Object
}

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
	e := vm.NewObject(vm.ErrorProtos[kind])
	e.Class = ClassError
	vm.Define(e, vm.K.Message, vm.NewString(wtf8.FromValidUTF8(msg)), FlagsHidden)
	vm.Define(e, vm.K.Stack, vm.NewString(vm.StackTrace(e, msg)), FlagsHidden)
	return e
}

func (vm *VM) throwError(kind ErrorKind, msg string) {
	vm.Throw(Value{O: vm.newError(kind, msg)})
}

// ThrowTypeError throws a TypeError with the given message. It does not return.
func (vm *VM) ThrowTypeError(msg string) {
	vm.throwError(ErrorKindType, msg)
}

// ThrowRangeError throws a RangeError with the given message. It does not return.
func (vm *VM) ThrowRangeError(msg string) {
	vm.throwError(ErrorKindRange, msg)
}

func (vm *VM) throwReferenceError(msg string) {
	vm.throwError(ErrorKindReference, msg)
}

func (vm *VM) ThrowSyntaxError(msg string) {
	vm.throwError(ErrorKindSyntax, msg)
}

// stackTrace returns the text of the stack property of a new error.
func (vm *VM) StackTrace(e *Object, msg string) string {
	var b wtf8.Builder
	name := "Error"
	if v, ok := vm.LookupData(e, vm.K.Name); ok && v.IsString() {
		name = v.O.Str
	}
	b.WriteString(name)
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(wtf8.FromValidUTF8(msg))
	}
	for i := len(vm.frames) - 1; i >= 0 && i >= len(vm.frames)-16; i-- {
		fp := vm.frames[i].fp
		b.WriteString("\n    at ")
		if fp.Name == "" {
			b.WriteString("<anonymous>")
		} else {
			b.WriteString(fp.Name)
		}
		if fp.File != "" {
			b.WriteString(" (")
			b.WriteString(wtf8.FromValidUTF8(fp.File))
			b.WriteString(")")
		}
	}
	return b.String()
}

// lookupData returns the data property key of o or its prototypes, without running any JavaScript.
func (vm *VM) LookupData(o *Object, key *Object) (Value, bool) {
	h, p, ok := Find(o, key)
	if !ok || p.Flags&FlagAccessor != 0 {
		return Undefined, false
	}
	return h.Slots[p.Slot], true
}

// takeException converts the pending exception to a Go error.
func (vm *VM) takeException() error {
	v := vm.thrown
	vm.thrown = Undefined
	return vm.ErrorOf(v)
}

// ErrorOf returns the Go error for the thrown value v.
func (vm *VM) ErrorOf(v Value) *Error {
	msg := ""
	if v.IsObject() {
		if s, ok := vm.LookupData(v.O, vm.K.Stack); ok && s.IsString() {
			msg = s.O.Str
		} else if s, ok := vm.LookupData(v.O, vm.K.Message); ok && s.IsString() {
			msg = s.O.Str
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
	return Value{O: vm.newError(kind, msg)}
}

// displayString describes v for messages, without running any JavaScript.
func (vm *VM) DisplayString(v Value) string {
	o := v.O
	if o == nil {
		return numconv.NumberToString(v.N)
	}
	switch o.Kind {
	case kindUndefined:
		return "undefined"
	case kindNull:
		return "null"
	case kindFalse:
		return "false"
	case kindTrue:
		return "true"
	case KindString:
		return o.Str
	case kindSymbol:
		return "Symbol(" + o.Str + ")"
	case kindBigInt:
		return o.BigInt().String()
	case KindArray:
		return "[object Array]"
	}
	if o.Fn != nil {
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
