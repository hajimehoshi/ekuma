// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// NewModuleSourceObject returns a module source object with the prototype proto whose
// [[ModuleSourceClassName]] is className.
func (cx *ExecutionContext) NewModuleSourceObject(proto *Object, className string) *Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), classModuleSource, className)
}

// ModuleSourceClassName returns the [[ModuleSourceClassName]] of v. ok is false when v has
// no such slot.
func (ag *agent) ModuleSourceClassName(v Value) (name string, ok bool) {
	if !v.IsObject() || v.O.Class != classModuleSource {
		return "", false
	}
	return v.O.ExtState().(string), true
}
