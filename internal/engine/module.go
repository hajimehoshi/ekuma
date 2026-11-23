// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// scriptOrModule is what a Script Record or a Module Record is as the referrer of an import,
// and what the realm is for code that has no ScriptOrModule.
type scriptOrModule struct {
	// file names the script or module, and is empty for the realm.
	file string

	// module is the Module Record, or nil for a script and for the realm.
	module *Module

	// realm is the [[Realm]] of the script or module, or the realm itself.
	realm *Realm

	// loaded is the [[LoadedModules]], by requestKey.
	loaded map[string]*Module
}

// ImportAttribute is an ImportAttribute Record.
type ImportAttribute struct {
	Key   wtf8.String
	Value wtf8.String
}

// ModuleRequest is a ModuleRequest Record. [ModuleRequest.Attributes] are sorted by key.
type ModuleRequest struct {
	Specifier  wtf8.String
	Attributes []ImportAttribute
}

// requestKey returns the key of r in [[LoadedModules]]: requests are equal when their keys are.
func requestKey(r ModuleRequest) string {
	var b strings.Builder
	add := func(s wtf8.String) {
		b.WriteString(strconv.Itoa(len(s)))
		b.WriteByte(':')
		b.WriteString(string(s))
	}
	add(r.Specifier)
	for _, a := range r.Attributes {
		add(a.Key)
		add(a.Value)
	}
	return b.String()
}

// HostLoadImportedModuleFunc implements HostLoadImportedModule synchronously: it returns the
// module that the request req of the referrer resolves to. referrer is the importing module,
// or nil for a script or for code that has neither; file is the name that the importing script
// or module was given, or empty; realm is the [[Realm]] of the importing script or module, or
// the realm for code that has neither. The engine calls it at most once for a request of a referrer
// that completed normally.
//
// The error is an [*Error] for a thrown value, a [*SyntaxError], or any other error, which is
// thrown as an Error with its message.
type HostLoadImportedModuleFunc func(vm *VM, referrer *Module, file string, realm *Realm, req ModuleRequest) (*Module, error)

// HostGetImportMetaPropertiesFunc implements HostGetImportMetaProperties: it returns the
// properties of the object import.meta of the module m, which is created then.
type HostGetImportMetaPropertiesFunc func(vm *VM, m *Module) []ImportMetaProperty

// ImportMetaProperty is a property that HostGetImportMetaProperties gives import.meta.
type ImportMetaProperty struct {
	Key   PropKey
	Value Value
}

// moduleHost is the state of the [VM] for modules.
type moduleHost struct {
	load HostLoadImportedModuleFunc
	meta HostGetImportMetaPropertiesFunc

	// asyncOrder is the last [[AsyncEvaluationOrder]] that was given out.
	asyncOrder int

	// evaluating holds the modules whose evaluation started, in the order it did. It includes
	// every module that is evaluating or evaluating-async, and may include evaluated ones.
	evaluating []*Module
}

// SetHostLoadImportedModule sets the implementation of HostLoadImportedModule. Without one,
// every import fails with a TypeError.
func (vm *VM) SetHostLoadImportedModule(f HostLoadImportedModuleFunc) {
	vm.modules.load = f
}

// SetHostGetImportMetaProperties sets the implementation of HostGetImportMetaProperties.
// Without one, import.meta has no properties.
func (vm *VM) SetHostGetImportMetaProperties(f HostGetImportMetaPropertiesFunc) {
	vm.modules.meta = f
}

type moduleStatus uint8

const (
	moduleNew moduleStatus = iota
	moduleUnlinked
	moduleLinking
	moduleLinked
	moduleEvaluating
	moduleEvaluatingAsync
	moduleEvaluated
)

// The special values of Module.asyncOrder besides the positive orders.
const (
	asyncOrderUnset = 0
	asyncOrderDone  = -1
)

// Module is a Module Record: a Source Text Module Record, or a Synthetic Module Record whose only
// export is default.
type Module struct {
	vm          *VM
	ref         scriptOrModule
	hostDefined any

	// code describes a source text module, and is nil for a synthetic module.
	code *bytecode.Module
	body *funcProto

	// env holds the bindings of the top level, in the order of code.Bindings. A binding of an
	// import is the binding of the exporting module, and fn has the bindings as its upvalues.
	env []*upvalue
	fn  *Object

	// requested holds the module of each of code.Requests once it is loaded.
	requested []*Module

	status moduleStatus

	// loading reports whether a walk of loadModuleGraph has visited m and not finished.
	loading bool

	// metaStarted is true during import.meta initialization and after it succeeds.
	metaStarted bool

	// evalError is the [[EvaluationError]] when hasError is set.
	evalError Value
	hasError  bool

	dfsIndex         int
	dfsAncestorIndex int
	cycleRoot        *Module

	// asyncOrder is the [[AsyncEvaluationOrder]]: asyncOrderUnset, asyncOrderDone or a positive order.
	asyncOrder int

	topLevelCapability *PromiseCapability
	asyncParents       []*Module
	pendingAsyncDeps   int

	namespace         *Object
	deferredNamespace *Object
	meta              *Object

	// source is the [[ModuleSource]], or nil when m has none.
	source *Object

	// exportIndex maps the name of each local export to its index in code.LocalExports, and
	// that of each indirect export i to -1-i, once a lookup in a long list has built it.
	exportIndex map[string]int
}

// minExportIndex is the number of exports from which ownExport looks them up in a map.
const minExportIndex = 16

// hasOwnExport reports whether the source text module m has a local or indirect export name.
func (m *Module) hasOwnExport(name string) bool {
	local, indirect := m.ownExport(name)
	return local >= 0 || indirect >= 0
}

