// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func TestQuietBigIntAccumulation(t *testing.T) {
	if !engine.ValidateExec {
		t.Skip("requires instruction tracing")
	}
	cx := newTestContext(t)
	f, err := runScript(cx, "quiet-bigint.js", `
		function accumulate(initial) {
			let term = initial, sum = 1n << 64n;
			for (let i = 0; i < 8; i++) {
				sum += term * term;
				term += 1n;
			}
			return sum;
		}
		accumulate;
	`)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	engine.TraceQuietMultiplications(f, &count)
	if _, err := runScript(cx, "quiet-bigint-calls.js", `
		accumulate(1n << 27n);
		accumulate(1n << 123n);
	`); err != nil {
		t.Fatal(err)
	}
	if got, want := count, 14; got != want {
		t.Errorf("annotated multiplies in execQuiet: got %d, want %d", got, want)
	}
}

func TestQuietNumericConcatenation(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "concat.js", `
		const numbers = [9, 10, 105, 999, 1000, 99999999, -9999999, -10000000, -0, 12.5];
		const rightEmpty = numbers.map(n => n + "").join(",");
		const leftEmpty = numbers.map(n => "" + n).join(",");
		rightEmpty + " | " + leftEmpty;
	`)
	if err != nil {
		t.Fatal(err)
	}
	const numbers = "9,10,105,999,1000,99999999,-9999999,-10000000,0,12.5"
	const want = numbers + " | " + numbers
	if got := cx.ToString(v); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestQuietCallResumesAfterMethodRead(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "quiet.js", `
		Object.prototype.quietData = ":inherited;";
		Object.prototype.quietProbe = function() { "use strict"; return typeof this; };
		const boxed = {
			quietData: ":own;",
			quietProbe() { return this === boxed ? "object" : "wrong receiver"; }
		};
		function probe(x) {
			let result = "";
			const map = new Map();
			for (let i = 0; i < 3; ++i) {
				map.set(i, i);
				result = result + x.quietProbe() + x.quietData;
			}
			return result;
		}
		probe(123) + probe(boxed) + probe(456);
	`)
	if err != nil {
		t.Fatal(err)
	}
	const want = "number:inherited;number:inherited;number:inherited;" +
		"object:own;object:own;object:own;" +
		"number:inherited;number:inherited;number:inherited;"
	if got := cx.ToString(v); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestQuietRegExpLiteralAfterPrototypeChange(t *testing.T) {
	cx := newTestContext(t)
	v, err := runScript(cx, "regexp.js", `
		function probe() {
			const map = new Map();
			let result = 0;
			for (let i = 0; i < 4; ++i) {
				map.set(i, i);
				if (/a/g.test("a")) ++result;
			}
			return result;
		}
		const original = probe();
		let last;
		Object.defineProperty(RegExp.prototype, "test", { get() {
			const current = this;
			const fresh = last !== current;
			last = current;
			return function() { return fresh && this === current; };
		} });
		original === 4 && probe() === 4;
	`)
	if err != nil {
		t.Fatal(err)
	}
	if !storage.IsStrictlyEqual(v, storage.True) {
		t.Errorf("got %s, want true", cx.DisplayString(v))
	}
}
