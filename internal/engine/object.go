// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/big"

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
	ObjNotExtensible

	// objIsProto marks an object that is the prototype of some shape. Changing the properties
	// of such an object invalidates the inline caches that looked through it.
	objIsProto

	// objFrozenLength marks an array whose length is not writable.
	ObjFrozenLength

	// objSparse marks an object that has properties with array index keys outside elems:
	// they are in the shape under their canonical string keys. elems only ever holds
	// data properties that are writable, enumerable and configurable.
	ObjSparse

	// objExotic marks an object whose named properties cannot be found by looking at its shape alone.
	ObjExotic
)

// class distinguishes the built-in objects that have internal state or exotic behavior.
type class uint8

const (
	classObject class = iota
	classArray
	ClassFunction
	ClassError
	ClassBoolean
	ClassNumber
	ClassString
	ClassSymbol
	classBigInt
	ClassArguments
	ClassDate
	ClassRegExp
	classMap
	classSet
	classWeakMap
	classWeakSet
	classWeakRef
	classArrayBuffer
	classTypedArray
	classDataView
	classPromise
	classGenerator
	classProxy
	ClassIterator
	classForIn
	ClassPrivateName
	classModuleNamespace
)

// Object is every heap value: plain objects, arrays, functions, strings, symbols, and the singletons.
type Object struct {
	Kind  kind
	Class class
	Flags uint8

	// length is the length of an array, which is at least len(elems).
	Length uint32

	// shape is the layout of slots. Only the singletons and internal values have none.
	Shape *Shape
	Slots []Value

	// elems holds the properties whose keys are array indices, by index.
	// An element that does not exist holds hole.
	Elems []Value

	// fn is set for a callable object.
	Fn *Function

	// str is the content of a string or the description of a symbol.
	Str string

	// meta caches what indexing a non-ASCII string needs.
	meta *stringMeta

	// ext is the internal state of a built-in object. For a string that is not interned
	// it is the interned string with the same content, once that was looked up.
	Ext any
}

// stringMeta is the cached UTF-16 view of a string that is not ASCII.
type stringMeta struct {
	length int

	// cursor is the position of the most recent access.
	cursor wtf8.Pos
}

func (o *Object) BigInt() *big.Int {
	return o.Ext.(*big.Int)
}

type prop struct {
	Slot  int32
	Flags uint8
}

type shapeKey struct {
	key   *Object
	flags uint8
}

// privateShapeThreshold is the number of properties above which an object stops sharing its shape.
const privateShapeThreshold = 128

// Shape describes the layout of an object: which property lives in which slot, and the prototype.
//
// Objects that got the same properties in the same order from the same prototype share a Shape,
// until an object has many properties or changes in a way other than adding one.
// Then it gets a private Shape, which is changed in place.
//
// A shared Shape never changes, so an inline cache that remembers one stays valid for as long
// as the prototypes it looked through keep their properties.
type Shape struct {
	// vm is the VM that the objects with this shape belong to.
	vm *VM

	proto  *Object
	parent *Shape

	// key is the property this shape adds to parent, stored in slot count-1.
	// It is an interned string or a symbol.
	key   *Object
	flags uint8
	count int32

	// table maps every property of the shape to its slot. For a shared shape it is built on first lookup.
	table       map[*Object]prop
	transitions map[shapeKey]*Shape

	private bool

	// order lists the keys of a private shape in insertion order.
	order []*Object
}

func (s *Shape) Lookup(key *Object) (prop, bool) {
	if s.table == nil {
		if s.count == 0 {
			return prop{}, false
		}
		s.table = make(map[*Object]prop, s.count)
		for t := s; t.parent != nil; t = t.parent {
			s.table[t.key] = prop{
				Slot:  t.count - 1,
				Flags: t.flags,
			}
		}
	}
	p, ok := s.table[key]
	return p, ok
}

