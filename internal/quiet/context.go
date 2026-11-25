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

// NumberToString returns Number::toString of n as a string value.
func (q Context) NumberToString(n float64) storage.Value {
	return q.Realm.NumberToString(n)
}
