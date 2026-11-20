// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"bytes"
	"cmp"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// typedArrayNames are the names of the typed array constructors, by element type.
var typedArrayNames = [engine.ElementTypeCount]string{
	"Int8Array", "Uint8Array", "Uint8ClampedArray", "Int16Array", "Uint16Array", "Int32Array",
	"Uint32Array", "Float16Array", "Float32Array", "Float64Array", "BigInt64Array", "BigUint64Array",
}

// validateTypedArray implements ValidateTypedArray with the access mode read: it returns this
// as a typed array and its length.
func validateTypedArray(vm *engine.VM, this engine.Value, what string) (*engine.Object, *engine.TypedArray, int) {
	return validateTypedArrayAccess(vm, this, false, what)
}

// validateWritableTypedArray implements ValidateTypedArray with the access mode write.
func validateWritableTypedArray(vm *engine.VM, this engine.Value, what string) (*engine.Object, *engine.TypedArray, int) {
	return validateTypedArrayAccess(vm, this, true, what)
}

// validateTypedArrayAccess implements ValidateTypedArray with the access mode write when write is set.
func validateTypedArrayAccess(vm *engine.VM, this engine.Value, write bool, what string) (*engine.Object, *engine.TypedArray, int) {
	t := engine.TypedArrayData(this)
	if t == nil {
		vm.ThrowTypeError(what + ": this is not a typed array")
	}
	if write && t.ArrayBuffer().IsImmutable() {
		vm.ThrowTypeError(what + ": the typed array is backed by an immutable ArrayBuffer")
	}
	n, ok := t.Length()
	if !ok {
		vm.ThrowTypeError(what + ": the typed array is detached or out of bounds")
	}
	return this.Object(), t, n
}

// storeAt implements TypedArraySetElement for the typed array o, whose state is t, and the index i.
func storeAt(vm *engine.VM, o *engine.Object, t *engine.TypedArray, i int, v engine.Value) {
	typ := t.ElementType()
	if v.IsNumber() && !typ.IsBigInt() {
		t.StoreElement(i, engine.ElementBits(typ, v))
		return
	}
	vm.Set(o, engine.IndexKey(uint32(i)), v)
}

// The bytes of a buffer are reached with Lock and Unlock, which keep the other agents from accessing
// a shared buffer in between. The helpers below run no JavaScript. Those that take vm work in runs,
// each under its own lock, and check for an interruption between runs; the others hold one lock.

// lockedRuns calls f with the bytes of the elements of t, while t is locked, and with the bounds
// i and j of each run of at most [engine.PollInterval] indices from start up to end, the last
// run first when backward is set. It checks for an interruption between runs, and stops when f
// reports false.
func lockedRuns(vm *engine.VM, t *engine.TypedArray, start, end int, backward bool, f func(b []byte, i, j int) bool) {
	for k := start; k < end; k += engine.PollInterval {
		if k > start {
			vm.Poll()
		}
		i, j := k, min(k+engine.PollInterval, end)
		if backward {
			i, j = start+end-j, start+end-k
		}
		if !lockedRun(t, i, j, f) {
			return
		}
	}
}

// lockedRun calls f with the bytes of the elements of t, while t is locked, and with i and j.
func lockedRun(t *engine.TypedArray, i, j int, f func(b []byte, i, j int) bool) bool {
	b := t.Lock()
	defer t.Unlock()
	return f(b, i, j)
}

// elementsOf returns the bytes of the elements of t, or nil when t is out of bounds. The bytes of
// a shared buffer are a copy, as other agents may change them.
func elementsOf(t *engine.TypedArray) []byte {
	b := t.Lock()
	defer t.Unlock()
	if t.ArrayBuffer().IsShared() {
		return slices.Clone(b)
	}
	return b
}

// writeElements copies src to the bytes of the elements of t from the element offset on, as far as
// they reach.
func writeElements(t *engine.TypedArray, offset int, src []byte) {
	b := t.Lock()
	defer t.Unlock()
	if o := offset * t.ElementType().Size(); o < len(b) {
		copy(b[o:], src)
	}
}

// copyElements copies the first n elements of src to the element offset in dst.
// The element types must match and both ranges must be in bounds.
func copyElements(dst *engine.TypedArray, offset int, src *engine.TypedArray, n int) {
	size := src.ElementType().Size()
	engine.CopyBufferBytes(dst.ArrayBuffer(), dst.ByteOffset()+offset*size, src.ArrayBuffer(), src.ByteOffset(), n*size)
}

// storeConverted stores the n elements of the type from in src as elements of t from the element
// offset on. t must have enough elements.
func storeConverted(vm *engine.VM, t *engine.TypedArray, offset int, src []byte, from engine.ElementType, n int) {
	to := t.ElementType()
	ts, fs := to.Size(), from.Size()
	lockedRuns(vm, t, 0, n, false, func(b []byte, i, j int) bool {
		copyConverted(b[(offset+i)*ts:], to, src[i*fs:], from, j-i)
		return true
	})
}

