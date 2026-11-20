// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

func TestToLowerCasePollsFinalSigmaContext(t *testing.T) {
	marks := strings.Repeat("́", 1<<16)
	for _, tc := range []struct {
		s    string
		want string
	}{
		{
			// The sigma is final: case-ignorable characters follow it up to the end.
			s:    "AΣ" + marks,
			want: "ς",
		},
		{
			// The context of the mapped part reaches past its end.
			s:    "AΣ" + marks + "B",
			want: "σ",
		},
	} {
		polls := 0
		// Only the sigma is mapped, so the polls come from reading its context.
		got := xstrings.ToLowerCase(tc.s, 1, 3, func() { polls++ })
		if got != tc.want {
			t.Errorf("ToLowerCase(%q...) = %q, want %q", tc.s[:3], got, tc.want)
		}
		if polls == 0 {
			t.Errorf("ToLowerCase(%q...): no polls while reading a long context", tc.s[:3])
		}
	}
}
