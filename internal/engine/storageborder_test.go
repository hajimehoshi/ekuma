// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// storageMembers is the list of the functions, variables, methods and fields of the storage package
// that packages above the engine use.
const storageMembers = "storagemembers.txt"

// TestStorageBorder tests that packages above the engine use only the part of the storage package
// in testdata/storagemembers.txt, apart from types and constants. The other names that it exports
// are for the engine.
//
// The test does not see reflection, unsafe, composite literals without keys, test files, and files
// that the build constraints of the running test exclude.
func TestStorageBorder(t *testing.T) {
	prog := checkProgram(t, "../../...")

	b, err := os.ReadFile(filepath.Join("testdata", storageMembers))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for line := range strings.Lines(string(b)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			listed[line] = false
		}
	}

	fields := map[types.Object]string{}
	for _, p := range prog.engine {
		if p.path != storagePath {
			continue
		}
		for _, n := range p.pkg.Scope().Names() {
			obj, ok := p.pkg.Scope().Lookup(n).(*types.TypeName)
			if !ok {
				continue
			}
			if st, ok := obj.Type().Underlying().(*types.Struct); ok {
				for f := range st.Fields() {
					fields[f] = n + "." + f.Name()
				}
			}
		}
	}

	for _, p := range prog.pkgs {
		if p.isEngine() {
			continue
		}
		for id, obj := range p.info.Uses {
			obj = used(obj)
			if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != storagePath {
				continue
			}
			var name string
			switch obj := obj.(type) {
			case *types.Var:
				switch {
				case obj.IsField():
					name = fields[obj]
				case obj.Parent() == obj.Pkg().Scope():
					name = obj.Name()
				default:
					continue
				}
			case *types.Func:
				name = obj.Name()
				if recv := obj.Signature().Recv(); recv != nil {
					name = namedType(recv.Type()).Obj().Name() + "." + name
				}
			default:
				continue
			}
			if _, ok := listed[name]; !ok {
				t.Errorf("%v: use of %s, which is not in %s", p.fset.Position(id.Pos()), name, storageMembers)
			}
			listed[name] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(listed)) {
		if !listed[name] {
			t.Errorf("%s is not used above the engine: remove it from %s", name, storageMembers)
		}
	}
}
