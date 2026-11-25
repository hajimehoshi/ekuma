// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/bits"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// proxyState holds the internal slots of a proxy object.
// A detached global proxy has a nil target. A revoked proxy has both slots nil.
type proxyState struct {
	target  *Object
	handler *Object
}

func (s *proxyState) isGlobal() bool {
	return s.handler != nil && s.handler.class == classGlobalProxyHandler
}

func (s *proxyState) allowsGlobalAccess(group *Object) bool {
	return s.target != nil && s.handler == group
}

// CanAccessReceiver reports whether a non-strict host call may use receiver in the current realm.
func (cx *ExecutionContext) CanAccessReceiver(receiver Value) bool {
	if receiver.IsNullish() {
		receiver = ObjectValue(cx.realm.globalThis)
	}
	if !receiver.IsObject() || receiver.o.class != classProxy {
		return true
	}
	st := receiver.o.extState().(*proxyState)
	return !st.isGlobal() || st.allowsGlobalAccess(cx.realm.globalProxyHandler)
}

// ProxyCreate implements ProxyCreate.
func (cx *ExecutionContext) ProxyCreate(target, handler Value) *Object {
	if !target.IsObject() || !handler.IsObject() {
		cx.ThrowTypeError("cannot create proxy with a non-object as target or handler")
	}
	p := (&propertyObject{
		kind:  kindObject,
		class: classProxy,
		flags: objExotic,
		shape: cx.realm.shapes.proxy,
		ext: &proxyState{
			target:  target.o,
			handler: handler.o,
		},
	}).init()
	if target.o.fn != nil {
		// The native function is the [[Call]] and [[Construct]] of the proxy, so that every way
		// of calling a function reaches them.
		p.fn = &function{
			info:         &proxyInfo,
			materialized: true,
			constructor:  cx.IsConstructor(target.o),
		}
	}
	return p
}

// InstallGlobalProxy makes a proxy the global this value of the current realm.
// A previous realm supplies its proxy after detachment. If shareAccess is true,
// the realm joins the previous realm's group after detachment, or the entered
// realm's group when previous is nil.
// Property operations on globals outside the group use foreignView.
func (cx *ExecutionContext) InstallGlobalProxy(foreignView *Object, previous *ExecutionContext, shareAccess bool) Value {
	var proxy, handler *Object
	if previous != nil {
		previous.Run(func() {
			previous.DetachGlobalProxy()
			proxy = previous.realm.globalThis
			if shareAccess {
				handler = previous.realm.globalProxyHandler
			}
		})
	} else if shareAccess && cx.enteredRealm != nil {
		handler = cx.enteredRealm.globalProxyHandler
	}
	if handler == nil {
		handler = cx.defaultGlobalHandler()
	}
	cx.trackJobRealms = true
	cx.realm.globalProxyHandler = handler
	cx.realm.foreignGlobalView = foreignView
	if proxy == nil {
		proxy = cx.ProxyCreate(ObjectValue(cx.realm.global), ObjectValue(handler))
		// A global proxy can outlive its first realm and acquire a new target on navigation.
		proxy.shape = cx.rootShape(nil, nil, delegatedRoot)
	} else {
		state := proxy.extState().(*proxyState)
		state.target = cx.realm.global
		state.handler = handler
	}
	cx.realm.globalThis = proxy
	cx.Define(cx.realm.global, cx.Intern("globalThis"), ObjectValue(proxy), FlagsHidden)
	return ObjectValue(proxy)
}

func (cx *ExecutionContext) defaultGlobalHandler() *Object {
	if cx.realm.defaultGlobalProxyHandler == nil {
		h := cx.NewObject(nil)
		h.class = classGlobalProxyHandler
		cx.realm.defaultGlobalProxyHandler = h
	}
	return cx.realm.defaultGlobalProxyHandler
}

// DetachGlobalProxy disconnects the current realm's installed global proxy.
func (cx *ExecutionContext) DetachGlobalProxy() {
	st := cx.realm.globalThis.extState().(*proxyState)
	if st.target != cx.realm.global {
		return
	}
	st.target = nil
	cx.realm.globalProxyHandler = cx.defaultGlobalHandler()
}

func (ag *agent) hasDetachedGlobal(r *realm) bool {
	return r != nil && r.globalProxyHandler != nil && r.globalThis.extState().(*proxyState).target != r.global
}

// RevokeProxy revokes the proxy p: every internal method of p throws a TypeError from then on.
func RevokeProxy(p *Object) {
	st := p.extState().(*proxyState)
	st.target = nil
	st.handler = nil
}

