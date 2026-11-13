// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// propKey is a property key: an array index when key is nil, otherwise an interned string or a symbol.
type PropKey struct {
	Key *Object
	Idx uint32
}

func (k PropKey) isIndex() bool {
	return k.Key == nil
}

func (k PropKey) IsSymbol() bool {
	return k.Key != nil && k.Key.Kind == kindSymbol
}

func indexKey(i uint32) PropKey {
	return PropKey{Idx: i}
}

func namedKey(key *Object) PropKey {
	return PropKey{Key: key}
}

// keyOfString returns the property key for the string s.
func (vm *VM) keyOfString(s string) PropKey {
	if i, ok := stringIndex(s); ok {
		return PropKey{Idx: i}
	}
	return PropKey{Key: vm.Intern(s)}
}

// indexName returns the key under which the array index i is stored in a shape.
func (vm *VM) indexName(i uint32) *Object {
	return vm.Intern(strconv.FormatUint(uint64(i), 10))
}

// keyValue returns the key k as a string or symbol value.
func (vm *VM) KeyValue(k PropKey) Value {
	if k.Key == nil {
		return vm.NewString(strconv.FormatUint(uint64(k.Idx), 10))
	}
	return Value{O: k.Key}
}

// keyString describes the key k for messages.
func (vm *VM) KeyString(k PropKey) string {
	if k.Key == nil {
		return strconv.FormatUint(uint64(k.Idx), 10)
	}
	if k.Key.Kind == kindSymbol {
		return "Symbol(" + k.Key.Str + ")"
	}
	return k.Key.Str
}

// toPropertyKey implements ToPropertyKey.
func (vm *VM) ToPropertyKey(v Value) PropKey {
	o := v.O
	if o == nil {
		if i, ok := numberIndex(v.N); ok {
			return PropKey{Idx: i}
		}
		return PropKey{Key: vm.Intern(numconv.NumberToString(v.N))}
	}
	switch o.Kind {
	case KindString:
		if c := o.Str; len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := stringIndex(c); ok {
				return PropKey{Idx: i}
			}
		}
		return PropKey{Key: vm.stringKey(o)}
	case kindSymbol:
		return PropKey{Key: o}
	}
	if o.Kind >= kindObject {
		return vm.ToPropertyKey(vm.toPrimitive(v, hintString))
	}
	return vm.keyOfString(vm.ToString(v))
}

// Fields of a property descriptor that are present.
const (
	HasValue uint8 = 1 << iota
	HasWritable
	HasGet
	HasSet
	HasEnumerable
	HasConfigurable
)

// propDesc is a property descriptor.
type PropDesc struct {
	Value  Value
	Getter *Object
	Setter *Object

	// flags holds the attributes. Only the ones that has names are meaningful.
	Flags uint8
	Has   uint8
}

func (d *PropDesc) IsAccessor() bool {
	return d.Has&(HasGet|HasSet) != 0
}

func (d *PropDesc) IsData() bool {
	return d.Has&(HasValue|HasWritable) != 0
}

func dataDesc(v Value, flags uint8) PropDesc {
	return PropDesc{
		Value: v,
		Flags: flags,
		Has:   HasValue | HasWritable | HasEnumerable | HasConfigurable,
	}
}

// materialize creates the properties that a function has from the start but that are only
// stored once something looks at them.
func (vm *VM) Materialize(o *Object) {
	f := o.Fn
	if f.Materialized {
		return
	}
	f.Materialized = true
	o.Flags &^= ObjExotic
	if c := f.Code; c != nil {
		vm.Add(o, vm.K.Length, Number(float64(c.length)), FlagConfigurable)
		vm.Add(o, vm.K.Name, vm.NewString(c.Name), FlagConfigurable)
		switch {
		case c.isGenerator:
			proto := vm.GeneratorProto
			if c.isAsync {
				proto = vm.AsyncGeneratorProto
			}
			vm.Add(o, vm.K.Prototype, Value{O: vm.NewObject(proto)}, FlagWritable)
		case f.Constructor:
			p := vm.NewObject(vm.ObjectProto)
			vm.Add(p, vm.K.Constructor, Value{O: o}, FlagsHidden)
			flags := FlagWritable
			if f.classConstructor {
				flags = 0
			}
			vm.Add(o, vm.K.Prototype, Value{O: p}, flags)
		}
		return
	}
	vm.Add(o, vm.K.Length, Number(float64(f.length)), FlagConfigurable)
	vm.Add(o, vm.K.Name, vm.NewString(f.Name), FlagConfigurable)
}

