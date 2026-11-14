// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// indexKey returns the property key for the integer index i, which may be beyond the array indices.
func indexKey(vm *engine.VM, i int64) engine.PropKey {
	if i >= 0 && i <= engine.MaxArrayIndex {
		return engine.IndexKey(uint32(i))
	}
	return engine.NamedKey(vm.Intern(numconv.NumberToString(float64(i))))
}

// reviver is the state of one call of Revive.
type reviver struct {
	vm        *engine.VM
	fn        *engine.Object
	keySource *engine.Object
}

// Revive implements the steps of JSON.parse that call the function fn on the value v and
// its parts. v and rec are the results of ParseWithRecord.
func Revive(vm *engine.VM, v engine.Value, rec *Record, fn *engine.Object) engine.Value {
	root := vm.NewOrdinaryObject()
	k := engine.NamedKey(vm.Intern(""))
	vm.CreateDataPropertyOrThrow(root, k, v)
	r := &reviver{
		vm:        vm,
		fn:        fn,
		keySource: vm.Intern("source"),
	}
	return r.internalize(root, k, rec, 0)
}

// internalize implements InternalizeJSONProperty.
func (r *reviver) internalize(holder *engine.Object, name engine.PropKey, rec *Record, depth int) engine.Value {
	vm := r.vm
	if depth > maxDepth {
		vm.ThrowRangeError("maximum call stack size exceeded")
	}
	val := vm.Get(holder, name)
	ctx := vm.NewOrdinaryObject()
	if rec != nil && engine.SameValue(rec.value, val) {
		if !val.IsObject() {
			vm.CreateDataPropertyOrThrow(ctx, engine.NamedKey(r.keySource), vm.NewString(rec.source))
		}
	} else {
		rec = nil
	}
	if val.IsObject() {
		o := val.Object()
		update := func(k engine.PropKey, sub *Record) {
			v := r.internalize(o, k, sub, depth+1)
			if v.IsUndefined() {
				vm.DeleteProp(o, k)
			} else {
				vm.CreateDataProperty(o, k, v)
			}
		}
		if engine.IsArray(val) {
			n := vm.LengthOfArrayLike(o)
			for i := range n {
				var sub *Record
				if rec != nil {
					sub = rec.element(i)
				}
				update(indexKey(vm, i), sub)
			}
		} else {
			for _, k := range vm.EnumerableOwnKeys(o, nil) {
				var sub *Record
				if rec != nil {
					sub = rec.entry(k)
				}
				update(k, sub)
			}
		}
	}
	return vm.Call(r.fn, engine.ObjectValue(holder), vm.KeyValue(name), val, engine.ObjectValue(ctx))
}
