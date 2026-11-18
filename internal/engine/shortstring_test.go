// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func BenchmarkShortString(b *testing.B) {
	vm := engine.NewVM(nil)
	for _, s := range []string{"abc", "abcdefgh", "é日", "😀😀"} {
		b.Run(s, func(b *testing.B) {
			v := vm.NewString(s)
			b.Run("New", func(b *testing.B) {
				for b.Loop() {
					vm.NewString(s)
				}
			})
			b.Run("Equal", func(b *testing.B) {
				other := vm.NewString(s)
				for b.Loop() {
					engine.IsStrictlyEqual(v, other)
				}
			})
			b.Run("DifferentLength", func(b *testing.B) {
				other := vm.NewString("ab")
				for b.Loop() {
					engine.IsStrictlyEqual(v, other)
				}
			})
			b.Run("Length", func(b *testing.B) {
				for b.Loop() {
					v.StringLength()
				}
			})
			b.Run("UnitAt", func(b *testing.B) {
				for b.Loop() {
					v.UnitAt(1)
				}
			})
		})
	}
}

func BenchmarkShortStringSubstring(b *testing.B) {
	vm := engine.NewVM(nil)
	for _, s := range []string{"abcdefgh", "é日a", "😀😀"} {
		b.Run(s, func(b *testing.B) {
			v := vm.NewString(s)
			b.Run("Value", func(b *testing.B) {
				for b.Loop() {
					vm.Substring(v, 1, 3)
				}
			})
			b.Run("String", func(b *testing.B) {
				for b.Loop() {
					v.Substring(1, 3)
				}
			})
		})
	}
}

func TestSubstringEquals(t *testing.T) {
	vm := engine.NewVM(nil)
	for _, tc := range []struct {
		name          string
		text          string
		pattern       string
		start         int
		end           int
		internPattern bool
		want          bool
	}{
		{
			name:    "packed equality",
			text:    "ab\x00c",
			pattern: "ab\x00",
			start:   0,
			end:     3,
			want:    true,
		},
		{
			name:    "trailing NUL",
			text:    "ab\x00c",
			pattern: "ab\x00",
			start:   0,
			end:     2,
			want:    false,
		},
		{
			name:    "payload mismatch",
			text:    "abcd",
			pattern: "ac",
			start:   0,
			end:     2,
			want:    false,
		},
		{
			name:          "object pattern",
			text:          "😀a",
			pattern:       "\xed\xa0\xbd",
			start:         0,
			end:           1,
			internPattern: true,
			want:          true,
		},
		{
			name:          "oversized object pattern",
			text:          "abcdefgh",
			pattern:       "日日日",
			start:         0,
			end:           3,
			internPattern: true,
			want:          false,
		},
		{
			name:    "long receiver",
			text:    "abcdefghijk",
			pattern: "bc",
			start:   1,
			end:     3,
			want:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := vm.NewString(tc.text)
			pattern := vm.NewString(tc.pattern)
			if tc.internPattern {
				pattern = engine.ObjectValue(vm.Intern(tc.pattern))
			}
			if got := v.SubstringEquals(tc.start, tc.end, pattern); got != tc.want {
				t.Errorf("SubstringEquals() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestShortStringOrdering(t *testing.T) {
	vm := newVM(t)
	v, err := runScript(vm, "ordering", `
  const a = "\ud83d\ue000";
  const b = "😀!".slice(0, -1);
  a > b && b < a && a >= b && b <= a;
 `)
	if err != nil {
		t.Fatal(err)
	}
	if !engine.ToBoolean(v) {
		t.Error("mixed packed and object strings did not follow UTF-16 ordering")
	}
}

func TestToWellFormedLiteral(t *testing.T) {
	vm := newVM(t)
	v, err := runScript(vm, "well-formed-literal", `"\udfff\udbff".toWellFormed();`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.Str(), "��"; got != want {
		t.Errorf("toWellFormed() = %q, want %q", got, want)
	}
}
