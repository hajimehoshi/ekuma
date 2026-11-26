// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"bytes"
	"cmp"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/element"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// typedArrayNames are the names of the typed array constructors, by element type.
var typedArrayNames = [element.TypeCount]string{
	"Int8Array", "Uint8Array", "Uint8ClampedArray", "Int16Array", "Uint16Array", "Int32Array",
	"Uint32Array", "Float16Array", "Float32Array", "Float64Array", "BigInt64Array", "BigUint64Array",
}

// validateTypedArray implements ValidateTypedArray with the access mode read: it returns this
// as a typed array and its length.
func validateTypedArray(cx *engine.ExecutionContext, this storage.Value, what string) (*storage.Object, *storage.TypedArray, int) {
	return validateTypedArrayAccess(cx, this, false, what)
}

// validateWritableTypedArray implements ValidateTypedArray with the access mode write.
func validateWritableTypedArray(cx *engine.ExecutionContext, this storage.Value, what string) (*storage.Object, *storage.TypedArray, int) {
	return validateTypedArrayAccess(cx, this, true, what)
}

// validateTypedArrayAccess implements ValidateTypedArray with the access mode write when write is set.
func validateTypedArrayAccess(cx *engine.ExecutionContext, this storage.Value, write bool, what string) (*storage.Object, *storage.TypedArray, int) {
	t := storage.TypedArrayData(this)
	if t == nil {
		cx.ThrowTypeError(what + ": this is not a typed array")
	}
	if write && t.ArrayBuffer().IsImmutable() {
		cx.ThrowTypeError(what + ": the typed array is backed by an immutable ArrayBuffer")
	}
	n, ok := t.Length()
	if !ok {
		cx.ThrowTypeError(what + ": the typed array is detached or out of bounds")
	}
	return this.Object(), t, n
}

// storeAt implements TypedArraySetElement for the typed array o, whose state is t, and the index i.
func storeAt(cx *engine.ExecutionContext, o *storage.Object, t *storage.TypedArray, i int, v storage.Value) {
	typ := t.ElementType()
	if v.IsNumber() && !typ.IsBigInt() {
		t.StoreElement(i, storage.ElementBits(typ, v))
		return
	}
	cx.Set(o, storage.IndexKey(uint32(i)), v)
}

// The bytes of a buffer are reached with Lock and Unlock, which keep the other agents from accessing
// a shared buffer in between. The helpers below run no JavaScript. Those that take cx work in runs,
// each under its own lock, and check for an interruption between runs; the others hold one lock.

// lockedRuns calls f with the bytes of the elements of t, while t is locked, and with the bounds
// i and j of each run of at most [storage.PollInterval] indices from start up to end, the last
// run first when backward is set. It checks for an interruption between runs, and stops when f
// reports false.
func lockedRuns(cx *engine.ExecutionContext, t *storage.TypedArray, start, end int, backward bool, f func(b []byte, i, j int) bool) {
	for k := start; k < end; k += storage.PollInterval {
		if k > start {
			cx.Poll()
		}
		i, j := k, min(k+storage.PollInterval, end)
		if backward {
			i, j = start+end-j, start+end-k
		}
		if !lockedRun(t, i, j, f) {
			return
		}
	}
}

// lockedRun calls f with the bytes of the elements of t, while t is locked, and with i and j.
func lockedRun(t *storage.TypedArray, i, j int, f func(b []byte, i, j int) bool) bool {
	b := t.Lock()
	defer t.Unlock()
	return f(b, i, j)
}

// elementsOf returns the bytes of the elements of t, or nil when t is out of bounds. The bytes of
// a shared buffer are a copy, as other agents may change them.
func elementsOf(t *storage.TypedArray) []byte {
	b := t.Lock()
	defer t.Unlock()
	if t.ArrayBuffer().IsShared() {
		return slices.Clone(b)
	}
	return b
}

// writeElements copies src to the bytes of the elements of t from the element offset on, as far as
// they reach.
func writeElements(t *storage.TypedArray, offset int, src []byte) {
	b := t.Lock()
	defer t.Unlock()
	if o := offset * t.ElementType().Size(); o < len(b) {
		copy(b[o:], src)
	}
}

// copyElements copies the first n elements of src to the element offset in dst.
// The element types must match and both ranges must be in bounds.
func copyElements(dst *storage.TypedArray, offset int, src *storage.TypedArray, n int) {
	size := src.ElementType().Size()
	storage.CopyBufferBytes(dst.ArrayBuffer(), dst.ByteOffset()+offset*size, src.ArrayBuffer(), src.ByteOffset(), n*size)
}

// storeConverted stores the n elements of the type from in src as elements of t from the element
// offset on. t must have enough elements.
func storeConverted(cx *engine.ExecutionContext, t *storage.TypedArray, offset int, src []byte, from element.Type, n int) {
	to := t.ElementType()
	ts, fs := to.Size(), from.Size()
	lockedRuns(cx, t, 0, n, false, func(b []byte, i, j int) bool {
		copyConverted(b[(offset+i)*ts:], to, src[i*fs:], from, j-i)
		return true
	})
}

// reverseFirst reverses the first n elements of t.
func reverseFirst(cx *engine.ExecutionContext, t *storage.TypedArray, n int) {
	size := t.ElementType().Size()
	lockedRuns(cx, t, 0, n/2, false, func(b []byte, i, j int) bool {
		m := min(n, len(b)/size)
		var tmp [8]byte
		for k := i; k < j && k < m-1-k; k++ {
			x, y := b[k*size:(k+1)*size], b[(m-1-k)*size:(m-k)*size]
			copy(tmp[:size], x)
			copy(x, y)
			copy(y, tmp[:size])
		}
		return true
	})
}

