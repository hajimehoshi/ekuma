// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the operations on the internal slots of the built-in objects.

// NewNumberObject returns a Number object for n with the prototype proto.
func (cx *ExecutionContext) NewNumberObject(n float64, proto *Object) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classNumber, n)
}

// NewBooleanObject returns a Boolean object for b with the prototype proto.
func (cx *ExecutionContext) NewBooleanObject(b bool, proto *Object) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classBoolean, b)
}

// NewErrorObject returns an Error object with prototype proto and no own properties.
func (cx *ExecutionContext) NewErrorObject(proto *Object) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classError, nil)
}

// NewGoError returns a new Error object whose message is the text of err and that
// [GoError] maps back to err.
func (cx *ExecutionContext) NewGoError(err error) Value {
	return cx.newGoError(err.Error(), err)
}

// newGoError returns a new Error object whose message is msg and that [GoError] maps to err.
func (cx *ExecutionContext) newGoError(msg string, err error) Value {
	e := cx.newError(ErrorKindError, msg)
	e.ExtState().(*errorData).GoErr = err
	return Value{O: e}
}

// NewIteratorObject returns a built-in iterator object with the prototype proto.
// state is what [IteratorState] returns for it.
func (cx *ExecutionContext) NewIteratorObject(proto *Object, state any) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classIterator, state)
}

// NewTemporalObject returns an object of one of the Temporal types with the prototype proto,
// whose internal slots are data.
func (cx *ExecutionContext) NewTemporalObject(proto *Object, data any) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classTemporal, data)
}

// NewIntlObject returns an object of one of the Intl types with the prototype proto, whose
// internal slots are data.
func (cx *ExecutionContext) NewIntlObject(proto *Object, data any) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classIntl, data)
}

// dateObject keeps a Date object and its [[DateValue]] in one allocation. The internal state of
// the object points to tv, so that setting the date value allocates nothing.
type dateObject struct {
	propertyObject
	tv float64
}

// NewDateObject returns a Date object with the prototype proto whose [[DateValue]] is tv.
func (cx *ExecutionContext) NewDateObject(proto *Object, tv float64) *Object {
	s := cx.ShapeFor(proto)
	d := &dateObject{tv: tv}
	return d.InitWithState(s, classDate, &d.tv)
}

// IteratorStateAndContext returns the state and creation context of a built-in iterator.
// For other values it returns two nil values.
func IteratorStateAndContext(v Value) (any, *ExecutionContext) {
	if v.IsObject() && v.O.Class == classIterator {
		return v.O.ExtState(), executionContextOf(v.O.Shape.Realm)
	}
	return nil, nil
}

// BuiltinTag returns the name that Object.prototype.toString uses for o
// when o has no Symbol.toStringTag property.
func (cx *ExecutionContext) BuiltinTag(o *Object) wtf8.String {
	switch {
	case cx.IsArray(Value{O: o}):
		return "Array"
	case o.Fn != nil:
		return "Function"
	}
	switch o.Class {
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
	if v, ok := r.ObjectTagStrings[tag]; ok {
		return v
	}
	if len(tag)+9 > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	v := cx.NewString("[object " + tag + "]")
	if len(r.ObjectTagStrings) < objectTagStringsLimit {
		if r.ObjectTagStrings == nil {
			r.ObjectTagStrings = map[wtf8.String]Value{}
		}
		r.ObjectTagStrings[tag] = v
	}
	return v
}

// NewRawJSONObject returns an object without any property or prototype
// that has the internal slot [[IsRawJSON]].
func (cx *ExecutionContext) NewRawJSONObject() *Object {
	return storage.NewObjectWithState(cx.ShapeFor(nil), classRawJSON, nil)
}

// NewDisposableStackObject returns a DisposableStack object, or an AsyncDisposableStack object
// when async is set, with the prototype proto. state is what [DisposableStackState] returns for it.
func (cx *ExecutionContext) NewDisposableStackObject(proto *Object, async bool, state any) *Object {
	c := classDisposableStack
	if async {
		c = classAsyncDisposableStack
	}
	return storage.NewObjectWithState(cx.ShapeFor(proto), c, state)
}
