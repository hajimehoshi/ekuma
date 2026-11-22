// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"crypto/subtle"
	"errors"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// initialStackSize is the number of values that the stack of a VM has room for from the start.
// It is large enough for the Go runtime to allocate it on pages of its own, which the interpreter
// loop runs measurably faster with.
const initialStackSize = 1 << 12

// NewVM creates a [VM] without the built-in objects. hostDefined is what [VM.HostDefined] returns.
func NewVM(hostDefined any) *VM {
	vm := &VM{
		hostDefined:         hostDefined,
		stack:               make([]Value, initialStackSize),
		symbolRegistry:      map[wtf8.String]*Object{},
		thrown:              Undefined,
		mailbox:             newMailbox(),
		maxBigIntResultBits: MaxBigIntBits,
	}
	vm.symbolShape = vm.newShape(nil, nil)
	vm.curRealm = &realm{}
	vm.referrer = &scriptOrModule{realm: vm.curRealm}
	vm.initRealmPrototypes()

	vm.emptyString = Value{
		o: &Object{
			kind:  kindString,
			flags: objInterned | objKept,
			shape: vm.shapes.string,
		},
	}
	vm.strings.interned.entries = make(map[wtf8.String]sweptRef[Object], internedInitialSize)
	vm.strings.keepNew = true
	vm.strings.interned.put("", vm.emptyString.o, internedMinSweep, isKept)
	for i := range vm.strings.ascii {
		vm.strings.ascii[i] = ObjectValue(vm.Intern(wtf8.String(rune(i))))
	}
	vm.initKeys()
	for t, name := range typeofNames {
		vm.typeofs[t] = ObjectValue(vm.Intern(name))
	}
	vm.missHolder = (&propertyObject{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
		slots: []Value{Undefined},
	}).init()
	vm.initRealmGlobals()
	vm.strings.keepNew = false
	return vm
}

// initRealmPrototypes creates the prototypes of the current realm that the strings, the keys
// and the other objects of the VM depend on.
func (vm *VM) initRealmPrototypes() {
	vm.shapes.nullProto = vm.newShape(nil, vm.curRealm)
	vm.shapes.proxy = vm.newShape(nil, vm.curRealm)
	objectProto := (&propertyObject{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
	}).init()
	vm.intrinsics[IntrinsicObjectPrototype] = objectProto
	vm.shapes.object = vm.shapeFor(objectProto)
	functionProto := (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.object,
		fn: &function{
			native: func(vm *VM, this Value, args []Value) Value {
				return Undefined
			},
			info: &nativeInfo{
				realm: vm.curRealm,
			},
		},
	}).init()
	vm.intrinsics[IntrinsicFunctionPrototype] = functionProto
	vm.shapes.function = vm.functionShapeFor(functionProto)
	arrayProto := (&propertyObject{
		kind:  kindArray,
		class: classArray,
		shape: vm.objectRootShape(objectProto, vm.curRealm, classArray),
	}).init()
	vm.intrinsics[IntrinsicArrayPrototype] = arrayProto
	vm.shapes.array = vm.objectRootShape(arrayProto, vm.curRealm, classArray)
	stringProto := vm.NewObject(objectProto)
	vm.intrinsics[IntrinsicStringPrototype] = stringProto
	// A primitive string has its own length even when an ordinary object with the same
	// prototype inherits its length.
	vm.shapes.string = vm.newShape(stringProto, vm.curRealm)
	vm.shapes.shortStrings = new([shortstrings.MaxBytes + 1]Object)
	for i := range vm.shapes.shortStrings {
		vm.shapes.shortStrings[i] = Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   wtf8.String("\x00\x00\x00\x00\x00\x00\x00\x00")[:i],
		}
	}
}

