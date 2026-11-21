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
func bigKey(vm *engine.VM, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.IndexKey(uint32(i))
	}
	return engine.NamedKey(vm.Intern(numconv.NumberToString(float64(i))))
}

// getAt returns the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func getAt(vm *engine.VM, o *engine.Object, i int64) engine.Value {
	if e, ok := o.Element(i); ok {
		vm.PollAt(i)
		return e
	}
	vm.Poll()
	return vm.Get(o, bigKey(vm, i))
}

// hasAt reports whether o has a property with the integer index i. A loop over consecutive
// indices that calls it checks for an interruption regularly.
func hasAt(vm *engine.VM, o *engine.Object, i int64) bool {
	if _, ok := o.Element(i); ok {
		vm.PollAt(i)
		return true
	}
	vm.Poll()
	return vm.HasProperty(o, bigKey(vm, i))
}

// skipHoles returns the next index from i toward end, which is excluded, that may have a property
// on o, or end when there is none.
func skipHoles(vm *engine.VM, o *engine.Object, i, end int64, backward bool) int64 {
	if backward {
		return vm.SkipHolesBackward(o, i, end)
	}
	return vm.SkipHoles(o, i, end)
}

// holeSteps returns the number of consecutive indices from i, moving by dir (1 or -1) and at most
// count, at which neither o nor its prototypes are known to have a property.
func holeSteps(vm *engine.VM, o *engine.Object, i, count, dir int64) int64 {
	if _, ok := o.Element(i); ok || i > engine.MaxArrayIndex {
		return 0
	}
	if dir > 0 {
		count = min(count, engine.MaxArrayIndex+1-i)
		return vm.SkipHoles(o, i, i+count) - i
	}
	return i - vm.SkipHolesBackward(o, i, i-count)
}

// moveSteps returns how many steps, at most count, of a loop that moves the property at src to dst,
// or deletes the property at dst when src has none, change nothing. Both indices move by dir (1 or
// -1).
func moveSteps(vm *engine.VM, o *engine.Object, src, dst, count, dir int64) int64 {
	m := holeSteps(vm, o, src, count, dir)
	if m == 0 {
		return 0
	}
	return min(m, holeSteps(vm, o, dst, m, dir))
}

// putAt sets the property of o with the integer index i to v. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func putAt(vm *engine.VM, o *engine.Object, i int64, v engine.Value) {
	if o.SetElement(i, v) {
		vm.PollAt(i)
		return
	}
	vm.Poll()
	vm.Set(o, bigKey(vm, i), v)
}

// deleteAt deletes the property of o with the integer index i. A loop over consecutive indices
// that calls it checks for an interruption regularly.
func deleteAt(vm *engine.VM, o *engine.Object, i int64) {
	vm.Poll()
	k := bigKey(vm, i)
	if !vm.DeleteProp(o, k) {
		vm.ThrowTypeError("cannot delete property '" + wtf8.ToValidUTF8(vm.KeyString(k)) + "'")
	}
}

func setLength(vm *engine.VM, o *engine.Object, n int64) {
	vm.Set(o, engine.NamedKey(vm.Key(engine.KeyLength)), engine.Number(float64(n)))
}

// arraySpeciesCreate implements ArraySpeciesCreate.
func arraySpeciesCreate(vm *engine.VM, orig *engine.Object, n int64) *engine.Object {
	if a, ok := vm.ArraySpeciesCreatePlain(orig, n); ok {
		return a
	}
	if vm.IsArray(engine.ObjectValue(orig)) {
		c := vm.GetV(engine.ObjectValue(orig), vm.Key(engine.KeyConstructor))
		if c.IsObject() {
			if c.IsCallable() && vm.IsConstructor(c.Object()) && vm.IsIntrinsicOfOtherRealm(c.Object(), engine.IntrinsicArray) {
				c = engine.Undefined
			} else if s := vm.GetV(c, vm.WellKnownSymbol(engine.SymbolSpecies)); s.IsNull() {
				c = engine.Undefined
			} else {
				c = s
			}
		}
		if !c.IsUndefined() && !(c.IsObject() && c.Object() == vm.Intrinsic(engine.IntrinsicArray)) {
			if !c.IsCallable() || !vm.IsConstructor(c.Object()) {
				vm.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
			}
			return vm.Construct(c.Object(), []engine.Value{engine.Number(float64(n))}, c.Object())
		}
	}
	if n > engine.MaxArrayIndex+1 {
		vm.ThrowRangeError("invalid array length")
	}
	return vm.ArrayCreate(uint32(n), vm.Intrinsic(engine.IntrinsicArrayPrototype))
}

func callback(vm *engine.VM, args []engine.Value, what string) *engine.Object {
	f := arg(args, 0)
	if !f.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(f) + " is not a function")
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
func joinArray(vm *engine.VM, g *joinGuard, o *engine.Object, n int64, sep wtf8.String) engine.Value {
	if !g.enter(o) {
		return vm.NewString("")
	}
	defer g.leave()
	return joinElements(vm, o, n, sep, nil)
}

