// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func (cx *ExecutionContext) throwTDZ(name *Object) {
	if name.str == "this" {
		cx.ThrowReferenceError("must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	cx.ThrowReferenceError("cannot access '" + wtf8.ToValidUTF8(name.str) + "' before initialization")
}

func (cx *ExecutionContext) throwReadOnly(v Value, k PropKey) {
	cx.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' of " + cx.DisplayString(v))
}

// callAt returns the index of the call instruction of fp that ends before pc, past its extra.
func callAt(fp *funcProto, pc int) int {
	pc--
	for fp.code[pc].Op == bytecode.OpExtra {
		pc--
	}
	return pc
}

// calleeName names the function that the call instruction at pc calls, for an error message.
func (ag *agent) calleeName(fp *funcProto, pc int) wtf8.String {
	target := fp.code[pc].B
	for j := pc - 1; j >= 0 && j >= pc-64; j-- {
		in := fp.code[j]
		if in.Op == bytecode.OpExtra || in.A != target {
			continue
		}
		switch in.Op {
		case bytecode.OpGetProp, bytecode.OpGetPropChain, bytecode.OpGetMethod:
			return fp.keys[in.C].str
		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict:
			return fp.keys[in.B].str
		}
		break
	}
	return ""
}

// throwNotCallable throws the TypeError for the call instruction of fp that ends before pc,
// whose callee is not a function.
func (cx *ExecutionContext) throwNotCallable(fp *funcProto, pc int, regs []Value) {
	pc = callAt(fp, pc)
	name := wtf8.ToValidUTF8(cx.calleeName(fp, pc))
	if name == "" {
		name = cx.DisplayString(regs[fp.code[pc].B])
	}
	cx.ThrowTypeError(name + " is not a function")
}

// throwNotConstructor throws the TypeError for the call instruction of fp that ends before pc,
// whose callee is not a constructor.
func (cx *ExecutionContext) throwNotConstructor(fp *funcProto, pc int, regs []Value) {
	pc = callAt(fp, pc)
	name := wtf8.ToValidUTF8(cx.calleeName(fp, pc))
	if name == "" {
		name = cx.DisplayString(regs[fp.code[pc].B])
	}
	cx.ThrowTypeError(name + " is not a constructor")
}

// getGlobal reads the global variable key, as strict code does when strict is set. When it does
// not exist, it throws a ReferenceError unless orUndef is set.
func (cx *ExecutionContext) getGlobal(key *Object, c *globalCache, orUndef, strict bool) Value {
	r := cx.realm
	g := r.global
	if v, ok := cx.getGlobalLexical(key, c); ok {
		return v
	}
	if p, ok := g.shape.lookup(key); ok && p.flags&FlagAccessor == 0 {
		c.holder = g
		c.shape = g.shape
		c.ref = &g.slots[p.slot]
		return g.slots[p.slot]
	}
	switch l := cx.Quiet().lookupNamed(g, key); l.kind {
	case lookupAbsent, lookupNumericMiss:
		if orUndef {
			return Undefined
		}
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	case lookupSlot:
		v, getter := cx.readNamedSlot(g, l.holder, l.prop, nil)
		if getter != nil {
			return cx.Call(getter, Value{o: r.globalThis})
		}
		return v
	}
	k := PropKey{key: key}
	if !cx.HasProperty(g, k) {
		if orUndef {
			return Undefined
		}
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	}
	// HasBinding can create a lexical binding or remove the property or its prototype.
	// GetBindingValue selects the binding again in the same global environment.
	if v, ok := cx.getGlobalLexical(key, c); ok {
		return v
	}
	if !cx.HasProperty(g, k) {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
		return Undefined
	}
	return cx.getProp(g, k, Value{o: r.globalThis})
}

func (cx *ExecutionContext) getGlobalLexical(key *Object, c *globalCache) (Value, bool) {
	r := cx.realm
	p, ok := r.globalLex.shape.lookup(key)
	if !ok {
		return Undefined, false
	}
	slot := &r.globalLex.slots[p.slot]
	if slot.o == tdzObject {
		cx.throwTDZ(key)
	}
	// Only initialized bindings enter the cache, and they stay initialized.
	c.holder = r.global
	c.shape = r.global.shape
	c.ref = slot
	return *slot, true
}

// resolveGlobal returns undefined for a global binding and null for an unresolvable name.
func (cx *ExecutionContext) resolveGlobal(key *Object, c *globalCache) Value {
	g := cx.realm.global
	_, lex := cx.realm.globalLex.shape.lookup(key)
	if _, own := g.shape.lookup(key); lex || own {
		if c != nil {
			c.holder = g
			c.shape = g.shape
		}
		return Undefined
	}
	if cx.HasProperty(g, PropKey{key: key}) {
		return Undefined
	}
	return Null
}

// setGlobal writes a resolved binding in the global environment.
func (cx *ExecutionContext) setGlobal(key *Object, val Value, c *globalCache, strict bool) {
	r := cx.realm
	g := r.global
	if p, ok := r.globalLex.shape.lookup(key); ok {
		if r.globalLex.slots[p.slot].o == tdzObject {
			cx.throwTDZ(key)
		}
		if p.flags&FlagWritable == 0 {
			cx.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(key.str) + "'")
		}
		r.globalLex.slots[p.slot] = val
		// As in getGlobal, c refers only to initialized bindings.
		c.holder = g
		c.shape = g.shape
		c.ref = &r.globalLex.slots[p.slot]
		return
	}
	if p, ok := g.shape.lookup(key); ok && p.flags&(FlagAccessor|FlagWritable) == FlagWritable {
		g.slots[p.slot] = val
		c.holder = g
		c.shape = g.shape
		c.ref = &g.slots[p.slot]
		return
	}
	k := PropKey{key: key}
	// SetMutableBinding checks whether the binding still exists. A proxy can observe
	// the check even in sloppy code.
	if !cx.HasProperty(g, k) && strict {
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	}
	if !cx.setProp(g, k, val, Value{o: r.globalThis}) && strict {
		cx.throwReadOnly(Value{o: g}, k)
	}
}

// checkGlobalDecl throws when declaring key at the top level of a script would conflict
// with an existing declaration. what is a bytecode Decl kind.
func (cx *ExecutionContext) checkGlobalDecl(key *Object, what int) {
	r := cx.realm
	_, lex := r.globalLex.shape.lookup(key)
	switch what {
	case bytecode.DeclVar:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		if !cx.HasOwnProperty(r.global, PropKey{key: key}) && !cx.IsExtensible(r.global) {
			cx.ThrowTypeError("cannot declare global variable '" + wtf8.ToValidUTF8(key.str) + "'")
		}
	case bytecode.DeclLexical:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		// A lexical declaration may not hide a global property that cannot be reconfigured.
		if d, ok := cx.GetOwnProperty(r.global, PropKey{key: key}); ok && d.flags&FlagConfigurable == 0 {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
	case bytecode.DeclFunction:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		d, ok := cx.GetOwnProperty(r.global, PropKey{key: key})
		switch {
		case !ok:
			if !cx.IsExtensible(r.global) {
				cx.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.str) + "'")
			}
		case d.flags&FlagConfigurable != 0:
		case d.flags&FlagAccessor != 0 || d.flags&(FlagWritable|FlagEnumerable) != FlagWritable|FlagEnumerable:
			cx.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.str) + "'")
		}
	}
}

// declareGlobalVar implements CreateGlobalVarBinding, and CreateGlobalFunctionBinding when
// flags has [bytecode.GlobalVarFunc]. flags is a set of the bytecode GlobalVar flags.
func (cx *ExecutionContext) declareGlobalVar(key *Object, flags int, fn Value) {
	g := cx.realm.global
	k := PropKey{key: key}
	d, ok := cx.GetOwnProperty(g, k)
	attrs := FlagEnumerable | FlagWritable
	if flags&bytecode.GlobalVarDeletable != 0 {
		attrs |= FlagConfigurable
	}
	if flags&bytecode.GlobalVarFunc == 0 {
		if !ok {
			cx.DefinePropertyOrThrow(g, k, dataDesc(Undefined, attrs))
		}
		return
	}
	if !ok || d.flags&FlagConfigurable != 0 {
		cx.DefinePropertyOrThrow(g, k, dataDesc(fn, attrs))
		return
	}
	cx.DefinePropertyOrThrow(g, k, PropDesc{
		value: fn,
		has:   HasValue,
	})
}

