// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// CanAccessReceiver reports whether a non-strict host call may use receiver in the current realm.
func (cx *ExecutionContext) CanAccessReceiver(receiver storage.Value) bool {
	if receiver.IsNullish() {
		receiver = storage.ObjectValue(cx.realm.GlobalThis)
	}
	if !receiver.IsObject() || receiver.O.Class != storage.ClassProxy {
		return true
	}
	st := receiver.O.ExtState().(*storage.ProxyState)
	return !st.IsGlobal() || st.AllowsGlobalAccess(cx.realm.globalProxyHandler)
}

// ProxyCreate implements ProxyCreate.
func (cx *ExecutionContext) ProxyCreate(target, handler storage.Value) *storage.Object {
	if !target.IsObject() || !handler.IsObject() {
		cx.ThrowTypeError("cannot create proxy with a non-object as target or handler")
	}
	var fn *storage.Function
	if target.O.Fn != nil {
		// The native function is the [[Call]] and [[Construct]] of the proxy, so that every way
		// of calling a function reaches them.
		fn = &storage.Function{
			Info:         &storage.ProxyInfo,
			Materialized: true,
			Constructor:  cx.IsConstructor(target.O),
		}
	}
	return storage.NewProxyObject(cx.realm.Shapes.Proxy, &storage.ProxyState{
		Target:  target.O,
		Handler: handler.O,
	}, fn)
}

