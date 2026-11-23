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
func (vm *VM) CanAccessReceiver(receiver Value) bool {
	if receiver.IsNullish() {
		receiver = ObjectValue(vm.curRealm.globalThis)
	}
	if !receiver.IsObject() || receiver.o.class != classProxy {
		return true
	}
	st := receiver.o.extState().(*proxyState)
	return !st.isGlobal() || st.allowsGlobalAccess(vm.curRealm.globalProxyHandler)
}

// ProxyCreate implements ProxyCreate.
func (vm *VM) ProxyCreate(target, handler Value) *Object {
	if !target.IsObject() || !handler.IsObject() {
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot create proxy with a non-object as target or handler")
	}
	p := (&propertyObject{
		kind:  kindObject,
		class: classProxy,
		flags: objExotic,
		shape: vm.curRealm.shapes.proxy,
		ext: &proxyState{
			target:  target.o,
			handler: handler.o,
		},
	}).init()
	if target.o.fn != nil {
		// The native function is the [[Call]] and [[Construct]] of the proxy, so that every way
		// of calling a function reaches them.
		p.fn = &function{
			native: func(vm *VM, this Value, args []Value) Value {
				if nt := vm.GetNewTarget(); nt != nil {
					return Value{o: vm.proxyConstruct(p, args, nt)}
				}
				return vm.proxyCall(p, this, args)
			},
			materialized: true,
			constructor:  vm.IsConstructor(target.o),
		}
	}
	return p
}

// InstallGlobalProxy makes a proxy the global this value of the current realm.
// A previous realm supplies its proxy after detachment. If shareAccess is true,
// the realm joins the previous realm's group after detachment, or the entered
// realm's group when previous is nil.
// Property operations on globals outside the group use foreignView.
func (vm *VM) InstallGlobalProxy(foreignView *Object, previous *Realm, shareAccess bool) Value {
	var proxy, handler *Object
	if previous != nil {
		vm.InRealm(previous, func() {
			vm.DetachGlobalProxy()
			proxy = vm.curRealm.globalThis
			if shareAccess {
				handler = vm.curRealm.globalProxyHandler
			}
		})
	} else if shareAccess && vm.enteredRealm != nil {
		handler = vm.enteredRealm.globalProxyHandler
	}
	if handler == nil {
		handler = vm.defaultGlobalHandler()
	}
	vm.trackJobRealms = true
	vm.curRealm.globalProxyHandler = handler
	vm.curRealm.foreignGlobalView = foreignView
	if proxy == nil {
		proxy = vm.ProxyCreate(ObjectValue(vm.curRealm.global), ObjectValue(handler))
		// A global proxy can outlive its first realm and acquire a new target on navigation.
		proxy.shape = vm.newShape(nil, nil)
	} else {
		state := proxy.extState().(*proxyState)
		state.target = vm.curRealm.global
		state.handler = handler
	}
	vm.curRealm.globalThis = proxy
	vm.Define(vm.curRealm.global, vm.Intern("globalThis"), ObjectValue(proxy), FlagsHidden)
	return ObjectValue(proxy)
}

func (vm *VM) defaultGlobalHandler() *Object {
	if vm.curRealm.defaultGlobalProxyHandler == nil {
		h := vm.NewObject(nil)
		h.class = classGlobalProxyHandler
		vm.curRealm.defaultGlobalProxyHandler = h
	}
	return vm.curRealm.defaultGlobalProxyHandler
}

// DetachGlobalProxy disconnects the current realm's installed global proxy.
func (vm *VM) DetachGlobalProxy() {
	st := vm.curRealm.globalThis.extState().(*proxyState)
	if st.target != vm.curRealm.global {
		return
	}
	st.target = nil
	vm.curRealm.globalProxyHandler = vm.defaultGlobalHandler()
}

func (vm *VM) hasDetachedGlobal(r *Realm) bool {
	return r != nil && r.globalProxyHandler != nil && r.globalThis.extState().(*proxyState).target != r.global
}

