// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// storageFiles are the files of the storage package that own raw storage, with the reasons. The
// fields of the structs that they declare are raw.
var storageFiles = map[string]string{
	"value.go":          "values",
	"object.go":         "objects",
	"shape.go":          "shapes",
	"inlinecache.go":    "inline caches",
	"builtinobject.go":  "creation of built-in objects",
	"functionobject.go": "creation of functions",
	"propkey.go":        "property keys",
	"propdesc.go":       "property descriptors and accessors",
	"property.go":       "properties",
	"array.go":          "creation and elements of arrays",
	"arrayiteration.go": "iteration of arrays",
	"sparseindex.go":    "indexed properties in shapes",
	"regexpobject.go":   "RegExp objects and match results",
	"weaktable.go":      "entries of weak collections",
	"valuestorage.go":   "slots and elements",
	"string.go":         "strings",
	"stringtable.go":    "shared strings",
	"stringvalue.go":    "strings as values",
	"concat.go":         "strings that concatenations extend",
	"symbol.go":         "symbols",
}

// validationFiles are the files that check raw storage in the ekumavalidate build. They are not
// subject to the list.
var validationFiles = []string{"validate_on.go", "validate_cache_on.go"}

// rawFields are the raw fields of the other structs, with the reasons.
var rawFields = map[string]string{
	"realmPart.intrinsics": "prototypes of new objects",
	"realmPart.shapes":     "shapes of new objects",
	"space.protoEpoch":     "validity of the inline caches",
	"space.strings":        "interned strings",
}

// rawValueTypes are the raw structs that code passes by value. Storing one through a pointer
// changes no object by itself.
var rawValueTypes = []string{"Value", "PropKey", "QueryKey", "PropDesc"}

// rawReads are the fields and the methods that read properties without the guards of the property
// operations: exotic objects, lazy objects, holes and accessors.
var rawReads = []string{
	"objectProperties.slots",
	"objectProperties.elems",
	"shape.lookup",
	"Object.getOwn",
	"Value.accessor",
}

// rawUse is a kind of use of a raw field. The names appear in the list.
type rawUse string

const (
	// rawWrite is a store to a field, to a part of it, or to an element of it, and an append to it.
	rawWrite rawUse = "write"
	// rawRef is a reference that makes a store possible: an address, a slice, the call of a method
	// with a pointer receiver, and a copy of a slice, a map or a pointer to a value.
	rawRef rawUse = "ref"
	// rawLit is a value for a field in a composite literal.
	rawLit rawUse = "lit"
	// rawRead is any other use.
	rawRead rawUse = "read"
)

// rawAccess is a use of a raw field, a raw struct or a method in rawReads.
type rawAccess struct {
	// member is the declaration of the field, the struct or the method.
	member types.Object
	use    rawUse
}

// rawAccesses counts the uses in a declaration.
type rawAccesses struct {
	name   string
	counts map[rawAccess]int
	// pos is the first of the uses in the source.
	pos token.Position

	// section is the section of the list. free is set for a declaration that can use raw storage
	// freely. They are set for the entries of the list only.
	section string
	free    bool
}

// rawUsers is the list of the declarations outside the storage package that use raw storage.
const rawUsers = "rawusers.txt"

// The sections of the list. A declaration is in the first one that fits.
const (
	// rawStores is for declarations that store to raw storage, refer to it, or create objects.
	rawStores = "[stores]"
	// rawReadsOnly is for declarations that read properties.
	rawReadsOnly = "[reads]"
	// rawValues is for declarations that only create values from their parts.
	rawValues = "[values]"
)