// InstallGlobalProxy makes a proxy the global this value of the current realm.
// A previous realm supplies its proxy after detachment. If shareAccess is true,
// the realm joins the previous realm's group after detachment, or the entered
// realm's group when previous is nil.
// Property operations on globals outside the group use foreignView.
func (cx *ExecutionContext) InstallGlobalProxy(foreignView *storage.Object, previous *ExecutionContext, shareAccess bool) storage.Value {
	var proxy, handler *storage.Object
	if previous != nil {
		previous.Run(func() {
			previous.DetachGlobalProxy()
			proxy = previous.realm.GlobalThis
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
		// A global proxy can outlive its first realm and acquire a new target on navigation.
		proxy = storage.NewProxyObject(cx.RootShape(nil, nil, storage.DelegatedRoot), &storage.ProxyState{
			Target:  cx.realm.Global,
			Handler: handler,
		}, nil)
	} else {
		state := proxy.ExtState().(*storage.ProxyState)
		state.Target = cx.realm.Global
		state.Handler = handler
	}
	cx.realm.GlobalThis = proxy
	cx.Define(cx.realm.Global, cx.Intern("globalThis"), storage.ObjectValue(proxy), storage.FlagsHidden)
	return storage.ObjectValue(proxy)
}

func (cx *ExecutionContext) defaultGlobalHandler() *storage.Object {
	if cx.realm.defaultGlobalProxyHandler == nil {
		cx.realm.defaultGlobalProxyHandler = storage.NewObjectWithState(cx.ShapeFor(nil), storage.ClassGlobalProxyHandler, nil)
	}
	return cx.realm.defaultGlobalProxyHandler
}

// DetachGlobalProxy disconnects the current realm's installed global proxy.
func (cx *ExecutionContext) DetachGlobalProxy() {
	st := cx.realm.GlobalThis.ExtState().(*storage.ProxyState)
	if st.Target != cx.realm.Global {
		return
	}
	st.Target = nil
	cx.realm.globalProxyHandler = cx.defaultGlobalHandler()
}

func (ag *agent) hasDetachedGlobal(r *realm) bool {
	return r != nil && r.globalProxyHandler != nil && r.GlobalThis.ExtState().(*storage.ProxyState).Target != r.Global
}

// RevokeProxy revokes the proxy p: every internal method of p throws a TypeError from then on.
func RevokeProxy(p *storage.Object) {
	st := p.ExtState().(*storage.ProxyState)
	st.Target = nil
	st.Handler = nil
}

// IsArray implements IsArray.
func (cx *ExecutionContext) IsArray(v storage.Value) bool {
	o := v.O
	for o != nil && o.Class == storage.ClassProxy {
		st := o.ExtState().(*storage.ProxyState)
		if st.Handler == nil {
			cx.throwRevoked("IsArray")
		}
		o = st.Target
	}
	return o != nil && o.Kind == storage.KindArray
}

// FunctionContext returns the context of GetFunctionRealm(f).
func (cx *ExecutionContext) FunctionContext(f *storage.Object) *ExecutionContext {
	if r := cx.functionRealmOrNil(f); r != nil {
		return executionContextOf(r)
	}
	cx.throwRevoked("GetFunctionRealm")
	return nil
}

// functionRealmOrNil returns the function's realm, or nil for a revoked proxy.
func (cx *ExecutionContext) functionRealmOrNil(f *storage.Object) *storage.RealmPart {
	for {
		switch {
		case f.Fn != nil && f.Fn.Bound != nil:
			f = f.Fn.Bound.Target
		case f.Class == storage.ClassProxy:
			st := f.ExtState().(*storage.ProxyState)
			if st.Handler == nil {
				return nil
			}
			f = st.Target
		case f.Fn != nil && f.Fn.Code != nil:
			return f.Fn.Code.Realm
		case f.Fn != nil && f.Fn.Info != nil:
			return f.Fn.Info.Realm
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
func (cx *ExecutionContext) proxyEnter(o *storage.Object, op string) (target, handler *storage.Object) {
	st := o.ExtState().(*storage.ProxyState)
	if st.Handler == nil {
		cx.throwRevoked(op)
	}
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	if cx.depth >= maxDepth {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	target = st.Target
	if st.IsGlobal() && !st.AllowsGlobalAccess(cx.realm.globalProxyHandler) {
		target = cx.realm.foreignGlobalView
		if target == nil {
			cx.ThrowTypeError("no access")
		}
	}
	cx.depth++
	return target, st.Handler
}

func (ag *agent) proxyLeave() {
	ag.depth--
}

// proxyTrap returns the trap key of handler, or nil when the operation forwards to the target.
func (cx *ExecutionContext) proxyTrap(handler *storage.Object, key storage.CommonKey) *storage.Object {
	return cx.GetMethod(storage.Value{O: handler}, cx.Key(key))
}

// callTrap calls trap with handler as this.
func (cx *ExecutionContext) callTrap(trap, handler *storage.Object, args ...storage.Value) storage.Value {
	return cx.callOnStack(trap, storage.Value{O: handler}, args)
}

func (cx *ExecutionContext) throwTrapError(trap, msg string) {
	cx.ThrowTypeError("'" + trap + "' on proxy: " + msg)
}

// proxyGetPrototypeOf implements [[GetPrototypeOf]] of a proxy.
func (cx *ExecutionContext) proxyGetPrototypeOf(o *storage.Object) *storage.Object {
	target, handler := cx.proxyEnter(o, "getPrototypeOf")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyGetPrototypeOf)
	if trap == nil {
		return cx.ProtoOf(target)
	}
	r := cx.callTrap(trap, handler, storage.Value{O: target})
	if !r.IsObject() && !r.IsNull() {
		cx.throwTrapError("getPrototypeOf", "trap returned neither object nor null")
	}
	var proto *storage.Object
	if r.IsObject() {
		proto = r.O
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
func (cx *ExecutionContext) proxySetPrototypeOf(o *storage.Object, proto *storage.Object) bool {
	target, handler := cx.proxyEnter(o, "setPrototypeOf")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeySetPrototypeOf)
	if trap == nil {
		return cx.SetPrototypeOf(target, proto)
	}
	v := storage.Null
	if proto != nil {
		v = storage.Value{O: proto}
	}
	if !storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}, v)) {
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
func (cx *ExecutionContext) proxyIsExtensible(o *storage.Object) bool {
	target, handler := cx.proxyEnter(o, "isExtensible")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyIsExtensible)
	if trap == nil {
		return cx.IsExtensible(target)
	}
	r := storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}))
	if r != cx.IsExtensible(target) {
		cx.throwTrapError("isExtensible", "trap result does not reflect extensibility of proxy target")
	}
	return r
}

// proxyPreventExtensions implements [[PreventExtensions]] of a proxy.
func (cx *ExecutionContext) proxyPreventExtensions(o *storage.Object) bool {
	target, handler := cx.proxyEnter(o, "preventExtensions")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyPreventExtensions)
	if trap == nil {
		return cx.PreventExtensions(target)
	}
	r := storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}))
	if r && cx.IsExtensible(target) {
		cx.throwTrapError("preventExtensions", "trap returned truish but the proxy target is extensible")
	}
	return r
}