// keys returns the property keys in insertion order. The result must not be modified.
func (s *Shape) keys() []*Object {
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
func (s *Shape) with(key *Object, flags uint8) *Shape {
	k := shapeKey{
		key:   key,
		flags: flags,
	}
	if t, ok := s.transitions[k]; ok {
		return t
	}
	t := &Shape{
		vm:     s.vm,
		proto:  s.proto,
		parent: s,
		key:    key,
		flags:  flags,
		count:  s.count + 1,
	}
	if s.transitions == nil {
		s.transitions = make(map[shapeKey]*Shape)
	}
	s.transitions[k] = t
	return t
}

// privateCopy returns a private shape with the properties of s.
func (s *Shape) privateCopy() *Shape {
	t := &Shape{
		vm:      s.vm,
		proto:   s.proto,
		count:   s.count,
		table:   make(map[*Object]prop, s.count+8),
		private: true,
		order:   append([]*Object(nil), s.keys()...),
	}
	for _, k := range t.order {
		p, _ := s.Lookup(k)
		t.table[k] = p
	}
	return t
}

// accessor is the content of the slot of an accessor property.
func NewAccessor(getter, setter *Object) Value {
	return Value{
		O: &Object{
			Kind: kindAccessor,
			Ext: &accessorPair{
				Getter: getter,
				setter: setter,
			},
		},
	}
}

type accessorPair struct {
	Getter *Object
	setter *Object
}

func (v Value) Accessor() *accessorPair {
	return v.O.Ext.(*accessorPair)
}

// shapeFor returns the empty shape for objects whose prototype is proto, which may be nil.
func (vm *VM) ShapeFor(proto *Object) *Shape {
	if proto == nil {
		return vm.nullProtoShape
	}
	if s, ok := vm.childShapes[proto]; ok {
		return s
	}
	s := &Shape{
		vm:    vm,
		proto: proto,
	}
	vm.childShapes[proto] = s
	proto.Flags |= objIsProto
	return s
}

func (vm *VM) NewObject(proto *Object) *Object {
	return &Object{
		Kind:  kindObject,
		Shape: vm.ShapeFor(proto),
	}
}

func (vm *VM) NewArray(elems []Value) *Object {
	return &Object{
		Kind:   KindArray,
		Class:  classArray,
		Shape:  vm.ArrayShape,
		Elems:  elems,
		Length: uint32(len(elems)),
	}
}

// newString returns the string s, which must be in canonical WTF-8, as a value.
func (vm *VM) NewString(s string) Value {
	if len(s) <= 1 {
		if s == "" {
			return vm.EmptyString
		}
		if c := s[0]; c < 0x80 {
			return vm.asciiStrings[c]
		}
	}
	return Value{
		O: &Object{
			Kind:  KindString,
			Shape: vm.stringShape,
			Str:   s,
		},
	}
}

// intern returns the string s as a property key.
func (vm *VM) Intern(s string) *Object {
	if o, ok := vm.interned[s]; ok {
		return o
	}
	o := &Object{
		Kind:  KindString,
		Flags: objInterned,
		Shape: vm.stringShape,
		Str:   s,
	}
	vm.interned[s] = o
	return o
}

// stringKey returns the property key for the string o.
func (vm *VM) stringKey(o *Object) *Object {
	if o.Flags&objInterned != 0 {
		return o
	}
	if k, ok := o.Ext.(*Object); ok {
		return k
	}
	k := vm.Intern(o.Str)
	o.Ext = k
	return k
}

func (o *Object) IsASCII() bool {
	if o.Flags&objASCIIKnown == 0 {
		o.Flags |= objASCIIKnown
		if wtf8.IsASCII(o.Str) {
			o.Flags |= objASCII
		}
	}
	return o.Flags&objASCII != 0
}

// stringLength returns the length of the string o in UTF-16 code units.
func (o *Object) StringLength() int {
	if o.IsASCII() {
		return len(o.Str)
	}
	if o.meta == nil {
		o.meta = &stringMeta{length: wtf8.Len(o.Str)}
	}
	return o.meta.length
}

// seek returns the position of the code unit i of the non-ASCII string o.
func (o *Object) Seek(i int) wtf8.Pos {
	if o.meta == nil {
		o.meta = &stringMeta{length: wtf8.Len(o.Str)}
	}
	p := wtf8.Seek(o.Str, o.meta.cursor, i)
	o.meta.cursor = p
	return p
}

// unitAt returns the code unit i of the string o. i must be in range.
func (o *Object) UnitAt(i int) uint16 {
	if o.IsASCII() {
		return uint16(o.Str[i])
	}
	return wtf8.At(o.Str, o.Seek(i))
}

// substring returns the code units from start up to end of the string o.
// The indices must be in range and in order.
func (o *Object) Substring(start, end int) string {
	if o.IsASCII() {
		return o.Str[start:end]
	}
	a := o.Seek(start)
	b := wtf8.Seek(o.Str, a, end)
	o.meta.cursor = b
	return wtf8.Slice(o.Str, a, b)
}

// unitString returns the string that consists of the code unit u.
func (vm *VM) UnitString(u uint16) Value {
	if u < 0x80 {
		return vm.asciiStrings[u]
	}
	return Value{
		O: &Object{
			Kind:  KindString,
			Shape: vm.stringShape,
			Str:   string(wtf8.AppendRune(nil, rune(u))),
		},
	}
}

// getOwn returns the own named property key of o.
func (o *Object) getOwn(key *Object) (prop, bool) {
	return o.Shape.Lookup(key)
}

// find looks key up on o and its prototype chain.
func Find(o *Object, key *Object) (holder *Object, p prop, ok bool) {
	for h := o; h != nil; h = h.Shape.proto {
		if p, ok := h.Shape.Lookup(key); ok {
			return h, p, true
		}
	}
	return nil, prop{}, false
}

// touchProto records that the properties of o changed, if o is a prototype.
func (vm *VM) TouchProto(o *Object) {
	if o.Flags&objIsProto != 0 {
		vm.protoEpoch++
	}
}

// add gives o the new own property key. The caller has checked that o has no such property.
func (vm *VM) Add(o *Object, key *Object, val Value, flags uint8) {
	s := o.Shape
	if !s.private && s.count >= privateShapeThreshold {
		s = s.privateCopy()
		o.Shape = s
	}
	if s.private {
		s.table[key] = prop{
			Slot:  s.count,
			Flags: flags,
		}
		s.order = append(s.order, key)
		s.count++
	} else {
		o.Shape = s.with(key, flags)
	}
	o.Slots = append(o.Slots, val)
	vm.TouchProto(o)
}

// define creates or replaces the own named property key of o with the given attributes,
// without any of the checks of the JavaScript operations.
func (vm *VM) Define(o *Object, key *Object, val Value, flags uint8) {
	p, ok := o.Shape.Lookup(key)
	if !ok {
		vm.Add(o, key, val, flags)
		return
	}
	if p.Flags != flags {
		vm.setFlags(o, key, p, flags)
	}
	o.Slots[p.Slot] = val
	vm.TouchProto(o)
}

// setFlags changes the attributes of the existing own property key of o.
func (vm *VM) setFlags(o *Object, key *Object, p prop, flags uint8) {
	// A shared shape may be remembered by inline caches, so the changed layout needs another one.
	s := o.Shape.privateCopy()
	s.table[key] = prop{
		Slot:  p.Slot,
		Flags: flags,
	}
	o.Shape = s
	vm.TouchProto(o)
}

// remove deletes the own named property key of o, which must exist.
func (vm *VM) remove(o *Object, key *Object) {
	// Removing a property moves the later ones, so the object gets a shape that no inline cache has seen.
	old := o.Shape
	s := &Shape{
		vm:      old.vm,
		proto:   old.proto,
		table:   make(map[*Object]prop, old.count),
		private: true,
	}
	slots := make([]Value, 0, len(o.Slots))
	for _, k := range old.keys() {
		if k == key {
			continue
		}
		q, _ := old.Lookup(k)
		s.table[k] = prop{
			Slot:  s.count,
			Flags: q.Flags,
		}
		s.order = append(s.order, k)
		s.count++
		slots = append(slots, o.Slots[q.Slot])
	}
	o.Shape = s
	o.Slots = slots
	vm.TouchProto(o)
}

// setProto changes the prototype of o.
func (vm *VM) SetProto(o *Object, proto *Object) {
	old := o.Shape
	if old.proto == proto {
		return
	}
	if old.private {
		s := old.privateCopy()
		s.proto = proto
		if proto != nil {
			proto.Flags |= objIsProto
		}
		o.Shape = s
	} else {
		s := vm.ShapeFor(proto)
		for _, k := range old.keys() {
			p, _ := old.Lookup(k)
			s = s.with(k, p.Flags)
		}
		o.Shape = s
	}
	vm.protoEpoch++
}

// makePrivate gives o a shape of its own, so that no inline cache filled for another object applies to it.
func (o *Object) makePrivate() {
	if !o.Shape.private {
		o.Shape = o.Shape.privateCopy()
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
	shape *Shape
	kind  uint8
	slot  int32

	holder *Object

	// epoch is the value of VM.protoEpoch the entry was filled at.
	epoch uint32

	next *Shape
}

// propCache is the inline cache of one property access site. It remembers two receiver shapes,
// because code that is shared by objects of two classes is common.
type propCache struct {
	cacheEntry
	second cacheEntry
}

// keep saves the current entry as the second one before c is filled for another shape.
func (c *propCache) keep(shape *Shape) {
	if c.shape != nil && c.shape != shape {
		c.second = c.cacheEntry
	}
}

// VM returns the VM that o belongs to, or nil for a value that is shared by all VMs.
func (o *Object) VM() *VM {
	if o.Shape == nil {
		return nil
	}
	return o.Shape.vm
}
