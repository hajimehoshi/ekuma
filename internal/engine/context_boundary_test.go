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
	"testing"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

// checkedPackage is a type-checked package.
type checkedPackage struct {
	path  string
	pkg   *types.Package
	fset  *token.FileSet
	files []*ast.File
	info  types.Info
}

// checkPackages type-checks the packages that patterns match, with the build constraints of the
// running test.
func checkPackages(t *testing.T, patterns ...string) []checkedPackage {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go toolchain unavailable")
	}
	// Use the same build constraints and export data as the packages under test.
	args := []string{"list", "-deps", "-export", "-json=Dir,ImportPath,GoFiles,Export,DepOnly"}
	if engine.ValidateExec {
		args = append(args, "-tags=ekumavalidate")
	}
	out, err := exec.Command(goTool, append(args, patterns...)...).Output()
	if err != nil {
		t.Fatal(err)
	}
	type sourcePackage struct {
		Dir, ImportPath, Export string
		GoFiles                 []string
		DepOnly                 bool
	}
	var roots []sourcePackage
	exports := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p sourcePackage
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		exports[p.ImportPath] = p.Export
		if !p.DepOnly {
			roots = append(roots, p)
		}
	}
	fset := token.NewFileSet()
	config := types.Config{
		Importer: importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
			return os.Open(exports[path])
		}),
	}
	var pkgs []checkedPackage
	for _, p := range roots {
		var files []*ast.File
		for _, name := range p.GoFiles {
			file, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, file)
		}
		info := types.Info{
			Types: map[ast.Expr]types.TypeAndValue{},
			Defs:  map[*ast.Ident]types.Object{},
			Uses:  map[*ast.Ident]types.Object{},
		}
		pkg, err := config.Check(p.ImportPath, fset, files, &info)
		if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, checkedPackage{
			path:  p.ImportPath,
			pkg:   pkg,
			fset:  fset,
			files: files,
			info:  info,
		})
	}
	return pkgs
}

func TestExecutionContextBoundary(t *testing.T) {
	const enginePath = "github.com/hajimehoshi/ekuma/internal/engine"
	for _, p := range checkPackages(t, ".", "../builtin/...", "../json") {
		pkg, fset, files, info := p.pkg, p.fset, p.files, p.info
		if p.path == enginePath {
			// Only dispatch, restoration and the host entry point may observe the active binding.
			ag := pkg.Scope().Lookup("agent").Type()
			active, _, _ := types.LookupFieldOrMethod(ag, true, pkg, "active")
			cx := types.NewPointer(pkg.Scope().Lookup("ExecutionContext").Type())
			if method, _, _ := types.LookupFieldOrMethod(cx, true, pkg, "ActiveContext"); method != nil {
				t.Error("ExecutionContext exposes the active context to built-ins")
			}
			activate, _, _ := types.LookupFieldOrMethod(cx, true, pkg, "activate")
			if active == nil || activate == nil {
				t.Fatal("missing execution context dispatch binding")
			}
			for _, file := range files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					var owner string
					if ok {
						owner = fn.Name.Name
					}
					ast.Inspect(decl, func(n ast.Node) bool {
						id, ok := n.(*ast.Ident)
						if !ok {
							return true
						}
						switch info.Uses[id] {
						case active:
							switch owner {
							case "activate", "ActiveContext", "runFrame", "protect":
							default:
								t.Errorf("%v: active context access in %s", fset.Position(id.Pos()), owner)
							}
						case activate:
							switch owner {
							case "NewExecutionContext", "Run", "enter", "enterJob", "enterBoundary", "exec", "runFrame", "callNative", "protect":
							default:
								t.Errorf("%v: context activation in %s", fset.Position(id.Pos()), owner)
							}
						}
						return true
					})
				}
			}
			continue
		}
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				f, ok := n.(*ast.FuncLit)
				if !ok || len(f.Type.Params.List) == 0 || !engineType(info.TypeOf(f.Type.Params.List[0].Type), "ExecutionContext") {
					return true
				}
				ast.Inspect(f.Body, func(n ast.Node) bool {
					id, ok := n.(*ast.Ident)
					if ok {
						if v, ok := info.Uses[id].(*types.Var); ok && !v.IsField() && v.Pos() < f.Pos() && engineType(v.Type(), "ExecutionContext") {
							t.Errorf("%v: callback captures the outer execution context %s", fset.Position(id.Pos()), id.Name)
						}
					}
					return true
				})
				return true
			})
		}
		// Check inferred expressions too, including handles obtained from a factory.
		for expr, tv := range info.Types {
			if engineType(tv.Type, "Host") {
				t.Errorf("%v: built-in uses the engine host handle", fset.Position(expr.Pos()))
			}
			if engineType(tv.Type, "realm") {
				t.Errorf("%v: built-in uses the raw engine realm", fset.Position(expr.Pos()))
			}
			if engineType(tv.Type, "agent") {
				t.Errorf("%v: built-in uses the raw engine agent", fset.Position(expr.Pos()))
			}
		}
	}
}

// engineType reports whether typ names the engine type or points to it.
func engineType(typ types.Type, name string) bool {
	typ = types.Unalias(typ)
	if ptr, ok := typ.(*types.Pointer); ok {
		typ = types.Unalias(ptr.Elem())
	}
	named, ok := typ.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "github.com/hajimehoshi/ekuma/internal/engine" && named.Obj().Name() == name
}

// TestFrameRegisterBoundary confines raw register storage to frame operations,
// interpreter fast paths, call setup and the independent runtime checks.
func TestFrameRegisterBoundary(t *testing.T) {
	p := checkPackages(t, ".")[0]
	member := func(typ, name string) types.Object {
		t.Helper()
		obj, _, _ := types.LookupFieldOrMethod(p.pkg.Scope().Lookup(typ).Type(), true, p.pkg, name)
		if obj == nil {
			t.Fatalf("missing %s.%s", typ, name)
		}
		return obj
	}
	allowed := map[types.Object][]string{
		member("agent", "stack"): {
			"agent.frameRegs", "agent.extraArgs", "agent.reload", "agent.findUpval",
			"NewExecutionContext", "agent.ensureStack", "agent.releaseStack", "agent.clearStale", "agent.pushFrame", "agent.exec",
			"ExecutionContext.callOnStack", "ExecutionContext.callSlow", "ExecutionContext.callBound",
			"ExecutionContext.replaceFrame", "ExecutionContext.unbind",
			"agent.checkStep", "agent.moveStack", "agent.stackUsed", "agent.abandonStack", "agent.holdArgs", "agent.checkIdleStack",
		},
		member("generator", "regs"): {
			"frame.registers", "agent.exec", "agent.checkStep",
			"newGenerator", "generator.findUpval", "ExecutionContext.resume",
		},
		member("frame", "registers"):     {"agent.frameRegs", "agent.exec"},
		member("agent", "findUpval"):     {"agent.captureRegister"},
		member("generator", "findUpval"): {"agent.captureRegister"},
	}
	for _, file := range p.files {
		for _, decl := range file.Decls {
			owner := funcName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if owners, ok := allowed[p.info.Uses[id]]; ok && !slices.Contains(owners, owner) {
						t.Errorf("%v: direct %s access in %s", p.fset.Position(id.Pos()), id.Name, owner)
					}
				}
				return true
			})
		}
	}
}
