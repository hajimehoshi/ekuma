// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestOwnKeysOfManyIndicesPoll(t *testing.T) {
	cause := errors.New("stop")
	for _, tc := range []struct {
		name string
		list func(cx *engine.ExecutionContext, o *engine.Object)
	}{
		{
			name: "EnumerableOwnKeys",
			list: func(cx *engine.ExecutionContext, o *engine.Object) { cx.EnumerableOwnKeys(o, nil) },
		},
	} {
		cx := newVM(t)
		returned := false
		list := cx.NewFunction("list", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			// The interruption is seen at the next poll.
			cx.Interrupt(cause)
			tc.list(cx, args[0].Object())
			returned = true
			return engine.Undefined
		})
		if err := cx.HostSet(cx.GlobalObject(), cx.StringFromGo("list"), list); err != nil {
			t.Fatalf("HostSet: %v", err)
		}
		// Scattered indices make a sparse object whose index keys need sorting.
		_, err := runScript(cx, "keys", `o = {}; for (i = 0; i < 2e5; i++) o[(i * 2654435761) % 4294967291] = 1; list(o);`)
		if interrupted, ok := errors.AsType[*engine.InterruptedError](err); !ok || interrupted.Cause != cause {
			t.Errorf("%s: got %v, want an interruption by the cause", tc.name, err)
		}
		if returned {
			t.Errorf("%s: the listing of %d keys finished without seeing the interruption", tc.name, 200000)
		}
	}
}
