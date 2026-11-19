// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// TestLargeModuleGraphs imports module graphs whose handling took time quadratic in their
// sizes, so that the import missed a deadline that the linear handling meets by far.
func TestLargeModuleGraphs(t *testing.T) {
	if raceEnabled {
		t.Skip("the graphs are too large for the race detector")
	}
	for _, tc := range []struct {
		name string
		n    int

		// src returns the source text of the module mi; m1 is the one imported. The modules
		// whose sources are empty are synthetic.
		src func(n, i int) string

		// want is what the root module computes from the namespace ns of m1.
		want string
		expr string
	}{
		{
			// The requests of m1 are told apart while it is parsed, and its dependencies
			// are listed while it is evaluated.
			name: "many requests",
			n:    240000,
			src: func(n, i int) string {
				if i != 1 {
					return ""
				}
				var b strings.Builder
				for j := 2; j < n; j++ {
					fmt.Fprintf(&b, "import 'm%d';\n", j)
				}
				b.WriteString("export const done = true;")
				return b.String()
			},
			expr: "ns.done",
			want: "true",
		},
		{
			name: "many indirect exports",
			n:    80000,
			src: func(n, i int) string {
				var b strings.Builder
				for j := range n {
					if i == 1 {
						fmt.Fprintf(&b, "export {e%d as f%d} from 'm2';\n", j, j)
					} else {
						fmt.Fprintf(&b, "export const e%d = %d;\n", j, j)
					}
				}
				return b.String()
			},
			expr: "Object.keys(ns).length + ':' + ns.f12345",
			want: "80000:12345",
		},
		{
			// Each name of the namespace of m1 is exported by a module further down a chain
			// of export * from.
			name: "long chain of export * from",
			n:    16000,
			src: func(n, i int) string {
				if i+1 < n {
					return fmt.Sprintf("export * from 'm%d'; export const v%d = %d;", i+1, i, i)
				}
				return "export const last = 1;"
			},
			expr: "Object.keys(ns).length + ':' + ns.v15000",
			want: "15999:15000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			loaded := map[string]*ekuma.Module{}
			vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
				if m, ok := loaded[req.Specifier]; ok {
					return m, nil
				}
				i, err := strconv.Atoi(strings.TrimPrefix(req.Specifier, "m"))
				if err != nil {
					return nil, err
				}
				src := tc.src(tc.n, i)
				if src == "" {
					m := vm.NewSyntheticModule(req.Specifier, ekuma.Undefined)
					loaded[req.Specifier] = m
					return m, nil
				}
				m, err := vm.ParseModule(req.Specifier, src)
				if err != nil {
					return nil, err
				}
				loaded[req.Specifier] = m
				return m, nil
			})
			// The dynamic import loads, links and evaluates m1 within the deadline.
			root, err := vm.ParseModule("root", `const ns = await import("m1"); globalThis.result = String(`+tc.expr+`);`)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			p, err := root.Evaluate(ctx)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if state, reason := p.PromiseState(); state != ekuma.PromiseFulfilled {
				t.Fatalf("Evaluate: promise %v: %v", state, reason)
			}
			got, err := vm.Global().Get("result")
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("result = %s, want %s", got, tc.want)
			}
		})
	}
}
