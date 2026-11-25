// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/collection"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// MapIterator is the state of a Map Iterator object.
type MapIterator struct {
	// table is nil once the iterator is done.
	Table  *collection.Table
	Cursor collection.Cursor
	Kind   IterationKind
}

// SetIterator is the state of a Set Iterator object.
type SetIterator struct {
	// table is nil once the iterator is done.
	Table  *collection.Table
	Cursor collection.Cursor
	Kind   IterationKind
}

// mapOf returns the [[MapData]] of this, or nil when this is not a Map object.
func mapOf(this storage.Value) *collection.Table {
	t, _ := storage.MapData(this).(*collection.Table)
	return t
}

// setOf returns the [[SetData]] of this, or nil when this is not a Set object.
func setOf(this storage.Value) *collection.Table {
	t, _ := storage.SetData(this).(*collection.Table)
	return t
}

// iterationUnobservable reports whether iterating the Map or Set o calls the method iter as its
// @@iterator, which proto holds, and the method next, which iterProto holds.
func iterationUnobservable(q Context, o, proto, iter, iterProto, next *storage.Object) bool {
	return q.InheritsMethod(o, proto, q.WellKnownSymbol(storage.SymbolIterator), iter) && q.HasOwnMethod(iterProto, q.Key(storage.KeyNext), next)
}

// NewMapCopy returns the Map that the constructor makes with proto from iterable when that calls
// the original methods of a Map only. It reports false otherwise.
func NewMapCopy(q Context, proto *storage.Object, iterable storage.Value) (*storage.Object, bool) {
	src := mapOf(iterable)
	if src == nil || proto != q.Intrinsic(storage.IntrinsicMapPrototype) {
		return nil, false
	}
	if !q.HasOwnMethod(proto, q.Key(storage.KeySet), q.Intrinsic(storage.IntrinsicMapPrototypeSet)) {
		return nil, false
	}
	if !iterationUnobservable(q, iterable.Object(), proto, q.Intrinsic(storage.IntrinsicMapPrototypeEntries), q.Intrinsic(storage.IntrinsicMapIteratorPrototype), q.Intrinsic(storage.IntrinsicMapIteratorPrototypeNext)) {
		return nil, false
	}
	return q.NewMapObject(proto, src.Clone(q.Poll)), true
}

// NewSetCopy returns the Set that the constructor makes with proto from iterable when that calls
// the original methods of a Set only. It reports false otherwise.
func NewSetCopy(q Context, proto *storage.Object, iterable storage.Value) (*storage.Object, bool) {
	src := setOf(iterable)
	if src == nil || proto != q.Intrinsic(storage.IntrinsicSetPrototype) {
		return nil, false
	}
	if !q.HasOwnMethod(proto, q.Key(storage.KeyAdd), q.Intrinsic(storage.IntrinsicSetPrototypeAdd)) {
		return nil, false
	}
	if !iterationUnobservable(q, iterable.Object(), proto, q.Intrinsic(storage.IntrinsicSetPrototypeValues), q.Intrinsic(storage.IntrinsicSetIteratorPrototype), q.Intrinsic(storage.IntrinsicSetIteratorPrototypeNext)) {
		return nil, false
	}
	return q.NewSetObject(proto, src.Clone(q.Poll)), true
}

func MapDeleteKernel(q Context, this, k storage.Value) (storage.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Bool(t.Delete(k)), true
	}
	return storage.Undefined, false
}

func MapGetKernel(q Context, this, k storage.Value) (storage.Value, bool) {
	if t := mapOf(this); t != nil {
		v, _ := t.Get(k)
		return v, true
	}
	return storage.Undefined, false
}

func MapHasKernel(q Context, this, k storage.Value) (storage.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Bool(t.Has(k)), true
	}
	return storage.Undefined, false
}

func MapSetKernel(q Context, this, k, v storage.Value) (storage.Value, bool) {
	if t := mapOf(this); t != nil {
		t.Set(k, v)
		return this, true
	}
	return storage.Undefined, false
}

func MapSizeKernel(q Context, this storage.Value) (storage.Value, bool) {
	if t := mapOf(this); t != nil {
		return storage.Number(float64(t.Size())), true
	}
	return storage.Undefined, false
}

func MapIteratorStep(q Context, iterator *storage.Object, it *MapIterator) (storage.Value, bool) {
	if it.Table == nil {
		return storage.Undefined, false
	}
	k, v, ok := it.Table.Next(&it.Cursor)
	if !ok {
		it.Table = nil
		it.Cursor = collection.Cursor{}
		return storage.Undefined, false
	}
	switch it.Kind {
	case IterateKeys:
		return k, true
	case IterateValues:
		return v, true
	}
	return storage.ObjectValue(CreationContext(iterator).NewArrayPair(k, v)), true
}

// MapIteratorPairStep is the pair step of the next method of Map Iterator objects.
func MapIteratorPairStep(state any) (storage.Value, storage.Value, PairStep) {
	it, _ := state.(*MapIterator)
	if it == nil || it.Kind != IterateEntries {
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	if it.Table == nil {
		return storage.Undefined, storage.Undefined, PairDone
	}
	k, v, ok := it.Table.Next(&it.Cursor)
	if !ok {
		it.Table = nil
		it.Cursor = collection.Cursor{}
		return storage.Undefined, storage.Undefined, PairDone
	}
	return k, v, PairYielded
}

func SetAddKernel(q Context, this, v storage.Value) (storage.Value, bool) {
	t := setOf(this)
	if t == nil {
		return storage.Undefined, false
	}
	if !t.Has(v) {
		t.Append(v, v)
	}
	return this, true
}

func SetDeleteKernel(q Context, this, v storage.Value) (storage.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Bool(t.Delete(v)), true
	}
	return storage.Undefined, false
}

func SetHasKernel(q Context, this, v storage.Value) (storage.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Bool(t.Has(v)), true
	}
	return storage.Undefined, false
}

func SetSizeKernel(q Context, this storage.Value) (storage.Value, bool) {
	if t := setOf(this); t != nil {
		return storage.Number(float64(t.Size())), true
	}
	return storage.Undefined, false
}

func SetIteratorStep(q Context, iterator *storage.Object, it *SetIterator) (storage.Value, bool) {
	if it.Table == nil {
		return storage.Undefined, false
	}
	k, _, ok := it.Table.Next(&it.Cursor)
	if !ok {
		it.Table = nil
		it.Cursor = collection.Cursor{}
		return storage.Undefined, false
	}
	if it.Kind == IterateEntries {
		return storage.ObjectValue(CreationContext(iterator).NewArrayPair(k, k)), true
	}
	return k, true
}

// SetIteratorPairStep is the pair step of the next method of Set Iterator objects.
func SetIteratorPairStep(state any) (storage.Value, storage.Value, PairStep) {
	it, _ := state.(*SetIterator)
	if it == nil || it.Kind != IterateEntries {
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	if it.Table == nil {
		return storage.Undefined, storage.Undefined, PairDone
	}
	k, _, ok := it.Table.Next(&it.Cursor)
	if !ok {
		it.Table = nil
		it.Cursor = collection.Cursor{}
		return storage.Undefined, storage.Undefined, PairDone
	}
	return k, k, PairYielded
}
