// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package srcpos_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/srcpos"
)

func TestPosition(t *testing.T) {
	type position struct {
		offset int32
		line   int
		column int
	}
	for _, tc := range []struct {
		name      string
		src       string
		positions []position
	}{
		{
			// U+2014 shares its first two bytes with U+2028 and U+2029.
			name: "LF only",
			src:  "é—\nx\n",
			positions: []position{
				{offset: 0, line: 1, column: 1},
				{offset: 2, line: 1, column: 2},
				{offset: 5, line: 1, column: 3},
				{offset: 6, line: 2, column: 1},
				{offset: 8, line: 3, column: 1},
			},
		},
		{
			name: "LS and PS without CR",
			src:  "a b \nc",
			positions: []position{
				{offset: 1, line: 1, column: 2},
				{offset: 4, line: 2, column: 1},
				{offset: 5, line: 2, column: 2},
				{offset: 8, line: 3, column: 1},
				{offset: 9, line: 4, column: 1},
				{offset: 10, line: 4, column: 2},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := srcpos.NewTable(tc.src)
			for _, p := range tc.positions {
				if line, column := srcpos.Position(tc.src, int(p.offset)); line != p.line || column != p.column {
					t.Errorf("Position(%d): got %d:%d, want %d:%d", p.offset, line, column, p.line, p.column)
				}
				if line, column := lines.Position(p.offset); line != p.line || column != p.column {
					t.Errorf("Table.Position(%d): got %d:%d, want %d:%d", p.offset, line, column, p.line, p.column)
				}
			}
		})
	}
}

func TestTable(t *testing.T) {
	long := strings.Repeat("aé𝒜", 200)
	longASCII := strings.Repeat("abc", 400)
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "CR and non-ASCII",
			src:  "a\r\nbé\rc d " + long + "\n" + long + "\r\n\r\nz",
		},
		{
			name: "ASCII",
			src:  "a\nb c " + longASCII + "\n" + longASCII + "\n\nz",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The offsets are those of code points, apart from the one between CR and LF,
			// which no token starts at.
			var offsets []int
			for i := range tc.src {
				if i > 0 && tc.src[i-1] == '\r' && tc.src[i] == '\n' {
					continue
				}
				offsets = append(offsets, i)
			}
			offsets = append(offsets, len(tc.src))

			lines := srcpos.NewTable(tc.src)
			for _, o := range offsets {
				wantLine, wantColumn := srcpos.Position(tc.src, o)
				if line, column := lines.Position(int32(o)); line != wantLine || column != wantColumn {
					t.Errorf("Table.Position(%d): got %d:%d, want %d:%d", o, line, column, wantLine, wantColumn)
				}
			}
		})
	}
}
