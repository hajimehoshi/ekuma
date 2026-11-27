// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestObjectLiteralCapacity(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int32
	}{
		{
			name: "distinct store names",
			body: "var o; o = {}; o.a = 1; o.a = 2; o.b = true;",
			want: 2,
		},
		{
			name: "stop at overwrite",
			body: "var o = {}; o.a = 1; o = null; o.b = 2;",
			want: 1,
		},
		{
			name: "stores of a variable with appends",
			body: `var r = ""; for (var i = 0; i < 3; i++) r += "x"; var o = {}; o.a = r; o.b = r;`,
			want: 2,
		},
		{
			name: "keep literal hint",
			body: "var o = {a: 1}; o.b = 2; o.c = 3;",
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := parser.ParseScript(wtf8.String(`function f() { "use strict"; `+tt.body+` }`), parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			for _, in := range f.Protos[0].Code {
				if in.Op == bytecode.OpNewObject {
					if in.C != tt.want {
						t.Errorf("object literal capacity = %d, want %d", in.C, tt.want)
					}
					return
				}
			}
			t.Error("missing OpNewObject")
		})
	}
}
