// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
	sub := cx.NewString(wtf8.String("\xed\xb0\x80" + strings.Repeat("x", parts*storage.PollInterval)))
	var polls int
	if got := text.UnitIndexOf(sub, 0, func() { polls++ }); got != -1 {
		t.Errorf("UnitIndexOf = %d, want -1", got)
	}
	// Both the conversion of the pattern and its border table take polls.
	if want := 2 * parts; polls < want {
		t.Errorf("got %d polls, want at least %d", polls, want)
	}
}

// TestLongPassesPoll checks that a pass over many properties or elements of one object stops when
// an interruption is pending.
func TestLongPassesPoll(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		pass func(cx *engine.ExecutionContext, o *storage.Object)
	}{
		{
			name: "search for an enumerable property",
			src:  `for (i = 0; i < 1 << 16; i++) Object.defineProperty(o, "p" + i, { value: 0 });`,
			pass: func(cx *engine.ExecutionContext, o *storage.Object) {
				o.Shape().HasEnumerableString()
			},
		},
		{
			name: "search for an element",
			src:  `for (i = 0; i < 1 << 16; i++) o[i] = 0; for (i in o) delete o[i];`,
			pass: func(cx *engine.ExecutionContext, o *storage.Object) {
				o.HasElement()
			},
		},
		{
			name: "copy of elements",
			src:  `o.p = 0; for (i = 0; i <= 1 << 20; i++) o[i] = 0;`,
			pass: func(cx *engine.ExecutionContext, o *storage.Object) {
				cx.NewOrdinaryObject().TakeDataProperties(o)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			pass := cx.NewFunction("pass", func(cx *engine.ExecutionContext, _ storage.Value, args []storage.Value) storage.Value {
				cx.Interrupt(nil)
				tc.pass(cx, args[0].Object())
				t.Error("the pass ended")
				return storage.Undefined
			})
			if err := cx.HostSet(cx.GlobalObject(), cx.StringFromGo("pass"), pass); err != nil {
				t.Fatal(err)
			}
			if _, err := runScript(cx, "pass", `var i, o = {}; `+tc.src+` pass(o);`); err == nil {
				t.Error("the script was not interrupted")
			}
		})
	}
}
