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
func (cx *ExecutionContext) KeyOfString(s wtf8.String) PropKey {
	if i, ok := stringIndex(s); ok {
		return PropKey{idx: i}
	}
	if o := cx.strings.lookup(s); o != nil {
		return PropKey{key: o}
	}
	return PropKey{key: cx.Intern(wtf8.String(strings.Clone(string(s))))}
}

// indexName returns the key under which the array index i is stored in a shape.
func (cx *ExecutionContext) indexName(i uint32) *Object {
	return cx.Intern(wtf8.String(strconv.FormatUint(uint64(i), 10)))
}

// indexKey returns the interned key for the index i, or nil when the key is not interned,
// in which case no object has a property with that key.
func (sp *space) indexKey(i uint32) *Object {
	if i < 10 {
		return sp.strings.character(byte('0' + i)).o
	}
	var buf [10]byte
	return sp.strings.lookup(wtf8.String(strconv.AppendUint(buf[:0], uint64(i), 10)))
}

// KeyValue returns the key k as a string or symbol value.
func (cx *ExecutionContext) KeyValue(k PropKey) Value {
	if k.key == nil {
		return cx.indexString(k.idx)
	}
	return ObjectValue(k.key)
}

// KeyString describes the key k for messages.
func (ag *agent) KeyString(k PropKey) wtf8.String {
	if k.key == nil {
		return wtf8.String(strconv.FormatUint(uint64(k.idx), 10))
	}
	if k.key.kind == kindSymbol {
		return "Symbol(" + k.key.str + ")"
	}
	return k.key.str
}

// ToPropertyKey implements ToPropertyKey.
func (cx *ExecutionContext) ToPropertyKey(v Value) PropKey {
	if v.isShortString() && v.o.flags&objInterned == 0 {
		return cx.KeyOfString(v.shortText())
	}
	o := v.o
	if o == numberObject {
		if i, ok := numberIndex(v.n); ok {
			return PropKey{idx: i}
		}
		return PropKey{key: cx.Intern(numconv.NumberToString(v.n))}
	}
	if o == nil {
		return cx.KeyOfString("undefined")
	}
	switch o.kind {
	case kindString:
		if c := o.Str(); len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := o.arrayIndex(); ok {
				return PropKey{idx: i}
			}
		}
		return PropKey{key: cx.stringKey(o)}
	case kindSymbol:
		return PropKey{key: o}
	}
	if o.kind >= kindFirstObject {
		return cx.ToPropertyKey(cx.ToPrimitive(v, HintString))
	}
	return cx.KeyOfString(cx.ToString(v))
}

// QueryKey is a property key for an operation that only reads properties. It may be a string that
// no property key has.
type QueryKey struct {
	// key and idx are as in [PropKey], except that key is not interned when idx is missingKey.
	key *Object
	idx uint32
}

// missingKey is the idx of a query key whose key has the contents of no property key.
const missingKey = 1

func (k QueryKey) missing() bool {
	return k.key != nil && k.idx == missingKey
}

// QueryKey returns ToPropertyKey(v) as a query key, which is valid until code runs or a property
// key is made.
func (cx *ExecutionContext) QueryKey(v Value) QueryKey {
	if o := v.o; o != nil && o.kind == kindString {
		if v.isShortString() && o.flags&objInterned == 0 {
			return cx.shortQueryKey(v)
		}
		return cx.queryStringKey(o)
	}
	return QueryKey(cx.ToPropertyKey(v))
}

// shortQueryKey returns the query key for the short string v, which is not interned.
func (sp *space) shortQueryKey(v Value) QueryKey {
	s := v.shortText()
	if i, ok := stringIndex(s); ok {
		return QueryKey{idx: i}
	}
	if o := sp.strings.lookup(s); o != nil {
		return QueryKey{key: o}
	}
	return QueryKey{
		key: v.stringObject(),
		idx: missingKey,
	}
}

// queryStringKey returns the query key for the string o.
func (sp *space) queryStringKey(o *Object) QueryKey {
	c := o.Str()
	if len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
		if i, ok := o.arrayIndex(); ok {
			return QueryKey{idx: i}
		}
	}
	if o.flags&objInterned != 0 {
		return QueryKey{key: o}
	}
	if k, ok := o.extObject(); ok {
		return QueryKey{key: k}
	}
	if k := sp.strings.lookup(c); k != nil {
		o.setExtState(k)
		return QueryKey{key: k}
	}
	// Not interning o keeps a read of a missing key from adding to the interned strings.
	return QueryKey{
		key: o,
		idx: missingKey,
	}
}

// propKey returns k as a property key.
func (cx *ExecutionContext) propKey(k QueryKey) PropKey {
	if k.missing() {
		return PropKey{key: cx.stringKey(k.key)}
	}
	return PropKey(k)
}

// QueryProperty is HasProperty of o and k.
func (cx *ExecutionContext) QueryProperty(o *Object, k QueryKey) bool {
	if k.missing() {
		return cx.queryMissing(o, k)
	}
	return cx.HasProperty(o, PropKey(k))
}

// queryMissing is QueryProperty of o and k, which is missing.
func (cx *ExecutionContext) queryMissing(o *Object, k QueryKey) bool {
	if _, ok := cx.Quiet().hasPropertyPlain(o, k.key); ok {
		return false
	}
	return cx.HasProperty(o, cx.propKey(k))
}

// QueryOwnProperty is HasOwnProperty of o and k.
func (cx *ExecutionContext) QueryOwnProperty(o *Object, k QueryKey) bool {
	if k.missing() {
		if !o.delegatesPropertyAccess() {
			return false
		}
	} else if k.key != nil && o.shape.family == ordinaryRoot {
		_, ok := o.shape.lookup(k.key)
		return ok
	}
	return cx.HasOwnProperty(o, cx.propKey(k))
}