// writeString writes ToString(v) to b.
func writeString(vm *engine.VM, b *wtf8.Buffer, v engine.Value) {
	if v.IsString() {
		v.WriteStringTo(b)
		return
	}
	if v.IsNumber() {
		var buf [32]byte
		b.WriteStringBytes(numconv.AppendNumber(buf[:0], v.Number()))
		return
	}
	b.WriteString(vm.ToString(v))
}

// joinElements joins the strings of the first n elements of the object o that are neither
// undefined nor null, with sep between elements. The string of an element e is ToString(e),
// or ToString(Invoke(e, method)) when method is not nil.
func joinElements(vm *engine.VM, o *engine.Object, n int64, sep wtf8.String, method *engine.Object) engine.Value {
	var b wtf8.Buffer
	for i := int64(0); i < n; {
		if i > 0 {
			vm.Poll()
		}
		i = joinRun(vm, &b, o, i, min(n, i+engine.PollInterval), n, sep, method)
	}
	return vm.NewString(b.String())
}

// joinRun writes the elements of o from i up to end, or further when holes follow, to b as
// joinElements joins the first n elements, and returns the index after the last one it wrote.
func joinRun(vm *engine.VM, b *wtf8.Buffer, o *engine.Object, i, end, n int64, sep wtf8.String, method *engine.Object) int64 {
	for ; i < end; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		e, ok := o.Element(i)
		if !ok {
			if j := vm.SkipHoles(o, i, n); j > i {
				// The indices from i to j-1 read as undefined.
				writeSeparators(vm, b, sep, j-1-i)
				i = j - 1
				e = engine.Undefined
			} else {
				e = getAt(vm, o, i)
			}
		}
		switch {
		case e.IsNullish():
		case method != nil:
			b.WriteString(vm.ToString(vm.CallValue(vm.GetV(e, method), e)))
		default:
			writeString(vm, b, e)
		}
		if b.Len() > engine.MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
	}
	return i
}

// writeSeparators writes sep count times to b, and throws a RangeError instead when b would get
// longer than MaxStringLength.
func writeSeparators(vm *engine.VM, b *wtf8.Buffer, sep wtf8.String, count int64) {
	// A separator after another one adds len(sep) bytes, or len(sep)-2 when the two join a
	// surrogate pair. The first one may also join a pair with what b ends with.
	per := int64(len(sep))
	if wtf8.JoinsPair(sep, sep) {
		per -= 2
	}
	if int64(b.Len())+count*per-2 > engine.MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
	writeRepeated(vm, b, sep, int(count))
	if b.Len() > engine.MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
}

