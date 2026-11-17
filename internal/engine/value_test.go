// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestZeroValueUndefined(t *testing.T) {
	var v engine.Value
	if !v.IsUndefined() || v.IsNumber() || engine.ToBoolean(v) {
		t.Error("zero value must be undefined, nonnumeric, and falsy")
	}
}
