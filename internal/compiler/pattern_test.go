// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// TestDestructuringIntoBindings tests that object and array patterns of local bindings read each
// value straight into the binding's register.
func TestDestructuringIntoBindings(t *testing.T) {
	testCases := []struct {
		name string
		src  string
	}{
		{
			name: "declaration",
			src:  `function f(o, a) { const {x, y} = o; let [p, q] = a; return x + y + p + q; }`,
		},
		{
			name: "assignment",
			src:  `function f(o, a) { var x, y, p, q; ({x, y} = o); [p, q] = a; return x + y + p + q; }`,
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
			var reads int
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpGetProp, bytecode.OpIterNext:
					reads++
				case bytecode.OpMove, bytecode.OpLoadUndef:
					t.Errorf("unexpected %v", i.Op)
				}
			}
			if reads != 4 {
				t.Errorf("reads: got %d, want 4", reads)
			}
		})
	}
}
