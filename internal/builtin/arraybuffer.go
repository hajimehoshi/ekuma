// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// binaryIntrinsics are the intrinsic objects of the binary data built-ins of one VM.
type binaryIntrinsics struct {
	arrayBufferCtor, arrayBufferProto             *engine.Object
	sharedArrayBufferCtor, sharedArrayBufferProto *engine.Object
	typedArrayCtor, typedArrayProto               *engine.Object
	dataViewProto                                 *engine.Object

	// ctors and protos are the typed array constructors and their prototypes, by element type.
	ctors  [engine.ElementTypeCount]*engine.Object
	protos [engine.ElementTypeCount]*engine.Object
}

// initBinary defines ArrayBuffer, SharedArrayBuffer, the typed arrays, DataView and Atomics.
func initBinary(vm *engine.VM) {
	bi := &binaryIntrinsics{}
	initArrayBuffer(vm, bi)
	initSharedArrayBuffer(vm, bi)
	initTypedArrays(vm, bi)
	initDataView(vm, bi)
	initAtomics(vm)
}

// maxByteLengthOption implements GetArrayBufferMaxByteLengthOption. It returns -1 for empty.
func maxByteLengthOption(vm *engine.VM, options engine.Value) int64 {
	if !options.IsObject() {
		return -1
	}
	m := vm.GetV(options, vm.Intern("maxByteLength"))
	if m.IsUndefined() {
		return -1
	}
	return toIndex(vm, m)
}

// thisArrayBuffer returns the data of this, which must be an ArrayBuffer that is shared when shared is set.
func thisArrayBuffer(vm *engine.VM, this engine.Value, shared bool, what string) *engine.ArrayBuffer {
	b := engine.ArrayBufferData(this)
	if b == nil || b.IsShared() != shared {
		name := "ArrayBuffer"
		if shared {
			name = "SharedArrayBuffer"
		}
		vm.ThrowTypeError(name + ".prototype." + what + " called on incompatible receiver " + vm.DisplayString(this))
	}
	return b
}

// allocateArrayBuffer implements AllocateArrayBuffer and AllocateSharedArrayBuffer for the constructor newTarget.
func allocateArrayBuffer(vm *engine.VM, newTarget, defaultProto *engine.Object, byteLength, maxByteLength int64, shared bool) *engine.Object {
	if maxByteLength >= 0 && byteLength > maxByteLength {
		vm.ThrowRangeError("byte length exceeds maxByteLength")
	}
	proto := vm.GetPrototypeFromConstructor(newTarget, defaultProto)
	return vm.NewArrayBufferObject(proto, byteLength, maxByteLength, shared)
}

// relativeRange resolves the start and end arguments of a slice-like method against length.
// An undefined end stands for length.
func relativeRange(vm *engine.VM, start, end engine.Value, length int) (int, int) {
	first := engine.RelativeIndex(vm.ToIntegerOrInfinity(start), length)
	final := length
	if !end.IsUndefined() {
		final = engine.RelativeIndex(vm.ToIntegerOrInfinity(end), length)
	}
	return first, final
}

