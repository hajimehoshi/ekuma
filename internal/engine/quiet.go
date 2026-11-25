// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// QuietContext binds operations that run no script or host code to one realm of an agent.
// A shortcut must preserve the ordinary result and effects or decline before changing observable state.
type QuietContext struct {
	realm *realmPart
}

// Quiet returns the quiet context of the realm of cx.
func (cx *ExecutionContext) Quiet() QuietContext {
	return QuietContext{
		realm: cx.realmPart,
	}
}

// quietContextOf returns the quiet context of the realm that embeds r.
func quietContextOf(r *realmPart) QuietContext {
	return QuietContext{
		realm: r,
	}
}

// NewArray is [ExecutionContext.NewArray].
func (q QuietContext) NewArray(elems []Value) *Object {
	return q.realm.NewArray(elems)
}

// NewArrayPair is [ExecutionContext.NewArrayPair].
func (q QuietContext) NewArrayPair(a, b Value) *Object {
	return q.realm.NewArrayPair(a, b)
}

// NewString is [ExecutionContext.NewString].
func (q QuietContext) NewString(s wtf8.String) Value {
	return q.realm.NewString(s)
}

// UnitString is [ExecutionContext.UnitString].
func (q QuietContext) UnitString(u uint16) Value {
	return q.realm.UnitString(u)
}

// Substring is [ExecutionContext.Substring].
func (q QuietContext) Substring(s Value, start, end int) Value {
	return q.realm.Substring(s, start, end)
}

// CreationQuietContext is the quiet context of [CreationContext].
func CreationQuietContext(o *Object) QuietContext {
	return QuietContext{
		realm: o.CreationRealm(),
	}
}

// Poll is [ExecutionContext.Poll].
func (q QuietContext) Poll() {
	q.realm.Poll()
}

// Intrinsic is [ExecutionContext.Intrinsic].
func (q QuietContext) Intrinsic(id Intrinsic) *Object {
	return q.realm.Intrinsic(id)
}

// Key is [ExecutionContext.Key].
func (q QuietContext) Key(k CommonKey) *Object {
	return q.realm.Key(k)
}

// WellKnownSymbol is [ExecutionContext.WellKnownSymbol].
func (q QuietContext) WellKnownSymbol(s WellKnownSymbol) *Object {
	return q.realm.WellKnownSymbol(s)
}

// NewMapObject is [ExecutionContext.NewMapObject].
func (q QuietContext) NewMapObject(proto *Object, data any) *Object {
	return q.realm.NewMapObject(proto, data)
}

// NewSetObject is [ExecutionContext.NewSetObject].
func (q QuietContext) NewSetObject(proto *Object, data any) *Object {
	return q.realm.NewSetObject(proto, data)
}

// hasPropertyPlain is HasProperty of o and the named key when finding out runs no code. ok is
// false otherwise.
func (q QuietContext) hasPropertyPlain(o, key *Object) (has, ok bool) {
	switch q.realm.Lookup(o, key).Kind {
	case lookupSlot, lookupLength:
		return true, true
	case lookupAbsent, lookupNumericMiss:
		return false, true
	}
	return false, false
}

// lookupNamed finds the named property key on o and its prototype chain, as far as that runs no
// code.
func (q QuietContext) lookupNamed(o, key *Object) namedLookup {
	return q.realm.Lookup(o, key)
}

// delegates reports whether looking up a property that is not found on o and its prototype chain
// can run code.
func (q QuietContext) delegates(o *Object) bool {
	for h := o; h != nil; h = h.Shape.Proto {
		if h.Shape.Family == delegatedRoot {
			return true
		}
	}
	return false
}

// GetVWithoutCall is GetV(v, key) when that runs no code and does not throw. ok is false
// otherwise.
func (q QuietContext) GetVWithoutCall(v Value, key *Object) (val Value, ok bool) {
	return q.realm.GetVWithoutCall(v, key, nil)
}

// quietJobs provides the promise operations that run no script or host code. Their context stays
// behind it, as they need the jobs of the agent.
type quietJobs struct {
	cx *ExecutionContext
}

// quietJobs returns the quiet promise operations of cx.
func (cx *ExecutionContext) quietJobs() quietJobs {
	return quietJobs{
		cx: cx,
	}
}

// HasOwnMethod reports whether method is the value of the own data property key of o. It can
// report false for an object with internal methods of its own.
func (q QuietContext) HasOwnMethod(o, key, method *Object) bool {
	return o.HasOwnMethod(key, method)
}

// InheritsMethod reports whether o has no own property key, its prototype is proto, and method
// is the value of the own data property key of proto. It can report false for an object with
// internal methods of its own.
func (q QuietContext) InheritsMethod(o, proto, key, method *Object) bool {
	return o.InheritsMethod(proto, key, method)
}

// AssignDataProperties copies the own enumerable properties of from to the empty object to as
// Object.assign does, when that is known to run no code, and reports whether it did.
func (q QuietContext) AssignDataProperties(to, from *Object) bool {
	return q.realm.AssignDataProperties(to, from)
}

// NumberToString returns Number::toString of n as a string value.
func (q QuietContext) NumberToString(n float64) Value {
	return q.realm.NumberToString(n)
}
