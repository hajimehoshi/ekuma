// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/json"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// wtf8Of returns the WTF-8 string with the code units u.
func wtf8Of(u ...uint16) string {
	return wtf8.FromUTF16(u)
}

func TestScanString(t *testing.T) {
	pair := wtf8Of(0xD83D, 0xDE00)
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "escaped pair",
			text: `"😀"`,
			want: pair,
		},
		{
			name: "escaped high and raw low",
			text: `"\uD83D` + wtf8Of(0xDE00) + `"`,
			want: pair,
		},
		{
			name: "raw high and escaped low",
			text: `"` + wtf8Of(0xD83D) + `\uDE00"`,
			want: pair,
		},
		{
			name: "lone surrogates",
			text: `"` + wtf8Of(0xDE00) + `x\uD83D"`,
			want: wtf8Of(0xDE00, 'x', 0xD83D),
		},
	}
	var b wtf8.Builder
	for _, tt := range tests {
		got, end, ok := json.ScanString(tt.text, 0, &b)
		if !ok || end != len(tt.text) || got != tt.want {
			t.Errorf("%s: got %q, %d, %v, want %q, %d, true", tt.name, got, end, ok, tt.want, len(tt.text))
		}
	}
}

func TestAppendQuote(t *testing.T) {
	s := wtf8Of(0xDE00, 0xD83D, 0xDE00, 0x1F, '"', 0xD83D)
	if got, want := string(json.AppendQuote(nil, s)), `"\ude00`+wtf8Of(0xD83D, 0xDE00)+`\u001f\"\ud83d"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValid(t *testing.T) {
	tests := []struct {
		text string
		pos  int
		ok   bool
	}{
		{
			text: ` [1, {"a": [true, null], "b": {}}, [], "x"] `,
			pos:  44,
			ok:   true,
		},
		{
			text: `[1, 2}`,
			pos:  5,
		},
		{
			text: `{"a": 1,}`,
			pos:  8,
		},
		{
			text: `[[]`,
			pos:  3,
		},
		{
			text: `[] []`,
			pos:  3,
		},
	}
	for _, tt := range tests {
		if pos, ok := json.Valid(tt.text); pos != tt.pos || ok != tt.ok {
			t.Errorf("Valid(%q): got %d, %v, want %d, %v", tt.text, pos, ok, tt.pos, tt.ok)
		}
	}
}
