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
	vm.Define(c, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(vm.GlobalObject().Object(), vm.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
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

// extensions are the functions that [RegisterExtension] registers.
var extensions []func(vm *engine.VM)

// RegisterExtension registers install to define further built-in objects in each realm that
// [Install] creates, after the standard ones. It must be called before any VM is created,
// typically from an init function.
func RegisterExtension(install func(vm *engine.VM)) {
	extensions = append(extensions, install)
}

// Install creates the standard built-in objects of vm, and those of the registered extensions.
func Install(vm *engine.VM) {
	vm.StartBuiltins()
	objectProto := vm.Intrinsic(engine.IntrinsicObjectPrototype)
	vm.SetIntrinsic(engine.IntrinsicNumberPrototype, vm.NewNumberObject(0, objectProto))
	vm.SetIntrinsic(engine.IntrinsicBooleanPrototype, vm.NewBooleanObject(false, objectProto))
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicSymbolPrototype, engine.IntrinsicBigIntPrototype, engine.IntrinsicErrorPrototype,
		engine.IntrinsicIteratorPrototype, engine.IntrinsicAsyncIteratorPrototype, engine.IntrinsicRegExpPrototype,
		engine.IntrinsicPromisePrototype, engine.IntrinsicMapPrototype, engine.IntrinsicSetPrototype,
		engine.IntrinsicDatePrototype,
	} {
		vm.SetIntrinsic(id, vm.NewObject(objectProto))
	}
	iteratorProto := vm.Intrinsic(engine.IntrinsicIteratorPrototype)
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicArrayIteratorPrototype, engine.IntrinsicStringIteratorPrototype, engine.IntrinsicGeneratorPrototype,
	} {
		vm.SetIntrinsic(id, vm.NewObject(iteratorProto))
	}
	vm.SetIntrinsic(engine.IntrinsicAsyncGeneratorPrototype, vm.NewObject(vm.Intrinsic(engine.IntrinsicAsyncIteratorPrototype)))
	functionProto := vm.Intrinsic(engine.IntrinsicFunctionPrototype)
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicGeneratorFunctionPrototype, engine.IntrinsicAsyncFunctionPrototype, engine.IntrinsicAsyncGeneratorFunctionPrototype,
	} {
		vm.SetIntrinsic(id, vm.NewObject(functionProto))
	}

	g := vm.GlobalObject().Object()
	vm.Define(g, vm.Intern("globalThis"), engine.ObjectValue(g), engine.FlagsHidden)
	constant(vm, g, "undefined", engine.Undefined)
	constant(vm, g, "NaN", engine.Number(math.NaN()))
	constant(vm, g, "Infinity", engine.Number(math.Inf(1)))

	throwTypeError := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("'caller', 'callee', and 'arguments' properties may not be accessed on strict mode functions or the arguments objects for calls to them")
		return engine.Undefined
	})
	setIntegrity(vm, throwTypeError, true)
	vm.SetIntrinsic(engine.IntrinsicThrowTypeError, throwTypeError)

	var strs objectStrings
	initObject(vm, &strs)
	initFunction(vm)
	initError(vm)
	initArray(vm, &strs)
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
	initProxy(vm)
	initMapAndSet(vm)
	initWeak(vm)
	initDate(vm)
	initTemporal(vm)
	initShadowRealm(vm)
	initAbstractModuleSource(vm)
	initPromise(vm)
	initAsync(vm)
	initDisposableStacks(vm)
	initBinary(vm)
	initGlobalFunctions(vm, parseFloat, parseInt)
	for _, install := range extensions {
		install(vm)
	}
	vm.FinishBuiltins()
}

// objectStrings holds the results of Object.prototype.toString of a [engine.VM] by tag.
type objectStrings struct {
	byTag map[string]engine.Value
}

// objectStringsLimit bounds the number of tags that an objectStrings keeps.
const objectStringsLimit = 64

// forTag returns the string "[object tag]".
func (s *objectStrings) forTag(vm *engine.VM, tag string) engine.Value {
	if v, ok := s.byTag[tag]; ok {
		return v
	}
	v := vm.NewString("[object " + tag + "]")
	if len(s.byTag) < objectStringsLimit {
		if s.byTag == nil {
			s.byTag = map[string]engine.Value{}
		}
		s.byTag[tag] = v
	}
	return v
}

