// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

func TestPosition(t *testing.T) {
	testCases := []struct {
		name string
		src  string
		op   bytecode.Opcode

		// last selects the last instruction with op instead of the first.
		last bool

		line   int
		column int
	}{
		{
			name:   "method call",
			src:    "a.\n  b()",
			op:     bytecode.OpCall,
			line:   2,
			column: 3,
		},
		{
			name:   "call after a call in its arguments",
			src:    "f(\n  g())",
			op:     bytecode.OpCallUndef,
			last:   true,
			line:   1,
			column: 1,
		},
		{
			name:   "call of a call",
			src:    "f()()",
			op:     bytecode.OpCallUndef,
			last:   true,
			line:   1,
			column: 4,
		},
		{
			name:   "property read",
			src:    "var v = 1;\nv + a.\n  b",
			op:     bytecode.OpGetProp,
			line:   3,
			column: 3,
		},
		{
			name:   "global reference",
			src:    "var v = 1;\nv + é + a",
			op:     bytecode.OpGetGlobal,
			last:   true,
			line:   2,
			column: 9,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(tc.src, parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			pc := -1
			for i, in := range f.Code {
				if in.Op != tc.op {
					continue
				}
				pc = i
				if !tc.last {
					break
				}
			}
			if pc < 0 {
				t.Fatalf("no instruction with opcode %d", tc.op)
			}
			if line, column := f.Position(pc); line != tc.line || column != tc.column {
				t.Errorf("Position: got %d:%d, want %d:%d", line, column, tc.line, tc.column)
			}
		})
	}
}
