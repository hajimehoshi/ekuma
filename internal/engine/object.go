// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/big"
	"strings"

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

// Object flags.
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

	// classEvalScope is the scope of OpEvalScope, whose properties are the variables that
	// direct evals declare in a function.
	classEvalScope
)

// Object is every heap value: plain objects, arrays, functions, strings, symbols, and the singletons.
type Object struct {
	kind  kind
	class class
	flags uint8

	// length is the length of an array, which is at least len(elems).
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
	// and the entries in weak collections of an object or a symbol.
	meta *objectMeta

	// ext is the internal state of a built-in object. For a string that is not interned
	// it is the interned string with the same content, once that was looked up.
	ext any
}

// objectMeta is the side data of an object.
type objectMeta struct {
	// length and cursor are the cached UTF-16 view of a string that is not ASCII:
	// its length and the position of the most recent access.
	length int
	cursor wtf8.Pos

	// weak holds the values that the weak collections associate with an object or a symbol,
	// by collection. See WeakTable.
	weak weakEntries
}

// BigInt returns the content of the BigInt o, which must not be modified.
func (o *Object) BigInt() *big.Int {
	return o.ext.(*big.Int)
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
	// vm is the VM that the objects with this shape belong to.
	vm *VM

	proto  *Object
	parent *shape

	// key is the property this shape adds to parent, stored in slot count-1.
	// It is an interned string or a symbol.
	key   *Object
	flags uint8
	count int32

	// table maps every property of the shape to its slot. For a shared shape it is built on first lookup.
	table       map[*Object]prop
	transitions map[shapeKey]*shape

	private bool

	// order lists the keys of a private shape in insertion order.
	order []*Object
}

