// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

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
			return IndexKey(i)
		}
		return NamedKey(cx.Intern(numconv.NumberToString(v.n)))
	}
	if o == nil {
		return cx.KeyOfString("undefined")
	}
	switch o.kind {
	case kindString:
		if c := o.Str(); len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := o.arrayIndex(); ok {
				return IndexKey(i)
			}
		}
		return NamedKey(cx.stringKey(o))
	case kindSymbol:
		return NamedKey(o)
	}
	if o.kind >= kindFirstObject {
		return cx.ToPropertyKey(cx.ToPrimitive(v, HintString))
	}
	return cx.KeyOfString(cx.ToString(v))
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
	start := cx.protoStart(v, NamedKey(k.key))
	if _, ok := cx.Quiet().hasPropertyPlain(start, k.key); ok {
		return Undefined, nil
	}
	return cx.getNamed(v, cx.propKey(k).key, nil)
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
	return newExoticObject(cx.rootShape(proto, cx.realmPart, delegatedRoot), classLazy, init)
}

// InitLazyObject runs the init function of o, an object of [ExecutionContext.NewLazyObject], unless it has
// run already.
func (ag *agent) InitLazyObject(o *Object) {
	if o.class != classLazy {
		return
	}
	init := o.extState().(func(*ExecutionContext, *Object))
	cx := CreationContext(o)
	cx.endLazy(o)
	cx.Run(func() {
		init(cx, o)
	})
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

// getNamed returns the named property key of v. When the property is an accessor with a getter,
// getNamed returns the getter instead and the caller has to call it with v as this.
// getNamed fills c, which may be nil, when the lookup can be repeated from the cache.
func (cx *ExecutionContext) getNamed(v Value, key *Object, c *propCache) (val Value, getter *Object) {
	if o := v.o; o != nil && o.class == classProxy {
		return cx.proxyGet(o, NamedKey(key), v), nil
	}
	start := cx.protoStart(v, NamedKey(key))
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
		return cx.getProp(r.holder, NamedKey(key), v), nil
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
			c.rememberRead(cx.space, start, r.holder, r.prop)
		}
		return cx.readNamedSlot(start, r.holder, r.prop, c)
	}
	panic("engine: unknown named lookup result")
}

// GetV returns the named property key of v, calling a getter.
func (cx *ExecutionContext) GetV(v Value, key *Object) Value {
	if v.IsObject() && v.o.shape.family == ordinaryRoot {
		return cx.getProp(v.o, NamedKey(key), v)
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
		start = cx.protoStart(v, IndexKey(i))
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
			return cx.getProp(h, IndexKey(i), v), nil
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
	name := k.key
	if name == nil {
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
		name = cx.indexKey(k.idx)
		if name == nil {
			return PropDesc{}, false
		}
	} else {
		if name == cx.Key(KeyLength) && o.kind == kindArray {
			flags := FlagWritable
			if o.flags&objFrozenLength != 0 {
				flags = 0
			}
			return dataDesc(Number(float64(o.length)), flags), true
		}
	}
	p, ok := o.shape.lookup(name)
	if !ok {
		return PropDesc{}, false
	}
	val := o.slots[p.slot]
	if p.flags&FlagAccessor != 0 {
		return accessorDesc(val.accessor(), p.flags), true
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
func HasOwnPropertyPlain(q QuietContext, o *Object, k Value) (has, ok bool) {
	r := q.realm
	var key QueryKey
	switch {
	case k.IsNumber():
		i, ok := numberIndex(k.n)
		if !ok {
			return false, false
		}
		key = QueryKey(IndexKey(i))
	case k.IsString():
		if k.isShortString() && k.o.flags&objInterned == 0 {
			key = r.shortQueryKey(k)
		} else {
			key = r.queryStringKey(k.o)
		}
	case k.o != nil && k.o.kind == kindSymbol:
		key = QueryKey(NamedKey(k.o))
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
			return typedArrayHas(o, key.idx), true
		case classString:
			s := o.extState().(*Object)
			if uint(key.idx) < uint(s.StringLength()) {
				return true, true
			}
		}
		if o.flags&objSparse == 0 {
			return false, true
		}
		name := r.indexKey(key.idx)
		if name == nil {
			return false, true
		}
		_, has = o.shape.lookup(name)
		return has, true
	}
	switch o.shape.family {
	case ordinaryRoot:
	case arrayRoot:
		if key.key == r.Key(KeyLength) {
			return true, true
		}
	case lazyFunctionRoot:
		if r.isLazyKey(key.key) {
			r.materialize(o)
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
					return k.key == nil && typedArrayHas(h, k.idx)
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
		if d.IsAccessor() {
			if d.Getter() == nil {
				return Undefined
			}
			return cx.Call(d.Getter(), receiver)
		}
		return d.Value()
	}
	return Undefined
}

// denseFactor is the largest number of slots per element for which [ExecutionContext.densify] moves the array
// index properties of an object into elems.
const denseFactor = 4

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
			o.clipElems()
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
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(NamedKey(key))) + "')")
		}
		start = cx.protoStart(v, NamedKey(key))
		c = nil
	} else if o.shape.family == lazyFunctionRoot {
		// The initial properties of a function precede every property that a store adds.
		cx.materialize(o)
	}
	r := cx.lookup(start, key)
	switch r.kind {
	case lookupDelegate, lookupNumericMiss:
		return nil, cx.setProp(r.holder, NamedKey(key), val, v)
	case lookupLength:
		if r.holder.kind == kindString {
			return nil, false
		}
		if r.holder == o {
			return nil, cx.setArrayLengthValue(o, val)
		}
		return nil, cx.setProp(r.holder, NamedKey(key), val, v)
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
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(IndexKey(i))) + "')")
		}
		if o != nil && o.kind == kindString && int64(i) < int64(v.StringLength()) {
			return nil, false
		}
		// Only an inherited setter or a proxy can observe the store.
		return nil, cx.setProp(cx.protoStart(v, IndexKey(i)), IndexKey(i), val, v)
	}
	if int64(i) < int64(len(o.elems)) && !o.elems[i].isHole() {
		o.elems[i] = val
		return nil, true
	}
	if o.class == classTypedArray {
		return nil, cx.typedArraySet(o, i, val)
	}
	// Anything but a plain store into a plain object or array takes the general path.
	if o.indicesInElements() && o.inheritsNoIndex() {
		return nil, cx.addIndex(o, i, val, FlagsData)
	}
	return nil, cx.setProp(o, IndexKey(i), val, v)
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
			if !typedArrayHas(h, k.idx) {
				return true
			}
		}
		if d, found = cx.GetOwnProperty(h, k); found {
			break
		}
	}
	if found && d.IsAccessor() {
		if d.Setter() == nil {
			return false
		}
		cx.Call(d.Setter(), receiver, val)
		return true
	}
	if found && !d.Writable() {
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
		if existing.IsAccessor() || !existing.Writable() {
			return false
		}
		return cx.defineOwnProperty(r, k, valueDesc(val))
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
		var val Value
		var flags uint8
		if d.IsAccessor() {
			val, flags = d.newAccessorProperty()
		} else {
			val, flags = d.newData()
		}
		if k.key == nil {
			return cx.addIndex(o, k.idx, val, flags)
		}
		cx.add(o, k.key, val, flags)
		return true
	}

	// ValidateAndApplyPropertyDescriptor for an existing property.
	if !cur.Configurable() && !nonConfigurableCompatible(&cur, &d) {
		return false
	}

	// Merge the fields that are present into the current property.
	var val Value
	var flags uint8
	if d.keepsData(&cur) {
		val, flags = d.redefinedData(&cur)
	} else {
		val, flags = d.redefinedProperty(&cur)
	}
	cx.replaceOwn(o, k, val, flags)
	return true
}

