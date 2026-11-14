// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtins

import (
	"math"
	"slices"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// bigKey returns the property key for an integer index, which may be beyond the array indices.
func bigKey(vm *engine.VM, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.IndexKey(uint32(i))
	}
	return engine.NamedKey(vm.Intern(numconv.NumberToString(float64(i))))
}

func getAt(vm *engine.VM, o *engine.Object, i int64) engine.Value {
	if e, ok := o.Element(i); ok {
		return e
	}
	vm.Poll()
	return vm.Get(o, bigKey(vm, i))
}

func hasAt(vm *engine.VM, o *engine.Object, i int64) bool {
	if _, ok := o.Element(i); ok {
		return true
	}
	vm.Poll()
	return vm.HasProperty(o, bigKey(vm, i))
}

func putAt(vm *engine.VM, o *engine.Object, i int64, v engine.Value) {
	if o.SetElement(i, v) {
		return
	}
	vm.Set(o, bigKey(vm, i), v)
}

func deleteAt(vm *engine.VM, o *engine.Object, i int64) {
	k := bigKey(vm, i)
	if !vm.DeleteProp(o, k) {
		vm.ThrowTypeError("cannot delete property '" + vm.KeyString(k) + "'")
	}
}

func setLength(vm *engine.VM, o *engine.Object, n int64) {
	vm.Set(o, engine.NamedKey(vm.K.Length), engine.Number(float64(n)))
}

// arraySpeciesCreate implements ArraySpeciesCreate.
func arraySpeciesCreate(vm *engine.VM, orig *engine.Object, n int64) *engine.Object {
	if orig.IsArray() {
		c := vm.GetV(engine.ObjectValue(orig), vm.K.Constructor)
		if c.IsObject() {
			if c.Object() == vm.ArrayCtor {
				c = engine.Undefined
			} else if s := vm.GetV(c, vm.K.SymSpecies); s.IsNull() {
				c = engine.Undefined
			} else {
				c = s
			}
		}
		if !c.IsUndefined() {
			if !c.IsCallable() || !vm.IsConstructor(c.Object()) {
				vm.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
			}
			return vm.Construct(c.Object(), []engine.Value{engine.Number(float64(n))}, c.Object())
		}
	}
	if n > engine.MaxArrayIndex+1 {
		vm.ThrowRangeError("invalid array length")
	}
	return vm.ArrayCreate(uint32(n), vm.ArrayProto)
}

func callback(vm *engine.VM, args []engine.Value, what string) *engine.Object {
	f := arg(args, 0)
	if !f.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(f) + " is not a function")
	}
	return f.Object()
}

// joinArray implements Array.prototype.join for the first n elements of the object o.
func joinArray(vm *engine.VM, o *engine.Object, n int64, sep string) engine.Value {
	if slices.Contains(vm.Joining, o) {
		return vm.EmptyString
	}
	vm.Joining = append(vm.Joining, o)
	defer func() {
		vm.Joining = vm.Joining[:len(vm.Joining)-1]
	}()
	var b wtf8.Builder
	for i := range n {
		if i > 0 {
			b.WriteString(sep)
		}
		if e := getAt(vm, o, i); !e.IsNullish() {
			b.WriteString(vm.ToString(e))
		}
		if b.Len() > engine.MaxStringLength {
			vm.ThrowRangeError("invalid string length")
		}
	}
	return vm.NewString(b.String())
}

