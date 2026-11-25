// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"maps"
	"slices"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
)

type Prop struct {
	Slot  int32
	Flags uint8
}

type ShapeKey struct {
	Key   *Object
	Flags uint8
}

// PrivateShapeThreshold is the number of properties above which an object stops sharing its shape.
const PrivateShapeThreshold = 128

// A shapeFamily determines which receivers can share a tree of named property layouts.
type shapeFamily uint8

const (
	OrdinaryRoot shapeFamily = iota
	ArrayRoot
	TypedArrayRoot
	LazyFunctionRoot
	// These families start a separate tree for each request.
	DelegatedRoot
	PrimitiveStringRoot
)

// Shape describes the layout of an object: which property lives in which slot, and the prototype.
//
// The root fixes the creation realm, prototype, and behavior of named properties. Objects built
// from that root by adding the same properties in the same order share a shape, until one
// gets a private shape, which is changed in place.
//
// A shared shape never changes, so an inline cache that remembers one stays valid for as long
// as the prototypes it looked through keep their properties.
type Shape struct {
	// Space and Realm identify where the objects with this shape were created.
	Space *Space
	Realm *RealmPart

	Proto  *Object
	Parent *Shape

	// Key is the property this shape adds to Parent, stored in slot Count-1.
	// It is an interned string or a symbol.
	Key    *Object
	Flags  uint8
	Family shapeFamily

	// enumerable tells whether a shared shape has an enumerable property whose key is a
	// string, once that is known: one of the enumerable* values.
	enumerable uint8

	Count int32

	// Table maps every property of the shape to its slot. A private shape always has one.
	// A shared shape with more than smallShapeSize properties builds it on first lookup,
	// or takes it over from its parent.
	Table map[*Object]Prop

	// Transitions maps a property to the shared shape that adds it to this one.
	Transitions transitionTable

	// Small lists the properties of a shared shape with at most smallShapeSize properties,
	// by slot. It is nil for any other shape.
	Small []ShapeEntry

	Private bool

	// renewOnAdd is set on a private shape when a cache relies on a key being absent.
	// Adding a property must then give the object a new shape.
	renewOnAdd bool

	// flatBased is set for a shared shape without parent that has properties of its own
	// (see [Space.FinishBuiltins]) and for the shapes that add properties to it. Such a shape with
	// more than smallShapeSize properties keeps a table from the start.
	flatBased bool

	// branchSlots is, for a shared shape with more than one transition, the largest number of
	// properties that objects were given after its own, up to maxInstanceSlots.
	branchSlots uint8

	// ReadID identifies this shape in its agent's megamorphic read caches. Zero is unassigned.
	ReadID uint32

	// Order lists the keys of a private shape in insertion order, at the indices of their
	// slots. A deleted key leaves nil behind until the shape is compacted, so Count is the number
	// of keys and len(Order) the number of slots.
	Order []*Object

	// deletion is the result of the last deletion of a property from a shared shape, or nil.
	deletion *deletion
}

// deletion is the shared shape that deleting the property key from a shared shape gives.
type deletion struct {
	key   *Object
	shape *Shape
}

// ShapeEntry is a property of a shape and where it is.
type ShapeEntry struct {
	Key *Object
	Prop
}

// transitionTable maps a property to the shared shape that adds it to a shape.
// The zero value is an empty table.
type transitionTable struct {
	// only is the shape of the sole transition while there is only one, and nil otherwise.
	// Its Key and Flags are the key of the transition.
	only *Shape

	// rest holds the transitions once there is more than one.
	rest sweptTable[ShapeKey, Shape]
}

// Get returns the shape under k, or nil.
func (t *transitionTable) Get(k ShapeKey) *Shape {
	if s := t.only; s != nil {
		if s.Key == k.Key && s.Flags == k.Flags {
			return s
		}
		return nil
	}
	return t.rest.get(k)
}