// ownExport returns the index of the local export name of m in code.LocalExports and -1, or -1
// and the index of the indirect export name in code.IndirectExports, or -1 and -1.
func (m *Module) ownExport(name string) (local, indirect int) {
	code := m.code
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
	if m.exportIndex == nil {
		m.exportIndex = make(map[string]int, len(code.LocalExports)+len(code.IndirectExports))
		// An export name is unique in a module.
		for i, e := range code.LocalExports {
			m.exportIndex[e.Name] = i
		}
		for i, e := range code.IndirectExports {
			m.exportIndex[e.Name] = -1 - i
		}
	}
	i, ok := m.exportIndex[name]
	switch {
	case !ok:
		return -1, -1
	case i >= 0:
		return i, -1
	default:
		return -1, -1 - i
	}
}

// HostDefined returns the [[HostDefined]] of m.
func (m *Module) HostDefined() any {
	return m.hostDefined
}

// newCell returns a binding that holds v and refers to no register.
func newCell(v Value) *upvalue {
	u := &upvalue{val: v}
	u.p = &u.val
	return u
}

// ParseModule implements ParseModule for the source text src. file names the module in error
// messages and stack traces, and hostDefined is the [[HostDefined]]. The error is a [*SyntaxError].
func (vm *VM) ParseModule(file, src string, hostDefined any) (*Module, error) {
	prog, err := parser.ParseModule(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, code, err := compiler.CompileModule(prog, file)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	m := &Module{
		vm:          vm,
		hostDefined: hostDefined,
		code:        code,
		env:         make([]*upvalue, len(code.Bindings)),
		requested:   make([]*Module, len(code.Requests)),
	}
	m.ref = scriptOrModule{
		file:   file,
		module: m,
		realm:  vm.curRealm,
	}
	m.body = vm.link(bc, &m.ref)
	for i := range m.env {
		m.env[i] = newCell(tdz)
	}
	return m, nil
}

// CreateDefaultExportSyntheticModule implements CreateDefaultExportSyntheticModule: it returns
// a module whose default export is v. hostDefined is the [[HostDefined]].
func (vm *VM) CreateDefaultExportSyntheticModule(v Value, hostDefined any) *Module {
	m := &Module{
		vm:          vm,
		hostDefined: hostDefined,
		env:         []*upvalue{newCell(v)},
		status:      moduleEvaluated,
	}
	m.ref = scriptOrModule{
		module: m,
		realm:  vm.curRealm,
	}
	return m
}

// SetModuleSource makes source, a module source object, the [[ModuleSource]] of m, which
// source phase imports of m give. m must not be a source text module.
func (m *Module) SetModuleSource(source *Object) {
	if m.isCyclic() {
		panic("engine: a source text module cannot have a module source")
	}
	m.source = source
}

// getModuleSource implements GetModuleSource. ok is false when m has no module source, and
// the result is the error to throw then.
func (vm *VM) getModuleSource(m *Module) (Value, bool) {
	if m.source == nil {
		return vm.NewError(vm.CurrentRealm(), ErrorKindSyntax, "a source phase import is not available for this module"), false
	}
	return Value{o: m.source}, true
}

// isCyclic reports whether m is a Cyclic Module Record.
func (m *Module) isCyclic() bool {
	return m.code != nil
}

// LoadRequestedModules implements LoadRequestedModules for a host whose loading completes
// synchronously: it loads the modules that m and its dependencies request. Once it has
// completed for m, it does nothing. A thrown exception is reported as an [*Error].
func (vm *VM) LoadRequestedModules(m *Module) error {
	saved := vm.curRealm
	if r := m.ref.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	_, err := vm.enter(func(ExecutionContext) Value {
		if e, ok := vm.loadModuleGraph(m); !ok {
			vm.Throw(e)
		}
		return Undefined
	})
	return err
}

// Link implements Link, after [VM.LoadRequestedModules] when m is not loaded. Once m is
// linked, it does nothing. A thrown exception is reported as an [*Error].
func (vm *VM) Link(m *Module) error {
	saved := vm.curRealm
	if r := m.ref.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	_, err := vm.enter(func(ExecutionContext) Value {
		vm.loadAndLink(m)
		return Undefined
	})
	return err
}

// loadAndLink loads and links m unless that is done, and throws the error of a failure.
func (vm *VM) loadAndLink(m *Module) {
	if e, ok := vm.loadModuleGraph(m); !ok {
		vm.Throw(e)
	}
	if e, ok := vm.linkModule(m); !ok {
		vm.Throw(e)
	}
}

// Evaluate implements Evaluate, after [VM.Link] when m is not linked. It returns the promise of the evaluation. A failure to load or
// link, which comes before any module code runs, is reported as an [*Error].
func (vm *VM) Evaluate(m *Module) (Value, error) {
	saved := vm.curRealm
	if r := m.ref.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	return vm.enter(func(ExecutionContext) Value {
		vm.loadAndLink(m)
		return vm.evaluateModule(m)
	})
}

// GetModuleNamespace implements GetModuleNamespace for the evaluation phase, after [VM.Link]
// when m is not linked. A thrown exception is reported as an [*Error].
func (vm *VM) GetModuleNamespace(m *Module) (Value, error) {
	saved := vm.curRealm
	if r := m.ref.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	return vm.enter(func(ExecutionContext) Value {
		vm.loadAndLink(m)
		return Value{o: vm.moduleNamespace(m)}
	})
}

// ParseJSONModule implements ParseJSONModule for the source text src. hostDefined is the
// [[HostDefined]]. A src that is not JSON is reported as an [*Error] holding the JavaScript
// SyntaxError.
func (vm *VM) ParseJSONModule(src string, hostDefined any) (*Module, error) {
	v, err := vm.enter(func(ExecutionContext) Value {
		return vm.Call(vm.CurrentRealm().Intrinsic(IntrinsicJSONParse), Undefined, vm.StringFromGo(src))
	})
	if err != nil {
		return nil, err
	}
	return vm.CreateDefaultExportSyntheticModule(v, hostDefined), nil
}

// activeScriptOrModule implements GetActiveScriptOrModule, with the realm standing for null.
func (vm *VM) activeScriptOrModule() *scriptOrModule {
	for _, fr := range slices.Backward(vm.frames) {
		if s := fr.fp.script; s != nil {
			return s
		}
	}
	return vm.curRealm.referrer
}

// errorValue returns the value to throw for an error of the host.
func (vm *VM) errorValue(r *Realm, err error) Value {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Value
	}
	if e, ok := errors.AsType[*SyntaxError](err); ok {
		return vm.NewError(r, ErrorKindSyntax, e.messageWithPosition())
	}
	return vm.NewGoError(r, err)
}

