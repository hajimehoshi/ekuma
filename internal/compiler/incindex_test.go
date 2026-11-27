// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestIncGetIndex tests that an increment right before an index read with its result as the key
// becomes an OpIncGetIndex or an OpPostIncGetIndex.
func TestIncGetIndex(t *testing.T) {
	testCases := []struct {
		name string

		// src is a function f.
		src string

		op    bytecode.Opcode
		count int
	}{
		{
			name:  "prefix",
			src:   `function f(a, i) { return a[++i]; }`,
			op:    bytecode.OpIncGetIndex,
			count: 1,
		},
		{
			name:  "postfix",
			src:   `function f(a, i) { return a[i++]; }`,
			op:    bytecode.OpPostIncGetIndex,
			count: 1,
		},
		{
			name:  "method callee",
			src:   `function f(a, i) { return a[i++](); }`,
			op:    bytecode.OpPostIncGetIndex,
			count: 1,
		},
		{
			name:  "increment of another variable",
			src:   `function f(a, i, j) { ++i; return a[j] + i; }`,
			op:    bytecode.OpIncGetIndex,
			count: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			count := 0
			for pc, i := range f.Code {
				if i.Op != bytecode.OpIncGetIndex && i.Op != bytecode.OpPostIncGetIndex {
					continue
				}
				if i.Op == tc.op {
					count++
				} else {
					t.Errorf("instruction at %d: got %s, want %s", pc, i.Op, tc.op)
				}
				if next := f.Code[pc+1]; next.Op != bytecode.OpGetIndex || next.C != i.A {
					t.Errorf("instruction after the %s at %d: got %s with the key %d, want an OpGetIndex with the key %d", i.Op, pc, next.Op, next.C, i.A)
				}
			}
			if count != tc.count {
				t.Errorf("%s instructions: got %d, want %d", tc.op, count, tc.count)
			}
		})
	}
}