// TestRawStorage tests that the declarations outside the storage package that store to raw storage, refer
// to it, create it, or read properties from it are those in testdata/rawusers.txt, so that the list
// can only shrink unless a line of it is edited.
//
// A line has the name of a function or a variable and its uses, in the form member:use or
// member:use*n for n of them. Reads count once: a function that already reads slots can read more
// of them. A name followed by an asterisk can use raw storage freely.
//
// The test does not see what a called function does with a reference passed to it or returned by a
// storage file, stores through pointers and slices kept in local variables apart from the reference
// that the variable was made from, empty composite literals, new and make, reads other than those
// in rawReads, reflection, unsafe, and files that the build constraints of the running test exclude.
//
// Moving a file in storageFiles or renaming something that the lists name fails the test until the
// lists follow. Exporting a declaration or moving it to another of enginePackages does not.
func TestRawStorage(t *testing.T) {
	prog := checkProgram(t, "../../...")
	s := newRawStorage(t, prog)
	got := s.collect(t)
	want := s.readList(t)

	for _, obj := range slices.SortedFunc(maps.Keys(got), func(a, b types.Object) int {
		p, q := got[a].pos, got[b].pos
		return cmp.Or(cmp.Compare(p.Filename, q.Filename), cmp.Compare(p.Offset, q.Offset))
	}) {
		g, w := got[obj], want[obj]
		if w != nil && w.free {
			continue
		}
		line := g.name + " " + s.format(g.counts)
		switch section := s.section(g.counts); {
		case w == nil:
			t.Errorf("%v: uses raw storage and is not in the list, where its line would be in %s:\n%s", g.pos, section, line)
		case !maps.Equal(g.counts, w.counts):
			t.Errorf("%v: uses raw storage differently:\n got %s\nwant %s %s", g.pos, line, w.name, s.format(w.counts))
		case section != w.section:
			t.Errorf("%v: got %s in %s, want %s", g.pos, g.name, w.section, section)
		}
	}
	for obj, w := range want {
		if got[obj] == nil {
			t.Errorf("%s no longer uses raw storage: remove it from %s", w.name, rawUsers)
		}
	}
}

// rawStorage is the raw storage of a program.
type rawStorage struct {
	prog *program
	// names holds the raw fields, the raw structs and the methods in rawReads.
	names map[types.Object]string
	// reads are the members of rawReads.
	reads map[types.Object]bool
	// whole are the raw structs that are no rawValueTypes.
	whole map[*types.Named]bool
	// values are the fields of rawValueTypes.
	values map[types.Object]bool
	value  *types.Named
}

func newRawStorage(t *testing.T, prog *program) *rawStorage {
	s := &rawStorage{
		prog:   prog,
		names:  map[types.Object]string{},
		reads:  map[types.Object]bool{},
		whole:  map[*types.Named]bool{},
		values: map[types.Object]bool{},
		value:  prog.typ(t, "Value"),
	}
	found := map[string]int{}
	var structs []*types.Named
	for _, p := range prog.engine {
		if p.path != storagePath {
			continue
		}
		for _, file := range p.files {
			base := filepath.Base(p.fset.Position(file.Pos()).Filename)
			if _, ok := storageFiles[base]; !ok {
				continue
			}
			found[base]++
			for _, decl := range file.Decls {
				d, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || ts.Assign.IsValid() {
						continue
					}
					named := p.info.Defs[ts.Name].Type().(*types.Named)
					if _, ok := named.Underlying().(*types.Struct); ok {
						structs = append(structs, named)
					}
				}
			}
		}
	}
	for base := range storageFiles {
		if n := found[base]; n != 1 {
			t.Fatalf("got %d files %s, want 1", n, base)
		}
	}
	for _, named := range structs {
		name := named.Obj().Name()
		s.names[named.Obj()] = name
		s.whole[named] = true
		for f := range named.Underlying().(*types.Struct).Fields() {
			// A type declared from a raw struct shares its fields.
			if _, ok := s.names[f]; !ok && f.Name() != "_" {
				s.names[f] = name + "." + f.Name()
			}
		}
	}
	for _, name := range rawValueTypes {
		named := prog.typ(t, name)
		delete(s.whole, named)
		for f := range named.Underlying().(*types.Struct).Fields() {
			s.values[f] = true
		}
	}
	for name := range rawFields {
		typ, field, _ := strings.Cut(name, ".")
		s.names[prog.field(t, typ, field)] = name
	}
	for _, name := range rawReads {
		for _, obj := range s.member(t, name) {
			s.names[obj] = name
			s.reads[obj] = true
		}
	}
	return s
}

