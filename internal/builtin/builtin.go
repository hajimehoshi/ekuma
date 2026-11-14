// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// arg returns the argument i, or undefined when it was not passed.
func arg(args []engine.Value, i int) engine.Value {
	if i < len(args) {
		return args[i]
	}
	return engine.Undefined
}

// method defines the non-enumerable function name on o.
func method(vm *engine.VM, o *engine.Object, name string, length int, f engine.NativeFunc) *engine.Object {
	fn := vm.NewNative(name, length, f)
	vm.Define(o, vm.Intern(name), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// symMethod defines the non-enumerable function under the symbol key on o.
func symMethod(vm *engine.VM, o *engine.Object, key *engine.Object, name string, length int, f engine.NativeFunc) *engine.Object {
	fn := vm.NewNative(name, length, f)
	vm.Define(o, key, engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// iteratorNext defines the next method of the built-in iterators with the prototype proto,
// which steps the iterator with step.
func iteratorNext(vm *engine.VM, proto *engine.Object, step engine.IteratorStepFunc) *engine.Object {
	fn := vm.NewIteratorNext(step)
	vm.Define(proto, vm.Intern("next"), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// getter defines the accessor property name on o with the getter f.
func getter(vm *engine.VM, o *engine.Object, key *engine.Object, name string, f engine.NativeFunc) {
	g := vm.NewNative("get "+name, 0, f)
	vm.DefineAccessor(o, key, g, nil, engine.FlagConfigurable)
}

// constant defines the property name of o that cannot be changed.
func constant(vm *engine.VM, o *engine.Object, name string, v engine.Value) {
	vm.Define(o, vm.Intern(name), v, 0)
}

// constructor creates the constructor function name with the prototype object proto,
// and defines it as a global.
func constructor(vm *engine.VM, name string, length int, proto *engine.Object, f engine.NativeFunc) *engine.Object {
	c := vm.NewNativeConstructor(name, length, f)
	vm.Define(c, vm.K.Prototype, engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.K.Constructor, engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(vm.Global, vm.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
	return c
}

// thisObject returns this as an object, or throws a TypeError naming the method.
func thisObject(vm *engine.VM, this engine.Value, what string) *engine.Object {
	if !this.IsObject() {
		vm.ThrowTypeError(what + " called on non-object")
	}
	return this.Object()
}

// setterThatIgnoresPrototypeProperties implements SetterThatIgnoresPrototypeProperties:
// it sets the property key of this to v unless this is home. what names the setter in errors.
func setterThatIgnoresPrototypeProperties(vm *engine.VM, this engine.Value, home *engine.Object, key engine.PropKey, v engine.Value, what string) {
	if !this.IsObject() {
		vm.ThrowTypeError(what + " called on non-object")
	}
	o := this.Object()
	if o == home {
		vm.ThrowTypeError("cannot assign to read only property '" + vm.KeyString(key) + "'")
	}
	if _, ok := vm.GetOwnProperty(o, key); !ok {
		vm.CreateDataPropertyOrThrow(o, key, v)
		return
	}
	vm.Set(o, key, v)
}

// Install creates the standard built-in objects of vm.
func Install(vm *engine.VM) {
	vm.NumberProto = vm.NewNumberObject(0, vm.ObjectProto)
	vm.BooleanProto = vm.NewBooleanObject(false, vm.ObjectProto)
	for _, p := range []**engine.Object{
		&vm.SymbolProto, &vm.BigintProto, &vm.ErrorProto,
		&vm.IteratorProto, &vm.AsyncIteratorProto, &vm.RegexpProto, &vm.PromiseProto,
		&vm.MapProto, &vm.SetObjectProto, &vm.DateProto,
	} {
		*p = vm.NewObject(vm.ObjectProto)
	}
	vm.ArrayIteratorProto = vm.NewObject(vm.IteratorProto)
	vm.StringIteratorProto = vm.NewObject(vm.IteratorProto)
	vm.GeneratorProto = vm.NewObject(vm.IteratorProto)
	vm.AsyncGeneratorProto = vm.NewObject(vm.AsyncIteratorProto)
	vm.GeneratorFunctionProto = vm.NewObject(vm.FunctionProto)
	vm.AsyncFunctionProto = vm.NewObject(vm.FunctionProto)
	vm.AsyncGeneratorFunctionProto = vm.NewObject(vm.FunctionProto)

	g := vm.Global
	vm.Define(g, vm.Intern("globalThis"), engine.ObjectValue(g), engine.FlagsHidden)
	constant(vm, g, "undefined", engine.Undefined)
	constant(vm, g, "NaN", engine.Number(math.NaN()))
	constant(vm, g, "Infinity", engine.Number(math.Inf(1)))

	vm.ThrowTypeErrorFunc = vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("'caller', 'callee', and 'arguments' properties may not be accessed on strict mode functions or the arguments objects for calls to them")
		return engine.Undefined
	})
	vm.PreventExtensions(vm.ThrowTypeErrorFunc)

	initObject(vm)
	initFunction(vm)
	initError(vm)
	initArray(vm)
	initString(vm)
	initRegExp(vm)
	parseFloat, parseInt := newParseFunctions(vm)
	initNumber(vm, parseFloat, parseInt)
	initSymbol(vm)
	initBigInt(vm)
	initMath(vm)
	initIterators(vm)
	initIteratorHelpers(vm)
	initJSON(vm)
	initReflect(vm)
	initMapAndSet(vm)
	initWeak(vm)
	initDate(vm)
	initPromise(vm)
	initAsync(vm)
	initGlobalFunctions(vm, parseFloat, parseInt)
}

// toDescriptor implements ToPropertyDescriptor.
func toDescriptor(vm *engine.VM, v engine.Value) engine.PropDesc {
	if !v.IsObject() {
		vm.ThrowTypeError("property description must be an object: " + vm.DisplayString(v))
	}
	o := v.Object()
	var d engine.PropDesc
	field := func(key *engine.Object) (engine.Value, bool) {
		k := engine.NamedKey(key)
		if !vm.HasProperty(o, k) {
			return engine.Undefined, false
		}
		return vm.Get(o, k), true
	}
	if x, ok := field(vm.K.Enumerable); ok {
		d.Has |= engine.HasEnumerable
		if engine.ToBoolean(x) {
			d.Flags |= engine.FlagEnumerable
		}
	}
	if x, ok := field(vm.K.Configurable); ok {
		d.Has |= engine.HasConfigurable
		if engine.ToBoolean(x) {
			d.Flags |= engine.FlagConfigurable
		}
	}
	if x, ok := field(vm.K.Value); ok {
		d.Has |= engine.HasValue
		d.Value = x
	}
	if x, ok := field(vm.K.Writable); ok {
		d.Has |= engine.HasWritable
		if engine.ToBoolean(x) {
			d.Flags |= engine.FlagWritable
		}
	}
	if x, ok := field(vm.K.Get); ok {
		d.Has |= engine.HasGet
		if !x.IsUndefined() {
			if !x.IsCallable() {
				vm.ThrowTypeError("getter must be a function: " + vm.DisplayString(x))
			}
			d.Getter = x.Object()
		}
	}
	if x, ok := field(vm.K.Set); ok {
		d.Has |= engine.HasSet
		if !x.IsUndefined() {
			if !x.IsCallable() {
				vm.ThrowTypeError("setter must be a function: " + vm.DisplayString(x))
			}
			d.Setter = x.Object()
		}
	}
	if d.IsAccessor() && d.IsData() {
		vm.ThrowTypeError("invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	}
	return d
}

// fromDescriptor implements FromPropertyDescriptor.
func fromDescriptor(vm *engine.VM, d engine.PropDesc) engine.Value {
	o := vm.NewObject(vm.ObjectProto)
	fn := func(f *engine.Object) engine.Value {
		if f == nil {
			return engine.Undefined
		}
		return engine.ObjectValue(f)
	}
	if d.Flags&engine.FlagAccessor != 0 {
		vm.Define(o, vm.K.Get, fn(d.Getter), engine.FlagsData)
		vm.Define(o, vm.K.Set, fn(d.Setter), engine.FlagsData)
	} else {
		vm.Define(o, vm.K.Value, d.Value, engine.FlagsData)
		vm.Define(o, vm.K.Writable, engine.Bool(d.Flags&engine.FlagWritable != 0), engine.FlagsData)
	}
	vm.Define(o, vm.K.Enumerable, engine.Bool(d.Flags&engine.FlagEnumerable != 0), engine.FlagsData)
	vm.Define(o, vm.K.Configurable, engine.Bool(d.Flags&engine.FlagConfigurable != 0), engine.FlagsData)
	return engine.ObjectValue(o)
}

// setIntegrity implements SetIntegrityLevel. frozen selects frozen over sealed.
func setIntegrity(vm *engine.VM, o *engine.Object, frozen bool) {
	if !vm.PreventExtensions(o) {
		vm.ThrowTypeError("cannot prevent extensions")
	}
	for _, k := range vm.OwnKeys(o) {
		d := engine.PropDesc{Has: engine.HasConfigurable}
		if frozen {
			cur, ok := vm.GetOwnProperty(o, k)
			if !ok {
				continue
			}
			if cur.Flags&engine.FlagAccessor == 0 {
				d.Has |= engine.HasWritable
			}
		}
		vm.DefinePropertyOrThrow(o, k, d)
	}
}

// testIntegrity implements TestIntegrityLevel.
func testIntegrity(vm *engine.VM, o *engine.Object, frozen bool) bool {
	if vm.IsExtensible(o) {
		return false
	}
	for _, k := range vm.OwnKeys(o) {
		d, ok := vm.GetOwnProperty(o, k)
		if !ok {
			continue
		}
		if d.Flags&engine.FlagConfigurable != 0 {
			return false
		}
		if frozen && d.Flags&engine.FlagAccessor == 0 && d.Flags&engine.FlagWritable != 0 {
			return false
		}
	}
	return true
}

// enumerableOwn returns the enumerable own string keys of o and, when wanted, their values.
func enumerableOwn(vm *engine.VM, o *engine.Object, keys, values bool) []engine.Value {
	var out []engine.Value
	for _, k := range vm.OwnKeys(o) {
		if k.IsSymbol() {
			continue
		}
		d, ok := vm.GetOwnProperty(o, k)
		if !ok || d.Flags&engine.FlagEnumerable == 0 {
			continue
		}
		kv := vm.KeyValue(k)
		if !values {
			out = append(out, kv)
			continue
		}
		v := vm.Get(o, k)
		if keys {
			out = append(out, engine.ObjectValue(vm.NewArray([]engine.Value{kv, v})))
		} else {
			out = append(out, v)
		}
	}
	return out
}

func initObject(vm *engine.VM) {
	proto := vm.ObjectProto
	ctor := constructor(vm, "Object", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if nt := vm.NewTarget; nt != nil && nt != vm.ObjectCtor {
			return engine.ObjectValue(vm.NewObject(vm.GetPrototypeFromConstructor(nt, vm.ObjectProto)))
		}
		v := arg(args, 0)
		if v.IsNullish() {
			return engine.ObjectValue(vm.NewObject(vm.ObjectProto))
		}
		return engine.ObjectValue(vm.ToObject(v))
	})
	vm.ObjectCtor = ctor

	method(vm, proto, "hasOwnProperty", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		k := vm.ToPropertyKey(arg(args, 0))
		return engine.Bool(vm.HasOwnProperty(vm.ToObject(this), k))
	})
	method(vm, proto, "isPrototypeOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		if !v.IsObject() {
			return engine.False
		}
		o := vm.ToObject(this)
		for p := vm.ProtoOf(v.Object()); p != nil; p = vm.ProtoOf(p) {
			if p == o {
				return engine.True
			}
		}
		return engine.False
	})
	method(vm, proto, "propertyIsEnumerable", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		k := vm.ToPropertyKey(arg(args, 0))
		d, ok := vm.GetOwnProperty(vm.ToObject(this), k)
		return engine.Bool(ok && d.Flags&engine.FlagEnumerable != 0)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		switch {
		case this.IsUndefined():
			return vm.NewString("[object Undefined]")
		case this.IsNull():
			return vm.NewString("[object Null]")
		}
		o := vm.ToObject(this)
		tag := o.BuiltinTag()
		if t := vm.GetV(engine.ObjectValue(o), vm.K.SymToStringTag); t.IsString() {
			tag = t.Object().Str()
		}
		return vm.NewString("[object " + tag + "]")
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.CallValue(vm.GetV(this, vm.K.ToString), this)
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(vm.ToObject(this))
	})
	protoKey := vm.Intern("__proto__")
	vm.DefineAccessor(proto, protoKey,
		vm.NewNative("get __proto__", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if p := vm.ProtoOf(vm.ToObject(this)); p != nil {
				return engine.ObjectValue(p)
			}
			return engine.Null
		}),
		vm.NewNative("set __proto__", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			vm.RequireObjectCoercible(this, "Object.prototype.__proto__")
			p := arg(args, 0)
			if !this.IsObject() || !(p.IsObject() || p.IsNull()) {
				return engine.Undefined
			}
			var po *engine.Object
			if p.IsObject() {
				po = p.Object()
			}
			if !vm.SetPrototypeOf(this.Object(), po) {
				vm.ThrowTypeError("cannot set prototype")
			}
			return engine.Undefined
		}),
		engine.FlagConfigurable)

	defineAccessor := func(name string, setter bool) {
		method(vm, proto, name, 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			f := arg(args, 1)
			if !f.IsCallable() {
				vm.ThrowTypeError("Object.prototype." + name + ": expecting function")
			}
			d := engine.PropDesc{
				Flags: engine.FlagAccessor | engine.FlagEnumerable | engine.FlagConfigurable,
				Has:   engine.HasEnumerable | engine.HasConfigurable,
			}
			if setter {
				d.Setter = f.Object()
				d.Has |= engine.HasSet
			} else {
				d.Getter = f.Object()
				d.Has |= engine.HasGet
			}
			vm.DefinePropertyOrThrow(o, vm.ToPropertyKey(arg(args, 0)), d)
			return engine.Undefined
		})
	}
	defineAccessor("__defineGetter__", false)
	defineAccessor("__defineSetter__", true)
	lookupAccessor := func(name string, setter bool) {
		method(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			o := vm.ToObject(this)
			k := vm.ToPropertyKey(arg(args, 0))
			for ; o != nil; o = vm.ProtoOf(o) {
				d, ok := vm.GetOwnProperty(o, k)
				if !ok {
					continue
				}
				f := d.Getter
				if setter {
					f = d.Setter
				}
				if !d.IsAccessor() || f == nil {
					return engine.Undefined
				}
				return engine.ObjectValue(f)
			}
			return engine.Undefined
		})
	}
	lookupAccessor("__lookupGetter__", false)
	lookupAccessor("__lookupSetter__", true)

	method(vm, ctor, "getPrototypeOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if p := vm.ProtoOf(vm.ToObject(arg(args, 0))); p != nil {
			return engine.ObjectValue(p)
		}
		return engine.Null
	})
	method(vm, ctor, "setPrototypeOf", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o, p := arg(args, 0), arg(args, 1)
		vm.RequireObjectCoercible(o, "Object.setPrototypeOf")
		if !p.IsObject() && !p.IsNull() {
			vm.ThrowTypeError("object prototype may only be an Object or null: " + vm.DisplayString(p))
		}
		if !o.IsObject() {
			return o
		}
		var po *engine.Object
		if p.IsObject() {
			po = p.Object()
		}
		if !vm.SetPrototypeOf(o.Object(), po) {
			vm.ThrowTypeError("cannot set prototype")
		}
		return o
	})
	defineProperties := func(vm *engine.VM, o *engine.Object, props engine.Value) {
		src := vm.ToObject(props)
		type entry struct {
			k engine.PropKey
			d engine.PropDesc
		}
		var list []entry
		for _, k := range vm.OwnKeys(src) {
			d, ok := vm.GetOwnProperty(src, k)
			if !ok || d.Flags&engine.FlagEnumerable == 0 {
				continue
			}
			list = append(list, entry{
				k: k,
				d: toDescriptor(vm, vm.Get(src, k)),
			})
		}
		for _, e := range list {
			vm.DefinePropertyOrThrow(o, e.k, e.d)
		}
	}
	method(vm, ctor, "create", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		p := arg(args, 0)
		if !p.IsObject() && !p.IsNull() {
			vm.ThrowTypeError("object prototype may only be an Object or null: " + vm.DisplayString(p))
		}
		var po *engine.Object
		if p.IsObject() {
			po = p.Object()
		}
		o := vm.NewObject(po)
		if props := arg(args, 1); !props.IsUndefined() {
			defineProperties(vm, o, props)
		}
		return engine.ObjectValue(o)
	})
	method(vm, ctor, "defineProperty", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		if !o.IsObject() {
			vm.ThrowTypeError("Object.defineProperty called on non-object")
		}
		k := vm.ToPropertyKey(arg(args, 1))
		vm.DefinePropertyOrThrow(o.Object(), k, toDescriptor(vm, arg(args, 2)))
		return o
	})
	method(vm, ctor, "defineProperties", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		if !o.IsObject() {
			vm.ThrowTypeError("Object.defineProperties called on non-object")
		}
		defineProperties(vm, o.Object(), arg(args, 1))
		return o
	})
	method(vm, ctor, "getOwnPropertyDescriptor", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(arg(args, 0))
		d, ok := vm.GetOwnProperty(o, vm.ToPropertyKey(arg(args, 1)))
		if !ok {
			return engine.Undefined
		}
		return fromDescriptor(vm, d)
	})
	method(vm, ctor, "getOwnPropertyDescriptors", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(arg(args, 0))
		out := vm.NewObject(vm.ObjectProto)
		for _, k := range vm.OwnKeys(o) {
			if d, ok := vm.GetOwnProperty(o, k); ok {
				vm.CreateDataPropertyOrThrow(out, k, fromDescriptor(vm, d))
			}
		}
		return engine.ObjectValue(out)
	})
	method(vm, ctor, "getOwnPropertyNames", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var out []engine.Value
		for _, k := range vm.OwnKeys(vm.ToObject(arg(args, 0))) {
			if !k.IsSymbol() {
				out = append(out, vm.KeyValue(k))
			}
		}
		return engine.ObjectValue(vm.NewArray(out))
	})
	method(vm, ctor, "getOwnPropertySymbols", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		var out []engine.Value
		for _, k := range vm.OwnKeys(vm.ToObject(arg(args, 0))) {
			if k.IsSymbol() && !k.IsPrivateName() {
				out = append(out, vm.KeyValue(k))
			}
		}
		return engine.ObjectValue(vm.NewArray(out))
	})
	method(vm, ctor, "keys", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(vm.NewArray(enumerableOwn(vm, vm.ToObject(arg(args, 0)), true, false)))
	})
	method(vm, ctor, "values", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(vm.NewArray(enumerableOwn(vm, vm.ToObject(arg(args, 0)), false, true)))
	})
	method(vm, ctor, "entries", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(vm.NewArray(enumerableOwn(vm, vm.ToObject(arg(args, 0)), true, true)))
	})
	method(vm, ctor, "assign", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		to := vm.ToObject(arg(args, 0))
		for _, src := range args[min(1, len(args)):] {
			if src.IsNullish() {
				continue
			}
			from := vm.ToObject(src)
			for _, k := range vm.OwnKeys(from) {
				d, ok := vm.GetOwnProperty(from, k)
				if !ok || d.Flags&engine.FlagEnumerable == 0 {
					continue
				}
				vm.Set(to, k, vm.Get(from, k))
			}
		}
		return engine.ObjectValue(to)
	})
	method(vm, ctor, "freeze", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			setIntegrity(vm, o.Object(), true)
		}
		return arg(args, 0)
	})
	method(vm, ctor, "seal", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			setIntegrity(vm, o.Object(), false)
		}
		return arg(args, 0)
	})
	method(vm, ctor, "preventExtensions", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			if !vm.PreventExtensions(o.Object()) {
				vm.ThrowTypeError("cannot prevent extensions")
			}
		}
		return arg(args, 0)
	})
	method(vm, ctor, "isFrozen", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(!o.IsObject() || testIntegrity(vm, o.Object(), true))
	})
	method(vm, ctor, "isSealed", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(!o.IsObject() || testIntegrity(vm, o.Object(), false))
	})
	method(vm, ctor, "isExtensible", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(o.IsObject() && vm.IsExtensible(o.Object()))
	})
	method(vm, ctor, "is", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.SameValue(arg(args, 0), arg(args, 1)))
	})
	method(vm, ctor, "fromEntries", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.NewObject(vm.ObjectProto)
		vm.Iterate(arg(args, 0), func(e engine.Value) bool {
			if !e.IsObject() {
				vm.ThrowTypeError("iterator value " + vm.DisplayString(e) + " is not an entry object")
			}
			k := vm.Index(e, 0)
			v := vm.Index(e, 1)
			vm.CreateDataPropertyOrThrow(o, vm.ToPropertyKey(k), v)
			return true
		})
		return engine.ObjectValue(o)
	})
	method(vm, ctor, "hasOwn", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(arg(args, 0))
		return engine.Bool(vm.HasOwnProperty(o, vm.ToPropertyKey(arg(args, 1))))
	})
}

