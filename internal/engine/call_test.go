// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"
)

func TestLeafCalls(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "leaf", `
		function one() { return 1; }
		function second(a, b) { return b; }
		function sum(seed, n) {
			let result = seed;
			for (let i = 0; i < n; i++) {
				result += one();
				result = second(8, 2) + result;
			}
			return result;
		}
		function counter(x) {
			function inc() { x++; }
			function dec() { x--; }
			inc();
			inc();
			return () => { dec(); dec(); inc(); return x; };
		}
		function omitted() { second(1, 99); return second(2) === undefined; }
		[sum(0, 3), sum("", 2), omitted(), omitted(), counter(4)(), counter(4n)()].join("|");
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cx.ToString(v), "9|2211|true|true|5|5"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSurplusArgumentsAcrossInlineCalls(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "surplus", `
		function read(a) {
			"use strict";
			a += arguments[1];
			return a + arguments[2];
		}
		function mapped(a) {
			a += 1;
			const args = arguments;
			const inspect = () => a;
			args[0] += read(1, 2, 3);
			return [args, inspect];
		}
		function observe(f) { return f.arguments[2]; }
		function legacy(a) { return a + observe(legacy); }
		function run(seed) {
			let total = seed;
			for (let i = 0; i < 3; i++) total += read(i, 4, 5);
			const [a, inspect] = mapped(10, 20, 30);
			mapped(40, 50, 60);
			return [total, a[0], a[1], a[2], inspect(), legacy(2, 7, 8)].join("|");
		}
		run(0);
		run(1);
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cx.ToString(v), "31|17|20|30|17|10"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