// sortFirst sorts the first n elements of t numerically.
func sortFirst(cx *engine.ExecutionContext, t *storage.TypedArray, n int) {
	typ := t.ElementType()
	switch typ {
	case element.BigInt64:
		vals := loadElements(cx, t, n, func(x uint64) int64 {
			return int64(x)
		})
		sortPolled(cx, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		storeElements(cx, t, vals, func(v int64) uint64 {
			return uint64(v)
		})
	case element.BigUint64:
		vals := loadElements(cx, t, n, func(x uint64) uint64 {
			return x
		})
		sortPolled(cx, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		storeElements(cx, t, vals, func(v uint64) uint64 {
			return v
		})
	default:
		vals := loadElements(cx, t, n, func(x uint64) float64 {
			return element.DecodeNumber(typ, x)
		})
		switch typ {
		case element.Float16, element.Float32, element.Float64:
			sortPolled(cx, vals, numberSortRunLength, func(s []float64) {
				slices.SortFunc(s, compareNumbers)
			}, compareNumbers)
		default:
			sortPolled(cx, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		}
		storeElements(cx, t, vals, func(v float64) uint64 {
			return storage.ElementBits(typ, storage.Number(v))
		})
	}
}

// loadElements returns the first n elements of t, which has them, decoded by decode.
func loadElements[E any](cx *engine.ExecutionContext, t *storage.TypedArray, n int, decode func(x uint64) E) []E {
	typ := t.ElementType()
	size := typ.Size()
	vals := make([]E, n)
	lockedRuns(cx, t, 0, n, false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			vals[k] = decode(element.Load(b[k*size:], typ))
		}
		return true
	})
	return vals
}

// storeElements stores vals encoded by encode as the first elements of t, which has room for them.
func storeElements[E any](cx *engine.ExecutionContext, t *storage.TypedArray, vals []E, encode func(v E) uint64) {
	typ := t.ElementType()
	size := typ.Size()
	lockedRuns(cx, t, 0, len(vals), false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			element.Store(b[k*size:], typ, encode(vals[k]))
		}
		return true
	})
}

// fillRange stores the encoding x in the elements of t from start up to end.
func fillRange(cx *engine.ExecutionContext, t *storage.TypedArray, start, end int, x uint64) {
	typ := t.ElementType()
	size := typ.Size()
	if end-start <= storage.PollInterval {
		lockedRun(t, start, end, func(b []byte, i, j int) bool {
			fillElements(b[i*size:min(j*size, len(b))], typ, x)
			return true
		})
		return
	}
	// Every run is a copy of one pattern.
	pattern := make([]byte, storage.PollInterval*size)
	fillElements(pattern, typ, x)
	lockedRuns(cx, t, start, end, false, func(b []byte, i, j int) bool {
		copy(b[i*size:min(j*size, len(b))], pattern)
		return true
	})
}

// searchRange is searchElements on the elements of t.
func searchRange(cx *engine.ExecutionContext, t *storage.TypedArray, x storage.Value, from, to int, backward, sameValueZero bool) int {
	typ := t.ElementType()
	found := -1
	start, end := from, to
	if backward {
		start, end = to, from+1
	}
	lockedRuns(cx, t, start, end, backward, func(b []byte, i, j int) bool {
		if backward {
			found = searchElements(b, typ, x, j-1, i, true, sameValueZero)
		} else {
			found = searchElements(b, typ, x, i, j, false, sameValueZero)
		}
		return found < 0
	})
	return found
}

// copyBytesWithin copies c bytes of the elements of t from the byte offset from to the byte offset to.
func copyBytesWithin(t *storage.TypedArray, to, from, c int) {
	b := t.Lock()
	defer t.Unlock()
	copy(b[to:to+c], b[from:from+c])
}

// copyBytesForward copies n bytes of the buffer b from the byte index src to the byte index dst one
// at a time from the first on.
func copyBytesForward(cx *engine.ExecutionContext, b *storage.ArrayBuffer, dst, src, n int) {
	for i := 0; i < n; i += storage.PollInterval {
		if i > 0 {
			cx.Poll()
		}
		copyBytesForwardRun(b, dst+i, src+i, min(storage.PollInterval, n-i))
	}
}

// copyBytesForwardRun is copyBytesForward under one lock.
func copyBytesForwardRun(b *storage.ArrayBuffer, dst, src, n int) {
	data := b.Lock()
	defer b.Unlock()
	for i := range n {
		data[dst+i] = data[src+i]
	}
}

// storeNumbers stores the elements of src from 0 on that are numbers that reading runs no
// JavaScript for in the elements of t from the element offset on, as far as they reach, and
// returns how many it stored.
func storeNumbers(cx *engine.ExecutionContext, t *storage.TypedArray, offset int, src *storage.Object, n int64) int64 {
	typ := t.ElementType()
	size := typ.Size()
	var stored int64
	lockedRuns(cx, t, 0, int(n), false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			e, ok := storage.Element(src, int64(k))
			if !ok || !e.IsNumber() || (offset+k+1)*size > len(b) {
				return false
			}
			element.Store(b[(offset+k)*size:], typ, storage.ElementBits(typ, e))
			stored++
		}
		return true
	})
	return stored
}

// storeNumberValues stores the leading numbers of vals in the elements of t from 0 on, as far as
// they reach, and returns how many it stored. It stores nothing in a typed array of BigInts.
// It checks for an interruption while it stores many numbers.
func storeNumberValues(cx *engine.ExecutionContext, t *storage.TypedArray, vals []storage.Value) int {
	var i int
	for {
		end := min(i+storage.PollInterval, len(vals))
		stored := storeNumberRange(t, vals, i, end)
		if stored < end || end == len(vals) {
			return stored
		}
		i = end
		cx.Poll()
	}
}

// storeNumberRange is [storeNumberValues] for the values from start up to end, whose indices in t
// are those in vals. It returns the index of the first value that it did not store.
func storeNumberRange(t *storage.TypedArray, vals []storage.Value, start, end int) int {
	typ := t.ElementType()
	if typ.IsBigInt() {
		return start
	}
	b := t.Lock()
	defer t.Unlock()
	size := typ.Size()
	end = max(start, min(end, len(b)/size))
	for i, v := range vals[start:end] {
		if !v.IsNumber() {
			return start + i
		}
		element.Store(b[(start+i)*size:], typ, storage.ElementBits(typ, v))
	}
	return end
}

// newTypedArray returns a new typed array of n elements of the type typ on a new ArrayBuffer.
func newTypedArray(cx *engine.ExecutionContext, proto *storage.Object, typ element.Type, n int64) *storage.Object {
	buf := cx.NewArrayBufferObject(cx.Intrinsic(storage.IntrinsicArrayBufferPrototype), n*int64(typ.Size()), -1, false)
	return cx.NewTypedArrayObject(proto, typ, buf, 0, int(n))
}

// createFromConstructor implements TypedArrayCreateFromConstructor. The access mode is write when write is set.
func createFromConstructor(cx *engine.ExecutionContext, c *storage.Object, args []storage.Value, write bool) (*storage.Object, *storage.TypedArray) {
	r := storage.ObjectValue(cx.Construct(c, args, c))
	o, t, n := validateTypedArrayAccess(cx, r, write, "typed array constructor")
	if len(args) == 1 && args[0].IsNumber() && float64(n) < args[0].Number() {
		cx.ThrowTypeError("typed array constructor returned a too short typed array")
	}
	return o, t
}

