// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// maxArrayFromPresize is the largest number of elements that Array.from reserves for the values
// of an iterator before it has them.
const maxArrayFromPresize = 1 << 16

// bigKey returns the property key for an integer index, which may be beyond the array indices.
func bigKey(cx *engine.ExecutionContext, i int64) storage.PropKey {
	if i >= 0 && i <= storage.MaxArrayIndex {
		return storage.IndexKey(uint32(i))
	}
	return storage.NamedKey(cx.Intern(numconv.NumberToString(float64(i))))
}

// getAt returns the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func getAt(cx *engine.ExecutionContext, o *storage.Object, i int64) storage.Value {
	if e, ok := storage.Element(o, i); ok {
		cx.PollAt(i)
		return e
	}
	cx.Poll()
	return cx.Get(o, bigKey(cx, i))
}

// hasAt reports whether o has a property with the integer index i. A loop over consecutive
// indices that calls it checks for an interruption regularly.
func hasAt(cx *engine.ExecutionContext, o *storage.Object, i int64) bool {
	if _, ok := storage.Element(o, i); ok {
		cx.PollAt(i)
		return true
	}
	cx.Poll()
	return cx.HasProperty(o, bigKey(cx, i))
}

// skipHoles returns an index from i toward end, which is excluded, such that o has no property from i
// up to it, or end.
func skipHoles(cx *engine.ExecutionContext, o *storage.Object, i, end int64, backward bool) int64 {
	if backward {
		return cx.Quiet().SkipHolesBackward(o, i, end)
	}
	return cx.Quiet().SkipHoles(o, i, end)
}

// holeSteps returns a number of consecutive indices from i, moving by dir (1 or -1) and at most
// count, at which neither o nor its prototypes have a property.
func holeSteps(cx *engine.ExecutionContext, o *storage.Object, i, count, dir int64) int64 {
	if _, ok := storage.Element(o, i); ok || i > storage.MaxArrayIndex {
		return 0
	}
	if dir > 0 {
		count = min(count, storage.MaxArrayIndex+1-i)
		return cx.Quiet().SkipHoles(o, i, i+count) - i
	}
	return i - cx.Quiet().SkipHolesBackward(o, i, i-count)
}

// minMoveRun is the number of steps of the first look ahead of moveSteps.
const minMoveRun = 256

// moveSteps returns a number of steps, at most count, of a loop that moves the property at src to dst,
// or deletes the property at dst when src has none, that change nothing. Both indices move by dir (1
// or -1).
func moveSteps(cx *engine.ExecutionContext, o *storage.Object, src, dst, count, dir int64) int64 {
	// A long run of holes at one index can meet a property at the other, so each look ahead is
	// limited in proportion to the steps found so far.
	n := int64(0)
	for run := int64(minMoveRun); n < count; run *= 8 {
		c := min(run, count-n)
		m := holeSteps(cx, o, src+dir*n, c, dir)
		if m > 0 {
			m = holeSteps(cx, o, dst+dir*n, m, dir)
		}
		n += m
		if m < c {
			break
		}
	}
	return n
}

// putAt sets the property of o with the integer index i to v. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func putAt(cx *engine.ExecutionContext, o *storage.Object, i int64, v storage.Value) {
	if o.SetElement(i, v) {
		cx.PollAt(i)
		return
	}
	cx.Poll()
	cx.Set(o, bigKey(cx, i), v)
}

// deleteAt deletes the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func deleteAt(cx *engine.ExecutionContext, o *storage.Object, i int64) {
	cx.Poll()
	k := bigKey(cx, i)
	if !cx.DeleteProp(o, k) {
		cx.ThrowTypeError("cannot delete property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

func setLength(cx *engine.ExecutionContext, o *storage.Object, n int64) {
	cx.Set(o, storage.NamedKey(cx.Key(storage.KeyLength)), storage.Number(float64(n)))
}

// arraySpeciesCreate implements ArraySpeciesCreate.
func arraySpeciesCreate(cx *engine.ExecutionContext, orig *storage.Object, n int64) *storage.Object {
	if a, ok := cx.Quiet().ArraySpeciesCreatePlain(orig, n); ok {
		return a
	}
	if cx.IsArray(storage.ObjectValue(orig)) {
		c := cx.GetV(storage.ObjectValue(orig), cx.Key(storage.KeyConstructor))
		if c.IsObject() {
			if c.IsCallable() && cx.IsConstructor(c.Object()) && cx.IsIntrinsicOfOtherRealm(c.Object(), storage.IntrinsicArray) {
				c = storage.Undefined
			} else if s := cx.GetV(c, cx.WellKnownSymbol(storage.SymbolSpecies)); s.IsNull() {
				c = storage.Undefined
			} else {
				c = s
			}
		}
		if !c.IsUndefined() && !(c.IsObject() && c.Object() == cx.Intrinsic(storage.IntrinsicArray)) {
			if !c.IsCallable() || !cx.IsConstructor(c.Object()) {
				cx.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
			}
			return cx.Construct(c.Object(), []storage.Value{storage.Number(float64(n))}, c.Object())
		}
	}
	if n > storage.MaxArrayIndex+1 {
		cx.ThrowRangeError("invalid array length")
	}
	return cx.ArrayCreate(uint32(n), cx.Intrinsic(storage.IntrinsicArrayPrototype))
}

func callback(cx *engine.ExecutionContext, args []storage.Value, what string) *storage.Object {
	f := arg(args, 0)
	if !f.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(f) + " is not a function")
	}
	return f.Object()
}

// joinArray implements Array.prototype.join for the first n elements of the object o.
func joinArray(cx *engine.ExecutionContext, o *storage.Object, n int64, sep wtf8.String) storage.Value {
	if !cx.EnterJoin(o) {
		return cx.NewString("")
	}
	defer cx.LeaveJoin()
	if v, ok := cx.Quiet().JoinStrings(o, n, sep); ok {
		return v
	}
	return joinElements(cx, o, n, sep, nil)
}

// writeString writes ToString(v) to b.
func writeString(cx *engine.ExecutionContext, b *wtf8.Buffer, v storage.Value) {
	if v.IsString() {
		v.WriteStringTo(b)
		return
	}
	if v.IsNumber() {
		var buf [32]byte
		b.WriteStringBytes(numconv.AppendNumber(buf[:0], v.Number()))
		return
	}
	b.WriteString(cx.ToString(v))
}

// joinElements joins the strings of the first n elements of the object o that are neither
// undefined nor null, with sep between elements. The string of an element e is ToString(e),
// or ToString(Invoke(e, method)) when method is not nil.
func joinElements(cx *engine.ExecutionContext, o *storage.Object, n int64, sep wtf8.String, method *storage.Object) storage.Value {
	var b wtf8.Buffer
	for i := int64(0); i < n; {
		if i > 0 {
			cx.Poll()
		}
		i = joinRun(cx, &b, o, i, min(n, i+storage.PollInterval), n, sep, method)
	}
	return cx.NewString(b.String())
}

