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

// TestAppend tests which appends to a local variable become OpAppends: those whose variable no
// code can read without a seal of its register first.
func TestAppend(t *testing.T) {
	testCases := []struct {
		name string

		// src declares a function f.
		src string

		// appends is the number of OpAppend instructions of f.
		appends int
	}{
		{
			name:    "compound assignment",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) r += "x"; return r; }`,
			appends: 1,
		},
		{
			name:    "addition",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) r = r + "x"; return r; }`,
			appends: 1,
		},
		{
			name:    "parameter",
			src:     `function f(n, r) { for (var i = 0; i < n; i++) r += "x"; g = r; }`,
			appends: 1,
		},
		{
			name:    "parameter with a rest parameter",
			src:     `function f(r, ...rest) { for (var i = 0; i < rest.length; i++) r += "x"; return r; }`,
			appends: 1,
		},
		{
			name:    "number appended after a string",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) { r += "x"; r += 1; r = r + 2; } return r; }`,
			appends: 3,
		},
		{
			name:    "read into a variable",
			src:     `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { r += "x"; s = r; } return s; }`,
			appends: 1,
		},
		{
			name:    "read as an operand",
			src:     `function f(n) { var r = "", s = 0; for (var i = 0; i < n; i++) { r += "x"; s += r.length; } return s; }`,
			appends: 1,
		},
		{
			name:    "method receiver",
			src:     `function f(n) { var r = "", s = ""; for (var i = 0; i < n; i++) { r += "x"; s = r.slice(1); } return s; }`,
			appends: 1,
		},
		{
			name:    "global store",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) { r += "x"; g = r; } }`,
			appends: 1,
		},
		{
			name:    "string chain",
			src:     `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { r += "x"; s = "<" + r + ">"; } return s; }`,
			appends: 1,
		},
		{
			name:    "value of the append",
			src:     `function f(n) { var r = "", s; for (var i = 0; i < n; i++) s = (r += "x"); return s; }`,
			appends: 1,
		},
		{
			name:    "other compound assignment",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) { r += "x"; r -= 0; } return r; }`,
			appends: 1,
		},
		{
			name: "value that is not a string",
			src:  `function f(n, x) { var r = ""; for (var i = 0; i < n; i++) r += x; return r; }`,
		},
		{
			name: "nested function",
			src:  `function f(n) { var r = ""; for (var i = 0; i < n; i++) r += "x"; return () => r; }`,
		},
		{
			name: "parameter of a function that uses arguments",
			src:  `function f(r) { for (var i = 0; i < arguments.length; i++) r += "x"; return r; }`,
		},
		{
			name: "direct eval",
			src:  `function f(n) { var r = ""; for (var i = 0; i < n; i++) r += "x"; return eval("r"); }`,
		},
		{
			name: "with statement",
			src:  `function f(n, o) { var r = ""; for (var i = 0; i < n; i++) r += "x"; with (o) { g = r; } }`,
		},
		{
			name: "read before the append in a loop",
			src:  `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { s = r; r += "x"; } return s; }`,
		},
		{
			name: "read in a handler before the append",
			src:  `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { try { h(); } catch (e) { s = r; } r += "x"; } return s; }`,
		},
		{
			name: "bindings that share a register",
			src:  `function f(n, s) { for (var i = 0; i < n; i++) { { let r = ""; r += "a"; r += s; g = r; } { let t = ""; for (var j = 0; j < 2; j++) { g = t; t += "x"; } } } }`,
		},
		{
			name:    "continue out of a block",
			src:     `function f(n) { for (var i = 0; i < n; i++) { let t = ""; for (var j = 0; j < n; j++) t += "x"; if (i % 2) { t += "y"; continue; } g = t; } }`,
			appends: 2,
		},
		{
			name: "top-level code",
			src:  `{ let r = ""; for (let i = 0; i < 3; i++) r += "x"; g = r; } function f() {}`,
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
		f := findFunc(s, "f")
		if strings.HasPrefix(tc.src, "{") {
			f = s
		}
		if got := countOps(f, bytecode.OpAppend); got != tc.appends {
			var b strings.Builder
			f.Disassemble(&b)
			t.Errorf("%s: OpAppend instructions: got %d, want %d\n%s", tc.name, got, tc.appends, b.String())
		}
		// An OpAppend that became an OpAdd still adds the value to the variable.
		for _, in := range f.Code {
			if in.Op == bytecode.OpAdd && in.A == in.B && in.B == in.C {
				t.Errorf("%s: %s r%d, r%d, r%d", tc.name, in.Op, in.A, in.B, in.C)
			}
		}
	}
}

// TestAppendSeals tests that every read of a variable with OpAppends that can see a string that
// an OpAppend left follows an OpSeal of the variable, and that the OpAppend keeps the position of
// the assignment.
func TestAppendSeals(t *testing.T) {
	const src = `function f(n) {
	var r = "";
	for (var i = 0; i < n; i++) {
		r += "x";
		h(r);
	}
	return r;
}`
	prog, err := parser.ParseScript(wtf8.String(src), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := compiler.CompileScript(prog, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	f := findFunc(s, "f")
	var r int32 = -1
	for pc, in := range f.Code {
		if in.Op == bytecode.OpAppend {
			r = in.A
			if got, want := f.SourceOffset(pc), int32(strings.Index(src, `r += "x"`)); got != want {
				t.Errorf("source offset of OpAppend: got %d, want %d", got, want)
			}
		}
	}
	if r < 0 {
		t.Fatal("no OpAppend")
	}
	var reads int
	for pc, in := range f.Code {
		if in.Op == bytecode.OpAppend || in.Op == bytecode.OpSeal || !bytecode.MayRead(f.Code, pc, int(r)) {
			continue
		}
		reads++
		if pc == 0 || f.Code[pc-1].Op != bytecode.OpSeal || f.Code[pc-1].A != r {
			t.Errorf("read of r%d at %d without an OpSeal before it", r, pc)
		}
	}
	// h(r) and return r.
	if reads != 2 {
		t.Errorf("reads of r%d: got %d, want 2", r, reads)
	}
}
