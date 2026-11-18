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
