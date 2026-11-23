// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"math/bits"
	"slices"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

// KeyOfString returns the property key for the string s.
// It does not keep s.
func (vm *VM) KeyOfString(s wtf8.String) PropKey {
	if i, ok := stringIndex(s); ok {
		return PropKey{idx: i}
	}
	if o := vm.strings.interned.get(s); o != nil {
		return PropKey{key: o}
	}
	return PropKey{key: vm.Intern(wtf8.String(strings.Clone(string(s))))}
}

// indexName returns the key under which the array index i is stored in a shape.
func (vm *VM) indexName(i uint32) *Object {
	return vm.Intern(wtf8.String(strconv.FormatUint(uint64(i), 10)))
}

// indexKey returns the interned key for the index i, or nil when the key is not interned,
// in which case no object has a property with that key.
func (vm *VM) indexKey(i uint32) *Object {
	var buf [10]byte
	return vm.strings.interned.get(wtf8.String(strconv.AppendUint(buf[:0], uint64(i), 10)))
}

// KeyValue returns the key k as a string or symbol value.
func (vm *VM) KeyValue(k PropKey) Value {
	if k.key == nil {
		return vm.indexString(k.idx)
	}
	return ObjectValue(k.key)
}

// KeyString describes the key k for messages.
func (vm *VM) KeyString(k PropKey) wtf8.String {
	if k.key == nil {
		return wtf8.String(strconv.FormatUint(uint64(k.idx), 10))
	}
	if k.key.kind == kindSymbol {
		return "Symbol(" + k.key.str + ")"
	}
	return k.key.str
}

// ToPropertyKey implements ToPropertyKey.
func (vm *VM) ToPropertyKey(v Value) PropKey {
	if v.isShortString() && v.o.flags&objInterned == 0 {
		return vm.KeyOfString(v.shortText())
	}
	o := v.o
	if o == numberObject {
		if i, ok := numberIndex(v.n); ok {
			return PropKey{idx: i}
		}
		return PropKey{key: vm.Intern(numconv.NumberToString(v.n))}
	}
	if o == nil {
		return vm.KeyOfString("undefined")
	}
	switch o.kind {
	case kindString:
		if c := o.Str(); len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := o.arrayIndex(); ok {
				return PropKey{idx: i}
			}
		}
		return PropKey{key: vm.stringKey(o)}
	case kindSymbol:
		return PropKey{key: o}
	}
	if o.kind >= kindFirstObject {
		return vm.ToPropertyKey(vm.ToPrimitive(v, HintString))
	}
	return vm.KeyOfString(vm.ToString(v))
}

// QueryKey returns ToPropertyKey(v) for an operation that only reads properties. The key is
// valid only for that operation and must not be used to create a property.
func (vm *VM) QueryKey(v Value) PropKey {
	if v.isShortString() && v.o.flags&objInterned == 0 {
		s := v.shortText()
		if i, ok := stringIndex(s); ok {
			return PropKey{idx: i}
		}
		if o := vm.strings.interned.get(s); o != nil {
			return PropKey{key: o}
		}
		return PropKey{key: v.stringObject()}
	}
	if o := v.o; o != nil && o.kind == kindString {
		return vm.queryStringKey(o)
	}
	return vm.ToPropertyKey(v)
}

// queryStringKey returns the property key for the string o for a read. When no property key has
// the contents of o, it returns a key that no object has as a property.
func (vm *VM) queryStringKey(o *Object) PropKey {
	c := o.Str()
	if len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
		if i, ok := o.arrayIndex(); ok {
			return PropKey{idx: i}
		}
	}
	if o.flags&objInterned != 0 {
		return PropKey{key: o}
	}
	if k, ok := o.extObject(); ok {
		return PropKey{key: k}
	}
	if k := vm.strings.interned.get(c); k != nil {
		o.setExtState(k)
		return PropKey{key: k}
	}
	// Every property key of every object is interned, so o differs from all of them. Not
	// interning o keeps a read of a missing key from adding to the interned strings.
	return PropKey{key: o}
}

// storedKey returns k as the key that a property with k is stored under. A trap of a proxy can
// create the property while a read with a key from [VM.QueryKey] runs, and the checks after the
// trap have to find it.
func (vm *VM) storedKey(k PropKey) PropKey {
	if o := k.key; o != nil && o.kind == kindString && o.flags&objInterned == 0 {
		return PropKey{key: vm.stringKey(o)}
	}
	return k
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
	value  Value
	getter *Object
	setter *Object

	// flags holds the attributes. Only the ones that has names are meaningful.
	flags uint8
	has   uint8
}

// Value returns the [[Value]] field of the data descriptor d.
func (d *PropDesc) Value() Value {
	return d.value
}

// Getter returns the [[Get]] field of the accessor descriptor d, or nil when it is undefined.
func (d *PropDesc) Getter() *Object {
	return d.getter
}

// Setter returns the [[Set]] field of the accessor descriptor d, or nil when it is undefined.
func (d *PropDesc) Setter() *Object {
	return d.setter
}

// Writable reports the [[Writable]] field of d, which is false when absent.
func (d *PropDesc) Writable() bool {
	return d.flags&FlagWritable != 0
}

// Enumerable reports the [[Enumerable]] field of d, which is false when absent.
func (d *PropDesc) Enumerable() bool {
	return d.flags&FlagEnumerable != 0
}

// Configurable reports the [[Configurable]] field of d, which is false when absent.
func (d *PropDesc) Configurable() bool {
	return d.flags&FlagConfigurable != 0
}

// SetValue sets the [[Value]] field of d to v.
func (d *PropDesc) SetValue(v Value) {
	d.value = v
	d.has |= HasValue
}

// SetGetter sets the [[Get]] field of d to f, where nil stands for undefined.
func (d *PropDesc) SetGetter(f *Object) {
	d.getter = f
	d.has |= HasGet
}

// SetSetter sets the [[Set]] field of d to f, where nil stands for undefined.
func (d *PropDesc) SetSetter(f *Object) {
	d.setter = f
	d.has |= HasSet
}