// isLazyKey reports whether key names a property that materialize creates.
func (vm *VM) isLazyKey(key *Object) bool {
	return key == vm.K.Prototype || key == vm.K.Name || key == vm.K.Length
}

// resolve prepares the exotic object o for an operation on its named properties.
func (vm *VM) resolve(o *Object) {
	if o.Fn != nil && !o.Fn.Materialized {
		vm.Materialize(o)
	}
}

// lookup finds the named property key on o and its prototype chain.
// For the length of an array, which is not stored, it returns the array as holder and slot -1.
func (vm *VM) lookup(o *Object, key *Object) (holder *Object, p prop, ok bool) {
	lazy := vm.isLazyKey(key)
	for h := o; h != nil; h = h.Shape.proto {
		if lazy {
			if h.Flags&ObjExotic != 0 {
				vm.resolve(h)
			}
			if h.Kind == KindArray && key == vm.K.Length {
				flags := FlagWritable
				if h.Flags&ObjFrozenLength != 0 {
					flags = 0
				}
				return h, prop{
					Slot:  -1,
					Flags: flags,
				}, true
			}
		}
		if p, ok := h.Shape.Lookup(key); ok {
			return h, p, true
		}
	}
	return nil, prop{}, false
}

// protoStart returns the object at which a property lookup on the value v starts.
// It throws a TypeError for undefined and null.
func (vm *VM) protoStart(v Value, key PropKey) *Object {
	o := v.O
	if o == nil {
		return vm.NumberProto
	}
	switch o.Kind {
	case kindFalse, kindTrue:
		return vm.BooleanProto
	case kindSymbol:
		return vm.SymbolProto
	case kindBigInt:
		return vm.BigintProto
	case kindUndefined, kindNull:
		vm.ThrowTypeError("Cannot read properties of " + vm.DisplayString(v) + " (reading '" + vm.KeyString(key) + "')")
	}
	// A string has the empty shape whose prototype is String.prototype.
	return o
}

// get returns the named property key of v. When the property is an accessor with a getter,
// get returns the getter instead and the caller has to call it with v as this.
// get fills c, which may be nil, when the lookup can be repeated from the cache.
func (vm *VM) get(v Value, key *Object, c *propCache) (val Value, getter *Object) {
	start := vm.protoStart(v, PropKey{Key: key})
	if start.Kind == KindString && key == vm.K.Length {
		return Number(float64(start.StringLength())), nil
	}
	if start.Class == classProxy {
		return vm.proxyGet(start, PropKey{Key: key}, v), nil
	}
	if c != nil {
		c.keep(start.Shape)
	}
	holder, p, ok := vm.lookup(start, key)
	if !ok {
		if c != nil && !start.Shape.private && !hasProxyOnChain(start) {
			// Cache the miss as a hit on an object that holds undefined. A private shape can
			// gain the property without changing, so only shared shapes qualify.
			c.shape = start.Shape
			c.kind = cacheProto
			c.holder = vm.missHolder
			c.slot = 0
			c.epoch = vm.protoEpoch
		}
		if h := proxyOnChain(start); h != nil {
			return vm.proxyGet(h, PropKey{Key: key}, v), nil
		}
		return Undefined, nil
	}
	if p.Slot < 0 {
		return Number(float64(holder.Length)), nil
	}
	val = holder.Slots[p.Slot]
	if p.Flags&FlagAccessor != 0 {
		g := val.Accessor().Getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && (holder == start || !start.Shape.private) {
			c.shape = start.Shape
			c.kind = cacheGetter
			c.holder = g
			c.epoch = vm.protoEpoch
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.shape = start.Shape
			c.slot = p.Slot
			c.kind = cacheOwn
		} else if !start.Shape.private {
			c.shape = start.Shape
			c.slot = p.Slot
			c.kind = cacheProto
			c.holder = holder
			c.epoch = vm.protoEpoch
		}
	}
	return val, nil
}

