// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initDataView(vm *engine.VM, bi *binaryIntrinsics) {
	proto := vm.NewObject(vm.ObjectProto)
	bi.dataViewProto = proto
	constructor(vm, "DataView", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.NewTarget
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
		if offset > int64(b.ByteLength()) {
			vm.ThrowRangeError("start offset " + itoa(int(offset)) + " is outside the bounds of the buffer")
		}
		length := arg(args, 2)
		viewLength := int64(-1)
		if length.IsUndefined() {
			if !b.IsResizable() {
				viewLength = int64(b.ByteLength()) - offset
			}
		} else {
			viewLength = toIndex(vm, length)
			if offset+viewLength > int64(b.ByteLength()) {
				vm.ThrowRangeError("invalid DataView length " + itoa(int(viewLength)))
			}
		}
		p := vm.GetPrototypeFromConstructor(nt, proto)
		// Getting the prototype may have detached or shrunk the buffer.
		if b.IsDetached() {
			vm.ThrowTypeError("cannot construct a DataView on a detached ArrayBuffer")
		}
		if offset > int64(b.ByteLength()) {
			vm.ThrowRangeError("start offset " + itoa(int(offset)) + " is outside the bounds of the buffer")
		}
		if !length.IsUndefined() && offset+viewLength > int64(b.ByteLength()) {
			vm.ThrowRangeError("invalid DataView length " + itoa(int(viewLength)))
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
	vm.Define(proto, vm.K.SymToStringTag, vm.NewString("DataView"), engine.FlagConfigurable)

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
		// view returns the bytes of the element at the converted index, as GetViewValue and SetViewValue check it.
		view := func(vm *engine.VM, d *engine.DataView, i int64, what string) []byte {
			b := d.Bytes()
			if b == nil {
				vm.ThrowTypeError("DataView.prototype." + what + ": the view is detached or out of bounds")
			}
			if i+int64(typ.Size()) > int64(len(b)) {
				vm.ThrowRangeError("offset is outside the bounds of the DataView")
			}
			return b[i:]
		}
		get := "get" + e.name
		method(vm, proto, get, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			d := thisView(vm, this, get)
			i := toIndex(vm, arg(args, 0))
			le := engine.ToBoolean(arg(args, 1))
			return vm.GetValueFromBuffer(view(vm, d, i, get), typ, le)
		})
		set := "set" + e.name
		method(vm, proto, set, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			d := thisView(vm, this, set)
			i := toIndex(vm, arg(args, 0))
			v := vm.ToElementValue(typ, arg(args, 1))
			le := engine.ToBoolean(arg(args, 2))
			engine.SetValueInBuffer(view(vm, d, i, set), typ, v, le)
			return engine.Undefined
		})
	}
}
