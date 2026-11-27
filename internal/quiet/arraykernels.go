// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func ArrayIsArrayKernel(q Context, this, v storage.Value) (storage.Value, bool) {
	is, ok := q.IsArrayPlain(v)
	return storage.Bool(is), ok
}

func ArrayPopKernel(q Context, this storage.Value) (storage.Value, bool) {
	if !this.IsObject() {
		return storage.Undefined, false
	}
	return this.Object().RemoveLastElementInPlace()
}

func ArrayIndexOfKernel(q Context, this, target storage.Value) (storage.Value, bool) {
	found, ok := q.SearchArray(this, target, false)
	return storage.Number(float64(found)), ok
}

func ArrayIncludesKernel(q Context, this, target storage.Value) (storage.Value, bool) {
	found, ok := q.SearchArray(this, target, true)
	return storage.Bool(found >= 0), ok
}