// listFromArrayLike implements CreateListFromArrayLike.
func listFromArrayLike(vm *engine.VM, v engine.Value) []engine.Value {
	if v.IsNullish() {
		return nil
	}
	if !v.IsObject() {
		vm.ThrowTypeError("CreateListFromArrayLike called on non-object")
	}
	if list, ok := v.Object().ElementList(); ok {
		return list
	}
	n := numconv.ToLength(vm.ToNumber(vm.GetV(v, vm.K.Length)))
	if n > 1<<24 {
		vm.ThrowRangeError("too many arguments")
	}
	list := make([]engine.Value, n)
	for i := range list {
		list[i] = vm.Index(v, uint32(i))
	}
	return list
}

func initFunction(vm *engine.VM) {
	proto := vm.FunctionProto
	ctor := constructor(vm, "Function", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.Unsupported("the Function constructor")
		return engine.Undefined
	})
	vm.FunctionCtor = ctor

	vm.FnCall = method(vm, proto, "call", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.call called on a value that is not a function")
		}
		return vm.Call(this.Object(), arg(args, 0), args[min(1, len(args)):]...)
	})
	vm.FnApply = method(vm, proto, "apply", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.apply called on a value that is not a function")
		}
		return vm.Call(this.Object(), arg(args, 0), listFromArrayLike(vm, arg(args, 1))...)
	})
	method(vm, proto, "bind", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("bind must be called on a function")
		}
		target := this.Object()
		b := vm.BoundFunctionCreate(target, arg(args, 0), args[min(1, len(args)):])
		length := 0.0
		if vm.HasOwnProperty(target, engine.NamedKey(vm.K.Length)) {
			if l := vm.GetV(this, vm.K.Length); l.IsNumber() {
				length = math.Max(0, numconv.ToIntegerOrInfinity(l.Number())-float64(max(0, len(args)-1)))
			}
		}
		name := ""
		if n := vm.GetV(this, vm.K.Name); n.IsString() {
			name = n.Object().Str()
		}
		vm.Define(b, vm.K.Length, engine.Number(length), engine.FlagConfigurable)
		vm.Define(b, vm.K.Name, vm.NewString("bound "+name), engine.FlagConfigurable)
		return engine.ObjectValue(b)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.toString requires that 'this' be a Function")
		}
		return vm.NewString(this.Object().SourceText())
	})
	vm.FnHasInstance = vm.NewNative("[Symbol.hasInstance]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(vm.OrdinaryHasInstance(this, arg(args, 0)))
	})
	vm.Define(proto, vm.K.SymHasInstance, engine.ObjectValue(vm.FnHasInstance), 0)

	// AddRestrictedFunctionProperties
	vm.DefineAccessor(proto, vm.Intern("caller"), vm.ThrowTypeErrorFunc, vm.ThrowTypeErrorFunc, engine.FlagConfigurable)
	vm.DefineAccessor(proto, vm.Intern("arguments"), vm.ThrowTypeErrorFunc, vm.ThrowTypeErrorFunc, engine.FlagConfigurable)
}