// put adds s under the key that s adds to its parent, which the table must not have.
func (t *transitionTable) put(s *Shape) {
	if t.only == nil && t.rest.Entries == nil {
		t.only = s
		return
	}
	if only := t.only; only != nil {
		t.only = nil
		t.rest.put(ShapeKey{
			Key:   only.Key,
			Flags: only.Flags,
		}, only, transitionsMinSweep, nil)
	}
	t.rest.put(ShapeKey{
		Key:   s.Key,
		Flags: s.Flags,
	}, s, transitionsMinSweep, nil)
}

// empty reports whether no transition was ever added.
func (t *transitionTable) empty() bool {
	return t.only == nil && t.rest.Entries == nil
}

// Len returns the number of entries.
func (t *transitionTable) Len() int {
	if t.only != nil {
		return 1
	}
	return len(t.rest.Entries)
}

// transitionsMinSweep is the number of transitions from one shape below which they are kept
// alive with the shape.
const transitionsMinSweep = 8

// smallShapeSize is the number of properties up to which a shared shape is searched linearly.
const smallShapeSize = 8

func (s *Shape) Lookup(key *Object) (Prop, bool) {
	if s.Small == nil {
		if s.Table == nil {
			s.Table = make(map[*Object]Prop, s.Count)
			for t := s; t.Parent != nil; t = t.Parent {
				s.Table[t.Key] = Prop{
					Slot:  t.Count - 1,
					Flags: t.Flags,
				}
			}
		}
		p, ok := s.Table[key]
		return p, ok
	}
	for _, e := range s.Small {
		if e.Key == key {
			return e.Prop, true
		}
	}
	return Prop{}, false
}

// Keys returns the property keys in insertion order. The result must not be modified.
func (s *Shape) Keys() []*Object {
	if s.Private {
		if len(s.Order) != int(s.Count) {
			return slices.DeleteFunc(slices.Clone(s.Order), func(k *Object) bool {
				return k == nil
			})
		}
		return s.Order
	}
	keys := make([]*Object, s.Count)
	t := s
	for ; t.Parent != nil; t = t.Parent {
		keys[t.Count-1] = t.Key
	}
	// A shared shape without parent may have properties of its own: see [Space.FinishBuiltins].
	for _, e := range t.Small {
		keys[e.Slot] = e.Key
	}
	for k, p := range t.Table {
		keys[p.Slot] = k
	}
	return keys
}

// StringKeys returns the property keys of s that are strings, in insertion order.
func (s *Shape) StringKeys() []PropKey {
	if s.Private {
		keys := make([]PropKey, 0, len(s.Order))
		for _, k := range s.Order {
			if k != nil && k.Kind == KindString {
				keys = append(keys, NamedKey(k))
			}
		}
		return keys
	}
	keys := make([]PropKey, s.Count)
	t := s
	for ; t.Parent != nil; t = t.Parent {
		keys[t.Count-1] = NamedKey(t.Key)
	}
	for _, e := range t.Small {
		keys[e.Slot] = NamedKey(e.Key)
	}
	for k, p := range t.Table {
		keys[p.Slot] = NamedKey(k)
	}
	return slices.DeleteFunc(keys, PropKey.IsSymbol)
}

// Values of Shape.enumerable.
const (
	enumerableUnknown uint8 = iota
	enumerableNone
	enumerableSome
)

// HasEnumerableString reports whether s has an enumerable property whose key is a string, checking
// for an interruption while it looks at many.
func (s *Shape) HasEnumerableString() bool {
	if s.Private {
		var n int
		for k, p := range s.Table {
			if isEnumerableString(k, p.Flags) {
				return true
			}
			if n++; n%PollInterval == 0 {
				s.Space.Poll()
			}
		}
		return false
	}
	// A shared shape does not change.
	if s.enumerable == enumerableUnknown {
		s.enumerable = enumerableNone
		if s.sharedHasEnumerableString() {
			s.enumerable = enumerableSome
		}
	}
	if ValidateExec {
		s.checkEnumerable()
	}
	return s.enumerable == enumerableSome
}