func initArray(vm *engine.VM, strs *objectStrings) {
	proto := vm.Intrinsic(engine.IntrinsicArrayPrototype)
	var joins joinGuard
	ctor := constructor(vm, "Array", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		proto := vm.Intrinsic(engine.IntrinsicArrayPrototype)
		if nt := vm.GetNewTarget(); nt != nil && nt != vm.Intrinsic(engine.IntrinsicArray) {
			proto = vm.GetPrototypeFromConstructor(nt, engine.IntrinsicArrayPrototype)
		}
		if len(args) == 1 {
			if n := args[0]; n.IsNumber() {
				u := numconv.ToUint32(n.Number())
				if float64(u) != n.Number() {
					vm.ThrowRangeError("invalid array length")
				}
				return engine.ObjectValue(vm.ArrayCreate(u, proto))
			}
		}
		var a *engine.Object
		if len(args) == 0 {
			a = vm.NewEmptyArrayWithRoom(4)
		} else {
			a = vm.NewArray(append([]engine.Value(nil), args...))
		}
		if proto != vm.Intrinsic(engine.IntrinsicArrayPrototype) {
			vm.SetPrototypeOf(a, proto)
		}
		return engine.ObjectValue(a)
	})
	vm.SetIntrinsic(engine.IntrinsicArray, ctor)
	speciesGetter := vm.NewNative("get [Symbol.species]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	vm.SetIntrinsic(engine.IntrinsicArraySpeciesGetter, speciesGetter)
	vm.DefineAccessor(ctor, vm.WellKnownSymbol(engine.SymbolSpecies), speciesGetter, nil, engine.FlagConfigurable)

	method(vm, ctor, "isArray", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(vm.IsArray(arg(args, 0)))
	})
	method(vm, ctor, "of", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if this.IsCallable() && vm.IsConstructor(this.Object()) && this.Object() != vm.Intrinsic(engine.IntrinsicArray) {
			a := vm.Construct(this.Object(), []engine.Value{engine.Number(float64(len(args)))}, this.Object())
			for i, v := range args {
				vm.CreateDataPropertyOrThrow(a, engine.IndexKey(uint32(i)), v)
			}
			setLength(vm, a, int64(len(args)))
			return engine.ObjectValue(a)
		}
		return engine.ObjectValue(vm.NewArray(append([]engine.Value(nil), args...)))
	})
	method(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		items, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
		if !mapFn.IsUndefined() && !mapFn.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(mapFn) + " is not a function")
		}
		custom := this.IsCallable() && vm.IsConstructor(this.Object()) && this.Object() != vm.Intrinsic(engine.IntrinsicArray)
		mapped := func(v engine.Value, i int64) engine.Value {
			if mapFn.IsUndefined() {
				return v
			}
			return vm.Call(mapFn.Object(), thisArg, v, engine.Number(float64(i)))
		}
		if m := vm.GetMethod(items, vm.WellKnownSymbol(engine.SymbolIterator)); m != nil {
			// A new array is out of reach of JavaScript until it is returned, so its
			// elements are collected first while they fit in one.
			var st struct {
				a    *engine.Object
				vals []engine.Value
				i    int64
			}
			if custom {
				st.a = vm.Construct(this.Object(), nil, this.Object())
			}
			// The size of the source is only a guess of how many values the iterator gives,
			// which a replaced iterator may not give at all.
			size := engine.ElementCount(items)
			if items.IsString() {
				size = items.StringLength()
			}
			size = min(size, maxArrayFromPresize)
			vm.IterateFromMethod(items, m, func(v engine.Value) bool {
				v = mapped(v, st.i)
				if st.a == nil && st.i <= engine.MaxArrayIndex {
					if st.vals == nil {
						st.vals = make([]engine.Value, 0, max(size, 8))
					}
					st.vals = append(st.vals, v)
				} else {
					if st.a == nil {
						st.a = vm.NewArray(st.vals)
					}
					vm.CreateDataPropertyOrThrow(st.a, bigKey(vm, st.i), v)
				}
				st.i++
				return true
			})
			if st.a == nil {
				return engine.ObjectValue(vm.NewArray(st.vals))
			}
			setLength(vm, st.a, st.i)
			return engine.ObjectValue(st.a)
		}
		src := vm.ToObject(items)
		n := vm.LengthOfArrayLike(src)
		var a *engine.Object
		switch {
		case custom:
			a = vm.Construct(this.Object(), []engine.Value{engine.Number(float64(n))}, this.Object())
		case n <= engine.MaxArrayIndex+1:
			a = vm.ArrayCreate(uint32(n), vm.Intrinsic(engine.IntrinsicArrayPrototype))
		default:
			vm.ThrowRangeError("invalid array length")
		}
		for i := range n {
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, i), mapped(getAt(vm, src, i), i))
		}
		setLength(vm, a, n)
		return engine.ObjectValue(a)
	})

	method(vm, proto, "push", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if n, ok := vm.PushElements(o, args); ok {
			return engine.Number(float64(n))
		}
		n := vm.LengthOfArrayLike(o)
		if n+int64(len(args)) > numconv.MaxSafeInteger {
			vm.ThrowTypeError("pushing elements past the maximum length")
		}
		for _, v := range args {
			vm.Set(o, bigKey(vm, n), v)
			n++
		}
		setLength(vm, o, n)
		return engine.Number(float64(n))
	})
	method(vm, proto, "pop", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if v, ok := o.RemoveLastElement(); ok {
			return v
		}
		n := vm.LengthOfArrayLike(o)
		if n == 0 {
			setLength(vm, o, 0)
			return engine.Undefined
		}
		v := getAt(vm, o, n-1)
		deleteAt(vm, o, n-1)
		setLength(vm, o, n-1)
		return v
	})
	method(vm, proto, "shift", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if v, ok := o.RemoveFirstElement(); ok {
			return v
		}
		n := vm.LengthOfArrayLike(o)
		if n == 0 {
			setLength(vm, o, 0)
			return engine.Undefined
		}
		first := getAt(vm, o, 0)
		for i := int64(1); i < n; i++ {
			if hasAt(vm, o, i) {
				putAt(vm, o, i-1, getAt(vm, o, i))
			} else {
				deleteAt(vm, o, i-1)
				i += moveSteps(vm, o, i+1, i, n-i-1, 1)
			}
		}
		deleteAt(vm, o, n-1)
		setLength(vm, o, n-1)
		return first
	})
	method(vm, proto, "unshift", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		k := int64(len(args))
		if n, ok := vm.PrependElements(o, args); ok {
			return engine.Number(float64(n))
		}
		n := vm.LengthOfArrayLike(o)
		if k > 0 {
			if n+k > numconv.MaxSafeInteger {
				vm.ThrowTypeError("unshifting elements past the maximum length")
			}
			for i := n - 1; i >= 0; i-- {
				if hasAt(vm, o, i) {
					putAt(vm, o, i+k, getAt(vm, o, i))
				} else {
					deleteAt(vm, o, i+k)
					i -= moveSteps(vm, o, i-1, i+k-1, i, -1)
				}
			}
			for i, v := range args {
				putAt(vm, o, int64(i), v)
			}
		}
		setLength(vm, o, n+k)
		return engine.Number(float64(n + k))
	})
	method(vm, proto, "slice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		start := relIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = relIndex(vm.ToIntegerOrInfinity(e), n)
		}
		if a, ok := vm.CopyElementRange(o, start, end); ok {
			return engine.ObjectValue(a)
		}
		a := arraySpeciesCreate(vm, o, max(end-start, 0))
		for i := start; i < end; i++ {
			if i = vm.SkipHoles(o, i, end); i == end {
				break
			}
			if hasAt(vm, o, i) {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, i-start), getAt(vm, o, i))
			}
		}
		setLength(vm, a, max(end-start, 0))
		return engine.ObjectValue(a)
	})
	method(vm, proto, "splice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		start := relIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		var items []engine.Value
		del := int64(0)
		switch {
		case len(args) == 1:
			del = n - start
		case len(args) >= 2:
			del = int64(math.Min(math.Max(vm.ToIntegerOrInfinity(args[1]), 0), float64(n-start)))
			items = args[2:]
		}
		k := int64(len(items))
		if n+k-del > numconv.MaxSafeInteger {
			vm.ThrowTypeError("splicing elements past the maximum length")
		}
		if removed, ok := vm.SpliceElements(o, n, start, del, items); ok {
			return engine.ObjectValue(removed)
		}
		a := arraySpeciesCreate(vm, o, del)
		for i := int64(0); i < del; i++ {
			if hasAt(vm, o, start+i) {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, i), getAt(vm, o, start+i))
			} else {
				i += holeSteps(vm, o, start+i+1, del-i-1, 1)
			}
		}
		setLength(vm, a, del)
		switch {
		case k < del:
			for i := start; i < n-del; i++ {
				if hasAt(vm, o, i+del) {
					putAt(vm, o, i+k, getAt(vm, o, i+del))
				} else {
					deleteAt(vm, o, i+k)
					i += moveSteps(vm, o, i+del+1, i+k+1, n-del-i-1, 1)
				}
			}
			for i := n; i > n-del+k; i-- {
				deleteAt(vm, o, i-1)
				i -= holeSteps(vm, o, i-2, i-1-(n-del+k), -1)
			}
		case k > del:
			for i := n - del; i > start; i-- {
				if hasAt(vm, o, i+del-1) {
					putAt(vm, o, i+k-1, getAt(vm, o, i+del-1))
				} else {
					deleteAt(vm, o, i+k-1)
					i -= moveSteps(vm, o, i+del-2, i+k-2, i-start-1, -1)
				}
			}
		}
		for i, v := range items {
			putAt(vm, o, start+int64(i), v)
		}
		setLength(vm, o, n-del+k)
		return engine.ObjectValue(a)
	})
	method(vm, proto, "concat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		a := arraySpeciesCreate(vm, o, 0)
		n := int64(0)
		for k := -1; k < len(args); k++ {
			item := engine.ObjectValue(o)
			if k >= 0 {
				item = args[k]
			}
			spreadable := false
			if item.IsObject() {
				if s := vm.GetV(item, vm.WellKnownSymbol(engine.SymbolIsConcatSpreadable)); !s.IsUndefined() {
					spreadable = engine.ToBoolean(s)
				} else {
					spreadable = vm.IsArray(item)
				}
			}
			if !spreadable {
				if n >= numconv.MaxSafeInteger {
					vm.ThrowTypeError("array too long")
				}
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, n), item)
				n++
				continue
			}
			l := vm.LengthOfArrayLike(item.Object())
			if n+l > numconv.MaxSafeInteger {
				vm.ThrowTypeError("array too long")
			}
			if vm.AppendElementsOf(a, n, item.Object()) {
				n += l
				continue
			}
			for i := int64(0); i < l; i++ {
				if i = vm.SkipHoles(item.Object(), i, l); i == l {
					break
				}
				if hasAt(vm, item.Object(), i) {
					vm.CreateDataPropertyOrThrow(a, bigKey(vm, n+i), getAt(vm, item.Object(), i))
				}
			}
			n += l
		}
		setLength(vm, a, n)
		return engine.ObjectValue(a)
	})
	method(vm, proto, "join", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		sep := wtf8.String(",")
		if s := arg(args, 0); !s.IsUndefined() {
			sep = vm.ToString(s)
		}
		return joinArray(vm, &joins, o, n, sep)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if j := vm.GetV(engine.ObjectValue(o), vm.Intern("join")); j.IsCallable() {
			return vm.Call(j.Object(), engine.ObjectValue(o))
		}
		return strs.objectToString(vm, o)
	})
	method(vm, proto, "reverse", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		if vm.ReverseElements(o) {
			return engine.ObjectValue(o)
		}
		for lo, hi := int64(0), n-1; lo < hi; lo, hi = lo+1, hi-1 {
			lok, hok := hasAt(vm, o, lo), false
			var lv, hv engine.Value
			if lok {
				lv = getAt(vm, o, lo)
			}
			hok = hasAt(vm, o, hi)
			if hok {
				hv = getAt(vm, o, hi)
			}
			switch {
			case lok && hok:
				putAt(vm, o, lo, hv)
				putAt(vm, o, hi, lv)
			case hok:
				putAt(vm, o, lo, hv)
				deleteAt(vm, o, hi)
			case lok:
				deleteAt(vm, o, lo)
				putAt(vm, o, hi, lv)
			default:
				// A pair of holes changes nothing, and so do the pairs of holes that follow.
				if m := holeSteps(vm, o, lo+1, (hi-lo-1)/2, 1); m > 0 {
					m = min(m, holeSteps(vm, o, hi-1, m, -1))
					lo, hi = lo+m, hi-m
				}
			}
		}
		return engine.ObjectValue(o)
	})

	search := func(name string, fromEnd, same bool) {
		length := 1
		method(vm, proto, wtf8.String(name), length, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			n := vm.LengthOfArrayLike(o)
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
					f := vm.ToIntegerOrInfinity(args[1])
					if f < 0 {
						from = n + int64(math.Max(f, float64(-n-1)))
					} else {
						from = int64(math.Min(f, float64(n-1)))
					}
				}
				for i := from; i >= 0; i-- {
					f, next := vm.SearchElements(o, i, -1, target, false)
					if f >= 0 {
						return engine.Number(float64(f))
					}
					if i = next; i < 0 {
						break
					}
					vm.PollAt(i)
					if e, ok := o.Element(i); ok {
						if eq(e, target) {
							return engine.Number(float64(i))
						}
						continue
					}
					if i = vm.SkipHolesBackward(o, i, -1); i < 0 {
						break
					}
					if hasAt(vm, o, i) && eq(getAt(vm, o, i), target) {
						return engine.Number(float64(i))
					}
				}
				return engine.Number(-1)
			}
			from := relIndex(vm.ToIntegerOrInfinity(arg(args, 1)), n)
			for i := from; i < n; i++ {
				f, next := vm.SearchElements(o, i, n, target, same)
				if f >= 0 {
					if same {
						return engine.True
					}
					return engine.Number(float64(f))
				}
				if i = next; i == n {
					break
				}
				vm.PollAt(i)
				if same {
					e, ok := o.Element(i)
					if !ok {
						if j := vm.SkipHoles(o, i, n); j > i {
							// The indices from i to j-1 read as undefined.
							if target.IsUndefined() {
								return engine.True
							}
							i = j - 1
							continue
						}
						e = getAt(vm, o, i)
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
				if i = vm.SkipHoles(o, i, n); i == n {
					break
				}
				if hasAt(vm, o, i) && eq(getAt(vm, o, i), target) {
					return engine.Number(float64(i))
				}
			}
			if same {
				return engine.False
			}
			return engine.Number(-1)
		})
	}
	search("indexOf", false, false)
	search("lastIndexOf", true, false)
	search("includes", false, true)

	method(vm, proto, "forEach", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "forEach", true)
		for {
			if _, _, ok := l.next(); !ok {
				return engine.Undefined
			}
		}
	})
	method(vm, proto, "map", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "map", true)
		a := arraySpeciesCreate(vm, l.o, l.n)
		for {
			_, r, ok := l.next()
			if !ok {
				return engine.ObjectValue(a)
			}
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, l.i), r)
		}
	})
	method(vm, proto, "filter", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "filter", true)
		a := arraySpeciesCreate(vm, l.o, 0)
		j := int64(0)
		for {
			v, r, ok := l.next()
			if !ok {
				return engine.ObjectValue(a)
			}
			if engine.ToBoolean(r) {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, j), v)
				j++
			}
		}
	})
	method(vm, proto, "some", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "some", true)
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
	method(vm, proto, "every", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "every", true)
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
	method(vm, proto, "find", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "find", false)
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
	method(vm, proto, "findIndex", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		l := newEachLoop(vm, this, args, "findIndex", false)
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
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			n := vm.LengthOfArrayLike(o)
			cb := callback(vm, args, name)
			for i := n - 1; i >= 0; i-- {
				v := getAt(vm, o, i)
				if engine.ToBoolean(vm.Call(cb, arg(args, 1), v, engine.Number(float64(i)), engine.ObjectValue(o))) {
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
		method(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			n := vm.LengthOfArrayLike(o)
			cb := callback(vm, args, name)
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
					if i = skipHoles(vm, o, i, end, right); i == end {
						break
					}
					if hasAt(vm, o, i) {
						acc = getAt(vm, o, i)
						found = true
						i += d
						break
					}
				}
				if !found {
					vm.ThrowTypeError("reduce of empty array with no initial value")
				}
			}
			for ; i != end; i += d {
				if _, ok := o.Element(i); !ok {
					if i = skipHoles(vm, o, i, end, right); i == end {
						break
					}
					if !hasAt(vm, o, i) {
						continue
					}
				}
				acc = vm.Call(cb, engine.Undefined, acc, getAt(vm, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
			}
			return acc
		})
	}
	reduce("reduce", false)
	reduce("reduceRight", true)

	method(vm, proto, "fill", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		start := relIndex(vm.ToIntegerOrInfinity(arg(args, 1)), n)
		end := n
		if e := arg(args, 2); !e.IsUndefined() {
			end = relIndex(vm.ToIntegerOrInfinity(e), n)
		}
		if vm.FillElements(o, n, start, end, arg(args, 0)) {
			return engine.ObjectValue(o)
		}
		for i := start; i < end; i++ {
			putAt(vm, o, i, arg(args, 0))
		}
		return engine.ObjectValue(o)
	})
	method(vm, proto, "at", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		i := vm.ToIntegerOrInfinity(arg(args, 0))
		if i < 0 {
			i += float64(n)
		}
		if i < 0 || i >= float64(n) {
			return engine.Undefined
		}
		return getAt(vm, o, int64(i))
	})
	method(vm, proto, "sort", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cmp := arg(args, 0)
		if !cmp.IsUndefined() && !cmp.IsCallable() {
			vm.ThrowTypeError("the comparison function must be either a function or undefined")
		}
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		var vals []engine.Value
		undefs := int64(0)
		for i := int64(0); i < n; i++ {
			if i = vm.SkipHoles(o, i, n); i == n {
				break
			}
			if !hasAt(vm, o, i) {
				continue
			}
			v := getAt(vm, o, i)
			if v.IsUndefined() {
				undefs++
				continue
			}
			vals = append(vals, v)
		}
		sortValues(vm, vals, cmp)
		i := int64(0)
		for _, v := range vals {
			putAt(vm, o, i, v)
			i++
		}
		for ; undefs > 0; undefs-- {
			putAt(vm, o, i, engine.Undefined)
			i++
		}
		for ; i < n; i++ {
			if i = vm.SkipHoles(o, i, n); i == n {
				break
			}
			deleteAt(vm, o, i)
		}
		return engine.ObjectValue(o)
	})
	// flatten implements FlattenIntoArray for the first l elements of src without a mapper function.
	// The nested arrays are kept on an explicit stack, as their depth is unbounded.
	flatten := func(a *engine.Object, src *engine.Object, l int64, depth float64, n *int64) {
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
			if t.i = vm.SkipHoles(t.src, t.i, t.l); t.i >= t.l {
				stack = stack[:len(stack)-1]
				continue
			}
			i := t.i
			t.i++
			if !hasAt(vm, t.src, i) {
				continue
			}
			v := getAt(vm, t.src, i)
			if t.depth > 0 && vm.IsArray(v) {
				stack = append(stack, level{
					src:   v.Object(),
					l:     vm.LengthOfArrayLike(v.Object()),
					depth: t.depth - 1,
				})
				continue
			}
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, *n), v)
			*n++
		}
	}
	method(vm, proto, "flat", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		l := vm.LengthOfArrayLike(o)
		depth := 1.0
		if d := arg(args, 0); !d.IsUndefined() {
			depth = vm.ToIntegerOrInfinity(d)
		}
		a := arraySpeciesCreate(vm, o, 0)
		var n int64
		flatten(a, o, l, depth, &n)
		return engine.ObjectValue(a)
	})
	method(vm, proto, "flatMap", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		l := vm.LengthOfArrayLike(o)
		cb := callback(vm, args, "flatMap")
		a := arraySpeciesCreate(vm, o, 0)
		var n int64
		for i := int64(0); i < l; i++ {
			if i = vm.SkipHoles(o, i, l); i == l {
				break
			}
			if !hasAt(vm, o, i) {
				continue
			}
			v := vm.Call(cb, arg(args, 1), getAt(vm, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
			if vm.IsArray(v) {
				flatten(a, v.Object(), vm.LengthOfArrayLike(v.Object()), 0, &n)
				continue
			}
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, n), v)
			n++
		}
		return engine.ObjectValue(a)
	})

	iter := func(name string, kind engine.IterationKind) *engine.Object {
		return method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return engine.ObjectValue(vm.NewArrayIterator(vm.ToObject(this), kind))
		})
	}
	iter("keys", engine.IterateKeys)
	iter("entries", engine.IterateEntries)
	vm.SetIntrinsic(engine.IntrinsicArrayPrototypeValues, iter("values", engine.IterateValues))
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolIterator), engine.ObjectValue(vm.Intrinsic(engine.IntrinsicArrayPrototypeValues)), engine.FlagsHidden)
	initArrayAdditions(vm, proto)
}

