// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func TestReplacementCapacity(t *testing.T) {
	if got := builtin.ReplacementCapacity(5, 3, 0, 2); got != 0 {
		t.Errorf("capacity for a negative estimate = %d, want 0", got)
	}
	if got := builtin.ReplacementCapacity(storage.MaxStringLength-2, 1, 2, 2); got != storage.MaxStringLength {
		t.Errorf("capacity at the limit = %d, want %d", got, storage.MaxStringLength)
	}
	if got := builtin.ReplacementCapacity(storage.MaxStringLength-1, 1, 2, 2); got != 0 {
		t.Errorf("capacity above the limit = %d, want 0", got)
	}
}