func initError(vm *engine.VM) {
	names := [engine.ErrorKindCount]string{
		engine.ErrorKindError:     "Error",
		engine.ErrorKindType:      "TypeError",
		engine.ErrorKindRange:     "RangeError",
		engine.ErrorKindReference: "ReferenceError",
		engine.ErrorKindSyntax:    "SyntaxError",
		engine.ErrorKindEval:      "EvalError",
		engine.ErrorKindURI:       "URIError",
		engine.ErrorKindAggregate: "AggregateError",
	}
	var base *engine.Object
	for kind, name := range names {
		kind := engine.ErrorKind(kind)
		proto := vm.ErrorProto
		if kind != engine.ErrorKindError {
			proto = vm.NewObject(vm.ErrorProto)
		}
		vm.ErrorProtos[kind] = proto
		length := 1
		if kind == engine.ErrorKindAggregate {
			length = 2
		}
		ctor := constructor(vm, name, length, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			nt := vm.NewTarget
			e := vm.NewErrorObject(vm.GetPrototypeFromConstructor(nt, vm.ErrorProtos[kind]))
			msgArg, optArg := arg(args, 0), arg(args, 1)
			if kind == engine.ErrorKindAggregate {
				msgArg, optArg = arg(args, 1), arg(args, 2)
			}
			msg := ""
			if !msgArg.IsUndefined() {
				msg = vm.ToString(msgArg)
				vm.Define(e, vm.K.Message, vm.NewString(msg), engine.FlagsHidden)
			}
			if optArg.IsObject() && vm.HasProperty(optArg.Object(), engine.NamedKey(vm.K.Cause)) {
				vm.Define(e, vm.K.Cause, vm.GetV(optArg, vm.K.Cause), engine.FlagsHidden)
			}
			if kind == engine.ErrorKindAggregate {
				var errs []engine.Value
				vm.Iterate(arg(args, 0), func(v engine.Value) bool {
					errs = append(errs, v)
					return true
				})
				vm.Define(e, vm.K.Errors, engine.ObjectValue(vm.NewArray(errs)), engine.FlagsHidden)
			}
			vm.RecordStack(e, wtf8.ToValidUTF8(msg))
			return engine.ObjectValue(e)
		})
		vm.Define(proto, vm.K.Name, vm.NewString(name), engine.FlagsHidden)
		vm.Define(proto, vm.K.Message, vm.EmptyString, engine.FlagsHidden)
		if kind == engine.ErrorKindError {
			base = ctor
		} else {
			vm.SetPrototypeOf(ctor, base)
		}
	}
	method(vm, base, "isError", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.IsError(arg(args, 0)))
	})
	stackKey := engine.NamedKey(vm.K.Stack)
	vm.DefineAccessor(vm.ErrorProto, vm.K.Stack,
		vm.NewNative("get stack", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				vm.ThrowTypeError("Error.prototype.stack getter called on non-object")
			}
			if s, ok := engine.ErrorStack(this); ok {
				return vm.NewString(s)
			}
			return engine.Undefined
		}),
		vm.NewNative("set stack", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				vm.ThrowTypeError("Error.prototype.stack setter called on non-object")
			}
			v := arg(args, 0)
			if !v.IsString() {
				vm.ThrowTypeError("Error.prototype.stack setter requires a string")
			}
			setterThatIgnoresPrototypeProperties(vm, this, vm.ErrorProto, stackKey, v, "Error.prototype.stack setter")
			return engine.Undefined
		}),
		engine.FlagConfigurable)
	method(vm, vm.ErrorProto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := thisObject(vm, this, "Error.prototype.toString")
		name := "Error"
		if n := vm.GetV(engine.ObjectValue(o), vm.K.Name); !n.IsUndefined() {
			name = vm.ToString(n)
		}
		msg := ""
		if m := vm.GetV(engine.ObjectValue(o), vm.K.Message); !m.IsUndefined() {
			msg = vm.ToString(m)
		}
		switch {
		case name == "":
			return vm.NewString(msg)
		case msg == "":
			return vm.NewString(name)
		}
		return vm.NewString(name + ": " + msg)
	})
}

