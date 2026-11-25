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
	"github.com/hajimehoshi/ekuma/internal/modulegraph"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
	realm *realm

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
// module that the request req of the referrer resolves to. cx is the executing context.
// referrerContext is the context of the importing script, module, or realm. referrer is the
// importing module, or nil for a script or realm. file is the name of the script or module,
// or empty. The engine calls the loader at most once per successfully resolved request
// for a given referrer.
//
// The error is an [*Error] for a thrown value, a [*SyntaxError], or any other error, which is
// thrown as an Error with its message.
type HostLoadImportedModuleFunc func(cx *ExecutionContext, referrer *Module, file string, referrerContext *ExecutionContext, req ModuleRequest) (*Module, error)

// HostGetImportMetaPropertiesFunc implements HostGetImportMetaProperties: it returns the
// properties of the object import.meta of the module m, which is created then.
type HostGetImportMetaPropertiesFunc func(cx *ExecutionContext, m *Module) []ImportMetaProperty

// ImportMetaProperty is a property that HostGetImportMetaProperties gives import.meta.
type ImportMetaProperty struct {
	Key   PropKey
	Value Value
}

// moduleHost holds the host functions of the agent for modules.
type moduleHost struct {
	load HostLoadImportedModuleFunc
	meta HostGetImportMetaPropertiesFunc
}

// SetHostLoadImportedModule sets the implementation of HostLoadImportedModule. Without one,
// every import fails with a TypeError.
func (ag *agent) SetHostLoadImportedModule(f HostLoadImportedModuleFunc) {
	ag.modules.load = f
}

// SetHostGetImportMetaProperties sets the implementation of HostGetImportMetaProperties.
// Without one, import.meta has no properties.
func (ag *agent) SetHostGetImportMetaProperties(f HostGetImportMetaPropertiesFunc) {
	ag.modules.meta = f
}

// moduleEvaluations tracks the evaluations of the modules of an agent.
type moduleEvaluations struct {
	// asyncOrder is the last [[AsyncEvaluationOrder]] that was given out.
	asyncOrder int

	// evaluating holds the modules whose evaluation started, in the order it did. It includes
	// every module that is evaluating or evaluating-async, and may include evaluated ones.
	evaluating []*Module
}

// nextAsyncOrder returns a new [[AsyncEvaluationOrder]], greater than all the earlier ones.
func (e *moduleEvaluations) nextAsyncOrder() int {
	e.asyncOrder++
	return e.asyncOrder
}

// add adds m, whose evaluation starts.
func (e *moduleEvaluations) add(m *Module) {
	e.evaluating = append(e.evaluating, m)
}

// dropEvaluated removes the modules that are evaluated.
func (e *moduleEvaluations) dropEvaluated() {
	e.evaluating = slices.DeleteFunc(e.evaluating, func(m *Module) bool {
		return m.status == moduleEvaluated
	})
}

// takeAll removes all the modules and returns them in the order their evaluation started, possibly
// with evaluated ones among them.
func (e *moduleEvaluations) takeAll() []*Module {
	modules := e.evaluating
	e.evaluating = nil
	return modules
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
	ref         scriptOrModule
	hostDefined any

	// node is the node of m in the graph of modules. Its Code describes a source text module, and
	// is nil for a synthetic module.
	node modulegraph.Node[*Module]

	body *funcProto

	// env holds the bindings of the top level, in the order of node.Code.Bindings. A binding of an
	// import is the binding of the exporting module, and fn has the bindings as its upvalues.
	env []*upvalue
	fn  *Object

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
}

// HostDefined returns the [[HostDefined]] of m.
func (m *Module) HostDefined() any {
	return m.hostDefined
}

// newCell returns a binding that holds v and refers to no register.
func newCell(v Value) *upvalue {
	u := &upvalue{Val: v}
	u.P = &u.Val
	return u
}