// loadImportedModule returns the module of the request req of the referrer ref from its
// [[LoadedModules]], or loads it with HostLoadImportedModule and records it. ok is false
// when loading fails, and the result is the error to throw then.
func (vm *VM) loadImportedModule(ref *scriptOrModule, req ModuleRequest) (m *Module, e Value, ok bool) {
	k := requestKey(req)
	if m := ref.loaded[k]; m != nil {
		return m, Undefined, true
	}
	r := ref.realm
	if vm.modules.load == nil {
		return nil, vm.NewError(r, ErrorKindType, "cannot import '"+wtf8.ToValidUTF8(req.Specifier)+"': there is no module loader"), false
	}
	m, err := vm.modules.load(vm, ref.module, ref.file, r, req)
	vm.ResumeInterruption()
	if err != nil {
		return nil, vm.errorValue(r, err), false
	}
	if m == nil {
		return nil, vm.NewError(r, ErrorKindType, "cannot import '"+wtf8.ToValidUTF8(req.Specifier)+"'"), false
	}
	if ref.loaded == nil {
		ref.loaded = map[string]*Module{}
	}
	ref.loaded[k] = m
	return m, Undefined, true
}

// allImportAttributesSupported implements AllImportAttributesSupported.
func allImportAttributesSupported(attrs []ImportAttribute) bool {
	for _, a := range attrs {
		if a.Key != "type" {
			return false
		}
	}
	return true
}

// moduleRequest converts the request of compiled code to a [ModuleRequest].
func moduleRequest(r bytecode.ModuleRequest) ModuleRequest {
	req := ModuleRequest{
		Specifier: r.Specifier,
	}
	for _, a := range r.Attributes {
		req.Attributes = append(req.Attributes, ImportAttribute(a))
	}
	return req
}

// loadModuleGraph implements LoadRequestedModules and InnerModuleLoading for a host whose
// loading completes synchronously. ok is false when loading fails, and the result is the
// error that rejects the promise of LoadRequestedModules then.
//
// Loading fails when it reaches a module that an unfinished call, such as one that the
// module loader of the host entered from, is loading.
func (vm *VM) loadModuleGraph(root *Module) (Value, bool) {
	type frame struct {
		m *Module
		i int
	}
	visited := map[*Module]bool{}
	var order []*Module
	defer func() {
		for _, m := range order {
			m.loading = false
		}
	}()
	var stack []frame
	visit := func(m *Module) bool {
		if !m.isCyclic() || m.status != moduleNew || visited[m] {
			return true
		}
		if m.loading {
			return false
		}
		visited[m] = true
		m.loading = true
		order = append(order, m)
		stack = append(stack, frame{m: m})
		return true
	}
	if !visit(root) {
		return vm.loadingError(root.ref.realm, root), false
	}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.i == len(top.m.code.Requests) {
			stack = stack[:len(stack)-1]
			continue
		}
		m, i := top.m, top.i
		top.i++
		req := moduleRequest(m.code.Requests[i])
		if !allImportAttributesSupported(req.Attributes) {
			return vm.NewError(m.ref.realm, ErrorKindSyntax, "unsupported import attribute"), false
		}
		dep, e, ok := vm.loadImportedModule(&m.ref, req)
		if !ok {
			return e, false
		}
		m.requested[i] = dep
		if !visit(dep) {
			return vm.loadingError(m.ref.realm, dep), false
		}
	}
	for _, m := range order {
		if m.status == moduleNew {
			m.status = moduleUnlinked
		}
	}
	return Undefined, true
}

// loadingError returns the TypeError of loading m while an unfinished call is loading it.
func (vm *VM) loadingError(r *Realm, m *Module) Value {
	return vm.NewError(r, ErrorKindType, "cannot load '"+m.ref.file+"' while it is being loaded")
}

// loadRequestedModules implements LoadRequestedModules: it returns its promise.
func (vm *VM) loadRequestedModules(m *Module) Value {
	c := vm.newIntrinsicCapability()
	if e, ok := vm.loadModuleGraph(m); ok {
		vm.ResolveCapability(c, Undefined)
	} else {
		vm.RejectCapability(c, e)
	}
	return Value{o: c.promise}
}

