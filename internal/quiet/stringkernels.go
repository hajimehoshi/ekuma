// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func StringConstructorKernel(q Context, this, x storage.Value) (storage.Value, bool) {
	if x.IsString() {
		return x, true
	}
	if x.IsNumber() {
		return q.NumberToString(x.Number()), true
	}
	return storage.Undefined, false
}

func StringFromCharCodeKernel(q Context, this, x storage.Value) (storage.Value, bool) {
	if !x.IsNumber() {
		return storage.Undefined, false
	}
	return q.UnitString(uint16(numconv.ToUint32(x.Number()))), true
}

func StringCharAtKernel(q Context, this, x storage.Value) (storage.Value, bool) {
	if !this.IsString() || !x.IsNumber() {
		return storage.Undefined, false
	}
	return UnitStringAt(q, this, numconv.ToIntegerOrInfinity(x.Number())), true
}

func StringCharCodeAtKernel(q Context, this, x storage.Value) (storage.Value, bool) {
	if !this.IsString() || !x.IsNumber() {
		return storage.Undefined, false
	}
	return CodeUnitAt(this, numconv.ToIntegerOrInfinity(x.Number())), true
}

func StringIndexOfKernel(q Context, this, sub storage.Value) (storage.Value, bool) {
	if !this.IsString() || !sub.IsString() {
		return storage.Undefined, false
	}
	return storage.Number(float64(this.UnitIndexOf(sub, 0, q.Poll))), true
}

func StringIncludesKernel(q Context, this, sub storage.Value) (storage.Value, bool) {
	if !this.IsString() || !sub.IsString() {
		return storage.Undefined, false
	}
	return storage.Bool(this.UnitIndexOf(sub, 0, q.Poll) >= 0), true
}

func StringSliceKernel1(q Context, this, start storage.Value) (storage.Value, bool) {
	if !this.IsString() || !start.IsNumber() {
		return storage.Undefined, false
	}
	return SliceString(q, this, start.Number(), math.Inf(1)), true
}

func StringSliceKernel2(q Context, this, start, end storage.Value) (storage.Value, bool) {
	if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
		return storage.Undefined, false
	}
	return SliceString(q, this, start.Number(), end.Number()), true
}

func StringSubstringKernel1(q Context, this, start storage.Value) (storage.Value, bool) {
	if !this.IsString() || !start.IsNumber() {
		return storage.Undefined, false
	}
	return SubstringOf(q, this, start.Number(), math.Inf(1)), true
}

func StringSubstringKernel2(q Context, this, start, end storage.Value) (storage.Value, bool) {
	if !this.IsString() || !start.IsNumber() || !end.IsNumber() {
		return storage.Undefined, false
	}
	return SubstringOf(q, this, start.Number(), end.Number()), true
}

// CodeUnitAt returns the code unit of the string s at the index i, or NaN when s has none there.
func CodeUnitAt(s storage.Value, i float64) storage.Value {
	if i < 0 || i >= float64(s.StringLength()) {
		return storage.Number(math.NaN())
	}
	return storage.Number(float64(s.UnitAt(int(i))))
}

// UnitStringAt returns the code unit of the string s at the index i as a string, or the empty
// string when s has none there.
func UnitStringAt(q Context, s storage.Value, i float64) storage.Value {
	if i < 0 || i >= float64(s.StringLength()) {
		return q.NewString("")
	}
	return q.UnitString(s.UnitAt(int(i)))
}

// SliceString returns the result of String.prototype.slice for the string s and the converted
// arguments start and end.
func SliceString(q Context, s storage.Value, start, end float64) storage.Value {
	n := s.StringLength()
	from, to := numconv.RelativeIndex(start, n), numconv.RelativeIndex(end, n)
	if from >= to {
		return q.NewString("")
	}
	return q.Substring(s, from, to)
}

// SubstringOf returns the result of String.prototype.substring for the string s and the converted
// arguments start and end.
func SubstringOf(q Context, s storage.Value, start, end float64) storage.Value {
	n := s.StringLength()
	from, to := numconv.RelativeIndex(max(start, 0), n), numconv.RelativeIndex(max(end, 0), n)
	if from > to {
		from, to = to, from
	}
	return q.Substring(s, from, to)
}
