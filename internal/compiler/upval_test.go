// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
)

// findFunc returns the function named name among the functions nested in f.
func findFunc(f *bytecode.Func, name string) *bytecode.Func {
	for _, p := range f.Protos {
		if p.Name == name {
			return p
		}
		if q := findFunc(p, name); q != nil {
			return q
		}
	}
	return nil
}

// manyCaptured returns statements that declare n variables and a closure that captures them.
func manyCaptured(n int) string {
	var decls, uses []string
	for i := range n {
		decls = append(decls, fmt.Sprintf("w%d = %d", i, i))
		uses = append(uses, fmt.Sprintf("w%d", i))
	}
	return "var " + strings.Join(decls, ", ") + "; const all = () => " + strings.Join(uses, " + ") + ";"
}

// closuresInLoop returns a function that assigns n variables in a loop and then returns n
// closures, one for each variable, of which inner captures the first.
func closuresInLoop(n int) string {
	var vars, assigns, closures []string
	for i := range n {
		vars = append(vars, fmt.Sprintf("v%d", i))
		assigns = append(assigns, fmt.Sprintf("v%d = x;", i))
		if i > 0 {
			closures = append(closures, fmt.Sprintf("() => v%d", i))
		}
	}
	return "function f(x) { var " + strings.Join(vars, ", ") + "; while (x) { " + strings.Join(assigns, " ") +
		" if (x.y) { const inner = () => v0; return [inner, " + strings.Join(closures, ", ") + "]; } x = x.next; } }"
}

func TestUpvalByValue(t *testing.T) {
	testCases := []struct {
		name string

		// src declares a function inner that captures exactly one variable.
		src  string
		want bool
	}{
		{
			name: "parameter",
			src:  `function f(a) { const inner = () => a; return inner; }`,
			want: true,
		},
		{
			name: "parameter assigned after the closure is created",
			src:  `function f(a) { const inner = () => a; a = 1; return inner; }`,
			want: false,
		},
		{
			name: "parameter named like variables that functions around it assign",
			src:  `function outer() { function g() { a = 1; } function f(a) { const inner = () => a; return inner; } a = 2; }`,
			want: true,
		},
		{
			name: "parameter declared again by var",
			src:  `function f(a) { const inner = () => a; var a; return inner; }`,
			want: true,
		},
		{
			name: "parameter mapped by the arguments object",
			src:  `function f(a) { const inner = () => a; return arguments; }`,
			want: false,
		},
		{
			name: "var",
			src:  `function f() { var v = 1; const inner = () => v; return inner; }`,
			want: true,
		},
		{
			name: "var assigned before the closure is created",
			src:  `function f(x) { var v; while (x) { v = x; if (x.y) { const inner = () => v; return inner; } x = x.next; } }`,
			want: true,
		},
		{
			name: "var assigned in a loop that creates the closure",
			src:  `function f(xs) { var v; var fs = []; for (var i = 0; i < xs.length; i++) { v = xs[i]; const inner = () => v; fs.push(inner); } return fs; }`,
			want: false,
		},
		{
			name: "var that holds the closure",
			src:  `function f() { var inner; inner = function () { return inner; }; return inner; }`,
			want: false,
		},
		{
			name: "var captured by a hoisted function",
			src:  `function f() { function inner() { return v; } var v = 1; return inner; }`,
			want: false,
		},
		{
			name: "var after more than 64 other captured variables",
			src:  `function f() { ` + manyCaptured(70) + ` var v = 1; const inner = () => v; return [all, inner]; }`,
			want: true,
		},
		{
			name: "var assigned in a loop that creates the closure, after more than 64 other captured variables",
			src:  `function f(xs) { ` + manyCaptured(70) + ` var v; var fs = [all]; for (var i = 0; i < xs.length; i++) { v = xs[i]; const inner = () => v; fs.push(inner); } return fs; }`,
			want: false,
		},
		{
			name: "var assigned in a loop before as many closures are created as are searched",
			src:  closuresInLoop(compiler.MaxSearches),
			want: true,
		},
		{
			name: "var assigned in a loop before more closures are created than are searched",
			src:  closuresInLoop(compiler.MaxSearches + 1),
			want: false,
		},
		{
			name: "var assigned in a handler",
			src:  `function f(g) { var v = 1; const inner = () => v; try { g(); } catch (e) { v = 2; } return inner; }`,
			want: false,
		},
		{
			name: "this",
			src:  `function f() { const inner = () => this; return inner; }`,
			want: true,
		},
		{
			name: "this of a derived constructor",
			src:  `class C extends Object { constructor() { super(); const inner = () => this; } }`,
			want: false,
		},
		{
			name: "let after its initialization",
			src:  `function f() { let v = 1; const inner = () => v; return inner; }`,
			want: true,
		},
		{
			name: "let captured by a hoisted function",
			src:  `function f() { function inner() { return v; } let v = 1; return inner; }`,
			want: false,
		},
		{
			name: "function with a direct eval",
			src:  `function f(a) { const inner = () => a; eval(""); return inner; }`,
			want: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(tc.src, parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			inner := findFunc(f, "inner")
			if inner == nil || len(inner.Upvals) != 1 {
				t.Fatalf("no function inner with one upvalue")
			}
			if got := inner.Upvals[0].ByValue; got != tc.want {
				t.Errorf("ByValue: got %v, want %v", got, tc.want)
			}
		})
	}
}