// SetWritable sets the [[Writable]] field of d to b.
func (d *PropDesc) SetWritable(b bool) {
	d.setFlag(HasWritable, FlagWritable, b)
}

// SetEnumerable sets the [[Enumerable]] field of d to b.
func (d *PropDesc) SetEnumerable(b bool) {
	d.setFlag(HasEnumerable, FlagEnumerable, b)
}

// SetConfigurable sets the [[Configurable]] field of d to b.
func (d *PropDesc) SetConfigurable(b bool) {
	d.setFlag(HasConfigurable, FlagConfigurable, b)
}

// setFlag makes the field has of d present with the attribute flag set to b.
func (d *PropDesc) setFlag(has, flag uint8, b bool) {
	d.has |= has
	if b {
		d.flags |= flag
	} else {
		d.flags &^= flag
	}
}

// IsAccessor implements IsAccessorDescriptor.
func (d *PropDesc) IsAccessor() bool {
	return d.has&(HasGet|HasSet) != 0
}

// IsData implements IsDataDescriptor.
func (d *PropDesc) IsData() bool {
	return d.has&(HasValue|HasWritable) != 0
}

func dataDesc(v Value, flags uint8) PropDesc {
	return PropDesc{
		value: v,
		flags: flags,
		has:   HasValue | HasWritable | HasEnumerable | HasConfigurable,
	}
}

// materialize creates the properties that a function has from the start but that are only
// stored once something looks at them.
func (vm *VM) materialize(o *Object) {
	if o.fn.materialized {
		return
	}
	f := o.ownFunction()
	f.materialized = true
	o.flags &^= objExotic
	if c := f.code; c != nil {
		vm.add(o, vm.Key(KeyLength), Number(float64(c.length)), FlagConfigurable)
		vm.add(o, vm.Key(KeyName), vm.SharedString(c.name), FlagConfigurable)
		if c.legacyCallerArguments {
			// Accessors, as the values change while the properties are neither writable nor
			// configurable.
			vm.add(o, vm.Key(keyArguments), newAccessor(c.realm.intrinsics[IntrinsicFunctionArgumentsGetter], nil), FlagAccessor)
			vm.add(o, vm.Key(keyCaller), newAccessor(c.realm.intrinsics[IntrinsicFunctionCallerGetter], nil), FlagAccessor)
		}
		switch {
		case c.isGenerator:
			proto := c.realm.intrinsics[IntrinsicGeneratorPrototype]
			if c.isAsync {
				proto = c.realm.intrinsics[IntrinsicAsyncGeneratorPrototype]
			}
			p := newObject(vm.shapeForRealm(proto, c.realm))
			vm.add(o, vm.Key(KeyPrototype), Value{o: p}, FlagWritable)
		case f.constructor:
			proto := c.realm.intrinsics[IntrinsicObjectPrototype]
			p := newObject(vm.shapeForRealm(proto, c.realm))
			vm.add(p, vm.Key(KeyConstructor), Value{o: o}, FlagsHidden)
			flags := FlagWritable
			if f.classConstructor {
				flags = 0
			}
			vm.add(o, vm.Key(KeyPrototype), Value{o: p}, flags)
		}
		return
	}
	var length int32
	if f.info != nil {
		length = f.info.length
	}
	vm.add(o, vm.Key(KeyLength), Number(float64(length)), FlagConfigurable)
	vm.add(o, vm.Key(KeyName), vm.SharedString(f.nativeName()), FlagConfigurable)
}

// isLazyKey reports whether key names a property that materialize creates.
func (vm *VM) isLazyKey(key *Object) bool {
	return key == vm.Key(KeyPrototype) || key == vm.Key(KeyName) || key == vm.Key(KeyLength) || key == vm.Key(keyCaller) || key == vm.Key(keyArguments)
}

// resolve prepares the exotic object o for an operation on its named properties.
func (vm *VM) resolve(o *Object) {
	if o.fn != nil && !o.fn.materialized {
		vm.materialize(o)
		return
	}
	if o.class == classLazy {
		vm.InitLazyObject(o)
	}
}

// NewLazyObject returns an ordinary object with the prototype proto whose own properties
// init defines. init runs once, before the first operation that needs the own properties of
// the object, or in [VM.InitLazyObject].
func (vm *VM) NewLazyObject(proto *Object, init func(o *Object)) *Object {
	// A cached miss on an ordinary object must still run this object's initializer.
	o := newObject(vm.newShape(proto, vm.curRealm))
	o.class = classLazy
	o.flags |= objExotic
	o.setExtState(init)
	return o
}

// InitLazyObject runs the init function of o, an object of [VM.NewLazyObject], unless it has
// run already.
func (vm *VM) InitLazyObject(o *Object) {
	if o.class != classLazy {
		return
	}
	init := o.extState().(func(*Object))
	o.class = classObject
	o.setExtState(nil)
	o.flags &^= objExotic
	vm.InRealm(o.CreationRealm(), func() {
		init(o)
	})
}

