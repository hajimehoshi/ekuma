// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"maps"
	"slices"
	"strconv"
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
	// objInterned marks a string that is the unique representative of its contents,
	// so that it can serve as a property key compared by identity.
	objInterned uint8 = 1 << iota

	// objASCIIKnown says that objASCII is valid.
	objASCIIKnown
	objASCII

	// objNotExtensible marks an object that refuses new properties.
	objNotExtensible

	// objIsProto marks an object that is the prototype of some shape. Changing the properties
	// of such an object invalidates the inline caches that looked through it.
	objIsProto

	// objFrozenLength marks an array whose length is not writable.
	objFrozenLength

	// objSparse marks an object that has properties with array index keys outside elems:
	// they are in the shape under their canonical string keys. elems only ever holds
	// data properties that are writable, enumerable and configurable.
	objSparse

	// objExotic marks an object whose named properties cannot be found by looking at its shape alone.
	objExotic
)

// objReversed marks a string whose str holds its contents with the bytes in reverse order.
// Its bit is that of objFrozenLength, which only arrays and private names use. The byte length,
// the ASCII-ness and the UTF-16 length of str are those of the contents, so only a read of the
// contents themselves needs [Object.Str].
const objReversed = objFrozenLength

// objKept marks an interned string that the VM keeps alive for as long as it lives. Its bit is
// that of objIsProto, which strings never use.
const objKept = objIsProto

// class distinguishes the built-in objects that have internal state or exotic behavior.
type class uint8

const (
	classObject class = iota
	classArray
	classFunction
	classError
	classBoolean
	classNumber
	classString
	classSymbol
	classBigInt
	classArguments
	classDate
	classRegExp
	classMap
	classSet
	classWeakMap
	classWeakSet
	classWeakRef
	classFinalizationRegistry
	classArrayBuffer
	classTypedArray
	classDataView
	classPromise
	classGenerator
	classProxy
	classGlobalProxyHandler
	classIterator
	classForIn
	classPrivateName
	classModuleNamespace
	classRawJSON
	classDisposableStack
	classAsyncDisposableStack
	classTemporal
	classIntl
	classShadowRealm
	classModuleSource
	// classLazy is an ordinary object whose properties are not defined yet.
	classLazy

	// classEvalScope is the scope of OpEvalScope, whose properties are the variables that
	// direct evals declare in a function.
	classEvalScope
)

// Object is every heap value: plain objects, arrays, functions, strings, symbols, and the singletons.
type Object struct {
	kind  kind
	class class
	flags uint8

	// length is the length of an array, which is at least len(elems). For a string it is 0,
	// or the array index that the string stands for plus one. See [Object.arrayIndex].
	length uint32

	// shape is the layout of slots. Only the singletons and internal values have none.
	shape *shape

	// Primitive values have no property storage.
	*objectProperties

	// fn is set for a callable object. A closure must call ownFunction before changing its state.
	fn *function

	// str is the content of a string or the description of a symbol. A string marked objReversed
	// holds its bytes in reverse order. Packed-string sentinels use zero bytes to carry only the
	// byte length.
	str wtf8.String

	// ext is the internal state of a built-in object or the push size hint of a closure.
	// For a string that is not interned it is the interned string with the same content,
	// once that was looked up, or the state of concatenations onto it (see concat.go).
	// When the object has side data, ext is its *objectMeta, which holds the internal state
	// instead. See [Object.extState] and [Object.meta].
	ext any
}

// objectProperties holds the own properties of a JavaScript object.
type objectProperties struct {
	slots []Value

	// elems holds the properties whose keys are array indices, by index.
	// An element that does not exist holds hole.
	elems []Value
}

// propertyObject keeps an object's header and property storage in one allocation.
// Primitive heap values use only Object.
type propertyObject struct {
	Object
	objectProperties
}

func (o *propertyObject) init() *Object {
	o.Object.objectProperties = &o.objectProperties
	return &o.Object
}

// objectMeta is the side data that few objects need: what indexing a non-ASCII string needs,
// the entries in weak collections of an object or a symbol, and the empty shapes of the
// objects whose prototype is this object.
type objectMeta struct {
	// length and cursor are the cached UTF-16 view of a string that is not ASCII:
	// its length, or 0 before it is known, and the position of the most recent access.
	// Only a string of at most MaxStringLength bytes has them, so they fit in 32 bits.
	length int32
	cursor stringPos

	// checkpoints holds the positions of the code units of such a string whose indices are
	// multiples of checkpointInterval, once an access far from cursor needed one. See
	// [checkpointsOf].
	checkpoints *[]uint32

	// weak holds the values that the weak collections associate with an object or a symbol,
	// by collection. See [WeakTable].
	weak *weakEntries

	protoShapes prototypeShapeCache

	// Arrays have an implicit length and typed arrays have integer-indexed properties.
	// Their property caches require separate shapes even with the same prototype.
	indexedProtoShapes *indexedPrototypeShapes

	// functionProtoShape is the empty shape of the functions whose prototype is this object
	// and whose initial properties are not stored yet, once one was made.
	// See [VM.functionShapeFor].
	functionProtoShape *shape

	// shiftedElems is the number of unused values before the elements of an array in their
	// storage, left by removing the first elements. It counts only while the elements have the
	// capacity shiftedCap. See [Object.elemsStorage].
	shiftedElems int
	shiftedCap   int

	// ext is the internal state of the object that has this side data.
	ext any
}

// extState returns the internal state of o.
func (o *Object) extState() any {
	if m, ok := o.ext.(*objectMeta); ok {
		return m.ext
	}
	return o.ext
}

// setExtState sets the internal state of o to v.
func (o *Object) setExtState(v any) {
	if m, ok := o.ext.(*objectMeta); ok {
		m.ext = v
		return
	}
	o.ext = v
}

// extConcatCount returns the concatenation count in the internal state of the string o, or 0.
func (o *Object) extConcatCount() concatCount {
	if c, ok := o.ext.(concatCount); ok {
		return c
	}
	if m, ok := o.ext.(*objectMeta); ok {
		c, _ := m.ext.(concatCount)
		return c
	}
	return 0
}

// extObject returns the object in the internal state of o, and reports whether there is one.
func (o *Object) extObject() (*Object, bool) {
	if k, ok := o.ext.(*Object); ok {
		return k, true
	}
	if m, ok := o.ext.(*objectMeta); ok {
		k, ok := m.ext.(*Object)
		return k, ok
	}
	return nil, false
}

