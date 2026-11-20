// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"fmt"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestBigIntBitwiseSizeLimit(t *testing.T) {
	const bits = 128
	vm := engine.NewVMWithBigIntResultLimit(bits)
	builtin.Install(vm)
	setup := fmt.Sprintf(`
		const x = BigInt.asUintN(%d, -1n);
		function throwsRangeError(f) {
			try { f(); } catch (e) { return e instanceof RangeError; }
			return false;
		}
	`, bits)
	if _, err := runScript(vm, "setup", setup); err != nil {
		t.Fatal(err)
	}
	for _, expr := range []string{
		`throwsRangeError(() => ~x)`,
		`throwsRangeError(() => x ^ -1n)`,
		`throwsRangeError(() => -x & -2n)`,
		`(-x | x) === -1n`,
	} {
		got, err := runScript(vm, "bitwise", expr)
		if err != nil {
			t.Errorf("%s: %v", expr, err)
		} else if !engine.ToBoolean(got) {
			t.Errorf("%s is false", expr)
		}
	}
}