// RevokeProxy revokes the proxy p: every internal method of p throws a TypeError from then on.
func RevokeProxy(p *Object) {
	st := p.extState().(*proxyState)
	st.target = nil
	st.handler = nil
}

// IsArray implements IsArray.
func (vm *VM) IsArray(v Value) bool {
	o := v.o
	for o != nil && o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.handler == nil {
			vm.throwRevoked(vm.CurrentRealm(), "IsArray")
		}
		o = st.target
	}
	return o != nil && o.kind == kindArray
}

// IsArrayPlain is IsArray of v when v is not a proxy. It reports false as ok for a proxy.
func IsArrayPlain(v Value) (is, ok bool) {
	o := v.o
	if o != nil && o.class == classProxy {
		return false, false
	}
	return o != nil && o.kind == kindArray, true
}

// GetFunctionRealm implements GetFunctionRealm.
func (vm *VM) GetFunctionRealm(f *Object) *Realm {
	if r := vm.functionRealmOrNil(f); r != nil {
		return r
	}
	vm.throwRevoked(vm.CurrentRealm(), "GetFunctionRealm")
	return nil
}

// functionRealmOrNil returns the function's realm, or nil for a revoked proxy.
func (vm *VM) functionRealmOrNil(f *Object) *Realm {
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
			return f.fn.code.context.realm
		case f.fn != nil && f.fn.info != nil:
			return f.fn.info.realm
		default:
			return vm.curRealm
		}
	}
}

func (vm *VM) throwRevoked(errorRealm *Realm, op string) {
	vm.ThrowTypeError(errorRealm, "cannot perform '"+op+"' on a proxy that has been revoked")
}

// proxyEnter starts an internal method of the proxy o and returns its target and handler.
// A global proxy outside the accessing realm's group uses its foreign view.
// The caller has to call proxyLeave when the method ends.
//
// The internal methods of a proxy whose target is a proxy call each other without running
// any JavaScript, so they count as nested interpreter loops and poll for interruptions.
func (vm *VM) proxyEnter(o *Object, op string) (target, handler *Object) {
	st := o.extState().(*proxyState)
	if st.handler == nil {
		vm.throwRevoked(vm.CurrentRealm(), op)
	}
	if vm.interrupt.Load() {
		vm.Poll()
	}
	if vm.depth >= maxDepth {
		vm.ThrowRangeError(vm.CurrentRealm(), "maximum call stack size exceeded")
	}
	target = st.target
	if st.isGlobal() && !st.allowsGlobalAccess(vm.curRealm.globalProxyHandler) {
		target = vm.curRealm.foreignGlobalView
		if target == nil {
			vm.ThrowTypeError(vm.CurrentRealm(), "no access")
		}
	}
	vm.depth++
	return target, st.handler
}

func (vm *VM) proxyLeave() {
	vm.depth--
}

// proxyTrap returns the trap key of handler, or nil when the operation forwards to the target.
func (vm *VM) proxyTrap(handler *Object, key CommonKey) *Object {
	return vm.GetMethod(Value{o: handler}, vm.Key(key))
}

// callTrap calls trap with handler as this.
func (vm *VM) callTrap(trap, handler *Object, args ...Value) Value {
	return vm.callOnStack(trap, Value{o: handler}, args)
}

func (vm *VM) throwTrapError(errorRealm *Realm, trap, msg string) {
	vm.ThrowTypeError(errorRealm, "'"+trap+"' on proxy: "+msg)
}

// proxyGetPrototypeOf implements [[GetPrototypeOf]] of a proxy.
func (vm *VM) proxyGetPrototypeOf(o *Object) *Object {
	target, handler := vm.proxyEnter(o, "getPrototypeOf")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyGetPrototypeOf)
	if trap == nil {
		return vm.ProtoOf(target)
	}
	r := vm.callTrap(trap, handler, Value{o: target})
	if !r.IsObject() && !r.IsNull() {
		vm.throwTrapError(vm.CurrentRealm(), "getPrototypeOf", "trap returned neither object nor null")
	}
	var proto *Object
	if r.IsObject() {
		proto = r.o
	}
	if vm.IsExtensible(target) {
		return proto
	}
	if vm.ProtoOf(target) != proto {
		vm.throwTrapError(vm.CurrentRealm(), "getPrototypeOf", "proxy target is non-extensible but the trap did not return its actual prototype")
	}
	return proto
}

