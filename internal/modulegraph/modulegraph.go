// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package modulegraph resolves the exports of modules.
package modulegraph

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Node is the node of a module in the graph of modules and the modules that they request. M is the
// type that represents modules outside of the graph.
type Node[M comparable] struct {
	// Module is the module of the node, which no other node has.
	Module M

	// Code describes a source text module, and is nil for a synthetic module.
	Code *bytecode.Module

	// Requested holds the node of the module of each of Code.Requests once that is loaded.
	Requested []*Node[M]

	// exportIndex maps the name of each local export to its index in Code.LocalExports, and
	// that of each indirect export i to -1-i, once a lookup in a long list has built it.
	exportIndex map[string]int
}

// isSourceText reports whether n is the node of a source text module.
func (n *Node[M]) isSourceText() bool {
	return n.Code != nil
}

// minExportIndex is the number of exports from which ownExport looks them up in a map.
const minExportIndex = 16

// hasOwnExport reports whether the source text module of n has a local or indirect export name.
func (n *Node[M]) hasOwnExport(name string) bool {
	local, indirect := n.ownExport(name)
	return local >= 0 || indirect >= 0
}

// ownExport returns the index of the local export name of n in Code.LocalExports and -1, or -1
// and the index of the indirect export name in Code.IndirectExports, or -1 and -1.
func (n *Node[M]) ownExport(name string) (local, indirect int) {
	code := n.Code
	if len(code.LocalExports)+len(code.IndirectExports) < minExportIndex {
		if i := slices.IndexFunc(code.LocalExports, func(e bytecode.LocalExport) bool {
			return e.Name == name
		}); i >= 0 {
			return i, -1
		}
		return -1, slices.IndexFunc(code.IndirectExports, func(e bytecode.IndirectExport) bool {
			return e.Name == name
		})
	}
	if n.exportIndex == nil {
		n.exportIndex = make(map[string]int, len(code.LocalExports)+len(code.IndirectExports))
		// An export name is unique in a module.
		for i, e := range code.LocalExports {
			n.exportIndex[e.Name] = i
		}
		for i, e := range code.IndirectExports {
			n.exportIndex[e.Name] = -1 - i
		}
	}
	i, ok := n.exportIndex[name]
	switch {
	case !ok:
		return -1, -1
	case i >= 0:
		return i, -1
	default:
		return -1, -1 - i
	}
}

// ResolutionKind is the kind of a [Resolution].
type ResolutionKind uint8

const (
	// Null says that the name resolves to no binding, or only circularly.
	Null ResolutionKind = iota

	// Ambiguous says that star exports resolve the name to different bindings.
	Ambiguous

	// Found says that the name resolves to a binding.
	Found
)

// The special values of [Resolution.Binding] for the bindings that are not of the environment.
const (
	// BindingNamespace is the [[BindingName]] namespace.
	BindingNamespace = -1

	// BindingSource is the [[BindingName]] source.
	BindingSource = -2
)

// Resolution is the result of ResolveExport. Module and Binding are set when Kind is [Found].
type Resolution[M comparable] struct {
	Kind   ResolutionKind
	Module M

	// Binding is the index of the binding in [bytecode.Module.Bindings] of a source text module, 0
	// for the default export of a synthetic module, or [BindingNamespace] or [BindingSource].
	Binding int
}