// joinRun writes the elements of o from i up to end, or further when holes follow, to b as
// joinElements joins the first n elements, and returns the index after the last one it wrote.
func joinRun(cx *engine.ExecutionContext, b *wtf8.Buffer, o *storage.Object, i, end, n int64, sep wtf8.String, method *storage.Object) int64 {
	for ; i < end; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		e, ok := storage.Element(o, i)
		if !ok {
			if j := cx.Quiet().SkipHoles(o, i, n); j > i {
				// The indices from i to j-1 read as undefined.
				writeSeparators(cx, b, sep, j-1-i)
				i = j - 1
				e = storage.Undefined
			} else {
				e = getAt(cx, o, i)
			}
		}
		switch {
		case e.IsNullish():
		case method != nil:
			b.WriteString(cx.ToString(cx.CallValue(cx.GetV(e, method), e)))
		default:
			writeString(cx, b, e)
		}
		if b.Len() > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	}
	return i
}

// writeSeparators writes sep count times to b, and throws a RangeError instead when b would get
// longer than MaxStringLength.
func writeSeparators(cx *engine.ExecutionContext, b *wtf8.Buffer, sep wtf8.String, count int64) {
	// A separator after another one adds len(sep) bytes, or len(sep)-2 when the two join a
	// surrogate pair. The first one may also join a pair with what b ends with.
	per := int64(len(sep))
	if wtf8.JoinsPair(sep, sep) {
		per -= 2
	}
	if int64(b.Len())+count*per-2 > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	writeRepeated(cx, b, sep, int(count))
	if b.Len() > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// flattenIntoArray implements FlattenIntoArray for the first l elements of src without a mapper function.
// The nested arrays are kept on an explicit stack, as their depth is unbounded.
func flattenIntoArray(cx *engine.ExecutionContext, a *storage.Object, src *storage.Object, l int64, depth float64, n *int64) {
	type level struct {
		src   *storage.Object
		i, l  int64
		depth float64
	}
	stack := []level{{
		src:   src,
		l:     l,
		depth: depth,
	}}
	var pacer engine.Pacer
	for len(stack) > 0 {
		cx.PollStep()
		t := &stack[len(stack)-1]
		if i := cx.Quiet().SkipHoles(t.src, t.i, t.l); i > t.i {
			pacer.Steps(cx, int(min(i-t.i, storage.PollInterval)))
			t.i = i
		}
		if t.i >= t.l {
			stack = stack[:len(stack)-1]
			continue
		}
		i := t.i
		t.i++
		if !hasAt(cx, t.src, i) {
			continue
		}
		v := getAt(cx, t.src, i)
		if t.depth > 0 && cx.IsArray(v) {
			stack = append(stack, level{
				src:   v.Object(),
				l:     cx.LengthOfArrayLike(v.Object()),
				depth: t.depth - 1,
			})
			continue
		}
		cx.CreateDataPropertyOrThrow(a, bigKey(cx, *n), v)
		*n++
	}
}

var (
	nativeArrayConstructor = engine.RegisterNative(arrayConstructor)
	nativeArrayIsArray     = engine.RegisterNativeWithKernels(arrayIsArray, engine.Kernels{
		Method1:  quiet.ArrayIsArrayKernel,
		Contract: shortContract,
	})
	nativeArrayOf   = engine.RegisterNative(arrayOf)
	nativeArrayFrom = engine.RegisterNative(arrayFrom)
	nativeArrayPush = engine.RegisterNativeWithKernels(arrayPush, engine.Kernels{
		Method1:  engine.ArrayPushKernel,
		Contract: polledContract,
	})
	nativeArrayPop = engine.RegisterNativeWithKernels(arrayPop, engine.Kernels{
		Method0:  quiet.ArrayPopKernel,
		Contract: shortContract,
	})
	nativeArrayShift   = engine.RegisterNative(arrayShift)
	nativeArrayUnshift = engine.RegisterNative(arrayUnshift)
	nativeArraySlice   = engine.RegisterNativeWithKernels(arraySlice, engine.Kernels{
		Method0:  quiet.ArraySliceKernel0,
		Method1:  quiet.ArraySliceKernel1,
		Method2:  quiet.ArraySliceKernel2,
		Contract: shortContract,
	})
	nativeArraySplice    = engine.RegisterNative(arraySplice)
	nativeArrayConcat    = engine.RegisterNative(arrayConcat)
	nativeArrayJoin      = engine.RegisterNative(arrayJoin)
	nativeArrayToString  = engine.RegisterNative(arrayToString)
	nativeArrayReverse   = engine.RegisterNative(arrayReverse)
	nativeArrayForEach   = engine.RegisterNative(arrayForEach)
	nativeArrayMap       = engine.RegisterNative(arrayMap)
	nativeArrayFilter    = engine.RegisterNative(arrayFilter)
	nativeArraySome      = engine.RegisterNative(arraySome)
	nativeArrayEvery     = engine.RegisterNative(arrayEvery)
	nativeArrayFind      = engine.RegisterNative(arrayFind)
	nativeArrayFindIndex = engine.RegisterNative(arrayFindIndex)
	nativeArrayFill      = engine.RegisterNative(arrayFill)
	nativeArrayAt        = engine.RegisterNative(arrayAt)
	nativeArraySort      = engine.RegisterNative(arraySort)
	nativeArrayFlat      = engine.RegisterNative(arrayFlat)
	nativeArrayFlatMap   = engine.RegisterNative(arrayFlatMap)
	nativeArrayKeys      = engine.RegisterNative(arrayKeys)
	nativeArrayEntries   = engine.RegisterNative(arrayEntries)
	nativeArrayValues    = engine.RegisterNative(arrayValues)
)

func initArray(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(storage.IntrinsicArrayPrototype)
	ctor := constructor(cx, "Array", 1, storage.IntrinsicArrayPrototype, nativeArrayConstructor)
	cx.SetIntrinsic(storage.IntrinsicArray, ctor)
	speciesGetter := cx.NewNative("get [Symbol.species]", 0, nativeReturnThis)
	cx.SetIntrinsic(storage.IntrinsicArraySpeciesGetter, speciesGetter)
	cx.DefineAccessor(ctor, cx.WellKnownSymbol(storage.SymbolSpecies), speciesGetter, nil, storage.FlagConfigurable)

	method(cx, ctor, "isArray", 1, nativeArrayIsArray)
	method(cx, ctor, "of", 0, nativeArrayOf)
	method(cx, ctor, "from", 1, nativeArrayFrom)

	cx.SetIntrinsic(storage.IntrinsicArrayPrototypePush, method(cx, proto, "push", 1, nativeArrayPush))
	cx.SetIntrinsic(storage.IntrinsicArrayPrototypePop, method(cx, proto, "pop", 0, nativeArrayPop))
	method(cx, proto, "shift", 0, nativeArrayShift)
	method(cx, proto, "unshift", 1, nativeArrayUnshift)
	method(cx, proto, "slice", 2, nativeArraySlice)
	method(cx, proto, "splice", 2, nativeArraySplice)
	method(cx, proto, "concat", 1, nativeArrayConcat)
	method(cx, proto, "join", 1, nativeArrayJoin)
	cx.SetIntrinsic(storage.IntrinsicArrayPrototypeToString, method(cx, proto, "toString", 0, nativeArrayToString))
	method(cx, proto, "reverse", 0, nativeArrayReverse)

	method(cx, proto, "indexOf", 1, nativeArrayIndexOf)
	method(cx, proto, "lastIndexOf", 1, nativeArrayLastIndexOf)
	method(cx, proto, "includes", 1, nativeArrayIncludes)

	method(cx, proto, "forEach", 1, nativeArrayForEach)
	method(cx, proto, "map", 1, nativeArrayMap)
	method(cx, proto, "filter", 1, nativeArrayFilter)
	method(cx, proto, "some", 1, nativeArraySome)
	method(cx, proto, "every", 1, nativeArrayEvery)
	method(cx, proto, "find", 1, nativeArrayFind)
	method(cx, proto, "findIndex", 1, nativeArrayFindIndex)
	method(cx, proto, "findLast", 1, nativeArrayFindLast)
	method(cx, proto, "findLastIndex", 1, nativeArrayFindLastIndex)

	method(cx, proto, "reduce", 1, nativeArrayReduce)
	method(cx, proto, "reduceRight", 1, nativeArrayReduceRight)

	method(cx, proto, "fill", 1, nativeArrayFill)
	method(cx, proto, "at", 1, nativeArrayAt)
	method(cx, proto, "sort", 1, nativeArraySort)
	method(cx, proto, "flat", 0, nativeArrayFlat)
	method(cx, proto, "flatMap", 1, nativeArrayFlatMap)

	method(cx, proto, "keys", 0, nativeArrayKeys)
	method(cx, proto, "entries", 0, nativeArrayEntries)
	cx.SetIntrinsic(storage.IntrinsicArrayPrototypeValues, method(cx, proto, "values", 0, nativeArrayValues))
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolIterator), storage.ObjectValue(cx.Intrinsic(storage.IntrinsicArrayPrototypeValues)), storage.FlagsHidden)
	initArrayAdditions(cx, proto)
}

func arrayConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	proto := cx.Intrinsic(storage.IntrinsicArrayPrototype)
	if nt := cx.GetNewTarget(); nt != nil && nt != cx.Intrinsic(storage.IntrinsicArray) {
		proto = cx.GetPrototypeFromConstructor(nt, storage.IntrinsicArrayPrototype)
	}
	if len(args) == 1 {
		if n := args[0]; n.IsNumber() {
			u := numconv.ToUint32(n.Number())
			if float64(u) != n.Number() {
				cx.ThrowRangeError("invalid array length")
			}
			return storage.ObjectValue(cx.ArrayCreate(u, proto))
		}
	}
	var a *storage.Object
	if len(args) == 0 {
		a = cx.NewEmptyArrayWithRoom(4)
	} else {
		a = cx.NewArray(append([]storage.Value(nil), args...))
	}
	if proto != cx.Intrinsic(storage.IntrinsicArrayPrototype) {
		cx.SetPrototypeOf(a, proto)
	}
	return storage.ObjectValue(a)
}

func arrayIsArray(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Bool(cx.IsArray(arg(args, 0)))
}

func arrayOf(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	if this.IsCallable() && cx.IsConstructor(this.Object()) && this.Object() != cx.Intrinsic(storage.IntrinsicArray) {
		a := cx.Construct(this.Object(), []storage.Value{storage.Number(float64(len(args)))}, this.Object())
		for i, v := range args {
			cx.CreateDataPropertyOrThrow(a, storage.IndexKey(uint32(i)), v)
		}
		setLength(cx, a, int64(len(args)))
		return storage.ObjectValue(a)
	}
	return storage.ObjectValue(cx.NewArray(append([]storage.Value(nil), args...)))
}

func arrayFrom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	items, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
	if !mapFn.IsUndefined() && !mapFn.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(mapFn) + " is not a function")
	}
	custom := this.IsCallable() && cx.IsConstructor(this.Object()) && this.Object() != cx.Intrinsic(storage.IntrinsicArray)
	mapped := func(v storage.Value, i int64) storage.Value {
		if mapFn.IsUndefined() {
			return v
		}
		return cx.Call(mapFn.Object(), thisArg, v, storage.Number(float64(i)))
	}
	if m := cx.GetMethod(items, cx.WellKnownSymbol(storage.SymbolIterator)); m != nil {
		// A new array is out of reach of JavaScript until it is returned, so its
		// elements are collected first while they fit in one.
		var st struct {
			a    *storage.Object
			vals []storage.Value
			i    int64
		}
		if custom {
			st.a = cx.Construct(this.Object(), nil, this.Object())
		}
		// The size of the source is only a guess of how many values the iterator gives,
		// which a replaced iterator may not give at all.
		size := storage.ElementCount(items)
		if items.IsString() {
			size = items.StringLength()
		}
		size = min(size, maxArrayFromPresize)
		cx.IterateFromMethod(items, m, func(v storage.Value) bool {
			v = mapped(v, st.i)
			if st.a == nil && st.i <= storage.MaxArrayIndex {
				if st.vals == nil {
					st.vals = make([]storage.Value, 0, max(size, 8))
				}
				st.vals = append(st.vals, v)
			} else {
				if st.a == nil {
					st.a = cx.NewArray(st.vals)
				}
				cx.CreateDataPropertyOrThrow(st.a, bigKey(cx, st.i), v)
			}
			st.i++
			return true
		})
		if st.a == nil {
			return storage.ObjectValue(cx.NewArray(st.vals))
		}
		setLength(cx, st.a, st.i)
		return storage.ObjectValue(st.a)
	}
	src := cx.ToObject(items)
	n := cx.LengthOfArrayLike(src)
	var a *storage.Object
	switch {
	case custom:
		a = cx.Construct(this.Object(), []storage.Value{storage.Number(float64(n))}, this.Object())
	case n <= storage.MaxArrayIndex+1:
		a = cx.ArrayCreate(uint32(n), cx.Intrinsic(storage.IntrinsicArrayPrototype))
	default:
		cx.ThrowRangeError("invalid array length")
	}
	for i := range n {
		cx.CreateDataPropertyOrThrow(a, bigKey(cx, i), mapped(getAt(cx, src, i), i))
	}
	setLength(cx, a, n)
	return storage.ObjectValue(a)
}

