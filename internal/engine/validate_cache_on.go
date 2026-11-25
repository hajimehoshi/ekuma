// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package engine

import (
	"fmt"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// This file checks the answers of the caches of property lookups against a lookup that uses no
// cache. The lookup decides by the classes of the objects and not by the shape families, which the
// caches rely on, and changes nothing that JavaScript can observe.

// cacheFailure describes a cache that answered with got where the lookup of key, which is nil when
// the cache is not about a single key, finds want. fp and pc are the instruction, or fp is nil for
// the instruction of the running frame.
func (ag *agent) cacheFailure(fp *storage.FuncProto, pc int, cache string, key *storage.Object, got, want string) string {
	if key != nil {
		cache += " of " + ag.DisplayString(storage.Value{O: key})
	}
	msg := storage.CacheMismatch(cache, got, want)
	if fp == nil {
		if len(ag.frames) == 0 {
			return "engine: " + msg
		}
		fr := &ag.frames[len(ag.frames)-1]
		// A frame that runs slow code continues after its instruction.
		fp, pc = fr.fp, max(fr.pc-1, 0)
	}
	return validationFailure(fp, pc, msg)
}

// checkReadHit checks that the property key of v is the data property at ref, or missing when ref
// is the slot for misses.
func (cx *ExecutionContext) checkReadHit(fp *storage.FuncProto, pc int, v storage.Value, key *storage.Object, ref *storage.Value) {
	p := cx.Peek(v, key)
	switch p.Kind {
	case peekData:
		if ref == p.Slot() && sameBits(*ref, *p.Slot()) {
			return
		}
	case peekMiss, peekNumericMiss:
		if ref == cx.MissHolder.Raw().SlotRef(0) && sameBits(*ref, storage.Undefined) {
			return
		}
	}
	got := "the data property at " + fmt.Sprintf("%p", ref)
	if ref == cx.MissHolder.Raw().SlotRef(0) {
		got = "a miss"
	}
	panic(cx.cacheFailure(fp, pc, "the read cache", key, got, p.String()))
}

// checkEntryRead checks the answer of e, an entry of the kind storage.CacheOwn or
// storage.CacheProto for the shape of o, where reading key from v starts at o.
func (cx *ExecutionContext) checkEntryRead(fp *storage.FuncProto, pc int, v storage.Value, o, key *storage.Object, e *storage.CacheEntry) {
	if e.Kind == storage.CacheOwn {
		cx.checkReadHit(fp, pc, v, key, o.Raw().SlotRef(int(e.Slot)))
		return
	}
	cx.checkReadHit(fp, pc, v, key, e.Ref)
}

// checkRememberedRead checks that the property key of v is the property q of holder.
func (cx *ExecutionContext) checkRememberedRead(v storage.Value, key, holder *storage.Object, q storage.Prop) {
	if p := cx.Peek(v, key); p.Kind != peekData && p.Kind != peekAccessor || p.Holder != holder || p.Prop != q {
		panic(cx.cacheFailure(nil, 0, "the cache of further shapes", key, fmt.Sprintf("the slot %d of the object %p", q.Slot, holder), p.String()))
	}
}

// checkGetterHit checks that getter is the getter of the property key of v.
func (cx *ExecutionContext) checkGetterHit(fp *storage.FuncProto, pc int, v storage.Value, key, getter *storage.Object) {
	if p := cx.Peek(v, key); p.Kind != peekAccessor || p.Slot().Accessor().Getter != getter {
		panic(cx.cacheFailure(fp, pc, "the getter cache", key, fmt.Sprintf("the getter %p", getter), p.String()))
	}
}

// checkOwnGetterHit checks that the property key of v is the accessor property in the slot of o.
func (cx *ExecutionContext) checkOwnGetterHit(fp *storage.FuncProto, pc int, v storage.Value, key *storage.Object, o *storage.Object, slot int32) {
	ref := o.Raw().SlotRef(int(slot))
	if p := cx.Peek(v, key); p.Kind != peekAccessor || p.Slot() != ref {
		panic(cx.cacheFailure(fp, pc, "the own getter cache", key, fmt.Sprintf("the accessor property at %p", ref), p.String()))
	}
}

// checkLengthHit checks that n is the implicit length of v.
func (cx *ExecutionContext) checkLengthHit(fp *storage.FuncProto, pc int, v storage.Value, n float64) {
	if p := cx.Peek(v, cx.Key(storage.KeyLength)); p.Kind != peekLength || p.Length != n {
		panic(cx.cacheFailure(fp, pc, "the implicit length", nil, fmt.Sprint(n), p.String()))
	}
}

// checkTypedArrayLengthHit checks that n is what the length of the typed array v is.
func (cx *ExecutionContext) checkTypedArrayLengthHit(fp *storage.FuncProto, pc int, v storage.Value, n float64) {
	getter := cx.Intrinsic(storage.IntrinsicTypedArrayPrototypeLengthGetter)
	cx.checkGetterHit(fp, pc, v, cx.Key(storage.KeyLength), getter)
	if want := float64(storage.TypedArrayLength(v.O)); n != want {
		panic(cx.cacheFailure(fp, pc, "the typed array length cache", nil, fmt.Sprint(n), fmt.Sprint(want)))
	}
}

// checkStoreHit checks that a store to the property key of v goes to the own writable data
// property in the slot of o.
func (cx *ExecutionContext) checkStoreHit(fp *storage.FuncProto, pc int, v storage.Value, key *storage.Object, o *storage.Object, slot int32) {
	ref := o.Raw().SlotRef(int(slot))
	p := cx.Peek(v, key)
	if !v.IsObject() || p.Kind != peekData || p.Holder != v.O || p.Prop.Flags&storage.FlagWritable == 0 || p.Slot() != ref {
		panic(cx.cacheFailure(fp, pc, "the store cache", key, fmt.Sprintf("the own data property at %p", ref), p.String()))
	}
}

// checkAddHit checks that a store to the property key of o adds it and gives o the shape next.
func (cx *ExecutionContext) checkAddHit(fp *storage.FuncProto, pc int, o, key *storage.Object, next *storage.Shape) {
	p := cx.PeekFrom(o, key)
	inherited := p.Kind == peekData && p.Holder != o && p.Prop.Flags&storage.FlagWritable != 0
	if p.Kind != peekMiss && !inherited {
		panic(cx.cacheFailure(fp, pc, "the add cache", key, "a new property", p.String()))
	}
	cx.checkAddedShape(fp, pc, o, key, next)
}

// checkDefineHit checks that defining the data property key of o adds it and gives o the shape
// next.
func (cx *ExecutionContext) checkDefineHit(fp *storage.FuncProto, pc int, o, key *storage.Object, next *storage.Shape) {
	if p, why := cx.PeekOwn(o, key); why != "" {
		panic(cx.cacheFailure(fp, pc, "the define cache", key, "a new property", "an operation of "+why))
	} else if p.Kind != peekMiss {
		panic(cx.cacheFailure(fp, pc, "the define cache", key, "a new property", p.String()))
	}
	cx.checkAddedShape(fp, pc, o, key, next)
}

// checkAddedShape checks that o can be given the new enumerable, writable and configurable data
// property key by moving it to the shape next.
func (cx *ExecutionContext) checkAddedShape(fp *storage.FuncProto, pc int, o, key *storage.Object, next *storage.Shape) {
	s := o.Shape()
	var want string
	switch {
	case o.Flags()&storage.ObjNotExtensible != 0:
		want = "an object that is not extensible"
	case o.Flags()&storage.ObjIsProto != 0:
		want = "a prototype"
	case s.Private:
		want = "an object with a private shape"
	case o.SlotCount() != int(s.Count):
		want = fmt.Sprintf("%d slots for %d properties", o.SlotCount(), s.Count)
	case next.Parent != s || next.Key != key || next.Flags != storage.FlagsData || next.Count != s.Count+1 ||
		next.Private || next.Proto != s.Proto || next.Realm != s.Realm || next.Family != s.Family:
		want = "another shape"
	default:
		return
	}
	panic(cx.cacheFailure(fp, pc, "the cache of the next shape", key, "a new property", want))
}

// checkSetterHit checks that setter is the setter of the property key of v.
func (cx *ExecutionContext) checkSetterHit(fp *storage.FuncProto, pc int, v storage.Value, key, setter *storage.Object) {
	if p := cx.Peek(v, key); !v.IsObject() || p.Kind != peekAccessor || p.Slot().Accessor().Setter != setter {
		panic(cx.cacheFailure(fp, pc, "the setter cache", key, fmt.Sprintf("the setter %p", setter), p.String()))
	}
}

// checkGlobalExists checks that the global variable key exists, as c says.
func (cx *ExecutionContext) checkGlobalExists(fp *storage.FuncProto, pc int, key *storage.Object, c *storage.GlobalCache) {
	if p := cx.peekGlobal(key); c.Holder != cx.realm.Global || p.Kind != peekData && p.Kind != peekAccessor {
		panic(cx.cacheFailure(fp, pc, "the cache of existing globals", key, fmt.Sprintf("a binding of the global object %p", c.Holder), p.String()))
	}
}

// checkGlobalHit checks that the global variable key is where c says, and that it can be assigned
// if write is set.
func (cx *ExecutionContext) checkGlobalHit(fp *storage.FuncProto, pc int, key *storage.Object, c *storage.GlobalCache, write bool) {
	p := cx.peekGlobal(key)
	if c.Holder != cx.realm.Global || p.Kind != peekData || p.Slot() != c.Ref || c.Ref.O == tdzObject || write && p.Prop.Flags&storage.FlagWritable == 0 {
		panic(cx.cacheFailure(fp, pc, "the global cache", key, fmt.Sprintf("the initialized binding at %p of the global object %p", c.Ref, c.Holder), p.String()))
	}
}

// peekGlobal looks the global variable key up in the lexical declarations and then in the own
// properties of the global object.
func (cx *ExecutionContext) peekGlobal(key *storage.Object) peeked {
	r := cx.realm
	if q, ok := r.GlobalLex.Shape().Lookup(key); ok {
		return peeked{
			Kind:   peekData,
			Holder: r.GlobalLex,
			Prop:   q,
		}
	}
	p, why := cx.PeekOwn(r.Global, key)
	if why != "" {
		return peeked{
			Kind:   peekOpaque,
			Holder: r.Global,
			Why:    why,
		}
	}
	return p
}

// checkInstanceofHit checks that r is the result of v instanceof Value{o: c}, whose prototype
// property is in the slot.
func (cx *ExecutionContext) checkInstanceofHit(fp *storage.FuncProto, pc int, v storage.Value, c *storage.Object, slot int32, r bool) {
	ref := c.Raw().SlotRef(int(slot))
	key := cx.WellKnownSymbol(storage.SymbolHasInstance)
	hasInstance := cx.Intrinsic(storage.IntrinsicFunctionPrototypeHasInstance)
	if p := cx.PeekFrom(c, key); p.Kind != peekData || p.Slot().O != hasInstance {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", key, fmt.Sprintf("the function %p", hasInstance), p.String()))
	}
	if c.Fn == nil || c.Fn.Bound != nil {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, "an ordinary function", "another object"))
	}
	cx.checkReadHit(fp, pc, storage.Value{O: c}, cx.Key(storage.KeyPrototype), ref)
	want := false
	if v.IsObject() && v.O.Class != storage.ClassProxy {
		for h := v.O.Shape().Proto; h != nil && !want; h = h.Shape().Proto {
			want = h == ref.O
			if h.Class == storage.ClassProxy && !want {
				panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, fmt.Sprint(r), "a proxy on the prototype chain"))
			}
		}
	}
	if v.IsObject() && v.O.Class == storage.ClassProxy || r != want {
		panic(cx.cacheFailure(fp, pc, "the instanceof cache", nil, fmt.Sprint(r), "another result"))
	}
}

