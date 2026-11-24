// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package builtin installs the standard ECMAScript built-in objects in each realm.
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

// shortContract is the contract of the kernels that run in a short time.
var shortContract = engine.ShortcutContract{
	Frame: engine.FrameUnobservable,
	Pace:  engine.PaceShort,
}

// polledContract is the contract of the kernels that check for an interruption as they run.
var polledContract = engine.ShortcutContract{
	Frame: engine.FrameUnobservable,
	Pace:  engine.PacePolled,
}

// method defines the non-enumerable function name on o.
func method(cx *engine.ExecutionContext, o *engine.Object, name wtf8.String, length int, f engine.NativeFunc) *engine.Object {
	fn := cx.NewNative(name, length, f)
	cx.Define(o, cx.Intern(name), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// symMethod defines the non-enumerable function under the symbol key on o.
func symMethod(cx *engine.ExecutionContext, o *engine.Object, key *engine.Object, name wtf8.String, length int, f engine.NativeFunc) *engine.Object {
	fn := cx.NewNative(name, length, f)
	cx.Define(o, key, engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// iteratorNext defines the next method of the built-in iterators with the prototype proto,
// which steps the iterator with step and creates its results in the realm that yieldRealm selects.
func iteratorNext(cx *engine.ExecutionContext, proto *engine.Object, yieldRealm engine.IteratorYieldRealm, step engine.IteratorStepFunc) *engine.Object {
	fn := cx.NewIteratorNext(yieldRealm, step)
	cx.Define(proto, cx.Intern("next"), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// plainIteratorNext defines a next method whose step runs no JavaScript and cannot throw.
func plainIteratorNext[T any](cx *engine.ExecutionContext, proto *engine.Object, yieldRealm engine.IteratorYieldRealm, receiverError string, step func(*engine.ExecutionContext, *engine.Object, T) (engine.Value, bool)) *engine.Object {
	fn := engine.NewPlainIteratorNext(cx, yieldRealm, receiverError, step)
	cx.Define(proto, cx.Intern("next"), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// getter defines the accessor property key on o with the getter f, named "get " + name, and
// returns the getter.
func getter(cx *engine.ExecutionContext, o *engine.Object, key *engine.Object, name wtf8.String, f engine.NativeFunc) *engine.Object {
	g := cx.NewNative("get "+name, 0, f)
	cx.DefineAccessor(o, key, g, nil, engine.FlagConfigurable)
	return g
}

// constant defines the property name of o that cannot be changed.
func constant(cx *engine.ExecutionContext, o *engine.Object, name wtf8.String, v engine.Value) {
	cx.Define(o, cx.Intern(name), v, 0)
}

// constructor creates the constructor function name with the intrinsic prototype protoID,
// and defines it as a global.
func constructor(cx *engine.ExecutionContext, name wtf8.String, length int, protoID engine.Intrinsic, f engine.NativeFunc) *engine.Object {
	c := cx.NewNativeConstructor(name, length, f)
	cx.DefineConstructorPrototype(c, protoID)
	cx.Define(cx.Intrinsic(protoID), cx.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	cx.Define(cx.GlobalObject().Object(), cx.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
	return c
}

// thisObject returns this as an object, or throws a TypeError naming the method.
func thisObject(cx *engine.ExecutionContext, this engine.Value, what string) *engine.Object {
	if !this.IsObject() {
		cx.ThrowTypeError(what + " called on non-object")
	}
	return this.Object()
}

// setterThatIgnoresPrototypeProperties implements SetterThatIgnoresPrototypeProperties:
// it sets the property key of this to v unless this is home. what names the setter in errors.
func setterThatIgnoresPrototypeProperties(cx *engine.ExecutionContext, this engine.Value, home *engine.Object, key engine.PropKey, v engine.Value, what string) {
	if !this.IsObject() {
		cx.ThrowTypeError(what + " called on non-object")
	}
	o := this.Object()
	if o == home {
		cx.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "'")
	}
	if _, ok := cx.GetOwnProperty(o, key); !ok {
		cx.CreateDataPropertyOrThrow(o, key, v)
		return
	}
	cx.Set(o, key, v)
}

// extensions are the functions that [RegisterExtension] registers.
var extensions []func(cx *engine.ExecutionContext)

// RegisterExtension registers install to define further built-in objects in each realm that
// [Install] creates, after the standard ones. It must be called before any VM is created,
// typically from an init function.
func RegisterExtension(install func(cx *engine.ExecutionContext)) {
	extensions = append(extensions, install)
}

// Install creates the standard built-in objects of the realm of cx, and those of the registered extensions.
func Install(cx *engine.ExecutionContext) {
	cx.StartBuiltins()
	objectProto := cx.Intrinsic(engine.IntrinsicObjectPrototype)
	cx.SetIntrinsic(engine.IntrinsicNumberPrototype, cx.NewNumberObject(0, objectProto))
	cx.SetIntrinsic(engine.IntrinsicBooleanPrototype, cx.NewBooleanObject(false, objectProto))
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicSymbolPrototype, engine.IntrinsicBigIntPrototype, engine.IntrinsicErrorPrototype,
		engine.IntrinsicIteratorPrototype, engine.IntrinsicAsyncIteratorPrototype, engine.IntrinsicRegExpPrototype,
		engine.IntrinsicPromisePrototype, engine.IntrinsicMapPrototype, engine.IntrinsicSetPrototype,
		engine.IntrinsicDatePrototype,
	} {
		cx.SetIntrinsic(id, cx.NewObject(objectProto))
	}
	iteratorProto := cx.Intrinsic(engine.IntrinsicIteratorPrototype)
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicArrayIteratorPrototype, engine.IntrinsicStringIteratorPrototype, engine.IntrinsicGeneratorPrototype,
	} {
		cx.SetIntrinsic(id, cx.NewObject(iteratorProto))
	}
	cx.SetIntrinsic(engine.IntrinsicAsyncGeneratorPrototype, cx.NewObject(cx.Intrinsic(engine.IntrinsicAsyncIteratorPrototype)))
	functionProto := cx.Intrinsic(engine.IntrinsicFunctionPrototype)
	for _, id := range []engine.Intrinsic{
		engine.IntrinsicGeneratorFunctionPrototype, engine.IntrinsicAsyncFunctionPrototype, engine.IntrinsicAsyncGeneratorFunctionPrototype,
	} {
		cx.SetIntrinsic(id, cx.NewObject(functionProto))
	}

	g := cx.GlobalObject().Object()
	cx.Define(g, cx.Intern("globalThis"), engine.ObjectValue(g), engine.FlagsHidden)
	constant(cx, g, "undefined", engine.Undefined)
	constant(cx, g, "NaN", engine.Number(math.NaN()))
	constant(cx, g, "Infinity", engine.Number(math.Inf(1)))

	throwTypeError := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.ThrowTypeError("'caller', 'callee', and 'arguments' properties may not be accessed on strict mode functions or the arguments objects for calls to them")
		return engine.Undefined
	})
	setIntegrity(cx, throwTypeError, true)
	cx.SetIntrinsic(engine.IntrinsicThrowTypeError, throwTypeError)

	var strs objectStrings
	initObject(cx, &strs)
	initFunction(cx)
	initError(cx)
	initArray(cx, &strs)
	initString(cx)
	initRegExp(cx)
	parseFloat, parseInt := newParseFunctions(cx)
	initNumber(cx, parseFloat, parseInt)
	initSymbol(cx)
	initBigInt(cx)
	initMath(cx)
	initIterators(cx)
	initIteratorHelpers(cx)
	initJSON(cx)
	initReflect(cx)
	initProxy(cx)
	initMapAndSet(cx)
	initWeak(cx)
	initDate(cx)
	initTemporal(cx)
	initShadowRealm(cx)
	initAbstractModuleSource(cx)
	initPromise(cx)
	initAsync(cx)
	initDisposableStacks(cx)
	initBinary(cx)
	initGlobalFunctions(cx, parseFloat, parseInt)
	for _, install := range extensions {
		install(cx)
	}
	cx.FinishBuiltins()
}

// objectStrings holds the results of Object.prototype.toString of one realm by tag.
type objectStrings struct {
	byTag map[wtf8.String]engine.Value
}

// objectStringsLimit bounds the number of tags that an objectStrings keeps.
const objectStringsLimit = 64

// forTag returns the string "[object tag]".
func (s *objectStrings) forTag(cx *engine.ExecutionContext, tag wtf8.String) engine.Value {
	if v, ok := s.byTag[tag]; ok {
		return v
	}
	checkStringLength(cx, len(tag)+9)
	v := cx.NewString("[object " + tag + "]")
	if len(s.byTag) < objectStringsLimit {
		if s.byTag == nil {
			s.byTag = map[wtf8.String]engine.Value{}
		}
		s.byTag[tag] = v
	}
	return v
}

// toString implements Object.prototype.toString for the value v, which is neither undefined nor null.
func (s *objectStrings) toString(cx *engine.ExecutionContext, v engine.Value) engine.Value {
	if !v.IsObject() {
		// A wrapper object from ToObject is observable only by code that the lookup of
		// @@toStringTag runs.
		if t, ok := cx.GetVWithoutCall(v, cx.WellKnownSymbol(engine.SymbolToStringTag)); ok {
			if t.IsString() {
				return s.forTag(cx, t.Str())
			}
			return s.forTag(cx, primitiveTag(v))
		}
	}
	return s.objectToString(cx, cx.ToObject(v))
}

// primitiveTag returns the builtinTag of Object.prototype.toString for the object that
// ToObject makes of the primitive v.
func primitiveTag(v engine.Value) wtf8.String {
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
func (s *objectStrings) objectToString(cx *engine.ExecutionContext, o *engine.Object) engine.Value {
	tag := cx.BuiltinTag(o)
	if t := cx.GetV(engine.ObjectValue(o), cx.WellKnownSymbol(engine.SymbolToStringTag)); t.IsString() {
		tag = t.Str()
	}
	return s.forTag(cx, tag)
}

// setIntegrity implements SetIntegrityLevel. frozen selects frozen over sealed.
func setIntegrity(cx *engine.ExecutionContext, o *engine.Object, frozen bool) {
	if !cx.PreventExtensions(o) {
		cx.ThrowTypeError("cannot prevent extensions")
	}
	var pacer engine.Pacer
	for _, k := range cx.OwnKeys(o) {
		pacer.Step(cx)
		var d engine.PropDesc
		d.SetConfigurable(false)
		if frozen {
			cur, ok := cx.GetOwnProperty(o, k)
			if !ok {
				continue
			}
			if !cur.IsAccessor() {
				d.SetWritable(false)
			}
		}
		cx.DefinePropertyOrThrow(o, k, d)
	}
}

// testIntegrity implements TestIntegrityLevel.
func testIntegrity(cx *engine.ExecutionContext, o *engine.Object, frozen bool) bool {
	if cx.IsExtensible(o) {
		return false
	}
	var pacer engine.Pacer
	for _, k := range cx.OwnKeys(o) {
		pacer.Step(cx)
		d, ok := cx.GetOwnProperty(o, k)
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

// enumerableOwn returns the values of the enumerable own string-keyed properties of o, or their
// key-value pairs when entries is set.
func enumerableOwn(cx *engine.ExecutionContext, o *engine.Object, entries bool) []engine.Value {
	ks := cx.OwnKeys(o)
	out := make([]engine.Value, 0, len(ks))
	var pacer engine.Pacer
	for _, k := range ks {
		pacer.Step(cx)
		if k.IsSymbol() {
			continue
		}
		d, ok := cx.GetOwnProperty(o, k)
		if !ok || !d.Enumerable() {
			continue
		}
		v := cx.Get(o, k)
		if entries {
			v = engine.ObjectValue(cx.NewArrayPair(cx.KeyValue(k), v))
		}
		out = append(out, v)
	}
	return out
}

func initObject(cx *engine.ExecutionContext, strs *objectStrings) {
	proto := cx.Intrinsic(engine.IntrinsicObjectPrototype)
	ctor := constructor(cx, "Object", 1, engine.IntrinsicObjectPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if nt := cx.GetNewTarget(); nt != nil && nt != cx.Intrinsic(engine.IntrinsicObject) {
			return engine.ObjectValue(cx.NewInstance(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicObjectPrototype)))
		}
		v := arg(args, 0)
		if v.IsNullish() {
			return engine.ObjectValue(cx.NewOrdinaryObject())
		}
		return engine.ObjectValue(cx.ToObject(v))
	})
	cx.SetIntrinsic(engine.IntrinsicObject, ctor)

	hasOwn := method(cx, proto, "hasOwnProperty", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		k := cx.QueryKey(arg(args, 0))
		return engine.Bool(cx.HasOwnProperty(cx.ToObject(this), k))
	})
	cx.SetMethodKernels(hasOwn, shortContract, nil, func(cx *engine.ExecutionContext, this, k engine.Value) (engine.Value, bool) {
		if !this.IsObject() {
			return engine.Undefined, false
		}
		has, ok := cx.HasOwnPropertyPlain(this.Object(), k)
		return engine.Bool(has), ok
	}, nil)
	method(cx, proto, "isPrototypeOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		v := arg(args, 0)
		if !v.IsObject() {
			return engine.False
		}
		o := cx.ToObject(this)
		for p := cx.ProtoOf(v.Object()); p != nil; p = cx.ProtoOf(p) {
			if p == o {
				return engine.True
			}
		}
		return engine.False
	})
	method(cx, proto, "propertyIsEnumerable", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		k := cx.ToPropertyKey(arg(args, 0))
		d, ok := cx.GetOwnProperty(cx.ToObject(this), k)
		return engine.Bool(ok && d.Enumerable())
	})
	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		switch {
		case this.IsUndefined():
			return strs.forTag(cx, "Undefined")
		case this.IsNull():
			return strs.forTag(cx, "Null")
		}
		return strs.toString(cx, this)
	})
	method(cx, proto, "toLocaleString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.CallValue(cx.GetV(this, cx.Key(engine.KeyToString)), this)
	})
	method(cx, proto, "valueOf", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(cx.ToObject(this))
	})
	protoKey := cx.Intern("__proto__")
	cx.DefineAccessor(proto, protoKey,
		cx.NewNative("get __proto__", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			if p := cx.ProtoOf(cx.ToObject(this)); p != nil {
				return engine.ObjectValue(p)
			}
			return engine.Null
		}),
		cx.NewNative("set __proto__", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			cx.RequireObjectCoercible(this, "Object.prototype.__proto__")
			p := arg(args, 0)
			if !this.IsObject() || !(p.IsObject() || p.IsNull()) {
				return engine.Undefined
			}
			var po *engine.Object
			if p.IsObject() {
				po = p.Object()
			}
			if !cx.SetPrototypeOf(this.Object(), po) {
				cx.ThrowTypeError("cannot set prototype")
			}
			return engine.Undefined
		}),
		engine.FlagConfigurable)

	defineAccessor := func(name string, setter bool) {
		method(cx, proto, wtf8.String(name), 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			o := cx.ToObject(this)
			f := arg(args, 1)
			if !f.IsCallable() {
				cx.ThrowTypeError("Object.prototype." + name + ": expecting function")
			}
			var d engine.PropDesc
			d.SetEnumerable(true)
			d.SetConfigurable(true)
			if setter {
				d.SetSetter(f.Object())
			} else {
				d.SetGetter(f.Object())
			}
			cx.DefinePropertyOrThrow(o, cx.ToPropertyKey(arg(args, 0)), d)
			return engine.Undefined
		})
	}
	defineAccessor("__defineGetter__", false)
	defineAccessor("__defineSetter__", true)
	lookupAccessor := func(name string, setter bool) {
		method(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			o := cx.ToObject(this)
			k := cx.ToPropertyKey(arg(args, 0))
			for ; o != nil; o = cx.ProtoOf(o) {
				d, ok := cx.GetOwnProperty(o, k)
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

	method(cx, ctor, "getPrototypeOf", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if p := cx.ProtoOf(cx.ToObject(arg(args, 0))); p != nil {
			return engine.ObjectValue(p)
		}
		return engine.Null
	})
	method(cx, ctor, "setPrototypeOf", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o, p := arg(args, 0), arg(args, 1)
		cx.RequireObjectCoercible(o, "Object.setPrototypeOf")
		if !p.IsObject() && !p.IsNull() {
			cx.ThrowTypeError("object prototype may only be an Object or null: " + cx.DisplayString(p))
		}
		if !o.IsObject() {
			return o
		}
		var po *engine.Object
		if p.IsObject() {
			po = p.Object()
		}
		if !cx.SetPrototypeOf(o.Object(), po) {
			cx.ThrowTypeError("cannot set prototype")
		}
		return o
	})
	defineProperties := func(cx *engine.ExecutionContext, o *engine.Object, props engine.Value) {
		src := cx.ToObject(props)
		type entry struct {
			k engine.PropKey
			d engine.PropDesc
		}
		var list []entry
		var pacer engine.Pacer
		for _, k := range cx.OwnKeys(src) {
			pacer.Step(cx)
			d, ok := cx.GetOwnProperty(src, k)
			if !ok || !d.Enumerable() {
				continue
			}
			list = append(list, entry{
				k: k,
				d: cx.ToPropertyDescriptor(cx.Get(src, k)),
			})
		}
		for _, e := range list {
			pacer.Step(cx)
			cx.DefinePropertyOrThrow(o, e.k, e.d)
		}
	}
	method(cx, ctor, "create", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		p := arg(args, 0)
		if !p.IsObject() && !p.IsNull() {
			cx.ThrowTypeError("object prototype may only be an Object or null: " + cx.DisplayString(p))
		}
		var po *engine.Object
		if p.IsObject() {
			po = p.Object()
		}
		o := cx.NewObject(po)
		if props := arg(args, 1); !props.IsUndefined() {
			defineProperties(cx, o, props)
		}
		return engine.ObjectValue(o)
	})
	method(cx, ctor, "defineProperty", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		if !o.IsObject() {
			cx.ThrowTypeError("Object.defineProperty called on non-object")
		}
		k := cx.ToPropertyKey(arg(args, 1))
		cx.DefinePropertyOrThrow(o.Object(), k, cx.ToPropertyDescriptor(arg(args, 2)))
		return o
	})
	method(cx, ctor, "defineProperties", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		if !o.IsObject() {
			cx.ThrowTypeError("Object.defineProperties called on non-object")
		}
		defineProperties(cx, o.Object(), arg(args, 1))
		return o
	})
	method(cx, ctor, "getOwnPropertyDescriptor", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(arg(args, 0))
		d, ok := cx.GetOwnProperty(o, cx.ToPropertyKey(arg(args, 1)))
		if !ok {
			return engine.Undefined
		}
		return cx.FromPropertyDescriptor(d)
	})
	method(cx, ctor, "getOwnPropertyDescriptors", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(arg(args, 0))
		out := cx.NewOrdinaryObject()
		var pacer engine.Pacer
		for _, k := range cx.OwnKeys(o) {
			pacer.Step(cx)
			if d, ok := cx.GetOwnProperty(o, k); ok {
				cx.CreateDataPropertyOrThrow(out, k, cx.FromPropertyDescriptor(d))
			}
		}
		return engine.ObjectValue(out)
	})
	method(cx, ctor, "getOwnPropertyNames", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var out []engine.Value
		var pacer engine.Pacer
		for _, k := range cx.OwnKeys(cx.ToObject(arg(args, 0))) {
			pacer.Step(cx)
			if !k.IsSymbol() {
				out = append(out, cx.KeyValue(k))
			}
		}
		return engine.ObjectValue(cx.NewArray(out))
	})
	method(cx, ctor, "getOwnPropertySymbols", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var out []engine.Value
		var pacer engine.Pacer
		for _, k := range cx.OwnKeys(cx.ToObject(arg(args, 0))) {
			pacer.Step(cx)
			if k.IsSymbol() {
				out = append(out, cx.KeyValue(k))
			}
		}
		return engine.ObjectValue(cx.NewArray(out))
	})
	method(cx, ctor, "keys", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		ks := cx.EnumerableOwnKeys(cx.ToObject(arg(args, 0)), nil)
		out := make([]engine.Value, len(ks))
		var pacer engine.Pacer
		for i, k := range ks {
			pacer.Step(cx)
			out[i] = cx.KeyValue(k)
		}
		return engine.ObjectValue(cx.NewArray(out))
	})
	method(cx, ctor, "values", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(cx.NewArray(enumerableOwn(cx, cx.ToObject(arg(args, 0)), false)))
	})
	method(cx, ctor, "entries", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.ObjectValue(cx.NewArray(enumerableOwn(cx, cx.ToObject(arg(args, 0)), true)))
	})
	method(cx, ctor, "assign", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		to := cx.ToObject(arg(args, 0))
		for _, src := range args[min(1, len(args)):] {
			if src.IsNullish() {
				continue
			}
			from := cx.ToObject(src)
			if cx.AssignDataProperties(to, from) {
				continue
			}
			var pacer engine.Pacer
			for _, k := range cx.OwnKeys(from) {
				pacer.Step(cx)
				d, ok := cx.GetOwnProperty(from, k)
				if !ok || !d.Enumerable() {
					continue
				}
				cx.Set(to, k, cx.Get(from, k))
			}
		}
		return engine.ObjectValue(to)
	})
	method(cx, ctor, "freeze", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			setIntegrity(cx, o.Object(), true)
		}
		return arg(args, 0)
	})
	method(cx, ctor, "seal", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			setIntegrity(cx, o.Object(), false)
		}
		return arg(args, 0)
	})
	method(cx, ctor, "preventExtensions", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if o := arg(args, 0); o.IsObject() {
			if !cx.PreventExtensions(o.Object()) {
				cx.ThrowTypeError("cannot prevent extensions")
			}
		}
		return arg(args, 0)
	})
	method(cx, ctor, "isFrozen", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(!o.IsObject() || testIntegrity(cx, o.Object(), true))
	})
	method(cx, ctor, "isSealed", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(!o.IsObject() || testIntegrity(cx, o.Object(), false))
	})
	method(cx, ctor, "isExtensible", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := arg(args, 0)
		return engine.Bool(o.IsObject() && cx.IsExtensible(o.Object()))
	})
	method(cx, ctor, "is", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.SameValue(arg(args, 0), arg(args, 1)))
	})
	method(cx, ctor, "fromEntries", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.NewOrdinaryObject()
		cx.Iterate(arg(args, 0), func(e engine.Value) bool {
			if !e.IsObject() {
				cx.ThrowTypeError("iterator value " + cx.DisplayString(e) + " is not an entry object")
			}
			k := cx.Index(e, 0)
			v := cx.Index(e, 1)
			cx.CreateDataPropertyOrThrow(o, cx.ToPropertyKey(k), v)
			return true
		})
		return engine.ObjectValue(o)
	})
	method(cx, ctor, "hasOwn", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := cx.ToObject(arg(args, 0))
		return engine.Bool(cx.HasOwnProperty(o, cx.QueryKey(arg(args, 1))))
	})
}

