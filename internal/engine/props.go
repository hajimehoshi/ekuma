// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// KeyValue returns the key k as a string or symbol value.
func (cx *ExecutionContext) KeyValue(k storage.PropKey) storage.Value {
	if k.Key == nil {
		return cx.indexString(k.Idx)
	}
	return storage.ObjectValue(k.Key)
}

// KeyString describes the key k for messages.
func (ag *agent) KeyString(k storage.PropKey) wtf8.String {
	if k.Key == nil {
		return wtf8.String(strconv.FormatUint(uint64(k.Idx), 10))
	}
	if k.Key.Kind == storage.KindSymbol {
		return "Symbol(" + k.Key.Text + ")"
	}
	return k.Key.Text
}

// ToPropertyKey implements ToPropertyKey.
func (cx *ExecutionContext) ToPropertyKey(v storage.Value) storage.PropKey {
	if v.IsShortString() && v.O.Flags&storage.ObjInterned == 0 {
		return cx.KeyOfString(v.ShortText())
	}
	o := v.O
	if o == storage.NumberObject {
		if i, ok := storage.NumberIndex(v.N); ok {
			return storage.IndexKey(i)
		}
		return storage.NamedKey(cx.Intern(numconv.NumberToString(v.N)))
	}
	if o == nil {
		return cx.KeyOfString("undefined")
	}
	switch o.Kind {
	case storage.KindString:
		if c := o.Str(); len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
			if i, ok := o.ArrayIndex(); ok {
				return storage.IndexKey(i)
			}
		}
		return storage.NamedKey(cx.stringKey(o))
	case storage.KindSymbol:
		return storage.NamedKey(o)
	}
	if o.Kind >= storage.KindFirstObject {
		return cx.ToPropertyKey(cx.ToPrimitive(v, HintString))
	}
	return cx.KeyOfString(cx.ToString(v))
}

// QueryKey returns ToPropertyKey(v) as a query key, which is valid until code runs or a property
// key is made.
func (cx *ExecutionContext) QueryKey(v storage.Value) storage.QueryKey {
	if o := v.O; o != nil && o.Kind == storage.KindString {
		if v.IsShortString() && o.Flags&storage.ObjInterned == 0 {
			return cx.ShortQueryKey(v)
		}
		return cx.QueryStringKey(o)
	}
	return storage.QueryKey(cx.ToPropertyKey(v))
}

// QueryProperty is HasProperty of o and k.
func (cx *ExecutionContext) QueryProperty(o *storage.Object, k storage.QueryKey) bool {
	if k.Missing() {
		return cx.queryMissing(o, k)
	}
	return cx.HasProperty(o, storage.PropKey(k))
}

// queryMissing is QueryProperty of o and k, which is missing.
func (cx *ExecutionContext) queryMissing(o *storage.Object, k storage.QueryKey) bool {
	if _, ok := cx.Quiet().HasPropertyPlain(o, k.Key); ok {
		return false
	}
	return cx.HasProperty(o, cx.propKey(k))
}

// QueryOwnProperty is HasOwnProperty of o and k.
func (cx *ExecutionContext) QueryOwnProperty(o *storage.Object, k storage.QueryKey) bool {
	if k.Missing() {
		if !o.DelegatesPropertyAccess() {
			return false
		}
	} else if k.Key != nil && o.Shape.Family == storage.OrdinaryRoot {
		_, ok := o.Shape.Lookup(k.Key)
		return ok
	}
	return cx.HasOwnProperty(o, cx.propKey(k))
}

// getMissing is getNamed of v and k, which is missing.
func (cx *ExecutionContext) getMissing(v storage.Value, k storage.QueryKey) (storage.Value, *storage.Object) {
	start := cx.protoStart(v, storage.NamedKey(k.Key))
	if _, ok := cx.Quiet().HasPropertyPlain(start, k.Key); ok {
		return storage.Undefined, nil
	}
	return cx.getNamed(v, cx.propKey(k).Key, nil)
}

// resolve prepares the exotic object o for an operation on its named properties.
func (cx *ExecutionContext) resolve(o *storage.Object) {
	if o.Fn != nil && !o.Fn.Materialized {
		cx.Materialize(o)
		return
	}
	if o.Class == storage.ClassLazy {
		cx.InitLazyObject(o)
	}
}

// resolveForKey materializes the own properties needed for key.
func (cx *ExecutionContext) resolveForKey(o *storage.Object, key *storage.Object) {
	if o.Class != storage.ClassLazy {
		if o.Fn != nil && key != nil && cx.IsLazyKey(key) {
			cx.Materialize(o)
		}
		return
	}
	cx.InitLazyObject(o)
}

// NewLazyObject returns an ordinary object with prototype proto. The first operation that
// needs its own properties, or a call to [agent.InitLazyObject], runs init once.
func (cx *ExecutionContext) NewLazyObject(proto *storage.Object, init func(*ExecutionContext, *storage.Object)) *storage.Object {
	// A cached miss on an ordinary object must still run this object's initializer.
	return storage.NewExoticObject(cx.RootShape(proto, cx.realmPart, storage.DelegatedRoot), storage.ClassLazy, init)
}

// InitLazyObject runs the init function of o, an object of [ExecutionContext.NewLazyObject], unless it has
// run already.
func (ag *agent) InitLazyObject(o *storage.Object) {
	if o.Class != storage.ClassLazy {
		return
	}
	init := o.ExtState().(func(*ExecutionContext, *storage.Object))
	cx := CreationContext(o)
	cx.EndLazy(o)
	cx.Run(func() {
		init(cx, o)
	})
}