// linkModule implements Link and InnerModuleLinking. ok is false when linking fails, and the
// result is the error to throw then.
func (vm *VM) linkModule(root *Module) (Value, bool) {
	if !root.isCyclic() {
		return Undefined, true
	}
	type frame struct {
		m *Module
		i int
	}
	var stack []*Module
	var frames []frame
	index := 0
	enter := func(m *Module) {
		if !m.isCyclic() || m.status != moduleUnlinked {
			return
		}
		m.status = moduleLinking
		m.dfsIndex = index
		m.dfsAncestorIndex = index
		index++
		stack = append(stack, m)
		frames = append(frames, frame{m: m})
	}
	enter(root)
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		m := top.m
		if top.i < len(m.requested) {
			dep := m.requested[top.i]
			top.i++
			if dep.isCyclic() && dep.status == moduleUnlinked {
				enter(dep)
				continue
			}
			if dep.isCyclic() && dep.status == moduleLinking {
				m.dfsAncestorIndex = min(m.dfsAncestorIndex, dep.dfsAncestorIndex)
			}
			continue
		}
		frames = frames[:len(frames)-1]
		if e, ok := vm.initializeEnvironment(m); !ok {
			for _, s := range stack {
				s.status = moduleUnlinked
			}
			return e, false
		}
		if m.dfsAncestorIndex == m.dfsIndex {
			for {
				s := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				s.status = moduleLinked
				if s == m {
					break
				}
			}
		}
		// The caller continues with the minimum of the ancestor indices, as after the
		// recursive call of InnerModuleLinking.
		if n := len(frames); n > 0 {
			if p := frames[n-1].m; m.status == moduleLinking {
				p.dfsAncestorIndex = min(p.dfsAncestorIndex, m.dfsAncestorIndex)
			}
		}
	}
	return Undefined, true
}

// resolutionKind is the kind of the result of ResolveExport.
type resolutionKind uint8

const (
	resolutionNull resolutionKind = iota
	resolutionAmbiguous
	resolutionFound
)

// The special values of resolution.index for the bindings that are not of the environment.
const (
	// resolutionNamespace is the [[BindingName]] namespace.
	resolutionNamespace = -1

	// resolutionSource is the [[BindingName]] source.
	resolutionSource = -2
)

// resolution is the result of ResolveExport: for resolutionFound, the binding env[index]
// of module, or the namespace object or the module source of module when index is
// resolutionNamespace or resolutionSource.
type resolution struct {
	kind   resolutionKind
	module *Module
	index  int
}

