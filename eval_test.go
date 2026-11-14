// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestDirectEval covers what test262 does not: the lifetime of the variables that eval code
// captures, and the reuse of compiled eval code.
func TestDirectEval(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "closures made by eval code outlive the caller",
			src: `function make(n) {
					var count = n;
					let step = 10;
					return eval("(function () { count += step; return count; })");
				}
				var a = make(1), b = make(2);
				a(); a(); b();
				[a(), b()].join()`,
			want: "31,22",
		},
		{
			name: "one source at one site sees the current values",
			src: `function f(x) {
					var out = [];
					for (let i = 0; i < 3; i++) {
						out.push(eval("x + i"));
					}
					return out.join();
				}
				[f(1), f(10)].join("/")`,
			want: "1,2,3/10,11,12",
		},
		{
			name: "one source at two sites sees the scope of each",
			src: `var src = "var v = typeof x == 'undefined' ? 'none' : x; v";
				function f(x) {
					let r = [eval(src)];
					{
						let x = "block";
						r.push(eval(src));
					}
					r.push(v);
					return r.join();
				}
				f("param")`,
			want: "param,block,block",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
