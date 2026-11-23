// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// indexKey returns the property key for the integer index i, which may be beyond the array indices.
func indexKey(cx *engine.ExecutionContext, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.IndexKey(uint32(i))
	}
	return engine.NamedKey(cx.Intern(numconv.NumberToString(float64(i))))
}

// reviver is the state of one call of [Revive].
type reviver struct {
	cx        *engine.ExecutionContext
	fn        *engine.Object
	keySource *engine.Object
}

// Revive implements the steps of JSON.parse that call the function fn on the value v and
// its parts. v and rec are the results of ParseWithRecord.
func Revive(cx *engine.ExecutionContext, v engine.Value, rec *Record, fn *engine.Object) engine.Value {
	root := cx.NewOrdinaryObject()
	k := engine.NamedKey(cx.Intern(""))
	cx.CreateDataPropertyOrThrow(root, k, v)
	r := &reviver{
		cx:        cx,
		fn:        fn,
		keySource: cx.Intern("source"),
	}
	return r.internalize(root, k, rec, 0)
}

// internalize implements InternalizeJSONProperty.
func (r *reviver) internalize(holder *engine.Object, name engine.PropKey, rec *Record, depth int) engine.Value {
	cx := r.cx
	if depth > maxDepth {
		cx.ThrowRangeError("maximum call stack size exceeded")
	}
	val := cx.Get(holder, name)
	ctx := cx.NewOrdinaryObject()
	if rec != nil && engine.SameValue(rec.value, val) {
		if !val.IsObject() {
			cx.CreateDataPropertyOrThrow(ctx, engine.NamedKey(r.keySource), cx.NewString(rec.source))
		}
	} else {
		rec = nil
	}
	if val.IsObject() {
		o := val.Object()
		update := func(k engine.PropKey, sub *Record) {
			v := r.internalize(o, k, sub, depth+1)
			if v.IsUndefined() {
				cx.DeleteProp(o, k)
			} else {
				cx.CreateDataProperty(o, k, v)
			}
		}
		if cx.IsArray(val) {
			n := cx.LengthOfArrayLike(o)
			for i := range n {
				var sub *Record
				if rec != nil {
					sub = rec.element(i)
				}
				update(indexKey(cx, i), sub)
			}
		} else {
			for _, k := range cx.EnumerableOwnKeys(o, nil) {
				var sub *Record
				if rec != nil {
					sub = rec.entry(k)
				}
				update(k, sub)
			}
		}
	}
	return cx.Call(r.fn, engine.ObjectValue(holder), cx.KeyValue(name), val, engine.ObjectValue(ctx))
}
