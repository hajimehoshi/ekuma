// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

const (
	modulePath = "github.com/hajimehoshi/ekuma"
	enginePath = modulePath + "/internal/engine"

	storagePath = modulePath + "/internal/storage"
)

// enginePackages are the packages that the engine consists of. The rules about the inside of the
// engine name its types, fields and functions without a package. The first package that declares
// a type decides what its name means, so a package that forwards a type precedes the one that it
// forwards to.
var enginePackages = []string{enginePath, storagePath}

// checkedPackage is a type-checked package.
type checkedPackage struct {
	path  string
	pkg   *types.Package
	fset  *token.FileSet
	files []*ast.File
	info  types.Info
}

// isEngine reports whether the engine consists of p among others.
func (p *checkedPackage) isEngine() bool {
	return slices.Contains(enginePackages, p.path)
}

// program is the packages that a test inspects. A declaration is the same types.Object in all of
// them.
type program struct {
	pkgs []checkedPackage
	// engine holds the engine packages in the order of enginePackages.
	engine []checkedPackage
	// funcs holds the functions of the engine packages by their folded names.
	funcs map[string][]*engineFunc
}

// sourceImporter imports the packages checked from source, and the others from export data.
type sourceImporter struct {
	checked map[string]*types.Package
	exports types.Importer
}

func (i *sourceImporter) Import(path string) (*types.Package, error) {
	if p := i.checked[path]; p != nil {
		return p, nil
	}
	return i.exports.Import(path)
}

// checkProgram type-checks the packages that patterns match, which must include the engine, with
// the build constraints of the running test.
func checkProgram(t *testing.T, patterns ...string) *program {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go toolchain unavailable")
	}
	// Use the same build constraints and export data as the packages under test.
	args := []string{"list", "-deps", "-export", "-json=Dir,ImportPath,GoFiles,Export,DepOnly,Deps"}
	if engine.ValidateExec {
		args = append(args, "-tags=ekumavalidate")
	}
	out, err := exec.Command(goTool, append(args, patterns...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	type sourcePackage struct {
		Dir, ImportPath, Export string
		GoFiles, Deps           []string
		DepOnly                 bool
	}
	exports := map[string]string{}
	fset := token.NewFileSet()
	imp := &sourceImporter{
		checked: map[string]*types.Package{},
		exports: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			return os.Open(exports[path])
		}),
	}
	config := types.Config{
		Importer: imp,
	}
	var prog program
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var sp sourcePackage
		if err := dec.Decode(&sp); err != nil {
			t.Fatal(err)
		}
		exports[sp.ImportPath] = sp.Export
		// The export data of a package would bring its own copy of a checked package.
		if sp.DepOnly && !slices.ContainsFunc(sp.Deps, func(path string) bool {
			return imp.checked[path] != nil
		}) {
			continue
		}
		p := checkedPackage{
			path: sp.ImportPath,
			fset: fset,
			info: types.Info{
				Types: map[ast.Expr]types.TypeAndValue{},
				Defs:  map[*ast.Ident]types.Object{},
				Uses:  map[*ast.Ident]types.Object{},
			},
		}
		for _, name := range sp.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(sp.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			p.files = append(p.files, file)
		}
		// The list has dependencies first.
		p.pkg, err = config.Check(p.path, fset, p.files, &p.info)
		if err != nil {
			t.Fatal(err)
		}
		imp.checked[p.path] = p.pkg
		if !sp.DepOnly {
			prog.pkgs = append(prog.pkgs, p)
		}
	}
	for _, path := range enginePackages {
		i := slices.IndexFunc(prog.pkgs, func(p checkedPackage) bool {
			return p.path == path
		})
		if i < 0 {
			t.Fatalf("no package %s", path)
		}
		prog.engine = append(prog.engine, prog.pkgs[i])
	}
	prog.indexFuncs()
	return &prog
}

// fold returns a name with a lower-case first letter. The rules do not tell names that differ only
// in that apart, as exporting a declaration of the engine to another engine package changes it.
func fold(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(r)) + name[n:]
}

// foldMethod is fold for both parts of a name of the form T.m.
func foldMethod(name string) string {
	if t, m, ok := strings.Cut(name, "."); ok {
		return fold(t) + "." + fold(m)
	}
	return fold(name)
}

// namedType returns the type that typ or its element, if it is a pointer, names, or nil.
func namedType(typ types.Type) *types.Named {
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = types.Unalias(ptr.Elem())
	}
	if n, ok := typ.(*types.Named); ok {
		return n.Origin()
	}
	return nil
}

// isType reports whether typ is want, a type declared from want or the type that want is declared
// from, or a pointer to one of these.
func isType(typ types.Type, want *types.Named) bool {
	if typ == nil {
		return false
	}
	n := namedType(typ)
	if n == nil || n == want {
		return n == want
	}
	switch u := n.Underlying().(type) {
	case *types.Struct, *types.Signature:
		// A declaration of one type from another shares this.
		return u == want.Underlying()
	}
	return false
}

