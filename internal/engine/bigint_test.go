// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"fmt"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func TestBigIntBitwiseSizeLimit(t *testing.T) {
	const bits = 128
	cx := engine.NewExecutionContextWithBigIntResultLimit(bits)
	builtin.Install(cx)
	setup := fmt.Sprintf(`
		const x = BigInt.asUintN(%d, -1n);
		function throwsRangeError(f) {
			try { f(); } catch (e) { return e instanceof RangeError; }
			return false;
		}
	`, bits)
	if _, err := runScript(cx, "setup", setup); err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{
		`throwsRangeError(() => ~x)`,
		`throwsRangeError(() => x ^ -1n)`,
		`throwsRangeError(() => -x & -2n)`,
		`(-x | x) === -1n`,
	} {
		got, err := runScript(cx, "bitwise", expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
		} else if !storage.ToBoolean(got) {
			t.Errorf("%s is false", expr)
		}
	}
}

func TestBigIntArithmeticSizeLimit(t *testing.T) {
	cx := engine.NewExecutionContextWithBigIntResultLimit(128)
	builtin.Install(cx)
	if _, err := runScript(cx, "setup", `
		const max = BigInt.asUintN(128, -1n);
		const half = BigInt.asUintN(64, -1n);
		function throwsRangeError(f) {
			try { f(); } catch (e) { return e instanceof RangeError; }
			return false;
		}
	`); err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{
		`throwsRangeError(() => max + 1n)`,
		`throwsRangeError(() => -max - 1n)`,
		`throwsRangeError(() => max * 2n)`,
		`(half + 1n) * half === max - half`,
	} {
		got, err := runScript(cx, "arithmetic", expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
		} else if !storage.ToBoolean(got) {
			t.Errorf("%s is false", expr)
		}
	}
}
