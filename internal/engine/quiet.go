// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import "github.com/hajimehoshi/ekuma/internal/wtf8"

// QuietContext binds the engine operations that run no script or host code to one realm of an
// agent.
type QuietContext struct {
	cx *ExecutionContext
}

// Quiet returns the quiet context of the realm of cx.
func (cx *ExecutionContext) Quiet() QuietContext {
	return QuietContext{
		cx: cx,
	}
}

// NewArray is [ExecutionContext.NewArray].
func (q QuietContext) NewArray(elems []Value) *Object {
	return q.cx.NewArray(elems)
}

// NewArrayPair is [ExecutionContext.NewArrayPair].
func (q QuietContext) NewArrayPair(a, b Value) *Object {
	return q.cx.NewArrayPair(a, b)
}

// NewString is [ExecutionContext.NewString].
func (q QuietContext) NewString(s wtf8.String) Value {
	return q.cx.NewString(s)
}

// UnitString is [ExecutionContext.UnitString].
func (q QuietContext) UnitString(u uint16) Value {
	return q.cx.UnitString(u)
}

// Substring is [ExecutionContext.Substring].
func (q QuietContext) Substring(s Value, start, end int) Value {
	return q.cx.Substring(s, start, end)
}

// CreationQuietContext is the quiet context of [Object.CreationContext].
func (o *Object) CreationQuietContext() QuietContext {
	return QuietContext{
		cx: o.CreationContext(),
	}
}

// Poll is [ExecutionContext.Poll].
func (q QuietContext) Poll() {
	q.cx.Poll()
}

// WellKnownSymbol is [ExecutionContext.WellKnownSymbol].
func (q QuietContext) WellKnownSymbol(s WellKnownSymbol) *Object {
	return q.cx.WellKnownSymbol(s)
}

// NewMapObject is [ExecutionContext.NewMapObject].
func (q QuietContext) NewMapObject(proto *Object, data any) *Object {
	return q.cx.NewMapObject(proto, data)
}

// NewSetObject is [ExecutionContext.NewSetObject].
func (q QuietContext) NewSetObject(proto *Object, data any) *Object {
	return q.cx.NewSetObject(proto, data)
}

// HasOwnMethod reports whether method is the value of the own data property key of o. It can
// report false for an object with internal methods of its own.
func (q QuietContext) HasOwnMethod(o, key, method *Object) bool {
	if o.flags&objExotic != 0 {
		return false
	}
	p, ok := o.shape.lookup(key)
	return ok && p.flags&FlagAccessor == 0 && o.slots[p.slot].o == method
}

// InheritsMethod reports whether o has no own property key, its prototype is proto, and method
// is the value of the own data property key of proto. It can report false for an object with
// internal methods of its own.
func (q QuietContext) InheritsMethod(o, proto, key, method *Object) bool {
	if o.flags&objExotic != 0 || o.shape.proto != proto {
		return false
	}
	if _, ok := o.shape.lookup(key); ok {
		return false
	}
	return q.HasOwnMethod(proto, key, method)
}

// hasPropertyPlain is HasProperty of o and the named key when finding out runs no code. ok is
// false otherwise.
func (q QuietContext) hasPropertyPlain(o, key *Object) (has, ok bool) {
	switch q.cx.lookup(o, key).kind {
	case lookupSlot, lookupLength:
		return true, true
	case lookupAbsent, lookupNumericMiss:
		return false, true
	}
	return false, false
}

// lookupNamed finds the named property key on o and its prototype chain, as far as that runs no
// code.
func (q QuietContext) lookupNamed(o, key *Object) namedLookup {
	return q.cx.lookup(o, key)
}

// delegates reports whether looking up a property that is not found on o and its prototype chain
// can run code.
func (q QuietContext) delegates(o *Object) bool {
	for h := o; h != nil; h = h.shape.proto {
		if h.shape.family == delegatedRoot {
			return true
		}
	}
	return false
}

// GetVWithoutCall is GetV(v, key) when that runs no code and does not throw. ok is false
// otherwise.
func (q QuietContext) GetVWithoutCall(v Value, key *Object) (val Value, ok bool) {
	return q.getVWithoutCall(v, key, nil)
}

// getVWithoutCall is GetVWithoutCall, which fills c, which may be nil, when the lookup can be
// repeated from the cache.
func (q QuietContext) getVWithoutCall(v Value, key *Object, c *propCache) (val Value, ok bool) {
	cx := q.cx
	start := v.o
	switch {
	case v.IsObject():
	case v.IsNumber():
		start = cx.Intrinsic(IntrinsicNumberPrototype)
	case v.IsBool():
		start = cx.Intrinsic(IntrinsicBooleanPrototype)
	case v.IsSymbol():
		start = cx.Intrinsic(IntrinsicSymbolPrototype)
	case v.IsBigInt():
		start = cx.Intrinsic(IntrinsicBigIntPrototype)
	case v.IsString() && key != cx.Key(KeyLength):
		start = cx.Intrinsic(IntrinsicStringPrototype)
	default:
		return Undefined, false
	}
	r := cx.lookup(start, key)
	switch r.kind {
	case lookupAbsent, lookupNumericMiss:
		if c != nil {
			// Both misses are stable for this shape and prototype epoch.
			c.keep(start.shape)
			c.setInherited(start.shape, cacheProto, cx.protoEpoch)
			c.ref = &cx.missHolder.slots[0]
		}
		return Undefined, true
	case lookupLength:
		return r.length(v), true
	case lookupSlot:
		if r.prop.flags&FlagAccessor == 0 {
			if c != nil {
				c.keep(start.shape)
			}
			val, _ = cx.readNamedSlot(start, r.holder, r.prop, c)
			return val, true
		}
	}
	return Undefined, false
}