// checkInstanceShape checks that s is the shape of a new instance in the realm r whose prototype
// is proto.
func (cx *ExecutionContext) checkInstanceShape(fp *storage.FuncProto, pc int, proto *storage.Object, r *storage.RealmPart, s *storage.Shape) {
	if s.Proto != proto || s.Realm != r || s.Space != cx.space || s.Family != storage.OrdinaryRoot || s.Parent != nil || s.Count != 0 || s.Private {
		panic(cx.cacheFailure(fp, pc, "the cache of the instance shape", nil, fmt.Sprintf("a shape for the prototype %p", s.Proto), fmt.Sprintf("the prototype %p", proto)))
	}
}

// checkReturnedProperty checks the answer of e, the entry of the property that c returns of this.
func (cx *ExecutionContext) checkReturnedProperty(c *storage.FuncProto, this storage.Value, e *storage.CacheEntry) {
	get := c.Code[1]
	if get.Op != bytecode.OpGetProp {
		panic(validationFailure(c, 1, "the property that the function returns is not read here"))
	}
	cx.contextOf(c.Realm).checkEntryRead(c, 1, this, this.O, c.Keys[get.C], e)
}

const peekAccessor = storage.PeekAccessor

const peekData = storage.PeekData

const peekLength = storage.PeekLength

const peekMiss = storage.PeekMiss

const peekNumericMiss = storage.PeekNumericMiss

const peekOpaque = storage.PeekOpaque

type peeked = storage.Peeked
