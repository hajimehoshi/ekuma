// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package storage

import (
	"fmt"
	"slices"
)

// peekKind is the kind of the outcome of a lookup that runs no JavaScript.
type peekKind uint8

const (
	// PeekMiss is the end of the prototype chain.
	PeekMiss peekKind = iota

	// PeekNumericMiss is a typed array that answers for a key that is not an array index.
	PeekNumericMiss

	PeekData
	PeekAccessor

	// PeekLength is the length of an array or a primitive string, which has no slot.
	PeekLength

	// PeekOpaque is an object that has to perform the operation itself.
	PeekOpaque
)

type Peeked struct {
	Kind   peekKind
	Holder *Object
	Prop   Prop

	// Length is the value of a PeekLength.
	Length float64

	// Why describes the object of a PeekOpaque.
	Why string
}

// Slot returns the slot of a data property or an accessor property.
func (p *Peeked) Slot() *Value {
	return &p.Holder.slots[p.Prop.Slot]
}

func (p *Peeked) String() string {
	switch p.Kind {
	case PeekMiss:
		return "no property"
	case PeekNumericMiss:
		return "no property of a typed array"
	case PeekData:
		return fmt.Sprintf("a data property in slot %d of the object %p with the attributes %#x", p.Prop.Slot, p.Holder, p.Prop.Flags)
	case PeekAccessor:
		a := p.Slot().Accessor()
		return fmt.Sprintf("an accessor property in slot %d of the object %p with the getter %p and the setter %p", p.Prop.Slot, p.Holder, a.Getter, a.Setter)
	case PeekLength:
		return fmt.Sprintf("the length %v", p.Length)
	}
	return "an operation of " + p.Why
}

// Peek looks the named property key of v up without running JavaScript.
func (r *RealmPart) Peek(v Value, key *Object) Peeked {
	start := v.O
	switch {
	case v.IsNullish():
		return Peeked{
			Kind: PeekOpaque,
			Why:  "a value without properties",
		}
	case v.O == NumberObject:
		start = r.Intrinsic(IntrinsicNumberPrototype)
	case v.IsString():
		if key == r.Key(KeyLength) {
			return Peeked{
				Kind:   PeekLength,
				Length: float64(v.StringLength()),
			}
		}
		start = r.Intrinsic(IntrinsicStringPrototype)
	case v.IsBool():
		start = r.Intrinsic(IntrinsicBooleanPrototype)
	case v.IsSymbol():
		start = r.Intrinsic(IntrinsicSymbolPrototype)
	case v.IsBigInt():
		start = r.Intrinsic(IntrinsicBigIntPrototype)
	}
	return r.PeekFrom(start, key)
}

// PeekFrom is [RealmPart.Peek] from the object start on.
func (r *RealmPart) PeekFrom(start, key *Object) Peeked {
	for h := start; h != nil; h = h.shape.Proto {
		p, why := r.PeekOwn(h, key)
		if why != "" {
			return Peeked{
				Kind:   PeekOpaque,
				Holder: h,
				Why:    why,
			}
		}
		if p.Kind != PeekMiss {
			return p
		}
	}
	return Peeked{}
}

// PeekOwn looks the own named property key of h up. why describes h when h has to perform the
// operation itself.
func (sp *Space) PeekOwn(h, key *Object) (p Peeked, why string) {
	switch {
	case h.Class == ClassProxy:
		return Peeked{}, "a proxy"
	case h.Class == ClassModuleNamespace:
		return Peeked{}, "a module namespace"
	case h.Class == ClassLazy:
		return Peeked{}, "an object whose properties are not defined yet"
	case h.Fn != nil && !h.Fn.Materialized && sp.IsLazyKey(key):
		return Peeked{}, "a function whose initial properties are not stored yet"
	case h.Kind == KindArray && key == sp.Key(KeyLength):
		return Peeked{
			Kind:   PeekLength,
			Holder: h,
			Length: float64(h.length),
		}, ""
	case h.Class == ClassTypedArray && IsCanonicalNumericString(key):
		return Peeked{
			Kind:   PeekNumericMiss,
			Holder: h,
		}, ""
	}
	q, ok := h.shape.Lookup(key)
	if !ok {
		return Peeked{}, ""
	}
	p = Peeked{
		Kind:   PeekData,
		Holder: h,
		Prop:   q,
	}
	if q.Flags&FlagAccessor != 0 {
		p.Kind = PeekAccessor
	}
	return p, ""
}

// originalFailure is agent.cacheFailure for a check that does not know the running instruction.
func originalFailure(cache string, key *Object, got, want string) string {
	if key != nil {
		cache += " of " + DisplayString(Value{O: key})
	}
	return "storage: " + CacheMismatch(cache, got, want)
}

// CacheMismatch describes a cache that answered with got where the lookup finds want.
func CacheMismatch(cache, got, want string) string {
	return cache + " answered " + got + ", but the lookup finds " + want
}

