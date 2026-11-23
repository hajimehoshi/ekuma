// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// NewModuleSourceObject returns a module source object with the prototype proto whose
// [[ModuleSourceClassName]] is className.
func (cx *ExecutionContext) NewModuleSourceObject(proto *Object, className string) *Object {
	o := cx.NewObject(proto)
	o.class = classModuleSource
	o.setExtState(className)
	return o
}

// ModuleSourceClassName returns the [[ModuleSourceClassName]] of v. ok is false when v has
// no such slot.
func (ag *agent) ModuleSourceClassName(v Value) (name string, ok bool) {
	if !v.IsObject() || v.o.class != classModuleSource {
		return "", false
	}
	return v.o.extState().(string), true
}