// typ returns the engine type called name. If several engine packages declare it, the later ones
// must declare what the first one forwards to.
func (prog *program) typ(t *testing.T, name string) *types.Named {
	t.Helper()
	var found *types.Named
	for _, p := range prog.engine {
		scope := p.pkg.Scope()
		for _, n := range scope.Names() {
			obj, ok := scope.Lookup(n).(*types.TypeName)
			if !ok || fold(n) != fold(name) || n != name && scope.Lookup(name) != nil {
				continue
			}
			named := namedType(obj.Type())
			switch {
			case named == nil:
				t.Fatalf("%v: %s is no defined type", p.fset.Position(obj.Pos()), name)
			case found == nil:
				found = named
			case !isType(named, found):
				t.Fatalf("%v: another type %s", p.fset.Position(obj.Pos()), name)
			}
		}
	}
	if found == nil {
		t.Fatalf("missing type %s", name)
	}
	return found
}

// forms returns the types of the engine packages that isType tells to be typ.
func (prog *program) forms(typ *types.Named) []*types.Named {
	var forms []*types.Named
	for _, p := range prog.engine {
		for _, n := range p.pkg.Scope().Names() {
			obj, ok := p.pkg.Scope().Lookup(n).(*types.TypeName)
			if !ok {
				continue
			}
			if named := namedType(obj.Type()); named != nil && isType(named, typ) && !slices.Contains(forms, named) {
				forms = append(forms, named)
			}
		}
	}
	return forms
}

// field returns the field called name of the engine type called typ.
func (prog *program) field(t *testing.T, typ, name string) *types.Var {
	t.Helper()
	named := prog.typ(t, typ)
	upper := strings.ToUpper(name[:1]) + name[1:]
	for _, name := range []string{name, upper} {
		for _, p := range prog.engine {
			obj, _, _ := types.LookupFieldOrMethod(named, true, p.pkg, name)
			if f, ok := obj.(*types.Var); ok {
				return f.Origin()
			}
		}
	}
	t.Fatalf("missing field %s.%s", typ, name)
	return nil
}

// used returns the declaration that an identifier with the object obj refers to, and nil for nil.
func used(obj types.Object) types.Object {
	switch obj := obj.(type) {
	case *types.Var:
		return obj.Origin()
	case *types.Func:
		return obj.Origin()
	}
	return obj
}

// engineFunc is a declared function or method of an engine package.
type engineFunc struct {
	obj *types.Func
	// name is the name in the rules, not folded.
	name string
	// forwards is set if all that the function does is to call the one of its name in another
	// engine package.
	forwards bool
}

// indexFuncs fills prog.funcs.
func (prog *program) indexFuncs() {
	prog.funcs = map[string][]*engineFunc{}

	// typeName returns the name of an engine type in the first engine package that declares it.
	typeName := func(named *types.Named) string {
		for _, p := range prog.engine {
			scope := p.pkg.Scope()
			for _, n := range scope.Names() {
				obj, ok := scope.Lookup(n).(*types.TypeName)
				if !ok {
					continue
				}
				// Within its own package, only the declaration of a type itself names it.
				if p.pkg == named.Obj().Pkg() {
					if obj == named.Obj() {
						return n
					}
					continue
				}
				if o := namedType(obj.Type()); o != nil && isType(named, o) {
					return n
				}
			}
		}
		return named.Obj().Name()
	}
	names := map[*types.Named]string{}
	cachedName := func(named *types.Named) string {
		if _, ok := names[named]; !ok {
			names[named] = typeName(named)
		}
		return names[named]
	}

	bodies := map[*types.Func]*ast.BlockStmt{}
	for _, p := range prog.engine {
		for _, file := range p.files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				f := p.info.Defs[fn.Name].(*types.Func)
				bodies[f] = fn.Body
				sig := f.Type().(*types.Signature)
				var ns []string
				if r := sig.Recv(); r != nil {
					ns = append(ns, cachedName(namedType(r.Type()))+"."+f.Name())
				} else {
					ns = append(ns, f.Name())
					// A package cannot declare methods of the types of another one.
					if sig.Params().Len() > 0 {
						if named := namedType(sig.Params().At(0).Type()); named != nil && named.Obj().Pkg() != f.Pkg() {
							typ := cachedName(named)
							ns = append(ns, typ+"."+strings.TrimPrefix(f.Name(), typ+"_"))
						}
					}
				}
				for _, name := range ns {
					n := foldMethod(name)
					prog.funcs[n] = append(prog.funcs[n], &engineFunc{
						obj:  f,
						name: name,
					})
				}
			}
		}
	}
	for _, fs := range prog.funcs {
		for _, f := range fs {
			callee := prog.soleCall(f.obj, bodies[f.obj])
			f.forwards = callee != nil && slices.ContainsFunc(fs, func(g *engineFunc) bool {
				return g.obj == callee && callee.Pkg() != f.obj.Pkg()
			})
		}
	}
}

