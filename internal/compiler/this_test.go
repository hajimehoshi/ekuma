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

// TestMethodReceiverRegister tests that a method call reads the object from the register of a
// local variable or of this, with no copy of it into the call window.
func TestMethodReceiverRegister(t *testing.T) {
	testCases := []struct {
		name string
		src  string
	}{
		{
			name: "local",
			src:  `function f(o) { o.m(); }`,
		},
		{
			name: "this",
			src:  `function f() { this.m(); }`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			// The parameter o is in register 0.
			recv, obj := int32(0), int32(-1)
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpLoadThisSloppy:
					recv = i.A
				case bytecode.OpGetMethod:
					obj = i.B
				case bytecode.OpMove:
					t.Errorf("register %d is copied to register %d", i.B, i.A)
				}
			}
			if obj != recv {
				t.Errorf("object register of the method read: got %d, want %d", obj, recv)
			}
		})
	}
}
