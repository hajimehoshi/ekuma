// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// PropKey is a property key: an array index when key is nil, otherwise an interned string or a symbol.
type PropKey struct {
	key *Object
	idx uint32
}

func (k PropKey) isIndex() bool {
	return k.key == nil
}

// IsSymbol reports whether k is a symbol.
func (k PropKey) IsSymbol() bool {
	return k.key != nil && k.key.kind == kindSymbol
}

// KeyOfString returns the property key for the string s, which must be in canonical WTF-8.
// It does not keep s.
func (vm *VM) KeyOfString(s string) PropKey {
	if i, ok := stringIndex(s); ok {
		return PropKey{idx: i}
	}
	if o, ok := vm.interned[s]; ok {
		return PropKey{key: o}
	}
	return PropKey{key: vm.Intern(strings.Clone(s))}
}

// indexName returns the key under which the array index i is stored in a shape.
func (vm *VM) indexName(i uint32) *Object {
	return vm.Intern(strconv.FormatUint(uint64(i), 10))
}

// KeyValue returns the key k as a string or symbol value.
func (vm *VM) KeyValue(k PropKey) Value {
	if k.key == nil {
		return vm.NewString(strconv.FormatUint(uint64(k.idx), 10))
	}
	return Value{o: k.key}
}

// KeyString describes the key k for messages.
func (vm *VM) KeyString(k PropKey) string {
	if k.key == nil {
		return strconv.FormatUint(uint64(k.idx), 10)
	}
	if k.key.kind == kindSymbol {
		return "Symbol(" + k.key.str + ")"
	}
	return k.key.str
}

// ToPropertyKey implements ToPropertyKey.
func (vm *VM) ToPropertyKey(v Value) PropKey {
	o := v.o
	if o == nil {
		if i, ok := numberIndex(v.n); ok {
			return PropKey{idx: i}
		}
		return PropKey{key: vm.Intern(numconv.NumberToString(v.n))}
	}
	switch o.kind {
	case kindString:
		if c := o.str; len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := stringIndex(c); ok {
				return PropKey{idx: i}
			}
		}
		return PropKey{key: vm.stringKey(o)}
	case kindSymbol:
		return PropKey{key: o}
	}
	if o.kind >= kindObject {
		return vm.ToPropertyKey(vm.ToPrimitive(v, HintString))
	}
	return vm.KeyOfString(vm.ToString(v))
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

// PropDesc is a property descriptor.
type PropDesc struct {
	Value  Value
	Getter *Object
	Setter *Object

	// Flags holds the attributes. Only the ones that has names are meaningful.
	Flags uint8
	Has   uint8
}

// IsAccessor implements IsAccessorDescriptor.
func (d *PropDesc) IsAccessor() bool {
	return d.Has&(HasGet|HasSet) != 0
}

// IsData implements IsDataDescriptor.
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
func (vm *VM) materialize(o *Object) {
	f := o.fn
	if f.materialized {
		return
	}
	f.materialized = true
	o.flags &^= objExotic
	if c := f.code; c != nil {
		vm.add(o, vm.K.Length, Number(float64(c.length)), FlagConfigurable)
		vm.add(o, vm.K.Name, vm.NewString(c.name), FlagConfigurable)
		if c.legacyCallerArguments {
			vm.add(o, vm.K.arguments, Undefined, 0)
			vm.add(o, vm.K.caller, Undefined, 0)
		}
		switch {
		case c.isGenerator:
			proto := vm.GeneratorProto
			if c.isAsync {
				proto = vm.AsyncGeneratorProto
			}
			vm.add(o, vm.K.Prototype, Value{o: vm.NewObject(proto)}, FlagWritable)
		case f.constructor:
			p := vm.NewObject(vm.ObjectProto)
			vm.add(p, vm.K.Constructor, Value{o: o}, FlagsHidden)
			flags := FlagWritable
			if f.classConstructor {
				flags = 0
			}
			vm.add(o, vm.K.Prototype, Value{o: p}, flags)
		}
		return
	}
	vm.add(o, vm.K.Length, Number(float64(f.length)), FlagConfigurable)
	vm.add(o, vm.K.Name, vm.NewString(f.name), FlagConfigurable)
}

