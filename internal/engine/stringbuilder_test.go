// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestStringFromCodeUnits(t *testing.T) {
	vm := engine.NewVM(nil)
	for _, tc := range []struct {
		name  string
		units []uint16
		want  wtf8.String
	}{
		{
			name:  "pair joined in a short string",
			units: []uint16{'a', 'b', 'c', 0xd83d, 0xde00, 'x'},
			want:  "abc😀x",
		},
		{
			name:  "pair joined where the string stops being short",
			units: []uint16{'a', 'b', 'c', 'd', 'e', 0xd83d, 0xde00, 'x'},
			want:  "abcde😀x",
		},
		{
			name:  "more units than a short string has bytes",
			units: []uint16{'a', 'b', 'c', 'd', 'e', 'f', 'g', 0xd83d, 0xde00},
			want:  "abcdefg😀",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := vm.StringFromCodeUnits(tc.units).Str(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodePointsToStringJoinsSurrogates(t *testing.T) {
	vm := engine.NewVM(nil)
	points := []rune{'a', 'b', 'c', 'd', 'e', 'f', 'g', 0xd83d, 0xde00}
	if got, want := vm.CodePointsToString(points).Str(), wtf8.String("abcdefg😀"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