// toString implements Object.prototype.toString for the value v, which is neither undefined nor null.
func (s *objectStrings) toString(vm *engine.VM, v engine.Value) engine.Value {
	if !v.IsObject() {
		// A wrapper object from ToObject is observable only by code that the lookup of
		// @@toStringTag runs.
		if t, ok := vm.GetVWithoutCall(v, vm.WellKnownSymbol(engine.SymbolToStringTag)); ok {
			if t.IsString() {
				return s.forTag(vm, t.Object().Str())
			}
			return s.forTag(vm, primitiveTag(v))
		}
	}
	return s.objectToString(vm, vm.ToObject(v))
}

// primitiveTag returns the builtinTag of Object.prototype.toString for the object that
// ToObject makes of the primitive v.
func primitiveTag(v engine.Value) string {
	switch {
	case v.IsString():
		return "String"
	case v.IsNumber():
		return "Number"
	case v.IsBool():
		return "Boolean"
	}
	return "Object"
}

// objectToString implements Object.prototype.toString for the object o.
func (s *objectStrings) objectToString(vm *engine.VM, o *engine.Object) engine.Value {
	tag := vm.BuiltinTag(o)
	if t := vm.GetV(engine.ObjectValue(o), vm.WellKnownSymbol(engine.SymbolToStringTag)); t.IsString() {
		tag = t.Object().Str()
	}
	return s.forTag(vm, tag)
}

