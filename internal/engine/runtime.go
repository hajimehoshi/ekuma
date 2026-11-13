// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

func (vm *VM) throwTDZ(name *Object) {
	if name.Str == "this" {
		vm.throwReferenceError("Must call super constructor in derived class before accessing 'this' or returning from derived constructor")
	}
	vm.throwReferenceError("Cannot access '" + name.Str + "' before initialization")
}

func (vm *VM) throwReadOnly(v Value, k PropKey) {
	vm.ThrowTypeError("Cannot assign to read only property '" + vm.KeyString(k) + "' of " + vm.DisplayString(v))
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
			return fp.keys[in.C].Str
		case bytecode.OpGetGlobal:
			return fp.keys[in.B].Str
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
	if p, ok := vm.globalLex.Shape.Lookup(key); ok {
		v := vm.globalLex.Slots[p.Slot]
		if v.O == tdzObject {
			vm.throwTDZ(key)
		}
		c.holder = vm.globalLex
		c.shape = vm.globalLex.Shape
		c.slot = p.Slot
		c.kind = cacheOwn
		if p.Flags&FlagWritable == 0 {
			c.kind = cacheProto
		}
		c.epoch = vm.globalEpoch
		return v
	}
	g := vm.Global
	if p, ok := g.Shape.Lookup(key); ok && p.Flags&FlagAccessor == 0 {
		c.holder = g
		c.shape = g.Shape
		c.slot = p.Slot
		c.kind = cacheOwn
		if p.Flags&FlagWritable == 0 {
			c.kind = cacheProto
		}
		c.epoch = vm.globalEpoch
		return g.Slots[p.Slot]
	}
	k := PropKey{Key: key}
	if !vm.HasProperty(g, k) {
		if orUndef {
			return Undefined
		}
		vm.throwReferenceError(key.Str + " is not defined")
	}
	return vm.GetProp(g, k, Value{O: g})
}

// setGlobal assigns to the global variable key.
func (vm *VM) setGlobal(key *Object, val Value, c *propCache, strict bool) {
	if p, ok := vm.globalLex.Shape.Lookup(key); ok {
		if vm.globalLex.Slots[p.Slot].O == tdzObject {
			vm.throwTDZ(key)
		}
		if p.Flags&FlagWritable == 0 {
			vm.ThrowTypeError("Assignment to constant variable '" + key.Str + "'")
		}
		vm.globalLex.Slots[p.Slot] = val
		c.holder = vm.globalLex
		c.shape = vm.globalLex.Shape
		c.slot = p.Slot
		c.kind = cacheOwn
		c.epoch = vm.globalEpoch
		return
	}
	g := vm.Global
	if p, ok := g.Shape.Lookup(key); ok && p.Flags&(FlagAccessor|FlagWritable) == FlagWritable {
		g.Slots[p.Slot] = val
		c.holder = g
		c.shape = g.Shape
		c.slot = p.Slot
		c.kind = cacheOwn
		c.epoch = vm.globalEpoch
		return
	}
	k := PropKey{Key: key}
	if strict && !vm.HasProperty(g, k) {
		vm.throwReferenceError(key.Str + " is not defined")
	}
	if !vm.setProp(g, k, val, Value{O: g}) && strict {
		vm.throwReadOnly(Value{O: g}, k)
	}
}