// proxyGetOwnProperty implements [[GetOwnProperty]] of a proxy.
func (cx *ExecutionContext) proxyGetOwnProperty(o *storage.Object, k storage.PropKey) (storage.PropDesc, bool) {
	target, handler := cx.proxyEnter(o, "getOwnPropertyDescriptor")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyGetOwnPropertyDescriptor)
	if trap == nil {
		return cx.GetOwnProperty(target, k)
	}
	const name = "getOwnPropertyDescriptor"
	r := cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k))
	if !r.IsObject() && !r.IsUndefined() {
		cx.throwTrapError(name, "trap returned neither object nor undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"'")
	}
	targetDesc, targetHas := cx.GetOwnProperty(target, k)
	if r.IsUndefined() {
		if !targetHas {
			return storage.PropDesc{}, false
		}
		if !targetDesc.Configurable() {
			cx.throwTrapError(name, "trap returned undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable in the proxy target")
		}
		if !cx.IsExtensible(target) {
			cx.throwTrapError(name, "trap returned undefined for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the non-extensible proxy target")
		}
		return storage.PropDesc{}, false
	}
	extensible := cx.IsExtensible(target)
	d := cx.ToPropertyDescriptor(r)
	d.Complete()
	if !storage.IsCompatiblePropertyDescriptor(extensible, &d, &targetDesc, targetHas) {
		cx.throwTrapError(name, "trap returned descriptor for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if !d.Configurable() {
		if !targetHas || targetDesc.Configurable() {
			cx.throwTrapError(name, "trap reported non-configurability for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is either non-existent or configurable in the proxy target")
		}
		if d.HasFalse(storage.HasWritable, storage.FlagWritable) && targetDesc.Writable() {
			cx.throwTrapError(name, "trap reported non-configurable and non-writable for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable, writable in the proxy target")
		}
	}
	return d, true
}

// proxyDefineOwnProperty implements [[DefineOwnProperty]] of a proxy.
func (cx *ExecutionContext) proxyDefineOwnProperty(o *storage.Object, k storage.PropKey, d storage.PropDesc) bool {
	target, handler := cx.proxyEnter(o, "defineProperty")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyDefineProperty)
	if trap == nil {
		return cx.defineOwnProperty(target, k, d)
	}
	const name = "defineProperty"
	if !storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k), cx.FromPropertyDescriptor(d))) {
		return false
	}
	targetDesc, targetHas := cx.GetOwnProperty(target, k)
	extensible := cx.IsExtensible(target)
	settingConfigFalse := d.HasFalse(storage.HasConfigurable, storage.FlagConfigurable)
	if !targetHas {
		if !extensible {
			cx.throwTrapError(name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' to the non-extensible proxy target")
		}
		if settingConfigFalse {
			cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-existent in the proxy target")
		}
		return true
	}
	if !storage.IsCompatiblePropertyDescriptor(extensible, &d, &targetDesc, true) {
		cx.throwTrapError(name, "trap returned truish for adding property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' that is incompatible with the existing property in the proxy target")
	}
	if settingConfigFalse && targetDesc.Configurable() {
		cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is configurable in the proxy target")
	}
	if !targetDesc.IsAccessor() && !targetDesc.Configurable() && targetDesc.Writable() && d.HasFalse(storage.HasWritable, storage.FlagWritable) {
		cx.throwTrapError(name, "trap returned truish for defining non-configurable property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which cannot be non-writable, unless there exists a corresponding non-configurable, non-writable own property of the target object")
	}
	return true
}

// proxyHas implements [[HasProperty]] of a proxy.
func (cx *ExecutionContext) proxyHas(o *storage.Object, k storage.PropKey) bool {
	target, handler := cx.proxyEnter(o, "has")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyHas)
	if trap == nil {
		return cx.HasProperty(target, k)
	}
	if storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k))) {
		return true
	}
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok {
		if !targetDesc.Configurable() {
			cx.throwTrapError("has", "trap returned falsish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as non-configurable")
		}
		if !cx.IsExtensible(target) {
			cx.throwTrapError("has", "trap returned falsish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' but the proxy target is not extensible")
		}
	}
	return false
}

// proxyGet implements [[Get]] of a proxy.
func (cx *ExecutionContext) proxyGet(o *storage.Object, k storage.PropKey, receiver storage.Value) storage.Value {
	target, handler := cx.proxyEnter(o, "get")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyGet)
	if trap == nil {
		return cx.getProp(target, k, receiver)
	}
	r := cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k), receiver)
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok && !targetDesc.Configurable() {
		if !targetDesc.IsAccessor() && !targetDesc.Writable() && !storage.SameValue(r, targetDesc.Value()) {
			cx.throwTrapError("get", "property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' is a read-only and non-configurable data property on the proxy target but the proxy did not return its actual value")
		}
		if targetDesc.IsAccessor() && targetDesc.Getter() == nil && !r.IsUndefined() {
			cx.throwTrapError("get", "property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' is a non-configurable accessor property on the proxy target and does not have a getter function, but the trap did not return 'undefined'")
		}
	}
	return r
}