// resolveExport implements ResolveExport for the export name of m.
func (m *Module) resolveExport(name string) resolution {
	type pair struct {
		m    *Module
		name string
	}
	resolveSet := map[pair]bool{}
	type frame struct {
		m    *Module
		name string

		// star is the index of the star export being resolved, or -1 before the star exports.
		star int

		// found is the starResolution.
		found resolution
	}
	frames := []frame{{
		m:    m,
		name: name,
		star: -1,
	}}
	// ret is the result of the frame that returned last.
	var ret resolution
	for len(frames) > 0 {
		fr := &frames[len(frames)-1]
		mod := fr.m
		if fr.star < 0 {
			if !mod.isCyclic() {
				ret = resolution{}
				if fr.name == "default" {
					ret = resolution{
						kind:   resolutionFound,
						module: mod,
					}
				}
				frames = frames[:len(frames)-1]
				continue
			}
			p := pair{
				m:    mod,
				name: fr.name,
			}
			if resolveSet[p] {
				// A circular import request.
				ret = resolution{}
				frames = frames[:len(frames)-1]
				continue
			}
			resolveSet[p] = true
			local, indirect := mod.ownExport(fr.name)
			if local >= 0 {
				ret = resolution{
					kind:   resolutionFound,
					module: mod,
					index:  int(mod.code.LocalExports[local].Binding),
				}
				frames = frames[:len(frames)-1]
				continue
			}
			if j := indirect; j >= 0 {
				e := mod.code.IndirectExports[j]
				imported := mod.requested[e.Request]
				if e.All || e.Source {
					ret = resolution{
						kind:   resolutionFound,
						module: imported,
						index:  resolutionNamespace,
					}
					if e.Source {
						ret.index = resolutionSource
					}
					frames = frames[:len(frames)-1]
					continue
				}
				// The result of the frame is that of the imported module.
				*fr = frame{
					m:    imported,
					name: e.ImportName,
					star: -1,
				}
				continue
			}
			if fr.name == "default" {
				// A default export cannot be provided by an export * from.
				ret = resolution{}
				frames = frames[:len(frames)-1]
				continue
			}
			fr.star = 0
			fr.found = resolution{}
		} else {
			// The star export fr.star resolved to ret.
			if ret.kind == resolutionAmbiguous {
				frames = frames[:len(frames)-1]
				continue
			}
			if ret.kind == resolutionFound {
				if fr.found.kind == resolutionNull {
					fr.found = ret
				} else if ret.module != fr.found.module || ret.index != fr.found.index {
					ret = resolution{kind: resolutionAmbiguous}
					frames = frames[:len(frames)-1]
					continue
				}
			}
			fr.star++
		}
		if fr.star < len(mod.code.StarExports) {
			frames = append(frames, frame{
				m:    mod.requested[mod.code.StarExports[fr.star]],
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

// exportedNames implements GetExportedNames.
func (m *Module) exportedNames() []string {
	if !m.isCyclic() {
		return []string{"default"}
	}
	visited := map[*Module]bool{}
	seen := map[string]bool{}
	var names []string
	add := func(n string, root bool) {
		if (root || n != "default") && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	stack := []*Module{m}
	for len(stack) > 0 {
		mod := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[mod] || !mod.isCyclic() {
			// The default export of a synthetic module is not exported by an export * from.
			continue
		}
		visited[mod] = true
		root := mod == m
		for _, e := range mod.code.LocalExports {
			add(e.Name, root)
		}
		for _, e := range mod.code.IndirectExports {
			add(e.Name, root)
		}
		for _, r := range slices.Backward(mod.code.StarExports) {
			stack = append(stack, mod.requested[r])
		}
	}
	return names
}

// initializeEnvironment implements InitializeEnvironment for the source text module m.
func (vm *VM) initializeEnvironment(m *Module) (Value, bool) {
	saved := vm.curRealm
	if r := m.ref.realm; r != saved {
		vm.switchRealm(r)
		defer vm.switchRealm(saved)
	}
	code := m.code
	for _, e := range code.IndirectExports {
		if r := m.resolveExport(e.Name); r.kind != resolutionFound {
			return vm.resolutionError(m, e.Name, r), false
		}
	}
	// The bindings of the module itself are those that importers already refer to.
	for i, b := range code.Bindings {
		switch b.Kind {
		case bytecode.ModuleVar:
			*m.env[i].p = Undefined
		case bytecode.ModuleLexical, bytecode.ModuleFunction:
			*m.env[i].p = tdz
		}
	}
	for _, ie := range code.Imports {
		imported := m.requested[ie.Request]
		r := imported.resolveExport(ie.ImportName)
		if r.kind != resolutionFound {
			return vm.resolutionError(m, ie.ImportName, r), false
		}
		switch r.index {
		case resolutionNamespace:
			m.env[ie.Binding] = newCell(Value{o: vm.moduleNamespace(r.module)})
		case resolutionSource:
			v, ok := vm.getModuleSource(r.module)
			if !ok {
				return v, false
			}
			m.env[ie.Binding] = newCell(v)
		default:
			m.env[ie.Binding] = r.module.env[r.index]
		}
	}
	for i, b := range code.Bindings {
		switch b.Kind {
		case bytecode.ModuleNamespace:
			m.env[i] = newCell(Value{o: vm.moduleNamespace(m.requested[b.Request])})
		case bytecode.ModuleDeferredNamespace:
			// A circular importer may already refer to this cell.
			*m.env[i].p = Value{o: vm.deferredModuleNamespace(m.requested[b.Request])}
		case bytecode.ModuleSource:
			v, ok := vm.getModuleSource(m.requested[b.Request])
			if !ok {
				return v, false
			}
			m.env[i] = newCell(v)
		}
	}

	upvals := make([]*upvalue, len(m.body.Upvals))
	for j, d := range m.body.Upvals {
		upvals[j] = m.env[d.Index]
	}
	m.fn = (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.curRealm.shapes.function,
		fn: &function{
			code:   m.body,
			upvals: upvals,
		},
	}).init()
	for _, f := range code.Funcs {
		*m.env[f.Binding].p = Value{o: vm.newClosure(m.body.protos[f.Proto], upvals)}
	}
	return Undefined, true
}

// resolutionError returns the SyntaxError of the import or export name of m that did not
// resolve to r.
func (vm *VM) resolutionError(m *Module, n string, r resolution) Value {
	if r.kind == resolutionAmbiguous {
		return vm.NewError(m.ref.realm, ErrorKindSyntax, "the export '"+n+"' is ambiguous ("+m.ref.file+")")
	}
	return vm.NewError(m.ref.realm, ErrorKindSyntax, "the export '"+n+"' cannot be resolved ("+m.ref.file+")")
}

// newClosure returns a closure of p whose enclosing function has the upvalues upvals. p must
// not refer to registers of the enclosing function.
func (vm *VM) newClosure(p *funcProto, upvals []*upvalue) *Object {
	fn := &function{
		code:             p,
		constructor:      p.isConstructor,
		classConstructor: p.isClassConstructor,
		derived:          p.isDerived,
	}
	o := (&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.curRealm.shapes.function,
		fn:    fn,
	}).init()
	if p.isGenerator || p.isAsync {
		o.shape = vm.functionShapeFor(p.closurePrototype())
	}
	if n := len(p.Upvals); n > 0 {
		fn.upvals = make([]*upvalue, n)
		for j, d := range p.Upvals {
			fn.upvals[j] = upvals[d.Index]
		}
	}
	return o
}

// evaluateModule implements Evaluate: it returns the promise of the evaluation of m.
func (vm *VM) evaluateModule(m *Module) Value {
	if !m.isCyclic() {
		c := vm.newIntrinsicCapability()
		vm.ResolveCapability(c, Undefined)
		return Value{o: c.promise}
	}
	if m.status == moduleEvaluatingAsync || m.status == moduleEvaluated {
		if m.cycleRoot != nil {
			m = m.cycleRoot
		}
	}
	if m.topLevelCapability != nil {
		return Value{o: m.topLevelCapability.promise}
	}
	vm.modules.evaluating = slices.DeleteFunc(vm.modules.evaluating, func(mod *Module) bool {
		return mod.status == moduleEvaluated
	})
	c := vm.newIntrinsicCapability()
	m.topLevelCapability = c
	var stack []*Module
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*xerrors.InternalError); ok {
				vm.rejectModuleEvaluations(stack, e)
			} else if isHostPanic(r) {
				vm.rejectModuleEvaluations(stack, &PanicError{
					Value: r,
				})
			}
			panic(r)
		}
	}()
	if e, ok := vm.innerModuleEvaluation(m, &stack); !ok {
		for _, s := range stack {
			s.status = moduleEvaluated
			s.evalError = e
			s.hasError = true
		}
		vm.RejectCapability(c, e)
	} else if m.asyncOrder == asyncOrderUnset || m.asyncOrder == asyncOrderDone {
		// m finished evaluating, possibly asynchronously as a dependency of another module.
		vm.ResolveCapability(c, Undefined)
	}
	return Value{o: c.promise}
}

// innerModuleEvaluation implements InnerModuleEvaluation for the module m and the index 0.
// ok is false when the evaluation throws, and the result is the exception then.
func (vm *VM) innerModuleEvaluation(root *Module, stack *[]*Module) (Value, bool) {
	type frame struct {
		m *Module

		// list is the evaluationList of m, and i the index of the next module of it.
		list []*Module
		i    int
	}
	var frames []frame
	index := 0
	// enter starts the evaluation of the module m. It reports false when m needs no frame,
	// and its error when it was evaluated with one.
	enter := func(m *Module) (Value, bool, bool) {
		if !m.isCyclic() {
			return Undefined, true, false
		}
		switch m.status {
		case moduleEvaluatingAsync, moduleEvaluated:
			if m.hasError {
				return m.evalError, false, false
			}
			return Undefined, true, false
		case moduleEvaluating:
			return Undefined, true, false
		}
		m.status = moduleEvaluating
		vm.modules.evaluating = append(vm.modules.evaluating, m)
		m.dfsIndex = index
		m.dfsAncestorIndex = index
		m.pendingAsyncDeps = 0
		index++
		*stack = append(*stack, m)
		frames = append(frames, frame{
			m:    m,
			list: m.evaluationList(),
		})
		return Undefined, true, true
	}
	// after continues the module m after the evaluation of its requested module dep.
	after := func(m, dep *Module) (Value, bool) {
		if !dep.isCyclic() {
			return Undefined, true
		}
		if dep.status == moduleEvaluating {
			m.dfsAncestorIndex = min(m.dfsAncestorIndex, dep.dfsAncestorIndex)
		} else {
			dep = dep.cycleRoot
			if dep.hasError {
				return dep.evalError, false
			}
		}
		if dep.asyncOrder > 0 {
			m.pendingAsyncDeps++
			dep.asyncParents = append(dep.asyncParents, m)
		}
		return Undefined, true
	}
	if e, ok, _ := enter(root); !ok {
		return e, false
	}
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		m := top.m
		if top.i < len(top.list) {
			dep := top.list[top.i]
			top.i++
			e, ok, pushed := enter(dep)
			if !ok {
				return e, false
			}
			if pushed {
				continue
			}
			if e, ok := after(m, dep); !ok {
				return e, false
			}
			continue
		}
		frames = frames[:len(frames)-1]
		if m.pendingAsyncDeps > 0 || m.code.HasTLA {
			vm.modules.asyncOrder++
			m.asyncOrder = vm.modules.asyncOrder
			if m.pendingAsyncDeps == 0 {
				vm.executeAsyncModule(m)
			}
		} else if e, ok := vm.Completion(func() {
			vm.Call(m.fn, Undefined)
		}); !ok {
			return e, false
		}
		if m.dfsAncestorIndex == m.dfsIndex {
			for {
				s := (*stack)[len(*stack)-1]
				*stack = (*stack)[:len(*stack)-1]
				if s.asyncOrder == asyncOrderUnset {
					s.status = moduleEvaluated
				} else {
					s.status = moduleEvaluatingAsync
				}
				s.cycleRoot = m
				if s == m {
					break
				}
			}
		}
		if n := len(frames); n > 0 {
			if e, ok := after(frames[n-1].m, m); !ok {
				return e, false
			}
		}
	}
	return Undefined, true
}

