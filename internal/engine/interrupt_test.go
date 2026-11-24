// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

// TestSearchPollsForLongPattern checks that preparing the search for a long pattern that could
// match inside a surrogate pair polls.
func TestSearchPollsForLongPattern(t *testing.T) {
	cx := newTestContext(t)
	const parts = 4
	// The text is too short for reading it to poll.
	text := cx.NewString("é" + wtf8.String(strings.Repeat("x", 100)))
	// The pattern starts with a lone low surrogate.
	sub := cx.NewString(wtf8.String("\xed\xb0\x80" + strings.Repeat("x", parts*engine.PollInterval)))
	var polls int
	if got := text.UnitIndexOf(sub, 0, func() { polls++ }); got != -1 {
		t.Errorf("UnitIndexOf = %d, want -1", got)
	}
	// Both the conversion of the pattern and its border table take polls.
	if want := 2 * parts; polls < want {
		t.Errorf("got %d polls, want at least %d", polls, want)
	}
}