// proxyOnChain returns the first proxy on the prototype chain of o, which shape lookups stop at.
func proxyOnChain(o *Object) *Object {
	for h := o; h != nil; h = h.Shape.proto {
		if h.Class == classProxy {
			return h
		}
	}
	return nil
}

func hasProxyOnChain(o *Object) bool {
	return proxyOnChain(o) != nil
}

// getV returns the named property key of v, calling a getter.
func (vm *VM) GetV(v Value, key *Object) Value {
	val, getter := vm.get(v, key, nil)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// getKey implements the Get operation on the value v for any key.
func (vm *VM) GetKey(v Value, k PropKey) Value {
	if k.Key != nil {
		return vm.GetV(v, k.Key)
	}
	val, getter := vm.getIndex(v, k.Idx)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// getIndex returns the property of v whose key is the array index i, or the getter to call.
func (vm *VM) getIndex(v Value, i uint32) (Value, *Object) {
	start := vm.protoStart(v, PropKey{Idx: i})
	for h := start; h != nil; h = h.Shape.proto {
		if int64(i) < int64(len(h.Elems)) {
			if e := h.Elems[i]; !e.IsHole() {
				return e, nil
			}
		}
		if h.Kind == kindObject && h.Class == classObject && h.Flags&ObjSparse == 0 {
			continue
		}
		switch h.Class {
		case classProxy:
			return vm.proxyGet(h, PropKey{Idx: i}, v), nil
		case classTypedArray:
			// An integer-indexed object answers for every index and never defers to its prototype.
			return vm.typedArrayGet(h, i), nil
		}
		if h.Kind == KindString || h.Class == ClassString {
			s := h
			if h.Class == ClassString {
				s = h.Ext.(*Object)
			}
			if int(i) < s.StringLength() {
				return vm.UnitString(s.UnitAt(int(i))), nil
			}
		}
		if h.Flags&ObjSparse != 0 {
			if p, ok := h.Shape.Lookup(vm.indexName(i)); ok {
				val := h.Slots[p.Slot]
				if p.Flags&FlagAccessor != 0 {
					return Undefined, val.Accessor().Getter
				}
				return val, nil
			}
		}
	}
	return Undefined, nil
}

// getOwnProperty implements [[GetOwnProperty]].
func (vm *VM) GetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	if o.Class == classProxy {
		return vm.proxyGetOwnProperty(o, k)
	}
	if k.Key == nil {
		if int64(k.Idx) < int64(len(o.Elems)) {
			if e := o.Elems[k.Idx]; !e.IsHole() {
				return dataDesc(e, FlagsData), true
			}
		}
		switch o.Class {
		case classTypedArray:
			if v := vm.typedArrayGet(o, k.Idx); !v.IsUndefined() {
				return dataDesc(v, FlagsData), true
			}
			return PropDesc{}, false
		case ClassString:
			s := o.Ext.(*Object)
			if int(k.Idx) < s.StringLength() {
				return dataDesc(vm.UnitString(s.UnitAt(int(k.Idx))), FlagEnumerable), true
			}
		}
		if o.Flags&ObjSparse == 0 {
			return PropDesc{}, false
		}
		k.Key = vm.indexName(k.Idx)
	} else if vm.isLazyKey(k.Key) {
		if o.Flags&ObjExotic != 0 {
			vm.resolve(o)
		}
		if o.Kind == KindArray && k.Key == vm.K.Length {
			flags := FlagWritable
			if o.Flags&ObjFrozenLength != 0 {
				flags = 0
			}
			return dataDesc(Number(float64(o.Length)), flags), true
		}
	}
	p, ok := o.Shape.Lookup(k.Key)
	if !ok {
		return PropDesc{}, false
	}
	val := o.Slots[p.Slot]
	if p.Flags&FlagAccessor != 0 {
		a := val.Accessor()
		return PropDesc{
			Getter: a.Getter,
			Setter: a.setter,
			Flags:  p.Flags,
			Has:    HasGet | HasSet | HasEnumerable | HasConfigurable,
		}, true
	}
	return dataDesc(val, p.Flags), true
}

// hasOwn reports whether o has the own property k.
func (vm *VM) HasOwn(o *Object, k PropKey) bool {
	_, ok := vm.GetOwnProperty(o, k)
	return ok
}

// hasProperty implements [[HasProperty]].
func (vm *VM) HasProperty(o *Object, k PropKey) bool {
	for h := o; h != nil; h = vm.ProtoOf(h) {
		if h.Class == classProxy {
			return vm.proxyHas(h, k)
		}
		if vm.HasOwn(h, k) {
			return true
		}
		if h.Class == classTypedArray && k.Key == nil {
			return false
		}
	}
	return false
}

// protoOf implements [[GetPrototypeOf]].
func (vm *VM) ProtoOf(o *Object) *Object {
	if o.Class == classProxy {
		return vm.proxyGetPrototypeOf(o)
	}
	return o.Shape.proto
}

// getProp implements [[Get]] with a receiver.
func (vm *VM) GetProp(o *Object, k PropKey, receiver Value) Value {
	for h := o; h != nil; h = h.Shape.proto {
		if h.Class == classProxy {
			return vm.proxyGet(h, k, receiver)
		}
		d, ok := vm.GetOwnProperty(h, k)
		if !ok {
			if h.Class == classTypedArray && k.Key == nil {
				return Undefined
			}
			continue
		}
		if d.Flags&FlagAccessor != 0 {
			if d.Getter == nil {
				return Undefined
			}
			return vm.Call(d.Getter, receiver)
		}
		return d.Value
	}
	return Undefined
}

// growElems reports whether storing the index i in o should extend elems, given their length n.
func growElems(n int, i uint32) bool {
	return int64(i) <= int64(n)+int64(n)/2+16
}

// addIndex creates the data property with the array index i on o, which has no such own property.
// It reports false when o does not allow it.
func (vm *VM) addIndex(o *Object, i uint32, val Value, flags uint8) bool {
	if o.Flags&ObjNotExtensible != 0 {
		return false
	}
	if o.Kind == KindArray && i >= o.Length {
		if o.Flags&ObjFrozenLength != 0 {
			return false
		}
		o.Length = i + 1
	}
	n := len(o.Elems)
	if flags == FlagsData {
		if int(i) < n {
			o.Elems[i] = val
			vm.TouchProto(o)
			return true
		}
		if growElems(n, i) {
			for uint32(len(o.Elems)) < i {
				o.Elems = append(o.Elems, hole)
			}
			o.Elems = append(o.Elems, val)
			vm.TouchProto(o)
			return true
		}
	}
	o.Flags |= ObjSparse
	vm.Add(o, vm.indexName(i), val, flags)
	if o.Flags&objIsProto != 0 {
		vm.protoEpoch++
	}
	return true
}

// setArrayLength implements ArraySetLength for a length that is already converted.
// It reports false when an element could not be deleted or the length is not writable.
func (vm *VM) setArrayLength(o *Object, n uint32) bool {
	if n == o.Length {
		return true
	}
	if o.Flags&ObjFrozenLength != 0 {
		return false
	}
	if n > o.Length {
		o.Length = n
		return true
	}
	ok := true
	if o.Flags&ObjSparse != 0 {
		// Delete the stored index properties from the end, stopping at one that is not configurable.
		var idxs []uint32
		for _, k := range o.Shape.keys() {
			if k.Kind != KindString {
				continue
			}
			if i, isIdx := stringIndex(k.Str); isIdx && i >= n {
				idxs = append(idxs, i)
			}
		}
		slices.Sort(idxs)
		for j := len(idxs) - 1; j >= 0; j-- {
			k := vm.indexName(idxs[j])
			if p, _ := o.Shape.Lookup(k); p.Flags&FlagConfigurable == 0 {
				n = idxs[j] + 1
				ok = false
				break
			}
			vm.remove(o, k)
		}
	}
	if int64(n) < int64(len(o.Elems)) {
		clear(o.Elems[n:])
		o.Elems = o.Elems[:n]
	}
	o.Length = n
	return ok
}

// set stores val as the named property key of v. When the property has a setter, set returns it
// and the caller has to call it with v as this. ok is false when the store was refused,
// which is a TypeError in strict code.
func (vm *VM) set(v Value, key *Object, val Value, c *propCache) (setter *Object, ok bool) {
	o := v.O
	if o == nil || o.Kind < kindObject {
		if v.IsNullish() {
			vm.ThrowTypeError("Cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.KeyString(PropKey{Key: key}) + "')")
		}
		// A store to a primitive only has an effect when a setter is inherited.
		start := vm.protoStart(v, PropKey{Key: key})
		if h, p, found := vm.lookup(start, key); found && p.Slot >= 0 && p.Flags&FlagAccessor != 0 {
			if s := h.Slots[p.Slot].Accessor().setter; s != nil {
				return s, true
			}
		}
		return nil, false
	}
	if o.Class == classProxy || o.Flags&ObjExotic != 0 && o.Fn == nil {
		return nil, vm.setProp(o, PropKey{Key: key}, val, v)
	}
	if vm.isLazyKey(key) {
		if o.Flags&ObjExotic != 0 {
			vm.resolve(o)
		}
		if o.Kind == KindArray && key == vm.K.Length {
			return nil, vm.setArrayLengthValue(o, val)
		}
	}
	if c != nil {
		c.keep(o.Shape)
	}
	if p, found := o.Shape.Lookup(key); found {
		if p.Flags&FlagAccessor != 0 {
			s := o.Slots[p.Slot].Accessor().setter
			if s == nil {
				return nil, false
			}
			if c != nil {
				c.shape = o.Shape
				c.kind = cacheSetter
				c.holder = s
				c.epoch = vm.protoEpoch
			}
			return s, true
		}
		if p.Flags&FlagWritable == 0 {
			return nil, false
		}
		o.Slots[p.Slot] = val
		if c != nil {
			c.shape = o.Shape
			c.kind = cacheOwn
			c.slot = p.Slot
		}
		return nil, true
	}
	// The property is new on o: a setter or a read-only property further up takes precedence.
	if proto := o.Shape.proto; proto != nil {
		if hasProxyOnChain(proto) {
			return nil, vm.setProp(o, PropKey{Key: key}, val, v)
		}
		if h, p, found := vm.lookup(proto, key); found {
			if p.Slot < 0 {
				return nil, false
			}
			if p.Flags&FlagAccessor != 0 {
				s := h.Slots[p.Slot].Accessor().setter
				if s == nil {
					return nil, false
				}
				if c != nil && !o.Shape.private {
					c.shape = o.Shape
					c.kind = cacheSetter
					c.holder = s
					c.epoch = vm.protoEpoch
				}
				return s, true
			}
			if p.Flags&FlagWritable == 0 {
				return nil, false
			}
		}
	}
	if o.Flags&ObjNotExtensible != 0 {
		return nil, false
	}
	old := o.Shape
	vm.Add(o, key, val, FlagsData)
	if c != nil && !old.private && !o.Shape.private && o.Flags&objIsProto == 0 {
		c.shape = old
		c.kind = cacheAdd
		c.next = o.Shape
		c.epoch = vm.protoEpoch
	}
	return nil, true
}

// setArrayLengthValue implements the store of an arbitrary value to the length of an array.
func (vm *VM) setArrayLengthValue(o *Object, val Value) bool {
	n := vm.ToNumber(val)
	u := numconv.ToUint32(n)
	if float64(u) != n {
		vm.ThrowRangeError("Invalid array length")
	}
	return vm.setArrayLength(o, u)
}

// setIndex stores val as the property of v whose key is the array index i.
// The results are those of set.
func (vm *VM) setIndex(v Value, i uint32, val Value) (setter *Object, ok bool) {
	o := v.O
	if o == nil || o.Kind < kindObject {
		if v.IsNullish() {
			vm.ThrowTypeError("Cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.KeyString(PropKey{Idx: i}) + "')")
		}
		return nil, false
	}
	if int64(i) < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		o.Elems[i] = val
		return nil, true
	}
	if o.Class == classTypedArray {
		vm.typedArraySet(o, i, val)
		return nil, true
	}
	// Anything but a plain store into a plain object or array takes the general path.
	if (o.Kind == KindArray || o.Class == classObject) && o.Flags&(ObjSparse|ObjExotic) == 0 {
		plain := true
		for h := o.Shape.proto; h != nil; h = h.Shape.proto {
			if len(h.Elems) != 0 || h.Flags&(ObjSparse|ObjExotic) != 0 || h.Class != classObject && h.Class != classArray && h.Class != ClassFunction {
				plain = false
				break
			}
		}
		if plain {
			return nil, vm.addIndex(o, i, val, FlagsData)
		}
	}
	return nil, vm.setProp(o, PropKey{Idx: i}, val, v)
}

// setProp implements [[Set]] with a receiver. It reports false when the store was refused.
func (vm *VM) setProp(o *Object, k PropKey, val Value, receiver Value) bool {
	var d PropDesc
	found := false
	for h := o; h != nil; h = h.Shape.proto {
		if h.Class == classProxy {
			return vm.proxySet(h, k, val, receiver)
		}
		if h.Class == classTypedArray && k.Key == nil {
			if receiver.O == h {
				vm.typedArraySet(h, k.Idx, val)
				return true
			}
			if !vm.typedArrayHas(h, k.Idx) {
				return true
			}
		}
		if d, found = vm.GetOwnProperty(h, k); found {
			break
		}
	}
	if found && d.Flags&FlagAccessor != 0 {
		if d.Setter == nil {
			return false
		}
		vm.Call(d.Setter, receiver, val)
		return true
	}
	if found && d.Flags&FlagWritable == 0 {
		return false
	}
	if !receiver.IsObject() {
		return false
	}
	r := receiver.O
	if existing, ok := vm.GetOwnProperty(r, k); ok {
		if existing.Flags&FlagAccessor != 0 || existing.Flags&FlagWritable == 0 {
			return false
		}
		return vm.defineOwnProperty(r, k, PropDesc{
			Value: val,
			Has:   HasValue,
		})
	}
	return vm.defineOwnProperty(r, k, dataDesc(val, FlagsData))
}

// put stores val as the property k of o and throws a TypeError when the store is refused.
func (vm *VM) Put(o *Object, k PropKey, val Value) {
	if !vm.setProp(o, k, val, Value{O: o}) {
		vm.ThrowTypeError("Cannot assign to read only property '" + vm.KeyString(k) + "'")
	}
}

// defineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (vm *VM) defineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	if o.Class == classProxy {
		return vm.proxyDefineOwnProperty(o, k, d)
	}
	if o.Class == classTypedArray && k.Key == nil {
		return vm.typedArrayDefine(o, k.Idx, d)
	}
	if o.Kind == KindArray && k.Key == vm.K.Length {
		return vm.defineArrayLength(o, d)
	}
	cur, exists := vm.GetOwnProperty(o, k)
	if !exists {
		if o.Flags&ObjNotExtensible != 0 {
			return false
		}
		if o.Kind == KindArray && k.Key == nil && k.Idx >= o.Length && o.Flags&ObjFrozenLength != 0 {
			return false
		}
		flags := d.Flags & d.attrMask()
		val := d.Value
		if d.Has&HasValue == 0 {
			val = Undefined
		}
		if d.IsAccessor() {
			flags = flags&^FlagWritable | FlagAccessor
			val = NewAccessor(d.Getter, d.Setter)
		}
		if k.Key == nil {
			return vm.addIndex(o, k.Idx, val, flags)
		}
		vm.Add(o, k.Key, val, flags)
		return true
	}

	// ValidateAndApplyPropertyDescriptor for an existing property.
	if cur.Flags&FlagConfigurable == 0 {
		if d.Has&HasConfigurable != 0 && d.Flags&FlagConfigurable != 0 {
			return false
		}
		if d.Has&HasEnumerable != 0 && (d.Flags^cur.Flags)&FlagEnumerable != 0 {
			return false
		}
		curAccessor := cur.Flags&FlagAccessor != 0
		if d.IsAccessor() != curAccessor && (d.IsAccessor() || d.IsData()) {
			return false
		}
		if curAccessor {
			if d.Has&HasGet != 0 && d.Getter != cur.Getter {
				return false
			}
			if d.Has&HasSet != 0 && d.Setter != cur.Setter {
				return false
			}
		} else if cur.Flags&FlagWritable == 0 {
			if d.Has&HasWritable != 0 && d.Flags&FlagWritable != 0 {
				return false
			}
			if d.Has&HasValue != 0 && !SameValue(d.Value, cur.Value) {
				return false
			}
		}
	}

	// Merge the fields that are present into the current property.
	flags := cur.Flags
	if d.Has&HasEnumerable != 0 {
		flags = flags&^FlagEnumerable | d.Flags&FlagEnumerable
	}
	if d.Has&HasConfigurable != 0 {
		flags = flags&^FlagConfigurable | d.Flags&FlagConfigurable
	}
	var val Value
	switch {
	case d.IsAccessor():
		g, s := cur.Getter, cur.Setter
		if cur.Flags&FlagAccessor == 0 {
			g, s = nil, nil
		}
		if d.Has&HasGet != 0 {
			g = d.Getter
		}
		if d.Has&HasSet != 0 {
			s = d.Setter
		}
		flags = flags&^FlagWritable | FlagAccessor
		val = NewAccessor(g, s)
	case cur.Flags&FlagAccessor != 0 && !d.IsData():
		val = NewAccessor(cur.Getter, cur.Setter)
	default:
		val = cur.Value
		if cur.Flags&FlagAccessor != 0 {
			val = Undefined
			flags &^= FlagAccessor | FlagWritable
		}
		if d.Has&HasValue != 0 {
			val = d.Value
		}
		if d.Has&HasWritable != 0 {
			flags = flags&^FlagWritable | d.Flags&FlagWritable
		}
	}
	vm.replaceOwn(o, k, val, flags)
	return true
}

