// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestLiteralComparisonJump tests that a condition comparing a value with undefined, null or a
// boolean jumps on the value itself, without loading the literal.
func TestLiteralComparisonJump(t *testing.T) {
	testCases := []struct {
		name string

		// cond is a condition on the parameters x and y.
		cond string

		op bytecode.Opcode
		c  int32
	}{
		{
			name: "strict null",
			cond: `x === null`,
			op:   bytecode.OpJumpNotSEqP,
			c:    bytecode.PrimNull,
		},
		{
			name: "undefined on the left",
			cond: `undefined !== x`,
			op:   bytecode.OpJumpNotSNeP,
			c:    bytecode.PrimUndefined,
		},
		{
			name: "void",
			cond: `x === void 0`,
			op:   bytecode.OpJumpNotSEqP,
			c:    bytecode.PrimUndefined,
		},
		{
			name: "jump when true",
			cond: `x === false || y`,
			op:   bytecode.OpJumpNotSNeP,
			c:    bytecode.PrimFalse,
		},
		{
			name: "loose null",
			cond: `x == null`,
			op:   bytecode.OpJumpIfNotNullish,
			c:    1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, `function f(x, y) { if (`+tc.cond+`) return 1; }`)
			found := false
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpLoadUndef, bytecode.OpLoadNull, bytecode.OpLoadFalse, bytecode.OpLoadTrue:
					t.Errorf("the literal is loaded into register %d", i.A)
				case tc.op:
					found = true
					if i.C != tc.c {
						t.Errorf("operand c: got %d, want %d", i.C, tc.c)
					}
				}
			}
			if !found {
				t.Errorf("no jump with opcode %d", tc.op)
			}
		})
	}
}
