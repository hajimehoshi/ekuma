// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// maxEvalCache is the number of compiled eval codes that evalCache keeps.
const maxEvalCache = 256

// evalKey identifies the eval code of a source text at a call site of a direct eval, or of
// an indirect eval whose ScriptOrModule is script.
type evalKey struct {
	fp     *funcProto
	site   int
	src    wtf8.String
	script *scriptOrModule
}

// evalCache holds the compiled code of direct evals. The code of a call site depends only on
// the source text, because the site fixes the scope that the code sees.
type evalCache struct {
	codes map[evalKey]*funcProto

	// order holds the keys of codes, used as a ring: next is the oldest entry once it is full.
	order []evalKey
	next  int
}

func (c *evalCache) get(k evalKey) *funcProto {
	return c.codes[k]
}

func (c *evalCache) add(k evalKey, p *funcProto) {
	if c.codes == nil {
		c.codes = map[evalKey]*funcProto{}
	}
	if len(c.order) < maxEvalCache {
		c.order = append(c.order, k)
	} else {
		delete(c.codes, c.order[c.next])
		c.order[c.next] = k
		c.next = (c.next + 1) % maxEvalCache
	}
	c.codes[k] = p
}

// evalArg returns the first argument of a direct eval: args holds the argc arguments, or the
// array of the arguments when argc is negative.
func evalArg(args []Value, argc int) Value {
	switch {
	case argc > 0:
		return args[0]
	case argc < 0:
		if elems := args[0].o.elems; len(elems) > 0 {
			return elems[0]
		}
	}
	return Undefined
}

// callEval performs the call instruction i of a function named eval as a direct eval with the
// call site site, when the callee is the eval function, and reports whether it did. argc is as
// for evalArg. The registers may move when it returns.
func (cx *ExecutionContext) callEval(regs []Value, i *bytecode.Instr, site, argc int) (Value, bool) {
	if regs[i.B].o != cx.Intrinsic(IntrinsicEval) {
		return Undefined, false
	}
	return cx.directEval(evalArg(regs[i.B+2:], argc), site), true
}

// directEval implements PerformEval for a direct eval of x by the running function at its
// call site site.
func (cx *ExecutionContext) directEval(x Value, site int) Value {
	if cx.interrupt.Load() {
		cx.Poll()
	}
	if !x.IsString() {
		return x
	}
	fr := &cx.frames[len(cx.frames)-1]
	p := cx.evalCode(fr.fp, site, x.Str())
	if es := &fr.fp.EvalScopes[site]; es.LazyArguments {
		cx.createLazyArguments(fr, es, p)
	}

	// The eval code is a closure of the calling function, made at the call.
	fn := &function{code: p}
	if n := len(p.Upvals); n > 0 {
		fn.upvals = make([]*upvalue, n)
		for j, d := range p.Upvals {
			if d.Local {
				fn.upvals[j] = cx.captureRegister(fr, int(d.Index))
			} else {
				fn.upvals[j] = fr.cl.fn.upvals[d.Index]
			}
		}
	}
	fn.scopes = cx.openScopes(fr.cl)
	return cx.Call((&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: cx.realm.shapes.function,
		fn:    fn,
	}).init(), Undefined)
}

// PerformEval implements PerformEval for an indirect eval.
func (cx *ExecutionContext) PerformEval(x Value) Value {
	if !x.IsString() {
		return x
	}
	return cx.Call(cx.newScriptFunction(cx.evalCode(nil, 0, x.Str())), Value{o: cx.realm.globalThis})
}

// createLazyArguments creates the arguments object of the frame fr, whose direct eval at the
// call site es runs the eval code p, when p refers to the register that waits for it.
func (cx *ExecutionContext) createLazyArguments(fr *frame, es *bytecode.EvalScope, p *funcProto) {
	i, ok := slices.BinarySearchFunc(es.Vars, "arguments", func(v bytecode.EvalVar, name string) int {
		return strings.Compare(v.Name, name)
	})
	if !ok || !es.Vars[i].Local {
		return
	}
	reg := es.Vars[i].Index
	for _, d := range p.Upvals {
		if !d.Local || d.Index != reg {
			continue
		}
		regs := cx.frameRegs(fr)
		if regs[reg].o == tdzObject {
			regs[reg] = Value{o: cx.newArguments(fr, true)}
		}
		return
	}
}

// indirectEvalScope is what the eval code of an indirect eval sees.
var indirectEvalScope bytecode.EvalScope

// evalCode returns the compiled code of the eval code src at the call site site of fp,
// or of an indirect eval when fp is nil.
func (cx *ExecutionContext) evalCode(fp *funcProto, site int, src wtf8.String) *funcProto {
	// The eval code has the ScriptOrModule of the code that calls eval: the caller of a direct
	// eval, and the active one for an indirect eval.
	var script *scriptOrModule
	if fp != nil {
		script = fp.script
	} else {
		script = cx.activeScriptOrModule()
	}
	k := evalKey{
		fp:     fp,
		site:   site,
		src:    src,
		script: script,
	}
	if p := cx.realm.evals.get(k); p != nil {
		return p
	}
	bc := cx.compileEval(fp, site, src)
	p := cx.link(bc, script)
	// Every evaluation of a tagged template in new eval code has template objects of its own.
	if !hasTemplates(bc) {
		cx.realm.evals.add(k, p)
	}
	return p
}

