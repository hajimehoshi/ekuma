// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collate_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/collate"
)

func TestSort(t *testing.T) {
	tertiary := collate.Options{
		Strength: 3,
	}
	for _, tc := range []struct {
		name   string
		locale string
		typ    string
		opts   collate.Options
		in     string
		want   string
	}{
		{
			name:   "root",
			locale: "und",
			typ:    "standard",
			opts:   tertiary,
			in:     "b B a A á ä ab",
			want:   "a A á ä ab b B",
		},
		{
			name:   "contraction of a tailoring",
			locale: "cs",
			typ:    "standard",
			opts:   tertiary,
			in:     "i h ch c č",
			want:   "c č h ch i",
		},
		{
			name:   "expansion of a tailoring",
			locale: "de",
			typ:    "phonebk",
			opts:   tertiary,
			in:     "Af Ab Ä",
			want:   "Ab Ä Af",
		},
		{
			name:   "search",
			locale: "de",
			typ:    "search",
			opts:   tertiary,
			in:     "Ä AE",
			want:   "AE Ä",
		},
		{
			name:   "primary after z",
			locale: "sv",
			typ:    "standard",
			opts:   tertiary,
			in:     "ö z ä a o",
			want:   "a o z ä ö",
		},
		{
			name:   "reordering",
			locale: "ru",
			typ:    "standard",
			opts:   tertiary,
			in:     "b б a а",
			want:   "а б a b",
		},
		{
			name:   "default type with an inherited tailoring",
			locale: "zh-Hant",
			typ:    "standard",
			opts:   tertiary,
			in:     "国 中 人 a",
			want:   "人 中 国 a",
		},
		{
			name:   "pinyin",
			locale: "zh",
			typ:    "standard",
			opts:   tertiary,
			in:     "中 人 北 国",
			want:   "北 国 人 中",
		},
		{
			name:   "case first of a tailoring",
			locale: "da",
			typ:    "standard",
			opts: collate.Options{
				Strength:  3,
				CaseFirst: "upper",
			},
			in:   "a A b B",
			want: "A a B b",
		},
		{
			name:   "numeric",
			locale: "und",
			typ:    "standard",
			opts: collate.Options{
				Strength: 3,
				Numeric:  true,
			},
			in:   "a10 a2 a01 a1x",
			want: "a01 a1x a2 a10",
		},
		{
			name:   "prefix",
			locale: "ja",
			typ:    "standard",
			opts:   tertiary,
			in:     "カー カア",
			want:   "カー カア",
		},
	} {
		c := collate.New(tc.locale, tc.typ, tc.opts)
		got := strings.Fields(tc.in)
		slices.SortStableFunc(got, c.Compare)
		if s := strings.Join(got, " "); s != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, s, tc.want)
		}
	}
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts collate.Options
		a, b string
		want int
	}{
		{
			name: "canonical equivalence",
			opts: collate.Options{
				Strength: 3,
			},
			a:    "ạ̈",
			b:    "ạ̈",
			want: 0,
		},
		{
			name: "base",
			opts: collate.Options{
				Strength: 1,
			},
			a:    "á",
			b:    "A",
			want: 0,
		},
		{
			name: "accent",
			opts: collate.Options{
				Strength: 2,
			},
			a:    "á",
			b:    "a",
			want: 1,
		},
		{
			name: "case level",
			opts: collate.Options{
				Strength:  1,
				CaseLevel: true,
			},
			a:    "a",
			b:    "á",
			want: 0,
		},
		{
			name: "case level with cases",
			opts: collate.Options{
				Strength:  1,
				CaseLevel: true,
			},
			a:    "a",
			b:    "A",
			want: -1,
		},
		{
			name: "punctuation ignored",
			opts: collate.Options{
				Strength:          3,
				IgnorePunctuation: true,
			},
			a:    "a-b",
			b:    "ab",
			want: 0,
		},
		{
			name: "punctuation",
			opts: collate.Options{
				Strength: 3,
			},
			a:    "a-b",
			b:    "ab",
			want: -1,
		},
	} {
		c := collate.New("und", "standard", tc.opts)
		if got := c.Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Compare(%q, %q): got %d, want %d", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNumericSegmentLeadingZeros(t *testing.T) {
	c := collate.New("en", "standard", collate.Options{
		Strength: 3,
		Numeric:  true,
	})
	segment := strings.Repeat("9", 254)
	if got := c.Compare("0"+segment+"001", segment+"1"); got != 0 {
		t.Errorf("leading zeros in successive numeric segments: got %d, want 0", got)
	}
}

func TestBackwardSecondary(t *testing.T) {
	// French of Canada compares the accents from the end of the strings.
	for _, tc := range []struct {
		locale string
		want   int
	}{
		{
			locale: "fr",
			want:   -1,
		},
		{
			locale: "fr-CA",
			want:   1,
		},
	} {
		c := collate.New(tc.locale, "standard", collate.Options{
			Strength: 3,
		})
		if got := c.Compare("coté", "côte"); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.locale, got, tc.want)
		}
	}
}

func TestDefaults(t *testing.T) {
	for _, tc := range []struct {
		locale            string
		caseFirst         string
		ignorePunctuation bool
	}{
		{
			locale:    "en",
			caseFirst: "false",
		},
		{
			locale:    "da",
			caseFirst: "upper",
		},
		{
			locale:            "th",
			caseFirst:         "false",
			ignorePunctuation: true,
		},
	} {
		cf, ip := collate.Defaults(tc.locale, "standard")
		if cf != tc.caseFirst || ip != tc.ignorePunctuation {
			t.Errorf("Defaults(%q): got %q, %t; want %q, %t", tc.locale, cf, ip, tc.caseFirst, tc.ignorePunctuation)
		}
	}
}

func BenchmarkCompare(b *testing.B) {
	words := strings.Fields("The quick brown fox jumps over the lazy dog Ångström façade naïve coöperate résumé Zürich")
	for _, l := range []string{"und", "de", "zh"} {
		b.Run(l, func(b *testing.B) {
			c := collate.New(l, "standard", collate.Options{
				Strength: 3,
			})
			for b.Loop() {
				s := slices.Clone(words)
				slices.SortFunc(s, c.Compare)
			}
		})
	}
}
