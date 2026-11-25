// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package engine

import (
	"fmt"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// This file checks the answers of the caches of property lookups against a lookup that uses no
// cache. The lookup decides by the classes of the objects and not by the shape families, which the
// caches rely on, and changes nothing that JavaScript can observe.

// peekKind is the kind of the outcome of a lookup that runs no JavaScript.
type peekKind uint8

const (
	// peekMiss is the end of the prototype chain.
	peekMiss peekKind = iota

	// peekNumericMiss is a typed array that answers for a key that is not an array index.
	peekNumericMiss

	peekData
	peekAccessor

	// peekLength is the length of an array or a primitive string, which has no slot.
	peekLength

	// peekOpaque is an object that has to perform the operation itself.
	peekOpaque
)

type peeked struct {
	kind   peekKind
	holder *Object
	prop   prop

	// length is the value of a peekLength.
	length float64

	// why describes the object of a peekOpaque.
	why string
}

// slot returns the slot of a data property or an accessor property.
func (p *peeked) slot() *Value {
	return &p.holder.slots[p.prop.slot]
}

func (p *peeked) String() string {
	switch p.kind {
	case peekMiss:
		return "no property"
	case peekNumericMiss:
		return "no property of a typed array"
	case peekData:
		return fmt.Sprintf("a data property in slot %d of the object %p with the attributes %#x", p.prop.slot, p.holder, p.prop.flags)
	case peekAccessor:
		a := p.slot().accessor()
		return fmt.Sprintf("an accessor property in slot %d of the object %p with the getter %p and the setter %p", p.prop.slot, p.holder, a.getter, a.setter)
	case peekLength:
		return fmt.Sprintf("the length %v", p.length)
	}
	return "an operation of " + p.why
}

// peek looks the named property key of v up without running JavaScript.
func (cx *ExecutionContext) peek(v Value, key *Object) peeked {
	start := v.o
	switch {
	case v.IsNullish():
		return peeked{
			kind: peekOpaque,
			why:  "a value without properties",
		}
	case v.o == numberObject:
		start = cx.Intrinsic(IntrinsicNumberPrototype)
	case v.IsString():
		if key == cx.Key(KeyLength) {
			return peeked{
				kind:   peekLength,
				length: float64(v.StringLength()),
			}
		}
		start = cx.Intrinsic(IntrinsicStringPrototype)
	case v.IsBool():
		start = cx.Intrinsic(IntrinsicBooleanPrototype)
	case v.IsSymbol():
		start = cx.Intrinsic(IntrinsicSymbolPrototype)
	case v.IsBigInt():
		start = cx.Intrinsic(IntrinsicBigIntPrototype)
	}
	return cx.peekFrom(start, key)
}

// peekFrom is peek from the object start on.
func (cx *ExecutionContext) peekFrom(start, key *Object) peeked {
	for h := start; h != nil; h = h.shape.proto {
		p, why := cx.peekOwn(h, key)
		if why != "" {
			return peeked{
				kind:   peekOpaque,
				holder: h,
				why:    why,
			}
		}
		if p.kind != peekMiss {
			return p
		}
	}
	return peeked{}
}

// peekOwn looks the own named property key of h up. why describes h when h has to perform the
// operation itself.
func (cx *ExecutionContext) peekOwn(h, key *Object) (p peeked, why string) {
	switch {
	case h.class == classProxy:
		return peeked{}, "a proxy"
	case h.class == classModuleNamespace:
		return peeked{}, "a module namespace"
	case h.class == classLazy:
		return peeked{}, "an object whose properties are not defined yet"
	case h.fn != nil && !h.fn.materialized && cx.isLazyKey(key):
		return peeked{}, "a function whose initial properties are not stored yet"
	case h.kind == kindArray && key == cx.Key(KeyLength):
		return peeked{
			kind:   peekLength,
			holder: h,
			length: float64(h.length),
		}, ""
	case h.class == classTypedArray && isCanonicalNumericString(key):
		return peeked{
			kind:   peekNumericMiss,
			holder: h,
		}, ""
	}
	q, ok := h.shape.lookup(key)
	if !ok {
		return peeked{}, ""
	}
	p = peeked{
		kind:   peekData,
		holder: h,
		prop:   q,
	}
	if q.flags&FlagAccessor != 0 {
		p.kind = peekAccessor
	}
	return p, ""
}

// cacheFailure describes a cache that answered with got where the lookup of key, which is nil when
// the cache is not about a single key, finds want. fp and pc are the instruction, or fp is nil for
// the instruction of the running frame.
func (ag *agent) cacheFailure(fp *funcProto, pc int, cache string, key *Object, got, want string) string {
	if key != nil {
		cache += " of " + ag.DisplayString(Value{o: key})
	}
	msg := cacheMismatch(cache, got, want)
	if fp == nil {
		if len(ag.frames) == 0 {
			return "ekumavalidate: " + msg
		}
		fr := &ag.frames[len(ag.frames)-1]
		// A frame that runs slow code continues after its instruction.
		fp, pc = fr.fp, max(fr.pc-1, 0)
	}
	return validationFailure(fp, pc, msg)
}

// cacheMismatch describes a cache that answered with got where the lookup finds want.
func cacheMismatch(cache, got, want string) string {
	return cache + " answered " + got + ", but the lookup finds " + want
}

// checkReadHit checks that the property key of v is the data property at ref, or missing when ref
// is the slot for misses.
func (cx *ExecutionContext) checkReadHit(fp *funcProto, pc int, v Value, key *Object, ref *Value) {
	p := cx.peek(v, key)
	switch p.kind {
	case peekData:
		if ref == p.slot() && sameBits(*ref, *p.slot()) {
			return
		}
	case peekMiss, peekNumericMiss:
		if ref == &cx.missHolder.slots[0] && sameBits(*ref, Undefined) {
			return
		}
	}
	got := "the data property at " + fmt.Sprintf("%p", ref)
	if ref == &cx.missHolder.slots[0] {
		got = "a miss"
	}
	panic(cx.cacheFailure(fp, pc, "the read cache", key, got, p.String()))
}

// checkEntryRead checks the answer of e, an entry of the kind cacheOwn or cacheProto for the shape
// of o, where reading key from v starts at o.
func (cx *ExecutionContext) checkEntryRead(fp *funcProto, pc int, v Value, o, key *Object, e *cacheEntry) {
	if e.kind == cacheOwn {
		cx.checkReadHit(fp, pc, v, key, &o.slots[e.slot])
		return
	}
	cx.checkReadHit(fp, pc, v, key, e.ref)
}

// checkRememberedRead checks that the property key of v is the property q of holder.
func (cx *ExecutionContext) checkRememberedRead(v Value, key, holder *Object, q prop) {
	if p := cx.peek(v, key); p.kind != peekData && p.kind != peekAccessor || p.holder != holder || p.prop != q {
		panic(cx.cacheFailure(nil, 0, "the cache of further shapes", key, fmt.Sprintf("the slot %d of the object %p", q.slot, holder), p.String()))
	}
}

// checkGetterHit checks that getter is the getter of the property key of v.
func (cx *ExecutionContext) checkGetterHit(fp *funcProto, pc int, v Value, key, getter *Object) {
	if p := cx.peek(v, key); p.kind != peekAccessor || p.slot().accessor().getter != getter {
		panic(cx.cacheFailure(fp, pc, "the getter cache", key, fmt.Sprintf("the getter %p", getter), p.String()))
	}
}

// checkOwnGetterHit checks that the property key of v is the accessor property at ref.
func (cx *ExecutionContext) checkOwnGetterHit(fp *funcProto, pc int, v Value, key *Object, ref *Value) {
	if p := cx.peek(v, key); p.kind != peekAccessor || p.slot() != ref {
		panic(cx.cacheFailure(fp, pc, "the own getter cache", key, fmt.Sprintf("the accessor property at %p", ref), p.String()))
	}
}

// checkLengthHit checks that n is the implicit length of v.
func (cx *ExecutionContext) checkLengthHit(fp *funcProto, pc int, v Value, n float64) {
	if p := cx.peek(v, cx.Key(KeyLength)); p.kind != peekLength || p.length != n {
		panic(cx.cacheFailure(fp, pc, "the implicit length", nil, fmt.Sprint(n), p.String()))
	}
}

// checkTypedArrayLengthHit checks that n is what the length of the typed array v is.
func (cx *ExecutionContext) checkTypedArrayLengthHit(fp *funcProto, pc int, v Value, n float64) {
	getter := cx.Intrinsic(IntrinsicTypedArrayPrototypeLengthGetter)
	cx.checkGetterHit(fp, pc, v, cx.Key(KeyLength), getter)
	if want := float64(cx.typedArrayLength(v.o)); n != want {
		panic(cx.cacheFailure(fp, pc, "the typed array length cache", nil, fmt.Sprint(n), fmt.Sprint(want)))
	}
}

// checkStoreHit checks that a store to the property key of v goes to the own writable data
// property at ref.
func (cx *ExecutionContext) checkStoreHit(fp *funcProto, pc int, v Value, key *Object, ref *Value) {
	p := cx.peek(v, key)
	if !v.IsObject() || p.kind != peekData || p.holder != v.o || p.prop.flags&FlagWritable == 0 || p.slot() != ref {
		panic(cx.cacheFailure(fp, pc, "the store cache", key, fmt.Sprintf("the own data property at %p", ref), p.String()))
	}
}

// checkAddHit checks that a store to the property key of o adds it and gives o the shape next.
func (cx *ExecutionContext) checkAddHit(fp *funcProto, pc int, o, key *Object, next *shape) {
	p := cx.peekFrom(o, key)
	inherited := p.kind == peekData && p.holder != o && p.prop.flags&FlagWritable != 0
	if p.kind != peekMiss && !inherited {
		panic(cx.cacheFailure(fp, pc, "the add cache", key, "a new property", p.String()))
	}
	cx.checkAddedShape(fp, pc, o, key, next)
}

// checkDefineHit checks that defining the data property key of o adds it and gives o the shape
// next.
func (cx *ExecutionContext) checkDefineHit(fp *funcProto, pc int, o, key *Object, next *shape) {
	if p, why := cx.peekOwn(o, key); why != "" {
		panic(cx.cacheFailure(fp, pc, "the define cache", key, "a new property", "an operation of "+why))
	} else if p.kind != peekMiss {
		panic(cx.cacheFailure(fp, pc, "the define cache", key, "a new property", p.String()))
	}
	cx.checkAddedShape(fp, pc, o, key, next)
}

// checkAddedShape checks that o can be given the new enumerable, writable and configurable data
// property key by moving it to the shape next.
func (cx *ExecutionContext) checkAddedShape(fp *funcProto, pc int, o, key *Object, next *shape) {
	s := o.shape
	var want string
	switch {
	case o.flags&objNotExtensible != 0:
		want = "an object that is not extensible"
	case o.flags&objIsProto != 0:
		want = "a prototype"
	case s.private:
		want = "an object with a private shape"
	case len(o.slots) != int(s.count):
		want = fmt.Sprintf("%d slots for %d properties", len(o.slots), s.count)
	case next.parent != s || next.key != key || next.flags != FlagsData || next.count != s.count+1 ||
		next.private || next.proto != s.proto || next.realm != s.realm || next.family != s.family:
		want = "another shape"
	default:
		return
	}
	panic(cx.cacheFailure(fp, pc, "the cache of the next shape", key, "a new property", want))
}

// checkSetterHit checks that setter is the setter of the property key of v.
func (cx *ExecutionContext) checkSetterHit(fp *funcProto, pc int, v Value, key, setter *Object) {
	if p := cx.peek(v, key); !v.IsObject() || p.kind != peekAccessor || p.slot().accessor().setter != setter {
		panic(cx.cacheFailure(fp, pc, "the setter cache", key, fmt.Sprintf("the setter %p", setter), p.String()))
	}
}

// checkGlobalExists checks that the global variable key exists, as c says.
func (cx *ExecutionContext) checkGlobalExists(fp *funcProto, pc int, key *Object, c *globalCache) {
	if p := cx.peekGlobal(key); c.holder != cx.realm.global || p.kind != peekData && p.kind != peekAccessor {
		panic(cx.cacheFailure(fp, pc, "the cache of existing globals", key, fmt.Sprintf("a binding of the global object %p", c.holder), p.String()))
	}
}

// checkGlobalHit checks that the global variable key is where c says, and that it can be assigned
// if write is set.
func (cx *ExecutionContext) checkGlobalHit(fp *funcProto, pc int, key *Object, c *globalCache, write bool) {
	p := cx.peekGlobal(key)
	if c.holder != cx.realm.global || p.kind != peekData || p.slot() != c.ref || c.ref.o == tdzObject || write && p.prop.flags&FlagWritable == 0 {
		panic(cx.cacheFailure(fp, pc, "the global cache", key, fmt.Sprintf("the initialized binding at %p of the global object %p", c.ref, c.holder), p.String()))
	}
}

// peekGlobal looks the global variable key up in the lexical declarations and then in the own
// properties of the global object.
func (cx *ExecutionContext) peekGlobal(key *Object) peeked {
	r := cx.realm
	if q, ok := r.globalLex.shape.lookup(key); ok {
		return peeked{
			kind:   peekData,
			holder: r.globalLex,
			prop:   q,
		}
	}
	p, why := cx.peekOwn(r.global, key)
	if why != "" {
		return peeked{
			kind:   peekOpaque,
			holder: r.global,
			why:    why,
		}
	}
	return p
}

// checkInstanceofHit checks that r is the result of v instanceof Value{o: c}, whose prototype
// property is at ref.
func (cx *ExecutionContext) checkInstanceofHit(fp *funcProto, pc int, v Value, c *Object, ref *Value, r bool) {
	key := cx.WellKnownSymbol(SymbolHasInstance)
	hasInstance := cx.Intrinsic(IntrinsicFunctionPrototypeHasInstance)
	if p := cx.peekFrom(c, key); p.kind != peekData || p.slot().o != hasInstance {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", key, fmt.Sprintf("the function %p", hasInstance), p.String()))
	}
	if c.fn == nil || c.fn.bound != nil {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, "an ordinary function", "another object"))
	}
	cx.checkReadHit(fp, pc, Value{o: c}, cx.Key(KeyPrototype), ref)
	want := false
	if v.IsObject() && v.o.class != classProxy {
		for h := v.o.shape.proto; h != nil && !want; h = h.shape.proto {
			want = h == ref.o
			if h.class == classProxy && !want {
				panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, fmt.Sprint(r), "a proxy on the prototype chain"))
			}
		}
	}
	if v.IsObject() && v.o.class == classProxy || r != want {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, fmt.Sprint(r), "another result"))
	}
}

