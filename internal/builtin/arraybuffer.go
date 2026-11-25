// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// initBinary defines ArrayBuffer, SharedArrayBuffer, the typed arrays, DataView and Atomics.
func initBinary(cx *engine.ExecutionContext) {
	initArrayBuffer(cx)
	initSharedArrayBuffer(cx)
	initTypedArrays(cx)
	initDataView(cx)
	initAtomics(cx)
}

// maxByteLengthOption implements GetArrayBufferMaxByteLengthOption. It returns -1 for empty.
func maxByteLengthOption(cx *engine.ExecutionContext, options engine.Value) int64 {
	if !options.IsObject() {
		return -1
	}
	m := cx.GetV(options, cx.Intern("maxByteLength"))
	if m.IsUndefined() {
		return -1
	}
	return toIndex(cx, m)
}

// thisArrayBuffer returns the data of this, which must be an ArrayBuffer that is shared when shared is set.
func thisArrayBuffer(cx *engine.ExecutionContext, this engine.Value, shared bool, what string) *engine.ArrayBuffer {
	b := engine.ArrayBufferData(this)
	if b == nil || b.IsShared() != shared {
		name := "ArrayBuffer"
		if shared {
			name = "SharedArrayBuffer"
		}
		cx.ThrowTypeError(name + ".prototype." + what + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return b
}

// allocateArrayBuffer implements AllocateArrayBuffer and AllocateSharedArrayBuffer for the constructor newTarget.
func allocateArrayBuffer(cx *engine.ExecutionContext, newTarget *engine.Object, byteLength, maxByteLength int64, shared bool) *engine.Object {
	if maxByteLength >= 0 && byteLength > maxByteLength {
		cx.ThrowRangeError("byte length exceeds maxByteLength")
	}
	defaultProto := engine.IntrinsicArrayBufferPrototype
	if shared {
		defaultProto = engine.IntrinsicSharedArrayBufferPrototype
	}
	proto := cx.GetPrototypeFromConstructor(newTarget, defaultProto)
	return cx.NewArrayBufferObject(proto, byteLength, maxByteLength, shared)
}

// relativeRange resolves the start and end arguments of a slice-like method against length.
// An undefined end stands for length.
func relativeRange(cx *engine.ExecutionContext, start, end engine.Value, length int) (int, int) {
	first := numconv.RelativeIndex(cx.ToIntegerOrInfinity(start), length)
	final := length
	if !end.IsUndefined() {
		final = numconv.RelativeIndex(cx.ToIntegerOrInfinity(end), length)
	}
	return first, final
}

func initArrayBuffer(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicArrayBufferPrototype, proto)
	ctor := constructor(cx, "ArrayBuffer", 1, engine.IntrinsicArrayBufferPrototype, arrayBufferConstructor)
	cx.SetIntrinsic(engine.IntrinsicArrayBuffer, ctor)
	speciesGetter(cx, ctor)
	method(cx, ctor, "isView", 1, arrayBufferIsView)

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", arrayBufferByteLength)
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", arrayBufferMaxByteLength)
	getter(cx, proto, cx.Intern("resizable"), "resizable", arrayBufferResizable)
	getter(cx, proto, cx.Intern("detached"), "detached", arrayBufferDetached)
	getter(cx, proto, cx.Intern("immutable"), "immutable", arrayBufferImmutable)
	method(cx, proto, "slice", 2, arrayBufferSlice)
	method(cx, proto, "sliceToImmutable", 2, arrayBufferSliceToImmutable)
	method(cx, proto, "resize", 1, arrayBufferResize)
	method(cx, proto, "transfer", 0, arrayBufferTransfer)
	method(cx, proto, "transferToFixedLength", 0, arrayBufferTransferToFixedLength)
	method(cx, proto, "transferToImmutable", 0, arrayBufferTransferToImmutable)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("ArrayBuffer"), engine.FlagConfigurable)
}

func arrayBufferConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor ArrayBuffer requires 'new'")
	}
	n := toIndex(cx, arg(args, 0))
	m := maxByteLengthOption(cx, arg(args, 1))
	return engine.ObjectValue(allocateArrayBuffer(cx, nt, n, m, false))
}