// typedArraySpeciesCreate implements TypedArraySpeciesCreate. The access mode is write when write is set.
func typedArraySpeciesCreate(cx *engine.ExecutionContext, exemplar *storage.Object, t *storage.TypedArray, args []storage.Value, write bool) (*storage.Object, *storage.TypedArray) {
	c := speciesConstructor(cx, exemplar, cx.Intrinsic(engine.TypedArrayConstructorIntrinsic(t.ElementType())))
	o, r := createFromConstructor(cx, c, args, write)
	if r.ElementType().IsBigInt() != t.ElementType().IsBigInt() {
		cx.ThrowTypeError("typed array species constructor returned a typed array of a different content type")
	}
	return o, r
}

// typedArrayCreateSameType implements TypedArrayCreateSameType.
func typedArrayCreateSameType(cx *engine.ExecutionContext, t *storage.TypedArray, n int) (*storage.Object, *storage.TypedArray) {
	typ := t.ElementType()
	o := newTypedArray(cx, cx.Intrinsic(engine.TypedArrayPrototypeIntrinsic(typ)), typ, int64(n))
	return o, storage.TypedArrayData(storage.ObjectValue(o))
}

// copyConverted stores the n elements of the type from in src as elements of the type to in dst.
func copyConverted(dst []byte, to element.Type, src []byte, from element.Type, n int) {
	if from == to {
		copy(dst[:n*to.Size()], src)
		return
	}
	fs, ts := from.Size(), to.Size()
	for i := range n {
		x := element.Load(src[i*fs:], from)
		element.Store(dst[i*ts:], to, element.Convert(from, to, x))
	}
}

// constructTypedArray implements the typed array constructors for the element type typ.
func constructTypedArray(cx *engine.ExecutionContext, typ element.Type, args []storage.Value) storage.Value {
	nt := cx.GetNewTarget()
	name := typedArrayNames[typ]
	if nt == nil {
		cx.ThrowTypeError("constructor " + name + " requires 'new'")
	}
	defaultProto := engine.TypedArrayPrototypeIntrinsic(typ)
	size := typ.Size()
	first := arg(args, 0)
	if !first.IsObject() {
		var n int64
		if len(args) > 0 {
			n = toIndex(cx, first)
		}
		proto := cx.GetPrototypeFromConstructor(nt, defaultProto)
		return storage.ObjectValue(newTypedArray(cx, proto, typ, n))
	}
	proto := cx.GetPrototypeFromConstructor(nt, defaultProto)
	if src := storage.TypedArrayData(first); src != nil {
		n, ok := src.Length()
		if !ok {
			cx.ThrowTypeError("cannot construct " + name + " from a detached or out of bounds typed array")
		}
		if src.ElementType().IsBigInt() != typ.IsBigInt() {
			cx.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
		}
		o := newTypedArray(cx, proto, typ, int64(n))
		t := storage.TypedArrayData(storage.ObjectValue(o))
		if src.ArrayBuffer().IsShared() && src.ElementType() == typ {
			// The shared bytes are copied directly, without the copy that elementsOf makes.
			copyElements(t, 0, src, n)
		} else {
			storeConverted(cx, t, 0, elementsOf(src), src.ElementType(), n)
		}
		return storage.ObjectValue(o)
	}
	if b := storage.ArrayBufferData(first); b != nil {
		offset := toIndex(cx, arg(args, 1))
		if offset%int64(size) != 0 {
			cx.ThrowRangeError("start offset of " + name + " should be a multiple of " + itoa(int64(size)))
		}
		fixed := !b.IsResizable()
		length := arg(args, 2)
		var newLength int64
		if !length.IsUndefined() {
			newLength = toIndex(cx, length)
		}
		if b.IsDetached() {
			cx.ThrowTypeError("cannot construct " + name + " on a detached ArrayBuffer")
		}
		bl := int64(b.ByteLength())
		if length.IsUndefined() && !fixed {
			if offset > bl {
				cx.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
			}
			return storage.ObjectValue(cx.NewTypedArrayObject(proto, typ, first.Object(), int(offset), -1))
		}
		var newByteLength int64
		if length.IsUndefined() {
			if bl%int64(size) != 0 {
				cx.ThrowRangeError("byte length of " + name + " should be a multiple of " + itoa(int64(size)))
			}
			newByteLength = bl - offset
			if newByteLength < 0 {
				cx.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
			}
		} else {
			newByteLength = newLength * int64(size)
			if offset+newByteLength > bl {
				cx.ThrowRangeError("invalid typed array length: " + itoa(newLength))
			}
		}
		return storage.ObjectValue(cx.NewTypedArrayObject(proto, typ, first.Object(), int(offset), int(newByteLength/int64(size))))
	}
	if m := cx.GetMethod(first, cx.WellKnownSymbol(storage.SymbolIterator)); m != nil {
		vals := cx.IteratorToList(first, m)
		o := newTypedArray(cx, proto, typ, int64(len(vals)))
		t := storage.TypedArrayData(storage.ObjectValue(o))
		for i := storeNumberValues(cx, t, vals); i < len(vals); i++ {
			storeAt(cx, o, t, i, vals[i])
		}
		return storage.ObjectValue(o)
	}
	src := first.Object()
	n := cx.LengthOfArrayLike(src)
	o := newTypedArray(cx, proto, typ, n)
	t := storage.TypedArrayData(storage.ObjectValue(o))
	for i := range n {
		storeAt(cx, o, t, int(i), getAt(cx, src, i))
	}
	return storage.ObjectValue(o)
}

func itoa(n int64) string {
	return string(numconv.NumberToString(float64(n)))
}

// compareNumbers orders numbers as the default comparison of typed arrays does: -0 before +0 and NaN last.
func compareNumbers(a, b float64) int {
	switch {
	case a != a:
		if b != b {
			return 0
		}
		return 1
	case b != b:
		return -1
	case a < b:
		return -1
	case a > b:
		return 1
	case a == 0:
		sa, sb := math.Signbit(a), math.Signbit(b)
		if sa && !sb {
			return -1
		}
		if !sa && sb {
			return 1
		}
	}
	return 0
}