// IsArray implements IsArray.
func (cx *ExecutionContext) IsArray(v Value) bool {
	o := v.o
	for o != nil && o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.handler == nil {
			cx.throwRevoked("IsArray")
		}
		o = st.target
	}
	return o != nil && o.kind == kindArray
}

// IsArrayPlain is IsArray of v when v is not a proxy. It reports false as ok for a proxy.
func (q QuietContext) IsArrayPlain(v Value) (is, ok bool) {
	o := v.o
	if o != nil && o.class == classProxy {
		return false, false
	}
	return o != nil && o.kind == kindArray, true
}

// FunctionContext returns the context of GetFunctionRealm(f).
func (cx *ExecutionContext) FunctionContext(f *Object) *ExecutionContext {
	if r := cx.functionRealmOrNil(f); r != nil {
		return r.executionContext()
	}
	cx.throwRevoked("GetFunctionRealm")
	return nil
}

// functionRealmOrNil returns the function's realm, or nil for a revoked proxy.
func (cx *ExecutionContext) functionRealmOrNil(f *Object) *realmPart {
	for {
		switch {
		case f.fn != nil && f.fn.bound != nil:
			f = f.fn.bound.target
		case f.class == classProxy:
			st := f.extState().(*proxyState)
			if st.handler == nil {
				return nil
			}
			f = st.target
		case f.fn != nil && f.fn.code != nil:
			return f.fn.code.realm
		case f.fn != nil && f.fn.info != nil:
			return f.fn.info.realm
		default:
			return cx.realmPart
		}
	}
}

func (cx *ExecutionContext) throwRevoked(op string) {
	cx.ThrowTypeError("cannot perform '" + op + "' on a proxy that has been revoked")
}

// proxyEnter starts an internal method of the proxy o and returns its target and handler.
// A global proxy outside the accessing realm's group uses its foreign view.
// The caller has to call proxyLeave when the method ends.
//
// The internal methods of a proxy whose target is a proxy call each other without running
// any JavaScript, so they count as nested interpreter loops and poll for interruptions.
func (cx *ExecutionContext) proxyEnter(o *Object, op string) (target, handler *Object) {
	st := o.extState().(*proxyState)
	if st.handler == nil {
		cx.throwRevoked(op)
	}
	if cx.interrupt.Load() {
		cx.Poll()
	}
	if cx.depth >= maxDepth {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	target = st.target
	if st.isGlobal() && !st.allowsGlobalAccess(cx.realm.globalProxyHandler) {
		target = cx.realm.foreignGlobalView
		if target == nil {
			cx.ThrowTypeError("no access")
		}
	}
	cx.depth++
	return target, st.handler
}

func (ag *agent) proxyLeave() {
	ag.depth--
}

// proxyTrap returns the trap key of handler, or nil when the operation forwards to the target.
func (cx *ExecutionContext) proxyTrap(handler *Object, key CommonKey) *Object {
	return cx.GetMethod(Value{o: handler}, cx.Key(key))
}

// callTrap calls trap with handler as this.
func (cx *ExecutionContext) callTrap(trap, handler *Object, args ...Value) Value {
	return cx.callOnStack(trap, Value{o: handler}, args)
}

func (cx *ExecutionContext) throwTrapError(trap, msg string) {
	cx.ThrowTypeError("'" + trap + "' on proxy: " + msg)
}

// proxyGetPrototypeOf implements [[GetPrototypeOf]] of a proxy.
func (cx *ExecutionContext) proxyGetPrototypeOf(o *Object) *Object {
	target, handler := cx.proxyEnter(o, "getPrototypeOf")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyGetPrototypeOf)
	if trap == nil {
		return cx.ProtoOf(target)
	}
	r := cx.callTrap(trap, handler, Value{o: target})
	if !r.IsObject() && !r.IsNull() {
		cx.throwTrapError("getPrototypeOf", "trap returned neither object nor null")
	}
	var proto *Object
	if r.IsObject() {
		proto = r.o
	}
	if cx.IsExtensible(target) {
		return proto
	}
	if cx.ProtoOf(target) != proto {
		cx.throwTrapError("getPrototypeOf", "proxy target is non-extensible but the trap did not return its actual prototype")
	}
	return proto
}

