// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/ast"
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
			src:  "é\u2014\nx\n",
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
			src:  "a\u2028b\u2029\nc",
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
			prog := &ast.Program{
				Source: tc.src,
			}
			pr := ast.NewPositioner(prog)
			for _, p := range tc.positions {
				if line, column := ast.Position(tc.src, int(p.offset)); line != p.line || column != p.column {
					t.Errorf("Position(%d): got %d:%d, want %d:%d", p.offset, line, column, p.line, p.column)
				}
				if line, column := prog.Position(p.offset); line != p.line || column != p.column {
					t.Errorf("Program.Position(%d): got %d:%d, want %d:%d", p.offset, line, column, p.line, p.column)
				}
				if line, column := pr.Position(p.offset); line != p.line || column != p.column {
					t.Errorf("Positioner.Position(%d): got %d:%d, want %d:%d", p.offset, line, column, p.line, p.column)
				}
			}
		})
	}
}

func TestPositioner(t *testing.T) {
	long := strings.Repeat("aé𝒜", 200)
	longASCII := strings.Repeat("abc", 400)
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "CR and non-ASCII",
			src:  "a\r\nbé\rc\u2028d\u2029" + long + "\n" + long + "\r\n\r\nz",
		},
		{
			name: "ASCII",
			src:  "a\nb c " + longASCII + "\n" + longASCII + "\n\nz",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testPositioner(t, tc.src)
		})
	}
}

func testPositioner(t *testing.T, src string) {
	// The offsets are those of code points, apart from the one between CR and LF, which no
	// token starts at.
	var offsets []int32
	for i := range src {
		if i > 0 && src[i-1] == '\r' && src[i] == '\n' {
			continue
		}
		offsets = append(offsets, int32(i))
	}
	offsets = append(offsets, int32(len(src)))

	backward := make([]int32, len(offsets))
	for i, o := range offsets {
		backward[len(offsets)-1-i] = o
	}
	shuffled := append([]int32(nil), offsets...)
	r := rand.New(rand.NewPCG(1, 2))
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	// Repeated offsets and short steps back, as in a compiled call.
	var mixed []int32
	for i, o := range offsets {
		mixed = append(mixed, o, o)
		if i >= 3 {
			mixed = append(mixed, offsets[i-3])
		}
	}

	for _, tc := range []struct {
		name    string
		offsets []int32
	}{
		{
			name:    "forward",
			offsets: offsets,
		},
		{
			name:    "backward",
			offsets: backward,
		},
		{
			name:    "shuffled",
			offsets: shuffled,
		},
		{
			name:    "mixed",
			offsets: mixed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prog := &ast.Program{
				Source: src,
			}
			pr := ast.NewPositioner(prog)
			for _, o := range tc.offsets {
				wantLine, wantColumn := ast.Position(src, int(o))
				if line, column := pr.Position(o); line != wantLine || column != wantColumn {
					t.Errorf("Positioner.Position(%d): got %d:%d, want %d:%d", o, line, column, wantLine, wantColumn)
				}
				if line, column := prog.Position(o); line != wantLine || column != wantColumn {
					t.Errorf("Program.Position(%d): got %d:%d, want %d:%d", o, line, column, wantLine, wantColumn)
				}
			}
		})
	}
}
