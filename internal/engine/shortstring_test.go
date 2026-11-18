// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"strings"
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
			name:          "interned pattern",
			text:          "😀a",
			pattern:       "\xed\xa0\xbd",
			start:         0,
			end:           1,
			internPattern: true,
			want:          true,
		},
		{
			name:          "oversized interned pattern",
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
		t.Error("strings did not follow UTF-16 ordering")
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

func BenchmarkShortStringLeadingSurrogate(b *testing.B) {
	vm := engine.NewVM(nil)
	v := vm.NewString("😀😀")
	for b.Loop() {
		v.UnitAt(0)
	}
}

func TestCodePointAtCachedLowSurrogate(t *testing.T) {
	vm := engine.NewVM(nil)
	const prefixUnits = 90
	v := vm.NewString(strings.Repeat("日", prefixUnits) + "😀")
	if got := v.CodePointAt(prefixUnits + 1); got != 0xde00 {
		t.Errorf("CodePointAt(low) = %#x, want %#x", got, 0xde00)
	}
	if got := v.CodePointAt(prefixUnits); got != 0x1f600 {
		t.Errorf("CodePointAt(lead) = %#x, want %#x", got, 0x1f600)
	}
}

func TestStringValueBoundaries(t *testing.T) {
	vm := newVM(t)
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "empty",
			src:  `""`,
			want: "",
		},
		{
			name: "ASCII",
			src:  `String.fromCharCode(120)`,
			want: "x",
		},
		{
			name: "literal at byte limit",
			src:  `"abc\0defg"`,
			want: "abc\x00defg",
		},
		{
			name: "boxed string",
			src:  `new String("😀😀").valueOf()`,
			want: "😀😀",
		},
		{
			name: "named key",
			src:  `Object.keys({"ab\0": 1})[0]`,
			want: "ab\x00",
		},
		{
			name: "cached index key",
			src:  `Object.keys({123: 1}); Object.keys({123: 2})[0]`,
			want: "123",
		},
		{
			name: "typeof",
			src:  `typeof 1`,
			want: "number",
		},
		{
			name: "above byte limit",
			src:  `"日日日"`,
			want: "日日日",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := runScript(vm, tc.name, tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := v.Str(); got != tc.want {
				t.Errorf("Str() = %q, want %q", got, tc.want)
			}
			if !engine.IsStrictlyEqual(v, vm.NewString(tc.want)) {
				t.Error("value differs from NewString")
			}
		})
	}
}

func TestInternedStringValue(t *testing.T) {
	vm := engine.NewVM(nil)
	const text = "\xed\xa0\xbdabc"
	v := vm.NewString(text)
	for _, tc := range []struct {
		name  string
		value engine.Value
	}{
		{
			name:  "interned",
			value: engine.ObjectValue(vm.Intern(text)),
		},
		{
			name:  "shared",
			value: vm.SharedString(text),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !engine.IsStrictlyEqual(tc.value, v) {
				t.Error("interned value differs from NewString")
			}
		})
	}
}