// proxySet implements [[Set]] of a proxy. It reports false when the store was refused.
func (cx *ExecutionContext) proxySet(o *storage.Object, k storage.PropKey, val, receiver storage.Value) bool {
	target, handler := cx.proxyEnter(o, "set")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeySet)
	if trap == nil {
		return cx.setProp(target, k, val, receiver)
	}
	if !storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k), val, receiver)) {
		return false
	}
	if targetDesc, ok := cx.GetOwnProperty(target, k); ok && !targetDesc.Configurable() {
		if !targetDesc.IsAccessor() && !targetDesc.Writable() && !storage.SameValue(val, targetDesc.Value()) {
			cx.throwTrapError("set", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable data property with a different value")
		}
		if targetDesc.IsAccessor() && targetDesc.Setter() == nil {
			cx.throwTrapError("set", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which exists in the proxy target as a non-configurable and non-writable accessor property without a setter")
		}
	}
	return true
}

// proxyDelete implements [[Delete]] of a proxy. It reports false when the property is not deleted.
func (cx *ExecutionContext) proxyDelete(o *storage.Object, k storage.PropKey) bool {
	target, handler := cx.proxyEnter(o, "deleteProperty")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyDeleteProperty)
	if trap == nil {
		return cx.DeleteProp(target, k)
	}
	if !storage.ToBoolean(cx.callTrap(trap, handler, storage.Value{O: target}, cx.KeyValue(k))) {
		return false
	}
	targetDesc, ok := cx.GetOwnProperty(target, k)
	if !ok {
		return true
	}
	if !targetDesc.Configurable() {
		cx.throwTrapError("deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' which is non-configurable in the proxy target")
	}
	if !cx.IsExtensible(target) {
		cx.throwTrapError("deleteProperty", "trap returned truish for property '"+wtf8.ToValidUTF8(cx.KeyString(k))+"' but the proxy target is non-extensible")
	}
	return true
}