// searchElements returns the index of the first element of the type typ in b from from up to to
// (downward from from to to when backward is set) that equals x, or -1. sameValueZero selects
// SameValueZero instead of IsStrictlyEqual.
func searchElements(b []byte, typ element.Type, x storage.Value, from, to int, backward, sameValueZero bool) int {
	if typ.IsBigInt() != x.IsBigInt() || !typ.IsBigInt() && !x.IsNumber() {
		return -1
	}
	if !typ.IsBigInt() {
		isFloat := typ == element.Float16 || typ == element.Float32 || typ == element.Float64
		// A number other than NaN, and other than a zero for a floating-point type, has at
		// most one encoding, which the elements equal to it have.
		if f := x.Number(); f == f && (f != 0 || !isFloat) {
			bits := storage.ElementBits(typ, x)
			if element.DecodeNumber(typ, bits) != f {
				return -1
			}
			return searchBits(b, typ, bits, from, to, backward)
		}
	}
	size := typ.Size()
	match := func(i int) bool {
		bits := element.Load(b[i*size:], typ)
		switch {
		case typ == element.BigInt64:
			v, ok := x.BigInt64()
			return ok && v == int64(bits)
		case typ == element.BigUint64:
			if v, ok := x.BigInt64(); ok {
				return v >= 0 && uint64(v) == bits
			}
			v := x.BigInt()
			return v.IsUint64() && v.Uint64() == bits
		}
		f := element.DecodeNumber(typ, bits)
		return f == x.Number() || sameValueZero && f != f && x.Number() != x.Number()
	}
	if backward {
		for i := from; i >= to; i-- {
			if match(i) {
				return i
			}
		}
		return -1
	}
	for i := from; i < to; i++ {
		if match(i) {
			return i
		}
	}
	return -1
}

// searchBits is searchElements for the elements whose encoding is bits.
func searchBits(b []byte, typ element.Type, bits uint64, from, to int, backward bool) int {
	size := typ.Size()
	// A loaded encoding has no bits above the element size.
	bits &= 1<<(8*size) - 1
	if size == 1 {
		if backward {
			if from < to {
				return -1
			}
			if i := bytes.LastIndexByte(b[to:from+1], byte(bits)); i >= 0 {
				return to + i
			}
			return -1
		}
		if from >= to {
			return -1
		}
		if i := bytes.IndexByte(b[from:to], byte(bits)); i >= 0 {
			return from + i
		}
		return -1
	}
	if backward {
		for i := from; i >= to; i-- {
			if element.Load(b[i*size:], typ) == bits {
				return i
			}
		}
		return -1
	}
	for i := from; i < to; i++ {
		if element.Load(b[i*size:], typ) == bits {
			return i
		}
	}
	return -1
}

// fillElements stores the encoding x of an element of the type typ in every element of b.
func fillElements(b []byte, typ element.Type, x uint64) {
	size := typ.Size()
	if len(b) < size {
		return
	}
	element.Store(b, typ, x)
	for n := size; n < len(b); n *= 2 {
		copy(b[n:], b[:n])
	}
}

func requireTypedArray(cx *engine.ExecutionContext, t *storage.TypedArray, what string) {
	if t == nil {
		throwNotTypedArray(cx, what)
	}
}

func throwNotTypedArray(cx *engine.ExecutionContext, what string) {
	cx.ThrowTypeError("%TypedArray%.prototype." + what + ": this is not a typed array")
}

var (
	nativeTypedArrayConstructor    = engine.RegisterNative(typedArrayConstructor)
	nativeTypedArrayFrom           = engine.RegisterNative(typedArrayFrom)
	nativeTypedArrayOf             = engine.RegisterNative(typedArrayOf)
	nativeTypedArrayBuffer         = engine.RegisterNative(typedArrayBuffer)
	nativeTypedArrayByteLength     = engine.RegisterNative(typedArrayByteLength)
	nativeTypedArrayByteOffset     = engine.RegisterNative(typedArrayByteOffset)
	nativeTypedArrayLengthGetter   = engine.RegisterNative(typedArrayLengthGetter)
	nativeTypedArrayToStringTag    = engine.RegisterNative(typedArrayToStringTag)
	nativeTypedArrayAt             = engine.RegisterNative(typedArrayAt)
	nativeTypedArrayCopyWithin     = engine.RegisterNative(typedArrayCopyWithin)
	nativeTypedArrayEvery          = engine.RegisterNative(typedArrayEvery)
	nativeTypedArrayForEach        = engine.RegisterNative(typedArrayForEach)
	nativeTypedArrayFill           = engine.RegisterNative(typedArrayFill)
	nativeTypedArrayFilter         = engine.RegisterNative(typedArrayFilter)
	nativeTypedArrayJoin           = engine.RegisterNative(typedArrayJoin)
	nativeTypedArrayMap            = engine.RegisterNative(typedArrayMap)
	nativeTypedArrayReverse        = engine.RegisterNative(typedArrayReverse)
	nativeTypedArraySet            = engine.RegisterNative(typedArraySet)
	nativeTypedArraySlice          = engine.RegisterNative(typedArraySlice)
	nativeTypedArraySort           = engine.RegisterNative(typedArraySort)
	nativeTypedArraySubarray       = engine.RegisterNative(typedArraySubarray)
	nativeTypedArrayToLocaleString = engine.RegisterNative(typedArrayToLocaleString)
	nativeTypedArrayToReversed     = engine.RegisterNative(typedArrayToReversed)
	nativeTypedArrayToSorted       = engine.RegisterNative(typedArrayToSorted)
	nativeTypedArrayWith           = engine.RegisterNative(typedArrayWith)
)

