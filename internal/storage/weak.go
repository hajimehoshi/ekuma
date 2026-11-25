// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"weak"
)

// CanBeHeldWeakly implements CanBeHeldWeakly.
func (sp *Space) CanBeHeldWeakly(v Value) bool {
	if v.IsObject() {
		return true
	}
	if !v.IsSymbol() {
		return false
	}
	_, registered := sp.KeyForSymbol(v.O)
	return !registered
}

// WeakRefDeref implements WeakRefDeref for the WeakRef object v.
// It reports false when v is not a WeakRef object.
func (sp *Space) WeakRefDeref(v Value) (Value, bool) {
	if !v.IsObject() || v.O.Class != ClassWeakRef {
		return Undefined, false
	}
	t := v.O.ExtState().(weak.Pointer[Object]).Value()
	if t == nil {
		return Undefined, true
	}
	sp.AddToKeptObjects(t)
	return Value{O: t}, true
}

// keptObjects is a [[KeptAlive]] list: the objects and symbols that must stay alive until the
// current job ends.
type keptObjects struct {
	objects map[*Object]struct{}
}

func (k *keptObjects) add(o *Object) {
	if k.objects == nil {
		k.objects = map[*Object]struct{}{}
	}
	k.objects[o] = struct{}{}
}

func (k *keptObjects) clear() {
	clear(k.objects)
}

// AddToKeptObjects implements AddToKeptObjects: o stays alive until the current job ends.
func (sp *Space) AddToKeptObjects(o *Object) {
	sp.keptObjects.add(o)
}

// ClearKeptObjects implements ClearKeptObjects.
// It must be called when a job ends: after each promise job, and when the outermost call into
// the engine returns.
func (sp *Space) ClearKeptObjects() {
	sp.keptObjects.clear()
}