// declareGlobalAnnexB declares the global variable key of a block-level function of sloppy
// code, as Annex B.3.2.2 and B.3.2.3 do, and reports whether it is declared. It declares
// nothing when a global lexical declaration of key exists or CanDeclareGlobalVar(key) is false.
// flags is a set of the bytecode GlobalVar flags.
func (cx *ExecutionContext) declareGlobalAnnexB(key *Object, flags int) bool {
	r := cx.realm
	if _, lex := r.globalLex.shape.lookup(key); lex {
		return false
	}
	if !cx.HasOwnProperty(r.global, PropKey{key: key}) && !cx.IsExtensible(r.global) {
		return false
	}
	cx.declareGlobalVar(key, flags&^bytecode.GlobalVarFunc, Undefined)
	return true
}

// declareGlobalLex creates the uninitialized global lexical binding key.
func (cx *ExecutionContext) declareGlobalLex(key *Object, isConst bool) {
	r := cx.realm
	flags := FlagsData
	if isConst {
		flags = FlagEnumerable | FlagConfigurable
	}
	cx.add(r.globalLex, key, tdz, flags)
	// The binding may hide a property of the global object, and the bindings may have moved,
	// so the global variable caches must not apply any longer.
	r.global.renewShape()
}

func (cx *ExecutionContext) initGlobalLex(key *Object, val Value) {
	r := cx.realm
	p, _ := r.globalLex.shape.lookup(key)
	r.globalLex.slots[p.slot] = val
}

// getValue returns v[k], or the getter to call.
func (cx *ExecutionContext) getValue(v, k Value) (Value, *Object) {
	if ko := k.o; ko == numberObject {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return cx.typedArrayGet(o, i), nil
			}
			return cx.getIndex(v, i)
		}
	} else if ko != nil && ko.kind == kindString {
		// An element in elems is an own data property, whatever object v is.
		if i, ok := ko.knownArrayIndex(); ok {
			if o := v.o; o != nil && o.objectProperties != nil && int64(i) < int64(len(o.elems)) {
				if e := o.elems[i]; !e.isHole() {
					return e, nil
				}
			}
		}
	}
	// The receiver is checked before the key is converted.
	if v.IsNullish() {
		cx.ThrowTypeError("cannot read properties of " + cx.DisplayString(v) + " (reading '" + cx.DisplayString(k) + "')")
	}
	key := cx.QueryKey(k)
	if key.key == nil {
		return cx.getIndex(v, key.idx)
	}
	if key.idx == missingKey {
		return cx.getMissing(v, key)
	}
	return cx.getNamed(v, key.key, nil)
}

// setValue stores v[k] = val and returns the setter to call, if any. A store that fails throws
// a TypeError in strict mode.
func (cx *ExecutionContext) setValue(v, k, val Value, strict bool) *Object {
	setter, ok, key := cx.storeValue(v, k, val)
	if !ok && strict {
		cx.throwReadOnly(v, key)
	}
	return setter
}

// storeValue stores v[k] = val. The first two results are those of setNamed, and the last is k
// converted to a property key when the store fails.
func (cx *ExecutionContext) storeValue(v, k, val Value) (*Object, bool, PropKey) {
	if ko := k.o; ko == numberObject {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return nil, cx.typedArraySet(o, i, val), IndexKey(i)
			}
			setter, ok := cx.setIndex(v, i, val)
			return setter, ok, IndexKey(i)
		}
	} else if ko != nil && ko.kind == kindString {
		// An element in elems is a writable own data property, whatever object v is.
		if i, ok := ko.knownArrayIndex(); ok {
			if o := v.o; o != nil && o.objectProperties != nil && int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
				o.elems[i] = val
				return nil, true, PropKey{}
			}
		}
	}
	if v.IsNullish() {
		cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + cx.DisplayString(k) + "')")
	}
	key := cx.ToPropertyKey(k)
	if key.key == nil {
		setter, ok := cx.setIndex(v, key.idx, val)
		return setter, ok, key
	}
	setter, ok := cx.setNamed(v, key.key, val, nil)
	return setter, ok, key
}