// listFromArrayLike implements CreateListFromArrayLike.
func listFromArrayLike(cx *engine.ExecutionContext, v engine.Value) []engine.Value {
	if v.IsNullish() {
		return nil
	}
	if !v.IsObject() {
		cx.ThrowTypeError("CreateListFromArrayLike called on non-object")
	}
	if list, ok := v.Object().ElementList(); ok {
		return list
	}
	n := numconv.ToLength(cx.ToNumber(cx.GetV(v, cx.Key(engine.KeyLength))))
	if n > 1<<24 {
		cx.ThrowRangeError("too many arguments")
	}
	list := make([]engine.Value, n)
	for i := 0; i < len(list); i += engine.PollInterval {
		if i > 0 {
			cx.Poll()
		}
		indexValues(cx, list[i:min(i+engine.PollInterval, len(list))], v, i)
	}
	return list
}

// indexValues sets the elements of out to the properties of v from the index start on.
func indexValues(cx *engine.ExecutionContext, out []engine.Value, v engine.Value, start int) {
	for i := range out {
		out[i] = cx.Index(v, uint32(start+i))
	}
}

func initFunction(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicFunctionPrototype)
	var ctor *engine.Object
	ctor = constructor(cx, "Function", 1, engine.IntrinsicFunctionPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.CreateDynamicFunction(ctor, cx.GetNewTarget(), engine.NormalFunction, args)
	})
	cx.SetIntrinsic(engine.IntrinsicFunction, ctor)

	call := method(cx, proto, "call", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			cx.ThrowTypeError("Function.prototype.call called on a value that is not a function")
		}
		return cx.Call(this.Object(), arg(args, 0), args[min(1, len(args)):]...)
	})
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeCall, call)
	// The interpreter calls the targets of call and apply directly where it can, so every call
	// of them is invisible to stack traces and to the caller of the functions they call.
	cx.SetFrameless(call)
	apply := method(cx, proto, "apply", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			cx.ThrowTypeError("Function.prototype.apply called on a value that is not a function")
		}
		return cx.Call(this.Object(), arg(args, 0), listFromArrayLike(cx, arg(args, 1))...)
	})
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeApply, apply)
	cx.SetFrameless(apply)
	method(cx, proto, "bind", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			cx.ThrowTypeError("bind must be called on a function")
		}
		target := this.Object()
		b := cx.BoundFunctionCreate(target, arg(args, 0), args[min(1, len(args)):])
		copyNameAndLength(cx, b, target, "bound ", max(0, len(args)-1))
		return engine.ObjectValue(b)
	})
	method(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if !this.IsCallable() {
			cx.ThrowTypeError("Function.prototype.toString requires that 'this' be a Function")
		}
		src := this.Object().SourceText()
		checkStringLength(cx, len(src))
		return cx.NewString(src)
	})
	hasInstance := cx.NewNative("[Symbol.hasInstance]", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(cx.OrdinaryHasInstance(this, arg(args, 0)))
	})
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeHasInstance, hasInstance)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolHasInstance), engine.ObjectValue(hasInstance), 0)
	// Function.prototype[Symbol.metadata] of the decorator metadata proposal.
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolMetadata), engine.Null, 0)

	// AddRestrictedFunctionProperties
	throwTypeError := cx.Intrinsic(engine.IntrinsicThrowTypeError)
	cx.DefineAccessor(proto, cx.Intern("caller"), throwTypeError, throwTypeError, engine.FlagConfigurable)
	cx.DefineAccessor(proto, cx.Intern("arguments"), throwTypeError, throwTypeError, engine.FlagConfigurable)

	// The getters of the own caller and arguments properties of non-strict functions.
	cx.SetIntrinsic(engine.IntrinsicFunctionCallerGetter, cx.NewNative("get caller", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.FunctionCaller(this)
	}))
	cx.SetIntrinsic(engine.IntrinsicFunctionArgumentsGetter, cx.NewNative("get arguments", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.FunctionArguments(this)
	}))
}