// setIntegrity implements SetIntegrityLevel. frozen selects frozen over sealed.
func setIntegrity(vm *engine.VM, o *engine.Object, frozen bool) {
	if !vm.PreventExtensions(o) {
		vm.ThrowTypeError("cannot prevent extensions")
	}
	for _, k := range vm.OwnKeys(o) {
		var d engine.PropDesc
		d.SetConfigurable(false)
		if frozen {
			cur, ok := vm.GetOwnProperty(o, k)
			if !ok {
				continue
			}
			if !cur.IsAccessor() {
				d.SetWritable(false)
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
		if d.Configurable() {
			return false
		}
		if frozen && !d.IsAccessor() && d.Writable() {
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
		if !ok || !d.Enumerable() {
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

func initObject(vm *engine.VM, strs *objectStrings) {
	proto := vm.Intrinsic(engine.IntrinsicObjectPrototype)
	ctor := constructor(vm, "Object", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if nt := vm.GetNewTarget(); nt != nil && nt != vm.Intrinsic(engine.IntrinsicObject) {
			return engine.ObjectValue(vm.NewObject(vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicObjectPrototype))))
		}
		v := arg(args, 0)
		if v.IsNullish() {
			return engine.ObjectValue(vm.NewOrdinaryObject())
		}
		return engine.ObjectValue(vm.ToObject(v))
	})
	vm.SetIntrinsic(engine.IntrinsicObject, ctor)

	method(vm, proto, "hasOwnProperty", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		k := vm.QueryKey(arg(args, 0))
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
		return engine.Bool(ok && d.Enumerable())
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		switch {
		case this.IsUndefined():
			return strs.forTag(vm, "Undefined")
		case this.IsNull():
			return strs.forTag(vm, "Null")
		}
		return strs.toString(vm, this)
	})
	method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.CallValue(vm.GetV(this, vm.Key(engine.KeyToString)), this)
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
			var d engine.PropDesc
			d.SetEnumerable(true)
			d.SetConfigurable(true)
			if setter {
				d.SetSetter(f.Object())
			} else {
				d.SetGetter(f.Object())
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
				f := d.Getter()
				if setter {
					f = d.Setter()
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
			if !ok || !d.Enumerable() {
				continue
			}
			list = append(list, entry{
				k: k,
				d: vm.ToPropertyDescriptor(vm.Get(src, k)),
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
		vm.DefinePropertyOrThrow(o.Object(), k, vm.ToPropertyDescriptor(arg(args, 2)))
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
		return vm.FromPropertyDescriptor(d)
	})
	method(vm, ctor, "getOwnPropertyDescriptors", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := vm.ToObject(arg(args, 0))
		out := vm.NewOrdinaryObject()
		for _, k := range vm.OwnKeys(o) {
			if d, ok := vm.GetOwnProperty(o, k); ok {
				vm.CreateDataPropertyOrThrow(out, k, vm.FromPropertyDescriptor(d))
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
			if k.IsSymbol() {
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
				if !ok || !d.Enumerable() {
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
		o := vm.NewOrdinaryObject()
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
		return engine.Bool(vm.HasOwnProperty(o, vm.QueryKey(arg(args, 1))))
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
	n := numconv.ToLength(vm.ToNumber(vm.GetV(v, vm.Key(engine.KeyLength))))
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
	proto := vm.Intrinsic(engine.IntrinsicFunctionPrototype)
	var ctor *engine.Object
	ctor = constructor(vm, "Function", 1, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.CreateDynamicFunction(ctor, vm.GetNewTarget(), engine.NormalFunction, args)
	})
	vm.SetIntrinsic(engine.IntrinsicFunction, ctor)

	call := method(vm, proto, "call", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.call called on a value that is not a function")
		}
		return vm.Call(this.Object(), arg(args, 0), args[min(1, len(args)):]...)
	})
	vm.SetIntrinsic(engine.IntrinsicFunctionPrototypeCall, call)
	apply := method(vm, proto, "apply", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.apply called on a value that is not a function")
		}
		return vm.Call(this.Object(), arg(args, 0), listFromArrayLike(vm, arg(args, 1))...)
	})
	vm.SetIntrinsic(engine.IntrinsicFunctionPrototypeApply, apply)
	method(vm, proto, "bind", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("bind must be called on a function")
		}
		target := this.Object()
		b := vm.BoundFunctionCreate(target, arg(args, 0), args[min(1, len(args)):])
		copyNameAndLength(vm, b, target, "bound ", max(0, len(args)-1))
		return engine.ObjectValue(b)
	})
	method(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			vm.ThrowTypeError("Function.prototype.toString requires that 'this' be a Function")
		}
		return vm.NewString(this.Object().SourceText())
	})
	hasInstance := vm.NewNative("[Symbol.hasInstance]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(vm.OrdinaryHasInstance(this, arg(args, 0)))
	})
	vm.SetIntrinsic(engine.IntrinsicFunctionPrototypeHasInstance, hasInstance)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolHasInstance), engine.ObjectValue(hasInstance), 0)
	// Function.prototype[Symbol.metadata] of the decorator metadata proposal.
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolMetadata), engine.Null, 0)

	// AddRestrictedFunctionProperties
	throwTypeError := vm.Intrinsic(engine.IntrinsicThrowTypeError)
	vm.DefineAccessor(proto, vm.Intern("caller"), throwTypeError, throwTypeError, engine.FlagConfigurable)
	vm.DefineAccessor(proto, vm.Intern("arguments"), throwTypeError, throwTypeError, engine.FlagConfigurable)
}

// copyNameAndLength implements CopyNameAndLength.
func copyNameAndLength(vm *engine.VM, f, target *engine.Object, prefix string, argCount int) {
	length := 0.0
	if vm.HasOwnProperty(target, engine.NamedKey(vm.Key(engine.KeyLength))) {
		if l := vm.GetV(engine.ObjectValue(target), vm.Key(engine.KeyLength)); l.IsNumber() {
			length = math.Max(0, numconv.ToIntegerOrInfinity(l.Number())-float64(argCount))
		}
	}
	name := ""
	if n := vm.GetV(engine.ObjectValue(target), vm.Key(engine.KeyName)); n.IsString() {
		name = n.Object().Str()
	}
	vm.Define(f, vm.Key(engine.KeyLength), engine.Number(length), engine.FlagConfigurable)
	vm.Define(f, vm.Key(engine.KeyName), vm.NewString(prefix+name), engine.FlagConfigurable)
}

