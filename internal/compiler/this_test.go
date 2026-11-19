// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// TestThisPropertyRead tests that a property read of this reads the register of this itself.
func TestThisPropertyRead(t *testing.T) {
	prog, err := parser.ParseScript(`function f() { return this.x; }`, parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := compiler.CompileScript(prog, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	f := findFunc(s, "f")
	if f == nil {
		t.Fatal("function f not found")
	}
	this, obj := int32(-1), int32(-2)
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpLoadThisSloppy:
			this = i.A
		case bytecode.OpGetProp:
			obj = i.B
		}
	}
	if obj != this {
		t.Errorf("object register of the read: got %d, want %d, the register of this", obj, this)
	}
}

// TestThisForwardedApply tests that f.apply(this, arguments) passes the register of this itself.
func TestThisForwardedApply(t *testing.T) {
	f := compileF(t, `function f(a) { g.apply(this, arguments); }`)
	this, passed := int32(-1), int32(-2)
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpLoadThisSloppy:
			this = i.A
		case bytecode.OpApplyFwd:
			passed = i.C
		}
	}
	if passed != this {
		t.Errorf("register of the this value: got %d, want %d, the register of this", passed, this)
	}
}
