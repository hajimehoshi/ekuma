// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtins

import (
	"math"
	"sort"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// lengthOf implements LengthOfArrayLike.
func lengthOf(vm *engine.VM, o *engine.Object) int64 {
	if o.Kind == engine.KindArray {
		return int64(o.Length)
	}
	return numconv.ToLength(vm.ToNumber(vm.GetV(engine.Value{O: o}, vm.K.Length)))
}

// bigKey returns the property key for an integer index, which may be beyond the array indices.
func bigKey(vm *engine.VM, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.PropKey{Idx: uint32(i)}
	}
	return engine.PropKey{Key: vm.Intern(numconv.NumberToString(float64(i)))}
}

func getAt(vm *engine.VM, o *engine.Object, i int64) engine.Value {
	if i >= 0 && i < int64(len(o.Elems)) {
		if e := o.Elems[i]; !e.IsHole() {
			return e
		}
	}
	vm.Poll()
	return vm.GetProp(o, bigKey(vm, i), engine.Value{O: o})
}

func hasAt(vm *engine.VM, o *engine.Object, i int64) bool {
	if i >= 0 && i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		return true
	}
	vm.Poll()
	return vm.HasProperty(o, bigKey(vm, i))
}

func putAt(vm *engine.VM, o *engine.Object, i int64, v engine.Value) {
	if i >= 0 && i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		o.Elems[i] = v
		return
	}
	vm.Put(o, bigKey(vm, i), v)
}

func deleteAt(vm *engine.VM, o *engine.Object, i int64) {
	k := bigKey(vm, i)
	if !vm.DeleteProp(o, k) {
		vm.ThrowTypeError("Cannot delete property '" + vm.KeyString(k) + "'")
	}
}

func setLength(vm *engine.VM, o *engine.Object, n int64) {
	vm.Put(o, engine.PropKey{Key: vm.K.Length}, engine.Number(float64(n)))
}

// isArray implements IsArray.
func isArray(vm *engine.VM, v engine.Value) bool {
	return v.O != nil && v.O.Kind == engine.KindArray
}

// isPlainArray reports whether o is an array whose elements are all present in elems,
// so that it can be read without looking at prototypes.
func isDense(vm *engine.VM, o *engine.Object) bool {
	if o.Kind != engine.KindArray || int(o.Length) != len(o.Elems) || o.Flags&(engine.ObjSparse|engine.ObjNotExtensible|engine.ObjFrozenLength) != 0 {
		return false
	}
	for _, e := range o.Elems {
		if e.IsHole() {
			return false
		}
	}
	return true
}

// arraySpeciesCreate implements ArraySpeciesCreate.
func arraySpeciesCreate(vm *engine.VM, orig *engine.Object, n int64) *engine.Object {
	if orig.Kind == engine.KindArray {
		c := vm.GetV(engine.Value{O: orig}, vm.K.Constructor)
		if c.IsObject() {
			if c.O == vm.ArrayCtor {
				c = engine.Undefined
			} else if s := vm.GetV(c, vm.K.SymSpecies); s.IsNull() {
				c = engine.Undefined
			} else {
				c = s
			}
		}
		if !c.IsUndefined() {
			if !c.IsCallable() || !vm.IsConstructor(c.O) {
				vm.ThrowTypeError("object.constructor[Symbol.species] is not a constructor")
			}
			return vm.Construct(c.O, []engine.Value{engine.Number(float64(n))}, c.O)
		}
	}
	if n > engine.MaxArrayIndex+1 {
		vm.ThrowRangeError("Invalid array length")
	}
	a := vm.NewArray(nil)
	a.Length = uint32(n)
	return a
}

func callback(vm *engine.VM, args []engine.Value, what string) *engine.Object {
	f := arg(args, 0)
	if !f.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(f) + " is not a function")
	}
	return f.O
}

// joinArray implements Array.prototype.join for the object o.
func joinArray(vm *engine.VM, o *engine.Object, sep string) engine.Value {
	for _, a := range vm.Joining {
		if a == o {
			return vm.EmptyString
		}
	}
	vm.Joining = append(vm.Joining, o)
	defer func() {
		vm.Joining = vm.Joining[:len(vm.Joining)-1]
	}()
	n := lengthOf(vm, o)
	var b wtf8.Builder
	for i := int64(0); i < n; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		if e := getAt(vm, o, i); !e.IsNullish() {
			b.WriteString(vm.ToString(e))
		}
		if b.Len() > engine.MaxStringLength {
			vm.ThrowRangeError("Invalid string length")
		}
	}
	return vm.NewString(b.String())
}

