// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// The engine makes the accesses to a shared data block atomic, as other agents may access it.

// validateIntegerTypedArray implements ValidateIntegerTypedArray, with the access mode write when write is set,
// and returns the typed array and its length.
func validateIntegerTypedArray(cx *engine.ExecutionContext, v engine.Value, waitable, write bool, what string) (*engine.TypedArray, int) {
	_, t, n := validateTypedArrayAccess(cx, v, write, "Atomics."+what)
	typ := t.ElementType()
	if waitable {
		if typ != element.Int32 && typ != element.BigInt64 {
			cx.ThrowTypeError("Atomics." + what + ": the typed array must be an Int32Array or a BigInt64Array")
		}
	} else if !typ.IsInteger() {
		cx.ThrowTypeError("Atomics." + what + ": the typed array must be an integer typed array")
	}
	return t, n
}

// validateAtomicAccess implements ValidateAtomicAccess and returns the element index.
func validateAtomicAccess(cx *engine.ExecutionContext, n int, index engine.Value) int {
	i := toIndex(cx, index)
	if i >= int64(n) {
		cx.ThrowRangeError("invalid atomic access index")
	}
	return int(i)
}

// revalidateAtomicAccess implements RevalidateAtomicAccess for the element i.
func revalidateAtomicAccess(cx *engine.ExecutionContext, t *engine.TypedArray, i int) {
	n, ok := t.Length()
	if !ok {
		cx.ThrowTypeError("the typed array is detached or out of bounds")
	}
	if i >= n {
		cx.ThrowRangeError("invalid atomic access index")
	}
}

// toAtomicValue converts v as the Atomics functions do for an element of the type typ.
func toAtomicValue(cx *engine.ExecutionContext, typ element.Type, v engine.Value) engine.Value {
	if typ.IsBigInt() {
		return cx.ToBigInt(v)
	}
	return engine.Number(cx.ToIntegerOrInfinity(v))
}

// doWait implements DoWait.
func doWait(cx *engine.ExecutionContext, async bool, args []engine.Value) engine.Value {
	what := "wait"
	if async {
		what = "waitAsync"
	}
	t, n := validateIntegerTypedArray(cx, arg(args, 0), true, false, what)
	if !t.ArrayBuffer().IsShared() {
		cx.ThrowTypeError("Atomics." + what + ": the typed array must be backed by a SharedArrayBuffer")
	}
	i := validateAtomicAccess(cx, n, arg(args, 1))
	typ := t.ElementType()
	var v engine.Value
	if typ.IsBigInt() {
		v = cx.ToBigInt(arg(args, 2))
	} else {
		v = engine.Number(float64(cx.ToInt32(arg(args, 2))))
	}
	q := cx.ToNumber(arg(args, 3))
	timeout := math.Inf(1)
	if !math.IsNaN(q) {
		timeout = max(q, 0)
	}
	if !async && !cx.AgentCanSuspend() {
		cx.ThrowTypeError("Atomics.wait cannot be called in this context")
	}
	revalidateAtomicAccess(cx, t, i)
	r, isPromise := cx.Wait(t, i, engine.ElementBits(typ, v), timeout, async)
	if !async {
		return r
	}
	return waitResult(cx, isPromise, r)
}

// waitResult returns the result object of Atomics.waitAsync.
func waitResult(cx *engine.ExecutionContext, async bool, value engine.Value) engine.Value {
	o := cx.NewObjectFromProperties(
		[]engine.PropKey{engine.NamedKey(cx.Intern("async")), engine.NamedKey(cx.Intern("value"))},
		[]engine.Value{engine.Bool(async), value},
	)
	return engine.ObjectValue(o)
}

func initAtomics(cx *engine.ExecutionContext) {
	atomics := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Atomics"), engine.ObjectValue(atomics), engine.FlagsHidden)
	cx.Define(atomics, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Atomics"), engine.FlagConfigurable)

	// readModifyWrite implements AtomicReadModifyWrite with op on the encodings of the elements.
	readModifyWrite := func(name string, op engine.AtomicOp) {
		method(cx, atomics, wtf8.String(name), 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, name)
			i := validateAtomicAccess(cx, n, arg(args, 1))
			typ := t.ElementType()
			v := toAtomicValue(cx, typ, arg(args, 2))
			revalidateAtomicAccess(cx, t, i)
			return cx.ElementFromBits(typ, t.AtomicReadModifyWrite(i, op, engine.ElementBits(typ, v), 0))
		})
	}
	readModifyWrite("add", engine.AtomicAdd)
	readModifyWrite("and", engine.AtomicAnd)
	readModifyWrite("exchange", engine.AtomicExchange)
	readModifyWrite("or", engine.AtomicOr)
	readModifyWrite("sub", engine.AtomicSub)
	readModifyWrite("xor", engine.AtomicXor)

	method(cx, atomics, "compareExchange", 4, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, "compareExchange")
		i := validateAtomicAccess(cx, n, arg(args, 1))
		typ := t.ElementType()
		expected := toAtomicValue(cx, typ, arg(args, 2))
		replacement := toAtomicValue(cx, typ, arg(args, 3))
		revalidateAtomicAccess(cx, t, i)
		old := t.AtomicReadModifyWrite(i, engine.AtomicCompareExchange, engine.ElementBits(typ, expected), engine.ElementBits(typ, replacement))
		return cx.ElementFromBits(typ, old)
	})
	method(cx, atomics, "isLockFree", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		switch cx.ToIntegerOrInfinity(arg(args, 0)) {
		case 1, 2, 4, 8:
			return engine.True
		}
		return engine.False
	})
	method(cx, atomics, "load", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(cx, arg(args, 0), false, false, "load")
		i := validateAtomicAccess(cx, n, arg(args, 1))
		revalidateAtomicAccess(cx, t, i)
		return cx.ElementFromBits(t.ElementType(), t.AtomicLoad(i))
	})
	method(cx, atomics, "store", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, "store")
		i := validateAtomicAccess(cx, n, arg(args, 1))
		typ := t.ElementType()
		v := toAtomicValue(cx, typ, arg(args, 2))
		revalidateAtomicAccess(cx, t, i)
		t.AtomicStore(i, engine.ElementBits(typ, v))
		return v
	})
	method(cx, atomics, "wait", 4, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return doWait(cx, false, args)
	})
	method(cx, atomics, "waitAsync", 4, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return doWait(cx, true, args)
	})
	method(cx, atomics, "notify", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(cx, arg(args, 0), true, false, "notify")
		i := validateAtomicAccess(cx, n, arg(args, 1))
		c := math.Inf(1)
		if v := arg(args, 2); !v.IsUndefined() {
			c = max(cx.ToIntegerOrInfinity(v), 0)
		}
		b := t.ArrayBuffer()
		if !b.IsShared() {
			return engine.Number(0)
		}
		byteIndex := t.ByteOffset() + i*t.ElementType().Size()
		return engine.Number(float64(cx.NotifyWaiters(b, byteIndex, c)))
	})
	method(cx, atomics, "pause", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if v := arg(args, 0); !v.IsUndefined() {
			if !v.IsNumber() || v.Number() != math.Trunc(v.Number()) || math.IsInf(v.Number(), 0) {
				cx.ThrowTypeError("Atomics.pause: the argument must be an integral number")
			}
		}
		return engine.Undefined
	})
}