func arrayBufferIsView(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	a := arg(args, 0)
	return engine.Bool(engine.TypedArrayData(a) != nil || engine.DataViewData(a) != nil)
}

func arrayBufferByteLength(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(float64(thisArrayBuffer(cx, this, false, "byteLength").ByteLength()))
}

func arrayBufferMaxByteLength(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := thisArrayBuffer(cx, this, false, "maxByteLength")
	if b.IsDetached() {
		return engine.Number(0)
	}
	return engine.Number(float64(b.MaxByteLength()))
}

func arrayBufferResizable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(thisArrayBuffer(cx, this, false, "resizable").IsResizable())
}

func arrayBufferDetached(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(thisArrayBuffer(cx, this, false, "detached").IsDetached())
}

func arrayBufferImmutable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(thisArrayBuffer(cx, this, false, "immutable").IsImmutable())
}

func arrayBufferSlice(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := thisArrayBuffer(cx, this, false, "slice")
	if b.IsDetached() {
		cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
	}
	first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
	newLen := max(final-first, 0)
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicArrayBuffer))
	r := cx.Construct(c, []engine.Value{engine.Number(float64(newLen))}, c)
	nb := engine.ArrayBufferData(engine.ObjectValue(r))
	if nb == nil || nb.IsShared() {
		cx.ThrowTypeError("species constructor did not return an ArrayBuffer")
	}
	if nb.IsDetached() {
		cx.ThrowTypeError("species constructor returned a detached ArrayBuffer")
	}
	if nb.IsImmutable() {
		cx.ThrowTypeError("species constructor returned an immutable ArrayBuffer")
	}
	if r == this.Object() {
		cx.ThrowTypeError("species constructor returned the same ArrayBuffer")
	}
	if nb.ByteLength() < newLen {
		cx.ThrowTypeError("species constructor returned a too small ArrayBuffer")
	}
	// The constructor may have detached or shrunk the buffer.
	if b.IsDetached() {
		cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
	}
	if cur := b.ByteLength(); first < cur {
		copy(nb.Bytes()[:min(newLen, cur-first)], b.Bytes()[first:])
	}
	return engine.ObjectValue(r)
}

func arrayBufferSliceToImmutable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := thisArrayBuffer(cx, this, false, "sliceToImmutable")
	if b.IsDetached() {
		cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.sliceToImmutable on a detached ArrayBuffer")
	}
	first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
	newLen := max(final-first, 0)
	// The conversions may have detached or shrunk the buffer.
	if b.IsDetached() {
		cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.sliceToImmutable on a detached ArrayBuffer")
	}
	if b.ByteLength() < final {
		cx.ThrowRangeError("the ArrayBuffer is too short for the slice")
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
	return engine.ObjectValue(cx.NewImmutableArrayBufferFromBytes(cx.Intrinsic(engine.IntrinsicArrayBufferPrototype), data))
}

func arrayBufferResize(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := engine.ArrayBufferData(this)
	if b == nil || b.IsShared() || !b.IsResizable() {
		cx.ThrowTypeError("ArrayBuffer.prototype.resize called on incompatible receiver " + cx.DisplayString(this))
	}
	n := toIndex(cx, arg(args, 0))
	if b.IsDetached() {
		cx.ThrowTypeError("cannot resize a detached ArrayBuffer")
	}
	if n > int64(b.MaxByteLength()) {
		cx.ThrowRangeError("new length exceeds maxByteLength")
	}
	b.Resize(int(n))
	return engine.Undefined
}

var (
	arrayBufferTransfer              = arrayBufferTransferNative("transfer", true, false)
	arrayBufferTransferToFixedLength = arrayBufferTransferNative("transferToFixedLength", false, false)
	arrayBufferTransferToImmutable   = arrayBufferTransferNative("transferToImmutable", false, true)
)

