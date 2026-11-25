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
func method(cx *engine.ExecutionContext, o *engine.Object, name wtf8.String, length int, f engine.Native) *engine.Object {
	fn := cx.NewNative(name, length, f)
	cx.Define(o, cx.Intern(name), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// methodWithState is [method] for a function whose internal slots are state.
func methodWithState(cx *engine.ExecutionContext, o *engine.Object, name wtf8.String, length int, f engine.Native, state any) *engine.Object {
	fn := cx.NewNativeWithState(name, length, f, state)
	cx.Define(o, cx.Intern(name), engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// symMethod defines the non-enumerable function under the symbol key on o.
func symMethod(cx *engine.ExecutionContext, o *engine.Object, key *engine.Object, name wtf8.String, length int, f engine.Native) *engine.Object {
	fn := cx.NewNative(name, length, f)
	cx.Define(o, key, engine.ObjectValue(fn), engine.FlagsHidden)
	return fn
}

// getter defines the accessor property key on o with the getter f, named "get " + name, and
// returns the getter.
func getter(cx *engine.ExecutionContext, o *engine.Object, key *engine.Object, name wtf8.String, f engine.Native) *engine.Object {
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
func constructor(cx *engine.ExecutionContext, name wtf8.String, length int, protoID engine.Intrinsic, f engine.Native) *engine.Object {
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

var nativeThrowRestrictedProperty = engine.RegisterNative(throwRestrictedProperty)

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

	throwTypeError := cx.NewNative("", 0, nativeThrowRestrictedProperty)
	setIntegrity(cx, throwTypeError, true)
	cx.SetIntrinsic(engine.IntrinsicThrowTypeError, throwTypeError)

	initObject(cx)
	initFunction(cx)
	initError(cx)
	initArray(cx)
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

func throwRestrictedProperty(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	cx.ThrowTypeError("'caller', 'callee', and 'arguments' properties may not be accessed on strict mode functions or the arguments objects for calls to them")
	return engine.Undefined
}

// objectPrototypeToString implements Object.prototype.toString for the value v, which is neither undefined nor null.
func objectPrototypeToString(cx *engine.ExecutionContext, v engine.Value) engine.Value {
	if !v.IsObject() {
		// A wrapper object from ToObject is observable only by code that the lookup of
		// @@toStringTag runs.
		if t, ok := cx.Quiet().GetVWithoutCall(v, cx.WellKnownSymbol(engine.SymbolToStringTag)); ok {
			if t.IsString() {
				return cx.ObjectTagString(t.Str())
			}
			return cx.ObjectTagString(primitiveTag(v))
		}
	}
	return objectToString(cx, cx.ToObject(v))
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
func objectToString(cx *engine.ExecutionContext, o *engine.Object) engine.Value {
	tag := cx.BuiltinTag(o)
	if t := cx.GetV(engine.ObjectValue(o), cx.WellKnownSymbol(engine.SymbolToStringTag)); t.IsString() {
		tag = t.Str()
	}
	return cx.ObjectTagString(tag)
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

func objectDefineProperties(cx *engine.ExecutionContext, o *engine.Object, props engine.Value) {
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

var (
	nativeObjectConstructor    = engine.RegisterNative(objectConstructor)
	nativeObjectHasOwnProperty = engine.RegisterNativeWithKernels(objectHasOwnProperty, engine.Kernels{
		Method1:  objectHasOwnPropertyKernel,
		Contract: shortContract,
	})
	nativeObjectIsPrototypeOf             = engine.RegisterNative(objectIsPrototypeOf)
	nativeObjectPropertyIsEnumerable      = engine.RegisterNative(objectPropertyIsEnumerable)
	nativeObjectToStringMethod            = engine.RegisterNative(objectToStringMethod)
	nativeObjectToLocaleString            = engine.RegisterNative(objectToLocaleString)
	nativeObjectValueOf                   = engine.RegisterNative(objectValueOf)
	nativeObjectProtoGetter               = engine.RegisterNative(objectProtoGetter)
	nativeObjectProtoSetter               = engine.RegisterNative(objectProtoSetter)
	nativeObjectDefineGetter              = engine.RegisterNative(objectDefineGetter)
	nativeObjectDefineSetter              = engine.RegisterNative(objectDefineSetter)
	nativeObjectLookupGetter              = engine.RegisterNative(objectLookupGetter)
	nativeObjectLookupSetter              = engine.RegisterNative(objectLookupSetter)
	nativeObjectGetPrototypeOf            = engine.RegisterNative(objectGetPrototypeOf)
	nativeObjectSetPrototypeOf            = engine.RegisterNative(objectSetPrototypeOf)
	nativeObjectCreate                    = engine.RegisterNative(objectCreate)
	nativeObjectDefineProperty            = engine.RegisterNative(objectDefineProperty)
	nativeObjectDefinePropertiesMethod    = engine.RegisterNative(objectDefinePropertiesMethod)
	nativeObjectGetOwnPropertyDescriptor  = engine.RegisterNative(objectGetOwnPropertyDescriptor)
	nativeObjectGetOwnPropertyDescriptors = engine.RegisterNative(objectGetOwnPropertyDescriptors)
	nativeObjectGetOwnPropertyNames       = engine.RegisterNative(objectGetOwnPropertyNames)
	nativeObjectGetOwnPropertySymbols     = engine.RegisterNative(objectGetOwnPropertySymbols)
	nativeObjectKeys                      = engine.RegisterNative(objectKeys)
	nativeObjectValues                    = engine.RegisterNative(objectValues)
	nativeObjectEntries                   = engine.RegisterNative(objectEntries)
	nativeObjectAssign                    = engine.RegisterNative(objectAssign)
	nativeObjectFreeze                    = engine.RegisterNative(objectFreeze)
	nativeObjectSeal                      = engine.RegisterNative(objectSeal)
	nativeObjectPreventExtensions         = engine.RegisterNative(objectPreventExtensions)
	nativeObjectIsFrozen                  = engine.RegisterNative(objectIsFrozen)
	nativeObjectIsSealed                  = engine.RegisterNative(objectIsSealed)
	nativeObjectIsExtensible              = engine.RegisterNative(objectIsExtensible)
	nativeObjectIs                        = engine.RegisterNative(objectIs)
	nativeObjectFromEntries               = engine.RegisterNative(objectFromEntries)
	nativeObjectHasOwn                    = engine.RegisterNative(objectHasOwn)
)

func initObject(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicObjectPrototype)
	ctor := constructor(cx, "Object", 1, engine.IntrinsicObjectPrototype, nativeObjectConstructor)
	cx.SetIntrinsic(engine.IntrinsicObject, ctor)

	method(cx, proto, "hasOwnProperty", 1, nativeObjectHasOwnProperty)
	method(cx, proto, "isPrototypeOf", 1, nativeObjectIsPrototypeOf)
	method(cx, proto, "propertyIsEnumerable", 1, nativeObjectPropertyIsEnumerable)
	method(cx, proto, "toString", 0, nativeObjectToStringMethod)
	method(cx, proto, "toLocaleString", 0, nativeObjectToLocaleString)
	method(cx, proto, "valueOf", 0, nativeObjectValueOf)
	protoKey := cx.Intern("__proto__")
	cx.DefineAccessor(proto, protoKey,
		cx.NewNative("get __proto__", 0, nativeObjectProtoGetter),
		cx.NewNative("set __proto__", 1, nativeObjectProtoSetter),
		engine.FlagConfigurable)

	method(cx, proto, "__defineGetter__", 2, nativeObjectDefineGetter)
	method(cx, proto, "__defineSetter__", 2, nativeObjectDefineSetter)
	method(cx, proto, "__lookupGetter__", 1, nativeObjectLookupGetter)
	method(cx, proto, "__lookupSetter__", 1, nativeObjectLookupSetter)

	method(cx, ctor, "getPrototypeOf", 1, nativeObjectGetPrototypeOf)
	method(cx, ctor, "setPrototypeOf", 2, nativeObjectSetPrototypeOf)
	method(cx, ctor, "create", 2, nativeObjectCreate)
	method(cx, ctor, "defineProperty", 3, nativeObjectDefineProperty)
	method(cx, ctor, "defineProperties", 2, nativeObjectDefinePropertiesMethod)
	method(cx, ctor, "getOwnPropertyDescriptor", 2, nativeObjectGetOwnPropertyDescriptor)
	method(cx, ctor, "getOwnPropertyDescriptors", 1, nativeObjectGetOwnPropertyDescriptors)
	method(cx, ctor, "getOwnPropertyNames", 1, nativeObjectGetOwnPropertyNames)
	method(cx, ctor, "getOwnPropertySymbols", 1, nativeObjectGetOwnPropertySymbols)
	method(cx, ctor, "keys", 1, nativeObjectKeys)
	method(cx, ctor, "values", 1, nativeObjectValues)
	method(cx, ctor, "entries", 1, nativeObjectEntries)
	method(cx, ctor, "assign", 2, nativeObjectAssign)
	method(cx, ctor, "freeze", 1, nativeObjectFreeze)
	method(cx, ctor, "seal", 1, nativeObjectSeal)
	method(cx, ctor, "preventExtensions", 1, nativeObjectPreventExtensions)
	method(cx, ctor, "isFrozen", 1, nativeObjectIsFrozen)
	method(cx, ctor, "isSealed", 1, nativeObjectIsSealed)
	method(cx, ctor, "isExtensible", 1, nativeObjectIsExtensible)
	method(cx, ctor, "is", 2, nativeObjectIs)
	method(cx, ctor, "fromEntries", 1, nativeObjectFromEntries)
	method(cx, ctor, "hasOwn", 2, nativeObjectHasOwn)
}

func objectConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if nt := cx.GetNewTarget(); nt != nil && nt != cx.Intrinsic(engine.IntrinsicObject) {
		return engine.ObjectValue(cx.NewInstance(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicObjectPrototype)))
	}
	v := arg(args, 0)
	if v.IsNullish() {
		return engine.ObjectValue(cx.NewOrdinaryObject())
	}
	return engine.ObjectValue(cx.ToObject(v))
}

func objectHasOwnProperty(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	k := cx.QueryKey(arg(args, 0))
	return engine.Bool(cx.QueryOwnProperty(cx.ToObject(this), k))
}

func objectHasOwnPropertyKernel(q engine.QuietContext, this, k engine.Value) (engine.Value, bool) {
	if !this.IsObject() {
		return engine.Undefined, false
	}
	has, ok := q.HasOwnPropertyPlain(this.Object(), k)
	return engine.Bool(has), ok
}

func objectIsPrototypeOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func objectPropertyIsEnumerable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	k := cx.ToPropertyKey(arg(args, 0))
	d, ok := cx.GetOwnProperty(cx.ToObject(this), k)
	return engine.Bool(ok && d.Enumerable())
}

func objectToStringMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	switch {
	case this.IsUndefined():
		return cx.ObjectTagString("Undefined")
	case this.IsNull():
		return cx.ObjectTagString("Null")
	}
	return objectPrototypeToString(cx, this)
}

func objectToLocaleString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.CallValue(cx.GetV(this, cx.Key(engine.KeyToString)), this)
}

func objectValueOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.ObjectValue(cx.ToObject(this))
}

func objectProtoGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if p := cx.ProtoOf(cx.ToObject(this)); p != nil {
		return engine.ObjectValue(p)
	}
	return engine.Null
}

func objectProtoSetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func objectDefineGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return objectDefineAccessor(cx, this, args, "__defineGetter__", false)
}

func objectDefineSetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return objectDefineAccessor(cx, this, args, "__defineSetter__", true)
}

// objectDefineAccessor implements the method name of Object.prototype, which defines a setter
// with setter and a getter otherwise.
func objectDefineAccessor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value, name string, setter bool) engine.Value {
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
}

func objectLookupGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return objectLookupAccessor(cx, this, args, false)
}

func objectLookupSetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return objectLookupAccessor(cx, this, args, true)
}

// objectLookupAccessor implements the methods of Object.prototype that return the setter of a
// property with setter and its getter otherwise.
func objectLookupAccessor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value, setter bool) engine.Value {
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
}

func objectGetPrototypeOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if p := cx.ProtoOf(cx.ToObject(arg(args, 0))); p != nil {
		return engine.ObjectValue(p)
	}
	return engine.Null
}

func objectSetPrototypeOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func objectCreate(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
		objectDefineProperties(cx, o, props)
	}
	return engine.ObjectValue(o)
}

func objectDefineProperty(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := arg(args, 0)
	if !o.IsObject() {
		cx.ThrowTypeError("Object.defineProperty called on non-object")
	}
	k := cx.ToPropertyKey(arg(args, 1))
	cx.DefinePropertyOrThrow(o.Object(), k, cx.ToPropertyDescriptor(arg(args, 2)))
	return o
}

