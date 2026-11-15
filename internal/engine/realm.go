// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"

	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// New creates a [VM] with the standard built-in objects. hostDefined is what [VM.HostDefined]
// returns.
func New(hostDefined any) *VM {
	vm := &VM{
		hostDefined:    hostDefined,
		stack:          make([]Value, 1<<8),
		symbolRegistry: map[string]*Object{},
		thrown:         Undefined,
		mailbox:        newMailbox(),
	}
	vm.curRealm = &realm{}
	vm.initRealmPrototypes()

	vm.emptyString = Value{
		o: &Object{
			kind:  kindString,
			flags: objInterned,
			shape: vm.shapes.string,
		},
	}
	vm.strings.interned.entries = make(map[string]sweptRef[Object], internedInitialSize)
	vm.strings.interned.put("", vm.emptyString.o, internedMinSweep)
	for i := range vm.strings.ascii {
		vm.strings.ascii[i] = Value{o: vm.Intern(string(rune(i)))}
	}
	vm.initKeys()
	vm.missHolder = &Object{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
		slots: []Value{Undefined},
	}
	vm.initRealmGlobals()
	return vm
}

// initRealmPrototypes creates the prototypes of the current realm that the strings, the keys
// and the other objects of the VM depend on.
func (vm *VM) initRealmPrototypes() {
	vm.shapes.nullProto = vm.newShape(nil)
	vm.shapes.proxy = vm.newShape(nil)
	objectProto := &Object{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
	}
	vm.intrinsics[IntrinsicObjectPrototype] = objectProto
	vm.shapes.object = vm.shapeFor(objectProto)
	functionProto := &Object{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.object,
		fn: &function{
			native: func(vm *VM, this Value, args []Value) Value {
				return Undefined
			},
		},
	}
	vm.intrinsics[IntrinsicFunctionPrototype] = functionProto
	vm.shapes.function = vm.functionShapeFor(functionProto)
	arrayProto := &Object{
		kind:  kindArray,
		class: classArray,
		shape: vm.shapes.object,
	}
	vm.intrinsics[IntrinsicArrayPrototype] = arrayProto
	vm.shapes.array = vm.shapeFor(arrayProto)
	stringProto := vm.NewObject(objectProto)
	vm.intrinsics[IntrinsicStringPrototype] = stringProto
	vm.shapes.string = vm.shapeFor(stringProto)
}

// initRealmGlobals finishes the prototypes of the current realm and creates its global object.
func (vm *VM) initRealmGlobals() {
	stringProto := vm.intrinsics[IntrinsicStringPrototype]
	stringProto.class = classString
	stringProto.ext = vm.emptyString.o
	vm.add(stringProto, vm.Key(KeyLength), Number(0), 0)
	vm.shapes.iterResult = vm.shapes.object.with(vm.Key(KeyValue), FlagsData).with(vm.Key(keyDone), FlagsData)
	vm.shapes.arguments = vm.shapes.object.with(vm.Key(KeyLength), FlagsHidden).with(vm.WellKnownSymbol(SymbolIterator), FlagsHidden)
	vm.global = vm.NewObject(vm.intrinsics[IntrinsicObjectPrototype])
	vm.globalLex = vm.NewObject(nil)
}

