// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// This file holds the operations on the internal slots of the built-in objects.

// NewNumberObject returns a Number object for n with the prototype proto.
func (vm *VM) NewNumberObject(n float64, proto *Object) *Object {
	w := vm.NewObject(proto)
	w.class = classNumber
	w.ext = n
	return w
}

// NewBooleanObject returns a Boolean object for b with the prototype proto.
func (vm *VM) NewBooleanObject(b bool, proto *Object) *Object {
	w := vm.NewObject(proto)
	w.class = classBoolean
	w.ext = b
	return w
}

// NewErrorObject returns an Error object without any property, with the prototype proto.
func (vm *VM) NewErrorObject(proto *Object) *Object {
	e := vm.NewObject(proto)
	e.class = classError
	return e
}

// NewIteratorObject returns a built-in iterator object with the prototype proto.
// state is what IteratorState returns for it.
func (vm *VM) NewIteratorObject(proto *Object, state any) *Object {
	it := vm.NewObject(proto)
	it.class = classIterator
	it.ext = state
	return it
}

// IteratorState returns the state of the built-in iterator object v,
// or nil when v is not such an object.
func IteratorState(v Value) any {
	if v.IsObject() && v.o.class == classIterator {
		return v.o.ext
	}
	return nil
}

// ThisNumberValue implements ThisNumberValue. It reports false when the specification throws.
func ThisNumberValue(v Value) (float64, bool) {
	if v.o == nil {
		return v.n, true
	}
	if v.IsObject() && v.o.class == classNumber {
		return v.o.ext.(float64), true
	}
	return 0, false
}

// ThisBooleanValue implements ThisBooleanValue. It reports false when the specification throws.
func ThisBooleanValue(v Value) (bool, bool) {
	if v.IsBool() {
		return v.o == trueObject, true
	}
	if v.IsObject() && v.o.class == classBoolean {
		return v.o.ext.(bool), true
	}
	return false, false
}

// ThisStringValue implements ThisStringValue. It reports false when the specification throws.
func ThisStringValue(v Value) (Value, bool) {
	if v.IsString() {
		return v, true
	}
	if v.IsObject() && v.o.class == classString {
		return Value{o: v.o.ext.(*Object)}, true
	}
	return Undefined, false
}

// ThisSymbolValue implements ThisSymbolValue. It reports false when the specification throws.
func ThisSymbolValue(v Value) (*Object, bool) {
	if v.IsSymbol() {
		return v.o, true
	}
	if v.IsObject() && v.o.class == classSymbol {
		return v.o.ext.(*Object), true
	}
	return nil, false
}

// SymbolDescription returns the description of the symbol sym and whether it has one.
func SymbolDescription(sym *Object) (string, bool) {
	return sym.str, sym.ext != nil
}

// BuiltinTag returns the name that Object.prototype.toString uses for o
// when o has no Symbol.toStringTag property.
func (o *Object) BuiltinTag() string {
	switch {
	case o.kind == kindArray:
		return "Array"
	case o.fn != nil:
		return "Function"
	}
	switch o.class {
	case classError:
		return "Error"
	case classBoolean:
		return "Boolean"
	case classNumber:
		return "Number"
	case classString:
		return "String"
	case classDate:
		return "Date"
	case classRegExp:
		return "RegExp"
	case classArguments:
		return "Arguments"
	}
	return "Object"
}

// IsRegExpObject reports whether o is a RegExp object.
func (o *Object) IsRegExpObject() bool {
	return o.class == classRegExp
}