// minListedSet is the length from which evaluationList checks for duplicates in a set.
const minListedSet = 16

// evaluationList returns the modules that the evaluation of m evaluates first: those that it
// requests, except that for a deferred import, only the asynchronous modules that the
// deferred module depends on.
func (m *Module) evaluationList() []*Module {
	var list []*Module
	// listed holds the modules of list once it is long.
	var listed map[*Module]bool
	add := func(dep *Module) {
		if listed == nil && len(list) < minListedSet {
			if !slices.Contains(list, dep) {
				list = append(list, dep)
			}
			return
		}
		if listed == nil {
			listed = make(map[*Module]bool, 2*len(list))
			for _, m := range list {
				listed[m] = true
			}
		}
		if !listed[dep] {
			listed[dep] = true
			list = append(list, dep)
		}
	}
	for i, r := range m.code.Requests {
		dep := m.requested[i]
		switch r.Phase {
		case bytecode.PhaseDefer:
			for _, d := range dep.asyncTransitiveDependencies() {
				add(d)
			}
		case bytecode.PhaseSource:
		default:
			add(dep)
		}
	}
	return list
}

// isSCCEvaluated implements IsModuleSCCEvaluated.
func (m *Module) isSCCEvaluated() bool {
	if m.cycleRoot != nil {
		return m.cycleRoot.status == moduleEvaluated
	}
	return m.status == moduleEvaluated
}

// asyncTransitiveDependencies implements GatherAsynchronousTransitiveDependencies.
func (m *Module) asyncTransitiveDependencies() []*Module {
	type frame struct {
		m *Module
		i int
	}
	seen := map[*Module]bool{}
	var result []*Module
	var frames []frame
	visit := func(m *Module) {
		if seen[m] {
			return
		}
		seen[m] = true
		if !m.isCyclic() || m.status == moduleEvaluating || m.isSCCEvaluated() {
			return
		}
		if m.code.HasTLA {
			// seen keeps m from being added twice.
			result = append(result, m)
			return
		}
		frames = append(frames, frame{m: m})
	}
	visit(m)
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		if top.i == len(top.m.requested) {
			frames = frames[:len(frames)-1]
			continue
		}
		dep := top.m.requested[top.i]
		top.i++
		visit(dep)
	}
	return result
}

