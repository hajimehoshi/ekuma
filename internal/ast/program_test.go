// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/ast"
)

func TestPositioner(t *testing.T) {
	long := strings.Repeat("aé𝒜", 200)
	src := "a\r\nbé\rc d " + long + "\n" + long + "\r\n\r\nz"

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