// reverseFirst reverses the first n elements of t.
func reverseFirst(vm *engine.VM, t *engine.TypedArray, n int) {
	size := t.ElementType().Size()
	lockedRuns(vm, t, 0, n/2, false, func(b []byte, i, j int) bool {
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
func sortFirst(vm *engine.VM, t *engine.TypedArray, n int) {
	typ := t.ElementType()
	switch typ {
	case engine.ElementBigInt64:
		vals := loadElements(vm, t, n, func(x uint64) int64 {
			return int64(x)
		})
		sortPolled(vm, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		storeElements(vm, t, vals, func(v int64) uint64 {
			return uint64(v)
		})
	case engine.ElementBigUint64:
		vals := loadElements(vm, t, n, func(x uint64) uint64 {
			return x
		})
		sortPolled(vm, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		storeElements(vm, t, vals, func(v uint64) uint64 {
			return v
		})
	default:
		vals := loadElements(vm, t, n, func(x uint64) float64 {
			return engine.ElementNumber(typ, x)
		})
		switch typ {
		case engine.ElementFloat16, engine.ElementFloat32, engine.ElementFloat64:
			sortPolled(vm, vals, numberSortRunLength, func(s []float64) {
				slices.SortFunc(s, compareNumbers)
			}, compareNumbers)
		default:
			sortPolled(vm, vals, numberSortRunLength, slices.Sort, cmp.Compare)
		}
		storeElements(vm, t, vals, func(v float64) uint64 {
			return engine.ElementBits(typ, engine.Number(v))
		})
	}
}

// loadElements returns the first n elements of t, which has them, decoded by decode.
func loadElements[E any](vm *engine.VM, t *engine.TypedArray, n int, decode func(x uint64) E) []E {
	typ := t.ElementType()
	size := typ.Size()
	vals := make([]E, n)
	lockedRuns(vm, t, 0, n, false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			vals[k] = decode(engine.LoadElementBits(b[k*size:], typ))
		}
		return true
	})
	return vals
}

// storeElements stores vals encoded by encode as the first elements of t, which has room for them.
func storeElements[E any](vm *engine.VM, t *engine.TypedArray, vals []E, encode func(v E) uint64) {
	typ := t.ElementType()
	size := typ.Size()
	lockedRuns(vm, t, 0, len(vals), false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			engine.StoreElementBits(b[k*size:], typ, encode(vals[k]))
		}
		return true
	})
}

// fillRange stores the encoding x in the elements of t from start up to end.
func fillRange(vm *engine.VM, t *engine.TypedArray, start, end int, x uint64) {
	typ := t.ElementType()
	size := typ.Size()
	if end-start <= engine.PollInterval {
		lockedRun(t, start, end, func(b []byte, i, j int) bool {
			fillElements(b[i*size:min(j*size, len(b))], typ, x)
			return true
		})
		return
	}
	// Every run is a copy of one pattern.
	pattern := make([]byte, engine.PollInterval*size)
	fillElements(pattern, typ, x)
	lockedRuns(vm, t, start, end, false, func(b []byte, i, j int) bool {
		copy(b[i*size:min(j*size, len(b))], pattern)
		return true
	})
}

// searchRange is searchElements on the elements of t.
func searchRange(vm *engine.VM, t *engine.TypedArray, x engine.Value, from, to int, backward, sameValueZero bool) int {
	typ := t.ElementType()
	found := -1
	start, end := from, to
	if backward {
		start, end = to, from+1
	}
	lockedRuns(vm, t, start, end, backward, func(b []byte, i, j int) bool {
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
func copyBytesWithin(t *engine.TypedArray, to, from, c int) {
	b := t.Lock()
	defer t.Unlock()
	copy(b[to:to+c], b[from:from+c])
}

// copyBytesForward copies n bytes of the buffer b from the byte index src to the byte index dst one
// at a time from the first on.
func copyBytesForward(vm *engine.VM, b *engine.ArrayBuffer, dst, src, n int) {
	for i := 0; i < n; i += engine.PollInterval {
		if i > 0 {
			vm.Poll()
		}
		copyBytesForwardRun(b, dst+i, src+i, min(engine.PollInterval, n-i))
	}
}

// copyBytesForwardRun is copyBytesForward under one lock.
func copyBytesForwardRun(b *engine.ArrayBuffer, dst, src, n int) {
	data := b.Lock()
	defer b.Unlock()
	for i := range n {
		data[dst+i] = data[src+i]
	}
}

// storeNumbers stores the elements of src from 0 on that are numbers that reading runs no
// JavaScript for in the elements of t from the element offset on, as far as they reach, and
// returns how many it stored.
func storeNumbers(vm *engine.VM, t *engine.TypedArray, offset int, src *engine.Object, n int64) int64 {
	typ := t.ElementType()
	size := typ.Size()
	var stored int64
	lockedRuns(vm, t, 0, int(n), false, func(b []byte, i, j int) bool {
		for k := i; k < j; k++ {
			e, ok := src.Element(int64(k))
			if !ok || !e.IsNumber() || (offset+k+1)*size > len(b) {
				return false
			}
			engine.StoreElementBits(b[(offset+k)*size:], typ, engine.ElementBits(typ, e))
			stored++
		}
		return true
	})
	return stored
}

// storeNumberValues stores the leading numbers of vals in the elements of t from 0 on, as far as
// they reach, and returns how many it stored. It stores nothing in a typed array of BigInts.
func storeNumberValues(t *engine.TypedArray, vals []engine.Value) int {
	typ := t.ElementType()
	if typ.IsBigInt() {
		return 0
	}
	b := t.Lock()
	defer t.Unlock()
	size := typ.Size()
	n := min(len(vals), len(b)/size)
	for i, v := range vals[:n] {
		if !v.IsNumber() {
			return i
		}
		engine.StoreElementBits(b[i*size:], typ, engine.ElementBits(typ, v))
	}
	return n
}

// newTypedArray returns a new typed array of n elements of the type typ on a new ArrayBuffer.
func (bi *binaryIntrinsics) newTypedArray(vm *engine.VM, proto *engine.Object, typ engine.ElementType, n int64) *engine.Object {
	buf := vm.NewArrayBufferObject(bi.arrayBufferProto, n*int64(typ.Size()), -1, false)
	return vm.NewTypedArrayObject(proto, typ, buf, 0, int(n))
}

// createFromConstructor implements TypedArrayCreateFromConstructor. The access mode is write when write is set.
func createFromConstructor(vm *engine.VM, c *engine.Object, args []engine.Value, write bool) (*engine.Object, *engine.TypedArray) {
	r := engine.ObjectValue(vm.Construct(c, args, c))
	o, t, n := validateTypedArrayAccess(vm, r, write, "typed array constructor")
	if len(args) == 1 && args[0].IsNumber() && float64(n) < args[0].Number() {
		vm.ThrowTypeError("typed array constructor returned a too short typed array")
	}
	return o, t
}

// speciesCreate implements TypedArraySpeciesCreate. The access mode is write when write is set.
func (bi *binaryIntrinsics) speciesCreate(vm *engine.VM, exemplar *engine.Object, t *engine.TypedArray, args []engine.Value, write bool) (*engine.Object, *engine.TypedArray) {
	c := speciesConstructor(vm, exemplar, bi.ctors[t.ElementType()])
	o, r := createFromConstructor(vm, c, args, write)
	if r.ElementType().IsBigInt() != t.ElementType().IsBigInt() {
		vm.ThrowTypeError("typed array species constructor returned a typed array of a different content type")
	}
	return o, r
}

// createSameType implements TypedArrayCreateSameType.
func (bi *binaryIntrinsics) createSameType(vm *engine.VM, t *engine.TypedArray, n int) (*engine.Object, *engine.TypedArray) {
	typ := t.ElementType()
	o := bi.newTypedArray(vm, bi.protos[typ], typ, int64(n))
	return o, engine.TypedArrayData(engine.ObjectValue(o))
}

// copyConverted stores the n elements of the type from in src as elements of the type to in dst.
func copyConverted(dst []byte, to engine.ElementType, src []byte, from engine.ElementType, n int) {
	if from == to {
		copy(dst[:n*to.Size()], src)
		return
	}
	fs, ts := from.Size(), to.Size()
	for i := range n {
		x := engine.LoadElementBits(src[i*fs:], from)
		engine.StoreElementBits(dst[i*ts:], to, engine.ConvertElementBits(from, to, x))
	}
}

// construct implements the typed array constructors for the element type typ.
func (bi *binaryIntrinsics) construct(vm *engine.VM, typ engine.ElementType, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	name := typedArrayNames[typ]
	if nt == nil {
		vm.ThrowTypeError("constructor " + name + " requires 'new'")
	}
	defProto := bi.protos[typ]
	size := typ.Size()
	first := arg(args, 0)
	if !first.IsObject() {
		var n int64
		if len(args) > 0 {
			n = toIndex(vm, first)
		}
		proto := vm.GetPrototypeFromConstructor(nt, defProto)
		return engine.ObjectValue(bi.newTypedArray(vm, proto, typ, n))
	}
	proto := vm.GetPrototypeFromConstructor(nt, defProto)
	if src := engine.TypedArrayData(first); src != nil {
		n, ok := src.Length()
		if !ok {
			vm.ThrowTypeError("cannot construct " + name + " from a detached or out of bounds typed array")
		}
		if src.ElementType().IsBigInt() != typ.IsBigInt() {
			vm.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
		}
		o := bi.newTypedArray(vm, proto, typ, int64(n))
		storeConverted(vm, engine.TypedArrayData(engine.ObjectValue(o)), 0, elementsOf(src), src.ElementType(), n)
		return engine.ObjectValue(o)
	}
	if b := engine.ArrayBufferData(first); b != nil {
		offset := toIndex(vm, arg(args, 1))
		if offset%int64(size) != 0 {
			vm.ThrowRangeError("start offset of " + name + " should be a multiple of " + itoa(int64(size)))
		}
		fixed := !b.IsResizable()
		length := arg(args, 2)
		var newLength int64
		if !length.IsUndefined() {
			newLength = toIndex(vm, length)
		}
		if b.IsDetached() {
			vm.ThrowTypeError("cannot construct " + name + " on a detached ArrayBuffer")
		}
		bl := int64(b.ByteLength())
		if length.IsUndefined() && !fixed {
			if offset > bl {
				vm.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
			}
			return engine.ObjectValue(vm.NewTypedArrayObject(proto, typ, first.Object(), int(offset), -1))
		}
		var newByteLength int64
		if length.IsUndefined() {
			if bl%int64(size) != 0 {
				vm.ThrowRangeError("byte length of " + name + " should be a multiple of " + itoa(int64(size)))
			}
			newByteLength = bl - offset
			if newByteLength < 0 {
				vm.ThrowRangeError("start offset " + itoa(offset) + " is outside the bounds of the buffer")
			}
		} else {
			newByteLength = newLength * int64(size)
			if offset+newByteLength > bl {
				vm.ThrowRangeError("invalid typed array length: " + itoa(newLength))
			}
		}
		return engine.ObjectValue(vm.NewTypedArrayObject(proto, typ, first.Object(), int(offset), int(newByteLength/int64(size))))
	}
	if m := vm.GetMethod(first, vm.WellKnownSymbol(engine.SymbolIterator)); m != nil {
		vals := vm.IteratorToList(first, m)
		o := bi.newTypedArray(vm, proto, typ, int64(len(vals)))
		t := engine.TypedArrayData(engine.ObjectValue(o))
		for i := storeNumberValues(t, vals); i < len(vals); i++ {
			storeAt(vm, o, t, i, vals[i])
		}
		return engine.ObjectValue(o)
	}
	src := first.Object()
	n := vm.LengthOfArrayLike(src)
	o := bi.newTypedArray(vm, proto, typ, n)
	t := engine.TypedArrayData(engine.ObjectValue(o))
	for i := range n {
		storeAt(vm, o, t, int(i), getAt(vm, src, i))
	}
	return engine.ObjectValue(o)
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
func searchElements(b []byte, typ engine.ElementType, x engine.Value, from, to int, backward, sameValueZero bool) int {
	if typ.IsBigInt() != x.IsBigInt() || !typ.IsBigInt() && !x.IsNumber() {
		return -1
	}
	if !typ.IsBigInt() {
		isFloat := typ == engine.ElementFloat16 || typ == engine.ElementFloat32 || typ == engine.ElementFloat64
		// A number other than NaN, and other than a zero for a floating-point type, has at
		// most one encoding, which the elements equal to it have.
		if f := x.Number(); f == f && (f != 0 || !isFloat) {
			bits := engine.ElementBits(typ, x)
			if engine.ElementNumber(typ, bits) != f {
				return -1
			}
			return searchBits(b, typ, bits, from, to, backward)
		}
	}
	size := typ.Size()
	match := func(i int) bool {
		bits := engine.LoadElementBits(b[i*size:], typ)
		switch {
		case typ == engine.ElementBigInt64:
			v, ok := x.BigInt64()
			return ok && v == int64(bits)
		case typ == engine.ElementBigUint64:
			if v, ok := x.BigInt64(); ok {
				return v >= 0 && uint64(v) == bits
			}
			v := x.BigInt()
			return v.IsUint64() && v.Uint64() == bits
		}
		f := engine.ElementNumber(typ, bits)
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
func searchBits(b []byte, typ engine.ElementType, bits uint64, from, to int, backward bool) int {
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
			if engine.LoadElementBits(b[i*size:], typ) == bits {
				return i
			}
		}
		return -1
	}
	for i := from; i < to; i++ {
		if engine.LoadElementBits(b[i*size:], typ) == bits {
			return i
		}
	}
	return -1
}

// fillElements stores the encoding x of an element of the type typ in every element of b.
func fillElements(b []byte, typ engine.ElementType, x uint64) {
	size := typ.Size()
	if len(b) < size {
		return
	}
	engine.StoreElementBits(b, typ, x)
	for n := size; n < len(b); n *= 2 {
		copy(b[n:], b[:n])
	}
}

func initTypedArrays(vm *engine.VM, bi *binaryIntrinsics) {
	proto := vm.NewOrdinaryObject()
	bi.typedArrayProto = proto
	ctor := vm.NewNativeConstructor("TypedArray", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("abstract class TypedArray not directly constructable")
		return engine.Undefined
	})
	bi.typedArrayCtor = ctor
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	speciesGetter(vm, ctor)

	method(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() || !vm.IsConstructor(this.Object()) {
			vm.ThrowTypeError(vm.DisplayString(this) + " is not a constructor")
		}
		c := this.Object()
		source, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
		if !mapFn.IsUndefined() && !mapFn.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(mapFn) + " is not a function")
		}
		mapped := func(v engine.Value, i int) engine.Value {
			if mapFn.IsUndefined() {
				return v
			}
			return vm.Call(mapFn.Object(), thisArg, v, engine.Number(float64(i)))
		}
		if m := vm.GetMethod(source, vm.WellKnownSymbol(engine.SymbolIterator)); m != nil {
			vals := vm.IteratorToList(source, m)
			o, t := createFromConstructor(vm, c, []engine.Value{engine.Number(float64(len(vals)))}, true)
			var i int
			if mapFn.IsUndefined() {
				i = storeNumberValues(t, vals)
			}
			for ; i < len(vals); i++ {
				storeAt(vm, o, t, i, mapped(vals[i], i))
			}
			return engine.ObjectValue(o)
		}
		src := vm.ToObject(source)
		n := vm.LengthOfArrayLike(src)
		o, t := createFromConstructor(vm, c, []engine.Value{engine.Number(float64(n))}, true)
		for i := range n {
			storeAt(vm, o, t, int(i), mapped(getAt(vm, src, i), int(i)))
		}
		return engine.ObjectValue(o)
	})
	method(vm, ctor, "of", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() || !vm.IsConstructor(this.Object()) {
			vm.ThrowTypeError(vm.DisplayString(this) + " is not a constructor")
		}
		o, t := createFromConstructor(vm, this.Object(), []engine.Value{engine.Number(float64(len(args)))}, true)
		for i, v := range args {
			storeAt(vm, o, t, i, v)
		}
		return engine.ObjectValue(o)
	})

	thisTypedArray := func(vm *engine.VM, this engine.Value, what string) *engine.TypedArray {
		t := engine.TypedArrayData(this)
		if t == nil {
			vm.ThrowTypeError("%TypedArray%.prototype." + what + ": this is not a typed array")
		}
		return t
	}
	getter(vm, proto, vm.Intern("buffer"), "buffer", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(thisTypedArray(vm, this, "buffer").Buffer())
	})
	getter(vm, proto, vm.Intern("byteLength"), "byteLength", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisTypedArray(vm, this, "byteLength")
		n, _ := t.Length()
		return engine.Number(float64(n * t.ElementType().Size()))
	})
	getter(vm, proto, vm.Intern("byteOffset"), "byteOffset", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisTypedArray(vm, this, "byteOffset")
		if _, ok := t.Length(); !ok {
			return engine.Number(0)
		}
		return engine.Number(float64(t.ByteOffset()))
	})
	lengthGetter := vm.NewNative("get length", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n, _ := thisTypedArray(vm, this, "length").Length()
		return engine.Number(float64(n))
	})
	vm.SetIntrinsic(engine.IntrinsicTypedArrayPrototypeLengthGetter, lengthGetter)
	vm.DefineAccessor(proto, vm.Key(engine.KeyLength), lengthGetter, nil, engine.FlagConfigurable)
	getter(vm, proto, vm.WellKnownSymbol(engine.SymbolToStringTag), "[Symbol.toStringTag]", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := engine.TypedArrayData(this)
		if t == nil {
			return engine.Undefined
		}
		return vm.NewString(wtf8.String(typedArrayNames[t.ElementType()]))
	})

	method(vm, proto, "at", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype.at")
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 {
			i += float64(n)
		}
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		return getAt(vm, o, int64(i))
	})
	method(vm, proto, "copyWithin", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateWritableTypedArray(vm, this, "%TypedArray%.prototype.copyWithin")
		target := engine.RelativeIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		start, end := relativeRange(vm, arg(args, 1), arg(args, 2), n)
		count := min(end-start, n-target)
		if count > 0 {
			n, ok := t.Length()
			if !ok {
				vm.ThrowTypeError("%TypedArray%.prototype.copyWithin: the typed array is detached or out of bounds")
			}
			size := t.ElementType().Size()
			limit := n * size
			from, to := start*size, target*size
			if c := min(count*size, limit-from, limit-to); c > 0 {
				copyBytesWithin(t, to, from, c)
			}
		}
		return engine.ObjectValue(o)
	})
	iterMethod := func(name string, kind engine.IterationKind) *engine.Object {
		return method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o, _, _ := validateTypedArray(vm, this, "%TypedArray%.prototype."+name)
			return engine.ObjectValue(vm.NewArrayIterator(o, kind))
		})
	}
	iterMethod("entries", engine.IterateEntries)
	iterMethod("keys", engine.IterateKeys)
	values := iterMethod("values", engine.IterateValues)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(values), engine.FlagsHidden)

	// predicate defines the methods that call a callback for the elements in order until it returns true.
	predicate := func(name string, backward bool, result func(found bool, i int, v engine.Value) engine.Value) {
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype."+name)
			f := callback(vm, args, name)
			thisArg := arg(args, 1)
			for j := range n {
				i := j
				if backward {
					i = n - 1 - j
				}
				v := getAt(vm, o, int64(i))
				if engine.ToBoolean(vm.Call(f, thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o))) {
					return result(true, i, v)
				}
			}
			return result(false, -1, engine.Undefined)
		})
	}
	predicate("find", false, func(found bool, i int, v engine.Value) engine.Value {
		return v
	})
	predicate("findIndex", false, func(found bool, i int, v engine.Value) engine.Value {
		return engine.Number(float64(i))
	})
	predicate("findLast", true, func(found bool, i int, v engine.Value) engine.Value {
		return v
	})
	predicate("findLastIndex", true, func(found bool, i int, v engine.Value) engine.Value {
		return engine.Number(float64(i))
	})
	predicate("some", false, func(found bool, i int, v engine.Value) engine.Value {
		return engine.Bool(found)
	})
	// every stops at the first false result instead.
	method(vm, proto, "every", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype.every")
		f := callback(vm, args, "every")
		thisArg := arg(args, 1)
		for i := range n {
			v := getAt(vm, o, int64(i))
			if !engine.ToBoolean(vm.Call(f, thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o))) {
				return engine.False
			}
		}
		return engine.True
	})
	method(vm, proto, "forEach", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype.forEach")
		f := callback(vm, args, "forEach")
		thisArg := arg(args, 1)
		for i := range n {
			vm.Call(f, thisArg, getAt(vm, o, int64(i)), engine.Number(float64(i)), engine.ObjectValue(o))
		}
		return engine.Undefined
	})
	method(vm, proto, "fill", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateWritableTypedArray(vm, this, "%TypedArray%.prototype.fill")
		typ := t.ElementType()
		v := vm.ToElementValue(typ, arg(args, 0))
		start, end := relativeRange(vm, arg(args, 1), arg(args, 2), n)
		cur, ok := t.Length()
		if !ok {
			vm.ThrowTypeError("%TypedArray%.prototype.fill: the typed array is detached or out of bounds")
		}
		end = min(end, cur)
		if start < end {
			fillRange(vm, t, start, end, engine.ElementBits(typ, v))
		}
		return engine.ObjectValue(o)
	})
	method(vm, proto, "filter", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.filter")
		f := callback(vm, args, "filter")
		thisArg := arg(args, 1)
		var kept []engine.Value
		for i := range n {
			v := getAt(vm, o, int64(i))
			if engine.ToBoolean(vm.Call(f, thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o))) {
				kept = append(kept, v)
			}
		}
		a, at := bi.speciesCreate(vm, o, t, []engine.Value{engine.Number(float64(len(kept)))}, true)
		for i, v := range kept {
			storeAt(vm, a, at, i, v)
		}
		return engine.ObjectValue(a)
	})
	search := func(name string, backward, sameValueZero bool) {
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			notFound := engine.Number(-1)
			if sameValueZero {
				notFound = engine.False
			}
			_, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype."+name)
			if n == 0 {
				return notFound
			}
			x := arg(args, 0)
			var from, to int
			if backward {
				from = n - 1
				if len(args) > 1 {
					f := vm.ToIntegerOrInfinity(args[1])
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
				f := vm.ToIntegerOrInfinity(arg(args, 1))
				if math.IsInf(f, 1) {
					return notFound
				}
				from = engine.RelativeIndex(f, n)
				to = n
			}
			// The conversion of the index may have shrunk the buffer: the elements beyond its end are undefined.
			cur, _ := t.Length()
			if sameValueZero && x.IsUndefined() && from < to && to > cur {
				return engine.True
			}
			if backward {
				from = min(from, cur-1)
			} else {
				to = min(to, cur)
			}
			i := searchRange(vm, t, x, from, to, backward, sameValueZero)
			if sameValueZero {
				return engine.Bool(i >= 0)
			}
			return engine.Number(float64(i))
		})
	}
	search("includes", false, true)
	search("indexOf", false, false)
	search("lastIndexOf", true, false)
	method(vm, proto, "join", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype.join")
		sep := wtf8.String(",")
		if s := arg(args, 0); !s.IsUndefined() {
			sep = vm.ToString(s)
		}
		var b wtf8.Buffer
		for i := range n {
			if i > 0 {
				b.WriteString(sep)
			}
			if e := getAt(vm, o, int64(i)); !e.IsUndefined() {
				writeString(vm, &b, e)
			}
			if b.Len() > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "map", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.map")
		f := callback(vm, args, "map")
		thisArg := arg(args, 1)
		a, at := bi.speciesCreate(vm, o, t, []engine.Value{engine.Number(float64(n))}, true)
		for i := range n {
			v := vm.Call(f, thisArg, getAt(vm, o, int64(i)), engine.Number(float64(i)), engine.ObjectValue(o))
			storeAt(vm, a, at, i, v)
		}
		return engine.ObjectValue(a)
	})
	reduce := func(name string, backward bool) {
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype."+name)
			f := callback(vm, args, name)
			index := func(j int) int {
				if backward {
					return n - 1 - j
				}
				return j
			}
			j := 0
			var acc engine.Value
			if len(args) > 1 {
				acc = args[1]
			} else {
				if n == 0 {
					vm.ThrowTypeError("reduce of empty array with no initial value")
				}
				acc = getAt(vm, o, int64(index(0)))
				j = 1
			}
			for ; j < n; j++ {
				i := index(j)
				acc = vm.Call(f, engine.Undefined, acc, getAt(vm, o, int64(i)), engine.Number(float64(i)), engine.ObjectValue(o))
			}
			return acc
		})
	}
	reduce("reduce", false)
	reduce("reduceRight", true)
	method(vm, proto, "reverse", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateWritableTypedArray(vm, this, "%TypedArray%.prototype.reverse")
		reverseFirst(vm, t, n)
		return engine.ObjectValue(o)
	})
	method(vm, proto, "set", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := engine.TypedArrayData(this)
		if t == nil {
			vm.ThrowTypeError("%TypedArray%.prototype.set: this is not a typed array")
		}
		if t.ArrayBuffer().IsImmutable() {
			vm.ThrowTypeError("%TypedArray%.prototype.set: the typed array is backed by an immutable ArrayBuffer")
		}
		offset := vm.ToIntegerOrInfinity(arg(args, 1))
		if offset < 0 {
			vm.ThrowRangeError("offset is out of bounds")
		}
		bi.setFrom(vm, this.Object(), t, offset, arg(args, 0))
		return engine.Undefined
	})
	method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.slice")
		start, end := relativeRange(vm, arg(args, 0), arg(args, 1), n)
		count := max(end-start, 0)
		a, at := bi.speciesCreate(vm, o, t, []engine.Value{engine.Number(float64(count))}, true)
		if count == 0 {
			return engine.ObjectValue(a)
		}
		cur, ok := t.Length()
		if !ok {
			vm.ThrowTypeError("%TypedArray%.prototype.slice: the typed array is detached or out of bounds")
		}
		end = min(end, cur)
		count = max(end-start, 0)
		typ := t.ElementType()
		if typ != at.ElementType() {
			for i := range count {
				storeAt(vm, a, at, i, getAt(vm, o, int64(start+i)))
			}
			return engine.ObjectValue(a)
		}
		size := typ.Size()
		src, dst := start*size+t.ByteOffset(), at.ByteOffset()
		if at.ArrayBuffer().SameDataBlock(t.ArrayBuffer()) && src < dst && dst < src+count*size {
			// The bytes are copied one at a time from the first on, as the specification says.
			copyBytesForward(vm, t.ArrayBuffer(), dst, src, count*size)
		} else {
			engine.CopyBufferBytes(at.ArrayBuffer(), dst, t.ArrayBuffer(), src, count*size)
		}
		return engine.ObjectValue(a)
	})
	method(vm, proto, "sort", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cmp := arg(args, 0)
		if !cmp.IsUndefined() && !cmp.IsCallable() {
			vm.ThrowTypeError("the comparison function must be either a function or undefined")
		}
		o, t, n := validateWritableTypedArray(vm, this, "%TypedArray%.prototype.sort")
		if cmp.IsUndefined() {
			sortFirst(vm, t, n)
			return engine.ObjectValue(o)
		}
		vals := make([]engine.Value, n)
		for i := range vals {
			vals[i] = getAt(vm, o, int64(i))
		}
		sortValues(vm, vals, cmp)
		for i, v := range vals {
			storeAt(vm, o, t, i, v)
		}
		return engine.ObjectValue(o)
	})
	method(vm, proto, "subarray", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := engine.TypedArrayData(this)
		if t == nil {
			vm.ThrowTypeError("%TypedArray%.prototype.subarray: this is not a typed array")
		}
		n, _ := t.Length()
		start := engine.RelativeIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		size := t.ElementType().Size()
		begin := engine.Number(float64(t.ByteOffset() + start*size))
		buf := engine.ObjectValue(t.Buffer())
		var a *engine.Object
		if end := arg(args, 1); t.IsLengthTracking() && end.IsUndefined() {
			a, _ = bi.speciesCreate(vm, this.Object(), t, []engine.Value{buf, begin}, false)
		} else {
			e := n
			if !end.IsUndefined() {
				e = engine.RelativeIndex(vm.ToIntegerOrInfinity(end), n)
			}
			a, _ = bi.speciesCreate(vm, this.Object(), t, []engine.Value{buf, begin, engine.Number(float64(max(e-start, 0)))}, false)
		}
		return engine.ObjectValue(a)
	})
	toLocaleStringKey := vm.Intern("toLocaleString")
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, _, n := validateTypedArray(vm, this, "%TypedArray%.prototype.toLocaleString")
		var b wtf8.Buffer
		for i := range n {
			if i > 0 {
				b.WriteString(",")
			}
			if e := getAt(vm, o, int64(i)); !e.IsNullish() {
				b.WriteString(vm.ToString(vm.CallValue(vm.GetV(e, toLocaleStringKey), e)))
			}
			if b.Len() > engine.MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "toReversed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		_, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.toReversed")
		a, at := bi.createSameType(vm, t, n)
		copyElements(at, 0, t, n)
		reverseFirst(vm, at, n)
		return engine.ObjectValue(a)
	})
	method(vm, proto, "toSorted", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cmp := arg(args, 0)
		if !cmp.IsUndefined() && !cmp.IsCallable() {
			vm.ThrowTypeError("the comparison function must be either a function or undefined")
		}
		o, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.toSorted")
		a, at := bi.createSameType(vm, t, n)
		if cmp.IsUndefined() {
			copyElements(at, 0, t, n)
			sortFirst(vm, at, n)
			return engine.ObjectValue(a)
		}
		vals := make([]engine.Value, n)
		for i := range vals {
			vals[i] = getAt(vm, o, int64(i))
		}
		sortValues(vm, vals, cmp)
		for i, v := range vals {
			storeAt(vm, a, at, i, v)
		}
		return engine.ObjectValue(a)
	})
	method(vm, proto, "with", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, t, n := validateTypedArray(vm, this, "%TypedArray%.prototype.with")
		rel := vm.ToIntegerOrInfinity(arg(args, 0))
		idx := rel
		if rel < 0 {
			idx = float64(n) + rel
		}
		typ := t.ElementType()
		v := vm.ToElementValue(typ, arg(args, 1))
		cur, _ := t.Length()
		if idx < 0 || idx >= float64(cur) {
			vm.ThrowRangeError("invalid typed array index")
		}
		a, at := bi.createSameType(vm, t, n)
		copied := min(n, cur)
		copyElements(at, 0, t, copied)
		for i := copied; i < n; i++ {
			storeAt(vm, a, at, i, getAt(vm, o, int64(i)))
		}
		storeAt(vm, a, at, int(idx), v)
		return engine.ObjectValue(a)
	})
	toString := vm.Get(vm.Intrinsic(engine.IntrinsicArrayPrototype), engine.NamedKey(vm.Key(engine.KeyToString)))
	vm.Define(proto, vm.Key(engine.KeyToString), toString, engine.FlagsHidden)

	for typ := range engine.ElementTypeCount {
		name := typedArrayNames[typ]
		p := vm.NewObject(proto)
		vm.SetIntrinsic(engine.IntrinsicInt8ArrayPrototype+engine.Intrinsic(typ), p)
		c := vm.NewNativeConstructor(wtf8.String(name), 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return bi.construct(vm, typ, args)
		})
		vm.SetPrototypeOf(c, ctor)
		vm.Define(c, vm.Key(engine.KeyPrototype), engine.ObjectValue(p), 0)
		vm.Define(p, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
		vm.Define(vm.GlobalObject().Object(), vm.Intern(wtf8.String(name)), engine.ObjectValue(c), engine.FlagsHidden)
		bpe := engine.Number(float64(typ.Size()))
		constant(vm, c, "BYTES_PER_ELEMENT", bpe)
		constant(vm, p, "BYTES_PER_ELEMENT", bpe)
		bi.ctors[typ] = c
		bi.protos[typ] = p
	}
	initUint8ArrayBase64(vm, bi)
}

