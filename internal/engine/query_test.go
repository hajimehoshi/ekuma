// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestQueryOfMissingKeyNotInterned(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{
			name:  "get",
			query: `o[k]`,
		},
		{
			name:  "in",
			query: `k in o`,
		},
		{
			name:  "Object.hasOwn",
			query: `Object.hasOwn(o, k)`,
		},
		{
			name:  "hasOwnProperty",
			query: `o.hasOwnProperty(k)`,
		},
		{
			name:  "Reflect.has",
			query: `Reflect.has(o, k)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newVM(t)
			before := engine.InternedCount(cx)
			if _, err := runScript(cx, "query", `
				o = {};
				for (let i = 0; i < 10000; i++) {
					const k = "k" + i;
					`+tc.query+`;
				}
			`); err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if got := engine.InternedCount(cx); got != before {
				t.Errorf("got %d interned strings, want %d", got, before)
			}
		})
	}
}

// TestQueryKeyThroughProxy tests that the checks after a trap find the property that the trap
// created with a key that was not interned before.
func TestQueryKeyThroughProxy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		trap  string
		query string
	}{
		{
			name:  "get",
			trap:  `get(t, k) { define(t, k); return 2; }`,
			query: `p[k]`,
		},
		{
			name:  "has",
			trap:  `has(t, k) { define(t, k); return false; }`,
			query: `k in p`,
		},
		{
			name:  "getOwnPropertyDescriptor",
			trap:  `getOwnPropertyDescriptor(t, k) { define(t, k); return undefined; }`,
			query: `Object.hasOwn(p, k)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newVM(t)
			v, err := runScript(cx, "proxy", `
				function define(t, k) {
					Object.defineProperty(t, k, {value: 1});
				}
				const p = new Proxy({}, {`+tc.trap+`});
				const k = "fresh" + 1;
				try {
					`+tc.query+`;
					false;
				} catch (e) {
					e instanceof TypeError;
				}
			`)
			if err != nil {
				t.Fatalf("runScript: %v", err)
			}
			if !engine.ToBoolean(v) {
				t.Errorf("got no TypeError for a trap result that violates an invariant")
			}
		})
	}
}
