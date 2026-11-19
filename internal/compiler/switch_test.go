// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// TestSwitchCaseJumps tests that each case compares the register of a local discriminant and
// jumps in one instruction.
func TestSwitchCaseJumps(t *testing.T) {
	prog, err := parser.ParseScript(`function f(x) { switch (x) { case 1: return 1; case "a": return 2; } }`, parser.Options{})
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
	var discs []int32
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpJumpNotSNeK, bytecode.OpJumpNotSNe:
			discs = append(discs, i.B)
		case bytecode.OpMove, bytecode.OpStrictEq:
			t.Errorf("unexpected %v", i.Op)
		}
	}
	if len(discs) != 2 || discs[0] != discs[1] {
		t.Errorf("discriminant registers of the case jumps: got %v, want two equal registers", discs)
	}
}