func initTypedArrays(cx *engine.ExecutionContext) {
	proto := cx.NewOrdinaryObject()
	ctor := cx.NewNativeConstructor("TypedArray", 0, nativeTypedArrayConstructor)
	cx.Define(ctor, cx.Key(storage.KeyPrototype), storage.ObjectValue(proto), 0)
	cx.Define(proto, cx.Key(storage.KeyConstructor), storage.ObjectValue(ctor), storage.FlagsHidden)
	speciesGetter(cx, ctor)

	method(cx, ctor, "from", 1, nativeTypedArrayFrom)
	method(cx, ctor, "of", 0, nativeTypedArrayOf)

	getter(cx, proto, cx.Intern("buffer"), "buffer", nativeTypedArrayBuffer)
	getter(cx, proto, cx.Intern("byteLength"), "byteLength", nativeTypedArrayByteLength)
	getter(cx, proto, cx.Intern("byteOffset"), "byteOffset", nativeTypedArrayByteOffset)
	lengthGetter := cx.NewNative("get length", 0, nativeTypedArrayLengthGetter)
	cx.SetIntrinsic(storage.IntrinsicTypedArrayPrototypeLengthGetter, lengthGetter)
	cx.DefineAccessor(proto, cx.Key(storage.KeyLength), lengthGetter, nil, storage.FlagConfigurable)
	getter(cx, proto, cx.WellKnownSymbol(storage.SymbolToStringTag), "[Symbol.toStringTag]", nativeTypedArrayToStringTag)

	method(cx, proto, "at", 1, nativeTypedArrayAt)
	method(cx, proto, "copyWithin", 2, nativeTypedArrayCopyWithin)
	method(cx, proto, "entries", 0, nativeTypedArrayEntries)
	method(cx, proto, "keys", 0, nativeTypedArrayKeys)
	values := method(cx, proto, "values", 0, nativeTypedArrayValues)
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolIterator), storage.ObjectValue(values), storage.FlagsHidden)

	method(cx, proto, "find", 1, nativeTypedArrayFind)
	method(cx, proto, "findIndex", 1, nativeTypedArrayFindIndex)
	method(cx, proto, "findLast", 1, nativeTypedArrayFindLast)
	method(cx, proto, "findLastIndex", 1, nativeTypedArrayFindLastIndex)
	method(cx, proto, "some", 1, nativeTypedArraySome)
	method(cx, proto, "every", 1, nativeTypedArrayEvery)
	method(cx, proto, "forEach", 1, nativeTypedArrayForEach)
	method(cx, proto, "fill", 1, nativeTypedArrayFill)
	method(cx, proto, "filter", 1, nativeTypedArrayFilter)
	method(cx, proto, "includes", 1, nativeTypedArrayIncludes)
	method(cx, proto, "indexOf", 1, nativeTypedArrayIndexOf)
	method(cx, proto, "lastIndexOf", 1, nativeTypedArrayLastIndexOf)
	method(cx, proto, "join", 1, nativeTypedArrayJoin)
	method(cx, proto, "map", 1, nativeTypedArrayMap)
	method(cx, proto, "reduce", 1, nativeTypedArrayReduce)
	method(cx, proto, "reduceRight", 1, nativeTypedArrayReduceRight)
	method(cx, proto, "reverse", 0, nativeTypedArrayReverse)
	method(cx, proto, "set", 1, nativeTypedArraySet)
	method(cx, proto, "slice", 2, nativeTypedArraySlice)
	method(cx, proto, "sort", 1, nativeTypedArraySort)
	method(cx, proto, "subarray", 2, nativeTypedArraySubarray)
	method(cx, proto, "toLocaleString", 0, nativeTypedArrayToLocaleString)
	method(cx, proto, "toReversed", 0, nativeTypedArrayToReversed)
	method(cx, proto, "toSorted", 1, nativeTypedArrayToSorted)
	method(cx, proto, "with", 2, nativeTypedArrayWith)
	toString := cx.Get(cx.Intrinsic(storage.IntrinsicArrayPrototype), storage.NamedKey(cx.Key(storage.KeyToString)))
	cx.Define(proto, cx.Key(storage.KeyToString), toString, storage.FlagsHidden)

	for typ := range element.TypeCount {
		name := typedArrayNames[typ]
		p := cx.NewObject(proto)
		cx.SetIntrinsic(engine.TypedArrayPrototypeIntrinsic(typ), p)
		c := cx.NewNativeConstructor(wtf8.String(name), 3, typedArrayConstructors[typ])
		cx.SetIntrinsic(engine.TypedArrayConstructorIntrinsic(typ), c)
		cx.SetPrototypeOf(c, ctor)
		cx.DefineConstructorPrototype(c, engine.TypedArrayPrototypeIntrinsic(typ))
		cx.Define(p, cx.Key(storage.KeyConstructor), storage.ObjectValue(c), storage.FlagsHidden)
		cx.Define(cx.GlobalObject().Object(), cx.Intern(wtf8.String(name)), storage.ObjectValue(c), storage.FlagsHidden)
		bpe := storage.Number(float64(typ.Size()))
		constant(cx, c, "BYTES_PER_ELEMENT", bpe)
		constant(cx, p, "BYTES_PER_ELEMENT", bpe)
	}
	initUint8ArrayBase64(cx)
}

func typedArrayConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cx.ThrowTypeError("abstract class TypedArray not directly constructable")
	return storage.Undefined
}

func typedArrayFrom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if !this.IsCallable() || !cx.IsConstructor(this.Object()) {
		cx.ThrowTypeError(cx.DisplayString(this) + " is not a constructor")
	}
	c := this.Object()
	source, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
	if !mapFn.IsUndefined() && !mapFn.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(mapFn) + " is not a function")
	}
	mapped := func(v storage.Value, i int) storage.Value {
		if mapFn.IsUndefined() {
			return v
		}
		return cx.Call(mapFn.Object(), thisArg, v, storage.Number(float64(i)))
	}
	if m := cx.GetMethod(source, cx.WellKnownSymbol(storage.SymbolIterator)); m != nil {
		vals := cx.IteratorToList(source, m)
		o, t := createFromConstructor(cx, c, []storage.Value{storage.Number(float64(len(vals)))}, true)
		var i int
		if mapFn.IsUndefined() {
			i = storeNumberValues(cx, t, vals)
		}
		for ; i < len(vals); i++ {
			storeAt(cx, o, t, i, mapped(vals[i], i))
		}
		return storage.ObjectValue(o)
	}
	src := cx.ToObject(source)
	n := cx.LengthOfArrayLike(src)
	o, t := createFromConstructor(cx, c, []storage.Value{storage.Number(float64(n))}, true)
	for i := range n {
		storeAt(cx, o, t, int(i), mapped(getAt(cx, src, i), int(i)))
	}
	return storage.ObjectValue(o)
}

func typedArrayOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if !this.IsCallable() || !cx.IsConstructor(this.Object()) {
		cx.ThrowTypeError(cx.DisplayString(this) + " is not a constructor")
	}
	o, t := createFromConstructor(cx, this.Object(), []storage.Value{storage.Number(float64(len(args)))}, true)
	for i, v := range args {
		storeAt(cx, o, t, i, v)
	}
	return storage.ObjectValue(o)
}

func typedArrayBuffer(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	requireTypedArray(cx, t, "buffer")
	return storage.ObjectValue(t.Buffer())
}

func typedArrayByteLength(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	requireTypedArray(cx, t, "byteLength")
	n, _ := t.Length()
	return storage.Number(float64(n * t.ElementType().Size()))
}

func typedArrayByteOffset(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	requireTypedArray(cx, t, "byteOffset")
	if _, ok := t.Length(); !ok {
		return storage.Number(0)
	}
	return storage.Number(float64(t.ByteOffset()))
}

func typedArrayLengthGetter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	requireTypedArray(cx, t, "length")
	n, _ := t.Length()
	return storage.Number(float64(n))
}

func typedArrayToStringTag(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	if t == nil {
		return storage.Undefined
	}
	return cx.NewString(wtf8.String(typedArrayNames[t.ElementType()]))
}

func typedArrayAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype.at")
	i := cx.ToIntegerOrInfinity(arg(args, 0))
	if i < 0 {
		i += float64(n)
	}
	if i < 0 || i >= float64(n) {
		return storage.Undefined
	}
	return getAt(cx, o, int64(i))
}

func typedArrayCopyWithin(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateWritableTypedArray(cx, this, "%TypedArray%.prototype.copyWithin")
	target := numconv.RelativeIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	start, end := relativeRange(cx, arg(args, 1), arg(args, 2), n)
	count := min(end-start, n-target)
	if count > 0 {
		n, ok := t.Length()
		if !ok {
			cx.ThrowTypeError("%TypedArray%.prototype.copyWithin: the typed array is detached or out of bounds")
		}
		size := t.ElementType().Size()
		limit := n * size
		from, to := start*size, target*size
		if c := min(count*size, limit-from, limit-to); c > 0 {
			copyBytesWithin(t, to, from, c)
		}
	}
	return storage.ObjectValue(o)
}

var (
	nativeTypedArrayEntries = engine.RegisterNative(typedArrayIterationNative("entries", quiet.IterateEntries))
	nativeTypedArrayKeys    = engine.RegisterNative(typedArrayIterationNative("keys", quiet.IterateKeys))
	nativeTypedArrayValues  = engine.RegisterNative(typedArrayIterationNative("values", quiet.IterateValues))
)

// typedArrayIterationNative returns the method name of %TypedArray%.prototype, which returns an
// Array Iterator of kind.
//
//go:noinline
func typedArrayIterationNative(name string, kind quiet.IterationKind) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o, _, _ := validateTypedArray(cx, this, "%TypedArray%.prototype."+name)
		return storage.ObjectValue(cx.NewArrayIterator(o, kind))
	}
}

var (
	nativeTypedArrayFind          = engine.RegisterNative(typedArrayFindNative("find", false, foundElement))
	nativeTypedArrayFindIndex     = engine.RegisterNative(typedArrayFindNative("findIndex", false, foundIndex))
	nativeTypedArrayFindLast      = engine.RegisterNative(typedArrayFindNative("findLast", true, foundElement))
	nativeTypedArrayFindLastIndex = engine.RegisterNative(typedArrayFindNative("findLastIndex", true, foundIndex))
	nativeTypedArraySome          = engine.RegisterNative(typedArrayFindNative("some", false, foundAny))
)

// typedArrayFindNative returns the method name of %TypedArray%.prototype, which calls a callback for
// the elements in order until it returns true, and returns result for that element. With backward,
// the method starts at the last element.
//
//go:noinline
func typedArrayFindNative(name string, backward bool, result func(found bool, i int, v storage.Value) storage.Value) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype."+name)
		f := callback(cx, args, name)
		thisArg := arg(args, 1)
		for j := range n {
			i := j
			if backward {
				i = n - 1 - j
			}
			v := getAt(cx, o, int64(i))
			if storage.ToBoolean(cx.Call(f, thisArg, v, storage.Number(float64(i)), storage.ObjectValue(o))) {
				return result(true, i, v)
			}
		}
		return result(false, -1, storage.Undefined)
	}
}

func foundElement(found bool, i int, v storage.Value) storage.Value {
	return v
}

func foundIndex(found bool, i int, v storage.Value) storage.Value {
	return storage.Number(float64(i))
}

func foundAny(found bool, i int, v storage.Value) storage.Value {
	return storage.Bool(found)
}

// typedArrayEvery stops at the first false result instead of the first true one.
func typedArrayEvery(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype.every")
	f := callback(cx, args, "every")
	thisArg := arg(args, 1)
	for i := range n {
		v := getAt(cx, o, int64(i))
		if !storage.ToBoolean(cx.Call(f, thisArg, v, storage.Number(float64(i)), storage.ObjectValue(o))) {
			return storage.False
		}
	}
	return storage.True
}

func typedArrayForEach(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype.forEach")
	f := callback(cx, args, "forEach")
	thisArg := arg(args, 1)
	for i := range n {
		cx.Call(f, thisArg, getAt(cx, o, int64(i)), storage.Number(float64(i)), storage.ObjectValue(o))
	}
	return storage.Undefined
}

func typedArrayFill(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateWritableTypedArray(cx, this, "%TypedArray%.prototype.fill")
	typ := t.ElementType()
	v := cx.ToElementValue(typ, arg(args, 0))
	start, end := relativeRange(cx, arg(args, 1), arg(args, 2), n)
	cur, ok := t.Length()
	if !ok {
		cx.ThrowTypeError("%TypedArray%.prototype.fill: the typed array is detached or out of bounds")
	}
	end = min(end, cur)
	if start < end {
		fillRange(cx, t, start, end, storage.ElementBits(typ, v))
	}
	return storage.ObjectValue(o)
}

func typedArrayFilter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.filter")
	f := callback(cx, args, "filter")
	thisArg := arg(args, 1)
	var kept []storage.Value
	for i := range n {
		v := getAt(cx, o, int64(i))
		if storage.ToBoolean(cx.Call(f, thisArg, v, storage.Number(float64(i)), storage.ObjectValue(o))) {
			kept = append(kept, v)
		}
	}
	a, at := typedArraySpeciesCreate(cx, o, t, []storage.Value{storage.Number(float64(len(kept)))}, true)
	for i, v := range kept {
		storeAt(cx, a, at, i, v)
	}
	return storage.ObjectValue(a)
}

var (
	nativeTypedArrayIncludes    = engine.RegisterNative(typedArraySearchNative("includes", false, true))
	nativeTypedArrayIndexOf     = engine.RegisterNative(typedArraySearchNative("indexOf", false, false))
	nativeTypedArrayLastIndexOf = engine.RegisterNative(typedArraySearchNative("lastIndexOf", true, false))
)

