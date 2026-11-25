// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

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

// HasOwnPropertyPlain is HasOwnProperty of o and the key k when finding out neither throws nor
// runs code, and takes a short time. It reports false as ok otherwise.
func (q Context) HasOwnPropertyPlain(o *storage.Object, k storage.Value) (has, ok bool) {
	return q.Realm.HasOwnPropertyPlain(o, k)
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
