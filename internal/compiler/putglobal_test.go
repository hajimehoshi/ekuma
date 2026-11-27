// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// countOps returns how many instructions of f and its nested functions have the opcode op.
func countOps(f *bytecode.Func, op bytecode.Opcode) int {
	var n int
	for _, i := range f.Code {
		if i.Op == op {
			n++
		}
	}
	for _, p := range f.Protos {
		n += countOps(p, op)
	}
	return n
}

// TestPutGlobal tests that an assignment to a global variable resolves its reference at the
// store when nothing between the two can observe the order.
func TestPutGlobal(t *testing.T) {
	testCases := []struct {
		name string
		src  string

		// put is the number of op instructions, and resolve the number of OpResolveGlobal
		// instructions.
		op      bytecode.Opcode
		put     int
		resolve int
	}{
		{
			name: "literal",
			src:  `function f() { g = null; }`,
			op:   bytecode.OpPutGlobal,
			put:  1,
		},
		{
			name: "strict",
			src:  `"use strict"; function f() { g = 1; }`,
			op:   bytecode.OpPutGlobalStrict,
			put:  1,
		},
		{
			name: "global undefined",
			src:  `function f() { g = undefined; }`,
			op:   bytecode.OpPutGlobal,
			put:  1,
		},
		{
			name: "local variable",
			src:  `function f(a) { g = void !a; }`,
			op:   bytecode.OpPutGlobal,
			put:  1,
		},
		{
			name: "var declaration",
			src:  `var g = -1;`,
			op:   bytecode.OpPutGlobal,
			put:  1,
		},
		{
			name:    "var declaration of a call",
			src:     `var g = h();`,
			op:      bytecode.OpPutGlobal,
			resolve: 1,
		},
		{
			name: "for-in target",
			src:  `function f(o) { for (g in o); }`,
			op:   bytecode.OpPutGlobal,
			put:  1,
		},
	}
	for _, tc := range testCases {
		prog, err := parser.ParseScript(wtf8.String(tc.src), parser.Options{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := compiler.CompileScript(prog, "test.js")
		if err != nil {
			t.Fatal(err)
		}
		if got := countOps(s, tc.op); got != tc.put {
			t.Errorf("%s: %s instructions: got %d, want %d", tc.name, tc.op, got, tc.put)
		}
		if got := countOps(s, bytecode.OpResolveGlobal); got != tc.resolve {
			t.Errorf("%s: OpResolveGlobal instructions: got %d, want %d", tc.name, got, tc.resolve)
		}
	}
}

// TestPutGlobalOfLocal tests that a global store of a local variable that no other code can
// assign reads the register of the variable.
func TestPutGlobalOfLocal(t *testing.T) {
	const src = `function f(a) { g = a; }`
	prog, err := parser.ParseScript(wtf8.String(src), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := compiler.CompileScript(prog, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	f := s.Protos[0]
	if got := countOps(f, bytecode.OpMove); got != 0 {
		t.Errorf("OpMove instructions: got %d, want 0", got)
	}
	for pc, i := range f.Code {
		if i.Op != bytecode.OpPutGlobal {
			continue
		}
		// The parameter a is in the first register.
		if i.A != 0 {
			t.Errorf("OpPutGlobal operand: got r%d, want r0", i.A)
		}
		if got, want := f.SourceOffset(pc), int32(strings.Index(src, "g = a")); got != want {
			t.Errorf("source offset of OpPutGlobal: got %d, want %d", got, want)
		}
	}
	if got := countOps(f, bytecode.OpPutGlobal); got != 1 {
		t.Errorf("OpPutGlobal instructions: got %d, want 1", got)
	}
}