// proxySetPrototypeOf implements [[SetPrototypeOf]] of a proxy.
func (vm *VM) proxySetPrototypeOf(o *Object, proto *Object) bool {
	target, handler := vm.proxyEnter(o, "setPrototypeOf")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keySetPrototypeOf)
	if trap == nil {
		return vm.SetPrototypeOf(target, proto)
	}
	v := Null
	if proto != nil {
		v = Value{o: proto}
	}
	if !ToBoolean(vm.callTrap(trap, handler, Value{o: target}, v)) {
		return false
	}
	if vm.IsExtensible(target) {
		return true
	}
	if vm.ProtoOf(target) != proto {
		vm.throwTrapError(vm.CurrentRealm(), "setPrototypeOf", "trap returned truish for setting a new prototype on the non-extensible proxy target")
	}
	return true
}

// proxyIsExtensible implements [[IsExtensible]] of a proxy.
func (vm *VM) proxyIsExtensible(o *Object) bool {
	target, handler := vm.proxyEnter(o, "isExtensible")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyIsExtensible)
	if trap == nil {
		return vm.IsExtensible(target)
	}
	r := ToBoolean(vm.callTrap(trap, handler, Value{o: target}))
	if r != vm.IsExtensible(target) {
		vm.throwTrapError(vm.CurrentRealm(), "isExtensible", "trap result does not reflect extensibility of proxy target")
	}
	return r
}

// proxyPreventExtensions implements [[PreventExtensions]] of a proxy.
func (vm *VM) proxyPreventExtensions(o *Object) bool {
	target, handler := vm.proxyEnter(o, "preventExtensions")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyPreventExtensions)
	if trap == nil {
		return vm.PreventExtensions(target)
	}
	r := ToBoolean(vm.callTrap(trap, handler, Value{o: target}))
	if r && vm.IsExtensible(target) {
		vm.throwTrapError(vm.CurrentRealm(), "preventExtensions", "trap returned truish but the proxy target is extensible")
	}
	return r
}

// proxyGetOwnProperty implements [[GetOwnProperty]] of a proxy.
func (vm *VM) proxyGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	k = vm.storedKey(k)
	target, handler := vm.proxyEnter(o, "getOwnPropertyDescriptor")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyGetOwnPropertyDescriptor)
	if trap == nil {
		return vm.GetOwnProperty(target, k)
	}
	const name = "getOwnPropertyDescriptor"
	r := vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k))
	if !r.IsObject() && !r.IsUndefined() {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned neither object nor undefined for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"'")
	}
	targetDesc, targetHas := vm.GetOwnProperty(target, k)
	if r.IsUndefined() {
		if !targetHas {
			return PropDesc{}, false
		}
		if targetDesc.flags&FlagConfigurable == 0 {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap returned undefined for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is non-configurable in the proxy target")
		}
		if !vm.IsExtensible(target) {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap returned undefined for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which exists in the non-extensible proxy target")
		}
		return PropDesc{}, false
	}
	extensible := vm.IsExtensible(target)
	d := vm.ToPropertyDescriptor(r)
	d.complete()
	if !isCompatiblePropertyDescriptor(extensible, &d, &targetDesc, targetHas) {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned descriptor for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if d.flags&FlagConfigurable == 0 {
		if !targetHas || targetDesc.flags&FlagConfigurable != 0 {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap reported non-configurability for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is either non-existent or configurable in the proxy target")
		}
		if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 && targetDesc.flags&FlagWritable != 0 {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap reported non-configurable and non-writable for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is non-configurable, writable in the proxy target")
		}
	}
	return d, true
}