func objectDefinePropertiesMethod(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := arg(args, 0)
	if !o.IsObject() {
		cx.ThrowTypeError("Object.defineProperties called on non-object")
	}
	objectDefineProperties(cx, o.Object(), arg(args, 1))
	return o
}

func objectGetOwnPropertyDescriptor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := cx.ToObject(arg(args, 0))
	d, ok := cx.GetOwnProperty(o, cx.ToPropertyKey(arg(args, 1)))
	if !ok {
		return engine.Undefined
	}
	return cx.FromPropertyDescriptor(d)
}

func objectGetOwnPropertyDescriptors(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func objectGetOwnPropertyNames(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	var out []engine.Value
	var pacer engine.Pacer
	for _, k := range cx.OwnKeys(cx.ToObject(arg(args, 0))) {
		pacer.Step(cx)
		if !k.IsSymbol() {
			out = append(out, cx.KeyValue(k))
		}
	}
	return engine.ObjectValue(cx.NewArray(out))
}

func objectGetOwnPropertySymbols(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	var out []engine.Value
	var pacer engine.Pacer
	for _, k := range cx.OwnKeys(cx.ToObject(arg(args, 0))) {
		pacer.Step(cx)
		if k.IsSymbol() {
			out = append(out, cx.KeyValue(k))
		}
	}
	return engine.ObjectValue(cx.NewArray(out))
}

func objectKeys(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	ks := cx.EnumerableOwnKeys(cx.ToObject(arg(args, 0)), nil)
	out := make([]engine.Value, len(ks))
	var pacer engine.Pacer
	for i, k := range ks {
		pacer.Step(cx)
		out[i] = cx.KeyValue(k)
	}
	return engine.ObjectValue(cx.NewArray(out))
}

func objectValues(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.ObjectValue(cx.NewArray(enumerableOwn(cx, cx.ToObject(arg(args, 0)), false)))
}

func objectEntries(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.ObjectValue(cx.NewArray(enumerableOwn(cx, cx.ToObject(arg(args, 0)), true)))
}

func objectAssign(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	to := cx.ToObject(arg(args, 0))
	for _, src := range args[min(1, len(args)):] {
		if src.IsNullish() {
			continue
		}
		from := cx.ToObject(src)
		if cx.Quiet().AssignDataProperties(to, from) {
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
}

func objectFreeze(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if o := arg(args, 0); o.IsObject() {
		setIntegrity(cx, o.Object(), true)
	}
	return arg(args, 0)
}

func objectSeal(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if o := arg(args, 0); o.IsObject() {
		setIntegrity(cx, o.Object(), false)
	}
	return arg(args, 0)
}

func objectPreventExtensions(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if o := arg(args, 0); o.IsObject() {
		if !cx.PreventExtensions(o.Object()) {
			cx.ThrowTypeError("cannot prevent extensions")
		}
	}
	return arg(args, 0)
}

func objectIsFrozen(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := arg(args, 0)
	return engine.Bool(!o.IsObject() || testIntegrity(cx, o.Object(), true))
}

func objectIsSealed(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := arg(args, 0)
	return engine.Bool(!o.IsObject() || testIntegrity(cx, o.Object(), false))
}

func objectIsExtensible(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := arg(args, 0)
	return engine.Bool(o.IsObject() && cx.IsExtensible(o.Object()))
}

func objectIs(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(engine.SameValue(arg(args, 0), arg(args, 1)))
}

func objectFromEntries(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

func objectHasOwn(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := cx.ToObject(arg(args, 0))
	return engine.Bool(cx.QueryOwnProperty(o, cx.QueryKey(arg(args, 1))))
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

var (
	nativeFunctionConstructor     = engine.RegisterNative(functionConstructor)
	nativeFunctionCall            = engine.RegisterNative(functionCall)
	nativeFunctionApply           = engine.RegisterNative(functionApply)
	nativeFunctionBind            = engine.RegisterNative(functionBind)
	nativeFunctionToString        = engine.RegisterNative(functionToString)
	nativeFunctionHasInstance     = engine.RegisterNative(functionHasInstance)
	nativeFunctionCallerGetter    = engine.RegisterNative(functionCallerGetter)
	nativeFunctionArgumentsGetter = engine.RegisterNative(functionArgumentsGetter)
)

func initFunction(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicFunctionPrototype)
	ctor := constructor(cx, "Function", 1, engine.IntrinsicFunctionPrototype, nativeFunctionConstructor)
	cx.SetIntrinsic(engine.IntrinsicFunction, ctor)

	call := method(cx, proto, "call", 1, nativeFunctionCall)
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeCall, call)
	// The interpreter calls the targets of call and apply directly where it can, so every call
	// of them is invisible to stack traces and to the caller of the functions they call.
	cx.SetFrameless(call)
	apply := method(cx, proto, "apply", 2, nativeFunctionApply)
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeApply, apply)
	cx.SetFrameless(apply)
	method(cx, proto, "bind", 1, nativeFunctionBind)
	method(cx, proto, "toString", 0, nativeFunctionToString)
	hasInstance := cx.NewNative("[Symbol.hasInstance]", 1, nativeFunctionHasInstance)
	cx.SetIntrinsic(engine.IntrinsicFunctionPrototypeHasInstance, hasInstance)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolHasInstance), engine.ObjectValue(hasInstance), 0)
	// Function.prototype[Symbol.metadata] of the decorator metadata proposal.
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolMetadata), engine.Null, 0)

	// AddRestrictedFunctionProperties
	throwTypeError := cx.Intrinsic(engine.IntrinsicThrowTypeError)
	cx.DefineAccessor(proto, cx.Intern("caller"), throwTypeError, throwTypeError, engine.FlagConfigurable)
	cx.DefineAccessor(proto, cx.Intern("arguments"), throwTypeError, throwTypeError, engine.FlagConfigurable)

	// The getters of the own caller and arguments properties of non-strict functions.
	cx.SetIntrinsic(engine.IntrinsicFunctionCallerGetter, cx.NewNative("get caller", 0, nativeFunctionCallerGetter))
	cx.SetIntrinsic(engine.IntrinsicFunctionArgumentsGetter, cx.NewNative("get arguments", 0, nativeFunctionArgumentsGetter))
}

func functionConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.CreateDynamicFunction(engine.NormalFunction, args)
}

func functionCall(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsCallable() {
		cx.ThrowTypeError("Function.prototype.call called on a value that is not a function")
	}
	return cx.Call(this.Object(), arg(args, 0), args[min(1, len(args)):]...)
}

func functionApply(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsCallable() {
		cx.ThrowTypeError("Function.prototype.apply called on a value that is not a function")
	}
	return cx.Call(this.Object(), arg(args, 0), listFromArrayLike(cx, arg(args, 1))...)
}

func functionBind(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsCallable() {
		cx.ThrowTypeError("bind must be called on a function")
	}
	target := this.Object()
	b := cx.BoundFunctionCreate(target, arg(args, 0), args[min(1, len(args)):])
	copyNameAndLength(cx, b, target, "bound ", max(0, len(args)-1))
	return engine.ObjectValue(b)
}

func functionToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsCallable() {
		cx.ThrowTypeError("Function.prototype.toString requires that 'this' be a Function")
	}
	src := this.Object().SourceText()
	checkStringLength(cx, len(src))
	return cx.NewString(src)
}

func functionHasInstance(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(cx.OrdinaryHasInstance(this, arg(args, 0)))
}

func functionCallerGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.FunctionCaller(this)
}

func functionArgumentsGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.FunctionArguments(this)
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

var (
	nativeErrorIsError     = engine.RegisterNative(errorIsError)
	nativeErrorStackGetter = engine.RegisterNative(errorStackGetter)
	nativeErrorStackSetter = engine.RegisterNative(errorStackSetter)
	nativeErrorToString    = engine.RegisterNative(errorToString)
)

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
		ctor := constructor(cx, wtf8.String(name), length, protoID, errorConstructors[kind])
		cx.Define(proto, cx.Key(engine.KeyName), cx.SharedString(wtf8.String(name)), engine.FlagsHidden)
		cx.Define(proto, cx.Key(engine.KeyMessage), cx.NewString(""), engine.FlagsHidden)
		if kind == engine.ErrorKindError {
			base = ctor
		} else {
			cx.SetPrototypeOf(ctor, base)
		}
	}
	method(cx, base, "isError", 1, nativeErrorIsError)
	cx.DefineAccessor(cx.Intrinsic(engine.IntrinsicErrorPrototype), cx.Key(engine.KeyStack),
		cx.NewNative("get stack", 0, nativeErrorStackGetter),
		cx.NewNative("set stack", 1, nativeErrorStackSetter),
		engine.FlagConfigurable)
	method(cx, cx.Intrinsic(engine.IntrinsicErrorPrototype), "toString", 0, nativeErrorToString)
}