// checkArrayIteration checks what r.ArrayIter, which [Space.ArrayIterationUnmodified] has brought up
// to date, says about the iteration of the arrays of r.
func (r *RealmPart) checkArrayIteration() {
	c := &r.ArrayIter
	values := r.Intrinsics[IntrinsicArrayPrototypeValues]
	next := r.Intrinsics[IntrinsicArrayIteratorPrototypeNext]
	if c.hasOriginalMethods(r) {
		r.checkOriginal(r.Intrinsics[IntrinsicArrayPrototype], r.WellKnownSymbol(SymbolIterator), values)
		r.checkOriginal(r.Intrinsics[IntrinsicArrayIteratorPrototype], r.Key(KeyNext), next)
	}
	if c.NoReturn {
		r.CheckNoIteratorReturn()
	}
}

// CheckNoIteratorReturn checks that the array iterators of r have no return method.
func (r *RealmPart) CheckNoIteratorReturn() {
	key := r.Key(KeyReturn)
	if p := r.PeekFrom(r.Intrinsics[IntrinsicArrayIteratorPrototype], key); p.Kind != PeekMiss {
		panic(originalFailure("the array iteration cache", key, "no property", p.String()))
	}
}

// CheckPlainArrayIteration checks the array iteration cache for o.
func (r *RealmPart) CheckPlainArrayIteration(o *Object) {
	if o.shape != r.Shapes.Array || !r.ArrayIterationUnmodified(r) {
		return
	}
	r.checkOriginal(o, r.WellKnownSymbol(SymbolIterator), r.Intrinsic(IntrinsicArrayPrototypeValues))
}

// checkOriginal checks that the property key of o is a data property whose value is want.
func (r *RealmPart) checkOriginal(o, key, want *Object) {
	if p := r.PeekFrom(o, key); p.Kind != PeekData || p.Slot().O != want {
		panic(originalFailure("the cache of original properties", key, fmt.Sprintf("the function %p", want), p.String()))
	}
}

// checkUnmodifiedRegExp checks that o finds the properties recorded by
// [RealmPart.RecordRegExpOriginals].
func (r *RealmPart) checkUnmodifiedRegExp(o *Object) {
	proto := r.Intrinsic(IntrinsicRegExpPrototype)
	for i := range r.regExpOriginals.props {
		q := &r.regExpOriginals.props[i]
		start := q.holder
		if start == proto {
			start = o
		}
		p := r.PeekFrom(start, q.key)
		ok := p.Holder == q.holder
		switch p.Kind {
		case PeekData:
			ok = ok && q.flags&FlagAccessor == 0 && p.Slot().O == q.value
		case PeekAccessor:
			ok = ok && q.flags&FlagAccessor != 0 && p.Slot().Accessor().Getter == q.value
		default:
			ok = false
		}
		if !ok {
			panic(originalFailure("the cache of RegExp properties", q.key, fmt.Sprintf("the function %p", q.value), p.String()))
		}
	}
}

// checkRegExpShape checks that the own lastIndex property of o is a writable data property in slot
// 0, if o has the shape that says so.
func (r *RealmPart) checkRegExpShape(o *Object) {
	s := r.Shapes.regExp
	if s == nil || o.shape != s {
		return
	}
	p, _ := r.PeekOwn(o, r.Key(KeyLastIndex))
	if p.Kind != PeekData || p.Prop.Slot != 0 || p.Prop.Flags != FlagWritable || s.Proto != r.Intrinsic(IntrinsicRegExpPrototype) {
		panic(originalFailure("the RegExp shape", r.Key(KeyLastIndex), "the slot 0", p.String()))
	}
}

// checkEnumerable checks what the shared shape s remembers about its enumerable properties.
func (s *Shape) checkEnumerable() {
	if want := s.sharedHasEnumerableString(); (s.enumerable == enumerableSome) != want {
		panic("storage: " + CacheMismatch("the cache of enumerable properties", fmt.Sprint(!want), fmt.Sprint(want)))
	}
}

// maxCheckedSparseIndices is the number of properties up to which checkSparseIndices lists them
// again, which bounds its cost.
const maxCheckedSparseIndices = 4096

// checkSparseIndices checks the array indices that c remembers of the shape s.
func (c *sparseIndexCache) checkSparseIndices(s *Shape) {
	if s.Count > maxCheckedSparseIndices {
		return
	}
	var want []uint32
	for _, k := range s.Keys() {
		if k.Kind == KindString {
			if i, ok := StringIndex(k.Text()); ok {
				want = append(want, i)
			}
		}
	}
	slices.Sort(want)
	if !slices.Equal(c.indices, want) {
		panic("storage: " + CacheMismatch("the cache of array indices", fmt.Sprint(c.indices), fmt.Sprint(want)))
	}
}