// initRealmGlobals finishes the prototypes of the current realm and creates its global object.
func (vm *VM) initRealmGlobals() {
	stringProto := vm.intrinsics[IntrinsicStringPrototype]
	stringProto.class = classString
	stringProto.setExtState(vm.emptyString.o)
	vm.add(stringProto, vm.Key(KeyLength), Number(0), 0)
	vm.shapes.iterResult = vm.shapes.object.with(vm.Key(KeyValue), FlagsData).with(vm.Key(keyDone), FlagsData)
	vm.shapes.arguments = vm.shapes.object.with(vm.Key(KeyLength), FlagsHidden).with(vm.WellKnownSymbol(SymbolIterator), FlagsHidden)
	vm.global = vm.NewObject(vm.intrinsics[IntrinsicObjectPrototype])
	// Deleting a property can restore a shared shape and its cached global slot pointers.
	// Keep the slots in place until adding a property gives the global object a private shape.
	vm.global.slots = make([]Value, 0, privateShapeThreshold)
	vm.globalThis = vm.global
	vm.globalLex = vm.NewObject(nil)
}

// CreateRealm creates a realm whose built-in objects install creates while the realm is current.
func (vm *VM) CreateRealm(install func()) *Realm {
	saved := vm.curRealm
	r := &realm{}
	vm.switchRealm(r)
	vm.referrer = &scriptOrModule{realm: r}
	vm.initRealmPrototypes()
	vm.initRealmGlobals()
	install()
	vm.switchRealm(saved)
	return (*Realm)(r)
}

// realmIntrinsic returns the intrinsic id of the realm r.
func (vm *VM) realmIntrinsic(r *realm, id Intrinsic) *Object {
	if r == vm.curRealm {
		return vm.intrinsics[id]
	}
	return r.intrinsics[id]
}

// IsIntrinsicOfOtherRealm reports whether the function f is the intrinsic id of
// GetFunctionRealm(f), and that realm is not the current one.
func (vm *VM) IsIntrinsicOfOtherRealm(f *Object, id Intrinsic) bool {
	r := vm.getFunctionRealm(f)
	return r != vm.curRealm && r.intrinsics[id] == f
}

// InRealm runs f with r as the current realm.
func (vm *VM) InRealm(r *Realm, f func()) {
	saved := vm.curRealm
	if saved == (*realm)(r) {
		f()
		return
	}
	vm.switchRealm((*realm)(r))
	defer vm.switchRealm(saved)
	f()
}

// NewSymbol returns a new symbol. desc is its description when hasDesc is set.
func (vm *VM) NewSymbol(desc wtf8.String, hasDesc bool) *Object {
	s := &Object{
		kind:  kindSymbol,
		shape: vm.symbolShape,
		str:   desc,
	}
	if hasDesc {
		s.setExtState(true)
	}
	return s
}

// RegisteredSymbol returns the symbol of the GlobalSymbolRegistry List with the key key,
// which it adds when there is none.
func (vm *VM) RegisteredSymbol(key wtf8.String) *Object {
	if s, ok := vm.symbolRegistry[key]; ok {
		return s
	}
	s := vm.NewSymbol(key, true)
	vm.symbolRegistry[key] = s
	return s
}

// KeyForSymbol implements KeyForSymbol: it returns the key of the symbol sym in the
// GlobalSymbolRegistry List, and reports whether sym is in it.
func (vm *VM) KeyForSymbol(sym *Object) (wtf8.String, bool) {
	if r, ok := vm.symbolRegistry[sym.str]; !ok || r != sym {
		return "", false
	}
	return sym.str, true
}

func (vm *VM) initKeys() {
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
		vm.keys.common[k] = vm.Intern(name)
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
		vm.keys.wellKnown[s] = vm.NewSymbol(name, true)
	}
}

// SyntaxError is a script that could not be parsed or compiled.
type SyntaxError struct {
	// File is the name of the script.
	File string

	// Line is the 1-based line of the error.
	Line int

	Message string
}

func (e *SyntaxError) Error() string {
	return "SyntaxError: " + e.messageWithPosition()
}

// messageWithPosition returns the message followed by the file and the line.
func (e *SyntaxError) messageWithPosition() string {
	return e.Message + " (" + e.File + ":" + itoa(e.Line) + ")"
}

// throwCompileError throws a SyntaxError for err, an error of the parser or the compiler for the source file.
func (vm *VM) throwCompileError(file string, err error) {
	if se, ok := errors.AsType[*SyntaxError](toSyntaxError(file, err)); ok {
		vm.throwSyntaxError(se.messageWithPosition())
	}
	vm.throwSyntaxError(err.Error())
}

