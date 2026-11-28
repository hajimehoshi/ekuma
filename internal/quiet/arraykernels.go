// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/numconv"
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

func ArraySliceKernel0(q Context, this storage.Value) (storage.Value, bool) {
	return ArraySliceKernel2(q, this, storage.Undefined, storage.Undefined)
}

func ArraySliceKernel1(q Context, this, start storage.Value) (storage.Value, bool) {
	return ArraySliceKernel2(q, this, start, storage.Undefined)
}

func ArraySliceKernel2(q Context, this, start, end storage.Value) (storage.Value, bool) {
	if !this.IsObject() || this.O.Kind != storage.KindArray {
		return storage.Undefined, false
	}
	n := int64(this.O.Length())
	var i int64
	if !start.IsUndefined() {
		if !start.IsNumber() {
			return storage.Undefined, false
		}
		i = numconv.ClampRelativeIndex(numconv.ToIntegerOrInfinity(start.Number()), n)
	}
	j := n
	if !end.IsUndefined() {
		if !end.IsNumber() {
			return storage.Undefined, false
		}
		j = numconv.ClampRelativeIndex(numconv.ToIntegerOrInfinity(end.Number()), n)
	}
	if j-i > storage.PollInterval {
		return storage.Undefined, false
	}
	a, ok := q.CopyElementRange(this.O, i, j)
	return storage.ObjectValue(a), ok
}
