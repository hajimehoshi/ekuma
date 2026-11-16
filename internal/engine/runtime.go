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
)

func (vm *VM) throwTDZ(name *Object) {
	if name.str == "this" {
		vm.ThrowReferenceError("must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	vm.ThrowReferenceError("cannot access '" + name.str + "' before initialization")
}

func (vm *VM) throwReadOnly(v Value, k PropKey) {
	vm.ThrowTypeError("cannot assign to read only property '" + vm.KeyString(k) + "' of " + vm.DisplayString(v))
}

// callee names the function that the call instruction at pc calls, for an error message.
func (vm *VM) calleeName(fp *funcProto, pc int) string {
	target := fp.code[pc].A
	for j := pc - 1; j >= 0 && j >= pc-64; j-- {
		in := fp.code[j]
		if in.A != target {
			continue
		}
		switch in.Op {
		case bytecode.OpGetProp:
			return fp.keys[in.C].str
		case bytecode.OpGetGlobal:
			return fp.keys[in.B].str
		}
		break
	}
	return ""
}

func (vm *VM) throwNotCallable(fp *funcProto, pc int, regs []Value) {
	name := vm.calleeName(fp, pc)
	if name == "" {
		name = vm.DisplayString(regs[fp.code[pc].A])
	}
	vm.ThrowTypeError(name + " is not a function")
}

func (vm *VM) throwNotConstructor(fp *funcProto, pc int, regs []Value) {
	name := vm.calleeName(fp, pc)
	if name == "" {
		name = vm.DisplayString(regs[fp.code[pc].A])
	}
	vm.ThrowTypeError(name + " is not a constructor")
}

// getGlobal returns the global variable key. When it does not exist, it throws a ReferenceError
// unless orUndef is set.
func (vm *VM) getGlobal(key *Object, c *propCache, orUndef bool) Value {
	if p, ok := vm.globalLex.shape.lookup(key); ok {
		v := vm.globalLex.slots[p.slot]
		if v.o == tdzObject {
			vm.throwTDZ(key)
		}
		c.holder = vm.globalLex
		c.shape = vm.globalLex.shape
		c.slot = p.slot
		c.kind = cacheOwn
		if p.flags&FlagWritable == 0 {
			c.kind = cacheProto
		}
		c.epoch = vm.globalEpoch
		return v
	}
	g := vm.global
	if p, ok := g.shape.lookup(key); ok && p.flags&FlagAccessor == 0 {
		c.holder = g
		c.shape = g.shape
		c.slot = p.slot
		c.kind = cacheOwn
		if p.flags&FlagWritable == 0 {
			c.kind = cacheProto
		}
		c.epoch = vm.globalEpoch
		return g.slots[p.slot]
	}
	k := PropKey{key: key}
	if !vm.HasProperty(g, k) {
		if orUndef {
			return Undefined
		}
		vm.ThrowReferenceError(key.str + " is not defined")
	}
	// GetBindingValue checks for the property again, which only a proxy can observe.
	if proxyOnChain(g) != nil && !vm.HasProperty(g, k) {
		if vm.frames[len(vm.frames)-1].fp.strict {
			vm.ThrowReferenceError(key.str + " is not defined")
		}
		return Undefined
	}
	return vm.getProp(g, k, Value{o: g})
}

// resolveGlobal returns whether the global variable key exists.
func (vm *VM) resolveGlobal(key *Object, c *propCache) Value {
	if p, ok := vm.globalLex.shape.lookup(key); ok {
		c.holder = vm.globalLex
		c.shape = vm.globalLex.shape
		c.slot = p.slot
		c.kind = cacheProto
		c.epoch = vm.globalEpoch
		return True
	}
	g := vm.global
	if p, ok := g.shape.lookup(key); ok {
		c.holder = g
		c.shape = g.shape
		c.slot = p.slot
		c.kind = cacheProto
		c.epoch = vm.globalEpoch
		return True
	}
	return Bool(vm.HasProperty(g, PropKey{key: key}))
}

// setGlobal assigns to the global variable key.
func (vm *VM) setGlobal(key *Object, val Value, c *propCache, strict bool) {
	if p, ok := vm.globalLex.shape.lookup(key); ok {
		if vm.globalLex.slots[p.slot].o == tdzObject {
			vm.throwTDZ(key)
		}
		if p.flags&FlagWritable == 0 {
			vm.ThrowTypeError("assignment to constant variable '" + key.str + "'")
		}
		vm.globalLex.slots[p.slot] = val
		c.holder = vm.globalLex
		c.shape = vm.globalLex.shape
		c.slot = p.slot
		c.kind = cacheOwn
		c.epoch = vm.globalEpoch
		return
	}
	g := vm.global
	if p, ok := g.shape.lookup(key); ok && p.flags&(FlagAccessor|FlagWritable) == FlagWritable {
		g.slots[p.slot] = val
		c.holder = g
		c.shape = g.shape
		c.slot = p.slot
		c.kind = cacheOwn
		c.epoch = vm.globalEpoch
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
			vm.ThrowReferenceError(key.str + " is not defined")
		}
	}
	if !vm.setProp(g, k, val, Value{o: g}) && strict {
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
			vm.throwSyntaxError("identifier '" + key.str + "' has already been declared")
		}
		if !vm.HasOwnProperty(vm.global, PropKey{key: key}) && !vm.IsExtensible(vm.global) {
			vm.ThrowTypeError("cannot declare global variable '" + key.str + "'")
		}
	case bytecode.DeclLexical:
		if lex {
			vm.throwSyntaxError("identifier '" + key.str + "' has already been declared")
		}
		// A lexical declaration may not hide a global property that cannot be reconfigured.
		if d, ok := vm.GetOwnProperty(vm.global, PropKey{key: key}); ok && d.flags&FlagConfigurable == 0 {
			vm.throwSyntaxError("identifier '" + key.str + "' has already been declared")
		}
	case bytecode.DeclFunction:
		if lex {
			vm.throwSyntaxError("identifier '" + key.str + "' has already been declared")
		}
		d, ok := vm.GetOwnProperty(vm.global, PropKey{key: key})
		switch {
		case !ok:
			if !vm.IsExtensible(vm.global) {
				vm.ThrowTypeError("cannot declare global function '" + key.str + "'")
			}
		case d.flags&FlagConfigurable != 0:
		case d.flags&FlagAccessor != 0 || d.flags&(FlagWritable|FlagEnumerable) != FlagWritable|FlagEnumerable:
			vm.ThrowTypeError("cannot declare global function '" + key.str + "'")
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
	vm.globalEpoch++
}

func (vm *VM) initGlobalLex(key *Object, val Value) {
	p, _ := vm.globalLex.shape.lookup(key)
	vm.globalLex.slots[p.slot] = val
}

// getValue returns v[k], or the getter to call.
func (vm *VM) getValue(v, k Value) (Value, *Object) {
	if ko := k.o; ko == nil {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return vm.typedArrayGet(o, i), nil
			}
			return vm.getIndex(v, i)
		}
	} else if ko.kind == kindString {
		// An element in elems is an own data property, whatever object v is.
		if i, ok := ko.knownArrayIndex(); ok {
			if o := v.o; o != nil && int64(i) < int64(len(o.elems)) {
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

// setValue stores v[k] = val. The results are those of setNamed.
func (vm *VM) setValue(v, k, val Value) (*Object, bool) {
	if ko := k.o; ko == nil {
		if i, ok := numberIndex(k.n); ok {
			if o := v.o; o != nil && o.class == classTypedArray {
				return nil, vm.typedArraySet(o, i, val)
			}
			return vm.setIndex(v, i, val)
		}
	} else if ko.kind == kindString {
		// An element in elems is a writable own data property, whatever object v is.
		if i, ok := ko.knownArrayIndex(); ok {
			if o := v.o; o != nil && int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
				o.elems[i] = val
				return nil, true
			}
		}
	}
	if v.IsNullish() {
		vm.ThrowTypeError("cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.DisplayString(k) + "')")
	}
	key := vm.ToPropertyKey(k)
	if key.key == nil {
		return vm.setIndex(v, key.idx, val)
	}
	return vm.setNamed(v, key.key, val, nil)
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
		vm.ThrowTypeError("cannot delete property '" + vm.KeyString(key) + "' of " + vm.DisplayString(v))
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

// instanceOf implements InstanceofOperator.
func (vm *VM) instanceOf(v, c Value) bool {
	if !c.IsObject() {
		vm.ThrowTypeError("right-hand side of 'instanceof' is not an object")
	}
	if m := vm.GetMethod(c, vm.WellKnownSymbol(SymbolHasInstance)); m != nil {
		if m == vm.Intrinsic(IntrinsicFunctionPrototypeHasInstance) {
			return vm.OrdinaryHasInstance(c, v)
		}
		return ToBoolean(vm.Call(m, c, v))
	}
	if !c.IsCallable() {
		vm.ThrowTypeError("right-hand side of 'instanceof' is not callable")
	}
	return vm.OrdinaryHasInstance(c, v)
}

// OrdinaryHasInstance implements OrdinaryHasInstance.
func (vm *VM) OrdinaryHasInstance(c, v Value) bool {
	if !c.IsCallable() {
		return false
	}
	if b := c.o.fn.bound; b != nil {
		return vm.instanceOf(v, Value{o: b.target})
	}
	if !v.IsObject() {
		return false
	}
	p := vm.GetV(c, vm.Key(KeyPrototype))
	if !p.IsObject() {
		vm.ThrowTypeError("function has non-object prototype in instanceof check")
	}
	for o := vm.ProtoOf(v.o); o != nil; o = vm.ProtoOf(o) {
		if o == p.o {
			return true
		}
	}
	return false
}

// getPrivate returns the private element name of v, or the getter to call.
func (vm *VM) getPrivate(v Value, name *Object) (Value, *Object) {
	if v.IsObject() {
		if p, ok := v.o.shape.lookup(name); ok {
			val := v.o.slots[p.slot]
			if p.flags&FlagAccessor != 0 {
				g := val.accessor().getter
				if g == nil {
					vm.ThrowTypeError("'#" + name.str + "' was defined without a getter")
				}
				return Undefined, g
			}
			return val, nil
		}
	}
	vm.ThrowTypeError("cannot read private member #" + name.str + " from an object whose class did not declare it")
	return Undefined, nil
}

// setPrivate stores val as the private element name of v, or returns the setter to call.
func (vm *VM) setPrivate(v Value, name *Object, val Value) *Object {
	if v.IsObject() {
		if p, ok := v.o.shape.lookup(name); ok {
			if p.flags&FlagAccessor != 0 {
				s := v.o.slots[p.slot].accessor().setter
				if s == nil {
					vm.ThrowTypeError("'#" + name.str + "' was defined without a setter")
				}
				return s
			}
			if p.flags&FlagWritable == 0 {
				vm.ThrowTypeError("private method '#" + name.str + "' is not writable")
			}
			v.o.slots[p.slot] = val
			return nil
		}
	}
	vm.ThrowTypeError("cannot write private member #" + name.str + " to an object whose class did not declare it")
	return nil
}

// definePrivate adds the private element name to v. val is the field value, a method, or an accessor.
func (vm *VM) definePrivate(v Value, name *Object, val Value) {
	if !v.IsObject() {
		vm.ThrowTypeError("cannot define a private member on a non-object")
	}
	o := v.o
	if _, ok := o.shape.lookup(name); ok {
		vm.ThrowTypeError("cannot initialize #" + name.str + " twice on the same object")
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
	if v.IsObject() && !vm.IsExtensible(v.o) {
		vm.ThrowTypeError("cannot define private member #" + name.str + " on an object that is not extensible")
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
	if x.o == nil && y.o == nil {
		a, b := x.n, y.n
		switch op {
		case bytecode.OpSub:
			return Value{n: a - b}
		case bytecode.OpMul:
			return Value{n: a * b}
		case bytecode.OpDiv:
			return Value{n: a / b}
		case bytecode.OpMod:
			return Value{n: math.Mod(a, b)}
		case bytecode.OpExp:
			return Value{n: NumberPow(a, b)}
		case bytecode.OpBitOr:
			return Value{n: float64(numconv.ToInt32(a) | numconv.ToInt32(b))}
		case bytecode.OpBitAnd:
			return Value{n: float64(numconv.ToInt32(a) & numconv.ToInt32(b))}
		case bytecode.OpBitXor:
			return Value{n: float64(numconv.ToInt32(a) ^ numconv.ToInt32(b))}
		case bytecode.OpShl:
			return Value{n: float64(numconv.ToInt32(a) << (numconv.ToUint32(b) & 31))}
		case bytecode.OpShr:
			return Value{n: float64(numconv.ToInt32(a) >> (numconv.ToUint32(b) & 31))}
		case bytecode.OpUShr:
			return Value{n: float64(numconv.ToUint32(a) >> (numconv.ToUint32(b) & 31))}
		}
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	if x.o == nil || y.o == nil {
		vm.ThrowTypeError("cannot mix BigInt and other types, use explicit conversions")
	}
	a, b := x.BigInt(), y.BigInt()
	r := new(big.Int)
	switch op {
	case bytecode.OpSub:
		r.Sub(a, b)
	case bytecode.OpMul:
		r.Mul(a, b)
	case bytecode.OpDiv:
		if b.Sign() == 0 {
			vm.ThrowRangeError("division by zero")
		}
		r.Quo(a, b)
	case bytecode.OpMod:
		if b.Sign() == 0 {
			vm.ThrowRangeError("division by zero")
		}
		r.Rem(a, b)
	case bytecode.OpExp:
		if b.Sign() < 0 {
			vm.ThrowRangeError("exponent must be non-negative")
		}
		// The result of a base other than 0, 1 and -1 has about BitLen(a)*b bits.
		const maxExpBits = 1 << 25
		if a.CmpAbs(big.NewInt(1)) > 0 &&
			(!b.IsInt64() || b.Int64() > maxExpBits || int64(a.BitLen())*b.Int64() > maxExpBits) {
			vm.ThrowRangeError("maximum BigInt size exceeded")
		}
		r.Exp(a, b, nil)
	case bytecode.OpBitOr:
		r.Or(a, b)
	case bytecode.OpBitAnd:
		r.And(a, b)
	case bytecode.OpBitXor:
		r.Xor(a, b)
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
	if x.o == nil {
		return Value{n: -x.n}
	}
	return vm.NewBigInt(new(big.Int).Neg(x.BigInt()))
}

func (vm *VM) bitNot(x Value) Value {
	if i, ok := x.BigInt64(); ok {
		return BigIntFromInt64(^i)
	}
	x = vm.ToNumeric(x)
	if x.o == nil {
		return Value{n: float64(^numconv.ToInt32(x.n))}
	}
	return vm.NewBigInt(new(big.Int).Not(x.BigInt()))
}

// step implements ++ and -- for an operand that is not a number: it adds d to x.
func (vm *VM) step(x Value, d int64) Value {
	if i, ok := x.BigInt64(); ok {
		if s, ok := add64(i, d); ok {
			return BigIntFromInt64(s)
		}
	}
	x = vm.ToNumeric(x)
	if x.o == nil {
		return Value{n: x.n + float64(d)}
	}
	return vm.NewBigInt(new(big.Int).Add(x.BigInt(), big.NewInt(d)))
}

// classElement is an element that the constructor of a class adds to every instance.
type classElement struct {
	kind int
	key  Value
	val  *Object

	// dec is what the decorators of a field add to it, or nil.
	dec *fieldDecoration
}

// addClassElement registers an instance element of the class ctor, or defines a static element at once.
// For a field, val is the function that computes the initial value, or nil.
func (vm *VM) addClassElement(ctor *Object, key Value, val *Object, kind int) {
	e := classElement{
		kind: kind &^ bytecode.ElemStatic,
		key:  key,
		val:  val,
	}
	if kind&bytecode.ElemStatic != 0 {
		vm.defineClassElement(Value{o: ctor}, e)
		return
	}
	if ctor.fn.fields == nil {
		ctor.fn.fields = &[]classElement{}
	}
	*ctor.fn.fields = append(*ctor.fn.fields, e)
}

// initFields defines the instance elements of the class ctor on this.
func (vm *VM) initFields(ctor *Object, this Value) {
	// Private methods and accessors exist before any field initializer runs.
	checked := false
	for _, e := range *ctor.fn.fields {
		if e.kind != bytecode.ElemField && e.kind != elemInitializer {
			if !checked {
				vm.ensureCanAddPrivate(this, e.key.o)
				checked = true
			}
			vm.defineClassElement(this, e)
		}
	}
	for _, e := range *ctor.fn.fields {
		switch e.kind {
		case bytecode.ElemField:
			vm.defineClassElement(this, e)
		case elemInitializer:
			vm.Call(e.val, this)
		}
	}
}

func (vm *VM) defineClassElement(this Value, e classElement) {
	private := e.key.o != nil && e.key.o.class == classPrivateName
	switch e.kind {
	case bytecode.ElemField:
		v := Undefined
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
			vm.CreateDataPropertyOrThrow(this.o, vm.ToPropertyKey(e.key), v)
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
			if p, ok := this.o.shape.lookup(e.key.o); ok && p.flags&FlagAccessor != 0 {
				a := this.o.slots[p.slot].accessor()
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
	return Value{o: vm.Intern(typeOf(v))}
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

// defineMethod defines the method f under the key k of o.
func (vm *VM) defineMethod(o *Object, k, f Value) {
	f.o.fn.home = o
	vm.DefinePropertyOrThrow(o, vm.ToPropertyKey(k), dataDesc(f, FlagsHidden))
}

// defineAccessor defines the getter or setter f under the key k of o.
func (vm *VM) defineAccessor(o *Object, k Value, f *Object, isSetter, enumerable bool) {
	f.fn.home = o
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
func (vm *VM) funcNameOfKey(k Value) string {
	switch {
	case k.IsSymbol():
		if k.o.class == classPrivateName {
			return "#" + k.o.str
		}
		if k.o.ext != nil {
			return "[" + k.o.str + "]"
		}
		return ""
	case k.IsString():
		return k.o.Str()
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
	o := f.o
	if o.fn.materialized {
		if _, ok := o.shape.lookup(vm.Key(KeyName)); ok {
			if d, _ := vm.GetOwnProperty(o, PropKey{key: vm.Key(KeyName)}); d.flags&FlagAccessor != 0 || !d.value.IsString() || d.value.o.str != "" {
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
keys:
	for _, k := range ks {
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
	fn := ctor.fn
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
	if b := fn.bound; b != nil {
		if len(b.args) > 0 {
			args = append(append(make([]Value, 0, len(b.args)+len(args)), b.args...), args...)
		}
		return vm.callSlow(b.target, b.this, args, false, nil)
	}
	if fn.native != nil {
		if fn.info != nil && fn.info.realm != vm.curRealm {
			return vm.callNativeInRealm(f, this, args, nil)
		}
		saved := vm.newTarget
		vm.newTarget = nil
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
		vm.ThrowTypeError("class constructor " + c.name + " cannot be invoked without 'new'")
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

// callNativeInRealm calls the native function f, whose realm is not the current one, in its realm.
// newTarget is nil for a call and the new.target of a construction otherwise.
func (vm *VM) callNativeInRealm(f *Object, this Value, args []Value, newTarget *Object) Value {
	fn := f.fn
	savedRealm, savedTarget := vm.curRealm, vm.newTarget
	vm.switchRealm(fn.info.realm)
	defer vm.switchRealm(savedRealm)
	vm.newTarget = newTarget
	vm.pushNative(f, this, newTarget != nil)
	r := fn.native(vm, this, args)
	vm.popNative()
	vm.newTarget = savedTarget
	return r
}

// Construct implements Construct: new f(...args) with the given new.target.
func (vm *VM) Construct(f *Object, args []Value, newTarget *Object) *Object {
	fn := f.fn
	if b := fn.bound; b != nil {
		if len(b.args) > 0 {
			args = append(append(make([]Value, 0, len(b.args)+len(args)), b.args...), args...)
		}
		if newTarget == f {
			newTarget = b.target
		}
		return vm.Construct(b.target, args, newTarget)
	}
	if fn.native != nil {
		var r Value
		if fn.info != nil && fn.info.realm != vm.curRealm {
			r = vm.callNativeInRealm(f, Undefined, args, newTarget)
		} else {
			saved := vm.newTarget
			vm.newTarget = newTarget
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
		this = Value{o: vm.NewObject(vm.GetPrototypeFromConstructor(newTarget, vm.Intrinsic(IntrinsicObjectPrototype)))}
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
func (vm *VM) NewNative(name string, length int, f NativeFunc) *Object {
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
	return &n.Object
}

// nativeObject is a native function object together with its function state and description,
// so that creating one is a single allocation.
type nativeObject struct {
	Object
	state function
	info  nativeInfo
}

// NewNativeConstructor returns a function implemented in Go that can be called with new.
// f learns the constructor it is constructing for from [VM.GetNewTarget].
func (vm *VM) NewNativeConstructor(name string, length int, f NativeFunc) *Object {
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
			vm.ThrowReferenceError(key.str + " is not defined")
		}
		return Undefined
	}
	return vm.getProp(o, k, Value{o: o})
}

// setScoped implements SetMutableBinding of the object environment record of the dynamic scope o.
func (vm *VM) setScoped(o *Object, key *Object, val Value, strict bool) {
	k := PropKey{key: key}
	if !vm.HasProperty(o, k) && strict {
		vm.ThrowReferenceError(key.str + " is not defined")
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
	return &Object{
		kind:  kindFunction,
		class: classFunction,
		shape: vm.shapeFor(vm.ProtoOf(target)),
		fn: &function{
			bound: &boundFunc{
				target: target,
				this:   this,
				args:   append([]Value(nil), args...),
			},
			materialized: true,
			constructor:  vm.IsConstructor(target),
		},
	}
}

// SourceText returns the result of Function.prototype.toString for the function f.
func (f *Object) SourceText() string {
	fn := f.fn
	if fn.code != nil && fn.code.Source != "" {
		return fn.code.Source
	}
	name := fn.nativeName()
	if fn.code != nil {
		name = fn.code.name
	}
	// The name is a PropertyName after an optional accessor word, and is left out when it is not.
	n := strings.TrimPrefix(strings.TrimPrefix(name, "get "), "set ")
	if !parser.IsIdentifierName(n) && !strings.HasPrefix(n, "[") {
		name = ""
	}
	return "function " + name + "() { [native code] }"
}