// member returns the raw field, the raw struct, or the method in rawReads called name.
func (s *rawStorage) member(t *testing.T, name string) []types.Object {
	t.Helper()
	typ, sel, ok := strings.Cut(name, ".")
	if !ok {
		return []types.Object{s.prog.typ(t, typ).Obj()}
	}
	// A method of another case can have the name of a field.
	if slices.Contains(rawReads, name) && len(s.prog.named(name)) > 0 {
		return s.prog.function(t, name)
	}
	return []types.Object{s.prog.field(t, typ, sel)}
}

// collect returns the uses by the declarations outside the storage files.
func (s *rawStorage) collect(t *testing.T) map[types.Object]*rawAccesses {
	funcNames := map[types.Object]string{}
	for _, fs := range s.prog.funcs {
		for _, f := range fs {
			// A function that stands for a method of another package also has that name.
			if f.obj.Signature().Recv() == nil {
				funcNames[f.obj] = f.obj.Name()
			} else {
				funcNames[f.obj] = f.name
			}
		}
	}
	all := map[types.Object]*rawAccesses{}
	unused := maps.Clone(storageFiles)
	for _, p := range s.prog.pkgs {
		for _, file := range p.files {
			base := filepath.Base(p.fset.Position(file.Pos()).Filename)
			isStorage := p.path == storagePath
			if p.isEngine() && slices.Contains(validationFiles, base) {
				continue
			}
			visit := func(decl ast.Node, owner types.Object, name string) {
				s.inspect(&p, decl, func(n ast.Node, a rawAccess) {
					if isStorage {
						delete(unused, base)
						return
					}
					if !p.isEngine() {
						name = p.path + "." + owner.Name()
					}
					acc := all[owner]
					if acc == nil {
						acc = &rawAccesses{
							name:   name,
							counts: map[rawAccess]int{},
							pos:    p.fset.Position(n.Pos()),
						}
						all[owner] = acc
					}
					if a.use == rawRead {
						acc.counts[a] = 1
						return
					}
					acc.counts[a]++
				})
			}
			for _, decl := range file.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					obj := p.info.Defs[decl.Name]
					visit(decl, obj, cmp.Or(funcNames[obj], obj.(*types.Func).FullName()))
				case *ast.GenDecl:
					for _, spec := range decl.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							visit(vs, p.info.Defs[vs.Names[0]], vs.Names[0].Name)
						}
					}
				}
			}
		}
	}
	for base := range unused {
		t.Errorf("%s does not use raw storage: remove it from storageFiles", base)
	}
	return all
}

// readList returns the entries of the list.
func (s *rawStorage) readList(t *testing.T) map[types.Object]*rawAccesses {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", rawUsers))
	if err != nil {
		t.Fatal(err)
	}
	want := map[types.Object]*rawAccesses{}
	var section, previous string
	for i, line := range strings.Split(string(b), "\n") {
		// A checkout on Windows can end lines with CRLF.
		line = strings.TrimSpace(line)
		pos := fmt.Sprintf("%s:%d", rawUsers, i+1)
		switch {
		case line == "", strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "["):
			section, previous = line, ""
			continue
		}
		name, uses, _ := strings.Cut(line, " ")
		if name <= previous {
			t.Errorf("%s: %s is out of order", pos, name)
		}
		previous = name
		obj := s.declaration(t, name)
		if obj == nil {
			t.Errorf("%s: no declaration %s", pos, name)
			continue
		}
		if want[obj] != nil {
			t.Errorf("%s: %s is listed twice", pos, name)
		}
		acc := &rawAccesses{
			name:    name,
			counts:  map[rawAccess]int{},
			section: section,
			free:    uses == "*",
		}
		want[obj] = acc
		if acc.free {
			continue
		}
		for field := range strings.FieldsSeq(uses) {
			member, use, _ := strings.Cut(field, ":")
			use, times, hasTimes := strings.Cut(use, "*")
			n := 1
			if hasTimes {
				n, err = strconv.Atoi(times)
				if err != nil {
					t.Fatalf("%s: %v", pos, err)
				}
			}
			m := s.member(t, member)[0]
			if _, ok := s.names[m]; !ok {
				t.Errorf("%s: %s is not raw", pos, member)
			}
			acc.counts[rawAccess{
				member: m,
				use:    rawUse(use),
			}] = n
		}
	}
	return want
}

