// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestValuesWithoutCopies tests that a value goes straight to the register that holds it, or is read
// from the register of a local variable, instead of being copied between registers.
func TestValuesWithoutCopies(t *testing.T) {
	testCases := []struct {
		name string

		// src declares the function f.
		src string

		// want is the number of instructions op in f.
		op   bytecode.Opcode
		want int
	}{
		{
			name: "literals in a new register",
			src:  `function f(x) { return {a: [x], b: {x}}; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			name: "parts of a string chain",
			src:  `function f(x) { return "a" + x + "b" + this; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			name: "sequence assigned to a local",
			src:  `function f(o) { var x; x = (o.a, o.b); return x; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			name: "conditional assigned to a local",
			src:  `function f(c, o) { var x = c ? o.a : o.b; return x; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			// Only a copies b.
			name: "assignment to a local assigned to a local",
			src:  `function f(o) { var a, b; a = b = o.p; return a; }`,
			op:   bytecode.OpMove,
			want: 1,
		},
		{
			name: "keys of compound assignments and updates",
			src:  `function f(o, i) { o[i] += 1; o[++i]++; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			name: "prefix update key of a store",
			src:  `function f(a, i) { a[++i] = 1; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			name: "assignment to a local as an operand",
			src:  `function f(o) { var x; if ((x = o.p) != null) return x; }`,
			op:   bytecode.OpMove,
			want: 0,
		},
		{
			// x ? x : 0 jumps only when x is truthy.
			name: "local variable or a default",
			src:  `function f(x) { return x ? x : 0; }`,
			op:   bytecode.OpJump,
			want: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			var got int
			for _, i := range f.Code {
				if i.Op == tc.op {
					got++
				}
			}
			if got != tc.want {
				t.Errorf("%v: got %d, want %d", tc.op, got, tc.want)
			}
		})
	}
}
