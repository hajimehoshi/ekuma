// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"unsafe"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
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

// ObjReversed marks a string whose Text holds its contents with the bytes in reverse order.
// Its bit is that of ObjFrozenLength, which only arrays and private names use. The byte length,
// the ASCII-ness and the UTF-16 length of Text are those of the contents, so only a read of the
// contents themselves needs [Object.Str].
const ObjReversed = ObjFrozenLength

// ObjExclusive marks a string that is the tail of its builder and that nothing refers to but the
// register of the variable whose append made it, so that the next append to the variable can
// extend it in place. Its bit is that of ObjSparse, which only objects use.
const ObjExclusive = ObjSparse

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

// SharedElems is the bit of [Object.Pinned] that marks elements which are the constants of an
// array literal, shared by the arrays that it made. They have no holes and no room for more
// elements.
const SharedElems = PinnedGlobalSlots << 1

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
	flags uint8

	// Pinned holds the pinned bits of the storage of slots and elems, and [SharedElems]. A string,
	// which has neither, holds its [ConcatCount] there instead.
	Pinned uint8

	// length is the length of an array, which is at least len(elems). For a string it is 0,
	// or the array index that the string stands for plus one. See [Object.ArrayIndex].
	length uint32

	// shape is the layout of slots. Only the singletons and internal values have none.
	shape *Shape

	// slots holds the values of the named properties in the order of the shape. Primitive values
	// have none: a string or a symbol holds its text in the first two words instead. See
	// [Object.Text].
	slots []Value

	// objectTail holds what plain objects and most strings do not need. It is never nil: such an
	// object shares zeroTail, which is never written. See [Object.ownTail].
	*objectTail
}

// Text returns the content of a string or the description of a symbol. A string marked
// ObjReversed holds its bytes in reverse order. Packed-string sentinels use zero bytes to carry
// only the byte length. Text is empty for the other primitive values, and o must not be an object.
func (o *Object) Text() wtf8.String {
	return *o.text()
}

// SetText makes s the text of the string or the symbol o.
func (o *Object) SetText(s wtf8.String) {
	*o.text() = s
}

// text returns the text of a string or a symbol, which has no slots, in the words of slots: the
// pointer to the bytes takes the place of the pointer to the values, and the length that of the
// number of values. The capacity of slots stays zero.
func (o *Object) text() *wtf8.String {
	return (*wtf8.String)(unsafe.Pointer(&o.slots))
}

// textObject is an Object of a string or a symbol, with its text in place of its slots. A
// composite literal of it makes such an object in one go. Its layout is that of Object, with the
// same pointers at the same offsets, and a pointer in the word of the capacity of slots, which a
// string never reads. Every string that can have a builder is allocated as a textObject, so that
// the collector sees that pointer.
type textObject struct {
	Kind   Kind
	Class  class
	flags  uint8
	Pinned uint8
	length uint32
	shape  *Shape
	text   wtf8.String

	// builder is the builder that the string is the tail or the head of: a *ReversedBuilder when
	// the string is marked ObjReversed, and a *strings.Builder otherwise. See concat.go.
	builder unsafe.Pointer

	*objectTail
}

// The layouts of textObject and Object must agree.
const (
	_ = -uint(unsafe.Sizeof(textObject{}) ^ unsafe.Sizeof(Object{}))
	_ = -uint(unsafe.Offsetof(textObject{}.text) ^ unsafe.Offsetof(Object{}.slots))
	_ = -uint(unsafe.Offsetof(textObject{}.shape) ^ unsafe.Offsetof(Object{}.shape))
	_ = -uint(unsafe.Offsetof(textObject{}.objectTail) ^ unsafe.Offsetof(Object{}.objectTail))
	_ = -uint(unsafe.Offsetof(textObject{}.length) ^ unsafe.Offsetof(Object{}.length))
	_ = -uint(unsafe.Offsetof(textObject{}.builder) ^ (unsafe.Offsetof(Object{}.slots) + 2*unsafe.Sizeof(uintptr(0))))
)

// stringBuilder returns the builder word of the string o.
func (o *Object) stringBuilder() unsafe.Pointer {
	return (*textObject)(unsafe.Pointer(o)).builder
}

// ClearBuilder records that the string o is no longer the tail or the head of a builder.
func (o *Object) ClearBuilder() {
	(*textObject)(unsafe.Pointer(o)).builder = nil
}

// newText returns the Object of t.
func newText(t *textObject) *Object {
	return (*Object)(unsafe.Pointer(t))
}

// textTailObject is a textObject together with its tail.
type textTailObject struct {
	textObject
	tail objectTail
}

func (t *textTailObject) Init() *Object {
	t.objectTail = &t.tail
	return newText(&t.textObject)
}