// section returns the section of the list for a declaration with counts.
func (s *rawStorage) section(counts map[rawAccess]int) string {
	section := rawValues
	for a := range counts {
		switch {
		case a.use == rawRead:
			section = rawReadsOnly
		case a.use != rawLit || !s.values[a.member]:
			return rawStores
		}
	}
	return section
}

// declaration returns the function or variable of the engine called name, or nil.
func (s *rawStorage) declaration(t *testing.T, name string) types.Object {
	t.Helper()
	if len(s.prog.named(name)) > 0 {
		return s.prog.implementation(t, name)
	}
	for _, p := range s.prog.engine {
		if v, ok := p.pkg.Scope().Lookup(name).(*types.Var); ok {
			return v
		}
	}
	return nil
}

// format returns counts as in the list.
func (s *rawStorage) format(counts map[rawAccess]int) string {
	var uses []string
	for a, n := range counts {
		use := s.names[a.member] + ":" + string(a.use)
		if n > 1 {
			use = fmt.Sprintf("%s*%d", use, n)
		}
		uses = append(uses, use)
	}
	slices.Sort(uses)
	return strings.Join(uses, " ")
}

// inspect calls report with each use in decl of p.
func (s *rawStorage) inspect(p *checkedPackage, decl ast.Node, report func(n ast.Node, a rawAccess)) {
	field := func(e ast.Expr) types.Object {
		if sel, ok := ast.Unparen(e).(*ast.SelectorExpr); ok {
			return used(p.info.Uses[sel.Sel])
		}
		return nil
	}
	isPointer := func(typ types.Type) bool {
		_, ok := typ.Underlying().(*types.Pointer)
		return ok
	}
	// stored returns the raw field that a store to e changes, or a store to what it leads to, or nil.
	stored := func(e ast.Expr) types.Object {
		for {
			switch x := e.(type) {
			case *ast.ParenExpr:
				e = x.X
			case *ast.IndexExpr:
				e = x.X
			case *ast.SliceExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			case *ast.SelectorExpr:
				f := field(x)
				if _, ok := s.names[f]; ok {
					return f
				}
				if v, ok := f.(*types.Var); !ok || !v.IsField() || isPointer(p.info.TypeOf(x.X)) {
					return nil
				}
				e = x.X
			default:
				return nil
			}
		}
	}
	// whole returns the raw struct that a store to e replaces, or nil.
	whole := func(e ast.Expr) types.Object {
		if named, ok := types.Unalias(p.info.TypeOf(e)).(*types.Named); ok && s.whole[named.Origin()] {
			return named.Origin().Obj()
		}
		return nil
	}
	// refers reports whether a copy of the value of a field of this type makes stores possible that
	// name no raw field.
	refers := func(typ types.Type) bool {
		switch u := typ.Underlying().(type) {
		case *types.Slice, *types.Map:
			return true
		case *types.Pointer:
			return isType(u, s.value)
		}
		return false
	}
	builtin := func(call *ast.CallExpr) string {
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
			if b, ok := p.info.Uses[id].(*types.Builtin); ok {
				return b.Name()
			}
		}
		return ""
	}
	// handled holds the expressions of raw fields that their context accounts for.
	handled := map[ast.Expr]bool{}
	// account marks the raw field that e leads to.
	account := func(e ast.Expr) {
		for {
			e = ast.Unparen(e)
			handled[e] = true
			switch x := e.(type) {
			case *ast.IndexExpr:
				e = x.X
			case *ast.SliceExpr:
				e = x.X
			case *ast.StarExpr:
				e = x.X
			default:
				return
			}
		}
	}
	store := func(e ast.Expr, use rawUse) {
		f := stored(e)
		if f == nil && use == rawWrite {
			f = whole(e)
		}
		if f == nil {
			return
		}
		account(e)
		report(e, rawAccess{
			member: f,
			use:    use,
		})
	}
	// read marks e as used for its content only.
	read := func(e ast.Expr) {
		e = ast.Unparen(e)
		handled[e] = true
		if f := field(e); s.reads[f] {
			report(e, rawAccess{
				member: f,
				use:    rawRead,
			})
		}
	}

	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				for _, l := range n.Lhs {
					store(l, rawWrite)
				}
			}
		case *ast.IncDecStmt:
			store(n.X, rawWrite)
		case *ast.RangeStmt:
			if n.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if e != nil {
						store(e, rawWrite)
					}
				}
			}
			if !handled[ast.Unparen(n.X)] {
				read(n.X)
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				store(n.X, rawRef)
			}
		case *ast.SliceExpr:
			if handled[n] {
				return true
			}
			if _, ok := p.info.TypeOf(n).Underlying().(*types.Slice); ok {
				store(n.X, rawRef)
			}
		case *ast.CompositeLit:
			typ := p.info.TypeOf(n)
			if ptr, ok := typ.Underlying().(*types.Pointer); ok {
				typ = ptr.Elem()
			}
			st, ok := typ.Underlying().(*types.Struct)
			if !ok {
				return true
			}
			for i, e := range n.Elts {
				var f types.Object
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					f = used(p.info.Uses[kv.Key.(*ast.Ident)])
				} else {
					f = st.Field(i).Origin()
				}
				if _, ok := s.names[f]; ok {
					report(e, rawAccess{
						member: f,
						use:    rawLit,
					})
				}
			}
		case *ast.CallExpr:
			switch builtin(n) {
			case "append", "clear", "copy", "delete":
				store(n.Args[0], rawWrite)
				for _, e := range n.Args[1:] {
					if n.Ellipsis.IsValid() || builtin(n) == "copy" {
						// A copy of elements only reads them.
						if s, ok := ast.Unparen(e).(*ast.SliceExpr); ok {
							handled[s] = true
							e = s.X
						}
						if field(e) != nil {
							read(e)
						}
					}
				}
			case "len", "cap":
				read(n.Args[0])
			}
		case *ast.BinaryExpr:
			if n.Op == token.EQL || n.Op == token.NEQ {
				for _, e := range []ast.Expr{n.X, n.Y} {
					if field(e) != nil {
						read(e)
					}
				}
			}
		case *ast.IndexExpr:
			if !handled[n] && field(n.X) != nil {
				read(n.X)
			}
		case *ast.StarExpr:
			if !handled[n] && field(n.X) != nil {
				read(n.X)
			}
		case *ast.SelectorExpr:
			obj := field(n)
			if fn, ok := obj.(*types.Func); ok {
				// The call of a method or a method value.
				r := fn.Signature().Recv()
				if r != nil && isPointer(r.Type()) && !p.info.Types[n.X].IsType() && !isPointer(p.info.TypeOf(n.X)) {
					store(n.X, rawRef)
				}
				if s.reads[obj] {
					report(n, rawAccess{
						member: obj,
						use:    rawRead,
					})
				}
				return true
			}
			if _, ok := s.names[obj]; !ok || handled[n] {
				return true
			}
			use := rawRead
			if refers(obj.Type()) {
				use = rawRef
			}
			if use == rawRef || s.reads[obj] {
				report(n, rawAccess{
					member: obj,
					use:    use,
				})
			}
		}
		return true
	})
}