// proxyDefineOwnProperty implements [[DefineOwnProperty]] of a proxy.
func (vm *VM) proxyDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	target, handler := vm.proxyEnter(o, "defineProperty")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyDefineProperty)
	if trap == nil {
		return vm.defineOwnProperty(target, k, d)
	}
	const name = "defineProperty"
	if !ToBoolean(vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k), vm.FromPropertyDescriptor(d))) {
		return false
	}
	targetDesc, targetHas := vm.GetOwnProperty(target, k)
	extensible := vm.IsExtensible(target)
	settingConfigFalse := d.has&HasConfigurable != 0 && d.flags&FlagConfigurable == 0
	if !targetHas {
		if !extensible {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' to the non-extensible proxy target")
		}
		if settingConfigFalse {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is non-existent in the proxy target")
		}
		return true
	}
	if !isCompatiblePropertyDescriptor(extensible, &d, &targetDesc, true) {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if settingConfigFalse && targetDesc.flags&FlagConfigurable != 0 {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is configurable in the proxy target")
	}
	if targetDesc.flags&(FlagAccessor|FlagConfigurable|FlagWritable) == FlagWritable && d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which cannot be non-writable, unless there exists a corresponding non-configurable, non-writable own property of the target object")
	}
	return true
}

// proxyHas implements [[HasProperty]] of a proxy.
func (vm *VM) proxyHas(o *Object, k PropKey) bool {
	k = vm.storedKey(k)
	target, handler := vm.proxyEnter(o, "has")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyHas)
	if trap == nil {
		return vm.HasProperty(target, k)
	}
	if ToBoolean(vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k))) {
		return true
	}
	if targetDesc, ok := vm.GetOwnProperty(target, k); ok {
		if targetDesc.flags&FlagConfigurable == 0 {
			vm.throwTrapError(vm.CurrentRealm(), "has", "trap returned falsish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which exists in the proxy target as non-configurable")
		}
		if !vm.IsExtensible(target) {
			vm.throwTrapError(vm.CurrentRealm(), "has", "trap returned falsish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' but the proxy target is not extensible")
		}
	}
	return false
}

// proxyGet implements [[Get]] of a proxy.
func (vm *VM) proxyGet(o *Object, k PropKey, receiver Value) Value {
	k = vm.storedKey(k)
	target, handler := vm.proxyEnter(o, "get")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, KeyGet)
	if trap == nil {
		return vm.getProp(target, k, receiver)
	}
	r := vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k), receiver)
	if targetDesc, ok := vm.GetOwnProperty(target, k); ok && targetDesc.flags&FlagConfigurable == 0 {
		if targetDesc.flags&(FlagAccessor|FlagWritable) == 0 && !SameValue(r, targetDesc.value) {
			vm.throwTrapError(vm.CurrentRealm(), "get", "property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' is a read-only and non-configurable data property on the proxy target but the proxy did not return its actual value")
		}
		if targetDesc.flags&FlagAccessor != 0 && targetDesc.getter == nil && !r.IsUndefined() {
			vm.throwTrapError(vm.CurrentRealm(), "get", "property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' is a non-configurable accessor property on the proxy target and does not have a getter function, but the trap did not return 'undefined'")
		}
	}
	return r
}

// proxySet implements [[Set]] of a proxy. It reports false when the store was refused.
func (vm *VM) proxySet(o *Object, k PropKey, val, receiver Value) bool {
	target, handler := vm.proxyEnter(o, "set")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, KeySet)
	if trap == nil {
		return vm.setProp(target, k, val, receiver)
	}
	if !ToBoolean(vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k), val, receiver)) {
		return false
	}
	if targetDesc, ok := vm.GetOwnProperty(target, k); ok && targetDesc.flags&FlagConfigurable == 0 {
		if targetDesc.flags&(FlagAccessor|FlagWritable) == 0 && !SameValue(val, targetDesc.value) {
			vm.throwTrapError(vm.CurrentRealm(), "set", "trap returned truish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable data property with a different value")
		}
		if targetDesc.flags&FlagAccessor != 0 && targetDesc.setter == nil {
			vm.throwTrapError(vm.CurrentRealm(), "set", "trap returned truish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable accessor property without a setter")
		}
	}
	return true
}