// attrMask returns the attribute bits that d specifies.
func (d *PropDesc) attrMask() uint8 {
	var m uint8
	if d.Has&HasEnumerable != 0 {
		m |= FlagEnumerable
	}
	if d.Has&HasConfigurable != 0 {
		m |= FlagConfigurable
	}
	if d.Has&HasWritable != 0 {
		m |= FlagWritable
	}
	return m
}

// replaceOwn stores the value and attributes of the existing own property k of o.
func (vm *VM) replaceOwn(o *Object, k PropKey, val Value, flags uint8) {
	if k.Key == nil {
		if int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole() {
			if flags == FlagsData {
				o.Elems[k.Idx] = val
				vm.TouchProto(o)
				return
			}
			// The property no longer fits in elems.
			o.Elems[k.Idx] = hole
			o.Flags |= ObjSparse
			vm.Add(o, vm.indexName(k.Idx), val, flags)
			return
		}
		k.Key = vm.indexName(k.Idx)
	}
	vm.Define(o, k.Key, val, flags)
}

// defineArrayLength implements ArraySetLength.
func (vm *VM) defineArrayLength(o *Object, d PropDesc) bool {
	n := o.Length
	if d.Has&HasValue != 0 {
		f := vm.ToNumber(d.Value)
		n = numconv.ToUint32(f)
		if float64(n) != f {
			vm.ThrowRangeError("Invalid array length")
		}
	}
	if d.IsAccessor() {
		return false
	}
	if d.Has&HasConfigurable != 0 && d.Flags&FlagConfigurable != 0 {
		return false
	}
	if d.Has&HasEnumerable != 0 && d.Flags&FlagEnumerable != 0 {
		return false
	}
	frozen := o.Flags&ObjFrozenLength != 0
	if frozen && d.Has&HasWritable != 0 && d.Flags&FlagWritable != 0 {
		return false
	}
	ok := true
	if n != o.Length {
		if frozen {
			return false
		}
		ok = vm.setArrayLength(o, n)
	}
	if d.Has&HasWritable != 0 && d.Flags&FlagWritable == 0 {
		o.Flags |= ObjFrozenLength
	}
	return ok
}

