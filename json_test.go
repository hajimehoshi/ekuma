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

// TestJSON covers the behaviour of JSON.parse and JSON.stringify that depends on the
// representation of strings and on the depth of nesting.
func TestJSON(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a gap whose copies form a pair",
			src:  `JSON.stringify([[1]], null, "\uDC00\uD800").split("\n")[2] === "\uDC00𐀀\uD8001"`,
			want: "true",
		},
		{
			name: "index keys and repeated index keys",
			src: `var o = JSON.parse('{"9999999":1,"1":2,"x":3,"0":4,"9999999":5,"1":6}');
				Object.keys(o).map(function(k) { return k + "=" + o[k]; }).join()`,
			want: "0=4,1=6,9999999=5,x=3",
		},
		{
			name: "deep nesting parses and a deep reviver walk or stringify throws a RangeError",
			src: `var n = 100000, a = JSON.parse("[".repeat(n) + "]".repeat(n)), d = 0;
				for (var x = a; x.length; x = x[0]) d++;
				var r = [d];
				try { JSON.stringify(a); } catch (e) { r.push(e instanceof RangeError); }
				try { JSON.parse("[".repeat(n) + "]".repeat(n), function(k, v) { return v; }); } catch (e) { r.push(e instanceof RangeError); }
				r.join()`,
			want: "99999,true,true",
		},
		{
			name: "stringify called from toJSON",
			src:  `JSON.stringify({a: {toJSON: function() { return JSON.stringify([1, "b"]); }}, c: "d"})`,
			want: `{"a":"[1,\"b\"]","c":"d"}`,
		},
		{
			name: "parse after a syntax error",
			src: `try { JSON.parse('[1,{"a":[2,'); } catch (e) {}
				JSON.stringify(JSON.parse('[3,{"b":4}]'))`,
			want: `[3,{"b":4}]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := ekuma.New()
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

// TestCancelJSON checks that parsing a long text can be stopped.
func TestCancelJSON(t *testing.T) {
	vm := ekuma.New()
	if _, err := vm.RunScript(t.Context(), "setup", `var text = "[" + "[0],".repeat(5000000) + "0]";`); err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := vm.RunScript(ctx, "parse", `JSON.parse(text)`); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("got %v, want context.DeadlineExceeded", err)
	}
	v, err := vm.RunScript(t.Context(), "after", `JSON.stringify(JSON.parse('{"a":[1]}'))`)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if got, want := v.String(), `{"a":[1]}`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