// proxyDelete implements [[Delete]] of a proxy. It reports false when the property is not deleted.
func (vm *VM) proxyDelete(o *Object, k PropKey) bool {
	target, handler := vm.proxyEnter(o, "deleteProperty")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyDeleteProperty)
	if trap == nil {
		return vm.DeleteProp(target, k)
	}
	if !ToBoolean(vm.callTrap(trap, handler, Value{o: target}, vm.KeyValue(k))) {
		return false
	}
	targetDesc, ok := vm.GetOwnProperty(target, k)
	if !ok {
		return true
	}
	if targetDesc.flags&FlagConfigurable == 0 {
		vm.throwTrapError(vm.CurrentRealm(), "deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' which is non-configurable in the proxy target")
	}
	if !vm.IsExtensible(target) {
		vm.throwTrapError(vm.CurrentRealm(), "deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' but the proxy target is non-extensible")
	}
	return true
}

// proxyOwnKeys implements [[OwnPropertyKeys]] of a proxy.
func (vm *VM) proxyOwnKeys(o *Object) []PropKey {
	target, handler := vm.proxyEnter(o, "ownKeys")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyOwnKeys)
	if trap == nil {
		return vm.OwnKeys(target)
	}
	const name = "ownKeys"
	keys := vm.propertyKeyList(vm.callTrap(trap, handler, Value{o: target}))
	var p Pacer
	unchecked := make(map[PropKey]struct{}, len(keys))
	for _, k := range keys {
		p.Step(vm)
		if _, dup := unchecked[k]; dup {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap returned duplicate entries")
		}
		unchecked[k] = struct{}{}
	}
	extensible := vm.IsExtensible(target)
	var configurable, nonconfigurable []PropKey
	for _, k := range vm.OwnKeys(target) {
		p.Step(vm)
		if d, ok := vm.GetOwnProperty(target, k); ok && d.flags&FlagConfigurable == 0 {
			nonconfigurable = append(nonconfigurable, k)
		} else {
			configurable = append(configurable, k)
		}
	}
	if extensible && len(nonconfigurable) == 0 {
		return keys
	}
	for _, k := range nonconfigurable {
		p.Step(vm)
		if _, ok := unchecked[k]; !ok {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap result did not include '"+wtf8.ToValidUTF8(vm.KeyString(k))+"'")
		}
		delete(unchecked, k)
	}
	if extensible {
		return keys
	}
	for _, k := range configurable {
		p.Step(vm)
		if _, ok := unchecked[k]; !ok {
			vm.throwTrapError(vm.CurrentRealm(), name, "trap result did not include '"+wtf8.ToValidUTF8(vm.KeyString(k))+"'")
		}
		delete(unchecked, k)
	}
	if len(unchecked) != 0 {
		vm.throwTrapError(vm.CurrentRealm(), name, "trap returned extra keys but proxy target is non-extensible")
	}
	return keys
}

// propertyKeyList implements CreateListFromArrayLike(v, property-key).
func (vm *VM) propertyKeyList(v Value) []PropKey {
	if !v.IsObject() {
		vm.ThrowTypeError(vm.CurrentRealm(), "CreateListFromArrayLike called on non-object")
	}
	n := vm.LengthOfArrayLike(v.o)
	var keys []PropKey
	for i := range n {
		vm.PollAt(i)
		k := PropKey{idx: uint32(i)}
		if i > MaxArrayIndex {
			k = vm.KeyOfString(wtf8.String(strconv.FormatInt(i, 10)))
		}
		e := vm.getProp(v.o, k, v)
		if !e.IsString() && !e.IsSymbol() {
			vm.ThrowTypeError(vm.CurrentRealm(), vm.DisplayString(e)+" is not a valid property name")
		}
		keys = append(keys, vm.ToPropertyKey(e))
	}
	return keys
}