// sharedHasEnumerableString reports whether the shared shape s has an enumerable property
// whose key is a string.
func (s *Shape) sharedHasEnumerableString() bool {
	t := s
	for ; t.Parent != nil; t = t.Parent {
		if isEnumerableString(t.Key, t.Flags) {
			return true
		}
	}
	// A shared shape without parent may have properties of its own: see [Space.FinishBuiltins].
	for _, e := range t.Small {
		if isEnumerableString(e.Key, e.Flags) {
			return true
		}
	}
	for k, p := range t.Table {
		if isEnumerableString(k, p.Flags) {
			return true
		}
	}
	return false
}

// With returns the shared shape that has key in addition to the properties of s.
func (s *Shape) With(key *Object, flags uint8) *Shape {
	k := ShapeKey{
		Key:   key,
		Flags: flags,
	}
	if t := s.Transitions.Get(k); t != nil {
		return t
	}
	t := &Shape{
		Space:  s.Space,
		Realm:  s.Realm,
		Proto:  s.Proto,
		Family: s.Family,
		Parent: s,
		Key:    key,
		Flags:  flags,
		Count:  s.Count + 1,
	}
	if t.Count <= smallShapeSize {
		t.Small = append(make([]ShapeEntry, 0, t.Count), s.Small...)
		t.Small = append(t.Small, ShapeEntry{
			Key:   key,
			Slot:  s.Count,
			Flags: flags,
		})
	}
	switch {
	case s.flatBased:
		t.flatBased = true
		if t.Small == nil {
			// The chain of t does not list the properties of its base, so t cannot build its
			// table from the chain.
			t.Table = make(map[*Object]Prop, t.Count)
			maps.Copy(t.Table, s.Table)
			for _, e := range s.Small {
				t.Table[e.Key] = e.Prop
			}
			t.Table[key] = Prop{
				Slot:  s.Count,
				Flags: flags,
			}
		}
	case s.Table != nil && s.Transitions.empty() && !s.Private:
		// An object that gains properties one by one looks each intermediate shape up once, so the
		// first child takes over the table, and s builds a new one only if it is looked up again.
		t.Table, s.Table = s.Table, nil
		t.Table[key] = Prop{
			Slot:  s.Count,
			Flags: flags,
		}
	}
	s.Transitions.put(t)
	d := 1
	for p := s; p != nil && d <= maxInstanceSlots; p = p.Parent {
		if p.Transitions.only == nil {
			p.branchSlots = max(p.branchSlots, uint8(d))
		}
		d++
	}
	return t
}

// maxReadded is the largest number of properties after a deleted one for which the deletion
// gives a shared shape.
const maxReadded = 16

// Without returns the shared shape with the properties of the shared shape s other than key, in
// the same order and with the same attributes. It returns nil when the chain of s does not add
// key or adds more than maxReadded properties after it.
func (s *Shape) Without(key *Object) *Shape {
	if d := s.deletion; d != nil && d.key == key {
		return d.shape
	}
	// The properties after key, from the last one. Each can make a shape that the tree of shapes
	// did not have yet.
	var buf [maxReadded]ShapeKey
	later := buf[:0]
	p := s
	for p != nil && p.Key != key {
		if len(later) == maxReadded {
			return nil
		}
		later = append(later, ShapeKey{
			Key:   p.Key,
			Flags: p.Flags,
		})
		p = p.Parent
	}
	if p == nil {
		return nil
	}
	t := p.Parent
	for _, k := range slices.Backward(later) {
		t = t.With(k.Key, k.Flags)
	}
	if s.deletion == nil {
		s.deletion = &deletion{}
	}
	s.deletion.key, s.deletion.shape = key, t
	return t
}

