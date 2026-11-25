// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "github.com/hajimehoshi/ekuma/internal/wtf8"

// This file holds the operations on the internal slots of the built-in objects.

// NewNumberObject returns a Number object for n with the prototype proto.
func (cx *ExecutionContext) NewNumberObject(n float64, proto *Object) *Object {
	return newObjectWithState(cx.shapeFor(proto), classNumber, n)
}

// NewBooleanObject returns a Boolean object for b with the prototype proto.
func (cx *ExecutionContext) NewBooleanObject(b bool, proto *Object) *Object {
	return newObjectWithState(cx.shapeFor(proto), classBoolean, b)
}

// errorData is the internal state of an error object that has a recorded stack trace.
type errorData struct {
	stack wtf8.String

	// pending is set until stack is formatted from name, msg and calls.
	pending bool

	// name in WTF-8 and msg in UTF-8 begin the stack trace, and calls are its lines.
	name  wtf8.String
	msg   string
	calls []stackCall

	// goErr is the Go error that the error object was made from, or nil.
	goErr error
}

// NewErrorObject returns an Error object with prototype proto and no own properties.
func (cx *ExecutionContext) NewErrorObject(proto *Object) *Object {
	return newObjectWithState(cx.shapeFor(proto), classError, nil)
}

// IsError reports whether v is an object with an [[ErrorData]] internal slot.
func IsError(v Value) bool {
	return v.IsObject() && v.o.class == classError
}

// ErrorStack returns the stack trace that [agent.RecordStack] recorded for the error object v.
// It reports false when v has no [[ErrorData]] internal slot or no recorded stack trace.
func ErrorStack(v Value) (wtf8.String, bool) {
	if !IsError(v) {
		return "", false
	}
	d, ok := v.o.extState().(*errorData)
	if !ok {
		return "", false
	}
	if d.pending {
		d.stack = formatStack(d.name, d.msg, d.calls)
		d.pending = false
		d.name, d.msg, d.calls = "", "", nil
	}
	return d.stack, true
}

// NewGoError returns a new Error object whose message is the text of err and that
// [GoError] maps back to err.
func (cx *ExecutionContext) NewGoError(err error) Value {
	return cx.newGoError(err.Error(), err)
}

// newGoError returns a new Error object whose message is msg and that [GoError] maps to err.
func (cx *ExecutionContext) newGoError(msg string, err error) Value {
	e := cx.newError(ErrorKindError, msg)
	e.extState().(*errorData).goErr = err
	return Value{o: e}
}

// GoError returns the Go error that v was made from by [ExecutionContext.NewGoError], or nil when it was not.
func GoError(v Value) error {
	if !IsError(v) {
		return nil
	}
	if d, ok := v.o.extState().(*errorData); ok {
		return d.goErr
	}
	return nil
}

// NewIteratorObject returns a built-in iterator object with the prototype proto.
// state is what [IteratorState] returns for it.
func (cx *ExecutionContext) NewIteratorObject(proto *Object, state any) *Object {
	return newObjectWithState(cx.shapeFor(proto), classIterator, state)
}

// NewMapObject returns a Map object with the prototype proto whose [[MapData]] is data.
func (r *realmPart) NewMapObject(proto *Object, data any) *Object {
	return newObjectWithState(r.shapeFor(proto), classMap, data)
}

// MapData returns the [[MapData]] of the Map object v, or nil when v is not a Map object.
func MapData(v Value) any {
	if v.IsObject() && v.o.class == classMap {
		return v.o.extState()
	}
	return nil
}

// NewSetObject returns a Set object with the prototype proto whose [[SetData]] is data.
func (r *realmPart) NewSetObject(proto *Object, data any) *Object {
	return newObjectWithState(r.shapeFor(proto), classSet, data)
}

// SetData returns the [[SetData]] of the Set object v, or nil when v is not a Set object.
func SetData(v Value) any {
	if v.IsObject() && v.o.class == classSet {
		return v.o.extState()
	}
	return nil
}

// NewTemporalObject returns an object of one of the Temporal types with the prototype proto,
// whose internal slots are data.
func (cx *ExecutionContext) NewTemporalObject(proto *Object, data any) *Object {
	return newObjectWithState(cx.shapeFor(proto), classTemporal, data)
}

// TemporalData returns the internal slots of the Temporal object v, or nil when v is not one.
func TemporalData(v Value) any {
	if v.IsObject() && v.o.class == classTemporal {
		return v.o.extState()
	}
	return nil
}

// NewIntlObject returns an object of one of the Intl types with the prototype proto, whose
// internal slots are data.
func (cx *ExecutionContext) NewIntlObject(proto *Object, data any) *Object {
	return newObjectWithState(cx.shapeFor(proto), classIntl, data)
}

// IntlData returns the internal slots of the Intl object v, or nil when v is not one.
func IntlData(v Value) any {
	if v.IsObject() && v.o.class == classIntl {
		return v.o.extState()
	}
	return nil
}

// dateObject keeps a Date object and its [[DateValue]] in one allocation. The internal state of
// the object points to tv, so that setting the date value allocates nothing.
type dateObject struct {
	propertyObject
	tv float64
}

// NewDateObject returns a Date object with the prototype proto whose [[DateValue]] is tv.
func (cx *ExecutionContext) NewDateObject(proto *Object, tv float64) *Object {
	s := cx.shapeFor(proto)
	d := &dateObject{tv: tv}
	return d.initWithState(s, classDate, &d.tv)
}