// protoStart returns the object at which a property lookup on the value v starts.
// It throws a TypeError for undefined and null.
func (cx *ExecutionContext) protoStart(v storage.Value, key storage.PropKey) *storage.Object {
	o := v.O
	if o == storage.NumberObject {
		return cx.Intrinsic(storage.IntrinsicNumberPrototype)
	}
	if o == nil {
		cx.ThrowTypeError("cannot read properties of undefined (reading '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "')")
	}
	switch o.Kind {
	case storage.KindFalse, storage.KindTrue:
		return cx.Intrinsic(storage.IntrinsicBooleanPrototype)
	case storage.KindSymbol:
		return cx.Intrinsic(storage.IntrinsicSymbolPrototype)
	case storage.KindBigInt:
		return cx.Intrinsic(storage.IntrinsicBigIntPrototype)
	case storage.KindNull:
		cx.ThrowTypeError("cannot read properties of " + cx.DisplayString(v) + " (reading '" + wtf8.ToValidUTF8(cx.KeyString(key)) + "')")
	case storage.KindString:
		if key.Key == cx.Key(storage.KeyLength) {
			return o
		}
		if v.IsShortString() && o.Flags&storage.ObjInterned == 0 {
			o = v.StringObject()
		}
		if p := cx.Intrinsic(storage.IntrinsicStringPrototype); o.Shape.Proto != p {
			// The shape of a string made in another realm leads to the String.prototype of
			// that realm.
			return cx.NewStringObject(o, p)
		}
	}
	// A string has the empty shape whose prototype is String.prototype.
	return o
}

// getNamedAtSite reads a named property through the additional shapes recorded at the site.
func (cx *ExecutionContext) getNamedAtSite(v storage.Value, key *storage.Object, c *storage.PropCache) (storage.Value, *storage.Object) {
	if r := c.Reads; r != nil && r.Space == cx.space && r.Epoch == cx.ProtoEpoch {
		if o := v.O; o != nil && o.Kind == storage.KindObject && o.Class == storage.ClassObject && o.Flags&storage.ObjExotic == 0 && o.Shape.Space == cx.space {
			if e, ok := r.Shapes[o.Shape.ReadID]; ok && e.Count == o.Shape.Count {
				h := o
				for range e.Depth {
					h = h.Shape.Proto
				}
				if validateExec {
					cx.checkRememberedRead(v, key, h, e.Prop)
				}
				c.Keep(o.Shape)
				return cx.ReadNamedSlot(o, h, e.Prop, c)
			}
		}
	}
	return cx.getNamed(v, key, c)
}

// getNamed returns the named property key of v. When the property is an accessor with a getter,
// getNamed returns the getter instead and the caller has to call it with v as this.
// getNamed fills c, which may be nil, when the lookup can be repeated from the cache.
func (cx *ExecutionContext) getNamed(v storage.Value, key *storage.Object, c *storage.PropCache) (val storage.Value, getter *storage.Object) {
	if o := v.O; o != nil && o.Class == storage.ClassProxy {
		return cx.proxyGet(o, storage.NamedKey(key), v), nil
	}
	start := cx.protoStart(v, storage.NamedKey(key))
	if start.Class == storage.ClassTypedArray && key == cx.Key(storage.KeyLength) && c != nil {
		// The cache says that the length is the original getter, whose result needs no call.
		e := &c.CacheEntry
		if e.Shape != start.Shape {
			e = &c.Second
		}
		if e.Shape == start.Shape && e.Kind == storage.CacheGetter && e.Epoch == cx.ProtoEpoch && e.Holder == cx.Intrinsic(storage.IntrinsicTypedArrayPrototypeLengthGetter) {
			if validateExec {
				cx.checkTypedArrayLengthHit(nil, 0, v, float64(storage.TypedArrayLength(start)))
			}
			return storage.Number(float64(storage.TypedArrayLength(start))), nil
		}
	}
	s, epoch := start.Shape, cx.ProtoEpoch
	r := cx.Lookup(start, key)
	switch r.Kind {
	case storage.LookupDelegate:
		return cx.getProp(r.Holder, storage.NamedKey(key), v), nil
	case storage.LookupLength:
		return r.Length(v), nil
	case storage.LookupAbsent, storage.LookupNumericMiss:
		if c != nil {
			// Both misses are stable for this shape and prototype epoch.
			c.Keep(start.Shape)
			c.SetInherited(start.Shape, storage.CacheProto, cx.ProtoEpoch)
			c.Ref = &cx.MissHolder.Slots[0]
		}
		return storage.Undefined, nil
	case storage.LookupSlot:
		if c != nil && c.Keep(start.Shape) && start == v.O && start.Kind == storage.KindObject && start.Class == storage.ClassObject && start.Flags&storage.ObjExotic == 0 &&
			s == start.Shape && s.Space == cx.space && epoch == cx.ProtoEpoch {
			c.RememberRead(cx.space, start, r.Holder, r.Prop)
		}
		return cx.ReadNamedSlot(start, r.Holder, r.Prop, c)
	}
	panic("engine: unknown named lookup result")
}

// GetV returns the named property key of v, calling a getter.
func (cx *ExecutionContext) GetV(v storage.Value, key *storage.Object) storage.Value {
	if v.IsObject() && v.O.Shape.Family == storage.OrdinaryRoot {
		return cx.getProp(v.O, storage.NamedKey(key), v)
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
	key   storage.PropKey
	c     storage.PropCache
}

// GetVCached implements GetV(v, k), using and updating c.
func (cx *ExecutionContext) GetVCached(v storage.Value, k storage.PropKey, c *PropertyCache) storage.Value {
	if k.Key == nil {
		return cx.Index(v, k.Idx)
	}
	// A primitive inherits from the intrinsics of the realm performing the read.
	if c.realm != cx.realm || c.key != k {
		*c = PropertyCache{
			realm: cx.realm,
			key:   k,
		}
	}
	if o := v.O; o != nil {
		e := &c.c.CacheEntry
		if o.Shape != e.Shape {
			e = &c.c.Second
		}
		if o.Shape == e.Shape {
			if e.Kind == storage.CacheOwn {
				if validateExec {
					cx.checkEntryRead(nil, 0, v, o, k.Key, e)
				}
				return o.Slots[e.Slot]
			}
			if e.Epoch == cx.ProtoEpoch {
				switch e.Kind {
				case storage.CacheProto:
					if validateExec {
						cx.checkEntryRead(nil, 0, v, o, k.Key, e)
					}
					return *e.Ref
				case storage.CacheGetter:
					if validateExec {
						cx.checkGetterHit(nil, 0, v, k.Key, e.Holder)
					}
					return cx.Call(e.Holder, v)
				}
			}
			if e.Kind == storage.CacheOwnGetter {
				if validateExec {
					cx.checkOwnGetterHit(nil, 0, v, k.Key, &o.Slots[e.Slot])
				}
				if g := o.Slots[e.Slot].Accessor().Getter; g != nil {
					return cx.Call(g, v)
				}
				return storage.Undefined
			}
		}
	}
	val, getter := cx.getNamedAtSite(v, k.Key, &c.c)
	if getter != nil {
		return cx.Call(getter, v)
	}
	return val
}

// Index returns v[i], calling a getter.
func (cx *ExecutionContext) Index(v storage.Value, i uint32) storage.Value {
	val, getter := cx.getIndex(v, i)
	if getter != nil {
		return cx.Call(getter, v)
	}
	return val
}

// getIndex returns the property of v whose key is the array index i, or the getter to call.
func (cx *ExecutionContext) getIndex(v storage.Value, i uint32) (storage.Value, *storage.Object) {
	start := v.O
	if v.IsObject() {
		if start.Class == storage.ClassTypedArray {
			return storage.TypedArrayGet(start, i), nil
		}
	} else {
		if v.IsShortString() && uint(i) < uint(v.StringLength()) {
			return cx.UnitString(v.UnitAt(int(i))), nil
		}
		start = cx.protoStart(v, storage.IndexKey(i))
	}
	for h := start; h != nil; h = h.Shape.Proto {
		if h.ObjectProperties != nil && int64(i) < int64(len(h.Elems)) {
			if e := h.Elems[i]; !e.IsHole() {
				return e, nil
			}
		}
		if h.IndicesInElements() {
			continue
		}
		if h.DelegatesPropertyAccess() {
			return cx.getProp(h, storage.IndexKey(i), v), nil
		}
		if h.Class == storage.ClassTypedArray {
			// An integer-indexed object answers for every index and never defers to its prototype.
			return storage.TypedArrayGet(h, i), nil
		}
		if h.Kind == storage.KindString || h.Class == storage.ClassString {
			s := h
			if h.Class == storage.ClassString {
				s = h.ExtState().(*storage.Object)
			}
			if uint(i) < uint(s.StringLength()) {
				return cx.UnitString(s.UnitAt(int(i))), nil
			}
		}
		if h.Flags&storage.ObjSparse != 0 {
			if p, ok := h.Shape.Lookup(cx.IndexKey(i)); ok {
				val := h.Slots[p.Slot]
				if p.Flags&storage.FlagAccessor != 0 {
					return storage.Undefined, val.Accessor().Getter
				}
				if u := mappedParam(h, i); u != nil {
					return *u.P, nil
				}
				return val, nil
			}
		}
	}
	return storage.Undefined, nil
}

// GetOwnProperty implements [[GetOwnProperty]].
func (cx *ExecutionContext) GetOwnProperty(o *storage.Object, k storage.PropKey) (storage.PropDesc, bool) {
	if validateExec && k.Key != nil && k.Key.Kind == storage.KindString && k.Key.Flags&storage.ObjInterned == 0 {
		panic("engine: property key that is not interned")
	}
	if o.Flags&storage.ObjExotic != 0 {
		switch o.Class {
		case storage.ClassProxy:
			return cx.proxyGetOwnProperty(o, k)
		case storage.ClassModuleNamespace:
			if cx.isNamespaceExportKey(o, k) {
				return cx.namespaceGetOwnProperty(o, k)
			}
		case storage.ClassTypedArray:
			if k.Key != nil && storage.IsCanonicalNumericString(k.Key) {
				return storage.PropDesc{}, false
			}
		default:
			cx.resolveForKey(o, k.Key)
		}
	}
	name := k.Key
	if name == nil {
		if o.ObjectProperties != nil && int64(k.Idx) < int64(len(o.Elems)) {
			if e := o.Elems[k.Idx]; !e.IsHole() {
				return storage.DataDesc(e, storage.FlagsData), true
			}
		}
		switch o.Class {
		case storage.ClassTypedArray:
			if v := storage.TypedArrayGet(o, k.Idx); !v.IsUndefined() {
				return storage.DataDesc(v, storage.TypedArrayFlags(o)), true
			}
			return storage.PropDesc{}, false
		case storage.ClassString:
			s := o.ExtState().(*storage.Object)
			if uint(k.Idx) < uint(s.StringLength()) {
				return storage.DataDesc(cx.UnitString(s.UnitAt(int(k.Idx))), storage.FlagEnumerable), true
			}
		}
		if o.Flags&storage.ObjSparse == 0 {
			return storage.PropDesc{}, false
		}
		if u := mappedParam(o, k.Idx); u != nil {
			p, _ := o.Shape.Lookup(cx.IndexName(k.Idx))
			return storage.DataDesc(*u.P, p.Flags), true
		}
		name = cx.IndexKey(k.Idx)
		if name == nil {
			return storage.PropDesc{}, false
		}
	} else {
		if name == cx.Key(storage.KeyLength) && o.Kind == storage.KindArray {
			flags := storage.FlagWritable
			if o.Flags&storage.ObjFrozenLength != 0 {
				flags = 0
			}
			return storage.DataDesc(storage.Number(float64(o.Length)), flags), true
		}
	}
	p, ok := o.Shape.Lookup(name)
	if !ok {
		return storage.PropDesc{}, false
	}
	val := o.Slots[p.Slot]
	if p.Flags&storage.FlagAccessor != 0 {
		return storage.AccessorDesc(val.Accessor(), p.Flags), true
	}
	return storage.DataDesc(val, p.Flags), true
}

// HasOwnProperty reports whether o has the own property k.
func (cx *ExecutionContext) HasOwnProperty(o *storage.Object, k storage.PropKey) bool {
	if k.Key == nil {
		if o.IndicesInElements() {
			return int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole()
		}
	} else if o.Shape.Family == storage.OrdinaryRoot {
		_, ok := o.Shape.Lookup(k.Key)
		return ok
	}
	_, ok := cx.GetOwnProperty(o, k)
	return ok
}

// HasProperty implements [[HasProperty]].
func (cx *ExecutionContext) HasProperty(o *storage.Object, k storage.PropKey) bool {
	for h := o; h != nil; h = h.Shape.Proto {
		if h.Flags&storage.ObjExotic != 0 {
			switch h.Class {
			case storage.ClassProxy:
				return cx.proxyHas(h, k)
			case storage.ClassModuleNamespace:
				if cx.isNamespaceExportKey(h, k) {
					return cx.namespaceHas(h, k)
				}
			case storage.ClassTypedArray:
				if isNumericKey(k) {
					return k.Key == nil && storage.TypedArrayHas(h, k.Idx)
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
func (cx *ExecutionContext) ProtoOf(o *storage.Object) *storage.Object {
	if o.Class == storage.ClassProxy {
		return cx.proxyGetPrototypeOf(o)
	}
	return o.Shape.Proto
}

// getProp implements [[Get]] with a receiver.
func (cx *ExecutionContext) getProp(o *storage.Object, k storage.PropKey, receiver storage.Value) storage.Value {
	for h := o; h != nil; h = h.Shape.Proto {
		if k.Key != nil && h.Shape.Family == storage.OrdinaryRoot {
			if p, ok := h.Shape.Lookup(k.Key); ok {
				v, getter := cx.ReadNamedSlot(o, h, p, nil)
				if getter != nil {
					return cx.Call(getter, receiver)
				}
				return v
			}
			continue
		}
		if h.Class == storage.ClassProxy {
			return cx.proxyGet(h, k, receiver)
		}
		if h.Class == storage.ClassModuleNamespace && cx.isNamespaceExportKey(h, k) {
			return cx.namespaceGet(h, k)
		}
		if h.Class == storage.ClassTypedArray && isNumericKey(k) {
			if k.Key != nil {
				return storage.Undefined
			}
			return storage.TypedArrayGet(h, k.Idx)
		}
		d, ok := cx.GetOwnProperty(h, k)
		if !ok {
			continue
		}
		if d.IsAccessor() {
			if d.Getter() == nil {
				return storage.Undefined
			}
			return cx.Call(d.Getter(), receiver)
		}
		return d.Value()
	}
	return storage.Undefined
}

// setArrayLength implements ArraySetLength for a length that is already converted.
// It reports false when an element could not be deleted or the length is not writable.
func (cx *ExecutionContext) setArrayLength(o *storage.Object, n uint32) bool {
	if n == o.Length {
		return true
	}
	if o.Flags&storage.ObjFrozenLength != 0 {
		return false
	}
	if n > o.Length {
		o.Length = n
		return true
	}
	ok := true
	if o.Flags&storage.ObjSparse != 0 {
		// Delete the stored index properties from the end, stopping at one that is not configurable.
		var idxs []uint32
		for _, k := range o.Shape.Keys() {
			if k.Kind != storage.KindString {
				continue
			}
			if i, isIdx := storage.StringIndex(k.Text); isIdx && i >= n {
				idxs = append(idxs, i)
			}
		}
		slices.Sort(idxs)
		var p Pacer
		for _, idx := range slices.Backward(idxs) {
			p.Step(cx)
			k := cx.IndexKey(idx)
			if p, _ := o.Shape.Lookup(k); p.Flags&storage.FlagConfigurable == 0 {
				n = idx + 1
				ok = false
				break
			}
			cx.remove(o, k)
		}
	}
	if int64(n) < int64(len(o.Elems)) {
		tail := o.Elems[n:]
		o.Elems = o.Elems[:n]
		oversized := o.ElemsOversized()
		if len(tail) > storage.PollChunk {
			// The tail, which may be left uncleared, stays out of reach of later growth.
			o.ClipElems()
		}
		o.Length = n
		if oversized {
			// The tail goes away with the storage.
			cx.ReallocElems(o)
		} else {
			clearPolled(cx, tail)
		}
	}
	o.Length = n
	return ok
}

// setNamed stores val as the named property key of v. When the property has a setter, setNamed returns it
// and the caller has to call it with v as this. ok is false when the store was refused,
// which is a TypeError in strict code.
func (cx *ExecutionContext) setNamed(v storage.Value, key *storage.Object, val storage.Value, c *storage.PropCache) (setter *storage.Object, ok bool) {
	o := v.O
	start := o
	if !v.IsObject() {
		if v.IsNullish() {
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(storage.NamedKey(key))) + "')")
		}
		start = cx.protoStart(v, storage.NamedKey(key))
		c = nil
	} else if o.Shape.Family == storage.LazyFunctionRoot {
		// The initial properties of a function precede every property that a store adds.
		cx.Materialize(o)
	}
	r := cx.Lookup(start, key)
	switch r.Kind {
	case storage.LookupDelegate, storage.LookupNumericMiss:
		return nil, cx.setProp(r.Holder, storage.NamedKey(key), val, v)
	case storage.LookupLength:
		if r.Holder.Kind == storage.KindString {
			return nil, false
		}
		if r.Holder == o {
			return nil, cx.setArrayLengthValue(o, val)
		}
		return nil, cx.setProp(r.Holder, storage.NamedKey(key), val, v)
	case storage.LookupSlot, storage.LookupAbsent:
		if c != nil {
			c.Keep(o.Shape)
		}
	default:
		panic("engine: unknown named lookup result")
	}
	if r.Kind == storage.LookupSlot {
		p := r.Prop
		if p.Flags&storage.FlagAccessor != 0 {
			setter := r.Holder.Slots[p.Slot].Accessor().Setter
			if setter != nil && c != nil && r.Holder != o && !o.Shape.Private {
				c.SetInherited(o.Shape, storage.CacheSetter, cx.ProtoEpoch)
				c.Holder = setter
			}
			return setter, setter != nil
		}
		if p.Flags&storage.FlagWritable == 0 {
			return nil, false
		}
		if r.Holder == o {
			o.Slots[p.Slot] = val
			if c != nil {
				c.Shape = o.Shape
				c.Kind = storage.CacheOwn
				c.Slot = p.Slot
			}
			return nil, true
		}
	}
	if !v.IsObject() || o.Flags&storage.ObjNotExtensible != 0 {
		return nil, false
	}
	old := o.Shape
	cx.Add(o, key, val, storage.FlagsData)
	if c != nil && !old.Private && !o.Shape.Private && o.Flags&storage.ObjIsProto == 0 {
		c.Shape = old
		c.Kind = storage.CacheAdd
		c.Next = o.Shape
		c.Epoch = cx.ProtoEpoch
	}
	return nil, true
}

// setArrayLengthValue implements the store of an arbitrary value to the length of an array.
func (cx *ExecutionContext) setArrayLengthValue(o *storage.Object, val storage.Value) bool {
	if o.Flags&storage.ObjFrozenLength != 0 {
		return false
	}
	return cx.setArrayLength(o, cx.toArrayLength(val))
}

// toArrayLength converts val to a new array length as ArraySetLength does, throwing a RangeError when
// val is not a valid length.
func (cx *ExecutionContext) toArrayLength(val storage.Value) uint32 {
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
func (cx *ExecutionContext) setIndex(v storage.Value, i uint32, val storage.Value) (setter *storage.Object, ok bool) {
	o := v.O
	if o == nil || o.Kind < storage.KindFirstObject {
		if v.IsNullish() {
			cx.ThrowTypeError("cannot set properties of " + cx.DisplayString(v) + " (setting '" + wtf8.ToValidUTF8(cx.KeyString(storage.IndexKey(i))) + "')")
		}
		if o != nil && o.Kind == storage.KindString && int64(i) < int64(v.StringLength()) {
			return nil, false
		}
		// Only an inherited setter or a proxy can observe the store.
		return nil, cx.setProp(cx.protoStart(v, storage.IndexKey(i)), storage.IndexKey(i), val, v)
	}
	if int64(i) < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		o.Elems[i] = val
		return nil, true
	}
	if o.Class == storage.ClassTypedArray {
		return nil, cx.typedArraySet(o, i, val)
	}
	// Anything but a plain store into a plain object or array takes the general path.
	if o.IndicesInElements() && o.InheritsNoIndex() {
		return nil, cx.AddIndex(o, i, val, storage.FlagsData)
	}
	return nil, cx.setProp(o, storage.IndexKey(i), val, v)
}

// setProp implements [[Set]] with a receiver. It reports false when the store was refused.
func (cx *ExecutionContext) setProp(o *storage.Object, k storage.PropKey, val storage.Value, receiver storage.Value) bool {
	var d storage.PropDesc
	found := false
	for h := o; h != nil; h = h.Shape.Proto {
		if h.Class == storage.ClassProxy {
			return cx.proxySet(h, k, val, receiver)
		}
		if h.Class == storage.ClassModuleNamespace {
			return false
		}
		if h.Class == storage.ClassTypedArray && isNumericKey(k) {
			if storage.TypedArrayImmutable(h) {
				return false
			}
			if k.Key != nil {
				cx.typedArraySetNamed(h, val, receiver)
				return true
			}
			if receiver.O == h {
				cx.typedArraySetElement(h.ExtState().(*storage.TypedArray), k.Idx, val)
				return true
			}
			if !storage.TypedArrayHas(h, k.Idx) {
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
	r := receiver.O
	if o == cx.realm.Global && r == cx.realm.GlobalThis {
		// A global binding stores on the backing object even after detachment.
		r = o
	}
	if existing, ok := cx.GetOwnProperty(r, k); ok {
		if existing.IsAccessor() || !existing.Writable() {
			return false
		}
		return cx.defineOwnProperty(r, k, storage.ValueDesc(val))
	}
	return cx.defineOwnProperty(r, k, storage.DataDesc(val, storage.FlagsData))
}

// Set stores val as the property k of o and throws a TypeError when the store is refused.
func (cx *ExecutionContext) Set(o *storage.Object, k storage.PropKey, val storage.Value) {
	if !cx.setProp(o, k, val, storage.Value{O: o}) {
		cx.ThrowTypeError("cannot assign to read only property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

// defineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (cx *ExecutionContext) defineOwnProperty(o *storage.Object, k storage.PropKey, d storage.PropDesc) bool {
	if o.Class == storage.ClassProxy {
		return cx.proxyDefineOwnProperty(o, k, d)
	}
	if o.Class == storage.ClassModuleNamespace && cx.isNamespaceExportKey(o, k) {
		return cx.namespaceDefineOwnProperty(o, k, d)
	}
	if o.Class == storage.ClassTypedArray && isNumericKey(k) {
		return k.Key == nil && cx.typedArrayDefine(o, k.Idx, d)
	}
	if o.Kind == storage.KindArray && k.Key == cx.Key(storage.KeyLength) {
		return cx.defineArrayLength(o, d)
	}
	if k.Key == nil {
		if u := mappedParam(o, k.Idx); u != nil {
			return cx.defineMappedArgument(o, k.Idx, u, d)
		}
	}
	cur, exists := cx.GetOwnProperty(o, k)
	if !exists {
		if o.Flags&storage.ObjNotExtensible != 0 {
			return false
		}
		if o.Kind == storage.KindArray && k.Key == nil && k.Idx >= o.Length && o.Flags&storage.ObjFrozenLength != 0 {
			return false
		}
		if o.Flags&storage.ObjExotic != 0 && o.Fn != nil {
			// The initial properties of a function precede every property that is defined later.
			cx.Materialize(o)
		}
		var val storage.Value
		var flags uint8
		if d.IsAccessor() {
			val, flags = d.NewAccessorProperty()
		} else {
			val, flags = d.NewData()
		}
		if k.Key == nil {
			return cx.AddIndex(o, k.Idx, val, flags)
		}
		cx.Add(o, k.Key, val, flags)
		return true
	}

	// ValidateAndApplyPropertyDescriptor for an existing property.
	if !cur.Configurable() && !storage.NonConfigurableCompatible(&cur, &d) {
		return false
	}

	// Merge the fields that are present into the current property.
	var val storage.Value
	var flags uint8
	if d.KeepsData(&cur) {
		val, flags = d.RedefinedData(&cur)
	} else {
		val, flags = d.RedefinedProperty(&cur)
	}
	cx.ReplaceOwn(o, k, val, flags)
	return true
}

// defineArrayLength implements ArraySetLength.
func (cx *ExecutionContext) defineArrayLength(o *storage.Object, d storage.PropDesc) bool {
	n := o.Length
	if d.HasField(storage.HasValue) {
		n = cx.toArrayLength(d.Value())
	}
	if d.IsAccessor() {
		return false
	}
	if d.HasTrue(storage.HasConfigurable, storage.FlagConfigurable) {
		return false
	}
	if d.HasTrue(storage.HasEnumerable, storage.FlagEnumerable) {
		return false
	}
	frozen := o.Flags&storage.ObjFrozenLength != 0
	if frozen && d.HasTrue(storage.HasWritable, storage.FlagWritable) {
		return false
	}
	ok := true
	if n != o.Length {
		if frozen {
			return false
		}
		ok = cx.setArrayLength(o, n)
	}
	if d.HasFalse(storage.HasWritable, storage.FlagWritable) {
		o.FreezeLength()
	}
	return ok
}

// DeleteProp implements [[Delete]]. It reports false when the property is not configurable.
func (cx *ExecutionContext) DeleteProp(o *storage.Object, k storage.PropKey) bool {
	if o.Flags&storage.ObjExotic != 0 {
		switch o.Class {
		case storage.ClassProxy:
			return cx.proxyDelete(o, k)
		case storage.ClassModuleNamespace:
			if cx.isNamespaceExportKey(o, k) {
				return cx.namespaceDelete(o, k)
			}
		case storage.ClassTypedArray:
			if k.Key != nil && storage.IsCanonicalNumericString(k.Key) {
				return true
			}
		default:
			cx.resolveForKey(o, k.Key)
		}
	}
	if k.Key == nil {
		if int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole() {
			o.Elems[k.Idx] = storage.Hole
			return true
		}
		switch o.Class {
		case storage.ClassTypedArray:
			return !storage.TypedArrayHas(o, k.Idx)
		case storage.ClassString:
			if uint(k.Idx) < uint(o.ExtState().(*storage.Object).StringLength()) {
				return false
			}
		}
		if o.Flags&storage.ObjSparse == 0 {
			return true
		}
		if mappedParam(o, k.Idx) != nil {
			return cx.deleteMappedArgument(o, k.Idx)
		}
		name := cx.IndexKey(k.Idx)
		if name == nil {
			return true
		}
		p, ok := o.Shape.Lookup(name)
		return !ok || cx.deleteOwnSlot(o, name, p)
	}
	if k.Key == cx.Key(storage.KeyLength) && o.Kind == storage.KindArray {
		return false
	}
	p, ok := o.Shape.Lookup(k.Key)
	return !ok || cx.deleteOwnSlot(o, k.Key, p)
}

func (cx *ExecutionContext) deleteOwnSlot(o *storage.Object, key *storage.Object, p storage.Prop) bool {
	if p.Flags&storage.FlagConfigurable == 0 {
		return false
	}
	cx.remove(o, key)
	return true
}

// OwnKeys implements [[OwnPropertyKeys]]: array indices in ascending order,
// then strings and then symbols, each in the order of creation.
func (cx *ExecutionContext) OwnKeys(o *storage.Object) []storage.PropKey {
	if o.Class == storage.ClassProxy {
		return cx.proxyOwnKeys(o)
	}
	if o.Class == storage.ClassModuleNamespace {
		return cx.namespaceOwnKeys(o)
	}
	if o.Flags&storage.ObjExotic != 0 {
		cx.resolve(o)
	}
	// Every element, every named property and length.
	keys := make([]storage.PropKey, 0, len(o.Elems)+int(o.Shape.Count)+1)
	switch o.Class {
	case storage.ClassString:
		keys = cx.appendIndexKeys(keys, o.ExtState().(*storage.Object).StringLength())
	case storage.ClassTypedArray:
		keys = cx.appendIndexKeys(keys, storage.TypedArrayLength(o))
	}
	keys = cx.appendElemKeys(keys, o.Elems)
	names := o.Shape.Keys()
	if o.Flags&storage.ObjSparse != 0 {
		sorted := true
		for j, k := range names {
			cx.pollAt(j)
			if k.Kind != storage.KindString {
				continue
			}
			if i, ok := k.ArrayIndex(); ok {
				if n := len(keys); n > 0 && keys[n-1].Idx > i {
					sorted = false
				}
				keys = append(keys, storage.IndexKey(i))
			}
		}
		if !sorted {
			cx.sortIndexKeys(keys)
		}
	}
	if o.Kind == storage.KindArray {
		keys = append(keys, storage.NamedKey(cx.Key(storage.KeyLength)))
	}
	for j, k := range names {
		cx.pollAt(j)
		if k.Kind != storage.KindString {
			continue
		}
		if o.Flags&storage.ObjSparse != 0 {
			if _, ok := k.KnownArrayIndex(); ok {
				continue
			}
		}
		keys = append(keys, storage.NamedKey(k))
	}
	for j, k := range names {
		cx.pollAt(j)
		if k.Kind == storage.KindSymbol && k.Class != storage.ClassPrivateName {
			keys = append(keys, storage.NamedKey(k))
		}
	}
	return keys
}

// pollAt checks for an interruption at every PollInterval-th step i of a loop, other than the
// first.
func (cx *ExecutionContext) pollAt(i int) {
	if i > 0 && i%storage.PollInterval == 0 {
		cx.Poll()
	}
}

// sortIndexKeys sorts the index keys keys in ascending order, checking for an interruption while
// it sorts many.
func (cx *ExecutionContext) sortIndexKeys(keys []storage.PropKey) {
	if len(keys) <= storage.PollInterval {
		slices.SortFunc(keys, func(a, b storage.PropKey) int {
			return cmp.Compare(a.Idx, b.Idx)
		})
		return
	}
	// A radix sort by 16 bits at a time, which ends with the keys back in keys.
	src, dst := keys, make([]storage.PropKey, len(keys))
	count := make([]int, 1<<16)
	for shift := 0; shift < 32; shift += 16 {
		clear(count)
		for i, k := range src {
			cx.pollAt(i)
			count[k.Idx>>shift&0xFFFF]++
		}
		sum := 0
		for d, c := range count {
			count[d] = sum
			sum += c
		}
		for i, k := range src {
			cx.pollAt(i)
			d := k.Idx >> shift & 0xFFFF
			dst[count[d]] = k
			count[d]++
		}
		src, dst = dst, src
	}
}

// IsExtensible implements [[IsExtensible]].
func (cx *ExecutionContext) IsExtensible(o *storage.Object) bool {
	if o.Class == storage.ClassProxy {
		return cx.proxyIsExtensible(o)
	}
	return o.Flags&storage.ObjNotExtensible == 0
}

// PreventExtensions implements [[PreventExtensions]].
func (cx *ExecutionContext) PreventExtensions(o *storage.Object) bool {
	if o.Class == storage.ClassProxy {
		return cx.proxyPreventExtensions(o)
	}
	if o.Class == storage.ClassTypedArray && !o.ExtState().(*storage.TypedArray).IsTypedArrayFixedLength() {
		return false
	}
	if o.Flags&storage.ObjNotExtensible == 0 {
		if o.Flags&storage.ObjExotic != 0 {
			cx.resolve(o)
		}
		o.MakeNonExtensible()
	}
	return true
}

// SetPrototypeOf implements [[SetPrototypeOf]].
func (cx *ExecutionContext) SetPrototypeOf(o *storage.Object, proto *storage.Object) bool {
	if o.Class == storage.ClassProxy {
		return cx.proxySetPrototypeOf(o, proto)
	}
	if o.Shape.Proto == proto {
		return true
	}
	if o.Flags&storage.ObjNotExtensible != 0 || o.Shape.Proto == nil && o == o.Shape.Realm.Intrinsics[storage.IntrinsicObjectPrototype] {
		return false
	}
	for p := proto; p != nil; p = p.Shape.Proto {
		if p.Class == storage.ClassProxy {
			st := p.ExtState().(*storage.ProxyState)
			if !st.IsGlobal() || st.Target == nil {
				break
			}
			// Global proxies forward to the same backing global for cycle
			// detection regardless of the realm setting the prototype.
			p = st.Target
		}
		if p == o {
			return false
		}
	}
	cx.SetProto(o, proto)
	return true
}

// CreateDataPropertyOrThrow implements CreateDataPropertyOrThrow.
func (cx *ExecutionContext) CreateDataPropertyOrThrow(o *storage.Object, k storage.PropKey, val storage.Value) {
	if !cx.CreateDataProperty(o, k, val) {
		cx.ThrowTypeError("cannot define property '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "'")
	}
}

// CreateDataProperty implements CreateDataProperty. It reports false when the definition is refused.
func (cx *ExecutionContext) CreateDataProperty(o *storage.Object, k storage.PropKey, val storage.Value) bool {
	// An array whose index properties are all in its elements has none at the index just
	// past them.
	if k.Key == nil && o.Kind == storage.KindArray && int(k.Idx) == len(o.Elems) && o.Flags&(storage.ObjSparse|storage.ObjExotic) == 0 {
		return cx.AddIndex(o, k.Idx, val, storage.FlagsData)
	}
	return cx.defineOwnProperty(o, k, storage.DataDesc(val, storage.FlagsData))
}

// DefinePropertyOrThrow implements DefinePropertyOrThrow.
func (cx *ExecutionContext) DefinePropertyOrThrow(o *storage.Object, k storage.PropKey, d storage.PropDesc) {
	if !cx.defineOwnProperty(o, k, d) {
		cx.ThrowTypeError("cannot redefine property: " + wtf8.ToValidUTF8(cx.KeyString(k)))
	}
}

// Get implements Get.
func (cx *ExecutionContext) Get(o *storage.Object, k storage.PropKey) storage.Value {
	return cx.getProp(o, k, storage.Value{O: o})
}

// GetWithReceiver implements o.[[Get]](k, receiver).
func (cx *ExecutionContext) GetWithReceiver(o *storage.Object, k storage.PropKey, receiver storage.Value) storage.Value {
	return cx.getProp(o, k, receiver)
}

// SetWithReceiver implements o.[[Set]](k, val, receiver). It reports false when the store was refused.
func (cx *ExecutionContext) SetWithReceiver(o *storage.Object, k storage.PropKey, val storage.Value, receiver storage.Value) bool {
	return cx.setProp(o, k, val, receiver)
}

// DefineOwnProperty implements [[DefineOwnProperty]]. It reports false when the definition is refused.
func (cx *ExecutionContext) DefineOwnProperty(o *storage.Object, k storage.PropKey, d storage.PropDesc) bool {
	return cx.defineOwnProperty(o, k, d)
}

// appendIndexKeys appends the keys of the indices from 0 up to n to keys, checking for an
// interruption while it appends many.
func (cx *ExecutionContext) appendIndexKeys(keys []storage.PropKey, n int) []storage.PropKey {
	for i := 0; i < n; i += storage.PollInterval {
		if i > 0 {
			cx.Poll()
		}
		keys = storage.AppendIndexRun(keys, i, min(i+storage.PollInterval, n))
	}
	return keys
}

// appendElemKeys appends the keys of the elements in elems to keys, checking for an
// interruption while it appends many.
func (cx *ExecutionContext) appendElemKeys(keys []storage.PropKey, elems []storage.Value) []storage.PropKey {
	for i := 0; i < len(elems); i += storage.PollInterval {
		if i > 0 {
			cx.Poll()
		}
		keys = storage.AppendElemRun(keys, elems[i:min(i+storage.PollInterval, len(elems))], i)
	}
	return keys
}

// EnumerableOwnKeys implements EnumerableOwnProperties with the kind key.
// It appends the keys to keys and returns the extended slice.
func (cx *ExecutionContext) EnumerableOwnKeys(o *storage.Object, keys []storage.PropKey) []storage.PropKey {
	if o.Flags&storage.ObjExotic != 0 && (o.Fn != nil || o.Class == storage.ClassLazy) {
		cx.resolve(o)
	}
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	if (o.Class != storage.ClassObject && o.Class != storage.ClassArray && o.Class != storage.ClassFunction) || o.Flags&(storage.ObjSparse|storage.ObjExotic) != 0 {
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
	keys = cx.appendElemKeys(keys, o.Elems)
	s := o.Shape
	if s.Private {
		for i := 0; i < len(s.Order); i += storage.PollInterval {
			if i > 0 {
				cx.Poll()
			}
			keys = storage.AppendEnumerableNames(keys, s, s.Order[i:min(i+storage.PollInterval, len(s.Order))])
		}
		return keys
	}
	// The chain of a shared shape lists its keys from the last one.
	base := len(keys)
	keys = slices.Grow(keys, int(s.Count))[:base+int(s.Count)]
	t := s
	for ; t.Parent != nil; t = t.Parent {
		keys[base+int(t.Count)-1] = storage.EnumerableKey(t.Key, t.Flags)
	}
	for _, e := range t.Small {
		keys[base+int(e.Slot)] = storage.EnumerableKey(e.Key, e.Flags)
	}
	for key, p := range t.Table {
		keys[base+int(p.Slot)] = storage.EnumerableKey(key, p.Flags)
	}
	n := base
	for _, k := range keys[base:] {
		if k.Key != nil {
			keys[n] = k
			n++
		}
	}
	return keys[:n]
}