// newParseFunctions creates the functions parseFloat and parseInt.
func newParseFunctions(vm *engine.VM) (parseFloat, parseInt *engine.Object) {
	parseFloat = vm.NewNative("parseFloat", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(numconv.ParseFloat(vm.ToString(arg(args, 0))))
	})
	parseInt = vm.NewNative("parseInt", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := vm.ToString(arg(args, 0))
		return engine.Number(numconv.ParseInt(s, int(vm.ToInt32(arg(args, 1)))))
	})
	return parseFloat, parseInt
}

// initGlobalFunctions defines the function properties of the global object.
// parseFloat and parseInt are the functions to define under those names.
func initGlobalFunctions(vm *engine.VM, parseFloat, parseInt *engine.Object) {
	g := vm.Global
	vm.FnEval = method(vm, g, "eval", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.PerformEval(arg(args, 0))
	})
	method(vm, g, "isNaN", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := vm.ToNumber(arg(args, 0))
		return engine.Bool(n != n)
	})
	method(vm, g, "isFinite", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := vm.ToNumber(arg(args, 0))
		return engine.Bool(n == n && !math.IsInf(n, 0))
	})
	vm.Define(g, vm.Intern("parseFloat"), engine.ObjectValue(parseFloat), engine.FlagsHidden)
	vm.Define(g, vm.Intern("parseInt"), engine.ObjectValue(parseInt), engine.FlagsHidden)
	initURIFunctions(vm)
}
