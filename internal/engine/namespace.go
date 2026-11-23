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

// newModuleNamespace collects the exports for the requested phase and implements ModuleNamespaceCreate.
func (vm *VM) newModuleNamespace(m *Module, deferred bool) *Object {
	type export struct {
		name string
		r    resolution
	}
	var exports []export
	names := m.exportedNames()
	var star map[string]resolution
	if len(names) >= minStarTable && m.isCyclic() {
		star = m.starTable()
	}
	for _, n := range names {
		if deferred && n == "then" {
			continue
		}
		var r resolution
		if star != nil && !m.hasOwnExport(n) {
			// The default export is never provided by a star export, so n is not "default".
			r = star[n]
		} else {
			r = m.resolveExport(n)
		}
		if r.kind == resolutionFound {
			exports = append(exports, export{
				name: n,
				r:    r,
			})
		}
	}
	slices.SortFunc(exports, func(a, b export) int {
		return wtf8.Compare(wtf8.String(a.name), wtf8.String(b.name))
	})
	ns := &moduleNamespace{
		module:   m,
		keys:     make([]PropKey, len(exports)),
		targets:  make([]resolution, len(exports)),
		index:    make(map[PropKey]int, len(exports)),
		deferred: deferred,
	}
	for i, e := range exports {
		k := vm.ToPropertyKey(ObjectValue(vm.Intern(wtf8.String(e.name))))
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
	tag := wtf8.String("Module")
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
	return o.extState().(*moduleNamespace)
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
		vm.evaluateModuleSync(ns.module)
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
			vm.ThrowReferenceError(vm.CurrentRealm(), "the module source of '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' is not available")
		}
		return Value{o: r.module.source}
	}
	v := *r.module.env[r.index].p
	if v.o == tdzObject {
		vm.ThrowReferenceError(vm.CurrentRealm(), "cannot access '"+wtf8.ToValidUTF8(vm.KeyString(k))+"' before initialization")
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

// minStarTable is the number of export names from which newModuleNamespace resolves the names
// that star exports provide all at once.
const minStarTable = 32

// starTable returns what ResolveExport gives for each name that the star exports of the module
// m provide: the names that it lacks resolve to null. It returns nil when the modules that star
// exports reach from m form a cycle, which the table cannot represent.
func (m *Module) starTable() map[string]resolution {
	// parents counts the star exports of the reached modules that refer to each module, and post
	// lists the reached modules after the modules that their star exports refer to.
	parents := map[*Module]int{}
	var post []*Module
	const (
		visiting = 1
		visited  = 2
	)
	state := map[*Module]int8{m: visiting}
	type frame struct {
		m *Module
		i int
	}
	frames := []frame{{m: m}}
	for len(frames) > 0 {
		fr := &frames[len(frames)-1]
		mod := fr.m
		if !mod.isCyclic() || fr.i == len(mod.code.StarExports) {
			state[mod] = visited
			post = append(post, mod)
			frames = frames[:len(frames)-1]
			continue
		}
		dep := mod.requested[mod.code.StarExports[fr.i]]
		fr.i++
		parents[dep]++
		switch state[dep] {
		case visiting:
			return nil
		case 0:
			state[dep] = visiting
			frames = append(frames, frame{m: dep})
		}
	}

	// provided holds, for each module whose star exports refer to it and that a module still
	// needs, the resolutions of the names that it provides to them.
	provided := map[*Module]map[string]resolution{}
	for _, mod := range post {
		var t map[string]resolution
		if mod.isCyclic() {
			for _, r := range mod.code.StarExports {
				t = mergeStarTable(t, provided, parents, mod.requested[r])
			}
		}
		if mod == m {
			return t
		}
		if !mod.isCyclic() {
			// A synthetic module provides only a default export, which star exports skip.
			continue
		}
		// The own exports of mod hide those of its star exports.
		for _, e := range mod.code.LocalExports {
			if e.Name != "default" {
				t = setStarEntry(t, e.Name, resolution{
					kind:   resolutionFound,
					module: mod,
					index:  int(e.Binding),
				})
			}
		}
		for _, e := range mod.code.IndirectExports {
			if e.Name != "default" {
				t = setStarEntry(t, e.Name, mod.resolveExport(e.Name))
			}
		}
		provided[mod] = t
	}
	panic("unreachable")
}

// setStarEntry sets the resolution r of name in the table t, creating t when it is nil, and
// returns t. A null resolution removes name.
func setStarEntry(t map[string]resolution, name string, r resolution) map[string]resolution {
	if r.kind == resolutionNull {
		delete(t, name)
		return t
	}
	if t == nil {
		t = map[string]resolution{}
	}
	t[name] = r
	return t
}

// mergeStarTable merges the table that dep provides into t by the rules of the star exports of
// ResolveExport, and returns the merged table. The table of dep is taken over by the last module
// that needs it.
func mergeStarTable(t map[string]resolution, provided map[*Module]map[string]resolution, parents map[*Module]int, dep *Module) map[string]resolution {
	d := provided[dep]
	parents[dep]--
	last := parents[dep] == 0
	if last {
		delete(provided, dep)
	}
	if t == nil && last {
		return d
	}
	for name, r := range d {
		cur, ok := t[name]
		switch {
		case !ok:
			if t == nil {
				t = map[string]resolution{}
			}
			t[name] = r
		case cur.kind == resolutionAmbiguous:
		case r.kind == resolutionAmbiguous || r.module != cur.module || r.index != cur.index:
			t[name] = resolution{kind: resolutionAmbiguous}
		}
	}
	return t
}