func arrayPush(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	if n, ok := engine.PushElements(cx.Quiet(), o, args); ok {
		return storage.Number(float64(n))
	}
	n := cx.LengthOfArrayLike(o)
	if n+int64(len(args)) > numconv.MaxSafeInteger {
		cx.ThrowTypeError("pushing elements past the maximum length")
	}
	for _, v := range args {
		cx.PollStep()
		cx.Set(o, bigKey(cx, n), v)
		n++
	}
	setLength(cx, o, n)
	return storage.Number(float64(n))
}

func arrayPop(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	if v, ok := o.RemoveLastElement(); ok {
		return v
	}
	n := cx.LengthOfArrayLike(o)
	if n == 0 {
		setLength(cx, o, 0)
		return storage.Undefined
	}
	v := getAt(cx, o, n-1)
	deleteAt(cx, o, n-1)
	setLength(cx, o, n-1)
	return v
}

func arrayShift(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	if v, ok := o.RemoveFirstElement(); ok {
		return v
	}
	n := cx.LengthOfArrayLike(o)
	if n == 0 {
		setLength(cx, o, 0)
		return storage.Undefined
	}
	first := getAt(cx, o, 0)
	for i := int64(1); i < n; i++ {
		if hasAt(cx, o, i) {
			putAt(cx, o, i-1, getAt(cx, o, i))
		} else {
			deleteAt(cx, o, i-1)
			i += moveSteps(cx, o, i+1, i, n-i-1, 1)
		}
	}
	deleteAt(cx, o, n-1)
	setLength(cx, o, n-1)
	return first
}

func arrayUnshift(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	k := int64(len(args))
	if n, ok := cx.Quiet().PrependElements(o, args); ok {
		return storage.Number(float64(n))
	}
	n := cx.LengthOfArrayLike(o)
	if k > 0 {
		if n+k > numconv.MaxSafeInteger {
			cx.ThrowTypeError("unshifting elements past the maximum length")
		}
		for i := n - 1; i >= 0; i-- {
			if hasAt(cx, o, i) {
				putAt(cx, o, i+k, getAt(cx, o, i))
			} else {
				deleteAt(cx, o, i+k)
				i -= moveSteps(cx, o, i-1, i+k-1, i, -1)
			}
		}
		for i, v := range args {
			putAt(cx, o, int64(i), v)
		}
	}
	setLength(cx, o, n+k)
	return storage.Number(float64(n + k))
}

func arraySlice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	end := n
	if e := arg(args, 1); !e.IsUndefined() {
		end = relIndex(cx.ToIntegerOrInfinity(e), n)
	}
	if a, ok := cx.Quiet().CopyElementRange(o, start, end); ok {
		return storage.ObjectValue(a)
	}
	a := arraySpeciesCreate(cx, o, max(end-start, 0))
	for i := start; i < end; i++ {
		if i = cx.Quiet().SkipHoles(o, i, end); i == end {
			break
		}
		if hasAt(cx, o, i) {
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, i-start), getAt(cx, o, i))
		}
	}
	setLength(cx, a, max(end-start, 0))
	return storage.ObjectValue(a)
}

func arraySplice(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	var items []storage.Value
	del := int64(0)
	switch {
	case len(args) == 1:
		del = n - start
	case len(args) >= 2:
		del = int64(math.Min(math.Max(cx.ToIntegerOrInfinity(args[1]), 0), float64(n-start)))
		items = args[2:]
	}
	k := int64(len(items))
	if n+k-del > numconv.MaxSafeInteger {
		cx.ThrowTypeError("splicing elements past the maximum length")
	}
	if removed, ok := cx.Quiet().SpliceElements(o, n, start, del, items); ok {
		return storage.ObjectValue(removed)
	}
	a := arraySpeciesCreate(cx, o, del)
	for i := int64(0); i < del; i++ {
		if hasAt(cx, o, start+i) {
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, i), getAt(cx, o, start+i))
		} else {
			i += holeSteps(cx, o, start+i+1, del-i-1, 1)
		}
	}
	setLength(cx, a, del)
	switch {
	case k < del:
		for i := start; i < n-del; i++ {
			if hasAt(cx, o, i+del) {
				putAt(cx, o, i+k, getAt(cx, o, i+del))
			} else {
				deleteAt(cx, o, i+k)
				i += moveSteps(cx, o, i+del+1, i+k+1, n-del-i-1, 1)
			}
		}
		for i := n; i > n-del+k; i-- {
			deleteAt(cx, o, i-1)
			i -= holeSteps(cx, o, i-2, i-1-(n-del+k), -1)
		}
	case k > del:
		for i := n - del; i > start; i-- {
			if hasAt(cx, o, i+del-1) {
				putAt(cx, o, i+k-1, getAt(cx, o, i+del-1))
			} else {
				deleteAt(cx, o, i+k-1)
				i -= moveSteps(cx, o, i+del-2, i+k-2, i-start-1, -1)
			}
		}
	}
	for i, v := range items {
		putAt(cx, o, start+int64(i), v)
	}
	setLength(cx, o, n-del+k)
	return storage.ObjectValue(a)
}

func arrayConcat(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	a := arraySpeciesCreate(cx, o, 0)
	n := int64(0)
	for k := -1; k < len(args); k++ {
		cx.PollStep()
		item := storage.ObjectValue(o)
		if k >= 0 {
			item = args[k]
		}
		spreadable := false
		if item.IsObject() {
			if s := cx.GetV(item, cx.WellKnownSymbol(storage.SymbolIsConcatSpreadable)); !s.IsUndefined() {
				spreadable = storage.ToBoolean(s)
			} else {
				spreadable = cx.IsArray(item)
			}
		}
		if !spreadable {
			if n >= numconv.MaxSafeInteger {
				cx.ThrowTypeError("array too long")
			}
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, n), item)
			n++
			continue
		}
		l := cx.LengthOfArrayLike(item.Object())
		if n+l > numconv.MaxSafeInteger {
			cx.ThrowTypeError("array too long")
		}
		if cx.Quiet().AppendElementsOf(a, n, item.Object()) {
			n += l
			continue
		}
		// Skipping the holes of an item can look at many elements.
		cx.Poll()
		for i := int64(0); i < l; i++ {
			if i = cx.Quiet().SkipHoles(item.Object(), i, l); i == l {
				break
			}
			if hasAt(cx, item.Object(), i) {
				cx.CreateDataPropertyOrThrow(a, bigKey(cx, n+i), getAt(cx, item.Object(), i))
			}
		}
		n += l
	}
	setLength(cx, a, n)
	return storage.ObjectValue(a)
}

func arrayJoin(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	sep := wtf8.String(",")
	if s := arg(args, 0); !s.IsUndefined() {
		sep = cx.ToString(s)
	}
	return joinArray(cx, o, n, sep)
}

func arrayToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	if j := cx.GetV(storage.ObjectValue(o), cx.Intern("join")); j.IsCallable() {
		return cx.Call(j.Object(), storage.ObjectValue(o))
	}
	return objectToString(cx, o)
}

