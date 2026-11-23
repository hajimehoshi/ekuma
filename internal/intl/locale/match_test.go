// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestFromHost(t *testing.T) {
	for _, tc := range []struct {
		host string
		want string
		ok   bool
	}{
		{
			host: "ja_JP.UTF-8",
			want: "ja-JP",
			ok:   true,
		},
		{
			host: "de_DE@euro",
			want: "de-DE",
			ok:   true,
		},
		{
			// An alias, and a region without data of its own.
			host: "iw_AQ",
			want: "he",
			ok:   true,
		},
		{
			host: "C.UTF-8",
		},
		{
			host: "POSIX",
		},
		{
			host: "",
		},
		{
			// A language without data.
			host: "tlh",
		},
	} {
		got, ok := locale.FromHost(tc.host)
		if got != tc.want || ok != tc.ok {
			t.Errorf("FromHost(%q): got %q, %t; want %q, %t", tc.host, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCollationParent(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{
			// A script that is not the likely one inherits from its language.
			in:   "zh-Hant",
			want: "zh",
		},
		{
			in:   "nb",
			want: "no",
		},
		{
			in:   "yue",
			want: "zh-Hant",
		},
		{
			in:   "de-AT",
			want: "de",
		},
	} {
		if got, _ := locale.CollationParent(tc.in); got != tc.want {
			t.Errorf("CollationParent(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBestFitVariantPrefix(t *testing.T) {
	m := locale.NewMatcher([]string{"en", "be-tarask"})
	got, _ := m.Match([]string{"be-tarask-unifon"}, "en", true)
	if got != "be-tarask" {
		t.Errorf("matched %q, want be-tarask", got)
	}
}

func TestBestFitImplicitSubtags(t *testing.T) {
	m := locale.NewMatcher([]string{"zh-Hans", "zh-CN"})
	got, _ := m.Match([]string{"zh"}, "en", true)
	if got != "zh-CN" {
		t.Errorf("matched %q, want zh-CN", got)
	}
}
