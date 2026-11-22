// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// compileF compiles the script src and returns its function f.
func compileF(t *testing.T, src string) *bytecode.Func {
	t.Helper()
	prog, err := parser.ParseScript(wtf8.String(src), parser.Options{})
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
	return f
}

// TestCallResultInPlace tests that a call whose result has no destination of its own leaves the
// result where the call puts it.
func TestCallResultInPlace(t *testing.T) {
	testCases := []struct {
		name string

		// body is the body of a function with the parameter g.
		body string
	}{
		{
			name: "statement",
			body: `g();`,
		},
		{
			name: "operand",
			body: `return g().x;`,
		},
		{
			name: "argument",
			body: `g(g());`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, `function f(g) { `+tc.body+` }`)
			calls := 0
			for pc, i := range f.Code {
				if i.Op != bytecode.OpCallUndef {
					continue
				}
				calls++
				if next := f.Code[pc+1]; next.Op == bytecode.OpMove && next.B == i.A {
					t.Errorf("the result of the call at %d is moved to register %d", pc, next.A)
				}
			}
			if calls == 0 {
				t.Fatal("no call found")
			}
		})
	}
}

// TestCallResultToLocal tests that a call whose result goes to a local variable writes it to the
// register of the variable.
func TestCallResultToLocal(t *testing.T) {
	testCases := []struct {
		name string

		// body is the body of a function with the parameters g and o, which returns the local
		// variable c.
		body string

		op bytecode.Opcode
	}{
		{
			name: "declaration",
			body: `var c = g(); return c;`,
			op:   bytecode.OpCallUndef,
		},
		{
			name: "assignment",
			body: `let c; c = o.m(); return c;`,
			op:   bytecode.OpCall,
		},
		{
			name: "new",
			body: `var c = new g(); return c;`,
			op:   bytecode.OpNew,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, `function f(g, o) { `+tc.body+` }`)
			dst, local := int32(-1), int32(-2)
			for _, i := range f.Code {
				switch i.Op {
				case tc.op:
					dst = i.A
				case bytecode.OpReturn:
					local = i.A
				}
			}
			if dst != local {
				t.Errorf("destination of the call: got %d, want %d, the register of c", dst, local)
			}
		})
	}
}

// TestReturnLocalRegister tests that return reads a local variable from its own register.
func TestReturnLocalRegister(t *testing.T) {
	f := compileF(t, `function f(a) { let x = a + 1; return x; }`)
	local, ret := int32(-1), int32(-2)
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpAddK:
			local = i.A
		case bytecode.OpReturn:
			ret = i.A
		}
	}
	if ret != local {
		t.Errorf("register of the returned value: got %d, want %d, the register of x", ret, local)
	}
}

// TestPrefixUpdateComparison tests that a comparison reads a prefix update of a local variable from
// the register of the variable itself.
func TestPrefixUpdateComparison(t *testing.T) {
	f := compileF(t, `function f(n) { while (--n >= 0) {} }`)
	local, compared := int32(-1), int32(-2)
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpDec:
			local = i.A
		case bytecode.OpLoopGeK:
			compared = i.B
		}
	}
	if compared != local {
		t.Errorf("compared register: got %d, want %d, the register of n", compared, local)
	}
}
