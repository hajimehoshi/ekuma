// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestUndefinedThisCall tests that a call whose this value is undefined leaves loading it to the
// call instruction.
func TestUndefinedThisCall(t *testing.T) {
	testCases := []struct {
		name string

		// src is a function f with the parameter g.
		src string

		op bytecode.Opcode
	}{
		{
			name: "variable",
			src:  `function f(g) { g(1); }`,
			op:   bytecode.OpCallUndef,
		},
		{
			name: "expression",
			src:  `function f(g) { (0, g)(1); }`,
			op:   bytecode.OpCallUndef,
		},
		{
			name: "tagged template",
			src:  "function f(g) { g`a`; }",
			op:   bytecode.OpCallUndef,
		},
		{
			name: "tail call",
			src:  `function f(g) { "use strict"; return g(1); }`,
			op:   bytecode.OpTailCallUndef,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			calls := 0
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpLoadUndef:
					t.Errorf("OpLoadUndef into register %d", i.A)
				case tc.op:
					calls++
				}
			}
			if calls != 1 {
				t.Errorf("calls with opcode %d: got %d, want 1", tc.op, calls)
			}
		})
	}
}