func arrayReverse(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	if cx.Quiet().ReverseElements(o) {
		return storage.ObjectValue(o)
	}
	for lo, hi := int64(0), n-1; lo < hi; lo, hi = lo+1, hi-1 {
		lok, hok := hasAt(cx, o, lo), false
		var lv, hv storage.Value
		if lok {
			lv = getAt(cx, o, lo)
		}
		hok = hasAt(cx, o, hi)
		if hok {
			hv = getAt(cx, o, hi)
		}
		switch {
		case lok && hok:
			putAt(cx, o, lo, hv)
			putAt(cx, o, hi, lv)
		case hok:
			putAt(cx, o, lo, hv)
			deleteAt(cx, o, hi)
		case lok:
			deleteAt(cx, o, lo)
			putAt(cx, o, hi, lv)
		default:
			// A pair of holes changes nothing, and so do the pairs of holes that follow.
			if m := holeSteps(cx, o, lo+1, (hi-lo-1)/2, 1); m > 0 {
				m = min(m, holeSteps(cx, o, hi-1, m, -1))
				lo, hi = lo+m, hi-m
			}
		}
	}
	return storage.ObjectValue(o)
}

var (
	// The kernels of indexOf and includes are the searches of an Array from its start.
	nativeArrayIndexOf = engine.RegisterNativeWithKernels(arraySearchNative(false, false), engine.Kernels{
		Method1:  quiet.ArrayIndexOfKernel,
		Contract: polledContract,
	})
	nativeArrayLastIndexOf = engine.RegisterNative(arraySearchNative(true, false))
	nativeArrayIncludes    = engine.RegisterNativeWithKernels(arraySearchNative(false, true), engine.Kernels{
		Method1:  quiet.ArrayIncludesKernel,
		Contract: polledContract,
	})
)

// arraySearchNative returns a method that searches an array for a value. With fromEnd, the method
// goes from the end of the array to its start. With same, it compares by SameValueZero and returns
// whether it finds the value. Otherwise, it compares by IsStrictlyEqual and returns the index.
//
//go:noinline
func arraySearchNative(fromEnd, same bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		target := arg(args, 0)
		eq := storage.IsStrictlyEqual
		if same {
			eq = storage.SameValueZero
		}
		if n == 0 {
			if same {
				return storage.False
			}
			return storage.Number(-1)
		}
		if fromEnd {
			from := n - 1
			if len(args) > 1 {
				f := cx.ToIntegerOrInfinity(args[1])
				if f < 0 {
					from = n + int64(math.Max(f, float64(-n-1)))
				} else {
					from = int64(math.Min(f, float64(n-1)))
				}
			}
			for i := from; i >= 0; i-- {
				f, next := cx.Quiet().SearchElements(o, i, -1, target, false)
				if f >= 0 {
					return storage.Number(float64(f))
				}
				if i = next; i < 0 {
					break
				}
				cx.PollAt(i)
				if e, ok := storage.Element(o, i); ok {
					if eq(e, target) {
						return storage.Number(float64(i))
					}
					continue
				}
				if i = cx.Quiet().SkipHolesBackward(o, i, -1); i < 0 {
					break
				}
				if hasAt(cx, o, i) && eq(getAt(cx, o, i), target) {
					return storage.Number(float64(i))
				}
			}
			return storage.Number(-1)
		}
		from := relIndex(cx.ToIntegerOrInfinity(arg(args, 1)), n)
		for i := from; i < n; i++ {
			f, next := cx.Quiet().SearchElements(o, i, n, target, same)
			if f >= 0 {
				if same {
					return storage.True
				}
				return storage.Number(float64(f))
			}
			if i = next; i == n {
				break
			}
			cx.PollAt(i)
			if same {
				e, ok := storage.Element(o, i)
				if !ok {
					if j := cx.Quiet().SkipHoles(o, i, n); j > i {
						// The indices from i to j-1 read as undefined.
						if target.IsUndefined() {
							return storage.True
						}
						i = j - 1
						continue
					}
					e = getAt(cx, o, i)
				}
				if eq(e, target) {
					return storage.True
				}
				continue
			}
			if e, ok := storage.Element(o, i); ok {
				if eq(e, target) {
					return storage.Number(float64(i))
				}
				continue
			}
			if i = cx.Quiet().SkipHoles(o, i, n); i == n {
				break
			}
			if hasAt(cx, o, i) && eq(getAt(cx, o, i), target) {
				return storage.Number(float64(i))
			}
		}
		if same {
			return storage.False
		}
		return storage.Number(-1)
	}
}

func arrayForEach(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "forEach", true)
	for {
		if _, _, ok := l.next(); !ok {
			return storage.Undefined
		}
	}
}

func arrayMap(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "map", true)
	a := arraySpeciesCreate(cx, l.o, l.n)
	for {
		_, r, ok := l.next()
		if !ok {
			return storage.ObjectValue(a)
		}
		cx.CreateDataPropertyOrThrow(a, bigKey(cx, l.i), r)
	}
}

func arrayFilter(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "filter", true)
	a := arraySpeciesCreate(cx, l.o, 0)
	j := int64(0)
	for {
		v, r, ok := l.next()
		if !ok {
			return storage.ObjectValue(a)
		}
		if storage.ToBoolean(r) {
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, j), v)
			j++
		}
	}
}

func arraySome(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "some", true)
	for {
		_, r, ok := l.next()
		if !ok {
			return storage.False
		}
		if storage.ToBoolean(r) {
			return storage.True
		}
	}
}

func arrayEvery(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "every", true)
	for {
		_, r, ok := l.next()
		if !ok {
			return storage.True
		}
		if !storage.ToBoolean(r) {
			return storage.False
		}
	}
}

func arrayFind(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "find", false)
	for {
		v, r, ok := l.next()
		if !ok {
			return storage.Undefined
		}
		if storage.ToBoolean(r) {
			return v
		}
	}
}

func arrayFindIndex(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	l := newEachLoop(cx, this, args, "findIndex", false)
	for {
		_, r, ok := l.next()
		if !ok {
			return storage.Number(-1)
		}
		if storage.ToBoolean(r) {
			return storage.Number(float64(l.i))
		}
	}
}

var (
	nativeArrayFindLast      = engine.RegisterNative(arrayFindLastNative("findLast", false))
	nativeArrayFindLastIndex = engine.RegisterNative(arrayFindLastNative("findLastIndex", true))
)

// arrayFindLastNative returns the method name, which finds the last element that its callback
// accepts. With index, the method returns the index of the element.
//
//go:noinline
func arrayFindLastNative(name string, index bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		cb := callback(cx, args, name)
		for i := n - 1; i >= 0; i-- {
			v := getAt(cx, o, i)
			if storage.ToBoolean(cx.Call(cb, arg(args, 1), v, storage.Number(float64(i)), storage.ObjectValue(o))) {
				if index {
					return storage.Number(float64(i))
				}
				return v
			}
		}
		if index {
			return storage.Number(-1)
		}
		return storage.Undefined
	}
}

