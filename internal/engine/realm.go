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
		stack:          make([]Value, 1<<14),
		interned:       map[string]*Object{},
		childShapes:    map[*Object]*Shape{},
		SymbolRegistry: map[string]*Object{},
		thrown:         Undefined,
	}
	vm.nullProtoShape = &Shape{vm: vm}
	vm.ObjectProto = &Object{
		Kind:  kindObject,
		Shape: vm.nullProtoShape,
	}
	vm.objectShape = vm.ShapeFor(vm.ObjectProto)
	vm.FunctionProto = &Object{
		Kind:  KindFunction,
		Class: ClassFunction,
		Flags: ObjExotic,
		Shape: vm.objectShape,
		Fn: &Function{
			native: func(vm *VM, this Value, args []Value) Value {
				return Undefined
			},
		},
	}
	vm.FunctionShape = vm.ShapeFor(vm.FunctionProto)
	vm.ArrayProto = &Object{
		Kind:  KindArray,
		Class: classArray,
		Shape: vm.objectShape,
	}
	vm.ArrayShape = vm.ShapeFor(vm.ArrayProto)
	vm.StringProto = vm.NewObject(vm.ObjectProto)
	vm.stringShape = vm.ShapeFor(vm.StringProto)

	vm.EmptyString = Value{
		O: &Object{
			Kind:  KindString,
			Flags: objInterned,
			Shape: vm.stringShape,
		},
	}
	vm.interned[""] = vm.EmptyString.O
	for i := range vm.asciiStrings {
		vm.asciiStrings[i] = Value{O: vm.Intern(string(rune(i)))}
	}
	vm.initKeys()

	vm.missHolder = &Object{
		Kind:  kindObject,
		Shape: vm.nullProtoShape,
		Slots: []Value{Undefined},
	}
	vm.iterResultShape = vm.objectShape.with(vm.K.Value, FlagsData).with(vm.K.done, FlagsData)
	vm.argumentsShape = vm.objectShape.with(vm.K.Length, FlagsHidden).with(vm.K.SymIterator, FlagsHidden)

	vm.Global = vm.NewObject(vm.ObjectProto)
	vm.globalLex = vm.NewObject(nil)
	return vm
}

func (vm *VM) NewSymbol(desc string, hasDesc bool) *Object {
	s := &Object{
		Kind: kindSymbol,
		Str:  desc,
	}
	if hasDesc {
		s.Ext = true
	}
	return s
}

func (vm *VM) initKeys() {
	k := &vm.K
	for _, e := range []struct {
		p    **Object
		name string
	}{
		{&k.Length, "length"}, {&k.Prototype, "prototype"}, {&k.Constructor, "constructor"}, {&k.Name, "name"},
		{&k.Message, "message"}, {&k.Stack, "stack"}, {&k.Cause, "cause"}, {&k.Errors, "errors"},
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
	fp, err := vm.CompileScript(file, src)
	if err != nil {
		return Undefined, err
	}
	f := &Object{
		Kind:  KindFunction,
		Class: ClassFunction,
		Flags: ObjExotic,
		Shape: vm.FunctionShape,
		Fn:    &Function{Code: fp},
	}
	return vm.enter(func() Value {
		result := vm.Call(f, Value{O: vm.Global})
		vm.runJobs()
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
		if r := recover(); r != nil {
			if r != any(errInterrupt) || !outermost {
				panic(r)
			}
			// The interruption left frames and generators behind in whatever state they were.
			vm.unwindTo(frames)
			vm.sp, vm.depth = sp, depth
			vm.thrown = Undefined
			vm.jobs = nil
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

func (vm *VM) CompileScript(file, src string) (*funcProto, error) {
	prog, err := parser.ParseScript(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			return nil, &SyntaxError{
				File:    file,
				Line:    pe.Line,
				Message: pe.Message,
			}
		}
		return nil, err
	}
	bc, err := compiler.CompileScript(prog, file)
	if err != nil {
		var ce *compiler.Error
		if errors.As(err, &ce) {
			return nil, &SyntaxError{
				File:    file,
				Line:    ce.Line,
				Message: ce.Message,
			}
		}
		return nil, err
	}
	return vm.link(bc), nil
}

// runJobs runs the pending promise jobs until none is left.
func (vm *VM) runJobs() {
	for len(vm.jobs) > 0 {
		job := vm.jobs[0]
		vm.jobs[0] = nil
		vm.jobs = vm.jobs[1:]
		job()
	}
}
