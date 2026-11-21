// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func (vm *VM) throwTDZ(name *Object) {
	if name.str == "this" {
		vm.ThrowReferenceError("must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	vm.ThrowReferenceError("cannot access '" + wtf8.ToValidUTF8(name.str) + "' before initialization")
}

func (vm *VM) throwReadOnly(v Value, k PropKey) {
	vm.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(vm.KeyString(k)) + "' of " + vm.DisplayString(v))
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
func (vm *VM) calleeName(fp *funcProto, pc int) wtf8.String {
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
func (vm *VM) throwNotCallable(fp *funcProto, pc int, regs []Value) {
	pc = callAt(fp, pc)
	name := wtf8.ToValidUTF8(vm.calleeName(fp, pc))
	if name == "" {
		name = vm.DisplayString(regs[fp.code[pc].B])
	}
	vm.ThrowTypeError(name + " is not a function")
}

// throwNotConstructor throws the TypeError for the call instruction of fp that ends before pc,
// whose callee is not a constructor.
func (vm *VM) throwNotConstructor(fp *funcProto, pc int, regs []Value) {
	pc = callAt(fp, pc)
	name := wtf8.ToValidUTF8(vm.calleeName(fp, pc))
	if name == "" {
		name = vm.DisplayString(regs[fp.code[pc].B])
	}
	vm.ThrowTypeError(name + " is not a constructor")
}

// getGlobal reads the global variable key, as strict code does when strict is set. When it does
// not exist, it throws a ReferenceError unless orUndef is set.
func (vm *VM) getGlobal(key *Object, c *globalCache, orUndef, strict bool) Value {
	g := vm.global
	if p, ok := vm.globalLex.shape.lookup(key); ok {
		v := vm.globalLex.slots[p.slot]
		if v.o == tdzObject {
			vm.throwTDZ(key)
		}
		// c refers only to initialized bindings, which stay initialized, so a hit needs no
		// check.
		c.holder = g
		c.shape = g.shape
		c.ref = &vm.globalLex.slots[p.slot]
		return v
	}
	if p, ok := g.shape.lookup(key); ok && p.flags&FlagAccessor == 0 {
		c.holder = g
		c.shape = g.shape
		c.ref = &g.slots[p.slot]
		return g.slots[p.slot]
	}
	k := PropKey{key: key}
	if !vm.HasProperty(g, k) {
		if orUndef {
			return Undefined
		}
		vm.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	}
	// GetBindingValue checks for the property again, which only a proxy can observe.
	if proxyOnChain(g) != nil && !vm.HasProperty(g, k) {
		if strict {
			vm.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
		return Undefined
	}
	return vm.getProp(g, k, Value{o: vm.globalThis})
}

// resolveGlobal returns whether the global variable key exists.
func (vm *VM) resolveGlobal(key *Object, c *globalCache) Value {
	g := vm.global
	_, lex := vm.globalLex.shape.lookup(key)
	if _, own := g.shape.lookup(key); lex || own {
		c.holder = g
		c.shape = g.shape
		return True
	}
	return Bool(vm.HasProperty(g, PropKey{key: key}))
}

// setGlobal assigns to the global variable key.
func (vm *VM) setGlobal(key *Object, val Value, c *globalCache, strict bool) {
	g := vm.global
	if p, ok := vm.globalLex.shape.lookup(key); ok {
		if vm.globalLex.slots[p.slot].o == tdzObject {
			vm.throwTDZ(key)
		}
		if p.flags&FlagWritable == 0 {
			vm.ThrowTypeError("assignment to constant variable '" + wtf8.ToValidUTF8(key.str) + "'")
		}
		vm.globalLex.slots[p.slot] = val
		// As in getGlobal, c refers only to initialized bindings.
		c.holder = g
		c.shape = g.shape
		c.ref = &vm.globalLex.slots[p.slot]
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
	// Resolving the name and SetMutableBinding both check for the property, which only matters
	// in strict code or to a proxy.
	if proxied := proxyOnChain(g) != nil; strict || proxied {
		found := vm.HasProperty(g, k)
		if found && proxied {
			found = vm.HasProperty(g, k)
		}
		if !found && strict {
			vm.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
	}
	if !vm.setProp(g, k, val, Value{o: vm.globalThis}) && strict {
		vm.throwReadOnly(Value{o: g}, k)
	}
}

// checkGlobalDecl throws when declaring key at the top level of a script would conflict
// with an existing declaration. what is a bytecode Decl kind.
func (vm *VM) checkGlobalDecl(key *Object, what int) {
	_, lex := vm.globalLex.shape.lookup(key)
	switch what {
	case bytecode.DeclVar:
		if lex {
			vm.throwSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		if !vm.HasOwnProperty(vm.global, PropKey{key: key}) && !vm.IsExtensible(vm.global) {
			vm.ThrowTypeError("cannot declare global variable '" + wtf8.ToValidUTF8(key.str) + "'")
		}
	case bytecode.DeclLexical:
		if lex {
			vm.throwSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		// A lexical declaration may not hide a global property that cannot be reconfigured.
		if d, ok := vm.GetOwnProperty(vm.global, PropKey{key: key}); ok && d.flags&FlagConfigurable == 0 {
			vm.throwSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
	case bytecode.DeclFunction:
		if lex {
			vm.throwSyntaxError("identifier '" + wtf8.ToValidUTF8(key.str) + "' has already been declared")
		}
		d, ok := vm.GetOwnProperty(vm.global, PropKey{key: key})
		switch {
		case !ok:
			if !vm.IsExtensible(vm.global) {
				vm.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.str) + "'")
			}
		case d.flags&FlagConfigurable != 0:
		case d.flags&FlagAccessor != 0 || d.flags&(FlagWritable|FlagEnumerable) != FlagWritable|FlagEnumerable:
			vm.ThrowTypeError("cannot declare global function '" + wtf8.ToValidUTF8(key.str) + "'")
		}
	}
}

// declareGlobalVar implements CreateGlobalVarBinding, and CreateGlobalFunctionBinding when
// flags has [bytecode.GlobalVarFunc]. flags is a set of the bytecode GlobalVar flags.
func (vm *VM) declareGlobalVar(key *Object, flags int, fn Value) {
	g := vm.global
	k := PropKey{key: key}
	d, ok := vm.GetOwnProperty(g, k)
	attrs := FlagEnumerable | FlagWritable
	if flags&bytecode.GlobalVarDeletable != 0 {
		attrs |= FlagConfigurable
	}
	if flags&bytecode.GlobalVarFunc == 0 {
		if !ok {
			vm.DefinePropertyOrThrow(g, k, dataDesc(Undefined, attrs))
		}
		return
	}
	if !ok || d.flags&FlagConfigurable != 0 {
		vm.DefinePropertyOrThrow(g, k, dataDesc(fn, attrs))
		return
	}
	vm.DefinePropertyOrThrow(g, k, PropDesc{
		value: fn,
		has:   HasValue,
	})
}

// declareGlobalAnnexB declares the global variable key of a block-level function of sloppy
// code, as Annex B.3.2.2 and B.3.2.3 do, and reports whether it is declared. It declares
// nothing when a global lexical declaration of key exists or CanDeclareGlobalVar(key) is false.
// flags is a set of the bytecode GlobalVar flags.
func (vm *VM) declareGlobalAnnexB(key *Object, flags int) bool {
	if _, lex := vm.globalLex.shape.lookup(key); lex {
		return false
	}
	if !vm.HasOwnProperty(vm.global, PropKey{key: key}) && !vm.IsExtensible(vm.global) {
		return false
	}
	vm.declareGlobalVar(key, flags&^bytecode.GlobalVarFunc, Undefined)
	return true
}

// declareGlobalLex creates the uninitialized global lexical binding key.
func (vm *VM) declareGlobalLex(key *Object, isConst bool) {
	flags := FlagsData
	if isConst {
		flags = FlagEnumerable | FlagConfigurable
	}
	vm.add(vm.globalLex, key, tdz, flags)
	// The binding may hide a property of the global object, and the bindings may have moved,
	// so the global variable caches must not apply any longer.
	vm.global.renewShape()
}

func (vm *VM) initGlobalLex(key *Object, val Value) {
	p, _ := vm.globalLex.shape.lookup(key)
	vm.globalLex.slots[p.slot] = val
}

// getValue returns v[k], or the getter to call.
func (vm *VM) getValue(v, k Value) (Value, *Object) {
	if ko := k.o; ko == numberObject {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return vm.typedArrayGet(o, i), nil
			}
			return vm.getIndex(v, i)
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
		vm.ThrowTypeError("cannot read properties of " + vm.DisplayString(v) + " (reading '" + vm.DisplayString(k) + "')")
	}
	key := vm.QueryKey(k)
	if key.key == nil {
		return vm.getIndex(v, key.idx)
	}
	return vm.getNamed(v, key.key, nil)
}

// setValue stores v[k] = val and returns the setter to call, if any. A store that fails throws
// a TypeError in strict mode.
func (vm *VM) setValue(v, k, val Value, strict bool) *Object {
	setter, ok, key := vm.storeValue(v, k, val)
	if !ok && strict {
		vm.throwReadOnly(v, key)
	}
	return setter
}

// storeValue stores v[k] = val. The first two results are those of setNamed, and the last is k
// converted to a property key when the store fails.
func (vm *VM) storeValue(v, k, val Value) (*Object, bool, PropKey) {
	if ko := k.o; ko == numberObject {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return nil, vm.typedArraySet(o, i, val), IndexKey(i)
			}
			setter, ok := vm.setIndex(v, i, val)
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
		vm.ThrowTypeError("cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.DisplayString(k) + "')")
	}
	key := vm.ToPropertyKey(k)
	if key.key == nil {
		setter, ok := vm.setIndex(v, key.idx, val)
		return setter, ok, key
	}
	setter, ok := vm.setNamed(v, key.key, val, nil)
	return setter, ok, key
}

// deleteValue implements the delete operator on a property.
func (vm *VM) deleteValue(v, k Value, strict bool) Value {
	if v.IsNullish() {
		vm.ThrowTypeError("cannot convert undefined or null to object")
	}
	key := vm.ToPropertyKey(k)
	o := vm.ToObject(v)
	ok := vm.DeleteProp(o, key)
	if !ok && strict {
		vm.ThrowTypeError("cannot delete property '" + wtf8.ToValidUTF8(vm.KeyString(key)) + "' of " + vm.DisplayString(v))
	}
	return Bool(ok)
}

// superBase implements GetSuperBase for the home object home.
func (vm *VM) superBase(home Value) Value {
	if !home.IsObject() {
		vm.throwSyntaxError("'super' keyword unexpected here")
	}
	if p := vm.ProtoOf(home.o); p != nil {
		return ObjectValue(p)
	}
	return Null
}

// superObject returns the object that a super property access with the super base base starts from.
func (vm *VM) superObject(base Value) *Object {
	if base.IsNull() {
		vm.ThrowTypeError("cannot access a property of a null prototype through 'super'")
	}
	return base.o
}

// getSuper reads the property k of the super base base with the this value this,
// and returns the property key as well.
func (vm *VM) getSuper(base, k, this Value) (Value, PropKey) {
	o := vm.superObject(base)
	key := vm.ToPropertyKey(k)
	return vm.getProp(o, key, this), key
}

func (vm *VM) setSuper(base, k, val, this Value, strict bool) {
	o := vm.superObject(base)
	key := vm.ToPropertyKey(k)
	if !vm.setProp(o, key, val, this) && strict {
		vm.throwReadOnly(this, key)
	}
}

// instanceOf implements InstanceofOperator. site is the inline cache of the OpInstanceof that
// evaluates it, or nil.
func (vm *VM) instanceOf(v, c Value, site *propCache) bool {
	for {
		if !c.IsObject() {
			vm.ThrowTypeError("right-hand side of 'instanceof' is not an object")
		}
		if !vm.inheritsHasInstance(c.o) {
			site = nil
			if m := vm.GetMethod(c, vm.WellKnownSymbol(SymbolHasInstance)); m != nil {
				if m != vm.Intrinsic(IntrinsicFunctionPrototypeHasInstance) {
					return ToBoolean(vm.Call(m, c, v))
				}
			} else if !c.IsCallable() {
				vm.ThrowTypeError("right-hand side of 'instanceof' is not callable")
			}
		}
		// OrdinaryHasInstance of a bound function is InstanceofOperator of its target.
		if !c.IsCallable() || c.o.fn.bound == nil {
			return vm.ordinaryHasInstance(c, v, site)
		}
		c, site = Value{o: c.o.fn.bound.target}, nil
	}
}

// inheritsHasInstance reports whether the @@hasInstance property of c is the one that it
// inherits from %Function.prototype% of the current realm. A false result does not mean the
// opposite.
func (vm *VM) inheritsHasInstance(c *Object) bool {
	// The property of %Function.prototype% is neither writable nor configurable, so only the
	// objects before it on the chain can hide it.
	fp := vm.Intrinsic(IntrinsicFunctionPrototype)
	key := vm.WellKnownSymbol(SymbolHasInstance)
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

// functionPrototype returns Get(c, "prototype") for the function c.
func (vm *VM) functionPrototype(c *Object) Value {
	if c.class == classFunction {
		if c.flags&objExotic != 0 {
			vm.resolve(c)
		}
		if p, ok := c.shape.lookup(vm.Key(KeyPrototype)); ok && p.flags&FlagAccessor == 0 {
			return c.slots[p.slot]
		}
	}
	return vm.GetV(Value{o: c}, vm.Key(KeyPrototype))
}

// OrdinaryHasInstance implements OrdinaryHasInstance.
func (vm *VM) OrdinaryHasInstance(c, v Value) bool {
	return vm.ordinaryHasInstance(c, v, nil)
}

// ordinaryHasInstance implements OrdinaryHasInstance for the instanceof operator with the inline
// cache site, which is nil unless the @@hasInstance of c is that of %Function.prototype%.
func (vm *VM) ordinaryHasInstance(c, v Value, site *propCache) bool {
	if !c.IsCallable() {
		return false
	}
	if b := c.o.fn.bound; b != nil {
		return vm.instanceOf(v, Value{o: b.target}, nil)
	}
	if !v.IsObject() {
		return false
	}
	p := vm.functionPrototype(c.o)
	if !p.IsObject() {
		vm.ThrowTypeError("function has non-object prototype in instanceof check")
	}
	if site != nil && c.o.class == classFunction && c.o.flags&objExotic == 0 {
		if s, ok := c.o.shape.lookup(vm.Key(KeyPrototype)); ok && s.flags&FlagAccessor == 0 {
			// Caching the constructor's own prototype slot also relies on @@hasInstance staying inherited.
			site.setInherited(c.o.shape, cacheOwn, vm.protoEpoch)
			site.slot = s.slot
		}
	}
	for o := vm.ProtoOf(v.o); o != nil; o = vm.ProtoOf(o) {
		if o == p.o {
			return true
		}
	}
	return false
}

// privateStorage returns the object holding o's private elements.
func (vm *VM) privateStorage(o *Object) *Object {
	if o.class == classProxy {
		st := o.extState().(*proxyState)
		if st.isGlobal() {
			if !st.allowsGlobalAccess(vm.globalProxyHandler) {
				vm.ThrowTypeError("no access")
			}
			return st.target
		}
	}
	return o
}

// getPrivate returns the private element name of v, or the getter to call.
func (vm *VM) getPrivate(v Value, name *Object) (Value, *Object) {
	if v.IsObject() {
		o := vm.privateStorage(v.o)
		if p, ok := o.shape.lookup(name); ok {
			val := o.slots[p.slot]
			if p.flags&FlagAccessor != 0 {
				g := val.accessor().getter
				if g == nil {
					vm.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.str) + "' was defined without a getter")
				}
				return Undefined, g
			}
			return val, nil
		}
	}
	vm.ThrowTypeError("cannot read private member #" + wtf8.ToValidUTF8(name.str) + " from an object whose class did not declare it")
	return Undefined, nil
}

// setPrivate stores val as the private element name of v, or returns the setter to call.
func (vm *VM) setPrivate(v Value, name *Object, val Value) *Object {
	if v.IsObject() {
		o := vm.privateStorage(v.o)
		if p, ok := o.shape.lookup(name); ok {
			if p.flags&FlagAccessor != 0 {
				s := o.slots[p.slot].accessor().setter
				if s == nil {
					vm.ThrowTypeError("'#" + wtf8.ToValidUTF8(name.str) + "' was defined without a setter")
				}
				return s
			}
			if p.flags&FlagWritable == 0 {
				vm.ThrowTypeError("private method '#" + wtf8.ToValidUTF8(name.str) + "' is not writable")
			}
			o.slots[p.slot] = val
			return nil
		}
	}
	vm.ThrowTypeError("cannot write private member #" + wtf8.ToValidUTF8(name.str) + " to an object whose class did not declare it")
	return nil
}

// definePrivate adds the private element name to v. val is the field value, a method, or an accessor.
func (vm *VM) definePrivate(v Value, name *Object, val Value) {
	if !v.IsObject() {
		vm.ThrowTypeError("cannot define a private member on a non-object")
	}
	o := vm.privateStorage(v.o)
	if _, ok := o.shape.lookup(name); ok {
		vm.ThrowTypeError("cannot initialize #" + wtf8.ToValidUTF8(name.str) + " twice on the same object")
	}
	flags := FlagWritable
	switch {
	case val.o != nil && val.o.kind == kindAccessor:
		flags = FlagAccessor
	case name.flags&objFrozenLength != 0:
		// The name stands for a method, which cannot be assigned to.
		flags = 0
	}
	vm.add(o, name, val, flags)
}

// ensureCanAddPrivate throws a TypeError if the private element name cannot be added to v
// because v is not extensible.
func (vm *VM) ensureCanAddPrivate(v Value, name *Object) {
	if v.IsObject() && !vm.IsExtensible(vm.privateStorage(v.o)) {
		vm.ThrowTypeError("cannot define private member #" + wtf8.ToValidUTF8(name.str) + " on an object that is not extensible")
	}
}

// arith implements the binary arithmetic operators other than + for operands that are not both numbers.
func (vm *VM) arith(op bytecode.Opcode, x, y Value) Value {
	if xi, ok := x.BigInt64(); ok {
		if yi, ok := y.BigInt64(); ok {
			if r, ok := arithBigInt64(op, xi, yi); ok {
				return r
			}
		}
	}
	x = vm.ToNumeric(x)
	y = vm.ToNumeric(y)
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
			return Number(NumberPow(a, b))
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
		vm.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	// Subtraction, the bitwise operations and the shifts on huge BigInts cannot stop once they
	// start, so a pending interruption is taken before them.
	if vm.interrupt.Load() {
		vm.Poll()
	}
	a, b := x.BigInt(), y.BigInt()
	r := new(big.Int)
	switch op {
	case bytecode.OpSub:
		r.Sub(a, b)
		vm.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpMul:
		return vm.multiplyBigInts(a, b)
	case bytecode.OpDiv:
		if b.Sign() == 0 {
			vm.ThrowRangeError("division by zero")
		}
		r, _ = numconv.QuoRemBigInt(a, b, vm.Poll)
	case bytecode.OpMod:
		if b.Sign() == 0 {
			vm.ThrowRangeError("division by zero")
		}
		_, r = numconv.QuoRemBigInt(a, b, vm.Poll)
	case bytecode.OpExp:
		if b.Sign() < 0 {
			vm.ThrowRangeError("exponent must be non-negative")
		}
		if a.CmpAbs(big.NewInt(1)) <= 0 {
			r.Exp(a, b, nil)
			break
		}
		// The result of a base other than 0, 1 and -1 has more bits than the exponent.
		if !b.IsInt64() || b.Int64() >= MaxBigIntBits {
			vm.ThrowRangeError("maximum BigInt size exceeded")
		}
		n := b.Int64()
		size := powBitLen(a, n)
		vm.checkBigIntBits(size)
		if size > bigExpStepBits {
			vm.bigExp(r, a, n)
		} else {
			r.Exp(a, b, nil)
		}
		vm.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitOr:
		r.Or(a, b)
		vm.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitAnd:
		r.And(a, b)
		vm.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpBitXor:
		r.Xor(a, b)
		vm.checkBigIntBits(int64(r.BitLen()))
	case bytecode.OpShl, bytecode.OpShr:
		n := b
		if op == bytecode.OpShr {
			n = new(big.Int).Neg(b)
		}
		switch {
		case !n.IsInt64() || n.Int64() > 1<<30:
			if n.Sign() > 0 && a.Sign() != 0 {
				vm.ThrowRangeError("maximum BigInt size exceeded")
			}
			if a.Sign() < 0 && n.Sign() < 0 {
				r.SetInt64(-1)
			}
		case n.Sign() >= 0:
			if a.Sign() != 0 {
				vm.checkBigIntBits(int64(a.BitLen()) + n.Int64())
			}
			r.Lsh(a, uint(n.Int64()))
		case -n.Int64() > 1<<30:
			if a.Sign() < 0 {
				r.SetInt64(-1)
			}
		default:
			r.Rsh(a, uint(-n.Int64()))
		}
	case bytecode.OpUShr:
		vm.ThrowTypeError("BigInts have no unsigned right shift, use >> instead")
	default:
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	return vm.NewBigInt(r)
}

func (vm *VM) negate(x Value) Value {
	if i, ok := x.BigInt64(); ok && i != math.MinInt64 {
		return BigIntFromInt64(-i)
	}
	x = vm.ToNumeric(x)
	if x.o == numberObject {
		return Number(-x.n)
	}
	return vm.NewBigInt(new(big.Int).Neg(x.BigInt()))
}

func (vm *VM) bitNot(x Value) Value {
	if i, ok := x.BigInt64(); ok {
		return BigIntFromInt64(^i)
	}
	x = vm.ToNumeric(x)
	if x.o == numberObject {
		return Number(float64(^numconv.ToInt32(x.n)))
	}
	r := new(big.Int).Not(x.BigInt())
	vm.checkBigIntBits(int64(r.BitLen()))
	return vm.NewBigInt(r)
}

// step implements ++ and -- for an operand that is not a number: it adds d to x.
func (vm *VM) step(x Value, d int64) Value {
	if i, ok := x.BigInt64(); ok {
		if s, ok := add64(i, d); ok {
			return BigIntFromInt64(s)
		}
	}
	x = vm.ToNumeric(x)
	if x.o == numberObject {
		return Number(x.n + float64(d))
	}
	return vm.addBigInts(x, BigIntFromInt64(d))
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
func (vm *VM) addClassElement(ctor *Object, key Value, v Value, kind int) {
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
		vm.defineClassElement(Value{o: ctor}, &e)
		return
	}
	if ctor.fn.fields == nil {
		ctor.ownFunction().fields = &[]classElement{}
	}
	*ctor.fn.fields = append(*ctor.fn.fields, e)
}

// initFields defines the instance elements of the class ctor on this.
func (vm *VM) initFields(ctor *Object, this Value) {
	fields := *ctor.fn.fields
	// Private methods and accessors exist before any field initializer runs.
	checked := false
	for i := range fields {
		if e := &fields[i]; e.kind != bytecode.ElemField && e.kind != elemInitializer {
			if !checked {
				vm.ensureCanAddPrivate(this, e.key.o)
				checked = true
			}
			vm.defineClassElement(this, e)
		}
	}
	for i := range fields {
		switch e := &fields[i]; e.kind {
		case bytecode.ElemField:
			vm.defineClassElement(this, e)
		case elemInitializer:
			vm.Call(e.val, this)
		}
	}
}

func (vm *VM) defineClassElement(this Value, e *classElement) {
	private := e.key.o != nil && e.key.o.class == classPrivateName
	switch e.kind {
	case bytecode.ElemField:
		v := e.init
		if e.val != nil {
			v = vm.Call(e.val, this)
		}
		if e.dec != nil {
			for _, init := range e.dec.initializers {
				v = vm.Call(init, this, v)
			}
		}
		if private {
			vm.ensureCanAddPrivate(this, e.key.o)
			vm.definePrivate(this, e.key.o, v)
		} else {
			vm.definePublicField(this.o, e.key, v, &e.cache)
		}
		if e.dec != nil {
			for _, init := range e.dec.extra {
				vm.Call(init, this)
			}
		}
	case bytecode.ElemMethod:
		vm.definePrivate(this, e.key.o, Value{o: e.val})
	case bytecode.ElemGetter, bytecode.ElemSetter:
		if this.IsObject() {
			o := vm.privateStorage(this.o)
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
			vm.definePrivate(this, e.key.o, newAccessor(e.val, nil))
		} else {
			vm.definePrivate(this, e.key.o, newAccessor(nil, e.val))
		}
	}
}

func (vm *VM) typeofValue(v Value) Value {
	return ObjectValue(vm.Intern(typeOf(v)))
}

// defineField implements CreateDataPropertyOrThrow for a computed key, as object literals
// and public class fields use it.
func (vm *VM) defineField(o *Object, k, val Value) {
	vm.CreateDataPropertyOrThrow(o, vm.ToPropertyKey(k), val)
}

// defineFieldK defines the enumerable data property key of o.
func (vm *VM) defineFieldK(o *Object, key *Object, val Value, c *propCache) {
	if o.class == classObject && o.flags&(objNotExtensible|objIsProto|objExotic) == 0 {
		if _, ok := o.shape.lookup(key); !ok {
			old := o.shape
			vm.add(o, key, val, FlagsData)
			if !old.private && !o.shape.private {
				c.shape = old
				c.kind = cacheAdd
				c.next = o.shape
			}
			return
		}
	}
	vm.CreateDataPropertyOrThrow(o, PropKey{key: key}, val)
}

// definePublicField implements CreateDataPropertyOrThrow for the public class field k of o.
// c caches the shape transition of the field, whose key is always k.
func (vm *VM) definePublicField(o *Object, k, val Value, c *propCache) {
	if o.class == classObject && o.flags&(objNotExtensible|objIsProto|objExotic) == 0 && o.shape == c.shape && c.kind == cacheAdd {
		o.shape = c.next
		o.slots = append(o.slots, val)
		return
	}
	if pk := vm.ToPropertyKey(k); pk.key != nil {
		vm.defineFieldK(o, pk.key, val, c)
		return
	}
	vm.defineField(o, k, val)
}

// defineMethod defines the method f under the key k of o.
func (vm *VM) defineMethod(o *Object, k, f Value) {
	f.o.ownFunction().home = o
	vm.DefinePropertyOrThrow(o, vm.ToPropertyKey(k), dataDesc(f, FlagsHidden))
}

// defineAccessor defines the getter or setter f under the key k of o.
func (vm *VM) defineAccessor(o *Object, k Value, f *Object, isSetter, enumerable bool) {
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
	vm.DefinePropertyOrThrow(o, vm.ToPropertyKey(k), d)
}

// funcNameOfKey returns the name that SetFunctionName gives a function for the property key or private name k.
func (vm *VM) funcNameOfKey(k Value) wtf8.String {
	switch {
	case k.IsSymbol():
		if k.o.class == classPrivateName {
			return "#" + k.o.str
		}
		if k.o.extState() != nil {
			if len(k.o.str)+2 > MaxStringLength {
				vm.ThrowRangeError("invalid string length")
			}
			return "[" + k.o.str + "]"
		}
		return ""
	case k.IsString():
		return k.Str()
	}
	return vm.ToString(k)
}

// setFuncName implements SetFunctionName for the function or class f, which must be anonymous.
func (vm *VM) setFuncName(f, k Value, prefix int) {
	if !f.IsCallable() {
		return
	}
	name := vm.funcNameOfKey(k)
	switch prefix {
	case bytecode.NamePrefixGet:
		name = "get " + name
	case bytecode.NamePrefixSet:
		name = "set " + name
	}
	if len(name) > MaxStringLength {
		vm.ThrowRangeError("invalid string length")
	}
	o := f.o
	if o.fn.materialized {
		if _, ok := o.shape.lookup(vm.Key(KeyName)); ok {
			if d, _ := vm.GetOwnProperty(o, PropKey{key: vm.Key(KeyName)}); d.flags&FlagAccessor != 0 || !d.value.IsString() || d.value.rawString() != "" {
				return
			}
		}
		vm.defineOwnProperty(o, PropKey{key: vm.Key(KeyName)}, dataDesc(vm.NewString(name), FlagConfigurable))
		return
	}
	// The name is part of the code of an unmaterialized function, which closures share,
	// so give this one its properties now.
	vm.materialize(o)
	vm.Define(o, vm.Key(KeyName), vm.NewString(name), FlagConfigurable)
}

// copyDataProperties implements CopyDataProperties.
func (vm *VM) copyDataProperties(dst *Object, src Value, excluded []Value) {
	if src.IsNullish() {
		return
	}
	from := vm.ToObject(src)
	if len(excluded) == 0 && takeDataProperties(dst, from) {
		return
	}
	var skip []PropKey
	for _, e := range excluded {
		skip = append(skip, vm.ToPropertyKey(e))
	}
	ks := vm.OwnKeys(from)
	if dst.flags&objExotic == 0 {
		// The keys of the elements come first, and the other keys usually get slots.
		elems := 0
		for elems < len(ks) && ks[elems].isIndex() {
			elems++
		}
		dst.slots = slices.Grow(dst.slots, len(ks)-elems)
	}
	var pacer Pacer
keys:
	for _, k := range ks {
		pacer.Step(vm)
		for _, s := range skip {
			if s == k {
				continue keys
			}
		}
		d, ok := vm.GetOwnProperty(from, k)
		if !ok || d.flags&FlagEnumerable == 0 {
			continue
		}
		val := d.value
		if d.flags&FlagAccessor != 0 || from.class == classProxy {
			val = vm.getProp(from, k, Value{o: from})
		}
		vm.CreateDataPropertyOrThrow(dst, k, val)
	}
}

// takeDataProperties gives the empty ordinary object dst the properties of from and reports
// true when creating them on dst one by one would give dst the shape of from. Otherwise it
// does nothing and reports false.
func takeDataProperties(dst, from *Object) bool {
	s := takenShape(dst, from)
	if s == nil {
		return false
	}
	dst.shape = s
	dst.slots = append(dst.slots[:0], from.slots[:s.count]...)
	return true
}

// AssignDataProperties copies the own enumerable properties of from to the empty object to as
// Object.assign does, when that is known to run no code, and reports whether it did.
func (vm *VM) AssignDataProperties(to, from *Object) bool {
	s := takenShape(to, from)
	if s == nil {
		return false
	}
	// Set creates an own data property on to unless the first property of the same key on
	// the prototype chain is an accessor or read-only. The keys of s are strings that are not
	// array indices, which only an ordinary object is known to look up in its shape alone.
	for h := to.shape.proto; h != nil; h = h.shape.proto {
		if h.kind != kindObject || h.class != classObject || h.flags&objExotic != 0 {
			return false
		}
	}
	for t := s; t.parent != nil; t = t.parent {
		if _, p, ok := find(to.shape.proto, t.key); ok && p.flags&(FlagAccessor|FlagWritable) != FlagWritable {
			return false
		}
	}
	to.shape = s
	to.slots = append(to.slots[:0], from.slots[:s.count]...)
	return true
}

// takenShape returns the shape that dst, an empty ordinary object, would have after creating
// the properties of from on it one by one, when that shape is the shape of from and from has
// only enumerable, writable and configurable data properties with string keys. Otherwise it
// returns nil.
func takenShape(dst, from *Object) *shape {
	if dst.kind != kindObject || dst.class != classObject || dst.flags&(objNotExtensible|objIsProto|objExotic|objSparse) != 0 || dst.shape.count != 0 || len(dst.elems) != 0 {
		return nil
	}
	// An ordinary object without elements and not sparse has no property with an array index
	// key, so its keys are in insertion order, which is the order of its slots.
	if from.kind != kindObject || from.class != classObject || from.flags&(objExotic|objSparse) != 0 || len(from.elems) != 0 {
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
func (vm *VM) setupClass(ctor *Object, parent Value) *Object {
	protoParent := vm.Intrinsic(IntrinsicObjectPrototype)
	ctorParent := vm.Intrinsic(IntrinsicFunctionPrototype)
	if !parent.isHole() {
		switch {
		case parent.IsNull():
			protoParent = nil
		case !parent.IsCallable() || !vm.IsConstructor(parent.o):
			vm.ThrowTypeError("class extends value " + vm.DisplayString(parent) + " is not a constructor or null")
		default:
			p := vm.GetV(parent, vm.Key(KeyPrototype))
			switch {
			case p.IsObject():
				protoParent = p.o
			case p.IsNull():
				protoParent = nil
			default:
				vm.ThrowTypeError("class extends value does not have valid prototype property")
			}
			ctorParent = parent.o
		}
	}
	proto := vm.NewObject(protoParent)
	fn := ctor.ownFunction()
	fn.materialized = true
	fn.home = proto
	ctor.flags &^= objExotic
	ctor.shape = vm.shapeFor(ctorParent)
	vm.add(ctor, vm.Key(KeyLength), Number(float64(fn.code.length)), FlagConfigurable)
	vm.add(ctor, vm.Key(KeyName), vm.NewString(fn.code.name), FlagConfigurable)
	vm.add(ctor, vm.Key(KeyPrototype), Value{o: proto}, 0)
	vm.add(proto, vm.Key(KeyConstructor), Value{o: ctor}, FlagsHidden)
	return proto
}

// IsConstructor implements IsConstructor.
func (vm *VM) IsConstructor(f *Object) bool {
	fn := f.fn
	if fn == nil {
		return false
	}
	if fn.bound != nil {
		return vm.IsConstructor(fn.bound.target)
	}
	return fn.constructor
}

// functionProtoFor returns the prototype of the closures of p, which is a generator or async function.
func (vm *VM) functionProtoFor(p *funcProto) *Object {
	switch {
	case p.isGenerator && p.isAsync:
		return vm.Intrinsic(IntrinsicAsyncGeneratorFunctionPrototype)
	case p.isGenerator:
		return vm.Intrinsic(IntrinsicGeneratorFunctionPrototype)
	case p.isAsync:
		return vm.Intrinsic(IntrinsicAsyncFunctionPrototype)
	}
	return vm.Intrinsic(IntrinsicFunctionPrototype)
}

// arrayProtoPlain reports whether no array index property exists on the prototypes of plain arrays,
// so that storing a new element in such an array cannot run a setter.
func (vm *VM) arrayProtoPlain() bool {
	a, o := vm.Intrinsic(IntrinsicArrayPrototype), vm.Intrinsic(IntrinsicObjectPrototype)
	return len(a.elems) == 0 && len(o.elems) == 0 && (a.flags|o.flags)&objSparse == 0 && a.shape.proto == o
}

// arrayIterationPlain reports whether iterating the array o visits its elements by index
// without any observable call.
func (vm *VM) arrayIterationPlain(o *Object) bool {
	return o.shape == vm.shapes.array && vm.arrayIterationUnmodified()
}

// arrayIterationUnmodified reports whether %Array.prototype%[@@iterator] and
// %ArrayIteratorPrototype%.next are the original functions.
func (vm *VM) arrayIterationUnmodified() bool {
	c := &vm.arrayIter
	if c.epoch != vm.protoEpoch || !c.checked {
		c.checked = true
		c.epoch = vm.protoEpoch
		c.found = c.iter.findData(vm.Intrinsic(IntrinsicArrayPrototype), vm.WellKnownSymbol(SymbolIterator)) &&
			c.next.findData(vm.Intrinsic(IntrinsicArrayIteratorPrototype), vm.Key(keyNext))
	}
	// Assigning to an existing data property does not move protoEpoch, so the values are
	// compared on every call.
	return c.found && c.iter.value().o == vm.Intrinsic(IntrinsicArrayPrototypeValues) &&
		c.next.value().o == vm.Intrinsic(IntrinsicArrayIteratorPrototypeNext)
}

// arrayIterCache is what arrayIterationPlain found for arrays with the initial shape.
type arrayIterCache struct {
	// iter and next are the data properties that iterating such an array reads.
	iter dataSlot
	next dataSlot

	// epoch is the value of VM.protoEpoch at which iter and next were found.
	epoch   uint32
	checked bool
	// found reports whether iter and next were found as data properties.
	found bool
}

// dataSlot is the slot of a data property in the object that holds it.
type dataSlot struct {
	holder *Object
	slot   int32
}

// findData looks key up on o and its prototypes, and reports whether it names a data property.
func (d *dataSlot) findData(o *Object, key *Object) bool {
	h, p, ok := find(o, key)
	if !ok || p.flags&FlagAccessor != 0 || p.slot < 0 {
		*d = dataSlot{}
		return false
	}
	*d = dataSlot{
		holder: h,
		slot:   p.slot,
	}
	return true
}

// value returns the current value of the property that findData found.
func (d *dataSlot) value() Value {
	return d.holder.slots[d.slot]
}

// Call calls the callable object f.
func (vm *VM) Call(f *Object, this Value, args ...Value) Value {
	return vm.callOnStack(f, this, args)
}

// CallValue calls f, which must be callable, or throws a TypeError.
func (vm *VM) CallValue(f, this Value, args ...Value) Value {
	if !f.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(f) + " is not a function")
	}
	return vm.callOnStack(f.o, this, args)
}

// callOnStack calls f with a copy of args on the [VM] stack, so that args does not escape.
func (vm *VM) callOnStack(f *Object, this Value, args []Value) Value {
	base := vm.sp
	n := len(args)
	if fn := f.fn; fn != nil && fn.bound == nil && fn.native == nil && !fn.classConstructor &&
		!fn.code.isGenerator && !fn.code.isAsync && !vm.interrupt.Load() && len(vm.frames) < maxFrames {
		// A plain closure gets its arguments written once, where its frame starts.
		vm.ensureStack(base + n + fn.code.nregs + 1)
		copy(vm.stack[base:], args)
		vm.pushFrame(f, this, base, n, 0, false, nil)
		r := vm.run(len(vm.frames) - 1)
		vm.sp = base
		return r
	}
	vm.ensureStack(base + n)
	copy(vm.stack[base:], args)
	vm.sp = base + n
	r := vm.callSlow(f, this, vm.stack[base:base+n:base+n], false, nil)
	vm.sp = base
	return r
}

// callSlow calls f with any kind of callee. With isNew set, this is the object under construction.
func (vm *VM) callSlow(f *Object, this Value, args []Value, isNew bool, newTarget *Object) Value {
	if vm.interrupt.Load() {
		vm.Poll()
	}
	fn := f.fn
	if fn == nil {
		vm.ThrowTypeError(vm.DisplayString(Value{o: f}) + " is not a function")
	}
	if fn.bound != nil {
		return vm.callBound(f, args)
	}
	if fn.native != nil {
		if fn.info != nil && fn.info.realm != vm.curRealm {
			return vm.callNativeInRealm(f, this, args, nil)
		}
		saved := vm.newTarget
		vm.newTarget = nil
		vm.checkNatives()
		vm.pushNative(f, this, false)
		r := fn.native(vm, this, args)
		vm.popNative()
		vm.newTarget = saved
		return r
	}
	c := fn.code
	if fn.classConstructor && !isNew && newTarget == nil {
		// The error belongs to the realm of the constructor.
		if c.realm != vm.curRealm {
			vm.switchRealm(c.realm)
		}
		vm.ThrowTypeError("class constructor " + wtf8.ToValidUTF8(c.name) + " cannot be invoked without 'new'")
	}
	if c.isGenerator || c.isAsync {
		return vm.callCoroutine(f, this, args)
	}
	if len(vm.frames) >= maxFrames {
		vm.ThrowRangeError("maximum call stack size exceeded")
	}
	base := vm.sp
	vm.ensureStack(base + len(args) + c.nregs + 1)
	copy(vm.stack[base:], args)
	vm.pushFrame(f, this, base, len(args), 0, isNew, newTarget)
	r := vm.run(len(vm.frames) - 1)
	vm.sp = base
	return r
}

// callBound calls the bound function f, unwrapping a chain of bound functions without recursion.
func (vm *VM) callBound(f *Object, args []Value) Value {
	var this Value
	base := vm.sp
	for b := f.fn.bound; b != nil; b = f.fn.bound {
		if k := len(b.args); k > 0 {
			// The combined arguments go on the stack above its top. args may start at the top,
			// so it moves up before the bound arguments are written.
			top := vm.sp
			n := k + len(args)
			vm.ensureStack(top + n)
			s := vm.stack[top : top+n : top+n]
			copy(s[k:], args)
			copy(s, b.args)
			vm.sp = top + n
			args = s
		}
		f, this = b.target, b.this
	}
	r := vm.callSlow(f, this, args, false, nil)
	vm.sp = base
	return r
}

// callNativeInRealm calls the native function f, whose realm is not the current one, in its realm.
// newTarget is nil for a call and the new.target of a construction otherwise.
func (vm *VM) callNativeInRealm(f *Object, this Value, args []Value, newTarget *Object) Value {
	fn := f.fn
	savedRealm, savedTarget := vm.curRealm, vm.newTarget
	vm.switchRealm(fn.info.realm)
	defer vm.switchRealm(savedRealm)
	vm.newTarget = newTarget
	vm.checkNatives()
	vm.pushNative(f, this, newTarget != nil)
	r := fn.native(vm, this, args)
	vm.popNative()
	vm.newTarget = savedTarget
	return r
}

// Construct implements Construct: new f(...args) with the given new.target.
func (vm *VM) Construct(f *Object, args []Value, newTarget *Object) *Object {
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
	if fn.native != nil {
		var r Value
		if fn.info != nil && fn.info.realm != vm.curRealm {
			r = vm.callNativeInRealm(f, Undefined, args, newTarget)
		} else {
			saved := vm.newTarget
			vm.newTarget = newTarget
			vm.checkNatives()
			vm.pushNative(f, Undefined, true)
			r = fn.native(vm, Undefined, args)
			vm.popNative()
			vm.newTarget = saved
		}
		if !r.IsObject() {
			vm.ThrowTypeError("constructor did not return an object")
		}
		return r.o
	}
	this := tdz
	if !fn.derived {
		this = Value{o: newInstance(vm.shapeFor(vm.GetPrototypeFromConstructor(newTarget, vm.Intrinsic(IntrinsicObjectPrototype))))}
	}
	r := vm.callSlow(f, this, args, !fn.derived, newTarget)
	if !r.IsObject() {
		vm.ThrowTypeError("constructor did not return an object")
	}
	return r.o
}

// GetPrototypeFromConstructor implements GetPrototypeFromConstructor.
func (vm *VM) GetPrototypeFromConstructor(ctor *Object, fallback *Object) *Object {
	if ctor == nil {
		return fallback
	}
	if p := vm.GetV(Value{o: ctor}, vm.Key(KeyPrototype)); p.IsObject() {
		return p.o
	}
	if r := vm.getFunctionRealm(ctor); r != vm.curRealm {
		return vm.intrinsicOfRealm(r, fallback)
	}
	return fallback
}

// intrinsicOfRealm returns the intrinsic of the realm r that corresponds to the intrinsic o of
// the current realm, or o when o is not one.
func (vm *VM) intrinsicOfRealm(r *realm, o *Object) *Object {
	for i, p := range vm.intrinsics {
		if p == o {
			if q := r.intrinsics[i]; q != nil {
				return q
			}
			break
		}
	}
	return o
}

// NewNative returns a function implemented in Go.
func (vm *VM) NewNative(name wtf8.String, length int, f NativeFunc) *Object {
	n := &nativeObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.function,
		state: function{
			native: f,
		},
		info: nativeInfo{
			name:   name,
			length: int32(length),
			realm:  vm.curRealm,
		},
	}
	n.fn = &n.state
	n.state.info = &n.info
	return n.init()
}

// nativeObject is a native function object together with its function state and description,
// so that creating one is a single allocation.
type nativeObject struct {
	propertyObject
	state function
	info  nativeInfo
}

// NewNativeConstructor returns a function implemented in Go that can be called with new.
// f learns the constructor it is constructing for from [VM.GetNewTarget].
func (vm *VM) NewNativeConstructor(name wtf8.String, length int, f NativeFunc) *Object {
	o := vm.NewNative(name, length, f)
	o.fn.constructor = true
	return o
}

// GetNewTarget implements GetNewTarget for the running native function: it returns the
// constructor the function is being constructed for, or nil when it was called.
func (vm *VM) GetNewTarget() *Object {
	return vm.newTarget
}

// findScope returns the innermost of the depth innermost dynamic scopes of the running
// function cl that has the variable key, or nil.
func (vm *VM) findScope(cl *Object, key *Object, depth int) *Object {
	k := PropKey{key: key}
	for _, scopes := range [...][]*Object{vm.frames[len(vm.frames)-1].scopes, cl.fn.capturedScopes()} {
		for _, o := range slices.Backward(scopes) {
			if depth == 0 {
				return nil
			}
			depth--
			if !vm.HasProperty(o, k) {
				continue
			}
			if o.class == classEvalScope {
				return o
			}
			if u := vm.getProp(o, PropKey{key: vm.WellKnownSymbol(SymbolUnscopables)}, Value{o: o}); u.IsObject() {
				if ToBoolean(vm.getProp(u.o, k, u)) {
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
func (vm *VM) openScopes(cl *Object) *[]*Object {
	scopes := slices.Concat(cl.fn.capturedScopes(), vm.frames[len(vm.frames)-1].scopes)
	if len(scopes) == 0 {
		return nil
	}
	return &scopes
}

// getScoped implements GetBindingValue of the object environment record of the dynamic scope o.
func (vm *VM) getScoped(o *Object, key *Object, strict bool) Value {
	k := PropKey{key: key}
	if !vm.HasProperty(o, k) {
		if strict {
			vm.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
		}
		return Undefined
	}
	return vm.getProp(o, k, Value{o: o})
}

// setScoped implements SetMutableBinding of the object environment record of the dynamic scope o.
func (vm *VM) setScoped(o *Object, key *Object, val Value, strict bool) {
	k := PropKey{key: key}
	if !vm.HasProperty(o, k) && strict {
		vm.ThrowReferenceError(wtf8.ToValidUTF8(key.str) + " is not defined")
	}
	if !vm.setProp(o, k, val, Value{o: o}) && strict {
		vm.throwReadOnly(Value{o: o}, k)
	}
}

// deleteName deletes the global variable key.
func (vm *VM) deleteName(key *Object) bool {
	if _, ok := vm.globalLex.shape.lookup(key); ok {
		return false
	}
	return vm.DeleteProp(vm.global, PropKey{key: key})
}

// BoundFunctionCreate implements BoundFunctionCreate. It copies args.
func (vm *VM) BoundFunctionCreate(target *Object, this Value, args []Value) *Object {
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
	b.shape = vm.shapeFor(vm.ProtoOf(target))
	b.slots = b.props[:0]
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
