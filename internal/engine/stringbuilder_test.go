// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

func TestStringBuilderSpill(t *testing.T) {
	vm := engine.NewVM(nil)
	var b xstrings.Builder
	vm.NewString("abc").WriteToBuilder(&b)
	b.WriteRune(0xd83d)
	first := vm.StringFromBuilder(&b)
	b.WriteUnit(0xde00)
	b.WriteString("x")
	packed := vm.StringFromBuilder(&b)
	b.WriteString("yzyzyzyzyz")
	if got := first.Str(); got != "abc\xed\xa0\xbd" {
		t.Errorf("first snapshot = %q", got)
	}
	if got := packed.Str(); got != "abc😀x" {
		t.Errorf("joined snapshot = %q", got)
	}
	if got := vm.StringFromBuilder(&b).Str(); got != "abc😀xyzyzyzyzyz" {
		t.Errorf("spilled result = %q", got)
	}
}
