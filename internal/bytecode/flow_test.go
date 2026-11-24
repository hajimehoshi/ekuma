// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode_test

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

func TestMayWrite(t *testing.T) {
	const numRegs = 6
	for _, tc := range []struct {
		name string
		in   bytecode.Instr
		want []int
	}{
		{
			name: "one register",
			in: bytecode.Instr{
				Op: bytecode.OpMove,
				A:  1,
				B:  2,
			},
			want: []int{1},
		},
		{
			name: "through the last register",
			in: bytecode.Instr{
				Op: bytecode.OpCall,
				A:  0,
				B:  2,
			},
			want: []int{0, 4, 5},
		},
		{
			name: "no register",
			in: bytecode.Instr{
				Op: bytecode.OpReturn,
				A:  1,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []int
			for r := range numRegs {
				if bytecode.MayWrite([]bytecode.Instr{tc.in}, 0, r) {
					got = append(got, r)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandlerCovers(t *testing.T) {
	h := bytecode.Handler{
		Start: 1,
		End:   3,
	}
	for pc, want := range []bool{false, true, true, false} {
		if got := h.Covers(pc); got != want {
			t.Errorf("pc %d: got %t, want %t", pc, got, want)
		}
	}
}