// typedArraySearchNative returns the method name of %TypedArray%.prototype, which searches a typed
// array for a value. With backward, the method goes from the end of the array to its start. With
// sameValueZero, it compares by SameValueZero and returns whether it finds the value. Otherwise, it
// compares by IsStrictlyEqual and returns the index.
//
//go:noinline
func typedArraySearchNative(name string, backward, sameValueZero bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		notFound := storage.Number(-1)
		if sameValueZero {
			notFound = storage.False
		}
		_, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype."+name)
		if n == 0 {
			return notFound
		}
		x := arg(args, 0)
		var from, to int
		if backward {
			from = n - 1
			if len(args) > 1 {
				f := cx.ToIntegerOrInfinity(args[1])
				if math.IsInf(f, -1) {
					return notFound
				}
				if f >= 0 {
					from = int(min(f, float64(n-1)))
				} else {
					from = int(float64(n) + f)
				}
			}
			to = 0
		} else {
			f := cx.ToIntegerOrInfinity(arg(args, 1))
			if math.IsInf(f, 1) {
				return notFound
			}
			from = numconv.RelativeIndex(f, n)
			to = n
		}
		// The conversion of the index may have shrunk the buffer: the elements beyond its end are undefined.
		cur, _ := t.Length()
		if sameValueZero && x.IsUndefined() && from < to && to > cur {
			return storage.True
		}
		if backward {
			from = min(from, cur-1)
		} else {
			to = min(to, cur)
		}
		i := searchRange(cx, t, x, from, to, backward, sameValueZero)
		if sameValueZero {
			return storage.Bool(i >= 0)
		}
		return storage.Number(float64(i))
	}
}

func typedArrayJoin(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype.join")
	sep := wtf8.String(",")
	if s := arg(args, 0); !s.IsUndefined() {
		sep = cx.ToString(s)
	}
	var b wtf8.Buffer
	for i := range n {
		if i > 0 {
			b.WriteString(sep)
		}
		if e := getAt(cx, o, int64(i)); !e.IsUndefined() {
			writeString(cx, &b, e)
		}
		if b.Len() > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	}
	return cx.NewString(b.String())
}

func typedArrayMap(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.map")
	f := callback(cx, args, "map")
	thisArg := arg(args, 1)
	a, at := typedArraySpeciesCreate(cx, o, t, []storage.Value{storage.Number(float64(n))}, true)
	for i := range n {
		v := cx.Call(f, thisArg, getAt(cx, o, int64(i)), storage.Number(float64(i)), storage.ObjectValue(o))
		storeAt(cx, a, at, i, v)
	}
	return storage.ObjectValue(a)
}

var (
	nativeTypedArrayReduce      = engine.RegisterNative(typedArrayReduceNative("reduce", false))
	nativeTypedArrayReduceRight = engine.RegisterNative(typedArrayReduceNative("reduceRight", true))
)

// typedArrayReduceNative returns the method name of %TypedArray%.prototype, which reduces a typed
// array. With backward, the method goes from the end of the array to its start.
//
//go:noinline
func typedArrayReduceNative(name string, backward bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype."+name)
		f := callback(cx, args, name)
		index := func(j int) int {
			if backward {
				return n - 1 - j
			}
			return j
		}
		j := 0
		var acc storage.Value
		if len(args) > 1 {
			acc = args[1]
		} else {
			if n == 0 {
				cx.ThrowTypeError("reduce of empty array with no initial value")
			}
			acc = getAt(cx, o, int64(index(0)))
			j = 1
		}
		for ; j < n; j++ {
			i := index(j)
			acc = cx.Call(f, storage.Undefined, acc, getAt(cx, o, int64(i)), storage.Number(float64(i)), storage.ObjectValue(o))
		}
		return acc
	}
}

func typedArrayReverse(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateWritableTypedArray(cx, this, "%TypedArray%.prototype.reverse")
	reverseFirst(cx, t, n)
	return storage.ObjectValue(o)
}

func typedArraySet(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	if t == nil {
		cx.ThrowTypeError("%TypedArray%.prototype.set: this is not a typed array")
	}
	if t.ArrayBuffer().IsImmutable() {
		cx.ThrowTypeError("%TypedArray%.prototype.set: the typed array is backed by an immutable ArrayBuffer")
	}
	offset := cx.ToIntegerOrInfinity(arg(args, 1))
	if offset < 0 {
		cx.ThrowRangeError("offset is out of bounds")
	}
	setTypedArrayFrom(cx, this.Object(), t, offset, arg(args, 0))
	return storage.Undefined
}

func typedArraySlice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.slice")
	start, end := relativeRange(cx, arg(args, 0), arg(args, 1), n)
	count := max(end-start, 0)
	a, at := typedArraySpeciesCreate(cx, o, t, []storage.Value{storage.Number(float64(count))}, true)
	if count == 0 {
		return storage.ObjectValue(a)
	}
	cur, ok := t.Length()
	if !ok {
		cx.ThrowTypeError("%TypedArray%.prototype.slice: the typed array is detached or out of bounds")
	}
	end = min(end, cur)
	count = max(end-start, 0)
	typ := t.ElementType()
	if typ != at.ElementType() {
		for i := range count {
			storeAt(cx, a, at, i, getAt(cx, o, int64(start+i)))
		}
		return storage.ObjectValue(a)
	}
	size := typ.Size()
	src, dst := start*size+t.ByteOffset(), at.ByteOffset()
	if at.ArrayBuffer().SameDataBlock(t.ArrayBuffer()) && src < dst && dst < src+count*size {
		// The bytes are copied one at a time from the first on, as the specification says.
		copyBytesForward(cx, t.ArrayBuffer(), dst, src, count*size)
	} else {
		storage.CopyBufferBytes(at.ArrayBuffer(), dst, t.ArrayBuffer(), src, count*size)
	}
	return storage.ObjectValue(a)
}

func typedArraySort(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cmp := arg(args, 0)
	if !cmp.IsUndefined() && !cmp.IsCallable() {
		cx.ThrowTypeError("the comparison function must be either a function or undefined")
	}
	o, t, n := validateWritableTypedArray(cx, this, "%TypedArray%.prototype.sort")
	if cmp.IsUndefined() {
		sortFirst(cx, t, n)
		return storage.ObjectValue(o)
	}
	vals := make([]storage.Value, n)
	for i := range vals {
		vals[i] = getAt(cx, o, int64(i))
	}
	sortValues(cx, vals, cmp)
	for i, v := range vals {
		storeAt(cx, o, t, i, v)
	}
	return storage.ObjectValue(o)
}

func typedArraySubarray(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	t := storage.TypedArrayData(this)
	if t == nil {
		cx.ThrowTypeError("%TypedArray%.prototype.subarray: this is not a typed array")
	}
	n, _ := t.Length()
	start := numconv.RelativeIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	size := t.ElementType().Size()
	begin := storage.Number(float64(t.ByteOffset() + start*size))
	buf := storage.ObjectValue(t.Buffer())
	var a *storage.Object
	if end := arg(args, 1); t.IsLengthTracking() && end.IsUndefined() {
		a, _ = typedArraySpeciesCreate(cx, this.Object(), t, []storage.Value{buf, begin}, false)
	} else {
		e := n
		if !end.IsUndefined() {
			e = numconv.RelativeIndex(cx.ToIntegerOrInfinity(end), n)
		}
		a, _ = typedArraySpeciesCreate(cx, this.Object(), t, []storage.Value{buf, begin, storage.Number(float64(max(e-start, 0)))}, false)
	}
	return storage.ObjectValue(a)
}

