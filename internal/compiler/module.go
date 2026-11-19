// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// defaultBinding is the name of the binding of a default export without a name.
const defaultBinding = "*default*"

// CompileModule compiles the program prog from parser.ParseModule. file names the source in
// stack traces. The returned function is the code of the module, and the returned Module
// describes its imports, exports and top-level bindings.
func CompileModule(prog *ast.Program, file string) (f *bytecode.Func, m *bytecode.Module, err error) {
	c := newCompiler(prog, file)
	defer c.release()
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			f, m, err = nil, nil, e
		}
	}()
	f, m = c.compileModule()
	return f, m, nil
}

// moduleRequests collects the module requests of a module.
type moduleRequests struct {
	list []bytecode.ModuleRequest

	// bySpecifier maps a specifier to the indices in list of the requests of it.
	bySpecifier map[string][]int32
}

// add returns the index of the request of the source s with the attributes attrs in the phase.
func (r *moduleRequests) add(s *ast.StringLit, attrs []ast.ImportAttribute, phase ast.ImportPhase) int32 {
	req := bytecode.ModuleRequest{
		Specifier: s.Value,
		Phase:     bytecode.Phase(phase),
	}
	for _, a := range attrs {
		req.Attributes = append(req.Attributes, bytecode.ImportAttribute{
			Key:   a.Key,
			Value: a.Value.Value,
		})
	}
	slices.SortFunc(req.Attributes, func(a, b bytecode.ImportAttribute) int {
		return strings.Compare(a.Key, b.Key)
	})
	indices := r.bySpecifier[req.Specifier]
	for _, i := range indices {
		if q := r.list[i]; slices.Equal(q.Attributes, req.Attributes) && q.Phase == req.Phase {
			return i
		}
	}
	i := int32(len(r.list))
	r.list = append(r.list, req)
	if r.bySpecifier == nil {
		r.bySpecifier = map[string][]int32{}
	}
	r.bySpecifier[req.Specifier] = append(indices, i)
	return i
}

// hasTopLevelAwait reports whether the statements contain await outside of functions.
func hasTopLevelAwait(body []ast.Stmt) bool {
	found := false
	visit := func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Function:
			return false
		case *ast.AwaitExpr:
			found = true
		case *ast.ForOfStmt:
			if n.Await {
				found = true
			}
		case *ast.VarDecl:
			if n.Kind == ast.DeclAwaitUsing {
				found = true
			}
		}
		return !found
	}
	for _, s := range body {
		walk(s, visit)
	}
	return found
}

