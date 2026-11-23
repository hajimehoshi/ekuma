// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

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
func (vm *VM) throwCompileError(errorRealm *Realm, file string, err error) {
	if se, ok := errors.AsType[*SyntaxError](toSyntaxError(file, err)); ok {
		vm.ThrowSyntaxError(errorRealm, se.messageWithPosition())
	}
	vm.ThrowSyntaxError(errorRealm, err.Error())
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
	return vm.Call(vm.newScriptFunction(s.code), Value{o: vm.curRealm.globalThis})
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
		shape: vm.curRealm.shapes.function,
		fn:    &function{code: fp},
	}).init()
}