func typedArrayToLocaleString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, _, n := validateTypedArray(cx, this, "%TypedArray%.prototype.toLocaleString")
	var b wtf8.Buffer
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		if e := getAt(cx, o, int64(i)); !e.IsNullish() {
			b.WriteString(cx.ToString(cx.CallValue(cx.GetV(e, cx.Key(storage.KeyToLocaleString)), e)))
		}
		if b.Len() > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	}
	return cx.NewString(b.String())
}

func typedArrayToReversed(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	_, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.toReversed")
	a, at := typedArrayCreateSameType(cx, t, n)
	copyElements(at, 0, t, n)
	reverseFirst(cx, at, n)
	return storage.ObjectValue(a)
}

func typedArrayToSorted(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cmp := arg(args, 0)
	if !cmp.IsUndefined() && !cmp.IsCallable() {
		cx.ThrowTypeError("the comparison function must be either a function or undefined")
	}
	o, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.toSorted")
	a, at := typedArrayCreateSameType(cx, t, n)
	if cmp.IsUndefined() {
		copyElements(at, 0, t, n)
		sortFirst(cx, at, n)
		return storage.ObjectValue(a)
	}
	vals := make([]storage.Value, n)
	for i := range vals {
		vals[i] = getAt(cx, o, int64(i))
	}
	sortValues(cx, vals, cmp)
	for i, v := range vals {
		storeAt(cx, a, at, i, v)
	}
	return storage.ObjectValue(a)
}

func typedArrayWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o, t, n := validateTypedArray(cx, this, "%TypedArray%.prototype.with")
	rel := cx.ToIntegerOrInfinity(arg(args, 0))
	idx := rel
	if rel < 0 {
		idx = float64(n) + rel
	}
	typ := t.ElementType()
	v := cx.ToElementValue(typ, arg(args, 1))
	cur, _ := t.Length()
	if idx < 0 || idx >= float64(cur) {
		cx.ThrowRangeError("invalid typed array index")
	}
	a, at := typedArrayCreateSameType(cx, t, n)
	copied := min(n, cur)
	copyElements(at, 0, t, copied)
	for i := copied; i < n; i++ {
		storeAt(cx, a, at, i, getAt(cx, o, int64(i)))
	}
	storeAt(cx, a, at, int(idx), v)
	return storage.ObjectValue(a)
}

var typedArrayConstructors = [element.TypeCount]storage.Native{
	element.Int8:         engine.RegisterNative(typedArrayConstructorNative(element.Int8)),
	element.Uint8:        engine.RegisterNative(typedArrayConstructorNative(element.Uint8)),
	element.Uint8Clamped: engine.RegisterNative(typedArrayConstructorNative(element.Uint8Clamped)),
	element.Int16:        engine.RegisterNative(typedArrayConstructorNative(element.Int16)),
	element.Uint16:       engine.RegisterNative(typedArrayConstructorNative(element.Uint16)),
	element.Int32:        engine.RegisterNative(typedArrayConstructorNative(element.Int32)),
	element.Uint32:       engine.RegisterNative(typedArrayConstructorNative(element.Uint32)),
	element.Float16:      engine.RegisterNative(typedArrayConstructorNative(element.Float16)),
	element.Float32:      engine.RegisterNative(typedArrayConstructorNative(element.Float32)),
	element.Float64:      engine.RegisterNative(typedArrayConstructorNative(element.Float64)),
	element.BigInt64:     engine.RegisterNative(typedArrayConstructorNative(element.BigInt64)),
	element.BigUint64:    engine.RegisterNative(typedArrayConstructorNative(element.BigUint64)),
}

// typedArrayConstructorNative returns the constructor of the typed arrays with elements of typ.
//
//go:noinline
func typedArrayConstructorNative(typ element.Type) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		return constructTypedArray(cx, typ, args)
	}
}

// setTypedArrayFrom implements the part of %TypedArray%.prototype.set after the offset is converted.
func setTypedArrayFrom(cx *engine.ExecutionContext, target *storage.Object, t *storage.TypedArray, offset float64, source storage.Value) {
	typ := t.ElementType()
	if src := storage.TypedArrayData(source); src != nil {
		targetLen, ok := t.Length()
		if !ok {
			cx.ThrowTypeError("%TypedArray%.prototype.set: the typed array is detached or out of bounds")
		}
		srcLen, ok := src.Length()
		if !ok {
			cx.ThrowTypeError("%TypedArray%.prototype.set: the source typed array is detached or out of bounds")
		}
		if math.IsInf(offset, 1) || float64(srcLen)+offset > float64(targetLen) {
			cx.ThrowRangeError("offset is out of bounds")
		}
		srcType := src.ElementType()
		if typ.IsBigInt() != srcType.IsBigInt() {
			cx.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
		}
		// A shared source is copied directly to a target that is not shared, without the copy that
		// elementsOf makes.
		from, to := src.ArrayBuffer(), t.ArrayBuffer()
		if srcType == typ && (from.SameDataBlock(to) || from.IsShared() && !to.IsShared()) {
			copyElements(t, int(offset), src, srcLen)
			return
		}
		sb := elementsOf(src)
		if src.ArrayBuffer() == t.ArrayBuffer() && !src.ArrayBuffer().IsShared() {
			// Conversion needs the original elements when the views overlap.
			sb = slices.Clone(sb)
		}
		storeConverted(cx, t, int(offset), sb, srcType, srcLen)
		return
	}
	targetLen, ok := t.Length()
	if !ok {
		cx.ThrowTypeError("%TypedArray%.prototype.set: the typed array is detached or out of bounds")
	}
	src := cx.ToObject(source)
	srcLen := cx.LengthOfArrayLike(src)
	if math.IsInf(offset, 1) || float64(srcLen)+offset > float64(targetLen) {
		cx.ThrowRangeError("offset is out of bounds")
	}
	off := int(offset)
	if !typ.IsBigInt() {
		// The elements that are numbers that reading runs no JavaScript for are stored directly.
		// Reading the elements of a typed array on a shared buffer is never direct, so it takes
		// no lock.
		i := storeNumbers(cx, t, off, src, srcLen)
		for ; i < srcLen; i++ {
			storeAt(cx, target, t, off+int(i), getAt(cx, src, i))
		}
		return
	}
	for i := range srcLen {
		storeAt(cx, target, t, off+int(i), getAt(cx, src, i))
	}
}
