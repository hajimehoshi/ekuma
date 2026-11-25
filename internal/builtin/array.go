// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// maxArrayFromPresize is the largest number of elements that Array.from reserves for the values
// of an iterator before it has them.
const maxArrayFromPresize = 1 << 16

// bigKey returns the property key for an integer index, which may be beyond the array indices.
func bigKey(cx *engine.ExecutionContext, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.IndexKey(uint32(i))
	}
	return engine.NamedKey(cx.Intern(numconv.NumberToString(float64(i))))
}

// getAt returns the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func getAt(cx *engine.ExecutionContext, o *engine.Object, i int64) engine.Value {
	if e, ok := o.Element(i); ok {
		cx.PollAt(i)
		return e
	}
	cx.Poll()
	return cx.Get(o, bigKey(cx, i))
}

// hasAt reports whether o has a property with the integer index i. A loop over consecutive
// indices that calls it checks for an interruption regularly.
func hasAt(cx *engine.ExecutionContext, o *engine.Object, i int64) bool {
	if _, ok := o.Element(i); ok {
		cx.PollAt(i)
		return true
	}
	cx.Poll()
	return cx.HasProperty(o, bigKey(cx, i))
}

// skipHoles returns the next index from i toward end, which is excluded, that may have a property
// on o, or end when there is none.
func skipHoles(cx *engine.ExecutionContext, o *engine.Object, i, end int64, backward bool) int64 {
	if backward {
		return cx.Quiet().SkipHolesBackward(o, i, end)
	}
	return cx.Quiet().SkipHoles(o, i, end)
}

// holeSteps returns the number of consecutive indices from i, moving by dir (1 or -1) and at most
// count, at which neither o nor its prototypes are known to have a property.
func holeSteps(cx *engine.ExecutionContext, o *engine.Object, i, count, dir int64) int64 {
	if _, ok := o.Element(i); ok || i > engine.MaxArrayIndex {
		return 0
	}
	if dir > 0 {
		count = min(count, engine.MaxArrayIndex+1-i)
		return cx.Quiet().SkipHoles(o, i, i+count) - i
	}
	return i - cx.Quiet().SkipHolesBackward(o, i, i-count)
}

// moveSteps returns how many steps, at most count, of a loop that moves the property at src to dst,
// or deletes the property at dst when src has none, change nothing. Both indices move by dir (1 or
// -1).
func moveSteps(cx *engine.ExecutionContext, o *engine.Object, src, dst, count, dir int64) int64 {
	m := holeSteps(cx, o, src, count, dir)
	if m == 0 {
		return 0
	}
	return min(m, holeSteps(cx, o, dst, m, dir))
}

// putAt sets the property of o with the integer index i to v. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func putAt(cx *engine.ExecutionContext, o *engine.Object, i int64, v engine.Value) {
	if o.SetElement(i, v) {
		cx.PollAt(i)
		return
	}
	cx.Poll()
	cx.Set(o, bigKey(cx, i), v)
}

// deleteAt deletes the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func deleteAt(cx *engine.ExecutionContext, o *engine.Object, i int64) {
	cx.Poll()
	k := bigKey(cx, i)
	if !cx.DeleteProp(o, k) {
		cx.ThrowTypeError("cannot delete property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

func setLength(cx *engine.ExecutionContext, o *engine.Object, n int64) {
	cx.Set(o, engine.NamedKey(cx.Key(engine.KeyLength)), engine.Number(float64(n)))
}

// arraySpeciesCreate implements ArraySpeciesCreate.
func arraySpeciesCreate(cx *engine.ExecutionContext, orig *engine.Object, n int64) *engine.Object {
	if a, ok := cx.Quiet().ArraySpeciesCreatePlain(orig, n); ok {
		return a
	}
	if cx.IsArray(engine.ObjectValue(orig)) {
		c := cx.GetV(engine.ObjectValue(orig), cx.Key(engine.KeyConstructor))
		if c.IsObject() {
			if c.IsCallable() && cx.IsConstructor(c.Object()) && cx.IsIntrinsicOfOtherRealm(c.Object(), engine.IntrinsicArray) {
				c = engine.Undefined
			} else if s := cx.GetV(c, cx.WellKnownSymbol(engine.SymbolSpecies)); s.IsNull() {
				c = engine.Undefined
			} else {
				c = s
			}
		}
		if !c.IsUndefined() && !(c.IsObject() && c.Object() == cx.Intrinsic(engine.IntrinsicArray)) {
			if !c.IsCallable() || !cx.IsConstructor(c.Object()) {
				cx.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
			}
			return cx.Construct(c.Object(), []engine.Value{engine.Number(float64(n))}, c.Object())
		}
	}
	if n > engine.MaxArrayIndex+1 {
		cx.ThrowRangeError("invalid array length")
	}
	return cx.ArrayCreate(uint32(n), cx.Intrinsic(engine.IntrinsicArrayPrototype))
}

func callback(cx *engine.ExecutionContext, args []engine.Value, what string) *engine.Object {
	f := arg(args, 0)
	if !f.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(f) + " is not a function")
	}
	return f.Object()
}

