// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// function returns a script that calls a function declaring n variables, whose body ends with body.
func function(n int, body string) string {
	var b strings.Builder
	b.WriteString("(function () {\n")
	for i := range n {
		fmt.Fprintf(&b, "var v%d;\n", i)
	}
	b.WriteString(body)
	b.WriteString("})();\n")
	return b.String()
}

func TestScratchLimits(t *testing.T) {
	testCases := []struct {
		name string
		srcs []string

		// wantFns is the number of function states kept, with the script's own.
		wantFns int
	}{
		{
			name: "shorter closure run list",
			srcs: []string{
				`function f(x) { const a = () => x, b = () => x; x = 1; return [a, b]; }
				 function g(y) { const c = () => y; y = 2; return c; }`,
				"42",
			},
			wantFns: 3,
		},
		{
			name: "large function",
			srcs: []string{
				function(compiler.MaxFreeMapLen+1, strings.Repeat("x = 1;\n", compiler.MaxFreeCode)),
			},
			wantFns: 1,
		},
		{
			name: "deep nesting",
			srcs: []string{
				strings.Repeat("(function () {\n", 2*compiler.MaxFreeFns) + strings.Repeat("})();\n", 2*compiler.MaxFreeFns),
			},
			wantFns: compiler.MaxFreeFns,
		},
		{
			name: "small function after a large one",
			srcs: []string{
				function(compiler.MaxSparseMapLen+1, ""),
				function(1, ""),
			},
			wantFns: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var progs []*ast.Program
			for _, src := range tc.srcs {
				prog, err := parser.ParseScript(src, parser.Options{})
				if err != nil {
					t.Fatal(err)
				}
				progs = append(progs, prog)
			}
			code, fns, symChunks, bindChunks, copyFuncs := compiler.KeptScratch(progs...)
			if copyFuncs != 0 {
				t.Errorf("closure functions retained by copy runs: got %d, want 0", copyFuncs)
			}
			if code > compiler.MaxFreeCode {
				t.Errorf("code buffers: got %d elements, want at most %d", code, compiler.MaxFreeCode)
			}
			if symChunks > compiler.MaxFreeChunks {
				t.Errorf("symbol chunks: got %d, want at most %d", symChunks, compiler.MaxFreeChunks)
			}
			if bindChunks > compiler.MaxFreeChunks {
				t.Errorf("binding chunks: got %d, want at most %d", bindChunks, compiler.MaxFreeChunks)
			}
			if fns != tc.wantFns {
				t.Errorf("function states: got %d, want %d", fns, tc.wantFns)
			}
		})
	}
}
