// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// ArrayIterator is the state of an Array Iterator object.
type ArrayIterator struct {
	// Object is nil once the iterator is done.
	Object    *storage.Object
	NextIndex int64
	Kind      IterationKind
}

// Result returns what the iterator yields for key and value.
func (it *ArrayIterator) Result(q Context, key, value storage.Value) storage.Value {
	switch it.Kind {
	case IterateKeys:
		return key
	case IterateValues:
		return value
	default:
		return storage.Value{O: q.NewArrayPair(key, value)}
	}
}

// StepPlain advances the iterator when that neither runs code nor throws. It declines without
// advancing otherwise.
func (it *ArrayIterator) StepPlain(q Context) (key, value storage.Value, st PairStep) {
	if it == nil {
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	r := q.Realm
	o := it.Object
	if o == nil {
		return storage.Undefined, storage.Undefined, PairDone
	}
	i := it.NextIndex
	var length int64
	switch {
	case o.Kind == storage.KindArray:
		length = int64(o.Length)
	case o.Class == storage.ClassTypedArray:
		t := o.ExtState().(*storage.TypedArray)
		if it.Kind != IterateKeys && i <= storage.MaxArrayIndex {
			if b, ok := t.ElementBytes(uint32(i)); ok {
				value = storage.ElementFromBytes(t.Type, b)
				it.NextIndex = i + 1
				r.PollAtIndex(i)
				return storage.Number(float64(i)), value, PairYielded
			}
		}
		n, ok := t.Length()
		if !ok {
			return storage.Undefined, storage.Undefined, PairDeclined
		}
		length = int64(n)
	default:
		return storage.Undefined, storage.Undefined, PairDeclined
	}
	if i >= length {
		it.Object = nil
		return storage.Undefined, storage.Undefined, PairDone
	}
	key = storage.Number(float64(i))
	if it.Kind == IterateKeys {
		it.NextIndex = i + 1
		return key, storage.Undefined, PairYielded
	}
	if i < int64(len(o.Elems)) && !o.Elems[i].IsHole() {
		value = o.Elems[i]
		it.NextIndex = i + 1
		r.PollAtIndex(i)
		return key, value, PairYielded
	}
	return storage.Undefined, storage.Undefined, PairDeclined
}