// privateCopy returns a private shape with the properties of s in the same slots.
func (s *Shape) privateCopy() *Shape {
	order := s.Order
	if !s.Private {
		order = s.Keys()
	}
	t := &Shape{
		Space:   s.Space,
		Realm:   s.Realm,
		Proto:   s.Proto,
		Family:  s.Family,
		Count:   s.Count,
		Table:   make(map[*Object]Prop, s.Count+8),
		Private: true,
		Order:   slices.Clone(order),
	}
	for _, k := range t.Order {
		if k != nil {
			p, _ := s.Lookup(k)
			t.Table[k] = p
		}
	}
	return t
}

// handOver returns a new private shape with the properties of the private shape s, which
// it takes from s. Only the object that has s may call it.
func (s *Shape) handOver() *Shape {
	t := &Shape{
		Space:   s.Space,
		Realm:   s.Realm,
		Proto:   s.Proto,
		Family:  s.Family,
		Count:   s.Count,
		Table:   s.Table,
		Private: true,
		Order:   s.Order,
	}
	s.Table = nil
	s.Order = nil
	return t
}

// shapeTable holds the shapes that new objects start with.
type shapeTable struct {
	// ShortStrings are immutable sentinels. Per-string caches belong to materialized objects.
	ShortStrings         *[shortstrings.MaxBytes + 1]Object
	NullProto            [DelegatedRoot]*Shape
	Object               *Shape
	Array                *Shape
	Function             *Shape
	MaterializedFunction *Shape
	String               *Shape
	IterResult           *Shape
	Arguments            *Shape

	// regExp is the shape of a new RegExp object whose prototype is %RegExp.prototype%,
	// once one was made.
	regExp *Shape

	// Proxy is the shape of a new Proxy object. It has no prototype, so that a lookup in shapes
	// stops at a proxy, and only ever gains private names.
	Proxy *Shape
}

// NewShape returns a new empty shared shape for objects whose prototype is proto.
func (sp *Space) NewShape(proto *Object, r *RealmPart, family shapeFamily) *Shape {
	if proto != nil {
		proto.flags |= ObjIsProto
	}
	return &Shape{
		Space:  sp,
		Realm:  r,
		Proto:  proto,
		Family: family,
		Small:  []ShapeEntry{},
	}
}

// ShapeFor returns the ordinary root shape in r for prototype proto, which may be nil.
func (r *RealmPart) ShapeFor(proto *Object) *Shape {
	return r.ShapeForRealm(proto, r)
}

// ShapeForRealm returns the ordinary root shape in r for prototype proto, which may be nil.
func (sp *Space) ShapeForRealm(proto *Object, r *RealmPart) *Shape {
	if proto == nil {
		return r.Shapes.NullProto[OrdinaryRoot]
	}
	return proto.ensureMeta().protoShapes.forRealm(sp, proto, r, OrdinaryRoot)
}

// RootShape selects the tree of layouts shared by receivers of this family, prototype and realm.
func (sp *Space) RootShape(proto *Object, r *RealmPart, family shapeFamily) *Shape {
	if family == OrdinaryRoot {
		return sp.ShapeForRealm(proto, r)
	}
	if family >= DelegatedRoot {
		return sp.NewShape(proto, r, family)
	}
	if proto == nil {
		s := &r.Shapes.NullProto[family]
		if *s == nil {
			*s = sp.NewShape(nil, r, family)
		}
		return *s
	}
	m := proto.ensureMeta()
	if m.otherProtoShapes == nil {
		m.otherProtoShapes = new([DelegatedRoot - ArrayRoot]prototypeShapeCache)
	}
	return m.otherProtoShapes[family-ArrayRoot].forRealm(sp, proto, r, family)
}

type prototypeShapeCache struct {
	local *Shape
	// A prototype can outlive the realm that created an object with it.
	foreign weak.Pointer[Shape]
}

