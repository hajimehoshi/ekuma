// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// The VM is a single agent that cannot suspend and shares no memory with another agent,
// so the Atomics functions are ordinary reads and writes.

// validateIntegerTypedArray implements ValidateIntegerTypedArray, with the access mode write when write is set,
// and returns the typed array and its length.
func validateIntegerTypedArray(vm *engine.VM, v engine.Value, waitable, write bool, what string) (*engine.TypedArray, int) {
	_, t, n := validateTypedArrayAccess(vm, v, write, "Atomics."+what)
	typ := t.ElementType()
	if waitable {
		if typ != engine.ElementInt32 && typ != engine.ElementBigInt64 {
			vm.ThrowTypeError("Atomics." + what + ": the typed array must be an Int32Array or a BigInt64Array")
		}
	} else if !typ.IsInteger() {
		vm.ThrowTypeError("Atomics." + what + ": the typed array must be an integer typed array")
	}
	return t, n
}

// validateAtomicAccess implements ValidateAtomicAccess and returns the element index.
func validateAtomicAccess(vm *engine.VM, n int, index engine.Value) int {
	i := toIndex(vm, index)
	if i >= int64(n) {
		vm.ThrowRangeError("invalid atomic access index")
	}
	return int(i)
}

// revalidateAtomicAccess implements RevalidateAtomicAccess and returns the bytes of the element i.
func revalidateAtomicAccess(vm *engine.VM, t *engine.TypedArray, i int) []byte {
	b := t.Bytes()
	if b == nil {
		vm.ThrowTypeError("the typed array is detached or out of bounds")
	}
	size := t.ElementType().Size()
	if (i+1)*size > len(b) {
		vm.ThrowRangeError("invalid atomic access index")
	}
	return b[i*size:]
}

// toAtomicValue converts v as the Atomics functions do for an element of the type typ.
func toAtomicValue(vm *engine.VM, typ engine.ElementType, v engine.Value) engine.Value {
	if typ.IsBigInt() {
		return vm.ToBigInt(v)
	}
	return engine.Number(vm.ToIntegerOrInfinity(v))
}

// doWait implements DoWait. The VM is an agent that cannot suspend, so a synchronous wait throws.
func doWait(vm *engine.VM, async bool, args []engine.Value) engine.Value {
	what := "wait"
	if async {
		what = "waitAsync"
	}
	t, n := validateIntegerTypedArray(vm, arg(args, 0), true, false, what)
	if !t.ArrayBuffer().IsShared() {
		vm.ThrowTypeError("Atomics." + what + ": the typed array must be backed by a SharedArrayBuffer")
	}
	i := validateAtomicAccess(vm, n, arg(args, 1))
	typ := t.ElementType()
	var v engine.Value
	if typ.IsBigInt() {
		v = vm.ToBigInt(arg(args, 2))
	} else {
		v = engine.Number(float64(vm.ToInt32(arg(args, 2))))
	}
	q := vm.ToNumber(arg(args, 3))
	timeout := math.Inf(1)
	if !math.IsNaN(q) {
		timeout = max(q, 0)
	}
	if !async {
		vm.ThrowTypeError("Atomics.wait cannot be called in this context")
	}
	b := revalidateAtomicAccess(vm, t, i)
	// The encodings are compared in the size of the element.
	mask := uint64(math.MaxUint64) >> (64 - 8*typ.Size())
	if engine.LoadElementBits(b, typ)&mask != engine.ElementBits(typ, v)&mask {
		return waitResult(vm, false, vm.NewString("not-equal"))
	}
	if timeout == 0 {
		return waitResult(vm, false, vm.NewString("timed-out"))
	}
	byteIndex := t.ByteOffset() + i*typ.Size()
	return waitResult(vm, true, vm.AddWaiter(t.ArrayBuffer(), byteIndex, timeout))
}

// waitResult returns the result object of Atomics.waitAsync.
func waitResult(vm *engine.VM, async bool, value engine.Value) engine.Value {
	o := vm.NewOrdinaryObject()
	vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern("async")), engine.Bool(async))
	vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern("value")), value)
	return engine.ObjectValue(o)
}

