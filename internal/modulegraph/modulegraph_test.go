// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package modulegraph_test

import (
	"fmt"
	"maps"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/modulegraph"
)

// graph holds the nodes of a graph by the names of their modules.
type graph map[string]*modulegraph.Node[string]

// module returns the node of the source text module name, which it creates.
func (g graph) module(name string) *modulegraph.Node[string] {
	if g[name] == nil {
		g[name] = &modulegraph.Node[string]{
			Module: name,
			Code:   &bytecode.Module{},
		}
	}
	return g[name]
}

// request adds a request of dep to n and returns its index.
func request(n, dep *modulegraph.Node[string]) int32 {
	n.Requested = append(n.Requested, dep)
	return int32(len(n.Requested) - 1)
}

// local adds exports of bindings of the module name, whose indices follow the order of exports.
func (g graph) local(name string, exports ...string) {
	code := g.module(name).Code
	for _, e := range exports {
		code.LocalExports = append(code.LocalExports, bytecode.LocalExport{
			Name:    e,
			Binding: int32(len(code.LocalExports)),
		})
	}
}

// indirect adds the export of the binding importName of dep as export to the module name.
func (g graph) indirect(name, export, dep, importName string) {
	n := g.module(name)
	n.Code.IndirectExports = append(n.Code.IndirectExports, bytecode.IndirectExport{
		Name:       export,
		Request:    request(n, g.module(dep)),
		ImportName: importName,
	})
}

// star adds export * from each of deps to the module name.
func (g graph) star(name string, deps ...string) {
	n := g.module(name)
	for _, dep := range deps {
		n.Code.StarExports = append(n.Code.StarExports, request(n, g.module(dep)))
	}
}

// TestExportsOfManyNames checks the exports of a module with enough names that its star exports
// are resolved together when they form no cycle.
func TestExportsOfManyNames(t *testing.T) {
	for _, cyclic := range []bool{false, true} {
		t.Run(fmt.Sprintf("cyclic=%t", cyclic), func(t *testing.T) {
			g := graph{}
			want := map[string]string{}

			g.local("root", "own")
			want["own"] = "root.0"
			g.star("root", "a", "b", "c", "lower", "synthetic")

			var names []string
			for i := range 40 {
				name := fmt.Sprintf("name%d", i)
				names = append(names, name)
				want[name] = fmt.Sprintf("a.%d", i)
			}
			g.local("a", names...)
			g.local("a", "default", "own")

			// a, b and c provide different bindings of clash.
			g.local("a", "clash")
			g.local("b", "clash")
			g.local("c", "clash")

			// b and c provide the same binding of shared.
			g.local("d", "shared", "inner")
			g.star("b", "d")
			g.star("c", "d")
			want["shared"] = "d.0"
			g.indirect("c", "renamed", "d", "inner")
			want["renamed"] = "d.1"
			want["inner"] = "d.1"

			// b and c provide different bindings of d.
			g.indirect("b", "pair", "d", "shared")
			g.indirect("c", "pair", "d", "inner")

			// b provides its own binding, and c that of d.
			g.local("d", "overridden")
			g.local("b", "overridden")

			// The export of b hides that of its star export.
			g.star("b", "e")
			g.local("e", "unused", "hidden")
			g.local("b", "hidden")
			want["hidden"] = "b.2"
			want["unused"] = "e.0"

			// An export that does not resolve hides that of a star export, too.
			g.local("f", "dangling")
			g.star("c", "f")
			g.indirect("c", "dangling", "d", "missing")

			// A name that is ambiguous further down stays ambiguous.
			g.local("lower1", "lowerClash")
			g.local("lower2", "lowerClash")
			g.star("lower", "lower1", "lower2")

			// A synthetic module contributes named exports, including conflicting bindings.
			g["synthetic"].Code = nil
			g["synthetic"].SyntheticExportNames = []string{"default", "host", "hostClash"}
			g.local("e", "hostClash")
			want["host"] = "synthetic.1"

			if cyclic {
				g.star("d", "root")
			}

			got := map[string]string{}
			for _, e := range g["root"].Exports() {
				got[e.Name] = fmt.Sprintf("%s.%d", e.Resolution.Module, e.Resolution.Binding)
			}
			if !maps.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}
