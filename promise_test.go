// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// TestCancelJobs checks that a context stops a script whose jobs never run out, and that
// the jobs that were pending then are dropped without affecting later calls.
func TestCancelJobs(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "then",
			src:  `var count = 0; function f() { count++; Promise.resolve().then(f); } f();`,
		},
		{
			name: "await",
			src:  `var count = 0; (async () => { for (;;) { count++; await null; } })();`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			if _, err := vm.RunScript(ctx, "endless", tt.src); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("got %v, want context.DeadlineExceeded", err)
			}
			if _, err := vm.RunScript(t.Context(), "after", `var before = count; var r = 0; Promise.resolve(5).then(v => { r = v; });`); err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			v, err := vm.RunScript(t.Context(), "check", `[count > 0, count === before, r].join()`)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got, want := v.String(), "true,true,5"; got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestJobOrder checks when the jobs run relative to the script and to calls from Go.
func TestJobOrder(t *testing.T) {
	vm := ekuma.NewVM()
	call := vm.FuncOf("call", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		// The jobs do not run while the script that called this is still running.
		return args[0].Invoke(context.Background())
	})
	if err := vm.Global().Set("call", call); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, err := vm.RunScript(t.Context(), "order", `
		var log = [];
		Promise.resolve().then(() => log.push("job"));
		call(() => log.push("call"));
		log.push("script");
		throw new Error("thrown");
	`)
	if err == nil {
		t.Errorf("got no error, want the thrown one")
	}
	// The jobs run after a script that throws.
	v, err := vm.RunScript(t.Context(), "log", `log.join()`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), "call,script,job"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestForAwaitCloseOnReturn checks that closing the iterator of a for-await loop reports its
// error when a generator returns from inside the loop body.
func TestForAwaitCloseOnReturn(t *testing.T) {
	tests := []struct {
		name string
		ret  string
		want string
	}{
		{
			name: "throw",
			ret:  `throw "closed";`,
			want: "closed",
		},
		{
			name: "non-object result",
			ret:  `return 1;`,
			want: "TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.NewVM()
			src := `
				var it = { [Symbol.asyncIterator]() { return { next() { return { done: false }; }, return() { ` + tt.ret + ` } }; } };
				async function* g() { for await (var x of it) yield 1; }
				var got = "";
				var r = g();
				r.next().then(() => r.return()).then(() => { got = "fulfilled"; }, e => { got = e instanceof TypeError ? "TypeError" : String(e); });
			`
			if _, err := vm.RunScript(t.Context(), tt.name, src); err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			v, err := vm.RunScript(t.Context(), "got", `got`)
			if err != nil {
				t.Fatalf("RunScript: %v", err)
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