// checkInstanceShape checks that s is the shape of a new instance in the realm r whose prototype
// is proto.
func (cx *ExecutionContext) checkInstanceShape(fp *funcProto, pc int, proto *Object, r *realmPart, s *shape) {
	if s.proto != proto || s.realm != r || s.space != cx.space || s.family != ordinaryRoot || s.parent != nil || s.count != 0 || s.private {
		panic(cx.cacheFailure(fp, pc, "the cache of the instance shape", nil, fmt.Sprintf("a shape for the prototype %p", s.proto), fmt.Sprintf("the prototype %p", proto)))
	}
}

// checkArrayIteration checks what c, which arrayIterationUnmodified has brought up to date, says
// about the iteration of the arrays of r.
func (ag *agent) checkArrayIteration(r *realmPart) {
	c := &r.arrayIter
	cx := r.executionContext()
	values := r.intrinsics[IntrinsicArrayPrototypeValues]
	next := r.intrinsics[IntrinsicArrayIteratorPrototypeNext]
	if c.found && c.iter.value().o == values && c.next.value().o == next {
		ag.checkOriginal(cx, r.intrinsics[IntrinsicArrayPrototype], ag.WellKnownSymbol(SymbolIterator), values)
		ag.checkOriginal(cx, r.intrinsics[IntrinsicArrayIteratorPrototype], ag.Key(KeyNext), next)
	}
	if c.noReturn {
		ag.checkNoIteratorReturn(r)
	}
}