func initArrayBuffer(vm *engine.VM, bi *binaryIntrinsics) {
	proto := vm.NewOrdinaryObject()
	bi.arrayBufferProto = proto
	vm.SetIntrinsic(engine.IntrinsicArrayBufferPrototype, proto)
	ctor := constructor(vm, "ArrayBuffer", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor ArrayBuffer requires 'new'")
		}
		n := toIndex(vm, arg(args, 0))
		m := maxByteLengthOption(vm, arg(args, 1))
		return engine.ObjectValue(allocateArrayBuffer(vm, nt, proto, n, m, false))
	})
	bi.arrayBufferCtor = ctor
	speciesGetter(vm, ctor)
	method(vm, ctor, "isView", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		a := arg(args, 0)
		return engine.Bool(engine.TypedArrayData(a) != nil || engine.DataViewData(a) != nil)
	})

	getter(vm, proto, vm.Intern("byteLength"), "byteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(vm, this, false, "byteLength").ByteLength()))
	})
	getter(vm, proto, vm.Intern("maxByteLength"), "maxByteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(vm, this, false, "maxByteLength")
		if b.IsDetached() {
			return engine.Number(0)
		}
		return engine.Number(float64(b.MaxByteLength()))
	})
	getter(vm, proto, vm.Intern("resizable"), "resizable", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(vm, this, false, "resizable").IsResizable())
	})
	getter(vm, proto, vm.Intern("detached"), "detached", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(vm, this, false, "detached").IsDetached())
	})
	getter(vm, proto, vm.Intern("immutable"), "immutable", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(vm, this, false, "immutable").IsImmutable())
	})
	method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(vm, this, false, "slice")
		if b.IsDetached() {
			vm.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
		}
		first, final := relativeRange(vm, arg(args, 0), arg(args, 1), b.ByteLength())
		newLen := max(final-first, 0)
		c := speciesConstructor(vm, this.Object(), ctor)
		r := vm.Construct(c, []engine.Value{engine.Number(float64(newLen))}, c)
		nb := engine.ArrayBufferData(engine.ObjectValue(r))
		if nb == nil || nb.IsShared() {
			vm.ThrowTypeError("species constructor did not return an ArrayBuffer")
		}
		if nb.IsDetached() {
			vm.ThrowTypeError("species constructor returned a detached ArrayBuffer")
		}
		if nb.IsImmutable() {
			vm.ThrowTypeError("species constructor returned an immutable ArrayBuffer")
		}
		if r == this.Object() {
			vm.ThrowTypeError("species constructor returned the same ArrayBuffer")
		}
		if nb.ByteLength() < newLen {
			vm.ThrowTypeError("species constructor returned a too small ArrayBuffer")
		}
		// The constructor may have detached or shrunk the buffer.
		if b.IsDetached() {
			vm.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
		}
		if cur := b.ByteLength(); first < cur {
			copy(nb.Bytes()[:min(newLen, cur-first)], b.Bytes()[first:])
		}
		return engine.ObjectValue(r)
	})
	method(vm, proto, "sliceToImmutable", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(vm, this, false, "sliceToImmutable")
		if b.IsDetached() {
			vm.ThrowTypeError("cannot perform ArrayBuffer.prototype.sliceToImmutable on a detached ArrayBuffer")
		}
		first, final := relativeRange(vm, arg(args, 0), arg(args, 1), b.ByteLength())
		newLen := max(final-first, 0)
		// The conversions may have detached or shrunk the buffer.
		if b.IsDetached() {
			vm.ThrowTypeError("cannot perform ArrayBuffer.prototype.sliceToImmutable on a detached ArrayBuffer")
		}
		if b.ByteLength() < final {
			vm.ThrowRangeError("the ArrayBuffer is too short for the slice")
		}
		var data []byte
		if newLen > 0 {
			if b.IsImmutable() {
				// The bytes of an immutable buffer never change, so they can be shared.
				data = b.Bytes()[first:final:final]
			} else {
				data = slices.Clone(b.Bytes()[first:final])
			}
		}
		return engine.ObjectValue(vm.NewImmutableArrayBufferFromBytes(proto, data))
	})
	method(vm, proto, "resize", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := engine.ArrayBufferData(this)
		if b == nil || b.IsShared() || !b.IsResizable() {
			vm.ThrowTypeError("ArrayBuffer.prototype.resize called on incompatible receiver " + vm.DisplayString(this))
		}
		n := toIndex(vm, arg(args, 0))
		if b.IsDetached() {
			vm.ThrowTypeError("cannot resize a detached ArrayBuffer")
		}
		if n > int64(b.MaxByteLength()) {
			vm.ThrowRangeError("new length exceeds maxByteLength")
		}
		b.Resize(int(n))
		return engine.Undefined
	})
	// transfer implements ArrayBufferCopyAndDetach as the method name.
	// preserve is ~preserve-resizability~, and immutable is ~immutable~.
	transfer := func(name string, preserve, immutable bool) {
		method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			b := thisArrayBuffer(vm, this, false, name)
			var n int64
			if l := arg(args, 0); l.IsUndefined() {
				n = int64(b.ByteLength())
			} else {
				n = toIndex(vm, l)
			}
			if b.IsDetached() {
				vm.ThrowTypeError("cannot transfer a detached ArrayBuffer")
			}
			if b.IsImmutable() {
				vm.ThrowTypeError("cannot transfer an immutable ArrayBuffer")
			}
			if b.HasDetachKey() {
				vm.ThrowTypeError("cannot transfer an ArrayBuffer that only the host can detach")
			}
			m := int64(-1)
			if preserve && b.IsResizable() {
				m = int64(b.MaxByteLength())
				if n > m {
					vm.ThrowRangeError("new length exceeds maxByteLength")
				}
			}
			return engine.ObjectValue(vm.TransferArrayBuffer(this.Object(), proto, n, m, immutable))
		})
	}
	transfer("transfer", true, false)
	transfer("transferToFixedLength", false, false)
	transfer("transferToImmutable", false, true)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("ArrayBuffer"), engine.FlagConfigurable)
}

