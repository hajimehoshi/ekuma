// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// ErrorData is the internal state of an error object that has a recorded stack trace.
type ErrorData struct {
	stack wtf8.String

	// Pending is set until stack is formatted from name, msg and calls.
	Pending bool

	// Name in WTF-8 and msg in UTF-8 begin the stack trace, and calls are its lines.
	Name  wtf8.String
	Msg   string
	Calls []StackCall

	// GoErr is the Go error that the error object was made from, or nil.
	GoErr error
}

// IsError reports whether v is an object with an [[ErrorData]] internal slot.
func IsError(v Value) bool {
	return v.IsObject() && v.O.Class == ClassError
}

// ErrorStack returns the stack trace that agent.RecordStack recorded for the error object v.
// It reports false when v has no [[ErrorData]] internal slot or no recorded stack trace.
func ErrorStack(v Value) (wtf8.String, bool) {
	if !IsError(v) {
		return "", false
	}
	d, ok := v.O.ExtState().(*ErrorData)
	if !ok {
		return "", false
	}
	if d.Pending {
		d.stack = formatStack(d.Name, d.Msg, d.Calls)
		d.Pending = false
		d.Name, d.Msg, d.Calls = "", "", nil
	}
	return d.stack, true
}

// GoError returns the Go error that v was made from by ExecutionContext.NewGoError, or nil when it was not.
func GoError(v Value) error {
	if !IsError(v) {
		return nil
	}
	if d, ok := v.O.ExtState().(*ErrorData); ok {
		return d.GoErr
	}
	return nil
}

// NewMapObject returns a Map object with the prototype proto whose [[MapData]] is data.
func (r *RealmPart) NewMapObject(proto *Object, data any) *Object {
	return NewObjectWithState(r.ShapeFor(proto), ClassMap, data)
}

// MapData returns the [[MapData]] of the Map object v, or nil when v is not a Map object.
func MapData(v Value) any {
	if v.IsObject() && v.O.Class == ClassMap {
		return v.O.ExtState()
	}
	return nil
}

// NewSetObject returns a Set object with the prototype proto whose [[SetData]] is data.
func (r *RealmPart) NewSetObject(proto *Object, data any) *Object {
	return NewObjectWithState(r.ShapeFor(proto), ClassSet, data)
}

// SetData returns the [[SetData]] of the Set object v, or nil when v is not a Set object.
func SetData(v Value) any {
	if v.IsObject() && v.O.Class == ClassSet {
		return v.O.ExtState()
	}
	return nil
}

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

// SymbolDescription returns the description of the symbol sym and whether it has one.
func SymbolDescription(sym *Object) (wtf8.String, bool) {
	return sym.Text, sym.ExtState() != nil
}

// IsRegExpObject reports whether o is a RegExp object.
func (o *Object) IsRegExpObject() bool {
	return o.Class == ClassRegExp
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