// isLazyKey reports whether key names a property that materialize creates.
func (vm *VM) isLazyKey(key *Object) bool {
	return key == vm.K.Prototype || key == vm.K.Name || key == vm.K.Length || key == vm.K.caller || key == vm.K.arguments
}

// resolve prepares the exotic object o for an operation on its named properties.
func (vm *VM) resolve(o *Object) {
	if o.fn != nil && !o.fn.materialized {
		vm.materialize(o)
	}
}

// lookup finds the named property key on o and its prototype chain.
// For the length of an array, which is not stored, it returns the array as holder and slot -1.
func (vm *VM) lookup(o *Object, key *Object) (holder *Object, p prop, ok bool) {
	length := key == vm.K.Length
	for h := o; h != nil; h = h.shape.proto {
		if h.flags&objExotic != 0 && vm.isLazyKey(key) {
			vm.resolve(h)
		}
		if length && h.kind == kindArray {
			flags := FlagWritable
			if h.flags&objFrozenLength != 0 {
				flags = 0
			}
			return h, prop{
				slot:  -1,
				flags: flags,
			}, true
		}
		if p, ok := h.shape.lookup(key); ok {
			return h, p, true
		}
	}
	return nil, prop{}, false
}

// protoStart returns the object at which a property lookup on the value v starts.
// It throws a TypeError for undefined and null.
func (vm *VM) protoStart(v Value, key PropKey) *Object {
	o := v.o
	if o == nil {
		return vm.NumberProto
	}
	switch o.kind {
	case kindFalse, kindTrue:
		return vm.BooleanProto
	case kindSymbol:
		return vm.SymbolProto
	case kindBigInt:
		return vm.BigintProto
	case kindUndefined, kindNull:
		vm.ThrowTypeError("cannot read properties of " + vm.DisplayString(v) + " (reading '" + vm.KeyString(key) + "')")
	}
	// A string has the empty shape whose prototype is String.prototype.
	return o
}

// getNamed returns the named property key of v. When the property is an accessor with a getter,
// getNamed returns the getter instead and the caller has to call it with v as this.
// getNamed fills c, which may be nil, when the lookup can be repeated from the cache.
func (vm *VM) getNamed(v Value, key *Object, c *propCache) (val Value, getter *Object) {
	start := vm.protoStart(v, PropKey{key: key})
	if start.kind == kindString && key == vm.K.Length {
		return Number(float64(start.StringLength())), nil
	}
	if start.class == classProxy {
		return vm.proxyGet(start, PropKey{key: key}, v), nil
	}
	if c != nil {
		c.keep(start.shape)
	}
	holder, p, ok := vm.lookup(start, key)
	if !ok {
		if c != nil && !start.shape.private && !hasProxyOnChain(start) {
			// Cache the miss as a hit on an object that holds undefined. A private shape can
			// gain the property without changing, so only shared shapes qualify.
			c.shape = start.shape
			c.kind = cacheProto
			c.holder = vm.missHolder
			c.slot = 0
			c.epoch = vm.protoEpoch
		}
		if h := proxyOnChain(start); h != nil {
			return vm.proxyGet(h, PropKey{key: key}, v), nil
		}
		return Undefined, nil
	}
	if p.slot < 0 {
		return Number(float64(holder.length)), nil
	}
	val = holder.slots[p.slot]
	if p.flags&FlagAccessor != 0 {
		g := val.accessor().getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && (holder == start || !start.shape.private) {
			c.shape = start.shape
			c.kind = cacheGetter
			c.holder = g
			c.epoch = vm.protoEpoch
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.shape = start.shape
			c.slot = p.slot
			c.kind = cacheOwn
		} else if !start.shape.private {
			c.shape = start.shape
			c.slot = p.slot
			c.kind = cacheProto
			c.holder = holder
			c.epoch = vm.protoEpoch
		}
	}
	return val, nil
}