// proxySetPrototypeOf implements [[SetPrototypeOf]] of a proxy.
func (cx *ExecutionContext) proxySetPrototypeOf(o *Object, proto *Object) bool {
	target, handler := cx.proxyEnter(o, "setPrototypeOf")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keySetPrototypeOf)
	if trap == nil {
		return cx.SetPrototypeOf(target, proto)
	}
	v := Null
	if proto != nil {
		v = Value{o: proto}
	}
	if !ToBoolean(cx.callTrap(trap, handler, Value{o: target}, v)) {
		return false
	}
	if cx.IsExtensible(target) {
		return true
	}
	if cx.ProtoOf(target) != proto {
		cx.throwTrapError("setPrototypeOf", "trap returned truish for setting a new prototype on the non-extensible proxy target")
	}
	return true
}

// proxyIsExtensible implements [[IsExtensible]] of a proxy.
func (cx *ExecutionContext) proxyIsExtensible(o *Object) bool {
	target, handler := cx.proxyEnter(o, "isExtensible")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyIsExtensible)
	if trap == nil {
		return cx.IsExtensible(target)
	}
	r := ToBoolean(cx.callTrap(trap, handler, Value{o: target}))
	if r != cx.IsExtensible(target) {
		cx.throwTrapError("isExtensible", "trap result does not reflect extensibility of proxy target")
	}
	return r
}

// proxyPreventExtensions implements [[PreventExtensions]] of a proxy.
func (cx *ExecutionContext) proxyPreventExtensions(o *Object) bool {
	target, handler := cx.proxyEnter(o, "preventExtensions")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyPreventExtensions)
	if trap == nil {
		return cx.PreventExtensions(target)
	}
	r := ToBoolean(cx.callTrap(trap, handler, Value{o: target}))
	if r && cx.IsExtensible(target) {
		cx.throwTrapError("preventExtensions", "trap returned truish but the proxy target is extensible")
	}
	return r
}

// proxyGetOwnProperty implements [[GetOwnProperty]] of a proxy.
func (cx *ExecutionContext) proxyGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	target, handler := cx.proxyEnter(o, "getOwnPropertyDescriptor")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyGetOwnPropertyDescriptor)
	if trap == nil {
		return cx.GetOwnProperty(target, k)
	}
	const name = "getOwnPropertyDescriptor"
	r := cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k))
	if !r.IsObject() && !r.IsUndefined() {
		cx.throwTrapError(name, "trap returned neither object nor undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"'")
	}
	targetDesc, targetHas := cx.GetOwnProperty(target, k)
	if r.IsUndefined() {
		if !targetHas {
			return PropDesc{}, false
		}
		if targetDesc.flags&FlagConfigurable == 0 {
			cx.throwTrapError(name, "trap returned undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable in the proxy target")
		}
		if !cx.IsExtensible(target) {
			cx.throwTrapError(name, "trap returned undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the non-extensible proxy target")
		}
		return PropDesc{}, false
	}
	extensible := cx.IsExtensible(target)
	d := cx.ToPropertyDescriptor(r)
	d.complete()
	if !isCompatiblePropertyDescriptor(extensible, &d, &targetDesc, targetHas) {
		cx.throwTrapError(name, "trap returned descriptor for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if d.flags&FlagConfigurable == 0 {
		if !targetHas || targetDesc.flags&FlagConfigurable != 0 {
			cx.throwTrapError(name, "trap reported non-configurability for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is either non-existent or configurable in the proxy target")
		}
		if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 && targetDesc.flags&FlagWritable != 0 {
			cx.throwTrapError(name, "trap reported non-configurable and non-writable for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable, writable in the proxy target")
		}
	}
	return d, true
}

// proxyDefineOwnProperty implements [[DefineOwnProperty]] of a proxy.
func (cx *ExecutionContext) proxyDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	target, handler := cx.proxyEnter(o, "defineProperty")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyDefineProperty)
	if trap == nil {
		return cx.defineOwnProperty(target, k, d)
	}
	const name = "defineProperty"
	if !ToBoolean(cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k), cx.FromPropertyDescriptor(d))) {
		return false
	}
	targetDesc, targetHas := cx.GetOwnProperty(target, k)
	extensible := cx.IsExtensible(target)
	settingConfigFalse := d.has&HasConfigurable != 0 && d.flags&FlagConfigurable == 0
	if !targetHas {
		if !extensible {
			cx.throwTrapError(name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' to the non-extensible proxy target")
		}
		if settingConfigFalse {
			cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-existent in the proxy target")
		}
		return true
	}
	if !isCompatiblePropertyDescriptor(extensible, &d, &targetDesc, true) {
		cx.throwTrapError(name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if settingConfigFalse && targetDesc.flags&FlagConfigurable != 0 {
		cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is configurable in the proxy target")
	}
	if targetDesc.flags&(FlagAccessor|FlagConfigurable|FlagWritable) == FlagWritable && d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which cannot be non-writable, unless there exists a corresponding non-configurable, non-writable own property of the target object")
	}
	return true
}

// proxyHas implements [[HasProperty]] of a proxy.
func (cx *ExecutionContext) proxyHas(o *Object, k PropKey) bool {
	target, handler := cx.proxyEnter(o, "has")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, KeyHas)
	if trap == nil {
		return cx.HasProperty(target, k)
	}
	if ToBoolean(cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k))) {
		return true
	}
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok {
		if targetDesc.flags&FlagConfigurable == 0 {
			cx.throwTrapError("has", "trap returned falsish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as non-configurable")
		}
		if !cx.IsExtensible(target) {
			cx.throwTrapError("has", "trap returned falsish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' but the proxy target is not extensible")
		}
	}
	return false
}

