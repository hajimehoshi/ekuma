// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
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
			name: "parameter declared again by var",
			src:  `function f(a) { const inner = () => a; var a; return inner; }`,
			want: false,
		},
		{
			name: "parameter mapped by the arguments object",
			src:  `function f(a) { const inner = () => a; return arguments; }`,
			want: false,
		},
		{
			name: "var",
			src:  `function f() { var v = 1; const inner = () => v; return inner; }`,
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