// newArrayOfLength checks that n is a valid array length as ArrayCreate does, and returns the storage
// for the elements of a new array of that length.
func newArrayOfLength(vm *engine.VM, n int64) []engine.Value {
	if n > engine.MaxArrayIndex+1 {
		vm.ThrowRangeError("invalid array length")
	}
	// The length is not trusted to allocate: the values are read one by one, which may throw.
	return make([]engine.Value, 0, min(n, 1024))
}

// initArrayAdditions defines copyWithin, toLocaleString, the methods that change an array by copy,
// and Symbol.unscopables on Array.prototype.
func initArrayAdditions(vm *engine.VM, proto *engine.Object) {
	method(vm, proto, "copyWithin", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		to := relIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
		from := relIndex(vm.ToIntegerOrInfinity(arg(args, 1)), n)
		end := n
		if e := arg(args, 2); !e.IsUndefined() {
			end = relIndex(vm.ToIntegerOrInfinity(e), n)
		}
		count := min(end-from, n-to)
		dir := int64(1)
		if from < to && to < from+count {
			dir = -1
			from += count - 1
			to += count - 1
		}
		for ; count > 0; count-- {
			if hasAt(vm, o, from) {
				putAt(vm, o, to, getAt(vm, o, from))
			} else {
				deleteAt(vm, o, to)
				m := moveSteps(vm, o, from+dir, to+dir, count-1, dir)
				from += dir * m
				to += dir * m
				count -= m
			}
			from += dir
			to += dir
		}
		return engine.ObjectValue(o)
	})
	toLocaleStringKey := vm.Intern("toLocaleString")
	var localeJoins joinGuard
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if !localeJoins.enter(o) {
			return vm.NewString("")
		}
		defer localeJoins.leave()
		n := vm.LengthOfArrayLike(o)
		return joinElements(vm, o, n, ",", toLocaleStringKey)
	})
	method(vm, proto, "toReversed", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		vals := newArrayOfLength(vm, n)
		for i := n - 1; i >= 0; i-- {
			vals = append(vals, getAt(vm, o, i))
		}
		return vm.NewArrayOf(vals)
	})
	method(vm, proto, "toSorted", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		cmp := arg(args, 0)
		if !cmp.IsUndefined() && !cmp.IsCallable() {
			vm.ThrowTypeError("the comparison function must be either a function or undefined")
		}
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		vals := newArrayOfLength(vm, n)
		undefs := 0
		for i := range n {
			v := getAt(vm, o, i)
			if v.IsUndefined() {
				undefs++
				continue
			}
			vals = append(vals, v)
		}
		sortValues(vm, vals, cmp)
		vals = slices.Grow(vals, undefs)
		start := len(vals)
		vals = vals[:start+undefs]
		clear(vals[start:])
		return vm.NewArrayOf(vals)
	})
	method(vm, proto, "toSpliced", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		start := relIndex(vm.ToIntegerOrInfinity(arg(args, 0)), n)
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
			skip = int64(math.Min(math.Max(vm.ToIntegerOrInfinity(args[1]), 0), float64(n-start)))
		}
		newLen := n + int64(len(items)) - skip
		if newLen > numconv.MaxSafeInteger {
			vm.ThrowTypeError("invalid array length")
		}
		vals := newArrayOfLength(vm, newLen)
		for i := range start {
			vals = append(vals, getAt(vm, o, i))
		}
		vals = append(vals, items...)
		for i := start + skip; i < n; i++ {
			vals = append(vals, getAt(vm, o, i))
		}
		return vm.NewArrayOf(vals)
	})
	method(vm, proto, "with", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		rel := vm.ToIntegerOrInfinity(arg(args, 0))
		idx := rel
		if rel < 0 {
			idx = float64(n) + rel
		}
		if idx < 0 || idx >= float64(n) {
			vm.ThrowRangeError("invalid index")
		}
		vals := newArrayOfLength(vm, n)
		for i := range n {
			if i == int64(idx) {
				vals = append(vals, arg(args, 1))
				continue
			}
			vals = append(vals, getAt(vm, o, i))
		}
		return vm.NewArrayOf(vals)
	})

	unscopables := vm.NewObject(nil)
	for _, name := range []wtf8.String{
		"at", "copyWithin", "entries", "fill", "find", "findIndex", "findLast", "findLastIndex",
		"flat", "flatMap", "includes", "keys", "toReversed", "toSorted", "toSpliced", "values",
	} {
		vm.Define(unscopables, vm.Intern(name), engine.True, engine.FlagsData)
	}
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolUnscopables), engine.ObjectValue(unscopables), engine.FlagConfigurable)
}