var (
	nativeArrayReduce      = engine.RegisterNative(arrayReduceNative("reduce", false))
	nativeArrayReduceRight = engine.RegisterNative(arrayReduceNative("reduceRight", true))
)

// arrayReduceNative returns the method name, which reduces an array. With right, the method goes
// from the end of the array to its start.
//
//go:noinline
func arrayReduceNative(name string, right bool) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		cb := callback(cx, args, name)
		i, end, d := int64(0), n, int64(1)
		if right {
			i, end, d = n-1, -1, -1
		}
		var acc storage.Value
		if len(args) >= 2 {
			acc = args[1]
		} else {
			found := false
			for ; i != end; i += d {
				if i = skipHoles(cx, o, i, end, right); i == end {
					break
				}
				if hasAt(cx, o, i) {
					acc = getAt(cx, o, i)
					found = true
					i += d
					break
				}
			}
			if !found {
				cx.ThrowTypeError("reduce of empty array with no initial value")
			}
		}
		for ; i != end; i += d {
			if _, ok := storage.Element(o, i); !ok {
				if i = skipHoles(cx, o, i, end, right); i == end {
					break
				}
				if !hasAt(cx, o, i) {
					continue
				}
			}
			acc = cx.Call(cb, storage.Undefined, acc, getAt(cx, o, i), storage.Number(float64(i)), storage.ObjectValue(o))
		}
		return acc
	}
}

func arrayFill(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	start := relIndex(cx.ToIntegerOrInfinity(arg(args, 1)), n)
	end := n
	if e := arg(args, 2); !e.IsUndefined() {
		end = relIndex(cx.ToIntegerOrInfinity(e), n)
	}
	if cx.Quiet().FillElements(o, n, start, end, arg(args, 0)) {
		return storage.ObjectValue(o)
	}
	for i := start; i < end; i++ {
		putAt(cx, o, i, arg(args, 0))
	}
	return storage.ObjectValue(o)
}

func arrayAt(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	i := cx.ToIntegerOrInfinity(arg(args, 0))
	if i < 0 {
		i += float64(n)
	}
	if i < 0 || i >= float64(n) {
		return storage.Undefined
	}
	return getAt(cx, o, int64(i))
}

func arraySort(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cmp := arg(args, 0)
	if !cmp.IsUndefined() && !cmp.IsCallable() {
		cx.ThrowTypeError("the comparison function must be either a function or undefined")
	}
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	var vals []storage.Value
	undefs := int64(0)
	for i := int64(0); i < n; i++ {
		if i = cx.Quiet().SkipHoles(o, i, n); i == n {
			break
		}
		if !hasAt(cx, o, i) {
			continue
		}
		v := getAt(cx, o, i)
		if v.IsUndefined() {
			undefs++
			continue
		}
		vals = append(vals, v)
	}
	sortValues(cx, vals, cmp)
	i := int64(0)
	for _, v := range vals {
		putAt(cx, o, i, v)
		i++
	}
	for ; undefs > 0; undefs-- {
		putAt(cx, o, i, storage.Undefined)
		i++
	}
	for ; i < n; i++ {
		if i = cx.Quiet().SkipHoles(o, i, n); i == n {
			break
		}
		deleteAt(cx, o, i)
	}
	return storage.ObjectValue(o)
}

func arrayFlat(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	l := cx.LengthOfArrayLike(o)
	depth := 1.0
	if d := arg(args, 0); !d.IsUndefined() {
		depth = cx.ToIntegerOrInfinity(d)
	}
	a := arraySpeciesCreate(cx, o, 0)
	var n int64
	flattenIntoArray(cx, a, o, l, depth, &n)
	return storage.ObjectValue(a)
}

func arrayFlatMap(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	l := cx.LengthOfArrayLike(o)
	cb := callback(cx, args, "flatMap")
	a := arraySpeciesCreate(cx, o, 0)
	var n int64
	for i := int64(0); i < l; i++ {
		if i = cx.Quiet().SkipHoles(o, i, l); i == l {
			break
		}
		if !hasAt(cx, o, i) {
			continue
		}
		v := cx.Call(cb, arg(args, 1), getAt(cx, o, i), storage.Number(float64(i)), storage.ObjectValue(o))
		if cx.IsArray(v) {
			flattenIntoArray(cx, a, v.Object(), cx.LengthOfArrayLike(v.Object()), 0, &n)
			continue
		}
		cx.CreateDataPropertyOrThrow(a, bigKey(cx, n), v)
		n++
	}
	return storage.ObjectValue(a)
}

func arrayKeys(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.ObjectValue(cx.NewArrayIterator(cx.ToObject(this), quiet.IterateKeys))
}

func arrayEntries(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.ObjectValue(cx.NewArrayIterator(cx.ToObject(this), quiet.IterateEntries))
}

func arrayValues(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.ObjectValue(cx.NewArrayIterator(cx.ToObject(this), quiet.IterateValues))
}

// newArrayOfLength checks that n is a valid array length as ArrayCreate does, and returns the storage
// for the elements of a new array of that length.
func newArrayOfLength(cx *engine.ExecutionContext, n int64) []storage.Value {
	if n > storage.MaxArrayIndex+1 {
		cx.ThrowRangeError("invalid array length")
	}
	// The length is not trusted to allocate: the values are read one by one, which may throw.
	return make([]storage.Value, 0, min(n, 1024))
}

var (
	nativeArrayCopyWithin     = engine.RegisterNative(arrayCopyWithin)
	nativeArrayToLocaleString = engine.RegisterNative(arrayToLocaleString)
	nativeArrayToReversed     = engine.RegisterNative(arrayToReversed)
	nativeArrayToSorted       = engine.RegisterNative(arrayToSorted)
	nativeArrayToSpliced      = engine.RegisterNative(arrayToSpliced)
	nativeArrayWith           = engine.RegisterNative(arrayWith)
)

// initArrayAdditions defines copyWithin, toLocaleString, the methods that change an array by copy,
// and Symbol.unscopables on Array.prototype.
func initArrayAdditions(cx *engine.ExecutionContext, proto *storage.Object) {
	method(cx, proto, "copyWithin", 2, nativeArrayCopyWithin)
	method(cx, proto, "toLocaleString", 0, nativeArrayToLocaleString)
	method(cx, proto, "toReversed", 0, nativeArrayToReversed)
	method(cx, proto, "toSorted", 1, nativeArrayToSorted)
	method(cx, proto, "toSpliced", 2, nativeArrayToSpliced)
	method(cx, proto, "with", 2, nativeArrayWith)

	unscopables := cx.NewObject(nil)
	for _, name := range []wtf8.String{
		"at", "copyWithin", "entries", "fill", "find", "findIndex", "findLast", "findLastIndex",
		"flat", "flatMap", "includes", "keys", "toReversed", "toSorted", "toSpliced", "values",
	} {
		cx.Define(unscopables, cx.Intern(name), storage.True, storage.FlagsData)
	}
	cx.Define(proto, cx.WellKnownSymbol(storage.SymbolUnscopables), storage.ObjectValue(unscopables), storage.FlagConfigurable)
}