// joinGuard holds the objects that are being joined, to stop at cycles.
type joinGuard struct {
	joining []*engine.Object
}

// enter adds o and reports whether it was not being joined. leave must be called when it
// reports true.
func (g *joinGuard) enter(o *engine.Object) bool {
	if slices.Contains(g.joining, o) {
		return false
	}
	g.joining = append(g.joining, o)
	return true
}

// leave removes the object that the last call of enter added.
func (g *joinGuard) leave() {
	last := len(g.joining) - 1
	g.joining[last] = nil
	g.joining = g.joining[:last]
}

// joinArray implements Array.prototype.join for the first n elements of the object o.
func joinArray(cx *engine.ExecutionContext, g *joinGuard, o *engine.Object, n int64, sep wtf8.String) engine.Value {
	if !g.enter(o) {
		return cx.NewString("")
	}
	defer g.leave()
	if v, ok := cx.Quiet().JoinStrings(o, n, sep); ok {
		return v
	}
	return joinElements(cx, o, n, sep, nil)
}

// writeString writes ToString(v) to b.
func writeString(cx *engine.ExecutionContext, b *wtf8.Buffer, v engine.Value) {
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
func joinElements(cx *engine.ExecutionContext, o *engine.Object, n int64, sep wtf8.String, method *engine.Object) engine.Value {
	var b wtf8.Buffer
	for i := int64(0); i < n; {
		if i > 0 {
			cx.Poll()
		}
		i = joinRun(cx, &b, o, i, min(n, i+engine.PollInterval), n, sep, method)
	}
	return cx.NewString(b.String())
}

// joinRun writes the elements of o from i up to end, or further when holes follow, to b as
// joinElements joins the first n elements, and returns the index after the last one it wrote.
func joinRun(cx *engine.ExecutionContext, b *wtf8.Buffer, o *engine.Object, i, end, n int64, sep wtf8.String, method *engine.Object) int64 {
	for ; i < end; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		e, ok := o.Element(i)
		if !ok {
			if j := cx.Quiet().SkipHoles(o, i, n); j > i {
				// The indices from i to j-1 read as undefined.
				writeSeparators(cx, b, sep, j-1-i)
				i = j - 1
				e = engine.Undefined
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
		if b.Len() > engine.MaxStringLength {
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
	if int64(b.Len())+count*per-2 > engine.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	writeRepeated(cx, b, sep, int(count))
	if b.Len() > engine.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
}

// flattenIntoArray implements FlattenIntoArray for the first l elements of src without a mapper function.
// The nested arrays are kept on an explicit stack, as their depth is unbounded.
func flattenIntoArray(cx *engine.ExecutionContext, a *engine.Object, src *engine.Object, l int64, depth float64, n *int64) {
	type level struct {
		src   *engine.Object
		i, l  int64
		depth float64
	}
	stack := []level{{
		src:   src,
		l:     l,
		depth: depth,
	}}
	for len(stack) > 0 {
		t := &stack[len(stack)-1]
		if t.i = cx.Quiet().SkipHoles(t.src, t.i, t.l); t.i >= t.l {
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

func initArray(cx *engine.ExecutionContext, strs *objectStrings) {
	proto := cx.Intrinsic(engine.IntrinsicArrayPrototype)
	var joins joinGuard
	ctor := constructor(cx, "Array", 1, engine.IntrinsicArrayPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		proto := cx.Intrinsic(engine.IntrinsicArrayPrototype)
		if nt := cx.GetNewTarget(); nt != nil && nt != cx.Intrinsic(engine.IntrinsicArray) {
			proto = cx.GetPrototypeFromConstructor(nt, engine.IntrinsicArrayPrototype)
		}
		if len(args) == 1 {
			if n := args[0]; n.IsNumber() {
				u := numconv.ToUint32(n.Number())
				if float64(u) != n.Number() {
					cx.ThrowRangeError("invalid array length")
				}
				return engine.ObjectValue(cx.ArrayCreate(u, proto))
			}
		}
		var a *engine.Object
		if len(args) == 0 {
			a = cx.NewEmptyArrayWithRoom(4)
		} else {
			a = cx.NewArray(append([]engine.Value(nil), args...))
		}
		if proto != cx.Intrinsic(engine.IntrinsicArrayPrototype) {
			cx.SetPrototypeOf(a, proto)
		}
		return engine.ObjectValue(a)
	})
	cx.SetIntrinsic(engine.IntrinsicArray, ctor)
	speciesGetter := cx.NewNative("get [Symbol.species]", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	cx.SetIntrinsic(engine.IntrinsicArraySpeciesGetter, speciesGetter)
	cx.DefineAccessor(ctor, cx.WellKnownSymbol(engine.SymbolSpecies), speciesGetter, nil, engine.FlagConfigurable)

	isArray := method(cx, ctor, "isArray", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(cx.IsArray(arg(args, 0)))
	})
	cx.SetMethodKernels(isArray, shortContract, nil, func(q engine.QuietContext, this, v engine.Value) (engine.Value, bool) {
		is, ok := engine.IsArrayPlain(v)
		return engine.Bool(is), ok
	}, nil)
	method(cx, ctor, "of", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if this.IsCallable() && cx.IsConstructor(this.Object()) && this.Object() != cx.Intrinsic(engine.IntrinsicArray) {
			a := cx.Construct(this.Object(), []engine.Value{engine.Number(float64(len(args)))}, this.Object())
			for i, v := range args {
				cx.CreateDataPropertyOrThrow(a, engine.IndexKey(uint32(i)), v)
			}
			setLength(cx, a, int64(len(args)))
			return engine.ObjectValue(a)
		}
		return engine.ObjectValue(cx.NewArray(append([]engine.Value(nil), args...)))
	})
	method(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		items, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
		if !mapFn.IsUndefined() && !mapFn.IsCallable() {
			cx.ThrowTypeError(cx.DisplayString(mapFn) + " is not a function")
		}
		custom := this.IsCallable() && cx.IsConstructor(this.Object()) && this.Object() != cx.Intrinsic(engine.IntrinsicArray)
		mapped := func(v engine.Value, i int64) engine.Value {
			if mapFn.IsUndefined() {
				return v
			}
			return cx.Call(mapFn.Object(), thisArg, v, engine.Number(float64(i)))
		}
		if m := cx.GetMethod(items, cx.WellKnownSymbol(engine.SymbolIterator)); m != nil {
			// A new array is out of reach of JavaScript until it is returned, so its
			// elements are collected first while they fit in one.
			var st struct {
				a    *engine.Object
				vals []engine.Value
				i    int64
			}
			if custom {
				st.a = cx.Construct(this.Object(), nil, this.Object())
			}
			// The size of the source is only a guess of how many values the iterator gives,
			// which a replaced iterator may not give at all.
			size := engine.ElementCount(items)
			if items.IsString() {
				size = items.StringLength()
			}
			size = min(size, maxArrayFromPresize)
			cx.IterateFromMethod(items, m, func(v engine.Value) bool {
				v = mapped(v, st.i)
				if st.a == nil && st.i <= engine.MaxArrayIndex {
					if st.vals == nil {
						st.vals = make([]engine.Value, 0, max(size, 8))
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
				return engine.ObjectValue(cx.NewArray(st.vals))
			}
			setLength(cx, st.a, st.i)
			return engine.ObjectValue(st.a)
		}
		src := cx.ToObject(items)
		n := cx.LengthOfArrayLike(src)
		var a *engine.Object
		switch {
		case custom:
			a = cx.Construct(this.Object(), []engine.Value{engine.Number(float64(n))}, this.Object())
		case n <= engine.MaxArrayIndex+1:
			a = cx.ArrayCreate(uint32(n), cx.Intrinsic(engine.IntrinsicArrayPrototype))
		default:
			cx.ThrowRangeError("invalid array length")
		}
		for i := range n {
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, i), mapped(getAt(cx, src, i), i))
		}
		setLength(cx, a, n)
		return engine.ObjectValue(a)
	})

	push := method(cx, proto, "push", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		if n, ok := cx.Quiet().PushElements(o, args); ok {
			return engine.Number(float64(n))
		}
		n := cx.LengthOfArrayLike(o)
		if n+int64(len(args)) > numconv.MaxSafeInteger {
			cx.ThrowTypeError("pushing elements past the maximum length")
		}
		for _, v := range args {
			cx.Set(o, bigKey(cx, n), v)
			n++
		}
		setLength(cx, o, n)
		return engine.Number(float64(n))
	})
	// The kernel of push is the fast path of push for a single value.
	cx.SetMethodKernels(push, shortContract, nil, func(q engine.QuietContext, this, v engine.Value) (engine.Value, bool) {
		if !this.IsObject() {
			return engine.Undefined, false
		}
		vals := [...]engine.Value{v}
		n, ok := q.PushElements(this.Object(), vals[:])
		if !ok {
			return engine.Undefined, false
		}
		return engine.Number(float64(n)), true
	}, nil)
	pop := method(cx, proto, "pop", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		if v, ok := o.RemoveLastElement(); ok {
			return v
		}
		n := cx.LengthOfArrayLike(o)
		if n == 0 {
			setLength(cx, o, 0)
			return engine.Undefined
		}
		v := getAt(cx, o, n-1)
		deleteAt(cx, o, n-1)
		setLength(cx, o, n-1)
		return v
	})
	cx.SetMethodKernels(pop, shortContract, func(q engine.QuietContext, this engine.Value) (engine.Value, bool) {
		if !this.IsObject() {
			return engine.Undefined, false
		}
		return this.Object().RemoveLastElement()
	}, nil, nil)
	method(cx, proto, "shift", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		if v, ok := o.RemoveFirstElement(); ok {
			return v
		}
		n := cx.LengthOfArrayLike(o)
		if n == 0 {
			setLength(cx, o, 0)
			return engine.Undefined
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
	})
	method(cx, proto, "unshift", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		k := int64(len(args))
		if n, ok := cx.Quiet().PrependElements(o, args); ok {
			return engine.Number(float64(n))
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
		return engine.Number(float64(n + k))
	})
	method(cx, proto, "slice", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = relIndex(cx.ToIntegerOrInfinity(e), n)
		}
		if a, ok := cx.Quiet().CopyElementRange(o, start, end); ok {
			return engine.ObjectValue(a)
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
		return engine.ObjectValue(a)
	})
	method(cx, proto, "splice", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
		var items []engine.Value
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
			return engine.ObjectValue(removed)
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
		return engine.ObjectValue(a)
	})
	method(cx, proto, "concat", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		a := arraySpeciesCreate(cx, o, 0)
		n := int64(0)
		for k := -1; k < len(args); k++ {
			item := engine.ObjectValue(o)
			if k >= 0 {
				item = args[k]
			}
			spreadable := false
			if item.IsObject() {
				if s := cx.GetV(item, cx.WellKnownSymbol(engine.SymbolIsConcatSpreadable)); !s.IsUndefined() {
					spreadable = engine.ToBoolean(s)
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
		return engine.ObjectValue(a)
	})
	method(cx, proto, "join", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		sep := wtf8.String(",")
		if s := arg(args, 0); !s.IsUndefined() {
			sep = cx.ToString(s)
		}
		return joinArray(cx, &joins, o, n, sep)
	})
	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		if j := cx.GetV(engine.ObjectValue(o), cx.Intern("join")); j.IsCallable() {
			return cx.Call(j.Object(), engine.ObjectValue(o))
		}
		return strs.objectToString(cx, o)
	})
	method(cx, proto, "reverse", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		if cx.Quiet().ReverseElements(o) {
			return engine.ObjectValue(o)
		}
		for lo, hi := int64(0), n-1; lo < hi; lo, hi = lo+1, hi-1 {
			lok, hok := hasAt(cx, o, lo), false
			var lv, hv engine.Value
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
		return engine.ObjectValue(o)
	})

	for _, m := range []struct {
		name    string
		fromEnd bool
		same    bool
	}{
		{
			name: "indexOf",
		},
		{
			name:    "lastIndexOf",
			fromEnd: true,
		},
		{
			name: "includes",
			same: true,
		},
	} {
		name, fromEnd, same := m.name, m.fromEnd, m.same
		length := 1
		fn := method(cx, proto, wtf8.String(name), length, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			o := cx.ToObject(this)
			n := cx.LengthOfArrayLike(o)
			target := arg(args, 0)
			eq := engine.IsStrictlyEqual
			if same {
				eq = engine.SameValueZero
			}
			if n == 0 {
				if same {
					return engine.False
				}
				return engine.Number(-1)
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
						return engine.Number(float64(f))
					}
					if i = next; i < 0 {
						break
					}
					cx.PollAt(i)
					if e, ok := o.Element(i); ok {
						if eq(e, target) {
							return engine.Number(float64(i))
						}
						continue
					}
					if i = cx.Quiet().SkipHolesBackward(o, i, -1); i < 0 {
						break
					}
					if hasAt(cx, o, i) && eq(getAt(cx, o, i), target) {
						return engine.Number(float64(i))
					}
				}
				return engine.Number(-1)
			}
			from := relIndex(cx.ToIntegerOrInfinity(arg(args, 1)), n)
			for i := from; i < n; i++ {
				f, next := cx.Quiet().SearchElements(o, i, n, target, same)
				if f >= 0 {
					if same {
						return engine.True
					}
					return engine.Number(float64(f))
				}
				if i = next; i == n {
					break
				}
				cx.PollAt(i)
				if same {
					e, ok := o.Element(i)
					if !ok {
						if j := cx.Quiet().SkipHoles(o, i, n); j > i {
							// The indices from i to j-1 read as undefined.
							if target.IsUndefined() {
								return engine.True
							}
							i = j - 1
							continue
						}
						e = getAt(cx, o, i)
					}
					if eq(e, target) {
						return engine.True
					}
					continue
				}
				if e, ok := o.Element(i); ok {
					if eq(e, target) {
						return engine.Number(float64(i))
					}
					continue
				}
				if i = cx.Quiet().SkipHoles(o, i, n); i == n {
					break
				}
				if hasAt(cx, o, i) && eq(getAt(cx, o, i), target) {
					return engine.Number(float64(i))
				}
			}
			if same {
				return engine.False
			}
			return engine.Number(-1)
		})
		if fromEnd {
			continue
		}
		// The kernel is the search of an Array from its start.
		cx.SetMethodKernels(fn, polledContract, nil, func(q engine.QuietContext, this, target engine.Value) (engine.Value, bool) {
			found, ok := q.SearchArray(this, target, same)
			if same {
				return engine.Bool(found >= 0), ok
			}
			return engine.Number(float64(found)), ok
		}, nil)
	}

	method(cx, proto, "forEach", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "forEach", true)
		for {
			if _, _, ok := l.next(); !ok {
				return engine.Undefined
			}
		}
	})
	method(cx, proto, "map", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "map", true)
		a := arraySpeciesCreate(cx, l.o, l.n)
		for {
			_, r, ok := l.next()
			if !ok {
				return engine.ObjectValue(a)
			}
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, l.i), r)
		}
	})
	method(cx, proto, "filter", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "filter", true)
		a := arraySpeciesCreate(cx, l.o, 0)
		j := int64(0)
		for {
			v, r, ok := l.next()
			if !ok {
				return engine.ObjectValue(a)
			}
			if engine.ToBoolean(r) {
				cx.CreateDataPropertyOrThrow(a, bigKey(cx, j), v)
				j++
			}
		}
	})
	method(cx, proto, "some", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "some", true)
		for {
			_, r, ok := l.next()
			if !ok {
				return engine.False
			}
			if engine.ToBoolean(r) {
				return engine.True
			}
		}
	})
	method(cx, proto, "every", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "every", true)
		for {
			_, r, ok := l.next()
			if !ok {
				return engine.True
			}
			if !engine.ToBoolean(r) {
				return engine.False
			}
		}
	})
	method(cx, proto, "find", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "find", false)
		for {
			v, r, ok := l.next()
			if !ok {
				return engine.Undefined
			}
			if engine.ToBoolean(r) {
				return v
			}
		}
	})
	method(cx, proto, "findIndex", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(cx, this, args, "findIndex", false)
		for {
			_, r, ok := l.next()
			if !ok {
				return engine.Number(-1)
			}
			if engine.ToBoolean(r) {
				return engine.Number(float64(l.i))
			}
		}
	})
	findLast := func(name string, index bool) {
		method(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			o := cx.ToObject(this)
			n := cx.LengthOfArrayLike(o)
			cb := callback(cx, args, name)
			for i := n - 1; i >= 0; i-- {
				v := getAt(cx, o, i)
				if engine.ToBoolean(cx.Call(cb, arg(args, 1), v, engine.Number(float64(i)), engine.ObjectValue(o))) {
					if index {
						return engine.Number(float64(i))
					}
					return v
				}
			}
			if index {
				return engine.Number(-1)
			}
			return engine.Undefined
		})
	}
	findLast("findLast", false)
	findLast("findLastIndex", true)

	reduce := func(name string, right bool) {
		method(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			o := cx.ToObject(this)
			n := cx.LengthOfArrayLike(o)
			cb := callback(cx, args, name)
			i, end, d := int64(0), n, int64(1)
			if right {
				i, end, d = n-1, -1, -1
			}
			var acc engine.Value
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
				if _, ok := o.Element(i); !ok {
					if i = skipHoles(cx, o, i, end, right); i == end {
						break
					}
					if !hasAt(cx, o, i) {
						continue
					}
				}
				acc = cx.Call(cb, engine.Undefined, acc, getAt(cx, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
			}
			return acc
		})
	}
	reduce("reduce", false)
	reduce("reduceRight", true)

	method(cx, proto, "fill", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		start := relIndex(cx.ToIntegerOrInfinity(arg(args, 1)), n)
		end := n
		if e := arg(args, 2); !e.IsUndefined() {
			end = relIndex(cx.ToIntegerOrInfinity(e), n)
		}
		if cx.Quiet().FillElements(o, n, start, end, arg(args, 0)) {
			return engine.ObjectValue(o)
		}
		for i := start; i < end; i++ {
			putAt(cx, o, i, arg(args, 0))
		}
		return engine.ObjectValue(o)
	})
	method(cx, proto, "at", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		i := cx.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 {
			i += float64(n)
		}
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		return getAt(cx, o, int64(i))
	})
	method(cx, proto, "sort", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cmp := arg(args, 0)
		if !cmp.IsUndefined() && !cmp.IsCallable() {
			cx.ThrowTypeError("the comparison function must be either a function or undefined")
		}
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		var vals []engine.Value
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
			putAt(cx, o, i, engine.Undefined)
			i++
		}
		for ; i < n; i++ {
			if i = cx.Quiet().SkipHoles(o, i, n); i == n {
				break
			}
			deleteAt(cx, o, i)
		}
		return engine.ObjectValue(o)
	})
	method(cx, proto, "flat", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		l := cx.LengthOfArrayLike(o)
		depth := 1.0
		if d := arg(args, 0); !d.IsUndefined() {
			depth = cx.ToIntegerOrInfinity(d)
		}
		a := arraySpeciesCreate(cx, o, 0)
		var n int64
		flattenIntoArray(cx, a, o, l, depth, &n)
		return engine.ObjectValue(a)
	})
	method(cx, proto, "flatMap", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
			v := cx.Call(cb, arg(args, 1), getAt(cx, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
			if cx.IsArray(v) {
				flattenIntoArray(cx, a, v.Object(), cx.LengthOfArrayLike(v.Object()), 0, &n)
				continue
			}
			cx.CreateDataPropertyOrThrow(a, bigKey(cx, n), v)
			n++
		}
		return engine.ObjectValue(a)
	})

	iter := func(name string, kind engine.IterationKind) *engine.Object {
		return method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return engine.ObjectValue(cx.NewArrayIterator(cx.ToObject(this), kind))
		})
	}
	iter("keys", engine.IterateKeys)
	iter("entries", engine.IterateEntries)
	cx.SetIntrinsic(engine.IntrinsicArrayPrototypeValues, iter("values", engine.IterateValues))
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(cx.Intrinsic(engine.IntrinsicArrayPrototypeValues)), engine.FlagsHidden)
	initArrayAdditions(cx, proto)
}

