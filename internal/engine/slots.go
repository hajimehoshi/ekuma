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

// IsError reports whether v is an object with an [[ErrorData]] internal slot.
func IsError(v Value) bool {
	return v.IsObject() && v.o.class == classError
}

// ErrorStack returns the stack trace that RecordStack recorded for the error object v.
// It reports false when v has no [[ErrorData]] internal slot or no recorded stack trace.
func ErrorStack(v Value) (string, bool) {
	if !IsError(v) {
		return "", false
	}
	s, ok := v.o.ext.(string)
	return s, ok
}

// NewIteratorObject returns a built-in iterator object with the prototype proto.
// state is what IteratorState returns for it.
func (vm *VM) NewIteratorObject(proto *Object, state any) *Object {
	it := vm.NewObject(proto)
	it.class = classIterator
	it.ext = state
	return it
}

// NewMapObject returns a Map object with the prototype proto whose [[MapData]] is data.
func (vm *VM) NewMapObject(proto *Object, data any) *Object {
	o := vm.NewObject(proto)
	o.class = classMap
	o.ext = data
	return o
}

// MapData returns the [[MapData]] of the Map object v, or nil when v is not a Map object.
func MapData(v Value) any {
	if v.IsObject() && v.o.class == classMap {
		return v.o.ext
	}
	return nil
}

// NewSetObject returns a Set object with the prototype proto whose [[SetData]] is data.
func (vm *VM) NewSetObject(proto *Object, data any) *Object {
	o := vm.NewObject(proto)
	o.class = classSet
	o.ext = data
	return o
}

// SetData returns the [[SetData]] of the Set object v, or nil when v is not a Set object.
func SetData(v Value) any {
	if v.IsObject() && v.o.class == classSet {
		return v.o.ext
	}
	return nil
}

// NewDateObject returns a Date object with the prototype proto whose [[DateValue]] is tv.
func (vm *VM) NewDateObject(proto *Object, tv float64) *Object {
	o := vm.NewObject(proto)
	o.class = classDate
	o.ext = tv
	return o
}

// ThisTimeValue implements thisTimeValue. It reports false when the specification throws.
func ThisTimeValue(v Value) (float64, bool) {
	if v.IsObject() && v.o.class == classDate {
		return v.o.ext.(float64), true
	}
	return 0, false
}

// SetDateValue sets the [[DateValue]] of the Date object v to tv.
func SetDateValue(v Value, tv float64) {
	v.o.ext = tv
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

// ThisBigIntValue implements ThisBigIntValue. It reports false when the specification throws.
func ThisBigIntValue(v Value) (Value, bool) {
	if v.IsBigInt() {
		return v, true
	}
	if v.IsObject() && v.o.class == classBigInt {
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
func (vm *VM) BuiltinTag(o *Object) string {
	switch {
	case vm.IsArray(Value{o: o}):
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

// NewRawJSONObject returns an object without any property or prototype
// that has the internal slot [[IsRawJSON]].
func (vm *VM) NewRawJSONObject() *Object {
	o := vm.NewObject(nil)
	o.class = classRawJSON
	return o
}

// IsRawJSON reports whether o has the internal slot [[IsRawJSON]].
func (o *Object) IsRawJSON() bool {
	return o.class == classRawJSON
}

// NewDisposableStackObject returns a DisposableStack object, or an AsyncDisposableStack object
// when async is set, with the prototype proto. state is what DisposableStackState returns for it.
func (vm *VM) NewDisposableStackObject(proto *Object, async bool, state any) *Object {
	o := vm.NewObject(proto)
	o.class = classDisposableStack
	if async {
		o.class = classAsyncDisposableStack
	}
	o.ext = state
	return o
}

// DisposableStackState returns the state of v when it is a DisposableStack object, or
// an AsyncDisposableStack object when async is set, and nil otherwise.
func DisposableStackState(v Value, async bool) any {
	c := classDisposableStack
	if async {
		c = classAsyncDisposableStack
	}
	if v.IsObject() && v.o.class == c {
		return v.o.ext
	}
	return nil
}