func itoa(n int) string {
	return string(numconv.NumberToString(float64(n)))
}

// Script is a Script Record.
type Script struct {
	code *funcProto
}

// ParseScript implements ParseScript for the current realm. file names the script in error
// messages. The error is a [*SyntaxError] when src is not a valid script.
func (vm *VM) ParseScript(file string, src wtf8.String) (*Script, error) {
	fp, err := vm.compileScript(file, src)
	if err != nil {
		return nil, err
	}
	return &Script{
		code: fp,
	}, nil
}

// ScriptEvaluation evaluates s and runs pending jobs. completed reports whether
// the script returned normally before the job checkpoint.
func (vm *VM) ScriptEvaluation(s *Script) (Value, bool, error) {
	saved := vm.curRealm
	if r := s.code.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	var completed bool
	value, err := vm.enter(func() Value {
		v := vm.CallScript(s)
		completed = true
		return v
	})
	return value, completed, err
}

// CallScript evaluates s from a running host function and propagates its exception.
func (vm *VM) CallScript(s *Script) Value {
	saved := vm.curRealm
	if r := s.code.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	previous := vm.enteredRealm
	vm.enteredRealm = s.code.realm
	defer func() { vm.enteredRealm = previous }()
	return vm.Call(vm.newScriptFunction(s.code), Value{o: vm.globalThis})
}

// enter runs f as a call from Go and drains jobs at the outermost boundary, including
// after a JavaScript exception.
//
// An interruption or internal failure ends the outermost such call. When a host callback
// enters the engine again, the signal passes through that inner call and the Go code.
//
// A Go panic restores each boundary it escapes. The outermost boundary discards pending
// jobs and rejects unfinished module evaluations, including after a recovered inner panic.
func (vm *VM) enter(f func() Value) (result Value, err error) {
	if vm.entered > 0 {
		return vm.enterBoundary(f)
	}
	vm.enteredRealm = vm.curRealm
	vm.captureThrowSites = vm.exceptionReporter != nil
	defer func() {
		vm.enteredRealm = nil
		vm.captureThrowSites = false
		vm.thrownAt = throwSite{}
	}()
	// Data independent timing stays enabled for the goroutine across rescheduling, so the
	// nested calls need nothing more.
	subtle.WithDataIndependentTiming(func() {
		result, err = vm.enterBoundary(f)
	})
	return result, err
}

// enterBoundary implements [VM.enter].
func (vm *VM) enterBoundary(f func() Value) (result Value, err error) {
	// A host callback that recovered an interruption runs no more JavaScript.
	vm.ResumeInterruption()
	frames, natives, sp, depth := len(vm.frames), len(vm.natives), vm.sp, vm.depth
	curRealm, callerRealm := vm.curRealm, vm.callerRealm
	thrown, site := vm.thrown, vm.thrownAt
	context := vm.jobContext
	outermost := vm.entered == 0
	vm.entered++
	defer func() {
		vm.entered--
		if outermost {
			defer func() {
				if vm.abortCause != nil {
					cause := vm.abortCause
					vm.abortCause = nil
					if internal, ok := cause.(*xerrors.InternalError); ok {
						result, err = Undefined, internal
					}
					vm.abortModuleEvaluations(cause)
					vm.dropJobs()
				}
				vm.raised = nil
				vm.clearKeptObjects()
				vm.clearStale()
				vm.releaseStack()
			}()
		}
		if r := recover(); r != nil {
			sig, interrupted := r.(*interruptSignal)
			internal, failed := r.(*xerrors.InternalError)
			if interrupted && !outermost {
				panic(r)
			}
			vm.jobContext = context
			// Only activations belonging to this boundary have lost their Go callers.
			if vm.curRealm != curRealm {
				vm.switchRealm(curRealm)
			}
			for i := frames; i < len(vm.frames); i++ {
				if g := vm.frames[i].gen; g != nil {
					g.abort()
				}
			}
			vm.unwindTo(frames)
			vm.truncateNatives(natives)
			vm.sp, vm.depth = sp, depth
			vm.callerRealm = callerRealm
			vm.thrown, vm.thrownAt = thrown, site
			var cause error
			if interrupted {
				result, err = Undefined, &InterruptedError{
					Cause: sig.cause,
				}
				cause = sig.cause
				if cause == nil {
					cause = err
				}
			} else if failed {
				vm.abortCause = internal
				if !outermost {
					panic(r)
				}
				return
			} else {
				vm.abortCause = &PanicError{
					Value: r,
				}
				panic(r)
			}
			// The rejections of the modules queue jobs, which are dropped as well.
			vm.abortModuleEvaluations(cause)
			vm.dropJobs()
		}
	}()
	if !vm.protect(func() {
		ok := vm.protect(func() {
			result = f()
		})
		vm.ResumeInterruption()
		thrown := vm.takeThrown()
		site := vm.thrownAt
		if !ok && outermost {
			vm.reportUncaughtException(thrown, site)
		}
		vm.runJobs()
		if !ok {
			vm.thrown, vm.thrownAt = thrown, site
			panic(errThrow)
		}
	}) {
		return Undefined, vm.takeException()
	}
	return result, nil
}

