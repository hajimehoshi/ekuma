// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
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