// checkNoIteratorReturn checks that the array iterators of r have no return method.
func (ag *agent) checkNoIteratorReturn(r *realmPart) {
	key := ag.Key(KeyReturn)
	if p := r.executionContext().peekFrom(r.intrinsics[IntrinsicArrayIteratorPrototype], key); p.kind != peekMiss {
		panic(ag.cacheFailure(nil, 0, "the array iteration cache", key, "no property", p.String()))
	}
}

// checkPlainArray checks that iterating o starts with the original values method, if
// arrayIterationPlain says so.
func (cx *ExecutionContext) checkPlainArray(o *Object) {
	if o.shape != cx.realm.shapes.array || !cx.arrayIterationUnmodified(cx.realmPart) {
		return
	}
	cx.checkOriginal(cx, o, cx.WellKnownSymbol(SymbolIterator), cx.Intrinsic(IntrinsicArrayPrototypeValues))
}

// checkOriginal checks that the property key of o is a data property whose value is want.
func (ag *agent) checkOriginal(cx *ExecutionContext, o, key, want *Object) {
	if p := cx.peekFrom(o, key); p.kind != peekData || p.slot().o != want {
		panic(ag.cacheFailure(nil, 0, "the cache of original properties", key, fmt.Sprintf("the function %p", want), p.String()))
	}
}