// objectTail holds the fields of an object that plain objects do not use.
type objectTail struct {
	// elems holds the properties whose keys are array indices, by index.
	// An element that does not exist holds Hole.
	elems []Value

	// Fn is set for a callable object. A closure must call OwnFunction before changing its state.
	Fn *Function

	// Ext is the internal state of a built-in object or the push size hint of a closure.
	// For a string that is not interned it is the interned string with the same content,
	// once that was looked up, or the builder that the string is the tail or the head of (see
	// concat.go).
	// When the object has side data, Ext is its *ObjectMeta, which holds the internal state
	// instead. See [Object.ExtState] and [Object.Meta].
	Ext any
}

// zeroTail is the tail of the objects that have no elements, function state or internal state.
var zeroTail objectTail

// ownTail returns the tail of o, which it gives o first if o shares zeroTail.
func (o *Object) ownTail() *objectTail {
	if o.objectTail == &zeroTail {
		o.newTail()
	}
	return o.objectTail
}

// newTail gives o a tail of its own. It stays out of line so that ownTail can be inlined.
//
//go:noinline
func (o *Object) newTail() {
	o.objectTail = &objectTail{}
}

// PropertyObject keeps an object's header and its tail in one allocation, for the objects that
// have elements, function state or internal state.
type PropertyObject struct {
	Object
	objectTail
}

func (o *PropertyObject) Init() *Object {
	o.Object.objectTail = &o.objectTail
	return &o.Object
}

// plainObject is an object without a tail of its own.
type plainObject struct {
	Object
}

func (o *plainObject) Init() *Object {
	o.Object.objectTail = &zeroTail
	return &o.Object
}

// ObjectMeta is the side data that few objects need: what indexing a non-ASCII string needs,
// the entries in weak collections of an object or a symbol, the empty shapes of the objects
// whose prototype is this object, and the room that removing the first elements of an array left.
type ObjectMeta struct {
	// Length and Cursor are the cached UTF-16 view of a string that is not ASCII:
	// its length, or 0 before it is known, and the position of the most recent access.
	// Only a string of at most MaxStringLength bytes has them, so they fit in 32 bits.
	Length int32
	Cursor stringPos

	// checkpoints holds the positions of the code units of such a string whose indices are
	// multiples of checkpointInterval, once an access far from Cursor needed one. See
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
	if v == nil && o.objectTail == &zeroTail {
		return
	}
	o.ownTail().Ext = v
}