// lookup finds the named property key on o and its prototype chain.
// For the length of an array, which is not stored, it returns the array as holder and slot -1.
// If a proxy or module namespace must perform the lookup, it returns that object and ok=false.
func (vm *VM) lookup(o *Object, key *Object) (holder *Object, p prop, ok bool) {
	length := key == vm.Key(KeyLength)
	for h := o; h != nil; h = h.shape.proto {
		if h.flags&objExotic != 0 {
			if h.class == classProxy || h.class == classModuleNamespace {
				return h, prop{}, false
			}
			if h.class == classLazy || vm.isLazyKey(key) {
				vm.resolve(h)
			}
			if h.class == classTypedArray && isCanonicalNumericString(key) {
				// An integer-indexed object answers for every numeric key and never defers to its prototype.
				return nil, prop{}, false
			}
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
	if o == numberObject {
		return vm.Intrinsic(IntrinsicNumberPrototype)
	}
	if o == nil {
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot read properties of undefined (reading '"+wtf8.ToValidUTF8(vm.KeyString(key))+"')")
	}
	switch o.kind {
	case kindFalse, kindTrue:
		return vm.Intrinsic(IntrinsicBooleanPrototype)
	case kindSymbol:
		return vm.Intrinsic(IntrinsicSymbolPrototype)
	case kindBigInt:
		return vm.Intrinsic(IntrinsicBigIntPrototype)
	case kindNull:
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot read properties of "+vm.DisplayString(v)+" (reading '"+wtf8.ToValidUTF8(vm.KeyString(key))+"')")
	case kindString:
		if v.isShortString() && o.flags&objInterned == 0 {
			o = v.stringObject()
		}
		if p := vm.curRealm.intrinsics[IntrinsicStringPrototype]; o.shape.proto != p {
			// The shape of a string made in another realm leads to the String.prototype of
			// that realm.
			return vm.NewStringObject(o, p)
		}
	}
	// A string has the empty shape whose prototype is String.prototype.
	return o
}

// getNamedAtSite reads a named property through the additional shapes recorded at the site.
func (vm *VM) getNamedAtSite(v Value, key *Object, c *propCache) (Value, *Object) {
	if r := c.reads; r != nil && r.vm == vm && r.epoch == vm.protoEpoch {
		if o := v.o; o != nil && o.kind == kindObject && o.class == classObject && o.flags&objExotic == 0 && o.shape.vm == vm {
			if e, ok := r.shapes[o.shape.readID]; ok && e.count == o.shape.count {
				h := o
				for range e.depth {
					h = h.shape.proto
				}
				c.keep(o.shape)
				return vm.readNamedSlot(o, h, e.prop, c)
			}
		}
	}
	return vm.getNamed(v, key, c)
}

// rememberRead records the location of p for subsequent reads from start's shape.
func (c *propCache) rememberRead(vm *VM, start, holder *Object, p prop) {
	s := start.shape
	if s.readID == 0 {
		if vm.lastReadShapeID == ^uint32(0) {
			return
		}
		vm.lastReadShapeID++
		s.readID = vm.lastReadShapeID
	}
	r := c.reads
	if r == nil {
		r = &namedReads{
			shapes: make(map[uint32]namedRead),
		}
		c.reads = r
	}
	if r.vm != vm || r.epoch != vm.protoEpoch || len(r.shapes) >= 64 {
		clear(r.shapes)
		r.vm = vm
		r.epoch = vm.protoEpoch
	}
	var depth int32
	for h := start; h != holder; h = h.shape.proto {
		depth++
	}
	r.shapes[s.readID] = namedRead{
		depth: depth,
		count: s.count,
		prop:  p,
	}
}

// getNamed returns the named property key of v. When the property is an accessor with a getter,
// getNamed returns the getter instead and the caller has to call it with v as this.
// getNamed fills c, which may be nil, when the lookup can be repeated from the cache.
func (vm *VM) getNamed(v Value, key *Object, c *propCache) (val Value, getter *Object) {
	if key == vm.Key(KeyLength) && v.IsString() {
		return Number(float64(v.StringLength())), nil
	}
	start := vm.protoStart(v, PropKey{key: key})
	if start.kind == kindString && key == vm.Key(KeyLength) {
		return Number(float64(start.StringLength())), nil
	}
	if start.class == classProxy {
		return vm.proxyGet(start, PropKey{key: key}, v), nil
	}
	if start.class == classModuleNamespace {
		return vm.getProp(start, PropKey{key: key}, v), nil
	}
	if start.class == classTypedArray && key == vm.Key(KeyLength) && c != nil {
		// The cache says that the length is the original getter, whose result needs no call.
		e := &c.cacheEntry
		if e.shape != start.shape {
			e = &c.second
		}
		if e.shape == start.shape && e.kind == cacheGetter && e.epoch == vm.protoEpoch && e.holder == vm.Intrinsic(IntrinsicTypedArrayPrototypeLengthGetter) {
			return Number(float64(vm.typedArrayLength(start))), nil
		}
	}
	s, epoch := start.shape, vm.protoEpoch
	var evicted bool
	if c != nil {
		evicted = c.keep(s)
	}
	holder, p, ok := vm.lookup(start, key)
	if !ok {
		if holder != nil {
			return vm.getProp(holder, PropKey{key: key}, v), nil
		}
		if c != nil {
			// Cache the miss as a hit on an object that holds undefined.
			c.setInherited(start.shape, cacheProto, vm.protoEpoch)
			c.ref = &vm.missHolder.slots[0]
		}
		return Undefined, nil
	}
	if p.slot < 0 {
		return Number(float64(holder.length)), nil
	}
	if evicted && start == v.o && start.kind == kindObject && start.class == classObject && start.flags&objExotic == 0 &&
		s == start.shape && s.vm == vm && epoch == vm.protoEpoch {
		c.rememberRead(vm, start, holder, p)
	}
	return vm.readNamedSlot(start, holder, p, c)
}

// readNamedSlot reads p and returns its getter separately for the caller to invoke.
func (vm *VM) readNamedSlot(start, holder *Object, p prop, c *propCache) (val Value, getter *Object) {
	val = holder.slots[p.slot]
	if p.flags&FlagAccessor != 0 {
		// Objects of one shape can hold different own accessors, and replacing an own accessor
		// keeps the shape, so the cache of an own accessor remembers its slot, not its getter.
		if c != nil && holder == start {
			c.shape = start.shape
			c.kind = cacheOwnGetter
			c.slot = p.slot
		}
		g := val.accessor().getter
		if g == nil {
			return Undefined, nil
		}
		if c != nil && holder != start {
			c.setInherited(start.shape, cacheGetter, vm.protoEpoch)
			c.holder = g
		}
		return Undefined, g
	}
	if c != nil {
		if holder == start {
			c.shape = start.shape
			c.slot = p.slot
			c.kind = cacheOwn
		} else {
			c.setInherited(start.shape, cacheProto, vm.protoEpoch)
			c.ref = &holder.slots[p.slot]
		}
	}
	return val, nil
}

// proxyOnChain returns the first proxy or module namespace object on the prototype chain of o,
// which shape lookups stop at.
func proxyOnChain(o *Object) *Object {
	for h := o; h != nil; h = h.shape.proto {
		if h.class == classProxy || h.class == classModuleNamespace {
			return h
		}
	}
	return nil
}

func hasProxyOnChain(o *Object) bool {
	return proxyOnChain(o) != nil
}

// typedArrayOnChain reports whether a typed array is on the prototype chain of o before any proxy.
func typedArrayOnChain(o *Object) bool {
	for h := o; h != nil && h.class != classProxy; h = h.shape.proto {
		if h.class == classTypedArray {
			return true
		}
	}
	return false
}

// GetV returns the named property key of v, calling a getter.
func (vm *VM) GetV(v Value, key *Object) Value {
	val, getter := vm.getNamed(v, key, nil)
	if getter != nil {
		return vm.Call(getter, v)
	}
	return val
}

// GetVWithoutCall returns GetV(v, key) when that runs no code, that is when it reaches no
// getter, proxy or module namespace object. Otherwise ok is false.
func (vm *VM) GetVWithoutCall(v Value, key *Object) (val Value, ok bool) {
	start := vm.protoStart(v, PropKey{key: key})
	if start.kind == kindString && key == vm.Key(KeyLength) {
		return Number(float64(start.StringLength())), true
	}
	if hasProxyOnChain(start) {
		return Undefined, false
	}
	holder, p, found := vm.lookup(start, key)
	switch {
	case !found:
		return Undefined, true
	case p.slot < 0:
		return Number(float64(holder.length)), true
	case p.flags&FlagAccessor != 0:
		return Undefined, false
	}
	return holder.slots[p.slot], true
}

// PropertyCache remembers where [VM.GetVCached] found a property, so that looking up the same key
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
					return *e.ref
				case cacheGetter:
					return vm.Call(e.holder, v)
				}
			}
			if e.kind == cacheOwnGetter {
				if g := o.slots[e.slot].accessor().getter; g != nil {
					return vm.Call(g, v)
				}
				return Undefined
			}
		}
	}
	val, getter := vm.getNamedAtSite(v, k.key, &c.c)
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
	if v.isShortString() && uint(i) < uint(v.StringLength()) {
		return vm.UnitString(v.UnitAt(int(i))), nil
	}
	if o := v.o; o != nil && o.class == classTypedArray {
		return vm.typedArrayGet(o, i), nil
	}
	start := vm.protoStart(v, PropKey{idx: i})
	for h := start; h != nil; h = h.shape.proto {
		if h.objectProperties != nil && int64(i) < int64(len(h.elems)) {
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
		case classModuleNamespace:
			return vm.namespaceGet(h, PropKey{idx: i}), nil
		case classTypedArray:
			// An integer-indexed object answers for every index and never defers to its prototype.
			return vm.typedArrayGet(h, i), nil
		}
		if h.kind == kindString || h.class == classString {
			s := h
			if h.class == classString {
				s = h.extState().(*Object)
			}
			if uint(i) < uint(s.StringLength()) {
				return vm.UnitString(s.UnitAt(int(i))), nil
			}
		}
		if h.flags&objSparse != 0 {
			if p, ok := h.shape.lookup(vm.indexKey(i)); ok {
				val := h.slots[p.slot]
				if p.flags&FlagAccessor != 0 {
					return Undefined, val.accessor().getter
				}
				if u := mappedParam(h, i); u != nil {
					return *u.p, nil
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
	if o.class == classModuleNamespace && vm.isNamespaceExportKey(o, k) {
		return vm.namespaceGetOwnProperty(o, k)
	}
	if k.key == nil {
		if o.objectProperties != nil && int64(k.idx) < int64(len(o.elems)) {
			if e := o.elems[k.idx]; !e.isHole() {
				return dataDesc(e, FlagsData), true
			}
		}
		switch o.class {
		case classTypedArray:
			if v := vm.typedArrayGet(o, k.idx); !v.IsUndefined() {
				return dataDesc(v, typedArrayFlags(o)), true
			}
			return PropDesc{}, false
		case classString:
			s := o.extState().(*Object)
			if uint(k.idx) < uint(s.StringLength()) {
				return dataDesc(vm.UnitString(s.UnitAt(int(k.idx))), FlagEnumerable), true
			}
		}
		if o.flags&objSparse == 0 {
			return PropDesc{}, false
		}
		if u := mappedParam(o, k.idx); u != nil {
			p, _ := o.shape.lookup(vm.indexName(k.idx))
			return dataDesc(*u.p, p.flags), true
		}
		k.key = vm.indexKey(k.idx)
		if k.key == nil {
			return PropDesc{}, false
		}
	} else {
		if o.flags&objExotic != 0 {
			if o.class == classLazy || vm.isLazyKey(k.key) {
				vm.resolve(o)
			}
			if o.class == classTypedArray && isCanonicalNumericString(k.key) {
				return PropDesc{}, false
			}
		}
		if k.key == vm.Key(KeyLength) && o.kind == kindArray {
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
			getter: a.getter,
			setter: a.setter,
			flags:  p.flags,
			has:    HasGet | HasSet | HasEnumerable | HasConfigurable,
		}, true
	}
	return dataDesc(val, p.flags), true
}

// HasOwnProperty reports whether o has the own property k.
func (vm *VM) HasOwnProperty(o *Object, k PropKey) bool {
	_, ok := vm.GetOwnProperty(o, k)
	return ok
}

// HasOwnPropertyPlain is HasOwnProperty of o and the key k when k is a primitive and finding out
// neither throws nor runs JavaScript. It reports false as ok otherwise.
func (vm *VM) HasOwnPropertyPlain(o *Object, k Value) (has, ok bool) {
	if k.IsObject() || o.class == classProxy || o.class == classModuleNamespace {
		return false, false
	}
	return vm.HasOwnProperty(o, vm.QueryKey(k)), true
}

// HasProperty implements [[HasProperty]].
func (vm *VM) HasProperty(o *Object, k PropKey) bool {
	for h := o; h != nil; h = vm.ProtoOf(h) {
		if h.class == classProxy {
			return vm.proxyHas(h, k)
		}
		if h.class == classModuleNamespace && vm.isNamespaceExportKey(h, k) {
			return vm.namespaceHas(h, k)
		}
		if h.class == classTypedArray && isNumericKey(k) {
			return k.key == nil && vm.typedArrayHas(h, k.idx)
		}
		if vm.HasOwnProperty(h, k) {
			return true
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
		if h.class == classModuleNamespace && vm.isNamespaceExportKey(h, k) {
			return vm.namespaceGet(h, k)
		}
		if h.class == classTypedArray && isNumericKey(k) {
			if k.key != nil {
				return Undefined
			}
			return vm.typedArrayGet(h, k.idx)
		}
		d, ok := vm.GetOwnProperty(h, k)
		if !ok {
			continue
		}
		if d.flags&FlagAccessor != 0 {
			if d.getter == nil {
				return Undefined
			}
			return vm.Call(d.getter, receiver)
		}
		return d.value
	}
	return Undefined
}

// growElems reports whether storing the index i should extend elems.
func growElems(elems []Value, i uint32) bool {
	n := int64(len(elems))
	return int64(i) < int64(cap(elems)) || int64(i) <= n+n/2+16
}

// denseFactor is the largest number of slots per element for which [VM.densify] moves the array
// index properties of an object into elems.
const denseFactor = 4

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
		if uint(i) < uint(n) {
			o.elems[i] = val
			vm.touchProto(o)
			return true
		}
		if growElems(o.elems, i) {
			o.elems = reserveElems(o.elems, int(i)+1-n)
			for uint32(len(o.elems)) < i {
				o.elems = append(o.elems, hole)
			}
			o.elems = append(o.elems, val)
			vm.touchProto(o)
			// The elements may have reached the indices in the shape. Checking only when their
			// number passes a power of two bounds the cost of the checks by that of the growth.
			if o.flags&objSparse != 0 && bits.Len(uint(n)) != bits.Len(uint(len(o.elems))) {
				vm.densify(o)
			}
			return true
		}
	}
	o.flags |= objSparse
	vm.add(o, vm.indexName(i), val, flags)
	if o.flags&objIsProto != 0 {
		vm.protoEpoch++
	}
	// Checking only when the number of properties in the shape reaches a power of two bounds
	// the cost of the checks by that of the additions. The elements would need more than i
	// slots.
	if c := int64(o.shape.count); flags == FlagsData && c&(c-1) == 0 && (int64(len(o.elems))+c)*denseFactor > int64(i) {
		vm.densify(o)
	}
	return true
}

// densify moves the array index properties of o from its shape to elems, and clears objSparse,
// when they are all writable, enumerable and configurable data properties and elems would have
// at most denseFactor slots per element.
func (vm *VM) densify(o *Object) {
	if o.kind != kindArray && o.class != classObject || o.flags&(objExotic|objNotExtensible) != 0 {
		return
	}
	// The new shape adds the named properties to root, which keeps its own properties, if any,
	// in the first base slots.
	s := o.shape
	var root *shape
	var base int
	if s.private {
		root = vm.objectRootShape(s.proto, s.realm, o.class)
	} else {
		root = s
		for root.parent != nil {
			root = root.parent
		}
		base = int(root.count)
	}
	keys := s.keys()
	var count int64
	end := int64(len(o.elems))
	for j, k := range keys {
		if k.kind != kindString {
			continue
		}
		i, ok := stringIndex(k.str)
		if !ok {
			continue
		}
		if j < base {
			return
		}
		if p, _ := s.lookup(k); p.flags != FlagsData {
			return
		}
		count++
		end = max(end, int64(i)+1)
	}
	if (int64(len(o.elems))+count)*denseFactor < end {
		return
	}
	present := count
	for _, e := range o.elems {
		if !e.isHole() {
			present++
		}
	}
	if present*denseFactor < end {
		return
	}

	elems := reserveElems(o.elems, int(end)-len(o.elems))
	for int64(len(elems)) < end {
		elems = append(elems, hole)
	}
	slots := o.slots
	o.shape = root
	o.slots = append(make([]Value, 0, int64(len(slots))-count), slots[:base]...)
	for _, k := range keys[base:] {
		p, _ := s.lookup(k)
		if k.kind == kindString {
			if i, ok := stringIndex(k.str); ok {
				elems[i] = slots[p.slot]
				continue
			}
		}
		vm.add(o, k, slots[p.slot], p.flags)
	}
	o.elems = elems
	o.flags &^= objSparse
	if o.kind != kindArray {
		// The shape may be one that o had before, with other slots. The inline caches of
		// global variables point into the slots of the global object while it keeps its
		// shape, so o gets a new one.
		o.makePrivate()
	}
	vm.touchProto(o)
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
		var p Pacer
		for _, idx := range slices.Backward(idxs) {
			p.Step(vm)
			k := vm.indexKey(idx)
			if p, _ := o.shape.lookup(k); p.flags&FlagConfigurable == 0 {
				n = idx + 1
				ok = false
				break
			}
			vm.remove(o, k)
		}
	}
	if int64(n) < int64(len(o.elems)) {
		tail := o.elems[n:]
		o.elems = o.elems[:n]
		oversized := o.elemsOversized()
		if len(tail) > pollChunk {
			// The tail, which may be left uncleared, stays out of reach of later growth.
			o.elems = o.elems[:n:n]
		}
		o.length = n
		if oversized {
			// The tail goes away with the storage.
			vm.reallocElems(o)
		} else {
			clearPolled(vm, tail)
		}
	}
	o.length = n
	return ok
}

// setNamed stores val as the named property key of v. When the property has a setter, setNamed returns it
// and the caller has to call it with v as this. ok is false when the store was refused,
// which is a TypeError in strict code.
func (vm *VM) setNamed(v Value, key *Object, val Value, c *propCache) (setter *Object, ok bool) {
	o := v.o
	if o == nil || o.kind < kindFirstObject {
		if v.IsNullish() {
			vm.ThrowTypeError(vm.CurrentRealm(), "cannot set properties of "+vm.DisplayString(v)+" (setting '"+wtf8.ToValidUTF8(vm.KeyString(PropKey{key: key}))+"')")
		}
		// A store to a primitive only has an effect when a setter is inherited.
		start := vm.protoStart(v, PropKey{key: key})
		if hasProxyOnChain(start) {
			if start.kind == kindString && key == vm.Key(KeyLength) {
				return nil, false
			}
			return nil, vm.setProp(start, PropKey{key: key}, val, v)
		}
		if h, p, found := vm.lookup(start, key); found && p.slot >= 0 && p.flags&FlagAccessor != 0 {
			if s := h.slots[p.slot].accessor().setter; s != nil {
				return s, true
			}
		}
		return nil, false
	}
	if o.class == classProxy || o.class == classModuleNamespace || o.class == classTypedArray && isCanonicalNumericString(key) {
		return nil, vm.setProp(o, PropKey{key: key}, val, v)
	}
	if o.flags&objExotic != 0 {
		// The initial properties of a function precede every property that a store adds.
		vm.resolve(o)
	}
	if o.kind == kindArray && key == vm.Key(KeyLength) {
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
			// An own setter is not cached, for the same reason as an own getter in getNamed.
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
		if hasProxyOnChain(proto) || isCanonicalNumericString(key) && typedArrayOnChain(proto) {
			return nil, vm.setProp(o, PropKey{key: key}, val, v)
		}
		if h, p, found := vm.lookup(proto, key); found {
			if p.slot >= 0 && p.flags&FlagAccessor != 0 {
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
		vm.ThrowRangeError(vm.CurrentRealm(), "invalid array length")
	}
	return u
}

// setIndex stores val as the property of v whose key is the array index i.
// The results are those of setNamed.
func (vm *VM) setIndex(v Value, i uint32, val Value) (setter *Object, ok bool) {
	o := v.o
	if o == nil || o.kind < kindFirstObject {
		if v.IsNullish() {
			vm.ThrowTypeError(vm.CurrentRealm(), "cannot set properties of "+vm.DisplayString(v)+" (setting '"+wtf8.ToValidUTF8(vm.KeyString(PropKey{idx: i}))+"')")
		}
		if o != nil && o.kind == kindString && int64(i) < int64(v.StringLength()) {
			return nil, false
		}
		// Only an inherited setter or a proxy can observe the store.
		return nil, vm.setProp(vm.protoStart(v, PropKey{idx: i}), PropKey{idx: i}, val, v)
	}
	if int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = val
		return nil, true
	}
	if o.class == classTypedArray {
		return nil, vm.typedArraySet(o, i, val)
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
		if h.class == classModuleNamespace {
			return false
		}
		if h.class == classTypedArray && isNumericKey(k) {
			if typedArrayImmutable(h) {
				return false
			}
			if k.key != nil {
				vm.typedArraySetNamed(h, val, receiver)
				return true
			}
			if receiver.o == h {
				vm.typedArraySetElement(h.extState().(*TypedArray), k.idx, val)
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
	if found && d.flags&FlagAccessor != 0 {
		if d.setter == nil {
			return false
		}
		vm.Call(d.setter, receiver, val)
		return true
	}
	if found && d.flags&FlagWritable == 0 {
		return false
	}
	if !receiver.IsObject() {
		return false
	}
	r := receiver.o
	if o == vm.curRealm.global && r == vm.curRealm.globalThis {
		// A global binding stores on the backing object even after detachment.
		r = o
	}
	if existing, ok := vm.GetOwnProperty(r, k); ok {
		if existing.flags&FlagAccessor != 0 || existing.flags&FlagWritable == 0 {
			return false
		}
		return vm.defineOwnProperty(r, k, PropDesc{
			value: val,
			has:   HasValue,
		})
	}
	return vm.defineOwnProperty(r, k, dataDesc(val, FlagsData))
}

// Set stores val as the property k of o and throws a TypeError when the store is refused.
func (vm *VM) Set(o *Object, k PropKey, val Value) {
	if !vm.setProp(o, k, val, Value{o: o}) {
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot assign to read only property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"'")
	}
}

// defineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (vm *VM) defineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	if o.class == classProxy {
		return vm.proxyDefineOwnProperty(o, k, d)
	}
	if o.class == classModuleNamespace && vm.isNamespaceExportKey(o, k) {
		return vm.namespaceDefineOwnProperty(o, k, d)
	}
	if o.class == classTypedArray && isNumericKey(k) {
		return k.key == nil && vm.typedArrayDefine(o, k.idx, d)
	}
	if o.kind == kindArray && k.key == vm.Key(KeyLength) {
		return vm.defineArrayLength(o, d)
	}
	if k.key == nil {
		if u := mappedParam(o, k.idx); u != nil {
			return vm.defineMappedArgument(o, k.idx, u, d)
		}
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
		flags := d.flags & d.attrMask()
		val := d.value
		if d.has&HasValue == 0 {
			val = Undefined
		}
		if d.IsAccessor() {
			flags = flags&^FlagWritable | FlagAccessor
			val = newAccessor(d.getter, d.setter)
		}
		if k.key == nil {
			return vm.addIndex(o, k.idx, val, flags)
		}
		vm.add(o, k.key, val, flags)
		return true
	}

	// ValidateAndApplyPropertyDescriptor for an existing property.
	if cur.flags&FlagConfigurable == 0 && !nonConfigurableCompatible(&cur, &d) {
		return false
	}

	// Merge the fields that are present into the current property.
	flags := cur.flags
	if d.has&HasEnumerable != 0 {
		flags = flags&^FlagEnumerable | d.flags&FlagEnumerable
	}
	if d.has&HasConfigurable != 0 {
		flags = flags&^FlagConfigurable | d.flags&FlagConfigurable
	}
	var val Value
	switch {
	case d.IsAccessor():
		g, s := cur.getter, cur.setter
		if cur.flags&FlagAccessor == 0 {
			g, s = nil, nil
		}
		if d.has&HasGet != 0 {
			g = d.getter
		}
		if d.has&HasSet != 0 {
			s = d.setter
		}
		flags = flags&^FlagWritable | FlagAccessor
		val = newAccessor(g, s)
	case cur.flags&FlagAccessor != 0 && !d.IsData():
		val = newAccessor(cur.getter, cur.setter)
	default:
		val = cur.value
		if cur.flags&FlagAccessor != 0 {
			val = Undefined
			flags &^= FlagAccessor | FlagWritable
		}
		if d.has&HasValue != 0 {
			val = d.value
		}
		if d.has&HasWritable != 0 {
			flags = flags&^FlagWritable | d.flags&FlagWritable
		}
	}
	vm.replaceOwn(o, k, val, flags)
	return true
}

// attrMask returns the attribute bits that d specifies.
func (d *PropDesc) attrMask() uint8 {
	var m uint8
	if d.has&HasEnumerable != 0 {
		m |= FlagEnumerable
	}
	if d.has&HasConfigurable != 0 {
		m |= FlagConfigurable
	}
	if d.has&HasWritable != 0 {
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
	if d.has&HasValue != 0 {
		n = vm.toArrayLength(d.value)
	}
	if d.IsAccessor() {
		return false
	}
	if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable != 0 {
		return false
	}
	if d.has&HasEnumerable != 0 && d.flags&FlagEnumerable != 0 {
		return false
	}
	frozen := o.flags&objFrozenLength != 0
	if frozen && d.has&HasWritable != 0 && d.flags&FlagWritable != 0 {
		return false
	}
	ok := true
	if n != o.length {
		if frozen {
			return false
		}
		ok = vm.setArrayLength(o, n)
	}
	if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		o.flags |= objFrozenLength
		vm.touchProto(o)
	}
	return ok
}

// DeleteProp implements [[Delete]]. It reports false when the property is not configurable.
func (vm *VM) DeleteProp(o *Object, k PropKey) bool {
	if o.class == classProxy {
		return vm.proxyDelete(o, k)
	}
	if o.class == classModuleNamespace && vm.isNamespaceExportKey(o, k) {
		return vm.namespaceDelete(o, k)
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
			if uint(k.idx) < uint(o.extState().(*Object).StringLength()) {
				return false
			}
		}
		if o.flags&objSparse == 0 {
			return true
		}
		if mappedParam(o, k.idx) != nil {
			return vm.deleteMappedArgument(o, k.idx)
		}
		k.key = vm.indexKey(k.idx)
		if k.key == nil {
			return true
		}
	} else {
		if o.flags&objExotic != 0 {
			if o.class == classLazy || vm.isLazyKey(k.key) {
				vm.resolve(o)
			}
			if o.class == classTypedArray && isCanonicalNumericString(k.key) {
				return true
			}
		}
		if k.key == vm.Key(KeyLength) && o.kind == kindArray {
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
	if o.class == classModuleNamespace {
		return vm.namespaceOwnKeys(o)
	}
	if o.flags&objExotic != 0 {
		vm.resolve(o)
	}
	// Every element, every named property and length.
	keys := make([]PropKey, 0, len(o.elems)+int(o.shape.count)+1)
	switch o.class {
	case classString:
		keys = vm.appendIndexKeys(keys, o.extState().(*Object).StringLength())
	case classTypedArray:
		keys = vm.appendIndexKeys(keys, vm.typedArrayLength(o))
	}
	keys = vm.appendElemKeys(keys, o.elems)
	names := o.shape.keys()
	if o.flags&objSparse != 0 {
		sorted := true
		for j, k := range names {
			vm.pollAt(j)
			if k.kind != kindString {
				continue
			}
			if i, ok := k.arrayIndex(); ok {
				if n := len(keys); n > 0 && keys[n-1].idx > i {
					sorted = false
				}
				keys = append(keys, PropKey{idx: i})
			}
		}
		if !sorted {
			vm.sortIndexKeys(keys)
		}
	}
	if o.kind == kindArray {
		keys = append(keys, PropKey{key: vm.Key(KeyLength)})
	}
	for j, k := range names {
		vm.pollAt(j)
		if k.kind != kindString {
			continue
		}
		if o.flags&objSparse != 0 {
			if _, ok := k.knownArrayIndex(); ok {
				continue
			}
		}
		keys = append(keys, PropKey{key: k})
	}
	for j, k := range names {
		vm.pollAt(j)
		if k.kind == kindSymbol && k.class != classPrivateName {
			keys = append(keys, PropKey{key: k})
		}
	}
	return keys
}

// pollAt checks for an interruption at every PollInterval-th step i of a loop, other than the
// first.
func (vm *VM) pollAt(i int) {
	if i > 0 && i%PollInterval == 0 {
		vm.Poll()
	}
}

// sortIndexKeys sorts the index keys keys in ascending order, checking for an interruption while
// it sorts many.
func (vm *VM) sortIndexKeys(keys []PropKey) {
	if len(keys) <= PollInterval {
		slices.SortFunc(keys, func(a, b PropKey) int {
			return cmp.Compare(a.idx, b.idx)
		})
		return
	}
	// A radix sort by 16 bits at a time, which ends with the keys back in keys.
	src, dst := keys, make([]PropKey, len(keys))
	count := make([]int, 1<<16)
	for shift := 0; shift < 32; shift += 16 {
		clear(count)
		for i, k := range src {
			vm.pollAt(i)
			count[k.idx>>shift&0xFFFF]++
		}
		sum := 0
		for d, c := range count {
			count[d] = sum
			sum += c
		}
		for i, k := range src {
			vm.pollAt(i)
			d := k.idx >> shift & 0xFFFF
			dst[count[d]] = k
			count[d]++
		}
		src, dst = dst, src
	}
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
	if o.class == classTypedArray && !o.extState().(*TypedArray).isTypedArrayFixedLength() {
		return false
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
	if o.flags&objNotExtensible != 0 || o.shape.proto == nil && o == o.shape.realm.intrinsics[IntrinsicObjectPrototype] {
		return false
	}
	if o.flags&objExotic != 0 {
		// A function keeps the shape of functionShapeFor only until its properties are stored.
		vm.resolve(o)
	}
	for p := proto; p != nil; p = p.shape.proto {
		if p.class == classProxy {
			st := p.extState().(*proxyState)
			if !st.isGlobal() || st.target == nil {
				break
			}
			// Global proxies forward to the same backing global for cycle
			// detection regardless of the realm setting the prototype.
			p = st.target
		}
		if p == o {
			return false
		}
	}
	vm.setProto(o, proto)
	return true
}

// CreateDataPropertyOrThrow implements CreateDataPropertyOrThrow.
func (vm *VM) CreateDataPropertyOrThrow(o *Object, k PropKey, val Value) {
	if !vm.CreateDataProperty(o, k, val) {
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot define property '"+wtf8.ToValidUTF8(vm.KeyString(k))+"'")
	}
}

// CreateDataProperty implements CreateDataProperty. It reports false when the definition is refused.
func (vm *VM) CreateDataProperty(o *Object, k PropKey, val Value) bool {
	// An array whose index properties are all in its elements has none at the index just
	// past them.
	if k.key == nil && o.kind == kindArray && int(k.idx) == len(o.elems) && o.flags&(objSparse|objExotic) == 0 {
		return vm.addIndex(o, k.idx, val, FlagsData)
	}
	return vm.defineOwnProperty(o, k, dataDesc(val, FlagsData))
}

// DefinePropertyOrThrow implements DefinePropertyOrThrow.
func (vm *VM) DefinePropertyOrThrow(o *Object, k PropKey, d PropDesc) {
	if !vm.defineOwnProperty(o, k, d) {
		vm.ThrowTypeError(vm.CurrentRealm(), "cannot redefine property: "+wtf8.ToValidUTF8(vm.KeyString(k)))
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

// Get implements Get.
func (vm *VM) Get(o *Object, k PropKey) Value {
	return vm.getProp(o, k, Value{o: o})
}

// GetWithReceiver implements o.[[Get]](k, receiver).
func (vm *VM) GetWithReceiver(o *Object, k PropKey, receiver Value) Value {
	return vm.getProp(o, k, receiver)
}

// SetWithReceiver implements o.[[Set]](k, val, receiver). It reports false when the store was refused.
func (vm *VM) SetWithReceiver(o *Object, k PropKey, val Value, receiver Value) bool {
	return vm.setProp(o, k, val, receiver)
}

// DefineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (vm *VM) DefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	return vm.defineOwnProperty(o, k, d)
}

// isEnumerableString reports whether key is a string and flags make the property enumerable.
func isEnumerableString(key *Object, flags uint8) bool {
	return key.kind == kindString && flags&FlagEnumerable != 0
}

// enumerableKey returns key as a property key when it is a string and flags make it enumerable,
// and the zero PropKey otherwise.
func enumerableKey(key *Object, flags uint8) PropKey {
	if isEnumerableString(key, flags) {
		return PropKey{key: key}
	}
	return PropKey{}
}

// appendIndexKeys appends the keys of the indices from 0 up to n to keys, checking for an
// interruption while it appends many.
func (vm *VM) appendIndexKeys(keys []PropKey, n int) []PropKey {
	for i := 0; i < n; i += PollInterval {
		if i > 0 {
			vm.Poll()
		}
		keys = appendIndexRun(keys, i, min(i+PollInterval, n))
	}
	return keys
}

// appendIndexRun appends the keys of the indices from i up to j to keys.
func appendIndexRun(keys []PropKey, i, j int) []PropKey {
	for ; i < j; i++ {
		keys = append(keys, PropKey{idx: uint32(i)})
	}
	return keys
}

// appendElemKeys appends the keys of the elements in elems to keys, checking for an
// interruption while it appends many.
func (vm *VM) appendElemKeys(keys []PropKey, elems []Value) []PropKey {
	for i := 0; i < len(elems); i += PollInterval {
		if i > 0 {
			vm.Poll()
		}
		keys = appendElemRun(keys, elems[i:min(i+PollInterval, len(elems))], i)
	}
	return keys
}

// appendElemRun appends the keys of the elements in elems, the first of which has the index
// base, to keys.
func appendElemRun(keys []PropKey, elems []Value, base int) []PropKey {
	for i, e := range elems {
		if !e.isHole() {
			keys = append(keys, PropKey{idx: uint32(base + i)})
		}
	}
	return keys
}

// appendEnumerableNames appends the enumerable string keys among names, which are keys of the
// private shape s, to keys.
func appendEnumerableNames(keys []PropKey, s *shape, names []*Object) []PropKey {
	for _, k := range names {
		if k != nil && k.kind == kindString && s.table[k].flags&FlagEnumerable != 0 {
			keys = append(keys, PropKey{key: k})
		}
	}
	return keys
}

// EnumerableOwnKeys implements EnumerableOwnProperties with the kind key.
// It appends the keys to keys and returns the extended slice.
func (vm *VM) EnumerableOwnKeys(o *Object, keys []PropKey) []PropKey {
	if o.flags&objExotic != 0 && (o.fn != nil || o.class == classLazy) {
		vm.resolve(o)
	}
	if (o.class != classObject && o.class != classArray && o.class != classFunction) || o.flags&(objSparse|objExotic) != 0 {
		for i, k := range vm.OwnKeys(o) {
			vm.pollAt(i)
			if k.IsSymbol() {
				continue
			}
			if d, ok := vm.GetOwnProperty(o, k); ok && d.flags&FlagEnumerable != 0 {
				keys = append(keys, k)
			}
		}
		return keys
	}
	keys = vm.appendElemKeys(keys, o.elems)
	s := o.shape
	if s.private {
		for i := 0; i < len(s.order); i += PollInterval {
			if i > 0 {
				vm.Poll()
			}
			keys = appendEnumerableNames(keys, s, s.order[i:min(i+PollInterval, len(s.order))])
		}
		return keys
	}
	// The chain of a shared shape lists its keys from the last one.
	base := len(keys)
	keys = slices.Grow(keys, int(s.count))[:base+int(s.count)]
	t := s
	for ; t.parent != nil; t = t.parent {
		keys[base+int(t.count)-1] = enumerableKey(t.key, t.flags)
	}
	for _, e := range t.small {
		keys[base+int(e.slot)] = enumerableKey(e.key, e.flags)
	}
	for key, p := range t.table {
		keys[base+int(p.slot)] = enumerableKey(key, p.flags)
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