// compileEval compiles the eval code src of the call site site of fp, or of an indirect eval
// when fp is nil. It throws a SyntaxError when src is not valid.
func (cx *ExecutionContext) compileEval(fp *funcProto, site int, src wtf8.String) *bytecode.Func {
	es := &indirectEvalScope
	if fp != nil {
		es = &fp.EvalScopes[site]
	}
	opts := parser.Options{
		Strict:             es.Strict,
		AllowNewTarget:     es.NewTarget,
		AllowSuperProperty: es.SuperProperty,
		AllowSuperCall:     es.SuperCall,
		InClassFieldInit:   es.InFieldInit,
		Poll:               cx.Poll,
	}
	for _, v := range es.Vars {
		if name, ok := strings.CutPrefix(v.Name, "#"); ok {
			opts.PrivateNames = append(opts.PrivateNames, name)
		}
	}
	prog, err := parser.ParseScript(src, opts)
	if err != nil {
		cx.throwCompileError("<eval>", err)
	}
	bc, err := compiler.CompileEval(prog, "<eval>", es, cx.Poll)
	if err != nil {
		cx.throwCompileError("<eval>", err)
	}
	return bc
}

// hasTemplates reports whether f or a function nested in it has a tagged template.
func hasTemplates(f *bytecode.Func) bool {
	if len(f.Templates) > 0 {
		return true
	}
	return slices.ContainsFunc(f.Protos, hasTemplates)
}

// pushEvalScope opens a new scope for the variables that direct evals declare in the running function.
func (cx *ExecutionContext) pushEvalScope() {
	o := cx.NewObject(nil)
	o.class = classEvalScope
	cur := &cx.frames[len(cx.frames)-1]
	cur.scopes = append(cur.scopes, o)
}

// scopeThis returns the this value of a call of a function found in the dynamic scope s.
func scopeThis(s Value) Value {
	if s.o.class == classEvalScope {
		return Undefined
	}
	return s
}

// declareEvalVar declares the variable key of eval code in the depth-th innermost dynamic
// scope of the running function cl. With fn not -1, the variable is a function, which the
// register fn of regs holds.
func (cx *ExecutionContext) declareEvalVar(cl *Object, key *Object, depth int, regs []Value, fn int) {
	var o *Object
	for _, scopes := range [...][]*Object{cx.frames[len(cx.frames)-1].scopes, cl.fn.capturedScopes()} {
		if depth <= len(scopes) {
			o = scopes[len(scopes)-depth]
			break
		}
		depth -= len(scopes)
	}
	k := PropKey{key: key}
	switch {
	case !cx.HasOwnProperty(o, k):
		v := Undefined
		if fn >= 0 {
			v = regs[fn]
		}
		cx.DefinePropertyOrThrow(o, k, dataDesc(v, FlagsData))
	case fn >= 0:
		cx.setProp(o, k, regs[fn], Value{o: o})
	}
}

// FunctionKind is the kind of function that CreateDynamicFunction creates.
type FunctionKind uint8

const (
	NormalFunction FunctionKind = iota
	GeneratorFunction
	AsyncFunction
	AsyncGeneratorFunction
)

// CreateDynamicFunction implements CreateDynamicFunction. ctor is the constructor being called,
// newTarget is nil in a plain call, and the last of args is the body.
func (cx *ExecutionContext) CreateDynamicFunction(ctor, newTarget *Object, kind FunctionKind, args []Value) Value {
	if newTarget == nil {
		newTarget = ctor
	}
	var params, body wtf8.String
	if len(args) > 0 {
		var sb []byte
		for i, a := range args[:len(args)-1] {
			if i > 0 {
				sb = append(sb, ',')
			}
			p := cx.ToString(a)
			if len(sb)+len(p) > MaxStringLength {
				cx.ThrowRangeError("invalid string length")
			}
			sb = append(sb, p...)
		}
		params = wtf8.String(sb)
		body = cx.ToString(args[len(args)-1])
	}

	var pkind parser.FunctionKind
	fallback := IntrinsicFunctionPrototype
	switch kind {
	case GeneratorFunction:
		pkind = parser.GeneratorFunction
		fallback = IntrinsicGeneratorFunctionPrototype
	case AsyncFunction:
		pkind = parser.AsyncFunction
		fallback = IntrinsicAsyncFunctionPrototype
	case AsyncGeneratorFunction:
		pkind = parser.AsyncGeneratorFunction
		fallback = IntrinsicAsyncGeneratorFunctionPrototype
	}
	fp, err := cx.compileFunction("<anonymous>", params, body, pkind)
	if err != nil {
		cx.throwCompileError("<anonymous>", err)
	}
	proto := cx.GetPrototypeFromConstructor(newTarget, fallback)
	f := cx.Call(cx.newScriptFunction(fp), Value{o: cx.realm.globalThis})
	if proto != cx.Intrinsic(fallback) {
		cx.SetPrototypeOf(f.o, proto)
	}
	return f
}