// relIndex resolves a relative index against a length, clamping to the range from 0 to n.
func relIndex(f float64, n int64) int64 {
	if f < 0 {
		return int64(math.Max(f+float64(n), 0))
	}
	return int64(math.Min(f, float64(n)))
}

// sortValues sorts vals with the comparison function cmp, or as strings when cmp is undefined.
func sortValues(vm *engine.VM, vals []engine.Value, cmp engine.Value) {
	if len(vals) < 2 {
		return
	}
	if cmp.IsUndefined() {
		allStrings := true
		for i, v := range vals {
			vm.PollAt(int64(i))
			if v.IsObject() || v.IsSymbol() {
				// Object conversion can run code and symbol conversion throws.
				mergeSortPolled(vm, vals, func(a, b engine.Value) int {
					return wtf8.Compare(vm.ToString(a), vm.ToString(b))
				})
				return
			}
			allStrings = allStrings && v.IsString()
		}
		if allStrings {
			mergeSortPolled(vm, vals, compareStringValues)
			return
		}
		keyed := make([]keyedValue, len(vals))
		for i := 0; i < len(vals); i += engine.PollInterval {
			if i > 0 {
				vm.Poll()
			}
			keyValues(vm, keyed[i:], vals[i:min(i+engine.PollInterval, len(vals))])
		}
		mergeSortPolled(vm, keyed, compareKeyedValues)
		for i := range vals {
			vals[i] = keyed[i].v
		}
		return
	}
	mergeSort(vals, func(a, b engine.Value) int {
		if vm.ToNumber(vm.Call(cmp.Object(), engine.Undefined, a, b)) < 0 {
			return -1
		}
		return 0
	})
}