// readyForSyncExecution implements ReadyForSyncExecution.
func (m *Module) readyForSyncExecution() bool {
	seen := map[*Module]bool{}
	work := []*Module{m}
	for len(work) > 0 {
		mod := work[len(work)-1]
		work = work[:len(work)-1]
		if seen[mod] || !mod.isCyclic() {
			continue
		}
		seen[mod] = true
		switch mod.status {
		case moduleEvaluated:
			continue
		case moduleEvaluating, moduleEvaluatingAsync:
			return false
		}
		if mod.code.HasTLA {
			return false
		}
		work = append(work, mod.requested...)
	}
	return true
}

// evaluateModuleSync implements EvaluateModuleSync.
func (vm *VM) evaluateModuleSync(m *Module) {
	if m.isCyclic() && m.status != moduleEvaluated && !m.readyForSyncExecution() {
		vm.ThrowTypeError(vm.CurrentRealm(), "the deferred module cannot be evaluated synchronously")
	}
	p := vm.evaluateModule(m).o
	if d := promiseOf(p); d.state == PromiseRejected {
		if !d.isHandled {
			vm.hostPromiseRejectionTracker(p, true)
			d.isHandled = true
		}
		vm.Throw(d.result)
	}
}

// executeAsyncModule implements ExecuteAsyncModule.
func (vm *VM) executeAsyncModule(m *Module) {
	p := vm.Call(m.fn, Undefined)
	vm.addNativeReaction(p.o, func(vm *VM, v Value, rejected bool) {
		if rejected {
			vm.asyncModuleExecutionRejected(m, v)
			return
		}
		vm.asyncModuleExecutionFulfilled(m)
	})
}

// asyncModuleExecutionFulfilled implements AsyncModuleExecutionFulfilled.
func (vm *VM) asyncModuleExecutionFulfilled(m *Module) {
	if m.status == moduleEvaluated {
		return
	}
	m.asyncOrder = asyncOrderDone
	m.status = moduleEvaluated
	if c := m.topLevelCapability; c != nil {
		vm.ResolveCapability(c, Undefined)
	}

	// GatherAvailableAncestors.
	var execList []*Module
	inList := map[*Module]bool{}
	work := []*Module{m}
	for len(work) > 0 {
		mod := work[len(work)-1]
		work = work[:len(work)-1]
		for _, p := range mod.asyncParents {
			root := p.cycleRoot
			if root == nil {
				// The evaluation that p was part of threw before p's cycle was complete.
				root = p
			}
			if inList[p] || root.hasError {
				continue
			}
			p.pendingAsyncDeps--
			if p.pendingAsyncDeps == 0 {
				execList = append(execList, p)
				inList[p] = true
				if !p.code.HasTLA {
					work = append(work, p)
				}
			}
		}
	}
	slices.SortFunc(execList, func(a, b *Module) int {
		return a.asyncOrder - b.asyncOrder
	})
	for _, p := range execList {
		switch {
		case p.status == moduleEvaluated:
			// An error of an earlier module in the list rejected it.
		case p.code.HasTLA:
			vm.executeAsyncModule(p)
		default:
			if e, ok := vm.Completion(func() {
				vm.Call(p.fn, Undefined)
			}); !ok {
				vm.asyncModuleExecutionRejected(p, e)
				break
			}
			p.asyncOrder = asyncOrderDone
			p.status = moduleEvaluated
			if c := p.topLevelCapability; c != nil {
				vm.ResolveCapability(c, Undefined)
			}
		}
	}
}

// asyncModuleExecutionRejected implements AsyncModuleExecutionRejected.
func (vm *VM) asyncModuleExecutionRejected(m *Module, e Value) {
	type frame struct {
		m *Module
		i int
	}
	var frames []frame
	reject := func(m *Module) bool {
		if m.status == moduleEvaluated {
			return false
		}
		m.evalError = e
		m.hasError = true
		m.status = moduleEvaluated
		m.asyncOrder = asyncOrderDone
		// The promise of a module is rejected before those of the modules that wait for it.
		if c := m.topLevelCapability; c != nil {
			vm.RejectCapability(c, e)
		}
		frames = append(frames, frame{m: m})
		return true
	}
	reject(m)
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		if top.i < len(top.m.asyncParents) {
			p := top.m.asyncParents[top.i]
			top.i++
			reject(p)
			continue
		}
		frames = frames[:len(frames)-1]
	}
}

// abortModuleEvaluations evaluates every module whose evaluation is in progress with an Error
// that [GoError] maps to cause, as an exception of the module does. An interruption cuts these evaluations
// off: it unwinds the modules that run and drops the jobs that the others wait for.
func (vm *VM) abortModuleEvaluations(cause error) {
	modules := vm.modules.evaluating
	vm.modules.evaluating = nil
	vm.rejectModuleEvaluations(modules, cause)
}

func (vm *VM) rejectModuleEvaluations(modules []*Module, cause error) {
	var e Value
	var hasError bool
	for _, m := range modules {
		if m.status == moduleEvaluated {
			continue
		}
		if !hasError {
			// The message is fixed, so that scripts do not see the text of cause.
			e = vm.newGoError(vm.CurrentRealm(), "ekuma: interrupted", cause)
			hasError = true
		}
		vm.asyncModuleExecutionRejected(m, e)
	}
}

