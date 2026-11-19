// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// TestAssignedLocalRegister tests that an assignment whose result is used reads a local variable
// that no other code assigns from the register of the variable itself.
func TestAssignedLocalRegister(t *testing.T) {
	testCases := []struct {
		name string

		// body is the body of a function with the local variable i and the parameter o.
		body string
	}{
		{
			name: "property store",
			body: `return o.x = i;`,
		},
		{
			name: "destructuring",
			body: `let x; return ({ x } = i);`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(`function f(o) { let i = 1; `+tc.body+` }`, parser.Options{})
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
			local, src := int32(-1), int32(-2)
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpLoadInt:
					local = i.A
				case bytecode.OpSetProp:
					src = i.C
				case bytecode.OpRequireCoercible:
					src = i.A
				}
			}
			if src != local {
				t.Errorf("register of the value: got %d, want %d, the register of i", src, local)
			}
		})
	}
}