// deleteValue implements the delete operator on a property.
func (cx *ExecutionContext) deleteValue(v, k Value, strict bool) Value {
	if v.IsNullish() {
		cx.ThrowTypeError("cannot convert undefined or null to object")
	}
	key := cx.ToPropertyKey(k)
	o := cx.ToObject(v)
	ok := cx.DeleteProp(o, key)
	if !ok && strict {
		cx.ThrowTypeError("cannot delete property '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "' of " + cx.DisplayString(v))
	}
	return Bool(ok)
}

// superBase implements GetSuperBase for the home object home.
func (cx *ExecutionContext) superBase(home Value) Value {
	if !home.IsObject() {
		cx.ThrowSyntaxError("'super' keyword unexpected here")
	}
	if p := cx.ProtoOf(home.o); p != nil {
		return ObjectValue(p)
	}
	return Null
}

// superObject returns the object that a super property access with the super base base starts from.
func (cx *ExecutionContext) superObject(base Value) *Object {
	if base.IsNull() {
		cx.ThrowTypeError("cannot access a property of a null prototype through 'super'")
	}
	return base.o
}

// getSuper reads the property k of the super base base with the this value this,
// and returns the property key as well.
func (cx *ExecutionContext) getSuper(base, k, this Value) (Value, PropKey) {
	o := cx.superObject(base)
	key := cx.ToPropertyKey(k)
	return cx.getProp(o, key, this), key
}

func (cx *ExecutionContext) setSuper(base, k, val, this Value, strict bool) {
	o := cx.superObject(base)
	key := cx.ToPropertyKey(k)
	if !cx.setProp(o, key, val, this) && strict {
		cx.throwReadOnly(this, key)
	}
}

// instanceOf implements InstanceofOperator with cx active. site is the inline cache of the
// OpInstanceof that evaluates it, or nil.
func (cx *ExecutionContext) instanceOf(v, c Value, site *propCache) bool {
	if !c.IsObject() {
		cx.ThrowTypeError("right-hand side of 'instanceof' is not an object")
	}
	defaultMethod := cx.Intrinsic(IntrinsicFunctionPrototypeHasInstance)
	for {
		method := defaultMethod
		if !cx.inheritsHasInstance(c.o) {
			site = nil
			method = cx.GetMethod(c, cx.WellKnownSymbol(SymbolHasInstance))
		}
		if method == nil {
			if !c.IsCallable() {
				cx.ThrowTypeError("right-hand side of 'instanceof' is not callable")
			}
			// OrdinaryHasInstance of a bound function is InstanceofOperator of its target.
			if b := c.o.fn.bound; b != nil {
				c = Value{o: b.target}
				continue
			}
			return cx.OrdinaryHasInstance(c, v)
		}
		if method == defaultMethod && !cx.interrupt.Load() {
			call := defaultHasInstanceCall{
				method:   method,
				receiver: c.o,
			}
			return call.run(cx, v, site)
		}
		return ToBoolean(cx.Call(method, c, v))
	}
}

// defaultHasInstanceCall is a selected call of the current realm's default @@hasInstance.
type defaultHasInstanceCall struct {
	method   *Object
	receiver *Object
}

// enterThrough records the selected calls from the receiver through last. Each intervening
// bound target was proved to inherit the same method before any observable operation.
func (call defaultHasInstanceCall) enterThrough(cx *ExecutionContext, last *Object) int {
	depth := len(cx.natives)
	for ctor := call.receiver; ; ctor = ctor.fn.bound.target {
		cx.pushNative(call.method, Value{o: ctor}, nil)
		if ctor == last {
			return depth
		}
	}
}

func (call defaultHasInstanceCall) run(cx *ExecutionContext, v Value, site *propCache) bool {
	ctor := call.receiver
	for ctor.fn != nil && ctor.fn.bound != nil {
		target := ctor.fn.bound.target
		if cx.interrupt.Load() || !cx.inheritsHasInstance(target) {
			// Looking up the target's method can observe the enclosing calls.
			depth := call.enterThrough(cx, ctor)
			result := cx.instanceOf(v, Value{o: target}, nil)
			cx.truncateNatives(depth)
			return result
		}
		ctor, site = target, nil
	}
	if ctor.fn == nil || !v.IsObject() {
		return false
	}
	slot := cx.ownFunctionPrototypeSlot(ctor, site)
	h := v.o
	if slot != nil && slot.IsObject() {
		for h.class != classProxy {
			h = h.shape.proto
			if h == slot.o {
				return true
			}
			if h == nil {
				return false
			}
		}
	}
	depth := call.enterThrough(cx, ctor)
	result := cx.hasPrototype(h, cx.readInstancePrototype(ctor, slot))
	cx.truncateNatives(depth)
	return result
}

// inheritsHasInstance reports whether the @@hasInstance property of c is the one that it
// inherits from %Function.prototype% of the current realm. A false result does not mean the
// opposite.
func (cx *ExecutionContext) inheritsHasInstance(c *Object) bool {
	// The property of %Function.prototype% is neither writable nor configurable, so only the
	// objects before it on the chain can hide it.
	fp := cx.Intrinsic(IntrinsicFunctionPrototype)
	key := cx.WellKnownSymbol(SymbolHasInstance)
	for h := c; h != fp; h = h.shape.proto {
		// The properties that an unmaterialized function lacks have string keys.
		if h == nil || h.class != classFunction && (h.class != classObject || h.flags&objExotic != 0) {
			return false
		}
		if _, ok := h.shape.lookup(key); ok {
			return false
		}
	}
	return true
}

// ownFunctionPrototypeSlot returns the own prototype slot of an ordinary function, or nil.
func (cx *ExecutionContext) ownFunctionPrototypeSlot(ctor *Object, site *propCache) *Value {
	if ctor.class != classFunction {
		return nil
	}
	if ctor.flags&objExotic != 0 {
		cx.materialize(ctor)
	}
	p, ok := ctor.shape.lookup(cx.Key(KeyPrototype))
	if !ok {
		return nil
	}
	slot := &ctor.slots[p.slot]
	if site != nil && ctor.flags&objExotic == 0 && slot.IsObject() {
		// Caching the constructor's own prototype slot also relies on @@hasInstance staying inherited.
		site.setInherited(ctor.shape, cacheOwn, cx.protoEpoch)
		site.slot = p.slot
	}
	return slot
}

// OrdinaryHasInstance implements OrdinaryHasInstance.
func (cx *ExecutionContext) OrdinaryHasInstance(c, v Value) bool {
	if !c.IsCallable() {
		return false
	}
	if b := c.o.fn.bound; b != nil {
		var result bool
		cx.Run(func() {
			result = cx.instanceOf(v, Value{o: b.target}, nil)
		})
		return result
	}
	if !v.IsObject() {
		return false
	}
	slot := cx.ownFunctionPrototypeSlot(c.o, nil)
	return cx.hasPrototype(v.o, cx.readInstancePrototype(c.o, slot))
}

func (cx *ExecutionContext) readInstancePrototype(ctor *Object, slot *Value) *Object {
	var p Value
	if slot == nil {
		p = cx.GetV(Value{o: ctor}, cx.Key(KeyPrototype))
	} else {
		p = *slot
		if p.o != nil && p.o.kind == kindAccessor {
			if getter := p.accessor().getter; getter != nil {
				p = cx.Call(getter, Value{o: ctor})
			}
		}
	}
	if !p.IsObject() {
		cx.ThrowTypeError("function has non-object prototype in instanceof check")
	}
	return p.o
}

func (cx *ExecutionContext) hasPrototype(o, proto *Object) bool {
	for h := cx.ProtoOf(o); h != nil; h = cx.ProtoOf(h) {
		if h == proto {
			return true
		}
	}
	return false
}

// privateStorage returns the object holding o's private elements.
func (cx *ExecutionContext) privateStorage(o *Object) *Object {
	if o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.isGlobal() {
			if !st.allowsGlobalAccess(cx.realm.globalProxyHandler) {
				cx.ThrowTypeError("no access")
			}
			return st.target
		}
	}
	return o
}

// getPrivate returns the private element name of v, or the getter to call.
func (cx *ExecutionContext) getPrivate(v Value, name *Object) (Value, *Object) {
	if v.IsObject() {
		o := cx.privateStorage(v.o)
		if p, ok := o.shape.lookup(name); ok {
			val := o.slots[p.slot]
			if p.flags&FlagAccessor != 0 {
				g := val.accessor().getter
				if g == nil {
					cx.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.str) + "' was defined without a getter")
				}
				return Undefined, g
			}
			return val, nil
		}
	}
	cx.ThrowTypeError("cannot read private member #" + wtf8.ToValidUTF8(name.str) + " from an object whose class did not declare it")
	return Undefined, nil
}

// setPrivate stores val as the private element name of v, or returns the setter to call.
func (cx *ExecutionContext) setPrivate(v Value, name *Object, val Value) *Object {
	if v.IsObject() {
		o := cx.privateStorage(v.o)
		if p, ok := o.shape.lookup(name); ok {
			if p.flags&FlagAccessor != 0 {
				s := o.slots[p.slot].accessor().setter
				if s == nil {
					cx.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.str) + "' was defined without a setter")
				}
				return s
			}
			if p.flags&FlagWritable == 0 {
				cx.ThrowTypeError("private method '#" + wtf8.ToValidUTF8(name.str) + "' is not writable")
			}
			o.slots[p.slot] = val
			return nil
		}
	}
	cx.ThrowTypeError("cannot write private member #" + wtf8.ToValidUTF8(name.str) + " to an object whose class did not declare it")
	return nil
}

// definePrivate adds the private element name to v. val is the field value, a method, or an accessor.
func (cx *ExecutionContext) definePrivate(v Value, name *Object, val Value) {
	if !v.IsObject() {
		cx.ThrowTypeError("cannot define a private member on a non-object")
	}
	o := cx.privateStorage(v.o)
	if _, ok := o.shape.lookup(name); ok {
		cx.ThrowTypeError("cannot initialize #" + wtf8.ToValidUTF8(name.str) + " twice on the same object")
	}
	flags := FlagWritable
	switch {
	case val.o != nil && val.o.kind == kindAccessor:
		flags = FlagAccessor
	case name.flags&objFrozenLength != 0:
		// The name stands for a method, which cannot be assigned to.
		flags = 0
	}
	cx.add(o, name, val, flags)
}

// ensureCanAddPrivate throws a TypeError if the private element name cannot be added to v
// because v is not extensible.
func (cx *ExecutionContext) ensureCanAddPrivate(v Value, name *Object) {
	if v.IsObject() && !cx.IsExtensible(cx.privateStorage(v.o)) {
		cx.ThrowTypeError("cannot define private member #" + wtf8.ToValidUTF8(name.str) + " on an object that is not extensible")
	}
}