func initArray(vm *engine.VM) {
	proto := vm.ArrayProto
	ctor := constructor(vm, "Array", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		a := vm.NewArray(nil)
		if nt := vm.NewTarget; nt != nil && nt != vm.ArrayCtor {
			vm.SetProto(a, vm.ProtoFromConstructor(nt, vm.ArrayProto))
		}
		if len(args) == 1 {
			if n := args[0]; n.IsNumber() {
				u := numconv.ToUint32(n.N)
				if float64(u) != n.N {
					vm.ThrowRangeError("Invalid array length")
				}
				a.Length = u
				return engine.Value{O: a}
			}
		}
		a.Elems = append(a.Elems, args...)
		a.Length = uint32(len(args))
		return engine.Value{O: a}
	})
	vm.ArrayCtor = ctor
	vm.FnArraySpecies = vm.NewNative("get [Symbol.species]", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return this
	})
	vm.Define(ctor, vm.K.SymSpecies, engine.NewAccessor(vm.FnArraySpecies, nil), engine.FlagConfigurable|engine.FlagAccessor)

	method(vm, ctor, "isArray", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(isArray(vm, arg(args, 0)))
	})
	method(vm, ctor, "of", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if this.IsCallable() && vm.IsConstructor(this.O) && this.O != vm.ArrayCtor {
			a := vm.Construct(this.O, []engine.Value{engine.Number(float64(len(args)))}, this.O)
			for i, v := range args {
				vm.CreateDataProperty(a, engine.PropKey{Idx: uint32(i)}, v)
			}
			setLength(vm, a, int64(len(args)))
			return engine.Value{O: a}
		}
		return engine.Value{O: vm.NewArray(append([]engine.Value(nil), args...))}
	})
	method(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		items, mapFn, thisArg := arg(args, 0), arg(args, 1), arg(args, 2)
		if !mapFn.IsUndefined() && !mapFn.IsCallable() {
			vm.ThrowTypeError(vm.DisplayString(mapFn) + " is not a function")
		}
		custom := this.IsCallable() && vm.IsConstructor(this.O) && this.O != vm.ArrayCtor
		mapped := func(v engine.Value, i int64) engine.Value {
			if mapFn.IsUndefined() {
				return v
			}
			return vm.Call(mapFn.O, thisArg, v, engine.Number(float64(i)))
		}
		if m := vm.GetMethod(items, vm.K.SymIterator); m != nil {
			var a *engine.Object
			if custom {
				a = vm.Construct(this.O, nil, this.O)
			} else {
				a = vm.NewArray(nil)
			}
			i := int64(0)
			vm.Iterate(items, func(v engine.Value) bool {
				vm.CreateDataProperty(a, bigKey(vm, i), mapped(v, i))
				i++
				return true
			})
			setLength(vm, a, i)
			return engine.Value{O: a}
		}
		src := vm.ToObject(items)
		n := lengthOf(vm, src)
		var a *engine.Object
		if custom {
			a = vm.Construct(this.O, []engine.Value{engine.Number(float64(n))}, this.O)
		} else {
			a = vm.NewArray(nil)
		}
		for i := int64(0); i < n; i++ {
			vm.CreateDataProperty(a, bigKey(vm, i), mapped(getAt(vm, src, i), i))
		}
		setLength(vm, a, n)
		return engine.Value{O: a}
	})

	method(vm, proto, "push", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if o.Kind == engine.KindArray && int(o.Length) == len(o.Elems) && o.Flags&(engine.ObjSparse|engine.ObjNotExtensible|engine.ObjFrozenLength) == 0 && vm.ArrayProtoPlain() {
			o.Elems = append(o.Elems, args...)
			o.Length = uint32(len(o.Elems))
			vm.TouchProto(o)
			return engine.Number(float64(o.Length))
		}
		n := lengthOf(vm, o)
		if n+int64(len(args)) > numconv.MaxSafeInteger {
			vm.ThrowTypeError("Pushing elements past the maximum length")
		}
		for _, v := range args {
			vm.Put(o, bigKey(vm, n), v)
			n++
		}
		setLength(vm, o, n)
		return engine.Number(float64(n))
	})
	method(vm, proto, "pop", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if isDense(vm, o) && len(o.Elems) > 0 {
			n := len(o.Elems) - 1
			v := o.Elems[n]
			o.Elems[n] = engine.Value{}
			o.Elems = o.Elems[:n]
			o.Length = uint32(n)
			return v
		}
		n := lengthOf(vm, o)
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
		if isDense(vm, o) && len(o.Elems) > 0 {
			v := o.Elems[0]
			copy(o.Elems, o.Elems[1:])
			n := len(o.Elems) - 1
			o.Elems[n] = engine.Value{}
			o.Elems = o.Elems[:n]
			o.Length = uint32(n)
			return v
		}
		n := lengthOf(vm, o)
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
		if isDense(vm, o) && vm.ArrayProtoPlain() {
			o.Elems = append(append(make([]engine.Value, 0, len(o.Elems)+len(args)), args...), o.Elems...)
			o.Length = uint32(len(o.Elems))
			return engine.Number(float64(o.Length))
		}
		n := lengthOf(vm, o)
		if k > 0 {
			if n+k > numconv.MaxSafeInteger {
				vm.ThrowTypeError("Unshifting elements past the maximum length")
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
		n := lengthOf(vm, o)
		start := relIndex(vm.ToInteger(arg(args, 0)), n)
		end := n
		if e := arg(args, 1); !e.IsUndefined() {
			end = relIndex(vm.ToInteger(e), n)
		}
		count := max(end-start, 0)
		if isDense(vm, o) && arrayCtorPlain(vm, o) && end <= int64(len(o.Elems)) {
			return engine.Value{O: vm.NewArray(append([]engine.Value(nil), o.Elems[start:start+count]...))}
		}
		a := arraySpeciesCreate(vm, o, count)
		j := int64(0)
		for i := start; i < end; i, j = i+1, j+1 {
			if hasAt(vm, o, i) {
				vm.CreateDataProperty(a, bigKey(vm, j), getAt(vm, o, i))
			}
		}
		setLength(vm, a, j)
		return engine.Value{O: a}
	})
	method(vm, proto, "splice", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := lengthOf(vm, o)
		start := relIndex(vm.ToInteger(arg(args, 0)), n)
		var items []engine.Value
		del := int64(0)
		switch {
		case len(args) == 1:
			del = n - start
		case len(args) >= 2:
			del = int64(math.Min(math.Max(vm.ToInteger(args[1]), 0), float64(n-start)))
			items = args[2:]
		}
		k := int64(len(items))
		if n+k-del > numconv.MaxSafeInteger {
			vm.ThrowTypeError("Splicing elements past the maximum length")
		}
		if isDense(vm, o) && arrayCtorPlain(vm, o) && vm.ArrayProtoPlain() && n == int64(len(o.Elems)) {
			removed := append([]engine.Value(nil), o.Elems[start:start+del]...)
			tail := append([]engine.Value(nil), o.Elems[start+del:]...)
			o.Elems = append(append(o.Elems[:start], items...), tail...)
			o.Length = uint32(len(o.Elems))
			return engine.Value{O: vm.NewArray(removed)}
		}
		a := arraySpeciesCreate(vm, o, del)
		for i := int64(0); i < del; i++ {
			if hasAt(vm, o, start+i) {
				vm.CreateDataProperty(a, bigKey(vm, i), getAt(vm, o, start+i))
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
		return engine.Value{O: a}
	})
	method(vm, proto, "concat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		a := arraySpeciesCreate(vm, o, 0)
		n := int64(0)
		items := append([]engine.Value{{O: o}}, args...)
		for _, item := range items {
			spreadable := false
			if item.IsObject() {
				if s := vm.GetV(item, vm.K.SymIsConcatSpreadable); !s.IsUndefined() {
					spreadable = engine.Truthy(s)
				} else {
					spreadable = isArray(vm, item)
				}
			}
			if !spreadable {
				if n >= numconv.MaxSafeInteger {
					vm.ThrowTypeError("Array too long")
				}
				vm.CreateDataProperty(a, bigKey(vm, n), item)
				n++
				continue
			}
			l := lengthOf(vm, item.O)
			if n+l > numconv.MaxSafeInteger {
				vm.ThrowTypeError("Array too long")
			}
			if a.Kind == engine.KindArray && isDense(vm, item.O) && int64(len(a.Elems)) == n && a.Shape == vm.ArrayShape {
				a.Elems = append(a.Elems, item.O.Elems...)
				a.Length = uint32(len(a.Elems))
				n += l
				continue
			}
			for i := int64(0); i < l; i++ {
				if hasAt(vm, item.O, i) {
					vm.CreateDataProperty(a, bigKey(vm, n), getAt(vm, item.O, i))
				}
				n++
			}
		}
		setLength(vm, a, n)
		return engine.Value{O: a}
	})
	method(vm, proto, "join", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		sep := ","
		if s := arg(args, 0); !s.IsUndefined() {
			sep = vm.ToString(s)
		}
		return joinArray(vm, o, sep)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		if j := vm.GetV(engine.Value{O: o}, vm.Intern("join")); j.IsCallable() {
			return vm.Call(j.O, engine.Value{O: o})
		}
		return vm.NewString("[object " + classOf(vm, o) + "]")
	})
	method(vm, proto, "reverse", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := lengthOf(vm, o)
		if isDense(vm, o) {
			for i, j := 0, len(o.Elems)-1; i < j; i, j = i+1, j-1 {
				o.Elems[i], o.Elems[j] = o.Elems[j], o.Elems[i]
			}
			return engine.Value{O: o}
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
		return engine.Value{O: o}
	})

	search := func(name string, fromEnd, same bool) {
		length := 1
		method(vm, proto, name, length, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			n := lengthOf(vm, o)
			target := arg(args, 0)
			eq := engine.StrictEquals
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
					f := vm.ToInteger(args[1])
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
			from := relIndex(vm.ToInteger(arg(args, 1)), n)
			for i := from; i < n; i++ {
				if same {
					if eq(getAt(vm, o, i), target) {
						return engine.True
					}
					continue
				}
				if i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
					if eq(o.Elems[i], target) {
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
			n := lengthOf(vm, o)
			cb := callback(vm, args, name)
			thisArg := arg(args, 1)
			return run(vm, o, n, func(s step) {
				for i := int64(0); i < n; i++ {
					if skipHoles && !hasAt(vm, o, i) {
						continue
					}
					v := getAt(vm, o, i)
					r := vm.Call(cb, thisArg, v, engine.Number(float64(i)), engine.Value{O: o})
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
			vm.CreateDataProperty(a, bigKey(vm, i), r)
			return true
		})
		return engine.Value{O: a}
	})
	each("filter", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		a := arraySpeciesCreate(vm, o, 0)
		j := int64(0)
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			if engine.Truthy(r) {
				vm.CreateDataProperty(a, bigKey(vm, j), v)
				j++
			}
			return true
		})
		return engine.Value{O: a}
	})
	each("some", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		found := false
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			found = engine.Truthy(r)
			return !found
		})
		return engine.Bool(found)
	})
	each("every", true, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		all := true
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			all = engine.Truthy(r)
			return all
		})
		return engine.Bool(all)
	})
	each("find", false, func(vm *engine.VM, o *engine.Object, n int64, loop func(step)) engine.Value {
		res := engine.Undefined
		loop(func(v engine.Value, i int64, r engine.Value) bool {
			if engine.Truthy(r) {
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
			if engine.Truthy(r) {
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
			n := lengthOf(vm, o)
			cb := callback(vm, args, name)
			for i := n - 1; i >= 0; i-- {
				v := getAt(vm, o, i)
				if engine.Truthy(vm.Call(cb, arg(args, 1), v, engine.Number(float64(i)), engine.Value{O: o})) {
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
			n := lengthOf(vm, o)
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
					vm.ThrowTypeError("Reduce of empty array with no initial value")
				}
			}
			for ; i != end; i += d {
				if hasAt(vm, o, i) {
					acc = vm.Call(cb, engine.Undefined, acc, getAt(vm, o, i), engine.Number(float64(i)), engine.Value{O: o})
				}
			}
			return acc
		})
	}
	reduce("reduce", false)
	reduce("reduceRight", true)

	method(vm, proto, "fill", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := lengthOf(vm, o)
		start := relIndex(vm.ToInteger(arg(args, 1)), n)
		end := n
		if e := arg(args, 2); !e.IsUndefined() {
			end = relIndex(vm.ToInteger(e), n)
		}
		for i := start; i < end; i++ {
			putAt(vm, o, i, arg(args, 0))
		}
		return engine.Value{O: o}
	})
	method(vm, proto, "at", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		n := lengthOf(vm, o)
		i := vm.ToInteger(arg(args, 0))
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
			vm.ThrowTypeError("The comparison function must be either a function or undefined")
		}
		o := vm.ToObject(this)
		n := lengthOf(vm, o)
		var vals []engine.Value
		undefs := int64(0)
		for i := int64(0); i < n; i++ {
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
		return engine.Value{O: o}
	})
	var flatten func(a *engine.Object, src *engine.Object, depth float64, n *int64)
	flatten = func(a *engine.Object, src *engine.Object, depth float64, n *int64) {
		l := lengthOf(vm, src)
		for i := int64(0); i < l; i++ {
			if !hasAt(vm, src, i) {
				continue
			}
			v := getAt(vm, src, i)
			if depth > 0 && isArray(vm, v) {
				flatten(a, v.O, depth-1, n)
				continue
			}
			vm.CreateDataProperty(a, bigKey(vm, *n), v)
			*n++
		}
	}
	method(vm, proto, "flat", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		depth := 1.0
		if d := arg(args, 0); !d.IsUndefined() {
			depth = vm.ToInteger(d)
		}
		a := arraySpeciesCreate(vm, o, 0)
		var n int64
		flatten(a, o, depth, &n)
		return engine.Value{O: a}
	})
	method(vm, proto, "flatMap", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(this)
		l := lengthOf(vm, o)
		cb := callback(vm, args, "flatMap")
		a := arraySpeciesCreate(vm, o, 0)
		var n int64
		for i := int64(0); i < l; i++ {
			if !hasAt(vm, o, i) {
				continue
			}
			v := vm.Call(cb, arg(args, 1), getAt(vm, o, i), engine.Number(float64(i)), engine.Value{O: o})
			if isArray(vm, v) {
				flatten(a, v.O, 0, &n)
				continue
			}
			vm.CreateDataProperty(a, bigKey(vm, n), v)
			n++
		}
		return engine.Value{O: a}
	})

	iter := func(name string, kind int) *engine.Object {
		return method(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return engine.Value{O: newArrayIterator(vm, vm.ToObject(this), kind)}
		})
	}
	iter("keys", iterKeys)
	iter("entries", iterEntries)
	vm.FnArrayValues = iter("values", iterValues)
	vm.Define(proto, vm.K.SymIterator, engine.Value{O: vm.FnArrayValues}, engine.FlagsHidden)
}

