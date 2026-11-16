// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initDataView(vm *engine.VM, bi *binaryIntrinsics) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(engine.IntrinsicDataViewPrototype, proto)
	bi.dataViewProto = proto
	constructor(vm, "DataView", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		if nt == nil {
			vm.ThrowTypeError("constructor DataView requires 'new'")
		}
		buf := arg(args, 0)
		b := engine.ArrayBufferData(buf)
		if b == nil {
			vm.ThrowTypeError("first argument to DataView constructor must be an ArrayBuffer")
		}
		offset := toIndex(vm, arg(args, 1))
		if b.IsDetached() {
			vm.ThrowTypeError("cannot construct a DataView on a detached ArrayBuffer")
		}
		// The length of the buffer is taken before converting the view length, which can run
		// JavaScript.
		bufferLength := int64(b.ByteLength())
		if offset > bufferLength {
			vm.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
		}
		length := arg(args, 2)
		viewLength := int64(-1)
		if length.IsUndefined() {
			if !b.IsResizable() {
				viewLength = bufferLength - offset
			}
		} else {
			viewLength = toIndex(vm, length)
			if offset+viewLength > bufferLength {
				vm.ThrowRangeError("invalid DataView length " + itoa(viewLength))
			}
		}
		p := vm.GetPrototypeFromConstructor(nt, proto)
		// Getting the prototype may have detached or shrunk the buffer.
		if b.IsDetached() {
			vm.ThrowTypeError("cannot construct a DataView on a detached ArrayBuffer")
		}
		if offset > int64(b.ByteLength()) {
			vm.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
		}
		if !length.IsUndefined() && offset+viewLength > int64(b.ByteLength()) {
			vm.ThrowRangeError("invalid DataView length " + itoa(viewLength))
		}
		return engine.ObjectValue(vm.NewDataViewObject(p, buf.Object(), int(offset), int(viewLength)))
	})

	thisView := func(vm *engine.VM, this engine.Value, what string) *engine.DataView {
		d := engine.DataViewData(this)
		if d == nil {
			vm.ThrowTypeError("DataView.prototype." + what + " called on incompatible receiver " + vm.DisplayString(this))
		}
		return d
	}
	getter(vm, proto, vm.Intern("buffer"), "buffer", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(thisView(vm, this, "buffer").Buffer())
	})
	getter(vm, proto, vm.Intern("byteLength"), "byteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n, _ := thisView(vm, this, "byteLength").ByteLength()
		return engine.Number(float64(n))
	})
	getter(vm, proto, vm.Intern("byteOffset"), "byteOffset", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		d := thisView(vm, this, "byteOffset")
		if _, ok := d.ByteLength(); !ok {
			return engine.Number(0)
		}
		return engine.Number(float64(d.ByteOffset()))
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("DataView"), engine.FlagConfigurable)

	for _, e := range []struct {
		name string
		typ  engine.ElementType
	}{
		{"Int8", engine.ElementInt8},
		{"Uint8", engine.ElementUint8},
		{"Int16", engine.ElementInt16},
		{"Uint16", engine.ElementUint16},
		{"Int32", engine.ElementInt32},
		{"Uint32", engine.ElementUint32},
		{"Float16", engine.ElementFloat16},
		{"Float32", engine.ElementFloat32},
		{"Float64", engine.ElementFloat64},
		{"BigInt64", engine.ElementBigInt64},
		{"BigUint64", engine.ElementBigUint64},
	} {
		typ := e.typ
		// view returns the bytes of the element at the converted index, as GetViewValue and SetViewValue
		// check it, from the bytes b of the view.
		view := func(vm *engine.VM, b []byte, i int64, what string) []byte {
			if b == nil {
				vm.ThrowTypeError("DataView.prototype." + what + ": the view is detached or out of bounds")
			}
			if i+int64(typ.Size()) > int64(len(b)) {
				vm.ThrowRangeError("offset is outside the bounds of the DataView")
			}
			return b[i:]
		}
		// load and store hold the lock of the buffer, which a throw releases.
		load := func(vm *engine.VM, d *engine.DataView, i int64, le bool, what string) engine.Value {
			b := d.Lock()
			defer d.Unlock()
			return vm.GetValueFromBuffer(view(vm, b, i, what), typ, le)
		}
		store := func(vm *engine.VM, d *engine.DataView, i int64, v engine.Value, le bool, what string) {
			b := d.Lock()
			defer d.Unlock()
			engine.SetValueInBuffer(view(vm, b, i, what), typ, v, le)
		}
		get := "get" + e.name
		method(vm, proto, get, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			d := thisView(vm, this, get)
			i := toIndex(vm, arg(args, 0))
			le := engine.ToBoolean(arg(args, 1))
			return load(vm, d, i, le, get)
		})
		set := "set" + e.name
		method(vm, proto, set, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			d := thisView(vm, this, set)
			if engine.ArrayBufferData(engine.ObjectValue(d.Buffer())).IsImmutable() {
				vm.ThrowTypeError("DataView.prototype." + set + ": the DataView is backed by an immutable ArrayBuffer")
			}
			i := toIndex(vm, arg(args, 0))
			v := vm.ToElementValue(typ, arg(args, 1))
			le := engine.ToBoolean(arg(args, 2))
			store(vm, d, i, v, le, set)
			return engine.Undefined
		})
	}
}