// checkGlobalDecl throws when declaring key at the top level of a script would conflict
// with an existing declaration. what is 0 for var, 1 for a lexical declaration and 2 for a function.
func (vm *VM) checkGlobalDecl(key *Object, what int) {
	_, lex := vm.globalLex.Shape.Lookup(key)
	switch what {
	case 0:
		if lex {
			vm.ThrowSyntaxError("Identifier '" + key.Str + "' has already been declared")
		}
		if !vm.HasOwn(vm.Global, PropKey{Key: key}) && !vm.IsExtensible(vm.Global) {
			vm.ThrowTypeError("Cannot declare global variable '" + key.Str + "'")
		}
	case 1:
		if lex {
			vm.ThrowSyntaxError("Identifier '" + key.Str + "' has already been declared")
		}
		// A lexical declaration may not hide a global property that cannot be reconfigured.
		if d, ok := vm.GetOwnProperty(vm.Global, PropKey{Key: key}); ok && d.Flags&FlagConfigurable == 0 {
			vm.ThrowSyntaxError("Identifier '" + key.Str + "' has already been declared")
		}
	case 2:
		if lex {
			vm.ThrowSyntaxError("Identifier '" + key.Str + "' has already been declared")
		}
		d, ok := vm.GetOwnProperty(vm.Global, PropKey{Key: key})
		switch {
		case !ok:
			if !vm.IsExtensible(vm.Global) {
				vm.ThrowTypeError("Cannot declare global function '" + key.Str + "'")
			}
		case d.Flags&FlagConfigurable != 0:
		case d.Flags&FlagAccessor != 0 || d.Flags&(FlagWritable|FlagEnumerable) != FlagWritable|FlagEnumerable:
			vm.ThrowTypeError("Cannot declare global function '" + key.Str + "'")
		}
	}
}

// declareGlobalVar implements CreateGlobalVarBinding, and CreateGlobalFunctionBinding when isFunc is set.
func (vm *VM) declareGlobalVar(key *Object, isFunc bool, fn Value) {
	g := vm.Global
	k := PropKey{Key: key}
	d, ok := vm.GetOwnProperty(g, k)
	if !isFunc {
		if !ok {
			vm.DefineOrThrow(g, k, dataDesc(Undefined, FlagEnumerable|FlagWritable))
		}
		return
	}
	if !ok || d.Flags&FlagConfigurable != 0 {
		vm.DefineOrThrow(g, k, dataDesc(fn, FlagEnumerable|FlagWritable))
		return
	}
	vm.DefineOrThrow(g, k, PropDesc{
		Value: fn,
		Has:   HasValue,
	})
}

// declareGlobalLex creates the uninitialized global lexical binding key.
func (vm *VM) declareGlobalLex(key *Object, isConst bool) {
	flags := FlagsData
	if isConst {
		flags = FlagEnumerable | FlagConfigurable
	}
	vm.Add(vm.globalLex, key, tdz, flags)
	vm.globalEpoch++
}

func (vm *VM) initGlobalLex(key *Object, val Value) {
	p, _ := vm.globalLex.Shape.Lookup(key)
	vm.globalLex.Slots[p.Slot] = val
}

// getValue returns v[k], or the getter to call.
func (vm *VM) getValue(v, k Value) (Value, *Object) {
	if k.O == nil {
		if i, ok := numberIndex(k.N); ok {
			return vm.getIndex(v, i)
		}
	}
	// The receiver is checked before the key is converted.
	if v.IsNullish() {
		vm.ThrowTypeError("Cannot read properties of " + vm.DisplayString(v) + " (reading '" + vm.DisplayString(k) + "')")
	}
	key := vm.ToPropertyKey(k)
	if key.Key == nil {
		return vm.getIndex(v, key.Idx)
	}
	return vm.get(v, key.Key, nil)
}