func initArray(vm *engine.VM) {
	proto := vm.ArrayProto
	ctor := constructor(vm, "Array", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		proto := vm.ArrayProto
		if nt := vm.NewTarget; nt != nil && nt != vm.ArrayCtor {
			proto = vm.GetPrototypeFromConstructor(nt, vm.ArrayProto)
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
		a := vm.NewArray(append([]engine.Value(nil), args...))
		if proto != vm.ArrayProto {
			vm.SetPrototypeOf(a, proto)
		}
		return engine.ObjectValue(a)
	})
	vm.ArrayCtor = ctor
	vm.FnArraySpecies = vm.NewNative("get [Symbol.species]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	vm.DefineAccessor(ctor, vm.K.SymSpecies, vm.FnArraySpecies, nil, engine.FlagConfigurable)

	method(vm, ctor, "isArray", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.IsArray(arg(args, 0)))
	})
	method(vm, ctor, "of", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if this.IsCallable() && vm.IsConstructor(this.Object()) && this.Object() != vm.ArrayCtor {
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
		custom := this.IsCallable() && vm.IsConstructor(this.Object()) && this.Object() != vm.ArrayCtor
		mapped := func(v engine.Value, i int64) engine.Value {
			if mapFn.IsUndefined() {
				return v
			}
			return vm.Call(mapFn.Object(), thisArg, v, engine.Number(float64(i)))
		}
		if m := vm.GetMethod(items, vm.K.SymIterator); m != nil {
			var a *engine.Object
			if custom {
				a = vm.Construct(this.Object(), nil, this.Object())
			} else {
				a = vm.NewArray(nil)
			}
			i := int64(0)
			vm.Iterate(items, func(v engine.Value) bool {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, i), mapped(v, i))
				i++
				return true
			})
			setLength(vm, a, i)
			return engine.ObjectValue(a)
		}
		src := vm.ToObject(items)
		n := vm.LengthOfArrayLike(src)
		var a *engine.Object
		if custom {
			a = vm.Construct(this.Object(), []engine.Value{engine.Number(float64(n))}, this.Object())
		} else {
			a = vm.NewArray(nil)
		}
		for i := range n {
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, i), mapped(getAt(vm, src, i), i))
		}
		setLength(vm, a, n)
		return engine.ObjectValue(a)
	})

	method(vm, proto, "push", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if n, ok := vm.AppendElements(o, args); ok {
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
		j := int64(0)
		for i := start; i < end; i, j = i+1, j+1 {
			if hasAt(vm, o, i) {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, j), getAt(vm, o, i))
			}
		}
		setLength(vm, a, j)
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
				}
			}
			for i := n; i > n-del+k; i-- {
				deleteAt(vm, o, i-1)
			}
		case k > del:
			for i := n - del; i > start; i-- {
				if hasAt(vm, o, i+del-1) {
					putAt(vm, o, i+k-1, getAt(vm, o, i+del-1))
				} else {
					deleteAt(vm, o, i+k-1)
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
		items := append([]engine.Value{engine.ObjectValue(o)}, args...)
		for _, item := range items {
			spreadable := false
			if item.IsObject() {
				if s := vm.GetV(item, vm.K.SymIsConcatSpreadable); !s.IsUndefined() {
					spreadable = engine.ToBoolean(s)
				} else {
					spreadable = engine.IsArray(item)
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
			for i := range l {
				if hasAt(vm, item.Object(), i) {
					vm.CreateDataPropertyOrThrow(a, bigKey(vm, n), getAt(vm, item.Object(), i))
				}
				n++
			}
		}
		setLength(vm, a, n)
		return engine.ObjectValue(a)
	})
	method(vm, proto, "join", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		sep := ","
		if s := arg(args, 0); !s.IsUndefined() {
			sep = vm.ToString(s)
		}
		return joinArray(vm, o, n, sep)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if j := vm.GetV(engine.ObjectValue(o), vm.Intern("join")); j.IsCallable() {
			return vm.Call(j.Object(), engine.ObjectValue(o))
		}
		return vm.NewString("[object " + o.BuiltinTag() + "]")
	})
	method(vm, proto, "reverse", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := vm.LengthOfArrayLike(o)
		if o.ReverseElements() {
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
			if hok {
				putAt(vm, o, lo, hv)
			} else {
				deleteAt(vm, o, lo)
			}
			if lok {
				putAt(vm, o, hi, lv)
			} else {
				deleteAt(vm, o, hi)
			}
		}
		return engine.ObjectValue(o)
	})

	search := func(name string, fromEnd, same bool) {
		length := 1
		method(vm, proto, name, length, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
					if hasAt(vm, o, i) && eq(getAt(vm, o, i), target) {
						return engine.Number(float64(i))
					}
				}
				return engine.Number(-1)
			}
			from := relIndex(vm.ToIntegerOrInfinity(arg(args, 1)), n)
			for i := from; i < n; i++ {
				if same {
					if eq(getAt(vm, o, i), target) {
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

	// each defines an iteration method. f receives the element, its index and the result of
	// the callback, and returns false to stop.
	type step func(v engine.Value, i int64, r engine.Value) bool
	each := func(name string, skipHoles bool, run func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value) {
		method(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			n := vm.LengthOfArrayLike(o)
			cb := callback(vm, args, name)
			thisArg := arg(args, 1)
			return run(vm, o, n, func(s step) {
				for i := range n {
					if skipHoles && !hasAt(vm, o, i) {
						continue
					}
					v := getAt(vm, o, i)
					r := vm.Call(cb, thisArg, v, engine.Number(float64(i)), engine.ObjectValue(o))
					if !s(v, i, r) {
						return
					}
				}
			})
		})
	}
	each("forEach", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			return true
		})
		return engine.Undefined
	})
	each("map", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		a := arraySpeciesCreate(vm, o, n)
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, i), r)
			return true
		})
		return engine.ObjectValue(a)
	})
	each("filter", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		a := arraySpeciesCreate(vm, o, 0)
		j := int64(0)
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			if engine.ToBoolean(r) {
				vm.CreateDataPropertyOrThrow(a, bigKey(vm, j), v)
				j++
			}
			return true
		})
		return engine.ObjectValue(a)
	})
	each("some", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		found := false
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			found = engine.ToBoolean(r)
			return !found
		})
		return engine.Bool(found)
	})
	each("every", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		all := true
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			all = engine.ToBoolean(r)
			return all
		})
		return engine.Bool(all)
	})
	each("find", false, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		res := engine.Undefined
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			if engine.ToBoolean(r) {
				res = v
				return false
			}
			return true
		})
		return res
	})
	each("findIndex", false, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		res := engine.Number(-1)
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			if engine.ToBoolean(r) {
				res = engine.Number(float64(i))
				return false
			}
			return true
		})
		return res
	})
	findLast := func(name string, index bool) {
		method(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
		method(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
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
				if hasAt(vm, o, i) {
					acc = vm.Call(cb, engine.Undefined, acc, getAt(vm, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
				}
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
		for i := range n {
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
			if hasAt(vm, o, i) {
				deleteAt(vm, o, i)
			}
		}
		return engine.ObjectValue(o)
	})
	// flatten implements FlattenIntoArray for the first l elements of src without a mapper function.
	var flatten func(a *engine.Object, src *engine.Object, l int64, depth float64, n *int64)
	flatten = func(a *engine.Object, src *engine.Object, l int64, depth float64, n *int64) {
		for i := range l {
			if !hasAt(vm, src, i) {
				continue
			}
			v := getAt(vm, src, i)
			if depth > 0 && engine.IsArray(v) {
				flatten(a, v.Object(), vm.LengthOfArrayLike(v.Object()), depth-1, n)
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
		for i := range l {
			if !hasAt(vm, o, i) {
				continue
			}
			v := vm.Call(cb, arg(args, 1), getAt(vm, o, i), engine.Number(float64(i)), engine.ObjectValue(o))
			if engine.IsArray(v) {
				flatten(a, v.Object(), vm.LengthOfArrayLike(v.Object()), 0, &n)
				continue
			}
			vm.CreateDataPropertyOrThrow(a, bigKey(vm, n), v)
			n++
		}
		return engine.ObjectValue(a)
	})

	iter := func(name string, kind int) *engine.Object {
		return method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return engine.ObjectValue(newArrayIterator(vm, vm.ToObject(this), kind))
		})
	}
	iter("keys", iterKeys)
	iter("entries", iterEntries)
	vm.FnArrayValues = iter("values", iterValues)
	vm.Define(proto, vm.K.SymIterator, engine.ObjectValue(vm.FnArrayValues), engine.FlagsHidden)
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
	if cmp.IsUndefined() {
		allNumbersOrStrings := true
		for _, v := range vals {
			if !v.IsString() {
				allNumbersOrStrings = false
				break
			}
		}
		if allNumbersOrStrings {
			sort.SliceStable(vals, func(i, j int) bool {
				return wtf8.Compare(vals[i].Object().Str(), vals[j].Object().Str()) < 0
			})
			return
		}
		strs := make([]string, len(vals))
		for i, v := range vals {
			strs[i] = vm.ToString(v)
		}
		sort.Stable(&byString{
			vals: vals,
			strs: strs,
		})
		return
	}
	sort.SliceStable(vals, func(i, j int) bool {
		r := vm.Call(cmp.Object(), engine.Undefined, vals[i], vals[j])
		return vm.ToNumber(r) < 0
	})
}

type byString struct {
	vals []engine.Value
	strs []string
}

func (s *byString) Len() int {
	return len(s.vals)
}

func (s *byString) Less(i, j int) bool {
	return wtf8.Compare(s.strs[i], s.strs[j]) < 0
}

func (s *byString) Swap(i, j int) {
	s.vals[i], s.vals[j] = s.vals[j], s.vals[i]
	s.strs[i], s.strs[j] = s.strs[j], s.strs[i]
}

// Kinds of array iterators.
const (
	iterKeys = iota
	iterValues
	iterEntries
)

type arrayIterator struct {
	obj  *engine.Object
	pos  int64
	kind int
}

func newArrayIterator(vm *engine.VM, o *engine.Object, kind int) *engine.Object {
	return vm.NewIteratorObject(vm.ArrayIteratorProto, &arrayIterator{
		obj:  o,
		kind: kind,
	})
}