// newArrayOfLength checks that n is a valid array length as ArrayCreate does, and returns the storage
// for the elements of a new array of that length.
func newArrayOfLength(cx *engine.ExecutionContext, n int64) []engine.Value {
	if n > engine.MaxArrayIndex+1 {
		cx.ThrowRangeError("invalid array length")
	}
	// The length is not trusted to allocate: the values are read one by one, which may throw.
	return make([]engine.Value, 0, min(n, 1024))
}

// initArrayAdditions defines copyWithin, toLocaleString, the methods that change an array by copy,
// and Symbol.unscopables on Array.prototype.
func initArrayAdditions(cx *engine.ExecutionContext, proto *engine.Object) {
	method(cx, proto, "copyWithin", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		return engine.ObjectValue(o)
	})
	toLocaleStringKey := cx.Intern("toLocaleString")
	var localeJoins joinGuard
	method(cx, proto, "toLocaleString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		if !localeJoins.enter(o) {
			return cx.NewString("")
		}
		defer localeJoins.leave()
		n := cx.LengthOfArrayLike(o)
		return joinElements(cx, o, n, ",", toLocaleStringKey)
	})
	method(cx, proto, "toReversed", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		vals := newArrayOfLength(cx, n)
		for i := n - 1; i >= 0; i-- {
			vals = append(vals, getAt(cx, o, i))
		}
		return cx.NewArrayOf(vals)
	})
	method(cx, proto, "toSorted", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	method(cx, proto, "toSpliced", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(this)
		n := cx.LengthOfArrayLike(o)
		start := relIndex(cx.ToIntegerOrInfinity(arg(args, 0)), n)
		var items []engine.Value
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
	})
	method(cx, proto, "with", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})

	unscopables := cx.NewObject(nil)
	for _, name := range []wtf8.String{
		"at", "copyWithin", "entries", "fill", "find", "findIndex", "findLast", "findLastIndex",
		"flat", "flatMap", "includes", "keys", "toReversed", "toSorted", "toSpliced", "values",
	} {
		cx.Define(unscopables, cx.Intern(name), engine.True, engine.FlagsData)
	}
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolUnscopables), engine.ObjectValue(unscopables), engine.FlagConfigurable)
}

