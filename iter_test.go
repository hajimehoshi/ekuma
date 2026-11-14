// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// TestIterationWithoutResults covers the iteration of built-in iterators and generators
// that does not create iterator results.
func TestIterationWithoutResults(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the next method of another kind of built-in iterator throws",
			src: `var it = new Set([1]).values();
				it.next = new Map().keys().next;
				var r;
				try { for (var x of it) {} r = "no error"; } catch (e) { r = e instanceof TypeError; }
				r`,
			want: "true",
		},
		{
			name: "the result that yield* passes through is read by the loop",
			src: `var reads = [];
				var inner = {
					[Symbol.iterator]() { return this; },
					next() {
						return {
							get done() { reads.push("done"); return false; },
							get value() { reads.push("value"); return 1; },
						};
					},
				};
				function* g() { yield* inner; }
				for (var x of g()) { reads.push(x); break; }
				reads.join()`,
			want: "done,done,value,1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			v, err := vm.RunScript(t.Context(), tt.name, tt.src)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := toString(t, vm, v); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