// ResolveExport implements ResolveExport for the export name of the module of n.
func (n *Node[M]) ResolveExport(name string) Resolution[M] {
	type pair struct {
		node *Node[M]
		name string
	}
	resolveSet := map[pair]bool{}
	type frame struct {
		node *Node[M]
		name string

		// star is the index of the star export being resolved, or -1 before the star exports.
		star int

		// found is the starResolution.
		found Resolution[M]
	}
	frames := []frame{{
		node: n,
		name: name,
		star: -1,
	}}
	// ret is the result of the frame that returned last.
	var ret Resolution[M]
	for len(frames) > 0 {
		fr := &frames[len(frames)-1]
		node := fr.node
		if fr.star < 0 {
			if !node.isSourceText() {
				ret = Resolution[M]{}
				if fr.name == "default" {
					ret = Resolution[M]{
						Kind:   Found,
						Module: node.Module,
					}
				}
				frames = frames[:len(frames)-1]
				continue
			}
			p := pair{
				node: node,
				name: fr.name,
			}
			if resolveSet[p] {
				// A circular import request.
				ret = Resolution[M]{}
				frames = frames[:len(frames)-1]
				continue
			}
			resolveSet[p] = true
			local, indirect := node.ownExport(fr.name)
			if local >= 0 {
				ret = Resolution[M]{
					Kind:    Found,
					Module:  node.Module,
					Binding: int(node.Code.LocalExports[local].Binding),
				}
				frames = frames[:len(frames)-1]
				continue
			}
			if j := indirect; j >= 0 {
				e := node.Code.IndirectExports[j]
				imported := node.Requested[e.Request]
				if e.All || e.Source {
					ret = Resolution[M]{
						Kind:    Found,
						Module:  imported.Module,
						Binding: BindingNamespace,
					}
					if e.Source {
						ret.Binding = BindingSource
					}
					frames = frames[:len(frames)-1]
					continue
				}
				// The result of the frame is that of the imported module.
				*fr = frame{
					node: imported,
					name: e.ImportName,
					star: -1,
				}
				continue
			}
			if fr.name == "default" {
				// A default export cannot be provided by an export * from.
				ret = Resolution[M]{}
				frames = frames[:len(frames)-1]
				continue
			}
			fr.star = 0
			fr.found = Resolution[M]{}
		} else {
			// The star export fr.star resolved to ret.
			if ret.Kind == Ambiguous {
				frames = frames[:len(frames)-1]
				continue
			}
			if ret.Kind == Found {
				if fr.found.Kind == Null {
					fr.found = ret
				} else if ret.Module != fr.found.Module || ret.Binding != fr.found.Binding {
					ret = Resolution[M]{Kind: Ambiguous}
					frames = frames[:len(frames)-1]
					continue
				}
			}
			fr.star++
		}
		if fr.star < len(node.Code.StarExports) {
			frames = append(frames, frame{
				node: node.Requested[node.Code.StarExports[fr.star]],
				name: fr.name,
				star: -1,
			})
			continue
		}
		ret = fr.found
		frames = frames[:len(frames)-1]
	}
	return ret
}

// exportedNames implements GetExportedNames for the module of n.
func (n *Node[M]) exportedNames() []string {
	if !n.isSourceText() {
		return []string{"default"}
	}
	visited := map[*Node[M]]bool{}
	seen := map[string]bool{}
	var names []string
	add := func(name string, root bool) {
		if (root || name != "default") && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	stack := []*Node[M]{n}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[node] || !node.isSourceText() {
			// The default export of a synthetic module is not exported by an export * from.
			continue
		}
		visited[node] = true
		root := node == n
		for _, e := range node.Code.LocalExports {
			add(e.Name, root)
		}
		for _, e := range node.Code.IndirectExports {
			add(e.Name, root)
		}
		for _, r := range slices.Backward(node.Code.StarExports) {
			stack = append(stack, node.Requested[r])
		}
	}
	return names
}

// Export is a name that a module exports and the binding that it resolves to.
type Export[M comparable] struct {
	Name       string
	Resolution Resolution[M]
}

// Exports returns the exported names of the module of n that resolve to a binding, sorted in code
// unit order.
func (n *Node[M]) Exports() []Export[M] {
	var exports []Export[M]
	names := n.exportedNames()
	var star map[string]Resolution[M]
	if len(names) >= minStarTable && n.isSourceText() {
		star = n.starTable()
	}
	for _, name := range names {
		var r Resolution[M]
		if star != nil && !n.hasOwnExport(name) {
			// The default export is never provided by a star export, so name is not "default".
			r = star[name]
		} else {
			r = n.ResolveExport(name)
		}
		if r.Kind == Found {
			exports = append(exports, Export[M]{
				Name:       name,
				Resolution: r,
			})
		}
	}
	slices.SortFunc(exports, func(a, b Export[M]) int {
		return wtf8.Compare(wtf8.String(a.Name), wtf8.String(b.Name))
	})
	return exports
}

