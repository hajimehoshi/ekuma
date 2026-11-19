// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// TestPostfixUpdateLocal tests that a postfix update of a local variable whose result is used
// is one instruction.
func TestPostfixUpdateLocal(t *testing.T) {
	f := compileF(t, `function f(a, i) { return a[i++]; }`)
	fused := false
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpToNumeric, bytecode.OpInc:
			t.Errorf("unfused instruction %d", i.Op)
		case bytecode.OpPostInc:
			fused = true
		}
	}
	if !fused {
		t.Error("no OpPostInc")
	}
}
