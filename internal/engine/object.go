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
	return storage.NewObject(cx.realm.Shapes.Object)
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
	o.SetArrayIndex(i)
	t[i] = o
	return v
}

// newIndexString returns a new string of the array index i, which is at least 10.
func (cx *ExecutionContext) newIndexString(i uint32) storage.Value {
	v := cx.NewString(wtf8.String(strconv.FormatUint(uint64(i), 10)))
	if !v.IsShortString() {
		v.O.SetArrayIndex(i)
	}
	return v
}

// stringKey returns the property key for the string o.
func (cx *ExecutionContext) stringKey(o *storage.Object) *storage.Object {
	if o.Flags()&storage.ObjInterned != 0 {
		return o
	}
	if k, ok := o.ExtObject(); ok {
		return k
	}
	k := cx.Intern(o.Str())
	o.SetExtState(k)
	return k
}
