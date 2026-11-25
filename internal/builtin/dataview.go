// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func thisDataView(cx *engine.ExecutionContext, this engine.Value, what string) *engine.DataView {
	d := engine.DataViewData(this)
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

func loadViewElement(cx *engine.ExecutionContext, d *engine.DataView, typ element.Type, i int64, le bool, what string) engine.Value {
	// A throw releases the lock of the buffer.
	b, inBounds := d.Lock()
	defer d.Unlock()
	return cx.GetValueFromBuffer(viewElementBytes(cx, b, inBounds, typ, i, what), typ, le)
}

func storeViewElement(cx *engine.ExecutionContext, d *engine.DataView, typ element.Type, i int64, v engine.Value, le bool, what string) {
	// A throw releases the lock of the buffer.
	b, inBounds := d.Lock()
	defer d.Unlock()
	engine.SetValueInBuffer(viewElementBytes(cx, b, inBounds, typ, i, what), typ, v, le)
}

func initDataView(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicDataViewPrototype, proto)
	constructor(cx, "DataView", 1, engine.IntrinsicDataViewPrototype, dataViewConstructor)

	getter(cx, proto, cx.Intern("buffer"), "buffer", dataViewBuffer)
	getter(cx, proto, cx.Intern("byteLength"), "byteLength", dataViewByteLength)
	getter(cx, proto, cx.Intern("byteOffset"), "byteOffset", dataViewByteOffset)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("DataView"), engine.FlagConfigurable)

	method(cx, proto, "getInt8", 1, dataViewGetInt8)
	method(cx, proto, "setInt8", 2, dataViewSetInt8)
	method(cx, proto, "getUint8", 1, dataViewGetUint8)
	method(cx, proto, "setUint8", 2, dataViewSetUint8)
	method(cx, proto, "getInt16", 1, dataViewGetInt16)
	method(cx, proto, "setInt16", 2, dataViewSetInt16)
	method(cx, proto, "getUint16", 1, dataViewGetUint16)
	method(cx, proto, "setUint16", 2, dataViewSetUint16)
	method(cx, proto, "getInt32", 1, dataViewGetInt32)
	method(cx, proto, "setInt32", 2, dataViewSetInt32)
	method(cx, proto, "getUint32", 1, dataViewGetUint32)
	method(cx, proto, "setUint32", 2, dataViewSetUint32)
	method(cx, proto, "getFloat16", 1, dataViewGetFloat16)
	method(cx, proto, "setFloat16", 2, dataViewSetFloat16)
	method(cx, proto, "getFloat32", 1, dataViewGetFloat32)
	method(cx, proto, "setFloat32", 2, dataViewSetFloat32)
	method(cx, proto, "getFloat64", 1, dataViewGetFloat64)
	method(cx, proto, "setFloat64", 2, dataViewSetFloat64)
	method(cx, proto, "getBigInt64", 1, dataViewGetBigInt64)
	method(cx, proto, "setBigInt64", 2, dataViewSetBigInt64)
	method(cx, proto, "getBigUint64", 1, dataViewGetBigUint64)
	method(cx, proto, "setBigUint64", 2, dataViewSetBigUint64)
}

func dataViewConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor DataView requires 'new'")
	}
	buf := arg(args, 0)
	b := engine.ArrayBufferData(buf)
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
	p := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicDataViewPrototype)
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
	return engine.ObjectValue(cx.NewDataViewObject(p, buf.Object(), int(offset), int(viewLength)))
}

func dataViewBuffer(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.ObjectValue(thisDataView(cx, this, "buffer").Buffer())
}

func dataViewByteLength(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	n, _ := thisDataView(cx, this, "byteLength").ByteLength()
	return engine.Number(float64(n))
}

func dataViewByteOffset(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	d := thisDataView(cx, this, "byteOffset")
	if _, ok := d.ByteLength(); !ok {
		return engine.Number(0)
	}
	return engine.Number(float64(d.ByteOffset()))
}

var (
	dataViewGetInt8      = dataViewGetNative("getInt8", element.Int8)
	dataViewGetUint8     = dataViewGetNative("getUint8", element.Uint8)
	dataViewGetInt16     = dataViewGetNative("getInt16", element.Int16)
	dataViewGetUint16    = dataViewGetNative("getUint16", element.Uint16)
	dataViewGetInt32     = dataViewGetNative("getInt32", element.Int32)
	dataViewGetUint32    = dataViewGetNative("getUint32", element.Uint32)
	dataViewGetFloat16   = dataViewGetNative("getFloat16", element.Float16)
	dataViewGetFloat32   = dataViewGetNative("getFloat32", element.Float32)
	dataViewGetFloat64   = dataViewGetNative("getFloat64", element.Float64)
	dataViewGetBigInt64  = dataViewGetNative("getBigInt64", element.BigInt64)
	dataViewGetBigUint64 = dataViewGetNative("getBigUint64", element.BigUint64)
)

// dataViewGetNative returns the method name of DataView.prototype, which reads an element of typ.
//
//go:noinline
func dataViewGetNative(name string, typ element.Type) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisDataView(cx, this, name)
		i := toIndex(cx, arg(args, 0))
		le := engine.ToBoolean(arg(args, 1))
		return loadViewElement(cx, d, typ, i, le, name)
	}
}

var (
	dataViewSetInt8      = dataViewSetNative("setInt8", element.Int8)
	dataViewSetUint8     = dataViewSetNative("setUint8", element.Uint8)
	dataViewSetInt16     = dataViewSetNative("setInt16", element.Int16)
	dataViewSetUint16    = dataViewSetNative("setUint16", element.Uint16)
	dataViewSetInt32     = dataViewSetNative("setInt32", element.Int32)
	dataViewSetUint32    = dataViewSetNative("setUint32", element.Uint32)
	dataViewSetFloat16   = dataViewSetNative("setFloat16", element.Float16)
	dataViewSetFloat32   = dataViewSetNative("setFloat32", element.Float32)
	dataViewSetFloat64   = dataViewSetNative("setFloat64", element.Float64)
	dataViewSetBigInt64  = dataViewSetNative("setBigInt64", element.BigInt64)
	dataViewSetBigUint64 = dataViewSetNative("setBigUint64", element.BigUint64)
)

// dataViewSetNative returns the method name of DataView.prototype, which writes an element of typ.
//
//go:noinline
func dataViewSetNative(name string, typ element.Type) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisDataView(cx, this, name)
		if engine.ArrayBufferData(engine.ObjectValue(d.Buffer())).IsImmutable() {
			cx.ThrowTypeError("DataView.prototype." + name + ": the DataView is backed by an immutable ArrayBuffer")
		}
		i := toIndex(cx, arg(args, 0))
		v := cx.ToElementValue(typ, arg(args, 1))
		le := engine.ToBoolean(arg(args, 2))
		storeViewElement(cx, d, typ, i, v, le, name)
		return engine.Undefined
	}
}
