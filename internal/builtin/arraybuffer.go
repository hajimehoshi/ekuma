// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
func maxByteLengthOption(cx *engine.ExecutionContext, options storage.Value) int64 {
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
func thisArrayBuffer(cx *engine.ExecutionContext, this storage.Value, shared bool, what string) *storage.ArrayBuffer {
	b := storage.ArrayBufferData(this)
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
func allocateArrayBuffer(cx *engine.ExecutionContext, newTarget *storage.Object, byteLength, maxByteLength int64, shared bool) *storage.Object {
	if maxByteLength >= 0 && byteLength > maxByteLength {
		cx.ThrowRangeError("byte length exceeds maxByteLength")
	}
	defaultProto := storage.IntrinsicArrayBufferPrototype
	if shared {
		defaultProto = storage.IntrinsicSharedArrayBufferPrototype
	}
	proto := cx.GetPrototypeFromConstructor(newTarget, defaultProto)
	return cx.NewArrayBufferObject(proto, byteLength, maxByteLength, shared)
}

// relativeRange resolves the start and end arguments of a slice-like method against length.
// An undefined end stands for length.
func relativeRange(cx *engine.ExecutionContext, start, end storage.Value, length int) (int, int) {
	first := numconv.RelativeIndex(cx.ToIntegerOrInfinity(start), length)
	final := length
	if !end.IsUndefined() {
		final = numconv.RelativeIndex(cx.ToIntegerOrInfinity(end), length)
	}
	return first, final
}

var (
	nativeArrayBufferConstructor      = engine.RegisterNative(arrayBufferConstructor)
	nativeArrayBufferIsView           = engine.RegisterNative(arrayBufferIsView)
	nativeArrayBufferByteLength       = engine.RegisterNative(arrayBufferByteLength)
	nativeArrayBufferMaxByteLength    = engine.RegisterNative(arrayBufferMaxByteLength)
	nativeArrayBufferResizable        = engine.RegisterNative(arrayBufferResizable)
	nativeArrayBufferDetached         = engine.RegisterNative(arrayBufferDetached)
	nativeArrayBufferImmutable        = engine.RegisterNative(arrayBufferImmutable)
	nativeArrayBufferSlice            = engine.RegisterNative(arrayBufferSlice)
	nativeArrayBufferSliceToImmutable = engine.RegisterNative(arrayBufferSliceToImmutable)
	nativeArrayBufferResize           = engine.RegisterNative(arrayBufferResize)
)

func initArrayBuffer(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(storage.IntrinsicArrayBufferPrototype, proto)
	ctor := constructor(cx, "ArrayBuffer", 1, storage.IntrinsicArrayBufferPrototype, nativeArrayBufferConstructor)
	cx.SetIntrinsic(storage.IntrinsicArrayBuffer, ctor)
	speciesGetter(cx, ctor)
	method(cx, ctor, "isView", 1, nativeArrayBufferIsView)

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", nativeArrayBufferByteLength)
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", nativeArrayBufferMaxByteLength)
	getter(cx, proto, cx.Intern("resizable"), "resizable", nativeArrayBufferResizable)
	getter(cx, proto, cx.Intern("detached"), "detached", nativeArrayBufferDetached)
	getter(cx, proto, cx.Intern("immutable"), "immutable", nativeArrayBufferImmutable)
	method(cx, proto, "slice", 2, nativeArrayBufferSlice)
	method(cx, proto, "sliceToImmutable", 2, nativeArrayBufferSliceToImmutable)
	method(cx, proto, "resize", 1, nativeArrayBufferResize)
	method(cx, proto, "transfer", 0, nativeArrayBufferTransfer)
	method(cx, proto, "transferToFixedLength", 0, nativeArrayBufferTransferToFixedLength)
	method(cx, proto, "transferToImmutable", 0, nativeArrayBufferTransferToImmutable)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("ArrayBuffer"), storage.FlagConfigurable)
}

func arrayBufferConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor ArrayBuffer requires 'new'")
	}
	n := toIndex(cx, arg(args, 0))
	m := maxByteLengthOption(cx, arg(args, 1))
	return storage.ObjectValue(allocateArrayBuffer(cx, nt, n, m, false))
}

func arrayBufferIsView(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	a := arg(args, 0)
	return storage.Bool(storage.TypedArrayData(a) != nil || storage.DataViewData(a) != nil)
}

func arrayBufferByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(float64(thisArrayBuffer(cx, this, false, "byteLength").ByteLength()))
}

func arrayBufferMaxByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := thisArrayBuffer(cx, this, false, "maxByteLength")
	if b.IsDetached() {
		return storage.Number(0)
	}
	return storage.Number(float64(b.MaxByteLength()))
}

func arrayBufferResizable(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(thisArrayBuffer(cx, this, false, "resizable").IsResizable())
}

