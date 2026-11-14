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

// New creates a VM with the standard built-in objects.
func New() *VM {
	vm := &VM{
		stack: make([]Value, 1<<14),
		shapes: shapeTable{
			byProto:         map[*Object]*shape{},
			functionByProto: map[*Object]*shape{},
		},
		strings: stringTable{
			interned: map[string]*Object{},
		},
		symbolRegistry: map[string]*Object{},
		thrown:         Undefined,
	}
	vm.shapes.nullProto = &shape{vm: vm}
	vm.ObjectProto = &Object{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
	}
	vm.shapes.object = vm.shapeFor(vm.ObjectProto)
	vm.FunctionProto = &Object{
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
	vm.shapes.function = vm.functionShapeFor(vm.FunctionProto)
	vm.ArrayProto = &Object{
		kind:  kindArray,
		class: classArray,
		shape: vm.shapes.object,
	}
	vm.shapes.array = vm.shapeFor(vm.ArrayProto)
	vm.StringProto = vm.NewObject(vm.ObjectProto)
	vm.shapes.string = vm.shapeFor(vm.StringProto)

	vm.EmptyString = Value{
		o: &Object{
			kind:  kindString,
			flags: objInterned,
			shape: vm.shapes.string,
		},
	}
	vm.strings.interned[""] = vm.EmptyString.o
	for i := range vm.strings.ascii {
		vm.strings.ascii[i] = Value{o: vm.Intern(string(rune(i)))}
	}
	vm.initKeys()
	vm.StringProto.class = classString
	vm.StringProto.ext = vm.EmptyString.o
	vm.add(vm.StringProto, vm.K.Length, Number(0), 0)

	vm.missHolder = &Object{
		kind:  kindObject,
		shape: vm.shapes.nullProto,
		slots: []Value{Undefined},
	}
	vm.shapes.iterResult = vm.shapes.object.with(vm.K.Value, FlagsData).with(vm.K.done, FlagsData)
	vm.shapes.arguments = vm.shapes.object.with(vm.K.Length, FlagsHidden).with(vm.K.SymIterator, FlagsHidden)

	vm.Global = vm.NewObject(vm.ObjectProto)
	vm.globalLex = vm.NewObject(nil)
	return vm
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
	k := &vm.K
	for _, e := range []struct {
		p    **Object
		name string
	}{
		{&k.Length, "length"}, {&k.Prototype, "prototype"}, {&k.Constructor, "constructor"}, {&k.Name, "name"},
		{&k.Message, "message"}, {&k.Stack, "stack"}, {&k.Cause, "cause"}, {&k.Errors, "errors"},
		{&k.Error, "error"}, {&k.Suppressed, "suppressed"},
		{&k.ToString, "toString"}, {&k.valueOf, "valueOf"}, {&k.toJSON, "toJSON"},
		{&k.call, "call"}, {&k.apply, "apply"}, {&k.bind, "bind"},
		{&k.Get, "get"}, {&k.Set, "set"}, {&k.Value, "value"}, {&k.Writable, "writable"},
		{&k.Enumerable, "enumerable"}, {&k.Configurable, "configurable"},
		{&k.next, "next"}, {&k.done, "done"}, {&k.ret, "return"}, {&k.throw, "throw"}, {&k.then, "then"},
		{&k.callee, "callee"}, {&k.caller, "caller"}, {&k.arguments, "arguments"},
		{&k.index, "index"}, {&k.input, "input"}, {&k.groups, "groups"}, {&k.lastIndex, "lastIndex"},
		{&k.Raw, "raw"}, {&k.proxy, "proxy"}, {&k.revoke, "revoke"}, {&k.source, "source"},
		{&k.flags, "flags"}, {&k.default_, "default"},
	} {
		*e.p = vm.Intern(e.name)
	}
	for _, e := range []struct {
		p    **Object
		name string
	}{
		{&k.SymIterator, "Symbol.iterator"}, {&k.SymAsyncIterator, "Symbol.asyncIterator"},
		{&k.SymHasInstance, "Symbol.hasInstance"}, {&k.SymToPrimitive, "Symbol.toPrimitive"},
		{&k.SymToStringTag, "Symbol.toStringTag"}, {&k.SymSpecies, "Symbol.species"},
		{&k.SymIsConcatSpreadable, "Symbol.isConcatSpreadable"}, {&k.SymUnscopables, "Symbol.unscopables"},
		{&k.SymMatch, "Symbol.match"}, {&k.SymMatchAll, "Symbol.matchAll"}, {&k.SymReplace, "Symbol.replace"},
		{&k.SymSearch, "Symbol.search"}, {&k.SymSplit, "Symbol.split"},
		{&k.SymDispose, "Symbol.dispose"}, {&k.SymAsyncDispose, "Symbol.asyncDispose"},
	} {
		*e.p = vm.NewSymbol(e.name, true)
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
	return "SyntaxError: " + e.Message + " (" + e.File + ":" + itoa(e.Line) + ")"
}

func itoa(n int) string {
	return numconv.NumberToString(float64(n))
}

// RunScript evaluates src as a script and returns the value of its last expression statement.
// file names the script in error messages.
//
// The error is a *SyntaxError when src is not a valid script, and an *Error when
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
			result = vm.Call(f, Value{o: vm.Global})
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
	frames, sp, depth := len(vm.frames), vm.sp, vm.depth
	outermost := vm.entered == 0
	vm.entered++
	defer func() {
		vm.entered--
		if outermost {
			// The outermost call into the engine ends the last job.
			vm.clearKeptObjects()
		}
		if r := recover(); r != nil {
			if r != any(errInterrupt) || !outermost {
				panic(r)
			}
			// The interruption left frames and generators behind in whatever state they were.
			vm.unwindTo(frames)
			vm.sp, vm.depth = sp, depth
			vm.thrown = Undefined
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
	return vm.link(bc), nil
}

// compileFunction compiles the function of parser.ParseFunction into a script that evaluates to it.
func (vm *VM) compileFunction(file, params, body string, kind parser.FunctionKind) (*funcProto, error) {
	prog, err := parser.ParseFunction(params, body, kind, parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, err := compiler.CompileFunction(prog, file)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	return vm.link(bc), nil
}

// toSyntaxError converts an error of the parser or the compiler for the source file to a *SyntaxError.
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
	fp, err := vm.compileScript("<eval>", x.o.str)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	return vm.Call(vm.newScriptFunction(fp), Value{o: vm.Global})
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
	fallback := vm.FunctionProto
	switch kind {
	case GeneratorFunction:
		pkind = parser.GeneratorFunction
		fallback = vm.GeneratorFunctionProto
	case AsyncFunction:
		pkind = parser.AsyncFunction
		fallback = vm.AsyncFunctionProto
	case AsyncGeneratorFunction:
		pkind = parser.AsyncGeneratorFunction
		fallback = vm.AsyncGeneratorFunctionProto
	}
	fp, err := vm.compileFunction("<anonymous>", wtf8.FromValidUTF8(params), wtf8.FromValidUTF8(body), pkind)
	if err != nil {
		vm.throwSyntaxError(err.Error())
	}
	proto := vm.GetPrototypeFromConstructor(newTarget, fallback)
	f := vm.Call(vm.newScriptFunction(fp), Value{o: vm.Global})
	if proto != fallback {
		vm.SetPrototypeOf(f.o, proto)
	}
	return f
}