func initSharedArrayBuffer(vm *engine.VM, bi *binaryIntrinsics) {
	proto := vm.NewOrdinaryObject()
	bi.sharedArrayBufferProto = proto
	vm.SetIntrinsic(engine.IntrinsicSharedArrayBufferPrototype, proto)
	ctor := constructor(vm, "SharedArrayBuffer", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor SharedArrayBuffer requires 'new'")
		}
		n := toIndex(vm, arg(args, 0))
		m := maxByteLengthOption(vm, arg(args, 1))
		return engine.ObjectValue(allocateArrayBuffer(vm, nt, proto, n, m, true))
	})
	bi.sharedArrayBufferCtor = ctor
	speciesGetter(vm, ctor)

	getter(vm, proto, vm.Intern("byteLength"), "byteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(vm, this, true, "byteLength").ByteLength()))
	})
	getter(vm, proto, vm.Intern("maxByteLength"), "maxByteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(vm, this, true, "maxByteLength").MaxByteLength()))
	})
	getter(vm, proto, vm.Intern("growable"), "growable", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(vm, this, true, "growable").IsResizable())
	})
	method(vm, proto, "grow", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := engine.ArrayBufferData(this)
		if b == nil || !b.IsShared() || !b.IsResizable() {
			vm.ThrowTypeError("SharedArrayBuffer.prototype.grow called on incompatible receiver " + vm.DisplayString(this))
		}
		n := toIndex(vm, arg(args, 0))
		if n > int64(b.MaxByteLength()) {
			vm.ThrowRangeError("new length exceeds maxByteLength")
		}
		if !b.Grow(int(n)) {
			vm.ThrowRangeError("SharedArrayBuffer cannot shrink")
		}
		return engine.Undefined
	})
	method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(vm, this, true, "slice")
		first, final := relativeRange(vm, arg(args, 0), arg(args, 1), b.ByteLength())
		newLen := max(final-first, 0)
		c := speciesConstructor(vm, this.Object(), ctor)
		r := vm.Construct(c, []engine.Value{engine.Number(float64(newLen))}, c)
		nb := engine.ArrayBufferData(engine.ObjectValue(r))
		if nb == nil || !nb.IsShared() {
			vm.ThrowTypeError("species constructor did not return a SharedArrayBuffer")
		}
		if r == this.Object() {
			vm.ThrowTypeError("species constructor returned the same SharedArrayBuffer")
		}
		if nb.ByteLength() < newLen {
			vm.ThrowTypeError("species constructor returned a too small SharedArrayBuffer")
		}
		if cur := b.ByteLength(); first < cur {
			engine.CopyBufferBytes(nb, 0, b, first, min(newLen, cur-first))
		}
		return engine.ObjectValue(r)
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("SharedArrayBuffer"), engine.FlagConfigurable)
}