// ParseModule implements ParseModule for the source text src. file names the module in error
// messages and stack traces, and hostDefined is the [[HostDefined]]. The error is a [*SyntaxError].
func (cx *ExecutionContext) ParseModule(file, src string, hostDefined any) (*Module, error) {
	prog, err := parser.ParseModule(wtf8.FromValidUTF8(src), parser.Options{})
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	bc, code, err := compiler.CompileModule(prog, file)
	if err != nil {
		return nil, toSyntaxError(file, err)
	}
	m := &Module{
		hostDefined: hostDefined,
		env:         make([]*upvalue, len(code.Bindings)),
	}
	m.node = modulegraph.Node[*Module]{
		Module:    m,
		Code:      code,
		Requested: make([]*modulegraph.Node[*Module], len(code.Requests)),
	}
	m.ref = scriptOrModule{
		file:   file,
		module: m,
		realm:  cx.realm,
	}
	m.body = cx.link(bc, &m.ref)
	for i := range m.env {
		m.env[i] = newCell(tdz)
	}
	return m, nil
}

// CreateDefaultExportSyntheticModule implements CreateDefaultExportSyntheticModule: it returns
// a module whose default export is v. hostDefined is the [[HostDefined]].
func (cx *ExecutionContext) CreateDefaultExportSyntheticModule(v Value, hostDefined any) *Module {
	m := &Module{
		hostDefined: hostDefined,
		env:         []*upvalue{newCell(v)},
		status:      moduleEvaluated,
	}
	m.node.Module = m
	m.ref = scriptOrModule{
		module: m,
		realm:  cx.realm,
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
func (cx *ExecutionContext) getModuleSource(m *Module) (Value, bool) {
	if m.source == nil {
		return cx.NewError(ErrorKindSyntax, "a source phase import is not available for this module"), false
	}
	return Value{O: m.source}, true
}

// isCyclic reports whether m is a Cyclic Module Record.
func (m *Module) isCyclic() bool {
	return m.node.Code != nil
}

// LoadRequestedModules implements LoadRequestedModules for a host whose loading completes
// synchronously: it loads the modules that m and its dependencies request. Once it has
// completed for m, it does nothing. A thrown exception is reported as an [*Error].
func (m *Module) LoadRequestedModules() error {
	cx := &m.ref.realm.context
	_, err := cx.enter(func() Value {
		if e, ok := cx.loadModuleGraph(m); !ok {
			cx.Throw(e)
		}
		return storage.Undefined
	})
	return err
}

// Link implements Link, after [Module.LoadRequestedModules] when m is not loaded. Once m is
// linked, it does nothing. A thrown exception is reported as an [*Error].
func (m *Module) Link() error {
	cx := &m.ref.realm.context
	_, err := cx.enter(func() Value {
		cx.loadAndLink(m)
		return storage.Undefined
	})
	return err
}

// loadAndLink loads and links m unless that is done, and throws the error of a failure.
func (cx *ExecutionContext) loadAndLink(m *Module) {
	if e, ok := cx.loadModuleGraph(m); !ok {
		cx.Throw(e)
	}
	if e, ok := cx.linkModule(m); !ok {
		cx.Throw(e)
	}
}

// Evaluate implements Evaluate, after [Module.Link] when m is not linked. It returns the promise of the evaluation. A failure to load or
// link, which comes before any module code runs, is reported as an [*Error].
func (m *Module) Evaluate() (Value, error) {
	cx := &m.ref.realm.context
	return cx.enter(func() Value {
		cx.loadAndLink(m)
		return cx.evaluateModule(m)
	})
}

// GetModuleNamespace implements GetModuleNamespace for the evaluation phase, after [Module.Link]
// when m is not linked. A thrown exception is reported as an [*Error].
func (m *Module) GetModuleNamespace() (Value, error) {
	cx := &m.ref.realm.context
	return cx.enter(func() Value {
		cx.loadAndLink(m)
		return Value{O: cx.moduleNamespace(m)}
	})
}

// ParseJSONModule implements ParseJSONModule for the source text src. hostDefined is the
// [[HostDefined]]. A src that is not JSON is reported as an [*Error] holding the JavaScript
// SyntaxError.
func (cx *ExecutionContext) ParseJSONModule(src string, hostDefined any) (*Module, error) {
	v, err := cx.enter(func() Value {
		return cx.Call(cx.Intrinsic(IntrinsicJSONParse), storage.Undefined, cx.StringFromGo(src))
	})
	if err != nil {
		return nil, err
	}
	return cx.CreateDefaultExportSyntheticModule(v, hostDefined), nil
}

// activeScriptOrModule implements GetActiveScriptOrModule, with the realm standing for null.
func (cx *ExecutionContext) activeScriptOrModule() *scriptOrModule {
	if n := len(cx.frames); n > 0 {
		return scriptOrModuleOf(cx.frames[n-1].fp)
	}
	return cx.realm.referrer
}

// errorValue returns the value to throw for an error of the host.
func (cx *ExecutionContext) errorValue(r *realm, err error) Value {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Value
	}
	if e, ok := errors.AsType[*SyntaxError](err); ok {
		return r.context.NewError(ErrorKindSyntax, e.messageWithPosition())
	}
	return r.context.NewGoError(err)
}

// loadImportedModule returns the module of the request req of the referrer ref from its
// [[LoadedModules]], or loads it with HostLoadImportedModule and records it. ok is false
// when loading fails, and the result is the error to throw then.
func (cx *ExecutionContext) loadImportedModule(ref *scriptOrModule, req ModuleRequest) (m *Module, e Value, ok bool) {
	k := requestKey(req)
	if m := ref.loaded[k]; m != nil {
		return m, storage.Undefined, true
	}
	r := ref.realm
	if cx.modules.load == nil {
		return nil, r.context.NewError(ErrorKindType, "cannot import '"+wtf8.ToValidUTF8(req.Specifier)+"': there is no module loader"), false
	}
	m, err := cx.modules.load(cx, ref.module, ref.file, &r.context, req)
	cx.ResumeInterruption()
	if err != nil {
		return nil, cx.errorValue(r, err), false
	}
	if m == nil {
		return nil, r.context.NewError(ErrorKindType, "cannot import '"+wtf8.ToValidUTF8(req.Specifier)+"'"), false
	}
	if ref.loaded == nil {
		ref.loaded = map[string]*Module{}
	}
	ref.loaded[k] = m
	return m, storage.Undefined, true
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
func (cx *ExecutionContext) loadModuleGraph(root *Module) (Value, bool) {
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
		return cx.loadingError(root.ref.realm, root), false
	}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.i == len(top.m.node.Code.Requests) {
			stack = stack[:len(stack)-1]
			continue
		}
		m, i := top.m, top.i
		top.i++
		req := moduleRequest(m.node.Code.Requests[i])
		if !allImportAttributesSupported(req.Attributes) {
			return m.ref.realm.context.NewError(ErrorKindSyntax, "unsupported import attribute"), false
		}
		dep, e, ok := cx.loadImportedModule(&m.ref, req)
		if !ok {
			return e, false
		}
		m.node.Requested[i] = &dep.node
		if !visit(dep) {
			return cx.loadingError(m.ref.realm, dep), false
		}
	}
	for _, m := range order {
		if m.status == moduleNew {
			m.status = moduleUnlinked
		}
	}
	return storage.Undefined, true
}

