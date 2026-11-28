// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func NumberToStringKernel0(q Context, this storage.Value) (storage.Value, bool) {
	if !this.IsNumber() {
		return storage.Value{}, false
	}
	return q.NumberToString(this.Number()), true
}

func NumberToStringKernel1(q Context, this, arg storage.Value) (storage.Value, bool) {
	if arg.IsUndefined() {
		return NumberToStringKernel0(q, this)
	}
	if !this.IsNumber() || !arg.IsNumber() {
		return storage.Value{}, false
	}
	radix := numconv.ToIntegerOrInfinity(arg.Number())
	if radix < 2 || radix > 36 {
		return storage.Value{}, false
	}
	if radix == 10 {
		return q.NumberToString(this.Number()), true
	}
	return q.NewString(numconv.NumberToRadixString(this.Number(), int(radix))), true
}

func NumberToFixedKernel0(q Context, this storage.Value) (storage.Value, bool) {
	if !this.IsNumber() {
		return storage.Value{}, false
	}
	return numberToFixed(q, this.Number(), 0), true
}

func NumberToFixedKernel1(q Context, this, arg storage.Value) (storage.Value, bool) {
	if !this.IsNumber() {
		return storage.Value{}, false
	}
	var digits int
	if arg.IsNumber() {
		n := arg.Number()
		// Conversion to int truncates toward zero. NaN uses the default precision.
		if -1 < n && n < 101 {
			digits = int(n)
		} else if n == n {
			return storage.Value{}, false
		}
	} else if !arg.IsUndefined() {
		return storage.Value{}, false
	}
	return numberToFixed(q, this.Number(), digits), true
}

func numberToFixed(q Context, n float64, digits int) storage.Value {
	if math.IsNaN(n) || math.Abs(n) >= 1e21 {
		return q.NumberToString(n)
	}
	var buf [numconv.NumberFormatBufSize]byte
	return q.NewASCIIStringBytes(numconv.AppendFixed(buf[:0], n, digits))
}

func NumberToPrecisionKernel1(q Context, this, arg storage.Value) (storage.Value, bool) {
	if arg.IsUndefined() {
		return NumberToStringKernel0(q, this)
	}
	if !this.IsNumber() || !arg.IsNumber() {
		return storage.Value{}, false
	}
	n := this.Number()
	if !(math.Abs(n) <= math.MaxFloat64) {
		return q.NumberToString(n), true
	}
	if p := arg.Number(); 1 <= p && p < 101 {
		var buf [numconv.NumberFormatBufSize]byte
		return q.NewASCIIStringBytes(numconv.AppendPrecision(buf[:0], n, int(p))), true
	}
	return storage.Value{}, false
}

func NumberToExponentialKernel0(q Context, this storage.Value) (storage.Value, bool) {
	if !this.IsNumber() {
		return storage.Value{}, false
	}
	n := this.Number()
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return q.NumberToString(n), true
	}
	var buf [numconv.NumberFormatBufSize]byte
	return q.NewASCIIStringBytes(numconv.AppendExponentialAuto(buf[:0], n)), true
}

func NumberToExponentialKernel1(q Context, this, arg storage.Value) (storage.Value, bool) {
	if arg.IsUndefined() {
		return NumberToExponentialKernel0(q, this)
	}
	if !this.IsNumber() || !arg.IsNumber() {
		return storage.Value{}, false
	}
	n := this.Number()
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return q.NumberToString(n), true
	}
	digits := numconv.ToIntegerOrInfinity(arg.Number())
	if digits < 0 || digits > 100 {
		return storage.Value{}, false
	}
	var buf [numconv.NumberFormatBufSize]byte
	return q.NewASCIIStringBytes(numconv.AppendExponential(buf[:0], n, int(digits))), true
}