// deleteProp implements [[Delete]]. It reports false when the property is not configurable.
func (vm *VM) DeleteProp(o *Object, k PropKey) bool {
	if o.Class == classProxy {
		return vm.proxyDelete(o, k)
	}
	if k.Key == nil {
		if int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole() {
			o.Elems[k.Idx] = hole
			vm.TouchProto(o)
			return true
		}
		switch o.Class {
		case classTypedArray:
			return !vm.typedArrayHas(o, k.Idx)
		case ClassString:
			if int(k.Idx) < o.Ext.(*Object).StringLength() {
				return false
			}
		}
		if o.Flags&ObjSparse == 0 {
			return true
		}
		k.Key = vm.indexName(k.Idx)
	} else if vm.isLazyKey(k.Key) {
		if o.Flags&ObjExotic != 0 {
			vm.resolve(o)
		}
		if o.Kind == KindArray && k.Key == vm.K.Length {
			return false
		}
	}
	p, ok := o.Shape.Lookup(k.Key)
	if !ok {
		return true
	}
	if p.Flags&FlagConfigurable == 0 {
		return false
	}
	vm.remove(o, k.Key)
	return true
}

// ownKeys implements [[OwnPropertyKeys]]: array indices in ascending order,
// then strings and then symbols, each in the order of creation.
func (vm *VM) OwnKeys(o *Object) []PropKey {
	if o.Class == classProxy {
		return vm.proxyOwnKeys(o)
	}
	if o.Flags&ObjExotic != 0 {
		vm.resolve(o)
	}
	var keys []PropKey
	switch o.Class {
	case ClassString:
		for i := range o.Ext.(*Object).StringLength() {
			keys = append(keys, PropKey{Idx: uint32(i)})
		}
	case classTypedArray:
		for i := range vm.typedArrayLength(o) {
			keys = append(keys, PropKey{Idx: uint32(i)})
		}
	}
	for i, e := range o.Elems {
		if !e.IsHole() {
			keys = append(keys, PropKey{Idx: uint32(i)})
		}
	}
	names := o.Shape.keys()
	if o.Flags&ObjSparse != 0 {
		sorted := true
		for _, k := range names {
			if k.Kind != KindString {
				continue
			}
			if i, ok := stringIndex(k.Str); ok {
				if n := len(keys); n > 0 && keys[n-1].Idx > i {
					sorted = false
				}
				keys = append(keys, PropKey{Idx: i})
			}
		}
		if !sorted {
			slices.SortFunc(keys, func(a, b PropKey) int {
				switch {
				case a.Idx < b.Idx:
					return -1
				case a.Idx > b.Idx:
					return 1
				}
				return 0
			})
		}
	}
	if o.Kind == KindArray {
		keys = append(keys, PropKey{Key: vm.K.Length})
	}
	for _, k := range names {
		if k.Kind != KindString {
			continue
		}
		if o.Flags&ObjSparse != 0 {
			if _, ok := stringIndex(k.Str); ok {
				continue
			}
		}
		keys = append(keys, PropKey{Key: k})
	}
	for _, k := range names {
		if k.Kind == kindSymbol {
			keys = append(keys, PropKey{Key: k})
		}
	}
	return keys
}