// proxyGet implements [[Get]] of a proxy.
func (cx *ExecutionContext) proxyGet(o *Object, k PropKey, receiver Value) Value {
	target, handler := cx.proxyEnter(o, "get")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, KeyGet)
	if trap == nil {
		return cx.getProp(target, k, receiver)
	}
	r := cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k), receiver)
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok && targetDesc.flags&FlagConfigurable == 0 {
		if targetDesc.flags&(FlagAccessor|FlagWritable) == 0 && !SameValue(r, targetDesc.value) {
			cx.throwTrapError("get", "property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' is a read-only and non-configurable data property on the proxy target but the proxy did not return its actual value")
		}
		if targetDesc.flags&FlagAccessor != 0 && targetDesc.getter == nil && !r.IsUndefined() {
			cx.throwTrapError("get", "property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' is a non-configurable accessor property on the proxy target and does not have a getter function, but the trap did not return 'undefined'")
		}
	}
	return r
}

// proxySet implements [[Set]] of a proxy. It reports false when the store was refused.
func (cx *ExecutionContext) proxySet(o *Object, k PropKey, val, receiver Value) bool {
	target, handler := cx.proxyEnter(o, "set")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, KeySet)
	if trap == nil {
		return cx.setProp(target, k, val, receiver)
	}
	if !ToBoolean(cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k), val, receiver)) {
		return false
	}
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok && targetDesc.flags&FlagConfigurable == 0 {
		if targetDesc.flags&(FlagAccessor|FlagWritable) == 0 && !SameValue(val, targetDesc.value) {
			cx.throwTrapError("set", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable data property with a different value")
		}
		if targetDesc.flags&FlagAccessor != 0 && targetDesc.setter == nil {
			cx.throwTrapError("set", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable accessor property without a setter")
		}
	}
	return true
}

// proxyDelete implements [[Delete]] of a proxy. It reports false when the property is not deleted.
func (cx *ExecutionContext) proxyDelete(o *Object, k PropKey) bool {
	target, handler := cx.proxyEnter(o, "deleteProperty")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyDeleteProperty)
	if trap == nil {
		return cx.DeleteProp(target, k)
	}
	if !ToBoolean(cx.callTrap(trap, handler, Value{o: target}, cx.KeyValue(k))) {
		return false
	}
	targetDesc, ok := cx.GetOwnProperty(target, k)
	if !ok {
		return true
	}
	if targetDesc.flags&FlagConfigurable == 0 {
		cx.throwTrapError("deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable in the proxy target")
	}
	if !cx.IsExtensible(target) {
		cx.throwTrapError("deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' but the proxy target is non-extensible")
	}
	return true
}

