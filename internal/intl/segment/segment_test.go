// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package segment_test

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/segment"
)

func TestBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    segment.Granularity
		in   string
		want []string
	}{
		{
			name: "combining mark and CRLF",
			g:    segment.Grapheme,
			in:   "é\r\nx",
			want: []string{"é", "\r\n", "x"},
		},
		{
			name: "emoji ZWJ sequence and flags",
			g:    segment.Grapheme,
			in:   "\U0001F469‍\U0001F4BB\U0001F1EF\U0001F1F5\U0001F1FA",
			want: []string{"\U0001F469‍\U0001F4BB", "\U0001F1EF\U0001F1F5", "\U0001F1FA"},
		},
		{
			name: "Indic conjunct",
			g:    segment.Grapheme,
			in:   "क्ष",
			want: []string{"क्ष"},
		},
		{
			name: "words",
			g:    segment.Word,
			in:   "can't stop 3.14, e.g.",
			want: []string{"can't", " ", "stop", " ", "3.14", ",", " ", "e.g", "."},
		},
		{
			name: "katakana and ideographs",
			g:    segment.Word,
			in:   "カタカナ漢字",
			want: []string{"カタカナ", "漢", "字"},
		},
		{
			name: "sentences",
			g:    segment.Sentence,
			in:   "Mr. Smith left. (He did!) ok",
			want: []string{"Mr. ", "Smith left. ", "(He did!) ", "ok"},
		},
	} {
		rs := []rune(tc.in)
		var got []string
		bs := segment.Boundaries(tc.g, rs)
		for i := 1; i < len(bs); i++ {
			got = append(got, string(rs[bs[i-1]:bs[i]]))
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIsWordLike(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{
			in:   "word",
			want: true,
		},
		{
			in:   "42",
			want: true,
		},
		{
			in:   "漢",
			want: true,
		},
		{
			in:   " ",
			want: false,
		},
		{
			in:   "!",
			want: false,
		},
	} {
		if got := segment.IsWordLike([]rune(tc.in)); got != tc.want {
			t.Errorf("IsWordLike(%q): got %t, want %t", tc.in, got, tc.want)
		}
	}
}
