// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

func initDataView(cx *engine.ExecutionContext, bi *binaryIntrinsics) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(engine.IntrinsicDataViewPrototype, proto)
	bi.dataViewProto = proto
	constructor(cx, "DataView", 1, engine.IntrinsicDataViewPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})

	getter(cx, proto, cx.Intern("buffer"), "buffer", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(thisDataView(cx, this, "buffer").Buffer())
	})
	getter(cx, proto, cx.Intern("byteLength"), "byteLength", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		n, _ := thisDataView(cx, this, "byteLength").ByteLength()
		return engine.Number(float64(n))
	})
	getter(cx, proto, cx.Intern("byteOffset"), "byteOffset", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		d := thisDataView(cx, this, "byteOffset")
		if _, ok := d.ByteLength(); !ok {
			return engine.Number(0)
		}
		return engine.Number(float64(d.ByteOffset()))
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("DataView"), engine.FlagConfigurable)

	for _, e := range []struct {
		name string
		typ  element.Type
	}{
		{"Int8", element.Int8},
		{"Uint8", element.Uint8},
		{"Int16", element.Int16},
		{"Uint16", element.Uint16},
		{"Int32", element.Int32},
		{"Uint32", element.Uint32},
		{"Float16", element.Float16},
		{"Float32", element.Float32},
		{"Float64", element.Float64},
		{"BigInt64", element.BigInt64},
		{"BigUint64", element.BigUint64},
	} {
		typ := e.typ
		get := "get" + e.name
		method(cx, proto, wtf8.String(get), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			d := thisDataView(cx, this, get)
			i := toIndex(cx, arg(args, 0))
			le := engine.ToBoolean(arg(args, 1))
			return loadViewElement(cx, d, typ, i, le, get)
		})
		set := "set" + e.name
		method(cx, proto, wtf8.String(set), 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			d := thisDataView(cx, this, set)
			if engine.ArrayBufferData(engine.ObjectValue(d.Buffer())).IsImmutable() {
				cx.ThrowTypeError("DataView.prototype." + set + ": the DataView is backed by an immutable ArrayBuffer")
			}
			i := toIndex(cx, arg(args, 0))
			v := cx.ToElementValue(typ, arg(args, 1))
			le := engine.ToBoolean(arg(args, 2))
			storeViewElement(cx, d, typ, i, v, le, set)
			return engine.Undefined
		})
	}
}