func initError(vm *engine.VM) {
	names := [engine.ErrorKindCount]string{
		engine.ErrorKindError:      "Error",
		engine.ErrorKindType:       "TypeError",
		engine.ErrorKindRange:      "RangeError",
		engine.ErrorKindReference:  "ReferenceError",
		engine.ErrorKindSyntax:     "SyntaxError",
		engine.ErrorKindEval:       "EvalError",
		engine.ErrorKindURI:        "URIError",
		engine.ErrorKindAggregate:  "AggregateError",
		engine.ErrorKindSuppressed: "SuppressedError",
	}
	var base *engine.Object
	for kind, name := range names {
		kind := engine.ErrorKind(kind)
		proto := vm.Intrinsic(engine.IntrinsicErrorPrototype)
		if kind != engine.ErrorKindError {
			proto = vm.NewObject(proto)
			vm.SetIntrinsic(engine.ErrorPrototypeIntrinsic(kind), proto)
		}
		length := 1
		switch kind {
		case engine.ErrorKindAggregate:
			length = 2
		case engine.ErrorKindSuppressed:
			length = 3
		}
		ctor := constructor(vm, name, length, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			nt := vm.GetNewTarget()
			e := vm.NewErrorObject(vm.GetPrototypeFromConstructor(nt, proto))
			msgArg, optArg := arg(args, 0), arg(args, 1)
			switch kind {
			case engine.ErrorKindAggregate:
				msgArg, optArg = arg(args, 1), arg(args, 2)
			case engine.ErrorKindSuppressed:
				msgArg, optArg = arg(args, 2), engine.Undefined
			}
			msg := ""
			if !msgArg.IsUndefined() {
				msg = vm.ToString(msgArg)
				vm.Define(e, vm.Key(engine.KeyMessage), vm.NewString(msg), engine.FlagsHidden)
			}
			if optArg.IsObject() && vm.HasProperty(optArg.Object(), engine.NamedKey(vm.Key(engine.KeyCause))) {
				vm.Define(e, vm.Key(engine.KeyCause), vm.GetV(optArg, vm.Key(engine.KeyCause)), engine.FlagsHidden)
			}
			if kind == engine.ErrorKindAggregate {
				var errs []engine.Value
				vm.Iterate(arg(args, 0), func(v engine.Value) bool {
					errs = append(errs, v)
					return true
				})
				vm.Define(e, vm.Key(engine.KeyErrors), engine.ObjectValue(vm.NewArray(errs)), engine.FlagsHidden)
			}
			if kind == engine.ErrorKindSuppressed {
				vm.Define(e, vm.Key(engine.KeyError), arg(args, 0), engine.FlagsHidden)
				vm.Define(e, vm.Key(engine.KeySuppressed), arg(args, 1), engine.FlagsHidden)
			}
			vm.RecordStack(e, wtf8.ToValidUTF8(msg), nt)
			return engine.ObjectValue(e)
		})
		vm.Define(proto, vm.Key(engine.KeyName), vm.SharedString(name), engine.FlagsHidden)
		vm.Define(proto, vm.Key(engine.KeyMessage), vm.NewString(""), engine.FlagsHidden)
		if kind == engine.ErrorKindError {
			base = ctor
		} else {
			vm.SetPrototypeOf(ctor, base)
		}
	}
	method(vm, base, "isError", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.IsError(arg(args, 0)))
	})
	stackKey := engine.NamedKey(vm.Key(engine.KeyStack))
	vm.DefineAccessor(vm.Intrinsic(engine.IntrinsicErrorPrototype), vm.Key(engine.KeyStack),
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
			setterThatIgnoresPrototypeProperties(vm, this, vm.Intrinsic(engine.IntrinsicErrorPrototype), stackKey, v, "Error.prototype.stack setter")
			return engine.Undefined
		}),
		engine.FlagConfigurable)
	method(vm, vm.Intrinsic(engine.IntrinsicErrorPrototype), "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := thisObject(vm, this, "Error.prototype.toString")
		name := "Error"
		if n := vm.GetV(engine.ObjectValue(o), vm.Key(engine.KeyName)); !n.IsUndefined() {
			name = vm.ToString(n)
		}
		msg := ""
		if m := vm.GetV(engine.ObjectValue(o), vm.Key(engine.KeyMessage)); !m.IsUndefined() {
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
	g := vm.GlobalObject().Object()
	eval := method(vm, g, "eval", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.PerformEval(arg(args, 0))
	})
	vm.SetIntrinsic(engine.IntrinsicEval, eval)
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