// proxyOnChain returns the first proxy on the prototype chain of o, which shape lookups stop at.
func proxyOnChain(o *Object) *Object {
	for h := o; h != nil; h = h.shape.proto {
		if h.class == classProxy {
			return h
		}
	}
	return nil
}

func hasProxyOnChain(o *Object) bool {
	return proxyOnChain(o) != nil
}

// GetV returns the named property key of v, calling a getter.
func (vm *VM) GetV(v Value, key *Object) Value {
	val, getter := vm.getNamed(v, key, nil)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// PropertyCache remembers where GetVCached found a property, so that looking up the same key
// again on values with the same layout is fast. The zero value is empty.
type PropertyCache struct {
	key PropKey
	c   propCache
}

// GetVCached implements GetV(v, k), using and updating c.
func (vm *VM) GetVCached(v Value, k PropKey, c *PropertyCache) Value {
	if k.key == nil {
		return vm.Index(v, k.idx)
	}
	if c.key != k {
		*c = PropertyCache{key: k}
	}
	if o := v.o; o != nil {
		e := &c.c.cacheEntry
		if o.shape != e.shape {
			e = &c.c.second
		}
		if o.shape == e.shape {
			if e.kind == cacheOwn {
				return o.slots[e.slot]
			}
			if e.epoch == vm.protoEpoch {
				switch e.kind {
				case cacheProto:
					return e.holder.slots[e.slot]
				case cacheGetter:
					return vm.Call(e.holder, v)
				}
			}
		}
	}
	val, getter := vm.getNamed(v, k.key, &c.c)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// Index returns v[i], calling a getter.
func (vm *VM) Index(v Value, i uint32) Value {
	val, getter := vm.getIndex(v, i)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// getIndex returns the property of v whose key is the array index i, or the getter to call.
func (vm *VM) getIndex(v Value, i uint32) (Value, *Object) {
	start := vm.protoStart(v, PropKey{idx: i})
	for h := start; h != nil; h = h.shape.proto {
		if int64(i) < int64(len(h.elems)) {
			if e := h.elems[i]; !e.isHole() {
				return e, nil
			}
		}
		if h.kind == kindObject && h.class == classObject && h.flags&objSparse == 0 {
			continue
		}
		switch h.class {
		case classProxy:
			return vm.proxyGet(h, PropKey{idx: i}, v), nil
		case classTypedArray:
			// An integer-indexed object answers for every index and never defers to its prototype.
			return vm.typedArrayGet(h, i), nil
		}
		if h.kind == kindString || h.class == classString {
			s := h
			if h.class == classString {
				s = h.ext.(*Object)
			}
			if int(i) < s.StringLength() {
				return vm.UnitString(s.UnitAt(int(i))), nil
			}
		}
		if h.flags&objSparse != 0 {
			if p, ok := h.shape.lookup(vm.indexName(i)); ok {
				val := h.slots[p.slot]
				if p.flags&FlagAccessor != 0 {
					return Undefined, val.accessor().getter
				}
				return val, nil
			}
		}
	}
	return Undefined, nil
}

// GetOwnProperty implements [[GetOwnProperty]].
func (vm *VM) GetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	if o.class == classProxy {
		return vm.proxyGetOwnProperty(o, k)
	}
	if k.key == nil {
		if int64(k.idx) < int64(len(o.elems)) {
			if e := o.elems[k.idx]; !e.isHole() {
				return dataDesc(e, FlagsData), true
			}
		}
		switch o.class {
		case classTypedArray:
			if v := vm.typedArrayGet(o, k.idx); !v.IsUndefined() {
				return dataDesc(v, FlagsData), true
			}
			return PropDesc{}, false
		case classString:
			s := o.ext.(*Object)
			if int(k.idx) < s.StringLength() {
				return dataDesc(vm.UnitString(s.UnitAt(int(k.idx))), FlagEnumerable), true
			}
		}
		if o.flags&objSparse == 0 {
			return PropDesc{}, false
		}
		k.key = vm.indexName(k.idx)
	} else {
		if o.flags&objExotic != 0 && vm.isLazyKey(k.key) {
			vm.resolve(o)
		}
		if k.key == vm.K.Length && o.kind == kindArray {
			flags := FlagWritable
			if o.flags&objFrozenLength != 0 {
				flags = 0
			}
			return dataDesc(Number(float64(o.length)), flags), true
		}
	}
	p, ok := o.shape.lookup(k.key)
	if !ok {
		return PropDesc{}, false
	}
	val := o.slots[p.slot]
	if p.flags&FlagAccessor != 0 {
		a := val.accessor()
		return PropDesc{
			Getter: a.getter,
			Setter: a.setter,
			Flags:  p.flags,
			Has:    HasGet | HasSet | HasEnumerable | HasConfigurable,
		}, true
	}
	return dataDesc(val, p.flags), true
}

// HasOwnProperty reports whether o has the own property k.
func (vm *VM) HasOwnProperty(o *Object, k PropKey) bool {
	_, ok := vm.GetOwnProperty(o, k)
	return ok
}

// HasProperty implements [[HasProperty]].
func (vm *VM) HasProperty(o *Object, k PropKey) bool {
	for h := o; h != nil; h = vm.ProtoOf(h) {
		if h.class == classProxy {
			return vm.proxyHas(h, k)
		}
		if vm.HasOwnProperty(h, k) {
			return true
		}
		if h.class == classTypedArray && k.key == nil {
			return false
		}
	}
	return false
}

// ProtoOf implements [[GetPrototypeOf]].
func (vm *VM) ProtoOf(o *Object) *Object {
	if o.class == classProxy {
		return vm.proxyGetPrototypeOf(o)
	}
	return o.shape.proto
}

// getProp implements [[Get]] with a receiver.
func (vm *VM) getProp(o *Object, k PropKey, receiver Value) Value {
	for h := o; h != nil; h = h.shape.proto {
		if h.class == classProxy {
			return vm.proxyGet(h, k, receiver)
		}
		d, ok := vm.GetOwnProperty(h, k)
		if !ok {
			if h.class == classTypedArray && k.key == nil {
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
	if o.flags&objNotExtensible != 0 {
		return false
	}
	if o.kind == kindArray && i >= o.length {
		if o.flags&objFrozenLength != 0 {
			return false
		}
		o.length = i + 1
	}
	n := len(o.elems)
	if flags == FlagsData {
		if int(i) < n {
			o.elems[i] = val
			vm.touchProto(o)
			return true
		}
		if growElems(n, i) {
			for uint32(len(o.elems)) < i {
				o.elems = append(o.elems, hole)
			}
			o.elems = append(o.elems, val)
			vm.touchProto(o)
			return true
		}
	}
	o.flags |= objSparse
	vm.add(o, vm.indexName(i), val, flags)
	if o.flags&objIsProto != 0 {
		vm.protoEpoch++
	}
	return true
}

// setArrayLength implements ArraySetLength for a length that is already converted.
// It reports false when an element could not be deleted or the length is not writable.
func (vm *VM) setArrayLength(o *Object, n uint32) bool {
	if n == o.length {
		return true
	}
	if o.flags&objFrozenLength != 0 {
		return false
	}
	if n > o.length {
		o.length = n
		return true
	}
	ok := true
	if o.flags&objSparse != 0 {
		// Delete the stored index properties from the end, stopping at one that is not configurable.
		var idxs []uint32
		for _, k := range o.shape.keys() {
			if k.kind != kindString {
				continue
			}
			if i, isIdx := stringIndex(k.str); isIdx && i >= n {
				idxs = append(idxs, i)
			}
		}
		slices.Sort(idxs)
		for _, idx := range slices.Backward(idxs) {
			k := vm.indexName(idx)
			if p, _ := o.shape.lookup(k); p.flags&FlagConfigurable == 0 {
				n = idx + 1
				ok = false
				break
			}
			vm.remove(o, k)
		}
	}
	if int64(n) < int64(len(o.elems)) {
		clear(o.elems[n:])
		o.elems = o.elems[:n]
	}
	o.length = n
	return ok
}

// setNamed stores val as the named property key of v. When the property has a setter, setNamed returns it
// and the caller has to call it with v as this. ok is false when the store was refused,
// which is a TypeError in strict code.
func (vm *VM) setNamed(v Value, key *Object, val Value, c *propCache) (setter *Object, ok bool) {
	o := v.o
	if o == nil || o.kind < kindObject {
		if v.IsNullish() {
			vm.ThrowTypeError("cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.KeyString(PropKey{key: key}) + "')")
		}
		// A store to a primitive only has an effect when a setter is inherited.
		start := vm.protoStart(v, PropKey{key: key})
		if h, p, found := vm.lookup(start, key); found && p.slot >= 0 && p.flags&FlagAccessor != 0 {
			if s := h.slots[p.slot].accessor().setter; s != nil {
				return s, true
			}
		}
		return nil, false
	}
	if o.class == classProxy || o.flags&objExotic != 0 && o.fn == nil {
		return nil, vm.setProp(o, PropKey{key: key}, val, v)
	}
	if o.flags&objExotic != 0 {
		// The initial properties of a function precede every property that a store adds.
		vm.resolve(o)
	}
	if o.kind == kindArray && key == vm.K.Length {
		return nil, vm.setArrayLengthValue(o, val)
	}
	if c != nil {
		c.keep(o.shape)
	}
	if p, found := o.shape.lookup(key); found {
		if p.flags&FlagAccessor != 0 {
			s := o.slots[p.slot].accessor().setter
			if s == nil {
				return nil, false
			}
			if c != nil {
				c.shape = o.shape
				c.kind = cacheSetter
				c.holder = s
				c.epoch = vm.protoEpoch
			}
			return s, true
		}
		if p.flags&FlagWritable == 0 {
			return nil, false
		}
		o.slots[p.slot] = val
		if c != nil {
			c.shape = o.shape
			c.kind = cacheOwn
			c.slot = p.slot
		}
		return nil, true
	}
	// The property is new on o: a setter or a read-only property further up takes precedence.
	if proto := o.shape.proto; proto != nil {
		if hasProxyOnChain(proto) {
			return nil, vm.setProp(o, PropKey{key: key}, val, v)
		}
		if h, p, found := vm.lookup(proto, key); found {
			if p.slot < 0 {
				return nil, false
			}
			if p.flags&FlagAccessor != 0 {
				s := h.slots[p.slot].accessor().setter
				if s == nil {
					return nil, false
				}
				if c != nil && !o.shape.private {
					c.shape = o.shape
					c.kind = cacheSetter
					c.holder = s
					c.epoch = vm.protoEpoch
				}
				return s, true
			}
			if p.flags&FlagWritable == 0 {
				return nil, false
			}
		}
	}
	if o.flags&objNotExtensible != 0 {
		return nil, false
	}
	old := o.shape
	vm.add(o, key, val, FlagsData)
	if c != nil && !old.private && !o.shape.private && o.flags&objIsProto == 0 {
		c.shape = old
		c.kind = cacheAdd
		c.next = o.shape
		c.epoch = vm.protoEpoch
	}
	return nil, true
}

// setArrayLengthValue implements the store of an arbitrary value to the length of an array.
func (vm *VM) setArrayLengthValue(o *Object, val Value) bool {
	if o.flags&objFrozenLength != 0 {
		return false
	}
	return vm.setArrayLength(o, vm.toArrayLength(val))
}

// toArrayLength converts val to a new array length as ArraySetLength does, throwing a RangeError when
// val is not a valid length.
func (vm *VM) toArrayLength(val Value) uint32 {
	// ArraySetLength converts the value by ToUint32 and then by ToNumber. Only an object can observe the
	// second conversion.
	n := vm.ToNumber(val)
	u := numconv.ToUint32(n)
	if val.IsObject() {
		n = vm.ToNumber(val)
	}
	if float64(u) != n {
		vm.ThrowRangeError("invalid array length")
	}
	return u
}

// setIndex stores val as the property of v whose key is the array index i.
// The results are those of setNamed.
func (vm *VM) setIndex(v Value, i uint32, val Value) (setter *Object, ok bool) {
	o := v.o
	if o == nil || o.kind < kindObject {
		if v.IsNullish() {
			vm.ThrowTypeError("cannot set properties of " + vm.DisplayString(v) + " (setting '" + vm.KeyString(PropKey{idx: i}) + "')")
		}
		return nil, false
	}
	if int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = val
		return nil, true
	}
	if o.class == classTypedArray {
		vm.typedArraySet(o, i, val)
		return nil, true
	}
	// Anything but a plain store into a plain object or array takes the general path.
	if (o.kind == kindArray || o.class == classObject) && o.flags&(objSparse|objExotic) == 0 {
		plain := true
		for h := o.shape.proto; h != nil; h = h.shape.proto {
			if len(h.elems) != 0 || h.flags&(objSparse|objExotic) != 0 || h.class != classObject && h.class != classArray && h.class != classFunction {
				plain = false
				break
			}
		}
		if plain {
			return nil, vm.addIndex(o, i, val, FlagsData)
		}
	}
	return nil, vm.setProp(o, PropKey{idx: i}, val, v)
}

// setProp implements [[Set]] with a receiver. It reports false when the store was refused.
func (vm *VM) setProp(o *Object, k PropKey, val Value, receiver Value) bool {
	var d PropDesc
	found := false
	for h := o; h != nil; h = h.shape.proto {
		if h.class == classProxy {
			return vm.proxySet(h, k, val, receiver)
		}
		if h.class == classTypedArray && k.key == nil {
			if receiver.o == h {
				vm.typedArraySet(h, k.idx, val)
				return true
			}
			if !vm.typedArrayHas(h, k.idx) {
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
	r := receiver.o
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

// Set stores val as the property k of o and throws a TypeError when the store is refused.
func (vm *VM) Set(o *Object, k PropKey, val Value) {
	if !vm.setProp(o, k, val, Value{o: o}) {
		vm.ThrowTypeError("cannot assign to read only property '" + vm.KeyString(k) + "'")
	}
}

// defineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (vm *VM) defineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	if o.class == classProxy {
		return vm.proxyDefineOwnProperty(o, k, d)
	}
	if o.class == classTypedArray && k.key == nil {
		return vm.typedArrayDefine(o, k.idx, d)
	}
	if o.kind == kindArray && k.key == vm.K.Length {
		return vm.defineArrayLength(o, d)
	}
	cur, exists := vm.GetOwnProperty(o, k)
	if !exists {
		if o.flags&objNotExtensible != 0 {
			return false
		}
		if o.kind == kindArray && k.key == nil && k.idx >= o.length && o.flags&objFrozenLength != 0 {
			return false
		}
		if o.flags&objExotic != 0 {
			// The initial properties of a function precede every property that is defined later.
			vm.resolve(o)
		}
		flags := d.Flags & d.attrMask()
		val := d.Value
		if d.Has&HasValue == 0 {
			val = Undefined
		}
		if d.IsAccessor() {
			flags = flags&^FlagWritable | FlagAccessor
			val = newAccessor(d.Getter, d.Setter)
		}
		if k.key == nil {
			return vm.addIndex(o, k.idx, val, flags)
		}
		vm.add(o, k.key, val, flags)
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
		val = newAccessor(g, s)
	case cur.Flags&FlagAccessor != 0 && !d.IsData():
		val = newAccessor(cur.Getter, cur.Setter)
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
	if k.key == nil {
		if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
			if flags == FlagsData {
				o.elems[k.idx] = val
				vm.touchProto(o)
				return
			}
			// The property no longer fits in elems.
			o.elems[k.idx] = hole
			o.flags |= objSparse
			vm.add(o, vm.indexName(k.idx), val, flags)
			return
		}
		k.key = vm.indexName(k.idx)
	}
	vm.Define(o, k.key, val, flags)
}

// defineArrayLength implements ArraySetLength.
func (vm *VM) defineArrayLength(o *Object, d PropDesc) bool {
	n := o.length
	if d.Has&HasValue != 0 {
		n = vm.toArrayLength(d.Value)
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
	frozen := o.flags&objFrozenLength != 0
	if frozen && d.Has&HasWritable != 0 && d.Flags&FlagWritable != 0 {
		return false
	}
	ok := true
	if n != o.length {
		if frozen {
			return false
		}
		ok = vm.setArrayLength(o, n)
	}
	if d.Has&HasWritable != 0 && d.Flags&FlagWritable == 0 {
		o.flags |= objFrozenLength
	}
	return ok
}

// DeleteProp implements [[Delete]]. It reports false when the property is not configurable.
func (vm *VM) DeleteProp(o *Object, k PropKey) bool {
	if o.class == classProxy {
		return vm.proxyDelete(o, k)
	}
	if k.key == nil {
		if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
			o.elems[k.idx] = hole
			vm.touchProto(o)
			return true
		}
		switch o.class {
		case classTypedArray:
			return !vm.typedArrayHas(o, k.idx)
		case classString:
			if int(k.idx) < o.ext.(*Object).StringLength() {
				return false
			}
		}
		if o.flags&objSparse == 0 {
			return true
		}
		k.key = vm.indexName(k.idx)
	} else {
		if o.flags&objExotic != 0 && vm.isLazyKey(k.key) {
			vm.resolve(o)
		}
		if k.key == vm.K.Length && o.kind == kindArray {
			return false
		}
	}
	p, ok := o.shape.lookup(k.key)
	if !ok {
		return true
	}
	if p.flags&FlagConfigurable == 0 {
		return false
	}
	vm.remove(o, k.key)
	return true
}

// OwnKeys implements [[OwnPropertyKeys]]: array indices in ascending order,
// then strings and then symbols, each in the order of creation.
func (vm *VM) OwnKeys(o *Object) []PropKey {
	if o.class == classProxy {
		return vm.proxyOwnKeys(o)
	}
	if o.flags&objExotic != 0 {
		vm.resolve(o)
	}
	var keys []PropKey
	switch o.class {
	case classString:
		for i := range o.ext.(*Object).StringLength() {
			keys = append(keys, PropKey{idx: uint32(i)})
		}
	case classTypedArray:
		for i := range vm.typedArrayLength(o) {
			keys = append(keys, PropKey{idx: uint32(i)})
		}
	}
	for i, e := range o.elems {
		if !e.isHole() {
			keys = append(keys, PropKey{idx: uint32(i)})
		}
	}
	names := o.shape.keys()
	if o.flags&objSparse != 0 {
		sorted := true
		for _, k := range names {
			if k.kind != kindString {
				continue
			}
			if i, ok := stringIndex(k.str); ok {
				if n := len(keys); n > 0 && keys[n-1].idx > i {
					sorted = false
				}
				keys = append(keys, PropKey{idx: i})
			}
		}
		if !sorted {
			slices.SortFunc(keys, func(a, b PropKey) int {
				switch {
				case a.idx < b.idx:
					return -1
				case a.idx > b.idx:
					return 1
				}
				return 0
			})
		}
	}
	if o.kind == kindArray {
		keys = append(keys, PropKey{key: vm.K.Length})
	}
	for _, k := range names {
		if k.kind != kindString {
			continue
		}
		if o.flags&objSparse != 0 {
			if _, ok := stringIndex(k.str); ok {
				continue
			}
		}
		keys = append(keys, PropKey{key: k})
	}
	for _, k := range names {
		if k.kind == kindSymbol {
			keys = append(keys, PropKey{key: k})
		}
	}
	return keys
}

// IsExtensible implements [[IsExtensible]].
func (vm *VM) IsExtensible(o *Object) bool {
	if o.class == classProxy {
		return vm.proxyIsExtensible(o)
	}
	return o.flags&objNotExtensible == 0
}

// PreventExtensions implements [[PreventExtensions]].
func (vm *VM) PreventExtensions(o *Object) bool {
	if o.class == classProxy {
		return vm.proxyPreventExtensions(o)
	}
	if o.flags&objNotExtensible == 0 {
		if o.flags&objExotic != 0 {
			vm.resolve(o)
		}
		o.flags |= objNotExtensible
		// The inline caches that add properties are keyed by shared shapes.
		o.makePrivate()
	}
	return true
}

// SetPrototypeOf implements [[SetPrototypeOf]].
func (vm *VM) SetPrototypeOf(o *Object, proto *Object) bool {
	if o.class == classProxy {
		return vm.proxySetPrototypeOf(o, proto)
	}
	if o.shape.proto == proto {
		return true
	}
	if o.flags&objNotExtensible != 0 || o == vm.ObjectProto {
		return false
	}
	if o.flags&objExotic != 0 {
		// A function keeps the shape of functionShapeFor only until its properties are stored.
		vm.resolve(o)
	}
	for p := proto; p != nil; p = p.shape.proto {
		if p == o {
			return false
		}
		if p.class == classProxy {
			break
		}
	}
	vm.setProto(o, proto)
	return true
}

// CreateDataPropertyOrThrow implements CreateDataPropertyOrThrow.
func (vm *VM) CreateDataPropertyOrThrow(o *Object, k PropKey, val Value) {
	if !vm.defineOwnProperty(o, k, dataDesc(val, FlagsData)) {
		vm.ThrowTypeError("cannot define property '" + vm.KeyString(k) + "'")
	}
}

// CreateDataProperty implements CreateDataProperty. It reports false when the definition is refused.
func (vm *VM) CreateDataProperty(o *Object, k PropKey, val Value) bool {
	return vm.defineOwnProperty(o, k, dataDesc(val, FlagsData))
}

// DefinePropertyOrThrow implements DefinePropertyOrThrow.
func (vm *VM) DefinePropertyOrThrow(o *Object, k PropKey, d PropDesc) {
	if !vm.defineOwnProperty(o, k, d) {
		vm.ThrowTypeError("cannot redefine property: " + vm.KeyString(k))
	}
}

// IndexKey returns the property key for the array index i.
func IndexKey(i uint32) PropKey {
	return PropKey{idx: i}
}

// NamedKey returns the property key for key, which is an interned string or a symbol.
func NamedKey(key *Object) PropKey {
	return PropKey{key: key}
}

// IsPrivateName reports whether k is the private name of a class element.
func (k PropKey) IsPrivateName() bool {
	return k.key != nil && k.key.class == classPrivateName
}

// Get implements Get.
func (vm *VM) Get(o *Object, k PropKey) Value {
	return vm.getProp(o, k, Value{o: o})
}

// EnumerableOwnKeys implements EnumerableOwnProperties with the kind key.
// It appends the keys to keys and returns the extended slice.
func (vm *VM) EnumerableOwnKeys(o *Object, keys []PropKey) []PropKey {
	if o.flags&objExotic != 0 && o.fn != nil {
		vm.resolve(o)
	}
	if (o.class != classObject && o.class != classArray && o.class != classFunction) || o.flags&(objSparse|objExotic) != 0 {
		for _, k := range vm.OwnKeys(o) {
			if k.IsSymbol() {
				continue
			}
			if d, ok := vm.GetOwnProperty(o, k); ok && d.Flags&FlagEnumerable != 0 {
				keys = append(keys, k)
			}
		}
		return keys
	}
	for i, e := range o.elems {
		if !e.isHole() {
			keys = append(keys, PropKey{idx: uint32(i)})
		}
	}
	s := o.shape
	if s.private {
		for _, k := range s.order {
			if k.kind == kindString && s.table[k].flags&FlagEnumerable != 0 {
				keys = append(keys, PropKey{key: k})
			}
		}
		return keys
	}
	// The chain of a shared shape lists its keys from the last one.
	base := len(keys)
	keys = slices.Grow(keys, int(s.count))[:base+int(s.count)]
	for t := s; t.parent != nil; t = t.parent {
		k := PropKey{}
		if t.key.kind == kindString && t.flags&FlagEnumerable != 0 {
			k.key = t.key
		}
		keys[base+int(t.count)-1] = k
	}
	n := base
	for _, k := range keys[base:] {
		if k.key != nil {
			keys[n] = k
			n++
		}
	}
	return keys[:n]
}
