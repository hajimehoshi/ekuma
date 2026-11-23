// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package segment_test

import (
	"slices"
	"strings"
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
			want: []string{"カタカナ", "漢字"},
		},
		{
			// NFKC expands ㌙ to two dictionary words, グラム and トン.
			name: "dictionary boundary inside a compatibility character",
			g:    segment.Word,
			in:   "猫㌙寿司",
			want: []string{"猫", "㌙", "寿司"},
		},
		{
			name: "unknown Thai and supplementary Han words",
			g:    segment.Word,
			in:   "ฅฅฅกิน𠀀𠀁",
			want: []string{"ฅฅฅ", "กิน", "𠀀", "𠀁"},
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
		bs := segment.Boundaries(tc.g, rs, "", nil)
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
			// Khitan letters have Word_Break=Other.
			in:   "\U00018b00",
			want: true,
		},
		{
			in:   "__",
			want: true,
		},
		{
			in:   "א'\u0300",
			want: false,
		},
		{
			// The Khitan filler is both Ideographic and Word_Break=Extend.
			in:   "\U00016fe4",
			want: true,
		},
		{
			in:   "a\u200d😀",
			want: false,
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
		if got := segment.IsWordLike([]rune(tc.in), nil); got != tc.want {
			t.Errorf("IsWordLike(%q): got %t, want %t", tc.in, got, tc.want)
		}
	}
}

// TestBoundariesLongRuns checks inputs whose rules look over long runs of code points.
func TestBoundariesLongRuns(t *testing.T) {
	const n = 1 << 20
	ri := []rune(strings.Repeat("\U0001F1E6", n))
	pairs := make([]int32, 0, n/2+1)
	for i := 0; i <= n; i += 2 {
		pairs = append(pairs, int32(i))
	}
	for _, tc := range []struct {
		name string
		g    segment.Granularity
		in   []rune
		want []int32
	}{
		{
			name: "regional indicators as graphemes",
			g:    segment.Grapheme,
			in:   ri,
			want: pairs,
		},
		{
			name: "regional indicators as words",
			g:    segment.Word,
			in:   ri,
			want: pairs,
		},
		{
			name: "spaces before an upper-case letter",
			g:    segment.Sentence,
			in:   []rune("a." + strings.Repeat(" ", n) + "A"),
			want: []int32{0, n + 2, n + 3},
		},
		{
			name: "spaces and digits before a lower-case letter",
			g:    segment.Sentence,
			in:   []rune("a." + strings.Repeat(" ", n) + strings.Repeat("1", n) + "a"),
			want: []int32{0, 2*n + 3},
		},
	} {
		if got := segment.Boundaries(tc.g, tc.in, "", nil); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %d boundaries, want %d", tc.name, len(got), len(tc.want))
		}
	}
}

// TestBoundariesPoll checks that the poll function can stop a long input.
func TestBoundariesPoll(t *testing.T) {
	check := func(t *testing.T, g segment.Granularity, input string) {
		t.Helper()
		type stop struct{}
		defer func() {
			if _, ok := recover().(stop); !ok {
				t.Errorf("Boundaries did not stop")
			}
		}()
		segment.Boundaries(g, []rune(input), "", func() { panic(stop{}) })
	}
	t.Run("sentence", func(t *testing.T) {
		check(t, segment.Sentence, strings.Repeat(" ", 1<<20))
	})
	t.Run("word dictionary", func(t *testing.T) {
		// The input is short enough that only dictionary matching needs to poll.
		check(t, segment.Word, strings.Repeat("研究生命起源", 1800))
	})
}
