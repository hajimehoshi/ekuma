// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestArgumentsReads tests that reads of the length and the elements of arguments read the
// arguments without the arguments object being created first.
func TestArgumentsReads(t *testing.T) {
	testCases := []struct {
		name string
		src  string

		// lengths, indices and constIndices are the numbers of reads of the length, of an
		// element and of an element at a constant index.
		lengths      int
		indices      int
		constIndices int
	}{
		{
			name:    "length key",
			src:     `function f() { return arguments["length"]; }`,
			lengths: 1,
		},
		{
			name:         "constant index",
			src:          `function f(i) { return arguments[i] + arguments[0] + arguments["1"]; }`,
			indices:      1,
			constIndices: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			var lengths, indices, constIndices int
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpArguments:
					t.Error("OpArguments creates the arguments object")
				case bytecode.OpGetArgsLength:
					lengths++
				case bytecode.OpGetArgsIndex:
					indices++
				case bytecode.OpGetArgsIndexInt:
					constIndices++
				}
			}
			if lengths != tc.lengths || indices != tc.indices || constIndices != tc.constIndices {
				t.Errorf("reads of the length, of elements and of constant indices: got %d, %d and %d, want %d, %d and %d", lengths, indices, constIndices, tc.lengths, tc.indices, tc.constIndices)
			}
		})
	}
}