// loadingError returns the TypeError of loading m while an unfinished call is loading it.
func (cx *ExecutionContext) loadingError(r *realm, m *Module) Value {
	return r.context.NewError(ErrorKindType, "cannot load '"+m.ref.file+"' while it is being loaded")
}

// loadRequestedModules implements LoadRequestedModules: it returns its promise.
func (cx *ExecutionContext) loadRequestedModules(m *Module) Value {
	c := cx.newIntrinsicCapability()
	if e, ok := cx.loadModuleGraph(m); ok {
		cx.ResolveCapability(c, storage.Undefined)
	} else {
		cx.RejectCapability(c, e)
	}
	return Value{O: c.promise}
}

// linkModule implements Link and InnerModuleLinking. ok is false when linking fails, and the
// result is the error to throw then.
func (cx *ExecutionContext) linkModule(root *Module) (Value, bool) {
	if !root.isCyclic() {
		return storage.Undefined, true
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
		if top.i < len(m.node.Requested) {
			dep := m.node.Requested[top.i].Module
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
		if e, ok := m.initializeEnvironment(); !ok {
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
	return storage.Undefined, true
}

// initializeEnvironment implements InitializeEnvironment for the source text module m.
func (m *Module) initializeEnvironment() (Value, bool) {
	cx := &m.ref.realm.context
	code := m.node.Code
	for _, e := range code.IndirectExports {
		if r := m.node.ResolveExport(e.Name); r.Kind != modulegraph.Found {
			return cx.resolutionError(m, e.Name, r), false
		}
	}
	// The bindings of the module itself are those that importers already refer to.
	for i, b := range code.Bindings {
		switch b.Kind {
		case bytecode.ModuleVar:
			*m.env[i].P = storage.Undefined
		case bytecode.ModuleLexical, bytecode.ModuleFunction:
			*m.env[i].P = tdz
		}
	}
	for _, ie := range code.Imports {
		r := m.node.Requested[ie.Request].ResolveExport(ie.ImportName)
		if r.Kind != modulegraph.Found {
			return cx.resolutionError(m, ie.ImportName, r), false
		}
		switch r.Binding {
		case modulegraph.BindingNamespace:
			m.env[ie.Binding] = newCell(Value{O: cx.moduleNamespace(r.Module)})
		case modulegraph.BindingSource:
			v, ok := cx.getModuleSource(r.Module)
			if !ok {
				return v, false
			}
			m.env[ie.Binding] = newCell(v)
		default:
			m.env[ie.Binding] = r.Module.env[r.Binding]
		}
	}
	for i, b := range code.Bindings {
		switch b.Kind {
		case bytecode.ModuleNamespace:
			m.env[i] = newCell(Value{O: cx.moduleNamespace(m.node.Requested[b.Request].Module)})
		case bytecode.ModuleDeferredNamespace:
			// A circular importer may already refer to this cell.
			*m.env[i].P = Value{O: cx.deferredModuleNamespace(m.node.Requested[b.Request].Module)}
		case bytecode.ModuleSource:
			v, ok := cx.getModuleSource(m.node.Requested[b.Request].Module)
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
	m.fn = storage.NewFunctionObject(cx.realm.Shapes.Function, &function{
		Code:   m.body,
		Upvals: upvals,
	})
	for _, f := range code.Funcs {
		*m.env[f.Binding].P = Value{O: cx.newClosure(m.body.Protos[f.Proto], upvals)}
	}
	return storage.Undefined, true
}

// resolutionError returns the SyntaxError of the import or export name of m that did not
// resolve to r.
func (cx *ExecutionContext) resolutionError(m *Module, n string, r modulegraph.Resolution[*Module]) Value {
	if r.Kind == modulegraph.Ambiguous {
		return m.ref.realm.context.NewError(ErrorKindSyntax, "the export '"+n+"' is ambiguous ("+m.ref.file+")")
	}
	return m.ref.realm.context.NewError(ErrorKindSyntax, "the export '"+n+"' cannot be resolved ("+m.ref.file+")")
}

// newClosure returns a closure of p whose enclosing function has the upvalues upvals. p must
// not refer to registers of the enclosing function.
func (cx *ExecutionContext) newClosure(p *funcProto, upvals []*upvalue) *Object {
	fn := &function{
		Code:             p,
		Constructor:      p.IsConstructor,
		ClassConstructor: p.IsClassConstructor,
		Derived:          p.IsDerived,
	}
	s := cx.realm.Shapes.Function
	if p.IsGenerator || p.IsAsync {
		s = cx.FunctionShapeFor(p.ClosurePrototype())
	}
	o := storage.NewFunctionObject(s, fn)
	if n := len(p.Upvals); n > 0 {
		fn.Upvals = make([]*upvalue, n)
		for j, d := range p.Upvals {
			fn.Upvals[j] = upvals[d.Index]
		}
	}
	return o
}

// evaluateModule implements Evaluate: it returns the promise of the evaluation of m.
func (cx *ExecutionContext) evaluateModule(m *Module) Value {
	if !m.isCyclic() {
		c := cx.newIntrinsicCapability()
		cx.ResolveCapability(c, storage.Undefined)
		return Value{O: c.promise}
	}
	if m.status == moduleEvaluatingAsync || m.status == moduleEvaluated {
		if m.cycleRoot != nil {
			m = m.cycleRoot
		}
	}
	if m.topLevelCapability != nil {
		return Value{O: m.topLevelCapability.promise}
	}
	cx.moduleEvaluations.dropEvaluated()
	c := cx.newIntrinsicCapability()
	m.topLevelCapability = c
	var stack []*Module
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*xerrors.InternalError); ok {
				cx.rejectModuleEvaluations(stack, e)
			} else if isHostPanic(r) {
				cx.rejectModuleEvaluations(stack, &PanicError{
					Value: r,
				})
			}
			panic(r)
		}
	}()
	if e, ok := cx.innerModuleEvaluation(m, &stack); !ok {
		for _, s := range stack {
			s.status = moduleEvaluated
			s.evalError = e
			s.hasError = true
		}
		cx.RejectCapability(c, e)
	} else if m.asyncOrder == asyncOrderUnset || m.asyncOrder == asyncOrderDone {
		// m finished evaluating, possibly asynchronously as a dependency of another module.
		cx.ResolveCapability(c, storage.Undefined)
	}
	return Value{O: c.promise}
}

// innerModuleEvaluation implements InnerModuleEvaluation for the module root and the index 0.
// ok is false when the evaluation throws, and the result is the exception then.
func (cx *ExecutionContext) innerModuleEvaluation(root *Module, stack *[]*Module) (Value, bool) {
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
			return storage.Undefined, true, false
		}
		switch m.status {
		case moduleEvaluatingAsync, moduleEvaluated:
			if m.hasError {
				return m.evalError, false, false
			}
			return storage.Undefined, true, false
		case moduleEvaluating:
			return storage.Undefined, true, false
		}
		m.status = moduleEvaluating
		cx.moduleEvaluations.add(m)
		m.dfsIndex = index
		m.dfsAncestorIndex = index
		m.pendingAsyncDeps = 0
		index++
		*stack = append(*stack, m)
		frames = append(frames, frame{
			m:    m,
			list: m.evaluationList(),
		})
		return storage.Undefined, true, true
	}
	// after continues the module m after the evaluation of its requested module dep.
	after := func(m, dep *Module) (Value, bool) {
		if !dep.isCyclic() {
			return storage.Undefined, true
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
		return storage.Undefined, true
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
		if m.pendingAsyncDeps > 0 || m.node.Code.HasTLA {
			m.asyncOrder = cx.moduleEvaluations.nextAsyncOrder()
			if m.pendingAsyncDeps == 0 {
				cx.executeAsyncModule(m)
			}
		} else if e, ok := cx.Completion(func() {
			cx.Call(m.fn, storage.Undefined)
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
	return storage.Undefined, true
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
	for i, r := range m.node.Code.Requests {
		dep := m.node.Requested[i].Module
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
		if m.node.Code.HasTLA {
			// seen keeps m from being added twice.
			result = append(result, m)
			return
		}
		frames = append(frames, frame{m: m})
	}
	visit(m)
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		if top.i == len(top.m.node.Requested) {
			frames = frames[:len(frames)-1]
			continue
		}
		dep := top.m.node.Requested[top.i].Module
		top.i++
		visit(dep)
	}
	return result
}

// readyForSyncExecution implements ReadyForSyncExecution.
func (m *Module) readyForSyncExecution() bool {
	seen := map[*Module]bool{}
	work := []*modulegraph.Node[*Module]{&m.node}
	for len(work) > 0 {
		mod := work[len(work)-1].Module
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
		if mod.node.Code.HasTLA {
			return false
		}
		work = append(work, mod.node.Requested...)
	}
	return true
}

// evaluateModuleSync implements EvaluateModuleSync.
func (cx *ExecutionContext) evaluateModuleSync(m *Module) {
	if m.isCyclic() && m.status != moduleEvaluated && !m.readyForSyncExecution() {
		cx.ThrowTypeError("the deferred module cannot be evaluated synchronously")
	}
	p := cx.evaluateModule(m).O
	if d := promiseOf(p); d.state == PromiseRejected {
		if !d.isHandled {
			cx.hostPromiseRejectionTracker(p, true)
			d.isHandled = true
		}
		cx.Throw(d.result)
	}
}

// executeAsyncModule implements ExecuteAsyncModule.
func (cx *ExecutionContext) executeAsyncModule(m *Module) {
	p := cx.Call(m.fn, storage.Undefined)
	cx.addNativeReaction(p.O, func(cx *ExecutionContext, v Value, rejected bool) {
		if rejected {
			cx.asyncModuleExecutionRejected(m, v)
			return
		}
		cx.asyncModuleExecutionFulfilled(m)
	})
}

// asyncModuleExecutionFulfilled implements AsyncModuleExecutionFulfilled.
func (cx *ExecutionContext) asyncModuleExecutionFulfilled(m *Module) {
	if m.status == moduleEvaluated {
		return
	}
	m.asyncOrder = asyncOrderDone
	m.status = moduleEvaluated
	if c := m.topLevelCapability; c != nil {
		cx.ResolveCapability(c, storage.Undefined)
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
				if !p.node.Code.HasTLA {
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
		case p.node.Code.HasTLA:
			cx.executeAsyncModule(p)
		default:
			if e, ok := cx.Completion(func() {
				cx.Call(p.fn, storage.Undefined)
			}); !ok {
				cx.asyncModuleExecutionRejected(p, e)
				break
			}
			p.asyncOrder = asyncOrderDone
			p.status = moduleEvaluated
			if c := p.topLevelCapability; c != nil {
				cx.ResolveCapability(c, storage.Undefined)
			}
		}
	}
}

// asyncModuleExecutionRejected implements AsyncModuleExecutionRejected.
func (cx *ExecutionContext) asyncModuleExecutionRejected(m *Module, e Value) {
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
			cx.RejectCapability(c, e)
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

// abortModuleEvaluations rejects every module whose evaluation is in progress with an Error
// that [GoError] maps to cause, as an exception of the module does. An interruption cuts these
// evaluations off: it unwinds the modules that run and drops the jobs that the others wait for.
func (cx *ExecutionContext) abortModuleEvaluations(cause error) {
	cx.rejectModuleEvaluations(cx.moduleEvaluations.takeAll(), cause)
}

func (cx *ExecutionContext) rejectModuleEvaluations(modules []*Module, cause error) {
	var e Value
	var hasError bool
	for _, m := range modules {
		if m.status == moduleEvaluated {
			continue
		}
		if !hasError {
			// The message is fixed, so that scripts do not see the text of cause.
			e = cx.newGoError("ekuma: interrupted", cause)
			hasError = true
		}
		cx.asyncModuleExecutionRejected(m, e)
	}
}

// importCall implements EvaluateImportCall for the phase after the evaluation of the specifier
// and the options.
func (cx *ExecutionContext) importCall(specifier, options Value, phase bytecode.Phase) Value {
	ref := cx.activeScriptOrModule()
	c := cx.newIntrinsicCapability()
	var req ModuleRequest
	ok := cx.IfAbruptRejectPromise(c, func() {
		req.Specifier = cx.ToString(specifier)
		if options.IsUndefined() {
			return
		}
		if !options.IsObject() {
			cx.ThrowTypeError("the options of import() must be an object")
		}
		attrs := cx.Get(options.O, storage.NamedKey(cx.Intern("with")))
		if attrs.IsUndefined() {
			return
		}
		if !attrs.IsObject() {
			cx.ThrowTypeError("the attributes of import() must be an object")
		}
		req.Attributes = cx.dynamicImportAttributes(attrs.O)
	})
	if !ok {
		return Value{O: c.promise}
	}
	if cx.hasDetachedGlobal(cx.realm) {
		cx.RejectCapability(c, cx.NewError(ErrorKindError, "Cannot import module from an inactive context"))
		return Value{O: c.promise}
	}
	m, e, ok := cx.loadImportedModule(ref, req)
	if !ok {
		cx.RejectCapability(c, e)
		return Value{O: c.promise}
	}
	cx.continueDynamicImport(c, m, phase)
	return Value{O: c.promise}
}

// dynamicImportAttributes reads and validates the attributes of an import() call.
func (cx *ExecutionContext) dynamicImportAttributes(o *Object) []ImportAttribute {
	type entry struct {
		key   wtf8.String
		value Value
	}
	var entries []entry
	for i, k := range cx.OwnKeys(o) {
		cx.pollAt(i)
		if k.IsSymbol() {
			continue
		}
		d, ok := cx.GetOwnProperty(o, k)
		if !ok || !d.Enumerable() {
			continue
		}
		entries = append(entries, entry{
			key:   cx.KeyString(k),
			value: cx.Get(o, k),
		})
	}
	// All attribute getters run before their values are checked.
	var attrs []ImportAttribute
	for i, e := range entries {
		cx.pollAt(i)
		if !e.value.IsString() {
			cx.ThrowTypeError("the value of the import attribute '" + wtf8.ToValidUTF8(e.key) + "' must be a string")
		}
		attrs = append(attrs, ImportAttribute{
			Key:   e.key,
			Value: e.value.Str(),
		})
	}
	if !allImportAttributesSupported(attrs) {
		cx.ThrowTypeError("unsupported import attribute")
	}
	slices.SortFunc(attrs, func(a, b ImportAttribute) int {
		return wtf8.Compare(a.Key, b.Key)
	})
	return attrs
}

// continueDynamicImport implements ContinueDynamicImport for a module of the phase that loaded.
func (cx *ExecutionContext) continueDynamicImport(c *PromiseCapability, m *Module, phase bytecode.Phase) {
	if phase == bytecode.PhaseSource {
		if v, ok := cx.getModuleSource(m); ok {
			cx.ResolveCapability(c, v)
		} else {
			cx.RejectCapability(c, v)
		}
		return
	}
	load := cx.loadRequestedModules(m)
	cx.addNativeReaction(load.O, func(cx *ExecutionContext, v Value, rejected bool) {
		if rejected {
			cx.RejectCapability(c, v)
			return
		}
		if e, ok := cx.linkModule(m); !ok {
			cx.RejectCapability(c, e)
			return
		}
		var p Value
		if phase == bytecode.PhaseDefer {
			p = cx.evaluateAll(m.asyncTransitiveDependencies())
		} else {
			p = cx.evaluateModule(m)
		}
		cx.addNativeReaction(p.O, func(cx *ExecutionContext, v Value, rejected bool) {
			if rejected {
				cx.RejectCapability(c, v)
				return
			}
			ns := cx.moduleNamespace(m)
			if phase == bytecode.PhaseDefer {
				ns = cx.deferredModuleNamespace(m)
			}
			cx.ResolveCapability(c, Value{O: ns})
		})
	})
}

// evaluateAll evaluates the modules and returns a promise that is fulfilled when all of them
// are, as PerformPromiseAll does for their promises.
func (cx *ExecutionContext) evaluateAll(modules []*Module) Value {
	c := cx.newIntrinsicCapability()
	if len(modules) == 0 {
		cx.ResolveCapability(c, storage.Undefined)
		return Value{O: c.promise}
	}
	remaining := len(modules)
	for _, m := range modules {
		p := cx.evaluateModule(m)
		cx.addNativeReaction(p.O, func(cx *ExecutionContext, v Value, rejected bool) {
			if rejected {
				cx.RejectCapability(c, v)
				return
			}
			if remaining--; remaining == 0 {
				cx.ResolveCapability(c, storage.Undefined)
			}
		})
	}
	return Value{O: c.promise}
}

// importMeta returns the object import.meta of the running module.
func (cx *ExecutionContext) importMeta() Value {
	m := scriptOrModuleOf(cx.frames[len(cx.frames)-1].fp).module
	if !m.metaStarted {
		if m.meta == nil {
			m.meta = cx.NewObject(nil)
		}
		m.metaStarted = true
		if f := cx.modules.meta; f != nil {
			var initialized bool
			defer func() { m.metaStarted = initialized }()
			props := f(cx, m)
			cx.ResumeInterruption()
			for _, p := range props {
				cx.CreateDataPropertyOrThrow(m.meta, p.Key, p.Value)
			}
			initialized = true
		}
	}
	return Value{O: m.meta}
}

// importOp performs the instruction i, an [bytecode.OpImport] or an [bytecode.OpImportMeta], on
// the registers regs.
func (cx *ExecutionContext) importOp(regs []Value, i *bytecode.Instr) Value {
	if i.Op == bytecode.OpImportMeta {
		return cx.importMeta()
	}
	return cx.importCall(regs[i.B], regs[i.C], bytecode.Phase(regs[i.C+1].N))
}