// minStarTable is the number of export names from which Exports resolves the names that star
// exports provide all at once.
const minStarTable = 32

// starTable returns what ResolveExport gives for each name that the star exports of the module
// of n provide: the names that it lacks resolve to null. It returns nil when the modules that
// star exports reach from n form a cycle, which the table cannot represent.
func (n *Node[M]) starTable() map[string]Resolution[M] {
	// parents counts the star exports of the reached modules that refer to each module, and post
	// lists the reached modules after the modules that their star exports refer to.
	parents := map[*Node[M]]int{}
	var post []*Node[M]
	const (
		visiting = 1
		visited  = 2
	)
	state := map[*Node[M]]int8{n: visiting}
	type frame struct {
		node *Node[M]
		i    int
	}
	frames := []frame{{node: n}}
	for len(frames) > 0 {
		fr := &frames[len(frames)-1]
		node := fr.node
		if !node.isSourceText() || fr.i == len(node.Code.StarExports) {
			state[node] = visited
			post = append(post, node)
			frames = frames[:len(frames)-1]
			continue
		}
		dep := node.Requested[node.Code.StarExports[fr.i]]
		fr.i++
		parents[dep]++
		switch state[dep] {
		case visiting:
			return nil
		case 0:
			state[dep] = visiting
			frames = append(frames, frame{node: dep})
		}
	}

	// provided holds, for each module whose star exports refer to it and that a module still
	// needs, the resolutions of the names that it provides to them.
	provided := map[*Node[M]]map[string]Resolution[M]{}
	for _, node := range post {
		var t map[string]Resolution[M]
		if node.isSourceText() {
			for _, r := range node.Code.StarExports {
				t = mergeStarTable(t, provided, parents, node.Requested[r])
			}
		}
		if node == n {
			return t
		}
		if !node.isSourceText() {
			// A synthetic module provides only a default export, which star exports skip.
			continue
		}
		// The own exports of node hide those of its star exports.
		for _, e := range node.Code.LocalExports {
			if e.Name != "default" {
				t = setStarEntry(t, e.Name, Resolution[M]{
					Kind:    Found,
					Module:  node.Module,
					Binding: int(e.Binding),
				})
			}
		}
		for _, e := range node.Code.IndirectExports {
			if e.Name != "default" {
				t = setStarEntry(t, e.Name, node.ResolveExport(e.Name))
			}
		}
		provided[node] = t
	}
	panic("unreachable")
}

// setStarEntry sets the resolution r of name in the table t, creating t when it is nil, and
// returns t. A null resolution removes name.
func setStarEntry[M comparable](t map[string]Resolution[M], name string, r Resolution[M]) map[string]Resolution[M] {
	if r.Kind == Null {
		delete(t, name)
		return t
	}
	if t == nil {
		t = map[string]Resolution[M]{}
	}
	t[name] = r
	return t
}

// mergeStarTable merges the table that dep provides into t by the rules of the star exports of
// ResolveExport, and returns the merged table. The table of dep is taken over by the last module
// that needs it.
func mergeStarTable[M comparable](t map[string]Resolution[M], provided map[*Node[M]]map[string]Resolution[M], parents map[*Node[M]]int, dep *Node[M]) map[string]Resolution[M] {
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
				t = map[string]Resolution[M]{}
			}
			t[name] = r
		case cur.Kind == Ambiguous:
		case r.Kind == Ambiguous || r.Module != cur.Module || r.Binding != cur.Binding:
			t[name] = Resolution[M]{Kind: Ambiguous}
		}
	}
	return t
}
