// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// NewModuleSourceObject returns a module source object with the prototype proto whose
// [[ModuleSourceClassName]] is className.
func (cx *ExecutionContext) NewModuleSourceObject(proto *storage.Object, className string) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassModuleSource, className)
}

// ModuleSourceClassName returns the [[ModuleSourceClassName]] of v. ok is false when v has
// no such slot.
func (ag *agent) ModuleSourceClassName(v storage.Value) (name string, ok bool) {
	if !v.IsObject() || v.O.Class != storage.ClassModuleSource {
		return "", false
	}
	return v.O.ExtState().(string), true
}
