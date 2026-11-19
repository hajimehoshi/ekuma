// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestGetPropChain tests that a property read right before a property read of its result
// becomes an OpGetPropChain.
func TestGetPropChain(t *testing.T) {
	testCases := []struct {
		name string

		// src is a function f.
		src string

		chains int
	}{
		{
			name:   "member",
			src:    `function f(o) { return o.a.b; }`,
			chains: 1,
		},
		{
			name:   "string key",
			src:    `function f(o) { return o.a["b"]; }`,
			chains: 1,
		},
		{
			name:   "method callee",
			src:    `function f(o) { return o.a.m(); }`,
			chains: 1,
		},
		{
			name:   "compound assignment",
			src:    `function f(o) { o.a.b += 1; }`,
			chains: 1,
		},
		{
			name:   "decorator",
			src:    `function f(o) { @o.a.b class C {} }`,
			chains: 1,
		},
		{
			name:   "four links",
			src:    `function f(o) { return o.a.b.c.d; }`,
			chains: 3,
		},
		{
			name:   "independent reads",
			src:    `function f(o, p) { return o.a + p.b; }`,
			chains: 0,
		},
		{
			name:   "object from an index read",
			src:    `function f(o) { return o[0].b; }`,
			chains: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := compileF(t, tc.src)
			chains := 0
			for pc, i := range f.Code {
				if i.Op != bytecode.OpGetPropChain {
					continue
				}
				chains++
				if next := f.Code[pc+1]; (next.Op != bytecode.OpGetProp && next.Op != bytecode.OpGetPropChain) || next.B != i.A {
					t.Errorf("instruction after the OpGetPropChain at %d: got opcode %d reading register %d, want a property read of register %d", pc, next.Op, next.B, i.A)
				}
			}
			if chains != tc.chains {
				t.Errorf("OpGetPropChain instructions: got %d, want %d", chains, tc.chains)
			}
		})
	}
}
