// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func (cx *ExecutionContext) throwTDZ(name *Object) {
	if name.Text == "this" {
		cx.ThrowReferenceError("must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	cx.ThrowReferenceError("cannot access '" + wtf8.ToValidUTF8(name.Text) + "' before initialization")
}

func (cx *ExecutionContext) throwReadOnly(v Value, k PropKey) {
	cx.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' of " + cx.DisplayString(v))
}

// callAt returns the index of the call instruction of fp that ends before pc, past its extra.
func callAt(fp *funcProto, pc int) int {
	pc--
	for fp.Code[pc].Op == bytecode.OpExtra {
		pc--
	}
	return pc
}

// calleeName names the function that the call instruction at pc calls, for an error message.
func (ag *agent) calleeName(fp *funcProto, pc int) wtf8.String {
	target := fp.Code[pc].B
	for j := pc - 1; j >= 0 && j >= pc-64; j-- {
		in := fp.Code[j]
		if in.Op == bytecode.OpExtra || in.A != target {
			continue
		}
		switch in.Op {
		case bytecode.OpGetProp, bytecode.OpGetPropChain, bytecode.OpGetMethod:
			return fp.Keys[in.C].Text
		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict:
			return fp.Keys[in.B].Text
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
		name = cx.DisplayString(regs[fp.Code[pc].B])
	}
	cx.ThrowTypeError(name + " is not a function")
}

// throwNotConstructor throws the TypeError for the call instruction of fp that ends before pc,
// whose callee is not a constructor.
func (cx *ExecutionContext) throwNotConstructor(fp *funcProto, pc int, regs []Value) {
	pc = callAt(fp, pc)
	name := wtf8.ToValidUTF8(cx.calleeName(fp, pc))
	if name == "" {
		name = cx.DisplayString(regs[fp.Code[pc].B])
	}
	cx.ThrowTypeError(name + " is not a constructor")
}

// getGlobal reads the global variable key, as strict code does when strict is set. When it does
// not exist, it throws a ReferenceError unless orUndef is set.
func (cx *ExecutionContext) getGlobal(key *Object, c *globalCache, orUndef, strict bool) Value {
	r := cx.realmPart
	g := r.Global
	if v, ok := cx.getGlobalLexical(key, c); ok {
		return v
	}
	if p, ok := g.Shape.Lookup(key); ok && p.Flags&FlagAccessor == 0 {
		c.Holder = g
		c.Shape = g.Shape
		c.Ref = &g.Slots[p.Slot]
		return g.Slots[p.Slot]
	}
	switch l := quiet.ContextOf(r).LookupNamed(g, key); l.Kind {
	case lookupAbsent, lookupNumericMiss:
		if orUndef {
			return storage.Undefined
		}
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
	case lookupSlot:
		v, getter := cx.ReadNamedSlot(g, l.Holder, l.Prop, nil)
		if getter != nil {
			return cx.Call(getter, Value{O: r.GlobalThis})
		}
		return v
	}
	k := storage.NamedKey(key)
	if !cx.HasProperty(g, k) {
		if orUndef {
			return storage.Undefined
		}
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
	}
	// HasBinding can create a lexical binding or remove the property or its prototype.
	// GetBindingValue selects the binding again in the same global environment.
	if v, ok := cx.getGlobalLexical(key, c); ok {
		return v
	}
	if !cx.HasProperty(g, k) {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
		}
		return storage.Undefined
	}
	return cx.getProp(g, k, Value{O: r.GlobalThis})
}

func (cx *ExecutionContext) getGlobalLexical(key *Object, c *globalCache) (Value, bool) {
	r := cx.realmPart
	p, ok := r.GlobalLex.Shape.Lookup(key)
	if !ok {
		return storage.Undefined, false
	}
	slot := &r.GlobalLex.Slots[p.Slot]
	if slot.O == tdzObject {
		cx.throwTDZ(key)
	}
	// Only initialized bindings enter the cache, and they stay initialized.
	c.Holder = r.Global
	c.Shape = r.Global.Shape
	c.Ref = slot
	return *slot, true
}

// resolveGlobal returns undefined for a global binding and null for an unresolvable name.
func (cx *ExecutionContext) resolveGlobal(key *Object, c *globalCache) Value {
	g := cx.realm.Global
	_, lex := cx.realm.GlobalLex.Shape.Lookup(key)
	if _, own := g.Shape.Lookup(key); lex || own {
		if c != nil {
			c.Holder = g
			c.Shape = g.Shape
		}
		return storage.Undefined
	}
	if cx.HasProperty(g, storage.NamedKey(key)) {
		return storage.Undefined
	}
	return storage.Null
}

// setGlobal writes a resolved binding in the global environment.
func (cx *ExecutionContext) setGlobal(key *Object, val Value, c *globalCache, strict bool) {
	r := cx.realm
	g := r.Global
	if p, ok := r.GlobalLex.Shape.Lookup(key); ok {
		if r.GlobalLex.Slots[p.Slot].O == tdzObject {
			cx.throwTDZ(key)
		}
		if p.Flags&FlagWritable == 0 {
			cx.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(key.Text) + "'")
		}
		r.GlobalLex.Slots[p.Slot] = val
		// As in getGlobal, c refers only to initialized bindings.
		c.Holder = g
		c.Shape = g.Shape
		c.Ref = &r.GlobalLex.Slots[p.Slot]
		return
	}
	if p, ok := g.Shape.Lookup(key); ok && p.Flags&(FlagAccessor|FlagWritable) == FlagWritable {
		g.Slots[p.Slot] = val
		c.Holder = g
		c.Shape = g.Shape
		c.Ref = &g.Slots[p.Slot]
		return
	}
	k := storage.NamedKey(key)
	// SetMutableBinding checks whether the binding still exists. A proxy can observe
	// the check even in sloppy code.
	if !cx.HasProperty(g, k) && strict {
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
	}
	if !cx.setProp(g, k, val, Value{O: r.GlobalThis}) && strict {
		cx.throwReadOnly(Value{O: g}, k)
	}
}