// proxyOwnKeys implements [[OwnPropertyKeys]] of a proxy.
func (cx *ExecutionContext) proxyOwnKeys(o *Object) []PropKey {
	target, handler := cx.proxyEnter(o, "ownKeys")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyOwnKeys)
	if trap == nil {
		return cx.OwnKeys(target)
	}
	const name = "ownKeys"
	keys := cx.propertyKeyList(cx.callTrap(trap, handler, Value{o: target}))
	var p Pacer
	unchecked := make(map[PropKey]struct{}, len(keys))
	for _, k := range keys {
		p.Step(cx)
		if _, dup := unchecked[k]; dup {
			cx.throwTrapError(name, "trap returned duplicate entries")
		}
		unchecked[k] = struct{}{}
	}
	extensible := cx.IsExtensible(target)
	var configurable, nonconfigurable []PropKey
	for _, k := range cx.OwnKeys(target) {
		p.Step(cx)
		if d, ok := cx.GetOwnProperty(target, k); ok && d.flags&FlagConfigurable == 0 {
			nonconfigurable = append(nonconfigurable, k)
		} else {
			configurable = append(configurable, k)
		}
	}
	if extensible && len(nonconfigurable) == 0 {
		return keys
	}
	for _, k := range nonconfigurable {
		p.Step(cx)
		if _, ok := unchecked[k]; !ok {
			cx.throwTrapError(name, "trap result did not include '"+wtf8.ToValidUTF8(cx.KeyString(k))+"'")
		}
		delete(unchecked, k)
	}
	if extensible {
		return keys
	}
	for _, k := range configurable {
		p.Step(cx)
		if _, ok := unchecked[k]; !ok {
			cx.throwTrapError(name, "trap result did not include '"+wtf8.ToValidUTF8(cx.KeyString(k))+"'")
		}
		delete(unchecked, k)
	}
	if len(unchecked) != 0 {
		cx.throwTrapError(name, "trap returned extra keys but proxy target is non-extensible")
	}
	return keys
}

// propertyKeyList implements CreateListFromArrayLike(v, property-key).
func (cx *ExecutionContext) propertyKeyList(v Value) []PropKey {
	if !v.IsObject() {
		cx.ThrowTypeError("CreateListFromArrayLike called on non-object")
	}
	n := cx.LengthOfArrayLike(v.o)
	var keys []PropKey
	for i := range n {
		cx.PollAt(i)
		k := PropKey{idx: uint32(i)}
		if i > MaxArrayIndex {
			k = cx.KeyOfString(wtf8.String(strconv.FormatInt(i, 10)))
		}
		e := cx.getProp(v.o, k, v)
		if !e.IsString() && !e.IsSymbol() {
			cx.ThrowTypeError(cx.DisplayString(e) + " is not a valid property name")
		}
		keys = append(keys, cx.ToPropertyKey(e))
	}
	return keys
}

// proxyFunction is the code of the proxies that can be called.
func proxyFunction(cx *ExecutionContext, this Value, args []Value) Value {
	p := cx.activeNative()
	if nt := cx.GetNewTarget(); nt != nil {
		return Value{o: cx.proxyConstruct(p, args, nt)}
	}
	return cx.proxyCall(p, this, args)
}

// proxyCall implements [[Call]] of a proxy.
func (cx *ExecutionContext) proxyCall(o *Object, this Value, args []Value) Value {
	target, handler := cx.proxyEnter(o, "apply")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyApply)
	if trap == nil {
		return cx.Call(target, this, args...)
	}
	argArray := cx.NewArray(append([]Value(nil), args...))
	return cx.callTrap(trap, handler, Value{o: target}, this, Value{o: argArray})
}

// proxyConstruct implements [[Construct]] of a proxy.
func (cx *ExecutionContext) proxyConstruct(o *Object, args []Value, newTarget *Object) *Object {
	target, handler := cx.proxyEnter(o, "construct")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, keyConstruct)
	if trap == nil {
		return cx.Construct(target, args, newTarget)
	}
	argArray := cx.NewArray(append([]Value(nil), args...))
	r := cx.callTrap(trap, handler, Value{o: target}, Value{o: argArray}, Value{o: newTarget})
	if !r.IsObject() {
		cx.throwTrapError("construct", "trap returned non-object ('"+cx.DisplayString(r)+"')")
	}
	return r.o
}

// isCompatiblePropertyDescriptor implements IsCompatiblePropertyDescriptor. hasCur reports whether
// the property described by cur exists.
func isCompatiblePropertyDescriptor(extensible bool, d, cur *PropDesc, hasCur bool) bool {
	if !hasCur {
		return extensible
	}
	return cur.flags&FlagConfigurable != 0 || nonConfigurableCompatible(cur, d)
}