// meta returns the side data of o, or nil when it has none.
func (o *Object) meta() *objectMeta {
	m, _ := o.ext.(*objectMeta)
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
func (o *Object) ensureMeta() *objectMeta {
	if m := o.meta(); m != nil {
		return m
	}
	m := &objectMeta{ext: o.ext}
	o.ext = m
	return m
}

type prop struct {
	slot  int32
	flags uint8
}

type shapeKey struct {
	key   *Object
	flags uint8
}

// privateShapeThreshold is the number of properties above which an object stops sharing its shape.
const privateShapeThreshold = 128

// shape describes the layout of an object: which property lives in which slot, and the prototype.
//
// Objects with a common prototype in their creation realm that got the same properties in the
// same order share a Shape, until one has many properties or changes in a way other than adding one.
// Then it gets a private Shape, which is changed in place.
//
// A shared Shape never changes, so an inline cache that remembers one stays valid for as long
// as the prototypes it looked through keep their properties.
type shape struct {
	// vm and realm identify where the objects with this shape were created.
	vm    *VM
	realm *Realm

	proto  *Object
	parent *shape

	// key is the property this shape adds to parent, stored in slot count-1.
	// It is an interned string or a symbol.
	key   *Object
	flags uint8

	// enumerable tells whether a shared shape has an enumerable property whose key is a
	// string, once that is known: one of the enumerable* values.
	enumerable uint8

	count int32

	// table maps every property of the shape to its slot. A private shape always has one.
	// A shared shape with more than smallShapeSize properties builds it on first lookup,
	// or takes it over from its parent.
	table map[*Object]prop

	// transitions maps a property to the shared shape that adds it to this one.
	transitions transitionTable

	// small lists the properties of a shared shape with at most smallShapeSize properties,
	// by slot. It is nil for any other shape.
	small []shapeEntry

	private bool

	// renewOnAdd is set on a private shape when a cache relies on a key being absent.
	// Adding a property must then give the object a new shape.
	renewOnAdd bool

	// flatBased is set for a shared shape without parent that has properties of its own
	// (see [VM.FinishBuiltins]) and for the shapes that add properties to it. Such a shape with
	// more than smallShapeSize properties keeps a table from the start.
	flatBased bool

	// branchSlots is, for a shared shape with more than one transition, the largest number of
	// properties that objects were given after its own, up to maxInstanceSlots.
	branchSlots uint8

	// readID identifies this shape in its VM's megamorphic read caches. Zero is unassigned.
	readID uint32

	// order lists the keys of a private shape in insertion order, at the indices of their
	// slots. A deleted key leaves nil behind until the shape is compacted, so count is the number
	// of keys and len(order) the number of slots.
	order []*Object

	// deletion is the result of the last deletion of a property from a shared shape, or nil.
	deletion *deletion
}

// deletion is the shared shape that deleting the property key from a shared shape gives.
type deletion struct {
	key   *Object
	shape *shape
}

// shapeEntry is a property of a shape and where it is.
type shapeEntry struct {
	key *Object
	prop
}

// transitionTable maps a property to the shared shape that adds it to a shape.
// The zero value is an empty table.
type transitionTable struct {
	// only is the shape of the sole transition while there is only one, and nil otherwise.
	// Its key and flags are the key of the transition.
	only *shape

	// rest holds the transitions once there is more than one.
	rest sweptTable[shapeKey, shape]
}

// get returns the shape under k, or nil.
func (t *transitionTable) get(k shapeKey) *shape {
	if s := t.only; s != nil {
		if s.key == k.key && s.flags == k.flags {
			return s
		}
		return nil
	}
	return t.rest.get(k)
}

// put adds s under the key that s adds to its parent, which the table must not have.
func (t *transitionTable) put(s *shape) {
	if t.only == nil && t.rest.entries == nil {
		t.only = s
		return
	}
	if only := t.only; only != nil {
		t.only = nil
		t.rest.put(shapeKey{
			key:   only.key,
			flags: only.flags,
		}, only, transitionsMinSweep, nil)
	}
	t.rest.put(shapeKey{
		key:   s.key,
		flags: s.flags,
	}, s, transitionsMinSweep, nil)
}

// empty reports whether no transition was ever added.
func (t *transitionTable) empty() bool {
	return t.only == nil && t.rest.entries == nil
}

// len returns the number of entries.
func (t *transitionTable) len() int {
	if t.only != nil {
		return 1
	}
	return len(t.rest.entries)
}

// transitionsMinSweep is the number of transitions from one shape below which they are kept
// alive with the shape.
const transitionsMinSweep = 8

// smallShapeSize is the number of properties up to which a shared shape is searched linearly.
const smallShapeSize = 8

func (s *shape) lookup(key *Object) (prop, bool) {
	if s.small == nil {
		if s.table == nil {
			s.table = make(map[*Object]prop, s.count)
			for t := s; t.parent != nil; t = t.parent {
				s.table[t.key] = prop{
					slot:  t.count - 1,
					flags: t.flags,
				}
			}
		}
		p, ok := s.table[key]
		return p, ok
	}
	for _, e := range s.small {
		if e.key == key {
			return e.prop, true
		}
	}
	return prop{}, false
}

// keys returns the property keys in insertion order. The result must not be modified.
func (s *shape) keys() []*Object {
	if s.private {
		if len(s.order) != int(s.count) {
			return slices.DeleteFunc(slices.Clone(s.order), func(k *Object) bool {
				return k == nil
			})
		}
		return s.order
	}
	keys := make([]*Object, s.count)
	t := s
	for ; t.parent != nil; t = t.parent {
		keys[t.count-1] = t.key
	}
	// A shared shape without parent may have properties of its own: see [VM.FinishBuiltins].
	for _, e := range t.small {
		keys[e.slot] = e.key
	}
	for k, p := range t.table {
		keys[p.slot] = k
	}
	return keys
}

// stringKeys returns the property keys of s that are strings, in insertion order.
func (s *shape) stringKeys() []PropKey {
	if s.private {
		keys := make([]PropKey, 0, len(s.order))
		for _, k := range s.order {
			if k != nil && k.kind == kindString {
				keys = append(keys, PropKey{key: k})
			}
		}
		return keys
	}
	keys := make([]PropKey, s.count)
	t := s
	for ; t.parent != nil; t = t.parent {
		keys[t.count-1] = PropKey{key: t.key}
	}
	for _, e := range t.small {
		keys[e.slot] = PropKey{key: e.key}
	}
	for k, p := range t.table {
		keys[p.slot] = PropKey{key: k}
	}
	return slices.DeleteFunc(keys, PropKey.IsSymbol)
}

// Values of shape.enumerable.
const (
	enumerableUnknown uint8 = iota
	enumerableNone
	enumerableSome
)

// hasEnumerableString reports whether s has an enumerable property whose key is a string.
func (s *shape) hasEnumerableString() bool {
	if s.private {
		for k, p := range s.table {
			if isEnumerableString(k, p.flags) {
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
	return s.enumerable == enumerableSome
}

// sharedHasEnumerableString reports whether the shared shape s has an enumerable property
// whose key is a string.
func (s *shape) sharedHasEnumerableString() bool {
	t := s
	for ; t.parent != nil; t = t.parent {
		if isEnumerableString(t.key, t.flags) {
			return true
		}
	}
	// A shared shape without parent may have properties of its own: see [VM.FinishBuiltins].
	for _, e := range t.small {
		if isEnumerableString(e.key, e.flags) {
			return true
		}
	}
	for k, p := range t.table {
		if isEnumerableString(k, p.flags) {
			return true
		}
	}
	return false
}

// with returns the shared shape that has key in addition to the properties of s.
func (s *shape) with(key *Object, flags uint8) *shape {
	k := shapeKey{
		key:   key,
		flags: flags,
	}
	if t := s.transitions.get(k); t != nil {
		return t
	}
	t := &shape{
		vm:     s.vm,
		realm:  s.realm,
		proto:  s.proto,
		parent: s,
		key:    key,
		flags:  flags,
		count:  s.count + 1,
	}
	if t.count <= smallShapeSize {
		t.small = append(make([]shapeEntry, 0, t.count), s.small...)
		t.small = append(t.small, shapeEntry{
			key:   key,
			slot:  s.count,
			flags: flags,
		})
	}
	switch {
	case s.flatBased:
		t.flatBased = true
		if t.small == nil {
			// The chain of t does not list the properties of its base, so t cannot build its
			// table from the chain.
			t.table = make(map[*Object]prop, t.count)
			maps.Copy(t.table, s.table)
			for _, e := range s.small {
				t.table[e.key] = e.prop
			}
			t.table[key] = prop{
				slot:  s.count,
				flags: flags,
			}
		}
	case s.table != nil && s.transitions.empty() && !s.private:
		// An object that gains properties one by one looks each intermediate shape up once, so the
		// first child takes over the table, and s builds a new one only if it is looked up again.
		t.table, s.table = s.table, nil
		t.table[key] = prop{
			slot:  s.count,
			flags: flags,
		}
	}
	s.transitions.put(t)
	d := 1
	for p := s; p != nil && d <= maxInstanceSlots; p = p.parent {
		if p.transitions.only == nil {
			p.branchSlots = max(p.branchSlots, uint8(d))
		}
		d++
	}
	return t
}

// maxReadded is the largest number of properties after a deleted one for which the deletion
// gives a shared shape.
const maxReadded = 16

// without returns the shared shape with the properties of the shared shape s other than key, in
// the same order and with the same attributes. It returns nil when the chain of s does not add
// key or adds more than maxReadded properties after it.
func (s *shape) without(key *Object) *shape {
	if d := s.deletion; d != nil && d.key == key {
		return d.shape
	}
	// The properties after key, from the last one. Each can make a shape that the tree of shapes
	// did not have yet.
	var buf [maxReadded]shapeKey
	later := buf[:0]
	p := s
	for p != nil && p.key != key {
		if len(later) == maxReadded {
			return nil
		}
		later = append(later, shapeKey{
			key:   p.key,
			flags: p.flags,
		})
		p = p.parent
	}
	if p == nil {
		return nil
	}
	t := p.parent
	for _, k := range slices.Backward(later) {
		t = t.with(k.key, k.flags)
	}
	if s.deletion == nil {
		s.deletion = &deletion{}
	}
	s.deletion.key, s.deletion.shape = key, t
	return t
}

// privateCopy returns a private shape with the properties of s in the same slots.
func (s *shape) privateCopy() *shape {
	order := s.order
	if !s.private {
		order = s.keys()
	}
	t := &shape{
		vm:      s.vm,
		realm:   s.realm,
		proto:   s.proto,
		count:   s.count,
		table:   make(map[*Object]prop, s.count+8),
		private: true,
		order:   slices.Clone(order),
	}
	for _, k := range t.order {
		if k != nil {
			p, _ := s.lookup(k)
			t.table[k] = p
		}
	}
	return t
}

// handOver returns a new private shape with the properties of the private shape s, which
// it takes from s. Only the object that has s may call it.
func (s *shape) handOver() *shape {
	t := &shape{
		vm:      s.vm,
		realm:   s.realm,
		proto:   s.proto,
		count:   s.count,
		table:   s.table,
		private: true,
		order:   s.order,
	}
	s.table = nil
	s.order = nil
	return t
}

// accessor is the content of the slot of an accessor property.
func newAccessor(getter, setter *Object) Value {
	return Value{
		o: &Object{
			kind: kindAccessor,
			ext: &accessorPair{
				getter: getter,
				setter: setter,
			},
		},
	}
}

type accessorPair struct {
	getter *Object
	setter *Object
}

func (v Value) accessor() *accessorPair {
	return v.o.ext.(*accessorPair)
}

// shapeTable holds the shapes that new objects start with.
type shapeTable struct {
	// shortStrings are immutable sentinels. Per-string caches belong to materialized objects.
	shortStrings   *[shortstrings.MaxBytes + 1]Object
	nullProto      *shape
	nullArray      *shape
	nullTypedArray *shape
	object         *shape
	array          *shape
	function       *shape
	string         *shape
	iterResult     *shape
	arguments      *shape

	// regExp is the shape of a new RegExp object whose prototype is %RegExp.prototype%,
	// once one was made.
	regExp *shape

	// proxy is the shape of a new proxy object. It has no prototype, so that a lookup in shapes
	// stops at a proxy, and only ever gains private names.
	proxy *shape
}

// newShape returns a new empty shared shape for objects whose prototype is proto.
func (vm *VM) newShape(proto *Object, r *Realm) *shape {
	if proto != nil {
		proto.flags |= objIsProto
	}
	return &shape{
		vm:    vm,
		realm: r,
		proto: proto,
		small: []shapeEntry{},
	}
}

// shapeFor returns the empty shape for objects of the current realm with prototype proto, which may be nil.
func (vm *VM) shapeFor(proto *Object) *shape {
	return vm.shapeForRealm(proto, vm.curRealm)
}

// shapeForRealm returns the empty shape for objects of r with prototype proto, which may be nil.
func (vm *VM) shapeForRealm(proto *Object, r *Realm) *shape {
	if proto == nil {
		return r.shapes.nullProto
	}
	return proto.ensureMeta().protoShapes.forRealm(vm, proto, r)
}

type prototypeShapeCache struct {
	local *shape
	// A prototype can outlive the realm that created an object with it.
	foreign weak.Pointer[shape]
}

type indexedPrototypeShapes struct {
	array, typedArray prototypeShapeCache
}

func (c *prototypeShapeCache) forRealm(vm *VM, proto *Object, r *Realm) *shape {
	if proto.shape.realm != r {
		if s := c.foreign.Value(); s != nil && s.realm == r {
			return s
		}
		s := vm.newShape(proto, r)
		c.foreign = weak.Make(s)
		return s
	}
	// The shape is stored on proto, so that it and the shapes derived from it are collected
	// together with proto.
	if c.local == nil {
		c.local = vm.newShape(proto, r)
	}
	return c.local
}

// objectRootShape returns the initial layout for an object of class cls.
func (vm *VM) objectRootShape(proto *Object, r *Realm, cls class) *shape {
	if cls != classArray && cls != classTypedArray {
		return vm.shapeForRealm(proto, r)
	}
	if proto == nil {
		s := &r.shapes.nullArray
		if cls == classTypedArray {
			s = &r.shapes.nullTypedArray
		}
		if *s == nil {
			*s = vm.newShape(nil, r)
		}
		return *s
	}
	m := proto.ensureMeta()
	if m.indexedProtoShapes == nil {
		m.indexedProtoShapes = &indexedPrototypeShapes{}
	}
	if cls == classArray {
		return m.indexedProtoShapes.array.forRealm(vm, proto, r)
	}
	return m.indexedProtoShapes.typedArray.forRealm(vm, proto, r)
}

// functionShapeFor returns the empty shape for the functions whose prototype is proto and whose
// initial properties are not stored yet. No other object has this shape or one derived from it,
// so an inline cache filled for another kind of object never applies to such a function.
// proto must be an intrinsic prototype of the current realm.
func (vm *VM) functionShapeFor(proto *Object) *shape {
	m := proto.ensureMeta()
	if m.functionProtoShape == nil {
		m.functionProtoShape = vm.newShape(proto, vm.curRealm)
	}
	return m.functionProtoShape
}

// NewObject returns a new empty object with the prototype proto, which may be nil.
func (vm *VM) NewObject(proto *Object) *Object {
	return newObject(vm.shapeFor(proto))
}

func newObject(s *shape) *Object {
	return (&propertyObject{
		kind:  kindObject,
		shape: s,
	}).init()
}

// NewInstance returns a new empty ordinary instance with prototype proto, which may be nil.
func (vm *VM) NewInstance(proto *Object) *Object {
	return newInstance(vm.shapeFor(proto))
}

// maxInstanceSlots bounds the room for properties that newInstance gives an object.
const maxInstanceSlots = 16

// branchedInstanceSlots is the room for properties that newInstance gives an object when
// the objects with its shape have been given different properties.
const branchedInstanceSlots = 4

// newInstance returns a new empty ordinary object with the shape s, with room for the
// properties that the objects with s have been given one after another so far, and for some
// more when they went on with different properties.
func newInstance(s *shape) *Object {
	n := 0
	for t := s; n < maxInstanceSlots; t = t.transitions.only {
		if t.transitions.only == nil {
			switch {
			case t.transitions.empty():
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
	o, slots := newObjectWithStorage(min(n, maxInstanceSlots))
	o.kind = kindObject
	o.shape = s
	o.slots = slots[:0]
	return o
}

// NewOrdinaryObject returns a new empty ordinary object whose prototype is %Object.prototype%.
func (vm *VM) NewOrdinaryObject() *Object {
	return (&propertyObject{
		kind:  kindObject,
		shape: vm.curRealm.shapes.object,
	}).init()
}

// NewArray returns a new array with the given elements, which it keeps.
func (vm *VM) NewArray(elems []Value) *Object {
	return vm.curRealm.newArray(elems)
}

func (r *Realm) newArray(elems []Value) *Object {
	return (&propertyObject{
		kind:   kindArray,
		class:  classArray,
		shape:  r.shapes.array,
		elems:  elems,
		length: uint32(len(elems)),
	}).init()
}

// NewArrayCopy returns a new array with a copy of elems as its elements.
func (vm *VM) NewArrayCopy(elems []Value) *Object {
	a, storage := newObjectWithStorage(len(elems))
	a.kind = kindArray
	a.class = classArray
	a.shape = vm.curRealm.shapes.array
	a.elems = storage
	copy(a.elems, elems)
	a.length = uint32(len(elems))
	return a
}

// NewEmptyArrayWithRoom returns a new empty array with room for n elements.
func (vm *VM) NewEmptyArrayWithRoom(n int) *Object {
	a, storage := newObjectWithStorage(n)
	a.kind = kindArray
	a.class = classArray
	a.shape = vm.curRealm.shapes.array
	a.elems = storage[:0]
	return a
}

// pairArray is an array of two elements together with the storage of its elements,
// so that creating one is a single allocation.
type pairArray struct {
	propertyObject
	pair [2]Value
}

// closureObject is a function object together with its function state.
type closureObject struct {
	propertyObject
	state function
}

// closureObjectWith is a closureObject together with the storage of its upvalues and of the
// upvalues that hold copies of variables.
type closureObjectWith[U, C any] struct {
	closureObject

	// copies comes first so that an empty C adds no padding at the end.
	copies C
	upvals U
}

// newClosure returns a fresh function object and the storage for its copied upvalues.
func newClosure(p *funcProto) (*Object, []upvalue) {
	if len(p.Upvals) == 0 && !p.IsMethod && !p.isClassConstructor {
		if p.plainFunction == nil {
			p.plainFunction = &function{
				code:        p,
				constructor: p.isConstructor,
				shared:      true,
			}
		}
		return (&propertyObject{
			fn: p.plainFunction,
		}).init(), nil
	}
	o, copies := newClosureObject(len(p.Upvals), p.ncopied)
	o.fn.code = p
	o.fn.constructor = p.isConstructor
	o.fn.classConstructor = p.isClassConstructor
	o.fn.derived = p.isDerived
	return o, copies
}

// ownFunction gives o its own function state before an instance-specific change.
func (o *Object) ownFunction() *function {
	if o.fn.shared {
		f := *o.fn
		f.shared = false
		o.fn = &f
	}
	return o.fn
}

// newClosureObject returns a zero object whose function state has n nil upvalues, and
// storage for the ncopied of them that hold copies of variables. Small closures are a single
// allocation.
func newClosureObject(n, ncopied int) (*Object, []upvalue) {
	var c *closureObject
	var upvals []*upvalue
	var copies []upvalue
	switch n<<2 | ncopied {
	case 0:
		c = &closureObject{}
	case 1 << 2:
		w := &closureObjectWith[[1]*upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 1<<2 | 1:
		w := &closureObjectWith[[1]*upvalue, [1]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2 << 2:
		w := &closureObjectWith[[2]*upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 2<<2 | 1:
		w := &closureObjectWith[[2]*upvalue, [1]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 2<<2 | 2:
		w := &closureObjectWith[[2]*upvalue, [2]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3 << 2:
		w := &closureObjectWith[[3]*upvalue, struct{}]{}
		c, upvals = &w.closureObject, w.upvals[:]
	case 3<<2 | 1:
		w := &closureObjectWith[[3]*upvalue, [1]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 2:
		w := &closureObjectWith[[3]*upvalue, [2]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	case 3<<2 | 3:
		w := &closureObjectWith[[3]*upvalue, [3]upvalue]{}
		c, upvals, copies = &w.closureObject, w.upvals[:], w.copies[:]
	default:
		// The capacities fill an allocation size class.
		switch {
		case n <= 5 && ncopied == 0:
			w := &closureObjectWith[[5]*upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 7 && ncopied == 0:
			w := &closureObjectWith[[7]*upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 11 && ncopied == 0:
			w := &closureObjectWith[[11]*upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied == 0:
			w := &closureObjectWith[[15]*upvalue, struct{}]{}
			c, upvals = &w.closureObject, w.upvals[:n:n]
		case n <= 15 && ncopied > 4 && ncopied <= 8:
			w := &closureObjectWith[[15]*upvalue, [8]upvalue]{}
			c, upvals, copies = &w.closureObject, w.upvals[:n:n], w.copies[:ncopied:ncopied]
		default:
			c = &closureObject{}
			upvals = make([]*upvalue, n)
			if ncopied > 0 {
				copies = make([]upvalue, ncopied)
			}
		}
	}
	c.state.upvals = upvals
	c.fn = &c.state
	return c.init(), copies
}

// newObjectWithStorage returns a zero object and storage for n values. For a small n, both are
// a single allocation.
func newObjectWithStorage(n int) (*Object, []Value) {
	switch n {
	case 0:
		return (&propertyObject{}).init(), nil
	case 1:
		w := &objectWith[[1]Value]{}
		return w.init(), w.storage[:]
	case 2:
		w := &objectWith[[2]Value]{}
		return w.init(), w.storage[:]
	case 3:
		w := &objectWith[[3]Value]{}
		return w.init(), w.storage[:]
	case 4:
		w := &objectWith[[4]Value]{}
		return w.init(), w.storage[:]
	case 5:
		w := &objectWith[[5]Value]{}
		return w.init(), w.storage[:]
	case 6:
		w := &objectWith[[6]Value]{}
		return w.init(), w.storage[:]
	case 7:
		w := &objectWith[[7]Value]{}
		return w.init(), w.storage[:]
	case 8:
		w := &objectWith[[8]Value]{}
		return w.init(), w.storage[:]
	}
	return (&propertyObject{}).init(), make([]Value, n)
}

// objectWith is an object together with storage for its slots or elements.
type objectWith[S any] struct {
	propertyObject
	storage S
}

// NewArrayPair returns a new array with the elements a and b.
func (vm *VM) NewArrayPair(a, b Value) *Object {
	return vm.NewArrayPairInRealm(vm.CurrentRealm(), a, b)
}

// NewArrayPairInRealm returns an array with the elements a and b in realm.
func (vm *VM) NewArrayPairInRealm(realm *Realm, a, b Value) *Object {
	p := &pairArray{
		kind:   kindArray,
		class:  classArray,
		shape:  realm.shapes.array,
		length: 2,
		pair:   [2]Value{a, b},
	}
	p.elems = p.pair[:]
	return p.init()
}

// NewArrayWithRoom returns an array of length undefined elements with room for n named properties.
func (vm *VM) NewArrayWithRoom(length, n int) *Object {
	a, storage := newObjectWithStorage(length + n)
	a.kind = kindArray
	a.class = classArray
	a.shape = vm.curRealm.shapes.array
	// Appending elements must not overwrite the named properties.
	a.elems = storage[:length:length]
	a.length = uint32(length)
	a.slots = storage[length:length]
	return a
}

// NewObjectFromProperties returns a new ordinary object whose prototype is Object.prototype,
// with the data properties keys[i]: vals[i] created in order as CreateDataProperty creates them.
// A repeated key keeps its first position and takes the last value. It does not keep the slices.
func (vm *VM) NewObjectFromProperties(keys []PropKey, vals []Value) *Object {
	o, slots := newObjectWithStorage(len(vals))
	o.kind = kindObject
	o.shape = vm.curRealm.shapes.object
	o.slots = slots[:0]
	for i, k := range keys {
		v := vals[i]
		if k.key == nil {
			if int64(k.idx) < int64(len(o.elems)) && !o.elems[k.idx].isHole() {
				o.elems[k.idx] = v
				continue
			}
			if o.flags&objSparse != 0 {
				if key := vm.indexKey(k.idx); key != nil {
					if _, ok := o.shape.lookup(key); ok {
						vm.Define(o, key, v, FlagsData)
						continue
					}
				}
			}
			vm.addIndex(o, k.idx, v, FlagsData)
			continue
		}
		// A transition exists only for a key that the shape does not have.
		if s := o.shape; !s.private {
			if t := s.transitions.get(shapeKey{
				key:   k.key,
				flags: FlagsData,
			}); t != nil {
				o.shape = t
				o.slots = append(o.slots, v)
				continue
			}
		}
		vm.Define(o, k.key, v, FlagsData)
	}
	return o
}

// stringTable holds the strings that are shared.
type stringTable struct {
	// interned maps a string to its property key.
	interned sweptTable[wtf8.String, Object]

	// keepNew is set while the VM and its built-in objects are created. The strings interned
	// meanwhile are kept alive with the VM.
	keepNew bool

	// ascii are the strings of one ASCII character.
	ascii [128]Value
}

// indexStringCount is the number of the smallest array indices whose strings are shared.
const indexStringCount = 1024

// indexString returns the string of the array index i.
func (vm *VM) indexString(i uint32) Value {
	if i < 10 {
		return vm.strings.ascii['0'+i]
	}
	if i >= indexStringCount {
		return vm.newIndexString(i)
	}
	t := vm.indexStrings
	if int(i) >= len(t) {
		t = append(t, make([]*Object, int(i)+1-len(t))...)
		t = t[:cap(t)]
		vm.indexStrings = t
	}
	if o := t[i]; o != nil {
		return ObjectValue(o)
	}
	v := vm.newIndexString(i)
	o := v.stringObject()
	o.length = i + 1
	t[i] = o
	return v
}

// newIndexString returns a new string of the array index i, which is at least 10.
func (vm *VM) newIndexString(i uint32) Value {
	v := vm.NewString(wtf8.String(strconv.FormatUint(uint64(i), 10)))
	if !v.isShortString() {
		v.o.length = i + 1
	}
	return v
}

// internedMinSweep is the number of interned strings below which they are kept alive with the
// VM.
const internedMinSweep = 1 << 10

// internedInitialSize is the number of interned strings that a VM has room for from the start,
// which is more than the built-ins need.
const internedInitialSize = 896

// NewString returns the string s as a value.
func (vm *VM) NewString(s wtf8.String) Value {
	switch len(s) {
	case 0:
		return vm.emptyString
	case 1:
		if s[0] < 0x80 {
			return vm.strings.ascii[s[0]]
		}
	}
	if len(s) <= shortstrings.MaxBytes {
		return vm.shortString(s)
	}
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.curRealm.shapes.string,
			str:   s,
		},
	}
}

// SharedString returns the string s as a value that may be the same object as a property key
// with the same contents.
func (vm *VM) SharedString(s wtf8.String) Value {
	if o := vm.strings.interned.get(s); o != nil {
		return ObjectValue(o)
	}
	return vm.NewString(s)
}

// Intern returns the string s as a property key.
func (vm *VM) Intern(s wtf8.String) *Object {
	if o := vm.strings.interned.get(s); o != nil {
		return o
	}
	o := &Object{
		kind:  kindString,
		flags: objInterned,
		shape: vm.curRealm.shapes.string,
		str:   s,
	}
	if vm.strings.keepNew {
		o.flags |= objKept
	}
	vm.strings.interned.put(s, o, internedMinSweep, isKept)
	return o
}

// isKept reports whether the interned string o is kept alive with its VM.
func isKept(o *Object) bool {
	return o.flags&objKept != 0
}

// stringKey returns the property key for the string o.
func (vm *VM) stringKey(o *Object) *Object {
	if o.flags&objInterned != 0 {
		return o
	}
	if k, ok := o.extObject(); ok {
		return k
	}
	k := vm.Intern(o.Str())
	o.setExtState(k)
	return k
}

// IsASCII reports whether the string o consists of ASCII characters only.
func (o *Object) IsASCII() bool {
	if o.flags&objASCIIKnown == 0 {
		o.classifyASCII()
	}
	return o.flags&objASCII != 0
}

// classifyASCII records in the flags of the string o whether it is ASCII.
// It stays out of line so that IsASCII can be inlined.
//
//go:noinline
func (o *Object) classifyASCII() {
	o.flags |= objASCIIKnown
	if wtf8.IsASCII(o.str) {
		o.flags |= objASCII
	}
}

// StringLength returns the length of the string o in UTF-16 code units.
func (o *Object) StringLength() int {
	if o.IsASCII() {
		return len(o.str)
	}
	if !o.cachesUTF16() {
		return wtf8.Len(o.str)
	}
	m := o.ensureMeta()
	// A string that is not ASCII is never empty, so 0 means that the length is not known yet.
	if m.length == 0 {
		m.length = int32(wtf8.Len(o.str))
	}
	return int(m.length)
}

// cachesUTF16 reports whether the non-ASCII string o keeps its UTF-16 view in its side data.
func (o *Object) cachesUTF16() bool {
	if len(o.str) > MaxStringLength {
		// Only a host can make such a string.
		return false
	}
	return o.meta() != nil || len(o.str) > maxUncachedLength
}

// maxUncachedLength is the length in bytes up to which a non-ASCII string without side data
// computes its UTF-16 view at each use.
const maxUncachedLength = 32

// arrayIndex returns the array index whose canonical string form is the string o.
func (o *Object) arrayIndex() (uint32, bool) {
	if i, ok := o.knownArrayIndex(); ok {
		return i, true
	}
	i, ok := stringIndex(o.Str())
	if ok {
		o.length = i + 1
	}
	return i, ok
}

// knownArrayIndex returns the array index that the string o stands for, if it is known
// already. It reports false otherwise, even when o is an array index.
func (o *Object) knownArrayIndex() (uint32, bool) {
	return o.length - 1, o.length != 0
}

// seek returns the position of the code unit i of the non-ASCII string o.
func (o *Object) seek(i int) wtf8.Pos {
	s := o.Str()
	if !o.cachesUTF16() {
		return wtf8.Seek(s, wtf8.Pos{}, i)
	}
	m := o.ensureMeta()
	from := m.cursor.pos()
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
	m.cursor = newStringPos(p)
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
	if o.flags&objReversed != 0 {
		// Reading one unit leaves a string that is being prepended to reversed.
		return uint16(o.str[len(o.str)-1-i])
	}
	return uint16(o.str[i])
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
		o.meta().cursor = newStringPos(b)
	}
	return wtf8.Slice(s, a, b)
}

// UnitString returns the string that consists of the code unit u.
func (vm *VM) UnitString(u uint16) Value {
	if u < 0x80 {
		return vm.strings.ascii[u]
	}
	var buf [utf8.UTFMax]byte
	return vm.shortString(wtf8.String(wtf8.AppendRune(buf[:0], rune(u))))
}

// getOwn returns the own named property key of o.
func (o *Object) getOwn(key *Object) (prop, bool) {
	return o.shape.lookup(key)
}

// find looks key up on o and its prototype chain.
func find(o *Object, key *Object) (holder *Object, p prop, ok bool) {
	for h := o; h != nil; h = h.shape.proto {
		if p, ok := h.shape.lookup(key); ok {
			return h, p, true
		}
	}
	return nil, prop{}, false
}

// touchProto records that the properties of o changed, if o is a prototype. Whatever moves the
// slots of a prototype calls it too, because cacheProto entries point into them.
func (vm *VM) touchProto(o *Object) {
	if o.flags&objIsProto != 0 {
		vm.protoEpoch++
	}
}

// add gives o the new own property key. The caller has checked that o has no such property.
func (vm *VM) add(o *Object, key *Object, val Value, flags uint8) {
	s := o.shape
	if !s.private && (s.count >= privateShapeThreshold || vm.builtins.building) {
		s = s.privateCopy()
		o.shape = s
		if vm.builtins.building {
			vm.builtins.objects = append(vm.builtins.objects, o)
		}
	}
	if s.private {
		// Growing the slots moves the addresses held by global variable caches, which
		// rely on the shape. No code runs while the built-ins are created.
		if s.renewOnAdd || len(o.slots) == cap(o.slots) && !vm.builtins.building {
			s = s.handOver()
			o.shape = s
		}
		s.table[key] = prop{
			slot:  int32(len(s.order)),
			flags: flags,
		}
		s.order = append(s.order, key)
		s.count++
	} else {
		o.shape = s.with(key, flags)
	}
	o.slots = append(o.slots, val)
	vm.touchProto(o)
}

// builtinLayout is the state of the creation of the built-in objects.
type builtinLayout struct {
	// building is set between [VM.StartBuiltins] and [VM.FinishBuiltins].
	building bool

	// objects are the objects that got a private shape while building.
	objects []*Object
}

// StartBuiltins starts the creation of the built-in objects, which [VM.FinishBuiltins] ends.
// In between, objects should only get their initial properties.
func (vm *VM) StartBuiltins() {
	vm.builtins.building = true
	vm.strings.keepNew = true
}

// FinishBuiltins ends the creation of the built-in objects.
func (vm *VM) FinishBuiltins() {
	for _, o := range vm.builtins.objects {
		o.shareShape()
	}
	vm.builtins = builtinLayout{}
	vm.strings.keepNew = false
}

// shareShape turns the private shape of o into a shared shape without parent that holds all
// the properties itself, so that inline caches apply to o. The shape must not change afterwards.
func (o *Object) shareShape() {
	s := o.shape
	if !s.private || o.flags&objNotExtensible != 0 {
		return
	}
	o.compact()
	s = o.shape
	// The keys of a private shape are in the order of their slots.
	s.private = false
	s.flatBased = true
	order := s.order
	s.order = nil
	if s.count <= smallShapeSize {
		s.small = make([]shapeEntry, len(order))
		for i, k := range order {
			s.small[i] = shapeEntry{
				key:  k,
				prop: s.table[k],
			}
		}
		s.table = nil
	}
}

// Define creates or replaces the own named property key of o with the given attributes,
// without any of the checks of the JavaScript operations.
func (vm *VM) Define(o *Object, key *Object, val Value, flags uint8) {
	if o.flags&objExotic != 0 {
		vm.resolve(o)
	}
	p, ok := o.shape.lookup(key)
	if !ok {
		vm.add(o, key, val, flags)
		return
	}
	if p.flags != flags {
		vm.setFlags(o, key, p, flags)
	}
	o.slots[p.slot] = val
	vm.touchProto(o)
}

// setFlags changes the attributes of the existing own property key of o.
func (vm *VM) setFlags(o *Object, key *Object, p prop, flags uint8) {
	// An inline cache may remember the shape, so the changed layout needs another one.
	o.renewShape()
	o.shape.table[key] = prop{
		slot:  p.slot,
		flags: flags,
	}
	vm.touchProto(o)
}

// renewShape gives o a private shape with the properties of its shape, which no inline cache
// has seen.
func (o *Object) renewShape() {
	if o.shape.private {
		o.shape = o.shape.handOver()
	} else {
		o.shape = o.shape.privateCopy()
	}
}

// remove deletes the own named property key of o, which must exist.
func (vm *VM) remove(o *Object, key *Object) {
	old := o.shape
	if old.private {
		vm.removeFromPrivate(o, key)
		return
	}
	// The shared shape without key has the later properties in the slots after the one of key,
	// so their values move down by one.
	if s := old.without(key); s != nil {
		p, _ := old.lookup(key)
		n := len(o.slots)
		copy(o.slots[p.slot:], o.slots[p.slot+1:])
		o.slots[n-1] = Value{}
		o.slots = o.slots[:n-1]
		o.shape = s
		vm.touchProto(o)
		return
	}
	// Otherwise removing a property moves the later ones, so the object gets a shape that no
	// inline cache has seen.
	s := &shape{
		vm:      old.vm,
		realm:   old.realm,
		proto:   old.proto,
		table:   make(map[*Object]prop, old.count),
		private: true,
	}
	slots := make([]Value, 0, len(o.slots))
	for _, k := range old.keys() {
		if k == key {
			continue
		}
		q, _ := old.lookup(k)
		s.table[k] = prop{
			slot:  s.count,
			flags: q.flags,
		}
		s.order = append(s.order, k)
		s.count++
		slots = append(slots, o.slots[q.slot])
	}
	o.shape = s
	o.slots = slots
	vm.touchProto(o)
}

// removeFromPrivate deletes the own named property key of o, which must exist, from the private
// shape of o. The new shape takes over the storage of the old one, which only o has.
func (vm *VM) removeFromPrivate(o *Object, key *Object) {
	old := o.shape
	s := old.handOver()
	o.shape = s
	s.count--
	i := s.table[key].slot
	delete(s.table, key)
	s.order[i] = nil
	o.slots[i] = Value{}
	// Trailing deleted slots go at once, and the others once they outnumber the keys, which
	// bounds the cost of compacting by that of the deletions.
	n := len(s.order)
	for n > 0 && s.order[n-1] == nil {
		n--
	}
	clear(o.slots[n:])
	s.order = s.order[:n]
	o.slots = o.slots[:n]
	if n-int(s.count) > int(s.count) {
		o.compact()
	}
	vm.sparseIndices.remove(old, s, key)
	vm.touchProto(o)
}

// compact removes the deleted slots of the private shape of o.
func (o *Object) compact() {
	s := o.shape
	if !s.private || len(s.order) == int(s.count) {
		return
	}
	n := 0
	for i, k := range s.order {
		if k == nil {
			continue
		}
		p := s.table[k]
		p.slot = int32(n)
		s.table[k] = p
		s.order[n] = k
		o.slots[n] = o.slots[i]
		n++
	}
	clear(s.order[n:])
	clear(o.slots[n:])
	s.order = s.order[:n]
	o.slots = o.slots[:n]
}

// setProto changes the prototype of o.
func (vm *VM) setProto(o *Object, proto *Object) {
	old := o.shape
	if old.proto == proto {
		return
	}
	if old.private {
		s := old.privateCopy()
		s.proto = proto
		if proto != nil {
			proto.flags |= objIsProto
		}
		o.shape = s
	} else {
		var s *shape
		if o.flags&objExotic != 0 && o.class != classArray && o.class != classTypedArray {
			s = vm.newShape(proto, old.realm)
		} else {
			s = vm.objectRootShape(proto, old.realm, o.class)
		}
		for _, k := range old.keys() {
			p, _ := old.lookup(k)
			s = s.with(k, p.flags)
		}
		o.shape = s
	}
	vm.protoEpoch++
}

// makePrivate gives o a shape of its own, so that no inline cache filled for another object applies to it.
func (o *Object) makePrivate() {
	if !o.shape.private {
		o.shape = o.shape.privateCopy()
	}
}

// Kinds of inline cache entries.
const (
	cacheNone uint8 = iota

	// cacheOwn: the property is in slot of the receiver.
	cacheOwn

	// cacheProto: ref points at the property, which is in the slots of an object on the
	// prototype chain.
	cacheProto

	// cacheGetter and cacheSetter: holder is the function to call.
	cacheGetter
	cacheSetter

	// cacheOwnGetter: the property is an accessor in slot of the receiver, whose getter is called.
	cacheOwnGetter

	// cacheAdd: a store adds the property and moves the receiver to the shape next.
	cacheAdd
)

// cacheEntry is what an inline cache remembers about one receiver shape.
type cacheEntry struct {
	// shape is the shape of the receiver the entry is valid for.
	shape *shape
	kind  uint8
	slot  int32

	holder *Object

	// epoch is the value of VM.protoEpoch the entry was filled at.
	epoch uint32

	next *shape

	ref *Value
}

// setInherited records a lookup that depends on the receiver having no own property of that name.
func (e *cacheEntry) setInherited(s *shape, kind uint8, epoch uint32) {
	e.shape = s
	e.kind = kind
	e.epoch = epoch
	if s.private {
		s.renewOnAdd = true
	}
}

// propCache holds two inline entries for a property access site and records additional
// receiver shapes for named reads.
type propCache struct {
	cacheEntry
	second cacheEntry
	reads  *namedReads
}

// entry returns the inline entry for s, or nil on a miss.
func (c *propCache) entry(s *shape) *cacheEntry {
	if c.shape == s {
		return &c.cacheEntry
	}
	if c.second.shape == s {
		return &c.second
	}
	return nil
}

// namedReads holds the locations of properties read at a site with several receiver shapes.
// Its entries do not keep the shapes, their prototypes or their property values alive.
type namedReads struct {
	vm     *VM
	epoch  uint32
	shapes map[uint32]namedRead
}

type namedRead struct {
	// depth counts the prototype links to the holder. count detects properties added
	// to the receiver while it keeps its private shape.
	depth int32
	count int32
	prop
}

// globalCache is the inline cache of an instruction that accesses a global variable. holder is
// the global object of the realm, or nil. While holder has the shape shape, ref points at the
// value of the variable, which is initialized and, for an instruction that assigns, writable.
//
// The global object gets another shape whenever a global lexical binding is declared, and
// whenever its own properties or the storage of their values change (see [VM.add] and
// [VM.densify]).
type globalCache struct {
	shape  *shape
	ref    *Value
	holder *Object
}

// keep saves the current entry before c is filled for shape. It reports whether this
// displaces an entry for a third distinct shape.
func (c *propCache) keep(shape *shape) bool {
	if c.shape == nil || c.shape == shape {
		return false
	}
	previous := c.second.shape
	c.second = c.cacheEntry
	return previous != nil && previous != shape && previous != c.shape
}

// VM returns the [VM] that o belongs to, or nil for a value that is shared by all VMs.
func (o *Object) VM() *VM {
	if o.shape == nil {
		return nil
	}
	return o.shape.vm
}

// Str returns the content of the string o in WTF-8.
func (o *Object) Str() wtf8.String {
	if o.flags&objReversed != 0 {
		o.unreverse()
	}
	return o.str
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

// findUnitsPollSteps is the number of code units that findUnits reads between two calls of its
// poll function.
const findUnitsPollSteps = 1 << 16

// findUnits returns the start of the match of sub nearest to from in the given direction, or -1,
// comparing code units by the Knuth-Morris-Pratt algorithm.
func (o *Object) findUnits(sub wtf8.String, from int, backward bool, poll func()) int {
	units := wtf8.AppendUTF16(nil, sub)
	m := len(units)
	if backward {
		slices.Reverse(units)
	}
	// border[j] is the length of the longest proper border of units[:j+1].
	border := make([]int32, m)
	for j, k := 1, 0; j < m; j++ {
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
func (vm *VM) DefineAccessor(o *Object, key *Object, getter, setter *Object, flags uint8) {
	vm.Define(o, key, newAccessor(getter, setter), flags|FlagAccessor)
}
