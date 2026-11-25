// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
	return storage.Number(cx.ToIntegerOrInfinity(v))
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
		v = storage.Number(float64(cx.ToInt32(arg(args, 2))))
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
		[]engine.PropKey{storage.NamedKey(cx.Intern("async")), storage.NamedKey(cx.Intern("value"))},
		[]engine.Value{storage.Bool(async), value},
	)
	return storage.ObjectValue(o)
}

var (
	nativeAtomicsCompareExchange = engine.RegisterNative(atomicsCompareExchange)
	nativeAtomicsIsLockFree      = engine.RegisterNative(atomicsIsLockFree)
	nativeAtomicsLoad            = engine.RegisterNative(atomicsLoad)
	nativeAtomicsStore           = engine.RegisterNative(atomicsStore)
	nativeAtomicsWait            = engine.RegisterNative(atomicsWait)
	nativeAtomicsWaitAsync       = engine.RegisterNative(atomicsWaitAsync)
	nativeAtomicsNotify          = engine.RegisterNative(atomicsNotify)
	nativeAtomicsPause           = engine.RegisterNative(atomicsPause)
)

func initAtomics(cx *engine.ExecutionContext) {
	atomics := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Atomics"), storage.ObjectValue(atomics), engine.FlagsHidden)
	cx.Define(atomics, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Atomics"), engine.FlagConfigurable)

	method(cx, atomics, "add", 3, nativeAtomicsAdd)
	method(cx, atomics, "and", 3, nativeAtomicsAnd)
	method(cx, atomics, "exchange", 3, nativeAtomicsExchange)
	method(cx, atomics, "or", 3, nativeAtomicsOr)
	method(cx, atomics, "sub", 3, nativeAtomicsSub)
	method(cx, atomics, "xor", 3, nativeAtomicsXor)

	method(cx, atomics, "compareExchange", 4, nativeAtomicsCompareExchange)
	method(cx, atomics, "isLockFree", 1, nativeAtomicsIsLockFree)
	method(cx, atomics, "load", 2, nativeAtomicsLoad)
	method(cx, atomics, "store", 3, nativeAtomicsStore)
	method(cx, atomics, "wait", 4, nativeAtomicsWait)
	method(cx, atomics, "waitAsync", 4, nativeAtomicsWaitAsync)
	method(cx, atomics, "notify", 3, nativeAtomicsNotify)
	method(cx, atomics, "pause", 0, nativeAtomicsPause)
}

var (
	nativeAtomicsAdd      = engine.RegisterNative(atomicsReadModifyWriteNative("add", engine.AtomicAdd))
	nativeAtomicsAnd      = engine.RegisterNative(atomicsReadModifyWriteNative("and", engine.AtomicAnd))
	nativeAtomicsExchange = engine.RegisterNative(atomicsReadModifyWriteNative("exchange", engine.AtomicExchange))
	nativeAtomicsOr       = engine.RegisterNative(atomicsReadModifyWriteNative("or", engine.AtomicOr))
	nativeAtomicsSub      = engine.RegisterNative(atomicsReadModifyWriteNative("sub", engine.AtomicSub))
	nativeAtomicsXor      = engine.RegisterNative(atomicsReadModifyWriteNative("xor", engine.AtomicXor))
)

// atomicsReadModifyWriteNative returns the function name of Atomics, which implements
// AtomicReadModifyWrite with op on the encodings of the elements.
//
//go:noinline
func atomicsReadModifyWriteNative(name string, op engine.AtomicOp) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, name)
		i := validateAtomicAccess(cx, n, arg(args, 1))
		typ := t.ElementType()
		v := toAtomicValue(cx, typ, arg(args, 2))
		revalidateAtomicAccess(cx, t, i)
		return engine.ElementFromBits(typ, t.AtomicReadModifyWrite(i, op, engine.ElementBits(typ, v), 0))
	}
}

func atomicsCompareExchange(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, "compareExchange")
	i := validateAtomicAccess(cx, n, arg(args, 1))
	typ := t.ElementType()
	expected := toAtomicValue(cx, typ, arg(args, 2))
	replacement := toAtomicValue(cx, typ, arg(args, 3))
	revalidateAtomicAccess(cx, t, i)
	old := t.AtomicReadModifyWrite(i, engine.AtomicCompareExchange, engine.ElementBits(typ, expected), engine.ElementBits(typ, replacement))
	return engine.ElementFromBits(typ, old)
}

func atomicsIsLockFree(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	switch cx.ToIntegerOrInfinity(arg(args, 0)) {
	case 1, 2, 4, 8:
		return storage.True
	}
	return storage.False
}

func atomicsLoad(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t, n := validateIntegerTypedArray(cx, arg(args, 0), false, false, "load")
	i := validateAtomicAccess(cx, n, arg(args, 1))
	revalidateAtomicAccess(cx, t, i)
	return engine.ElementFromBits(t.ElementType(), t.AtomicLoad(i))
}

func atomicsStore(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t, n := validateIntegerTypedArray(cx, arg(args, 0), false, true, "store")
	i := validateAtomicAccess(cx, n, arg(args, 1))
	typ := t.ElementType()
	v := toAtomicValue(cx, typ, arg(args, 2))
	revalidateAtomicAccess(cx, t, i)
	t.AtomicStore(i, engine.ElementBits(typ, v))
	return v
}

func atomicsWait(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return doWait(cx, false, args)
}

func atomicsWaitAsync(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return doWait(cx, true, args)
}

func atomicsNotify(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t, n := validateIntegerTypedArray(cx, arg(args, 0), true, false, "notify")
	i := validateAtomicAccess(cx, n, arg(args, 1))
	c := math.Inf(1)
	if v := arg(args, 2); !v.IsUndefined() {
		c = max(cx.ToIntegerOrInfinity(v), 0)
	}
	b := t.ArrayBuffer()
	if !b.IsShared() {
		return storage.Number(0)
	}
	byteIndex := t.ByteOffset() + i*t.ElementType().Size()
	return storage.Number(float64(cx.NotifyWaiters(b, byteIndex, c)))
}

func atomicsPause(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if v := arg(args, 0); !v.IsUndefined() {
		if !v.IsNumber() || v.Number() != math.Trunc(v.Number()) || math.IsInf(v.Number(), 0) {
			cx.ThrowTypeError("Atomics.pause: the argument must be an integral number")
		}
	}
	return storage.Undefined
}