// checkGlobalDecl throws when declaring key at the top level of a script would conflict
// with an existing declaration. what is a bytecode Decl kind.
func (cx *ExecutionContext) checkGlobalDecl(key *Object, what int) {
	r := cx.realm
	_, lex := r.GlobalLex.Shape.Lookup(key)
	switch what {
	case bytecode.DeclVar:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.Text) + "' has already been declared")
		}
		if !cx.HasOwnProperty(r.Global, storage.NamedKey(key)) && !cx.IsExtensible(r.Global) {
			cx.ThrowTypeError("cannot declare global variable '" + wtf8.ToValidUTF8(key.Text) + "'")
		}
	case bytecode.DeclLexical:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.Text) + "' has already been declared")
		}
		// A lexical declaration may not hide a global property that cannot be reconfigured.
		if d, ok := cx.GetOwnProperty(r.Global, storage.NamedKey(key)); ok && !d.Configurable() {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.Text) + "' has already been declared")
		}
	case bytecode.DeclFunction:
		if lex {
			cx.ThrowSyntaxError("identifier '" + wtf8.ToValidUTF8(key.Text) + "' has already been declared")
		}
		d, ok := cx.GetOwnProperty(r.Global, storage.NamedKey(key))
		switch {
		case !ok:
			if !cx.IsExtensible(r.Global) {
				cx.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.Text) + "'")
			}
		case d.Configurable():
		case d.IsAccessor() || !d.Writable() || !d.Enumerable():
			cx.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.Text) + "'")
		}
	}
}

// declareGlobalVar implements CreateGlobalVarBinding, and CreateGlobalFunctionBinding when
// flags has [bytecode.GlobalVarFunc]. flags is a set of the bytecode GlobalVar flags.
func (cx *ExecutionContext) declareGlobalVar(key *Object, flags int, fn Value) {
	g := cx.realm.Global
	k := storage.NamedKey(key)
	d, ok := cx.GetOwnProperty(g, k)
	attrs := FlagEnumerable | FlagWritable
	if flags&bytecode.GlobalVarDeletable != 0 {
		attrs |= FlagConfigurable
	}
	if flags&bytecode.GlobalVarFunc == 0 {
		if !ok {
			cx.DefinePropertyOrThrow(g, k, storage.DataDesc(storage.Undefined, attrs))
		}
		return
	}
	if !ok || d.Configurable() {
		cx.DefinePropertyOrThrow(g, k, storage.DataDesc(fn, attrs))
		return
	}
	cx.DefinePropertyOrThrow(g, k, storage.ValueDesc(fn))
}

// declareGlobalAnnexB declares the global variable key of a block-level function of sloppy
// code, as Annex B.3.2.2 and B.3.2.3 do, and reports whether it is declared. It declares
// nothing when a global lexical declaration of key exists or CanDeclareGlobalVar(key) is false.
// flags is a set of the bytecode GlobalVar flags.
func (cx *ExecutionContext) declareGlobalAnnexB(key *Object, flags int) bool {
	r := cx.realm
	if _, lex := r.GlobalLex.Shape.Lookup(key); lex {
		return false
	}
	if !cx.HasOwnProperty(r.Global, storage.NamedKey(key)) && !cx.IsExtensible(r.Global) {
		return false
	}
	cx.declareGlobalVar(key, flags&^bytecode.GlobalVarFunc, storage.Undefined)
	return true
}

// declareGlobalLex creates the uninitialized global lexical binding key.
func (cx *ExecutionContext) declareGlobalLex(key *Object, isConst bool) {
	r := cx.realm
	flags := FlagsData
	if isConst {
		flags = FlagEnumerable | FlagConfigurable
	}
	cx.Add(r.GlobalLex, key, tdz, flags)
	// The binding may hide a property of the global object, and the bindings may have moved,
	// so the global variable caches must not apply any longer.
	r.Global.RenewShape()
}

func (cx *ExecutionContext) initGlobalLex(key *Object, val Value) {
	r := cx.realm
	p, _ := r.GlobalLex.Shape.Lookup(key)
	r.GlobalLex.Slots[p.Slot] = val
}

