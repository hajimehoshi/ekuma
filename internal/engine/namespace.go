// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/modulegraph"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// moduleNamespace holds the internal slots of a module namespace exotic object.
type moduleNamespace struct {
	module *Module

	// keys are the [[Exports]] as property keys, sorted in code unit order.
	keys []PropKey

	// targets holds the resolved binding of each of keys.
	targets []modulegraph.Resolution[*Module]

	// index maps a key to its index in keys.
	index map[PropKey]int

	// deferred is the [[Deferred]]: the module is evaluated when the exports are first used.
	deferred bool
}

// moduleNamespace implements GetModuleNamespace for the evaluation phase.
func (cx *ExecutionContext) moduleNamespace(m *Module) *Object {
	if m.namespace == nil {
		m.namespace = cx.newModuleNamespace(m, false)
	}
	return m.namespace
}

// deferredModuleNamespace implements GetModuleNamespace for the defer phase.
func (cx *ExecutionContext) deferredModuleNamespace(m *Module) *Object {
	if m.deferredNamespace == nil {
		m.deferredNamespace = cx.newModuleNamespace(m, true)
	}
	return m.deferredNamespace
}

// newModuleNamespace collects the exports for the requested phase and implements ModuleNamespaceCreate.
func (cx *ExecutionContext) newModuleNamespace(m *Module, deferred bool) *Object {
	exports := m.node.Exports()
	if deferred {
		exports = slices.DeleteFunc(exports, func(e modulegraph.Export[*Module]) bool {
			return e.Name == "then"
		})
	}
	ns := &moduleNamespace{
		module:   m,
		keys:     make([]PropKey, len(exports)),
		targets:  make([]modulegraph.Resolution[*Module], len(exports)),
		index:    make(map[PropKey]int, len(exports)),
		deferred: deferred,
	}
	for i, e := range exports {
		k := cx.ToPropertyKey(ObjectValue(cx.Intern(wtf8.String(e.Name))))
		ns.keys[i] = k
		ns.targets[i] = e.Resolution
		ns.index[k] = i
	}
	o := newNamespaceObject(cx.rootShape(nil, cx.realmPart, delegatedRoot), ns)
	tag := wtf8.String("Module")
	if deferred {
		tag = "Deferred Module"
	}
	cx.add(o, cx.WellKnownSymbol(SymbolToStringTag), cx.NewString(tag), 0)
	return o
}

func namespaceOf(o *Object) *moduleNamespace {
	return o.extState().(*moduleNamespace)
}

// isNamespaceExportKey reports whether k is a key that the module namespace object o
// handles as an export name, which is not what IsSymbolLikeNamespaceKey says.
func (ag *agent) isNamespaceExportKey(o *Object, k PropKey) bool {
	if k.IsSymbol() {
		return false
	}
	return !namespaceOf(o).deferred || k.key != ag.Key(KeyThen)
}

// namespaceExports implements GetModuleExportsList: it returns the namespace data of the
// module namespace object o, evaluating the module of a deferred one.
func (cx *ExecutionContext) namespaceExports(o *Object) *moduleNamespace {
	ns := namespaceOf(o)
	if ns.deferred {
		cx.evaluateModuleSync(ns.module)
	}
	return ns
}

// namespaceGet implements [[Get]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceGet(o *Object, k PropKey) Value {
	ns := cx.namespaceExports(o)
	i, ok := ns.index[k]
	if !ok {
		return Undefined
	}
	r := ns.targets[i]
	switch r.Binding {
	case modulegraph.BindingNamespace:
		return Value{o: cx.moduleNamespace(r.Module)}
	case modulegraph.BindingSource:
		if r.Module.source == nil {
			cx.ThrowReferenceError("the module source of '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' is not available")
		}
		return Value{o: r.Module.source}
	}
	v := *r.Module.env[r.Binding].p
	if v.o == tdzObject {
		cx.ThrowReferenceError("cannot access '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' before initialization")
	}
	return v
}

// namespaceHas implements [[HasProperty]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceHas(o *Object, k PropKey) bool {
	_, ok := cx.namespaceExports(o).index[k]
	return ok
}

// namespaceGetOwnProperty implements [[GetOwnProperty]] of the module namespace object o
// for an export key.
func (cx *ExecutionContext) namespaceGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	if !cx.namespaceHas(o, k) {
		return PropDesc{}, false
	}
	return dataDesc(cx.namespaceGet(o, k), FlagEnumerable|FlagWritable), true
}

// namespaceDefineOwnProperty implements [[DefineOwnProperty]] of the module namespace object o
// for an export key.
func (cx *ExecutionContext) namespaceDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	cur, ok := cx.namespaceGetOwnProperty(o, k)
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
func (cx *ExecutionContext) namespaceOwnKeys(o *Object) []PropKey {
	keys := slices.Clone(cx.namespaceExports(o).keys)
	for _, k := range o.shape.keys() {
		keys = append(keys, PropKey{key: k})
	}
	return keys
}

// namespaceDelete implements [[Delete]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceDelete(o *Object, k PropKey) bool {
	return !cx.namespaceHas(o, k)
}
