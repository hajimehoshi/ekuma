// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestLocaleFromAndroidProperties(t *testing.T) {
	properties := map[string]string{
		"persist.sys.locale":         "zh-Hant-TW",
		"persist.sys.language":       "ja",
		"persist.sys.country":        "JP",
		"persist.sys.localevar":      "JP",
		"ro.product.locale":          "fr-CA",
		"ro.product.locale.language": "en",
		"ro.product.locale.region":   "GB",
	}
	for _, tc := range []struct {
		remove string
		want   string
	}{
		{
			want: "zh-Hant-TW",
		},
		{
			remove: "persist.sys.locale",
			want:   "ja-JP",
		},
		{
			remove: "persist.sys.language",
			want:   "fr-CA",
		},
		{
			remove: "ro.product.locale",
			want:   "en-GB",
		},
		{
			remove: "ro.product.locale.region",
			want:   "en",
		},
		{
			remove: "ro.product.locale.language",
			want:   "",
		},
	} {
		delete(properties, tc.remove)
		if got := locale.LocaleFromAndroidProperties(properties); got != tc.want {
			t.Errorf("after removing %q: got %q, want %q", tc.remove, got, tc.want)
		}
	}
}
