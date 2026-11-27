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

// TestAppend tests which appends to a local variable become OpAppends or OpAppendKs: those whose
// variable no code can read without a seal of its register first. The others append a string
// constant with an OpConcatK.
func TestAppend(t *testing.T) {
	testCases := []struct {
		name string

		// src declares a function f.
		src string

		// appends is the number of OpAppend and OpAppendK instructions of f, and concats the
		// number of its OpConcatK instructions.
		appends int
		concats int
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
			name:    "appended variable",
			src:     `function f(n, x) { var r = ""; for (var i = 0; i < n; i++) { r += "x"; r += x; } return r; }`,
			appends: 2,
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
			name:    "nested function",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) r += "x"; return () => r; }`,
			concats: 1,
		},
		{
			name:    "addition in a nested function",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) r = r + "x"; return () => r; }`,
			concats: 1,
		},
		{
			name:    "parameter of a function that uses arguments",
			src:     `function f(r) { for (var i = 0; i < arguments.length; i++) r += "x"; return r; }`,
			concats: 1,
		},
		{
			name:    "direct eval",
			src:     `function f(n) { var r = ""; for (var i = 0; i < n; i++) r += "x"; return eval("r"); }`,
			concats: 1,
		},
		{
			name:    "with statement",
			src:     `function f(n, o) { var r = ""; for (var i = 0; i < n; i++) r += "x"; with (o) { g = r; } }`,
			concats: 1,
		},
		{
			name:    "read before the append in a loop",
			src:     `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { s = r; r += "x"; } return s; }`,
			concats: 1,
		},
		{
			name:    "read after a write before the append in a loop",
			src:     `function f(n) { var r, s; for (var i = 0; i < n; i++) { r = "<"; s = r; r += "x"; } return s; }`,
			appends: 1,
		},
		{
			name:    "read in a handler before the append",
			src:     `function f(n) { var r = "", s; for (var i = 0; i < n; i++) { try { h(); } catch (e) { s = r; } r += "x"; } return s; }`,
			concats: 1,
		},
		{
			name:    "bindings that share a register",
			src:     `function f(n, s) { for (var i = 0; i < n; i++) { { let r = ""; r += "a"; r += s; g = r; } { let t = ""; for (var j = 0; j < 2; j++) { g = t; t += "x"; } } } }`,
			concats: 2,
		},
		{
			name:    "continue out of a block",
			src:     `function f(n) { for (var i = 0; i < n; i++) { let t = ""; for (var j = 0; j < n; j++) t += "x"; if (i % 2) { t += "y"; continue; } g = t; } }`,
			appends: 2,
		},
		{
			name:    "top-level code",
			src:     `{ let r = ""; for (let i = 0; i < 3; i++) r += "x"; g = r; } function f() {}`,
			concats: 1,
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
		if got, want := countOps(f, bytecode.OpAppend)+countOps(f, bytecode.OpAppendK), tc.appends; got != want {
			var b strings.Builder
			f.Disassemble(&b)
			t.Errorf("%s: OpAppend and OpAppendK instructions: got %d, want %d\n%s", tc.name, got, want, b.String())
		}
		// An OpAppend that became an OpAdd still adds the value to the variable.
		for _, in := range f.Code {
			if in.Op == bytecode.OpAdd && in.A == in.B && in.B == in.C {
				t.Errorf("%s: %s r%d, r%d, r%d", tc.name, in.Op, in.A, in.B, in.C)
			}
		}
		if got, want := countOps(f, bytecode.OpConcatK), tc.concats; got != want {
			var b strings.Builder
			f.Disassemble(&b)
			t.Errorf("%s: OpConcatK instructions: got %d, want %d\n%s", tc.name, got, want, b.String())
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
		if in.Op == bytecode.OpAppendK {
			r = in.A
			if got, want := f.SourceOffset(pc), int32(strings.Index(src, `r += "x"`)); got != want {
				t.Errorf("source offset of OpAppendK: got %d, want %d", got, want)
			}
		}
	}
	if r < 0 {
		t.Fatal("no OpAppendK")
	}
	var reads int
	for pc, in := range f.Code {
		if in.Op == bytecode.OpAppendK || in.Op == bytecode.OpSeal || !bytecode.MayRead(f.Code, pc, int(r)) {
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

// TestAppendLengthReads tests that a read of the length of a variable with appends needs no seal
// of its register.
func TestAppendLengthReads(t *testing.T) {
	const src = `function f(n) { var r = "", m = 0; for (var i = 0; i < n; i++) { r += "x"; m += r.length; } return m; }`
	prog, err := parser.ParseScript(wtf8.String(src), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := compiler.CompileScript(prog, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	f := findFunc(s, "f")
	if got := countOps(f, bytecode.OpAppendK); got != 1 {
		t.Errorf("OpAppendK instructions: got %d, want 1", got)
	}
	if got := countOps(f, bytecode.OpSeal); got != 0 {
		t.Errorf("OpSeal instructions: got %d, want 0", got)
	}
}
