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
			name:    "appends outside loops",
			src:     `function f(k) { var s = "<"; s += k; s += ">"; s = "(" + s; return s; }`,
			concats: 1,
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

func TestCompoundBigIntAccumulation(t *testing.T) {
	const src = `
		function local(n, x) {
			var total = 0n;
			for (var i = 0; i < n; i++) {
				total += x * x;
				if (total < 0n) break;
			}
			return total;
		}
		function captured(n, x) {
			var total = 0n;
			function read() { return total; }
			for (var i = 0; i < n; i++) total += x * x;
			return read;
		}
	`
	prog, err := parser.ParseScript(wtf8.String(src), parser.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := compiler.CompileScript(prog, "test.js")
	if err != nil {
		t.Fatal(err)
	}
	f := findFunc(s, "local")
	var accumulations, seals int
	for pc, in := range f.Code {
		if in.Accumulate {
			accumulations++
			if pc == 0 || f.Code[pc-1].Op != bytecode.OpMulAdd {
				t.Errorf("accumulating add at %d has no annotated multiply", pc)
			}
		}
		if in.Op == bytecode.OpSeal {
			seals++
			if pc+1 == len(f.Code) || f.Code[pc+1].Op != bytecode.OpReturn || f.Code[pc+1].A != in.A {
				t.Errorf("seal at %d does not protect the return", pc)
			}
		}
	}
	if accumulations != 1 || seals != 1 {
		t.Errorf("got %d accumulating adds and %d seals, want one of each", accumulations, seals)
	}
	for pc, in := range findFunc(s, "captured").Code {
		if in.Accumulate {
			t.Errorf("add at %d can mutate a captured variable", pc)
		}
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
	if got := countAppendSeals(f); got != 0 {
		t.Errorf("string seals: got %d, want 0", got)
	}
}

func TestPrependRefusesUnsealedRead(t *testing.T) {
	f := compileF(t, `function f(n) {
		var r = "", saved;
		for (var i = 0; i < n; i++) {
			saved = r;
			r = "a" + r;
			r = (i ? "b" : "c") + r;
		}
		return saved;
	}`)
	var constants, additions int
	for _, in := range f.Code {
		switch in.Op {
		case bytecode.OpPrependK:
			constants++
			if in.C != 1 {
				t.Error("constant prepend may grow the string in place")
			}
		case bytecode.OpPrepend:
			t.Error("register prepend may grow the string in place")
		case bytecode.OpAdd:
			if in.C == in.A && in.B != in.A {
				additions++
			}
		}
	}
	if constants != 1 || additions != 1 {
		t.Errorf("got %d constant prepends and %d register additions, want one of each", constants, additions)
	}
}

// TestAppendLoopSeals tests that a loop without appends to a variable with appends seals the
// variable once before it instead of at each read in it.
func TestAppendLoopSeals(t *testing.T) {
	testCases := []struct {
		name string

		// src declares a function f.
		src string

		// appends is the number of OpAppend and OpAppendK instructions of f that extend strings
		// in place, and seals the number of its OpSeal instructions.
		appends int
		seals   int
	}{
		{
			name:    "reads in a later loop",
			src:     `function f(n) { var r = "", m = 0; for (var i = 0; i < n; i++) r = "x" + r; for (var i = 0; i < n; i++) m += r.charCodeAt(i) + r.charCodeAt(0); return m; }`,
			appends: 1,
			seals:   1,
		},
		{
			name:    "reads in a loop with appends",
			src:     `function f(n) { var r = "<"; for (var i = 0; i < n; i++) r += "<"; for (var i = 0; i < n; i++) { g = r; r += "x"; } }`,
			appends: 2,
			seals:   1,
		},
		{
			name:    "reads in a loop with prepends",
			src:     `function f(n) { var r = "<"; for (var i = 0; i < n; i++) r = "<" + r; for (var i = 0; i < n; i++) { r = "x" + r; g = r; } }`,
			appends: 2,
			seals:   1,
		},
		{
			name:    "prepend before another assignment in a loop",
			src:     `function f(n, a, b) { var r = "<", kept = []; for (var i = 0; i < n; i++) r = "x" + r; for (r = "y" + r; kept.length < 3; r = a + b) kept.push(r); return kept; }`,
			appends: 2,
			seals:   1,
		},
		{
			name:    "reads after a sealed loop",
			src:     `function f(n) { var r = "<"; for (var i = 0; i < n; i++) r += "<"; for (var i = 0; i < n; i++) h(r); r += "x"; g = r; }`,
			appends: 2,
			seals:   2,
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
		var appends int
		for _, in := range f.Code {
			if grows(in) {
				appends++
			}
		}
		if seals := countAppendSeals(f); appends != tc.appends || seals != tc.seals {
			var b strings.Builder
			f.Disassemble(&b)
			t.Errorf("%s: appends and seals: got %d, %d, want %d, %d\n%s", tc.name, appends, seals, tc.appends, tc.seals, b.String())
		}
	}
}

// TestAppendLoopScanLimit tests that a function whose loops are too large to scan for appends
// seals the reads in its later loops one by one, and keeps the appends of the loop whose scan
// stopped.
func TestAppendLoopScanLimit(t *testing.T) {
	// Each loop has about 4 nodes per read and a few more.
	const reads = 100
	loops := compiler.MaxLoopScanNodes/(4*reads) + 10
	testCases := []struct {
		name string

		// end ends the body of each loop.
		end string

		// appends is the number of OpPrependK instructions that extend strings in place, and
		// minSeals one less than the smallest number of OpSeal instructions.
		appends  int
		minSeals int
	}{
		{
			name:     "reads",
			appends:  1,
			minSeals: loops,
		},
		{
			name:     "reads and a prepend",
			end:      ` r = "y" + r;`,
			appends:  loops + 1,
			minSeals: 0,
		},
	}
	for _, tc := range testCases {
		var b strings.Builder
		b.WriteString(`function f(n) { var r = ""; for (var i = 0; i < n; i++) r = "x" + r;`)
		for range loops {
			b.WriteString(` for (var i = 0; i < n; i++) {`)
			for range reads {
				b.WriteString(` h(r);`)
			}
			b.WriteString(tc.end + ` }`)
		}
		b.WriteString(` }`)
		prog, err := parser.ParseScript(wtf8.String(b.String()), parser.Options{})
		if err != nil {
			t.Fatal(err)
		}
		s, err := compiler.CompileScript(prog, "test.js")
		if err != nil {
			t.Fatal(err)
		}
		f := findFunc(s, "f")
		var appends int
		for _, in := range f.Code {
			if in.Op == bytecode.OpPrependK && grows(in) {
				appends++
			}
		}
		if appends != tc.appends {
			t.Errorf("%s: OpPrependK instructions that extend strings in place: got %d, want %d", tc.name, appends, tc.appends)
		}
		if got := countOps(f, bytecode.OpSeal); got <= tc.minSeals {
			t.Errorf("%s: OpSeal instructions: got %d, want more than %d", tc.name, got, tc.minSeals)
		}
	}
}

// grows reports whether in is an append or a prepend that may extend a string in place.
func grows(in bytecode.Instr) bool {
	switch in.Op {
	case bytecode.OpAppend, bytecode.OpAppendK, bytecode.OpPrepend:
		return true
	case bytecode.OpPrependK:
		return in.C == 0
	}
	return false
}

func countAppendSeals(f *bytecode.Func) int {
	regs := map[int32]bool{}
	for _, in := range f.Code {
		if grows(in) {
			regs[in.A] = true
		}
	}
	var count int
	for _, in := range f.Code {
		if in.Op == bytecode.OpSeal && regs[in.A] {
			count++
		}
	}
	return count
}
