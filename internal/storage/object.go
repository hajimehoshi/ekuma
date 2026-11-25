// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"maps"
	"slices"
	"unicode/utf8"
	"weak"

	"github.com/hajimehoshi/ekuma/internal/shortstrings"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// Property attributes.
const (
	FlagEnumerable uint8 = 1 << iota
	FlagWritable
	FlagConfigurable
	FlagAccessor

	FlagsData   = FlagEnumerable | FlagWritable | FlagConfigurable
	FlagsHidden = FlagWritable | FlagConfigurable
)

// [Object] flags.
const (
	// ObjInterned marks a string that is the unique representative of its contents,
	// so that it can serve as a property key compared by identity.
	ObjInterned uint8 = 1 << iota

	// ObjASCIIKnown says that ObjASCII is valid.
	ObjASCIIKnown
	ObjASCII

	// ObjNotExtensible marks an object that refuses new properties.
	ObjNotExtensible

	// ObjIsProto marks an object that is the prototype of some shape. Changing the properties
	// of such an object invalidates the inline caches that looked through it.
	ObjIsProto

	// ObjFrozenLength marks an array whose length is not writable.
	ObjFrozenLength

	// ObjSparse marks an object that has properties with array index keys outside elems:
	// they are in the shape under their canonical string keys. elems only ever holds
	// data properties that are writable, enumerable and configurable.
	ObjSparse

	// ObjExotic marks an object whose named properties cannot be found by looking at its shape alone.
	ObjExotic
)

// ObjReversed marks a string whose str holds its contents with the bytes in reverse order.
// Its bit is that of ObjFrozenLength, which only arrays and private names use. The byte length,
// the ASCII-ness and the UTF-16 length of str are those of the contents, so only a read of the
// contents themselves needs [Object.Str].
const ObjReversed = ObjFrozenLength

// objKept marks an interned string that the agent keeps alive for as long as it lives. Its bit is
// that of ObjIsProto, which strings never use.
const objKept = ObjIsProto

// [Object] pinned bits, which mark storage that stays reachable after the slots or the elements
// of the object move out of it.
const (
	// PinnedSlots and PinnedElems mark slots and elements that may be in the object's allocation.
	PinnedSlots uint8 = 1 << iota
	PinnedElems

	// PinnedGlobalSlots marks the slots of a global object or of the global lexical bindings,
	// which the inline caches of global variables point into.
	PinnedGlobalSlots
)

// class distinguishes the built-in objects that have internal state or exotic behavior.
type class uint8

const (
	ClassObject class = iota
	ClassArray
	ClassFunction
	ClassError
	ClassBoolean
	ClassNumber
	ClassString
	ClassSymbol
	ClassBigInt
	ClassArguments
	ClassDate
	ClassRegExp
	ClassMap
	ClassSet
	ClassWeakMap
	ClassWeakSet
	ClassWeakRef
	ClassFinalizationRegistry
	ClassArrayBuffer
	ClassTypedArray
	ClassDataView
	ClassPromise
	ClassGenerator
	ClassProxy
	ClassGlobalProxyHandler
	ClassIterator
	ClassForIn
	ClassPrivateName
	ClassModuleNamespace
	ClassRawJSON
	ClassDisposableStack
	ClassAsyncDisposableStack
	ClassTemporal
	ClassIntl
	ClassShadowRealm
	ClassModuleSource
	// ClassLazy is an ordinary object whose properties are not defined yet.
	ClassLazy

	// ClassEvalScope is the scope of OpEvalScope, whose properties are the variables that
	// direct evals declare in a function.
	ClassEvalScope
)

// Object is every heap value: plain objects, arrays, functions, strings, symbols, and the singletons.
type Object struct {
	Kind  Kind
	Class class
	Flags uint8

	// Pinned holds the Pinned bits of the storage of slots and elems.
	Pinned uint8

	// Length is the Length of an array, which is at least len(elems). For a string it is 0,
	// or the array index that the string stands for plus one. See [Object.ArrayIndex].
	Length uint32

	// Shape is the layout of slots. Only the singletons and internal values have none.
	Shape *Shape

	// Primitive values have no property storage.
	*ObjectProperties

	// Fn is set for a callable object. A closure must call OwnFunction before changing its state.
	Fn *Function

	// Text is the content of a string or the description of a symbol. A string marked ObjReversed
	// holds its bytes in reverse order. Packed-string sentinels use zero bytes to carry only the
	// byte length.
	Text wtf8.String

	// Ext is the internal state of a built-in object or the push size hint of a closure.
	// For a string that is not interned it is the interned string with the same content,
	// once that was looked up, or the state of concatenations onto it (see concat.go).
	// When the object has side data, ext is its *ObjectMeta, which holds the internal state
	// instead. See [Object.ExtState] and [Object.Meta].
	Ext any
}

// ObjectProperties holds the own properties of a JavaScript object.
type ObjectProperties struct {
	Slots []Value

	// Elems holds the properties whose keys are array indices, by index.
	// An element that does not exist holds hole.
	Elems []Value
}

// PropertyObject keeps an object's header and property storage in one allocation.
// Primitive heap values use only Object.
type PropertyObject struct {
	Object
	ObjectProperties
}

