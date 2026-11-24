// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

const (
	MaxFreeFns      = maxFreeFns
	MaxFreeMapLen   = maxFreeMapLen
	MaxSparseMapLen = maxSparseMapLen
	MaxFreeCode     = maxFreeCode
	MaxFreeChunks   = maxFreeChunks
	MaxSearches     = maxSearches
)

// CompileConservativeScript compiles a script with assignment shortcuts disabled.
func CompileConservativeScript(prog *ast.Program, file string) (*bytecode.Func, error) {
	c := newCompiler(prog, file)
	c.conservativeAssignments = true
	return c.compile(nil, nil)
}

// KeptScratch compiles the scripts progs in turn with one scratch set and returns the total
// capacity of the code buffers and counts of retained function states, symbol and binding
// chunks, and references to closure bytecode in the copy runs.
func KeptScratch(progs ...*ast.Program) (code, fns, symChunks, bindChunks, copyFuncs int) {
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
	for _, r := range s.copies.runs[:cap(s.copies.runs)] {
		if r.fn != nil {
			copyFuncs++
		}
	}
	return code, len(s.fns), len(s.symChunks), len(s.bindChunks), copyFuncs
}