// soleCall returns the function whose call is all that body, the body of f, consists of, or nil.
func (prog *program) soleCall(f *types.Func, body *ast.BlockStmt) *types.Func {
	if body == nil || len(body.List) != 1 {
		return nil
	}
	var e ast.Expr
	switch s := body.List[0].(type) {
	case *ast.ExprStmt:
		e = s.X
	case *ast.ReturnStmt:
		if len(s.Results) == 1 {
			e = s.Results[0]
		}
	}
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return nil
	}
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	}
	for _, p := range prog.engine {
		if p.pkg == f.Pkg() {
			callee, _ := used(p.info.Uses[id]).(*types.Func)
			return callee
		}
	}
	return nil
}

// named returns the functions called name. A name that differs in the case of a first letter counts
// in a package without the name itself.
func (prog *program) named(name string) []*engineFunc {
	fs := prog.funcs[foldMethod(name)]
	return slices.DeleteFunc(slices.Clone(fs), func(f *engineFunc) bool {
		return f.name != name && slices.ContainsFunc(fs, func(g *engineFunc) bool {
			return g.name == name && g.obj.Pkg() == f.obj.Pkg()
		})
	})
}

// function returns the functions called name, which has the form f or T.m: the one that does the
// work, and those that forward to it. T.m is a method of the type T or, for a type T of another
// package, a function m or T_m whose first parameter is of that type.
func (prog *program) function(t *testing.T, name string) []types.Object {
	t.Helper()
	fs := prog.named(name)
	if len(fs) == 0 {
		t.Errorf("no function %s", name)
	}
	prog.checkUnique(t, name)
	var objs []types.Object
	for _, f := range fs {
		objs = append(objs, f.obj)
	}
	return objs
}

// implementation returns the function called name that does the work.
func (prog *program) implementation(t *testing.T, name string) *types.Func {
	t.Helper()
	prog.function(t, name)
	for _, f := range prog.named(name) {
		if !f.forwards {
			return f.obj
		}
	}
	t.Fatalf("no implementation of %s", name)
	return nil
}

// checkUnique tests that no two functions are called name, apart from those that forward.
func (prog *program) checkUnique(t *testing.T, name string) {
	t.Helper()
	var found *types.Func
	for _, f := range prog.named(name) {
		if f.forwards {
			continue
		}
		if found != nil {
			t.Errorf("two functions %s: %s and %s", name, found.FullName(), f.obj.FullName())
		}
		found = f.obj
	}
}

// owner returns the name among names of the function that decl in p declares. For another function
// and one that forwards it returns the full name, and for other declarations the empty string.
func (prog *program) owner(p *checkedPackage, decl ast.Decl, names []string) string {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok {
		return ""
	}
	obj := p.info.Defs[fn.Name].(*types.Func)
	for _, name := range names {
		for _, f := range prog.named(name) {
			if f.obj == obj && !f.forwards {
				return name
			}
		}
	}
	return obj.FullName()
}

// useRule confines the uses of declarations to some functions of the engine packages.
type useRule struct {
	// what describes a use.
	what string
	// objs are a field, or a function and those that forward to it.
	objs []types.Object
	// owners are the functions that may use objs.
	owners []string
}

// checkUses tests that all the packages keep to rules.
func (prog *program) checkUses(t *testing.T, rules ...useRule) {
	t.Helper()
	ruleOf := map[types.Object]useRule{}
	for _, rule := range rules {
		for _, name := range rule.owners {
			prog.checkUnique(t, name)
		}
		for _, obj := range rule.objs {
			ruleOf[obj] = rule
		}
	}
	for _, p := range prog.pkgs {
		for _, file := range p.files {
			for _, decl := range file.Decls {
				var self types.Object
				if fn, ok := decl.(*ast.FuncDecl); ok {
					self = p.info.Defs[fn.Name]
				}
				ast.Inspect(decl, func(n ast.Node) bool {
					id, ok := n.(*ast.Ident)
					if !ok {
						return true
					}
					obj := used(p.info.Uses[id])
					rule, ok := ruleOf[obj]
					if !ok || self != obj && slices.Contains(rule.objs, self) {
						return true
					}
					if owner := prog.owner(&p, decl, rule.owners); !slices.Contains(rule.owners, owner) {
						t.Errorf("%v: %s in %s", p.fset.Position(id.Pos()), rule.what, owner)
					}
					return true
				})
			}
		}
	}
}