// checkUnmodifiedRegExp checks that o finds the properties recorded by
// [ExecutionContext.RecordRegExpOriginals].
func (cx *ExecutionContext) checkUnmodifiedRegExp(o *Object) {
	proto := cx.Intrinsic(IntrinsicRegExpPrototype)
	for i := range cx.realm.regExpOriginals.props {
		q := &cx.realm.regExpOriginals.props[i]
		start := q.holder
		if start == proto {
			start = o
		}
		p := cx.peekFrom(start, q.key)
		ok := p.holder == q.holder
		switch p.kind {
		case peekData:
			ok = ok && q.flags&FlagAccessor == 0 && p.slot().o == q.value
		case peekAccessor:
			ok = ok && q.flags&FlagAccessor != 0 && p.slot().accessor().getter == q.value
		default:
			ok = false
		}
		if !ok {
			panic(cx.cacheFailure(nil, 0, "the cache of RegExp properties", q.key, fmt.Sprintf("the function %p", q.value), p.String()))
		}
	}
}

// checkRegExpShape checks that the own lastIndex property of o is a writable data property in slot
// 0, if o has the shape that says so.
func (cx *ExecutionContext) checkRegExpShape(o *Object) {
	s := cx.realm.shapes.regExp
	if s == nil || o.shape != s {
		return
	}
	p, _ := cx.peekOwn(o, cx.Key(KeyLastIndex))
	if p.kind != peekData || p.prop.slot != 0 || p.prop.flags != FlagWritable || s.proto != cx.Intrinsic(IntrinsicRegExpPrototype) {
		panic(cx.cacheFailure(nil, 0, "the RegExp shape", cx.Key(KeyLastIndex), "the slot 0", p.String()))
	}
}