// defineArrayLength implements ArraySetLength.
func (cx *ExecutionContext) defineArrayLength(o *Object, d PropDesc) bool {
	n := o.length
	if d.hasField(HasValue) {
		n = cx.toArrayLength(d.Value())
	}
	if d.IsAccessor() {
		return false
	}
	if d.hasTrue(HasConfigurable, FlagConfigurable) {
		return false
	}
	if d.hasTrue(HasEnumerable, FlagEnumerable) {
		return false
	}
	frozen := o.flags&objFrozenLength != 0
	if frozen && d.hasTrue(HasWritable, FlagWritable) {
		return false
	}
	ok := true
	if n != o.length {
		if frozen {
			return false
		}
		ok = cx.setArrayLength(o, n)
	}
	if d.hasFalse(HasWritable, FlagWritable) {
		o.freezeLength()
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
			return true
		}
		switch o.class {
		case classTypedArray:
			return !typedArrayHas(o, k.idx)
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
		name := cx.indexKey(k.idx)
		if name == nil {
			return true
		}
		p, ok := o.shape.lookup(name)
		return !ok || cx.deleteOwnSlot(o, name, p)
	}
	if k.key == cx.Key(KeyLength) && o.kind == kindArray {
		return false
	}
	p, ok := o.shape.lookup(k.key)
	return !ok || cx.deleteOwnSlot(o, k.key, p)
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
				keys = append(keys, IndexKey(i))
			}
		}
		if !sorted {
			cx.sortIndexKeys(keys)
		}
	}
	if o.kind == kindArray {
		keys = append(keys, NamedKey(cx.Key(KeyLength)))
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
		keys = append(keys, NamedKey(k))
	}
	for j, k := range names {
		cx.pollAt(j)
		if k.kind == kindSymbol && k.class != classPrivateName {
			keys = append(keys, NamedKey(k))
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
		o.makeNonExtensible()
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
		keys = append(keys, IndexKey(uint32(i)))
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
			keys = append(keys, IndexKey(uint32(base+i)))
		}
	}
	return keys
}

// appendEnumerableNames appends the enumerable string keys among names, which are keys of the
// private shape s, to keys.
func appendEnumerableNames(keys []PropKey, s *shape, names []*Object) []PropKey {
	for _, k := range names {
		if k != nil && k.kind == kindString && s.table[k].flags&FlagEnumerable != 0 {
			keys = append(keys, NamedKey(k))
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
			if d, ok := cx.GetOwnProperty(o, k); ok && d.Enumerable() {
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