// proxyCall implements [[Call]] of a proxy.
func (vm *VM) proxyCall(o *Object, this Value, args []Value) Value {
	target, handler := vm.proxyEnter(o, "apply")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyApply)
	if trap == nil {
		return vm.Call(target, this, args...)
	}
	argArray := vm.NewArray(append([]Value(nil), args...))
	return vm.callTrap(trap, handler, Value{o: target}, this, Value{o: argArray})
}

// proxyConstruct implements [[Construct]] of a proxy.
func (vm *VM) proxyConstruct(o *Object, args []Value, newTarget *Object) *Object {
	target, handler := vm.proxyEnter(o, "construct")
	defer vm.proxyLeave()
	trap := vm.proxyTrap(handler, keyConstruct)
	if trap == nil {
		return vm.Construct(target, args, newTarget)
	}
	argArray := vm.NewArray(append([]Value(nil), args...))
	r := vm.callTrap(trap, handler, Value{o: target}, Value{o: argArray}, Value{o: newTarget})
	if !r.IsObject() {
		vm.throwTrapError(vm.CurrentRealm(), "construct", "trap returned non-object ('"+vm.DisplayString(r)+"')")
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
func (vm *VM) ToPropertyDescriptor(v Value) PropDesc {
	if !v.IsObject() {
		vm.ThrowTypeError(vm.CurrentRealm(), "property description must be an object: "+vm.DisplayString(v))
	}
	o := v.o
	var d PropDesc
	field := func(key CommonKey) (Value, bool) {
		k := PropKey{key: vm.Key(key)}
		if !vm.HasProperty(o, k) {
			return Undefined, false
		}
		return vm.getProp(o, k, v), true
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
			vm.ThrowTypeError(vm.CurrentRealm(), "getter must be a function: "+vm.DisplayString(x))
		}
		var f *Object
		if x.IsObject() {
			f = x.o
		}
		d.SetGetter(f)
	}
	if x, ok := field(KeySet); ok {
		if !x.IsUndefined() && !x.IsCallable() {
			vm.ThrowTypeError(vm.CurrentRealm(), "setter must be a function: "+vm.DisplayString(x))
		}
		var f *Object
		if x.IsObject() {
			f = x.o
		}
		d.SetSetter(f)
	}
	if d.IsAccessor() && d.IsData() {
		vm.ThrowTypeError(vm.CurrentRealm(), "invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	}
	return d
}

// FromPropertyDescriptor implements FromPropertyDescriptor for a descriptor that is not undefined.
func (vm *VM) FromPropertyDescriptor(d PropDesc) Value {
	o, slots := newObjectWithStorage(bits.OnesCount8(d.has))
	o.kind = kindObject
	o.shape = vm.curRealm.shapes.object
	o.slots = slots[:0]
	fn := func(f *Object) Value {
		if f == nil {
			return Undefined
		}
		return Value{o: f}
	}
	if d.has&HasValue != 0 {
		vm.add(o, vm.Key(KeyValue), d.value, FlagsData)
	}
	if d.has&HasWritable != 0 {
		vm.add(o, vm.Key(KeyWritable), Bool(d.flags&FlagWritable != 0), FlagsData)
	}
	if d.has&HasGet != 0 {
		vm.add(o, vm.Key(KeyGet), fn(d.getter), FlagsData)
	}
	if d.has&HasSet != 0 {
		vm.add(o, vm.Key(KeySet), fn(d.setter), FlagsData)
	}
	if d.has&HasEnumerable != 0 {
		vm.add(o, vm.Key(KeyEnumerable), Bool(d.flags&FlagEnumerable != 0), FlagsData)
	}
	if d.has&HasConfigurable != 0 {
		vm.add(o, vm.Key(KeyConfigurable), Bool(d.flags&FlagConfigurable != 0), FlagsData)
	}
	return Value{o: o}
}
