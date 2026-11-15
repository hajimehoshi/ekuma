// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package plural_test

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/plural"
)

func TestSelect(t *testing.T) {
	for _, tc := range []struct {
		locale  string
		ordinal bool
		s       string
		e       int
		want    string
	}{
		// "one" needs v = 0, so visible fraction digits matter.
		{
			locale: "en",
			s:      "1",
			want:   "one",
		},
		{
			locale: "en",
			s:      "1.0",
			want:   "other",
		},
		// The modulus of a long integer.
		{
			locale: "ru",
			s:      "100000000000000000000021",
			want:   "one",
		},
		{
			locale: "ru",
			s:      "1.5",
			want:   "other",
		},
		// A range of n excludes a number with a fraction.
		{
			locale: "cy",
			s:      "3",
			want:   "few",
		},
		{
			locale: "cy",
			s:      "3.5",
			want:   "other",
		},
		// The exponent of compact notation.
		{
			locale: "fr",
			s:      "1500000",
			want:   "other",
		},
		{
			locale: "fr",
			s:      "1500000",
			e:      6,
			want:   "many",
		},
		{
			locale:  "en",
			ordinal: true,
			s:       "23",
			want:    "few",
		},
		// A locale without rules of its own uses those of its language.
		{
			locale: "pt-BR",
			s:      "0",
			want:   "one",
		},
		{
			locale: "pt-PT",
			s:      "0",
			want:   "other",
		},
		{
			locale: "sr-Latn",
			s:      "21",
			want:   "one",
		},
	} {
		if got := plural.Select(tc.locale, tc.ordinal, tc.s, tc.e); got != tc.want {
			t.Errorf("Select(%q, %t, %q, %d): got %q, want %q", tc.locale, tc.ordinal, tc.s, tc.e, got, tc.want)
		}
	}
}

func TestCategories(t *testing.T) {
	if got, want := plural.Categories("ar", false), []string{"zero", "one", "two", "few", "many", "other"}; !slices.Equal(got, want) {
		t.Errorf("Categories(ar): got %q, want %q", got, want)
	}
	if got, want := plural.Categories("ja", false), []string{"other"}; !slices.Equal(got, want) {
		t.Errorf("Categories(ja): got %q, want %q", got, want)
	}
}

func TestSelectRange(t *testing.T) {
	for _, tc := range []struct {
		locale, start, end, want string
	}{
		{
			locale: "lv",
			start:  "zero",
			end:    "one",
			want:   "one",
		},
		{
			locale: "en",
			start:  "one",
			end:    "other",
			want:   "other",
		},
		// No data for the pair.
		{
			locale: "en",
			start:  "few",
			end:    "many",
			want:   "other",
		},
	} {
		if got := plural.SelectRange(tc.locale, tc.start, tc.end); got != tc.want {
			t.Errorf("SelectRange(%q, %q, %q): got %q, want %q", tc.locale, tc.start, tc.end, got, tc.want)
		}
	}
}
