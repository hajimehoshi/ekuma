// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// TemporalData returns the internal slots of the Temporal object v, or nil when v is not one.
func TemporalData(v Value) any {
	if v.IsObject() && v.O.Class == ClassTemporal {
		return v.O.ExtState()
	}
	return nil
}

// IntlData returns the internal slots of the Intl object v, or nil when v is not one.
func IntlData(v Value) any {
	if v.IsObject() && v.O.Class == ClassIntl {
		return v.O.ExtState()
	}
	return nil
}

// ThisTimeValue implements thisTimeValue. It reports false when the specification throws.
func ThisTimeValue(v Value) (float64, bool) {
	if v.IsObject() && v.O.Class == ClassDate {
		return *v.O.ExtState().(*float64), true
	}
	return 0, false
}

// SetDateValue sets the [[DateValue]] of the Date object v to tv.
func SetDateValue(v Value, tv float64) {
	*v.O.ExtState().(*float64) = tv
}

// IteratorState returns the state of the built-in iterator object v,
// or nil when v is not such an object.
func IteratorState(v Value) any {
	if v.O != nil && v.O.Class == ClassIterator {
		return v.O.ExtState()
	}
	return nil
}

// ThisNumberValue implements ThisNumberValue. It reports false when the specification throws.
func ThisNumberValue(v Value) (float64, bool) {
	if v.O == NumberObject {
		return v.N, true
	}
	if v.IsObject() && v.O.Class == ClassNumber {
		return v.O.ExtState().(float64), true
	}
	return 0, false
}

// ThisBooleanValue implements ThisBooleanValue. It reports false when the specification throws.
func ThisBooleanValue(v Value) (bool, bool) {
	if v.IsBool() {
		return v.O == TrueObject, true
	}
	if v.IsObject() && v.O.Class == ClassBoolean {
		return v.O.ExtState().(bool), true
	}
	return false, false
}

// ThisStringValue implements ThisStringValue. It reports false when the specification throws.
func ThisStringValue(v Value) (Value, bool) {
	if v.IsString() {
		return v, true
	}
	if v.IsObject() && v.O.Class == ClassString {
		return ObjectValue(v.O.ExtState().(*Object)), true
	}
	return Undefined, false
}

// ThisBigIntValue implements ThisBigIntValue. It reports false when the specification throws.
func ThisBigIntValue(v Value) (Value, bool) {
	if v.IsBigInt() {
		return v, true
	}
	if v.IsObject() && v.O.Class == ClassBigInt {
		return v.O.ExtState().(Value), true
	}
	return Undefined, false
}

// ThisSymbolValue implements ThisSymbolValue. It reports false when the specification throws.
func ThisSymbolValue(v Value) (*Object, bool) {
	if v.IsSymbol() {
		return v.O, true
	}
	if v.IsObject() && v.O.Class == ClassSymbol {
		return v.O.ExtState().(*Object), true
	}
	return nil, false
}

// IsRawJSON reports whether o has the internal slot [[IsRawJSON]].
func (o *Object) IsRawJSON() bool {
	return o.Class == ClassRawJSON
}

// DisposableStackState returns the state of v when it is a DisposableStack object, or
// an AsyncDisposableStack object when async is set, and nil otherwise.
func DisposableStackState(v Value, async bool) any {
	c := ClassDisposableStack
	if async {
		c = ClassAsyncDisposableStack
	}
	if v.IsObject() && v.O.Class == c {
		return v.O.ExtState()
	}
	return nil
}
