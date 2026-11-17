// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestNumberingSystemOf(t *testing.T) {
	for _, tc := range []struct {
		locale string
		want   string
	}{
		{
			// as-IN has no numbers of its own and inherits those of as.
			locale: "as-IN",
			want:   "beng",
		},
		{
			// bn-IN has its own numbering system, unlike its parent bn.
			locale: "bn-IN",
			want:   "latn",
		},
	} {
		if got := locale.NumberingSystemOf(tc.locale); got != tc.want {
			t.Errorf("NumberingSystemOf(%q): got %q, want %q", tc.locale, got, tc.want)
		}
	}
}
