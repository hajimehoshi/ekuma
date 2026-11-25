// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// This file holds the operations on the internal slots of the built-in objects.

// NewNumberObject returns a Number object for n with the prototype proto.
func (cx *ExecutionContext) NewNumberObject(n float64, proto *storage.Object) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassNumber, n)
}

// NewBooleanObject returns a Boolean object for b with the prototype proto.
func (cx *ExecutionContext) NewBooleanObject(b bool, proto *storage.Object) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassBoolean, b)
}

// NewErrorObject returns an Error object with prototype proto and no own properties.
func (cx *ExecutionContext) NewErrorObject(proto *storage.Object) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassError, nil)
}

// NewGoError returns a new Error object whose message is the text of err and that
// [GoError] maps back to err.
func (cx *ExecutionContext) NewGoError(err error) storage.Value {
	return cx.newGoError(err.Error(), err)
}

// newGoError returns a new Error object whose message is msg and that [GoError] maps to err.
func (cx *ExecutionContext) newGoError(msg string, err error) storage.Value {
	e := cx.newError(ErrorKindError, msg)
	e.ExtState().(*storage.ErrorData).GoErr = err
	return storage.Value{O: e}
}

// NewIteratorObject returns a built-in iterator object with the prototype proto.
// state is what [IteratorState] returns for it.
func (cx *ExecutionContext) NewIteratorObject(proto *storage.Object, state any) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassIterator, state)
}

// NewTemporalObject returns an object of one of the Temporal types with the prototype proto,
// whose internal slots are data.
func (cx *ExecutionContext) NewTemporalObject(proto *storage.Object, data any) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassTemporal, data)
}

// NewIntlObject returns an object of one of the Intl types with the prototype proto, whose
// internal slots are data.
func (cx *ExecutionContext) NewIntlObject(proto *storage.Object, data any) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassIntl, data)
}

// dateObject keeps a Date object and its [[DateValue]] in one allocation. The internal state of
// the object points to tv, so that setting the date value allocates nothing.
type dateObject struct {
	propertyObject
	tv float64
}

// NewDateObject returns a Date object with the prototype proto whose [[DateValue]] is tv.
func (cx *ExecutionContext) NewDateObject(proto *storage.Object, tv float64) *storage.Object {
	s := cx.ShapeFor(proto)
	d := &dateObject{tv: tv}
	return d.InitWithState(s, storage.ClassDate, &d.tv)
}

// IteratorStateAndContext returns the state and creation context of a built-in iterator.
// For other values it returns two nil values.
func IteratorStateAndContext(v storage.Value) (any, *ExecutionContext) {
	if v.IsObject() && v.O.Class == storage.ClassIterator {
		return v.O.ExtState(), executionContextOf(v.O.Shape.Realm)
	}
	return nil, nil
}

// BuiltinTag returns the name that Object.prototype.toString uses for o
// when o has no Symbol.toStringTag property.
func (cx *ExecutionContext) BuiltinTag(o *storage.Object) wtf8.String {
	switch {
	case cx.IsArray(storage.Value{O: o}):
		return "Array"
	case o.Fn != nil:
		return "Function"
	}
	switch o.Class {
	case storage.ClassError:
		return "Error"
	case storage.ClassBoolean:
		return "Boolean"
	case storage.ClassNumber:
		return "Number"
	case storage.ClassString:
		return "String"
	case storage.ClassDate:
		return "Date"
	case storage.ClassRegExp:
		return "RegExp"
	case storage.ClassArguments:
		return "Arguments"
	}
	return "Object"
}

// objectTagStringsLimit bounds the number of tags whose strings a realm keeps.
const objectTagStringsLimit = 64

// ObjectTagString returns the string "[object tag]". It throws a RangeError when that is too long.
func (cx *ExecutionContext) ObjectTagString(tag wtf8.String) storage.Value {
	r := cx.realm
	if v, ok := r.ObjectTagStrings[tag]; ok {
		return v
	}
	if len(tag)+9 > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	v := cx.NewString("[object " + tag + "]")
	if len(r.ObjectTagStrings) < objectTagStringsLimit {
		if r.ObjectTagStrings == nil {
			r.ObjectTagStrings = map[wtf8.String]storage.Value{}
		}
		r.ObjectTagStrings[tag] = v
	}
	return v
}

// NewRawJSONObject returns an object without any property or prototype
// that has the internal slot [[IsRawJSON]].
func (cx *ExecutionContext) NewRawJSONObject() *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(nil), storage.ClassRawJSON, nil)
}

// NewDisposableStackObject returns a DisposableStack object, or an AsyncDisposableStack object
// when async is set, with the prototype proto. state is what [DisposableStackState] returns for it.
func (cx *ExecutionContext) NewDisposableStackObject(proto *storage.Object, async bool, state any) *storage.Object {
	c := storage.ClassDisposableStack
	if async {
		c = storage.ClassAsyncDisposableStack
	}
	return storage.NewObjectWithState(cx.ShapeFor(proto), c, state)
}
