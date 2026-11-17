// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// moduleNamespace holds the internal slots of a module namespace exotic object.
type moduleNamespace struct {
	module *Module

	// keys are the [[Exports]] as property keys, sorted in code unit order.
	keys []PropKey

	// targets holds the resolved binding of each of keys.
	targets []resolution

	// index maps a key to its index in keys.
	index map[PropKey]int

	// deferred is the [[Deferred]]: the module is evaluated when the exports are first used.
	deferred bool
}

// moduleNamespace implements GetModuleNamespace for the evaluation phase.
func (vm *VM) moduleNamespace(m *Module) *Object {
	if m.namespace == nil {
		m.namespace = vm.newModuleNamespace(m, false)
	}
	return m.namespace
}

// deferredModuleNamespace implements GetModuleNamespace for the defer phase.
func (vm *VM) deferredModuleNamespace(m *Module) *Object {
	if m.deferredNamespace == nil {
		m.deferredNamespace = vm.newModuleNamespace(m, true)
	}
	return m.deferredNamespace
}

// newModuleNamespace implements ModuleNamespaceCreate for the exports of m that resolve.
func (vm *VM) newModuleNamespace(m *Module, deferred bool) *Object {
	type export struct {
		name string
		r    resolution
	}
	var exports []export
	for _, n := range m.exportedNames() {
		if r := m.resolveExport(n); r.kind == resolutionFound {
			exports = append(exports, export{
				name: n,
				r:    r,
			})
		}
	}
	slices.SortFunc(exports, func(a, b export) int {
		return wtf8.Compare(a.name, b.name)
	})
	ns := &moduleNamespace{
		module:   m,
		keys:     make([]PropKey, len(exports)),
		targets:  make([]resolution, len(exports)),
		index:    make(map[PropKey]int, len(exports)),
		deferred: deferred,
	}
	for i, e := range exports {
		k := vm.ToPropertyKey(Value{o: vm.Intern(e.name)})
		ns.keys[i] = k
		ns.targets[i] = e.r
		ns.index[k] = i
	}
	o := (&propertyObject{
		kind:  kindObject,
		class: classModuleNamespace,
		flags: objExotic,
		shape: vm.shapes.nullProto,
		ext:   ns,
	}).init()
	tag := "Module"
	if deferred {
		tag = "Deferred Module"
	}
	vm.add(o, vm.WellKnownSymbol(SymbolToStringTag), vm.NewString(tag), 0)
	o.flags |= objNotExtensible
	// The properties of the exports are not in the shape, which no inline cache may share.
	o.makePrivate()
	return o
}

func namespaceOf(o *Object) *moduleNamespace {
	return o.ext.(*moduleNamespace)
}

// isNamespaceExportKey reports whether k is a key that the module namespace object o
// handles as an export name, which is not what IsSymbolLikeNamespaceKey says.
func (vm *VM) isNamespaceExportKey(o *Object, k PropKey) bool {
	if k.IsSymbol() {
		return false
	}
	return !namespaceOf(o).deferred || k.key != vm.Key(keyThen)
}

// namespaceExports implements GetModuleExportsList: it returns the namespace data of the
// module namespace object o, evaluating the module of a deferred one.
func (vm *VM) namespaceExports(o *Object) *moduleNamespace {
	ns := namespaceOf(o)
	if ns.deferred {
		vm.ensureDeferredNamespaceEvaluation(ns.module)
	}
	return ns
}

// namespaceGet implements [[Get]] of the module namespace object o for an export key.
func (vm *VM) namespaceGet(o *Object, k PropKey) Value {
	ns := vm.namespaceExports(o)
	i, ok := ns.index[k]
	if !ok {
		return Undefined
	}
	r := ns.targets[i]
	switch r.index {
	case resolutionNamespace:
		return Value{o: vm.moduleNamespace(r.module)}
	case resolutionSource:
		if r.module.source == nil {
			vm.ThrowReferenceError("the module source of '" + vm.KeyString(k) + "' is not available")
		}
		return Value{o: r.module.source}
	}
	v := *r.module.env[r.index].p
	if v.o == tdzObject {
		vm.ThrowReferenceError("cannot access '" + vm.KeyString(k) + "' before initialization")
	}
	return v
}

// namespaceHas implements [[HasProperty]] of the module namespace object o for an export key.
func (vm *VM) namespaceHas(o *Object, k PropKey) bool {
	_, ok := vm.namespaceExports(o).index[k]
	return ok
}

// namespaceGetOwnProperty implements [[GetOwnProperty]] of the module namespace object o
// for an export key.
func (vm *VM) namespaceGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	if !vm.namespaceHas(o, k) {
		return PropDesc{}, false
	}
	return dataDesc(vm.namespaceGet(o, k), FlagEnumerable|FlagWritable), true
}

// namespaceDefineOwnProperty implements [[DefineOwnProperty]] of the module namespace object o
// for an export key.
func (vm *VM) namespaceDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	cur, ok := vm.namespaceGetOwnProperty(o, k)
	if !ok {
		return false
	}
	if d.has&HasConfigurable != 0 && d.flags&FlagConfigurable != 0 {
		return false
	}
	if d.has&HasEnumerable != 0 && d.flags&FlagEnumerable == 0 {
		return false
	}
	if d.IsAccessor() {
		return false
	}
	if d.has&HasWritable != 0 && d.flags&FlagWritable == 0 {
		return false
	}
	if d.has&HasValue != 0 {
		return SameValue(d.value, cur.value)
	}
	return true
}

// namespaceOwnKeys implements [[OwnPropertyKeys]] of the module namespace object o.
func (vm *VM) namespaceOwnKeys(o *Object) []PropKey {
	keys := slices.Clone(vm.namespaceExports(o).keys)
	for _, k := range o.shape.keys() {
		keys = append(keys, PropKey{key: k})
	}
	return keys
}

// namespaceDelete implements [[Delete]] of the module namespace object o for an export key.
func (vm *VM) namespaceDelete(o *Object, k PropKey) bool {
	return !vm.namespaceHas(o, k)
}