// relIndex resolves a relative index against a length, clamping to the range from 0 to n.
func relIndex(f float64, n int64) int64 {
	if f < 0 {
		return int64(math.Max(f+float64(n), 0))
	}
	return int64(math.Min(f, float64(n)))
}

// sortValues sorts vals with the comparison function cmp, or as strings when cmp is undefined.
func sortValues(cx *engine.ExecutionContext, vals []engine.Value, cmp engine.Value) {
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
				mergeSortPolled(cx, vals, func(a, b engine.Value) int {
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
				mergeSortLong(cx, vals, engine.Value.StringByteLength, compareStringValues)
			}
			return
		}
		keyed := make([]keyedValue, len(vals))
		for i := 0; i < len(vals); i += engine.PollInterval {
			if i > 0 {
				cx.Poll()
			}
			lengths |= keyValues(cx, keyed[i:], vals[i:min(i+engine.PollInterval, len(vals))])
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
	mergeSort(vals, func(a, b engine.Value) int {
		if cx.ToNumber(cx.Call(cmp.Object(), engine.Undefined, a, b)) < 0 {
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
func compareStringValues(a, b engine.Value) int {
	return engine.CompareStrings(a, b)
}

// keyedValue is a value with the string it sorts by.
type keyedValue struct {
	v   engine.Value
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
func keyValues(cx *engine.ExecutionContext, keyed []keyedValue, vals []engine.Value) int {
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
		k, a, b = mergeSome(dst, a, b, engine.PollInterval, compare)
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
	o         *engine.Object
	n         int64
	cb        *engine.Object
	thisArg   engine.Value
	skipHoles bool

	// i is the index of the element of the last call of next.
	i int64
}

// newEachLoop converts this to an object, reads its length and checks the callback of the
// method name. With skipHoles, the loop skips the indices that are not present.
func newEachLoop(cx *engine.ExecutionContext, this engine.Value, args []engine.Value, name string, skipHoles bool) eachLoop {
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
func (l *eachLoop) next() (v, r engine.Value, ok bool) {
	cx, o := l.cx, l.o
	for i := l.i + 1; i < l.n; i++ {
		if l.skipHoles {
			if _, ok := o.Element(i); !ok {
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
		return v, cx.Call(l.cb, l.thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o)), true
	}
	l.i = l.n
	return engine.Undefined, engine.Undefined, false
}
