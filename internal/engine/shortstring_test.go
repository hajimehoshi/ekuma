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