// ThisTimeValue implements thisTimeValue. It reports false when the specification throws.
func ThisTimeValue(v Value) (float64, bool) {
	if v.IsObject() && v.o.class == classDate {
		return *v.o.extState().(*float64), true
	}
	return 0, false
}

// SetDateValue sets the [[DateValue]] of the Date object v to tv.
func SetDateValue(v Value, tv float64) {
	*v.o.extState().(*float64) = tv
}

// IteratorState returns the state of the built-in iterator object v,
// or nil when v is not such an object.
func IteratorState(v Value) any {
	if v.o != nil && v.o.class == classIterator {
		return v.o.extState()
	}
	return nil
}

// IteratorStateAndContext returns the state and creation context of a built-in iterator.
// For other values it returns two nil values.
func IteratorStateAndContext(v Value) (any, *ExecutionContext) {
	if v.IsObject() && v.o.class == classIterator {
		return v.o.extState(), v.o.shape.realm.executionContext()
	}
	return nil, nil
}

// ThisNumberValue implements ThisNumberValue. It reports false when the specification throws.
func ThisNumberValue(v Value) (float64, bool) {
	if v.o == numberObject {
		return v.n, true
	}
	if v.IsObject() && v.o.class == classNumber {
		return v.o.extState().(float64), true
	}
	return 0, false
}

// ThisBooleanValue implements ThisBooleanValue. It reports false when the specification throws.
func ThisBooleanValue(v Value) (bool, bool) {
	if v.IsBool() {
		return v.o == trueObject, true
	}
	if v.IsObject() && v.o.class == classBoolean {
		return v.o.extState().(bool), true
	}
	return false, false
}

// ThisStringValue implements ThisStringValue. It reports false when the specification throws.
func ThisStringValue(v Value) (Value, bool) {
	if v.IsString() {
		return v, true
	}
	if v.IsObject() && v.o.class == classString {
		return ObjectValue(v.o.extState().(*Object)), true
	}
	return Undefined, false
}

// ThisBigIntValue implements ThisBigIntValue. It reports false when the specification throws.
func ThisBigIntValue(v Value) (Value, bool) {
	if v.IsBigInt() {
		return v, true
	}
	if v.IsObject() && v.o.class == classBigInt {
		return v.o.extState().(Value), true
	}
	return Undefined, false
}

// ThisSymbolValue implements ThisSymbolValue. It reports false when the specification throws.
func ThisSymbolValue(v Value) (*Object, bool) {
	if v.IsSymbol() {
		return v.o, true
	}
	if v.IsObject() && v.o.class == classSymbol {
		return v.o.extState().(*Object), true
	}
	return nil, false
}

// SymbolDescription returns the description of the symbol sym and whether it has one.
func SymbolDescription(sym *Object) (wtf8.String, bool) {
	return sym.str, sym.extState() != nil
}

// BuiltinTag returns the name that Object.prototype.toString uses for o
// when o has no Symbol.toStringTag property.
func (cx *ExecutionContext) BuiltinTag(o *Object) wtf8.String {
	switch {
	case cx.IsArray(Value{o: o}):
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

// objectTagStringsLimit bounds the number of tags whose strings a realm keeps.
const objectTagStringsLimit = 64

// ObjectTagString returns the string "[object tag]". It throws a RangeError when that is too long.
func (cx *ExecutionContext) ObjectTagString(tag wtf8.String) Value {
	r := cx.realm
	if v, ok := r.objectTagStrings[tag]; ok {
		return v
	}
	if len(tag)+9 > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	v := cx.NewString("[object " + tag + "]")
	if len(r.objectTagStrings) < objectTagStringsLimit {
		if r.objectTagStrings == nil {
			r.objectTagStrings = map[wtf8.String]Value{}
		}
		r.objectTagStrings[tag] = v
	}
	return v
}

// IsRegExpObject reports whether o is a RegExp object.
func (o *Object) IsRegExpObject() bool {
	return o.class == classRegExp
}

// NewRawJSONObject returns an object without any property or prototype
// that has the internal slot [[IsRawJSON]].
func (cx *ExecutionContext) NewRawJSONObject() *Object {
	return newObjectWithState(cx.shapeFor(nil), classRawJSON, nil)
}

// IsRawJSON reports whether o has the internal slot [[IsRawJSON]].
func (o *Object) IsRawJSON() bool {
	return o.class == classRawJSON
}

// NewDisposableStackObject returns a DisposableStack object, or an AsyncDisposableStack object
// when async is set, with the prototype proto. state is what [DisposableStackState] returns for it.
func (cx *ExecutionContext) NewDisposableStackObject(proto *Object, async bool, state any) *Object {
	c := classDisposableStack
	if async {
		c = classAsyncDisposableStack
	}
	return newObjectWithState(cx.shapeFor(proto), c, state)
}

// DisposableStackState returns the state of v when it is a DisposableStack object, or
// an AsyncDisposableStack object when async is set, and nil otherwise.
func DisposableStackState(v Value, async bool) any {
	c := classDisposableStack
	if async {
		c = classAsyncDisposableStack
	}
	if v.IsObject() && v.o.class == c {
		return v.o.extState()
	}
	return nil
}
