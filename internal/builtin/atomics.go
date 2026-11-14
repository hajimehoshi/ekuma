// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// The VM is a single agent that cannot suspend and shares no memory with another agent,
// so the Atomics functions are ordinary reads and writes.

// validateIntegerTypedArray implements ValidateIntegerTypedArray and returns the typed array and its length.
func validateIntegerTypedArray(vm *engine.VM, v engine.Value, waitable bool, what string) (*engine.TypedArray, int) {
	_, t, n := validateTypedArray(vm, v, "Atomics."+what)
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

func initAtomics(vm *engine.VM) {
	atomics := vm.NewObject(vm.ObjectProto)
	vm.Define(vm.Global, vm.Intern("Atomics"), engine.ObjectValue(atomics), engine.FlagsHidden)
	vm.Define(atomics, vm.K.SymToStringTag, vm.NewString("Atomics"), engine.FlagConfigurable)

	// readModifyWrite implements AtomicReadModifyWrite with op on the encodings of the elements.
	readModifyWrite := func(name string, op func(old, v uint64) uint64) {
		method(vm, atomics, name, 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t, n := validateIntegerTypedArray(vm, arg(args, 0), false, name)
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
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, "compareExchange")
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
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, "load")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		b := revalidateAtomicAccess(vm, t, i)
		return vm.ElementFromBits(t.ElementType(), engine.LoadElementBits(b, t.ElementType()))
	})
	method(vm, atomics, "store", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), false, "store")
		i := validateAtomicAccess(vm, n, arg(args, 1))
		typ := t.ElementType()
		v := toAtomicValue(vm, typ, arg(args, 2))
		b := revalidateAtomicAccess(vm, t, i)
		engine.StoreElementBits(b, typ, engine.ElementBits(typ, v))
		return v
	})
	method(vm, atomics, "wait", 4, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t, n := validateIntegerTypedArray(vm, arg(args, 0), true, "wait")
		if !t.ArrayBuffer().IsShared() {
			vm.ThrowTypeError("Atomics.wait: the typed array must be backed by a SharedArrayBuffer")
		}
		validateAtomicAccess(vm, n, arg(args, 1))
		if t.ElementType().IsBigInt() {
			vm.ToBigInt(arg(args, 2))
		} else {
			vm.ToInt32(arg(args, 2))
		}
		vm.ToNumber(arg(args, 3))
		vm.ThrowTypeError("Atomics.wait cannot be called in this context")
		return engine.Undefined
	})
	method(vm, atomics, "notify", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		_, n := validateIntegerTypedArray(vm, arg(args, 0), true, "notify")
		validateAtomicAccess(vm, n, arg(args, 1))
		if c := arg(args, 2); !c.IsUndefined() {
			vm.ToIntegerOrInfinity(c)
		}
		// No agent can be waiting.
		return engine.Number(0)
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