var errorConstructors = [engine.ErrorKindCount]engine.Native{
	engine.ErrorKindError:      engine.RegisterNative(errorConstructorNative(engine.ErrorKindError)),
	engine.ErrorKindType:       engine.RegisterNative(errorConstructorNative(engine.ErrorKindType)),
	engine.ErrorKindRange:      engine.RegisterNative(errorConstructorNative(engine.ErrorKindRange)),
	engine.ErrorKindReference:  engine.RegisterNative(errorConstructorNative(engine.ErrorKindReference)),
	engine.ErrorKindSyntax:     engine.RegisterNative(errorConstructorNative(engine.ErrorKindSyntax)),
	engine.ErrorKindEval:       engine.RegisterNative(errorConstructorNative(engine.ErrorKindEval)),
	engine.ErrorKindURI:        engine.RegisterNative(errorConstructorNative(engine.ErrorKindURI)),
	engine.ErrorKindAggregate:  engine.RegisterNative(errorConstructorNative(engine.ErrorKindAggregate)),
	engine.ErrorKindSuppressed: engine.RegisterNative(errorConstructorNative(engine.ErrorKindSuppressed)),
}

// errorConstructorNative returns the constructor of the errors of kind.
//
//go:noinline
func errorConstructorNative(kind engine.ErrorKind) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := cx.GetNewTarget()
		e := cx.NewErrorObject(cx.GetPrototypeFromConstructor(nt, engine.ErrorPrototypeIntrinsic(kind)))
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
	}
}

func errorIsError(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Bool(engine.IsError(arg(args, 0)))
}

func errorStackGetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("Error.prototype.stack getter called on non-object")
	}
	if s, ok := engine.ErrorStack(this); ok {
		return cx.NewString(s)
	}
	return engine.Undefined
}

func errorStackSetter(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if !this.IsObject() {
		cx.ThrowTypeError("Error.prototype.stack setter called on non-object")
	}
	v := arg(args, 0)
	if !v.IsString() {
		cx.ThrowTypeError("Error.prototype.stack setter requires a string")
	}
	setterThatIgnoresPrototypeProperties(cx, this, cx.Intrinsic(engine.IntrinsicErrorPrototype), engine.NamedKey(cx.Key(engine.KeyStack)), v, "Error.prototype.stack setter")
	return engine.Undefined
}

func errorToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
}

var (
	nativeGlobalParseFloat = engine.RegisterNative(globalParseFloat)
	nativeGlobalParseInt   = engine.RegisterNative(globalParseInt)
)

// newParseFunctions creates the functions parseFloat and parseInt.
func newParseFunctions(cx *engine.ExecutionContext) (parseFloat, parseInt *engine.Object) {
	parseFloat = cx.NewNative("parseFloat", 1, nativeGlobalParseFloat)
	parseInt = cx.NewNative("parseInt", 2, nativeGlobalParseInt)
	return parseFloat, parseInt
}

func globalParseFloat(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(numconv.ParseFloat(cx.ToString(arg(args, 0)), cx.Poll))
}

func globalParseInt(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := cx.ToString(arg(args, 0))
	return engine.Number(numconv.ParseInt(s, int(cx.ToInt32(arg(args, 1))), cx.Poll))
}

var (
	nativeGlobalEval     = engine.RegisterNative(globalEval)
	nativeGlobalIsNaN    = engine.RegisterNative(globalIsNaN)
	nativeGlobalIsFinite = engine.RegisterNative(globalIsFinite)
)

// initGlobalFunctions defines the function properties of the global object.
// parseFloat and parseInt are the functions to define under those names.
func initGlobalFunctions(cx *engine.ExecutionContext, parseFloat, parseInt *engine.Object) {
	g := cx.GlobalObject().Object()
	eval := method(cx, g, "eval", 1, nativeGlobalEval)
	cx.SetIntrinsic(engine.IntrinsicEval, eval)
	method(cx, g, "isNaN", 1, nativeGlobalIsNaN)
	method(cx, g, "isFinite", 1, nativeGlobalIsFinite)
	cx.Define(g, cx.Intern("parseFloat"), engine.ObjectValue(parseFloat), engine.FlagsHidden)
	cx.Define(g, cx.Intern("parseInt"), engine.ObjectValue(parseInt), engine.FlagsHidden)
	initURIFunctions(cx)
}

func globalEval(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.PerformEval(arg(args, 0))
}

func globalIsNaN(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	n := cx.ToNumber(arg(args, 0))
	return engine.Bool(n != n)
}

func globalIsFinite(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	n := cx.ToNumber(arg(args, 0))
	return engine.Bool(n == n && !math.IsInf(n, 0))
}