// arith implements the binary arithmetic operators other than + for operands that are not both numbers.
func (cx *ExecutionContext) arith(op bytecode.Opcode, x, y Value) Value {
	if xi, ok := x.BigInt64(); ok {
		if yi, ok := y.BigInt64(); ok {
			if r, ok := arithBigInt64(op, xi, yi); ok {
				return r
			}
		}
	}
	x = cx.ToNumeric(x)
	y = cx.ToNumeric(y)
	if x.o == numberObject && y.o == numberObject {
		a, b := x.n, y.n
		switch op {
		case bytecode.OpSub:
			return Number(a - b)
		case bytecode.OpMul:
			return Number(a * b)
		case bytecode.OpDiv:
			return Number(a / b)
		case bytecode.OpMod:
			return Number(math.Mod(a, b))
		case bytecode.OpExp:
			return Number(numconv.NumberPow(a, b))
		case bytecode.OpBitOr:
			return Number(float64(numconv.ToInt32(a) | numconv.ToInt32(b)))
		case bytecode.OpBitAnd:
			return Number(float64(numconv.ToInt32(a) & numconv.ToInt32(b)))
		case bytecode.OpBitXor:
			return Number(float64(numconv.ToInt32(a) ^ numconv.ToInt32(b)))
		case bytecode.OpShl:
			return Number(float64(numconv.ToInt32(a) << (numconv.ToUint32(b) & 31)))
		case bytecode.OpShr:
			return Number(float64(numconv.ToInt32(a) >> (numconv.ToUint32(b) & 31)))
		case bytecode.OpUShr:
			return Number(float64(numconv.ToUint32(a) >> (numconv.ToUint32(b) & 31)))
		}
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	if x.o == numberObject || y.o == numberObject {
		cx.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	// Subtraction, the bitwise operations and the shifts on huge BigInts cannot stop once they
	// start, so a pending interruption is taken before them.
	if cx.interrupt.Load() {
		cx.Poll()
	}
	a, b := x.BigInt(), y.BigInt()
	r := new(big.Int)
	switch op {
	case bytecode.OpSub:
		r.Sub(a, b)
		cx.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpMul:
		return cx.multiplyBigInts(a, b)
	case bytecode.OpDiv:
		if b.Sign() == 0 {
			cx.ThrowRangeError("division by zero")
		}
		r, _ = numconv.QuoRemBigInt(a, b, cx.Poll)
	case bytecode.OpMod:
		if b.Sign() == 0 {
			cx.ThrowRangeError("division by zero")
		}
		_, r = numconv.QuoRemBigInt(a, b, cx.Poll)
	case bytecode.OpExp:
		if b.Sign() < 0 {
			cx.ThrowRangeError("exponent must be non-negative")
		}
		if a.CmpAbs(big.NewInt(1)) <= 0 {
			r.Exp(a, b, nil)
			break
		}
		// The result of a base other than 0, 1 and -1 has more bits than the exponent.
		if !b.IsInt64() || b.Int64() >= MaxBigIntBits {
			cx.ThrowRangeError("maximum BigInt size exceeded")
		}
		n := b.Int64()
		size := numconv.PowBitLen(a, n)
		cx.checkBigIntBits(size)
		if size > bigExpStepBits {
			numconv.ExpBigInt(r, a, n, cx.Poll)
		} else {
			r.Exp(a, b, nil)
		}
		cx.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitOr:
		r.Or(a, b)
		cx.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitAnd:
		r.And(a, b)
		cx.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitXor:
		r.Xor(a, b)
		cx.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpShl, bytecode.OpShr:
		n := b
		if op == bytecode.OpShr {
			n = new(big.Int).Neg(b)
		}
		switch {
		case !n.IsInt64() || n.Int64() > 1<<30:
			if n.Sign() > 0 && a.Sign() != 0 {
				cx.ThrowRangeError("maximum BigInt size exceeded")
			}
			if a.Sign() < 0 && n.Sign() < 0 {
				r.SetInt64(-1)
			}
		case n.Sign() >= 0:
			if a.Sign() != 0 {
				cx.checkBigIntBits(int64(a.BitLen()) + n.Int64())
			}
			r.Lsh(a, uint(n.Int64()))
		case n.Int64() < -(1 << 30):
			if a.Sign() < 0 {
				r.SetInt64(-1)
			}
		default:
			r.Rsh(a, uint(-n.Int64()))
		}
	case bytecode.OpUShr:
		cx.ThrowTypeError("BigInts have no unsigned right shift, use >> instead")
	default:
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	return NewBigInt(r)
}

func (cx *ExecutionContext) negate(x Value) Value {
	if i, ok := x.BigInt64(); ok && i != math.MinInt64 {
		return BigIntFromInt64(-i)
	}
	x = cx.ToNumeric(x)
	if x.o == numberObject {
		return Number(-x.n)
	}
	return NewBigInt(new(big.Int).Neg(x.BigInt()))
}

func (cx *ExecutionContext) bitNot(x Value) Value {
	if i, ok := x.BigInt64(); ok {
		return BigIntFromInt64(^i)
	}
	x = cx.ToNumeric(x)
	if x.o == numberObject {
		return Number(float64(^numconv.ToInt32(x.n)))
	}
	r := new(big.Int).Not(x.BigInt())
	cx.checkBigIntBits(int64(r.BitLen()))
	return NewBigInt(r)
}

// step implements ++ and -- for an operand that is not a number: it adds d to x.
func (cx *ExecutionContext) step(x Value, d int64) Value {
	if i, ok := x.BigInt64(); ok {
		if s, ok := add64(i, d); ok {
			return BigIntFromInt64(s)
		}
	}
	x = cx.ToNumeric(x)
	if x.o == numberObject {
		return Number(x.n + float64(d))
	}
	return cx.addBigInts(x, BigIntFromInt64(d))
}

// classElement is an element that the constructor of a class adds to every instance.
type classElement struct {
	kind int
	key  Value
	val  *Object

	// init is the initial value of a field without an initializer function.
	init Value

	// dec is what the decorators of a field add to it, or nil.
	dec *fieldDecoration

	// cache is the shape transition that defining a public field takes.
	cache propCache
}

// addClassElement registers an instance element of the class ctor, or defines a static element at once.
// For a field, v is the function that computes the initial value, or with [bytecode.ElemValue]
// in kind, the initial value itself.
func (cx *ExecutionContext) addClassElement(ctor *Object, key Value, v Value, kind int) {
	e := classElement{
		kind: kind &^ (bytecode.ElemStatic | bytecode.ElemValue),
		key:  key,
	}
	if kind&bytecode.ElemValue != 0 {
		e.init = v
	} else {
		e.val = v.o
	}
	if kind&bytecode.ElemStatic != 0 {
		cx.defineClassElement(Value{o: ctor}, &e)
		return
	}
	if ctor.fn.fields == nil {
		ctor.ownFunction().fields = &[]classElement{}
	}
	*ctor.fn.fields = append(*ctor.fn.fields, e)
}

// initFields defines the instance elements of the class ctor on this.
func (cx *ExecutionContext) initFields(ctor *Object, this Value) {
	fields := *ctor.fn.fields
	// Private methods and accessors exist before any field initializer runs.
	checked := false
	for i := range fields {
		if e := &fields[i]; e.kind != bytecode.ElemField && e.kind != elemInitializer {
			if !checked {
				cx.ensureCanAddPrivate(this, e.key.o)
				checked = true
			}
			cx.defineClassElement(this, e)
		}
	}
	for i := range fields {
		switch e := &fields[i]; e.kind {
		case bytecode.ElemField:
			cx.defineClassElement(this, e)
		case elemInitializer:
			cx.Call(e.val, this)
		}
	}
}

func (cx *ExecutionContext) defineClassElement(this Value, e *classElement) {
	private := e.key.o != nil && e.key.o.class == classPrivateName
	switch e.kind {
	case bytecode.ElemField:
		v := e.init
		if e.val != nil {
			v = cx.Call(e.val, this)
		}
		if e.dec != nil {
			for _, init := range e.dec.initializers {
				v = cx.Call(init, this, v)
			}
		}
		if private {
			cx.ensureCanAddPrivate(this, e.key.o)
			cx.definePrivate(this, e.key.o, v)
		} else {
			cx.definePublicField(this.o, e.key, v, &e.cache)
		}
		if e.dec != nil {
			for _, init := range e.dec.extra {
				cx.Call(init, this)
			}
		}
	case bytecode.ElemMethod:
		cx.definePrivate(this, e.key.o, Value{o: e.val})
	case bytecode.ElemGetter, bytecode.ElemSetter:
		if this.IsObject() {
			o := cx.privateStorage(this.o)
			if p, ok := o.shape.lookup(e.key.o); ok && p.flags&FlagAccessor != 0 {
				a := o.slots[p.slot].accessor()
				if e.kind == bytecode.ElemGetter && a.getter == nil {
					a.getter = e.val
					return
				}
				if e.kind == bytecode.ElemSetter && a.setter == nil {
					a.setter = e.val
					return
				}
			}
		}
		if e.kind == bytecode.ElemGetter {
			cx.definePrivate(this, e.key.o, newAccessor(e.val, nil))
		} else {
			cx.definePrivate(this, e.key.o, newAccessor(nil, e.val))
		}
	}
}

func (sp *space) typeofValue(v Value) Value {
	return sp.typeofs[typeOf(v)]
}

// defineField implements CreateDataPropertyOrThrow for a computed key, as object literals
// and public class fields use it.
func (cx *ExecutionContext) defineField(o *Object, k, val Value) {
	cx.CreateDataPropertyOrThrow(o, cx.ToPropertyKey(k), val)
}

// defineFieldK defines the enumerable data property key of o.
func (cx *ExecutionContext) defineFieldK(o *Object, key *Object, val Value, c *propCache) {
	if o.class == classObject && o.flags&(objNotExtensible|objIsProto|objExotic) == 0 {
		if _, ok := o.shape.lookup(key); !ok {
			old := o.shape
			cx.add(o, key, val, FlagsData)
			if !old.private && !o.shape.private {
				c.shape = old
				c.kind = cacheAdd
				c.next = o.shape
			}
			return
		}
	}
	cx.CreateDataPropertyOrThrow(o, PropKey{key: key}, val)
}

// definePublicField implements CreateDataPropertyOrThrow for the public class field k of o.
// c caches the shape transition of the field, whose key is always k.
func (cx *ExecutionContext) definePublicField(o *Object, k, val Value, c *propCache) {
	if o.class == classObject && o.flags&(objNotExtensible|objIsProto|objExotic) == 0 && o.shape == c.shape && c.kind == cacheAdd {
		if validateExec {
			cx.checkDefineHit(nil, 0, o, cx.ToPropertyKey(k).key, c.next)
		}
		o.shape = c.next
		o.appendSlot(val)
		return
	}
	if pk := cx.ToPropertyKey(k); pk.key != nil {
		cx.defineFieldK(o, pk.key, val, c)
		return
	}
	cx.defineField(o, k, val)
}

// defineMethod defines the method f under the key k of o.
func (cx *ExecutionContext) defineMethod(o *Object, k, f Value) {
	f.o.ownFunction().home = o
	cx.DefinePropertyOrThrow(o, cx.ToPropertyKey(k), dataDesc(f, FlagsHidden))
}

// defineAccessor defines the getter or setter f under the key k of o.
func (cx *ExecutionContext) defineAccessor(o *Object, k Value, f *Object, isSetter, enumerable bool) {
	f.ownFunction().home = o
	d := PropDesc{
		flags: FlagConfigurable,
		has:   HasEnumerable | HasConfigurable,
	}
	if enumerable {
		d.flags |= FlagEnumerable
	}
	if isSetter {
		d.setter = f
		d.has |= HasSet
	} else {
		d.getter = f
		d.has |= HasGet
	}
	cx.DefinePropertyOrThrow(o, cx.ToPropertyKey(k), d)
}

// funcNameOfKey returns the name that SetFunctionName gives a function for the property key or private name k.
func (cx *ExecutionContext) funcNameOfKey(k Value) wtf8.String {
	switch {
	case k.IsSymbol():
		if k.o.class == classPrivateName {
			return "#" + k.o.str
		}
		if k.o.extState() != nil {
			if len(k.o.str)+2 > MaxStringLength {
				cx.ThrowRangeError("invalid string length")
			}
			return "[" + k.o.str + "]"
		}
		return ""
	case k.IsString():
		return k.Str()
	}
	return cx.ToString(k)
}

// setFuncName implements SetFunctionName for the function or class f, which must be anonymous.
func (cx *ExecutionContext) setFuncName(f, k Value, prefix int) {
	if !f.IsCallable() {
		return
	}
	name := cx.funcNameOfKey(k)
	switch prefix {
	case bytecode.NamePrefixGet:
		name = "get " + name
	case bytecode.NamePrefixSet:
		name = "set " + name
	}
	if len(name) > MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	o := f.o
	if o.fn.materialized {
		if _, ok := o.shape.lookup(cx.Key(KeyName)); ok {
			if d, _ := cx.GetOwnProperty(o, PropKey{key: cx.Key(KeyName)}); d.flags&FlagAccessor != 0 || !d.value.IsString() || d.value.rawString() != "" {
				return
			}
		}
		cx.defineOwnProperty(o, PropKey{key: cx.Key(KeyName)}, dataDesc(cx.NewString(name), FlagConfigurable))
		return
	}
	// The name is part of the code of an unmaterialized function, which closures share,
	// so give this one its properties now.
	cx.materialize(o)
	cx.Define(o, cx.Key(KeyName), cx.NewString(name), FlagConfigurable)
}

// copyDataProperties implements CopyDataProperties.
func (cx *ExecutionContext) copyDataProperties(dst *Object, src Value, excluded []Value) {
	if src.IsNullish() {
		return
	}
	from := cx.ToObject(src)
	if len(excluded) == 0 && takeDataProperties(dst, from) {
		return
	}
	var skip []PropKey
	for _, e := range excluded {
		skip = append(skip, cx.ToPropertyKey(e))
	}
	ks := cx.OwnKeys(from)
	if dst.flags&objExotic == 0 {
		// The keys of the elements come first, and the other keys usually get slots.
		elems := 0
		for elems < len(ks) && ks[elems].isIndex() {
			elems++
		}
		dst.reserveSlots(len(ks) - elems)
	}
	var pacer Pacer
keys:
	for _, k := range ks {
		pacer.Step(cx)
		for _, s := range skip {
			if s == k {
				continue keys
			}
		}
		d, ok := cx.GetOwnProperty(from, k)
		if !ok || d.flags&FlagEnumerable == 0 {
			continue
		}
		val := d.value
		if d.flags&FlagAccessor != 0 || from.class == classProxy {
			val = cx.getProp(from, k, Value{o: from})
		}
		cx.CreateDataPropertyOrThrow(dst, k, val)
	}
}

// takeDataProperties gives the empty ordinary object dst the properties of from and reports
// true when creating them on dst one by one would give dst the shape and the elements of from.
// Otherwise it does nothing and reports false.
func takeDataProperties(dst, from *Object) bool {
	s := takenShape(dst, from)
	if s == nil || slices.ContainsFunc(from.elems, Value.isHole) {
		return false
	}
	dst.shape = s
	dst.fillSlots(from.slots[:s.count])
	// The keys of the elements come first, in index order, so the elements of dst are those of
	// from.
	if len(from.elems) != 0 {
		dst.initElems(slices.Clone(from.elems))
	}
	return true
}

// AssignDataProperties copies the own enumerable properties of from to the empty object to as
// Object.assign does, when that is known to run no code, and reports whether it did.
func (q QuietContext) AssignDataProperties(to, from *Object) bool {
	s := takenShape(to, from)
	if s == nil || len(from.elems) != 0 {
		return false
	}
	// Set creates an own data property when the prototype lookup finds a writable data property or no property.
	for t := s; t.parent != nil; t = t.parent {
		switch r := q.lookupNamed(to.shape.proto, t.key); r.kind {
		case lookupAbsent:
		case lookupSlot:
			if r.prop.flags&(FlagAccessor|FlagWritable) != FlagWritable {
				return false
			}
		default:
			return false
		}
	}
	to.shape = s
	to.fillSlots(from.slots[:s.count])
	return true
}

// takenShape returns the shape that dst, an empty ordinary object, would have after creating
// the properties of from other than its elements on it one by one, when that shape is the shape
// of from and from has only enumerable, writable and configurable data properties with string
// keys besides its elements. Otherwise it returns nil.
func takenShape(dst, from *Object) *shape {
	if dst.kind != kindObject || dst.class != classObject || dst.flags&(objNotExtensible|objIsProto|objExotic|objSparse) != 0 || dst.shape.count != 0 || len(dst.elems) != 0 {
		return nil
	}
	// An ordinary object that is not sparse has the properties with array index keys in its
	// elements, and the others in insertion order, which is the order of its slots.
	if from.kind != kindObject || from.class != classObject || from.flags&(objExotic|objSparse) != 0 {
		return nil
	}
	s := from.shape
	if s.private || s.flatBased || s.count == 0 {
		return nil
	}
	t := s
	for ; t.parent != nil; t = t.parent {
		if t.flags != FlagsData || t.key.kind != kindString {
			return nil
		}
	}
	// The root fixes the prototype, so the shape of dst would be reached through the same
	// transitions.
	if t != dst.shape {
		return nil
	}
	return s
}

// setupClass makes the function ctor a class constructor that extends parent,
// which is hole for a class without an extends clause. It returns the prototype object.
func (cx *ExecutionContext) setupClass(ctor *Object, parent Value) *Object {
	protoParent := cx.Intrinsic(IntrinsicObjectPrototype)
	ctorParent := cx.Intrinsic(IntrinsicFunctionPrototype)
	if !parent.isHole() {
		switch {
		case parent.IsNull():
			protoParent = nil
		case !parent.IsCallable() || !cx.IsConstructor(parent.o):
			cx.ThrowTypeError("class extends value " + cx.DisplayString(parent) + " is not a constructor or null")
		default:
			p := cx.GetV(parent, cx.Key(KeyPrototype))
			switch {
			case p.IsObject():
				protoParent = p.o
			case p.IsNull():
				protoParent = nil
			default:
				cx.ThrowTypeError("class extends value does not have valid prototype property")
			}
			ctorParent = parent.o
		}
	}
	proto := cx.NewObject(protoParent)
	fn := ctor.ownFunction()
	fn.materialized = true
	fn.home = proto
	ctor.flags &^= objExotic
	ctor.shape = cx.shapeFor(ctorParent)
	cx.add(ctor, cx.Key(KeyLength), Number(float64(fn.code.length)), FlagConfigurable)
	cx.add(ctor, cx.Key(KeyName), cx.NewString(fn.code.name), FlagConfigurable)
	cx.add(ctor, cx.Key(KeyPrototype), Value{o: proto}, 0)
	cx.add(proto, cx.Key(KeyConstructor), Value{o: ctor}, FlagsHidden)
	return proto
}

// IsConstructor implements IsConstructor.
func (cx *ExecutionContext) IsConstructor(f *Object) bool {
	fn := f.fn
	if fn == nil {
		return false
	}
	if fn.bound != nil {
		return cx.IsConstructor(fn.bound.target)
	}
	return fn.constructor
}

// closurePrototype returns the prototype of the closures of p, which is a generator or async function.
func (p *funcProto) closurePrototype() *Object {
	switch {
	case p.isGenerator && p.isAsync:
		return p.context.Intrinsic(IntrinsicAsyncGeneratorFunctionPrototype)
	case p.isGenerator:
		return p.context.Intrinsic(IntrinsicGeneratorFunctionPrototype)
	case p.isAsync:
		return p.context.Intrinsic(IntrinsicAsyncFunctionPrototype)
	}
	return p.context.Intrinsic(IntrinsicFunctionPrototype)
}

// arrayProtoPlain reports whether no array index property exists on the prototypes of plain arrays,
// so that storing a new element in such an array cannot run a setter.
func (cx *ExecutionContext) arrayProtoPlain() bool {
	a := cx.Intrinsic(IntrinsicArrayPrototype)
	o := cx.Intrinsic(IntrinsicObjectPrototype)
	return len(a.elems) == 0 && len(o.elems) == 0 && (a.flags|o.flags)&objSparse == 0 && a.shape.proto == o
}

// arrayIterationPlain reports whether o uses the original array values and next methods.
// Reading an element can still invoke a getter.
func (cx *ExecutionContext) arrayIterationPlain(o *Object) bool {
	if validateExec {
		cx.checkPlainArray(o)
	}
	return o.shape == cx.realm.shapes.array && cx.arrayIterationUnmodified(cx.realmPart)
}

// arrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (ag *agent) arrayIterationUnmodified(r *realmPart) bool {
	c := &r.arrayIter
	if c.epoch != ag.protoEpoch || !c.checked {
		c.checked = true
		c.epoch = ag.protoEpoch
		q := r.executionContext().Quiet()
		c.iter = q.lookupDataSlot(r.intrinsics[IntrinsicArrayPrototype], ag.WellKnownSymbol(SymbolIterator))
		c.next = q.lookupDataSlot(r.intrinsics[IntrinsicArrayIteratorPrototype], ag.Key(KeyNext))
		c.found = c.iter.holder != nil && c.next.holder != nil
		c.noReturn = ag.noArrayIteratorReturn(r.intrinsics[IntrinsicArrayIteratorPrototype])
	}
	if validateExec {
		ag.checkArrayIteration(r)
	}
	// Assigning to an existing data property does not move protoEpoch, so the values are
	// compared on every call.
	return c.found && c.iter.value().o == r.intrinsics[IntrinsicArrayPrototypeValues] &&
		c.next.value().o == r.intrinsics[IntrinsicArrayIteratorPrototypeNext]
}

// pairDestructuringPlain reports whether destructuring a new array of two elements into two
// bindings runs nothing that JavaScript could observe.
func (ag *agent) pairDestructuringPlain(r *realmPart) bool {
	return ag.arrayIterationUnmodified(r) && r.arrayIter.noReturn
}

// noArrayIteratorReturn reports whether array iterators have no return method, which closing
// one calls. The prototypes that it looks at count as prototypes from then on, so that adding a
// property to one moves protoEpoch.
func (ag *agent) noArrayIteratorReturn(proto *Object) bool {
	key := ag.Key(KeyReturn)
	for p := proto; p != nil; p = p.shape.proto {
		p.flags |= objIsProto
		if _, ok := p.getOwn(key); ok || p.flags&objExotic != 0 {
			return false
		}
	}
	return true
}

// arrayIterCache is what arrayIterationPlain found for arrays with the initial shape.
type arrayIterCache struct {
	// iter and next are the data properties that iterating such an array reads.
	iter dataSlot
	next dataSlot

	// epoch is the value of agent.protoEpoch at which iter and next were found.
	epoch   uint32
	checked bool
	// found reports whether iter and next were found as data properties.
	found bool
	// noReturn reports whether array iterators had no return method at epoch.
	noReturn bool
}

// dataSlot is the slot of a data property in the object that holds it.
type dataSlot struct {
	holder *Object
	slot   int32
}

// lookupDataSlot returns the named data slot on o or its prototypes, or a zero slot
// if the lookup cannot establish one without running code.
func (q QuietContext) lookupDataSlot(o, key *Object) dataSlot {
	r := q.lookupNamed(o, key)
	if r.kind != lookupSlot || r.prop.flags&FlagAccessor != 0 {
		return dataSlot{}
	}
	return dataSlot{
		holder: r.holder,
		slot:   r.prop.slot,
	}
}

// value returns the current value of the slot.
func (d *dataSlot) value() Value {
	return d.holder.slots[d.slot]
}

// Call calls the callable object f.
func (cx *ExecutionContext) Call(f *Object, this Value, args ...Value) Value {
	return cx.callOnStack(f, this, args)
}

// CallValue calls f, which must be callable, or throws a TypeError.
func (cx *ExecutionContext) CallValue(f, this Value, args ...Value) Value {
	if !f.IsCallable() {
		cx.ThrowTypeError(cx.DisplayString(f) + " is not a function")
	}
	return cx.callOnStack(f.o, this, args)
}

// callOnStack calls f with a copy of args on the agent stack, so that args does not escape.
func (cx *ExecutionContext) callOnStack(f *Object, this Value, args []Value) Value {
	base := cx.sp
	n := len(args)
	if fn := f.fn; fn != nil && fn.bound == nil && fn.info == nil && !fn.classConstructor &&
		!fn.code.isGenerator && !fn.code.isAsync && !cx.interrupt.Load() && len(cx.frames) < maxFrames {
		// A plain closure gets its arguments written once, where its frame starts.
		cx.ensureStack(base + n + fn.code.nregs + 1)
		copy(cx.stack[base:], args)
		cx.pushFrame(f, this, base, n, 0, false, nil)
		r := cx.run(len(cx.frames) - 1)
		cx.sp = base
		return r
	}
	cx.ensureStack(base + n)
	copy(cx.stack[base:], args)
	cx.sp = base + n
	var r Value
	if fn := f.fn; fn != nil && fn.info != nil && !cx.interrupt.Load() {
		// A native function needs none of the other checks of callSlow.
		r = cx.callNative(f, this, cx.stack[base:base+n:base+n], nil)
	} else {
		r = cx.callSlow(f, this, cx.stack[base:base+n:base+n], false, nil)
	}
	cx.sp = base
	return r
}

// callSlow calls f with any kind of callee. With isNew set, this is the object under construction.
func (cx *ExecutionContext) callSlow(f *Object, this Value, args []Value, isNew bool, newTarget *Object) Value {
	if validateExec {
		defer cx.releaseArgs(cx.holdArgs(args))
	}
	if cx.interrupt.Load() {
		cx.Poll()
	}
	fn := f.fn
	if fn == nil {
		cx.ThrowTypeError(cx.DisplayString(Value{o: f}) + " is not a function")
	}
	if fn.bound != nil {
		return cx.callBound(f, args)
	}
	if fn.info != nil {
		return cx.callNative(f, this, args, nil)
	}
	c := fn.code
	if fn.classConstructor && !isNew && newTarget == nil {
		c.context.ThrowTypeError("class constructor " + wtf8.ToValidUTF8(c.name) + " cannot be invoked without 'new'")
	}
	if c.isGenerator || c.isAsync {
		return cx.callCoroutine(f, this, args)
	}
	if len(cx.frames) >= maxFrames {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	base := cx.sp
	cx.ensureStack(base + len(args) + c.nregs + 1)
	copy(cx.stack[base:], args)
	cx.pushFrame(f, this, base, len(args), 0, isNew, newTarget)
	r := cx.run(len(cx.frames) - 1)
	cx.sp = base
	return r
}

// callBound calls the bound function f, unwrapping a chain of bound functions without recursion.
func (cx *ExecutionContext) callBound(f *Object, args []Value) Value {
	var this Value
	base := cx.sp
	for b := f.fn.bound; b != nil; b = f.fn.bound {
		if k := len(b.args); k > 0 {
			// The combined arguments go on the stack above its top. args may start at the top,
			// so it moves up before the bound arguments are written.
			top := cx.sp
			n := k + len(args)
			cx.ensureStack(top + n)
			s := cx.stack[top : top+n : top+n]
			copy(s[k:], args)
			copy(s, b.args)
			cx.sp = top + n
			args = s
			if validateExec {
				defer cx.releaseArgs(cx.holdArgs(args))
			}
		}
		f, this = b.target, b.this
	}
	r := cx.callSlow(f, this, args, false, nil)
	cx.sp = base
	return r
}

// Construct implements Construct: new f(...args) with the given new.target.
func (cx *ExecutionContext) Construct(f *Object, args []Value, newTarget *Object) *Object {
	if validateExec {
		defer cx.releaseArgs(cx.holdArgs(args))
	}
	if cx.interrupt.Load() {
		cx.Poll()
	}
	fn := f.fn
	for b := fn.bound; b != nil; b = fn.bound {
		if len(b.args) > 0 {
			args = append(append(make([]Value, 0, len(b.args)+len(args)), b.args...), args...)
		}
		if newTarget == f {
			newTarget = b.target
		}
		f = b.target
		fn = f.fn
	}
	if fn.info != nil {
		r := cx.callNative(f, Undefined, args, newTarget)
		if !r.IsObject() {
			cx.ThrowTypeError("constructor did not return an object")
		}
		return r.o
	}
	this := tdz
	if !fn.derived {
		proto := cx.GetPrototypeFromConstructor(newTarget, IntrinsicObjectPrototype)
		this = Value{o: newInstance(cx.shapeForRealm(proto, fn.code.context.realmPart))}
	}
	r := cx.callSlow(f, this, args, !fn.derived, newTarget)
	if !r.IsObject() {
		cx.ThrowTypeError("constructor did not return an object")
	}
	return r.o
}

// GetPrototypeFromConstructor implements GetPrototypeFromConstructor.
// A nil ctor selects defaultProto in the current realm.
func (cx *ExecutionContext) GetPrototypeFromConstructor(ctor *Object, defaultProto Intrinsic) *Object {
	r := cx.realm
	if ctor != nil {
		if f := ctor.fn; f != nil && f.info != nil && f.info.fixedPrototype {
			return f.info.realm.intrinsics[f.info.prototype]
		}
		if p := cx.GetV(Value{o: ctor}, cx.Key(KeyPrototype)); p.IsObject() {
			return p.o
		}
		r = cx.FunctionContext(ctor).realm
	}
	if IntrinsicTemporalInstantPrototype <= defaultProto && defaultProto <= IntrinsicIntlSegmenterPrototype {
		ns := r.intrinsics[IntrinsicTemporal]
		if defaultProto >= IntrinsicIntlCollatorPrototype {
			ns = r.intrinsics[IntrinsicIntl]
		}
		if ns != nil {
			cx.InitLazyObject(ns)
		}
	}
	if proto := r.intrinsics[defaultProto]; proto != nil {
		return proto
	}
	panic("engine: uninitialized intrinsic prototype " + strconv.Itoa(int(defaultProto)))
}

// NewNative returns a function object of the native function f.
func (cx *ExecutionContext) NewNative(name wtf8.String, length int, f Native) *Object {
	return cx.NewNativeWithState(name, length, f, nil)
}

// NewNativeWithState returns a function object of the native function f whose internal slots are state.
// f finds them with [agent.ActiveNativeState].
func (cx *ExecutionContext) NewNativeWithState(name wtf8.String, length int, f Native, state any) *Object {
	n := &nativeObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: cx.realm.shapes.function,
		ext:   state,
		info: nativeInfo{
			native: f,
			name:   name,
			length: int32(length),
			fast:   f.fast(),
			realm:  cx.realmPart,
		},
	}
	n.fn = &n.state
	n.state.info = &n.info
	return n.init()
}

// ShortcutFrame states how a shortcut accounts for the frame that calls of its function show.
type ShortcutFrame uint8

const (
	// FrameUndeclared is not a valid statement.
	FrameUndeclared ShortcutFrame = iota

	// FrameUnobservable says that nothing that the shortcut does can observe the frame: it
	// neither throws nor runs JavaScript or host code, and declines where the function may.
	FrameUnobservable

	// FrameRecorded says that the selected method records its frame around what can observe it.
	FrameRecorded
)

// ShortcutPace states how a shortcut accounts for the interruptions that its function takes.
type ShortcutPace uint8

const (
	// PaceUndeclared is not a valid statement.
	PaceUndeclared ShortcutPace = iota

	// PaceShort says that the function takes no interruption where the shortcut does not
	// decline: it does a bounded number of operations on its operands.
	PaceShort

	// PacePolled says that the shortcut can run for a time that depends on the script, and
	// checks for an interruption as often as the function.
	PacePolled
)

// ShortcutContract states what the forms of a native function that its callers use instead of
// calling it preserve of the call. The function shows a frame.
type ShortcutContract struct {
	Frame ShortcutFrame
	Pace  ShortcutPace
}

// NumberKernel1 and NumberKernel2 compute a function of one or two numbers. They neither throw
// nor run code.
type (
	NumberKernel1 func(x float64) float64
	NumberKernel2 func(x, y float64) float64
)

// Extremum identifies Math.min or Math.max for [agent.SetExtremum].
type Extremum uint8

const (
	ExtremumNone Extremum = iota
	ExtremumMin
	ExtremumMax
)

// SetExtremum says that the native function f is Math.min or Math.max, as e names. The contract of
// its number kernels holds for what the interpreter computes.
func (ag *agent) SetExtremum(f *Object, e Extremum) {
	f.fn.changeableInfo().extremum = e
}

// SetFrameless makes calls of the native function f show no frame: stack traces leave them out,
// and the caller of a function that f calls is the one that called f.
func (ag *agent) SetFrameless(f *Object) {
	f.fn.changeableInfo().frameless = true
}

// MethodKernel0, MethodKernel1 and MethodKernel2 compute a function for its this value and no,
// one or two arguments. They neither throw nor run code, and report false when the function has
// to be called instead.
type (
	MethodKernel0 func(q QuietContext, this Value) (Value, bool)
	MethodKernel1 func(q QuietContext, this, x Value) (Value, bool)
	MethodKernel2 func(q QuietContext, this, x, y Value) (Value, bool)
)

// nativeObject is a native function object together with its function state and description,
// so that creating one is a single allocation.
type nativeObject struct {
	propertyObject
	state function
	info  nativeInfo
}

// NewNativeConstructor returns a function object of the native function f that can be called with new.
// f learns the constructor it is constructing for from [agent.GetNewTarget].
func (cx *ExecutionContext) NewNativeConstructor(name wtf8.String, length int, f Native) *Object {
	o := cx.NewNative(name, length, f)
	o.fn.constructor = true
	return o
}

// DefineConstructorPrototype defines the property prototype of the constructor c, which
// NewNativeConstructor returned in the current realm, as the intrinsic id, which the property
// then holds for good.
func (cx *ExecutionContext) DefineConstructorPrototype(c *Object, id Intrinsic) {
	cx.Define(c, cx.Key(KeyPrototype), Value{o: cx.Intrinsic(id)}, 0)
	info := c.fn.changeableInfo()
	info.prototype, info.fixedPrototype = id, true
}

// GetNewTarget implements GetNewTarget for the running native function: it returns the
// constructor the function is being constructed for, or nil when it was called.
func (ag *agent) GetNewTarget() *Object {
	if n := len(ag.natives); n > 0 {
		return ag.natives[n-1].newTarget
	}
	return nil
}

// activeNative returns the active function object of the running native code, which is a native
// function or a proxy. It is another one while that code calls functions.
func (ag *agent) activeNative() *Object {
	return ag.natives[len(ag.natives)-1].f
}

// ActiveNativeState returns the internal slots of the active native function.
func (ag *agent) ActiveNativeState() any {
	return ag.activeNative().extState()
}

// findScope returns the innermost of the depth innermost dynamic scopes of the running
// function cl that has the variable key, or nil.
func (cx *ExecutionContext) findScope(cl *Object, key *Object, depth int) *Object {
	k := PropKey{key: key}
	for _, scopes := range [...][]*Object{cx.frames[len(cx.frames)-1].scopes, cl.fn.capturedScopes()} {
		for _, o := range slices.Backward(scopes) {
			if depth == 0 {
				return nil
			}
			depth--
			if !cx.HasProperty(o, k) {
				continue
			}
			if o.class == classEvalScope {
				return o
			}
			if u := cx.getProp(o, PropKey{key: cx.WellKnownSymbol(SymbolUnscopables)}, Value{o: o}); u.IsObject() {
				if ToBoolean(cx.getProp(u.o, k, u)) {
					continue
				}
			}
			return o
		}
	}
	return nil
}

// capturedScopes returns the dynamic scopes that were open where f was created, innermost last.
func (f *function) capturedScopes() []*Object {
	if f.scopes == nil {
		return nil
	}
	return *f.scopes
}

// openScopes returns a copy of the dynamic scopes that are open in the running function cl,
// innermost last, or nil when none are.
func (ag *agent) openScopes(cl *Object) *[]*Object {
	scopes := slices.Concat(cl.fn.capturedScopes(), ag.frames[len(ag.frames)-1].scopes)
	if len(scopes) == 0 {
		return nil
	}
	return &scopes
}

// getScoped implements GetBindingValue of the object environment record of the dynamic scope o.
func (cx *ExecutionContext) getScoped(o *Object, key *Object, strict bool) Value {
	k := PropKey{key: key}
	if !cx.HasProperty(o, k) {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
		return Undefined
	}
	return cx.getProp(o, k, Value{o: o})
}

// setScoped writes to a dynamic scope or performs PutValue on an unresolvable reference.
func (cx *ExecutionContext) setScoped(o *Object, key *Object, val Value, strict bool) {
	k := PropKey{key: key}
	if o == nullObject {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
		r := cx.realm
		cx.setProp(r.global, k, val, Value{o: r.globalThis})
		return
	}
	if !cx.HasProperty(o, k) && strict {
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	}
	if !cx.setProp(o, k, val, Value{o: o}) && strict {
		cx.throwReadOnly(Value{o: o}, k)
	}
}

// deleteName deletes the global variable key.
func (cx *ExecutionContext) deleteName(key *Object) bool {
	r := cx.realm
	if _, ok := r.globalLex.shape.lookup(key); ok {
		return false
	}
	g := r.global
	if p, ok := g.shape.lookup(key); ok {
		return cx.deleteOwnSlot(g, key, p)
	}
	// An inherited HasBinding check can create a lexical binding or an own global property.
	k := PropKey{key: key}
	if !cx.Quiet().delegates(g) || !cx.HasProperty(g, k) {
		return true
	}
	if _, ok := r.globalLex.shape.lookup(key); ok {
		return false
	}
	return cx.DeleteProp(g, k)
}

// BoundFunctionCreate implements BoundFunctionCreate. It copies args.
func (cx *ExecutionContext) BoundFunctionCreate(target *Object, this Value, args []Value) *Object {
	b := &boundObject{
		bound: boundFunc{
			target: target,
			this:   this,
			args:   append([]Value(nil), args...),
		},
	}
	b.fn = &b.state
	b.state = function{
		bound:        &b.bound,
		materialized: true,
		// The state of a bound target already says whether its own target is a constructor.
		constructor: target.fn.constructor,
	}
	b.kind = kindFunction
	b.class = classFunction
	b.shape = cx.shapeForRealm(cx.ProtoOf(target), target.shape.realm)
	b.slots = b.props[:0]
	b.pinned = pinnedSlots
	return b.init()
}

// boundObject is a bound function object together with its state and the storage of its
// length and name properties, so that creating one is a single allocation.
type boundObject struct {
	propertyObject
	state function
	bound boundFunc
	props [2]Value
}

// SourceText returns the result of Function.prototype.toString for the function f.
func (f *Object) SourceText() wtf8.String {
	fn := f.fn
	if fn.code != nil && fn.code.Source != "" {
		return fn.code.Source
	}
	name := fn.nativeName()
	if fn.code != nil {
		name = fn.code.name
	}
	// The name is a PropertyName after an optional accessor word, and is left out when it is not.
	n := strings.TrimPrefix(strings.TrimPrefix(string(name), "get "), "set ")
	if !parser.IsIdentifierName(n) && !strings.HasPrefix(n, "[") {
		name = ""
	}
	return "function " + name + "() { [native code] }"
}