func arrayCopyWithin(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	to := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	from := relIndex(cx.ToIntegerOrInfinity(arg(args, 1)), n)
	end := n
	if e := arg(args, 2); !e.IsUndefined() {
		end = relIndex(cx.ToIntegerOrInfinity(e), n)
	}
	count := min(end-from, n-to)
	dir := int64(1)
	if from < to && to < from+count {
		dir = -1
		from += count - 1
		to += count - 1
	}
	for ; count > 0; count-- {
		if hasAt(cx, o, from) {
			putAt(cx, o, to, getAt(cx, o, from))
		} else {
			deleteAt(cx, o, to)
			m := moveSteps(cx, o, from+dir, to+dir, count-1, dir)
			from += dir * m
			to += dir * m
			count -= m
		}
		from += dir
		to += dir
	}
	return storage.ObjectValue(o)
}

func arrayToLocaleString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	if !cx.EnterLocaleJoin(o) {
		return cx.NewString("")
	}
	defer cx.LeaveLocaleJoin()
	n := cx.LengthOfArrayLike(o)
	return joinElements(cx, o, n, ",", cx.Key(storage.KeyToLocaleString))
}

func arrayToReversed(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	vals := newArrayOfLength(cx, n)
	for i := n - 1; i >= 0; i-- {
		vals = append(vals, getAt(cx, o, i))
	}
	return cx.NewArrayOf(vals)
}

func arrayToSorted(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	cmp := arg(args, 0)
	if !cmp.IsUndefined() && !cmp.IsCallable() {
		cx.ThrowTypeError("the comparison function must be either a function or undefined")
	}
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	vals := newArrayOfLength(cx, n)
	undefs := 0
	for i := range n {
		v := getAt(cx, o, i)
		if v.IsUndefined() {
			undefs++
			continue
		}
		vals = append(vals, v)
	}
	sortValues(cx, vals, cmp)
	vals = slices.Grow(vals, undefs)
	start := len(vals)
	vals = vals[:start+undefs]
	clear(vals[start:])
	return cx.NewArrayOf(vals)
}

func arrayToSpliced(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
	var items []storage.Value
	if len(args) > 2 {
		items = args[2:]
	}
	var skip int64
	switch {
	case len(args) == 0:
	case len(args) == 1:
		skip = n - start
	default:
		skip = int64(math.Min(math.Max(cx.ToIntegerOrInfinity(args[1]), 0), float64(n-start)))
	}
	newLen := n + int64(len(items)) - skip
	if newLen > numconv.MaxSafeInteger {
		cx.ThrowTypeError("invalid array length")
	}
	vals := newArrayOfLength(cx, newLen)
	for i := range start {
		vals = append(vals, getAt(cx, o, i))
	}
	vals = append(vals, items...)
	for i := start + skip; i < n; i++ {
		vals = append(vals, getAt(cx, o, i))
	}
	return cx.NewArrayOf(vals)
}

func arrayWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	rel := cx.ToIntegerOrInfinity(arg(args, 0))
	idx := rel
	if rel < 0 {
		idx = float64(n) + rel
	}
	if idx < 0 || idx >= float64(n) {
		cx.ThrowRangeError("invalid index")
	}
	vals := newArrayOfLength(cx, n)
	for i := range n {
		if i == int64(idx) {
			vals = append(vals, arg(args, 1))
			continue
		}
		vals = append(vals, getAt(cx, o, i))
	}
	return cx.NewArrayOf(vals)
}

// relIndex resolves a relative index against a length, clamping to the range from 0 to n.
func relIndex(f float64, n int64) int64 {
	return numconv.ClampRelativeIndex(f, n)
}

// sortValues sorts vals with the comparison function cmp, or as strings when cmp is undefined.
func sortValues(cx *engine.ExecutionContext, vals []storage.Value, cmp storage.Value) {
	if len(vals) < 2 {
		return
	}
	if cmp.IsUndefined() {
		allStrings := true
		// The bits of lengths are those of all the lengths of the keys in bytes.
		lengths := 0
		for i, v := range vals {
			cx.PollAt(int64(i))
			if v.IsObject() || v.IsSymbol() {
				// Object conversion can run code and symbol conversion throws.
				var pacer engine.Pacer
				mergeSortPolled(cx, vals, func(a, b storage.Value) int {
					x, y := cx.ToString(a), cx.ToString(b)
					pacer.Steps(cx, min(len(x), len(y)))
					return wtf8.Compare(x, y)
				})
				return
			}
			allStrings = allStrings && v.IsString()
			if allStrings {
				lengths |= v.StringByteLength()
			}
		}
		if allStrings {
			if lengths < longSortKey {
				mergeSortPolled(cx, vals, compareStringValues)
			} else {
				mergeSortLong(cx, vals, storage.Value.StringByteLength, compareStringValues)
			}
			return
		}
		keyed := make([]keyedValue, len(vals))
		for i := 0; i < len(vals); i += storage.PollInterval {
			if i > 0 {
				cx.Poll()
			}
			lengths |= keyValues(cx, keyed[i:], vals[i:min(i+storage.PollInterval, len(vals))])
		}
		if lengths < longSortKey {
			mergeSortPolled(cx, keyed, compareKeyedValues)
		} else {
			mergeSortLong(cx, keyed, keyedValue.keyLength, compareKeyedValues)
		}
		for i := range vals {
			vals[i] = keyed[i].v
		}
		return
	}
	mergeSort(vals, func(a, b storage.Value) int {
		if cx.ToNumber(cx.Call(cmp.Object(), storage.Undefined, a, b)) < 0 {
			return -1
		}
		return 0
	})
}

func mergeSortPolled[E any](cx *engine.ExecutionContext, s []E, compare func(a, b E) int) {
	sortPolled(cx, s, sortRunLength, func(run []E) {
		mergeSort(run, compare)
	}, compare)
}

// longSortKey is the length in bytes from which the comparisons of a key count as more than one
// step. It is a power of two.
const longSortKey = 1 << 8

// mergeSortLong is mergeSortPolled for elements with long keys, whose lengths in bytes length
// returns.
func mergeSortLong[E any](cx *engine.ExecutionContext, s []E, length func(e E) int, compare func(a, b E) int) {
	var pacer engine.Pacer
	mergeSortPolled(cx, s, func(a, b E) int {
		pacer.Steps(cx, min(length(a), length(b)))
		return compare(a, b)
	})
}

// insertionSortLength is the length up to which mergeSort sorts by insertion.
const insertionSortLength = 12