// arrayBufferTransferNative returns the method name of ArrayBuffer.prototype, which implements
// ArrayBufferCopyAndDetach.
// preserve is ~preserve-resizability~, and immutable is ~immutable~.
//
//go:noinline
func arrayBufferTransferNative(name string, preserve, immutable bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(cx, this, false, name)
		var n int64
		if l := arg(args, 0); l.IsUndefined() {
			n = int64(b.ByteLength())
		} else {
			n = toIndex(cx, l)
		}
		if b.IsDetached() {
			cx.ThrowTypeError("cannot transfer a detached ArrayBuffer")
		}
		if b.IsImmutable() {
			cx.ThrowTypeError("cannot transfer an immutable ArrayBuffer")
		}
		if b.HasDetachKey() {
			cx.ThrowTypeError("cannot transfer an ArrayBuffer that only the host can detach")
		}
		m := int64(-1)
		if preserve && b.IsResizable() {
			m = int64(b.MaxByteLength())
			if n > m {
				cx.ThrowRangeError("new length exceeds maxByteLength")
			}
		}
		return engine.ObjectValue(cx.TransferArrayBuffer(this.Object(), cx.Intrinsic(engine.IntrinsicArrayBufferPrototype), n, m, immutable))
	}
}

func initSharedArrayBuffer(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicSharedArrayBufferPrototype, proto)
	ctor := constructor(cx, "SharedArrayBuffer", 1, engine.IntrinsicSharedArrayBufferPrototype, sharedArrayBufferConstructor)
	cx.SetIntrinsic(engine.IntrinsicSharedArrayBuffer, ctor)
	speciesGetter(cx, ctor)

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", sharedArrayBufferByteLength)
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", sharedArrayBufferMaxByteLength)
	getter(cx, proto, cx.Intern("growable"), "growable", sharedArrayBufferGrowable)
	method(cx, proto, "grow", 1, sharedArrayBufferGrow)
	method(cx, proto, "slice", 2, sharedArrayBufferSlice)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("SharedArrayBuffer"), engine.FlagConfigurable)
}

func sharedArrayBufferConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor SharedArrayBuffer requires 'new'")
	}
	n := toIndex(cx, arg(args, 0))
	m := maxByteLengthOption(cx, arg(args, 1))
	return engine.ObjectValue(allocateArrayBuffer(cx, nt, n, m, true))
}

func sharedArrayBufferByteLength(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(float64(thisArrayBuffer(cx, this, true, "byteLength").ByteLength()))
}

func sharedArrayBufferMaxByteLength(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(float64(thisArrayBuffer(cx, this, true, "maxByteLength").MaxByteLength()))
}

func sharedArrayBufferGrowable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(thisArrayBuffer(cx, this, true, "growable").IsResizable())
}

func sharedArrayBufferGrow(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := engine.ArrayBufferData(this)
	if b == nil || !b.IsShared() || !b.IsResizable() {
		cx.ThrowTypeError("SharedArrayBuffer.prototype.grow called on incompatible receiver " + cx.DisplayString(this))
	}
	n := toIndex(cx, arg(args, 0))
	if n > int64(b.MaxByteLength()) {
		cx.ThrowRangeError("new length exceeds maxByteLength")
	}
	if !b.Grow(int(n)) {
		cx.ThrowRangeError("SharedArrayBuffer cannot shrink")
	}
	return engine.Undefined
}

func sharedArrayBufferSlice(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	b := thisArrayBuffer(cx, this, true, "slice")
	first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
	newLen := max(final-first, 0)
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(engine.IntrinsicSharedArrayBuffer))
	r := cx.Construct(c, []engine.Value{engine.Number(float64(newLen))}, c)
	nb := engine.ArrayBufferData(engine.ObjectValue(r))
	if nb == nil || !nb.IsShared() {
		cx.ThrowTypeError("species constructor did not return a SharedArrayBuffer")
	}
	if b.SameDataBlock(nb) {
		cx.ThrowTypeError("species constructor returned a SharedArrayBuffer with the same data block")
	}
	if nb.ByteLength() < newLen {
		cx.ThrowTypeError("species constructor returned a too small SharedArrayBuffer")
	}
	if cur := b.ByteLength(); first < cur {
		engine.CopyBufferBytes(nb, 0, b, first, min(newLen, cur-first))
	}
	return engine.ObjectValue(r)
}