func (c *compiler) compileModule() (*bytecode.Func, *bytecode.Module) {
	prog := c.prog
	m := &bytecode.Module{
		HasTLA: hasTopLevelAwait(prog.Body),
	}

	// env holds the bindings of the top level. It emits no code: its registers are the indices
	// of the bindings, which the module function reaches as upvalues.
	env := c.newFn(nil, nil, "")
	env.f.Strict = true
	declare := func(name string, kind bindKind, mkind bytecode.ModuleBindingKind) *binding {
		b := env.declare(name, kind)
		if int(b.reg) == len(m.Bindings) {
			m.Bindings = append(m.Bindings, bytecode.ModuleBinding{
				Name: name,
				Kind: mkind,
			})
		}
		return b
	}
	var funcs []ast.Decl
	for _, d := range prog.Scope.Decls {
		switch d.Kind {
		case ast.DeclVar:
			declare(d.Name, bindVar, bytecode.ModuleVar)
		case ast.DeclLet:
			declare(d.Name, bindLet, bytecode.ModuleLexical)
		case ast.DeclConst, ast.DeclUsing, ast.DeclAwaitUsing:
			declare(d.Name, bindConst, bytecode.ModuleLexical)
		case ast.DeclClass:
			declare(d.Name, bindClass, bytecode.ModuleLexical)
		case ast.DeclFunction:
			// The function is instantiated before any code of the module can run.
			declare(d.Name, bindVar, bytecode.ModuleFunction)
			funcs = append(funcs, d)
		case ast.DeclImport:
			// An imported binding is immutable, and reading it checks that it is initialized.
			declare(d.Name, bindConst, bytecode.ModuleImport)
		}
	}

	// The import and export entries, as ParseModule computes them.
	var reqs moduleRequests
	imports := map[string]bytecode.ImportEntry{}
	namespaces := map[string]int32{}
	sources := map[string]int32{}
	type exportEntry struct {
		name  string
		local string
	}
	var locals []exportEntry
	for _, s := range prog.Body {
		switch s := s.(type) {
		case *ast.ImportDecl:
			r := reqs.add(s.Source, s.Attributes, s.Phase)
			for _, sp := range s.Specifiers {
				b, _ := env.lookupLocal(sp.Local.Name)
				switch sp.Kind {
				case ast.ImportNamespace, ast.ImportSource:
					kind := bytecode.ModuleNamespace
					switch {
					case sp.Kind == ast.ImportSource:
						kind = bytecode.ModuleSource
					case s.Phase == ast.PhaseDefer:
						kind = bytecode.ModuleDeferredNamespace
					}
					m.Bindings[b.reg].Kind = kind
					m.Bindings[b.reg].Request = r
					switch kind {
					case bytecode.ModuleNamespace:
						namespaces[sp.Local.Name] = r
					case bytecode.ModuleSource:
						sources[sp.Local.Name] = r
					}
				default:
					name := "default"
					if sp.Kind == ast.ImportNamed {
						name = sp.Imported.Name
					}
					e := bytecode.ImportEntry{
						Request:    r,
						ImportName: name,
						Binding:    b.reg,
					}
					imports[sp.Local.Name] = e
					m.Imports = append(m.Imports, e)
				}
			}
		case *ast.ExportAllDecl:
			r := reqs.add(s.Source, s.Attributes, ast.PhaseEvaluation)
			if s.Exported == nil {
				m.StarExports = append(m.StarExports, r)
				break
			}
			m.IndirectExports = append(m.IndirectExports, bytecode.IndirectExport{
				Name:    s.Exported.Name,
				Request: r,
				All:     true,
			})
		case *ast.ExportNamedDecl:
			switch {
			case s.Decl != nil:
				for _, name := range declaredNames(s.Decl) {
					locals = append(locals, exportEntry{
						name:  name,
						local: name,
					})
				}
			case s.Source != nil:
				r := reqs.add(s.Source, s.Attributes, ast.PhaseEvaluation)
				for _, sp := range s.Specifiers {
					m.IndirectExports = append(m.IndirectExports, bytecode.IndirectExport{
						Name:       sp.Exported.Name,
						Request:    r,
						ImportName: sp.Local.Name,
					})
				}
			default:
				for _, sp := range s.Specifiers {
					locals = append(locals, exportEntry{
						name:  sp.Exported.Name,
						local: sp.Local.Name,
					})
				}
			}
		case *ast.ExportDefaultDecl:
			local := defaultBinding
			switch d := s.Decl.(type) {
			case *ast.FuncDecl:
				if d.Func.Name != nil {
					local = d.Func.Name.Name
				}
			case *ast.ClassDecl:
				if d.Class.Name != nil {
					local = d.Class.Name.Name
				}
			}
			locals = append(locals, exportEntry{
				name:  "default",
				local: local,
			})
		}
	}
	for _, e := range locals {
		if ie, ok := imports[e.local]; ok {
			m.IndirectExports = append(m.IndirectExports, bytecode.IndirectExport{
				Name:       e.name,
				Request:    ie.Request,
				ImportName: ie.ImportName,
			})
			continue
		}
		if r, ok := namespaces[e.local]; ok {
			// A re-export of an imported namespace object.
			m.IndirectExports = append(m.IndirectExports, bytecode.IndirectExport{
				Name:    e.name,
				Request: r,
				All:     true,
			})
			continue
		}
		if r, ok := sources[e.local]; ok {
			// A re-export of an imported module source.
			m.IndirectExports = append(m.IndirectExports, bytecode.IndirectExport{
				Name:    e.name,
				Request: r,
				Source:  true,
			})
			continue
		}
		b, _ := env.lookupLocal(e.local)
		if b == nil {
			c.fail(prog, "export '%s' is not defined in the module", e.local)
		}
		m.LocalExports = append(m.LocalExports, bytecode.LocalExport{
			Name:    e.name,
			Binding: b.reg,
		})
	}
	m.Requests = reqs.list

	// The module function.
	f := c.newFn(env, nil, "")
	f.module = true
	f.f.Strict = true
	f.f.IsAsync = m.HasTLA
	f.f.Source = prog.Source
	f.anyMutated = prog.Scope.ContainsDirectEval
	this := f.declare("this", bindPseudo)
	f.emit(bytecode.OpLoadThis, this.reg, 0, 0)
	f.f.NumClear = int(f.top)

	for _, d := range funcs {
		name := d.Name
		if name == defaultBinding {
			name = "default"
		}
		p := c.compileFunction(f, d.Func, name)
		for _, u := range p.Upvals {
			if u.Local {
				c.fail(d.Func, "function declaration of the module refers to the module function")
			}
		}
		f.f.Protos = append(f.f.Protos, p)
		b, _ := env.lookupLocal(d.Name)
		m.Funcs = append(m.Funcs, bytecode.ModuleFunc{
			Proto:   int32(len(f.f.Protos) - 1),
			Binding: b.reg,
		})
	}

	if using, async := usingKinds(prog.Scope); using {
		f.disposing(async, func() {
			f.stmts(prog.Body)
		})
	} else {
		f.stmts(prog.Body)
	}
	f.emit(bytecode.OpReturnUndef, 0, 0, 0)
	return f.finish(), m
}

// declaredNames returns the names that the declaration s binds.
func declaredNames(s ast.Stmt) []string {
	var names []string
	switch s := s.(type) {
	case *ast.VarDecl:
		for i := range s.Decls {
			patternNames(s.Decls[i].Target, func(id *ast.Identifier) {
				names = append(names, id.Name)
			})
		}
	case *ast.FuncDecl:
		names = append(names, s.Func.Name.Name)
	case *ast.ClassDecl:
		names = append(names, s.Class.Name.Name)
	}
	return names
}

// exportDefault compiles the default export s.
func (f *fn) exportDefault(s *ast.ExportDefaultDecl) {
	switch d := s.Decl.(type) {
	case *ast.FuncDecl:
		// The function was instantiated with the module.
	case *ast.ClassDecl:
		t := f.newReg()
		name := defaultBinding
		if d.Class.Name != nil {
			name = d.Class.Name.Name
		}
		f.class(d.Class, t, "default", -1)
		f.storeVar(s, name, t, true)
	default:
		t := f.newReg()
		f.named(s.Expr, t, "default")
		f.storeVar(s, defaultBinding, t, true)
	}
}

// importCall compiles the dynamic import e into dst.
func (f *fn) importCall(e *ast.ImportCall, dst int32) {
	mark := f.top
	s := f.newReg()
	f.expr(e.Source, s)
	o := f.newRegs(2)
	if e.Options != nil {
		f.expr(e.Options, o)
	} else {
		f.emit(bytecode.OpLoadUndef, o, 0, 0)
	}
	f.emit(bytecode.OpLoadInt, o+1, int32(e.Phase), 0)
	f.setPos(e.Start())
	f.emit(bytecode.OpImport, dst, s, o)
	f.free(mark)
}