// isExtensible implements [[IsExtensible]].
func (vm *VM) IsExtensible(o *Object) bool {
	if o.Class == classProxy {
		return vm.proxyIsExtensible(o)
	}
	return o.Flags&ObjNotExtensible == 0
}

// preventExtensions implements [[PreventExtensions]].
func (vm *VM) PreventExtensions(o *Object) bool {
	if o.Class == classProxy {
		return vm.proxyPreventExtensions(o)
	}
	if o.Flags&ObjNotExtensible == 0 {
		if o.Flags&ObjExotic != 0 {
			vm.resolve(o)
		}
		o.Flags |= ObjNotExtensible
		// The inline caches that add properties are keyed by shared shapes.
		o.makePrivate()
	}
	return true
}

// setPrototypeOf implements [[SetPrototypeOf]].
func (vm *VM) SetPrototypeOf(o *Object, proto *Object) bool {
	if o.Class == classProxy {
		return vm.proxySetPrototypeOf(o, proto)
	}
	if o.Shape.proto == proto {
		return true
	}
	if o.Flags&ObjNotExtensible != 0 || o == vm.ObjectProto {
		return false
	}
	for p := proto; p != nil; p = p.Shape.proto {
		if p == o {
			return false
		}
		if p.Class == classProxy {
			break
		}
	}
	vm.SetProto(o, proto)
	return true
}

// createDataProperty implements CreateDataPropertyOrThrow.
func (vm *VM) CreateDataProperty(o *Object, k PropKey, val Value) {
	if !vm.defineOwnProperty(o, k, dataDesc(val, FlagsData)) {
		vm.ThrowTypeError("Cannot define property '" + vm.KeyString(k) + "'")
	}
}

// defineOrThrow implements DefinePropertyOrThrow.
func (vm *VM) DefineOrThrow(o *Object, k PropKey, d PropDesc) {
	if !vm.defineOwnProperty(o, k, d) {
		vm.ThrowTypeError("Cannot redefine property: " + vm.KeyString(k))
	}
}