func arrayBufferDetached(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(thisArrayBuffer(cx, this, false, "detached").IsDetached())
}

func arrayBufferImmutable(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(thisArrayBuffer(cx, this, false, "immutable").IsImmutable())
}

func arrayBufferSlice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := thisArrayBuffer(cx, this, false, "slice")
	if b.IsDetached() {
		cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
	}
	first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
	newLen := max(final-first, 0)
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(storage.IntrinsicArrayBuffer))
	r := cx.Construct(c, []storage.Value{storage.Number(float64(newLen))}, c)
	nb := storage.ArrayBufferData(storage.ObjectValue(r))
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
	return storage.ObjectValue(r)
}

func arrayBufferSliceToImmutable(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
	return storage.ObjectValue(cx.NewImmutableArrayBufferFromBytes(cx.Intrinsic(storage.IntrinsicArrayBufferPrototype), data))
}

func arrayBufferResize(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := storage.ArrayBufferData(this)
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
	return storage.Undefined
}

var (
	nativeArrayBufferTransfer              = engine.RegisterNative(arrayBufferTransferNative("transfer", true, false))
	nativeArrayBufferTransferToFixedLength = engine.RegisterNative(arrayBufferTransferNative("transferToFixedLength", false, false))
	nativeArrayBufferTransferToImmutable   = engine.RegisterNative(arrayBufferTransferNative("transferToImmutable", false, true))
)

// arrayBufferTransferNative returns the method name of ArrayBuffer.prototype, which implements
// ArrayBufferCopyAndDetach.
// preserve is ~preserve-resizability~, and immutable is ~immutable~.
//
//go:noinline
func arrayBufferTransferNative(name string, preserve, immutable bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
		return storage.ObjectValue(cx.TransferArrayBuffer(this.Object(), cx.Intrinsic(storage.IntrinsicArrayBufferPrototype), n, m, immutable))
	}
}

var (
	nativeSharedArrayBufferConstructor   = engine.RegisterNative(sharedArrayBufferConstructor)
	nativeSharedArrayBufferByteLength    = engine.RegisterNative(sharedArrayBufferByteLength)
	nativeSharedArrayBufferMaxByteLength = engine.RegisterNative(sharedArrayBufferMaxByteLength)
	nativeSharedArrayBufferGrowable      = engine.RegisterNative(sharedArrayBufferGrowable)
	nativeSharedArrayBufferGrow          = engine.RegisterNative(sharedArrayBufferGrow)
	nativeSharedArrayBufferSlice         = engine.RegisterNative(sharedArrayBufferSlice)
)

func initSharedArrayBuffer(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(storage.IntrinsicSharedArrayBufferPrototype, proto)
	ctor := constructor(cx, "SharedArrayBuffer", 1, storage.IntrinsicSharedArrayBufferPrototype, nativeSharedArrayBufferConstructor)
	cx.SetIntrinsic(storage.IntrinsicSharedArrayBuffer, ctor)
	speciesGetter(cx, ctor)

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", nativeSharedArrayBufferByteLength)
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", nativeSharedArrayBufferMaxByteLength)
	getter(cx, proto, cx.Intern("growable"), "growable", nativeSharedArrayBufferGrowable)
	method(cx, proto, "grow", 1, nativeSharedArrayBufferGrow)
	method(cx, proto, "slice", 2, nativeSharedArrayBufferSlice)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("SharedArrayBuffer"), storage.FlagConfigurable)
}

func sharedArrayBufferConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor SharedArrayBuffer requires 'new'")
	}
	n := toIndex(cx, arg(args, 0))
	m := maxByteLengthOption(cx, arg(args, 1))
	return storage.ObjectValue(allocateArrayBuffer(cx, nt, n, m, true))
}

func sharedArrayBufferByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(float64(thisArrayBuffer(cx, this, true, "byteLength").ByteLength()))
}

func sharedArrayBufferMaxByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Number(float64(thisArrayBuffer(cx, this, true, "maxByteLength").MaxByteLength()))
}

func sharedArrayBufferGrowable(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(thisArrayBuffer(cx, this, true, "growable").IsResizable())
}

func sharedArrayBufferGrow(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := storage.ArrayBufferData(this)
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
	return storage.Undefined
}

func sharedArrayBufferSlice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	b := thisArrayBuffer(cx, this, true, "slice")
	first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
	newLen := max(final-first, 0)
	c := speciesConstructor(cx, this.Object(), cx.Intrinsic(storage.IntrinsicSharedArrayBuffer))
	r := cx.Construct(c, []storage.Value{storage.Number(float64(newLen))}, c)
	nb := storage.ArrayBufferData(storage.ObjectValue(r))
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
		storage.CopyBufferBytes(nb, 0, b, first, min(newLen, cur-first))
	}
	return storage.ObjectValue(r)
}