// copyNameAndLength implements CopyNameAndLength.
func copyNameAndLength(cx *engine.ExecutionContext, f, target *engine.Object, prefix wtf8.String, argCount int) {
	length := 0.0
	if cx.HasOwnProperty(target, engine.NamedKey(cx.Key(engine.KeyLength))) {
		if l := cx.GetV(engine.ObjectValue(target), cx.Key(engine.KeyLength)); l.IsNumber() {
			length = math.Max(0, numconv.ToIntegerOrInfinity(l.Number())-float64(argCount))
		}
	}
	name := cx.NewString(prefix)
	if n := cx.GetV(engine.ObjectValue(target), cx.Key(engine.KeyName)); n.IsString() {
		// Concatenating the values keeps a chain of bound functions linear in its length.
		name = cx.Concat(name, n)
	}
	cx.Define(f, cx.Key(engine.KeyLength), engine.Number(length), engine.FlagConfigurable)
	cx.Define(f, cx.Key(engine.KeyName), name, engine.FlagConfigurable)
}

func initError(cx *engine.ExecutionContext) {
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
		protoID := engine.ErrorPrototypeIntrinsic(kind)
		proto := cx.Intrinsic(engine.IntrinsicErrorPrototype)
		if kind != engine.ErrorKindError {
			proto = cx.NewObject(proto)
			cx.SetIntrinsic(protoID, proto)
		}
		length := 1
		switch kind {
		case engine.ErrorKindAggregate:
			length = 2
		case engine.ErrorKindSuppressed:
			length = 3
		}
		ctor := constructor(cx, wtf8.String(name), length, protoID, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			nt := cx.GetNewTarget()
			e := cx.NewErrorObject(cx.GetPrototypeFromConstructor(nt, protoID))
			msgArg, optArg := arg(args, 0), arg(args, 1)
			switch kind {
			case engine.ErrorKindAggregate:
				msgArg, optArg = arg(args, 1), arg(args, 2)
			case engine.ErrorKindSuppressed:
				msgArg, optArg = arg(args, 2), engine.Undefined
			}
			var msg wtf8.String
			if !msgArg.IsUndefined() {
				msg = cx.ToString(msgArg)
				cx.Define(e, cx.Key(engine.KeyMessage), cx.NewString(msg), engine.FlagsHidden)
			}
			if optArg.IsObject() && cx.HasProperty(optArg.Object(), engine.NamedKey(cx.Key(engine.KeyCause))) {
				cx.Define(e, cx.Key(engine.KeyCause), cx.GetV(optArg, cx.Key(engine.KeyCause)), engine.FlagsHidden)
			}
			if kind == engine.ErrorKindAggregate {
				var errs []engine.Value
				cx.Iterate(arg(args, 0), func(v engine.Value) bool {
					errs = append(errs, v)
					return true
				})
				cx.Define(e, cx.Key(engine.KeyErrors), engine.ObjectValue(cx.NewArray(errs)), engine.FlagsHidden)
			}
			if kind == engine.ErrorKindSuppressed {
				cx.Define(e, cx.Key(engine.KeyError), arg(args, 0), engine.FlagsHidden)
				cx.Define(e, cx.Key(engine.KeySuppressed), arg(args, 1), engine.FlagsHidden)
			}
			cx.RecordStack(e, wtf8.ToValidUTF8(msg), nt)
			return engine.ObjectValue(e)
		})
		cx.Define(proto, cx.Key(engine.KeyName), cx.SharedString(wtf8.String(name)), engine.FlagsHidden)
		cx.Define(proto, cx.Key(engine.KeyMessage), cx.NewString(""), engine.FlagsHidden)
		if kind == engine.ErrorKindError {
			base = ctor
		} else {
			cx.SetPrototypeOf(ctor, base)
		}
	}
	method(cx, base, "isError", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(engine.IsError(arg(args, 0)))
	})
	stackKey := engine.NamedKey(cx.Key(engine.KeyStack))
	cx.DefineAccessor(cx.Intrinsic(engine.IntrinsicErrorPrototype), cx.Key(engine.KeyStack),
		cx.NewNative("get stack", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				cx.ThrowTypeError("Error.prototype.stack getter called on non-object")
			}
			if s, ok := engine.ErrorStack(this); ok {
				return cx.NewString(s)
			}
			return engine.Undefined
		}),
		cx.NewNative("set stack", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			if !this.IsObject() {
				cx.ThrowTypeError("Error.prototype.stack setter called on non-object")
			}
			v := arg(args, 0)
			if !v.IsString() {
				cx.ThrowTypeError("Error.prototype.stack setter requires a string")
			}
			setterThatIgnoresPrototypeProperties(cx, this, cx.Intrinsic(engine.IntrinsicErrorPrototype), stackKey, v, "Error.prototype.stack setter")
			return engine.Undefined
		}),
		engine.FlagConfigurable)
	method(cx, cx.Intrinsic(engine.IntrinsicErrorPrototype), "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := thisObject(cx, this, "Error.prototype.toString")
		name := wtf8.String("Error")
		if n := cx.GetV(engine.ObjectValue(o), cx.Key(engine.KeyName)); !n.IsUndefined() {
			name = cx.ToString(n)
		}
		var msg wtf8.String
		if m := cx.GetV(engine.ObjectValue(o), cx.Key(engine.KeyMessage)); !m.IsUndefined() {
			msg = cx.ToString(m)
		}
		switch {
		case name == "":
			return cx.NewString(msg)
		case msg == "":
			return cx.NewString(name)
		}
		checkStringLength(cx, len(name)+2+len(msg))
		return cx.NewString(name + ": " + msg)
	})
}