func (c *prototypeShapeCache) forRealm(sp *Space, proto *Object, r *RealmPart, family shapeFamily) *Shape {
	if proto.shape.Realm != r {
		if s := c.foreign.Value(); s != nil && s.Realm == r {
			return s
		}
		s := sp.NewShape(proto, r, family)
		c.foreign = weak.Make(s)
		return s
	}
	// The shape is stored on proto, so that it and the shapes derived from it are collected
	// together with proto.
	if c.local == nil {
		c.local = sp.NewShape(proto, r, family)
	}
	return c.local
}

// FunctionShapeFor returns the root for functions whose initial named properties are still implicit.
func (r *RealmPart) FunctionShapeFor(proto *Object) *Shape {
	return r.RootShape(proto, r, LazyFunctionRoot)
}

// RenewShape gives o a private shape with the properties of its shape, which no inline cache
// has seen.
func (o *Object) RenewShape() {
	if o.shape.Private {
		o.shape = o.shape.handOver()
	} else {
		o.shape = o.shape.privateCopy()
	}
}

// compact removes the deleted slots of the private shape of o.
func (o *Object) compact() {
	s := o.shape
	if !s.Private || len(s.Order) == int(s.Count) {
		return
	}
	n := 0
	for i, k := range s.Order {
		if k == nil {
			continue
		}
		p := s.Table[k]
		p.Slot = int32(n)
		s.Table[k] = p
		s.Order[n] = k
		o.slots[n] = o.slots[i]
		n++
	}
	clear(s.Order[n:])
	s.Order = s.Order[:n]
	o.truncateSlots(n)
}

// rebuildShape preserves the slots of o when its prototype or named property behavior changes.
func (sp *Space) rebuildShape(o *Object, proto *Object, family shapeFamily) {
	old := o.shape
	if old.Private {
		s := old.privateCopy()
		s.Proto = proto
		s.Family = family
		if proto != nil {
			proto.flags |= ObjIsProto
		}
		o.shape = s
		return
	}
	s := sp.RootShape(proto, old.Realm, family)
	for _, k := range old.Keys() {
		p, _ := old.Lookup(k)
		s = s.With(k, p.Flags)
	}
	o.shape = s
}

// MakePrivate gives o a shape of its own, so that no inline cache filled for another object applies to it.
func (o *Object) MakePrivate() {
	if !o.shape.Private {
		o.shape = o.shape.privateCopy()
	}
}

// builtinLayout is the state of the creation of the built-in objects.
type builtinLayout struct {
	// building is set between [Space.StartBuiltins] and [Space.FinishBuiltins].
	building bool

	// Objects are the objects that got a private shape while building.
	Objects []*Object
}

// StartBuiltins starts the creation of the built-in objects, which [Space.FinishBuiltins] ends.
// In between, objects should only get their initial properties.
func (sp *Space) StartBuiltins() {
	sp.Builtins.building = true
	sp.Strings.StartKeeping()
}

// FinishBuiltins ends the creation of the built-in objects.
func (sp *Space) FinishBuiltins() {
	for _, o := range sp.Builtins.Objects {
		o.shareShape()
	}
	sp.Builtins = builtinLayout{}
	sp.Strings.EndKeeping()
}

// shareShape turns the private shape of o into a shared shape without parent that holds all
// the properties itself, so that inline caches apply to o. The shape must not change afterwards.
func (o *Object) shareShape() {
	s := o.shape
	if !s.Private || o.flags&ObjNotExtensible != 0 {
		return
	}
	o.compact()
	s = o.shape
	// The keys of a private shape are in the order of their slots.
	s.Private = false
	s.flatBased = true
	order := s.Order
	s.Order = nil
	if s.Count <= smallShapeSize {
		s.Small = make([]ShapeEntry, len(order))
		for i, k := range order {
			s.Small[i] = ShapeEntry{
				Key:  k,
				Prop: s.Table[k],
			}
		}
		s.Table = nil
	}
}
