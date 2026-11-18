// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

const (
	MaxFreeFns      = maxFreeFns
	MaxFreeMapLen   = maxFreeMapLen
	MaxSparseMapLen = maxSparseMapLen
	MaxFreeCode     = maxFreeCode
)

// KeptScratch compiles the scripts progs in turn with one scratch set and returns the total
// capacity of the code buffers and the number of function states that the set keeps for later
// compilations.
func KeptScratch(progs ...*ast.Program) (code, fns int) {
	s := &scratch{}
	for _, prog := range progs {
		c := newCompiler(prog, "test.js")
		putScratch(c.scratch)
		c.scratch = s
		c.compileScript()
		s.trim()
	}
	for _, b := range s.codeBufs {
		code += cap(b)
	}
	return code, len(s.fns)
}
