// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func thisDataView(cx *engine.ExecutionContext, this storage.Value, what string) *storage.DataView {
	d := storage.DataViewData(this)
	if d == nil {
		cx.ThrowTypeError("DataView.prototype." + what + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return d
}

// viewElementBytes returns the bytes from the element of type typ at the converted index, as
// GetViewValue and SetViewValue check it, from the bytes b of the view.
func viewElementBytes(cx *engine.ExecutionContext, b []byte, inBounds bool, typ element.Type, i int64, what string) []byte {
	if !inBounds {
		cx.ThrowTypeError("DataView.prototype." + what + ": the view is detached or out of bounds")
	}
	if i+int64(typ.Size()) > int64(len(b)) {
		cx.ThrowRangeError("offset is outside the bounds of the DataView")
	}
	return b[i:]
}

func loadViewElement(cx *engine.ExecutionContext, d *storage.DataView, typ element.Type, i int64, le bool, what string) storage.Value {
	// A throw releases the lock of the buffer.
	b, inBounds := d.Lock()
	defer d.Unlock()
	return storage.GetValueFromBuffer(viewElementBytes(cx, b, inBounds, typ, i, what), typ, le)
}

func storeViewElement(cx *engine.ExecutionContext, d *storage.DataView, typ element.Type, i int64, v storage.Value, le bool, what string) {
	// A throw releases the lock of the buffer.
	b, inBounds := d.Lock()
	defer d.Unlock()
	storage.SetValueInBuffer(viewElementBytes(cx, b, inBounds, typ, i, what), typ, v, le)
}

var (
	nativeDataViewConstructor = engine.RegisterNative(dataViewConstructor)
	nativeDataViewBuffer      = engine.RegisterNative(dataViewBuffer)
	nativeDataViewByteLength  = engine.RegisterNative(dataViewByteLength)
	nativeDataViewByteOffset  = engine.RegisterNative(dataViewByteOffset)
)

func initDataView(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(storage.IntrinsicDataViewPrototype, proto)
	ctor := newConstructor(cx, "DataView", 1, storage.IntrinsicDataViewPrototype, nativeDataViewConstructor)
	cx.FillBinaryGlobal("DataView", ctor)

	getter(cx, proto, cx.Intern("buffer"), "buffer", nativeDataViewBuffer)
	getter(cx, proto, cx.Intern("byteLength"), "byteLength", nativeDataViewByteLength)
	getter(cx, proto, cx.Intern("byteOffset"), "byteOffset", nativeDataViewByteOffset)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("DataView"), storage.FlagConfigurable)

	method(cx, proto, "getInt8", 1, nativeDataViewGetInt8)
	method(cx, proto, "setInt8", 2, nativeDataViewSetInt8)
	method(cx, proto, "getUint8", 1, nativeDataViewGetUint8)
	method(cx, proto, "setUint8", 2, nativeDataViewSetUint8)
	method(cx, proto, "getInt16", 1, nativeDataViewGetInt16)
	method(cx, proto, "setInt16", 2, nativeDataViewSetInt16)
	method(cx, proto, "getUint16", 1, nativeDataViewGetUint16)
	method(cx, proto, "setUint16", 2, nativeDataViewSetUint16)
	method(cx, proto, "getInt32", 1, nativeDataViewGetInt32)
	method(cx, proto, "setInt32", 2, nativeDataViewSetInt32)
	method(cx, proto, "getUint32", 1, nativeDataViewGetUint32)
	method(cx, proto, "setUint32", 2, nativeDataViewSetUint32)
	method(cx, proto, "getFloat16", 1, nativeDataViewGetFloat16)
	method(cx, proto, "setFloat16", 2, nativeDataViewSetFloat16)
	method(cx, proto, "getFloat32", 1, nativeDataViewGetFloat32)
	method(cx, proto, "setFloat32", 2, nativeDataViewSetFloat32)
	method(cx, proto, "getFloat64", 1, nativeDataViewGetFloat64)
	method(cx, proto, "setFloat64", 2, nativeDataViewSetFloat64)
	method(cx, proto, "getBigInt64", 1, nativeDataViewGetBigInt64)
	method(cx, proto, "setBigInt64", 2, nativeDataViewSetBigInt64)
	method(cx, proto, "getBigUint64", 1, nativeDataViewGetBigUint64)
	method(cx, proto, "setBigUint64", 2, nativeDataViewSetBigUint64)
}

func dataViewConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor DataView requires 'new'")
	}
	buf := arg(args, 0)
	b := storage.ArrayBufferData(buf)
	if b == nil {
		cx.ThrowTypeError("first argument to DataView constructor must be an ArrayBuffer")
	}
	offset := toIndex(cx, arg(args, 1))
	if b.IsDetached() {
		cx.ThrowTypeError("cannot construct a DataView on a detached ArrayBuffer")
	}
	// The length of the buffer is taken before converting the view length, which can run
	// JavaScript.
	bufferLength := int64(b.ByteLength())
	if offset > bufferLength {
		cx.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
	}
	length := arg(args, 2)
	viewLength := int64(-1)
	if length.IsUndefined() {
		if !b.IsResizable() {
			viewLength = bufferLength - offset
		}
	} else {
		viewLength = toIndex(cx, length)
		if offset+viewLength > bufferLength {
			cx.ThrowRangeError("invalid DataView length " + itoa(viewLength))
		}
	}
	p := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicDataViewPrototype)
	// Getting the prototype may have detached or shrunk the buffer.
	if b.IsDetached() {
		cx.ThrowTypeError("cannot construct a DataView on a detached ArrayBuffer")
	}
	if offset > int64(b.ByteLength()) {
		cx.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
	}
	if !length.IsUndefined() && offset+viewLength > int64(b.ByteLength()) {
		cx.ThrowRangeError("invalid DataView length " + itoa(viewLength))
	}
	return storage.ObjectValue(cx.NewDataViewObject(p, buf.Object(), int(offset), int(viewLength)))
}

func dataViewBuffer(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.ObjectValue(thisDataView(cx, this, "buffer").Buffer())
}

func dataViewByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	n, _ := thisDataView(cx, this, "byteLength").ByteLength()
	return storage.Number(float64(n))
}

func dataViewByteOffset(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	d := thisDataView(cx, this, "byteOffset")
	if _, ok := d.ByteLength(); !ok {
		return storage.Number(0)
	}
	return storage.Number(float64(d.ByteOffset()))
}

var (
	nativeDataViewGetInt8      = engine.RegisterNative(dataViewGetNative("getInt8", element.Int8))
	nativeDataViewGetUint8     = engine.RegisterNative(dataViewGetNative("getUint8", element.Uint8))
	nativeDataViewGetInt16     = engine.RegisterNative(dataViewGetNative("getInt16", element.Int16))
	nativeDataViewGetUint16    = engine.RegisterNative(dataViewGetNative("getUint16", element.Uint16))
	nativeDataViewGetInt32     = engine.RegisterNative(dataViewGetNative("getInt32", element.Int32))
	nativeDataViewGetUint32    = engine.RegisterNative(dataViewGetNative("getUint32", element.Uint32))
	nativeDataViewGetFloat16   = engine.RegisterNative(dataViewGetNative("getFloat16", element.Float16))
	nativeDataViewGetFloat32   = engine.RegisterNative(dataViewGetNative("getFloat32", element.Float32))
	nativeDataViewGetFloat64   = engine.RegisterNative(dataViewGetNative("getFloat64", element.Float64))
	nativeDataViewGetBigInt64  = engine.RegisterNative(dataViewGetNative("getBigInt64", element.BigInt64))
	nativeDataViewGetBigUint64 = engine.RegisterNative(dataViewGetNative("getBigUint64", element.BigUint64))
)

// dataViewGetNative returns the method name of DataView.prototype, which reads an element of typ.
//
//go:noinline
func dataViewGetNative(name string, typ element.Type) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		d := thisDataView(cx, this, name)
		i := toIndex(cx, arg(args, 0))
		le := storage.ToBoolean(arg(args, 1))
		return loadViewElement(cx, d, typ, i, le, name)
	}
}

var (
	nativeDataViewSetInt8      = engine.RegisterNative(dataViewSetNative("setInt8", element.Int8))
	nativeDataViewSetUint8     = engine.RegisterNative(dataViewSetNative("setUint8", element.Uint8))
	nativeDataViewSetInt16     = engine.RegisterNative(dataViewSetNative("setInt16", element.Int16))
	nativeDataViewSetUint16    = engine.RegisterNative(dataViewSetNative("setUint16", element.Uint16))
	nativeDataViewSetInt32     = engine.RegisterNative(dataViewSetNative("setInt32", element.Int32))
	nativeDataViewSetUint32    = engine.RegisterNative(dataViewSetNative("setUint32", element.Uint32))
	nativeDataViewSetFloat16   = engine.RegisterNative(dataViewSetNative("setFloat16", element.Float16))
	nativeDataViewSetFloat32   = engine.RegisterNative(dataViewSetNative("setFloat32", element.Float32))
	nativeDataViewSetFloat64   = engine.RegisterNative(dataViewSetNative("setFloat64", element.Float64))
	nativeDataViewSetBigInt64  = engine.RegisterNative(dataViewSetNative("setBigInt64", element.BigInt64))
	nativeDataViewSetBigUint64 = engine.RegisterNative(dataViewSetNative("setBigUint64", element.BigUint64))
)

// dataViewSetNative returns the method name of DataView.prototype, which writes an element of typ.
//
//go:noinline
func dataViewSetNative(name string, typ element.Type) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		d := thisDataView(cx, this, name)
		if storage.ArrayBufferData(storage.ObjectValue(d.Buffer())).IsImmutable() {
			cx.ThrowTypeError("DataView.prototype." + name + ": the DataView is backed by an immutable ArrayBuffer")
		}
		i := toIndex(cx, arg(args, 0))
		v := cx.ToElementValue(typ, arg(args, 1))
		le := storage.ToBoolean(arg(args, 2))
		storeViewElement(cx, d, typ, i, v, le, name)
		return storage.Undefined
	}
}