// importCall implements EvaluateImportCall for the phase after the evaluation of the specifier
// and the options.
func (vm *VM) importCall(specifier, options Value, phase bytecode.Phase) Value {
	ref := vm.activeScriptOrModule()
	c := vm.newIntrinsicCapability()
	var req ModuleRequest
	ok := vm.IfAbruptRejectPromise(c, func() {
		req.Specifier = vm.ToString(specifier)
		if options.IsUndefined() {
			return
		}
		if !options.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "the options of import() must be an object")
		}
		attrs := vm.Get(options.o, PropKey{key: vm.Intern("with")})
		if attrs.IsUndefined() {
			return
		}
		if !attrs.IsObject() {
			vm.ThrowTypeError(vm.CurrentRealm(), "the attributes of import() must be an object")
		}
		req.Attributes = vm.dynamicImportAttributes(attrs.o)
	})
	if !ok {
		return Value{o: c.promise}
	}
	if vm.hasDetachedGlobal(vm.curRealm) {
		vm.RejectCapability(c, vm.NewError(vm.CurrentRealm(), ErrorKindError, "Cannot import module from an inactive context"))
		return Value{o: c.promise}
	}
	m, e, ok := vm.loadImportedModule(ref, req)
	if !ok {
		vm.RejectCapability(c, e)
		return Value{o: c.promise}
	}
	vm.continueDynamicImport(c, m, phase)
	return Value{o: c.promise}
}

// dynamicImportAttributes reads and validates the attributes of an import() call.
func (vm *VM) dynamicImportAttributes(o *Object) []ImportAttribute {
	type entry struct {
		key   wtf8.String
		value Value
	}
	var entries []entry
	for i, k := range vm.OwnKeys(o) {
		vm.pollAt(i)
		if k.IsSymbol() {
			continue
		}
		d, ok := vm.GetOwnProperty(o, k)
		if !ok || !d.Enumerable() {
			continue
		}
		entries = append(entries, entry{
			key:   vm.KeyString(k),
			value: vm.Get(o, k),
		})
	}
	// All attribute getters run before their values are checked.
	var attrs []ImportAttribute
	for i, e := range entries {
		vm.pollAt(i)
		if !e.value.IsString() {
			vm.ThrowTypeError(vm.CurrentRealm(), "the value of the import attribute '"+wtf8.ToValidUTF8(e.key)+"' must be a string")
		}
		attrs = append(attrs, ImportAttribute{
			Key:   e.key,
			Value: e.value.Str(),
		})
	}
	if !allImportAttributesSupported(attrs) {
		vm.ThrowTypeError(vm.CurrentRealm(), "unsupported import attribute")
	}
	slices.SortFunc(attrs, func(a, b ImportAttribute) int {
		return wtf8.Compare(a.Key, b.Key)
	})
	return attrs
}

// continueDynamicImport implements ContinueDynamicImport for a module of the phase that loaded.
func (vm *VM) continueDynamicImport(c *PromiseCapability, m *Module, phase bytecode.Phase) {
	if phase == bytecode.PhaseSource {
		if v, ok := vm.getModuleSource(m); ok {
			vm.ResolveCapability(c, v)
		} else {
			vm.RejectCapability(c, v)
		}
		return
	}
	load := vm.loadRequestedModules(m)
	vm.addNativeReaction(load.o, func(vm *VM, v Value, rejected bool) {
		if rejected {
			vm.RejectCapability(c, v)
			return
		}
		if e, ok := vm.linkModule(m); !ok {
			vm.RejectCapability(c, e)
			return
		}
		var p Value
		if phase == bytecode.PhaseDefer {
			p = vm.evaluateAll(m.asyncTransitiveDependencies())
		} else {
			p = vm.evaluateModule(m)
		}
		vm.addNativeReaction(p.o, func(vm *VM, v Value, rejected bool) {
			if rejected {
				vm.RejectCapability(c, v)
				return
			}
			ns := vm.moduleNamespace(m)
			if phase == bytecode.PhaseDefer {
				ns = vm.deferredModuleNamespace(m)
			}
			vm.ResolveCapability(c, Value{o: ns})
		})
	})
}

// evaluateAll evaluates the modules and returns a promise that is fulfilled when all of them
// are, as PerformPromiseAll does for their promises.
func (vm *VM) evaluateAll(modules []*Module) Value {
	c := vm.newIntrinsicCapability()
	if len(modules) == 0 {
		vm.ResolveCapability(c, Undefined)
		return Value{o: c.promise}
	}
	remaining := len(modules)
	for _, m := range modules {
		p := vm.evaluateModule(m)
		vm.addNativeReaction(p.o, func(vm *VM, v Value, rejected bool) {
			if rejected {
				vm.RejectCapability(c, v)
				return
			}
			if remaining--; remaining == 0 {
				vm.ResolveCapability(c, Undefined)
			}
		})
	}
	return Value{o: c.promise}
}

// importMeta returns the object import.meta of the running module.
func (vm *VM) importMeta() Value {
	m := vm.frames[len(vm.frames)-1].fp.script.module
	if !m.metaStarted {
		if m.meta == nil {
			m.meta = vm.NewObject(nil)
		}
		m.metaStarted = true
		if f := vm.modules.meta; f != nil {
			var initialized bool
			defer func() { m.metaStarted = initialized }()
			props := f(vm, m)
			vm.ResumeInterruption()
			for _, p := range props {
				vm.CreateDataPropertyOrThrow(m.meta, p.Key, p.Value)
			}
			initialized = true
		}
	}
	return Value{o: m.meta}
}

// importOp performs the instruction i, an [bytecode.OpImport] or an [bytecode.OpImportMeta], on
// the registers regs.
func (vm *VM) importOp(regs []Value, i *bytecode.Instr) Value {
	if i.Op == bytecode.OpImportMeta {
		return vm.importMeta()
	}
	return vm.importCall(regs[i.B], regs[i.C], bytecode.Phase(regs[i.C+1].n))
}
