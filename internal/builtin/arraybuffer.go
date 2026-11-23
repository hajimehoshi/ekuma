// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// binaryIntrinsics are the intrinsic objects of the binary data built-ins of one realm.
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
func initBinary(cx *engine.ExecutionContext) {
	bi := &binaryIntrinsics{}
	initArrayBuffer(cx, bi)
	initSharedArrayBuffer(cx, bi)
	initTypedArrays(cx, bi)
	initDataView(cx, bi)
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
	first := engine.RelativeIndex(cx.ToIntegerOrInfinity(start), length)
	final := length
	if !end.IsUndefined() {
		final = engine.RelativeIndex(cx.ToIntegerOrInfinity(end), length)
	}
	return first, final
}

func initArrayBuffer(cx *engine.ExecutionContext, bi *binaryIntrinsics) {
	proto := cx.NewOrdinaryObject()
	bi.arrayBufferProto = proto
	cx.SetIntrinsic(engine.IntrinsicArrayBufferPrototype, proto)
	ctor := constructor(cx, "ArrayBuffer", 1, engine.IntrinsicArrayBufferPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor ArrayBuffer requires 'new'")
		}
		n := toIndex(cx, arg(args, 0))
		m := maxByteLengthOption(cx, arg(args, 1))
		return engine.ObjectValue(allocateArrayBuffer(cx, nt, n, m, false))
	})
	bi.arrayBufferCtor = ctor
	speciesGetter(cx, ctor)
	method(cx, ctor, "isView", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		a := arg(args, 0)
		return engine.Bool(engine.TypedArrayData(a) != nil || engine.DataViewData(a) != nil)
	})

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(cx, this, false, "byteLength").ByteLength()))
	})
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(cx, this, false, "maxByteLength")
		if b.IsDetached() {
			return engine.Number(0)
		}
		return engine.Number(float64(b.MaxByteLength()))
	})
	getter(cx, proto, cx.Intern("resizable"), "resizable", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(cx, this, false, "resizable").IsResizable())
	})
	getter(cx, proto, cx.Intern("detached"), "detached", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(cx, this, false, "detached").IsDetached())
	})
	getter(cx, proto, cx.Intern("immutable"), "immutable", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(cx, this, false, "immutable").IsImmutable())
	})
	method(cx, proto, "slice", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(cx, this, false, "slice")
		if b.IsDetached() {
			cx.ThrowTypeError("cannot perform ArrayBuffer.prototype.slice on a detached ArrayBuffer")
		}
		first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
		newLen := max(final-first, 0)
		c := speciesConstructor(cx, this.Object(), ctor)
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
	})
	method(cx, proto, "sliceToImmutable", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		return engine.ObjectValue(cx.NewImmutableArrayBufferFromBytes(proto, data))
	})
	method(cx, proto, "resize", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	// transfer implements ArrayBufferCopyAndDetach as the method name.
	// preserve is ~preserve-resizability~, and immutable is ~immutable~.
	transfer := func(name string, preserve, immutable bool) {
		method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
			return engine.ObjectValue(cx.TransferArrayBuffer(this.Object(), proto, n, m, immutable))
		})
	}
	transfer("transfer", true, false)
	transfer("transferToFixedLength", false, false)
	transfer("transferToImmutable", false, true)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("ArrayBuffer"), engine.FlagConfigurable)
}

func initSharedArrayBuffer(cx *engine.ExecutionContext, bi *binaryIntrinsics) {
	proto := cx.NewOrdinaryObject()
	bi.sharedArrayBufferProto = proto
	cx.SetIntrinsic(engine.IntrinsicSharedArrayBufferPrototype, proto)
	ctor := constructor(cx, "SharedArrayBuffer", 1, engine.IntrinsicSharedArrayBufferPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		if nt == nil {
			cx.ThrowTypeError("constructor SharedArrayBuffer requires 'new'")
		}
		n := toIndex(cx, arg(args, 0))
		m := maxByteLengthOption(cx, arg(args, 1))
		return engine.ObjectValue(allocateArrayBuffer(cx, nt, n, m, true))
	})
	bi.sharedArrayBufferCtor = ctor
	speciesGetter(cx, ctor)

	getter(cx, proto, cx.Intern("byteLength"), "byteLength", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(cx, this, true, "byteLength").ByteLength()))
	})
	getter(cx, proto, cx.Intern("maxByteLength"), "maxByteLength", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(thisArrayBuffer(cx, this, true, "maxByteLength").MaxByteLength()))
	})
	getter(cx, proto, cx.Intern("growable"), "growable", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(thisArrayBuffer(cx, this, true, "growable").IsResizable())
	})
	method(cx, proto, "grow", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	method(cx, proto, "slice", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		b := thisArrayBuffer(cx, this, true, "slice")
		first, final := relativeRange(cx, arg(args, 0), arg(args, 1), b.ByteLength())
		newLen := max(final-first, 0)
		c := speciesConstructor(cx, this.Object(), ctor)
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
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("SharedArrayBuffer"), engine.FlagConfigurable)
}
