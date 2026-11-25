// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package quiet holds the operations that run no script or host code, for the engine and for the
// built-in objects, and the fast forms of built-in functions made of them. It must not depend on
// the engine.
package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Context binds operations that run no script or host code to one realm of an agent.
// A shortcut must preserve the ordinary result and effects or decline before changing observable state.
type Context struct {
	Realm *storage.RealmPart
}

// ContextOf returns the quiet context of the realm that embeds r.
func ContextOf(r *storage.RealmPart) Context {
	return Context{
		Realm: r,
	}
}

// NewArray is [storage.RealmPart.NewArray].
func (q Context) NewArray(elems []storage.Value) *storage.Object {
	return q.Realm.NewArray(elems)
}

// NewArrayPair is [storage.RealmPart.NewArrayPair].
func (q Context) NewArrayPair(a, b storage.Value) *storage.Object {
	return q.Realm.NewArrayPair(a, b)
}

// NewString is [storage.RealmPart.NewString].
func (q Context) NewString(s wtf8.String) storage.Value {
	return q.Realm.NewString(s)
}

// UnitString is [storage.RealmPart.UnitString].
func (q Context) UnitString(u uint16) storage.Value {
	return q.Realm.UnitString(u)
}

// Substring is [storage.RealmPart.Substring].
func (q Context) Substring(s storage.Value, start, end int) storage.Value {
	return q.Realm.Substring(s, start, end)
}

// CreationContext returns the quiet context of the creation realm of o.
func CreationContext(o *storage.Object) Context {
	return Context{
		Realm: o.CreationRealm(),
	}
}

// Poll is [storage.Poller.Poll].
func (q Context) Poll() {
	q.Realm.Poll()
}

// Intrinsic is [storage.RealmPart.Intrinsic].
func (q Context) Intrinsic(id storage.Intrinsic) *storage.Object {
	return q.Realm.Intrinsic(id)
}

// Key is [storage.Space.Key].
func (q Context) Key(k storage.CommonKey) *storage.Object {
	return q.Realm.Key(k)
}

// WellKnownSymbol is [storage.Space.WellKnownSymbol].
func (q Context) WellKnownSymbol(s storage.WellKnownSymbol) *storage.Object {
	return q.Realm.WellKnownSymbol(s)
}

// NewMapObject is [storage.RealmPart.NewMapObject].
func (q Context) NewMapObject(proto *storage.Object, data any) *storage.Object {
	return q.Realm.NewMapObject(proto, data)
}

// NewSetObject is [storage.RealmPart.NewSetObject].
func (q Context) NewSetObject(proto *storage.Object, data any) *storage.Object {
	return q.Realm.NewSetObject(proto, data)
}

// HasPropertyPlain is HasProperty of o and the named key when finding out runs no code. ok is
// false otherwise.
func (q Context) HasPropertyPlain(o, key *storage.Object) (has, ok bool) {
	switch q.Realm.Lookup(o, key).Kind {
	case storage.LookupSlot, storage.LookupLength:
		return true, true
	case storage.LookupAbsent, storage.LookupNumericMiss:
		return false, true
	}
	return false, false
}

// LookupNamed finds the named property key on o and its prototype chain, as far as that runs no
// code.
func (q Context) LookupNamed(o, key *storage.Object) storage.NamedLookup {
	return q.Realm.Lookup(o, key)
}

// Delegates reports whether looking up a property that is not found on o and its prototype chain
// can run code.
func (q Context) Delegates(o *storage.Object) bool {
	for h := o; h != nil; h = h.Shape.Proto {
		if h.Shape.Family == storage.DelegatedRoot {
			return true
		}
	}
	return false
}

// GetVWithoutCall is GetV(v, key) when that runs no code and does not throw. ok is false
// otherwise.
func (q Context) GetVWithoutCall(v storage.Value, key *storage.Object) (val storage.Value, ok bool) {
	return q.Realm.GetVWithoutCall(v, key, nil)
}

// HasOwnMethod reports whether method is the value of the own data property key of o. It can
// report false for an object with internal methods of its own.
func (q Context) HasOwnMethod(o, key, method *storage.Object) bool {
	return o.HasOwnMethod(key, method)
}

// InheritsMethod reports whether o has no own property key, its prototype is proto, and method
// is the value of the own data property key of proto. It can report false for an object with
// internal methods of its own.
func (q Context) InheritsMethod(o, proto, key, method *storage.Object) bool {
	return o.InheritsMethod(proto, key, method)
}

// AssignDataProperties copies the own enumerable properties of from to the empty object to as
// Object.assign does, when that is known to run no code, and reports whether it did.
func (q Context) AssignDataProperties(to, from *storage.Object) bool {
	return q.Realm.AssignDataProperties(to, from)
}

// NumberToString returns Number::toString of n as a string value.
func (q Context) NumberToString(n float64) storage.Value {
	return q.Realm.NumberToString(n)
}
