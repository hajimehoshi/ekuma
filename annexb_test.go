// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestAnnexBGlobalSkip covers the block-level functions of sloppy code that get no global
// variable, which test262 does not.
func TestAnnexBGlobalSkip(t *testing.T) {
	tests := []struct {
		name    string
		scripts []string
		want    string
	}{
		{
			name: "lexical declaration of an earlier script",
			scripts: []string{
				`let f = 1;`,
				`{ function f() {} } f`,
			},
			want: "1",
		},
		{
			name: "non-extensible global object",
			scripts: []string{
				`var r = []; Object.preventExtensions(globalThis);`,
				`{ function g() {} r.push(typeof g); }
				r.push(typeof g);
				r.join()`,
			},
			want: "function,undefined",
		},
		{
			name: "eval code",
			scripts: []string{
				`let h = 1; eval("{ function h() {} }"); h`,
			},
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			var v ekuma.Value
			for _, src := range tt.scripts {
				var err error
				v, err = vm.RunScript(t.Context(), tt.name, src)
				if err != nil {
					t.Fatalf("RunScript: %v", err)
				}
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