// nonConfigurableCompatible reports whether ValidateAndApplyPropertyDescriptor accepts d for the
// existing property described by cur, which is not configurable.
func nonConfigurableCompatible(cur, d *PropDesc) bool {
	if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable != 0 {
		return false
	}
	if d.has&HasEnumerable != 0 && (d.flags^cur.flags)&FlagEnumerable != 0 {
		return false
	}
	curAccessor := cur.flags&FlagAccessor != 0
	if d.IsAccessor() != curAccessor && (d.IsAccessor() || d.IsData()) {
		return false
	}
	if curAccessor {
		if d.has&HasGet != 0 && d.getter != cur.getter {
			return false
		}
		if d.has&HasSet != 0 && d.setter != cur.setter {
			return false
		}
	} else if cur.flags&FlagWritable == 0 {
		if d.has&HasWritable != 0 && d.flags&FlagWritable != 0 {
			return false
		}
		if d.has&HasValue != 0 && !SameValue(d.value, cur.value) {
			return false
		}
	}
	return true
}

// complete implements CompletePropertyDescriptor. It also marks an accessor descriptor with
// [FlagAccessor], as the descriptors of existing properties are.
func (d *PropDesc) complete() {
	if d.IsAccessor() {
		d.has |= HasGet | HasSet
		d.flags = d.flags&^FlagWritable | FlagAccessor
	} else {
		if d.has&HasValue == 0 {
			d.value = Undefined
		}
		if d.has&HasWritable == 0 {
			d.flags &^= FlagWritable
		}
		d.has |= HasValue | HasWritable
	}
	if d.has&HasEnumerable == 0 {
		d.flags &^= FlagEnumerable
	}
	if d.has&HasConfigurable == 0 {
		d.flags &^= FlagConfigurable
	}
	d.has |= HasEnumerable | HasConfigurable
}

// ToPropertyDescriptor implements ToPropertyDescriptor.
func (cx *ExecutionContext) ToPropertyDescriptor(v Value) PropDesc {
	if !v.IsObject() {
		cx.ThrowTypeError("property description must be an object: " + cx.DisplayString(v))
	}
	o := v.o
	var d PropDesc
	field := func(key CommonKey) (Value, bool) {
		k := PropKey{key: cx.Key(key)}
		if !cx.HasProperty(o, k) {
			return Undefined, false
		}
		return cx.getProp(o, k, v), true
	}
	if x, ok := field(KeyEnumerable); ok {
		d.SetEnumerable(ToBoolean(x))
	}
	if x, ok := field(KeyConfigurable); ok {
		d.SetConfigurable(ToBoolean(x))
	}
	if x, ok := field(KeyValue); ok {
		d.SetValue(x)
	}
	if x, ok := field(KeyWritable); ok {
		d.SetWritable(ToBoolean(x))
	}
	if x, ok := field(KeyGet); ok {
		if !x.IsUndefined() && !x.IsCallable() {
			cx.ThrowTypeError("getter must be a function: " + cx.DisplayString(x))
		}
		var f *Object
		if x.IsObject() {
			f = x.o
		}
		d.SetGetter(f)
	}
	if x, ok := field(KeySet); ok {
		if !x.IsUndefined() && !x.IsCallable() {
			cx.ThrowTypeError("setter must be a function: " + cx.DisplayString(x))
		}
		var f *Object
		if x.IsObject() {
			f = x.o
		}
		d.SetSetter(f)
	}
	if d.IsAccessor() && d.IsData() {
		cx.ThrowTypeError("invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	}
	return d
}

// FromPropertyDescriptor implements FromPropertyDescriptor for a descriptor that is not undefined.
func (cx *ExecutionContext) FromPropertyDescriptor(d PropDesc) Value {
	o, slots := newObjectWithStorage(bits.OnesCount8(d.has))
	o.kind = kindObject
	o.shape = cx.realm.shapes.object
	o.initSlots(slots[:0])
	fn := func(f *Object) Value {
		if f == nil {
			return Undefined
		}
		return Value{o: f}
	}
	if d.has&HasValue != 0 {
		cx.add(o, cx.Key(KeyValue), d.value, FlagsData)
	}
	if d.has&HasWritable != 0 {
		cx.add(o, cx.Key(KeyWritable), Bool(d.flags&FlagWritable != 0), FlagsData)
	}
	if d.has&HasGet != 0 {
		cx.add(o, cx.Key(KeyGet), fn(d.getter), FlagsData)
	}
	if d.has&HasSet != 0 {
		cx.add(o, cx.Key(KeySet), fn(d.setter), FlagsData)
	}
	if d.has&HasEnumerable != 0 {
		cx.add(o, cx.Key(KeyEnumerable), Bool(d.flags&FlagEnumerable != 0), FlagsData)
	}
	if d.has&HasConfigurable != 0 {
		cx.add(o, cx.Key(KeyConfigurable), Bool(d.flags&FlagConfigurable != 0), FlagsData)
	}
	return Value{o: o}
}