// setFrom implements the part of %TypedArray%.prototype.set after the offset is converted.
func (bi *binaryIntrinsics) setFrom(vm *engine.VM, target *engine.Object, t *engine.TypedArray, offset float64, source engine.Value) {
	typ := t.ElementType()
	if src := engine.TypedArrayData(source); src != nil {
		targetLen, ok := t.Length()
		if !ok {
			vm.ThrowTypeError("%TypedArray%.prototype.set: the typed array is detached or out of bounds")
		}
		srcLen, ok := src.Length()
		if !ok {
			vm.ThrowTypeError("%TypedArray%.prototype.set: the source typed array is detached or out of bounds")
		}
		if math.IsInf(offset, 1) || float64(srcLen)+offset > float64(targetLen) {
			vm.ThrowRangeError("offset is out of bounds")
		}
		srcType := src.ElementType()
		if typ.IsBigInt() != srcType.IsBigInt() {
			vm.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
		}
		if srcType == typ && src.ArrayBuffer().SameDataBlock(t.ArrayBuffer()) {
			copyElements(t, int(offset), src, srcLen)
			return
		}
		sb := elementsOf(src)
		if src.ArrayBuffer() == t.ArrayBuffer() && !src.ArrayBuffer().IsShared() {
			// Conversion needs the original elements when the views overlap.
			sb = slices.Clone(sb)
		}
		storeConverted(vm, t, int(offset), sb, srcType, srcLen)
		return
	}
	targetLen, ok := t.Length()
	if !ok {
		vm.ThrowTypeError("%TypedArray%.prototype.set: the typed array is detached or out of bounds")
	}
	src := vm.ToObject(source)
	srcLen := vm.LengthOfArrayLike(src)
	if math.IsInf(offset, 1) || float64(srcLen)+offset > float64(targetLen) {
		vm.ThrowRangeError("offset is out of bounds")
	}
	off := int(offset)
	if !typ.IsBigInt() {
		// The elements that are numbers that reading runs no JavaScript for are stored directly.
		// Reading the elements of a typed array on a shared buffer is never direct, so it takes
		// no lock.
		i := storeNumbers(vm, t, off, src, srcLen)
		for ; i < srcLen; i++ {
			storeAt(vm, target, t, off+int(i), getAt(vm, src, i))
		}
		return
	}
	for i := range srcLen {
		storeAt(vm, target, t, off+int(i), getAt(vm, src, i))
	}
}