// setValue stores v[k] = val. The results are those of set.
func (vm *VM) setValue(v, k, val Value) (*Object, bool) {
	if k.O == nil {
		if i, ok := numberIndex(k.N); ok {
			return vm.setIndex(v, i, val)
		}
	}
	if v.IsNullish() {
		vm.ThrowTypeError("Cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.DisplayString(k) + "')")
	}
	key := vm.ToPropertyKey(k)
	if key.Key == nil {
		return vm.setIndex(v, key.Idx, val)
	}
	return vm.set(v, key.Key, val, nil)
}

// deleteValue implements the delete operator on a property.
func (vm *VM) deleteValue(v, k Value, strict bool) Value {
	if v.IsNullish() {
		vm.ThrowTypeError("Cannot convert undefined or null to object")
	}
	key := vm.ToPropertyKey(k)
	o := vm.ToObject(v)
	ok := vm.DeleteProp(o, key)
	if !ok && strict {
		vm.ThrowTypeError("Cannot delete property '" + vm.KeyString(key) + "' of " + vm.DisplayString(v))
	}
	return Bool(ok)
}

// superBase returns the object that super property accesses start from, given the home object.
func (vm *VM) superBase(home Value, key PropKey) *Object {
	if !home.IsObject() {
		vm.ThrowSyntaxError("'super' keyword unexpected here")
	}
	base := vm.ProtoOf(home.O)
	if base == nil {
		vm.ThrowTypeError("Cannot access property '" + vm.KeyString(key) + "' of a null prototype")
	}
	return base
}

func (vm *VM) getSuper(home, k, this Value) Value {
	key := vm.ToPropertyKey(k)
	return vm.GetProp(vm.superBase(home, key), key, this)
}

func (vm *VM) setSuper(home, k, val, this Value, strict bool) {
	key := vm.ToPropertyKey(k)
	if !vm.setProp(vm.superBase(home, key), key, val, this) && strict {
		vm.throwReadOnly(this, key)
	}
}

// instanceOf implements InstanceofOperator.
func (vm *VM) instanceOf(v, c Value) bool {
	if !c.IsObject() {
		vm.ThrowTypeError("Right-hand side of 'instanceof' is not an object")
	}
	if m := vm.GetMethod(c, vm.K.SymHasInstance); m != nil {
		if m == vm.FnHasInstance {
			return vm.OrdinaryHasInstance(c, v)
		}
		return Truthy(vm.Call(m, c, v))
	}
	if !c.IsCallable() {
		vm.ThrowTypeError("Right-hand side of 'instanceof' is not callable")
	}
	return vm.OrdinaryHasInstance(c, v)
}

// ordinaryHasInstance implements OrdinaryHasInstance.
func (vm *VM) OrdinaryHasInstance(c, v Value) bool {
	if !c.IsCallable() {
		return false
	}
	if b := c.O.Fn.Bound; b != nil {
		return vm.instanceOf(v, Value{O: b.Target})
	}
	if !v.IsObject() {
		return false
	}
	p := vm.GetV(c, vm.K.Prototype)
	if !p.IsObject() {
		vm.ThrowTypeError("Function has non-object prototype in instanceof check")
	}
	for o := vm.ProtoOf(v.O); o != nil; o = vm.ProtoOf(o) {
		if o == p.O {
			return true
		}
	}
	return false
}

// getPrivate returns the private element name of v, or the getter to call.
func (vm *VM) getPrivate(v Value, name *Object) (Value, *Object) {
	if v.IsObject() {
		if p, ok := v.O.Shape.Lookup(name); ok {
			val := v.O.Slots[p.Slot]
			if p.Flags&FlagAccessor != 0 {
				g := val.Accessor().Getter
				if g == nil {
					vm.ThrowTypeError("'#" + name.Str + "' was defined without a getter")
				}
				return Undefined, g
			}
			return val, nil
		}
	}
	vm.ThrowTypeError("Cannot read private member #" + name.Str + " from an object whose class did not declare it")
	return Undefined, nil
}

// setPrivate stores val as the private element name of v, or returns the setter to call.
func (vm *VM) setPrivate(v Value, name *Object, val Value) *Object {
	if v.IsObject() {
		if p, ok := v.O.Shape.Lookup(name); ok {
			if p.Flags&FlagAccessor != 0 {
				s := v.O.Slots[p.Slot].Accessor().setter
				if s == nil {
					vm.ThrowTypeError("'#" + name.Str + "' was defined without a setter")
				}
				return s
			}
			if p.Flags&FlagWritable == 0 {
				vm.ThrowTypeError("Private method '#" + name.Str + "' is not writable")
			}
			v.O.Slots[p.Slot] = val
			return nil
		}
	}
	vm.ThrowTypeError("Cannot write private member #" + name.Str + " to an object whose class did not declare it")
	return nil
}

// definePrivate adds the private element name to v. val is the field value, a method, or an accessor.
func (vm *VM) definePrivate(v Value, name *Object, val Value) {
	if !v.IsObject() {
		vm.ThrowTypeError("Cannot define a private member on a non-object")
	}
	o := v.O
	if _, ok := o.Shape.Lookup(name); ok {
		vm.ThrowTypeError("Cannot initialize #" + name.Str + " twice on the same object")
	}
	flags := FlagWritable
	switch {
	case val.O != nil && val.O.Kind == kindAccessor:
		flags = FlagAccessor
	case name.Flags&ObjFrozenLength != 0:
		// The name stands for a method, which cannot be assigned to.
		flags = 0
	}
	vm.Add(o, name, val, flags)
}

// arith implements the binary arithmetic operators other than + for operands that are not both numbers.
func (vm *VM) arith(op bytecode.Opcode, x, y Value) Value {
	x = vm.ToNumeric(x)
	y = vm.ToNumeric(y)
	if x.O == nil && y.O == nil {
		a, b := x.N, y.N
		switch op {
		case bytecode.OpSub:
			return Value{N: a - b}
		case bytecode.OpMul:
			return Value{N: a * b}
		case bytecode.OpDiv:
			return Value{N: a / b}
		case bytecode.OpMod:
			return Value{N: math.Mod(a, b)}
		case bytecode.OpExp:
			return Value{N: NumberPow(a, b)}
		case bytecode.OpBitOr:
			return Value{N: float64(numconv.ToInt32(a) | numconv.ToInt32(b))}
		case bytecode.OpBitAnd:
			return Value{N: float64(numconv.ToInt32(a) & numconv.ToInt32(b))}
		case bytecode.OpBitXor:
			return Value{N: float64(numconv.ToInt32(a) ^ numconv.ToInt32(b))}
		case bytecode.OpShl:
			return Value{N: float64(numconv.ToInt32(a) << (numconv.ToUint32(b) & 31))}
		case bytecode.OpShr:
			return Value{N: float64(numconv.ToInt32(a) >> (numconv.ToUint32(b) & 31))}
		case bytecode.OpUShr:
			return Value{N: float64(numconv.ToUint32(a) >> (numconv.ToUint32(b) & 31))}
		}
		panic("ekuma: not an arithmetic bytecode.Opcode")
	}
	if x.O == nil || y.O == nil {
		vm.ThrowTypeError("Cannot mix BigInt and other types, use explicit conversions")
	}
	a, b := x.O.BigInt(), y.O.BigInt()
	r := new(big.Int)
	switch op {
	case bytecode.OpSub:
		r.Sub(a, b)
	case bytecode.OpMul:
		r.Mul(a, b)
	case bytecode.OpDiv:
		if b.Sign() == 0 {
			vm.ThrowRangeError("Division by zero")
		}
		r.Quo(a, b)
	case bytecode.OpMod:
		if b.Sign() == 0 {
			vm.ThrowRangeError("Division by zero")
		}
		r.Rem(a, b)
	case bytecode.OpExp:
		if b.Sign() < 0 {
			vm.ThrowRangeError("Exponent must be non-negative")
		}
		if !b.IsInt64() || (b.Int64() > 1<<24 && a.CmpAbs(big.NewInt(1)) > 0) {
			vm.ThrowRangeError("Maximum BigInt size exceeded")
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
				vm.ThrowRangeError("Maximum BigInt size exceeded")
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
	return vm.newBigInt(r)
}

func (vm *VM) negate(x Value) Value {
	x = vm.ToNumeric(x)
	if x.O == nil {
		return Value{N: -x.N}
	}
	return vm.newBigInt(new(big.Int).Neg(x.O.BigInt()))
}

func (vm *VM) bitNot(x Value) Value {
	x = vm.ToNumeric(x)
	if x.O == nil {
		return Value{N: float64(^numconv.ToInt32(x.N))}
	}
	return vm.newBigInt(new(big.Int).Not(x.O.BigInt()))
}

// step implements ++ and -- for an operand that is not a number: it adds d to x.
func (vm *VM) step(x Value, d int64) Value {
	x = vm.ToNumeric(x)
	if x.O == nil {
		return Value{N: x.N + float64(d)}
	}
	return vm.newBigInt(new(big.Int).Add(x.O.BigInt(), big.NewInt(d)))
}

// classElement is an element that the constructor of a class adds to every instance.
type classElement struct {
	kind int
	key  Value
	val  *Object
}

// Kinds of class elements. The compiler emits the same numbers.
const (
	elemField = iota
	elemMethod
	elemGetter
	elemSetter
	elemStatic = 8
)

// addClassElement registers an instance element of the class ctor, or defines a static element at once.
// For a field, val is the function that computes the initial value, or nil.
func (vm *VM) addClassElement(ctor *Object, key Value, val *Object, kind int) {
	e := classElement{
		kind: kind &^ elemStatic,
		key:  key,
		val:  val,
	}
	if kind&elemStatic != 0 {
		vm.defineClassElement(Value{O: ctor}, e)
		return
	}
	ctor.Fn.fields = append(ctor.Fn.fields, e)
}

// initFields defines the instance elements of the class ctor on this.
func (vm *VM) initFields(ctor *Object, this Value) {
	// Private methods and accessors exist before any field initializer runs.
	for _, e := range ctor.Fn.fields {
		if e.kind != elemField {
			vm.defineClassElement(this, e)
		}
	}
	for _, e := range ctor.Fn.fields {
		if e.kind == elemField {
			vm.defineClassElement(this, e)
		}
	}
}

func (vm *VM) defineClassElement(this Value, e classElement) {
	private := e.key.O != nil && e.key.O.Class == ClassPrivateName
	switch e.kind {
	case elemField:
		v := Undefined
		if e.val != nil {
			v = vm.Call(e.val, this)
		}
		if private {
			vm.definePrivate(this, e.key.O, v)
			return
		}
		vm.CreateDataProperty(this.O, vm.ToPropertyKey(e.key), v)
	case elemMethod:
		vm.definePrivate(this, e.key.O, Value{O: e.val})
	case elemGetter, elemSetter:
		if this.IsObject() {
			if p, ok := this.O.Shape.Lookup(e.key.O); ok && p.Flags&FlagAccessor != 0 {
				a := this.O.Slots[p.Slot].Accessor()
				if e.kind == elemGetter && a.Getter == nil {
					a.Getter = e.val
					return
				}
				if e.kind == elemSetter && a.setter == nil {
					a.setter = e.val
					return
				}
			}
		}
		if e.kind == elemGetter {
			vm.definePrivate(this, e.key.O, NewAccessor(e.val, nil))
		} else {
			vm.definePrivate(this, e.key.O, NewAccessor(nil, e.val))
		}
	}
}

func (vm *VM) typeofValue(v Value) Value {
	return Value{O: vm.Intern(typeOf(v))}
}

// defineField implements CreateDataPropertyOrThrow for a computed key, as object literals
// and public class fields use it.
func (vm *VM) defineField(o *Object, k, val Value) {
	vm.CreateDataProperty(o, vm.ToPropertyKey(k), val)
}

// defineFieldK defines the enumerable data property key of o.
func (vm *VM) defineFieldK(o *Object, key *Object, val Value, c *propCache) {
	if o.Class == classObject && o.Flags&(ObjNotExtensible|objIsProto|ObjExotic) == 0 {
		if _, ok := o.Shape.Lookup(key); !ok {
			old := o.Shape
			vm.Add(o, key, val, FlagsData)
			if !old.private && !o.Shape.private {
				c.shape = old
				c.kind = cacheAdd
				c.next = o.Shape
			}
			return
		}
	}
	vm.CreateDataProperty(o, PropKey{Key: key}, val)
}

// defineMethod defines the method f under the key k of o.
func (vm *VM) defineMethod(o *Object, k, f Value) {
	f.O.Fn.home = o
	vm.DefineOrThrow(o, vm.ToPropertyKey(k), dataDesc(f, FlagsHidden))
}

// defineAccessor defines the getter or setter f under the key k of o.
func (vm *VM) defineAccessor(o *Object, k Value, f *Object, isSetter, enumerable bool) {
	f.Fn.home = o
	d := PropDesc{
		Flags: FlagConfigurable,
		Has:   HasEnumerable | HasConfigurable,
	}
	if enumerable {
		d.Flags |= FlagEnumerable
	}
	if isSetter {
		d.Setter = f
		d.Has |= HasSet
	} else {
		d.Getter = f
		d.Has |= HasGet
	}
	vm.DefineOrThrow(o, vm.ToPropertyKey(k), d)
}

// Prefix kinds of SetFunctionName.
const (
	namePlain = iota
	nameGet
	nameSet
)

// setFuncName implements SetFunctionName for the function or class f, which must be anonymous.
func (vm *VM) setFuncName(f, k Value, prefix int) {
	if !f.IsCallable() {
		return
	}
	var name string
	switch {
	case k.IsSymbol():
		if k.O.Class == ClassPrivateName {
			name = "#" + k.O.Str
		} else if k.O.Ext != nil {
			name = "[" + k.O.Str + "]"
		}
	case k.IsString():
		name = k.O.Str
	default:
		name = vm.ToString(k)
	}
	switch prefix {
	case nameGet:
		name = "get " + name
	case nameSet:
		name = "set " + name
	}
	o := f.O
	if o.Fn.Materialized {
		if _, ok := o.Shape.Lookup(vm.K.Name); ok {
			if d, _ := vm.GetOwnProperty(o, PropKey{Key: vm.K.Name}); d.Flags&FlagAccessor != 0 || !d.Value.IsString() || d.Value.O.Str != "" {
				return
			}
		}
		vm.defineOwnProperty(o, PropKey{Key: vm.K.Name}, dataDesc(vm.NewString(name), FlagConfigurable))
		return
	}
	// The name is part of the code of an unmaterialized function, which closures share,
	// so give this one its properties now.
	vm.Materialize(o)
	vm.Define(o, vm.K.Name, vm.NewString(name), FlagConfigurable)
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
keys:
	for _, k := range vm.OwnKeys(from) {
		for _, s := range skip {
			if s == k {
				continue keys
			}
		}
		d, ok := vm.GetOwnProperty(from, k)
		if !ok || d.Flags&FlagEnumerable == 0 {
			continue
		}
		val := d.Value
		if d.Flags&FlagAccessor != 0 || from.Class == classProxy {
			val = vm.GetProp(from, k, Value{O: from})
		}
		vm.CreateDataProperty(dst, k, val)
	}
}

// setupClass makes the function ctor a class constructor that extends parent,
// which is hole for a class without an extends clause. It returns the prototype object.
func (vm *VM) setupClass(ctor *Object, parent Value) *Object {
	protoParent := vm.ObjectProto
	ctorParent := vm.FunctionProto
	if !parent.IsHole() {
		switch {
		case parent.IsNull():
			protoParent = nil
		case !parent.IsCallable() || !vm.IsConstructor(parent.O):
			vm.ThrowTypeError("Class extends value " + vm.DisplayString(parent) + " is not a constructor or null")
		default:
			p := vm.GetV(parent, vm.K.Prototype)
			switch {
			case p.IsObject():
				protoParent = p.O
			case p.IsNull():
				protoParent = nil
			default:
				vm.ThrowTypeError("Class extends value does not have valid prototype property")
			}
			ctorParent = parent.O
		}
	}
	proto := vm.NewObject(protoParent)
	fn := ctor.Fn
	fn.Materialized = true
	fn.home = proto
	ctor.Flags &^= ObjExotic
	ctor.Shape = vm.ShapeFor(ctorParent)
	vm.Add(ctor, vm.K.Length, Number(float64(fn.Code.length)), FlagConfigurable)
	vm.Add(ctor, vm.K.Name, vm.NewString(fn.Code.Name), FlagConfigurable)
	vm.Add(ctor, vm.K.Prototype, Value{O: proto}, 0)
	vm.Add(proto, vm.K.Constructor, Value{O: ctor}, FlagsHidden)
	return proto
}

// isConstructor implements IsConstructor.
func (vm *VM) IsConstructor(f *Object) bool {
	fn := f.Fn
	if fn == nil {
		return false
	}
	if fn.Bound != nil {
		return vm.IsConstructor(fn.Bound.Target)
	}
	if f.Class == classProxy {
		return vm.proxyIsConstructor(f)
	}
	return fn.Constructor
}

// functionProtoFor returns the prototype of the closures of p, which is a generator or async function.
func (vm *VM) functionProtoFor(p *funcProto) *Object {
	switch {
	case p.isGenerator && p.isAsync:
		return vm.AsyncGeneratorFunctionProto
	case p.isGenerator:
		return vm.GeneratorFunctionProto
	case p.isAsync:
		return vm.AsyncFunctionProto
	}
	return vm.FunctionProto
}

// arrayProtoPlain reports whether no array index property exists on the prototypes of plain arrays,
// so that storing a new element in such an array cannot run a setter.
func (vm *VM) ArrayProtoPlain() bool {
	a, o := vm.ArrayProto, vm.ObjectProto
	return len(a.Elems) == 0 && len(o.Elems) == 0 && (a.Flags|o.Flags)&ObjSparse == 0
}

// arrayIterationPlain reports whether iterating the array o visits its elements by index
// without any observable call.
func (vm *VM) arrayIterationPlain(o *Object) bool {
	if o.Shape != vm.ArrayShape {
		return false
	}
	if vm.arrayIterEpoch != vm.protoEpoch || !vm.arrayIterChecked {
		vm.arrayIterChecked = true
		vm.arrayIterEpoch = vm.protoEpoch
		it, ok1 := vm.LookupData(vm.ArrayProto, vm.K.SymIterator)
		next, ok2 := vm.LookupData(vm.ArrayIteratorProto, vm.K.next)
		vm.arrayIterPlain = ok1 && ok2 && it.O == vm.FnArrayValues && next.O == vm.FnArrayIterNext
	}
	return vm.arrayIterPlain
}

// call calls the callable object f.
func (vm *VM) Call(f *Object, this Value, args ...Value) Value {
	return vm.CallSlow(f, this, args, false, nil)
}

// callValue calls f, which must be callable, or throws a TypeError.
func (vm *VM) CallValue(f, this Value, args ...Value) Value {
	if !f.IsCallable() {
		vm.ThrowTypeError(vm.DisplayString(f) + " is not a function")
	}
	return vm.CallSlow(f.O, this, args, false, nil)
}

// callSlow calls f with any kind of callee. With isNew set, this is the object under construction.
func (vm *VM) CallSlow(f *Object, this Value, args []Value, isNew bool, newTarget *Object) Value {
	if vm.interrupt.Load() {
		vm.Poll()
	}
	fn := f.Fn
	if fn == nil {
		vm.ThrowTypeError(vm.DisplayString(Value{O: f}) + " is not a function")
	}
	if b := fn.Bound; b != nil {
		if len(b.Args) > 0 {
			args = append(append(make([]Value, 0, len(b.Args)+len(args)), b.Args...), args...)
		}
		return vm.CallSlow(b.Target, b.This, args, false, nil)
	}
	if fn.native != nil {
		saved := vm.NewTarget
		vm.NewTarget = nil
		r := fn.native(vm, this, args)
		vm.NewTarget = saved
		return r
	}
	c := fn.Code
	if fn.classConstructor && !isNew && newTarget == nil {
		vm.ThrowTypeError("Class constructor " + c.Name + " cannot be invoked without 'new'")
	}
	if c.isGenerator || c.isAsync {
		return vm.callCoroutine(f, this, args)
	}
	if len(vm.frames) >= maxFrames {
		vm.ThrowRangeError("Maximum call stack size exceeded")
	}
	base := vm.sp
	vm.ensureStack(base + len(args) + c.nregs + 1)
	copy(vm.stack[base:], args)
	vm.pushFrame(f, this, base, len(args), 0, isNew, newTarget)
	r := vm.run(len(vm.frames) - 1)
	vm.sp = base
	return r
}

// construct implements Construct: new f(...args) with the given new.target.
func (vm *VM) Construct(f *Object, args []Value, newTarget *Object) *Object {
	fn := f.Fn
	if b := fn.Bound; b != nil {
		if len(b.Args) > 0 {
			args = append(append(make([]Value, 0, len(b.Args)+len(args)), b.Args...), args...)
		}
		if newTarget == f {
			newTarget = b.Target
		}
		return vm.Construct(b.Target, args, newTarget)
	}
	if fn.native != nil {
		saved := vm.NewTarget
		vm.NewTarget = newTarget
		r := fn.native(vm, Undefined, args)
		vm.NewTarget = saved
		if !r.IsObject() {
			vm.ThrowTypeError("constructor did not return an object")
		}
		return r.O
	}
	this := tdz
	if !fn.derived {
		this = Value{O: vm.NewObject(vm.ProtoFromConstructor(newTarget, vm.ObjectProto))}
	}
	r := vm.CallSlow(f, this, args, !fn.derived, newTarget)
	if !r.IsObject() {
		vm.ThrowTypeError("constructor did not return an object")
	}
	return r.O
}

// protoFromConstructor implements GetPrototypeFromConstructor.
func (vm *VM) ProtoFromConstructor(ctor *Object, fallback *Object) *Object {
	if ctor == nil {
		return fallback
	}
	if p := vm.GetV(Value{O: ctor}, vm.K.Prototype); p.IsObject() {
		return p.O
	}
	return fallback
}

// newNative returns a function implemented in Go.
func (vm *VM) NewNative(name string, length int, f NativeFunc) *Object {
	return &Object{
		Kind:  KindFunction,
		Class: ClassFunction,
		Flags: ObjExotic,
		Shape: vm.FunctionShape,
		Fn: &Function{
			native: f,
			Name:   name,
			length: length,
		},
	}
}

// newNativeConstructor returns a function implemented in Go that can be called with new.
// f sees the constructor it is constructing for as VM.newTarget, which is nil in a plain call.
func (vm *VM) NewNativeConstructor(name string, length int, f NativeFunc) *Object {
	o := vm.NewNative(name, length, f)
	o.Fn.Constructor = true
	return o
}

// getName looks the variable key up in the dynamic scopes of the running function.
func (vm *VM) getName(key *Object) (Value, bool) {
	o := vm.findScope(key)
	if o == nil {
		return Undefined, false
	}
	return vm.GetProp(o, PropKey{Key: key}, Value{O: o}), true
}

// findScope returns the innermost with scope of the running function that has the variable key.
func (vm *VM) findScope(key *Object) *Object {
	scopes := vm.frames[len(vm.frames)-1].scopes
	k := PropKey{Key: key}
	for i := len(scopes) - 1; i >= 0; i-- {
		o := scopes[i]
		if !vm.HasProperty(o, k) {
			continue
		}
		if u := vm.GetProp(o, PropKey{Key: vm.K.SymUnscopables}, Value{O: o}); u.IsObject() {
			if Truthy(vm.GetProp(u.O, k, u)) {
				continue
			}
		}
		return o
	}
	return nil
}

func (vm *VM) setName(key *Object, val Value, strict bool) bool {
	o := vm.findScope(key)
	if o == nil {
		return false
	}
	k := PropKey{Key: key}
	if strict && !vm.HasProperty(o, k) {
		vm.throwReferenceError(key.Str + " is not defined")
	}
	if !vm.setProp(o, k, val, Value{O: o}) && strict {
		vm.throwReadOnly(Value{O: o}, k)
	}
	return true
}

func (vm *VM) deleteName(key *Object) bool {
	if o := vm.findScope(key); o != nil {
		return vm.DeleteProp(o, PropKey{Key: key})
	}
	if _, ok := vm.globalLex.Shape.Lookup(key); ok {
		return false
	}
	return vm.DeleteProp(vm.Global, PropKey{Key: key})
}

func (vm *VM) callName(key *Object) (fn, this Value, found bool) {
	o := vm.findScope(key)
	if o == nil {
		return Undefined, Undefined, false
	}
	return vm.GetProp(o, PropKey{Key: key}, Value{O: o}), Value{O: o}, true
}
