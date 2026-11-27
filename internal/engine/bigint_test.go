// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
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

func TestBigIntMultiplyAddErrorPositions(t *testing.T) {
	cx := engine.NewExecutionContextWithBigIntResultLimit(128)
	builtin.Install(cx)
	const src = `(function(a, b, c) { return c + a * b; })`
	fn, err := runScript(cx, "multiply-add.js", src)
	if err != nil {
		t.Fatal(err)
	}
	code := fn.O.Fn.Code
	pc := slices.IndexFunc(code.Code, func(i bytecode.Instr) bool { return i.Op == bytecode.OpMulAdd })
	if pc < 0 {
		t.Fatal("expected a multiply followed by an add")
	}
	// Give each instruction a distinct source position so the reporter identifies its PC.
	mulPos, addPos := strings.IndexByte(src, '*'), strings.IndexByte(src, '+')
	code.Positions = []bytecode.Pos{
		{
			PC:     int32(pc),
			Offset: int32(mulPos),
		},
		{
			PC:     int32(pc + 1),
			Offset: int32(addPos),
		},
	}

	var loc engine.ExceptionLocation
	cx.SetExceptionReporter(func(thrown storage.Value, site engine.ExceptionLocation) {
		loc = site
		if cx.ProtoOf(thrown.O) != cx.Intrinsic(storage.IntrinsicRangeErrorPrototype) {
			t.Error("expected a RangeError")
		}
	})
	max, err := runScript(cx, "max.js", `BigInt.asUintN(128, -1n)`)
	if err != nil {
		t.Fatal(err)
	}
	minusMax, err := runScript(cx, "min.js", `-BigInt.asUintN(128, -1n)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		limit  int64
		left   storage.Value
		factor int64
		addend storage.Value
		pos    int
	}{
		{
			name:   "product exceeds the limit even though the sum would fit",
			limit:  128,
			left:   max,
			factor: 2,
			addend: minusMax,
			pos:    mulPos,
		},
		{
			name:   "only the sum exceeds the limit",
			limit:  128,
			left:   max,
			factor: 1,
			addend: storage.BigIntFromInt64(1),
			pos:    addPos,
		},
		{
			name:   "int64 product exceeds a reduced limit",
			limit:  63,
			left:   storage.BigIntFromInt64(-1 << 62),
			factor: 2,
			addend: storage.BigIntFromInt64(1),
			pos:    mulPos,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc = engine.ExceptionLocation{}
			cx.MaxBigIntResultBits = tc.limit
			args := []storage.Value{tc.left, storage.BigIntFromInt64(tc.factor), tc.addend}
			if _, err := cx.HostCall(fn, storage.Undefined, args); err == nil {
				t.Error("expected a RangeError")
			}
			if loc.File != "multiply-add.js" || loc.Line != 1 || loc.Column != tc.pos+1 {
				t.Errorf("got throw site %+v, want multiply-add.js:1:%d", loc, tc.pos+1)
			}
		})
	}
}

func TestBigIntMultiplyAddInterrupt(t *testing.T) {
	cx := newTestContext(t)
	fn, err := runScript(cx, "multiply-add", `(function(interrupt, a, b, c) {
		interrupt();
		return a * b + c;
	})`)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("stop before multiplication")
	interrupt := cx.NewFunction("interrupt", func(cx *engine.ExecutionContext, _ storage.Value, _ []storage.Value) storage.Value {
		cx.Interrupt(cause)
		return storage.Undefined
	})
	args := []storage.Value{
		interrupt,
		storage.BigIntFromInt64(4_000_000_000),
		storage.BigIntFromInt64(5_000_000_000),
		storage.BigIntFromInt64(1),
	}
	if _, err := cx.HostCall(fn, storage.Undefined, args); !errors.Is(err, cause) {
		t.Errorf("got %v, want %v", err, cause)
	}
}