// newParseFunctions creates the functions parseFloat and parseInt.
func newParseFunctions(cx *engine.ExecutionContext) (parseFloat, parseInt *engine.Object) {
	parseFloat = cx.NewNative("parseFloat", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(numconv.ParseFloat(cx.ToString(arg(args, 0)), cx.Poll))
	})
	parseInt = cx.NewNative("parseInt", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := cx.ToString(arg(args, 0))
		return engine.Number(numconv.ParseInt(s, int(cx.ToInt32(arg(args, 1))), cx.Poll))
	})
	return parseFloat, parseInt
}

// initGlobalFunctions defines the function properties of the global object.
// parseFloat and parseInt are the functions to define under those names.
func initGlobalFunctions(cx *engine.ExecutionContext, parseFloat, parseInt *engine.Object) {
	g := cx.GlobalObject().Object()
	eval := method(cx, g, "eval", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.PerformEval(arg(args, 0))
	})
	cx.SetIntrinsic(engine.IntrinsicEval, eval)
	method(cx, g, "isNaN", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		n := cx.ToNumber(arg(args, 0))
		return engine.Bool(n != n)
	})
	method(cx, g, "isFinite", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		n := cx.ToNumber(arg(args, 0))
		return engine.Bool(n == n && !math.IsInf(n, 0))
	})
	cx.Define(g, cx.Intern("parseFloat"), engine.ObjectValue(parseFloat), engine.FlagsHidden)
	cx.Define(g, cx.Intern("parseInt"), engine.ObjectValue(parseInt), engine.FlagsHidden)
	initURIFunctions(cx)
}
