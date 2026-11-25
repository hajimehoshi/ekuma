// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewInstance returns a new empty ordinary instance with prototype proto, which may be nil.
func (cx *ExecutionContext) NewInstance(proto *storage.Object) *storage.Object {
	return storage.NewInstance(cx.ShapeFor(proto))
}

// NewOrdinaryObject returns a new empty ordinary object whose prototype is %Object.prototype%.
func (cx *ExecutionContext) NewOrdinaryObject() *storage.Object {
	return (&storage.PropertyObject{
		Kind:  storage.KindObject,
		Shape: cx.realm.Shapes.Object,
	}).Init()
}

// NewArrayCopy returns a new array with a copy of elems as its elements.
func (cx *ExecutionContext) NewArrayCopy(elems []storage.Value) *storage.Object {
	a, values := storage.NewObjectWithStorage(len(elems))
	a.Kind = storage.KindArray
	a.Class = storage.ClassArray
	a.Shape = cx.realm.Shapes.Array
	a.Elems = values
	copy(a.Elems, elems)
	a.Length = uint32(len(elems))
	return a
}

// NewEmptyArrayWithRoom returns a new empty array with room for n elements.
func (cx *ExecutionContext) NewEmptyArrayWithRoom(n int) *storage.Object {
	a, values := storage.NewObjectWithStorage(n)
	a.Kind = storage.KindArray
	a.Class = storage.ClassArray
	a.Shape = cx.realm.Shapes.Array
	a.InitElems(values[:0])
	return a
}

// NewArrayWithRoom returns an array of length undefined elements with room for n named properties.
func (cx *ExecutionContext) NewArrayWithRoom(length, n int) *storage.Object {
	var a *storage.Object
	var values []storage.Value
	if length+n <= storage.MaxInlineObjectValues {
		a, values = storage.NewObjectWithStorage(length + n)
		a.InitElems(values[:length:length])
		values = values[length:]
	} else {
		// Keep the named properties with the object so they cannot retain the elements
		// after the array shrinks.
		a, values = storage.NewObjectWithStorage(n)
		a.InitElems(make([]storage.Value, length))
		a.Pinned &^= storage.PinnedElems
	}
	a.Kind = storage.KindArray
	a.Class = storage.ClassArray
	a.Shape = cx.realm.Shapes.Array
	a.Length = uint32(length)
	a.InitSlots(values[:0])
	return a
}

// NewObjectFromProperties returns a new ordinary object whose prototype is Object.prototype,
// with the data properties keys[i]: vals[i] created in order as CreateDataProperty creates them.
// A repeated key keeps its first position and takes the last value. It does not keep the slices.
func (cx *ExecutionContext) NewObjectFromProperties(keys []storage.PropKey, vals []storage.Value) *storage.Object {
	o, slots := storage.NewObjectWithStorage(len(vals))
	o.Kind = storage.KindObject
	o.Shape = cx.realm.Shapes.Object
	o.InitSlots(slots[:0])
	for i, k := range keys {
		v := vals[i]
		if k.Key == nil {
			if int64(k.Idx) < int64(len(o.Elems)) && !o.Elems[k.Idx].IsHole() {
				o.Elems[k.Idx] = v
				continue
			}
			if o.Flags&storage.ObjSparse != 0 {
				if key := cx.IndexKey(k.Idx); key != nil {
					if _, ok := o.Shape.Lookup(key); ok {
						cx.Define(o, key, v, storage.FlagsData)
						continue
					}
				}
			}
			cx.AddIndex(o, k.Idx, v, storage.FlagsData)
			continue
		}
		// A transition exists only for a key that the shape does not have.
		if s := o.Shape; !s.Private {
			if t := s.Transitions.Get(storage.ShapeKey{
				Key:   k.Key,
				Flags: storage.FlagsData,
			}); t != nil {
				o.Shape = t
				o.AppendSlot(v)
				continue
			}
		}
		cx.Define(o, k.Key, v, storage.FlagsData)
	}
	return o
}

// indexStringCount is the number of the smallest array indices whose strings are shared.
const indexStringCount = 1024

// indexString returns the string of the array index i.
func (cx *ExecutionContext) indexString(i uint32) storage.Value {
	if i < 10 {
		return cx.Strings.Character(byte('0' + i))
	}
	if i >= indexStringCount {
		return cx.newIndexString(i)
	}
	t := cx.IndexStrings
	if int(i) >= len(t) {
		t = append(t, make([]*storage.Object, int(i)+1-len(t))...)
		t = t[:cap(t)]
		cx.IndexStrings = t
	}
	if o := t[i]; o != nil {
		return storage.ObjectValue(o)
	}
	v := cx.newIndexString(i)
	o := v.StringObject()
	o.Length = i + 1
	t[i] = o
	return v
}

// newIndexString returns a new string of the array index i, which is at least 10.
func (cx *ExecutionContext) newIndexString(i uint32) storage.Value {
	v := cx.NewString(wtf8.String(strconv.FormatUint(uint64(i), 10)))
	if !v.IsShortString() {
		v.O.Length = i + 1
	}
	return v
}

// stringKey returns the property key for the string o.
func (cx *ExecutionContext) stringKey(o *storage.Object) *storage.Object {
	if o.Flags&storage.ObjInterned != 0 {
		return o
	}
	if k, ok := o.ExtObject(); ok {
		return k
	}
	k := cx.Intern(o.Str())
	o.SetExtState(k)
	return k
}

// remove deletes the own named property key of o, which must exist.
func (cx *ExecutionContext) remove(o *storage.Object, key *storage.Object) {
	old := o.Shape
	if old.Private {
		cx.RemoveFromPrivate(o, key)
		return
	}
	// The shared shape without key has the later properties in the slots after the one of key,
	// so their values move down by one.
	if s := old.Without(key); s != nil {
		p, _ := old.Lookup(key)
		o.RemoveSlot(int(p.Slot))
		o.Shape = s
		cx.TouchProto(o)
		return
	}
	// Otherwise removing a property moves the later ones, so the object gets a shape that no
	// inline cache has seen.
	s := &storage.Shape{
		Space:   old.Space,
		Realm:   old.Realm,
		Proto:   old.Proto,
		Family:  old.Family,
		Table:   make(map[*storage.Object]storage.Prop, old.Count),
		Private: true,
	}
	slots := make([]storage.Value, 0, len(o.Slots))
	for _, k := range old.Keys() {
		if k == key {
			continue
		}
		q, _ := old.Lookup(k)
		s.Table[k] = storage.Prop{
			Slot:  s.Count,
			Flags: q.Flags,
		}
		s.Order = append(s.Order, k)
		s.Count++
		slots = append(slots, o.Slots[q.Slot])
	}
	o.ReplaceSlots(slots)
	o.Shape = s
	cx.TouchProto(o)
}
