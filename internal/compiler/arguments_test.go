// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestArgumentsReads tests which reads of the length and the elements of arguments read the
// arguments without the arguments object being created first.
func TestArgumentsReads(t *testing.T) {
	testCases := []struct {
		name string
		src  string

		// created reports whether the object is created first. lengths, indices and
		// constIndices are the numbers of reads of the length, of an element and of an element
		// at a constant index otherwise.
		created      bool
		lengths      int
		indices      int
		constIndices int
	}{
		{
			name:    "alias",
			src:     `function f(i) { var a = arguments; return a[i] + a.length; }`,
			lengths: 1,
			indices: 1,
		},
		{
			name:         "constant index",
			src:          `function f() { var a = arguments; return a[0] + a["1"] + arguments[2]; }`,
			constIndices: 3,
		},
		{
			name:    "length key",
			src:     `function f() { return arguments["length"]; }`,
			lengths: 1,
		},
		{
			name:    "other constant key of an alias",
			src:     `function f() { var a = arguments; return a["x"]; }`,
			created: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			var created bool
			var lengths, indices, constIndices int
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpArguments:
					created = true
				case bytecode.OpGetArgsLength:
					lengths++
				case bytecode.OpGetArgsIndex:
					indices++
				case bytecode.OpGetArgsIndexInt:
					constIndices++
				}
			}
			if created != tc.created {
				t.Errorf("created: got %t, want %t", created, tc.created)
			}
			if lengths != tc.lengths || indices != tc.indices || constIndices != tc.constIndices {
				t.Errorf("reads of the length, of elements and of constant indices: got %d, %d and %d, want %d, %d and %d", lengths, indices, constIndices, tc.lengths, tc.indices, tc.constIndices)
			}
		})
	}
}