// checkEnumerable checks what the shared shape s remembers about its enumerable properties.
func (s *shape) checkEnumerable() {
	if want := s.sharedHasEnumerableString(); (s.enumerable == enumerableSome) != want {
		panic("ekumavalidate: " + cacheMismatch("the cache of enumerable properties", fmt.Sprint(!want), fmt.Sprint(want)))
	}
}

// maxCheckedSparseIndices is the number of properties up to which checkSparseIndices lists them
// again, which bounds its cost.
const maxCheckedSparseIndices = 4096

// checkSparseIndices checks the array indices that c remembers of the shape s.
func (c *sparseIndexCache) checkSparseIndices(s *shape) {
	if s.count > maxCheckedSparseIndices {
		return
	}
	var want []uint32
	for _, k := range s.keys() {
		if k.kind == kindString {
			if i, ok := stringIndex(k.str); ok {
				want = append(want, i)
			}
		}
	}
	slices.Sort(want)
	if !slices.Equal(c.indices, want) {
		panic("ekumavalidate: " + cacheMismatch("the cache of array indices", fmt.Sprint(c.indices), fmt.Sprint(want)))
	}
}

// checkReturnedProperty checks the answer of e, the entry of the property that c returns of this.
func (c *funcProto) checkReturnedProperty(this Value, e *cacheEntry) {
	get := c.code[1]
	if get.Op != bytecode.OpGetProp {
		panic(validationFailure(c, 1, "the property that the function returns is not read here"))
	}
	c.realm.executionContext().checkEntryRead(c, 1, this, this.o, c.keys[get.C], e)
}