// proxyOwnKeys implements [[OwnPropertyKeys]] of a proxy.
func (cx *ExecutionContext) proxyOwnKeys(o *storage.Object) []storage.PropKey {
	target, handler := cx.proxyEnter(o, "ownKeys")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyOwnKeys)
	if trap == nil {
		return cx.OwnKeys(target)
	}
	const name = "ownKeys"
	keys := cx.propertyKeyList(cx.callTrap(trap, handler, storage.Value{O: target}))
	var p Pacer
	unchecked := make(map[storage.PropKey]struct{}, len(keys))
	for _, k := range keys {
		p.Step(cx)
		if _, dup := unchecked[k]; dup {
			cx.throwTrapError(name, "trap returned duplicate entries")
		}
		unchecked[k] = struct{}{}
	}
	extensible := cx.IsExtensible(target)
	var configurable, nonconfigurable []storage.PropKey
	for _, k := range cx.OwnKeys(target) {
		p.Step(cx)
		if d, ok := cx.GetOwnProperty(target, k); ok && !d.Configurable() {
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
func (cx *ExecutionContext) propertyKeyList(v storage.Value) []storage.PropKey {
	if !v.IsObject() {
		cx.ThrowTypeError("CreateListFromArrayLike called on non-object")
	}
	n := cx.LengthOfArrayLike(v.O)
	var keys []storage.PropKey
	for i := range n {
		cx.PollAt(i)
		k := storage.IndexKey(uint32(i))
		if i > storage.MaxArrayIndex {
			k = cx.KeyOfString(wtf8.String(strconv.FormatInt(i, 10)))
		}
		e := cx.getProp(v.O, k, v)
		if !e.IsString() && !e.IsSymbol() {
			cx.ThrowTypeError(cx.DisplayString(e) + " is not a valid property name")
		}
		keys = append(keys, cx.ToPropertyKey(e))
	}
	return keys
}

// proxyFunction is the code of the proxies that can be called.
func proxyFunction(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	p := cx.activeNative()
	if nt := cx.GetNewTarget(); nt != nil {
		return storage.Value{O: cx.proxyConstruct(p, args, nt)}
	}
	return cx.proxyCall(p, this, args)
}

// proxyCall implements [[Call]] of a proxy.
func (cx *ExecutionContext) proxyCall(o *storage.Object, this storage.Value, args []storage.Value) storage.Value {
	target, handler := cx.proxyEnter(o, "apply")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyApply)
	if trap == nil {
		return cx.Call(target, this, args...)
	}
	argArray := cx.NewArray(append([]storage.Value(nil), args...))
	return cx.callTrap(trap, handler, storage.Value{O: target}, this, storage.Value{O: argArray})
}

// proxyConstruct implements [[Construct]] of a proxy.
func (cx *ExecutionContext) proxyConstruct(o *storage.Object, args []storage.Value, newTarget *storage.Object) *storage.Object {
	target, handler := cx.proxyEnter(o, "construct")
	defer cx.proxyLeave()
	trap := cx.proxyTrap(handler, storage.KeyConstruct)
	if trap == nil {
		return cx.Construct(target, args, newTarget)
	}
	argArray := cx.NewArray(append([]storage.Value(nil), args...))
	r := cx.callTrap(trap, handler, storage.Value{O: target}, storage.Value{O: argArray}, storage.Value{O: newTarget})
	if !r.IsObject() {
		cx.throwTrapError("construct", "trap returned non-object ('"+cx.DisplayString(r)+"')")
	}
	return r.O
}

// ToPropertyDescriptor implements ToPropertyDescriptor.
func (cx *ExecutionContext) ToPropertyDescriptor(v storage.Value) storage.PropDesc {
	if !v.IsObject() {
		cx.ThrowTypeError("property description must be an object: " + cx.DisplayString(v))
	}
	o := v.O
	var d storage.PropDesc
	field := func(key storage.CommonKey) (storage.Value, bool) {
		k := storage.NamedKey(cx.Key(key))
		if !cx.HasProperty(o, k) {
			return storage.Undefined, false
		}
		return cx.getProp(o, k, v), true
	}
	if x, ok := field(storage.KeyEnumerable); ok {
		d.SetEnumerable(storage.ToBoolean(x))
	}
	if x, ok := field(storage.KeyConfigurable); ok {
		d.SetConfigurable(storage.ToBoolean(x))
	}
	if x, ok := field(storage.KeyValue); ok {
		d.SetValue(x)
	}
	if x, ok := field(storage.KeyWritable); ok {
		d.SetWritable(storage.ToBoolean(x))
	}
	if x, ok := field(storage.KeyGet); ok {
		if !x.IsUndefined() && !x.IsCallable() {
			cx.ThrowTypeError("getter must be a function: " + cx.DisplayString(x))
		}
		var f *storage.Object
		if x.IsObject() {
			f = x.O
		}
		d.SetGetter(f)
	}
	if x, ok := field(storage.KeySet); ok {
		if !x.IsUndefined() && !x.IsCallable() {
			cx.ThrowTypeError("setter must be a function: " + cx.DisplayString(x))
		}
		var f *storage.Object
		if x.IsObject() {
			f = x.O
		}
		d.SetSetter(f)
	}
	if d.IsAccessor() && d.IsData() {
		cx.ThrowTypeError("invalid property descriptor. Cannot both specify accessors and a value or writable attribute")
	}
	return d
}

// FromPropertyDescriptor implements FromPropertyDescriptor for a descriptor that is not undefined.
func (cx *ExecutionContext) FromPropertyDescriptor(d storage.PropDesc) storage.Value {
	o := cx.NewEmptyObjectWithRoom(d.FieldCount())
	fn := func(f *storage.Object) storage.Value {
		if f == nil {
			return storage.Undefined
		}
		return storage.Value{O: f}
	}
	if d.HasField(storage.HasValue) {
		cx.Add(o, cx.Key(storage.KeyValue), d.Value(), storage.FlagsData)
	}
	if d.HasField(storage.HasWritable) {
		cx.Add(o, cx.Key(storage.KeyWritable), storage.Bool(d.Writable()), storage.FlagsData)
	}
	if d.HasField(storage.HasGet) {
		cx.Add(o, cx.Key(storage.KeyGet), fn(d.Getter()), storage.FlagsData)
	}
	if d.HasField(storage.HasSet) {
		cx.Add(o, cx.Key(storage.KeySet), fn(d.Setter()), storage.FlagsData)
	}
	if d.HasField(storage.HasEnumerable) {
		cx.Add(o, cx.Key(storage.KeyEnumerable), storage.Bool(d.Enumerable()), storage.FlagsData)
	}
	if d.HasField(storage.HasConfigurable) {
		cx.Add(o, cx.Key(storage.KeyConfigurable), storage.Bool(d.Configurable()), storage.FlagsData)
	}
	return storage.Value{O: o}
}
