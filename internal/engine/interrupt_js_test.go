// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// TestInterruptBuiltinFromGoroutine checks that a call of a built-in function that checks for an
// interruption only a few times lets another goroutine interrupt it, as js/wasm runs that
// goroutine only when the running one yields.
func TestInterruptBuiltinFromGoroutine(t *testing.T) {
	cx := newTestContext(t)
	// Object.assign checks for an interruption every PollInterval properties.
	n := strconv.Itoa(4 * engine.PollInterval)
	if _, err := runScript(cx, "setup", `var o = {}; for (let i = 0; i < `+n+`; i++) o["k" + i] = i;`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	go cx.Interrupt(nil)
	_, err := runScript(cx, "assign", `Object.assign({}, o)`)
	if _, ok := errors.AsType[*engine.InterruptedError](err); !ok {
		t.Errorf("got %v, want *engine.InterruptedError", err)
	}
}
