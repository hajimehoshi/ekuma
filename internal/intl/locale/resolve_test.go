// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestResolveLocale(t *testing.T) {
	values := func(l string) []string {
		return []string{"latn", "arab", "thai"}
	}
	for _, tc := range []struct {
		name       string
		requested  []string
		option     string
		hasOption  bool
		wantLocale string
		wantData   string
		wantValue  string
	}{
		{
			name:       "keyword kept",
			requested:  []string{"de-AT-u-nu-thai"},
			wantLocale: "de-AT-u-nu-thai",
			wantData:   "de-AT",
			wantValue:  "thai",
		},
		{
			name:       "unsupported keyword dropped",
			requested:  []string{"de-u-nu-roman"},
			wantLocale: "de",
			wantData:   "de",
			wantValue:  "latn",
		},
		{
			name:       "option overrides the keyword",
			requested:  []string{"de-u-nu-thai"},
			option:     "ARAB",
			hasOption:  true,
			wantLocale: "de",
			wantData:   "de",
			wantValue:  "arab",
		},
		{
			name:       "option equal to the keyword",
			requested:  []string{"de-u-nu-thai"},
			option:     "thai",
			hasOption:  true,
			wantLocale: "de-u-nu-thai",
			wantData:   "de",
			wantValue:  "thai",
		},
		{
			name:       "fallback to a prefix",
			requested:  []string{"tlh", "fr-XX-u-nu-arab"},
			wantLocale: "fr-u-nu-arab",
			wantData:   "fr",
			wantValue:  "arab",
		},
		{
			// Neither quh nor und-Latn has a likely-subtags entry.
			name:       "related language with explicit script and region",
			requested:  []string{"quh-Latn-BO"},
			wantLocale: "qu-BO",
			wantData:   "qu-BO",
			wantValue:  "latn",
		},
		{
			name:       "related language with only an explicit script",
			requested:  []string{"czo-Hans"},
			wantLocale: "zh",
			wantData:   "zh",
			wantValue:  "latn",
		},
	} {
		r := locale.ResolveLocale(tc.requested, "en-US", true, []locale.ExtensionKey{{
			Key:       "nu",
			Option:    tc.option,
			HasOption: tc.hasOption,
			Values:    values,
		}})
		if r.Locale != tc.wantLocale || r.DataLocale != tc.wantData || !slices.Equal(r.Values, []string{tc.wantValue}) {
			t.Errorf("%s: got %q, %q, %q; want %q, %q, %q", tc.name, r.Locale, r.DataLocale, r.Values, tc.wantLocale, tc.wantData, tc.wantValue)
		}
	}
}

func TestResolveLocaleNullOption(t *testing.T) {
	// A null option, which hour12 makes of hourCycle, removes the keyword from the locale.
	r := locale.ResolveLocale([]string{"en-u-hc-h12"}, "en-US", true, []locale.ExtensionKey{{
		Key:        "hc",
		HasOption:  true,
		OptionNull: true,
		Values: func(l string) []string {
			return []string{"", "h11", "h12", "h23", "h24"}
		},
	}})
	if r.Locale != "en" || !slices.Equal(r.Values, []string{""}) {
		t.Errorf("got %q, %q; want %q, %q", r.Locale, r.Values, "en", []string{""})
	}
}

func TestDataLocale(t *testing.T) {
	// zh-TW is available as the language-region form of zh-Hant-TW, whose data applies.
	if got, want := locale.DataLocale("zh-TW"), "zh-Hant-TW"; got != want {
		t.Errorf("DataLocale(%q): got %q, want %q", "zh-TW", got, want)
	}
	if got, want := locale.DataLocale("de-AT"), "de-AT"; got != want {
		t.Errorf("DataLocale(%q): got %q, want %q", "de-AT", got, want)
	}
}