// relIndex resolves a relative index against a length, clamping to the range from 0 to n.
func relIndex(f float64, n int64) int64 {
	if f < 0 {
		return int64(math.Max(f+float64(n), 0))
	}
	return int64(math.Min(f, float64(n)))
}

// arrayCtorPlain reports whether creating the result of a method for the array o
// makes a plain array, because o has the original constructor.
func arrayCtorPlain(vm *engine.VM, o *engine.Object) bool {
	if o.Shape != vm.ArrayShape {
		return false
	}
	c, ok := vm.LookupData(vm.ArrayProto, vm.K.Constructor)
	if !ok || c.O != vm.ArrayCtor {
		return false
	}
	_, p, found := engine.Find(vm.ArrayCtor, vm.K.SymSpecies)
	return found && p.Flags&engine.FlagAccessor != 0 && vm.ArrayCtor.Slots[p.Slot].Accessor().Getter == vm.FnArraySpecies
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
				return wtf8.Compare(vals[i].O.Str, vals[j].O.Str) < 0
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
		r := vm.Call(cmp.O, engine.Undefined, vals[i], vals[j])
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
	it := vm.NewObject(vm.ArrayIteratorProto)
	it.Class = engine.ClassIterator
	it.Ext = &arrayIterator{
		obj:  o,
		kind: kind,
	}
	return it
}