// CreateRealm creates a realm whose built-in objects install creates while the realm is current.
func (vm *VM) CreateRealm(install func()) *Realm {
	saved := vm.curRealm
	r := &realm{}
	vm.switchRealm(r)
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
func (vm *VM) NewSymbol(desc string, hasDesc bool) *Object {
	s := &Object{
		kind: kindSymbol,
		str:  desc,
	}
	if hasDesc {
		s.ext = true
	}
	return s
}

// RegisteredSymbol returns the symbol of the GlobalSymbolRegistry List with the key key,
// which it adds when there is none.
func (vm *VM) RegisteredSymbol(key string) *Object {
	if s, ok := vm.symbolRegistry[key]; ok {
		return s
	}
	s := vm.NewSymbol(key, true)
	vm.symbolRegistry[key] = s
	return s
}

// KeyForSymbol implements KeyForSymbol: it returns the key of the symbol sym in the
// GlobalSymbolRegistry List, and reports whether sym is in it.
func (vm *VM) KeyForSymbol(sym *Object) (string, bool) {
	if r, ok := vm.symbolRegistry[sym.str]; !ok || r != sym {
		return "", false
	}
	return sym.str, true
}

func (vm *VM) initKeys() {
	for k, name := range [commonKeyCount]string{
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
	for s, name := range [wellKnownSymbolCount]string{
		SymbolIterator: "Symbol.iterator", SymbolAsyncIterator: "Symbol.asyncIterator",
		SymbolHasInstance: "Symbol.hasInstance", SymbolToPrimitive: "Symbol.toPrimitive",
		SymbolToStringTag: "Symbol.toStringTag", SymbolSpecies: "Symbol.species",
		SymbolIsConcatSpreadable: "Symbol.isConcatSpreadable", SymbolUnscopables: "Symbol.unscopables",
		SymbolMatch: "Symbol.match", SymbolMatchAll: "Symbol.matchAll", SymbolReplace: "Symbol.replace",
		SymbolSearch: "Symbol.search", SymbolSplit: "Symbol.split",
		SymbolDispose: "Symbol.dispose", SymbolAsyncDispose: "Symbol.asyncDispose",
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
	return numconv.NumberToString(float64(n))
}

// RunScript evaluates src as a script and returns the value of its last expression statement.
// file names the script in error messages.
//
// The error is a [*SyntaxError] when src is not a valid script, and an [*Error] when
// the script throws.
func (vm *VM) RunScript(file, src string) (Value, error) {
	fp, err := vm.compileScript(file, src)
	if err != nil {
		return Undefined, err
	}
	f := vm.newScriptFunction(fp)
	return vm.enter(func() Value {
		var result Value
		ok := vm.protect(func() {
			result = vm.Call(f, Value{o: vm.global})
		})
		// The jobs run after a script that throws too.
		e := vm.takeThrown()
		vm.runJobs()
		if !ok {
			vm.Throw(e)
		}
		return result
	})
}

// enter runs f, which executes JavaScript, as a call from Go code outside the engine.
//
// An interruption ends the outermost such call: when Go code that JavaScript called enters
// the engine again, the interruption passes through that inner call and the Go code.
func (vm *VM) enter(f func() Value) (result Value, err error) {
	frames, natives, sp, depth := len(vm.frames), len(vm.natives), vm.sp, vm.depth
	curRealm := vm.curRealm
	outermost := vm.entered == 0
	vm.entered++
	defer func() {
		vm.entered--
		if outermost {
			// The outermost call into the engine ends the last job.
			vm.clearKeptObjects()
		}
		if r := recover(); r != nil {
			sig, ok := r.(*interruptSignal)
			if !ok || !outermost {
				panic(r)
			}
			// The interruption left frames and generators behind in whatever state they were.
			if vm.curRealm != curRealm {
				vm.switchRealm(curRealm)
			}
			vm.unwindTo(frames)
			vm.truncateNatives(natives)
			vm.sp, vm.depth = sp, depth
			vm.thrown = Undefined
			// The rejections of the modules queue jobs, which are dropped as well.
			vm.abortModuleEvaluations(sig.cause)
			vm.dropJobs()
			result, err = Undefined, ErrInterrupted
		}
	}()
	if !vm.protect(func() {
		result = f()
	}) {
		return Undefined, vm.takeException()
	}
	return result, nil
}

func (vm *VM) compileScript(file, src string) (*funcProto, error) {
	prog, err := parser.ParseScript(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, err := compiler.CompileScript(prog, file)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	return vm.link(bc, &scriptOrModule{file: file}), nil
}

// compileFunction compiles the function of [parser.ParseFunction] into a script that evaluates
// to it.
func (vm *VM) compileFunction(file, params, body string, kind parser.FunctionKind) (*funcProto, error) {
	prog, err := parser.ParseFunction(params, body, kind, parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, err := compiler.CompileFunction(prog, file)
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
	return &Object{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.function,
		fn:    &function{code: fp},
	}
}

// PerformEval implements PerformEval for an indirect eval.
func (vm *VM) PerformEval(x Value) Value {
	if !x.IsString() {
		return x
	}
	return vm.Call(vm.newScriptFunction(vm.evalCode(nil, 0, x.o.str)), Value{o: vm.global})
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
	var params, body string
	if len(args) > 0 {
		var sb []byte
		for i, a := range args[:len(args)-1] {
			if i > 0 {
				sb = append(sb, ',')
			}
			sb = append(sb, vm.ToString(a)...)
		}
		params = string(sb)
		body = vm.ToString(args[len(args)-1])
	}

	var pkind parser.FunctionKind
	fallback := vm.Intrinsic(IntrinsicFunctionPrototype)
	switch kind {
	case GeneratorFunction:
		pkind = parser.GeneratorFunction
		fallback = vm.Intrinsic(IntrinsicGeneratorFunctionPrototype)
	case AsyncFunction:
		pkind = parser.AsyncFunction
		fallback = vm.Intrinsic(IntrinsicAsyncFunctionPrototype)
	case AsyncGeneratorFunction:
		pkind = parser.AsyncGeneratorFunction
		fallback = vm.Intrinsic(IntrinsicAsyncGeneratorFunctionPrototype)
	}
	fp, err := vm.compileFunction("<anonymous>", params, body, pkind)
	if err != nil {
		vm.throwCompileError("<anonymous>", err)
	}
	proto := vm.GetPrototypeFromConstructor(newTarget, fallback)
	f := vm.Call(vm.newScriptFunction(fp), Value{o: vm.global})
	if proto != fallback {
		vm.SetPrototypeOf(f.o, proto)
	}
	return f
}
