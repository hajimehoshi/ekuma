// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"reflect"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// TestLiteralStores tests that a literal stored to a local variable is moved once, and that
// an object literal defines a field from the register of a local variable.
func TestLiteralStores(t *testing.T) {
	testCases := []struct {
		name  string
		src   string
		moves int
	}{
		{
			name:  "object",
			src:   `function f(x, y) { const o = {x, y: y}; return o.x; }`,
			moves: 1,
		},
		{
			name:  "array",
			src:   `function f(x) { let a = [x]; a = [a]; return a.length; }`,
			moves: 2,
		},
		{
			// The substitution is copied to be converted to a string.
			name:  "template",
			src:   "function f(x) { let s = `${x}!`; return s.length; }",
			moves: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(tc.src, parser.Options{})
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
			var moves int
			for _, i := range f.Code {
				if i.Op == bytecode.OpMove {
					moves++
				}
			}
			if moves != tc.moves {
				t.Errorf("moves: got %d, want %d", moves, tc.moves)
			}
		})
	}
}

// TestConstantArrayLiteral tests that an array literal of constants is one instruction that
// writes the array to its destination, a new register or a local variable.
func TestConstantArrayLiteral(t *testing.T) {
	const lit = `[1, -2.5, "s", 3n, true, false, null, undefined, void 0]`
	want := []bytecode.Const{
		{
			Kind: bytecode.ConstNumber,
			Num:  1,
		},
		{
			Kind: bytecode.ConstNumber,
			Num:  -2.5,
		},
		{
			Kind: bytecode.ConstString,
			Str:  "s",
		},
		{
			Kind: bytecode.ConstBigInt,
			Str:  "3",
		},
		{
			Kind: bytecode.ConstTrue,
		},
		{
			Kind: bytecode.ConstFalse,
		},
		{
			Kind: bytecode.ConstNull,
		},
		{
			Kind: bytecode.ConstUndefined,
		},
		{
			Kind: bytecode.ConstUndefined,
		},
	}
	testCases := []struct {
		name string
		src  string
	}{
		{
			name: "new register",
			src:  `function f() { return ` + lit + `; }`,
		},
		{
			name: "local variable",
			src:  `function f() { let a; a = ` + lit + `; return a; }`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(tc.src, parser.Options{})
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
			var arrays []bytecode.Instr
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpNewArrayK:
					arrays = append(arrays, i)
				case bytecode.OpNewArray, bytecode.OpArrayPush, bytecode.OpMove:
					t.Errorf("%v in the code", i.Op)
				}
			}
			if len(arrays) != 1 {
				t.Fatalf("OpNewArrayK: got %d, want 1", len(arrays))
			}
			if a := arrays[0]; !reflect.DeepEqual(f.Consts[a.B:a.B+a.C], want) {
				t.Errorf("elements: got %+v, want %+v", f.Consts[a.B:a.B+a.C], want)
			}
		})
	}
}

// TestMixedArrayLiteral tests that an array literal with an element other than a constant pushes
// its elements, and keeps no constants for its leading constant elements.
func TestMixedArrayLiteral(t *testing.T) {
	prog, err := parser.ParseScript(`function f(x) { return [1, 2, x]; }`, parser.Options{})
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
	var pushes int
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpArrayPush:
			pushes++
		case bytecode.OpNewArrayK:
			t.Errorf("%v in the code", i.Op)
		}
	}
	if pushes != 3 {
		t.Errorf("OpArrayPush: got %d, want 3", pushes)
	}
	if len(f.Consts) != 0 {
		t.Errorf("constants: got %+v, want none", f.Consts)
	}
}
