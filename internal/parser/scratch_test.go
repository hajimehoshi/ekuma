// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/parser"
)

// vars returns a script of n var statements, each declaring a variable of its own.
func vars(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "var v%d;\n", i)
	}
	return b.String()
}

func TestScratchLimits(t *testing.T) {
	testCases := []struct {
		name string
		srcs []string

		// wantScopes is the number of scope records kept, or -1 for any number.
		wantScopes int
	}{
		{
			name: "large program",
			srcs: []string{
				vars(max(parser.MaxFreeNames, parser.MaxFreeItems) + 1),
			},
			wantScopes: 1,
		},
		{
			name: "deep nesting",
			srcs: []string{
				strings.Repeat("{ let x;\n", 2*parser.MaxFreeScopes) + strings.Repeat("}\n", 2*parser.MaxFreeScopes),
			},
			wantScopes: parser.MaxFreeScopes,
		},
		{
			name: "labels, arrow functions and assignments",
			srcs: []string{
				"a: for (;;) { x = 1; break a; }\nvar f = (p, [q] = [1]) => { p = q; };\nfunction g() { { let y; y = 2; } }\n",
			},
			wantScopes: -1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scopes, names, items, cleared := parser.KeptScratch(tc.srcs...)
			if tc.wantScopes >= 0 && scopes != tc.wantScopes {
				t.Errorf("scope records: got %d, want %d", scopes, tc.wantScopes)
			}
			if names > parser.MaxFreeNames {
				t.Errorf("names of a scope record: got %d, want at most %d", names, parser.MaxFreeNames)
			}
			if items > parser.MaxFreeItems {
				t.Errorf("list stack items: got %d, want at most %d", items, parser.MaxFreeItems)
			}
			if !cleared {
				t.Errorf("kept storage still points to something")
			}
		})
	}
}