func (vm *VM) reportUncaughtException(thrown Value, site throwSite) {
	if vm.exceptionReporter != nil {
		vm.protect(func() {
			vm.exceptionReporter(thrown, site.location())
		})
		vm.thrown = Undefined
	}
}

func (vm *VM) compileScript(file string, src wtf8.String) (*funcProto, error) {
	bc, err := compileScriptCode(file, src)
	if err != nil {
		return nil, err
	}
	return vm.link(bc, &scriptOrModule{
		file:  file,
		realm: vm.curRealm,
	}), nil
}

// compileScriptCode parses and compiles the script src. The error is a [*SyntaxError] when src is
// not a valid script.
func compileScriptCode(file string, src wtf8.String) (*bytecode.Func, error) {
	prog, err := parser.ParseScript(src, parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, err := compiler.CompileScript(prog, file)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	return bc, nil
}

// UnboundScript is a script parsed for no particular VM. Any number of VMs may bind it with
// [VM.BindScript], also at the same time.
type UnboundScript struct {
	file string
	code *bytecode.Func
}

// ParseUnboundScript parses src as the source text of a script for no particular VM. file names
// the script in error messages. The error is a [*SyntaxError] when src is not a valid script.
func ParseUnboundScript(file, src string) (*UnboundScript, error) {
	bc, err := compileScriptCode(file, wtf8.FromValidUTF8(src))
	if err != nil {
		return nil, err
	}
	return &UnboundScript{
		file: file,
		code: bc,
	}, nil
}

// BindScript returns the script of u for the current realm, as [VM.ParseScript] returns it for
// the source of u.
func (vm *VM) BindScript(u *UnboundScript) *Script {
	return &Script{
		code: vm.linkShared(u.code, &scriptOrModule{
			file:  u.file,
			realm: vm.curRealm,
		}),
	}
}

// compileFunction compiles the function of [parser.ParseFunction] into a script that evaluates
// to it.
func (vm *VM) compileFunction(file string, params, body wtf8.String, kind parser.FunctionKind) (*funcProto, error) {
	prog, err := parser.ParseFunction(params, body, kind, parser.Options{
		Poll: vm.Poll,
	})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, err := compiler.CompileFunction(prog, file, vm.Poll)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	// The function has the active ScriptOrModule of its creation.
	return vm.link(bc, vm.activeScriptOrModule()), nil
}

// toSyntaxError converts an error of the parser or the compiler for the source file to a
// [*SyntaxError].
func toSyntaxError(file string, err error) error {
	if pe, ok := errors.AsType[*parser.Error](err); ok {
		return &SyntaxError{
			File:    file,
			Line:    pe.Line,
			Message: pe.Message,
		}
	}
	if ce, ok := errors.AsType[*compiler.Error](err); ok {
		return &SyntaxError{
			File:    file,
			Line:    ce.Line,
			Message: ce.Message,
		}
	}
	return err
}

// newScriptFunction returns the function that runs the script fp.
func (vm *VM) newScriptFunction(fp *funcProto) *Object {
	return (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.function,
		fn:    &function{code: fp},
	}).init()
}