// mergeSort sorts s stably in the order of compare, which only needs to tell whether its first
// argument goes before its second. An inconsistent compare leaves s in some order of its elements.
func mergeSort[E any](s []E, compare func(a, b E) int) {
	if len(s) <= insertionSortLength {
		insertionSort(s, compare)
		return
	}
	mergeSortBuf(s, make([]E, len(s)/2), compare)
}

// mergeSortBuf is mergeSort with buf, which has room for half of s, as scratch.
func mergeSortBuf[E any](s, buf []E, compare func(a, b E) int) {
	if len(s) <= insertionSortLength {
		insertionSort(s, compare)
		return
	}
	mid := len(s) / 2
	mergeSortBuf(s[:mid], buf, compare)
	mergeSortBuf(s[mid:], buf, compare)
	if compare(s[mid], s[mid-1]) >= 0 {
		return
	}
	// Every element of the left half is taken from the copy in buf, and the index i in s that it
	// is written to never passes the index j of the next right element, so no right element is
	// overwritten before it is taken.
	left := buf[:copy(buf, s[:mid])]
	i, j := 0, mid
	for len(left) > 0 && j < len(s) {
		if compare(s[j], left[0]) < 0 {
			s[i] = s[j]
			j++
		} else {
			s[i] = left[0]
			left = left[1:]
		}
		i++
	}
	copy(s[i:], left)
}

// insertionSort sorts s stably in the order of compare by binary insertion.
func insertionSort[E any](s []E, compare func(a, b E) int) {
	for i := 1; i < len(s); i++ {
		x := s[i]
		if compare(x, s[i-1]) >= 0 {
			continue
		}
		// Find the first element that x goes before.
		lo, hi := 0, i-1
		for lo < hi {
			m := int(uint(lo+hi) >> 1)
			if compare(x, s[m]) < 0 {
				hi = m
			} else {
				lo = m + 1
			}
		}
		copy(s[lo+1:i+1], s[lo:i])
		s[lo] = x
	}
}

// compareStringValues compares the strings a and b by their code units.
func compareStringValues(a, b storage.Value) int {
	return storage.CompareStrings(a, b)
}

// keyedValue is a value with the string it sorts by.
type keyedValue struct {
	v   storage.Value
	key wtf8.String
}

// keyLength returns the length of the key of v in bytes.
func (v keyedValue) keyLength() int {
	return len(v.key)
}

// compareKeyedValues compares the keys of a and b by their code units.
func compareKeyedValues(a, b keyedValue) int {
	return wtf8.Compare(a.key, b.key)
}

// keyValues sets the elements of keyed to vals with their strings, and returns the bitwise OR of
// the lengths of the strings in bytes.
func keyValues(cx *engine.ExecutionContext, keyed []keyedValue, vals []storage.Value) int {
	lengths := 0
	for i, v := range vals {
		key := cx.ToString(v)
		lengths |= len(key)
		keyed[i] = keyedValue{
			v:   v,
			key: key,
		}
	}
	return lengths
}

// Run lengths of sortPolled.
const (
	// sortRunLength is the run length for elements that are slow to compare, such as strings.
	sortRunLength = 1 << 16

	// numberSortRunLength is the run length for numbers.
	numberSortRunLength = 1 << 18
)

// sortPolled sorts s, stably when sortRun is stable, checking for an interruption now and then.
// sortRun sorts a slice of up to run elements in the order of compare.
func sortPolled[E any](cx *engine.ExecutionContext, s []E, run int, sortRun func(s []E), compare func(a, b E) int) {
	n := len(s)
	if n <= run {
		sortRun(s)
		return
	}
	for i := 0; i < n; i += run {
		cx.Poll()
		sortRun(s[i:min(i+run, n)])
	}
	src, dst := s, make([]E, n)
	var inBuf bool
	for w := run; w < n; w *= 2 {
		for i := 0; i < n; i += 2 * w {
			mergeRuns(cx, dst[i:min(i+2*w, n)], src[i:min(i+w, n)], src[min(i+w, n):min(i+2*w, n)], compare)
		}
		src, dst = dst, src
		inBuf = !inBuf
	}
	if inBuf {
		engine.CopyPolled(cx, s, src)
	}
}

// mergeRuns merges the sorted a and b into dst, which has room for both, taking the elements of a
// first among equal ones. It checks for an interruption now and then.
func mergeRuns[E any](cx *engine.ExecutionContext, dst, a, b []E, compare func(a, b E) int) {
	for len(a) > 0 && len(b) > 0 {
		var k int
		k, a, b = mergeSome(dst, a, b, storage.PollInterval, compare)
		dst = dst[k:]
		cx.Poll()
	}
	k := copy(dst, a)
	copy(dst[k:], b)
}

// mergeSome merges up to max elements of the sorted a and b into dst as mergeRuns does, and
// returns how many it merged and what is left of a and b.
func mergeSome[E any](dst, a, b []E, max int, compare func(a, b E) int) (int, []E, []E) {
	k := 0
	for ; k < max && len(a) > 0 && len(b) > 0; k++ {
		if compare(b[0], a[0]) < 0 {
			dst[k] = b[0]
			b = b[1:]
		} else {
			dst[k] = a[0]
			a = a[1:]
		}
	}
	return k, a, b
}

// eachLoop is the loop of an iteration method such as forEach, which calls a callback with
// each element, its index and the object.
type eachLoop struct {
	cx        *engine.ExecutionContext
	o         *storage.Object
	n         int64
	cb        *storage.Object
	thisArg   storage.Value
	skipHoles bool

	// i is the index of the element of the last call of next.
	i int64
}

// newEachLoop converts this to an object, reads its length and checks the callback of the
// method name. With skipHoles, the loop skips the indices that are not present.
func newEachLoop(cx *engine.ExecutionContext, this storage.Value, args []storage.Value, name string, skipHoles bool) eachLoop {
	o := cx.ToObject(this)
	n := cx.LengthOfArrayLike(o)
	return eachLoop{
		cx:        cx,
		o:         o,
		n:         n,
		cb:        callback(cx, args, name),
		thisArg:   arg(args, 1),
		skipHoles: skipHoles,
		i:         -1,
	}
}

// next calls the callback with the next element. It returns the element and the result of
// the callback, or false when no element is left.
func (l *eachLoop) next() (v, r storage.Value, ok bool) {
	cx, o := l.cx, l.o
	for i := l.i + 1; i < l.n; i++ {
		if l.skipHoles {
			if _, ok := storage.Element(o, i); !ok {
				if i = cx.Quiet().SkipHoles(o, i, l.n); i == l.n {
					break
				}
				if !hasAt(cx, o, i) {
					continue
				}
			}
		}
		l.i = i
		v := getAt(cx, o, i)
		return v, cx.Call(l.cb, l.thisArg, v, storage.Number(float64(i)), storage.ObjectValue(o)), true
	}
	l.i = l.n
	return storage.Undefined, storage.Undefined, false
}
