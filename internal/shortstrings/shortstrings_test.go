// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package shortstrings_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
)

func TestUnitAt(t *testing.T) {
	cases := []struct {
		text  string
		units []uint16
	}{
		{
			text:  "a😀日",
			units: []uint16{'a', 0xd83d, 0xde00, 0x65e5},
		},
		{
			text:  "é日a",
			units: []uint16{0xe9, 0x65e5, 'a'},
		},
		{
			text:  "😀😀",
			units: []uint16{0xd83d, 0xde00, 0xd83d, 0xde00},
		},
		{
			text:  "\U00010000\U0010ffff",
			units: []uint16{0xd800, 0xdc00, 0xdbff, 0xdfff},
		},
		{
			text:  "\xed\xa0\x80",
			units: []uint16{0xd800},
		},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			data := shortstrings.Pack(tc.text)
			for i, want := range tc.units {
				if got := shortstrings.UnitAt(data, i); got != want {
					t.Errorf("UnitAt(%d) = %x, want %x", i, got, want)
				}
			}
		})
	}
}

func TestLen(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{
			text: "",
			want: 0,
		},
		{
			text: "abcdefgh",
			want: 8,
		},
		{
			text: "\x00é日",
			want: 3,
		},
		{
			text: "😀😀",
			want: 4,
		},
		{
			text: "\xed\xa0\x80",
			want: 1,
		},
	} {
		if got := shortstrings.Len(shortstrings.Pack(tc.text), len(tc.text)); got != tc.want {
			t.Errorf("Len(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestSlice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		text  string
		start int
		end   int
		want  string
	}{
		{
			name:  "ASCII with NUL",
			text:  "ab\x00cdefg",
			start: 1,
			end:   4,
			want:  "b\x00c",
		},
		{
			name:  "full payload",
			text:  "abcdefgh",
			start: 0,
			end:   8,
			want:  "abcdefgh",
		},
		{
			name:  "empty inside pair",
			text:  "😀",
			start: 1,
			end:   1,
			want:  "",
		},
		{
			name:  "multibyte boundaries",
			text:  "é日a",
			start: 1,
			end:   3,
			want:  "日a",
		},
		{
			name:  "high surrogate",
			text:  "a😀日",
			start: 1,
			end:   2,
			want:  "\xed\xa0\xbd",
		},
		{
			name:  "low surrogate and suffix",
			text:  "a😀日",
			start: 2,
			end:   4,
			want:  "\xed\xb8\x80日",
		},
		{
			name:  "whole pair",
			text:  "😀😀",
			start: 0,
			end:   2,
			want:  "😀",
		},
		{
			name:  "both boundaries split pairs",
			text:  "😀😀",
			start: 1,
			end:   3,
			want:  "\xed\xb8\x80\xed\xa0\xbd",
		},
		{
			name:  "existing lone surrogate",
			text:  "\xed\xa0\x80x",
			start: 0,
			end:   1,
			want:  "\xed\xa0\x80",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, n := shortstrings.Slice(shortstrings.Pack(tc.text), tc.start, tc.end)
			if data != shortstrings.Pack(tc.want) || n != len(tc.want) {
				t.Errorf("Slice() = (%#x, %d), want (%#x, %d)", data, n, shortstrings.Pack(tc.want), len(tc.want))
			}
		})
	}
}

func TestSearch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      string
		pattern   string
		from      int
		wantIndex int
		wantLast  int
	}{
		{
			name:      "repeated ASCII",
			text:      "ababa",
			pattern:   "ba",
			from:      2,
			wantIndex: 3,
			wantLast:  1,
		},
		{
			name:      "full payload",
			text:      "abcdefgh",
			pattern:   "abcdefgh",
			from:      8,
			wantIndex: -1,
			wantLast:  0,
		},
		{
			name:      "padding is not content",
			text:      "a\x00",
			pattern:   "\x00\x00",
			from:      0,
			wantIndex: -1,
			wantLast:  -1,
		},
		{
			name:      "empty at end",
			text:      "é",
			pattern:   "",
			from:      1,
			wantIndex: 1,
			wantLast:  1,
		},
		{
			name:      "multibyte offsets",
			text:      "é日é",
			pattern:   "é",
			from:      1,
			wantIndex: 2,
			wantLast:  0,
		},
		{
			name:      "whole pair from low surrogate",
			text:      "😀😀",
			pattern:   "😀",
			from:      1,
			wantIndex: 2,
			wantLast:  0,
		},
		{
			name:      "high surrogate",
			text:      "😀😀",
			pattern:   "\xed\xa0\xbd",
			from:      1,
			wantIndex: 2,
			wantLast:  0,
		},
		{
			name:      "low surrogate",
			text:      "😀😀",
			pattern:   "\xed\xb8\x80",
			from:      1,
			wantIndex: 1,
			wantLast:  1,
		},
		{
			name:      "both boundaries split pairs",
			text:      "😀😀",
			pattern:   "\xed\xb8\x80\xed\xa0\xbd",
			from:      1,
			wantIndex: 1,
			wantLast:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, sub := shortstrings.Pack(tc.text), shortstrings.Pack(tc.pattern)
			if got := shortstrings.Index(data, len(tc.text), sub, len(tc.pattern), tc.from); got != tc.wantIndex {
				t.Errorf("Index() = %d, want %d", got, tc.wantIndex)
			}
			if got := shortstrings.LastIndex(data, len(tc.text), sub, len(tc.pattern), tc.from); got != tc.wantLast {
				t.Errorf("LastIndex() = %d, want %d", got, tc.wantLast)
			}
		})
	}
}