// getValue returns v[k], or the getter to call.
func (cx *ExecutionContext) getValue(v, k Value) (Value, *Object) {
	if ko := k.O; ko == storage.NumberObject {
		if i, ok := storage.NumberIndex(k.N); ok {
			if o := v.O; o != nil && o.Class == classTypedArray {
				return storage.TypedArrayGet(o, i), nil
			}
			return cx.getIndex(v, i)
		}
	} else if ko != nil && ko.Kind == kindString {
		// An element in elems is an own data property, whatever object v is.
		if i, ok := ko.KnownArrayIndex(); ok {
			if o := v.O; o != nil && o.ObjectProperties != nil && int64(i) < int64(len(o.Elems)) {
				if e := o.Elems[i]; !e.IsHole() {
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
	if key.Key == nil {
		return cx.getIndex(v, key.Idx)
	}
	if key.Idx == missingKey {
		return cx.getMissing(v, key)
	}
	return cx.getNamed(v, key.Key, nil)
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
	if ko := k.O; ko == storage.NumberObject {
		if i, ok := storage.NumberIndex(k.N); ok {
			if o := v.O; o != nil && o.Class == classTypedArray {
				return nil, cx.typedArraySet(o, i, val), storage.IndexKey(i)
			}
			setter, ok := cx.setIndex(v, i, val)
			return setter, ok, storage.IndexKey(i)
		}
	} else if ko != nil && ko.Kind == kindString {
		// An element in elems is a writable own data property, whatever object v is.
		if i, ok := ko.KnownArrayIndex(); ok {
			if o := v.O; o != nil && o.ObjectProperties != nil && int64(i) < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
				o.Elems[i] = val
				return nil, true, PropKey{}
			}
		}
	}
	if v.IsNullish() {
		cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + cx.DisplayString(k) + "')")
	}
	key := cx.ToPropertyKey(k)
	if key.Key == nil {
		setter, ok := cx.setIndex(v, key.Idx, val)
		return setter, ok, key
	}
	setter, ok := cx.setNamed(v, key.Key, val, nil)
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
	return storage.Bool(ok)
}

// superBase implements GetSuperBase for the home object home.
func (cx *ExecutionContext) superBase(home Value) Value {
	if !home.IsObject() {
		cx.ThrowSyntaxError("'super' keyword unexpected here")
	}
	if p := cx.ProtoOf(home.O); p != nil {
		return storage.ObjectValue(p)
	}
	return storage.Null
}

// superObject returns the object that a super property access with the super base base starts from.
func (cx *ExecutionContext) superObject(base Value) *Object {
	if base.IsNull() {
		cx.ThrowTypeError("cannot access a property of a null prototype through 'super'")
	}
	return base.O
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
		if !cx.InheritsHasInstance(c.O) {
			site = nil
			method = cx.GetMethod(c, cx.WellKnownSymbol(SymbolHasInstance))
		}
		if method == nil {
			if !c.IsCallable() {
				cx.ThrowTypeError("right-hand side of 'instanceof' is not callable")
			}
			// OrdinaryHasInstance of a bound function is InstanceofOperator of its target.
			if b := c.O.Fn.Bound; b != nil {
				c = Value{O: b.Target}
				continue
			}
			return cx.OrdinaryHasInstance(c, v)
		}
		if method == defaultMethod && !cx.InterruptFlag.Load() {
			call := defaultHasInstanceCall{
				method:   method,
				receiver: c.O,
			}
			return call.run(cx, v, site)
		}
		return storage.ToBoolean(cx.Call(method, c, v))
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
	for ctor := call.receiver; ; ctor = ctor.Fn.Bound.Target {
		cx.pushNative(call.method, Value{O: ctor}, nil)
		if ctor == last {
			return depth
		}
	}
}

func (call defaultHasInstanceCall) run(cx *ExecutionContext, v Value, site *propCache) bool {
	ctor := call.receiver
	for ctor.Fn != nil && ctor.Fn.Bound != nil {
		target := ctor.Fn.Bound.Target
		if cx.InterruptFlag.Load() || !cx.InheritsHasInstance(target) {
			// Looking up the target's method can observe the enclosing calls.
			depth := call.enterThrough(cx, ctor)
			result := cx.instanceOf(v, Value{O: target}, nil)
			cx.truncateNatives(depth)
			return result
		}
		ctor, site = target, nil
	}
	if ctor.Fn == nil || !v.IsObject() {
		return false
	}
	slot := cx.OwnFunctionPrototypeSlot(ctor, site)
	h := v.O
	if slot != nil && slot.IsObject() {
		for h.Class != classProxy {
			h = h.Shape.Proto
			if h == slot.O {
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

// OrdinaryHasInstance implements OrdinaryHasInstance.
func (cx *ExecutionContext) OrdinaryHasInstance(c, v Value) bool {
	if !c.IsCallable() {
		return false
	}
	if b := c.O.Fn.Bound; b != nil {
		var result bool
		cx.Run(func() {
			result = cx.instanceOf(v, Value{O: b.Target}, nil)
		})
		return result
	}
	if !v.IsObject() {
		return false
	}
	slot := cx.OwnFunctionPrototypeSlot(c.O, nil)
	return cx.hasPrototype(v.O, cx.readInstancePrototype(c.O, slot))
}

func (cx *ExecutionContext) readInstancePrototype(ctor *Object, slot *Value) *Object {
	var p Value
	if slot == nil {
		p = cx.GetV(Value{O: ctor}, cx.Key(KeyPrototype))
	} else {
		p = *slot
		if p.O != nil && p.O.Kind == kindAccessor {
			if getter := p.Accessor().Getter; getter != nil {
				p = cx.Call(getter, Value{O: ctor})
			}
		}
	}
	if !p.IsObject() {
		cx.ThrowTypeError("function has non-object prototype in instanceof check")
	}
	return p.O
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
	if o.Class == classProxy {
		st := o.ExtState().(*proxyState)
		if st.IsGlobal() {
			if !st.AllowsGlobalAccess(cx.realm.globalProxyHandler) {
				cx.ThrowTypeError("no access")
			}
			return st.Target
		}
	}
	return o
}

// getPrivate returns the private element name of v, or the getter to call.
func (cx *ExecutionContext) getPrivate(v Value, name *Object) (Value, *Object) {
	if v.IsObject() {
		o := cx.privateStorage(v.O)
		if p, ok := o.Shape.Lookup(name); ok {
			val := o.Slots[p.Slot]
			if p.Flags&FlagAccessor != 0 {
				g := val.Accessor().Getter
				if g == nil {
					cx.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.Text) + "' was defined without a getter")
				}
				return storage.Undefined, g
			}
			return val, nil
		}
	}
	cx.ThrowTypeError("cannot read private member #" + wtf8.ToValidUTF8(name.Text) + " from an object whose class did not declare it")
	return storage.Undefined, nil
}

// setPrivate stores val as the private element name of v, or returns the setter to call.
func (cx *ExecutionContext) setPrivate(v Value, name *Object, val Value) *Object {
	if v.IsObject() {
		o := cx.privateStorage(v.O)
		if p, ok := o.Shape.Lookup(name); ok {
			if p.Flags&FlagAccessor != 0 {
				s := o.Slots[p.Slot].Accessor().Setter
				if s == nil {
					cx.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.Text) + "' was defined without a setter")
				}
				return s
			}
			if p.Flags&FlagWritable == 0 {
				cx.ThrowTypeError("private method '#" + wtf8.ToValidUTF8(name.Text) + "' is not writable")
			}
			o.Slots[p.Slot] = val
			return nil
		}
	}
	cx.ThrowTypeError("cannot write private member #" + wtf8.ToValidUTF8(name.Text) + " to an object whose class did not declare it")
	return nil
}

// definePrivate adds the private element name to v. val is the field value, a method, or an accessor.
func (cx *ExecutionContext) definePrivate(v Value, name *Object, val Value) {
	if !v.IsObject() {
		cx.ThrowTypeError("cannot define a private member on a non-object")
	}
	o := cx.privateStorage(v.O)
	if _, ok := o.Shape.Lookup(name); ok {
		cx.ThrowTypeError("cannot initialize #" + wtf8.ToValidUTF8(name.Text) + " twice on the same object")
	}
	flags := FlagWritable
	switch {
	case val.O != nil && val.O.Kind == kindAccessor:
		flags = FlagAccessor
	case name.Flags&objFrozenLength != 0:
		// The name stands for a method, which cannot be assigned to.
		flags = 0
	}
	cx.Add(o, name, val, flags)
}

// ensureCanAddPrivate throws a TypeError if the private element name cannot be added to v
// because v is not extensible.
func (cx *ExecutionContext) ensureCanAddPrivate(v Value, name *Object) {
	if v.IsObject() && !cx.IsExtensible(cx.privateStorage(v.O)) {
		cx.ThrowTypeError("cannot define private member #" + wtf8.ToValidUTF8(name.Text) + " on an object that is not extensible")
	}
}

// arith implements the binary arithmetic operators other than + for operands that are not both numbers.
func (cx *ExecutionContext) arith(op bytecode.Opcode, x, y Value) Value {
	if xi, ok := x.BigInt64(); ok {
		if yi, ok := y.BigInt64(); ok {
			if r, ok := storage.ArithBigInt64(op, xi, yi); ok {
				return r
			}
		}
	}
	x = cx.ToNumeric(x)
	y = cx.ToNumeric(y)
	if x.O == storage.NumberObject && y.O == storage.NumberObject {
		a, b := x.N, y.N
		switch op {
		case bytecode.OpSub:
			return storage.Number(a - b)
		case bytecode.OpMul:
			return storage.Number(a * b)
		case bytecode.OpDiv:
			return storage.Number(a / b)
		case bytecode.OpMod:
			return storage.Number(math.Mod(a, b))
		case bytecode.OpExp:
			return storage.Number(numconv.NumberPow(a, b))
		case bytecode.OpBitOr:
			return storage.Number(float64(numconv.ToInt32(a) | numconv.ToInt32(b)))
		case bytecode.OpBitAnd:
			return storage.Number(float64(numconv.ToInt32(a) & numconv.ToInt32(b)))
		case bytecode.OpBitXor:
			return storage.Number(float64(numconv.ToInt32(a) ^ numconv.ToInt32(b)))
		case bytecode.OpShl:
			return storage.Number(float64(numconv.ToInt32(a) << (numconv.ToUint32(b) & 31)))
		case bytecode.OpShr:
			return storage.Number(float64(numconv.ToInt32(a) >> (numconv.ToUint32(b) & 31)))
		case bytecode.OpUShr:
			return storage.Number(float64(numconv.ToUint32(a) >> (numconv.ToUint32(b) & 31)))
		}
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	if x.O == storage.NumberObject || y.O == storage.NumberObject {
		cx.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	// Subtraction, the bitwise operations and the shifts on huge BigInts cannot stop once they
	// start, so a pending interruption is taken before them.
	if cx.InterruptFlag.Load() {
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
	return storage.NewBigInt(r)
}

func (cx *ExecutionContext) negate(x Value) Value {
	if i, ok := x.BigInt64(); ok && i != math.MinInt64 {
		return storage.BigIntFromInt64(-i)
	}
	x = cx.ToNumeric(x)
	if x.O == storage.NumberObject {
		return storage.Number(-x.N)
	}
	return storage.NewBigInt(new(big.Int).Neg(x.BigInt()))
}

func (cx *ExecutionContext) bitNot(x Value) Value {
	if i, ok := x.BigInt64(); ok {
		return storage.BigIntFromInt64(^i)
	}
	x = cx.ToNumeric(x)
	if x.O == storage.NumberObject {
		return storage.Number(float64(^numconv.ToInt32(x.N)))
	}
	r := new(big.Int).Not(x.BigInt())
	cx.checkBigIntBits(int64(r.BitLen()))
	return storage.NewBigInt(r)
}

// step implements ++ and -- for an operand that is not a number: it adds d to x.
func (cx *ExecutionContext) step(x Value, d int64) Value {
	if i, ok := x.BigInt64(); ok {
		if s, ok := storage.Add64(i, d); ok {
			return storage.BigIntFromInt64(s)
		}
	}
	x = cx.ToNumeric(x)
	if x.O == storage.NumberObject {
		return storage.Number(x.N + float64(d))
	}
	return cx.addBigInts(x, storage.BigIntFromInt64(d))
}

// addClassElement registers an instance element of the class ctor, or defines a static element at once.
// For a field, v is the function that computes the initial value, or with [bytecode.ElemValue]
// in kind, the initial value itself.
func (cx *ExecutionContext) addClassElement(ctor *Object, key Value, v Value, kind int) {
	e := classElement{
		Kind: kind &^ (bytecode.ElemStatic | bytecode.ElemValue),
		Key:  key,
	}
	if kind&bytecode.ElemValue != 0 {
		e.Init = v
	} else {
		e.Val = v.O
	}
	if kind&bytecode.ElemStatic != 0 {
		cx.defineClassElement(Value{O: ctor}, &e)
		return
	}
	if ctor.Fn.Fields == nil {
		ctor.OwnFunction().Fields = &[]classElement{}
	}
	*ctor.Fn.Fields = append(*ctor.Fn.Fields, e)
}

// initFields defines the instance elements of the class ctor on this.
func (cx *ExecutionContext) initFields(ctor *Object, this Value) {
	fields := *ctor.Fn.Fields
	// Private methods and accessors exist before any field initializer runs.
	checked := false
	for i := range fields {
		if e := &fields[i]; e.Kind != bytecode.ElemField && e.Kind != elemInitializer {
			if !checked {
				cx.ensureCanAddPrivate(this, e.Key.O)
				checked = true
			}
			cx.defineClassElement(this, e)
		}
	}
	for i := range fields {
		switch e := &fields[i]; e.Kind {
		case bytecode.ElemField:
			cx.defineClassElement(this, e)
		case elemInitializer:
			cx.Call(e.Val, this)
		}
	}
}

func (cx *ExecutionContext) defineClassElement(this Value, e *classElement) {
	private := e.Key.O != nil && e.Key.O.Class == classPrivateName
	switch e.Kind {
	case bytecode.ElemField:
		v := e.Init
		if e.Val != nil {
			v = cx.Call(e.Val, this)
		}
		if e.Dec != nil {
			for _, init := range e.Dec.Initializers {
				v = cx.Call(init, this, v)
			}
		}
		if private {
			cx.ensureCanAddPrivate(this, e.Key.O)
			cx.definePrivate(this, e.Key.O, v)
		} else {
			cx.definePublicField(this.O, e.Key, v, &e.Cache)
		}
		if e.Dec != nil {
			for _, init := range e.Dec.Extra {
				cx.Call(init, this)
			}
		}
	case bytecode.ElemMethod:
		cx.definePrivate(this, e.Key.O, Value{O: e.Val})
	case bytecode.ElemGetter, bytecode.ElemSetter:
		if this.IsObject() {
			o := cx.privateStorage(this.O)
			if p, ok := o.Shape.Lookup(e.Key.O); ok && p.Flags&FlagAccessor != 0 {
				a := o.Slots[p.Slot].Accessor()
				if e.Kind == bytecode.ElemGetter && a.AddGetter(e.Val) {
					return
				}
				if e.Kind == bytecode.ElemSetter && a.AddSetter(e.Val) {
					return
				}
			}
		}
		if e.Kind == bytecode.ElemGetter {
			cx.definePrivate(this, e.Key.O, storage.NewAccessor(e.Val, nil))
		} else {
			cx.definePrivate(this, e.Key.O, storage.NewAccessor(nil, e.Val))
		}
	}
}

// defineField implements CreateDataPropertyOrThrow for a computed key, as object literals
// and public class fields use it.
func (cx *ExecutionContext) defineField(o *Object, k, val Value) {
	cx.CreateDataPropertyOrThrow(o, cx.ToPropertyKey(k), val)
}

// defineFieldK defines the enumerable data property key of o.
func (cx *ExecutionContext) defineFieldK(o *Object, key *Object, val Value, c *propCache) {
	if o.Class == classObject && o.Flags&(objNotExtensible|objIsProto|objExotic) == 0 {
		if _, ok := o.Shape.Lookup(key); !ok {
			old := o.Shape
			cx.Add(o, key, val, FlagsData)
			if !old.Private && !o.Shape.Private {
				c.Shape = old
				c.Kind = cacheAdd
				c.Next = o.Shape
			}
			return
		}
	}
	cx.CreateDataPropertyOrThrow(o, storage.NamedKey(key), val)
}

// definePublicField implements CreateDataPropertyOrThrow for the public class field k of o.
// c caches the shape transition of the field, whose key is always k.
func (cx *ExecutionContext) definePublicField(o *Object, k, val Value, c *propCache) {
	if o.Class == classObject && o.Flags&(objNotExtensible|objIsProto|objExotic) == 0 && o.Shape == c.Shape && c.Kind == cacheAdd {
		if validateExec {
			cx.checkDefineHit(nil, 0, o, cx.ToPropertyKey(k).Key, c.Next)
		}
		o.Shape = c.Next
		o.AppendSlot(val)
		return
	}
	if pk := cx.ToPropertyKey(k); pk.Key != nil {
		cx.defineFieldK(o, pk.Key, val, c)
		return
	}
	cx.defineField(o, k, val)
}

// defineMethod defines the method f under the key k of o.
func (cx *ExecutionContext) defineMethod(o *Object, k, f Value) {
	f.O.OwnFunction().Home = o
	cx.DefinePropertyOrThrow(o, cx.ToPropertyKey(k), storage.DataDesc(f, FlagsHidden))
}

// defineAccessor defines the getter or setter f under the key k of o.
func (cx *ExecutionContext) defineAccessor(o *Object, k Value, f *Object, isSetter, enumerable bool) {
	f.OwnFunction().Home = o
	var d PropDesc
	d.SetEnumerable(enumerable)
	d.SetConfigurable(true)
	if isSetter {
		d.SetSetter(f)
	} else {
		d.SetGetter(f)
	}
	cx.DefinePropertyOrThrow(o, cx.ToPropertyKey(k), d)
}

// funcNameOfKey returns the name that SetFunctionName gives a function for the property key or private name k.
func (cx *ExecutionContext) funcNameOfKey(k Value) wtf8.String {
	switch {
	case k.IsSymbol():
		if k.O.Class == classPrivateName {
			return "#" + k.O.Text
		}
		if k.O.ExtState() != nil {
			if len(k.O.Text)+2 > MaxStringLength {
				cx.ThrowRangeError("invalid string length")
			}
			return "[" + k.O.Text + "]"
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
	o := f.O
	if o.Fn.Materialized {
		if _, ok := o.Shape.Lookup(cx.Key(KeyName)); ok {
			if d, _ := cx.GetOwnProperty(o, storage.NamedKey(cx.Key(KeyName))); d.IsAccessor() || !d.Value().IsString() || d.Value().RawString() != "" {
				return
			}
		}
		cx.defineOwnProperty(o, storage.NamedKey(cx.Key(KeyName)), storage.DataDesc(cx.NewString(name), FlagConfigurable))
		return
	}
	// The name is part of the code of an unmaterialized function, which closures share,
	// so give this one its properties now.
	cx.Materialize(o)
	cx.Define(o, cx.Key(KeyName), cx.NewString(name), FlagConfigurable)
}

// copyDataProperties implements CopyDataProperties.
func (cx *ExecutionContext) copyDataProperties(dst *Object, src Value, excluded []Value) {
	if src.IsNullish() {
		return
	}
	from := cx.ToObject(src)
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	if len(excluded) == 0 && dst.TakeDataProperties(from) {
		return
	}
	var skip []PropKey
	for _, e := range excluded {
		skip = append(skip, cx.ToPropertyKey(e))
	}
	ks := cx.OwnKeys(from)
	if dst.Flags&objExotic == 0 {
		// The keys of the elements come first, and the other keys usually get slots.
		elems := 0
		for elems < len(ks) && ks[elems].IsIndex() {
			elems++
		}
		dst.ReserveSlots(len(ks) - elems)
	}
	var pacer Pacer
keys:
	for _, k := range ks {
		pacer.Steps(cx, 1+len(skip))
		for _, s := range skip {
			if s == k {
				continue keys
			}
		}
		d, ok := cx.GetOwnProperty(from, k)
		if !ok || !d.Enumerable() {
			continue
		}
		val := d.Value()
		if d.IsAccessor() || from.Class == classProxy {
			val = cx.getProp(from, k, Value{O: from})
		}
		cx.CreateDataPropertyOrThrow(dst, k, val)
	}
}

// setupClass makes the function ctor a class constructor that extends parent,
// which is hole for a class without an extends clause. It returns the prototype object.
func (cx *ExecutionContext) setupClass(ctor *Object, parent Value) *Object {
	protoParent := cx.Intrinsic(IntrinsicObjectPrototype)
	ctorParent := cx.Intrinsic(IntrinsicFunctionPrototype)
	if !parent.IsHole() {
		switch {
		case parent.IsNull():
			protoParent = nil
		case !parent.IsCallable() || !cx.IsConstructor(parent.O):
			cx.ThrowTypeError("class extends value " + cx.DisplayString(parent) + " is not a constructor or null")
		default:
			p := cx.GetV(parent, cx.Key(KeyPrototype))
			switch {
			case p.IsObject():
				protoParent = p.O
			case p.IsNull():
				protoParent = nil
			default:
				cx.ThrowTypeError("class extends value does not have valid prototype property")
			}
			ctorParent = parent.O
		}
	}
	proto := cx.NewObject(protoParent)
	fn := ctor.StartMaterialized(cx.ShapeFor(ctorParent))
	fn.Home = proto
	cx.Add(ctor, cx.Key(KeyLength), storage.Number(float64(fn.Code.Length)), FlagConfigurable)
	cx.Add(ctor, cx.Key(KeyName), cx.NewString(fn.Code.Name), FlagConfigurable)
	cx.Add(ctor, cx.Key(KeyPrototype), Value{O: proto}, 0)
	cx.Add(proto, cx.Key(KeyConstructor), Value{O: ctor}, FlagsHidden)
	return proto
}

// IsConstructor implements IsConstructor.
func (cx *ExecutionContext) IsConstructor(f *Object) bool {
	fn := f.Fn
	if fn == nil {
		return false
	}
	if fn.Bound != nil {
		return cx.IsConstructor(fn.Bound.Target)
	}
	return fn.Constructor
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
	return cx.callOnStack(f.O, this, args)
}

// callOnStack calls f with a copy of args on the agent stack, so that args does not escape.
func (cx *ExecutionContext) callOnStack(f *Object, this Value, args []Value) Value {
	base := cx.sp
	n := len(args)
	if fn := f.Fn; fn != nil && fn.Bound == nil && fn.Info == nil && !fn.ClassConstructor &&
		!fn.Code.IsGenerator && !fn.Code.IsAsync && !cx.InterruptFlag.Load() && len(cx.frames) < maxFrames {
		// A plain closure gets its arguments written once, where its frame starts.
		cx.ensureStack(base + n + fn.Code.Nregs + 1)
		copy(cx.stack[base:], args)
		cx.pushFrame(f, this, base, n, 0, false, nil)
		r := cx.run(len(cx.frames)-1, fn.Code.Realm)
		cx.sp = base
		return r
	}
	cx.ensureStack(base + n)
	copy(cx.stack[base:], args)
	cx.sp = base + n
	var r Value
	if fn := f.Fn; fn != nil && fn.Info != nil && !cx.InterruptFlag.Load() {
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
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	fn := f.Fn
	if fn == nil {
		cx.ThrowTypeError(cx.DisplayString(Value{O: f}) + " is not a function")
	}
	if fn.Bound != nil {
		return cx.callBound(f, args)
	}
	if fn.Info != nil {
		return cx.callNative(f, this, args, nil)
	}
	c := fn.Code
	if fn.ClassConstructor && !isNew && newTarget == nil {
		executionContextOf(c.Realm).ThrowTypeError("class constructor " + wtf8.ToValidUTF8(c.Name) + " cannot be invoked without 'new'")
	}
	if c.IsGenerator || c.IsAsync {
		return cx.callCoroutine(f, this, args)
	}
	if len(cx.frames) >= maxFrames {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	base := cx.sp
	cx.ensureStack(base + len(args) + c.Nregs + 1)
	copy(cx.stack[base:], args)
	cx.pushFrame(f, this, base, len(args), 0, isNew, newTarget)
	r := cx.run(len(cx.frames)-1, c.Realm)
	cx.sp = base
	return r
}

// callBound calls the bound function f, unwrapping a chain of bound functions without recursion.
func (cx *ExecutionContext) callBound(f *Object, args []Value) Value {
	var this Value
	base := cx.sp
	for b := f.Fn.Bound; b != nil; b = f.Fn.Bound {
		if k := len(b.Args); k > 0 {
			// The combined arguments go on the stack above its top. args may start at the top,
			// so it moves up before the bound arguments are written.
			top := cx.sp
			n := k + len(args)
			cx.ensureStack(top + n)
			s := cx.stack[top : top+n : top+n]
			copy(s[k:], args)
			copy(s, b.Args)
			cx.sp = top + n
			args = s
			if validateExec {
				defer cx.releaseArgs(cx.holdArgs(args))
			}
		}
		f, this = b.Target, b.This
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
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	fn := f.Fn
	for b := fn.Bound; b != nil; b = fn.Bound {
		if len(b.Args) > 0 {
			args = append(append(make([]Value, 0, len(b.Args)+len(args)), b.Args...), args...)
		}
		if newTarget == f {
			newTarget = b.Target
		}
		f = b.Target
		fn = f.Fn
	}
	if fn.Info != nil {
		r := cx.callNative(f, storage.Undefined, args, newTarget)
		if !r.IsObject() {
			cx.ThrowTypeError("constructor did not return an object")
		}
		return r.O
	}
	this := tdz
	if !fn.Derived {
		proto := cx.GetPrototypeFromConstructor(newTarget, IntrinsicObjectPrototype)
		this = Value{O: storage.NewInstance(cx.ShapeForRealm(proto, fn.Code.Realm))}
	}
	r := cx.callSlow(f, this, args, !fn.Derived, newTarget)
	if !r.IsObject() {
		cx.ThrowTypeError("constructor did not return an object")
	}
	return r.O
}

// GetPrototypeFromConstructor implements GetPrototypeFromConstructor.
// A nil ctor selects defaultProto in the current realm.
func (cx *ExecutionContext) GetPrototypeFromConstructor(ctor *Object, defaultProto Intrinsic) *Object {
	r := cx.realm
	if ctor != nil {
		if f := ctor.Fn; f != nil && f.Info != nil && f.Info.FixedPrototype {
			return f.Info.Realm.Intrinsics[f.Info.Prototype]
		}
		if p := cx.GetV(Value{O: ctor}, cx.Key(KeyPrototype)); p.IsObject() {
			return p.O
		}
		r = cx.FunctionContext(ctor).realm
	}
	if IntrinsicTemporalInstantPrototype <= defaultProto && defaultProto <= IntrinsicIntlSegmenterPrototype {
		ns := r.Intrinsics[IntrinsicTemporal]
		if defaultProto >= IntrinsicIntlCollatorPrototype {
			ns = r.Intrinsics[IntrinsicIntl]
		}
		if ns != nil {
			cx.InitLazyObject(ns)
		}
	}
	if proto := r.Intrinsics[defaultProto]; proto != nil {
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
		info: nativeInfo{
			Native: f,
			Name:   name,
			Length: int32(length),
			Realm:  cx.realmPart,
		},
	}
	n.state.Info = &n.info
	return n.InitFunction(cx.realm.Shapes.Function, &n.state, state)
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

const (
	ExtremumNone Extremum = iota
	ExtremumMin
	ExtremumMax
)

// SetExtremum says that the native function f is Math.min or Math.max, as e names. The contract of
// its number kernels holds for what the interpreter computes.
func (ag *agent) SetExtremum(f *Object, e Extremum) {
	f.Fn.ChangeableInfo().Extremum = e
}

// SetFrameless makes calls of the native function f show no frame: stack traces leave them out,
// and the caller of a function that f calls is the one that called f.
func (ag *agent) SetFrameless(f *Object) {
	f.Fn.ChangeableInfo().Frameless = true
}

// MethodKernel0, MethodKernel1 and MethodKernel2 compute a function for its this value and no,
// one or two arguments. They neither throw nor run code, and report false when the function has
// to be called instead.
type (
	MethodKernel0 func(q quiet.Context, this Value) (Value, bool)
	MethodKernel1 func(q quiet.Context, this, x Value) (Value, bool)
	MethodKernel2 func(q quiet.Context, this, x, y Value) (Value, bool)
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
	o.Fn.Constructor = true
	return o
}

// DefineConstructorPrototype defines the property prototype of the constructor c, which
// NewNativeConstructor returned in the current realm, as the intrinsic id, which the property
// then holds for good.
func (cx *ExecutionContext) DefineConstructorPrototype(c *Object, id Intrinsic) {
	cx.Define(c, cx.Key(KeyPrototype), Value{O: cx.Intrinsic(id)}, 0)
	info := c.Fn.ChangeableInfo()
	info.Prototype, info.FixedPrototype = id, true
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
	return ag.activeNative().ExtState()
}

// findScope returns the innermost of the depth innermost dynamic scopes of the running
// function cl that has the variable key, or nil.
func (cx *ExecutionContext) findScope(cl *Object, key *Object, depth int) *Object {
	k := storage.NamedKey(key)
	for _, scopes := range [...][]*Object{cx.frames[len(cx.frames)-1].scopes, cl.Fn.CapturedScopes()} {
		for _, o := range slices.Backward(scopes) {
			if depth == 0 {
				return nil
			}
			depth--
			if !cx.HasProperty(o, k) {
				continue
			}
			if o.Class == classEvalScope {
				return o
			}
			if u := cx.getProp(o, storage.NamedKey(cx.WellKnownSymbol(SymbolUnscopables)), Value{O: o}); u.IsObject() {
				if storage.ToBoolean(cx.getProp(u.O, k, u)) {
					continue
				}
			}
			return o
		}
	}
	return nil
}

// openScopes returns a copy of the dynamic scopes that are open in the running function cl,
// innermost last, or nil when none are.
func (ag *agent) openScopes(cl *Object) *[]*Object {
	scopes := slices.Concat(cl.Fn.CapturedScopes(), ag.frames[len(ag.frames)-1].scopes)
	if len(scopes) == 0 {
		return nil
	}
	return &scopes
}

// getScoped implements GetBindingValue of the object environment record of the dynamic scope o.
func (cx *ExecutionContext) getScoped(o *Object, key *Object, strict bool) Value {
	k := storage.NamedKey(key)
	if !cx.HasProperty(o, k) {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
		}
		return storage.Undefined
	}
	return cx.getProp(o, k, Value{O: o})
}

// setScoped writes to a dynamic scope or performs PutValue on an unresolvable reference.
func (cx *ExecutionContext) setScoped(o *Object, key *Object, val Value, strict bool) {
	k := storage.NamedKey(key)
	if o == storage.NullObject {
		if strict {
			cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
		}
		r := cx.realm
		cx.setProp(r.Global, k, val, Value{O: r.GlobalThis})
		return
	}
	if !cx.HasProperty(o, k) && strict {
		cx.ThrowReferenceError(wtf8.ToValidUTF8(key.Text) + " is not defined")
	}
	if !cx.setProp(o, k, val, Value{O: o}) && strict {
		cx.throwReadOnly(Value{O: o}, k)
	}
}

// deleteName deletes the global variable key.
func (cx *ExecutionContext) deleteName(key *Object) bool {
	r := cx.realm
	if _, ok := r.GlobalLex.Shape.Lookup(key); ok {
		return false
	}
	g := r.Global
	if p, ok := g.Shape.Lookup(key); ok {
		return cx.deleteOwnSlot(g, key, p)
	}
	// An inherited HasBinding check can create a lexical binding or an own global property.
	k := storage.NamedKey(key)
	if !cx.Quiet().Delegates(g) || !cx.HasProperty(g, k) {
		return true
	}
	if _, ok := r.GlobalLex.Shape.Lookup(key); ok {
		return false
	}
	return cx.DeleteProp(g, k)
}

// BoundFunctionCreate implements BoundFunctionCreate. It copies args.
func (cx *ExecutionContext) BoundFunctionCreate(target *Object, this Value, args []Value) *Object {
	s := cx.ShapeForRealm(cx.ProtoOf(target), target.Shape.Realm)
	b := &boundObject{
		bound: boundFunc{
			Target: target,
			This:   this,
			Args:   append([]Value(nil), args...),
		},
	}
	b.state = function{
		Bound:        &b.bound,
		Materialized: true,
		// The state of a bound target already says whether its own target is a constructor.
		Constructor: target.Fn.Constructor,
	}
	return b.InitMaterializedFunction(s, &b.state, b.props[:])
}

// boundObject is a bound function object together with its state and the storage of its
// length and name properties, so that creating one is a single allocation.
type boundObject struct {
	propertyObject
	state function
	bound boundFunc
	props [2]Value
}