func mergeSortPolled[E any](vm *engine.VM, s []E, compare func(a, b E) int) {
	sortPolled(vm, s, sortRunLength, func(run []E) {
		mergeSort(run, compare)
	}, compare)
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

// compareKeyedValues compares the keys of a and b by their code units.
func compareKeyedValues(a, b keyedValue) int {
	return wtf8.Compare(a.key, b.key)
}

// keyValues sets the elements of keyed to vals with their strings.
func keyValues(vm *engine.VM, keyed []keyedValue, vals []engine.Value) {
	for i, v := range vals {
		keyed[i] = keyedValue{
			v:   v,
			key: vm.ToString(v),
		}
	}
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
func sortPolled[E any](vm *engine.VM, s []E, run int, sortRun func(s []E), compare func(a, b E) int) {
	n := len(s)
	if n <= run {
		sortRun(s)
		return
	}
	for i := 0; i < n; i += run {
		vm.Poll()
		sortRun(s[i:min(i+run, n)])
	}
	src, dst := s, make([]E, n)
	var inBuf bool
	for w := run; w < n; w *= 2 {
		for i := 0; i < n; i += 2 * w {
			mergeRuns(vm, dst[i:min(i+2*w, n)], src[i:min(i+w, n)], src[min(i+w, n):min(i+2*w, n)], compare)
		}
		src, dst = dst, src
		inBuf = !inBuf
	}
	if inBuf {
		engine.CopyPolled(vm, s, src)
	}
}

// mergeRuns merges the sorted a and b into dst, which has room for both, taking the elements of a
// first among equal ones. It checks for an interruption now and then.
func mergeRuns[E any](vm *engine.VM, dst, a, b []E, compare func(a, b E) int) {
	for len(a) > 0 && len(b) > 0 {
		var k int
		k, a, b = mergeSome(dst, a, b, engine.PollInterval, compare)
		dst = dst[k:]
		vm.Poll()
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
	vm        *engine.VM
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
func newEachLoop(vm *engine.VM, this engine.Value, args []engine.Value, name string, skipHoles bool) eachLoop {
	o := vm.ToObject(this)
	n := vm.LengthOfArrayLike(o)
	return eachLoop{
		vm:        vm,
		o:         o,
		n:         n,
		cb:        callback(vm, args, name),
		thisArg:   arg(args, 1),
		skipHoles: skipHoles,
		i:         -1,
	}
}

// next calls the callback with the next element. It returns the element and the result of
// the callback, or false when no element is left.
func (l *eachLoop) next() (v, r engine.Value, ok bool) {
	vm, o := l.vm, l.o
	for i := l.i + 1; i < l.n; i++ {
		if l.skipHoles {
			if _, ok := o.Element(i); !ok {
				if i = vm.SkipHoles(o, i, l.n); i == l.n {
					break
				}
				if !hasAt(vm, o, i) {
					continue
				}
			}
		}
		l.i = i
		v := getAt(vm, o, i)
		return v, vm.Call(l.cb, l.thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o)), true
	}
	l.i = l.n
	return engine.Undefined, engine.Undefined, false
}