// getMissing is getNamed of v and k, which is missing.
func (cx *ExecutionContext) getMissing(v Value, k QueryKey) (Value, *Object) {
	start := cx.protoStart(v, PropKey{key: k.key})
	if _, ok := cx.Quiet().hasPropertyPlain(start, k.key); ok {
		return Undefined, nil
	}
	return cx.getNamed(v, cx.propKey(k).key, nil)
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
func (cx *ExecutionContext) materialize(o *Object) {
	if o.fn.materialized {
		return
	}
	f := o.ownFunction()
	f.materialized = true
	o.flags &^= objExotic
	if r := o.shape.realm; o.shape == r.shapes.function {
		o.shape = r.shapes.materializedFunction
	} else {
		cx.rebuildShape(o, o.shape.proto, ordinaryRoot)
	}
	if c := f.code; c != nil {
		cx.add(o, cx.Key(KeyLength), Number(float64(c.length)), FlagConfigurable)
		cx.add(o, cx.Key(KeyName), cx.SharedString(c.name), FlagConfigurable)
		if c.legacyCallerArguments {
			// Accessors, as the values change while the properties are neither writable nor
			// configurable.
			cx.add(o, cx.Key(keyArguments), newAccessor(c.realm.intrinsics[IntrinsicFunctionArgumentsGetter], nil), FlagAccessor)
			cx.add(o, cx.Key(keyCaller), newAccessor(c.realm.intrinsics[IntrinsicFunctionCallerGetter], nil), FlagAccessor)
		}
		switch {
		case c.isGenerator:
			proto := c.realm.intrinsics[IntrinsicGeneratorPrototype]
			if c.isAsync {
				proto = c.realm.intrinsics[IntrinsicAsyncGeneratorPrototype]
			}
			p := newObject(cx.shapeForRealm(proto, c.realm))
			cx.add(o, cx.Key(KeyPrototype), Value{o: p}, FlagWritable)
		case f.constructor:
			proto := c.realm.intrinsics[IntrinsicObjectPrototype]
			p := newObject(cx.shapeForRealm(proto, c.realm))
			cx.add(p, cx.Key(KeyConstructor), Value{o: o}, FlagsHidden)
			flags := FlagWritable
			if f.classConstructor {
				flags = 0
			}
			cx.add(o, cx.Key(KeyPrototype), Value{o: p}, flags)
		}
		return
	}
	var length int32
	if f.info != nil {
		length = f.info.length
	}
	cx.add(o, cx.Key(KeyLength), Number(float64(length)), FlagConfigurable)
	cx.add(o, cx.Key(KeyName), cx.SharedString(f.nativeName()), FlagConfigurable)
}

// isLazyKey reports whether key names a property that materialize creates.
func (sp *space) isLazyKey(key *Object) bool {
	return key == sp.Key(KeyPrototype) || key == sp.Key(KeyName) || key == sp.Key(KeyLength) || key == sp.Key(keyCaller) || key == sp.Key(keyArguments)
}

// resolve prepares the exotic object o for an operation on its named properties.
func (cx *ExecutionContext) resolve(o *Object) {
	if o.fn != nil && !o.fn.materialized {
		cx.materialize(o)
		return
	}
	if o.class == classLazy {
		cx.InitLazyObject(o)
	}
}

// resolveForKey materializes the own properties needed for key.
func (cx *ExecutionContext) resolveForKey(o *Object, key *Object) {
	if o.class != classLazy {
		if o.fn != nil && key != nil && cx.isLazyKey(key) {
			cx.materialize(o)
		}
		return
	}
	cx.InitLazyObject(o)
}

// NewLazyObject returns an ordinary object with prototype proto. The first operation that
// needs its own properties, or a call to [agent.InitLazyObject], runs init once.
func (cx *ExecutionContext) NewLazyObject(proto *Object, init func(*ExecutionContext, *Object)) *Object {
	// A cached miss on an ordinary object must still run this object's initializer.
	o := newObject(cx.rootShape(proto, cx.realmPart, delegatedRoot))
	o.class = classLazy
	o.flags |= objExotic
	o.setExtState(init)
	return o
}

// InitLazyObject runs the init function of o, an object of [ExecutionContext.NewLazyObject], unless it has
// run already.
func (ag *agent) InitLazyObject(o *Object) {
	if o.class != classLazy {
		return
	}
	init := o.extState().(func(*ExecutionContext, *Object))
	o.class = classObject
	o.setExtState(nil)
	o.flags &^= objExotic
	cx := o.CreationContext()
	cx.rebuildShape(o, o.shape.proto, ordinaryRoot)
	cx.Run(func() {
		init(cx, o)
	})
}

type namedLookupKind uint8

const (
	lookupAbsent      namedLookupKind = iota // The chain ended. A miss or property addition can be cached.
	lookupSlot                               // A slot supplies the value or accessor.
	lookupLength                             // The implicit length of an array or string has no slot.
	lookupNumericMiss                        // A typed array answers undefined on reads and handles stores itself.
	lookupDelegate                           // The holder must perform each operation.
)

type namedLookup struct {
	kind   namedLookupKind
	holder *Object
	prop   prop
}

func (r namedLookup) length(receiver Value) Value {
	if r.holder.kind == kindString {
		return Number(float64(receiver.StringLength()))
	}
	return Number(float64(r.holder.length))
}

// lookup finds the named property key on o and its prototype chain.
func (cx *ExecutionContext) lookup(o *Object, key *Object) namedLookup {
	for h := o; h != nil; h = h.shape.proto {
		switch h.shape.family {
		case delegatedRoot:
			return namedLookup{
				kind:   lookupDelegate,
				holder: h,
			}
		case arrayRoot, primitiveStringRoot:
			if key == cx.Key(KeyLength) {
				return namedLookup{
					kind:   lookupLength,
					holder: h,
				}
			}
		case typedArrayRoot:
			if isCanonicalNumericString(key) {
				return namedLookup{
					kind:   lookupNumericMiss,
					holder: h,
				}
			}
		case lazyFunctionRoot:
			if cx.isLazyKey(key) {
				cx.materialize(h)
			}
		}
		if p, ok := h.shape.lookup(key); ok {
			return namedLookup{
				kind:   lookupSlot,
				holder: h,
				prop:   p,
			}
		}
	}
	return namedLookup{
		kind: lookupAbsent,
	}
}

// protoStart returns the object at which a property lookup on the value v starts.
// It throws a TypeError for undefined and null.
func (cx *ExecutionContext) protoStart(v Value, key PropKey) *Object {
	o := v.o
	if o == numberObject {
		return cx.Intrinsic(IntrinsicNumberPrototype)
	}
	if o == nil {
		cx.ThrowTypeError("cannot read properties of undefined (reading '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "')")
	}
	switch o.kind {
	case kindFalse, kindTrue:
		return cx.Intrinsic(IntrinsicBooleanPrototype)
	case kindSymbol:
		return cx.Intrinsic(IntrinsicSymbolPrototype)
	case kindBigInt:
		return cx.Intrinsic(IntrinsicBigIntPrototype)
	case kindNull:
		cx.ThrowTypeError("cannot read properties of " + cx.DisplayString(v) + " (reading '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "')")
	case kindString:
		if key.key == cx.Key(KeyLength) {
			return o
		}
		if v.isShortString() && o.flags&objInterned == 0 {
			o = v.stringObject()
		}
		if p := cx.Intrinsic(IntrinsicStringPrototype); o.shape.proto != p {
			// The shape of a string made in another realm leads to the String.prototype of
			// that realm.
			return cx.NewStringObject(o, p)
		}
	}
	// A string has the empty shape whose prototype is String.prototype.
	return o
}

// getNamedAtSite reads a named property through the additional shapes recorded at the site.
func (cx *ExecutionContext) getNamedAtSite(v Value, key *Object, c *propCache) (Value, *Object) {
	if r := c.reads; r != nil && r.space == cx.space && r.epoch == cx.protoEpoch {
		if o := v.o; o != nil && o.kind == kindObject && o.class == classObject && o.flags&objExotic == 0 && o.shape.space == cx.space {
			if e, ok := r.shapes[o.shape.readID]; ok && e.count == o.shape.count {
				h := o
				for range e.depth {
					h = h.shape.proto
				}
				if validateExec {
					cx.checkRememberedRead(v, key, h, e.prop)
				}
				c.keep(o.shape)
				return cx.readNamedSlot(o, h, e.prop, c)
			}
		}
	}
	return cx.getNamed(v, key, c)
}

// rememberRead records the location of p for subsequent reads from start's shape.
func (c *propCache) rememberRead(cx *ExecutionContext, start, holder *Object, p prop) {
	s := start.shape
	if s.readID == 0 {
		if cx.lastReadShapeID == ^uint32(0) {
			return
		}
		cx.lastReadShapeID++
		s.readID = cx.lastReadShapeID
	}
	r := c.reads
	if r == nil {
		r = &namedReads{
			shapes: make(map[uint32]namedRead),
		}
		c.reads = r
	}
	if r.space != cx.space || r.epoch != cx.protoEpoch || len(r.shapes) >= 64 {
		clear(r.shapes)
		r.space = cx.space
		r.epoch = cx.protoEpoch
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
func (cx *ExecutionContext) getNamed(v Value, key *Object, c *propCache) (val Value, getter *Object) {
	if o := v.o; o != nil && o.class == classProxy {
		return cx.proxyGet(o, PropKey{key: key}, v), nil
	}
	start := cx.protoStart(v, PropKey{key: key})
	if start.class == classTypedArray && key == cx.Key(KeyLength) && c != nil {
		// The cache says that the length is the original getter, whose result needs no call.
		e := &c.cacheEntry
		if e.shape != start.shape {
			e = &c.second
		}
		if e.shape == start.shape && e.kind == cacheGetter && e.epoch == cx.protoEpoch && e.holder == cx.Intrinsic(IntrinsicTypedArrayPrototypeLengthGetter) {
			if validateExec {
				cx.checkTypedArrayLengthHit(nil, 0, v, float64(cx.typedArrayLength(start)))
			}
			return Number(float64(cx.typedArrayLength(start))), nil
		}
	}
	s, epoch := start.shape, cx.protoEpoch
	r := cx.lookup(start, key)
	switch r.kind {
	case lookupDelegate:
		return cx.getProp(r.holder, PropKey{key: key}, v), nil
	case lookupLength:
		return r.length(v), nil
	case lookupAbsent, lookupNumericMiss:
		if c != nil {
			// Both misses are stable for this shape and prototype epoch.
			c.keep(start.shape)
			c.setInherited(start.shape, cacheProto, cx.protoEpoch)
			c.ref = &cx.missHolder.slots[0]
		}
		return Undefined, nil
	case lookupSlot:
		if c != nil && c.keep(start.shape) && start == v.o && start.kind == kindObject && start.class == classObject && start.flags&objExotic == 0 &&
			s == start.shape && s.space == cx.space && epoch == cx.protoEpoch {
			c.rememberRead(cx, start, r.holder, r.prop)
		}
		return cx.readNamedSlot(start, r.holder, r.prop, c)
	}
	panic("engine: unknown named lookup result")
}

// readNamedSlot reads p and returns its getter separately for the caller to invoke.
func (sp *space) readNamedSlot(start, holder *Object, p prop, c *propCache) (val Value, getter *Object) {
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
			c.setInherited(start.shape, cacheGetter, sp.protoEpoch)
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
			c.setInherited(start.shape, cacheProto, sp.protoEpoch)
			c.ref = &holder.slots[p.slot]
		}
	}
	return val, nil
}

// GetV returns the named property key of v, calling a getter.
func (cx *ExecutionContext) GetV(v Value, key *Object) Value {
	if v.IsObject() && v.o.shape.family == ordinaryRoot {
		return cx.getProp(v.o, PropKey{key: key}, v)
	}
	val, getter := cx.getNamed(v, key, nil)
	if getter != nil {
		return cx.Call(getter, v)
	}
	return val
}

// PropertyCache remembers where [ExecutionContext.GetVCached] found a property, so that looking up the same key
// again on values with the same layout is fast. The zero value is empty.
type PropertyCache struct {
	realm *realm
	key   PropKey
	c     propCache
}

// GetVCached implements GetV(v, k), using and updating c.
func (cx *ExecutionContext) GetVCached(v Value, k PropKey, c *PropertyCache) Value {
	if k.key == nil {
		return cx.Index(v, k.idx)
	}
	// A primitive inherits from the intrinsics of the realm performing the read.
	if c.realm != cx.realm || c.key != k {
		*c = PropertyCache{
			realm: cx.realm,
			key:   k,
		}
	}
	if o := v.o; o != nil {
		e := &c.c.cacheEntry
		if o.shape != e.shape {
			e = &c.c.second
		}
		if o.shape == e.shape {
			if e.kind == cacheOwn {
				if validateExec {
					cx.checkEntryRead(nil, 0, v, o, k.key, e)
				}
				return o.slots[e.slot]
			}
			if e.epoch == cx.protoEpoch {
				switch e.kind {
				case cacheProto:
					if validateExec {
						cx.checkEntryRead(nil, 0, v, o, k.key, e)
					}
					return *e.ref
				case cacheGetter:
					if validateExec {
						cx.checkGetterHit(nil, 0, v, k.key, e.holder)
					}
					return cx.Call(e.holder, v)
				}
			}
			if e.kind == cacheOwnGetter {
				if validateExec {
					cx.checkOwnGetterHit(nil, 0, v, k.key, &o.slots[e.slot])
				}
				if g := o.slots[e.slot].accessor().getter; g != nil {
					return cx.Call(g, v)
				}
				return Undefined
			}
		}
	}
	val, getter := cx.getNamedAtSite(v, k.key, &c.c)
	if getter != nil {
		return cx.Call(getter, v)
	}
	return val
}

// Index returns v[i], calling a getter.
func (cx *ExecutionContext) Index(v Value, i uint32) Value {
	val, getter := cx.getIndex(v, i)
	if getter != nil {
		return cx.Call(getter, v)
	}
	return val
}

// getIndex returns the property of v whose key is the array index i, or the getter to call.
func (cx *ExecutionContext) getIndex(v Value, i uint32) (Value, *Object) {
	start := v.o
	if v.IsObject() {
		if start.class == classTypedArray {
			return cx.typedArrayGet(start, i), nil
		}
	} else {
		if v.isShortString() && uint(i) < uint(v.StringLength()) {
			return cx.UnitString(v.UnitAt(int(i))), nil
		}
		start = cx.protoStart(v, PropKey{idx: i})
	}
	for h := start; h != nil; h = h.shape.proto {
		if h.objectProperties != nil && int64(i) < int64(len(h.elems)) {
			if e := h.elems[i]; !e.isHole() {
				return e, nil
			}
		}
		if h.indicesInElements() {
			continue
		}
		if h.delegatesPropertyAccess() {
			return cx.getProp(h, PropKey{idx: i}, v), nil
		}
		if h.class == classTypedArray {
			// An integer-indexed object answers for every index and never defers to its prototype.
			return cx.typedArrayGet(h, i), nil
		}
		if h.kind == kindString || h.class == classString {
			s := h
			if h.class == classString {
				s = h.extState().(*Object)
			}
			if uint(i) < uint(s.StringLength()) {
				return cx.UnitString(s.UnitAt(int(i))), nil
			}
		}
		if h.flags&objSparse != 0 {
			if p, ok := h.shape.lookup(cx.indexKey(i)); ok {
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
func (cx *ExecutionContext) GetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	if validateExec && k.key != nil && k.key.kind == kindString && k.key.flags&objInterned == 0 {
		panic("engine: property key that is not interned")
	}
	if o.flags&objExotic != 0 {
		switch o.class {
		case classProxy:
			return cx.proxyGetOwnProperty(o, k)
		case classModuleNamespace:
			if cx.isNamespaceExportKey(o, k) {
				return cx.namespaceGetOwnProperty(o, k)
			}
		case classTypedArray:
			if k.key != nil && isCanonicalNumericString(k.key) {
				return PropDesc{}, false
			}
		default:
			cx.resolveForKey(o, k.key)
		}
	}
	if k.key == nil {
		if o.objectProperties != nil && int64(k.idx) < int64(len(o.elems)) {
			if e := o.elems[k.idx]; !e.isHole() {
				return dataDesc(e, FlagsData), true
			}
		}
		switch o.class {
		case classTypedArray:
			if v := cx.typedArrayGet(o, k.idx); !v.IsUndefined() {
				return dataDesc(v, typedArrayFlags(o)), true
			}
			return PropDesc{}, false
		case classString:
			s := o.extState().(*Object)
			if uint(k.idx) < uint(s.StringLength()) {
				return dataDesc(cx.UnitString(s.UnitAt(int(k.idx))), FlagEnumerable), true
			}
		}
		if o.flags&objSparse == 0 {
			return PropDesc{}, false
		}
		if u := mappedParam(o, k.idx); u != nil {
			p, _ := o.shape.lookup(cx.indexName(k.idx))
			return dataDesc(*u.p, p.flags), true
		}
		k.key = cx.indexKey(k.idx)
		if k.key == nil {
			return PropDesc{}, false
		}
	} else {
		if k.key == cx.Key(KeyLength) && o.kind == kindArray {
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
func (cx *ExecutionContext) HasOwnProperty(o *Object, k PropKey) bool {
	if k.key == nil {
		if o.indicesInElements() {
			return int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole()
		}
	} else if o.shape.family == ordinaryRoot {
		_, ok := o.shape.lookup(k.key)
		return ok
	}
	_, ok := cx.GetOwnProperty(o, k)
	return ok
}

// HasOwnPropertyPlain is HasOwnProperty of o and the key k when finding out neither throws nor
// runs code, and takes a short time. It reports false as ok otherwise.
func (q QuietContext) HasOwnPropertyPlain(o *Object, k Value) (has, ok bool) {
	cx := q.cx
	var key QueryKey
	switch {
	case k.IsNumber():
		i, ok := numberIndex(k.n)
		if !ok {
			return false, false
		}
		key.idx = i
	case k.IsString():
		if k.isShortString() && k.o.flags&objInterned == 0 {
			key = cx.shortQueryKey(k)
		} else {
			key = cx.queryStringKey(k.o)
		}
	case k.o != nil && k.o.kind == kindSymbol:
		key.key = k.o
	default:
		return false, false
	}
	if key.key == nil {
		if o.indicesInElements() {
			return int64(key.idx) < int64(len(o.elems)) && !o.elems[key.idx].isHole(), true
		}
		if o.delegatesPropertyAccess() {
			return false, false
		}
		if o.objectProperties != nil && int64(key.idx) < int64(len(o.elems)) && !o.elems[key.idx].isHole() {
			return true, true
		}
		switch o.class {
		case classTypedArray:
			return cx.typedArrayHas(o, key.idx), true
		case classString:
			s := o.extState().(*Object)
			if uint(key.idx) < uint(s.StringLength()) {
				return true, true
			}
		}
		if o.flags&objSparse == 0 {
			return false, true
		}
		if key.key = cx.indexKey(key.idx); key.key == nil {
			return false, true
		}
		_, has = o.shape.lookup(key.key)
		return has, true
	}
	switch o.shape.family {
	case ordinaryRoot:
	case arrayRoot:
		if key.key == cx.Key(KeyLength) {
			return true, true
		}
	case lazyFunctionRoot:
		if cx.isLazyKey(key.key) {
			cx.materialize(o)
		}
	default:
		return false, false
	}
	if key.missing() {
		return false, true
	}
	_, has = o.shape.lookup(key.key)
	return has, true
}

// HasProperty implements [[HasProperty]].
func (cx *ExecutionContext) HasProperty(o *Object, k PropKey) bool {
	for h := o; h != nil; h = h.shape.proto {
		if h.flags&objExotic != 0 {
			switch h.class {
			case classProxy:
				return cx.proxyHas(h, k)
			case classModuleNamespace:
				if cx.isNamespaceExportKey(h, k) {
					return cx.namespaceHas(h, k)
				}
			case classTypedArray:
				if isNumericKey(k) {
					return k.key == nil && cx.typedArrayHas(h, k.idx)
				}
			}
		}
		if cx.HasOwnProperty(h, k) {
			return true
		}
	}
	return false
}

// ProtoOf implements [[GetPrototypeOf]].
func (cx *ExecutionContext) ProtoOf(o *Object) *Object {
	if o.class == classProxy {
		return cx.proxyGetPrototypeOf(o)
	}
	return o.shape.proto
}

// getProp implements [[Get]] with a receiver.
func (cx *ExecutionContext) getProp(o *Object, k PropKey, receiver Value) Value {
	for h := o; h != nil; h = h.shape.proto {
		if k.key != nil && h.shape.family == ordinaryRoot {
			if p, ok := h.shape.lookup(k.key); ok {
				v, getter := cx.readNamedSlot(o, h, p, nil)
				if getter != nil {
					return cx.Call(getter, receiver)
				}
				return v
			}
			continue
		}
		if h.class == classProxy {
			return cx.proxyGet(h, k, receiver)
		}
		if h.class == classModuleNamespace && cx.isNamespaceExportKey(h, k) {
			return cx.namespaceGet(h, k)
		}
		if h.class == classTypedArray && isNumericKey(k) {
			if k.key != nil {
				return Undefined
			}
			return cx.typedArrayGet(h, k.idx)
		}
		d, ok := cx.GetOwnProperty(h, k)
		if !ok {
			continue
		}
		if d.flags&FlagAccessor != 0 {
			if d.getter == nil {
				return Undefined
			}
			return cx.Call(d.getter, receiver)
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

// denseFactor is the largest number of slots per element for which [ExecutionContext.densify] moves the array
// index properties of an object into elems.
const denseFactor = 4

// addIndex creates the data property with the array index i on o, which has no such own property.
// It reports false when o does not allow it.
func (cx *ExecutionContext) addIndex(o *Object, i uint32, val Value, flags uint8) bool {
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
			cx.touchProto(o)
			return true
		}
		if growElems(o.elems, i) {
			o.reserveElems(int(i) + 1 - n)
			for uint32(len(o.elems)) < i {
				o.pushElem(hole)
			}
			o.pushElem(val)
			cx.touchProto(o)
			// The elements may have reached the indices in the shape. Checking only when their
			// number passes a power of two bounds the cost of the checks by that of the growth.
			if o.flags&objSparse != 0 && bits.Len(uint(n)) != bits.Len(uint(len(o.elems))) {
				cx.densify(o)
			}
			return true
		}
	}
	o.flags |= objSparse
	cx.add(o, cx.indexName(i), val, flags)
	if o.flags&objIsProto != 0 {
		cx.protoEpoch++
	}
	// Checking only when the number of properties in the shape reaches a power of two bounds
	// the cost of the checks by that of the additions. The elements would need more than i
	// slots.
	if c := int64(o.shape.count); flags == FlagsData && c&(c-1) == 0 && (int64(len(o.elems))+c)*denseFactor > int64(i) {
		cx.densify(o)
	}
	return true
}

// densify moves the array index properties of o from its shape to elems, and clears objSparse,
// when they are all writable, enumerable and configurable data properties and elems would have
// at most denseFactor slots per element.
func (cx *ExecutionContext) densify(o *Object) {
	if o.kind != kindArray && o.class != classObject || o.flags&(objExotic|objNotExtensible) != 0 {
		return
	}
	// The new shape adds the named properties to root, which keeps its own properties, if any,
	// in the first base slots.
	s := o.shape
	var root *shape
	var base int
	if s.private {
		root = cx.rootShape(s.proto, s.realm, s.family)
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

	o.reserveElems(int(end) - len(o.elems))
	for int64(len(o.elems)) < end {
		o.pushElem(hole)
	}
	o.shape = root
	slots := o.detachSlots(base, len(o.slots)-int(count))
	for _, k := range keys[base:] {
		p, _ := s.lookup(k)
		if k.kind == kindString {
			if i, ok := stringIndex(k.str); ok {
				o.elems[i] = slots[p.slot]
				continue
			}
		}
		cx.add(o, k, slots[p.slot], p.flags)
	}
	o.leaveSlots(slots)
	o.flags &^= objSparse
	if o.kind != kindArray {
		// The shape may be one that o had before, with other slots. The inline caches of
		// global variables point into the slots of the global object while it keeps its
		// shape, so o gets a new one.
		o.makePrivate()
	}
	cx.touchProto(o)
}

// setArrayLength implements ArraySetLength for a length that is already converted.
// It reports false when an element could not be deleted or the length is not writable.
func (cx *ExecutionContext) setArrayLength(o *Object, n uint32) bool {
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
			p.Step(cx)
			k := cx.indexKey(idx)
			if p, _ := o.shape.lookup(k); p.flags&FlagConfigurable == 0 {
				n = idx + 1
				ok = false
				break
			}
			cx.remove(o, k)
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
			cx.reallocElems(o)
		} else {
			clearPolled(cx, tail)
		}
	}
	o.length = n
	return ok
}

// setNamed stores val as the named property key of v. When the property has a setter, setNamed returns it
// and the caller has to call it with v as this. ok is false when the store was refused,
// which is a TypeError in strict code.
func (cx *ExecutionContext) setNamed(v Value, key *Object, val Value, c *propCache) (setter *Object, ok bool) {
	o := v.o
	start := o
	if !v.IsObject() {
		if v.IsNullish() {
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(PropKey{key: key})) + "')")
		}
		start = cx.protoStart(v, PropKey{key: key})
		c = nil
	} else if o.shape.family == lazyFunctionRoot {
		// The initial properties of a function precede every property that a store adds.
		cx.materialize(o)
	}
	r := cx.lookup(start, key)
	switch r.kind {
	case lookupDelegate, lookupNumericMiss:
		return nil, cx.setProp(r.holder, PropKey{key: key}, val, v)
	case lookupLength:
		if r.holder.kind == kindString {
			return nil, false
		}
		if r.holder == o {
			return nil, cx.setArrayLengthValue(o, val)
		}
		return nil, cx.setProp(r.holder, PropKey{key: key}, val, v)
	case lookupSlot, lookupAbsent:
		if c != nil {
			c.keep(o.shape)
		}
	default:
		panic("engine: unknown named lookup result")
	}
	if r.kind == lookupSlot {
		p := r.prop
		if p.flags&FlagAccessor != 0 {
			setter := r.holder.slots[p.slot].accessor().setter
			if setter != nil && c != nil && r.holder != o && !o.shape.private {
				c.setInherited(o.shape, cacheSetter, cx.protoEpoch)
				c.holder = setter
			}
			return setter, setter != nil
		}
		if p.flags&FlagWritable == 0 {
			return nil, false
		}
		if r.holder == o {
			o.slots[p.slot] = val
			if c != nil {
				c.shape = o.shape
				c.kind = cacheOwn
				c.slot = p.slot
			}
			return nil, true
		}
	}
	if !v.IsObject() || o.flags&objNotExtensible != 0 {
		return nil, false
	}
	old := o.shape
	cx.add(o, key, val, FlagsData)
	if c != nil && !old.private && !o.shape.private && o.flags&objIsProto == 0 {
		c.shape = old
		c.kind = cacheAdd
		c.next = o.shape
		c.epoch = cx.protoEpoch
	}
	return nil, true
}

// setArrayLengthValue implements the store of an arbitrary value to the length of an array.
func (cx *ExecutionContext) setArrayLengthValue(o *Object, val Value) bool {
	if o.flags&objFrozenLength != 0 {
		return false
	}
	return cx.setArrayLength(o, cx.toArrayLength(val))
}

// toArrayLength converts val to a new array length as ArraySetLength does, throwing a RangeError when
// val is not a valid length.
func (cx *ExecutionContext) toArrayLength(val Value) uint32 {
	// ArraySetLength converts the value by ToUint32 and then by ToNumber. Only an object can observe the
	// second conversion.
	n := cx.ToNumber(val)
	u := numconv.ToUint32(n)
	if val.IsObject() {
		n = cx.ToNumber(val)
	}
	if float64(u) != n {
		cx.ThrowRangeError("invalid array length")
	}
	return u
}

// setIndex stores val as the property of v whose key is the array index i.
// The results are those of setNamed.
func (cx *ExecutionContext) setIndex(v Value, i uint32, val Value) (setter *Object, ok bool) {
	o := v.o
	if o == nil || o.kind < kindFirstObject {
		if v.IsNullish() {
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(PropKey{idx: i})) + "')")
		}
		if o != nil && o.kind == kindString && int64(i) < int64(v.StringLength()) {
			return nil, false
		}
		// Only an inherited setter or a proxy can observe the store.
		return nil, cx.setProp(cx.protoStart(v, PropKey{idx: i}), PropKey{idx: i}, val, v)
	}
	if int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = val
		return nil, true
	}
	if o.class == classTypedArray {
		return nil, cx.typedArraySet(o, i, val)
	}
	// Anything but a plain store into a plain object or array takes the general path.
	if o.indicesInElements() {
		plain := true
		for h := o.shape.proto; h != nil; h = h.shape.proto {
			if len(h.elems) != 0 || h.flags&(objSparse|objExotic) != 0 || h.class != classObject && h.class != classArray && h.class != classFunction {
				plain = false
				break
			}
		}
		if plain {
			return nil, cx.addIndex(o, i, val, FlagsData)
		}
	}
	return nil, cx.setProp(o, PropKey{idx: i}, val, v)
}

// setProp implements [[Set]] with a receiver. It reports false when the store was refused.
func (cx *ExecutionContext) setProp(o *Object, k PropKey, val Value, receiver Value) bool {
	var d PropDesc
	found := false
	for h := o; h != nil; h = h.shape.proto {
		if h.class == classProxy {
			return cx.proxySet(h, k, val, receiver)
		}
		if h.class == classModuleNamespace {
			return false
		}
		if h.class == classTypedArray && isNumericKey(k) {
			if typedArrayImmutable(h) {
				return false
			}
			if k.key != nil {
				cx.typedArraySetNamed(h, val, receiver)
				return true
			}
			if receiver.o == h {
				cx.typedArraySetElement(h.extState().(*TypedArray), k.idx, val)
				return true
			}
			if !cx.typedArrayHas(h, k.idx) {
				return true
			}
		}
		if d, found = cx.GetOwnProperty(h, k); found {
			break
		}
	}
	if found && d.flags&FlagAccessor != 0 {
		if d.setter == nil {
			return false
		}
		cx.Call(d.setter, receiver, val)
		return true
	}
	if found && d.flags&FlagWritable == 0 {
		return false
	}
	if !receiver.IsObject() {
		return false
	}
	r := receiver.o
	if o == cx.realm.global && r == cx.realm.globalThis {
		// A global binding stores on the backing object even after detachment.
		r = o
	}
	if existing, ok := cx.GetOwnProperty(r, k); ok {
		if existing.flags&FlagAccessor != 0 || existing.flags&FlagWritable == 0 {
			return false
		}
		return cx.defineOwnProperty(r, k, PropDesc{
			value: val,
			has:   HasValue,
		})
	}
	return cx.defineOwnProperty(r, k, dataDesc(val, FlagsData))
}

// Set stores val as the property k of o and throws a TypeError when the store is refused.
func (cx *ExecutionContext) Set(o *Object, k PropKey, val Value) {
	if !cx.setProp(o, k, val, Value{o: o}) {
		cx.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

// defineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (cx *ExecutionContext) defineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	if o.class == classProxy {
		return cx.proxyDefineOwnProperty(o, k, d)
	}
	if o.class == classModuleNamespace && cx.isNamespaceExportKey(o, k) {
		return cx.namespaceDefineOwnProperty(o, k, d)
	}
	if o.class == classTypedArray && isNumericKey(k) {
		return k.key == nil && cx.typedArrayDefine(o, k.idx, d)
	}
	if o.kind == kindArray && k.key == cx.Key(KeyLength) {
		return cx.defineArrayLength(o, d)
	}
	if k.key == nil {
		if u := mappedParam(o, k.idx); u != nil {
			return cx.defineMappedArgument(o, k.idx, u, d)
		}
	}
	cur, exists := cx.GetOwnProperty(o, k)
	if !exists {
		if o.flags&objNotExtensible != 0 {
			return false
		}
		if o.kind == kindArray && k.key == nil && k.idx >= o.length && o.flags&objFrozenLength != 0 {
			return false
		}
		if o.flags&objExotic != 0 && o.fn != nil {
			// The initial properties of a function precede every property that is defined later.
			cx.materialize(o)
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
			return cx.addIndex(o, k.idx, val, flags)
		}
		cx.add(o, k.key, val, flags)
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
	cx.replaceOwn(o, k, val, flags)
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
func (cx *ExecutionContext) replaceOwn(o *Object, k PropKey, val Value, flags uint8) {
	if k.key == nil {
		if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
			if flags == FlagsData {
				o.elems[k.idx] = val
				cx.touchProto(o)
				return
			}
			// The property no longer fits in elems.
			o.elems[k.idx] = hole
			o.flags |= objSparse
			cx.add(o, cx.indexName(k.idx), val, flags)
			return
		}
		k.key = cx.indexName(k.idx)
	}
	cx.Define(o, k.key, val, flags)
}

// defineArrayLength implements ArraySetLength.
func (cx *ExecutionContext) defineArrayLength(o *Object, d PropDesc) bool {
	n := o.length
	if d.has&HasValue != 0 {
		n = cx.toArrayLength(d.value)
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
		ok = cx.setArrayLength(o, n)
	}
	if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		o.flags |= objFrozenLength
		cx.touchProto(o)
	}
	return ok
}

// DeleteProp implements [[Delete]]. It reports false when the property is not configurable.
func (cx *ExecutionContext) DeleteProp(o *Object, k PropKey) bool {
	if o.flags&objExotic != 0 {
		switch o.class {
		case classProxy:
			return cx.proxyDelete(o, k)
		case classModuleNamespace:
			if cx.isNamespaceExportKey(o, k) {
				return cx.namespaceDelete(o, k)
			}
		case classTypedArray:
			if k.key != nil && isCanonicalNumericString(k.key) {
				return true
			}
		default:
			cx.resolveForKey(o, k.key)
		}
	}
	if k.key == nil {
		if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
			o.elems[k.idx] = hole
			cx.touchProto(o)
			return true
		}
		switch o.class {
		case classTypedArray:
			return !cx.typedArrayHas(o, k.idx)
		case classString:
			if uint(k.idx) < uint(o.extState().(*Object).StringLength()) {
				return false
			}
		}
		if o.flags&objSparse == 0 {
			return true
		}
		if mappedParam(o, k.idx) != nil {
			return cx.deleteMappedArgument(o, k.idx)
		}
		k.key = cx.indexKey(k.idx)
		if k.key == nil {
			return true
		}
	} else {
		if k.key == cx.Key(KeyLength) && o.kind == kindArray {
			return false
		}
	}
	p, ok := o.shape.lookup(k.key)
	if !ok {
		return true
	}
	return cx.deleteOwnSlot(o, k.key, p)
}

func (cx *ExecutionContext) deleteOwnSlot(o *Object, key *Object, p prop) bool {
	if p.flags&FlagConfigurable == 0 {
		return false
	}
	cx.remove(o, key)
	return true
}

// OwnKeys implements [[OwnPropertyKeys]]: array indices in ascending order,
// then strings and then symbols, each in the order of creation.
func (cx *ExecutionContext) OwnKeys(o *Object) []PropKey {
	if o.class == classProxy {
		return cx.proxyOwnKeys(o)
	}
	if o.class == classModuleNamespace {
		return cx.namespaceOwnKeys(o)
	}
	if o.flags&objExotic != 0 {
		cx.resolve(o)
	}
	// Every element, every named property and length.
	keys := make([]PropKey, 0, len(o.elems)+int(o.shape.count)+1)
	switch o.class {
	case classString:
		keys = cx.appendIndexKeys(keys, o.extState().(*Object).StringLength())
	case classTypedArray:
		keys = cx.appendIndexKeys(keys, cx.typedArrayLength(o))
	}
	keys = cx.appendElemKeys(keys, o.elems)
	names := o.shape.keys()
	if o.flags&objSparse != 0 {
		sorted := true
		for j, k := range names {
			cx.pollAt(j)
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
			cx.sortIndexKeys(keys)
		}
	}
	if o.kind == kindArray {
		keys = append(keys, PropKey{key: cx.Key(KeyLength)})
	}
	for j, k := range names {
		cx.pollAt(j)
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
		cx.pollAt(j)
		if k.kind == kindSymbol && k.class != classPrivateName {
			keys = append(keys, PropKey{key: k})
		}
	}
	return keys
}

// pollAt checks for an interruption at every PollInterval-th step i of a loop, other than the
// first.
func (cx *ExecutionContext) pollAt(i int) {
	if i > 0 && i%PollInterval == 0 {
		cx.Poll()
	}
}

// sortIndexKeys sorts the index keys keys in ascending order, checking for an interruption while
// it sorts many.
func (cx *ExecutionContext) sortIndexKeys(keys []PropKey) {
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
			cx.pollAt(i)
			count[k.idx>>shift&0xFFFF]++
		}
		sum := 0
		for d, c := range count {
			count[d] = sum
			sum += c
		}
		for i, k := range src {
			cx.pollAt(i)
			d := k.idx >> shift & 0xFFFF
			dst[count[d]] = k
			count[d]++
		}
		src, dst = dst, src
	}
}

// IsExtensible implements [[IsExtensible]].
func (cx *ExecutionContext) IsExtensible(o *Object) bool {
	if o.class == classProxy {
		return cx.proxyIsExtensible(o)
	}
	return o.flags&objNotExtensible == 0
}

// PreventExtensions implements [[PreventExtensions]].
func (cx *ExecutionContext) PreventExtensions(o *Object) bool {
	if o.class == classProxy {
		return cx.proxyPreventExtensions(o)
	}
	if o.class == classTypedArray && !o.extState().(*TypedArray).isTypedArrayFixedLength() {
		return false
	}
	if o.flags&objNotExtensible == 0 {
		if o.flags&objExotic != 0 {
			cx.resolve(o)
		}
		o.flags |= objNotExtensible
		// The inline caches that add properties are keyed by shared shapes.
		o.makePrivate()
	}
	return true
}

// SetPrototypeOf implements [[SetPrototypeOf]].
func (cx *ExecutionContext) SetPrototypeOf(o *Object, proto *Object) bool {
	if o.class == classProxy {
		return cx.proxySetPrototypeOf(o, proto)
	}
	if o.shape.proto == proto {
		return true
	}
	if o.flags&objNotExtensible != 0 || o.shape.proto == nil && o == o.shape.realm.intrinsics[IntrinsicObjectPrototype] {
		return false
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
	cx.setProto(o, proto)
	return true
}

// CreateDataPropertyOrThrow implements CreateDataPropertyOrThrow.
func (cx *ExecutionContext) CreateDataPropertyOrThrow(o *Object, k PropKey, val Value) {
	if !cx.CreateDataProperty(o, k, val) {
		cx.ThrowTypeError("cannot define property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

// CreateDataProperty implements CreateDataProperty. It reports false when the definition is refused.
func (cx *ExecutionContext) CreateDataProperty(o *Object, k PropKey, val Value) bool {
	// An array whose index properties are all in its elements has none at the index just
	// past them.
	if k.key == nil && o.kind == kindArray && int(k.idx) == len(o.elems) && o.flags&(objSparse|objExotic) == 0 {
		return cx.addIndex(o, k.idx, val, FlagsData)
	}
	return cx.defineOwnProperty(o, k, dataDesc(val, FlagsData))
}

// DefinePropertyOrThrow implements DefinePropertyOrThrow.
func (cx *ExecutionContext) DefinePropertyOrThrow(o *Object, k PropKey, d PropDesc) {
	if !cx.defineOwnProperty(o, k, d) {
		cx.ThrowTypeError("cannot redefine property: " + wtf8.ToValidUTF8(cx.KeyString(k)))
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
func (cx *ExecutionContext) Get(o *Object, k PropKey) Value {
	return cx.getProp(o, k, Value{o: o})
}

// GetWithReceiver implements o.[[Get]](k, receiver).
func (cx *ExecutionContext) GetWithReceiver(o *Object, k PropKey, receiver Value) Value {
	return cx.getProp(o, k, receiver)
}

// SetWithReceiver implements o.[[Set]](k, val, receiver). It reports false when the store was refused.
func (cx *ExecutionContext) SetWithReceiver(o *Object, k PropKey, val Value, receiver Value) bool {
	return cx.setProp(o, k, val, receiver)
}

// DefineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (cx *ExecutionContext) DefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	return cx.defineOwnProperty(o, k, d)
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
func (cx *ExecutionContext) appendIndexKeys(keys []PropKey, n int) []PropKey {
	for i := 0; i < n; i += PollInterval {
		if i > 0 {
			cx.Poll()
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
func (cx *ExecutionContext) appendElemKeys(keys []PropKey, elems []Value) []PropKey {
	for i := 0; i < len(elems); i += PollInterval {
		if i > 0 {
			cx.Poll()
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
func (cx *ExecutionContext) EnumerableOwnKeys(o *Object, keys []PropKey) []PropKey {
	if o.flags&objExotic != 0 && (o.fn != nil || o.class == classLazy) {
		cx.resolve(o)
	}
	if (o.class != classObject && o.class != classArray && o.class != classFunction) || o.flags&(objSparse|objExotic) != 0 {
		for i, k := range cx.OwnKeys(o) {
			cx.pollAt(i)
			if k.IsSymbol() {
				continue
			}
			if d, ok := cx.GetOwnProperty(o, k); ok && d.flags&FlagEnumerable != 0 {
				keys = append(keys, k)
			}
		}
		return keys
	}
	keys = cx.appendElemKeys(keys, o.elems)
	s := o.shape
	if s.private {
		for i := 0; i < len(s.order); i += PollInterval {
			if i > 0 {
				cx.Poll()
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
