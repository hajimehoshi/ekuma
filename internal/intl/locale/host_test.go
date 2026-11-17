// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestHostLocaleEnvironment(t *testing.T) {
	t.Setenv("LC_ALL", "ja_JP.UTF-8")
	t.Setenv("LC_MESSAGES", "de_DE.UTF-8")
	t.Setenv("LANG", "fr_FR.UTF-8")
	for _, tc := range []struct {
		clear string
		want  string
	}{
		{
			want: "ja_JP.UTF-8",
		},
		{
			clear: "LC_ALL",
			want:  "de_DE.UTF-8",
		},
		{
			clear: "LC_MESSAGES",
			want:  "fr_FR.UTF-8",
		},
	} {
		if tc.clear != "" {
			t.Setenv(tc.clear, "")
		}
		if got := locale.HostLocale(); got != tc.want {
			t.Errorf("host locale: got %q, want %q", got, tc.want)
		}
	}
}
