// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"runtime"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// runScript parses src as a script of cx and evaluates it.
func runScript(cx *engine.ExecutionContext, file, src string) (storage.Value, error) {
	s, err := cx.ParseScript(file, wtf8.FromValidUTF8(src))
	if err != nil {
		return storage.Undefined, err
	}
	v, _, err := s.ScriptEvaluation()
	return v, err
}

func newTestContext(t *testing.T) *engine.ExecutionContext {
	t.Helper()
	cx, _ := engine.NewExecutionContext(nil)
	builtin.Install(cx)
	if _, err := runScript(cx, "init", `var o, i = 0;`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	return cx
}

func TestDynamicKeysCollected(t *testing.T) {
	const (
		rounds = 20
		keys   = 5000
		max    = 4 * keys
	)
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "assign",
			src:  `for (let n = i + 5000; i < n; i++) { o = {}; o["k" + i] = i; }`,
		},
		{
			name: "JSON.parse",
			src:  `for (let n = i + 5000; i < n; i++) { o = JSON.parse('{"k' + i + '":' + i + '}'); }`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			if _, err := runScript(cx, "keep", `var kept = {}; kept["k" + 0] = 0;`); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			for range rounds {
				if _, err := runScript(cx, "round", tc.src); err != nil {
					t.Fatalf("runScript: %v", err)
				}
				runtime.GC()
			}
			if got := engine.InternedCount(cx); got > max {
				t.Errorf("got %d interned strings after %d keys, want at most %d", got, rounds*keys, max)
			}
			if got := engine.ObjectTransitionCount(cx); got > max {
				t.Errorf("got %d transitions after %d keys, want at most %d", got, rounds*keys, max)
			}
			// The key of a live object stays the same key.
			v, err := runScript(cx, "check", `kept["k" + 0] === 0 && JSON.stringify(Object.assign({}, kept)) === '{"k0":0}'`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if !storage.ToBoolean(v) {
				t.Errorf("the property of a live object was not found")
			}
		})
	}
}