// PerformEval implements PerformEval for an indirect eval.
func (vm *VM) PerformEval(x Value) Value {
	if !x.IsString() {
		return x
	}
	return vm.Call(vm.newScriptFunction(vm.evalCode(nil, 0, x.Str())), Value{o: vm.globalThis})
}

// FunctionKind is the kind of function that CreateDynamicFunction creates.
type FunctionKind uint8

const (
	NormalFunction FunctionKind = iota
	GeneratorFunction
	AsyncFunction
	AsyncGeneratorFunction
)

// CreateDynamicFunction implements CreateDynamicFunction. ctor is the constructor being called,
// newTarget is nil in a plain call, and the last of args is the body.
func (vm *VM) CreateDynamicFunction(ctor, newTarget *Object, kind FunctionKind, args []Value) Value {
	if newTarget == nil {
		newTarget = ctor
	}
	var params, body wtf8.String
	if len(args) > 0 {
		var sb []byte
		for i, a := range args[:len(args)-1] {
			if i > 0 {
				sb = append(sb, ',')
			}
			p := vm.ToString(a)
			if len(sb)+len(p) > MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			sb = append(sb, p...)
		}
		params = wtf8.String(sb)
		body = vm.ToString(args[len(args)-1])
	}

	var pkind parser.FunctionKind
	fallback := IntrinsicFunctionPrototype
	switch kind {
	case GeneratorFunction:
		pkind = parser.GeneratorFunction
		fallback = IntrinsicGeneratorFunctionPrototype
	case AsyncFunction:
		pkind = parser.AsyncFunction
		fallback = IntrinsicAsyncFunctionPrototype
	case AsyncGeneratorFunction:
		pkind = parser.AsyncGeneratorFunction
		fallback = IntrinsicAsyncGeneratorFunctionPrototype
	}
	fp, err := vm.compileFunction("<anonymous>", params, body, pkind)
	if err != nil {
		vm.throwCompileError("<anonymous>", err)
	}
	proto := vm.GetPrototypeFromConstructor(newTarget, fallback)
	f := vm.Call(vm.newScriptFunction(fp), Value{o: vm.globalThis})
	if proto != vm.Intrinsic(fallback) {
		vm.SetPrototypeOf(f.o, proto)
	}
	return f
}

// CurrentRealm returns the current Realm Record.
func (vm *VM) CurrentRealm() *Realm {
	return (*Realm)(vm.curRealm)
}

// CreationRealm returns the creation realm of the JavaScript object o, or nil for a detached global proxy.
func (o *Object) CreationRealm() *Realm {
	if o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.isGlobal() {
			if st.target == nil {
				return nil
			}
			o = st.target
		}
	}
	return (*Realm)(o.shape.realm)
}

// IsEnteredRealm reports whether r is the VM's entered realm.
// Promise jobs record an entered realm only in VMs with global proxies.
func (vm *VM) IsEnteredRealm(r *Realm) bool {
	return r != nil && (*realm)(r) == vm.enteredRealm
}

// SetRealmHostDefined sets the [[HostDefined]] field of the realm r to v.
func (vm *VM) SetRealmHostDefined(r *Realm, v any) {
	if (*realm)(r) == vm.curRealm {
		vm.realm.hostDefined = v
		return
	}
	r.hostDefined = v
}

// RealmHostDefined returns the [[HostDefined]] field of the realm r.
func (vm *VM) RealmHostDefined(r *Realm) any {
	if (*realm)(r) == vm.curRealm {
		return vm.realm.hostDefined
	}
	return r.hostDefined
}

// SetRealmExtension sets the state of the built-in extension of the current realm to v.
func (vm *VM) SetRealmExtension(v any) {
	vm.realm.extension = v
}

// RealmExtension returns the state of the built-in extension of the realm r.
func (vm *VM) RealmExtension(r *Realm) any {
	if (*realm)(r) == vm.curRealm {
		return vm.realm.extension
	}
	return r.extension
}
