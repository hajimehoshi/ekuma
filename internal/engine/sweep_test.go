// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"runtime"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func newVM(t *testing.T) *engine.VM {
	t.Helper()
	vm := engine.NewVM(nil)
	builtin.Install(vm)
	if _, err := vm.RunScript("init", `var o, i = 0;`); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	return vm
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
			vm := newVM(t)
			if _, err := vm.RunScript("keep", `var kept = {}; kept["k" + 0] = 0;`); err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			for range rounds {
				if _, err := vm.RunScript("round", tc.src); err != nil {
					t.Fatalf("RunScript: %v", err)
				}
				runtime.GC()
			}
			if got := engine.InternedCount(vm); got > max {
				t.Errorf("got %d interned strings after %d keys, want at most %d", got, rounds*keys, max)
			}
			if got := engine.ObjectTransitionCount(vm); got > max {
				t.Errorf("got %d transitions after %d keys, want at most %d", got, rounds*keys, max)
			}
			// The key of a live object stays the same key.
			v, err := vm.RunScript("check", `kept["k" + 0] === 0 && JSON.stringify(Object.assign({}, kept)) === '{"k0":0}'`)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if !engine.ToBoolean(v) {
				t.Errorf("the property of a live object was not found")
			}
		})
	}
}
