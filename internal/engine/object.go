// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
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
	slots []Value

	// elems holds the properties whose keys are array indices, by index.
	// An element that does not exist holds hole.
	elems []Value

	// fn is set for a callable object.
	fn *function

	// str is the content of a string or the description of a symbol.
	str string

	// meta is the side data that few objects need: what indexing a non-ASCII string needs,
	// the entries in weak collections of an object or a symbol, and the empty shapes of
	// the objects whose prototype is this object.
	meta *objectMeta

	// ext is the internal state of a built-in object. For a string that is not interned
	// it is the interned string with the same content, once that was looked up.
	ext any
}

// objectMeta is the side data of an object.
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
	weak weakEntries

	// protoShape is the empty shape of the objects whose prototype is this object, once one
	// was made. See [VM.shapeFor].
	protoShape *shape

	// functionProtoShape is the empty shape of the functions whose prototype is this object
	// and whose initial properties are not stored yet, once one was made.
	// See [VM.functionShapeFor].
	functionProtoShape *shape
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
	if o.meta == nil {
		o.meta = &objectMeta{}
	}
	return o.meta
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
// Objects that got the same properties in the same order from the same prototype share a Shape,
// until an object has many properties or changes in a way other than adding one.
// Then it gets a private Shape, which is changed in place.
//
// A shared Shape never changes, so an inline cache that remembers one stays valid for as long
// as the prototypes it looked through keep their properties.
type shape struct {
	// vm is the [VM] that the objects with this shape belong to.
	vm *VM

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

	// flatBased is set for a shared shape without parent that has properties of its own
	// (see [VM.FinishBuiltins]) and for the shapes that add properties to it. Such a shape with
	// more than smallShapeSize properties keeps a table from the start.
	flatBased bool

	// order lists the keys of a private shape in insertion order.
	order []*Object
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
			if k.kind == kindString {
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
	return t
}

// privateCopy returns a private shape with the properties of s.
func (s *shape) privateCopy() *shape {
	t := &shape{
		vm:      s.vm,
		proto:   s.proto,
		count:   s.count,
		table:   make(map[*Object]prop, s.count+8),
		private: true,
		order:   append([]*Object(nil), s.keys()...),
	}
	for _, k := range t.order {
		p, _ := s.lookup(k)
		t.table[k] = p
	}
	return t
}

// handOver returns a new private shape with the properties of the private shape s, which
// it takes from s. Only the object that has s may call it.
func (s *shape) handOver() *shape {
	t := &shape{
		vm:      s.vm,
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
	nullProto  *shape
	object     *shape
	array      *shape
	function   *shape
	string     *shape
	iterResult *shape
	arguments  *shape

	// regExp is the shape of a new RegExp object whose prototype is %RegExp.prototype%,
	// once one was made.
	regExp *shape

	// proxy is the shape of a new proxy object. It has no prototype, so that a lookup in shapes
	// stops at a proxy, and only ever gains private names.
	proxy *shape
}

// newShape returns a new empty shared shape for objects whose prototype is proto.
func (vm *VM) newShape(proto *Object) *shape {
	return &shape{
		vm:    vm,
		proto: proto,
		small: []shapeEntry{},
	}
}

// shapeFor returns the empty shape for objects whose prototype is proto, which may be nil.
func (vm *VM) shapeFor(proto *Object) *shape {
	if proto == nil {
		return vm.shapes.nullProto
	}
	// The shape is stored on proto, so that it and the shapes derived from it are collected
	// together with proto.
	m := proto.ensureMeta()
	if m.protoShape == nil {
		m.protoShape = vm.newShape(proto)
		proto.flags |= objIsProto
	}
	return m.protoShape
}

// functionShapeFor returns the empty shape for the functions whose prototype is proto and whose
// initial properties are not stored yet. No other object has this shape or one derived from it,
// so an inline cache filled for another kind of object never applies to such a function.
// proto must not be nil.
func (vm *VM) functionShapeFor(proto *Object) *shape {
	m := proto.ensureMeta()
	if m.functionProtoShape == nil {
		m.functionProtoShape = vm.newShape(proto)
		proto.flags |= objIsProto
	}
	return m.functionProtoShape
}

// NewObject returns a new empty object with the prototype proto, which may be nil.
func (vm *VM) NewObject(proto *Object) *Object {
	return &Object{
		kind:  kindObject,
		shape: vm.shapeFor(proto),
	}
}

// maxInstanceSlots bounds the room for properties that newInstance gives an object.
const maxInstanceSlots = 16

// branchedInstanceSlots is the room for properties that newInstance gives an object when
// the objects with its shape have been given different properties.
const branchedInstanceSlots = 4

// newInstance returns a new empty ordinary object with the shape s, with room for the
// properties that the objects with s have been given one after another so far.
func newInstance(s *shape) *Object {
	n := 0
	if s.transitions.only == nil && !s.transitions.empty() {
		// The prototype of a subclass also starts with s.
		n = branchedInstanceSlots
	}
	for t := s.transitions.only; t != nil && n < maxInstanceSlots; t = t.transitions.only {
		n++
	}
	o, slots := newObjectWithStorage(n)
	o.kind = kindObject
	o.shape = s
	o.slots = slots[:0]
	return o
}

// NewOrdinaryObject returns a new empty ordinary object whose prototype is %Object.prototype%.
func (vm *VM) NewOrdinaryObject() *Object {
	return &Object{
		kind:  kindObject,
		shape: vm.shapes.object,
	}
}

// NewArray returns a new array with the given elements, which it keeps.
func (vm *VM) NewArray(elems []Value) *Object {
	return &Object{
		kind:   kindArray,
		class:  classArray,
		shape:  vm.shapes.array,
		elems:  elems,
		length: uint32(len(elems)),
	}
}

// NewArrayCopy returns a new array with a copy of elems as its elements.
func (vm *VM) NewArrayCopy(elems []Value) *Object {
	a, storage := newObjectWithStorage(len(elems))
	a.kind = kindArray
	a.class = classArray
	a.shape = vm.shapes.array
	a.elems = storage
	copy(a.elems, elems)
	a.length = uint32(len(elems))
	return a
}

// pairArray is an array of two elements together with the storage of its elements,
// so that creating one is a single allocation.
type pairArray struct {
	Object
	pair [2]Value
}

// closureObject is a function object together with its function state.
type closureObject struct {
	Object
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
		c = &closureObject{}
		upvals = make([]*upvalue, n)
		if ncopied > 0 {
			copies = make([]upvalue, ncopied)
		}
	}
	c.state.upvals = upvals
	c.fn = &c.state
	return &c.Object, copies
}

// newObjectWithStorage returns a zero object and storage for n values. For a small n, both are
// a single allocation.
func newObjectWithStorage(n int) (*Object, []Value) {
	switch n {
	case 0:
		return &Object{}, nil
	case 1:
		w := &objectWith[[1]Value]{}
		return &w.Object, w.storage[:]
	case 2:
		w := &objectWith[[2]Value]{}
		return &w.Object, w.storage[:]
	case 3:
		w := &objectWith[[3]Value]{}
		return &w.Object, w.storage[:]
	case 4:
		w := &objectWith[[4]Value]{}
		return &w.Object, w.storage[:]
	}
	return &Object{}, make([]Value, n)
}

// objectWith is an object together with storage for its slots or elements.
type objectWith[S any] struct {
	Object
	storage S
}

// NewArrayPair returns a new array with the elements a and b.
func (vm *VM) NewArrayPair(a, b Value) *Object {
	p := &pairArray{
		kind:   kindArray,
		class:  classArray,
		shape:  vm.shapes.array,
		length: 2,
		pair:   [2]Value{a, b},
	}
	p.elems = p.pair[:]
	return &p.Object
}

// roomyArray is an array together with the storage of a few named properties,
// so that creating one and adding them is a single allocation.
type roomyArray struct {
	Object
	props [4]Value
}

// NewArrayWithRoom returns a new array with the elements elems, which it keeps,
// that has room for n named properties.
func (vm *VM) NewArrayWithRoom(elems []Value, n int) *Object {
	if n > len(roomyArray{}.props) {
		a := vm.NewArray(elems)
		a.slots = make([]Value, 0, n)
		return a
	}
	a := &roomyArray{
		kind:   kindArray,
		class:  classArray,
		shape:  vm.shapes.array,
		elems:  elems,
		length: uint32(len(elems)),
	}
	a.slots = a.props[:0]
	return &a.Object
}

// NewObjectFromProperties returns a new ordinary object whose prototype is Object.prototype,
// with the data properties keys[i]: vals[i] created in order as CreateDataProperty creates them.
// A repeated key keeps its first position and takes the last value. It does not keep the slices.
func (vm *VM) NewObjectFromProperties(keys []PropKey, vals []Value) *Object {
	o, slots := newObjectWithStorage(len(vals))
	o.kind = kindObject
	o.shape = vm.shapes.object
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
	interned sweptTable[string, Object]

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
		return Value{o: o}
	}
	v := vm.newIndexString(i)
	t[i] = v.o
	return v
}

