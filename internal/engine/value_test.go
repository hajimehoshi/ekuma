// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func TestDisplayStringLoneSurrogates(t *testing.T) {
	cx, _ := engine.NewExecutionContext(nil)
	// A low surrogate before a high one makes two lone surrogates in a row.
	s := wtf8.FromUTF16([]uint16{'a', 0xDE00, 0xD83D})
	for _, tc := range []struct {
		name string
		v    engine.Value
		want string
	}{
		{
			name: "string",
			v:    cx.NewString(s),
			want: "a��",
		},
		{
			name: "symbol",
			v:    engine.ObjectValue(cx.NewSymbol(s, true)),
			want: "Symbol(a��)",
		},
	} {
		if got := cx.DisplayString(tc.v); got != tc.want {
			t.Errorf("%s: DisplayString = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestZeroValueUndefined(t *testing.T) {
	var v engine.Value
	if !v.IsUndefined() || v.IsNumber() || engine.ToBoolean(v) {
		t.Error("zero value must be undefined, nonnumeric, and falsy")
	}
}
