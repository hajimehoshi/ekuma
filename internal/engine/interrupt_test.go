// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestInterruptWithoutCause(t *testing.T) {
	cx := newTestContext(t)
	cx.Interrupt(nil)
	_, err := runScript(cx, "interrupted", `while (true) {}`)
	interrupted, ok := errors.AsType[*engine.InterruptedError](err)
	if !ok {
		t.Fatalf("got %v, want *engine.InterruptedError", err)
	}
	if interrupted.Cause != nil {
		t.Errorf("got cause %v, want nil", interrupted.Cause)
	}
}