// newIndexString returns a new string of the array index i, which is at least 10.
func (vm *VM) newIndexString(i uint32) Value {
	v := vm.NewString(strconv.FormatUint(uint64(i), 10))
	v.o.length = i + 1
	return v
}

// internedMinSweep is the number of interned strings below which they are kept alive with the
// VM.
const internedMinSweep = 1 << 10

// internedInitialSize is the number of interned strings that a VM has room for from the start,
// which is more than the built-ins need.
const internedInitialSize = 896

// NewString returns the string s, which must be in canonical WTF-8, as a value.
func (vm *VM) NewString(s string) Value {
	if len(s) <= 1 {
		if s == "" {
			return vm.emptyString
		}
		if c := s[0]; c < 0x80 {
			return vm.strings.ascii[c]
		}
	}
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   s,
		},
	}
}

// SharedString returns the string s, which must be in canonical WTF-8, as a value that may be
// the same object as a property key with the same contents.
func (vm *VM) SharedString(s string) Value {
	if o := vm.strings.interned.get(s); o != nil {
		return Value{o: o}
	}
	return vm.NewString(s)
}

// Intern returns the string s as a property key.
func (vm *VM) Intern(s string) *Object {
	if o := vm.strings.interned.get(s); o != nil {
		return o
	}
	o := &Object{
		kind:  kindString,
		flags: objInterned,
		shape: vm.shapes.string,
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
	if k, ok := o.ext.(*Object); ok {
		return k
	}
	k := vm.Intern(o.Str())
	o.ext = k
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
	return o.meta != nil || len(o.str) > maxUncachedLength
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
func checkpointsOf(s string) []uint32 {
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
	s := o.Str()
	if o.IsASCII() {
		return uint16(s[i])
	}
	return wtf8.At(s, o.seek(i))
}

// Substring returns the code units from start up to end of the string o.
// The indices must be in range and in order.
func (o *Object) Substring(start, end int) string {
	s := o.Str()
	if o.IsASCII() {
		return s[start:end]
	}
	a := o.seek(start)
	b := wtf8.Seek(s, a, end)
	if o.cachesUTF16() {
		o.meta.cursor = newStringPos(b)
	}
	return wtf8.Slice(s, a, b)
}

// UnitString returns the string that consists of the code unit u.
func (vm *VM) UnitString(u uint16) Value {
	if u < 0x80 {
		return vm.strings.ascii[u]
	}
	var buf [utf8.UTFMax]byte
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   string(wtf8.AppendRune(buf[:0], rune(u))),
		},
	}
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

// touchProto records that the properties of o changed, if o is a prototype.
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
		s.table[key] = prop{
			slot:  s.count,
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
	var s *shape
	if o.shape.private {
		s = o.shape.handOver()
	} else {
		s = o.shape.privateCopy()
	}
	s.table[key] = prop{
		slot:  p.slot,
		flags: flags,
	}
	o.shape = s
	vm.touchProto(o)
}

// remove deletes the own named property key of o, which must exist.
func (vm *VM) remove(o *Object, key *Object) {
	// Removing a property moves the later ones, so the object gets a shape that no inline cache has seen.
	old := o.shape
	if old.private {
		vm.removeFromPrivate(o, key)
		return
	}
	s := &shape{
		vm:      old.vm,
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
	s := o.shape.handOver()
	s.count--
	// The keys of a private shape are in the order of their slots.
	i := int(s.table[key].slot)
	delete(s.table, key)
	last := len(s.order) - 1
	copy(s.order[i:], s.order[i+1:])
	s.order[last] = nil
	s.order = s.order[:last]
	for _, k := range s.order[i:] {
		p := s.table[k]
		p.slot--
		s.table[k] = p
	}
	copy(o.slots[i:], o.slots[i+1:])
	o.slots[last] = Value{}
	o.slots = o.slots[:last]
	o.shape = s
	vm.touchProto(o)
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
		s := vm.shapeFor(proto)
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

	// cacheProto: the property is in slot of holder, which is on the prototype chain.
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
}

// propCache is the inline cache of one property access site. It remembers two receiver shapes,
// because code that is shared by objects of two classes is common.
type propCache struct {
	cacheEntry
	second cacheEntry
}

// keep saves the current entry as the second one before c is filled for another shape.
func (c *propCache) keep(shape *shape) {
	if c.shape != nil && c.shape != shape {
		c.second = c.cacheEntry
	}
}

// VM returns the [VM] that o belongs to, or nil for a value that is shared by all VMs.
func (o *Object) VM() *VM {
	if o.shape == nil {
		return nil
	}
	return o.shape.vm
}

// Str returns the content of the string o in WTF-8.
func (o *Object) Str() string {
	if o.flags&objReversed != 0 {
		o.unreverse()
	}
	return o.str
}

// UnitIndexOf returns the index in code units of the first occurrence of sub in the string o
// at or after from, or -1.
func (o *Object) UnitIndexOf(sub string, from int) int {
	s := o.Str()
	n := o.StringLength()
	if from > n {
		return -1
	}
	if sub == "" {
		return from
	}
	if o.IsASCII() {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return -1
		}
		return from + i
	}
	if mayMatchInsidePair(sub) {
		return o.findUnits(sub, from, false)
	}
	p := o.seek(from)
	if p.Low {
		// This pattern can only start at a code point boundary.
		p = wtf8.Seek(s, p, from+1)
	}
	i := strings.Index(s[p.Byte:], sub)
	if i < 0 {
		return -1
	}
	return p.Unit + wtf8.Len(s[p.Byte:p.Byte+i])
}

// UnitLastIndexOf returns the index in code units of the last occurrence of sub in the string o
// that starts at or before from, or -1.
func (o *Object) UnitLastIndexOf(sub string, from int) int {
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
		return o.findUnits(sub, from, true)
	}
	s := o.Str()
	if o.IsASCII() {
		return strings.LastIndex(s[:from+subLen], sub)
	}
	end := o.seek(from + subLen)
	i := strings.LastIndex(s[:end.Byte], sub)
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

func mayMatchInsidePair(sub string) bool {
	// A canonical pattern can split a pair only at its outer boundaries.
	first, _ := wtf8.DecodeRune(sub)
	last, _ := wtf8.DecodeLastRune(sub)
	return wtf8.IsLowSurrogate(first) || wtf8.IsHighSurrogate(last)
}

// findUnits returns the nearest match to from in the given direction, or -1.
func (o *Object) findUnits(sub string, from int, backward bool) int {
	units := wtf8.AppendUTF16(nil, sub)
	last := o.StringLength() - len(units)
	step := 1
	if backward {
		step = -1
	}
	s := o.Str()
	p := o.seek(from)
	for i := from; i >= 0 && i <= last; i += step {
		p = wtf8.Seek(s, p, i)
		q := p
		matched := true
		for j, unit := range units {
			q = wtf8.Seek(s, q, i+j)
			if wtf8.At(s, q) != unit {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

// DefineAccessor creates or replaces the own accessor property key of o with the given
// attributes. getter and setter may be nil.
func (vm *VM) DefineAccessor(o *Object, key *Object, getter, setter *Object, flags uint8) {
	vm.Define(o, key, newAccessor(getter, setter), flags|FlagAccessor)
}