// ExtConcatCount returns the concatenation count of the string o, or 0.
func (o *Object) ExtConcatCount() ConcatCount {
	return ConcatCount(o.Pinned)
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

// ensureMeta returns the side data of o, creating it if needed.
func (o *Object) ensureMeta() *ObjectMeta {
	if m := o.Meta(); m != nil {
		return m
	}
	t := o.ownTail()
	m := &ObjectMeta{Ext: t.Ext}
	t.Ext = m
	return m
}

// DelegatesPropertyAccess reports whether a lookup must use the object's property operations
// even when its storage has no matching key.
func (o *Object) DelegatesPropertyAccess() bool {
	return o.flags&ObjExotic != 0 && o.shape.Family == DelegatedRoot
}

// IndicesInElements reports whether elems accounts for all own indexed properties.
func (o *Object) IndicesInElements() bool {
	return (o.Kind == KindObject && o.Class == ClassObject || o.Kind == KindArray) && o.flags&(ObjSparse|ObjExotic) == 0
}

// NewObject returns a new empty object with the prototype proto, which may be nil.
func (r *RealmPart) NewObject(proto *Object) *Object {
	return NewObject(r.ShapeFor(proto))
}

func NewObject(s *Shape) *Object {
	return (&plainObject{Object{
		Kind:  KindObject,
		shape: s,
	}}).Init()
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
	o.shape = s
	o.InitSlots(slots[:0])
	return o
}

// NewEmptyObjectWithRoom returns a new empty ordinary object whose prototype is %Object.prototype%,
// with room for n properties.
func (r *RealmPart) NewEmptyObjectWithRoom(n int) *Object {
	o, slots := NewObjectWithStorage(n)
	o.Kind = KindObject
	o.shape = r.Shapes.Object
	o.InitSlots(slots[:0])
	return o
}

const MaxInlineObjectValues = 16

// NewObjectWithStorage returns an object without a tail of its own and storage for n values. The
// object is zero but for its pinned bits. For a small n, both are a single allocation, and the
// storage may have room for more values.
func NewObjectWithStorage(n int) (*Object, []Value) {
	var o *Object
	var storage []Value
	switch n {
	case 0:
		return (&plainObject{}).Init(), nil
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
	case 8:
		w := &objectWith[[8]Value]{}
		o, storage = w.Init(), w.storage[:]
	// The capacities of the larger storage fill an allocation size class.
	case 9:
		w := &objectWith[[9]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 10, 11:
		w := &objectWith[[11]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case 12, 13:
		w := &objectWith[[13]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case 14, 15:
		w := &objectWith[[15]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case MaxInlineObjectValues:
		w := &objectWith[[MaxInlineObjectValues + 1]Value]{}
		o, storage = w.Init(), w.storage[:n]
	default:
		return (&plainObject{}).Init(), make([]Value, n)
	}
	// The storage becomes the slots, the elements or both.
	o.Pinned = PinnedSlots | PinnedElems
	return o, storage
}

// objectWith is an object without a tail of its own together with storage for its slots or
// elements.
type objectWith[S any] struct {
	plainObject
	storage S
}

// NewTailObjectWithStorage is [NewObjectWithStorage] for an object with a tail of its own, such as
// an array.
func NewTailObjectWithStorage(n int) (*Object, []Value) {
	var o *Object
	var storage []Value
	switch n {
	case 0:
		return (&PropertyObject{}).Init(), nil
	case 1:
		w := &tailObjectWith[[1]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 2:
		w := &tailObjectWith[[2]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 3:
		w := &tailObjectWith[[3]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 4:
		w := &tailObjectWith[[4]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 5:
		w := &tailObjectWith[[5]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 6:
		w := &tailObjectWith[[6]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 7:
		w := &tailObjectWith[[7]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 8:
		w := &tailObjectWith[[8]Value]{}
		o, storage = w.Init(), w.storage[:]
	// The capacities of the larger storage fill an allocation size class.
	case 9:
		w := &tailObjectWith[[9]Value]{}
		o, storage = w.Init(), w.storage[:]
	case 10, 11:
		w := &tailObjectWith[[11]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case 12, 13:
		w := &tailObjectWith[[13]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case 14, 15:
		w := &tailObjectWith[[15]Value]{}
		o, storage = w.Init(), w.storage[:n]
	case MaxInlineObjectValues:
		w := &tailObjectWith[[MaxInlineObjectValues + 1]Value]{}
		o, storage = w.Init(), w.storage[:n]
	default:
		return (&PropertyObject{}).Init(), make([]Value, n)
	}
	// The storage becomes the slots, the elements or both.
	o.Pinned = PinnedSlots | PinnedElems
	return o, storage
}

// tailObjectWith is an object with a tail of its own together with storage for its slots or
// elements.
type tailObjectWith[S any] struct {
	PropertyObject
	storage S
}

// TouchProto records that the named properties of o changed, if o is a prototype: one was added,
// removed or defined, or its attributes changed. Whatever moves the slots of a prototype calls it
// too, because CacheProto entries point into them.
//
// A change of elements, of the length of an array or of the value of a data property does not need
// it. The caches of property lookups and the records of original built-in properties hold named
// properties in slots only. The operations in array.go that depend on the elements of prototypes
// read them each time.
func (sp *Space) TouchProto(o *Object) {
	if o.flags&ObjIsProto != 0 {
		sp.ProtoEpoch++
	}
}

// SetProto changes the prototype of o. It stays out of line to keep ArrayCreate small.
//
//go:noinline
func (sp *Space) SetProto(o *Object, proto *Object) {
	if o.shape.Proto != proto {
		sp.rebuildShape(o, proto, o.shape.Family)
		sp.ProtoEpoch++
	}
}

// MakeNonExtensible makes o refuse new properties.
func (o *Object) MakeNonExtensible() {
	o.flags |= ObjNotExtensible
	// The inline caches that add properties are keyed by shared shapes.
	o.MakePrivate()
}

// EndLazy makes the lazy object o an ordinary object without properties.
func (sp *Space) EndLazy(o *Object) {
	o.Class = ClassObject
	o.SetExtState(nil)
	o.flags &^= ObjExotic
	sp.rebuildShape(o, o.shape.Proto, OrdinaryRoot)
}

// HasProperties reports whether o has property storage, which primitive values lack.
func (o *Object) HasProperties() bool {
	return o.Kind >= KindFirstObject
}

// Shape returns the layout of the slots of o, or nil for the singletons and internal values.
func (o *Object) Shape() *Shape {
	return o.shape
}

// Flags returns the flags of o.
func (o *Object) Flags() uint8 {
	return o.flags
}

// Length returns the length of the array o.
func (o *Object) Length() uint32 {
	return o.length
}

// SetArrayIndex records that the string o stands for the array index i.
func (o *Object) SetArrayIndex(i uint32) {
	o.length = i + 1
}

// GrowLength makes the length of the array o n, which is greater.
func (o *Object) GrowLength(n uint32) {
	if ValidateExec && n < o.length {
		panic("storage: GrowLength to a smaller length")
	}
	o.length = n
}
