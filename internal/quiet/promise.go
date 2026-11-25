// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func (q Context) ResolutionThen(p *storage.Object, v storage.Value) (storage.Value, bool) {
	if v.O == p {
		// The self-resolution TypeError records the frame.
		return storage.Undefined, false
	}
	return q.PromiseThen(v)
}

// PromiseThen returns the then property used by promise resolution, or undefined for a primitive.
func (q Context) PromiseThen(v storage.Value) (storage.Value, bool) {
	if !v.IsObject() {
		return storage.Undefined, true
	}
	return q.GetVWithoutCall(v, q.Realm.Key(storage.KeyThen))
}