func initAtomics(vm *engine.VM) {
	atomics := vm.NewOrdinaryObject()
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Atomics"), engine.ObjectValue(atomics), engine.FlagsHidden)
	vm.Define(atomics, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Atomics"), engine.FlagConfigurable)

	// readModifyWrite implements AtomicReadModifyWrite with op on the encodings of the elements.
	readModifyWrite := func(name string, op func(old, v uint64) uint64) {
		method(vm, atomics, name, 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t, n := validateIntegerTypedArray(vm, arg(args, 0), false, true, name)
			i := validateAtomicAccess(vm, n, arg(args, 1))
			typ := t.ElementType()
			v := toAtomicValue(vm, typ, arg(args, 2))
			b := revalidateAtomicAccess(vm, t, i)
			old := engine.LoadElementBits(b, typ)
			engine.StoreElementBits(b, typ, op(old, engine.ElementBits(typ, v)))
			return vm.ElementFromBits(typ, old)
		})
	}
	readModifyWrite("add", func(old, v uint64) uint64 { return old + v })
	readModifyWrite("and", func(old, v uint64) uint64 { return old & v })
	readModifyWrite("exchange", func(old, v uint64) uint64 { return v })
	readModifyWrite("or", func(old, v uint64) uint64 { return old | v })
	readModifyWrite("sub", func(old, v uint64) uint64 { return old - v })
	readModifyWrite("xor", func(old, v uint64) uint64 { return old ^ v })

	method(vm, atomics, "compareExchange", 4, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, true, "compareExchange")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		typ := t.ElementType()
		expected := toAtomicValue(vm, typ, arg(args, 2))
		replacement := toAtomicValue(vm, typ, arg(args, 3))
		b := revalidateAtomicAccess(vm, t, i)
		old := engine.LoadElementBits(b, typ)
		// The encodings are compared in the size of the element.
		mask := uint64(math.MaxUint64) >> (64 - 8*typ.Size())
		if old&mask == engine.ElementBits(typ, expected)&mask {
			engine.StoreElementBits(b, typ, engine.ElementBits(typ, replacement))
		}
		return vm.ElementFromBits(typ, old)
	})
	method(vm, atomics, "isLockFree", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		switch vm.ToIntegerOrInfinity(arg(args, 0)) {
		case 1, 2, 4, 8:
			return engine.True
		}
		return engine.False
	})
	method(vm, atomics, "load", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, false, "load")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		b := revalidateAtomicAccess(vm, t, i)
		return vm.ElementFromBits(t.ElementType(), engine.LoadElementBits(b, t.ElementType()))
	})
	method(vm, atomics, "store", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, true, "store")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		typ := t.ElementType()
		v := toAtomicValue(vm, typ, arg(args, 2))
		b := revalidateAtomicAccess(vm, t, i)
		engine.StoreElementBits(b, typ, engine.ElementBits(typ, v))
		return v
	})
	method(vm, atomics, "wait", 4, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return doWait(vm, false, args)
	})
	method(vm, atomics, "waitAsync", 4, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return doWait(vm, true, args)
	})
	method(vm, atomics, "notify", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), true, false, "notify")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		c := math.Inf(1)
		if v := arg(args, 2); !v.IsUndefined() {
			c = max(vm.ToIntegerOrInfinity(v), 0)
		}
		b := t.ArrayBuffer()
		if !b.IsShared() {
			return engine.Number(0)
		}
		byteIndex := t.ByteOffset() + i*t.ElementType().Size()
		return engine.Number(float64(vm.NotifyWaiters(b, byteIndex, c)))
	})
	method(vm, atomics, "pause", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if v := arg(args, 0); !v.IsUndefined() {
			if !v.IsNumber() || v.Number() != math.Trunc(v.Number()) || math.IsInf(v.Number(), 0) {
				vm.ThrowTypeError("Atomics.pause: the argument must be an integral number")
			}
		}
		return engine.Undefined
	})
}
