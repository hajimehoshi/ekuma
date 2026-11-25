// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// Kinds of inline cache entries.
const (
	CacheNone uint8 = iota

	// CacheOwn: the property is in slot of the receiver.
	CacheOwn

	// CacheProto: ref points to an inherited data slot or the undefined slot for a cached miss.
	CacheProto

	// CacheGetter and CacheSetter: holder is the function to call.
	CacheGetter
	CacheSetter

	// CacheOwnGetter: the property is an accessor in slot of the receiver, whose getter is called.
	CacheOwnGetter

	// CacheAdd: a store adds the property and moves the receiver to the shape next.
	CacheAdd
)

// CacheEntry is what an inline cache remembers about one receiver shape.
type CacheEntry struct {
	// Shape is the Shape of the receiver the entry is valid for.
	Shape *Shape
	Kind  uint8
	Slot  int32

	Holder *Object

	// Epoch is the value of agent.ProtoEpoch the entry was filled at.
	Epoch uint32

	Next *Shape

	Ref *Value
}

// SetInherited records a lookup that depends on the receiver having no own property of that name.
func (e *CacheEntry) SetInherited(s *Shape, kind uint8, epoch uint32) {
	e.Shape = s
	e.Kind = kind
	e.Epoch = epoch
	if s.Private {
		s.renewOnAdd = true
	}
}

// PropCache holds two inline entries for a property access site and records additional
// receiver shapes for named reads.
type PropCache struct {
	CacheEntry
	Second CacheEntry
	Reads  *namedReads
}

// Entry returns the inline Entry for s, or nil on a miss.
func (c *PropCache) Entry(s *Shape) *CacheEntry {
	if c.Shape == s {
		return &c.CacheEntry
	}
	if c.Second.Shape == s {
		return &c.Second
	}
	return nil
}

// namedReads holds the locations of properties read at a site with several receiver shapes.
// Its entries do not keep the shapes, their prototypes or their property values alive.
type namedReads struct {
	Space  *Space
	Epoch  uint32
	Shapes map[uint32]namedRead
}

type namedRead struct {
	// Depth counts the prototype links to the holder. count detects properties added
	// to the receiver while it keeps its private shape.
	Depth int32
	Count int32
	Prop
}

// GlobalCache is the inline cache of an instruction that accesses a global variable. holder is
// the global object of the realm, or nil. While its shape matches, the binding exists.
// A resolution caches existence alone. Reads and writes also cache the initialized slot
// in ref, and writes only cache writable bindings.
//
// Declaring a global lexical binding or invalidating a cached global slot changes the
// global object's shape.
type GlobalCache struct {
	Shape  *Shape
	Ref    *Value
	Holder *Object
}

// Keep saves the current entry before c is filled for shape. It reports whether this
// displaces an entry for a third distinct shape.
func (c *PropCache) Keep(shape *Shape) bool {
	if c.Shape == nil || c.Shape == shape {
		return false
	}
	previous := c.Second.Shape
	c.Second = c.CacheEntry
	return previous != nil && previous != shape && previous != c.Shape
}

// RememberRead records the location of p for subsequent reads from start's shape.
func (c *PropCache) RememberRead(sp *Space, start, holder *Object, p Prop) {
	s := start.shape
	if s.ReadID == 0 {
		if sp.lastReadShapeID == ^uint32(0) {
			return
		}
		sp.lastReadShapeID++
		s.ReadID = sp.lastReadShapeID
	}
	r := c.Reads
	if r == nil {
		r = &namedReads{
			Shapes: make(map[uint32]namedRead),
		}
		c.Reads = r
	}
	if r.Space != sp || r.Epoch != sp.ProtoEpoch || len(r.Shapes) >= 64 {
		clear(r.Shapes)
		r.Space = sp
		r.Epoch = sp.ProtoEpoch
	}
	var depth int32
	for h := start; h != holder; h = h.shape.Proto {
		depth++
	}
	r.Shapes[s.ReadID] = namedRead{
		Depth: depth,
		Count: s.Count,
		Prop:  p,
	}
}

// OwnValue returns the value or the accessors that e, an entry for an own property, remembers
// of o, whose shape is that of e.
func (e *CacheEntry) OwnValue(o *Object) Value {
	return o.slots[e.Slot]
}

// RememberAbsent records that a receiver with the shape s has no property of the name.
func (c *PropCache) RememberAbsent(sp *Space, s *Shape) {
	c.Keep(s)
	c.SetInherited(s, CacheProto, sp.ProtoEpoch)
	c.Ref = &sp.MissHolder.slots[0]
}

// Remember records that a binding of the realm with the global object global is p of holder,
// which is global or the global lexical bindings.
func (c *GlobalCache) Remember(global, holder *Object, p Prop) {
	c.Holder = global
	c.Shape = global.shape
	c.Ref = &holder.slots[p.Slot]
}

// AddTo adds the property that e, an entry that adds one, remembers to o, whose shape is that of
// e, with the value v.
func (e *CacheEntry) AddTo(o *Object, v Value) {
	o.shape = e.Next
	o.AppendSlot(v)
}