func (s *shape) lookup(key *Object) (prop, bool) {
	if s.table == nil {
		if s.count == 0 {
			return prop{}, false
		}
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

// keys returns the property keys in insertion order. The result must not be modified.
func (s *shape) keys() []*Object {
	if s.private {
		return s.order
	}
	keys := make([]*Object, s.count)
	for t := s; t.parent != nil; t = t.parent {
		keys[t.count-1] = t.key
	}
	return keys
}

// with returns the shared shape that has key in addition to the properties of s.
func (s *shape) with(key *Object, flags uint8) *shape {
	k := shapeKey{
		key:   key,
		flags: flags,
	}
	if t, ok := s.transitions[k]; ok {
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
	if s.transitions == nil {
		s.transitions = make(map[shapeKey]*shape)
	}
	s.transitions[k] = t
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

	// byProto maps a prototype to the empty shape of the objects with that prototype.
	byProto map[*Object]*shape

	// functionByProto maps a prototype to the shape of the functions with that prototype whose
	// initial properties are not stored yet.
	functionByProto map[*Object]*shape
}

// shapeFor returns the empty shape for objects whose prototype is proto, which may be nil.
func (vm *VM) shapeFor(proto *Object) *shape {
	if proto == nil {
		return vm.shapes.nullProto
	}
	if s, ok := vm.shapes.byProto[proto]; ok {
		return s
	}
	s := &shape{
		vm:    vm,
		proto: proto,
	}
	vm.shapes.byProto[proto] = s
	proto.flags |= objIsProto
	return s
}

// functionShapeFor returns the empty shape for the functions whose prototype is proto and whose
// initial properties are not stored yet. No other object has this shape or one derived from it,
// so an inline cache filled for another kind of object never applies to such a function.
func (vm *VM) functionShapeFor(proto *Object) *shape {
	if s, ok := vm.shapes.functionByProto[proto]; ok {
		return s
	}
	s := &shape{
		vm:    vm,
		proto: proto,
	}
	vm.shapes.functionByProto[proto] = s
	proto.flags |= objIsProto
	return s
}

// NewObject returns a new empty object with the prototype proto, which may be nil.
func (vm *VM) NewObject(proto *Object) *Object {
	return &Object{
		kind:  kindObject,
		shape: vm.shapeFor(proto),
	}
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

// pairArray is an array of two elements together with the storage of its elements,
// so that creating one is a single allocation.
type pairArray struct {
	Object
	pair [2]Value
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

// NewObjectFromProperties returns a new ordinary object whose prototype is Object.prototype,
// with the data properties keys[i]: vals[i] created in order as CreateDataProperty creates them.
// A repeated key keeps its first position and takes the last value. It does not keep the slices.
func (vm *VM) NewObjectFromProperties(keys []PropKey, vals []Value) *Object {
	o := &Object{
		kind:  kindObject,
		shape: vm.shapes.object,
		slots: make([]Value, 0, len(vals)),
	}
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
			if t, ok := s.transitions[shapeKey{
				key:   k.key,
				flags: FlagsData,
			}]; ok {
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
	interned map[string]*Object

	// ascii are the strings of one ASCII character.
	ascii [128]Value
}

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

// Intern returns the string s as a property key.
func (vm *VM) Intern(s string) *Object {
	if o, ok := vm.strings.interned[s]; ok {
		return o
	}
	o := &Object{
		kind:  kindString,
		flags: objInterned,
		shape: vm.shapes.string,
		str:   s,
	}
	vm.strings.interned[s] = o
	return o
}

// stringKey returns the property key for the string o.
func (vm *VM) stringKey(o *Object) *Object {
	if o.flags&objInterned != 0 {
		return o
	}
	if k, ok := o.ext.(*Object); ok {
		return k
	}
	k := vm.Intern(o.str)
	o.ext = k
	return k
}

// IsASCII reports whether the string o consists of ASCII characters only.
func (o *Object) IsASCII() bool {
	if o.flags&objASCIIKnown == 0 {
		o.flags |= objASCIIKnown
		if wtf8.IsASCII(o.str) {
			o.flags |= objASCII
		}
	}
	return o.flags&objASCII != 0
}

// StringLength returns the length of the string o in UTF-16 code units.
func (o *Object) StringLength() int {
	if o.IsASCII() {
		return len(o.str)
	}
	if o.meta == nil {
		o.meta = &objectMeta{length: wtf8.Len(o.str)}
	}
	return o.meta.length
}

// seek returns the position of the code unit i of the non-ASCII string o.
func (o *Object) seek(i int) wtf8.Pos {
	if o.meta == nil {
		o.meta = &objectMeta{length: wtf8.Len(o.str)}
	}
	p := wtf8.Seek(o.str, o.meta.cursor, i)
	o.meta.cursor = p
	return p
}

// UnitAt returns the code unit i of the string o. i must be in range.
func (o *Object) UnitAt(i int) uint16 {
	if o.IsASCII() {
		return uint16(o.str[i])
	}
	return wtf8.At(o.str, o.seek(i))
}

// Substring returns the code units from start up to end of the string o.
// The indices must be in range and in order.
func (o *Object) Substring(start, end int) string {
	if o.IsASCII() {
		return o.str[start:end]
	}
	a := o.seek(start)
	b := wtf8.Seek(o.str, a, end)
	o.meta.cursor = b
	return wtf8.Slice(o.str, a, b)
}

// UnitString returns the string that consists of the code unit u.
func (vm *VM) UnitString(u uint16) Value {
	if u < 0x80 {
		return vm.strings.ascii[u]
	}
	return Value{
		o: &Object{
			kind:  kindString,
			shape: vm.shapes.string,
			str:   string(wtf8.AppendRune(nil, rune(u))),
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
	if !s.private && s.count >= privateShapeThreshold {
		s = s.privateCopy()
		o.shape = s
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
	// A shared shape may be remembered by inline caches, so the changed layout needs another one.
	s := o.shape.privateCopy()
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

// VM returns the VM that o belongs to, or nil for a value that is shared by all VMs.
func (o *Object) VM() *VM {
	if o.shape == nil {
		return nil
	}
	return o.shape.vm
}

// Str returns the content of the string o in WTF-8.
func (o *Object) Str() string {
	return o.str
}

// UnitIndexOf returns the index in code units of the first occurrence of sub in the string o
// at or after from, or -1.
func (o *Object) UnitIndexOf(sub string, from int) int {
	n := o.StringLength()
	if from > n {
		return -1
	}
	if o.IsASCII() {
		i := strings.Index(o.str[from:], sub)
		if i < 0 {
			return -1
		}
		return from + i
	}
	p := o.seek(from)
	if p.Low {
		// A match cannot start in the middle of a pair unless sub starts with a lone low surrogate,
		// which never occurs inside a canonical string at that position.
		p = wtf8.Seek(o.str, p, from+1)
	}
	i := strings.Index(o.str[p.Byte:], sub)
	if i < 0 {
		return -1
	}
	return p.Unit + wtf8.Len(o.str[p.Byte:p.Byte+i])
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
	end := o.Substring(0, from+subLen)
	i := strings.LastIndex(end, sub)
	if i < 0 {
		return -1
	}
	return wtf8.Len(end[:i])
}

// DefineAccessor creates or replaces the own accessor property key of o with the given
// attributes. getter and setter may be nil.
func (vm *VM) DefineAccessor(o *Object, key *Object, getter, setter *Object, flags uint8) {
	vm.Define(o, key, newAccessor(getter, setter), flags|FlagAccessor)
}
