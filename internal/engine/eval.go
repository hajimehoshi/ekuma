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
func (vm *VM) callEval(regs []Value, i *bytecode.Instr, site, argc int) (Value, bool) {
	if regs[i.B].o != vm.Intrinsic(IntrinsicEval) {
		return Undefined, false
	}
	return vm.directEval(evalArg(regs[i.B+2:], argc), site), true
}

// directEval implements PerformEval for a direct eval of x by the running function at its
// call site site.
func (vm *VM) directEval(x Value, site int) Value {
	if !x.IsString() {
		return x
	}
	fr := &vm.frames[len(vm.frames)-1]
	p := vm.evalCode(fr.fp, site, x.Str())
	if es := &fr.fp.EvalScopes[site]; es.LazyArguments {
		vm.createLazyArguments(fr, es, p)
	}

	// The eval code is a closure of the calling function, made at the call.
	fn := &function{code: p}
	if n := len(p.Upvals); n > 0 {
		fn.upvals = make([]*upvalue, n)
		for j, d := range p.Upvals {
			switch {
			case !d.Local:
				fn.upvals[j] = fr.cl.fn.upvals[d.Index]
			case fr.gen != nil:
				fn.upvals[j] = fr.gen.findUpval(int(d.Index))
			default:
				fn.upvals[j] = vm.findUpval(fr.base + int(d.Index))
			}
		}
	}
	fn.scopes = vm.openScopes(fr.cl)
	return vm.Call((&propertyObject{
		kind:  kindFunction,
		class: classFunction,
		flags: objExotic,
		shape: vm.shapes.function,
		fn:    fn,
	}).init(), Undefined)
}

// createLazyArguments creates the arguments object of the frame fr, whose direct eval at the
// call site es runs the eval code p, when p refers to the register that waits for it.
func (vm *VM) createLazyArguments(fr *frame, es *bytecode.EvalScope, p *funcProto) {
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
		regs := vm.stack[fr.base : fr.base+fr.fp.nregs+1]
		if fr.gen != nil {
			regs = fr.gen.regs
		}
		if regs[reg].o == tdzObject {
			regs[reg] = Value{o: vm.newArguments(fr, regs, true)}
		}
		return
	}
}

// indirectEvalScope is what the eval code of an indirect eval sees.
var indirectEvalScope bytecode.EvalScope

// evalCode returns the compiled code of the eval code src at the call site site of fp,
// or of an indirect eval when fp is nil.
func (vm *VM) evalCode(fp *funcProto, site int, src wtf8.String) *funcProto {
	// The eval code has the ScriptOrModule of the code that calls eval: the caller of a direct
	// eval, and the active one for an indirect eval.
	var script *scriptOrModule
	if fp != nil {
		script = fp.script
	} else {
		script = vm.activeScriptOrModule()
	}
	k := evalKey{
		fp:     fp,
		site:   site,
		src:    src,
		script: script,
	}
	if p := vm.evals.get(k); p != nil {
		return p
	}
	bc := vm.compileEval(fp, site, src)
	p := vm.link(bc, script)
	// Every evaluation of a tagged template in new eval code has template objects of its own.
	if !hasTemplates(bc) {
		vm.evals.add(k, p)
	}
	return p
}

// compileEval compiles the eval code src of the call site site of fp, or of an indirect eval
// when fp is nil. It throws a SyntaxError when src is not valid.
func (vm *VM) compileEval(fp *funcProto, site int, src wtf8.String) *bytecode.Func {
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
		Poll:               vm.Poll,
	}
	for _, v := range es.Vars {
		if name, ok := strings.CutPrefix(v.Name, "#"); ok {
			opts.PrivateNames = append(opts.PrivateNames, name)
		}
	}
	prog, err := parser.ParseScript(src, opts)
	if err != nil {
		vm.throwCompileError("<eval>", err)
	}
	bc, err := compiler.CompileEval(prog, "<eval>", es, vm.Poll)
	if err != nil {
		vm.throwCompileError("<eval>", err)
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
func (vm *VM) pushEvalScope() {
	o := vm.NewObject(nil)
	o.class = classEvalScope
	cur := &vm.frames[len(vm.frames)-1]
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
func (vm *VM) declareEvalVar(cl *Object, key *Object, depth int, regs []Value, fn int) {
	var o *Object
	for _, scopes := range [...][]*Object{vm.frames[len(vm.frames)-1].scopes, cl.fn.capturedScopes()} {
		if depth <= len(scopes) {
			o = scopes[len(scopes)-depth]
			break
		}
		depth -= len(scopes)
	}
	k := PropKey{key: key}
	switch {
	case !vm.HasOwnProperty(o, k):
		v := Undefined
		if fn >= 0 {
			v = regs[fn]
		}
		vm.DefinePropertyOrThrow(o, k, dataDesc(v, FlagsData))
	case fn >= 0:
		vm.setProp(o, k, regs[fn], Value{o: o})
	}
}
