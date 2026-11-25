// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/modulegraph"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// moduleNamespace holds the internal slots of a module namespace exotic object.
type moduleNamespace struct {
	module *Module

	// keys are the [[Exports]] as property keys, sorted in code unit order.
	keys []storage.PropKey

	// targets holds the resolved binding of each of keys.
	targets []modulegraph.Resolution[*Module]

	// index maps a key to its index in keys.
	index map[storage.PropKey]int

	// deferred is the [[Deferred]]: the module is evaluated when the exports are first used.
	deferred bool
}

// moduleNamespace implements GetModuleNamespace for the evaluation phase.
func (cx *ExecutionContext) moduleNamespace(m *Module) *storage.Object {
	if m.namespace == nil {
		m.namespace = cx.newModuleNamespace(m, false)
	}
	return m.namespace
}

// deferredModuleNamespace implements GetModuleNamespace for the defer phase.
func (cx *ExecutionContext) deferredModuleNamespace(m *Module) *storage.Object {
	if m.deferredNamespace == nil {
		m.deferredNamespace = cx.newModuleNamespace(m, true)
	}
	return m.deferredNamespace
}

// newModuleNamespace collects the exports for the requested phase and implements ModuleNamespaceCreate.
func (cx *ExecutionContext) newModuleNamespace(m *Module, deferred bool) *storage.Object {
	exports := m.node.Exports()
	if deferred {
		exports = slices.DeleteFunc(exports, func(e modulegraph.Export[*Module]) bool {
			return e.Name == "then"
		})
	}
	ns := &moduleNamespace{
		module:   m,
		keys:     make([]storage.PropKey, len(exports)),
		targets:  make([]modulegraph.Resolution[*Module], len(exports)),
		index:    make(map[storage.PropKey]int, len(exports)),
		deferred: deferred,
	}
	for i, e := range exports {
		k := cx.ToPropertyKey(storage.ObjectValue(cx.Intern(wtf8.String(e.Name))))
		ns.keys[i] = k
		ns.targets[i] = e.Resolution
		ns.index[k] = i
	}
	o := storage.NewNamespaceObject(cx.RootShape(nil, cx.realmPart, storage.DelegatedRoot), ns)
	tag := wtf8.String("Module")
	if deferred {
		tag = "Deferred Module"
	}
	cx.Add(o, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.NewString(tag), 0)
	return o
}

func namespaceOf(o *storage.Object) *moduleNamespace {
	return o.ExtState().(*moduleNamespace)
}

// isNamespaceExportKey reports whether k is a key that the module namespace object o
// handles as an export name, which is not what IsSymbolLikeNamespaceKey says.
func (ag *agent) isNamespaceExportKey(o *storage.Object, k storage.PropKey) bool {
	if k.IsSymbol() {
		return false
	}
	return !namespaceOf(o).deferred || k.Key != ag.Key(storage.KeyThen)
}

// namespaceExports implements GetModuleExportsList: it returns the namespace data of the
// module namespace object o, evaluating the module of a deferred one.
func (cx *ExecutionContext) namespaceExports(o *storage.Object) *moduleNamespace {
	ns := namespaceOf(o)
	if ns.deferred {
		cx.evaluateModuleSync(ns.module)
	}
	return ns
}

// namespaceGet implements [[Get]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceGet(o *storage.Object, k storage.PropKey) storage.Value {
	ns := cx.namespaceExports(o)
	i, ok := ns.index[k]
	if !ok {
		return storage.Undefined
	}
	r := ns.targets[i]
	switch r.Binding {
	case modulegraph.BindingNamespace:
		return storage.Value{O: cx.moduleNamespace(r.Module)}
	case modulegraph.BindingSource:
		if r.Module.source == nil {
			cx.ThrowReferenceError("the module source of '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' is not available")
		}
		return storage.Value{O: r.Module.source}
	}
	v := *r.Module.env[r.Binding].P
	if v.O == tdzObject {
		cx.ThrowReferenceError("cannot access '" + wtf8.ToValidUTF8(cx.KeyString(k)) + "' before initialization")
	}
	return v
}

// namespaceHas implements [[HasProperty]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceHas(o *storage.Object, k storage.PropKey) bool {
	_, ok := cx.namespaceExports(o).index[k]
	return ok
}

// namespaceGetOwnProperty implements [[GetOwnProperty]] of the module namespace object o
// for an export key.
func (cx *ExecutionContext) namespaceGetOwnProperty(o *storage.Object, k storage.PropKey) (storage.PropDesc, bool) {
	if !cx.namespaceHas(o, k) {
		return storage.PropDesc{}, false
	}
	return storage.DataDesc(cx.namespaceGet(o, k), storage.FlagEnumerable|storage.FlagWritable), true
}

// namespaceDefineOwnProperty implements [[DefineOwnProperty]] of the module namespace object o
// for an export key.
func (cx *ExecutionContext) namespaceDefineOwnProperty(o *storage.Object, k storage.PropKey, d storage.PropDesc) bool {
	cur, ok := cx.namespaceGetOwnProperty(o, k)
	if !ok {
		return false
	}
	if d.HasTrue(storage.HasConfigurable, storage.FlagConfigurable) {
		return false
	}
	if d.HasFalse(storage.HasEnumerable, storage.FlagEnumerable) {
		return false
	}
	if d.IsAccessor() {
		return false
	}
	if d.HasFalse(storage.HasWritable, storage.FlagWritable) {
		return false
	}
	if d.HasField(storage.HasValue) {
		return storage.SameValue(d.Value(), cur.Value())
	}
	return true
}

// namespaceOwnKeys implements [[OwnPropertyKeys]] of the module namespace object o.
func (cx *ExecutionContext) namespaceOwnKeys(o *storage.Object) []storage.PropKey {
	keys := slices.Clone(cx.namespaceExports(o).keys)
	for _, k := range o.Shape().Keys() {
		keys = append(keys, storage.NamedKey(k))
	}
	return keys
}

// namespaceDelete implements [[Delete]] of the module namespace object o for an export key.
func (cx *ExecutionContext) namespaceDelete(o *storage.Object, k storage.PropKey) bool {
	return !cx.namespaceHas(o, k)
}