func (o *PropertyObject) Init() *Object {
	o.Object.ObjectProperties = &o.ObjectProperties
	return &o.Object
}

// ObjectMeta is the side data that few objects need: what indexing a non-ASCII string needs,
// the entries in weak collections of an object or a symbol, and the empty shapes of the
// objects whose prototype is this object.
type ObjectMeta struct {
	// Length and cursor are the cached UTF-16 view of a string that is not ASCII:
	// its length, or 0 before it is known, and the position of the most recent access.
	// Only a string of at most MaxStringLength bytes has them, so they fit in 32 bits.
	Length int32
	Cursor stringPos

	// checkpoints holds the positions of the code units of such a string whose indices are
	// multiples of checkpointInterval, once an access far from cursor needed one. See
	// [checkpointsOf].
	checkpoints *[]uint32

	// weak holds the values that the weak collections associate with an object or a symbol,
	// by collection. See [WeakTable].
	weak *weakEntries

	protoShapes prototypeShapeCache

	// otherProtoShapes holds the roots for receivers with special named properties.
	otherProtoShapes *[DelegatedRoot - ArrayRoot]prototypeShapeCache

	// shiftedElems is the number of unused values before the elements of an array in their
	// storage, left by removing the first elements. It counts only while the elements have the
	// capacity shiftedCap: growing them makes the capacity greater, and the operations that can
	// make it smaller keep the two in step. See [Object.ElemsStorage].
	shiftedElems int
	shiftedCap   int

	// Ext is the internal state of the object that has this side data.
	Ext any
}

// ExtState returns the internal state of o.
func (o *Object) ExtState() any {
	if m, ok := o.Ext.(*ObjectMeta); ok {
		return m.Ext
	}
	return o.Ext
}

// SetExtState sets the internal state of o to v.
func (o *Object) SetExtState(v any) {
	if m, ok := o.Ext.(*ObjectMeta); ok {
		m.Ext = v
		return
	}
	o.Ext = v
}

// ExtConcatCount returns the concatenation count in the internal state of the string o, or 0.
func (o *Object) ExtConcatCount() ConcatCount {
	if c, ok := o.Ext.(ConcatCount); ok {
		return c
	}
	if m, ok := o.Ext.(*ObjectMeta); ok {
		c, _ := m.Ext.(ConcatCount)
		return c
	}
	return 0
}

// ExtObject returns the object in the internal state of o, and reports whether there is one.
func (o *Object) ExtObject() (*Object, bool) {
	if k, ok := o.Ext.(*Object); ok {
		return k, true
	}
	if m, ok := o.Ext.(*ObjectMeta); ok {
		k, ok := m.Ext.(*Object)
		return k, ok
	}
	return nil, false
}

// Meta returns the side data of o, or nil when it has none.
func (o *Object) Meta() *ObjectMeta {
	m, _ := o.Ext.(*ObjectMeta)
	return m
}

// stringPos is a [wtf8.Pos] in a string of at most MaxStringLength bytes.
type stringPos struct {
	unit int32
	byte int32
	low  bool
}

func (p stringPos) pos() wtf8.Pos {
	return wtf8.Pos{
		Unit: int(p.unit),
		Byte: int(p.byte),
		Low:  p.low,
	}
}

func newStringPos(p wtf8.Pos) stringPos {
	return stringPos{
		unit: int32(p.Unit),
		byte: int32(p.Byte),
		low:  p.Low,
	}
}

// ensureMeta returns the side data of o, creating it if needed.
func (o *Object) ensureMeta() *ObjectMeta {
	if m := o.Meta(); m != nil {
		return m
	}
	m := &ObjectMeta{Ext: o.Ext}
	o.Ext = m
	return m
}

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

// DelegatesPropertyAccess reports whether a lookup must use the object's property operations
// even when its storage has no matching key.
func (o *Object) DelegatesPropertyAccess() bool {
	return o.Flags&ObjExotic != 0 && o.Shape.Family == DelegatedRoot
}

// IndicesInElements reports whether elems accounts for all own indexed properties.
func (o *Object) IndicesInElements() bool {
	return (o.Kind == KindObject && o.Class == ClassObject || o.Kind == KindArray) && o.Flags&(ObjSparse|ObjExotic) == 0
}

