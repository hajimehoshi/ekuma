// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestBigIntAsNWithLargeBits covers BigInt.asIntN and BigInt.asUintN with a bit count
// far beyond what a BigInt can hold, which test262 does not pin down.
func TestBigIntAsNWithLargeBits(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a value that already fits is returned",
			src:  `[BigInt.asIntN(2 ** 53 - 1, -5n), BigInt.asUintN(2 ** 53 - 1, 5n)].join()`,
			want: "-5,5",
		},
		{
			name: "a negative value for asUintN is too large",
			src:  `try { BigInt.asUintN(2 ** 53 - 1, -1n); "no error"; } catch (e) { e instanceof RangeError; }`,
			want: "true",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