// Shape describes the layout of an object: which property lives in which slot, and the prototype.
//
// The root fixes the creation realm, prototype, and behavior of named properties. Objects built
// from that root by adding the same properties in the same order share a shape, until one
// gets a private shape, which is changed in place.
//
// A shared shape never changes, so an inline cache that remembers one stays valid for as long
// as the prototypes it looked through keep their properties.
type Shape struct {
	// Space and realm identify where the objects with this shape were created.
	Space *Space
	Realm *RealmPart

	Proto  *Object
	Parent *Shape

	// Key is the property this shape adds to parent, stored in slot count-1.
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

	// Order lists the keys of a private shape in insertion Order, at the indices of their
	// slots. A deleted key leaves nil behind until the shape is compacted, so count is the number
	// of keys and len(order) the number of slots.
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
	// Its key and flags are the key of the transition.
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

// Keys returns the property Keys in insertion order. The result must not be modified.
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

// Values of shape.enumerable.
const (
	enumerableUnknown uint8 = iota
	enumerableNone
	enumerableSome
)

// HasEnumerableString reports whether s has an enumerable property whose key is a string.
func (s *Shape) HasEnumerableString() bool {
	if s.Private {
		for k, p := range s.Table {
			if isEnumerableString(k, p.Flags) {
				return true
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
		proto.Flags |= ObjIsProto
	}
	return &Shape{
		Space:  sp,
		Realm:  r,
		Proto:  proto,
		Family: family,
		Small:  []ShapeEntry{},
	}
}

// ShapeFor returns the ordinary root shape in the current realm for prototype proto, which may be nil.
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
	if proto.Shape.Realm != r {
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

// NewObject returns a new empty object with the prototype proto, which may be nil.
func (r *RealmPart) NewObject(proto *Object) *Object {
	return NewObject(r.ShapeFor(proto))
}

func NewObject(s *Shape) *Object {
	return (&PropertyObject{
		Kind:  KindObject,
		Shape: s,
	}).Init()
}

// maxInstanceSlots bounds the room for properties that NewInstance gives an object.
const maxInstanceSlots = 16

// branchedInstanceSlots is the room for properties that NewInstance gives an object when
// the objects with its shape have been given different properties.
const branchedInstanceSlots = 4

// NewInstance returns a new empty ordinary object with the shape s, with room for the
// properties that the objects with s have been given one after another so far, and for some
// more when they went on with different properties.
func NewInstance(s *Shape) *Object {
	n := 0
	for t := s; n < maxInstanceSlots; t = t.Transitions.only {
		if t.Transitions.only == nil {
			switch {
			case t.Transitions.empty():
			case t == s:
				// The prototype of a subclass also starts with s.
				n = branchedInstanceSlots
			default:
				// The objects with s have gone on with different properties from t on.
				n += int(t.branchSlots)
			}
			break
		}
		n++
	}
	o, slots := NewObjectWithStorage(min(n, maxInstanceSlots))
	o.Kind = KindObject
	o.Shape = s
	o.InitSlots(slots[:0])
	return o
}

// NewArray returns a new array with the given elements, which it keeps.
func (r *RealmPart) NewArray(elems []Value) *Object {
	return NewArray(r.Shapes.Array, elems)
}

// NewArray returns a new array with the shape s and the given elements, which it keeps.
func NewArray(s *Shape, elems []Value) *Object {
	return (&PropertyObject{
		Kind:   KindArray,
		Class:  ClassArray,
		Shape:  s,
		Elems:  elems,
		Length: uint32(len(elems)),
	}).Init()
}

// NewEmptyObjectWithRoom returns a new empty ordinary object whose prototype is %Object.prototype%,
// with room for n properties.
func (r *RealmPart) NewEmptyObjectWithRoom(n int) *Object {
	o, slots := NewObjectWithStorage(n)
	o.Kind = KindObject
	o.Shape = r.Shapes.Object
	o.InitSlots(slots[:0])
	return o
}

// ArrayCreate implements ArrayCreate.
func (r *RealmPart) ArrayCreate(length uint32, proto *Object) *Object {
	var a *Object
	if length > 0 && length <= MaxPresizedElems {
		// The array is usually filled up to its length afterwards.
		var elems []Value
		a, elems = NewObjectWithStorage(int(length))
		a.Kind = KindArray
		a.Class = ClassArray
		a.Shape = r.Shapes.Array
		a.InitElems(elems[:0])
	} else {
		a = r.NewArray(nil)
	}
	r.SetProto(a, proto)
	a.Length = length
	return a
}

// pairArray is an array of two elements together with the storage of its elements,
// so that creating one is a single allocation.
type pairArray struct {
	PropertyObject
	pair [2]Value
}

// closureObject is a function object together with its function state.
type closureObject struct {
	PropertyObject
	state Function
}

// closureObjectWith is a closureObject together with the storage of its upvalues and of the
// upvalues that hold copies of variables.
type closureObjectWith[U, C any] struct {
	closureObject

	// copies comes first so that an empty C adds no padding at the end.
	copies C
	upvals U
}

// NewClosure returns a fresh function object and the storage for its copied upvalues.
func NewClosure(p *FuncProto) (*Object, []Upvalue) {
	if len(p.Upvals) == 0 && !p.IsMethod && !p.IsClassConstructor {
		if p.plainFunction == nil {
			p.plainFunction = &Function{
				Code:        p,
				Constructor: p.IsConstructor,
				shared:      true,
			}
		}
		return (&PropertyObject{
			Fn: p.plainFunction,
		}).Init(), nil
	}
	o, copies := newClosureObject(len(p.Upvals), p.Ncopied)
	o.Fn.Code = p
	o.Fn.Constructor = p.IsConstructor
	o.Fn.ClassConstructor = p.IsClassConstructor
	o.Fn.Derived = p.IsDerived
	return o, copies
}

// OwnFunction gives o its own function state before an instance-specific change.
func (o *Object) OwnFunction() *Function {
	if o.Fn.shared {
		f := *o.Fn
		f.shared = false
		o.Fn = &f
	}
	return o.Fn
}

// newClosureObject returns a zero object whose function state has n nil upvalues, and
// storage for the ncopied of them that hold copies of variables. Small closures are a single
// allocation.
func newClosureObject(n, ncopied int) (*Object, []Upvalue) {
	var c *closureObject
	var upvals []*Upvalue
	var copies []Upvalue
	switch n<<2 | ncopied {
	case 0:
		c = &closureObject{}
	case 1 << 2:
		w := &closureObjectWith[[1]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 1<<2 | 1:
		w := &closureObjectWith[[1]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2 << 2:
		w := &closureObjectWith[[2]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 2<<2 | 1:
		w := &closureObjectWith[[2]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2<<2 | 2:
		w := &closureObjectWith[[2]*Upvalue, [2]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3 << 2:
		w := &closureObjectWith[[3]*Upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 3<<2 | 1:
		w := &closureObjectWith[[3]*Upvalue, [1]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 2:
		w := &closureObjectWith[[3]*Upvalue, [2]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 3:
		w := &closureObjectWith[[3]*Upvalue, [3]Upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	default:
		// The capacities fill an allocation size class.
		switch {
		case n <= 5 && ncopied == 0:
			w := &closureObjectWith[[5]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 7 && ncopied == 0:
			w := &closureObjectWith[[7]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 11 && ncopied == 0:
			w := &closureObjectWith[[11]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied == 0:
			w := &closureObjectWith[[15]*Upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied > 4 && ncopied <= 8:
			w := &closureObjectWith[[15]*Upvalue, [8]Upvalue]{}
			c, upvals, copies = &w.closureObject, w.upvals[:n:n], w.copies[:ncopied:ncopied]
		default:
			c = &closureObject{}
			upvals = make([]*Upvalue, n)
			if ncopied > 0 {
				copies = make([]Upvalue, ncopied)
			}
		}
	}
	c.state.Upvals = upvals
	c.Fn = &c.state
	return c.Init(), copies
}

const MaxInlineObjectValues = 8

// NewObjectWithStorage returns an object and storage for n values. The object is zero but for
// its pinned bits. For a small n, both are a single allocation.
func NewObjectWithStorage(n int) (*Object, []Value) {
	var o *Object
	var storage []Value
	switch n {
	case 0:
		return (&PropertyObject{}).Init(), nil
	case 1:
		w := &objectWith[[1]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 2:
		w := &objectWith[[2]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 3:
		w := &objectWith[[3]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 4:
		w := &objectWith[[4]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 5:
		w := &objectWith[[5]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 6:
		w := &objectWith[[6]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 7:
		w := &objectWith[[7]Value]{}
		o, storage = w.Init(), w.storage[:]
	case MaxInlineObjectValues:
		w := &objectWith[[MaxInlineObjectValues]Value]{}
		o, storage = w.Init(), w.storage[:]
	default:
		return (&PropertyObject{}).Init(), make([]Value, n)
	}
	// The storage becomes the slots, the elements or both.
	o.Pinned = PinnedSlots | PinnedElems
	return o, storage
}

// objectWith is an object together with storage for its slots or elements.
type objectWith[S any] struct {
	PropertyObject
	storage S
}

// NewArrayPair returns a new array with the elements a and b.
func (r *RealmPart) NewArrayPair(a, b Value) *Object {
	p := &pairArray{
		Kind:   KindArray,
		Class:  ClassArray,
		Pinned: PinnedElems,
		Shape:  r.Shapes.Array,
		Length: 2,
		pair:   [2]Value{a, b},
	}
	p.Elems = p.pair[:]
	return p.Init()
}

// stringTable holds the strings that are shared.
type stringTable struct {
	// Interned maps a string to its property key.
	Interned sweptTable[wtf8.String, Object]

	// keepNew is set while the agent and its built-in objects are created. The strings interned
	// meanwhile are kept alive with the agent.
	keepNew bool

	// Ascii are the strings of one ASCII character.
	Ascii [128]Value

	// Empty is the Empty string.
	Empty Value
}

// Init creates the empty string and the strings of one ASCII character with the shape
// stringShape. The table must be keeping new strings alive.
func (t *stringTable) Init(stringShape *Shape) {
	t.Interned.Entries = make(map[wtf8.String]sweptRef[Object], internedInitialSize)
	t.Empty = ObjectValue(t.add("", stringShape))
	for i := range t.Ascii {
		t.Ascii[i] = ObjectValue(t.add(wtf8.String(rune(i)), stringShape))
	}
}

// StartKeeping starts keeping newly interned strings alive with the agent, which EndKeeping ends.
func (t *stringTable) StartKeeping() {
	t.keepNew = true
}

// EndKeeping ends what StartKeeping started. The strings interned in between stay alive.
func (t *stringTable) EndKeeping() {
	t.keepNew = false
}

// Character returns the string of the ASCII Character c.
func (t *stringTable) Character(c byte) Value {
	return t.Ascii[c]
}

// lookup returns the property key of the string s, or nil if s is not interned.
func (t *stringTable) lookup(s wtf8.String) *Object {
	return t.Interned.get(s)
}

// add creates the property key of the string s, which must not be interned, with the shape
// stringShape.
func (t *stringTable) add(s wtf8.String, stringShape *Shape) *Object {
	o := &Object{
		Kind:  KindString,
		Flags: ObjInterned,
		Shape: stringShape,
		Text:  s,
	}
	if t.keepNew {
		o.Flags |= objKept
	}
	t.Interned.put(s, o, internedMinSweep, isKept)
	return o
}

// internedMinSweep is the number of interned strings below which they are kept alive with the
// agent.
const internedMinSweep = 1 << 10

// internedInitialSize is the number of interned strings that an agent has room for from the start,
// which is more than the built-ins need.
const internedInitialSize = 896

// NewString returns the string s as a value.
func (r *RealmPart) NewString(s wtf8.String) Value {
	switch len(s) {
	case 0:
		return r.Strings.Empty
	case 1:
		if s[0] < 0x80 {
			return r.Strings.Character(s[0])
		}
	}
	if len(s) <= shortstrings.MaxBytes {
		return r.ShortString(s)
	}
	return Value{
		O: &Object{
			Kind:  KindString,
			Shape: r.Shapes.String,
			Text:  s,
		},
	}
}

// SharedString returns the string s as a value that may be the same object as a property key
// with the same contents.
func (r *RealmPart) SharedString(s wtf8.String) Value {
	if o := r.Strings.lookup(s); o != nil {
		return ObjectValue(o)
	}
	return r.NewString(s)
}

// Intern returns the string s as a property key.
func (r *RealmPart) Intern(s wtf8.String) *Object {
	if o := r.Strings.lookup(s); o != nil {
		return o
	}
	return r.Strings.add(s, r.Shapes.String)
}

// isKept reports whether the interned string o is kept alive with its agent.
func isKept(o *Object) bool {
	return o.Flags&objKept != 0
}

// IsASCII reports whether the string o consists of ASCII characters only.
func (o *Object) IsASCII() bool {
	if o.Flags&ObjASCIIKnown == 0 {
		o.classifyASCII()
	}
	return o.Flags&ObjASCII != 0
}

// classifyASCII records in the flags of the string o whether it is ASCII.
// It stays out of line so that IsASCII can be inlined.
//
//go:noinline
func (o *Object) classifyASCII() {
	o.Flags |= ObjASCIIKnown
	if wtf8.IsASCII(o.Text) {
		o.Flags |= ObjASCII
	}
}

// StringLength returns the length of the string o in UTF-16 code units.
func (o *Object) StringLength() int {
	if o.IsASCII() {
		return len(o.Text)
	}
	if !o.cachesUTF16() {
		return wtf8.Len(o.Text)
	}
	m := o.ensureMeta()
	// A string that is not ASCII is never empty, so 0 means that the length is not known yet.
	if m.Length == 0 {
		m.Length = int32(wtf8.Len(o.Text))
	}
	return int(m.Length)
}

// cachesUTF16 reports whether the non-ASCII string o keeps its UTF-16 view in its side data.
func (o *Object) cachesUTF16() bool {
	if len(o.Text) > MaxStringLength {
		// Only a host can make such a string.
		return false
	}
	return o.Meta() != nil || len(o.Text) > maxUncachedLength
}

// maxUncachedLength is the length in bytes up to which a non-ASCII string without side data
// computes its UTF-16 view at each use.
const maxUncachedLength = 32

// ArrayIndex returns the array index whose canonical string form is the string o.
func (o *Object) ArrayIndex() (uint32, bool) {
	if i, ok := o.KnownArrayIndex(); ok {
		return i, true
	}
	i, ok := StringIndex(o.Str())
	if ok {
		o.Length = i + 1
	}
	return i, ok
}

// KnownArrayIndex returns the array index that the string o stands for, if it is known
// already. It reports false otherwise, even when o is an array index.
func (o *Object) KnownArrayIndex() (uint32, bool) {
	return o.Length - 1, o.Length != 0
}

// seek returns the position of the code unit i of the non-ASCII string o.
func (o *Object) seek(i int) wtf8.Pos {
	s := o.Str()
	if !o.cachesUTF16() {
		return wtf8.Seek(s, wtf8.Pos{}, i)
	}
	m := o.ensureMeta()
	from := m.Cursor.pos()
	if d := i - from.Unit; (d > checkpointInterval || d < -checkpointInterval) && i >= checkpointInterval {
		if m.checkpoints == nil && len(s) >= minCheckpointedLength {
			cs := checkpointsOf(s)
			m.checkpoints = &cs
		}
		if m.checkpoints != nil {
			if k := i / checkpointInterval; k < len(*m.checkpoints) {
				c := (*m.checkpoints)[k]
				from = wtf8.Pos{
					Unit: k * checkpointInterval,
					Byte: int(c >> 1),
					Low:  c&1 != 0,
				}
			}
		}
	}
	p := wtf8.Seek(s, from, i)
	m.Cursor = newStringPos(p)
	return p
}

const (
	// checkpointInterval is the distance in code units between two checkpoints of a string.
	checkpointInterval = 64

	// minCheckpointedLength is the length in bytes from which a string gets checkpoints.
	minCheckpointedLength = 4 * checkpointInterval
)

// checkpointsOf returns the positions of the code units of the non-ASCII string s whose
// indices are multiples of checkpointInterval, in order. A position is the byte of the code
// unit shifted left by one, with the low bit set when the code unit is the low surrogate of
// the four-byte sequence at that byte.
func checkpointsOf(s wtf8.String) []uint32 {
	cs := make([]uint32, 0, len(s)/(3*checkpointInterval)+1)
	var unit int
	for i := 0; i < len(s); {
		if unit%checkpointInterval == 0 {
			cs = append(cs, uint32(i)<<1)
		}
		switch b := s[i]; {
		case b < 0x80:
			i++
			unit++
		case b < 0xE0:
			i += 2
			unit++
		case b < 0xF0:
			i += 3
			unit++
		default:
			if (unit+1)%checkpointInterval == 0 {
				cs = append(cs, uint32(i)<<1|1)
			}
			i += 4
			unit += 2
		}
	}
	return cs
}

// UnitAt returns the code unit i of the string o. i must be in range.
func (o *Object) UnitAt(i int) uint16 {
	if o.IsASCII() {
		return o.asciiUnitAt(i)
	}
	return wtf8.At(o.Str(), o.seek(i))
}

func (o *Object) asciiUnitAt(i int) uint16 {
	if o.Flags&ObjReversed != 0 {
		// Reading one unit leaves a string that is being prepended to reversed.
		return uint16(o.Text[len(o.Text)-1-i])
	}
	return uint16(o.Text[i])
}

// CodePointAt returns the code point starting at UTF-16 index i. i must be in range.
func (o *Object) CodePointAt(i int) rune {
	if o.IsASCII() {
		return rune(o.asciiUnitAt(i))
	}
	s := o.Str()
	p := o.seek(i)
	if b := s[p.Byte]; b < 0x80 {
		return rune(b)
	}
	r, _ := wtf8.DecodeRune(s[p.Byte:])
	if p.Low {
		return 0xdc00 + (r-0x10000)&0x3ff
	}
	return r
}

// Substring returns the code units from start up to end of the string o.
// The indices must be in range and in order.
func (o *Object) Substring(start, end int) wtf8.String {
	s := o.Str()
	if o.IsASCII() {
		return s[start:end]
	}
	a := o.seek(start)
	b := wtf8.Seek(s, a, end)
	if o.cachesUTF16() {
		o.Meta().Cursor = newStringPos(b)
	}
	return wtf8.Slice(s, a, b)
}

// UnitString returns the string that consists of the code unit u.
func (r *RealmPart) UnitString(u uint16) Value {
	if u < 0x80 {
		return r.Strings.Character(byte(u))
	}
	var buf [utf8.UTFMax]byte
	return r.ShortString(wtf8.String(wtf8.AppendRune(buf[:0], rune(u))))
}

// getOwn returns the own named property key of o.
func (o *Object) getOwn(key *Object) (Prop, bool) {
	return o.Shape.Lookup(key)
}

// TouchProto records that the named properties of o changed, if o is a prototype: one was added,
// removed or defined, or its attributes changed. Whatever moves the slots of a prototype calls it
// too, because CacheProto entries point into them.
//
// A change of elements, of the length of an array or of the value of a data property does not need
// it. The caches of property lookups and the records of original built-in properties hold named
// properties in slots only. The operations in storage.go that depend on the elements of prototypes
// read them each time.
func (sp *Space) TouchProto(o *Object) {
	if o.Flags&ObjIsProto != 0 {
		sp.ProtoEpoch++
	}
}

// Add gives o the new own property key. The caller has checked that o has no such property.
func (sp *Space) Add(o *Object, key *Object, val Value, flags uint8) {
	s := o.Shape
	if !s.Private && (s.Count >= PrivateShapeThreshold || sp.Builtins.building) {
		s = s.privateCopy()
		o.Shape = s
		if sp.Builtins.building {
			sp.Builtins.Objects = append(sp.Builtins.Objects, o)
		}
	}
	if s.Private {
		// Growing the slots moves the addresses held by global variable caches, which
		// rely on the shape. No code runs while the built-ins are created.
		if s.renewOnAdd || len(o.Slots) == cap(o.Slots) && !sp.Builtins.building {
			s = s.handOver()
			o.Shape = s
		}
		s.Table[key] = Prop{
			Slot:  int32(len(s.Order)),
			Flags: flags,
		}
		s.Order = append(s.Order, key)
		s.Count++
	} else {
		o.Shape = s.With(key, flags)
	}
	o.AppendSlot(val)
	sp.TouchProto(o)
}

// builtinLayout is the state of the creation of the built-in objects.
type builtinLayout struct {
	// building is set between [Space.StartBuiltins] and [Space.FinishBuiltins].
	building bool

	// Objects are the Objects that got a private shape while building.
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
	s := o.Shape
	if !s.Private || o.Flags&ObjNotExtensible != 0 {
		return
	}
	o.compact()
	s = o.Shape
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

// Define creates or replaces the own named property key of o with the given attributes,
// without any of the checks of the JavaScript operations. An object of
// ExecutionContext.NewLazyObject must be initialized already.
func (r *RealmPart) Define(o *Object, key *Object, val Value, flags uint8) {
	if ValidateExec && o.Class == ClassLazy {
		panic("engine: property definition on an uninitialized lazy object")
	}
	if o.Flags&ObjExotic != 0 && o.Fn != nil {
		r.Materialize(o)
	}
	p, ok := o.Shape.Lookup(key)
	if !ok {
		r.Add(o, key, val, flags)
		return
	}
	if p.Flags != flags {
		r.setFlags(o, key, p, flags)
	}
	o.Slots[p.Slot] = val
	r.TouchProto(o)
}

// setFlags changes the attributes of the existing own property key of o.
func (sp *Space) setFlags(o *Object, key *Object, p Prop, flags uint8) {
	// An inline cache may remember the shape, so the changed layout needs another one.
	o.RenewShape()
	o.Shape.Table[key] = Prop{
		Slot:  p.Slot,
		Flags: flags,
	}
	sp.TouchProto(o)
}

// RenewShape gives o a private shape with the properties of its shape, which no inline cache
// has seen.
func (o *Object) RenewShape() {
	if o.Shape.Private {
		o.Shape = o.Shape.handOver()
	} else {
		o.Shape = o.Shape.privateCopy()
	}
}

// RemoveFromPrivate deletes the own named property key of o, which must exist, from the private
// shape of o. The new shape takes over the storage of the old one, which only o has.
func (sp *Space) RemoveFromPrivate(o *Object, key *Object) {
	old := o.Shape
	s := old.handOver()
	o.Shape = s
	s.Count--
	i := s.Table[key].Slot
	delete(s.Table, key)
	s.Order[i] = nil
	o.Slots[i] = Value{}
	// Trailing deleted slots go at once, and the others once they outnumber the keys, which
	// bounds the cost of compacting by that of the deletions.
	n := len(s.Order)
	for n > 0 && s.Order[n-1] == nil {
		n--
	}
	s.Order = s.Order[:n]
	o.truncateSlots(n)
	if n-int(s.Count) > int(s.Count) {
		o.compact()
	}
	sp.sparseIndices.remove(old, s, key)
	sp.TouchProto(o)
}

// compact removes the deleted slots of the private shape of o.
func (o *Object) compact() {
	s := o.Shape
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
		o.Slots[n] = o.Slots[i]
		n++
	}
	clear(s.Order[n:])
	s.Order = s.Order[:n]
	o.truncateSlots(n)
}

// SetProto changes the prototype of o. It stays out of line to keep ArrayCreate small.
//
//go:noinline
func (sp *Space) SetProto(o *Object, proto *Object) {
	if o.Shape.Proto != proto {
		sp.rebuildShape(o, proto, o.Shape.Family)
		sp.ProtoEpoch++
	}
}

// rebuildShape preserves the slots of o when its prototype or named property behavior changes.
func (sp *Space) rebuildShape(o *Object, proto *Object, family shapeFamily) {
	old := o.Shape
	if old.Private {
		s := old.privateCopy()
		s.Proto = proto
		s.Family = family
		if proto != nil {
			proto.Flags |= ObjIsProto
		}
		o.Shape = s
		return
	}
	s := sp.RootShape(proto, old.Realm, family)
	for _, k := range old.Keys() {
		p, _ := old.Lookup(k)
		s = s.With(k, p.Flags)
	}
	o.Shape = s
}

// MakePrivate gives o a shape of its own, so that no inline cache filled for another object applies to it.
func (o *Object) MakePrivate() {
	if !o.Shape.Private {
		o.Shape = o.Shape.privateCopy()
	}
}

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

// Owns reports whether o belongs to the agent of sp or is shared by all agents.
func (sp *Space) Owns(o *Object) bool {
	return o.Shape == nil || o.Shape.Space == sp
}

// HostDefined returns the host-defined value of the agent that owns o, or nil for shared values.
func (o *Object) HostDefined() any {
	if o.Shape == nil {
		return nil
	}
	return o.Shape.Space.HostDefinedValue
}

// Str returns the content of the string o in WTF-8.
func (o *Object) Str() wtf8.String {
	if o.Flags&ObjReversed != 0 {
		o.Unreverse()
	}
	return o.Text
}

// UnitIndexOf returns the index in code units of the first occurrence of sub in the string o
// at or after from, or -1. poll, which may be nil, is called now and then during a long search.
func (o *Object) UnitIndexOf(sub wtf8.String, from int, poll func()) int {
	s := o.Str()
	n := o.StringLength()
	if from > n {
		return -1
	}
	if sub == "" {
		return from
	}
	if o.IsASCII() {
		i := xstrings.Index(s[from:], sub, poll)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if mayMatchInsidePair(sub) {
		return o.findUnits(sub, from, false, poll)
	}
	p := o.seek(from)
	if p.Low {
		// This pattern can only start at a code point boundary.
		p = wtf8.Seek(s, p, from+1)
	}
	i := xstrings.Index(s[p.Byte:], sub, poll)
	if i < 0 {
		return -1
	}
	return p.Unit + wtf8.Len(s[p.Byte:p.Byte+i])
}

// UnitLastIndexOf returns the index in code units of the last occurrence of sub in the string o
// that starts at or before from, or -1. poll is as for [Object.UnitIndexOf].
func (o *Object) UnitLastIndexOf(sub wtf8.String, from int, poll func()) int {
	n := o.StringLength()
	subLen := wtf8.Len(sub)
	from = min(from, n-subLen)
	if from < 0 {
		return -1
	}
	if sub == "" {
		return from
	}
	if !o.IsASCII() && mayMatchInsidePair(sub) {
		return o.findUnits(sub, from, true, poll)
	}
	s := o.Str()
	if o.IsASCII() {
		return xstrings.LastIndex(s[:from+subLen], sub, poll)
	}
	end := o.seek(from + subLen)
	i := xstrings.LastIndex(s[:end.Byte], sub, poll)
	if i < 0 {
		return -1
	}
	// The match cannot end inside a pair, so the low surrogate of a pair at end stands for
	// the start of the pair.
	unit := end.Unit
	if end.Low {
		unit--
	}
	return unit - wtf8.Len(s[i:end.Byte])
}

func mayMatchInsidePair(sub wtf8.String) bool {
	// A canonical pattern can split a pair only at its outer boundaries.
	first, _ := wtf8.DecodeRune(sub)
	last, _ := wtf8.DecodeLastRune(sub)
	return wtf8.IsLowSurrogate(first) || wtf8.IsHighSurrogate(last)
}

// findUnitsPollSteps is the number of code units or bytes that findUnits reads between two calls
// of its poll function.
const findUnitsPollSteps = 1 << 16

// findUnits returns the start of the match of sub nearest to from in the given direction, or -1,
// comparing code units by the Knuth-Morris-Pratt algorithm.
func (o *Object) findUnits(sub wtf8.String, from int, backward bool, poll func()) int {
	var units []uint16
	rest := sub
	for len(rest) > findUnitsPollSteps {
		// A part ends before the first byte of a character.
		n := findUnitsPollSteps
		for n < len(rest) && rest[n]&0xC0 == 0x80 {
			n++
		}
		units = wtf8.AppendUTF16(units, rest[:n])
		rest = rest[n:]
		if poll != nil {
			poll()
		}
	}
	units = wtf8.AppendUTF16(units, rest)
	m := len(units)
	if backward {
		slices.Reverse(units)
	}
	// border[j] is the length of the longest proper border of units[:j+1].
	border := make([]int32, m)
	for j, k := 1, 0; j < m; j++ {
		if j%findUnitsPollSteps == 0 && poll != nil {
			poll()
		}
		for k > 0 && units[k] != units[j] {
			k = int(border[k-1])
		}
		if units[k] == units[j] {
			k++
		}
		border[j] = int32(k)
	}
	s := o.Str()
	n := o.StringLength()
	// i runs over the code units of o from from, or backwards from the last unit of a match
	// that starts at from.
	i, step := from, 1
	if backward {
		i, step = min(from+m, n)-1, -1
	}
	p := o.seek(i)
	for k, steps := 0, 0; i >= 0 && i < n; i += step {
		if steps++; steps%findUnitsPollSteps == 0 && poll != nil {
			poll()
		}
		p = wtf8.Seek(s, p, i)
		c := wtf8.At(s, p)
		for k > 0 && units[k] != c {
			k = int(border[k-1])
		}
		if units[k] == c {
			k++
		}
		if k == m {
			if backward {
				return i
			}
			return i - m + 1
		}
	}
	return -1
}

// DefineAccessor creates or replaces the own accessor property key of o with the given
// attributes. getter and setter may be nil.
func (r *RealmPart) DefineAccessor(o *Object, key *Object, getter, setter *Object, flags uint8) {
	r.Define(o, key, NewAccessor(getter, setter), flags|FlagAccessor)
}

// EndLazy makes the lazy object o an ordinary object without properties.
func (sp *Space) EndLazy(o *Object) {
	o.Class = ClassObject
	o.SetExtState(nil)
	o.Flags &^= ObjExotic
	sp.rebuildShape(o, o.Shape.Proto, OrdinaryRoot)
}

// MakeNonExtensible makes o refuse new properties.
func (o *Object) MakeNonExtensible() {
	o.Flags |= ObjNotExtensible
	// The inline caches that add properties are keyed by shared shapes.
	o.MakePrivate()
}

// FreezeLength makes the length of the array o read-only.
func (o *Object) FreezeLength() {
	o.Flags |= ObjFrozenLength
}

// NewTemplateArray returns a new frozen array with the elements vals, which still can be given
// named properties by [Space.Add].
func (r *RealmPart) NewTemplateArray(vals []Value) *Object {
	a := r.NewArray(nil)
	for i, v := range vals {
		r.Add(a, r.IndexName(uint32(i)), v, FlagEnumerable)
	}
	a.Length = uint32(len(vals))
	a.Flags |= ObjSparse | ObjFrozenLength | ObjNotExtensible
	return a
}

// SetIntrinsic makes o the intrinsic object id.
func (r *RealmPart) SetIntrinsic(id Intrinsic, o *Object) {
	r.Intrinsics[id] = o
}
